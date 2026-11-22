package embedded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// queryMock records the query the embedded transport makes on the service and
// answers with fixed points.
type queryMock struct {
	*mockQuicBaaS
	got    *service.TSQueryRequest
	points []*service.TSDataPoint
	err    error
}

func (m *queryMock) TSQuery(_ context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
	m.got = req
	if m.err != nil {
		return nil, m.err
	}
	return &service.TSQueryResponse{Metric: req.Metric, Points: m.points}, nil
}

func newQueryClient(t *testing.T, m *queryMock) timeseries.Client {
	t.Helper()
	m.mockQuicBaaS = &mockQuicBaaS{}
	tr, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	return timeseries.NewClient(tr)
}

func TestTSQuery_SDKRequestReachesTheServiceAndIsTyped(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	m := &queryMock{points: []*service.TSDataPoint{
		{Metric: "cpu", Value: 1.5, Timestamp: t0},
		nil,
		{Metric: "cpu", Value: 2.5, Timestamp: t0.Add(time.Minute)},
	}}
	resp, err := newQueryClient(t, m).Query(context.Background(), &timeseries.QueryRequest{
		Metric:    "cpu",
		Tags:      map[string]string{"host": "a"},
		Start:     t0,
		End:       t0.Add(time.Hour),
		Aggregate: timeseries.AggregateAvg,
		Interval:  time.Minute,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	got := m.got
	if got == nil || got.Metric != "cpu" || got.Tags["host"] != "a" || !got.StartTime.Equal(t0) || !got.EndTime.Equal(t0.Add(time.Hour)) {
		t.Fatalf("service received %+v", got)
	}
	if got.Aggregation != timeseries.AggregateAvg.String() || got.IntervalSec != 60 {
		t.Errorf("service received aggregation %q interval %d", got.Aggregation, got.IntervalSec)
	}
	want := []timeseries.Point{{Timestamp: t0, Value: 1.5}, {Timestamp: t0.Add(time.Minute), Value: 2.5}}
	if len(resp.Series) != len(want) {
		t.Fatalf("Series = %+v, want %+v", resp.Series, want)
	}
	for i := range want {
		if !resp.Series[i].Timestamp.Equal(want[i].Timestamp) || resp.Series[i].Value != want[i].Value {
			t.Errorf("Series[%d] = %+v, want %+v", i, resp.Series[i], want[i])
		}
	}
}

// The service takes the aggregation interval in seconds: a fractional
// interval is refused before the service is asked, never truncated.
func TestTSQuery_FractionalIntervalIsRefused(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0).UTC()
	for _, iv := range []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond} {
		m := &queryMock{}
		_, err := newQueryClient(t, m).Query(context.Background(), &timeseries.QueryRequest{
			Metric:    "cpu",
			Start:     t0,
			End:       t0.Add(time.Hour),
			Aggregate: timeseries.AggregateAvg,
			Interval:  iv,
		})
		if !sdkerrors.IsInvalidInput(err) {
			t.Errorf("interval %v: error = %v, want InvalidInput", iv, err)
		}
		if m.got != nil {
			t.Errorf("interval %v: service was asked %+v", iv, m.got)
		}
	}
}

func TestTSQuery_RawQuerySendsNoAggregation(t *testing.T) {
	m := &queryMock{}
	t0 := time.Unix(1_700_000_000, 0)
	resp, err := newQueryClient(t, m).Query(context.Background(), &timeseries.QueryRequest{
		Metric: "cpu", Start: t0, End: t0.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if m.got.Aggregation != "" || m.got.IntervalSec != 0 {
		t.Errorf("raw query sent aggregation %q interval %d", m.got.Aggregation, m.got.IntervalSec)
	}
	if resp.Series == nil || len(resp.Series) != 0 {
		t.Errorf("no points must be an empty Series, got %#v", resp.Series)
	}
}

func TestTSQuery_ServiceErrorIsReturned(t *testing.T) {
	boom := errors.New("boom")
	t0 := time.Unix(1_700_000_000, 0)
	_, err := newQueryClient(t, &queryMock{err: boom}).Query(context.Background(), &timeseries.QueryRequest{
		Metric: "cpu", Start: t0, End: t0.Add(time.Hour),
	})
	if !errors.Is(err, boom) {
		t.Errorf("Query: want the service error, got %v", err)
	}
}

func TestTSQuery_NilRequestIsRefused(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []interface{}{(*timeseries.QueryRequest)(nil), (*service.TSQueryRequest)(nil)} {
		_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Query", req)
		var invalidReqErr *InvalidRequestTypeError
		if !errors.As(err, &invalidReqErr) {
			t.Errorf("%T: error = %v, want *InvalidRequestTypeError", req, err)
		}
	}
}
