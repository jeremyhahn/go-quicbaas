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

import "testing"

// (d) The tenant-encoded id format round-trips: encode then decode recovers the
// owning tenant and the full id used as the storage-key suffix.
func TestTenantEncodedID_RoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		tenantID string
		raw      string
	}{
		{"uuid", "acme", "550e8400-e29b-41d4-a716-446655440000"},
		{"hex token", "tenant-2", "deadbeefcafef00d"},
		{"hyphenated tenant", "acme-corp", "abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := encodeTenantID(tc.tenantID, tc.raw)
			gotTenant, gotFull := decodeTenantID(id)
			if gotTenant != tc.tenantID {
				t.Fatalf("decoded tenant = %q, want %q", gotTenant, tc.tenantID)
			}
			if gotFull != id {
				t.Fatalf("decoded full id = %q, want %q", gotFull, id)
			}
		})
	}
}

// An empty tenant yields the raw id unchanged, which decodes to the system
// tenant (single-tenant / system callers).
func TestTenantEncodedID_EmptyTenant(t *testing.T) {
	id := encodeTenantID("", "raw-id")
	if id != "raw-id" {
		t.Fatalf("encodeTenantID(\"\", raw) = %q, want %q", id, "raw-id")
	}
	tenant, full := decodeTenantID(id)
	if tenant != systemTenantID {
		t.Fatalf("decoded tenant = %q, want %q", tenant, systemTenantID)
	}
	if full != "raw-id" {
		t.Fatalf("decoded full = %q, want %q", full, "raw-id")
	}
}

// A legacy id with no separator is treated as belonging to the system tenant
// (read-tolerant) and is returned unchanged so it remains lookup-able.
func TestTenantEncodedID_LegacyNoSeparator(t *testing.T) {
	tenant, full := decodeTenantID("legacy-uuid-no-dot")
	if tenant != systemTenantID {
		t.Fatalf("legacy tenant = %q, want %q", tenant, systemTenantID)
	}
	if full != "legacy-uuid-no-dot" {
		t.Fatalf("legacy full = %q, want %q", full, "legacy-uuid-no-dot")
	}
}

// ensureTenantEncoded is idempotent: re-encoding an already-encoded id does not
// accumulate prefixes (e.g. attempt-count re-saves).
func TestEnsureTenantEncoded_Idempotent(t *testing.T) {
	once := ensureTenantEncoded("acme", "raw")
	twice := ensureTenantEncoded("acme", once)
	if once != twice {
		t.Fatalf("ensureTenantEncoded not idempotent: %q vs %q", once, twice)
	}
	if tenant, _ := decodeTenantID(twice); tenant != "acme" {
		t.Fatalf("re-encoded tenant = %q, want acme", tenant)
	}
}
