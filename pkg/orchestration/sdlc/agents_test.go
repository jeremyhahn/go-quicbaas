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

package sdlc

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// seatedBinding is testBinding with a counting codex seat, an advisory qwen
// seat, and a second counting codex seat after it.
func seatedBinding() *ProjectBinding {
	binding := testBinding()
	binding.Reviewer = ReviewerConfig{Agent: testReviewerAgent, Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok"}
	binding.Reviewers = []ReviewerConfig{
		{Name: "qwen", Agent: testQwenAgent, Login: "qwen-bot", TokenSecretRef: "qwen-tok"},
		{Name: "backup", Agent: testReviewerAgent, Login: "backup-bot", TokenSecretRef: "backup-tok", CountsTowardMerge: boolPtr(true)},
	}
	return binding
}

func TestProjectBinding_CheckAgentsAcceptsAWellBoundProject(t *testing.T) {
	binding := seatedBinding()
	binding.Triage = orchestration.AgentRefTo(testTriageAgent)
	if err := binding.CheckAgents(testAgents()); err != nil {
		t.Fatalf("CheckAgents: %v", err)
	}
}

func TestProjectBinding_CheckAgentsRefusesWhatCannotRun(t *testing.T) {
	cases := map[string]struct {
		mutate func(*ProjectBinding)
		want   error
	}{
		"an undefined worker":         {func(b *ProjectBinding) { b.Worker = orchestration.AgentRefTo("ghost") }, orchestration.ErrAgentUnknown},
		"an undefined worker pool":    {func(b *ProjectBinding) { b.Worker = orchestration.PoolRefTo("ghost") }, orchestration.ErrAgentPoolUnknown},
		"a reviewer as the worker":    {func(b *ProjectBinding) { b.Worker = orchestration.AgentRefTo(testQwenAgent) }, orchestration.ErrAgentRoleMismatch},
		"a coder as triage":           {func(b *ProjectBinding) { b.Triage = orchestration.AgentRefTo(testMysteryAgent) }, orchestration.ErrAgentRoleMismatch},
		"an undefined seat agent":     {func(b *ProjectBinding) { b.Reviewers[0].Agent = "ghost" }, orchestration.ErrAgentUnknown},
		"a coder in a seat":           {func(b *ProjectBinding) { b.Reviewer.Agent = testCoderAgent }, orchestration.ErrAgentRoleMismatch},
		"a claude reviewer":           {func(b *ProjectBinding) { b.Reviewers[0].Agent = testClaudeReviewerAgent }, ErrReview},
		"a counting same-family seat": {func(b *ProjectBinding) { b.Worker = orchestration.AgentRefTo(testCodexCoderAgent) }, ErrReview},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			binding := seatedBinding()
			tc.mutate(binding)
			if err := binding.CheckAgents(testAgents()); !errors.Is(err, tc.want) {
				t.Fatalf("CheckAgents() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestReviewerFamilyRefusedError_IsAReviewError(t *testing.T) {
	binding := seatedBinding()
	binding.Reviewers[0].Agent = testClaudeReviewerAgent
	var refused *ReviewerFamilyRefusedError
	if err := binding.CheckAgents(testAgents()); !errors.As(err, &refused) || refused.Agent != testClaudeReviewerAgent {
		t.Fatalf("CheckAgents() = %v", err)
	}
}

func TestWorkerFamilies_CoversEveryPoolMember(t *testing.T) {
	catalogue := testAgents()
	catalogue.Pools["mixed"] = orchestration.AgentPool{ID: "mixed", Agents: []string{testCoderAgent, testCodexCoderAgent, testCoderAgent}}
	got := WorkerFamilies(catalogue, orchestration.PoolRefTo("mixed"))
	if !slices.Equal(got, []string{ModelFamilyAnthropic, ModelFamilyOpenAI}) {
		t.Fatalf("families = %v, want each distinct family once", got)
	}
	if WorkerFamilies(catalogue, orchestration.AgentRefTo("ghost")) != nil {
		t.Fatal("an unresolvable reference has no families")
	}
	if got := WorkerFamilies(catalogue, orchestration.AgentRefTo(testMysteryAgent)); !slices.Equal(got, []string{""}) {
		t.Fatalf("an agent of no known family contributes the unknown family, got %v", got)
	}
}

// TestProjectBinding_NextReviewerSeatNeverCrossesTheGate: a counting seat fails
// over to the next counting seat, skipping advisory ones, and the last seat of
// its standing has nowhere to go.
func TestProjectBinding_NextReviewerSeatNeverCrossesTheGate(t *testing.T) {
	binding := seatedBinding()
	seats := binding.ReviewerProfiles()
	next, ok := binding.NextReviewerSeat(seats[0])
	if !ok || next.Name != "backup" {
		t.Fatalf("the default seat fails over to %+v, %v; want the counting backup", next, ok)
	}
	if _, ok := binding.NextReviewerSeat(seats[1]); ok {
		t.Fatal("an advisory seat must not fail over to a counting one")
	}
	if _, ok := binding.NextReviewerSeat(seats[2]); ok {
		t.Fatal("the last counting seat has no successor")
	}
	if _, ok := binding.NextReviewerSeat(ReviewerProfile{ReviewerConfig: ReviewerConfig{Name: "ghost"}}); ok {
		t.Fatal("a seat the binding does not hold has no successor")
	}
}

func TestProjectBinding_AgentReferencesListsEverySlot(t *testing.T) {
	binding := seatedBinding()
	binding.Triage = orchestration.PoolRefTo("triagers")
	refs := binding.AgentReferences()
	var slots []string
	for _, ref := range refs {
		if ref.Usage.Kind != orchestration.AgentUsageKindBinding || ref.Usage.ID != testProject {
			t.Fatalf("usage = %+v", ref.Usage)
		}
		slots = append(slots, ref.Usage.Slot+"="+ref.Ref.String())
	}
	want := []string{
		"worker=agent:" + testCoderAgent, "triage=pool:triagers",
		"reviewer:default=agent:" + testReviewerAgent, "reviewer:qwen=agent:" + testQwenAgent,
		"reviewer:backup=agent:" + testReviewerAgent,
	}
	if !slices.Equal(slots, want) {
		t.Fatalf("slots = %v, want %v", slots, want)
	}
	if refs := (&ProjectBinding{ProjectID: "p"}).AgentReferences(); len(refs) != 0 {
		t.Fatalf("an empty binding references nothing, got %v", refs)
	}
}

func TestResolveBindingAgents(t *testing.T) {
	binding := seatedBinding()
	agents, err := resolveBindingAgents(context.Background(), testAgentCatalog(), testTenant, binding)
	if err != nil {
		t.Fatalf("resolveBindingAgents: %v", err)
	}
	if !slices.Equal(agents.authorFamilies, []string{ModelFamilyAnthropic}) || agents.seatFamilies["qwen"] != ModelFamilyQwen {
		t.Fatalf("agents = %+v", agents)
	}
	boom := errors.New("catalog down")
	if _, err := resolveBindingAgents(context.Background(), fixedAgents{err: boom}, testTenant, binding); !errors.Is(err, boom) {
		t.Fatalf("an unreadable catalog = %v", err)
	}
	if _, err := resolveBindingAgents(context.Background(), nil, testTenant, binding); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no catalog = %v", err)
	}
}

func TestTriageAgentPolicy_ForNeedsACatalogOnlyForTheDefault(t *testing.T) {
	binding := &ProjectBinding{Worker: orchestration.AgentRefTo(testCoderAgent)}
	policy := TriageAgentPolicy{Default: orchestration.AgentRefTo(testTriageAgent)}
	got, err := policy.For(context.Background(), testTenant, binding)
	if err != nil || !got.Names(binding.Worker) {
		t.Fatalf("a default with no catalog to judge it by is not taken: %+v, %v", got, err)
	}
}
