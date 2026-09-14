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
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// jwksValidator serves the given JWKS entries from a test IdP and returns a
// validator pointed at that endpoint. Exercising the validator (rather than the
// parser directly) keeps these tests on the boundary that actually decides
// whether a federated key becomes a usable verifier.
func jwksValidator(t *testing.T, keys ...map[string]interface{}) *IDTokenValidator {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"keys": keys}); err != nil {
			t.Errorf("encode jwks: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	v, err := NewIDTokenValidator(IDTokenValidatorConfig{
		Issuer:     "https://idp.example.com",
		Audience:   "test-client-id",
		JWKSURL:    srv.URL,
		HTTPClient: srv.Client(),
		Now:        func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	return v
}

// ecJWK renders a P-256 public key as a JWKS entry with RFC 7518 fixed-width
// coordinates.
func ecJWK(kid string, x, y *big.Int) map[string]interface{} {
	const coordLen = 32 // P-256 field element size.
	xb := make([]byte, coordLen)
	yb := make([]byte, coordLen)
	x.FillBytes(xb)
	y.FillBytes(yb)
	return map[string]interface{}{
		"kty": "EC",
		"kid": kid,
		"use": "sig",
		"alg": "ES256",
		"crv": "P-256",
		"x":   b64(xb),
		"y":   b64(yb),
	}
}

// rsaJWK renders an RSA public key as a JWKS entry.
func rsaJWK(kid string, pub *rsa.PublicKey) map[string]interface{} {
	return map[string]interface{}{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": "RS256",
		"n":   b64(pub.N.Bytes()),
		"e":   b64(big.NewInt(int64(pub.E)).Bytes()),
	}
}

// offCurveECJWK builds a P-256 JWKS entry whose (x, y) is provably NOT a point
// on the curve. crypto/ecdh confirms the premise: it rejects an uncompressed
// point that does not satisfy the curve equation, so if the point were on the
// curve this helper fails the test rather than letting it pass vacuously.
func offCurveECJWK(t *testing.T, kid string) map[string]interface{} {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ec keygen: %v", err)
	}
	// Perturbing y leaves x untouched, so the pair can no longer satisfy
	// y^2 = x^3 - 3x + b: for a given x only y and -y are valid.
	badY := new(big.Int).Add(key.Y, big.NewInt(1))

	const coordLen = 32
	point := make([]byte, 1+2*coordLen)
	point[0] = 4 // uncompressed point marker
	key.X.FillBytes(point[1 : 1+coordLen])
	badY.FillBytes(point[1+coordLen:])
	if _, err := ecdh.P256().NewPublicKey(point); err == nil {
		t.Fatal("test premise broken: the perturbed point is still on the P-256 curve")
	}

	return ecJWK(kid, key.X, badY)
}

// An IdP-supplied EC key whose coordinates are not on the declared curve must
// never become a usable verification key. Building an ecdsa.PublicKey from raw
// coordinates without this check lets whoever answers the JWKS fetch hand us an
// invalid key that ID-token signatures are then verified against.
func TestIDTokenJWKS_RejectsOffCurveECKey(t *testing.T) {
	v := jwksValidator(t, offCurveECJWK(t, "off-curve-kid"))

	key, err := v.publicKeyForKID(context.Background(), "off-curve-kid")
	if err == nil {
		t.Fatalf("off-curve EC JWK was accepted as a signing key: %#v", key)
	}
	if key != nil {
		t.Fatalf("off-curve EC JWK returned a key alongside the error: %#v", key)
	}
}

// A well-formed EC key on the declared curve is accepted and returned intact.
func TestIDTokenJWKS_AcceptsValidECKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ec keygen: %v", err)
	}
	v := jwksValidator(t, ecJWK("ec-kid", key.X, key.Y))

	got, err := v.publicKeyForKID(context.Background(), "ec-kid")
	if err != nil {
		t.Fatalf("valid EC JWK rejected: %v", err)
	}
	pub, ok := got.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("want *ecdsa.PublicKey, got %T", got)
	}
	if pub.Curve != elliptic.P256() || pub.X.Cmp(key.X) != 0 || pub.Y.Cmp(key.Y) != 0 {
		t.Fatalf("EC key mismatch: %+v", pub)
	}
}

// A well-formed RSA key is accepted and returned intact.
func TestIDTokenJWKS_AcceptsValidRSAKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}
	v := jwksValidator(t, rsaJWK("rsa-kid", &key.PublicKey))

	got, err := v.publicKeyForKID(context.Background(), "rsa-kid")
	if err != nil {
		t.Fatalf("valid RSA JWK rejected: %v", err)
	}
	pub, ok := got.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("want *rsa.PublicKey, got %T", got)
	}
	if pub.N.Cmp(key.N) != 0 || pub.E != key.E {
		t.Fatalf("RSA key mismatch: %+v", pub)
	}
}

// Key types the validator's permitted RS*/ES* algorithms cannot use, and JWKs
// with undecodable members, must not reach the key cache.
func TestIDTokenJWKS_RejectsUnusableKeys(t *testing.T) {
	edKid := "okp-kid"
	edPoint := make([]byte, ed25519.PublicKeySize)
	if _, err := rand.Read(edPoint); err != nil {
		t.Fatalf("rand: %v", err)
	}

	cases := []struct {
		name string
		jwk  map[string]interface{}
	}{
		{
			name: "symmetric oct key",
			jwk: map[string]interface{}{
				"kty": "oct",
				"kid": "oct-kid",
				"k":   b64([]byte("a-shared-secret-that-is-not-a-public-key")),
			},
		},
		{
			name: "OKP key an RS*/ES* verifier cannot use",
			jwk: map[string]interface{}{
				"kty": "OKP",
				"kid": edKid,
				"crv": "Ed25519",
				"x":   b64(edPoint),
			},
		},
		{
			name: "unknown key type",
			jwk:  map[string]interface{}{"kty": "MAGIC", "kid": "magic-kid", "x": b64([]byte("x"))},
		},
		{
			name: "malformed base64 RSA modulus",
			jwk:  map[string]interface{}{"kty": "RSA", "kid": "bad-n-kid", "n": "!!!not-base64!!!", "e": "AQAB"},
		},
		{
			name: "malformed base64 EC coordinate",
			jwk:  map[string]interface{}{"kty": "EC", "kid": "bad-x-kid", "crv": "P-256", "x": "!!!", "y": "!!!"},
		},
		{
			name: "unsupported EC curve",
			jwk:  map[string]interface{}{"kty": "EC", "kid": "p192-kid", "crv": "P-192", "x": b64([]byte("xxx")), "y": b64([]byte("yyy"))},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kid, ok := tc.jwk["kid"].(string)
			if !ok {
				t.Fatal("test JWK is missing a kid")
			}
			v := jwksValidator(t, tc.jwk)

			key, err := v.publicKeyForKID(context.Background(), kid)
			if err == nil {
				t.Fatalf("unusable JWK was accepted as a signing key: %#v", key)
			}
			if key != nil {
				t.Fatalf("unusable JWK returned a key alongside the error: %#v", key)
			}
		})
	}
}

// RFC 7517 section 5: an entry the implementation cannot use is ignored, not
// treated as a reason to discard the rest of the set.
func TestIDTokenJWKS_UnusableKeyDoesNotDiscardTheSet(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen: %v", err)
	}
	v := jwksValidator(t,
		offCurveECJWK(t, "off-curve-kid"),
		map[string]interface{}{"kty": "oct", "kid": "oct-kid", "k": b64([]byte("secret"))},
		rsaJWK("good-kid", &key.PublicKey),
	)

	got, err := v.publicKeyForKID(context.Background(), "good-kid")
	if err != nil {
		t.Fatalf("usable key was discarded because the set held unusable entries: %v", err)
	}
	if _, ok := got.(*rsa.PublicKey); !ok {
		t.Fatalf("want *rsa.PublicKey, got %T", got)
	}
}

// The parse boundary keeps its typed error shape: go-jose's own error is
// carried as the IDTokenJWKSError cause rather than replacing it, so callers
// matching on *IDTokenJWKSError see no change.
func TestParseIDTokenJWK_TypedErrorShape(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantCause bool
	}{
		{"off-curve EC point", "", true},
		{"undecodable member", `{"kty":"RSA","kid":"k","n":"!!!","e":"AQAB"}`, true},
		{"unusable key type", `{"kty":"oct","kid":"k","k":"c2VjcmV0"}`, false},
	}

	offCurve, err := json.Marshal(offCurveECJWK(t, "off-curve-kid"))
	if err != nil {
		t.Fatalf("marshal off-curve jwk: %v", err)
	}
	cases[0].raw = string(offCurve)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, kid, err := parseIDTokenJWK(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatalf("want error, got key %#v (kid %q)", key, kid)
			}
			if key != nil || kid != "" {
				t.Fatalf("failed parse leaked key %#v / kid %q", key, kid)
			}
			var jwksErr *IDTokenJWKSError
			if !errors.As(err, &jwksErr) {
				t.Fatalf("want *IDTokenJWKSError, got %T: %v", err, err)
			}
			if tc.wantCause && jwksErr.Cause == nil {
				t.Fatal("want the underlying go-jose error carried as Cause")
			}
		})
	}
}
