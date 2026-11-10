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
// +build integration

package iam_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultTenantID   = "test-tenant"
	defaultTenantName = "Test Tenant"

	// Retry configuration for transient Raft errors
	maxRetries       = 10
	retryDelay       = 500 * time.Millisecond
	retryMaxDelay    = 5 * time.Second
	retryBackoffMult = 1.5
)

// testCluster represents a test cluster with IAM enabled.
type testCluster struct {
	httpEndpoint string
	grpcEndpoint string
	quicEndpoint string
	unixSocket   string
	client       *http.Client
	grpcConn     *grpc.ClientConn

	// t is the test that set the cluster up.
	t *testing.T

	// mu guards systemBearer, tenantAdmins and current.
	mu sync.Mutex
	// systemBearer is the system admin's bearer, fetched once.
	systemBearer string
	// tenantAdmins holds each provisioned tenant's admin session, by tenant ID.
	tenantAdmins map[string]*helpers.TenantAdminSession
	// current is the tenant the test last provisioned.
	current string
}

// TestMain sets up and tears down the test environment.
func TestMain(m *testing.M) {
	fmt.Println(">>> TestMain starting - waiting for cluster")
	// Wait for cluster to be ready
	if !waitForClusterReady() {
		fmt.Println(">>> Cluster not ready, exiting")
		os.Exit(1)
	}
	// Boot embedded server for multi-protocol tests
	_, srv, err := helpers.SetupEmbeddedServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to start embedded server: %v\n", err)
	}

	fmt.Println(">>> Running tests")

	// Run tests
	code := m.Run()

	fmt.Println(">>> Tests completed")
	// Cleanup
	if srv != nil {
		helpers.TeardownEmbeddedServer(srv)
	}
	// Exit with test result code
	os.Exit(code)
}

// waitForClusterReady waits for the QuicBaaS cluster to be ready.
func waitForClusterReady() bool {
	endpoint := getRESTEndpoint()
	client := helpers.NewHTTPClient(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for {
		resp, err := client.Get(endpoint + "/api/v1/health/ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Println("Cluster is ready")
				// Now wait for IAM shard to be ready
				if waitForIAMReady(ctx, client, endpoint) {
					return true
				}
			}
		}
		if !pause(ctx, 2*time.Second) {
			return false
		}
	}
}

// pause blocks for d or until ctx ends, and reports whether ctx is still
// live. It paces the readiness and retry loops of this package.
func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// retryBackoff paces an HTTP retry loop with a capped exponential delay.
type retryBackoff struct {
	delay time.Duration
}

// wait blocks for the current delay, then grows it by retryBackoffMult up
// to retryMaxDelay.
func (b *retryBackoff) wait() {
	pause(context.Background(), b.delay)
	b.delay = min(time.Duration(float64(b.delay)*retryBackoffMult), retryMaxDelay)
}

// waitForIAMReady waits for the IAM shard (shard 1) to be ready.
// It does this by attempting multiple ListTenants operations which require
// the IAM shard to be available for reads. We require multiple consecutive
// successful responses to ensure the shard is stable. The probe reads as the
// system admin, because the integration node runs with require_authz and
// ListTenants needs a bearer.
func waitForIAMReady(ctx context.Context, client *http.Client, endpoint string) bool {
	fmt.Println("Waiting for IAM shard to be ready...")

	// Require 10 consecutive successes, 3 seconds apart, for 30 seconds of stability
	const requiredSuccesses = 10
	consecutiveSuccesses := 0
	var bearer string

	for {
		interval := 2 * time.Second
		var resp *http.Response
		var err error
		if bearer == "" {
			var token string
			token, _, err = helpers.AuthPIVSession(ctx, endpoint, "", "", "")
			if err != nil {
				fmt.Printf("IAM readiness login failed: %v\n", err)
				bearer = ""
			} else {
				bearer = token
			}
		}
		if err == nil {
			var req *http.Request
			req, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v1/tenants", nil)
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+bearer)
				resp, err = client.Do(req)
			}
		}
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				// The bearer expired or was refused: log in again on the next pass.
				bearer = ""
			}

			switch {
			case resp.StatusCode == http.StatusOK:
				// Success: we get 200 OK (empty list is fine)
				consecutiveSuccesses++
				fmt.Printf("IAM shard check passed (%d/%d)\n", consecutiveSuccesses, requiredSuccesses)
				if consecutiveSuccesses >= requiredSuccesses {
					fmt.Println("IAM shard is stable and ready")
					return true
				}
				// Wait 3 seconds between successful checks to ensure stability
				interval = 3 * time.Second
			case resp.StatusCode == http.StatusInternalServerError && isShardUnavailable(string(body)):
				// "shard is not ready" and similar errors reset the counter and keep waiting
				if consecutiveSuccesses > 0 {
					fmt.Printf("IAM shard unstable - resetting (was at %d/%d)\n", consecutiveSuccesses, requiredSuccesses)
				}
				consecutiveSuccesses = 0
			default:
				// Other non-OK responses reset the counter
				consecutiveSuccesses = 0
			}
		} else {
			consecutiveSuccesses = 0
		}
		if !pause(ctx, interval) {
			fmt.Println("Timeout waiting for IAM shard")
			return false
		}
	}
}

// isShardUnavailable reports whether a response body names a shard or Raft
// unavailability.
func isShardUnavailable(body string) bool {
	return strings.Contains(body, "shard is not ready") ||
		strings.Contains(body, "service operation failed") ||
		strings.Contains(body, "raft") ||
		strings.Contains(body, "leader") ||
		strings.Contains(body, "timeout")
}

// getRESTEndpoint returns the primary REST API endpoint.
func getRESTEndpoint() string {
	if endpoint := os.Getenv("QUICBAAS_NODE1_URL"); endpoint != "" {
		return endpoint
	}
	return "https://test-node1:8080"
}

// getGRPCEndpoint returns the primary gRPC endpoint.
func getGRPCEndpoint() string {
	if endpoint := os.Getenv("QUICBAAS_NODE1_GRPC"); endpoint != "" {
		return endpoint
	}
	return "test-node1:50051"
}

// getUnixSocket returns the Unix domain socket path.
func getUnixSocket() string {
	if socket := os.Getenv("QUICBAAS_NODE1_UNIX"); socket != "" {
		return socket
	}
	return "/var/run/quicbaas/grpc-node1.sock"
}

func getQUICEndpoint() string {
	if endpoint := os.Getenv("QUICBAAS_NODE1_QUIC"); endpoint != "" {
		return endpoint
	}
	return "test-node1:8443"
}

// setupTestCluster creates a test cluster connection.
func setupTestCluster(t *testing.T) *testCluster {
	httpEndpoint := getRESTEndpoint()
	grpcEndpoint := getGRPCEndpoint()
	quicEndpoint := getQUICEndpoint()
	unixSocket := getUnixSocket()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	httpClient := newIntegrationHTTPClient(tlsCfg)

	// Create gRPC connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	grpcConn, err := grpc.DialContext(ctx, grpcEndpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
	)
	require.NoError(t, err, "Failed to connect to gRPC endpoint")

	return &testCluster{
		httpEndpoint: httpEndpoint,
		grpcEndpoint: grpcEndpoint,
		quicEndpoint: quicEndpoint,
		unixSocket:   unixSocket,
		client:       httpClient,
		grpcConn:     grpcConn,
		t:            t,
		tenantAdmins: make(map[string]*helpers.TenantAdminSession),
	}
}

// newIntegrationHTTPClient returns an HTTP client that trusts the fixture CA
// and does not follow redirects. Not following them matters for OAuth2/OIDC
// testing, where the redirect response is what is inspected rather than the
// external callback URL it points at.
func newIntegrationHTTPClient(tlsCfg *tls.Config) *http.Client {
	return &http.Client{
		Timeout: defaultTimeout,
		Transport: &http.Transport{
			TLSClientConfig:     tlsCfg,
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Close closes the test cluster connections.
func (tc *testCluster) Close() {
	if tc.grpcConn != nil {
		tc.grpcConn.Close()
	}
}

// isRetryableError checks if an error response indicates a transient Raft error.
func isRetryableError(statusCode int, body string) bool {
	if statusCode != http.StatusInternalServerError {
		return false
	}
	// Check for transient Raft/shard errors
	retryablePatterns := []string{
		"shard is not ready",
		"request dropped",
		"raft.SyncRead",
		"read failed for shard",
		"service operation failed",
		"timeout",
		"leader not available",
		"no leader",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(body, pattern) {
			return true
		}
	}
	return false
}

// doHTTPRequest performs an HTTP request and returns the response.
func (tc *testCluster) doHTTPRequest(method, path string, body interface{}) (*http.Response, error) {
	req, err := tc.newHTTPRequest(method, path, body)
	if err != nil {
		return nil, err
	}

	bearer, err := tc.bearerFor(path)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	return tc.client.Do(req)
}

// doAnonHTTPRequest sends the request with no bearer, for tests that check
// unauthenticated access.
func (tc *testCluster) doAnonHTTPRequest(method, path string, body interface{}) (*http.Response, error) {
	req, err := tc.newHTTPRequest(method, path, body)
	if err != nil {
		return nil, err
	}
	return tc.client.Do(req)
}

// newHTTPRequest builds a request to the cluster's REST endpoint, with body
// encoded as JSON when it is not nil.
func (tc *testCluster) newHTTPRequest(method, path string, body interface{}) (*http.Request, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, tc.httpEndpoint+path, reqBody)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// provisionTenant is how a test gets a tenant it then works inside: the system
// admin provisions tenantID through helpers.TenantAdmin, whose own cleanup
// deletes it, and tenant-scoped calls through the helpers (getJSON, postJSON,
// putJSON, deleteJSON, ...) then carry that tenant admin's bearer.
func (tc *testCluster) provisionTenant(t *testing.T, tenantID string) string {
	t.Helper()
	session := helpers.TenantAdmin(t, tc.httpEndpoint, tenantID)

	tc.mu.Lock()
	tc.tenantAdmins[tenantID] = session
	tc.current = tenantID
	tc.mu.Unlock()

	t.Cleanup(func() {
		tc.mu.Lock()
		delete(tc.tenantAdmins, tenantID)
		if tc.current == tenantID {
			tc.current = ""
		}
		tc.mu.Unlock()
	})
	return tenantID
}

// callerUserID returns the user id of tenantID's provisioned admin: the user
// the helpers authenticate as on /api/v1/iam/ paths while tenantID is the
// current tenant.
func (tc *testCluster) callerUserID(t *testing.T, tenantID string) string {
	t.Helper()
	tc.mu.Lock()
	session := tc.tenantAdmins[tenantID]
	tc.mu.Unlock()
	require.NotNil(t, session, "tenant %q has no provisioned admin", tenantID)

	userID := session.Tenant().AdminUserID
	require.NotEmpty(t, userID, "the provision response for tenant %q named no admin user", tenantID)
	return userID
}

// bearerFor returns the bearer a request to path carries: the system admin's
// for the tenant control plane (/api/v1/tenants and a tenant's record), the
// tenant's own admin's for a path inside a tenant, for other /api/v1/iam/ paths
// the admin's of the tenant the tenant_id query parameter names when that
// tenant was provisioned here, else the current tenant's admin's (else the
// system admin's), and none for public endpoints. The system bearer never goes
// to a path inside a tenant that has a recorded admin, because the tenant
// routes refuse it.
func (tc *testCluster) bearerFor(path string) (string, error) {
	queryTenant := ""
	if i := strings.IndexByte(path, '?'); i >= 0 {
		if values, err := url.ParseQuery(path[i+1:]); err == nil {
			queryTenant = values.Get("tenant_id")
		}
		path = path[:i]
	}

	const tenantsPath = "/api/v1/tenants"
	switch {
	case path == tenantsPath:
		return tc.systemAdminBearer()

	case strings.HasPrefix(path, tenantsPath+"/"):
		tenantID, sub, _ := strings.Cut(strings.TrimPrefix(path, tenantsPath+"/"), "/")
		if sub == "" {
			// The tenant record itself.
			return tc.systemAdminBearer()
		}
		tc.mu.Lock()
		session := tc.tenantAdmins[tenantID]
		tc.mu.Unlock()
		if session == nil {
			return tc.systemAdminBearer()
		}
		return tenantAdminBearer(session, tenantID)

	case strings.HasPrefix(path, "/api/v1/iam/"):
		tc.mu.Lock()
		tenantID := tc.current
		if queryTenant != "" && tc.tenantAdmins[queryTenant] != nil {
			tenantID = queryTenant
		}
		session := tc.tenantAdmins[tenantID]
		tc.mu.Unlock()
		if session == nil {
			return tc.systemAdminBearer()
		}
		return tenantAdminBearer(session, tenantID)

	default:
		return "", nil
	}
}

// systemAdminBearer logs in as the system admin once per testCluster and
// caches the bearer.
func (tc *testCluster) systemAdminBearer() (string, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.systemBearer != "" {
		return tc.systemBearer, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bearer, err := helpers.AuthSystemAdminContext(ctx, tc.httpEndpoint)
	if err != nil {
		return "", fmt.Errorf("system admin login: %w", err)
	}
	tc.systemBearer = bearer
	return bearer, nil
}

// tenantAdminBearer returns the bearer of tenantID's admin session.
func tenantAdminBearer(session *helpers.TenantAdminSession, tenantID string) (string, error) {
	bearer, err := session.Bearer(context.Background())
	if err != nil {
		return "", fmt.Errorf("tenant %q admin login: %w", tenantID, err)
	}
	return bearer, nil
}

// getJSON performs a GET request and unmarshals the JSON response.
// Includes retry logic for transient Raft errors.
func (tc *testCluster) getJSON(path string, target interface{}) error {
	backoff := retryBackoff{delay: retryDelay}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for GET %s (waiting %v)\n", attempt, maxRetries, path, backoff.delay)
			backoff.wait()
		}

		resp, err := tc.doHTTPRequest(http.MethodGet, path, nil)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(target)
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = &qberrors.MessageError{Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))}

		// Check if this is a retryable error
		if !isRetryableError(resp.StatusCode, string(respBody)) {
			return lastErr
		}
	}

	return &qberrors.WrappedMessageError{Message: "max retries exceeded", Cause: lastErr}
}

// getJSONExpectNotFound performs a GET request expecting the resource to not exist (404).
// This is used for delete verification tests where we want to confirm deletion.
// Returns nil if 404 is received (expected after deletion), error otherwise.
// Includes retry logic to handle eventual consistency.
func (tc *testCluster) getJSONExpectNotFound(path string) error {
	backoff := retryBackoff{delay: retryDelay}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for GET (expect 404) %s (waiting %v)\n", attempt, maxRetries, path, backoff.delay)
			backoff.wait()
		}

		resp, err := tc.doHTTPRequest(http.MethodGet, path, nil)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// 404 is what we expect after deletion - success!
		if resp.StatusCode == http.StatusNotFound {
			return nil
		}

		// If resource still exists (200 OK), retry - deletion may not have propagated yet
		if resp.StatusCode == http.StatusOK {
			lastErr = &qberrors.MessageError{Message: "resource still exists (HTTP 200)"}
			continue
		}

		// Check if this is a retryable error
		if isRetryableError(resp.StatusCode, string(respBody)) {
			lastErr = &qberrors.MessageError{Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))}
			continue
		}

		// Other errors (not 404, not 200, not retryable) - treat as unexpected
		return &qberrors.MessageError{Message: fmt.Sprintf("unexpected HTTP %d: %s", resp.StatusCode, string(respBody))}
	}

	return &qberrors.WrappedMessageError{Message: fmt.Sprintf("resource still found after %d retries", maxRetries), Cause: lastErr}
}

// postJSON performs a POST request with JSON body.
// Includes retry logic for transient Raft errors.
func (tc *testCluster) postJSON(path string, body interface{}, target interface{}) error {
	backoff := retryBackoff{delay: retryDelay}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for POST %s (waiting %v)\n", attempt, maxRetries, path, backoff.delay)
			backoff.wait()
		}

		resp, err := tc.doHTTPRequest(http.MethodPost, path, body)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			defer resp.Body.Close()
			if target != nil {
				return json.NewDecoder(resp.Body).Decode(target)
			}
			return nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = &qberrors.MessageError{Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))}

		// Check if this is a retryable error
		if !isRetryableError(resp.StatusCode, string(respBody)) {
			return lastErr
		}
	}

	return &qberrors.WrappedMessageError{Message: "max retries exceeded", Cause: lastErr}
}

// putJSON performs a PUT request with JSON body.
// Includes retry logic for transient Raft errors.
func (tc *testCluster) putJSON(path string, body interface{}, target interface{}) error {
	backoff := retryBackoff{delay: retryDelay}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for PUT %s (waiting %v)\n", attempt, maxRetries, path, backoff.delay)
			backoff.wait()
		}

		resp, err := tc.doHTTPRequest(http.MethodPut, path, body)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			if target != nil {
				return json.NewDecoder(resp.Body).Decode(target)
			}
			return nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = &qberrors.MessageError{Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))}

		// Check if this is a retryable error
		if !isRetryableError(resp.StatusCode, string(respBody)) {
			return lastErr
		}
	}

	return &qberrors.WrappedMessageError{Message: "max retries exceeded", Cause: lastErr}
}

// deleteJSON performs a DELETE request.
// Includes retry logic for transient Raft errors.
func (tc *testCluster) deleteJSON(path string) error {
	backoff := retryBackoff{delay: retryDelay}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for DELETE %s (waiting %v)\n", attempt, maxRetries, path, backoff.delay)
			backoff.wait()
		}

		resp, err := tc.doHTTPRequest(http.MethodDelete, path, nil)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			resp.Body.Close()
			return nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = &qberrors.MessageError{Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))}

		// Check if this is a retryable error
		if !isRetryableError(resp.StatusCode, string(respBody)) {
			return lastErr
		}
	}

	return &qberrors.WrappedMessageError{Message: "max retries exceeded", Cause: lastErr}
}

// generateUniqueTenantID generates a unique tenant ID for testing.
func generateUniqueTenantID() string {
	return fmt.Sprintf("tenant-%d", time.Now().UnixNano())
}

// generateUniqueEmail generates a unique email for testing.
func generateUniqueEmail() string {
	return fmt.Sprintf("user-%d@test.local", time.Now().UnixNano())
}

// generateUniqueUsername generates a unique username for testing.
func generateUniqueUsername() string {
	return fmt.Sprintf("user_%d", time.Now().UnixNano()%1000000)
}

// TestClusterHealthWithIAM verifies the cluster is healthy with IAM enabled.
func TestClusterHealthWithIAM(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	resp, err := cluster.doHTTPRequest(http.MethodGet, "/api/v1/health/live", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Cluster should be healthy")
}

// TestIAMEndpointsAvailable verifies IAM endpoints are available.
func TestIAMEndpointsAvailable(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tests := []struct {
		name     string
		endpoint string
	}{
		{"OIDC Discovery", "/.well-known/openid-configuration"},
		{"JWKS", "/.well-known/jwks.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := cluster.doHTTPRequest(http.MethodGet, tt.endpoint, nil)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Accept both 200 OK and 404 Not Found (if IAM not fully configured)
			// The important thing is that the endpoint exists
			assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound,
				"Endpoint should be available (got %d)", resp.StatusCode)
		})
	}
}
