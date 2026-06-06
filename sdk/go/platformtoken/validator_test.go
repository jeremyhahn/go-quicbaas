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

package platformtoken

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
)

const (
	testIssuer   = "https://identity.example.gov"
	testAudience = "api://app"
	testKID      = "kid-1"
)

type issuerKey struct {
	priv *ecdsa.PrivateKey
	kid  string
}

func newIssuerKey(t *testing.T, kid string) *issuerKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("issuer key: %v", err)
	}
	return &issuerKey{priv: priv, kid: kid}
}

func (k *issuerKey) jwks() *jose.JSONWebKeySet {
	return &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       k.priv.Public(),
		KeyID:     k.kid,
		Algorithm: "ES256",
		Use:       "sig",
	}}}
}

// tokenClaims mirrors the issuer's MapClaims emission so tests exercise the
// exact wire format.
type tokenClaims struct {
	iss string
	aud []string
	sub string
	tid string
	scp []string
	exp time.Time
	iat time.Time
	jkt string
}

func (k *issuerKey) sign(t *testing.T, tc tokenClaims) string {
	t.Helper()
	m := jwt.MapClaims{
		"iss": tc.iss,
		"aud": tc.aud,
		"sub": tc.sub,
		"exp": tc.exp.Unix(),
		"iat": tc.iat.Unix(),
		"jti": "jti-" + tc.sub,
	}
	if tc.tid != "" {
		m["tenant_id"] = tc.tid
	}
	if len(tc.scp) > 0 {
		m["scp"] = tc.scp
	}
	if tc.jkt != "" {
		m["cnf"] = map[string]interface{}{"jkt": tc.jkt}
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, m)
	tok.Header["kid"] = k.kid
	s, err := tok.SignedString(k.priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func newTestValidator(t *testing.T, key *issuerKey, now time.Time) *Validator {
	t.Helper()
	v, err := NewValidator(Options{
		Issuer:   testIssuer,
		Audience: testAudience,
		JWKS:     key.jwks(),
		Clock:    fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	return v
}

func TestValidatePlatformToken_Valid(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newTestValidator(t, key, now)

	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "user-1",
		tid: "tenant-1", scp: []string{"app.read"},
		exp: now.Add(10 * time.Minute), iat: now,
	})

	claims, err := v.ValidatePlatformToken(context.Background(), tok)
	if err != nil {
		t.Fatalf("ValidatePlatformToken: %v", err)
	}
	if claims.Subject != "user-1" || claims.TenantID != "tenant-1" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	az := Authorize(claims)
	if !az.HasScope("app.read") {
		t.Fatal("expected app.read scope")
	}
}

func TestValidatePlatformToken_WrongIssuer(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newTestValidator(t, key, now)

	tok := key.sign(t, tokenClaims{
		iss: "https://evil.example", aud: []string{testAudience}, sub: "u",
		exp: now.Add(10 * time.Minute), iat: now,
	})
	_, err := v.ValidatePlatformToken(context.Background(), tok)
	var iss *InvalidIssuerError
	if !errors.As(err, &iss) {
		t.Fatalf("expected InvalidIssuerError, got %v", err)
	}
}

func TestValidatePlatformToken_WrongAudience(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newTestValidator(t, key, now)

	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{"api://other"}, sub: "u",
		exp: now.Add(10 * time.Minute), iat: now,
	})
	_, err := v.ValidatePlatformToken(context.Background(), tok)
	var aud *InvalidAudienceError
	if !errors.As(err, &aud) {
		t.Fatalf("expected InvalidAudienceError, got %v", err)
	}
}

func TestValidatePlatformToken_Expired(t *testing.T) {
	issuedAt := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	// Validator clock is 20 minutes after issuance; token TTL was 10 minutes.
	v := newTestValidator(t, key, issuedAt.Add(20*time.Minute))

	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		exp: issuedAt.Add(10 * time.Minute), iat: issuedAt,
	})
	_, err := v.ValidatePlatformToken(context.Background(), tok)
	var exp *ExpiredTokenError
	if !errors.As(err, &exp) {
		t.Fatalf("expected ExpiredTokenError, got %v", err)
	}
}

func TestValidatePlatformToken_UnknownKID(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	// Validator trusts kid-1 only; sign with a key advertising kid-2.
	trusted := newIssuerKey(t, testKID)
	v := newTestValidator(t, trusted, now)

	other := newIssuerKey(t, "kid-2")
	tok := other.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		exp: now.Add(10 * time.Minute), iat: now,
	})
	_, err := v.ValidatePlatformToken(context.Background(), tok)
	var unknown *UnknownKeyIDError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected UnknownKeyIDError, got %v", err)
	}
}

func TestValidatePlatformToken_WrongSigningKey(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	trusted := newIssuerKey(t, testKID)
	v := newTestValidator(t, trusted, now)

	// Attacker signs with a different key but reuses the trusted kid.
	forger := newIssuerKey(t, testKID)
	tok := forger.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		exp: now.Add(10 * time.Minute), iat: now,
	})
	_, err := v.ValidatePlatformToken(context.Background(), tok)
	var invalid *InvalidTokenError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidTokenError for bad signature, got %v", err)
	}
}

func TestNewValidator_ConfigErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"no issuer", Options{Audience: testAudience, JWKSURL: "https://x"}},
		{"no audience", Options{Issuer: testIssuer, JWKSURL: "https://x"}},
		{"no key source", Options{Issuer: testIssuer, Audience: testAudience}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewValidator(tc.opts)
			var cfg *ConfigError
			if !errors.As(err, &cfg) {
				t.Fatalf("expected ConfigError, got %v", err)
			}
		})
	}
}

func TestValidator_RemoteJWKS(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(key.jwks())
	}))
	defer srv.Close()

	v, err := NewValidator(Options{
		Issuer:     testIssuer,
		Audience:   testAudience,
		JWKSURL:    srv.URL,
		HTTPClient: srv.Client(),
		Clock:      fixedNow(now),
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		exp: now.Add(10 * time.Minute), iat: now,
	})
	if _, err := v.ValidatePlatformToken(context.Background(), tok); err != nil {
		t.Fatalf("remote JWKS validate: %v", err)
	}
}

func TestValidateWithDPoP_BindsTokenToProofKey(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newTestValidator(t, key, now)

	// DPoP key held by the legitimate sender.
	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("dpop key: %v", err)
	}
	jkt, err := dpop.Thumbprint(dpopKey.Public())
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}

	htm, htu := "GET", "https://api.app.example/v1/records"
	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		tid: "tenant-1", scp: []string{"app.read"},
		exp: now.Add(10 * time.Minute), iat: now, jkt: jkt,
	})

	proof, err := dpop.NewProofWithClock(dpopKey, htm, htu, &fixedDPoPClock{now})
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}

	cache := dpop.NewMemoryReplayCache(&fixedDPoPClock{now})
	claims, err := v.ValidateWithDPoP(context.Background(), tok, proof, htm, htu, cache)
	if err != nil {
		t.Fatalf("ValidateWithDPoP valid: %v", err)
	}
	if claims.Confirmation == nil || claims.Confirmation.Jkt != jkt {
		t.Fatalf("expected cnf.jkt %q, got %+v", jkt, claims.Confirmation)
	}
}

func TestValidateWithDPoP_StolenTokenDifferentKey(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newTestValidator(t, key, now)

	// Token bound to the legitimate sender's DPoP key.
	legitKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	jkt, _ := dpop.Thumbprint(legitKey.Public())

	htm, htu := "GET", "https://api.app.example/v1/records"
	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		exp: now.Add(10 * time.Minute), iat: now, jkt: jkt,
	})

	// Attacker steals the token and presents a proof signed with its OWN key.
	attackerKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	proof, err := dpop.NewProofWithClock(attackerKey, htm, htu, &fixedDPoPClock{now})
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}

	cache := dpop.NewMemoryReplayCache(&fixedDPoPClock{now})
	_, err = v.ValidateWithDPoP(context.Background(), tok, proof, htm, htu, cache)
	var mismatch *dpop.ThumbprintMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected ThumbprintMismatchError for stolen token, got %v", err)
	}
}

func TestValidateWithDPoP_BearerRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newTestValidator(t, key, now)

	// Token with NO cnf.jkt (a plain bearer).
	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		exp: now.Add(10 * time.Minute), iat: now,
	})

	dpopKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	proof, _ := dpop.NewProofWithClock(dpopKey, "GET", "https://api.app.example/v1/records", &fixedDPoPClock{now})

	cache := dpop.NewMemoryReplayCache(&fixedDPoPClock{now})
	_, err := v.ValidateWithDPoP(context.Background(), tok, proof, "GET", "https://api.app.example/v1/records", cache)
	var notSC *NotSenderConstrainedError
	if !errors.As(err, &notSC) {
		t.Fatalf("expected NotSenderConstrainedError, got %v", err)
	}
}

func TestValidateWithDPoP_MissingProof(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newTestValidator(t, key, now)

	jkt := "some-thumbprint"
	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		exp: now.Add(10 * time.Minute), iat: now, jkt: jkt,
	})
	cache := dpop.NewMemoryReplayCache(&fixedDPoPClock{now})
	_, err := v.ValidateWithDPoP(context.Background(), tok, "", "GET", "https://x/y", cache)
	var missing *dpop.MissingProofError
	if !errors.As(err, &missing) {
		t.Fatalf("expected MissingProofError, got %v", err)
	}
}

func TestRequireScope(t *testing.T) {
	az := &AuthorizedContext{Scopes: []string{"app.read"}}
	if err := az.RequireScope("app.read"); err != nil {
		t.Fatalf("RequireScope(app.read): %v", err)
	}
	err := az.RequireScope("app.admin")
	var denied *ScopeDeniedError
	if !errors.As(err, &denied) || denied.Required != "app.admin" {
		t.Fatalf("expected ScopeDeniedError for app.admin, got %v", err)
	}
	if err := az.RequireAnyScope("app.admin", "app.read"); err != nil {
		t.Fatalf("RequireAnyScope should pass: %v", err)
	}
	if err := az.RequireAnyScope("app.admin"); err == nil {
		t.Fatal("RequireAnyScope(app.admin) should be denied")
	}
}

func TestClaims_ScopesFromSpaceSeparated(t *testing.T) {
	c := &Claims{Scope: "app.read app.admin"}
	az := Authorize(c)
	if !az.HasScope("app.read") || !az.HasScope("app.admin") {
		t.Fatalf("expected both scopes from space-separated, got %v", az.Scopes)
	}
}

// fixedDPoPClock adapts a fixed time to dpop.Clock.
type fixedDPoPClock struct{ t time.Time }

func (f *fixedDPoPClock) Now() time.Time { return f.t }
