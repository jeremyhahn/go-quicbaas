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
// +build integration

package iam_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/certfixture"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// setupSessionSelf is setupUserForSessionTests for tests that call the
// caller-scoped session routes (/api/v1/iam/sessions...). It creates a tenant
// and a PIV user in it, both as the system admin, binds a fixture-CA client
// certificate to the user through the invite flow and logs the user in once.
// It returns the tenant, the user and a view of tc whose HTTP helpers send
// that user's own bearer. The user holds no role: the session routes act on
// the caller's own sessions. The caller deletes the tenant with
// cleanupSessionTests. The view holds no gRPC connection, so closing it is a
// no-op.
func setupSessionSelf(t *testing.T, tc *testCluster) (tenantID, userID string, self *testCluster) {
	t.Helper()
	admin := tc.asSystemAdmin()

	tenantID = generateUniqueTenantID()
	require.NoError(t, admin.postJSON("/api/v1/tenants", map[string]interface{}{
		"id":   tenantID,
		"name": "Session Test Tenant",
	}, nil))

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// The certificate's tenant-scoped SPIFFE SAN is what PIV login resolves
	// the tenant from; its email and common name name this user.
	email := fmt.Sprintf("session-user-%s@integration.test", tenantID)
	authority, err := helpers.FixtureAuthority(ctx)
	require.NoError(t, err, "fixture CA for the session user")
	issued, err := authority.IssueClient(ctx, certfixture.ClientFixture{
		CommonName:   email,
		Email:        email,
		TenantID:     tenantID,
		ValidityDays: 1,
	})
	require.NoError(t, err, "session user client certificate")

	var user struct {
		ID          string `json:"id"`
		InviteToken string `json:"invite_token"`
	}
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), map[string]interface{}{
		"email":        email,
		"username":     generateUniqueUsername(),
		"name":         "Session Test User",
		"enabled":      true,
		"auth_methods": []string{"piv"},
	}, &user))
	require.NotEmpty(t, user.ID, "session user id")
	require.NotEmpty(t, user.InviteToken, "session user invite token")

	require.NoError(t, helpers.ActivateInvitePIV(ctx, tc.httpEndpoint, tenantID, user.InviteToken, issued.CertificatePEM),
		"activate session user")

	// One login, one session: every self-call reuses this bearer so the
	// test's own calls add no sessions of their own.
	bearer, err := helpers.LoginWithCert(ctx, tc.httpEndpoint, issued.CertificatePEM, issued.PrivateKeyPEM)
	require.NoError(t, err, "session user PIV login")

	view := *tc
	view.grpcConn = nil
	view.bearer = func(context.Context) (string, error) { return bearer, nil }
	return tenantID, user.ID, &view
}
