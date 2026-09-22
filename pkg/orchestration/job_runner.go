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
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// The JobRunner seam
// =============================================================================
//
// A JOB HAS ONE ENVELOPE AND MANY SHAPES. The envelope — the lease, the
// heartbeat, the accounting, the reaper, the events, the sessions and the
// recovery — is the engine's, identical for every job it runs. The SHAPE of a
// job, the sequence of states it moves through and what happens in each, is
// the runner's.
//
// The engine's own shape is a lead agent decomposing a description into a DAG
// of work items executed by workers: that is ControlLoop, registered here as
// the "workflow" runner. A delivery pipeline is a different shape — no lead,
// no decomposition, one persistent worker — and it is a different runner over
// the same envelope, not a second engine.
//
// A runner declares its states so the consumers that used to hardcode the
// engine's thirteen — the stall detector's thresholds, the UI's stepper —
// read them from the registry instead.

// JobRunnerWorkflow is the name of the engine's own runner: a lead agent
// decomposing work into a DAG executed by workers.
const JobRunnerWorkflow = "workflow"

// JobStateSpec declares one state a runner drives a job through.
type JobStateSpec struct {
	// State is the state itself, as it appears on JobStatus.
	State JobState

	// Label is how a consumer names the state to a person.
	Label string

	// SessionBacked says an agent session is doing the work in this state.
	// The stall detector ends a job past its bound in such a state with no
	// addressable session; a state that is not session-backed is only ever
	// reported.
	SessionBacked bool

	// IdleThreshold is how long a job may sit in this state before the wait
	// is worth reporting. It is READ BY THE STALL DETECTOR — see
	// DefaultJobIdleThresholds, which is this field over every registered
	// runner and nothing else.
	//
	// Zero means the runner declined to bound the state, and it takes
	// DefaultJobIdleThreshold. A runner that shares a state with another
	// runner does not have to restate the bound: the merge in
	// mergeJobStateSpec keeps the longest anyone declared.
	IdleThreshold time.Duration

	// OffProjectBudget says a job in this state holds no worker, so it does
	// not spend one of its project's Quotas.MaxConcurrency slots while it is
	// here. It is the per-state form of JobLeasePolicy.OffProjectBudget, for
	// a runner whose jobs spend a slot in SOME states and not others: an
	// sdlc job waiting in reviewing on a forge runs no worker turn, and
	// holding a slot for that wait kept a project's next task pending for
	// the whole CI pipeline and review (go-quicbaas#978).
	//
	// The envelope reads it on every transition (JobRun.SetState). Entering
	// such a state moves the job's lease off the budget; leaving one for a
	// state that spends a slot takes the slot back THROUGH THE PROJECT'S
	// ADMISSION, and the transition WAITS while the project is full rather
	// than failing. The lease itself is held throughout — it is what keeps
	// one job per task — so only the budget moves.
	//
	// It is the RUNNER's answer and nothing else's: mergeJobStateSpec does
	// not merge it, because another runner's job in a state of the same name
	// may be doing work in it.
	OffProjectBudget bool
}

// JobRunner owns the state sequence of one shape of job. The engine keeps
// the envelope; the runner decides what the job does.
type JobRunner interface {
	// Name is what JobConfig.Runner selects the runner by.
	Name() string

	// States are the runner's non-terminal states in order; the first is
	// the state a fresh job enters.
	States() []JobStateSpec

	// Validate refuses a configuration this runner cannot drive, before
	// any goroutine or lease exists.
	Validate(ctx context.Context, cfg *JobConfig) error

	// Run drives the job to a terminal state. A nil return completes the
	// job; an error fails it.
	Run(ctx context.Context, job *JobRun) error
}

// JobResumer is the optional half of a runner: where an interrupted job
// restarts. A runner that does not implement it restarts at States()[0].
type JobResumer interface {
	ResumeState(interrupted JobState) JobState
}

// JobLeasePolicy is the optional half of a runner that says how the ENVELOPE
// should lease its jobs. A runner that does not implement it takes the
// envelope's own answer, which is correct for every runner whose jobs are
// attempts at a task.
//
// It exists because two facts about a lease are the RUNNER's and nothing
// else's, and both of them were previously only derivable for task-bound
// work:
//
//   - What the job is mutually exclusive WITH. The envelope names a
//     task-bound job's lease after the task's source reference, so two
//     attempts at one issue contend. A job bound to no task falls back to its
//     own job id, which contends with NOTHING — correct for a one-off, and
//     wrong for a runner whose jobs are per-subject singletons, where it
//     means every duplicate trigger runs a second job that does the same work
//     and writes a competing answer.
//   - Whether the job spends the project's WORKER budget.
//     Quotas.MaxConcurrency bounds how much of a project's work may run at
//     once, and a runner that is not doing that work — that reads a forge and
//     writes a verdict — must not queue behind it.
//
// Both are declared here rather than set on JobConfig by whoever starts the
// job, because they are properties of the SHAPE of the work and would
// otherwise have to be restated correctly at every call site that starts one.
type JobLeasePolicy interface {
	// LeaseSubject names the work a job of this runner holds, which is what
	// its lease serializes on. An empty answer takes the envelope's.
	LeaseSubject(cfg *JobConfig) string

	// OffProjectBudget says a job of this runner does not count against the
	// project's Quotas.MaxConcurrency, in either direction: it is admitted
	// while the budget is full, and it does not fill the budget itself.
	OffProjectBudget() bool
}

// runnerLeaseSubject is the subject a runner names for this job, or empty
// when it names none.
func runnerLeaseSubject(runner JobRunner, cfg *JobConfig) string {
	policy, ok := runner.(JobLeasePolicy)
	if !ok || policy == nil {
		return ""
	}
	return strings.TrimSpace(policy.LeaseSubject(cfg))
}

// runnerOffProjectBudget reports whether a runner's jobs are exempt from the
// project's worker budget.
func runnerOffProjectBudget(runner JobRunner) bool {
	policy, ok := runner.(JobLeasePolicy)
	if !ok || policy == nil {
		return false
	}
	return policy.OffProjectBudget()
}

// runnerBudgetStates answers, per declared state, whether a job of this
// runner is off its project's budget there. Nil means the runner's jobs never
// move on or off the budget: a runner with no states to answer for, or one
// whose every job is off it (JobLeasePolicy.OffProjectBudget), which no state
// can put back on.
func runnerBudgetStates(runner JobRunner) map[JobState]bool {
	if runner == nil || runnerOffProjectBudget(runner) {
		return nil
	}
	states := runner.States()
	offBudget := make(map[JobState]bool, len(states))
	for _, spec := range states {
		offBudget[spec.State] = spec.OffProjectBudget
	}
	return offBudget
}

// =============================================================================
// Sentinel and typed errors
// =============================================================================

var (
	// ErrJobRunnerRegistration indicates a runner could not be registered.
	ErrJobRunnerRegistration = errors.New("job runner: registration refused")

	// ErrJobRunnerAlreadyRegistered indicates a runner name is taken.
	ErrJobRunnerAlreadyRegistered = errors.New("job runner: already registered")

	// ErrUnknownJobRunner indicates a job named a runner nothing registered.
	ErrUnknownJobRunner = errors.New("job runner: not registered")
)

// JobRunnerRegistrationError reports a registration the registry refused for
// a reason other than a duplicate name.
type JobRunnerRegistrationError struct {
	Name    string
	Message string
}

// Error implements the error interface.
func (e *JobRunnerRegistrationError) Error() string {
	if e.Name == "" {
		return ErrJobRunnerRegistration.Error() + ": " + e.Message
	}
	return ErrJobRunnerRegistration.Error() + ": " + e.Name + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *JobRunnerRegistrationError) Unwrap() error {
	return ErrJobRunnerRegistration
}

// JobRunnerAlreadyRegisteredError reports a second registration under a name
// that is already taken.
type JobRunnerAlreadyRegisteredError struct {
	Name string
}

// Error implements the error interface.
func (e *JobRunnerAlreadyRegisteredError) Error() string {
	return ErrJobRunnerAlreadyRegistered.Error() + ": " + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *JobRunnerAlreadyRegisteredError) Unwrap() error {
	return ErrJobRunnerAlreadyRegistered
}

// UnknownJobRunnerError reports a job naming a runner nothing registered.
type UnknownJobRunnerError struct {
	Name string
}

// Error implements the error interface.
func (e *UnknownJobRunnerError) Error() string {
	return ErrUnknownJobRunner.Error() + ": " + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownJobRunnerError) Unwrap() error {
	return ErrUnknownJobRunner
}

// =============================================================================
// Registry
// =============================================================================

// JobRunnerRegistry holds runners by name. Registration is append-only.
type JobRunnerRegistry struct {
	mu     sync.RWMutex
	byName map[string]JobRunner
}

// NewJobRunnerRegistry creates an empty registry.
func NewJobRunnerRegistry() *JobRunnerRegistry {
	return &JobRunnerRegistry{byName: make(map[string]JobRunner)}
}

// Register adds a runner under the name it reports.
func (r *JobRunnerRegistry) Register(runner JobRunner) error {
	if runner == nil {
		return &JobRunnerRegistrationError{Message: "runner must not be nil"}
	}
	name := runner.Name()
	if name == "" {
		return &JobRunnerRegistrationError{Message: "runner name must not be empty"}
	}
	if len(runner.States()) == 0 {
		return &JobRunnerRegistrationError{Name: name, Message: "runner must declare at least one state"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[name]; exists {
		return &JobRunnerAlreadyRegisteredError{Name: name}
	}
	r.byName[name] = runner
	return nil
}

// Lookup returns the runner registered under name.
func (r *JobRunnerRegistry) Lookup(name string) (JobRunner, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runner, ok := r.byName[name]
	return runner, ok
}

// All returns every registered runner, ordered by name so two reads of the
// same registry serialize identically.
func (r *JobRunnerRegistry) All() []JobRunner {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	runners := make([]JobRunner, 0, len(names))
	for _, name := range names {
		runners = append(runners, r.byName[name])
	}
	return runners
}

// jobRunners is the engine's registry: the one a module registers into and
// the one JobConfig.Runner is resolved against.
var jobRunners = NewJobRunnerRegistry()

// RegisterJobRunner registers a runner in the engine's registry under the
// name a job selects it by.
func RegisterJobRunner(runner JobRunner) error {
	return jobRunners.Register(runner)
}

// LookupJobRunner returns the engine-registered runner under name.
func LookupJobRunner(name string) (JobRunner, bool) {
	return jobRunners.Lookup(name)
}

// JobRunners returns every registered runner, ordered by name.
func JobRunners() []JobRunner {
	return jobRunners.All()
}

// KnownJobStates is every state any registered runner declares, in registry
// order and first-declaration order within a runner, with a state two
// runners share listed ONCE AND MERGED. It is what the stall detector's
// thresholds and the UI's stepper read instead of keeping their own copy of
// the engine's states.
//
// See mergeJobStateSpec for what "merged" means and why each field merges
// the way it does.
func KnownJobStates() []JobStateSpec {
	return knownJobStatesIn(jobRunners)
}

// knownJobStatesIn is KnownJobStates over an explicit registry, so the merge
// can be exercised against a registry a test owns rather than the engine's
// process-wide one.
func knownJobStatesIn(registry *JobRunnerRegistry) []JobStateSpec {
	var specs []JobStateSpec
	at := make(map[JobState]int)
	for _, runner := range registry.All() {
		for _, spec := range runner.States() {
			if i, exists := at[spec.State]; exists {
				specs[i] = mergeJobStateSpec(specs[i], spec)
				continue
			}
			at[spec.State] = len(specs)
			specs = append(specs, spec)
		}
	}
	return specs
}

// mergeJobStateSpec folds a later runner's declaration of a state into the
// one already recorded. ONE STATE HAS ONE ENVELOPE-WIDE ANSWER because the
// stall detector reads JobStatus.State, so the merge has to be the safe
// reading of every declaration rather than whichever runner sorted first:
//
//   - SessionBacked is a disjunction. A state one runner drives with a live
//     session is session-backed; taking the first declaration instead would
//     silently stop the detector ending a stalled session-backed job the
//     moment another runner declared the same state without a session.
//   - IdleThreshold takes the LONGEST declared bound. A shorter one would
//     report — and, in a session-backed state, end — the other runner's job
//     before its own runner considers it late, and ending early is the
//     expensive direction.
//   - Label takes the first non-empty, which is registry order: a state two
//     runners name differently is named by the first that names it at all.
func mergeJobStateSpec(into, from JobStateSpec) JobStateSpec {
	into.SessionBacked = into.SessionBacked || from.SessionBacked
	if from.IdleThreshold > into.IdleThreshold {
		into.IdleThreshold = from.IdleThreshold
	}
	if into.Label == "" {
		into.Label = from.Label
	}
	return into
}

// ResolveJobRunnerName maps an empty runner name onto the engine's own
// runner. A job that names no runner is a workflow job.
func ResolveJobRunnerName(name string) string {
	if name == "" {
		return JobRunnerWorkflow
	}
	return name
}

// ValidateJobRunnerName checks that name is empty or names a registered
// runner. It is what a transport calls before a job is started.
func ValidateJobRunnerName(name string) error {
	if name == "" {
		return nil
	}
	if _, ok := jobRunners.Lookup(name); !ok {
		return &UnknownJobRunnerError{Name: name}
	}
	return nil
}

// resumeStateFor is where an interrupted job restarts: the runner's own
// answer when it implements JobResumer, its first state otherwise.
func resumeStateFor(runner JobRunner, interrupted JobState) JobState {
	if resumer, ok := runner.(JobResumer); ok {
		return resumer.ResumeState(interrupted)
	}
	return entryStateFor(runner)
}

// entryStateFor is the state a fresh job of this runner enters.
func entryStateFor(runner JobRunner) JobState {
	states := runner.States()
	if len(states) == 0 {
		return JobStateInitializing
	}
	return states[0].State
}

// =============================================================================
// The engine's own runner
// =============================================================================

// workflowRunner is the lead-driven shape: a lead agent decomposes the job's
// description into a DAG of work items that workers execute. It is stateless
// — the state lives on the JobRun and on the ControlLoop it builds per run.
type workflowRunner struct{}

// Name implements JobRunner.
func (workflowRunner) Name() string { return JobRunnerWorkflow }

// States implements JobRunner: the lead-driven state sequence.
func (workflowRunner) States() []JobStateSpec {
	// The bounds are declared here rather than in a table beside the stall
	// detector because THE RUNNER IS THE ONLY THING THAT KNOWS WHAT ITS
	// STATES WAIT FOR. They are ordered by that: minutes where the engine
	// itself is the only thing that has to act, hours where an agent is
	// doing the work.
	return []JobStateSpec{
		{State: JobStateInitializing, Label: "Initializing", IdleThreshold: 10 * time.Minute},
		{State: JobStateDecomposing, Label: "Decomposing", IdleThreshold: 30 * time.Minute},
		{State: JobStateScheduling, Label: "Scheduling", IdleThreshold: 10 * time.Minute},
		{State: JobStateAssigning, Label: "Assigning", IdleThreshold: 10 * time.Minute},
		{State: JobStateExecuting, Label: "Executing", SessionBacked: true, IdleThreshold: 3 * time.Hour},
		{State: JobStateValidating, Label: "Validating", IdleThreshold: 45 * time.Minute},
		{State: JobStateReviewing, Label: "Reviewing", SessionBacked: true, IdleThreshold: 30 * time.Minute},
		{State: JobStateRedelegating, Label: "Re-delegating", IdleThreshold: 10 * time.Minute},
		{State: JobStateCompleting, Label: "Completing", IdleThreshold: 10 * time.Minute},
		{State: JobStateCleanup, Label: "Cleanup", IdleThreshold: 10 * time.Minute},
		{State: JobStateEscalating, Label: "Escalating", IdleThreshold: 10 * time.Minute},
	}
}

// Validate implements JobRunner: a lead has nothing to decompose without a
// description.
func (workflowRunner) Validate(_ context.Context, cfg *JobConfig) error {
	if cfg == nil {
		return &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "job config must not be nil",
		}
	}
	if cfg.Description == "" {
		return &AgentServiceError{
			Sentinel: ErrTaskRequired,
			Message:  "job description is required",
		}
	}
	return nil
}

// ResumeState implements JobResumer.
func (workflowRunner) ResumeState(interrupted JobState) JobState {
	return resolveResumeState(interrupted)
}

// Run implements JobRunner: a fresh ControlLoop drives the job's states.
func (workflowRunner) Run(ctx context.Context, job *JobRun) error {
	cl := NewControlLoop(job.Agents, job.Logger)
	cl.SetWorkflow(job.Workflow)
	return cl.RunJob(ctx, job)
}

func init() {
	if err := RegisterJobRunner(workflowRunner{}); err != nil {
		panic("orchestration: the engine's own job runner could not be registered: " + err.Error())
	}
}
