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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ledgerBase is the instant every fixture is placed relative to. A fixed,
// mid-hour, mid-day UTC time rather than time.Now(): the bucket arithmetic
// under test truncates to the hour, so a fixture anchored at a random moment
// would straddle a boundary on some runs and not others.
var ledgerBase = time.Date(2026, 8, 22, 10, 30, 0, 0, time.UTC)

// newLedgerFixture returns a ledger and the KV beneath it, so a test can assert
// the STORED record at a key it computes itself rather than trusting the
// ledger's own report of what it wrote.
func newLedgerFixture() (ConsumptionLedger, interfaces.KVStore) {
	kv := NewMemoryTaskKV()
	return NewConsumptionLedger(kv), kv
}

// validEntry is a complete, acceptable entry. Every validation test mutates
// exactly one field of it, so a refusal can only be attributed to that field —
// and the unmutated entry is itself asserted to be ACCEPTED, which is the half
// of a guard test that is easy to leave out.
func validEntry() ConsumptionEntry {
	return ConsumptionEntry{
		EntryID:          "e1",
		TenantID:         "t1",
		ProjectID:        "p1",
		TaskID:           "w1",
		JobID:            "pr1",
		SessionID:        "s1",
		Round:            1,
		AccountID:        "a1",
		Engine:           "acp",
		Model:            "claude-opus-5",
		Outcome:          ConsumptionOutcomeSuccess,
		InputTokens:      1,
		OutputTokens:     2,
		EquivalentMicros: 500,
		PriceRef:         "v1:default:*/claude-opus-5",
		OccurredAt:       ledgerBase,
	}
}

// =============================================================================
// Record — the stored artifact
// =============================================================================

func TestConsumptionLedgerRecordWritesBothPlanes(t *testing.T) {
	ledger, kv := newLedgerFixture()
	ctx := context.Background()
	entry := validEntry()

	if err := ledger.Record(ctx, entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// The keys are spelled out rather than recomputed from the code under
	// test. A helper shared with the implementation would agree with it about
	// a wrong layout, and the layout is what makes a roll-up a prefix scan.
	chainKey := "orch/costchain/t1/p1/w1/pr1/s1/000001/e1"
	windowKey := "orch/costwindow/t1/a1/" +
		nanosSegment(ledgerBase.Truncate(time.Hour)) + "/" +
		nanosSegment(ledgerBase) + "/e1"

	for _, key := range []string{chainKey, windowKey} {
		raw, err := kv.Get(ctx, key)
		if err != nil {
			t.Fatalf("stored record missing at %s: %v", key, err)
		}
		var stored ConsumptionEntry
		if err := json.Unmarshal(raw, &stored); err != nil {
			t.Fatalf("stored record at %s does not decode: %v", key, err)
		}
		if stored.EntryID != entry.EntryID || stored.TotalTokens() != 3 || stored.EquivalentMicros != 500 {
			t.Fatalf("stored record at %s is not the entry recorded: %+v", key, stored)
		}
	}
}

func TestConsumptionLedgerRecordFillsUnattributedLevels(t *testing.T) {
	ledger, kv := newLedgerFixture()
	ctx := context.Background()

	entry := validEntry()
	entry.TaskID = ""
	entry.JobID = ""
	entry.SessionID = ""
	entry.Round = 0

	if err := ledger.Record(ctx, entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// The levels the caller had no value for are PRESENT in the key as the
	// sentinel. Were they collapsed, this entry's key would be four segments
	// short and a project roll-up would read the round segment as a task.
	key := "orch/costchain/t1/p1/-/-/-/000000/e1"
	if _, err := kv.Get(ctx, key); err != nil {
		t.Fatalf("unattributed levels not filled; expected a record at %s: %v", key, err)
	}
}

func TestConsumptionLedgerRecordIsIdempotent(t *testing.T) {
	ledger, _ := newLedgerFixture()
	ctx := context.Background()
	entry := validEntry()

	if err := ledger.Record(ctx, entry); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	if err := ledger.Record(ctx, entry); err != nil {
		t.Fatalf("re-recording an identical entry must be a no-op, got: %v", err)
	}

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 1 {
		t.Fatalf("a retried entry was charged twice: got %d entries", rollup.Entries)
	}
	if rollup.TotalTokens != 3 {
		t.Fatalf("a retried entry doubled the tokens: got %d, want 3", rollup.TotalTokens)
	}
}

func TestConsumptionLedgerRecordRefusesConflictingEntryID(t *testing.T) {
	ledger, kv := newLedgerFixture()
	ctx := context.Background()
	first := validEntry()

	if err := ledger.Record(ctx, first); err != nil {
		t.Fatalf("first Record: %v", err)
	}

	second := first
	second.InputTokens = 9999

	err := ledger.Record(ctx, second)
	var conflict *ConsumptionConflictError
	if !stderrors.As(err, &conflict) {
		t.Fatalf("two different records under one entry id must be refused, got: %v", err)
	}
	if !stderrors.Is(err, ErrConsumptionConflict) {
		t.Fatalf("conflict must unwrap to ErrConsumptionConflict, got: %v", err)
	}
	if conflict.EntryID != "e1" {
		t.Fatalf("conflict must name the entry id, got %q", conflict.EntryID)
	}
	// The refusal detects on the quota plane, which is written first.
	if conflict.Plane != consumptionPlaneWindow {
		t.Fatalf("conflict must name the plane it was detected on, got %q", conflict.Plane)
	}

	// The first record stands. A conflict that overwrote what was there would
	// be a silent re-price of consumption already accounted for.
	raw, err := kv.Get(ctx, "orch/costchain/t1/p1/w1/pr1/s1/000001/e1")
	if err != nil {
		t.Fatalf("original record missing after a refused conflict: %v", err)
	}
	var stored ConsumptionEntry
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stored.InputTokens != 1 {
		t.Fatalf("a refused conflict overwrote the stored record: input tokens %d, want 1", stored.InputTokens)
	}
}

func TestConsumptionLedgerRecordRefusesUnguardedStore(t *testing.T) {
	// An unguarded store is the raw storage adapter: it has the six plain
	// verbs and no conditional write to delegate to.
	unguarded := NewConsumptionLedger(newPlainKV())
	err := unguarded.Record(context.Background(), validEntry())

	var refusal *ConsumptionUnguardedError
	if !stderrors.As(err, &refusal) {
		t.Fatalf("a store that cannot decide a create must be refused, got: %v", err)
	}

	// The permitting direction: the identical entry through a guarded store is
	// accepted. Without this half, a Record that refused every store would
	// satisfy the test above.
	guarded, _ := newLedgerFixture()
	if err := guarded.Record(context.Background(), validEntry()); err != nil {
		t.Fatalf("a guarded store must accept the same entry, got: %v", err)
	}
}

// TestConsumptionLedgerRecordWritesQuotaPlaneFirst pins the ordering the
// implementation reasons about: a failure between the two planes must leave the
// spend VISIBLE to the limit decision and missing from the roll-up, never the
// other way round.
func TestConsumptionLedgerRecordWritesQuotaPlaneFirst(t *testing.T) {
	kv := newChainFaultKV()
	ledger := NewConsumptionLedger(kv)
	ctx := context.Background()
	entry := validEntry()

	err := ledger.Record(ctx, entry)
	var writeErr *ConsumptionWriteError
	if !stderrors.As(err, &writeErr) {
		t.Fatalf("a failed plane write must be reported, got: %v", err)
	}
	if writeErr.Plane != consumptionPlaneChain {
		t.Fatalf("the error must name the plane that failed, got %q", writeErr.Plane)
	}

	windowKey := "orch/costwindow/t1/a1/" +
		nanosSegment(ledgerBase.Truncate(time.Hour)) + "/" +
		nanosSegment(ledgerBase) + "/e1"
	if _, err := kv.KVStore.Get(ctx, windowKey); err != nil {
		t.Fatalf("the quota plane must already hold the entry when the attribution write fails: %v", err)
	}

	// And the caller's retry against a healthy store completes the pair
	// instead of duplicating the half that landed.
	kv.failChain = false
	if err := ledger.Record(ctx, entry); err != nil {
		t.Fatalf("retry after a partial write: %v", err)
	}
	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 1 {
		t.Fatalf("retry after a partial write charged twice: %d entries", rollup.Entries)
	}
	window, err := ledger.WindowUsage(ctx, "t1", "a1", ledgerBase.Add(-time.Hour), ledgerBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("WindowUsage: %v", err)
	}
	if window.Entries != 1 {
		t.Fatalf("retry after a partial write duplicated the quota-plane record: %d entries", window.Entries)
	}
}

// =============================================================================
// Record — validation, both directions
// =============================================================================

func TestConsumptionLedgerRecordValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ConsumptionEntry)
		field   string
		wantErr bool
	}{
		{"missing entry id", func(e *ConsumptionEntry) { e.EntryID = "" }, "entry_id", true},
		{"entry id with separator", func(e *ConsumptionEntry) { e.EntryID = "a/b" }, "entry_id", true},
		{"missing tenant", func(e *ConsumptionEntry) { e.TenantID = "" }, "tenant_id", true},
		{"tenant with separator", func(e *ConsumptionEntry) { e.TenantID = "t/1" }, "tenant_id", true},
		{"account with separator", func(e *ConsumptionEntry) { e.AccountID = "a/1" }, "account_id", true},
		{"missing engine", func(e *ConsumptionEntry) { e.Engine = "" }, "engine", true},
		{"missing model", func(e *ConsumptionEntry) { e.Model = "" }, "model", true},
		{"engine with separator", func(e *ConsumptionEntry) { e.Engine = "a/b" }, "engine", true},
		{"project spelling the sentinel", func(e *ConsumptionEntry) { e.ProjectID = consumptionUnattributed }, "project_id", true},
		{"task spelling the sentinel", func(e *ConsumptionEntry) { e.TaskID = consumptionUnattributed }, "task_id", true},
		{"session with separator", func(e *ConsumptionEntry) { e.SessionID = "s/1" }, "session_id", true},
		{"unknown outcome", func(e *ConsumptionEntry) { e.Outcome = "maybe" }, "outcome", true},
		{"empty outcome", func(e *ConsumptionEntry) { e.Outcome = "" }, "outcome", true},
		{"negative round", func(e *ConsumptionEntry) { e.Round = -1 }, "round", true},
		{"negative input tokens", func(e *ConsumptionEntry) { e.InputTokens = -1 }, "input_tokens", true},
		{"negative output tokens", func(e *ConsumptionEntry) { e.OutputTokens = -1 }, "output_tokens", true},
		{"negative cost", func(e *ConsumptionEntry) { e.EquivalentMicros = -1 }, "equivalent_micros", true},
		{"cost without its price", func(e *ConsumptionEntry) { e.PriceRef = "" }, "price_ref", true},
		{"negative cache read", func(e *ConsumptionEntry) { e.CacheReadTokens = -1 }, "cache_read_tokens", true},
		{"negative cache write", func(e *ConsumptionEntry) { e.CacheWriteTokens = -1 }, "cache_write_tokens", true},
		{"negative duration", func(e *ConsumptionEntry) { e.DurationMillis = -1 }, "duration_ms", true},
		{"zero occurred at", func(e *ConsumptionEntry) { e.OccurredAt = time.Time{} }, "occurred_at", true},

		// The permitting direction. Each of these sits on the other side of a
		// guard above, so a rule tightened into refusing everything fails here
		// rather than shipping as caution.
		{"unchanged", func(*ConsumptionEntry) {}, "", false},
		{"failure outcome", func(e *ConsumptionEntry) { e.Outcome = ConsumptionOutcomeFailure }, "", false},
		{"round zero", func(e *ConsumptionEntry) { e.Round = 0 }, "", false},
		{"zero tokens", func(e *ConsumptionEntry) { e.InputTokens, e.OutputTokens = 0, 0 }, "", false},
		{"zero cost", func(e *ConsumptionEntry) { e.EquivalentMicros = 0 }, "", false},
		{"unpriced", func(e *ConsumptionEntry) { e.EquivalentMicros, e.PriceRef = 0, "" }, "", false},
		{"no account (unpooled)", func(e *ConsumptionEntry) { e.AccountID = "" }, "", false},
		{"no project", func(e *ConsumptionEntry) { e.ProjectID = "" }, "", false},
		{"no session", func(e *ConsumptionEntry) { e.SessionID = "" }, "", false},
		{"sentinel-adjacent id", func(e *ConsumptionEntry) { e.TaskID = "--" }, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ledger, _ := newLedgerFixture()
			entry := validEntry()
			tc.mutate(&entry)

			err := ledger.Record(context.Background(), entry)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("entry must be accepted, got: %v", err)
				}
				return
			}

			var invalid *ConsumptionInvalidError
			if !stderrors.As(err, &invalid) {
				t.Fatalf("want ConsumptionInvalidError, got: %v", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("refusal must name the offending field: got %q, want %q", invalid.Field, tc.field)
			}
			if !stderrors.Is(err, ErrConsumptionInvalid) {
				t.Fatalf("refusal must unwrap to ErrConsumptionInvalid, got: %v", err)
			}
		})
	}
}

// =============================================================================
// Roll-ups
// =============================================================================

// seedRollupFixture writes a tree whose every level has a DISTINCT total, so a
// roll-up that scans one level too high or too low reports a number no other
// level could produce. Equal totals would let a wrong prefix pass.
//
//	t1 total                     11143
//	  p1                          1143
//	    w1                         143
//	      pr1                       43
//	        s1                      13   (round 1: 3, round 2: 10)
//	        s2                      30
//	      pr2                      100
//	    w2                        1000   (no job, no session)
//	  p2                         10000
//	t2 (another tenant)         100000
//	t1x (a tenant whose id extends t1's)  7
func seedRollupFixture(t *testing.T, ledger ConsumptionLedger) {
	t.Helper()
	ctx := context.Background()

	entries := []ConsumptionEntry{
		{EntryID: "e1", TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s1", Round: 1,
			AccountID: "a1", Engine: "acp", Model: "opus", Outcome: ConsumptionOutcomeSuccess,
			InputTokens: 1, OutputTokens: 2, EquivalentMicros: 10, PriceRef: "ref", OccurredAt: ledgerBase},
		{EntryID: "e2", TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s1", Round: 2,
			AccountID: "a1", Engine: "acp", Model: "opus", Outcome: ConsumptionOutcomeFailure,
			InputTokens: 4, OutputTokens: 6, EquivalentMicros: 20, PriceRef: "ref", OccurredAt: ledgerBase.Add(time.Minute)},
		{EntryID: "e3", TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s2", Round: 1,
			AccountID: "a1", Engine: "codex", Model: "gpt", Outcome: ConsumptionOutcomeSuccess,
			InputTokens: 10, OutputTokens: 20, EquivalentMicros: 30, PriceRef: "ref", OccurredAt: ledgerBase.Add(2 * time.Minute)},
		{EntryID: "e4", TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr2", SessionID: "s3", Round: 0,
			AccountID: "a2", Engine: "acp", Model: "opus", Outcome: ConsumptionOutcomeSuccess,
			InputTokens: 100, OutputTokens: 0, EquivalentMicros: 40, PriceRef: "ref", OccurredAt: ledgerBase.Add(3 * time.Minute)},
		{EntryID: "e5", TenantID: "t1", ProjectID: "p1", TaskID: "w2", Round: 0,
			AccountID: "a1", Engine: "acp", Model: "opus", Outcome: ConsumptionOutcomeSuccess,
			InputTokens: 1000, OutputTokens: 0, EquivalentMicros: 50, PriceRef: "ref", OccurredAt: ledgerBase.Add(4 * time.Minute)},
		{EntryID: "e6", TenantID: "t1", ProjectID: "p2", Round: 0,
			AccountID: "a3", Engine: "codex", Model: "gpt", Outcome: ConsumptionOutcomeSuccess,
			InputTokens: 10000, OutputTokens: 0, EquivalentMicros: 60, PriceRef: "ref", OccurredAt: ledgerBase.Add(5 * time.Minute)},
		{EntryID: "e7", TenantID: "t2", ProjectID: "p1", Round: 0,
			AccountID: "a1", Engine: "acp", Model: "opus", Outcome: ConsumptionOutcomeSuccess,
			InputTokens: 100000, OutputTokens: 0, EquivalentMicros: 70, PriceRef: "ref", OccurredAt: ledgerBase},
		{EntryID: "e8", TenantID: "t1x", ProjectID: "p1", Round: 0,
			AccountID: "a1", Engine: "acp", Model: "opus", Outcome: ConsumptionOutcomeSuccess,
			InputTokens: 7, OutputTokens: 0, EquivalentMicros: 80, PriceRef: "ref", OccurredAt: ledgerBase},
	}
	for _, entry := range entries {
		if err := ledger.Record(ctx, entry); err != nil {
			t.Fatalf("seed %s: %v", entry.EntryID, err)
		}
	}
}

func TestConsumptionLedgerRollupAtEveryLevel(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)

	tests := []struct {
		name       string
		scope      ConsumptionScope
		wantTokens int64
		wantCost   int64
		wantCount  int
	}{
		{"tenant", ConsumptionScope{TenantID: "t1"}, 11143, 210, 6},
		{"project", ConsumptionScope{TenantID: "t1", ProjectID: "p1"}, 1143, 150, 5},
		{"other project", ConsumptionScope{TenantID: "t1", ProjectID: "p2"}, 10000, 60, 1},
		{"task", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1"}, 143, 100, 4},
		{"job", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1"}, 43, 60, 3},
		{"session", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s1"}, 13, 30, 2},
		{"round", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s1", Round: 1, RoundSet: true}, 3, 10, 1},
		{"round two", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s1", Round: 2, RoundSet: true}, 10, 20, 1},
		{"other tenant", ConsumptionScope{TenantID: "t2"}, 100000, 70, 1},
		{"tenant whose id extends another", ConsumptionScope{TenantID: "t1x"}, 7, 80, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rollup, err := ledger.Rollup(context.Background(), tc.scope)
			if err != nil {
				t.Fatalf("Rollup: %v", err)
			}
			if rollup.TotalTokens != tc.wantTokens {
				t.Fatalf("tokens: got %d, want %d", rollup.TotalTokens, tc.wantTokens)
			}
			if rollup.EquivalentMicros != tc.wantCost {
				t.Fatalf("cost: got %d, want %d", rollup.EquivalentMicros, tc.wantCost)
			}
			if rollup.Entries != tc.wantCount {
				t.Fatalf("entries: got %d, want %d", rollup.Entries, tc.wantCount)
			}
			if rollup.InputTokens+rollup.OutputTokens != rollup.TotalTokens {
				t.Fatalf("input+output must equal the total: %d + %d != %d",
					rollup.InputTokens, rollup.OutputTokens, rollup.TotalTokens)
			}
		})
	}
}

func TestConsumptionLedgerRollupUnattributedLevelStaysDistinct(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)

	// w2 has no job. Its spend must be reachable at the task level and must
	// NOT appear under any named job of its sibling.
	item, err := ledger.Rollup(context.Background(), ConsumptionScope{
		TenantID: "t1", ProjectID: "p1", TaskID: "w2",
	})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if item.TotalTokens != 1000 {
		t.Fatalf("unattributed-level spend lost: got %d, want 1000", item.TotalTokens)
	}
	if got := item.ByChild[consumptionUnattributed].TotalTokens; got != 1000 {
		t.Fatalf("child breakdown must attribute to the sentinel: got %d, want 1000", got)
	}
}

// TestConsumptionLedgerRollupAddressesTheUnattributedSubtree asserts a scope
// may NAME the sentinel to open the subtree the breakdown above points at.
//
// THE WRITE SIDE IS WHAT MAKES THE READ SIDE UNAMBIGUOUS. An entry may not
// spell the sentinel as a chain value (see the validation table above), so no
// real task, job or session can bear that id, so a scope naming it can only
// mean "the entries with no value here". Refusing it on the read side instead
// would leave the subtree REPORTABLE BUT NOT ADDRESSABLE: a roll-up shows an
// unattributed row and nothing can drill into it. go-quicbaas#794.
func TestConsumptionLedgerRollupAddressesTheUnattributedSubtree(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)

	unattributed, err := ledger.Rollup(context.Background(), ConsumptionScope{
		TenantID: "t1", ProjectID: "p1", TaskID: "w2", JobID: consumptionUnattributed,
	})
	if err != nil {
		t.Fatalf("scoping to the sentinel must address the unattributed subtree: %v", err)
	}
	if unattributed.TotalTokens != 1000 || unattributed.Entries != 1 {
		t.Fatalf("unattributed subtree = %d tokens over %d entries, want 1000 over 1",
			unattributed.TotalTokens, unattributed.Entries)
	}

	// And it selects ONLY that subtree: w1's named jobs are a sibling, not a
	// member, so a sentinel scope under w1 sums nothing.
	sibling, err := ledger.Rollup(context.Background(), ConsumptionScope{
		TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: consumptionUnattributed,
	})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if sibling.Entries != 0 {
		t.Fatalf("the sentinel must not select named jobs: got %d entries", sibling.Entries)
	}
}

func TestConsumptionLedgerRollupChildBreakdown(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)

	tests := []struct {
		name  string
		scope ConsumptionScope
		want  map[string]int64
	}{
		{"projects below the tenant", ConsumptionScope{TenantID: "t1"}, map[string]int64{"p1": 1143, "p2": 10000}},
		{"tasks below the project", ConsumptionScope{TenantID: "t1", ProjectID: "p1"}, map[string]int64{"w1": 143, "w2": 1000}},
		{"jobs below the task", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1"}, map[string]int64{"pr1": 43, "pr2": 100}},
		{"sessions below the job", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1"}, map[string]int64{"s1": 13, "s2": 30}},
		{"rounds below the session", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s1"}, map[string]int64{"000001": 3, "000002": 10}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rollup, err := ledger.Rollup(context.Background(), tc.scope)
			if err != nil {
				t.Fatalf("Rollup: %v", err)
			}
			if len(rollup.ByChild) != len(tc.want) {
				t.Fatalf("child breakdown: got %v, want %v", rollup.ByChild, tc.want)
			}
			for child, tokens := range tc.want {
				if rollup.ByChild[child] == nil || rollup.ByChild[child].TotalTokens != tokens {
					t.Fatalf("child %q: got %+v, want %d tokens (full: %v)", child, rollup.ByChild[child], tokens, rollup.ByChild)
				}
			}
		})
	}
}

func TestConsumptionLedgerRollupHasNoChildAtTheDeepestLevel(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)

	rollup, err := ledger.Rollup(context.Background(), ConsumptionScope{
		TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s1",
		Round: 1, RoundSet: true,
	})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if len(rollup.ByChild) != 0 {
		t.Fatalf("the round is the deepest chain level; entry ids are not a breakdown: %v", rollup.ByChild)
	}
	// The level still reports its own numbers — this is not an empty result.
	if rollup.TotalTokens != 3 || rollup.Entries != 1 {
		t.Fatalf("deepest level lost its own totals: %d tokens, %d entries", rollup.TotalTokens, rollup.Entries)
	}
}

func TestConsumptionLedgerRollupBreaksDownByAccountAndModel(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)

	rollup, err := ledger.Rollup(context.Background(), ConsumptionScope{TenantID: "t1", ProjectID: "p1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}

	wantAccounts := map[string]int64{"a1": 1043, "a2": 100}
	for account, tokens := range wantAccounts {
		if rollup.ByAccount[account] == nil || rollup.ByAccount[account].TotalTokens != tokens {
			t.Fatalf("account %q: got %+v, want %d tokens (full: %v)", account, rollup.ByAccount[account], tokens, rollup.ByAccount)
		}
	}
	if len(rollup.ByAccount) != len(wantAccounts) {
		t.Fatalf("account breakdown: got %v, want %v", rollup.ByAccount, wantAccounts)
	}

	// Engine and model are one key: two engines serving a model of the same
	// name are different products with different prices.
	wantModels := map[string]int64{"acp/opus": 1113, "codex/gpt": 30}
	for model, tokens := range wantModels {
		if rollup.ByModel[model] == nil || rollup.ByModel[model].TotalTokens != tokens {
			t.Fatalf("model %q: got %+v, want %d tokens (full: %v)", model, rollup.ByModel[model], tokens, rollup.ByModel)
		}
	}
}

func TestConsumptionLedgerRollupPartitionsOutcomes(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)

	rollup, err := ledger.Rollup(context.Background(), ConsumptionScope{TenantID: "t1", ProjectID: "p1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Successes != 4 || rollup.Failures != 1 {
		t.Fatalf("outcome partition: %d successes, %d failures; want 4 and 1", rollup.Successes, rollup.Failures)
	}
	if rollup.Successes+rollup.Failures != rollup.Entries {
		t.Fatalf("outcomes must partition the entries: %d + %d != %d",
			rollup.Successes, rollup.Failures, rollup.Entries)
	}
	// A failed turn still cost tokens, and they are in the total.
	if rollup.TotalTokens != 1143 {
		t.Fatalf("failed turns must still be charged: got %d, want 1143", rollup.TotalTokens)
	}
}

func TestConsumptionLedgerRollupBoundsTheEntriesSummed(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)

	rollup, err := ledger.Rollup(context.Background(), ConsumptionScope{TenantID: "t1", ProjectID: "p1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if !rollup.FirstAt.Equal(ledgerBase) {
		t.Fatalf("FirstAt: got %s, want %s", rollup.FirstAt, ledgerBase)
	}
	if !rollup.LastAt.Equal(ledgerBase.Add(4 * time.Minute)) {
		t.Fatalf("LastAt: got %s, want %s", rollup.LastAt, ledgerBase.Add(4*time.Minute))
	}
}

// TestConsumptionLedgerRollupTracksEachOutcomeSeparately covers the two
// timestamps the circuit breaker is derived from. Their ORDER is the whole
// point, so the fixture puts a failure after a success in one scope and the
// reverse in another — a fixture where every failure preceded every success
// would agree with an implementation that tracked only one timestamp.
func TestConsumptionLedgerRollupTracksEachOutcomeSeparately(t *testing.T) {
	ledger, _ := newLedgerFixture()
	ctx := context.Background()

	write := func(id, project string, outcome ConsumptionOutcome, at time.Time) {
		entry := validEntry()
		entry.EntryID = id
		entry.ProjectID = project
		entry.Outcome = outcome
		entry.OccurredAt = at
		if err := ledger.Record(ctx, entry); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	// Failing project: the latest event is a failure.
	write("p1-ok", "failing", ConsumptionOutcomeSuccess, ledgerBase)
	write("p1-bad", "failing", ConsumptionOutcomeFailure, ledgerBase.Add(time.Minute))
	// Recovered project: the same two outcomes, the other way round.
	write("p2-bad", "recovered", ConsumptionOutcomeFailure, ledgerBase)
	write("p2-ok", "recovered", ConsumptionOutcomeSuccess, ledgerBase.Add(time.Minute))

	for _, tc := range []struct {
		project        string
		failureIsLater bool
	}{
		{"failing", true},
		{"recovered", false},
	} {
		t.Run(tc.project, func(t *testing.T) {
			rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1", ProjectID: tc.project})
			if err != nil {
				t.Fatalf("Rollup: %v", err)
			}
			if rollup.LastSuccessAt.IsZero() || rollup.LastFailureAt.IsZero() {
				t.Fatalf("both outcomes occurred and both must be timed: %+v", rollup)
			}
			if got := rollup.LastFailureAt.After(rollup.LastSuccessAt); got != tc.failureIsLater {
				t.Fatalf("failure-after-success: got %v, want %v (failure %s, success %s)",
					got, tc.failureIsLater, rollup.LastFailureAt, rollup.LastSuccessAt)
			}
		})
	}

	// A scope with only successes has no failure time at all, rather than a
	// stale one borrowed from a sibling.
	onlyGood, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1", ProjectID: "clean"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if !onlyGood.LastFailureAt.IsZero() || !onlyGood.LastSuccessAt.IsZero() {
		t.Fatalf("an empty scope must report no outcome times: %+v", onlyGood)
	}
}

func TestConsumptionLedgerRollupEmptySubtreeIsNotAnError(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)

	// A scan that succeeded and found nothing is knowledge; only a scan that
	// FAILED must refuse. The two are told apart by the error, not the number.
	rollup, err := ledger.Rollup(context.Background(), ConsumptionScope{TenantID: "t1", ProjectID: "nope"})
	if err != nil {
		t.Fatalf("an empty subtree is a valid answer: %v", err)
	}
	if rollup.Entries != 0 || rollup.TotalTokens != 0 {
		t.Fatalf("empty subtree reported spend: %+v", rollup)
	}
	if !rollup.FirstAt.IsZero() || !rollup.LastAt.IsZero() {
		t.Fatalf("empty subtree must report no bounds: %s .. %s", rollup.FirstAt, rollup.LastAt)
	}
}

func TestConsumptionLedgerRollupScopeMustBeAContiguousPath(t *testing.T) {
	ledger, _ := newLedgerFixture()
	ctx := context.Background()

	refused := []struct {
		name  string
		scope ConsumptionScope
		field string
	}{
		{"task without project", ConsumptionScope{TenantID: "t1", TaskID: "w1"}, "task_id"},
		{"job without task", ConsumptionScope{TenantID: "t1", ProjectID: "p1", JobID: "pr1"}, "job_id"},
		{"session without job", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1", SessionID: "s1"}, "session_id"},
		{"round without session", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", RoundSet: true}, "round"},
		{"no tenant", ConsumptionScope{ProjectID: "p1"}, "tenant_id"},
		{"tenant with separator", ConsumptionScope{TenantID: "t/1"}, "tenant_id"},
		{"negative round", ConsumptionScope{TenantID: "t1", ProjectID: "p1", TaskID: "w1",
			JobID: "pr1", SessionID: "s1", Round: -1, RoundSet: true}, "round"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ledger.Rollup(ctx, tc.scope)
			var invalid *ConsumptionInvalidError
			if !stderrors.As(err, &invalid) {
				t.Fatalf("want ConsumptionInvalidError, got: %v", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("refusal must name the level: got %q, want %q", invalid.Field, tc.field)
			}
		})
	}

	// The permitting direction: every contiguous prefix is accepted, including
	// the one-level scope and the full-depth one. A contiguity rule mutated
	// into refusing everything fails here.
	accepted := []ConsumptionScope{
		{TenantID: "t1"},
		{TenantID: "t1", ProjectID: "p1"},
		{TenantID: "t1", ProjectID: "p1", TaskID: "w1"},
		{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1"},
		{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s1"},
		{TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "pr1", SessionID: "s1", Round: 0, RoundSet: true},
		// The sentinel is a legal SCOPE value — it addresses the entries with
		// no value at that level. It remains illegal as an ENTRY value, which
		// is what keeps it unambiguous here.
		{TenantID: "t1", ProjectID: consumptionUnattributed},
		{TenantID: "t1", ProjectID: "p1", TaskID: "w2", JobID: consumptionUnattributed},
	}
	for _, scope := range accepted {
		if _, err := ledger.Rollup(ctx, scope); err != nil {
			t.Fatalf("contiguous scope %+v must be accepted, got: %v", scope, err)
		}
	}
}

// =============================================================================
// Roll-ups — a failed read is never a zero
// =============================================================================

func TestConsumptionLedgerRollupRefusesUndecodableRecord(t *testing.T) {
	kv := NewMemoryTaskKV()
	ledger := NewConsumptionLedger(kv)
	seedRollupFixture(t, ledger)

	corrupt := "orch/costchain/t1/p1/w1/pr1/s1/000001/e1"
	if err := kv.Put(context.Background(), corrupt, []byte("{not json")); err != nil {
		t.Fatalf("corrupt fixture: %v", err)
	}

	rollup, err := ledger.Rollup(context.Background(), ConsumptionScope{TenantID: "t1"})
	if rollup != nil {
		t.Fatalf("a roll-up over an undecodable record must return no report, got: %+v", rollup)
	}
	var readErr *ConsumptionReadError
	if !stderrors.As(err, &readErr) {
		t.Fatalf("want ConsumptionReadError, got: %v", err)
	}
	if readErr.Key != corrupt {
		t.Fatalf("the refusal must name the record: got %q, want %q", readErr.Key, corrupt)
	}
}

func TestConsumptionLedgerRollupRefusesFailedScan(t *testing.T) {
	kv := newChainFaultKV()
	kv.scanErr = stderrors.New("store unavailable")
	ledger := NewConsumptionLedger(kv)

	rollup, err := ledger.Rollup(context.Background(), ConsumptionScope{TenantID: "t1"})
	if rollup != nil {
		t.Fatalf("a failed scan must return no report, got: %+v", rollup)
	}
	var readErr *ConsumptionReadError
	if !stderrors.As(err, &readErr) {
		t.Fatalf("want ConsumptionReadError, got: %v", err)
	}
	if !stderrors.Is(err, ErrConsumptionRead) {
		t.Fatalf("must unwrap to ErrConsumptionRead, got: %v", err)
	}
}

func TestConsumptionLedgerWindowUsageRefusesFailedScan(t *testing.T) {
	kv := newChainFaultKV()
	kv.scanErr = stderrors.New("store unavailable")
	ledger := NewConsumptionLedger(kv)

	window, err := ledger.WindowUsage(context.Background(), "t1", "a1",
		ledgerBase.Add(-time.Hour), ledgerBase)
	if window != nil {
		t.Fatalf("a failed window read must return no report, got: %+v", window)
	}
	var readErr *ConsumptionReadError
	if !stderrors.As(err, &readErr) {
		t.Fatalf("a limit decision must never read a failure as headroom; got: %v", err)
	}
}

// =============================================================================
// Window usage
// =============================================================================

// seedWindowFixture places entries on both sides of every boundary the window
// read has to respect: outside the scanned buckets, inside a scanned bucket but
// before the window, inside the window in the FIRST and LAST scanned buckets,
// exactly at each end, and beyond it.
func seedWindowFixture(t *testing.T, ledger ConsumptionLedger) (from, to time.Time) {
	t.Helper()
	ctx := context.Background()

	from = ledgerBase.Add(-2 * time.Hour) // 08:30
	to = ledgerBase                       // 10:30

	entries := []struct {
		id     string
		at     time.Time
		tokens int64
	}{
		{"before-buckets", from.Add(-time.Hour), 100000},                // 07:30 — bucket never scanned
		{"in-bucket-before-window", from.Add(-20 * time.Minute), 10000}, // 08:10 — bucket IS scanned, time is not in window
		{"at-from", from, 1},                             // 08:30 — half-open interval includes it
		{"first-bucket", from.Add(5 * time.Minute), 2},   // 08:35
		{"middle-bucket", from.Add(90 * time.Minute), 4}, // 10:00
		{"last-bucket", to.Add(-time.Minute), 8},         // 10:29
		{"at-to", to, 20000},                             // 10:30 — excluded, the interval is half-open
		{"after", to.Add(time.Minute), 40000},            // 10:31
	}
	for _, e := range entries {
		entry := validEntry()
		entry.EntryID = e.id
		entry.InputTokens = e.tokens
		entry.OutputTokens = 0
		entry.OccurredAt = e.at
		if err := ledger.Record(ctx, entry); err != nil {
			t.Fatalf("seed %s: %v", e.id, err)
		}
	}

	// A second account, same tenant and window, to prove the read is scoped.
	other := validEntry()
	other.EntryID = "other-account"
	other.AccountID = "a2"
	other.InputTokens = 70000
	other.OutputTokens = 0
	other.OccurredAt = from.Add(time.Minute)
	if err := ledger.Record(ctx, other); err != nil {
		t.Fatalf("seed other account: %v", err)
	}
	return from, to
}

func TestConsumptionLedgerWindowUsageCountsOnlyTheWindow(t *testing.T) {
	ledger, _ := newLedgerFixture()
	from, to := seedWindowFixture(t, ledger)

	window, err := ledger.WindowUsage(context.Background(), "t1", "a1", from, to)
	if err != nil {
		t.Fatalf("WindowUsage: %v", err)
	}

	// 1 + 2 + 4 + 8: each in-window entry contributes a distinct power of two,
	// so a missing or extra one names itself in the total rather than merely
	// making it wrong.
	if window.TotalTokens != 15 {
		t.Fatalf("window total %d, want 15 (at-from 1, first-bucket 2, middle 4, last 8)", window.TotalTokens)
	}
	if window.Entries != 4 {
		t.Fatalf("window entries: got %d, want 4", window.Entries)
	}
}

func TestConsumptionLedgerWindowUsageIsScopedToTheAccount(t *testing.T) {
	ledger, _ := newLedgerFixture()
	from, to := seedWindowFixture(t, ledger)

	other, err := ledger.WindowUsage(context.Background(), "t1", "a2", from, to)
	if err != nil {
		t.Fatalf("WindowUsage: %v", err)
	}
	if other.TotalTokens != 70000 {
		t.Fatalf("second account window: got %d, want 70000", other.TotalTokens)
	}
}

func TestConsumptionLedgerWindowUsageIsScopedToTheTenant(t *testing.T) {
	ledger, _ := newLedgerFixture()
	from, to := seedWindowFixture(t, ledger)

	// The same account id under a different tenant is a different allowance.
	foreign := validEntry()
	foreign.EntryID = "foreign"
	foreign.TenantID = "t2"
	foreign.InputTokens = 90000
	foreign.OutputTokens = 0
	foreign.OccurredAt = from.Add(time.Minute)
	if err := ledger.Record(context.Background(), foreign); err != nil {
		t.Fatalf("seed foreign tenant: %v", err)
	}

	window, err := ledger.WindowUsage(context.Background(), "t1", "a1", from, to)
	if err != nil {
		t.Fatalf("WindowUsage: %v", err)
	}
	if window.TotalTokens != 15 {
		t.Fatalf("another tenant's spend leaked into the window: got %d, want 15", window.TotalTokens)
	}
}

func TestConsumptionLedgerWindowUsageRefusesAnEmptyInterval(t *testing.T) {
	ledger, _ := newLedgerFixture()
	ctx := context.Background()

	refused := []struct {
		name     string
		from, to time.Time
	}{
		{"equal", ledgerBase, ledgerBase},
		{"inverted", ledgerBase, ledgerBase.Add(-time.Hour)},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			window, err := ledger.WindowUsage(ctx, "t1", "a1", tc.from, tc.to)
			if window != nil {
				t.Fatalf("an empty window must not answer with a report: %+v", window)
			}
			var invalid *ConsumptionInvalidError
			if !stderrors.As(err, &invalid) {
				t.Fatalf("want ConsumptionInvalidError, got: %v", err)
			}
			if invalid.Field != "window" {
				t.Fatalf("refusal must name the window, got %q", invalid.Field)
			}
		})
	}

	// The permitting direction: the narrowest non-empty interval is accepted.
	if _, err := ledger.WindowUsage(ctx, "t1", "a1", ledgerBase, ledgerBase.Add(time.Nanosecond)); err != nil {
		t.Fatalf("a one-nanosecond window is still a window, got: %v", err)
	}
}

// TestConsumptionLedgerWindowUsageScansOnlyOverlappingBuckets makes the reason
// the bucket segment exists observable. Correctness alone cannot see it — the
// time filter would discard a surplus bucket's entries anyway — so without
// counting the scans, a window read that walked the account's whole history
// would pass every other test in this file while costing what the bucket was
// introduced to avoid.
func TestConsumptionLedgerWindowUsageScansOnlyOverlappingBuckets(t *testing.T) {
	tests := []struct {
		name     string
		from, to time.Time
		want     int
	}{
		// 08:30 → 10:30 touches the 08, 09 and 10 buckets.
		{"mid-bucket ends", ledgerBase.Add(-2 * time.Hour), ledgerBase, 3},
		// Aligned on both ends: 08:00 → 10:00 is buckets 08 and 09. The 10
		// bucket cannot hold anything the half-open interval reaches.
		{"boundary aligned", ledgerBase.Truncate(time.Hour).Add(-2 * time.Hour), ledgerBase.Truncate(time.Hour), 2},
		{"inside one bucket", ledgerBase, ledgerBase.Add(time.Minute), 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kv := newScanCountingKV()
			ledger := NewConsumptionLedger(kv)
			if _, err := ledger.WindowUsage(context.Background(), "t1", "a1", tc.from, tc.to); err != nil {
				t.Fatalf("WindowUsage: %v", err)
			}
			if kv.scans != tc.want {
				t.Fatalf("scanned %d buckets, want %d (prefixes: %v)", kv.scans, tc.want, kv.prefixes)
			}
		})
	}
}

func TestConsumptionLedgerWindowUsageValidatesIdentifiers(t *testing.T) {
	ledger, _ := newLedgerFixture()
	ctx := context.Background()

	for _, tc := range []struct {
		name            string
		tenant, account string
		field           string
	}{
		{"no tenant", "", "a1", "tenant_id"},
		{"no account", "t1", "", "account_id"},
		{"tenant with separator", "t/1", "a1", "tenant_id"},
		{"account with separator", "t1", "a/1", "account_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ledger.WindowUsage(ctx, tc.tenant, tc.account, ledgerBase, ledgerBase.Add(time.Hour))
			var invalid *ConsumptionInvalidError
			if !stderrors.As(err, &invalid) {
				t.Fatalf("want ConsumptionInvalidError, got: %v", err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("refusal must name the field: got %q, want %q", invalid.Field, tc.field)
			}
		})
	}
}

// =============================================================================
// Encoding
// =============================================================================

func TestConsumptionSegmentsAreLexicallyOrdered(t *testing.T) {
	// Round 2 must sort before round 10, and an earlier instant before a later
	// one. Both are prefixes of live scans, so an unpadded encoding would put
	// round 10 between rounds 1 and 2 and silently reorder a drill-down.
	if !(roundSegment(2) < roundSegment(10)) {
		t.Fatalf("round segments out of order: %q !< %q", roundSegment(2), roundSegment(10))
	}
	if !(nanosSegment(ledgerBase) < nanosSegment(ledgerBase.Add(time.Nanosecond))) {
		t.Fatalf("timestamp segments out of order")
	}
	if got := roundSegment(0); got != "000000" {
		t.Fatalf("round zero segment: got %q, want %q", got, "000000")
	}
	if got := len(nanosSegment(ledgerBase)); got != consumptionNanosWidth {
		t.Fatalf("timestamp segment width: got %d, want %d", got, consumptionNanosWidth)
	}
}

func TestPadSegmentNeverTruncates(t *testing.T) {
	// A value already at or over the width is left alone. Truncating would map
	// two distinct values onto one segment, merging two records into one key.
	long := strings.Repeat("9", consumptionNanosWidth+3)
	if got := padSegment(long, consumptionNanosWidth); got != long {
		t.Fatalf("padSegment truncated: got %q, want %q", got, long)
	}
	if got := padSegment("7", 3); got != "007" {
		t.Fatalf("padSegment: got %q, want %q", got, "007")
	}
}

func TestConsumptionEntryModelKeySeparatesEngines(t *testing.T) {
	acp := ConsumptionEntry{Engine: "acp", Model: "opus"}
	codex := ConsumptionEntry{Engine: "codex", Model: "opus"}
	if acp.ModelKey() == codex.ModelKey() {
		t.Fatalf("two engines serving a model of the same name must not merge: %q", acp.ModelKey())
	}
}

// =============================================================================
// Fakes
// =============================================================================

// chainFaultKV fails the attribution-plane write while leaving the quota plane
// working, which is what a partial record looks like from inside Record. It
// also carries an optional scan failure.
type chainFaultKV struct {
	interfaces.KVStore

	failChain bool
	scanErr   error
}

func newChainFaultKV() *chainFaultKV {
	return &chainFaultKV{KVStore: NewMemoryTaskKV(), failChain: true}
}

func (k *chainFaultKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if k.failChain && strings.HasPrefix(key, consumptionChainKeyspace) {
		return false, stderrors.New("attribution plane unavailable")
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *chainFaultKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if k.scanErr != nil {
		return k.scanErr
	}
	return k.KVStore.Scan(ctx, prefix, fn)
}

// scanCountingKV records how many prefix scans a read performed, which is the
// only way the bucket layout's benefit is visible from outside.
type scanCountingKV struct {
	interfaces.KVStore

	scans    int
	prefixes []string
}

func newScanCountingKV() *scanCountingKV {
	return &scanCountingKV{KVStore: NewMemoryTaskKV()}
}

func (k *scanCountingKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *scanCountingKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	k.scans++
	k.prefixes = append(k.prefixes, prefix)
	return k.KVStore.Scan(ctx, prefix, fn)
}

// plainKV is a store with the six plain verbs and nothing else — no guarded
// create for the ledger to discover. Embedding by VALUE rather than promoting
// the inner store's capability is the point: it is exactly what a wrapper that
// forgot to forward PutIfAbsent looks like.
type plainKV struct {
	inner interfaces.KVStore
}

func newPlainKV() interfaces.KVStore { return &plainKV{inner: NewMemoryTaskKV()} }

func (k *plainKV) Put(ctx context.Context, key string, value []byte) error {
	return k.inner.Put(ctx, key, value)
}
func (k *plainKV) Get(ctx context.Context, key string) ([]byte, error) { return k.inner.Get(ctx, key) }
func (k *plainKV) Delete(ctx context.Context, key string) error        { return k.inner.Delete(ctx, key) }
func (k *plainKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	return k.inner.Scan(ctx, prefix, fn)
}
func (k *plainKV) List(ctx context.Context, prefix string) ([]string, error) {
	return k.inner.List(ctx, prefix)
}
func (k *plainKV) Exists(ctx context.Context, key string) (bool, error) {
	return k.inner.Exists(ctx, key)
}

// =============================================================================
// The project and tenant quota planes (go-quicbaas#1058, ruling F2)
// =============================================================================

func TestConsumptionLedgerRecordWritesTheProjectAndTenantPlanes(t *testing.T) {
	ledger, kv := newLedgerFixture()
	ctx := context.Background()
	if err := ledger.Record(ctx, validEntry()); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// Spelled out, not recomputed from the code under test.
	suffix := nanosSegment(ledgerBase.Truncate(time.Hour)) + "/" + nanosSegment(ledgerBase) + "/e1"
	for _, key := range []string{
		"orch/costproject/t1/p1/" + suffix,
		"orch/costtenant/t1/" + suffix,
	} {
		if _, err := kv.Get(ctx, key); err != nil {
			t.Fatalf("record missing at %s: %v", key, err)
		}
	}
}

func TestConsumptionLedgerKeepsUnattributedAndUnpooledEntriesOffTheirPlanes(t *testing.T) {
	ledger, kv := newLedgerFixture()
	ctx := context.Background()
	entry := validEntry()
	entry.ProjectID, entry.TaskID, entry.JobID, entry.SessionID = "", "", "", ""
	entry.AccountID = ""
	if err := ledger.Record(ctx, entry); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var keys []string
	if err := kv.Scan(ctx, "orch/", func(key string, _ []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, key := range keys {
		if strings.HasPrefix(key, "orch/costwindow/") || strings.HasPrefix(key, "orch/costproject/") {
			t.Fatalf("key %s written: an unpooled entry draws on no account and an unattributed one on no project", key)
		}
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %v, want the tenant plane and the chain", keys)
	}
}

func TestConsumptionLedgerProjectAndTenantWindows(t *testing.T) {
	ledger, _ := newLedgerFixture()
	seedRollupFixture(t, ledger)
	ctx := context.Background()
	from, to := ledgerBase.Add(-time.Hour), ledgerBase.Add(time.Hour)

	project, err := ledger.ProjectWindowUsage(ctx, "t1", "p1", from, to)
	if err != nil {
		t.Fatalf("ProjectWindowUsage: %v", err)
	}
	if project.TotalTokens != 1143 {
		t.Fatalf("project p1 = %d tokens, want 1143 (every p1 entry in the window)", project.TotalTokens)
	}
	tenant, err := ledger.TenantWindowUsage(ctx, "t1", from, to)
	if err != nil {
		t.Fatalf("TenantWindowUsage: %v", err)
	}
	if tenant.TotalTokens != 11143 {
		t.Fatalf("tenant t1 = %d tokens, want 11143 — and not t1x's", tenant.TotalTokens)
	}
	// The window is honoured: nothing before the first entry.
	early, err := ledger.TenantWindowUsage(ctx, "t1", ledgerBase.Add(-2*time.Hour), ledgerBase)
	if err != nil {
		t.Fatalf("TenantWindowUsage(early): %v", err)
	}
	if early.Entries != 0 {
		t.Fatalf("early window = %d entries, want 0", early.Entries)
	}
}

func TestConsumptionLedgerProjectAndTenantWindowsRefuseBadInput(t *testing.T) {
	ledger, _ := newLedgerFixture()
	ctx := context.Background()
	from, to := ledgerBase, ledgerBase.Add(time.Hour)
	tests := []struct {
		name  string
		read  func() error
		field string
	}{
		{"project window with no project", func() error {
			_, err := ledger.ProjectWindowUsage(ctx, "t1", "", from, to)
			return err
		}, "project_id"},
		{"project window naming the sentinel", func() error {
			_, err := ledger.ProjectWindowUsage(ctx, "t1", consumptionUnattributed, from, to)
			return err
		}, "project_id"},
		{"project window with no tenant", func() error {
			_, err := ledger.ProjectWindowUsage(ctx, "", "p1", from, to)
			return err
		}, "tenant_id"},
		{"tenant window with no tenant", func() error {
			_, err := ledger.TenantWindowUsage(ctx, "", from, to)
			return err
		}, "tenant_id"},
		{"an inverted window", func() error {
			_, err := ledger.TenantWindowUsage(ctx, "t1", to, from)
			return err
		}, "window"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var invalid *ConsumptionInvalidError
			if err := tc.read(); !stderrors.As(err, &invalid) || invalid.Field != tc.field {
				t.Fatalf("err = %v, want a ConsumptionInvalidError on %s", err, tc.field)
			}
		})
	}
}
