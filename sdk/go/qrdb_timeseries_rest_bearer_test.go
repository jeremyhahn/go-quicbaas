package quicbaas

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// TestSearchQRDBTimeSeries_REST_SendsPlatformBearer pins go-quicbaas#2500:
// over REST, Search().QRDBTimeSeries() must carry the bearer set by
// WithAuthToken on every time-series request. go-qrdb's REST sub-client
// cannot add an Authorization header, so without the SDK's own adapter the
// server's default-deny gate answers 401.
func TestSearchQRDBTimeSeries_REST_SendsPlatformBearer(t *testing.T) {
	const want = "Bearer t-2500"
	var (
		mu   sync.Mutex
		seen []string
	)
	record := func(w http.ResponseWriter, r *http.Request, body string) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" "+auth)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if auth != want {
			w.WriteHeader(http.StatusUnauthorized)
			body = `{"error":"platform bearer authorization is missing"}`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write %s body: %v", r.URL.Path, err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/ts/write", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, `{}`)
	})
	mux.HandleFunc("/ts/series/m/retention", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			record(w, r, `{"metric":"m","policy":{"duration_ns":3600000000000}}`)
			return
		}
		record(w, r, `{}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithREST(), WithAuthToken("t-2500"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ts, err := c.Search().QRDBTimeSeries()
	if err != nil {
		t.Fatalf("Search().QRDBTimeSeries(): %v", err)
	}
	ctx := context.Background()
	if err := ts.TSWrite(ctx, &qrdbtransport.TSDataPoint{Metric: "m", Value: 1, TimestampNs: 1}); err != nil {
		t.Fatalf("TSWrite: %v", err)
	}
	if err := ts.TSSetRetention(ctx, &qrdbtransport.TSSetRetentionRequest{
		Metric: "m", Policy: &qrdbtransport.TSRetentionPolicy{DurationNs: int64(time.Hour)},
	}); err != nil {
		t.Fatalf("TSSetRetention: %v", err)
	}
	got, err := ts.TSGetRetention(ctx, &qrdbtransport.TSGetRetentionRequest{Metric: "m"})
	if err != nil {
		t.Fatalf("TSGetRetention: %v", err)
	}
	if got.Policy == nil || got.Policy.DurationNs != int64(time.Hour) {
		t.Errorf("TSGetRetention policy: got %+v, want a 1h duration", got.Policy)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("time-series requests: got %d (%v), want 3", len(seen), seen)
	}
	for _, s := range seen {
		if len(s) < len(want) || s[len(s)-len(want):] != want {
			t.Errorf("request %q did not carry %q", s, want)
		}
	}
}
