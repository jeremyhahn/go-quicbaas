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
	stderrors "errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// breakerPolicy is the rule every test below varies from: three failures in an
// hour trip it, and it stays open for ten minutes past the last one.
func breakerPolicy() BreakerPolicy {
	return BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: 10 * time.Minute}
}

// newBreakerFixture returns a breaker, its ledger and the KV the probe lives
// on, so a test can assert the STORED probe rather than the report of it.
func newBreakerFixture() (CircuitBreaker, ConsumptionLedger, interfaces.KVStore) {
	kv := NewMemoryTaskKV()
	ledger := NewConsumptionLedger(kv)
	return NewCircuitBreaker(ledger, kv), ledger, kv
}

// record writes one outcome into the ledger at a given moment.
func record(t *testing.T, ledger ConsumptionLedger, id string, outcome ConsumptionOutcome, at time.Time) {
	t.Helper()
	entry := validEntry()
	entry.EntryID = id
	entry.Outcome = outcome
	entry.OccurredAt = at
	if err := ledger.Record(context.Background(), entry); err != nil {
		t.Fatalf("record %s: %v", id, err)
	}
}

// =============================================================================
// Evaluate — the derived state
// =============================================================================

func TestCircuitBreakerClosedBelowTheThreshold(t *testing.T) {
	breaker, ledger, _ := newBreakerFixture()
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-30*time.Minute))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, quotaNow.Add(-20*time.Minute))

	verdict, err := breaker.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if verdict.State != BreakerClosed {
		t.Fatalf("two failures against a threshold of three must stay closed, got %q", verdict.State)
	}
	if !verdict.Admits() {
		t.Fatal("a closed breaker must admit")
	}
	if verdict.Failures != 2 {
		t.Fatalf("the verdict must show its working: %d failures, want 2", verdict.Failures)
	}
}

// TestCircuitBreakerThresholdBoundary pins both sides. A threshold tested only
// from far below or far above would agree with an off-by-one.
func TestCircuitBreakerThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failures  int
		wantState BreakerState
	}{
		{"one short", 2, BreakerClosed},
		{"exactly at the threshold", 3, BreakerOpen},
		{"one past", 4, BreakerOpen},
	} {
		t.Run(tc.name, func(t *testing.T) {
			breaker, ledger, _ := newBreakerFixture()
			// All inside the one-hour window AND inside the ten-minute
			// cooldown, so a tripped breaker reports Open rather than
			// half-open — this test is about the threshold, and letting the
			// cooldown expire would measure that instead.
			for i := 0; i < tc.failures; i++ {
				record(t, ledger, "f"+string(rune('a'+i)), ConsumptionOutcomeFailure,
					quotaNow.Add(-time.Duration(tc.failures-i)*time.Minute))
			}
			verdict, err := breaker.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if verdict.State != tc.wantState {
				t.Fatalf("%d failures against a threshold of 3: got %q, want %q",
					tc.failures, verdict.State, tc.wantState)
			}
		})
	}
}

func TestCircuitBreakerOpensUntilTheCooldownElapses(t *testing.T) {
	breaker, ledger, _ := newBreakerFixture()
	last := quotaNow.Add(-5 * time.Minute)
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-30*time.Minute))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, quotaNow.Add(-20*time.Minute))
	record(t, ledger, "f3", ConsumptionOutcomeFailure, last)

	verdict, err := breaker.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if verdict.State != BreakerOpen {
		t.Fatalf("state: got %q, want %q", verdict.State, BreakerOpen)
	}
	if verdict.Admits() {
		t.Fatal("an open breaker must not admit")
	}
	// Measured from the account's last failure, which every host reads
	// identically, rather than from when this host noticed.
	if want := last.Add(10 * time.Minute); !verdict.OpenUntil.Equal(want) {
		t.Fatalf("OpenUntil: got %s, want %s", verdict.OpenUntil, want)
	}
}

func TestCircuitBreakerGoesHalfOpenAfterTheCooldown(t *testing.T) {
	breaker, ledger, _ := newBreakerFixture()
	// The last failure is 15 minutes ago and the cooldown is 10, so the trial
	// is due; all three are still inside the one-hour window.
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-40*time.Minute))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, quotaNow.Add(-30*time.Minute))
	record(t, ledger, "f3", ConsumptionOutcomeFailure, quotaNow.Add(-15*time.Minute))

	verdict, err := breaker.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if verdict.State != BreakerHalfOpen {
		t.Fatalf("state: got %q, want %q", verdict.State, BreakerHalfOpen)
	}
	// Half-open is not open season: ordinary callers are still refused, and
	// only the probe holder proceeds.
	if verdict.Admits() {
		t.Fatal("a half-open breaker must not admit ordinary callers")
	}
	if !verdict.OpenUntil.IsZero() {
		t.Fatalf("OpenUntil belongs to an open breaker only, got %s", verdict.OpenUntil)
	}
}

// TestCircuitBreakerRecoversOnASuccessAfterTheLastFailure is the rule the file
// header states, and the direction that matters: the failures are still in the
// window and still over the threshold, and the account is working again.
func TestCircuitBreakerRecoversOnASuccessAfterTheLastFailure(t *testing.T) {
	breaker, ledger, _ := newBreakerFixture()
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-40*time.Minute))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, quotaNow.Add(-30*time.Minute))
	record(t, ledger, "f3", ConsumptionOutcomeFailure, quotaNow.Add(-20*time.Minute))
	record(t, ledger, "s1", ConsumptionOutcomeSuccess, quotaNow.Add(-5*time.Minute))

	verdict, err := breaker.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if verdict.State != BreakerClosed {
		t.Fatalf("a success after the last failure means the account works: got %q", verdict.State)
	}
	if verdict.Failures != 3 {
		t.Fatalf("the failures are still counted and shown: got %d, want 3", verdict.Failures)
	}
}

// TestCircuitBreakerIsNotSavedByAnEarlierSuccess is the other side of the same
// comparison. Without it, a rule that closed on ANY success in the window
// would pass the test above.
func TestCircuitBreakerIsNotSavedByAnEarlierSuccess(t *testing.T) {
	breaker, ledger, _ := newBreakerFixture()
	record(t, ledger, "s1", ConsumptionOutcomeSuccess, quotaNow.Add(-50*time.Minute))
	record(t, ledger, "f1", ConsumptionOutcomeFailure, quotaNow.Add(-40*time.Minute))
	record(t, ledger, "f2", ConsumptionOutcomeFailure, quotaNow.Add(-30*time.Minute))
	record(t, ledger, "f3", ConsumptionOutcomeFailure, quotaNow.Add(-5*time.Minute))

	verdict, err := breaker.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if verdict.State != BreakerOpen {
		t.Fatalf("a success BEFORE the failures does not clear them: got %q", verdict.State)
	}
	if verdict.LastSuccessAt.After(verdict.LastFailureAt) {
		t.Fatalf("the verdict misreports the order: success %s, failure %s",
			verdict.LastSuccessAt, verdict.LastFailureAt)
	}
}

func TestCircuitBreakerWindowIsSliding(t *testing.T) {
	breaker, ledger, _ := newBreakerFixture()
	// Three failures, but two of them are older than the one-hour window.
	record(t, ledger, "old1", ConsumptionOutcomeFailure, quotaNow.Add(-3*time.Hour))
	record(t, ledger, "old2", ConsumptionOutcomeFailure, quotaNow.Add(-2*time.Hour))
	record(t, ledger, "f3", ConsumptionOutcomeFailure, quotaNow.Add(-5*time.Minute))

	verdict, err := breaker.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if verdict.State != BreakerClosed {
		t.Fatalf("failures older than the window must age out: got %q with %d failures",
			verdict.State, verdict.Failures)
	}
	if verdict.Failures != 1 {
		t.Fatalf("only the in-window failure counts: got %d, want 1", verdict.Failures)
	}
}

// TestCircuitBreakerTellsAnEmptyWindowFromAnUnreadableOne is the provenance
// rule, asserted as one comparison rather than two separate tests, because the
// property IS the difference between them.
//
// A breaker that read store-failure as zero-failures would CLOSE exactly when
// the store is sick — which is when accounts are most likely being hammered —
// and it would look identical to a healthy quiet account from the outside. So
// "nothing happened" must produce a verdict and "I could not find out" must
// produce an error, and no input may produce the other one's answer.
func TestCircuitBreakerTellsAnEmptyWindowFromAnUnreadableOne(t *testing.T) {
	empty, _, _ := newBreakerFixture()
	quiet, err := empty.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
	if err != nil {
		t.Fatalf("an account with no history is knowledge, not a failure: %v", err)
	}
	if quiet.State != BreakerClosed || quiet.Failures != 0 {
		t.Fatalf("an empty window is a closed breaker with no failures, got %q with %d",
			quiet.State, quiet.Failures)
	}

	unreadable := NewCircuitBreaker(&countingLedger{
		ConsumptionLedger: NewInMemoryConsumptionLedger(),
		windowErr:         stderrors.New("qrdb unavailable"),
	}, NewMemoryTaskKV())
	blind, err := unreadable.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
	if err == nil {
		t.Fatal("an unreadable window must not be answerable")
	}
	if blind != nil {
		t.Fatalf("an unreadable window must yield NO verdict, got %+v", blind)
	}
}

// TestCircuitBreakerDerivationCostIsBoundedByTheWindow keeps the breaker off
// the slow path. Its decision must cost what its POLICY WINDOW is worth, not
// what the account's history is worth — otherwise the check on the hot path
// gets slower every day the account is used, and the first symptom is latency
// nobody attributes to the breaker.
func TestCircuitBreakerDerivationCostIsBoundedByTheWindow(t *testing.T) {
	kv := newScanCountingKV()
	ledger := NewConsumptionLedger(kv)
	breaker := NewCircuitBreaker(ledger, kv)

	// A year of history, far outside any policy window under test.
	for i := 0; i < 40; i++ {
		record(t, ledger, "ancient-"+strconv.Itoa(i), ConsumptionOutcomeFailure,
			quotaNow.Add(-time.Duration(i+1)*24*time.Hour))
	}
	kv.scans = 0

	// A one-hour window touches at most two hourly buckets whatever sits
	// behind it. The assertion is the BOUND, not an exact figure, so the
	// bucket size can change without rewriting the test — what may not change
	// is the cost growing with history.
	if _, err := breaker.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if kv.scans > 2 {
		t.Fatalf("a one-hour window scanned %d buckets against 40 days of history; "+
			"the derivation must be bounded by the window (prefixes: %v)", kv.scans, kv.prefixes)
	}
}

func TestCircuitBreakerRefusesToGuessWhenTheLedgerFails(t *testing.T) {
	ledger := &countingLedger{
		ConsumptionLedger: NewInMemoryConsumptionLedger(),
		windowErr:         stderrors.New("qrdb unavailable"),
	}
	breaker := NewCircuitBreaker(ledger, NewMemoryTaskKV())

	verdict, err := breaker.Evaluate(context.Background(), "t1", "a1", breakerPolicy(), quotaNow)
	if verdict != nil {
		t.Fatalf("an unreadable failure history must produce NO verdict, got %+v", verdict)
	}
	if err == nil {
		t.Fatal("an unreadable ledger is not a healthy account")
	}
}

func TestCircuitBreakerPolicyValidation(t *testing.T) {
	breaker, _, _ := newBreakerFixture()
	ctx := context.Background()

	refused := []struct {
		name   string
		policy BreakerPolicy
		field  string
	}{
		{"zero window", BreakerPolicy{Window: 0, FailureThreshold: 3, Cooldown: time.Minute}, "window"},
		{"negative window", BreakerPolicy{Window: -time.Hour, FailureThreshold: 3, Cooldown: time.Minute}, "window"},
		{"zero threshold", BreakerPolicy{Window: time.Hour, FailureThreshold: 0, Cooldown: time.Minute}, "failure_threshold"},
		{"negative threshold", BreakerPolicy{Window: time.Hour, FailureThreshold: -1, Cooldown: time.Minute}, "failure_threshold"},
		{"zero cooldown", BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: 0}, "cooldown"},
		{"negative cooldown", BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: -time.Minute}, "cooldown"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			verdict, err := breaker.Evaluate(ctx, "t1", "a1", tc.policy, quotaNow)
			if verdict != nil {
				t.Fatalf("a malformed policy must yield no verdict, got %+v", verdict)
			}
			var invalid *BreakerInvalidError
			if !stderrors.As(err, &invalid) {
				t.Fatalf("want BreakerInvalidError, got: %v", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("refusal must name the field: got %q, want %q", invalid.Field, tc.field)
			}
		})
	}

	// The permitting direction: the smallest legal policy is accepted. A
	// validation tightened into refusing everything fails here.
	if _, err := breaker.Evaluate(ctx, "t1", "a1", BreakerPolicy{
		Window: time.Nanosecond, FailureThreshold: 1, Cooldown: time.Nanosecond,
	}, quotaNow); err != nil {
		t.Fatalf("the smallest legal policy must be accepted, got: %v", err)
	}
}

func TestBreakerVerdictAdmitsOnlyWhenClosed(t *testing.T) {
	var absent *BreakerVerdict
	if absent.Admits() {
		t.Fatal("a nil verdict must not admit")
	}
	for state, want := range map[BreakerState]bool{
		BreakerClosed:   true,
		BreakerOpen:     false,
		BreakerHalfOpen: false,
	} {
		if got := (&BreakerVerdict{State: state}).Admits(); got != want {
			t.Fatalf("state %q admits=%v, want %v", state, got, want)
		}
	}
}

// =============================================================================
// The probe — the one decision that cannot be derived
// =============================================================================

func TestCircuitBreakerProbeIsHeldByExactlyOne(t *testing.T) {
	breaker, _, kv := newBreakerFixture()
	ctx := context.Background()

	won, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow, time.Minute)
	if err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	if !won {
		t.Fatal("the first caller must win the probe")
	}

	// Losing is an ordinary outcome, not an error: the second host simply
	// keeps away.
	lost, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-b", quotaNow, time.Minute)
	if err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	if lost {
		t.Fatal("a second caller must not also hold the single probe")
	}

	// The stored artifact names the winner, not the last caller.
	raw, err := kv.Get(ctx, "orch/costprobe/t1/a1")
	if err != nil {
		t.Fatalf("stored probe missing: %v", err)
	}
	var probe breakerProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if probe.Holder != "host-a" {
		t.Fatalf("stored probe holder: got %q, want %q", probe.Holder, "host-a")
	}
}

func TestCircuitBreakerProbeIsReentrantForItsHolder(t *testing.T) {
	breaker, _, _ := newBreakerFixture()
	ctx := context.Background()

	if _, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow, time.Minute); err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	again, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow.Add(time.Second), time.Minute)
	if err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	if !again {
		t.Fatal("a holder asking twice must not be told it lost its own probe")
	}
}

func TestCircuitBreakerProbeIsReclaimedOnlyOnceExpired(t *testing.T) {
	breaker, _, _ := newBreakerFixture()
	ctx := context.Background()

	if _, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow, time.Minute); err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}

	// Still live: another host must not take it. This is the direction that
	// stops a reclaim rule from simply ignoring the expiry.
	early, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-b", quotaNow.Add(30*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	if early {
		t.Fatal("a live probe must not be reclaimable")
	}

	// Expired: the holder died mid-trial, and the probe must not be lost
	// forever, or the breaker never closes again.
	late, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-b", quotaNow.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	if !late {
		t.Fatal("an expired probe must be reclaimable, or a dead holder parks the account forever")
	}
}

func TestCircuitBreakerProbeReleaseFreesIt(t *testing.T) {
	breaker, _, kv := newBreakerFixture()
	ctx := context.Background()

	if _, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow, time.Minute); err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	if err := breaker.ReleaseProbe(ctx, "t1", "a1", "host-a"); err != nil {
		t.Fatalf("ReleaseProbe: %v", err)
	}
	if exists, _ := kv.Exists(ctx, "orch/costprobe/t1/a1"); exists {
		t.Fatal("a released probe must be gone from the store")
	}

	won, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-b", quotaNow, time.Minute)
	if err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	if !won {
		t.Fatal("the next caller must be able to take a released probe")
	}
}

func TestCircuitBreakerProbeReleaseByNonHolderIsRefused(t *testing.T) {
	breaker, _, kv := newBreakerFixture()
	ctx := context.Background()

	if _, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow, time.Minute); err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}

	err := breaker.ReleaseProbe(ctx, "t1", "a1", "host-b")
	var notHeld *BreakerProbeNotHeldError
	if !stderrors.As(err, &notHeld) {
		t.Fatalf("want BreakerProbeNotHeldError, got: %v", err)
	}
	if notHeld.Actual != "host-a" {
		t.Fatalf("the refusal must name the real holder, got %q", notHeld.Actual)
	}

	// And the probe SURVIVES. A refusal that had already deleted it would
	// leave the account with a trial nobody is running.
	if exists, _ := kv.Exists(ctx, "orch/costprobe/t1/a1"); !exists {
		t.Fatal("a refused release must leave the probe in place")
	}
}

func TestCircuitBreakerProbeReleaseIsIdempotent(t *testing.T) {
	breaker, _, _ := newBreakerFixture()
	ctx := context.Background()

	// Releasing what is not held is success, not failure: a caller retrying a
	// release must not have to handle an error for succeeding.
	if err := breaker.ReleaseProbe(ctx, "t1", "a1", "host-a"); err != nil {
		t.Fatalf("releasing an absent probe must be a no-op, got: %v", err)
	}
	if _, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow, time.Minute); err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	if err := breaker.ReleaseProbe(ctx, "t1", "a1", "host-a"); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := breaker.ReleaseProbe(ctx, "t1", "a1", "host-a"); err != nil {
		t.Fatalf("second release must also succeed, got: %v", err)
	}
}

func TestCircuitBreakerProbeRefusesAnUnguardedStore(t *testing.T) {
	unguarded := NewCircuitBreaker(NewInMemoryConsumptionLedger(), newPlainKV())
	ctx := context.Background()

	_, err := unguarded.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow, time.Minute)
	var refusal *BreakerUnguardedError
	if !stderrors.As(err, &refusal) {
		t.Fatalf("a store that cannot decide a create must be refused, got: %v", err)
	}
	if err := unguarded.ReleaseProbe(ctx, "t1", "a1", "host-a"); !stderrors.As(err, &refusal) {
		t.Fatalf("release must refuse the same store, got: %v", err)
	}

	// The permitting direction: a guarded store takes the same probe.
	guarded, _, _ := newBreakerFixture()
	if won, err := guarded.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow, time.Minute); err != nil || !won {
		t.Fatalf("a guarded store must grant the probe: won=%v err=%v", won, err)
	}
}

func TestCircuitBreakerProbeValidation(t *testing.T) {
	breaker, _, _ := newBreakerFixture()
	ctx := context.Background()

	for _, tc := range []struct {
		name                    string
		tenant, account, holder string
		ttl                     time.Duration
		field                   string
	}{
		{"no tenant", "", "a1", "host-a", time.Minute, "tenant_id"},
		{"no account", "t1", "", "host-a", time.Minute, "account_id"},
		{"no holder", "t1", "a1", "", time.Minute, "holder"},
		{"tenant with separator", "t/1", "a1", "host-a", time.Minute, "tenant_id"},
		{"holder with separator", "t1", "a1", "host/a", time.Minute, "holder"},
		{"zero ttl", "t1", "a1", "host-a", 0, "ttl"},
		{"negative ttl", "t1", "a1", "host-a", -time.Minute, "ttl"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := breaker.AcquireProbe(ctx, tc.tenant, tc.account, tc.holder, quotaNow, tc.ttl)
			var invalid *BreakerInvalidError
			if !stderrors.As(err, &invalid) {
				t.Fatalf("want BreakerInvalidError, got: %v", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("refusal must name the field: got %q, want %q", invalid.Field, tc.field)
			}
		})
	}
}

// TestCircuitBreakerProbeReleaseValidatesIdentity is the sibling of
// TestCircuitBreakerProbeValidation, and it exists because acquire validated
// and release did not. An empty tenant on release builds a key belonging to no
// account, finds nothing there, and would report a successful release of a
// probe that is still held.
func TestCircuitBreakerProbeReleaseValidatesIdentity(t *testing.T) {
	breaker, _, _ := newBreakerFixture()
	ctx := context.Background()

	for _, tc := range []struct {
		name                    string
		tenant, account, holder string
		field                   string
	}{
		{"no tenant", "", "a1", "host-a", "tenant_id"},
		{"no account", "t1", "", "host-a", "account_id"},
		{"no holder", "t1", "a1", "", "holder"},
		{"account with separator", "t1", "a/1", "host-a", "account_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := breaker.ReleaseProbe(ctx, tc.tenant, tc.account, tc.holder)
			var invalid *BreakerInvalidError
			if !stderrors.As(err, &invalid) {
				t.Fatalf("want BreakerInvalidError, got: %v", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("refusal must name the field: got %q, want %q", invalid.Field, tc.field)
			}
		})
	}

	// The permitting direction: a well-formed release still works, so the
	// added guard has not turned every release into a refusal.
	if _, err := breaker.AcquireProbe(ctx, "t1", "a1", "host-a", quotaNow, time.Minute); err != nil {
		t.Fatalf("AcquireProbe: %v", err)
	}
	if err := breaker.ReleaseProbe(ctx, "t1", "a1", "host-a"); err != nil {
		t.Fatalf("a well-formed release must succeed, got: %v", err)
	}
}

// TestCircuitBreakerProbeReadFailureNamesTheRecord covers the second review
// finding: a probe fault that names nothing is a report rather than a lead.
func TestCircuitBreakerProbeReadFailureNamesTheRecord(t *testing.T) {
	kv := &probeReadFaultKV{KVStore: NewMemoryTaskKV()}
	breaker := NewCircuitBreaker(NewInMemoryConsumptionLedger(), kv)

	err := breaker.ReleaseProbe(context.Background(), "t1", "a1", "host-a")
	var probeErr *BreakerProbeError
	if !stderrors.As(err, &probeErr) {
		t.Fatalf("want BreakerProbeError, got: %v", err)
	}
	if probeErr.Key != "orch/costprobe/t1/a1" {
		t.Fatalf("the failure must name the record: got %q", probeErr.Key)
	}
	if !strings.Contains(probeErr.Error(), "orch/costprobe/t1/a1") {
		t.Fatalf("the rendered message must carry the key, got %q", probeErr.Error())
	}
}

// probeReadFaultKV fails reads of the probe record while leaving writes alone.
type probeReadFaultKV struct {
	interfaces.KVStore
}

func (k *probeReadFaultKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *probeReadFaultKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

func (k *probeReadFaultKV) Get(context.Context, string) ([]byte, error) {
	return nil, stderrors.New("probe record unreadable")
}

func TestBreakerErrorsUnwrap(t *testing.T) {
	cases := []struct {
		err      error
		sentinel error
		contains string
	}{
		{&BreakerInvalidError{Field: "window", Message: "must be positive"}, ErrBreakerInvalid, "window"},
		{&BreakerUnguardedError{Message: "cannot decide"}, ErrBreakerUnguarded, "cannot decide"},
		{&BreakerProbeError{AccountID: "a1", Cause: stderrors.New("boom")}, ErrBreakerProbe, "boom"},
		{&BreakerProbeError{AccountID: "a1"}, ErrBreakerProbe, "a1"},
		{&BreakerProbeAbsentError{Key: "orch/costprobe/t1/a1"}, ErrBreakerProbeAbsent, "orch/costprobe/t1/a1"},
		{&BreakerProbeNotHeldError{AccountID: "a1", Holder: "b", Actual: "a"}, ErrBreakerProbeNotHeld, "held by a"},
	}
	for _, tc := range cases {
		if !stderrors.Is(tc.err, tc.sentinel) {
			t.Fatalf("%q must unwrap to its sentinel", tc.err)
		}
		if !strings.Contains(tc.err.Error(), tc.contains) {
			t.Fatalf("message %q must carry %q", tc.err.Error(), tc.contains)
		}
	}
}
