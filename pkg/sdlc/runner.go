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
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The sdlc JobRunner — one issue, one branch, one worker
// =============================================================================
//
// AN ISSUE IS NOT A DAG. The engine's own runner is a lead agent decomposing a
// description into work items executed by workers; that is a Workflow, and it
// stays exactly that. An issue-driven delivery has a different shape: there is
// nothing to decompose, because the issue IS the unit of work, and there is
// nothing for a lead to paraphrase, because the issue text is the contract.
// So this runner has no decomposing, scheduling or assigning state, spawns
// exactly ONE persistent worker session, and prompts it with the issue
// contract VERBATIM.
//
// What it does not own, it does not repeat. The branch is the Strategy's, the
// forge writes are the Hooks', the review is the forge_review gate's, and the
// lease, heartbeat, accounting, events and recovery are the engine envelope's.
// This file is the ORDER those run in.
//
// The steps run through job.RunStage rather than the engine's state→moment
// table: the runner gets the operator's stage timeout, JobStatus.Stage and the
// stage event, and it can run Reviewable again on each review round, which the
// table cannot express.

// JobRunnerName is the name a Task and a Job select this runner by.
const JobRunnerName = "sdlc"

// WorkerSessionCount is how many sessions an sdlc job holds at once: one.
//
// It is a named constant because it is the property the whole runner exists
// to guarantee — the shape the lead-driven loop got wrong — and a test asserts
// against it rather than against a literal. A job may hold that one session
// in more than one container over its life: a session replaced on a full
// context or a rotated account, and one closed for the wait in reviewing and
// spawned again for a review round (releaseWorker), each close the old
// container before the new one exists.
const WorkerSessionCount = 1

// stateLabels name the runner's states for a person reading a job.
const (
	labelInitializing = "Initializing"
	labelExecuting    = "Executing"
	labelValidating   = "Validating"
	labelReviewing    = "Reviewing"
	labelRedelegating = "Re-delegating"
	labelCompleting   = "Completing"
	labelCleanup      = "Cleanup"
	labelEscalating   = "Escalating"
)

// RunnerConfig wires the runner to the module's own pieces.
type RunnerConfig struct {
	// Hooks is the module's lifecycle hook set. The runner calls its
	// moments directly rather than through the engine's state table.
	Hooks *Hooks

	// Tokens resolves the forge token the worker's container clones and
	// pushes with.
	Tokens ForgeTokenReader

	// Signing starts the ephemeral, KMS-backed agent the worker's container
	// signs its commit against. The key itself never enters the container.
	Signing SigningAgent

	// CheckoutRoot is the directory under which each attempt's checkout is
	// mounted, as the CONTAINER sees it. Empty takes the engine's default.
	CheckoutRoot string

	// Guard is the operator's repository allowlist (sdlc.repo_hosts). Every
	// workspace is checked against it when it is opened, not only at bind;
	// it is required. See RepoHostGuard.CheckProject.
	Guard RepoHostGuard

	// SocketsRoot is the DAEMON's root for per-session agent sockets
	// (orchestration.sockets_root, resolved by the host). NewRunner prepares
	// and measures it with EnsureSigningSocketRoot, so a root no socket
	// could be bound under refuses the module at startup rather than the
	// first delivery, and the sessions a killed process left under it are
	// swept before the first attempt binds one.
	//
	// It is required and has no default here: the module knows nothing of
	// the data directory or the runtime directory the host derives it from,
	// and guessing one would bind a socket somewhere the mount policy never
	// admitted.
	SocketsRoot string

	// Now is the clock ONE WORKER TURN IS MEASURED AGAINST: the elapsed time
	// the sdlc:agent_turn event reports, and with it the NonAnswerTurnBound
	// decision that reads the same number. Nil takes time.Now.
	//
	// It is injectable because the only other way a test can put a turn on
	// either side of that bound is to make the suite wait, and a suite that
	// sleeps to age a duration is a suite nobody runs per push.
	Now func() time.Time
}

// Runner is the module's orchestration.JobRunner.
type Runner struct {
	hooks *Hooks

	// access is the one seam this runner's containers reach the forge
	// through: the token, the ssh agent, the checkout root and the sockets
	// root. The reviewer and triage hold the same seam, which is what stops
	// the three from authenticating differently (go-quicbaas#929).
	access forgeAccess

	// now is the clock a turn's elapsed time is read from, never nil.
	now func() time.Time
}

var (
	_ orchestration.JobRunner  = (*Runner)(nil)
	_ orchestration.JobResumer = (*Runner)(nil)
)

// NewRunner builds the runner; every collaborator is required.
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.Hooks == nil {
		return nil, &InvalidInputError{Field: "hooks", Message: "the runner drives the module's own lifecycle moments"}
	}
	access, err := newForgeAccess(cfg.Tokens, cfg.Signing, cfg.Guard, cfg.CheckoutRoot, cfg.SocketsRoot)
	if err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Runner{hooks: cfg.Hooks, access: access, now: now}, nil
}

// SocketSweep reports what preparing the sockets root removed: the signing
// sessions a previous process was killed holding.
//
// It is read once, by the host that started the module, to say so in the
// log. Nothing in the delivery path consults it — the sweep is finished
// before this runner exists.
func (r *Runner) SocketSweep() orchestration.SigningSocketSweep { return r.access.sweep }

// Name implements orchestration.JobRunner.
func (*Runner) Name() string { return JobRunnerName }

// States implements orchestration.JobRunner.
func (*Runner) States() []orchestration.JobStateSpec { return RunnerStates() }

// RunnerStates is the delivery sequence, in order. There is no decomposing,
// scheduling or assigning state — nothing decomposes an issue and nothing
// assigns it to a fleet.
//
// It is a package function as well as a method so the host can declare the
// runner's states to the engine before a runtime exists to drive them: the
// stall detector's thresholds and the UI's stepper read the registry, and a
// registration that waited for the module to start would be read too late.
//
// ONLY EXECUTING IS SESSION-BACKED. The declaration says which states the
// stall detector may CONCLUDE about: a job past its bound in one of them with
// no addressable session has lost the worker it is waiting on and is ended,
// and a job in any other state is only reported. The worker session is
// spawned in initializing and closed on entry to reviewing, or in cleanup or
// escalating for a run that never got there, so "the run happens to hold a
// session here" is not the test. What is:
//
//   - executing: the worker is prompted and the run is waiting on its answer.
//   - reviewing is NOT declared: it holds NO worker. The container commits,
//     signs and pushes (publishForReview → publishWork execs every git
//     command into it) and is then CLOSED for the whole forge poll
//     (releaseWorker, go-quicbaas#938). A round the gate asks for spawns a
//     fresh container on the same branch and workspace, through the same
//     initializing path a replaced session takes. A verdict that never
//     arrives is reported, and there is no worker whose death could strand
//     it. The stall detector reads this runner's own answer rather than the
//     workflow runner's, which does hold a session in its reviewing.
//   - validating is NOT declared, though its commands do run in the
//     container: the state's own work IS the exec, so a worker that has gone
//     fails the pipeline rather than leaving the run waiting on it. There is
//     no window in which the state idles past its bound with a dead worker.
//   - initializing, re-delegating, completing and cleanup are the daemon's
//     own work — the spawn, the round arithmetic, the merge and the
//     post-merge steps, the teardown — and each finishes correctly whether
//     or not a worker is still there. Completing and cleanup hold the
//     session; holding one is not doing work through it.
//
// The engine's own workflow runner declares reviewing session-backed too, and
// mergeJobStateSpec takes the disjunction — but a declaration that is only
// true because ANOTHER runner made it is not this runner's answer, and it
// would go silently false the day that runner changed (go-quicbaas#668).
//
// REVIEWING, CLEANUP AND ESCALATING ARE OFF THE PROJECT BUDGET; THE OTHERS
// SPEND A SLOT. The owner's ruling on go-quicbaas#978 (2026-09-22): the
// project's MaxConcurrency bounds the states that bear a worker — initializing,
// executing, validating, re-delegating and completing — and nothing else. A
// job in reviewing waits on a CI pipeline and a reviewer for up to an hour
// with no worker turn running, and holding a slot for that wait left the
// project's next ready task pending the whole time. A round the review or a
// red pipeline asks for goes back through the project's admission on its way
// to re-delegating, and WAITS there while the project is full. Cleanup and
// escalating are the run's teardown and its park, entered from a state that
// already held a slot and leading only to a terminal one, so they give the
// slot back rather than hold it through work no worker does.
//
// THE SAME THREE STATES HOLD NO WORKER ON ANY AXIS. The engine marks the lease
// dormant in them (orchestration.JobLease.Dormant), so the fleet and backend
// checks stop counting the job too (go-quicbaas#1036), and reviewing closes
// the container so the slot it frees is real.
func RunnerStates() []orchestration.JobStateSpec {
	return []orchestration.JobStateSpec{
		// Every state declares a bound, so none inherits the workflow
		// runner's ceiling. The five states below do short work and keep the
		// ten minutes they derived before #621; that is pinned by
		// TestSDLCModule_DerivedIdleThresholdsAreUnchanged.
		{State: orchestration.JobStateInitializing, Label: labelInitializing, IdleThreshold: 10 * time.Minute},
		// THE BOUNDS ARE THE WORK'S, NOT THE ENGINE'S DEFAULT. A delivery
		// turn on a large repository runs an hour; its verification runs
		// `make test` for twenty-five minutes three times over; and reviewing
		// waits on a CI pipeline of fifty minutes and then on a reviewer. The
		// engine's default idle bound is minutes, and on 2026-09-21 the stall
		// detector ended a resumed go-quicbaas job in reviewing at thirty
		// minutes — while its pull request's checks were still being judged —
		// and the task was re-dispatched to be reviewed again from scratch.
		{State: orchestration.JobStateExecuting, Label: labelExecuting, SessionBacked: true, IdleThreshold: 2 * time.Hour},
		{State: orchestration.JobStateValidating, Label: labelValidating, IdleThreshold: 2 * time.Hour},
		{State: orchestration.JobStateReviewing, Label: labelReviewing, IdleThreshold: 4 * time.Hour, OffProjectBudget: true},
		{State: orchestration.JobStateRedelegating, Label: labelRedelegating, IdleThreshold: 10 * time.Minute},
		{State: orchestration.JobStateCompleting, Label: labelCompleting, IdleThreshold: 10 * time.Minute},
		{State: orchestration.JobStateCleanup, Label: labelCleanup, IdleThreshold: 10 * time.Minute, OffProjectBudget: true},
		{State: orchestration.JobStateEscalating, Label: labelEscalating, IdleThreshold: 10 * time.Minute, OffProjectBudget: true},
	}
}

// Validate implements orchestration.JobRunner.
func (*Runner) Validate(_ context.Context, cfg *orchestration.JobConfig) error {
	return ValidateRunnerConfig(cfg)
}

// ValidateRunnerConfig refuses a job configuration this runner cannot drive:
// an sdlc job is an attempt at the task an issue was adopted as, and it is
// refused without one before any goroutine or lease exists.
func ValidateRunnerConfig(cfg *orchestration.JobConfig) error {
	if cfg == nil {
		return &InvalidInputError{Field: "config", Message: "must not be nil"}
	}
	if strings.TrimSpace(cfg.TaskID) == "" {
		return &InvalidInputError{
			Field:   "task_id",
			Message: "an sdlc job is an attempt at the task an issue was adopted as; there is no free-form sdlc job",
		}
	}
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return &InvalidInputError{
			Field:   "project_id",
			Message: "the repository, the identity and the binding all hang off the project",
		}
	}
	return nil
}

// ResumeState implements orchestration.JobResumer.
//
// THE QUESTION IS NOT "how far did it get" BUT "what is already true on the
// forge". A resumed run holds no worker session — the container died with the
// daemon — so a state restarts where the durable record leaves the work:
//
//   - initializing, executing, validating restart at initializing. Nothing
//     reached the forge, and the only path back to a worker is a fresh spawn.
//     Prepare's continues-branch picks up the branch an earlier attempt
//     pushed, and its pull request and reviews carry over onto the Delivery.
//   - re-delegating restarts at initializing for the same reason: the next
//     round needs a worker, and the round and its findings are restored from
//     the persisted iteration and job memory.
//   - reviewing resumes THERE. The head is already on the forge, and what is
//     left is the daemon's own half — open or refresh the pull request, ask
//     for the verdict. When the Delivery records no head the push never
//     happened, and the reviewing step sends the run back to initializing.
//   - completing resumes THERE: Complete is bound to the head the review
//     approved and is idempotent — a pull request already merged is recorded,
//     not merged twice — so finishing the merge is both safe and the only way
//     the task ever concludes.
//   - cleanup resumes THERE. The merge and the strategy's post-merge steps
//     are done by the time the state is persisted; all that remains is a
//     teardown that needs no session.
//   - escalating resumes THERE, and the escalation FINISHES: the park reason
//     reaches the issue exactly once, the worker is torn down and the state
//     returns failed. It does not map straight to failed, which the engine's
//     own workflow runner can do because it has no park reason to deliver:
//     resumeJob never launches a run for a terminal answer, so a job that
//     died between entering escalating and Blocked completing would end with
//     nothing on the issue a person was asked to look at.
func (*Runner) ResumeState(interrupted orchestration.JobState) orchestration.JobState {
	return RunnerResumeState(interrupted)
}

// runnerResumeStates is the recovery table, keyed by the state the job was
// interrupted in. Map-based rather than a switch so a state answers in
// constant time and the table reads as the contract it is; a state not in it
// — a record written by a runner that has since changed shape — restarts at
// initializing, which is safe for any state that published nothing.
var runnerResumeStates = map[orchestration.JobState]orchestration.JobState{
	orchestration.JobStateInitializing: orchestration.JobStateInitializing,
	orchestration.JobStateExecuting:    orchestration.JobStateInitializing,
	orchestration.JobStateValidating:   orchestration.JobStateInitializing,
	orchestration.JobStateReviewing:    orchestration.JobStateReviewing,
	orchestration.JobStateRedelegating: orchestration.JobStateInitializing,
	orchestration.JobStateCompleting:   orchestration.JobStateCompleting,
	orchestration.JobStateCleanup:      orchestration.JobStateCleanup,
	orchestration.JobStateEscalating:   orchestration.JobStateEscalating,
}

// RunnerResumeState is where an interrupted sdlc job restarts; see
// Runner.ResumeState.
func RunnerResumeState(interrupted orchestration.JobState) orchestration.JobState {
	if resumed, ok := runnerResumeStates[interrupted]; ok {
		return resumed
	}
	return orchestration.JobStateInitializing
}

// Run implements orchestration.JobRunner: the state sequence, driven to a
// terminal state.
func (r *Runner) Run(ctx context.Context, job *orchestration.JobRun) error {
	if job == nil {
		return &InvalidInputError{Field: "job", Message: "must not be nil"}
	}
	run := &runnerRun{runner: r, job: job}
	return run.drive(ctx)
}

// =============================================================================
// One run
// =============================================================================

// runnerRun is the state one attempt accumulates. It lives for exactly one
// Run: everything durable is on the Delivery and the Task.
type runnerRun struct {
	// roundRequested records that a review round asked for new work, so
	// resumeReview does not send the job back to a review of the head the
	// gate just rejected. See nextRound.
	roundRequested bool

	runner *Runner
	job    *orchestration.JobRun

	attempt   *attempt
	delivery  *Delivery
	sessionID string

	// workDir is the attempt's checkout as the DAEMON sees it — the host
	// side of the bind mount, recorded on the job for a reader there.
	// Nothing this runner executes uses it.
	workDir string

	// containerDir is the checkout as the CONTAINER sees it, which is the
	// only directory any command in this run — git or verification — names.
	containerDir string

	// spec is the workspace spec this attempt materialized for its spawn:
	// the same bytes the container reads at $QUICBAAS_WORKSPACE_SPEC, and
	// what the checkout step clones from.
	spec orchestration.WorkspaceSpec

	// workspace is the attempt's materialized workspace and the ssh agent
	// its checkout, its commit and its push authenticate with. The agent is
	// alive for as long as the worker's container is, because the worker
	// pushes at the end of every turn.
	workspace *containerWorkspace

	// feedback is what the next executing turn is told: the verification
	// output that failed, or the review's findings.
	feedback string

	// lastTurn is the worker turn this run last took, classified when it
	// changed nothing. It is the run's copy of what the job event recorded.
	lastTurn AgentTurn

	// silentTurns counts the turns this attempt answered with no text at
	// all, bounded by MaxSilentTurns.
	silentTurns int

	// nonAnswerTurns counts the CONSECUTIVE turns this attempt answered
	// without addressing the prompt, bounded by MaxNonAnswerTurns. A turn
	// that was anything else resets it (recordTurn).
	nonAnswerTurns int

	// turnNote is what the NEXT prompt of this run adds that the last one
	// did not: the account of a turn that answered with nothing, or of one
	// that answered something other than the prompt. Without it the retry
	// would be the identical prompt, which is the loop this whole path
	// exists to break.
	turnNote string

	// parked is how this run parks its task: the kind, and whose fault it
	// was. Zero until the run parks — escalation is the default kind, and no
	// attribution is the default class, which is the CHARGED direction.
	parked parkJudgement

	// teardown releases the signing agent and the materialized workspace
	// spec, in reverse order of acquisition.
	teardown []func()

	// tornDown records that teardownWorker has run, so the exit path does
	// not run it a second time behind a state that already did.
	tornDown bool

	// rolled and rolledRound record that this run has already replaced a
	// worker session whose context was full, and in which round. One
	// rollover per round: a fresh session that fills again on the same
	// prompt is the work's own doing and is charged, not rolled forever.
	rolled      bool
	rolledRound int

	// limitedAccounts are the pooled accounts the provider refused during
	// this run, for a limit or for a login it rejected. A session is rolled off an account at most once: if the
	// replacement lands on an account already in this set, the hold did not
	// take and rolling again would loop, so the failure is returned instead.
	limitedAccounts map[string]struct{}

	// credentialReplaced and credentialReplacedRound record that this run has
	// already replaced a session whose pooled account's credential was
	// rotated under it, and in which round. Counted apart from the context
	// rollover: the two causes are unrelated, and one spending the other's
	// replacement would leave the run charged for a fault it did not have.
	credentialReplaced      bool
	credentialReplacedRound int
}

// handler drives one state and answers the next.
type handler func(ctx context.Context) (orchestration.JobState, error)

// drive is the dispatch loop. Map-based rather than a switch so a state
// answers in constant time and the table reads as the sequence it is.
func (s *runnerRun) drive(ctx context.Context) error {
	handlers := map[orchestration.JobState]handler{
		orchestration.JobStateInitializing: s.initialize,
		orchestration.JobStateExecuting:    s.execute,
		orchestration.JobStateValidating:   s.validate,
		orchestration.JobStateReviewing:    s.review,
		orchestration.JobStateRedelegating: s.redelegate,
		orchestration.JobStateCompleting:   s.complete,
		orchestration.JobStateCleanup:      s.cleanup,
		orchestration.JobStateEscalating:   s.escalate,
	}
	s.job.Decorate(s.decorate)
	if s.job.Resumed != nil {
		s.job.SetIteration(s.job.Resumed.Iteration)
	}

	// EVERY EXIT TEARS THE WORKER DOWN, not only the two states that reach a
	// teardown of their own. cleanup and escalating are the exits a run
	// PLANS; a refusal after the spawn, a checkout that will not run, a
	// cancellation and a panic are exits it does not, and each of those used
	// to return from here with a live container and a session record the
	// daemon still held (go-quicbaas#750 — three leaked in one minute).
	// Deferred so it also covers the panic, and it is the same teardownWorker
	// a completion runs rather than a second, thinner copy of it.
	defer s.abandonWorker(ctx)

	state := s.job.StartState
	if state == "" {
		state = orchestration.JobStateInitializing
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// A BLOCKED TASK GETS NO FURTHER STATE, from whatever drove the run
		// here: a review verdict, a CI result, a resume after a restart. The
		// task is re-read on every pass because a block can land at any point
		// in a run that lasts hours (go-quicbaas#1054, attempt_admission.go).
		if err := s.runner.hooks.admitAttempt(ctx, s.job.TenantID, s.taskID(), s.job.JobID); err != nil {
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
		switch next {
		case orchestration.JobStateCompleted:
			return s.job.SetState(ctx, orchestration.JobStateCompleted)
		case orchestration.JobStateFailed:
			return &EscalatedError{TaskID: s.taskID(), Reason: s.job.EscalationReason()}
		}
		if err := s.job.SetState(ctx, next); err != nil {
			return err
		}
		state = next
	}
}

// decorate adds the runner's own recoverable state to every snapshot: the
// worker session, so a person and the reaper can both find it. The worker
// session is the job's only session and so its lead; naming it is what lets
// the job page badge and link it (go-quicbaas#863).
func (s *runnerRun) decorate(status *orchestration.JobStatus) {
	if status == nil {
		return
	}
	if s.sessionID != "" {
		status.SessionIDs = []string{s.sessionID}
		status.LeadSessionID = s.sessionID
	}
}

// taskID is the task the run is an attempt at.
func (s *runnerRun) taskID() string {
	if s.job.Config == nil {
		return ""
	}
	return s.job.Config.TaskID
}

// resolve loads the project, binding, task, issue and forge once per state
// that needs them; the records may have moved between states, so it re-reads
// rather than caching across the whole run.
func (s *runnerRun) resolve(ctx context.Context) error {
	a, err := s.runner.hooks.resolve(ctx, s.job.Context())
	if err != nil {
		return err
	}
	s.attempt = a
	return nil
}

// loadDelivery re-reads the attempt's Delivery.
func (s *runnerRun) loadDelivery(ctx context.Context) error {
	delivery, err := s.runner.hooks.deliveries.GetDelivery(ctx, s.job.TenantID, s.taskID())
	if err != nil {
		return err
	}
	s.delivery = delivery
	return nil
}

// release runs the run's teardown in reverse order. It is idempotent.
func (s *runnerRun) release() {
	for i := len(s.teardown) - 1; i >= 0; i-- {
		s.teardown[i]()
	}
	s.teardown = nil
}

// =============================================================================
// 1. initializing — plan the branch, hand the worker its workspace
// =============================================================================

// initialize plans the branch through the strategy, records the Delivery,
// labels the issue in progress, and spawns the ONE worker session the attempt
// has, sandboxed in the binding's worker image with the workspace spec, the
// forge token and the signing agent's socket mounted.
//
// THE DAEMON MAKES NO CHECKOUT. It creates the empty directory the container's
// clone is bind-mounted through and nothing else. The clone itself runs INSIDE
// the container, driven from here through the runtime's exec seam: the project
// and every context repository fresh, side by side, on the planned branch —
// fetched when a prior attempt pushed it. See checkout.go for why it is driven
// rather than left to an image entrypoint.
func (s *runnerRun) initialize(ctx context.Context) (orchestration.JobState, error) {
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	if err := s.job.RunStage(ctx, orchestration.JobStagePrepare, func(hookCtx context.Context) error {
		_, err := s.runner.hooks.Prepare(hookCtx, s.job.Context())
		return err
	}); err != nil {
		return "", err
	}
	if err := s.loadDelivery(ctx); err != nil {
		return "", err
	}
	// AN ATTEMPT THAT ALREADY HAS A PULL REQUEST RESUMES IT RATHER THAN
	// WORKING AGAIN. See resumeReview for what this fixes.
	if state, err := s.resumeReview(ctx); state != "" || err != nil {
		return state, err
	}
	// A BINDING THIS NODE WILL NOT VERIFY STARTS NO WORKER. See
	// refusedVerification.
	if err := s.attempt.binding.CheckVerification(s.job.Guard()); err != nil {
		return s.refusedVerification(err)
	}
	if state, err := s.prepareWorkspace(ctx); state != "" || err != nil {
		return state, err
	}
	s.job.Emit(orchestration.JobStateExecuting, "worker session "+s.sessionID+" on branch "+s.delivery.Branch)
	return orchestration.JobStateExecuting, nil
}

// adoptBranchPullRequest binds a delivery that holds no pull request to one
// already open on the branch this delivery plans to use, reporting whether it
// found one.
//
// THE CASE IT EXISTS FOR IS A RE-ADOPTED ISSUE. A delivery is keyed by task and
// a task is terminal, so an issue whose first task ended — completed, failed,
// abandoned — is adopted again as a NEW task with a FRESH delivery that knows
// nothing of the pull request the old one opened. The branch name is planned
// from the ISSUE, so it is the same name; the remote branch still carries the
// old commits; and the new attempt cuts its branch from the base instead.
//
// Measured on go-truststrap#51 (2026-09-15), which is what this prevents: the
// fresh task did the work, and every push was refused —
//
//	! [rejected] issue/51-… (non-fast-forward)
//	Updates were rejected because the tip of your current branch is behind
//
// — and each refusal was charged to the WORK, so the task spent its whole
// budget failing at something the work had nothing to do with. Re-labelling an
// orphaned issue was therefore not merely useless but harmful.
//
// BINDING IS A DELIVERY WRITE, not a field set on this run, because the merge,
// the gate and the outcome comment all read the delivery — a binding only this
// attempt could see would be lost the moment the attempt ended, which is the
// same shape of bug as the orphan it repairs.
func (s *runnerRun) adoptBranchPullRequest(ctx context.Context) (bool, error) {
	branch := strings.TrimSpace(s.delivery.Branch)
	if branch == "" {
		return false, nil
	}
	open, err := s.attempt.forge.ListOpenPullRequests(ctx, s.attempt.repo)
	if err != nil {
		return false, err
	}
	for i := range open {
		pr := open[i]
		if pr.Merged || !strings.EqualFold(strings.TrimSpace(pr.HeadRef), branch) {
			continue
		}
		if err := s.runner.hooks.updateDelivery(ctx, s.job.Context(), s.delivery, func(d *Delivery) {
			d.PullRequest = DeliveryPullRequest{
				Number: pr.Number, URL: pr.URL, State: PullRequestOpen,
				Title: pr.Title, Base: pr.BaseRef,
			}
			// THE HEAD COMES FROM THE FORGE. This attempt published nothing,
			// so the delivery's own HeadSHA is empty — and resumeReview
			// refuses to resume without one, for good reason.
			d.HeadSHA = pr.HeadSHA
		}); err != nil {
			return false, err
		}
		s.job.Emit(orchestration.JobStateInitializing,
			"adopted the pull request already open on "+branch+" (#"+strconv.Itoa(pr.Number)+
				"); this task did not open it, and cutting a rival branch could not be pushed")
		return true, nil
	}
	return false, nil
}

// resumeReview sends an attempt whose task ALREADY has an open pull request
// straight to the review state, skipping the worker. It answers "" when there
// is nothing to resume and the attempt should do the work.
//
// WHAT IT FIXES, MEASURED ON THE BURN-IN FLEET 2026-09-14. A delivery is keyed
// by TASK, so it outlives the attempt that created it — but every attempt used
// to run the worker regardless. When an attempt ended for a reason that is not
// the work's fault (an environment fault, a lost lease, a restart) AFTER its
// pull request was opened, the next attempt was handed a workspace with the
// previous attempt's own branch checked out. The worker read the tree, found
// the change already present, and reported `satisfied` — exactly what its
// contract says that means. The conclusion seam then completed the task as
// `already_satisfied` on the premise that "the work was already present, so
// there is nothing to merge", which is true of a stale issue and false here:
// the pull request that attempt opened was still open, and completing the task
// left it with nobody to merge it, ever.
//
// Three of the fleet's nine open pull requests were stranded that way
// (go-truststrap#44 -> PR #66, go-truststrap#51 -> PR #65, go-luks2#92 ->
// PR #197), two of them carrying review verdicts nobody would ever act on and
// one of them APPROVED and gate-cleared.
//
// THE FORGE IS ASKED, NOT THE RECORD. The delivery says what was true when it
// was written; a pull request merged or closed by a person since then must not
// send this attempt into a review of something that already landed. This is
// the same reason Hooks.Complete re-reads before merging.
//
// A CLOSED-BUT-UNMERGED PULL REQUEST ENDS THE ATTEMPT as superseded
// (go-quicbaas#1054). Somebody closed that delivery on purpose, so no worker
// round can change it and nobody has a question to answer. That check runs
// BEFORE every other exit here. A requested round and a recorded refusal both
// send the attempt to the worker, and a pull request closed after either would
// otherwise get another worker round.
func (s *runnerRun) resumeReview(ctx context.Context) (orchestration.JobState, error) {
	if s.delivery == nil || !s.delivery.RequiresPullRequest {
		return "", nil
	}
	var pr *ForgePullRequest
	fetched := false
	if s.delivery.PullRequest.Opened() {
		var err error
		pr, err = s.attempt.forge.GetPullRequest(ctx, s.attempt.repo, s.delivery.PullRequest.Number)
		if err != nil {
			return "", err
		}
		fetched = true
		// ForgePullRequest.State is the forge's own string ("open"/"closed"),
		// not the delivery's typed PullRequestState; forge_review.go compares
		// it the same way.
		if pr != nil && !pr.Merged && pr.State == "closed" {
			// Falling through would initialize a worker for a pull request
			// nobody will merge. That is the extra round task 3a58725f ran
			// against its closed PR #1052.
			return s.supersede(pr.Number), nil
		}
	}
	// A REQUESTED ROUND OUTRANKS THE RESUME. The gate rejected the head this
	// pull request carries, so reviewing it again would judge the identical
	// tree and reject it identically.
	if s.roundRequested {
		return "", nil
	}
	// A TASK THAT HOLDS NO PULL REQUEST MAY STILL BE ABOUT ONE. See
	// adoptBranchPullRequest: an issue re-adopted after its first task ended
	// gets a FRESH delivery, and cutting a rival branch over the one the old
	// pull request is still open on cannot even be pushed.
	if !s.delivery.PullRequest.Opened() {
		adopted, err := s.adoptBranchPullRequest(ctx)
		if err != nil || !adopted {
			return "", err
		}
	}
	// A HEAD IS REQUIRED, AND WITHOUT IT THIS MUST NOT FIRE. publishForReview
	// sends an attempt that reaches Reviewing with no session and no HeadSHA
	// BACK to Initializing so the work is done — and Initializing is where
	// this runs, so firing on an empty head would bounce the job between the
	// two states for as long as its budget lasts.
	if strings.TrimSpace(s.delivery.HeadSHA) == "" {
		return "", nil
	}
	// A REFUSAL THE DELIVERY RECORDS OUTRANKS THE RESUME TOO, and this check
	// must sit AFTER the adoption above rather than before it. roundRequested
	// is this JOB's memory of a rejected head and dies with it; the delivery
	// is the durable one. But a RE-ADOPTED issue starts with an empty delivery
	// and learns its pull request, its head and its reviews from the forge in
	// adoptBranchPullRequest — so asking before that runs reads a blank
	// delivery, finds no refusal, and resumes into Reviewing anyway. Measured
	// both ways on go-truststrap 2026-09-16: task 61e3aa53 readmitted (three
	// attempts in 91 seconds), then task c4f558a8 re-adopted from issue #44
	// and refused identically with the check in the wrong place.
	//
	// Resuming a refused head asks for the identical verdict on the identical
	// tree while no worker is running to address it, which surfaces as "there
	// is no worker session to prompt", one attempt at a time, until the budget
	// is gone. PRs #64, #65 and #66 have all sat in that state.
	if s.delivery.HeadRefused() {
		return "", nil
	}
	// An adopted pull request was not read above: it came from the open
	// listing, so it is read here for its current state.
	if !fetched {
		var err error
		pr, err = s.attempt.forge.GetPullRequest(ctx, s.attempt.repo, s.delivery.PullRequest.Number)
		if err != nil {
			return "", err
		}
	}
	if pr == nil || pr.Merged || pr.State == "closed" {
		return "", nil
	}
	s.job.Emit(orchestration.JobStateReviewing,
		"resuming the pull request this task already opened (#"+strconv.Itoa(pr.Number)+
			"); the worker is not re-run because the delivery exists")
	return orchestration.JobStateReviewing, nil
}

// prepareWorkspace gets the worker a checkout and PROVES it before the run
// goes on to prompt anything: the container, the clone, and then the
// pre-flight's probes.
//
// A WORKSPACE FAULT NEVER REACHES A MODEL. Every way of being unfit — a
// project with no workspace path, a mount point the daemon could not make, a
// checkout that is absent, empty, or holding another repository, a context
// repository the project declared and nobody materialized — arrives here as
// ErrWorkspaceUnfit and is PARKED for an operator, attributed to the factory,
// with zero tokens spent. The measured alternative is a worker reading an issue,
// a Makefile and a handbook to establish that a file it was told to adopt is
// not mounted, and an attempt charged for saying so (go-quicbaas#908).
//
// It answers a state ONLY when it parked; an empty state and no error means the
// workspace is fit and the caller carries on.
func (s *runnerRun) prepareWorkspace(ctx context.Context) (orchestration.JobState, error) {
	err := s.openWorkspace(ctx)
	if err == nil {
		return "", nil
	}
	if !errors.Is(err, ErrWorkspaceUnfit) {
		return "", err
	}
	reason, composeErr := WorkspaceUnfitParkReason(err)
	if composeErr != nil {
		return "", composeErr
	}
	// TaskBlockKindBlocked, not escalated: the engine did not give up on the
	// work — it never got to it — and the condition is one an operator repairs
	// and then re-labels, which is what the class on the block releases.
	return s.park(orchestration.TaskBlockKindBlocked, reason, orchestration.FaultWorkspaceUnfit), nil
}

// openWorkspace is the ordered part: the spawn, the clone, the proof. Split out
// so prepareWorkspace's judgement reads as the one decision it is.
func (s *runnerRun) openWorkspace(ctx context.Context) error {
	if err := s.spawnWorker(ctx); err != nil {
		return err
	}
	if err := s.makeCheckout(ctx); err != nil {
		return err
	}
	return s.verifyWorkspace(ctx)
}

// verifyWorkspace proves the checkout the worker is about to be prompted on is
// the one the spec described, and records every probe it ran on the job.
//
// THE PROBES ARE THE EVIDENCE, whether it passed or failed. A pre-flight that
// refused and said only that leaves an operator to re-run the same commands by
// hand; the point of running them here is that nobody has to.
func (s *runnerRun) verifyWorkspace(ctx context.Context) error {
	return s.checkout().Verify(ctx)
}

// checkout is this attempt's checkout: the clone that makes it and the
// pre-flight that proves it, driven through the one seam all three runners
// share.
func (s *runnerRun) checkout() containerCheckout {
	return containerCheckout{
		job:       s.job,
		sessionID: s.sessionID,
		spec:      s.spec,
		project:   s.job.Project,
		taskID:    s.taskID(),
	}
}

// spawnWorker starts the attempt's single session and records where its work
// happens.
//
// THE SESSION MUST BE A CONTAINER. The delivery's commit, its signature and
// its push all happen inside it, so a session that came back without one is
// refused here rather than at publish time, before a worker has spent a turn
// on work the runner could never publish.
func (s *runnerRun) spawnWorker(ctx context.Context) error {
	if err := s.requireProject(); err != nil {
		return err
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
		return &WorkerSessionError{TaskID: s.taskID(), Message: "the spawn returned no session"}
	}
	s.sessionID = resp.SessionID
	if strings.TrimSpace(resp.ContainerID) == "" {
		return &WorkerNotContainerizedError{TaskID: s.taskID(), SessionID: resp.SessionID}
	}
	if resp.Workspace == nil {
		return &WorkerSessionError{TaskID: s.taskID(), Message: "the spawn reported no workspace for the checkout"}
	}
	// The job's working directory is THE PROJECT CHECKOUT, not the mount root
	// it sits in. /workspace is the whole checkout root — the project in its
	// own directory and each context repository beside it — so a job that
	// recorded the root would point every reader one level above the
	// repository the attempt is about.
	s.containerDir = filepath.Join(s.runner.access.checkoutRoot, s.job.Project.Name)
	s.workDir = filepath.Join(resp.Workspace.Dir, s.job.Project.Name)
	s.job.SetWorkDir(s.workDir)
	// AND THE JOB'S WORK HAPPENS IN THE CONTAINER, which is what this says.
	// s.workDir names the host side of the bind mount ON THE HOST THAT RAN THE
	// SESSION — a fleet node's, as often as not, where it names nothing on
	// this daemon; the container names the same checkout /workspace/<project>,
	// and that is the only name anything the runner EXECUTES may use.
	if err := s.job.SetContainer(s.sessionID, s.containerDir); err != nil {
		return err
	}
	// AND THE WORK ONLY SURVIVES THE CONTAINER IF IT IS PUSHED, which is what
	// this says. The engine decides WHEN — after a turn that touched the tree
	// — and this run supplies the publication; see PushTurn. Installed in the
	// same breath as the container, because the two describe the same fact:
	// where this job's work physically is.
	s.job.SetTurnPusher(s)
	return s.runner.hooks.updateDelivery(ctx, s.job.Context(), s.delivery, func(d *Delivery) {
		d.WorkDir = s.containerDir
	})
}

// workspaceKey names the attempt's workspace: one per attempt, so every
// session the attempt spawns — its rounds, a rolled-over session, the respawn
// after a restart — continues the same clone, on the host the job is pinned
// to, and a new attempt starts clean.
func (s *runnerRun) workspaceKey() string {
	attemptNo := 1
	if s.job.Config != nil && s.job.Config.Attempt > 0 {
		attemptNo = s.job.Config.Attempt
	}
	return s.taskID() + "-a" + strconv.Itoa(attemptNo)
}

// requireProject refuses a job with no project record. Everything an attempt
// needs to describe itself — the repository, the identity, the workspace root
// — hangs off it, so there is nothing to guess from.
func (s *runnerRun) requireProject() error {
	if s.job.Project == nil {
		var taskID, projectID string
		if s.job.Config != nil {
			taskID, projectID = s.job.Config.TaskID, s.job.Config.ProjectID
		}
		return &ProjectRecordMissingError{TaskID: taskID, ProjectID: projectID}
	}
	return nil
}

// spawnRequest builds the worker's spawn: the charter as its system prompt,
// the binding's worker Agent or pool, and the workspace spec, forge token and
// signing socket mounted.
func (s *runnerRun) spawnRequest(ctx context.Context) (*orchestration.SpawnSessionRequest, error) {
	// THE WORKER SLOT NAMES AN AGENT OR A POOL, and the spawn resolves it:
	// the Agent supplies the runtime, the model and the image, and a pool
	// fails over to its next Agent when one's accounts are exhausted or its
	// backend is unavailable.
	worker := s.attempt.binding.Worker
	req := &orchestration.SpawnSessionRequest{
		AgentID:      worker.AgentID,
		PoolID:       worker.PoolID,
		Role:         orchestration.AgentRoleCoding,
		ProjectID:    s.job.Project.ID,
		SystemPrompt: WorkerCharter(),
		PromptAppend: s.attempt.binding.WorkerPromptAppend,
		Persistent:   true,
	}
	// THE WORKER'S GO MUST REACH THE PROJECT'S PRIVATE MODULES, or its
	// verification fails before its change compiles and the failure is
	// charged to the work. See ModulePolicy.
	moduleEnv, err := ModuleEnv(s.attempt.binding.Modules, s.job.Project.Git.RepoURL)
	if err != nil {
		return nil, err
	}
	if len(moduleEnv) > 0 {
		if req.Env == nil {
			req.Env = make(map[string]string, len(moduleEnv))
		}
		for k, v := range moduleEnv {
			req.Env[k] = v
		}
	}
	// SigningScopeDelivery, which is the whole of what makes this container
	// different from the reviewer's and triage's: its agent serves the commit
	// signing key as well as the access key, because this worker commits,
	// signs and pushes. The socket and every path naming it are put on the
	// request by the seam.
	workspace, err := s.runner.access.Open(ctx, req, workspacePlan{
		TenantID: s.job.TenantID,
		Project:  s.job.Project,
		Forge:    s.attempt.binding.Forge,
		Branch: orchestration.WorkspaceBranchPlan{
			Name:   s.delivery.Branch,
			Base:   s.delivery.Base,
			Create: s.delivery.HeadSHA == "",
		},
		Key:   s.workspaceKey(),
		Scope: SigningScopeDelivery,
	})
	if err != nil {
		return nil, err
	}
	stopCtx := context.WithoutCancel(ctx)
	s.teardown = append(s.teardown, func() { workspace.Release(stopCtx) })
	s.workspace = workspace
	// The spec the checkout is driven from is the one that was WRITTEN — the
	// same bytes the container reads at $QUICBAAS_WORKSPACE_SPEC — so both
	// sides act on one description and neither invents a sibling file.
	s.spec = workspace.Spec
	return req, nil
}

// containerExec runs one command in the worker's container. It is THE ONLY WAY
// this runner runs anything: the checkout and the publish both take it, so
// there is no path by which a command of either could run on the daemon
// instead.
func (s *runnerRun) containerExec(ctx context.Context) func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	return sessionExec(ctx, s.job, s.sessionID)
}

// publisher is the git side of this attempt, bound to the worker's container.
func (s *runnerRun) publisher(ctx context.Context) *publisher {
	return &publisher{
		exec:    s.containerExec(ctx),
		cleanup: s.containerCleanupExec(ctx),
		project: s.job.Project,
		signing: s.workspace.signingSession(),
		taskID:  s.taskID(),
		admit: func() error {
			return s.runner.hooks.admitAttempt(ctx, s.job.TenantID, s.taskID(), s.job.JobID)
		},
	}
}

// containerCleanupExec runs one UNDO command in the worker's container on a
// context detached from the stage's, bounded by the discard timeout.
//
// A CANCELLED STAGE IS WHEN THE UNDO MATTERS MOST. The stage context is
// already done when a verification times out or the daemon is shutting down,
// and a cleanup issued through containerExec would then be refused before it
// reached the container — leaving, in this runner's case, a git worktree
// inside a checkout the next attempt on that node will use. The bound is the
// SAME one every other detached cleanup in this runner takes (rollSession,
// the teardown), because an unbounded undo on a dying daemon is a hang.
//
// The context is made PER COMMAND, so nothing holds a timer open for the life
// of the publisher.
func (s *runnerRun) containerCleanupExec(ctx context.Context) func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	sessionID := s.sessionID
	return func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx),
			s.job.Policy().StageTimeouts.For(orchestration.JobStageDiscard))
		defer cancel()
		return s.job.Exec(cleanupCtx, &orchestration.ExecSessionRequest{SessionID: sessionID, Spec: spec})
	}
}

// makeCheckout clones the project and its context repositories inside the
// worker's container and puts the planned branch on the project checkout.
//
// THE COMMANDS ARE THE EVIDENCE. Every one of them, with what it said, is
// recorded on the job whether the checkout succeeded or failed: a clone that
// could not reach the forge is the most common way an attempt dies, and the
// only place a person can see why is the job it died on.
func (s *runnerRun) makeCheckout(ctx context.Context) error {
	return s.checkout().Make(ctx)
}

// publishPlan describes what this attempt publishes: the branch under review,
// or the target itself on the direct-push carve-out.
func (s *runnerRun) publishPlan() publishPlan {
	plan := publishPlan{
		WorkDir: s.containerDir,
		Branch:  s.delivery.Branch,
		Base:    s.delivery.Base,
		Message: deliveryCommitMessage(s.attempt, s.job.Context()),
	}
	if !s.delivery.RequiresPullRequest {
		plan.RemoteBranch = s.delivery.Target
	}
	return plan
}

// =============================================================================
// 2. executing — the issue contract, verbatim
// =============================================================================

// execute prompts the worker with the task's description, which is the issue
// contract the source rendered: the repository, the issue number, the title,
// the body and the whole discussion.
//
// VERBATIM, NEVER A PARAPHRASE. The lead-driven loop this replaced handed the
// worker an LLM's summary of the issue framed as "Execute the following
// task", and the worker never saw what a person actually wrote. On a review
// round the findings are appended — the contract stays first and whole.
func (s *runnerRun) execute(ctx context.Context) (orchestration.JobState, error) {
	if s.sessionID == "" {
		// FOUR RUNS LEGITIMATELY REACH A TURN WITH NO SESSION: one resumed
		// at reviewing, one whose full session was rolled, one whose worker
		// was released for the review (releaseWorker) and the gate has now
		// asked for a round, and one that never spawned because its task
		// already had an open pull request (resumeReview sent it straight to
		// the gate) and the gate has now asked for a round. The last was refused here as a run that skipped
		// initialization — measured on go-quicbaas#53 the moment the gate
		// began reporting a red pipeline ahead of a missing approval
		// (2026-09-21): the CI-red round it had waited an hour for died on
		// this line. A run with NO round asked for and no session is the
		// programming error the refusal is for.
		if s.job.Resumed == nil && !s.rolled && !s.roundRequested {
			return "", &WorkerSessionError{TaskID: s.taskID(), Message: "there is no worker session to prompt"}
		}
		// Initializing is the only state that spawns a worker; the round
		// and its findings are already on the job's iteration and memory.
		s.job.Emit(orchestration.JobStateInitializing,
			"another round needs a worker this run does not hold; the attempt's container is spawned")
		return orchestration.JobStateInitializing, nil
	}
	if s.attempt == nil {
		if err := s.resolve(ctx); err != nil {
			return "", err
		}
	}
	// THE POOL IS ASKED BEFORE THE TURN IS SPENT. A session materialized its
	// provider credential at spawn and never re-reads it, so an account whose
	// credential was rotated since leaves this session holding material the
	// provider has stopped honouring: the prompt below would fail in seconds
	// as an api_error, refund the attempt and re-dispatch the whole round
	// (go-quicbaas#997). See replaceStaleCredential.
	if s.canReplaceStaleCredential() {
		if rotated := s.job.AccountRotated(ctx, s.sessionID); rotated != nil {
			return s.replaceStaleCredential(ctx, rotated)
		}
	}
	prompt, err := s.turnPrompt(ctx)
	if err != nil {
		return "", err
	}
	// THE NOTE IS CONSUMED BY THE PROMPT IT WAS WRITTEN FOR. Each of the
	// three — a turn that answered with nothing, a session replaced because
	// its context filled, a reply that did not address the prompt — is an
	// account of what just happened; carried into a later round it would tell
	// a worker its last reply missed when it did not.
	s.turnNote = ""
	repeated, err := s.repeatedPrompt(prompt)
	if err != nil {
		return "", err
	}
	if repeated != "" {
		return s.park(orchestration.TaskBlockKindEscalated, repeated, ""), nil
	}
	state, err := s.runTurn(ctx, prompt)
	if err != nil {
		// THE ACCOUNT FIRST. A refused account makes every other reading of
		// the failure beside the point: the turn never reached the model, and
		// whatever else the evidence says, the fix is another account.
		if refusal, refused := accountRefusalOf(err); refused && s.canRollOffAccount(refusal.accountID) {
			return s.rollOffAccount(ctx, refusal)
		}
		if class, exhausted := orchestration.ContextExhaustionOf(err); exhausted && s.canRollSession() {
			return s.rollSession(ctx, class)
		}
		return "", err
	}
	return state, nil
}

// canRollOffAccount reports whether a session refused on accountID may be
// replaced: once per account per run. See runnerRun.limitedAccounts.
func (s *runnerRun) canRollOffAccount(accountID string) bool {
	if accountID == "" {
		return false
	}
	_, seen := s.limitedAccounts[accountID]
	return !seen
}

// accountRefusal is the provider refusing the worker's pooled account: for a
// limit (held until its reset) or for a dead login (errored until an operator
// logs it in again). Either way the fix is another account.
type accountRefusal struct {
	accountID   string
	class       string
	why         string
	availableAt time.Time
}

// accountRefusalOf reads the refusal a failed turn carries, if any.
func accountRefusalOf(err error) (accountRefusal, bool) {
	if limit, limited := orchestration.AccountLimitOf(err); limited {
		return accountRefusal{accountID: limit.AccountID, class: orchestration.FaultAccountLimited,
			why: "for a limit", availableAt: limit.AvailableAt}, true
	}
	if rejected, ok := orchestration.AccountAuthRejectionOf(err); ok {
		return accountRefusal{accountID: rejected.AccountID, class: orchestration.FaultAccountAuthRejected,
			why: "because its login was rejected"}, true
	}
	return accountRefusal{}, false
}

// rollOffAccount replaces the worker's session with a fresh one on another
// pooled account, because the provider refused this one — for a limit, or for
// a login it rejected.
//
// THE OWNER'S RULE: switch accounts when a limit is reached. The orchestration
// layer already held the refused account until the provider's stated reset
// (holdOnAccountLimit), so the fresh spawn's selection cannot choose it; this
// is the other half, and it is the context rollover's mechanics exactly: push
// what the session committed, close it, spawn a new one on the same branch in
// the same round. The attempt is not charged, because nothing about the work
// was judged. When the pool has no other account free, that spawn fails with
// the pool's own cooldown, which requeues the task, uncharged, until the first
// hold lifts.
//
// WHAT THE NEW SESSION KEEPS IS THE PROJECT'S CHOICE (Project.SessionRotation),
// exactly as for a rotated credential (replaceStaleCredential): resume carries
// the refused session's conversation into the new one and is audited as
// sdlc:session_resumed; fresh tells the new session only why it is new and is
// audited as sdlc:session_rolled. See conversationToCarry.
func (s *runnerRun) rollOffAccount(ctx context.Context, refusal accountRefusal) (orchestration.JobState, error) {
	if s.limitedAccounts == nil {
		s.limitedAccounts = make(map[string]struct{})
	}
	s.limitedAccounts[refusal.accountID] = struct{}{}
	round := s.job.Iteration()
	detail := "class=" + refusal.class + " account=" + refusal.accountID +
		" round=" + strconv.Itoa(round) + " session=" + s.sessionID
	if !refusal.availableAt.IsZero() {
		detail += " available_at=" + refusal.availableAt.UTC().Format(time.RFC3339)
	}
	carried, err := s.conversationToCarry(ctx)
	if err != nil {
		return "", err
	}
	detail += carried.auditFields()
	refused := "the provider refused the worker's pooled account " + refusal.accountID + " " + refusal.why +
		" in round " + strconv.Itoa(round)
	if carried.resumed() {
		s.job.Audit(auditSessionResumed, detail)
		return s.replaceSession(ctx, accountResumedNote(round, carried.transcript),
			refused+"; the session's conversation is resumed in a new session on another account on the same branch")
	}
	s.job.Audit(auditSessionRolled, detail)
	return s.replaceSession(ctx, accountRolledNote(round),
		refused+"; a fresh session is spawned on another account on the same branch")
}

// canRollSession reports whether a session whose context is full may be
// replaced in this round: once per round, never twice.
func (s *runnerRun) canRollSession() bool {
	return !s.rolled || s.rolledRound != s.job.Iteration()
}

// rollSession replaces the worker's session with a fresh one on the same
// branch, because the old one's context is full.
//
// THE BRANCH IS THE MEMORY; THE CONVERSATION IS NOT. A delivery turn of
// twenty minutes reads and runs enough that the session's context fills, and
// in print mode the CLI cannot compact it: the next prompt — the round's
// verification findings, a few hundred bytes — is refused after minutes with
// no text and no usage (`blocking_limit`, which the CLI files under
// context_limit). Measured twice on the burn-in fleet on 2026-09-21
// (go-quicbaas #51 and #965): both had passed build and test, both were
// failed as attempts, and both redispatches began the same twenty-minute turn
// over in a session that would fill the same way.
//
// What the situation calls for is what a person does at that point: keep the
// branch, open a new session. Whatever the dead session committed and did not
// commit is committed and pushed first, so nothing it did is lost and the
// fresh container checks out exactly that head; the round, its findings and
// the job's memory are untouched, because they live on the run and the job,
// not in the conversation; and the fresh session is told, once, why it is
// fresh, so it reads the branch before it writes. The attempt is not charged:
// the work was never judged.
//
// ONCE PER ROUND. A fresh session that fills again on the same prompt has a
// prompt the work cannot take, which is `prompt_too_long`'s class and is
// charged like it.
func (s *runnerRun) rollSession(ctx context.Context, class string) (orchestration.JobState, error) {
	round := s.job.Iteration()
	elapsed := s.lastTurn.Elapsed.Round(time.Second)
	s.rolled, s.rolledRound = true, round
	s.job.Audit(auditSessionRolled, "class="+class+" round="+strconv.Itoa(round)+
		" session="+s.sessionID+" elapsed="+elapsed.String())
	return s.replaceSession(ctx, sessionRolledNote(elapsed, round),
		"the worker's session ran out of context ("+class+") in round "+strconv.Itoa(round)+
			"; a fresh session is spawned on the same branch")
}

// canReplaceStaleCredential reports whether a session whose pooled account
// was rotated under it may be replaced in this round: once per round.
//
// The replacement materializes the credential the pool holds NOW, so a second
// report inside one round is the pool moving twice. Refusing it keeps a
// flapping pool from spawning containers without end, and costs exactly the
// failure the run would have had without the check.
func (s *runnerRun) canReplaceStaleCredential() bool {
	return !s.credentialReplaced || s.credentialReplacedRound != s.job.Iteration()
}

// sessionRotation is the policy the job's project runs when a worker's session
// is replaced for a reason that is not the work's (see conversationToCarry);
// the default for a project that never chose one.
func (s *runnerRun) sessionRotation() orchestration.SessionRotationPolicy {
	var policy orchestration.SessionRotationPolicy
	if s.job.Project != nil {
		policy = s.job.Project.SessionRotation
	}
	return policy.Effective()
}

// replaceStaleCredential replaces a session whose pooled provider account's
// credential was rotated after the session took its copy.
//
// A SESSION TAKES ITS CREDENTIAL ONCE, AT SPAWN, into its container's process
// environment and a per-session directory, and neither is re-read. So the
// only way onto the current credential is a new container — the same four
// steps as every other replacement (see replaceSession): push what the session
// committed, close it, spawn again on the same branch, in the same workspace
// and the same round. The attempt is not charged and the round does not
// advance: nothing about the work was judged.
//
// WHAT THE NEW SESSION KEEPS IS THE PROJECT'S CHOICE (Project.SessionRotation):
//
//   - resume, the default: the old session's conversation is read BEFORE it
//     is closed and carried into the new session's first prompt, so the
//     worker continues where it was rather than re-deriving what it had
//     already worked out. Audited as sdlc:session_resumed.
//   - fresh: the new session is told only why it is new and reads the branch
//     (go-quicbaas#997 as first shipped). Audited as sdlc:session_rolled.
//
// A resume whose conversation cannot be read — the service keeps no session
// log, or the log will not answer — is replaced fresh rather than not at all,
// and the audit record says so (resume_unavailable=). The alternative is
// prompting a session the provider will refuse, which loses the whole round.
func (s *runnerRun) replaceStaleCredential(
	ctx context.Context,
	rotated *orchestration.SessionAccountFreshness,
) (orchestration.JobState, error) {
	round := s.job.Iteration()
	s.credentialReplaced, s.credentialReplacedRound = true, round
	detail := "reason=" + auditReasonAccountRotated + " account=" + rotated.AccountID + " " +
		accountRotationEvidence(rotated) + " round=" + strconv.Itoa(round) + " session=" + s.sessionID

	carried, err := s.conversationToCarry(ctx)
	if err != nil {
		return "", err
	}
	detail += carried.auditFields()
	if carried.resumed() {
		s.job.Audit(auditSessionResumed, detail)
		return s.replaceSession(ctx, sessionResumedAccountNote(round, carried.transcript),
			"the worker's pooled provider account was rotated after this session took its credential; "+
				"the session's conversation is resumed in a new session on the same branch")
	}

	s.job.Audit(auditSessionRolled, detail)
	return s.replaceSession(ctx, sessionRolledAccountNote(round),
		"the worker's pooled provider account was rotated after this session took its credential; "+
			"a fresh session is spawned on the same branch")
}

// carriedConversation is what a session replacing the live worker continues,
// under the project's session_rotation. The zero value starts fresh.
type carriedConversation struct {
	// transcript is the replaced session's conversation; nil means the new
	// session starts a new one.
	transcript *orchestration.ReplayTranscript

	// unavailable is why a resume the project asked for could not read the
	// conversation, so the new session starts fresh instead.
	unavailable string
}

// resumed reports whether the new session continues the old conversation.
func (c carriedConversation) resumed() bool {
	return c.transcript != nil
}

// auditFields renders, as audit fields with a leading space, what the new
// session was handed: how much conversation was replayed, or why the resume
// the project asked for was unavailable. A fresh project records nothing.
func (c carriedConversation) auditFields() string {
	switch {
	case c.transcript != nil:
		return " replayed_turns=" + strconv.Itoa(len(c.transcript.Turns)) +
			" omitted_turns=" + strconv.Itoa(c.transcript.Omitted)
	case c.unavailable != "":
		return " resume_unavailable=" + strconv.Quote(c.unavailable)
	}
	return ""
}

// conversationToCarry reads the conversation the live worker session's
// replacement continues, under the project's session_rotation. It must be
// called BEFORE the session is closed: the conversation is read from the
// session's own event log, which answers only for a live session.
//
// ONE RULE FOR EVERY REPLACEMENT that is not the work's doing: a rotated
// credential (go-quicbaas#997), a refused account (go-quicbaas#1046) and a
// worker released for a review (go-quicbaas#938) all carry the conversation on
// resume, the default, and none of it on fresh.
//
// A resume whose conversation cannot be read — the service keeps no session
// log, or the log will not answer — starts fresh and says so
// (carriedConversation.unavailable): the session is being replaced whatever
// this answers. Any other failure is returned, because the session is not what
// the runner believes it is and a fresh session does not answer that.
func (s *runnerRun) conversationToCarry(ctx context.Context) (carriedConversation, error) {
	if s.sessionRotation() != orchestration.SessionRotationResume {
		return carriedConversation{}, nil
	}
	transcript, err := s.job.SessionTranscript(ctx, s.sessionID)
	if err == nil {
		return carriedConversation{transcript: transcript}, nil
	}
	var unavailable *orchestration.SessionTranscriptUnavailableError
	if !errors.As(err, &unavailable) {
		return carriedConversation{}, err
	}
	return carriedConversation{unavailable: unavailable.Reason}, nil
}

// refusedPush answers a publication that failed: a push the publisher refused
// for an untrusted signature parks the task escalated for a person, and any
// other failure is returned as it was.
//
// PARKED, NOT RETRIED. The factory never rewrites a pushed branch to re-sign
// a commit, so every later attempt would verify the same commits and refuse
// identically — go-luks2 task 143c280a spent its attempts exactly that way on
// PR #194's pre-rotation commits. The park carries FaultUntrustedSigner, an
// operator fault, so the attempt is refunded; the merge train parks an
// ejected car the same way (trainEjected).
func (s *runnerRun) refusedPush(err error) (orchestration.JobState, error) {
	var unsigned *UnsignedCommitError
	if !errors.As(err, &unsigned) {
		return "", err
	}
	return s.park(orchestration.TaskBlockKindEscalated, untrustedSignerReason(unsigned), orchestration.FaultUntrustedSigner), nil
}

// untrustedSignerReason is what a person reads on a park for an untrusted
// signature: the commit, what git said about it, and the two ways out.
func untrustedSignerReason(unsigned *UnsignedCommitError) string {
	detail := "Commit `" + shortSHA(unsigned.Commit) + "` is not signed by a key this project trusts for it (git signature status " +
		strconv.Quote(unsigned.Status)
	if unsigned.Signer != "" {
		detail += ", signer `" + unsigned.Signer + "`"
	}
	return detail + "). The factory never rewrites a pushed branch. Record the signing key in the project's trusted " +
		"signing history if it was the project's, or replace the commit, then readmit the task."
}

// refusedVerification answers a verification command the operator's allowlist
// refuses: the task is parked for an operator with FaultConfigError, so the
// attempt is refunded, and any other error is returned as it was.
//
// PARKED, NOT RETRIED, AND BEFORE ANY WORKER IS PAID. The allowlist is daemon
// configuration and the command is the binding's, so no attempt changes the
// answer. The guard used to discover it only at validation, after the worker's
// turn, and the refusal was charged as a failure of the work: go-blockstore
// tasks 41a680e0 and b9f897ca each spent three attempts that way (X14,
// 2026-09-25). initialize now refuses before anything is spawned, and validate
// parks the same way for an allowlist that changed while the job ran.
func (s *runnerRun) refusedVerification(err error) (orchestration.JobState, error) {
	if !errors.Is(err, orchestration.ErrCommandRefused) {
		return "", err
	}
	reason, composeErr := ComposeBlockedReason(UnblockCondition{Kind: UnblockOperatorRepair},
		"This project's verification cannot run on this node: "+err.Error()+". Add the command to "+
			"orchestration.verification_commands, or rebind the project with a command the allowlist carries, "+
			"then readmit the task. No worker turn was charged for it.")
	if composeErr != nil {
		return "", composeErr
	}
	return s.park(orchestration.TaskBlockKindBlocked, reason, orchestration.FaultConfigError), nil
}

// replaceSession is the mechanics both rollovers share: publish what the
// session committed, close it, and go back to initializing so a fresh session
// is spawned on the same branch in the same round, told once why it is fresh.
func (s *runnerRun) replaceSession(ctx context.Context, note, event string) (orchestration.JobState, error) {
	if err := s.pushTurnWork(ctx); err != nil {
		return s.refusedPush(err)
	}
	s.closeWorker(ctx)
	s.turnNote = note
	s.job.Emit(orchestration.JobStateInitializing, event)
	return orchestration.JobStateInitializing, nil
}

// pushTurnWork commits whatever the worker's checkout holds and pushes the
// attempt's branch, recording the head on the delivery so a container spawned
// after this one checks that head out rather than cutting the branch again
// from its base. A checkout with nothing new to publish is not an error.
func (s *runnerRun) pushTurnWork(ctx context.Context) error {
	if s.delivery == nil || s.sessionID == "" || s.containerDir == "" || s.attempt == nil {
		// The engine only calls this after a turn taken in a live container,
		// which cannot be reached without all four. Refusing — rather than
		// returning nil — is deliberate: on this tier a skipped push is
		// silent data loss, so the impossible case must be loud.
		return &WorkerSessionError{
			TaskID:  s.taskID(),
			Message: "the turn's work cannot be published: this run holds no delivery, session or checkout",
		}
	}
	plan := s.publishPlan()
	plan.RemoteBranch = ""
	head, err := s.publisher(ctx).Publish(plan)
	if err != nil {
		var nothing *NoChangesError
		if errors.As(err, &nothing) {
			return nil
		}
		return err
	}
	return s.runner.hooks.updateDelivery(ctx, s.job.Context(), s.delivery, func(d *Delivery) {
		d.HeadSHA = head
	})
}

// turnPrompt is the whole text one turn is handed: the issue contract, the
// round's findings, and whatever the last no-change turn added.
func (s *runnerRun) turnPrompt(ctx context.Context) (string, error) {
	prompt := s.freshContract(ctx)
	if strings.TrimSpace(prompt) == "" {
		return "", &InvalidInputError{
			Field:   "task.description",
			Message: "the task carries no issue contract; there is nothing to hand the worker",
		}
	}
	if findings := s.roundFindings(); findings != "" {
		prompt += "\n\n" + findings
	}
	if note := s.priorTurnNote(); note != "" {
		prompt += "\n\n" + note
	}
	return prompt, nil
}

// freshContract is the issue contract as the forge holds it NOW, or the one
// the task was adopted with when the forge will not answer.
//
// THE CONTRACT USED TO BE RENDERED ONCE, AT ADOPTION. Task.Description was
// written by the issue source when the task was created and read back by
// every attempt after, so a worker readmitted after a park was handed the
// discussion as it stood the day the task was adopted -- and never saw the
// comment that unparked it. That comment is the whole mechanism: a needs-info
// is answered on the issue, an operator repair is posted on the issue, a
// ruling is given on the issue, and then the task is readmitted. Measured on
// go-truststrap#24 (2026-09-21): the template the worker had asked for was
// posted verbatim, the task readmitted, and attempt 3 spent three review
// rounds insisting the file "cannot be found", because its prompt predated
// the post. The forge is therefore asked at every turn; two reads, against
// a worker turn that costs minutes.
//
// FAIL-SOFT, AUDITED. A forge that cannot be read leaves the worker the
// adopted contract rather than nothing -- the attempt is not failed for the
// forge's outage -- and the audit trail says the prompt may be stale, which
// is what an operator needs when a worker later claims it was not told.
func (s *runnerRun) freshContract(ctx context.Context) string {
	stored := s.attempt.task.Description
	// THE FIRST ATTEMPT IS REFRESHED TOO. It used to keep the adopted
	// contract on the theory that adoption had rendered it moments before
	// dispatch — true for a task adopted ready, false for one adopted PARKED:
	// that task's first attempt runs only after a person has answered the
	// question on the issue, and the answer is exactly what the adopted
	// contract predates (issue_source_parked.go). A task adopted with no
	// contract at all is still refused below rather than papered over by a
	// fresh read.
	if strings.TrimSpace(stored) == "" || s.attempt.forge == nil || s.attempt.issue <= 0 {
		return stored
	}
	issue, err := s.attempt.forge.GetIssue(ctx, s.attempt.repo, s.attempt.issue)
	if err == nil && issue == nil {
		err = &NotFoundError{Entity: "issue", TenantID: s.job.Context().TenantID, ID: IssueRefString(s.attempt.repo, s.attempt.issue)}
	}
	var comments []ForgeComment
	if err == nil {
		comments, err = s.attempt.forge.ListIssueComments(ctx, s.attempt.repo, s.attempt.issue)
	}
	var contract string
	if err == nil {
		contract, err = IssueContract(IssueContractSpec{
			Repo: s.attempt.repo, IssueNumber: issue.Number, Title: issue.Title, Body: issue.Body, Comments: comments,
		})
	}
	if err != nil {
		s.job.Audit("sdlc:contract_stale", "issue="+strconv.Itoa(s.attempt.issue)+
			" error="+err.Error()+"; the worker is handed the contract the task was adopted with")
		return stored
	}
	return contract
}

// priorTurnNote is what this prompt says that the last one did not: the
// account of a previous attempt's no-change turn, and of a turn this run
// already took that answered with nothing.
//
// IT IS THE "NEW INPUT" RULE MADE CONCRETE. An attempt that changed nothing
// is followed by an attempt rendering the SAME issue contract, so with
// nothing added the second worker is handed the first one's prompt verbatim
// and has every reason to answer the same way — measured three times on
// go-truststrap#26 (go-quicbaas#787). What is added is not an instruction; it
// is evidence the previous worker could not have had.
func (s *runnerRun) priorTurnNote() string {
	var b strings.Builder
	if last := s.deliveryTurn(); last != nil {
		b.WriteString(renderPriorTurn(*last))
	}
	if s.turnNote != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(s.turnNote)
	}
	return b.String()
}

// deliveryTurn is the no-change turn a previous attempt at this task ended
// on, nil when the last attempt reached the checkout or there was none.
func (s *runnerRun) deliveryTurn() *AgentTurn {
	if s.delivery == nil {
		return nil
	}
	return s.delivery.LastTurn
}

// renderPriorTurn tells this worker what the last attempt at the task said
// and that it changed no file.
func renderPriorTurn(turn AgentTurn) string {
	classification := ""
	if turn.Outcome != "" {
		classification = ", " + sdlcPrompts.MustRender("prior_turn_classification", prompts.Vars{
			"outcome": string(turn.Outcome),
		})
	}
	account := sdlcPrompts.MustRender("prior_turn_silent", nil)
	if strings.TrimSpace(turn.Reply) != "" {
		account = sdlcPrompts.MustRender("prior_turn_said", prompts.Vars{"reply": turn.Reply})
	}
	return sdlcPrompts.MustRender("prior_turn", prompts.Vars{
		"attempt":        strconv.Itoa(turn.Attempt),
		"elapsed":        turn.Elapsed.Round(time.Second).String(),
		"classification": classification,
		"account":        account,
	}) + "\n"
}

// repeatedPrompt answers why this prompt must not be sent: it is byte for
// byte the prompt a previous attempt already answered without changing a
// file. It answers empty for a prompt that is safe to send.
//
// A GUARD, NOT THE MECHANISM. priorTurnNote is what ordinarily makes the next
// prompt different; this is what refuses to dispatch when that has somehow
// not happened, so the failure is one parked task with a stated reason rather
// than a budget spent on identical turns.
func (s *runnerRun) repeatedPrompt(prompt string) (string, error) {
	last := s.deliveryTurn()
	if last == nil || last.PromptDigest == "" || last.PromptDigest != promptDigest(prompt) {
		return "", nil
	}
	return repeatedPromptReason()
}

// silentTurnNote is what the next prompt of this run adds when the last turn
// answered with nothing.
func silentTurnNote(elapsed time.Duration) string {
	return sdlcPrompts.MustRender("silent_turn_note", prompts.Vars{
		"elapsed": elapsed.Round(time.Second).String(),
	})
}

// nonAnswerTurnNote is what the re-sent prompt of this run adds when the last
// reply answered something other than the prompt: how fast it came back, and
// that the prompt above is still the one to answer.
func nonAnswerTurnNote(elapsed time.Duration) string {
	return sdlcPrompts.MustRender("non_answer_turn_note", prompts.Vars{
		"elapsed": elapsed.Round(time.Millisecond).String(),
	})
}

// sessionRolledNote is what the first prompt of a fresh session says about
// the session it replaced: how long the turn that filled its context took,
// and in which round.
func sessionRolledNote(elapsed time.Duration, round int) string {
	return sdlcPrompts.MustRender("session_rolled_note", prompts.Vars{
		"elapsed": elapsed.Round(time.Second).String(),
		"round":   strconv.Itoa(round),
	})
}

// accountRotationEvidence renders, as audit fields, WHY the pool was judged to
// have moved under the session.
//
// It names the two credential generations when the daemon had both — the copy
// the session materialized against the copy the account holds now — because
// that pair is the whole decision, and an operator reading the audit trail
// should see which two copies disagreed rather than only that something did.
// It falls back to the rotation timestamp for a session that carries no
// generation, which is the evidence the check used for it.
func accountRotationEvidence(rotated *orchestration.SessionAccountFreshness) string {
	if rotated.SessionGeneration > 0 && rotated.AccountGeneration > 0 {
		return "session_generation=" + strconv.FormatInt(rotated.SessionGeneration, 10) +
			" account_generation=" + strconv.FormatInt(rotated.AccountGeneration, 10)
	}
	return "rotated_at=" + rotated.RotatedAt.UTC().Format(time.RFC3339)
}

// sessionRolledAccountNote is what the first prompt of a FRESH session says
// about the one it replaced when the pool, not the work, ended it.
//
// It carries no elapsed time because there is no failed turn to measure: the
// replacement happens BEFORE a turn is spent, which is the whole point of it.
func sessionRolledAccountNote(round int) string {
	return sdlcPrompts.MustRender("session_rolled_account_note", prompts.Vars{
		"round": strconv.Itoa(round),
	})
}

// sessionResumedAccountNote is what the first prompt of a RESUMED session says:
// why it is in a new container, and the conversation it continues. A session
// replaced before it was ever prompted has no conversation, and is told only
// the first half.
func sessionResumedAccountNote(round int, transcript *orchestration.ReplayTranscript) string {
	return resumedNote("session_resumed_account_note", round, transcript)
}

// accountResumedNote is what the first prompt of a session that CONTINUES the
// conversation of one whose pooled account the provider refused is told.
func accountResumedNote(round int, transcript *orchestration.ReplayTranscript) string {
	return resumedNote("account_resumed_note", round, transcript)
}

// workerResumedNote is what the first prompt of a session spawned for a review
// round is told when it CONTINUES the conversation of the one closed while the
// review ran.
func workerResumedNote(round int, transcript *orchestration.ReplayTranscript) string {
	return resumedNote("worker_resumed_note", round, transcript)
}

// resumedNote renders a resumed session's note: why it is in a new container,
// then the conversation it continues. A session that was never prompted has
// no conversation and is told only the first half.
func resumedNote(name string, round int, transcript *orchestration.ReplayTranscript) string {
	conversation := ""
	if !transcript.Empty() {
		conversation = sdlcPrompts.MustRender("session_resumed_account_transcript", prompts.Vars{
			"transcript": transcript.Render(),
		})
	}
	return sdlcPrompts.MustRender(name, prompts.Vars{
		"round":        strconv.Itoa(round),
		"conversation": conversation,
	})
}

// workerReleasedNote is what the first prompt of a session spawned for a review
// round is told about the one that was closed while the review ran.
func workerReleasedNote(round int) string {
	return sdlcPrompts.MustRender("worker_released_note", prompts.Vars{
		"round": strconv.Itoa(round),
	})
}

// accountRolledNote is what the first prompt of a session that replaced one
// on a refused account is told.
func accountRolledNote(round int) string {
	return sdlcPrompts.MustRender("account_rolled_note", prompts.Vars{
		"round": strconv.Itoa(round),
	})
}

// repeatedPromptReason is why a task parks rather than being handed a prompt
// a previous attempt already answered without changing a file.
func repeatedPromptReason() (string, error) {
	return ComposeBlockedReason(UnblockCondition{Kind: UnblockOwnerRuling},
		sdlcPrompts.MustRender("repeated_prompt", nil))
}

// promptDigest identifies a prompt by content, so "the same prompt" is a
// decidable question rather than a judgement.
func promptDigest(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// runTurn takes ONE turn: the prompt goes to the worker, the answer is
// recorded on the job whatever it was, and where the run goes next is decided
// by whether the checkout moved.
//
// THE RECORD IS WRITTEN BEFORE ANYTHING IS DECIDED, and on the failure path
// too. What the worker said is the only account of an attempt that produced
// no diff, and a turn whose text is discarded is a turn nobody can review.
func (s *runnerRun) runTurn(ctx context.Context, prompt string) (orchestration.JobState, error) {
	// THE BASELINE IS READ BEFORE THE WORKER IS PROMPTED. It is the only
	// moment at which "the checkout as this turn found it" can be observed.
	baseline, err := s.turnBaseline(ctx)
	if err != nil {
		return "", err
	}
	started := s.runner.now()
	resp, promptErr := s.job.Prompt(ctx, &orchestration.PromptSessionRequest{
		SessionID: s.sessionID,
		Prompt:    prompt,
	})
	turn := ReadAgentTurn(resp)
	// THE ELAPSED TIME IS THE TURN'S OWN MEASUREMENT, taken once and read by
	// everything downstream: the sdlc:agent_turn event and the
	// NonAnswerTurnBound decision both take it from here, so the record and
	// the routing cannot disagree about how long the turn took.
	turn.Elapsed = s.runner.now().Sub(started)
	turn.Attempt = s.attemptNumber()
	turn.Round = s.job.Iteration()
	turn.PromptDigest = promptDigest(prompt)
	// THE TURN IS CLASSIFIED BEFORE ITS EVENT IS WRITTEN, so the event
	// carries the verdict. The event is the operator's account of the turn;
	// one that says how long the turn took and what it replied but not what
	// the factory CONCLUDED about it sends a reader to a second event to
	// find out, which is the thing this record exists to avoid.
	//
	// The two failure paths audit immediately instead, because there is no
	// classification to wait for: a prompt that errored and a tree that
	// could not be read both end the turn with nothing decided about it,
	// and the account must survive either way.
	if promptErr != nil {
		s.recordTurn(turn)
		return "", promptErr
	}
	touched, err := s.workerTouchedTree(ctx, baseline)
	if err != nil {
		s.recordTurn(turn)
		return "", err
	}
	if !touched {
		verdict := ClassifyTurn(turn)
		turn.Outcome = verdict.Outcome
		s.recordTurn(turn)
		return s.noChangeTurn(ctx, turn, verdict)
	}
	s.recordTurn(turn)
	// THE TURN'S WORK IS PUBLISHED BEFORE THE RUN MOVES ON. This is the one
	// place in the engine that knows a turn touched the tree, and on an
	// ephemeral workspace the window between here and the next state is a
	// window in which a restart loses everything the worker just did. A
	// failure ends the turn rather than being logged past: an attempt that
	// kept building on work it had already lost would spend a whole budget
	// producing a head nobody can fetch.
	if err := s.job.PushTurn(ctx); err != nil {
		return s.refusedPush(err)
	}
	if err := s.forgetNoChangeTurn(ctx); err != nil {
		return "", err
	}
	return orchestration.JobStateValidating, nil
}

// recordTurn puts the turn on the job's audit trail and keeps the run's own
// copy of it.
func (s *runnerRun) recordTurn(turn AgentTurn) {
	s.lastTurn = turn
	// THE NON-ANSWER STREAK IS CONSECUTIVE. Anything else this session did —
	// a turn that reached the checkout, a declared stop, a prompt that failed
	// — ends it, so a round three turns later is re-prompted on its own
	// evidence instead of being charged for a non-answer nobody remembers.
	if turn.Outcome != TurnOutcomeNonAnswer {
		s.nonAnswerTurns = 0
	}
	s.job.Audit(auditAgentTurn, turn.Describe())
}

// turnBaseline is the commit the checkout sits on before the worker is
// prompted. It is what makes "did THIS turn change anything" a decidable
// question rather than a guess from the branch's position.
//
// IT IS READ ONCE PER TURN, and it has to be. A branch that is ahead of its
// base proves only that SOME turn was productive: a review round, a
// re-delegation and a resumed attempt all begin on a checkout that already
// carries the commits an earlier turn published, and the published head is
// exactly what a re-spawned container checks out. Reading the baseline at
// this moment is the difference between the two.
//
// A HEAD THAT CANNOT BE READ ENDS THE TURN BEFORE IT COSTS ANYTHING. Without
// a baseline there is no honest classification to make afterwards, and both
// ways of inventing one are wrong: assume the turn was productive and an
// untouched tree goes to verification and review, assume it was not and a
// worker that did the work is charged with a failure.
func (s *runnerRun) turnBaseline(ctx context.Context) (string, error) {
	if s.delivery == nil {
		if err := s.loadDelivery(ctx); err != nil {
			return "", err
		}
	}
	return s.publisher(ctx).Head(s.publishPlan())
}

// workerTouchedTree reports whether THIS TURN reached the checkout:
// uncommitted work the worker left behind, or a HEAD that moved off the
// baseline read before it was prompted.
//
// THE SECOND HALF IS WHAT MAKES A REVIEW ROUND WORK, and why it is measured
// against the turn's own baseline rather than against the base branch. A
// round that commits its own work leaves a clean tree, so reading only the
// working tree would classify it as a worker that did nothing. But the
// branch's position relative to its BASE answers the opposite way just as
// wrongly: after the first productive turn the branch stays ahead forever, so
// every later turn would read as productive — the follow-up worker that
// correctly declares a stop would have its declaration dropped, the stale
// head it never touched would be sent to verification and review, and
// go-quicbaas#787's silent loop would be back for the commonest case of all,
// a task whose pull request already exists.
//
// AheadOfBase keeps the two jobs it is right for: deciding whether there is
// anything to publish (publisher.Publish) and whether an ending attempt has
// commits worth pushing before its container goes (publishBranch). Neither is
// a claim about one turn.
func (s *runnerRun) workerTouchedTree(ctx context.Context, baseline string) (bool, error) {
	publisher := s.publisher(ctx)
	plan := s.publishPlan()
	changed, err := publisher.HasChanges(plan)
	if err != nil {
		return false, err
	}
	if changed {
		return true, nil
	}
	head, err := publisher.Head(plan)
	if err != nil {
		return false, err
	}
	return head != baseline, nil
}

// turnActions is what the runner does about each classified turn. Method
// expressions in a map rather than a switch, so the action answers in
// constant time and the table is the contract.
var turnActions = map[TurnAction]func(*runnerRun, context.Context, TurnVerdict, AgentTurn) (orchestration.JobState, error){
	TurnActionRetry:    (*runnerRun).retryTurn,
	TurnActionPark:     (*runnerRun).parkTurn,
	TurnActionCharge:   (*runnerRun).chargeTurn,
	TurnActionConclude: (*runnerRun).concludeTurn,
	TurnActionReprompt: (*runnerRun).repromptTurn,
}

// concludeTurn ends the attempt as done-with-nothing-to-do.
//
// It is neither parkTurn nor chargeTurn, and the difference is the point. A
// park hands the task to a person and waits; a charge spends one of the task's
// attempts on a failure of the work. A worker that read the tree, found the
// change already present and said so with evidence did its job — so the task
// COMPLETES, no attempt is charged, and the finding is posted on the issue so a
// person can close or restate it.
//
// Before this existed a worker in that position had `owner-decision` or silence
// to choose from. Measured on the burn-in node 2026-09-13: six escalations
// awaiting an owner ruling, four of them one stale README issue each worker had
// correctly reported as already accurate.
func (s *runnerRun) concludeTurn(ctx context.Context, _ TurnVerdict, turn AgentTurn) (orchestration.JobState, error) {
	if err := s.rememberNoChangeTurn(ctx, turn); err != nil {
		return "", err
	}
	// THE BRANCH MAY BE THE WORK. A worker that reads its checkout and finds
	// the change present is right about the CHECKOUT — and on a resumed
	// attempt the checkout is the attempt's own branch, carrying what an
	// earlier attempt wrote and pushed but never got to publish (its
	// verification failed, its session died, the daemon restarted). "Already
	// satisfied" then completed the task with the code sitting unmerged on a
	// branch no pull request names and the issue told nothing will happen.
	// Measured on go-quicbaas#51 (2026-09-21): three attempts of work, pushed
	// at every turn, closed as satisfied by the fourth. The question the
	// verdict must answer is whether the BASE has the work; when the branch
	// is ahead of it and no pull request is open, the branch is verified and
	// published like any other turn's work.
	if ahead, err := s.unpublishedBranchAhead(ctx); err != nil {
		return "", err
	} else if ahead {
		s.job.Audit(auditSatisfiedOnBranch, "the worker found the work already on "+s.delivery.Branch+
			", which is ahead of "+s.delivery.Base+" with no pull request open; it is verified and published rather than concluded")
		s.job.Emit(orchestration.JobStateValidating, "the attempt's branch already carries the work; verifying and publishing it")
		return orchestration.JobStateValidating, nil
	}
	// THE PULL REQUEST MAY BE THE WORK. A worker whose checkout is this
	// task's own branch, with this task's own pull request open on it, finds
	// the change "already present" because it is on the pull request — and
	// completing the task then strands that pull request with nobody to
	// review or merge it (go-luks2 PR #210, 2026-09-22: approved with CI green
	// an hour later, and left unmerged for three days).
	if state, err := s.satisfiedOnOwnPullRequest(ctx); state != "" || err != nil {
		return state, err
	}
	s.job.Audit(auditVerificationSkipped, "the worker reported the task's work already present and "+
		"changed no file, so there was nothing to verify and nothing to review")
	// The judgement goes on the run, which puts it on the persisted status:
	// applyConclusion reads it there, so it survives a crash between the turn
	// and the conclusion exactly as a park does.
	s.job.Satisfy()
	return orchestration.JobStateCompleted, nil
}

// satisfiedOnOwnPullRequest routes a satisfied turn whose task already has its
// own pull request to where that pull request's life goes on, answering ""
// when the task has none.
//
// THE FORGE IS ASKED, NOT THE RECORD, as resumeReview asks it: an open pull
// request goes to review, the gate judging it exactly as a resumed attempt's
// would; one merged by any path completes the task as landed; one closed
// without merging supersedes it. Only a task with no pull request of its own
// is a stale issue the verdict can conclude.
func (s *runnerRun) satisfiedOnOwnPullRequest(ctx context.Context) (orchestration.JobState, error) {
	if s.delivery == nil || !s.delivery.RequiresPullRequest || !s.delivery.PullRequest.Opened() || s.attempt == nil {
		return "", nil
	}
	pr, err := s.attempt.forge.GetPullRequest(ctx, s.attempt.repo, s.delivery.PullRequest.Number)
	if err != nil {
		return "", err
	}
	if pr == nil {
		return "", nil
	}
	number := "#" + strconv.Itoa(pr.Number)
	switch {
	case pr.Merged:
		s.job.Audit(auditSatisfiedOnPullRequest, "the worker found the work present; pull request "+number+" has merged")
		s.job.Emit(orchestration.JobStateCompleting, "pull request "+number+" carrying this task's work has merged")
		return orchestration.JobStateCompleting, nil
	case pr.State == "closed":
		return s.supersede(pr.Number), nil
	}
	s.job.Audit(auditSatisfiedOnPullRequest, "the worker found the work present on this task's own open pull request "+
		number+"; it goes to review rather than concluding the task")
	s.job.Emit(orchestration.JobStateReviewing, "the work is on this task's open pull request "+number+"; the gate judges it")
	return orchestration.JobStateReviewing, nil
}

// unpublishedBranchAhead reports whether this attempt's branch carries commits
// its base does not and no pull request has been opened for it — work that
// exists and that nobody has been asked to merge.
func (s *runnerRun) unpublishedBranchAhead(ctx context.Context) (bool, error) {
	if s.delivery == nil || !s.delivery.RequiresPullRequest || s.delivery.PullRequest.Opened() {
		return false, nil
	}
	if s.sessionID == "" || s.containerDir == "" || s.attempt == nil {
		return false, nil
	}
	return s.publisher(ctx).AheadOfBase(s.publishPlan())
}

// silentEscalation is how an attempt ends when the worker answered with
// nothing every time it was asked. It is an escalation rather than a
// question, because there is no question: nobody knows what happened, and
// the evidence is the silence itself.
//
// AND IT IS THE FACTORY'S FAULT, NOT THE WORK'S. MaxSilentTurns is one, so
// reaching here means the session answered with no text TWICE — which is the
// case that bound exists to distinguish: a transport that dropped a single
// answer is gone by the next prompt, and a worker whose process died answers
// with nothing again. Nothing judged the work either time, so charging the
// attempt would spend a task's budget on a backend that is not there.
var silentEscalation = TurnVerdict{
	Outcome:    TurnOutcomeSilent,
	Action:     TurnActionPark,
	BlockKind:  orchestration.TaskBlockKindEscalated,
	Condition:  UnblockOperatorRepair,
	FaultClass: orchestration.FaultWorkerEnvironment,
	Headline: "The worker answered with no text on every turn of this attempt and changed no " +
		"file, so nothing recorded why it stopped.",
}

// noChangeTurn is what happens when a turn ends with the checkout untouched:
// the verification commands are skipped and SAID to be skipped, and the run
// goes where the classification sends it.
//
// The verdict is handed in rather than recomputed, because the turn's own
// event is written from it first — one classification per turn, and the
// record and the routing cannot disagree about what it was.
func (s *runnerRun) noChangeTurn(ctx context.Context, turn AgentTurn, verdict TurnVerdict) (orchestration.JobState, error) {
	action, ok := turnActions[verdict.Action]
	if !ok {
		return "", &UnknownTurnActionError{Action: string(verdict.Action), Outcome: string(verdict.Outcome)}
	}
	state, err := action(s, ctx, verdict, turn)
	// THE SKIP IS RECORDED ONLY WHEN IT HAPPENED. A satisfied verdict answered
	// by the attempt's own unpublished branch goes to verification instead
	// (concludeTurn), and an audit saying the commands were not run would
	// then be false on a job whose next events are exactly those commands.
	if err == nil && state != orchestration.JobStateValidating {
		s.job.Audit(auditVerificationSkipped, "the worker changed no file in "+s.containerDir+
			", so the binding's verification commands were not run: there is nothing to build, "+
			"test or review. outcome="+string(verdict.Outcome)+" action="+string(verdict.Action))
	}
	return state, err
}

// retryTurn prompts the same session once more, telling it what just
// happened, and escalates when the bound is spent.
func (s *runnerRun) retryTurn(ctx context.Context, verdict TurnVerdict, turn AgentTurn) (orchestration.JobState, error) {
	if s.silentTurns >= MaxSilentTurns {
		return s.parkTurn(ctx, silentEscalation, turn)
	}
	s.silentTurns++
	s.turnNote = silentTurnNote(turn.Elapsed)
	s.job.Emit(orchestration.JobStateExecuting, "the worker answered with nothing and changed no file; "+
		"the turn is taken once more with that stated")
	return orchestration.JobStateExecuting, nil
}

// repromptTurn sends the SAME prompt once more, because the reply that came
// back did not answer it.
//
// IT COSTS THE TASK NOTHING, and that is the whole point. The round is the
// job's iteration and is untouched; the attempt is the engine's and is
// untouched; the delivery's LastTurn is not written, so the next attempt is
// not warned about a turn that judged nothing. Measured on go-quicbaas#62
// (2026-09-22): a worker holding a queued background-task notification
// answered THAT when the review round arrived — 451ms, one text block, no
// tool use — and the task lost its job, its pull-request adoption state, a
// review round and an attempt to a reply that never read the prompt.
//
// THE PROMPT IS REBUILT, NOT REPLAYED, and comes out the same but for one
// line: turnPrompt renders the contract and the round's findings from the run
// exactly as it did a moment ago, and turnNote adds the account of the reply
// that missed. Sending the identical bytes is the loop repeatedPrompt exists
// to refuse.
//
// THE SECOND CONSECUTIVE ONE IS CHARGED. A session that answers past two
// prompts running is not reading prompts at all, and on an untouched tree
// that is the ordinary claimed-work failure of the work.
func (s *runnerRun) repromptTurn(ctx context.Context, _ TurnVerdict, turn AgentTurn) (orchestration.JobState, error) {
	if s.nonAnswerTurns >= MaxNonAnswerTurns {
		return s.chargeTurn(ctx, turnVerdicts[TurnOutcomeClaimedWork], turn)
	}
	s.nonAnswerTurns++
	s.turnNote = nonAnswerTurnNote(turn.Elapsed)
	s.job.Audit(auditNonAnswerReprompted, "attempt="+strconv.Itoa(turn.Attempt)+
		" round="+strconv.Itoa(turn.Round)+
		" elapsed="+turn.Elapsed.Round(time.Millisecond).String()+
		" tool_uses="+strconv.Itoa(turn.ToolUses())+
		"; the reply did not address the prompt, so the same prompt is sent once more: "+
		"neither a new round nor a new attempt")
	s.job.Emit(orchestration.JobStateExecuting, "the worker's reply did not address the prompt; "+
		"the same prompt is sent once more with that stated")
	return orchestration.JobStateExecuting, nil
}

// parkTurn stops the task for a person, with the worker's own words as the
// reason, and remembers the turn so the next attempt is not handed the same
// prompt.
func (s *runnerRun) parkTurn(ctx context.Context, verdict TurnVerdict, turn AgentTurn) (orchestration.JobState, error) {
	if err := s.rememberNoChangeTurn(ctx, turn); err != nil {
		return "", err
	}
	reason, err := TurnParkReason(verdict, turn)
	if err != nil {
		return "", err
	}
	return s.park(verdict.BlockKind, reason, verdict.FaultClass), nil
}

// chargeTurn is the one no-change turn that is still a failure of the work: a
// worker that reported edits on a tree it did not touch.
func (s *runnerRun) chargeTurn(ctx context.Context, _ TurnVerdict, turn AgentTurn) (orchestration.JobState, error) {
	if err := s.rememberNoChangeTurn(ctx, turn); err != nil {
		return "", err
	}
	return "", &NoChangesError{TaskID: s.taskID(), WorkDir: s.containerDir, Reply: turn.Capped().Reply}
}

// rememberNoChangeTurn writes the turn onto the Delivery, where the NEXT
// attempt at this task reads it.
func (s *runnerRun) rememberNoChangeTurn(ctx context.Context, turn AgentTurn) error {
	if s.delivery == nil {
		return nil
	}
	// STORED CAPPED. The run classifies from the whole reply; the durable
	// record only has to be readable by the next attempt.
	recorded := turn.Capped()
	return s.runner.hooks.updateDelivery(ctx, s.job.Context(), s.delivery, func(d *Delivery) {
		d.LastTurn = &recorded
	})
}

// forgetNoChangeTurn clears a previous attempt's no-change turn once a turn
// has reached the checkout. A worker that changed a file is not something to
// warn the next one about.
func (s *runnerRun) forgetNoChangeTurn(ctx context.Context) error {
	if s.delivery == nil || s.delivery.LastTurn == nil {
		return nil
	}
	return s.runner.hooks.updateDelivery(ctx, s.job.Context(), s.delivery, func(d *Delivery) {
		d.LastTurn = nil
	})
}

// parkJudgement is how a run parks: with which kind, and attributed to whom.
//
// The two travel together everywhere — onto the engine, onto the audit trail,
// and back off it on a resume — because a park restored with its kind and
// without its attribution is a park that charges the work for a fault the run
// had already proved was the factory's.
type parkJudgement struct {
	// kind is the block kind the task is parked with.
	kind orchestration.TaskBlockKind

	// faultClass is the orchestration operator fault class when the run
	// proved the factory was at fault, empty for a park about the work.
	faultClass string
}

// park stops the run for a person: the kind, the attribution and the reason
// are recorded on the audit trail — the only copy that survives an
// interruption — and the run goes to escalating, which delivers them.
//
// faultClass is empty for a park about the WORK's content — a question, an
// owner's decision, a prompt that repeated — and names an operator fault class
// when the run proved the factory could not present the work at all. The
// engine refunds the attempt only in the second case.
func (s *runnerRun) park(kind orchestration.TaskBlockKind, reason, faultClass string) orchestration.JobState {
	s.parked = parkJudgement{kind: kind, faultClass: faultClass}
	s.job.Audit(auditParked, parkRecordHeader(s.parked)+"\n"+reason)
	s.job.Escalate(reason)
	// THE ENGINE IS TOLD HERE, AT THE MOMENT THE JUDGEMENT IS MADE, and not
	// in escalating where it used to be. Returning this state sends the run
	// through JobRun.SetState, which SNAPSHOTS AND PERSISTS `escalating`
	// before the escalating handler ever runs — so a park recorded there
	// left a durable record saying "escalating" with no park on it. A daemon
	// that died in that window handed the lease reaper a record it reads as
	// an undiagnosed death, and the task was requeued: go-quicbaas#787's
	// loop surviving a crash, one window later than the one the persisted
	// field already closed.
	s.job.Park(kind, reason, faultClass)
	return orchestration.JobStateEscalating
}

// roundFindings renders what the last round said, from the first of three
// sources that holds anything: the job's memory, which is what survives a
// resume; the feedback this run recorded; and the refusal the DELIVERY records
// against the head the pull request still carries.
//
// THE THIRD SOURCE IS THE ONE A READMITTED TASK HAS. Its job is fresh -- no
// iteration, no memory -- so both live sources are empty and the worker used
// to be handed the bare issue contract. It would then read a branch that
// already holds the work, correctly answer "the branch already carries the
// complete implementation", and be charged a failure for producing no changes,
// while the reviewer's actual objection was never delivered to anybody.
// Measured on go-truststrap task 61e3aa53 / PR #66, 2026-09-16.
//
// The round number comes from the refusal for the same reason: a fresh job
// numbers its first iteration zero, and a round rendered as "round 0" reads to
// the worker as the first attempt rather than as an answer to a review.
//
// It is empty on the first round, when there is genuinely nothing said yet.
func (s *runnerRun) roundFindings() string {
	round := s.job.Iteration()
	body := ""
	if memory := s.job.Memory(); memory != nil {
		body = strings.TrimSpace(memory.Serialize())
	}
	if body == "" {
		body = strings.TrimSpace(s.feedback)
	}
	if body == "" {
		if refusal := s.delivery.HeadRefusal(); refusal != nil {
			body = strings.TrimSpace(refusal.Findings)
			if round <= 0 {
				round = refusal.Round + 1
			}
		}
	}
	if round <= 0 || body == "" {
		return ""
	}
	return sdlcPrompts.MustRender("round_findings", prompts.Vars{
		"round":    strconv.Itoa(round),
		"findings": body,
	})
}

// =============================================================================
// 3. validating — the binding's verification commands
// =============================================================================

// validate runs the binding's verification commands, in order, through the
// module's container_command validator: the operator's ExecutionGuard checks
// each against its allowlist ON THE DAEMON, and a command it admits then runs
// INSIDE the worker's container, in the checkout the container's own
// entrypoint made. Nothing verifies on the daemon.
//
// A failure sends the work back to the worker with the output as feedback and
// counts as a round, so a project whose build never passes escalates instead
// of looping forever.
func (s *runnerRun) validate(ctx context.Context) (orchestration.JobState, error) {
	if s.attempt == nil {
		if err := s.resolve(ctx); err != nil {
			return "", err
		}
	}
	criteria := verificationCriteria(s.attempt.binding.Verification)
	if len(criteria) == 0 {
		s.job.Emit(orchestration.JobStateReviewing, "no verification commands configured; the forge's own checks are the gate")
		return orchestration.JobStateReviewing, nil
	}
	// WHAT THE GATE WILL JUDGE IS WHAT IS JUDGED HERE. A branch behind its
	// base is verified on the merge result, because that is the tree the
	// forge's checks run on; see merge_result.go.
	merged, err := s.mergeResultForVerification(ctx)
	if err != nil {
		return "", err
	}
	// A MERGE RESULT THAT COULD NOT BE THROWN AWAY IS SAID OUT LOUD. The
	// worktree lives in a checkout that outlives this job on a persistent
	// workspace tier, so one left behind is the next attempt's problem, and
	// an operator who cannot see it here has nowhere else to read it.
	defer func() {
		if err := merged.Release(); err != nil {
			s.job.Audit(auditVerification, "the merge result was not thrown away: "+err.Error())
		}
	}()
	results, passed, err := s.job.Verify(ctx, criteria)
	// EVERY COMMAND'S ANSWER IS ON THE RECORD, whether the pipeline went on
	// or not. The one line an operator greps for when an attempt dies at
	// verification is "what did make say", and until this was written the
	// job carried the turn, the checkout and the teardown but not that.
	// Measured on go-quicbaas PR #970 (2026-09-21): a failed `make build`
	// was misread as a vanished container, and nothing on the job said the
	// build had run at all.
	//
	// A FAILED COMMAND'S RECORD CARRIES ITS FAILING LINES, not its first
	// line: see verificationSummary. The header stays the first line, so a
	// grep for `passed=false` still finds it.
	var failed []orchestration.ValidationResult
	for _, result := range results {
		summary := verificationSummary(result)
		header := "command=" + result.Name + " passed=" + strconv.FormatBool(result.Passed)
		if result.ExitCode != 0 {
			header += " exit_code=" + strconv.Itoa(result.ExitCode)
		}
		header += " output_bytes=" + strconv.Itoa(len(result.Details))
		if result.Passed {
			s.job.Audit(auditVerification, header+" "+summary)
			continue
		}
		s.job.Audit(auditVerification, header+"\n"+summary)
		failed = append(failed, result)
	}
	if err != nil {
		return s.refusedVerification(err)
	}
	if passed {
		s.job.Emit(orchestration.JobStateReviewing, "verification passed: "+strconv.Itoa(len(results))+" command(s)")
		return orchestration.JobStateReviewing, nil
	}
	s.feedback = sdlcPrompts.MustRender("verification_failed", prompts.Vars{
		"failures": leadWith(merged.Note(), renderFailures(results)),
	})
	s.job.Memory().AddFeedback(s.feedback)
	return s.nextRoundWithEvidence(ctx, "verification failed", func(budget int) string {
		return verificationFailureEvidence(failed, budget)
	})
}

// verificationCriteria turns the binding's commands into criteria the
// module's container_command validator judges.
func verificationCriteria(commands []string) []orchestration.ReviewCriterion {
	criteria := make([]orchestration.ReviewCriterion, 0, len(commands))
	for _, command := range commands {
		trimmed := strings.TrimSpace(command)
		if trimmed == "" {
			continue
		}
		criteria = append(criteria, orchestration.ReviewCriterion{
			Name:      trimmed,
			Validator: ValidatorKindContainerCommand,
			Config:    trimmed,
			Required:  true,
			Phase:     orchestration.CriterionPhaseValidating,
		})
	}
	return criteria
}

// mergeResultForVerification prepares the tree the binding's verification
// commands run on, and points the container at it.
//
// THE CONTAINER BINDING IS WHAT DECIDES WHERE A COMMAND RUNS. JobContainer
// pins the working directory precisely so a validator cannot choose one, so
// the only honest way to verify a different tree is to say, here, that this
// is the tree the job's work is being judged in — and to put the binding back
// the moment verification is done, which Release does.
//
// A RUN WITH NOTHING TO PREPARE PREPARES NOTHING. Without a delivery, a live
// session or a checkout there is no branch to count and no container to count
// it in; verification then answers for itself, exactly as it did before.
func (s *runnerRun) mergeResultForVerification(ctx context.Context) (*mergeResult, error) {
	if s.delivery == nil || s.sessionID == "" || s.containerDir == "" {
		return &mergeResult{}, nil
	}
	merged, err := s.publisher(ctx).MergeResult(s.publishPlan())
	if err != nil {
		return nil, err
	}
	if merged.Behind == 0 {
		return merged, nil
	}
	// WHAT WAS JUDGED IS ON THE RECORD, whichever tree it was. A verification
	// that quietly judged something other than the branch as pushed would be
	// unreadable from the job afterwards, and a merge result that could NOT
	// be prepared must not look like one that was.
	s.job.Audit(auditVerification, "behind="+strconv.Itoa(merged.Behind)+" base="+merged.Base+
		" judged="+mergeResultJudged(merged)+" uncommitted="+strconv.FormatBool(merged.Uncommitted))
	// MergeResult answers a worktree or an error for every branch that is
	// behind, so this is the invariant that makes binding the container to
	// the directory below safe to read here rather than a case to handle.
	if merged.WorkDir == "" {
		return merged, nil
	}
	if err := s.job.SetContainer(s.sessionID, merged.WorkDir); err != nil {
		// The worktree was prepared and the job never ran in it, so it goes
		// away here — and if it will not, that is said alongside the binding
		// failure rather than instead of it.
		return nil, withCleanup(err, merged.Release())
	}
	removeWorktree := merged.release
	merged.release = func() error {
		// The binding goes back FIRST: everything after verification — the
		// commit, the push, the teardown — belongs to the real checkout, and
		// a failure to remove a throwaway worktree must not leave the job
		// pointing at it.
		rebound := s.job.SetContainer(s.sessionID, s.containerDir)
		if rebound != nil {
			s.job.Audit(auditVerification, "the container binding could not be put back: "+rebound.Error())
		}
		var removed error
		if removeWorktree != nil {
			removed = removeWorktree()
		}
		// BOTH FAILURES OR NEITHER. Each one leaves the attempt somewhere it
		// should not be — bound to a tree that is about to vanish, or with a
		// worktree the next attempt will find — and dropping either to report
		// the other is how one of them goes unnoticed.
		return errors.Join(rebound, removed)
	}
	return merged, nil
}

// mergeResultJudged names the tree verification ran on, for the audit trail.
// "branch_tree" is what a merge result that was never prepared would read as,
// which must never be indistinguishable from one that was.
func mergeResultJudged(merged *mergeResult) string {
	if merged.WorkDir != "" {
		return "merge_result"
	}
	return "branch_tree"
}

// leadWith puts a lead line in front of a body, and is the identity on either
// one being empty. It is what keeps the "behind by N" line ahead of the
// evidence without an empty paragraph when there is no lead.
func leadWith(lead, body string) string {
	lead, body = strings.TrimSpace(lead), strings.TrimSpace(body)
	switch {
	case lead == "":
		return body
	case body == "":
		return lead
	}
	return lead + "\n\n" + body
}

// renderFailures names every criterion that failed and what it said.
func renderFailures(results []orchestration.ValidationResult) string {
	var b strings.Builder
	for _, result := range results {
		if result.Passed {
			continue
		}
		b.WriteString("### " + result.Name + "\n\n")
		if message := strings.TrimSpace(result.Message); message != "" {
			b.WriteString(message + "\n\n")
		}
		if details := strings.TrimSpace(result.Details); details != "" {
			b.WriteString(details + "\n\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// =============================================================================
// 4. reviewing — publish, then the forge review gate
// =============================================================================

// review publishes the work and asks the forge for its verdict.
//
// The two halves are done by the two things that can do them. THE CONTAINER
// commits, signs and pushes — it is where the checkout is, where the signing
// socket is and where the forge token is. THE DAEMON then opens or refreshes
// the pull request through the forge API and records the head and the pull
// request on the Delivery (Reviewable). A plan that requires no pull request
// — the trunk-based carve-out for a change class the project exempted —
// pushed straight onto the target and has nothing to review.
func (s *runnerRun) review(ctx context.Context) (orchestration.JobState, error) {
	diverted, err := s.publishForReview(ctx)
	if err != nil {
		return "", err
	}
	if diverted != "" {
		return diverted, nil
	}
	// THE WORK IS ON THE FORGE, so the worker goes before the wait does.
	if err := s.releaseWorker(ctx); err != nil {
		return "", err
	}
	if err := s.job.RunStage(ctx, orchestration.JobStageReviewable, func(hookCtx context.Context) error {
		return s.runner.hooks.Reviewable(hookCtx, s.job.Context())
	}); err != nil {
		return "", err
	}
	if err := s.loadDelivery(ctx); err != nil {
		return "", err
	}
	if !s.delivery.RequiresPullRequest {
		s.job.Emit(orchestration.JobStateCompleting, "carve-out: pushed to "+s.delivery.Target+" without a pull request")
		return orchestration.JobStateCompleting, nil
	}
	// THROUGH Verify, not the registry: Verify is the only path that records
	// what the validator said on the job, and the review gate's refusal is
	// exactly the evidence a reader needs for a redelegation. Calling the
	// registry directly ran the gate and threw its answer away, leaving the
	// job page's verification panel empty on every send-back.
	// The gate re-runs a check the runner broke (go-quicbaas#1011) without
	// returning, so it is handed this job's audit trail to say so on.
	results, passed, err := s.job.Verify(withCIRerunAudit(ctx, s.job.Audit), []orchestration.ReviewCriterion{{
		Name:      ValidatorKindForgeReview,
		Validator: ValidatorKindForgeReview,
		Required:  true,
		Phase:     orchestration.CriterionPhaseReviewing,
	}})
	if err != nil {
		// A RED THAT NO CHANGE COULD FIX PARKS; IT DOES NOT FAIL THE RUN AND
		// IT DOES NOT GO BACK TO THE WORKER. The gate has classified the
		// pipeline's failure as the machine's — an unreachable registry, a
		// container the runtime refuses, a module-resolution environment that
		// was never provisioned — and the park posts that account to the forge
		// issue a person is watching (go-quicbaas#898). Re-delegating instead
		// is what cost 1.5M tokens on 2026-09-12; failing the run instead
		// would lose the account.
		var defect *CIHostDefectError
		if errors.As(err, &defect) {
			return s.park(orchestration.TaskBlockKindEscalated, defect.ParkReason(), orchestration.FaultCIHostDefect), nil
		}
		// A CLOSED, UNMERGED PULL REQUEST ENDS THE DELIVERY. It is neither
		// a refusal to address in another round nor a question for the
		// owner (go-quicbaas#1054).
		var closed *PullRequestClosedUnmergedError
		if errors.As(err, &closed) {
			if err := s.republishDelivery(ctx); err != nil {
				return "", err
			}
			return s.supersede(closed.Number), nil
		}
		return "", err
	}
	// THE VERDICT IS ONLY READABLE ONCE IT IS AN ARTIFACT. Reviewable
	// published this attempt's delivery BEFORE the gate ran, so the round the
	// gate just recorded on the Delivery is not in `reviews@aN` yet, and the
	// Delivery is this module's own — the job page reads the artifact and
	// nothing else. An approval is republished by Complete, but a refusal
	// returned straight to another round, so the LAST round of an attempt
	// that then escalates or exhausts its budget left the page with no
	// findings at all: exactly the reader go-quicbaas#796 exists for.
	// Republishing here, before the branch, makes the artifact say what the
	// gate decided on every path out of it.
	if err := s.republishDelivery(ctx); err != nil {
		return "", err
	}
	if passed {
		// A TRAINED PROJECT QUEUES THE APPROVED HEAD instead of merging it,
		// and this job waits here, in reviewing, for the train's answer. See
		// merge_train_delivery.go.
		// AN UNTRAINED PROJECT SAYS WHY, so a person reading a change that
		// merged on its own is not left to guess whether the train was
		// skipped or broken.
		if err := s.resolve(ctx); err != nil {
			return "", err
		}
		reason := s.attempt.binding.TrainOffReason()
		if reason == "" {
			return s.rideTrain(ctx)
		}
		s.job.Audit(auditTrainOff, reason)
		s.job.Emit(orchestration.JobStateCompleting, "review approved; it merges on its own, not by a merge train: "+reason)
		return orchestration.JobStateCompleting, nil
	}
	s.feedback = sdlcPrompts.MustRender("review_findings", prompts.Vars{
		"failures": renderFailures(results),
	})
	s.job.Memory().AddFeedback(s.feedback)
	return orchestration.JobStateRedelegating, nil
}

// supersede ends the attempt because its pull request was closed without
// merging (go-quicbaas#1054).
//
// Like concludeTurn, it completes the task. It runs no further worker round
// and asks the owner nothing. The judgement goes
// on the run so the persisted status carries it, and applyConclusion reads it
// there as `superseded`.
func (s *runnerRun) supersede(number int) orchestration.JobState {
	reason := "pull request #" + strconv.Itoa(number) + " was closed without merging; the attempt ends as superseded"
	s.job.Audit(auditSuperseded, reason)
	s.job.Emit(orchestration.JobStateCompleted, reason)
	s.job.Supersede()
	return orchestration.JobStateCompleted
}

// republishDelivery re-reads the delivery the review gate wrote and publishes
// it as this attempt's task artifacts. The re-read is the point: the gate
// records through its own copy, so the run's is stale the moment it returns.
func (s *runnerRun) republishDelivery(ctx context.Context) error {
	if err := s.loadDelivery(ctx); err != nil {
		return err
	}
	return s.runner.hooks.publish(ctx, s.job.Context(), s.delivery)
}

// publishForReview runs the container's half of the delivery and answers the
// empty state to say the forge half may follow. It answers a state instead
// when the run cannot publish and must go somewhere else first.
//
// A RESUMED RUN HAS NO CONTAINER to publish from. What decides is the
// Delivery: a recorded head is the push that already happened, and the forge
// half — open or refresh the pull request, ask for the verdict — is the
// daemon's own and runs unchanged. No head means the push never happened and
// the worker's edits went with the container, so the attempt is executed
// again from initializing.
func (s *runnerRun) publishForReview(ctx context.Context) (orchestration.JobState, error) {
	if s.sessionID != "" {
		if err := s.publishWork(ctx); err != nil {
			return s.refusedPush(err)
		}
		return "", nil
	}
	if err := s.loadDelivery(ctx); err != nil {
		return "", err
	}
	if s.delivery.HeadSHA == "" {
		s.job.Emit(orchestration.JobStateInitializing,
			"resumed in reviewing before the work reached the forge; the attempt is executed again")
		return orchestration.JobStateInitializing, nil
	}
	s.job.Emit(orchestration.JobStateReviewing,
		"resumed on the published head "+s.delivery.HeadSHA+"; the worker published before the interruption")
	return "", nil
}

// =============================================================================
// 5. re-delegating — another round, or a person
// =============================================================================

// redelegate starts the next round, or escalates when the binding's bound is
// spent.
func (s *runnerRun) redelegate(ctx context.Context) (orchestration.JobState, error) {
	return s.nextRound(ctx, "changes requested")
}

// nextRound advances the review round and answers where the job goes: back to
// the worker while rounds remain, to a person when they do not.
func (s *runnerRun) nextRound(ctx context.Context, reason string) (orchestration.JobState, error) {
	return s.nextRoundWithEvidence(ctx, reason, nil)
}

// nextRoundWithEvidence is nextRound for a round whose cause has evidence a
// person needs if the round budget is spent: a failed verification's failing
// lines. The evidence is appended to the escalation, never to the round's
// event, which stays one line.
//
// THE EVIDENCE IS RENDERED TO THE ROOM THE PUBLICATION LEAVES IT. The forge
// comment keeps only the first publicOutputLimit bytes of the reason, so
// evidence rendered without knowing the sentence before it could be cut
// before the lines it exists to show.
func (s *runnerRun) nextRoundWithEvidence(ctx context.Context, reason string, evidence func(budget int) string) (orchestration.JobState, error) {
	if s.attempt == nil {
		if err := s.resolve(ctx); err != nil {
			return "", err
		}
	}
	round := s.job.NextIteration()
	// The project's bound PLUS whatever an operator granted this one task
	// from the attention inbox after an earlier escalation
	// (Task.ReviewRoundBonus, raised by BumpTaskReviewRoundBudget). The bonus
	// is additive and per-task, never per-project: granting one task another
	// round must not quietly raise the ceiling for every other task the
	// binding drives.
	max := s.attempt.binding.ReviewRounds() + s.attempt.task.ReviewRoundBonus
	if round > max {
		// THE PARK CARRIES A TYPED CONDITION, and the sentence a person
		// reads is derived from it. Review that did not converge is a call
		// the owner makes — the SDLC convention reserves it — so the
		// condition says so explicitly, where prose used to leave "waiting
		// on the owner" and "waiting on nothing" looking identical. The
		// sweep reads the same marker and reports who is being waited on
		// instead of passing the task over in silence.
		detail := reason + " after " + strconv.Itoa(max) + " review round(s); this change needs a person"
		if evidence != nil {
			detail += escalationEvidence(evidence(publicOutputLimit - len(detail) - len("\n\n")))
		}
		parked, err := ComposeBlockedReason(UnblockCondition{Kind: UnblockOwnerRuling}, detail)
		if err != nil {
			return "", err
		}
		// THROUGH THE SAME DURABLE HELPER AS EVERY OTHER PARK, and this is
		// the path that actually ends attempts in production: three review
		// rounds is a common ending, a classified worker stop is not.
		//
		// It used to write its own audit event and Escalate reason and then
		// return the state, which is precisely the shape the crash window
		// exploits — SetState persists `escalating` before the handler for
		// it runs, so the reaper found a record with no park on it and
		// accounted an exhausted review as an undiagnosed death. There is
		// one park helper now and every park goes through it.
		return s.park(orchestration.TaskBlockKindEscalated, parked, ""), nil
	}
	// A ROUND HAS ASKED FOR NEW WORK, and resumeReview must not undo that.
	// Executing needs a container a resumed run does not hold, so it returns
	// through Initializing to get one — and Initializing is where resumeReview
	// lives. Without this flag it would see the pull request still open, send
	// the job straight back to Reviewing without running the worker, and the
	// gate would reject the same unchanged head until the round budget was
	// spent: a review that never converges because nobody ever changed
	// anything.
	s.roundRequested = true
	s.job.Emit(orchestration.JobStateExecuting, reason+": round "+strconv.Itoa(round)+" of "+strconv.Itoa(max))
	return orchestration.JobStateExecuting, nil
}

// escalationEvidence renders a round's evidence for the escalation that
// follows the reason, or nothing when there is none.
//
// COMMAND OUTPUT IS NOT TRUSTED TO BE PROSE. An `unblock[` in it would be
// refused by ComposeBlockedReason as an injected condition, and the park
// would fail on the very output it exists to report; the marker is defused
// instead, because the output is what a person must read. A verification
// summary arrives already defused (verificationSummaryLines), so its
// measured length holds; this is the guard for any other evidence.
func escalationEvidence(evidence string) string {
	trimmed := strings.TrimSpace(evidence)
	if trimmed == "" {
		return ""
	}
	return "\n\n" + defuseUnblockMarker(trimmed)
}

// verificationVerdict says how a failed command failed: the exit status it
// ran to, or plainly "failed" for one refused before it ran, which has none.
func verificationVerdict(result orchestration.ValidationResult) string {
	if result.ExitCode != 0 {
		return "exited " + strconv.Itoa(result.ExitCode)
	}
	return "failed"
}

// verificationFailureEvidence renders each failed command's failing lines in
// at most budget bytes, the room split evenly between the commands so one
// long failure cannot crowd another out of the escalation.
func verificationFailureEvidence(failed []orchestration.ValidationResult, budget int) string {
	if len(failed) == 0 || budget <= 0 {
		return ""
	}
	const separator = "\n\n"
	share := (budget - len(separator)*(len(failed)-1)) / len(failed)
	blocks := make([]string, 0, len(failed))
	for _, result := range failed {
		head := "`" + result.Name + "` " + verificationVerdict(result) + ":\n```\n"
		const tail = "\n```"
		room := share - len(head) - len(tail)
		if room <= 0 {
			continue
		}
		excerpt := verificationExcerpt(result, room)
		if excerpt == "" {
			continue
		}
		blocks = append(blocks, head+excerpt+tail)
	}
	return strings.Join(blocks, separator)
}

// =============================================================================
// 6. completing — merge, then what the strategy says follows
// =============================================================================

// complete merges the pull request bound to the reviewed head and runs the
// strategy's post-merge steps.
//
// It is the state a job interrupted here resumes at, so everything in it is
// idempotent: Complete records an already-merged pull request rather than
// merging it twice, and a back-merge whose pull request already exists is
// refreshed rather than duplicated.
func (s *runnerRun) complete(ctx context.Context) (orchestration.JobState, error) {
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	if err := s.job.RunStage(ctx, orchestration.JobStageComplete, func(hookCtx context.Context) error {
		return s.runner.hooks.Complete(hookCtx, s.job.Context())
	}); err != nil {
		var conflict *MergeConflictError
		if errors.As(err, &conflict) {
			return s.mergeRound(ctx, conflict)
		}
		return "", err
	}
	if err := s.loadDelivery(ctx); err != nil {
		return "", err
	}
	if err := s.runPostMerge(ctx); err != nil {
		return "", err
	}
	return orchestration.JobStateCleanup, nil
}

// mergeRound sends an approved pull request that conflicts with its base
// back to the worker, to merge the latest base into the same branch.
//
// IT IS A ROUND OF THE WORK, not a failure of it and not the environment's.
// The base moved under an accepted change, and only the worker's checkout can
// bring the change up to date with it. Before this, the conflict came back
// from Complete as a hook error. The attempt was charged as a failure of the
// work, and every re-dispatch repeated the same merge until the task blocked
// (go-truststrap#64, 2026-09-22).
//
// A MERGE, NEVER A REBASE. The owner's ruling (2026-09-04, handbook
// version-control §5b) is absolute: a pushed branch is never rewritten and
// never force-pushed. Merging the base keeps the published head as an
// ancestor of the new one, so the publication that follows is the same plain
// fast-forward push every other round makes. The runner prepares nothing on
// the branch itself; the worker runs the merge (merge_conflict).
//
// THE SAME PATH AS EVERY OTHER ROUND. The findings go into job memory the way
// verification output and review findings do, and nextRound spends a round from
// the same budget. So a pull request that conflicts again every time the base
// is merged still escalates to a person. The merged head then goes through the
// ordinary pipeline, where validation runs, the new head is published and the
// review gate judges it again, because an approval covers only the head it was
// given.
func (s *runnerRun) mergeRound(ctx context.Context, conflict *MergeConflictError) (orchestration.JobState, error) {
	s.job.Audit(auditMergeConflict, "base="+conflict.Base+" head="+conflict.Head+
		" attempts="+strconv.Itoa(conflict.Attempts))
	s.feedback = mergeConflictFindings(conflict)
	s.job.Memory().AddFeedback(s.feedback)
	return s.nextRound(ctx, "merge conflict with "+conflict.Base)
}

// mergeConflictFindings is what the worker is told about a pull request that
// conflicts with its base.
func mergeConflictFindings(conflict *MergeConflictError) string {
	return sdlcPrompts.MustRender("merge_conflict", prompts.Vars{
		"base": conflict.Base,
		"head": conflict.Head,
	})
}

// runPostMerge executes what the strategy says follows the merge. The steps
// are VALUES the strategy declared and this is the only thing that executes
// them, through the forge adapter — a strategy never holds a forge client.
func (s *runnerRun) runPostMerge(ctx context.Context) error {
	if !s.delivery.RequiresPullRequest {
		return nil
	}
	strategy, err := LookupStrategy(s.attempt.binding.Strategy)
	if err != nil {
		return err
	}
	issue, err := s.runner.hooks.loadIssue(ctx, s.attempt)
	if err != nil {
		return err
	}
	steps, err := strategy.PostMerge(MergedRef{
		Number:       s.delivery.PullRequest.Number,
		Title:        s.delivery.PullRequest.Title,
		IssueNumber:  s.attempt.issue,
		SourceBranch: s.delivery.Branch,
		TargetBranch: s.delivery.Target,
		Labels:       issue.Labels,
	})
	if err != nil {
		return err
	}
	for _, step := range steps {
		if err := s.runStep(ctx, step); err != nil {
			return err
		}
	}
	return nil
}

// runStep performs one post-merge step through the forge.
func (s *runnerRun) runStep(ctx context.Context, step Step) error {
	if step.Kind != StepBackMerge {
		return &UnknownPostMergeStepError{Kind: string(step.Kind)}
	}
	pr, err := s.attempt.forge.CreatePullRequest(ctx, s.attempt.repo, PullRequestSpec{
		Title:   step.Title,
		Body:    step.Body,
		HeadRef: step.Source,
		BaseRef: step.Target,
	})
	if err != nil {
		return err
	}
	if pr == nil {
		return &ForgeWriteNotAppliedError{
			Type:      s.attempt.forge.Type(),
			Operation: "CreatePullRequest",
			Subject:   s.attempt.repo.String() + " " + step.Source + " -> " + step.Target,
		}
	}
	if err := s.attempt.forge.MergePullRequest(ctx, s.attempt.repo, pr.Number, MergeSpec{
		Method:      MergeMethodMerge,
		CommitTitle: step.Title,
	}, pr.HeadSHA); err != nil {
		return err
	}
	s.job.Audit("sdlc:post_merge_step", "kind="+string(step.Kind)+" source="+step.Source+" target="+step.Target+
		" pull_request="+strconv.Itoa(pr.Number))
	return nil
}

// =============================================================================
// 7. cleanup and escalating — nothing is lost
// =============================================================================

// cleanup ends a job that delivered: the branch is published so nothing the
// worker did is lost, the session is closed and its container goes with it,
// and Discard records the end of the attempt.
func (s *runnerRun) cleanup(ctx context.Context) (orchestration.JobState, error) {
	if err := s.teardownWorker(ctx, true); err != nil {
		return "", err
	}
	return orchestration.JobStateCompleted, nil
}

// auditEscalated is the audit event an escalation used to be recorded on.
//
// NOTHING WRITES IT ANY MORE — every park, the exhausted review included,
// goes through runnerRun.park and records auditParked, which carries the
// block kind as well as the reason. It is still READ, because a job record
// written before that unification carries only this event and a resumed run
// must still be able to restore from it. See restorePark.
const auditEscalated = "sdlc:escalated"

// auditParkPosted records that a resumed escalation found its park reason
// already on the issue and posted nothing.
const auditParkPosted = "sdlc:park_already_posted"

// auditParked is the audit event a run records when it parks its task for a
// reason the WORKER stated. It carries the block kind on its first line and
// the reason after it, because a park that came back from a resume with the
// kind lost would be delivered as an escalation whatever the worker actually
// said.
//
// auditEscalated stays exactly what it was — the engine's own "this needs a
// person" — so a record written before workers could state a reason still
// restores.
const auditParked = "sdlc:parked"

// auditAgentTurn is the job event carrying ONE worker turn: how long it took,
// what its reply carried, and the reply itself.
const auditAgentTurn = "sdlc:agent_turn"

// auditNonAnswerReprompted records a reply that did not address its prompt,
// and the re-prompt it earned.
//
// It is the ONLY trace of one: the re-prompt is neither a round nor an
// attempt, so without this event nothing in the record would say that a turn
// happened and was deliberately not counted.
const auditNonAnswerReprompted = "sdlc:non_answer_reprompted"

// auditMergeConflict records that an approved pull request conflicted with its
// base and was sent back to the worker as a merge round.
const auditMergeConflict = "sdlc:merge_conflict"

// auditVerificationSkipped records that the binding's verification commands
// were not run because the worker changed no file.
const auditVerificationSkipped = "sdlc:verification_skipped"

// auditSatisfiedOnBranch records a satisfied verdict that was answered by
// the attempt's own unpublished branch rather than by the base, and was
// therefore verified and published instead of concluded.
const auditSatisfiedOnBranch = "sdlc:satisfied_on_branch"

// auditSatisfiedOnPullRequest records a satisfied verdict answered by the
// task's own pull request, which was therefore reviewed, completed or
// superseded instead of concluded.
const auditSatisfiedOnPullRequest = "sdlc:satisfied_on_pull_request"

// auditSuperseded records an attempt that ended because its pull request was
// closed without merging (go-quicbaas#1054).
const auditSuperseded = "sdlc:superseded"

// auditVerification records one verification command's answer: its name,
// whether it passed, how much output it produced, and what it said: the first
// line for a passing command, the failing lines for a failed one (see
// verificationSummary).
const auditVerification = "sdlc:verification"

// auditSessionRolled records that a worker session was replaced by a fresh
// one on the same branch, within the same attempt: because its context was
// full, because the provider refused its pooled account for a limit
// (class=account_limited), or because its pooled account's credential was
// rotated under it (reason=account_rotated), on a project that starts fresh or
// whose conversation could not be read (resume_unavailable=).
const auditSessionRolled = "sdlc:session_rolled"

// auditSessionResumed records that a worker session was replaced by one that
// CONTINUES its conversation, on the same branch, within the same attempt and
// the same round, because its pooled account's credential was rotated
// (reason=account_rotated) or the provider refused its account for a limit
// (class=account_limited), and the project resumes rather than starts fresh
// (Project.SessionRotation).
const auditSessionResumed = "sdlc:session_resumed"

// auditWorkerReleased records that a worker's session and container were
// closed on entry to reviewing, once its work was published (go-quicbaas#938),
// and what a session respawned for a review round will continue:
// replayed_turns= on a project that resumes, resume_unavailable= when its
// conversation could not be read, neither on a project that starts fresh.
const auditWorkerReleased = "sdlc:worker_released"

// auditReasonAccountRotated is the `reason=` a replacement records when the
// pool rotated the session's credential under it. One word, stable across
// releases, because an operator greps the audit trail for it.
const auditReasonAccountRotated = "account_rotated"

// escalate parks the task for a person: the reason is posted to the issue and
// the issue is labelled for it, the branch is published and the container is
// removed.
//
// IT IS ALSO WHERE AN INTERRUPTED ESCALATION FINISHES. A job whose daemon
// died between entering escalating and Blocked completing resumes here rather
// than at initializing: the escalation is not work to redo, it is work to
// conclude. The park reason is restored from the audit trail, posted only if
// the issue does not already carry it, and the state ends failed either way.
func (s *runnerRun) escalate(ctx context.Context) (orchestration.JobState, error) {
	s.restorePark()
	// A RESUMED RUN HOLDS NO PARK: the JobRun is fresh and the judgement was
	// made by the run that died, so it is re-armed here from what
	// restorePark recovered. A run that parked in this process already told
	// the engine in park(), before the escalating state was persisted.
	s.job.Park(s.blockKind(), s.job.EscalationReason(), s.parked.faultClass)
	if err := s.surfacePark(ctx); err != nil {
		return "", err
	}
	// THE PARK HAS NOW REACHED A PERSON, and only now is it recorded as
	// having done so. Everything above this line can fail, and when it does
	// the conclusion must post the outcome itself rather than assume this
	// run did — see parkConclusion.
	s.job.ParkSurfaced()
	if err := s.teardownWorker(ctx, false); err != nil {
		return "", err
	}
	return orchestration.JobStateFailed, nil
}

// blockKind is the kind this run parks its task with: the one the worker's
// own declaration earned, or escalation for a park the engine decided.
func (s *runnerRun) blockKind() orchestration.TaskBlockKind {
	if s.parked.kind != "" {
		return s.parked.kind
	}
	return orchestration.TaskBlockKindEscalated
}

// restorePark reinstates WHY and WITH WHICH KIND the task was parked when
// this run is the resumption of the run that decided it. Without it the
// Blocked comment and the EscalatedError would both say the module recorded
// no reason.
//
// The newest record wins and both vocabularies are read: auditParked carries
// a kind the worker's declaration earned and the attribution the run proved,
// auditEscalated is the engine's own escalation and is all a record written
// before this existed can carry.
//
// THE ATTRIBUTION MUST SURVIVE THE RESUME. A run that proved its workspace was
// unfit, and whose daemon then died in escalating, resumes here; without the
// class restored the conclusion charges the attempt it had already earned back,
// so the crash — not the fault — is what spends the budget.
func (s *runnerRun) restorePark() {
	if s.job.Resumed == nil || s.job.EscalationReason() != "" {
		return
	}
	events := s.job.Resumed.Events
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case auditParked:
			judgement, reason := splitParkRecord(events[i].Details)
			s.parked = judgement
			s.job.Escalate(reason)
			return
		case auditEscalated:
			s.job.Escalate(events[i].Details)
			return
		}
	}
}

// parkRecordHeader is the first line of an auditParked event: the block kind,
// and the operator fault class after it when the run named one.
//
// A record with no class is exactly the one-token header every record written
// before attribution existed carries, which is what makes splitParkRecord able
// to read both without a version marker.
func parkRecordHeader(judgement parkJudgement) string {
	if judgement.faultClass == "" {
		return string(judgement.kind)
	}
	return string(judgement.kind) + " " + judgement.faultClass
}

// splitParkRecord reads the judgement and the reason out of an auditParked
// event.
//
// A record whose first line is not a kind the engine knows is restored as an
// escalation with the whole record as its reason: the reason is what a person
// must read, and losing it to a malformed header would be the worse failure.
//
// A SECOND TOKEN THAT IS NOT AN OPERATOR FAULT CLASS IS DROPPED, not treated
// as one. The class decides whether an attempt is refunded, and the only class
// worth believing is one this binary produces — JobRun.Park drops it a second
// time for the same reason.
func splitParkRecord(details string) (parkJudgement, string) {
	head, reason, found := strings.Cut(details, "\n")
	fields := strings.Fields(head)
	if !found || len(fields) == 0 {
		return parkJudgement{kind: orchestration.TaskBlockKindEscalated}, details
	}
	kind := orchestration.TaskBlockKind(fields[0])
	if kind.Validate() != nil {
		return parkJudgement{kind: orchestration.TaskBlockKindEscalated}, details
	}
	judgement := parkJudgement{kind: kind}
	if len(fields) > 1 && orchestration.OperatorFaultClass(fields[1]) {
		judgement.faultClass = fields[1]
	}
	return judgement, reason
}

// surfacePark posts the park reason through the Blocked hook, EXACTLY ONCE
// across an interruption.
func (s *runnerRun) surfacePark(ctx context.Context) error {
	posted, err := s.parkPosted(ctx)
	if err != nil {
		return err
	}
	if posted {
		s.job.Audit(auditParkPosted, "task="+s.taskID()+" attempt="+strconv.Itoa(s.attemptNumber()))
		// THE COMMENT is what must not be doubled. The label writes are
		// idempotent and the interruption may have landed between them, so
		// they are re-run rather than assumed — including the pull request's,
		// which is the one a person reading the change sees.
		delivery, err := s.runner.hooks.deliveryOrNil(ctx, s.job.Context())
		if err != nil {
			return err
		}
		return s.runner.hooks.markParked(ctx, s.attempt, delivery, blockedLabelKey(s.blockKind()))
	}
	return s.job.RunStage(ctx, orchestration.JobStageBlocked, func(hookCtx context.Context) error {
		return s.runner.hooks.Blocked(hookCtx, s.job.Context(), orchestration.BlockReason{
			Kind:    s.blockKind(),
			Message: s.job.EscalationReason(),
			// THE CAUSE TRAVELS WITH THE COMMENT. The class reaches the task
			// record only when the attempt is concluded, which is after this
			// posts, so a comment that waited for the record would name no
			// cause on exactly the parks whose cause was just proved.
			FaultClass: s.parked.faultClass,
			Iterations: s.job.Iteration(),
		})
	})
}

// parkPosted reports whether THIS attempt's park reason is already on the
// issue.
//
// The comment is the escalation's one visible effect and the only record of
// it that survives the daemon, so the issue is what a resumed run asks. The
// marker names the task, the attempt and the kind together: an earlier
// attempt's park is a different park, and a task readmitted after one must
// still be able to escalate again.
//
// A fresh run answers false without a forge round trip. It cannot have posted
// anything yet, and a park it finds on the issue belongs to another attempt.
func (s *runnerRun) parkPosted(ctx context.Context) (bool, error) {
	if s.job.Resumed == nil {
		return false, nil
	}
	if s.attempt == nil {
		if err := s.resolve(ctx); err != nil {
			return false, err
		}
	}
	comments, err := s.attempt.forge.ListIssueComments(ctx, s.attempt.repo, s.attempt.issue)
	if err != nil {
		return false, err
	}
	marker := blockedMarker(s.taskID(), s.attemptNumber(), blockedOutcome(s.blockKind()))
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			return true, nil
		}
	}
	return false, nil
}

// attemptNumber is the task attempt this run is, zero for a job bound to none.
func (s *runnerRun) attemptNumber() int {
	if s.job.Config == nil {
		return 0
	}
	return s.job.Config.Attempt
}

// teardownWorker publishes whatever the worker has not published yet, then
// closes the session and runs Discard. It runs AT MOST ONCE per run.
//
// THE PUSH COMES FIRST, on every non-completed exit. The container is the only
// thing that made a checkout, so once it is gone an unpushed commit is gone
// with it; publishing before teardown is what makes an escalation recoverable
// by a person rather than a restart from nothing.
//
// THE CLOSE OUTLIVES THE RUN'S CANCELLATION. A cancelled attempt is exactly
// the case where the container must still go, so the close is issued on a
// context the cancellation cannot reach — bounded by the operator's own
// discard bound, because a daemon that will not answer must not hold the
// runner's exit open forever. It is the close that reaches whichever daemon
// owns the session: JobRun.Close goes to the agent service, which pins the
// backend that holds the live record, so a session spawned on a remote daemon
// is closed by a FleetRuntime.Close RPC to that daemon and not against this
// host's container runtime.
func (s *runnerRun) teardownWorker(ctx context.Context, completed bool) error {
	if s.tornDown {
		return nil
	}
	s.tornDown = true
	if !completed {
		s.publishBranch(ctx)
	}
	s.closeWorker(ctx)
	return s.job.RunStage(context.WithoutCancel(ctx), orchestration.JobStageDiscard, func(hookCtx context.Context) error {
		return s.runner.hooks.Discard(hookCtx, s.job.Context(), completed)
	})
}

// closeWorker closes the worker's session — and its container with it — and
// releases the signing agent and the workspace spec the spawn acquired. It is
// the one close every path shares: a replaced session, a worker released for
// reviewing, and the teardown. Safe when no session is held.
//
// THE CLOSE OUTLIVES THE RUN'S CANCELLATION, bounded by the operator's discard
// bound: see teardownWorker. The workspace itself is not touched: it is keyed
// per attempt (workspaceKey), so the next spawn continues the same clone.
func (s *runnerRun) closeWorker(ctx context.Context) {
	if s.sessionID != "" {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx),
			s.job.Policy().StageTimeouts.For(orchestration.JobStageDiscard))
		s.job.Close(closeCtx, s.sessionID)
		cancel()
		s.sessionID = ""
	}
	s.workspace = nil
	s.release()
}

// releaseWorker closes the worker for the wait in reviewing, once its work is
// on the forge (go-quicbaas#938).
//
// A JOB WAITING ON A VERDICT RUNS NO WORKER, so it holds no container. Before
// this, the container sat idle for the whole CI pipeline and review — three
// hours on go-truststrap, still labelled with a pending session — and the
// job counted against the fleet and the backend while it did. The engine has
// already moved the lease off every axis on the transition (the state is off
// the project budget, so the lease is dormant); this makes the freed worker
// real.
//
// NOTHING IS LOST. publishForReview has pushed the branch and recorded its
// head, which is everything the container held that matters. If the review
// asks for another round, executing finds no session and goes back through
// initializing, which wakes the lease through admission and spawns a fresh
// container on the same branch and workspace — the path a replaced session
// takes (replaceSession).
//
// WHAT THE RESPAWNED SESSION KEEPS IS THE PROJECT'S CHOICE
// (Project.SessionRotation), the same as every other replacement that is not
// the work's doing (see conversationToCarry): resume, the default, reads the
// conversation here, before the close, and carries it into the respawned
// session's first prompt; fresh tells it only why it is new. The audit record
// says which (replayed_turns= or resume_unavailable=).
func (s *runnerRun) releaseWorker(ctx context.Context) error {
	if s.sessionID == "" {
		return nil
	}
	round := s.job.Iteration()
	carried, err := s.conversationToCarry(ctx)
	if err != nil {
		return err
	}
	s.job.Audit(auditWorkerReleased, "session="+s.sessionID+" round="+strconv.Itoa(round)+carried.auditFields())
	s.closeWorker(ctx)
	if carried.resumed() {
		s.turnNote = workerResumedNote(round, carried.transcript)
		return nil
	}
	s.turnNote = workerReleasedNote(round)
	return nil
}

// abandonWorker is the exit path of a run that did not reach a teardown of its
// own: a refusal after the spawn, a checkout or verification that would not
// run, a cancellation, a panic.
//
// It is teardownWorker, not a lighter cousin of it, so an unplanned exit
// publishes, closes and discards exactly what a planned one does — the defect
// go-quicbaas#750 reports is a session that was spawned and then never closed,
// and a second teardown path is how that comes back. A run that reached
// cleanup or escalating already tore down and passes straight through; a run
// that failed before it ever spawned still releases the signing agent and the
// materialized workspace spec it acquired, because teardownWorker does.
//
// A failure HERE is reported and never raised. The run's outcome is already
// decided by the time this runs, and returning a teardown failure from a
// deferred call would replace the reason the attempt is ending.
func (s *runnerRun) abandonWorker(ctx context.Context) {
	if err := s.teardownWorker(ctx, false); err != nil {
		s.job.Logger.Warnf("sdlc runner: task %s: teardown after an unplanned exit failed: %s",
			s.taskID(), err.Error())
	}
}

// publishWork is the container's half of the delivery: everything the worker
// changed is committed as one signed commit, the branch is pushed, and the
// head it produced is recorded on the Delivery for the forge writes that
// follow.
//
// NOTHING HERE RUNS ON THE DAEMON. Every command is an exec into the worker's
// container, in the checkout the initializing step made there.
func (s *runnerRun) publishWork(ctx context.Context) error {
	if err := s.loadDelivery(ctx); err != nil {
		return err
	}
	if s.attempt == nil {
		if err := s.resolve(ctx); err != nil {
			return err
		}
	}
	head, err := s.publisher(ctx).Publish(s.publishPlan())
	if err != nil {
		return err
	}
	// THE PUSH IS WHERE THE ENGINE'S OWN ROUNDS DISPLACE CI. Round two's
	// commits move the head while round one's pipeline is very likely still
	// running, and the write below replaces the head the previous round's
	// checks are bound to — so this is the last moment those runs exist
	// anywhere. The validator records the other case, a head that moved on the
	// forge ahead of the delivery; both fold through RecordSupersession, which
	// deduplicates, so a displacement seen by both is recorded once.
	superseded := supersedeChecks(ctx, s.attempt.forge, s.attempt.repo, s.delivery, head, s.runner.hooks.now())
	return s.runner.hooks.updateDelivery(ctx, s.job.Context(), s.delivery, func(d *Delivery) {
		d.RecordSupersession(superseded)
		d.HeadSHA = head
	})
}

// =============================================================================
// The post-turn publication — the branch is what survives the container
// =============================================================================

// PushTurn publishes what the turn just left in the worker's checkout. It is
// the orchestration.TurnPusher this run installs on its job, and the engine
// calls it after every turn that touched the tree (JobRun.PushTurn).
//
// IT IS A NO-OP OFF THE EPHEMERAL TIER, and that is the whole gate. A
// persistent workspace is still sitting on the host after the container goes,
// so pushing every turn would only add a forge round-trip to a turn that did
// not need one. On the ephemeral tier the tmpfs IS the only copy, so the push
// is what makes "the work was done" and "the work still exists" the same fact.
//
// IT PUBLISHES THE ATTEMPT'S OWN BRANCH, NEVER THE CARVE-OUT'S TARGET. Work
// mid-attempt has not been verified and has not been reviewed; the
// direct-push delivery's target is where an APPROVED change lands, and
// clearing RemoteBranch is what makes it unreachable from here.
//
// NOTHING TO PUBLISH IS A SUCCESS. A turn that touched the tree and then put
// it back — an edit reverted, a file formatted to what it already was —
// leaves the branch where it was, and refusing the job over it would be
// refusing a turn that behaved correctly.
func (s *runnerRun) PushTurn(ctx context.Context) error {
	if !s.ephemeralWorkspace() {
		return nil
	}
	return s.pushTurnWork(ctx)
}

// The run IS the job's TurnPusher. Stated at compile time so a signature drift
// on the engine's seam is a build failure here rather than a job that silently
// stopped publishing.
var _ orchestration.TurnPusher = (*runnerRun)(nil)

// ephemeralWorkspace reports whether this job's checkout lives only in the
// container's memory, which is the single question the post-turn push turns
// on. A job with no project record answers false: nothing knows where its
// work lives, so nothing may assume it is about to vanish.
func (s *runnerRun) ephemeralWorkspace() bool {
	return s.job.Project != nil &&
		s.job.Project.Storage.WorkspaceTier == orchestration.WorkspaceTierEphemeral
}

// publishBranch pushes the attempt's branch so the container may go.
//
// A failure here is REPORTED, never fatal: the job's outcome is already
// decided by the time teardown runs, and failing it a second time on a push
// would hide the reason it is ending. The unpushed work is named in the log
// so a person can find it.
func (s *runnerRun) publishBranch(ctx context.Context) {
	if s.delivery == nil || s.sessionID == "" || s.containerDir == "" || s.attempt == nil {
		return
	}
	plan := s.publishPlan()
	// The carve-out's target is not where unreviewed work belongs. An
	// escalated attempt publishes its own branch so a person can look at it,
	// never the trunk it never got approval to land on.
	plan.RemoteBranch = ""
	pub := s.publisher(ctx)
	ahead, err := pub.AheadOfBase(plan)
	if err != nil || !ahead {
		return
	}
	if _, err := pub.Push(plan); err != nil {
		s.job.Logger.Warnf("sdlc runner: task %s: publishing %s before teardown failed: %s",
			s.taskID(), s.delivery.Branch, err.Error())
	}
}
