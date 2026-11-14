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

//go:build !integration

package grpc

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

const approveUserMethod = "/quicbaas.iam.v1.IAMService/ApproveUser"

// newApproveStack serves IAMService over a real identity store holding one
// pending user of tenant acme, and returns that user's id.
func newApproveStack(t *testing.T) (*IAMServer, *iam.IdentityStore, uint64) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mem := iam.NewMemoryStorage()
	tenants := iam.NewDefaultTenantManager(mem)
	_, err = tenants.CreateTenant(callerContext("acme"), iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem, Tenants: tenants})

	pending, err := store.CreateUser(callerContext("acme"), "acme", iam.CreateUserOptions{
		Email: "pat@acme.test", Username: "pat", Name: "Pat", Lifecycle: iam.LifecyclePending,
	})
	require.NoError(t, err)
	require.Equal(t, iam.LifecyclePending, pending.Lifecycle)

	return NewIAMServer(NewIAMServiceAdapter(tenants, store, nil, nil, nil, nil), log), store, pending.ID
}

func TestApproveUser_PendingBecomesActive(t *testing.T) {
	srv, store, userID := newApproveStack(t)

	resp, err := srv.ApproveUser(callerContext("acme"), &iampb.ApproveUserRequest{
		TenantId: "acme", UserId: strconv.FormatUint(userID, 10),
	})
	require.NoError(t, err)
	assert.Equal(t, string(iam.LifecycleActive), resp.GetUser().GetLifecycle())

	stored, err := store.GetUser(callerContext("acme"), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, stored.Lifecycle)
}

func TestApproveUser_UnknownUserIsNotFound(t *testing.T) {
	srv, _, userID := newApproveStack(t)

	_, err := srv.ApproveUser(callerContext("acme"), &iampb.ApproveUserRequest{
		TenantId: "acme", UserId: strconv.FormatUint(userID+1000, 10),
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestApproveUser_RejectsBadRequests(t *testing.T) {
	srv, store, userID := newApproveStack(t)
	id := strconv.FormatUint(userID, 10)

	for name, tc := range map[string]struct {
		ctx  context.Context
		req  *iampb.ApproveUserRequest
		code codes.Code
	}{
		"no tenant":      {callerContext("acme"), &iampb.ApproveUserRequest{UserId: id}, codes.InvalidArgument},
		"no user":        {callerContext("acme"), &iampb.ApproveUserRequest{TenantId: "acme"}, codes.InvalidArgument},
		"bad user id":    {callerContext("acme"), &iampb.ApproveUserRequest{TenantId: "acme", UserId: "pat"}, codes.InvalidArgument},
		"another tenant": {callerContext("other"), &iampb.ApproveUserRequest{TenantId: "acme", UserId: id}, codes.PermissionDenied},
	} {
		_, err := srv.ApproveUser(tc.ctx, tc.req)
		assert.Equal(t, tc.code, status.Code(err), name)
	}

	stored, err := store.GetUser(callerContext("acme"), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecyclePending, stored.Lifecycle, "a refused approve must leave the user pending")
}

// ApproveUser carries REST's users:enable grant through the real
// authorization interceptor, and UpdateUser stays unmapped.
func TestApproveUser_Authorization(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: approveUserMethod}
	for _, tc := range []struct {
		granted string
		allowed bool
	}{
		{granted: "users:enable", allowed: true},
		{granted: "users:read", allowed: false},
	} {
		t.Run(tc.granted, func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: tc.granted,
				authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "t1"},
			}
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }

			_, err := UnaryAuthorizationInterceptor(authorizer)(ctxWithBearer("good"), nil, info, handler)
			assert.Equal(t, "users:enable", authorizer.lastPerm)
			assert.Equal(t, tc.allowed, called)
			if tc.allowed {
				require.NoError(t, err)
				return
			}
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
		})
	}

	assert.Equal(t, methodPermission{"users", "update"}, grpcMethodPermissions["/quicbaas.iam.v1.IAMService/UpdateUser"],
		"UpdateUser takes users:update, never users:enable; approving goes through ApproveUser")
}
