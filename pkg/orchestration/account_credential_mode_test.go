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

import "testing"

// TestAccountCredentialMode_Canonical proves both spellings of each mode —
// the domain one and the transport alias — resolve to the same canonical
// value. A decision that compared against only one spelling would silently
// stop refreshing accounts written with the other.
func TestAccountCredentialMode_Canonical(t *testing.T) {
	tests := []struct {
		name      string
		mode      AccountCredentialMode
		canonical AccountCredentialMode
		oauth     bool
		static    bool
	}{
		{"domain oauth spelling", AccountCredentialModeOAuthRefresh, AccountCredentialModeOAuthRefresh, true, false},
		{"transport oauth alias", AccountRefreshOAuth2, AccountCredentialModeOAuthRefresh, true, false},
		{"domain static spelling", AccountCredentialModeStaticToken, AccountCredentialModeStaticToken, false, true},
		{"transport static alias", AccountRefreshNone, AccountCredentialModeStaticToken, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canonical, ok := tt.mode.Canonical()
			if !ok {
				t.Fatalf("Canonical(%q) ok = false, want true", tt.mode)
			}
			if canonical != tt.canonical {
				t.Fatalf("Canonical(%q) = %q, want %q", tt.mode, canonical, tt.canonical)
			}
			if got := tt.mode.IsOAuthRefresh(); got != tt.oauth {
				t.Fatalf("IsOAuthRefresh(%q) = %v, want %v", tt.mode, got, tt.oauth)
			}
			if got := tt.mode.IsStaticToken(); got != tt.static {
				t.Fatalf("IsStaticToken(%q) = %v, want %v", tt.mode, got, tt.static)
			}
		})
	}
}

// TestAccountCredentialMode_Canonical_Unrecognized proves an empty or
// misspelled mode is reported as unusable rather than being guessed at.
func TestAccountCredentialMode_Canonical_Unrecognized(t *testing.T) {
	for _, mode := range []AccountCredentialMode{"", "oauth", "OAUTH2", "refresh", "static"} {
		t.Run(string(mode), func(t *testing.T) {
			if _, ok := mode.Canonical(); ok {
				t.Fatalf("Canonical(%q) ok = true, want false", mode)
			}
			if mode.IsOAuthRefresh() || mode.IsStaticToken() {
				t.Fatalf("mode %q must be neither oauth nor static", mode)
			}
		})
	}
}
