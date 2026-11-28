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

import "strconv"

// SDLCProfile names the profile a binding is held to.
//
// The standard profile holds a binding to the field rules alone. The
// ai-native profile also refuses a binding that leaves any link of the
// factory's artifact chain off.
type SDLCProfile string

const (
	// ProfileStandard is the empty profile: the field rules alone.
	ProfileStandard SDLCProfile = ""

	// ProfileAINative refuses a binding that leaves any link of the
	// artifact chain off. Under it every change lands through a pull
	// request: a strategy's direct-push carve-out does not apply.
	ProfileAINative SDLCProfile = "ai-native"
)

// ArtifactStage names one link of the factory's artifact chain.
type ArtifactStage string

// The artifact chain, in the order a change passes through it.
const (
	// StageIssue is the issue the work starts from.
	StageIssue ArtifactStage = "issue"
	// StageBranch is the branch the work is cut on.
	StageBranch ArtifactStage = "branch"
	// StageCommit is the worker's signed commit.
	StageCommit ArtifactStage = "commit"
	// StageVerification is the binding's verification commands.
	StageVerification ArtifactStage = "verification"
	// StagePullRequest is the pull request that offers the change.
	StagePullRequest ArtifactStage = "pull_request"
	// StageReview is the review that decides whether it may land.
	StageReview ArtifactStage = "review"
	// StageChecks is the forge's checks on the head.
	StageChecks ArtifactStage = "checks"
	// StageTrain is the merge train the change rides.
	StageTrain ArtifactStage = "train"
	// StageMerge is the merge itself.
	StageMerge ArtifactStage = "merge"
	// StageIssueClosed is the issue closed by the merge.
	StageIssueClosed ArtifactStage = "issue_closed"
)

// ChainLink is one link of a binding's artifact chain: the stage, the
// binding setting that governs it, and whether that setting has it on.
type ChainLink struct {
	Stage   ArtifactStage `json:"stage"`
	Setting string        `json:"setting"`
	Enabled bool          `json:"enabled"`
}

// ArtifactChain returns the binding's artifact chain in stage order. A nil
// binding has none.
func (b *ProjectBinding) ArtifactChain() []ChainLink {
	if b == nil {
		return nil
	}
	_, counting := b.TrainReviewer()
	return []ChainLink{
		{Stage: StageIssue, Setting: "readiness", Enabled: b.Readiness.RequireAcceptanceCriteria && !b.Readiness.SkipDependencies},
		{Stage: StageBranch, Setting: "strategy", Enabled: true},
		{Stage: StageCommit, Setting: "worker", Enabled: true},
		{Stage: StageVerification, Setting: "verification", Enabled: len(b.Verification) > 0},
		{Stage: StagePullRequest, Setting: "strategy", Enabled: true},
		{Stage: StageReview, Setting: "reviewer", Enabled: counting || b.Review.RequiredApprovals > 0},
		{Stage: StageChecks, Setting: "review.require_green_checks", Enabled: b.Review.RequireGreenChecks},
		{Stage: StageTrain, Setting: "merge_train", Enabled: b.TrainEnabled()},
		{Stage: StageMerge, Setting: "strategy", Enabled: true},
		{Stage: StageIssueClosed, Setting: "forge", Enabled: true},
	}
}

// profileRule is one setting the ai-native profile requires.
type profileRule struct {
	stage ArtifactStage
	field string
	want  string
	ok    func(*ProjectBinding) bool
}

// aiNativeRules are the ai-native profile's rules, checked in order; the
// first that fails is the one reported.
var aiNativeRules = []profileRule{
	{StageVerification, "verification", "at least one verification command", func(b *ProjectBinding) bool { return len(b.Verification) > 0 }},
	{StageReview, "reviewer", "a reviewer profile whose verdict counts toward merge", func(b *ProjectBinding) bool { _, ok := b.TrainReviewer(); return ok }},
	{StageReview, "review.require_independent_reviewer", "true", func(b *ProjectBinding) bool { return b.Review.RequireIndependentReviewer }},
	{StageChecks, "review.require_green_checks", "true", func(b *ProjectBinding) bool { return b.Review.RequireGreenChecks }},
	{StageTrain, "merge_train.disabled", "false", func(b *ProjectBinding) bool { return !b.MergeTrain.Disabled }},
	{StageIssue, "readiness.require_acceptance_criteria", "true", func(b *ProjectBinding) bool { return b.Readiness.RequireAcceptanceCriteria }},
	{StageIssue, "readiness.skip_dependencies", "false", func(b *ProjectBinding) bool { return !b.Readiness.SkipDependencies }},
}

// validateProfile refuses an unknown profile, and a binding whose settings
// leave a link of its profile's artifact chain off.
func (b *ProjectBinding) validateProfile() error {
	switch b.Profile {
	case ProfileStandard:
		return nil
	case ProfileAINative:
	default:
		return &InvalidInputError{
			Field:   "profile",
			Message: "unknown profile " + strconv.Quote(string(b.Profile)) + "; the known profiles are \"ai-native\" and the empty standard profile",
		}
	}
	for _, rule := range aiNativeRules {
		if !rule.ok(b) {
			return &ProfileConflictError{Profile: b.Profile, Stage: rule.stage, Field: rule.field, Want: rule.want}
		}
	}
	return nil
}
