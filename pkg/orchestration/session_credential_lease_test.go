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
	stderrors "errors"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
)

// =============================================================================
// THE REAPER AS A SECURITY CONTROL
//
// A session ends by many routes and only one of them has anything left in
// memory. The property under test is that EVERY route withdraws the grant over
// the provider credential the session was handed — the operator's close, the
// job's ending, and the sweep that runs after a node restart, which has nothing
// but the durable record to work from.
// =============================================================================

// countingLeaser wraps a real leaser and records which grants were withdrawn,
// so a test can assert the revocation happened rather than only that the
// session closed. It can also be armed to fail.
type countingLeaser struct {
	inner AccountCredentialLeaser

	mu      sync.Mutex
	revoked []string
	failing error
}

func newCountingLeaser(t *testing.T) *countingLeaser {
	t.Helper()
	leaser, _, _ := newTestLeaser(t)
	return &countingLeaser{inner: leaser}
}

func (l *countingLeaser) GrantDelivery(ctx context.Context, scope AccountLeaseScope) (*AccountCredentialLease, error) {
	return l.inner.GrantDelivery(ctx, scope)
}

func (l *countingLeaser) Validate(ctx context.Context, leaseID string, scope AccountLeaseScope) (*AccountCredentialLease, error) {
	return l.inner.Validate(ctx, leaseID, scope)
}

func (l *countingLeaser) Revoke(ctx context.Context, leaseID string) error {
	l.mu.Lock()
	failing := l.failing
	if failing == nil {
		l.revoked = append(l.revoked, leaseID)
	}
	l.mu.Unlock()
	if failing != nil {
		return failing
	}
	return l.inner.Revoke(ctx, leaseID)
}

func (l *countingLeaser) withdrawn() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.revoked...)
}

func (l *countingLeaser) failWith(err error) {
	l.mu.Lock()
	l.failing = err
	l.mu.Unlock()
}

// =============================================================================
// SessionRecord.AddCredentialLease
// =============================================================================

func TestSessionRecord_AddCredentialLease(t *testing.T) {
	record := &SessionRecord{SessionID: "s1"}

	record.AddCredentialLease("lease_a")
	record.AddCredentialLease("lease_b")
	// Append-only, and never twice: a superseded grant is revoked, not
	// forgotten, so a retrying conclusion withdraws every grant the session
	// ever held.
	record.AddCredentialLease("lease_a")

	if got := record.CredentialLeaseIDs; len(got) != 2 || got[0] != "lease_a" || got[1] != "lease_b" {
		t.Fatalf("CredentialLeaseIDs = %v, want [lease_a lease_b]", got)
	}
}

func TestSessionRecord_AddCredentialLease_BadInput(t *testing.T) {
	t.Run("empty id is ignored", func(t *testing.T) {
		record := &SessionRecord{SessionID: "s1"}
		record.AddCredentialLease("")
		if len(record.CredentialLeaseIDs) != 0 {
			t.Fatalf("CredentialLeaseIDs = %v, want empty", record.CredentialLeaseIDs)
		}
	})

	t.Run("nil record does not panic", func(t *testing.T) {
		var record *SessionRecord
		record.AddCredentialLease("lease_a")
	})
}

// =============================================================================
// sessionRecord (live) accessors
// =============================================================================

func TestLiveSessionRecord_CredentialLeases(t *testing.T) {
	mem := &sessionRecord{}

	if got := mem.CredentialLeaseIDs(); got != nil {
		t.Fatalf("CredentialLeaseIDs() = %v on a fresh record, want nil", got)
	}

	mem.AddCredentialLease("lease_a")
	mem.AddCredentialLease("lease_a")
	mem.AddCredentialLease("lease_b")
	if got := mem.CredentialLeaseIDs(); len(got) != 2 {
		t.Fatalf("CredentialLeaseIDs() = %v, want two distinct grants", got)
	}

	mem.SetCredentialLeaseIDs([]string{"lease_c"})
	if got := mem.CredentialLeaseIDs(); len(got) != 1 || got[0] != "lease_c" {
		t.Fatalf("CredentialLeaseIDs() = %v, want [lease_c]", got)
	}

	mem.SetCredentialLeaseIDs(nil)
	if got := mem.CredentialLeaseIDs(); got != nil {
		t.Fatalf("CredentialLeaseIDs() = %v after clearing, want nil", got)
	}
}

func TestLiveSessionRecord_CredentialLeases_BadInput(t *testing.T) {
	mem := &sessionRecord{}
	mem.AddCredentialLease("")
	if got := mem.CredentialLeaseIDs(); got != nil {
		t.Fatalf("CredentialLeaseIDs() = %v after adding an empty grant, want nil", got)
	}
}

func TestLiveSessionRecord_CredentialLeases_Concurrent(t *testing.T) {
	mem := &sessionRecord{}
	var wg sync.WaitGroup

	// A rotation appends while a conclusion reads. The slice is replaced rather
	// than grown, so a reader never sees a partial one.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mem.AddCredentialLease("lease_" + string(rune('a'+i)))
		}(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, id := range mem.CredentialLeaseIDs() {
				if id == "" {
					t.Error("read an empty grant out of the live record")
				}
			}
		}()
	}
	wg.Wait()
}

// =============================================================================
// Conclusion — every route
// =============================================================================

// TestConcludeAbsentSession_RevokesTheCredentialGrant is the NODE RESTART case,
// and the one the durable field exists for: nothing is left in memory, so the
// grants can only come off the store.
func TestConcludeAbsentSession_RevokesTheCredentialGrant(t *testing.T) {
	svc, store := reconcileHarness(t)
	leaser := newCountingLeaser(t)
	svc.SetAccountCredentialLeaser(leaser)
	ctx := context.Background()

	issued, err := leaser.GrantDelivery(ctx, AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("GrantDelivery: %v", err)
	}
	record := seedSession(t, store, &SessionRecord{
		SessionID:          "s-restart",
		TenantID:           "t1",
		AccountID:          "acct-1",
		CredentialLeaseIDs: []string{issued.LeaseID},
	})

	written, err := svc.concludeSessionRecord(ctx, record, ConcludedByNodeRestart)
	if err != nil {
		t.Fatalf("concludeSessionRecord: %v", err)
	}
	if !written {
		t.Fatal("concludeSessionRecord reported no write for a live record")
	}

	if got := leaser.withdrawn(); len(got) != 1 || got[0] != issued.LeaseID {
		t.Fatalf("withdrawn grants = %v, want [%s]", got, issued.LeaseID)
	}
	// And the grant is really unusable now, from the lease store.
	if _, err := leaser.Validate(ctx, issued.LeaseID, AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"}); !lease.IsLeaseRevoked(err) {
		t.Fatalf("Validate after conclusion error = %v, want a revoked refusal", err)
	}
}

// TestConcludeSessionsForJob_RevokesEveryBoundSessionsGrant is the FAILED JOB
// case: the job is over, however it got there, and every session's credential
// grant goes with it.
func TestConcludeSessionsForJob_RevokesEveryBoundSessionsGrant(t *testing.T) {
	svc, store := reconcileHarness(t)
	leaser := newCountingLeaser(t)
	svc.SetAccountCredentialLeaser(leaser)
	ctx := context.Background()

	var issuedIDs []string
	for _, sessionID := range []string{"s-a", "s-b"} {
		issued, err := leaser.GrantDelivery(ctx, AccountLeaseScope{TenantID: "t1", AccountID: "acct-" + sessionID, JobID: "job-1"})
		if err != nil {
			t.Fatalf("GrantDelivery: %v", err)
		}
		issuedIDs = append(issuedIDs, issued.LeaseID)
		seedSession(t, store, &SessionRecord{
			SessionID:          sessionID,
			TenantID:           "t1",
			JobID:              "job-1",
			CredentialLeaseIDs: []string{issued.LeaseID},
		})
	}
	seedJob(t, svc, "t1", "job-1", JobStateFailed, "s-a", "s-b")

	if _, err := svc.ConcludeSessionsForJob(ctx, "t1", "job-1"); err != nil {
		t.Fatalf("ConcludeSessionsForJob: %v", err)
	}

	withdrawn := leaser.withdrawn()
	if len(withdrawn) != len(issuedIDs) {
		t.Fatalf("withdrawn grants = %v, want all of %v", withdrawn, issuedIDs)
	}
	for _, id := range issuedIDs {
		if _, err := leaser.Validate(ctx, id, AccountLeaseScope{TenantID: "t1"}); err == nil {
			t.Fatalf("grant %s still validates after the job failed", id)
		}
	}
}

// TestConcludeAbsentSession_RefusesToConcludeWhenTheGrantSurvives is the
// bad-input twin, and the reason the revocation's failure is RETURNED: a record
// marked closed is never revisited, so concluding over a live grant would strand
// it permanently.
func TestConcludeAbsentSession_RefusesToConcludeWhenTheGrantSurvives(t *testing.T) {
	svc, store := reconcileHarness(t)
	leaser := newCountingLeaser(t)
	svc.SetAccountCredentialLeaser(leaser)
	ctx := context.Background()

	issued, err := leaser.GrantDelivery(ctx, AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("GrantDelivery: %v", err)
	}
	record := seedSession(t, store, &SessionRecord{
		SessionID:          "s-stuck",
		TenantID:           "t1",
		CredentialLeaseIDs: []string{issued.LeaseID},
	})
	leaser.failWith(&AccountStoreError{Message: "lease store unavailable"})

	written, err := svc.concludeSessionRecord(ctx, record, ConcludedByNodeRestart)
	if err == nil {
		t.Fatal("concludeSessionRecord succeeded while the credential grant was still live")
	}
	if written {
		t.Fatal("concludeSessionRecord reported a write it did not make")
	}
	if !strings.Contains(err.Error(), issued.LeaseID) {
		t.Fatalf("error %q does not name the grant that blocked the conclusion", err)
	}

	// The record is untouched, so the next pass retries the whole teardown.
	state, _ := storedStatus(t, store, "t1", "s-stuck")
	if state == SessionStateClosed {
		t.Fatal("a session whose grant could not be withdrawn was marked closed")
	}

	// And once the store recovers, the retry converges.
	leaser.failWith(nil)
	if _, err := svc.concludeSessionRecord(ctx, record, ConcludedByNodeRestart); err != nil {
		t.Fatalf("retry after recovery: %v", err)
	}
	if _, err := leaser.Validate(ctx, issued.LeaseID, AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"}); !lease.IsLeaseRevoked(err) {
		t.Fatalf("Validate after the retry error = %v, want a revoked refusal", err)
	}
}

func TestRevokeSessionCredentialLeases_NothingToDo(t *testing.T) {
	svc, _ := reconcileHarness(t)
	ctx := context.Background()

	t.Run("no leaser wired", func(t *testing.T) {
		mem := &sessionRecord{}
		mem.AddCredentialLease("lease_a")
		if err := svc.revokeSessionCredentialLeases(ctx, mem, "t1", "s1"); err != nil {
			t.Fatalf("revokeSessionCredentialLeases with no leaser = %v, want nil", err)
		}
	})

	t.Run("session holds no grant", func(t *testing.T) {
		svc.SetAccountCredentialLeaser(newCountingLeaser(t))
		if err := svc.revokeSessionCredentialLeases(ctx, &sessionRecord{}, "t1", "s1"); err != nil {
			t.Fatalf("revokeSessionCredentialLeases with no grants = %v, want nil", err)
		}
	})
}

func TestSetAccountCredentialLeaser_IgnoresNil(t *testing.T) {
	svc, _ := reconcileHarness(t)
	leaser := newCountingLeaser(t)

	svc.SetAccountCredentialLeaser(leaser)
	svc.SetAccountCredentialLeaser(nil)

	// A nil must not replace a working authority: that would silently turn the
	// reaper back into hygiene.
	if svc.accountLeases == nil {
		t.Fatal("SetAccountCredentialLeaser(nil) cleared the configured leaser")
	}
}

// =============================================================================
// Rotation supersession
// =============================================================================

func TestSupersedeCredentialLease_WithdrawsTheOutgoingGrant(t *testing.T) {
	svc, _ := reconcileHarness(t)
	leaser := newCountingLeaser(t)
	svc.SetAccountCredentialLeaser(leaser)
	ctx := context.Background()

	outgoing, err := leaser.GrantDelivery(ctx, AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("GrantDelivery: %v", err)
	}
	replacement, err := leaser.GrantDelivery(ctx, AccountLeaseScope{TenantID: "t1", AccountID: "acct-2"})
	if err != nil {
		t.Fatalf("GrantDelivery: %v", err)
	}

	record := &SessionRecord{SessionID: "s1", TenantID: "t1", CredentialLeaseIDs: []string{outgoing.LeaseID}}
	mem := &sessionRecord{}
	mem.SetCredentialLeaseIDs(record.CredentialLeaseIDs)

	svc.supersedeCredentialLease(ctx, mem, record, replacement.LeaseID)

	if got := leaser.withdrawn(); len(got) != 1 || got[0] != outgoing.LeaseID {
		t.Fatalf("withdrawn = %v, want only the outgoing grant %s", got, outgoing.LeaseID)
	}
	if len(record.CredentialLeaseIDs) != 2 {
		t.Fatalf("record grants = %v, want both the outgoing and the replacement", record.CredentialLeaseIDs)
	}
	if _, err := leaser.Validate(ctx, replacement.LeaseID, AccountLeaseScope{TenantID: "t1", AccountID: "acct-2"}); err != nil {
		t.Fatalf("the replacement grant must survive the supersession: %v", err)
	}
}

func TestSupersedeCredentialLease_BadInput(t *testing.T) {
	svc, _ := reconcileHarness(t)
	ctx := context.Background()

	t.Run("no leaser wired", func(t *testing.T) {
		record := &SessionRecord{SessionID: "s1", CredentialLeaseIDs: []string{"lease_a"}}
		svc.supersedeCredentialLease(ctx, nil, record, "lease_b")
		if len(record.CredentialLeaseIDs) != 1 {
			t.Fatalf("record grants = %v, want the record untouched with no leaser", record.CredentialLeaseIDs)
		}
	})

	t.Run("a failed withdrawal does not undo the rotation", func(t *testing.T) {
		leaser := newCountingLeaser(t)
		svc.SetAccountCredentialLeaser(leaser)
		leaser.failWith(&AccountStoreError{Message: "lease store unavailable"})

		record := &SessionRecord{SessionID: "s1", CredentialLeaseIDs: []string{"lease_outgoing"}}
		svc.supersedeCredentialLease(ctx, nil, record, "lease_replacement")

		// The replacement is still recorded, so the session's own conclusion
		// retries every withdrawal this one could not make.
		if len(record.CredentialLeaseIDs) != 2 {
			t.Fatalf("record grants = %v, want both recorded", record.CredentialLeaseIDs)
		}
	})
}

// =============================================================================
// Helper assertions
// =============================================================================

func TestCountingLeaserIsATrueWrapper(t *testing.T) {
	// The harness above must not be a stand-in for the real authority: a test
	// asserting revocation against a fake that always succeeds would prove
	// nothing. This pins that Issue/Validate go through the production leaser.
	leaser := newCountingLeaser(t)
	ctx := context.Background()

	issued, err := leaser.GrantDelivery(ctx, AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("GrantDelivery: %v", err)
	}
	if _, err := leaser.Validate(ctx, issued.LeaseID, AccountLeaseScope{TenantID: "t2", AccountID: "acct-1"}); err == nil {
		t.Fatal("Validate accepted another tenant's scope; the wrapper is not delegating")
	}
	var scopeErr *AccountLeaseScopeError
	if _, err := leaser.Validate(ctx, issued.LeaseID, AccountLeaseScope{TenantID: "t2", AccountID: "acct-1"}); !stderrors.As(err, &scopeErr) {
		t.Fatalf("Validate error = %v, want AccountLeaseScopeError from the real leaser", err)
	}
}

// =============================================================================
// A SHARED GRANT IS WITHDRAWN BY ITS LAST HOLDER
//
// A worker daemon's credential cache is keyed by account, so a burst of spawns
// onto one account collapses onto one release and therefore onto one delivery
// grant (pkg/orchestration/fleet/account_grant.go). Two concurrent sessions can be served
// under it. Withdrawing it when the first of them ends would refuse the other's
// next refresh and break a job nobody ended — so the conclusion asks the
// durable records who else is still holding it.
// =============================================================================

// TestConcludeSession_LeavesAGrantAnotherLiveSessionStillHolds is the
// availability property: ending one session must not cut the other off.
func TestConcludeSession_LeavesAGrantAnotherLiveSessionStillHolds(t *testing.T) {
	svc, store := reconcileHarness(t)
	leaser := newCountingLeaser(t)
	svc.SetAccountCredentialLeaser(leaser)
	ctx := context.Background()

	shared, err := leaser.GrantDelivery(ctx, AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("GrantDelivery: %v", err)
	}
	first := seedSession(t, store, &SessionRecord{
		SessionID:          "s-first",
		TenantID:           "t1",
		AccountID:          "acct-1",
		CredentialLeaseIDs: []string{shared.LeaseID},
	})
	seedSession(t, store, &SessionRecord{
		SessionID:          "s-second",
		TenantID:           "t1",
		AccountID:          "acct-1",
		CredentialLeaseIDs: []string{shared.LeaseID},
	})

	if _, err := svc.concludeSessionRecord(ctx, first, ConcludedByNodeRestart); err != nil {
		t.Fatalf("concludeSessionRecord: %v", err)
	}

	if got := leaser.withdrawn(); len(got) != 0 {
		t.Fatalf("withdrawn grants = %v, want none while another session holds the grant", got)
	}
	// And it is still usable, which is the point: the surviving session can
	// still refresh its copy.
	if _, err := leaser.Validate(ctx, shared.LeaseID,
		AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"}); err != nil {
		t.Fatalf("Validate() while a live session still holds the grant = %v, want nil", err)
	}
}

// TestConcludeSession_WithdrawsASharedGrantOnceItsLastHolderEnds is the security
// property: the grant does go, and the delay is bounded by the other session's
// own ending rather than by a timeout.
func TestConcludeSession_WithdrawsASharedGrantOnceItsLastHolderEnds(t *testing.T) {
	svc, store := reconcileHarness(t)
	leaser := newCountingLeaser(t)
	svc.SetAccountCredentialLeaser(leaser)
	ctx := context.Background()

	shared, err := leaser.GrantDelivery(ctx, AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("GrantDelivery: %v", err)
	}
	records := make([]*SessionRecord, 0, 2)
	for _, sessionID := range []string{"s-first", "s-second"} {
		records = append(records, seedSession(t, store, &SessionRecord{
			SessionID:          sessionID,
			TenantID:           "t1",
			AccountID:          "acct-1",
			CredentialLeaseIDs: []string{shared.LeaseID},
		}))
	}

	for _, record := range records {
		if _, err := svc.concludeSessionRecord(ctx, record, ConcludedByNodeRestart); err != nil {
			t.Fatalf("concludeSessionRecord(%s): %v", record.SessionID, err)
		}
	}

	if got := leaser.withdrawn(); len(got) != 1 || got[0] != shared.LeaseID {
		t.Fatalf("withdrawn grants = %v, want exactly [%s]", got, shared.LeaseID)
	}
	if _, err := leaser.Validate(ctx, shared.LeaseID,
		AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"}); !lease.IsLeaseRevoked(err) {
		t.Fatalf("Validate() after the last holder ended = %v, want a revoked refusal", err)
	}
}

// TestConcludeSession_AnEndedSessionIsNotAHolder proves the holder scan reads
// LIVENESS rather than mere presence: a closed or errored record still names its
// grants, and counting those as holders would make a shared grant permanently
// unrevocable.
func TestConcludeSession_AnEndedSessionIsNotAHolder(t *testing.T) {
	ctx := context.Background()

	for _, state := range []SessionState{SessionStateClosed, SessionStateError} {
		t.Run(string(state), func(t *testing.T) {
			svc, store := reconcileHarness(t)
			leaser := newCountingLeaser(t)
			svc.SetAccountCredentialLeaser(leaser)

			shared, err := leaser.GrantDelivery(ctx, AccountLeaseScope{TenantID: "t1", AccountID: "acct-1"})
			if err != nil {
				t.Fatalf("GrantDelivery: %v", err)
			}
			seedSession(t, store, &SessionRecord{
				SessionID:          "s-gone",
				TenantID:           "t1",
				Status:             state,
				CredentialLeaseIDs: []string{shared.LeaseID},
			})
			live := seedSession(t, store, &SessionRecord{
				SessionID:          "s-live",
				TenantID:           "t1",
				CredentialLeaseIDs: []string{shared.LeaseID},
			})

			if _, err := svc.concludeSessionRecord(ctx, live, ConcludedByNodeRestart); err != nil {
				t.Fatalf("concludeSessionRecord: %v", err)
			}
			if got := leaser.withdrawn(); len(got) != 1 || got[0] != shared.LeaseID {
				t.Fatalf("withdrawn grants = %v, want [%s]", got, shared.LeaseID)
			}
		})
	}
}

// TestCredentialLeasesHeldElsewhere proves the scan's own answers: a grant held
// by another live session is reported, the concluding session's own grants are
// not, and an empty keyspace reports nothing.
func TestCredentialLeasesHeldElsewhere(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := context.Background()

	seedSession(t, store, &SessionRecord{
		SessionID:          "s-self",
		TenantID:           "t1",
		CredentialLeaseIDs: []string{"lease_self"},
	})
	seedSession(t, store, &SessionRecord{
		SessionID:          "s-other",
		TenantID:           "t1",
		CredentialLeaseIDs: []string{"lease_other", ""},
	})

	held, err := svc.credentialLeasesHeldElsewhere(ctx, "t1", "s-self")
	if err != nil {
		t.Fatalf("credentialLeasesHeldElsewhere() error = %v", err)
	}
	if _, ok := held["lease_other"]; !ok {
		t.Fatalf("held = %v, want lease_other", held)
	}
	// The concluding session is not its own holder, or nothing would ever be
	// withdrawn.
	if _, ok := held["lease_self"]; ok {
		t.Fatalf("held = %v, want the concluding session's own grant excluded", held)
	}
	// An empty grant ID is not a grant.
	if _, ok := held[""]; ok {
		t.Fatalf("held = %v, want no empty grant", held)
	}

	empty, err := svc.credentialLeasesHeldElsewhere(ctx, "tenant-with-nothing", "s-self")
	if err != nil {
		t.Fatalf("credentialLeasesHeldElsewhere() on an empty tenant error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("held = %v for a tenant with no sessions, want empty", empty)
	}
}

// TestCredentialLeasesHeldElsewhere_BadInput proves a store that cannot answer
// FAILS the conclusion rather than being assumed to mean "nobody else holds it".
// Revoking on that guess is the one outcome that breaks a running session.
func TestCredentialLeasesHeldElsewhere_BadInput(t *testing.T) {
	ctx := context.Background()

	t.Run("no store wired", func(t *testing.T) {
		svc, _ := reconcileHarness(t)
		svc.store = nil
		held, err := svc.credentialLeasesHeldElsewhere(ctx, "t1", "s1")
		if err != nil {
			t.Fatalf("credentialLeasesHeldElsewhere() with no store = %v, want nil", err)
		}
		if len(held) != 0 {
			t.Fatalf("held = %v, want empty", held)
		}
	})

	t.Run("store refuses the scan", func(t *testing.T) {
		svc, _ := reconcileHarness(t)
		leaser := newCountingLeaser(t)
		svc.SetAccountCredentialLeaser(leaser)
		svc.store = &listFailingAgentStore{
			AgentStore: svc.store,
			err:        stderrors.New("kv unavailable"),
		}

		mem := &sessionRecord{TenantID: "t1"}
		mem.AddCredentialLease("lease_a")
		err := svc.revokeSessionCredentialLeases(ctx, mem, "t1", "s1")
		if err == nil {
			t.Fatal("the conclusion succeeded while the holder scan was unreadable")
		}
		if !strings.Contains(err.Error(), "kv unavailable") {
			t.Fatalf("error %q does not name the store failure", err)
		}
		if got := leaser.withdrawn(); len(got) != 0 {
			t.Fatalf("withdrawn grants = %v, want none: the scan never answered", got)
		}
	})
}

// listFailingAgentStore refuses ListSessions and delegates the rest.
type listFailingAgentStore struct {
	AgentStore
	err error
}

func (s *listFailingAgentStore) ListSessions(context.Context, string) ([]*SessionRecord, error) {
	return nil, s.err
}
