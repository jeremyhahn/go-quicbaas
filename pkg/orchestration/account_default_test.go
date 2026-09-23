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
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// "The first account added to a scope becomes its default."
//
// The scope is the one AccountStore already keys its default pointer by:
// tenant + provider + project-scope ("_tenant" for an unscoped account). These
// tests pin that choice down from both ends — a new scope claims, an occupied
// scope declines — plus the two states an operator can otherwise get stuck in:
// a concurrent double-create and a deleted default.
// =============================================================================

func mustCreateAccount(t *testing.T, store AccountStore, account *Account) *Account {
	t.Helper()
	if err := store.Create(context.Background(), account); err != nil {
		t.Fatalf("Create(%s) error = %v", account.ID, err)
	}
	return account
}

func accountDefault(t *testing.T, store AccountStore, tenantID, accountID string) bool {
	t.Helper()
	got, err := store.Get(context.Background(), tenantID, accountID)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", accountID, err)
	}
	return got.Default
}

// defaultsInScope returns the IDs the store reports as default for exactly one
// tenant+provider+project scope. More than one is a bug: the scope's pointer
// names exactly one account.
//
// ListByTenant's empty projectID means "do not filter by project", not "the
// tenant-wide scope", so the unscoped case is narrowed here — a project-scoped
// account is in a DIFFERENT default scope and must not be counted against the
// tenant-wide one.
func defaultsInScope(t *testing.T, store AccountStore, tenantID string, provider AccountProvider, projectID string) []string {
	t.Helper()
	accounts, err := store.ListByTenant(context.Background(), tenantID, provider, projectID)
	if err != nil {
		t.Fatalf("ListByTenant() error = %v", err)
	}
	var ids []string
	for _, a := range accounts {
		if a.Default && a.ProjectID == projectID {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

func TestAccountStore_Create_FirstAccountInScopeBecomesDefault(t *testing.T) {
	store := NewInMemoryAccountStore()

	first := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-1", AccountProviderClaude))

	// Create sets the flag on the caller's own struct, so the value every
	// transport echoes back in its create response is already correct without
	// a follow-up read.
	if !first.Default {
		t.Fatalf("Create() left Default = false on the returned account; the first account in a scope must be its default")
	}
	if !accountDefault(t, store, "tenant-a", "acct-1") {
		t.Fatalf("stored acct-1.Default = false, want true")
	}
}

func TestAccountStore_Create_SecondAccountInScopeDoesNotDisplaceDefault(t *testing.T) {
	store := NewInMemoryAccountStore()

	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-1", AccountProviderClaude))
	second := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-2", AccountProviderClaude))

	if second.Default {
		t.Fatalf("acct-2.Default = true; only a vacant scope is claimed automatically")
	}
	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, ""); len(got) != 1 || got[0] != "acct-1" {
		t.Fatalf("defaults in scope = %v, want exactly [acct-1]", got)
	}
}

// TestAccountStore_Create_ExplicitDefaultDisplacesIncumbent proves the
// auto-claim did not take the ability to import an account AS the default away
// from an operator.
func TestAccountStore_Create_ExplicitDefaultDisplacesIncumbent(t *testing.T) {
	store := NewInMemoryAccountStore()

	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-1", AccountProviderClaude))

	second := newTestAccount("tenant-a", "acct-2", AccountProviderClaude)
	second.Default = true
	mustCreateAccount(t, store, second)

	if accountDefault(t, store, "tenant-a", "acct-1") {
		t.Fatalf("acct-1 still default after acct-2 was created as the default")
	}
	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, ""); len(got) != 1 || got[0] != "acct-2" {
		t.Fatalf("defaults in scope = %v, want exactly [acct-2]", got)
	}
}

// TestAccountStore_Create_DefaultScopeIsPerProvider proves an Anthropic account
// and an OpenAI account do not compete for one default: an unpinned claude
// spawn and an unpinned codex spawn resolve independently.
func TestAccountStore_Create_DefaultScopeIsPerProvider(t *testing.T) {
	store := NewInMemoryAccountStore()

	claude := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-claude", AccountProviderClaude))
	codex := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-codex", AccountProviderCodex))

	if !claude.Default || !codex.Default {
		t.Fatalf("Default flags = claude:%t codex:%t, want both true (separate provider scopes)", claude.Default, codex.Default)
	}
}

// TestAccountStore_Create_DefaultScopeIsPerTenant proves one tenant's first
// account never becomes another tenant's default.
func TestAccountStore_Create_DefaultScopeIsPerTenant(t *testing.T) {
	store := NewInMemoryAccountStore()

	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-a", AccountProviderClaude))
	b := mustCreateAccount(t, store, newTestAccount("tenant-b", "acct-b", AccountProviderClaude))

	if !b.Default {
		t.Fatalf("tenant-b's first account is not its default; tenant-a's occupancy leaked across the tenant boundary")
	}
}

// TestAccountStore_Create_ProjectScopedAccountClaimsOnlyItsProject pins the
// scope choice at its most contentious edge.
//
// AccountResolver partitions candidates the same way: a project-scoped
// resolution sees only that project's accounts, falling back to the tenant-wide
// pool ONLY when the project pool is empty. So a project-scoped account that
// claimed the tenant-wide default would silently become what every OTHER
// project's unpinned spawn uses.
func TestAccountStore_Create_ProjectScopedAccountClaimsOnlyItsProject(t *testing.T) {
	store := NewInMemoryAccountStore()

	tenantWide := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-wide", AccountProviderClaude))

	scoped := newTestAccount("tenant-a", "acct-proj", AccountProviderClaude)
	scoped.ProjectID = "proj-1"
	mustCreateAccount(t, store, scoped)

	if !scoped.Default {
		t.Fatalf("first account of project proj-1 is not that project's default")
	}
	if !accountDefault(t, store, "tenant-a", tenantWide.ID) {
		t.Fatalf("the project-scoped account displaced the tenant-wide default")
	}
	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, "proj-1"); len(got) != 1 || got[0] != "acct-proj" {
		t.Fatalf("defaults in proj-1 = %v, want exactly [acct-proj]", got)
	}
}

// TestAccountStore_Create_ConcurrentFirstCreates_ExactlyOneDefault is the
// race-safety requirement: N operators (or N transports) importing into an
// empty scope at the same instant must produce exactly one default — never two
// (an ambiguous pool that still resolves, silently, to whichever the scan hits
// first) and never zero (a pool of N accounts that refuses to resolve).
func TestAccountStore_Create_ConcurrentFirstCreates_ExactlyOneDefault(t *testing.T) {
	store := NewInMemoryAccountStore()

	const creators = 16
	accounts := make([]*Account, creators)
	for i := range accounts {
		accounts[i] = newTestAccount("tenant-a", "acct-"+string(rune('a'+i)), AccountProviderClaude)
	}

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	errs := make([]error, creators)
	for i := range accounts {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			errs[i] = store.Create(context.Background(), accounts[i])
		}(i)
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Create(%s) error = %v", accounts[i].ID, err)
		}
	}

	// The in-place flag each caller observed, and the durable records, must
	// agree on the same single winner.
	var claimedInPlace []string
	for _, a := range accounts {
		if a.Default {
			claimedInPlace = append(claimedInPlace, a.ID)
		}
	}
	if len(claimedInPlace) != 1 {
		t.Fatalf("%d creators observed Default = true (%v), want exactly 1", len(claimedInPlace), claimedInPlace)
	}
	stored := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, "")
	if len(stored) != 1 || stored[0] != claimedInPlace[0] {
		t.Fatalf("stored defaults = %v, want exactly [%s]", stored, claimedInPlace[0])
	}
}

// TestAccountStore_Delete_DefaultLeavesScopeVacantWithoutPromotion documents
// the chosen delete behavior: no survivor is promoted.
func TestAccountStore_Delete_DefaultLeavesScopeVacantWithoutPromotion(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-1", AccountProviderClaude))
	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-2", AccountProviderClaude))
	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-3", AccountProviderClaude))

	if err := store.Delete(ctx, "tenant-a", "acct-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, ""); len(got) != 0 {
		t.Fatalf("defaults after deleting the default = %v, want none — promoting a survivor would silently redirect unpinned spawns", got)
	}

	// The vacancy is not a dead end: the next account added claims it, exactly
	// as the first one did.
	fourth := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-4", AccountProviderClaude))
	if !fourth.Default {
		t.Fatalf("acct-4.Default = false; a scope left vacant by a delete must be claimable again")
	}
}

// TestAccountStore_Delete_NonDefaultLeavesDefaultIntact guards the inverse:
// deleting a spare must not disturb the pointer.
func TestAccountStore_Delete_NonDefaultLeavesDefaultIntact(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-1", AccountProviderClaude))
	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-2", AccountProviderClaude))

	if err := store.Delete(ctx, "tenant-a", "acct-2"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, ""); len(got) != 1 || got[0] != "acct-1" {
		t.Fatalf("defaults = %v, want exactly [acct-1]", got)
	}
}

// TestAccountStore_Create_HealsDefaultPointerStrandedOnDeletedAccount covers
// the crash window between the two KV writes a delete performs. A pointer left
// naming a record that is gone must not lock the scope out of ever getting a
// default again.
func TestAccountStore_Create_HealsDefaultPointerStrandedOnDeletedAccount(t *testing.T) {
	kv := NewMemoryTaskKV()
	store := NewAccountStore(kv)
	ctx := context.Background()

	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-1", AccountProviderClaude))

	// Simulate the crash: drop the record, leave the pointer.
	recordKey, err := accountRecordKey("tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("accountRecordKey() error = %v", err)
	}
	if err := kv.Delete(ctx, recordKey); err != nil {
		t.Fatalf("Delete(record) error = %v", err)
	}

	healed := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-2", AccountProviderClaude))
	if !healed.Default {
		t.Fatalf("acct-2.Default = false; a pointer stranded on a deleted account must count as a vacant scope")
	}
	defaultKey, err := accountDefaultKey("tenant-a", AccountProviderClaude, "")
	if err != nil {
		t.Fatalf("accountDefaultKey() error = %v", err)
	}
	pointer, err := kv.Get(ctx, defaultKey)
	if err != nil {
		t.Fatalf("Get(pointer) error = %v", err)
	}
	if string(pointer) != "acct-2" {
		t.Fatalf("default pointer = %q, want acct-2", string(pointer))
	}
}

// TestAccountStore_Create_InvalidAccountNeverTouchesDefaultPointer is the
// error-path half: a rejected create must leave the scope exactly as it was.
func TestAccountStore_Create_InvalidAccountNeverTouchesDefaultPointer(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	// A provider name that is not a legal key segment cannot be turned into a
	// default-pointer key at all.
	err := store.Create(ctx, &Account{ID: "acct-bad", TenantID: "tenant-a", Provider: AccountProvider("bad/provider")})
	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Create() error = %v (%T), want *AccountInvalidError", err, err)
	}

	good := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-good", AccountProviderClaude))
	if !good.Default {
		t.Fatalf("a rejected create consumed the scope's vacancy")
	}
}

// =============================================================================
// Moving an account between default scopes.
//
// Update can change an account's ProjectID, which moves it from one default
// scope to another. The flag must be re-evaluated for the destination, or a
// scope ends up with two flagged candidates and StaticAccountStrategy silently
// picks whichever the scan reaches first.
// =============================================================================

func TestAccountStore_Update_MoveToOccupiedScope_ArrivesNonDefault(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	// proj-1 already has a default of its own.
	incumbent := newTestAccount("tenant-a", "acct-incumbent", AccountProviderClaude)
	incumbent.ProjectID = "proj-1"
	mustCreateAccount(t, store, incumbent)

	// A tenant-wide account that claimed the tenant-wide scope.
	mover := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-mover", AccountProviderClaude))
	if !mover.Default {
		t.Fatalf("precondition: the tenant-wide account should have claimed its scope")
	}

	mover.ProjectID = "proj-1"
	if err := store.Update(ctx, mover); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, "proj-1"); len(got) != 1 || got[0] != "acct-incumbent" {
		t.Fatalf("defaults in proj-1 = %v, want exactly [acct-incumbent]", got)
	}
	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, ""); len(got) != 0 {
		t.Fatalf("defaults in the tenant-wide scope = %v, want none — the mover left it", got)
	}
}

func TestAccountStore_Update_MoveToVacantScope_ClaimsIt(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	mover := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-mover", AccountProviderClaude))
	mover.ProjectID = "proj-empty"
	if err := store.Update(ctx, mover); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if !accountDefault(t, store, "tenant-a", "acct-mover") {
		t.Fatalf("the account did not claim the vacant destination scope")
	}
	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, "proj-empty"); len(got) != 1 || got[0] != "acct-mover" {
		t.Fatalf("defaults in proj-empty = %v, want exactly [acct-mover]", got)
	}
	// And the scope it left is genuinely released, not just flagged false.
	successor := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-successor", AccountProviderClaude))
	if !successor.Default {
		t.Fatalf("the tenant-wide scope was not released when its default moved away")
	}
}

// TestAccountStore_Update_WithinSameScope_LeavesDefaultAlone guards the common
// path: renaming an account must not disturb the pointer.
func TestAccountStore_Update_WithinSameScope_LeavesDefaultAlone(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	first := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-1", AccountProviderClaude))
	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-2", AccountProviderClaude))

	first.DisplayName = "renamed"
	if err := store.Update(ctx, first); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, ""); len(got) != 1 || got[0] != "acct-1" {
		t.Fatalf("defaults = %v, want exactly [acct-1]", got)
	}
}

// =============================================================================
// Deadlock regression.
//
// The default pointer used to be mirrored onto each Account.Default and kept in
// step by clearing the flag on the PREVIOUS holder's record. That clear ran
// while the scope's defaultMu was held, so the lock order was
// recordMu(A) -> defaultMu(scope) -> recordMu(B). Two SetDefault calls on two
// accounts in one scope that are each other's incumbent inverted it and wedged
// permanently:
//
//	X: holds recordMu(A) + defaultMu, wants recordMu(B)
//	Y: holds recordMu(B),            wants defaultMu
//
// keyedMutex has no timeout, so both request goroutines leaked until process
// exit. Deriving Default from the pointer removed the mirror, and with it the
// second record lock: defaultMu is now a leaf and the inversion cannot be
// expressed. This test wedged and timed out against the mirrored implementation.
// =============================================================================

func TestAccountStore_SetDefault_ConcurrentMutualDisplacement_DoesNotDeadlock(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	first := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-a", AccountProviderClaude))
	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-b", AccountProviderClaude))
	if !first.Default {
		t.Fatalf("precondition: acct-a should hold the scope default")
	}

	// Each goroutine repeatedly makes its own account the default, so every
	// iteration displaces the account the other goroutine is working on — the
	// exact mutual-incumbent pattern that inverted the old lock order.
	const rounds = 500
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []string{"acct-a", "acct-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if err := store.SetDefault(ctx, "tenant-a", id); err != nil {
					errs <- err
					return
				}
			}
		}(id)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case err := <-errs:
		t.Fatalf("SetDefault() error = %v", err)
	case <-time.After(30 * time.Second):
		t.Fatalf("concurrent mutual SetDefault deadlocked: the default pointer swap must never hold defaultMu while taking another account's record lock")
	}
	select {
	case err := <-errs:
		t.Fatalf("SetDefault() error = %v", err)
	default:
	}

	// However the interleaving landed, the scope has exactly one default.
	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, ""); len(got) != 1 {
		t.Fatalf("defaults after the storm = %v, want exactly one", got)
	}
}

// =============================================================================
// Default is derived, never persisted.
// =============================================================================

// TestAccountStore_DefaultIsNotPersisted proves the stored record carries no
// default of its own. If it did, the record and the pointer could disagree —
// and StaticAccountStrategy selects on this field, so a stale "true" silently
// redirects an unpinned spawn, and its billing, to another provider login.
func TestAccountStore_DefaultIsNotPersisted(t *testing.T) {
	kv := NewMemoryTaskKV()
	store := NewAccountStore(kv)
	ctx := context.Background()

	account := mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-1", AccountProviderClaude))
	if !account.Default {
		t.Fatalf("precondition: the first account should hold the scope default")
	}

	recordKey, err := accountRecordKey("tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("accountRecordKey() error = %v", err)
	}
	raw, err := kv.Get(ctx, recordKey)
	if err != nil {
		t.Fatalf("kv.Get() error = %v", err)
	}
	var stored struct {
		Default bool `json:"default"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if stored.Default {
		t.Fatalf("the durable record persisted default=true; the scope pointer is the only authority")
	}

	// And the read still reports it, because it is derived.
	loaded, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !loaded.Default {
		t.Fatalf("Get() reported Default=false; the field must be derived from the scope pointer")
	}
}

// TestAccountStore_DerivedDefaultFollowsPointer_WithoutRecordWrites is the
// sharp edge of deriving: moving the pointer changes what BOTH accounts report
// with no record write at all, so no window exists where two are flagged.
func TestAccountStore_DerivedDefaultFollowsPointer_WithoutRecordWrites(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-a", AccountProviderClaude))
	mustCreateAccount(t, store, newTestAccount("tenant-a", "acct-b", AccountProviderClaude))

	if err := store.SetDefault(ctx, "tenant-a", "acct-b"); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}

	a, err := store.Get(ctx, "tenant-a", "acct-a")
	if err != nil {
		t.Fatalf("Get(acct-a) error = %v", err)
	}
	b, err := store.Get(ctx, "tenant-a", "acct-b")
	if err != nil {
		t.Fatalf("Get(acct-b) error = %v", err)
	}
	if a.Default {
		t.Fatalf("acct-a still reports Default after the pointer moved to acct-b")
	}
	if !b.Default {
		t.Fatalf("acct-b does not report Default after the pointer moved to it")
	}

	// A stranded pointer (crash between the pointer write and the record write)
	// leaves the scope with NO default rather than a phantom one.
	if err := store.Delete(ctx, "tenant-a", "acct-b"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := defaultsInScope(t, store, "tenant-a", AccountProviderClaude, ""); len(got) != 0 {
		t.Fatalf("defaults after deleting the holder = %v, want none", got)
	}
}

// TestAccount_MarshalJSON_EmitsDefault covers the three transports that do NOT
// reproject an Account and instead serialize the service response directly —
// QUIC, MCP, and Unix. For those, Account.MarshalJSON IS the wire contract, so
// the derived default reaches the client only if it is emitted here.
//
// Deriving Default deliberately changed only where the value is STORED, never
// where it appears. This test is the guard that the wire shape did not move:
// dropping "default" (e.g. by adding omitempty while making it derived) would
// silently blank the field on exactly those three protocols.
func TestAccount_MarshalJSON_EmitsDefault(t *testing.T) {
	for _, isDefault := range []bool{true, false} {
		data, err := json.Marshal(Account{
			ID:       "acct-1",
			TenantID: "tenant-a",
			Provider: AccountProviderClaude,
			Default:  isDefault,
		})
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		var wire map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		got, present := wire["default"]
		if !present {
			t.Fatalf("the wire shape has no \"default\" key; QUIC, MCP and Unix serialize Account directly (wire = %v)", wire)
		}
		if got != isDefault {
			t.Fatalf("wire default = %v, want %v", got, isDefault)
		}
	}
}

// =============================================================================
// Multi-node account creation and the default election
// =============================================================================

// accountHoldBeforeRecord parks one node between claiming a scope's default
// pointer and writing its own record — the window in which a slow creator's
// claim can be healed away by another node that reasonably concludes the scope
// is orphaned.
//
// The hold is keyed on the RECORD write specifically, so the pointer claim has
// already happened when the node parks. That is the state the hazard needs.
type accountHoldBeforeRecord struct {
	interfaces.KVStore

	recordKey string
	parked    chan struct{}
	release   chan struct{}
	once      sync.Once
}

func newAccountHoldBeforeRecord(inner interfaces.KVStore, recordKey string) *accountHoldBeforeRecord {
	return &accountHoldBeforeRecord{
		KVStore:   inner,
		recordKey: recordKey,
		parked:    make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (k *accountHoldBeforeRecord) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if key == k.recordKey {
		k.once.Do(func() { close(k.parked) })
		select {
		case <-k.release:
		case <-time.After(barrierWait):
		}
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *accountHoldBeforeRecord) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	deleter, ok := k.KVStore.(guardedDeleter)
	if !ok {
		return false, errors.New("test wrapper: inner store cannot guard a delete")
	}
	return deleter.CompareAndDelete(ctx, key, expected)
}

// TestAccountStore_Create_SlowCreatorDoesNotClaimADefaultItLost is the
// slow-creator hazard, and the reason a create's success is only confirmed
// AFTER its record write.
//
// Node A claims the vacant scope and then stalls before writing its record.
// Node B arrives, finds the pointer naming an account whose record does not
// exist, correctly concludes the scope is orphaned, heals it and takes the
// default. A then completes: its own record write succeeds, because its record
// key is uncontested.
//
// Without the post-write re-read, A returns Default:true for a scope B owns —
// two accounts each believing they are the default, and nothing detecting it.
// The assertion is that A is told the truth, not that A fails: A's account is
// perfectly good, it simply is not the default. Losing an election it was not
// party to must not invalidate the record.
func TestAccountStore_Create_SlowCreatorDoesNotClaimADefaultItLost(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()

	slowKey, err := accountRecordKey("tenant-a", "acct-slow")
	if err != nil {
		t.Fatalf("accountRecordKey() error = %v", err)
	}
	held := newAccountHoldBeforeRecord(shared, slowKey)

	slowNode := NewAccountStore(held)
	fastNode := NewAccountStore(shared)

	var (
		wg       sync.WaitGroup
		slowAcct = newTestAccount("tenant-a", "acct-slow", AccountProviderClaude)
		slowErr  error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		slowErr = slowNode.Create(ctx, slowAcct)
	}()

	select {
	case <-held.parked:
	case <-time.After(barrierWait):
		t.Fatal("the slow node never reached its record write")
	}

	// The slow node holds the pointer but has no record, which is exactly what
	// an orphaned scope looks like from outside.
	fastAcct := newTestAccount("tenant-a", "acct-fast", AccountProviderClaude)
	if err := fastNode.Create(ctx, fastAcct); err != nil {
		t.Fatalf("fast node Create() error = %v", err)
	}
	if !fastAcct.Default {
		t.Fatal("the fast node healed an orphaned scope but was not made its default")
	}

	close(held.release)
	wg.Wait()

	if slowErr != nil {
		t.Fatalf("the slow node's create failed; losing the election must not invalidate the account: %v", slowErr)
	}
	if slowAcct.Default {
		t.Fatal("the slow node was told it is the scope default, but the fast node holds it: " +
			"two accounts each believe they are the default and nothing detects it")
	}

	// The stored pointer is the authority and must name exactly one of them.
	defaultKey, err := accountDefaultKey("tenant-a", AccountProviderClaude, "")
	if err != nil {
		t.Fatalf("accountDefaultKey() error = %v", err)
	}
	pointer, err := shared.Get(ctx, defaultKey)
	if err != nil {
		t.Fatalf("Get(pointer) error = %v", err)
	}
	if string(pointer) != "acct-fast" {
		t.Fatalf("default pointer = %q, want acct-fast", string(pointer))
	}
}

// TestAccountStore_Create_NineNodesOneScopeElectOneDefault proves the scope
// default is a single-winner election across the fleet.
//
// Nine DIFFERENT accounts in one scope, created at once. All nine must exist —
// they are distinct accounts and none of them is in conflict — and the scope
// must end with exactly one default.
//
// WHERE THE SINGLE WINNER IS READ, AND WHY NOT FROM THE RETURN VALUES (#244).
// This test used to count how many of the nine *returned* Account structs came
// back with Default set, and it flaked at 0.00s with "2 accounts claim the
// scope default" — 99 times in 4000 concurrent rounds under -race (~2.5%), on
// CI in PR #233's Unit leg.
//
// That count is not a property of the store. Default is DERIVED on every read
// from the scope pointer (accountStore.deriveDefault) and is never persisted —
// marshalAccount zeroes it before the record is written. So the value a
// concurrent Create hands back is a snapshot of the pointer at the instant that
// call re-read it, and the losing creates are still healing and re-claiming the
// pointer around it: node A can re-read the pointer while it still names A, and
// only afterwards does B's claim land, so both callers were told the truth at
// the moment they asked and the two moments disagree. No implementation can
// make nine snapshots taken at nine different instants agree on a value that is
// by design only ever true "as of now" — the old assertion required an
// impossible ordering, which is why it failed intermittently rather than
// consistently.
//
// The durable state was never wrong: across those same 4000 rounds the pointer
// was non-empty every time and a fresh read of all nine accounts reported
// exactly one default every time — 0 violations. So the invariant is asserted
// where it actually lives, against reads taken after the race has quiesced.
func TestAccountStore_Create_NineNodesOneScopeElectOneDefault(t *testing.T) {
	const nodes = 9

	ctx := context.Background()
	shared := NewMemoryTaskKV()

	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		accounts = make([]*Account, nodes)
		errs     = make([]error, nodes)
	)
	for i := 0; i < nodes; i++ {
		accounts[i] = newTestAccount("tenant-elect", "acct-"+strconv.Itoa(i), AccountProviderClaude)
		wg.Add(1)
		go func(node int) {
			defer wg.Done()
			store := NewAccountStore(shared)
			<-start
			errs[node] = store.Create(ctx, accounts[node])
		}(i)
	}
	close(start)
	wg.Wait()

	for node, err := range errs {
		if err != nil {
			t.Fatalf("node %d Create() error = %v; distinct accounts must all be created", node, err)
		}
	}

	// Every read below happens after wg.Wait(), so nothing is still writing and
	// these are stable facts rather than another snapshot of a live race.
	reader := NewAccountStore(shared)

	winners := make([]string, 0, nodes)
	for i := 0; i < nodes; i++ {
		id := "acct-" + strconv.Itoa(i)
		got, err := reader.Get(ctx, "tenant-elect", id)
		if err != nil {
			t.Fatalf("Get(%s) error = %v; all nine distinct accounts must exist", id, err)
		}
		if got.Default {
			winners = append(winners, id)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("%d accounts read back as the scope default (%v), want exactly 1", len(winners), winners)
	}

	defaultKey, err := accountDefaultKey("tenant-elect", AccountProviderClaude, "")
	if err != nil {
		t.Fatalf("accountDefaultKey() error = %v", err)
	}
	pointer, err := shared.Get(ctx, defaultKey)
	if err != nil {
		t.Fatalf("Get(pointer) error = %v", err)
	}
	if string(pointer) != winners[0] {
		t.Fatalf("default pointer = %q, but %q reads back as the default", string(pointer), winners[0])
	}

	// The winner's OWN create must have been told it won. This is assertable
	// where the nine-way count is not: a node elects once, and a node that has
	// been displaced never re-claims, so the account the pointer ends on is one
	// that claimed the pointer and was never displaced after. Its re-derive runs
	// after its own claim, so it necessarily read the pointer naming itself.
	// Losers may also have been told they won -- that is the snapshot this test
	// no longer counts -- but the eventual winner being told it lost would mean
	// the caller that owns the scope was never informed of it.
	for node, account := range accounts {
		if account.ID != winners[0] {
			continue
		}
		if !account.Default {
			t.Errorf("node %d owns the scope (pointer = %q) but its Create() returned Default=false",
				node, string(pointer))
		}
	}

	// The election is only single-winner BECAUSE the flag lives in one place.
	// If a future change persists Default on the record, the reads above would
	// agree with themselves while the scope quietly gained a second default that
	// no pointer arbitrates — so assert the record never carries it.
	for i := 0; i < nodes; i++ {
		id := "acct-" + strconv.Itoa(i)
		recordKey, err := accountRecordKey("tenant-elect", id)
		if err != nil {
			t.Fatalf("accountRecordKey(%s) error = %v", id, err)
		}
		raw, err := shared.Get(ctx, recordKey)
		if err != nil {
			t.Fatalf("Get(record %s) error = %v", id, err)
		}
		var stored Account
		if err := json.Unmarshal(raw, &stored); err != nil {
			t.Fatalf("unmarshal record %s: %v", id, err)
		}
		if stored.Default {
			t.Errorf("record %s persists Default=true; the scope pointer must be the only place the default is recorded", id)
		}
	}
}

// TestAccountStore_Create_NineNodesOneIDProduceOneAccount is the create-side
// race: one ID, nine nodes, exactly one record.
func TestAccountStore_Create_NineNodesOneIDProduceOneAccount(t *testing.T) {
	const nodes = 9

	ctx := context.Background()
	shared := NewMemoryTaskKV()

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  = make([]error, nodes)
	)
	for i := 0; i < nodes; i++ {
		wg.Add(1)
		go func(node int) {
			defer wg.Done()
			store := NewAccountStore(shared)
			acct := newTestAccount("tenant-dup", "acct-contested", AccountProviderClaude)
			<-start
			errs[node] = store.Create(ctx, acct)
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
			continue
		}
		var invalid *AccountInvalidError
		if !errors.As(err, &invalid) {
			t.Fatalf("a losing create returned %T: %v, want *AccountInvalidError", err, err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d nodes created one account ID, want exactly 1", winners)
	}
}

// accountHoldBeforeHealDelete parks a healer between OBSERVING an orphaned
// default pointer and DELETING it — the only window in which a guarded heal and
// an unguarded one behave differently.
//
// Everywhere else the two are indistinguishable, which is why a test that only
// proves the heal happens cannot tell them apart: it never constructs the case
// the guard exists for.
type accountHoldBeforeHealDelete struct {
	interfaces.KVStore

	defaultKey string
	parked     chan struct{}
	release    chan struct{}
	once       sync.Once
}

func newAccountHoldBeforeHealDelete(inner interfaces.KVStore, defaultKey string) *accountHoldBeforeHealDelete {
	return &accountHoldBeforeHealDelete{
		KVStore:    inner,
		defaultKey: defaultKey,
		parked:     make(chan struct{}),
		release:    make(chan struct{}),
	}
}

func (k *accountHoldBeforeHealDelete) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

// park holds this caller until the test releases it.
func (k *accountHoldBeforeHealDelete) park() {
	k.once.Do(func() { close(k.parked) })
	select {
	case <-k.release:
	case <-time.After(barrierWait):
	}
}

// CompareAndDelete parks before the delete, so the pointer can be moved
// underneath this caller by a healer that got there first.
func (k *accountHoldBeforeHealDelete) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	if key == k.defaultKey {
		k.park()
	}
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

// Delete parks at the SAME logical point, and this is not redundant: an
// unguarded heal reaches the pointer through Delete rather than
// CompareAndDelete, so a barrier hooked only to the guarded verb holds nothing
// once the guard is removed. The test would then hang on its own timeout
// instead of failing on the damage — a harness pinned to a hazard the mutation
// deletes, which is the vacuous-pass shape this suite has hit before.
func (k *accountHoldBeforeHealDelete) Delete(ctx context.Context, key string) error {
	if key == k.defaultKey {
		k.park()
	}
	return k.KVStore.Delete(ctx, key)
}

// TestAccountStore_Create_HealerDoesNotDeleteAPointerThatMoved proves the guard
// on the heal, not merely that healing works.
//
// Two nodes find the same orphaned pointer — one naming an account whose record
// is gone — and both decide to heal it. One completes: it clears the orphan and
// takes the scope. The other is parked between its observation and its delete,
// so by the time it acts, the pointer no longer names the orphan it saw. Its
// CompareAndDelete must remove NOTHING.
//
// With an unguarded Delete the late healer removes the WINNER's pointer and then
// claims the scope itself, so the winner is left believing it holds a default
// that now names someone else — the erasure class again, inside the repair path
// meant to prevent it. The assertion is therefore on the pointer, not on the
// loser's verdict: a test checking only "the late healer lost" passes under both
// implementations, because under the mutation it does not lose.
func TestAccountStore_Create_HealerDoesNotDeleteAPointerThatMoved(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()

	// An orphaned scope: acct-dead held the default, then its record vanished.
	seed := NewAccountStore(shared)
	mustCreateAccount(t, seed, newTestAccount("tenant-heal", "acct-dead", AccountProviderClaude))
	deadKey, err := accountRecordKey("tenant-heal", "acct-dead")
	if err != nil {
		t.Fatalf("accountRecordKey() error = %v", err)
	}
	if err := shared.Delete(ctx, deadKey); err != nil {
		t.Fatalf("Delete(record) error = %v", err)
	}

	defaultKey, err := accountDefaultKey("tenant-heal", AccountProviderClaude, "")
	if err != nil {
		t.Fatalf("accountDefaultKey() error = %v", err)
	}

	held := newAccountHoldBeforeHealDelete(shared, defaultKey)
	lateHealer := NewAccountStore(held)

	var (
		wg      sync.WaitGroup
		lateAcc = newTestAccount("tenant-heal", "acct-late", AccountProviderClaude)
		lateErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		lateErr = lateHealer.Create(ctx, lateAcc)
	}()

	select {
	case <-held.parked:
	case <-time.After(barrierWait):
		t.Fatal("the late healer never reached its delete")
	}

	// The winner heals the orphan and takes the scope while the other is parked.
	winner := newTestAccount("tenant-heal", "acct-winner", AccountProviderClaude)
	if err := NewAccountStore(shared).Create(ctx, winner); err != nil {
		t.Fatalf("winner Create() error = %v", err)
	}
	if !winner.Default {
		t.Fatal("the winner healed the orphaned scope but was not made its default")
	}

	close(held.release)
	wg.Wait()

	if lateErr != nil {
		t.Fatalf("the late healer's create failed; losing a heal race must not invalidate the account: %v", lateErr)
	}

	// THE ASSERTION THAT DISTINGUISHES GUARDED FROM UNGUARDED: the winner's
	// pointer must survive. An unguarded delete removes it here.
	pointer, err := shared.Get(ctx, defaultKey)
	if err != nil {
		t.Fatalf("Get(pointer) error = %v", err)
	}
	if string(pointer) != "acct-winner" {
		t.Fatalf("default pointer = %q, want acct-winner: the late healer deleted a pointer that had "+
			"already moved, and the winner now believes it holds a default naming someone else",
			string(pointer))
	}
	if lateAcc.Default {
		t.Error("the late healer claimed a scope whose orphan it never actually removed")
	}
}

// =============================================================================
// Moving between scopes
// =============================================================================

// moverBarrier gathers every mover at its FIRST touch of the destination
// pointer, releasing them only once all have arrived.
//
// One barrier, one wrapper per node, because the counting must be per NODE and
// each node touches the pointer more than once. It hooks BOTH routes — Get for
// a probe-then-write implementation, PutIfAbsent for a guarded one — because
// the two reach the pointer through different verbs, and a barrier hooked to
// only one holds nothing against the other.
//
// This is not a precaution. Without it the nine movers interleave freely: the
// first to write is usually seen by the rest, they decline, and an UNGUARDED
// election passes the test. Measured, not assumed — the first version of this
// test survived the probe-then-write mutation 10 runs out of 10.
type moverBarrier struct {
	nodes int

	mu      sync.Mutex
	arrived int
	gate    chan struct{}
}

func newMoverBarrier(nodes int) *moverBarrier {
	return &moverBarrier{nodes: nodes, gate: make(chan struct{})}
}

func (b *moverBarrier) arrive() {
	b.mu.Lock()
	b.arrived++
	// Equality rather than >=, so a later arrival cannot close a closed gate.
	if b.arrived == b.nodes {
		close(b.gate)
	}
	b.mu.Unlock()

	select {
	case <-b.gate:
	case <-time.After(barrierWait):
	}
}

// moverBarrierKV is one node's view of the shared barrier.
type moverBarrierKV struct {
	interfaces.KVStore

	key     string
	barrier *moverBarrier
	once    sync.Once
}

func (k *moverBarrierKV) arriveOnce() {
	k.once.Do(k.barrier.arrive)
}

func (k *moverBarrierKV) Get(ctx context.Context, key string) ([]byte, error) {
	if key == k.key {
		k.arriveOnce()
	}
	return k.KVStore.Get(ctx, key)
}

func (k *moverBarrierKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if key == k.key {
		k.arriveOnce()
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *moverBarrierKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

func (k *moverBarrierKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	return forwardCompareAndSwap(ctx, k.KVStore, key, expected, next)
}

// TestAccountStore_Update_NineMoversIntoOneScopeElectOneDefault proves the
// destination of a move is a single-winner election, not a vacancy probe.
//
// This was the last read-then-write election in the store: nine accounts moving
// into one empty scope could all observe it vacant and all claim it, because
// the probe and the write are two operations with a process-local lock between
// them.
func TestAccountStore_Update_NineMoversIntoOneScopeElectOneDefault(t *testing.T) {
	const movers = 9

	ctx := context.Background()
	shared := NewMemoryTaskKV()
	seed := NewAccountStore(shared)

	// Each account starts in its own project scope, so none of them is moving
	// out of a scope another one cares about.
	accounts := make([]*Account, movers)
	for i := range accounts {
		acct := newTestAccount("tenant-move", "acct-"+strconv.Itoa(i), AccountProviderClaude)
		acct.ProjectID = "origin-" + strconv.Itoa(i)
		mustCreateAccount(t, seed, acct)
		accounts[i] = acct
	}

	destKey, err := accountDefaultKey("tenant-move", AccountProviderClaude, "destination")
	if err != nil {
		t.Fatalf("accountDefaultKey() error = %v", err)
	}
	barrier := newMoverBarrier(movers)

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  = make([]error, movers)
	)
	for i := range accounts {
		wg.Add(1)
		go func(node int) {
			defer wg.Done()
			// One wrapper per node: the barrier counts NODES, and each node
			// touches the destination pointer more than once.
			store := NewAccountStore(&moverBarrierKV{KVStore: shared, key: destKey, barrier: barrier})
			moved := *accounts[node]
			moved.ProjectID = "destination"
			<-start
			errs[node] = store.Update(ctx, &moved)
			accounts[node] = &moved
		}(i)
	}
	close(start)
	wg.Wait()

	defaults := 0
	winner := -1
	for node, err := range errs {
		if err != nil {
			t.Fatalf("node %d Update() error = %v; a move into an occupied scope is not an error", node, err)
		}
		if accounts[node].Default {
			defaults++
			winner = node
		}
	}
	if defaults != 1 {
		t.Fatalf("%d movers claim the destination default, want exactly 1", defaults)
	}

	pointer, err := shared.Get(ctx, destKey)
	if err != nil {
		t.Fatalf("Get(pointer) error = %v", err)
	}
	if want := "acct-" + strconv.Itoa(winner); string(pointer) != want {
		t.Fatalf("destination pointer = %q, but %q was told it won", string(pointer), want)
	}
}

// TestAccountStore_Update_MoverIsToldTheTruthWhenAnOperatorTakesTheScope proves
// the returned Default is derived from the pointer after the election, not from
// the election's own verdict.
//
// SetDefault is a plain last-writer-wins Put — deliberately, because a plain Put
// never exposes a vacant moment for a bystander to claim — so an operator can
// legitimately take the scope immediately after a mover won it. The mover must
// then be told it is not the default, because that is what every later read
// will say.
func TestAccountStore_Update_MoverIsToldTheTruthWhenAnOperatorTakesTheScope(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	store := NewAccountStore(shared)

	// The operator's account already lives in the destination scope.
	operatorAcct := newTestAccount("tenant-truth", "acct-operator", AccountProviderClaude)
	operatorAcct.ProjectID = "destination"
	mustCreateAccount(t, store, operatorAcct)

	mover := newTestAccount("tenant-truth", "acct-mover", AccountProviderClaude)
	mover.ProjectID = "origin"
	mustCreateAccount(t, store, mover)

	// The destination is occupied, so the mover must not become its default.
	moved := *mover
	moved.ProjectID = "destination"
	if err := store.Update(ctx, &moved); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if moved.Default {
		t.Fatal("a mover arriving in an OCCUPIED scope was told it is the default")
	}

	// And the operator still holds it.
	defaultKey, err := accountDefaultKey("tenant-truth", AccountProviderClaude, "destination")
	if err != nil {
		t.Fatalf("accountDefaultKey() error = %v", err)
	}
	pointer, err := shared.Get(ctx, defaultKey)
	if err != nil {
		t.Fatalf("Get(pointer) error = %v", err)
	}
	if string(pointer) != "acct-operator" {
		t.Fatalf("destination pointer = %q, want acct-operator: a move stole an occupied scope", string(pointer))
	}
}

// TestAccountStore_Create_DoesNotHealAScopeAnAccountIsMovingInto is the
// deliberate non-healing, mirroring the project store's rename-in-flight pin.
//
// A pointer naming a LIVE account whose record places it in a different scope is
// ambiguous: either a move into this scope is in flight, or a move out of it
// crashed. Those states are byte-identical, so healing on that condition would
// steal the scope from a move about to complete.
//
// This pins the safe answer — the arriving account does NOT auto-claim. If
// someone later widens defaultHolderMissing to cover "the holder is elsewhere",
// this fails, which is the point.
func TestAccountStore_Create_DoesNotHealAScopeAnAccountIsMovingInto(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	store := NewAccountStore(shared)

	// A live account, in its own scope.
	mover := newTestAccount("tenant-pin", "acct-mover", AccountProviderClaude)
	mover.ProjectID = "elsewhere"
	mustCreateAccount(t, store, mover)

	// The state a move into "destination" has after its first pointer write:
	// the destination names acct-mover, whose record still says "elsewhere".
	defaultKey, err := accountDefaultKey("tenant-pin", AccountProviderClaude, "destination")
	if err != nil {
		t.Fatalf("accountDefaultKey() error = %v", err)
	}
	if err := shared.Put(ctx, defaultKey, []byte("acct-mover")); err != nil {
		t.Fatalf("Put(pointer) error = %v", err)
	}

	arriving := newTestAccount("tenant-pin", "acct-arriving", AccountProviderClaude)
	arriving.ProjectID = "destination"
	if err := store.Create(ctx, arriving); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if arriving.Default {
		t.Fatal("a create healed a scope pointer held by a LIVE account: that state is " +
			"indistinguishable from a move in flight, and healing it steals the scope " +
			"from a move about to complete")
	}

	pointer, err := shared.Get(ctx, defaultKey)
	if err != nil {
		t.Fatalf("Get(pointer) error = %v", err)
	}
	if string(pointer) != "acct-mover" {
		t.Fatalf("pointer now names %q; the refused auto-claim must not have touched it", string(pointer))
	}
}

// holdBetweenProbeAndClaim parks a mover once, AFTER it has looked at the
// destination pointer and BEFORE it writes one.
//
// The two implementations reach that point by different routes, so the hook
// covers both and a sync.Once keeps it to a single park per node: a
// probe-then-write election parks after its Get and then writes freely, while a
// guarded election parks before its PutIfAbsent. Either way the node has
// decided nothing and written nothing when the test takes over.
type holdBetweenProbeAndClaim struct {
	interfaces.KVStore

	key     string
	parked  chan struct{}
	release chan struct{}
	once    sync.Once
}

func newHoldBetweenProbeAndClaim(inner interfaces.KVStore, key string) *holdBetweenProbeAndClaim {
	return &holdBetweenProbeAndClaim{
		KVStore: inner,
		key:     key,
		parked:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (k *holdBetweenProbeAndClaim) park() {
	k.once.Do(func() {
		close(k.parked)
		select {
		case <-k.release:
		case <-time.After(barrierWait):
		}
	})
}

// Get parks AFTER the read, so a probing implementation has already seen the
// scope as vacant when the test moves the pointer underneath it.
func (k *holdBetweenProbeAndClaim) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := k.KVStore.Get(ctx, key)
	if key == k.key {
		k.park()
	}
	return data, err
}

// PutIfAbsent parks BEFORE the write, which is where a guarded implementation
// reaches the same point.
func (k *holdBetweenProbeAndClaim) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if key == k.key {
		k.park()
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *holdBetweenProbeAndClaim) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

func (k *holdBetweenProbeAndClaim) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	return forwardCompareAndSwap(ctx, k.KVStore, key, expected, next)
}

// TestAccountStore_Update_MoverDoesNotOverwriteADefaultSetWhileItWasDeciding is
// the test that actually distinguishes a guarded destination election from a
// probe-then-write one.
//
// THE NINE-MOVER RACE ABOVE DOES NOT. Measured, not assumed: with the election
// reverted to probe-then-write it passed 10 runs out of 10, because the
// re-derive at the end of rescopeDefault reads the pointer back and only the
// account matching it reports Default. Nine movers racing an empty scope all
// write, last writer wins, and each then re-derives correctly — so the returned
// flags look right even though the pointer was overwritten eight times. The
// re-derive MASKS the defect in exactly the value that test inspects.
//
// The damage an unguarded election does is not to a race between equals. It is
// to a decision made by someone OUTSIDE the race: a mover that probed a vacant
// scope, and then wrote over the default that was legitimately claimed while it
// was deciding. That is an authority transfer on a lost race — the thing this
// whole family refuses to do — so it is asserted on the pointer, which is where
// the authority lives.
func TestAccountStore_Update_MoverDoesNotOverwriteADefaultSetWhileItWasDeciding(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()

	mover := newTestAccount("tenant-steal", "acct-mover", AccountProviderClaude)
	mover.ProjectID = "origin"
	mustCreateAccount(t, NewAccountStore(shared), mover)

	destKey, err := accountDefaultKey("tenant-steal", AccountProviderClaude, "destination")
	if err != nil {
		t.Fatalf("accountDefaultKey() error = %v", err)
	}
	held := newHoldBetweenProbeAndClaim(shared, destKey)

	var (
		wg       sync.WaitGroup
		moved    = *mover
		movedErr error
	)
	moved.ProjectID = "destination"
	wg.Add(1)
	go func() {
		defer wg.Done()
		movedErr = NewAccountStore(held).Update(ctx, &moved)
	}()

	select {
	case <-held.parked:
	case <-time.After(barrierWait):
		t.Fatal("the mover never reached the destination pointer")
	}

	// While the mover is deciding, the scope is legitimately claimed by a new
	// account created directly in it.
	claimant := newTestAccount("tenant-steal", "acct-claimant", AccountProviderClaude)
	claimant.ProjectID = "destination"
	if err := NewAccountStore(shared).Create(ctx, claimant); err != nil {
		t.Fatalf("claimant Create() error = %v", err)
	}
	if !claimant.Default {
		t.Fatal("the claimant created into a vacant scope but was not made its default")
	}

	close(held.release)
	wg.Wait()

	if movedErr != nil {
		t.Fatalf("the move failed; arriving in an occupied scope is not an error: %v", movedErr)
	}

	pointer, err := shared.Get(ctx, destKey)
	if err != nil {
		t.Fatalf("Get(pointer) error = %v", err)
	}
	if string(pointer) != "acct-claimant" {
		t.Fatalf("destination pointer = %q, want acct-claimant: the mover overwrote a default "+
			"claimed while it was deciding, transferring the scope away from the account that won it",
			string(pointer))
	}
	if moved.Default {
		t.Error("the mover was told it holds a scope it did not win")
	}
}
