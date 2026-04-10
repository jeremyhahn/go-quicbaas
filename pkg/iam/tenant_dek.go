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

import "context"

// TenantDEKProvisioner provisions and deprovisions per-tenant Data Encryption
// Keys. When a tenant is created, ProvisionDEK generates a tenant-specific DEK
// for encrypting that tenant's secrets at rest. When a tenant is deleted,
// DeprovisionDEK removes the tenant's DEK.
type TenantDEKProvisioner interface {
	// ProvisionDEK creates a DEK for the given tenant.
	// This method is idempotent; calling it for an existing tenant is safe.
	ProvisionDEK(ctx context.Context, tenantID string) error

	// DeprovisionDEK removes the DEK for the given tenant.
	// This method is idempotent; calling it for a non-existent tenant is safe.
	DeprovisionDEK(ctx context.Context, tenantID string) error
}

// SetDEKProvisioner sets the tenant DEK provisioner on the DefaultTenantManager.
// When set, CreateTenant will call ProvisionDEK after successful creation,
// and DeleteTenant will call DeprovisionDEK before removal.
func (m *DefaultTenantManager) SetDEKProvisioner(p TenantDEKProvisioner) {
	m.dekProvisioner = p
}

// DEKProvisioner returns the configured tenant DEK provisioner, or nil.
func (m *DefaultTenantManager) DEKProvisioner() TenantDEKProvisioner {
	return m.dekProvisioner
}
