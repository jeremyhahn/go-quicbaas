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

package orchestration

import (
	"strings"
	"testing"
)

// =============================================================================
// The credential path scheme is EXPORTED as a pair (go-quicbaas#734) so a
// SecretsManager written to serve AccountSecretStore can invert the addressing
// instead of hardcoding it. That only holds if the two stay exact inverses,
// which is what these tests pin.
// =============================================================================

// TestAccountSecretPath_RoundTrips proves the pair is an exact inverse for the
// identifiers the store admits.
func TestAccountSecretPath_RoundTrips(t *testing.T) {
	cases := []struct{ tenantID, accountID string }{
		{"tenant-a", "acct-1"},
		{"6042debb-0000-4000-8000-000000000000", "acct_2"},
		{"t", "a"},
	}
	for _, tc := range cases {
		t.Run(tc.tenantID+"/"+tc.accountID, func(t *testing.T) {
			path, err := AccountSecretPath(tc.tenantID, tc.accountID)
			if err != nil {
				t.Fatalf("AccountSecretPath() error = %v", err)
			}
			if !strings.HasPrefix(path, accountSecretPathPrefix) {
				t.Fatalf("path %q is outside the account namespace", path)
			}
			tenantID, accountID, ok := ParseAccountSecretPath(path)
			if !ok {
				t.Fatalf("ParseAccountSecretPath(%q) reported the path unrecognized", path)
			}
			if tenantID != tc.tenantID || accountID != tc.accountID {
				t.Errorf("round trip = %q/%q, want %q/%q", tenantID, accountID, tc.tenantID, tc.accountID)
			}
		})
	}
}

// TestAccountSecretPath_RejectsUnusableSegments proves the constructor refuses
// an identifier that would smear across the path's own separator, which is what
// stops one account addressing another's envelope.
func TestAccountSecretPath_RejectsUnusableSegments(t *testing.T) {
	cases := []struct{ name, tenantID, accountID string }{
		{"empty tenant", "", "acct-1"},
		{"empty account", "tenant-a", ""},
		{"separator in tenant", "tenant/a", "acct-1"},
		{"separator in account", "tenant-a", "acct/1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AccountSecretPath(tc.tenantID, tc.accountID); err == nil {
				t.Fatalf("AccountSecretPath(%q, %q) accepted an unusable identifier", tc.tenantID, tc.accountID)
			}
		})
	}
}

// TestParseAccountSecretPath_RefusesAnythingElse proves the inverse is TOTAL:
// a path it does not own is reported as unrecognized rather than being split
// into whatever it happens to contain.
func TestParseAccountSecretPath_RefusesAnythingElse(t *testing.T) {
	for _, path := range []string{
		"",
		"orchestration/agent-accounts",
		accountSecretPathPrefix,
		accountSecretPathPrefix + "tenant-a",
		accountSecretPathPrefix + "/acct-1",
		accountSecretPathPrefix + "tenant-a/",
		accountSecretPathPrefix + "tenant-a/acct-1/extra",
		"some/other/namespace/tenant-a/acct-1",
	} {
		if _, _, ok := ParseAccountSecretPath(path); ok {
			t.Errorf("ParseAccountSecretPath(%q) accepted a path it does not address", path)
		}
	}
}

// TestAccountSecretStore_UsesTheExportedScheme proves the store itself
// addresses through the exported constructor, so a caller inverting the path
// finds the envelope the store actually wrote.
func TestAccountSecretStore_UsesTheExportedScheme(t *testing.T) {
	store := &AccountSecretStore{}
	got, err := store.path("tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("path() error = %v", err)
	}
	want, err := AccountSecretPath("tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("AccountSecretPath() error = %v", err)
	}
	if got != want {
		t.Errorf("store addresses %q, exported scheme says %q", got, want)
	}
}
