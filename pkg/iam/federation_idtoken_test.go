//go:build iam

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package iam

import (
	"context"
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

	"github.com/golang-jwt/jwt/v5"
)

// idTokenTestHarness builds a signing key, a JWKS server, and a token factory.
type idTokenTestHarness struct {
	key      *rsa.PrivateKey
	kid      string
	jwksSrv  *httptest.Server
	issuer   string
	audience string
	now      time.Time
}

func newIDTokenHarness(t *testing.T) *idTokenTestHarness {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	h := &idTokenTestHarness{
		key:      key,
		kid:      "test-kid-1",
		issuer:   "https://idp.example.com",
		audience: "test-client-id",
		now:      time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	jwk := map[string]interface{}{
		"kty": "RSA",
		"kid": h.kid,
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{jwk}})
	})
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jwks_uri": h.jwksSrv.URL + "/jwks"})
	})
	h.jwksSrv = httptest.NewServer(mux)
	t.Cleanup(h.jwksSrv.Close)
	return h
}

func (h *idTokenTestHarness) clock() func() time.Time {
	return func() time.Time { return h.now }
}

// sign produces a signed ID token with the given claim overrides applied.
func (h *idTokenTestHarness) sign(t *testing.T, mutate func(jwt.MapClaims), kid string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":   h.issuer,
		"aud":   h.audience,
		"sub":   "okta|user-123",
		"email": "user@acme.com",
		"name":  "Test User",
		"nonce": "expected-nonce",
		"iat":   h.now.Unix(),
		"exp":   h.now.Add(time.Hour).Unix(),
	}
	if mutate != nil {
		mutate(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(h.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func (h *idTokenTestHarness) validator(t *testing.T, jwksURL string) *IDTokenValidator {
	t.Helper()
	v, err := NewIDTokenValidator(IDTokenValidatorConfig{
		Issuer:     h.issuer,
		Audience:   h.audience,
		JWKSURL:    jwksURL,
		HTTPClient: h.jwksSrv.Client(),
		Now:        h.clock(),
	})
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	return v
}

func TestIDTokenValidator_ValidToken(t *testing.T) {
	h := newIDTokenHarness(t)
	v := h.validator(t, h.jwksSrv.URL+"/jwks")

	token := h.sign(t, nil, h.kid)
	res, err := v.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
	if res.Nonce != "expected-nonce" {
		t.Errorf("nonce = %q, want expected-nonce", res.Nonce)
	}
	if res.Raw["email"] != "user@acme.com" {
		t.Errorf("email claim = %v", res.Raw["email"])
	}
}

func TestIDTokenValidator_DiscoversJWKS(t *testing.T) {
	h := newIDTokenHarness(t)
	// No explicit JWKS URL: must be discovered from the issuer's discovery doc.
	// Point the issuer at the test server so discovery resolves there.
	v, err := NewIDTokenValidator(IDTokenValidatorConfig{
		Issuer:     h.jwksSrv.URL,
		Audience:   h.audience,
		HTTPClient: h.jwksSrv.Client(),
		Now:        h.clock(),
	})
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	token := h.sign(t, func(c jwt.MapClaims) { c["iss"] = h.jwksSrv.URL }, h.kid)
	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatalf("expected discovery to succeed, got: %v", err)
	}
}

func TestIDTokenValidator_BadSignature(t *testing.T) {
	h := newIDTokenHarness(t)
	v := h.validator(t, h.jwksSrv.URL+"/jwks")

	// Sign with a DIFFERENT key but advertise the trusted kid.
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	claims := jwt.MapClaims{
		"iss": h.issuer, "aud": h.audience, "sub": "x",
		"iat": h.now.Unix(), "exp": h.now.Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = h.kid
	forged, err := tok.SignedString(other)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = v.Validate(context.Background(), forged)
	if err == nil {
		t.Fatal("expected bad-signature rejection")
	}
	var sigErr *IDTokenSignatureError
	if !errors.As(err, &sigErr) {
		t.Errorf("error = %v, want IDTokenSignatureError", err)
	}
}

func TestIDTokenValidator_WrongIssuer(t *testing.T) {
	h := newIDTokenHarness(t)
	v := h.validator(t, h.jwksSrv.URL+"/jwks")

	token := h.sign(t, func(c jwt.MapClaims) { c["iss"] = "https://evil.example.com" }, h.kid)
	_, err := v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected wrong-issuer rejection")
	}
	var issErr *IDTokenIssuerError
	if !errors.As(err, &issErr) {
		t.Errorf("error = %v, want IDTokenIssuerError", err)
	}
}

func TestIDTokenValidator_WrongAudience(t *testing.T) {
	h := newIDTokenHarness(t)
	v := h.validator(t, h.jwksSrv.URL+"/jwks")

	token := h.sign(t, func(c jwt.MapClaims) { c["aud"] = "some-other-client" }, h.kid)
	_, err := v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected wrong-audience rejection")
	}
	var audErr *IDTokenAudienceError
	if !errors.As(err, &audErr) {
		t.Errorf("error = %v, want IDTokenAudienceError", err)
	}
}

func TestIDTokenValidator_Expired(t *testing.T) {
	h := newIDTokenHarness(t)
	v := h.validator(t, h.jwksSrv.URL+"/jwks")

	token := h.sign(t, func(c jwt.MapClaims) {
		c["iat"] = h.now.Add(-2 * time.Hour).Unix()
		c["exp"] = h.now.Add(-1 * time.Hour).Unix()
	}, h.kid)
	_, err := v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected expired rejection")
	}
	var expErr *IDTokenExpiredError
	if !errors.As(err, &expErr) {
		t.Errorf("error = %v, want IDTokenExpiredError", err)
	}
}

func TestIDTokenValidator_UnsignedTokenRejected(t *testing.T) {
	h := newIDTokenHarness(t)
	v := h.validator(t, h.jwksSrv.URL+"/jwks")

	// alg=none token (the classic forged-JWT attack the old code was vulnerable to).
	claims := jwt.MapClaims{
		"iss": h.issuer, "aud": h.audience, "sub": "x",
		"iat": h.now.Unix(), "exp": h.now.Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	tok.Header["kid"] = h.kid
	unsigned, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	if _, err := v.Validate(context.Background(), unsigned); err == nil {
		t.Fatal("expected alg=none token to be rejected")
	}
}

func TestIDTokenValidator_MissingKID(t *testing.T) {
	h := newIDTokenHarness(t)
	v := h.validator(t, h.jwksSrv.URL+"/jwks")

	token := h.sign(t, nil, "") // no kid header
	if _, err := v.Validate(context.Background(), token); err == nil {
		t.Fatal("expected missing-kid rejection")
	}
}

func TestIDTokenValidator_ConfigValidation(t *testing.T) {
	client := &http.Client{}
	cases := []struct {
		name string
		cfg  IDTokenValidatorConfig
	}{
		{"missing issuer", IDTokenValidatorConfig{Audience: "a", HTTPClient: client}},
		{"missing audience", IDTokenValidatorConfig{Issuer: "i", HTTPClient: client}},
		{"missing client", IDTokenValidatorConfig{Issuer: "i", Audience: "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewIDTokenValidator(tc.cfg)
			if err == nil {
				t.Fatalf("expected config error for %s", tc.name)
			}
			var cfgErr *IDTokenValidatorConfigError
			if !errors.As(err, &cfgErr) {
				t.Errorf("error = %v, want IDTokenValidatorConfigError", err)
			}
		})
	}
}
