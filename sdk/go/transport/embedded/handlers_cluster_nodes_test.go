package embedded

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestNodes_SDKClientListsTheClusterNodes(t *testing.T) {
	var got *service.ClusterStatusRequest
	tr, err := New(&mockQuicBaaS{clusterStatusFunc: func(_ context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		got = req
		return &service.ClusterStatusResponse{ClusterID: "c1", Nodes: []*service.NodeInfo{
			{NodeID: 1, Address: "node-1.example.com:63001", IsLeader: true, HealthStatus: service.HealthStatusHealthy},
			{NodeID: 2, Address: "node-2.example.com:63001", HealthStatus: service.HealthStatusDegraded},
			{NodeID: 3, Address: "node-3.example.com:63001", HealthStatus: service.HealthStatusUnknown},
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := cluster.NewClient(tr).Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if got == nil || got.IncludeDetails {
		t.Fatalf("service received %+v, want a status request without details", got)
	}
	want := []cluster.NodeInfo{
		{ID: "1", Name: "node-1.example.com:63001", Address: "node-1.example.com:63001", State: cluster.NodeStateHealthy},
		{ID: "2", Name: "node-2.example.com:63001", Address: "node-2.example.com:63001", State: cluster.NodeStateUnhealthy},
		{ID: "3", Name: "node-3.example.com:63001", Address: "node-3.example.com:63001", State: cluster.NodeStateUnknown},
	}
	if len(resp.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d", len(resp.Nodes), len(want))
	}
	for i := range want {
		if resp.Nodes[i] != want[i] {
			t.Errorf("node %d = %+v, want %+v", i, resp.Nodes[i], want[i])
		}
	}
}

func TestNodes_UnavailableBackendIsAnError(t *testing.T) {
	unavailable := sdkerrors.NewUnavailable("cluster.Status", "raft host not started")
	tr, err := New(&mockQuicBaaS{clusterStatusFunc: func(context.Context, *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		return nil, unavailable
	}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = cluster.NewClient(tr).Nodes(context.Background())
	if !errors.Is(err, unavailable) {
		t.Fatalf("Nodes error = %v, want %v", err, unavailable)
	}
}

func TestNodes_NilBackendResponseIsAnEmptyList(t *testing.T) {
	tr, err := New(&mockQuicBaaS{clusterStatusFunc: func(context.Context, *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := cluster.NewClient(tr).Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if len(resp.Nodes) != 0 {
		t.Fatalf("got %d nodes, want none", len(resp.Nodes))
	}
}
