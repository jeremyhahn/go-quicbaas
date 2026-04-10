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

package service

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// Keyspace prefixes for multi-tenant isolation.
const (
	// SystemKeyspacePrefix is the reserved prefix for system/infrastructure data.
	// Keys with this prefix are inaccessible to tenant operations.
	SystemKeyspacePrefix = "__sys:"

	// TenantKeyspacePrefix is the prefix for per-tenant data.
	// Format: __t:{tenantID}:{entityType}:{id}.
	TenantKeyspacePrefix = "__t:"

	// ReservedPrefix is the double underscore prefix reserved for internal use.
	ReservedPrefix = "__"

	// KeySeparator is the separator used between key components.
	KeySeparator = ":"
)

// KeyspaceResolver handles keyspace-aware key construction, parsing, and validation.
// It ensures proper tenant isolation and protects the system keyspace.
type KeyspaceResolver interface {
	// BuildKey constructs a storage key with the appropriate prefix based on tenant context.
	// For multi-tenant mode: __t:{tenantID}:{entityType}:{id}
	// For single-tenant mode (empty tenant): {entityType}:{id}
	BuildKey(ctx context.Context, entityType string, id uint64) ([]byte, error)

	// BuildPrefix constructs a prefix for scanning entities of a specific type.
	// For multi-tenant mode: __t:{tenantID}:{entityType}:
	// For single-tenant mode: {entityType}:
	BuildPrefix(ctx context.Context, entityType string) ([]byte, error)

	// BuildTenantPrefix constructs the tenant's root prefix for listing all entity types.
	// For multi-tenant mode: __t:{tenantID}:
	// For single-tenant mode: returns empty (scan all non-system keys)
	BuildTenantPrefix(ctx context.Context) ([]byte, error)

	// ParseKey extracts the tenant ID, entity type, and ID from a storage key.
	// Returns the components and any error encountered.
	ParseKey(key []byte) (tenantID string, entityType string, id uint64, err error)

	// ValidateAccess ensures the key belongs to the requesting tenant.
	// Returns an error if the key is in the system keyspace or belongs to another tenant.
	ValidateAccess(ctx context.Context, key []byte) error

	// IsSystemKey returns true if the key is in the system keyspace.
	IsSystemKey(key []byte) bool

	// IsTenantKey returns true if the key is in a tenant keyspace.
	IsTenantKey(key []byte) bool

	// IsLegacyKey returns true if the key is in legacy format (no prefix).
	IsLegacyKey(key []byte) bool

	// BuildSystemKey constructs a key in the system keyspace.
	// Only for internal use by system components.
	BuildSystemKey(path string) []byte

	// ValidateEntityType ensures the entity type is valid and not using reserved prefixes.
	ValidateEntityType(entityType string) error
}

// defaultKeyspaceResolver is the default implementation of KeyspaceResolver.
type defaultKeyspaceResolver struct{}

// NewKeyspaceResolver creates a new KeyspaceResolver instance.
func NewKeyspaceResolver() KeyspaceResolver {
	return &defaultKeyspaceResolver{}
}

// BuildKey constructs a storage key with the appropriate prefix based on tenant context.
func (r *defaultKeyspaceResolver) BuildKey(ctx context.Context, entityType string, id uint64) ([]byte, error) {
	if err := r.ValidateEntityType(entityType); err != nil {
		return nil, err
	}

	tenantID := iam.TenantFromContext(ctx)
	if tenantID == "" {
		// Legacy single-tenant mode: {entityType}:{id}
		var buf bytes.Buffer
		buf.WriteString(entityType)
		buf.WriteByte(':')
		buf.WriteString(strconv.FormatUint(id, 10))
		return buf.Bytes(), nil
	}

	// Multi-tenant mode: __t:{tenantID}:{entityType}:{id}
	var buf bytes.Buffer
	buf.WriteString(TenantKeyspacePrefix)
	buf.WriteString(tenantID)
	buf.WriteByte(':')
	buf.WriteString(entityType)
	buf.WriteByte(':')
	buf.WriteString(strconv.FormatUint(id, 10))
	return buf.Bytes(), nil
}

// BuildPrefix constructs a prefix for scanning entities of a specific type.
func (r *defaultKeyspaceResolver) BuildPrefix(ctx context.Context, entityType string) ([]byte, error) {
	if err := r.ValidateEntityType(entityType); err != nil {
		return nil, err
	}

	tenantID := iam.TenantFromContext(ctx)
	if tenantID == "" {
		// Legacy single-tenant mode: {entityType}:
		return []byte(entityType + KeySeparator), nil
	}

	// Multi-tenant mode: __t:{tenantID}:{entityType}:
	var buf bytes.Buffer
	buf.WriteString(TenantKeyspacePrefix)
	buf.WriteString(tenantID)
	buf.WriteByte(':')
	buf.WriteString(entityType)
	buf.WriteByte(':')
	return buf.Bytes(), nil
}

// BuildTenantPrefix constructs the tenant's root prefix for listing all entity types.
func (r *defaultKeyspaceResolver) BuildTenantPrefix(ctx context.Context) ([]byte, error) {
	tenantID := iam.TenantFromContext(ctx)
	if tenantID == "" {
		// Legacy single-tenant mode: return empty to scan all non-system keys
		return nil, nil
	}

	// Multi-tenant mode: __t:{tenantID}:
	var buf bytes.Buffer
	buf.WriteString(TenantKeyspacePrefix)
	buf.WriteString(tenantID)
	buf.WriteByte(':')
	return buf.Bytes(), nil
}

// ParseKey extracts the tenant ID, entity type, and ID from a storage key.
func (r *defaultKeyspaceResolver) ParseKey(key []byte) (tenantID string, entityType string, id uint64, err error) {
	keyStr := string(key)

	// Check for system keyspace
	if strings.HasPrefix(keyStr, SystemKeyspacePrefix) {
		return "", "", 0, api.NewKeyParseError("keyspace.ParseKey", "cannot parse system key", keyStr)
	}

	// Check for tenant keyspace: __t:{tenantID}:{entityType}:{id}
	if strings.HasPrefix(keyStr, TenantKeyspacePrefix) {
		remainder := keyStr[len(TenantKeyspacePrefix):]
		parts := strings.SplitN(remainder, KeySeparator, 3)
		if len(parts) < 3 {
			return "", "", 0, api.NewKeyParseError("keyspace.ParseKey", "invalid tenant key format", keyStr)
		}

		tenantID = parts[0]
		entityType = parts[1]

		// Parse ID from the third part (may contain additional colons for composite keys)
		idStr := parts[2]
		// Extract just the numeric ID (first component)
		if idx := strings.Index(idStr, KeySeparator); idx > 0 {
			idStr = idStr[:idx]
		}

		parsedID, parseErr := strconv.ParseUint(idStr, 10, 64)
		if parseErr != nil {
			return "", "", 0, api.NewKeyParseError("keyspace.ParseKey", "invalid ID in key", keyStr)
		}

		return tenantID, entityType, parsedID, nil
	}

	// Legacy format: {entityType}:{id}
	parts := strings.SplitN(keyStr, KeySeparator, 2)
	if len(parts) < 2 {
		return "", "", 0, api.NewKeyParseError("keyspace.ParseKey", "invalid legacy key format", keyStr)
	}

	entityType = parts[0]
	idStr := parts[1]

	// Extract just the numeric ID
	if idx := strings.Index(idStr, KeySeparator); idx > 0 {
		idStr = idStr[:idx]
	}

	parsedID, parseErr := strconv.ParseUint(idStr, 10, 64)
	if parseErr != nil {
		return "", "", 0, api.NewKeyParseError("keyspace.ParseKey", "invalid ID in legacy key", keyStr)
	}

	return "", entityType, parsedID, nil
}

// ValidateAccess ensures the key belongs to the requesting tenant.
func (r *defaultKeyspaceResolver) ValidateAccess(ctx context.Context, key []byte) error {
	keyStr := string(key)

	// Block access to system keyspace
	if r.IsSystemKey(key) {
		return errors.NewSystemKeyspaceViolationError("keyspace.ValidateAccess", keyStr)
	}

	requestingTenant := iam.TenantFromContext(ctx)

	// If no tenant in context (single-tenant mode)
	if requestingTenant == "" {
		// Allow access to legacy keys only
		if r.IsTenantKey(key) {
			// Single-tenant mode cannot access tenant-prefixed keys
			return errors.NewCrossTenantAccessError("keyspace.ValidateAccess", "", "tenant-prefixed")
		}
		return nil
	}

	// Multi-tenant mode: check if key belongs to requesting tenant
	if r.IsTenantKey(key) {
		keyTenant, _, _, err := r.ParseKey(key)
		if err != nil {
			return err
		}
		if keyTenant != requestingTenant {
			return errors.NewCrossTenantAccessError("keyspace.ValidateAccess", requestingTenant, keyTenant)
		}
		return nil
	}

	// Legacy key accessed in multi-tenant mode
	// This is allowed for backward compatibility, but should be migrated
	return nil
}

// IsSystemKey returns true if the key is in the system keyspace.
func (r *defaultKeyspaceResolver) IsSystemKey(key []byte) bool {
	return bytes.HasPrefix(key, []byte(SystemKeyspacePrefix))
}

// IsTenantKey returns true if the key is in a tenant keyspace.
func (r *defaultKeyspaceResolver) IsTenantKey(key []byte) bool {
	return bytes.HasPrefix(key, []byte(TenantKeyspacePrefix))
}

// IsLegacyKey returns true if the key is in legacy format (no prefix).
func (r *defaultKeyspaceResolver) IsLegacyKey(key []byte) bool {
	return !r.IsSystemKey(key) && !r.IsTenantKey(key)
}

// BuildSystemKey constructs a key in the system keyspace.
func (r *defaultKeyspaceResolver) BuildSystemKey(path string) []byte {
	return []byte(SystemKeyspacePrefix + path)
}

// ValidateEntityType ensures the entity type is valid and not using reserved prefixes.
func (r *defaultKeyspaceResolver) ValidateEntityType(entityType string) error {
	if entityType == "" {
		return api.NewEntityTypeEmptyError("keyspace.ValidateEntityType")
	}

	// Block reserved prefixes
	if strings.HasPrefix(entityType, ReservedPrefix) {
		return api.NewReservedEntityPrefixError("keyspace.ValidateEntityType", "__", entityType)
	}

	// Block entity types containing the key separator at the start
	if strings.HasPrefix(entityType, KeySeparator) {
		return api.NewSeparatorPrefixError("keyspace.ValidateEntityType", ":", entityType)
	}

	return nil
}

// ExtractEntityTypeFromKey extracts just the entity type from a key without full parsing.
// This is useful for ListEntityTypes where we only need the entity type.
func ExtractEntityTypeFromKey(resolver KeyspaceResolver, key []byte) (string, bool) {
	_, entityType, _, err := resolver.ParseKey(key)
	if err != nil {
		return "", false
	}
	return entityType, true
}
