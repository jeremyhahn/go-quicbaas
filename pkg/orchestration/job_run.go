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
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// JobRun — the engine's handle to one running job
// =============================================================================
//
// EVERY JOB HAS THIS, WHATEVER SHAPE IT IS. The lease and its heartbeat, the
// status snapshot and its persistence, the event stream, the audit trail, the
// lifecycle hooks, the sessions and the abort path are the envelope's, and
// the envelope is the same for a lead-driven workflow and for a delivery
// pipeline. A runner (job_runner.go) is handed one of these and drives its
// states through SetState; everything else it needs — spawning a session,
// prompting it, judging a criterion, escalating — it asks for here.
//
// SetState IS THE ONE CHOKE POINT. A state change proves the lease, writes
// and persists the snapshot, announces itself on the event stream, and runs
// the lifecycle moment that belongs to the state, in that order. Nothing else
// writes the job's state.

// FailureClassPanic is the JobStatus.FailureClass of a job whose runner
// panicked. It is the envelope's own classification: no runner reports it,
// because a runner that panicked reported nothing.
const FailureClassPanic = "panic"

// JobRunConfig is everything the envelope needs to build a run. Every field
// but Config, Agents and Logger is optional; the zero value of each is the
// correct behaviour for a run without it.
type JobRunConfig struct {
	JobID      string
	Runner     string
	Config     *JobConfig
	Task       *Task
	Project    *Project
	Workflow   *WorkflowDefinition
	StartState JobState
	Resumed    *JobStatus

	Agents      AgentService
	Logger      interfaces.Logger
	CostTracker CostTracker

	Leases   JobLeaseStore
	Node     string
	LeaseTTL time.Duration

	// PersistFn writes a status snapshot to the store. It is handed a copy
	// of the snapshot, never the published one: see snapshot.
	PersistFn func(status *JobStatus)
	AuditFn   func(eventType, details string)

	// StoreFn writes a status snapshot and REPORTS whether it was stored. It
	// is used where a write must not be best-effort — the workspace pin, which
	// a session may not be used before it is durable. Nil falls back to
	// PersistFn, which reports nothing.
	StoreFn func(status *JobStatus) error

	Hooks      JobLifecycleHooks
	HooksName  string
	Policy     *JobLifecyclePolicy
	Validators *ValidatorRegistry
}

// JobRun is one running job: the envelope the engine owns, and the handle a
// runner drives it through.
//
// The exported fields are read-only for the runner's whole life. Everything
// that changes goes through a verb.
type JobRun struct {
	// JobID is the job's identity, minted by the envelope before the run
	// starts. TenantID is the tenant every store call is scoped to.
	JobID    string
	TenantID string

	// Runner names the runner driving this job.
	Runner string

	// Config is what the job was started with.
	Config *JobConfig

	// Task is the task this job is an attempt at, loaded when
	// Config.TaskID is set and nil otherwise.
	Task *Task

	// Project is the project the job runs for, loaded when Config.ProjectID
	// names one the daemon holds a record for.
	Project *Project

	// Workflow is the definition a workflow job runs; nil for every other
	// runner.
	Workflow *WorkflowDefinition

	// StartState is the state the run enters: the runner's first state, or
	// the resume point of an interrupted job.
	StartState JobState

	// Resumed is the persisted status this run continues, nil for a fresh
	// job. A runner restores its own recoverable state from it.
	Resumed *JobStatus

	// Agents is the service every session call goes through; Logger is the
	// daemon's; CostTracker is the ledger a budget is checked against.
	Agents      AgentService
	Logger      interfaces.Logger
	CostTracker CostTracker

	status   atomic.Value // *JobStatus
	events   chan JobEvent
	decorate func(status *JobStatus)

	persistFn func(status *JobStatus)
	storeFn   func(status *JobStatus) error
	auditFn   func(eventType, details string)

	auditMu     sync.Mutex
	auditEvents []JobAuditEvent

	// The job's side of admission; see job_run_lease.go. A nil store runs
	// the job unleased, which is correct only for a run constructed outside
	// a service.
	leases        JobLeaseStore
	node          string
	leaseTTL      time.Duration
	lease         atomic.Pointer[JobLease]
	heartbeatAt   atomic.Int64
	heartbeatDone chan struct{}

	// budgetStates is the runner's per-state answer to whether a job there
	// is off its project's budget (runnerBudgetStates), read once when the
	// lease is taken; standing is where the job's lease stands now. Both
	// belong to the run goroutine, the only caller of SetState, and the
	// standing is kept here rather than read off the lease because the
	// heartbeat goroutine replaces the lease with the record it read, which
	// can be a write older than the last move.
	budgetStates map[JobState]bool
	standing     budgetStanding

	// cancelRun ends the run from outside the runner; abortCause is why.
	// finished is set when execute returns.
	cancelRun  atomic.Pointer[context.CancelFunc]
	abortCause atomic.Pointer[abortCause]
	finished   atomic.Bool

	// leaseSettled is closed once the run's lease question is answered:
	// taken, refused, or never asked because the run ended first. Job
	// recovery waits on it so the task dispatcher never sweeps a resumed
	// job's task before the lease that protects it exists.
	leaseSettled     chan struct{}
	leaseSettledOnce sync.Once

	// The terminal failure, classified once so the status carries a class
	// rather than prose.
	failureClass   string
	failureMessage string
	fault          EnvironmentFault

	// park is the runner's own judgement that the task must wait for a
	// person; see Park. Nil for a job that made none.
	park *BlockReason

	// satisfied is the runner's judgement that the work was already done. See
	// JobRun.Satisfy — it concludes the task rather than parking it.
	satisfied bool

	// superseded is the runner's judgement that the attempt's pull request
	// was closed without merging. See JobRun.Supersede.
	superseded bool

	// turnPusher publishes a turn's work as soon as the turn produced any.
	// Nil for a run nothing installed one on, which is every lead-driven job
	// and every job whose workspace survives its container. See
	// job_turn_push.go.
	turnPusher atomic.Pointer[turnPusherHolder]

	// Lifecycle seams: the hook set and its registered name, the operator's
	// policy, the state-keyed dispatch table for the entering moments, the
	// moment the job last passed through, and the directory Prepare
	// returned. jobCtx is the view hooks and validators receive.
	hooks             JobLifecycleHooks
	hooksName         string
	policy            *JobLifecyclePolicy
	stageHooks        map[JobState]func(context.Context) error
	stage             JobStage
	workDir           string
	container         *JobContainer
	jobCtx            *JobContext
	escalationReason  string
	validatorRegistry *ValidatorRegistry

	// announced is the state the runner last announced on the event stream.
	// SetState emits its own state_change only when the runner did not
	// announce the transition itself, so a transition produces exactly one
	// state_change however it was reached — the runner's message when it
	// had one, the engine's otherwise.
	announced atomic.Value // JobState

	// coalesced counts the events a full stream displaced; see publish.
	coalesced atomic.Int64

	// entered is set by the first SetState. UseHooks is refused after it:
	// a hook set installed once a moment has already passed would silently
	// skip that moment.
	entered atomic.Bool

	// memory is the job's accumulated context across review rounds, and
	// iteration is the round it is on. Both are the envelope's because both
	// are persisted and restored on resume.
	memory    *JobMemory
	iteration int

	// sessionMu guards the sessions the run has spawned and not closed.
	sessionMu sync.Mutex
	sessions  []string

	// fanoutMu guards the sub-job lineage. It lives on the run, not on the
	// status, because a status the run has published is IMMUTABLE: it is
	// read by every status poll and by the next snapshot, from goroutines
	// the run does not own, so a caller that appended a child to it in
	// place would be writing under those readers. Everything that changes
	// about a job changes here and reaches the record at the next snapshot.
	fanoutMu    sync.Mutex
	parentJobID string
	childJobIDs []string
	spawnedBy   string
	spawnedAt   int64

	// stageMu guards the moments the run has entered. Like the fanout
	// lineage it lives on the run rather than on the published status,
	// which is immutable once published; the list reaches the record at the
	// next snapshot, and markStage takes one.
	stageMu sync.Mutex
	stages  []JobStageRecord

	// agentID is the fleet node the run's sessions were dispatched to. It
	// is atomic because Spawn runs on the loop's goroutine while a status
	// poll reads the run from another.
	agentID atomic.Value // string

	// workspacePin is where the job's first named workspace was provisioned.
	// Every later spawn of the job that names a workspace carries it, so the
	// job's sessions stay on the host holding its checkout. Atomic for the
	// reason agentID is.
	workspacePin atomic.Pointer[WorkspacePlacement]

	// verifyMu guards the last validator pipeline's results, for the same
	// reason stageMu guards the moments: the published status is immutable.
	verifyMu     sync.Mutex
	verification []ValidationResult
}

// NewJobRun builds a run from cfg, outside the envelope that normally owns
// one.
//
// It exists for a MODULE'S RUNNER: a runner lives in its own package, and
// without an exported constructor the only way to exercise it would be to
// boot a daemon. A run built here has whatever cfg gave it and nothing else —
// no lease unless Leases is set, no persistence unless PersistFn is — which
// is exactly what a runner test wants and never what a job the engine runs
// should have. launchJob is still the ONE path a real job starts through.
func NewJobRun(cfg JobRunConfig) *JobRun { return newJobRun(cfg) }

// newJobRun builds a run from cfg. It is the only constructor; launchJob
// calls it for a real job and the ControlLoop test wrappers call it for a
// standalone loop.
func newJobRun(cfg JobRunConfig) *JobRun {
	config := cfg.Config
	if config == nil {
		config = &JobConfig{}
	}
	tenantID := config.TenantID
	leaseTTL := cfg.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = DefaultJobLeaseTTL
	}
	run := &JobRun{
		JobID:             cfg.JobID,
		TenantID:          tenantID,
		Runner:            ResolveJobRunnerName(cfg.Runner),
		Config:            config,
		Task:              cfg.Task,
		Project:           cfg.Project,
		Workflow:          cfg.Workflow,
		StartState:        cfg.StartState,
		Resumed:           cfg.Resumed,
		Agents:            cfg.Agents,
		Logger:            cfg.Logger,
		CostTracker:       cfg.CostTracker,
		events:            make(chan JobEvent, defaultEventBufferSize),
		leaseSettled:      make(chan struct{}),
		persistFn:         cfg.PersistFn,
		storeFn:           cfg.StoreFn,
		auditFn:           cfg.AuditFn,
		leases:            cfg.Leases,
		node:              cfg.Node,
		leaseTTL:          leaseTTL,
		hooks:             cfg.Hooks,
		hooksName:         cfg.HooksName,
		policy:            cfg.Policy,
		validatorRegistry: cfg.Validators,
		memory:            NewJobMemory(),
	}
	if run.hooksName == "" && run.hooks != nil {
		run.hooksName = "explicit"
	}
	run.stageHooks = map[JobState]func(context.Context) error{
		JobStateInitializing: run.stagePrepare,
		JobStateReviewing:    run.stageReviewable,
		JobStateCompleting:   run.stageComplete,
		JobStateEscalating:   run.stageBlocked,
	}
	// A RUN BUILT WITH A RESUMED STATUS IS A RESUMED RUN, and it hydrates
	// here rather than waiting for its caller to remember. The recoverable
	// state — the memory, the iteration, the park — is part of what the run
	// IS; a constructor that returned a half-restored one made correctness
	// depend on every call site repeating the same two lines, and the park
	// is durable precisely so a restart cannot lose it.
	run.restore(cfg.Resumed)
	return run
}

// =============================================================================
// Read-only views
// =============================================================================

// Status returns the current job status, nil before the first state.
func (r *JobRun) Status() *JobStatus {
	val := r.status.Load()
	if val == nil {
		return nil
	}
	status, ok := val.(*JobStatus)
	if !ok {
		return nil
	}
	return status
}

// Events returns a read-only channel of job events.
func (r *JobRun) Events() <-chan JobEvent {
	return r.events
}

// Memory is the job's accumulated context across review rounds.
func (r *JobRun) Memory() *JobMemory {
	return r.memory
}

// Iteration is the review round the job is on.
func (r *JobRun) Iteration() int {
	return r.iteration
}

// WorkDir is the directory Prepare returned, empty before Prepare ran or
// when the job runs no hooks.
func (r *JobRun) WorkDir() string {
	return r.workDir
}

// SetWorkDir records the directory the job's work happens in.
//
// The engine sets it from Prepare's return; a runner that decides the
// directory itself — the SDLC runner names the checkout its worker's
// container is given, which Prepare no longer creates — sets it here, so the
// validators and hooks that read JobContext.WorkDir see the same directory
// the session was spawned into.
func (r *JobRun) SetWorkDir(dir string) {
	r.workDir = dir
}

// SetContainer records the containerized worker session the job's work
// happens in, and the path the CONTAINER calls its checkout.
//
// A runner calls it right after each spawn that produces a session, which is
// once for a runner holding one session for the whole job and once per round
// for one that replaces its session — the review runner does, when the head
// moves under it. The latest call wins, because the job's work is wherever
// the CURRENT session is. From then on a validator judging the job's work —
// the binding's build and tests — runs its command where the work is, through
// the same exec seam the delivery's git goes through, instead of on the
// daemon.
func (r *JobRun) SetContainer(sessionID, workDir string) error {
	container, err := NewJobContainer(sessionID, workDir, func(ctx context.Context, spec *ExecSpec) (*ExecResult, error) {
		return r.Exec(ctx, &ExecSessionRequest{SessionID: sessionID, Spec: spec})
	})
	if err != nil {
		return err
	}
	r.container = container
	return nil
}

// Container is the containerized session the job's work happens in, nil for
// a job that spawned none.
func (r *JobRun) Container() *JobContainer {
	return r.container
}

// SetIteration restores the review round a resumed job was on. It is the
// resume half of NextIteration and is not a general setter: a runner calls it
// once, from the persisted status, before it enters its first state.
func (r *JobRun) SetIteration(iteration int) {
	if iteration > 0 {
		r.iteration = iteration
	}
}

// NextIteration advances the job to its next review round and returns it.
// The round reaches the status, the events and JobContext.Iteration through
// the next snapshot.
func (r *JobRun) NextIteration() int {
	r.iteration++
	return r.iteration
}

// Bound reports whether the job is an attempt at a task.
func (r *JobRun) Bound() bool {
	return r.Config != nil && r.Config.TaskID != ""
}

// Fault is the environment fault the job's terminal error was classified as,
// zero when the job did not fail on one.
func (r *JobRun) Fault() EnvironmentFault {
	return r.fault
}

// Verify runs the criteria through the job's validator registry AND records
// what they said on the job.
//
// IT IS THE SEAM EVERY RUNNER VERIFIES THROUGH, rather than each runner
// calling Validators().RunPipeline itself, because the results are the only
// evidence a reader has for why a job was sent back: a runner that ran the
// pipeline and kept the answer to itself left the job page saying a job
// failed with nothing naming the command that refused it (go-quicbaas#796).
// Recording it once, here, is what makes that impossible to forget.
//
// Only a pipeline that RAN replaces the record: a criteria list that is empty
// verifies nothing and must not erase the round before it.
func (r *JobRun) Verify(ctx context.Context, criteria []ReviewCriterion) ([]ValidationResult, bool, error) {
	results, passed, err := r.Validators().RunPipeline(ctx, r.Context(), criteria)
	// Each result keeps its tail: it is persisted on the job, and runners
	// quote it back to the agent as review feedback. See
	// MaxValidationTextBytes.
	for i, result := range results {
		results[i] = result.bounded()
	}
	if len(results) > 0 {
		r.verifyMu.Lock()
		r.verification = append([]ValidationResult(nil), results...)
		r.verifyMu.Unlock()
	}
	return results, passed, err
}

// verificationResults copies the last pipeline's results.
func (r *JobRun) verificationResults() []ValidationResult {
	r.verifyMu.Lock()
	defer r.verifyMu.Unlock()
	if len(r.verification) == 0 {
		return nil
	}
	return append([]ValidationResult(nil), r.verification...)
}

// Validators is the registry criteria are judged through: the one a test
// injected, or the engine's.
func (r *JobRun) Validators() *ValidatorRegistry {
	if r.validatorRegistry != nil {
		return r.validatorRegistry
	}
	return validators
}

// Policy is the operator's command allowlist and per-stage bounds, resolved
// from the agent service on first use so a run built directly by a test
// still has one.
func (r *JobRun) Policy() *JobLifecyclePolicy {
	if r.policy != nil {
		return r.policy
	}
	var policy JobLifecyclePolicy
	if provider, ok := r.Agents.(JobLifecyclePolicyProvider); ok {
		policy = provider.JobLifecyclePolicy()
	}
	policy.StageTimeouts = policy.StageTimeouts.WithDefaults()
	r.policy = &policy
	return r.policy
}

// Guard is the operator's command allowlist.
func (r *JobRun) Guard() ExecutionGuard {
	return r.Policy().Guard
}

// Context is the view of the job that hooks and validators receive. It is
// one value refreshed in place, so a hook that kept a reference sees the
// job's current iteration, memory and prepared directory.
func (r *JobRun) Context() *JobContext {
	if r.jobCtx == nil {
		r.jobCtx = &JobContext{Agents: r.Agents}
	}
	ctx := r.jobCtx
	ctx.JobID = r.JobID
	ctx.TenantID = r.TenantID
	ctx.Config = r.Config
	ctx.Workflow = r.Workflow
	ctx.Iteration = r.iteration
	ctx.Memory = r.memory
	ctx.WorkDir = r.workDir
	ctx.Container = r.container
	ctx.Agents = r.Agents
	ctx.Guard = r.Guard()
	if r.Config != nil {
		ctx.ProjectID = r.Config.ProjectID
		ctx.TaskID = r.Config.TaskID
		ctx.Attempt = r.Config.Attempt
	}
	return ctx
}

// =============================================================================
// State: the one choke point
// =============================================================================

// Decorate installs the runner's contribution to every status snapshot: the
// recoverable state only the runner knows about. It is applied after the
// envelope's own fields and before the snapshot is stored or persisted.
func (r *JobRun) Decorate(fn func(status *JobStatus)) {
	r.decorate = fn
}

// SetState moves the job to state. It is the ONE path a job's state changes
// through: the lease is proved, the snapshot is written and persisted, the
// change is announced, and the lifecycle moment that belongs to the state
// runs. A hook failure is returned as a JobHookError and fails the job with
// FailureClassHook.
//
// A state that moves the job on or off its project's budget moves it first,
// and a move back onto a full budget WAITS here — the job still in the state
// it is leaving — until a slot frees or the run ends. See moveBudget.
func (r *JobRun) SetState(ctx context.Context, state JobState) error {
	if err := r.moveBudget(ctx, state); err != nil {
		return err
	}
	r.entered.Store(true)
	r.snapshot(state)
	r.emitTransition(state)
	if err := r.enterStage(ctx, state); err != nil {
		r.failureClass = FailureClassHook
		r.failureMessage = err.Error()
		r.snapshot(JobStateFailed)
		r.Emit(JobStateFailed, "lifecycle hook failed entering "+string(state)+": "+err.Error())
		r.announced.Store(JobState(""))
		return err
	}
	return nil
}

// snapshot builds, decorates, stores and persists the status for state.
// Every transition proves the lease is still held; see heartbeat.
func (r *JobRun) snapshot(state JobState) *JobStatus {
	r.heartbeat(context.Background())
	status := &JobStatus{
		JobID:        r.JobID,
		TenantID:     r.TenantID,
		State:        state,
		Iteration:    r.iteration,
		Description:  r.Config.Description,
		StartedAt:    r.startedAt(),
		UpdatedAt:    time.Now().UnixNano(),
		Events:       r.auditEventsCopy(),
		Config:       r.Config,
		Memory:       r.memory,
		Runner:       r.Runner,
		WorkflowName: r.workflowName(),
		TaskID:       r.Config.TaskID,
		Attempt:      r.Config.Attempt,
		PromptAppend: r.Config.PromptAppend,
		Node:         r.node,
		HeartbeatAt:  r.heartbeatAt.Load(),
		Stage:        string(r.stage),
		FailureClass: r.failureClass,
		Error:        r.failureMessage,
		Park:         r.park.clone(),
		Satisfied:    r.satisfied,
		Superseded:   r.superseded,
	}
	r.fanoutMu.Lock()
	status.ParentJobID = r.parentJobID
	status.SpawnedBy = r.spawnedBy
	status.SpawnedAt = r.spawnedAt
	if len(r.childJobIDs) > 0 {
		status.ChildJobIDs = append([]string(nil), r.childJobIDs...)
	}
	r.fanoutMu.Unlock()

	status.Stages = r.stageHistory()
	status.AgentID = r.AgentID()
	status.WorkspacePin = r.WorkspacePin()
	status.Verification = r.verificationResults()

	if r.decorate != nil {
		r.decorate(status)
	}
	r.status.Store(status)
	if r.persistFn != nil {
		// The hook is handed a copy. Publication is the moment the status
		// becomes immutable — republishFanout copies the published value
		// from another goroutine — and the hook runs after it, so a hook
		// that wrote to the published status would be racing that copy.
		// Everything the run itself contributes is set above, before the
		// store, so the copy the hook persists carries the same record.
		persisted := *status
		r.persistFn(&persisted)
	}
	return status
}

// AddChildJob records a sub-job this job fanned out to. The child reaches
// the record at the next snapshot; a job that has already written its last
// status keeps its lineage in the persisted record instead.
func (r *JobRun) AddChildJob(childJobID string) {
	if childJobID == "" {
		return
	}
	r.fanoutMu.Lock()
	r.childJobIDs = append(r.childJobIDs, childJobID)
	ids := append([]string(nil), r.childJobIDs...)
	r.fanoutMu.Unlock()
	r.republishFanout(func(status *JobStatus) { status.ChildJobIDs = ids })
}

// ChildJobIDs lists the sub-jobs this job has fanned out to.
func (r *JobRun) ChildJobIDs() []string {
	r.fanoutMu.Lock()
	defer r.fanoutMu.Unlock()
	if len(r.childJobIDs) == 0 {
		return nil
	}
	return append([]string(nil), r.childJobIDs...)
}

// RecordParent records the job this one was spawned from, by whom and when.
func (r *JobRun) RecordParent(parentJobID, spawnedBy string, spawnedAt int64) {
	r.fanoutMu.Lock()
	r.parentJobID = parentJobID
	r.spawnedBy = spawnedBy
	r.spawnedAt = spawnedAt
	r.fanoutMu.Unlock()
	r.republishFanout(func(status *JobStatus) {
		status.ParentJobID = parentJobID
		status.SpawnedBy = spawnedBy
		status.SpawnedAt = spawnedAt
	})
}

// republishFanout makes the new lineage visible on the record at once,
// rather than at the run's next state change, because the caller that
// spawned the child expects to read it back.
//
// It publishes a COPY: a status the run has published is immutable, so the
// copy is taken from a value no one is writing, the lineage is set on the
// copy, and the swap is compared against the status the copy was taken from
// — a snapshot that landed in between wins and is retried against, so a
// concurrent state change can never be lost. No field the run owns is read
// from this goroutine; only the published value is.
func (r *JobRun) republishFanout(apply func(status *JobStatus)) {
	for {
		current, ok := r.status.Load().(*JobStatus)
		if !ok || current == nil {
			// Nothing published yet; the first snapshot carries the lineage.
			return
		}
		updated := *current
		apply(&updated)
		if r.status.CompareAndSwap(current, &updated) {
			return
		}
	}
}

// startedAt returns the started timestamp from the current status, or now
// when the job has none yet.
func (r *JobRun) startedAt() int64 {
	if current := r.Status(); current != nil {
		return current.StartedAt
	}
	return time.Now().UnixNano()
}

// workflowName is what the job's lease and status name its work: the
// workflow a workflow job runs, or the runner for every other shape.
func (r *JobRun) workflowName() string {
	if r.Workflow != nil && r.Workflow.Name != "" {
		return r.Workflow.Name
	}
	if r.Config != nil && r.Config.WorkflowName != "" {
		return r.Config.WorkflowName
	}
	return ""
}

// =============================================================================
// Events and the audit trail
// =============================================================================

// Emit announces a state change on the event stream. A full stream makes room
// by coalescing its oldest events (see publish): a slow subscriber must not
// hold the job, and must not lose the newest event either.
func (r *JobRun) Emit(state JobState, message string) {
	r.announced.Store(state)
	r.publish(JobEvent{
		JobID:     r.JobID,
		Type:      JobEventTypeStateChange,
		State:     state,
		Message:   message,
		Iteration: r.iteration,
		Timestamp: time.Now().UnixNano(),
	})
}

// emitTransition announces the state the job has just entered, unless the
// runner already announced it with a message of its own.
func (r *JobRun) emitTransition(state JobState) {
	announced, _ := r.announced.Load().(JobState)
	r.announced.Store(JobState(""))
	if announced == state {
		return
	}
	r.Emit(state, "entering state "+string(state))
}

// JobEventMetadataCoalesced is the JobEvent.Metadata key carrying how many
// older events the stream had displaced, in total, when this event was put on
// it.
const JobEventMetadataCoalesced = "coalesced_events"

// publish puts one event on the stream, never blocking the job.
//
// A FULL STREAM COALESCES; IT DOES NOT DROP THE NEW EVENT. The oldest buffered
// event makes room, and the event that displaced it carries the running count
// under JobEventMetadataCoalesced, so a subscriber knows exactly how much of
// the stream it missed and still reads the newest state. The old behaviour
// kept the stale events and threw away the new one: a delivery looping on
// its merge train logged "event channel full, dropping event" for every one
// of seventy-three ejections, and every one was lost (X15, 2026-09-25). The
// job's audit trail, not this stream, is the durable record.
//
// The first displacement in a run is logged; the count carries the rest.
func (r *JobRun) publish(event JobEvent) {
	for {
		select {
		case r.events <- event:
			return
		default:
		}
		select {
		case <-r.events:
			total := r.coalesced.Add(1)
			if total == 1 {
				r.Logger.Warnf("job run %s: event stream full, coalescing its oldest events to deliver: %s", r.JobID, event.Message)
			}
			event.Metadata = withCoalesced(event.Metadata, total)
		default:
			// A subscriber emptied the stream in between; the send is retried.
		}
	}
}

// withCoalesced returns a copy of metadata carrying the coalesced count, so
// the caller's map is never written.
func withCoalesced(metadata map[string]string, total int64) map[string]string {
	next := make(map[string]string, len(metadata)+1)
	for key, value := range metadata {
		next[key] = value
	}
	next[JobEventMetadataCoalesced] = strconv.FormatInt(total, 10)
	return next
}

// Audit records an event in the job's audit trail and calls the audit
// callback if one is configured. Events are captured either way.
func (r *JobRun) Audit(eventType, details string) {
	r.auditMu.Lock()
	r.auditEvents = append(r.auditEvents, JobAuditEvent{
		Timestamp: time.Now().UnixNano(),
		Type:      eventType,
		Details:   details,
	})
	r.auditMu.Unlock()
	if r.auditFn != nil {
		r.auditFn(eventType, details)
	}
}

// auditEventsCopy returns a snapshot of the audit trail under the audit
// mutex. The returned slice is safe to read without holding the lock.
func (r *JobRun) auditEventsCopy() []JobAuditEvent {
	r.auditMu.Lock()
	cp := make([]JobAuditEvent, len(r.auditEvents))
	copy(cp, r.auditEvents)
	r.auditMu.Unlock()
	return cp
}

// =============================================================================
// Sessions
// =============================================================================

// Spawn starts an agent session for this job. It is the ONE place every
// spawn passes through, so a session the engine runs never depends on the
// daemon host's ambient identity: the engine's own grant, configuration
// directory and pooled-account selection are applied (worker_spawn.go), the
// job and project are stamped on the record, and the session is tracked so
// CloseAll can release it.
func (r *JobRun) Spawn(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error) {
	if req == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "spawn session request must not be nil",
		}
	}
	if req.JobID == "" {
		req.JobID = r.JobID
	}
	if req.ProjectID == "" && r.Config != nil {
		req.ProjectID = r.Config.ProjectID
	}
	// THE JOB'S RUN-TIME PROMPT MATERIAL REACHES EVERY SESSION IT SPAWNS,
	// after whatever the spawning slot added, so the most specific content —
	// the task's or the caller's — is read last. It goes on the job's trail
	// once per spawn, by size, so the job page says what a session ran with.
	if r.Config != nil {
		req.PromptAppend = ComposePrompt(req.PromptAppend, r.Config.PromptAppend)
	}
	if req.PromptAppend != "" {
		r.Audit("prompt:append", strconv.Itoa(len(req.PromptAppend))+" bytes appended to the session's prompt")
	}
	// A SPAWN NAMING AN AGENT DOES NOT KNOW ITS BACKEND YET, and the worker
	// environment is decided per backend — so it is applied to each Agent the
	// reference resolves to, as that Agent is tried.
	configDir := r.Policy().AgentConfigDir
	if req.AgentRef().IsZero() {
		ApplyWorkerSpawnEnvironment(req, configDir)
	} else {
		req.prepareCandidate = func(candidate *SpawnSessionRequest) { ApplyWorkerSpawnEnvironment(candidate, configDir) }
	}
	// A LATER SESSION GOES WHERE THE CHECKOUT IS. The job's first named
	// workspace was provisioned on one host and cloned into there; a round,
	// a rolled-over session or a respawn after a restart continues that
	// clone, so it is pinned to that host and refused anywhere else.
	if req.Workspace != nil && req.Workspace.Pin == nil {
		req.Workspace.Pin = r.WorkspacePin()
	}
	resp, err := r.Agents.SpawnSession(ctx, req)
	if err != nil {
		return nil, err
	}
	r.auditAgentFailovers(req, resp)
	if resp != nil && resp.SessionID != "" {
		r.trackSession(resp.SessionID)
		r.RecordAgent(resp.AgentID)
		if resp.Workspace != nil {
			if pinErr := r.recordWorkspace(*resp.Workspace); pinErr != nil {
				// THE SESSION IS NOT HANDED OUT. Its checkout would be made
				// in a workspace no durable record names, and a recovery
				// after a crash would be free to place the attempt anywhere.
				r.Close(context.WithoutCancel(ctx), resp.SessionID)
				return nil, pinErr
			}
		}
	}
	return resp, nil
}

// auditAgentFailovers puts every Agent a pooled spawn passed over on the job's
// own trail, so the job page says why its session runs where it does.
func (r *JobRun) auditAgentFailovers(req *SpawnSessionRequest, resp *SpawnSessionResponse) {
	if resp == nil || resp.ResolvedAgentID == "" {
		return
	}
	ref := req.AgentRef().String()
	for _, attempt := range resp.Failovers {
		r.Audit("agent:failover", ref+": passed over "+attempt.AgentID+" ("+attempt.Reason+")")
	}
	r.Audit("agent:resolved", ref+": session runs as agent "+resp.ResolvedAgentID)
}

// Prompt sends one turn to a session the job holds.
//
// EVERY TURN THAT GOES THROUGH THE ENVELOPE IS ATTRIBUTED TO THE JOB, here
// rather than at each runner's call site. A runner that forgot to attach the
// attribution would leave its turns out of the consumption ledger's chain —
// visible in the tenant's total and attributable to nothing under it — and
// the spend authority would then be capping an account whose largest consumer
// it could not name. Attaching it once, at the seam every runner already
// prompts through, is what makes that impossible to forget.
//
// A caller that already attached one (the control loop, which additionally
// knows the AGENT that ran the turn) keeps it: overwriting would replace a
// richer attribution with a poorer one.
func (r *JobRun) Prompt(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error) {
	if TurnAttributionFromContext(ctx).JobID == "" {
		taskID := ""
		if r.Config != nil {
			taskID = r.Config.TaskID
		}
		ctx = WithTurnAttribution(ctx, TurnAttribution{JobID: r.JobID, TaskID: taskID, Kind: r.Runner})
	}
	return r.Agents.PromptSession(ctx, req)
}

// Exec runs one command inside the container a session the job holds runs in.
//
// It is Prompt's peer for the steps the SYSTEM owns rather than the agent: a
// delivery's commit and push are not a turn to be persuaded into, they are
// commands with an exit status. The agent service must be a SessionExecutor —
// the engine's own is; a remote or embedded client is not — and a session
// with no container is refused rather than run on the daemon.
func (r *JobRun) Exec(ctx context.Context, req *ExecSessionRequest) (*ExecResult, error) {
	if req == nil || req.SessionID == "" {
		return nil, &AgentServiceError{Sentinel: ErrSessionIDRequired}
	}
	executor, ok := r.Agents.(SessionExecutor)
	if !ok {
		return nil, &SessionExecUnsupportedError{SessionID: req.SessionID}
	}
	return executor.ExecSession(ctx, req)
}

// Close releases one session the job holds.
//
// Closing the session the job's container runs in forgets the container too
// (SetContainer), so nothing judging the job's work afterwards execs into a
// container that is gone — the sdlc runner closes its worker for the whole of
// reviewing and spawns a new one only if a round is asked for.
func (r *JobRun) Close(ctx context.Context, sessionID string) {
	if sessionID == "" {
		return
	}
	if r.container != nil && r.container.SessionID == sessionID {
		r.container = nil
	}
	r.untrackSession(sessionID)
	if _, err := r.Agents.CloseSession(ctx, &CloseSessionRequest{SessionID: sessionID}); err != nil {
		r.Logger.Warnf("job run: failed to close session %s: %s", sessionID, err.Error())
	}
}

// CloseAll releases every session the job still holds.
func (r *JobRun) CloseAll(ctx context.Context) {
	for _, id := range r.takeSessions() {
		if _, err := r.Agents.CloseSession(ctx, &CloseSessionRequest{SessionID: id}); err != nil {
			r.Logger.Warnf("job run: failed to close session %s: %s", id, err.Error())
		}
	}
}

// Sessions lists the sessions the job currently holds, in spawn order.
func (r *JobRun) Sessions() []string {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	if len(r.sessions) == 0 {
		return nil
	}
	ids := make([]string, len(r.sessions))
	copy(ids, r.sessions)
	return ids
}

// trackSession records a session the job holds.
func (r *JobRun) trackSession(sessionID string) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	for _, id := range r.sessions {
		if id == sessionID {
			return
		}
	}
	r.sessions = append(r.sessions, sessionID)
}

// untrackSession forgets a session without closing it.
func (r *JobRun) untrackSession(sessionID string) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	for i, id := range r.sessions {
		if id == sessionID {
			r.sessions = append(r.sessions[:i], r.sessions[i+1:]...)
			return
		}
	}
}

// takeSessions empties the tracked set and returns what it held.
func (r *JobRun) takeSessions() []string {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	ids := r.sessions
	r.sessions = nil
	return ids
}

// =============================================================================
// Lifecycle moments
// =============================================================================

// UseHooks installs the lifecycle hook set the job runs, and is refused once
// the job has entered its first state: a hook set installed after a moment
// has passed would silently skip that moment.
func (r *JobRun) UseHooks(name string, hooks JobLifecycleHooks) error {
	if r.entered.Load() {
		return &JobHooksRegistrationError{
			Name:    name,
			Message: "hooks must be installed before the job enters its first state",
		}
	}
	if hooks == nil {
		return &JobHooksRegistrationError{Name: name, Message: "hooks must not be nil"}
	}
	r.hooks = hooks
	r.hooksName = name
	if r.hooksName == "" {
		r.hooksName = "explicit"
	}
	return nil
}

// Hooks is the lifecycle hook set the job runs, nil when it runs none.
func (r *JobRun) Hooks() JobLifecycleHooks {
	return r.hooks
}

// enterStage runs the lifecycle moment that belongs to the state the job is
// entering, if the job has hooks and the state has a moment.
func (r *JobRun) enterStage(ctx context.Context, state JobState) error {
	if r.hooks == nil {
		return nil
	}
	hook, ok := r.stageHooks[state]
	if !ok {
		return nil
	}
	return hook(ctx)
}

// RunStage records the moment, bounds it with the operator's timeout for the
// stage, and wraps a failure in JobHookError so the job fails with
// FailureClassHook. A runner that drives its own steps rather than the
// state-keyed table calls this directly.
func (r *JobRun) RunStage(ctx context.Context, stage JobStage, fn func(context.Context) error) error {
	r.markStage(stage)
	hookCtx, cancel := context.WithTimeout(ctx, r.Policy().StageTimeouts.For(stage))
	defer cancel()
	if err := fn(hookCtx); err != nil {
		r.Audit("orchestration:stage_failed", "stage="+string(stage)+" hooks="+r.hooksName+" error="+err.Error())
		return &JobHookError{Stage: stage, Hooks: r.hooksName, JobID: r.JobID, Cause: err}
	}
	r.Audit("orchestration:stage_passed", "stage="+string(stage)+" hooks="+r.hooksName)
	return nil
}

// markStage records the moment on the status and announces it on the event
// stream as a stage event.
func (r *JobRun) markStage(stage JobStage) {
	r.stage = stage
	r.recordStage(stage)
	var state JobState
	if status := r.Status(); status != nil {
		state = status.State
		r.snapshot(state)
	}
	r.publish(JobEvent{
		JobID:     r.JobID,
		Type:      JobEventTypeStage,
		State:     state,
		Message:   "entering stage " + string(stage),
		Iteration: r.iteration,
		Metadata:  map[string]string{"stage": string(stage), "hooks": r.hooksName},
		Timestamp: time.Now().UnixNano(),
	})
}

// recordStage appends the moment to the run's stage history.
//
// A REPEATED MOMENT IS RECORDED AGAIN, not collapsed: a job that reviews,
// sends work back and reviews again entered reviewable twice, and a timeline
// that showed one would hide a whole round.
func (r *JobRun) recordStage(stage JobStage) {
	r.stageMu.Lock()
	r.stages = append(r.stages, JobStageRecord{Stage: stage, EnteredAt: time.Now().UnixNano()})
	r.stageMu.Unlock()
}

// stageHistory copies the moments the run has entered, oldest first.
func (r *JobRun) stageHistory() []JobStageRecord {
	r.stageMu.Lock()
	defer r.stageMu.Unlock()
	if len(r.stages) == 0 {
		return nil
	}
	return append([]JobStageRecord(nil), r.stages...)
}

// AgentID is the fleet node the run's sessions were dispatched to, empty
// until a spawn resolves one.
func (r *JobRun) AgentID() string {
	id, _ := r.agentID.Load().(string)
	return id
}

// RecordAgent names the fleet node the run's work was dispatched to. The
// FIRST answer wins: a run whose lead and worker land on different agents is
// attributed to the one that took its work, and a later spawn on a second
// agent must not silently retitle the whole job.
func (r *JobRun) RecordAgent(agentID string) {
	if agentID == "" {
		return
	}
	r.agentID.CompareAndSwap(nil, agentID)
}

// WorkspacePin is where the job's named workspace was provisioned, nil until
// a spawn provisioned one. The returned value is a copy.
func (r *JobRun) WorkspacePin() *WorkspacePlacement {
	pin := r.workspacePin.Load()
	if pin == nil {
		return nil
	}
	cp := *pin
	return &cp
}

// recordWorkspace pins the job to the host its workspace was provisioned on.
// The FIRST placement wins: every later session was pinned to it, so a second
// answer could only be the same host.
//
// THE PIN IS DURABLE BEFORE THE SESSION IS USED. It is published and persisted
// here, not left for the next state transition: the runner clones into the
// workspace before it transitions, and a daemon that died in that window would
// otherwise recover a job with no record of where its checkout is — free to
// provision the same attempt on another host instead of honouring, or
// refusing, the original placement.
//
// THE WRITE IS STRICT. A pin the store refused is a pin a recovery will not
// find, so the failure is returned — the caller closes the session and hands
// it to no one — and the in-memory pin is withdrawn with it, so nothing this
// run publishes later claims a placement no session holds.
func (r *JobRun) recordWorkspace(placement WorkspacePlacement) error {
	pin := &placement
	if !r.workspacePin.CompareAndSwap(nil, pin) {
		return nil
	}
	published := r.republishWorkspacePin()
	if published == nil {
		return nil
	}
	persisted := *published
	switch {
	case r.storeFn != nil:
		if err := r.storeFn(&persisted); err != nil {
			r.workspacePin.CompareAndSwap(pin, nil)
			r.republishWorkspacePin()
			return &WorkspacePinPersistError{JobID: r.JobID, Key: placement.Key, Cause: err}
		}
	case r.persistFn != nil:
		r.persistFn(&persisted)
	}
	return nil
}

// republishWorkspacePin swaps the published status for one carrying the
// workspace pin, and answers what it published. Nil when the job has
// published nothing yet; the envelope publishes its start state before any
// runner can spawn, so that is a run no envelope drives.
func (r *JobRun) republishWorkspacePin() *JobStatus {
	for {
		current, ok := r.status.Load().(*JobStatus)
		if !ok || current == nil {
			return nil
		}
		updated := *current
		updated.WorkspacePin = r.WorkspacePin()
		updated.UpdatedAt = time.Now().UnixNano()
		if r.status.CompareAndSwap(current, &updated) {
			return &updated
		}
	}
}

// stagePrepare runs Prepare and records the directory every session is
// spawned into.
func (r *JobRun) stagePrepare(ctx context.Context) error {
	return r.RunStage(ctx, JobStagePrepare, func(hookCtx context.Context) error {
		workDir, err := r.hooks.Prepare(hookCtx, r.Context())
		if err != nil {
			return err
		}
		r.workDir = workDir
		r.Context().WorkDir = workDir
		return nil
	})
}

// stageReviewable runs Reviewable on entering reviewing.
func (r *JobRun) stageReviewable(ctx context.Context) error {
	return r.RunStage(ctx, JobStageReviewable, func(hookCtx context.Context) error {
		return r.hooks.Reviewable(hookCtx, r.Context())
	})
}

// stageComplete runs Complete on entering completing.
func (r *JobRun) stageComplete(ctx context.Context) error {
	return r.RunStage(ctx, JobStageComplete, func(hookCtx context.Context) error {
		return r.hooks.Complete(hookCtx, r.Context())
	})
}

// stageBlocked runs Blocked on entering escalating, with the reason the
// engine is parking the task.
func (r *JobRun) stageBlocked(ctx context.Context) error {
	return r.RunStage(ctx, JobStageBlocked, func(hookCtx context.Context) error {
		return r.hooks.Blocked(hookCtx, r.Context(), BlockReason{
			Kind:       TaskBlockKindEscalated,
			Message:    r.escalationReason,
			Iterations: r.iteration,
		})
	})
}

// discard runs Discard once the job is over, on a context that survives the
// job's own being cancelled and is bounded by the discard stage timeout.
func (r *JobRun) discard(ctx context.Context, completed bool) error {
	if r.hooks == nil {
		return nil
	}
	return r.RunStage(context.WithoutCancel(ctx), JobStageDiscard, func(hookCtx context.Context) error {
		return r.hooks.Discard(hookCtx, r.Context(), completed)
	})
}

// =============================================================================
// Terminal outcomes
// =============================================================================

// Escalate records why the engine is parking the job's task. The reason
// reaches the Blocked hook when the job enters escalating.
func (r *JobRun) Escalate(reason string) {
	r.escalationReason = reason
}

// EscalationReason is the reason the job was escalated, empty when it was
// not.
func (r *JobRun) EscalationReason() string {
	return r.escalationReason
}

// Park records the runner's OWN judgement that the task must stop and wait
// for a person, and with which kind.
//
// IT IS NOT A FAILURE OF THE WORK AND MUST NOT BE ACCOUNTED AS ONE. A job
// whose terminal error is an ordinary failure tells the task's accounting
// only "this attempt did not deliver", and the accounting's answer to that is
// to dispatch the next attempt — correctly, because most failures are worth
// another try. A park is the case where the runner KNOWS another attempt
// changes nothing: the worker asked a question, or reached a decision it may
// not make. Requeueing that is how one unanswered question spends a whole
// budget in silence (go-quicbaas#787).
//
// The reason travels as prose because it is what a person reads; the kind
// travels as the closed enum the task store already validates. A kind outside
// that enum is IGNORED rather than stored, so a caller cannot park a task
// into a state nothing can render.
//
// faultClass ATTRIBUTES THE PARK, and is empty for the ordinary case: a park
// about the content of the work, which spends the attempt. A runner that
// proved the FACTORY was at fault — no workspace, a checkout that is not
// there, a worker reporting unusable tooling — names the operator fault class
// it proved, and the task's accounting then refunds the attempt rather than
// charging the work for the deployment (go-quicbaas#908).
//
// A CLASS OperatorFaultClass DOES NOT RECOGNISE IS DROPPED, and the park is
// kept without it. Dropping it charges the attempt, which is the direction
// that cannot hide a broken change; keeping the park is what stops the task
// being retried into the same wall. A caller therefore cannot refund an
// attempt by inventing a class, and cannot lose a park by mistyping one.
func (r *JobRun) Park(kind TaskBlockKind, reason, faultClass string) {
	if kind.Validate() != nil {
		return
	}
	if !OperatorFaultClass(faultClass) {
		faultClass = ""
	}
	// RE-RECORDING THE SAME PARK DOES NOT UN-ANNOUNCE IT. A resumed run
	// restores a park that may already have been surfaced and then re-arms
	// it here before trying to surface it again; replacing the value
	// outright would drop that fact, and a surfacing that then failed
	// LATE — the comment already on the issue, only the label move left —
	// would persist as unsurfaced and have the conclusion post a duplicate.
	//
	// The same park is the same kind and the same words. A park that
	// changed either is a different judgement and has not been announced.
	surfaced := r.park != nil && r.park.Surfaced &&
		r.park.Kind == kind && r.park.Message == reason
	r.park = &BlockReason{
		Kind: kind, Message: reason, FaultClass: faultClass,
		Iterations: r.iteration, Surfaced: surfaced,
	}
}

// Parked is the runner's park judgement, nil when it made none.
func (r *JobRun) Parked() *BlockReason {
	return r.park
}

// Satisfy records the runner's judgement that this task's work was ALREADY
// DONE before the attempt began — the issue is stale, or an earlier attempt of
// this task landed it.
//
// IT IS NOT A PARK AND NOT A FAILURE, which is the whole point: nobody has a
// decision to make and nothing is broken, so the task concludes rather than
// waiting for a person, and the attempt is not charged. Park and this are
// mutually exclusive by construction — a run that made a park judgement is
// waiting on somebody, and a run that reached here is not.
func (r *JobRun) Satisfy() { r.satisfied = true }

// Satisfied reports the judgement Satisfy recorded.
func (r *JobRun) Satisfied() bool { return r.satisfied }

// Supersede records the runner's judgement that this attempt's pull request
// was CLOSED WITHOUT MERGING, so the delivery is over (go-quicbaas#1054).
//
// Like Satisfy, it is not a park and not a failure. A person closed the pull
// request, so no worker round can change the outcome and nobody else has a
// decision to make. The task completes and concludes `superseded`.
func (r *JobRun) Supersede() { r.superseded = true }

// Superseded reports the judgement Supersede recorded.
func (r *JobRun) Superseded() bool { return r.superseded }

// ParkSurfaced records that the park has reached the person it is for: the
// comment is on the issue and the label has moved.
//
// IT IS CALLED AFTER THE POST, NEVER BEFORE. The whole value of the flag is
// that it distinguishes an announcement that happened from one that was
// attempted, so a caller that set it alongside Park would be asserting the
// thing it is supposed to be measuring.
func (r *JobRun) ParkSurfaced() {
	if r.park == nil || r.park.Surfaced {
		return
	}
	r.park.Surfaced = true
	// PUBLISHED AND PERSISTED HERE, not left for a later snapshot. The
	// window between the comment landing and the next snapshot is one in
	// which the durable record still says the park was never announced —
	// and a death in it makes RecordJobDeath post the comment a second
	// time. The duplicate is a trade worth making when the surfacing really
	// did fail; on this path it would be a duplicate for no reason at all.
	published := r.republishPark()
	if published != nil && r.persistFn != nil {
		persisted := *published
		r.persistFn(&persisted)
	}
}

// republishPark swaps the published status for one carrying the park's
// current state, and answers what it published. Nil when the job has
// published nothing yet, which is a job that cannot have surfaced anything
// either.
//
// It is the same compare-and-swap republishFanout uses, for the same reason:
// the published status is immutable, so a change to it is a new value
// swapped in rather than a write through a pointer somebody is reading.
func (r *JobRun) republishPark() *JobStatus {
	for {
		current, ok := r.status.Load().(*JobStatus)
		if !ok || current == nil {
			return nil
		}
		updated := *current
		updated.Park = r.park.clone()
		updated.UpdatedAt = time.Now().UnixNano()
		if r.status.CompareAndSwap(current, &updated) {
			return &updated
		}
	}
}

// Fail writes the job's terminal failure: the class the caller assigns, or
// the engine's own classification of err when class is empty.
func (r *JobRun) Fail(class string, err error) {
	if err == nil {
		return
	}
	if class == "" {
		r.recordFailure(err)
	} else {
		r.failureClass = class
		r.failureMessage = err.Error()
	}
	r.snapshot(JobStateFailed)
	r.emitTransition(JobStateFailed)
}

// recordFailure classifies the job's terminal error so the status it writes
// carries a class rather than prose.
func (r *JobRun) recordFailure(err error) {
	if err == nil {
		return
	}
	r.failureClass, r.fault = classifyJobFailure(err)
	r.failureMessage = err.Error()
}

// FailureClass is how the job's terminal failure was classified, empty when
// the job did not fail.
func (r *JobRun) FailureClass() string {
	return r.failureClass
}

// LeaseSettled is closed once the run has taken its lease, been refused one,
// or ended before asking.
func (r *JobRun) LeaseSettled() <-chan struct{} {
	return r.leaseSettled
}

// settleLease closes LeaseSettled; every call after the first is a no-op.
func (r *JobRun) settleLease() {
	if r.leaseSettled == nil {
		return
	}
	r.leaseSettledOnce.Do(func() { close(r.leaseSettled) })
}
