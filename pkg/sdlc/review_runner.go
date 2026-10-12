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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
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

// MaxReviewReprompts bounds how many times one reviewer is asked again for a
// verdict its reply did not state: ONCE.
//
// A REVIEW IS EXPENSIVE AND A PAUSE IS NOT A REFUSAL. Measured on the burn-in
// fleet (2026-09-22 03:39, PR #983, review job c64bd251): a reviewer that had
// read the whole diff ended its turn on a progress note — "I'm finishing the
// checks around forge mergeability and the transition back to review" — and
// declared nothing. That was refused, the job failed, and the review sweep
// spawned a fresh container to re-read the same diff from nothing. The
// reviewer was still there, still held the change in its context, and had
// never been asked to finish.
//
// THE SECOND ONE IS REFUSED UNCHANGED. A session that answers two prompts
// running without ever declaring is not going to declare, and a reviewer
// asked indefinitely is the unbounded spend this bound exists to refuse.
const MaxReviewReprompts = 1

// reviewRepromptPrompt is what a reviewer that stated no verdict is asked.
//
// IT NAMES THE MARKER ParseReviewVerdict ACTUALLY READS, from that same
// constant, so the ask and the parse cannot drift apart. It carries nothing
// else: the change, the charter, the issue and the file list are all still in
// the session's context, and re-sending them would spend a whole review
// prompt restating what the reviewer had just finished reading.
const reviewRepromptPrompt = "Your reply stated no verdict. Finish your checks and end with the " +
	ReviewVerdictMarker + " declaration and findings."

// auditReviewVerdict is the audit event a produced verdict is recorded under.
const auditReviewVerdict = "sdlc:review_verdict"

// auditReviewReprompted records a reply that stated no verdict, and the one
// re-prompt it earned.
//
// It is the ONLY trace of one: the re-prompt is not a review round, not a new
// job and not a second session, so without this event nothing in the record
// would say that a turn happened and was deliberately not counted.
const auditReviewReprompted = "sdlc:review_reprompted"

// auditReviewGate is the audit event the gate's answer is recorded under.
const auditReviewGate = "sdlc:review_gate"

// auditReviewSeatFailover is the audit event a review's move from one seat to
// the next is recorded under.
const auditReviewSeatFailover = "sdlc:review_seat_failover"

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

	// Trains is the merge train ledger. A train's pull request is reviewed
	// only while its train is waiting on that verdict, and the ledger is the
	// record that says so.
	Trains TrainStore

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

	// Guard is the operator's repository allowlist; see RunnerConfig.Guard.
	Guard RepoHostGuard

	// SocketsRoot is the DAEMON's root for per-session agent sockets
	// (orchestration.sockets_root, resolved by the host). It is required and
	// has no default here; see RunnerConfig.SocketsRoot.
	SocketsRoot string

	// Agents is the tenant agent catalog a seat's Agent is read from, for the
	// independence rule and the refusal of a reviewer from the author's
	// family. Required.
	Agents AgentCatalog

	// RepromptTimeout is the deadline on the one reminder turn a reviewer
	// that stated no verdict is given (MaxReviewReprompts). Zero takes
	// ReviewTurnTimeout, which is what production uses; a test sets it short
	// so a reviewer that never answers is cancelled without the suite
	// waiting an hour.
	RepromptTimeout time.Duration
}

// ReviewRunner is the module's review-side orchestration.JobRunner.
type ReviewRunner struct {
	bindings BindingReader
	forges   ForgeResolver
	trains   TrainStore
	agents   AgentCatalog

	// access is the one seam every runner's container reaches the forge
	// through — the same one the delivery runner and triage hold — so the
	// three cannot authenticate differently (go-quicbaas#929).
	access forgeAccess

	// repromptTimeout bounds the reminder turn. See
	// ReviewRunnerConfig.RepromptTimeout.
	repromptTimeout time.Duration
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
	case cfg.Trains == nil:
		return nil, &InvalidInputError{Field: "trains", Message: "whether a merge train is waiting on this review is on its ledger"}
	case cfg.Agents == nil:
		return nil, &InvalidInputError{Field: "agents", Message: "a review seat's model family is read from its Agent"}
	}
	access, err := newForgeAccess(cfg.Tokens, cfg.Signing, cfg.Guard, cfg.CheckoutRoot, cfg.SocketsRoot)
	if err != nil {
		return nil, err
	}
	repromptTimeout := cfg.RepromptTimeout
	if repromptTimeout <= 0 {
		repromptTimeout = ReviewTurnTimeout
	}
	return &ReviewRunner{
		bindings: cfg.Bindings, forges: cfg.Forges, trains: cfg.Trains, agents: cfg.Agents, access: access,
		repromptTimeout: repromptTimeout,
	}, nil
}

// Name implements orchestration.JobRunner.
func (*ReviewRunner) Name() string { return ReviewJobRunnerName }

// PromptSet implements orchestration.PromptSetProvider: the reviewer's
// charter and its train brief come from the sdlc set.
func (*ReviewRunner) PromptSet() prompts.SetKind { return SetSDLC }

// States implements orchestration.JobRunner.
func (*ReviewRunner) States() []orchestration.JobStateSpec { return ReviewRunnerStates() }

// ReviewTurnTimeout is how long a reviewer turn may take: the executing
// state's IdleThreshold in ReviewRunnerStates, and the deadline the reminder
// turn (MaxReviewReprompts) is prompted under.
//
// THE REMINDER NEEDS A DEADLINE OF ITS OWN. The stall detector ends a job
// whose session is GONE; a session that is still addressable but never
// answers is left running, and a prompt forwards its context without adding
// one. So the reminder is bounded here, by the same bound the state already
// declares, rather than by a detector that would not act on it.
const ReviewTurnTimeout = time.Hour

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
		{State: orchestration.JobStateExecuting, Label: labelReviewExecuting, SessionBacked: true, IdleThreshold: ReviewTurnTimeout},
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
	reviewer ReviewerProfile

	// agents is what the binding's references resolved to when the run
	// started: the families behind the worker slot and each seat.
	agents bindingAgents

	// issue is the issue this change claims to close, and discussion is its
	// comments. Both are read on the control plane and RENDERED INTO THE
	// PROMPT rather than left for the reviewer to fetch.
	//
	// THE REVIEWER IS ASKED TO VERIFY ACCEPTANCE CRITERIA AND WAS NEVER GIVEN
	// THEM. Its charter says to check the change against the issue, the
	// container holds no forge credential, and every attempt to fetch one came
	// back 404 — so a reviewer that had read the whole diff and found nothing
	// wrong still refused to approve, saying it could not confirm the change
	// closes the issue. That is a missing input reported as an unresolved
	// question, and it blocked merges on changes with no defect in them.
	//
	// ABSENT IS A VALID STATE. A change opened on a branch this module did not
	// name has no issue to render, and the prompt says so rather than
	// pretending the criteria were checked.
	issue      *ForgeIssue
	discussion []ForgeComment

	// train is the merge train this pull request is, when it is one whose
	// train is waiting on this verdict. Its changes, not an issue, are what
	// the prompt describes.
	train *MergeTrain

	// issueUnavailable says why the issue is absent, empty when it is not.
	// It is RENDERED INTO THE PROMPT: a reviewer that knows the criteria could
	// not be fetched can say its conformance verdict is unverified and still
	// judge the code, where one left to guess reports the gap as an open
	// question and declines to judge at all.
	issueUnavailable string

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

	// reprompts counts how many times THIS ROUND's reviewer has been asked
	// again for the verdict its reply did not state, bounded by
	// MaxReviewReprompts.
	//
	// IT IS PER ROUND BECAUSE THE REVIEWER IS. resetForNextRound releases the
	// session that read the previous head and the next round spawns its own,
	// so carrying the count across would charge a fresh reviewer for the
	// silence of one it never was.
	reprompts int

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
// reaper can both find it. It is the job's only session and so its lead
// (go-quicbaas#863).
func (s *reviewRun) decorate(status *orchestration.JobStatus) {
	if status == nil || s.sessionID == "" {
		return
	}
	status.SessionIDs = []string{s.sessionID}
	status.LeadSessionID = s.sessionID
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

	// ONE PASS PER SEAT. A seat whose Agent cannot take the work — its
	// accounts spent or rate-limited, its backend unserved — hands the review
	// to the next seat of the same standing, and that seat's own standing
	// verdict is asked about afresh, because it posts under its own login.
	prepared := false
	for {
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
		if !prepared {
			if err := s.prepareReading(ctx, pr); err != nil {
				return "", err
			}
			prepared = true
		}
		spawnErr := s.spawnReviewer(ctx)
		if spawnErr == nil {
			break
		}
		switched, err := s.failOverSeat(ctx, spawnErr)
		if err != nil {
			return "", err
		}
		if !switched {
			return "", spawnErr
		}
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
	if !binding.Reviewer.Configured() {
		return &ReviewerNotConfiguredError{ProjectID: binding.ProjectID}
	}
	// THE PROFILE THE DISPATCHER CHOSE, RESOLVED AGAINST THE BINDING AS IT IS
	// NOW. A profile removed since the job was started is refused rather than
	// replaced by the default: the operator asked for that reviewer, and a
	// verdict from another would answer a question nobody asked.
	reviewer, err := binding.ReviewerProfile(subject.Profile)
	if err != nil {
		return err
	}
	agents, err := resolveBindingAgents(ctx, s.runner.agents, s.job.TenantID, binding)
	if err != nil {
		return err
	}
	s.agents = agents
	return s.takeSeat(ctx, reviewer)
}

// takeSeat makes seat the one this run reviews as: it is checked, and the
// forge adapter is rebuilt with the seat's own token.
func (s *reviewRun) takeSeat(ctx context.Context, seat ReviewerProfile) error {
	if err := s.checkSeat(seat); err != nil {
		return err
	}
	s.reviewer = seat
	forge, err := s.runner.forges.ForgeFor(ctx, s.job.TenantID, s.reviewer.ForgeConfigFor(s.binding.Forge))
	if err != nil {
		return err
	}
	if forge == nil {
		return NewForgeNotRegisteredError(s.binding.Forge.Type)
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

// checkSeat refuses a seat whose review could only produce a verdict nobody
// may count.
//
// INDEPENDENCE IS CHECKED BEFORE A MODEL IS SPENT. The gate discards a
// same-family approval, so a review run under this configuration could only
// ever produce a verdict the gate ignores. An ADVISORY seat is exempt: the
// gate reads past its verdict whatever its family, and hearing a second
// opinion from the author's own family is a legitimate reason to have chosen
// it.
//
// CLAUDE REVIEWS NOTHING, WHATEVER THE POLICY SAYS. Owner ruling 2026-09-13: a
// review runs on codex or on the local qwen model. This is deliberately
// OUTSIDE the independence branch: that is a policy an operator may switch
// off, and the ruling is not a policy, so it is not behind one.
func (s *reviewRun) checkSeat(seat ReviewerProfile) error {
	family := s.agents.seatFamilies[seat.Name]
	if s.binding.Review.RequireIndependentReviewer && seat.Counts &&
		(family == "" || slices.Contains(s.agents.authorFamilies, family)) {
		return &ReviewerNotIndependentError{ProjectID: s.binding.ProjectID, Family: family}
	}
	if family == ModelFamilyAnthropic {
		return &ReviewerFamilyRefusedError{ProjectID: s.binding.ProjectID, Agent: seat.Agent, Family: ModelFamilyAnthropic}
	}
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
	// A MERGE TRAIN'S PULL REQUEST IS REVIEWED WHEN ITS TRAIN ASKS, AND ONLY
	// THEN. The train verifies prefixes of itself in CI while it bisects a
	// red, and only the one it decided to land is ever waiting on a verdict;
	// reviewing any other would be a bill for a head that will never land.
	// The ledger, not the payload that started this job, says which it is.
	if IsTrainBranch(s.pr.HeadRef) && sameRepo(s.pr.HeadRepo, s.repo()) {
		skip, err := s.trainSkip(ctx)
		if err != nil || skip.skip {
			return skip, err
		}
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

// trainSkip decides whether a merge train's pull request is this run's to
// review, and records the train when it is.
//
// NO STATUS IS WRITTEN ON A SKIP. A prefix the train is not landing has no
// verdict to wait for, and a status there would be read by nothing but a
// person wondering why a pull request that is about to be closed was judged.
func (s *reviewRun) trainSkip(ctx context.Context) (reviewSkip, error) {
	train, err := s.awaitedTrain(ctx)
	if err != nil {
		return reviewSkip{}, err
	}
	if train == nil {
		return reviewSkip{
			skip: true,
			reason: s.subject.String() + " is merge train branch " + s.pr.HeadRef + " at " + shortSHA(s.headSHA) +
				", which its train is not waiting to have reviewed",
		}, nil
	}
	// THE TRAIN'S REVIEWER, NOT ANY REVIEWER. The train lands on the verdict
	// of the profile it asked (ProjectBinding.TrainReviewer); an operator's
	// override or an advisory profile reviewing it would be a verdict the
	// train does not read.
	if wanted, ok := s.binding.TrainReviewer(); !ok || wanted.Name != s.reviewer.Name {
		return reviewSkip{
			skip: true,
			reason: s.subject.String() + " is merge train #" + strconv.Itoa(train.Number) + ", which is reviewed by " +
				"the project's counting reviewer profile, not " + s.reviewer.Name,
		}, nil
	}
	s.train = train
	return reviewSkip{}, nil
}

// awaitedTrain returns the merge train waiting on a verdict for this pull
// request at this head, nil when none is.
func (s *reviewRun) awaitedTrain(ctx context.Context) (*MergeTrain, error) {
	ledger, err := s.runner.trains.GetTrainLedger(ctx, s.job.TenantID, s.binding.ProjectID)
	if err != nil {
		return nil, err
	}
	train := ledger.Active
	if train == nil || train.Phase != TrainReviewing {
		return nil, nil
	}
	probe := train.ProbeOf(train.Land)
	if probe == nil || probe.PullRequest != s.pr.Number || !strings.EqualFold(probe.HeadSHA, s.headSHA) {
		return nil, nil
	}
	return train, nil
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

// prepareReading loads what the reviewer reads besides the checkout: the
// changed files, and the issue the change claims to close.
func (s *reviewRun) prepareReading(ctx context.Context, pr *ForgePullRequest) error {
	files, err := s.forge.ListPullRequestFiles(ctx, s.repo(), pr.Number)
	if err != nil {
		return err
	}
	s.files = files
	if s.train == nil {
		s.loadIssue(ctx)
	}
	return nil
}

// failOverSeat moves the run to the next review seat when cause is a refusal
// the next seat's Agent might not share, and reports whether it did. Seat
// order is the failover chain and never crosses the gate (NextReviewerSeat);
// a seat the independence rule or the family ruling would refuse is passed
// over, and every switch is on the job's audit trail.
func (s *reviewRun) failOverSeat(ctx context.Context, cause error) (bool, error) {
	reason, ok := orchestration.AgentFailoverReason(cause)
	if !ok {
		return false, nil
	}
	from := s.reviewer
	for current := from; ; {
		next, found := s.binding.NextReviewerSeat(current)
		if !found {
			s.job.Audit(auditReviewSeatFailover, s.subject.String()+": seat "+from.Name+" ("+from.Agent+
				") cannot review ("+reason+") and no seat of the same standing follows it")
			return false, nil
		}
		if err := s.checkSeat(next); err != nil {
			s.job.Audit(auditReviewSeatFailover, s.subject.String()+": passed over seat "+next.Name+": "+err.Error())
			current = next
			continue
		}
		if err := s.takeSeat(ctx, next); err != nil {
			return false, err
		}
		s.job.Audit(auditReviewSeatFailover, s.subject.String()+": seat "+from.Name+" ("+from.Agent+") -> "+
			next.Name+" ("+next.Agent+"): "+reason)
		return true, nil
	}
}

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
	req, err := s.spawnRequest(ctx)
	if err != nil {
		return err
	}
	resp, err := s.job.Spawn(ctx, req)
	if err != nil {
		return workspaceSpawnFault(s.job.Project.ID, err)
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
	if resp.Workspace == nil {
		return &ReviewSessionError{PullRequest: s.subject.PullRequest, Message: "the spawn reported no workspace for the checkout"}
	}
	s.containerDir = filepath.Join(s.runner.access.checkoutRoot, s.job.Project.Name)
	// The host side of the checkout, on whichever host ran the session.
	s.job.SetWorkDir(filepath.Join(resp.Workspace.Dir, s.job.Project.Name))
	return s.job.SetContainer(s.sessionID, s.containerDir)
}

// workspaceKey names the reviewer's workspace for the pull request, the head
// AND the reviewer, so two reviews of different heads never share a directory
// and a re-review of the same head by the same reviewer on the same host
// reuses the clone the last one made.
//
// THE REVIEWER IS PART OF THE NAME because two reviewers of one head may be
// placed on the same host — the default profile and an operator's override,
// or a reviewer whose run outlives the override that replaced it. Sharing a
// directory would race their clone, fetch, checkout and git configuration
// against each other.
func (s *reviewRun) workspaceKey() string {
	return reviewWorkspaceKey(s.subject.PullRequest, s.headSHA, s.reviewer.ProfileName(), s.reviewer.Login)
}

// reviewWorkspaceKey renders the workspace key for one reviewer of one head.
// The profile name is already a safe path segment (validReviewerProfileName);
// a digest of the normalized login keeps two profiles of one name — a profile
// deleted and re-added under another identity — apart.
func reviewWorkspaceKey(pullRequest int, headSHA, profile, login string) string {
	sum := sha256.Sum256([]byte(normalizeLogin(login)))
	return "review-" + strconv.Itoa(pullRequest) + "-" + shortSHA(headSHA) + "-" +
		profile + "-" + hex.EncodeToString(sum[:4])
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
func (s *reviewRun) spawnRequest(ctx context.Context) (*orchestration.SpawnSessionRequest, error) {
	// THE SEAT'S AGENT SUPPLIES THE RUNTIME, THE MODEL, THE IMAGE AND THE
	// REASONING EFFORT; the seat supplies only the identity it posts as.
	req := &orchestration.SpawnSessionRequest{
		AgentID:      s.reviewer.Agent,
		Role:         orchestration.AgentRoleReview,
		ProjectID:    s.job.Project.ID,
		SystemPrompt: ReviewerCharter(jobPrompts(s.job)),
		PromptAppend: s.reviewer.PromptAppend,
		Persistent:   true,
	}
	if err := placeReviewer(req, s.reviewer.ReviewerConfig, s.agents.seatAgents[s.reviewer.Name]); err != nil {
		return nil, err
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
		Key:   s.workspaceKey(),
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

// placeReviewer applies where the reviewer may run and what it runs against.
//
// PLACEMENT IS THE FLEET'S, AND THIS ONLY NARROWS IT. The dispatcher still
// picks an online node that serves the backend and that the project admits; a
// reviewer pinned to a host makes that pin STRICT, because the pin exists to
// reach a model server, and a node chosen instead might not
// (orchestration.strictAffinityRefusal).
//
// AN OPENCODE REVIEWER ON A LOCAL MODEL IS HANDED ITS ENDPOINT. Whether the
// seat is one is the seat's Agent's to say — its backend and model — and the
// endpoint is the seat's. The provider is declared in an inline configuration
// pointing at the endpoint with the Agent's model pinned on it, so the model
// that answers is the one the verdict is attributed to, not whatever the node
// configures. The spawn names the Agent, which supplies that same model, and a
// local model presents no provider credential, so the spawn skips the account
// pool (orchestration.SpawnNeedsNoAccount).
func placeReviewer(req *orchestration.SpawnSessionRequest, seat ReviewerConfig, agent orchestration.Agent) error {
	if affinity := strings.TrimSpace(seat.HostAffinity); affinity != "" {
		req.HostAffinity = affinity
		req.HostAffinityStrict = true
	}
	if !ReviewsOnLocalModel(agent) {
		return nil
	}
	config, err := opencodeProviderConfig(seat.Endpoint, agent.Model)
	if err != nil {
		return err
	}
	if req.Env == nil {
		req.Env = make(map[string]string, 1)
	}
	req.Env[opencodeConfigEnv] = config
	return nil
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
// 2. executing — one verdict, and at most one reminder to state it
// =============================================================================

// execute prompts the reviewer and reads a verdict out of the answer.
//
// ONE VERDICT, NOT ONE TURN. A reviewer is asked to judge once; it is never
// asked to judge again, because asking a second time would be asking it to
// agree with itself. But a reply that DECLARED NOTHING has not judged at all
// — it paused mid-sentence — and the one thing this step may do about that is
// ask it to finish, exactly once. See MaxReviewReprompts.
//
// THE EXTRA TURN CONTINUES THE SAME CONVERSATION AND HAS A REAL DEADLINE. It
// is sent through this same session, and the backend carries the reviewer's
// conversation into it — for codex's exec transport by resuming the thread
// the first turn started — so the reminder reaches a reviewer that still
// holds the charter, the change and its own analysis. It is prompted under
// ReviewTurnTimeout (see prompt): a reviewer that goes silent after being
// reminded is cancelled, not waited on. The round is untouched —
// MaxReviewRounds counts HEADS judged, and no new head is read here — and no
// second session is spawned.
func (s *reviewRun) execute(ctx context.Context) (orchestration.JobState, error) {
	if s.sessionID == "" {
		return "", &ReviewSessionError{PullRequest: s.subject.PullRequest, Message: "there is no reviewer session to prompt"}
	}
	prompt := s.reviewPrompt()
	reminder := false
	for {
		resp, err := s.prompt(ctx, prompt, reminder)
		if err != nil {
			return "", err
		}
		turn := ReadAgentTurn(resp)
		outcome, parseErr := ParseReviewVerdict(turn.Reply)
		s.outcome = outcome
		s.produced = parseErr == nil

		// THE ANSWER IS RECORDED BEFORE ANYTHING IS DECIDED ABOUT IT, and on
		// the failure path too: what the reviewer said is the only account of
		// a review that produced no verdict. Every turn is recorded, the
		// re-prompted one included, so the trail carries both replies.
		s.job.Audit(auditReviewVerdict, s.describeTurn(turn, parseErr))
		if parseErr == nil {
			return orchestration.JobStateCompleting, nil
		}
		if s.reprompt(turn) {
			prompt = reviewRepromptPrompt
			reminder = true
			continue
		}
		// A review that states no verdict is published as one that could not
		// be produced — a red status and the reviewer's own words — and then
		// FAILS the job, so the engine's retry drives another attempt rather
		// than this runner inventing one.
		if publishErr := s.publishUnreadable(ctx, parseErr); publishErr != nil {
			return "", publishErr
		}
		return "", parseErr
	}
}

// prompt sends one turn to the reviewer's session.
//
// THE REMINDER RUNS UNDER A DEADLINE; the first turn runs under the job's
// context as it always has. A reviewer that is reminded and then never
// answers is cancelled at the runner's repromptTimeout (ReviewTurnTimeout in
// production) and the job fails with a ReviewRepromptTimeoutError, rather
// than holding the job open on an addressable session the stall detector
// would not end.
func (s *reviewRun) prompt(ctx context.Context, prompt string, reminder bool) (*orchestration.PromptSessionResponse, error) {
	req := &orchestration.PromptSessionRequest{SessionID: s.sessionID, Prompt: prompt}
	if !reminder {
		return s.job.Prompt(ctx, req)
	}
	timeout := s.runner.repromptTimeout
	if timeout <= 0 {
		timeout = ReviewTurnTimeout
	}
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := s.job.Prompt(turnCtx, req)
	if err != nil && ctx.Err() == nil && errors.Is(turnCtx.Err(), context.DeadlineExceeded) {
		return nil, &ReviewRepromptTimeoutError{PullRequest: s.subject.PullRequest, Timeout: timeout, Cause: err}
	}
	return resp, err
}

// reprompt decides whether the reviewer is asked once more for the verdict its
// reply did not state, and records the ask when it is.
//
// ONLY A MISSING DECLARATION EARNS ONE. A reviewer that declared an unknown
// code, or declared a code above nothing, reached the end of its review and
// answered badly; those are the parser's refusals and they stand. A reply
// with no declaration at all is the one case where the reviewer demonstrably
// has not finished, which is what makes "finish your checks" the right thing
// to say to it rather than a nudge towards any answer that parses.
//
// THE SESSION IS STILL THE ONE THAT READ THE CHANGE. Nothing between the turn
// above and this decision releases it — release runs on the way out of the
// whole run, and resetForNextRound only on a head that moved — so the
// reminder reaches a reviewer that still holds the diff, which is the entire
// saving over the fresh container the sweep would otherwise spawn.
func (s *reviewRun) reprompt(turn AgentTurn) bool {
	if s.outcome.Declared || s.reprompts >= MaxReviewReprompts || s.sessionID == "" {
		return false
	}
	s.reprompts++
	s.job.Audit(auditReviewReprompted, s.subject.String()+" at "+shortSHA(s.headSHA)+
		": round="+strconv.Itoa(s.round)+
		" reply_bytes="+strconv.Itoa(turn.ReplyBytes)+
		"; the reply carries no "+ReviewVerdictMarker+" declaration, so the same reviewer is asked "+
		"once more for one: neither a review round nor a new review job")
	s.job.Emit(orchestration.JobStateExecuting, "the reviewer's reply stated no verdict; "+
		"it is asked once more to finish its checks and declare one")
	return true
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
// loadIssue reads the issue this change claims to close, and its discussion,
// so the prompt can carry the acceptance criteria the reviewer is told to
// check.
//
// IT NEVER FAILS THE REVIEW. A change on a branch this module did not name has
// no issue to find, and a forge that will not serve one is a degraded review
// rather than no review — the code is still there to be read, and a reviewer
// that reports a real defect is worth more than a refused job. What the
// reviewer must not do is silently assume the criteria were met, so the prompt
// says plainly when they are missing.
func (s *reviewRun) loadIssue(ctx context.Context) {
	if s.pr == nil {
		return
	}
	number, ok := issueNumberFromBranch("issue/", s.pr.HeadRef)
	if !ok {
		s.issueUnavailable = "the branch " + strconv.Quote(s.pr.HeadRef) +
			" was not named by this module, so the issue it closes is not recoverable from it"
		return
	}
	label := "#" + strconv.Itoa(number)
	issue, err := s.forge.GetIssue(ctx, s.repo(), number)
	if err != nil {
		s.issueUnavailable = "issue " + label + " could not be read from the forge: " + err.Error()
		return
	}
	if issue == nil {
		s.issueUnavailable = "the forge reports no issue " + label
		return
	}
	s.issue = issue
	// THE DISCUSSION CARRIES THE RULINGS. An owner's answer to a question the
	// worker raised lives in a comment, not the body, and a reviewer judging
	// the change against the body alone contradicts decisions already made.
	comments, err := s.forge.ListIssueComments(ctx, s.repo(), number)
	if err != nil {
		s.issueUnavailable = "issue " + label +
			" was read but its discussion was not, so any ruling recorded in a comment is missing: " + err.Error()
		return
	}
	s.discussion = comments
}

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

	if s.train != nil {
		b.WriteString(s.trainSection())
	} else {
		b.WriteString(s.issueSection())
	}

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

// trainSection renders what a merge train's reviewer is asked to judge: which
// changes the train carries, that each was already approved on its own, and
// that what is left to review is how they combine.
func (s *reviewRun) trainSection() string {
	cars := s.train.Cars[:s.train.Land]
	var members strings.Builder
	for _, car := range cars {
		members.WriteString("- #" + strconv.Itoa(car.PullRequest) + " " + strings.TrimSpace(car.Title) +
			" (branch " + car.Branch + "), approved at `" + car.HeadSHA + "`\n")
	}
	issues := "none"
	if closes := trainCloses(cars); len(closes) > 0 {
		refs := make([]string, 0, len(closes))
		for _, issue := range closes {
			refs = append(refs, "#"+strconv.Itoa(issue))
		}
		issues = strings.Join(refs, ", ")
	}
	return "\n" + jobPrompts(s.job).MustRender("train_review", prompts.Vars{
		"train":   strconv.Itoa(s.train.Number),
		"target":  s.train.Target,
		"base":    s.train.BaseSHA,
		"members": strings.TrimRight(members.String(), "\n"),
		"issues":  issues,
	})
}

// maxIssueBodyBytes and maxCommentBytes bound what one issue may contribute to
// the prompt. An issue's discussion is unbounded and a long one would crowd out
// the diff, which is the thing actually being reviewed.
const (
	maxIssueBodyBytes = 8000
	maxCommentBytes   = 2000
	maxIssueComments  = 20
)

// issueSection renders the issue the change claims to close, so the reviewer
// can check the change against it rather than asking for it.
func (s *reviewRun) issueSection() string {
	var b strings.Builder
	b.WriteString("\n## The issue this change must satisfy\n\n")
	if s.issue == nil {
		b.WriteString("NOT AVAILABLE: " + s.issueUnavailableReason() + "\n\n")
		// SAID PLAINLY, BECAUSE THE ALTERNATIVE IS A REFUSED REVIEW. A reviewer
		// left to infer that the criteria are missing reports it as an open
		// question and declines to judge, which satisfies no gate and blocks a
		// change that may have nothing wrong with it.
		b.WriteString("Review the CODE on its own merits and declare a verdict on it. " +
			"Do not withhold a verdict for want of this record, and do not ask for it: " +
			"it was not fetchable on this run. Say in your review that conformance to " +
			"the issue is unverified, and judge what you can see.\n")
		return b.String()
	}
	b.WriteString("#" + strconv.Itoa(s.issue.Number) + " " + strings.TrimSpace(s.issue.Title) + "\n\n")
	if body := strings.TrimSpace(s.issue.Body); body != "" {
		b.WriteString(truncateForPrompt(body, maxIssueBodyBytes) + "\n")
	} else {
		b.WriteString("(the issue has no description)\n")
	}
	b.WriteString(s.discussionSection())
	return b.String()
}

// discussionSection renders the issue's comments, where the rulings live.
func (s *reviewRun) discussionSection() string {
	if len(s.discussion) == 0 {
		if s.issueUnavailable != "" {
			return "\n### Discussion\n\nNOT AVAILABLE: " + s.issueUnavailable + "\n"
		}
		return "\n### Discussion\n\n(none)\n"
	}
	var b strings.Builder
	b.WriteString("\n### Discussion\n\n")
	// THE NEWEST COMMENTS ARE THE ONES THAT DECIDE. A ruling supersedes the
	// question it answers, so when the bound cuts, it keeps the tail.
	comments := s.discussion
	if len(comments) > maxIssueComments {
		b.WriteString("(" + strconv.Itoa(len(comments)-maxIssueComments) +
			" earlier comment(s) omitted; the most recent follow)\n\n")
		comments = comments[len(comments)-maxIssueComments:]
	}
	for _, comment := range comments {
		author := strings.TrimSpace(comment.Author)
		if author == "" {
			author = "(unknown)"
		}
		b.WriteString("- **" + author + "**: " +
			truncateForPrompt(strings.TrimSpace(comment.Body), maxCommentBytes) + "\n")
	}
	return b.String()
}

// issueUnavailableReason is why the issue is absent, in words, always naming
// something rather than leaving the reviewer to guess.
func (s *reviewRun) issueUnavailableReason() string {
	if reason := strings.TrimSpace(s.issueUnavailable); reason != "" {
		return reason
	}
	return "the issue was not read on this run"
}

// truncateForPrompt bounds one field and says it cut, so a reviewer never
// mistakes a truncated record for a complete one.
func truncateForPrompt(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := strings.ToValidUTF8(text[:limit], "")
	return cut + "\n\n[…truncated at " + strconv.Itoa(limit) + " bytes]"
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
	s.issue = nil
	s.discussion = nil
	s.train = nil
	s.issueUnavailable = ""
	s.headSHA = ""
	s.outcome = ReviewOutcome{}
	s.produced = false
	s.published = CommitStatus{}
	s.skipReason = ""
	s.reprompts = 0
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
// A COMMENT IS PENDING, NOT GREEN, AND PENDING IS THE POINT. It decides
// nothing, so publishing green would let a required check pass on a review that
// declined to judge — and publishing a TERMINAL status is worse, because the
// head then carries a decision nobody made and no path to getting one.
//
// PENDING IS THE ONLY STATUS THAT MEANS "NO VERDICT YET", which is exactly what
// a comment leaves behind: it binds onto no review record at all, so the gate
// correctly still wants a review for this head and the floor must still be able
// to dispatch one. The floor treats a pending reviewer status older than the
// stall bound as abandoned rather than in flight (checkRunPresent), so a head
// left this way recovers on its own.
//
// THAT BOUND IS WHAT MAKES PENDING SAFE, and without it this mapping WAS a
// permanent deadlock: a finished review left pending, the floor read it as a
// review in flight and skipped the head forever, and the change waited on a
// re-review no code path could dispatch. Measured on go-luks2 #202 and #200.
// The defect was the floor's reading of pending, not the mapping — mapping
// these to failure instead only moved the deadlock, because a terminal status
// cannot be retried either.
//
// VerdictError is the same case: the reviewer produced no verdict, so no
// decision exists to record, and the head is owed another attempt.
var reviewStatuses = map[ReviewVerdict]CheckStatus{
	VerdictApprove:        CheckSuccess,
	VerdictRequestChanges: CheckFailure,
	VerdictComment:        CheckPending,
	VerdictError:          CheckPending,
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

// advisoryNote marks an advisory review in the gate audit line, so a reader
// sees why a rejection from this reviewer did not hold the change back.
var advisoryNote = map[bool]string{true: " (advisory: not counted toward merge)", false: ""}

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
	policy := s.binding.reviewPolicy(s.agents)
	records := make([]ReviewRecord, 0, len(reviews))
	for _, review := range reviews {
		if record, acted := reviewRecordFor(review, policy, s.job.Iteration()); acted {
			records = append(records, record)
		}
	}
	decision := EvaluateReviewGate(GateInput{
		Policy:              s.binding.Review,
		PullRequestState:    pullRequestStateFor(s.pr),
		HeadSHA:             s.headSHA,
		Reviews:             records,
		Checks:              checkRecordsFor(checks, s.headSHA, now),
		AuthorModelFamilies: s.agents.authorFamilies,
		Draft:               s.pr.Draft,
		Advisory:            s.binding.AdvisoryReviewers(),
	})
	s.job.Audit(auditReviewGate, s.subject.String()+" at "+shortSHA(s.headSHA)+" reviewed as "+
		s.reviewer.Name+advisoryNote[s.reviewer.Advisory()]+": "+
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
