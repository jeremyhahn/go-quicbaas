package quicbaas

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	qrdbtimeseries "github.com/jeremyhahn/go-qrdb/pkg/timeseries"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// tsUnscopedRefusal is the stub's typed refusal of a contextless method, as
// the broker's scoped time-series proxy refuses them.
type tsUnscopedRefusal struct{ method string }

func (e *tsUnscopedRefusal) Error() string {
	return "timeseries: " + e.method + " carries no context"
}

// scopedTSStub is a time-series servicer shaped like the broker's scoped
// proxy: the contextless reads refuse, the context forms serve.
type scopedTSStub struct {
	qrdbtimeseries.TimeSeriesServicer

	mu     sync.Mutex
	policy *qrdbtimeseries.RetentionPolicy
}

func (s *scopedTSStub) SetRetentionPolicy(_ context.Context, _ string, p *qrdbtimeseries.RetentionPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = p
	return nil
}

func (s *scopedTSStub) GetRetentionPolicy(string) (*qrdbtimeseries.RetentionPolicy, error) {
	return nil, &tsUnscopedRefusal{method: "GetRetentionPolicy"}
}

func (s *scopedTSStub) ListMetrics(string, int) ([]string, error) {
	return nil, &tsUnscopedRefusal{method: "ListMetrics"}
}

func (s *scopedTSStub) GetRetentionPolicyContext(context.Context, string) (*qrdbtimeseries.RetentionPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy, nil
}

func (s *scopedTSStub) ListMetricsContext(context.Context, string, int) ([]string, error) {
	return []string{"m"}, nil
}

// TestSearchQRDBTimeSeries_EmbeddedRetentionResolvesScope pins
// go-quicbaas#2500: an embedded client whose time-series servicer resolves
// scope from the call's context (the broker's proxy) reads retention and
// lists metrics through the context forms, never the contextless methods
// the proxy must refuse.
func TestSearchQRDBTimeSeries_EmbeddedRetentionResolvesScope(t *testing.T) {
	stub := &scopedTSStub{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: stubKVService{}, Admin: stubAdminService{}, TimeSeries: stub})
	t.Cleanup(func() { _ = c.Close() })

	ts, err := c.Search().QRDBTimeSeries()
	if err != nil {
		t.Fatalf("Search().QRDBTimeSeries(): %v", err)
	}
	ctx := context.Background()
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

	metrics, err := ts.TSListMetrics(ctx, &qrdbtransport.TSListMetricsRequest{})
	if err != nil {
		t.Fatalf("TSListMetrics: %v", err)
	}
	if !reflect.DeepEqual(metrics.Metrics, []string{"m"}) {
		t.Errorf("TSListMetrics: got %v, want [m]", metrics.Metrics)
	}
}

// closeTrackingTSStub is a scopedTSStub that also counts the writes and
// series deletions that reach the servicer, so a test can tell whether a
// call went through.
type closeTrackingTSStub struct {
	scopedTSStub

	mu      sync.Mutex
	writes  int
	deletes int
}

func (s *closeTrackingTSStub) Write(_ context.Context, _ string, _ float64, _ time.Time, _ map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	return nil
}

func (s *closeTrackingTSStub) DeleteSeries(_ context.Context, _ *qrdbtimeseries.DeleteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes++
	return nil
}

// counts returns how many writes and deletions reached the stub.
func (s *closeTrackingTSStub) counts() (writes, deletes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes, s.deletes
}

// TestSearchQRDBTimeSeries_EmbeddedHandleRefusedAfterClose pins that Close
// stops an initialized embedded qrdb sub-client: a time-series handle taken
// before Close refuses every call afterwards, and none of them reaches the
// servicer.
func TestSearchQRDBTimeSeries_EmbeddedHandleRefusedAfterClose(t *testing.T) {
	stub := &closeTrackingTSStub{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: stubKVService{}, Admin: stubAdminService{}, TimeSeries: stub})

	ts, err := c.Search().QRDBTimeSeries()
	if err != nil {
		t.Fatalf("Search().QRDBTimeSeries(): %v", err)
	}
	ctx := context.Background()
	if err := ts.TSWrite(ctx, &qrdbtransport.TSDataPoint{Metric: "m", Value: 1, TimestampNs: time.Now().UnixNano()}); err != nil {
		t.Fatalf("TSWrite before Close: %v", err)
	}
	if writes, _ := stub.counts(); writes != 1 {
		t.Fatalf("writes before Close: got %d, want 1", writes)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	calls := []struct {
		name string
		call func() error
	}{
		{"TSWrite", func() error {
			return ts.TSWrite(ctx, &qrdbtransport.TSDataPoint{Metric: "m", Value: 2, TimestampNs: time.Now().UnixNano()})
		}},
		{"TSDeleteSeries", func() error {
			_, err := ts.TSDeleteSeries(ctx, &qrdbtransport.TSDeleteRequest{Metric: "m"})
			return err
		}},
		{"TSSetRetention", func() error {
			return ts.TSSetRetention(ctx, &qrdbtransport.TSSetRetentionRequest{
				Metric: "m", Policy: &qrdbtransport.TSRetentionPolicy{DurationNs: int64(time.Hour)},
			})
		}},
		{"TSGetRetention", func() error {
			_, err := ts.TSGetRetention(ctx, &qrdbtransport.TSGetRetentionRequest{Metric: "m"})
			return err
		}},
		{"TSListMetrics", func() error {
			_, err := ts.TSListMetrics(ctx, &qrdbtransport.TSListMetricsRequest{})
			return err
		}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.As(err, new(*qrdbtransport.ClosedError)) {
				t.Errorf("%s after Close: got %v, want a *transport.ClosedError", tc.name, err)
			}
		})
	}

	writes, deletes := stub.counts()
	if writes != 1 || deletes != 0 {
		t.Errorf("servicer calls after Close: got writes=%d deletes=%d, want writes=1 deletes=0", writes, deletes)
	}
	stub.scopedTSStub.mu.Lock()
	policy := stub.scopedTSStub.policy
	stub.scopedTSStub.mu.Unlock()
	if policy != nil {
		t.Errorf("retention policy after Close: got %+v, want none set", policy)
	}
}

// TestEmbeddedQRDBAccessorsRefusedAfterClose pins that the qrdb accessors
// refuse after Close, whether or not the embedded sub-client was built
// before it.
func TestEmbeddedQRDBAccessorsRefusedAfterClose(t *testing.T) {
	newClient := func(t *testing.T) *client {
		t.Helper()
		return buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{KV: stubKVService{}, Admin: stubAdminService{}, TimeSeries: &scopedTSStub{}})
	}
	assertRefused := func(t *testing.T, c *client) {
		t.Helper()
		ts, err := c.Search().QRDBTimeSeries()
		if err == nil {
			t.Error("Search().QRDBTimeSeries() after Close: got no error")
		}
		if ts != nil {
			t.Error("Search().QRDBTimeSeries() after Close: got a non-nil client")
		}
		ds, err := c.DataStore()
		if err == nil {
			t.Error("DataStore() after Close: got no error")
		}
		if ds != nil {
			t.Error("DataStore() after Close: got a non-nil client")
		}
	}

	t.Run("initialized before close", func(t *testing.T) {
		c := newClient(t)
		if _, err := c.Search().QRDBTimeSeries(); err != nil {
			t.Fatalf("Search().QRDBTimeSeries() before Close: %v", err)
		}
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		assertRefused(t, c)
	})

	t.Run("closed before any accessor", func(t *testing.T) {
		c := newClient(t)
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		assertRefused(t, c)
	})
}
