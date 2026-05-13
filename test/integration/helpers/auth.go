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

//go:build integration

// Package helpers provides shared utilities for integration tests.
package helpers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// AuthError represents a failure during PIV-based authentication.
type AuthError struct {
	Op    string
	URL   string
	Cause error
}

func (e *AuthError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("auth: %s %s: %v", e.Op, e.URL, e.Cause)
	}
	return fmt.Sprintf("auth: %s %s", e.Op, e.URL)
}

func (e *AuthError) Unwrap() error { return e.Cause }

// AuthBadStatusError is returned when the PIV login endpoint responds with a
// non-200 status. The body is captured for diagnostic logging.
type AuthBadStatusError struct {
	URL    string
	Status int
	Body   string
}

func (e *AuthBadStatusError) Error() string {
	return fmt.Sprintf("auth: %s returned %d: %s", e.URL, e.Status, e.Body)
}

// pivAuthCache memoizes (baseURL, certPath, keyPath, caPath) -> bearer
// so tests that share a cluster don't re-do the PIV handshake for every
// subtest. The bearer is valid for the lifetime of the token TTL configured
// on the server, which exceeds any reasonable single-test runtime.
var pivAuthCache = struct {
	sync.Mutex
	tokens map[string]pivCachedToken
}{tokens: make(map[string]pivCachedToken)}

type pivCachedToken struct {
	Bearer    string
	ExpiresAt time.Time
}

// AuthPIV performs a PIV-cert-based login against the cluster's
// /api/v1/auth/login/piv endpoint and returns the resulting bearer token.
//
// The handshake uses mTLS: the helper opens a TLS connection presenting the
// integration test admin client certificate, the server resolves the user by
// the cert thumbprint, and the response delivers an OIDC-style token bundle.
// Only the access_token is returned here.
//
// The function is safe to call concurrently and memoizes successful tokens
// keyed by (baseURL, certPath, keyPath, caPath). The cache is invalidated
// 60 seconds before the token's reported expiry.
//
// baseURL is the REST endpoint of any cluster node (e.g.
// "https://test-node1:8080"). It must NOT include a path.
//
// Defaults: when certPath / keyPath / caPath are empty, the helper uses the
// generated integration-test certs at /etc/quicbaas/certs/{admin.crt,
// admin.key, ca.crt} (the path the test runner container mounts them at).
// Override via the QUICBAAS_TEST_PIV_CERT / _KEY / _CA env vars when running
// outside the Docker test environment.
func AuthPIV(t *testing.T, baseURL string) string {
	t.Helper()
	bearer, err := authPIV(context.Background(), baseURL, pivCertPath(), pivKeyPath(), pivCAFile())
	if err != nil {
		t.Fatalf("PIV authentication failed: %v", err)
	}
	return bearer
}

// AuthPIVContext is the context-aware variant of AuthPIV. It returns the
// bearer and the error rather than calling t.Fatal so callers can implement
// custom retry / error policies. Empty certPath / keyPath / caPath fall back
// to the standard integration-test locations under /etc/quicbaas/certs (and
// honor the QUICBAAS_TEST_PIV_CERT / _KEY / _CA env vars).
func AuthPIVContext(ctx context.Context, baseURL, certPath, keyPath, caPath string) (string, error) {
	if certPath == "" {
		certPath = pivCertPath()
	}
	if keyPath == "" {
		keyPath = pivKeyPath()
	}
	if caPath == "" {
		caPath = pivCAFile()
	}
	return authPIV(ctx, baseURL, certPath, keyPath, caPath)
}

func authPIV(ctx context.Context, baseURL, certPath, keyPath, caPath string) (string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	cacheKey := baseURL + "|" + certPath + "|" + keyPath + "|" + caPath

	pivAuthCache.Lock()
	if cached, ok := pivAuthCache.tokens[cacheKey]; ok && time.Now().Before(cached.ExpiresAt) {
		pivAuthCache.Unlock()
		return cached.Bearer, nil
	}
	pivAuthCache.Unlock()

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return "", &AuthError{Op: "load client cert", URL: certPath, Cause: err}
	}

	roots, err := loadCAPool(caPath)
	if err != nil {
		return "", &AuthError{Op: "load CA pool", URL: caPath, Cause: err}
	}

	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            roots,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, //#nosec G402 -- integration test certs are self-signed against the test CA
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     tlsCfg,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	endpoint := baseURL + "/api/v1/auth/login/piv"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", &AuthError{Op: "build request", URL: endpoint, Cause: err}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", &AuthError{Op: "POST", URL: endpoint, Cause: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &AuthError{Op: "read response", URL: endpoint, Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return "", &AuthBadStatusError{URL: endpoint, Status: resp.StatusCode, Body: string(body)}
	}

	var out struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", &AuthError{Op: "decode response", URL: endpoint, Cause: err}
	}
	if out.AccessToken == "" {
		return "", &AuthError{Op: "missing access_token", URL: endpoint}
	}

	expiry := time.Now().Add(15 * time.Minute)
	if out.ExpiresIn > 60 {
		expiry = time.Now().Add(time.Duration(out.ExpiresIn-60) * time.Second)
	}
	pivAuthCache.Lock()
	pivAuthCache.tokens[cacheKey] = pivCachedToken{Bearer: out.AccessToken, ExpiresAt: expiry}
	pivAuthCache.Unlock()

	return out.AccessToken, nil
}

// pivCertPath resolves the admin client certificate path.
func pivCertPath() string {
	if v := os.Getenv("QUICBAAS_TEST_PIV_CERT"); v != "" {
		return v
	}
	return "/etc/quicbaas/certs/admin.crt"
}

// pivKeyPath resolves the admin client private key path.
func pivKeyPath() string {
	if v := os.Getenv("QUICBAAS_TEST_PIV_KEY"); v != "" {
		return v
	}
	return "/etc/quicbaas/certs/admin.key"
}

// pivCAFile resolves the trusted CA bundle path.
func pivCAFile() string {
	if v := os.Getenv("QUICBAAS_TEST_PIV_CA"); v != "" {
		return v
	}
	return "/etc/quicbaas/certs/ca.crt"
}

// loadCAPool reads a PEM CA bundle from disk and returns it as an *x509.CertPool.
// Returns an empty pool when path is empty so callers can opt out of CA pinning.
func loadCAPool(path string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if path == "" {
		return pool, nil
	}
	pem, err := os.ReadFile(path) //nolint:gosec // path comes from test config
	if err != nil {
		return nil, err
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("loadCAPool: no PEM blocks in %s", path)
	}
	return pool, nil
}
