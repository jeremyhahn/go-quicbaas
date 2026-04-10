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

package secrets

import (
	"errors"
	"strings"
)

// DEKID represents a Data Encryption Key identifier.
// DEKIDs follow the format:
//   - "dek:global" for the global DEK
//   - "dek:tenant:{tenant-id}" for tenant-specific DEKs
//   - "dek:tenant:{tenant-id}:db:{db-name}" for database-specific DEKs
type DEKID string

// DEKIDGlobal is the DEKID for the global data encryption key.
const DEKIDGlobal DEKID = "dek:global"

// DEKID format constants.
const (
	dekPrefix     = "dek:"
	globalScope   = "global"
	tenantScope   = "tenant"
	dbScope       = "db"
	dekSeparator  = ":"
	minDEKIDParts = 2 // dek:global
	tenantIDParts = 3 // dek:tenant:id
	dbDEKIDParts  = 5 // dek:tenant:id:db:name
)

// dbScopeMarker is the separator pattern that identifies a database-scoped DEKID
// within the tenant portion of the string.
const dbScopeMarker = dekSeparator + dbScope + dekSeparator

// DEKID errors.
var (
	// ErrInvalidDEKIDFormat indicates the DEKID format is invalid.
	ErrInvalidDEKIDFormat = errors.New("secrets: invalid DEKID format")

	// ErrEmptyDEKID indicates an empty DEKID string was provided.
	ErrEmptyDEKID = errors.New("secrets: DEKID cannot be empty")

	// ErrInvalidTenantID indicates an invalid or empty tenant ID.
	ErrInvalidTenantID = errors.New("secrets: invalid or empty tenant ID")

	// ErrInvalidDBName indicates an invalid or empty database name.
	ErrInvalidDBName = errors.New("secrets: invalid or empty database name")

	// ErrInvalidDatabaseDEKIDFormat indicates the DEKID is not a valid database DEK ID.
	ErrInvalidDatabaseDEKIDFormat = errors.New("secrets: invalid database DEKID format")
)

// ParseDEKID parses and validates a DEKID string.
// Returns an error if the format is invalid.
func ParseDEKID(s string) (DEKID, error) {
	// Trim whitespace
	s = strings.TrimSpace(s)

	// Check for empty string
	if s == "" {
		return "", ErrEmptyDEKID
	}

	// Check for dek: prefix
	if !strings.HasPrefix(s, dekPrefix) {
		return "", ErrInvalidDEKIDFormat
	}

	// Split into minimum parts first to determine scope
	parts := strings.SplitN(s, dekSeparator, tenantIDParts)
	if len(parts) < minDEKIDParts {
		return "", ErrInvalidDEKIDFormat
	}

	// Validate scope
	scope := parts[1]
	switch scope {
	case globalScope:
		// Global DEK should be exactly "dek:global"
		if len(parts) != minDEKIDParts {
			return "", ErrInvalidDEKIDFormat
		}
		return DEKIDGlobal, nil

	case tenantScope:
		// Tenant DEK requires a tenant ID
		if len(parts) < tenantIDParts {
			return "", ErrInvalidDEKIDFormat
		}

		// Extract everything after "dek:tenant:"
		remainder := strings.TrimSpace(strings.TrimPrefix(s, dekPrefix+tenantScope+dekSeparator))
		if remainder == "" {
			return "", ErrInvalidTenantID
		}

		// Check if this is a database-scoped DEKID by looking for ":db:" in the remainder.
		if idx := strings.Index(remainder, dbScopeMarker); idx >= 0 {
			tenantID := strings.TrimSpace(remainder[:idx])
			if tenantID == "" {
				return "", ErrInvalidTenantID
			}
			dbName := strings.TrimSpace(remainder[idx+len(dbScopeMarker):])
			if dbName == "" {
				return "", ErrInvalidDBName
			}
			return DEKID(s), nil
		}

		// Check for trailing ":db" without a name
		if strings.HasSuffix(remainder, dekSeparator+dbScope) {
			return "", ErrInvalidDBName
		}

		return DEKID(s), nil

	default:
		return "", ErrInvalidDEKIDFormat
	}
}

// IsDEKID checks if a string starts with the DEKID prefix.
// This is a quick check that does not perform full validation.
func IsDEKID(s string) bool {
	return strings.HasPrefix(s, dekPrefix)
}

// NewTenantDEKID creates a new tenant-specific DEKID.
// Returns an empty DEKID if the tenant ID is invalid.
func NewTenantDEKID(tenantID string) DEKID {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ""
	}
	return DEKID(dekPrefix + tenantScope + dekSeparator + tenantID)
}

// DatabaseDEKID returns the DEK ID for a tenant's database.
// Returns an empty DEKID if tenantID or dbName is invalid.
func DatabaseDEKID(tenantID, dbName string) DEKID {
	tenantID = strings.TrimSpace(tenantID)
	dbName = strings.TrimSpace(dbName)
	if tenantID == "" || dbName == "" {
		return ""
	}
	return DEKID(dekPrefix + tenantScope + dekSeparator + tenantID + dbScopeMarker + dbName)
}

// ParseDatabaseDEKID extracts tenantID and dbName from a database DEK ID.
// Returns error if the format doesn't match.
func ParseDatabaseDEKID(dekID string) (tenantID, dbName string, err error) {
	dekID = strings.TrimSpace(dekID)
	if dekID == "" {
		return "", "", ErrEmptyDEKID
	}

	// Check prefix
	tenantPrefix := dekPrefix + tenantScope + dekSeparator
	if !strings.HasPrefix(dekID, tenantPrefix) {
		return "", "", ErrInvalidDatabaseDEKIDFormat
	}

	// Extract everything after "dek:tenant:"
	remainder := dekID[len(tenantPrefix):]

	// Find ":db:" marker in the remainder
	idx := strings.Index(remainder, dbScopeMarker)
	if idx < 0 {
		return "", "", ErrInvalidDatabaseDEKIDFormat
	}

	tenantID = strings.TrimSpace(remainder[:idx])
	if tenantID == "" {
		return "", "", ErrInvalidTenantID
	}

	dbName = strings.TrimSpace(remainder[idx+len(dbScopeMarker):])
	if dbName == "" {
		return "", "", ErrInvalidDBName
	}

	return tenantID, dbName, nil
}

// GetTenantID extracts the tenant ID from a tenant DEKID.
// Returns the tenant ID and true if this is a tenant DEKID.
// Returns empty string and false for global DEKIDs or invalid formats.
func GetTenantID(dekid DEKID) (string, bool) {
	s := string(dekid)

	// Quick checks
	tenantPrefix := dekPrefix + tenantScope + dekSeparator
	if !strings.HasPrefix(s, tenantPrefix) {
		return "", false
	}

	// Extract everything after "dek:tenant:"
	tenantID := strings.TrimPrefix(s, tenantPrefix)
	if tenantID == "" {
		return "", false
	}

	// If this is a database DEKID, extract only the tenant portion
	if idx := strings.Index(tenantID, dbScopeMarker); idx > 0 {
		tenantID = tenantID[:idx]
	}

	return tenantID, true
}

// GetDatabaseInfo extracts tenantID and dbName from a database DEKID.
// Returns the tenantID, dbName, and true if this is a database DEKID.
// Returns empty strings and false for non-database DEKIDs.
func GetDatabaseInfo(dekid DEKID) (tenantID, dbName string, ok bool) {
	tenantID, dbName, err := ParseDatabaseDEKID(string(dekid))
	if err != nil {
		return "", "", false
	}
	return tenantID, dbName, true
}

// IsGlobal returns true if this is the global DEKID.
func IsGlobal(dekid DEKID) bool {
	return dekid == DEKIDGlobal
}

// IsTenant returns true if this is a tenant-specific DEKID (not database-scoped).
func IsTenant(dekid DEKID) bool {
	_, ok := GetTenantID(dekid)
	if !ok {
		return false
	}
	// Exclude database-scoped DEKIDs
	_, _, isDB := GetDatabaseInfo(dekid)
	return !isDB
}

// IsDatabase returns true if this is a database-specific DEKID.
func IsDatabase(dekid DEKID) bool {
	_, _, ok := GetDatabaseInfo(dekid)
	return ok
}

// String returns the string representation of the DEKID.
func (d DEKID) String() string {
	return string(d)
}
