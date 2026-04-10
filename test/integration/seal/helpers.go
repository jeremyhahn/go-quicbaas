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

package seal

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// setupGRPCConnection creates a gRPC connection to the first cluster endpoint
// for seal integration tests.
func setupGRPCConnection(t *testing.T, ctx context.Context) *grpc.ClientConn {
	t.Helper()
	endpoints := helpers.GetClusterEndpoints()
	if len(endpoints) == 0 {
		t.Fatal("No gRPC endpoints configured")
	}
	return helpers.MustSetupGRPCConnectionToEndpoint(t, ctx, endpoints[0])
}
