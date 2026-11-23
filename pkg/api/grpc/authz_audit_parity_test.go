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

// auditGrants is the grant REST requires for the route each AuditService RPC
// mirrors (pkg/api/rest/routes_permissions_generated.go). gRPC must take the
// same grant: an authenticated caller holding it is admitted, one without it
// is refused, whichever transport carried the call.
var auditGrants = map[string]methodPermission{
	// GET /api/v1/iam/audit
	"/quicbaas.iam.v1.AuditService/QueryAuditEvents": {"audit", "query"},
	// GET /api/v1/iam/audit/{id}
	"/quicbaas.iam.v1.AuditService/GetAuditEvent": {"audit", "read"},
}

func TestAuditGRPCResourceAndAction(t *testing.T) {
	for method, want := range auditGrants {
		t.Run(method, func(t *testing.T) {
			got, ok := grpcMethodPermissions[method]
			require.True(t, ok, "unmapped: under RequireAuthz the verb is refused for every caller")
			assert.Equal(t, want, got)

			resource, action, ok := grpcResourceAndAction(method)
			require.True(t, ok)
			assert.Equal(t, want.resource, resource)
			assert.Equal(t, want.action, action)
		})
	}
}

func TestAuditGRPCAuthorization(t *testing.T) {
	for method, want := range auditGrants {
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

// TestAuditGRPCRecordStaysUnmapped pins RecordAuditEvent as unmapped, so it
// stays default-denied: no REST grant is copied onto it.
func TestAuditGRPCRecordStaysUnmapped(t *testing.T) {
	_, mapped := grpcMethodPermissions["/quicbaas.iam.v1.AuditService/RecordAuditEvent"]
	assert.False(t, mapped, "RecordAuditEvent must carry no grant")
}
