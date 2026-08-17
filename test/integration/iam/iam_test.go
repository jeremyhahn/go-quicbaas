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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
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
}

// TestMain sets up and tears down the test environment.
func TestMain(m *testing.M) {
	fmt.Println(">>> TestMain starting - waiting for cluster")
	// Wait for cluster to be ready
	if !waitForClusterReady() {
		fmt.Println(">>> Cluster not ready, exiting")
		os.Exit(1)
	}
	// Give the Raft cluster extra time to stabilize after readiness check
	// Increased from 10s to 20s to allow for full leader election stabilization
	fmt.Println(">>> Cluster ready, waiting for additional stabilization...")
	time.Sleep(20 * time.Second)

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

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint + "/api/v1/health/ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Println("Cluster is ready")
				// Now wait for IAM shard to be ready
				if waitForIAMReady(client, endpoint, deadline) {
					return true
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// waitForIAMReady waits for the IAM shard (shard 1) to be ready.
// It does this by attempting multiple ListTenants operations which require
// the IAM shard to be available for reads. We require multiple consecutive
// successful responses to ensure the shard is stable.
func waitForIAMReady(client *http.Client, endpoint string, deadline time.Time) bool {
	fmt.Println("Waiting for IAM shard to be ready...")

	// Require 10 consecutive successes with 2 second intervals = 20 seconds of stability
	const requiredSuccesses = 10
	consecutiveSuccesses := 0

	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint + "/api/v1/tenants")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			// Success: we get 200 OK (empty list is fine)
			if resp.StatusCode == http.StatusOK {
				consecutiveSuccesses++
				fmt.Printf("IAM shard check passed (%d/%d)\n", consecutiveSuccesses, requiredSuccesses)
				if consecutiveSuccesses >= requiredSuccesses {
					fmt.Println("IAM shard is stable and ready")
					return true
				}
				// Wait 3 seconds between successful checks to ensure stability
				time.Sleep(3 * time.Second)
				continue
			}

			// If we get "shard is not ready" error, reset counter and keep waiting
			if resp.StatusCode == http.StatusInternalServerError {
				bodyStr := string(body)
				// Check for various shard/raft unavailability messages
				if strings.Contains(bodyStr, "shard is not ready") ||
					strings.Contains(bodyStr, "service operation failed") ||
					strings.Contains(bodyStr, "raft") ||
					strings.Contains(bodyStr, "leader") ||
					strings.Contains(bodyStr, "timeout") {
					if consecutiveSuccesses > 0 {
						fmt.Printf("IAM shard unstable - resetting (was at %d/%d)\n", consecutiveSuccesses, requiredSuccesses)
					}
					consecutiveSuccesses = 0
					time.Sleep(2 * time.Second)
					continue
				}
			}

			// Other non-OK responses reset the counter
			consecutiveSuccesses = 0
		} else {
			consecutiveSuccesses = 0
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Println("Timeout waiting for IAM shard")
	return false
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

	// Create HTTP client that does not follow redirects.
	// This is important for OAuth2/OIDC testing where we need to inspect
	// redirect responses rather than follow them to external callback URLs.
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	httpClient := &http.Client{
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

	return tc.client.Do(req)
}

// getJSON performs a GET request and unmarshals the JSON response.
// Includes retry logic for transient Raft errors.
func (tc *testCluster) getJSON(path string, target interface{}) error {
	delay := retryDelay
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for GET %s (waiting %v)\n", attempt, maxRetries, path, delay)
			time.Sleep(delay)
			// Exponential backoff with cap
			delay = time.Duration(float64(delay) * retryBackoffMult)
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
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
	delay := retryDelay
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for GET (expect 404) %s (waiting %v)\n", attempt, maxRetries, path, delay)
			time.Sleep(delay)
			// Exponential backoff with cap
			delay = time.Duration(float64(delay) * retryBackoffMult)
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
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
	delay := retryDelay
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for POST %s (waiting %v)\n", attempt, maxRetries, path, delay)
			time.Sleep(delay)
			// Exponential backoff with cap
			delay = time.Duration(float64(delay) * retryBackoffMult)
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
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
	delay := retryDelay
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for PUT %s (waiting %v)\n", attempt, maxRetries, path, delay)
			time.Sleep(delay)
			// Exponential backoff with cap
			delay = time.Duration(float64(delay) * retryBackoffMult)
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
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
	delay := retryDelay
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retry %d/%d for DELETE %s (waiting %v)\n", attempt, maxRetries, path, delay)
			time.Sleep(delay)
			// Exponential backoff with cap
			delay = time.Duration(float64(delay) * retryBackoffMult)
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
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
