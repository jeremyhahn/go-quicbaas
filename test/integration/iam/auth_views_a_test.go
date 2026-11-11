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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/certfixture"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// asTenantAuditor returns a view of tc whose HTTP helpers send the bearer of
// an auditor of tenantID: a PIV user the system admin creates in the tenant
// and binds to go-qrdb's built-in Auditor role, which holds the self-scoped
// audit:read and audit:query the audit routes check. The tenant must already
// exist; the caller deletes it, and the user with it. The fixture CA issues
// the user's client certificate, whose tenant-scoped SPIFFE SAN is what PIV
// login resolves the tenant from; the invite flow binds the certificate to
// the user and the user logs in once, so every call through the view reuses
// that one bearer. The view holds no gRPC connection, so closing it is a
// no-op.
func (tc *testCluster) asTenantAuditor(t *testing.T, tenantID string) *testCluster {
	t.Helper()
	admin := tc.asSystemAdmin()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	email := fmt.Sprintf("auditor-%s@integration.test", tenantID)
	authority, err := helpers.FixtureAuthority(ctx)
	require.NoError(t, err, "fixture CA for the tenant auditor")
	issued, err := authority.IssueClient(ctx, certfixture.ClientFixture{
		CommonName:   email,
		Email:        email,
		TenantID:     tenantID,
		ValidityDays: 1,
	})
	require.NoError(t, err, "tenant auditor client certificate")

	var user struct {
		ID          string `json:"id"`
		InviteToken string `json:"invite_token"`
	}
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), map[string]interface{}{
		"email":        email,
		"username":     generateUniqueUsername(),
		"name":         "Audit Test Auditor",
		"enabled":      true,
		"auth_methods": []string{"piv"},
	}, &user))
	require.NotEmpty(t, user.ID, "tenant auditor id")
	require.NotEmpty(t, user.InviteToken, "tenant auditor invite token")

	require.NoError(t, helpers.ActivateInvitePIV(ctx, tc.httpEndpoint, tenantID, user.InviteToken, issued.CertificatePEM),
		"activate tenant auditor")

	// The tenant is fresh, so its Auditor role carries the identifier go-qrdb
	// seeds it under. It is bound before the login so the session holds it.
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/roles", tenantID, user.ID), map[string]interface{}{
		"role_id": qrdbsdk.SystemRoleID(tenantID, qrdbsdk.RoleNameAuditor),
	}, nil), "bind the Auditor role to the tenant auditor")

	bearer, err := helpers.LoginWithCert(ctx, tc.httpEndpoint, issued.CertificatePEM, issued.PrivateKeyPEM)
	require.NoError(t, err, "tenant auditor PIV login")

	view := *tc
	view.grpcConn = nil
	view.bearer = func(context.Context) (string, error) { return bearer, nil }
	return &view
}
