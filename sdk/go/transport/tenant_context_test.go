// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package transport

import (
	"context"
	"testing"
)

// TestTenantContext_RoundTrip verifies a tenant ID stored via ContextWithTenant
// is retrievable via TenantFromContext, and that overwriting replaces the value.
func TestTenantContext_RoundTrip(t *testing.T) {
	ctx := ContextWithTenant(context.Background(), "acme")
	if got := TenantFromContext(ctx); got != "acme" {
		t.Fatalf("expected tenant %q, got %q", "acme", got)
	}

	// Overwriting with a new tenant must shadow the previous value.
	ctx = ContextWithTenant(ctx, "globex")
	if got := TenantFromContext(ctx); got != "globex" {
		t.Fatalf("expected tenant %q after overwrite, got %q", "globex", got)
	}

	// The empty string is a valid (if unusual) tenant value and round-trips.
	ctx = ContextWithTenant(context.Background(), "")
	if got := TenantFromContext(ctx); got != "" {
		t.Fatalf("expected empty tenant, got %q", got)
	}
}

// TestTenantFromContext_Missing ensures TenantFromContext returns the empty
// string when no tenant was ever set, including when the value under the key
// is of an unexpected type.
func TestTenantFromContext_Missing(t *testing.T) {
	if got := TenantFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty tenant for background context, got %q", got)
	}

	// A value of the wrong type under the tenant key must not be returned;
	// the comma-ok type assertion should yield the empty string.
	ctx := context.WithValue(context.Background(), tenantContextKey{}, 42)
	if got := TenantFromContext(ctx); got != "" {
		t.Fatalf("expected empty tenant for non-string value, got %q", got)
	}
}
