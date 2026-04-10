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
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/database"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// Resource quota names for tenant quota management.
const (
	QuotaResourceUsers    = "users"
	QuotaResourceGroups   = "groups"
	QuotaResourceRoles    = "roles"
	QuotaResourcePolicies = "policies"
	QuotaResourceApps     = "apps"
	QuotaResourceStorage  = "storage"
)

// Tenant ID validation constraints.
const (
	TenantIDMinLength = 3
	TenantIDMaxLength = 63
)

var (
	// tenantIDRegex validates tenant IDs (alphanumeric lowercase, hyphens allowed).
	tenantIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// DefaultTenantManager is the default implementation of TenantManager.
type DefaultTenantManager struct {
	storage        TenantStorage
	provisioner    database.TenantProvisioner
	dekProvisioner TenantDEKProvisioner
}

// NewDefaultTenantManager creates a new default tenant manager with the provided storage backend.
func NewDefaultTenantManager(storage TenantStorage) *DefaultTenantManager {
	return &DefaultTenantManager{
		storage: storage,
	}
}

// CreateTenant creates a new tenant with the specified options.
// It validates the tenant ID, generates one if not provided, and initializes
// default settings and quotas.
func (m *DefaultTenantManager) CreateTenant(ctx context.Context, opts CreateTenantOptions) (*Tenant, error) {
	// Validate required fields
	if opts.Name == "" {
		return nil, dberrors.InvalidInput("iam.CreateTenant", "tenant name is required")
	}

	// Generate or validate tenant ID
	tenantID := opts.ID
	if tenantID == "" {
		var err error
		tenantID, err = generateTenantIDString()
		if err != nil {
			return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.CreateTenant", "failed to generate tenant ID")
		}
	} else {
		if err := validateTenantIDPattern(tenantID); err != nil {
			return nil, err
		}
	}

	// Check if tenant already exists
	exists, err := m.storage.TenantExists(ctx, tenantID)
	if err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.CreateTenant", "failed to check tenant existence")
	}
	if exists {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.CreateTenant",
			Err:  &IAMMessageError{Message: "tenant already exists: " + tenantID},
			Context: map[string]interface{}{
				"tenant_id": tenantID,
			},
		}
	}

	// Initialize default settings if not provided
	settings := TenantSettings{}
	if opts.Settings != nil {
		settings = *opts.Settings
	}

	// Initialize default quotas if not provided
	quotas := TenantQuotas{
		MaxUsers:    10000,
		MaxGroups:   1000,
		MaxRoles:    100,
		MaxPolicies: 1000,
		MaxApps:     100,
	}
	if opts.Quotas != nil {
		quotas = *opts.Quotas
	}

	// Create tenant object
	now := time.Now()
	tenant := &Tenant{
		ID:       tenantID,
		Name:     opts.Name,
		Settings: settings,
		Quotas:   quotas,
		Created:  now,
		Updated:  now,
	}

	// Store the tenant
	if err := m.storage.CreateTenant(ctx, tenant); err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.CreateTenant", "failed to create tenant")
	}

	// Provision tenant database infrastructure if a provisioner is configured.
	if m.provisioner != nil {
		if err := m.provisioner.ProvisionTenant(ctx, tenantID); err != nil {
			// Roll back: delete the tenant record since provisioning failed.
			_ = m.storage.DeleteTenant(ctx, tenantID)
			return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.CreateTenant", "failed to provision tenant database")
		}
	}

	// Provision tenant DEK if a DEK provisioner is configured.
	if m.dekProvisioner != nil {
		if err := m.dekProvisioner.ProvisionDEK(ctx, tenantID); err != nil {
			// Roll back: deprovision database and delete tenant record.
			if m.provisioner != nil {
				_ = m.provisioner.DeprovisionTenant(ctx, tenantID)
			}
			_ = m.storage.DeleteTenant(ctx, tenantID)
			return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.CreateTenant", "failed to provision tenant DEK")
		}
	}

	return tenant, nil
}

// GetTenant retrieves a tenant by ID.
func (m *DefaultTenantManager) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	if err := validateTenantIDPattern(id); err != nil {
		return nil, err
	}

	tenant, err := m.storage.GetTenant(ctx, id)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, NewTenantNotFoundError(id)
		}
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.GetTenant", "failed to get tenant")
	}

	return tenant, nil
}

// UpdateTenant updates an existing tenant with the specified options.
func (m *DefaultTenantManager) UpdateTenant(ctx context.Context, id string, opts UpdateTenantOptions) (*Tenant, error) {
	if err := validateTenantIDPattern(id); err != nil {
		return nil, err
	}

	// Get existing tenant
	tenant, err := m.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if opts.Name != nil {
		if *opts.Name == "" {
			return nil, dberrors.InvalidInput("iam.UpdateTenant", "tenant name cannot be empty")
		}
		tenant.Name = *opts.Name
	}

	if opts.Settings != nil {
		tenant.Settings = *opts.Settings
	}

	if opts.Quotas != nil {
		tenant.Quotas = *opts.Quotas
	}

	tenant.Updated = time.Now()

	// Store updated tenant
	if err := m.storage.UpdateTenant(ctx, tenant); err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.UpdateTenant", "failed to update tenant")
	}

	return tenant, nil
}

// DeleteTenant marks a tenant as deleted (soft delete).
func (m *DefaultTenantManager) DeleteTenant(ctx context.Context, id string) error {
	if err := validateTenantIDPattern(id); err != nil {
		return err
	}

	// Verify tenant exists
	_, err := m.GetTenant(ctx, id)
	if err != nil {
		return err
	}

	// Deprovision tenant DEK if a DEK provisioner is configured.
	if m.dekProvisioner != nil {
		if err := m.dekProvisioner.DeprovisionDEK(ctx, id); err != nil {
			return dberrors.Wrapf(err, dberrors.ErrInternal, "iam.DeleteTenant", "failed to deprovision tenant DEK")
		}
	}

	// Deprovision tenant database infrastructure if a provisioner is configured.
	if m.provisioner != nil {
		if err := m.provisioner.DeprovisionTenant(ctx, id); err != nil {
			return dberrors.Wrapf(err, dberrors.ErrInternal, "iam.DeleteTenant", "failed to deprovision tenant database")
		}
	}

	// Delete the tenant
	if err := m.storage.DeleteTenant(ctx, id); err != nil {
		if dberrors.IsNotFound(err) {
			return NewTenantNotFoundError(id)
		}
		return dberrors.Wrapf(err, dberrors.ErrStorage, "iam.DeleteTenant", "failed to delete tenant")
	}

	return nil
}

// ListTenants retrieves tenants with pagination and filtering.
func (m *DefaultTenantManager) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error) {
	tenants, err := m.storage.ListTenants(ctx, opts)
	if err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.ListTenants", "failed to list tenants")
	}

	return tenants, nil
}

// CheckQuota verifies that a resource increment would not exceed quota limits.
func (m *DefaultTenantManager) CheckQuota(ctx context.Context, tenantID string, resource string) error {
	if err := validateTenantIDPattern(tenantID); err != nil {
		return err
	}

	quota, err := m.storage.GetQuota(ctx, tenantID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return NewTenantNotFoundError(tenantID)
		}
		return dberrors.Wrapf(err, dberrors.ErrStorage, "iam.CheckQuota", "failed to get quota")
	}

	// Check resource-specific quota
	var limit, current int64
	switch resource {
	case QuotaResourceUsers:
		limit, current = quota.MaxUsers, quota.CurrentUsers
	case QuotaResourceGroups:
		limit, current = quota.MaxGroups, quota.CurrentGroups
	case QuotaResourceRoles:
		limit, current = quota.MaxRoles, quota.CurrentRoles
	case QuotaResourcePolicies:
		limit, current = quota.MaxPolicies, quota.CurrentPolicies
	case QuotaResourceApps:
		limit, current = quota.MaxApps, quota.CurrentApps
	case QuotaResourceStorage:
		limit, current = quota.MaxStorageBytes, quota.CurrentStorageBytes
	default:
		return dberrors.InvalidInputf("iam.CheckQuota", "unknown resource: %s", resource)
	}

	// limit == 0 means unlimited
	if limit > 0 && current >= limit {
		return NewQuotaExceededError(tenantID, resource, limit, current)
	}

	return nil
}

// validateTenantIDPattern validates a tenant ID according to the rules:
// - Alphanumeric lowercase characters and hyphens only
// - Must start and end with alphanumeric character
// - Length between 3 and 63 characters.
func validateTenantIDPattern(id string) error {
	if id == "" {
		return NewInvalidTenantIDError(id, "ID cannot be empty")
	}

	if len(id) < TenantIDMinLength {
		return NewInvalidTenantIDError(id, fmt.Sprintf("ID must be at least %d characters", TenantIDMinLength))
	}

	if len(id) > TenantIDMaxLength {
		return NewInvalidTenantIDError(id, fmt.Sprintf("ID must not exceed %d characters", TenantIDMaxLength))
	}

	if !tenantIDPattern.MatchString(id) {
		return NewInvalidTenantIDError(id, "ID must contain only lowercase alphanumeric characters and hyphens, and must start and end with an alphanumeric character")
	}

	return nil
}

// generateTenantIDString generates a random tenant ID.
func generateTenantIDString() (string, error) {
	// Generate 8 random bytes (16 hex characters)
	b, err := xkms.Rand(8)
	if err != nil {
		return "", err
	}

	// Format as "tenant-" prefix + hex string
	return "tenant-" + hex.EncodeToString(b), nil
}
