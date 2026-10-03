package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type tokens struct {
	AccessToken   string   `json:"access_token"`
	RefreshToken  string   `json:"refresh_token"`
	ExpiresIn     int      `json:"expires_in"`
	RecoveryCodes []string `json:"recovery_codes"`
	User          struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
}

func call(t *testing.T, method, url, bearer string, body any, out any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func uniqueName() string { return "user-" + uuid.NewString()[:8] }

const testPassword = "correct horse battery"

// login регистрирует нового пользователя и возвращает его токены.
func login(t *testing.T, srv, username string) tokens {
	t.Helper()
	var tk tokens
	body := map[string]string{"username": username, "password": testPassword, "name": "Тест"}
	if code := call(t, "POST", srv+"/api/v1/auth/register", "", body, &tk); code != http.StatusCreated {
		t.Fatalf("register status = %d", code)
	}
	return tk
}

func TestRegisterAndLogin(t *testing.T) {
	srv, _ := newTestEnv(t)
	name := uniqueName()

	tk := login(t, srv.URL, name)
	if tk.AccessToken == "" || tk.RefreshToken == "" || tk.User.Username != name || tk.ExpiresIn != 900 || len(tk.RecoveryCodes) != 8 {
		t.Fatalf("unexpected tokens: %+v", tk)
	}

	var again tokens
	// Имя пользователя нечувствительно к регистру.
	creds := map[string]string{"username": strings.ToUpper(name), "password": testPassword}
	if code := call(t, "POST", srv.URL+"/api/v1/auth/login", "", creds, &again); code != http.StatusOK || again.User.ID != tk.User.ID {
		t.Fatalf("login: status=%d tokens=%+v", code, again)
	}
	if len(again.RecoveryCodes) != 0 {
		t.Fatal("login must not return recovery codes")
	}

	var me struct{ Username, Name string }
	if code := call(t, "GET", srv.URL+"/api/v1/me", tk.AccessToken, nil, &me); code != http.StatusOK || me.Username != name {
		t.Fatalf("me: status=%d body=%+v", code, me)
	}
	if code := call(t, "PATCH", srv.URL+"/api/v1/me", tk.AccessToken, map[string]string{"name": "Оля"}, &me); code != http.StatusOK || me.Name != "Оля" {
		t.Fatalf("patch me: status=%d body=%+v", code, me)
	}
}

func TestRegisterValidation(t *testing.T) {
	srv, _ := newTestEnv(t)
	url := srv.URL + "/api/v1/auth/register"
	for _, c := range []struct{ user, pass string }{
		{"", testPassword}, {"ab", testPassword}, {"имя-кириллицей", testPassword},
		{"has space", testPassword}, {strings.Repeat("a", 33), testPassword},
		{uniqueName(), "short"}, {uniqueName(), strings.Repeat("x", 129)},
	} {
		if code := call(t, "POST", url, "", map[string]string{"username": c.user, "password": c.pass}, nil); code != http.StatusBadRequest {
			t.Errorf("%q/%d chars: status = %d, want 400", c.user, len(c.pass), code)
		}
	}
	name := uniqueName()
	login(t, srv.URL, name)
	if code := call(t, "POST", url, "", map[string]string{"username": strings.ToUpper(name), "password": testPassword}, nil); code != http.StatusConflict {
		t.Fatalf("duplicate username status = %d, want 409", code)
	}
}

func TestLoginWrongPasswordAndLockout(t *testing.T) {
	srv, _ := newTestEnv(t)
	name := uniqueName()
	login(t, srv.URL, name)
	url := srv.URL + "/api/v1/auth/login"

	if code := call(t, "POST", url, "", map[string]string{"username": "nobody-" + name, "password": testPassword}, nil); code != http.StatusUnauthorized {
		t.Fatalf("unknown user status = %d, want 401", code)
	}
	for i := 0; i < 10; i++ {
		if code := call(t, "POST", url, "", map[string]string{"username": name, "password": "wrong password"}, nil); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, code)
		}
	}
	// После десяти ошибок подряд даже верный пароль не принимается.
	if code := call(t, "POST", url, "", map[string]string{"username": name, "password": testPassword}, nil); code != http.StatusTooManyRequests {
		t.Fatalf("login after lockout: status = %d, want 429", code)
	}
}

func TestRecoverWithCode(t *testing.T) {
	srv, _ := newTestEnv(t)
	name := uniqueName()
	tk := login(t, srv.URL, name)
	url := srv.URL + "/api/v1/auth/recover"
	newPass := "brand new password"

	if code := call(t, "POST", url, "", map[string]string{"username": name, "code": "AAAAA-AAAAA", "new_password": newPass}, nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong code status = %d, want 401", code)
	}
	var rec tokens
	body := map[string]string{"username": name, "code": tk.RecoveryCodes[0], "new_password": newPass}
	if code := call(t, "POST", url, "", body, &rec); code != http.StatusOK || rec.User.ID != tk.User.ID {
		t.Fatalf("recover: status=%d tokens=%+v", code, rec)
	}
	// Код одноразовый.
	if code := call(t, "POST", url, "", body, nil); code != http.StatusUnauthorized {
		t.Fatalf("reused code status = %d, want 401", code)
	}
	// Старые сессии отозваны, старый пароль не подходит, новый подходит.
	if code := call(t, "POST", srv.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": tk.RefreshToken}, nil); code != http.StatusUnauthorized {
		t.Fatalf("old refresh after recover status = %d, want 401", code)
	}
	login := srv.URL + "/api/v1/auth/login"
	if code := call(t, "POST", login, "", map[string]string{"username": name, "password": testPassword}, nil); code != http.StatusUnauthorized {
		t.Fatalf("old password status = %d, want 401", code)
	}
	if code := call(t, "POST", login, "", map[string]string{"username": name, "password": newPass}, nil); code != http.StatusOK {
		t.Fatalf("new password status = %d, want 200", code)
	}
}

func TestChangePasswordAndRecoveryCodes(t *testing.T) {
	srv, _ := newTestEnv(t)
	name := uniqueName()
	tk := login(t, srv.URL, name)
	base := srv.URL + "/api/v1/me"

	if code := call(t, "PUT", base+"/password", tk.AccessToken, map[string]string{"old_password": "nope nope nope", "new_password": "another password"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong old password status = %d, want 401", code)
	}
	var changed tokens
	if code := call(t, "PUT", base+"/password", tk.AccessToken, map[string]string{"old_password": testPassword, "new_password": "another password"}, &changed); code != http.StatusOK || changed.RefreshToken == "" {
		t.Fatalf("change password: status=%d tokens=%+v", code, changed)
	}
	if code := call(t, "POST", srv.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": tk.RefreshToken}, nil); code != http.StatusUnauthorized {
		t.Fatalf("old refresh after change status = %d, want 401", code)
	}

	var cnt struct{ Unused int }
	if code := call(t, "GET", base+"/recovery-codes", changed.AccessToken, nil, &cnt); code != http.StatusOK || cnt.Unused != 8 {
		t.Fatalf("count: status=%d %+v", code, cnt)
	}
	if code := call(t, "POST", base+"/recovery-codes", changed.AccessToken, map[string]string{"password": testPassword}, nil); code != http.StatusUnauthorized {
		t.Fatalf("regenerate with wrong password status = %d, want 401", code)
	}
	var fresh struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if code := call(t, "POST", base+"/recovery-codes", changed.AccessToken, map[string]string{"password": "another password"}, &fresh); code != http.StatusOK || len(fresh.RecoveryCodes) != 8 {
		t.Fatalf("regenerate: status=%d %+v", code, fresh)
	}
	// Старые коды после перевыпуска недействительны.
	body := map[string]string{"username": name, "code": tk.RecoveryCodes[1], "new_password": "yet another pass"}
	if code := call(t, "POST", srv.URL+"/api/v1/auth/recover", "", body, nil); code != http.StatusUnauthorized {
		t.Fatalf("old code after regenerate status = %d, want 401", code)
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	srv, _ := newTestEnv(t)
	tk := login(t, srv.URL, uniqueName())

	var next tokens
	if code := call(t, "POST", srv.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": tk.RefreshToken}, &next); code != http.StatusOK {
		t.Fatalf("refresh status = %d", code)
	}
	if next.RefreshToken == tk.RefreshToken || next.AccessToken == "" {
		t.Fatalf("refresh token was not rotated: %+v", next)
	}

	// Старый токен больше не работает, а его повторное использование отзывает и новый.
	if code := call(t, "POST", srv.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": tk.RefreshToken}, nil); code != http.StatusUnauthorized {
		t.Fatalf("reused refresh status = %d, want 401", code)
	}
	if code := call(t, "POST", srv.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": next.RefreshToken}, nil); code != http.StatusUnauthorized {
		t.Fatalf("refresh after reuse status = %d, want 401", code)
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	srv, _ := newTestEnv(t)
	tk := login(t, srv.URL, uniqueName())
	if code := call(t, "POST", srv.URL+"/api/v1/auth/logout", "", map[string]string{"refresh_token": tk.RefreshToken}, nil); code != http.StatusNoContent {
		t.Fatalf("logout status = %d", code)
	}
	if code := call(t, "POST", srv.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": tk.RefreshToken}, nil); code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want 401", code)
	}
}

func TestProtectedRoutesNeedValidToken(t *testing.T) {
	srv, _ := newTestEnv(t)
	for _, token := range []string{"", "garbage", "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0."} {
		if code := call(t, "GET", srv.URL+"/api/v1/me", token, nil, nil); code != http.StatusUnauthorized {
			t.Errorf("token %q: status = %d, want 401", token, code)
		}
	}
}

func TestDeleteAccountTransfersSharedLists(t *testing.T) {
	srv, pool := newTestEnv(t)
	owner := login(t, srv.URL, uniqueName())
	member := login(t, srv.URL, uniqueName())

	ctx := context.Background()
	var shared, solo uuid.UUID
	for _, r := range []struct {
		id    *uuid.UUID
		title string
	}{{&shared, "Общий"}, {&solo, "Личный"}} {
		if err := pool.QueryRow(ctx, `INSERT INTO lists (title, owner_id) VALUES ($1, $2) RETURNING id`, r.title, owner.User.ID).Scan(r.id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO list_members (list_id, user_id, role) VALUES ($1, $2, 'owner')`, *r.id, owner.User.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO list_members (list_id, user_id) VALUES ($1, $2)`, shared, member.User.ID); err != nil {
		t.Fatal(err)
	}

	if code := call(t, "DELETE", srv.URL+"/api/v1/me", owner.AccessToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete status = %d", code)
	}

	var ownerID uuid.UUID
	var role string
	if err := pool.QueryRow(ctx, `SELECT l.owner_id, lm.role FROM lists l JOIN list_members lm ON lm.list_id = l.id AND lm.user_id = l.owner_id WHERE l.id = $1`, shared).Scan(&ownerID, &role); err != nil {
		t.Fatalf("shared list lost: %v", err)
	}
	if ownerID.String() != member.User.ID || role != "owner" {
		t.Fatalf("shared list owner = %s role = %s, want %s owner", ownerID, role, member.User.ID)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM lists WHERE id = $1`, solo).Scan(&n); err != nil || n != 0 {
		t.Fatalf("personal list should be gone: n=%d err=%v", n, err)
	}
	// Токен удалённого пользователя больше не открывает профиль.
	if code := call(t, "GET", srv.URL+"/api/v1/me", owner.AccessToken, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("me after delete status = %d, want 401", code)
	}
}
