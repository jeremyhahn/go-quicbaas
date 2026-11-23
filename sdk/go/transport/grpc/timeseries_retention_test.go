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

// fakeRetentionServer records the retention RPCs it receives.
type fakeRetentionServer struct {
	pb.UnimplementedQuicBaaSServer
	fail    bool
	gotSet  *pb.TSSetRetentionPolicyRequest
	gotGet  *pb.TSGetRetentionPolicyRequest
	getResp *pb.TSGetRetentionPolicyResponse
}

func (f *fakeRetentionServer) TSSetRetentionPolicy(_ context.Context, req *pb.TSSetRetentionPolicyRequest) (*pb.TSSetRetentionPolicyResponse, error) {
	f.gotSet = req
	if f.fail {
		return nil, status.Error(codes.NotFound, "no such metric")
	}
	return &pb.TSSetRetentionPolicyResponse{Success: true}, nil
}

func (f *fakeRetentionServer) TSGetRetentionPolicy(_ context.Context, req *pb.TSGetRetentionPolicyRequest) (*pb.TSGetRetentionPolicyResponse, error) {
	f.gotGet = req
	if f.fail {
		return nil, status.Error(codes.NotFound, "no such metric")
	}
	return f.getResp, nil
}

func newRetentionClient(t *testing.T, fake *fakeRetentionServer) timeseries.Client {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterQuicBaaSServer(s, fake) })
	return timeseries.NewClient(&Transport{
		cfg:  &transport.Config{Timeout: 5 * time.Second},
		conn: srv.dial(t),
	})
}

func TestTimeSeriesSetRetention_ReachesTSSetRetentionPolicy(t *testing.T) {
	fake := &fakeRetentionServer{}
	client := newRetentionClient(t, fake)

	err := client.SetRetention(context.Background(), &timeseries.SetRetentionRequest{
		Metric: "cpu.load",
		Policy: timeseries.RetentionPolicy{Duration: time.Hour, Resolution: time.Minute, Aggregation: timeseries.AggregateAvg},
	})
	if err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	got := fake.gotSet
	if got == nil || got.GetMetric() != "cpu.load" {
		t.Fatalf("SetRetention did not reach TSSetRetentionPolicy for cpu.load: %v", got)
	}
	p := got.GetPolicy()
	if p.GetDurationSeconds() != 3600 || p.GetResolutionSeconds() != 60 || p.GetAggregation() != "avg" {
		t.Errorf("TSSetRetentionPolicy got policy %v", p)
	}
}

func TestTimeSeriesGetRetention_ReachesTSGetRetentionPolicy(t *testing.T) {
	fake := &fakeRetentionServer{getResp: &pb.TSGetRetentionPolicyResponse{
		Metric: "cpu",
		Policy: &pb.TSRetentionPolicy{DurationSeconds: 86400, ResolutionSeconds: 300, Aggregation: "max"},
	}}
	client := newRetentionClient(t, fake)

	resp, err := client.GetRetention(context.Background(), &timeseries.GetRetentionRequest{Metric: "cpu"})
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if fake.gotGet.GetMetric() != "cpu" {
		t.Errorf("TSGetRetentionPolicy got metric %q", fake.gotGet.GetMetric())
	}
	want := timeseries.RetentionPolicy{Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: timeseries.AggregateMax}
	if resp.Metric != "cpu" || resp.Policy == nil || *resp.Policy != want {
		t.Fatalf("GetRetention: got %+v (policy %+v), want %+v", resp, resp.Policy, want)
	}
}

func TestTimeSeriesGetRetention_NoPolicyIsNil(t *testing.T) {
	client := newRetentionClient(t, &fakeRetentionServer{getResp: &pb.TSGetRetentionPolicyResponse{}})
	resp, err := client.GetRetention(context.Background(), &timeseries.GetRetentionRequest{Metric: "cpu"})
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if resp.Metric != "cpu" || resp.Policy != nil {
		t.Fatalf("GetRetention: got %+v, want metric cpu and a nil policy", resp)
	}
}

// TestTimeSeriesGetRetention_ZeroPolicyIsNil pins that a zero policy from the
// server (the backend's representation of "no policy") reads nil.
func TestTimeSeriesGetRetention_ZeroPolicyIsNil(t *testing.T) {
	client := newRetentionClient(t, &fakeRetentionServer{getResp: &pb.TSGetRetentionPolicyResponse{
		Metric: "m", Policy: &pb.TSRetentionPolicy{},
	}})
	resp, err := client.GetRetention(context.Background(), &timeseries.GetRetentionRequest{Metric: "m"})
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if resp.Policy != nil {
		t.Fatalf("GetRetention: got policy %+v, want nil", resp.Policy)
	}
}

func TestTimeSeriesRetention_ServerErrorIsTyped(t *testing.T) {
	client := newRetentionClient(t, &fakeRetentionServer{fail: true})
	ctx := context.Background()
	if err := client.SetRetention(ctx, &timeseries.SetRetentionRequest{
		Metric: "cpu", Policy: timeseries.RetentionPolicy{Duration: time.Hour},
	}); !errors.IsNotFound(err) {
		t.Errorf("SetRetention: want NotFound, got %v", err)
	}
	if _, err := client.GetRetention(ctx, &timeseries.GetRetentionRequest{Metric: "cpu"}); !errors.IsNotFound(err) {
		t.Errorf("GetRetention: want NotFound, got %v", err)
	}
}
