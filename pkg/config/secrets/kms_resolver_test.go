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

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// mockSecretsClient is a mock implementation of SecretsClient for testing.
type mockSecretsClient struct {
	secrets map[string][]byte
	err     error
}

func newMockSecretsClient() *mockSecretsClient {
	return &mockSecretsClient{
		secrets: make(map[string][]byte),
	}
}

func (m *mockSecretsClient) withSecret(path string, data []byte) *mockSecretsClient {
	m.secrets[path] = data
	return m
}

func (m *mockSecretsClient) withError(err error) *mockSecretsClient {
	m.err = err
	return m
}

func (m *mockSecretsClient) GetSecret(ctx context.Context, secretID string) ([]byte, error) {
	// Check context first
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if m.err != nil {
		return nil, m.err
	}

	data, ok := m.secrets[secretID]
	if !ok {
		return nil, NewSecretNotFoundError("mockSecretsClient.GetSecret", secretID, "local_kms")
	}
	return data, nil
}

func TestLocalKMSResolver_Scheme(t *testing.T) {
	client := newMockSecretsClient()
	resolver := NewLocalKMSResolver(client)

	got := resolver.Scheme()
	want := "kms"

	if got != want {
		t.Errorf("Scheme() = %q, want %q", got, want)
	}
}

func TestLocalKMSResolver_CanResolve(t *testing.T) {
	client := newMockSecretsClient()
	resolver := NewLocalKMSResolver(client)

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		// Valid URIs
		{name: "valid simple path", raw: "kms://secret", want: true},
		{name: "valid path with slashes", raw: "kms://path/to/secret", want: true},
		{name: "valid path with multiple levels", raw: "kms://app/db/password", want: true},
		{name: "valid single char path", raw: "kms://x", want: true},
		{name: "valid numeric path", raw: "kms://12345", want: true},
		{name: "valid path with dashes", raw: "kms://my-secret-key", want: true},
		{name: "valid path with underscores", raw: "kms://my_secret_key", want: true},

		// Invalid URIs
		{name: "empty string", raw: "", want: false},
		{name: "no scheme", raw: "path/to/secret", want: false},
		{name: "wrong scheme http", raw: "http://path/to/secret", want: false},
		{name: "wrong scheme https", raw: "https://path/to/secret", want: false},
		{name: "wrong scheme vault", raw: "vault://path/to/secret", want: false},
		{name: "kms without colon slashes", raw: "kms:secret", want: false},
		{name: "kms with single slash", raw: "kms:/secret", want: false},
		{name: "just kms", raw: "kms", want: false},
		{name: "kms colon only", raw: "kms:", want: false},
		{name: "kms colon slash only", raw: "kms:/", want: false},
		{name: "kms:// only no path", raw: "kms://", want: false},
		{name: "prefix only exact length", raw: "kms://", want: false},
		{name: "case sensitive KMS", raw: "KMS://secret", want: false},
		{name: "case sensitive Kms", raw: "Kms://secret", want: false},
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

func TestLocalKMSResolver_Resolve_Success(t *testing.T) {
	secretPath := "app/database/password"
	secretValue := []byte("super-secret-password-123")

	client := newMockSecretsClient().withSecret(secretPath, secretValue)
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://app/database/password"

	result, err := resolver.Resolve(ctx, raw)
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}

	if result == nil {
		t.Fatal("Resolve() returned nil result")
	}

	// Verify raw value is preserved
	if result.Raw != raw {
		t.Errorf("Raw = %q, want %q", result.Raw, raw)
	}

	// Verify value matches secret
	if result.Value != string(secretValue) {
		t.Errorf("Value = %q, want %q", result.Value, string(secretValue))
	}

	// Verify strategy is LocalKMS
	if result.Strategy != StrategyLocalKMS {
		t.Errorf("Strategy = %q, want %q", result.Strategy, StrategyLocalKMS)
	}
}

func TestLocalKMSResolver_Resolve_SuccessVariousPaths(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		secretData []byte
	}{
		{name: "simple path", path: "secret", secretData: []byte("value1")},
		{name: "nested path", path: "path/to/secret", secretData: []byte("value2")},
		{name: "deep nested", path: "a/b/c/d/e/f", secretData: []byte("deep-value")},
		{name: "numeric path", path: "12345", secretData: []byte("numeric-secret")},
		{name: "special chars", path: "my-secret_key.v1", secretData: []byte("special-secret")},
		{name: "single char", path: "x", secretData: []byte("x-value")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockSecretsClient().withSecret(tt.path, tt.secretData)
			resolver := NewLocalKMSResolver(client)

			ctx := context.Background()
			raw := "kms://" + tt.path

			result, err := resolver.Resolve(ctx, raw)
			if err != nil {
				t.Fatalf("Resolve() error = %v, want nil", err)
			}

			if result.Value != string(tt.secretData) {
				t.Errorf("Value = %q, want %q", result.Value, string(tt.secretData))
			}

			if result.Strategy != StrategyLocalKMS {
				t.Errorf("Strategy = %q, want %q", result.Strategy, StrategyLocalKMS)
			}
		})
	}
}

func TestLocalKMSResolver_Resolve_SecretNotFound(t *testing.T) {
	client := newMockSecretsClient() // No secrets added
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://nonexistent/secret"

	result, err := resolver.Resolve(ctx, raw)

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	if err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}

	// Verify it's a not found error
	if !IsSecretNotFound(err) {
		t.Errorf("Expected secret not found error, got: %T - %v", err, err)
	}

	// Verify error contains the path
	if !strings.Contains(err.Error(), "nonexistent/secret") {
		t.Errorf("Error message does not contain path: %s", err.Error())
	}
}

func TestLocalKMSResolver_Resolve_ClientError(t *testing.T) {
	clientErr := errors.New("connection refused: KMS unavailable")
	client := newMockSecretsClient().withError(clientErr)
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://some/secret"

	result, err := resolver.Resolve(ctx, raw)

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	if err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}

	// Verify it's a resolution failed error (not a not found error)
	if !IsResolutionFailed(err) {
		t.Errorf("Expected resolution failed error, got: %T - %v", err, err)
	}

	// Verify error contains context about failure
	if !strings.Contains(err.Error(), "failed to resolve") || !strings.Contains(err.Error(), "kms://some/secret") {
		t.Errorf("Error message missing expected content: %s", err.Error())
	}
}

func TestLocalKMSResolver_Resolve_InvalidURI(t *testing.T) {
	client := newMockSecretsClient()
	resolver := NewLocalKMSResolver(client)

	tests := []struct {
		name          string
		raw           string
		expectedInMsg string
	}{
		{name: "empty path", raw: "kms://", expectedInMsg: "empty"},
		{name: "wrong scheme", raw: "http://secret", expectedInMsg: "must start with kms://"},
		{name: "no scheme", raw: "secret", expectedInMsg: "must start with kms://"},
		{name: "partial scheme", raw: "kms:/secret", expectedInMsg: "must start with kms://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			result, err := resolver.Resolve(ctx, tt.raw)

			if result != nil {
				t.Errorf("Resolve() result = %+v, want nil", result)
			}

			if err == nil {
				t.Fatal("Resolve() error = nil, want error")
			}

			// Verify it's an invalid URI error
			if !IsInvalidSecretURI(err) {
				t.Errorf("Expected invalid secret URI error, got: %T - %v", err, err)
			}

			// Verify error contains expected message
			if !strings.Contains(strings.ToLower(err.Error()), tt.expectedInMsg) {
				t.Errorf("Error message does not contain %q: %s", tt.expectedInMsg, err.Error())
			}
		})
	}
}

func TestLocalKMSResolver_Resolve_EmptyPath(t *testing.T) {
	client := newMockSecretsClient()
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://"

	result, err := resolver.Resolve(ctx, raw)

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	if err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}

	// Verify it's an invalid URI error
	if !IsInvalidSecretURI(err) {
		t.Errorf("Expected invalid secret URI error, got: %T - %v", err, err)
	}

	// Verify error mentions empty path
	if !strings.Contains(strings.ToLower(err.Error()), "empty") {
		t.Errorf("Error message does not mention empty path: %s", err.Error())
	}
}

func TestLocalKMSResolver_Resolve_ContextCancellation(t *testing.T) {
	// Create a secret that exists
	client := newMockSecretsClient().withSecret("secret", []byte("value"))
	resolver := NewLocalKMSResolver(client)

	// Cancel context before calling Resolve
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	raw := "kms://secret"

	result, err := resolver.Resolve(ctx, raw)

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	if err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}

	// Verify error mentions context cancellation
	if !strings.Contains(strings.ToLower(err.Error()), "context") {
		t.Errorf("Error message does not mention context: %s", err.Error())
	}
}

func TestLocalKMSResolver_Resolve_ContextCancellationDuringClientCall(t *testing.T) {
	// Create a client that will check context cancellation
	client := newMockSecretsClient().withSecret("secret", []byte("value"))
	resolver := NewLocalKMSResolver(client)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	raw := "kms://secret"

	result, err := resolver.Resolve(ctx, raw)

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	if err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}
}

func TestNewLocalKMSResolver_NilClient(t *testing.T) {
	resolver := NewLocalKMSResolver(nil)

	if resolver != nil {
		t.Errorf("NewLocalKMSResolver(nil) = %+v, want nil", resolver)
	}
}

func TestLocalKMSResolver_ImplementsInterface(t *testing.T) {
	// Compile-time check that LocalKMSResolver implements SecretResolver
	var _ SecretResolver = (*LocalKMSResolver)(nil)

	// Runtime verification
	client := newMockSecretsClient()
	resolver := NewLocalKMSResolver(client)
	var sr SecretResolver = resolver

	if sr.Scheme() != "kms" {
		t.Errorf("Interface Scheme() = %q, want %q", sr.Scheme(), "kms")
	}
}

func TestLocalKMSResolver_Resolve_PreservesRawValue(t *testing.T) {
	secretData := []byte("my-secret-value")
	client := newMockSecretsClient().
		withSecret("path/to/secret", secretData).
		withSecret("another-secret", secretData)
	resolver := NewLocalKMSResolver(client)

	testCases := []string{
		"kms://path/to/secret",
		"kms://another-secret",
	}

	for _, raw := range testCases {
		t.Run(raw, func(t *testing.T) {
			ctx := context.Background()

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

func TestLocalKMSResolver_Resolve_BinarySecret(t *testing.T) {
	// Test with binary data (non-UTF8)
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	client := newMockSecretsClient().withSecret("binary-secret", binaryData)
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://binary-secret"

	result, err := resolver.Resolve(ctx, raw)
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}

	if result.Value != string(binaryData) {
		t.Errorf("Value bytes mismatch")
	}
}

func TestLocalKMSResolver_Resolve_EmptySecret(t *testing.T) {
	// Test with empty secret value
	client := newMockSecretsClient().withSecret("empty-secret", []byte{})
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://empty-secret"

	result, err := resolver.Resolve(ctx, raw)
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}

	if result.Value != "" {
		t.Errorf("Value = %q, want empty string", result.Value)
	}

	if result.Strategy != StrategyLocalKMS {
		t.Errorf("Strategy = %q, want %q", result.Strategy, StrategyLocalKMS)
	}
}

func TestLocalKMSResolver_Resolve_LargeSecret(t *testing.T) {
	// Test with a large secret
	largeData := make([]byte, 1024*1024) // 1MB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	client := newMockSecretsClient().withSecret("large-secret", largeData)
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://large-secret"

	result, err := resolver.Resolve(ctx, raw)
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}

	if len(result.Value) != len(largeData) {
		t.Errorf("Value length = %d, want %d", len(result.Value), len(largeData))
	}
}

func TestLocalKMSResolver_Resolve_QBErrorWrapping(t *testing.T) {
	client := newMockSecretsClient() // No secrets, will return not found
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://missing/secret"

	_, err := resolver.Resolve(ctx, raw)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verify the error is wrapped in a QBError
	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Fatalf("Expected QBError, got %T", err)
	}

	// Verify error code
	if qbErr.Code != dberrors.ErrNotFound {
		t.Errorf("Error code = %v, want %v", qbErr.Code, dberrors.ErrNotFound)
	}

	// Verify operation is set
	if qbErr.Op == "" {
		t.Error("Operation is empty")
	}

	// Verify context contains path
	if qbErr.Context == nil {
		t.Error("Context is nil")
	}
	if path, ok := qbErr.Context["path"]; !ok || path != "missing/secret" {
		t.Errorf("Context path = %v, want 'missing/secret'", path)
	}
}

func TestLocalKMSResolver_Resolve_InvalidURIErrorDetails(t *testing.T) {
	client := newMockSecretsClient()
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://"

	_, err := resolver.Resolve(ctx, raw)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verify the error is wrapped in a QBError
	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Fatalf("Expected QBError, got %T", err)
	}

	// Verify error code is InvalidInput
	if qbErr.Code != dberrors.ErrInvalidInput {
		t.Errorf("Error code = %v, want %v", qbErr.Code, dberrors.ErrInvalidInput)
	}

	// Verify the underlying error is InvalidSecretURIError
	var invalidURI *InvalidSecretURIError
	if !errors.As(err, &invalidURI) {
		t.Fatalf("Expected InvalidSecretURIError in chain, got %T", qbErr.Err)
	}

	if invalidURI.URI != raw {
		t.Errorf("InvalidSecretURIError.URI = %q, want %q", invalidURI.URI, raw)
	}
}

func TestLocalKMSResolver_UsesCorrectProvider(t *testing.T) {
	client := newMockSecretsClient() // No secrets
	resolver := NewLocalKMSResolver(client)

	ctx := context.Background()
	raw := "kms://secret"

	_, err := resolver.Resolve(ctx, raw)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verify the provider is set correctly in the error
	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Fatalf("Expected QBError, got %T", err)
	}

	if provider, ok := qbErr.Context["provider"]; ok {
		if provider != "local_kms" {
			t.Errorf("Provider = %v, want 'local_kms'", provider)
		}
	}
}
