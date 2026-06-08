//go:build quic && iam

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

package quic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func TestGetIAMQRDBPermission_Mapped(t *testing.T) {
	cases := []struct {
		opcode   byte
		resource string
		action   string
	}{
		{OpIAMCreateTenant, "tenants", "create"},
		{OpIAMGetTenant, "tenants", "read"},
		{OpIAMListTenants, "tenants", "list"},
		{OpIAMUpdateTenant, "tenants", "update"},
		{OpIAMDeleteTenant, "tenants", "delete"},
		{OpIAMCreateUser, "users", "create"},
		{OpIAMGetUser, "users", "read"},
		{OpIAMListUsers, "users", "list"},
		{OpIAMDeleteUser, "users", "delete"},
		{OpIAMCreateRole, "roles", "create"},
		{OpIAMListRoles, "roles", "list"},
		{OpIAMAssignRole, "roles", "assign"},
		{OpIAMRevokeRole, "roles", "unassign"},
	}
	for _, c := range cases {
		res, act, ok := getIAMQRDBPermission(c.opcode)
		require.True(t, ok, "opcode 0x%02X", c.opcode)
		assert.Equal(t, c.resource, res, "opcode 0x%02X", c.opcode)
		assert.Equal(t, c.action, act, "opcode 0x%02X", c.opcode)

		// And the top-level getQRDBPermission must delegate to it.
		r2, a2, ok2 := getQRDBPermission(c.opcode)
		require.True(t, ok2)
		assert.Equal(t, c.resource, r2)
		assert.Equal(t, c.action, a2)
	}
}

func TestGetIAMQRDBPermission_Unmapped(t *testing.T) {
	// IAM group/app/policy have no qrdb permission family -> unmapped.
	for _, op := range []byte{OpIAMCreateGroup, OpIAMRegisterApp, OpIAMCreatePolicy} {
		_, _, ok := getIAMQRDBPermission(op)
		assert.False(t, ok, "opcode 0x%02X should be unmapped", op)
	}
}

// TestSetAuthzEnforcement verifies the QUIC server setter installs the validator
// and authz service so the next request's handler enforces them.
func TestSetAuthzEnforcement(t *testing.T) {
	srv := &Server{}
	validator := &fakeQUICValidator{token: "good", id: &AuthzIdentity{ID: "u1", TenantID: "system"}}
	authz := &fakeQUICAuthzService{allowResource: "kv", allowAction: "put"}

	srv.SetAuthzEnforcement(validator, authz)

	var got interfaces.AuthorizationService = srv.authzService
	require.NotNil(t, got)
	require.NotNil(t, srv.authzTokenValidator)
}
