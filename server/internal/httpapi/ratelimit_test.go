package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterBurstThenRefill(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(1, 3)
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if ok, _ := l.allow("a"); !ok {
			t.Fatalf("request %d should pass within burst", i)
		}
	}
	ok, retry := l.allow("a")
	if ok || retry != 1 {
		t.Fatalf("4th request: ok=%v retry=%d, want blocked with retry 1", ok, retry)
	}
	if ok, _ := l.allow("b"); !ok {
		t.Fatal("another key must have its own bucket")
	}
	now = now.Add(2 * time.Second)
	if ok, _ := l.allow("a"); !ok {
		t.Fatal("tokens must refill over time")
	}
}

func TestLimiterSweepsIdleKeys(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLimiter(1, 2)
	l.now = func() time.Time { return now }
	l.allow("a")
	now = now.Add(5 * time.Minute)
	l.allow("b")
	if _, ok := l.buckets["a"]; ok {
		t.Fatal("idle key was not swept")
	}
}

func TestLimitMiddleware(t *testing.T) {
	h := limit(0.001, 2)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	do := func(addr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 2; i++ {
		if c := do("1.2.3.4:5555").Code; c != http.StatusNoContent {
			t.Fatalf("request %d: %d", i, c)
		}
	}
	rec := do("1.2.3.4:6666") // другой порт, тот же IP
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("got %d, Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if c := do("5.6.7.8:1").Code; c != http.StatusNoContent {
		t.Fatalf("other IP must not be limited: %d", c)
	}
}
