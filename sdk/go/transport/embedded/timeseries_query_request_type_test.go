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

func TestQuery_SDKRequestReachesTheService(t *testing.T) {
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	var got *service.TSQueryRequest
	tr, err := New(&mockQuicBaaS{tsQueryFunc: func(_ context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
		got = req
		return &service.TSQueryResponse{
			Metric: req.Metric,
			Points: []*service.TSDataPoint{
				{Metric: req.Metric, Timestamp: start, Value: 1.5},
				nil,
				{Metric: req.Metric, Timestamp: start.Add(5 * time.Minute), Value: -2.25},
			},
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := timeseries.NewClient(tr).Query(context.Background(), &timeseries.QueryRequest{
		Metric:    "cpu.usage",
		Tags:      map[string]string{"host": "web-1"},
		Start:     start,
		End:       end,
		Aggregate: timeseries.AggregateAvg,
		Interval:  5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if got == nil {
		t.Fatal("service was not called")
	}
	if got.Metric != "cpu.usage" || got.Tags["host"] != "web-1" || len(got.Tags) != 1 {
		t.Errorf("metric/tags = %q %v", got.Metric, got.Tags)
	}
	if !got.StartTime.Equal(start) || !got.EndTime.Equal(end) {
		t.Errorf("window = %v..%v, want %v..%v", got.StartTime, got.EndTime, start, end)
	}
	if got.Aggregation != "avg" {
		t.Errorf("Aggregation = %q, want avg", got.Aggregation)
	}
	if got.IntervalSec != 300 {
		t.Errorf("IntervalSec = %d, want 300", got.IntervalSec)
	}

	want := []timeseries.Point{
		{Timestamp: start, Value: 1.5},
		{Timestamp: start.Add(5 * time.Minute), Value: -2.25},
	}
	if len(resp.Series) != len(want) {
		t.Fatalf("Series = %+v, want %+v", resp.Series, want)
	}
	for i, p := range want {
		if !resp.Series[i].Timestamp.Equal(p.Timestamp) || resp.Series[i].Value != p.Value {
			t.Errorf("Series[%d] = %+v, want %+v", i, resp.Series[i], p)
		}
	}
}

func TestQuery_RawQuerySendsNoAggregation(t *testing.T) {
	var got *service.TSQueryRequest
	tr, err := New(&mockQuicBaaS{tsQueryFunc: func(_ context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
		got = req
		return &service.TSQueryResponse{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	resp, err := timeseries.NewClient(tr).Query(context.Background(), &timeseries.QueryRequest{
		Metric: "cpu.usage",
		Start:  start,
		End:    start.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got == nil || got.Aggregation != "" || got.IntervalSec != 0 {
		t.Fatalf("service received %+v", got)
	}
	if resp.Series == nil || len(resp.Series) != 0 {
		t.Fatalf("Series = %#v, want empty", resp.Series)
	}
}

func TestQuery_ServiceErrorReachesTheCaller(t *testing.T) {
	notFound := sdkerrors.NewNotFound("timeseries.Query", "metric not found")
	tr, err := New(&mockQuicBaaS{tsQueryFunc: func(context.Context, *service.TSQueryRequest) (*service.TSQueryResponse, error) {
		return nil, notFound
	}})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	_, err = timeseries.NewClient(tr).Query(context.Background(), &timeseries.QueryRequest{
		Metric: "cpu.usage",
		Start:  start,
		End:    start.Add(time.Minute),
	})
	if !errors.Is(err, notFound) {
		t.Fatalf("Query error = %v, want %v", err, notFound)
	}
}

func TestQuery_WrongOrNilRequestIsRefused(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}

	for name, req := range map[string]interface{}{
		"string":    "cpu.usage",
		"nil query": (*timeseries.QueryRequest)(nil),
	} {
		_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Query", req)
		var invalidReqErr *InvalidRequestTypeError
		if !errors.As(err, &invalidReqErr) {
			t.Errorf("%s: error = %v, want *InvalidRequestTypeError", name, err)
		}
	}
}

func TestQuery_ServiceRequestIsStillServed(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	resp, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Query",
		&service.TSQueryRequest{Metric: "cpu.usage", StartTime: now.Add(-time.Minute), EndTime: now})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if _, ok := resp.(*service.TSQueryResponse); !ok {
		t.Fatalf("response = %#v, want *service.TSQueryResponse", resp)
	}
}
