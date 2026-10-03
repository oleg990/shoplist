package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type listResp struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	OwnerID     string `json:"owner_id"`
	Role        string `json:"role"`
	MemberCount int    `json:"member_count"`
}

type inviteResp struct {
	Code    string `json:"code"`
	MaxUses int    `json:"max_uses"`
	Uses    int    `json:"uses"`
}

func mustCreateList(t *testing.T, srv string, owner tokens, title string) listResp {
	t.Helper()
	var l listResp
	if code := call(t, "POST", srv+"/api/v1/lists", owner.AccessToken, map[string]string{"title": title}, &l); code != http.StatusCreated {
		t.Fatalf("create list status = %d", code)
	}
	return l
}

func mustInvite(t *testing.T, srv string, who tokens, listID string, body any) inviteResp {
	t.Helper()
	var inv inviteResp
	if code := call(t, "POST", srv+"/api/v1/lists/"+listID+"/invites", who.AccessToken, body, &inv); code != http.StatusCreated {
		t.Fatalf("create invite status = %d", code)
	}
	return inv
}

func TestCreateAndListLists(t *testing.T) {
	srv, _ := newTestEnv(t)
	owner := login(t, srv.URL, uniqueName())
	other := login(t, srv.URL, uniqueName())

	l := mustCreateList(t, srv.URL, owner, "  Продукты  ")
	if l.Title != "Продукты" || l.Role != "owner" || l.MemberCount != 1 || l.OwnerID != owner.User.ID {
		t.Fatalf("unexpected list: %+v", l)
	}

	var mine struct{ Items []listResp }
	if code := call(t, "GET", srv.URL+"/api/v1/lists", owner.AccessToken, nil, &mine); code != http.StatusOK || len(mine.Items) != 1 || mine.Items[0].ID != l.ID {
		t.Fatalf("owner lists: status=%d %+v", code, mine)
	}

	// Чужой список не виден: ни в перечне, ни по id (404, а не 403).
	var theirs struct{ Items []listResp }
	if code := call(t, "GET", srv.URL+"/api/v1/lists", other.AccessToken, nil, &theirs); code != http.StatusOK || len(theirs.Items) != 0 {
		t.Fatalf("other lists: status=%d %+v", code, theirs)
	}
	if code := call(t, "GET", srv.URL+"/api/v1/lists/"+l.ID, other.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Fatalf("foreign list status = %d, want 404", code)
	}
	if code := call(t, "GET", srv.URL+"/api/v1/lists/not-a-uuid", owner.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Fatalf("bad id status = %d, want 404", code)
	}
}

func TestListTitleValidationAndAuth(t *testing.T) {
	srv, _ := newTestEnv(t)
	owner := login(t, srv.URL, uniqueName())
	tooLong := strings.Repeat("я", 101)
	for _, title := range []string{"", "   ", tooLong} {
		if code := call(t, "POST", srv.URL+"/api/v1/lists", owner.AccessToken, map[string]string{"title": title}, nil); code != http.StatusBadRequest {
			t.Errorf("title len %d: status = %d, want 400", len([]rune(title)), code)
		}
	}
	if code := call(t, "POST", srv.URL+"/api/v1/lists", owner.AccessToken, map[string]string{"title": strings.Repeat("я", 100)}, nil); code != http.StatusCreated {
		t.Errorf("100-char title status = %d, want 201", code)
	}
	if code := call(t, "POST", srv.URL+"/api/v1/lists", "", map[string]string{"title": "x"}, nil); code != http.StatusUnauthorized {
		t.Errorf("no token status = %d, want 401", code)
	}
}

func TestInviteJoinAndMembers(t *testing.T) {
	srv, _ := newTestEnv(t)
	owner := login(t, srv.URL, uniqueName())
	guest := login(t, srv.URL, uniqueName())
	l := mustCreateList(t, srv.URL, owner, "Дача")

	inv := mustInvite(t, srv.URL, owner, l.ID, nil) // тело необязательно
	if len(inv.Code) != 8 || inv.MaxUses != 10 {
		t.Fatalf("unexpected invite: %+v", inv)
	}

	// Код принимается в любом регистре, после этого список виден гостю как editor.
	var joined listResp
	if code := call(t, "POST", srv.URL+"/api/v1/invites/accept", guest.AccessToken, map[string]string{"code": " " + lower(inv.Code) + " "}, &joined); code != http.StatusOK {
		t.Fatalf("accept status = %d", code)
	}
	if joined.ID != l.ID || joined.Role != "editor" || joined.MemberCount != 2 {
		t.Fatalf("joined list: %+v", joined)
	}

	var members struct {
		Items []struct {
			UserID string `json:"user_id"`
			Name   string `json:"name"`
			Role   string `json:"role"`
		}
	}
	if code := call(t, "GET", srv.URL+"/api/v1/lists/"+l.ID+"/members", guest.AccessToken, nil, &members); code != http.StatusOK || len(members.Items) != 2 {
		t.Fatalf("members: status=%d %+v", code, members)
	}
	for _, m := range members.Items {
		if m.Name != "Тест" {
			t.Errorf("member name = %q, want %q", m.Name, "Тест")
		}
	}

	// Повторное принятие тем же пользователем не тратит приглашение.
	call(t, "POST", srv.URL+"/api/v1/invites/accept", guest.AccessToken, map[string]string{"code": inv.Code}, nil)
	var again listResp
	if code := call(t, "POST", srv.URL+"/api/v1/invites/accept", guest.AccessToken, map[string]string{"code": inv.Code}, &again); code != http.StatusOK || again.MemberCount != 2 {
		t.Fatalf("repeat accept: status=%d %+v", code, again)
	}
}

func lower(s string) string { return strings.ToLower(s) }

func TestInviteMaxUsesHoldsUnderConcurrency(t *testing.T) {
	srv, _ := newTestEnv(t)
	owner := login(t, srv.URL, uniqueName())
	l := mustCreateList(t, srv.URL, owner, "Гонка")
	inv := mustInvite(t, srv.URL, owner, l.ID, map[string]int{"max_uses": 1})

	const guests = 6
	users := make([]tokens, guests)
	for i := range users {
		users[i] = login(t, srv.URL, uniqueName())
	}
	var (
		wg sync.WaitGroup
		mu sync.Mutex
		ok int
	)
	for _, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if call(t, "POST", srv.URL+"/api/v1/invites/accept", u.AccessToken, map[string]string{"code": inv.Code}, nil) == http.StatusOK {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("%d guests joined with a single-use invite, want exactly 1", ok)
	}
}

func TestInviteLimitsExpiryAndGarbage(t *testing.T) {
	srv, pool := newTestEnv(t)
	owner := login(t, srv.URL, uniqueName())
	a := login(t, srv.URL, uniqueName())
	b := login(t, srv.URL, uniqueName())
	l := mustCreateList(t, srv.URL, owner, "Лимиты")

	one := mustInvite(t, srv.URL, owner, l.ID, map[string]int{"max_uses": 1})
	if code := call(t, "POST", srv.URL+"/api/v1/invites/accept", a.AccessToken, map[string]string{"code": one.Code}, nil); code != http.StatusOK {
		t.Fatalf("first accept status = %d", code)
	}
	if code := call(t, "POST", srv.URL+"/api/v1/invites/accept", b.AccessToken, map[string]string{"code": one.Code}, nil); code != http.StatusNotFound {
		t.Fatalf("used-up invite status = %d, want 404", code)
	}

	expired := mustInvite(t, srv.URL, owner, l.ID, nil)
	if _, err := pool.Exec(context.Background(), `UPDATE invites SET expires_at = $1 WHERE code = $2`, time.Now().Add(-time.Minute), expired.Code); err != nil {
		t.Fatal(err)
	}
	if code := call(t, "POST", srv.URL+"/api/v1/invites/accept", b.AccessToken, map[string]string{"code": expired.Code}, nil); code != http.StatusNotFound {
		t.Fatalf("expired invite status = %d, want 404", code)
	}
	for _, code := range []string{"", "ZZZZZZZZ"} {
		if st := call(t, "POST", srv.URL+"/api/v1/invites/accept", b.AccessToken, map[string]string{"code": code}, nil); st != http.StatusNotFound {
			t.Errorf("garbage code %q status = %d, want 404", code, st)
		}
	}
}

func TestOnlyOwnerRenamesAndDeletes(t *testing.T) {
	srv, _ := newTestEnv(t)
	owner := login(t, srv.URL, uniqueName())
	guest := login(t, srv.URL, uniqueName())
	l := mustCreateList(t, srv.URL, owner, "Старое")
	inv := mustInvite(t, srv.URL, owner, l.ID, nil)
	call(t, "POST", srv.URL+"/api/v1/invites/accept", guest.AccessToken, map[string]string{"code": inv.Code}, nil)

	if code := call(t, "PATCH", srv.URL+"/api/v1/lists/"+l.ID, guest.AccessToken, map[string]string{"title": "Взлом"}, nil); code != http.StatusForbidden {
		t.Fatalf("editor rename status = %d, want 403", code)
	}
	if code := call(t, "DELETE", srv.URL+"/api/v1/lists/"+l.ID, guest.AccessToken, nil, nil); code != http.StatusForbidden {
		t.Fatalf("editor delete status = %d, want 403", code)
	}

	var renamed listResp
	if code := call(t, "PATCH", srv.URL+"/api/v1/lists/"+l.ID, owner.AccessToken, map[string]string{"title": "Новое"}, &renamed); code != http.StatusOK || renamed.Title != "Новое" {
		t.Fatalf("owner rename: status=%d %+v", code, renamed)
	}

	// Участник может создать приглашение, посторонний нет.
	stranger := login(t, srv.URL, uniqueName())
	if code := call(t, "POST", srv.URL+"/api/v1/lists/"+l.ID+"/invites", stranger.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Fatalf("stranger invite status = %d, want 404", code)
	}
	mustInvite(t, srv.URL, guest, l.ID, nil)

	if code := call(t, "DELETE", srv.URL+"/api/v1/lists/"+l.ID, owner.AccessToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("owner delete status = %d", code)
	}
	for _, tk := range []tokens{owner, guest} {
		if code := call(t, "GET", srv.URL+"/api/v1/lists/"+l.ID, tk.AccessToken, nil, nil); code != http.StatusNotFound {
			t.Fatalf("deleted list status = %d, want 404", code)
		}
	}
	// Удалённый список не работает и как приглашение.
	if code := call(t, "POST", srv.URL+"/api/v1/invites/accept", stranger.AccessToken, map[string]string{"code": inv.Code}, nil); code != http.StatusNotFound {
		t.Fatalf("invite to deleted list status = %d, want 404", code)
	}
}

func TestLeaveAndRemoveMembers(t *testing.T) {
	srv, _ := newTestEnv(t)
	owner := login(t, srv.URL, uniqueName())
	a := login(t, srv.URL, uniqueName())
	b := login(t, srv.URL, uniqueName())
	l := mustCreateList(t, srv.URL, owner, "Семья")
	inv := mustInvite(t, srv.URL, owner, l.ID, nil)
	for _, tk := range []tokens{a, b} {
		call(t, "POST", srv.URL+"/api/v1/invites/accept", tk.AccessToken, map[string]string{"code": inv.Code}, nil)
	}
	base := srv.URL + "/api/v1/lists/" + l.ID + "/members/"

	// Участник не может выгнать другого, но может выйти сам.
	if code := call(t, "DELETE", base+b.User.ID, a.AccessToken, nil, nil); code != http.StatusForbidden {
		t.Fatalf("editor removes other status = %d, want 403", code)
	}
	if code := call(t, "DELETE", base+a.User.ID, a.AccessToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("leave status = %d", code)
	}
	if code := call(t, "GET", srv.URL+"/api/v1/lists/"+l.ID, a.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Fatalf("list after leaving status = %d, want 404", code)
	}

	// Владелец может убрать участника, но не себя.
	if code := call(t, "DELETE", base+b.User.ID, owner.AccessToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("owner removes status = %d", code)
	}
	if code := call(t, "DELETE", base+owner.User.ID, owner.AccessToken, nil, nil); code != http.StatusConflict {
		t.Fatalf("owner leaves status = %d, want 409", code)
	}
	// Выгнанный может вернуться по действующему приглашению, а удалять несуществующего участника нельзя.
	if code := call(t, "DELETE", base+b.User.ID, owner.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Fatalf("remove absent member status = %d, want 404", code)
	}
}
