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

	// creator and deleter are kv's guarded-write capabilities, asserted once at
	// construction. Nil when the backing store cannot decide a contested write,
	// in which case CreateAccount refuses rather than probing and hoping.
	//
	// This store needs BOTH halves, unlike the task store: it claims a scope's
	// default pointer and it releases one, and an unguarded release can remove a
	// pointer another account legitimately took (go-qrdb#41).
	creator guardedCreator
	deleter guardedDeleter

	// swapper is kv's guarded-edit capability. EVERY write of an existing
	// account record commits through it, against the exact bytes that write
	// read (commitRecord). recordMu below only orders writers inside ONE store
	// instance, and there is more than one per process — the service layer
	// and the orchestration runtime each build their own over the same KV —
	// and one per daemon beyond that. Only the backend can make "the record I
	// read is still the record" and "write mine" a single step for all of
	// them, so a stale snapshot can never overwrite a newer record.
	swapper guardedSwapper

	// recordMu serializes the read-modify-write sequence on one account
	// record (Update, Touch, and the default-pointer swap in SetDefault)
	// within this instance, so local writers queue rather than spin on the
	// swap. It is not what makes the write safe; swapper is.
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
	store := &accountStore{kv: kv, auditor: cfg.auditor}
	// Asserted once: the capabilities are static properties of the injected
	// store, so probing them per call would ask the same question forever.
	if creator, ok := kv.(guardedCreator); ok {
		store.creator = creator
	}
	if deleter, ok := kv.(guardedDeleter); ok {
		store.deleter = deleter
	}
	if swapper, ok := kv.(guardedSwapper); ok {
		store.swapper = swapper
	}
	return store
}

// NewInMemoryAccountStore returns a memory-backed AccountStore for tests and
// embedded use that need no durability.
func NewInMemoryAccountStore() AccountStore {
	// go-qrdb's memory KVStore rather than kvstore.NewMemory(): creating an
	// account is a guarded create and claiming a scope default is a guarded
	// election, and that adapter wraps a raw storage engine with no conditional
	// write to delegate to.
	return NewAccountStore(NewMemoryTaskKV())
}

func (s *accountStore) ready() error {
	if s.kv == nil {
		return &AccountStoreError{Message: "kv store must not be nil"}
	}
	return nil
}

// scopeCheck refuses a verb whose context is scoped to a tenant other than
// the one the request names.
//
// The KV beneath this store routes by the CONTEXT's tenant, and a
// prefix-isolation tenant's records live in that tenant's own namespace; the
// tenant segment in the key is a label, not the router's input. A mismatch
// therefore means the record would be read from, or written into, a namespace
// its own tenant field disagrees with — exactly the state go-quicbaas#535
// measured (a record tagged tenant=T sitting in the system namespace, listable
// by the admin, invisible to T's spawn path). Refusing it up front is what
// keeps that state from ever looking healthy again.
//
// An unscoped context is allowed through: in-process embedded callers and
// unit tests carry none, and the service layer above scopes every API request
// before it reaches here (service.accountTenantContext).
func scopeCheck(ctx context.Context, tenantID string) error {
	carried := TenantFromContext(ctx)
	if carried == "" || carried == tenantID {
		return nil
	}
	return &AccountTenantScopeError{ContextTenant: carried, TenantID: tenantID}
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

func (s *accountStore) create(ctx context.Context, account *Account) (err error) {
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
	// A SPEND POLICY IS REFUSED AT THE WRITE, NEVER AT THE SPAWN. A window
	// that meters nothing, or a breaker threshold of zero, is only detectable
	// as a defect here — by the time the gate reads it, the choice is between
	// refusing an account the operator believes is capped and admitting one
	// they believe is metered, and both are wrong.
	if err := account.Spend.Validate(); err != nil {
		return err
	}
	if err := account.validateSessionCap(); err != nil {
		return err
	}
	if err := account.Billing.Validate(); err != nil {
		return err
	}
	if err := scopeCheck(ctx, account.TenantID); err != nil {
		return err
	}

	recordKey, err := accountRecordKey(account.TenantID, account.ID)
	if err != nil {
		return err
	}

	if s.creator == nil || s.deleter == nil {
		return &AccountStoreError{
			Message: "creates need a store that can decide them; this one cannot guard a write",
		}
	}

	unlock := s.recordMu.lock(recordKey)
	defer unlock()

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

	// Whether to ATTEMPT the auto-claim. The answer to "is this scope vacant?"
	// is not decided here any more — electDefaultLocked asks the store, which is
	// the only place the answer survives contact with a second node.
	vacancyClaimed := !account.Default

	// The pointer is written BEFORE the record, and is the ONLY place the
	// default is recorded — Account.Default is derived from it on every read, so
	// there is no second copy to keep in step.
	//
	// TWO DIFFERENT WRITES, because they are two different intentions. A caller
	// that ASKED for Default:true is naming a default, which is an operator
	// update and is last-writer-wins exactly as SetDefault is. A caller that did
	// not ask is auto-claiming a scope only if it is vacant, which is a contested
	// election and must be decided by the store.
	// claimedScope records that THIS call WROTE the scope pointer, which is the
	// only pointer it is entitled to give back — see the defer below. Winning
	// is not enough: a pointer that already named this account was written by
	// someone else, and releasing it would take the scope from a live account.
	claimedScope := false
	if account.Default {
		if err := s.writeDefaultPointerLocked(ctx, defaultKey, account.ID); err != nil {
			return err
		}
	} else if vacancyClaimed {
		won, claimed, electErr := s.electDefaultLocked(ctx, defaultKey, account.TenantID, account.ID)
		if electErr != nil {
			return electErr
		}
		account.Default = won
		claimedScope = claimed
	}

	// A WON ELECTION IS A LOAN UNTIL THE RECORD EXISTS (#249).
	//
	// The pointer is claimed before the record is written, so every return
	// between here and a successful PutIfAbsent used to leave the scope naming
	// an account this call never created. That residue is DURABLE, not
	// transient: electDefaultLocked heals only a pointer whose holder has no
	// record (defaultHolderMissing), and the create that fails most often is a
	// duplicate ID — whose record exists, in another scope. That is precisely
	// the state the store deliberately refuses to heal, because it cannot be
	// told apart from a move in flight. So nothing recovered the scope
	// afterwards: every later create there reported Default=false and selection
	// found no default at all.
	//
	// A DEFER RATHER THAN A RELEASE ON EACH PATH, because the defect IS a
	// forgotten path and there were three of them. recordWritten is set at
	// exactly one point — the moment the account exists and the claim stops
	// being a loan — so a failure path added later is covered by construction
	// instead of by whoever adds it remembering.
	//
	// IT RUNS UNDER defaultMu. Defers are LIFO and unlockDefault was registered
	// above, so the release happens while this call still holds the scope lock,
	// exactly as the claim did.
	//
	// GUARDED BY THIS CALL'S OWN ID, and only after a WON election. A create
	// that lost never sets claimedScope, so it cannot release the winner's
	// pointer; and CompareAndDelete removes the pointer only while it still
	// names this account, so a release racing a legitimate SetDefault does
	// nothing rather than stranding the new holder — the same reasoning
	// releaseDefaultLocked carries (go-qrdb#41).
	//
	// THE OTHER BRANCH OF THE if ABOVE IS NOT COVERED HERE. A create that asked
	// for Default:true writes the pointer with an unconditional Put, so a
	// failure there leaves the scope naming a phantom AND has already destroyed
	// whatever default it displaced. Releasing this call's ID would leave the
	// scope vacant rather than restore the operator's choice — a different end
	// state needing a different remedy, filed as #480 rather than decided here.
	//
	// THE ORDERING IS NOT CHANGED. Writing the record before claiming the
	// pointer would also close this window, and would remove the concurrent
	// pointer-steal behind #244, but it gives up the "pointer written BEFORE
	// the record" property the surrounding comments rely on. That is an
	// architecture decision and is the owner's, not this fix's (#249).
	recordWritten := false
	defer func() {
		if !claimedScope || recordWritten {
			return
		}
		// Joined, never substituted: the caller must still be able to see the
		// reason its create failed. errors.Join keeps errors.As/Is working on
		// the original, so an AccountInvalidError stays one.
		if releaseErr := s.releaseClaimedDefaultLocked(ctx, defaultKey, account.ID); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
	}()

	data, marshalErr := marshalAccount(account)
	if marshalErr != nil {
		return marshalErr
	}
	stored, err := s.creator.PutIfAbsent(ctx, recordKey, data)
	if err != nil {
		return &AccountStoreError{Message: "failed to write account: " + err.Error()}
	}
	if !stored {
		return &AccountInvalidError{Field: "id", Message: "account already exists"}
	}
	recordWritten = true

	// VERIFY AFTER THE RECORD WRITE, then RE-DERIVE rather than roll back.
	//
	// A node that claimed the pointer and was then SLOW — not crashed — can have
	// its claim healed away by another create that observed its record absent and
	// concluded the scope was vacant. Its own record write still succeeds
	// afterwards, on its own uncontested key, so without this it would return
	// Default:true for a scope another account now owns. Nothing else detects
	// that.
	//
	// The remedy is a re-read, not a rollback, because of what is contested. The
	// pointer is not part of the account's identity: losing the election leaves a
	// perfectly good non-default account, and marshalAccount already zeroes the
	// persisted field so no stored state disagrees. Only the value handed back to
	// the caller can be stale, and re-deriving it makes that value agree with
	// what any later read would report. Contrast the project name, which IS an
	// identity — there a lost race invalidates the record and the create must
	// fail.
	holderID, holderErr := s.currentDefaultID(ctx, defaultKey)
	if holderErr != nil {
		return holderErr
	}
	account.Default = holderID == account.ID
	return nil
}

// guarded reports whether the backing store can decide a contested write.
//
// Checked by every path that claims or releases a scope pointer, so a store
// without the capability refuses up front instead of dereferencing a missing
// capability partway through a multi-key sequence.
func (s *accountStore) guarded() error {
	if s.creator == nil || s.deleter == nil {
		return &AccountStoreError{
			Message: "scope defaults need a store that can decide them; this one cannot guard a write",
		}
	}
	return nil
}

// electDefaultLocked claims a vacant scope's default pointer for accountID, and
// reports whether it won.
//
// Losing is a verdict, not an error: a scope that already has a live default is
// the ordinary case, and the account is created either way.
//
// An orphaned pointer — one naming an account whose record no longer exists — is
// healed rather than obeyed, but BOTH steps are guarded. CompareAndDelete
// removes only the exact orphan this caller observed, so a healer racing the
// holder's own late write cannot delete a pointer that has since moved, and the
// following PutIfAbsent cannot overwrite whatever took its place.
//
// WON AND CLAIMED ARE NOT THE SAME ANSWER, and conflating them lets a create
// release a pointer it never took (#249). won says this account owns the scope,
// which is what Account.Default is derived from. claimed says THIS CALL is what
// put the pointer there, which is the only thing that entitles it to give the
// pointer back. They differ in exactly one case: a pointer that already names
// this account ID. That is ordinary — it is the residue of an earlier failed
// create, which a successful record write here heals — and a create that then
// fails must leave it exactly as it found it.
func (s *accountStore) electDefaultLocked(ctx context.Context, defaultKey, tenantID, accountID string) (won, claimed bool, err error) {
	won, err = s.creator.PutIfAbsent(ctx, defaultKey, []byte(accountID))
	if err != nil {
		return false, false, &AccountStoreError{Message: "failed to claim account default pointer: " + err.Error()}
	}
	if won {
		return true, true, nil
	}

	holderID, err := s.currentDefaultID(ctx, defaultKey)
	if err != nil {
		return false, false, err
	}
	if holderID == "" || holderID == accountID {
		// The pointer was taken and released, or already names this account.
		// Either way there is nothing to heal and nothing to steal — and
		// nothing this call may release, because it wrote none of it.
		return holderID == accountID, false, nil
	}
	orphaned, err := s.defaultHolderMissing(ctx, tenantID, holderID)
	if err != nil {
		return false, false, err
	}
	if !orphaned {
		return false, false, nil
	}

	removed, err := s.deleter.CompareAndDelete(ctx, defaultKey, []byte(holderID))
	if err != nil {
		return false, false, &AccountStoreError{Message: "failed to clear orphaned account default pointer: " + err.Error()}
	}
	if !removed {
		// The pointer moved between the read and the delete, so the orphan this
		// caller saw is already gone and someone else owns the scope.
		return false, false, nil
	}
	won, err = s.creator.PutIfAbsent(ctx, defaultKey, []byte(accountID))
	if err != nil {
		return false, false, &AccountStoreError{Message: "failed to claim account default pointer: " + err.Error()}
	}
	return won, won, nil
}

// releaseClaimedDefaultLocked gives back a scope pointer that a create won and
// then could not use, leaving the scope as the call found it: vacant.
//
// Distinct from releaseDefaultLocked, which retires the default of an account
// being DELETED. This one undoes a claim whose account never came into
// existence, and its caller must have WON the election — releasing on any other
// path would remove a pointer belonging to someone else.
//
// removed=false is the ordinary outcome of a late release, not an error: it
// means the pointer no longer names this account, so there is nothing of this
// call's to give back.
func (s *accountStore) releaseClaimedDefaultLocked(ctx context.Context, defaultKey, accountID string) error {
	if s.deleter == nil {
		return &AccountStoreError{
			Message: "releasing a claimed default needs a store that can decide it; this one cannot guard a delete",
		}
	}
	removed, err := s.deleter.CompareAndDelete(ctx, defaultKey, []byte(accountID))
	if err != nil {
		return &AccountStoreError{Message: "failed to release claimed account default pointer: " + err.Error()}
	}
	_ = removed
	return nil
}

// defaultHolderMissing reports whether a pointer names an account with no
// record — the only unambiguous evidence that a default is orphaned.
//
// THE NARROWNESS IS THE DESIGN, NOT AN OVERSIGHT, and it is the same proof the
// project store's reserveName carries, transposed to scopes. A pointer naming a
// LIVE account whose record places it in a DIFFERENT scope has exactly two
// causes: a move INTO this scope in flight (pointer claimed, record not yet
// updated) and a move OUT of this scope that crashed before releasing. Those
// produce byte-identical key states — the pointer names the account, the
// account's record names the other scope — so no read distinguishes them, and
// healing on that condition would steal the scope from a move about to
// complete.
//
// Record-absence cannot arise that way: a move never creates a record, so a
// pointer naming nothing can only be a create that did not finish. That is why
// this predicate asks only about existence.
//
// The residue is a scope blocked by a pointer naming an account that has left
// it, and clearing it needs evidence this store cannot obtain — proof that no
// move is in flight. It belongs to WP10's run-is-authority reaper, with the
// same invariant the scheduler's firing mark already states: A POINTER NAMING
// AN ACCOUNT THAT IS LIVE IN A DIFFERENT SCOPE, WITH NO MOVE IN FLIGHT, IS AN
// INVARIANT VIOLATION AND IS WP10'S TO REAP. Recorded here so its implementer
// finds the case from the code rather than from the plan.
func (s *accountStore) defaultHolderMissing(ctx context.Context, tenantID, holderID string) (bool, error) {
	holderKey, keyErr := accountRecordKey(tenantID, holderID)
	if keyErr != nil {
		// The pointer holds a value that is not even a legal key segment, so
		// nothing it names can be loaded. The scope has no reachable default.
		return true, nil
	}
	exists, err := s.kv.Exists(ctx, holderKey)
	if err != nil {
		return false, &AccountStoreError{Message: "failed to probe account default holder: " + err.Error()}
	}
	return !exists, nil
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
	if err := scopeCheck(ctx, tenantID); err != nil {
		return nil, err
	}
	recordKey, err := accountRecordKey(tenantID, accountID)
	if err != nil {
		return nil, err
	}
	return s.load(ctx, recordKey, tenantID, accountID)
}

func (s *accountStore) load(ctx context.Context, recordKey, tenantID, accountID string) (*Account, error) {
	account, _, err := s.loadRaw(ctx, recordKey, tenantID, accountID)
	return account, err
}

// loadRaw is load, also returning the exact stored bytes the account was
// decoded from. A guarded write compares against THOSE bytes (commitRecord):
// re-encoding the decoded account would not match them, because the read
// normalizes a lapsed cooldown and derives Default.
func (s *accountStore) loadRaw(ctx context.Context, recordKey, tenantID, accountID string) (*Account, []byte, error) {
	data, err := s.kv.Get(ctx, recordKey)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, nil, &AccountNotFoundError{TenantID: tenantID, AccountID: accountID}
		}
		return nil, nil, &AccountStoreError{Message: "failed to read account: " + err.Error()}
	}
	if data == nil {
		return nil, nil, &AccountNotFoundError{TenantID: tenantID, AccountID: accountID}
	}

	var account Account
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, nil, &AccountStoreError{Message: "failed to unmarshal account: " + err.Error()}
	}

	// Defence in depth: the key already scopes the read to tenantID, so a
	// record whose body names a different tenant means the key layout was
	// bypassed. Report it as absent rather than returning another tenant's
	// account.
	if account.TenantID != tenantID {
		return nil, nil, &AccountNotFoundError{TenantID: tenantID, AccountID: accountID}
	}

	// A limit hold that has lapsed is returned to the active pool here, on the
	// read, rather than by a background sweeper. Every mutating path in this
	// store (Update, Touch, SetDefault) reloads through this function before it
	// writes, so the normalization is persisted by the next write that touches
	// the record and no caller ever observes a stale "limited".
	account.expireCooldown(time.Now().UTC())

	if err := s.deriveDefault(ctx, &account); err != nil {
		return nil, nil, err
	}
	return &account, data, nil
}

// commitRecord writes data over recordKey only if the record still holds
// exactly raw, the bytes the caller read. It reports false, having written
// nothing, when another writer — in this process or any other — committed in
// between; the caller then re-reads.
//
// A store without the guarded edit is refused rather than written to
// unconditionally: an unconditional write is precisely the lost update this
// exists to prevent, and succeeding quietly would hide it.
func (s *accountStore) commitRecord(ctx context.Context, recordKey string, raw, data []byte) (bool, error) {
	if s.swapper == nil {
		return false, &AccountStoreError{
			Message: "account records need a store that can decide a guarded edit; this one cannot",
		}
	}
	swapped, err := s.swapper.CompareAndSwap(ctx, recordKey, raw, data)
	if err != nil {
		return false, &AccountStoreError{Message: "failed to write account: " + err.Error(), Cause: err}
	}
	return swapped, nil
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
	err := s.update(ctx, account, nil)
	s.audit(ctx, AuditActionUpdate, tenantID, accountID, err)
	return err
}

// UpdateIfUnchanged implements AccountStore. The UpdatedAt comparison is made
// against the record this write read, and the write itself is a guarded edit
// against that record's bytes (commitRecord), so no writer — in any store
// instance or process — can land between the check and the write.
func (s *accountStore) UpdateIfUnchanged(ctx context.Context, account *Account, expected time.Time) error {
	var tenantID, accountID string
	if account != nil {
		tenantID, accountID = account.TenantID, account.ID
	}
	err := s.update(ctx, account, &expected)
	s.audit(ctx, AuditActionUpdate, tenantID, accountID, err)
	return err
}

// update writes account. A non-nil expected makes the write conditional on the
// stored record's UpdatedAt; see UpdateIfUnchanged.
func (s *accountStore) update(ctx context.Context, account *Account, expected *time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if account == nil {
		return &AccountInvalidError{Message: "account must not be nil"}
	}
	if err := account.Spend.Validate(); err != nil {
		return err
	}
	if err := account.validateSessionCap(); err != nil {
		return err
	}
	if err := account.Billing.Validate(); err != nil {
		return err
	}
	if err := scopeCheck(ctx, account.TenantID); err != nil {
		return err
	}

	recordKey, err := accountRecordKey(account.TenantID, account.ID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(recordKey)
	defer unlock()

	current, raw, err := s.loadRaw(ctx, recordKey, account.TenantID, account.ID)
	if err != nil {
		return err
	}
	if expected != nil && !current.UpdatedAt.Equal(*expected) {
		return &AccountVersionConflictError{TenantID: account.TenantID, AccountID: account.ID}
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
		if err := s.guarded(); err != nil {
			return err
		}
	}

	data, marshalErr := marshalAccount(account)
	if marshalErr != nil {
		return marshalErr
	}

	// THE RECORD MOVES FIRST, THEN THE DESTINATION IS CLAIMED, THEN THE OLD
	// SCOPE IS RELEASED. The order is chosen for what a crash leaves behind,
	// and each residue was compared rather than assumed:
	//
	//   Claim-destination first (the previous order) left the destination
	//   pointer naming an account whose record still said it was elsewhere.
	//   That state is INDISTINGUISHABLE from a move OUT of the destination that
	//   crashed — byte-identical, see reserveName in project_store.go for the
	//   same proof — so nothing may heal it and the destination is blocked until
	//   reaped.
	//
	//   Record first turns that into an unclaimed destination, which is an
	//   ordinary vacant scope that the next claimant takes. One undecidable
	//   residue becomes a benign one, and it costs nothing: Default is never
	//   persisted (marshalAccount zeroes it), so claiming before or after the
	//   record write changes no stored state.
	//
	// RELEASING THE OLD SCOPE FIRST WAS CONSIDERED AND REJECTED, and the reason
	// is a principle rather than effort. It would make every residue benign —
	// nothing left for a reaper at all — because a crash would leave the old
	// scope merely vacant. But a vacant scope is claimable by ANY bystander, so
	// a crashed move would permanently transfer an operator-established default
	// to whoever arrived next. That is handing authority away on a crash. Every
	// decision in this family errs the same direction: refuse a name rather than
	// give one project's name to another, refuse to answer rather than answer
	// plausibly, block a scope rather than reassign it. A squat inconveniences;
	// a transfer decides. We keep the squat.
	swapped, err := s.commitRecord(ctx, recordKey, raw, data)
	if err != nil {
		return err
	}
	if !swapped {
		// The record changed after this write read it. Whatever the caller
		// built was built from an older record, so nothing is written and the
		// caller re-reads, exactly as for a failed UpdatedAt check.
		return &AccountVersionConflictError{TenantID: account.TenantID, AccountID: account.ID}
	}

	if moved {
		if err := s.rescopeDefault(ctx, current, account); err != nil {
			return err
		}
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

	// THE STORE DECIDES THE DESTINATION, not a vacancy probe followed by a
	// hopeful write. This was the last read-then-write election in the store:
	// two accounts moving into one vacant scope could both observe it vacant
	// and both claim it, because the probe and the write are two operations
	// and the lock between them is process-local.
	//
	// Same election as a create's auto-claim, so it is the same call. An
	// account arriving in an occupied scope simply is not its default, which is
	// a verdict rather than an error.
	// The claim verdict is not consulted here: a rescope that fails after this
	// point does not unwind, because the destination pointer is derived below
	// from the store rather than from this election, and the account being moved
	// exists either way. Only a create holds a claim it may have to give back
	// (#249).
	if _, _, err := s.electDefaultLocked(ctx, defaultKey, updated.TenantID, updated.ID); err != nil {
		return err
	}

	// Derived from the pointer rather than from the election's own verdict, so
	// the value handed back agrees with what any later read reports. An
	// operator's SetDefault is a plain last-writer-wins Put and may land between
	// the two; the pointer is the authority and this asks it.
	holderID, err := s.currentDefaultID(ctx, defaultKey)
	if err != nil {
		return err
	}
	updated.Default = holderID == updated.ID
	return nil
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
	if err := scopeCheck(ctx, tenantID); err != nil {
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

	if s.deleter == nil {
		return &AccountStoreError{
			Message: "releasing a default needs a store that can decide it; this one cannot guard a delete",
		}
	}

	// GUARDED, because a read followed by a delete is half a primitive. The
	// previous shape read the pointer, compared it to this account, and then
	// deleted unconditionally — and in the window between the two another
	// account could legitimately take the scope, whose pointer this delete then
	// removed. No amount of re-reading closes that, because the window IS
	// between the read and the delete (go-qrdb#41).
	//
	// CompareAndDelete removes the pointer only while it still names this
	// account, so a release that arrives late does nothing instead of stranding
	// the new holder. deleted=false is the ordinary outcome, not an error: it
	// means someone else owns the scope now, which is exactly when this must not
	// act.
	removed, err := s.deleter.CompareAndDelete(ctx, defaultKey, []byte(account.ID))
	if err != nil {
		return &AccountStoreError{Message: "failed to clear account default pointer: " + err.Error()}
	}
	_ = removed
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
	if err := scopeCheck(ctx, tenantID); err != nil {
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
	if err := scopeCheck(ctx, tenantID); err != nil {
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
// writeDefaultPointerLocked names accountID the scope's default, unconditionally.
//
// A PLAIN Put, deliberately, and it is SAFER here than a guarded pair rather
// than merely simpler. Naming a default is an operator UPDATE — SetDefault, or a
// create that explicitly asked for it — so last-writer-wins is the correct
// semantics for two operators racing. More importantly, a plain Put never leaves
// the pointer ABSENT: a concurrent create's PutIfAbsent correctly loses to
// whatever pointer exists before or after the re-point, and a healer's
// CompareAndDelete can only remove the exact orphan value it observed. Expressing
// this as CompareAndDelete-then-PutIfAbsent would MANUFACTURE a vacant moment,
// and a create landing in it would take the default the operator was in the
// middle of setting.
//
// A conditional re-point — "make me the default only if X still is" — is not
// expressible on this plane today; it needs a guarded swap (go-qrdb#56). Until
// then last-writer-wins is the design, not a compromise.
//
// Callers validate that the target account exists before reaching here:
// setDefault loads the record first, and create has just written it. Pointing at
// a never-existent ID is therefore not constructible from a typo. An account
// deleted between that check and this write leaves an orphaned pointer, which
// electDefaultLocked heals.
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

// accountTouchAttempts bounds how often Touch re-reads and re-applies after
// losing its guarded edit to a concurrent writer. Touch applies field-level
// changes to whatever record it reads, so re-applying after a loss is always
// correct; the bound only stops a pathological store from spinning here.
const accountTouchAttempts = 5

func (s *accountStore) touch(ctx context.Context, tenantID, accountID string, opts TouchAccountOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if err := scopeCheck(ctx, tenantID); err != nil {
		return err
	}
	recordKey, err := accountRecordKey(tenantID, accountID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(recordKey)
	defer unlock()

	for attempt := 0; attempt < accountTouchAttempts; attempt++ {
		account, raw, err := s.loadRaw(ctx, recordKey, tenantID, accountID)
		if err != nil {
			return err
		}
		// Checked on the record this attempt commits over, so it is decided
		// atomically with the write: an account switched to a reference after
		// the caller read it is refused here, not written to.
		if err := touchPrecondition(account, opts); err != nil {
			return err
		}
		applyTouch(account, opts, time.Now().UTC())

		data, marshalErr := marshalAccount(account)
		if marshalErr != nil {
			return marshalErr
		}
		swapped, err := s.commitRecord(ctx, recordKey, raw, data)
		if err != nil {
			return err
		}
		if swapped {
			return nil
		}
	}
	return &AccountVersionConflictError{TenantID: tenantID, AccountID: accountID}
}

// touchPrecondition decides opts' conditions against account, the record the
// touch is about to commit over. Decided there, and not on a copy the caller
// read earlier, the check and the write are one guarded step.
func touchPrecondition(account *Account, opts TouchAccountOptions) error {
	changed := func() error {
		return &AccountCredentialSourceChangedError{
			TenantID: account.TenantID, AccountID: account.ID, Ref: account.CredentialSecretRef,
		}
	}
	if opts.RequireInlineCredential && account.UsesSecretRef() {
		return changed()
	}
	if opts.RequireSource != nil && !opts.RequireSource.matches(account) {
		return changed()
	}
	if opts.PreserveRevoked && account.Status == AccountStatusRevoked {
		return &AccountRevokedError{AccountID: account.ID}
	}
	return nil
}

// applyTouch applies opts to account in memory, stamping now.
func applyTouch(account *Account, opts TouchAccountOptions, now time.Time) {
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
	if opts.MarkCredentialRotated {
		account.CredentialRotatedAt = now
	}
	// Monotonic: see TouchAccountOptions.SetCredentialGeneration. A reconciler
	// holding a superseded envelope must not walk the mirror back.
	if opts.SetCredentialGeneration > account.CredentialGeneration {
		account.CredentialGeneration = opts.SetCredentialGeneration
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
