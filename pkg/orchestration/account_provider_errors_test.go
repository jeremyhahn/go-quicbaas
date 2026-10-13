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
	"errors"
	"strings"
	"testing"
)

// TestAccountProviderErrors_MessagesAndSentinels checks each new error prints
// the detail an operator needs to act on and unwraps to its sentinel. An error
// that names no provider (or no remedy) turns a write rejection into a support
// ticket.
func TestAccountProviderErrors_MessagesAndSentinels(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		sentinel error
		contains []string
	}{
		{
			name:     "credential mode invalid",
			err:      &AccountCredentialModeInvalidError{Mode: "sometimes"},
			sentinel: ErrAccountCredentialModeInvalid,
			contains: []string{"sometimes"},
		},
		{
			name:     "endpoint unresolved",
			err:      &AccountProviderEndpointUnresolvedError{Provider: AccountProviderClaude},
			sentinel: ErrAccountProviderEndpointUnresolved,
			contains: []string{"provider=claude", "orchestration.account_providers"},
		},
		{
			name:     "refresh token required",
			err:      &AccountRefreshTokenRequiredError{Provider: AccountProviderCodex},
			sentinel: ErrAccountRefreshTokenRequired,
			contains: []string{"provider=codex"},
		},
		{
			name:     "static token refreshable",
			err:      &AccountStaticTokenRefreshableError{Provider: AccountProviderClaude},
			sentinel: ErrAccountStaticTokenRefreshable,
			contains: []string{"provider=claude", "refresh token"},
		},
		{
			name:     "empty credential",
			err:      &AccountCredentialEmptyError{Provider: AccountProviderCodex},
			sentinel: ErrAccountCredentialEmpty,
			contains: []string{"provider=codex", "no access token, env or files"},
		},
		{
			name:     "duplicate provider",
			err:      &AccountProviderDuplicateError{Provider: "acme-llm"},
			sentinel: ErrAccountProviderDuplicate,
			contains: []string{"provider=acme-llm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.sentinel) {
				t.Fatalf("errors.Is(%v, %v) = false", tt.err, tt.sentinel)
			}
			for _, want := range tt.contains {
				if !strings.Contains(tt.err.Error(), want) {
					t.Fatalf("Error() = %q, want it to contain %q", tt.err.Error(), want)
				}
			}
		})
	}
}

// TestAccountProviderRegistry_NilReceiver proves the accessors are safe on a
// nil registry and, crucially, that a nil registry invents no defaults rather
// than quietly substituting the built-ins.
func TestAccountProviderRegistry_NilReceiver(t *testing.T) {
	var registry *AccountProviderRegistry

	if _, ok := registry.Descriptor(AccountProviderClaude); ok {
		t.Fatalf("Descriptor() on a nil registry returned a descriptor")
	}
	if got := registry.Providers(); got != nil {
		t.Fatalf("Providers() = %v, want nil", got)
	}
	if got := registry.OAuthCapableProviders(); got != nil {
		t.Fatalf("OAuthCapableProviders() = %v, want nil", got)
	}

	cred := &AccountCredential{AccessToken: "at"}
	if got := registry.Effective(AccountProviderClaude, cred); got != cred {
		t.Fatalf("Effective() = %+v, want the credential unchanged", got)
	}
	if err := registry.ValidateCredential(AccountProviderClaude, AccountRefreshNone, cred); err != nil {
		t.Fatalf("ValidateCredential() error = %v, want nil", err)
	}
}
