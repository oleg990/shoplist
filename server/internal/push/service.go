// Package push регистрирует устройства и отправляет push-уведомления участникам списка через Expo.
package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"shoplist/server/internal/store"
)

var (
	ErrInvalidToken    = errors.New("invalid expo push token")
	ErrInvalidPlatform = errors.New("platform must be android or ios")
)

// ExponentPushToken[xxxxxxxxxxxxxxxxxxxxxx] (старый формат ExpoPushToken[...] тоже встречается).
var tokenRE = regexp.MustCompile(`^Expo(nent)?PushToken\[[A-Za-z0-9_-]{8,200}\]$`)

const (
	maxShownNames = 3
	maxBodyRunes  = 150
)

type Service struct {
	q      *store.Queries
	sender Sender
	log    *slog.Logger
	// window — на сколько откладывается отправка о добавленных позициях, чтобы пачку из десяти товаров
	// превратить в одно уведомление.
	window time.Duration

	mu      sync.Mutex
	pending map[string]*pendingAdds
	wg      sync.WaitGroup // отправки, которые ещё идут
}

type pendingAdds struct {
	listID, actorID uuid.UUID
	names           []string
	total           int
	timer           *time.Timer
}

func NewService(pool *pgxpool.Pool, sender Sender, window time.Duration, log *slog.Logger) *Service {
	return &Service{q: store.New(pool), sender: sender, window: window, log: log, pending: map[string]*pendingAdds{}}
}

// RegisterDevice сохраняет push-токен устройства. Повторный вызов безопасен; токен, ранее
// принадлежавший другому пользователю (телефон перешёл другому человеку), переписывается на текущего.
func (s *Service) RegisterDevice(ctx context.Context, userID uuid.UUID, token, platform string) error {
	if !tokenRE.MatchString(token) {
		return ErrInvalidToken
	}
	if platform != "android" && platform != "ios" {
		return ErrInvalidPlatform
	}
	if err := s.q.UpsertDevice(ctx, store.UpsertDeviceParams{UserID: userID, ExpoPushToken: token, Platform: platform}); err != nil {
		return err
	}
	return s.q.TrimUserDevices(ctx, userID)
}

// UnregisterDevice удаляет токен пользователя (вызывать при выходе из аккаунта). Повтор безопасен.
func (s *Service) UnregisterDevice(ctx context.Context, userID uuid.UUID, token string) error {
	_, err := s.q.DeleteUserDevice(ctx, store.DeleteUserDeviceParams{ExpoPushToken: token, UserID: userID})
	return err
}

// ItemAdded запоминает новую позицию. Уведомление уйдёт через s.window одним сообщением на пачку.
func (s *Service) ItemAdded(listID, actorID uuid.UUID, itemName string) {
	key := listID.String() + "/" + actorID.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pending[key]
	if p == nil {
		p = &pendingAdds{listID: listID, actorID: actorID}
		s.wg.Add(1)
		p.timer = time.AfterFunc(s.window, func() { defer s.wg.Done(); s.flush(key) })
		s.pending[key] = p
	}
	p.total++
	if len(p.names) < maxShownNames {
		p.names = append(p.names, itemName)
	}
}

// ListCompleted сообщает остальным участникам, что в списке отмечено всё. Отправка идёт в фоне,
// чтобы запрос пользователя не ждал ответа Expo.
func (s *Service) ListCompleted(_ context.Context, listID, actorID uuid.UUID) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		title, actor, ok := s.context(ctx, listID, actorID)
		if !ok {
			return
		}
		s.send(ctx, listID, actorID, "Всё куплено", fmt.Sprintf("В списке «%s» отмечены все позиции (%s)", title, actor), "completed")
	}()
}

// Close отправляет всё, что ещё ждёт своей очереди, и дожидается текущих отправок (при остановке сервера).
func (s *Service) Close() {
	defer s.wg.Wait()
	s.mu.Lock()
	keys := make([]string, 0, len(s.pending))
	for k := range s.pending {
		keys = append(keys, k)
	}
	s.mu.Unlock()
	for _, k := range keys {
		s.flush(k)
	}
}

func (s *Service) flush(key string) {
	s.mu.Lock()
	p := s.pending[key]
	delete(s.pending, key)
	s.mu.Unlock()
	if p == nil {
		return
	}
	if p.timer.Stop() {
		// Таймер не успел сработать (нас позвал Close), его функция не выполнится и сама не освободит wg.
		s.wg.Done()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	title, actor, ok := s.context(ctx, p.listID, p.actorID)
	if !ok {
		return
	}
	var body string
	if p.total == 1 {
		body = fmt.Sprintf("Добавлено: %s (%s)", p.names[0], actor)
	} else {
		list := strings.Join(p.names, ", ")
		if p.total > len(p.names) {
			list += fmt.Sprintf(" и ещё %d", p.total-len(p.names))
		}
		body = fmt.Sprintf("Добавлено %d: %s (%s)", p.total, list, actor)
	}
	s.send(ctx, p.listID, p.actorID, title, truncate(body, maxBodyRunes), "items")
}

// context возвращает название списка и имя того, кто его изменил. Если списка уже нет, ok=false.
func (s *Service) context(ctx context.Context, listID, actorID uuid.UUID) (title, actor string, ok bool) {
	title, err := s.q.GetListTitle(ctx, listID)
	if err != nil {
		return "", "", false
	}
	actor, _ = s.q.GetUserName(ctx, actorID)
	if actor = strings.TrimSpace(actor); actor == "" {
		actor = "Кто-то"
	}
	return title, actor, true
}

// send рассылает сообщение всем устройствам участников списка, кроме автора изменения.
func (s *Service) send(ctx context.Context, listID, actorID uuid.UUID, title, body, kind string) {
	members, err := s.q.ListMemberIDs(ctx, listID)
	if err != nil {
		s.log.Error("push: members", "err", err)
		return
	}
	recipients := make([]uuid.UUID, 0, len(members))
	for _, id := range members {
		if id != actorID {
			recipients = append(recipients, id)
		}
	}
	if len(recipients) == 0 {
		return
	}
	devices, err := s.q.ListDevicesForUsers(ctx, recipients)
	if err != nil {
		s.log.Error("push: devices", "err", err)
		return
	}
	if len(devices) == 0 {
		return
	}
	msgs := make([]Message, len(devices))
	for i, d := range devices {
		msgs[i] = Message{
			To: d.ExpoPushToken, Title: title, Body: body, Sound: "default", ChannelID: "default",
			Data: map[string]any{"list_id": listID.String(), "kind": kind},
		}
	}
	results, err := s.sender.Send(ctx, msgs)
	if err != nil {
		// Уведомления второстепенны: при сбое Expo ничего не повторяем, приложение догонит данные само.
		s.log.Warn("push: send failed", "err", err)
	}
	for i, r := range results {
		if r.Status != "ok" && r.Error == "DeviceNotRegistered" {
			if err := s.q.DeleteDeviceByToken(ctx, msgs[i].To); err != nil {
				s.log.Warn("push: delete stale token", "err", err)
			}
		}
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
