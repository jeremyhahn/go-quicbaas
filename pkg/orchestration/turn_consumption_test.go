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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// One turn, two records
// =============================================================================
//
// The counters report and the ledger decides, and both are written from the
// single turn-recording site. These assert that a turn lands in BOTH — a turn
// missing from the ledger is an account spending past a cap nobody is
// counting against — and that an unpooled turn lands only in the counters,
// which is the one case where the two legitimately differ.

// newConsumptionRecorder returns a service wired to both records, plus the
// ledger so a test can read back what the turn wrote.
func newConsumptionRecorder(t *testing.T) (*agentServiceImpl, ConsumptionLedger, *capturingLogger) {
	t.Helper()
	log := &capturingLogger{}
	kv := kvstore.NewMemory()
	svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{}, log)
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	impl.SetCostTracker(NewCostTracker(kv, log))
	ledger := NewConsumptionLedger(NewMemoryTaskKV())
	impl.SetConsumptionLedger(ledger)
	return impl, ledger, log
}

// pooledSession registers a live session on a pooled account, which is what
// makes a turn ledgerable.
func pooledSession(svc *agentServiceImpl, sessionID, tenantID, projectID, accountID string) {
	record := &sessionRecord{
		Backend:   "claude",
		ProjectID: projectID,
		TenantID:  tenantID,
		Model:     "claude-opus-5",
	}
	record.SetAccountID(accountID)
	svc.sessions.Store(sessionID, record)
}

func TestRecordTurnUsageWritesTheLedgerAndTheCounters(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	ctx = WithTurnAttribution(ctx, TurnAttribution{JobID: "job-1", TaskID: "task-1"})
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	svc.recordTurnUsage(ctx, completedTurn{
		turnID:    "turn-1",
		sessionID: "sess-1",
		projectID: "proj-1",
		engine:    "claude",
		model:     "claude-opus-5",
	}, &TokenUsage{InputTokens: 10, OutputTokens: 5, CacheReadInputTokens: 2})

	// The counters, unchanged behaviour.
	usage, err := svc.costTracker.GetAccountUsage(ctx, "t1", "a1")
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	if usage.TotalTokens != 17 {
		t.Errorf("counter total = %d, want 17", usage.TotalTokens)
	}

	// The ledger, keyed by the whole chain the turn ran under.
	rollup, err := ledger.Rollup(ctx, ConsumptionScope{
		TenantID: "t1", ProjectID: "proj-1", TaskID: "task-1", JobID: "job-1", SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 1 {
		t.Fatalf("ledger entries = %d, want 1 — a turn missing here is spend the cap cannot see", rollup.Entries)
	}
	if rollup.TotalTokens != 17 {
		t.Errorf("ledger total = %d, want 17; the two records must not contradict each other", rollup.TotalTokens)
	}
	if rollup.InputTokens != 10 || rollup.OutputTokens != 5 || rollup.CacheReadTokens != 2 {
		t.Errorf("input/output/cache-read = %d/%d/%d, want 10/5/2 — cache tokens price apart from a fresh prompt",
			rollup.InputTokens, rollup.OutputTokens, rollup.CacheReadTokens)
	}
	if rollup.Successes != 1 {
		t.Errorf("successes = %d, want 1", rollup.Successes)
	}
}

// TestRecordTurnUsageLedgersAProviderQualifiedModel is the regression for the
// go-quicbaas#1058 review: opencode reports its model as
// "ollama/review-q8:latest", and the ledger refused the separator, so every
// such turn reached the counters but never the ledger or the report.
func TestRecordTurnUsageLedgersAProviderQualifiedModel(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	ctx = WithTurnAttribution(ctx, TurnAttribution{JobID: "job-1", TaskID: "task-1"})
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	svc.recordTurnUsage(ctx, completedTurn{
		turnID:    "turn-1",
		sessionID: "sess-1",
		projectID: "proj-1",
		engine:    "opencode",
		model:     "ollama/review-q8:latest",
	}, &TokenUsage{InputTokens: 10, OutputTokens: 5})

	report, err := ReadSpendReport(ctx, SpendSources{Ledger: ledger, Accounts: nil}, &SpendReportRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("ReadSpendReport: %v", err)
	}
	if len(report.Models) != 1 {
		t.Fatalf("Models = %+v, want the opencode turn ledgered", report.Models)
	}
	row := report.Models[0]
	if row.Engine != "opencode" || row.Model != "ollama/review-q8:latest" || row.TotalTokens != 15 {
		t.Fatalf("model row = %+v, want opencode / ollama/review-q8:latest with 15 tokens", row)
	}
}

func TestRecordTurnUsageIsIdempotentOnTheTurnIdentity(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")
	turn := completedTurn{turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m"}
	usage := &TokenUsage{InputTokens: 10}

	svc.recordTurnUsage(ctx, turn, usage)
	svc.recordTurnUsage(ctx, turn, usage)

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	// The turn identity is minted when the turn is PLANNED, so a repeated
	// record of one turn converges rather than charging twice. Without that
	// the account's cap would be spent by bookkeeping.
	if rollup.Entries != 1 {
		t.Errorf("entries = %d, want 1 after recording one turn twice", rollup.Entries)
	}
}

// TestRecordTurnUsageLedgersAnUnpooledTurnOffTheQuotaPlane is the owner's
// 2026-09-23 requirement (go-quicbaas#1058): local inference draws on no pooled
// account and must still be tracked. It is attributed and reported, and it is
// never counted against an account's allowance.
func TestRecordTurnUsageLedgersAnUnpooledTurnOffTheQuotaPlane(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "")

	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "openvino", model: "qwen3-8b",
	}, &TokenUsage{InputTokens: 10})

	usage, err := svc.costTracker.GetProjectUsage(ctx, "t1", "proj-1")
	if err != nil {
		t.Fatalf("GetProjectUsage: %v", err)
	}
	if usage.TotalTokens != 10 {
		t.Errorf("counter total = %d, want 10", usage.TotalTokens)
	}

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 1 || rollup.TotalTokens != 10 {
		t.Fatalf("entries=%d tokens=%d, want the local turn attributed with its 10 tokens", rollup.Entries, rollup.TotalTokens)
	}
	if len(rollup.ByAccount) != 0 {
		t.Errorf("ByAccount = %v, want no account — a placeholder would make local inference a pooled spender", rollup.ByAccount)
	}
	if rollup.ByModel["openvino/qwen3-8b"] == nil {
		t.Errorf("ByModel = %v, want the local model broken out", rollup.ByModel)
	}
}

// TestRecordTurnUsageRecordsKindHostAgentAndDuration asserts every per-turn
// dimension the insights read reaches the entry: the runner that drove the
// job, the host the session ran on, the agent, and how long the turn took.
func TestRecordTurnUsageRecordsKindHostAgentAndDuration(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	ctx = WithTurnAttribution(ctx, TurnAttribution{JobID: "job-1", TaskID: "task-1", Kind: "review", AgentID: "agent-1"})
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")
	record, _ := svc.liveSessionRecord("sess-1")
	record.HostAffinity = "gpu-host"

	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "codex", model: "gpt-5",
		duration: 1500 * time.Millisecond,
	}, &TokenUsage{InputTokens: 10, OutputTokens: 1})

	entry := onlyLedgerEntry(t, ledger, "t1")
	if entry.Kind != "review" || entry.Host != "gpu-host" || entry.AgentID != "agent-1" || entry.DurationMillis != 1500 {
		t.Fatalf("entry = %+v, want kind=review host=gpu-host agent=agent-1 duration=1500ms", entry)
	}
}

// TestRecordTurnUsageAttributesALocalSessionToThisNode asserts a session with
// no named worker host is attributed to the node that ran it, never to an
// empty host.
func TestRecordTurnUsageAttributesALocalSessionToThisNode(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	svc.SetNode("node-a")
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, &TokenUsage{InputTokens: 1})

	if entry := onlyLedgerEntry(t, ledger, "t1"); entry.Host != "node-a" {
		t.Fatalf("Host = %q, want node-a", entry.Host)
	}
}

// onlyLedgerEntry reads back the single entry a test recorded for tenantID.
func onlyLedgerEntry(t *testing.T, ledger ConsumptionLedger, tenantID string) ConsumptionEntry {
	t.Helper()
	kv, ok := ledger.(*kvConsumptionLedger)
	if !ok {
		t.Fatalf("ledger is %T, want the KV ledger", ledger)
	}
	var entries []ConsumptionEntry
	if err := kv.scanEntries(context.Background(), consumptionChainKeyspace+tenantID+keySeparator,
		func(_ string, entry ConsumptionEntry) error {
			entries = append(entries, entry)
			return nil
		}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want exactly one", entries)
	}
	return entries[0]
}

func TestRecordTurnUsageWithoutALedgerStillCounts(t *testing.T) {
	// The deployment that wired counters and no ledger. It must keep
	// reporting rather than failing: the ledger is additive.
	log := &capturingLogger{}
	kv := kvstore.NewMemory()
	svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{}, log)
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetCostTracker(NewCostTracker(kv, log))
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(impl, "sess-1", "t1", "proj-1", "a1")

	impl.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, &TokenUsage{InputTokens: 4})

	usage, err := impl.costTracker.GetAccountUsage(ctx, "t1", "a1")
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	if usage.TotalTokens != 4 {
		t.Errorf("counter total = %d, want 4", usage.TotalTokens)
	}
}

func TestRecordFailedTurnIsWhatMakesABreakerPossible(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	// A provider that refuses instantly costs nothing and is exactly the
	// failure worth tripping on, so the entry is written with no tokens.
	svc.recordFailedTurn(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, nil)

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Failures != 1 {
		t.Fatalf("failures = %d, want 1 — a ledger of successes says every account is healthy", rollup.Failures)
	}
	if rollup.TotalTokens != 0 {
		t.Errorf("total tokens = %d, want 0 for a failure the backend reported no usage for", rollup.TotalTokens)
	}
	if rollup.LastFailureAt.IsZero() {
		t.Error("LastFailureAt is zero; the breaker's cooldown is measured from it")
	}
}

func TestRecordFailedTurnWithoutAnAccountIsAttributedButTripsNoAccount(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "")

	svc.recordFailedTurn(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "openvino", model: "m",
	}, &TokenUsage{InputTokens: 3})

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Failures != 1 || rollup.TotalTokens != 3 {
		t.Errorf("failures=%d tokens=%d, want the failed local turn recorded with its 3 tokens", rollup.Failures, rollup.TotalTokens)
	}
	if len(rollup.ByAccount) != 0 {
		t.Errorf("ByAccount = %v, want none; a failure on no pooled account trips no account's breaker", rollup.ByAccount)
	}
}

func TestRecordConsumptionReportsAGapInTheAuthoritysRecord(t *testing.T) {
	log := &capturingLogger{}
	kv := kvstore.NewMemory()
	svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{}, log)
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetCostTracker(NewCostTracker(kv, log))
	// A ledger over a store that cannot decide a contested create refuses
	// every write. The turn must still succeed — it has already produced its
	// answer — and the gap must be audible.
	impl.SetConsumptionLedger(NewConsumptionLedger(newPlainKV()))
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(impl, "sess-1", "t1", "proj-1", "a1")

	impl.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, &TokenUsage{InputTokens: 10})

	found := false
	for _, w := range log.warnings() {
		if strings.Contains(w, "under-counting") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning about the missed ledger write; warnings = %v", log.warnings())
	}
	// The counters still recorded it, so the turn is not lost entirely.
	usage, usageErr := impl.costTracker.GetAccountUsage(ctx, "t1", "a1")
	if usageErr != nil {
		t.Fatalf("GetAccountUsage: %v", usageErr)
	}
	if usage.TotalTokens != 10 {
		t.Errorf("counter total = %d, want 10", usage.TotalTokens)
	}
}

func TestFailedTurnUsageReadsTheBackendsOwnAccount(t *testing.T) {
	reported := &TokenUsage{InputTokens: 7}
	err := &OneShotExecutionError{TurnFailureEvidence: TurnFailureEvidence{Usage: reported}}
	if got := failedTurnUsage(err); got != reported {
		t.Errorf("failedTurnUsage = %+v, want the evidence's own usage", got)
	}
}

func TestFailedTurnUsageReportsNothingWhenNothingWasReported(t *testing.T) {
	// Nil is not zero: a backend that reported no accounting must not be
	// recorded as having reported zero.
	if got := failedTurnUsage(context.Canceled); got != nil {
		t.Errorf("failedTurnUsage on an error carrying no evidence = %+v, want nil", got)
	}
}

// TestRecordTurnUsageKeepsEveryDimensionApart: the cache dimensions used to
// be folded into InputTokens, which left a cached turn impossible to price.
// Each dimension has its own field and the total is unchanged, so no quota
// window moves. go-quicbaas#1058.
func TestRecordTurnUsageKeepsEveryDimensionApart(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	usage := &TokenUsage{InputTokens: 10, OutputTokens: 5, CacheCreationInputTokens: 3, CacheReadInputTokens: 2}
	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, usage)

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.InputTokens != 10 || rollup.OutputTokens != 5 || rollup.CacheWriteTokens != 3 || rollup.CacheReadTokens != 2 {
		t.Fatalf("rollup = in %d out %d cache-write %d cache-read %d, want 10/5/3/2",
			rollup.InputTokens, rollup.OutputTokens, rollup.CacheWriteTokens, rollup.CacheReadTokens)
	}
	if rollup.TotalTokens != usage.Total() {
		t.Fatalf("ledger total = %d, want Total() = %d", rollup.TotalTokens, usage.Total())
	}
}

// TestRecordFailedTurnWithNoAccountingRecordsNoTokens asserts a nil usage
// still records the occurrence, with zero on every dimension, marked
// unmeasured: nobody knows what the failed turn cost, so it counts against
// money coverage rather than reading as free (go-quicbaas#1058 review).
func TestRecordFailedTurnWithNoAccountingRecordsNoTokens(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	svc.recordFailedTurn(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, nil)

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 1 || rollup.TotalTokens != 0 || rollup.UnpricedEntries != 1 || rollup.UnbilledEntries != 1 {
		t.Fatalf("rollup = %+v, want one entry with no tokens, counted unpriced and unbilled", rollup.ConsumptionTotals)
	}
}

// TestRecordFailedTurnKeepsAMeasuredZeroApartFromNoAccounting is the
// go-quicbaas#1058 review regression, through the recorder: a failure whose
// backend REPORTED zero tokens is a measurement — no request reached the
// model — while one that reported nothing is unmeasured. Only the second may
// count against the report's coverage.
func TestRecordFailedTurnKeepsAMeasuredZeroApartFromNoAccounting(t *testing.T) {
	tests := []struct {
		name         string
		usage        *TokenUsage
		wantUnpriced int
	}{
		{"no accounting is missing coverage", nil, 1},
		{"a reported zero is a measurement", &TokenUsage{}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, ledger, _ := newConsumptionRecorder(t)
			ctx := ContextWithTenant(context.Background(), "t1")
			pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

			svc.recordFailedTurn(ctx, completedTurn{
				turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
			}, tc.usage)

			rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
			if err != nil {
				t.Fatalf("Rollup: %v", err)
			}
			if rollup.Failures != 1 || rollup.UnpricedEntries != tc.wantUnpriced || rollup.UnbilledEntries != tc.wantUnpriced {
				t.Fatalf("rollup = %+v, want one failure with %d unpriced and unbilled", rollup.ConsumptionTotals, tc.wantUnpriced)
			}
		})
	}
}

// TestRecordTurnUsage_JobWithoutAnAgentLeavesTheCountersAlone is the other
// half of the relaxed attribution contract: the ledger takes a job on its
// own, and the counters still do not.
func TestRecordTurnUsage_JobWithoutAnAgentLeavesTheCountersAlone(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	ctx = WithTurnAttribution(ctx, TurnAttribution{JobID: "job-1", TaskID: "task-1"})
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, &TokenUsage{InputTokens: 10})

	// A job total with no agent breakdown under it is a total nothing can
	// explain, so the counters record none.
	usage, err := svc.costTracker.GetJobUsage(ctx, "t1", "job-1")
	if err == nil && usage != nil && usage.TotalTokens != 0 {
		t.Errorf("job counter = %d, want 0 for a turn with no agent identity", usage.TotalTokens)
	}
	// The ledger, which has no agent level, records it under the job.
	rollup, rollupErr := ledger.Rollup(ctx, ConsumptionScope{
		TenantID: "t1", ProjectID: "proj-1", TaskID: "task-1", JobID: "job-1",
	})
	if rollupErr != nil {
		t.Fatalf("Rollup: %v", rollupErr)
	}
	if rollup.TotalTokens != 10 {
		t.Errorf("ledger total = %d, want 10 under the job", rollup.TotalTokens)
	}
}

func TestJobRunPromptAttributesTurnsToItsJob(t *testing.T) {
	captured := make(chan TurnAttribution, 1)
	run := &JobRun{
		JobID:  "job-1",
		Runner: "sdlc",
		Config: &JobConfig{TaskID: "task-1"},
		Agents: &attributionCapturingAgents{seen: captured},
	}
	if _, err := run.Prompt(context.Background(), &PromptSessionRequest{SessionID: "s1", Prompt: "hi"}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	select {
	case got := <-captured:
		if got.JobID != "job-1" || got.TaskID != "task-1" || got.Kind != "sdlc" {
			t.Errorf("attribution = %+v, want job-1/task-1/sdlc attached by the envelope", got)
		}
	case <-time.After(time.Second):
		t.Fatal("the agent service was never called")
	}
}

func TestJobRunPromptKeepsARicherAttribution(t *testing.T) {
	captured := make(chan TurnAttribution, 1)
	run := &JobRun{
		JobID:  "job-1",
		Config: &JobConfig{TaskID: "task-1"},
		Agents: &attributionCapturingAgents{seen: captured},
	}
	// The control loop knows the AGENT as well. Overwriting its attribution
	// here would replace a richer record with a poorer one.
	ctx := WithTurnAttribution(context.Background(), TurnAttribution{JobID: "job-1", TaskID: "task-1", AgentID: "agent-worker"})
	if _, err := run.Prompt(ctx, &PromptSessionRequest{SessionID: "s1", Prompt: "hi"}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	select {
	case got := <-captured:
		if got.AgentID != "agent-worker" {
			t.Errorf("attribution = %+v, want the caller's own agent preserved", got)
		}
	case <-time.After(time.Second):
		t.Fatal("the agent service was never called")
	}
}

// attributionCapturingAgents records the turn attribution its caller
// attached, which is the only way to observe an addition made to a context.
type attributionCapturingAgents struct {
	AgentService
	seen chan TurnAttribution
}

func (a *attributionCapturingAgents) PromptSession(ctx context.Context, _ *PromptSessionRequest) (*PromptSessionResponse, error) {
	a.seen <- TurnAttributionFromContext(ctx)
	return &PromptSessionResponse{}, nil
}

// TestRecordTurnUsageLedgersAnUnmeasuredSuccess is the success side of the
// occurrence rule, and it exists because its absence was a live defect: the
// empty-usage early return skipped the ledger as well as the counters, so a
// backend that publishes no token counts recorded failures and never
// successes.
func TestRecordTurnUsageLedgersAnUnmeasuredSuccess(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	// nil usage is what the ACP JSON-RPC dialect and `codex exec` report.
	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, nil)

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Successes != 1 {
		t.Fatalf("successes = %d, want 1 — an unmeasured success is still a success, and it is "+
			"what tells the breaker the account is working", rollup.Successes)
	}
	if rollup.TotalTokens != 0 {
		t.Errorf("tokens = %d, want 0 — the occurrence is recorded, a figure nobody measured is not", rollup.TotalTokens)
	}
	if rollup.UnpricedEntries != 1 || rollup.PricedEntries != 0 {
		t.Errorf("priced/unpriced = %d/%d, want 0/1 — an unmeasured turn is missing coverage, not a free one",
			rollup.PricedEntries, rollup.UnpricedEntries)
	}

	// The counters must NOT see it: they are a quantity, and
	// LeastUsageAccountStrategy balances a pool on them, so a zero there
	// makes this account look perpetually least-used.
	usage, err := svc.costTracker.GetAccountUsage(ctx, "t1", "a1")
	if err == nil && usage != nil && usage.TotalTokens != 0 {
		t.Errorf("counter total = %d, want 0 — an unmeasured turn must not be counted as zero", usage.TotalTokens)
	}
}

// TestUnmeasuredSuccessUntripsTheBreaker is the property the fix above exists
// for, asserted end to end rather than on the recording call alone.
//
// The breaker is tripped by failures and released by a success recorded AFTER
// the latest one. On a backend that publishes no token counts every success
// is unmeasured, so dropping those from the ledger left an account that could
// trip and could never be talked out of it by the very turn that proved it
// had recovered — it would stay parked until the failures aged out.
func TestUnmeasuredSuccessUntripsTheBreaker(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")

	policy := BreakerPolicy{Window: 24 * time.Hour, FailureThreshold: 2, Cooldown: time.Minute}
	breaker := NewCircuitBreaker(ledger, NewMemoryTaskKV())

	turn := func(id string) completedTurn {
		return completedTurn{turnID: id, sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m"}
	}
	svc.recordFailedTurn(ctx, turn("f1"), nil)
	svc.recordFailedTurn(ctx, turn("f2"), nil)

	verdict, err := breaker.Evaluate(ctx, "t1", "a1", policy, time.Now().UTC())
	if err != nil {
		t.Fatalf("Evaluate after failures: %v", err)
	}
	if verdict.Admits() {
		t.Fatalf("the breaker admits after %d failures; it should be tripped", verdict.Failures)
	}

	// The recovery turn, reporting nothing — the case that used to vanish.
	svc.recordTurnUsage(ctx, turn("s1"), nil)

	verdict, err = breaker.Evaluate(ctx, "t1", "a1", policy, time.Now().UTC())
	if err != nil {
		t.Fatalf("Evaluate after recovery: %v", err)
	}
	if !verdict.Admits() {
		t.Fatalf("the breaker is still %s after an unmeasured success; a recovered account stays "+
			"parked for the whole failure window", verdict.State)
	}
}

// TestControlLoopTurnsCarryTheirJobKind is the go-quicbaas#1058 review
// finding: control-loop agents prompt the agent service directly with the
// loop's own attribution, so the kind must travel on it — with the agent's
// identity preserved — or those turns land in the ledger with no kind.
func TestControlLoopTurnsCarryTheirJobKind(t *testing.T) {
	cl := NewControlLoop(nil, &capturingLogger{})
	cl.job.JobID = "job-1"
	cl.agentIdentities["reviewer"] = &AgentIdentity{AgentID: "agent-rev"}

	if got := cl.turnAttribution("reviewer"); got.Kind != JobRunnerWorkflow || got.AgentID != "agent-rev" {
		t.Fatalf("attribution = %+v, want kind workflow and the minted agent", got)
	}
	cl.job.Runner = "sdlc"
	attribution := cl.turnAttribution("worker")
	if attribution.Kind != "sdlc" || attribution.AgentID != "agent-worker-job-1" {
		t.Fatalf("attribution = %+v, want kind sdlc and the role-derived agent", attribution)
	}

	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := WithTurnAttribution(ContextWithTenant(context.Background(), "t1"), attribution)
	pooledSession(svc, "sess-1", "t1", "proj-1", "a1")
	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, &TokenUsage{InputTokens: 3})
	if entry := onlyLedgerEntry(t, ledger, "t1"); entry.Kind != "sdlc" || entry.AgentID != "agent-worker-job-1" {
		t.Fatalf("entry = %+v, want kind sdlc and the agent", entry)
	}
}
