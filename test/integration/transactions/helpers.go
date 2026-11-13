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

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// setupGRPCConnection establishes a gRPC connection to the first cluster endpoint.
// The connection acts as the tenant operator, whose Operator role holds the
// KV, batch and transaction grants: these verbs are tenant-scoped and refuse
// a caller with no tenant bound.
func setupGRPCConnection(t *testing.T, ctx context.Context) *grpc.ClientConn {
	t.Helper()
	endpoint := helpers.GetClusterEndpoints()[0]

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
		grpc.WithPerRPCCredentials(helpers.TenantOperatorGRPCCredentials(t)),
	)
	require.NoError(t, err, "Failed to connect to gRPC server at %s", endpoint)
	return conn
}
