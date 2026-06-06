//go:build iam

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

package providers

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// stubScopeResolver is a per-tenant scope resolver test double. It returns a
// configured scope per (tenant, permission); when sealed is set for a tenant,
// ResolveScope returns ErrTenantSealed to exercise the fail-closed path.
type stubScopeResolver struct {
	scopes map[string]map[permission.Permission]permission.Scope
	sealed map[string]bool
}

func newStubScopeResolver() *stubScopeResolver {
	return &stubScopeResolver{
		scopes: make(map[string]map[permission.Permission]permission.Scope),
		sealed: make(map[string]bool),
	}
}

func (s *stubScopeResolver) set(tenantID string, p permission.Permission, scope permission.Scope) {
	if s.scopes[tenantID] == nil {
		s.scopes[tenantID] = make(map[permission.Permission]permission.Scope)
	}
	s.scopes[tenantID][p] = scope
}

func (s *stubScopeResolver) ResolveScope(_ context.Context, tenantID string, p permission.Permission) (permission.Scope, error) {
	if s.sealed[tenantID] {
		return permission.ScopeSelf, qrdbtenant.ErrTenantSealed
	}
	if m, ok := s.scopes[tenantID]; ok {
		if scope, ok := m[p]; ok {
			return scope, nil
		}
	}
	// No per-tenant override: built-in default (ScopeSelf for unregistered).
	return permission.LookupScope(p), nil
}

// scopeTestProvider builds an RBACProvider whose role manager grants the
// "keys:write" custom permission to the "analyst" role, with the given
// scope resolver installed.
func scopeTestProvider(t *testing.T, resolver permission.ScopeResolver) *RBACProvider {
	t.Helper()
	rm := newMockRoleManager()
	// The mock grants the built-in "readwrite" role keys:read/write/delete;
	// we treat "keys:write" as the caller-defined permission whose scope is
	// configured per tenant by the resolver.
	p, err := NewRBACProvider(rm, nil)
	if err != nil {
		t.Fatalf("NewRBACProvider: %v", err)
	}
	return p.WithScopeResolver(resolver)
}

func analystRequest(subjectTenant string) *interfaces.AuthorizationRequest {
	return &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "u1",
			TenantID:      subjectTenant,
			Roles:         []string{"readwrite"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "write",
	}
}

// (d.1) A per-tenant ScopeSelf permission is admitted when the subject acts on
// its own tenant, and DENIED when the subject's tenant differs from the
// request's target tenant.
func TestRBACProvider_PerTenantScopeSelf(t *testing.T) {
	resolver := newStubScopeResolver()
	resolver.set("acme", "keys:write", permission.ScopeSelf)
	provider := scopeTestProvider(t, resolver)

	// Subject in acme, target acme -> allowed.
	ctx := iam.ContextWithTenant(context.Background(), "acme")
	dec, err := provider.Authorize(ctx, analystRequest("acme"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !dec.Allowed {
		t.Fatalf("self-scope same-tenant: Allowed=false, reason=%q", dec.Reason)
	}

	// Subject in acme, target other -> denied (self-scope, not system).
	ctx = iam.ContextWithTenant(context.Background(), "other")
	dec, err = provider.Authorize(ctx, analystRequest("acme"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if dec.Allowed {
		t.Fatal("self-scope cross-tenant must be denied")
	}
}

// (d.2) A per-tenant ScopeCross permission is admitted only for a system-tenant
// subject; a non-system subject is denied even on its own tenant.
func TestRBACProvider_PerTenantScopeCross(t *testing.T) {
	resolver := newStubScopeResolver()
	resolver.set(qrdbtenant.SystemTenantID, "keys:write", permission.ScopeCross)
	resolver.set("acme", "keys:write", permission.ScopeCross)
	provider := scopeTestProvider(t, resolver)

	// System subject, any target -> allowed.
	ctx := iam.ContextWithTenant(context.Background(), "acme")
	dec, err := provider.Authorize(ctx, analystRequest(qrdbtenant.SystemTenantID))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !dec.Allowed {
		t.Fatalf("cross-scope system subject: Allowed=false, reason=%q", dec.Reason)
	}

	// Non-system subject with a cross-scope permission -> denied.
	ctx = iam.ContextWithTenant(context.Background(), "acme")
	dec, err = provider.Authorize(ctx, analystRequest("acme"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if dec.Allowed {
		t.Fatal("cross-scope permission must be denied to a non-system subject")
	}
}

// (d.3) A sealed barrier tenant fails closed: scope resolution returns
// ErrTenantSealed and the otherwise-admitted permission is DENIED.
func TestRBACProvider_SealedTenantDeniesScope(t *testing.T) {
	resolver := newStubScopeResolver()
	resolver.set("acme", "keys:write", permission.ScopeSelf)
	resolver.sealed["acme"] = true
	provider := scopeTestProvider(t, resolver)

	ctx := iam.ContextWithTenant(context.Background(), "acme")
	dec, err := provider.Authorize(ctx, analystRequest("acme"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if dec.Allowed {
		t.Fatal("sealed tenant scope resolution must deny (fail closed)")
	}
	if dec.Reason == "" {
		t.Fatal("expected a deny reason citing scope resolution failure")
	}
}

// Back-compat: with no scope resolver installed, the provider keeps its
// scope-unaware behaviour — an RBAC-granted permission is admitted regardless
// of tenant.
func TestRBACProvider_NoScopeResolverBackCompat(t *testing.T) {
	rm := newMockRoleManager()
	provider, err := NewRBACProvider(rm, nil)
	if err != nil {
		t.Fatalf("NewRBACProvider: %v", err)
	}

	ctx := iam.ContextWithTenant(context.Background(), "other")
	dec, err := provider.Authorize(ctx, analystRequest("acme"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !dec.Allowed {
		t.Fatalf("no-resolver back-compat: Allowed=false, reason=%q", dec.Reason)
	}
}

// A scope resolver that fails must never leak through BatchAuthorize either.
func TestRBACProvider_BatchScopeFailClosed(t *testing.T) {
	resolver := newStubScopeResolver()
	resolver.sealed["acme"] = true
	provider := scopeTestProvider(t, resolver)

	ctx := iam.ContextWithTenant(context.Background(), "acme")
	decs, err := provider.BatchAuthorize(ctx, []*interfaces.AuthorizationRequest{analystRequest("acme")})
	if err != nil {
		t.Fatalf("BatchAuthorize: %v", err)
	}
	if len(decs) != 1 || decs[0].Allowed {
		t.Fatalf("batch sealed-tenant scope must deny: %+v", decs)
	}
}
