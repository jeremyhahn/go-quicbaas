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
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// federationSessionGrants is the grant REST requires for the route each
// FederationService and SessionService RPC mirrors
// (pkg/api/rest/routes_permissions_generated.go). gRPC must take the same
// grant: an authenticated caller holding it is admitted, one without it is
// refused, whichever transport carried the call (go-quicbaas#1624).
var federationSessionGrants = map[string]methodPermission{
	// POST /api/v1/tenants/{tenant_id}/providers/
	"/quicbaas.iam.v1.FederationService/CreateProvider": {"tenants", "create"},
	// GET /api/v1/tenants/{tenant_id}/providers/{provider_id}
	"/quicbaas.iam.v1.FederationService/GetProvider": {"tenants", "read"},
	// GET /api/v1/tenants/{tenant_id}/providers/
	"/quicbaas.iam.v1.FederationService/ListProviders": {"tenants", "read"},
	// PUT /api/v1/tenants/{tenant_id}/providers/{provider_id}
	"/quicbaas.iam.v1.FederationService/UpdateProvider": {"tenants", "create"},
	// DELETE /api/v1/tenants/{tenant_id}/providers/{provider_id}
	"/quicbaas.iam.v1.FederationService/DeleteProvider": {"tenants", "delete"},
	// POST /api/v1/tenants/{tenant_id}/users/{user_id}/links/
	"/quicbaas.iam.v1.FederationService/LinkAccount": {"users", "update"},
	// GET /api/v1/tenants/{tenant_id}/users/{user_id}/links/
	"/quicbaas.iam.v1.FederationService/GetUserLinks": {"users", "read"},
	// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/links/{provider_id}
	"/quicbaas.iam.v1.FederationService/UnlinkAccount": {"users", "update"},
	// GET /api/v1/iam/sessions
	"/quicbaas.iam.v1.SessionService/ListSessions": {"users", "read"},
	// GET /api/v1/iam/sessions/{id}
	"/quicbaas.iam.v1.SessionService/GetSession": {"users", "read"},
	// DELETE /api/v1/iam/sessions/{id}
	"/quicbaas.iam.v1.SessionService/RevokeSession": {"users", "update"},
	// DELETE /api/v1/iam/sessions
	"/quicbaas.iam.v1.SessionService/RevokeAllSessions": {"users", "update"},
	// POST /api/v1/iam/sessions/revoke-others
	"/quicbaas.iam.v1.SessionService/RevokeOtherSessions": {"users", "update"},
}

func TestFederationSessionGRPCResourceAndAction(t *testing.T) {
	for method, want := range federationSessionGrants {
		t.Run(method, func(t *testing.T) {
			resource, action, ok := grpcResourceAndAction(method)
			require.True(t, ok, "unmapped: under RequireAuthz the verb is refused for every caller")
			assert.Equal(t, want.resource, resource)
			assert.Equal(t, want.action, action)
		})
	}
}

func TestFederationSessionGRPCAuthorization(t *testing.T) {
	for method, want := range federationSessionGrants {
		grant := want.resource + ":" + want.action
		info := &grpc.UnaryServerInfo{FullMethod: method}

		t.Run(method+"/granted", func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: grant,
				authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "t1"},
			}
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }

			_, err := UnaryAuthorizationInterceptor(authorizer)(ctxWithBearer("good"), nil, info, handler)
			require.NoError(t, err)
			assert.True(t, called, "a caller holding %s must reach the handler", grant)
			assert.Equal(t, grant, authorizer.lastPerm)
		})

		t.Run(method+"/not_granted", func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: "kv:get",
				authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "t1"},
			}
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }

			_, err := UnaryAuthorizationInterceptor(authorizer)(ctxWithBearer("good"), nil, info, handler)
			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.NotEqual(t, (&UnmappedMethodError{Method: method}).Error(), status.Convert(err).Message(),
				"refusal must come from the grant check, not from the method being unmapped")
			assert.Equal(t, grant, authorizer.lastPerm)
			assert.False(t, called, "handler must not run when authorization denies")
		})
	}
}

func TestFederatedLoginGRPCStaysPublic(t *testing.T) {
	methods := []string{
		"/quicbaas.iam.v1.FederationService/BeginFederatedLogin",
		"/quicbaas.iam.v1.FederationService/HandleFederatedCallback",
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			_, mapped := grpcMethodPermissions[method]
			assert.False(t, mapped, "a public method carries no grant")

			authorizer := &fakeValidatorAuthorizer{denyErr: &platformtoken.InvalidTokenError{Detail: "should never be called"}}
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }

			_, err := UnaryAuthorizationInterceptor(authorizer)(context.Background(), nil,
				&grpc.UnaryServerInfo{FullMethod: method}, handler)
			require.NoError(t, err)
			assert.True(t, called, "an anonymous caller must reach the handler")
		})
	}
}
