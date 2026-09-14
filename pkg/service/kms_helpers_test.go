//go:build !integration

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
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"crypto/elliptic"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

func TestServiceUsageToKMS(t *testing.T) {
	t.Run("converts non-empty slice", func(t *testing.T) {
		input := []KeyUsage{"encrypt", "decrypt", "sign"}
		result := serviceUsageToKMS(input)
		assert.Len(t, result, 3)
		assert.Equal(t, kms.KeyUsage("encrypt"), result[0])
		assert.Equal(t, kms.KeyUsage("decrypt"), result[1])
		assert.Equal(t, kms.KeyUsage("sign"), result[2])
	})

	t.Run("returns nil for empty slice", func(t *testing.T) {
		result := serviceUsageToKMS(nil)
		assert.Nil(t, result)
	})

	t.Run("returns nil for zero-length slice", func(t *testing.T) {
		result := serviceUsageToKMS([]KeyUsage{})
		assert.Nil(t, result)
	})
}

func TestKMSUsageToService(t *testing.T) {
	t.Run("converts non-empty slice", func(t *testing.T) {
		input := []kms.KeyUsage{"encrypt", "decrypt", "verify"}
		result := kmsUsageToService(input)
		assert.Len(t, result, 3)
		assert.Equal(t, KeyUsage("encrypt"), result[0])
		assert.Equal(t, KeyUsage("decrypt"), result[1])
		assert.Equal(t, KeyUsage("verify"), result[2])
	})

	t.Run("returns nil for nil input", func(t *testing.T) {
		result := kmsUsageToService(nil)
		assert.Nil(t, result)
	})

	t.Run("returns nil for zero-length input", func(t *testing.T) {
		result := kmsUsageToService([]kms.KeyUsage{})
		assert.Nil(t, result)
	})
}

func TestVersionStatusKey(t *testing.T) {
	t.Run("generates correct key format", func(t *testing.T) {
		key := versionStatusKey("my-key-123", 5)
		assert.Equal(t, "__sys:kms/version_status/my-key-123/5", string(key))
	})

	t.Run("handles version zero", func(t *testing.T) {
		key := versionStatusKey("test-key", 0)
		assert.Equal(t, "__sys:kms/version_status/test-key/0", string(key))
	})
}

func TestSetKVStore(t *testing.T) {
	t.Run("sets kv store on kms service", func(t *testing.T) {
		svc := &kmsServiceImpl{}
		assert.Nil(t, svc.kvStore)

		// Create a mock kvStore - SetKVStore accepts interfaces.KVStore
		// We just verify it sets the field; a nil value is valid for clearing
		svc.SetKVStore(nil)
		assert.Nil(t, svc.kvStore)
	})
}

func TestExtractKeyMetadata(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-1 * time.Hour)

	t.Run("extracts from kms.KeyMetadata with all fields", func(t *testing.T) {
		kmsMeta := &kms.KeyMetadata{
			KeyID:              "key-1",
			Name:               "test-key",
			Description:        "test description",
			Algorithm:          "AES-256-GCM",
			State:              kms.KeyStateEnabled,
			Version:            3,
			Exportable:         true,
			DeletionProtection: true,
			Created:            now,
			Updated:            now,
			LastUsedAt:         &lastUsed,
			Usage:              []kms.KeyUsage{"encrypt", "decrypt"},
			Tags:               map[string]string{"env": "prod"},
			Owner:              "admin",
		}

		result := extractKeyMetadata("key-1", kmsMeta)
		assert.Equal(t, "key-1", result.KeyID)
		assert.Equal(t, "test-key", result.Name)
		assert.Equal(t, "test description", result.Description)
		assert.Equal(t, KeyAlgorithm("AES-256-GCM"), result.Algorithm)
		assert.Equal(t, KeyStatusEnabled, result.Status)
		assert.Equal(t, uint64(3), result.Version)
		assert.True(t, result.Exportable)
		assert.True(t, result.DeletionProtection)
		assert.Equal(t, now, result.CreatedAt)
		assert.Equal(t, now, result.UpdatedAt)
		assert.NotNil(t, result.LastUsedAt)
		assert.Equal(t, lastUsed, *result.LastUsedAt)
		assert.Len(t, result.Usage, 2)
		assert.Equal(t, KeyUsage("encrypt"), result.Usage[0])
		assert.Equal(t, "prod", result.Tags["env"])
		assert.Equal(t, "admin", result.Owner)
	})

	t.Run("extracts from kms.KeyMetadata without optional fields", func(t *testing.T) {
		kmsMeta := &kms.KeyMetadata{
			KeyID:     "key-2",
			Algorithm: "RSA-2048",
			State:     kms.KeyStateDisabled,
			Version:   1,
			Created:   now,
			Updated:   now,
		}

		result := extractKeyMetadata("key-2", kmsMeta)
		assert.Equal(t, "key-2", result.KeyID)
		assert.Equal(t, KeyStatusDisabled, result.Status)
		assert.Nil(t, result.LastUsedAt)
		assert.Nil(t, result.Tags)
		assert.Empty(t, result.Usage)
	})

	t.Run("extracts from service.KeyMetadata with all fields", func(t *testing.T) {
		svcMeta := &KeyMetadata{
			KeyID:              "key-3",
			Name:               "svc-key",
			Description:        "service key",
			Algorithm:          KeyAlgAES256GCM,
			Status:             KeyStatusEnabled,
			Version:            2,
			Exportable:         false,
			DeletionProtection: true,
			CreatedAt:          now,
			UpdatedAt:          now,
			LastUsedAt:         &lastUsed,
			Usage:              []KeyUsage{"sign", "verify"},
			Tags:               map[string]string{"team": "security"},
			Owner:              "system",
		}

		result := extractKeyMetadata("key-3", svcMeta)
		assert.Equal(t, "key-3", result.KeyID)
		assert.Equal(t, "svc-key", result.Name)
		assert.Equal(t, "service key", result.Description)
		assert.Equal(t, KeyAlgAES256GCM, result.Algorithm)
		assert.Equal(t, KeyStatusEnabled, result.Status)
		assert.Equal(t, uint64(2), result.Version)
		assert.False(t, result.Exportable)
		assert.True(t, result.DeletionProtection)
		assert.NotNil(t, result.LastUsedAt)
		assert.Len(t, result.Usage, 2)
		assert.Equal(t, "security", result.Tags["team"])
		assert.Equal(t, "system", result.Owner)
	})

	t.Run("returns fallback for unrecognized type", func(t *testing.T) {
		result := extractKeyMetadata("key-4", "unexpected-type")
		assert.Equal(t, "key-4", result.KeyID)
		assert.Equal(t, KeyAlgAES256GCM, result.Algorithm)
		assert.Equal(t, KeyStatusEnabled, result.Status)
		assert.Equal(t, uint64(1), result.Version)
	})

	t.Run("returns fallback for nil input", func(t *testing.T) {
		result := extractKeyMetadata("key-5", nil)
		assert.Equal(t, "key-5", result.KeyID)
		assert.Equal(t, KeyAlgAES256GCM, result.Algorithm)
	})
}

func TestSetVersionStatus(t *testing.T) {
	t.Run("stores status in local cache", func(t *testing.T) {
		svc := &kmsServiceImpl{}
		disabledAt := time.Now()
		svc.setVersionStatus("key-1", 1, KeyStatusDisabled, &disabledAt)

		// Verify it's in the sync.Map cache
		val, ok := svc.versionStatus.Load("key-1:1")
		require.True(t, ok)
		vsi, ok := val.(*versionStatusInfo)
		require.True(t, ok)
		assert.Equal(t, KeyStatusDisabled, vsi.Status)
		assert.NotNil(t, vsi.DisabledAt)
	})

	t.Run("stores status without disabledAt", func(t *testing.T) {
		svc := &kmsServiceImpl{}
		svc.setVersionStatus("key-2", 3, KeyStatusEnabled, nil)

		val, ok := svc.versionStatus.Load("key-2:3")
		require.True(t, ok)
		vsi, ok := val.(*versionStatusInfo)
		require.True(t, ok)
		assert.Equal(t, KeyStatusEnabled, vsi.Status)
		assert.Nil(t, vsi.DisabledAt)
	})

	t.Run("persists to kvStore when available", func(t *testing.T) {
		store := &mockKVStoreForKMS{data: make(map[string][]byte)}
		svc := &kmsServiceImpl{kvStore: store}

		disabledAt := time.Now()
		svc.setVersionStatus("key-3", 2, KeyStatusDisabled, &disabledAt)

		// Verify it was persisted
		key := "__sys:kms/version_status/key-3/2"
		data, exists := store.data[key]
		require.True(t, exists)

		var ps persistedVersionStatus
		err := json.Unmarshal(data, &ps)
		require.NoError(t, err)
		assert.Equal(t, string(KeyStatusDisabled), ps.Status)
		assert.NotZero(t, ps.DisabledAt)
	})
}

func TestGetVersionStatus(t *testing.T) {
	t.Run("returns from local cache", func(t *testing.T) {
		svc := &kmsServiceImpl{}
		disabledAt := time.Now()
		svc.versionStatus.Store("key-1:1", &versionStatusInfo{
			Status:     KeyStatusDisabled,
			DisabledAt: &disabledAt,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		status, dt := svc.getVersionStatus(ctx, "key-1", 1)
		assert.Equal(t, KeyStatusDisabled, status)
		assert.NotNil(t, dt)
	})

	t.Run("returns empty when not found", func(t *testing.T) {
		svc := &kmsServiceImpl{}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		status, dt := svc.getVersionStatus(ctx, "nonexistent", 1)
		assert.Empty(t, status)
		assert.Nil(t, dt)
	})

	t.Run("falls back to kvStore", func(t *testing.T) {
		disabledAt := time.Now()
		ps := persistedVersionStatus{
			Status:     string(KeyStatusDisabled),
			DisabledAt: disabledAt.UnixNano(),
		}
		data, err := json.Marshal(&ps)
		require.NoError(t, err)

		store := &mockKVStoreForKMS{data: map[string][]byte{
			"__sys:kms/version_status/key-2/1": data,
		}}
		svc := &kmsServiceImpl{kvStore: store}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		status, dt := svc.getVersionStatus(ctx, "key-2", 1)
		assert.Equal(t, KeyStatusDisabled, status)
		assert.NotNil(t, dt)
	})

	t.Run("adds deadline to context without one", func(t *testing.T) {
		svc := &kmsServiceImpl{}
		// Pass nil context - should not panic
		status, dt := svc.getVersionStatus(nil, "key-nil", 1)
		assert.Empty(t, status)
		assert.Nil(t, dt)
	})

	t.Run("adds deadline to context.Background", func(t *testing.T) {
		svc := &kmsServiceImpl{}
		status, dt := svc.getVersionStatus(context.Background(), "key-bg", 1)
		assert.Empty(t, status)
		assert.Nil(t, dt)
	})
}

// mockKVStoreForKMS is a minimal in-memory KVStore for testing KMS helpers.
type mockKVStoreForKMS struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func (m *mockKVStoreForKMS) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (m *mockKVStoreForKMS) Put(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockKVStoreForKMS) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *mockKVStoreForKMS) Scan(_ context.Context, _ string, _ func(key string, value []byte) error) error {
	return nil
}

func (m *mockKVStoreForKMS) List(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (m *mockKVStoreForKMS) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

func TestAlgorithmToKeySize(t *testing.T) {
	tests := []struct {
		name     string
		algo     KeyAlgorithm
		expected int
	}{
		{
			name:     "AES-128-GCM returns 16 bytes",
			algo:     KeyAlgAES128GCM,
			expected: 16,
		},
		{
			name:     "AES-192-GCM returns 24 bytes",
			algo:     KeyAlgAES192GCM,
			expected: 24,
		},
		{
			name:     "AES-256-GCM returns 32 bytes",
			algo:     KeyAlgAES256GCM,
			expected: 32,
		},
		{
			name:     "ChaCha20-Poly1305 returns 32 bytes",
			algo:     KeyAlgChaCha20Poly1305,
			expected: 32,
		},
		{
			name:     "XChaCha20-Poly1305 returns 32 bytes",
			algo:     KeyAlgXChaCha20Poly1305,
			expected: 32,
		},
		{
			name:     "Unknown algorithm returns default 32 bytes",
			algo:     KeyAlgorithm("UNKNOWN-ALGO"),
			expected: 32,
		},
		{
			name:     "Empty algorithm returns default 32 bytes",
			algo:     KeyAlgorithm(""),
			expected: 32,
		},
		{
			name:     "RSA algorithm returns default 32 bytes",
			algo:     KeyAlgRSA2048,
			expected: 32,
		},
		{
			name:     "ECDSA algorithm returns default 32 bytes",
			algo:     KeyAlgECDSAP256,
			expected: 32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := algorithmToKeySize(tt.algo)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKmsStateToServiceStatus(t *testing.T) {
	tests := []struct {
		name     string
		state    kms.KeyState
		expected KeyStatus
	}{
		{
			name:     "enabled state maps to enabled status",
			state:    kms.KeyStateEnabled,
			expected: KeyStatusEnabled,
		},
		{
			name:     "disabled state maps to disabled status",
			state:    kms.KeyStateDisabled,
			expected: KeyStatusDisabled,
		},
		{
			name:     "pending_deletion state maps to pending_deletion status",
			state:    kms.KeyStatePendingDeletion,
			expected: KeyStatusPendingDeletion,
		},
		{
			name:     "deleted state maps to deleted status",
			state:    kms.KeyStateDeleted,
			expected: KeyStatusDeleted,
		},
		{
			name:     "unknown state defaults to enabled status",
			state:    kms.KeyState("unknown"),
			expected: KeyStatusEnabled,
		},
		{
			name:     "empty state defaults to enabled status",
			state:    kms.KeyState(""),
			expected: KeyStatusEnabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kmsStateToServiceStatus(tt.state)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAttrsToAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		attrs    *xkmssdk.KeyAttributes
		expected string
	}{
		{
			name:     "nil attributes returns AES-256-GCM",
			attrs:    nil,
			expected: string(KeyAlgAES256GCM),
		},
		{
			name: "AES-128-GCM symmetric algorithm",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES128GCM,
			},
			expected: string(KeyAlgAES128GCM),
		},
		{
			name: "AES-192-GCM symmetric algorithm",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES192GCM,
			},
			expected: string(KeyAlgAES192GCM),
		},
		{
			name: "AES-256-GCM symmetric algorithm",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
			},
			expected: string(KeyAlgAES256GCM),
		},
		{
			name: "ChaCha20-Poly1305 symmetric algorithm",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricChaCha20Poly1305,
			},
			expected: string(KeyAlgChaCha20Poly1305),
		},
		{
			name: "XChaCha20-Poly1305 symmetric algorithm",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricXChaCha20Poly1305,
			},
			expected: string(KeyAlgXChaCha20Poly1305),
		},
		{
			name: "RSA 2048 asymmetric algorithm",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  1, // x509.RSA
				RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048},
			},
			expected: string(KeyAlgRSA2048),
		},
		{
			name: "RSA 3072 asymmetric algorithm",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  1, // x509.RSA
				RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 3072},
			},
			expected: string(KeyAlgRSA3072),
		},
		{
			name: "RSA 4096 asymmetric algorithm",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  1, // x509.RSA
				RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 4096},
			},
			expected: string(KeyAlgRSA4096),
		},
		{
			name: "RSA with nil attributes defaults to 2048",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 1, // x509.RSA
			},
			expected: string(KeyAlgRSA2048),
		},
		{
			name: "RSA with unsupported key size defaults to 2048",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  1, // x509.RSA
				RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 1024},
			},
			expected: string(KeyAlgRSA2048),
		},
		{
			name: "ECDSA P-256 curve",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  3, // x509.ECDSA
				ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
			},
			expected: string(KeyAlgECDSAP256),
		},
		{
			name: "ECDSA P-384 curve",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  3, // x509.ECDSA
				ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P384()},
			},
			expected: string(KeyAlgECDSAP384),
		},
		{
			name: "ECDSA P-521 curve",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  3, // x509.ECDSA
				ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P521()},
			},
			expected: string(KeyAlgECDSAP521),
		},
		{
			name: "ECDSA with nil ECCAttributes defaults to P-256",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 3, // x509.ECDSA
			},
			expected: string(KeyAlgECDSAP256),
		},
		{
			name: "Ed25519 algorithm",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 4, // x509.Ed25519
			},
			expected: string(KeyAlgEd25519),
		},
		{
			name:     "Empty attributes defaults to AES-256-GCM",
			attrs:    &xkmssdk.KeyAttributes{},
			expected: string(KeyAlgAES256GCM),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := attrsToAlgorithm(tt.attrs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasExplicitAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		attrs    *xkmssdk.KeyAttributes
		expected bool
	}{
		{
			name:     "nil attributes returns false",
			attrs:    nil,
			expected: false,
		},
		{
			name:     "empty attributes returns false",
			attrs:    &xkmssdk.KeyAttributes{},
			expected: false,
		},
		{
			name: "symmetric algorithm set returns true",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
			},
			expected: true,
		},
		{
			name: "RSA algorithm (1) returns true",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 1, // x509.RSA
			},
			expected: true,
		},
		{
			name: "DSA algorithm (2) returns true",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 2, // x509.DSA
			},
			expected: true,
		},
		{
			name: "ECDSA algorithm (3) returns true",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 3, // x509.ECDSA
			},
			expected: true,
		},
		{
			name: "Ed25519 algorithm (4) returns true",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 4, // x509.Ed25519
			},
			expected: true,
		},
		{
			name: "invalid algorithm (0) returns false",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 0, // Unknown
			},
			expected: false,
		},
		{
			name: "out of range algorithm (5) returns false",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 5, // Beyond Ed25519
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasExplicitAlgorithm(tt.attrs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidDEKAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		algo     KeyAlgorithm
		expected bool
	}{
		{
			name:     "AES-128-GCM is valid DEK algorithm",
			algo:     KeyAlgAES128GCM,
			expected: true,
		},
		{
			name:     "AES-192-GCM is valid DEK algorithm",
			algo:     KeyAlgAES192GCM,
			expected: true,
		},
		{
			name:     "AES-256-GCM is valid DEK algorithm",
			algo:     KeyAlgAES256GCM,
			expected: true,
		},
		{
			name:     "ChaCha20-Poly1305 is valid DEK algorithm",
			algo:     KeyAlgChaCha20Poly1305,
			expected: true,
		},
		{
			name:     "XChaCha20-Poly1305 is valid DEK algorithm",
			algo:     KeyAlgXChaCha20Poly1305,
			expected: true,
		},
		{
			name:     "RSA-2048 is not valid DEK algorithm",
			algo:     KeyAlgRSA2048,
			expected: false,
		},
		{
			name:     "RSA-3072 is not valid DEK algorithm",
			algo:     KeyAlgRSA3072,
			expected: false,
		},
		{
			name:     "RSA-4096 is not valid DEK algorithm",
			algo:     KeyAlgRSA4096,
			expected: false,
		},
		{
			name:     "ECDSA-P256 is not valid DEK algorithm",
			algo:     KeyAlgECDSAP256,
			expected: false,
		},
		{
			name:     "ECDSA-P384 is not valid DEK algorithm",
			algo:     KeyAlgECDSAP384,
			expected: false,
		},
		{
			name:     "ECDSA-P521 is not valid DEK algorithm",
			algo:     KeyAlgECDSAP521,
			expected: false,
		},
		{
			name:     "Ed25519 is not valid DEK algorithm",
			algo:     KeyAlgEd25519,
			expected: false,
		},
		{
			name:     "HMAC-SHA256 is not valid DEK algorithm",
			algo:     KeyAlgHMACSHA256,
			expected: false,
		},
		{
			name:     "Unknown algorithm is not valid DEK algorithm",
			algo:     KeyAlgorithm("UNKNOWN"),
			expected: false,
		},
		{
			name:     "Empty algorithm is not valid DEK algorithm",
			algo:     KeyAlgorithm(""),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidDEKAlgorithm(tt.algo)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKeySizeFromAttributes(t *testing.T) {
	tests := []struct {
		name     string
		attrs    *xkmssdk.KeyAttributes
		expected int
	}{
		{
			name:     "nil attributes returns default 32 bytes",
			attrs:    nil,
			expected: 32,
		},
		{
			name:     "empty attributes returns default 32 bytes",
			attrs:    &xkmssdk.KeyAttributes{},
			expected: 32,
		},
		{
			name: "AES-128-GCM returns 16 bytes",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES128GCM,
			},
			expected: 16,
		},
		{
			name: "AES-192-GCM returns 24 bytes",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES192GCM,
			},
			expected: 24,
		},
		{
			name: "AES-256-GCM returns 32 bytes",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
			},
			expected: 32,
		},
		{
			name: "ChaCha20-Poly1305 returns 32 bytes",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricChaCha20Poly1305,
			},
			expected: 32,
		},
		{
			name: "XChaCha20-Poly1305 returns 32 bytes",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricXChaCha20Poly1305,
			},
			expected: 32,
		},
		{
			name: "unknown symmetric algorithm returns default 32 bytes",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("UNKNOWN"),
			},
			expected: 32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := keySizeFromAttributes(tt.attrs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateKeyMaterialSize(t *testing.T) {
	// Create a minimal KMS service for testing the method
	mockManager := newMockKMSManager()
	svc := newKMSService(mockManager, nil)

	tests := []struct {
		name        string
		material    []byte
		algorithm   KeyAlgorithm
		expectError bool
		errorMsg    string
	}{
		{
			name:        "AES-128-GCM with correct size (16 bytes)",
			material:    make([]byte, 16),
			algorithm:   KeyAlgAES128GCM,
			expectError: false,
		},
		{
			name:        "AES-128-GCM with wrong size (32 bytes)",
			material:    make([]byte, 32),
			algorithm:   KeyAlgAES128GCM,
			expectError: true,
			errorMsg:    "invalid key material size",
		},
		{
			name:        "AES-192-GCM with correct size (24 bytes)",
			material:    make([]byte, 24),
			algorithm:   KeyAlgAES192GCM,
			expectError: false,
		},
		{
			name:        "AES-192-GCM with wrong size (16 bytes)",
			material:    make([]byte, 16),
			algorithm:   KeyAlgAES192GCM,
			expectError: true,
			errorMsg:    "invalid key material size",
		},
		{
			name:        "AES-256-GCM with correct size (32 bytes)",
			material:    make([]byte, 32),
			algorithm:   KeyAlgAES256GCM,
			expectError: false,
		},
		{
			name:        "AES-256-GCM with wrong size (24 bytes)",
			material:    make([]byte, 24),
			algorithm:   KeyAlgAES256GCM,
			expectError: true,
			errorMsg:    "invalid key material size",
		},
		{
			name:        "ChaCha20-Poly1305 with correct size (32 bytes)",
			material:    make([]byte, 32),
			algorithm:   KeyAlgChaCha20Poly1305,
			expectError: false,
		},
		{
			name:        "ChaCha20-Poly1305 with wrong size (16 bytes)",
			material:    make([]byte, 16),
			algorithm:   KeyAlgChaCha20Poly1305,
			expectError: true,
			errorMsg:    "invalid key material size",
		},
		{
			name:        "XChaCha20-Poly1305 with correct size (32 bytes)",
			material:    make([]byte, 32),
			algorithm:   KeyAlgXChaCha20Poly1305,
			expectError: false,
		},
		{
			name:        "XChaCha20-Poly1305 with wrong size (24 bytes)",
			material:    make([]byte, 24),
			algorithm:   KeyAlgXChaCha20Poly1305,
			expectError: true,
			errorMsg:    "invalid key material size",
		},
		{
			name:        "RSA-2048 (asymmetric) - size not validated",
			material:    make([]byte, 256),
			algorithm:   KeyAlgRSA2048,
			expectError: false,
		},
		{
			name:        "ECDSA-P256 (asymmetric) - size not validated",
			material:    make([]byte, 64),
			algorithm:   KeyAlgECDSAP256,
			expectError: false,
		},
		{
			name:        "Ed25519 (asymmetric) - size not validated",
			material:    make([]byte, 32),
			algorithm:   KeyAlgEd25519,
			expectError: false,
		},
		{
			name:        "Unknown algorithm - size not validated",
			material:    make([]byte, 100),
			algorithm:   KeyAlgorithm("UNKNOWN"),
			expectError: false,
		},
		{
			name:        "Empty material for AES-128-GCM",
			material:    []byte{},
			algorithm:   KeyAlgAES128GCM,
			expectError: true,
			errorMsg:    "invalid key material size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.validateKeyMaterialSize(tt.material, tt.algorithm)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
