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

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Harness
// =============================================================================

// usageReportingBackend is an AgentBackend whose turns report a fixed token
// count and whose spawns echo back a pooled account, so a test can follow one
// turn's usage from the backend all the way into the ledger.
type usageReportingBackend struct {
	// usage is what every turn reports. Nil models a CLI that publishes no
	// token counts.
	usage *TokenUsage

	// account is the pooled account every spawn resolves to.
	account string

	// strategy names the policy that chose account.
	strategy string

	// beforePrompt, when set, runs at the start of every turn, so a test can
	// hold a turn open.
	beforePrompt func()
}

func (b *usageReportingBackend) Spawn(_ context.Context, req *SpawnRequest) (*Session, error) {
	account := b.account
	strategy := b.strategy
	if req.AccountID != "" {
		account = req.AccountID
		strategy = "pinned"
	}
	return &Session{
		ID:              "sess" + strings.ReplaceAll(req.WorkDir, "/", "-"),
		ProjectID:       req.ProjectID,
		Backend:         "usage",
		Status:          SessionStateActive,
		WorkDir:         req.WorkDir,
		CreatedAt:       time.Now().UnixNano(),
		AccountID:       account,
		AccountStrategy: strategy,
	}, nil
}

func (b *usageReportingBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	if b.beforePrompt != nil {
		b.beforePrompt()
	}
	return &PromptResponse{
		SessionID:  req.SessionID,
		Content:    []ContentBlock{{Type: "text", Text: "ok"}},
		StopReason: StopReasonEndTurn,
		Usage:      b.usage,
	}, nil
}

func (b *usageReportingBackend) Status(_ context.Context, sessionID string) (*SessionStatus, error) {
	return &SessionStatus{SessionID: sessionID, State: SessionStateActive}, nil
}

func (b *usageReportingBackend) Close(_ context.Context, _ string) error { return nil }

func (b *usageReportingBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{Kind: BackendKindAgent, PersistentSessions: true}
}

func (b *usageReportingBackend) Type() string         { return "usage" }
func (b *usageReportingBackend) ToolGuidance() string { return "" }

var _ AgentBackend = (*usageReportingBackend)(nil)

// usageHarness is a service wired to a usage-reporting backend and a real
// KV-backed cost ledger.
type usageHarness struct {
	service *agentServiceImpl
	backend *usageReportingBackend
	tracker CostTracker
	kv      interfaces.KVStore
}

func newUsageHarness(t *testing.T, backend *usageReportingBackend, withTracker bool) *usageHarness {
	t.Helper()

	kv := kvstore.NewMemory()
	svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{"usage": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}

	harness := &usageHarness{service: impl, backend: backend, kv: kv}
	if withTracker {
		harness.tracker = NewCostTracker(kv, &agentTestLogger{})
		impl.SetCostTracker(harness.tracker)
	}
	return harness
}

// spawnAndPrompt runs one full spawn + turn for tenant and returns the session ID.
func (h *usageHarness) spawnAndPrompt(t *testing.T, ctx context.Context, req *SpawnSessionRequest) string {
	t.Helper()

	spawn, err := h.service.SpawnSession(ctx, req)
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if _, err := h.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawn.SessionID,
		ProjectID: req.ProjectID,
		Prompt:    "do the thing",
	}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	return spawn.SessionID
}

// =============================================================================
// Attribution context
// =============================================================================

func TestWithTurnAttribution_RoundTrips(t *testing.T) {
	ctx := WithTurnAttribution(context.Background(), TurnAttribution{JobID: "job-1", AgentID: "agent-1"})

	got := TurnAttributionFromContext(ctx)
	if got.JobID != "job-1" || got.AgentID != "agent-1" {
		t.Fatalf("TurnAttributionFromContext() = %+v, want job-1/agent-1", got)
	}
}

// TestWithTurnAttribution_RequiresAJob asserts what the context will and will
// not carry.
//
// THE AGENT USED TO BE REQUIRED AND IS NOT ANY MORE, because two consumers
// want different things from one attribution. The advisory counters' job
// dimension is per-agent-within-job and still refuses a job with no agent —
// asserted below in TestRecordTurnUsage_JobWithoutAnAgentLeavesTheCountersAlone
// — while the CONSUMPTION LEDGER's chain has no agent level at all and a
// runner that prompts one session directly has a job and no agent. Dropping
// those here took that whole population out of the spend authority's record.
func TestWithTurnAttribution_RequiresAJob(t *testing.T) {
	t.Run("a job without an agent is carried", func(t *testing.T) {
		ctx := WithTurnAttribution(context.Background(), TurnAttribution{JobID: "job-1", TaskID: "task-1"})
		got := TurnAttributionFromContext(ctx)
		if got.JobID != "job-1" || got.TaskID != "task-1" {
			t.Fatalf("TurnAttributionFromContext() = %+v, want job-1/task-1", got)
		}
	})
	for name, attribution := range map[string]TurnAttribution{
		"no job":  {AgentID: "agent-1"},
		"neither": {},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := WithTurnAttribution(context.Background(), attribution)
			if got := TurnAttributionFromContext(ctx); got != (TurnAttribution{}) {
				t.Fatalf("TurnAttributionFromContext() = %+v, want the zero value; there is nothing to attribute to", got)
			}
		})
	}
}

func TestTurnAttributionFromContext_UnsetAndNil(t *testing.T) {
	if got := TurnAttributionFromContext(context.Background()); got != (TurnAttribution{}) {
		t.Fatalf("TurnAttributionFromContext(background) = %+v, want the zero value", got)
	}
	var nilCtx context.Context
	if got := TurnAttributionFromContext(nilCtx); got != (TurnAttribution{}) {
		t.Fatalf("TurnAttributionFromContext(nil) = %+v, want the zero value", got)
	}
}

// =============================================================================
// Recording a completed turn
// =============================================================================

// TestPromptSession_RecordsTurnUsage is the regression this whole feature turns
// on: before it, RecordTurnUsage had zero callers and the ledger was empty no
// matter how many turns ran.
func TestPromptSession_RecordsTurnUsage(t *testing.T) {
	harness := newUsageHarness(t, &usageReportingBackend{
		usage:    &TokenUsage{InputTokens: 100, OutputTokens: 40, CacheReadInputTokens: 10},
		account:  "acct-1",
		strategy: LeastUsageAccountStrategyName,
	}, true)

	ctx := ContextWithTenant(context.Background(), "tenant-a")
	harness.spawnAndPrompt(t, ctx, &SpawnSessionRequest{
		Backend:     "usage",
		WorkDir:     "/work",
		ProjectID:   "proj-1",
		Persistent:  true,
		AccountAuto: true,
	})

	accountUsage, err := harness.tracker.GetAccountUsage(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	if accountUsage.TotalTokens != 150 {
		t.Fatalf("account usage = %d, want 150 — the turn's tokens must be charged to the account that served it", accountUsage.TotalTokens)
	}

	projectUsage, err := harness.tracker.GetProjectUsage(ctx, "tenant-a", "proj-1")
	if err != nil {
		t.Fatalf("GetProjectUsage: %v", err)
	}
	if projectUsage.TotalTokens != 150 {
		t.Fatalf("project usage = %d, want 150", projectUsage.TotalTokens)
	}
}

// TestPromptSession_RecordsJobAttributionWhenPresent covers the control loop's
// path, where the job and agent are known and the turn must also land on the
// job budget CheckBudget reads.
func TestPromptSession_RecordsJobAttributionWhenPresent(t *testing.T) {
	harness := newUsageHarness(t, &usageReportingBackend{
		usage:   &TokenUsage{InputTokens: 60, OutputTokens: 40},
		account: "acct-1",
	}, true)

	ctx := WithTurnAttribution(
		ContextWithTenant(context.Background(), "tenant-a"),
		TurnAttribution{JobID: "job-7", AgentID: "agent-coder-job-7"},
	)
	harness.spawnAndPrompt(t, ctx, &SpawnSessionRequest{
		Backend: "usage", WorkDir: "/work", Persistent: true, AccountAuto: true,
	})

	jobUsage, err := harness.tracker.GetJobUsage(ctx, "tenant-a", "job-7")
	if err != nil {
		t.Fatalf("GetJobUsage: %v", err)
	}
	if jobUsage.TotalTokens != 100 {
		t.Fatalf("job usage = %d, want 100", jobUsage.TotalTokens)
	}
	if got := jobUsage.ByAgent["agent-coder-job-7"]; got != 100 {
		t.Fatalf("per-agent usage = %d, want 100 — a job total with no agent breakdown cannot be explained", got)
	}
}

// TestPromptSession_UnmeasuredTurnRecordsNothing is the honesty case. A backend
// that publishes no token counts must leave the ledger untouched rather than
// contribute an estimate.
func TestPromptSession_UnmeasuredTurnRecordsNothing(t *testing.T) {
	harness := newUsageHarness(t, &usageReportingBackend{usage: nil, account: "acct-1"}, true)

	ctx := ContextWithTenant(context.Background(), "tenant-a")
	harness.spawnAndPrompt(t, ctx, &SpawnSessionRequest{
		Backend: "usage", WorkDir: "/work", Persistent: true, AccountAuto: true,
	})

	usage, err := harness.tracker.GetAccountUsage(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	if usage.TotalTokens != 0 {
		t.Fatalf("account usage = %d, want 0 — a turn with no reported counts must not be estimated into the ledger", usage.TotalTokens)
	}
}

// TestPromptSession_WithoutCostTrackerStillSucceeds asserts the turn path is
// unaffected when no ledger is wired (the storage-less dev boot).
func TestPromptSession_WithoutCostTrackerStillSucceeds(t *testing.T) {
	harness := newUsageHarness(t, &usageReportingBackend{
		usage:   &TokenUsage{InputTokens: 10, OutputTokens: 10},
		account: "acct-1",
	}, false)

	ctx := ContextWithTenant(context.Background(), "tenant-a")
	harness.spawnAndPrompt(t, ctx, &SpawnSessionRequest{
		Backend: "usage", WorkDir: "/work", Persistent: true, AccountAuto: true,
	})
}

// TestRecordTurnUsage_LedgerFailureDoesNotFailTheTurn asserts the ledger is
// best-effort: a turn that already produced its answer must not be reported as
// failed because the usage write did not land.
func TestRecordTurnUsage_LedgerFailureDoesNotFailTheTurn(t *testing.T) {
	harness := newUsageHarness(t, &usageReportingBackend{
		usage:   &TokenUsage{InputTokens: 10, OutputTokens: 10},
		account: "acct-1",
	}, false)
	harness.service.SetCostTracker(&failingCostTracker{})

	ctx := ContextWithTenant(context.Background(), "tenant-a")
	spawn, err := harness.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend: "usage", WorkDir: "/work", Persistent: true, AccountAuto: true,
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	resp, err := harness.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawn.SessionID,
		Prompt:    "do the thing",
	})
	if err != nil {
		t.Fatalf("PromptSession returned %v; a ledger write failure must not fail a completed turn", err)
	}
	if resp.Usage == nil || resp.Usage.Total() != 20 {
		t.Fatalf("PromptSessionResponse.Usage = %+v, want the turn's reported counts surfaced to the caller", resp.Usage)
	}
}

// failingCostTracker fails every write, so the best-effort contract can be
// asserted without corrupting a real ledger.
type failingCostTracker struct{}

func (failingCostTracker) RecordTokenUsage(context.Context, string, string, string, int64) error {
	return &UsageRecordError{TenantID: "tenant-a", Cause: &CostInvalidInputError{Field: "kv", Message: "unavailable"}}
}

func (failingCostTracker) GetJobUsage(context.Context, string, string) (*UsageReport, error) {
	return nil, &UsageRetrievalError{TenantID: "tenant-a"}
}

func (failingCostTracker) GetAgentUsage(context.Context, string, string, string) (*UsageReport, error) {
	return nil, &UsageRetrievalError{TenantID: "tenant-a"}
}

func (failingCostTracker) CheckBudget(context.Context, string, string) (BudgetStatus, error) {
	return "", &BudgetNotSetError{TenantID: "tenant-a"}
}

func (failingCostTracker) RecordTurnUsage(context.Context, TurnUsage) error {
	return &UsageRecordError{TenantID: "tenant-a", Cause: &CostInvalidInputError{Field: "kv", Message: "unavailable"}}
}

func (failingCostTracker) GetAccountUsage(context.Context, string, string) (*UsageReport, error) {
	return nil, &UsageRetrievalError{TenantID: "tenant-a"}
}

func (failingCostTracker) GetProjectUsage(context.Context, string, string) (*UsageReport, error) {
	return nil, &UsageRetrievalError{TenantID: "tenant-a"}
}

func (failingCostTracker) AccountTokens(context.Context, string, string) (int64, error) {
	return 0, &UsageRetrievalError{TenantID: "tenant-a"}
}

var _ CostTracker = failingCostTracker{}

// =============================================================================
// Tenant isolation
// =============================================================================

// TestRecordTurnUsage_TenantIsolation asserts one tenant's turns never appear
// in another tenant's ledger, even when both name the same account and project
// identifiers and share one physical store.
func TestRecordTurnUsage_TenantIsolation(t *testing.T) {
	harness := newUsageHarness(t, &usageReportingBackend{
		usage:   &TokenUsage{InputTokens: 400, OutputTokens: 100},
		account: "acct-shared-id",
	}, true)

	tenantA := ContextWithTenant(context.Background(), "tenant-a")
	harness.spawnAndPrompt(t, tenantA, &SpawnSessionRequest{
		Backend: "usage", WorkDir: "/a", ProjectID: "proj-shared-id", Persistent: true, AccountAuto: true,
	})

	tenantB := ContextWithTenant(context.Background(), "tenant-b")
	accountUsage, err := harness.tracker.GetAccountUsage(tenantB, "tenant-b", "acct-shared-id")
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	if accountUsage.TotalTokens != 0 {
		t.Fatalf("tenant-b sees %d tokens on acct-shared-id; tenant-a's usage must never be visible across the tenant boundary", accountUsage.TotalTokens)
	}

	projectUsage, err := harness.tracker.GetProjectUsage(tenantB, "tenant-b", "proj-shared-id")
	if err != nil {
		t.Fatalf("GetProjectUsage: %v", err)
	}
	if projectUsage.TotalTokens != 0 {
		t.Fatalf("tenant-b sees %d tokens on proj-shared-id", projectUsage.TotalTokens)
	}
}
