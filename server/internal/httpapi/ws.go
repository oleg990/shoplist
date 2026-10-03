package httpapi

import (
	"context"
	"net/url"
	"time"

	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	wsAuthTimeout = 5 * time.Second
	wsPingEvery   = 30 * time.Second
	wsWriteTimout = 10 * time.Second

	// Коды закрытия из диапазона приложений (4000–4999). Клиент по ним понимает, что делать.
	wsCloseUnauthorized = websocket.StatusCode(4401) // токен неверный или истёк: обновить токен и подключиться заново
)

// ws: поток уведомлений «в списке X что-то изменилось». Токен передаётся первым сообщением
// {"type":"auth","token":"..."}, а не в адресе, чтобы он не попадал в логи прокси.
// Соединение живёт, пока действует access-токен; потом сервер закрывает его с кодом 4401,
// клиент обновляет токен и подключается заново. После подключения (и после любого разрыва)
// клиент должен один раз запросить изменения через GET .../items?since=N.
func (a *API) ws(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, a.wsAcceptOptions())
	if err != nil {
		return // Accept уже ответил клиенту
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 10)

	authCtx, cancel := context.WithTimeout(r.Context(), wsAuthTimeout)
	var first struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	err = wsjson.Read(authCtx, conn, &first)
	cancel()
	if err != nil || first.Type != "auth" {
		conn.Close(wsCloseUnauthorized, "auth message required")
		return
	}
	uid, exp, err := a.auth.ParseAccessTokenExp(first.Token)
	if err != nil {
		conn.Close(wsCloseUnauthorized, "invalid or expired token")
		return
	}
	if _, err := a.auth.GetUser(r.Context(), uid); err != nil {
		conn.Close(wsCloseUnauthorized, "user not found")
		return
	}

	client := a.hub.Add(uid)
	defer a.hub.Remove(client)

	ctx := conn.CloseRead(r.Context()) // читаем только служебные кадры, ctx отменится при закрытии
	write := func(msg []byte) error {
		wctx, cancel := context.WithTimeout(ctx, wsWriteTimout)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, msg)
	}
	if err := write([]byte(`{"type":"ready"}`)); err != nil {
		return
	}

	expiry := time.NewTimer(time.Until(exp))
	defer expiry.Stop()
	ping := time.NewTicker(wsPingEvery)
	defer ping.Stop()

	for {
		select {
		case msg := <-client.Send():
			if err := write(msg); err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, wsWriteTimout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case <-expiry.C:
			conn.Close(wsCloseUnauthorized, "token expired")
			return
		case <-client.Done():
			conn.Close(websocket.StatusPolicyViolation, "connection replaced or too slow, reconnect and resync")
			return
		case <-ctx.Done():
			return
		}
	}
}

// wsAcceptOptions пускает в WebSocket браузеры с разрешённых адресов (по умолчанию только тот же хост).
func (a *API) wsAcceptOptions() *websocket.AcceptOptions {
	if len(a.origins) == 0 {
		return nil
	}
	patterns := make([]string, 0, len(a.origins))
	for _, o := range a.origins {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			patterns = append(patterns, u.Host)
		}
	}
	return &websocket.AcceptOptions{OriginPatterns: patterns}
}
