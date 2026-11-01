package iam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// mockKVStore implements interfaces.KVStore for testing.
type mockKVStore struct {
	data map[string][]byte
}

func newMockKVStore() interfaces.KVStore {
	return &mockKVStore{
		data: make(map[string][]byte),
	}
}

func (m *mockKVStore) Put(_ context.Context, key string, value []byte) error {
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	m.data[key] = valueCopy
	return nil
}

func (m *mockKVStore) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := m.data[key]
	if !ok {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "mock.get"}
	}
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	return valueCopy, nil
}

func (m *mockKVStore) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *mockKVStore) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockKVStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

// Compile-time check
var _ interfaces.KVStore = (*mockKVStore)(nil)

func TestBackendStorage_TenantCRUD(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	tenant := &Tenant{
		ID:   "test-tenant",
		Name: "Test Tenant",
	}

	// Create
	err := storage.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create duplicate should fail
	err = storage.CreateTenant(ctx, tenant)
	assert.Error(t, err)

	// Get
	retrieved, err := storage.GetTenant(ctx, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, tenant.ID, retrieved.ID)
	assert.Equal(t, tenant.Name, retrieved.Name)

	// Update
	tenant.Name = "Updated Tenant"
	err = storage.UpdateTenant(ctx, tenant)
	require.NoError(t, err)

	retrieved, err = storage.GetTenant(ctx, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Tenant", retrieved.Name)

	// Exists
	exists, err := storage.TenantExists(ctx, tenant.ID)
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = storage.TenantExists(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)

	// Delete
	err = storage.DeleteTenant(ctx, tenant.ID)
	require.NoError(t, err)

	_, err = storage.GetTenant(ctx, tenant.ID)
	assert.Error(t, err)
}

// BackendStorage user CRUD tests were removed in phase C1-F: user persistence
// is owned by qrdb (qrdbadapter.QRDBUserStorage), not BackendStorage. The
// UserStorage seam is exercised against MemoryStorage and the qrdbadapter suite.

func TestBackendStorage_RoleCRUD(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	role := &Role{
		ID:       "role-1",
		TenantID: "tenant-1",
		Name:     "Test Role",
	}

	// Create
	err := storage.CreateRole(ctx, role)
	require.NoError(t, err)

	// Get
	retrieved, err := storage.GetRole(ctx, role.TenantID, role.ID)
	require.NoError(t, err)
	assert.Equal(t, role.ID, retrieved.ID)

	// Update
	role.Name = "Updated Role"
	err = storage.UpdateRole(ctx, role)
	require.NoError(t, err)

	retrieved, err = storage.GetRole(ctx, role.TenantID, role.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Role", retrieved.Name)

	// Delete
	err = storage.DeleteRole(ctx, role.TenantID, role.ID)
	require.NoError(t, err)

	_, err = storage.GetRole(ctx, role.TenantID, role.ID)
	assert.Error(t, err)
}

func TestBackendStorage_AppCRUD(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	app := &Application{
		ClientID:     "app-client-123",
		ClientSecret: "hashed-secret",
		Name:         "Test App",
		TenantID:     "tenant-1",
		RedirectURIs: []string{"https://example.com/callback"},
	}

	// Create
	err := storage.CreateApp(ctx, app)
	require.NoError(t, err)

	// Get
	retrieved, err := storage.GetApp(ctx, app.ClientID)
	require.NoError(t, err)
	assert.Equal(t, app.ClientID, retrieved.ClientID)
	assert.Equal(t, app.Name, retrieved.Name)

	// GetByTenant
	retrieved, err = storage.GetAppByTenant(ctx, app.TenantID, app.ClientID)
	require.NoError(t, err)
	assert.Equal(t, app.ClientID, retrieved.ClientID)

	// Update
	app.Name = "Updated App"
	err = storage.UpdateApp(ctx, app)
	require.NoError(t, err)

	// Delete
	err = storage.DeleteApp(ctx, app.ClientID)
	require.NoError(t, err)

	_, err = storage.GetApp(ctx, app.ClientID)
	assert.Error(t, err)
}

func TestBackendStorage_TokenOperations(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	token := &Token{
		JTI:       "token-jti-123",
		Subject:   "user-1",
		TenantID:  "tenant-1",
		TokenType: TokenTypeAccess,
		Expires:   time.Now().Add(time.Hour),
		Issued:    time.Now(),
	}

	// Store
	err := storage.StoreToken(ctx, token)
	require.NoError(t, err)

	// Get
	retrieved, err := storage.GetToken(ctx, token.JTI)
	require.NoError(t, err)
	assert.Equal(t, token.JTI, retrieved.JTI)
	assert.Equal(t, token.Subject, retrieved.Subject)

	// Revoke
	err = storage.RevokeToken(ctx, token.JTI)
	require.NoError(t, err)

	// Check revoked
	revoked, err := storage.IsTokenRevoked(ctx, token.JTI)
	require.NoError(t, err)
	assert.True(t, revoked)
}

func TestBackendStorage_AuthCodeOperations(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	authCode := &AuthCodeData{
		Code:        "auth-code-123",
		ClientID:    "client-1",
		UserID:      10001,
		RedirectURI: "https://example.com/callback",
		Scope:       []string{"openid", "profile"},
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}

	// Store
	err := storage.StoreAuthCode(ctx, authCode)
	require.NoError(t, err)

	// Get
	retrieved, err := storage.GetAuthCode(ctx, authCode.Code)
	require.NoError(t, err)
	assert.Equal(t, authCode.Code, retrieved.Code)
	assert.Equal(t, authCode.ClientID, retrieved.ClientID)
	assert.Equal(t, authCode.UserID, retrieved.UserID)
	assert.Equal(t, authCode.Scope, retrieved.Scope)

	// Delete
	err = storage.DeleteAuthCode(ctx, authCode.Code)
	require.NoError(t, err)

	_, err = storage.GetAuthCode(ctx, authCode.Code)
	assert.Error(t, err)
}

func TestBackendStorage_PolicyCRUD(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	policy := &Policy{
		APIVersion: "quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:        "test-policy",
			Tenant:      "tenant-1",
			Description: "Test policy",
		},
		Spec: PolicySpec{
			Subjects: SubjectSelector{
				Users: []string{"user-1"},
			},
		},
	}

	// Create
	err := storage.CreatePolicy(ctx, "tenant-1", policy)
	require.NoError(t, err)

	// Get
	retrieved, err := storage.GetPolicy(ctx, "tenant-1", policy.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, policy.Metadata.Name, retrieved.Metadata.Name)
	assert.Equal(t, policy.Metadata.Description, retrieved.Metadata.Description)

	// Update
	policy.Metadata.Description = "Updated description"
	err = storage.UpdatePolicy(ctx, "tenant-1", policy)
	require.NoError(t, err)

	retrieved, err = storage.GetPolicy(ctx, "tenant-1", policy.Metadata.Name)
	require.NoError(t, err)
	assert.Equal(t, "Updated description", retrieved.Metadata.Description)

	// Delete
	err = storage.DeletePolicy(ctx, "tenant-1", policy.Metadata.Name)
	require.NoError(t, err)

	_, err = storage.GetPolicy(ctx, "tenant-1", policy.Metadata.Name)
	assert.Error(t, err)
}

func TestBackendStorage_TenantSettings(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	// Create tenant first
	tenant := &Tenant{
		ID:   "tenant-settings-test",
		Name: "Test Tenant",
	}
	err := storage.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Get settings (should return empty)
	settings, err := storage.GetSettings(ctx, tenant.ID)
	require.NoError(t, err)
	assert.NotNil(t, settings)

	// Update settings
	settings.SessionTimeout = time.Hour
	settings.MFARequired = true
	err = storage.UpdateSettings(ctx, tenant.ID, settings)
	require.NoError(t, err)

	// Get updated settings
	updated, err := storage.GetSettings(ctx, tenant.ID)
	require.NoError(t, err)
	assert.Equal(t, time.Hour, updated.SessionTimeout)
	assert.True(t, updated.MFARequired)
}

func TestBackendStorage_QuotaOperations(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	tenantID := "quota-test-tenant"

	// Get empty quota
	quota, err := storage.GetQuota(ctx, tenantID)
	require.NoError(t, err)
	assert.NotNil(t, quota)

	// Update BaaS-layer IAM quotas (users/roles are owned by qrdb TenantServicer)
	quota.MaxPolicies = 50
	err = storage.UpdateQuota(ctx, tenantID, quota)
	require.NoError(t, err)

	// Increment policies usage (BaaS-layer quota)
	err = storage.IncrementQuotaUsage(ctx, tenantID, QuotaResourcePolicies, 5)
	require.NoError(t, err)

	updated, err := storage.GetQuota(ctx, tenantID)
	require.NoError(t, err)
	assert.Equal(t, int64(5), updated.CurrentPolicies)

	// Decrement policies usage
	err = storage.DecrementQuotaUsage(ctx, tenantID, QuotaResourcePolicies, 2)
	require.NoError(t, err)

	updated, err = storage.GetQuota(ctx, tenantID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), updated.CurrentPolicies)
}

func TestBackendStorage_NotFoundErrors(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())

	tests := []struct {
		name string
		call func() error
	}{
		{"GetTenant", func() error { _, err := storage.GetTenant(ctx, "missing"); return err }},
		{"UpdateTenant", func() error { return storage.UpdateTenant(ctx, &Tenant{ID: "missing", Name: "Test"}) }},
		{"DeleteTenant", func() error { return storage.DeleteTenant(ctx, "missing") }},
		{"GetRole", func() error { _, err := storage.GetRole(ctx, "tenant", "missing"); return err }},
		{"UpdateRole", func() error {
			return storage.UpdateRole(ctx, &Role{ID: "missing", TenantID: "tenant", Name: "Missing"})
		}},
		{"DeleteRole", func() error { return storage.DeleteRole(ctx, "tenant", "missing") }},
		{"GetApp", func() error { _, err := storage.GetApp(ctx, "missing"); return err }},
		{"GetAppByTenant", func() error { _, err := storage.GetAppByTenant(ctx, "tenant", "missing"); return err }},
		{"UpdateApp", func() error {
			return storage.UpdateApp(ctx, &Application{ClientID: "missing", TenantID: "tenant", Name: "Missing"})
		}},
		{"DeleteApp", func() error { return storage.DeleteApp(ctx, "missing") }},
		{"ValidateClientSecret", func() error { _, err := storage.ValidateClientSecret(ctx, "missing", "secret"); return err }},
		{"GetPolicy", func() error { _, err := storage.GetPolicy(ctx, "tenant", "missing"); return err }},
		{"UpdatePolicy", func() error {
			return storage.UpdatePolicy(ctx, "tenant", &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "missing", Tenant: "tenant"},
				Spec: PolicySpec{
					Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
				},
			})
		}},
		{"DeletePolicy", func() error { return storage.DeletePolicy(ctx, "tenant", "missing") }},
		{"GetToken", func() error { _, err := storage.GetToken(ctx, "missing"); return err }},
		{"GetTokenByValue", func() error { _, err := storage.GetTokenByValue(ctx, "missing"); return err }},
		{"RevokeToken", func() error { return storage.RevokeToken(ctx, "missing") }},
		{"GetAuthCode", func() error { _, err := storage.GetAuthCode(ctx, "missing"); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.True(t, dberrors.IsNotFound(err), "want not-found, got %v", err)
		})
	}
}

// TestBackendStorage_ListOperations (user listing) was removed in phase C1-F:
// BackendStorage no longer persists users. User listing pagination is covered by
// the MemoryStorage suite and the qrdbadapter suite.

// TestBackendStorage_KeyPrefixes ensures keys are properly formatted
func TestBackendStorage_KeyPrefixes(t *testing.T) {
	// Test tenant key
	key := tenantKey("test-tenant")
	assert.Equal(t, "__iam:tenant:test-tenant", key)

	// Test role key
	key = roleKey("tenant-1", "role-1")
	assert.Equal(t, "__iam:tenant:tenant-1:role:role-1", key)

	// Test policy key
	key = policyKey("tenant-1", "policy-1")
	assert.Equal(t, "__iam:tenant:tenant-1:policy:policy-1", key)

	// Test app key (tenant-scoped) + legacy variant
	key = appKey("tenant-1", "client-123")
	assert.Equal(t, "__iam:app:tenant-1:client-123", key)
	assert.Equal(t, "__iam:app:client-123", legacyAppKey("client-123"))

	// Test token key (tenant-scoped) + legacy variant
	key = tokenKey("tenant-1", "jti-123")
	assert.Equal(t, "__iam:token:tenant-1:jti-123", key)
	assert.Equal(t, "__iam:token:jti-123", legacyTokenKey("jti-123"))

	// Test session key
	key = sessionKey("tenant-1", "session-123")
	assert.Equal(t, "__iam:tenant:tenant-1:session:session-123", key)
}

func TestBackendStorage_DeleteTenantCascade(t *testing.T) {
	tenantID := "cascade-tenant"
	ctx := tenantctx.WithTenant(context.Background(), tenantID)
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	// Create tenant
	tenant := &Tenant{
		ID:   tenantID,
		Name: "Cascade Test Tenant",
	}
	require.NoError(t, storage.CreateTenant(ctx, tenant))

	// Create roles
	role := &Role{
		ID:          "role-1",
		TenantID:    tenantID,
		Name:        "Test Role",
		Permissions: []string{"read", "write"},
	}
	require.NoError(t, storage.CreateRole(ctx, role))

	// Create policy
	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:        "policy-1",
			Description: "Test policy",
		},
		Spec: PolicySpec{},
	}
	require.NoError(t, storage.CreatePolicy(ctx, tenantID, policy))

	// Create app
	app := &Application{
		ClientID: "app-1",
		TenantID: tenantID,
		Name:     "Test App",
	}
	require.NoError(t, storage.CreateApp(ctx, app))

	// Verify all entities exist
	_, err := storage.GetTenant(ctx, tenantID)
	require.NoError(t, err, "tenant should exist")

	_, err = storage.GetRole(ctx, tenantID, role.ID)
	require.NoError(t, err, "role should exist")

	_, err = storage.GetPolicy(ctx, tenantID, policy.Metadata.Name)
	require.NoError(t, err, "policy should exist")

	_, err = storage.GetApp(ctx, app.ClientID)
	require.NoError(t, err, "app should exist")

	// Delete tenant (cascade delete)
	err = storage.DeleteTenant(ctx, tenantID)
	require.NoError(t, err)

	// Verify all entities are deleted
	_, err = storage.GetTenant(ctx, tenantID)
	assert.Error(t, err, "tenant should be deleted")

	_, err = storage.GetRole(ctx, tenantID, role.ID)
	assert.Error(t, err, "role should be deleted")

	_, err = storage.GetPolicy(ctx, tenantID, policy.Metadata.Name)
	assert.Error(t, err, "policy should be deleted")

	_, err = storage.GetApp(ctx, app.ClientID)
	assert.Error(t, err, "app should be deleted")
}

// Benchmark tests. The CreateUser/GetUser benchmarks were removed in phase
// C1-F (BackendStorage no longer persists users; qrdb owns the user record).
func BenchmarkBackendStorage_JSONMarshal(b *testing.B) {
	user := &User{
		ID:       40050,
		TenantID: "tenant-1",
		Email:    "bench@example.com",
		Name:     "Benchmark User",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(user)
	}
}

// =============================================================================
// BatchProcessor - processPolicies additional edge cases
// =============================================================================

func TestBatchProcessor_ProcessPolicies_DryRunValidation(t *testing.T) {
	tests := []struct {
		name string
		opts BatchOptions
	}{
		{name: "dry run", opts: BatchOptions{DryRun: true}},
		{name: "validate only", opts: BatchOptions{ValidateOnly: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &finalOpStorage{
				mockBatchStorage: newMockBatchStorage(),
				done:             make(chan BatchOperation, 1),
			}
			tenantMgr := newMockTenantManager()
			bp := NewIAMBatchProcessor(storage, tenantMgr)

			ctx := context.Background()
			tenantID := "test-tenant"
			items := []PolicyImportItem{
				{
					Policy: &Policy{
						APIVersion: "v1",
						Kind:       "Policy",
						Metadata:   PolicyMetadata{Name: "no-write-test", Tenant: tenantID},
						Spec: PolicySpec{
							Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
						},
					},
				},
			}

			_, err := bp.ImportPolicies(ctx, tenantID, items, tt.opts)
			require.NoError(t, err)

			var final BatchOperation
			select {
			case final = <-storage.done:
			case <-time.After(10 * time.Second):
				t.Fatal("timed out waiting for the import to finish")
			}

			assert.Equal(t, BatchStatusCompleted, final.Status)
			assert.Equal(t, 1, final.Progress.Succeeded)
			_, err = tenantMgr.GetPolicy(ctx, tenantID, "no-write-test")
			assert.ErrorIs(t, err, ErrPolicyNotFound, "a no-write import must not create the policy")
		})
	}
}

// finalOpStorage hands the test a copy of the operation once it is
// finalized, so the test synchronizes on completion instead of sleeping.
type finalOpStorage struct {
	*mockBatchStorage
	done chan BatchOperation
}

func (s *finalOpStorage) UpdateOperation(ctx context.Context, op *BatchOperation) error {
	if op.CompletedAt != nil {
		s.done <- *op
	}
	return s.mockBatchStorage.UpdateOperation(ctx, op)
}

func TestMemoryConsentStorage_DeleteConsent_ByPurpose(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	// Create multiple consents for different purposes
	_, err := storage.CreateConsent(ctx, CreateConsentOptions{
		TenantID: "tenant-1",
		UserID:   10001,
		Purpose:  "marketing",
		Status:   ConsentStatusGranted,
		Version:  "1.0",
	})
	if err != nil {
		t.Fatalf("CreateConsent failed: %v", err)
	}

	consent2, err := storage.CreateConsent(ctx, CreateConsentOptions{
		TenantID: "tenant-1",
		UserID:   10001,
		Purpose:  "analytics",
		Status:   ConsentStatusGranted,
		Version:  "1.0",
	})
	if err != nil {
		t.Fatalf("CreateConsent failed: %v", err)
	}

	// Delete just the analytics consent
	err = storage.DeleteConsent(ctx, consent2.ID)
	if err != nil {
		t.Fatalf("DeleteConsent failed: %v", err)
	}

	// Marketing consent should still exist
	consents, err := storage.ListConsents(ctx, ListConsentsOptions{
		TenantID: "tenant-1",
		UserID:   10001,
	})
	if err != nil {
		t.Fatalf("ListConsents failed: %v", err)
	}

	if len(consents) != 1 {
		t.Errorf("expected 1 consent, got %d", len(consents))
	}
	if consents[0].Purpose != "marketing" {
		t.Errorf("expected marketing consent, got %s", consents[0].Purpose)
	}
}

// =============================================================================
// UpdateExportRequest coverage - additional paths
// =============================================================================

func TestMemoryDataExportStorage_UpdateExportRequest_MultipleUpdates(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	// Create an export request
	request, err := storage.CreateExportRequest(ctx, CreateDataExportOptions{
		TenantID:    "tenant-1",
		UserID:      10001,
		RequestedBy: 1,
		Format:      DataExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("CreateExportRequest failed: %v", err)
	}

	// First update - processing
	request.Status = DataExportStatusProcessing
	err = storage.UpdateExportRequest(ctx, request)
	if err != nil {
		t.Fatalf("UpdateExportRequest (processing) failed: %v", err)
	}

	// Second update - completed
	now := time.Now()
	request.Status = DataExportStatusCompleted
	request.CompletedAt = &now
	request.DownloadURL = "https://example.com/export.zip"
	err = storage.UpdateExportRequest(ctx, request)
	if err != nil {
		t.Fatalf("UpdateExportRequest (completed) failed: %v", err)
	}

	// Verify final state
	updated, _ := storage.GetExportRequest(ctx, request.ID)
	if updated.Status != DataExportStatusCompleted {
		t.Errorf("expected status completed, got %s", updated.Status)
	}
}

// =============================================================================
// UpdateDeletionRequest coverage - additional paths
// =============================================================================

func TestMemoryDeletionStorage_UpdateDeletionRequest_MultipleUpdates(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	// Create a deletion request
	request, err := storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		TenantID:    "tenant-1",
		UserID:      10001,
		RequestedBy: 1,
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest failed: %v", err)
	}

	// First update - processing
	request.Status = DeletionStatusProcessing
	err = storage.UpdateDeletionRequest(ctx, request)
	if err != nil {
		t.Fatalf("UpdateDeletionRequest (processing) failed: %v", err)
	}

	// Second update - completed
	now := time.Now()
	request.Status = DeletionStatusCompleted
	request.CompletedAt = &now
	err = storage.UpdateDeletionRequest(ctx, request)
	if err != nil {
		t.Fatalf("UpdateDeletionRequest (completed) failed: %v", err)
	}

	// Verify final state
	updated, _ := storage.GetDeletionRequest(ctx, request.ID)
	if updated.Status != DeletionStatusCompleted {
		t.Errorf("expected status completed, got %s", updated.Status)
	}
}

// =============================================================================
// copyDataDeletionRequest coverage
// =============================================================================

func TestMemoryDeletionStorage_ReturnsCopies(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	// Create a deletion request
	request, err := storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		TenantID:    "tenant-1",
		UserID:      10001,
		RequestedBy: 1,
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest failed: %v", err)
	}

	// Get it
	request1, _ := storage.GetDeletionRequest(ctx, request.ID)
	originalStatus := request1.Status

	// Modify the returned copy
	request1.Status = DeletionStatusCompleted

	// Get again - should have original status
	request2, _ := storage.GetDeletionRequest(ctx, request.ID)
	if request2.Status != originalStatus {
		t.Error("storage did not return a copy - modifications affected stored value")
	}
}

// =============================================================================
// matchesExportFilter and matchesDeletionFilter edge cases
// =============================================================================

func TestMemoryDataExportStorage_ListExportRequests_PaginatedFilter(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	// Create multiple export requests
	for i := 0; i < 5; i++ {
		_, err := storage.CreateExportRequest(ctx, CreateDataExportOptions{
			TenantID:    "tenant-1",
			UserID:      10001,
			RequestedBy: 1,
			Format:      DataExportFormatJSON,
		})
		if err != nil {
			t.Fatalf("CreateExportRequest failed: %v", err)
		}
	}

	// List with both status and user filters
	requests, err := storage.ListExportRequests(ctx, ListExportRequestsOptions{
		TenantID: "tenant-1",
		UserID:   10001,
		Status:   DataExportStatusPending,
	})
	if err != nil {
		t.Fatalf("ListExportRequests failed: %v", err)
	}

	if len(requests) != 5 {
		t.Errorf("expected 5 requests, got %d", len(requests))
	}
}

func TestMemoryDeletionStorage_ListDeletionRequests_PaginatedFilter(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	// Create multiple deletion requests
	for i := 0; i < 5; i++ {
		_, err := storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
			TenantID:    "tenant-1",
			UserID:      10001,
			RequestedBy: 1,
		})
		if err != nil {
			t.Fatalf("CreateDeletionRequest failed: %v", err)
		}
	}

	// List with both status and user filters
	requests, err := storage.ListDeletionRequests(ctx, ListDeletionRequestsOptions{
		TenantID: "tenant-1",
		UserID:   10001,
		Status:   DeletionStatusPending,
	})
	if err != nil {
		t.Fatalf("ListDeletionRequests failed: %v", err)
	}

	if len(requests) != 5 {
		t.Errorf("expected 5 requests, got %d", len(requests))
	}
}

// =============================================================================
// Federation tests using proper setup pattern
// =============================================================================

func TestFederationManager_UpdateProvider_ChangeClientSecretBoost(t *testing.T) {
	providerStorage := NewMemoryProviderStorage()
	linkStorage := NewMemoryFederationStorage()
	stateStorage := NewMemoryStateStorage()

	userStorage := newMockUserStorage()
	roleStorage := newMockRoleStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: userStorage,
		RoleStorage: roleStorage,
	})

	manager, err := NewFederationManager(
		WithProviderStorage(providerStorage),
		WithLinkStorage(linkStorage),
		WithStateStorage(stateStorage),
		WithFederationIdentityStore(identityStore),
	)
	if err != nil {
		t.Fatalf("NewFederationManager failed: %v", err)
	}

	ctx := context.Background()

	// Create a provider first
	provider := &ExternalProvider{
		TenantID:      "tenant-1",
		Name:          "update-test",
		Type:          ProviderTypeOIDC,
		Enabled:       true,
		ClientID:      "client-id",
		ClientSecret:  "old-secret",
		AuthURL:       "https://auth.example.com/authorize",
		TokenURL:      "https://auth.example.com/token",
		IssuerURL:     "https://auth.example.com",
		AutoProvision: true,
	}

	err = manager.RegisterProvider(ctx, provider)
	if err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}

	// Get the provider to get its ID
	saved, err := manager.GetProviderByName(ctx, "tenant-1", "update-test")
	if err != nil {
		t.Fatalf("GetProviderByName failed: %v", err)
	}

	// Update client secret
	saved.ClientSecret = "new-secret"
	err = manager.UpdateProvider(ctx, saved)
	if err != nil {
		t.Fatalf("UpdateProvider failed: %v", err)
	}

	// Verify update
	updated, err := manager.GetProvider(ctx, "tenant-1", saved.ID)
	if err != nil {
		t.Fatalf("GetProvider failed: %v", err)
	}

	if updated.ClientSecret != "new-secret" {
		t.Error("expected client secret to be updated")
	}
}

func TestFederationManager_GetProviderByName_SuccessBoost(t *testing.T) {
	providerStorage := NewMemoryProviderStorage()
	linkStorage := NewMemoryFederationStorage()
	stateStorage := NewMemoryStateStorage()

	userStorage := newMockUserStorage()
	roleStorage := newMockRoleStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: userStorage,
		RoleStorage: roleStorage,
	})

	manager, err := NewFederationManager(
		WithProviderStorage(providerStorage),
		WithLinkStorage(linkStorage),
		WithStateStorage(stateStorage),
		WithFederationIdentityStore(identityStore),
	)
	if err != nil {
		t.Fatalf("NewFederationManager failed: %v", err)
	}

	ctx := context.Background()

	// Create a provider
	provider := &ExternalProvider{
		TenantID:      "tenant-1",
		Name:          "google-oauth",
		Type:          ProviderTypeOIDC,
		Enabled:       true,
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		AuthURL:       "https://accounts.google.com/o/oauth2/auth",
		TokenURL:      "https://oauth2.googleapis.com/token",
		IssuerURL:     "https://accounts.google.com",
		AutoProvision: true,
	}

	err = manager.RegisterProvider(ctx, provider)
	if err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}

	// Get by name
	found, err := manager.GetProviderByName(ctx, "tenant-1", "google-oauth")
	if err != nil {
		t.Fatalf("GetProviderByName failed: %v", err)
	}

	if found.Name != "google-oauth" {
		t.Errorf("expected provider name google-oauth, got %s", found.Name)
	}
}

func TestFederationManager_GetProviderByName_NotFoundBoost(t *testing.T) {
	providerStorage := NewMemoryProviderStorage()
	linkStorage := NewMemoryFederationStorage()
	stateStorage := NewMemoryStateStorage()

	userStorage := newMockUserStorage()
	roleStorage := newMockRoleStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: userStorage,
		RoleStorage: roleStorage,
	})

	manager, err := NewFederationManager(
		WithProviderStorage(providerStorage),
		WithLinkStorage(linkStorage),
		WithStateStorage(stateStorage),
		WithFederationIdentityStore(identityStore),
	)
	if err != nil {
		t.Fatalf("NewFederationManager failed: %v", err)
	}

	ctx := context.Background()

	// Get non-existent provider by name
	_, err = manager.GetProviderByName(ctx, "tenant-1", "non-existent")
	if err == nil {
		t.Error("expected error when getting non-existent provider by name")
	}
}

func TestFederationManager_DeleteProvider_CascadeLinks(t *testing.T) {
	providerStorage := NewMemoryProviderStorage()
	linkStorage := NewMemoryFederationStorage()
	stateStorage := NewMemoryStateStorage()

	userStorage := newMockUserStorage()
	roleStorage := newMockRoleStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: userStorage,
		RoleStorage: roleStorage,
	})

	manager, err := NewFederationManager(
		WithProviderStorage(providerStorage),
		WithLinkStorage(linkStorage),
		WithStateStorage(stateStorage),
		WithFederationIdentityStore(identityStore),
	)
	if err != nil {
		t.Fatalf("NewFederationManager failed: %v", err)
	}

	ctx := context.Background()

	// Create a provider
	provider := &ExternalProvider{
		TenantID:      "tenant-1",
		Name:          "delete-test",
		Type:          ProviderTypeOIDC,
		Enabled:       true,
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		AuthURL:       "https://auth.example.com/authorize",
		TokenURL:      "https://auth.example.com/token",
		IssuerURL:     "https://auth.example.com",
		AutoProvision: true,
	}

	err = manager.RegisterProvider(ctx, provider)
	if err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}

	// Get the provider to get its ID
	saved, err := manager.GetProviderByName(ctx, "tenant-1", "delete-test")
	if err != nil {
		t.Fatalf("GetProviderByName failed: %v", err)
	}

	// Delete provider
	err = manager.DeleteProvider(ctx, "tenant-1", saved.ID)
	if err != nil {
		t.Fatalf("DeleteProvider failed: %v", err)
	}

	// Verify provider is gone
	_, err = manager.GetProvider(ctx, "tenant-1", saved.ID)
	if err == nil {
		t.Error("expected error when getting deleted provider")
	}
}

func TestFederationManager_WithNilAudit(t *testing.T) {
	providerStorage := NewMemoryProviderStorage()
	linkStorage := NewMemoryFederationStorage()
	stateStorage := NewMemoryStateStorage()

	userStorage := newMockUserStorage()
	roleStorage := newMockRoleStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: userStorage,
		RoleStorage: roleStorage,
	})

	// Create manager without audit logger - should work fine
	manager, err := NewFederationManager(
		WithProviderStorage(providerStorage),
		WithLinkStorage(linkStorage),
		WithStateStorage(stateStorage),
		WithFederationIdentityStore(identityStore),
		// No WithFederationAuditLogger - nil audit
	)
	if err != nil {
		t.Fatalf("NewFederationManager (no audit) failed: %v", err)
	}

	ctx := context.Background()

	// Operations should work without audit
	provider := &ExternalProvider{
		TenantID:      "tenant-1",
		Name:          "no-audit",
		Type:          ProviderTypeOIDC,
		Enabled:       true,
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		AuthURL:       "https://auth.example.com/authorize",
		TokenURL:      "https://auth.example.com/token",
		IssuerURL:     "https://auth.example.com",
		AutoProvision: true,
	}

	err = manager.RegisterProvider(ctx, provider)
	if err != nil {
		t.Fatalf("RegisterProvider (no audit) failed: %v", err)
	}

	// Get the provider to get its ID
	saved, err := manager.GetProviderByName(ctx, "tenant-1", "no-audit")
	if err != nil {
		t.Fatalf("GetProviderByName failed: %v", err)
	}

	// Delete should also work
	err = manager.DeleteProvider(ctx, "tenant-1", saved.ID)
	if err != nil {
		t.Fatalf("DeleteProvider (no audit) failed: %v", err)
	}
}

func TestFederationManager_BeginLogin_GeneratesUniqueNonces(t *testing.T) {
	providerStorage := NewMemoryProviderStorage()
	linkStorage := NewMemoryFederationStorage()
	stateStorage := NewMemoryStateStorage()

	userStorage := newMockUserStorage()
	roleStorage := newMockRoleStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: userStorage,
		RoleStorage: roleStorage,
	})

	manager, err := NewFederationManager(
		WithProviderStorage(providerStorage),
		WithLinkStorage(linkStorage),
		WithStateStorage(stateStorage),
		WithFederationIdentityStore(identityStore),
	)
	if err != nil {
		t.Fatalf("NewFederationManager failed: %v", err)
	}

	ctx := context.Background()

	// Create a provider
	provider := &ExternalProvider{
		TenantID:      "tenant-1",
		Name:          "nonce-test",
		Type:          ProviderTypeOIDC,
		Enabled:       true,
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		AuthURL:       "https://auth.example.com/authorize",
		TokenURL:      "https://auth.example.com/token",
		IssuerURL:     "https://auth.example.com",
		AutoProvision: true,
	}

	err = manager.RegisterProvider(ctx, provider)
	if err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}

	nonces := make(map[string]bool)
	for i := 0; i < 10; i++ {
		redirect, err := manager.BeginLogin(ctx, "tenant-1", "nonce-test", "https://app.example.com/callback")
		if err != nil {
			t.Fatalf("BeginLogin failed: %v", err)
		}

		if nonces[redirect.State] {
			t.Errorf("duplicate nonce generated: %s", redirect.State)
		}
		nonces[redirect.State] = true
	}
}

// =============================================================================
// PEP Metrics - Coverage for Percentile and GetCacheHitRate
// =============================================================================

func TestPEPMetrics_PercentileEdgeCases(t *testing.T) {
	metrics := NewPEPMetrics()

	// Test with empty samples
	p50 := metrics.latencyHistogram.Percentile(0.5)
	if p50 != 0 {
		t.Errorf("expected 0 for empty histogram, got %d", p50)
	}

	// Add samples
	metrics.RecordCheck(100*time.Microsecond, true)
	metrics.RecordCheck(200*time.Microsecond, false)
	metrics.RecordCheck(300*time.Microsecond, true)
	metrics.RecordCheck(400*time.Microsecond, true)
	metrics.RecordCheck(500*time.Microsecond, false)

	// Test percentile edge cases - negative percentile
	p0 := metrics.latencyHistogram.Percentile(-0.1)
	if p0 < 0 {
		t.Errorf("expected non-negative result for negative percentile")
	}

	// Test percentile edge cases - percentile > 1.0
	p100Plus := metrics.latencyHistogram.Percentile(1.5)
	if p100Plus < 0 {
		t.Errorf("expected non-negative result for percentile > 1.0")
	}

	// Test p50
	p50 = metrics.latencyHistogram.Percentile(0.5)
	if p50 <= 0 {
		t.Errorf("expected positive p50, got %d", p50)
	}

	// Test p99
	p99 := metrics.latencyHistogram.Percentile(0.99)
	if p99 <= 0 {
		t.Errorf("expected positive p99, got %d", p99)
	}
}

func TestPEPMetrics_GetCacheHitRate_EdgeCases(t *testing.T) {
	metrics := NewPEPMetrics()

	// Test with zero totals
	hitRate := metrics.GetCacheHitRate()
	if hitRate != 0.0 {
		t.Errorf("expected 0.0 for empty cache stats, got %f", hitRate)
	}

	// Add cache hits
	metrics.RecordCacheHit()
	metrics.RecordCacheHit()
	metrics.RecordCacheHit()
	metrics.RecordCacheMiss()

	// Should be 75%
	hitRate = metrics.GetCacheHitRate()
	expected := 0.75
	if hitRate != expected {
		t.Errorf("expected %f, got %f", expected, hitRate)
	}

	// 100% hit rate
	metrics.Reset()
	metrics.RecordCacheHit()
	metrics.RecordCacheHit()

	hitRate = metrics.GetCacheHitRate()
	if hitRate != 1.0 {
		t.Errorf("expected 1.0 for 100%% hit rate, got %f", hitRate)
	}
}

func TestPEPMetrics_LatencyHistogram_Overflow(t *testing.T) {
	// Create histogram with small capacity
	h := NewLatencyHistogram(5)

	// Add more samples than capacity
	for i := 0; i < 10; i++ {
		h.Add(int64(i * 1000))
	}

	// Should only have 5 samples
	if h.Size() != 5 {
		t.Errorf("expected 5 samples, got %d", h.Size())
	}

	// Newest samples should remain (5000-9000)
	p50 := h.Percentile(0.5)
	if p50 < 5000 {
		t.Errorf("expected p50 >= 5000 (oldest values removed), got %d", p50)
	}
}

// =============================================================================
// Storage Backend - IncrementQuotaUsage coverage
// =============================================================================

// mockKVStore91 implements interfaces.KVStore for testing.
type mockKVStore91 struct {
	data map[string][]byte
}

func newMockKVStore91() interfaces.KVStore {
	return &mockKVStore91{
		data: make(map[string][]byte),
	}
}

func (m *mockKVStore91) Put(_ context.Context, key string, value []byte) error {
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	m.data[key] = valueCopy
	return nil
}

func (m *mockKVStore91) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := m.data[key]
	if !ok {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "mock.get"}
	}
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	return valueCopy, nil
}

func (m *mockKVStore91) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockKVStore91) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *mockKVStore91) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockKVStore91) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func TestBackendStorage_IncrementQuotaUsage_AllResources(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "quota-test-tenant"

	// Create tenant first
	tenant := &Tenant{ID: tenantID, Name: "Quota Test Tenant"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create initial BaaS-layer IAM quota (users/roles are owned by qrdb TenantServicer)
	initialQuota := &TenantQuota{
		MaxPolicies:     100,
		MaxApps:         10,
		CurrentPolicies: 0,
		CurrentApps:     0,
	}
	if err := storage.UpdateQuota(ctx, tenantID, initialQuota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// users/roles are engine-level quotas; IncrementQuotaUsage is a no-op for them
	if err := storage.IncrementQuotaUsage(ctx, tenantID, QuotaResourceUsers, 5); err != nil {
		t.Fatalf("IncrementQuotaUsage (users, no-op) should not error: %v", err)
	}

	// roles are engine-level; no-op
	if err := storage.IncrementQuotaUsage(ctx, tenantID, QuotaResourceRoles, 2); err != nil {
		t.Fatalf("IncrementQuotaUsage (roles, no-op) should not error: %v", err)
	}

	// Test incrementing policies quota
	if err := storage.IncrementQuotaUsage(ctx, tenantID, QuotaResourcePolicies, 10); err != nil {
		t.Fatalf("IncrementQuotaUsage (policies) failed: %v", err)
	}

	// Test incrementing apps quota
	if err := storage.IncrementQuotaUsage(ctx, tenantID, QuotaResourceApps, 1); err != nil {
		t.Fatalf("IncrementQuotaUsage (apps) failed: %v", err)
	}

	// Verify BaaS-layer quotas were updated
	quota, err := storage.GetQuota(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}

	// Users are not tracked here (qrdb owns them); expect 0
	if quota.CurrentPolicies != 10 {
		t.Errorf("expected CurrentPolicies=10, got %d", quota.CurrentPolicies)
	}
	if quota.CurrentApps != 1 {
		t.Errorf("expected CurrentApps=1, got %d", quota.CurrentApps)
	}

	// Test decrementing policies
	if err := storage.DecrementQuotaUsage(ctx, tenantID, QuotaResourcePolicies, 2); err != nil {
		t.Fatalf("DecrementQuotaUsage failed: %v", err)
	}

	quota, _ = storage.GetQuota(ctx, tenantID)
	if quota.CurrentPolicies != 8 {
		t.Errorf("expected CurrentPolicies=8 after decrement, got %d", quota.CurrentPolicies)
	}
}

func TestBackendStorage_TokenOps_Boost(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Store a token
	token := &Token{
		JTI:       "boost-test-jti-123",
		Subject:   "user-1",
		ClientID:  "client-1",
		TokenType: TokenTypeAccess,
		Scopes:    []string{"openid", "profile"},
		Issued:    time.Now(),
		Expires:   time.Now().Add(1 * time.Hour),
		Revoked:   false,
	}

	if err := storage.StoreToken(ctx, token); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	// Get token
	retrieved, err := storage.GetToken(ctx, token.JTI)
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if retrieved.JTI != token.JTI {
		t.Errorf("expected JTI %s, got %s", token.JTI, retrieved.JTI)
	}

	// Check if revoked (should be false)
	revoked, err := storage.IsTokenRevoked(ctx, token.JTI)
	if err != nil {
		t.Fatalf("IsTokenRevoked failed: %v", err)
	}
	if revoked {
		t.Error("expected token not to be revoked")
	}

	// Revoke token
	if err := storage.RevokeToken(ctx, token.JTI); err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	// Check if revoked (should be true now)
	revoked, err = storage.IsTokenRevoked(ctx, token.JTI)
	if err != nil {
		t.Fatalf("IsTokenRevoked (after revoke) failed: %v", err)
	}
	if !revoked {
		t.Error("expected token to be revoked")
	}

	// List tokens by user
	tokens, err := storage.ListTokensByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListTokensByUser failed: %v", err)
	}
	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(tokens))
	}
}

func TestBackendStorage_IsTokenRevoked_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Check non-existent token - should return false, nil
	revoked, err := storage.IsTokenRevoked(ctx, "non-existent-token")
	if err != nil {
		t.Fatalf("IsTokenRevoked (not found) failed: %v", err)
	}
	if revoked {
		t.Error("expected false for non-existent token")
	}
}

func TestBackendStorage_RevokeUserTokens(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Store multiple tokens for a user
	for i := 0; i < 3; i++ {
		token := &Token{
			JTI:       "user-token-boost-" + string(rune('a'+i)),
			Subject:   "user-to-revoke",
			TokenType: TokenTypeAccess,
			Issued:    time.Now(),
			Expires:   time.Now().Add(1 * time.Hour),
		}
		if err := storage.StoreToken(ctx, token); err != nil {
			t.Fatalf("StoreToken failed: %v", err)
		}
	}

	// Store a token for another user
	otherToken := &Token{
		JTI:       "other-user-token-boost",
		Subject:   "other-user",
		TokenType: TokenTypeAccess,
		Issued:    time.Now(),
		Expires:   time.Now().Add(1 * time.Hour),
	}
	if err := storage.StoreToken(ctx, otherToken); err != nil {
		t.Fatalf("StoreToken (other) failed: %v", err)
	}

	// Revoke all tokens for the target user
	if err := storage.RevokeUserTokens(ctx, "user-to-revoke"); err != nil {
		t.Fatalf("RevokeUserTokens failed: %v", err)
	}

	// Check that all tokens for the user are revoked
	tokens, _ := storage.ListTokensByUser(ctx, "user-to-revoke")
	for _, tok := range tokens {
		if !tok.Revoked {
			t.Errorf("expected token %s to be revoked", tok.JTI)
		}
	}

	// Other user's token should not be revoked
	otherRetrieved, _ := storage.GetToken(ctx, "other-user-token-boost")
	if otherRetrieved.Revoked {
		t.Error("other user's token should not be revoked")
	}
}

// =============================================================================
// OIDC Provider - ValidateAccessToken coverage
// =============================================================================

// =============================================================================
// OIDC Adapter - ValidateAccessToken coverage
// =============================================================================

func TestOIDCProviderAdapter_ValidateAccessToken_Success(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	config := &OIDCConfig{
		Issuer:            "https://test.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testSigningKeyManager(t, privateKey, "RS256"),
		AccessTokenTTL:    1 * time.Hour,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        1 * time.Hour,
	}

	appStorage := newMockAppStorage()
	tokenStorage := newMockTokenStorage()
	memStorage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	deps := OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    appStorage,
		TokenStorage:  tokenStorage,
	}

	provider, err := NewOIDCProvider(config, deps)
	if err != nil {
		t.Fatalf("NewOIDCProvider failed: %v", err)
	}

	adapter := NewOIDCProviderAdapter(provider)

	ctx := context.Background()

	// Create Claims and generate token
	now := time.Now()
	claims := &Claims{
		Issuer:    "https://test.example.com",
		Subject:   "adapter-user-1",
		Audience:  []string{"https://test.example.com"},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "adapter-test-jti",
		TenantID:  "tenant-1",
		Email:     "adapter@example.com",
		Name:      "Adapter Test User",
		Scope:     "openid profile",
		ClientID:  "client-1",
	}

	// Generate token
	tokenService := provider.tokenService
	accessToken, err := tokenService.GenerateAccessToken(ctx, claims)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	// Validate through adapter
	validatedClaims, err := adapter.ValidateAccessToken(ctx, accessToken)
	if err != nil {
		t.Fatalf("adapter.ValidateAccessToken failed: %v", err)
	}

	if validatedClaims == nil {
		t.Error("expected claims, got nil")
	}
}

func TestOIDCProviderAdapter_ValidateAccessToken_Error(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	config := &OIDCConfig{
		Issuer:            "https://test.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testSigningKeyManager(t, privateKey, "RS256"),
		AccessTokenTTL:    1 * time.Hour,
	}

	appStorage := newMockAppStorage()
	tokenStorage := newMockTokenStorage()
	memStorage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	deps := OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    appStorage,
		TokenStorage:  tokenStorage,
	}

	provider, err := NewOIDCProvider(config, deps)
	if err != nil {
		t.Fatalf("NewOIDCProvider failed: %v", err)
	}

	adapter := NewOIDCProviderAdapter(provider)

	ctx := context.Background()

	// Validate invalid token through adapter
	_, err = adapter.ValidateAccessToken(ctx, "invalid-token")
	if err == nil {
		t.Error("expected error for invalid token through adapter")
	}
}

func TestOIDCProviderAdapter_GetOAuthASMetadata_NilCheck(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	config := &OIDCConfig{
		Issuer:            "https://test.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testSigningKeyManager(t, privateKey, "RS256"),
		AccessTokenTTL:    1 * time.Hour,
	}

	appStorage := newMockAppStorage()
	tokenStorage := newMockTokenStorage()
	memStorage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	deps := OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    appStorage,
		TokenStorage:  tokenStorage,
	}

	provider, err := NewOIDCProvider(config, deps)
	if err != nil {
		t.Fatalf("NewOIDCProvider failed: %v", err)
	}

	adapter := NewOIDCProviderAdapter(provider)

	metadata, ok := adapter.GetOAuthASMetadata().(*OAuthASMetadata)
	require.True(t, ok)
	assert.Equal(t, config.Issuer, metadata.Issuer)
}

// =============================================================================
// Storage Backend - UpdateSettings and GetSettings coverage
// =============================================================================

// =============================================================================
// Storage Backend - List operations with pagination
// =============================================================================

// TestBackendStorage_ListUsersWithPagination was removed in phase C1-F:
// BackendStorage no longer persists or lists users (qrdb owns the user record).

func TestBackendStorage_ListRolesWithPagination(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore91())

	tenantID := "list-roles-tenant"
	for i := 0; i < 5; i++ {
		role := &Role{
			ID:       fmt.Sprintf("role-%d", i),
			TenantID: tenantID,
			Name:     fmt.Sprintf("Role %d", i),
			Created:  time.Now(),
			Updated:  time.Now(),
		}
		require.NoError(t, storage.CreateRole(ctx, role))
	}

	tests := []struct {
		name string
		opts ListOptions
		want int
	}{
		{"all", ListOptions{}, 5},
		{"offset beyond total", ListOptions{Offset: 100}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roles, err := storage.ListRoles(ctx, tenantID, tt.opts)
			require.NoError(t, err)
			assert.Len(t, roles, tt.want)
		})
	}
}

func TestBackendStorage_ListAppsWithPagination(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore91())

	tenantID := "list-apps-tenant"
	for i := 0; i < 5; i++ {
		app := &Application{
			ClientID:     fmt.Sprintf("client-%d", i),
			TenantID:     tenantID,
			Name:         fmt.Sprintf("App %d", i),
			ClientSecret: "secret",
			RedirectURIs: []string{"https://example.com/callback"},
			GrantTypes:   []string{"authorization_code"},
			Created:      time.Now(),
			Updated:      time.Now(),
		}
		require.NoError(t, storage.CreateApp(ctx, app))
	}

	tests := []struct {
		name string
		opts ListOptions
		want int
	}{
		{"all", ListOptions{}, 5},
		{"offset beyond total", ListOptions{Offset: 100}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apps, err := storage.ListApps(ctx, tenantID, tt.opts)
			require.NoError(t, err)
			assert.Len(t, apps, tt.want)
		})
	}
}

// =============================================================================
// Storage Backend - TenantExists and CountTenants
// =============================================================================

func TestBackendStorage_CountTenants(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Create tenants
	for i := 0; i < 3; i++ {
		tenant := &Tenant{ID: "count-tenant-" + string(rune('a'+i)), Name: "Count Tenant " + string(rune('A'+i))}
		if err := storage.CreateTenant(ctx, tenant); err != nil {
			t.Fatalf("CreateTenant failed: %v", err)
		}
	}

	// Count tenants
	count, err := storage.CountTenants(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("CountTenants failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected 3 tenants, got %d", count)
	}
}

// =============================================================================
// Storage Backend - GetAppByTenant
// =============================================================================

// =============================================================================
// Storage Backend - ValidateClientSecret
// =============================================================================

func TestBackendStorage_ValidateClientSecret(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Seed the current PBKDF2 credential form (#37). This test's subject is
	// that ValidateClientSecret accepts the right secret and rejects the wrong
	// one, not which digest predates the migration.
	plainSecret := "correct-secret"
	hashedSecret := currentSecretHash(t, plainSecret)

	// Create an app with the hashed secret
	app := &Application{
		ClientID:     "validate-secret-client",
		TenantID:     "validate-tenant",
		Name:         "Validate Secret Test",
		ClientSecret: hashedSecret,
		RedirectURIs: []string{"https://example.com/callback"},
		GrantTypes:   []string{"authorization_code"},
		Created:      time.Now(),
		Updated:      time.Now(),
	}
	if err := storage.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Validate correct secret
	valid, err := storage.ValidateClientSecret(ctx, app.ClientID, plainSecret)
	if err != nil {
		t.Fatalf("ValidateClientSecret failed: %v", err)
	}
	if !valid {
		t.Error("expected secret to be valid")
	}

	// Validate incorrect secret
	valid, err = storage.ValidateClientSecret(ctx, app.ClientID, "wrong-secret")
	if err != nil {
		t.Fatalf("ValidateClientSecret (wrong) failed: %v", err)
	}
	if valid {
		t.Error("expected secret to be invalid")
	}
}

// =============================================================================
// Storage Backend - StoreAuthCode and GetAuthCode
// =============================================================================

// =============================================================================
// Storage Backend - GetTokenByValue
// =============================================================================

func TestBackendStorage_GetTokenByValue(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Store a refresh token. StoreToken writes the value index itself, keyed by
	// the hash of the opaque secret the client will present.
	const secret = "opaque-refresh-secret"
	token := &Token{
		JTI:       "token-value-jti-boost",
		Subject:   "user-1",
		ClientID:  "client-1",
		TokenType: TokenTypeRefresh,
		Issued:    time.Now(),
		Expires:   time.Now().Add(24 * time.Hour),
		ValueHash: HashTokenValue(secret),
	}

	if err := storage.StoreToken(ctx, token); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	// Get token by the value the client holds.
	retrieved, err := storage.GetTokenByValue(ctx, secret)
	if err != nil {
		t.Fatalf("GetTokenByValue failed: %v", err)
	}

	if retrieved.JTI != token.JTI {
		t.Errorf("expected JTI %s, got %s", token.JTI, retrieved.JTI)
	}

	// The JTI is NOT a lookup handle: only the presented secret resolves.
	if _, err := storage.GetTokenByValue(ctx, token.JTI); err == nil {
		t.Error("GetTokenByValue resolved a token by JTI; the index must be value-keyed")
	}
	_ = kv
}

// =============================================================================
// Storage Backend - UpdateUser with email changes
// =============================================================================

// BackendStorage UpdateUser/DeleteUser email-and-PIV-index tests were removed in
// phase C1-F: user persistence and the username/PIV lookup indexes moved to qrdb
// (qrdbadapter.QRDBUserStorage). The MemoryStorage suite and the qrdbadapter
// suite cover the UpdateUser/DeleteUser index lifecycle for the UserStorage seam.

// =============================================================================
// Storage Backend - DeleteTenant
// =============================================================================

// =============================================================================
// Storage Backend - UpdateTenant
// =============================================================================

// =============================================================================
// Storage Backend - CreateUser_AlreadyExists
// =============================================================================

// TestBackendStorage_CreateUser_AlreadyExists was removed in phase C1-F:
// BackendStorage no longer persists users (qrdb owns the user record).

// =============================================================================
// Storage Backend - DeleteAuthCode
// =============================================================================

// =============================================================================
// Storage Backend - GetAppByTenant NotFound
// =============================================================================

// =============================================================================
// Storage Backend - DeleteApp
// =============================================================================

// =============================================================================
// Storage Backend - UpdateApp
// =============================================================================

func TestBackendStorage_UpdateApp(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	app := &Application{
		ClientID:     "app-to-update",
		TenantID:     "update-app-tenant",
		Name:         "Original App Name",
		ClientSecret: "secret",
		RedirectURIs: []string{"https://example.com/callback"},
		GrantTypes:   []string{"authorization_code"},
		Created:      time.Now(),
		Updated:      time.Now(),
	}
	if err := storage.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	app.Name = "Updated App Name"
	if err := storage.UpdateApp(ctx, app); err != nil {
		t.Fatalf("UpdateApp failed: %v", err)
	}

	updated, err := storage.GetApp(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("GetApp failed: %v", err)
	}
	if updated.Name != "Updated App Name" {
		t.Errorf("expected name 'Updated App Name', got %s", updated.Name)
	}
}

// =============================================================================
// Storage Backend - ListTenants
// =============================================================================

// TestBackendStorage_ListTenants_ExcludesSubResources verifies that ListTenants
// only returns actual tenant records, filtering out sub-resource keys such as
// settings, quotas, users, groups, and roles that share the same key prefix.
func TestBackendStorage_ListTenants_ExcludesSubResources(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Create a tenant
	tenant := &Tenant{
		ID:      "filter-test-tenant",
		Name:    "Filter Test Tenant",
		Created: time.Now(),
		Updated: time.Now(),
	}
	require.NoError(t, storage.CreateTenant(ctx, tenant))

	// Manually insert sub-resource keys that share the tenant prefix.
	// These simulate users, groups, roles, settings, and quotas stored
	// under the same "__iam:tenant:" prefix.
	userJSON, _ := json.Marshal(map[string]interface{}{
		"id": "user-123", "email": "user@test.com", "name": "Test User",
	})
	require.NoError(t, kv.Put(ctx, "__iam:tenant:filter-test-tenant:user:user-123", userJSON))

	groupJSON, _ := json.Marshal(map[string]interface{}{
		"id": "group-abc", "name": "Test Group",
	})
	require.NoError(t, kv.Put(ctx, "__iam:tenant:filter-test-tenant:group:group-abc", groupJSON))

	roleJSON, _ := json.Marshal(map[string]interface{}{
		"id": "role-xyz", "name": "Test Role",
	})
	require.NoError(t, kv.Put(ctx, "__iam:tenant:filter-test-tenant:role:role-xyz", roleJSON))

	settingsJSON, _ := json.Marshal(map[string]interface{}{
		"mfa_required": true,
	})
	require.NoError(t, kv.Put(ctx, "__iam:tenant:filter-test-tenant:settings", settingsJSON))

	quotaJSON, _ := json.Marshal(map[string]interface{}{
		"max_users": 100,
	})
	require.NoError(t, kv.Put(ctx, "__iam:tenant:filter-test-tenant:quota", quotaJSON))

	// List tenants - should only return the actual tenant, not sub-resources
	tenants, err := storage.ListTenants(ctx, ListOptions{})
	require.NoError(t, err)

	assert.Len(t, tenants, 1, "ListTenants should return only 1 tenant, not sub-resources")
	assert.Equal(t, "filter-test-tenant", tenants[0].ID)
	assert.Equal(t, "Filter Test Tenant", tenants[0].Name)
}

// TestBackendStorage_ListTenants_ExcludesSubResources_MultiTenant verifies that
// ListTenants correctly filters sub-resources across multiple tenants.
func TestBackendStorage_ListTenants_ExcludesSubResources_MultiTenant(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Create two tenants
	tenant1 := &Tenant{ID: "multi-tenant-1", Name: "Tenant One", Created: time.Now(), Updated: time.Now()}
	tenant2 := &Tenant{ID: "multi-tenant-2", Name: "Tenant Two", Created: time.Now(), Updated: time.Now()}
	require.NoError(t, storage.CreateTenant(ctx, tenant1))
	require.NoError(t, storage.CreateTenant(ctx, tenant2))

	// Add sub-resources under each tenant
	for _, tid := range []string{"multi-tenant-1", "multi-tenant-2"} {
		for i := 0; i < 5; i++ {
			userJSON, _ := json.Marshal(map[string]interface{}{
				"id":   fmt.Sprintf("user-%d", i),
				"name": fmt.Sprintf("User %d", i),
			})
			require.NoError(t, kv.Put(ctx, fmt.Sprintf("__iam:tenant:%s:user:user-%d", tid, i), userJSON))
		}
		settingsJSON, _ := json.Marshal(map[string]interface{}{"mfa_required": true})
		require.NoError(t, kv.Put(ctx, fmt.Sprintf("__iam:tenant:%s:settings", tid), settingsJSON))
	}

	// List tenants - should only return the 2 actual tenants
	tenants, err := storage.ListTenants(ctx, ListOptions{})
	require.NoError(t, err)

	assert.Len(t, tenants, 2, "ListTenants should return only 2 tenants, not sub-resources")

	// Verify tenant IDs
	tenantIDs := make(map[string]bool)
	for _, tenant := range tenants {
		tenantIDs[tenant.ID] = true
	}
	assert.True(t, tenantIDs["multi-tenant-1"], "multi-tenant-1 should be in list")
	assert.True(t, tenantIDs["multi-tenant-2"], "multi-tenant-2 should be in list")
}

// =============================================================================
// Storage Backend - CreatePolicy and GetPolicy
// =============================================================================

// =============================================================================
// Storage Backend - UpdateRole and DeleteRole
// =============================================================================

// =============================================================================
// Additional tests to boost coverage past 91%
// =============================================================================

// Test CreatePolicy already exists error path
func TestBackendStorage_AlreadyExistsErrors(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore91())

	tests := []struct {
		name   string
		create func() error
	}{
		{"CreateRole", func() error {
			return storage.CreateRole(ctx, &Role{ID: "dup-role", TenantID: "tenant", Name: "Duplicate Role"})
		}},
		{"CreateApp", func() error {
			return storage.CreateApp(ctx, &Application{
				ClientID:     "dup-app",
				TenantID:     "tenant",
				Name:         "Duplicate App",
				ClientSecret: "secret",
				RedirectURIs: []string{"https://example.com/callback"},
				GrantTypes:   []string{"authorization_code"},
			})
		}},
		{"CreatePolicy", func() error {
			return storage.CreatePolicy(ctx, "tenant", &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "dup-policy", Tenant: "tenant"},
				Spec: PolicySpec{
					Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
				},
			})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.create())
			assert.Error(t, tt.create(), "second create must fail")
		})
	}
}

// Test ListPolicies with pagination offset and limit
func TestBackendStorage_ListPolicies_WithOffsetAndLimit(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore91())

	tenantID := "paginated-policies-tenant"
	for i := 0; i < 10; i++ {
		policy := &Policy{
			APIVersion: "v1",
			Kind:       "Policy",
			Metadata:   PolicyMetadata{Name: fmt.Sprintf("policy-%02d", i), Tenant: tenantID},
			Spec: PolicySpec{
				Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
			},
		}
		require.NoError(t, storage.CreatePolicy(ctx, tenantID, policy))
	}

	tests := []struct {
		name string
		opts ListOptions
		want int
	}{
		{"all", ListOptions{}, 10},
		{"offset and limit", ListOptions{Offset: 2, Limit: 3}, 3},
		{"offset beyond total", ListOptions{Offset: 100}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policies, err := storage.ListPolicies(ctx, tenantID, tt.opts)
			require.NoError(t, err)
			assert.Len(t, policies, tt.want)
		})
	}
}

// Test RevokeUserTokens with no tokens
func TestBackendStorage_RevokeUserTokens_NoTokens(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Revoke tokens for a user with no tokens - should not error
	err := storage.RevokeUserTokens(ctx, "user-with-no-tokens")
	if err != nil {
		t.Fatalf("RevokeUserTokens (no tokens) failed: %v", err)
	}
}

// Test quota operations with all quota resources
func TestBackendStorage_QuotaOperations_AllResources(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "quota-all-resources-tenant"

	// Create tenant and quota
	tenant := &Tenant{ID: tenantID, Name: "Quota All Resources Tenant"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// BaaS-layer IAM quotas only (users/roles are owned by qrdb TenantServicer)
	initialQuota := &TenantQuota{
		MaxPolicies: 10,
		MaxApps:     5,
	}
	if err := storage.UpdateQuota(ctx, tenantID, initialQuota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// Test increment and decrement for BaaS-layer resources
	// Users and roles are engine-level (qrdb-owned); Increment/Decrement are no-ops for them
	resources := []string{
		QuotaResourceUsers,    // no-op (engine-level)
		QuotaResourceRoles,    // no-op (engine-level)
		QuotaResourcePolicies, // BaaS-layer
		QuotaResourceApps,     // BaaS-layer
	}

	for _, resource := range resources {
		// Increment
		if err := storage.IncrementQuotaUsage(ctx, tenantID, resource, 1); err != nil {
			t.Fatalf("IncrementQuotaUsage (%s) failed: %v", resource, err)
		}

		// Decrement
		if err := storage.DecrementQuotaUsage(ctx, tenantID, resource, 1); err != nil {
			t.Fatalf("DecrementQuotaUsage (%s) failed: %v", resource, err)
		}
	}

	// Verify BaaS-layer quotas are back to zero
	quota, _ := storage.GetQuota(ctx, tenantID)
	if quota.CurrentPolicies != 0 || quota.CurrentApps != 0 {
		t.Error("expected all BaaS-layer current values to be 0 after increment/decrement")
	}
}

// Test IncrementQuotaUsage silently ignores unknown resource (per implementation)
func TestBackendStorage_IncrementQuotaUsage_UnknownResourceIgnored(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "unknown-resource-tenant"

	// Create tenant and quota
	tenant := &Tenant{ID: tenantID, Name: "Unknown Resource Tenant"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Use a BaaS-layer field for the "unchanged" assertion
	initialQuota := &TenantQuota{CurrentPolicies: 5}
	if err := storage.UpdateQuota(ctx, tenantID, initialQuota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// Increment unknown resources - implementation silently ignores them.
	// "groups" is one: quicbaas keeps no groups, so it tracks no group quota.
	for _, resource := range []string{"unknown", "groups"} {
		if err := storage.IncrementQuotaUsage(ctx, tenantID, resource, 1); err != nil {
			t.Fatalf("IncrementQuotaUsage (%s) should not error: %v", resource, err)
		}
	}

	// Verify BaaS-layer quota unchanged
	quota, _ := storage.GetQuota(ctx, tenantID)
	if quota.CurrentPolicies != 5 {
		t.Errorf("expected CurrentPolicies=5, got %d", quota.CurrentPolicies)
	}
}

// Test UpdateUser not found
// BackendStorage UpdateUser/DeleteUser not-found tests were removed in phase
// C1-F: BackendStorage no longer persists users (qrdb owns the user record).

// Test GetUserByEmail not found
// TestBackendStorage_GetUserByEmail_NotFound and the BackendStorage ListUsers
// offset tests were removed in phase C1-F: BackendStorage no longer persists or
// lists users (qrdb owns the user record). User list pagination is covered by
// the MemoryStorage and qrdbadapter suites.

// =============================================================================
// Additional tests to boost coverage past 91%
// =============================================================================

// Test ListTenants with pagination
func TestBackendStorage_ListTenants_WithPagination(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore91())

	for i := 0; i < 5; i++ {
		tenant := &Tenant{
			ID:   fmt.Sprintf("tenant-pagination-%d", i),
			Name: fmt.Sprintf("Tenant Pagination %d", i),
		}
		require.NoError(t, storage.CreateTenant(ctx, tenant))
	}

	tests := []struct {
		name string
		opts ListOptions
		want int
	}{
		{"all", ListOptions{}, 5},
		{"limit", ListOptions{Limit: 2}, 2},
		{"offset", ListOptions{Offset: 3}, 2},
		{"offset and limit", ListOptions{Offset: 1, Limit: 2}, 2},
		{"offset beyond total", ListOptions{Offset: 100}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenants, err := storage.ListTenants(ctx, tt.opts)
			require.NoError(t, err)
			assert.Len(t, tenants, tt.want)
		})
	}
}

// Test GetUser not found
// TestBackendStorage_GetUser_NotFound was removed in phase C1-F: BackendStorage
// no longer persists users (qrdb owns the user record).

// Test GetSettings returns empty when not set
func TestBackendStorage_GetSettings_ReturnsEmptyWhenNotSet(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// GetSettings returns empty settings (not error) when not set
	settings, err := storage.GetSettings(ctx, "non-existent-tenant")
	if err != nil {
		t.Fatalf("GetSettings should not error: %v", err)
	}
	if settings == nil {
		t.Error("expected empty settings, got nil")
	}
}

// Test StoreToken with different token types
func TestBackendStorage_StoreToken_DifferentTypes(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tokenTypes := []string{TokenTypeAccess, TokenTypeRefresh, TokenTypeID}

	for _, tokenType := range tokenTypes {
		token := &Token{
			JTI:       "token-type-" + tokenType,
			Subject:   "user-1",
			ClientID:  "client-1",
			TokenType: tokenType,
			Issued:    time.Now(),
			Expires:   time.Now().Add(1 * time.Hour),
		}

		if err := storage.StoreToken(ctx, token); err != nil {
			t.Fatalf("StoreToken (%s) failed: %v", tokenType, err)
		}

		retrieved, err := storage.GetToken(ctx, token.JTI)
		if err != nil {
			t.Fatalf("GetToken (%s) failed: %v", tokenType, err)
		}

		if retrieved.TokenType != tokenType {
			t.Errorf("expected type %s, got %s", tokenType, retrieved.TokenType)
		}
	}
}

// =============================================================================
// Additional tests for 91%+ coverage (unique tests)
// =============================================================================

// Test DeleteExpiredTokens with actual expired tokens and refresh token cleanup
func TestBackendStorage_DeleteExpiredTokens_ExpiredAndRefresh(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Store an expired access token
	expiredAccess := &Token{
		JTI:       "expired-access-token-91",
		Subject:   "user-91",
		TokenType: TokenTypeAccess,
		Issued:    time.Now().Add(-2 * time.Hour),
		Expires:   time.Now().Add(-1 * time.Hour),
	}
	if err := storage.StoreToken(ctx, expiredAccess); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	// Store an expired refresh token (should also clean up value index)
	expiredRefresh := &Token{
		JTI:       "expired-refresh-token-91",
		Subject:   "user-91",
		TokenType: TokenTypeRefresh,
		Issued:    time.Now().Add(-2 * time.Hour),
		Expires:   time.Now().Add(-1 * time.Hour),
	}
	if err := storage.StoreToken(ctx, expiredRefresh); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	// Store a valid token
	validToken := &Token{
		JTI:       "valid-token-91",
		Subject:   "user-91",
		TokenType: TokenTypeAccess,
		Issued:    time.Now(),
		Expires:   time.Now().Add(1 * time.Hour),
	}
	if err := storage.StoreToken(ctx, validToken); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	// Delete expired tokens
	if err := storage.DeleteExpiredTokens(ctx); err != nil {
		t.Fatalf("DeleteExpiredTokens failed: %v", err)
	}

	// Expired tokens should be gone
	_, err := storage.GetToken(ctx, expiredAccess.JTI)
	if err == nil {
		t.Error("expired access token should have been deleted")
	}

	_, err = storage.GetToken(ctx, expiredRefresh.JTI)
	if err == nil {
		t.Error("expired refresh token should have been deleted")
	}

	// Valid token should still exist
	_, err = storage.GetToken(ctx, validToken.JTI)
	if err != nil {
		t.Errorf("valid token should still exist: %v", err)
	}
}

// Test RevokeUserTokens with multiple token types
func TestBackendStorage_RevokeUserTokens_MultipleTypes(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	userID := "user-revoke-multi-91"

	// Store access tokens
	for i := 0; i < 2; i++ {
		token := &Token{
			JTI:       fmt.Sprintf("access-token-91-%d", i),
			Subject:   userID,
			TokenType: TokenTypeAccess,
			Issued:    time.Now(),
			Expires:   time.Now().Add(1 * time.Hour),
		}
		if err := storage.StoreToken(ctx, token); err != nil {
			t.Fatalf("StoreToken failed: %v", err)
		}
	}

	// Store refresh token
	refreshToken := &Token{
		JTI:       "refresh-token-91",
		Subject:   userID,
		TokenType: TokenTypeRefresh,
		Issued:    time.Now(),
		Expires:   time.Now().Add(24 * time.Hour),
	}
	if err := storage.StoreToken(ctx, refreshToken); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	// Revoke all tokens for the user
	if err := storage.RevokeUserTokens(ctx, userID); err != nil {
		t.Fatalf("RevokeUserTokens failed: %v", err)
	}

	// Check that all tokens are revoked
	for i := 0; i < 2; i++ {
		revoked, err := storage.IsTokenRevoked(ctx, fmt.Sprintf("access-token-91-%d", i))
		if err != nil {
			t.Errorf("IsTokenRevoked failed: %v", err)
		}
		if !revoked {
			t.Errorf("access token %d should be revoked", i)
		}
	}

	revoked, err := storage.IsTokenRevoked(ctx, refreshToken.JTI)
	if err != nil {
		t.Errorf("IsTokenRevoked failed: %v", err)
	}
	if !revoked {
		t.Error("refresh token should be revoked")
	}
}

// Test ListTokensByUser with mixed user tokens
func TestBackendStorage_ListTokensByUser_MixedUsers(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	user1 := "user-list-91-1"
	user2 := "user-list-91-2"

	// Store tokens for user1
	for i := 0; i < 3; i++ {
		token := &Token{
			JTI:       fmt.Sprintf("user1-token-91-%d", i),
			Subject:   user1,
			TokenType: TokenTypeAccess,
			Issued:    time.Now(),
			Expires:   time.Now().Add(1 * time.Hour),
		}
		if err := storage.StoreToken(ctx, token); err != nil {
			t.Fatalf("StoreToken failed: %v", err)
		}
	}

	// Store tokens for user2
	for i := 0; i < 2; i++ {
		token := &Token{
			JTI:       fmt.Sprintf("user2-token-91-%d", i),
			Subject:   user2,
			TokenType: TokenTypeAccess,
			Issued:    time.Now(),
			Expires:   time.Now().Add(1 * time.Hour),
		}
		if err := storage.StoreToken(ctx, token); err != nil {
			t.Fatalf("StoreToken failed: %v", err)
		}
	}

	// List tokens for user1
	tokens1, err := storage.ListTokensByUser(ctx, user1)
	if err != nil {
		t.Fatalf("ListTokensByUser failed: %v", err)
	}
	if len(tokens1) != 3 {
		t.Errorf("expected 3 tokens for user1, got %d", len(tokens1))
	}

	// List tokens for user2
	tokens2, err := storage.ListTokensByUser(ctx, user2)
	if err != nil {
		t.Fatalf("ListTokensByUser failed: %v", err)
	}
	if len(tokens2) != 2 {
		t.Errorf("expected 2 tokens for user2, got %d", len(tokens2))
	}
}

// Test UpdateSettings with valid tenant

// Test CreateApp index creation
func TestBackendStorage_CreateApp_IndexCreation91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "app-index-91"
	clientID := "client-index-91"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Test"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create app
	app := &Application{
		ClientID: clientID,
		Name:     "Index Test App",
		TenantID: tenantID,
	}
	if err := storage.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Verify both global and tenant lookups work
	globalApp, err := storage.GetApp(ctx, clientID)
	if err != nil {
		t.Fatalf("GetApp (global) failed: %v", err)
	}
	if globalApp.ClientID != clientID {
		t.Errorf("global lookup: expected %s, got %s", clientID, globalApp.ClientID)
	}

	tenantApp, err := storage.GetAppByTenant(ctx, tenantID, clientID)
	if err != nil {
		t.Fatalf("GetAppByTenant failed: %v", err)
	}
	if tenantApp.ClientID != clientID {
		t.Errorf("tenant lookup: expected %s, got %s", clientID, tenantApp.ClientID)
	}
}

// Test DeleteApp index cleanup
func TestBackendStorage_DeleteApp_IndexCleanup91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "app-delete-index-91"
	clientID := "client-delete-91"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Test"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create app
	app := &Application{
		ClientID: clientID,
		Name:     "Delete Test App",
		TenantID: tenantID,
	}
	if err := storage.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Delete app
	if err := storage.DeleteApp(ctx, clientID); err != nil {
		t.Fatalf("DeleteApp failed: %v", err)
	}

	// Verify both lookups fail
	_, err := storage.GetApp(ctx, clientID)
	if err == nil {
		t.Error("global lookup should fail after delete")
	}

	_, err = storage.GetAppByTenant(ctx, tenantID, clientID)
	if err == nil {
		t.Error("tenant lookup should fail after delete")
	}
}

// TestBackendStorage_ListUsers_AllPaginationPaths91 was removed in phase C1-F:
// BackendStorage no longer persists or lists users (qrdb owns the user record).
// User list pagination is covered by the MemoryStorage and qrdbadapter suites.

// =============================================================================
// DeltaLog tests for cache_sync.go coverage
// =============================================================================

// Test DeltaLog GetMinVersion and GetMaxVersion
func TestDeltaLog_VersionTracking91(t *testing.T) {
	dl := NewDeltaLog(100)

	// Initially 0
	if dl.GetMinVersion() != 0 {
		t.Errorf("expected min version 0, got %d", dl.GetMinVersion())
	}

	// Add events
	for i := int64(5); i < 15; i++ {
		event := &EnhancedPolicyChangeEvent{
			TenantID: "test",
			Version:  i,
		}
		if err := dl.Append(event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	if dl.GetMinVersion() != 5 {
		t.Errorf("expected min version 5, got %d", dl.GetMinVersion())
	}
	if dl.GetMaxVersion() != 14 {
		t.Errorf("expected max version 14, got %d", dl.GetMaxVersion())
	}
}
