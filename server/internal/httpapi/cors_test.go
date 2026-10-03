package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	a := &API{origins: []string{"http://localhost:8081"}}
	h := a.cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	do := func(method, origin string, preflight bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/me", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if preflight {
			req.Header.Set("Access-Control-Request-Method", "PUT")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("OPTIONS", "http://localhost:8081", true); rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:8081" || rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatalf("preflight: %d %v", rec.Code, rec.Header())
	}
	if rec := do("GET", "http://localhost:8081", false); rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:8081" {
		t.Fatalf("simple request: %d %v", rec.Code, rec.Header())
	}
	if rec := do("GET", "http://evil.example", false); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("foreign origin must not be allowed")
	}
	if rec := do("GET", "", false); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no Origin, no CORS headers")
	}

	off := (&API{}).cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "http://localhost:8081")
	rec := httptest.NewRecorder()
	off.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS must be off by default")
	}
}
