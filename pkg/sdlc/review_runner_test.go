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
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The review runner's rig
// =============================================================================
//
// TWENTY-SIX VERSIONS OF A SHELL WORKFLOW ARE A SPECIFICATION WRITTEN IN
// INCIDENT REPORTS, and the tests below are that specification. Each one is a
// production failure the shell review automation shipped; each fails without
// its fix. They are named for the property rather than for the version that
// broke it, because the property is what has to keep holding.
//
// The rig is the real module over in-memory stores: the real runner, the real
// verdict parser, the real gate, and the module's own forge and container
// doubles. Nothing here re-describes what the runner does — a test that
// agreed with a second description of the runner would prove nothing.

const reviewHead = "head-sha-abcdef0123456789"

type reviewRunnerRig struct {
	t         *testing.T
	store     *Store
	forge     *fakeForge
	forges    *fakeForges
	agents    *fakeAgents
	container *fakeContainer
	tokens    *fakeTokens
	runner    *ReviewRunner
	root      string

	// signing is the agent the reviewer's checkout authenticates against, so
	// a test can ask which SCOPE the reviewer requested.
	signing *fakeSigningAgent

	// repoURL overrides the project's remote, so a test can review a project
	// on the transport the fleet actually uses.
	repoURL string

	auditMu sync.Mutex
	audits  []orchestration.JobAuditEvent
}

// newReviewRunnerRig builds the module with a project that IS self-reviewed, one
// open pull request on reviewHead, and a container that answers the clone.
func newReviewRunnerRig(t *testing.T) *reviewRunnerRig {
	t.Helper()
	store := newTestStore(t)
	binding := testBinding()
	binding.Reviewer = ReviewerConfig{
		Agent: testReviewerAgent, Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok",
	}
	// The independence rule is on by default in testBinding, so the reviewer
	// has to be recorded as a family the gate can tell from the worker's.
	binding.Review.Reviewers[fakeReviewerLogin] = ModelFamilyOpenAI
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}

	forge := newFakeForge()
	forge.prs[41] = &ForgePullRequest{
		Number: 41, Title: "Fix the thing", Body: "It was broken.", State: "open",
		HeadRef: "issue/25-fix", HeadSHA: reviewHead, BaseRef: "main",
		URL: "http://forge.test/acme/api/pulls/41",
	}
	forge.files[41] = []ForgeChangedFile{{Path: "pkg/thing/thing.go", Status: "modified", Additions: 10, Deletions: 2}}

	root := t.TempDir()
	container := &fakeContainer{checkout: filepath.Join(orchestration.WorkspaceCheckoutRoot, testOrchProject().Name)}
	agents := &fakeAgents{container: container, host: executingHost{root: root}}
	forges := &fakeForges{forge: forge}
	tokens := &fakeTokens{token: []byte("a-reviewer-api-token")}
	signing := &fakeSigningAgent{}
	runner, err := NewReviewRunner(ReviewRunnerConfig{Agents: testAgentCatalog(),
		Bindings: store, Forges: forges, Trains: store, Tokens: tokens, Signing: signing,
		Guard:       testRepoGuard(t),
		SocketsRoot: testSocketsRoot(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &reviewRunnerRig{
		t: t, store: store, forge: forge, forges: forges, agents: agents,
		container: container, tokens: tokens, signing: signing, runner: runner, root: root,
	}
}

// reviewerSays makes the reviewer answer every prompt with text.
func (r *reviewRunnerRig) reviewerSays(text string) { r.agents.answers(text) }

// reviewerSaysInOrder makes the reviewer answer the nth prompt with
// texts[n-1], repeating the last for every prompt after it.
//
// It is how a test puts a reply that declared nothing and the reply that
// finally declares into ONE session, which is the only way to tell a reviewer
// that was reminded from one that was re-spawned.
func (r *reviewRunnerRig) reviewerSaysInOrder(texts ...string) {
	r.t.Helper()
	if len(texts) == 0 {
		r.t.Fatal("reviewerSaysInOrder needs at least one reply")
	}
	r.agents.mu.Lock()
	defer r.agents.mu.Unlock()
	r.agents.replyFn = func(turn int) *orchestration.PromptSessionResponse {
		if turn > len(texts) {
			turn = len(texts)
		}
		return workerSaid(texts[turn-1])
	}
}

// rebind edits the project's binding in place.
func (r *reviewRunnerRig) rebind(edit func(*ProjectBinding)) {
	r.t.Helper()
	binding, err := r.store.GetBinding(context.Background(), testTenant, testProject)
	if err != nil {
		r.t.Fatal(err)
	}
	edit(binding)
	if err := r.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		r.t.Fatal(err)
	}
}

// job is the JobRun the engine hands the runner for a review of pull request
// 41, built exactly as launchJob builds one.
func (r *reviewRunnerRig) job(pullRequest int) *orchestration.JobRun {
	return r.jobAs("review-job-1", pullRequest)
}

// jobAs is job under a named job id, for the tests where two jobs about one
// pull request have to be told apart.
func (r *reviewRunnerRig) jobAs(jobID string, pullRequest int) *orchestration.JobRun {
	r.t.Helper()
	guard, err := orchestration.NewExecutionGuard([]string{"true"})
	if err != nil {
		r.t.Fatal(err)
	}
	base, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	if err != nil {
		r.t.Fatal(err)
	}
	project := testOrchProject()
	if r.repoURL != "" {
		project.Git.RepoURL = r.repoURL
	}
	project.Workspace = orchestration.ProjectWorkspace{Path: r.root}
	return orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:  jobID,
		Runner: ReviewJobRunnerName,
		Config: &orchestration.JobConfig{
			TenantID: testTenant, ProjectID: testProject,
			Params: ReviewJobSubject{PullRequest: pullRequest}.Params(),
		},
		Project: project,
		Agents:  r.agents,
		Logger:  logger.NewAdapter(base),
		Policy:  &orchestration.JobLifecyclePolicy{Guard: guard},
		AuditFn: func(eventType, details string) {
			r.auditMu.Lock()
			r.audits = append(r.audits, orchestration.JobAuditEvent{Type: eventType, Details: details})
			r.auditMu.Unlock()
		},
		PersistFn: func(*orchestration.JobStatus) {},
	})
}

// run drives one review to its end.
func (r *reviewRunnerRig) run(pullRequest int) error {
	r.t.Helper()
	return r.runner.Run(context.Background(), r.job(pullRequest))
}

// mustRun drives one review and fails the test if it did not finish.
func (r *reviewRunnerRig) mustRun(pullRequest int) {
	r.t.Helper()
	if err := r.run(pullRequest); err != nil {
		r.t.Fatalf("Run: %v", err)
	}
}

// auditDetails is what the run recorded under one audit event type.
func (r *reviewRunnerRig) auditDetails(eventType string) []string {
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	var out []string
	for _, event := range r.audits {
		if event.Type == eventType {
			out = append(out, event.Details)
		}
	}
	return out
}

// reviewSaying is a reviewer reply: findings, then a declaration.
func reviewSaying(findings, code string) string {
	return findings + "\n\n" + ReviewVerdictMarker + " " + code
}

// =============================================================================
// The happy path — what "it works" means
// =============================================================================

func TestReviewRunner_ApprovalIsPostedOnTheHeadAndTurnsTheCheckGreen(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSays(reviewSaying("I read every file and it is correct.", ReviewCodeApprove))
	rig.mustRun(41)

	submissions := rig.forge.submissions()
	if len(submissions) != 1 {
		t.Fatalf("submissions = %+v, want exactly one", submissions)
	}
	if submissions[0].Verdict != VerdictApprove {
		t.Fatalf("verdict = %s", submissions[0].Verdict)
	}
	// THE VERDICT BINDS TO THE HEAD IT JUDGED. A submission carrying no head
	// could never be attributed by the gate, which is failure 9 with the
	// module itself as the cause.
	if submissions[0].HeadSHA != reviewHead {
		t.Fatalf("head = %q, want %q", submissions[0].HeadSHA, reviewHead)
	}
	if !strings.Contains(submissions[0].Body, "I read every file") {
		t.Fatalf("body did not carry the findings: %q", submissions[0].Body)
	}

	status, ok := rig.forge.lastStatus()
	if !ok {
		t.Fatal("an approval published no commit status, so branch protection has nothing to require")
	}
	if status.Status != CheckSuccess {
		t.Fatalf("status = %s, want success", status.Status)
	}
	if status.Context != DefaultReviewStatusContext {
		t.Fatalf("context = %q", status.Context)
	}
}

func TestReviewRunner_TheReviewerIsSpawnedFromTheBindingAndSignsNothing(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)

	if rig.agents.sessionCount() != ReviewSessionCount {
		t.Fatalf("sessions = %d, want %d", rig.agents.sessionCount(), ReviewSessionCount)
	}
	spawn := rig.agents.spawns[0]
	if spawn.AgentID != testReviewerAgent || spawn.Role != orchestration.AgentRoleReview || spawn.Backend != "" {
		t.Fatalf("the reviewer was not spawned as the seat's agent: %+v", spawn)
	}
	if spawn.SystemPrompt != ReviewerCharter() {
		t.Fatal("the reviewer did not get the reviewer charter")
	}
	// NOTHING RATIONS THE REVIEWER INSIDE ITS CONTAINER (go-quicbaas#927). Its
	// whole job is reading a diff, and `git diff` is a command. The grant a
	// spawn carries is decided by the backend it runs (WorkerSpawnArgs, applied
	// at JobRun.Spawn); what this runner must never do is state a narrower one.
	if slices.Contains(spawn.ExtraArgs, "--permission-mode") {
		t.Fatalf("the reviewer was rationed inside its container: %v", spawn.ExtraArgs)
	}
	// A REVIEWER HOLDS NO KEY. ApplyWorkspaceSpec sets SSH_AUTH_SOCK only
	// when it is given a signing socket, and this runner never gives one.
	if _, signing := spawn.Env[orchestration.WorkspaceSigningAgentEnv]; signing {
		t.Fatal("the reviewer's container was given a signing agent it has no use for")
	}
	if len(rig.agents.closed) != 1 {
		t.Fatalf("the reviewer session was not released: closed = %v", rig.agents.closed)
	}
}

// TestReviewRunner_RefusesAClaudeReviewer pins the owner ruling of 2026-09-13:
// claude reviews nothing in this fleet.
//
// THIS TEST REPLACES ONE THAT ASSERTED THE OPPOSITE. It used to bind a codex
// worker and a claude reviewer — a topology the independence rule allowed,
// because the two families differ — and check that the claude reviewer received
// the full-control container grant. That configuration is now refused outright:
// independence is necessary and no longer sufficient, because Claude Code writes
// this fleet's code and does not judge it.
//
// The refusal happens BEFORE a model is spent, and it is not behind
// RequireIndependentReviewer: an operator switching that policy off must not be
// able to admit a Claude Code reviewer.
func TestReviewRunner_RefusesAClaudeReviewer(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) {
		b.Worker = orchestration.AgentRefTo(testCodexCoderAgent)
		b.Reviewer.Agent = testClaudeReviewerAgent
		b.Review.Reviewers[fakeReviewerLogin] = ModelFamilyAnthropic
		// Independence OFF, to prove the refusal does not depend on it.
		b.Review.RequireIndependentReviewer = false
	})

	err := rig.run(41)
	var refused *ReviewerFamilyRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("want ReviewerFamilyRefusedError, got %v", err)
	}
	if refused.Agent != testClaudeReviewerAgent {
		t.Errorf("Agent = %q, want %s", refused.Agent, testClaudeReviewerAgent)
	}
	if len(rig.agents.spawns) != 0 {
		t.Errorf("a refused reviewer must not spawn: %d spawn(s)", len(rig.agents.spawns))
	}
}
func TestReviewRunner_TheGateIsEvaluatedAndRecorded(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) { b.Review.RequireGreenChecks = false })
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)

	gate := rig.auditDetails(auditReviewGate)
	if len(gate) != 1 {
		t.Fatalf("gate audits = %v, want one", gate)
	}
	// The run records the gate's OWN answer, from EvaluateReviewGate, rather
	// than a second opinion of its own.
	if !strings.Contains(gate[0], GateClear.String()) {
		t.Fatalf("the gate did not clear after an independent approval: %q", gate[0])
	}
}

// =============================================================================
// FAILURE 1 — a draft pull request may not be approved
// =============================================================================
//
// An approval contradicts the one person who has said the work is
// unfinished.

func TestReviewRunner_ADraftIsNotApproved(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.forge.prs[41].Draft = true
	rig.reviewerSays(reviewSaying("looks fine to me", ReviewCodeApprove))
	rig.mustRun(41)

	if got := rig.forge.submissions(); len(got) != 0 {
		t.Fatalf("a draft was reviewed anyway: %+v", got)
	}
	if rig.agents.sessionCount() != 0 {
		t.Fatal("a reviewer was spawned for a draft, spending a model on unfinished work")
	}
	status, ok := rig.forge.lastStatus()
	if !ok {
		t.Fatal("a draft published no status at all; a required check that is absent reads as nothing to wait for")
	}
	if status.Status == CheckSuccess {
		t.Fatalf("a draft's check went green: %+v", status)
	}
}

func TestReviewRunner_ADraftApprovalIsRefusedEvenPastTheSkip(t *testing.T) {
	// The skip above is the ordinary path. THIS is the assertion that holds
	// when a pull request is marked draft between the read and the verdict:
	// the submit step refuses an approval on a draft rather than posting it.
	rig := newReviewRunnerRig(t)
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		rig.forge.mu.Lock()
		rig.forge.prs[41].Draft = true
		rig.forge.mu.Unlock()
	}
	err := rig.run(41)
	if !errors.Is(err, ErrReview) {
		t.Fatalf("err = %v, want an ErrReview refusal", err)
	}
	if got := rig.forge.submissions(); len(got) != 0 {
		t.Fatalf("an approval was posted on a draft: %+v", got)
	}
}

func TestEvaluateReviewGate_ADraftNeverClears(t *testing.T) {
	// The gate's half of the same property: even fully approved and green, a
	// draft may not land.
	in := GateInput{
		Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h",
		AuthorModelFamilies: []string{ModelFamilyAnthropic},
		Reviews:             []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h", 1)},
		Checks:              []CheckRecord{green("ci", "h")},
	}
	if got := EvaluateReviewGate(in); !got.Mergeable() {
		t.Fatalf("the control case does not clear: %+v", got)
	}
	in.Draft = true
	got := EvaluateReviewGate(in)
	if got.Reason != GateBlockedPullRequestDraft {
		t.Fatalf("reason = %s (%s), want %s", got.Reason, got.Detail, GateBlockedPullRequestDraft)
	}
	if got.Mergeable() {
		t.Fatal("an approved, green DRAFT was reported mergeable")
	}
	// Pending, not a refusal: marking a pull request ready is what ordinarily
	// happens next.
	if !got.Reason.Pending() {
		t.Fatal("a draft was reported as a refusal rather than as a wait")
	}
}

// =============================================================================
// FAILURE 2 — a COMMENT must not dedup a head for ever
// =============================================================================
//
// A review posted while a pull request was a draft made that head
// permanently unapprovable: the reviewer saw its own comment, concluded it
// had already spoken, and never gave the verdict the ready pull request was
// waiting for.

func TestReviewRunner_AnEarlierCommentDoesNotDedupTheHead(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.forge.putReview(41, ForgeReview{
		Reviewer: fakeReviewerLogin, State: "COMMENT", Body: "read it while it was a draft",
		HeadSHA: reviewHead, Submitted: time.Unix(10, 0),
	})
	rig.reviewerSays(reviewSaying("now it is ready and it is correct", ReviewCodeApprove))
	rig.mustRun(41)

	submissions := rig.forge.submissions()
	if len(submissions) != 1 || submissions[0].Verdict != VerdictApprove {
		t.Fatalf("the head stayed unapprovable behind this reviewer's own comment: %+v", submissions)
	}
}

func TestStandingVerdict_ACommentIsNotADecision(t *testing.T) {
	reviews := []ForgeReview{
		{Reviewer: fakeReviewerLogin, State: "COMMENT", HeadSHA: reviewHead, Submitted: time.Unix(9, 0)},
		{Reviewer: "someone-else", State: "APPROVED", HeadSHA: reviewHead, Submitted: time.Unix(9, 0)},
		{Reviewer: fakeReviewerLogin, State: "APPROVED", HeadSHA: "another-head", Submitted: time.Unix(9, 0)},
	}
	if _, _, found := standingVerdict(reviews, fakeReviewerLogin, reviewHead); found {
		t.Fatal("a comment, another reviewer's approval, or a verdict on another head was read as this reviewer's decision")
	}
	if _, _, found := standingVerdict(reviews, "", reviewHead); found {
		t.Fatal("a reviewer with no login matched something")
	}
	if _, _, found := standingVerdict(reviews, fakeReviewerLogin, ""); found {
		t.Fatal("an empty head matched something")
	}
}

// =============================================================================
// FAILURE 3 — a pull request with no labels must not crash the reviewer
// =============================================================================
//
// `toJSON` of an empty label set is `null`, not `[]`, and the shell reviewer
// indexed into it. Nothing in this module routes on labels at all, which is
// the fix; these are the tests that keep it that way.

func TestParseWebhook_APullRequestWithNoLabelsParses(t *testing.T) {
	forge, err := NewGiteaBackend(GiteaConfig{BaseURL: "http://forge.test/api/v1", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"labels null":    `{"action":"opened","number":41,"labels":null,"pull_request":{"number":41,"labels":null,"head":{"sha":"abc"}},"repository":{"name":"api","owner":{"login":"acme"}}}`,
		"labels empty":   `{"action":"opened","number":41,"labels":[],"pull_request":{"number":41,"labels":[],"head":{"sha":"abc"}},"repository":{"name":"api","owner":{"login":"acme"}}}`,
		"labels absent":  `{"action":"opened","number":41,"pull_request":{"number":41,"head":{"sha":"abc"}},"repository":{"name":"api","owner":{"login":"acme"}}}`,
		"labels present": `{"action":"opened","number":41,"labels":[{"name":"x"}],"pull_request":{"number":41,"head":{"sha":"abc"}},"repository":{"name":"api","owner":{"login":"acme"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			event, err := forge.ParseWebhook("pull_request", []byte(payload))
			if err != nil {
				t.Fatalf("ParseWebhook: %v", err)
			}
			if event.Kind != EventPullRequestOpened || event.PullRequestNumber != 41 {
				t.Fatalf("event = %+v", event)
			}
			// AND THE LABEL SET IS NOT CARRIED AT ALL. A ForgeEvent has
			// nowhere to put one, which is what makes label-driven routing
			// unexpressible rather than merely discouraged.
			started, err := DispatchReview(context.Background(), &fakeReviewTrigger{started: true}, testTenant, deliveredTo(""), event)
			if err != nil || !started {
				t.Fatalf("dispatch on an unlabelled pull request: started=%v err=%v", started, err)
			}
		})
	}
}

func TestForgeEvent_CarriesNoLabelsAtAll(t *testing.T) {
	// THE STRUCTURAL TRIPWIRE. The payload tests above prove that today's
	// parser survives a null label set; this proves the whole question
	// cannot come back. A ForgeEvent has nowhere to put a label, so label
	// routing is unexpressible rather than merely discouraged — and the day
	// somebody adds the field, this fails before any routing is written on
	// top of it.
	shape := reflect.TypeOf(ForgeEvent{})
	for i := 0; i < shape.NumField(); i++ {
		if strings.Contains(strings.ToLower(shape.Field(i).Name), "label") {
			t.Fatalf("ForgeEvent grew a %s field; a webhook payload must not decide who reviews or whether to",
				shape.Field(i).Name)
		}
	}
	// And nothing on the review path reads the binding's display labels
	// either: the reviewer comes from ReviewerConfig and from nowhere else.
	config := reflect.TypeOf(ReviewerConfig{})
	for i := 0; i < config.NumField(); i++ {
		if strings.Contains(strings.ToLower(config.Field(i).Name), "label") {
			t.Fatalf("ReviewerConfig grew a %s field; the reviewer is project configuration, not a label",
				config.Field(i).Name)
		}
	}
}

func TestReviewRunner_AnUnlabelledPullRequestIsReviewed(t *testing.T) {
	rig := newReviewRunnerRig(t)
	// The forge's issue record for the pull request carries no labels at
	// all. Nothing in the review path reads one, so this must simply work.
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)
	if len(rig.forge.submissions()) != 1 {
		t.Fatal("an unlabelled pull request was not reviewed")
	}
}

// =============================================================================
// FAILURE 4 — a relabel must not overwrite a standing rejection
// =============================================================================
//
// The bypass jumped the rejection check it was written to respect. Here a
// second trigger on an unchanged head re-asserts the standing rejection
// rather than re-deriving a verdict that might differ.

func TestReviewRunner_ASecondTriggerReassertsAStandingRejection(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSays(reviewSaying("this breaks the error contract", ReviewCodeRequestChanges))
	rig.mustRun(41)
	if got := rig.forge.submissions(); len(got) != 1 || got[0].Verdict != VerdictRequestChanges {
		t.Fatalf("first run: %+v", got)
	}

	// The second trigger arrives on the SAME head with a reviewer that would
	// now say something else. The standing rejection must survive it.
	rig.reviewerSays(reviewSaying("actually it is fine", ReviewCodeApprove))
	rig.mustRun(41)

	submissions := rig.forge.submissions()
	if len(submissions) != 1 {
		t.Fatalf("a re-trigger posted a second verdict over a standing rejection: %+v", submissions)
	}
	status, ok := rig.forge.lastStatus()
	if !ok || status.Status != CheckFailure {
		t.Fatalf("the re-trigger did not re-assert the rejection's red status: %+v", status)
	}
	if rig.agents.sessionCount() != 1 {
		t.Fatal("the re-trigger spawned a second reviewer on a head that was already decided")
	}
}

func TestReviewRunner_ANewHeadIsReviewedAgain(t *testing.T) {
	// The other side of the dedup: it binds to a HEAD, not to a pull
	// request. A dedup that outlived the head would leave every later commit
	// unreviewed.
	rig := newReviewRunnerRig(t)
	rig.reviewerSays(reviewSaying("this breaks the error contract", ReviewCodeRequestChanges))
	rig.mustRun(41)

	rig.forge.mu.Lock()
	rig.forge.prs[41].HeadSHA = "head-sha-second"
	rig.forge.mu.Unlock()
	rig.reviewerSays(reviewSaying("the contract is honoured now", ReviewCodeApprove))
	rig.mustRun(41)

	submissions := rig.forge.submissions()
	if len(submissions) != 2 {
		t.Fatalf("a new head was not reviewed: %+v", submissions)
	}
	if submissions[1].HeadSHA != "head-sha-second" || submissions[1].Verdict != VerdictApprove {
		t.Fatalf("the second verdict is wrong: %+v", submissions[1])
	}
}

func TestStandingVerdict_TheHardestDecisionWinsWhateverTheOrder(t *testing.T) {
	rejection := ForgeReview{Reviewer: fakeReviewerLogin, State: "REQUEST_CHANGES", HeadSHA: reviewHead, Submitted: time.Unix(1, 0)}
	later := ForgeReview{Reviewer: fakeReviewerLogin, State: "COMMENT", HeadSHA: reviewHead, Submitted: time.Unix(9, 0)}
	approval := ForgeReview{Reviewer: fakeReviewerLogin, State: "APPROVED", HeadSHA: reviewHead, Submitted: time.Unix(5, 0)}
	for name, reviews := range map[string][]ForgeReview{
		"rejection first": {rejection, approval, later},
		"rejection last":  {later, approval, rejection},
		"rejection alone": {rejection},
	} {
		t.Run(name, func(t *testing.T) {
			_, verdict, found := standingVerdict(reviews, fakeReviewerLogin, reviewHead)
			if !found || verdict != VerdictRequestChanges {
				t.Fatalf("verdict = %s found = %v; a rejection was retracted by ordering", verdict, found)
			}
		})
	}
}

// =============================================================================
// FAILURE 5 — the changed-file list must paginate
// =============================================================================
//
// A single capped page was treated as the whole listing, so a reviewer
// handed part of a change reviewed part of a change and approved all of it.
// (The Gitea adapter's half is in forge_gitea_test.go.)

func TestReviewRunner_TheWholeChangedFileListReachesThePrompt(t *testing.T) {
	rig := newReviewRunnerRig(t)
	files := make([]ForgeChangedFile, 0, 120)
	for i := 0; i < 120; i++ {
		files = append(files, ForgeChangedFile{Path: "pkg/thing/file" + itoa(i) + ".go", Status: "modified"})
	}
	rig.forge.files[41] = files
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)

	prompts := rig.agents.promptTexts()
	if len(prompts) != 1 {
		t.Fatalf("prompts = %d", len(prompts))
	}
	// The FIRST page's worth is not enough: the listing is the reviewer's
	// map of the change, and a prefix is a confident answer to a different
	// question.
	for _, file := range []string{"file0.go", "file49.go", "file50.go", "file119.go"} {
		if !strings.Contains(prompts[0], file) {
			t.Fatalf("the reviewer was not told about %s", file)
		}
	}
	if !strings.Contains(prompts[0], "(120, complete)") {
		t.Fatal("the prompt does not tell the reviewer the listing is complete")
	}
}

// =============================================================================
// FAILURE 6 — an empty verdict is not a verdict
// =============================================================================
//
// An approval with no body is indistinguishable from a review that never
// read the change.

func TestReviewRunner_AnApprovalWithNoFindingsIsNotPosted(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSays(ReviewVerdictMarker + " " + ReviewCodeApprove)
	err := rig.run(41)
	if !errors.Is(err, ErrReview) {
		t.Fatalf("err = %v, want an ErrReview refusal", err)
	}
	for _, submission := range rig.forge.submissions() {
		if submission.Verdict == VerdictApprove {
			t.Fatalf("an empty approval reached the forge: %+v", submission)
		}
	}
	status, ok := rig.forge.lastStatus()
	if !ok || status.Status != CheckFailure {
		t.Fatalf("a review that produced no verdict did not turn the check red: %+v", status)
	}
}

func TestReviewRunner_AReplyWithNoDeclarationIsNotAVerdict(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSays("Looks good to me, ship it.")
	err := rig.run(41)
	if !errors.Is(err, ErrReview) {
		t.Fatalf("err = %v, want an ErrReview refusal", err)
	}
	// The reviewer's words still reach the author, as a comment that decides
	// nothing — but never as an approval.
	for _, submission := range rig.forge.submissions() {
		if submission.Verdict != VerdictComment {
			t.Fatalf("prose was read as a verdict: %+v", submission)
		}
		if !strings.Contains(submission.Body, "Looks good to me") {
			t.Fatalf("the reviewer's words were discarded: %q", submission.Body)
		}
	}
	status, _ := rig.forge.lastStatus()
	if status.Status != CheckFailure {
		t.Fatalf("status = %s, want failure", status.Status)
	}
}

// =============================================================================
// A reply that stated no verdict is asked once more for one
// =============================================================================
//
// Measured on the burn-in fleet, 2026-09-22 03:39, PR #983, review job
// c64bd251: the reviewer had read the whole diff and ended its turn on a
// progress note — "I'm finishing the checks around forge mergeability" — with
// no declaration. The job was failed and the session closed, and the sweep
// spawned a fresh container to re-read the same diff from nothing. A whole
// review's cost thrown away because the model paused mid-sentence.

func TestReviewRunner_AReplyWithNoVerdictIsAskedOnceMoreInTheSameSession(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSaysInOrder(
		"The round limit is shared with ordinary review rounds, and clean rebases return to "+
			"the gate for approval on the new head. I'm finishing the checks around forge "+
			"mergeability and the transition back to review.",
		reviewSaying("Read the whole diff; the error types are right.", ReviewCodeApprove),
	)
	rig.mustRun(41)

	submissions := rig.forge.submissions()
	if len(submissions) != 1 || submissions[0].Verdict != VerdictApprove {
		t.Fatalf("submissions = %+v, want the one verdict the second reply declared", submissions)
	}
	if status, _ := rig.forge.lastStatus(); status.Status != CheckSuccess {
		t.Fatalf("status = %s, want success", status.Status)
	}
	// ONE SESSION AND ONE JOB. The reminder reached the reviewer that had
	// already read the diff; a second spawn here would be the cost this
	// whole path exists to stop paying.
	if got := rig.agents.sessionCount(); got != 1 {
		t.Fatalf("sessions = %d, want 1: the re-prompt spawned another reviewer", got)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %d, want the review and exactly one reminder", len(prompts))
	}
	if prompts[1] != reviewRepromptPrompt {
		t.Fatalf("the second prompt was not the reminder: %q", prompts[1])
	}
	if !strings.Contains(prompts[1], ReviewVerdictMarker) {
		t.Fatalf("the reminder never names the marker the parser reads: %q", prompts[1])
	}
	if got := rig.auditDetails(auditReviewReprompted); len(got) != 1 {
		t.Fatalf("%s = %v, want exactly one", auditReviewReprompted, got)
	}
	// BOTH TURNS ARE ON THE RECORD. The re-prompt is not a round, so the
	// audit trail is the only place a reader can see that it happened.
	if got := rig.auditDetails(auditReviewVerdict); len(got) != 2 {
		t.Fatalf("%s = %d, want both turns recorded", auditReviewVerdict, len(got))
	}
}

func TestReviewRunner_ASecondReplyWithNoVerdictIsRefusedAsBefore(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSaysInOrder(
		"Still working through the diff.",
		"Nearly done, one more file to read.",
	)
	err := rig.run(41)
	if !errors.Is(err, ErrReview) {
		t.Fatalf("err = %v, want an ErrReview refusal", err)
	}
	if got := len(rig.agents.promptTexts()); got != 2 {
		t.Fatalf("prompts = %d, want the review and exactly one reminder", got)
	}
	if got := rig.auditDetails(auditReviewReprompted); len(got) != 1 {
		t.Fatalf("%s = %v, want exactly one; a reviewer is not asked indefinitely", auditReviewReprompted, got)
	}
	if status, _ := rig.forge.lastStatus(); status.Status != CheckFailure {
		t.Fatalf("status = %s, want failure", status.Status)
	}
	for _, submission := range rig.forge.submissions() {
		if submission.Verdict != VerdictComment {
			t.Fatalf("prose was read as a verdict: %+v", submission)
		}
		if !strings.Contains(submission.Body, "Nearly done") {
			t.Fatalf("the refusal noted a reply other than the last one: %q", submission.Body)
		}
	}
}

func TestReviewRunner_ADeclaredReplyIsNeverReprompted(t *testing.T) {
	// INCLUDING THE TWO REFUSALS THAT ARE NOT A MISSING DECLARATION. A
	// reviewer that declared an unknown code, or a code above nothing,
	// reached the end of its review and answered badly; asking it again
	// would be nudging it towards any answer that parses rather than
	// letting the parser's refusal stand.
	for name, tc := range map[string]struct {
		reply   string
		refused bool
	}{
		"a verdict":       {reply: reviewSaying("read it", ReviewCodeApprove)},
		"an unknown code": {reply: reviewSaying("read it", "looks-fine"), refused: true},
		"no findings":     {reply: ReviewVerdictMarker + " " + ReviewCodeApprove, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newReviewRunnerRig(t)
			rig.reviewerSays(tc.reply)
			err := rig.run(41)
			switch {
			case tc.refused && !errors.Is(err, ErrReview):
				t.Fatalf("err = %v, want the parser's ErrReview refusal to stand", err)
			case !tc.refused && err != nil:
				t.Fatalf("Run: %v, want the declared verdict to complete the review", err)
			}
			if got := len(rig.agents.promptTexts()); got != 1 {
				t.Fatalf("prompts = %d, want exactly one: a declared reply is never re-prompted", got)
			}
			if got := rig.auditDetails(auditReviewReprompted); len(got) != 0 {
				t.Fatalf("%s = %v, want none", auditReviewReprompted, got)
			}
		})
	}
}

func TestReviewRunner_AReminderThatIsNeverAnsweredIsCancelledAtItsDeadline(t *testing.T) {
	// THE STALL DETECTOR WOULD NOT END THIS. It ends a job whose session is
	// gone; this session is still addressable, it just never answers. So the
	// reminder carries a deadline of its own, and a reviewer that goes silent
	// after being reminded fails the job instead of holding it open.
	rig := newReviewRunnerRig(t)
	runner, err := NewReviewRunner(ReviewRunnerConfig{Agents: testAgentCatalog(),
		Bindings: rig.store, Forges: rig.forges, Trains: rig.store, Tokens: rig.tokens, Signing: rig.signing,
		Guard:           testRepoGuard(t),
		SocketsRoot:     testSocketsRoot(t),
		RepromptTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	rig.runner = runner
	rig.reviewerSays("Still reading the diff.")
	rig.agents.mu.Lock()
	rig.agents.hangFn = func(turn int) bool { return turn == 2 }
	rig.agents.mu.Unlock()

	err = rig.run(41)
	var timeout *ReviewRepromptTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("err = %v, want a ReviewRepromptTimeoutError", err)
	}
	if !errors.Is(err, ErrReview) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want it to match ErrReview and context.DeadlineExceeded", err)
	}
	if timeout.Timeout != 50*time.Millisecond {
		t.Fatalf("timeout = %s, want the runner's configured reminder deadline", timeout.Timeout)
	}
	if got := len(rig.agents.promptTexts()); got != 2 {
		t.Fatalf("prompts = %d, want the review and the one reminder", got)
	}
	if got := rig.auditDetails(auditReviewReprompted); len(got) != 1 {
		t.Fatalf("%s = %v, want exactly one", auditReviewReprompted, got)
	}
	if got := rig.agents.sessionCount(); got != 1 {
		t.Fatalf("sessions = %d, want 1", got)
	}
}

func TestReviewRunner_TheReminderDeadlineDefaultsToTheReviewTurnTimeout(t *testing.T) {
	rig := newReviewRunnerRig(t)
	if rig.runner.repromptTimeout != ReviewTurnTimeout {
		t.Fatalf("repromptTimeout = %s, want ReviewTurnTimeout (%s)", rig.runner.repromptTimeout, ReviewTurnTimeout)
	}
	for _, spec := range ReviewRunnerStates() {
		if spec.State == orchestration.JobStateExecuting && spec.IdleThreshold != ReviewTurnTimeout {
			t.Fatalf("executing IdleThreshold = %s, want ReviewTurnTimeout", spec.IdleThreshold)
		}
	}
}

func TestReviewSubmission_ValidateRefusesWhatNoForgeShouldStore(t *testing.T) {
	for name, tc := range map[string]struct {
		submission ReviewSubmission
		wantOK     bool
	}{
		"complete":      {ReviewSubmission{Verdict: VerdictApprove, Body: "read it", HeadSHA: "h"}, true},
		"empty body":    {ReviewSubmission{Verdict: VerdictApprove, Body: "  \n ", HeadSHA: "h"}, false},
		"no head":       {ReviewSubmission{Verdict: VerdictApprove, Body: "read it"}, false},
		"error verdict": {ReviewSubmission{Verdict: VerdictError, Body: "read it", HeadSHA: "h"}, false},
		"no verdict":    {ReviewSubmission{Body: "read it", HeadSHA: "h"}, false},
		"comment":       {ReviewSubmission{Verdict: VerdictComment, Body: "read it", HeadSHA: "h"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.submission.Validate()
			if tc.wantOK && err != nil {
				t.Fatalf("refused a valid submission: %v", err)
			}
			if !tc.wantOK && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("accepted an invalid submission: %v", err)
			}
		})
	}
}

// =============================================================================
// FAILURE 7 — a rejection must make the gate red
// =============================================================================
//
// The check went green on the very run that filed REQUEST_CHANGES, so
// auto-merge landed rejected code.

func TestReviewRunner_ARejectionIsRedOnTheRunThatFilesIt(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSays(reviewSaying("the error path swallows the cause", ReviewCodeRequestChanges))
	rig.mustRun(41)

	status, ok := rig.forge.lastStatus()
	if !ok {
		t.Fatal("a rejection published no status at all")
	}
	if status.Status != CheckFailure {
		t.Fatalf("status = %s on the run that filed the rejection, want failure", status.Status)
	}
	// And nothing green was published on the way there: a check that went
	// green first and red afterwards is a window auto-merge can land in.
	for _, published := range rig.forge.statuses {
		if published.Status == CheckSuccess {
			t.Fatalf("a green status was published during a rejecting run: %+v", rig.forge.statuses)
		}
	}
}

func TestReviewRunner_AVerdictThatDecidesNothingIsNotGreen(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSays(reviewSaying("I cannot judge the cryptography here", ReviewCodeComment))
	rig.mustRun(41)

	status, ok := rig.forge.lastStatus()
	if !ok || status.Status != CheckPending {
		t.Fatalf("a comment published %+v; a review that declined to judge must leave the "+
			"head unjudged and retriable, which only pending says", status)
	}
	if got := rig.forge.submissions(); len(got) != 1 || got[0].Verdict != VerdictComment {
		t.Fatalf("submissions = %+v", got)
	}
}

func TestReviewStatuses_EveryVerdictMapsAndNoneDefaultsToGreen(t *testing.T) {
	// A verdict added to the vocabulary without a status blocks until
	// somebody maps it, rather than merging on the default.
	for _, verdict := range []ReviewVerdict{VerdictApprove, VerdictRequestChanges, VerdictComment, VerdictError} {
		if _, ok := reviewStatuses[verdict]; !ok {
			t.Fatalf("verdict %s has no status mapping", verdict)
		}
	}
	run := &reviewRun{reviewer: ReviewerProfile{}}
	if got := run.statusForVerdict("a-verdict-from-the-future", "body"); got.Status != CheckFailure {
		t.Fatalf("an unmapped verdict published %s, want failure", got.Status)
	}
}

// TestReviewStatuses_AVerdictlessReviewStaysRetriable pins the shape of the
// deadlock that stranded three finished changes.
//
// A verdict that records NO REVIEW leaves the head genuinely unjudged, so the
// gate still wants a review for it and the floor must still be able to
// dispatch one. Only `pending` says that. A terminal status would claim a
// decision nobody made and leave no way to get one — which is what happened
// when these were mapped to failure: the deadlock moved rather than closing,
// because a terminal status cannot be retried either.
//
// What makes pending safe is the floor's staleness bound, which is why these
// two tests are a pair: see TestCheckRunPresent_StalePendingIsNotInFlight.
func TestReviewStatuses_AVerdictlessReviewStaysRetriable(t *testing.T) {
	run := &reviewRun{reviewer: ReviewerProfile{}}
	for _, verdict := range []ReviewVerdict{VerdictComment, VerdictError} {
		got := run.statusForVerdict(verdict, "read it")
		if got.Status != CheckPending {
			t.Fatalf("verdict %s published %s; a review that recorded no verdict must stay "+
				"retriable, or the head can never be judged", verdict, got.Status)
		}
		if got.Status == CheckSuccess {
			t.Fatalf("verdict %s must never satisfy the check", verdict)
		}
	}
	// And a decision IS terminal: nothing about it is owed another attempt.
	for _, verdict := range []ReviewVerdict{VerdictApprove, VerdictRequestChanges} {
		if got := run.statusForVerdict(verdict, "x"); got.Status == CheckPending {
			t.Fatalf("verdict %s published pending; a real decision is terminal", verdict)
		}
	}
	if got := run.statusForVerdict(VerdictComment, "read it"); !strings.Contains(got.Description, "without a verdict") {
		t.Fatalf("the status must say the reviewer declined to judge, got %q", got.Description)
	}
}

// =============================================================================
// FAILURE 8 — a rejection is not order-dependent
// =============================================================================
//
// A later COMMENT must not retract an earlier REQUEST_CHANGES.

func TestEvaluateReviewGate_ALaterCommentDoesNotRetractARejection(t *testing.T) {
	// A forge COMMENT carries no verdict at all, so it cannot supersede one.
	// This is the end-to-end statement of that, through the same conversion
	// the validator uses.
	policy := gatePolicy()
	rejection, acted := reviewRecordFor(ForgeReview{
		ID: 1, Reviewer: "codex", State: "REQUEST_CHANGES", HeadSHA: "h",
		Body: "no", Submitted: time.Unix(1, 0),
	}, policy, 1)
	if !acted {
		t.Fatal("a REQUEST_CHANGES produced no record")
	}
	if _, commented := reviewRecordFor(ForgeReview{
		ID: 2, Reviewer: "codex", State: "COMMENT", HeadSHA: "h",
		Body: "one more thought", Submitted: time.Unix(9, 0),
	}, policy, 1); commented {
		t.Fatal("a COMMENT produced a verdict record, so it can retract a refusal")
	}

	in := GateInput{
		Policy: policy, PullRequestState: PullRequestOpen, HeadSHA: "h",
		AuthorModelFamilies: []string{ModelFamilyAnthropic},
		Reviews:             []ReviewRecord{rejection},
		Checks:              []CheckRecord{green("ci", "h")},
	}
	if got := EvaluateReviewGate(in); got.Reason != GateBlockedChangesRequested {
		t.Fatalf("reason = %s, want %s", got.Reason, GateBlockedChangesRequested)
	}
}

func TestLatestVerdictsForHead_ACommentTieDoesNotClearAnApproval(t *testing.T) {
	// Severity is the tie-break when two verdicts by one reviewer share a
	// tick, and a comment ranks above an approval on purpose: the gate fails
	// closed on an ordering it cannot decide.
	if verdictSeverity(VerdictComment) <= verdictSeverity(VerdictApprove) {
		t.Fatal("a comment does not outrank an approval, so a tie could clear the gate")
	}
	if verdictSeverity(VerdictComment) >= verdictSeverity(VerdictRequestChanges) {
		t.Fatal("a comment outranks a refusal, so a tie could retract one")
	}
	reviews := []ReviewRecord{
		{Reviewer: "codex", Verdict: VerdictApprove, HeadSHA: "h", Round: 1, SubmittedAt: time.Unix(3, 0)},
		{Reviewer: "codex", Verdict: VerdictComment, HeadSHA: "h", Round: 1, SubmittedAt: time.Unix(3, 0)},
	}
	got := latestVerdictsForHead(reviews, "h")
	if len(got) != 1 || got[0].Verdict != VerdictComment {
		t.Fatalf("verdicts = %+v", got)
	}
}

// =============================================================================
// FAILURE 9 — a stale verdict must not gate a new head
// =============================================================================

func TestReviewRunner_AVerdictOnAnEarlierHeadDoesNotCountForThisOne(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.forge.putReview(41, ForgeReview{
		Reviewer: fakeReviewerLogin, State: "APPROVED", Body: "approved the previous commit",
		HeadSHA: "head-sha-previous", Submitted: time.Unix(1, 0),
	})
	rig.reviewerSays(reviewSaying("the new commit is wrong", ReviewCodeRequestChanges))
	rig.mustRun(41)

	submissions := rig.forge.submissions()
	if len(submissions) != 1 || submissions[0].Verdict != VerdictRequestChanges {
		t.Fatalf("a stale approval dedupped the current head: %+v", submissions)
	}
	if submissions[0].HeadSHA != reviewHead {
		t.Fatalf("head = %q", submissions[0].HeadSHA)
	}
}

// =============================================================================
// Refusals and configuration
// =============================================================================

func TestReviewRunner_RefusesAProjectThatBindsNoReviewer(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) { b.Reviewer = ReviewerConfig{} })
	err := rig.run(41)
	if !errors.Is(err, ErrReview) {
		t.Fatalf("err = %v, want ErrReview", err)
	}
	if rig.agents.sessionCount() != 0 {
		t.Fatal("a reviewer was spawned for a project that binds none")
	}
}

func TestReviewRunner_RefusesAReviewerFromTheAuthorsOwnFamily(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) {
		b.Reviewer.Agent = testClaudeReviewerAgent
		// The fixture's stated family for this login is openai; a configured
		// reviewer's family is derived from its Agent, so the statement goes
		// with the Agent it described.
		delete(b.Review.Reviewers, b.Reviewer.Login)
	})
	err := rig.run(41)
	if !errors.Is(err, ErrReview) {
		t.Fatalf("err = %v, want ErrReview", err)
	}
	if len(rig.forge.submissions()) != 0 || len(rig.forge.statuses) != 0 {
		t.Fatal("a verdict the gate could never count was published anyway")
	}
}

func TestReviewRunner_RefusesAForgeThatCannotBeToldAVerdict(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.forge.caps = ForgeCapabilities{SubmitReviews: false}
	err := rig.run(41)
	if !errors.Is(err, ErrForgeUnsupported) {
		t.Fatalf("err = %v, want ErrForgeUnsupported", err)
	}
}

func TestReviewRunner_AForgeWithoutCommitStatusesStillReviews(t *testing.T) {
	// Losing enforcement is not losing the review: the verdict is still
	// posted and the module's own gate still reads it.
	rig := newReviewRunnerRig(t)
	rig.forge.caps = ForgeCapabilities{SubmitReviews: true, CommitStatuses: false}
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)
	if len(rig.forge.submissions()) != 1 {
		t.Fatal("the verdict was not posted")
	}
	if len(rig.forge.statuses) != 0 {
		t.Fatal("a status was published to a forge that has none")
	}
}

func TestReviewRunner_SkipsAPullRequestThatIsNoLongerOpen(t *testing.T) {
	for name, edit := range map[string]func(*ForgePullRequest){
		"merged": func(pr *ForgePullRequest) { pr.Merged = true; pr.State = "closed" },
		"closed": func(pr *ForgePullRequest) { pr.State = "closed" },
	} {
		t.Run(name, func(t *testing.T) {
			rig := newReviewRunnerRig(t)
			edit(rig.forge.prs[41])
			rig.mustRun(41)
			if len(rig.forge.submissions()) != 0 || len(rig.forge.statuses) != 0 {
				t.Fatal("a pull request that is not open was reviewed")
			}
		})
	}
}

func TestReviewRunner_RefusesAPullRequestThatDoesNotExist(t *testing.T) {
	rig := newReviewRunnerRig(t)
	if err := rig.run(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestReviewRunner_RefusesAHeadlessPullRequest(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.forge.prs[41].HeadSHA = ""
	if err := rig.run(41); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestReviewRunner_RefusesAnUncontainerizedReviewer(t *testing.T) {
	// A reviewer reads a candidate change, which is untrusted input; it must
	// not read it on the daemon.
	rig := newReviewRunnerRig(t)
	rig.agents.noContainer = true
	if err := rig.run(41); !errors.Is(err, ErrReview) {
		t.Fatalf("err = %v, want ErrReview", err)
	}
}

func TestNewReviewRunner_RefusesIncompleteWiring(t *testing.T) {
	store := newTestStore(t)
	forges := &fakeForges{}
	tokens := &fakeTokens{}
	signing := &fakeSigningAgent{}
	sockets := testSocketsRoot(t)
	complete := ReviewRunnerConfig{Agents: testAgentCatalog(),
		Bindings: store, Forges: forges, Trains: store, Tokens: tokens, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets,
	}
	if _, err := NewReviewRunner(complete); err != nil {
		t.Fatal(err)
	}
	for name, cfg := range map[string]ReviewRunnerConfig{
		"agents":   {Bindings: store, Forges: forges, Trains: store, Tokens: tokens, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets},
		"bindings": {Agents: testAgentCatalog(), Forges: forges, Trains: store, Tokens: tokens, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets},
		"forges":   {Agents: testAgentCatalog(), Bindings: store, Trains: store, Tokens: tokens, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets},
		"trains":   {Agents: testAgentCatalog(), Bindings: store, Forges: forges, Tokens: tokens, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets},
		"tokens":   {Agents: testAgentCatalog(), Bindings: store, Forges: forges, Trains: store, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets},
		// The reviewer clones over whatever transport the project names, so
		// an ssh remote needs an agent and a runner without one is refused
		// here rather than inside a container (go-quicbaas#929).
		"signing": {Agents: testAgentCatalog(), Bindings: store, Forges: forges, Trains: store, Tokens: tokens, Guard: testRepoGuard(t), SocketsRoot: sockets},
	} {
		if _, err := NewReviewRunner(cfg); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s nil accepted: %v", name, err)
		}
	}
	if _, err := (&ReviewRunner{}).Run(context.Background(), nil), error(nil); err != nil {
		t.Fatal(err)
	}
	runner, err := NewReviewRunner(complete)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(context.Background(), nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil job was accepted: %v", err)
	}
}

func TestReviewRunner_DeclaresItsShapeToTheEngine(t *testing.T) {
	runner := &ReviewRunner{}
	if runner.Name() != ReviewJobRunnerName {
		t.Fatalf("name = %q", runner.Name())
	}
	states := runner.States()
	if len(states) == 0 || states[0].State != orchestration.JobStateInitializing {
		t.Fatalf("states = %+v", states)
	}
	backed := 0
	for _, spec := range states {
		if spec.SessionBacked {
			backed++
			if spec.State != orchestration.JobStateExecuting {
				t.Fatalf("%s is declared session-backed", spec.State)
			}
		}
		if spec.IdleThreshold <= 0 {
			t.Fatalf("%s declares no idle bound", spec.State)
		}
	}
	if backed != 1 {
		t.Fatalf("session-backed states = %d, want exactly one", backed)
	}
	// Every interrupted review restarts at the beginning: the forge is the
	// record and the first step re-reads it.
	for _, state := range []orchestration.JobState{
		orchestration.JobStateExecuting, orchestration.JobStateCompleting, orchestration.JobStateCleanup,
	} {
		if got := runner.ResumeState(state); got != orchestration.JobStateInitializing {
			t.Fatalf("resume from %s = %s", state, got)
		}
	}
}

func TestReviewRunner_ResumingAfterAPublishedVerdictPublishesNothingTwice(t *testing.T) {
	// A run interrupted after the verdict landed restarts at the beginning,
	// finds its own standing verdict on the forge, and finishes.
	rig := newReviewRunnerRig(t)
	rig.forge.putReview(41, ForgeReview{
		Reviewer: fakeReviewerLogin, State: "APPROVED", Body: "read it", HeadSHA: reviewHead,
		Submitted: time.Unix(1, 0),
	})
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)
	if got := rig.forge.submissions(); len(got) != 0 {
		t.Fatalf("a resumed review posted its verdict a second time: %+v", got)
	}
	status, ok := rig.forge.lastStatus()
	if !ok || status.Status != CheckSuccess {
		t.Fatalf("the standing approval's status was not re-asserted: %+v", status)
	}
}

func TestValidateReviewJobConfig_RefusesWhatCannotBeDriven(t *testing.T) {
	subject := ReviewJobSubject{PullRequest: 41}
	good := &orchestration.JobConfig{ProjectID: testProject, Params: subject.Params()}
	if err := ValidateReviewJobConfig(good); err != nil {
		t.Fatalf("refused a drivable config: %v", err)
	}
	for name, cfg := range map[string]*orchestration.JobConfig{
		"nil":           nil,
		"no project":    {Params: subject.Params()},
		"no subject":    {ProjectID: testProject},
		"unparsable":    {ProjectID: testProject, Params: map[string]string{ParamPullRequest: "forty-one"}},
		"zero subject":  {ProjectID: testProject, Params: map[string]string{ParamPullRequest: "0"}},
		"negative":      {ProjectID: testProject, Params: map[string]string{ParamPullRequest: "-1"}},
		"empty subject": {ProjectID: testProject, Params: map[string]string{ParamPullRequest: ""}},
	} {
		if err := ValidateReviewJobConfig(cfg); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if err := (&ReviewRunner{}).Validate(context.Background(), good); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// =============================================================================
// The forge failing, at each point it can
// =============================================================================
//
// Every one of these used to be a way for the shell automation to leave a
// pull request in a state nobody could read: a verdict with no status, a
// status with no verdict, a job that reported success on a write that never
// landed. The runner reports each as the fault it is.

func TestReviewRunner_AForgeFailureAtAnyStepIsReported(t *testing.T) {
	boom := errors.New("the forge stopped answering")
	for name, arrange := range map[string]func(*reviewRunnerRig){
		"listing the reviews": func(r *reviewRunnerRig) {
			r.forge.listReviewsErr = boom
		},
		"listing the files": func(r *reviewRunnerRig) {
			r.forge.listFilesErr = boom
		},
		"submitting the verdict": func(r *reviewRunnerRig) {
			r.forge.submitErr = boom
			r.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
		},
		"publishing the status": func(r *reviewRunnerRig) {
			r.forge.statusErr = boom
			r.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
		},
		"reading the checks for the gate": func(r *reviewRunnerRig) {
			r.forge.listChecksErr = boom
			r.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
		},
		"resolving the reviewer's token": func(r *reviewRunnerRig) {
			r.tokens.err = boom
		},
		"building the reviewer's adapter": func(r *reviewRunnerRig) {
			r.forges.err = boom
		},
		"spawning the reviewer": func(r *reviewRunnerRig) {
			r.agents.spawnErr = boom
		},
		"prompting the reviewer": func(r *reviewRunnerRig) {
			r.agents.promptErr = boom
		},
		"cloning the checkout": func(r *reviewRunnerRig) {
			r.container.cloneErr = boom
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newReviewRunnerRig(t)
			if rig.forge.caps.SubmitReviews {
				rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
			}
			arrange(rig)
			if err := rig.run(41); err == nil {
				t.Fatal("a forge failure was reported as a finished review")
			}
			// And the reviewer session never outlives the failure.
			if rig.agents.sessionCount() > 0 && len(rig.agents.closed) == 0 {
				t.Fatal("the reviewer session was leaked past the failure")
			}
		})
	}
}

func TestReviewRunner_AForgeWithNoAdapterIsRefused(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.forges.forge = nil
	if err := rig.run(41); !errors.Is(err, ErrForgeNotRegistered) {
		t.Fatalf("err = %v, want ErrForgeNotRegistered", err)
	}
}

func TestReviewRunner_RefusesAJobWithNoProjectRecord(t *testing.T) {
	// Everything the reviewer's checkout is described from hangs off the
	// project; there is nothing to guess from.
	rig := newReviewRunnerRig(t)
	job := rig.job(41)
	job.Project = nil
	if err := rig.runner.Run(context.Background(), job); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

// TestReviewRunner_RefusesAWorkspaceNoHostCanProvision: a host with nowhere
// to put the reviewer's checkout is a workspace fault, typed as one.
func TestReviewRunner_RefusesAWorkspaceNoHostCanProvision(t *testing.T) {
	rig := newReviewRunnerRig(t)
	job := rig.job(41)
	rig.agents.host.root = ""
	err := rig.runner.Run(context.Background(), job)
	var unavailable *WorkspaceUnavailableError
	if !errors.As(err, &unavailable) || !errors.Is(err, ErrWorkspaceUnfit) ||
		!errors.Is(err, orchestration.ErrSessionWorkspace) {
		t.Fatalf("err = %v, want a workspace fault", err)
	}
}

// TestReviewRunner_NamesTheReviewWorkspaceForTheHead: the reviewer's spawn
// names a workspace for the pull request, the head and the reviewer, and
// carries no path.
func TestReviewRunner_NamesTheReviewWorkspaceForTheHead(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	job := rig.job(41)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	spawn := rig.agents.spawns[0]
	if spawn.WorkDir != "" || spawn.Workspace == nil ||
		spawn.Workspace.Key != reviewWorkspaceKey(41, reviewHead, DefaultReviewerProfileName, fakeReviewerLogin) {
		t.Fatalf("spawn = work_dir %q workspace %+v", spawn.WorkDir, spawn.Workspace)
	}
	provisioned := rig.agents.host.workspaces()
	if len(provisioned) != 1 {
		t.Fatalf("provisioned = %d, want one", len(provisioned))
	}
	if want := filepath.Join(provisioned[0].Dir, testOrchProject().Name); job.WorkDir() != want {
		t.Errorf("job work dir = %q, want the checkout on the executing host %q", job.WorkDir(), want)
	}
}

func TestReviewRunner_RefusesAStateItDoesNotDrive(t *testing.T) {
	rig := newReviewRunnerRig(t)
	job := rig.job(41)
	job.StartState = orchestration.JobStateDecomposing
	err := rig.runner.Run(context.Background(), job)
	var unknown *UnknownRunnerStateError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want UnknownRunnerStateError", err)
	}
}

func TestReviewRunner_ACancelledContextStopsTheRun(t *testing.T) {
	rig := newReviewRunnerRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rig.runner.Run(ctx, rig.job(41)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestReviewRunner_APullRequestWithNoDescriptionAndNoFilesStillPrompts(t *testing.T) {
	// A change the forge reports as touching nothing is itself worth a
	// finding, so the reviewer is told rather than the run refusing.
	rig := newReviewRunnerRig(t)
	rig.forge.prs[41].Body = ""
	rig.forge.files[41] = nil
	rig.reviewerSays(reviewSaying("the forge says this change touches nothing", ReviewCodeRequestChanges))
	rig.mustRun(41)
	prompt := rig.agents.promptTexts()[0]
	if !strings.Contains(prompt, "no description") || !strings.Contains(prompt, "no changed files") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestReviewRunner_TheStatusDescriptionIsBoundedAndSummarises(t *testing.T) {
	rig := newReviewRunnerRig(t)
	headline := strings.Repeat("a finding that goes on. ", 40)
	rig.reviewerSays(reviewSaying("## "+headline, ReviewCodeRequestChanges))
	rig.mustRun(41)
	status, _ := rig.forge.lastStatus()
	if len(status.Description) > maxStatusDescription {
		t.Fatalf("description is %d bytes; forges truncate silently", len(status.Description))
	}
	if !strings.HasSuffix(status.Description, statusEllipsis) {
		t.Fatalf("a cut description does not say it was cut: %q", status.Description)
	}
	if !utf8.ValidString(status.Description) {
		t.Fatal("the cut left a partial rune")
	}
	if !strings.HasPrefix(status.Description, "changes requested: a finding that goes on") {
		t.Fatalf("description = %q", status.Description)
	}
	if status.TargetURL != rig.forge.prs[41].URL {
		t.Fatalf("target = %q", status.TargetURL)
	}
}

func TestFirstLine_SkipsDecorationAndEmptiness(t *testing.T) {
	for input, want := range map[string]string{
		"":                   "",
		"\n\n":               "",
		"## Findings\nbody":  "Findings",
		"\n\n   \nreal line": "real line",
		"**bold**":           "bold",
		"###":                "",
	} {
		if got := firstLine(input); got != want {
			t.Fatalf("firstLine(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestReviewRunner_RefusesAnAdapterThatAnswersWithNothing(t *testing.T) {
	// An adapter answering neither a pull request nor an error breaks
	// GetPullRequest's contract. Both places the runner reads one refuse
	// rather than dereference it.
	t.Run("before the review", func(t *testing.T) {
		rig := newReviewRunnerRig(t)
		rig.forge.getPRNil = true
		if err := rig.run(41); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("before the publish", func(t *testing.T) {
		rig := newReviewRunnerRig(t)
		rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
		rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
			rig.forge.mu.Lock()
			rig.forge.getPRNil = true
			rig.forge.mu.Unlock()
		}
		if err := rig.run(41); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if len(rig.forge.submissions()) != 0 {
			t.Fatal("a verdict was posted against a pull request the forge would not confirm")
		}
	})
}

func TestReviewRunner_AnUnreadableVerdictThatCannotEvenBeNotedIsReported(t *testing.T) {
	// The reviewer produced no verdict AND the forge refused the note. The
	// run reports the forge fault rather than the parse failure, because
	// the forge fault is the one an operator can act on.
	rig := newReviewRunnerRig(t)
	rig.reviewerSays("Looks good to me.")
	rig.forge.submitErr = errors.New("the forge stopped answering")
	err := rig.run(41)
	if err == nil || errors.Is(err, ErrReview) {
		t.Fatalf("err = %v, want the forge fault", err)
	}
}

// =============================================================================
// THE INVARIANT: the verdict and the status cannot disagree
// =============================================================================
//
// This is the single property the whole change exists to guarantee, and the
// one whose absence let #853 and #868 merge over standing rejections. The
// shell workflow computed its exit code and its review verdict SEPARATELY,
// from state read at different moments, so the two could differ — and on the
// run that filed REQUEST_CHANGES the check reported green and auto-merge
// landed rejected code.
//
// Here they come from one decision: submit() posts the verdict and derives
// the status from THAT SAME submission.Verdict, in the same step. The test
// below is written so the two cannot be satisfied independently — the
// expected status is looked up from the verdict the FORGE ACTUALLY RECEIVED,
// not from what the test intended to produce. A runner that posted one
// verdict and published another's colour fails it whichever way round the
// mistake was made.

func TestReviewRunner_TheVerdictAndTheStatusCannotDisagree(t *testing.T) {
	for _, code := range []string{ReviewCodeApprove, ReviewCodeRequestChanges, ReviewCodeComment} {
		t.Run(code, func(t *testing.T) {
			rig := newReviewRunnerRig(t)
			rig.reviewerSays(reviewSaying("what the reviewer found", code))
			rig.mustRun(41)

			submissions := rig.forge.submissions()
			statuses, heads := rig.forge.publishedStatuses()
			if len(submissions) != 1 {
				t.Fatalf("submissions = %+v, want exactly one", submissions)
			}
			if len(statuses) != 1 {
				t.Fatalf("statuses = %+v, want exactly one", statuses)
			}

			// THE EXPECTATION IS DERIVED FROM WHAT WAS POSTED. Nothing here
			// restates what the verdict should have been.
			posted := submissions[0]
			want, mapped := reviewStatuses[posted.Verdict]
			if !mapped {
				t.Fatalf("verdict %s was posted with no status mapping at all", posted.Verdict)
			}
			if statuses[0].Status != want {
				t.Fatalf("posted %s but published %s; the verdict and the check disagree",
					posted.Verdict, statuses[0].Status)
			}

			// AND THEY ARE ABOUT THE SAME COMMIT. A status addressed to a
			// different head would re-colour a tree nobody judged.
			if heads[0] != posted.HeadSHA {
				t.Fatalf("verdict on %s, status on %s", posted.HeadSHA, heads[0])
			}

			// AND THE CHECK IS THE ONE BRANCH PROTECTION REQUIRES.
			if statuses[0].Context != DefaultReviewStatusContext {
				t.Fatalf("context = %q, want %q", statuses[0].Context, DefaultReviewStatusContext)
			}
		})
	}
}

func TestDefaultReviewStatusContext_IsTheNameBranchProtectionRequires(t *testing.T) {
	// Branch protection on main requires this literal string. It is pinned
	// here because changing it silently is the one edit that would leave
	// every protected branch waiting for a check nothing writes.
	if DefaultReviewStatusContext != "quicbaas/review" {
		t.Fatalf("DefaultReviewStatusContext = %q; branch protection requires %q",
			DefaultReviewStatusContext, "quicbaas/review")
	}
}

func TestReviewRunner_TheStatusContextIsProjectConfigurationNotALiteral(t *testing.T) {
	// Onboarding a repository that wants a different check name must never
	// mean editing Go: the name lives on the binding beside the rest of the
	// per-project policy, and the call site reads it rather than a constant.
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) { b.Reviewer.StatusContext = "checks/peer-review" })
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)

	statuses, _ := rig.forge.publishedStatuses()
	if len(statuses) != 1 || statuses[0].Context != "checks/peer-review" {
		t.Fatalf("statuses = %+v; the context was not read from the binding", statuses)
	}
}

func TestReviewRunner_AReassertedVerdictAgreesWithItToo(t *testing.T) {
	// The dedup path publishes a status WITHOUT posting a verdict, which is
	// the one place the two could drift apart without anyone noticing. It
	// re-asserts the standing verdict's own colour, derived from the same
	// table.
	for _, code := range []string{ReviewCodeApprove, ReviewCodeRequestChanges} {
		t.Run(code, func(t *testing.T) {
			rig := newReviewRunnerRig(t)
			rig.reviewerSays(reviewSaying("what the reviewer found", code))
			rig.mustRun(41)
			posted := rig.forge.submissions()[0]

			// A second trigger on the unchanged head re-asserts, never
			// re-derives. The reviewer would now say something else.
			rig.reviewerSays(reviewSaying("I changed my mind", ReviewCodeApprove))
			rig.mustRun(41)

			if got := rig.forge.submissions(); len(got) != 1 {
				t.Fatalf("the re-assertion posted a second verdict: %+v", got)
			}
			statuses, heads := rig.forge.publishedStatuses()
			if len(statuses) != 2 {
				t.Fatalf("statuses = %+v, want the original and the re-assertion", statuses)
			}
			if statuses[1].Status != reviewStatuses[posted.Verdict] {
				t.Fatalf("re-asserted %s over a standing %s", statuses[1].Status, posted.Verdict)
			}
			if heads[1] != posted.HeadSHA {
				t.Fatalf("the re-assertion named %s, the verdict named %s", heads[1], posted.HeadSHA)
			}
		})
	}
}

func TestReviewStatuses_NoVerdictIsGreenExceptAnApproval(t *testing.T) {
	// The table read as a whole. Green is the one state that lets a change
	// land, so exactly one verdict may produce it.
	green := 0
	for verdict, status := range reviewStatuses {
		if status != CheckSuccess {
			continue
		}
		green++
		if verdict != VerdictApprove {
			t.Fatalf("verdict %s publishes a green check", verdict)
		}
	}
	if green != 1 {
		t.Fatalf("%d verdicts publish green, want exactly one", green)
	}
}

// =============================================================================
// The tree the reviewer read is the tree the verdict is about
// =============================================================================
//
// An author who pushes between the head this run captured and the moment the
// container clones moves the branch. Everything the run then says — the
// verdict, the commit status, the gate record — names the captured head, so
// a checkout that followed the branch instead would attach a judgement to a
// tree the reviewer never opened. That is worse than no verdict: it is a
// confident answer about code nobody read.

// branchMovesTo makes the forge's branch already hold a commit the run never
// captured by the time the container fetches, which is what a push landing
// inside the review window looks like from the checkout's side. reachable
// lists the commits the remote still has: a plain push keeps the captured
// one, a force-push does not.
func (r *reviewRunnerRig) branchMovesTo(moved string, reachable ...string) {
	r.t.Helper()
	r.container.remoteBranches = map[string]string{"issue/25-fix": moved}
	r.container.knownCommits = map[string]bool{moved: true}
	for _, sha := range reachable {
		r.container.knownCommits[sha] = true
	}
}

func TestReviewRunner_APushDuringTheReviewDoesNotMoveTheTreeUnderTheVerdict(t *testing.T) {
	const pushedSince = "head-sha-9999999999999999"

	rig := newReviewRunnerRig(t)
	rig.branchMovesTo(pushedSince, reviewHead)
	rig.reviewerSays(reviewSaying("I read every file and it is correct.", ReviewCodeApprove))
	rig.mustRun(41)

	submissions := rig.forge.submissions()
	if len(submissions) != 1 {
		t.Fatalf("submissions = %+v, want exactly one", submissions)
	}

	// THE OBSERVABLE PROPERTY, and the two halves cannot be satisfied
	// independently: the commit the checkout ended on is read from the
	// container, and the commit the verdict names is read from the forge.
	// A checkout that followed the branch makes them differ.
	if head := rig.container.currentHead(); head != submissions[0].HeadSHA {
		t.Fatalf("the reviewer read %q and the verdict was posted on %q; it judged a tree it never opened",
			head, submissions[0].HeadSHA)
	}

	// And the commit status is on the same object, so branch protection is
	// colouring the tree that was judged.
	statuses, heads := rig.forge.publishedStatuses()
	if len(statuses) != 1 || heads[0] != submissions[0].HeadSHA {
		t.Fatalf("status heads = %+v, verdict head = %q", heads, submissions[0].HeadSHA)
	}
}

func TestReviewRunner_AForcePushThatDropsTheCapturedHeadRefusesRatherThanJudgesAnother(t *testing.T) {
	const forcePushed = "head-sha-9999999999999999"

	rig := newReviewRunnerRig(t)
	// The captured head is NOT reachable any more: the author rewrote the
	// branch. There is no tree to review, and substituting the one that
	// replaced it is the failure this refuses.
	rig.branchMovesTo(forcePushed)
	rig.reviewerSays(reviewSaying("I read every file and it is correct.", ReviewCodeApprove))

	if err := rig.run(41); err == nil {
		t.Fatal("the review succeeded after the branch was rewritten under it")
	}

	if got := rig.forge.submissions(); len(got) != 0 {
		t.Fatalf("submissions = %+v; a verdict was posted about a tree that no longer exists", got)
	}
	statuses, _ := rig.forge.publishedStatuses()
	for _, status := range statuses {
		if status.Status == CheckSuccess {
			t.Fatalf("a green check was published for a tree nobody read: %+v", status)
		}
	}
}

func TestReviewRunner_TheReviewerIsNeverPromptedBeforeTheHeadIsProven(t *testing.T) {
	// The order is the guarantee. If the prompt could go out first, the
	// reviewer would spend a model run reading whatever the clone produced
	// and the refusal would arrive too late to have prevented anything.
	const forcePushed = "head-sha-9999999999999999"

	rig := newReviewRunnerRig(t)
	rig.branchMovesTo(forcePushed)
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))

	if err := rig.run(41); err == nil {
		t.Fatal("the review succeeded after the branch was rewritten under it")
	}
	if prompts := len(rig.agents.promptTexts()); prompts != 0 {
		t.Fatalf("the reviewer was prompted %d time(s) on a checkout that could not be pinned", prompts)
	}
}

// =============================================================================
// One pull request, one reviewer at a time
// =============================================================================
//
// A forge redelivers, and an opened pull request that is immediately labelled
// or retargeted produces several deliveries within a second. Each starts a
// job. The standing-verdict skip cannot tell them apart, because when either
// looks, neither has published anything — so both spend a model run and both
// write a commit status under one login, and the later write wins. That is how
// a green check lands over a rejection that was being filed while it ran.

// deliverUnderLease is what a node does with a review job: it takes the lease
// the RUNNER names for the job and runs the runner only if it got it.
//
// The envelope's own half of this — that it asks the runner for the subject at
// all, and declines a contender — is proved next door in
// TestJobLeasePolicy_TwoJobsOnOneSubjectAreOneRun, against the real envelope.
// What is proved here is the consequence at the forge, which is the thing that
// actually went wrong.
func (r *reviewRunnerRig) deliverUnderLease(ctx context.Context, leases orchestration.JobLeaseStore, jobID string, pullRequest int) error {
	r.t.Helper()
	job := r.jobAs(jobID, pullRequest)
	ref := r.runner.LeaseSubject(job.Config)
	if ref == "" {
		// What the envelope does when the runner names no subject and the
		// job is bound to no task: a lease keyed on the job itself, which
		// contends with nothing. Stated here so that removing the runner's
		// subject reproduces THE DEFECT rather than an invalid lease.
		ref = jobID
	}
	lease := &orchestration.JobLease{
		TenantID:   testTenant,
		ProjectID:  testProject,
		Workflow:   ReviewJobRunnerName,
		Ref:        ref,
		JobID:      jobID,
		Generation: 1,
		Node:       "node-a",
		OffBudget:  r.runner.OffProjectBudget(),
	}
	if err := leases.Acquire(ctx, lease); err != nil {
		return err
	}
	defer func() {
		if err := leases.Release(context.WithoutCancel(ctx), lease); err != nil {
			r.t.Errorf("release: %v", err)
		}
	}()
	return r.runner.Run(ctx, job)
}

func TestReviewRunner_TwoConcurrentDeliveriesAreOneReviewAndOneVerdict(t *testing.T) {
	ctx := context.Background()
	leases, err := orchestration.NewProcessJobLeaseStore(time.Minute)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}

	rig := newReviewRunnerRig(t)
	// The first reviewer is held at its prompt, so the second delivery
	// arrives while a reviewer is demonstrably still reading — the window
	// the standing-verdict skip cannot see into.
	prompted := make(chan struct{})
	release := make(chan struct{})
	// ONLY THE FIRST PROMPT WAITS. A gate that held every caller — a
	// sync.Once around the wait does exactly that — would DEADLOCK rather
	// than fail if a second reviewer were wrongly admitted, and a regression
	// test that hangs reports nothing.
	var prompts atomic.Int64
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		if prompts.Add(1) == 1 {
			close(prompted)
			<-release
		}
	}
	rig.reviewerSays(reviewSaying("this change is wrong and here is why", ReviewCodeRequestChanges))

	firstDone := make(chan error, 1)
	go func() { firstDone <- rig.deliverUnderLease(ctx, leases, "job-first", 41) }()
	<-prompted

	secondErr := rig.deliverUnderLease(ctx, leases, "job-second", 41)
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("the first review: %v", err)
	}

	var taken *orchestration.JobLeaseTakenError
	if !errors.As(secondErr, &taken) {
		t.Fatalf("the second delivery got %T %v, want the review already claimed", secondErr, secondErr)
	}

	// ONE REVIEWER, ONE VERDICT, ONE STATUS — read off the forge and the
	// agent service, not off the runner's own bookkeeping.
	if sessions := rig.agents.sessionCount(); sessions != ReviewSessionCount {
		t.Fatalf("%d reviewers were spawned for one pull request, want %d", sessions, ReviewSessionCount)
	}
	submissions := rig.forge.submissions()
	if len(submissions) != 1 {
		t.Fatalf("submissions = %+v, want exactly one", submissions)
	}

	// AND THE REJECTION STANDS. The expected colour is looked up from the
	// verdict the forge actually received, so the two cannot be satisfied
	// independently — and every status is checked, not just the last, because
	// the failure being pinned is a later write overwriting an earlier one.
	statuses, heads := rig.forge.publishedStatuses()
	want := reviewStatuses[submissions[0].Verdict]
	if want == CheckSuccess {
		t.Fatalf("the fixture posted %s, which cannot demonstrate a rejection", submissions[0].Verdict)
	}
	for i, status := range statuses {
		if status.Status != want {
			t.Fatalf("status %d on %s is %s, want %s: a concurrent run wrote over the verdict",
				i, heads[i], status.Status, want)
		}
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %+v, want exactly the one the verdict implies", statuses)
	}
}

func TestReviewRunner_TheLeaseSubjectIsOnePerPullRequest(t *testing.T) {
	// The subject is what makes two deliveries contend, so what it is a
	// function OF is the whole of the claim: one pull request of one project.
	rig := newReviewRunnerRig(t)
	subjectOf := func(projectID string, pullRequest int) string {
		job := rig.jobAs("job-x", pullRequest)
		job.Config.ProjectID = projectID
		return rig.runner.LeaseSubject(job.Config)
	}

	if subjectOf(testProject, 41) != subjectOf(testProject, 41) {
		t.Fatal("two deliveries about one pull request name different subjects, so neither excludes the other")
	}
	for _, other := range []struct {
		name      string
		projectID string
		pr        int
	}{
		{"another pull request", testProject, 42},
		{"another project", "proj-other", 41},
	} {
		if subjectOf(other.projectID, other.pr) == subjectOf(testProject, 41) {
			t.Fatalf("%s shares a subject with pull request 41, so one would block the other", other.name)
		}
	}

	// A configuration with no pull request names NO subject, so the envelope
	// leases such a job by its own id rather than collapsing every malformed
	// review job in the tenant onto one lease.
	blank := rig.jobAs("job-x", 41)
	blank.Config.Params = nil
	if subject := rig.runner.LeaseSubject(blank.Config); subject != "" {
		t.Fatalf("a job naming no pull request claimed %q", subject)
	}
	if subject := rig.runner.LeaseSubject(nil); subject != "" {
		t.Fatalf("a nil configuration claimed %q", subject)
	}
}

func TestReviewRunner_AReviewDoesNotSpendTheProjectsWorkerBudget(t *testing.T) {
	// Quotas.MaxConcurrency bounds a project's development work. A review
	// that counted against it would sit behind the worker whose pull request
	// it has to review, and the queue would never drain.
	rig := newReviewRunnerRig(t)
	if !rig.runner.OffProjectBudget() {
		t.Fatal("a review spends a worker slot, so a project at max_concurrency 1 cannot review its own work")
	}
}

// =============================================================================
// A head that moved under the review is still reviewed
// =============================================================================
//
// Serializing on the pull request means the holder is the ONLY job that can
// review it while it runs. So a head the author pushes mid-review would be
// left with no verdict at all if the holder stopped at the head it started
// on — which would trade one defect for a worse one. It does not stop: it
// sees the move when it re-reads the pull request to publish, and reviews the
// new head itself.

// authorPushes makes the forge's pull request advance to a new head on every
// read after the first, which is an author pushing throughout the review.
// The heads are returned in the order they became current.
func (r *reviewRunnerRig) authorPushes(heads ...string) {
	r.t.Helper()
	var reads int
	r.forge.onGetPullRequest = func(pr *ForgePullRequest) {
		if reads < len(heads) {
			pr.HeadSHA = heads[reads]
		}
		reads++
	}
}

func TestReviewRunner_AHeadPushedDuringTheReviewIsReviewedInTheSameJob(t *testing.T) {
	const pushed = "head-sha-2222222222222222"

	rig := newReviewRunnerRig(t)
	// Read 1 (initializing) sees the original head; read 2 (publishing)
	// finds the author has pushed; reads 3 and 4 are the second round on the
	// new head, which then holds still.
	rig.authorPushes(reviewHead, pushed, pushed, pushed)
	rig.reviewerSays(reviewSaying("what the reviewer found", ReviewCodeRequestChanges))
	rig.mustRun(41)

	// BOTH HEADS CARRY A VERDICT, in the order they became current, and each
	// verdict's status is on its own commit.
	submissions := rig.forge.submissions()
	if len(submissions) != 2 {
		t.Fatalf("submissions = %+v, want one per head", submissions)
	}
	if submissions[0].HeadSHA != reviewHead || submissions[1].HeadSHA != pushed {
		t.Fatalf("verdicts landed on %q then %q, want %q then %q",
			submissions[0].HeadSHA, submissions[1].HeadSHA, reviewHead, pushed)
	}
	statuses, heads := rig.forge.publishedStatuses()
	if len(statuses) != 2 {
		t.Fatalf("statuses = %+v, want one per head", statuses)
	}
	for i, submission := range submissions {
		if heads[i] != submission.HeadSHA {
			t.Fatalf("verdict %d is on %s and its status on %s", i, submission.HeadSHA, heads[i])
		}
		if statuses[i].Status != reviewStatuses[submission.Verdict] {
			t.Fatalf("verdict %d posted %s and published %s", i, submission.Verdict, statuses[i].Status)
		}
	}

	// A FRESH REVIEWER PER HEAD. One carrying the previous head's
	// conversation would be answering about two trees.
	if sessions := rig.agents.sessionCount(); sessions != 2 {
		t.Fatalf("%d reviewers read two heads, want one each", sessions)
	}
}

func TestReviewRunner_ABranchThatNeverStopsMovingLeavesTheCheckPendingNotGreen(t *testing.T) {
	rig := newReviewRunnerRig(t)
	// The head advances on EVERY read, so the job never catches up. The
	// round budget is what stops it spending reviewers for ever.
	var reads int
	rig.forge.onGetPullRequest = func(pr *ForgePullRequest) {
		reads++
		pr.HeadSHA = "head-sha-" + strconv.Itoa(reads)
	}
	rig.reviewerSays(reviewSaying("what the reviewer found", ReviewCodeApprove))
	rig.mustRun(41)

	if got := len(rig.forge.submissions()); got != MaxReviewRounds {
		t.Fatalf("%d verdicts were produced, want the round budget of %d", got, MaxReviewRounds)
	}

	// THE ENDING IS PENDING ON THE HEAD NOBODY REVIEWED, not green and not
	// absent: the change is unreviewed at that commit, and a required check
	// that is missing reads on some platforms as nothing to wait for.
	statuses, heads := rig.forge.publishedStatuses()
	last := len(statuses) - 1
	if statuses[last].Status != CheckPending {
		t.Fatalf("the unreviewed head's check is %s, want pending", statuses[last].Status)
	}
	for _, submission := range rig.forge.submissions() {
		if submission.HeadSHA == heads[last] {
			t.Fatalf("head %s was both reviewed and reported unreviewed", heads[last])
		}
	}
	// And no green check was ever published for a head the reviewer did not
	// finish with, which is what branch protection would act on.
	if statuses[last].Status == CheckSuccess {
		t.Fatal("a green check was published for an unreviewed head")
	}
}

// TestReviewRunner_ClonesAnSSHHeadItCanAuthenticateAndSignsNothing is the
// reviewer's half of go-quicbaas#929.
//
// The reviewer was handed no agent on purpose — "nothing in it can produce a
// signature" — and on an ssh remote that left it unable to clone the head it
// was asked to read. It now asks for the CHECKOUT scope: the project's access
// key, no commit signing key, and the agent stopped before the reviewer is
// prompted, so it reads an untrusted change with a socket that answers nothing.
func TestReviewRunner_ClonesAnSSHHeadItCanAuthenticateAndSignsNothing(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.repoURL = sshForgeRepoURL
	rig.rebind(func(b *ProjectBinding) { b.Forge.SSHHostKey = testSSHHostKey })
	rig.reviewerSays(reviewSaying("Reads correctly.", ReviewCodeApprove))

	stoppedAtFirstPrompt := -1
	rig.agents.mu.Lock()
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		if stoppedAtFirstPrompt < 0 {
			stoppedAtFirstPrompt = rig.signing.stopCount()
		}
	}
	rig.agents.mu.Unlock()

	rig.mustRun(41)

	clones := rig.container.clonesMade()
	if len(clones) != 1 || clones[0].URL != sshForgeRepoURL {
		t.Fatalf("clones = %+v, want the head cloned over ssh", clones)
	}
	scopes := rig.signing.startedScopes()
	if len(scopes) != 1 || scopes[0] != SigningScopeCheckout {
		t.Fatalf("scopes = %v, want exactly one checkout-scoped agent", scopes)
	}
	if stoppedAtFirstPrompt != 1 {
		t.Fatalf("agents stopped before the first prompt = %d, want 1", stoppedAtFirstPrompt)
	}
}

// TestIssueNumberFromBranch reads back what issueBranch wrote.
func TestIssueNumberFromBranch(t *testing.T) {
	for name, tc := range map[string]struct {
		ref  string
		want int
		ok   bool
	}{
		"a branch this module named":      {"issue/101-addkey-discards-the-keyslot", 101, true},
		"whitespace is trimmed":           {"  issue/7-x  ", 7, true},
		"a branch with no slug":           {"issue/42-", 42, true},
		"someone else's branch":           {"feature/add-a-thing", 0, false},
		"the prefix with no number":       {"issue/addkey-thing", 0, false},
		"a zero issue is not an issue":    {"issue/0-nothing", 0, false},
		"a negative number is not parsed": {"issue/-3-x", 0, false},
		"empty":                           {"", 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := issueNumberFromBranch("issue/", tc.ref)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("issueNumberFromBranch(%q) = %d,%v want %d,%v", tc.ref, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestReviewPrompt_CarriesTheIssueItMustJudgeAgainst pins the input whose
// absence blocked merges on changes that had nothing wrong with them.
//
// The reviewer's charter tells it to check the change against the issue. The
// container holds no forge credential, so every attempt to fetch one returned
// 404 — and a reviewer that had read all 21 files and found no defect still
// refused to approve, because it could not confirm the change closed the
// issue. The criteria now travel in the prompt.
func TestReviewPrompt_CarriesTheIssueItMustJudgeAgainst(t *testing.T) {
	run := &reviewRun{
		binding: &ProjectBinding{Forge: ForgeConfig{Owner: "jeremyhahn", Repo: "go-luks2"}},
		pr: &ForgePullRequest{
			Number: 200, Title: "widen AddKey", HeadRef: "issue/101-addkey", BaseRef: "main",
			Body: "closes #101",
		},
		headSHA: "abc123",
		issue: &ForgeIssue{
			Number: 101, Title: "AddKey discards the keyslot it chose",
			Body: "ACCEPTANCE: AddKey returns (int, error) and the slot is the one written.",
		},
		discussion: []ForgeComment{
			{Author: "owner", Body: "Ruling: widen the signature, do not add a second method."},
		},
	}
	prompt := run.reviewPrompt()
	for _, want := range []string{
		"The issue this change must satisfy",
		"#101 AddKey discards the keyslot it chose",
		"ACCEPTANCE: AddKey returns (int, error)",
		// The ruling lives in a comment, not the body.
		"Ruling: widen the signature",
		"owner",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the review prompt does not carry %q:\n%s", want, prompt)
		}
	}
}

// TestReviewPrompt_SaysWhenTheIssueIsMissingAndStillDemandsAVerdict keeps a
// missing input from becoming a refused review.
//
// A reviewer left to infer that the criteria are absent reports it as an
// unresolved question and declines to judge, which satisfies no gate and
// strands a finished change. Told plainly, it judges the code and marks
// conformance unverified.
func TestReviewPrompt_SaysWhenTheIssueIsMissingAndStillDemandsAVerdict(t *testing.T) {
	run := &reviewRun{
		binding: &ProjectBinding{Forge: ForgeConfig{Owner: "jeremyhahn", Repo: "go-luks2"}},
		pr: &ForgePullRequest{
			Number: 9, Title: "a change", HeadRef: "feature/no-issue", BaseRef: "main",
		},
		headSHA:          "deadbee",
		issueUnavailable: `the branch "feature/no-issue" was not named by this module`,
	}
	prompt := run.reviewPrompt()
	if !strings.Contains(prompt, "NOT AVAILABLE") {
		t.Fatalf("the prompt must say the issue is absent:\n%s", prompt)
	}
	if !strings.Contains(prompt, `"feature/no-issue"`) {
		t.Fatalf("the prompt must name WHY it is absent:\n%s", prompt)
	}
	for _, want := range []string{
		"Do not withhold a verdict",
		"conformance to the issue is unverified",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the prompt must still demand a verdict (%q):\n%s", want, prompt)
		}
	}
}

// TestDiscussionSection_KeepsTheNewestComments proves the bound drops the
// oldest, because a ruling supersedes the question it answers.
func TestDiscussionSection_KeepsTheNewestComments(t *testing.T) {
	comments := make([]ForgeComment, 0, maxIssueComments+5)
	for i := range maxIssueComments + 5 {
		comments = append(comments, ForgeComment{Author: "a", Body: "comment-" + strconv.Itoa(i)})
	}
	run := &reviewRun{issue: &ForgeIssue{Number: 1, Title: "t", Body: "b"}, discussion: comments}
	got := run.discussionSection()
	if !strings.Contains(got, "comment-"+strconv.Itoa(maxIssueComments+4)) {
		t.Fatal("the newest comment must survive the bound")
	}
	if strings.Contains(got, "comment-0\n") {
		t.Fatal("the oldest comment must be dropped, not the newest")
	}
	if !strings.Contains(got, "earlier comment(s) omitted") {
		t.Fatalf("a cut discussion must say it cut:\n%s", got)
	}
}

// TestTruncateForPrompt_SaysItCut keeps a truncated record from reading as a
// complete one.
func TestTruncateForPrompt_SaysItCut(t *testing.T) {
	if got := truncateForPrompt("short", 100); got != "short" {
		t.Fatalf("an unbounded value must pass through unchanged, got %q", got)
	}
	got := truncateForPrompt(strings.Repeat("x", 200), 50)
	if !strings.Contains(got, "truncated at 50 bytes") {
		t.Fatalf("a cut value must say so, got %q", got)
	}
}

// TestReviewRun_DecorateNamesTheReviewerSessionAsTheLead: the reviewer
// session is the job's only session, so it is the job's lead. The job page
// badges and links the lead; a review job that never named one rendered no
// badge (go-quicbaas#863).
func TestReviewRun_DecorateNamesTheReviewerSessionAsTheLead(t *testing.T) {
	run := &reviewRun{}
	run.decorate(nil)
	status := &orchestration.JobStatus{}
	run.decorate(status)
	if len(status.SessionIDs) != 0 || status.LeadSessionID != "" {
		t.Fatalf("a run with no session decorated one: %v lead=%q", status.SessionIDs, status.LeadSessionID)
	}
	run.sessionID = "reviewer-1"
	run.decorate(status)
	if len(status.SessionIDs) != 1 || status.SessionIDs[0] != "reviewer-1" {
		t.Fatalf("session ids = %v", status.SessionIDs)
	}
	if status.LeadSessionID != "reviewer-1" {
		t.Fatalf("lead session = %q, want the reviewer session", status.LeadSessionID)
	}
}

// TestReviewRunner_ASeatWhoseAgentCannotReviewFailsOverToTheNext: seat order is
// the failover chain. The default seat's Agent is rate-limited, so the review
// runs as the next counting seat — its own Agent and its own forge identity —
// and the switch is on the job's audit trail.
func TestReviewRunner_ASeatWhoseAgentCannotReviewFailsOverToTheNext(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) {
		b.Reviewers = []ReviewerConfig{
			{Name: "advisor", Agent: testQwenAgent, Login: "qwen-bot", TokenSecretRef: "qwen-tok"},
			{Name: "backup", Agent: testBackupReviewerAgent, Login: "backup-bot", TokenSecretRef: "backup-tok",
				CountsTowardMerge: boolPtr(true)},
		}
		b.Review.Reviewers["backup-bot"] = ModelFamilyOpenAI
	})
	rig.agents.spawnErrFor = map[string]error{testReviewerAgent: &orchestration.AccountLimitedError{AccountID: "acct-1"}}
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)

	var spawned []string
	for _, spawn := range rig.agents.spawns {
		spawned = append(spawned, spawn.AgentID)
	}
	if !slices.Equal(spawned, []string{testReviewerAgent, testBackupReviewerAgent}) {
		t.Fatalf("spawned %v, want the default seat then the counting backup (never the advisory seat)", spawned)
	}
	if refs := rig.forges.resolvedTokenRefs(); !slices.Contains(refs, "backup-tok") {
		t.Fatalf("the backup seat must post as itself, forge reached as %v", refs)
	}
	switches := rig.auditDetails(auditReviewSeatFailover)
	if len(switches) != 1 || !strings.Contains(switches[0], "default ("+testReviewerAgent+") -> backup") ||
		!strings.Contains(switches[0], orchestration.AgentFailoverRateLimited) {
		t.Fatalf("failover audit = %v", switches)
	}
}

// TestReviewRunner_ASeatWithNoSuccessorReturnsItsRefusal: the chain ends, and
// the run fails with the Agent's own refusal rather than a generic one.
func TestReviewRunner_ASeatWithNoSuccessorReturnsItsRefusal(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.agents.spawnErrFor = map[string]error{testReviewerAgent: orchestration.ErrAccountsSpent}
	err := rig.run(41)
	if !errors.Is(err, orchestration.ErrAccountsSpent) {
		t.Fatalf("err = %v, want the seat's own refusal", err)
	}
	if switches := rig.auditDetails(auditReviewSeatFailover); len(switches) != 1 || !strings.Contains(switches[0], "no seat") {
		t.Fatalf("the dead end must be audited: %v", switches)
	}
}

// TestReviewRunner_ARefusalTheNextSeatWouldShareDoesNotFailOver: a policy
// denial or a workspace fault would refuse every seat identically.
func TestReviewRunner_ARefusalTheNextSeatWouldShareDoesNotFailOver(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) {
		b.Reviewers = []ReviewerConfig{{Name: "backup", Agent: testBackupReviewerAgent, Login: "backup-bot",
			TokenSecretRef: "backup-tok", CountsTowardMerge: boolPtr(true)}}
	})
	boom := &orchestration.InputValidationError{Field: "workdir", Message: "refused"}
	rig.agents.spawnErrFor = map[string]error{testReviewerAgent: boom}
	if err := rig.run(41); !errors.Is(err, orchestration.ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
	if len(rig.agents.spawns) != 1 {
		t.Fatalf("spawned %d sessions; a shared refusal must not be retried", len(rig.agents.spawns))
	}
}

// TestReviewRunner_FailoverPassesOverASeatThatCannotCount: a counting seat
// from the author's own family is skipped, not taken.
func TestReviewRunner_FailoverPassesOverASeatThatCannotCount(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.rebind(func(b *ProjectBinding) {
		b.Reviewers = []ReviewerConfig{
			{Name: "claude", Agent: testClaudeReviewerAgent, Login: "claude-bot2", TokenSecretRef: "claude-tok", CountsTowardMerge: boolPtr(true)},
			{Name: "backup", Agent: testBackupReviewerAgent, Login: "backup-bot", TokenSecretRef: "backup-tok", CountsTowardMerge: boolPtr(true)},
		}
		b.Review.Reviewers["backup-bot"] = ModelFamilyOpenAI
	})
	rig.agents.spawnErrFor = map[string]error{testReviewerAgent: orchestration.ErrNoAccountAvailable}
	rig.reviewerSays(reviewSaying("fine", ReviewCodeApprove))
	rig.mustRun(41)
	last := rig.agents.spawns[len(rig.agents.spawns)-1]
	if last.AgentID != testBackupReviewerAgent {
		t.Fatalf("the run reviewed as %q, want the backup past the refused claude seat", last.AgentID)
	}
	if switches := rig.auditDetails(auditReviewSeatFailover); len(switches) != 2 || !strings.Contains(switches[0], "passed over seat claude") {
		t.Fatalf("failover audit = %v", switches)
	}
}
