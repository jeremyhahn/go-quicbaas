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
// This file implements multi-tenant KMS isolation. When a tenantID is present
// in the request context, all KMS key operations are scoped to that tenant's
// namespace. When tenantID is empty, the system operates in single-tenant
// mode with no behavioral changes (full backward compatibility).
package service

import (
	"context"
	"fmt"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// kmsTenantKeyPrefix is the storage prefix for tenant-scoped KMS keys. A
// scoped key ID is passed to the XKMS backend as the keyname of a key
// reference, which allows only a-z, A-Z, 0-9, '-', '_' and '.', so the
// namespace is delimited with '.'. A tenant ID never contains '.'
// (isValidTenantChar), so the first '.' after the prefix ends the tenant ID.
const kmsTenantKeyPrefix = "tenant."

// kmsTenantKeySuffix follows the tenant ID in the key namespace.
const kmsTenantKeySuffix = "."

// tenantScopedKeyID returns a tenant-scoped key ID if tenantID is non-empty.
// For single-tenant mode (empty tenantID) or the system tenant, the key ID is
// returned as-is: a system-tenant caller is platform-scoped, not isolated
// into a namespace literally named "system".
// Format: "tenant.{tenantID}.{keyID}".
func tenantScopedKeyID(tenantID, keyID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return keyID
	}
	return kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix + keyID
}

// tenantStripKeyID removes the tenant prefix from a key ID.
// If the key ID doesn't have a tenant prefix, it is returned as-is.
func tenantStripKeyID(tenantID, keyID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return keyID
	}
	prefix := kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix
	return strings.TrimPrefix(keyID, prefix)
}

// tenantKeyBelongsTo checks if a key ID belongs to the given tenant.
// For single-tenant mode (empty tenantID) or the system tenant, all keys
// belong (no isolation applies to the platform scope).
// For multi-tenant mode, the key must have the matching tenant prefix.
func tenantKeyBelongsTo(tenantID, keyID string) bool {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return true
	}
	prefix := kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix
	return strings.HasPrefix(keyID, prefix)
}

// ReservedKeyNameError is returned when a caller names a new key or DEK
// with the reserved tenant prefix. The system scope stores a name
// unscoped, so "tenant.acme.k" would otherwise create a key that tenant
// acme lists and owns.
type ReservedKeyNameError struct {
	// Name is the name the caller supplied.
	Name string
}

// Error returns a human-readable description.
func (e *ReservedKeyNameError) Error() string {
	return fmt.Sprintf("service: key name %q uses the reserved tenant namespace %q", e.Name, kmsTenantKeyPrefix)
}

// rejectReservedKeyName refuses a caller-chosen name for a new key or DEK
// that starts with kmsTenantKeyPrefix, in every scope, so a stored name
// is attributed to exactly the tenant that created it. Every verb that
// creates a name (GenerateKey, ImportKey, UnwrapKey, CreateDEK) calls it.
func rejectReservedKeyName(name, operation string) error {
	if !strings.HasPrefix(name, kmsTenantKeyPrefix) {
		return nil
	}
	return &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "kms." + operation,
		Err:  &ReservedKeyNameError{Name: name},
	}
}

// safetyResultsForTenant keeps the shared safety tracker's results that
// belong to tenantID, each ID returned in the tenant's own namespace. The
// system caller keeps every tenant's results.
func safetyResultsForTenant(tenantID string, results []SafetyCheckResult) []SafetyCheckResult {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return results
	}
	owned := make([]SafetyCheckResult, 0, len(results))
	for _, r := range results {
		if !tenantKeyBelongsTo(tenantID, r.ID) {
			continue
		}
		r.ID = tenantStripKeyID(tenantID, r.ID)
		owned = append(owned, r)
	}
	return owned
}

// tenantListPrefix returns the prefix to use when listing keys for a tenant.
// For single-tenant mode or the system tenant, returns empty string (list all).
// For multi-tenant mode, returns "tenant.{tenantID}.".
func tenantListPrefix(tenantID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return ""
	}
	return kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix
}

// validateTenantID validates that a tenant ID contains only safe characters.
// Empty tenant ID is valid (single-tenant mode).
func validateTenantID(tenantID string) error {
	if tenantID == "" {
		return nil
	}
	// Tenant IDs must be alphanumeric with hyphens and underscores, 1-128 chars
	if len(tenantID) > 128 {
		return NewError(errors.ErrInvalidInput, "kms.validateTenantID", "tenant ID exceeds maximum length of 128 characters")
	}
	for _, ch := range tenantID {
		if !isValidTenantChar(ch) {
			return NewError(errors.ErrInvalidInput, "kms.validateTenantID",
				"tenant ID contains invalid character; only alphanumeric, hyphens, and underscores are allowed")
		}
	}
	return nil
}

// isValidTenantChar returns true if the character is allowed in tenant IDs.
func isValidTenantChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '-' || ch == '_'
}

// enforceTenantAccess validates that the requesting tenant has access to the key.
// Returns nil if access is allowed, or an error if the tenant doesn't own the key.
func enforceTenantAccess(tenantID, keyID, operation string) error {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil // Single-tenant mode / system tenant, no access control
	}
	if !tenantKeyBelongsTo(tenantID, keyID) {
		return NewError(errors.ErrCrossTenantAccess, "kms."+operation,
			"key does not belong to the requesting tenant")
	}
	return nil
}

// kmsOwnerRecordPrefix is where the system scope records which tenant owns
// each tenant-namespaced key. The record, not the name, is the proof of
// ownership: a key created by the system tenant may carry a "tenant." name
// (written before rejectReservedKeyName existed), and it has no record.
const kmsOwnerRecordPrefix = "kms/owner/"

// KeyOwnershipError reports that a tenant asked for a key it does not own:
// the key has no ownership record, or the record names another tenant.
type KeyOwnershipError struct {
	// TenantID is the requesting tenant.
	TenantID string
	// Key is the key name the tenant supplied.
	Key string
}

func (e *KeyOwnershipError) Error() string {
	return fmt.Sprintf("tenant %q does not own KMS key %q", e.TenantID, e.Key)
}

// KeyOwnershipStoreUnsetError reports that a tenant key operation ran
// without the durable store that holds ownership records.
type KeyOwnershipStoreUnsetError struct{}

func (e *KeyOwnershipStoreUnsetError) Error() string {
	return "KMS key ownership store is not configured"
}

// kmsOwnerRecordKey returns the system-scope key of the ownership record
// for a tenant-scoped key ID.
func kmsOwnerRecordKey(scopedID string) string {
	return kmsOwnerRecordPrefix + scopedID
}

// ownerStore returns the ownership store, or a typed error when none is set.
func (k *kmsServiceImpl) ownerStore(operation string) (interfaces.KVStore, error) {
	if k.kvStore == nil {
		return nil, &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "kms." + operation,
			Err:  &KeyOwnershipStoreUnsetError{},
		}
	}
	return k.kvStore, nil
}

// readKeyOwner returns the tenant recorded as the owner of scopedID, or ""
// when there is no record.
func (k *kmsServiceImpl) readKeyOwner(ctx context.Context, scopedID, operation string) (string, error) {
	store, err := k.ownerStore(operation)
	if err != nil {
		return "", err
	}
	owner, err := store.Get(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(scopedID))
	if err != nil {
		if errors.IsNotFound(err) {
			return "", nil
		}
		return "", WrapError(err, errors.ErrStorage, "kms."+operation, "failed to read key ownership record")
	}
	return string(owner), nil
}

// claimTenantKey writes the ownership record for a key a tenant is about to
// create. It must run before the key is created; when creation fails the
// caller calls releaseTenantKey. The system tenant claims nothing.
func (k *kmsServiceImpl) claimTenantKey(ctx context.Context, tenantID, name, operation string) error {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil
	}
	scopedID := tenantScopedKeyID(tenantID, name)
	owner, err := k.readKeyOwner(ctx, scopedID, operation)
	if err != nil {
		return err
	}
	if owner != "" {
		return NewErrorf(errors.ErrAlreadyExists, "kms."+operation, "key %q already exists", name)
	}
	if err := k.kvStore.Put(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(scopedID), []byte(tenantID)); err != nil {
		return WrapError(err, errors.ErrStorage, "kms."+operation, "failed to write key ownership record")
	}
	return nil
}

// releaseTenantKey deletes the ownership record of a tenant key, after the
// key is deleted or when its creation failed. The system tenant has none.
func (k *kmsServiceImpl) releaseTenantKey(ctx context.Context, tenantID, name, operation string) error {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil
	}
	store, err := k.ownerStore(operation)
	if err != nil {
		return err
	}
	if err := store.Delete(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(tenantScopedKeyID(tenantID, name))); err != nil && !errors.IsNotFound(err) {
		return WrapError(err, errors.ErrStorage, "kms."+operation, "failed to delete key ownership record")
	}
	return nil
}

// abandonTenantKeyClaim releases the claim of a key whose creation failed
// and returns the creation error. A failed release is logged, because the
// creation error is the one the caller needs. When the key exists anyway, a
// concurrent create of the same name won, and the record is that key's.
func (k *kmsServiceImpl) abandonTenantKeyClaim(ctx context.Context, tenantID, name, operation string, createErr error) error {
	if _, err := k.kmsManager.GetKey(ctx, tenantScopedKeyID(tenantID, name)); err == nil {
		return createErr
	}
	if err := k.releaseTenantKey(ctx, tenantID, name, operation); err != nil && k.logger != nil {
		k.logger.Error("failed to release key ownership record after a failed create",
			"operation", operation, "key", name, "error", err)
	}
	return createErr
}

// resolveTenantKey returns the scoped key ID for a tenant's key name after
// checking that the tenant owns it. A key without a matching ownership
// record is refused before the KMS manager is reached. The system tenant
// resolves every name unchecked.
func (k *kmsServiceImpl) resolveTenantKey(ctx context.Context, tenantID, name, operation string) (string, error) {
	scopedID := tenantScopedKeyID(tenantID, name)
	if qrdbsdk.IsSystemTenant(tenantID) {
		return scopedID, nil
	}
	owner, err := k.readKeyOwner(ctx, scopedID, operation)
	if err != nil {
		return "", err
	}
	if owner != tenantID {
		return "", &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "kms." + operation,
			Err:  &KeyOwnershipError{TenantID: tenantID, Key: name},
		}
	}
	return scopedID, nil
}

// tenantOwnsKey reports whether a tenant's ownership record exists for a
// scoped key ID. List uses it to hide keys the tenant does not own.
func (k *kmsServiceImpl) tenantOwnsKey(ctx context.Context, tenantID, scopedID, operation string) (bool, error) {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return true, nil
	}
	owner, err := k.readKeyOwner(ctx, scopedID, operation)
	if err != nil {
		return false, err
	}
	return owner == tenantID, nil
}

// prepareTenantContext reads the request's tenant (tenantctx.FromContext,
// which refuses a context with no tenant bound) and validates it.
//
// It returns the tenant in the services' own convention, where "" is the
// platform scope: an explicit system binding maps to "", which is what the
// tenant-scoping helpers and the downstream stores (an xkms SignCSR with no
// tenant SAN, an unprefixed key or secret) have always taken for the system.
// What changed in go-quicbaas#1150 is only that the empty result now has to
// be asked for by binding the system tenant, never produced by the absence
// of a binding.
func prepareTenantContext(ctx context.Context) (string, error) {
	tenantID, err := tenantctx.FromContext(ctx, "service.tenant")
	if err != nil {
		return "", err
	}
	if tenantctx.IsSystem(tenantID) {
		return "", nil
	}
	if err := validateTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}
