// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeTimeSeriesServer records the TimeSeries RPCs it receives and answers
// with a fixed query result, or with a NotFound status when fail is set.
type fakeTimeSeriesServer struct {
	pb.UnimplementedQuicBaaSServer
	fail      bool
	gotWrite  *pb.TSWritePointRequest
	gotQuery  *pb.TSQueryRequest
	queryResp *pb.TSQueryResponse
}

func (f *fakeTimeSeriesServer) TSWritePoint(_ context.Context, req *pb.TSWritePointRequest) (*pb.TSWritePointResponse, error) {
	f.gotWrite = req
	if f.fail {
		return nil, status.Error(codes.NotFound, "no such metric")
	}
	return &pb.TSWritePointResponse{Success: true, Timestamp: req.GetTimestamp()}, nil
}

func (f *fakeTimeSeriesServer) TSQuery(_ context.Context, req *pb.TSQueryRequest) (*pb.TSQueryResponse, error) {
	f.gotQuery = req
	if f.fail {
		return nil, status.Error(codes.NotFound, "no such metric")
	}
	return f.queryResp, nil
}

// newTimeSeriesClient returns the real sdk/go TimeSeries client over a gRPC
// transport dialled to fake on a bufconn listener.
func newTimeSeriesClient(t *testing.T, fake *fakeTimeSeriesServer) (timeseries.Client, *Transport) {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterQuicBaaSServer(s, fake) })
	tr := &Transport{
		cfg:  &transport.Config{Timeout: 5 * time.Second},
		conn: srv.dial(t),
	}
	return timeseries.NewClient(tr), tr
}

func TestIsTimeSeriesService(t *testing.T) {
	if !isTimeSeriesService("quicbaas.TimeSeries") {
		t.Error("quicbaas.TimeSeries must be claimed by the TimeSeries handler")
	}
	if isTimeSeriesService("quicbaas.KV") {
		t.Error("quicbaas.KV must not be claimed by the TimeSeries handler")
	}
}

func TestTimeSeriesWrite_ReachesTSWritePoint(t *testing.T) {
	fake := &fakeTimeSeriesServer{}
	client, _ := newTimeSeriesClient(t, fake)
	ts := time.Unix(0, 1_700_000_000_123_456_789)

	err := client.Write(context.Background(), &timeseries.WriteRequest{
		Metric:    "cpu.load",
		Tags:      map[string]string{"host": "a", "dc": "east"},
		Timestamp: ts,
		Value:     0.75,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := fake.gotWrite
	if got == nil {
		t.Fatal("Write did not reach TSWritePoint")
	}
	if got.GetMetric() != "cpu.load" || got.GetValue() != 0.75 || got.GetTimestamp() != ts.UnixNano() {
		t.Errorf("TSWritePoint got metric=%q value=%v timestamp=%d", got.GetMetric(), got.GetValue(), got.GetTimestamp())
	}
	if len(got.GetTags()) != 2 || got.GetTags()["host"] != "a" || got.GetTags()["dc"] != "east" {
		t.Errorf("TSWritePoint got tags %v", got.GetTags())
	}
}

func TestTimeSeriesWrite_ServerErrorIsTyped(t *testing.T) {
	client, _ := newTimeSeriesClient(t, &fakeTimeSeriesServer{fail: true})

	err := client.Write(context.Background(), &timeseries.WriteRequest{Metric: "m", Timestamp: time.Unix(1, 0)})
	if !errors.IsNotFound(err) {
		t.Fatalf("Write error = %v, want a NotFound SDK error", err)
	}
}

func TestTimeSeriesQuery_ReachesTSQuery(t *testing.T) {
	start := time.Unix(0, 1_700_000_000_000_000_000)
	end := start.Add(time.Hour)
	fake := &fakeTimeSeriesServer{queryResp: &pb.TSQueryResponse{
		Metric: "cpu.load",
		Points: []*pb.TSDataPoint{
			{Metric: "cpu.load", Value: 1.5, Timestamp: start.UnixNano()},
			{Metric: "cpu.load", Value: 2.5, Timestamp: end.UnixNano()},
		},
	}}
	client, _ := newTimeSeriesClient(t, fake)

	resp, err := client.Query(context.Background(), &timeseries.QueryRequest{
		Metric:    "cpu.load",
		Tags:      map[string]string{"host": "a"},
		Start:     start,
		End:       end,
		Aggregate: timeseries.AggregateAvg,
		Interval:  5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	got := fake.gotQuery
	if got == nil {
		t.Fatal("Query did not reach TSQuery")
	}
	if got.GetMetric() != "cpu.load" || got.GetTags()["host"] != "a" || len(got.GetTags()) != 1 {
		t.Errorf("TSQuery got metric=%q tags=%v", got.GetMetric(), got.GetTags())
	}
	if got.GetStartTime() != start.UnixNano() || got.GetEndTime() != end.UnixNano() {
		t.Errorf("TSQuery got start=%d end=%d", got.GetStartTime(), got.GetEndTime())
	}
	if got.GetAggregation() != "AVG" || got.GetIntervalSeconds() != 300 {
		t.Errorf("TSQuery got aggregation=%q interval=%d", got.GetAggregation(), got.GetIntervalSeconds())
	}

	if len(resp.Series) != 2 {
		t.Fatalf("Query returned %d points, want 2", len(resp.Series))
	}
	if !resp.Series[0].Timestamp.Equal(start) || resp.Series[0].Value != 1.5 {
		t.Errorf("point 0 = %+v", resp.Series[0])
	}
	if !resp.Series[1].Timestamp.Equal(end) || resp.Series[1].Value != 2.5 {
		t.Errorf("point 1 = %+v", resp.Series[1])
	}
}

func TestTimeSeriesQuery_NoAggregateSendsNoAggregation(t *testing.T) {
	fake := &fakeTimeSeriesServer{queryResp: &pb.TSQueryResponse{}}
	client, _ := newTimeSeriesClient(t, fake)
	start := time.Unix(100, 0)

	resp, err := client.Query(context.Background(), &timeseries.QueryRequest{Metric: "m", Start: start, End: start.Add(time.Minute)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if fake.gotQuery.GetAggregation() != "" || fake.gotQuery.GetIntervalSeconds() != 0 {
		t.Errorf("TSQuery got aggregation=%q interval=%d, want none", fake.gotQuery.GetAggregation(), fake.gotQuery.GetIntervalSeconds())
	}
	if resp.Series == nil || len(resp.Series) != 0 {
		t.Errorf("Query series = %v, want an empty non-nil slice", resp.Series)
	}
}

func TestTimeSeriesQuery_ServerErrorIsTyped(t *testing.T) {
	client, _ := newTimeSeriesClient(t, &fakeTimeSeriesServer{fail: true})
	start := time.Unix(100, 0)

	_, err := client.Query(context.Background(), &timeseries.QueryRequest{Metric: "m", Start: start, End: start.Add(time.Minute)})
	if !errors.IsNotFound(err) {
		t.Fatalf("Query error = %v, want a NotFound SDK error", err)
	}
}

func TestTimeSeriesDispatch_UnknownMethodIsTyped(t *testing.T) {
	_, tr := newTimeSeriesClient(t, &fakeTimeSeriesServer{})

	_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Compact", &timeseries.QueryRequest{})
	if !errors.IsInvalidInput(err) {
		t.Fatalf("unknown method error = %v, want an InvalidInput SDK error", err)
	}
}

func TestTimeSeriesDispatch_WrongRequestTypeIsTyped(t *testing.T) {
	_, tr := newTimeSeriesClient(t, &fakeTimeSeriesServer{})

	for _, method := range []string{"Write", "Query"} {
		_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", method, "not a request")
		if !errors.IsInvalidInput(err) {
			t.Errorf("%s with a wrong request type: error = %v, want an InvalidInput SDK error", method, err)
		}
	}
}
