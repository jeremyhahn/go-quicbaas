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
		t.Errorf("input/output/cache-read = %d/%d/%d, want 10/5/2 — a cache read prices apart from a fresh prompt token",
			rollup.InputTokens, rollup.OutputTokens, rollup.CacheReadTokens)
	}
	if rollup.Successes != 1 {
		t.Errorf("successes = %d, want 1", rollup.Successes)
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

func TestRecordTurnUsageLeavesAnUnpooledTurnOutOfTheLedger(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "")

	svc.recordTurnUsage(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, &TokenUsage{InputTokens: 10})

	// The counters still see it — spend is spend.
	usage, err := svc.costTracker.GetProjectUsage(ctx, "t1", "proj-1")
	if err != nil {
		t.Fatalf("GetProjectUsage: %v", err)
	}
	if usage.TotalTokens != 10 {
		t.Errorf("counter total = %d, want 10; an unpooled turn is still reported", usage.TotalTokens)
	}

	// The ledger does not: its quota plane is keyed by account, and filing a
	// turn that drew on no allowance under a placeholder would make a
	// fictional account the tenant's largest spender.
	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 0 {
		t.Errorf("ledger entries = %d, want 0 for a turn that drew on no pooled account", rollup.Entries)
	}
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

func TestRecordFailedTurnWithoutAnAccountRecordsNothing(t *testing.T) {
	svc, ledger, _ := newConsumptionRecorder(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	pooledSession(svc, "sess-1", "t1", "proj-1", "")

	svc.recordFailedTurn(ctx, completedTurn{
		turnID: "turn-1", sessionID: "sess-1", projectID: "proj-1", engine: "claude", model: "m",
	}, &TokenUsage{InputTokens: 3})

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 0 {
		t.Errorf("entries = %d, want 0; a failure on no pooled account trips no account's breaker", rollup.Entries)
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

// TestRecordTurnUsageKeepsEveryDimensionApart is the go-quicbaas#1058
// regression: the cache dimensions used to be folded into InputTokens, which
// left a cached turn impossible to price. Each dimension now has its own field
// and the total is unchanged, so no quota window moves.
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
		t.Fatalf("ledger total = %d, want Total() = %d — the cap and the dashboard must agree", rollup.TotalTokens, usage.Total())
	}
}

// TestRecordFailedTurnWithNoAccountingRecordsNoTokens asserts a nil usage — a
// backend that reported nothing — still records the occurrence, with zero on
// every dimension rather than a panic or an invented figure.
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
	if rollup.Entries != 1 || rollup.TotalTokens != 0 || rollup.CacheReadTokens != 0 || rollup.CacheWriteTokens != 0 {
		t.Fatalf("rollup = %+v, want one entry with no tokens", rollup)
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
		Config: &JobConfig{TaskID: "task-1"},
		Agents: &attributionCapturingAgents{seen: captured},
	}
	if _, err := run.Prompt(context.Background(), &PromptSessionRequest{SessionID: "s1", Prompt: "hi"}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	select {
	case got := <-captured:
		if got.JobID != "job-1" || got.TaskID != "task-1" {
			t.Errorf("attribution = %+v, want job-1/task-1 attached by the envelope", got)
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
