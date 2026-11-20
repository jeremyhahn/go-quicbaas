// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//go:build integration

package helpers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// fakeStatusClient is a quicbaas.Client whose Cluster().Status returns a
// fixed answer. Only Cluster is implemented; any other method panics on the
// nil embedded interface.
type fakeStatusClient struct {
	quicbaas.Client
	cluster *fakeClusterClient
}

func (f *fakeStatusClient) Cluster() quicbaas.ClusterClient { return f.cluster }

type fakeClusterClient struct {
	quicbaas.ClusterClient
	status *quicbaas.ClusterStatus
	err    error
	// hang makes Status block until its context is done, as a node that
	// never answers does.
	hang bool
}

// Status answers like a real client: a context that is already done, or
// ends while hang holds the call, fails it with the context's error.
func (f *fakeClusterClient) Status(ctx context.Context) (*quicbaas.ClusterStatus, error) {
	if f.hang {
		<-ctx.Done()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.status, f.err
}

func newFakeCluster(answers ...*fakeClusterClient) *TestCluster {
	tc := &TestCluster{}
	for i, a := range answers {
		tc.Nodes = append(tc.Nodes, &TestNode{ID: uint64(i + 1)})
		tc.Clients = append(tc.Clients, &fakeStatusClient{cluster: a})
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

func TestFindLeaderReportsEachNodeStatusError(t *testing.T) {
	tc := newFakeCluster(
		&fakeClusterClient{err: errors.New("permission denied: platform token rejected")},
		&fakeClusterClient{err: errors.New("decode cluster status: unexpected EOF")},
		&fakeClusterClient{err: errors.New("connection refused")},
	)

	node, client, err := tc.FindLeader(context.Background())

	require.Error(t, err)
	assert.Nil(t, node)
	assert.Nil(t, client)
	msg := err.Error()
	assert.Contains(t, msg, "no leader found in cluster")
	assert.Contains(t, msg, "node1: permission denied: platform token rejected")
	assert.Contains(t, msg, "node2: decode cluster status: unexpected EOF")
	assert.Contains(t, msg, "node3: connection refused")
}

func TestFindLeaderReportsEmptyAndLeaderlessStatus(t *testing.T) {
	tc := newFakeCluster(
		&fakeClusterClient{status: &quicbaas.ClusterStatus{}},
		&fakeClusterClient{status: &quicbaas.ClusterStatus{Members: []quicbaas.ClusterMember{{ID: "1"}, {ID: "2"}}}},
	)

	_, _, err := tc.FindLeader(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "node1: no members")
	assert.Contains(t, err.Error(), "node2: no member marked leader")
}

func TestFindLeaderReturnsMarkedLeader(t *testing.T) {
	tc := newFakeCluster(
		&fakeClusterClient{err: errors.New("unavailable")},
		&fakeClusterClient{status: &quicbaas.ClusterStatus{Members: []quicbaas.ClusterMember{{ID: "1"}, {ID: "2", IsLeader: true}}}},
	)

	node, client, err := tc.FindLeader(context.Background())

	require.NoError(t, err)
	require.NotNil(t, node)
	assert.Equal(t, uint64(2), node.ID)
	assert.True(t, node.IsLeader)
	assert.Same(t, tc.Clients[1], client)
}

// TestFindLeaderBoundsEachNodeStatusCall checks that a node that never
// answers uses up only its own bound, not the whole context, so the next
// node is still asked and its leader is found.
func TestFindLeaderBoundsEachNodeStatusCall(t *testing.T) {
	tc := newFakeCluster(
		&fakeClusterClient{hang: true},
		&fakeClusterClient{status: &quicbaas.ClusterStatus{Members: []quicbaas.ClusterMember{{ID: "1"}, {ID: "2", IsLeader: true}}}},
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(tc.Clients))*nodeStatusTimeout+time.Second)
	defer cancel()

	node, client, err := tc.FindLeader(ctx)

	require.NoError(t, err)
	require.NotNil(t, node)
	assert.Equal(t, uint64(2), node.ID)
	assert.Same(t, tc.Clients[1], client)
}

func TestFindLeaderReportsInvalidAndUnknownLeaderIDs(t *testing.T) {
	tests := []struct {
		name   string
		status *quicbaas.ClusterStatus
		want   string
	}{
		{
			name:   "unparsable member-marked leader",
			status: &quicbaas.ClusterStatus{Members: []quicbaas.ClusterMember{{ID: "1"}, {ID: "node-x", IsLeader: true}}},
			want:   `node2: leader member ID "node-x"`,
		},
		{
			name:   "unparsable LeaderID",
			status: &quicbaas.ClusterStatus{LeaderID: "leader-y", Members: []quicbaas.ClusterMember{{ID: "1"}, {ID: "2"}}},
			want:   `node2: leader ID "leader-y"`,
		},
		{
			name:   "unknown member-marked leader",
			status: &quicbaas.ClusterStatus{Members: []quicbaas.ClusterMember{{ID: "1"}, {ID: "9", IsLeader: true}}},
			want:   "node2: leader member 9 is not a test node",
		},
		{
			name:   "unknown LeaderID",
			status: &quicbaas.ClusterStatus{LeaderID: "42", Members: []quicbaas.ClusterMember{{ID: "1"}, {ID: "2"}}},
			want:   "node2: leader ID 42 is not a test node",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := newFakeCluster(
				&fakeClusterClient{err: errors.New("unavailable")},
				&fakeClusterClient{status: tt.status},
			)

			node, client, err := tc.FindLeader(context.Background())

			require.Error(t, err)
			assert.Nil(t, node)
			assert.Nil(t, client)
			assert.Contains(t, err.Error(), "node1: unavailable")
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestFindLeaderFallsBackToLeaderIDWhenMarkedLeaderUnusable(t *testing.T) {
	tests := []struct {
		name     string
		memberID string
	}{
		{name: "unparsable member-marked leader", memberID: "node-x"},
		{name: "unknown member-marked leader", memberID: "9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := newFakeCluster(
				&fakeClusterClient{status: &quicbaas.ClusterStatus{
					LeaderID: "2",
					Members:  []quicbaas.ClusterMember{{ID: tt.memberID, IsLeader: true}, {ID: "2"}},
				}},
				&fakeClusterClient{err: errors.New("unavailable")},
			)

			node, client, err := tc.FindLeader(context.Background())

			require.NoError(t, err)
			require.NotNil(t, node)
			assert.Equal(t, uint64(2), node.ID)
			assert.True(t, node.IsLeader)
			assert.Same(t, tc.Clients[1], client)
		})
	}
}

// TestClusterBuilderAuthToken checks that a built cluster's clients carry the
// tenant operator's bearer unless the caller sets one, and that a bearer the
// source cannot supply fails Build before it connects.
func TestClusterBuilderAuthToken(t *testing.T) {
	t.Run("DefaultIsTenantOperator", func(t *testing.T) {
		calls := 0
		cb := NewClusterBuilder()
		cb.bearerSource = func(context.Context) (string, error) {
			calls++
			return "fake-bearer", nil
		}

		token, err := cb.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "fake-bearer", token)
		assert.Equal(t, 1, calls)
	})

	t.Run("ExplicitTokenWins", func(t *testing.T) {
		calls := 0
		cb := NewClusterBuilder().WithAuthToken("explicit")
		cb.bearerSource = func(context.Context) (string, error) {
			calls++
			return "fake-bearer", nil
		}

		token, err := cb.resolveAuthToken(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "explicit", token)
		assert.Zero(t, calls)
	})

	t.Run("SourceErrorFailsBuild", func(t *testing.T) {
		sourceErr := errors.New("fake source failure")
		cb := NewClusterBuilder().WithEndpoints([]string{"127.0.0.1:1"})
		cb.bearerSource = func(context.Context) (string, error) {
			return "", sourceErr
		}

		cluster, err := cb.Build(context.Background())
		assert.Nil(t, cluster)
		require.Error(t, err)
		assert.ErrorIs(t, err, sourceErr)
		assert.NotContains(t, err.Error(), "failed to connect")
	})
}
