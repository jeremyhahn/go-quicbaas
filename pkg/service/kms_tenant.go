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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

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
// tenant, the holder of the create request that wrote it, and the claim time
// in UTC unix nanoseconds. Tenant IDs
// never contain it (see isValidTenantChar).
const kmsOwnerRecordSeparator = ":"

// kmsOwnerHolderBytes is the size of the random holder in an ownership record.
const kmsOwnerHolderBytes = 16

// kmsClaimCleanupTimeout bounds the release of a claim after a failed
// create. The release runs detached from the request, so a cancelled request
// still frees the name.
const kmsClaimCleanupTimeout = 5 * time.Second

// kmsClaimStaleAfter is the age past which a claim with no key behind it is
// treated as left by a failed cleanup and reclaimed. It is far longer than
// any key creation, so a live create never loses its claim.
const kmsClaimStaleAfter = 10 * time.Minute

// kmsOwnerRecordCompleted replaces the claim time in an ownership record once
// the key it reserves has been created. A completed record proves ownership
// of a live key and is never reclaimed, whatever its age: a backend that
// reports a live key as not found, as during an outage, cannot strip the
// owner of its key. Only a pending record, one whose create never finished,
// can go stale.
const kmsOwnerRecordCompleted = "completed"

// kmsOwnerRecordDeleting replaces the state of an ownership record before
// the service deletes its key. It records the intent ahead of the backend
// delete, so a delete whose finalization never ran (a cancelled request, a
// failed release, a crash) leaves a record its retry can release: a later
// not-found on a record already in this state releases it, and a create
// that finds the name free reclaims it. A delete that fails, or meets an
// unconfirmed not-found on a record it marked itself, restores the record.
const kmsOwnerRecordDeleting = "deleting"

// kmsClaimCompleteAttempts bounds the writes that mark a claim completed. A
// create reports success only once its record is completed, so a transient
// store fault is retried before the create gives up and rolls its key back.
const kmsClaimCompleteAttempts = 3

// KeyOwnershipRecordMalformedError reports an ownership record that does not
// parse as tenant, holder and claim time. It is never reclaimed.
type KeyOwnershipRecordMalformedError struct {
	Record string
}

func (e *KeyOwnershipRecordMalformedError) Error() string {
	return fmt.Sprintf("service: malformed key ownership record %q", e.Record)
}

// KeyOwnershipRecordChangedError reports an ownership record that another
// request replaced while this one was moving it to a new state.
type KeyOwnershipRecordChangedError struct {
	Record string
}

func (e *KeyOwnershipRecordChangedError) Error() string {
	return fmt.Sprintf("service: key ownership record %q changed concurrently", e.Record)
}

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

// atomicOwnerStore returns the ownership store as a GuardedKVStore. Claims
// and releases are compare-and-swap operations, so a store that cannot do
// them atomically is refused rather than raced.
func (k *kmsServiceImpl) atomicOwnerStore(operation string) (interfaces.GuardedKVStore, error) {
	store, err := k.ownerStore(operation)
	if err != nil {
		return nil, err
	}
	atomicStore, ok := store.(interfaces.GuardedKVStore)
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
	store    interfaces.GuardedKVStore
	scopedID string
	key      string
	value    []byte
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
	existing, err := k.kmsManager.GetKey(ctx, scopedID)
	if err != nil && !stderrors.Is(err, kms.ErrKeyNotFound) {
		// Only a confirmed not-found proves the name is free. Any other
		// lookup failure, such as a backend outage, may hide a live key,
		// so no record is claimed or reclaimed on it.
		return nil, WrapError(err, errors.ErrStorage, op, "failed to look up key before claiming its name")
	}
	if err == nil && existing != nil {
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
		store:    atomicStore,
		scopedID: scopedID,
		key:      kmsOwnerRecordKey(scopedID),
		value: []byte(tenantID + kmsOwnerRecordSeparator + holder + kmsOwnerRecordSeparator +
			strconv.FormatInt(k.now().UTC().UnixNano(), 10)),
	}
	stored, err := atomicStore.PutIfAbsent(tenantctx.WithSystem(ctx), claim.key, claim.value)
	if err != nil {
		return nil, WrapError(err, errors.ErrStorage, op, "failed to write key ownership record")
	}
	if stored {
		return claim, nil
	}

	// The name is reserved but has no key behind it (checked above). A
	// claim whose failed create could not release it is reclaimed once it
	// is older than any create could take; a fresh one is a create in flight.
	reclaimed, err := k.reclaimStaleTenantKeyClaim(ctx, atomicStore, claim.key, op)
	if err != nil {
		return nil, err
	}
	if reclaimed {
		stored, err = atomicStore.PutIfAbsent(tenantctx.WithSystem(ctx), claim.key, claim.value)
		if err != nil {
			return nil, WrapError(err, errors.ErrStorage, op, "failed to write key ownership record")
		}
		if stored {
			return claim, nil
		}
	}
	return nil, NewErrorf(errors.ErrAlreadyExists, op, "key %q already exists", name)
}

// reclaimStaleTenantKeyClaim removes the ownership record at recordKey when
// its claim time is older than kmsClaimStaleAfter, comparing on exactly the
// value it read. It reports whether the record was removed. A malformed
// record is never removed.
func (k *kmsServiceImpl) reclaimStaleTenantKeyClaim(ctx context.Context, store interfaces.GuardedKVStore, recordKey, op string) (bool, error) {
	record, err := store.Get(tenantctx.WithSystem(ctx), recordKey)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, WrapError(err, errors.ErrStorage, op, "failed to read key ownership record")
	}
	if !tenantKeyRecordDeleting(record) {
		claimedAt, completed, err := tenantKeyClaimTime(record)
		if err != nil {
			return false, WrapError(err, errors.ErrStorage, op, "failed to parse key ownership record")
		}
		if completed || k.now().Sub(claimedAt) < kmsClaimStaleAfter {
			return false, nil
		}
	}
	deleted, err := store.CompareAndDelete(tenantctx.WithSystem(ctx), recordKey, record)
	if err != nil {
		return false, WrapError(err, errors.ErrStorage, op, "failed to delete stale key ownership record")
	}
	if deleted && k.logger != nil {
		k.logger.Warn("reclaimed a stale key ownership record with no key", "operation", op, "record", recordKey)
	}
	return deleted, nil
}

// tenantKeyClaimTime parses the claim time of an ownership record of the form
// tenant, holder and UTC unix nanoseconds, joined with kmsOwnerRecordSeparator.
// It reports completed, with no time, when the last field is
// kmsOwnerRecordCompleted.
func tenantKeyClaimTime(record []byte) (claimedAt time.Time, completed bool, err error) {
	fields := strings.Split(string(record), kmsOwnerRecordSeparator)
	if len(fields) != 3 || fields[0] == "" || fields[1] == "" {
		return time.Time{}, false, &KeyOwnershipRecordMalformedError{Record: string(record)}
	}
	if fields[2] == kmsOwnerRecordCompleted {
		return time.Time{}, true, nil
	}
	nanos, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return time.Time{}, false, &KeyOwnershipRecordMalformedError{Record: string(record)}
	}
	return time.Unix(0, nanos).UTC(), false, nil
}

// tenantKeyRecordDeleting reports whether record carries the intent of a
// delete of its key, written before the backend delete was issued.
func tenantKeyRecordDeleting(record []byte) bool {
	fields := strings.Split(string(record), kmsOwnerRecordSeparator)
	return len(fields) == 3 && fields[0] != "" && fields[1] != "" && fields[2] == kmsOwnerRecordDeleting
}

// tenantKeyRecordWithState returns record, keeping its owner and holder,
// with its claim time or state replaced by state.
func tenantKeyRecordWithState(record []byte, state string) []byte {
	tenantID, rest, _ := strings.Cut(string(record), kmsOwnerRecordSeparator)
	holder, _, _ := strings.Cut(rest, kmsOwnerRecordSeparator)
	return []byte(tenantID + kmsOwnerRecordSeparator + holder + kmsOwnerRecordSeparator + state)
}

// markTenantKeyDeleting records the intent to delete the key before the
// backend delete runs, and returns the record the delete then finalizes.
// marked reports whether this call wrote the intent, so the caller knows to
// restore the record if the delete does not go through. A nil record, as a
// system key has, has nothing to mark, and a record already carrying the
// intent, left by an earlier delete, is returned as it is. No key has been
// touched yet, so the request's own context bounds the write.
func (k *kmsServiceImpl) markTenantKeyDeleting(ctx context.Context, scopedID string, record []byte, operation string) (intent []byte, marked bool, err error) {
	if record == nil || tenantKeyRecordDeleting(record) {
		return record, false, nil
	}
	atomicStore, err := k.atomicOwnerStore(operation)
	if err != nil {
		return nil, false, err
	}
	intent = tenantKeyRecordWithState(record, kmsOwnerRecordDeleting)
	swapped, err := atomicStore.CompareAndSwap(tenantctx.WithSystem(ctx), kmsOwnerRecordKey(scopedID), record, intent)
	if err != nil {
		return nil, false, WrapError(err, errors.ErrStorage, "kms."+operation, "failed to mark key ownership record for deletion")
	}
	if !swapped {
		return nil, false, &errors.QBError{
			Code: errors.ErrConflict,
			Op:   "kms." + operation,
			Err:  &KeyOwnershipRecordChangedError{Record: kmsOwnerRecordKey(scopedID)},
		}
	}
	return intent, true, nil
}

func (k *kmsServiceImpl) now() time.Time {
	if k.clock != nil {
		return k.clock()
	}
	return time.Now()
}

// restoreTenantKeyRecord puts back the record a delete marked when the key
// may still exist: the backend delete failed, or its not-found is not
// confirmed. It compares on the intent, so a record another request has
// since replaced is left alone. It runs detached from the request, bounded,
// and a failed restore is logged: the delete's own error is the one the
// caller needs, and the intent still names the owner.
func (k *kmsServiceImpl) restoreTenantKeyRecord(ctx context.Context, scopedID string, intent, original []byte, operation string) {
	atomicStore, err := k.atomicOwnerStore(operation)
	if err != nil {
		return
	}
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), kmsClaimCleanupTimeout)
	defer cancel()
	swapped, err := atomicStore.CompareAndSwap(tenantctx.WithSystem(restoreCtx), kmsOwnerRecordKey(scopedID), intent, original)
	if (err != nil || !swapped) && k.logger != nil {
		k.logger.Error("failed to restore key ownership record after an unfinished delete",
			"operation", operation, "record", kmsOwnerRecordKey(scopedID), "restored", swapped, "error", err)
	}
}

// releaseTenantKey deletes the ownership record of a deleted key, provided
// the store still holds that exact record. A nil record (a system-tenant key)
// releases nothing. The backend delete has already happened, so the release
// runs detached from the request, bounded: a request cancelled after the
// delete still frees the name, and a release that fails anyway leaves the
// intent for the retry to release.
func (k *kmsServiceImpl) releaseTenantKey(ctx context.Context, scopedID string, record []byte, operation string) error {
	if record == nil {
		return nil
	}
	atomicStore, err := k.atomicOwnerStore(operation)
	if err != nil {
		return err
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), kmsClaimCleanupTimeout)
	defer cancel()
	// A lost compare is not an error: the record now belongs to a newer
	// key of the same name, created after this delete resolved its own.
	if _, err := atomicStore.CompareAndDelete(tenantctx.WithSystem(releaseCtx), kmsOwnerRecordKey(scopedID), record); err != nil {
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
	// The create may have failed because the request was cancelled, and a
	// cancelled context would refuse the release too, leaving the name
	// reserved with no key. The rollback outlives the request, bounded.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), kmsClaimCleanupTimeout)
	defer cancel()
	deleted, err := claim.store.CompareAndDelete(tenantctx.WithSystem(cleanupCtx), claim.key, claim.value)
	if err != nil || !deleted {
		if k.logger != nil {
			k.logger.Error("failed to release key ownership record after a failed create",
				"operation", operation, "record", claim.key, "released", deleted, "error", err)
		}
	}
	return createErr
}

// completeTenantKeyClaim marks the claim's ownership record completed once
// its key has been created, so the record is never reclaimed as stale. A
// create may report success only on a completed record: a pending one whose
// key exists is reclaimed once old by a create whose backend lookup folds an
// outage into not-found, and the tenant loses its live key. So the mark is
// retried, bounded, and if it still fails the create is rolled back and the
// error returned. Like the rollback, the mark outlives a request cancelled
// after the create.
func (k *kmsServiceImpl) completeTenantKeyClaim(ctx context.Context, claim *tenantKeyClaim, operation string) error {
	if claim == nil {
		return nil
	}
	completed := tenantKeyRecordWithState(claim.value, kmsOwnerRecordCompleted)
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), kmsClaimCleanupTimeout)
	defer cancel()
	if err := markTenantKeyClaimCompleted(markCtx, claim, completed, operation); err != nil {
		return k.rollBackTenantKey(ctx, claim, operation, err)
	}
	claim.value = completed
	return nil
}

// markTenantKeyClaimCompleted swaps the claim's pending record for
// completed, retrying a store fault up to kmsClaimCompleteAttempts times.
// A lost compare is success when the store already holds completed, as when
// an earlier attempt applied but its reply was lost; otherwise another
// request replaced the record and the claim is lost.
func markTenantKeyClaimCompleted(ctx context.Context, claim *tenantKeyClaim, completed []byte, operation string) error {
	op := "kms." + operation
	var lastErr error
	for attempt := 0; attempt < kmsClaimCompleteAttempts && ctx.Err() == nil; attempt++ {
		swapped, err := claim.store.CompareAndSwap(tenantctx.WithSystem(ctx), claim.key, claim.value, completed)
		if err != nil {
			lastErr = err
			continue
		}
		if swapped {
			return nil
		}
		stored, err := claim.store.Get(tenantctx.WithSystem(ctx), claim.key)
		if err != nil && !errors.IsNotFound(err) {
			lastErr = err
			continue
		}
		if err == nil && bytes.Equal(stored, completed) {
			return nil
		}
		return &errors.QBError{
			Code: errors.ErrConflict,
			Op:   op,
			Err:  &KeyOwnershipRecordChangedError{Record: claim.key},
		}
	}
	if lastErr == nil {
		lastErr = ctx.Err()
	}
	return WrapError(lastErr, errors.ErrStorage, op, "failed to mark key ownership record completed")
}

// rollBackTenantKey undoes a create whose ownership record could not be
// completed, and returns cause. The key is deleted first and the claim
// released only once that delete succeeded: a key that survives keeps its
// pending claim, so it stays its tenant's to delete rather than becoming a
// key nobody owns. The rollback runs detached from the request, bounded.
func (k *kmsServiceImpl) rollBackTenantKey(ctx context.Context, claim *tenantKeyClaim, operation string, cause error) error {
	deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), kmsClaimCleanupTimeout)
	defer cancel()
	if err := k.kmsManager.DeleteKey(deleteCtx, claim.scopedID); err != nil {
		if k.logger != nil {
			k.logger.Error("failed to delete a created key whose ownership record could not be completed",
				"operation", operation, "record", claim.key, "error", err)
		}
		return cause
	}
	return k.abandonTenantKeyClaim(ctx, claim, operation, cause)
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
