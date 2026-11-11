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

// pivUser is a PIV user of a test tenant and the fixture-CA client
// credential bound to it.
type pivUser struct {
	id      string
	certPEM []byte
	keyPEM  []byte
}

// asResolver returns a view of tc whose HTTP helpers send the bearer of a
// dedicated PIV user of tenantID bound to the tenant's built-in Admin role.
// That role is the narrowest existing one holding permissions:resolve, the
// permission of the PEP check routes (/api/v1/iam/check and its batch
// forms): go-qrdb places it on none of its five built-in roles, so the
// quicbaas module grants it to Admin (pkg/apimodule/module_grants.go,
// registerRoleGrants). The permission is self-scoped, so the view checks
// subjects of tenantID only. The user is created and bound by the system
// admin through the tenant lift, then logs in once; tenantID's deletion
// removes it. The view holds no gRPC connection, so closing it is a no-op.
func (tc *testCluster) asResolver(t *testing.T, tenantID string) *testCluster {
	t.Helper()
	user := enrollPIVUser(t, tc, tenantID, "resolver")

	binding := map[string]interface{}{"role_id": builtInRoleID(t, tenantID, qrdbsdk.RoleNameAdmin)}
	require.NoError(t, tc.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/roles", tenantID, user.id), binding, nil),
		"bind the Admin role to the resolver")

	return pivUserView(t, tc, user)
}

// enrollPIVUser has the system admin create a PIV user named after label in
// tenantID and binds a fixture-CA client certificate to it through the invite
// flow. The user holds no role.
func enrollPIVUser(t *testing.T, tc *testCluster, tenantID, label string) pivUser {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// The certificate's tenant-scoped SPIFFE SAN is what PIV login resolves
	// the tenant from; its email and common name name this user.
	email := fmt.Sprintf("%s-user-%s@integration.test", label, tenantID)
	authority, err := helpers.FixtureAuthority(ctx)
	require.NoError(t, err, "fixture CA for the %s user", label)
	issued, err := authority.IssueClient(ctx, certfixture.ClientFixture{
		CommonName:   email,
		Email:        email,
		TenantID:     tenantID,
		ValidityDays: 1,
	})
	require.NoError(t, err, "%s user client certificate", label)

	var user struct {
		ID          string `json:"id"`
		InviteToken string `json:"invite_token"`
	}
	require.NoError(t, tc.asSystemAdmin().postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), map[string]interface{}{
		"email":        email,
		"username":     generateUniqueUsername(),
		"name":         label + " test user",
		"enabled":      true,
		"auth_methods": []string{"piv"},
	}, &user))
	require.NotEmpty(t, user.ID, "%s user id", label)
	require.NotEmpty(t, user.InviteToken, "%s user invite token", label)

	require.NoError(t, helpers.ActivateInvitePIV(ctx, tc.httpEndpoint, tenantID, user.InviteToken, issued.CertificatePEM),
		"activate %s user", label)

	return pivUser{id: user.ID, certPEM: issued.CertificatePEM, keyPEM: issued.PrivateKeyPEM}
}

// pivUserView logs user in once and returns a view of tc whose HTTP helpers
// send that bearer, so the test's own calls add no sessions of their own.
// The view holds no gRPC connection, so closing it is a no-op.
func pivUserView(t *testing.T, tc *testCluster, user pivUser) *testCluster {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	bearer, err := helpers.LoginWithCert(ctx, tc.httpEndpoint, user.certPEM, user.keyPEM)
	require.NoError(t, err, "PIV login of user %s", user.id)

	view := *tc
	view.grpcConn = nil
	view.bearer = func(context.Context) (string, error) { return bearer, nil }
	return &view
}
