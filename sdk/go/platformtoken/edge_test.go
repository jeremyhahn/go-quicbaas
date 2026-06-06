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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidatePlatformToken_EmptyToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newTestValidator(t, key, now)
	_, err := v.ValidatePlatformToken(context.Background(), "")
	var invalid *InvalidTokenError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidTokenError for empty token, got %v", err)
	}
}

func TestValidatePlatformToken_MissingKID(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, "")
	v := newTestValidator(t, key, now)
	// Sign with an empty kid header.
	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		exp: now.Add(10 * time.Minute), iat: now,
	})
	_, err := v.ValidatePlatformToken(context.Background(), tok)
	if err == nil {
		t.Fatal("expected error for missing kid")
	}
}

func TestValidator_RemoteJWKS_FetchFailure(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
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
	_, err = v.ValidatePlatformToken(context.Background(), tok)
	var fetch *JWKSFetchError
	if !errors.As(err, &fetch) {
		t.Fatalf("expected JWKSFetchError, got %v", err)
	}
}

func TestValidator_RemoteJWKS_CacheTTLRefresh(t *testing.T) {
	clk := &mutableClock{t: time.Unix(1_700_000_000, 0)}
	key := newIssuerKey(t, testKID)

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(key.jwks())
	}))
	defer srv.Close()

	v, err := NewValidator(Options{
		Issuer:     testIssuer,
		Audience:   testAudience,
		JWKSURL:    srv.URL,
		HTTPClient: srv.Client(),
		CacheTTL:   time.Minute,
		Clock:      clk.now,
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "u",
		exp: clk.t.Add(2 * time.Hour), iat: clk.t,
	})
	if _, err := v.ValidatePlatformToken(context.Background(), tok); err != nil {
		t.Fatalf("first validate: %v", err)
	}
	// Within TTL: cached, no extra fetch.
	if _, err := v.ValidatePlatformToken(context.Background(), tok); err != nil {
		t.Fatalf("second validate: %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 JWKS fetch within TTL, got %d", hits)
	}

	// Advance beyond TTL: a refresh fetch occurs.
	clk.t = clk.t.Add(2 * time.Minute)
	if _, err := v.ValidatePlatformToken(context.Background(), tok); err != nil {
		t.Fatalf("third validate after TTL: %v", err)
	}
	if hits != 2 {
		t.Fatalf("expected JWKS refetch after TTL, got %d", hits)
	}
}

func TestErrorMessages(t *testing.T) {
	cases := []error{
		&ConfigError{Detail: "x"},
		&InvalidTokenError{Detail: "x", Cause: errors.New("c")},
		&InvalidTokenError{Detail: "x"},
		&ExpiredTokenError{},
		&InvalidIssuerError{Expected: "a", Actual: "b"},
		&InvalidAudienceError{Expected: "a"},
		&UnknownKeyIDError{KID: "k"},
		&JWKSFetchError{Detail: "x", Cause: errors.New("c")},
		&JWKSFetchError{Detail: "x"},
		&NotSenderConstrainedError{},
		&ScopeDeniedError{Required: "s"},
	}
	for _, err := range cases {
		if err.Error() == "" {
			t.Errorf("empty error message for %T", err)
		}
	}
	if errors.Unwrap(&InvalidTokenError{Cause: errors.New("c")}) == nil {
		t.Error("InvalidTokenError.Unwrap nil")
	}
	if errors.Unwrap(&JWKSFetchError{Cause: errors.New("c")}) == nil {
		t.Error("JWKSFetchError.Unwrap nil")
	}
}

func TestAuthorize_RolesAndConfirmation(t *testing.T) {
	c := &Claims{
		Roles:        []string{"admin"},
		SCP:          []string{"app.admin"},
		Confirmation: &Confirmation{Jkt: "abc"},
	}
	c.Subject = "user-1"
	c.TenantID = "tenant-1"
	az := Authorize(c)
	if !az.HasRole("admin") || az.HasRole("nope") {
		t.Fatal("HasRole mismatch")
	}
	if az.TenantID != "tenant-1" || az.Subject != "user-1" {
		t.Fatal("subject/tenant not projected")
	}
	if az.Confirmation == nil || az.Confirmation.Jkt != "abc" {
		t.Fatal("confirmation not projected")
	}
}

// mutableClock is an adjustable clock for TTL-refresh tests.
type mutableClock struct{ t time.Time }

func (c *mutableClock) now() time.Time { return c.t }
