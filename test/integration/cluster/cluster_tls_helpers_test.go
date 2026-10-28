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

//go:build integration

package cluster

import (
	"crypto/tls"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// requireTLSConfig loads the integration TLS config for tests that use
// gRPC with TLS. If the CA certificate is not available (e.g. when the
// cluster stack runs plain HTTP without mounted certs), the test is
// skipped rather than failed.
func requireTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	cfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		t.Skipf("skipping TLS-gRPC test: integration CA unavailable (%v); "+
			"cluster may be running plain HTTP", err)
	}
	return cfg
}

// tenantOperatorAuth sends the tenant operator's bearer on every call of a
// connection. KV RPCs (Put, Get, Delete) need go-qrdb's Operator grants,
// which the system admin does not hold in the tenant's namespace.
func tenantOperatorAuth(t *testing.T) grpc.DialOption {
	t.Helper()
	return grpc.WithPerRPCCredentials(helpers.TenantOperatorGRPCCredentials(t))
}

// systemAdminAuth sends the system admin's bearer on every call of a
// connection. Cluster RPCs (ClusterStatus, RebalanceCluster, AddNode, ...)
// need admin grants, which the tenant operator does not hold, so a test that
// calls both kinds uses one connection per principal.
func systemAdminAuth(t *testing.T) grpc.DialOption {
	t.Helper()
	return grpc.WithPerRPCCredentials(helpers.SystemAdminGRPCCredentials(t))
}

// dialSystemAdmin opens a connection to endpoint that calls as the system
// admin and closes it when the test ends.
func dialSystemAdmin(t *testing.T, endpoint string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(requireTLSConfig(t))),
		systemAdminAuth(t),
	)
	if err != nil {
		t.Fatalf("connect to %s as the system admin: %v", endpoint, err)
	}
	t.Cleanup(func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Logf("close system admin connection to %s: %v", endpoint, closeErr)
		}
	})
	return conn
}
