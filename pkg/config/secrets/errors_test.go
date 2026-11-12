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
	stderrors "errors"
	"strings"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestSecretResolutionError_Error tests all branches of SecretResolutionError.Error()
func TestSecretResolutionError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *SecretResolutionError
		expected string
	}{
		{
			name: "with scheme, message, and cause",
			err: &SecretResolutionError{
				Scheme:  "kms",
				Message: "secret not found",
				Cause:   stderrors.New("underlying error"),
			},
			expected: "kms: secret not found: underlying error",
		},
		{
			name: "with scheme and message only",
			err: &SecretResolutionError{
				Scheme:  "vault",
				Message: "connection timeout",
				Cause:   nil,
			},
			expected: "vault: connection timeout",
		},
		{
			name: "with message only",
			err: &SecretResolutionError{
				Scheme:  "",
				Message: "generic error",
				Cause:   nil,
			},
			expected: "generic error",
		},
		{
			name: "empty scheme with cause",
			err: &SecretResolutionError{
				Scheme:  "",
				Message: "something failed",
				Cause:   stderrors.New("root cause"),
			},
			expected: ": something failed: root cause",
		},
		{
			name: "all empty fields",
			err: &SecretResolutionError{
				Scheme:  "",
				Message: "",
				Cause:   nil,
			},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestSecretResolutionError_Unwrap tests error unwrapping
func TestSecretResolutionError_Unwrap(t *testing.T) {
	tests := []struct {
		name      string
		err       *SecretResolutionError
		expectNil bool
	}{
		{
			name: "with cause",
			err: &SecretResolutionError{
				Scheme:  "kms",
				Message: "error",
				Cause:   stderrors.New("root cause"),
			},
			expectNil: false,
		},
		{
			name: "without cause",
			err: &SecretResolutionError{
				Scheme:  "kms",
				Message: "error",
				Cause:   nil,
			},
			expectNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			unwrapped := tc.err.Unwrap()
			if tc.expectNil && unwrapped != nil {
				t.Errorf("Expected nil, got %v", unwrapped)
			}
			if !tc.expectNil && unwrapped == nil {
				t.Error("Expected non-nil, got nil")
			}
		})
	}
}

// TestSecretResolutionError_ErrorsIs tests errors.Is support through Unwrap
func TestSecretResolutionError_ErrorsIs(t *testing.T) {
	sentinel := stderrors.New("sentinel error")
	err := &SecretResolutionError{
		Scheme:  "test",
		Message: "wrapped sentinel",
		Cause:   sentinel,
	}

	if !stderrors.Is(err, sentinel) {
		t.Error("Expected errors.Is to find sentinel error")
	}

	otherErr := stderrors.New("other error")
	if stderrors.Is(err, otherErr) {
		t.Error("Expected errors.Is to not find other error")
	}
}

// TestNewSecretResolutionError tests constructor
func TestNewSecretResolutionError(t *testing.T) {
	cause := stderrors.New("test cause")
	err := NewSecretResolutionError("kms", "resolution failed", cause)

	if err.Scheme != "kms" {
		t.Errorf("Expected scheme 'kms', got %s", err.Scheme)
	}
	if err.Message != "resolution failed" {
		t.Errorf("Expected message 'resolution failed', got %s", err.Message)
	}
	if err.Cause != cause {
		t.Errorf("Expected cause to match")
	}
}

// TestSecretNotFoundError_Error tests all branches of SecretNotFoundError.Error()
func TestSecretNotFoundError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *SecretNotFoundError
		expected string
	}{
		{
			name: "with provider",
			err: &SecretNotFoundError{
				Path:     "/secrets/db/password",
				Provider: "vault",
			},
			expected: `secret not found: path "/secrets/db/password" in provider "vault"`,
		},
		{
			name: "without provider",
			err: &SecretNotFoundError{
				Path:     "/secrets/api/key",
				Provider: "",
			},
			expected: `secret not found: path "/secrets/api/key"`,
		},
		{
			name: "empty path with provider",
			err: &SecretNotFoundError{
				Path:     "",
				Provider: "kms",
			},
			expected: `secret not found: path "" in provider "kms"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestResolutionFailedError_Error tests all branches
func TestResolutionFailedError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *ResolutionFailedError
		expected string
	}{
		{
			name: "with reason",
			err: &ResolutionFailedError{
				URI:    "kms://secret/path",
				Reason: "connection refused",
			},
			expected: `failed to resolve secret URI "kms://secret/path": connection refused`,
		},
		{
			name: "without reason",
			err: &ResolutionFailedError{
				URI:    "vault://secret/path",
				Reason: "",
			},
			expected: `failed to resolve secret URI "vault://secret/path"`,
		},
		{
			name: "empty URI with reason",
			err: &ResolutionFailedError{
				URI:    "",
				Reason: "unknown error",
			},
			expected: `failed to resolve secret URI "": unknown error`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestRemoteConnectionFailedError_Error tests all branch combinations
func TestRemoteConnectionFailedError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *RemoteConnectionFailedError
		contains []string
	}{
		{
			name: "all fields populated",
			err: &RemoteConnectionFailedError{
				Endpoint: "https://vault.example.com:8200",
				Provider: "vault",
				Cause:    stderrors.New("connection refused"),
			},
			contains: []string{"vault KMS", "https://vault.example.com:8200", "connection refused"},
		},
		{
			name: "provider and endpoint only",
			err: &RemoteConnectionFailedError{
				Endpoint: "https://kms.aws.com",
				Provider: "aws",
				Cause:    nil,
			},
			contains: []string{"aws KMS", "https://kms.aws.com"},
		},
		{
			name: "provider only",
			err: &RemoteConnectionFailedError{
				Endpoint: "",
				Provider: "gcp",
				Cause:    nil,
			},
			contains: []string{"gcp KMS"},
		},
		{
			name: "endpoint only",
			err: &RemoteConnectionFailedError{
				Endpoint: "https://remote.host:443",
				Provider: "",
				Cause:    nil,
			},
			contains: []string{"remote KMS", "https://remote.host:443"},
		},
		{
			name: "cause only",
			err: &RemoteConnectionFailedError{
				Endpoint: "",
				Provider: "",
				Cause:    stderrors.New("timeout"),
			},
			contains: []string{"remote KMS", "timeout"},
		},
		{
			name: "all empty",
			err: &RemoteConnectionFailedError{
				Endpoint: "",
				Provider: "",
				Cause:    nil,
			},
			contains: []string{"failed to connect to remote KMS"},
		},
		{
			name: "provider and cause",
			err: &RemoteConnectionFailedError{
				Endpoint: "",
				Provider: "azure",
				Cause:    stderrors.New("auth failed"),
			},
			contains: []string{"azure KMS", "auth failed"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			for _, substr := range tc.contains {
				if !strings.Contains(result, substr) {
					t.Errorf("Expected error message to contain %q, got %q", substr, result)
				}
			}
		})
	}
}

// TestRemoteConnectionFailedError_Unwrap tests error unwrapping
func TestRemoteConnectionFailedError_Unwrap(t *testing.T) {
	tests := []struct {
		name      string
		err       *RemoteConnectionFailedError
		expectNil bool
	}{
		{
			name: "with cause",
			err: &RemoteConnectionFailedError{
				Endpoint: "https://vault.example.com",
				Provider: "vault",
				Cause:    stderrors.New("connection timeout"),
			},
			expectNil: false,
		},
		{
			name: "without cause",
			err: &RemoteConnectionFailedError{
				Endpoint: "https://vault.example.com",
				Provider: "vault",
				Cause:    nil,
			},
			expectNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			unwrapped := tc.err.Unwrap()
			if tc.expectNil && unwrapped != nil {
				t.Errorf("Expected nil, got %v", unwrapped)
			}
			if !tc.expectNil && unwrapped == nil {
				t.Error("Expected non-nil, got nil")
			}
		})
	}
}

// TestRemoteConnectionFailedError_ErrorsIs tests errors.Is support
func TestRemoteConnectionFailedError_ErrorsIs(t *testing.T) {
	sentinel := stderrors.New("sentinel connection error")
	err := &RemoteConnectionFailedError{
		Endpoint: "https://test.example.com",
		Provider: "test",
		Cause:    sentinel,
	}

	if !stderrors.Is(err, sentinel) {
		t.Error("Expected errors.Is to find sentinel error")
	}
}

// TestInvalidSecretURIError_Error tests all branches
func TestInvalidSecretURIError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *InvalidSecretURIError
		expected string
	}{
		{
			name: "with reason",
			err: &InvalidSecretURIError{
				URI:    "invalid://bad/uri",
				Reason: "scheme not recognized",
			},
			expected: `invalid secret URI "invalid://bad/uri": scheme not recognized`,
		},
		{
			name: "without reason",
			err: &InvalidSecretURIError{
				URI:    "malformed-uri",
				Reason: "",
			},
			expected: `invalid secret URI "malformed-uri"`,
		},
		{
			name: "empty URI with reason",
			err: &InvalidSecretURIError{
				URI:    "",
				Reason: "empty URI",
			},
			expected: `invalid secret URI "": empty URI`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestEntropyGenerationFailedError_ErrorMethod tests all branch combinations
func TestEntropyGenerationFailedError_ErrorMethod(t *testing.T) {
	tests := []struct {
		name     string
		err      *EntropyGenerationFailedError
		expected string
	}{
		{
			name: "with bytes and cause",
			err: &EntropyGenerationFailedError{
				RequestedBytes: 32,
				Cause:          stderrors.New("entropy pool exhausted"),
			},
			expected: "failed to generate 32 bytes of entropy: entropy pool exhausted",
		},
		{
			name: "with cause only",
			err: &EntropyGenerationFailedError{
				RequestedBytes: 0,
				Cause:          stderrors.New("random device unavailable"),
			},
			expected: "failed to generate entropy: random device unavailable",
		},
		{
			name: "with bytes only",
			err: &EntropyGenerationFailedError{
				RequestedBytes: 64,
				Cause:          nil,
			},
			expected: "failed to generate 64 bytes of entropy",
		},
		{
			name: "no bytes no cause",
			err: &EntropyGenerationFailedError{
				RequestedBytes: 0,
				Cause:          nil,
			},
			expected: "failed to generate entropy",
		},
		{
			name: "negative bytes with cause",
			err: &EntropyGenerationFailedError{
				RequestedBytes: -1,
				Cause:          stderrors.New("invalid request"),
			},
			expected: "failed to generate entropy: invalid request",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestEntropyGenerationFailedError_UnwrapMethod tests error unwrapping
func TestEntropyGenerationFailedError_UnwrapMethod(t *testing.T) {
	tests := []struct {
		name      string
		err       *EntropyGenerationFailedError
		expectNil bool
	}{
		{
			name: "with cause",
			err: &EntropyGenerationFailedError{
				RequestedBytes: 32,
				Cause:          stderrors.New("entropy failure"),
			},
			expectNil: false,
		},
		{
			name: "without cause",
			err: &EntropyGenerationFailedError{
				RequestedBytes: 32,
				Cause:          nil,
			},
			expectNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			unwrapped := tc.err.Unwrap()
			if tc.expectNil && unwrapped != nil {
				t.Errorf("Expected nil, got %v", unwrapped)
			}
			if !tc.expectNil && unwrapped == nil {
				t.Error("Expected non-nil, got nil")
			}
		})
	}
}

// TestEntropyGenerationFailedError_ErrorsIs tests errors.Is support
func TestEntropyGenerationFailedError_ErrorsIs(t *testing.T) {
	sentinel := stderrors.New("sentinel entropy error")
	err := &EntropyGenerationFailedError{
		RequestedBytes: 16,
		Cause:          sentinel,
	}

	if !stderrors.Is(err, sentinel) {
		t.Error("Expected errors.Is to find sentinel error")
	}
}

// TestUnsupportedSchemeError_Error tests all branches
func TestUnsupportedSchemeError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *UnsupportedSchemeError
		expected string
	}{
		{
			name: "with supported schemes",
			err: &UnsupportedSchemeError{
				Scheme:           "unknown",
				URI:              "unknown://secret/path",
				SupportedSchemes: []string{"kms", "vault", "env"},
			},
			expected: `unsupported scheme "unknown" in URI "unknown://secret/path": supported schemes are [kms vault env]`,
		},
		{
			name: "without supported schemes",
			err: &UnsupportedSchemeError{
				Scheme:           "custom",
				URI:              "custom://path",
				SupportedSchemes: nil,
			},
			expected: `unsupported scheme "custom" in URI "custom://path"`,
		},
		{
			name: "empty supported schemes slice",
			err: &UnsupportedSchemeError{
				Scheme:           "test",
				URI:              "test://uri",
				SupportedSchemes: []string{},
			},
			expected: `unsupported scheme "test" in URI "test://uri"`,
		},
		{
			name: "single supported scheme",
			err: &UnsupportedSchemeError{
				Scheme:           "bad",
				URI:              "bad://path",
				SupportedSchemes: []string{"kms"},
			},
			expected: `unsupported scheme "bad" in URI "bad://path": supported schemes are [kms]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
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

func TestNewResolutionFailedError_UnwrapsToCause(t *testing.T) {
	sentinel := stderrors.New("kms client failure")
	err := NewResolutionFailedError("op", "kms://secret", "failed to retrieve secret from KMS", sentinel)

	if !stderrors.Is(err, sentinel) {
		t.Errorf("errors.Is(err, sentinel) = false, want true")
	}
	if !IsResolutionFailed(err) {
		t.Errorf("IsResolutionFailed(err) = false, want true")
	}

	var rfe *ResolutionFailedError
	if !stderrors.As(err, &rfe) {
		t.Fatal("errors.As(err, *ResolutionFailedError) = false, want true")
	}
	if rfe.Cause != sentinel {
		t.Errorf("Cause = %v, want %v", rfe.Cause, sentinel)
	}
	if got, want := rfe.Error(), `failed to resolve secret URI "kms://secret": failed to retrieve secret from KMS`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got := err.Context["cause"]; got != sentinel.Error() {
		t.Errorf("Context[cause] = %v, want %q", got, sentinel.Error())
	}
}

func TestResolutionFailedError_NilCauseUnwrapsToNil(t *testing.T) {
	err := NewResolutionFailedError("op", "kms://secret", "no cause", nil)

	var rfe *ResolutionFailedError
	if !stderrors.As(err, &rfe) {
		t.Fatal("errors.As(err, *ResolutionFailedError) = false, want true")
	}
	if got := rfe.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
	if _, ok := err.Context["cause"]; ok {
		t.Errorf("Context[cause] set for a nil cause")
	}
	if !IsResolutionFailed(err) {
		t.Errorf("IsResolutionFailed(err) = false, want true")
	}
}

func TestLocalKMSResolver_Resolve_CancelledContextUnwrapsToCanceled(t *testing.T) {
	client := newMockSecretsClient().withSecret("secret", []byte("value"))
	resolver := NewLocalKMSResolver(client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolver.Resolve(ctx, "kms://secret")
	if err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}
	if !stderrors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false, want true; err = %v", err)
	}
	if !IsResolutionFailed(err) {
		t.Errorf("IsResolutionFailed(err) = false, want true")
	}
}
