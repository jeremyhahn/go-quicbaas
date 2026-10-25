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

package helpers

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
)

// systemAdminPrincipalName names the fixture system admin in errors.
const systemAdminPrincipalName = "system admin"

// systemAdminAuth is the bootstrap system admin: the fixture client cert the
// cluster binds at first boot. It belongs to the system tenant, so it is the
// principal for cluster-wide routes (backups, cluster and tenant management,
// metrics) that no tenant principal may reach. It is never released: the
// system tenant outlives every suite.
var systemAdminAuth = newPrincipalRegistry(systemAdminPrincipalName, enrollSystemAdmin, loginWithPIVCA)

// NewSystemAdminHTTPClient returns NewHTTPClient's client with every request
// carrying the system admin's bearer.
func NewSystemAdminHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	return systemAdminAuth.httpClient(t, timeout)
}

// SystemAdminHTTPClient is NewSystemAdminHTTPClient returning its error, for
// callers without a *testing.T.
func SystemAdminHTTPClient(ctx context.Context, timeout time.Duration) (*http.Client, error) {
	return systemAdminAuth.newHTTPClient(ctx, timeout)
}

// SystemAdminGRPCCredentials returns per-RPC credentials that send the system
// admin's bearer on every gRPC call.
func SystemAdminGRPCCredentials(t *testing.T) credentials.PerRPCCredentials {
	t.Helper()
	return systemAdminAuth.grpcCredentials(t)
}

// enrollSystemAdmin loads the fixture system admin cert and key. Nothing is
// provisioned: first boot already bound the cert to the system admin.
func enrollSystemAdmin(_ context.Context, _ string) (*enrolledPrincipal, error) {
	certPEM, err := os.ReadFile(pivCertPath())
	if err != nil {
		return nil, &PrincipalAuthError{Principal: systemAdminPrincipalName, Step: "read client cert", Cause: err}
	}
	keyPEM, err := os.ReadFile(pivKeyPath())
	if err != nil {
		return nil, &PrincipalAuthError{Principal: systemAdminPrincipalName, Step: "read client key", Cause: err}
	}
	return &enrolledPrincipal{certPEM: certPEM, keyPEM: keyPEM}, nil
}
