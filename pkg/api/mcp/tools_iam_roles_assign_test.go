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
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// rolesAssignDeniedError is the denial the recording authorizer returns.
type rolesAssignDeniedError struct{}

func (e *rolesAssignDeniedError) Error() string { return "permission denied" }

// rolesAssignAuthorizer records the permissions it is asked for and the
// tenant bound to the context, and allows only when allow is set.
type rolesAssignAuthorizer struct {
	allow  bool
	asked  []string
	tenant string
}

func (a *rolesAssignAuthorizer) Authorize(ctx context.Context, _ *qrdbsdk.Identity, resource, action string) error {
	a.asked = append(a.asked, resource+":"+action)
	if tenant, ok := qrdbsdk.TenantFromContext(ctx); ok {
		a.tenant = tenant
	}
	if !a.allow {
		return &rolesAssignDeniedError{}
	}
	return nil
}

// rolesAssignFixture is a registry whose IAM service counts the writes that
// reach it.
type rolesAssignFixture struct {
	reg    *ToolRegistry
	writes int
}

func newRolesAssignFixture() *rolesAssignFixture {
	f := &rolesAssignFixture{}
	mock := &mockIAMService{
		createUserFunc: func(_ context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
			f.writes++
			return &service.IAMCreateUserResponse{User: &iam.User{ID: 1, TenantID: req.TenantID}, Timestamp: time.Now()}, nil
		},
		updateUserFunc: func(_ context.Context, req *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
			f.writes++
			return &service.IAMUpdateUserResponse{User: &iam.User{ID: req.UserID, TenantID: req.TenantID}, Timestamp: time.Now()}, nil
		},
	}
	f.reg = newIAMTestRegistry(mock)
	return f
}

func resolveTo(authz qrdbsdk.Authorizer) func() qrdbsdk.Authorizer {
	return func() qrdbsdk.Authorizer { return authz }
}

func callerContext() context.Context {
	return withCaller(context.Background())
}

// withCaller binds an authenticated caller identity to ctx.
func withCaller(ctx context.Context) context.Context {
	return qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{Subject: "mallory", TenantID: "acme"})
}

// TestMCP_UserWriteWithRolesRequiresRolesAssign proves a user create or update
// carrying roles asks for roles:assign in the target tenant and is refused
// without it, before the write: a caller holding only users:update must not
// write the Admin role into its own account.
func TestMCP_UserWriteWithRolesRequiresRolesAssign(t *testing.T) {
	tests := []struct {
		name string
		call func(r *ToolRegistry, ctx context.Context) (interface{}, error)
	}{
		{"create", func(r *ToolRegistry, ctx context.Context) (interface{}, error) {
			return r.executeIAMUserCreate(ctx, map[string]interface{}{
				"tenant_id": "acme", "email": "m@acme.test", "roles": []interface{}{"admin"},
			})
		}},
		{"update", func(r *ToolRegistry, ctx context.Context) (interface{}, error) {
			return r.executeIAMUserUpdate(ctx, map[string]interface{}{
				"tenant_id": "acme", "user_id": "1", "roles": []interface{}{"admin"},
			})
		}},
		{"update with an empty roles list", func(r *ToolRegistry, ctx context.Context) (interface{}, error) {
			return r.executeIAMUserUpdate(ctx, map[string]interface{}{
				"tenant_id": "acme", "user_id": "1", "roles": []interface{}{},
			})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name+" denied", func(t *testing.T) {
			f := newRolesAssignFixture()
			authz := &rolesAssignAuthorizer{}
			f.reg.SetRoleAssignmentAuthorizer(resolveTo(authz))

			_, err := tc.call(f.reg, callerContext())
			var denied *RoleAssignmentDeniedError
			require.ErrorAs(t, err, &denied)
			assert.Equal(t, []string{"roles:assign"}, authz.asked)
			assert.Equal(t, "acme", authz.tenant)
			assert.Zero(t, f.writes, "a denied role assignment reached the store")
		})
		t.Run(tc.name+" allowed", func(t *testing.T) {
			f := newRolesAssignFixture()
			f.reg.SetRoleAssignmentAuthorizer(resolveTo(&rolesAssignAuthorizer{allow: true}))

			_, err := tc.call(f.reg, callerContext())
			require.NoError(t, err)
			assert.Equal(t, 1, f.writes)
		})
		t.Run(tc.name+" fails closed", func(t *testing.T) {
			f := newRolesAssignFixture()

			_, err := tc.call(f.reg, callerContext())
			var denied *RoleAssignmentDeniedError
			require.ErrorAs(t, err, &denied, "no authorizer must refuse, not allow")
			assert.Zero(t, f.writes)

			f.reg.SetRoleAssignmentAuthorizer(resolveTo(&rolesAssignAuthorizer{allow: true}))
			_, err = tc.call(f.reg, context.Background())
			require.ErrorAs(t, err, &denied, "no identity must refuse")
			assert.Zero(t, f.writes)
		})
	}
}

// TestMCP_UserWriteWithoutRolesSkipsRolesAssign proves a write that carries no
// roles needs only the tool's own permission.
func TestMCP_UserWriteWithoutRolesSkipsRolesAssign(t *testing.T) {
	f := newRolesAssignFixture()
	authz := &rolesAssignAuthorizer{}
	f.reg.SetRoleAssignmentAuthorizer(resolveTo(authz))

	_, err := f.reg.executeIAMUserCreate(callerContext(), map[string]interface{}{"tenant_id": "acme", "email": "m@acme.test"})
	require.NoError(t, err)
	_, err = f.reg.executeIAMUserUpdate(callerContext(), map[string]interface{}{"tenant_id": "acme", "user_id": "1", "name": "M"})
	require.NoError(t, err)
	assert.Empty(t, authz.asked)
	assert.Equal(t, 2, f.writes)
}
