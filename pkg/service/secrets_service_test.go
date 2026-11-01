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

// Package service provides unit tests for the Secrets service implementation.
package service

import (
	"context"
	"errors"
	stderrors "errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Mock Secrets Manager for Unit Testing
// =============================================================================

// mockSecretsManager implements interfaces.SecretsManager for testing.
type mockSecretsManager struct {
	mu               sync.RWMutex
	secrets          map[string]*mockSecretData
	storeFn          func(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error
	getFn            func(ctx context.Context, path string) (map[string][]byte, map[string]string, error)
	getVersionFn     func(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error)
	listFn           func(ctx context.Context, prefix string) ([]string, error)
	deleteFn         func(ctx context.Context, path string) error
	destroyFn        func(ctx context.Context, path string) error
	listVersionsFn   func(ctx context.Context, path string) ([]*interfaces.SecretVersionMetadata, error)
	rollbackFn       func(ctx context.Context, path string, version int) error
	getBackendTypeFn func() string
	storeErr         error // Error to return on Store calls
	contextErr       error // Error to return when context is canceled
}

type mockSecretData struct {
	path     string
	data     map[string][]byte
	metadata map[string]string
	versions []mockSecretVersion
}

type mockSecretVersion struct {
	version  int
	data     map[string][]byte
	metadata map[string]string
}

func newMockSecretsManager() *mockSecretsManager {
	return &mockSecretsManager{
		secrets: make(map[string]*mockSecretData),
	}
}

func (m *mockSecretsManager) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	_, err := m.StoreVersion(ctx, path, data, metadata)
	return err
}

// StoreVersion records the write and reports the version it assigned, as
// every production manager does.
func (m *mockSecretsManager) StoreVersion(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) (int, error) {
	if m.storeErr != nil {
		return 0, m.storeErr
	}
	if m.storeFn != nil {
		if err := m.storeFn(ctx, path, data, metadata); err != nil {
			return 0, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.secrets[path]
	if !ok {
		existing = &mockSecretData{path: path}
		m.secrets[path] = existing
	} else if len(existing.versions) == 0 {
		// A fixture seeded without history reads as version 1.
		existing.versions = []mockSecretVersion{{version: 1, data: existing.data, metadata: existing.metadata}}
	}
	newVersion := len(existing.versions) + 1
	stored := withMockVersion(metadata, newVersion)
	existing.versions = append(existing.versions, mockSecretVersion{
		version:  newVersion,
		data:     data,
		metadata: stored,
	})
	existing.data = data
	existing.metadata = stored
	return newVersion, nil
}

func (m *mockSecretsManager) StoreWithOptions(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *interfaces.StoreOptions) error {
	// Delegate to Store - options are not needed for tests
	return m.Store(ctx, path, data, metadata)
}

func (m *mockSecretsManager) StoreVersionWithOptions(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, _ *interfaces.StoreOptions) (int, error) {
	return m.StoreVersion(ctx, path, data, metadata)
}

// withMockVersion copies metadata and records the version in it, the way the
// production backends report a version on read.
func withMockVersion(metadata map[string]string, version int) map[string]string {
	stored := make(map[string]string, len(metadata)+1)
	for k, v := range metadata {
		stored[k] = v
	}
	stored[secretVersionKey] = strconv.Itoa(version)
	return stored
}

// reportedMockVersion is the metadata a read reports: a fixture seeded
// straight into the mock, without a version, reads as the given version.
func reportedMockVersion(metadata map[string]string, version int) map[string]string {
	if _, ok := metadata[secretVersionKey]; ok {
		return metadata
	}
	return withMockVersion(metadata, version)
}

func (m *mockSecretsManager) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if m.contextErr != nil {
		return nil, nil, m.contextErr
	}
	if m.getFn != nil {
		return m.getFn(ctx, path)
	}
	// Default behavior: return secret if exists
	m.mu.RLock()
	defer m.mu.RUnlock()
	if secret, ok := m.secrets[path]; ok {
		return secret.data, reportedMockVersion(secret.metadata, max(len(secret.versions), 1)), nil
	}
	return nil, nil, secrets.ErrSecretNotFound
}

func (m *mockSecretsManager) GetVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	if m.getVersionFn != nil {
		return m.getVersionFn(ctx, path, version)
	}
	// Default behavior: return specific version if exists
	m.mu.RLock()
	defer m.mu.RUnlock()
	if secret, ok := m.secrets[path]; ok {
		if version > 0 && version <= len(secret.versions) {
			v := secret.versions[version-1]
			return v.data, reportedMockVersion(v.metadata, v.version), nil
		}
	}
	return nil, nil, secrets.ErrVersionNotFound
}

func (m *mockSecretsManager) List(ctx context.Context, prefix string) ([]string, error) {
	if m.listFn != nil {
		return m.listFn(ctx, prefix)
	}
	// Default behavior: return all secret paths
	m.mu.RLock()
	defer m.mu.RUnlock()
	paths := make([]string, 0, len(m.secrets))
	for path := range m.secrets {
		paths = append(paths, path)
	}
	return paths, nil
}

func (m *mockSecretsManager) Delete(ctx context.Context, path string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, path)
	}
	// Default behavior: soft delete (mark deleted but keep data)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.secrets[path]; ok {
		return nil
	}
	return errors.New("secret not found")
}

func (m *mockSecretsManager) Destroy(ctx context.Context, path string) error {
	if m.destroyFn != nil {
		return m.destroyFn(ctx, path)
	}
	// Default behavior: permanently delete
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.secrets[path]; ok {
		delete(m.secrets, path)
		return nil
	}
	return errors.New("secret not found")
}

func (m *mockSecretsManager) ListVersions(ctx context.Context, path string) ([]*interfaces.SecretVersionMetadata, error) {
	if m.listVersionsFn != nil {
		return m.listVersionsFn(ctx, path)
	}
	// Default behavior: return version list
	m.mu.RLock()
	defer m.mu.RUnlock()
	if secret, ok := m.secrets[path]; ok {
		// A secret seeded straight into the map without a versions slice
		// stands for one written once: every stored secret has at least its
		// first version. A non-nil empty slice is an explicit empty history.
		if secret.versions == nil {
			return []*interfaces.SecretVersionMetadata{{Version: 1, CreatedAt: time.Now()}}, nil
		}
		result := make([]*interfaces.SecretVersionMetadata, len(secret.versions))
		for i := range secret.versions {
			result[i] = &interfaces.SecretVersionMetadata{
				Version:   secret.versions[i].version,
				CreatedAt: time.Now(),
				Deleted:   false,
			}
		}
		return result, nil
	}
	return nil, errors.New("secret not found")
}

func (m *mockSecretsManager) Rollback(ctx context.Context, path string, version int) error {
	if m.rollbackFn != nil {
		return m.rollbackFn(ctx, path, version)
	}
	// Default behavior: rollback to specified version
	m.mu.Lock()
	defer m.mu.Unlock()
	if secret, ok := m.secrets[path]; ok {
		if version > 0 && version <= len(secret.versions) {
			oldVersion := secret.versions[version-1]
			secret.data = oldVersion.data
			secret.metadata = oldVersion.metadata
			return nil
		}
	}
	return errors.New("secret or version not found")
}

func (m *mockSecretsManager) GetBackendType() string {
	if m.getBackendTypeFn != nil {
		return m.getBackendTypeFn()
	}
	return "mock"
}

func (m *mockSecretsManager) Close() error {
	return nil
}

// =============================================================================
// StoreSecret Tests
// =============================================================================

func TestSecretsService_StoreSecret_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &StoreSecretRequest{
		Name:        "test-secret",
		Description: "Test secret for unit tests",
		Data:        []byte("super-secret-data"),
		Metadata:    map[string]string{"env": "test"},
		Tags:        map[string]string{"team": "platform"},
	}

	resp, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-secret", resp.SecretID)
	assert.Equal(t, uint64(1), resp.Version)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestSecretsService_StoreSecret_WithSecretID(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &StoreSecretRequest{
		SecretID:    "my-custom-id",
		Name:        "test-secret",
		Description: "Test secret with custom ID",
		Data:        []byte("secret-data"),
	}

	resp, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "my-custom-id", resp.SecretID)
}

func TestSecretsService_StoreSecret_VersionIncrements(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	secretID := "version-test-secret"

	// Store first version
	req1 := &StoreSecretRequest{
		SecretID:    secretID,
		Name:        "version-test",
		Description: "First version",
		Data:        []byte("version-1-data"),
	}

	resp1, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req1)
	require.NoError(t, err)
	assert.NotNil(t, resp1)
	assert.Equal(t, uint64(1), resp1.Version, "First store should be version 1")

	// Store second version
	req2 := &StoreSecretRequest{
		SecretID:    secretID,
		Name:        "version-test",
		Description: "Second version",
		Data:        []byte("version-2-data"),
	}

	resp2, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req2)
	require.NoError(t, err)
	assert.NotNil(t, resp2)
	assert.Equal(t, uint64(2), resp2.Version, "Second store should be version 2")
	assert.Greater(t, resp2.Version, resp1.Version, "Version should increment")

	// Store third version
	req3 := &StoreSecretRequest{
		SecretID:    secretID,
		Name:        "version-test",
		Description: "Third version",
		Data:        []byte("version-3-data"),
	}

	resp3, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req3)
	require.NoError(t, err)
	assert.NotNil(t, resp3)
	assert.Equal(t, uint64(3), resp3.Version, "Third store should be version 3")
	assert.Greater(t, resp3.Version, resp2.Version, "Version should increment")
}

func TestSecretsService_StoreSecret_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *StoreSecretRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{name: "missing name and id", req: &StoreSecretRequest{Data: []byte("secret-data")}, wantErr: "either name or secret_id must be provided"},
		{name: "missing data", req: &StoreSecretRequest{Name: "test-secret", Data: []byte{}}, wantErr: "secret data is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_StoreSecret_ManagerError(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.storeFn = func(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
		return errors.New("storage backend failure")
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &StoreSecretRequest{
		Name: "test-secret",
		Data: []byte("secret-data"),
	}

	resp, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

// =============================================================================
// GetSecret Tests
// =============================================================================

func TestSecretsService_GetSecret_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	secretData := []byte("my-secret-data")
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		data: map[string][]byte{"data": secretData},
		metadata: map[string]string{
			"description": "Test secret",
			"env":         "production",
			// An explicit non-disabled status reads like no status at all.
			"status": "enabled",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "test-secret-123",
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-secret-123", resp.SecretID)
	assert.Equal(t, secretData, resp.Data)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestSecretsService_GetSecret_SpecificVersion(t *testing.T) {
	mockManager := newMockSecretsManager()
	secretData := []byte("version-2-data")
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		versions: []mockSecretVersion{
			{version: 1, data: map[string][]byte{"data": []byte("version-1-data")}},
			{version: 2, data: map[string][]byte{"data": secretData}},
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "test-secret-123",
		Version:  2,
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, uint64(2), resp.Version)
	assert.Equal(t, secretData, resp.Data)
}

func TestSecretsService_GetSecret_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *GetSecretRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{name: "missing secret id", req: &GetSecretRequest{SecretID: ""}, wantErr: "secret_id is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_GetSecret_NotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "nonexistent-secret",
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

func TestSecretsService_GetSecret_MissingDataField(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path:     "test-secret-123",
		data:     map[string][]byte{}, // Missing "data" field
		metadata: map[string]string{},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "test-secret-123",
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "secret data field not found")
}

func TestSecretsService_GetSecret_DisabledSecret(t *testing.T) {
	mockManager := newMockSecretsManager()
	secretData := []byte("my-secret-data")
	mockManager.secrets["test-secret-disabled"] = &mockSecretData{
		path: "test-secret-disabled",
		data: map[string][]byte{"data": secretData},
		metadata: map[string]string{
			"description": "Disabled secret",
			"status":      "disabled",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "test-secret-disabled",
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "secret is disabled")
	// Check that it returns ErrForbidden
	dErr, ok := err.(*quicbaasErrors.QBError)
	require.True(t, ok, "error should be QBError")
	assert.Equal(t, quicbaasErrors.ErrForbidden, dErr.Code)
}

// =============================================================================
// ListSecrets Tests
// =============================================================================

func TestSecretsService_ListSecrets_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	// Pre-create some secrets
	for i := 0; i < 5; i++ {
		secretID := "test-secret-" + string(rune('a'+i))
		mockManager.secrets[secretID] = &mockSecretData{
			path: secretID,
			data: map[string][]byte{"data": []byte("data")},
			metadata: map[string]string{
				"description": "Test secret",
			},
		}
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretsRequest{
		Limit: 10,
	}

	resp, err := svc.ListSecrets(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 5, resp.TotalCount)
	assert.Len(t, resp.Secrets, 5)
	assert.False(t, resp.HasMore)
}

func TestSecretsService_ListSecrets_NilRequest(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	resp, err := svc.ListSecrets(tenantctx.WithSystem(context.Background()), nil)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestSecretsService_ListSecrets_Empty(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretsRequest{}

	resp, err := svc.ListSecrets(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 0, resp.TotalCount)
	assert.Empty(t, resp.Secrets)
}

func TestSecretsService_ListSecrets_WithPagination(t *testing.T) {
	mockManager := newMockSecretsManager()
	// Create 10 secrets
	for i := 0; i < 10; i++ {
		secretID := "test-secret-" + string(rune('a'+i))
		mockManager.secrets[secretID] = &mockSecretData{
			path:     secretID,
			data:     map[string][]byte{"data": []byte("data")},
			metadata: map[string]string{},
		}
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretsRequest{
		Limit: 5,
	}

	resp, err := svc.ListSecrets(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 10, resp.TotalCount)
	assert.Len(t, resp.Secrets, 5)
	assert.True(t, resp.HasMore)
}

func TestSecretsService_ListSecrets_ManagerError(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, errors.New("database error")
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretsRequest{}

	resp, err := svc.ListSecrets(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// DeleteSecret Tests
// =============================================================================

func TestSecretsService_DeleteSecret_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		data: map[string][]byte{"data": []byte("data")},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &DeleteSecretRequest{
		SecretID:          "test-secret-123",
		PermanentlyDelete: false,
	}

	resp, err := svc.DeleteSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-secret-123", resp.SecretID)
	assert.True(t, resp.Deleted)
	// A soft delete keeps the secret in the store.
	assert.Contains(t, mockManager.secrets, "test-secret-123")
}

func TestSecretsService_DeleteSecret_Permanent(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		data: map[string][]byte{"data": []byte("data")},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &DeleteSecretRequest{
		SecretID:          "test-secret-123",
		PermanentlyDelete: true,
	}

	resp, err := svc.DeleteSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	// Verify permanent deletion
	assert.NotContains(t, mockManager.secrets, "test-secret-123")
}

func TestSecretsService_DeleteSecret_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *DeleteSecretRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{name: "missing secret id", req: &DeleteSecretRequest{SecretID: ""}, wantErr: "secret_id is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.DeleteSecret(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_DeleteSecret_ManagerError(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.deleteFn = func(ctx context.Context, path string) error {
		return errors.New("deletion failed")
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &DeleteSecretRequest{
		SecretID: "test-secret-123",
	}

	resp, err := svc.DeleteSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GetSecretVersion Tests
// =============================================================================

func TestSecretsService_GetSecretVersion_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	secretData := []byte("version-1-data")
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		versions: []mockSecretVersion{
			{version: 1, data: map[string][]byte{"data": secretData}},
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretVersionRequest{
		SecretID: "test-secret-123",
		Version:  1,
	}

	resp, err := svc.GetSecretVersion(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Version)
	assert.Equal(t, "test-secret-123", resp.Version.SecretID)
	assert.Equal(t, uint64(1), resp.Version.Version)
	assert.Equal(t, secretData, resp.Version.Data)
}

func TestSecretsService_GetSecretVersion_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *GetSecretVersionRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{name: "missing secret id", req: &GetSecretVersionRequest{SecretID: "", Version: 1}, wantErr: "secret_id is required"},
		{name: "zero version", req: &GetSecretVersionRequest{SecretID: "test-secret-123", Version: 0}, wantErr: "version must be greater than 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.GetSecretVersion(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_GetSecretVersion_NotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretVersionRequest{
		SecretID: "nonexistent-secret",
		Version:  1,
	}

	resp, err := svc.GetSecretVersion(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

func TestSecretsService_GetSecretVersion_MissingDataField(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		versions: []mockSecretVersion{
			{version: 1, data: map[string][]byte{}}, // Missing "data" field
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretVersionRequest{
		SecretID: "test-secret-123",
		Version:  1,
	}

	resp, err := svc.GetSecretVersion(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "secret data field not found")
}

// =============================================================================
// ListSecretVersions Tests
// =============================================================================

func TestSecretsService_ListSecretVersions_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		versions: []mockSecretVersion{
			{version: 1, data: map[string][]byte{"data": []byte("v1")}},
			{version: 2, data: map[string][]byte{"data": []byte("v2")}},
			{version: 3, data: map[string][]byte{"data": []byte("v3")}},
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretVersionsRequest{
		SecretID: "test-secret-123",
	}

	resp, err := svc.ListSecretVersions(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Versions, 3)
	assert.False(t, resp.HasMore)
}

func TestSecretsService_ListSecretVersions_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *ListSecretVersionsRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{name: "missing secret id", req: &ListSecretVersionsRequest{SecretID: ""}, wantErr: "secret_id is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.ListSecretVersions(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_ListSecretVersions_NotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretVersionsRequest{
		SecretID: "nonexistent-secret",
	}

	resp, err := svc.ListSecretVersions(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// RollbackSecret Tests
// =============================================================================

func TestSecretsService_RollbackSecret_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		versions: []mockSecretVersion{
			{version: 1, data: map[string][]byte{"data": []byte("v1")}},
			{version: 2, data: map[string][]byte{"data": []byte("v2")}},
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &RollbackSecretRequest{
		SecretID:      "test-secret-123",
		TargetVersion: 1,
	}

	resp, err := svc.RollbackSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-secret-123", resp.SecretID)
	assert.Equal(t, uint64(2), resp.NewVersion) // Creates new version
}

func TestSecretsService_RollbackSecret_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *RollbackSecretRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{name: "missing secret id", req: &RollbackSecretRequest{SecretID: "", TargetVersion: 1}, wantErr: "secret_id is required"},
		{name: "zero version", req: &RollbackSecretRequest{SecretID: "test-secret-123", TargetVersion: 0}, wantErr: "target_version must be greater than 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.RollbackSecret(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_RollbackSecret_ManagerError(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.rollbackFn = func(ctx context.Context, path string, version int) error {
		return errors.New("rollback failed")
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &RollbackSecretRequest{
		SecretID:      "test-secret-123",
		TargetVersion: 1,
	}

	resp, err := svc.RollbackSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// RotateSecret Tests
// =============================================================================

func TestSecretsService_RotateSecret_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		data: map[string][]byte{"data": []byte("old-data")},
		metadata: map[string]string{
			"description": "Test secret",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &RotateSecretRequest{
		SecretID: "test-secret-123",
		NewData:  []byte("new-rotated-data"),
		Metadata: map[string]string{"rotated": "true"},
	}

	resp, err := svc.RotateSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-secret-123", resp.SecretID)
	assert.Equal(t, uint64(1), resp.OldVersion)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestSecretsService_RotateSecret_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *RotateSecretRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{name: "missing secret id", req: &RotateSecretRequest{SecretID: "", NewData: []byte("new-data")}, wantErr: "secret_id is required"},
		{name: "missing new data", req: &RotateSecretRequest{SecretID: "test-secret-123", NewData: []byte{}}, wantErr: "new_data is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.RotateSecret(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_RotateSecret_NotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &RotateSecretRequest{
		SecretID: "nonexistent-secret",
		NewData:  []byte("new-data"),
	}

	resp, err := svc.RotateSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

func TestSecretsService_RotateSecret_StoreError(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path:     "test-secret-123",
		data:     map[string][]byte{"data": []byte("old-data")},
		metadata: map[string]string{},
	}
	mockManager.storeFn = func(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
		if path == "test-secret-123" {
			return errors.New("storage error")
		}
		return nil
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &RotateSecretRequest{
		SecretID: "test-secret-123",
		NewData:  []byte("new-data"),
	}

	resp, err := svc.RotateSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// SetRotationPolicy Tests
// =============================================================================

func TestSecretsService_SetRotationPolicy_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path:     "test-secret-123",
		data:     map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	nextRotation := time.Now().Add(30 * 24 * time.Hour)
	req := &SetRotationPolicyRequest{
		SecretID: "test-secret-123",
		Policy: &SecretRotationPolicy{
			Enabled:        true,
			Interval:       30 * 24 * time.Hour,
			NextRotation:   nextRotation,
			RetainVersions: 5,
		},
	}

	resp, err := svc.SetRotationPolicy(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-secret-123", resp.SecretID)
	assert.NotNil(t, resp.Policy)
	assert.True(t, resp.Policy.Enabled)
	assert.Equal(t, 30*24*time.Hour, resp.Policy.Interval)
}

func TestSecretsService_SetRotationPolicy_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *SetRotationPolicyRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{
			name:    "missing secret id",
			req:     &SetRotationPolicyRequest{SecretID: "", Policy: &SecretRotationPolicy{Enabled: true}},
			wantErr: "secret_id is required",
		},
		{name: "nil policy", req: &SetRotationPolicyRequest{SecretID: "test-secret-123", Policy: nil}, wantErr: "policy is required"},
		{
			name:    "enabled with zero interval",
			req:     &SetRotationPolicyRequest{SecretID: "test-secret-123", Policy: &SecretRotationPolicy{Enabled: true, Interval: 0}},
			wantErr: "interval must be positive when rotation is enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.SetRotationPolicy(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_SetRotationPolicy_NotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &SetRotationPolicyRequest{
		SecretID: "nonexistent-secret",
		Policy: &SecretRotationPolicy{
			Enabled:  true,
			Interval: 30 * 24 * time.Hour,
		},
	}

	resp, err := svc.SetRotationPolicy(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

// =============================================================================
// GetRotationPolicy Tests
// =============================================================================

func TestSecretsService_GetRotationPolicy_Success(t *testing.T) {
	mockManager := newMockSecretsManager()
	nextRotation := time.Now().Add(30 * 24 * time.Hour)
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"rotation_enabled":  "true",
			"rotation_interval": "720h",
			"rotation_next":     nextRotation.Format(time.RFC3339),
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetRotationPolicyRequest{
		SecretID: "test-secret-123",
	}

	resp, err := svc.GetRotationPolicy(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-secret-123", resp.SecretID)
	assert.NotNil(t, resp.Policy)
	assert.True(t, resp.Policy.Enabled)
	assert.Equal(t, 720*time.Hour, resp.Policy.Interval)
	assert.WithinDuration(t, nextRotation, resp.Policy.NextRotation, time.Second)
}

func TestSecretsService_GetRotationPolicy_Disabled(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
	}{
		{name: "no rotation metadata", metadata: map[string]string{}},
		{name: "rotation_enabled false", metadata: map[string]string{"rotation_enabled": "false"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockManager := newMockSecretsManager()
			mockManager.secrets["test-secret-123"] = &mockSecretData{
				path:     "test-secret-123",
				data:     map[string][]byte{"data": []byte("data")},
				metadata: tt.metadata,
			}
			svc := newSecretsService(mockManager, newMockLogger())

			req := &GetRotationPolicyRequest{
				SecretID: "test-secret-123",
			}

			resp, err := svc.GetRotationPolicy(tenantctx.WithSystem(context.Background()), req)

			require.NoError(t, err)
			assert.NotNil(t, resp)
			assert.NotNil(t, resp.Policy)
			assert.False(t, resp.Policy.Enabled)
		})
	}
}

func TestSecretsService_GetRotationPolicy_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *GetRotationPolicyRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{name: "missing secret id", req: &GetRotationPolicyRequest{SecretID: ""}, wantErr: "secret_id is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.GetRotationPolicy(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_GetRotationPolicy_NotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetRotationPolicyRequest{
		SecretID: "nonexistent-secret",
	}

	resp, err := svc.GetRotationPolicy(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

// =============================================================================
// Service Construction Tests
// =============================================================================

func TestNewSecretsServiceImpl_NilLogger(t *testing.T) {
	mockManager := newMockSecretsManager()
	svc := newSecretsService(mockManager, nil)

	// Service should still be usable without logger
	req := &StoreSecretRequest{
		Name: "test-secret",
		Data: []byte("secret-data"),
	}

	resp, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestSecretsService_MetricsInitialized(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	// Verify metrics are initialized
	assert.NotNil(t, svc.metrics)
	assert.NotNil(t, svc.metrics.secretsStoredTotal)
	assert.NotNil(t, svc.metrics.secretsRetrievedTotal)
	assert.NotNil(t, svc.metrics.secretsRotatedTotal)
	assert.NotNil(t, svc.metrics.secretsDeletedTotal)
	assert.NotNil(t, svc.metrics.operationDuration)
	assert.NotNil(t, svc.metrics.operationErrors)
}

// =============================================================================
// Edge Cases and Additional Coverage
// =============================================================================

func TestSecretsService_GetSecret_WithMetadataParsing(t *testing.T) {
	mockManager := newMockSecretsManager()
	createdAt := time.Now().Add(-1 * time.Hour)
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path: "test-secret-123",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"created_at":  createdAt.Format(time.RFC3339),
			"custom_key":  "custom_value",
			"tag:team":    "platform",
			"description": "Test secret",
		},
	}
	// The store's recorded write time wins over a caller-supplied
	// "created_at" metadata entry (go-quicbaas#179).
	storedAt := createdAt.Add(-24 * time.Hour)
	mockManager.listVersionsFn = func(context.Context, string) ([]*interfaces.SecretVersionMetadata, error) {
		return []*interfaces.SecretVersionMetadata{{Version: 1, CreatedAt: storedAt}}, nil
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "test-secret-123",
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, storedAt.Equal(resp.CreatedAt), "want stored %s, got %s", storedAt, resp.CreatedAt)
	// Metadata should contain custom_key but not description or tags
	assert.Contains(t, resp.Metadata, "custom_key")
}

func TestSecretsService_ListSecrets_SkipsUnreadableSecrets(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["secret-1"] = &mockSecretData{
		path: "secret-1",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"description": "Good secret",
		},
	}
	mockManager.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		if path == "secret-2" {
			return nil, nil, errors.New("access denied")
		}
		secret := mockManager.secrets[path]
		return secret.data, secret.metadata, nil
	}
	mockManager.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return []string{"secret-1", "secret-2"}, nil
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretsRequest{}

	resp, err := svc.ListSecrets(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Should only include secret-1, skipping secret-2
	assert.Equal(t, 1, len(resp.Secrets))
	assert.Equal(t, 1, resp.TotalCount)
}

func TestSecretsService_SetRotationPolicy_DisabledPolicy(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path:     "test-secret-123",
		data:     map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &SetRotationPolicyRequest{
		SecretID: "test-secret-123",
		Policy: &SecretRotationPolicy{
			Enabled:  false,
			Interval: 0, // Should be OK when disabled
		},
	}

	resp, err := svc.SetRotationPolicy(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Policy.Enabled)
}

func TestSecretsService_UpdateSecretStatus_Transitions(t *testing.T) {
	tests := []struct {
		name      string
		metadata  map[string]string
		status    SecretStatus
		oldStatus SecretStatus
	}{
		{name: "enable a disabled secret", metadata: map[string]string{"status": "disabled"}, status: SecretStatusEnabled, oldStatus: SecretStatusDisabled},
		{name: "disable an enabled secret", metadata: map[string]string{"status": "enabled"}, status: SecretStatusDisabled, oldStatus: SecretStatusEnabled},
		// A secret without a status field defaults to enabled.
		{name: "disable a secret with no status", metadata: map[string]string{}, status: SecretStatusDisabled, oldStatus: SecretStatusEnabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockManager := newMockSecretsManager()
			mockManager.secrets["test-secret-123"] = &mockSecretData{
				path:     "test-secret-123",
				data:     map[string][]byte{"data": []byte("secret-data")},
				metadata: tt.metadata,
			}
			svc := newSecretsService(mockManager, newMockLogger())

			req := &UpdateSecretStatusRequest{
				SecretID: "test-secret-123",
				Status:   tt.status,
			}

			resp, err := svc.UpdateSecretStatus(tenantctx.WithSystem(context.Background()), req)

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, "test-secret-123", resp.SecretID)
			assert.Equal(t, tt.oldStatus, resp.OldStatus)
			assert.Equal(t, tt.status, resp.NewStatus)
			assert.False(t, resp.Timestamp.IsZero())
			assert.Equal(t, string(tt.status), mockManager.secrets["test-secret-123"].metadata["status"])
		})
	}
}

func TestSecretsService_UpdateSecretStatus_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     *UpdateSecretStatusRequest
		wantErr string
	}{
		{name: "nil request", req: nil, wantErr: "request cannot be nil"},
		{name: "missing secret id", req: &UpdateSecretStatusRequest{SecretID: "", Status: SecretStatusEnabled}, wantErr: "secret_id is required"},
		{name: "invalid status", req: &UpdateSecretStatusRequest{SecretID: "test-secret-123", Status: SecretStatus("invalid")}, wantErr: "invalid status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSecretsService(newMockSecretsManager(), newMockLogger())

			resp, err := svc.UpdateSecretStatus(tenantctx.WithSystem(context.Background()), tt.req)

			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSecretsService_UpdateSecretStatus_NotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &UpdateSecretStatusRequest{
		SecretID: "nonexistent-secret",
		Status:   SecretStatusEnabled,
	}

	resp, err := svc.UpdateSecretStatus(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "secret not found")
}

func TestSecretsService_UpdateSecretStatus_StoreError(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path:     "test-secret-123",
		data:     map[string][]byte{"data": []byte("secret-data")},
		metadata: map[string]string{"status": "enabled"},
	}
	mockManager.storeErr = stderrors.New("storage error")
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &UpdateSecretStatusRequest{
		SecretID: "test-secret-123",
		Status:   SecretStatusDisabled,
	}

	resp, err := svc.UpdateSecretStatus(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "update secret status failed")
}

func TestSecretsService_UpdateSecretStatus_ContextCanceled(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret-123"] = &mockSecretData{
		path:     "test-secret-123",
		data:     map[string][]byte{"data": []byte("secret-data")},
		metadata: map[string]string{"status": "enabled"},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	cancel() // Cancel the context immediately

	req := &UpdateSecretStatusRequest{
		SecretID: "test-secret-123",
		Status:   SecretStatusDisabled,
	}

	// Set context error on manager
	mockManager.contextErr = ctx.Err()

	resp, err := svc.UpdateSecretStatus(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSecretsService_StoreSecret_WithAllMetadataFields(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	expiresAt := time.Now().Add(48 * time.Hour)
	req := &StoreSecretRequest{
		SecretID:    "full-metadata-secret",
		Name:        "Full Metadata Test",
		Description: "Secret with all metadata fields populated",
		Data:        []byte("secret-data-123"),
		Metadata: map[string]string{
			"env":         "production",
			"application": "quicbaas",
			"owner":       "platform-team",
		},
		Tags: map[string]string{
			"team":     "platform",
			"critical": "true",
			"region":   "us-west-2",
		},
		ExpiresAt: &expiresAt,
	}

	resp, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "full-metadata-secret", resp.SecretID)

	// Verify metadata was stored correctly
	secret := mockManager.secrets[resp.SecretID]
	assert.NotNil(t, secret)
	assert.Equal(t, "Secret with all metadata fields populated", secret.metadata["description"])
	assert.NotEmpty(t, secret.metadata["expires_at"])
	assert.Equal(t, "production", secret.metadata["env"])
	assert.Equal(t, "platform", secret.metadata["tag:team"])
	assert.Equal(t, "true", secret.metadata["tag:critical"])
}

func TestSecretsService_StoreSecret_WithNilMetrics(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)
	svc.metrics = nil // Simulate nil metrics

	req := &StoreSecretRequest{
		Name: "test-secret",
		Data: []byte("secret-data"),
	}

	resp, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestSecretsService_GetSecret_WithInvalidCreatedAt(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"created_at": "invalid-timestamp",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "test-secret",
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// The metadata entry is ignored: created_at is the stored write time
	assert.False(t, resp.CreatedAt.IsZero())
}

func TestSecretsService_GetSecret_MetadataFiltering(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"description": "Should not appear",
			"expires_at":  "2025-12-31T23:59:59Z",
			"tag:team":    "Should not appear",
			"custom_key":  "Should appear",
			"env":         "Should appear",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "test-secret",
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Contains(t, resp.Metadata, "custom_key")
	assert.Contains(t, resp.Metadata, "env")
	// Reserved keys and tags are not caller metadata.
	assert.NotContains(t, resp.Metadata, "description")
	assert.NotContains(t, resp.Metadata, "expires_at")
	assert.NotContains(t, resp.Metadata, "tag:team")
}

func TestSecretsService_GetSecret_WithVersionNotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		versions: []mockSecretVersion{
			{version: 1, data: map[string][]byte{"data": []byte("v1")}},
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "test-secret",
		Version:  99, // Non-existent version
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

func TestSecretsService_GetSecret_EmptySecretData(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path:     "test-secret",
		data:     map[string][]byte{"data": {}}, // Empty but present
		metadata: map[string]string{},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretRequest{
		SecretID: "test-secret",
	}

	resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.Data)
}

func TestSecretsService_ListSecrets_WithTags(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["secret-1"] = &mockSecretData{
		path: "secret-1",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"tag:env":  "production",
			"tag:team": "platform",
		},
	}
	mockManager.secrets["secret-2"] = &mockSecretData{
		path: "secret-2",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"tag:env": "staging",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretsRequest{}

	resp, err := svc.ListSecrets(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 2, len(resp.Secrets))

	// Verify tags are extracted
	for _, secret := range resp.Secrets {
		if secret.SecretID == "secret-1" {
			assert.Equal(t, "production", secret.Tags["env"])
			assert.Equal(t, "platform", secret.Tags["team"])
		} else if secret.SecretID == "secret-2" {
			assert.Equal(t, "staging", secret.Tags["env"])
		}
	}
}

func TestSecretsService_ListSecrets_ZeroLimit(t *testing.T) {
	mockManager := newMockSecretsManager()
	for i := 0; i < 3; i++ {
		id := "secret-" + string(rune('a'+i))
		mockManager.secrets[id] = &mockSecretData{
			path:     id,
			data:     map[string][]byte{"data": []byte("data")},
			metadata: map[string]string{},
		}
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretsRequest{
		Limit: 0, // Zero means no limit
	}

	resp, err := svc.ListSecrets(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 3, len(resp.Secrets))
	assert.False(t, resp.HasMore)
}

func TestSecretsService_DeleteSecret_NotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &DeleteSecretRequest{
		SecretID:          "nonexistent-secret",
		PermanentlyDelete: false,
	}

	resp, err := svc.DeleteSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSecretsService_DeleteSecret_DestroyNotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &DeleteSecretRequest{
		SecretID:          "nonexistent-secret",
		PermanentlyDelete: true,
	}

	resp, err := svc.DeleteSecret(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSecretsService_GetSecretVersion_WithMetadata(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		versions: []mockSecretVersion{
			{
				version: 1,
				data:    map[string][]byte{"data": []byte("v1-data")},
				metadata: map[string]string{
					"created_by": "user-123",
					"reason":     "initial version",
				},
			},
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetSecretVersionRequest{
		SecretID: "test-secret",
		Version:  1,
	}

	resp, err := svc.GetSecretVersion(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Version)
	assert.Equal(t, "user-123", resp.Version.Metadata["created_by"])
	assert.Equal(t, "initial version", resp.Version.Metadata["reason"])
}

func TestSecretsService_ListSecretVersions_NoVersions(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path:     "test-secret",
		versions: []mockSecretVersion{},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &ListSecretVersionsRequest{
		SecretID: "test-secret",
	}

	resp, err := svc.ListSecretVersions(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.Versions)
}

func TestSecretsService_RotateSecret_PreservesMetadata(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		data: map[string][]byte{"data": []byte("old-data")},
		metadata: map[string]string{
			"env":         "production",
			"description": "API key",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &RotateSecretRequest{
		SecretID: "test-secret",
		NewData:  []byte("new-data"),
		Metadata: map[string]string{
			"rotated_by": "admin",
		},
	}

	resp, err := svc.RotateSecret(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)

	// Verify metadata was merged
	secret := mockManager.secrets["test-secret"]
	assert.Equal(t, "production", secret.metadata["env"])
	assert.Equal(t, "admin", secret.metadata["rotated_by"])
}

func TestSecretsService_SetRotationPolicy_StoreUpdateFails(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path:     "test-secret",
		data:     map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{},
	}
	// Make store always fail to simulate storage error
	mockManager.storeFn = func(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
		return errors.New("storage error")
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &SetRotationPolicyRequest{
		SecretID: "test-secret",
		Policy: &SecretRotationPolicy{
			Enabled:        true,
			Interval:       24 * time.Hour,
			NextRotation:   time.Now().Add(24 * time.Hour),
			RetainVersions: 5,
		},
	}

	resp, err := svc.SetRotationPolicy(tenantctx.WithSystem(context.Background()), req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSecretsService_GetRotationPolicy_InvalidInterval(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"rotation_enabled":  "true",
			"rotation_interval": "invalid-duration",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetRotationPolicyRequest{
		SecretID: "test-secret",
	}

	resp, err := svc.GetRotationPolicy(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Invalid interval is ignored, defaults to zero
	assert.Equal(t, time.Duration(0), resp.Policy.Interval)
}

func TestSecretsService_GetRotationPolicy_InvalidNextRotation(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"rotation_enabled": "true",
			"rotation_next":    "not-a-timestamp",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	req := &GetRotationPolicyRequest{
		SecretID: "test-secret",
	}

	resp, err := svc.GetRotationPolicy(tenantctx.WithSystem(context.Background()), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Invalid timestamp is ignored, defaults to zero time
	assert.True(t, resp.Policy.NextRotation.IsZero())
}

func TestSecretsService_GetSecret_ContextTimeout(t *testing.T) {
	mockManager := newMockSecretsManager()
	// Wait on Done(), never on a sleep past the deadline: ctx.Err() is set by
	// the goroutine context.WithTimeout schedules, so a sleeper can read nil
	// (see TestCryptoService_Sign_ContextTimeout).
	mockManager.getFn = func(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(30 * time.Second):
			// Only reachable if GetSecret stopped propagating the caller's
			// context. Fail loudly instead of returning a generic error that
			// would satisfy a bare require.Error for the wrong reason.
			return nil, nil, errors.New("caller context was not propagated to the secrets manager")
		}
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	ctx, cancel := context.WithTimeout(tenantctx.WithSystem(context.Background()), 10*time.Millisecond)
	defer cancel()

	req := &GetSecretRequest{
		SecretID: "test-secret",
	}

	resp, err := svc.GetSecret(ctx, req)

	// ErrorIs, not Error: the expiry must be what surfaced. A bare Error()
	// also passes on the not-propagated branch above.
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, resp)
}

func TestSecretsService_ConcurrentStoreSecret(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			req := &StoreSecretRequest{
				Name: "secret-" + string(rune('a'+n)),
				Data: []byte("data"),
			}
			_, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), req)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	assert.Equal(t, numGoroutines, len(mockManager.secrets))
}

func TestSecretsService_ConcurrentGetSecret(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		data: map[string][]byte{"data": []byte("data")},
		metadata: map[string]string{
			"env": "test",
		},
	}
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			req := &GetSecretRequest{
				SecretID: "test-secret",
			}
			resp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), req)
			assert.NoError(t, err)
			assert.NotNil(t, resp)
			done <- true
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func TestSecretsService_CompleteLifecycle(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	// 1. Store a secret
	storeReq := &StoreSecretRequest{
		Name: "lifecycle-secret",
		Data: []byte("original-data"),
		Metadata: map[string]string{
			"env": "test",
		},
	}
	storeResp, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), storeReq)
	require.NoError(t, err)
	assert.NotNil(t, storeResp)

	// 2. Get the secret
	getReq := &GetSecretRequest{
		SecretID: storeResp.SecretID,
	}
	getResp, err := svc.GetSecret(tenantctx.WithSystem(context.Background()), getReq)
	require.NoError(t, err)
	assert.Equal(t, []byte("original-data"), getResp.Data)

	// 3. Rotate the secret
	rotateReq := &RotateSecretRequest{
		SecretID: storeResp.SecretID,
		NewData:  []byte("rotated-data"),
	}
	rotateResp, err := svc.RotateSecret(tenantctx.WithSystem(context.Background()), rotateReq)
	require.NoError(t, err)
	assert.NotNil(t, rotateResp)

	// 4. List secrets
	listReq := &ListSecretsRequest{}
	listResp, err := svc.ListSecrets(tenantctx.WithSystem(context.Background()), listReq)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listResp.Secrets), 1)

	// 5. Delete the secret
	deleteReq := &DeleteSecretRequest{
		SecretID:          storeResp.SecretID,
		PermanentlyDelete: true,
	}
	deleteResp, err := svc.DeleteSecret(tenantctx.WithSystem(context.Background()), deleteReq)
	require.NoError(t, err)
	assert.True(t, deleteResp.Deleted)
}

func TestSecretsService_RotationPolicyLifecycle(t *testing.T) {
	mockManager := newMockSecretsManager()
	logger := newMockLogger()
	svc := newSecretsService(mockManager, logger)

	// Store a secret
	storeReq := &StoreSecretRequest{
		Name: "policy-secret",
		Data: []byte("data"),
	}
	storeResp, err := svc.StoreSecret(tenantctx.WithSystem(context.Background()), storeReq)
	require.NoError(t, err)

	// Set rotation policy
	setPolicyReq := &SetRotationPolicyRequest{
		SecretID: storeResp.SecretID,
		Policy: &SecretRotationPolicy{
			Enabled:        true,
			Interval:       24 * time.Hour,
			NextRotation:   time.Now().Add(24 * time.Hour),
			RetainVersions: 5,
		},
	}
	setPolicyResp, err := svc.SetRotationPolicy(tenantctx.WithSystem(context.Background()), setPolicyReq)
	require.NoError(t, err)
	assert.True(t, setPolicyResp.Policy.Enabled)

	// Get rotation policy
	getPolicyReq := &GetRotationPolicyRequest{
		SecretID: storeResp.SecretID,
	}
	getPolicyResp, err := svc.GetRotationPolicy(tenantctx.WithSystem(context.Background()), getPolicyReq)
	require.NoError(t, err)
	assert.True(t, getPolicyResp.Policy.Enabled)
	assert.Equal(t, 24*time.Hour, getPolicyResp.Policy.Interval)
}
