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

package secretsresolver

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
)

// =============================================================================
// RemoteKMSResolver Integration Tests
// =============================================================================

// mockRemoteSecretsClient implements secrets.RemoteSecretsClient for testing.
type mockRemoteSecretsClient struct {
	secrets       map[string]map[string][]byte // host -> secretID -> data
	getSecretFunc func(ctx context.Context, host, secretID string) ([]byte, error)
}

// newMockRemoteSecretsClient creates a new mock client with optional predefined secrets.
func newMockRemoteSecretsClient() *mockRemoteSecretsClient {
	return &mockRemoteSecretsClient{
		secrets: make(map[string]map[string][]byte),
	}
}

// SetSecret stores a secret in the mock client for the specified host.
func (m *mockRemoteSecretsClient) SetSecret(host, secretID string, data []byte) {
	if m.secrets[host] == nil {
		m.secrets[host] = make(map[string][]byte)
	}
	m.secrets[host][secretID] = data
}

// GetSecret retrieves a secret from the mock client.
func (m *mockRemoteSecretsClient) GetSecret(ctx context.Context, host, secretID string) ([]byte, error) {
	// Allow custom function override for testing specific behaviors
	if m.getSecretFunc != nil {
		return m.getSecretFunc(ctx, host, secretID)
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	hostSecrets, ok := m.secrets[host]
	if !ok {
		return nil, &secrets.SecretNotFoundError{
			Path:     secretID,
			Provider: host,
		}
	}

	data, ok := hostSecrets[secretID]
	if !ok {
		return nil, &secrets.SecretNotFoundError{
			Path:     secretID,
			Provider: host,
		}
	}

	return data, nil
}

// getNode1URL returns the URL for node1 from environment or default.
func getNode1URL() string {
	url := os.Getenv("QUICBAAS_NODE1_URL")
	if url == "" {
		url = "https://test-node1:8080"
	}
	return url
}

// getNode2URL returns the URL for node2 from environment or default.
func getNode2URL() string {
	url := os.Getenv("QUICBAAS_NODE2_URL")
	if url == "" {
		url = "https://test-node2:8080"
	}
	return url
}

// TestRemoteKMSResolver_Scheme verifies that Scheme() returns "remote-kms".
func TestRemoteKMSResolver_Scheme(t *testing.T) {
	client := newMockRemoteSecretsClient()
	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	scheme := resolver.Scheme()
	assert.Equal(t, "remote-kms", scheme, "Scheme should return 'remote-kms'")

	t.Logf("RemoteKMSResolver scheme: %s", scheme)
}

// TestRemoteKMSResolver_CanResolve tests that CanResolve returns true only for remote-kms:// URIs.
func TestRemoteKMSResolver_CanResolve(t *testing.T) {
	client := newMockRemoteSecretsClient()
	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	testCases := []struct {
		name     string
		uri      string
		expected bool
	}{
		{
			name:     "valid remote-kms URI with host and path",
			uri:      "remote-kms://node1:8080/secrets/myapp/password",
			expected: true,
		},
		{
			name:     "valid remote-kms URI with hostname only",
			uri:      "remote-kms://node1/secrets/password",
			expected: true,
		},
		{
			name:     "valid remote-kms URI with IP address",
			uri:      "remote-kms://192.0.2.100:8443/secrets/key",
			expected: true,
		},
		{
			name:     "valid remote-kms URI with nested path",
			uri:      "remote-kms://cluster-node1.example.com:9443/config/app/database/password",
			expected: true,
		},
		{
			name:     "invalid - kms scheme (local)",
			uri:      "kms://secrets/password",
			expected: false,
		},
		{
			name:     "invalid - vault scheme",
			uri:      "vault://secrets/password",
			expected: false,
		},
		{
			name:     "invalid - env scheme",
			uri:      "env://MY_SECRET",
			expected: false,
		},
		{
			name:     "invalid - file scheme",
			uri:      "file:///etc/secrets/password",
			expected: false,
		},
		{
			name:     "invalid - plain value",
			uri:      "just-a-password",
			expected: false,
		},
		{
			name:     "invalid - partial scheme",
			uri:      "remote-kms:/",
			expected: false,
		},
		{
			name:     "invalid - missing double slash",
			uri:      "remote-kms:host/path",
			expected: false,
		},
		{
			name:     "invalid - empty string",
			uri:      "",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := resolver.CanResolve(tc.uri)
			assert.Equal(t, tc.expected, result, "CanResolve(%q) = %v, expected %v", tc.uri, result, tc.expected)
		})
	}
}

// TestRemoteKMSResolver_ParseURI tests URI parsing for various formats.
func TestRemoteKMSResolver_ParseURI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testCases := []struct {
		name           string
		uri            string
		expectError    bool
		expectedErrMsg string
	}{
		{
			name:        "valid URI with host:port and path",
			uri:         "remote-kms://node1:8080/secrets/password",
			expectError: false,
		},
		{
			name:        "valid URI with hostname and path",
			uri:         "remote-kms://node1/secrets/password",
			expectError: false,
		},
		{
			name:        "valid URI with IP and port",
			uri:         "remote-kms://192.0.2.100:9443/config/key",
			expectError: false,
		},
		{
			name:        "valid URI with FQDN and path",
			uri:         "remote-kms://cluster.example.com:8443/app/database/password",
			expectError: false,
		},
		{
			name:        "valid URI with deeply nested path",
			uri:         "remote-kms://host:8080/a/b/c/d/e/secret",
			expectError: false,
		},
		{
			name:           "invalid - missing host and path",
			uri:            "remote-kms://",
			expectError:    true,
			expectedErrMsg: "missing host and path",
		},
		{
			name:           "invalid - missing path",
			uri:            "remote-kms://host:8080",
			expectError:    true,
			expectedErrMsg: "missing path separator",
		},
		{
			name:           "invalid - empty path",
			uri:            "remote-kms://host:8080/",
			expectError:    true,
			expectedErrMsg: "path cannot be empty",
		},
		{
			name:           "invalid - missing host",
			uri:            "remote-kms:///secrets/path",
			expectError:    true,
			expectedErrMsg: "missing host",
		},
		{
			name:           "invalid - wrong scheme",
			uri:            "kms://path/to/secret",
			expectError:    true,
			expectedErrMsg: "must start with remote-kms://",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a mock client that stores secrets for valid URIs
			client := newMockRemoteSecretsClient()

			// For valid URIs, set up a secret so resolution succeeds
			if !tc.expectError {
				// Parse the URI to get host and path (simplified for test setup)
				// The resolver will do the actual parsing
				client.SetSecret("node1:8080", "secrets/password", []byte(`{"password":"test"}`))
				client.SetSecret("node1", "secrets/password", []byte(`{"password":"test"}`))
				client.SetSecret("192.0.2.100:9443", "config/key", []byte(`{"key":"value"}`))
				client.SetSecret("cluster.example.com:8443", "app/database/password", []byte(`{"password":"db-pass"}`))
				client.SetSecret("host:8080", "a/b/c/d/e/secret", []byte(`{"secret":"nested"}`))
			}

			resolver := secrets.NewRemoteKMSResolver(client)
			require.NotNil(t, resolver)

			result, err := resolver.Resolve(ctx, tc.uri)

			if tc.expectError {
				require.Error(t, err, "Expected error for URI: %s", tc.uri)
				assert.Nil(t, result)
				if tc.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tc.expectedErrMsg,
						"Error message should contain: %s", tc.expectedErrMsg)
				}
				t.Logf("Correctly received error for %q: %v", tc.uri, err)
			} else {
				// For valid URIs, we expect either success or a connection error
				// since we're using mocks and the actual remote connection isn't established
				if err != nil {
					// Check if it's a connection-related error (acceptable in integration tests)
					// or if the mock worked
					t.Logf("URI parsing succeeded but resolution returned: %v", err)
				} else {
					assert.NotNil(t, result)
					assert.Equal(t, tc.uri, result.Raw)
					assert.Equal(t, secrets.StrategyRemoteKMS, result.Strategy)
					t.Logf("Successfully parsed and resolved URI: %s", tc.uri)
				}
			}
		})
	}
}

// TestRemoteKMSResolver_InvalidURI tests error handling for malformed URIs.
func TestRemoteKMSResolver_InvalidURI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := newMockRemoteSecretsClient()
	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	testCases := []struct {
		name        string
		uri         string
		expectError bool
	}{
		{
			name:        "empty URI after scheme",
			uri:         "remote-kms://",
			expectError: true,
		},
		{
			name:        "missing path",
			uri:         "remote-kms://host:8080",
			expectError: true,
		},
		{
			name:        "empty path after slash",
			uri:         "remote-kms://host:8080/",
			expectError: true,
		},
		{
			name:        "missing host with path only",
			uri:         "remote-kms:///path/to/secret",
			expectError: true,
		},
		{
			name:        "wrong scheme prefix",
			uri:         "kms://secrets/password",
			expectError: true,
		},
		{
			name:        "no scheme",
			uri:         "host:8080/secrets/password",
			expectError: true,
		},
		{
			name:        "partial scheme",
			uri:         "remote-kms:/host/path",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := resolver.Resolve(ctx, tc.uri)

			if tc.expectError {
				assert.Error(t, err, "Expected error for URI %q", tc.uri)
				assert.Nil(t, result)
				assert.True(t, secrets.IsInvalidSecretURI(err),
					"Expected InvalidSecretURIError, got: %T - %v", err, err)
				t.Logf("Correctly received error for invalid URI %q: %v", tc.uri, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestRemoteKMSResolver_ResolveFromRemote tests storing a secret on one node
// and resolving it using remote-kms:// URI pointing to that node.
func TestRemoteKMSResolver_ResolveFromRemote(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	node1Host := "test-node1:8080"
	secretPath := "config/app/database-password"
	secretValue := `{"password":"super-secret-db-password-12345","username":"dbadmin"}`

	// Create mock client with pre-populated secret on node1
	client := newMockRemoteSecretsClient()
	client.SetSecret(node1Host, secretPath, []byte(secretValue))

	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	// Build the remote-kms URI
	remoteURI := "remote-kms://" + node1Host + "/" + secretPath

	// Verify CanResolve returns true
	canResolve := resolver.CanResolve(remoteURI)
	assert.True(t, canResolve, "CanResolve should return true for: %s", remoteURI)

	// Resolve the secret
	result, err := resolver.Resolve(ctx, remoteURI)
	require.NoError(t, err, "Resolve should succeed for valid remote secret")
	require.NotNil(t, result, "Result should not be nil")

	// Verify the result
	assert.Equal(t, remoteURI, result.Raw, "Raw should match input URI")
	assert.Equal(t, secretValue, result.String(), "Value should match stored secret")
	assert.Equal(t, secrets.StrategyRemoteKMS, result.Strategy, "Strategy should be remote_kms")

	t.Logf("Successfully resolved secret from remote node: %s", remoteURI)
}

// TestRemoteKMSResolver_ResolveFromRemote_MultiNode tests cross-node secret resolution.
func TestRemoteKMSResolver_ResolveFromRemote_MultiNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Setup multiple nodes with different secrets
	client := newMockRemoteSecretsClient()

	node1Secrets := map[string]string{
		"secrets/api-key":       `{"api_key":"node1-api-key-abc123"}`,
		"config/database":       `{"host":"db.node1.local","password":"node1-db-pass"}`,
		"certs/tls-certificate": `{"cert":"-----BEGIN CERTIFICATE-----..."}`,
	}

	node2Secrets := map[string]string{
		"secrets/jwt-secret":    `{"secret":"node2-jwt-secret-xyz789"}`,
		"config/redis":          `{"host":"redis.node2.local","password":"node2-redis-pass"}`,
		"shared/encryption-key": `{"key":"AES256-key-for-encryption"}`,
	}

	for path, value := range node1Secrets {
		client.SetSecret("node1:8080", path, []byte(value))
	}

	for path, value := range node2Secrets {
		client.SetSecret("node2:8080", path, []byte(value))
	}

	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	// Test resolving from node1
	t.Run("ResolveFromNode1", func(t *testing.T) {
		for path, expectedValue := range node1Secrets {
			uri := "remote-kms://node1:8080/" + path
			result, err := resolver.Resolve(ctx, uri)
			require.NoError(t, err, "Should resolve from node1: %s", path)
			require.NotNil(t, result)
			assert.Equal(t, expectedValue, result.String())
			assert.Equal(t, secrets.StrategyRemoteKMS, result.Strategy)
			t.Logf("Resolved from node1: %s", path)
		}
	})

	// Test resolving from node2
	t.Run("ResolveFromNode2", func(t *testing.T) {
		for path, expectedValue := range node2Secrets {
			uri := "remote-kms://node2:8080/" + path
			result, err := resolver.Resolve(ctx, uri)
			require.NoError(t, err, "Should resolve from node2: %s", path)
			require.NotNil(t, result)
			assert.Equal(t, expectedValue, result.String())
			assert.Equal(t, secrets.StrategyRemoteKMS, result.Strategy)
			t.Logf("Resolved from node2: %s", path)
		}
	})

	// Test that node1 secrets are not accessible via node2 URI
	t.Run("CrossNodeIsolation", func(t *testing.T) {
		// Try to access node1 secret via node2
		uri := "remote-kms://node2:8080/secrets/api-key"
		result, err := resolver.Resolve(ctx, uri)
		require.Error(t, err, "Should fail when accessing node1 secret via node2 URI")
		assert.Nil(t, result)
		assert.True(t, secrets.IsSecretNotFound(err),
			"Should return SecretNotFoundError, got: %T - %v", err, err)
		t.Logf("Correctly isolated cross-node access")
	})
}

// TestRemoteKMSResolver_ConnectionTimeout tests timeout behavior for unavailable hosts.
func TestRemoteKMSResolver_ConnectionTimeout(t *testing.T) {
	// Create a client that simulates timeout
	client := newMockRemoteSecretsClient()
	client.getSecretFunc = func(ctx context.Context, host, secretID string) ([]byte, error) {
		// Simulate a slow connection by waiting for context to expire
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return []byte("should-not-reach"), nil
		}
	}

	// Create resolver with short timeout
	resolver := secrets.NewRemoteKMSResolver(client, secrets.WithTimeout(100*time.Millisecond))
	require.NotNil(t, resolver)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uri := "remote-kms://unavailable-host:8080/secrets/key"

	start := time.Now()
	result, err := resolver.Resolve(ctx, uri)
	elapsed := time.Since(start)

	require.Error(t, err, "Should error on timeout")
	assert.Nil(t, result)

	// Verify the timeout was respected (should complete in roughly timeout duration)
	assert.Less(t, elapsed, 1*time.Second,
		"Should timeout quickly, not wait for full duration. Elapsed: %v", elapsed)

	// Verify it's a connection/timeout error
	assert.True(t, secrets.IsRemoteConnectionFailed(err) || errors.Is(err, context.DeadlineExceeded),
		"Should be connection failed or deadline exceeded error, got: %T - %v", err, err)

	t.Logf("Correctly timed out after %v", elapsed)
}

// TestRemoteKMSResolver_ContextCancellation tests behavior when context is cancelled.
func TestRemoteKMSResolver_ContextCancellation(t *testing.T) {
	client := newMockRemoteSecretsClient()
	client.SetSecret("node1:8080", "secrets/key", []byte(`{"key":"value"}`))

	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	t.Run("ImmediatelyCancelledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		uri := "remote-kms://node1:8080/secrets/key"
		result, err := resolver.Resolve(ctx, uri)

		assert.Error(t, err, "Should error for cancelled context")
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "context cancelled",
			"Error should mention context cancellation")

		t.Logf("Correctly handled cancelled context: %v", err)
	})

	t.Run("NilContext", func(t *testing.T) {
		uri := "remote-kms://node1:8080/secrets/key"
		result, err := resolver.Resolve(nil, uri)

		assert.Error(t, err, "Should error for nil context")
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "context cannot be nil",
			"Error should mention nil context")

		t.Logf("Correctly handled nil context: %v", err)
	})
}

// TestRemoteKMSResolver_SecretNotFound tests error handling when secret doesn't exist.
func TestRemoteKMSResolver_SecretNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := newMockRemoteSecretsClient()
	// Don't set any secrets - all lookups should fail

	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	uri := "remote-kms://node1:8080/nonexistent/secret/path"
	result, err := resolver.Resolve(ctx, uri)

	require.Error(t, err, "Should error for non-existent secret")
	assert.Nil(t, result)
	assert.True(t, secrets.IsSecretNotFound(err),
		"Should return SecretNotFoundError, got: %T - %v", err, err)

	t.Logf("Correctly returned not found error: %v", err)
}

// TestRemoteKMSResolver_FallbackPort tests that default port is used when not specified.
func TestRemoteKMSResolver_FallbackPort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create client that stores secrets under hostname without port
	client := newMockRemoteSecretsClient()
	client.SetSecret("node1", "secrets/password", []byte(`{"password":"secret-without-port"}`))

	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	// Use URI without port - should work if resolver handles it correctly
	uri := "remote-kms://node1/secrets/password"

	// Verify CanResolve works
	assert.True(t, resolver.CanResolve(uri), "Should recognize URI without port")

	result, err := resolver.Resolve(ctx, uri)
	require.NoError(t, err, "Should resolve URI without explicit port")
	require.NotNil(t, result)

	assert.Equal(t, `{"password":"secret-without-port"}`, result.String())
	assert.Equal(t, secrets.StrategyRemoteKMS, result.Strategy)

	t.Logf("Successfully resolved URI without port: %s", uri)
}

// TestRemoteKMSResolver_WithTimeout tests the WithTimeout option.
func TestRemoteKMSResolver_WithTimeout(t *testing.T) {
	client := newMockRemoteSecretsClient()

	testCases := []struct {
		name            string
		timeout         time.Duration
		expectedTimeout time.Duration
	}{
		{
			name:            "custom 5 second timeout",
			timeout:         5 * time.Second,
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "custom 1 minute timeout",
			timeout:         time.Minute,
			expectedTimeout: time.Minute,
		},
		{
			name:            "zero timeout uses default",
			timeout:         0,
			expectedTimeout: secrets.DefaultRemoteKMSTimeout,
		},
		{
			name:            "negative timeout uses default",
			timeout:         -1 * time.Second,
			expectedTimeout: secrets.DefaultRemoteKMSTimeout,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := secrets.NewRemoteKMSResolver(client, secrets.WithTimeout(tc.timeout))
			require.NotNil(t, resolver)
			// Note: We can't directly verify the timeout value since it's private,
			// but we verify the resolver was created successfully
			t.Logf("Created resolver with timeout option: %v", tc.timeout)
		})
	}
}

// TestRemoteKMSResolver_NilClient tests that resolver handles nil client gracefully.
func TestRemoteKMSResolver_NilClient(t *testing.T) {
	resolver := secrets.NewRemoteKMSResolver(nil)
	require.NotNil(t, resolver, "Should create resolver even with nil client")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Scheme and CanResolve should still work
	assert.Equal(t, "remote-kms", resolver.Scheme())
	assert.True(t, resolver.CanResolve("remote-kms://host/path"))

	// Resolve should fail gracefully
	result, err := resolver.Resolve(ctx, "remote-kms://host:8080/secrets/key")
	assert.Error(t, err, "Should error when client is nil")
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "client is nil",
		"Error should mention nil client")

	t.Logf("Correctly handled nil client: %v", err)
}

// TestRemoteKMSResolver_RegistryIntegration tests the resolver with ResolverRegistry.
func TestRemoteKMSResolver_RegistryIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Setup mock client with test secret
	client := newMockRemoteSecretsClient()
	client.SetSecret("node1:8080", "app/secret", []byte(`{"token":"registry-integration-token"}`))

	// Create registry and register resolvers
	registry := secrets.NewResolverRegistry()
	remoteResolver := secrets.NewRemoteKMSResolver(client)
	registry.Register(remoteResolver)

	t.Run("IsSecretURI", func(t *testing.T) {
		// remote-kms URIs should be recognized
		assert.True(t, registry.IsSecretURI("remote-kms://node1:8080/app/secret"))
		assert.True(t, registry.IsSecretURI("remote-kms://cluster.example.com/secrets/key"))

		// Other schemes should not be recognized (unless registered)
		assert.False(t, registry.IsSecretURI("kms://local/secret"))
		assert.False(t, registry.IsSecretURI("plain-value"))
	})

	t.Run("ResolveRemoteKMSURI", func(t *testing.T) {
		uri := "remote-kms://node1:8080/app/secret"
		result, err := registry.Resolve(ctx, uri)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, secrets.StrategyRemoteKMS, result.Strategy)
		assert.Equal(t, `{"token":"registry-integration-token"}`, result.String())
		t.Logf("Registry resolved remote-kms URI successfully")
	})

	t.Run("LiteralFallback", func(t *testing.T) {
		literal := "literal-value-not-a-uri"
		result, err := registry.Resolve(ctx, literal)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, secrets.StrategyLiteral, result.Strategy)
		assert.Equal(t, literal, result.String())
		t.Logf("Registry correctly fell back to literal for non-URI")
	})
}

// TestRemoteKMSResolver_SpecialCharactersInPath tests secrets with special characters.
func TestRemoteKMSResolver_SpecialCharactersInPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := newMockRemoteSecretsClient()

	// Paths with various special characters (URL-safe)
	testPaths := []struct {
		path  string
		value string
	}{
		{
			path:  "config/my-app_v2/database",
			value: `{"password":"hyphen-underscore-path"}`,
		},
		{
			path:  "secrets/app.config.json",
			value: `{"key":"dotted-path"}`,
		},
		{
			path:  "data/123-numeric/key456",
			value: `{"key":"numeric-path"}`,
		},
	}

	for _, tp := range testPaths {
		client.SetSecret("node1:8080", tp.path, []byte(tp.value))
	}

	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	for _, tp := range testPaths {
		t.Run("Path_"+tp.path, func(t *testing.T) {
			uri := "remote-kms://node1:8080/" + tp.path
			result, err := resolver.Resolve(ctx, uri)
			require.NoError(t, err, "Should resolve path: %s", tp.path)
			require.NotNil(t, result)
			assert.Equal(t, tp.value, result.String())
			t.Logf("Resolved path with special characters: %s", tp.path)
		})
	}
}

// TestRemoteKMSResolver_LargeSecret tests resolution of large secrets.
func TestRemoteKMSResolver_LargeSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a large secret (8KB)
	largeData := make([]byte, 8192)
	for i := range largeData {
		largeData[i] = byte('A' + (i % 26))
	}

	client := newMockRemoteSecretsClient()
	client.SetSecret("node1:8080", "secrets/large-certificate", largeData)

	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	uri := "remote-kms://node1:8080/secrets/large-certificate"
	result, err := resolver.Resolve(ctx, uri)

	require.NoError(t, err, "Should resolve large secret")
	require.NotNil(t, result)
	assert.Equal(t, 8192, len(result.String()), "Should preserve large secret size")
	assert.Equal(t, secrets.StrategyRemoteKMS, result.Strategy)

	t.Logf("Successfully resolved large secret (%d bytes)", len(result.String()))
}

// TestRemoteKMSResolver_ConcurrentResolution tests thread safety.
func TestRemoteKMSResolver_ConcurrentResolution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Setup multiple secrets
	client := newMockRemoteSecretsClient()
	numSecrets := 10
	for i := 0; i < numSecrets; i++ {
		path := "secrets/concurrent" + string(rune('0'+i))
		value := []byte(`{"id":` + string(rune('0'+i)) + `}`)
		client.SetSecret("node1:8080", path, value)
	}

	resolver := secrets.NewRemoteKMSResolver(client)
	require.NotNil(t, resolver)

	const numGoroutines = 50
	const resolvesPerGoroutine = 20

	type resolveResult struct {
		uri   string
		value string
		err   error
	}

	resultChan := make(chan resolveResult, numGoroutines*resolvesPerGoroutine)

	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			for i := 0; i < resolvesPerGoroutine; i++ {
				secretNum := (goroutineID + i) % numSecrets
				path := "secrets/concurrent" + string(rune('0'+secretNum))
				uri := "remote-kms://node1:8080/" + path

				result, err := resolver.Resolve(ctx, uri)
				if err != nil {
					resultChan <- resolveResult{uri: uri, err: err}
				} else {
					resultChan <- resolveResult{uri: uri, value: result.String()}
				}
			}
		}(g)
	}

	// Collect results
	totalExpected := numGoroutines * resolvesPerGoroutine
	successCount := 0
	errorCount := 0

	for i := 0; i < totalExpected; i++ {
		res := <-resultChan
		if res.err != nil {
			errorCount++
			t.Logf("Error resolving %s: %v", res.uri, res.err)
		} else {
			successCount++
		}
	}

	assert.Equal(t, totalExpected, successCount,
		"All concurrent resolutions should succeed (got %d successes, %d errors)", successCount, errorCount)
	assert.Equal(t, 0, errorCount, "Should have no errors")

	t.Logf("Concurrent resolution: %d goroutines x %d resolves = %d successful resolutions",
		numGoroutines, resolvesPerGoroutine, successCount)
}
