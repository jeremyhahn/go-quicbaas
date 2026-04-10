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

// DatabasePurpose identifies the purpose of a database within a tenant.
type DatabasePurpose string

const (
	// PurposeSystem is the tenant's system database (IAM, KMS, secrets, etc).
	PurposeSystem DatabasePurpose = "system"

	// PurposeUser is a user-created database.
	PurposeUser DatabasePurpose = "user"
)

// SystemDatabaseSuffix is appended to the tenant ID to form the system
// database name.
const SystemDatabaseSuffix = "_system"

// SystemDatabaseName returns the system database name for a tenant.
func SystemDatabaseName(tenantID string) string {
	return tenantID + SystemDatabaseSuffix
}

// TenantDatabaseName returns the fully-qualified database name for a
// tenant's user database.
func TenantDatabaseName(tenantID, dbName string) string {
	return tenantID + "_" + dbName
}
