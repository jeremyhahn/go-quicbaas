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
	"path/filepath"
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

// pivLoginPath is quicbaas's own OIDC-session PIV login, whose bearer the
// platform validator trusts. On a composed node /api/v1/auth/login/piv is
// go-xkms's session login, whose token is signed by a key outside quicbaas's
// JWKS.
const pivLoginPath = "/api/v1/iam/login/piv"

// AuthPIV performs a PIV-cert-based login against the cluster's
// /api/v1/iam/login/piv endpoint and returns the resulting bearer token.
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
	bearer, _, err := authPIV(context.Background(), baseURL, pivCertPath(), pivKeyPath(), pivCAFile())
	if err != nil {
		t.Fatalf("PIV authentication failed: %v", err)
	}
	return bearer
}

// AuthSystemAdminContext is AuthPIV with an explicit context and error: it
// logs in with the fixture system-admin certificate and returns its bearer,
// which is bound to the system tenant.
func AuthSystemAdminContext(ctx context.Context, baseURL string) (string, error) {
	bearer, _, err := authPIV(ctx, baseURL, pivCertPath(), pivKeyPath(), pivCAFile())
	return bearer, err
}

// AuthPIVContext is the context-aware variant of AuthPIV. It returns the
// bearer and the error rather than calling t.Fatal so callers can implement
// custom retry / error policies. Empty certPath / keyPath / caPath fall back
// to the standard integration-test locations under /etc/quicbaas/certs (and
// honor the QUICBAAS_TEST_PIV_CERT / _KEY / _CA env vars).
func AuthPIVContext(ctx context.Context, baseURL, certPath, keyPath, caPath string) (string, error) {
	bearer, _, err := AuthPIVSession(ctx, baseURL, certPath, keyPath, caPath)
	return bearer, err
}

// AuthPIVSession is like AuthPIVContext but additionally returns the
// bearer's effective expiry (the server-reported expires_in minus a
// 60-second safety skew). Callers that persist the bearer — e.g. to the
// CLI default token file consumed by loadStoredAuthToken() in
// cmd/quicbaas/client.go — MUST store this expiry rather than inventing
// one: the server's default access-token TTL is 15 minutes
// (pkg/config DefaultOIDCAccessTokenTTL), and a fabricated longer
// client-side expiry makes the CLI present already-expired bearers,
// which the REST transport rejects with 401 invalid_token.
func AuthPIVSession(ctx context.Context, baseURL, certPath, keyPath, caPath string) (string, time.Time, error) {
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

// PIVSession is the full token bundle a PIV mTLS login returns. Tests that
// exercise session RENEWAL need the refresh credential, not just the bearer,
// and must not share a cached session with other tests: the credential is
// rotated on redemption, so a second holder of the same value is locked out.
type PIVSession struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// PIVMTLSClient builds an HTTP client that presents the integration admin PIV
// certificate. Empty paths fall back to the standard integration locations.
func PIVMTLSClient(certPath, keyPath, caPath string) (*http.Client, error) {
	if certPath == "" {
		certPath = pivCertPath()
	}
	if keyPath == "" {
		keyPath = pivKeyPath()
	}
	if caPath == "" {
		caPath = pivCAFile()
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, &AuthError{Op: "load client cert", URL: certPath, Cause: err}
	}
	roots, err := loadCAPool(caPath)
	if err != nil {
		return nil, &AuthError{Op: "load CA pool", URL: caPath, Cause: err}
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      roots,
				MinVersion:   tls.VersionTLS13,
			},
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}, nil
}

// AuthPIVFullSession performs an UNCACHED PIV mTLS login and returns the whole
// token bundle, including the refresh credential.
func AuthPIVFullSession(ctx context.Context, baseURL, certPath, keyPath, caPath string) (*PIVSession, error) {
	client, err := PIVMTLSClient(certPath, keyPath, caPath)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(baseURL, "/") + pivLoginPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, &AuthError{Op: "build request", URL: endpoint, Cause: err}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, &AuthError{Op: "POST", URL: endpoint, Cause: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &AuthError{Op: "read response", URL: endpoint, Cause: err}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &AuthBadStatusError{URL: endpoint, Status: resp.StatusCode, Body: string(body)}
	}

	var out PIVSession
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, &AuthError{Op: "decode response", URL: endpoint, Cause: err}
	}
	if out.AccessToken == "" {
		return nil, &AuthError{Op: "missing access_token", URL: endpoint}
	}
	return &out, nil
}

func authPIV(ctx context.Context, baseURL, certPath, keyPath, caPath string) (string, time.Time, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	cacheKey := baseURL + "|" + certPath + "|" + keyPath + "|" + caPath

	pivAuthCache.Lock()
	if cached, ok := pivAuthCache.tokens[cacheKey]; ok && time.Now().Before(cached.ExpiresAt) {
		pivAuthCache.Unlock()
		return cached.Bearer, cached.ExpiresAt, nil
	}
	pivAuthCache.Unlock()

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return "", time.Time{}, &AuthError{Op: "load client cert", URL: certPath, Cause: err}
	}

	roots, err := loadCAPool(caPath)
	if err != nil {
		return "", time.Time{}, &AuthError{Op: "load CA pool", URL: caPath, Cause: err}
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      roots,
		MinVersion:   tls.VersionTLS13,
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     tlsCfg,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	endpoint := baseURL + pivLoginPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", time.Time{}, &AuthError{Op: "build request", URL: endpoint, Cause: err}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, &AuthError{Op: "POST", URL: endpoint, Cause: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, &AuthError{Op: "read response", URL: endpoint, Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, &AuthBadStatusError{URL: endpoint, Status: resp.StatusCode, Body: string(body)}
	}

	var out struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, &AuthError{Op: "decode response", URL: endpoint, Cause: err}
	}
	if out.AccessToken == "" {
		return "", time.Time{}, &AuthError{Op: "missing access_token", URL: endpoint}
	}

	expiry := time.Now().Add(15 * time.Minute)
	if out.ExpiresIn > 60 {
		expiry = time.Now().Add(time.Duration(out.ExpiresIn-60) * time.Second)
	}
	pivAuthCache.Lock()
	pivAuthCache.tokens[cacheKey] = pivCachedToken{Bearer: out.AccessToken, ExpiresAt: expiry}
	pivAuthCache.Unlock()

	return out.AccessToken, expiry, nil
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
	pem, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("loadCAPool: no PEM blocks in %s", path)
	}
	return pool, nil
}
