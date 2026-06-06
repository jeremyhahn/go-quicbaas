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

package iam

import (
	"context"
	"errors"
	"testing"
)

// Scenario (d): a tenant self-registers a domain in the SHARED routing
// directory; a browser/WebAuthn login (no SPIFFE SAN) resolves its tenant from
// the email domain. The directory is backed by the shared store, which is the
// control-plane store readable regardless of any tenant barrier state — so the
// resolution has no chicken-and-egg dependency on the target tenant's barrier.
func TestHomeRealm_DomainResolvesBrowserLogin(t *testing.T) {
	shared := newMockKVStore() // stands in for the shared/system control-plane store
	dir, err := NewHomeRealmDirectory(shared)
	if err != nil {
		t.Fatalf("NewHomeRealmDirectory: %v", err)
	}
	ctx := context.Background()

	// Tenant self-service registration (writes ONLY to the shared store).
	if err := dir.RegisterDomain(ctx, "tenant-acme", "acme.example"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}

	// Browser login presents only an email; HRD resolves the tenant.
	tenantID, err := dir.Resolve(ctx, RealmHints{Email: "alice@acme.example"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tenantID != "tenant-acme" {
		t.Fatalf("tenant = %q, want tenant-acme", tenantID)
	}

	// The routing entry holds ONLY the tenant ID — no secrets/PII.
	val, _ := shared.Get(ctx, hrdDomainKey("acme.example"))
	if string(val) != "tenant-acme" {
		t.Fatalf("routing entry value = %q, want bare tenant id", string(val))
	}
}

// Scenario (d): SNI/host resolution, including a subdomain falling back to the
// registered parent domain.
func TestHomeRealm_HostAndSubdomain(t *testing.T) {
	shared := newMockKVStore()
	dir, _ := NewHomeRealmDirectory(shared)
	ctx := context.Background()

	if err := dir.RegisterHost(ctx, "tenant-b", "portal.tenant-b.io"); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}
	if err := dir.RegisterDomain(ctx, "tenant-c", "tenant-c.io"); err != nil {
		t.Fatalf("RegisterDomain: %v", err)
	}

	got, err := dir.Resolve(ctx, RealmHints{SNIHost: "portal.tenant-b.io"})
	if err != nil || got != "tenant-b" {
		t.Fatalf("host resolve = %q, %v", got, err)
	}

	// Subdomain falls back to the registered parent domain.
	got, err = dir.Resolve(ctx, RealmHints{SNIHost: "app.tenant-c.io"})
	if err != nil || got != "tenant-c" {
		t.Fatalf("subdomain resolve = %q, %v", got, err)
	}
}

// Fail-closed: an unregistered login resolves to no tenant.
func TestHomeRealm_FailClosed(t *testing.T) {
	shared := newMockKVStore()
	dir, _ := NewHomeRealmDirectory(shared)
	ctx := context.Background()

	_, err := dir.Resolve(ctx, RealmHints{Email: "x@unknown.example", SNIHost: "nope.example"})
	if !errors.As(err, new(*HRDNotFoundError)) {
		t.Fatalf("expected HRDNotFoundError, got %v", err)
	}
}

// An explicit tenant hint is honored only when it matches a registered entry —
// a caller cannot assert an arbitrary tenant.
func TestHomeRealm_ExplicitHintMustMatchRegistration(t *testing.T) {
	shared := newMockKVStore()
	dir, _ := NewHomeRealmDirectory(shared)
	ctx := context.Background()

	_ = dir.RegisterDomain(ctx, "tenant-real", "real.example")

	// Hint matches a registered entry -> accepted.
	got, err := dir.Resolve(ctx, RealmHints{TenantHint: "tenant-real", Email: "u@real.example"})
	if err != nil || got != "tenant-real" {
		t.Fatalf("matching hint = %q, %v", got, err)
	}

	// Spoofed hint with no matching registration -> fail closed.
	_, err = dir.Resolve(ctx, RealmHints{TenantHint: "tenant-evil", Email: "u@real.example"})
	if got, _ := dir.Resolve(ctx, RealmHints{TenantHint: "tenant-evil", Email: "u@real.example"}); got == "tenant-evil" {
		t.Fatal("spoofed hint must not resolve to the asserted tenant")
	}
	// It should instead resolve via the email domain to the real owner.
	if err != nil {
		t.Fatalf("expected domain fallback resolution, got %v", err)
	}
}

// Registering a domain already owned by another tenant is rejected.
func TestHomeRealm_DomainConflict(t *testing.T) {
	shared := newMockKVStore()
	dir, _ := NewHomeRealmDirectory(shared)
	ctx := context.Background()

	if err := dir.RegisterDomain(ctx, "tenant-a", "shared.example"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := dir.RegisterDomain(ctx, "tenant-b", "shared.example")
	if !errors.As(err, new(*HRDConflictError)) {
		t.Fatalf("expected HRDConflictError, got %v", err)
	}

	// Re-registering by the same owner is idempotent.
	if err := dir.RegisterDomain(ctx, "tenant-a", "shared.example"); err != nil {
		t.Fatalf("idempotent re-register: %v", err)
	}
}

// Unregister is owner-gated.
func TestHomeRealm_UnregisterOwnerGated(t *testing.T) {
	shared := newMockKVStore()
	dir, _ := NewHomeRealmDirectory(shared)
	ctx := context.Background()

	_ = dir.RegisterDomain(ctx, "tenant-a", "owned.example")

	if err := dir.UnregisterDomain(ctx, "tenant-b", "owned.example"); !errors.As(err, new(*HRDConflictError)) {
		t.Fatalf("non-owner unregister should conflict, got %v", err)
	}
	if err := dir.UnregisterDomain(ctx, "tenant-a", "owned.example"); err != nil {
		t.Fatalf("owner unregister: %v", err)
	}
	if _, err := dir.Resolve(ctx, RealmHints{Email: "u@owned.example"}); !errors.As(err, new(*HRDNotFoundError)) {
		t.Fatalf("entry should be gone after unregister, got %v", err)
	}
}
