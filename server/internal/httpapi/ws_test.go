package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type wsMsg struct {
	Type    string `json:"type"`
	ListID  string `json:"list_id"`
	Kind    string `json:"kind"`
	Version int64  `json:"version"`
}

func wsURL(srv string) string { return "ws" + strings.TrimPrefix(srv, "http") + "/api/v1/ws" }

// dialWS подключается и проходит авторизацию до сообщения ready.
func dialWS(t *testing.T, srv string, tk tokens) *websocket.Conn {
	t.Helper()
	c := dialRaw(t, srv)
	if err := wsjson.Write(context.Background(), c, map[string]string{"type": "auth", "token": tk.AccessToken}); err != nil {
		t.Fatal(err)
	}
	if m, err := readWS(c, 3*time.Second); err != nil || m.Type != "ready" {
		t.Fatalf("expected ready, got %+v err=%v", m, err)
	}
	return c
}

func dialRaw(t *testing.T, srv string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(srv), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func readWS(c *websocket.Conn, timeout time.Duration) (wsMsg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		return wsMsg{}, err
	}
	var m wsMsg
	return m, json.Unmarshal(b, &m)
}

// expectNothing проверяет, что за короткое время сообщений нет. Таймаут чтения закрывает соединение
// (так устроена библиотека), поэтому вызывать его можно один раз, в конце сценария.
func expectNothing(t *testing.T, name string, c *websocket.Conn) {
	t.Helper()
	if m, err := readWS(c, 300*time.Millisecond); err == nil {
		t.Errorf("%s got unexpected message %+v", name, m)
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("%s: connection broke: %v", name, err)
	}
}

func expectMsg(t *testing.T, name string, c *websocket.Conn, typ, kind string) wsMsg {
	t.Helper()
	m, err := readWS(c, 3*time.Second)
	if err != nil {
		t.Fatalf("%s: no message (%s/%s): %v", name, typ, kind, err)
	}
	if m.Type != typ || m.Kind != kind {
		t.Fatalf("%s: got %+v, want %s/%s", name, m, typ, kind)
	}
	return m
}

func TestWSNotifiesListMembersOnly(t *testing.T) {
	e := newListEnv(t)
	stranger := login(t, e.srv, uniqueName())
	owner, guest, out := dialWS(t, e.srv, e.owner), dialWS(t, e.srv, e.guest), dialWS(t, e.srv, stranger)

	// Новая позиция: оба участника получают версию, посторонний ничего.
	it, id := e.put(t, e.owner, map[string]any{"name": "Молоко"})
	for name, c := range map[string]*websocket.Conn{"owner": owner, "guest": guest} {
		m := expectMsg(t, name, c, "list_changed", "items")
		if m.ListID != e.listID || m.Version != it.Version {
			t.Fatalf("%s: %+v, want list %s version %d", name, m, e.listID, it.Version)
		}
	}

	// Отметка «куплено» гостем доходит до владельца с новой версией.
	var bought itemResp
	call(t, "PATCH", e.itemURL(id), e.guest.AccessToken, map[string]any{"is_bought": true}, &bought)
	if m := expectMsg(t, "owner", owner, "list_changed", "items"); m.Version != bought.Version {
		t.Fatalf("owner version %d, want %d", m.Version, bought.Version)
	}
	expectMsg(t, "guest", guest, "list_changed", "items")

	// Очистка купленного и удаление тоже уведомляют.
	call(t, "POST", e.srv+"/api/v1/lists/"+e.listID+"/items/clear-bought", e.owner.AccessToken, nil, nil)
	expectMsg(t, "owner", owner, "list_changed", "items")
	expectMsg(t, "guest", guest, "list_changed", "items")

	// Переименование списка.
	call(t, "PATCH", e.srv+"/api/v1/lists/"+e.listID, e.owner.AccessToken, map[string]string{"title": "Дача"}, nil)
	expectMsg(t, "owner", owner, "list_changed", "list")
	expectMsg(t, "guest", guest, "list_changed", "list")
	expectNothing(t, "stranger", out)
}

func TestWSMembershipChanges(t *testing.T) {
	e := newListEnv(t)
	newcomer := login(t, e.srv, uniqueName())
	owner, guest, newbie := dialWS(t, e.srv, e.owner), dialWS(t, e.srv, e.guest), dialWS(t, e.srv, newcomer)

	// Вступление по коду: все текущие участники и сам вступивший узнают об изменении состава.
	inv := mustInvite(t, e.srv, e.owner, e.listID, nil)
	call(t, "POST", e.srv+"/api/v1/invites/accept", newcomer.AccessToken, map[string]string{"code": inv.Code}, nil)
	for name, c := range map[string]*websocket.Conn{"owner": owner, "guest": guest, "newcomer": newbie} {
		expectMsg(t, name, c, "list_changed", "members")
	}

	// Убранный участник получает уведомление последний раз и дальше изменений не видит.
	call(t, "DELETE", e.srv+"/api/v1/lists/"+e.listID+"/members/"+e.guest.User.ID, e.owner.AccessToken, nil, nil)
	for name, c := range map[string]*websocket.Conn{"owner": owner, "guest": guest, "newcomer": newbie} {
		expectMsg(t, name, c, "list_changed", "members")
	}
	e.put(t, e.owner, map[string]any{"name": "Хлеб"})
	expectMsg(t, "owner", owner, "list_changed", "items")
	expectMsg(t, "newcomer", newbie, "list_changed", "items")
	expectNothing(t, "removed guest", guest)

	// Новый список виден другим устройствам создателя.
	second := dialWS(t, e.srv, e.owner)
	l := mustCreateList(t, e.srv, e.owner, "Новый")
	if m := expectMsg(t, "owner device 2", second, "list_changed", "list"); m.ListID != l.ID {
		t.Fatalf("list id %s, want %s", m.ListID, l.ID)
	}
}

func TestWSAuthRules(t *testing.T) {
	srv, _ := newTestEnv(t)
	tk := login(t, srv.URL, uniqueName())

	closeCode := func(c *websocket.Conn) websocket.StatusCode {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _, err := c.Read(ctx)
		return websocket.CloseStatus(err)
	}

	// Неверный токен и неверный тип первого сообщения: закрытие с кодом 4401.
	for name, msg := range map[string]map[string]string{
		"bad token":  {"type": "auth", "token": "garbage"},
		"wrong type": {"type": "hello", "token": tk.AccessToken},
		"empty":      {},
	} {
		c := dialRaw(t, srv.URL)
		wsjson.Write(context.Background(), c, msg)
		if code := closeCode(c); code != 4401 {
			t.Errorf("%s: close code = %d, want 4401", name, code)
		}
	}

	// Токен в адресе не принимается: авторизация только первым сообщением.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(srv.URL)+"?token="+tk.AccessToken, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tk.AccessToken}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	// Если бы токен из адреса или заголовка принимался, соединение осталось бы открытым.
	wsjson.Write(ctx, c, map[string]string{"type": "ping"})
	if code := closeCode(c); code != 4401 {
		t.Errorf("token in URL/header: close code = %d, want 4401 (auth only via first message)", code)
	}
}

func TestWSClosesWhenAccessTokenExpires(t *testing.T) {
	srv, _ := newTestEnvTTL(t, 1500*time.Millisecond)
	tk := login(t, srv.URL, uniqueName())
	c := dialWS(t, srv.URL, tk)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if code := websocket.CloseStatus(err); code != 4401 {
		t.Fatalf("close code = %d (err %v), want 4401", code, err)
	}
	// Токен уже просрочен: новое подключение с ним не проходит.
	time.Sleep(200 * time.Millisecond)
	c2 := dialRaw(t, srv.URL)
	wsjson.Write(context.Background(), c2, map[string]string{"type": "auth", "token": tk.AccessToken})
	_, _, err = c2.Read(ctx)
	if code := websocket.CloseStatus(err); code != 4401 {
		t.Fatalf("expired token: close code = %d, want 4401", code)
	}
}

func TestWSOldestConnectionIsDroppedBeyondLimit(t *testing.T) {
	srv, _ := newTestEnv(t)
	tk := login(t, srv.URL, uniqueName())
	first := dialWS(t, srv.URL, tk)
	for i := 0; i < 10; i++ {
		dialWS(t, srv.URL, tk)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := first.Read(ctx)
	if code := websocket.CloseStatus(err); code != websocket.StatusPolicyViolation {
		t.Fatalf("oldest connection close code = %d (err %v), want policy violation", code, err)
	}
}

func TestWSResyncAfterListenerReconnect(t *testing.T) {
	e := newListEnv(t)
	c := dialWS(t, e.srv, e.owner)

	// Обрываем соединение, которое слушает Postgres: hub должен переподключиться и попросить клиентов перечитать данные.
	if _, err := e.pool.Exec(context.Background(),
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query LIKE 'LISTEN list_changes%' AND pid <> pg_backend_pid()`); err != nil {
		t.Fatal(err)
	}
	if m, err := readWS(c, 8*time.Second); err != nil || m.Type != "resync" {
		t.Fatalf("expected resync, got %+v err=%v", m, err)
	}

	// После переподключения обычные уведомления снова приходят.
	e.put(t, e.owner, map[string]any{"name": "Чай"})
	expectMsg(t, "owner", c, "list_changed", "items")
}
