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

package ociregistry

import (
	"errors"
	"strings"
	"testing"
)

func TestErrAttestationRequired(t *testing.T) {
	err := AttestationRequiredError{}

	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}

	if !strings.Contains(err.Error(), "attestation") {
		t.Errorf("expected error to contain 'attestation', got: %s", err.Error())
	}
}

func TestErrAttestationFailed(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		contains string
	}{
		{
			name:     "with reason",
			reason:   "quote signature invalid",
			contains: "quote signature invalid",
		},
		{
			name:     "empty reason",
			reason:   "",
			contains: "attestation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AttestationFailedError{Reason: tt.reason}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, err.Error())
			}
		})
	}
}

func TestErrInvalidQuote(t *testing.T) {
	tests := []struct {
		name     string
		err      InvalidQuoteError
		contains string
	}{
		{
			name:     "with reason",
			err:      InvalidQuoteError{Reason: "malformed nonce"},
			contains: "malformed nonce",
		},
		{
			name:     "with cause",
			err:      InvalidQuoteError{Cause: errors.New("underlying error")},
			contains: "underlying error",
		},
		{
			name:     "empty",
			err:      InvalidQuoteError{},
			contains: "invalid TPM quote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, tt.err.Error())
			}
		})
	}

	// Test Unwrap
	cause := errors.New("wrapped error")
	err := InvalidQuoteError{Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestErrAccessDenied(t *testing.T) {
	tests := []struct {
		name     string
		err      AccessDeniedError
		contains []string
	}{
		{
			name:     "with subject and resource",
			err:      AccessDeniedError{Subject: "client1", Resource: "image:latest", Reason: "policy violation"},
			contains: []string{"client1", "image:latest", "policy violation"},
		},
		{
			name:     "reason only",
			err:      AccessDeniedError{Reason: "unauthorized"},
			contains: []string{"unauthorized"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := tt.err.Error()
			for _, c := range tt.contains {
				if !strings.Contains(errMsg, c) {
					t.Errorf("expected error to contain %q, got: %s", c, errMsg)
				}
			}
		})
	}
}

func TestErrImageNotFound(t *testing.T) {
	err := ImageNotFoundError{Reference: "myrepo/myimage:v1"}

	if !strings.Contains(err.Error(), "myrepo/myimage:v1") {
		t.Errorf("expected error to contain reference, got: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to contain 'not found', got: %s", err.Error())
	}
}

func TestErrImageExists(t *testing.T) {
	err := ImageExistsError{Reference: "myrepo/myimage:v1"}

	if !strings.Contains(err.Error(), "myrepo/myimage:v1") {
		t.Errorf("expected error to contain reference, got: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected error to contain 'already exists', got: %s", err.Error())
	}
}

func TestErrDecryptionFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      DecryptionFailedError
		contains string
	}{
		{
			name:     "with layer and cause",
			err:      DecryptionFailedError{LayerID: "sha256:abc123", Cause: errors.New("key not found")},
			contains: "sha256:abc123",
		},
		{
			name:     "with reference and cause",
			err:      DecryptionFailedError{Reference: "myimage:v1", Cause: errors.New("key error")},
			contains: "myimage:v1",
		},
		{
			name:     "cause only",
			err:      DecryptionFailedError{Cause: errors.New("decrypt error")},
			contains: "decrypt error",
		},
		{
			name:     "empty",
			err:      DecryptionFailedError{},
			contains: "decryption failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, tt.err.Error())
			}
		})
	}

	// Test Unwrap
	cause := errors.New("wrapped error")
	err := DecryptionFailedError{Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestErrEncryptionFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      EncryptionFailedError
		contains string
	}{
		{
			name:     "with layer and cause",
			err:      EncryptionFailedError{LayerID: "sha256:abc123", Cause: errors.New("key generation failed")},
			contains: "sha256:abc123",
		},
		{
			name:     "with reference and cause",
			err:      EncryptionFailedError{Reference: "myimage:v1", Cause: errors.New("key error")},
			contains: "myimage:v1",
		},
		{
			name:     "cause only",
			err:      EncryptionFailedError{Cause: errors.New("encrypt error")},
			contains: "encrypt error",
		},
		{
			name:     "empty",
			err:      EncryptionFailedError{},
			contains: "encryption failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, tt.err.Error())
			}
		})
	}

	// Test Unwrap
	cause := errors.New("wrapped error")
	err := EncryptionFailedError{Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestErrStorageError(t *testing.T) {
	tests := []struct {
		name     string
		err      ErrStorageError
		contains []string
	}{
		{
			name:     "with key and cause",
			err:      ErrStorageError{Operation: "get", Key: "sha256:abc", Cause: errors.New("connection failed")},
			contains: []string{"get", "sha256:abc", "connection failed"},
		},
		{
			name:     "with cause only",
			err:      ErrStorageError{Operation: "put", Cause: errors.New("timeout")},
			contains: []string{"put", "timeout"},
		},
		{
			name:     "operation only",
			err:      ErrStorageError{Operation: "delete"},
			contains: []string{"delete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := tt.err.Error()
			for _, c := range tt.contains {
				if !strings.Contains(errMsg, c) {
					t.Errorf("expected error to contain %q, got: %s", c, errMsg)
				}
			}
		})
	}

	// Test Unwrap
	cause := errors.New("wrapped error")
	err := ErrStorageError{Operation: "get", Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestErrRegistryNotInitialized(t *testing.T) {
	err := RegistryNotInitializedError{}

	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected error to contain 'not initialized', got: %s", err.Error())
	}
}

func TestErrRegistryClosed(t *testing.T) {
	err := RegistryClosedError{}

	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected error to contain 'closed', got: %s", err.Error())
	}
}

func TestErrInvalidImageRef(t *testing.T) {
	err := InvalidImageRefError{Reference: "INVALID/ref:tag", Reason: "uppercase not allowed"}

	if !strings.Contains(err.Error(), "INVALID/ref:tag") {
		t.Errorf("expected error to contain reference, got: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "uppercase not allowed") {
		t.Errorf("expected error to contain reason, got: %s", err.Error())
	}
}

func TestErrBlobNotFound(t *testing.T) {
	err := BlobNotFoundError{Digest: "sha256:abc123def456"}

	if !strings.Contains(err.Error(), "sha256:abc123def456") {
		t.Errorf("expected error to contain digest, got: %s", err.Error())
	}
}

func TestErrManifestNotFound(t *testing.T) {
	err := ManifestNotFoundError{Reference: "myrepo/myimage:v1"}

	if !strings.Contains(err.Error(), "myrepo/myimage:v1") {
		t.Errorf("expected error to contain reference, got: %s", err.Error())
	}
}

func TestErrInvalidManifest(t *testing.T) {
	err := InvalidManifestError{Reference: "myimage:v1", Reason: "missing config"}

	if !strings.Contains(err.Error(), "myimage:v1") {
		t.Errorf("expected error to contain reference, got: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "missing config") {
		t.Errorf("expected error to contain reason, got: %s", err.Error())
	}
}

func TestErrKeyNotFound(t *testing.T) {
	err := KeyNotFoundError{KeyID: "my-encryption-key"}

	if !strings.Contains(err.Error(), "my-encryption-key") {
		t.Errorf("expected error to contain key ID, got: %s", err.Error())
	}
}

func TestErrKeyCreationFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      KeyCreationFailedError
		contains string
	}{
		{
			name:     "with key and cause",
			err:      KeyCreationFailedError{KeyID: "my-key", Cause: errors.New("permission denied")},
			contains: "my-key",
		},
		{
			name:     "cause only",
			err:      KeyCreationFailedError{Cause: errors.New("db error")},
			contains: "db error",
		},
		{
			name:     "empty",
			err:      KeyCreationFailedError{},
			contains: "key creation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, tt.err.Error())
			}
		})
	}

	// Test Unwrap
	cause := errors.New("wrapped error")
	err := KeyCreationFailedError{Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestErrKeyWrapFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      KeyWrapFailedError
		contains string
	}{
		{
			name:     "with key and cause",
			err:      KeyWrapFailedError{KeyID: "kek-1", Cause: errors.New("key expired")},
			contains: "kek-1",
		},
		{
			name:     "cause only",
			err:      KeyWrapFailedError{Cause: errors.New("crypto error")},
			contains: "crypto error",
		},
		{
			name:     "empty",
			err:      KeyWrapFailedError{},
			contains: "DEK wrap failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, tt.err.Error())
			}
		})
	}

	// Test Unwrap
	cause := errors.New("wrapped error")
	err := KeyWrapFailedError{Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestErrKeyUnwrapFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      KeyUnwrapFailedError
		contains string
	}{
		{
			name:     "with key and cause",
			err:      KeyUnwrapFailedError{KeyID: "kek-1", Cause: errors.New("decryption failed")},
			contains: "kek-1",
		},
		{
			name:     "cause only",
			err:      KeyUnwrapFailedError{Cause: errors.New("invalid ciphertext")},
			contains: "invalid ciphertext",
		},
		{
			name:     "empty",
			err:      KeyUnwrapFailedError{},
			contains: "DEK unwrap failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, tt.err.Error())
			}
		})
	}

	// Test Unwrap
	cause := errors.New("wrapped error")
	err := KeyUnwrapFailedError{Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestErrInvalidConfig(t *testing.T) {
	tests := []struct {
		name     string
		err      InvalidConfigError
		contains []string
	}{
		{
			name:     "with field and reason",
			err:      InvalidConfigError{Field: "listen_addr", Reason: "cannot be empty"},
			contains: []string{"listen_addr", "cannot be empty"},
		},
		{
			name:     "reason only",
			err:      InvalidConfigError{Reason: "missing required fields"},
			contains: []string{"missing required fields"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := tt.err.Error()
			for _, c := range tt.contains {
				if !strings.Contains(errMsg, c) {
					t.Errorf("expected error to contain %q, got: %s", c, errMsg)
				}
			}
		})
	}
}

func TestErrPCRMismatch(t *testing.T) {
	expected := []byte{0x01, 0x02, 0x03}
	actual := []byte{0x04, 0x05, 0x06}
	err := PCRMismatchError{PCRIndex: 7, Expected: expected, Actual: actual}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "7") {
		t.Errorf("expected error to contain PCR index, got: %s", errMsg)
	}

	if !strings.Contains(errMsg, "010203") {
		t.Errorf("expected error to contain expected value, got: %s", errMsg)
	}

	if !strings.Contains(errMsg, "040506") {
		t.Errorf("expected error to contain actual value, got: %s", errMsg)
	}
}

func TestErrAIKNotAllowed(t *testing.T) {
	err := AIKNotAllowedError{AIK: "sha256:fingerprint123"}

	if !strings.Contains(err.Error(), "sha256:fingerprint123") {
		t.Errorf("expected error to contain AIK, got: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "not in allowed list") {
		t.Errorf("expected error to contain 'not in allowed list', got: %s", err.Error())
	}
}

func TestErrQuicBaaSConnection(t *testing.T) {
	tests := []struct {
		name     string
		err      QuicBaaSConnectionError
		contains []string
	}{
		{
			name:     "with cause",
			err:      QuicBaaSConnectionError{Addr: "localhost:9000", Cause: errors.New("connection refused")},
			contains: []string{"localhost:9000", "connection refused"},
		},
		{
			name:     "without cause",
			err:      QuicBaaSConnectionError{Addr: "localhost:9000"},
			contains: []string{"localhost:9000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := tt.err.Error()
			for _, c := range tt.contains {
				if !strings.Contains(errMsg, c) {
					t.Errorf("expected error to contain %q, got: %s", c, errMsg)
				}
			}
		})
	}

	// Test Unwrap
	cause := errors.New("wrapped error")
	err := QuicBaaSConnectionError{Addr: "localhost:9000", Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestErrSessionExpired(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		contains  string
	}{
		{
			name:      "with session ID",
			sessionID: "session-123",
			contains:  "session-123",
		},
		{
			name:      "without session ID",
			sessionID: "",
			contains:  "expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SessionExpiredError{SessionID: tt.sessionID}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, err.Error())
			}
		})
	}
}

func TestErrSessionNotFound(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		contains  string
	}{
		{
			name:      "with session ID",
			sessionID: "session-456",
			contains:  "session-456",
		},
		{
			name:      "without session ID",
			sessionID: "",
			contains:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SessionNotFoundError{SessionID: tt.sessionID}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, err.Error())
			}
		})
	}
}

func TestErrInvalidSession(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		contains string
	}{
		{
			name:     "with reason",
			reason:   "token malformed",
			contains: "token malformed",
		},
		{
			name:     "without reason",
			reason:   "",
			contains: "invalid session",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := InvalidSessionError{Reason: tt.reason}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, err.Error())
			}
		})
	}
}

func TestErrOperationTimeout(t *testing.T) {
	err := OperationTimeoutError{Operation: "pull"}

	if !strings.Contains(err.Error(), "pull") {
		t.Errorf("expected error to contain operation, got: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected error to contain 'timed out', got: %s", err.Error())
	}
}

func TestErrDigestMismatch(t *testing.T) {
	err := DigestMismatchError{Expected: "sha256:expected", Actual: "sha256:actual"}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "sha256:expected") {
		t.Errorf("expected error to contain expected digest, got: %s", errMsg)
	}

	if !strings.Contains(errMsg, "sha256:actual") {
		t.Errorf("expected error to contain actual digest, got: %s", errMsg)
	}
}

func TestErrNilClient(t *testing.T) {
	tests := []struct {
		name     string
		client   string
		contains string
	}{
		{
			name:     "with client name",
			client:   "QuicBaaS",
			contains: "QuicBaaS",
		},
		{
			name:     "without client name",
			client:   "",
			contains: "client is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NilClientError{Client: tt.client}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, err.Error())
			}
		})
	}
}

func TestErrNilContext(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		contains  string
	}{
		{
			name:      "with operation",
			operation: "Push",
			contains:  "Push",
		},
		{
			name:      "without operation",
			operation: "",
			contains:  "nil context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NilContextError{Operation: tt.operation}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, err.Error())
			}
		})
	}
}

func TestErrImageEmpty(t *testing.T) {
	err := ImageEmptyError{}

	if !strings.Contains(err.Error(), "no layers") {
		t.Errorf("expected error to contain 'no layers', got: %s", err.Error())
	}
}

func TestErrPolicyEvaluationFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      PolicyEvaluationFailedError
		contains string
	}{
		{
			name:     "with policy and cause",
			err:      PolicyEvaluationFailedError{Policy: "registry.access", Cause: errors.New("undefined rule")},
			contains: "registry.access",
		},
		{
			name:     "cause only",
			err:      PolicyEvaluationFailedError{Cause: errors.New("rego error")},
			contains: "rego error",
		},
		{
			name:     "empty",
			err:      PolicyEvaluationFailedError{},
			contains: "policy evaluation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, tt.err.Error())
			}
		})
	}

	// Test Unwrap
	cause := errors.New("wrapped error")
	err := PolicyEvaluationFailedError{Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestErrInvalidDigest(t *testing.T) {
	tests := []struct {
		name     string
		err      InvalidDigestError
		contains string
	}{
		{
			name:     "with digest and reason",
			err:      InvalidDigestError{Digest: "invalid-digest", Reason: "missing algorithm prefix"},
			contains: "invalid-digest",
		},
		{
			name:     "digest only",
			err:      InvalidDigestError{Digest: "bad"},
			contains: "bad",
		},
		{
			name:     "empty",
			err:      InvalidDigestError{},
			contains: "invalid digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("expected error to contain %q, got: %s", tt.contains, tt.err.Error())
			}
		})
	}
}

// Test errors.Is for checking error types.
func TestErrorsIs(t *testing.T) {
	err := ImageNotFoundError{Reference: "test:latest"}

	var target ImageNotFoundError
	if !errors.As(err, &target) {
		t.Error("errors.As should match ImageNotFoundError")
	}

	if target.Reference != "test:latest" {
		t.Errorf("expected reference 'test:latest', got: %s", target.Reference)
	}
}

// Test error wrapping chain.
func TestErrorWrappingChain(t *testing.T) {
	root := errors.New("root cause")
	decryptErr := DecryptionFailedError{Reference: "myimage", Cause: root}
	storageErr := ErrStorageError{Operation: "get", Cause: decryptErr}

	// Should be able to unwrap to decryption error
	var decryptTarget DecryptionFailedError
	if !errors.As(storageErr, &decryptTarget) {
		t.Error("should be able to unwrap to DecryptionFailedError")
	}

	// Should be able to find root cause
	if !errors.Is(storageErr.Unwrap(), decryptErr) {
		t.Error("should find DecryptionFailedError in chain")
	}
}
