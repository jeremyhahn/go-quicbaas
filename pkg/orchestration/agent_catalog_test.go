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
	"testing"
)

// fixedLoad reports a canned live-job count per backend.
type fixedLoad struct {
	live map[string]int
	err  error
}

func (f fixedLoad) LiveJobsForBackend(_ context.Context, _, backend string) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.live[backend], nil
}

// stubDemand answers what each role has queued.
type stubDemand struct {
	queued map[AgentRole]int
	err    error
}

func (s stubDemand) QueuedForRole(_ context.Context, _ string, role AgentRole) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.queued[role], nil
}

// testAgent is a valid Agent to vary per test.
func testAgent(id, backend, model string, roles ...AgentRole) Agent {
	if len(roles) == 0 {
		roles = []AgentRole{AgentRoleCoding}
	}
	return Agent{ID: id, Name: strings.ToUpper(id), Roles: roles, Backend: backend, Model: model}
}

func testCatalogue() AgentCatalogue {
	npu := testAgent("npu-qwen", "openvino", "qwen3-8b", AgentRoleTriage)
	npu.MaxConcurrent = 2
	codex := testAgent("codex-hosted", "codex", "gpt-5.5", AgentRoleTriage, AgentRoleReview)
	return AgentCatalogue{
		Agents: map[string]Agent{npu.ID: npu, codex.ID: codex},
		Pools: map[string]AgentPool{
			"triage": {ID: "triage", Name: "Triage", Agents: []string{"npu-qwen", "codex-hosted"}},
		},
	}
}

func TestAgentRole_Valid(t *testing.T) {
	for _, role := range AgentRoles() {
		if !role.Valid() {
			t.Fatalf("role %q must be valid", role)
		}
	}
	if AgentRole("wizard").Valid() || AgentRole("").Valid() {
		t.Fatal("an unknown or empty role was accepted")
	}
}

func TestAgent_ValidateAcceptsAWellFormedAgent(t *testing.T) {
	agent := testAgent("claude-coder", "claude", "claude-opus-5-5")
	agent.ReasoningEffort = "high"
	agent.Provider = ProviderEndpoint{Name: "ovms", BaseURL: "http://gpu:9000/v1", WireAPI: "chat"}
	if err := agent.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestAgent_ValidateRefusesEveryMalformedField(t *testing.T) {
	cases := map[string]func(*Agent){
		"agent.id":                func(a *Agent) { a.ID = "Bad ID" },
		"agent.name":              func(a *Agent) { a.Name = " " },
		"agent.roles":             func(a *Agent) { a.Roles = nil },
		"agent.backend":           func(a *Agent) { a.Backend = "" },
		"agent.model":             func(a *Agent) { a.Model = "" },
		"agent.reasoning_effort":  func(a *Agent) { a.ReasoningEffort = "extreme" },
		"agent.provider.wire_api": func(a *Agent) { a.Provider = ProviderEndpoint{BaseURL: "http://gpu:9000/v1"} },
		"agent.max_concurrent":    func(a *Agent) { a.MaxConcurrent = -1 },
		"agent.context_tokens":    func(a *Agent) { a.ContextTokens = -1 },
		"role_policy.reserve":     func(a *Agent) { a.Placement = RolePolicy{Prefer: AgentRoleReview, Reserve: -1} },
		"agent.prompt":            func(a *Agent) { a.Prompt = AgentPrompt{Charter: "x", CharterSet: "y", System: "z"} },
		"agent.spend":             func(a *Agent) { a.Spend = SpendLimits{Windows: []QuotaWindow{{Name: "daily", MaxTokens: 10}}} },
	}
	for field, mutate := range cases {
		t.Run(field, func(t *testing.T) {
			agent := testAgent("a", "claude", "claude-opus-5-5")
			mutate(&agent)
			err := agent.Validate()
			var invalid *InputValidationError
			if !errors.As(err, &invalid) || invalid.Field != field {
				t.Fatalf("Validate() = %v, want an InputValidationError on %s", err, field)
			}
		})
	}
}

func TestAgent_ValidateRefusesAnUnknownOrRepeatedRole(t *testing.T) {
	agent := testAgent("a", "claude", "m", AgentRole("wizard"))
	if err := agent.Validate(); err == nil {
		t.Fatal("an unknown role was accepted")
	}
	agent = testAgent("a", "claude", "m", AgentRoleReview, AgentRoleReview)
	if err := agent.Validate(); err == nil {
		t.Fatal("a repeated role was accepted")
	}
}

func TestAgent_HasRole(t *testing.T) {
	agent := testAgent("a", "codex", "gpt-5.5", AgentRoleReview, AgentRoleTriage)
	if !agent.HasRole(AgentRoleReview) || agent.HasRole(AgentRoleCoding) {
		t.Fatal("HasRole must answer exactly the declared roles")
	}
}

// TestAgent_ExtraArgsCarryProviderAndEffortPerRuntime: codex takes both as -c
// overrides; a runtime whose dialect is unknown takes nothing.
func TestAgent_ExtraArgsCarryProviderAndEffortPerRuntime(t *testing.T) {
	agent := testAgent("q", "codex", "qwen3-8b")
	agent.Provider = ProviderEndpoint{Name: "ollama", BaseURL: "http://gpu:11434/v1", WireAPI: "Responses"}
	agent.ReasoningEffort = "medium"
	args := strings.Join(agent.ExtraArgs(), " ")
	for _, want := range []string{
		`model_provider="ollama"`,
		`model_providers.ollama.base_url="http://gpu:11434/v1"`,
		`model_providers.ollama.wire_api="responses"`,
		`model_reasoning_effort="medium"`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	claude := testAgent("c", "claude", "claude-opus-5-5")
	claude.ReasoningEffort = "high"
	claude.Provider = agent.Provider
	if got := claude.ExtraArgs(); len(got) != 0 {
		t.Fatalf("claude takes no codex overrides, got %v", got)
	}
}

func TestAgentPrompt_ValidateAndRender(t *testing.T) {
	custom := AgentPrompt{System: "be terse"}
	if err := custom.Validate(); err != nil {
		t.Fatalf("a custom prompt is valid: %v", err)
	}
	if got, err := custom.Render(); err != nil || got != "be terse" {
		t.Fatalf("Render() = %q, %v", got, err)
	}
	if got, err := (AgentPrompt{}).Render(); err != nil || got != "" {
		t.Fatalf("an empty prompt renders nothing, got %q, %v", got, err)
	}
	if !(AgentPrompt{}).IsZero() || custom.IsZero() {
		t.Fatal("IsZero must answer whether the prompt names anything")
	}
	set := WorkflowPrompts()
	charter := AgentPrompt{CharterSet: string(set.Kind()), Charter: set.Names()[0]}
	if err := charter.Validate(); err != nil {
		t.Fatalf("a registered charter is valid: %v", err)
	}
}

func TestAgentPrompt_ValidateRefusesAnUnknownCharter(t *testing.T) {
	if err := (AgentPrompt{CharterSet: "nope", Charter: "x"}).Validate(); err == nil {
		t.Fatal("an unregistered prompt set was accepted")
	}
	set := WorkflowPrompts()
	err := (AgentPrompt{CharterSet: string(set.Kind()), Charter: "no-such-charter"}).Validate()
	if err == nil {
		t.Fatal("a charter the set does not carry was accepted")
	}
	if _, err := (AgentPrompt{CharterSet: "nope", Charter: "x"}).Render(); err == nil {
		t.Fatal("Render must refuse what Validate refuses")
	}
}

func TestAgentPool_Validate(t *testing.T) {
	good := AgentPool{ID: "p", Name: "P", Agents: []string{"a", "b"}}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	bad := map[string]AgentPool{
		"id":        {ID: "P!", Name: "P", Agents: []string{"a"}},
		"name":      {ID: "p", Agents: []string{"a"}},
		"empty":     {ID: "p", Name: "P"},
		"duplicate": {ID: "p", Name: "P", Agents: []string{"a", "a"}},
		"malformed": {ID: "p", Name: "P", Agents: []string{"A"}},
	}
	for name, pool := range bad {
		if err := pool.Validate(); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: Validate() = %v, want invalid input", name, err)
		}
	}
}

func TestAgentRef_ValidateAndAccessors(t *testing.T) {
	agent, pool := AgentRefTo("a"), PoolRefTo("p")
	if agent.IsZero() || agent.IsPool() || !pool.IsPool() || !(AgentRef{}).IsZero() {
		t.Fatal("IsZero/IsPool disagree with the reference")
	}
	if agent.String() != "agent:a" || pool.String() != "pool:p" || (AgentRef{}).String() != "" {
		t.Fatalf("String() = %q, %q", agent.String(), pool.String())
	}
	if !agent.Names(AgentRefTo("a")) || agent.Names(PoolRefTo("a")) {
		t.Fatal("Names must compare both fields")
	}
	for _, ref := range []AgentRef{agent, pool, {}} {
		if err := ref.Validate("slot"); err != nil {
			t.Fatalf("Validate(%v): %v", ref, err)
		}
	}
	for _, ref := range []AgentRef{{AgentID: "a", PoolID: "p"}, {AgentID: "A"}, {PoolID: "p q"}} {
		if err := ref.Validate("slot"); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Validate(%v) = %v, want invalid input", ref, err)
		}
	}
	if err := RequireAgentRef(AgentRef{}, "slot"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("RequireAgentRef(zero) = %v", err)
	}
	if err := RequireAgentRef(agent, "slot"); err != nil {
		t.Fatalf("RequireAgentRef(agent): %v", err)
	}
}

func TestValidCatalogID(t *testing.T) {
	for _, id := range []string{"a", "claude-coder", "qwen3.8b_npu"} {
		if !ValidCatalogID(id) {
			t.Fatalf("%q must be valid", id)
		}
	}
	for _, id := range []string{"", "-a", "A", "a b", strings.Repeat("a", 65)} {
		if ValidCatalogID(id) {
			t.Fatalf("%q must be refused", id)
		}
	}
}

// =============================================================================
// Catalogue
// =============================================================================

func TestAgentCatalogue_CandidatesKeepPoolOrder(t *testing.T) {
	got, err := testCatalogue().Candidates(PoolRefTo("triage"))
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(got) != 2 || got[0].ID != "npu-qwen" || got[1].ID != "codex-hosted" {
		t.Fatalf("candidates = %v, want the pool's order", got)
	}
	one, err := testCatalogue().Candidates(AgentRefTo("codex-hosted"))
	if err != nil || len(one) != 1 {
		t.Fatalf("an agent reference is one candidate: %v, %v", one, err)
	}
}

func TestAgentCatalogue_CandidatesRefuseWhatIsNotDefined(t *testing.T) {
	c := testCatalogue()
	if _, err := c.Candidates(AgentRefTo("ghost")); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("unknown agent: %v", err)
	}
	if _, err := c.Candidates(PoolRefTo("ghost")); !errors.Is(err, ErrAgentPoolUnknown) {
		t.Fatalf("unknown pool: %v", err)
	}
	c.Pools["broken"] = AgentPool{ID: "broken", Name: "B", Agents: []string{"ghost"}}
	_, err := c.Candidates(PoolRefTo("broken"))
	var unknown *UnknownAgentError
	if !errors.As(err, &unknown) || unknown.Pool != "broken" {
		t.Fatalf("a dangling member must name its pool, got %v", err)
	}
	if _, err := c.Candidates(AgentRef{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a zero reference resolves to nothing: %v", err)
	}
}

func TestAgentCatalogue_CheckRefEnforcesTheSlotRole(t *testing.T) {
	c := testCatalogue()
	if err := c.CheckRef(PoolRefTo("triage"), "triage", AgentRoleTriage); err != nil {
		t.Fatalf("every member declares triage: %v", err)
	}
	err := c.CheckRef(PoolRefTo("triage"), "reviewer", AgentRoleReview)
	var mismatch *AgentRoleMismatchError
	if !errors.As(err, &mismatch) || mismatch.AgentID != "npu-qwen" {
		t.Fatalf("a member without the role must be named, got %v", err)
	}
	if err := c.CheckRef(AgentRef{}, "x", AgentRoleCoding); err != nil {
		t.Fatalf("a zero reference is not checked: %v", err)
	}
	if err := c.CheckRef(AgentRefTo("npu-qwen"), "x", ""); err != nil {
		t.Fatalf("an empty role checks existence only: %v", err)
	}
	if err := c.CheckRef(AgentRef{AgentID: "a", PoolID: "b"}, "x", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a malformed reference: %v", err)
	}
}

// =============================================================================
// Resolver
// =============================================================================

// TestAgentResolver_PrefersTheFirstWithCapacity is the routing policy: order
// is the preference, and load only decides when an Agent is full.
func TestAgentResolver_PrefersTheFirstWithCapacity(t *testing.T) {
	candidates, _ := testCatalogue().Candidates(PoolRefTo("triage"))
	r := NewAgentResolver(fixedLoad{live: map[string]int{"openvino": 1}})
	admitted, passed, err := r.Admit(context.Background(), "acme", candidates, AgentRoleTriage)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if len(passed) != 0 || admitted[0].ID != "npu-qwen" {
		t.Fatalf("with the preferred agent at 1/2 it must still lead, got %v", admitted)
	}
}

func TestAgentResolver_SpillsWhenThePreferredIsFull(t *testing.T) {
	candidates, _ := testCatalogue().Candidates(PoolRefTo("triage"))
	r := NewAgentResolver(fixedLoad{live: map[string]int{"openvino": 2}})
	admitted, passed, err := r.Admit(context.Background(), "acme", candidates, AgentRoleTriage)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if len(admitted) != 1 || admitted[0].ID != "codex-hosted" {
		t.Fatalf("a full preferred agent must spill to the next, got %v", admitted)
	}
	if len(passed) != 1 || passed[0].Reason != AgentFailoverAtCapacity || !strings.Contains(passed[0].Detail, "2/2") {
		t.Fatalf("the full agent must be recorded with its load, got %v", passed)
	}
}

func TestAgentResolver_LoadReadFailureIsReturned(t *testing.T) {
	candidates, _ := testCatalogue().Candidates(PoolRefTo("triage"))
	r := NewAgentResolver(fixedLoad{err: errors.New("lease store down")})
	if _, _, err := r.Admit(context.Background(), "acme", candidates, AgentRoleTriage); err == nil {
		t.Fatal("a load that cannot be read must not be guessed")
	}
}

func TestAgentResolver_NilLoadReaderLooksIdle(t *testing.T) {
	candidates, _ := testCatalogue().Candidates(PoolRefTo("triage"))
	admitted, _, err := NewAgentResolver(nil).Admit(context.Background(), "acme", candidates, AgentRoleTriage)
	if err != nil || len(admitted) != 2 {
		t.Fatalf("admitted = %v, %v", admitted, err)
	}
	if (*AgentResolver)(nil).WithRoleDemand(nil) != nil {
		t.Fatal("a nil resolver stays nil")
	}
}

// placementCandidates: bigcard is the scarce accelerator held for review;
// small is the always-available fallback.
func placementCandidates(policy RolePolicy) []Agent {
	big := testAgent("bigcard", "ollama", "review-q4", AgentRoleReview, AgentRoleTriage)
	big.MaxConcurrent = 2
	big.Placement = policy
	small := testAgent("small", "openvino", "qwen3-8b", AgentRoleTriage)
	small.MaxConcurrent = 1
	return []Agent{big, small}
}

func firstAdmitted(t *testing.T, r *AgentResolver, candidates []Agent, role AgentRole) string {
	t.Helper()
	admitted, _, err := r.Admit(context.Background(), "t", candidates, role)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if len(admitted) == 0 {
		t.Fatal("nothing admitted")
	}
	return admitted[0].ID
}

// TestAgentResolver_IdleCapacityIsSharedNotReserved: prefer one role, but help
// out when that role's queue is empty.
func TestAgentResolver_IdleCapacityIsSharedNotReserved(t *testing.T) {
	r := NewAgentResolver(nil).WithRoleDemand(stubDemand{queued: map[AgentRole]int{AgentRoleReview: 0}})
	if got := firstAdmitted(t, r, placementCandidates(RolePolicy{Prefer: AgentRoleReview}), AgentRoleTriage); got != "bigcard" {
		t.Fatalf("agent = %q, want triage to use the idle preferred card", got)
	}
}

// TestAgentResolver_AQueuedPreferredRoleTakesItsCardBack: when review has work
// waiting, triage spills rather than occupy the card set aside.
func TestAgentResolver_AQueuedPreferredRoleTakesItsCardBack(t *testing.T) {
	r := NewAgentResolver(nil).WithRoleDemand(stubDemand{queued: map[AgentRole]int{AgentRoleReview: 3}})
	candidates := placementCandidates(RolePolicy{Prefer: AgentRoleReview})
	if got := firstAdmitted(t, r, candidates, AgentRoleTriage); got != "small" {
		t.Fatalf("agent = %q, want triage to spill off the contended card", got)
	}
	if got := firstAdmitted(t, r, candidates, AgentRoleReview); got != "bigcard" {
		t.Fatalf("agent = %q, want review on the card held for it", got)
	}
}

func TestAgentResolver_ExclusiveRefusesEvenAnEmptyQueue(t *testing.T) {
	r := NewAgentResolver(nil).WithRoleDemand(stubDemand{})
	candidates := placementCandidates(RolePolicy{Prefer: AgentRoleReview, Exclusive: true})
	admitted, passed, _ := r.Admit(context.Background(), "t", candidates, AgentRoleTriage)
	if admitted[0].ID != "small" || passed[0].Reason != AgentFailoverHeldForRole {
		t.Fatalf("an exclusive card must be refused even when idle: %v %v", admitted, passed)
	}
}

func TestAgentResolver_ReserveHoldsSlotsForLatency(t *testing.T) {
	r := NewAgentResolver(fixedLoad{live: map[string]int{"ollama": 1}}).WithRoleDemand(stubDemand{})
	candidates := placementCandidates(RolePolicy{Prefer: AgentRoleReview, Reserve: 1})
	if got := firstAdmitted(t, r, candidates, AgentRoleTriage); got != "small" {
		t.Fatalf("agent = %q, want the reserved slot withheld from triage", got)
	}
}

func TestAgentResolver_AnUnreadableQueueAdmitsTheWork(t *testing.T) {
	r := NewAgentResolver(nil).WithRoleDemand(stubDemand{err: errors.New("store unavailable")})
	if got := firstAdmitted(t, r, placementCandidates(RolePolicy{Prefer: AgentRoleReview}), AgentRoleTriage); got != "bigcard" {
		t.Fatalf("agent = %q, want an unreadable queue to admit rather than idle the fleet", got)
	}
}

func TestRolePolicy_ValidateRefusesTermsWithNoSubject(t *testing.T) {
	for _, policy := range []RolePolicy{
		{Reserve: 2}, {Exclusive: true}, {Prefer: AgentRoleReview, Reserve: -1}, {Prefer: "wizard"},
	} {
		if err := policy.Validate(); err == nil {
			t.Fatalf("%+v was accepted", policy)
		}
	}
	if err := (RolePolicy{}).Validate(); err != nil {
		t.Fatalf("the zero value must be valid: %v", err)
	}
}

// =============================================================================
// Failover classification and errors
// =============================================================================

func TestAgentFailoverReason_ClassifiesEveryTrigger(t *testing.T) {
	cases := map[error]string{
		&AccountLimitedError{}:                     AgentFailoverRateLimited,
		ErrAccountsCoolingDown:                     AgentFailoverRateLimited,
		ErrAccountCredentialExpiring:               AgentFailoverAccountExpired,
		ErrAccountRefreshFailed:                    AgentFailoverAccountExpired,
		ErrNoAccountAvailable:                      AgentFailoverAccountExhausted,
		ErrAccountsSpent:                           AgentFailoverAccountExhausted,
		ErrAccountsSaturated:                       AgentFailoverAccountExhausted,
		&BackendNotFoundError{Backend: "openvino"}: AgentFailoverBackendUnavailable,
		&NoMatchingFleetNodeError{Detail: "none"}:  AgentFailoverBackendUnavailable,
	}
	for err, want := range cases {
		got, ok := AgentFailoverReason(err)
		if !ok || got != want {
			t.Fatalf("AgentFailoverReason(%v) = %q, %v; want %q", err, got, ok, want)
		}
	}
}

func TestAgentFailoverReason_OtherRefusalsStopTheWalk(t *testing.T) {
	for _, err := range []error{nil, errors.New("boom"), &InputValidationError{Field: "x"}, ErrSessionWorkspace} {
		if reason, ok := AgentFailoverReason(err); ok {
			t.Fatalf("AgentFailoverReason(%v) = %q, want no failover", err, reason)
		}
	}
}

func TestAgentCatalogErrors_UnwrapToTheirSentinels(t *testing.T) {
	cases := map[error]error{
		&UnknownAgentError{ID: "a", Pool: "p"}:    ErrAgentUnknown,
		&UnknownAgentPoolError{ID: "p"}:           ErrAgentPoolUnknown,
		&AgentExistsError{Kind: "agent", ID: "a"}: ErrAgentExists,
		&AgentInUseError{Ref: AgentRefTo("a"), UsedBy: []AgentUsage{{Kind: "pool", ID: "p", Slot: "member"}}}: ErrAgentInUse,
		&AgentRoleMismatchError{Field: "worker", AgentID: "a", Role: AgentRoleCoding}:                         ErrAgentRoleMismatch,
		&AgentCatalogInvalidError{Field: "agent.model", Value: "m", Known: []string{"x"}, Message: "no"}:      ErrAgentCatalogInvalid,
		&AgentCatalogUnavailableError{Ref: PoolRefTo("p")}:                                                    ErrAgentCatalogUnavailable,
		&AgentCatalogStoreError{Message: "down"}:                                                              ErrAgentCatalogStore,
	}
	for err, sentinel := range cases {
		if !errors.Is(err, sentinel) || err.Error() == "" {
			t.Fatalf("%T must unwrap to %v and render, got %q", err, sentinel, err.Error())
		}
	}
	inUse := &AgentInUseError{Ref: AgentRefTo("a"), UsedBy: []AgentUsage{{Kind: "binding", ID: "proj", Slot: "worker"}}}
	if !strings.Contains(inUse.Error(), `binding "proj" (worker)`) {
		t.Fatalf("the refusal must name its users: %s", inUse.Error())
	}
}

func TestAgentPoolExhaustedError_UnwrapsToTheLastCause(t *testing.T) {
	err := &AgentPoolExhaustedError{
		Ref:      PoolRefTo("p"),
		Attempts: []AgentAttempt{{AgentID: "a", Reason: AgentFailoverRateLimited}, {AgentID: "b", Reason: AgentFailoverAccountExhausted}},
		Last:     ErrAccountsSpent,
	}
	if !errors.Is(err, ErrAgentPoolExhausted) || !errors.Is(err, ErrAccountsSpent) {
		t.Fatal("the exhaustion must answer both the sentinel and the last cause")
	}
	if !strings.Contains(err.Error(), "a (rate_limited), b (account_exhausted)") {
		t.Fatalf("the error must list each attempt: %s", err.Error())
	}
	bare := &AgentPoolExhaustedError{Ref: PoolRefTo("p")}
	if !errors.Is(bare, ErrAgentPoolExhausted) || slices.Contains(bare.Unwrap(), nil) {
		t.Fatal("an exhaustion with no cause unwraps to the sentinel alone")
	}
}
