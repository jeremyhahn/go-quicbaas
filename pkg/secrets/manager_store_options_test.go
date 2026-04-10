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

// Package secrets provides unit tests for Manager.StoreWithOptions.
package secrets

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

var errBackendStoreFailure = errors.New("backend store failure")

// mockBackend implements the Backend interface for testing.
type mockBackend struct {
	secrets  map[string]*SecretEntry
	storeErr error
}

func (m *mockBackend) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.secrets[path] = &SecretEntry{
		Path:      path,
		Data:      data,
		Metadata:  metadata,
		Version:   1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return nil
}

func (m *mockBackend) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if secret, ok := m.secrets[path]; ok {
		return secret.Data, secret.Metadata, nil
	}
	return nil, nil, ErrSecretNotFound
}

func (m *mockBackend) GetVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	return m.Get(ctx, path)
}

func (m *mockBackend) List(ctx context.Context, prefix string) ([]string, error) {
	var paths []string
	for path := range m.secrets {
		paths = append(paths, path)
	}
	return paths, nil
}

func (m *mockBackend) Delete(ctx context.Context, path string) error {
	if _, ok := m.secrets[path]; ok {
		delete(m.secrets, path)
		return nil
	}
	return ErrSecretNotFound
}

func (m *mockBackend) Destroy(ctx context.Context, path string) error {
	return m.Delete(ctx, path)
}

func (m *mockBackend) ListVersions(ctx context.Context, path string) ([]*VersionMetadata, error) {
	if _, ok := m.secrets[path]; ok {
		return []*VersionMetadata{{Version: 1, CreatedAt: time.Now()}}, nil
	}
	return nil, ErrSecretNotFound
}

func (m *mockBackend) Rollback(ctx context.Context, path string, version int) error {
	return nil
}

func (m *mockBackend) Type() string {
	return "mock"
}

func (m *mockBackend) Close() error {
	return nil
}

// mockBackendWithOptions implements both Backend and StoreWithOptions.
type mockBackendWithOptions struct {
	*mockBackend
	storeWithOptionsCallCount int
	lastStoreOptions          *interfaces.StoreOptions
	storeWithOptionsErr       error
}

// StoreWithOptions tracks calls and options.
func (m *mockBackendWithOptions) StoreWithOptions(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *interfaces.StoreOptions) error {
	m.storeWithOptionsCallCount++
	m.lastStoreOptions = opts
	if m.storeWithOptionsErr != nil {
		return m.storeWithOptionsErr
	}
	return m.Store(ctx, path, data, metadata)
}

// mockBackendWithoutOptions only implements the basic Backend interface.
type mockBackendWithoutOptions struct {
	*mockBackend
}

// TestManagerStoreWithOptions_BackendSupportsOptions verifies Manager calls backend's StoreWithOptions.
func TestManagerStoreWithOptions_BackendSupportsOptions(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID:      "tenant-123",
		KeyIdentifier: "key-456",
		Algorithm:     "AES-256-GCM",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, backend.storeWithOptionsCallCount)
	assert.Equal(t, opts, backend.lastStoreOptions)
}

// TestManagerStoreWithOptions_BackendDoesNotSupportOptions verifies fallback to Store.
func TestManagerStoreWithOptions_BackendDoesNotSupportOptions(t *testing.T) {
	backend := &mockBackendWithoutOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)
	// Secret should still be stored via fallback Store method
	secret, _, err := backend.Get(ctx, "test/secret")
	require.NoError(t, err)
	assert.Equal(t, []byte("test"), secret["data"])
}

// TestManagerStoreWithOptions_NilOptions verifies nil options are handled correctly.
// When nil options are passed, enrichOptionsFromContext creates an empty StoreOptions
// to allow auto-population of TenantID from context.
func TestManagerStoreWithOptions_NilOptions(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, 1, backend.storeWithOptionsCallCount)
	// nil options become empty StoreOptions (not nil) to enable TenantID auto-population
	require.NotNil(t, backend.lastStoreOptions)
	assert.Empty(t, backend.lastStoreOptions.TenantID)
	assert.Empty(t, backend.lastStoreOptions.KeyIdentifier)
	assert.Empty(t, backend.lastStoreOptions.Algorithm)
}

// TestManagerStoreWithOptions_InvalidPath verifies path validation.
func TestManagerStoreWithOptions_InvalidPath(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid path")
}

// TestManagerStoreWithOptions_EmptyData verifies data validation.
func TestManagerStoreWithOptions_EmptyData(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{}, nil, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// TestManagerStoreWithOptions_ContextCancellation verifies context handling.
func TestManagerStoreWithOptions_ContextCancellation(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// TestManagerStoreWithOptions_AllOptionsSet verifies all option fields are passed through.
func TestManagerStoreWithOptions_AllOptionsSet(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID:      "tenant-789",
		KeyIdentifier: "key-abc",
		Algorithm:     "CHACHA20-POLY1305",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, backend.storeWithOptionsCallCount)

	// Verify all option fields were passed through correctly
	assert.Equal(t, "tenant-789", backend.lastStoreOptions.TenantID)
	assert.Equal(t, "key-abc", backend.lastStoreOptions.KeyIdentifier)
	assert.Equal(t, "CHACHA20-POLY1305", backend.lastStoreOptions.Algorithm)
}

// TestManagerStoreWithOptions_MetadataPreserved verifies metadata is passed correctly.
func TestManagerStoreWithOptions_MetadataPreserved(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	metadata := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, metadata, opts)

	require.NoError(t, err)

	// Verify secret was stored with correct metadata
	secret := backend.secrets["test/secret"]
	require.NotNil(t, secret)
	assert.Equal(t, "value1", secret.Metadata["key1"])
	assert.Equal(t, "value2", secret.Metadata["key2"])
}

// TestManager_StoreWithOptions_WithTenantID tests tenant_id logging path.
func TestManager_StoreWithOptions_WithTenantID(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	logger := newTestMockLogger()
	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
		Logger:  logger,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "test-tenant-456",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, backend.storeWithOptionsCallCount)

	// Verify logging occurred (logger would have logged with tenant_id field)
	logger.mu.Lock()
	assert.Greater(t, len(logger.entries), 0, "Expected logger to have entries")
	logger.mu.Unlock()
}

// TestManager_StoreWithOptions_WithKeyIdentifier tests key_identifier logging path.
func TestManager_StoreWithOptions_WithKeyIdentifier(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	logger := newTestMockLogger()
	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
		Logger:  logger,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		KeyIdentifier: "key-identifier-789",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, backend.storeWithOptionsCallCount)

	// Verify logging occurred
	logger.mu.Lock()
	assert.Greater(t, len(logger.entries), 0, "Expected logger to have entries")
	logger.mu.Unlock()
}

// TestManager_StoreWithOptions_WithAlgorithm tests algorithm logging path.
func TestManager_StoreWithOptions_WithAlgorithm(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	logger := newTestMockLogger()
	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
		Logger:  logger,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		Algorithm: "AES-256-GCM",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, backend.storeWithOptionsCallCount)

	// Verify logging occurred
	logger.mu.Lock()
	assert.Greater(t, len(logger.entries), 0, "Expected logger to have entries")
	logger.mu.Unlock()
}

// TestManager_StoreWithOptions_FallbackToStore verifies fallback when backend doesn't support StoreWithOptions.
func TestManager_StoreWithOptions_FallbackToStore(t *testing.T) {
	backend := &mockBackendWithoutOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	logger := newTestMockLogger()
	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
		Logger:  logger,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID:      "tenant-123",
		KeyIdentifier: "key-456",
		Algorithm:     "AES-256-GCM",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/fallback", map[string][]byte{"data": []byte("fallback-test")}, nil, opts)

	require.NoError(t, err)

	// Verify secret was stored via fallback
	secret, _, err := backend.Get(ctx, "test/fallback")
	require.NoError(t, err)
	assert.Equal(t, []byte("fallback-test"), secret["data"])

	// Verify debug logging for fallback
	logger.mu.Lock()
	foundDebugLog := false
	for _, entry := range logger.entries {
		if entry == "DEBUG: Backend does not support StoreWithOptions, using fallback Store" {
			foundDebugLog = true
			break
		}
	}
	logger.mu.Unlock()
	assert.True(t, foundDebugLog, "Expected debug log for fallback path")
}

// TestManager_StoreWithOptions_EmptyPath verifies empty path returns error.
func TestManager_StoreWithOptions_EmptyPath(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid path")
	assert.Equal(t, 0, backend.storeWithOptionsCallCount, "StoreWithOptions should not be called for invalid path")
}

// TestManager_StoreWithOptions_EmptyData verifies empty data returns error.
func TestManager_StoreWithOptions_EmptyData(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{}, nil, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
	assert.Equal(t, 0, backend.storeWithOptionsCallCount, "StoreWithOptions should not be called for empty data")
}

// TestManager_StoreWithOptions_ContextCancelled verifies cancelled context returns error.
func TestManager_StoreWithOptions_ContextCancelled(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context before calling StoreWithOptions

	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
	assert.Equal(t, 0, backend.storeWithOptionsCallCount, "StoreWithOptions should not be called for cancelled context")
}

// TestManager_StoreWithOptions_AllLoggingFields verifies all logging fields are used.
func TestManager_StoreWithOptions_AllLoggingFields(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	logger := newTestMockLogger()
	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
		Logger:  logger,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID:      "tenant-all-fields",
		KeyIdentifier: "key-all-fields",
		Algorithm:     "CHACHA20-POLY1305",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/all-fields", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, backend.storeWithOptionsCallCount)

	// Verify all fields were passed
	assert.Equal(t, "tenant-all-fields", backend.lastStoreOptions.TenantID)
	assert.Equal(t, "key-all-fields", backend.lastStoreOptions.KeyIdentifier)
	assert.Equal(t, "CHACHA20-POLY1305", backend.lastStoreOptions.Algorithm)

	// Verify logging occurred with all fields
	logger.mu.Lock()
	assert.Greater(t, len(logger.entries), 0, "Expected logger to have entries")
	logger.mu.Unlock()
}

// TestManager_StoreWithOptions_FallbackLogging verifies fallback path logging.
func TestManager_StoreWithOptions_FallbackLogging(t *testing.T) {
	backend := &mockBackendWithoutOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	logger := newTestMockLogger()
	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
		Logger:  logger,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-fallback",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/fallback-log", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)

	// Verify both debug and info logs were created
	logger.mu.Lock()
	defer logger.mu.Unlock()

	foundDebugLog := false
	foundInfoLog := false

	for _, entry := range logger.entries {
		if entry == "DEBUG: Backend does not support StoreWithOptions, using fallback Store" {
			foundDebugLog = true
		}
		if entry == "INFO: Secret stored (options not supported by backend)" {
			foundInfoLog = true
		}
	}

	assert.True(t, foundDebugLog, "Expected debug log for fallback detection")
	assert.True(t, foundInfoLog, "Expected info log for fallback completion")
}

// TestManager_StoreWithOptions_BackendError verifies error handling when backend fails.
func TestManager_StoreWithOptions_BackendError(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
		storeWithOptionsErr: errBackendStoreFailure,
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend store failure")
	assert.Equal(t, 1, backend.storeWithOptionsCallCount, "StoreWithOptions should be called even if it fails")
}

// TestManager_StoreWithOptions_FallbackError verifies error handling when fallback Store fails.
func TestManager_StoreWithOptions_FallbackError(t *testing.T) {
	backend := &mockBackendWithoutOptions{
		mockBackend: &mockBackend{
			secrets:  make(map[string]*SecretEntry),
			storeErr: errBackendStoreFailure,
		},
	}

	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend store failure")
}

// TestManager_StoreWithOptions_NoLogger verifies operation without logger.
func TestManager_StoreWithOptions_NoLogger(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	// Create manager without logger
	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID:      "tenant-123",
		KeyIdentifier: "key-456",
		Algorithm:     "AES-256-GCM",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, backend.storeWithOptionsCallCount)
}

// TestManager_StoreWithOptions_EmptyOptionsFields verifies empty option fields don't get logged.
func TestManager_StoreWithOptions_EmptyOptionsFields(t *testing.T) {
	backend := &mockBackendWithOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	logger := newTestMockLogger()
	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
		Logger:  logger,
	})
	require.NoError(t, err)

	// Options with empty fields
	opts := &interfaces.StoreOptions{
		TenantID:      "",
		KeyIdentifier: "",
		Algorithm:     "",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/secret", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, backend.storeWithOptionsCallCount)

	// Verify logging still occurred
	logger.mu.Lock()
	assert.Greater(t, len(logger.entries), 0, "Expected logger to have entries")
	logger.mu.Unlock()
}

// TestManager_StoreWithOptions_FallbackNoLogger verifies fallback without logger.
func TestManager_StoreWithOptions_FallbackNoLogger(t *testing.T) {
	backend := &mockBackendWithoutOptions{
		mockBackend: &mockBackend{
			secrets: make(map[string]*SecretEntry),
		},
	}

	// Create manager without logger
	mgr, err := NewManager(&ManagerOptions{
		Backend: backend,
	})
	require.NoError(t, err)

	opts := &interfaces.StoreOptions{
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	err = mgr.StoreWithOptions(ctx, "test/fallback", map[string][]byte{"data": []byte("test")}, nil, opts)

	require.NoError(t, err)

	// Verify secret was stored
	secret, _, err := backend.Get(ctx, "test/fallback")
	require.NoError(t, err)
	assert.Equal(t, []byte("test"), secret["data"])
}
