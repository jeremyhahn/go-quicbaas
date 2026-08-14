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
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Harness
//
// The account pin has to survive a daemon restart, so the harness models two
// daemon lifetimes over one durable store — and, crucially, a pool whose
// least-used account CHANGES between them. That is the condition under which a
// resume that re-runs selection silently moves a conversation onto a different
// provider account.
// =============================================================================

// poolResumeBackend is a resume-capable backend backed by a settable account
// pool. Its "selection" is whatever autoPick currently names, which lets a test
// move the pool between daemon lifetimes exactly as real usage would.
type poolResumeBackend struct {
	mu sync.Mutex

	// autoPick is the account strategy-based selection currently resolves to.
	autoPick string

	// spawnCalls and resumeCalls record what the orchestrator asked for.
	spawnCalls  []*SpawnRequest
	resumeCalls []*ResumeRequest

	live map[string]bool
}

func (b *poolResumeBackend) setAutoPick(account string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.autoPick = account
}

// resolve mirrors AccountResolver's contract: a pinned account wins outright
// and no strategy runs; otherwise the strategy picks, honoring the rotation
// policy's allow-list and exclusions exactly as accountEligibility does. The
// double has to model those or a rotation test could pass while the real
// resolver handed back the very account the rotation was moving off.
func (b *poolResumeBackend) resolve(req *SpawnRequest) (accountID, strategy string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if req.AccountID != "" {
		return req.AccountID, "pinned"
	}
	if !req.AccountAuto {
		return "", ""
	}
	picked := b.autoPick
	for _, excluded := range req.AccountExclude {
		if picked == excluded {
			return "", ""
		}
	}
	if len(req.AccountAllow) > 0 {
		allowed := false
		for _, id := range req.AccountAllow {
			if id == picked {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", ""
		}
	}
	return picked, LeastUsageAccountStrategyName
}

func (b *poolResumeBackend) Spawn(_ context.Context, req *SpawnRequest) (*Session, error) {
	accountID, strategy := b.resolve(req)

	b.mu.Lock()
	b.spawnCalls = append(b.spawnCalls, req)
	if b.live == nil {
		b.live = map[string]bool{}
	}
	id := "sess" + strings.ReplaceAll(req.WorkDir, "/", "-")
	b.live[id] = true
	b.mu.Unlock()

	return &Session{
		ID:              id,
		ProjectID:       req.ProjectID,
		Backend:         "pool",
		Status:          SessionStateActive,
		WorkDir:         req.WorkDir,
		CreatedAt:       time.Now().UnixNano(),
		AccountID:       accountID,
		AccountStrategy: strategy,
	}, nil
}

func (b *poolResumeBackend) Resume(_ context.Context, req *ResumeRequest) (*ResumeResult, error) {
	accountID, strategy := b.resolve(req.Spawn)

	b.mu.Lock()
	b.resumeCalls = append(b.resumeCalls, req)
	if b.live == nil {
		b.live = map[string]bool{}
	}
	b.live[req.SessionID] = true
	b.mu.Unlock()

	return &ResumeResult{
		Session: &Session{
			ID:              req.SessionID,
			Backend:         "pool",
			Status:          SessionStateActive,
			AccountID:       accountID,
			AccountStrategy: strategy,
		},
		Mode: ResumeModeReplay,
	}, nil
}

func (b *poolResumeBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	b.mu.Lock()
	live := b.live[req.SessionID]
	b.mu.Unlock()
	if req.SessionID != "" && !live {
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}
	return &PromptResponse{
		SessionID:  req.SessionID,
		Content:    []ContentBlock{{Type: "text", Text: "ok"}},
		StopReason: StopReasonEndTurn,
		Usage:      &TokenUsage{InputTokens: 70, OutputTokens: 30},
	}, nil
}

func (b *poolResumeBackend) Status(_ context.Context, sessionID string) (*SessionStatus, error) {
	return &SessionStatus{SessionID: sessionID, State: SessionStateActive}, nil
}

func (b *poolResumeBackend) Close(_ context.Context, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.live, sessionID)
	return nil
}

func (b *poolResumeBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{Kind: BackendKindAgent, PersistentSessions: true}
}

func (b *poolResumeBackend) Type() string         { return "pool" }
func (b *poolResumeBackend) ToolGuidance() string { return "" }

var _ AgentBackend = (*poolResumeBackend)(nil)
var _ SessionResumer = (*poolResumeBackend)(nil)

// accountResumeHarness is one daemon lifetime over a shared durable store.
type accountResumeHarness struct {
	service *agentServiceImpl
	backend *poolResumeBackend
	tracker CostTracker
	kv      interfaces.KVStore
}

func newAccountResumeHarness(t *testing.T, kv interfaces.KVStore, backend *poolResumeBackend) *accountResumeHarness {
	t.Helper()
	if kv == nil {
		kv = kvstore.NewMemory()
	}

	svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{"pool": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}

	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	impl.SetSessionEventLog(log)

	tracker := NewCostTracker(kv, &agentTestLogger{})
	impl.SetCostTracker(tracker)

	return &accountResumeHarness{service: impl, backend: backend, tracker: tracker, kv: kv}
}

// restart builds a second daemon lifetime over the same durable store and runs
// boot-time restoration, exactly as the daemon does at startup.
func (h *accountResumeHarness) restart(t *testing.T) *accountResumeHarness {
	t.Helper()
	next := newAccountResumeHarness(t, h.kv, h.backend)
	if err := next.service.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	return next
}

// =============================================================================
// The regression: a resumed session must keep its ORIGINAL account
// =============================================================================

// TestResume_ReusesOriginalAccount_NotAFreshSelection is the account-pin
// regression. A session spawned with strategy-based selection resolved to one
// account and has been billing to it. If the resume re-runs selection, the
// reconstituted process authenticates as whichever account is least-used NOW —
// a different one — which both breaks the pin and mis-attributes every
// subsequent turn's cost.
func TestResume_ReusesOriginalAccount_NotAFreshSelection(t *testing.T) {
	backend := &poolResumeBackend{autoPick: "acct-a"}
	harness := newAccountResumeHarness(t, nil, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawn, err := harness.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:         "pool",
		WorkDir:         "/work",
		ProjectID:       "proj-1",
		Persistent:      true,
		AccountAuto:     true,
		AccountStrategy: LeastUsageAccountStrategyName,
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if _, err := harness.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawn.SessionID, Prompt: "start the work",
	}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	// The account the strategy actually chose must be on the durable record —
	// the request named none.
	record, err := harness.service.store.GetSession(ctx, "tenant-a", spawn.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.AccountID != "acct-a" {
		t.Fatalf("SessionRecord.AccountID = %q, want acct-a — the resolved account must be persisted, not the (empty) requested one", record.AccountID)
	}
	if record.AccountStrategy != LeastUsageAccountStrategyName {
		t.Fatalf("SessionRecord.AccountStrategy = %q, want %q", record.AccountStrategy, LeastUsageAccountStrategyName)
	}

	// The pool moves: acct-a is now the busy one, so a fresh selection would
	// pick acct-b.
	backend.setAutoPick("acct-b")

	restarted := harness.restart(t)
	if _, err := restarted.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawn.SessionID, Prompt: "carry on",
	}); err != nil {
		t.Fatalf("PromptSession after restart: %v", err)
	}

	backend.mu.Lock()
	resumeCalls := append([]*ResumeRequest(nil), backend.resumeCalls...)
	backend.mu.Unlock()

	if len(resumeCalls) != 1 {
		t.Fatalf("resume calls = %d, want 1", len(resumeCalls))
	}
	replayed := resumeCalls[0].Spawn
	if replayed.AccountID != "acct-a" {
		t.Fatalf("resumed spawn AccountID = %q, want acct-a — the session must keep the account it was billing to", replayed.AccountID)
	}
	// A PLAIN resume still pins. Rotation is the one thing that may re-select,
	// and it goes through RotateSessionAccount / rotationSpawnRequestFromRecord
	// — an explicit, audited, operator-or-limit-triggered move — never through
	// the ordinary reconstitution a prompt triggers. If this ever starts
	// setting AccountAuto, every restart silently re-bills conversations to
	// whichever account the pool happens to favour at boot.
	if replayed.AccountAuto {
		t.Fatal("resumed spawn set AccountAuto; a plain resume must replay the resolved account as a pin — only RotateSessionAccount may re-run selection")
	}
	if len(replayed.AccountExclude) != 0 {
		t.Fatalf("resumed spawn set AccountExclude=%v; exclusions belong to rotation, not to a plain resume", replayed.AccountExclude)
	}

	// And the cost ledger must still see one account, not two.
	usageA, err := restarted.tracker.GetAccountUsage(ctx, "tenant-a", "acct-a")
	if err != nil {
		t.Fatalf("GetAccountUsage(acct-a): %v", err)
	}
	if usageA.TotalTokens != 200 {
		t.Fatalf("acct-a usage = %d, want 200 (two turns of 100)", usageA.TotalTokens)
	}
	usageB, err := restarted.tracker.GetAccountUsage(ctx, "tenant-a", "acct-b")
	if err != nil {
		t.Fatalf("GetAccountUsage(acct-b): %v", err)
	}
	if usageB.TotalTokens != 0 {
		t.Fatalf("acct-b usage = %d, want 0 — the resumed turn must not be charged to an account the session never used", usageB.TotalTokens)
	}
}

// TestResume_PinnedAccountSurvivesRestart covers the static half of the same
// contract: a session pinned to a named account must come back on that account.
func TestResume_PinnedAccountSurvivesRestart(t *testing.T) {
	backend := &poolResumeBackend{autoPick: "acct-drift"}
	harness := newAccountResumeHarness(t, nil, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawn, err := harness.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:    "pool",
		WorkDir:    "/pinned",
		Persistent: true,
		AccountID:  "acct-pinned",
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	restarted := harness.restart(t)
	if _, err := restarted.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawn.SessionID, Prompt: "carry on",
	}); err != nil {
		t.Fatalf("PromptSession after restart: %v", err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.resumeCalls) != 1 {
		t.Fatalf("resume calls = %d, want 1", len(backend.resumeCalls))
	}
	if got := backend.resumeCalls[0].Spawn.AccountID; got != "acct-pinned" {
		t.Fatalf("resumed spawn AccountID = %q, want acct-pinned", got)
	}
}

// TestResume_NoAccountStaysAccountless asserts the untouched default path: a
// session that never used the account pool must not acquire one on resume.
func TestResume_NoAccountStaysAccountless(t *testing.T) {
	backend := &poolResumeBackend{autoPick: "acct-should-not-be-used"}
	harness := newAccountResumeHarness(t, nil, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawn, err := harness.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:    "pool",
		WorkDir:    "/plain",
		Persistent: true,
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	record, err := harness.service.store.GetSession(ctx, "tenant-a", spawn.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.AccountID != "" {
		t.Fatalf("SessionRecord.AccountID = %q, want empty for a spawn that never asked for an account", record.AccountID)
	}

	restarted := harness.restart(t)
	if _, err := restarted.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawn.SessionID, Prompt: "carry on",
	}); err != nil {
		t.Fatalf("PromptSession after restart: %v", err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.resumeCalls) != 1 {
		t.Fatalf("resume calls = %d, want 1", len(backend.resumeCalls))
	}
	replayed := backend.resumeCalls[0].Spawn
	if replayed.AccountID != "" || replayed.AccountAuto {
		t.Fatalf("resumed spawn = {AccountID:%q AccountAuto:%v}, want no account at all", replayed.AccountID, replayed.AccountAuto)
	}
}

// =============================================================================
// Record construction
// =============================================================================

func TestNewDurableSessionRecord_CarriesRequestedAccount(t *testing.T) {
	record := newDurableSessionRecord(
		&SpawnSessionRequest{
			Backend:         "pool",
			AccountID:       "acct-1",
			AccountStrategy: LeastUsageAccountStrategyName,
		},
		"tenant-a", "/work", "model-x", SandboxModeDefault, TrustLevelNone, "", true)

	if record.AccountID != "acct-1" {
		t.Fatalf("AccountID = %q, want acct-1", record.AccountID)
	}
	if record.AccountStrategy != LeastUsageAccountStrategyName {
		t.Fatalf("AccountStrategy = %q, want %q", record.AccountStrategy, LeastUsageAccountStrategyName)
	}
}

func TestSpawnRequestFromRecord_ReplaysAccountAsAPin(t *testing.T) {
	req := spawnRequestFromRecord(&SessionRecord{
		WorkDir:         "/work",
		Persistent:      true,
		AccountID:       "acct-1",
		AccountStrategy: LeastUsageAccountStrategyName,
	})

	if req.AccountID != "acct-1" {
		t.Fatalf("AccountID = %q, want acct-1", req.AccountID)
	}
	if req.AccountAuto {
		t.Fatal("AccountAuto = true; replaying the strategy would re-select and could move the session to another account")
	}
	if req.AccountStrategy != "" {
		t.Fatalf("AccountStrategy = %q, want empty — the strategy is audit history, not a resume input", req.AccountStrategy)
	}
}

func TestSpawnRequestFromRecord_NoAccountProducesNoAccount(t *testing.T) {
	req := spawnRequestFromRecord(&SessionRecord{WorkDir: "/work", Persistent: true})

	if req.AccountID != "" || req.AccountAuto || req.AccountStrategy != "" {
		t.Fatalf("spawnRequestFromRecord() = {AccountID:%q Auto:%v Strategy:%q}, want no account fields set",
			req.AccountID, req.AccountAuto, req.AccountStrategy)
	}
}
