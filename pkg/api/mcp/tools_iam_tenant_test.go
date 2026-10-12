//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestIAMTools_RefuseACrossTenantRequest is the go-quicbaas#509 regression: a
// caller bound to one tenant that names another in tenant_id is refused with
// the typed tenantctx error before any IAM service is reached, for every tool
// family that reads tenant_id.
func TestIAMTools_RefuseACrossTenantRequest(t *testing.T) {
	t.Parallel()
	for name := range iamTenantScopedTools {
		t.Run(name, func(t *testing.T) {
			reached := false
			svc := &mockIAMService{listUsersFunc: func(context.Context, *service.IAMListUsersRequest) (*service.IAMListUsersResponse, error) {
				reached = true
				return &service.IAMListUsersResponse{}, nil
			}}
			reg := newIAMTestRegistry(svc)

			_, err := reg.executeIAMTool(tenantctx.WithTenant(context.Background(), "acme"), name,
				map[string]interface{}{"tenant_id": "globex"})

			var cross *tenantctx.CrossTenantError
			require.ErrorAs(t, err, &cross)
			assert.Equal(t, "acme", cross.CallerTenant)
			assert.Equal(t, "globex", cross.TargetTenant)
			assert.False(t, reached)
		})
	}
}

// TestIAMTools_RefuseTheSystemTenantNamingATenant: the system tenant gets no
// cross-tenant lift through tenant_id, matching tenantctx.Target.
func TestIAMTools_RefuseTheSystemTenantNamingATenant(t *testing.T) {
	t.Parallel()
	reg := newIAMTestRegistry(&mockIAMService{})

	_, err := reg.executeIAMTool(tenantctx.WithSystem(context.Background()), "quicbaas_iam_user_list",
		map[string]interface{}{"tenant_id": "acme"})

	var cross *tenantctx.CrossTenantError
	require.ErrorAs(t, err, &cross)
}

// TestIAMTools_RefuseAnUnboundCaller: a call with no tenant bound is refused
// whether or not it names one.
func TestIAMTools_RefuseAnUnboundCaller(t *testing.T) {
	t.Parallel()
	reg := newIAMTestRegistry(&mockIAMService{})

	_, err := reg.executeIAMTool(context.Background(), "quicbaas_iam_user_list",
		map[string]interface{}{"tenant_id": "acme"})

	var missing *tenantctx.MissingTenantError
	require.ErrorAs(t, err, &missing)
}

// TestIAMTools_ActForTheCallersTenant: naming the caller's own tenant, or none,
// reaches the service with the caller's tenant, and the caller's argument map is
// left untouched.
func TestIAMTools_ActForTheCallersTenant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"the caller's own tenant", map[string]interface{}{"tenant_id": "acme"}},
		{"no tenant named", map[string]interface{}{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			reg := newIAMTestRegistry(&mockIAMService{listUsersFunc: func(_ context.Context, req *service.IAMListUsersRequest) (*service.IAMListUsersResponse, error) {
				got = req.TenantID
				return &service.IAMListUsersResponse{}, nil
			}})
			wantArgs := len(tc.args)

			_, err := reg.executeIAMTool(tenantctx.WithTenant(context.Background(), "acme"), "quicbaas_iam_user_list", tc.args)

			require.NoError(t, err)
			assert.Equal(t, "acme", got)
			assert.Len(t, tc.args, wantArgs)
		})
	}
}

// TestScopeIAMTenant_PassesUnscopedToolsThrough: a tool that does not read
// tenant_id is not subject to the guard, so an unbound caller is not refused.
func TestScopeIAMTenant_PassesUnscopedToolsThrough(t *testing.T) {
	t.Parallel()
	args := map[string]interface{}{"tenant_id": "acme"}

	got, err := scopeIAMTenant(context.Background(), "quicbaas_mfa_status", args)

	require.NoError(t, err)
	assert.Equal(t, args, got)
}
