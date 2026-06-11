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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"testing"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// toPublicKey reconstructs RSA and EC keys from JWK material and rejects bad
// input.
func TestIDTokenJWK_ToPublicKey(t *testing.T) {
	// RSA round-trip.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}
	eBytes := big.NewInt(int64(rsaKey.E)).Bytes()
	rsaJWK := &idTokenJWK{
		Kty: "RSA",
		N:   b64(rsaKey.N.Bytes()),
		E:   b64(eBytes),
	}
	pub, err := rsaJWK.toPublicKey()
	if err != nil {
		t.Fatalf("RSA toPublicKey: %v", err)
	}
	rp, ok := pub.(*rsa.PublicKey)
	if !ok || rp.N.Cmp(rsaKey.N) != 0 || rp.E != rsaKey.E {
		t.Fatalf("RSA key mismatch: %+v", pub)
	}

	// EC round-trip.
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ec keygen: %v", err)
	}
	ecJWK := &idTokenJWK{
		Kty: "EC",
		Crv: "P-256",
		X:   b64(ecKey.X.Bytes()),
		Y:   b64(ecKey.Y.Bytes()),
	}
	pub, err = ecJWK.toPublicKey()
	if err != nil {
		t.Fatalf("EC toPublicKey: %v", err)
	}
	ep, ok := pub.(*ecdsa.PublicKey)
	if !ok || ep.X.Cmp(ecKey.X) != 0 || ep.Y.Cmp(ecKey.Y) != 0 {
		t.Fatalf("EC key mismatch: %+v", pub)
	}

	// Unsupported key type.
	if _, err := (&idTokenJWK{Kty: "OKP"}).toPublicKey(); err == nil {
		t.Fatal("unsupported kty should error")
	}

	// Malformed RSA modulus / EC coordinates surface decode errors.
	if _, err := (&idTokenJWK{Kty: "RSA", N: "!!!not-base64!!!", E: "AQAB"}).toPublicKey(); err == nil {
		t.Fatal("bad RSA modulus should error")
	}
	if _, err := (&idTokenJWK{Kty: "EC", Crv: "P-256", X: "!!!", Y: b64(ecKey.Y.Bytes())}).toPublicKey(); err == nil {
		t.Fatal("bad EC x should error")
	}
	// An unsupported curve is rejected even with valid coordinates.
	if _, err := (&idTokenJWK{Kty: "EC", Crv: "P-192", X: b64(ecKey.X.Bytes()), Y: b64(ecKey.Y.Bytes())}).toPublicKey(); err == nil {
		t.Fatal("unsupported EC curve should error")
	}
}
