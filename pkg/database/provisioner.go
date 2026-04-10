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

package database

import "context"

// TenantProvisioner provisions and deprovisions per-tenant database
// infrastructure. When a tenant is created, ProvisionTenant sets up
// the system database and bootstraps required data. When a tenant is
// deleted, DeprovisionTenant removes all tenant databases.
type TenantProvisioner interface {
	// ProvisionTenant creates the system database for the given tenant
	// and bootstraps required subsystem data (IAM, KMS, secrets, etc).
	ProvisionTenant(ctx context.Context, tenantID string) error

	// DeprovisionTenant removes all databases owned by the given tenant,
	// including the system database.
	DeprovisionTenant(ctx context.Context, tenantID string) error
}
