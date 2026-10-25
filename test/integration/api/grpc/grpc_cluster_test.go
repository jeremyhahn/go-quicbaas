// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestGRPC_ClusterStatus verifies that the ClusterStatus RPC returns a valid
// response. In embedded/standalone mode the server has no real Raft consensus,
// so Nodes and Shards may be empty. The test validates the API is reachable
// and returns a well-formed response.
func TestGRPC_ClusterStatus(t *testing.T) {
	tlsConfig, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	conn, err := grpc.Dial("test-node1:50051", grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithPerRPCCredentials(helpers.SystemAdminGRPCCredentials(t)), grpc.WithBlock(), grpc.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("failed to dial grpc: %v", err)
	}
	defer conn.Close()
	client := pb.NewQuicBaaSClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.ClusterStatus(ctx, &pb.ClusterStatusRequest{})
	if err != nil {
		t.Fatalf("ClusterStatus error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil cluster status response")
	}

	// In embedded mode, shards and nodes may be empty. Log actual state
	// for diagnostics rather than requiring specific counts.
	t.Logf("ClusterStatus response: nodes=%d, shards=%d", len(resp.Nodes), len(resp.Shards))
}
