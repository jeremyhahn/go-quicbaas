//go:build integration

package helpers

import (
	"context"
	"errors"
	"testing"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// statusOnlyCluster answers Status with a fixed payload or error. Every other
// cluster call panics through the nil embedded interface.
type statusOnlyCluster struct {
	cluster.Client
	status *cluster.ClusterStatus
	err    error
}

func (c *statusOnlyCluster) Status(context.Context) (*cluster.ClusterStatus, error) {
	return c.status, c.err
}

// statusOnlyClient hands out a statusOnlyCluster. Every other client call
// panics through the nil embedded interface.
type statusOnlyClient struct {
	quicbaas.Client
	cluster *statusOnlyCluster
}

func (c *statusOnlyClient) Cluster() quicbaas.ClusterClient {
	return c.cluster
}

func testClusterOf(clusters ...*statusOnlyCluster) *TestCluster {
	tc := &TestCluster{}
	for i, c := range clusters {
		tc.Clients = append(tc.Clients, &statusOnlyClient{cluster: c})
		tc.Nodes = append(tc.Nodes, &TestNode{ID: uint64(i + 1)})
	}
	return tc
}

// healthyStatusFromNode3 is the status node 3 of a healthy three-node cluster
// returns: node 2 leads the cluster and its shard.
func healthyStatusFromNode3() *cluster.ClusterStatus {
	return &cluster.ClusterStatus{
		LeaderID: "2",
		Healthy:  true,
		Members: []cluster.ClusterMember{
			{ID: "3", IsLeader: false, Healthy: true},
			{ID: "1", IsLeader: false, Healthy: true},
			{ID: "2", IsLeader: true, Healthy: true},
		},
		Shards: []cluster.ClusterShard{{ShardID: 1, LeaderID: 2, Replicas: []uint64{1, 2, 3}}},
	}
}

func TestFindLeader_ReadsLeaderFromStatusPayload(t *testing.T) {
	status := healthyStatusFromNode3()
	tc := testClusterOf(
		&statusOnlyCluster{status: status},
		&statusOnlyCluster{status: status},
		&statusOnlyCluster{status: status},
	)

	node, client, err := tc.FindLeader(context.Background())
	if err != nil {
		t.Fatalf("FindLeader: %v", err)
	}
	if node.ID != 2 || !node.IsLeader {
		t.Fatalf("leader = node %d (IsLeader=%v), want node 2", node.ID, node.IsLeader)
	}
	if client != tc.Clients[1] {
		t.Fatalf("leader client is not node 2's client")
	}
}

func TestFindLeader_LeaderIDWithoutMemberFlag(t *testing.T) {
	status := healthyStatusFromNode3()
	for i := range status.Members {
		status.Members[i].IsLeader = false
	}
	tc := testClusterOf(&statusOnlyCluster{status: status}, &statusOnlyCluster{status: status}, &statusOnlyCluster{status: status})

	node, _, err := tc.FindLeader(context.Background())
	if err != nil {
		t.Fatalf("FindLeader: %v", err)
	}
	if node.ID != 2 {
		t.Fatalf("leader = node %d, want node 2", node.ID)
	}
}

func TestFindLeader_SurfacesRefusedStatus(t *testing.T) {
	for _, refusal := range []error{
		sdkerrors.NewUnauthorized("cluster.Status", "missing bearer"),
		sdkerrors.NewForbidden("cluster.Status", "admin:status not granted"),
	} {
		tc := testClusterOf(&statusOnlyCluster{err: refusal}, &statusOnlyCluster{err: refusal}, &statusOnlyCluster{err: refusal})

		_, _, err := tc.FindLeader(context.Background())

		var refused *ClusterStatusRefusedError
		if !errors.As(err, &refused) {
			t.Fatalf("FindLeader error = %v, want *ClusterStatusRefusedError", err)
		}
		if refused.NodeID != 1 {
			t.Fatalf("refused node = %d, want 1", refused.NodeID)
		}
		if !errors.Is(err, refusal) {
			t.Fatalf("FindLeader error does not wrap the status refusal %v", refusal)
		}
	}
}

func TestFindLeader_NoLeaderKeepsLastStatusError(t *testing.T) {
	transient := errors.New("connection reset")
	noLeader := healthyStatusFromNode3()
	noLeader.LeaderID = ""
	for i := range noLeader.Members {
		noLeader.Members[i].IsLeader = false
	}
	tc := testClusterOf(&statusOnlyCluster{status: noLeader}, &statusOnlyCluster{err: transient})

	_, _, err := tc.FindLeader(context.Background())

	var none *NoLeaderError
	if !errors.As(err, &none) {
		t.Fatalf("FindLeader error = %v, want *NoLeaderError", err)
	}
	if !errors.Is(err, transient) {
		t.Fatalf("FindLeader error does not wrap the last status error")
	}
}
