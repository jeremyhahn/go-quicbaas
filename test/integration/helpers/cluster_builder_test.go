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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

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
}

func (f *fakeClusterClient) Status(context.Context) (*quicbaas.ClusterStatus, error) {
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
