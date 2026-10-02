package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeExpo подменяет Expo Push API: запоминает сообщения и может отвечать ошибками.
type fakeExpo struct {
	*httptest.Server
	mu            sync.Mutex
	msgs          []pushMsg
	status        int             // если не 0, отвечать таким кодом
	notRegistered map[string]bool // токены, на которые отвечать DeviceNotRegistered
	auth          string          // последний заголовок Authorization
}

type pushMsg struct {
	To    string         `json:"to"`
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Data  map[string]any `json:"data"`
}

func newFakeExpo(t *testing.T) *fakeExpo {
	t.Helper()
	f := &fakeExpo{notRegistered: map[string]bool{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch []pushMsg
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = r.Header.Get("Authorization")
		if f.status != 0 {
			http.Error(w, "boom", f.status)
			return
		}
		f.msgs = append(f.msgs, batch...)
		type result map[string]any
		out := make([]result, len(batch))
		for i, m := range batch {
			if f.notRegistered[m.To] {
				out[i] = result{"status": "error", "message": "not registered", "details": map[string]string{"error": "DeviceNotRegistered"}}
			} else {
				out[i] = result{"status": "ok", "id": uuid.NewString()}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": out})
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeExpo) sent() []pushMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pushMsg(nil), f.msgs...)
}

// waitFor ждёт, пока накопится хотя бы n сообщений.
func (f *fakeExpo) waitFor(t *testing.T, n int) []pushMsg {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := f.sent(); len(got) >= n {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected %d push messages, got %+v", n, f.sent())
	return nil
}

// expectNone проверяет, что за время, большее окна отправки, ничего не ушло сверх have сообщений.
func (f *fakeExpo) expectNone(t *testing.T, have int) {
	t.Helper()
	time.Sleep(pushWindow*2 + 100*time.Millisecond)
	if got := f.sent(); len(got) != have {
		t.Fatalf("expected %d push messages, got %d: %+v", have, len(got), got)
	}
}

func pushToken() string {
	return "ExponentPushToken[" + strings.ReplaceAll(uuid.NewString(), "-", "") + "]"
}

func registerDevice(t *testing.T, srv string, who tokens, token string) {
	t.Helper()
	if code := call(t, "PUT", srv+"/api/v1/devices", who.AccessToken, map[string]string{"expo_push_token": token, "platform": "android"}, nil); code != http.StatusNoContent {
		t.Fatalf("register device status = %d", code)
	}
}

func TestDeviceRegistration(t *testing.T) {
	srv, mailer, pool := newTestEnv(t)
	a := login(t, srv.URL, mailer, uniqueEmail())
	b := login(t, srv.URL, mailer, uniqueEmail())
	token := pushToken()
	count := func(userID string) (n int) {
		pool.QueryRow(context.Background(), `SELECT count(*) FROM devices WHERE user_id = $1`, userID).Scan(&n)
		return
	}

	for _, body := range []map[string]string{
		{"expo_push_token": "", "platform": "ios"},
		{"expo_push_token": "not-a-token", "platform": "ios"},
		{"expo_push_token": "ExponentPushToken[short]", "platform": "ios"},
		{"expo_push_token": token, "platform": "windows"},
		{"expo_push_token": token},
	} {
		if code := call(t, "PUT", srv.URL+"/api/v1/devices", a.AccessToken, body, nil); code != http.StatusBadRequest {
			t.Errorf("register %v: status = %d, want 400", body, code)
		}
	}
	if code := call(t, "PUT", srv.URL+"/api/v1/devices", "", map[string]string{"expo_push_token": token, "platform": "ios"}, nil); code != http.StatusUnauthorized {
		t.Errorf("register without token status = %d, want 401", code)
	}

	// Повтор безопасен; токен, перешедший к другому пользователю, переписывается на него.
	registerDevice(t, srv.URL, a, token)
	registerDevice(t, srv.URL, a, token)
	if count(a.User.ID) != 1 {
		t.Fatalf("user a devices = %d, want 1", count(a.User.ID))
	}
	registerDevice(t, srv.URL, b, token)
	if count(a.User.ID) != 0 || count(b.User.ID) != 1 {
		t.Fatalf("device was not reassigned: a=%d b=%d", count(a.User.ID), count(b.User.ID))
	}

	// Чужой токен удалить нельзя, свой можно (и повторно).
	call(t, "DELETE", srv.URL+"/api/v1/devices", a.AccessToken, map[string]string{"expo_push_token": token}, nil)
	if count(b.User.ID) != 1 {
		t.Fatal("foreign device was deleted")
	}
	for i := 0; i < 2; i++ {
		if code := call(t, "DELETE", srv.URL+"/api/v1/devices", b.AccessToken, map[string]string{"expo_push_token": token}, nil); code != http.StatusNoContent {
			t.Fatalf("unregister status = %d", code)
		}
	}
	if count(b.User.ID) != 0 {
		t.Fatal("device was not deleted")
	}
}

func TestDeviceCapPerUser(t *testing.T) {
	srv, mailer, pool := newTestEnv(t)
	u := login(t, srv.URL, mailer, uniqueEmail())
	for i := 0; i < 23; i++ {
		registerDevice(t, srv.URL, u, pushToken())
	}
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM devices WHERE user_id = $1`, u.User.ID).Scan(&n)
	if n != 20 {
		t.Fatalf("devices = %d, want 20 (oldest are dropped)", n)
	}
}

func TestPushOnNewItemGoesToOthersOnly(t *testing.T) {
	e := newListEnv(t)
	stranger := login(t, e.srv, e.mailer, uniqueEmail())
	call(t, "PATCH", e.srv+"/api/v1/me", e.owner.AccessToken, map[string]string{"name": "Оля"}, nil)
	ownerTok, guestTok, guestTok2, strangerTok := pushToken(), pushToken(), pushToken(), pushToken()
	registerDevice(t, e.srv, e.owner, ownerTok)
	registerDevice(t, e.srv, e.guest, guestTok)
	registerDevice(t, e.srv, e.guest, guestTok2) // два устройства у гостя
	registerDevice(t, e.srv, stranger, strangerTok)

	_, id := e.put(t, e.owner, map[string]any{"name": "Молоко"})
	got := e.expo.waitFor(t, 2)
	e.expo.expectNone(t, 2)
	to := map[string]bool{}
	for _, m := range got {
		to[m.To] = true
		if m.Title != "Продукты" || m.Body != "Добавлено: Молоко (Оля)" || m.Data["list_id"] != e.listID || m.Data["kind"] != "items" {
			t.Errorf("unexpected push: %+v", m)
		}
	}
	if !to[guestTok] || !to[guestTok2] || to[ownerTok] || to[strangerTok] {
		t.Fatalf("recipients = %v: only the guest's two devices expected", to)
	}

	// Повторный PUT той же позиции (правка) уведомлений не создаёт.
	call(t, "PUT", e.itemURL(id), e.owner.AccessToken, map[string]any{"name": "Молоко 3.2%"}, nil)
	e.expo.expectNone(t, 2)
}

func TestPushBatchesRapidAdditions(t *testing.T) {
	e := newListEnv(t)
	registerDevice(t, e.srv, e.guest, pushToken())
	for _, name := range []string{"Хлеб", "Молоко", "Яйца", "Сыр", "Чай"} {
		e.put(t, e.owner, map[string]any{"name": name})
	}
	got := e.expo.waitFor(t, 1)
	e.expo.expectNone(t, 1)
	if got[0].Body != "Добавлено 5: Хлеб, Молоко, Яйца и ещё 2 (Кто-то)" {
		t.Fatalf("body = %q", got[0].Body)
	}
}

func TestPushWhenEverythingBought(t *testing.T) {
	e := newListEnv(t)
	call(t, "PATCH", e.srv+"/api/v1/me", e.guest.AccessToken, map[string]string{"name": "Петя"}, nil)
	ownerTok, guestTok := pushToken(), pushToken()
	registerDevice(t, e.srv, e.owner, ownerTok)
	registerDevice(t, e.srv, e.guest, guestTok)
	_, a := e.put(t, e.owner, map[string]any{"name": "Хлеб"})
	_, b := e.put(t, e.owner, map[string]any{"name": "Чай"})
	e.expo.waitFor(t, 1) // уведомление о добавлении ушло гостю
	e.expo.expectNone(t, 1)

	// Одна из двух отмечена: рано. Когда отмечена последняя, владелец (но не сам Петя) получает «Всё куплено».
	call(t, "PATCH", e.itemURL(a), e.guest.AccessToken, map[string]any{"is_bought": true}, nil)
	e.expo.expectNone(t, 1)
	call(t, "PATCH", e.itemURL(b), e.guest.AccessToken, map[string]any{"is_bought": true}, nil)
	got := e.expo.waitFor(t, 2)[1]
	if got.To != ownerTok || got.Title != "Всё куплено" || got.Body != "В списке «Продукты» отмечены все позиции (Петя)" || got.Data["kind"] != "completed" {
		t.Fatalf("completion push: %+v", got)
	}

	// Повторная отметка уже купленного ничего не шлёт, снятие и новая отметка шлют снова.
	call(t, "PATCH", e.itemURL(b), e.guest.AccessToken, map[string]any{"is_bought": true}, nil)
	e.expo.expectNone(t, 2)
	call(t, "PATCH", e.itemURL(b), e.guest.AccessToken, map[string]any{"is_bought": false}, nil)
	call(t, "PATCH", e.itemURL(b), e.owner.AccessToken, map[string]any{"is_bought": true}, nil)
	if last := e.expo.waitFor(t, 3)[2]; last.To != guestTok || last.Title != "Всё куплено" {
		t.Fatalf("second completion push: %+v", last)
	}
}

func TestPushRemovesDeadTokensAndSurvivesExpoFailure(t *testing.T) {
	e := newListEnv(t)
	dead, alive := pushToken(), pushToken()
	registerDevice(t, e.srv, e.guest, dead)
	registerDevice(t, e.srv, e.guest, alive)
	e.expo.mu.Lock()
	e.expo.notRegistered[dead] = true
	e.expo.mu.Unlock()

	e.put(t, e.owner, map[string]any{"name": "Хлеб"})
	e.expo.waitFor(t, 2)
	deadline := time.Now().Add(2 * time.Second)
	for {
		var n int
		e.pool.QueryRow(context.Background(), `SELECT count(*) FROM devices WHERE expo_push_token = $1`, dead).Scan(&n)
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("DeviceNotRegistered token was not deleted")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var n int
	e.pool.QueryRow(context.Background(), `SELECT count(*) FROM devices WHERE expo_push_token = $1`, alive).Scan(&n)
	if n != 1 {
		t.Fatal("working token must stay")
	}

	// Если Expo недоступен, запросы пользователей продолжают работать.
	e.expo.mu.Lock()
	e.expo.status = http.StatusInternalServerError
	e.expo.mu.Unlock()
	e.put(t, e.owner, map[string]any{"name": "Чай"})
	time.Sleep(pushWindow + 150*time.Millisecond)
	if got := e.items(t, e.owner, ""); len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(got.Items))
	}
}
