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

// auditRPCPermissions are the gRPC audit RPCs with the permission REST and
// MCP require for the same verb (#1751).
var auditRPCPermissions = []struct {
	method string
	perm   string
}{
	{"/quicbaas.iam.v1.AuditService/QueryAuditEvents", "audit:query"},
	{"/quicbaas.iam.v1.AuditService/GetAuditEvent", "audit:read"},
	{"/quicbaas.iam.v1.AuditService/RecordAuditEvent", "audit_events:record"},
}

// TestAuditRPCs_HolderOfPermissionIsAllowed proves a caller holding the
// audit permission reaches each audit RPC's handler on gRPC.
func TestAuditRPCs_HolderOfPermissionIsAllowed(t *testing.T) {
	for _, tc := range auditRPCPermissions {
		t.Run(tc.method, func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: tc.perm,
				authzCtx:    &platformtoken.AuthorizedContext{Subject: "auditor", TenantID: "tenant-a"},
			}
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return "ok", nil }

			resp, err := UnaryAuthorizationInterceptor(authorizer)(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: tc.method}, handler)
			require.NoError(t, err)
			assert.Equal(t, "ok", resp)
			assert.True(t, called, "a caller holding %s must reach the handler", tc.perm)
			assert.Equal(t, tc.perm, authorizer.lastPerm)
		})
	}
}

// TestAuditRPCs_CallerWithoutPermissionIsDenied proves a caller without the
// audit permission is refused before the handler runs.
func TestAuditRPCs_CallerWithoutPermissionIsDenied(t *testing.T) {
	for _, tc := range auditRPCPermissions {
		t.Run(tc.method, func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: "kv:get",
				authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "tenant-a"},
			}
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }

			_, err := UnaryAuthorizationInterceptor(authorizer)(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: tc.method}, handler)
			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.False(t, called, "a caller without %s must not reach the handler", tc.perm)
		})
	}
}
