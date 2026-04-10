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

// Package service provides the unified service facade for QuicBaaS.
// This file implements multi-tenant object storage isolation. When a tenantID
// is present in the request context, all object and bucket operations are
// scoped to that tenant's namespace. When tenantID is empty, the system
// operates in single-tenant mode with no behavioral changes (full backward
// compatibility).
package service

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// osTenantPrefix is the storage prefix for tenant-scoped object keys.
const osTenantPrefix = "tenant/"

// osTenantSeparator follows the tenant ID in the object storage namespace.
const osTenantSeparator = "/"

// osTenantScopedKey returns a tenant-scoped object key if tenantID is non-empty.
// For single-tenant mode (empty tenantID), the key is returned as-is.
// Format: "tenant/{tenantID}/{key}".
func osTenantScopedKey(tenantID, key string) string {
	if tenantID == "" {
		return key
	}
	return osTenantPrefix + tenantID + osTenantSeparator + key
}

// osTenantStripKey removes the tenant prefix from an object key.
// If the key doesn't have a tenant prefix, it is returned as-is.
func osTenantStripKey(tenantID, key string) string {
	if tenantID == "" {
		return key
	}
	prefix := osTenantPrefix + tenantID + osTenantSeparator
	return strings.TrimPrefix(key, prefix)
}

// osTenantKeyBelongsTo checks if an object key belongs to the given tenant.
// For single-tenant mode (empty tenantID), all keys belong.
// For multi-tenant mode, the key must have the matching tenant prefix.
func osTenantKeyBelongsTo(tenantID, key string) bool {
	if tenantID == "" {
		return true
	}
	prefix := osTenantPrefix + tenantID + osTenantSeparator
	return strings.HasPrefix(key, prefix)
}

// osTenantListPrefix returns the prefix to use when listing objects for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "tenant/{tenantID}/".
func osTenantListPrefix(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return osTenantPrefix + tenantID + osTenantSeparator
}

// osTenantScopedBucket returns a tenant-scoped bucket name if tenantID is non-empty.
// For single-tenant mode (empty tenantID), the bucket name is returned as-is.
// Format: "tenant/{tenantID}/{bucket}".
func osTenantScopedBucket(tenantID, bucket string) string {
	if tenantID == "" {
		return bucket
	}
	return osTenantPrefix + tenantID + osTenantSeparator + bucket
}

// osTenantStripBucket removes the tenant prefix from a bucket name.
// If the bucket name doesn't have a tenant prefix, it is returned as-is.
func osTenantStripBucket(tenantID, bucket string) string {
	if tenantID == "" {
		return bucket
	}
	prefix := osTenantPrefix + tenantID + osTenantSeparator
	return strings.TrimPrefix(bucket, prefix)
}

// osTenantBucketBelongsTo checks if a bucket name belongs to the given tenant.
// For single-tenant mode (empty tenantID), all buckets belong.
// For multi-tenant mode, the bucket must have the matching tenant prefix.
func osTenantBucketBelongsTo(tenantID, bucket string) bool {
	if tenantID == "" {
		return true
	}
	prefix := osTenantPrefix + tenantID + osTenantSeparator
	return strings.HasPrefix(bucket, prefix)
}

// osEnforceTenantAccess validates that the requesting tenant has access to the object.
// Returns nil if access is allowed, or an error if the tenant doesn't own the object.
func osEnforceTenantAccess(tenantID, key, operation string) error {
	if tenantID == "" {
		return nil // Single-tenant mode, no access control
	}
	if !osTenantKeyBelongsTo(tenantID, key) {
		return NewError(errors.ErrCrossTenantAccess, "objectstorage."+operation,
			"object does not belong to the requesting tenant")
	}
	return nil
}

// osEnforceBucketTenantAccess validates that the requesting tenant has access to the bucket.
// Returns nil if access is allowed, or an error if the tenant doesn't own the bucket.
func osEnforceBucketTenantAccess(tenantID, bucket, operation string) error {
	if tenantID == "" {
		return nil // Single-tenant mode, no access control
	}
	if !osTenantBucketBelongsTo(tenantID, bucket) {
		return NewError(errors.ErrCrossTenantAccess, "objectstorage."+operation,
			"bucket does not belong to the requesting tenant")
	}
	return nil
}

// osPrepareTenantContext extracts and validates the tenant ID from context.
// Returns the validated tenant ID or an error if the tenant ID is invalid.
func osPrepareTenantContext(ctx context.Context) (string, error) {
	tenantID := extractTenantID(ctx)
	if err := validateTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}
