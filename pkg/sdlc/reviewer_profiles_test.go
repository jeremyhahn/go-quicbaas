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
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Reviewer profiles: a default, named alternates, and the merge rule
// =============================================================================

const (
	qwenProfile = "qwen"
	qwenLogin   = "qwen-reviewer"
)

// qwenAlternate is the advisory alternate every profile test binds: the
// owner's reference profile, on the local model.
func qwenAlternate() ReviewerConfig {
	return ReviewerConfig{
		Name: qwenProfile, Agent: testQwenAgent,
		Login: qwenLogin, TokenSecretRef: "sdlc/reviewer/api/qwen-token",
	}
}

// profiledBinding is testBinding with a codex default and the qwen alternate.
func profiledBinding() *ProjectBinding {
	binding := testBinding()
	binding.Reviewer = ReviewerConfig{Agent: testReviewerAgent, Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok"}
	binding.Reviewers = []ReviewerConfig{qwenAlternate()}
	return binding
}

func boolPtr(b bool) *bool { return &b }

func TestProjectBinding_ReviewerProfilesResolveNameRoleAndContext(t *testing.T) {
	binding := profiledBinding()
	if err := binding.Validate(); err != nil {
		t.Fatalf("a default plus an advisory alternate is a valid binding: %v", err)
	}
	profiles := binding.ReviewerProfiles()
	if len(profiles) != 2 {
		t.Fatalf("profiles = %+v, want the default and one alternate", profiles)
	}
	def, qwen := profiles[0], profiles[1]
	// THE DEFAULT COUNTS UNLESS IT SAYS OTHERWISE, which is what every binding
	// written before profiles existed meant.
	if !def.Default || !def.Counts || def.Name != DefaultReviewerProfileName || def.Context() != DefaultReviewStatusContext {
		t.Fatalf("default = %+v", def)
	}
	// AN ALTERNATE IS ADVISORY UNLESS IT SAYS OTHERWISE, and publishes under a
	// context of its own so it can never satisfy the required one.
	if qwen.Default || qwen.Counts || !qwen.Advisory() || qwen.Context() != DefaultReviewStatusContext+"/"+qwenProfile {
		t.Fatalf("alternate = %+v", qwen)
	}

	// Switched on, an alternate publishes under the ONE required context.
	binding.Reviewers[0].CountsTowardMerge = boolPtr(true)
	counted, err := binding.ReviewerProfile(qwenProfile)
	if err != nil {
		t.Fatal(err)
	}
	if !counted.Counts || counted.Context() != binding.RequiredReviewContext() {
		t.Fatalf("a counting alternate = %+v, want the required context %q", counted, binding.RequiredReviewContext())
	}
}

func TestProjectBinding_ReviewerProfileRefusesANameItDoesNotCarry(t *testing.T) {
	binding := profiledBinding()
	if profile, err := binding.ReviewerProfile(""); err != nil || !profile.Default {
		t.Fatalf("an empty name is the default: %+v, %v", profile, err)
	}
	if profile, err := binding.ReviewerProfile(" QWEN "); err != nil || profile.Login != qwenLogin {
		t.Fatalf("names are matched trimmed and case-insensitively: %+v, %v", profile, err)
	}
	_, err := binding.ReviewerProfile("gone")
	var missing *ReviewerProfileNotFoundError
	if !errors.As(err, &missing) || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unknown profile must be refused, never read as the default: %v", err)
	}
}

func TestProjectBinding_ValidateRefusesProfilesThatCannotBeToldApart(t *testing.T) {
	cases := map[string]func(b *ProjectBinding){
		"an alternate with no name": func(b *ProjectBinding) { b.Reviewers[0].Name = "" },
		"a name outside the vocabulary": func(b *ProjectBinding) {
			b.Reviewers[0].Name = "Qwen Reviewer"
		},
		"an alternate with no agent": func(b *ProjectBinding) { b.Reviewers[0].Agent = "" },
		"an alternate with no login": func(b *ProjectBinding) { b.Reviewers[0].Login = "" },
		"an alternate with no token": func(b *ProjectBinding) { b.Reviewers[0].TokenSecretRef = "" },
		"an alternate with a malformed agent": func(b *ProjectBinding) {
			b.Reviewers[0].Agent = "Qwen Reviewer"
		},
		"alternates with no default": func(b *ProjectBinding) { b.Reviewer = ReviewerConfig{} },
		"a name the default holds": func(b *ProjectBinding) {
			b.Reviewers[0].Name = DefaultReviewerProfileName
		},
		"two alternates with one name": func(b *ProjectBinding) {
			second := qwenAlternate()
			second.Login, second.TokenSecretRef = "other", "other-tok"
			b.Reviewers = append(b.Reviewers, second)
		},
		"a login the default holds": func(b *ProjectBinding) {
			b.Reviewers[0].Login = strings.ToUpper(fakeReviewerLogin)
		},
		"a token the default holds": func(b *ProjectBinding) { b.Reviewers[0].TokenSecretRef = "reviewer-tok" },
		"an advisory profile on the required context": func(b *ProjectBinding) {
			b.Reviewers[0].StatusContext = DefaultReviewStatusContext
		},
		"a counting profile off the required context": func(b *ProjectBinding) {
			b.Reviewers[0].CountsTowardMerge = boolPtr(true)
			b.Reviewers[0].StatusContext = "somewhere/else"
		},
		"two advisory profiles on one context": func(b *ProjectBinding) {
			second := qwenAlternate()
			second.Name, second.Login, second.TokenSecretRef = "qwen-small", "qwen-small", "qwen-small-tok"
			second.StatusContext = DefaultReviewStatusContext + "/" + qwenProfile
			b.Reviewers = append(b.Reviewers, second)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			binding := profiledBinding()
			mutate(binding)
			if err := binding.Validate(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want an invalid-input refusal, got %v", err)
			}
		})
	}
}

func TestProjectBinding_AnAdvisoryDefaultLeavesTheRequiredContextAlone(t *testing.T) {
	binding := profiledBinding()
	binding.Reviewer.CountsTowardMerge = boolPtr(false)
	binding.Reviewers[0].CountsTowardMerge = boolPtr(true)
	if err := binding.Validate(); err != nil {
		t.Fatalf("an advisory default beside a counting alternate is valid: %v", err)
	}
	if binding.RequiredReviewContext() != DefaultReviewStatusContext {
		t.Fatalf("required context = %q", binding.RequiredReviewContext())
	}
	def := binding.DefaultReviewerProfile()
	if def.Counts || def.Context() != DefaultReviewStatusContext+"/"+DefaultReviewerProfileName {
		t.Fatalf("an advisory default = %+v", def)
	}
	contexts := binding.ReviewerStatusContexts()
	if !slices.Contains(contexts, DefaultReviewStatusContext) || !slices.Contains(contexts, def.Context()) {
		t.Fatalf("status contexts = %v", contexts)
	}
}

func TestProjectBinding_ReviewPolicyAttributesEveryProfilesFamily(t *testing.T) {
	binding := profiledBinding()
	agents := bindingAgentsFrom(testAgents(), binding)
	policy := binding.reviewPolicy(agents)
	if policy.ReviewerModelFamily(qwenLogin) != ModelFamilyQwen {
		t.Fatalf("the qwen login was not attributed: %v", policy.Reviewers)
	}
	// A PROFILE'S DERIVED FAMILY WINS over a stated one: what the reviewer
	// runs decides its family, not what it is called
	// (TestProjectBinding_AFamilyOverrideCannotLaunderTheAuthorsFamily).
	binding.Review.Reviewers[qwenLogin] = "stated"
	if got := binding.reviewPolicy(agents).ReviewerModelFamily(qwenLogin); got != ModelFamilyQwen {
		t.Fatalf("family = %q, want the derived qwen", got)
	}
	// And the binding's own policy is not written through.
	if _, leaked := testBinding().Review.Reviewers[qwenLogin]; leaked {
		t.Fatal("reviewPolicy mutated a shared map")
	}
}

// TestProjectBinding_ReviewersSurviveTheStore proves a bind round-trips the
// alternates, which is the first half of the bug class the memory note
// records: a field the record does not carry is a field a rebind erases.
func TestProjectBinding_ReviewersSurviveTheStore(t *testing.T) {
	store := newTestStore(t)
	binding := profiledBinding()
	binding.Reviewers[0].StatusContext = DefaultReviewStatusContext + "/" + qwenProfile
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetBinding(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Reviewers) != 1 || got.Reviewers[0] != binding.Reviewers[0] {
		t.Fatalf("reviewers = %+v, want %+v", got.Reviewers, binding.Reviewers)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"reviewers":[{"name":"qwen"`) {
		t.Fatalf("the wire shape does not carry reviewers: %s", raw)
	}
}

// =============================================================================
// The override record and the selection
// =============================================================================

func TestStore_ReviewerOverrideLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if _, err := store.GetReviewerOverride(ctx, testTenant, testProject, 41); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no override is not-found: %v", err)
	}
	override := &ReviewerOverride{ProjectID: testProject, PullRequest: 41, Profile: qwenProfile}
	if err := store.PutReviewerOverride(ctx, testTenant, override); err != nil {
		t.Fatal(err)
	}
	created := override.CreatedAt
	got, err := store.GetReviewerOverride(ctx, testTenant, testProject, 41)
	if err != nil || got.Profile != qwenProfile {
		t.Fatalf("override = %+v, %v", got, err)
	}
	// A different pull request of the same project is a different record.
	if _, err := store.GetReviewerOverride(ctx, testTenant, testProject, 42); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an override leaked to another pull request: %v", err)
	}
	// And another tenant cannot see it.
	if _, err := store.GetReviewerOverride(ctx, "tenant-b", testProject, 41); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an override leaked across tenants: %v", err)
	}
	// Replacing keeps the creation time.
	override.Profile = DefaultReviewerProfileName
	if err := store.PutReviewerOverride(ctx, testTenant, override); err != nil {
		t.Fatal(err)
	}
	if !override.CreatedAt.Equal(created) {
		t.Fatalf("a replaced override lost its creation time: %v != %v", override.CreatedAt, created)
	}
	if err := store.DeleteReviewerOverride(ctx, testTenant, testProject, 41); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteReviewerOverride(ctx, testTenant, testProject, 41); err != nil {
		t.Fatalf("clearing an absent override is not an error: %v", err)
	}
	if _, err := store.GetReviewerOverride(ctx, testTenant, testProject, 41); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a cleared override is still there: %v", err)
	}
}

func TestStore_PutReviewerOverrideRefusesAnIncompleteRecord(t *testing.T) {
	store := newTestStore(t)
	for name, override := range map[string]*ReviewerOverride{
		"no project":      {PullRequest: 41, Profile: qwenProfile},
		"no pull request": {ProjectID: testProject, Profile: qwenProfile},
		"no profile":      {ProjectID: testProject, PullRequest: 41, Profile: " "},
		"a nil record":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			err := store.PutReviewerOverride(context.Background(), testTenant, override)
			if !errors.Is(err, ErrInvalidInput) && !errors.Is(err, ErrInvalidKeySegment) {
				t.Fatalf("an incomplete override was stored: %v", err)
			}
		})
	}
}

func TestStore_DeleteReviewerOverridesClearsOneProjectOnly(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	for _, override := range []*ReviewerOverride{
		{ProjectID: testProject, PullRequest: 1, Profile: qwenProfile},
		{ProjectID: testProject, PullRequest: 2, Profile: qwenProfile},
		{ProjectID: "proj-2", PullRequest: 1, Profile: qwenProfile},
	} {
		if err := store.PutReviewerOverride(ctx, testTenant, override); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteReviewerOverrides(ctx, testTenant, testProject); err != nil {
		t.Fatal(err)
	}
	for _, pr := range []int{1, 2} {
		if _, err := store.GetReviewerOverride(ctx, testTenant, testProject, pr); !errors.Is(err, ErrNotFound) {
			t.Fatalf("override #%d survived its project's clear: %v", pr, err)
		}
	}
	if _, err := store.GetReviewerOverride(ctx, testTenant, "proj-2", 1); err != nil {
		t.Fatalf("another project's override was cleared: %v", err)
	}
	if err := store.DeleteReviewerOverrides(ctx, testTenant, "a/b"); !errors.Is(err, ErrInvalidKeySegment) {
		t.Fatalf("a malformed project id was accepted: %v", err)
	}
}

func TestSelectReviewer_TheOverrideElseTheDefault(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	binding := profiledBinding()

	profile, selection, err := SelectReviewer(ctx, store, testTenant, binding, 41)
	if err != nil || !profile.Default || selection.Overridden || selection.Profile != DefaultReviewerProfileName ||
		!selection.CountsTowardMerge || selection.StatusContext != DefaultReviewStatusContext {
		t.Fatalf("with no override: %+v %+v %v", profile, selection, err)
	}

	if err := store.PutReviewerOverride(ctx, testTenant, &ReviewerOverride{
		ProjectID: testProject, PullRequest: 41, Profile: qwenProfile,
	}); err != nil {
		t.Fatal(err)
	}
	profile, selection, err = SelectReviewer(ctx, store, testTenant, binding, 41)
	if err != nil || profile.Login != qwenLogin || !selection.Overridden || selection.CountsTowardMerge ||
		selection.UpdatedAt.IsZero() {
		t.Fatalf("with an override: %+v %+v %v", profile, selection, err)
	}

	// A PROFILE REMOVED AFTER THE OVERRIDE WAS SET IS REFUSED, not quietly
	// replaced by the default.
	binding.Reviewers = nil
	if _, _, err := SelectReviewer(ctx, store, testTenant, binding, 41); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an override naming a removed profile was honoured: %v", err)
	}
}

func TestSelectReviewer_AStoreFailureIsNotTheDefault(t *testing.T) {
	down := errors.New("store down")
	_, _, err := SelectReviewer(context.Background(), failingOverrides{err: down}, testTenant, profiledBinding(), 41)
	if !errors.Is(err, down) {
		t.Fatalf("an unreadable override was read as none: %v", err)
	}
}

// failingOverrides is an override store that cannot be read.
type failingOverrides struct{ err error }

func (f failingOverrides) GetReviewerOverride(context.Context, string, string, int) (*ReviewerOverride, error) {
	return nil, f.err
}

// =============================================================================
// The gate reads past an advisory reviewer
// =============================================================================

func TestEvaluateReviewGate_AnAdvisoryVerdictNeitherApprovesNorBlocks(t *testing.T) {
	binding := profiledBinding()
	advisory := binding.AdvisoryReviewers()
	base := GateInput{
		Policy:              ReviewPolicy{RequiredApprovals: 1, RequireGreenChecks: true},
		PullRequestState:    PullRequestOpen,
		HeadSHA:             "h",
		AuthorModelFamilies: []string{ModelFamilyAnthropic},
		Checks:              []CheckRecord{{Name: "ci", HeadSHA: "h", Status: CheckSuccess}},
		Advisory:            advisory,
	}

	approvedByQwen := base
	approvedByQwen.Reviews = []ReviewRecord{{Reviewer: qwenLogin, Verdict: VerdictApprove, HeadSHA: "h", ModelFamily: ModelFamilyQwen}}
	if got := EvaluateReviewGate(approvedByQwen); got.Reason != GateBlockedInsufficientApprovals {
		t.Fatalf("an advisory approval cleared the gate: %+v", got)
	}

	refusedByQwen := base
	refusedByQwen.Reviews = []ReviewRecord{
		{Reviewer: qwenLogin, Verdict: VerdictRequestChanges, HeadSHA: "h"},
		{Reviewer: fakeReviewerLogin, Verdict: VerdictApprove, HeadSHA: "h", ModelFamily: ModelFamilyOpenAI},
	}
	refusedByQwen.Checks = append(refusedByQwen.Checks, CheckRecord{
		Name: DefaultReviewStatusContext + "/" + qwenProfile, HeadSHA: "h", Status: CheckFailure,
	})
	if got := EvaluateReviewGate(refusedByQwen); !got.Mergeable() || got.Approvals != 1 {
		t.Fatalf("an advisory refusal or its red status blocked a change the default approved: %+v", got)
	}

	// Without the advisory set the same records DO block: the filter is what
	// changed the answer, not the fixture.
	unfiltered := refusedByQwen
	unfiltered.Advisory = AdvisoryReviewers{}
	if got := EvaluateReviewGate(unfiltered); got.Mergeable() {
		t.Fatalf("the fixture clears even without the advisory set: %+v", got)
	}
}

// =============================================================================
// Dispatch, the sweep and the runner run the chosen profile
// =============================================================================

func TestReviewDispatcher_TheOverrideChoosesTheProfileTheJobRunsAs(t *testing.T) {
	rig := newDispatchRig(t)
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	ctx := context.Background()
	binding, err := rig.store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	binding.Reviewers = []ReviewerConfig{qwenAlternate()}
	if err := rig.store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatal(err)
	}

	if _, err := rig.dispatcher.TriggerReview(ctx, prRequest(41)); err != nil {
		t.Fatal(err)
	}
	if err := rig.store.PutReviewerOverride(ctx, testTenant, &ReviewerOverride{
		ProjectID: testProject, PullRequest: 41, Profile: qwenProfile,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.dispatcher.TriggerReview(ctx, prRequest(41)); err != nil {
		t.Fatal(err)
	}

	reqs := rig.jobs.requests()
	if len(reqs) != 2 {
		t.Fatalf("started %d jobs, want 2", len(reqs))
	}
	first, err := ReviewJobSubjectFrom(&orchestration.JobConfig{Params: reqs[0].Params})
	if err != nil || first.Profile != DefaultReviewerProfileName || reqs[0].Lead.AgentID != testReviewerAgent {
		t.Fatalf("with no override the default reviews: %+v %+v %v", reqs[0], first, err)
	}
	second, err := ReviewJobSubjectFrom(&orchestration.JobConfig{Params: reqs[1].Params})
	if err != nil || second.Profile != qwenProfile || reqs[1].Lead.AgentID != testQwenAgent {
		t.Fatalf("the override did not choose the reviewer: %+v %+v %v", reqs[1], second, err)
	}
}

func TestReviewDispatcher_AnOverrideNamingARemovedProfileStartsNothing(t *testing.T) {
	rig := newDispatchRig(t)
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	ctx := context.Background()
	if err := rig.store.PutReviewerOverride(ctx, testTenant, &ReviewerOverride{
		ProjectID: testProject, PullRequest: 41, Profile: "gone",
	}); err != nil {
		t.Fatal(err)
	}
	started, err := rig.dispatcher.TriggerReview(ctx, prRequest(41))
	var missing *ReviewerProfileNotFoundError
	if started || !errors.As(err, &missing) {
		t.Fatalf("started=%v err=%v, want the removed profile refused", started, err)
	}
	if len(rig.jobs.requests()) != 0 {
		t.Fatal("a job was started for a reviewer the binding no longer has")
	}
}

// TestReviewDispatcher_AdvisoryStatusesDoNotHoldTheHeadBack proves an advisory
// reviewer's red status does not make a green pipeline read as red.
func TestReviewDispatcher_AdvisoryStatusesDoNotHoldTheHeadBack(t *testing.T) {
	rig := newDispatchRig(t)
	ctx := context.Background()
	binding, err := rig.store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	binding.Reviewers = []ReviewerConfig{qwenAlternate()}
	if err := rig.store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatal(err)
	}
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead), redRun(DefaultReviewStatusContext+"/"+qwenProfile, dispatchHead))
	started, err := rig.dispatcher.TriggerReview(ctx, prRequest(41))
	if err != nil || !started {
		t.Fatalf("started=%v err=%v; an advisory red must not read as the pipeline's", started, err)
	}
}

func TestReviewSweeper_JudgedMeansJudgedByAVerdictThatAnswersTheChosenReviewer(t *testing.T) {
	cases := map[string]struct {
		override string
		reviews  []ForgeReview
		want     int
	}{
		"the default is answered by a counting approval": {
			reviews: []ForgeReview{judgedBy("APPROVED")}, want: 0,
		},
		"the default is NOT answered by an advisory approval": {
			reviews: []ForgeReview{{ID: 8, Reviewer: qwenLogin, State: "APPROVED", HeadSHA: sweepHead}}, want: 1,
		},
		"an advisory override is not answered by the default's approval": {
			override: qwenProfile, reviews: []ForgeReview{judgedBy("APPROVED")}, want: 1,
		},
		"an advisory override is answered by its own verdict": {
			override: qwenProfile,
			reviews:  []ForgeReview{{ID: 8, Reviewer: qwenLogin, State: "REQUEST_CHANGES", HeadSHA: sweepHead}},
			want:     0,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newSweepRig(t)
			rig.rebind(t, func(b *ProjectBinding) { b.Reviewers = []ReviewerConfig{qwenAlternate()} })
			if tc.override != "" {
				if err := rig.store.PutReviewerOverride(context.Background(), testTenant, &ReviewerOverride{
					ProjectID: testProject, PullRequest: 41, Profile: tc.override,
				}); err != nil {
					t.Fatal(err)
				}
			}
			rig.setReviews(tc.reviews...)
			started, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
			if err != nil {
				t.Fatalf("SweepProject: %v", err)
			}
			if started != tc.want {
				t.Fatalf("started %d reviews, want %d", started, tc.want)
			}
		})
	}
}

func TestReviewSweeper_AReviewInFlightIsTheChosenReviewers(t *testing.T) {
	rig := newSweepRig(t)
	rig.rebind(t, func(b *ProjectBinding) { b.Reviewers = []ReviewerConfig{qwenAlternate()} })
	if err := rig.store.PutReviewerOverride(context.Background(), testTenant, &ReviewerOverride{
		ProjectID: testProject, PullRequest: 41, Profile: qwenProfile,
	}); err != nil {
		t.Fatal(err)
	}
	running := reviewRunning()
	running.Name = DefaultReviewStatusContext + "/" + qwenProfile
	rig.setChecks(greenRun("ci", sweepHead), running)
	started, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil || started != 0 {
		t.Fatalf("started=%d err=%v; the chosen reviewer is already at work", started, err)
	}

	// The DEFAULT's pending status is not the advisory reviewer at work.
	rig.setChecks(greenRun("ci", sweepHead), reviewRunning())
	started, err = rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil || started != 1 {
		t.Fatalf("started=%d err=%v; another reviewer's status held the chosen one back", started, err)
	}
}

func TestReviewSweeper_ARemovedProfileIsAVisibleFailure(t *testing.T) {
	rig := newSweepRig(t)
	if err := rig.store.PutReviewerOverride(context.Background(), testTenant, &ReviewerOverride{
		ProjectID: testProject, PullRequest: 41, Profile: "gone",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
	var sweepErr *ReviewSweepError
	if !errors.As(err, &sweepErr) || sweepErr.Op != "SelectReviewer" {
		t.Fatalf("want a SelectReviewer sweep failure, got %v", err)
	}
}

// qwenJob is a review job for pull request 41 run as the qwen profile.
func (r *reviewRunnerRig) qwenJob() *orchestration.JobRun {
	job := r.job(41)
	job.Config.Params = ReviewJobSubject{PullRequest: 41, Profile: qwenProfile}.Params()
	return job
}

// bindQwenAsTheFakeForgesReviewer binds the qwen alternate under the login the
// fake forge attributes submissions to, and moves the default to another, so
// the runner's own-verdict recognition is exercised for the ALTERNATE.
func (r *reviewRunnerRig) bindQwenAsTheFakeForgesReviewer() {
	r.rebind(func(b *ProjectBinding) {
		b.Reviewer.Login = "codex-reviewer"
		qwen := qwenAlternate()
		qwen.Login = fakeReviewerLogin
		b.Reviewers = []ReviewerConfig{qwen}
		delete(b.Review.Reviewers, fakeReviewerLogin)
	})
}

func TestReviewRunner_RunsAsTheProfileTheJobNames(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.bindQwenAsTheFakeForgesReviewer()
	rig.reviewerSays(reviewSaying("I read it; one thing is wrong.", ReviewCodeRequestChanges))
	if err := rig.runner.Run(context.Background(), rig.qwenJob()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	spawn := rig.agents.spawns[0]
	if spawn.AgentID != testQwenAgent || spawn.Backend != "" {
		t.Fatalf("the reviewer was not spawned as the chosen seat's agent: %+v", spawn)
	}
	// THE VERDICT IS POSTED AS THE PROFILE'S OWN IDENTITY.
	if refs := rig.forges.resolvedTokenRefs(); !slices.Contains(refs, "sdlc/reviewer/api/qwen-token") ||
		slices.Contains(refs, "reviewer-tok") {
		t.Fatalf("the forge was reached as %v, want the qwen token only", refs)
	}
	// AN ADVISORY STATUS NEVER LANDS ON THE REQUIRED CONTEXT.
	statuses, _ := rig.forge.publishedStatuses()
	if len(statuses) == 0 {
		t.Fatal("the advisory review published no status")
	}
	for _, status := range statuses {
		if status.Context != DefaultReviewStatusContext+"/"+qwenProfile {
			t.Fatalf("an advisory review published under %q", status.Context)
		}
	}
	// The recorded gate names the reviewer and says it was not counted, and
	// its refusal did not become the gate's.
	gate := rig.auditDetails(auditReviewGate)
	if len(gate) != 1 || !strings.Contains(gate[0], "reviewed as qwen (advisory") ||
		strings.Contains(gate[0], GateBlockedChangesRequested.String()) {
		t.Fatalf("gate audit = %v", gate)
	}
}

// TestReviewRunner_AnAdvisoryProfileSkipsTheIndependenceRefusal proves the
// independence rule, which exists to keep an approval the gate would discard
// from costing a model run, does not refuse a profile whose approval the gate
// reads past anyway.
func TestReviewRunner_AnAdvisoryProfileSkipsTheIndependenceRefusal(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.bindQwenAsTheFakeForgesReviewer()
	rig.rebind(func(b *ProjectBinding) { b.Worker = orchestration.AgentRefTo(testQwenCoderAgent) })
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	if err := rig.runner.Run(context.Background(), rig.qwenJob()); err != nil {
		t.Fatalf("an advisory same-family review was refused: %v", err)
	}

	// Switched on, the same profile IS refused before a model is spent.
	rig2 := newReviewRunnerRig(t)
	rig2.bindQwenAsTheFakeForgesReviewer()
	rig2.rebind(func(b *ProjectBinding) {
		b.Worker = orchestration.AgentRefTo(testQwenCoderAgent)
		b.Reviewers[0].CountsTowardMerge = boolPtr(true)
	})
	var notIndependent *ReviewerNotIndependentError
	if err := rig2.runner.Run(context.Background(), rig2.qwenJob()); !errors.As(err, &notIndependent) {
		t.Fatalf("a counting same-family reviewer was admitted: %v", err)
	}
	if len(rig2.agents.spawns) != 0 {
		t.Fatal("a refused reviewer spawned")
	}
}

func TestReviewRunner_ARemovedProfileIsRefusedNotImpersonated(t *testing.T) {
	rig := newReviewRunnerRig(t)
	err := rig.runner.Run(context.Background(), rig.qwenJob())
	var missing *ReviewerProfileNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("want ReviewerProfileNotFoundError, got %v", err)
	}
	if len(rig.agents.spawns) != 0 || len(rig.forge.submissions()) != 0 {
		t.Fatal("a review ran as a reviewer the binding does not have")
	}
}

func TestReviewJobSubject_CarriesTheProfile(t *testing.T) {
	params := ReviewJobSubject{PullRequest: 7, Profile: qwenProfile}.Params()
	if params[ParamReviewerProfile] != qwenProfile {
		t.Fatalf("params = %v", params)
	}
	subject, err := ReviewJobSubjectFrom(&orchestration.JobConfig{Params: params})
	if err != nil || subject.Profile != qwenProfile || subject.PullRequest != 7 {
		t.Fatalf("subject = %+v, %v", subject, err)
	}
	if _, named := (ReviewJobSubject{PullRequest: 7}).Params()[ParamReviewerProfile]; named {
		t.Fatal("an unnamed profile was written as a parameter")
	}
	if _, err := ReviewJobSubjectFrom(&orchestration.JobConfig{Params: map[string]string{ParamReviewerProfile: qwenProfile}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a profile with no pull request was accepted: %v", err)
	}
}
