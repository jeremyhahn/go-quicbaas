// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// Registry behaviour
// =============================================================================

// TestRegistry_OnlyCompleteStrategiesAreRegistered pins the completeness rule.
//
// Three strategies ship, and each answers all four questions an SDLC is made
// of. The rule this guards is that a half-built strategy is never registered
// under a name and left to fail when selected — a name a project can choose is
// a name that must work. If this test needs updating, the question to answer
// first is whether the new strategy is complete, not whether the assertion is
// inconvenient.
func TestRegistry_OnlyCompleteStrategiesAreRegistered(t *testing.T) {
	want := []string{GitFlowName, GitHubFlowName, TrunkBasedName}
	got := RegisteredStrategies()
	if len(got) != len(want) {
		t.Fatalf("registered strategies = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("registered strategies = %v, want %v (sorted)", got, want)
		}
	}
}

// TestLookupStrategy_ResolvesEveryRegisteredName checks each name reaches the
// strategy that answers to it.
func TestLookupStrategy_ResolvesEveryRegisteredName(t *testing.T) {
	for _, name := range RegisteredStrategies() {
		s, err := LookupStrategy(name)
		if err != nil {
			t.Fatalf("LookupStrategy(%q): %v", name, err)
		}
		if s.Name() != name {
			t.Errorf("strategy registered as %q answers to %q", name, s.Name())
		}
	}
}

// TestLookupStrategy_UnknownNameFailsClosed is the rule that keeps a typo from
// running a project under an SDLC nobody chose. It must be a typed refusal
// naming what IS available, never a fallback.
func TestLookupStrategy_UnknownNameFailsClosed(t *testing.T) {
	for _, name := range []string{"", "nope", "GitHub-Flow", "squash"} {
		s, err := LookupStrategy(name)
		if s != nil {
			t.Fatalf("LookupStrategy(%q) returned a strategy", name)
		}
		if !errors.Is(err, ErrStrategyNotRegistered) {
			t.Fatalf("LookupStrategy(%q) err = %v, want ErrStrategyNotRegistered", name, err)
		}
		var typed *StrategyNotRegisteredError
		if !errors.As(err, &typed) {
			t.Fatalf("LookupStrategy(%q) err is not a *StrategyNotRegisteredError", name)
		}
		if len(typed.Available) != len(RegisteredStrategies()) {
			t.Errorf("refusal for %q lists %v, which is not what is registered", name, typed.Available)
		}
		if !strings.Contains(err.Error(), GitHubFlowName) {
			t.Errorf("refusal %q does not tell the operator what they could have chosen", err.Error())
		}
	}
}

// TestRegisterStrategy_DuplicateNamePanics: two strategies under one name make
// a project's selection depend on registration order, which is not a runtime
// condition to recover from.
func TestRegisterStrategy_DuplicateNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering a duplicate name did not panic")
		}
	}()
	RegisterStrategy(gitHubFlow{})
}

// TestRegisterStrategy_NilPanics guards the same class of programming error.
func TestRegisterStrategy_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering a nil strategy did not panic")
		}
	}()
	RegisterStrategy(nil)
}

// =============================================================================
// BranchPlan.Resolve — the project's default branch is a DEFAULT, not an
// override
// =============================================================================

// TestBranchPlan_ResolveFillsOnlyWhatTheStrategyLeftOpen is the fix this file
// exists for. The old code overwrote Base AND Target with the project's
// default branch whenever it was set, which made every plan single-ref and put
// gitflow out of reach entirely.
func TestBranchPlan_ResolveFillsOnlyWhatTheStrategyLeftOpen(t *testing.T) {
	cases := map[string]struct {
		plan           BranchPlan
		projectDefault string
		wantBase       string
		wantTarget     string
	}{
		"both open take the project default": {
			BranchPlan{Branch: "issue/1-x"}, "trunk", "trunk", "trunk",
		},
		"a decided base survives the project default": {
			BranchPlan{Branch: "feature/1-x", Base: "develop", Target: "develop"}, "main", "develop", "develop",
		},
		"base and target may differ": {
			BranchPlan{Branch: "hotfix/1-x", Base: "release", Target: "main"}, "main", "release", "main",
		},
		"only the open half is filled": {
			BranchPlan{Branch: "x", Base: "develop"}, "main", "develop", "main",
		},
		"no project default falls back": {
			BranchPlan{Branch: "issue/1-x"}, "", FallbackDefaultBranch, FallbackDefaultBranch,
		},
		"a blank project default falls back": {
			BranchPlan{Branch: "issue/1-x"}, "   ", FallbackDefaultBranch, FallbackDefaultBranch,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.plan.Resolve(tc.projectDefault)
			if got.Base != tc.wantBase || got.Target != tc.wantTarget {
				t.Fatalf("Resolve(%q) = base %q target %q, want %q / %q",
					tc.projectDefault, got.Base, got.Target, tc.wantBase, tc.wantTarget)
			}
			if got.Branch != tc.plan.Branch {
				t.Errorf("Resolve changed the branch name to %q", got.Branch)
			}
		})
	}
}

// =============================================================================
// IssueRef label matching
// =============================================================================

// TestIssueRef_LabelMatchingIgnoresCaseAndSpace: change classes are human
// labels on a forge that lets people type them. Refusing "Docs" because the
// strategy was configured with "docs" would send a docs change through a
// review nobody asked for, silently.
func TestIssueRef_LabelMatchingIgnoresCaseAndSpace(t *testing.T) {
	ref := IssueRef{Labels: []string{"Docs", " ci ", "task:ready"}}
	for _, label := range []string{"docs", "DOCS", "ci", "  CI  ", "task:ready"} {
		if !ref.HasLabel(label) {
			t.Errorf("HasLabel(%q) = false on %v", label, ref.Labels)
		}
	}
	for _, label := range []string{"", "   ", "doc", "cicd", "hotfix"} {
		if ref.HasLabel(label) {
			t.Errorf("HasLabel(%q) = true on %v", label, ref.Labels)
		}
	}
	if !ref.HasAnyLabel([]string{"nope", "CI"}) {
		t.Error("HasAnyLabel missed a label present under a different case")
	}
	if ref.HasAnyLabel(nil) || ref.HasAnyLabel([]string{"nope"}) {
		t.Error("HasAnyLabel matched nothing it should have")
	}
}

// =============================================================================
// github-flow
// =============================================================================

// TestGitHubFlow_PlansTheCompanyBranch pins the company's own SDLC
// (handbook conventions/sdlc.md §2): issue/<n>-<slug> off the default branch,
// through a pull request, deleted after merge.
func TestGitHubFlow_PlansTheCompanyBranch(t *testing.T) {
	s, err := LookupStrategy(GitHubFlowName)
	if err != nil {
		t.Fatalf("LookupStrategy: %v", err)
	}
	plan, err := s.PlanBranch(IssueRef{Number: 42, Title: "DAO index honesty"})
	if err != nil {
		t.Fatalf("PlanBranch: %v", err)
	}
	if plan.Branch != "issue/42-dao-index-honesty" {
		t.Errorf("branch = %q", plan.Branch)
	}
	if plan.Base != "" || plan.Target != "" {
		t.Errorf("github-flow named a ref (%q → %q); it has one long-lived branch and the PROJECT names it",
			plan.Base, plan.Target)
	}
	if resolved := plan.Resolve("main"); resolved.Base != "main" || resolved.Target != "main" {
		t.Errorf("resolved = %q → %q, want main → main", resolved.Base, resolved.Target)
	}
	if !plan.RequiresPullRequest {
		t.Error("github-flow reviews every change")
	}
	if !plan.DeleteAfterMerge {
		t.Error("github-flow branches never live past their pull request")
	}
}

// TestGitHubFlow_ReviewsEveryChangeClass: the docs/CI carve-out belongs to
// trunk-based. A github-flow project that labelled an issue "docs" still gets
// a reviewed pull request, because that is what it selected.
func TestGitHubFlow_ReviewsEveryChangeClass(t *testing.T) {
	s, _ := LookupStrategy(GitHubFlowName)
	for _, label := range append([]string{"hotfix"}, DefaultDirectPushLabels...) {
		plan, err := s.PlanBranch(IssueRef{Number: 1, Title: "t", Labels: []string{label}})
		if err != nil {
			t.Fatalf("PlanBranch(%q): %v", label, err)
		}
		if !plan.RequiresPullRequest {
			t.Errorf("label %q skipped review under github-flow", label)
		}
		if !strings.HasPrefix(plan.Branch, "issue/") {
			t.Errorf("label %q changed the branch prefix to %q", label, plan.Branch)
		}
	}
}

// TestGitHubFlow_MergeIsOneSquashedCommitClosingTheIssue: the linkage line is
// what keeps the forge's issue state and the module's record from drifting.
func TestGitHubFlow_MergeIsOneSquashedCommitClosingTheIssue(t *testing.T) {
	s, _ := LookupStrategy(GitHubFlowName)
	spec, err := s.PlanMerge(PullRequestRef{Number: 7, Title: "Fix the thing", IssueNumber: 42, SourceBranch: "issue/42-fix"})
	if err != nil {
		t.Fatalf("PlanMerge: %v", err)
	}
	if spec.Method != MergeMethodSquash {
		t.Errorf("method = %q, want %q", spec.Method, MergeMethodSquash)
	}
	if spec.CommitTitle != "Fix the thing (#7)" {
		t.Errorf("commit title = %q", spec.CommitTitle)
	}
	if spec.CommitMessage != "Closes #42" {
		t.Errorf("commit message = %q", spec.CommitMessage)
	}
	if spec.ExpectSourceDeleted {
		t.Error("a strategy must not decide branch lifecycle in the merge spec; " +
			"BranchPlan.DeleteAfterMerge is the one decision and the caller copies it")
	}
}

// TestGitHubFlow_MergeWithoutAnIssueCarriesNoLinkage: a carve-out change that
// closes no issue is legal and simply has no linkage line.
func TestGitHubFlow_MergeWithoutAnIssueCarriesNoLinkage(t *testing.T) {
	s, _ := LookupStrategy(GitHubFlowName)
	spec, err := s.PlanMerge(PullRequestRef{Number: 7, Title: "Bump", IssueNumber: 0})
	if err != nil {
		t.Fatalf("PlanMerge: %v", err)
	}
	if spec.CommitMessage != "" {
		t.Errorf("commit message = %q, want empty", spec.CommitMessage)
	}
}

// TestGitHubFlow_PostMergeHasNothingToReconcile: one long-lived branch means
// nothing follows a merge.
func TestGitHubFlow_PostMergeHasNothingToReconcile(t *testing.T) {
	s, _ := LookupStrategy(GitHubFlowName)
	steps, err := s.PostMerge(MergedRef{Number: 7, TargetBranch: "main", Labels: []string{"hotfix", "docs"}})
	if err != nil {
		t.Fatalf("PostMerge: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("steps = %v, want none", steps)
	}
}

// =============================================================================
// trunk-based
// =============================================================================

// TestTrunkBased_ReviewsEverythingOutsideTheCarveOut is the default half of
// the strategy: trunk-based is not "push whatever you like to main".
func TestTrunkBased_ReviewsEverythingOutsideTheCarveOut(t *testing.T) {
	s, err := LookupStrategy(TrunkBasedName)
	if err != nil {
		t.Fatalf("LookupStrategy: %v", err)
	}
	plan, err := s.PlanBranch(IssueRef{Number: 42, Title: "Rework the store", Labels: []string{"task:ready"}})
	if err != nil {
		t.Fatalf("PlanBranch: %v", err)
	}
	if plan.Branch != "issue/42-rework-the-store" {
		t.Errorf("branch = %q", plan.Branch)
	}
	if !plan.RequiresPullRequest {
		t.Error("a source change under trunk-based is reviewed")
	}
	if plan.Base != "" || plan.Target != "" {
		t.Errorf("trunk-based named a ref (%q → %q); the trunk is the project's default branch",
			plan.Base, plan.Target)
	}
}

// TestTrunkBased_CarvesOutTheDocumentedChangeClasses covers the direct-push
// path the handbook permits (conventions/sdlc.md §2): documentation and CI
// configuration land without a review round.
func TestTrunkBased_CarvesOutTheDocumentedChangeClasses(t *testing.T) {
	s, _ := LookupStrategy(TrunkBasedName)
	for _, label := range DefaultDirectPushLabels {
		t.Run(label, func(t *testing.T) {
			plan, err := s.PlanBranch(IssueRef{Number: 9, Title: "Document it", Labels: []string{"task:ready", strings.ToUpper(label)}})
			if err != nil {
				t.Fatalf("PlanBranch: %v", err)
			}
			if plan.RequiresPullRequest {
				t.Errorf("label %q did not carve out of review", label)
			}
			if plan.Branch != "issue/9-document-it" {
				t.Errorf("a carve-out still works on its own branch; got %q", plan.Branch)
			}
			if !plan.DeleteAfterMerge {
				t.Error("a carve-out branch is still short-lived")
			}
		})
	}
}

// TestNewTrunkBased_CarveOutIsConfigurable: a build with different change
// classes registers its own strategy rather than mutating the one other
// projects already selected.
func TestNewTrunkBased_CarveOutIsConfigurable(t *testing.T) {
	custom := NewTrunkBased("chore", " ", "release")
	reviewed, err := custom.PlanBranch(IssueRef{Number: 1, Title: "t", Labels: []string{"docs"}})
	if err != nil {
		t.Fatalf("PlanBranch: %v", err)
	}
	if !reviewed.RequiresPullRequest {
		t.Error("a label outside this strategy's carve-out skipped review")
	}
	exempt, err := custom.PlanBranch(IssueRef{Number: 1, Title: "t", Labels: []string{"release"}})
	if err != nil {
		t.Fatalf("PlanBranch: %v", err)
	}
	if exempt.RequiresPullRequest {
		t.Error("a label inside this strategy's carve-out was still reviewed")
	}
	if custom.Name() != TrunkBasedName {
		t.Errorf("Name() = %q", custom.Name())
	}
}

// TestNewTrunkBased_AnEmptyCarveOutReviewsEverything distinguishes "no
// arguments" (take the documented default) from "an explicitly empty set"
// (review everything) — a difference a nil check would silently lose.
func TestNewTrunkBased_AnEmptyCarveOutReviewsEverything(t *testing.T) {
	strict := NewTrunkBased([]string{}...)
	for _, label := range DefaultDirectPushLabels {
		plan, err := strict.PlanBranch(IssueRef{Number: 1, Title: "t", Labels: []string{label}})
		if err != nil {
			t.Fatalf("PlanBranch: %v", err)
		}
		if !plan.RequiresPullRequest {
			t.Errorf("label %q carved out of a strategy configured with no carve-out", label)
		}
	}
}

// TestTrunkBased_MergeAndPostMergeMatchGitHubFlow: a reviewed trunk-based
// change lands exactly as a github-flow one, and nothing follows it.
func TestTrunkBased_MergeAndPostMergeMatchGitHubFlow(t *testing.T) {
	s, _ := LookupStrategy(TrunkBasedName)
	spec, err := s.PlanMerge(PullRequestRef{Number: 3, Title: "Rework", IssueNumber: 42})
	if err != nil {
		t.Fatalf("PlanMerge: %v", err)
	}
	if spec.Method != MergeMethodSquash || spec.CommitTitle != "Rework (#3)" || spec.CommitMessage != "Closes #42" {
		t.Errorf("spec = %+v", spec)
	}
	steps, err := s.PostMerge(MergedRef{Number: 3, TargetBranch: "main"})
	if err != nil || len(steps) != 0 {
		t.Errorf("PostMerge = %v, %v; want no steps", steps, err)
	}
}

// =============================================================================
// gitflow
// =============================================================================

// TestGitFlow_FeatureCutsFromDevelopAndLandsThere is the case the old
// branching/merge split could not express at all: a base that is NOT the
// project's default branch.
func TestGitFlow_FeatureCutsFromDevelopAndLandsThere(t *testing.T) {
	s, err := LookupStrategy(GitFlowName)
	if err != nil {
		t.Fatalf("LookupStrategy: %v", err)
	}
	plan, err := s.PlanBranch(IssueRef{Number: 42, Title: "Add the widget", Labels: []string{"task:ready"}})
	if err != nil {
		t.Fatalf("PlanBranch: %v", err)
	}
	if plan.Branch != "feature/42-add-the-widget" {
		t.Errorf("branch = %q", plan.Branch)
	}
	if plan.Base != GitFlowDevelopBranch || plan.Target != GitFlowDevelopBranch {
		t.Errorf("feature plan = %q → %q, want develop → develop", plan.Base, plan.Target)
	}
	// The project's default branch is the RELEASE line and must not touch a
	// plan that named its own refs.
	resolved := plan.Resolve("main")
	if resolved.Base != GitFlowDevelopBranch || resolved.Target != GitFlowDevelopBranch {
		t.Errorf("the project default overwrote the strategy: %q → %q", resolved.Base, resolved.Target)
	}
	if !plan.RequiresPullRequest || !plan.DeleteAfterMerge {
		t.Errorf("plan = %+v", plan)
	}
}

// TestGitFlow_HotfixCutsFromTheReleaseLine: base and target are the project's
// default branch, left open so only the project names it.
func TestGitFlow_HotfixCutsFromTheReleaseLine(t *testing.T) {
	s, _ := LookupStrategy(GitFlowName)
	plan, err := s.PlanBranch(IssueRef{Number: 43, Title: "Patch the leak", Labels: []string{"task:ready", "HotFix"}})
	if err != nil {
		t.Fatalf("PlanBranch: %v", err)
	}
	if plan.Branch != "hotfix/43-patch-the-leak" {
		t.Errorf("branch = %q", plan.Branch)
	}
	if plan.Base != "" || plan.Target != "" {
		t.Errorf("hotfix plan named a ref (%q → %q); the release line is the project's", plan.Base, plan.Target)
	}
	resolved := plan.Resolve("release-1.x")
	if resolved.Base != "release-1.x" || resolved.Target != "release-1.x" {
		t.Errorf("resolved = %q → %q", resolved.Base, resolved.Target)
	}
	if !plan.RequiresPullRequest {
		t.Error("a hotfix on the release line is still reviewed")
	}
}

// TestGitFlow_MergeIsAMergeCommit: gitflow's history is its record of what
// landed together, and a squash would erase it.
func TestGitFlow_MergeIsAMergeCommit(t *testing.T) {
	s, _ := LookupStrategy(GitFlowName)
	spec, err := s.PlanMerge(PullRequestRef{Number: 5, Title: "Add the widget", IssueNumber: 42, SourceBranch: "feature/42-add-the-widget"})
	if err != nil {
		t.Fatalf("PlanMerge: %v", err)
	}
	if spec.Method != MergeMethodMerge {
		t.Errorf("method = %q, want %q", spec.Method, MergeMethodMerge)
	}
	if spec.CommitTitle != "Merge feature/42-add-the-widget (#5)" {
		t.Errorf("commit title = %q", spec.CommitTitle)
	}
	if spec.CommitMessage != "Closes #42" {
		t.Errorf("commit message = %q", spec.CommitMessage)
	}
}

// TestGitFlow_HotfixBackMergesIntoDevelop is the whole reason PostMerge
// exists: a fix landed on the release line and not carried into develop is a
// bug the next release rebuilds.
func TestGitFlow_HotfixBackMergesIntoDevelop(t *testing.T) {
	s, _ := LookupStrategy(GitFlowName)
	steps, err := s.PostMerge(MergedRef{
		Number: 5, Title: "Patch the leak", IssueNumber: 43,
		SourceBranch: "hotfix/43-patch-the-leak", TargetBranch: "main", Labels: []string{"hotfix"},
	})
	if err != nil {
		t.Fatalf("PostMerge: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %v, want exactly one back-merge", steps)
	}
	step := steps[0]
	if step.Kind != StepBackMerge {
		t.Errorf("kind = %q, want %q", step.Kind, StepBackMerge)
	}
	if step.Source != "main" || step.Target != GitFlowDevelopBranch {
		t.Errorf("step = %q → %q, want main → develop", step.Source, step.Target)
	}
	if !strings.Contains(step.Title, "#5") || !strings.Contains(step.Body, "#5") {
		t.Errorf("the back-merge does not name the hotfix it carries: %+v", step)
	}
}

// TestGitFlow_FeatureMergeHasNoBackMerge: a feature landed on develop is
// already where develop wants it.
func TestGitFlow_FeatureMergeHasNoBackMerge(t *testing.T) {
	s, _ := LookupStrategy(GitFlowName)
	steps, err := s.PostMerge(MergedRef{Number: 5, TargetBranch: GitFlowDevelopBranch, Labels: []string{"task:ready"}})
	if err != nil {
		t.Fatalf("PostMerge: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("steps = %v, want none", steps)
	}
}

// TestGitFlow_HotfixThatLandedOnDevelopNeedsNoBackMerge covers the degenerate
// project whose default branch IS develop: back-merging a branch into itself
// would open a pull request with nothing in it.
func TestGitFlow_HotfixThatLandedOnDevelopNeedsNoBackMerge(t *testing.T) {
	s, _ := LookupStrategy(GitFlowName)
	steps, err := s.PostMerge(MergedRef{Number: 5, TargetBranch: GitFlowDevelopBranch, Labels: []string{"hotfix"}})
	if err != nil {
		t.Fatalf("PostMerge: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("steps = %v, want none", steps)
	}
}

// TestGitFlow_HotfixWithoutATargetIsRefused: PostMerge is told where the
// change landed. Guessing "main" would back-merge from a branch the release
// may not even be on.
func TestGitFlow_HotfixWithoutATargetIsRefused(t *testing.T) {
	s, _ := LookupStrategy(GitFlowName)
	for _, target := range []string{"", "   "} {
		steps, err := s.PostMerge(MergedRef{Number: 5, TargetBranch: target, Labels: []string{"hotfix"}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("PostMerge(target=%q) err = %v, want ErrInvalidInput", target, err)
		}
		if steps != nil {
			t.Errorf("PostMerge(target=%q) returned steps alongside its refusal", target)
		}
	}
}

// =============================================================================
// Refusals shared by every strategy
// =============================================================================

// TestEveryStrategy_RefusesAnIssueItCannotName: a branch is named after the
// issue it closes, so an unnumbered issue has no branch. Every registered
// strategy must refuse it the same way.
func TestEveryStrategy_RefusesAnIssueItCannotName(t *testing.T) {
	for _, name := range RegisteredStrategies() {
		s, _ := LookupStrategy(name)
		for _, number := range []int{0, -1} {
			plan, err := s.PlanBranch(IssueRef{Number: number, Title: "x"})
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("%s.PlanBranch(#%d) err = %v, want ErrInvalidInput", name, number, err)
			}
			if plan.Branch != "" {
				t.Errorf("%s.PlanBranch(#%d) returned a branch alongside its refusal: %q", name, number, plan.Branch)
			}
		}
	}
}

// TestGitFlow_RefusesAHotfixItCannotName covers the label-routed half of the
// same rule: gitflow picks a different prefix for a hotfix, and that path has
// to refuse an unnumbered issue too.
func TestGitFlow_RefusesAHotfixItCannotName(t *testing.T) {
	s, _ := LookupStrategy(GitFlowName)
	plan, err := s.PlanBranch(IssueRef{Number: 0, Title: "x", Labels: []string{"hotfix"}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("PlanBranch err = %v, want ErrInvalidInput", err)
	}
	if plan.Branch != "" {
		t.Errorf("PlanBranch returned a branch alongside its refusal: %q", plan.Branch)
	}
}

// TestEveryStrategy_RefusesAPullRequestItCannotLand covers the merge side of
// the same rule.
func TestEveryStrategy_RefusesAPullRequestItCannotLand(t *testing.T) {
	cases := []struct {
		name string
		pr   PullRequestRef
	}{
		{"no number", PullRequestRef{Number: 0, Title: "x"}},
		{"negative number", PullRequestRef{Number: -1, Title: "x"}},
		{"no title", PullRequestRef{Number: 1, Title: ""}},
		{"blank title", PullRequestRef{Number: 1, Title: "   "}},
	}
	for _, strategyName := range RegisteredStrategies() {
		s, _ := LookupStrategy(strategyName)
		for _, tc := range cases {
			t.Run(strategyName+"/"+tc.name, func(t *testing.T) {
				spec, err := s.PlanMerge(tc.pr)
				if !errors.Is(err, ErrInvalidInput) {
					t.Errorf("PlanMerge err = %v, want ErrInvalidInput", err)
				}
				if spec.Method != "" {
					t.Errorf("PlanMerge returned a spec alongside its refusal: %+v", spec)
				}
			})
		}
	}
}

// =============================================================================
// Slug derivation
// =============================================================================

// TestSlugify_ProducesRefSafeSegments is the property that matters: whatever an
// issue title contains, the slug must be safe in a ref name, a path and a URL
// without escaping.
//
// Issue titles are arbitrary human text — punctuation, emoji, other scripts,
// leading and trailing junk — and a slug that let any of it through would
// produce a branch the forge refuses to create, failing the issue for a
// reason that has nothing to do with the work.
func TestSlugify_ProducesRefSafeSegments(t *testing.T) {
	titles := []string{
		"DAO index honesty",
		"fix: QueryIndex is a silent no-op!!",
		"  leading and trailing  ",
		"Multiple   internal   spaces",
		"UPPER Case Title",
		"emoji 🎉 and ünïcödé",
		"slashes/and\\backslashes",
		"...",
		"",
		strings.Repeat("very long title ", 20),
	}

	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			slug := Slugify(title)
			if slug == "" {
				t.Fatal("a slug must never be empty — it would leave a trailing hyphen in the branch")
			}
			if len(slug) > maxSlugLen {
				t.Errorf("slug %q exceeds the bound (%d)", slug, maxSlugLen)
			}
			for _, r := range slug {
				isLower := r >= 'a' && r <= 'z'
				isDigit := r >= '0' && r <= '9'
				if !isLower && !isDigit && r != '-' {
					t.Errorf("slug %q contains %q, which is not ref-safe", slug, r)
				}
			}
			if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
				t.Errorf("slug %q has a bare hyphen at an edge", slug)
			}
			if strings.Contains(slug, "--") {
				t.Errorf("slug %q has a doubled hyphen", slug)
			}
		})
	}
}

// TestSlugify_UnnameableTitlesStillProduceABranch covers the degenerate inputs
// specifically: a title that slugifies to nothing must still yield a usable
// branch rather than one ending in a hyphen.
func TestSlugify_UnnameableTitlesStillProduceABranch(t *testing.T) {
	for _, title := range []string{"", "...", "!!!", "   ", "🎉"} {
		if got := Slugify(title); got != "untitled" {
			t.Errorf("Slugify(%q) = %q, want untitled", title, got)
		}
	}
}

// TestSlugify_TruncationDoesNotStrandAHyphen covers the interaction between the
// length bound and the hyphen rule — truncating mid-word can leave a trailing
// hyphen, which the trim after truncation exists to remove.
func TestSlugify_TruncationDoesNotStrandAHyphen(t *testing.T) {
	// Engineered so the cut lands exactly on a hyphen.
	title := strings.Repeat("ab ", maxSlugLen)
	slug := Slugify(title)
	if strings.HasSuffix(slug, "-") {
		t.Errorf("truncated slug %q ends in a hyphen", slug)
	}
}

// TestGitHubFlow_BranchStaysRefSafeForAnyTitle joins the two: whatever the
// title, the planned branch must be a legal ref.
func TestGitHubFlow_BranchStaysRefSafeForAnyTitle(t *testing.T) {
	s, _ := LookupStrategy(GitHubFlowName)
	for _, title := range []string{"", "...", "a/b", strings.Repeat("x ", 100), "🎉"} {
		plan, err := s.PlanBranch(IssueRef{Number: 7, Title: title})
		if err != nil {
			t.Fatalf("PlanBranch(%q): %v", title, err)
		}
		if !strings.HasPrefix(plan.Branch, "issue/7-") {
			t.Errorf("branch %q lost its issue prefix", plan.Branch)
		}
		// Exactly one separator: the one this strategy puts there.
		if strings.Count(plan.Branch, "/") != 1 {
			t.Errorf("branch %q has an unexpected separator — a title must not be able "+
				"to add path segments to a ref", plan.Branch)
		}
		if strings.HasSuffix(plan.Branch, "-") {
			t.Errorf("branch %q ends in a bare hyphen", plan.Branch)
		}
	}
}
