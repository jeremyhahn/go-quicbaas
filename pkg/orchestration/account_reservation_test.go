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
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// =============================================================================
// Doubles
// =============================================================================

// mutableSessionLister is a concurrency-safe session store double whose
// records a test can change between reads.
type mutableSessionLister struct {
	mu      sync.Mutex
	records []*SessionRecord
}

func (m *mutableSessionLister) ListSessions(context.Context, string) ([]*SessionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*SessionRecord(nil), m.records...), nil
}

func (m *mutableSessionLister) set(records ...*SessionRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = records
}

// reservationTable builds the production counter over lister with a clock the
// test controls.
func reservationTable(t *testing.T, lister SessionRecordLister, now *time.Time) *sessionRecordCounter {
	t.Helper()
	reserver, err := NewAccountSessionCounter(lister)
	if err != nil {
		t.Fatalf("NewAccountSessionCounter: %v", err)
	}
	table := reserver.(*sessionRecordCounter)
	if now != nil {
		table.now = func() time.Time { return *now }
	}
	return table
}

func cappedAccount(id string, maxSessions int) *Account {
	return &Account{ID: id, TenantID: "tenant-a", Provider: AccountProviderClaude,
		Status: AccountStatusActive, MaxConcurrentSessions: maxSessions}
}

func liveOf(t *testing.T, table AccountSessionLookup, accountID string) int {
	t.Helper()
	live, err := table.LiveSessions(context.Background(), "tenant-a", []string{accountID})
	if err != nil {
		t.Fatalf("LiveSessions: %v", err)
	}
	return live[accountID]
}

// =============================================================================
// The table
// =============================================================================

func claimOn(account *Account) SessionReservationRequest {
	return SessionReservationRequest{Account: account}
}

// TestReservation_ConcurrentReservesTakeExactlyTheCap is P1 at its source: N
// callers racing for a cap of one on the real table get one slot between them.
func TestReservation_ConcurrentReservesTakeExactlyTheCap(t *testing.T) {
	table := reservationTable(t, &mutableSessionLister{}, nil)
	account := cappedAccount("claude1", 1)

	const callers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, refused := 0, 0
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := table.Reserve(context.Background(), "tenant-a", claimOn(account))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case errors.Is(err, ErrAccountsSaturated):
				refused++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if won != 1 || refused != callers-1 {
		t.Fatalf("won=%d refused=%d, want exactly one slot under a cap of one", won, refused)
	}
	if got := liveOf(t, table, "claude1"); got != 1 {
		t.Fatalf("LiveSessions = %d, want the one reservation counted", got)
	}
}

func TestReservation_ReleaseFreesTheSlot(t *testing.T) {
	table := reservationTable(t, &mutableSessionLister{}, nil)
	account := cappedAccount("claude1", 1)
	ctx := context.Background()

	rid, err := table.Reserve(ctx, "tenant-a", claimOn(account))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if _, err := table.Reserve(ctx, "tenant-a", claimOn(account)); !errors.Is(err, ErrAccountsSaturated) {
		t.Fatalf("second Reserve error = %v, want saturated", err)
	}
	if err := table.Release(ctx, "tenant-a", "claude1", rid); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := liveOf(t, table, "claude1"); got != 0 {
		t.Fatalf("LiveSessions after release = %d, want 0", got)
	}
	if _, err := table.Reserve(ctx, "tenant-a", claimOn(account)); err != nil {
		t.Fatalf("Reserve after release: %v", err)
	}
	// An unknown or empty release is not an error: there is nothing to give back.
	if err := table.Release(ctx, "tenant-a", "claude1", "no-such"); err != nil {
		t.Fatalf("Release(unknown) = %v", err)
	}
	if err := table.Release(ctx, "tenant-a", "claude1", ""); err != nil {
		t.Fatalf("Release(empty) = %v", err)
	}
}

func TestReservation_OnlyTheRecordNamingItSatisfiesIt(t *testing.T) {
	lister := &mutableSessionLister{}
	table := reservationTable(t, lister, nil)
	account := cappedAccount("claude1", 3)
	ctx := context.Background()

	rid, err := table.Reserve(ctx, "tenant-a", claimOn(account))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	// A new live session on the same account, with the same (empty) job, that
	// does NOT name the reservation: two sessions, counted twice. Matching by
	// account and job would have merged them.
	lister.set(&SessionRecord{SessionID: "other", AccountID: "claude1", Status: SessionStateActive})
	if got := liveOf(t, table, "claude1"); got != 2 {
		t.Fatalf("LiveSessions = %d, want the unrelated session and the reservation", got)
	}

	// The spawn's record lands naming it: one session, counted once.
	lister.set(
		&SessionRecord{SessionID: "other", AccountID: "claude1", Status: SessionStateActive},
		&SessionRecord{SessionID: "mine", AccountID: "claude1", Status: SessionStateActive, ReservationIDs: []string{rid}},
	)
	if got := liveOf(t, table, "claude1"); got != 2 {
		t.Fatalf("LiveSessions = %d, want two sessions (the reservation satisfied)", got)
	}

	// The session ends: its slot frees, and the satisfied reservation does
	// not come back.
	lister.set(
		&SessionRecord{SessionID: "other", AccountID: "claude1", Status: SessionStateActive},
		&SessionRecord{SessionID: "mine", AccountID: "claude1", Status: SessionStateClosed, ReservationIDs: []string{rid}},
	)
	if got := liveOf(t, table, "claude1"); got != 1 {
		t.Fatalf("LiveSessions = %d, want only the other session", got)
	}
}

// TestReservation_OneRecordSatisfiesOneReservation: a record names exactly the
// reservation it was spawned under, so a second, outstanding reservation stays
// counted on every read.
func TestReservation_OneRecordSatisfiesOneReservation(t *testing.T) {
	lister := &mutableSessionLister{}
	table := reservationTable(t, lister, nil)
	account := cappedAccount("claude1", 5)
	first, err := table.Reserve(context.Background(), "tenant-a", claimOn(account))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if _, err := table.Reserve(context.Background(), "tenant-a", claimOn(account)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	lister.set(&SessionRecord{SessionID: "s", AccountID: "claude1", Status: SessionStateActive, ReservationIDs: []string{first}})
	for read := 1; read <= 3; read++ {
		if got := liveOf(t, table, "claude1"); got != 2 {
			t.Fatalf("read %d: LiveSessions = %d, want the record plus the outstanding reservation", read, got)
		}
	}
}

func TestReservation_AnOrphanedReservationExpires(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	table := reservationTable(t, &mutableSessionLister{}, &now)
	account := cappedAccount("claude1", 1)
	if _, err := table.Reserve(context.Background(), "tenant-a", claimOn(account)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	now = now.Add(AccountSessionReservationTTL - time.Second)
	if got := liveOf(t, table, "claude1"); got != 1 {
		t.Fatalf("LiveSessions just inside the TTL = %d, want 1", got)
	}
	now = now.Add(2 * time.Second)
	if got := liveOf(t, table, "claude1"); got != 0 {
		t.Fatalf("LiveSessions past the TTL = %d, want the orphan dropped", got)
	}
	if _, err := table.Reserve(context.Background(), "tenant-a", claimOn(account)); err != nil {
		t.Fatalf("Reserve after expiry: %v", err)
	}
}

// TestReservation_AnUnmeteredAccountIsCountedButNeverRefused: occupancy is
// tracked for balancing whether or not a cap is enforced.
func TestReservation_AnUnmeteredAccountIsCountedButNeverRefused(t *testing.T) {
	table := reservationTable(t, &mutableSessionLister{}, nil)
	for i := 0; i < 3; i++ {
		rid, err := table.Reserve(context.Background(), "tenant-a", claimOn(cappedAccount("claude1", 0)))
		if err != nil || rid == "" {
			t.Fatalf("Reserve = (%q, %v), want a reservation and no refusal", rid, err)
		}
	}
	if got := liveOf(t, table, "claude1"); got != 3 {
		t.Fatalf("LiveSessions = %d, want the three in-flight spawns counted", got)
	}
	if _, err := table.Reserve(context.Background(), "tenant-a", SessionReservationRequest{}); !errors.Is(err, ErrAccountInvalid) {
		t.Fatalf("Reserve(no account) = %v, want ErrAccountInvalid", err)
	}
}

func TestReservation_AConditionalClaimRefusesAMovedCount(t *testing.T) {
	table := reservationTable(t, &mutableSessionLister{}, nil)
	account := cappedAccount("claude1", 0)
	ctx := context.Background()
	if _, err := table.Reserve(ctx, "tenant-a", SessionReservationRequest{Account: account, ExpectLive: true, ExpectedLive: 0}); err != nil {
		t.Fatalf("Reserve at the expected count: %v", err)
	}
	_, err := table.Reserve(ctx, "tenant-a", SessionReservationRequest{Account: account, ExpectLive: true, ExpectedLive: 0})
	var changed *AccountSessionCountChangedError
	if !errors.As(err, &changed) || changed.Expected != 0 || changed.Actual != 1 {
		t.Fatalf("Reserve on a moved count = %v, want AccountSessionCountChangedError{0 -> 1}", err)
	}
	if got := liveOf(t, table, "claude1"); got != 1 {
		t.Fatalf("LiveSessions = %d, want the refused claim not counted", got)
	}
}

func TestReservation_TenantsDoNotShareSlots(t *testing.T) {
	table := reservationTable(t, &mutableSessionLister{}, nil)
	account := cappedAccount("claude1", 1)
	if _, err := table.Reserve(context.Background(), "tenant-a", claimOn(account)); err != nil {
		t.Fatalf("Reserve tenant-a: %v", err)
	}
	if _, err := table.Reserve(context.Background(), "tenant-b", claimOn(account)); err != nil {
		t.Fatalf("Reserve tenant-b: %v — another tenant's reservation must not count", err)
	}
}

// =============================================================================
// The resolver reserves, and gives back what it cannot use
// =============================================================================

func reservingResolver(t *testing.T, store AccountStore, secrets *AccountSecretStore, table AccountSessionReserver) AccountResolver {
	t.Helper()
	registry, err := NewBalancedAccountStrategyRegistry(&fakeUsageLookup{}, table)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return NewAccountResolver(store, NewAccountTokenManager(secrets, store, time.Minute), registry,
		WithAccountSessionReserver(table))
}

// TestAccountResolver_ConcurrentResolvesRespectTheCap is P1 end to end on the
// control plane: N resolutions race for a cap of one, and one wins.
func TestAccountResolver_ConcurrentResolvesRespectTheCap(t *testing.T) {
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	seedPoolAccount(t, store, secrets, cappedAccount("claude1", 1))
	table := reservationTable(t, &mutableSessionLister{}, nil)
	resolver := reservingResolver(t, store, secrets, table)

	const spawns = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, spawns)
	ids := make(chan string, spawns)
	for i := 0; i < spawns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resolved, err := resolver.Resolve(context.Background(), AccountResolveRequest{
				TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true, JobID: "job",
			})
			errs <- err
			if err == nil {
				ids <- resolved.ReservationID
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(ids)
	won := 0
	for err := range errs {
		if err == nil {
			won++
			continue
		}
		if !errors.Is(err, ErrAccountsSaturated) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d resolutions got the account, want exactly 1", won)
	}
	if rid := <-ids; rid == "" {
		t.Fatal("the winning resolution carries no ReservationID to release on a failed spawn")
	}
}

func TestAccountResolver_AFailedResolutionReleasesItsReservation(t *testing.T) {
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	// Metadata with no credential envelope: selection and reservation succeed,
	// and the credential step then fails.
	if err := store.Create(context.Background(), cappedAccount("claude1", 1)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	table := reservationTable(t, &mutableSessionLister{}, nil)
	resolver := reservingResolver(t, store, secrets, table)

	_, err := resolver.Resolve(context.Background(), AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true, JobID: "job",
	})
	if !errors.Is(err, ErrAccountCredentialNotFound) {
		t.Fatalf("Resolve error = %v, want the missing credential", err)
	}
	if got := liveOf(t, table, "claude1"); got != 0 {
		t.Fatalf("LiveSessions = %d, want the reservation given back", got)
	}
}

func TestAccountResolver_APinnedAccountAtItsCapIsRefused(t *testing.T) {
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	seedPoolAccount(t, store, secrets, cappedAccount("claude1", 1))
	table := reservationTable(t, &mutableSessionLister{}, nil)
	resolver := reservingResolver(t, store, secrets, table)
	ctx := context.Background()

	if _, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", AccountID: "claude1", JobID: "a"}); err != nil {
		t.Fatalf("first pinned Resolve: %v", err)
	}
	if _, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", AccountID: "claude1", JobID: "b"}); !errors.Is(err, ErrAccountsSaturated) {
		t.Fatalf("second pinned Resolve error = %v, want saturated", err)
	}
}

func TestAccountResolver_ReleaseReservationDelegatesToTheTable(t *testing.T) {
	table := reservationTable(t, &mutableSessionLister{}, nil)
	rid, err := table.Reserve(context.Background(), "tenant-a", claimOn(cappedAccount("claude1", 1)))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	resolver := reservingResolver(t, NewInMemoryAccountStore(), newTestAccountSecretStore(t), table)
	releaser, ok := resolver.(AccountReservationReleaser)
	if !ok {
		t.Fatal("the production resolver must release reservations")
	}
	if err := releaser.ReleaseReservation(context.Background(), "tenant-a", "claude1", rid); err != nil {
		t.Fatalf("ReleaseReservation: %v", err)
	}
	if got := liveOf(t, table, "claude1"); got != 0 {
		t.Fatalf("LiveSessions = %d, want 0", got)
	}
	// With no reserver wired there is nothing to release, and no error.
	plain := NewAccountResolver(NewInMemoryAccountStore(), nil, NewAccountStrategyRegistry(nil))
	if err := plain.(AccountReservationReleaser).ReleaseReservation(context.Background(), "t", "a", "r"); err != nil {
		t.Fatalf("ReleaseReservation without a reserver = %v", err)
	}
}

// recordingReleaser is an AccountResolver that records releases.
type recordingReleaser struct {
	AccountResolver
	released []string
	err      error
}

func (r *recordingReleaser) ReleaseReservation(_ context.Context, tenantID, accountID, reservationID string) error {
	r.released = append(r.released, tenantID+"/"+accountID+"/"+reservationID)
	return r.err
}

func TestACPBackend_AFailedSpawnReleasesItsReservation(t *testing.T) {
	releaser := &recordingReleaser{}
	backend := &ACPBackend{accountResolver: releaser, logger: &agentTestLogger{}}

	backend.releaseAccountReservation(context.Background(), &resolvedSpawnAccount{
		AccountID: "claude1", TenantID: "tenant-a", ReservationID: "r1",
	})
	backend.releaseAccountReservation(context.Background(), &resolvedSpawnAccount{AccountID: "claude1"})
	backend.releaseAccountReservation(context.Background(), nil)
	if len(releaser.released) != 1 || releaser.released[0] != "tenant-a/claude1/r1" {
		t.Fatalf("released = %v, want exactly the one reservation", releaser.released)
	}

	// A release that fails is logged, not raised: the spawn's own error is
	// what the caller needs.
	releaser.err = errors.New("control plane unreachable")
	backend.releaseAccountReservation(context.Background(), &resolvedSpawnAccount{
		AccountID: "claude1", TenantID: "tenant-a", ReservationID: "r2",
	})
	if len(releaser.released) != 2 {
		t.Fatalf("released = %v, want the second attempt made", releaser.released)
	}
}

// =============================================================================
// P2a: a capped project pool falls back to the tenant-wide pool
// =============================================================================

func TestAccountResolver_ACappedProjectPoolFallsBackTenantWide(t *testing.T) {
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	project := cappedAccount("proj-seat", 1)
	project.ProjectID = "proj-1"
	seedPoolAccount(t, store, secrets, project)
	seedPoolAccount(t, store, secrets, cappedAccount("tenant-seat", 0))
	table := reservationTable(t, &mutableSessionLister{records: []*SessionRecord{
		{SessionID: "busy", AccountID: "proj-seat", Status: SessionStateActive},
	}}, nil)
	resolver := reservingResolver(t, store, secrets, table)

	resolved, err := resolver.Resolve(context.Background(), AccountResolveRequest{
		TenantID: "tenant-a", ProjectID: "proj-1", Provider: AccountProviderClaude, Auto: true,
	})
	if err != nil {
		t.Fatalf("Resolve: %v — a full project pool must fall back to the tenant-wide one", err)
	}
	if resolved.AccountID != "tenant-seat" {
		t.Fatalf("Resolve chose %s, want tenant-seat", resolved.AccountID)
	}
}

func TestAccountResolver_EverythingCappedIsSaturated(t *testing.T) {
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	project := cappedAccount("proj-seat", 1)
	project.ProjectID = "proj-1"
	seedPoolAccount(t, store, secrets, project)
	seedPoolAccount(t, store, secrets, cappedAccount("tenant-seat", 1))
	table := reservationTable(t, &mutableSessionLister{records: []*SessionRecord{
		{SessionID: "a", AccountID: "proj-seat", Status: SessionStateActive},
		{SessionID: "b", AccountID: "tenant-seat", Status: SessionStateActive},
	}}, nil)
	resolver := reservingResolver(t, store, secrets, table)

	_, err := resolver.Resolve(context.Background(), AccountResolveRequest{
		TenantID: "tenant-a", ProjectID: "proj-1", Provider: AccountProviderClaude, Auto: true,
	})
	var saturated *AccountsSaturatedError
	if !errors.As(err, &saturated) || saturated.Candidates != 2 {
		t.Fatalf("Resolve error = %v, want saturated over both pools", err)
	}
}

// =============================================================================
// P2b: admission reports an exhausted pool through the shared reporter
// =============================================================================

func poolExhaustedCount(tenantID, reason string) float64 {
	return testutil.ToFloat64(metrics.GetMetrics().OrchAccountPoolExhausted.With(prometheus.Labels{
		"tenant": tenantID, "provider": accountPoolAnyProvider, "reason": reason,
	}))
}

func exhaustedEvents(auditor *recordingAuditor) []StoreAuditEvent {
	var out []StoreAuditEvent
	for _, e := range auditor.all() {
		if e.Action == AuditActionPoolExhausted {
			out = append(out, e)
		}
	}
	return out
}

func TestAccountPoolHeadroom_ADeferredPoolIsReportedOncePerExhaustion(t *testing.T) {
	cases := []struct {
		name    string
		reason  string
		prepare func(t *testing.T, rig *headroomRig, sessions *fakeSessionLookup)
	}{
		{
			name:   "every account at its cap",
			reason: AccountPoolSaturated,
			prepare: func(t *testing.T, rig *headroomRig, sessions *fakeSessionLookup) {
				rig.cappedAccount(t, "claude1", 1)
				sessions.live["claude1"] = 1
			},
		},
		{
			name:   "every account held",
			reason: AccountPoolCoolingDown,
			prepare: func(t *testing.T, rig *headroomRig, _ *fakeSessionLookup) {
				rig.account(t, "claude1", "", false)
				holdAccountUntil(t, rig.accounts, admissionTenant, "claude1", time.Now().UTC().Add(time.Hour))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessions := &fakeSessionLookup{live: map[string]int{}}
			auditor := &recordingAuditor{}
			rig := newHeadroomRig(t)
			reader, err := NewAccountPoolHeadroom(rig.accounts, NewQuotaAuthority(rig.ledger),
				WithHeadroomSessions(sessions), WithHeadroomReporting(auditor, &agentTestLogger{}))
			if err != nil {
				t.Fatalf("NewAccountPoolHeadroom: %v", err)
			}
			tc.prepare(t, rig, sessions)
			before := poolExhaustedCount(admissionTenant, tc.reason)

			// Two sweeps over the same exhausted pool: one report.
			for i := 0; i < 2; i++ {
				headroom, err := reader.Headroom(context.Background(), admissionTenant, "p", time.Now().UTC())
				if err != nil {
					t.Fatalf("Headroom: %v", err)
				}
				if headroom.HasHeadroom() {
					t.Fatalf("headroom = %+v, want none", headroom)
				}
			}
			events := exhaustedEvents(auditor)
			if len(events) != 1 {
				t.Fatalf("pool_exhausted events = %+v, want exactly one for one exhaustion", events)
			}
			if events[0].ResourceID != "pool/p/any" || events[0].Success {
				t.Fatalf("event = %+v, want a failed event naming the project's pool", events[0])
			}
			if got := poolExhaustedCount(admissionTenant, tc.reason) - before; got != 1 {
				t.Fatalf("metric advanced by %v, want 1", got)
			}
		})
	}
}

func TestAccountPoolHeadroom_ARecoveredPoolIsReportedAgainWhenItEmpties(t *testing.T) {
	sessions := &fakeSessionLookup{live: map[string]int{"claude1": 1}}
	auditor := &recordingAuditor{}
	rig := newHeadroomRig(t)
	reader, err := NewAccountPoolHeadroom(rig.accounts, NewQuotaAuthority(rig.ledger),
		WithHeadroomSessions(sessions), WithHeadroomReporting(auditor, nil))
	if err != nil {
		t.Fatalf("NewAccountPoolHeadroom: %v", err)
	}
	rig.cappedAccount(t, "claude1", 1)
	ask := func() {
		t.Helper()
		if _, err := reader.Headroom(context.Background(), admissionTenant, "p", time.Now().UTC()); err != nil {
			t.Fatalf("Headroom: %v", err)
		}
	}
	ask()
	sessions.live["claude1"] = 0
	ask() // recovered: nothing reported
	sessions.live["claude1"] = 1
	ask()
	if events := exhaustedEvents(auditor); len(events) != 2 {
		t.Fatalf("pool_exhausted events = %d, want one per exhaustion", len(events))
	}
}

// TestReservation_AResumeDoesNotCountTheSessionItReconstitutes is the round-4
// review finding on #1047: after a restart a persistent session keeps its
// durable idle status with no process, and its resume re-claims the same
// pinned account. Counting its own record there made a session on a cap-one
// account refuse itself as saturated. The resume names the session it
// replaces; any OTHER session still counts.
func TestReservation_AResumeDoesNotCountTheSessionItReconstitutes(t *testing.T) {
	lister := &mutableSessionLister{}
	table := reservationTable(t, lister, nil)
	account := cappedAccount("claude1", 1)
	lister.set(&SessionRecord{SessionID: "idle-1", AccountID: "claude1", Status: SessionStateIdle})

	if _, err := table.Reserve(context.Background(), "tenant-a", claimOn(account)); !errors.Is(err, ErrAccountsSaturated) {
		t.Fatalf("a fresh claim on a cap-one account holding an idle session: err = %v, want saturated", err)
	}
	resume := claimOn(account)
	resume.ReplacesSessionID = "idle-1"
	if _, err := table.Reserve(context.Background(), "tenant-a", resume); err != nil {
		t.Fatalf("the idle session's own resume refused itself: %v", err)
	}
	other := claimOn(account)
	other.ReplacesSessionID = "someone-else"
	if _, err := table.Reserve(context.Background(), "tenant-a", other); !errors.Is(err, ErrAccountsSaturated) {
		t.Fatalf("a claim replacing a different session: err = %v, want saturated", err)
	}
}

// TestSpawnRequestFromRecord_NamesTheSessionItReconstitutes: the resume path
// is what carries the replaced session's ID down to the reservation.
func TestSpawnRequestFromRecord_NamesTheSessionItReconstitutes(t *testing.T) {
	req := spawnRequestFromRecord(&SessionRecord{SessionID: "idle-1", AccountID: "claude1"})
	if req.ReplacesSessionID != "idle-1" || req.AccountID != "claude1" {
		t.Fatalf("spawn request = %+v, want it to name idle-1 on its pinned account", req)
	}
}
