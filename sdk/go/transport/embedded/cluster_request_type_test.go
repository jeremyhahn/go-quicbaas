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

package embedded

import (
	"context"
	stderrors "errors"
	"math"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

// The cluster client sends its own request types; these tests drive the real
// client over the embedded transport so a handler that only accepts the
// service request type fails here (go-quicbaas#1544).

func newClusterClient(t *testing.T, svc *mockQuicBaaS) cluster.Client {
	t.Helper()
	tr, err := New(svc)
	if err != nil {
		t.Fatal(err)
	}
	return cluster.NewClient(tr)
}

func TestClusterStatus_SDKRequestReachesTheService(t *testing.T) {
	var got *service.ClusterStatusRequest
	client := newClusterClient(t, &mockQuicBaaS{clusterStatusFunc: func(_ context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		got = req
		return &service.ClusterStatusResponse{
			ClusterID: "c1",
			Nodes: []*service.NodeInfo{
				{NodeID: 1, Address: "node-1:63001", IsLeader: true, HealthStatus: service.HealthStatusHealthy},
				{NodeID: 2, Address: "node-2:63001", HealthStatus: service.HealthStatusHealthy},
			},
			Shards: []*service.ShardInfo{{ShardID: 7, LeaderID: 1, Replicas: []uint64{1, 2}}},
		}, nil
	}})

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got == nil || !got.IncludeDetails {
		t.Fatalf("service received %+v", got)
	}
	if status.ClusterID != "c1" || status.LeaderID != "1" || !status.Healthy || len(status.Members) != 2 {
		t.Fatalf("status = %+v", status)
	}
	if m := status.Members[1]; m.ID != "2" || m.Address != "node-2:63001" || m.IsLeader || !m.Healthy {
		t.Fatalf("member = %+v", m)
	}
	if len(status.Shards) != 1 || status.Shards[0].ShardID != 7 || status.Shards[0].LeaderID != 1 || len(status.Shards[0].Replicas) != 2 {
		t.Fatalf("shards = %+v", status.Shards)
	}
}

func TestClusterStatus_UnhealthyMemberMakesClusterUnhealthy(t *testing.T) {
	client := newClusterClient(t, &mockQuicBaaS{clusterStatusFunc: func(context.Context, *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		return &service.ClusterStatusResponse{Nodes: []*service.NodeInfo{
			{NodeID: 1, IsLeader: true, HealthStatus: service.HealthStatusHealthy},
			{NodeID: 2, HealthStatus: service.HealthStatusUnhealthy},
		}}, nil
	}})

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Healthy {
		t.Fatal("a cluster with an unhealthy member must not report healthy")
	}
}

func TestClusterStatus_ServiceErrorIsReturned(t *testing.T) {
	want := stderrors.New("status failed")
	client := newClusterClient(t, &mockQuicBaaS{clusterStatusFunc: func(context.Context, *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		return nil, want
	}})

	if _, err := client.Status(context.Background()); !stderrors.Is(err, want) {
		t.Fatalf("Status err = %v, want %v", err, want)
	}
}

func TestClusterRemoveNode_SDKRequestReachesTheService(t *testing.T) {
	var got *service.RemoveNodeRequest
	client := newClusterClient(t, &mockQuicBaaS{removeNodeFunc: func(_ context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
		got = req
		return &service.RemoveNodeResponse{Success: true}, nil
	}})

	if err := client.RemoveNode(context.Background(), &cluster.RemoveNodeRequest{NodeID: "5"}); err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}
	if got == nil || got.NodeID != 5 {
		t.Fatalf("service received %+v", got)
	}
}

func TestClusterRemoveNode_UnsuccessfulRemoveIsAnError(t *testing.T) {
	client := newClusterClient(t, &mockQuicBaaS{removeNodeFunc: func(context.Context, *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
		return &service.RemoveNodeResponse{Success: false, Message: "not a member"}, nil
	}})

	if err := client.RemoveNode(context.Background(), &cluster.RemoveNodeRequest{NodeID: "5"}); err == nil {
		t.Fatal("an unsuccessful remove must be an error")
	}
}

func TestClusterRemoveNode_NonNumericNodeIDIsRefused(t *testing.T) {
	called := false
	client := newClusterClient(t, &mockQuicBaaS{removeNodeFunc: func(context.Context, *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
		called = true
		return &service.RemoveNodeResponse{Success: true}, nil
	}})

	if err := client.RemoveNode(context.Background(), &cluster.RemoveNodeRequest{NodeID: "node-5"}); err == nil {
		t.Fatal("a non-numeric node ID must be refused")
	}
	if called {
		t.Fatal("the service must not be called with an unparsable node ID")
	}
}

func TestClusterRebalance_SDKRequestReachesTheService(t *testing.T) {
	var got *service.RebalanceClusterRequest
	client := newClusterClient(t, &mockQuicBaaS{rebalanceFunc: func(_ context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
		got = req
		return &service.RebalanceClusterResponse{JobID: "job-9", EstimatedCompletion: 1700000000}, nil
	}})

	resp, err := client.RebalanceCluster(context.Background(), &cluster.RebalanceClusterRequest{Strategy: "even"})
	if err != nil {
		t.Fatalf("RebalanceCluster: %v", err)
	}
	if got == nil || got.Strategy != "even" {
		t.Fatalf("service received %+v", got)
	}
	if resp.OperationID != "job-9" || resp.Started != 1700000000 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestClusterRebalance_ServiceErrorIsReturned(t *testing.T) {
	want := stderrors.New("rebalance failed")
	client := newClusterClient(t, &mockQuicBaaS{rebalanceFunc: func(context.Context, *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
		return nil, want
	}})

	if _, err := client.RebalanceCluster(context.Background(), &cluster.RebalanceClusterRequest{Strategy: "even"}); !stderrors.Is(err, want) {
		t.Fatalf("RebalanceCluster err = %v, want %v", err, want)
	}
}

func TestClusterRebalanceProgress_SDKRequestReachesTheService(t *testing.T) {
	var got *service.GetRebalanceProgressRequest
	client := newClusterClient(t, &mockQuicBaaS{rebalanceProgressFunc: func(_ context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
		got = req
		return &service.GetRebalanceProgressResponse{
			OperationID:    "job-9",
			Status:         "in_progress",
			Progress:       40,
			ShardsMoved:    2,
			TotalShards:    5,
			ElapsedSeconds: 12.7,
		}, nil
	}})

	resp, err := client.GetRebalanceProgress(context.Background(), &cluster.GetRebalanceProgressRequest{OperationID: "job-9"})
	if err != nil {
		t.Fatalf("GetRebalanceProgress: %v", err)
	}
	if got == nil || got.OperationID != "job-9" {
		t.Fatalf("service received %+v", got)
	}
	want := cluster.GetRebalanceProgressResponse{OperationID: "job-9", Status: "in_progress", PercentComplete: 40, ShardsMoved: 2, TotalShards: 5, ElapsedTime: 12}
	if *resp != want {
		t.Fatalf("response = %+v, want %+v", *resp, want)
	}
}

func TestClusterRebalanceProgress_ServiceErrorIsReturned(t *testing.T) {
	want := stderrors.New("progress failed")
	client := newClusterClient(t, &mockQuicBaaS{rebalanceProgressFunc: func(context.Context, *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
		return nil, want
	}})

	if _, err := client.GetRebalanceProgress(context.Background(), &cluster.GetRebalanceProgressRequest{OperationID: "job-9"}); !stderrors.Is(err, want) {
		t.Fatalf("GetRebalanceProgress err = %v, want %v", err, want)
	}
}

func TestClusterRebalanceProgress_ShardCountOutOfRangeIsAnError(t *testing.T) {
	client := newClusterClient(t, &mockQuicBaaS{rebalanceProgressFunc: func(context.Context, *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
		return &service.GetRebalanceProgressResponse{OperationID: "job-9", TotalShards: math.MaxInt32 + 1}, nil
	}})

	if _, err := client.GetRebalanceProgress(context.Background(), &cluster.GetRebalanceProgressRequest{OperationID: "job-9"}); err == nil {
		t.Fatal("a shard count past int32 must be an error, not a wrapped value")
	}
}

func TestClusterHandlers_WrongRequestTypeIsTyped(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"Status", "RemoveNode", "Rebalance", "GetRebalanceStatus"} {
		t.Run(method, func(t *testing.T) {
			_, err := tr.Request(context.Background(), "quicbaas.Cluster", method, "wrong")
			var typed *InvalidRequestTypeError
			if !stderrors.As(err, &typed) {
				t.Fatalf("err = %v, want *InvalidRequestTypeError", err)
			}
			if typed.Method != method || typed.ActualType != "string" {
				t.Fatalf("typed error = %+v", typed)
			}
		})
	}
}
