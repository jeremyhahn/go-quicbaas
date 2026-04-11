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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// mockDEKProvisioner implements TenantDEKProvisioner for testing.
type mockDEKProvisioner struct {
	mu             sync.Mutex
	provisioned    map[string]bool
	provisionErr   error
	deprovisionErr error
}

func newMockDEKProvisioner() *mockDEKProvisioner {
	return &mockDEKProvisioner{
		provisioned: make(map[string]bool),
	}
}

func (m *mockDEKProvisioner) ProvisionDEK(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.provisionErr != nil {
		return m.provisionErr
	}
	m.provisioned[tenantID] = true
	return nil
}

func (m *mockDEKProvisioner) DeprovisionDEK(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deprovisionErr != nil {
		return m.deprovisionErr
	}
	delete(m.provisioned, tenantID)
	return nil
}

func (m *mockDEKProvisioner) isProvisioned(tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provisioned[tenantID]
}

// =============================================================================
// CreateTenant with DEK provisioning tests
// =============================================================================

func TestCreateTenant_ProvisionsDEK(t *testing.T) {
	storage := newMockTenantStorage()
	dekProv := newMockDEKProvisioner()

	mgr := NewDefaultTenantManager(storage)
	mgr.SetDEKProvisioner(dekProv)

	ctx := context.Background()
	tenant, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "test-tenant",
		Name: "Test Tenant",
	})

	require.NoError(t, err)
	require.NotNil(t, tenant)
	assert.Equal(t, "test-tenant", tenant.ID)

	// Verify DEK was provisioned
	assert.True(t, dekProv.isProvisioned("test-tenant"),
		"DEK should be provisioned for new tenant")
}

func TestCreateTenant_DEKProvisioningFailure_RollsBack(t *testing.T) {
	storage := newMockTenantStorage()
	dekProv := newMockDEKProvisioner()
	dekProv.provisionErr = &DEKProvisionError{Message: "KMS unavailable"}

	mgr := NewDefaultTenantManager(storage)
	mgr.SetDEKProvisioner(dekProv)

	ctx := context.Background()
	tenant, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "fail-tenant",
		Name: "Fail Tenant",
	})

	require.Error(t, err)
	assert.Nil(t, tenant)
	assert.Contains(t, err.Error(), "failed to provision tenant DEK")

	// Verify tenant record was rolled back (deleted from storage)
	exists, storageErr := storage.TenantExists(ctx, "fail-tenant")
	require.NoError(t, storageErr)
	assert.False(t, exists, "tenant record should be rolled back on DEK provisioning failure")
}

func TestCreateTenant_NoDEKProvisioner_Succeeds(t *testing.T) {
	storage := newMockTenantStorage()
	mgr := NewDefaultTenantManager(storage)
	// No DEK provisioner set

	ctx := context.Background()
	tenant, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "no-dek-tenant",
		Name: "No DEK Tenant",
	})

	require.NoError(t, err)
	require.NotNil(t, tenant)
	assert.Equal(t, "no-dek-tenant", tenant.ID)
}

// =============================================================================
// DeleteTenant with DEK deprovisioning tests
// =============================================================================

func TestDeleteTenant_DeprovisionsDEK(t *testing.T) {
	storage := newMockTenantStorage()
	dekProv := newMockDEKProvisioner()

	mgr := NewDefaultTenantManager(storage)
	mgr.SetDEKProvisioner(dekProv)

	ctx := context.Background()

	// Create tenant first
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "delete-tenant",
		Name: "Delete Tenant",
	})
	require.NoError(t, err)
	assert.True(t, dekProv.isProvisioned("delete-tenant"))

	// Delete tenant
	err = mgr.DeleteTenant(ctx, "delete-tenant")
	require.NoError(t, err)

	// Verify DEK was deprovisioned
	assert.False(t, dekProv.isProvisioned("delete-tenant"),
		"DEK should be deprovisioned when tenant is deleted")
}

func TestDeleteTenant_DEKDeprovisioningFailure(t *testing.T) {
	storage := newMockTenantStorage()
	dekProv := newMockDEKProvisioner()

	mgr := NewDefaultTenantManager(storage)
	mgr.SetDEKProvisioner(dekProv)

	ctx := context.Background()

	// Create tenant first
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "fail-delete",
		Name: "Fail Delete",
	})
	require.NoError(t, err)

	// Set DEK deprovisioning to fail
	dekProv.deprovisionErr = &DEKProvisionError{Message: "storage error"}

	// Delete should fail
	err = mgr.DeleteTenant(ctx, "fail-delete")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to deprovision tenant DEK")

	// Tenant should still exist since DEK deprovisioning failed
	exists, storageErr := storage.TenantExists(ctx, "fail-delete")
	require.NoError(t, storageErr)
	assert.True(t, exists, "tenant should still exist when DEK deprovisioning fails")
}

func TestDeleteTenant_NoDEKProvisioner_Succeeds(t *testing.T) {
	storage := newMockTenantStorage()
	mgr := NewDefaultTenantManager(storage)
	// No DEK provisioner set

	ctx := context.Background()

	// Create tenant first
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "simple-delete",
		Name: "Simple Delete",
	})
	require.NoError(t, err)

	// Delete should succeed without DEK provisioner
	err = mgr.DeleteTenant(ctx, "simple-delete")
	require.NoError(t, err)
}

// =============================================================================
// SetDEKProvisioner / DEKProvisioner accessor tests
// =============================================================================

func TestSetDEKProvisioner(t *testing.T) {
	mgr := NewDefaultTenantManager(newMockTenantStorage())
	assert.Nil(t, mgr.DEKProvisioner(), "should be nil before setting")

	dekProv := newMockDEKProvisioner()
	mgr.SetDEKProvisioner(dekProv)
	assert.Equal(t, dekProv, mgr.DEKProvisioner(), "should return the set provisioner")
}

func TestSetDEKProvisioner_Nil(t *testing.T) {
	mgr := NewDefaultTenantManager(newMockTenantStorage())
	mgr.SetDEKProvisioner(nil)
	assert.Nil(t, mgr.DEKProvisioner(), "should be nil after setting nil")
}

// =============================================================================
// DEK provisioning with database provisioner rollback
// =============================================================================

func TestCreateTenant_DEKFailure_RollsBackDatabaseProvisioner(t *testing.T) {
	storage := newMockTenantStorage()
	dekProv := newMockDEKProvisioner()
	dekProv.provisionErr = &DEKProvisionError{Message: "DEK generation failed"}

	dbProv := &mockDBProvisioner{provisioned: make(map[string]bool)}

	mgr := NewDefaultTenantManager(storage)
	mgr.SetProvisioner(dbProv)
	mgr.SetDEKProvisioner(dekProv)

	ctx := context.Background()
	tenant, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:   "rollback-test",
		Name: "Rollback Test",
	})

	require.Error(t, err)
	assert.Nil(t, tenant)

	// Both tenant record and database provisioning should be rolled back
	exists, storageErr := storage.TenantExists(ctx, "rollback-test")
	require.NoError(t, storageErr)
	assert.False(t, exists, "tenant record should be rolled back")
	assert.False(t, dbProv.isProvisioned("rollback-test"),
		"database provisioning should be rolled back")
}

// mockDBProvisioner implements database.TenantProvisioner for testing.
type mockDBProvisioner struct {
	mu          sync.Mutex
	provisioned map[string]bool
}

func (m *mockDBProvisioner) ProvisionTenant(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provisioned[tenantID] = true
	return nil
}

func (m *mockDBProvisioner) DeprovisionTenant(_ context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.provisioned, tenantID)
	return nil
}

func (m *mockDBProvisioner) isProvisioned(tenantID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provisioned[tenantID]
}

// DEKProvisionError is a typed error for DEK provisioning failures in tests.
type DEKProvisionError struct {
	Message string
}

// Error returns the error message.
func (e *DEKProvisionError) Error() string {
	return e.Message
}

// Compile-time check that DEKProvisionError satisfies the error interface.
var _ error = (*DEKProvisionError)(nil)

// Compile-time check that mockDEKProvisioner implements TenantDEKProvisioner.
var _ TenantDEKProvisioner = (*mockDEKProvisioner)(nil)

// Compile-time check that QBError is used correctly in the flow.
var _ error = (*dberrors.QBError)(nil)
