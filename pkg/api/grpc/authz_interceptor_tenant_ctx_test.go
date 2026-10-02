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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// TestUnaryAuthorizationInterceptor_InjectsTenantContext asserts that a
// successful validator-allow binds the tenant under go-qrdb's central key
// (tenantctx.WithTenant) in addition to the authenticated Subject, so the
// service / storage layer resolves row-level isolation from tenantctx — the one
// tenant source — and not just from auth.SubjectFromContext().TenantID. This
// matches the REST and MCP validator-allow paths.
func TestUnaryAuthorizationInterceptor_InjectsTenantContext(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{
		token:       "good",
		grantedPerm: "kv:put",
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "u1",
			Username: "alice",
			TenantID: "tenant-a",
			Roles:    []string{"admin"},
			Scopes:   []string{"kv:put"},
		},
	}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	var sawTenant string
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		sawTenant = boundTenant(ctx)
		return "ok", nil
	}

	resp, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, "tenant-a", sawTenant,
		"tenant must be bound under the central key after a successful authorize")
}

// TestUnaryAuthorizationInterceptor_EmptyTenantNoContextKey asserts the negative
// case: when the verified AuthorizedContext carries no tenant, no tenant is
// bound — and the services then refuse the request instead of reading the
// absence as system reach (TestLegacyListener_TokenWithoutTenantFailsClosed).
// The authenticated Subject is still injected.
func TestUnaryAuthorizationInterceptor_EmptyTenantNoContextKey(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{
		token:       "good",
		grantedPerm: "kv:put",
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "u1",
			Username: "alice",
			TenantID: "", // no tenant
			Roles:    []string{"admin"},
			Scopes:   []string{"kv:put"},
		},
	}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	var sawTenant string
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		sawTenant = boundTenant(ctx)
		return "ok", nil
	}

	resp, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, "", sawTenant,
		"no tenant context key must be set when TenantID is empty")
}
