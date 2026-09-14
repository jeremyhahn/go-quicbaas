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

package iam

import (
	"strings"
	"testing"
)

// =============================================================================
// go-quicbaas #644, first defect — a minted client id must be parseable back
// into the tenant it names.
//
// generateClientID built app_<tenant>_<suffix> with a base64url suffix, whose
// alphabet contains "_". TenantFromClientID splits on the LAST "_", so
// app_automatethethings_o9ufKgB_s9c parsed as tenant
// "automatethethings_o9ufKgB": roughly one issued client in six could never be
// resolved by the token endpoint, on any transport, forever.
// =============================================================================

// TestGenerateClientID_RoundTripsThroughTenantFromClientID is the regression.
// Every id the generator mints must parse back to the tenant it was minted for.
// The sample size makes the ~16% per-id failure rate a certainty, not a flake:
// a single "_" anywhere in a suffix fails the run.
func TestGenerateClientID_RoundTripsThroughTenantFromClientID(t *testing.T) {
	for _, tenantID := range []string{"automatethethings", "acme", "system", "with_underscore"} {
		t.Run(tenantID, func(t *testing.T) {
			for i := 0; i < 4096; i++ {
				clientID, err := generateClientID(tenantID)
				if err != nil {
					t.Fatalf("generateClientID: %v", err)
				}
				if got := TenantFromClientID(clientID); got != tenantID {
					t.Fatalf("TenantFromClientID(%q) = %q, want %q", clientID, got, tenantID)
				}
			}
		})
	}
}

// TestGenerateClientID_SuffixCarriesNoSeparator pins the property the round
// trip depends on: the separator character must not appear in the random
// suffix, or the last-separator split is ambiguous.
func TestGenerateClientID_SuffixCarriesNoSeparator(t *testing.T) {
	const tenantID = "acme"
	for i := 0; i < 4096; i++ {
		clientID, err := generateClientID(tenantID)
		if err != nil {
			t.Fatalf("generateClientID: %v", err)
		}
		suffix := strings.TrimPrefix(clientID, ClientIDPrefix+tenantID+ClientIDSeparator)
		if suffix == clientID {
			t.Fatalf("generateClientID(%q) = %q, which does not carry the app_<tenant>_ shape", tenantID, clientID)
		}
		if strings.Contains(suffix, ClientIDSeparator) {
			t.Fatalf("client id %q has a %q inside its random suffix %q", clientID, ClientIDSeparator, suffix)
		}
	}
}

// TestTenantFromClientID covers the parser's accepted and rejected shapes.
func TestTenantFromClientID(t *testing.T) {
	for _, tc := range []struct {
		name     string
		clientID string
		want     string
	}{
		{"generated shape", "app_automatethethings_QQnqeujdEo", "automatethethings"},
		{"tenant containing the separator", "app_with_underscore_QQnqeujdEo", "with_underscore"},
		{"no app prefix", "agent-automatethethings-node-mtls", ""},
		{"prefix but no separator", "app_automatethethings", ""},
		{"empty tenant segment", "app__QQnqeujdEo", ""},
		{"empty string", "", ""},
		{"prefix only", "app_", ""},
		{"separator only after prefix", "app__", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TenantFromClientID(tc.clientID); got != tc.want {
				t.Fatalf("TenantFromClientID(%q) = %q, want %q", tc.clientID, got, tc.want)
			}
		})
	}
}
