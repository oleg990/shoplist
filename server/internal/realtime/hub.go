package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"shoplist/server/internal/store"
)

const (
	maxConnsPerUser = 10
	sendBuffer      = 16
)

// Client — одно подключение пользователя (устройство).
type Client struct {
	UserID uuid.UUID
	send   chan []byte
	closed chan struct{}
	once   sync.Once
}

// Send возвращает канал сообщений для записи в сокет. Он не закрывается; конец подключения виден по Done.
func (c *Client) Send() <-chan []byte { return c.send }

// Done закрывается, когда подключение нужно закончить.
func (c *Client) Done() <-chan struct{} { return c.closed }

func (c *Client) close() { c.once.Do(func() { close(c.closed) }) }

// Hub хранит подключения и раздаёт им события из Postgres.
type Hub struct {
	pool  *pgxpool.Pool
	q     *store.Queries
	log   *slog.Logger
	ready chan struct{}

	mu      sync.Mutex
	clients map[uuid.UUID][]*Client // по пользователю, старые первыми
}

func NewHub(pool *pgxpool.Pool, log *slog.Logger) *Hub {
	return &Hub{pool: pool, q: store.New(pool), log: log, ready: make(chan struct{}), clients: map[uuid.UUID][]*Client{}}
}

// Ready закрывается, когда hub начал слушать события (нужно в тестах).
func (h *Hub) Ready() <-chan struct{} { return h.ready }

// Add регистрирует подключение. Если у пользователя уже много устройств, самое старое отключается.
func (h *Hub) Add(userID uuid.UUID) *Client {
	c := &Client{UserID: userID, send: make(chan []byte, sendBuffer), closed: make(chan struct{})}
	h.mu.Lock()
	defer h.mu.Unlock()
	list := append(h.clients[userID], c)
	if len(list) > maxConnsPerUser {
		list[0].close()
		list = list[1:]
	}
	h.clients[userID] = list
	return c
}

func (h *Hub) Remove(c *Client) {
	c.close()
	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.clients[c.UserID]
	for i, x := range list {
		if x == c {
			list = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(h.clients, c.UserID)
	} else {
		h.clients[c.UserID] = list
	}
}

// deliver кладёт сообщение в очередь клиента. Медленного клиента отключаем: он переподключится и догонит через since.
func (h *Hub) deliver(userID uuid.UUID, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.clients[userID] {
		select {
		case c.send <- msg:
		default:
			c.close()
		}
	}
}

func (h *Hub) broadcastAll(msg []byte) {
	h.mu.Lock()
	ids := make([]uuid.UUID, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.deliver(id, msg)
	}
}

// Run слушает канал Postgres, пока не отменён ctx. При потере соединения переподключается
// и просит всех клиентов перечитать данные, потому что события за время разрыва потеряны.
func (h *Hub) Run(ctx context.Context) {
	first := true
	backoff := time.Second
	for ctx.Err() == nil {
		err := h.listen(ctx, first)
		first = false
		if ctx.Err() != nil {
			return
		}
		h.log.Error("realtime: listener stopped, reconnecting", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (h *Hub) listen(ctx context.Context, first bool) error {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return err
	}
	if first {
		close(h.ready)
	} else {
		h.broadcastAll(resyncMessage)
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		h.dispatch(ctx, n.Payload)
	}
}

var resyncMessage = []byte(`{"type":"resync"}`)

type message struct {
	Type    string    `json:"type"`
	ListID  uuid.UUID `json:"list_id"`
	Kind    string    `json:"kind"`
	Version int64     `json:"version,omitempty"`
}

func (h *Hub) dispatch(ctx context.Context, payload string) {
	var ev Event
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		h.log.Warn("realtime: bad payload", "err", err)
		return
	}
	ids, err := h.q.ListMemberIDs(ctx, ev.ListID)
	if err != nil {
		h.log.Error("realtime: members lookup", "err", err)
		return
	}
	if ev.AlsoUser != nil {
		ids = append(ids, *ev.AlsoUser)
	}
	msg, _ := json.Marshal(message{Type: "list_changed", ListID: ev.ListID, Kind: ev.Kind, Version: ev.Version})
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			h.deliver(id, msg)
		}
	}
}
