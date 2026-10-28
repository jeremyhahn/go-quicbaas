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

package transactions

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// setupGRPCConnection establishes a gRPC connection to the first cluster endpoint.
// It wraps the central helpers package for transaction-specific test setup.
func setupGRPCConnection(t *testing.T, ctx context.Context) *grpc.ClientConn {
	endpoints := helpers.GetClusterEndpoints()
	return helpers.MustSetupGRPCConnectionToEndpoint(t, ctx, endpoints[0],
		grpc.WithPerRPCCredentials(helpers.TenantOperatorGRPCCredentials(t)))
}
