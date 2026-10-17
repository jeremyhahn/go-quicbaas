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

package service

import (
	"context"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// =============================================================================
// ExportKey Deep Path Tests
// =============================================================================

func TestKMSService_ExportKey_NilRequest(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.ExportKey(systemTenantCtx(), nil)

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_ExportKey_SourceKeyNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	// Do not add any keys - source key will not be found
	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "missing-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source key not found")
}

func TestKMSService_ExportKey_KeyNotExportable(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		if keyID == "source-key" {
			return &KeyMetadata{
				KeyID:      "source-key",
				Algorithm:  KeyAlgAES256GCM,
				Version:    1,
				Exportable: false,
				Status:     KeyStatusEnabled,
			}, nil
		}
		return nil, errors.New("key not found")
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not exportable")
}

func TestKMSService_ExportKey_KeyNotEnabled(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		if keyID == "source-key" {
			return &KeyMetadata{
				KeyID:      "source-key",
				Algorithm:  KeyAlgAES256GCM,
				Version:    1,
				Exportable: true,
				Status:     KeyStatusDisabled,
			}, nil
		}
		return nil, errors.New("key not found")
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestKMSService_ExportKey_WrappingKeyNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		if keyID == "source-key" {
			return &KeyMetadata{
				KeyID:      "source-key",
				Algorithm:  KeyAlgAES256GCM,
				Version:    1,
				Exportable: true,
				Status:     KeyStatusEnabled,
			}, nil
		}
		return nil, errors.New("key not found")
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "missing-wrapper",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping key not found")
}

func TestKMSService_ExportKey_WrappingKeyNotEnabled(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		if keyID == "source-key" {
			return &KeyMetadata{
				KeyID:      "source-key",
				Algorithm:  KeyAlgAES256GCM,
				Version:    1,
				Exportable: true,
				Status:     KeyStatusEnabled,
			}, nil
		}
		if keyID == "wrapper-key" {
			return &KeyMetadata{
				KeyID:  "wrapper-key",
				Status: KeyStatusDisabled,
			}, nil
		}
		return nil, errors.New("key not found")
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping key")
	assert.Contains(t, err.Error(), "not enabled")
}

func TestKMSService_ExportKey_UnknownMetadataType(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	// Return an unknown type for the source key metadata
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return struct{ Name string }{Name: "unknown"}, nil
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown metadata type")
}

func TestKMSService_ExportKey_WithKMSKeyMetadata(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	// Use kms.KeyMetadata type for the source key
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		if keyID == "source-key" {
			return &kms.KeyMetadata{
				KeyID:      "source-key",
				Algorithm:  "AES-256-GCM",
				Version:    1,
				Exportable: true,
				State:      kms.KeyStateEnabled,
			}, nil
		}
		if keyID == "wrapper-key" {
			return &kms.KeyMetadata{
				KeyID: "wrapper-key",
				State: kms.KeyStateEnabled,
			}, nil
		}
		return nil, errors.New("key not found")
	}

	mockMgr.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		return make([]byte, 32), make([]byte, 48), nil
	}

	mockMgr.encryptFn = func(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
		return []byte("wrapped-key-material"), nil
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Format:            "raw",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "source-key", resp.KeyID)
	assert.Equal(t, uint64(1), resp.Version)
	assert.Equal(t, "wrapper-key", resp.WrappingKeyID)
	assert.Equal(t, "AES-KW", resp.WrappingAlgorithm)
	assert.Equal(t, "raw", resp.Format)
}

func TestKMSService_ExportKey_WithServiceKeyMetadata(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	// Use service.KeyMetadata type for the source key
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		if keyID == "source-key" {
			return &KeyMetadata{
				KeyID:      "source-key",
				Algorithm:  KeyAlgAES256GCM,
				Version:    2,
				Exportable: true,
				Status:     KeyStatusEnabled,
			}, nil
		}
		if keyID == "wrapper-key" {
			return &KeyMetadata{
				KeyID:  "wrapper-key",
				Status: KeyStatusEnabled,
			}, nil
		}
		return nil, errors.New("key not found")
	}

	mockMgr.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		return make([]byte, 32), make([]byte, 48), nil
	}

	mockMgr.encryptFn = func(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
		return []byte("wrapped-key-data"), nil
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Version:           5, // Override version
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(5), resp.Version) // Should use requested version
}

func TestKMSService_ExportKey_GenerateDataKeyError(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:      keyID,
			Algorithm:  KeyAlgAES256GCM,
			Version:    1,
			Exportable: true,
			Status:     KeyStatusEnabled,
		}, nil
	}

	mockMgr.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		return nil, nil, errors.New("HSM failure")
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate export key material")
}

func TestKMSService_ExportKey_EncryptError(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:      keyID,
			Algorithm:  KeyAlgAES256GCM,
			Version:    1,
			Exportable: true,
			Status:     KeyStatusEnabled,
		}, nil
	}

	mockMgr.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		return make([]byte, 32), make([]byte, 48), nil
	}

	mockMgr.encryptFn = func(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
		return nil, errors.New("encryption failure")
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to wrap key")
}

func TestKMSService_ExportKey_WithAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)
	auditLogger := &mockKMSAuditLogger{}
	svc.auditLogger = auditLogger

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:      keyID,
			Algorithm:  KeyAlgAES256GCM,
			Version:    1,
			Exportable: true,
			Status:     KeyStatusEnabled,
		}, nil
	}

	mockMgr.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
		return make([]byte, 32), make([]byte, 48), nil
	}

	mockMgr.encryptFn = func(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
		return []byte("wrapped"), nil
	}

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify audit logger was called
	assert.Contains(t, auditLogger.exports, "source-key")
}

func TestKMSService_ExportKey_FailedAuditOnSourceNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)
	auditLogger := &mockKMSAuditLogger{}
	svc.auditLogger = auditLogger

	resp, err := svc.ExportKey(systemTenantCtx(), &ExportKeyRequest{
		KeyID:             "missing-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	// Audit logger should have been called with failure
	assert.Contains(t, auditLogger.exports, "missing-key")
}

// =============================================================================
// UnwrapKey Deep Path Tests
// =============================================================================

func TestKMSService_UnwrapKey_NilRequest(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.UnwrapKey(systemTenantCtx(), nil)

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_UnwrapKey_KeyAlreadyExists(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{KeyID: keyID}, nil // Key exists
	}

	resp, err := svc.UnwrapKey(systemTenantCtx(), &UnwrapKeyRequest{
		KeyID:             "existing-key",
		WrappedKey:        []byte("data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key already exists")
}

func TestKMSService_UnwrapKey_WrappingKeyNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return nil, errors.New("key not found")
	}

	resp, err := svc.UnwrapKey(systemTenantCtx(), &UnwrapKeyRequest{
		KeyID:             "new-key",
		WrappedKey:        []byte("data"),
		WrappingKeyID:     "missing-wrapper",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping key not found")
}

func TestKMSService_UnwrapKey_WrappingKeyDisabled(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	callCount := 0
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("not found")
		}
		return &KeyMetadata{
			KeyID:  keyID,
			Status: KeyStatusDisabled,
		}, nil
	}

	resp, err := svc.UnwrapKey(systemTenantCtx(), &UnwrapKeyRequest{
		KeyID:             "new-key",
		WrappedKey:        []byte("data"),
		WrappingKeyID:     "disabled-wrapper",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestKMSService_UnwrapKey_DecryptError(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	callCount := 0
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("not found")
		}
		return &KeyMetadata{
			KeyID:  keyID,
			Status: KeyStatusEnabled,
		}, nil
	}

	mockMgr.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		return nil, errors.New("decryption failed")
	}

	resp, err := svc.UnwrapKey(systemTenantCtx(), &UnwrapKeyRequest{
		KeyID:             "new-key",
		WrappedKey:        []byte("encrypted-data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unwrap key material")
}

func TestKMSService_UnwrapKey_InvalidKeySize(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	callCount := 0
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("not found")
		}
		return &KeyMetadata{
			KeyID:  keyID,
			Status: KeyStatusEnabled,
		}, nil
	}

	mockMgr.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		return make([]byte, 15), nil // Wrong size for AES-256-GCM (should be 32)
	}

	resp, err := svc.UnwrapKey(systemTenantCtx(), &UnwrapKeyRequest{
		KeyID:             "new-key",
		WrappedKey:        []byte("encrypted-data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid key material size")
}

func TestKMSService_UnwrapKey_Success(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	callCount := 0
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("not found")
		}
		return &KeyMetadata{
			KeyID:  keyID,
			Status: KeyStatusEnabled,
		}, nil
	}

	mockMgr.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		return make([]byte, 32), nil
	}

	mockMgr.importKeyFn = func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
		return &kms.KeyMetadata{
			KeyID:     keyID,
			Algorithm: algorithm,
			Version:   1,
			State:     kms.KeyStateEnabled,
		}, nil
	}

	resp, err := svc.UnwrapKey(systemTenantCtx(), &UnwrapKeyRequest{
		KeyID:             "new-key",
		Name:              "imported-key",
		WrappedKey:        []byte("encrypted-data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "new-key", resp.KeyID)
	assert.Equal(t, uint64(1), resp.Version)
	assert.Equal(t, KeyAlgAES256GCM, resp.Algorithm)
	assert.Equal(t, KeyStatusEnabled, resp.Status)
}

func TestKMSService_UnwrapKey_WithSourceKeyIDAndVersion(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	callCount := 0
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("not found")
		}
		return &KeyMetadata{KeyID: keyID, Status: KeyStatusEnabled}, nil
	}

	var capturedAAD []byte
	mockMgr.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		capturedAAD = aad
		return make([]byte, 32), nil
	}

	resp, err := svc.UnwrapKey(systemTenantCtx(), &UnwrapKeyRequest{
		KeyID:             "new-key",
		WrappedKey:        []byte("encrypted-data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
		SourceKeyID:       "original-key",
		SourceVersion:     3,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	// Verify AAD includes the source key ID and version
	assert.Contains(t, string(capturedAAD), "original-key")
	assert.Contains(t, string(capturedAAD), "v3")
}

func TestKMSService_UnwrapKey_ImportError(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	callCount := 0
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("not found")
		}
		return &KeyMetadata{KeyID: keyID, Status: KeyStatusEnabled}, nil
	}

	mockMgr.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		return make([]byte, 32), nil
	}

	mockMgr.importKeyFn = func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
		return nil, errors.New("storage full")
	}

	resp, err := svc.UnwrapKey(systemTenantCtx(), &UnwrapKeyRequest{
		KeyID:             "new-key",
		WrappedKey:        []byte("encrypted-data"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to import unwrapped key")
}

func TestKMSService_UnwrapKey_WithAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)
	auditLogger := &mockKMSAuditLogger{}
	svc.auditLogger = auditLogger

	callCount := 0
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("not found")
		}
		return &KeyMetadata{KeyID: keyID, Status: KeyStatusEnabled}, nil
	}

	mockMgr.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		return make([]byte, 32), nil
	}

	resp, err := svc.UnwrapKey(systemTenantCtx(), &UnwrapKeyRequest{
		KeyID:             "new-key",
		Name:              "imported",
		WrappedKey:        []byte("data"),
		WrappingKeyID:     "wrapper",
		WrappingAlgorithm: "AES-KW",
		Algorithm:         "AES-256-GCM",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// logAuditExport and logAuditImport Tests
// =============================================================================

func TestKMSService_LogAuditExport_NilAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)
	svc.auditLogger = nil

	// Should not panic with nil audit logger
	svc.logAuditExport(systemTenantCtx(), "key-id", "AES-256-GCM", "wrapping-key", "AES-KW", true)
}

func TestKMSService_LogAuditImport_NilAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)
	svc.auditLogger = nil

	// Should not panic with nil audit logger
	svc.logAuditImport(systemTenantCtx(), "key-id", "key-name", "AES-256-GCM", true)
}

func TestKMSService_LogAuditExport_WithAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)
	auditLogger := &mockKMSAuditLogger{}
	svc.auditLogger = auditLogger

	svc.logAuditExport(systemTenantCtx(), "exported-key", "AES-256-GCM", "wrapper-key", "AES-KW", true)

	assert.Contains(t, auditLogger.exports, "exported-key")
}

func TestKMSService_LogAuditImport_WithAuditLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)
	auditLogger := &mockKMSAuditLogger{}
	svc.auditLogger = auditLogger

	svc.logAuditImport(systemTenantCtx(), "imported-key", "my-key", "AES-256-GCM", true)
	// Just verify no panic
}

func TestKMSService_LogAuditExport_WithAuditContext(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)
	auditLogger := &mockKMSAuditLogger{}
	svc.auditLogger = auditLogger

	ctx := WithAuditContext(systemTenantCtx(), "admin", "user-123", "10.0.0.1")
	svc.logAuditExport(ctx, "audit-key", "AES-256-GCM", "wrap-key", "AES-KW", true)

	assert.Contains(t, auditLogger.exports, "audit-key")
}

// =============================================================================
// validateKeyMaterialSize Tests
// =============================================================================

func TestKMSService_ValidateKeyMaterialSize(t *testing.T) {
	mockMgr := newMockKMSManager()
	svc := newKMSService(mockMgr, nil)

	tests := []struct {
		name      string
		material  []byte
		algorithm KeyAlgorithm
		wantErr   bool
	}{
		{
			name:      "AES-128-GCM correct size",
			material:  make([]byte, 16),
			algorithm: KeyAlgAES128GCM,
			wantErr:   false,
		},
		{
			name:      "AES-128-GCM wrong size",
			material:  make([]byte, 32),
			algorithm: KeyAlgAES128GCM,
			wantErr:   true,
		},
		{
			name:      "AES-192-GCM correct size",
			material:  make([]byte, 24),
			algorithm: KeyAlgAES192GCM,
			wantErr:   false,
		},
		{
			name:      "AES-192-GCM wrong size",
			material:  make([]byte, 16),
			algorithm: KeyAlgAES192GCM,
			wantErr:   true,
		},
		{
			name:      "AES-256-GCM correct size",
			material:  make([]byte, 32),
			algorithm: KeyAlgAES256GCM,
			wantErr:   false,
		},
		{
			name:      "AES-256-GCM wrong size",
			material:  make([]byte, 16),
			algorithm: KeyAlgAES256GCM,
			wantErr:   true,
		},
		{
			name:      "ChaCha20-Poly1305 correct size",
			material:  make([]byte, 32),
			algorithm: KeyAlgChaCha20Poly1305,
			wantErr:   false,
		},
		{
			name:      "ChaCha20-Poly1305 wrong size",
			material:  make([]byte, 16),
			algorithm: KeyAlgChaCha20Poly1305,
			wantErr:   true,
		},
		{
			name:      "XChaCha20-Poly1305 correct size",
			material:  make([]byte, 32),
			algorithm: KeyAlgXChaCha20Poly1305,
			wantErr:   false,
		},
		{
			name:      "XChaCha20-Poly1305 wrong size",
			material:  make([]byte, 64),
			algorithm: KeyAlgXChaCha20Poly1305,
			wantErr:   true,
		},
		{
			name:      "unknown algorithm skips validation",
			material:  make([]byte, 64),
			algorithm: KeyAlgorithm("RSA-4096"),
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.validateKeyMaterialSize(tt.material, tt.algorithm)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid key material size")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
