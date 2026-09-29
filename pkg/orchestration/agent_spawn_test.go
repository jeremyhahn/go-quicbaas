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
	"slices"
	"strings"
	"sync"
	"testing"
)

// scriptedBackend spawns or refuses as scripted, and records what it saw.
type scriptedBackend struct {
	testMinimalBackend
	name string
	err  error

	mu    sync.Mutex
	spawn []*SpawnRequest
}

func (b *scriptedBackend) Type() string { return b.name }

func (b *scriptedBackend) Spawn(_ context.Context, req *SpawnRequest) (*Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spawn = append(b.spawn, req)
	if b.err != nil {
		return nil, b.err
	}
	return &Session{ID: "ses-" + b.name}, nil
}

func (b *scriptedBackend) requests() []*SpawnRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.spawn)
}

// spawnFixture is an agent service with a catalog of two claude agents on
// two backends, and a pool naming both in order.
type spawnFixture struct {
	svc       *agentServiceImpl
	auditor   *recordingAuditor
	primary   *scriptedBackend
	secondary *scriptedBackend
}

func newSpawnFixture(t *testing.T, primaryErr, secondaryErr error) *spawnFixture {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	primary := &scriptedBackend{name: "primary", err: primaryErr}
	secondary := &scriptedBackend{name: "secondary", err: secondaryErr}
	impl.backends.Store("primary", primary)
	impl.backends.Store("secondary", secondary)

	auditor := &recordingAuditor{}
	dir, err := NewAgentDirectory(AgentDirectoryConfig{
		Store:   NewInMemoryAgentCatalogStore(),
		Models:  stubModels{"primary": {}, "secondary": {}},
		Auditor: auditor,
	})
	if err != nil {
		t.Fatalf("NewAgentDirectory: %v", err)
	}
	ctx := context.Background()
	first := testAgent("first", "primary", "model-a", AgentRoleGeneral)
	first.Prompt = AgentPrompt{System: "you are first"}
	first.ReasoningEffort = "low"
	second := testAgent("second", "secondary", "model-b", AgentRoleGeneral)
	for _, agent := range []*Agent{&first, &second} {
		if _, err := dir.CreateAgent(ctx, SystemTenantID, agent); err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}
	}
	pool := AgentPool{ID: "chain", Name: "Chain", Agents: []string{"first", "second"}}
	if _, err := dir.CreatePool(ctx, SystemTenantID, &pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	impl.SetAgentDirectory(dir)
	return &spawnFixture{svc: impl, auditor: auditor, primary: primary, secondary: secondary}
}

func TestSpawnSession_ByAgentAppliesTheAgent(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	resp, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{AgentID: "first"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if resp.ResolvedAgentID != "first" || len(resp.Failovers) != 0 {
		t.Fatalf("response = %+v, want the agent resolved with no failover", resp)
	}
	got := f.primary.requests()
	if len(got) != 1 || got[0].Model != "model-a" || got[0].SystemPrompt != "you are first" {
		t.Fatalf("the backend saw %+v, want the agent's model and prompt", got)
	}
	if f.svc.AgentDirectory() == nil {
		t.Fatal("AgentDirectory must return what was installed")
	}
}

func TestSpawnSession_PoolFailsOverOnAnAccountLimitAndAuditsIt(t *testing.T) {
	f := newSpawnFixture(t, &AccountLimitedError{AccountID: "acct-1"}, nil)
	resp, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{PoolID: "chain"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if resp.ResolvedAgentID != "second" {
		t.Fatalf("resolved %q, want the pool to fail over to second", resp.ResolvedAgentID)
	}
	if len(resp.Failovers) != 1 || resp.Failovers[0].AgentID != "first" || resp.Failovers[0].Reason != AgentFailoverRateLimited {
		t.Fatalf("failovers = %+v", resp.Failovers)
	}
	events := auditActions(f.auditor, AuditActionAgentFailover)
	if len(events) != 1 || !strings.Contains(events[0].ResourceID, "first -> second (rate_limited)") || !events[0].Success {
		t.Fatalf("the switch must be audited: %+v", events)
	}
}

func TestSpawnSession_PoolStopsOnARefusalTheNextAgentWouldShare(t *testing.T) {
	boom := &InputValidationError{Field: "workdir", Message: "refused"}
	f := newSpawnFixture(t, boom, nil)
	_, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{PoolID: "chain"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SpawnSession = %v, want the refusal itself", err)
	}
	if len(f.secondary.requests()) != 0 {
		t.Fatal("a refusal the next agent would share must not be retried on it")
	}
}

func TestSpawnSession_ExhaustedPoolNamesEveryAttempt(t *testing.T) {
	f := newSpawnFixture(t, &AccountLimitedError{}, ErrAccountsSpent)
	_, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{PoolID: "chain"})
	var exhausted *AgentPoolExhaustedError
	if !errors.As(err, &exhausted) || len(exhausted.Attempts) != 2 {
		t.Fatalf("SpawnSession = %v, want both attempts recorded", err)
	}
	if !errors.Is(err, ErrAccountsSpent) || !errors.Is(err, ErrAgentPoolExhausted) {
		t.Fatal("the exhaustion must unwrap to the last cause and the sentinel")
	}
	events := auditActions(f.auditor, AuditActionAgentFailover)
	if len(events) != 2 || events[1].Success || !strings.HasSuffix(events[1].ResourceID, "second -> none (account_exhausted)") {
		t.Fatalf("both switches are audited as unresolved: %+v", events)
	}
}

func TestSpawnSession_OneAgentReturnsItsOwnRefusal(t *testing.T) {
	f := newSpawnFixture(t, &AccountLimitedError{AccountID: "acct-1"}, nil)
	_, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{AgentID: "first"})
	var limited *AccountLimitedError
	if !errors.As(err, &limited) || errors.Is(err, ErrAgentPoolExhausted) {
		t.Fatalf("SpawnSession = %v, want the account limit unwrapped", err)
	}
}

func TestSpawnSession_RefusesAReferenceItCannotResolve(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	ctx := context.Background()
	if _, err := f.svc.SpawnSession(ctx, &SpawnSessionRequest{AgentID: "first", Backend: "primary"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an agent and a backend = %v", err)
	}
	if _, err := f.svc.SpawnSession(ctx, &SpawnSessionRequest{AgentID: "first", PoolID: "chain"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an agent and a pool = %v", err)
	}
	if _, err := f.svc.SpawnSession(ctx, &SpawnSessionRequest{AgentID: "ghost"}); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("an unknown agent = %v", err)
	}
	f.svc.SetAgentDirectory(nil)
	if _, err := f.svc.SpawnSession(ctx, &SpawnSessionRequest{AgentID: "first"}); !errors.Is(err, ErrAgentCatalogUnavailable) {
		t.Fatalf("no catalog = %v", err)
	}
	if _, err := f.svc.SpawnSession(ctx, nil); err == nil {
		t.Fatal("a nil request must be refused")
	}
}

func TestSpawnSessionRequest_ForAgentAppliesEveryAgentSetting(t *testing.T) {
	agent := testAgent("q", "codex", "gpt-5.5", AgentRoleReview)
	agent.Image = "worker:1"
	agent.ReasoningEffort = "high"
	agent.Account = AgentAccountPolicy{Strategy: LeastUsageAccountStrategyName, Accounts: []string{"acct-1"}}
	agent.Prompt = AgentPrompt{System: "review carefully"}
	prepared := false
	req := &SpawnSessionRequest{
		AgentID:   "q",
		ExtraArgs: []string{"--flag"},
		Env:       map[string]string{"A": "1"},
		Workspace: &SessionWorkspace{Key: "k", Spec: &WorkspaceSpecFiles{Files: map[string][]byte{"token": []byte("secret")}}},
		prepareCandidate: func(r *SpawnSessionRequest) {
			prepared = r.Backend == "codex"
		},
	}
	got, err := req.forAgent(agent)
	if err != nil {
		t.Fatalf("forAgent: %v", err)
	}
	if got.AgentID != "" || got.Backend != "codex" || got.Model != "gpt-5.5" || got.ContainerImage != "worker:1" ||
		got.SandboxMode != SandboxModeRequired || got.SystemPrompt != "review carefully" {
		t.Fatalf("candidate = %+v", got)
	}
	if !got.AccountAuto || got.AccountStrategy != LeastUsageAccountStrategyName || !slices.Equal(got.AccountAllow, []string{"acct-1"}) {
		t.Fatalf("the account policy must reach the spawn: %+v", got)
	}
	if !slices.Contains(got.ExtraArgs, `model_reasoning_effort="high"`) || req.ExtraArgs[len(req.ExtraArgs)-1] != "--flag" {
		t.Fatalf("args = %v; the caller's are kept and not mutated", got.ExtraArgs)
	}
	if !prepared || got.prepareCandidate != nil {
		t.Fatal("the job's preparation runs once, against the resolved backend")
	}
	got.Workspace.Spec.Wipe()
	if string(req.Workspace.Spec.Files["token"]) != "secret" {
		t.Fatal("wiping a candidate's spec must leave the original for the next attempt")
	}
	// A request that already names its prompt and image keeps them.
	req.SystemPrompt, req.ContainerImage = "charter", "own:1"
	got, _ = req.forAgent(agent)
	if got.SystemPrompt != "charter" || got.ContainerImage != "own:1" {
		t.Fatalf("the caller's prompt and image win: %+v", got)
	}
}

func TestSpawnSessionRequest_ForAgentRefusesAnUnrenderablePrompt(t *testing.T) {
	agent := testAgent("q", "codex", "gpt-5.5")
	agent.Prompt = AgentPrompt{CharterSet: "nope", Charter: "x"}
	if _, err := (&SpawnSessionRequest{AgentID: "q"}).forAgent(agent); err == nil {
		t.Fatal("an unrenderable prompt must refuse the spawn")
	}
	if (*SessionWorkspace)(nil).clone() != nil {
		t.Fatal("a nil workspace clones to nil")
	}
}

func TestJobRun_AuditsAgentFailoversOnTheJobTrail(t *testing.T) {
	run := &JobRun{}
	req := &SpawnSessionRequest{PoolID: "chain"}
	run.auditAgentFailovers(req, &SpawnSessionResponse{
		ResolvedAgentID: "second",
		Failovers:       []AgentAttempt{{AgentID: "first", Reason: AgentFailoverRateLimited}},
	})
	trail := run.auditEventsCopy()
	if len(trail) != 2 || !strings.Contains(trail[0].Details, "passed over first (rate_limited)") ||
		!strings.Contains(trail[1].Details, "runs as agent second") {
		t.Fatalf("trail = %+v", trail)
	}
	run.auditAgentFailovers(req, &SpawnSessionResponse{})
	run.auditAgentFailovers(req, nil)
	if len(run.auditEventsCopy()) != 2 {
		t.Fatal("a spawn by backend records nothing")
	}
}

func TestJobLead_ResolvesTheRequestThenTheProjectDefault(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	ctx := context.Background()

	lead, err := f.svc.jobLead(ctx, SystemTenantID, &StartJobRequest{Lead: PoolRefTo("chain")})
	if err != nil || lead.Backend != "primary" || lead.Model != "model-a" || !lead.Ref.Names(PoolRefTo("chain")) {
		t.Fatalf("jobLead(pool) = %+v, %v", lead, err)
	}
	lead, err = f.svc.jobLead(ctx, SystemTenantID, &StartJobRequest{routedBackend: "ollama"})
	if err != nil || lead.Backend != "ollama" || !lead.Ref.IsZero() {
		t.Fatalf("jobLead(routed) = %+v, %v", lead, err)
	}

	projects := NewInMemoryProjectStore()
	if err := projects.Create(ctx, &Project{
		ID: "proj-1", TenantID: SystemTenantID, Name: "p",
		DefaultLeadAgent: AgentRefTo("second"),
	}); err != nil {
		t.Fatalf("Create project: %v", err)
	}
	f.svc.projectStore = projects
	lead, err = f.svc.jobLead(ctx, SystemTenantID, &StartJobRequest{ProjectID: "proj-1"})
	if err != nil || lead.Backend != "secondary" {
		t.Fatalf("jobLead(project default) = %+v, %v", lead, err)
	}
	lead, err = f.svc.jobLead(ctx, SystemTenantID, &StartJobRequest{ProjectID: "missing"})
	if err != nil || !lead.Ref.IsZero() {
		t.Fatalf("an unknown project names no lead: %+v, %v", lead, err)
	}
}

func TestJobLead_RefusesWhatCannotBeResolved(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	ctx := context.Background()
	if _, err := f.svc.jobLead(ctx, SystemTenantID, &StartJobRequest{Lead: AgentRefTo("ghost")}); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("an unknown lead = %v", err)
	}
	if _, err := f.svc.jobLead(ctx, SystemTenantID, &StartJobRequest{Lead: AgentRef{AgentID: "a", PoolID: "b"}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a malformed lead = %v", err)
	}
	f.svc.SetAgentDirectory(nil)
	if _, err := f.svc.jobLead(ctx, SystemTenantID, &StartJobRequest{Lead: AgentRefTo("first")}); !errors.Is(err, ErrAgentCatalogUnavailable) {
		t.Fatalf("no catalog = %v", err)
	}
}
