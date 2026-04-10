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

import "time"

// DatabaseRecord is the metadata record stored in a tenant's system
// database to track user-created databases.
type DatabaseRecord struct {
	Name     string          `json:"name"`
	TenantID string          `json:"tenant_id"`
	Engine   string          `json:"engine"`
	Backend  string          `json:"backend"`
	ShardID  uint64          `json:"shard_id,omitempty"`
	Purpose  DatabasePurpose `json:"purpose"`
	Created  time.Time       `json:"created"`
	Updated  time.Time       `json:"updated"`
}

// RecordKeyPrefix is the key prefix for database records in the system DB.
const RecordKeyPrefix = "sys:db:"

// RecordKey returns the storage key for a database record.
func RecordKey(dbName string) string {
	return RecordKeyPrefix + dbName
}

// PlatformTenantKeyPrefix is the key prefix for tenant records in the
// platform database.
const PlatformTenantKeyPrefix = "platform:tenant:"

// PlatformTenantKey returns the key for a tenant's platform record.
func PlatformTenantKey(tenantID string) string {
	return PlatformTenantKeyPrefix + tenantID
}
