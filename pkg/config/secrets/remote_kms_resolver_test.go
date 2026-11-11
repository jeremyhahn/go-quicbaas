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

package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// mockRemoteSecretsClient is a mock implementation of RemoteSecretsClient for testing.
type mockRemoteSecretsClient struct {
	secrets      map[string]map[string][]byte // host -> secretID -> data
	err          error
	delay        time.Duration
	callCount    int
	lastHost     string
	lastSecretID string
}

func newMockRemoteSecretsClient() *mockRemoteSecretsClient {
	return &mockRemoteSecretsClient{
		secrets: make(map[string]map[string][]byte),
	}
}

func (m *mockRemoteSecretsClient) withSecret(host, secretID string, data []byte) *mockRemoteSecretsClient {
	if m.secrets[host] == nil {
		m.secrets[host] = make(map[string][]byte)
	}
	m.secrets[host][secretID] = data
	return m
}

func (m *mockRemoteSecretsClient) withError(err error) *mockRemoteSecretsClient {
	m.err = err
	return m
}

func (m *mockRemoteSecretsClient) withDelay(d time.Duration) *mockRemoteSecretsClient {
	m.delay = d
	return m
}

func (m *mockRemoteSecretsClient) GetSecret(ctx context.Context, host string, secretID string) ([]byte, error) {
	m.callCount++
	m.lastHost = host
	m.lastSecretID = secretID

	// Simulate network delay
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}

	// Check for pre-configured error
	if m.err != nil {
		return nil, m.err
	}

	// Look up the secret
	hostSecrets, ok := m.secrets[host]
	if !ok {
		return nil, NewSecretNotFoundError("mockRemoteSecretsClient.GetSecret", secretID, host).Err
	}

	data, ok := hostSecrets[secretID]
	if !ok {
		return nil, NewSecretNotFoundError("mockRemoteSecretsClient.GetSecret", secretID, host).Err
	}

	return data, nil
}

func TestRemoteKMSResolver_Scheme(t *testing.T) {
	client := newMockRemoteSecretsClient()
	resolver := NewRemoteKMSResolver(client)

	got := resolver.Scheme()
	want := "remote-kms"

	if got != want {
		t.Errorf("Scheme() = %q, want %q", got, want)
	}
}

func TestRemoteKMSResolver_CanResolve(t *testing.T) {
	client := newMockRemoteSecretsClient()
	resolver := NewRemoteKMSResolver(client)

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		// Positive cases
		{name: "valid remote-kms URI", raw: "remote-kms://host:9443/path/to/secret", want: true},
		{name: "remote-kms with localhost", raw: "remote-kms://localhost:9443/secret", want: true},
		{name: "remote-kms with IP", raw: "remote-kms://192.0.2.100:9443/db/password", want: true},
		{name: "remote-kms with long path", raw: "remote-kms://host:9443/very/long/path/to/secret", want: true},
		{name: "remote-kms without port", raw: "remote-kms://host/secret", want: true},

		// Negative cases
		{name: "empty string", raw: "", want: false},
		{name: "plain text", raw: "my-secret", want: false},
		{name: "local kms scheme", raw: "kms://local/secret", want: false},
		{name: "env scheme", raw: "env://MY_SECRET", want: false},
		{name: "file scheme", raw: "file:///path/to/secret", want: false},
		{name: "http scheme", raw: "http://host/secret", want: false},
		{name: "https scheme", raw: "https://host/secret", want: false},
		{name: "remote-kms without colon slash slash", raw: "remote-kms:host/secret", want: false},
		{name: "remote-kms prefix only", raw: "remote-kms://", want: true}, // Has prefix, validation happens in Resolve
		{name: "case sensitive - uppercase", raw: "REMOTE-KMS://host/secret", want: false},
		{name: "similar but different", raw: "remotekms://host/secret", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolver.CanResolve(tt.raw)
			if got != tt.want {
				t.Errorf("CanResolve(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestRemoteKMSResolver_Resolve_Success(t *testing.T) {
	secretData := []byte("super-secret-value")
	client := newMockRemoteSecretsClient().
		withSecret("host.example.com:9443", "path/to/secret", secretData)

	resolver := NewRemoteKMSResolver(client)
	ctx := context.Background()

	result, err := resolver.Resolve(ctx, "remote-kms://host.example.com:9443/path/to/secret")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}

	if result == nil {
		t.Fatal("Resolve() returned nil result")
	}

	// Verify strategy
	if result.Strategy != StrategyRemoteKMS {
		t.Errorf("Strategy = %q, want %q", result.Strategy, StrategyRemoteKMS)
	}

	// Verify raw is preserved
	if result.Raw != "remote-kms://host.example.com:9443/path/to/secret" {
		t.Errorf("Raw = %q, want %q", result.Raw, "remote-kms://host.example.com:9443/path/to/secret")
	}

	// Verify value
	if result.String() != string(secretData) {
		t.Errorf("Value = %q, want %q", result.String(), string(secretData))
	}

	// Verify client was called with correct parameters
	if client.lastHost != "host.example.com:9443" {
		t.Errorf("Client called with host = %q, want %q", client.lastHost, "host.example.com:9443")
	}
	if client.lastSecretID != "path/to/secret" {
		t.Errorf("Client called with secretID = %q, want %q", client.lastSecretID, "path/to/secret")
	}
}

func TestRemoteKMSResolver_Resolve_URIParsing(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantHost     string
		wantSecretID string
	}{
		{
			name:         "standard format",
			uri:          "remote-kms://host:9443/secret",
			wantHost:     "host:9443",
			wantSecretID: "secret",
		},
		{
			name:         "nested path",
			uri:          "remote-kms://host:9443/path/to/secret",
			wantHost:     "host:9443",
			wantSecretID: "path/to/secret",
		},
		{
			name:         "localhost",
			uri:          "remote-kms://localhost:9443/my-secret",
			wantHost:     "localhost:9443",
			wantSecretID: "my-secret",
		},
		{
			name:         "IP address",
			uri:          "remote-kms://192.0.2.100:9443/db/password",
			wantHost:     "192.0.2.100:9443",
			wantSecretID: "db/password",
		},
		{
			name:         "IPv6 address",
			uri:          "remote-kms://[::1]:9443/secret",
			wantHost:     "[::1]:9443",
			wantSecretID: "secret",
		},
		{
			name:         "without port",
			uri:          "remote-kms://host/secret",
			wantHost:     "host",
			wantSecretID: "secret",
		},
		{
			name:         "deep nested path",
			uri:          "remote-kms://host:9443/a/b/c/d/e/f/secret",
			wantHost:     "host:9443",
			wantSecretID: "a/b/c/d/e/f/secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secretData := []byte("test-secret")
			client := newMockRemoteSecretsClient().
				withSecret(tt.wantHost, tt.wantSecretID, secretData)

			resolver := NewRemoteKMSResolver(client)
			ctx := context.Background()

			result, err := resolver.Resolve(ctx, tt.uri)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}

			if client.lastHost != tt.wantHost {
				t.Errorf("Parsed host = %q, want %q", client.lastHost, tt.wantHost)
			}
			if client.lastSecretID != tt.wantSecretID {
				t.Errorf("Parsed secretID = %q, want %q", client.lastSecretID, tt.wantSecretID)
			}
			if result.String() != string(secretData) {
				t.Errorf("Value = %q, want %q", result.String(), string(secretData))
			}
		})
	}
}

func TestRemoteKMSResolver_Resolve_InvalidURI(t *testing.T) {
	client := newMockRemoteSecretsClient()
	resolver := NewRemoteKMSResolver(client)
	ctx := context.Background()

	tests := []struct {
		name       string
		uri        string
		wantReason string
	}{
		{
			name:       "missing host and path",
			uri:        "remote-kms://",
			wantReason: "missing host and path",
		},
		{
			name:       "missing path separator",
			uri:        "remote-kms://host",
			wantReason: "missing path separator",
		},
		{
			name:       "missing host with path",
			uri:        "remote-kms:///secret",
			wantReason: "missing host",
		},
		{
			name:       "empty path",
			uri:        "remote-kms://host/",
			wantReason: "path cannot be empty",
		},
		{
			name:       "wrong scheme",
			uri:        "kms://host/secret",
			wantReason: "must start with",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolver.Resolve(ctx, tt.uri)

			if err == nil {
				t.Fatalf("Resolve(%q) error = nil, want error", tt.uri)
			}

			if result != nil {
				t.Errorf("Resolve(%q) result = %+v, want nil", tt.uri, result)
			}

			// Verify it's an InvalidSecretURIError
			if !IsInvalidSecretURI(err) {
				t.Errorf("Expected InvalidSecretURIError, got %T: %v", err, err)
			}

			// Verify error message contains reason
			if !strings.Contains(err.Error(), tt.wantReason) {
				t.Errorf("Error message %q does not contain %q", err.Error(), tt.wantReason)
			}
		})
	}
}

func TestRemoteKMSResolver_Resolve_ConnectionFailure(t *testing.T) {
	connectionErr := errors.New("connection refused")
	client := newMockRemoteSecretsClient().withError(connectionErr)

	resolver := NewRemoteKMSResolver(client)
	ctx := context.Background()

	result, err := resolver.Resolve(ctx, "remote-kms://host:9443/secret")

	if err == nil {
		t.Fatal("Resolve() error = nil, want connection failure error")
	}

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	// Verify it's a RemoteConnectionFailedError
	if !IsRemoteConnectionFailed(err) {
		t.Errorf("Expected RemoteConnectionFailedError, got %T: %v", err, err)
	}

	// Verify error message mentions the host
	if !strings.Contains(err.Error(), "host:9443") {
		t.Errorf("Error message %q does not contain host", err.Error())
	}
}

func TestRemoteKMSResolver_Resolve_SecretNotFound(t *testing.T) {
	// Client with no secrets configured
	client := newMockRemoteSecretsClient()

	resolver := NewRemoteKMSResolver(client)
	ctx := context.Background()

	result, err := resolver.Resolve(ctx, "remote-kms://host:9443/nonexistent/secret")

	if err == nil {
		t.Fatal("Resolve() error = nil, want secret not found error")
	}

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	// Verify it's a SecretNotFoundError
	if !IsSecretNotFound(err) {
		t.Errorf("Expected SecretNotFoundError, got %T: %v", err, err)
	}
}

func TestRemoteKMSResolver_Resolve_Timeout(t *testing.T) {
	// Client that delays longer than the timeout
	client := newMockRemoteSecretsClient().
		withSecret("host:9443", "secret", []byte("data")).
		withDelay(100 * time.Millisecond)

	resolver := NewRemoteKMSResolver(client, WithTimeout(10*time.Millisecond))
	ctx := context.Background()

	result, err := resolver.Resolve(ctx, "remote-kms://host:9443/secret")

	if err == nil {
		t.Fatal("Resolve() error = nil, want timeout error")
	}

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	// Verify it's a connection failed error (timeout)
	if !IsRemoteConnectionFailed(err) {
		t.Errorf("Expected RemoteConnectionFailedError for timeout, got %T: %v", err, err)
	}
}

func TestRemoteKMSResolver_Resolve_ContextCancellation(t *testing.T) {
	client := newMockRemoteSecretsClient().
		withSecret("host:9443", "secret", []byte("data")).
		withDelay(100 * time.Millisecond)

	resolver := NewRemoteKMSResolver(client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := resolver.Resolve(ctx, "remote-kms://host:9443/secret")

	if err == nil {
		t.Fatal("Resolve() error = nil, want context cancelled error")
	}

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	// Verify error mentions context
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("Error message %q does not mention context", err.Error())
	}
}

func TestRemoteKMSResolver_Resolve_NilContext(t *testing.T) {
	client := newMockRemoteSecretsClient()
	resolver := NewRemoteKMSResolver(client)

	var nilCtx context.Context
	result, err := resolver.Resolve(nilCtx, "remote-kms://host:9443/secret")

	if err == nil {
		t.Fatal("Resolve() error = nil, want nil context error")
	}

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	// Verify error mentions context
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("Error message %q does not mention context", err.Error())
	}
}

func TestRemoteKMSResolver_Resolve_NilClient(t *testing.T) {
	resolver := NewRemoteKMSResolver(nil)
	ctx := context.Background()

	result, err := resolver.Resolve(ctx, "remote-kms://host:9443/secret")

	if err == nil {
		t.Fatal("Resolve() error = nil, want nil client error")
	}

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	// Verify error mentions client
	if !strings.Contains(err.Error(), "client") {
		t.Errorf("Error message %q does not mention client", err.Error())
	}
}

func TestRemoteKMSResolver_WithTimeout(t *testing.T) {
	client := newMockRemoteSecretsClient()

	tests := []struct {
		name            string
		timeout         time.Duration
		expectedTimeout time.Duration
	}{
		{
			name:            "custom timeout",
			timeout:         10 * time.Second,
			expectedTimeout: 10 * time.Second,
		},
		{
			name:            "very short timeout",
			timeout:         1 * time.Millisecond,
			expectedTimeout: 1 * time.Millisecond,
		},
		{
			name:            "long timeout",
			timeout:         5 * time.Minute,
			expectedTimeout: 5 * time.Minute,
		},
		{
			name:            "zero timeout uses default",
			timeout:         0,
			expectedTimeout: DefaultRemoteKMSTimeout,
		},
		{
			name:            "negative timeout uses default",
			timeout:         -1 * time.Second,
			expectedTimeout: DefaultRemoteKMSTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewRemoteKMSResolver(client, WithTimeout(tt.timeout))

			if resolver.timeout != tt.expectedTimeout {
				t.Errorf("timeout = %v, want %v", resolver.timeout, tt.expectedTimeout)
			}
		})
	}
}

func TestRemoteKMSResolver_DefaultTimeout(t *testing.T) {
	client := newMockRemoteSecretsClient()
	resolver := NewRemoteKMSResolver(client)

	if resolver.timeout != DefaultRemoteKMSTimeout {
		t.Errorf("Default timeout = %v, want %v", resolver.timeout, DefaultRemoteKMSTimeout)
	}
}

func TestRemoteKMSResolver_ImplementsInterface(t *testing.T) {
	// Compile-time check
	var _ SecretResolver = (*RemoteKMSResolver)(nil)

	// Runtime verification
	client := newMockRemoteSecretsClient()
	resolver := NewRemoteKMSResolver(client)
	var sr SecretResolver = resolver

	if sr.Scheme() != "remote-kms" {
		t.Errorf("Interface Scheme() = %q, want %q", sr.Scheme(), "remote-kms")
	}
}

func TestRemoteKMSResolver_Resolve_PreservesRawValue(t *testing.T) {
	secretData := []byte("secret-value")
	client := newMockRemoteSecretsClient().
		withSecret("host:9443", "secret", secretData)

	resolver := NewRemoteKMSResolver(client)
	ctx := context.Background()

	rawURIs := []string{
		"remote-kms://host:9443/secret",
	}

	for _, raw := range rawURIs {
		t.Run(raw, func(t *testing.T) {
			result, err := resolver.Resolve(ctx, raw)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", raw, err)
			}

			if result.Raw != raw {
				t.Errorf("Raw = %q, want %q", result.Raw, raw)
			}
		})
	}
}

func TestRemoteKMSResolver_MultipleOptions(t *testing.T) {
	client := newMockRemoteSecretsClient()

	// Apply multiple options
	resolver := NewRemoteKMSResolver(
		client,
		WithTimeout(15*time.Second),
	)

	if resolver.timeout != 15*time.Second {
		t.Errorf("timeout = %v, want %v", resolver.timeout, 15*time.Second)
	}

	if resolver.client != client {
		t.Error("client was not set correctly")
	}
}

func TestRemoteKMSResolver_Resolve_BinaryData(t *testing.T) {
	// Test with binary data (non-UTF8)
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	client := newMockRemoteSecretsClient().
		withSecret("host:9443", "binary-secret", binaryData)

	resolver := NewRemoteKMSResolver(client)
	ctx := context.Background()

	result, err := resolver.Resolve(ctx, "remote-kms://host:9443/binary-secret")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// The value is stored as string, which may not preserve binary data well
	// This test documents the current behavior
	if len(result.String()) != len(binaryData) {
		t.Errorf("Value length = %d, want %d", len(result.String()), len(binaryData))
	}
}

func TestRemoteKMSResolver_Resolve_EmptySecret(t *testing.T) {
	// Test with empty secret value
	client := newMockRemoteSecretsClient().
		withSecret("host:9443", "empty-secret", []byte{})

	resolver := NewRemoteKMSResolver(client)
	ctx := context.Background()

	result, err := resolver.Resolve(ctx, "remote-kms://host:9443/empty-secret")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if result.String() != "" {
		t.Errorf("Value = %q, want empty string", result.String())
	}

	if result.Strategy != StrategyRemoteKMS {
		t.Errorf("Strategy = %q, want %q", result.Strategy, StrategyRemoteKMS)
	}
}

func TestRemoteKMSResolver_ClientCallCount(t *testing.T) {
	secretData := []byte("secret")
	client := newMockRemoteSecretsClient().
		withSecret("host:9443", "secret", secretData)

	resolver := NewRemoteKMSResolver(client)
	ctx := context.Background()

	// Call multiple times
	for i := 0; i < 5; i++ {
		_, err := resolver.Resolve(ctx, "remote-kms://host:9443/secret")
		if err != nil {
			t.Fatalf("Resolve() iteration %d error = %v", i, err)
		}
	}

	// Verify client was called the expected number of times
	if client.callCount != 5 {
		t.Errorf("Client call count = %d, want 5", client.callCount)
	}
}

func TestRemoteKMSSchemeConstants(t *testing.T) {
	// Verify constants are consistent
	if RemoteKMSSchemePrefix != RemoteKMSScheme+"://" {
		t.Errorf("RemoteKMSSchemePrefix = %q, want %q", RemoteKMSSchemePrefix, RemoteKMSScheme+"://")
	}

	if RemoteKMSScheme != "remote-kms" {
		t.Errorf("RemoteKMSScheme = %q, want %q", RemoteKMSScheme, "remote-kms")
	}

	if DefaultRemoteKMSTimeout != 30*time.Second {
		t.Errorf("DefaultRemoteKMSTimeout = %v, want %v", DefaultRemoteKMSTimeout, 30*time.Second)
	}
}
