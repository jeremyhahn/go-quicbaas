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

package secret_test

import (
	"bytes"
	"context"
	"testing"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/secret"
)

// Compile-time proof SecretValue, ResolvedStrategy and NewSecretValue are the
// go-qrdb ones, so a value built through this SDK is what a qrdb
// ConfigProcessor fills and a qrdb resolver returns.
var (
	_ qrdbsecret.SecretValue      = secret.SecretValue{}
	_ secret.SecretValue          = qrdbsecret.SecretValue{}
	_ qrdbsecret.ResolvedStrategy = secret.StrategyLiteral
	_ secret.ResolvedStrategy     = qrdbsecret.StrategyLiteral

	_ func(context.Context, string, []byte, qrdbsecret.ResolvedStrategy) (*qrdbsecret.SecretValue, error) = secret.NewSecretValue
)

// TestStrategyConstants_AreTheGoQRDBValues pins every resolved strategy to the
// go-qrdb value, so a consumer branching on SecretValue.Strategy sees the
// strategy the resolver recorded.
func TestStrategyConstants_AreTheGoQRDBValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		local, up secret.ResolvedStrategy
	}{
		{"StrategyLiteral", secret.StrategyLiteral, qrdbsecret.StrategyLiteral},
		{"StrategyCredential", secret.StrategyCredential, qrdbsecret.StrategyCredential},
		{"StrategyLocalKMS", secret.StrategyLocalKMS, qrdbsecret.StrategyLocalKMS},
		{"StrategyRemoteKMS", secret.StrategyRemoteKMS, qrdbsecret.StrategyRemoteKMS},
		{"StrategyAutoGen", secret.StrategyAutoGen, qrdbsecret.StrategyAutoGen},
	} {
		if tc.local != tc.up {
			t.Errorf("%s = %q, want go-qrdb %q", tc.name, tc.local, tc.up)
		}
	}
}

// plaintext opens value and returns a copy of its plaintext.
func plaintext(t *testing.T, value *secret.SecretValue) []byte {
	t.Helper()
	var out []byte
	if err := value.WithBytes(context.Background(), func(b []byte) error {
		out = bytes.Clone(b)
		return nil
	}); err != nil {
		t.Fatalf("WithBytes: %v", err)
	}
	return out
}

// TestNewSecretValue_SealsAndKeepsMetadata proves the re-exported constructor
// seals the plaintext, leaves the caller's slice intact and records the
// reference and strategy it was given.
func TestNewSecretValue_SealsAndKeepsMetadata(t *testing.T) {
	given := []byte("db-password")
	value, err := secret.NewSecretValue(context.Background(), "credential://db-password", given, secret.StrategyCredential)
	if err != nil {
		t.Fatalf("NewSecretValue: %v", err)
	}
	defer value.Clear()

	if !bytes.Equal(plaintext(t, value), []byte("db-password")) {
		t.Fatal("WithBytes did not return the sealed plaintext")
	}
	if !bytes.Equal(given, []byte("db-password")) {
		t.Fatal("NewSecretValue modified the caller's slice")
	}
	if value.Raw != "credential://db-password" || value.Strategy != secret.StrategyCredential {
		t.Fatalf("metadata = (%q, %q), want the reference and strategy given", value.Raw, value.Strategy)
	}
}

// TestNewSecretValue_EmptyValueHoldsNothing proves an empty plaintext yields a
// value whose accessor reports empty, and that Clear leaves nothing behind.
func TestNewSecretValue_EmptyValueHoldsNothing(t *testing.T) {
	value, err := secret.NewSecretValue(context.Background(), "literal://", nil, secret.StrategyLiteral)
	if err != nil {
		t.Fatalf("NewSecretValue(empty): %v", err)
	}
	if len(plaintext(t, value)) != 0 {
		t.Fatal("an empty value must report empty")
	}

	sealed, err := secret.NewSecretValue(context.Background(), "literal://x", []byte("x"), secret.StrategyLiteral)
	if err != nil {
		t.Fatalf("NewSecretValue: %v", err)
	}
	sealed.Clear()
	if len(plaintext(t, sealed)) != 0 {
		t.Fatal("a cleared value must report empty")
	}
}
