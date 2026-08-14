// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Key derivation
//
// orch/account/{tenant}/{accountID}                     -> Account (JSON)
// orch/accountdefault/{tenant}/{provider}/{scope}        -> accountID
//
// scope is "_tenant" for a tenant-wide default, or the projectID for a
// project-scoped default. Both segments are validated exactly like any other
// key segment (store_keys.go), so tenant isolation and key-forgery resistance
// hold for accounts identically to every other orchestration store.
// =============================================================================

// accountDefaultScopeTenant is the scope segment for a tenant-wide default
// (Account.ProjectID == "").
const accountDefaultScopeTenant = "_tenant"

func accountRecordKey(tenantID, accountID string) (string, error) {
	key, err := entityKey(accountKeyspace, tenantID, "id", accountID)
	if err != nil {
		return "", accountKeyError(err)
	}
	return key, nil
}

func accountDefaultScope(projectID string) string {
	if projectID == "" {
		return accountDefaultScopeTenant
	}
	return projectID
}

func accountDefaultKey(tenantID string, provider AccountProvider, projectID string) (string, error) {
	prefix, err := tenantPrefix(accountDefaultKeyspace, tenantID)
	if err != nil {
		return "", accountKeyError(err)
	}
	if err := validateKeySegment("provider", string(provider)); err != nil {
		return "", accountKeyError(err)
	}
	scope := accountDefaultScope(projectID)
	if err := validateKeySegment("project_scope", scope); err != nil {
		return "", accountKeyError(err)
	}
	return prefix + string(provider) + keySeparator + scope, nil
}

// accountKeyError converts a key-layout violation into the store's own
// validation vocabulary, mirroring projectKeyError.
func accountKeyError(err error) error {
	var segment *KeySegmentError
	if !errors.As(err, &segment) {
		return err
	}
	return &AccountInvalidError{Field: segment.Field, Message: segment.Message}
}

// =============================================================================
// accountStore — the single KV-backed AccountStore
// =============================================================================

// accountStore is the only AccountStore implementation. Like projectStore, it
// holds no data of its own: durability, replication, and (via the injected
// KV) storage-barrier encryption are properties of the backing
// interfaces.KVStore. This record is METADATA ONLY — see account_types.go —
// so it does not require the stronger, secret-specific envelope encryption
// AccountSecretStore applies to credential material.
type accountStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor

	// recordMu serializes the read-modify-write sequence on one account
	// record (Update, Touch, and the default-pointer swap in SetDefault).
	recordMu keyedMutex

	// defaultMu serializes default-pointer reservation within one
	// tenant+provider(+project) scope.
	//
	// defaultMu is a LEAF lock: no other lock may be acquired while it is held.
	// recordMu -> defaultMu is the only legal nesting. This is what makes the
	// pointer swap deadlock-free by construction: the previous default holder's
	// record is never locked to clear a mirrored flag, because there is no
	// mirrored flag — Account.Default is derived from this pointer on read.
	defaultMu keyedMutex
}

// Compile-time interface satisfaction check.
var _ AccountStore = (*accountStore)(nil)

// NewAccountStore returns an AccountStore backed by kv.
func NewAccountStore(kv interfaces.KVStore, opts ...StoreOption) AccountStore {
	cfg := newStoreConfig(opts...)
	return &accountStore{kv: kv, auditor: cfg.auditor}
}

// NewInMemoryAccountStore returns a memory-backed AccountStore for tests and
// embedded use that need no durability.
func NewInMemoryAccountStore() AccountStore {
	return NewAccountStore(kvstore.NewMemory())
}

func (s *accountStore) ready() error {
	if s.kv == nil {
		return &AccountStoreError{Message: "kv store must not be nil"}
	}
	return nil
}

// =============================================================================
// Create
// =============================================================================

func (s *accountStore) Create(ctx context.Context, account *Account) error {
	var tenantID, accountID string
	if account != nil {
		tenantID, accountID = account.TenantID, account.ID
	}
	err := s.create(ctx, account)
	s.audit(ctx, AuditActionCreate, tenantID, accountID, err)
	return err
}

func (s *accountStore) create(ctx context.Context, account *Account) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if account == nil {
		return &AccountInvalidError{Message: "account must not be nil"}
	}
	if account.ID == "" {
		return &AccountInvalidError{Field: "id", Message: "must not be empty"}
	}
	if account.TenantID == "" {
		return &AccountInvalidError{Field: "tenant_id", Message: "must not be empty"}
	}
	if account.Provider == "" {
		return &AccountInvalidError{Field: "provider", Message: "must not be empty"}
	}

	recordKey, err := accountRecordKey(account.TenantID, account.ID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(recordKey)
	defer unlock()

	exists, err := s.kv.Exists(ctx, recordKey)
	if err != nil {
		return &AccountStoreError{Message: "failed to probe account: " + err.Error()}
	}
	if exists {
		return &AccountInvalidError{Field: "id", Message: "account already exists"}
	}

	now := time.Now().UTC()
	if account.CreatedAt.IsZero() {
		account.CreatedAt = now
	}
	account.UpdatedAt = now
	if account.Status == "" {
		account.Status = AccountStatusActive
	}
	if account.RefreshPolicy == "" {
		account.RefreshPolicy = AccountRefreshNone
	}

	defaultKey, err := accountDefaultKey(account.TenantID, account.Provider, account.ProjectID)
	if err != nil {
		return err
	}

	// The scope's default pointer is held from here to the end of the call.
	// Asking "does this scope already have a default?" and recording the answer
	// therefore happen as one step against any other writer of the same
	// pointer, so two accounts created for the same scope at the same instant
	// can neither both claim the default nor both decline it. This is the same
	// pointer and the same lock SetDefault uses — there is exactly one
	// mechanism deciding who the default is.
	unlockDefault := s.defaultMu.lock(defaultKey)
	defer unlockDefault()

	if !account.Default {
		vacant, vacantErr := s.defaultVacantLocked(ctx, defaultKey, account.TenantID)
		if vacantErr != nil {
			return vacantErr
		}
		account.Default = vacant
	}

	// The pointer is written BEFORE the record, and is the ONLY place the
	// default is recorded — Account.Default is derived from it on every read, so
	// there is no second copy to keep in step. A failure of the record write
	// therefore leaves a pointer naming an account that does not exist, which
	// defaultVacantLocked already treats as a vacant scope, so the next create
	// heals it.
	if account.Default {
		if err := s.writeDefaultPointerLocked(ctx, defaultKey, account.ID); err != nil {
			return err
		}
	}

	data, marshalErr := marshalAccount(account)
	if marshalErr != nil {
		return marshalErr
	}
	if err := s.kv.Put(ctx, recordKey, data); err != nil {
		return &AccountStoreError{Message: "failed to write account: " + err.Error()}
	}
	return nil
}

// =============================================================================
// Get
// =============================================================================

func (s *accountStore) Get(ctx context.Context, tenantID, accountID string) (*Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	recordKey, err := accountRecordKey(tenantID, accountID)
	if err != nil {
		return nil, err
	}
	return s.load(ctx, recordKey, tenantID, accountID)
}

func (s *accountStore) load(ctx context.Context, recordKey, tenantID, accountID string) (*Account, error) {
	data, err := s.kv.Get(ctx, recordKey)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &AccountNotFoundError{TenantID: tenantID, AccountID: accountID}
		}
		return nil, &AccountStoreError{Message: "failed to read account: " + err.Error()}
	}
	if data == nil {
		return nil, &AccountNotFoundError{TenantID: tenantID, AccountID: accountID}
	}

	var account Account
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, &AccountStoreError{Message: "failed to unmarshal account: " + err.Error()}
	}

	// Defence in depth: the key already scopes the read to tenantID, so a
	// record whose body names a different tenant means the key layout was
	// bypassed. Report it as absent rather than returning another tenant's
	// account.
	if account.TenantID != tenantID {
		return nil, &AccountNotFoundError{TenantID: tenantID, AccountID: accountID}
	}

	// A limit hold that has lapsed is returned to the active pool here, on the
	// read, rather than by a background sweeper. Every mutating path in this
	// store (Update, Touch, SetDefault) reloads through this function before it
	// writes, so the normalization is persisted by the next write that touches
	// the record and no caller ever observes a stale "limited".
	account.expireCooldown(time.Now().UTC())

	if err := s.deriveDefault(ctx, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

// deriveDefault sets account.Default from its scope's durable default pointer.
//
// The pointer is the sole authority. Deriving here rather than persisting a
// mirror is what removes the divergence class entirely: there is no window in
// which two accounts in one scope both report Default, which matters because
// StaticAccountStrategy selects on this field and a second flagged candidate
// would silently redirect an unpinned spawn — and its billing — to another
// provider login. Same reasoning as CredentialHealth, which is likewise derived
// and deliberately never written.
func (s *accountStore) deriveDefault(ctx context.Context, account *Account) error {
	defaultKey, err := accountDefaultKey(account.TenantID, account.Provider, account.ProjectID)
	if err != nil {
		return err
	}
	holderID, err := s.currentDefaultID(ctx, defaultKey)
	if err != nil {
		return err
	}
	account.Default = holderID == account.ID
	return nil
}

// =============================================================================
// Update
// =============================================================================

func (s *accountStore) Update(ctx context.Context, account *Account) error {
	var tenantID, accountID string
	if account != nil {
		tenantID, accountID = account.TenantID, account.ID
	}
	err := s.update(ctx, account)
	s.audit(ctx, AuditActionUpdate, tenantID, accountID, err)
	return err
}

func (s *accountStore) update(ctx context.Context, account *Account) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if account == nil {
		return &AccountInvalidError{Message: "account must not be nil"}
	}

	recordKey, err := accountRecordKey(account.TenantID, account.ID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(recordKey)
	defer unlock()

	current, err := s.load(ctx, recordKey, account.TenantID, account.ID)
	if err != nil {
		return err
	}
	if account.CreatedAt.IsZero() {
		account.CreatedAt = current.CreatedAt
	}
	account.UpdatedAt = time.Now().UTC()

	moved, err := s.accountChangedDefaultScope(current, account)
	if err != nil {
		return err
	}
	if moved {
		if err := s.rescopeDefault(ctx, current, account); err != nil {
			return err
		}
	}

	data, marshalErr := marshalAccount(account)
	if marshalErr != nil {
		return marshalErr
	}
	if err := s.kv.Put(ctx, recordKey, data); err != nil {
		return &AccountStoreError{Message: "failed to write account: " + err.Error()}
	}

	if moved {
		return s.releaseDefaultScope(ctx, current)
	}
	if account.Default && !current.Default {
		if err := s.setDefaultLocked(ctx, account.TenantID, account.Provider, account.ProjectID, account.ID); err != nil {
			return err
		}
	}
	return nil
}

// accountChangedDefaultScope reports whether an update moves the account out of
// the default scope it currently occupies — a change of project (the common
// case: an operator narrowing a tenant-wide account to one project, or widening
// it back) or of provider.
func (s *accountStore) accountChangedDefaultScope(current, updated *Account) (bool, error) {
	currentKey, err := accountDefaultKey(current.TenantID, current.Provider, current.ProjectID)
	if err != nil {
		return false, err
	}
	updatedKey, err := accountDefaultKey(updated.TenantID, updated.Provider, updated.ProjectID)
	if err != nil {
		return false, err
	}
	return currentKey != updatedKey, nil
}

// rescopeDefault re-evaluates an account's Default for the scope it is moving
// INTO, setting it in place on updated.
//
// A move is treated exactly like a create: the account claims the destination
// scope when that scope has no default, and otherwise arrives non-default. An
// operator who wants the moved account to be the destination's default says so
// with SetDefault, which displaces the incumbent explicitly.
//
// Without this, a move would silently strand the destination scope's pointer:
// the account leaving would take its old scope's pointer with it while the
// destination's pointer still named someone else.
func (s *accountStore) rescopeDefault(ctx context.Context, current, updated *Account) error {
	defaultKey, err := accountDefaultKey(updated.TenantID, updated.Provider, updated.ProjectID)
	if err != nil {
		return err
	}

	unlockDefault := s.defaultMu.lock(defaultKey)
	defer unlockDefault()

	vacant, err := s.defaultVacantLocked(ctx, defaultKey, updated.TenantID)
	if err != nil {
		return err
	}
	// The account's own record still names its OLD scope, so a vacancy probe of
	// the destination cannot be answered by the account itself.
	updated.Default = vacant
	if !vacant {
		return nil
	}
	return s.writeDefaultPointerLocked(ctx, defaultKey, updated.ID)
}

// releaseDefaultScope clears the pointer of the scope an account has left, when
// that pointer still names it. The two scopes' pointers are locked one after
// the other rather than together: holding both would create an ABBA ordering
// between two accounts moving in opposite directions.
func (s *accountStore) releaseDefaultScope(ctx context.Context, previous *Account) error {
	if !previous.Default {
		return nil
	}
	return s.releaseDefaultLocked(ctx, previous.TenantID, previous)
}

// =============================================================================
// Delete
// =============================================================================

func (s *accountStore) Delete(ctx context.Context, tenantID, accountID string) error {
	err := s.remove(ctx, tenantID, accountID)
	s.audit(ctx, AuditActionDelete, tenantID, accountID, err)
	return err
}

func (s *accountStore) remove(ctx context.Context, tenantID, accountID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}

	recordKey, err := accountRecordKey(tenantID, accountID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(recordKey)
	defer unlock()

	current, err := s.load(ctx, recordKey, tenantID, accountID)
	if err != nil {
		return err
	}

	// Release the default pointer BEFORE the record, so a caller that gets a
	// non-nil error can rely on the account still existing. AccountService
	// destroys the credential envelope only after Delete returns nil; the
	// opposite ordering could remove the record, fail on the pointer, and leave
	// an orphaned credential behind an account nobody can list.
	if current.Default {
		if err := s.releaseDefaultLocked(ctx, tenantID, current); err != nil {
			return err
		}
	}

	if err := s.kv.Delete(ctx, recordKey); err != nil {
		return &AccountStoreError{Message: "failed to delete account: " + err.Error()}
	}
	return nil
}

// releaseDefaultLocked clears the default pointer of account's scope when it
// still names account.
//
// Deleting the default leaves the scope WITHOUT one; no survivor is promoted.
// Promoting by age or creation order would silently redirect every unpinned
// spawn — and its billing — to a different provider login with no operator
// action, which is precisely the ambiguity StaticAccountStrategy refuses to
// resolve by guessing. What the operator gets instead is the safe pair of
// outcomes already built into selection: a scope with ONE survivor still
// resolves through the lone-candidate rule, and a scope with several fails
// loudly with NoAccountAvailableError until either an explicit SetDefault or
// the next created account (see create) fills the vacancy.
func (s *accountStore) releaseDefaultLocked(ctx context.Context, tenantID string, account *Account) error {
	defaultKey, err := accountDefaultKey(tenantID, account.Provider, account.ProjectID)
	if err != nil {
		return err
	}

	unlockDefault := s.defaultMu.lock(defaultKey)
	defer unlockDefault()

	currentID, err := s.currentDefaultID(ctx, defaultKey)
	if err != nil {
		return err
	}
	if currentID != account.ID {
		// Another account took the default between this record's load and now.
		// Clearing the pointer would strand that one instead of this one.
		return nil
	}
	if delErr := s.kv.Delete(ctx, defaultKey); delErr != nil {
		return &AccountStoreError{Message: "failed to clear account default pointer: " + delErr.Error()}
	}
	return nil
}

// =============================================================================
// ListByTenant
// =============================================================================

func (s *accountStore) ListByTenant(ctx context.Context, tenantID string, provider AccountProvider, projectID string) ([]*Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	prefix, err := tenantPrefix(accountKeyspace, tenantID)
	if err != nil {
		return nil, accountKeyError(err)
	}

	var (
		accounts  []*Account
		decodeErr error
	)
	now := time.Now().UTC()
	scanErr := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		if err := ctx.Err(); err != nil {
			decodeErr = err
			return err
		}
		var account Account
		if err := json.Unmarshal(value, &account); err != nil {
			decodeErr = &AccountStoreError{Message: "failed to unmarshal account " + idFromKey(key, prefix) + ": " + err.Error()}
			return decodeErr
		}
		if account.TenantID != tenantID {
			return nil
		}
		if provider != "" && account.Provider != provider {
			return nil
		}
		if projectID != "" && account.ProjectID != projectID {
			return nil
		}
		account.expireCooldown(now)
		accounts = append(accounts, &account)
		return nil
	})
	if decodeErr != nil {
		return nil, decodeErr
	}
	if scanErr != nil {
		return nil, &AccountStoreError{Message: "failed to list accounts: " + scanErr.Error()}
	}
	if err := s.deriveDefaults(ctx, accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

// deriveDefaults is deriveDefault for a whole listing, reading each distinct
// scope pointer once rather than once per account. A tenant's accounts collapse
// into very few scopes, so a list costs a handful of extra reads — the price of
// having exactly one authority instead of a mirror that can drift.
func (s *accountStore) deriveDefaults(ctx context.Context, accounts []*Account) error {
	holders := make(map[string]string)
	for _, account := range accounts {
		defaultKey, err := accountDefaultKey(account.TenantID, account.Provider, account.ProjectID)
		if err != nil {
			return err
		}
		holderID, cached := holders[defaultKey]
		if !cached {
			holderID, err = s.currentDefaultID(ctx, defaultKey)
			if err != nil {
				return err
			}
			holders[defaultKey] = holderID
		}
		account.Default = holderID == account.ID
	}
	return nil
}

// =============================================================================
// SetDefault
// =============================================================================

func (s *accountStore) SetDefault(ctx context.Context, tenantID, accountID string) error {
	err := s.setDefault(ctx, tenantID, accountID)
	s.audit(ctx, AuditActionUpdate, tenantID, accountID, err)
	return err
}

func (s *accountStore) setDefault(ctx context.Context, tenantID, accountID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	recordKey, err := accountRecordKey(tenantID, accountID)
	if err != nil {
		return err
	}

	// Read-only: the record is not written at all. Making an account the default
	// is now a single-key operation on the scope pointer, so there is nothing to
	// serialize against concurrent record writes and no record lock to take.
	// This is what makes the previous ABBA inversion impossible rather than
	// merely unlikely — see accountStore.defaultMu.
	account, err := s.load(ctx, recordKey, tenantID, accountID)
	if err != nil {
		return err
	}
	if account.Status == AccountStatusRevoked {
		return &AccountRevokedError{AccountID: accountID}
	}

	return s.setDefaultLocked(ctx, tenantID, account.Provider, account.ProjectID, accountID)
}

// setDefaultLocked re-points the tenant+provider(+project) default pointer at
// accountID. Whoever previously held the scope stops being the default the
// instant this write lands, because every reader derives Default from this
// pointer — there is no second copy to clear.
func (s *accountStore) setDefaultLocked(ctx context.Context, tenantID string, provider AccountProvider, projectID, accountID string) error {
	defaultKey, err := accountDefaultKey(tenantID, provider, projectID)
	if err != nil {
		return err
	}

	unlockDefault := s.defaultMu.lock(defaultKey)
	defer unlockDefault()

	return s.writeDefaultPointerLocked(ctx, defaultKey, accountID)
}

// writeDefaultPointerLocked points defaultKey at accountID. The caller must
// already hold defaultMu for defaultKey — keyedMutex is not reentrant, and
// create must decide and act under one continuous hold.
func (s *accountStore) writeDefaultPointerLocked(ctx context.Context, defaultKey, accountID string) error {
	if err := s.kv.Put(ctx, defaultKey, []byte(accountID)); err != nil {
		return &AccountStoreError{Message: "failed to write account default pointer: " + err.Error()}
	}
	return nil
}

// currentDefaultID returns the account ID the scope's default pointer names, or
// "" when the scope has no pointer at all.
//
// Callers that intend to WRITE the pointer based on the answer must hold
// defaultMu for defaultKey, so that decide-then-act is one step. The read paths
// (deriveDefault, deriveDefaults) deliberately do not lock: a single-key read
// cannot tear, and taking the scope lock would serialize every list behind every
// SetDefault to narrow a window that is inherent anyway — a caller can always
// observe the default from just before or just after a swap.
func (s *accountStore) currentDefaultID(ctx context.Context, defaultKey string) (string, error) {
	value, err := s.kv.Get(ctx, defaultKey)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return "", nil
		}
		return "", &AccountStoreError{Message: "failed to read account default pointer: " + err.Error()}
	}
	return string(value), nil
}

// defaultVacantLocked reports whether the scope guarded by defaultKey currently
// has no default account. The caller must already hold defaultMu for defaultKey.
//
// A scope is vacant when its pointer is unset, and also when the pointer names
// an account that no longer exists: the record and the pointer are two separate
// KV writes with no transaction spanning them, so a crash between them can
// strand a pointer on a deleted account. Treating a stranded pointer as vacant
// lets the next created account heal the scope instead of leaving it unable to
// ever auto-claim a default again.
func (s *accountStore) defaultVacantLocked(ctx context.Context, defaultKey, tenantID string) (bool, error) {
	currentID, err := s.currentDefaultID(ctx, defaultKey)
	if err != nil {
		return false, err
	}
	if currentID == "" {
		return true, nil
	}
	currentKey, keyErr := accountRecordKey(tenantID, currentID)
	if keyErr != nil {
		// The pointer holds a value that is not even a legal key segment, so
		// nothing it names can be loaded. The scope has no reachable default.
		return true, nil
	}
	exists, existsErr := s.kv.Exists(ctx, currentKey)
	if existsErr != nil {
		return false, &AccountStoreError{Message: "failed to probe account default holder: " + existsErr.Error()}
	}
	return !exists, nil
}

// =============================================================================
// Touch
// =============================================================================

func (s *accountStore) Touch(ctx context.Context, tenantID, accountID string, opts TouchAccountOptions) error {
	err := s.touch(ctx, tenantID, accountID, opts)
	action := AuditActionUpdate
	if opts.MarkUsed && !opts.MarkRefreshed && !opts.SetStatusValid {
		action = AuditActionUse
	}
	s.audit(ctx, action, tenantID, accountID, err)
	return err
}

func (s *accountStore) touch(ctx context.Context, tenantID, accountID string, opts TouchAccountOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	recordKey, err := accountRecordKey(tenantID, accountID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(recordKey)
	defer unlock()

	account, err := s.load(ctx, recordKey, tenantID, accountID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if opts.MarkUsed {
		account.LastUsedAt = now
	}
	if opts.MarkRefreshed {
		account.LastRefreshedAt = now
	}
	if opts.SetStatusValid {
		account.Status = opts.SetStatus
		// Leaving a stale AvailableAt on an account that just moved OUT of the
		// limited state would make it look held forever to any reader that
		// checks the timestamp before the status. Clear it unless this same
		// Touch is explicitly setting one.
		if opts.SetStatus != AccountStatusLimited && !opts.SetAvailableAtValid {
			account.AvailableAt = time.Time{}
		}
	}
	if opts.SetAvailableAtValid {
		account.AvailableAt = opts.SetAvailableAt
	}
	if opts.MarkValidated {
		account.LastValidatedAt = now
	}
	if opts.SetCredentialExpiryValid {
		account.CredentialExpiresAt = opts.SetCredentialExpiresAt
	}
	if opts.ClearLastError {
		account.LastError = ""
	} else if opts.SetLastError != "" {
		account.LastError = opts.SetLastError
	}
	account.UpdatedAt = now

	data, marshalErr := marshalAccount(account)
	if marshalErr != nil {
		return marshalErr
	}
	if err := s.kv.Put(ctx, recordKey, data); err != nil {
		return &AccountStoreError{Message: "failed to write account: " + err.Error()}
	}
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

// storedAccount is the durable projection of an Account. It exists solely to
// bypass Account.MarshalJSON: credential_health is derived from a timestamp and
// re-derived on every read, so writing it into the record would persist a
// verdict that is already stale by the time anyone reads it back.
type storedAccount Account

// marshalAccount encodes the durable projection of account.
//
// Default is zeroed rather than written: the scope's default pointer is the
// sole authority and every read derives the field from it (see deriveDefault).
// Persisting a copy is what allowed a record to claim a default the pointer had
// already moved elsewhere.
func marshalAccount(account *Account) (data []byte, storeErr *AccountStoreError) {
	stored := storedAccount(*account)
	stored.Default = false
	b, err := json.Marshal(&stored)
	if err != nil {
		return nil, &AccountStoreError{Message: "failed to marshal account: " + err.Error()}
	}
	return b, nil
}

// audit emits exactly one store event per mutating call.
func (s *accountStore) audit(ctx context.Context, action, tenantID, accountID string, err error) {
	emitStoreAudit(ctx, s.auditor, StoreAuditEvent{
		TenantID:   tenantID,
		Resource:   AuditResourceAccount,
		ResourceID: accountID,
		Action:     action,
		Success:    err == nil,
	})
}
