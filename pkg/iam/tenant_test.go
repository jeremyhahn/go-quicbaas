//go:build iam

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

package iam

import (
	"context"
	"sync"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// mockTenantStorage implements TenantStorage for testing.
type mockTenantStorage struct {
	mu       sync.RWMutex
	tenants  map[string]*Tenant // key: tenant ID
	settings map[string]*TenantSettings
	quotas   map[string]*TenantQuota
}

func newMockTenantStorage() *mockTenantStorage {
	return &mockTenantStorage{
		tenants:  make(map[string]*Tenant),
		settings: make(map[string]*TenantSettings),
		quotas:   make(map[string]*TenantQuota),
	}
}

func (m *mockTenantStorage) CreateTenant(ctx context.Context, tenant *Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[tenant.ID]; exists {
		return &dberrors.QBError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.tenant",
			Err:  ErrTenantAlreadyExists.Err,
		}
	}

	// Deep copy to avoid mutation
	t := *tenant
	m.tenants[tenant.ID] = &t

	// Store settings and quotas separately
	settings := tenant.Settings
	m.settings[tenant.ID] = &settings

	// BaaS-layer IAM quotas only (users/roles/storage are owned by qrdb TenantServicer)
	quota := &TenantQuota{
		MaxGroups:   int64(tenant.Quotas.MaxGroups),
		MaxPolicies: int64(tenant.Quotas.MaxPolicies),
		MaxApps:     int64(tenant.Quotas.MaxApps),
	}
	m.quotas[tenant.ID] = quota

	return nil
}

func (m *mockTenantStorage) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenant, exists := m.tenants[id]
	if !exists {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.tenant",
			Err:  ErrTenantNotFound.Err,
		}
	}

	// Return a copy
	t := *tenant
	return &t, nil
}

func (m *mockTenantStorage) UpdateTenant(ctx context.Context, tenant *Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[tenant.ID]; !exists {
		return &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.tenant",
			Err:  ErrTenantNotFound.Err,
		}
	}

	// Deep copy
	t := *tenant
	m.tenants[tenant.ID] = &t

	// Update settings and quotas
	settings := tenant.Settings
	m.settings[tenant.ID] = &settings

	// BaaS-layer IAM quotas only (users/roles/storage are owned by qrdb TenantServicer)
	quota := &TenantQuota{
		MaxGroups:   int64(tenant.Quotas.MaxGroups),
		MaxPolicies: int64(tenant.Quotas.MaxPolicies),
		MaxApps:     int64(tenant.Quotas.MaxApps),
	}
	m.quotas[tenant.ID] = quota

	return nil
}

func (m *mockTenantStorage) DeleteTenant(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[id]; !exists {
		return &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.tenant",
			Err:  ErrTenantNotFound.Err,
		}
	}

	delete(m.tenants, id)
	delete(m.settings, id)
	delete(m.quotas, id)
	return nil
}

func (m *mockTenantStorage) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tenants []*Tenant
	for _, tenant := range m.tenants {
		t := *tenant
		tenants = append(tenants, &t)
	}

	// Apply pagination
	if opts.Limit > 0 {
		// Handle offset beyond range
		if opts.Offset >= len(tenants) {
			return []*Tenant{}, nil
		}
		end := opts.Offset + opts.Limit
		if end > len(tenants) {
			end = len(tenants)
		}
		tenants = tenants[opts.Offset:end]
	}

	return tenants, nil
}

func (m *mockTenantStorage) CountTenants(ctx context.Context, opts ListOptions) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.tenants)), nil
}

func (m *mockTenantStorage) TenantExists(ctx context.Context, id string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.tenants[id]
	return exists, nil
}

func (m *mockTenantStorage) GetSettings(ctx context.Context, tenantID string) (*TenantSettings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	settings, exists := m.settings[tenantID]
	if !exists {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.tenant",
			Err:  ErrTenantNotFound.Err,
		}
	}

	s := *settings
	return &s, nil
}

func (m *mockTenantStorage) UpdateSettings(ctx context.Context, tenantID string, settings *TenantSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[tenantID]; !exists {
		return &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.tenant",
			Err:  ErrTenantNotFound.Err,
		}
	}

	s := *settings
	m.settings[tenantID] = &s
	return nil
}

func (m *mockTenantStorage) GetQuota(ctx context.Context, tenantID string) (*TenantQuota, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	quota, exists := m.quotas[tenantID]
	if !exists {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.tenant",
			Err:  ErrTenantNotFound.Err,
		}
	}

	q := *quota
	return &q, nil
}

func (m *mockTenantStorage) UpdateQuota(ctx context.Context, tenantID string, quota *TenantQuota) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[tenantID]; !exists {
		return &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.tenant",
			Err:  ErrTenantNotFound.Err,
		}
	}

	q := *quota
	m.quotas[tenantID] = &q
	return nil
}

func (m *mockTenantStorage) IncrementQuotaUsage(ctx context.Context, tenantID string, resource string, delta int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	quota, exists := m.quotas[tenantID]
	if !exists {
		return &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.tenant",
			Err:  ErrTenantNotFound.Err,
		}
	}

	// BaaS-layer IAM quotas only; users/roles/storage are engine-level (qrdb-owned).
	switch resource {
	case QuotaResourceGroups:
		quota.CurrentGroups += delta
	case QuotaResourcePolicies:
		quota.CurrentPolicies += delta
	case QuotaResourceApps:
		quota.CurrentApps += delta
	}

	return nil
}

func (m *mockTenantStorage) DecrementQuotaUsage(ctx context.Context, tenantID string, resource string, delta int64) error {
	return m.IncrementQuotaUsage(ctx, tenantID, resource, -delta)
}

func newTestTenantManager() *DefaultTenantManager {
	return NewDefaultTenantManager(newMockTenantStorage())
}

// Test CreateTenant

func TestCreateTenant_Success(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	opts := CreateTenantOptions{
		Name: "Test Tenant",
		Settings: &TenantSettings{
			MFARequired: true,
		},
	}

	tenant, err := mgr.CreateTenant(ctx, opts)
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	if tenant.ID == "" {
		t.Error("Expected tenant ID to be generated")
	}
	if tenant.Name != "Test Tenant" {
		t.Errorf("Expected name 'Test Tenant', got %s", tenant.Name)
	}
	if !tenant.Settings.MFARequired {
		t.Error("Expected MFARequired to be true")
	}
	if tenant.Created.IsZero() {
		t.Error("Expected Created timestamp to be set")
	}
	if tenant.Updated.IsZero() {
		t.Error("Expected Updated timestamp to be set")
	}
}

func TestCreateTenant_WithCustomID(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	opts := CreateTenantOptions{
		ID:   "custom-tenant-id",
		Name: "Custom Tenant",
	}

	tenant, err := mgr.CreateTenant(ctx, opts)
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	if tenant.ID != "custom-tenant-id" {
		t.Errorf("Expected ID 'custom-tenant-id', got %s", tenant.ID)
	}
}

func TestCreateTenant_InvalidName(t *testing.T) {
	tests := []struct {
		name     string
		opts     CreateTenantOptions
		wantCode dberrors.ErrorCode
	}{
		{
			name: "empty name",
			opts: CreateTenantOptions{
				Name: "",
			},
			wantCode: dberrors.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newTestTenantManager()
			ctx := context.Background()

			_, err := mgr.CreateTenant(ctx, tt.opts)
			if err == nil {
				t.Fatal("Expected error for invalid tenant name")
			}

			var dbErr *dberrors.QBError
			if !dberrors.As(err, &dbErr) {
				t.Fatalf("Expected QBError, got %T", err)
			}
			if dbErr.Code != tt.wantCode {
				t.Errorf("Expected error code %v, got %v", tt.wantCode, dbErr.Code)
			}
		})
	}
}

func TestCreateTenant_InvalidID(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		wantErr  bool
	}{
		{"empty ID allowed (auto-generated)", "", false},
		{"too short", "ab", true},
		{"too long", "a123456789012345678901234567890123456789012345678901234567890123456789", true},
		{"uppercase letters", "Test-Tenant", true},
		{"starts with hyphen", "-tenant", true},
		{"ends with hyphen", "tenant-", true},
		{"special characters", "tenant_id", true},
		{"valid lowercase", "valid-tenant-123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newTestTenantManager()
			ctx := context.Background()

			opts := CreateTenantOptions{
				ID:   tt.tenantID,
				Name: "Test Tenant",
			}

			_, err := mgr.CreateTenant(ctx, opts)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error for invalid tenant ID")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCreateTenant_DuplicateID(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	opts := CreateTenantOptions{
		ID:   "duplicate-tenant",
		Name: "First Tenant",
	}

	// Create first tenant
	_, err := mgr.CreateTenant(ctx, opts)
	if err != nil {
		t.Fatalf("First CreateTenant failed: %v", err)
	}

	// Try to create second tenant with same ID
	opts.Name = "Second Tenant"
	_, err = mgr.CreateTenant(ctx, opts)
	if err == nil {
		t.Fatal("Expected error for duplicate tenant ID")
	}

	var dbErr *dberrors.QBError
	if !dberrors.As(err, &dbErr) {
		t.Fatalf("Expected QBError, got %T", err)
	}
	if dbErr.Code != dberrors.ErrAlreadyExists {
		t.Errorf("Expected ErrAlreadyExists, got %v", dbErr.Code)
	}
}

func TestCreateTenant_WithQuotas(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// BaaS-layer IAM quotas only; MaxUsers/MaxRoles are engine-level (qrdb-owned).
	customQuotas := &TenantQuotas{
		MaxGroups:   500,
		MaxPolicies: 500,
		MaxApps:     50,
	}

	opts := CreateTenantOptions{
		Name:   "Quota Tenant",
		Quotas: customQuotas,
	}

	tenant, err := mgr.CreateTenant(ctx, opts)
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	if tenant.Quotas.MaxGroups != 500 {
		t.Errorf("Expected MaxGroups 500, got %d", tenant.Quotas.MaxGroups)
	}
}

func TestCreateTenant_DefaultQuotas(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	opts := CreateTenantOptions{
		Name: "Default Quota Tenant",
	}

	tenant, err := mgr.CreateTenant(ctx, opts)
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Verify default BaaS-layer IAM quotas (users/roles are engine-level, owned by qrdb)
	if tenant.Quotas.MaxGroups != 1000 {
		t.Errorf("Expected default MaxGroups 1000, got %d", tenant.Quotas.MaxGroups)
	}
	if tenant.Quotas.MaxPolicies != 1000 {
		t.Errorf("Expected default MaxPolicies 1000, got %d", tenant.Quotas.MaxPolicies)
	}
	if tenant.Quotas.MaxApps != 100 {
		t.Errorf("Expected default MaxApps 100, got %d", tenant.Quotas.MaxApps)
	}
}

// Test GetTenant

func TestGetTenant_Success(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a tenant
	created, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "test-tenant",
		Name: "Test Tenant",
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Get the tenant
	tenant, err := mgr.GetTenant(ctx, "test-tenant")
	if err != nil {
		t.Fatalf("GetTenant failed: %v", err)
	}

	if tenant.ID != created.ID {
		t.Errorf("Expected ID %s, got %s", created.ID, tenant.ID)
	}
	if tenant.Name != created.Name {
		t.Errorf("Expected name %s, got %s", created.Name, tenant.Name)
	}
}

func TestGetTenant_NotFound(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	_, err := mgr.GetTenant(ctx, "nonexistent-tenant")
	if err == nil {
		t.Fatal("Expected error for nonexistent tenant")
	}

	if !dberrors.IsNotFound(err) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestGetTenant_InvalidID(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
	}{
		{"empty ID", ""},
		{"too short", "ab"},
		{"uppercase", "TEST"},
		{"special chars", "test_tenant"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newTestTenantManager()
			ctx := context.Background()

			_, err := mgr.GetTenant(ctx, tt.tenantID)
			if err == nil {
				t.Error("Expected validation error for invalid tenant ID")
			}
		})
	}
}

// Test UpdateTenant

func TestUpdateTenant_Success(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a tenant
	created, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "update-test",
		Name: "Original Name",
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Wait a moment to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	// Update the tenant
	newName := "Updated Name"
	updated, err := mgr.UpdateTenant(ctx, "update-test", UpdateTenantOptions{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("UpdateTenant failed: %v", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got %s", updated.Name)
	}
	if !updated.Updated.After(created.Updated) {
		t.Error("Expected Updated timestamp to be newer")
	}
}

func TestUpdateTenant_NotFound(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	newName := "New Name"
	_, err := mgr.UpdateTenant(ctx, "nonexistent-tenant", UpdateTenantOptions{
		Name: &newName,
	})
	if err == nil {
		t.Fatal("Expected error for nonexistent tenant")
	}

	if !dberrors.IsNotFound(err) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestUpdateTenant_EmptyName(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a tenant
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "test-tenant",
		Name: "Original Name",
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Try to update with empty name
	emptyName := ""
	_, err = mgr.UpdateTenant(ctx, "test-tenant", UpdateTenantOptions{
		Name: &emptyName,
	})
	if err == nil {
		t.Fatal("Expected error for empty tenant name")
	}

	var dbErr *dberrors.QBError
	if !dberrors.As(err, &dbErr) {
		t.Fatalf("Expected QBError, got %T", err)
	}
	if dbErr.Code != dberrors.ErrInvalidInput {
		t.Errorf("Expected ErrInvalidInput, got %v", dbErr.Code)
	}
}

func TestUpdateTenant_Settings(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a tenant
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "test-tenant",
		Name: "Test Tenant",
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Update settings
	newSettings := &TenantSettings{
		MFARequired:        true,
		SessionTimeout:     30 * time.Minute,
		SessionIdleTimeout: 15 * time.Minute,
	}

	updated, err := mgr.UpdateTenant(ctx, "test-tenant", UpdateTenantOptions{
		Settings: newSettings,
	})
	if err != nil {
		t.Fatalf("UpdateTenant failed: %v", err)
	}

	if !updated.Settings.MFARequired {
		t.Error("Expected MFARequired to be true")
	}
	if updated.Settings.SessionTimeout != 30*time.Minute {
		t.Errorf("Expected SessionTimeout 30m, got %v", updated.Settings.SessionTimeout)
	}
}

func TestUpdateTenant_Quotas(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a tenant
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "test-tenant",
		Name: "Test Tenant",
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Update BaaS-layer IAM quotas (MaxUsers/MaxRoles are engine-level, owned by qrdb)
	newQuotas := &TenantQuotas{
		MaxGroups:   200,
		MaxPolicies: 200,
		MaxApps:     20,
	}

	updated, err := mgr.UpdateTenant(ctx, "test-tenant", UpdateTenantOptions{
		Quotas: newQuotas,
	})
	if err != nil {
		t.Fatalf("UpdateTenant failed: %v", err)
	}

	if updated.Quotas.MaxGroups != 200 {
		t.Errorf("Expected MaxGroups 200, got %d", updated.Quotas.MaxGroups)
	}
}

func TestUpdateTenant_PartialUpdate(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a tenant with specific settings
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "test-tenant",
		Name: "Original Name",
		Settings: &TenantSettings{
			MFARequired: true,
		},
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Update only the name, settings should remain unchanged
	newName := "Updated Name"
	updated, err := mgr.UpdateTenant(ctx, "test-tenant", UpdateTenantOptions{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("UpdateTenant failed: %v", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got %s", updated.Name)
	}
	if !updated.Settings.MFARequired {
		t.Error("Expected MFARequired to remain true")
	}
}

// Test DeleteTenant

func TestDeleteTenant_Success(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a tenant
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "delete-test",
		Name: "Delete Test",
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Delete the tenant
	err = mgr.DeleteTenant(ctx, "delete-test")
	if err != nil {
		t.Fatalf("DeleteTenant failed: %v", err)
	}

	// Verify tenant is deleted
	_, err = mgr.GetTenant(ctx, "delete-test")
	if !dberrors.IsNotFound(err) {
		t.Error("Expected tenant to be deleted")
	}
}

func TestDeleteTenant_NotFound(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	err := mgr.DeleteTenant(ctx, "nonexistent-tenant")
	if err == nil {
		t.Fatal("Expected error for nonexistent tenant")
	}

	if !dberrors.IsNotFound(err) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestDeleteTenant_InvalidID(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	err := mgr.DeleteTenant(ctx, "")
	if err == nil {
		t.Fatal("Expected error for empty tenant ID")
	}
}

// Test ListTenants

func TestListTenants_Success(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create multiple tenants
	for i := 0; i < 5; i++ {
		_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
			Name: "Tenant " + string(rune('A'+i)),
		})
		if err != nil {
			t.Fatalf("CreateTenant %d failed: %v", i, err)
		}
	}

	// List all tenants
	tenants, err := mgr.ListTenants(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListTenants failed: %v", err)
	}

	if len(tenants) != 5 {
		t.Errorf("Expected 5 tenants, got %d", len(tenants))
	}
}

func TestListTenants_WithPagination(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create 10 tenants
	for i := 0; i < 10; i++ {
		_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
			Name: "Tenant " + string(rune('A'+i)),
		})
		if err != nil {
			t.Fatalf("CreateTenant %d failed: %v", i, err)
		}
	}

	// Test pagination
	tests := []struct {
		name   string
		limit  int
		offset int
		want   int
	}{
		{"first page", 3, 0, 3},
		{"second page", 3, 3, 3},
		{"partial page", 3, 8, 2},
		{"beyond range", 3, 15, 0},
		{"no limit", 0, 0, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenants, err := mgr.ListTenants(ctx, ListOptions{
				Limit:  tt.limit,
				Offset: tt.offset,
			})
			if err != nil {
				t.Fatalf("ListTenants failed: %v", err)
			}

			if len(tenants) != tt.want {
				t.Errorf("Expected %d tenants, got %d", tt.want, len(tenants))
			}
		})
	}
}

func TestListTenants_Empty(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	tenants, err := mgr.ListTenants(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListTenants failed: %v", err)
	}

	if len(tenants) != 0 {
		t.Errorf("Expected 0 tenants, got %d", len(tenants))
	}
}

// Test CheckQuota

func TestCheckQuota_Success(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a tenant
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "quota-test",
		Name: "Quota Test",
		Quotas: &TenantQuotas{
			MaxGroups: 100, // users/roles are engine-level, owned by qrdb
		},
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Check BaaS-layer quota (should pass since current is 0)
	err = mgr.CheckQuota(ctx, "quota-test", QuotaResourceGroups)
	if err != nil {
		t.Errorf("CheckQuota failed: %v", err)
	}
}

func TestCheckQuota_Exceeded(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	storage := newMockTenantStorage()
	mgr = NewDefaultTenantManager(storage)

	// Create a tenant with BaaS-layer IAM quotas
	tenant, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "quota-test",
		Name: "Quota Test",
		Quotas: &TenantQuotas{
			MaxGroups: 100, // users/roles are engine-level, owned by qrdb
		},
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Manually set current usage to limit
	quota, _ := storage.GetQuota(ctx, tenant.ID)
	quota.CurrentGroups = quota.MaxGroups
	storage.UpdateQuota(ctx, tenant.ID, quota)

	// Check quota (should fail since at limit)
	err = mgr.CheckQuota(ctx, tenant.ID, QuotaResourceGroups)
	if err == nil {
		t.Fatal("Expected quota exceeded error")
	}

	var dbErr *dberrors.QBError
	if !dberrors.As(err, &dbErr) {
		t.Fatalf("Expected QBError, got %T", err)
	}
	if dbErr.Code != dberrors.ErrResourceExhausted {
		t.Errorf("Expected ErrResourceExhausted, got %v", dbErr.Code)
	}
}

func TestCheckQuota_UnknownResource(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a tenant
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "quota-test",
		Name: "Quota Test",
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Unknown resource types (including engine-level resources like "users", "roles",
	// "storage") return nil. Engine-level quota enforcement is owned by qrdb's
	// TenantServicer; BaaS IAM only enforces groups/policies/apps.
	err = mgr.CheckQuota(ctx, "quota-test", "unknown_resource")
	if err != nil {
		t.Fatalf("expected nil for unknown/engine-level resource, got: %v", err)
	}
}

func TestCheckQuota_TenantNotFound(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	err := mgr.CheckQuota(ctx, "nonexistent-tenant", QuotaResourceUsers)
	if err == nil {
		t.Fatal("Expected error for nonexistent tenant")
	}

	if !dberrors.IsNotFound(err) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestCheckQuota_AllResources(t *testing.T) {
	resources := []string{
		QuotaResourceUsers,
		QuotaResourceGroups,
		QuotaResourceRoles,
		QuotaResourcePolicies,
		QuotaResourceStorage,
	}

	for _, resource := range resources {
		t.Run(resource, func(t *testing.T) {
			mgr := newTestTenantManager()
			ctx := context.Background()

			// Create a tenant
			_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
				ID:   "quota-test",
				Name: "Quota Test",
			})
			if err != nil {
				t.Fatalf("CreateTenant failed: %v", err)
			}

			// Check quota for resource
			err = mgr.CheckQuota(ctx, "quota-test", resource)
			if err != nil {
				t.Errorf("CheckQuota for %s failed: %v", resource, err)
			}
		})
	}
}

// Test Edge Cases

func TestTenantIDGeneration_Uniqueness(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create multiple tenants without specifying IDs
	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		tenant, err := mgr.CreateTenant(ctx, CreateTenantOptions{
			Name: "Auto ID Tenant",
		})
		if err != nil {
			t.Fatalf("CreateTenant %d failed: %v", i, err)
		}

		if ids[tenant.ID] {
			t.Errorf("Duplicate ID generated: %s", tenant.ID)
		}
		ids[tenant.ID] = true
	}

	if len(ids) != 10 {
		t.Errorf("Expected 10 unique IDs, got %d", len(ids))
	}
}

func TestTenantIDGeneration_Format(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	tenant, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		Name: "Format Test",
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Verify ID format: "tenant-" + 16 hex characters
	if len(tenant.ID) != 23 { // "tenant-" (7) + 16 hex chars
		t.Errorf("Expected ID length 23, got %d", len(tenant.ID))
	}
	if tenant.ID[:7] != "tenant-" {
		t.Errorf("Expected ID to start with 'tenant-', got %s", tenant.ID[:7])
	}
}

func TestValidateTenantIDPattern(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid lowercase", "valid-tenant-123", false},
		{"valid min length", "abc", false},
		{"valid max length", "a123456789012345678901234567890123456789012345678901234567890ab", false},
		{"empty", "", true},
		{"too short", "ab", true},
		{"too long", "a123456789012345678901234567890123456789012345678901234567890123456789", true},
		{"uppercase", "TEST", true},
		{"starts with hyphen", "-test", true},
		{"ends with hyphen", "test-", true},
		{"underscore", "test_tenant", true},
		{"space", "test tenant", true},
		{"double hyphen", "test--tenant", false}, // allowed
		{"single char valid", "a", true},         // too short
		{"numbers only", "123", false},
		{"hyphen in middle", "test-tenant", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTenantIDPattern(tt.id)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error for ID %q", tt.id)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for ID %q: %v", tt.id, err)
				}
			}
		})
	}
}

func TestTenantContextCancellation(t *testing.T) {
	mgr := newTestTenantManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Operations should handle cancelled context gracefully
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		Name: "Test Tenant",
	})
	// Note: Mock storage doesn't check context, but real implementations should
	_ = err
}

func TestConcurrentTenantOperations(t *testing.T) {
	mgr := newTestTenantManager()
	ctx := context.Background()

	// Create a base tenant
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "concurrent-test",
		Name: "Concurrent Test",
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Perform concurrent reads
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := mgr.GetTenant(ctx, "concurrent-test")
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent read error: %v", err)
	}
}
