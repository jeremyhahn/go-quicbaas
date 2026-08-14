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

// Package auth test helpers for asymmetric (ES256) JWT signing. All tests in
// this package that need a signing-capable AuthConfig use newTestAuthConfig
// so ECDSA key generation is not repeated in every test.
package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"
)

// testSigningKeyID is the JWT signing key ID used by auth package tests.
const testSigningKeyID = "test-signing-key"

// newTestSigner generates a fresh ECDSA P-256 private key for tests. The
// returned key satisfies crypto.Signer.
func newTestSigner(tb testing.TB) *ecdsa.PrivateKey {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatalf("generate test signing key: %v", err)
	}
	return key
}

// newTestAuthConfig returns an enabled AuthConfig ready for asymmetric ES256
// JWT signing with a fresh ECDSA P-256 key. Tests override TTLs or other
// fields on the returned config as needed.
func newTestAuthConfig(tb testing.TB) *AuthConfig {
	tb.Helper()
	cfg := &AuthConfig{
		Enabled:             true,
		JWTSigningKeyID:     testSigningKeyID,
		JWTSigningAlgorithm: "ES256",
		TokenTTL:            1 * time.Hour,
		RefreshTokenTTL:     24 * time.Hour,
	}
	SetAsymmetricSigning(cfg, newTestSigner(tb))
	return cfg
}
