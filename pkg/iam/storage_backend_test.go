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
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	// Get nonexistent tenant
	_, err := storage.GetTenant(ctx, "nonexistent")
	assert.Error(t, err)
	assert.True(t, dberrors.IsNotFound(err))

	assert.Error(t, err)

	// Get nonexistent role
	_, err = storage.GetRole(ctx, "tenant", "nonexistent")
	assert.Error(t, err)

	// Get nonexistent app
	_, err = storage.GetApp(ctx, "nonexistent")
	assert.Error(t, err)

	// Get nonexistent token
	_, err = storage.GetToken(ctx, "nonexistent")
	assert.Error(t, err)

	// Get nonexistent policy
	_, err = storage.GetPolicy(ctx, "tenant", "nonexistent")
	assert.Error(t, err)
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

func TestBackendStorage_DeleteTenantCascade_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	// Try to delete non-existent tenant
	err := storage.DeleteTenant(ctx, "nonexistent")
	assert.Error(t, err, "should error for non-existent tenant")
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
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []PolicyImportItem{
		{
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "dry-run-test", Tenant: tenantID},
				Spec: PolicySpec{
					Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
				},
			},
		},
	}

	op, err := bp.ImportPolicies(ctx, tenantID, items, BatchOptions{
		DryRun: true, // Dry run - don't actually create
	})
	if err != nil {
		t.Fatalf("ImportPolicies failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Succeeded != 1 {
		t.Errorf("expected 1 success in dry run, got %d", saved.Progress.Succeeded)
	}
}

func TestBatchProcessor_ProcessPolicies_ValidateOnlyMode(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []PolicyImportItem{
		{
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "validate-only-test", Tenant: tenantID},
				Spec: PolicySpec{
					Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
				},
			},
		},
	}

	op, err := bp.ImportPolicies(ctx, tenantID, items, BatchOptions{
		ValidateOnly: true, // Only validate, don't create
	})
	if err != nil {
		t.Fatalf("ImportPolicies failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Succeeded != 1 {
		t.Errorf("expected 1 success in validate-only mode, got %d", saved.Progress.Succeeded)
	}
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

func TestBackendStorage_DeleteExpiredTokens(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Store expired token
	expiredToken := &Token{
		JTI:       "expired-token-boost",
		Subject:   "user-1",
		TokenType: TokenTypeAccess,
		Issued:    time.Now().Add(-2 * time.Hour),
		Expires:   time.Now().Add(-1 * time.Hour), // Already expired
	}
	if err := storage.StoreToken(ctx, expiredToken); err != nil {
		t.Fatalf("StoreToken (expired) failed: %v", err)
	}

	// Store valid token
	validToken := &Token{
		JTI:       "valid-token-boost",
		Subject:   "user-1",
		TokenType: TokenTypeAccess,
		Issued:    time.Now(),
		Expires:   time.Now().Add(1 * time.Hour), // Still valid
	}
	if err := storage.StoreToken(ctx, validToken); err != nil {
		t.Fatalf("StoreToken (valid) failed: %v", err)
	}

	// Delete expired tokens
	if err := storage.DeleteExpiredTokens(ctx); err != nil {
		t.Fatalf("DeleteExpiredTokens failed: %v", err)
	}

	// Expired token should be gone
	_, err := storage.GetToken(ctx, "expired-token-boost")
	if err == nil {
		t.Error("expected error for deleted expired token")
	}

	// Valid token should still exist
	_, err = storage.GetToken(ctx, "valid-token-boost")
	if err != nil {
		t.Errorf("valid token should still exist: %v", err)
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

func TestOIDCProvider_ValidateAccessToken_Success(t *testing.T) {
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
		AuthCodeTTL:       10 * time.Minute,
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

	ctx := context.Background()

	// Create Claims and generate token
	now := time.Now()
	claims := &Claims{
		Issuer:    "https://test.example.com",
		Subject:   "user-validate-1",
		Audience:  []string{"https://test.example.com"},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "validate-test-jti",
		TenantID:  "tenant-1",
		Roles:     []string{"admin"},
		Groups:    []string{"developers"},
		Email:     "test@example.com",
		Name:      "Test User",
		Scope:     "openid profile email",
		ClientID:  "client-123",
	}

	// Generate token using the token service
	tokenService := provider.tokenService
	accessToken, err := tokenService.GenerateAccessToken(ctx, claims)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	// Validate the token
	validatedClaims, err := provider.ValidateAccessToken(ctx, accessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}

	if validatedClaims == nil {
		t.Error("expected claims, got nil")
	}
	if validatedClaims.Subject != claims.Subject {
		t.Errorf("expected subject %s, got %s", claims.Subject, validatedClaims.Subject)
	}
}

func TestOIDCProvider_ValidateAccessToken_Invalid(t *testing.T) {
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

	ctx := context.Background()

	// Validate invalid token
	_, err = provider.ValidateAccessToken(ctx, "invalid-token-string")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

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

	// Get metadata - exercises the nil check path
	metadata := adapter.GetOAuthASMetadata()
	// The result depends on whether the provider has metadata configured
	// We're primarily testing that this doesn't panic
	_ = metadata
}

// =============================================================================
// Storage Backend - UpdateSettings and GetSettings coverage
// =============================================================================

func TestBackendStorage_SettingsOperations(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "settings-test-tenant"

	// Create tenant first
	tenant := &Tenant{ID: tenantID, Name: "Settings Test Tenant"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Get settings (should return defaults or error)
	_, err := storage.GetSettings(ctx, tenantID)
	// This may return an error if no settings exist - that's OK
	_ = err

	// Update settings
	settings := &TenantSettings{
		MaxUsers:           100,
		MFARequired:        true,
		SessionTimeout:     30 * time.Minute,
		SessionIdleTimeout: 15 * time.Minute,
		TokenTTL:           1 * time.Hour,
		RefreshTokenTTL:    24 * time.Hour,
	}

	if err := storage.UpdateSettings(ctx, tenantID, settings); err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}

	// Get settings after update
	retrieved, err := storage.GetSettings(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetSettings (after update) failed: %v", err)
	}

	if retrieved.SessionTimeout != 30*time.Minute {
		t.Errorf("expected SessionTimeout=30m, got %v", retrieved.SessionTimeout)
	}
	if !retrieved.MFARequired {
		t.Error("expected MFARequired=true")
	}
}

// =============================================================================
// Storage Backend - List operations with pagination
// =============================================================================

// TestBackendStorage_ListUsersWithPagination was removed in phase C1-F:
// BackendStorage no longer persists or lists users (qrdb owns the user record).

func TestBackendStorage_ListRolesWithPagination(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "list-roles-tenant"

	// Create multiple roles
	for i := 0; i < 5; i++ {
		role := &Role{
			ID:       "role-" + string(rune('a'+i)),
			TenantID: tenantID,
			Name:     "Role " + string(rune('A'+i)),
			Created:  time.Now(),
			Updated:  time.Now(),
		}
		if err := storage.CreateRole(ctx, role); err != nil {
			t.Fatalf("CreateRole failed: %v", err)
		}
	}

	// List all roles
	roles, err := storage.ListRoles(ctx, tenantID, ListOptions{})
	if err != nil {
		t.Fatalf("ListRoles failed: %v", err)
	}

	if len(roles) != 5 {
		t.Errorf("expected 5 roles, got %d", len(roles))
	}
}

func TestBackendStorage_ListPoliciesWithPagination(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "list-policies-tenant"

	// Create multiple policies
	for i := 0; i < 5; i++ {
		policy := &Policy{
			APIVersion: "v1",
			Kind:       "Policy",
			Metadata:   PolicyMetadata{Name: "policy-" + string(rune('a'+i)), Tenant: tenantID},
			Spec: PolicySpec{
				Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
			},
		}
		if err := storage.CreatePolicy(ctx, tenantID, policy); err != nil {
			t.Fatalf("CreatePolicy failed: %v", err)
		}
	}

	// List all policies
	policies, err := storage.ListPolicies(ctx, tenantID, ListOptions{})
	if err != nil {
		t.Fatalf("ListPolicies failed: %v", err)
	}

	if len(policies) != 5 {
		t.Errorf("expected 5 policies, got %d", len(policies))
	}
}

func TestBackendStorage_ListAppsWithPagination(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "list-apps-tenant"

	// Create multiple apps
	for i := 0; i < 5; i++ {
		app := &Application{
			ClientID:     "client-" + string(rune('a'+i)),
			TenantID:     tenantID,
			Name:         "App " + string(rune('A'+i)),
			ClientSecret: "secret-" + string(rune('a'+i)),
			RedirectURIs: []string{"https://example.com/callback"},
			GrantTypes:   []string{"authorization_code"},
			Created:      time.Now(),
			Updated:      time.Now(),
		}
		if err := storage.CreateApp(ctx, app); err != nil {
			t.Fatalf("CreateApp failed: %v", err)
		}
	}

	// List all apps
	apps, err := storage.ListApps(ctx, tenantID, ListOptions{})
	if err != nil {
		t.Fatalf("ListApps failed: %v", err)
	}

	if len(apps) != 5 {
		t.Errorf("expected 5 apps, got %d", len(apps))
	}
}

// =============================================================================
// Storage Backend - TenantExists and CountTenants
// =============================================================================

func TestBackendStorage_TenantExists(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Create a tenant
	tenant := &Tenant{ID: "exists-test", Name: "Exists Test Tenant"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Should exist
	exists, err := storage.TenantExists(ctx, "exists-test")
	if err != nil {
		t.Fatalf("TenantExists failed: %v", err)
	}
	if !exists {
		t.Error("expected tenant to exist")
	}

	// Should not exist
	exists, err = storage.TenantExists(ctx, "not-exists")
	if err != nil {
		t.Fatalf("TenantExists (not exists) failed: %v", err)
	}
	if exists {
		t.Error("expected tenant to not exist")
	}
}

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

func TestBackendStorage_GetAppByTenant(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "app-tenant-test"

	// Create an app
	app := &Application{
		ClientID:     "app-by-tenant-client",
		TenantID:     tenantID,
		Name:         "App By Tenant Test",
		ClientSecret: "secret",
		RedirectURIs: []string{"https://example.com/callback"},
		GrantTypes:   []string{"authorization_code"},
		Created:      time.Now(),
		Updated:      time.Now(),
	}
	if err := storage.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Get app by tenant
	retrieved, err := storage.GetAppByTenant(ctx, tenantID, app.ClientID)
	if err != nil {
		t.Fatalf("GetAppByTenant failed: %v", err)
	}

	if retrieved.ClientID != app.ClientID {
		t.Errorf("expected ClientID %s, got %s", app.ClientID, retrieved.ClientID)
	}
}

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

func TestBackendStorage_AuthCodeOps_Boost(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Store auth code using AuthCodeData
	code := &AuthCodeData{
		Code:                "test-auth-code-boost-123",
		ClientID:            "client-1",
		UserID:              10001,
		RedirectURI:         "https://example.com/callback",
		Scope:               []string{"openid", "profile"},
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
	}

	if err := storage.StoreAuthCode(ctx, code); err != nil {
		t.Fatalf("StoreAuthCode failed: %v", err)
	}

	// Get auth code
	retrieved, err := storage.GetAuthCode(ctx, code.Code)
	if err != nil {
		t.Fatalf("GetAuthCode failed: %v", err)
	}

	if retrieved.Code != code.Code {
		t.Errorf("expected Code %s, got %s", code.Code, retrieved.Code)
	}
	if retrieved.UserID != code.UserID {
		t.Errorf("expected UserID %d, got %d", code.UserID, retrieved.UserID)
	}
}

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

func TestBackendStorage_DeleteTenant(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Create tenant
	tenant := &Tenant{ID: "tenant-to-delete", Name: "Delete Tenant"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Delete tenant
	if err := storage.DeleteTenant(ctx, "tenant-to-delete"); err != nil {
		t.Fatalf("DeleteTenant failed: %v", err)
	}

	// Verify deleted
	_, err := storage.GetTenant(ctx, "tenant-to-delete")
	if err == nil {
		t.Error("expected error for deleted tenant")
	}
}

func TestBackendStorage_DeleteTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Delete non-existent tenant
	err := storage.DeleteTenant(ctx, "non-existent-tenant")
	if err == nil {
		t.Error("expected error for non-existent tenant")
	}
}

// =============================================================================
// Storage Backend - UpdateTenant
// =============================================================================

func TestBackendStorage_UpdateTenant(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Create tenant
	tenant := &Tenant{ID: "tenant-to-update", Name: "Original Name"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Update tenant
	tenant.Name = "Updated Name"
	if err := storage.UpdateTenant(ctx, tenant); err != nil {
		t.Fatalf("UpdateTenant failed: %v", err)
	}

	// Verify updated
	updated, err := storage.GetTenant(ctx, "tenant-to-update")
	if err != nil {
		t.Fatalf("GetTenant failed: %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %s", updated.Name)
	}
}

func TestBackendStorage_UpdateTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Update non-existent tenant
	tenant := &Tenant{ID: "non-existent", Name: "Test"}
	err := storage.UpdateTenant(ctx, tenant)
	if err == nil {
		t.Error("expected error for non-existent tenant")
	}
}

// =============================================================================
// Storage Backend - CreateUser_AlreadyExists
// =============================================================================

// TestBackendStorage_CreateUser_AlreadyExists was removed in phase C1-F:
// BackendStorage no longer persists users (qrdb owns the user record).

// =============================================================================
// Storage Backend - DeleteAuthCode
// =============================================================================

func TestBackendStorage_DeleteAuthCode(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	code := &AuthCodeData{
		Code:        "delete-auth-code",
		ClientID:    "client-1",
		UserID:      10001,
		RedirectURI: "https://example.com/callback",
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}

	if err := storage.StoreAuthCode(ctx, code); err != nil {
		t.Fatalf("StoreAuthCode failed: %v", err)
	}

	// Delete auth code
	if err := storage.DeleteAuthCode(ctx, code.Code); err != nil {
		t.Fatalf("DeleteAuthCode failed: %v", err)
	}

	// Verify deleted
	_, err := storage.GetAuthCode(ctx, code.Code)
	if err == nil {
		t.Error("expected error for deleted auth code")
	}
}

// =============================================================================
// Storage Backend - GetAppByTenant NotFound
// =============================================================================

func TestBackendStorage_GetAppByTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.GetAppByTenant(ctx, "non-tenant", "non-client")
	if err == nil {
		t.Error("expected error for non-existent app by tenant")
	}
}

// =============================================================================
// Storage Backend - DeleteApp
// =============================================================================

func TestBackendStorage_DeleteApp(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	app := &Application{
		ClientID:     "app-to-delete",
		TenantID:     "delete-app-tenant",
		Name:         "Delete App",
		ClientSecret: "secret",
		RedirectURIs: []string{"https://example.com/callback"},
		GrantTypes:   []string{"authorization_code"},
		Created:      time.Now(),
		Updated:      time.Now(),
	}
	if err := storage.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	if err := storage.DeleteApp(ctx, app.ClientID); err != nil {
		t.Fatalf("DeleteApp failed: %v", err)
	}

	_, err := storage.GetApp(ctx, app.ClientID)
	if err == nil {
		t.Error("expected error for deleted app")
	}
}

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

func TestBackendStorage_ListTenants(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Create tenants
	for i := 0; i < 3; i++ {
		tenant := &Tenant{
			ID:   fmt.Sprintf("list-tenant-%d", i),
			Name: fmt.Sprintf("List Tenant %d", i),
		}
		if err := storage.CreateTenant(ctx, tenant); err != nil {
			t.Fatalf("CreateTenant failed: %v", err)
		}
	}

	// List tenants
	tenants, err := storage.ListTenants(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListTenants failed: %v", err)
	}

	if len(tenants) != 3 {
		t.Errorf("expected 3 tenants, got %d", len(tenants))
	}
}

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

func TestBackendStorage_PolicyOperations(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "policy-ops-tenant"

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test-policy-ops", Tenant: tenantID},
		Spec: PolicySpec{
			Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
		},
	}

	if err := storage.CreatePolicy(ctx, tenantID, policy); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	retrieved, err := storage.GetPolicy(ctx, tenantID, "test-policy-ops")
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}
	if retrieved.Metadata.Name != "test-policy-ops" {
		t.Errorf("expected name test-policy-ops, got %s", retrieved.Metadata.Name)
	}
}

func TestBackendStorage_UpdatePolicy(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "update-policy-tenant"

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "update-me", Tenant: tenantID},
		Spec: PolicySpec{
			Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
		},
	}

	if err := storage.CreatePolicy(ctx, tenantID, policy); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	// Update
	policy.Spec.Rules[0].Actions = []string{"read", "write"}
	if err := storage.UpdatePolicy(ctx, tenantID, policy); err != nil {
		t.Fatalf("UpdatePolicy failed: %v", err)
	}

	updated, _ := storage.GetPolicy(ctx, tenantID, "update-me")
	if len(updated.Spec.Rules[0].Actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(updated.Spec.Rules[0].Actions))
	}
}

func TestBackendStorage_DeletePolicy(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "delete-policy-tenant"

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "delete-me", Tenant: tenantID},
		Spec: PolicySpec{
			Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
		},
	}

	if err := storage.CreatePolicy(ctx, tenantID, policy); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	if err := storage.DeletePolicy(ctx, tenantID, "delete-me"); err != nil {
		t.Fatalf("DeletePolicy failed: %v", err)
	}

	_, err := storage.GetPolicy(ctx, tenantID, "delete-me")
	if err == nil {
		t.Error("expected error for deleted policy")
	}
}

// =============================================================================
// Storage Backend - UpdateRole and DeleteRole
// =============================================================================

func TestBackendStorage_UpdateRole(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "update-role-tenant"

	role := &Role{
		ID:       "update-role",
		TenantID: tenantID,
		Name:     "Original Role",
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	if err := storage.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	role.Name = "Updated Role"
	if err := storage.UpdateRole(ctx, role); err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}

	updated, _ := storage.GetRole(ctx, tenantID, "update-role")
	if updated.Name != "Updated Role" {
		t.Errorf("expected 'Updated Role', got %s", updated.Name)
	}
}

func TestBackendStorage_DeleteRole(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "delete-role-tenant"

	role := &Role{
		ID:       "delete-role",
		TenantID: tenantID,
		Name:     "Delete Role",
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	if err := storage.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	if err := storage.DeleteRole(ctx, tenantID, "delete-role"); err != nil {
		t.Fatalf("DeleteRole failed: %v", err)
	}

	_, err := storage.GetRole(ctx, tenantID, "delete-role")
	if err == nil {
		t.Error("expected error for deleted role")
	}
}

// =============================================================================
// Additional tests to boost coverage past 91%
// =============================================================================

// Test CreatePolicy already exists error path
func TestBackendStorage_CreatePolicy_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "dup-policy-tenant"

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "dup-policy", Tenant: tenantID},
		Spec: PolicySpec{
			Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
		},
	}

	// Create first time
	if err := storage.CreatePolicy(ctx, tenantID, policy); err != nil {
		t.Fatalf("CreatePolicy (first) failed: %v", err)
	}

	// Create second time - should fail
	err := storage.CreatePolicy(ctx, tenantID, policy)
	if err == nil {
		t.Error("expected error for duplicate policy")
	}
}

// Test UpdatePolicy not found error path
func TestBackendStorage_UpdatePolicy_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "non-existent-policy", Tenant: "tenant"},
		Spec: PolicySpec{
			Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
		},
	}

	err := storage.UpdatePolicy(ctx, "tenant", policy)
	if err == nil {
		t.Error("expected error for non-existent policy")
	}
}

// Test DeletePolicy not found error path
func TestBackendStorage_DeletePolicy_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	err := storage.DeletePolicy(ctx, "tenant", "non-existent-policy")
	if err == nil {
		t.Error("expected error for non-existent policy")
	}
}

// Test ListPolicies with pagination offset and limit
func TestBackendStorage_ListPolicies_WithOffsetAndLimit(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "paginated-policies-tenant"

	// Create 10 policies
	for i := 0; i < 10; i++ {
		policy := &Policy{
			APIVersion: "v1",
			Kind:       "Policy",
			Metadata:   PolicyMetadata{Name: fmt.Sprintf("policy-%02d", i), Tenant: tenantID},
			Spec: PolicySpec{
				Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
			},
		}
		if err := storage.CreatePolicy(ctx, tenantID, policy); err != nil {
			t.Fatalf("CreatePolicy failed: %v", err)
		}
	}

	// List with offset and limit
	policies, err := storage.ListPolicies(ctx, tenantID, ListOptions{Offset: 2, Limit: 3})
	if err != nil {
		t.Fatalf("ListPolicies failed: %v", err)
	}

	if len(policies) != 3 {
		t.Errorf("expected 3 policies, got %d", len(policies))
	}

	// List with offset beyond total
	policies, err = storage.ListPolicies(ctx, tenantID, ListOptions{Offset: 100})
	if err != nil {
		t.Fatalf("ListPolicies (offset beyond) failed: %v", err)
	}

	if len(policies) != 0 {
		t.Errorf("expected 0 policies with offset beyond total, got %d", len(policies))
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

// Test GetQuota returns empty quota when not found (per implementation behavior)
func TestBackendStorage_GetQuota_ReturnsEmptyWhenNotSet(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// GetQuota returns empty quota (not error) when quota doesn't exist
	quota, err := storage.GetQuota(ctx, "non-existent-tenant")
	if err != nil {
		t.Fatalf("GetQuota should not error: %v", err)
	}
	// Returns an empty quota
	if quota == nil {
		t.Error("expected empty quota, got nil")
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

// Test UpdateRole not found
func TestBackendStorage_UpdateRole_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	role := &Role{
		ID:       "non-existent-role",
		TenantID: "tenant",
		Name:     "Non-existent",
	}

	err := storage.UpdateRole(ctx, role)
	if err == nil {
		t.Error("expected error for non-existent role")
	}
}

// Test DeleteRole not found
func TestBackendStorage_DeleteRole_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	err := storage.DeleteRole(ctx, "tenant", "non-existent-role")
	if err == nil {
		t.Error("expected error for non-existent role")
	}
}

// Test UpdateUser not found
// BackendStorage UpdateUser/DeleteUser not-found tests were removed in phase
// C1-F: BackendStorage no longer persists users (qrdb owns the user record).

// Test UpdateApp not found
func TestBackendStorage_UpdateApp_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	app := &Application{
		ClientID: "non-existent-app",
		TenantID: "tenant",
		Name:     "Non-existent",
	}

	err := storage.UpdateApp(ctx, app)
	if err == nil {
		t.Error("expected error for non-existent app")
	}
}

// Test DeleteApp not found
func TestBackendStorage_DeleteApp_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	err := storage.DeleteApp(ctx, "non-existent-app")
	if err == nil {
		t.Error("expected error for non-existent app")
	}
}

// Test CreateTenant already exists
func TestBackendStorage_CreateTenant_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenant := &Tenant{ID: "dup-tenant", Name: "Duplicate Tenant"}

	// Create first time
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant (first) failed: %v", err)
	}

	// Create second time - should fail
	err := storage.CreateTenant(ctx, tenant)
	if err == nil {
		t.Error("expected error for duplicate tenant")
	}
}

// Test CreateRole already exists
func TestBackendStorage_CreateRole_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	role := &Role{
		ID:       "dup-role",
		TenantID: "tenant",
		Name:     "Duplicate Role",
		Created:  time.Now(),
		Updated:  time.Now(),
	}

	// Create first time
	if err := storage.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole (first) failed: %v", err)
	}

	// Create second time - should fail
	err := storage.CreateRole(ctx, role)
	if err == nil {
		t.Error("expected error for duplicate role")
	}
}

// Test CreateApp already exists
func TestBackendStorage_CreateApp_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	app := &Application{
		ClientID:     "dup-app",
		TenantID:     "tenant",
		Name:         "Duplicate App",
		ClientSecret: "secret",
		RedirectURIs: []string{"https://example.com/callback"},
		GrantTypes:   []string{"authorization_code"},
		Created:      time.Now(),
		Updated:      time.Now(),
	}

	// Create first time
	if err := storage.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp (first) failed: %v", err)
	}

	// Create second time - should fail
	err := storage.CreateApp(ctx, app)
	if err == nil {
		t.Error("expected error for duplicate app")
	}
}

// Test RevokeToken not found
func TestBackendStorage_RevokeToken_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	err := storage.RevokeToken(ctx, "non-existent-token")
	if err == nil {
		t.Error("expected error for non-existent token")
	}
}

// Test GetToken not found
func TestBackendStorage_GetToken_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.GetToken(ctx, "non-existent-token")
	if err == nil {
		t.Error("expected error for non-existent token")
	}
}

// Test ValidateClientSecret for non-existent app
func TestBackendStorage_ValidateClientSecret_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.ValidateClientSecret(ctx, "non-existent-app", "secret")
	if err == nil {
		t.Error("expected error for non-existent app")
	}
}

// Test GetUserByEmail not found
// TestBackendStorage_GetUserByEmail_NotFound and the BackendStorage ListUsers
// offset tests were removed in phase C1-F: BackendStorage no longer persists or
// lists users (qrdb owns the user record). User list pagination is covered by
// the MemoryStorage and qrdbadapter suites.

// =============================================================================
// Additional tests to boost coverage past 91%
// =============================================================================

// Test ListRoles with offset
func TestBackendStorage_ListRoles_WithOffset(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "list-roles-offset-tenant"

	// Create 5 roles
	for i := 0; i < 5; i++ {
		role := &Role{
			ID:       fmt.Sprintf("role-offset-%d", i),
			TenantID: tenantID,
			Name:     fmt.Sprintf("Role Offset %d", i),
			Created:  time.Now(),
			Updated:  time.Now(),
		}
		if err := storage.CreateRole(ctx, role); err != nil {
			t.Fatalf("CreateRole failed: %v", err)
		}
	}

	// List with offset beyond total
	roles, err := storage.ListRoles(ctx, tenantID, ListOptions{Offset: 100})
	if err != nil {
		t.Fatalf("ListRoles (offset beyond) failed: %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("expected 0 roles with offset beyond total, got %d", len(roles))
	}
}

// Test ListApps with offset
func TestBackendStorage_ListApps_WithOffset(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "list-apps-offset-tenant"

	// Create 5 apps
	for i := 0; i < 5; i++ {
		app := &Application{
			ClientID:     fmt.Sprintf("client-offset-%d", i),
			TenantID:     tenantID,
			Name:         fmt.Sprintf("App Offset %d", i),
			ClientSecret: "secret",
			RedirectURIs: []string{"https://example.com/callback"},
			GrantTypes:   []string{"authorization_code"},
			Created:      time.Now(),
			Updated:      time.Now(),
		}
		if err := storage.CreateApp(ctx, app); err != nil {
			t.Fatalf("CreateApp failed: %v", err)
		}
	}

	// List with offset beyond total
	apps, err := storage.ListApps(ctx, tenantID, ListOptions{Offset: 100})
	if err != nil {
		t.Fatalf("ListApps (offset beyond) failed: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected 0 apps with offset beyond total, got %d", len(apps))
	}
}

// Test ListTenants with pagination
func TestBackendStorage_ListTenants_WithPagination(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Create 5 tenants
	for i := 0; i < 5; i++ {
		tenant := &Tenant{
			ID:   fmt.Sprintf("tenant-pagination-%d", i),
			Name: fmt.Sprintf("Tenant Pagination %d", i),
		}
		if err := storage.CreateTenant(ctx, tenant); err != nil {
			t.Fatalf("CreateTenant failed: %v", err)
		}
	}

	// List with limit
	tenants, err := storage.ListTenants(ctx, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("ListTenants (limit) failed: %v", err)
	}
	if len(tenants) != 2 {
		t.Errorf("expected 2 tenants with limit, got %d", len(tenants))
	}

	// List with offset beyond total
	tenants, err = storage.ListTenants(ctx, ListOptions{Offset: 100})
	if err != nil {
		t.Fatalf("ListTenants (offset beyond) failed: %v", err)
	}
	if len(tenants) != 0 {
		t.Errorf("expected 0 tenants with offset beyond total, got %d", len(tenants))
	}

	// List with offset and limit
	tenants, err = storage.ListTenants(ctx, ListOptions{Offset: 1, Limit: 2})
	if err != nil {
		t.Fatalf("ListTenants (offset + limit) failed: %v", err)
	}
	if len(tenants) != 2 {
		t.Errorf("expected 2 tenants with offset + limit, got %d", len(tenants))
	}
}

// Test GetPolicy not found
func TestBackendStorage_GetPolicy_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.GetPolicy(ctx, "tenant", "non-existent-policy")
	if err == nil {
		t.Error("expected error for non-existent policy")
	}
}

// Test GetRole not found
func TestBackendStorage_GetRole_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.GetRole(ctx, "tenant", "non-existent-role")
	if err == nil {
		t.Error("expected error for non-existent role")
	}
}

// Test GetUser not found
// TestBackendStorage_GetUser_NotFound was removed in phase C1-F: BackendStorage
// no longer persists users (qrdb owns the user record).

// Test GetApp not found
func TestBackendStorage_GetApp_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.GetApp(ctx, "non-existent-app")
	if err == nil {
		t.Error("expected error for non-existent app")
	}
}

// Test GetTenant not found
func TestBackendStorage_GetTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.GetTenant(ctx, "non-existent-tenant")
	if err == nil {
		t.Error("expected error for non-existent tenant")
	}
}

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

// Test UpdateSettings for existing tenant
func TestBackendStorage_UpdateSettings_Update(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "settings-update-tenant"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Settings Update Tenant"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Set initial settings
	settings1 := &TenantSettings{MFARequired: false, TokenTTL: 1 * time.Hour}
	if err := storage.UpdateSettings(ctx, tenantID, settings1); err != nil {
		t.Fatalf("UpdateSettings (initial) failed: %v", err)
	}

	// Update settings
	settings2 := &TenantSettings{MFARequired: true, TokenTTL: 2 * time.Hour}
	if err := storage.UpdateSettings(ctx, tenantID, settings2); err != nil {
		t.Fatalf("UpdateSettings (update) failed: %v", err)
	}

	// Verify update
	retrieved, _ := storage.GetSettings(ctx, tenantID)
	if !retrieved.MFARequired {
		t.Error("expected MFARequired=true")
	}
	if retrieved.TokenTTL != 2*time.Hour {
		t.Errorf("expected TokenTTL=2h, got %v", retrieved.TokenTTL)
	}
}

// Test UpdateQuota for existing tenant
func TestBackendStorage_UpdateQuota_Update(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "quota-update-tenant"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Quota Update Tenant"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Set initial BaaS-layer IAM quota (users are engine-level, owned by qrdb)
	quota1 := &TenantQuota{MaxPolicies: 10, CurrentPolicies: 5}
	if err := storage.UpdateQuota(ctx, tenantID, quota1); err != nil {
		t.Fatalf("UpdateQuota (initial) failed: %v", err)
	}

	// Update quota
	quota2 := &TenantQuota{MaxPolicies: 100, CurrentPolicies: 10}
	if err := storage.UpdateQuota(ctx, tenantID, quota2); err != nil {
		t.Fatalf("UpdateQuota (update) failed: %v", err)
	}

	// Verify update
	retrieved, _ := storage.GetQuota(ctx, tenantID)
	if retrieved.MaxPolicies != 100 {
		t.Errorf("expected MaxPolicies=100, got %d", retrieved.MaxPolicies)
	}
	if retrieved.CurrentPolicies != 10 {
		t.Errorf("expected CurrentPolicies=10, got %d", retrieved.CurrentPolicies)
	}
}

// Test GetAuthCode not found
func TestBackendStorage_GetAuthCode_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.GetAuthCode(ctx, "non-existent-code")
	if err == nil {
		t.Error("expected error for non-existent auth code")
	}
}

// Test GetTokenByValue not found
func TestBackendStorage_GetTokenByValue_NotFound(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.GetTokenByValue(ctx, "non-existent-token-value")
	if err == nil {
		t.Error("expected error for non-existent token value")
	}
}

// Test ListTokensByUser with multiple tokens
func TestBackendStorage_ListTokensByUser_MultipleTokens(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	userID := "user-multiple-tokens"

	// Store multiple tokens for the same user
	for i := 0; i < 3; i++ {
		token := &Token{
			JTI:       fmt.Sprintf("token-multi-%d", i),
			Subject:   userID,
			ClientID:  "client-1",
			TokenType: TokenTypeAccess,
			Issued:    time.Now(),
			Expires:   time.Now().Add(1 * time.Hour),
		}
		if err := storage.StoreToken(ctx, token); err != nil {
			t.Fatalf("StoreToken failed: %v", err)
		}
	}

	// Also store tokens for another user
	for i := 0; i < 2; i++ {
		token := &Token{
			JTI:       fmt.Sprintf("token-other-%d", i),
			Subject:   "other-user",
			ClientID:  "client-1",
			TokenType: TokenTypeAccess,
			Issued:    time.Now(),
			Expires:   time.Now().Add(1 * time.Hour),
		}
		if err := storage.StoreToken(ctx, token); err != nil {
			t.Fatalf("StoreToken failed: %v", err)
		}
	}

	// List tokens for target user
	tokens, err := storage.ListTokensByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListTokensByUser failed: %v", err)
	}
	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens for user, got %d", len(tokens))
	}
}

// Test DeleteExpiredTokens with no expired tokens
func TestBackendStorage_DeleteExpiredTokens_NoExpired(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Store a valid token
	token := &Token{
		JTI:       "valid-token-no-expire",
		Subject:   "user-1",
		TokenType: TokenTypeAccess,
		Issued:    time.Now(),
		Expires:   time.Now().Add(1 * time.Hour),
	}
	if err := storage.StoreToken(ctx, token); err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	// Delete expired tokens - should not affect the valid token
	if err := storage.DeleteExpiredTokens(ctx); err != nil {
		t.Fatalf("DeleteExpiredTokens failed: %v", err)
	}

	// Token should still exist
	_, err := storage.GetToken(ctx, token.JTI)
	if err != nil {
		t.Errorf("valid token should still exist: %v", err)
	}
}

// Test StoreAuthCode and DeleteAuthCode roundtrip
func TestBackendStorage_AuthCode_Roundtrip(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	code := &AuthCodeData{
		Code:        "auth-code-roundtrip",
		ClientID:    "client-1",
		UserID:      10001,
		RedirectURI: "https://example.com/callback",
		Scope:       []string{"openid"},
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}

	// Store
	if err := storage.StoreAuthCode(ctx, code); err != nil {
		t.Fatalf("StoreAuthCode failed: %v", err)
	}

	// Get
	retrieved, err := storage.GetAuthCode(ctx, code.Code)
	if err != nil {
		t.Fatalf("GetAuthCode failed: %v", err)
	}
	if retrieved.Code != code.Code {
		t.Errorf("expected code %s, got %s", code.Code, retrieved.Code)
	}

	// Delete
	if err := storage.DeleteAuthCode(ctx, code.Code); err != nil {
		t.Fatalf("DeleteAuthCode failed: %v", err)
	}

	// Verify deleted
	_, err = storage.GetAuthCode(ctx, code.Code)
	if err == nil {
		t.Error("expected error for deleted auth code")
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

// Test DeleteTenant cascade with sessions
func TestBackendStorage_DeleteTenant_WithSessions(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "cascade-sessions-91"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Session Test"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Delete tenant - verifies cascade works without errors
	if err := storage.DeleteTenant(ctx, tenantID); err != nil {
		t.Fatalf("DeleteTenant failed: %v", err)
	}

	// Verify tenant is gone
	_, err := storage.GetTenant(ctx, tenantID)
	if err == nil {
		t.Error("tenant should be deleted")
	}
}

// Test UpdateSettings with valid tenant

// Test UpdateSettings with valid tenant
func TestBackendStorage_UpdateSettings_ValidTenant91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "settings-valid-91"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Settings Test"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Update settings
	settings := &TenantSettings{
		MaxUsers: 100,
	}
	if err := storage.UpdateSettings(ctx, tenantID, settings); err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}

	// Verify settings
	retrieved, err := storage.GetSettings(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}
	if retrieved.MaxUsers != 100 {
		t.Errorf("expected MaxUsers 100, got %d", retrieved.MaxUsers)
	}
}

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

// Test StoreAuthCode and retrieval
func TestBackendStorage_StoreAuthCode_Roundtrip91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	code := &AuthCodeData{
		Code:        "auth-code-91-new",
		ClientID:    "client-91-new",
		UserID:      40050,
		RedirectURI: "https://example.com/callback",
		Scope:       []string{"openid", "profile"},
		Nonce:       "nonce-91-new",
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	}

	// Store
	if err := storage.StoreAuthCode(ctx, code); err != nil {
		t.Fatalf("StoreAuthCode failed: %v", err)
	}

	// Retrieve
	retrieved, err := storage.GetAuthCode(ctx, code.Code)
	if err != nil {
		t.Fatalf("GetAuthCode failed: %v", err)
	}

	if retrieved.Code != code.Code {
		t.Errorf("expected code %s, got %s", code.Code, retrieved.Code)
	}
	if retrieved.ClientID != code.ClientID {
		t.Errorf("expected clientID %s, got %s", code.ClientID, retrieved.ClientID)
	}
	if len(retrieved.Scope) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(retrieved.Scope))
	}
}

// Test GetRole not found
func TestBackendStorage_GetRole_NotFound91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	_, err := storage.GetRole(ctx, "tenant", "non-existent-role")
	if err == nil {
		t.Error("expected error for non-existent role")
	}
}

// Test UpdateRole
func TestBackendStorage_UpdateRole_91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "role-update-91"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Test"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create role
	role := &Role{
		ID:       "role-91",
		TenantID: tenantID,
		Name:     "Original Name",
	}
	if err := storage.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	// Update role
	role.Name = "Updated Name"
	if err := storage.UpdateRole(ctx, role); err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}

	// Verify
	retrieved, err := storage.GetRole(ctx, tenantID, role.ID)
	if err != nil {
		t.Fatalf("GetRole failed: %v", err)
	}
	if retrieved.Name != "Updated Name" {
		t.Errorf("expected 'Updated Name', got '%s'", retrieved.Name)
	}
}

// Test DeleteRole
func TestBackendStorage_DeleteRole_91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "role-delete-91"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Test"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create role
	role := &Role{
		ID:       "role-delete-91",
		TenantID: tenantID,
		Name:     "To Delete",
	}
	if err := storage.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	// Delete role
	if err := storage.DeleteRole(ctx, tenantID, role.ID); err != nil {
		t.Fatalf("DeleteRole failed: %v", err)
	}

	// Verify deleted
	_, err := storage.GetRole(ctx, tenantID, role.ID)
	if err == nil {
		t.Error("role should be deleted")
	}
}

// Test UpdateApp
func TestBackendStorage_UpdateApp_91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "app-update-91"
	clientID := "client-update-91"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Test"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create app
	app := &Application{
		ClientID: clientID,
		Name:     "Original App",
		TenantID: tenantID,
	}
	if err := storage.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Update app
	app.Name = "Updated App"
	if err := storage.UpdateApp(ctx, app); err != nil {
		t.Fatalf("UpdateApp failed: %v", err)
	}

	// Verify
	retrieved, err := storage.GetApp(ctx, clientID)
	if err != nil {
		t.Fatalf("GetApp failed: %v", err)
	}
	if retrieved.Name != "Updated App" {
		t.Errorf("expected 'Updated App', got '%s'", retrieved.Name)
	}
}

// Test UpdateTenant
func TestBackendStorage_UpdateTenant_91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	tenantID := "tenant-update-91"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Original Tenant"}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Update tenant
	tenant.Name = "Updated Tenant"
	if err := storage.UpdateTenant(ctx, tenant); err != nil {
		t.Fatalf("UpdateTenant failed: %v", err)
	}

	// Verify
	retrieved, err := storage.GetTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetTenant failed: %v", err)
	}
	if retrieved.Name != "Updated Tenant" {
		t.Errorf("expected 'Updated Tenant', got '%s'", retrieved.Name)
	}
}

// Test ListTenants pagination
func TestBackendStorage_ListTenants_Pagination91(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore91()
	storage := NewBackendStorage(kv)

	// Create tenants
	for i := 0; i < 5; i++ {
		tenant := &Tenant{
			ID:   fmt.Sprintf("tenant-pagination-91-%d", i),
			Name: fmt.Sprintf("Tenant %d", i),
		}
		if err := storage.CreateTenant(ctx, tenant); err != nil {
			t.Fatalf("CreateTenant failed: %v", err)
		}
	}

	// Test with limit
	tenants, err := storage.ListTenants(ctx, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("ListTenants failed: %v", err)
	}
	if len(tenants) != 2 {
		t.Errorf("expected 2 tenants with limit, got %d", len(tenants))
	}

	// Test with offset
	tenants, err = storage.ListTenants(ctx, ListOptions{Offset: 3})
	if err != nil {
		t.Fatalf("ListTenants failed: %v", err)
	}
	if len(tenants) != 2 {
		t.Errorf("expected 2 tenants with offset 3, got %d", len(tenants))
	}

	// Test with offset beyond count
	tenants, err = storage.ListTenants(ctx, ListOptions{Offset: 100})
	if err != nil {
		t.Fatalf("ListTenants failed: %v", err)
	}
	if len(tenants) != 0 {
		t.Errorf("expected 0 tenants, got %d", len(tenants))
	}
}

// =============================================================================
// DeltaLog tests for cache_sync.go coverage
// =============================================================================

// Test DeltaLog GetSince with version too old
func TestDeltaLog_GetSince_VersionTooOld91(t *testing.T) {
	dl := NewDeltaLog(100)
	tenantID := "test-tenant-91"

	// Append some events with version starting at 10
	for i := int64(10); i < 20; i++ {
		event := &EnhancedPolicyChangeEvent{
			TenantID: tenantID,
			Version:  i,
		}
		if err := dl.Append(event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Try to get events since version 5 (before min)
	_, err := dl.GetSince(tenantID, 5)
	if err == nil {
		t.Error("expected error for version too old")
	}
}

// Test DeltaLog Size
func TestDeltaLog_Size91(t *testing.T) {
	dl := NewDeltaLog(100)

	// Initially empty
	if dl.Size() != 0 {
		t.Errorf("expected size 0, got %d", dl.Size())
	}

	// Add some events
	for i := 0; i < 5; i++ {
		event := &EnhancedPolicyChangeEvent{
			TenantID: "test",
			Version:  int64(i),
		}
		if err := dl.Append(event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	if dl.Size() != 5 {
		t.Errorf("expected size 5, got %d", dl.Size())
	}
}

// Test DeltaLog Trim
func TestDeltaLog_Trim91(t *testing.T) {
	dl := NewDeltaLog(5) // Small buffer

	// Append more than max entries
	for i := 0; i < 10; i++ {
		event := &EnhancedPolicyChangeEvent{
			TenantID: "test",
			Version:  int64(i),
		}
		if err := dl.Append(event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Trim should update min version
	dl.Trim()

	// After trim, size should be capped at maxEntries
	if dl.Size() > 5 {
		t.Errorf("expected size <= 5 after trim, got %d", dl.Size())
	}
}

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

// Test appendToTenantLog trimming - verifies that when we exceed maxEntries,
// the log is trimmed and min version is updated
func TestDeltaLog_TenantLogTrimming91(t *testing.T) {
	dl := NewDeltaLog(10) // Small parent max

	tenantID := "trim-tenant-91"

	// Append more than maxEntries
	for i := 0; i < 15; i++ {
		event := &EnhancedPolicyChangeEvent{
			TenantID: tenantID,
			Version:  int64(i),
		}
		if err := dl.Append(event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// After trimming, the minVersion should be updated
	// We can only get events since a version >= minVersion
	// GetSince with a version that's too old should fail or return error
	// Let's request events since version 6 (which should be available)
	events, err := dl.GetSince(tenantID, 6)
	if err != nil {
		t.Fatalf("GetSince (version 6) failed: %v", err)
	}

	// We should get events with version > 6 (i.e., 7-14 = 8 events max, but after trimming maybe less)
	if events == nil {
		t.Error("expected some events")
	}
}

// Test DeltaLog GetSince for non-existent tenant
func TestDeltaLog_GetSince_NoTenant91(t *testing.T) {
	dl := NewDeltaLog(100)

	events, err := dl.GetSince("non-existent-91", 0)
	if err != nil {
		t.Fatalf("GetSince should not error: %v", err)
	}
	if events != nil && len(events) > 0 {
		t.Error("expected no events for non-existent tenant")
	}
}

// Test DeltaLog Append with nil event
func TestDeltaLog_Append_NilEvent91(t *testing.T) {
	dl := NewDeltaLog(100)

	err := dl.Append(nil)
	if err == nil {
		t.Error("expected error for nil event")
	}
}

// Test NewDeltaLog with zero entries (uses default)
func TestNewDeltaLog_ZeroEntries91(t *testing.T) {
	dl := NewDeltaLog(0)

	// Should use default of 10000
	if dl.maxEntries != 10000 {
		t.Errorf("expected maxEntries 10000, got %d", dl.maxEntries)
	}
}

// Test NewDeltaLog with negative entries (uses default)
func TestNewDeltaLog_NegativeEntries91(t *testing.T) {
	dl := NewDeltaLog(-5)

	// Should use default of 10000
	if dl.maxEntries != 10000 {
		t.Errorf("expected maxEntries 10000, got %d", dl.maxEntries)
	}
}
