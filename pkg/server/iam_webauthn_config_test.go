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

package server

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestWebauthnFactoryConfigFromOperator_AppliesDefaults verifies that the
// translator falls back to the historical dev defaults (localhost /
// QuicBaaS / https://localhost) when the operator omits required
// fields. This preserves backward compatibility with operator configs
// that pre-date Phase 5.
func TestWebauthnFactoryConfigFromOperator_AppliesDefaults(t *testing.T) {
	got := webauthnFactoryConfigFromOperator(config.IAMWebAuthnConfig{})
	if got.RPID != "localhost" {
		t.Fatalf("expected default RPID=localhost, got %q", got.RPID)
	}
	if got.RPDisplayName != "QuicBaaS" {
		t.Fatalf("expected default RPDisplayName=QuicBaaS, got %q", got.RPDisplayName)
	}
	if len(got.RPOrigins) != 1 || got.RPOrigins[0] != "https://localhost" {
		t.Fatalf("expected default RPOrigins=[https://localhost], got %v", got.RPOrigins)
	}
	if got.Timeout != 0 {
		t.Fatalf("expected zero Timeout when operator field empty, got %v", got.Timeout)
	}
}

// TestWebauthnFactoryConfigFromOperator_PreservesOperatorValues verifies
// that explicit operator values pass through unchanged.
func TestWebauthnFactoryConfigFromOperator_PreservesOperatorValues(t *testing.T) {
	op := config.IAMWebAuthnConfig{
		RPID:                    "example.com",
		RPDisplayName:           "Example, Inc.",
		RPOrigins:               []string{"https://app.example.com", "https://admin.example.com"},
		Timeout:                 "90s",
		UserVerification:        "required",
		AttestationPreference:   "direct",
		ResidentKeyRequirement:  "required",
		AuthenticatorAttachment: "platform",
		Debug:                   true,
	}
	got := webauthnFactoryConfigFromOperator(op)
	if got.RPID != "example.com" {
		t.Fatalf("RPID not preserved: %q", got.RPID)
	}
	if got.RPDisplayName != "Example, Inc." {
		t.Fatalf("RPDisplayName not preserved: %q", got.RPDisplayName)
	}
	if len(got.RPOrigins) != 2 {
		t.Fatalf("RPOrigins length wrong: %d", len(got.RPOrigins))
	}
	if got.Timeout != 90*time.Second {
		t.Fatalf("Timeout not parsed: %v", got.Timeout)
	}
	if got.UserVerification != "required" {
		t.Fatalf("UserVerification not preserved: %q", got.UserVerification)
	}
	if !got.Debug {
		t.Fatalf("Debug not preserved")
	}
}

// TestWebauthnFactoryConfigFromOperator_InvalidTimeoutIgnored verifies
// that a malformed timeout string falls back to zero (the underlying
// xkms library's default) rather than failing the conversion.
func TestWebauthnFactoryConfigFromOperator_InvalidTimeoutIgnored(t *testing.T) {
	op := config.IAMWebAuthnConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://example.com"},
		Timeout:       "not-a-duration",
	}
	got := webauthnFactoryConfigFromOperator(op)
	if got.Timeout != 0 {
		t.Fatalf("expected zero Timeout on parse failure, got %v", got.Timeout)
	}
}

// TestWebauthnFactoryConfigFromOperator_OriginSliceIsCopied verifies
// that mutations to the operator's input slice do not leak into the
// returned config. Important for hot-reload scenarios where the
// operator config is rebuilt while the factory holds a stale snapshot.
func TestWebauthnFactoryConfigFromOperator_OriginSliceIsCopied(t *testing.T) {
	origins := []string{"https://example.com"}
	op := config.IAMWebAuthnConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     origins,
	}
	got := webauthnFactoryConfigFromOperator(op)

	// Mutate the operator slice; the factory copy must not change.
	origins[0] = "https://attacker.example.com"
	if got.RPOrigins[0] != "https://example.com" {
		t.Fatalf("origin slice was not copied: factory got %q after mutation",
			got.RPOrigins[0])
	}
}
