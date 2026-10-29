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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
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
// belong to tenantID, each ID returned in the tenant's own namespace. A key
// result is kept only when the tenant's ownership record names it, as in
// ListKeys; DEK results, which carry no record, go by namespace. The system
// caller keeps every tenant's results.
func (k *kmsServiceImpl) safetyResultsForTenant(ctx context.Context, tenantID, operation string, results []SafetyCheckResult) ([]SafetyCheckResult, error) {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return results, nil
	}
	owned := make([]SafetyCheckResult, 0, len(results))
	for _, r := range results {
		if !tenantKeyBelongsTo(tenantID, r.ID) {
			continue
		}
		if r.Type == "key" {
			ok, err := k.tenantOwnsKey(ctx, tenantID, r.ID, operation)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		r.ID = tenantStripKeyID(tenantID, r.ID)
		owned = append(owned, r)
	}
	return owned, nil
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

// kmsOwnerRecordSeparator splits an ownership record's value into the owning
// tenant and the holder of the create request that wrote it. Tenant IDs
// never contain it (see isValidTenantChar).
const kmsOwnerRecordSeparator = ":"

// kmsOwnerHolderBytes is the size of the random holder in an ownership record.
const kmsOwnerHolderBytes = 16

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

// KeyOwnershipStoreNotAtomicError reports that the ownership store cannot
// reserve a key name atomically, so two creates of one name could both
// believe they own it.
type KeyOwnershipStoreNotAtomicError struct {
	StoreType string
}

func (e *KeyOwnershipStoreNotAtomicError) Error() string {
	return fmt.Sprintf("KMS key ownership store %s does not support atomic reservations", e.StoreType)
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

// atomicOwnerStore returns the ownership store as an AtomicKVStore. Claims
// and releases are compare-and-swap operations, so a store that cannot do
// them atomically is refused rather than raced.
func (k *kmsServiceImpl) atomicOwnerStore(operation string) (qrdbsdk.AtomicKVStore, error) {
	store, err := k.ownerStore(operation)
	if err != nil {
		return nil, err
	}
	atomicStore, ok := store.(qrdbsdk.AtomicKVStore)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "kms." + operation,
			Err:  &KeyOwnershipStoreNotAtomicError{StoreType: fmt.Sprintf("%T", store)},
		}
	}
	return atomicStore, nil
}

// readKeyOwner returns the tenant recorded as the owner of scopedID, or ""
// when there is no record.
func (k *kmsServiceImpl) readKeyOwner(ctx context.Context, scopedID, operation string) (string, error) {
	record, err := k.readKeyOwnerRecord(ctx, scopedID, operation)
	if err != nil {
		return "", err
	}
	tenantID, _, _ := strings.Cut(string(record), kmsOwnerRecordSeparator)
	return tenantID, nil
}

// readKeyOwnerRecord returns the raw ownership record of a scoped key ID, or
// nil when the key has none.
func (k *kmsServiceImpl) readKeyOwnerRecord(ctx context.Context, scopedID, operation string) ([]byte, error) {
	store, err := k.ownerStore(operation)
	if err != nil {
		return nil, err
	}
	record, err := store.Get(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(scopedID))
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, WrapError(err, errors.ErrStorage, "kms."+operation, "failed to read key ownership record")
	}
	return record, nil
}

// tenantKeyClaim is one create request's reservation of a key name. Its
// value names the tenant and a random holder unique to the request, so a
// rollback deletes the record only while it is still this request's.
type tenantKeyClaim struct {
	store qrdbsdk.AtomicKVStore
	key   string
	value []byte
}

// newTenantKeyClaimHolder returns a random holder that tells one create
// request's claim apart from any other's.
func newTenantKeyClaimHolder() (string, error) {
	buf := make([]byte, kmsOwnerHolderBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// claimTenantKey atomically reserves the ownership record for a key a
// tenant is about to create. It must run before the key is created; when
// creation fails the caller passes the claim to abandonTenantKeyClaim. A
// name that already holds a key or a record is refused. The system tenant
// claims nothing and gets a nil claim.
func (k *kmsServiceImpl) claimTenantKey(ctx context.Context, tenantID, name, operation string) (*tenantKeyClaim, error) {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil, nil
	}
	op := "kms." + operation
	scopedID := tenantScopedKeyID(tenantID, name)

	// A key that already exists under the scoped name is never claimed: it
	// is either this tenant's own key or one the tenant does not own, such
	// as a legacy system key in the tenant namespace.
	if existing, err := k.kmsManager.GetKey(ctx, scopedID); err == nil && existing != nil {
		owner, ownerErr := k.readKeyOwner(ctx, scopedID, operation)
		if ownerErr != nil {
			return nil, ownerErr
		}
		if owner == tenantID {
			return nil, NewErrorf(errors.ErrAlreadyExists, op, "key %q already exists", name)
		}
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   op,
			Err:  &KeyOwnershipError{TenantID: tenantID, Key: name},
		}
	}

	atomicStore, err := k.atomicOwnerStore(operation)
	if err != nil {
		return nil, err
	}
	holder, err := newTenantKeyClaimHolder()
	if err != nil {
		return nil, WrapError(err, errors.ErrInternal, op, "failed to generate key ownership claim holder")
	}
	claim := &tenantKeyClaim{
		store: atomicStore,
		key:   kmsOwnerRecordKey(scopedID),
		value: []byte(tenantID + kmsOwnerRecordSeparator + holder),
	}
	stored, err := atomicStore.PutIfAbsent(tenantctx.WithSystem(ctx), claim.key, claim.value)
	if err != nil {
		return nil, WrapError(err, errors.ErrStorage, op, "failed to write key ownership record")
	}
	if !stored {
		return nil, NewErrorf(errors.ErrAlreadyExists, op, "key %q already exists", name)
	}
	return claim, nil
}

// releaseTenantKey deletes the ownership record a delete resolved, only if
// the store still holds that exact record. A nil record (a system-tenant key)
// releases nothing.
func (k *kmsServiceImpl) releaseTenantKey(ctx context.Context, scopedID string, record []byte, operation string) error {
	if record == nil {
		return nil
	}
	atomicStore, err := k.atomicOwnerStore(operation)
	if err != nil {
		return err
	}
	// A lost compare is not an error: the record now belongs to a newer
	// key of the same name, created after this delete resolved its own.
	if _, err := atomicStore.CompareAndDelete(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(scopedID), record); err != nil {
		return WrapError(err, errors.ErrStorage, "kms."+operation, "failed to delete key ownership record")
	}
	return nil
}

// resolveDeletedKey resolves a key for DeleteKey and returns the ownership
// record it read alongside the scoped ID. The record names the key's
// generation, so the delete releases exactly that record and never one a
// later create wrote for a new key of the same name. A system-tenant caller
// deleting a key in a tenant namespace gets that tenant's record too, so the
// tenant can create the name again; the system tenant's own keys have none.
func (k *kmsServiceImpl) resolveDeletedKey(ctx context.Context, tenantID, name, operation string) (string, []byte, error) {
	scopedID := tenantScopedKeyID(tenantID, name)
	if qrdbsdk.IsSystemTenant(tenantID) && !strings.HasPrefix(scopedID, kmsTenantKeyPrefix) {
		return scopedID, nil, nil
	}
	record, err := k.readKeyOwnerRecord(ctx, scopedID, operation)
	if err != nil {
		return "", nil, err
	}
	if qrdbsdk.IsSystemTenant(tenantID) {
		return scopedID, record, nil
	}
	owner, _, _ := strings.Cut(string(record), kmsOwnerRecordSeparator)
	if record == nil || owner != tenantID {
		return "", nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "kms." + operation,
			Err:  &KeyOwnershipError{TenantID: tenantID, Key: name},
		}
	}
	return scopedID, record, nil
}

// abandonTenantKeyClaim releases the claim of a key whose creation failed
// and returns the creation error. The record is deleted only if it still
// holds this request's value, so a rollback never erases another request's
// record. A failed release is logged, because the creation error is the one
// the caller needs.
func (k *kmsServiceImpl) abandonTenantKeyClaim(ctx context.Context, claim *tenantKeyClaim, operation string, createErr error) error {
	if claim == nil {
		return createErr
	}
	deleted, err := claim.store.CompareAndDelete(tenantctx.WithSystem(ctx), claim.key, claim.value)
	if err != nil || !deleted {
		if k.logger != nil {
			k.logger.Error("failed to release key ownership record after a failed create",
				"operation", operation, "record", claim.key, "released", deleted, "error", err)
		}
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

// ownedListedKeys keeps the entries of a KMS manager key listing that
// tenantID owns: each must sit in the tenant's namespace and carry the
// tenant's ownership record, as in ListKeys. An entry whose key ID cannot be
// read is not the tenant's. The system caller keeps every entry.
func (k *kmsServiceImpl) ownedListedKeys(ctx context.Context, tenantID, operation string, keys []interface{}) ([]interface{}, error) {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return keys, nil
	}
	owned := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		keyID, ok := listedKeyID(key)
		if !ok || !tenantKeyBelongsTo(tenantID, keyID) {
			continue
		}
		ownsKey, err := k.tenantOwnsKey(ctx, tenantID, keyID, operation)
		if err != nil {
			return nil, err
		}
		if ownsKey {
			owned = append(owned, key)
		}
	}
	return owned, nil
}

// listedKeyID returns the key ID of one entry of a KMS manager key listing.
func listedKeyID(key interface{}) (string, bool) {
	switch meta := key.(type) {
	case *kms.KeyMetadata:
		return meta.KeyID, true
	case *KeyMetadata:
		return meta.KeyID, true
	default:
		return "", false
	}
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
