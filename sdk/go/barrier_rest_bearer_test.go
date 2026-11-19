package quicbaas

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestBarrierStatus_REST_SendsPlatformBearer pins go-quicbaas#2538: over
// REST, Security().Barrier() must carry the bearer set by WithAuthToken on
// every barrier request, the same as every other verb of the REST transport.
// Without it the server's default-deny gate answers 401 "platform bearer
// authorization is missing".
func TestBarrierStatus_REST_SendsPlatformBearer(t *testing.T) {
	var (
		mu   sync.Mutex
		auth string
		seen bool
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/barrier/status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = r.Header.Get("Authorization")
		seen = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"sealed":false,"strategy":"passphrase"}`)); err != nil {
			t.Errorf("write barrier status body: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithREST(), WithAuthToken("t-2538"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	barrier, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Security().Barrier(): %v", err)
	}
	status, err := barrier.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}
	if status.Sealed {
		t.Errorf("sealed: got true, want false")
	}

	mu.Lock()
	defer mu.Unlock()
	if !seen {
		t.Fatal("barrier status never reached the server")
	}
	if got, want := auth, "Bearer t-2538"; got != want {
		t.Errorf("Authorization on /v1/barrier/status: got %q, want %q", got, want)
	}
}
