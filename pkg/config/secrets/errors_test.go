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
	stderrors "errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestErrorTypes_Error checks the message of each error type, one row per
// formatting branch of its Error method.
func TestErrorTypes_Error(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "resolution with scheme, message and cause",
			err:  NewSecretResolutionError("kms", "secret not found", stderrors.New("underlying error")),
			want: "kms: secret not found: underlying error",
		},
		{
			name: "resolution with scheme and message",
			err:  NewSecretResolutionError("vault", "connection timeout", nil),
			want: "vault: connection timeout",
		},
		{
			name: "resolution with message only",
			err:  NewSecretResolutionError("", "generic error", nil),
			want: "generic error",
		},
		{
			name: "not found with provider",
			err:  &SecretNotFoundError{Path: "/secrets/db/password", Provider: "vault"},
			want: `secret not found: path "/secrets/db/password" in provider "vault"`,
		},
		{
			name: "not found without provider",
			err:  &SecretNotFoundError{Path: "/secrets/api/key"},
			want: `secret not found: path "/secrets/api/key"`,
		},
		{
			name: "resolution failed with reason",
			err:  &ResolutionFailedError{URI: "kms://secret/path", Reason: "connection refused"},
			want: `failed to resolve secret URI "kms://secret/path": connection refused`,
		},
		{
			name: "resolution failed without reason",
			err:  &ResolutionFailedError{URI: "vault://secret/path"},
			want: `failed to resolve secret URI "vault://secret/path"`,
		},
		{
			name: "remote connection with provider, endpoint and cause",
			err: &RemoteConnectionFailedError{
				Endpoint: "https://vault.example.com:8200",
				Provider: "vault",
				Cause:    stderrors.New("connection refused"),
			},
			want: "failed to connect to vault KMS at https://vault.example.com:8200: connection refused",
		},
		{
			name: "remote connection with provider and cause",
			err:  &RemoteConnectionFailedError{Provider: "azure", Cause: stderrors.New("auth failed")},
			want: "failed to connect to azure KMS: auth failed",
		},
		{
			name: "remote connection with endpoint only",
			err:  &RemoteConnectionFailedError{Endpoint: "https://remote.host:443"},
			want: "failed to connect to remote KMS at https://remote.host:443",
		},
		{
			name: "remote connection with no fields",
			err:  &RemoteConnectionFailedError{},
			want: "failed to connect to remote KMS",
		},
		{
			name: "invalid URI with reason",
			err:  &InvalidSecretURIError{URI: "invalid://bad/uri", Reason: "scheme not recognized"},
			want: `invalid secret URI "invalid://bad/uri": scheme not recognized`,
		},
		{
			name: "invalid URI without reason",
			err:  &InvalidSecretURIError{URI: "malformed-uri"},
			want: `invalid secret URI "malformed-uri"`,
		},
		{
			name: "entropy with bytes and cause",
			err:  &EntropyGenerationFailedError{RequestedBytes: 32, Cause: stderrors.New("entropy pool exhausted")},
			want: "failed to generate 32 bytes of entropy: entropy pool exhausted",
		},
		{
			name: "entropy with cause only",
			err:  &EntropyGenerationFailedError{Cause: stderrors.New("random device unavailable")},
			want: "failed to generate entropy: random device unavailable",
		},
		{
			name: "entropy with negative bytes and cause",
			err:  &EntropyGenerationFailedError{RequestedBytes: -1, Cause: stderrors.New("invalid request")},
			want: "failed to generate entropy: invalid request",
		},
		{
			name: "entropy with bytes only",
			err:  &EntropyGenerationFailedError{RequestedBytes: 64},
			want: "failed to generate 64 bytes of entropy",
		},
		{
			name: "entropy with no bytes and no cause",
			err:  &EntropyGenerationFailedError{},
			want: "failed to generate entropy",
		},
		{
			name: "unsupported scheme with supported schemes",
			err: &UnsupportedSchemeError{
				Scheme:           "unknown",
				URI:              "unknown://secret/path",
				SupportedSchemes: []string{"kms", "vault", "env"},
			},
			want: `unsupported scheme "unknown" in URI "unknown://secret/path": supported schemes are [kms vault env]`,
		},
		{
			name: "unsupported scheme with nil supported schemes",
			err:  &UnsupportedSchemeError{Scheme: "custom", URI: "custom://path"},
			want: `unsupported scheme "custom" in URI "custom://path"`,
		},
		{
			name: "unsupported scheme with empty supported schemes",
			err:  &UnsupportedSchemeError{Scheme: "test", URI: "test://uri", SupportedSchemes: []string{}},
			want: `unsupported scheme "test" in URI "test://uri"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestErrorTypes_Unwrap checks that each error type carrying a cause exposes
// it to errors.Is, and unwraps to nil without one.
func TestErrorTypes_Unwrap(t *testing.T) {
	sentinel := stderrors.New("sentinel error")
	tests := []struct {
		name      string
		withCause interface{ Unwrap() error }
		noCause   interface{ Unwrap() error }
	}{
		{
			name:      "SecretResolutionError",
			withCause: NewSecretResolutionError("kms", "wrapped sentinel", sentinel),
			noCause:   NewSecretResolutionError("kms", "error", nil),
		},
		{
			name:      "RemoteConnectionFailedError",
			withCause: &RemoteConnectionFailedError{Endpoint: "https://vault.example.com", Provider: "vault", Cause: sentinel},
			noCause:   &RemoteConnectionFailedError{Endpoint: "https://vault.example.com", Provider: "vault"},
		},
		{
			name:      "EntropyGenerationFailedError",
			withCause: &EntropyGenerationFailedError{RequestedBytes: 16, Cause: sentinel},
			noCause:   &EntropyGenerationFailedError{RequestedBytes: 16},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.withCause.Unwrap(); got != sentinel {
				t.Errorf("Unwrap() = %v, want the sentinel cause", got)
			}
			if !stderrors.Is(tc.withCause.(error), sentinel) {
				t.Error("errors.Is did not find the sentinel cause")
			}
			if got := tc.noCause.Unwrap(); got != nil {
				t.Errorf("Unwrap() without cause = %v, want nil", got)
			}
		})
	}
}

// TestNewEntropyGenerationError tests the convenience constructor
func TestNewEntropyGenerationError(t *testing.T) {
	tests := []struct {
		name  string
		cause error
	}{
		{
			name:  "with cause",
			cause: stderrors.New("entropy source error"),
		},
		{
			name:  "with nil cause",
			cause: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NewEntropyGenerationError(tc.cause)

			if err == nil {
				t.Fatal("Expected non-nil error")
			}
			if err.Cause != tc.cause {
				t.Errorf("Expected cause to match")
			}
			if err.RequestedBytes != 0 {
				t.Errorf("Expected RequestedBytes to be 0, got %d", err.RequestedBytes)
			}
		})
	}
}

// TestNewEntropyGenerationFailedError_Constructor tests the QBError constructor
func TestNewEntropyGenerationFailedError_Constructor(t *testing.T) {
	cause := stderrors.New("entropy source unavailable")
	err := NewEntropyGenerationFailedError("secrets.generateRandom", 64, cause)

	if err == nil {
		t.Fatal("Expected non-nil error")
	}

	if err.Code != dberrors.ErrEncryption {
		t.Errorf("Expected ErrEncryption, got %v", err.Code)
	}
	if err.Op != "secrets.generateRandom" {
		t.Errorf("Expected op 'secrets.generateRandom', got %s", err.Op)
	}

	var egf *EntropyGenerationFailedError
	if !stderrors.As(err.Err, &egf) {
		t.Fatalf("Expected EntropyGenerationFailedError, got %T", err.Err)
	}
	if egf.RequestedBytes != 64 {
		t.Errorf("Expected RequestedBytes 64, got %d", egf.RequestedBytes)
	}
	if egf.Cause != cause {
		t.Error("Expected cause to match")
	}

	// Check context
	if rb, ok := err.Context["requested_bytes"]; !ok || rb != 64 {
		t.Errorf("Expected requested_bytes context to be 64, got %v", rb)
	}
}

// TestNewUnsupportedSchemeError tests the QBError constructor
func TestNewUnsupportedSchemeError(t *testing.T) {
	supportedSchemes := []string{"kms", "vault", "env"}
	err := NewUnsupportedSchemeError("secrets.resolve", "custom", "custom://path", supportedSchemes)

	if err == nil {
		t.Fatal("Expected non-nil error")
	}

	if err.Code != dberrors.ErrInvalidInput {
		t.Errorf("Expected ErrInvalidInput, got %v", err.Code)
	}
	if err.Op != "secrets.resolve" {
		t.Errorf("Expected op 'secrets.resolve', got %s", err.Op)
	}

	var use *UnsupportedSchemeError
	if !stderrors.As(err.Err, &use) {
		t.Fatalf("Expected UnsupportedSchemeError, got %T", err.Err)
	}
	if use.Scheme != "custom" {
		t.Errorf("Expected scheme 'custom', got %s", use.Scheme)
	}
	if use.URI != "custom://path" {
		t.Errorf("Expected URI 'custom://path', got %s", use.URI)
	}
	if len(use.SupportedSchemes) != 3 {
		t.Errorf("Expected 3 supported schemes, got %d", len(use.SupportedSchemes))
	}

	// Check context
	if scheme, ok := err.Context["scheme"]; !ok || scheme != "custom" {
		t.Errorf("Expected scheme context to be 'custom', got %v", scheme)
	}
	if uri, ok := err.Context["uri"]; !ok || uri != "custom://path" {
		t.Errorf("Expected uri context to be 'custom://path', got %v", uri)
	}
}

// TestNewUnsupportedSchemeError_NilSchemes tests with nil supported schemes
func TestNewUnsupportedSchemeError_NilSchemes(t *testing.T) {
	err := NewUnsupportedSchemeError("secrets.resolve", "unknown", "unknown://path", nil)

	if err == nil {
		t.Fatal("Expected non-nil error")
	}

	var use *UnsupportedSchemeError
	if !stderrors.As(err.Err, &use) {
		t.Fatalf("Expected UnsupportedSchemeError, got %T", err.Err)
	}
	if use.SupportedSchemes != nil {
		t.Errorf("Expected nil SupportedSchemes, got %v", use.SupportedSchemes)
	}
}

// TestIsEntropyGenerationFailed tests all code paths
func TestIsEntropyGenerationFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "direct EntropyGenerationFailedError",
			err:      &EntropyGenerationFailedError{RequestedBytes: 32},
			expected: true,
		},
		{
			name: "wrapped EntropyGenerationFailedError",
			err: &dberrors.WrappedMessageError{
				Message: "outer",
				Cause: &EntropyGenerationFailedError{
					RequestedBytes: 32,
					Cause:          stderrors.New("inner"),
				},
			},
			expected: true,
		},
		{
			name:     "QBError with ErrEncryption code",
			err:      &dberrors.QBError{Code: dberrors.ErrEncryption, Op: "test"},
			expected: true,
		},
		{
			name: "wrapped QBError with ErrEncryption",
			err: &dberrors.WrappedMessageError{
				Message: "outer",
				Cause: &dberrors.QBError{
					Code: dberrors.ErrEncryption,
					Op:   "test",
				},
			},
			expected: true,
		},
		{
			name:     "QBError with different code",
			err:      &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"},
			expected: false,
		},
		{
			name:     "unrelated error",
			err:      stderrors.New("some error"),
			expected: false,
		},
		{
			name:     "from NewEntropyGenerationFailedError",
			err:      NewEntropyGenerationFailedError("op", 32, nil),
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsEntropyGenerationFailed(tc.err)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

// TestIsUnsupportedScheme tests all code paths
func TestIsUnsupportedScheme(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name: "direct UnsupportedSchemeError",
			err: &UnsupportedSchemeError{
				Scheme: "custom",
				URI:    "custom://path",
			},
			expected: true,
		},
		{
			name: "wrapped UnsupportedSchemeError",
			err: &dberrors.WrappedMessageError{
				Message: "outer",
				Cause: &UnsupportedSchemeError{
					Scheme: "custom",
					URI:    "custom://path",
				},
			},
			expected: true,
		},
		{
			name:     "unrelated error",
			err:      stderrors.New("some error"),
			expected: false,
		},
		{
			name:     "from NewUnsupportedSchemeError",
			err:      NewUnsupportedSchemeError("op", "bad", "bad://path", nil),
			expected: true,
		},
		{
			name: "QBError with different error type inside",
			err: &dberrors.QBError{
				Code: dberrors.ErrInvalidInput,
				Err:  stderrors.New("not unsupported scheme"),
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsUnsupportedScheme(tc.err)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

// TestIsRemoteConnectionFailed tests all code paths
func TestIsRemoteConnectionFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name: "direct RemoteConnectionFailedError",
			err: &RemoteConnectionFailedError{
				Endpoint: "https://vault.example.com",
				Provider: "vault",
			},
			expected: true,
		},
		{
			name: "wrapped RemoteConnectionFailedError",
			err: &dberrors.WrappedMessageError{
				Message: "outer",
				Cause: &RemoteConnectionFailedError{
					Endpoint: "https://vault.example.com",
					Provider: "vault",
				},
			},
			expected: true,
		},
		{
			name:     "QBError with ErrUnavailable code",
			err:      &dberrors.QBError{Code: dberrors.ErrUnavailable, Op: "test"},
			expected: true,
		},
		{
			name: "wrapped QBError with ErrUnavailable",
			err: &dberrors.WrappedMessageError{
				Message: "outer",
				Cause: &dberrors.QBError{
					Code: dberrors.ErrUnavailable,
					Op:   "test",
				},
			},
			expected: true,
		},
		{
			name:     "QBError with different code",
			err:      &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"},
			expected: false,
		},
		{
			name:     "unrelated error",
			err:      stderrors.New("some error"),
			expected: false,
		},
		{
			name:     "from NewRemoteConnectionFailedError",
			err:      NewRemoteConnectionFailedError("op", "https://test.com", "vault", nil),
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsRemoteConnectionFailed(tc.err)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

// TestIsSecretNotFound tests all code paths
func TestIsSecretNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name: "direct SecretNotFoundError",
			err: &SecretNotFoundError{
				Path:     "/secrets/test",
				Provider: "vault",
			},
			expected: true,
		},
		{
			name: "wrapped SecretNotFoundError",
			err: &dberrors.WrappedMessageError{
				Message: "outer",
				Cause: &SecretNotFoundError{
					Path:     "/secrets/test",
					Provider: "vault",
				},
			},
			expected: true,
		},
		{
			name:     "QBError with ErrNotFound code",
			err:      &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"},
			expected: true,
		},
		{
			name: "wrapped QBError with ErrNotFound",
			err: &dberrors.WrappedMessageError{
				Message: "outer",
				Cause: &dberrors.QBError{
					Code: dberrors.ErrNotFound,
					Op:   "test",
				},
			},
			expected: true,
		},
		{
			name:     "QBError with different code",
			err:      &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"},
			expected: false,
		},
		{
			name:     "unrelated error",
			err:      stderrors.New("some error"),
			expected: false,
		},
		{
			name:     "from NewSecretNotFoundError",
			err:      NewSecretNotFoundError("op", "/path", "provider"),
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsSecretNotFound(tc.err)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

// TestIsResolutionFailed tests all code paths
func TestIsResolutionFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name: "direct ResolutionFailedError",
			err: &ResolutionFailedError{
				URI:    "kms://test",
				Reason: "timeout",
			},
			expected: true,
		},
		{
			name: "wrapped ResolutionFailedError",
			err: &dberrors.WrappedMessageError{
				Message: "outer",
				Cause: &ResolutionFailedError{
					URI:    "kms://test",
					Reason: "timeout",
				},
			},
			expected: true,
		},
		{
			name:     "unrelated error",
			err:      stderrors.New("some error"),
			expected: false,
		},
		{
			name:     "QBError without ResolutionFailedError inside",
			err:      &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsResolutionFailed(tc.err)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

// TestIsInvalidSecretURI tests all code paths
func TestIsInvalidSecretURI(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name: "direct InvalidSecretURIError",
			err: &InvalidSecretURIError{
				URI:    "bad-uri",
				Reason: "missing scheme",
			},
			expected: true,
		},
		{
			name: "wrapped InvalidSecretURIError",
			err: &dberrors.WrappedMessageError{
				Message: "outer",
				Cause: &InvalidSecretURIError{
					URI:    "bad-uri",
					Reason: "missing scheme",
				},
			},
			expected: true,
		},
		{
			name:     "unrelated error",
			err:      stderrors.New("some error"),
			expected: false,
		},
		{
			name:     "from NewInvalidSecretURIError",
			err:      NewInvalidSecretURIError("op", "bad://uri", "bad scheme"),
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsInvalidSecretURI(tc.err)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

// TestNewSecretNotFoundError tests QBError constructor
func TestNewSecretNotFoundError(t *testing.T) {
	err := NewSecretNotFoundError("secrets.get", "/secrets/db/password", "vault")

	if err == nil {
		t.Fatal("Expected non-nil error")
	}

	if err.Code != dberrors.ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err.Code)
	}
	if err.Op != "secrets.get" {
		t.Errorf("Expected op 'secrets.get', got %s", err.Op)
	}

	var snf *SecretNotFoundError
	if !stderrors.As(err.Err, &snf) {
		t.Fatalf("Expected SecretNotFoundError, got %T", err.Err)
	}
	if snf.Path != "/secrets/db/password" {
		t.Errorf("Expected path '/secrets/db/password', got %s", snf.Path)
	}
	if snf.Provider != "vault" {
		t.Errorf("Expected provider 'vault', got %s", snf.Provider)
	}

	// Check context
	if path, ok := err.Context["path"]; !ok || path != "/secrets/db/password" {
		t.Errorf("Expected path context, got %v", path)
	}
	if provider, ok := err.Context["provider"]; !ok || provider != "vault" {
		t.Errorf("Expected provider context, got %v", provider)
	}
}

// TestNewResolutionFailedError tests QBError constructor
func TestNewResolutionFailedError(t *testing.T) {
	tests := []struct {
		name       string
		op         string
		uri        string
		reason     string
		cause      error
		checkCause bool
	}{
		{
			name:       "with cause",
			op:         "secrets.resolve",
			uri:        "kms://secret/path",
			reason:     "connection failed",
			cause:      stderrors.New("timeout"),
			checkCause: true,
		},
		{
			name:       "without cause",
			op:         "secrets.resolve",
			uri:        "vault://secret/path",
			reason:     "not found",
			cause:      nil,
			checkCause: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NewResolutionFailedError(tc.op, tc.uri, tc.reason, tc.cause)

			if err == nil {
				t.Fatal("Expected non-nil error")
			}

			if err.Code != dberrors.ErrInternal {
				t.Errorf("Expected ErrInternal, got %v", err.Code)
			}
			if err.Op != tc.op {
				t.Errorf("Expected op %q, got %s", tc.op, err.Op)
			}

			var rf *ResolutionFailedError
			if !stderrors.As(err.Err, &rf) {
				t.Fatalf("Expected ResolutionFailedError, got %T", err.Err)
			}
			if rf.URI != tc.uri {
				t.Errorf("Expected URI %q, got %s", tc.uri, rf.URI)
			}
			if rf.Reason != tc.reason {
				t.Errorf("Expected reason %q, got %s", tc.reason, rf.Reason)
			}

			// Check context
			if uri, ok := err.Context["uri"]; !ok || uri != tc.uri {
				t.Errorf("Expected uri context, got %v", uri)
			}
			if reason, ok := err.Context["reason"]; !ok || reason != tc.reason {
				t.Errorf("Expected reason context, got %v", reason)
			}
			if tc.checkCause {
				if cause, ok := err.Context["cause"]; !ok || cause != tc.cause.Error() {
					t.Errorf("Expected cause context, got %v", cause)
				}
			} else {
				if _, ok := err.Context["cause"]; ok {
					t.Error("Expected no cause context for nil cause")
				}
			}
		})
	}
}

// TestNewRemoteConnectionFailedError tests QBError constructor
func TestNewRemoteConnectionFailedError(t *testing.T) {
	cause := stderrors.New("connection refused")
	err := NewRemoteConnectionFailedError("secrets.connect", "https://vault.example.com:8200", "vault", cause)

	if err == nil {
		t.Fatal("Expected non-nil error")
	}

	if err.Code != dberrors.ErrUnavailable {
		t.Errorf("Expected ErrUnavailable, got %v", err.Code)
	}
	if err.Op != "secrets.connect" {
		t.Errorf("Expected op 'secrets.connect', got %s", err.Op)
	}
	if !err.Retry {
		t.Error("Expected Retry to be true")
	}

	var rcf *RemoteConnectionFailedError
	if !stderrors.As(err.Err, &rcf) {
		t.Fatalf("Expected RemoteConnectionFailedError, got %T", err.Err)
	}
	if rcf.Endpoint != "https://vault.example.com:8200" {
		t.Errorf("Expected endpoint 'https://vault.example.com:8200', got %s", rcf.Endpoint)
	}
	if rcf.Provider != "vault" {
		t.Errorf("Expected provider 'vault', got %s", rcf.Provider)
	}
	if rcf.Cause != cause {
		t.Error("Expected cause to match")
	}

	// Check context
	if endpoint, ok := err.Context["endpoint"]; !ok || endpoint != "https://vault.example.com:8200" {
		t.Errorf("Expected endpoint context, got %v", endpoint)
	}
	if provider, ok := err.Context["provider"]; !ok || provider != "vault" {
		t.Errorf("Expected provider context, got %v", provider)
	}
}

// TestNewInvalidSecretURIError tests QBError constructor
func TestNewInvalidSecretURIError(t *testing.T) {
	err := NewInvalidSecretURIError("secrets.parse", "bad-uri://test", "missing path")

	if err == nil {
		t.Fatal("Expected non-nil error")
	}

	if err.Code != dberrors.ErrInvalidInput {
		t.Errorf("Expected ErrInvalidInput, got %v", err.Code)
	}
	if err.Op != "secrets.parse" {
		t.Errorf("Expected op 'secrets.parse', got %s", err.Op)
	}

	var isu *InvalidSecretURIError
	if !stderrors.As(err.Err, &isu) {
		t.Fatalf("Expected InvalidSecretURIError, got %T", err.Err)
	}
	if isu.URI != "bad-uri://test" {
		t.Errorf("Expected URI 'bad-uri://test', got %s", isu.URI)
	}
	if isu.Reason != "missing path" {
		t.Errorf("Expected reason 'missing path', got %s", isu.Reason)
	}

	// Check context
	if uri, ok := err.Context["uri"]; !ok || uri != "bad-uri://test" {
		t.Errorf("Expected uri context, got %v", uri)
	}
	if reason, ok := err.Context["reason"]; !ok || reason != "missing path" {
		t.Errorf("Expected reason context, got %v", reason)
	}
}

// TestErrNilContext_Sentinel tests the sentinel error
func TestErrNilContext_Sentinel(t *testing.T) {
	if ErrNilContext == nil {
		t.Fatal("Expected non-nil ErrNilContext")
	}
	if ErrNilContext.Error() != "context cannot be nil" {
		t.Errorf("Expected 'context cannot be nil', got %s", ErrNilContext.Error())
	}

	// Test errors.Is
	wrappedErr := &dberrors.WrappedMessageError{Message: "wrapped", Cause: ErrNilContext}
	if !stderrors.Is(wrappedErr, ErrNilContext) {
		t.Error("Expected errors.Is to find ErrNilContext")
	}
}

// TestErrorChaining tests complex error wrapping chains
func TestErrorChaining(t *testing.T) {
	// Create a multi-level error chain
	innerErr := stderrors.New("network timeout")
	entropyErr := &EntropyGenerationFailedError{
		RequestedBytes: 32,
		Cause:          innerErr,
	}
	qbErr := &dberrors.QBError{
		Code: dberrors.ErrEncryption,
		Op:   "secrets.generate",
		Err:  entropyErr,
	}
	outerErr := &dberrors.WrappedMessageError{Message: "operation failed", Cause: qbErr}

	// Test that errors.Is can find innerErr through the chain
	if !stderrors.Is(outerErr, innerErr) {
		t.Error("Expected errors.Is to find innerErr through chain")
	}

	// Test errors.As can find EntropyGenerationFailedError
	var egf *EntropyGenerationFailedError
	if !stderrors.As(outerErr, &egf) {
		t.Error("Expected errors.As to find EntropyGenerationFailedError")
	}
	if egf.RequestedBytes != 32 {
		t.Errorf("Expected RequestedBytes 32, got %d", egf.RequestedBytes)
	}

	// Test errors.As can find QBError
	var de *dberrors.QBError
	if !stderrors.As(outerErr, &de) {
		t.Error("Expected errors.As to find QBError")
	}
	if de.Code != dberrors.ErrEncryption {
		t.Errorf("Expected ErrEncryption, got %v", de.Code)
	}

	// Test IsEntropyGenerationFailed works through chain
	if !IsEntropyGenerationFailed(outerErr) {
		t.Error("Expected IsEntropyGenerationFailed to return true")
	}
}

// TestRemoteConnectionFailedError_ErrorChaining tests Unwrap chain
func TestRemoteConnectionFailedError_ErrorChaining(t *testing.T) {
	sentinel := stderrors.New("connection timeout")
	rcf := &RemoteConnectionFailedError{
		Endpoint: "https://vault.example.com",
		Provider: "vault",
		Cause:    sentinel,
	}
	qbErr := NewRemoteConnectionFailedError("secrets.connect", "https://vault.example.com", "vault", sentinel)

	// Test direct error unwrap
	if rcf.Unwrap() != sentinel {
		t.Error("Expected Unwrap to return sentinel")
	}

	// Test errors.Is through QBError
	var rcfFound *RemoteConnectionFailedError
	if !stderrors.As(qbErr, &rcfFound) {
		t.Error("Expected errors.As to find RemoteConnectionFailedError in QBError")
	}

	// Test IsRemoteConnectionFailed
	if !IsRemoteConnectionFailed(qbErr) {
		t.Error("Expected IsRemoteConnectionFailed to return true")
	}
}

// TestAllErrorTypes_ErrorInterface ensures all error types implement error interface
func TestAllErrorTypes_ErrorInterface(t *testing.T) {
	// This test verifies at compile time that all types implement error interface
	var _ error = &SecretResolutionError{}
	var _ error = &SecretNotFoundError{}
	var _ error = &ResolutionFailedError{}
	var _ error = &RemoteConnectionFailedError{}
	var _ error = &InvalidSecretURIError{}
	var _ error = &EntropyGenerationFailedError{}
	var _ error = &UnsupportedSchemeError{}

	// Test that each can be used as an error
	errors := []error{
		&SecretResolutionError{Message: "test"},
		&SecretNotFoundError{Path: "test"},
		&ResolutionFailedError{URI: "test"},
		&RemoteConnectionFailedError{Endpoint: "test"},
		&InvalidSecretURIError{URI: "test"},
		&EntropyGenerationFailedError{RequestedBytes: 32},
		&UnsupportedSchemeError{Scheme: "test"},
	}

	for i, err := range errors {
		if err.Error() == "" {
			t.Errorf("Error %d returned empty string", i)
		}
	}
}

// TestErrorsAs_AllTypes tests errors.As for all error types
func TestErrorsAs_AllTypes(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target interface{}
	}{
		{
			name:   "SecretResolutionError",
			err:    &dberrors.WrappedMessageError{Message: "wrapped", Cause: &SecretResolutionError{Message: "test"}},
			target: &SecretResolutionError{},
		},
		{
			name:   "SecretNotFoundError",
			err:    &dberrors.WrappedMessageError{Message: "wrapped", Cause: &SecretNotFoundError{Path: "test"}},
			target: &SecretNotFoundError{},
		},
		{
			name:   "ResolutionFailedError",
			err:    &dberrors.WrappedMessageError{Message: "wrapped", Cause: &ResolutionFailedError{URI: "test"}},
			target: &ResolutionFailedError{},
		},
		{
			name:   "RemoteConnectionFailedError",
			err:    &dberrors.WrappedMessageError{Message: "wrapped", Cause: &RemoteConnectionFailedError{Endpoint: "test"}},
			target: &RemoteConnectionFailedError{},
		},
		{
			name:   "InvalidSecretURIError",
			err:    &dberrors.WrappedMessageError{Message: "wrapped", Cause: &InvalidSecretURIError{URI: "test"}},
			target: &InvalidSecretURIError{},
		},
		{
			name:   "EntropyGenerationFailedError",
			err:    &dberrors.WrappedMessageError{Message: "wrapped", Cause: &EntropyGenerationFailedError{RequestedBytes: 32}},
			target: &EntropyGenerationFailedError{},
		},
		{
			name:   "UnsupportedSchemeError",
			err:    &dberrors.WrappedMessageError{Message: "wrapped", Cause: &UnsupportedSchemeError{Scheme: "test"}},
			target: &UnsupportedSchemeError{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			switch target := tc.target.(type) {
			case *SecretResolutionError:
				var found *SecretResolutionError
				if !stderrors.As(tc.err, &found) {
					t.Error("Expected errors.As to find type")
				}
			case *SecretNotFoundError:
				var found *SecretNotFoundError
				if !stderrors.As(tc.err, &found) {
					t.Error("Expected errors.As to find type")
				}
			case *ResolutionFailedError:
				var found *ResolutionFailedError
				if !stderrors.As(tc.err, &found) {
					t.Error("Expected errors.As to find type")
				}
			case *RemoteConnectionFailedError:
				var found *RemoteConnectionFailedError
				if !stderrors.As(tc.err, &found) {
					t.Error("Expected errors.As to find type")
				}
			case *InvalidSecretURIError:
				var found *InvalidSecretURIError
				if !stderrors.As(tc.err, &found) {
					t.Error("Expected errors.As to find type")
				}
			case *EntropyGenerationFailedError:
				var found *EntropyGenerationFailedError
				if !stderrors.As(tc.err, &found) {
					t.Error("Expected errors.As to find type")
				}
			case *UnsupportedSchemeError:
				var found *UnsupportedSchemeError
				if !stderrors.As(tc.err, &found) {
					t.Error("Expected errors.As to find type")
				}
			default:
				t.Errorf("Unknown target type: %T", target)
			}
		})
	}
}
