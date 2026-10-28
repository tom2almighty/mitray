package mihomo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPermanentHTTPAndJSONFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		permanent bool
	}{
		{"unauthorized", http.StatusUnauthorized, "no", true},
		{"forbidden", http.StatusForbidden, "no", true},
		{"bad request", http.StatusBadRequest, "no", true},
		{"not found", http.StatusNotFound, "no", true},
		{"server error", http.StatusInternalServerError, "no", false},
		{"malformed JSON", http.StatusOK, "{", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			client := NewClient(Connection{Endpoint: srv.URL})
			defer client.Close()
			_, err := client.Status(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}
			if got := IsPermanent(err); got != tc.permanent {
				t.Fatalf("IsPermanent=%t, want %t: %v", got, tc.permanent, err)
			}
		})
	}
}
