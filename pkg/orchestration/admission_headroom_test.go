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
	"errors"
	"strconv"
	"testing"
	"time"
)

// =============================================================================
// Fixtures
// =============================================================================

// headroomWindow is the metered allowance every test account is judged against.
// Small enough that one recorded entry can spend it.
var headroomWindow = QuotaWindow{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 1000}

// headroomRig assembles a PoolHeadroomReader over a real account store and a
// real ledger-backed quota authority.
//
// REAL, NOT DOUBLED, and deliberately: the value of this reader is that it asks
// the SAME two authorities the spawn path asks, so a double of either would test
// the wiring against a copy of the rules rather than the rules.
type headroomRig struct {
	accounts AccountStore
	ledger   ConsumptionLedger
	reader   PoolHeadroomReader
}

func newHeadroomRig(t *testing.T) *headroomRig {
	t.Helper()
	ledger, _ := newLedgerFixture()
	accounts := NewInMemoryAccountStore()
	reader, err := NewAccountPoolHeadroom(accounts, NewQuotaAuthority(ledger))
	if err != nil {
		t.Fatalf("NewAccountPoolHeadroom: %v", err)
	}
	return &headroomRig{accounts: accounts, ledger: ledger, reader: reader}
}

// account creates a pooled account with the metered window above.
func (r *headroomRig) account(t *testing.T, id, projectID string, metered bool) *Account {
	t.Helper()
	account := newTestAccount(admissionTenant, id, AccountProviderClaude)
	account.ProjectID = projectID
	account.Status = AccountStatusActive
	if metered {
		account.Spend = AccountSpendPolicy{Quota: AccountQuotaPolicy{Windows: []QuotaWindow{headroomWindow}}}
	}
	if err := r.accounts.Create(context.Background(), account); err != nil {
		t.Fatalf("create account %s: %v", id, err)
	}
	return account
}

// spend records consumption against an account inside the window.
func (r *headroomRig) spend(t *testing.T, accountID string, tokens int64, at time.Time) {
	t.Helper()
	if err := r.ledger.Record(context.Background(), ConsumptionEntry{
		EntryID:      accountID + "-" + strconv.FormatInt(at.UnixNano(), 10),
		TenantID:     admissionTenant,
		ProjectID:    "p",
		AccountID:    accountID,
		Engine:       "acp",
		Model:        "opus",
		Outcome:      ConsumptionOutcomeSuccess,
		InputTokens:  tokens,
		OutputTokens: 0,
		OccurredAt:   at,
	}); err != nil {
		t.Fatalf("record consumption: %v", err)
	}
}

// failingAccountLister cannot list a tenant's accounts.
type failingAccountLister struct {
	AccountStore
	err error
}

func (f *failingAccountLister) ListByTenant(context.Context, string, AccountProvider, string) ([]*Account, error) {
	return nil, f.err
}

// failingQuotaAuthority cannot decide an account's window.
type failingQuotaAuthority struct{ err error }

func (f *failingQuotaAuthority) Admit(context.Context, QuotaAdmitRequest, time.Time) (*QuotaDecision, error) {
	return nil, f.err
}

// =============================================================================
// Construction
// =============================================================================

func TestNewAccountPoolHeadroom_RequiresBothAuthorities(t *testing.T) {
	t.Parallel()
	ledger, _ := newLedgerFixture()
	if _, err := NewAccountPoolHeadroom(NewInMemoryAccountStore(), NewQuotaAuthority(ledger)); err != nil {
		t.Fatalf("a complete configuration was refused: %v", err)
	}
	if _, err := NewAccountPoolHeadroom(nil, NewQuotaAuthority(ledger)); !errors.Is(err, ErrAdmissionConfig) {
		t.Fatalf("a nil account store: want ErrAdmissionConfig, got %v", err)
	}
	if _, err := NewAccountPoolHeadroom(NewInMemoryAccountStore(), nil); !errors.Is(err, ErrAdmissionConfig) {
		t.Fatalf("a nil quota authority: want ErrAdmissionConfig, got %v", err)
	}
}

func TestAccountPoolHeadroom_RefusesAQuestionWithNoTenant(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	_, err := rig.reader.Headroom(context.Background(), "", "p", admissionClock)
	if !errors.Is(err, ErrAdmissionConfig) {
		t.Fatalf("a pool with no tenant: want ErrAdmissionConfig, got %v", err)
	}
}

// =============================================================================
// The answer
// =============================================================================

// TestAccountPoolHeadroom_AnUnmeteredAccountHasHeadroom: an account nobody set a
// window on is not held by this axis, which is the same stance
// AccountSpendPolicy takes one layer down.
func TestAccountPoolHeadroom_AnUnmeteredAccountHasHeadroom(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	rig.account(t, "unmetered", "", false)

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", admissionClock)
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Pooled != 1 || headroom.Ready != 1 {
		t.Fatalf("Headroom = %+v, want one pooled and ready account", headroom)
	}
	if !headroom.HasHeadroom() || headroom.Unmetered() {
		t.Fatalf("an account IS pooled, and it has headroom: %+v", headroom)
	}
}

// TestAccountPoolHeadroom_AnEmptyPoolIsUnmetered: a deployment that pools no
// account has nothing to say on this axis, and must not be read as spent.
func TestAccountPoolHeadroom_AnEmptyPoolIsUnmetered(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", admissionClock)
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if !headroom.Unmetered() || !headroom.HasHeadroom() {
		t.Fatalf("an empty pool must be unmetered and unblocking: %+v", headroom)
	}
	if headroom.Reason != "" {
		t.Fatalf("an empty pool has nothing to explain: %q", headroom.Reason)
	}
}

// TestAccountPoolHeadroom_ASpentWindowRemovesHeadroom is the subscription-usage
// axis doing its job: the account is healthy, the provider has refused nothing,
// and the sliding window says the plan's allowance for the last five hours is
// gone. No price is involved anywhere.
func TestAccountPoolHeadroom_ASpentWindowRemovesHeadroom(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	rig.account(t, "spent", "", true)
	// One entry inside the window spends the whole allowance.
	spentAt := ledgerBase
	rig.spend(t, "spent", headroomWindow.MaxTokens+1, spentAt)

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", spentAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Ready != 0 {
		t.Fatalf("a spent window leaves no ready account: %+v", headroom)
	}
	if headroom.HasHeadroom() {
		t.Fatal("a pooled account whose window is spent is not headroom")
	}
	if headroom.Reason == "" {
		t.Fatal("a spent pool must say why, so an operator knows whether to add an account or wait")
	}
	// The window is sliding, so it frees up when the oldest entry ages out.
	if headroom.RetryAt.IsZero() {
		t.Fatal("a spent sliding window knows when it frees up and must say so")
	}
	if want := spentAt.Add(headroomWindow.Size); !headroom.RetryAt.Equal(want) {
		t.Fatalf("RetryAt = %s, want the oldest entry aging out at %s", headroom.RetryAt, want)
	}
}

// TestAccountPoolHeadroom_UsageOutsideTheWindowDoesNotCount: the window is
// sliding, so consumption older than its size is spent allowance that has
// already come back.
func TestAccountPoolHeadroom_UsageOutsideTheWindowDoesNotCount(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	rig.account(t, "recovered", "", true)
	rig.spend(t, "recovered", headroomWindow.MaxTokens*5, ledgerBase)

	// Well past the window: the entry has aged out.
	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p",
		ledgerBase.Add(headroomWindow.Size).Add(time.Hour))
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Ready != 1 {
		t.Fatalf("an aged-out entry must not hold the window: %+v", headroom)
	}
}

// TestAccountPoolHeadroom_AProviderCooldownRemovesHeadroom: the cooldown
// AvailableAt records is the provider's OWN refusal, and it outranks any
// arithmetic we do. It is also the signal with a stated return time.
func TestAccountPoolHeadroom_AProviderCooldownRemovesHeadroom(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	// THE COOLDOWN IS ANCHORED TO THE WALL CLOCK, not to the rig's fixed
	// instant, because the account store normalizes a LAPSED hold away on every
	// decode (Account.expireCooldown) against time.Now. A hold placed at a
	// fixed historical instant would therefore be read back as already expired,
	// and the test would pass for the wrong reason.
	now := time.Now().UTC()
	account := rig.account(t, "cooling", "", true)
	account.Status = AccountStatusLimited
	account.AvailableAt = now.Add(90 * time.Minute)
	if err := rig.accounts.Update(context.Background(), account); err != nil {
		t.Fatalf("update account: %v", err)
	}

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", now)
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Ready != 0 || headroom.HasHeadroom() {
		t.Fatalf("an account in a provider cooldown is not headroom: %+v", headroom)
	}
	if !headroom.RetryAt.Equal(account.AvailableAt) {
		t.Fatalf("RetryAt = %s, want the account's own AvailableAt %s", headroom.RetryAt, account.AvailableAt)
	}
	if headroom.Reason == "" {
		t.Fatal("a held pool must say so")
	}
}

// TestAccountPoolHeadroom_OneReadyAccountIsEnough: the question is whether the
// pool can serve ANOTHER session, so one usable account answers it however many
// of its neighbours are spent.
func TestAccountPoolHeadroom_OneReadyAccountIsEnough(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	rig.account(t, "ready", "", true)
	rig.account(t, "spent", "", true)
	rig.spend(t, "spent", headroomWindow.MaxTokens+1, ledgerBase)

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", ledgerBase.Add(time.Minute))
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Pooled != 2 {
		t.Fatalf("Pooled = %d, want 2", headroom.Pooled)
	}
	if headroom.Ready != 1 || !headroom.HasHeadroom() {
		t.Fatalf("one ready account is headroom: %+v", headroom)
	}
	if headroom.Reason != "" {
		t.Fatalf("a pool with headroom has nothing to explain: %q", headroom.Reason)
	}
}

// TestAccountPoolHeadroom_TheEarliestReturnIsReported: with two held accounts
// returning at two times, the pool returns at the first of them.
func TestAccountPoolHeadroom_TheEarliestReturnIsReported(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	// See the note in TestAccountPoolHeadroom_AProviderCooldownRemovesHeadroom
	// for why a live hold is anchored to the wall clock.
	now := time.Now().UTC()
	for id, ahead := range map[string]time.Duration{"late": 3 * time.Hour, "soon": 20 * time.Minute} {
		account := rig.account(t, id, "", false)
		account.Status = AccountStatusLimited
		account.AvailableAt = now.Add(ahead)
		if err := rig.accounts.Update(context.Background(), account); err != nil {
			t.Fatalf("update %s: %v", id, err)
		}
	}
	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", now)
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if want := now.Add(20 * time.Minute); !headroom.RetryAt.Equal(want) {
		t.Fatalf("RetryAt = %s, want the earliest return %s", headroom.RetryAt, want)
	}
}

// =============================================================================
// Scoping
// =============================================================================

// TestAccountPoolHeadroom_ThePoolIsTheOneASpawnWouldDrawOn: the project's own
// accounts plus the tenant-wide unscoped ones, which is how
// accountResolver.selectAccount assembles it. An account scoped to a DIFFERENT
// project is not in this project's pool.
func TestAccountPoolHeadroom_ThePoolIsTheOneASpawnWouldDrawOn(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	rig.account(t, "tenant-wide", "", false)
	rig.account(t, "mine", "mine", false)
	rig.account(t, "theirs", "theirs", false)

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "mine", admissionClock)
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Pooled != 2 {
		t.Fatalf("Pooled = %d, want the tenant-wide account and this project's own", headroom.Pooled)
	}
}

// TestAccountPoolHeadroom_AnotherTenantsAccountIsNotInThePool: the account store
// is tenant-scoped, and this asserts the reader does not widen it.
func TestAccountPoolHeadroom_AnotherTenantsAccountIsNotInThePool(t *testing.T) {
	t.Parallel()
	rig := newHeadroomRig(t)
	rig.account(t, "ours", "", false)
	other := newTestAccount(admissionTenant2, "theirs", AccountProviderClaude)
	other.Status = AccountStatusActive
	if err := rig.accounts.Create(context.Background(), other); err != nil {
		t.Fatalf("create other tenant's account: %v", err)
	}

	headroom, err := rig.reader.Headroom(context.Background(), admissionTenant, "p", admissionClock)
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Pooled != 1 {
		t.Fatalf("Pooled = %d, want only this tenant's account", headroom.Pooled)
	}
}

// =============================================================================
// Fail closed
// =============================================================================

// TestAccountPoolHeadroom_FailsClosedOnAnUnreadablePool: a store that could not
// answer is not an answer. Reporting "no accounts, therefore unmetered" on a
// read failure would admit unmetered work through every outage.
func TestAccountPoolHeadroom_FailsClosedOnAnUnreadablePool(t *testing.T) {
	t.Parallel()
	down := errors.New("kv down")
	ledger, _ := newLedgerFixture()
	reader, err := NewAccountPoolHeadroom(
		&failingAccountLister{AccountStore: NewInMemoryAccountStore(), err: down},
		NewQuotaAuthority(ledger))
	if err != nil {
		t.Fatalf("NewAccountPoolHeadroom: %v", err)
	}
	headroom, err := reader.Headroom(context.Background(), admissionTenant, "p", admissionClock)
	if !errors.Is(err, ErrAdmissionUndecidable) {
		t.Fatalf("want ErrAdmissionUndecidable, got %v", err)
	}
	if !errors.Is(err, down) {
		t.Fatalf("the cause must survive: %v", err)
	}
	if headroom.Pooled != 0 || headroom.Ready != 0 {
		t.Fatalf("a failed read must answer nothing, got %+v", headroom)
	}
}

// TestAccountPoolHeadroom_FailsClosedOnAnUndecidableWindow: an unreadable ledger
// is not a window that admits. That is the fallback the quota authority refuses
// one layer down, and re-adding it here would reinstate it.
func TestAccountPoolHeadroom_FailsClosedOnAnUndecidableWindow(t *testing.T) {
	t.Parallel()
	down := errors.New("ledger unreadable")
	accounts := NewInMemoryAccountStore()
	account := newTestAccount(admissionTenant, "metered", AccountProviderClaude)
	account.Status = AccountStatusActive
	if err := accounts.Create(context.Background(), account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	reader, err := NewAccountPoolHeadroom(accounts, &failingQuotaAuthority{err: down})
	if err != nil {
		t.Fatalf("NewAccountPoolHeadroom: %v", err)
	}
	headroom, err := reader.Headroom(context.Background(), admissionTenant, "p", admissionClock)
	if !errors.Is(err, down) {
		t.Fatalf("want the ledger's own failure, got %v", err)
	}
	if headroom.HasHeadroom() && headroom.Pooled != 0 {
		t.Fatalf("a failed read must answer nothing, got %+v", headroom)
	}
}

// listingAccountStore answers ListByTenant with records the test wrote verbatim,
// including the shapes a healthy store never produces.
type listingAccountStore struct {
	AccountStore
	records []*Account
}

func (s *listingAccountStore) ListByTenant(context.Context, string, AccountProvider, string) ([]*Account, error) {
	return s.records, nil
}

// TestAccountPoolHeadroom_ToleratesANilAndADuplicateRecord: the pool is assembled
// from a listing, and a listing that carried a nil or repeated one account is a
// store fault. Neither may become a pooled account — a nil would be dereferenced
// on the very next line, and a duplicate would make one spent account look like
// two and one ready account look like headroom it does not have.
func TestAccountPoolHeadroom_ToleratesANilAndADuplicateRecord(t *testing.T) {
	t.Parallel()

	ledger, _ := newLedgerFixture()
	ready := newTestAccount(admissionTenant, "acct-ready", AccountProviderClaude)
	ready.Status = AccountStatusActive

	reader, err := NewAccountPoolHeadroom(
		&listingAccountStore{records: []*Account{nil, ready, ready}},
		NewQuotaAuthority(ledger))
	if err != nil {
		t.Fatalf("NewAccountPoolHeadroom: %v", err)
	}

	headroom, err := reader.Headroom(context.Background(), admissionTenant, "p", admissionClock)
	if err != nil {
		t.Fatalf("Headroom: %v", err)
	}
	if headroom.Pooled != 1 {
		t.Fatalf("Pooled = %d, want 1: a nil is not an account and a repeat is not a second one", headroom.Pooled)
	}
	if !headroom.HasHeadroom() {
		t.Fatal("the one real account is selectable and unmetered, so the pool has headroom")
	}
}
