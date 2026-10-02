package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

type tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	User         struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
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

func uniqueEmail() string { return fmt.Sprintf("user-%s@example.com", uuid.NewString()[:8]) }

func login(t *testing.T, srv string, m *captureMailer, email string) tokens {
	t.Helper()
	if code := call(t, "POST", srv+"/api/v1/auth/request-code", "", map[string]string{"email": email}, nil); code != http.StatusNoContent {
		t.Fatalf("request-code status = %d", code)
	}
	var tk tokens
	if code := call(t, "POST", srv+"/api/v1/auth/verify", "", map[string]string{"email": email, "code": m.code(email)}, &tk); code != http.StatusOK {
		t.Fatalf("verify status = %d", code)
	}
	return tk
}

func TestLoginFlow(t *testing.T) {
	srv, mailer, _ := newTestEnv(t)
	email := uniqueEmail()

	tk := login(t, srv.URL, mailer, email)
	if tk.AccessToken == "" || tk.RefreshToken == "" || tk.User.Email != email || tk.ExpiresIn != 900 {
		t.Fatalf("unexpected tokens: %+v", tk)
	}

	var me struct{ Email, Name string }
	if code := call(t, "GET", srv.URL+"/api/v1/me", tk.AccessToken, nil, &me); code != http.StatusOK || me.Email != email {
		t.Fatalf("me: status=%d body=%+v", code, me)
	}
	if code := call(t, "PATCH", srv.URL+"/api/v1/me", tk.AccessToken, map[string]string{"name": "Оля"}, &me); code != http.StatusOK || me.Name != "Оля" {
		t.Fatalf("patch me: status=%d body=%+v", code, me)
	}
}

func TestSameEmailSameAccount(t *testing.T) {
	srv, mailer, pool := newTestEnv(t)
	email := uniqueEmail()
	first := login(t, srv.URL, mailer, email)

	// Пауза между кодами здесь не нужна: сбрасываем её, удалив использованные коды.
	if _, err := pool.Exec(context.Background(), `DELETE FROM login_codes WHERE email = $1`, email); err != nil {
		t.Fatal(err)
	}
	second := login(t, srv.URL, mailer, email)
	if second.User.ID != first.User.ID {
		t.Fatalf("second login created another user: %s vs %s", second.User.ID, first.User.ID)
	}
}

func TestRequestCodeValidationAndThrottle(t *testing.T) {
	srv, _, _ := newTestEnv(t)
	for _, bad := range []string{"", "not-an-email", "a@b", "Имя <a@b.co>"} {
		if code := call(t, "POST", srv.URL+"/api/v1/auth/request-code", "", map[string]string{"email": bad}, nil); code != http.StatusBadRequest {
			t.Errorf("email %q: status = %d, want 400", bad, code)
		}
	}

	email := uniqueEmail()
	if code := call(t, "POST", srv.URL+"/api/v1/auth/request-code", "", map[string]string{"email": email}, nil); code != http.StatusNoContent {
		t.Fatalf("first request status = %d", code)
	}
	if code := call(t, "POST", srv.URL+"/api/v1/auth/request-code", "", map[string]string{"email": email}, nil); code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", code)
	}
}

func TestWrongCodeLocksAfterFiveAttempts(t *testing.T) {
	srv, mailer, _ := newTestEnv(t)
	email := uniqueEmail()
	call(t, "POST", srv.URL+"/api/v1/auth/request-code", "", map[string]string{"email": email}, nil)
	right := mailer.code(email)
	wrong := "000000"
	if right == wrong {
		wrong = "111111"
	}
	for i := 0; i < 5; i++ {
		if code := call(t, "POST", srv.URL+"/api/v1/auth/verify", "", map[string]string{"email": email, "code": wrong}, nil); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d", i, code)
		}
	}
	// После пяти ошибок даже верный код не принимается.
	if code := call(t, "POST", srv.URL+"/api/v1/auth/verify", "", map[string]string{"email": email, "code": right}, nil); code != http.StatusUnauthorized {
		t.Fatalf("right code after lockout: status = %d, want 401", code)
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	srv, mailer, _ := newTestEnv(t)
	tk := login(t, srv.URL, mailer, uniqueEmail())

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
	srv, mailer, _ := newTestEnv(t)
	tk := login(t, srv.URL, mailer, uniqueEmail())
	if code := call(t, "POST", srv.URL+"/api/v1/auth/logout", "", map[string]string{"refresh_token": tk.RefreshToken}, nil); code != http.StatusNoContent {
		t.Fatalf("logout status = %d", code)
	}
	if code := call(t, "POST", srv.URL+"/api/v1/auth/refresh", "", map[string]string{"refresh_token": tk.RefreshToken}, nil); code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want 401", code)
	}
}

func TestProtectedRoutesNeedValidToken(t *testing.T) {
	srv, _, _ := newTestEnv(t)
	for _, token := range []string{"", "garbage", "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0."} {
		if code := call(t, "GET", srv.URL+"/api/v1/me", token, nil, nil); code != http.StatusUnauthorized {
			t.Errorf("token %q: status = %d, want 401", token, code)
		}
	}
}

func TestDeleteAccountTransfersSharedLists(t *testing.T) {
	srv, mailer, pool := newTestEnv(t)
	owner := login(t, srv.URL, mailer, uniqueEmail())
	member := login(t, srv.URL, mailer, uniqueEmail())

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
