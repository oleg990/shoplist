package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func fakeExpoServer(t *testing.T, batches *[]int, auth *string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msgs []Message
		_ = json.NewDecoder(r.Body).Decode(&msgs)
		*batches = append(*batches, len(msgs))
		*auth = r.Header.Get("Authorization")
		if status != 0 {
			http.Error(w, "boom", status)
			return
		}
		data := make([]map[string]any, len(msgs))
		for i, m := range msgs {
			if m.To == "dead" {
				data[i] = map[string]any{"status": "error", "message": "x", "details": map[string]string{"error": "DeviceNotRegistered"}}
			} else {
				data[i] = map[string]any{"status": "ok", "id": strconv.Itoa(i)}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExpoSenderSplitsIntoBatchesOf100(t *testing.T) {
	var batches []int
	var auth string
	srv := fakeExpoServer(t, &batches, &auth, 0)

	msgs := make([]Message, 250)
	for i := range msgs {
		msgs[i] = Message{To: "ExponentPushToken[x" + strconv.Itoa(i) + "]", Title: "t", Body: "b"}
	}
	msgs[120].To = "dead"

	got, err := ExpoSender{URL: srv.URL, AccessToken: "secret"}.Send(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 3 || batches[0] != 100 || batches[1] != 100 || batches[2] != 50 {
		t.Fatalf("batches = %v, want [100 100 50]", batches)
	}
	if len(got) != 250 || got[120].Error != "DeviceNotRegistered" || got[0].Status != "ok" || got[249].Status != "ok" {
		t.Fatalf("results misaligned: len=%d r120=%+v", len(got), got[120])
	}
	if auth != "Bearer secret" {
		t.Fatalf("Authorization = %q", auth)
	}
}

func TestExpoSenderReportsHTTPErrors(t *testing.T) {
	var batches []int
	var auth string
	srv := fakeExpoServer(t, &batches, &auth, http.StatusServiceUnavailable)
	if _, err := (ExpoSender{URL: srv.URL}).Send(context.Background(), []Message{{To: "a"}}); err == nil {
		t.Fatal("expected an error for HTTP 503")
	}
	if auth != "" {
		t.Fatalf("Authorization must be empty without a token, got %q", auth)
	}
}

func TestTokenValidation(t *testing.T) {
	ok := []string{"ExponentPushToken[abcdefghijklmnop]", "ExpoPushToken[abc_def-123456]"}
	bad := []string{"", "ExponentPushToken[]", "ExponentPushToken[short]", "ExponentPushToken[bad token!!]", "FcmToken[abcdefghij]", "ExponentPushToken[abcdefghij]x"}
	for _, s := range ok {
		if !tokenRE.MatchString(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range bad {
		if tokenRE.MatchString(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("абвгд", 10); got != "абвгд" {
		t.Fatalf("got %q", got)
	}
	if got := truncate("абвгдеёжзи", 5); got != "абвг…" {
		t.Fatalf("got %q", got)
	}
}
