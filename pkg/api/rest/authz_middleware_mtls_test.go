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

//go:build rest

package rest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// generateTestCert creates a self-signed test certificate for mTLS tests.
func generateTestCert(t *testing.T) *x509.Certificate {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-mtls-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create test certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatalf("failed to parse test certificate: %v", err)
	}

	return cert
}

// certThumbprint returns the base64url SHA-256 thumbprint of a certificate.
func certThumbprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// buildBoundJWT creates a minimal JWT with a cnf.x5t#S256 claim for the given cert.
// The signature is left as dummy bytes since the middleware only parses claims.
func buildBoundJWT(cert *x509.Certificate) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT"}`))

	thumb := certThumbprint(cert)
	claimsMap := map[string]interface{}{
		"sub": "service-account",
		"exp": time.Now().Add(time.Hour).Unix(),
		"cnf": map[string]interface{}{
			"x5t#S256": thumb,
		},
	}
	claimsJSON, _ := json.Marshal(claimsMap)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("dummy-sig"))

	return header + "." + payload + "." + sig
}

// buildUnboundJWT creates a minimal JWT without a cnf claim (human-user token).
func buildUnboundJWT() string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT"}`))

	claimsMap := map[string]interface{}{
		"sub": "human-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	claimsJSON, _ := json.Marshal(claimsMap)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("dummy-sig"))

	return header + "." + payload + "." + sig
}

// buildBoundJWTWithThumbprint builds a bound JWT with an arbitrary thumbprint.
func buildBoundJWTWithThumbprint(thumb string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT"}`))

	claimsMap := map[string]interface{}{
		"sub": "service-account",
		"exp": time.Now().Add(time.Hour).Unix(),
		"cnf": map[string]interface{}{
			"x5t#S256": thumb,
		},
	}
	claimsJSON, _ := json.Marshal(claimsMap)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := base64.RawURLEncoding.EncodeToString([]byte("dummy-sig"))

	return header + "." + payload + "." + sig
}

// TestAuthzMiddleware_BoundTokenWithoutCert_Returns401 verifies that a cnf-bound
// token without a peer certificate is rejected with 401.
func TestAuthzMiddleware_BoundTokenWithoutCert_Returns401(t *testing.T) {
	cert := generateTestCert(t)
	boundToken := buildBoundJWT(cert)

	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:    true,
			UserID:   "svc-1",
			Username: "svc-1",
		},
	}
	authzSvc := &mockAuthzService{
		decision: &interfaces.AuthorizationDecision{Allowed: true},
	}

	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})
	middleware := NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: validator,
		AuthzService:   authzSvc,
	})

	// Request without TLS (no peer certs).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer "+boundToken)

	w := httptest.NewRecorder()
	handler := middleware.Authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestAuthzMiddleware_BoundTokenThumbprintMismatch_Returns401 verifies that a
// cnf-bound token presented with a different client cert returns 401.
func TestAuthzMiddleware_BoundTokenThumbprintMismatch_Returns401(t *testing.T) {
	boundCert := generateTestCert(t)
	presentedCert := generateTestCert(t) // Different cert — thumbprint won't match.

	// Build a JWT bound to boundCert's thumbprint.
	boundToken := buildBoundJWT(boundCert)

	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:    true,
			UserID:   "svc-1",
			Username: "svc-1",
		},
	}
	authzSvc := &mockAuthzService{
		decision: &interfaces.AuthorizationDecision{Allowed: true},
	}

	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})
	middleware := NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: validator,
		AuthzService:   authzSvc,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer "+boundToken)
	// Set a different cert in TLS state.
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{presentedCert},
	}

	w := httptest.NewRecorder()
	handler := middleware.Authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (thumbprint mismatch), got %d", w.Code)
	}
}

// TestAuthzMiddleware_BoundTokenMatch_Allows verifies that a cnf-bound token
// presented with the matching client cert is allowed through.
func TestAuthzMiddleware_BoundTokenMatch_Allows(t *testing.T) {
	cert := generateTestCert(t)
	boundToken := buildBoundJWT(cert) // Token bound to cert's thumbprint.

	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:    true,
			UserID:   "svc-1",
			Username: "svc-1",
		},
	}
	authzSvc := &mockAuthzService{
		decision: &interfaces.AuthorizationDecision{Allowed: true},
	}

	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})
	middleware := NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: validator,
		AuthzService:   authzSvc,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer "+boundToken)
	// Present the matching cert.
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	w := httptest.NewRecorder()
	called := false
	handler := middleware.Authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !called {
		t.Error("expected next handler to be called")
	}
}

// TestAuthzMiddleware_UnboundTokenBypassesCheck verifies that a human-user token
// without a cnf claim passes through without requiring a peer certificate.
func TestAuthzMiddleware_UnboundTokenBypassesCheck(t *testing.T) {
	unboundToken := buildUnboundJWT()

	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:    true,
			UserID:   "human-1",
			Username: "alice",
		},
	}
	authzSvc := &mockAuthzService{
		decision: &interfaces.AuthorizationDecision{Allowed: true},
	}

	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})
	middleware := NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: validator,
		AuthzService:   authzSvc,
	})

	// No TLS, no peer cert — unbound token should still pass.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/foo", nil)
	req.Header.Set("Authorization", "Bearer "+unboundToken)

	w := httptest.NewRecorder()
	called := false
	handler := middleware.Authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (unbound token bypass), got %d", w.Code)
	}
	if !called {
		t.Error("expected next handler to be called for unbound token")
	}
}

// TestExtractJWTClaims_ValidPayload verifies correct claim extraction.
func TestExtractJWTClaims_ValidPayload(t *testing.T) {
	cert := generateTestCert(t)
	token := buildBoundJWT(cert)

	claims, err := extractJWTClaims(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cnf, ok := claims["cnf"].(map[string]interface{})
	if !ok {
		t.Fatal("cnf claim missing or wrong type")
	}

	thumb, ok := cnf["x5t#S256"].(string)
	if !ok || thumb == "" {
		t.Fatal("x5t#S256 missing from cnf claim")
	}

	if expected := certThumbprint(cert); thumb != expected {
		t.Errorf("thumbprint mismatch: got %s, want %s", thumb, expected)
	}
}

// TestExtractJWTClaims_InvalidFormat verifies that malformed JWTs are handled.
func TestExtractJWTClaims_InvalidFormat(t *testing.T) {
	cases := []struct {
		name  string
		token string
	}{
		{"no dots", "notavalidjwt"},
		{"one dot", "header.payload"},
		{"invalid base64", "hdr." + strings.Repeat("!", 10) + ".sig"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := extractJWTClaims(tc.token)
			if err == nil {
				t.Error("expected error for invalid JWT, got nil")
			}
		})
	}
}
