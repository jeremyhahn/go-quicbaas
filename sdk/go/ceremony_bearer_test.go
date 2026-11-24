package quicbaas

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestCeremonyStatus_REST_SendsPlatformBearer pins that over REST,
// Security().Ceremony() carries the bearer set by WithAuthToken on every
// ceremony request, as Security().Barrier() does. Without it an operational
// node's default-deny gate answers 401 "platform bearer authorization is
// missing" to ceremony status.
func TestCeremonyStatus_REST_SendsPlatformBearer(t *testing.T) {
	var (
		mu   sync.Mutex
		auth string
		seen bool
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/ceremony/status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = r.Header.Get("Authorization")
		seen = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"state":"complete","spki_pin":"pin-1700"}`)); err != nil {
			t.Errorf("write ceremony status body: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithREST(), WithAuthToken("t-1700"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ceremony, err := c.Security().Ceremony()
	if err != nil {
		t.Fatalf("Security().Ceremony(): %v", err)
	}
	status, err := ceremony.CeremonyStatus(context.Background())
	if err != nil {
		t.Fatalf("CeremonyStatus: %v", err)
	}
	if status.State != "complete" {
		t.Errorf("state: got %q, want %q", status.State, "complete")
	}

	mu.Lock()
	defer mu.Unlock()
	if !seen {
		t.Fatal("ceremony status never reached the server")
	}
	if got, want := auth, "Bearer t-1700"; got != want {
		t.Errorf("Authorization on /v1/ceremony/status: got %q, want %q", got, want)
	}
}
