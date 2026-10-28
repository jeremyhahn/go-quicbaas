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

//go:build integration

package helpers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

const (
	testNode1REST = "https://test-node1:8080"
	testNode2REST = "https://test-node2:8080"
	testNode2RPC  = "https://test-node2:50051/quicbaas.v1.KeyManagement"
)

var errNodeLoginRefused = errors.New("node login refused")

// setTestClusterEndpoints pins the cluster's REST and gRPC endpoints, which
// restNodeOf pairs by node index.
func setTestClusterEndpoints(t *testing.T) {
	t.Setenv("QUICBAAS_NODE1_URL", testNode1REST)
	t.Setenv("QUICBAAS_NODE2_URL", testNode2REST)
	t.Setenv("QUICBAAS_NODE3_URL", "https://test-node3:8080")
	t.Setenv("QUICBAAS_NODE1_GRPC", "test-node1:50051")
	t.Setenv("QUICBAAS_NODE2_GRPC", "test-node2:50051")
	t.Setenv("QUICBAAS_NODE3_GRPC", "test-node3:50051")
}

func TestNodeBearerCredentials_SendsEachNodeItsOwnBearer(t *testing.T) {
	setTestClusterEndpoints(t)
	logins := map[string]int{}
	creds := newNodeBearerCredentials(testNode1REST, "test", func(_ context.Context, node string) (dataplane.TokenSource, error) {
		logins[node]++
		return func(context.Context) (string, error) { return "bearer@" + node, nil }, nil
	})

	for range 2 {
		md, err := creds.GetRequestMetadata(context.Background(), testNode2RPC)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"authorization": "Bearer bearer@" + testNode2REST}, md)
	}
	md, err := creds.GetRequestMetadata(context.Background(), "https://elsewhere:50051/quicbaas.v1.KeyManagement")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"authorization": "Bearer bearer@" + testNode1REST}, md,
		"an RPC to no known node carries the home node's bearer")
	require.Equal(t, map[string]int{testNode1REST: 1, testNode2REST: 1}, logins)
	require.True(t, creds.RequireTransportSecurity())
}

func TestNodeBearerCredentials_SourceFailureIsReturned(t *testing.T) {
	setTestClusterEndpoints(t)
	creds := newNodeBearerCredentials(testNode1REST, "test", func(context.Context, string) (dataplane.TokenSource, error) {
		return nil, errNodeLoginRefused
	})

	md, err := creds.GetRequestMetadata(context.Background(), testNode2RPC)
	require.ErrorIs(t, err, errNodeLoginRefused)
	require.Nil(t, md)
}

func TestPrincipalGRPCCredentials_LogInAtTheRPCsNode(t *testing.T) {
	setTestClusterEndpoints(t)
	fake := &fakeEnrollment{expiresIn: 900}
	logins := map[string]int{}
	registry := newPrincipalRegistry("test", fake.enroll, func(_ context.Context, baseURL string, _, _ []byte) (string, int, error) {
		logins[baseURL]++
		return "bearer@" + baseURL, 900, nil
	})

	creds, err := registry.newGRPCCredentials(context.Background())
	require.NoError(t, err)
	md, err := creds.GetRequestMetadata(context.Background(), testNode2RPC)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"authorization": "Bearer bearer@" + testNode2REST}, md)
	require.Equal(t, map[string]int{testNode1REST: 1, testNode2REST: 1}, logins,
		"the principal logs in at the cluster's first node, then at the node the RPC goes to")
}
