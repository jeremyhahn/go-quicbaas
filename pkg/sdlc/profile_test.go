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
	"testing"
)

// aiNativeBinding is testBinding with every link of the artifact chain on.
func aiNativeBinding() *ProjectBinding {
	b := testBinding()
	b.Profile = ProfileAINative
	b.Verification = []string{"make test"}
	b.Reviewer = ReviewerConfig{Agent: testReviewerAgent, Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok"}
	b.Review.RequireIndependentReviewer = true
	b.Review.RequireGreenChecks = true
	b.MergeTrain.Disabled = false
	b.Readiness.RequireAcceptanceCriteria = true
	b.Readiness.SkipDependencies = false
	return b
}

func TestSDLCProfile_StandardValidatesAsBefore(t *testing.T) {
	b := testBinding()
	b.Profile = ProfileStandard
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestSDLCProfile_AnUnknownProfileIsRefused(t *testing.T) {
	b := testBinding()
	b.Profile = "yolo"
	err := b.Validate()
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) || invalid.Field != "profile" {
		t.Fatalf("Validate() = %v, want an InvalidInputError on profile", err)
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("errors.Is(%v, ErrInvalidInput) = false", err)
	}
}

func TestSDLCProfile_AINativeAcceptsACompleteChain(t *testing.T) {
	b := aiNativeBinding()
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	for _, link := range b.ArtifactChain() {
		if !link.Enabled {
			t.Errorf("link %s (%s) is off on a complete ai-native binding", link.Stage, link.Setting)
		}
	}
}

func TestSDLCProfile_AINativeRefusesEachLinkLeftOff(t *testing.T) {
	cases := []struct {
		name  string
		field string
		stage ArtifactStage
		mut   func(*ProjectBinding)
	}{
		{"no verification", "verification", StageVerification, func(b *ProjectBinding) { b.Verification = nil }},
		{"no counting reviewer", "reviewer", StageReview, func(b *ProjectBinding) {
			b.Reviewer = ReviewerConfig{}
			b.Reviewers = nil
		}},
		{"dependent reviewer", "review.require_independent_reviewer", StageReview, func(b *ProjectBinding) { b.Review.RequireIndependentReviewer = false }},
		{"red checks", "review.require_green_checks", StageChecks, func(b *ProjectBinding) { b.Review.RequireGreenChecks = false }},
		{"train off", "merge_train.disabled", StageTrain, func(b *ProjectBinding) { b.MergeTrain.Disabled = true }},
		{"no acceptance criteria", "readiness.require_acceptance_criteria", StageIssue, func(b *ProjectBinding) { b.Readiness.RequireAcceptanceCriteria = false }},
		{"dependencies skipped", "readiness.skip_dependencies", StageIssue, func(b *ProjectBinding) { b.Readiness.SkipDependencies = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := aiNativeBinding()
			tc.mut(b)
			err := b.Validate()
			var conflict *ProfileConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("Validate() = %v, want a ProfileConflictError", err)
			}
			if conflict.Field != tc.field || conflict.Stage != tc.stage || conflict.Profile != ProfileAINative {
				t.Fatalf("conflict = %+v, want field %q stage %q", conflict, tc.field, tc.stage)
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("errors.Is(%v, ErrInvalidInput) = false", err)
			}
		})
	}
}

func TestProjectBinding_ArtifactChainIsOrderedAndNamesItsSettings(t *testing.T) {
	want := []ChainLink{
		{Stage: StageIssue, Setting: "readiness"},
		{Stage: StageBranch, Setting: "strategy"},
		{Stage: StageCommit, Setting: "worker"},
		{Stage: StageVerification, Setting: "verification"},
		{Stage: StagePullRequest, Setting: "strategy"},
		{Stage: StageReview, Setting: "reviewer"},
		{Stage: StageChecks, Setting: "review.require_green_checks"},
		{Stage: StageTrain, Setting: "merge_train"},
		{Stage: StageMerge, Setting: "strategy"},
		{Stage: StageIssueClosed, Setting: "forge"},
	}
	b := aiNativeBinding()
	chain := b.ArtifactChain()
	if len(chain) != len(want) {
		t.Fatalf("chain has %d links, want %d", len(chain), len(want))
	}
	for i, link := range chain {
		if link.Stage != want[i].Stage || link.Setting != want[i].Setting {
			t.Errorf("link %d = %s/%s, want %s/%s", i, link.Stage, link.Setting, want[i].Stage, want[i].Setting)
		}
	}

	b.Review.RequireGreenChecks = false
	b.MergeTrain.Disabled = true
	for _, link := range b.ArtifactChain() {
		if (link.Stage == StageChecks || link.Stage == StageTrain) && link.Enabled {
			t.Errorf("link %s is on after its setting was turned off", link.Stage)
		}
	}

	var nilBinding *ProjectBinding
	if got := nilBinding.ArtifactChain(); got != nil {
		t.Fatalf("nil binding chain = %v, want nil", got)
	}
}
