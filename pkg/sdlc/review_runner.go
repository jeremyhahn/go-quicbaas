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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The review JobRunner — one pull request, one head, one verdict
// =============================================================================
//
// A REVIEW IS NOT A DELIVERY. The sdlc runner drives an issue to a merged
// pull request and holds a worker that writes code; this drives one pull
// request to one verdict on one head and holds a reviewer that writes none.
// Different shape, same envelope: the lease, the heartbeat, the cost
// accounting, the audit trail, the session record, the stall detector and the
// retry all belong to the engine and are not restated here.
//
// WHAT THIS RUNNER OWNS is the order of four things — decide whether a review
// is needed, produce one, publish it, and say what the gate now makes of the
// pull request — and the fail-closed answer at each. Everything else is
// borrowed: the verdict is parsed by review_verdict.go, the merge decision is
// EvaluateReviewGate's, the checkout is the same cloner the delivery runner
// drives, and the forge writes are the adapter's.
//
// THE REVIEWER IS A DIFFERENT PRINCIPAL FROM THE WORKER, and this is the one
// place that is arranged. The binding names the reviewer's own forge token
// (ReviewerConfig.TokenSecretRef); the adapter this runner uses is built from
// it, so every read and every write on this path is attributed to the
// reviewer's account. A change cannot approve itself by configuration
// accident.
//
// THE REVIEWER SIGNS NOTHING. No signing agent is started and none is
// mounted, because a reviewer never commits: the session that reads a
// candidate change is the session with the least reason in the whole system
// to hold a key. It is also why this runner needs no sockets root, which the
// delivery runner cannot do without.

// reviewStateLabels name the runner's states for a person reading a job.
const (
	labelReviewInitializing = "Preparing review"
	labelReviewExecuting    = "Reviewing"
	labelReviewPublishing   = "Publishing verdict"
	labelReviewCleanup      = "Cleanup"
)

// ReviewSessionCount is how many sessions a review job holds AT A TIME: one.
const ReviewSessionCount = 1

// MaxReviewRounds bounds how many heads one review job will judge.
//
// A JOB IS RESPONSIBLE FOR ITS PULL REQUEST, NOT FOR ONE HEAD OF IT. The job
// holds the pull request's lease (see ReviewLeaseSubject), so while it runs
// no other job can review the same pull request — which means a head the
// author pushes mid-review would be left with no verdict at all if this job
// simply stopped at the head it started on. So it does not: when the
// pull request it re-reads to publish has moved, it reviews the new head.
//
// The bound is what keeps that from becoming unbounded spending on a branch
// somebody is pushing to continuously. On reaching it the job publishes a
// pending status on the current head — saying plainly that the change kept
// moving — and ends, which RELEASES the lease so the next delivery starts a
// fresh job. Nothing is stranded either way.
const MaxReviewRounds = 3

// auditReviewVerdict is the audit event a produced verdict is recorded under.
const auditReviewVerdict = "sdlc:review_verdict"

// auditReviewGate is the audit event the gate's answer is recorded under.
const auditReviewGate = "sdlc:review_gate"

// ReviewRunnerConfig wires the runner to what it borrows.
type ReviewRunnerConfig struct {
	// Bindings supplies the project's reviewer and review policy.
	Bindings BindingReader

	// Forges builds the adapter for a forge configuration, resolving
	// whichever token that configuration names — which is how the reviewer's
	// identity is applied.
	Forges ForgeResolver

	// Tokens resolves the reviewer's forge token as a value, for the
	// container's clone.
	Tokens ForgeTokenReader

	// Signing starts the ephemeral, KMS-backed ssh agent the reviewer's
	// container AUTHENTICATES ITS CLONE against when the project's remote is
	// ssh. It is required, for the reason go-quicbaas#929 records: a reader
	// with no agent cannot clone an ssh remote, and it finds that out inside a
	// container it has already paid for. The scope is SigningScopeCheckout and
	// the agent is stopped as soon as the checkout is made.
	Signing SigningAgent

	// CheckoutRoot is the directory the reviewer's checkout is mounted at,
	// as the CONTAINER sees it. Empty takes the engine's default.
	CheckoutRoot string

	// SocketsRoot is the DAEMON's root for per-session agent sockets
	// (orchestration.sockets_root, resolved by the host). It is required and
	// has no default here; see RunnerConfig.SocketsRoot.
	SocketsRoot string
}

// ReviewRunner is the module's review-side orchestration.JobRunner.
type ReviewRunner struct {
	bindings BindingReader
	forges   ForgeResolver

	// access is the one seam every runner's container reaches the forge
	// through — the same one the delivery runner and triage hold — so the
	// three cannot authenticate differently (go-quicbaas#929).
	access forgeAccess
}

var (
	_ orchestration.JobRunner      = (*ReviewRunner)(nil)
	_ orchestration.JobResumer     = (*ReviewRunner)(nil)
	_ orchestration.JobLeasePolicy = (*ReviewRunner)(nil)
)

// NewReviewRunner builds the runner; every collaborator is required.
func NewReviewRunner(cfg ReviewRunnerConfig) (*ReviewRunner, error) {
	switch {
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "the reviewer and the review policy live on the binding"}
	case cfg.Forges == nil:
		return nil, &InvalidInputError{Field: "forges", Message: "the verdict is read from and written to the forge"}
	}
	access, err := newForgeAccess(cfg.Tokens, cfg.Signing, cfg.CheckoutRoot, cfg.SocketsRoot)
	if err != nil {
		return nil, err
	}
	return &ReviewRunner{bindings: cfg.Bindings, forges: cfg.Forges, access: access}, nil
}

// Name implements orchestration.JobRunner.
func (*ReviewRunner) Name() string { return ReviewJobRunnerName }

// States implements orchestration.JobRunner.
func (*ReviewRunner) States() []orchestration.JobStateSpec { return ReviewRunnerStates() }

// ReviewRunnerStates is the review sequence, in order.
//
// ONLY EXECUTING IS SESSION-BACKED, which is the declaration the stall
// detector acts on: a review job past its bound while a reviewer is meant to
// be reading has lost the session it is waiting on and is ended. The other
// three are the daemon's own work — reading the forge and spawning, writing
// two records back, tearing a session down — and each finishes correctly
// whether or not the reviewer is still there.
//
// The bound on executing is much shorter than the delivery runner's on the
// same state. A reviewer reads a bounded change and answers once; it does not
// iterate, does not run a build and takes no review rounds of its own, so an
// hour of silence is a reviewer that has gone rather than one that is
// thorough.
func ReviewRunnerStates() []orchestration.JobStateSpec {
	return []orchestration.JobStateSpec{
		{State: orchestration.JobStateInitializing, Label: labelReviewInitializing, IdleThreshold: 10 * time.Minute},
		{State: orchestration.JobStateExecuting, Label: labelReviewExecuting, SessionBacked: true, IdleThreshold: time.Hour},
		{State: orchestration.JobStateCompleting, Label: labelReviewPublishing, IdleThreshold: 10 * time.Minute},
		{State: orchestration.JobStateCleanup, Label: labelReviewCleanup, IdleThreshold: 10 * time.Minute},
	}
}

// Validate implements orchestration.JobRunner.
func (*ReviewRunner) Validate(_ context.Context, cfg *orchestration.JobConfig) error {
	return ValidateReviewJobConfig(cfg)
}

// ValidateReviewJobConfig refuses a job configuration this runner cannot
// drive, before any goroutine or lease exists.
func ValidateReviewJobConfig(cfg *orchestration.JobConfig) error {
	if cfg == nil {
		return &InvalidInputError{Field: "config", Message: "must not be nil"}
	}
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return &InvalidInputError{
			Field:   "project_id",
			Message: "the repository, the reviewer and the review policy all hang off the project",
		}
	}
	_, err := ReviewJobSubjectFrom(cfg)
	return err
}

// ResumeState implements orchestration.JobResumer.
//
// EVERY INTERRUPTED REVIEW RESTARTS AT THE BEGINNING, and that is cheap and
// correct rather than lazy. A resumed run holds no reviewer session, so
// nothing after initializing can proceed without spawning one anyway; and
// the first thing initializing does is ask the forge whether this head
// already carries a verdict from this reviewer, so a run interrupted AFTER
// the verdict was published finds it, publishes nothing twice, and finishes.
// The forge is the record, so no state of the run's own has to survive it.
func (*ReviewRunner) ResumeState(orchestration.JobState) orchestration.JobState {
	return orchestration.JobStateInitializing
}

// LeaseSubject implements orchestration.JobLeasePolicy: every review job of
// one pull request holds one lease, so a second delivery cannot put a second
// reviewer on it. See ReviewLeaseSubject for why the subject is the pull
// request and not the head.
//
// AN UNREADABLE CONFIGURATION NAMES NO SUBJECT, and the envelope then leases
// the job by its own id. That is not a hole: Validate refuses the same
// configuration before any lease exists, so a job that reaches here has a
// pull request number. Answering with a constant instead would make every
// malformed review job in the tenant contend with every other one.
func (*ReviewRunner) LeaseSubject(cfg *orchestration.JobConfig) string {
	if cfg == nil {
		return ""
	}
	subject, err := ReviewJobSubjectFrom(cfg)
	if err != nil {
		return ""
	}
	return ReviewLeaseSubject(strings.TrimSpace(cfg.ProjectID), subject.PullRequest)
}

// OffProjectBudget implements orchestration.JobLeasePolicy: a review does not
// spend one of the project's worker slots.
//
// THE WORK THAT UNBLOCKS THE PIPELINE MUST NOT BE STARVED BY THE WORK IT
// UNBLOCKS. Quotas.MaxConcurrency bounds how many workers a project runs at
// once, and a project at 1 is the common case. If a review counted against
// it, a pull request would sit unreviewed while the next delivery's worker
// held the only slot — and that worker's own pull request would then need a
// review, which would queue behind the worker after it. The queue never
// drains, and it fails worse the busier the project is.
//
// A review is cheap against that budget by construction: one session, one
// turn, no build, and it writes no commits. What MaxConcurrency exists to
// bound is the project's development work, and this is not that.
func (*ReviewRunner) OffProjectBudget() bool { return true }

// Run implements orchestration.JobRunner.
func (r *ReviewRunner) Run(ctx context.Context, job *orchestration.JobRun) error {
	if job == nil {
		return &InvalidInputError{Field: "job", Message: "must not be nil"}
	}
	run := &reviewRun{runner: r, job: job}
	return run.drive(ctx)
}

// =============================================================================
// One review
// =============================================================================

// reviewRun is the state one review accumulates. It lives for exactly one
// Run; the durable record is the forge's.
type reviewRun struct {
	runner *ReviewRunner
	job    *orchestration.JobRun

	subject  ReviewJobSubject
	binding  *ProjectBinding
	forge    ForgeBackend
	pr       *ForgePullRequest
	files    []ForgeChangedFile
	reviews  []ForgeReview
	headSHA  string
	reviewer ReviewerConfig

	sessionID    string
	containerDir string

	// spec is the workspace spec this review materialized for its spawn:
	// the same bytes the container reads at $QUICBAAS_WORKSPACE_SPEC, and
	// what the checkout is driven from. One description, both sides.
	spec orchestration.WorkspaceSpec

	// outcome is the verdict this run produced, and published is what it
	// published. They differ when a standing verdict was re-asserted rather
	// than produced.
	outcome   ReviewOutcome
	produced  bool
	published CommitStatus

	// skipReason says why no reviewer was spawned, empty when one was.
	skipReason string

	// round counts the heads this job has judged, from one. It is bounded
	// by MaxReviewRounds.
	round int

	// workspace is the reviewer's materialized workspace and the ssh agent
	// its checkout authenticated with. The agent is stopped as soon as the
	// checkout is made, before the reviewer is prompted.
	workspace *containerWorkspace

	// teardown releases what the run acquired, in reverse order.
	teardown []func()
}

// reviewHandler drives one state and answers the next.
type reviewHandler func(ctx context.Context) (orchestration.JobState, error)

// drive is the dispatch loop. Map-based so a state answers in constant time
// and the table reads as the sequence it is.
func (s *reviewRun) drive(ctx context.Context) error {
	handlers := map[orchestration.JobState]reviewHandler{
		orchestration.JobStateInitializing: s.initialize,
		orchestration.JobStateExecuting:    s.execute,
		orchestration.JobStateCompleting:   s.publish,
		orchestration.JobStateCleanup:      s.cleanup,
	}
	s.job.Decorate(s.decorate)

	// EVERY EXIT RELEASES THE SESSION, including the exits a run does not
	// plan: a forge that stopped answering, a cancellation, a panic. A
	// reviewer left running is a container and a model budget nobody is
	// waiting on.
	defer s.release(ctx)

	state := s.job.StartState
	if state == "" {
		state = orchestration.JobStateInitializing
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		step, ok := handlers[state]
		if !ok {
			return &UnknownRunnerStateError{State: string(state)}
		}
		next, err := step(ctx)
		if err != nil {
			return err
		}
		if next == orchestration.JobStateCompleted {
			return s.job.SetState(ctx, orchestration.JobStateCompleted)
		}
		if err := s.job.SetState(ctx, next); err != nil {
			return err
		}
		state = next
	}
}

// decorate adds the reviewer session to every snapshot, so a person and the
// reaper can both find it.
func (s *reviewRun) decorate(status *orchestration.JobStatus) {
	if status == nil || s.sessionID == "" {
		return
	}
	status.SessionIDs = []string{s.sessionID}
}

// release runs the run's teardown in reverse order and closes the reviewer.
func (s *reviewRun) release(ctx context.Context) {
	if s.sessionID != "" {
		s.job.Close(context.WithoutCancel(ctx), s.sessionID)
		s.sessionID = ""
	}
	for i := len(s.teardown) - 1; i >= 0; i-- {
		s.teardown[i]()
	}
	s.teardown = nil
}

// =============================================================================
// 1. initializing — is a review needed, and if so, spawn one
// =============================================================================

// initialize reads the binding and the pull request, decides whether this
// head needs a verdict at all, and spawns the reviewer when it does.
//
// THE FORGE IS RE-READ, ALWAYS. Whatever prompted this job — a webhook, a
// poll, an operator — is a hint about something that may have changed. The
// head the verdict binds to, whether the pull request is still open, whether
// the author has marked it a draft and whether a verdict already stands are
// all read here, at the moment the review starts.
func (s *reviewRun) initialize(ctx context.Context) (orchestration.JobState, error) {
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	pr, err := s.forge.GetPullRequest(ctx, s.repo(), s.subject.PullRequest)
	if err != nil {
		return "", err
	}
	if pr == nil {
		return "", &NotFoundError{
			Entity:   "pull request",
			TenantID: s.job.TenantID,
			ID:       s.repo().String() + "#" + strconv.Itoa(s.subject.PullRequest),
		}
	}
	s.pr = pr
	s.round++
	s.headSHA = strings.TrimSpace(pr.HeadSHA)
	if s.headSHA == "" {
		return "", &InvalidInputError{
			Field:   "pull_request.head_sha",
			Message: "the forge reports no head for " + s.subject.String() + "; a verdict would bind to nothing",
		}
	}

	skip, err := s.skipDecision(ctx)
	if err != nil {
		return "", err
	}
	if skip.skip {
		s.skipReason = skip.reason
		s.published = skip.status
		s.job.Emit(orchestration.JobStateCompleting, skip.reason)
		return orchestration.JobStateCompleting, nil
	}

	files, err := s.forge.ListPullRequestFiles(ctx, s.repo(), pr.Number)
	if err != nil {
		return "", err
	}
	s.files = files

	if err := s.spawnReviewer(ctx); err != nil {
		return "", err
	}
	if err := s.openCheckout(ctx); err != nil {
		return "", err
	}
	s.job.Emit(orchestration.JobStateExecuting,
		"reviewer session "+s.sessionID+" reading "+s.subject.String()+" at "+shortSHA(s.headSHA))
	return orchestration.JobStateExecuting, nil
}

// resolve loads the subject, the binding, the reviewer and the reviewer's own
// forge adapter, refusing every configuration that could not produce a usable
// verdict.
func (s *reviewRun) resolve(ctx context.Context) error {
	subject, err := ReviewJobSubjectFrom(s.job.Config)
	if err != nil {
		return err
	}
	s.subject = subject

	binding, err := s.bindings().GetBinding(ctx, s.job.TenantID, s.job.Config.ProjectID)
	if err != nil {
		return err
	}
	s.binding = binding
	s.reviewer = binding.Reviewer
	if !s.reviewer.Configured() {
		return &ReviewerNotConfiguredError{ProjectID: binding.ProjectID}
	}
	// INDEPENDENCE IS CHECKED BEFORE A MODEL IS SPENT. The gate discards a
	// same-family approval, so a review run under this configuration could
	// only ever produce a verdict the gate ignores.
	if binding.Review.RequireIndependentReviewer {
		family := s.reviewer.ModelFamily()
		if family == "" || family == ModelFamily(binding.Worker.Backend, binding.Worker.Model) {
			return &ReviewerNotIndependentError{ProjectID: binding.ProjectID, Family: family}
		}
	}

	// CLAUDE REVIEWS NOTHING, WHATEVER THE POLICY SAYS. Owner ruling
	// 2026-09-13: a review runs on codex or on the local qwen model.
	//
	// This is deliberately OUTSIDE the RequireIndependentReviewer branch above.
	// That check is a policy an operator may switch off, and switching it off
	// would then admit a Claude Code reviewer over Claude Code work — the
	// binding would look configured, the gate would count the approval, and the
	// author's own family would have judged the change. The ruling is not a
	// policy, so it is not behind one.
	if ModelFamily(s.reviewer.Backend, s.reviewer.Model) == ModelFamilyAnthropic {
		return &ReviewerFamilyRefusedError{
			ProjectID: binding.ProjectID,
			Backend:   s.reviewer.Backend,
			Family:    ModelFamilyAnthropic,
		}
	}

	forge, err := s.runner.forges.ForgeFor(ctx, s.job.TenantID, s.reviewer.ForgeConfigFor(binding.Forge))
	if err != nil {
		return err
	}
	if forge == nil {
		return NewForgeNotRegisteredError(binding.Forge.Type)
	}
	// CAPABILITIES, NOT THE PLATFORM'S NAME. A forge that cannot be told a
	// verdict cannot be reviewed by this module, whichever forge it is, and
	// finding that out here costs nothing.
	if !forge.Capabilities().SubmitReviews {
		return &ForgeUnsupportedError{Type: forge.Type(), Capability: "submitting a review"}
	}
	s.forge = forge
	return nil
}

func (s *reviewRun) bindings() BindingReader { return s.runner.bindings }

func (s *reviewRun) repo() RepoRef { return s.binding.Repo() }

// reviewSkip is the answer to "does this head need a verdict".
type reviewSkip struct {
	skip   bool
	reason string
	status CommitStatus
}

// skipDecision decides whether to spend a reviewer on this head.
//
// THE THREE SKIPS, and each of them is an incident the shell automation had:
//
//   - A PULL REQUEST THAT IS NOT OPEN. Nothing to decide, and no status
//     worth writing: a merged or closed pull request's checks decide nothing.
//   - A DRAFT. The author has said the work is unfinished, so there is no
//     approval to give — an approval would contradict the one person who
//     knows. A PENDING status is published rather than none, because a
//     required check that is simply absent reads on some platforms as
//     nothing to wait for.
//   - A HEAD THIS REVIEWER HAS ALREADY DECIDED. Re-reviewing it would burn a
//     model to reach the same answer, and — the part that actually broke — a
//     second run that reached a DIFFERENT answer would silently retract a
//     standing rejection. So a standing verdict is re-asserted rather than
//     re-derived, and the commit status is written again from it, because
//     the trigger may have been something that cleared the status.
//
// AND THE SKIP THAT IS NOT THERE: a COMMENT this reviewer left on the head
// does not count as a decision, because it is not one. Treating it as one is
// how a review posted while a pull request was a draft made that head
// permanently unapprovable — the reviewer saw its own comment, concluded it
// had already spoken, and never gave the verdict the ready-for-review pull
// request was waiting for.
func (s *reviewRun) skipDecision(ctx context.Context) (reviewSkip, error) {
	if state := pullRequestStateFor(s.pr); state != PullRequestOpen {
		return reviewSkip{skip: true, reason: s.subject.String() + " is " + state.String() + "; there is nothing to review"}, nil
	}
	if s.pr.Draft {
		return reviewSkip{
			skip:   true,
			reason: s.subject.String() + " is a draft; the author has said the work is unfinished, so it is not approved",
			status: s.statusFor(CheckPending, "draft: the author has not offered this change for review"),
		}, nil
	}

	reviews, err := s.forge.ListReviews(ctx, s.repo(), s.pr.Number)
	if err != nil {
		return reviewSkip{}, err
	}
	s.reviews = reviews

	standing, verdict, found := standingVerdict(reviews, s.reviewer.Login, s.headSHA)
	if !found {
		return reviewSkip{}, nil
	}
	return reviewSkip{
		skip: true,
		reason: s.reviewer.Login + " has already decided " + string(verdict) + " on " +
			shortSHA(s.headSHA) + "; the standing verdict is re-asserted, not replaced",
		status: s.statusForVerdict(verdict, standing.Body),
	}, nil
}

// standingVerdict returns the reviewer's own operative DECISION on one head,
// and whether it made one.
//
// A DECISION IS AN APPROVAL OR A REFUSAL AND NOTHING ELSE. A comment is
// excluded by construction, through the same forgeReviewVerdicts table the
// gate reads, so the runner's idea of "already decided" and the gate's idea
// of "carries a verdict" cannot drift apart.
//
// The hardest-blocking decision wins where there is more than one, which
// makes the answer independent of the order the forge returns them in: a
// reviewer that refused a head and later added a comment has still refused
// it, and no re-read can turn that into an unreviewed head.
func standingVerdict(reviews []ForgeReview, login, headSHA string) (ForgeReview, ReviewVerdict, bool) {
	if strings.TrimSpace(login) == "" || strings.TrimSpace(headSHA) == "" {
		return ForgeReview{}, "", false
	}
	var held ForgeReview
	var verdict ReviewVerdict
	found := false
	for _, review := range reviews {
		if !strings.EqualFold(review.Reviewer, login) || !strings.EqualFold(review.HeadSHA, headSHA) {
			continue
		}
		candidate, decided := forgeReviewVerdicts[strings.ToUpper(strings.TrimSpace(review.State))]
		if !decided {
			continue
		}
		if !found || verdictSeverity(candidate) > verdictSeverity(verdict) ||
			(verdictSeverity(candidate) == verdictSeverity(verdict) && review.Submitted.After(held.Submitted)) {
			held, verdict, found = review, candidate, true
		}
	}
	return held, verdict, found
}

// String names the pull request under review.
func (s ReviewJobSubject) String() string { return "pull request #" + strconv.Itoa(s.PullRequest) }

// =============================================================================
// The reviewer's session
// =============================================================================

// spawnReviewer starts the one session this job holds, in a container with a
// checkout of the head under review and no signing agent.
func (s *reviewRun) spawnReviewer(ctx context.Context) error {
	if s.job.Project == nil {
		return &InvalidInputError{
			Field:   "project",
			Message: "the job carries no project record; the reviewer's checkout is described from it",
		}
	}
	mount, err := s.mountPoint()
	if err != nil {
		return err
	}
	req, err := s.spawnRequest(ctx, mount)
	if err != nil {
		return err
	}
	resp, err := s.job.Spawn(ctx, req)
	if err != nil {
		return err
	}
	if resp == nil || resp.SessionID == "" {
		return &ReviewSessionError{PullRequest: s.subject.PullRequest, Message: "the spawn returned no session"}
	}
	s.sessionID = resp.SessionID
	if strings.TrimSpace(resp.ContainerID) == "" {
		return &ReviewSessionError{
			PullRequest: s.subject.PullRequest,
			Message: "session " + resp.SessionID + " is not containerized; a reviewer reads a candidate change, " +
				"which is untrusted input, and must not read it on the daemon",
		}
	}
	s.containerDir = filepath.Join(s.runner.access.checkoutRoot, s.job.Project.Name)
	s.job.SetWorkDir(filepath.Join(req.WorkDir, s.job.Project.Name))
	return s.job.SetContainer(s.sessionID, s.containerDir)
}

// mountPoint is the host directory the reviewer's checkout lives in.
//
// It is named for the pull request and the head, so two reviews of different
// heads never share a directory and a re-review of the same head reuses the
// clone the last one made.
func (s *reviewRun) mountPoint() (string, error) {
	root := strings.TrimSpace(s.job.Project.Workspace.Path)
	if root == "" {
		return "", &WorkspaceUnavailableError{ProjectID: s.job.Project.ID}
	}
	dir := filepath.Join(root, "review-"+strconv.Itoa(s.subject.PullRequest)+"-"+shortSHA(s.headSHA))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", &WorkspaceUnavailableError{ProjectID: s.job.Project.ID, Cause: err}
	}
	return dir, nil
}

// spawnRequest builds the reviewer's spawn: the reviewer charter as its
// system prompt, the binding's reviewer backend, model and image, and the
// workspace spec and the reviewer's forge token mounted.
//
// SigningScopeCheckout IS THE WHOLE OF THE REVIEWER'S KEY POSTURE. Its agent
// serves the project's ACCESS key — which is what authenticates a clone over
// ssh, and without which the reviewer could not read the change at all — and
// no commit signing key, so nothing in the container can produce a signature
// under the project's provenance identity. The agent is then stopped the
// moment the checkout is made (openCheckout), so for the whole time the
// reviewer is actually reading an untrusted change, the socket it holds
// answers nothing.
//
// THE REVIEWER'S TOKEN IS ITS OWN. ForgeConfigFor picks the reviewer's forge
// configuration over the binding's, which is how the verdict is posted as the
// reviewer rather than as the author.
func (s *reviewRun) spawnRequest(ctx context.Context, mount string) (*orchestration.SpawnSessionRequest, error) {
	req := &orchestration.SpawnSessionRequest{
		Backend:      s.reviewer.Backend,
		Model:        s.reviewer.Model,
		ProjectID:    s.job.Project.ID,
		SystemPrompt: ReviewerCharter(),
		WorkDir:      mount,
		Persistent:   true,
	}
	if image := strings.TrimSpace(s.reviewer.Image); image != "" {
		req.ContainerImage = image
		req.SandboxMode = orchestration.SandboxModeRequired
	}
	// Create is false and stays false: the branch exists — the author pushed
	// it — and the reviewer reads it rather than cutting anything.
	//
	// AND IT IS PINNED TO THE HEAD THIS RUN CAPTURED. The branch name alone
	// would fetch whatever the author has pushed by the time the container
	// clones, and the reviewer would then read one tree while the verdict
	// and the commit status this run submits name another — a judgement
	// about code nobody inspected. The commit makes the two the same object
	// or makes the checkout fail; see cloner.verifyPinnedHead.
	workspace, err := s.runner.access.Open(ctx, req, workspacePlan{
		TenantID: s.job.TenantID,
		Project:  s.job.Project,
		Forge:    s.reviewer.ForgeConfigFor(s.binding.Forge),
		Branch: orchestration.WorkspaceBranchPlan{
			Name:   s.pr.HeadRef,
			Base:   s.pr.BaseRef,
			Commit: s.headSHA,
		},
		Mount: mount,
		Scope: SigningScopeCheckout,
	})
	if err != nil {
		return nil, err
	}
	stopCtx := context.WithoutCancel(ctx)
	s.teardown = append(s.teardown, func() { workspace.Release(stopCtx) })
	s.workspace = workspace
	// The spec the checkout is driven from is the one that was WRITTEN, so
	// both sides act on one description and neither invents a sibling file.
	s.spec = workspace.Spec
	return req, nil
}

// checkout is this review's checkout, driven through the one seam the delivery
// runner and triage also drive.
func (s *reviewRun) checkout() containerCheckout {
	return containerCheckout{
		job:       s.job,
		sessionID: s.sessionID,
		spec:      s.spec,
		project:   s.job.Project,
		taskID:    s.subject.String(),
	}
}

// openCheckout clones the head under review, proves the clone, and stops the
// agent that authenticated it — and the stop happens whether the clone
// succeeded or not.
//
// THE AGENT GOES DOWN BEFORE THE REVIEWER IS PROMPTED. Nothing after the
// checkout reaches the forge over git: the verdict and the commit status are
// forge API calls the DAEMON makes. So the reviewer reads an untrusted change
// with a mounted socket that answers nothing at all.
//
// AND THE CHECKOUT IS PROVED BEFORE THE REVIEWER READS IT. The reviewer's
// tokens are worth the same as the worker's: one handed a checkout that is
// absent, empty or holding another repository reads what it finds and states a
// verdict about it — and a verdict about a tree nobody intended is worse than
// no verdict, because it is submitted to the forge under a commit status. There
// is no park here: a review is not an attempt at a task, so the fault fails the
// job and the engine's retry decides what happens next.
func (s *reviewRun) openCheckout(ctx context.Context) error {
	checkout := s.checkout()
	err := checkout.Make(ctx)
	s.workspace.StopSigning(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	return checkout.Verify(ctx)
}

// =============================================================================
// 2. executing — one turn, one verdict
// =============================================================================

// execute prompts the reviewer once and reads a verdict out of the answer.
//
// ONE TURN. A reviewer that needs a second prompt to state a verdict has not
// reached one, and asking again would be asking it to agree with itself.
func (s *reviewRun) execute(ctx context.Context) (orchestration.JobState, error) {
	if s.sessionID == "" {
		return "", &ReviewSessionError{PullRequest: s.subject.PullRequest, Message: "there is no reviewer session to prompt"}
	}
	resp, err := s.job.Prompt(ctx, &orchestration.PromptSessionRequest{
		SessionID: s.sessionID,
		Prompt:    s.reviewPrompt(),
	})
	if err != nil {
		return "", err
	}
	turn := ReadAgentTurn(resp)
	outcome, parseErr := ParseReviewVerdict(turn.Reply)
	s.outcome = outcome
	s.produced = parseErr == nil

	// THE ANSWER IS RECORDED BEFORE ANYTHING IS DECIDED ABOUT IT, and on the
	// failure path too: what the reviewer said is the only account of a
	// review that produced no verdict.
	s.job.Audit(auditReviewVerdict, s.describeTurn(turn, parseErr))
	if parseErr != nil {
		// A review that states no verdict is published as one that could not
		// be produced — a red status and the reviewer's own words — and then
		// FAILS the job, so the engine's retry drives another attempt rather
		// than this runner inventing one.
		if publishErr := s.publishUnreadable(ctx, parseErr); publishErr != nil {
			return "", publishErr
		}
		return "", parseErr
	}
	return orchestration.JobStateCompleting, nil
}

// describeTurn renders the reviewer's turn for the job's audit trail.
func (s *reviewRun) describeTurn(turn AgentTurn, parseErr error) string {
	var b strings.Builder
	b.WriteString(s.subject.String() + " at " + shortSHA(s.headSHA) + ": ")
	if parseErr != nil {
		b.WriteString("no verdict — " + parseErr.Error())
	} else {
		b.WriteString(string(s.outcome.Verdict))
	}
	b.WriteString(" (" + strconv.Itoa(turn.ReplyBytes) + " bytes)")
	if reply := strings.TrimSpace(turn.Reply); reply != "" {
		b.WriteString("\n\n" + reply)
	}
	return b.String()
}

// reviewPrompt is the whole text the reviewer's one turn is handed: which
// pull request, on which head, in which checkout, and the map of what it
// changes.
//
// THE CHANGED-FILE LIST IS THE MAP AND THE CHECKOUT IS THE TERRITORY. The
// list scopes the review — it is how the reviewer knows it has seen the whole
// change rather than the part it thought to look for — and the checkout is
// where it reads the code. Which is why the listing must be complete: a
// reviewer handed a prefix reviews the prefix and approves the change.
func (s *reviewRun) reviewPrompt() string {
	var b strings.Builder
	b.WriteString("Review " + s.repo().String() + "#" + strconv.Itoa(s.pr.Number) + ".\n\n")
	b.WriteString("Title: " + strings.TrimSpace(s.pr.Title) + "\n")
	b.WriteString("Head: " + s.headSHA + " (branch " + s.pr.HeadRef + ")\n")
	b.WriteString("Base: " + s.pr.BaseRef + "\n")
	if s.pr.URL != "" {
		b.WriteString("URL: " + s.pr.URL + "\n")
	}
	b.WriteString("Checkout: " + s.containerDir + ", already on the head under review.\n")
	b.WriteString("Its diff against the base is `git diff " + gitRemote + "/" + s.pr.BaseRef + "...HEAD`.\n")

	b.WriteString("\n## What the author wrote\n\n")
	if body := strings.TrimSpace(s.pr.Body); body != "" {
		b.WriteString(body + "\n")
	} else {
		b.WriteString("(the pull request has no description)\n")
	}

	b.WriteString("\n## Files changed (" + strconv.Itoa(len(s.files)) + ", complete)\n\n")
	if len(s.files) == 0 {
		b.WriteString("(the forge reports no changed files, which is itself worth a finding)\n")
	}
	for _, file := range s.files {
		b.WriteString("- " + file.Path)
		if file.PreviousPath != "" {
			b.WriteString(" (was " + file.PreviousPath + ")")
		}
		if file.Status != "" {
			b.WriteString(" [" + file.Status + "]")
		}
		b.WriteString(" +" + strconv.Itoa(file.Additions) + "/-" + strconv.Itoa(file.Deletions) + "\n")
	}
	b.WriteString("\nThat list is the whole change. Review all of it, then declare your verdict.\n")
	return b.String()
}

// =============================================================================
// 3. completing — publish the verdict, then say what the gate makes of it
// =============================================================================

// publish writes the verdict to the forge, publishes the commit status that
// makes it enforceable, and records what the gate now says about the pull
// request.
//
// THE REVIEW GOES FIRST AND THE STATUS SECOND, which is the safe order for
// the failure between them. A verdict recorded with no status leaves a check
// pending — the change waits, and the next run re-asserts the standing
// verdict's status and clears it. A status written with no verdict behind it
// would be the module's own word for a review that does not exist, and a
// green one would merge a change nobody reviewed.
func (s *reviewRun) publish(ctx context.Context) (orchestration.JobState, error) {
	if err := s.refreshPullRequest(ctx); err != nil {
		return "", err
	}
	if s.produced {
		if err := s.submit(ctx); err != nil {
			return "", err
		}
	}
	if s.published.Context != "" {
		if err := s.setStatus(ctx, s.published); err != nil {
			return "", err
		}
	}
	if err := s.recordGate(ctx); err != nil {
		return "", err
	}
	return s.afterPublish(ctx)
}

// afterPublish decides whether this job is finished with its pull request.
//
// THE HEAD IT JUDGED MAY NO LONGER BE THE HEAD. refreshPullRequest read the
// pull request as it stands, so the move is already known here; and because
// this job holds the pull request's lease, no other job can pick the new
// head up while it runs. Leaving would therefore leave the new head with no
// verdict until something else happened to the pull request. So it goes
// round again — a fresh reviewer on a fresh checkout of the new head —
// until the head stops moving or MaxReviewRounds is spent.
//
// RE-ENTERING INITIALIZING RE-FIRES NOTHING, and that is a property of the
// envelope rather than of this runner: JobRun's state-keyed lifecycle hooks
// (Prepare on initializing, Complete on completing) are installed only for
// WORKFLOW jobs — see agentServiceImpl.launchJob, which resolves a hook set
// from the workflow definition and nowhere else. A review job carries none,
// so a second pass through a state has no side effect outside this runner. A
// change that gave non-workflow runners hook sets would have to revisit this.
func (s *reviewRun) afterPublish(ctx context.Context) (orchestration.JobState, error) {
	current := strings.TrimSpace(s.pr.HeadSHA)
	if current == "" || strings.EqualFold(current, s.headSHA) {
		return orchestration.JobStateCleanup, nil
	}
	if s.round >= MaxReviewRounds {
		// THE CHECK IS LEFT PENDING, NOT ABSENT. The change is unreviewed
		// at this head and a required status that is simply missing reads
		// on some platforms as nothing to wait for — the same reason the
		// draft skip publishes one.
		s.skipReason = s.subject.String() + " moved " + strconv.Itoa(s.round) +
			" times while it was being reviewed; " + shortSHA(current) + " is left for a later review"
		status := s.statusFor(CheckPending, "the change kept moving during review; "+
			shortSHA(current)+" has not been reviewed")
		if err := s.setStatusOn(ctx, current, status); err != nil {
			return "", err
		}
		s.job.Emit(orchestration.JobStateCleanup, s.skipReason)
		return orchestration.JobStateCleanup, nil
	}
	s.job.Emit(orchestration.JobStateInitializing, s.subject.String()+" moved from "+
		shortSHA(s.headSHA)+" to "+shortSHA(current)+" during the review; the new head is reviewed next")
	s.resetForNextRound(ctx)
	return orchestration.JobStateInitializing, nil
}

// resetForNextRound releases the reviewer that read the previous head and
// clears everything that described it, so the next round reads the forge
// from scratch exactly as the first one did.
//
// THE SESSION GOES FIRST AND IS NOT REUSED. The next round is a different
// tree in a different checkout, and a reviewer carrying the previous head's
// conversation would be answering about both.
func (s *reviewRun) resetForNextRound(ctx context.Context) {
	s.release(ctx)
	s.containerDir = ""
	s.spec = orchestration.WorkspaceSpec{}
	s.pr = nil
	s.files = nil
	s.reviews = nil
	s.headSHA = ""
	s.outcome = ReviewOutcome{}
	s.produced = false
	s.published = CommitStatus{}
	s.skipReason = ""
}

// refreshPullRequest re-reads the pull request before anything is written.
//
// A REVIEW TAKES TIME, AND THE PULL REQUEST DOES NOT HOLD STILL WHILE IT
// DOES. The author can push, close it, or mark it a draft between the read
// that started the review and the write that ends it, and every guard the
// initializing step applied was applied to a record that old. Re-reading
// costs one call and is what makes the draft refusal below a guard rather
// than a re-statement of something already checked.
//
// THE REVIEWED HEAD IS NOT UPDATED. s.headSHA stays the tree the reviewer
// actually read, because that is what the verdict is about; a verdict moved
// onto a commit nobody read would be failure 9 committed by the module
// itself. The gate discards it as stale, which is the correct outcome.
//
// A pull request that has gone is not a failure: the review simply has
// nothing left to say about it.
func (s *reviewRun) refreshPullRequest(ctx context.Context) error {
	pr, err := s.forge.GetPullRequest(ctx, s.repo(), s.subject.PullRequest)
	if err != nil {
		return err
	}
	if pr == nil {
		return &NotFoundError{
			Entity:   "pull request",
			TenantID: s.job.TenantID,
			ID:       s.repo().String() + "#" + strconv.Itoa(s.subject.PullRequest),
		}
	}
	s.pr = pr
	return nil
}

// submit posts the produced verdict and publishes the status it implies.
func (s *reviewRun) submit(ctx context.Context) error {
	submission := s.outcome.Submission(s.headSHA)
	// A DRAFT MAY NOT BE APPROVED, and this is the assertion that holds even
	// when the skip above did not run — a pull request marked draft between
	// the read and the verdict, or a caller that drove this state directly.
	// Refused rather than downgraded: the reviewer formed an opinion about a
	// change its author says is unfinished, and quietly re-labelling that
	// opinion as a comment would publish a judgement nobody made.
	if s.pr.Draft && submission.Verdict == VerdictApprove {
		return &ReviewVerdictUnreadableError{
			Reason: s.subject.String() + " is a draft and an approval would contradict its author; " +
				"the verdict was not posted",
		}
	}
	if err := submission.Validate(); err != nil {
		return err
	}
	stored, err := s.forge.SubmitReview(ctx, s.repo(), s.pr.Number, submission)
	if err != nil {
		return err
	}
	s.published = s.statusForVerdict(submission.Verdict, submission.Body)
	s.job.Emit(orchestration.JobStateCompleting,
		"verdict "+string(submission.Verdict)+" posted on "+s.subject.String()+" at "+shortSHA(stored.HeadSHA))
	return nil
}

// publishUnreadable records a review that could not be produced: a red status
// and, when the forge will take one, the reviewer's own words as a comment so
// the author can see what happened.
func (s *reviewRun) publishUnreadable(ctx context.Context, cause error) error {
	if findings := strings.TrimSpace(s.outcome.Findings); findings != "" {
		note := ReviewSubmission{
			Verdict: VerdictComment,
			HeadSHA: s.headSHA,
			Body: "This review did not reach a verdict: " + cause.Error() + "\n\n" +
				"What the reviewer wrote is below.\n\n---\n\n" + findings,
		}
		if _, err := s.forge.SubmitReview(ctx, s.repo(), s.pr.Number, note); err != nil {
			return err
		}
	}
	return s.setStatus(ctx, s.statusFor(CheckFailure, "the reviewer produced no verdict"))
}

// reviewStatuses maps a verdict onto the state its commit status carries.
//
// A REFUSAL IS RED ON THE RUN THAT FILES IT. That is the whole of failure 7,
// and it is one table entry: the shell automation computed the check's colour
// from the verdicts it could see at the start of the run, so the very run
// that filed REQUEST_CHANGES reported green and auto-merge landed rejected
// code. Here the status is derived from the verdict this run produced, in the
// same step that produced it.
//
// A COMMENT IS NEITHER GREEN NOR PENDING. It decides nothing, so publishing
// green would let a required check pass on a review that explicitly declined
// to judge — but publishing PENDING was worse, because pending is the shape of
// a review still running, and this one has finished.
//
// PENDING DEADLOCKED THE PULL REQUEST PERMANENTLY. The review floor skips any
// head already carrying a status under the reviewer's context, deliberately
// and by presence rather than state, so that a review in flight is never given
// a duplicate reviewer (see checkRunPresent). A finished review that left
// `pending` behind therefore blocked the gate AND the backstop that exists to
// recover the gate: nothing could ever re-review that head. Measured on
// go-luks2 #202, whose round-2 commit answered the round-1 finding, went green
// in CI, and then waited on a re-review that could not be dispatched.
//
// CheckFailure is what VerdictError already publishes, and the two mean the
// same thing to every reader: the reviewer finished and produced nothing that
// satisfies the gate. It fails closed, and it is terminal, so the head's state
// is honest about being done.
var reviewStatuses = map[ReviewVerdict]CheckStatus{
	VerdictApprove:        CheckSuccess,
	VerdictRequestChanges: CheckFailure,
	VerdictComment:        CheckFailure,
	VerdictError:          CheckFailure,
}

// reviewStatusDescriptions is the one-line summary each verdict publishes.
var reviewStatusDescriptions = map[ReviewVerdict]string{
	VerdictApprove:        "approved",
	VerdictRequestChanges: "changes requested",
	VerdictComment:        "reviewed without a verdict",
	VerdictError:          "the reviewer produced no verdict",
}

// statusForVerdict renders the commit status a verdict publishes.
func (s *reviewRun) statusForVerdict(verdict ReviewVerdict, body string) CommitStatus {
	state, known := reviewStatuses[verdict]
	if !known {
		// An unknown verdict is not an approval. Failing closed here means a
		// verdict added to the vocabulary without a status mapping blocks
		// until someone maps it, rather than merging on the default.
		state = CheckFailure
	}
	description := reviewStatusDescriptions[verdict]
	if description == "" {
		description = string(verdict)
	}
	if headline := firstLine(body); headline != "" {
		description += ": " + headline
	}
	return s.statusFor(state, description)
}

// maxStatusDescription bounds the status description, in bytes. Forges cap it
// and truncate silently; bounding here keeps what the module meant to say,
// and keeps the ellipsis that says there was more.
const maxStatusDescription = 120

// statusEllipsis marks a description the bound cut short.
const statusEllipsis = "…"

// statusFor renders one commit status under the reviewer's context.
func (s *reviewRun) statusFor(state CheckStatus, description string) CommitStatus {
	if len(description) > maxStatusDescription {
		// ToValidUTF8 drops the partial rune the byte-wise cut may have
		// left: a status ending in a broken rune is one some forges reject
		// outright and others render as a replacement character.
		cut := strings.ToValidUTF8(description[:maxStatusDescription-len(statusEllipsis)], "")
		description = cut + statusEllipsis
	}
	status := CommitStatus{
		Context:     s.reviewer.Context(),
		Status:      state,
		Description: description,
	}
	if s.pr != nil {
		status.TargetURL = s.pr.URL
	}
	return status
}

// firstLine is the first non-empty line of a body, for a one-line summary.
func firstLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if trimmed := strings.TrimSpace(strings.Trim(line, "#* ")); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// setStatus publishes one commit status, when the platform has them.
//
// A PLATFORM WITHOUT COMMIT STATUSES IS NOT A FAILED REVIEW. The verdict is
// still posted and the module's own gate still reads it; what is lost is the
// platform's ability to enforce it. That is recorded and the job carries on,
// because failing the review would mean a forge lacking one optional feature
// could not be reviewed at all.
func (s *reviewRun) setStatus(ctx context.Context, status CommitStatus) error {
	return s.setStatusOn(ctx, s.headSHA, status)
}

// setStatusOn publishes one commit status against a named commit. Every
// status but one is about the head this round reviewed; the exception is the
// round budget's, which is about the head it did not get to.
func (s *reviewRun) setStatusOn(ctx context.Context, headSHA string, status CommitStatus) error {
	if !s.forge.Capabilities().CommitStatuses {
		s.job.Audit(auditReviewVerdict, string(s.forge.Type())+
			" publishes no commit statuses, so this verdict is recorded but not enforceable by branch protection")
		return nil
	}
	return s.forge.SetCommitStatus(ctx, s.repo(), headSHA, status)
}

// recordGate says what the pull request's state now IS, through the module's
// one gate.
//
// IT DECIDES NOTHING HERE. The review runner produces a verdict; whether the
// change lands is the delivery runner's step, and re-deciding it in a second
// place is exactly the duplication that let the shell automation merge over
// its own rejection. What this does is read the same function the merge
// reads, against the forge as it stands after the verdict was posted, and
// record the answer — so a person looking at the review job can see whether
// the pull request is now clear, and what is still holding it if not.
func (s *reviewRun) recordGate(ctx context.Context) error {
	reviews, err := s.forge.ListReviews(ctx, s.repo(), s.pr.Number)
	if err != nil {
		return err
	}
	var checks []ForgeCheckRun
	if s.binding.Review.RequireGreenChecks {
		if checks, err = s.forge.ListCheckRuns(ctx, s.repo(), s.headSHA); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	records := make([]ReviewRecord, 0, len(reviews))
	for _, review := range reviews {
		if record, acted := reviewRecordFor(review, s.binding.Review, s.job.Iteration()); acted {
			records = append(records, record)
		}
	}
	decision := EvaluateReviewGate(GateInput{
		Policy:            s.binding.Review,
		PullRequestState:  pullRequestStateFor(s.pr),
		HeadSHA:           s.headSHA,
		Reviews:           records,
		Checks:            checkRecordsFor(checks, s.headSHA, now),
		AuthorModelFamily: ModelFamily(s.binding.Worker.Backend, s.binding.Worker.Model),
		Draft:             s.pr.Draft,
	})
	s.job.Audit(auditReviewGate, s.subject.String()+" at "+shortSHA(s.headSHA)+": "+
		decision.Reason.String()+" ("+decision.Detail+"), "+strconv.Itoa(decision.Approvals)+" independent approval(s)")
	return nil
}

// =============================================================================
// 4. cleanup — the reviewer goes
// =============================================================================

// cleanup releases the reviewer session and finishes the job.
func (s *reviewRun) cleanup(ctx context.Context) (orchestration.JobState, error) {
	s.release(ctx)
	if s.skipReason != "" {
		s.job.Emit(orchestration.JobStateCompleted, s.skipReason)
	}
	return orchestration.JobStateCompleted, nil
}
