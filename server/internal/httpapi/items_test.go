package httpapi_test

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type itemResp struct {
	ID            string   `json:"id"`
	ListID        string   `json:"list_id"`
	CatalogItemID *int     `json:"catalog_item_id"`
	Name          string   `json:"name"`
	Quantity      *float64 `json:"quantity"`
	Unit          *string  `json:"unit"`
	Price         *float64 `json:"price"`
	CategoryID    *int     `json:"category_id"`
	IsBought      bool     `json:"is_bought"`
	BoughtBy      *string  `json:"bought_by"`
	Position      int      `json:"position"`
	Version       int64    `json:"version"`
	Deleted       bool     `json:"deleted"`
}

type itemsResp struct {
	Items  []itemResp `json:"items"`
	Cursor int64      `json:"cursor"`
}

// listEnv: список с владельцем и одним участником (editor).
type listEnv struct {
	srv          string
	owner, guest tokens
	listID       string
	pool         *pgxpool.Pool
	expo         *fakeExpo
}

func newListEnv(t *testing.T) listEnv {
	t.Helper()
	env := buildEnv(t, 0)
	e := listEnv{srv: env.srv.URL, pool: env.pool, expo: env.expo}
	e.owner = login(t, e.srv, uniqueName())
	e.guest = login(t, e.srv, uniqueName())
	l := mustCreateList(t, e.srv, e.owner, "Продукты")
	e.listID = l.ID
	inv := mustInvite(t, e.srv, e.owner, l.ID, nil)
	call(t, "POST", e.srv+"/api/v1/invites/accept", e.guest.AccessToken, map[string]string{"code": inv.Code}, nil)
	return e
}

func (e listEnv) itemURL(itemID string) string {
	return e.srv + "/api/v1/lists/" + e.listID + "/items/" + itemID
}

func (e listEnv) put(t *testing.T, who tokens, body map[string]any) (itemResp, string) {
	t.Helper()
	id := uuid.NewString()
	var it itemResp
	if code := call(t, "PUT", e.itemURL(id), who.AccessToken, body, &it); code != http.StatusOK {
		t.Fatalf("put item status = %d", code)
	}
	return it, id
}

func (e listEnv) items(t *testing.T, who tokens, since string) itemsResp {
	t.Helper()
	url := e.srv + "/api/v1/lists/" + e.listID + "/items"
	if since != "" {
		url += "?since=" + since
	}
	var r itemsResp
	if code := call(t, "GET", url, who.AccessToken, nil, &r); code != http.StatusOK {
		t.Fatalf("get items status = %d", code)
	}
	return r
}

func TestItemLifecycleAndSharing(t *testing.T) {
	e := newListEnv(t)
	it, id := e.put(t, e.owner, map[string]any{
		"name": "  Молоко 3.2%  ", "quantity": 2, "unit": "l", "price": 89.9, "category_id": 4, "position": 3,
	})
	if it.ID != id || it.Name != "Молоко 3.2%" || it.Quantity == nil || *it.Quantity != 2 ||
		it.Unit == nil || *it.Unit != "l" || it.Price == nil || *it.Price != 89.9 || it.IsBought || it.Version < 1 {
		t.Fatalf("unexpected item: %+v", it)
	}

	// Участник видит позицию и отмечает «куплено»: в bought_by он сам.
	if got := e.items(t, e.guest, ""); len(got.Items) != 1 || got.Items[0].ID != id {
		t.Fatalf("guest items: %+v", got)
	}
	var bought itemResp
	if code := call(t, "PATCH", e.itemURL(id), e.guest.AccessToken, map[string]any{"is_bought": true}, &bought); code != http.StatusOK {
		t.Fatalf("patch status = %d", code)
	}
	if !bought.IsBought || bought.BoughtBy == nil || *bought.BoughtBy != e.guest.User.ID || bought.Version <= it.Version {
		t.Fatalf("bought item: %+v", bought)
	}
	if bought.Name != "Молоко 3.2%" || bought.Price == nil || *bought.Price != 89.9 || bought.Position != 3 {
		t.Fatalf("patch changed other fields: %+v", bought)
	}

	// Повторная отметка другим человеком не перетирает, кто купил. Снятие флажка очищает.
	var again, undone itemResp
	call(t, "PATCH", e.itemURL(id), e.owner.AccessToken, map[string]any{"is_bought": true}, &again)
	if again.BoughtBy == nil || *again.BoughtBy != e.guest.User.ID {
		t.Fatalf("bought_by overwritten: %+v", again)
	}
	call(t, "PATCH", e.itemURL(id), e.owner.AccessToken, map[string]any{"is_bought": false}, &undone)
	if undone.IsBought || undone.BoughtBy != nil {
		t.Fatalf("unbought item: %+v", undone)
	}
}

func TestPatchClearsWithNullAndKeepsAbsentFields(t *testing.T) {
	e := newListEnv(t)
	_, id := e.put(t, e.owner, map[string]any{"name": "Сыр", "quantity": 0.5, "unit": "kg", "price": 300})

	var p itemResp
	if code := call(t, "PATCH", e.itemURL(id), e.owner.AccessToken, map[string]any{"price": nil, "name": "Сыр твёрдый"}, &p); code != http.StatusOK {
		t.Fatalf("patch status = %d", code)
	}
	if p.Price != nil || p.Name != "Сыр твёрдый" || p.Quantity == nil || *p.Quantity != 0.5 || p.Unit == nil || *p.Unit != "kg" {
		t.Fatalf("patch result: %+v", p)
	}
	for _, body := range []map[string]any{
		{"name": nil}, {"is_bought": nil}, {"name": ""}, {"quantity": -1}, {"unit": "bushel"}, {"category_id": 99999}, {"position": -1},
	} {
		if code := call(t, "PATCH", e.itemURL(id), e.owner.AccessToken, body, nil); code != http.StatusBadRequest {
			t.Errorf("patch %v: status = %d, want 400", body, code)
		}
	}
	if code := call(t, "PATCH", e.itemURL(uuid.NewString()), e.owner.AccessToken, map[string]any{"name": "x"}, nil); code != http.StatusNotFound {
		t.Errorf("patch missing item status = %d, want 404", code)
	}
}

func TestPutValidation(t *testing.T) {
	e := newListEnv(t)
	bad := []map[string]any{
		{"name": ""}, {"name": "   "}, {"name": strings.Repeat("я", 101)},
		{"name": "x", "quantity": 0}, {"name": "x", "quantity": -2}, {"name": "x", "quantity": 1e9},
		{"name": "x", "price": -1}, {"name": "x", "unit": "bushel"}, {"name": "x", "category_id": 99999},
		{"name": "x", "catalog_item_id": 99999}, {"name": "x", "position": -1},
	}
	for _, body := range bad {
		if code := call(t, "PUT", e.itemURL(uuid.NewString()), e.owner.AccessToken, body, nil); code != http.StatusBadRequest {
			t.Errorf("put %v: status = %d, want 400", body, code)
		}
	}
	if code := call(t, "PUT", e.itemURL("not-a-uuid"), e.owner.AccessToken, map[string]any{"name": "x"}, nil); code != http.StatusNotFound {
		t.Errorf("bad item id status = %d, want 404", code)
	}
	// Достаточно одного названия.
	e.put(t, e.owner, map[string]any{"name": "Хлеб"})
}

func TestPutIsIdempotentAndReplaces(t *testing.T) {
	e := newListEnv(t)
	id := uuid.NewString()
	var first, second itemResp
	call(t, "PUT", e.itemURL(id), e.owner.AccessToken, map[string]any{"name": "Яйца", "quantity": 10, "unit": "pcs"}, &first)
	call(t, "PUT", e.itemURL(id), e.owner.AccessToken, map[string]any{"name": "Яйца C1", "quantity": 20}, &second)

	if second.ID != first.ID || second.Name != "Яйца C1" || second.Version <= first.Version {
		t.Fatalf("replace result: %+v then %+v", first, second)
	}
	// PUT заменяет позицию целиком: unit, которого нет в новом теле, очищен.
	if second.Unit != nil || second.Quantity == nil || *second.Quantity != 20 {
		t.Fatalf("PUT must replace all fields: %+v", second)
	}
	if got := e.items(t, e.owner, ""); len(got.Items) != 1 {
		t.Fatalf("expected one item, got %d", len(got.Items))
	}
}

func TestStrangerAndForeignItemID(t *testing.T) {
	e := newListEnv(t)
	stranger := login(t, e.srv, uniqueName())
	_, id := e.put(t, e.owner, map[string]any{"name": "Чай"})

	// Посторонний ничего не видит и не меняет: везде 404.
	if code := call(t, "GET", e.srv+"/api/v1/lists/"+e.listID+"/items", stranger.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Errorf("stranger list items status = %d, want 404", code)
	}
	if code := call(t, "PUT", e.itemURL(uuid.NewString()), stranger.AccessToken, map[string]any{"name": "x"}, nil); code != http.StatusNotFound {
		t.Errorf("stranger put status = %d, want 404", code)
	}
	if code := call(t, "PATCH", e.itemURL(id), stranger.AccessToken, map[string]any{"name": "x"}, nil); code != http.StatusNotFound {
		t.Errorf("stranger patch status = %d, want 404", code)
	}
	if code := call(t, "DELETE", e.itemURL(id), stranger.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Errorf("stranger delete status = %d, want 404", code)
	}
	if code := call(t, "POST", e.srv+"/api/v1/lists/"+e.listID+"/items/clear-bought", stranger.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Errorf("stranger clear status = %d, want 404", code)
	}

	// Свой список, но чужой id позиции: перехватить её нельзя.
	other := mustCreateList(t, e.srv, stranger, "Чужой")
	url := e.srv + "/api/v1/lists/" + other.ID + "/items/" + id
	if code := call(t, "PUT", url, stranger.AccessToken, map[string]any{"name": "Угон"}, nil); code != http.StatusConflict {
		t.Errorf("hijack put status = %d, want 409", code)
	}
	if got := e.items(t, e.owner, ""); len(got.Items) != 1 || got.Items[0].Name != "Чай" {
		t.Fatalf("item was changed: %+v", got)
	}
}

func TestDeleteAndSinceCursor(t *testing.T) {
	e := newListEnv(t)
	_, a := e.put(t, e.owner, map[string]any{"name": "A"})
	_, b := e.put(t, e.owner, map[string]any{"name": "B"})

	full := e.items(t, e.guest, "")
	if len(full.Items) != 2 || full.Cursor == 0 {
		t.Fatalf("full load: %+v", full)
	}

	// После курсора приходят только изменения: новая позиция и удаление A (с пометкой deleted).
	_, c := e.put(t, e.owner, map[string]any{"name": "C"})
	if code := call(t, "DELETE", e.itemURL(a), e.guest.AccessToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete status = %d", code)
	}
	delta := e.items(t, e.guest, itoa(full.Cursor))
	got := map[string]itemResp{}
	for _, it := range delta.Items {
		got[it.ID] = it
	}
	if len(got) != 2 || !got[a].Deleted || got[c].Deleted || got[c].Name != "C" {
		t.Fatalf("delta: %+v", delta)
	}
	if _, ok := got[b]; ok {
		t.Fatalf("unchanged item must not be in delta: %+v", delta)
	}
	if delta.Cursor <= full.Cursor {
		t.Fatalf("cursor did not advance: %d -> %d", full.Cursor, delta.Cursor)
	}
	// Без since удалённых не видно, а с актуальным курсором изменений нет.
	if cur := e.items(t, e.guest, ""); len(cur.Items) != 2 {
		t.Fatalf("current items: %+v", cur)
	}
	if none := e.items(t, e.guest, itoa(delta.Cursor)); len(none.Items) != 0 || none.Cursor != delta.Cursor {
		t.Fatalf("expected no changes: %+v", none)
	}

	// Удалённую позицию нельзя ни удалить, ни воскресить повторным PUT.
	if code := call(t, "DELETE", e.itemURL(a), e.owner.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", code)
	}
	if code := call(t, "PUT", e.itemURL(a), e.owner.AccessToken, map[string]any{"name": "A again"}, nil); code != http.StatusConflict {
		t.Errorf("put deleted status = %d, want 409", code)
	}
	if code := call(t, "GET", e.srv+"/api/v1/lists/"+e.listID+"/items?since=-1", e.owner.AccessToken, nil, nil); code != http.StatusBadRequest {
		t.Errorf("bad since status = %d, want 400", code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestClearBoughtArchivesAndHidesItems(t *testing.T) {
	e := newListEnv(t)
	_, milk := e.put(t, e.owner, map[string]any{"name": "Молоко", "quantity": 1, "unit": "l", "price": 90})
	_, bread := e.put(t, e.owner, map[string]any{"name": "Хлеб", "is_bought": true})
	_, tea := e.put(t, e.owner, map[string]any{"name": "Чай"})
	call(t, "PATCH", e.itemURL(milk), e.guest.AccessToken, map[string]any{"is_bought": true}, nil)
	before := e.items(t, e.owner, "")

	var res struct{ Cleared int }
	if code := call(t, "POST", e.srv+"/api/v1/lists/"+e.listID+"/items/clear-bought", e.guest.AccessToken, nil, &res); code != http.StatusOK || res.Cleared != 2 {
		t.Fatalf("clear: status=%d cleared=%d", code, res.Cleared)
	}
	left := e.items(t, e.owner, "")
	if len(left.Items) != 1 || left.Items[0].ID != tea {
		t.Fatalf("left after clear: %+v", left)
	}
	// Другие клиенты узнают об очистке через since: обе позиции помечены deleted.
	delta := e.items(t, e.owner, itoa(before.Cursor))
	del := map[string]bool{}
	for _, it := range delta.Items {
		del[it.ID] = it.Deleted
	}
	if !del[milk] || !del[bread] || len(del) != 2 {
		t.Fatalf("delta after clear: %+v", delta)
	}

	var names []string
	rows, err := e.pool.Query(context.Background(), `SELECT item_name FROM purchase_history WHERE list_id = $1`, e.listID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "Молоко,Хлеб" {
		t.Fatalf("purchase history = %v", names)
	}

	// Повторная очистка ничего не находит.
	if code := call(t, "POST", e.srv+"/api/v1/lists/"+e.listID+"/items/clear-bought", e.owner.AccessToken, nil, &res); code != http.StatusOK || res.Cleared != 0 {
		t.Fatalf("second clear: status=%d cleared=%d", code, res.Cleared)
	}
}

func TestConcurrentWritesGetDistinctOrderedVersions(t *testing.T) {
	e := newListEnv(t)
	const n = 20
	var wg sync.WaitGroup
	versions := make([]int64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var it itemResp
			who := e.owner
			if i%2 == 1 {
				who = e.guest
			}
			call(t, "PUT", e.itemURL(uuid.NewString()), who.AccessToken, map[string]any{"name": "Позиция"}, &it)
			versions[i] = it.Version
		}()
	}
	wg.Wait()
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for i, v := range versions {
		if v != int64(i+1) {
			t.Fatalf("versions must be 1..%d without gaps or repeats, got %v", n, versions)
		}
	}
	if got := e.items(t, e.owner, ""); len(got.Items) != n || got.Cursor != n {
		t.Fatalf("items = %d cursor = %d", len(got.Items), got.Cursor)
	}
}

func TestDeletedListHidesItems(t *testing.T) {
	e := newListEnv(t)
	e.put(t, e.owner, map[string]any{"name": "Чай"})
	call(t, "DELETE", e.srv+"/api/v1/lists/"+e.listID, e.owner.AccessToken, nil, nil)
	if code := call(t, "GET", e.srv+"/api/v1/lists/"+e.listID+"/items", e.owner.AccessToken, nil, nil); code != http.StatusNotFound {
		t.Fatalf("items of deleted list status = %d, want 404", code)
	}
}
