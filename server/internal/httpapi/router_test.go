package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"shoplist/server/internal/db"
	"shoplist/server/internal/httpapi"
	"shoplist/server/internal/store"
)

// Интеграционный тест: нужен Postgres, адрес в TEST_DATABASE_URL.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpapi.NewRouter(pool, store.New(pool), log))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string, out any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
	if code := get(t, srv.URL+"/healthz", nil); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
}

func TestSearchCatalog(t *testing.T) {
	srv := newTestServer(t)

	var res struct {
		Items []struct {
			Name         string  `json:"name"`
			CategoryName *string `json:"category_name"`
		} `json:"items"`
	}
	// «малоко» с опечаткой всё равно находит «Молоко 3.2% (1 л)».
	for _, q := range []string{"молоко", "малоко"} {
		res.Items = nil
		if code := get(t, srv.URL+"/api/v1/catalog/search?q="+q, &res); code != http.StatusOK {
			t.Fatalf("q=%s status = %d", q, code)
		}
		if len(res.Items) == 0 || res.Items[0].Name != "Молоко 3.2% (1 л)" {
			t.Fatalf("q=%s: unexpected result %+v", q, res.Items)
		}
		if res.Items[0].CategoryName == nil || *res.Items[0].CategoryName != "Молочные продукты" {
			t.Fatalf("q=%s: category = %v", q, res.Items[0].CategoryName)
		}
	}
}

func TestSearchCatalogValidation(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/api/v1/catalog/search", "/api/v1/catalog/search?q=a&limit=0", "/api/v1/catalog/search?q=a&limit=abc"} {
		if code := get(t, srv.URL+path, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", path, code)
		}
	}
}

func TestListCategories(t *testing.T) {
	srv := newTestServer(t)
	var res struct {
		Items []struct{ Name string } `json:"items"`
	}
	if code := get(t, srv.URL+"/api/v1/categories", &res); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(res.Items) != 12 {
		t.Fatalf("categories = %d, want 12", len(res.Items))
	}
}
