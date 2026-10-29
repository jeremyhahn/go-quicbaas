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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

func bearerContext(token string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}

// TestAuthorizeGRPCArguments_UserWriteWithRolesRequiresRolesAssign proves an
// IAM user create or update carrying roles needs roles:assign on top of the
// method's own permission, and that one without roles does not.
func TestAuthorizeGRPCArguments_UserWriteWithRolesRequiresRolesAssign(t *testing.T) {
	tests := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"create", iampb.IAMService_CreateUser_FullMethodName, &iampb.CreateUserRequest{Roles: []string{"admin"}}},
		{"update", iampb.IAMService_UpdateUser_FullMethodName, &iampb.UpdateUserRequest{Roles: []string{"admin"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name+" without roles:assign is denied", func(t *testing.T) {
			authz := &scopeSetAuthorizer{token: "tok", scp: map[string]bool{"users:update": true, "users:create": true}}
			err := authorizeGRPCArguments(bearerContext("tok"), tc.method, tc.req, authz)
			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
		})
		t.Run(tc.name+" with roles:assign is allowed", func(t *testing.T) {
			authz := &scopeSetAuthorizer{token: "tok", scp: map[string]bool{"roles:assign": true}}
			require.NoError(t, authorizeGRPCArguments(bearerContext("tok"), tc.method, tc.req, authz))
		})
		t.Run(tc.name+" without a token is unauthenticated", func(t *testing.T) {
			authz := &scopeSetAuthorizer{token: "tok", scp: map[string]bool{"roles:assign": true}}
			err := authorizeGRPCArguments(context.Background(), tc.method, tc.req, authz)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}
}

// TestAuthorizeGRPCArguments_NoExtraPermission proves a request without roles,
// and a method with no argument rule, ask the authorizer for nothing.
func TestAuthorizeGRPCArguments_NoExtraPermission(t *testing.T) {
	authz := &scopeSetAuthorizer{token: "tok", scp: map[string]bool{}}
	ctx := bearerContext("tok")

	require.NoError(t, authorizeGRPCArguments(ctx, iampb.IAMService_CreateUser_FullMethodName, &iampb.CreateUserRequest{}, authz))
	require.NoError(t, authorizeGRPCArguments(ctx, iampb.IAMService_UpdateUser_FullMethodName, &iampb.UpdateUserRequest{Roles: []string{}}, authz))
	require.NoError(t, authorizeGRPCArguments(ctx, "/quicbaas.iam.v1.IAMService/GetUser", &iampb.CreateUserRequest{Roles: []string{"admin"}}, authz))
	assert.Nil(t, grpcArgumentPermissionsFor(iampb.IAMService_CreateUser_FullMethodName, struct{}{}))
}

// TestUnaryAuthorizationInterceptor_UserWriteMappedAndRolesAssignReached
// proves IAMService CreateUser and UpdateUser are mapped to the grants their
// REST twins require, so the interceptor reaches the roles:assign argument
// check instead of default-denying the method (go-quicbaas#2064).
func TestUnaryAuthorizationInterceptor_UserWriteMappedAndRolesAssignReached(t *testing.T) {
	assert.Equal(t, methodPermission{"users", "create"}, grpcMethodPermissions[iampb.IAMService_CreateUser_FullMethodName])
	assert.Equal(t, methodPermission{"users", "update"}, grpcMethodPermissions[iampb.IAMService_UpdateUser_FullMethodName])

	tests := []struct {
		name    string
		scp     map[string]bool
		req     interface{}
		allowed bool
	}{
		{"users:create without roles:assign is refused", map[string]bool{"users:create": true}, &iampb.CreateUserRequest{Roles: []string{"admin"}}, false},
		{"users:create with roles:assign is allowed", map[string]bool{"users:create": true, "roles:assign": true}, &iampb.CreateUserRequest{Roles: []string{"admin"}}, true},
		{"users:create without roles in the request is allowed", map[string]bool{"users:create": true}, &iampb.CreateUserRequest{}, true},
	}
	info := &grpc.UnaryServerInfo{FullMethod: iampb.IAMService_CreateUser_FullMethodName}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }
			authz := &scopeSetAuthorizer{token: "tok", scp: tc.scp}

			_, err := UnaryAuthorizationInterceptor(authz)(bearerContext("tok"), tc.req, info, handler)
			assert.Equal(t, tc.allowed, called)
			if tc.allowed {
				require.NoError(t, err)
				return
			}
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
		})
	}
}
