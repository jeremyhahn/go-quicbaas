//go:build quic

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
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// TestAuthorizeViaValidator_InjectsTenantContext asserts that a successful
// validator-allow injects the tenant context key (iam.ContextWithTenant) in
// addition to the authenticated Subject, so the service / storage layer resolves
// row-level isolation from iam.TenantFromContext — the canonical RLS source — and
// not just from auth.SubjectFromContext().TenantID. This matches the REST and MCP
// validator-allow paths.
func TestAuthorizeViaValidator_InjectsTenantContext(t *testing.T) {
	v := &fakeValidatorAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "user-1",
			Username: "alice",
			TenantID: "tenant-a",
			Roles:    []string{"admin"},
		},
	}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	ctx, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp != nil {
		t.Fatalf("expected allow, got %v", resp)
	}

	if got := iam.TenantFromContext(ctx); got != "tenant-a" {
		t.Fatalf("tenant must be resolvable from iam.TenantFromContext, got %q", got)
	}
}

// TestAuthorizeViaValidator_EmptyTenantNoContextKey asserts the negative case:
// when the verified AuthorizedContext carries no tenant, the tenant context key
// is NOT set, preserving single-tenant (legacy) behavior.
func TestAuthorizeViaValidator_EmptyTenantNoContextKey(t *testing.T) {
	v := &fakeValidatorAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "user-1",
			Username: "alice",
			TenantID: "", // no tenant
			Roles:    []string{"admin"},
		},
	}
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithHandlerValidatorAuthorizer(v))

	ctx, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpGet, Token: "tok"})
	if resp != nil {
		t.Fatalf("expected allow, got %v", resp)
	}

	if got := iam.TenantFromContext(ctx); got != "" {
		t.Fatalf("no tenant context key must be set when TenantID is empty, got %q", got)
	}
}
