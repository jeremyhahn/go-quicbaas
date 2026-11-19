package quicbaas

import (
	"context"
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
