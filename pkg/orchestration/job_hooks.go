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

package orchestration

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// Job lifecycle hooks — the moments a module attaches to
// =============================================================================
//
// A job passes through five generic moments at which something outside the
// engine may need to act: before any session runs (Prepare), when the work is
// ready to be judged (Reviewable), when it has been accepted (Complete), when
// the job is over for any reason (Discard), and when the engine gives up and
// parks the task for a person (Blocked). JobLifecycleHooks is that set, fixed
// at five: a sixth moment is a design discussion, not a registration.
//
// The engine calls each moment at exactly one place in the control loop, on
// the state the moment belongs to, and treats a hook error as a job failure
// with FailureClass set — never as something to log and continue past. Which
// set of hooks a job runs is the Workflow's decision (WorkflowSettings.Hooks
// names a registered set); a workflow that names none runs none.

// JobStage names a lifecycle moment. It is what JobStatus.Stage reports and
// what a stage JobEvent carries.
type JobStage string

const (
	// JobStagePrepare runs on initializing, before any session spawns. The
	// hook returns the directory every worker session is spawned into.
	JobStagePrepare JobStage = "prepare"

	// JobStageReviewable runs on entering reviewing: the work is done and
	// validated, and may be published for judgement.
	JobStageReviewable JobStage = "reviewable"

	// JobStageComplete runs on completing: the review accepted the work.
	JobStageComplete JobStage = "complete"

	// JobStageDiscard runs once the job is over, completed or not, on a
	// context detached from the job's own so a cancelled job still cleans up.
	JobStageDiscard JobStage = "discard"

	// JobStageBlocked runs on escalating: the engine is parking the task for
	// a person.
	JobStageBlocked JobStage = "blocked"
)

// FailureClassHook is the JobStatus.FailureClass of a job a lifecycle hook
// failed.
const FailureClassHook = "hook"

// BlockReason is what the Blocked hook is told about why the engine parked
// the task.
type BlockReason struct {
	// Kind classifies the park; the engine's own escalation is
	// TaskBlockKindEscalated.
	Kind TaskBlockKind

	// Message is the human-readable reason.
	Message string

	// Iterations is how many review rounds the job ran before parking.
	Iterations int

	// FaultClass attributes the park: the operator fault class the runner
	// proved, or empty when the park is a judgement about THE WORK.
	//
	// EMPTY IS THE ORDINARY CASE and it means charged. A worker that asked a
	// question or reached a decision it may not make has attempted the work
	// and stopped on its content; the attempt is spent and the task waits for
	// a person. A park carrying a class says something different — the
	// factory could not present the work at all — and the accounting refunds
	// it (applyConclusion) and names the class on the block so an operator
	// reads the cause and a re-label after the repair releases the source.
	//
	// Only a class OperatorFaultClass recognises reaches here: JobRun.Park
	// drops any other, so a caller cannot refund an attempt by inventing a
	// class.
	FaultClass string

	// Surfaced records that the PARK HAS ALREADY REACHED THE PERSON it is
	// for — the runner posted it and moved the label before the job ended.
	//
	// It is meaningful only on the durable record (JobStatus.Park), and the
	// Blocked moment ignores it: that moment is the thing that DOES the
	// surfacing, so it cannot be told the answer in advance.
	//
	// WHAT READS IT is the task's conclusion. A park the run announced needs
	// no second comment; a park whose announcement FAILED needs one badly,
	// and that is precisely the case where the run cannot make it. Without
	// this flag both look identical to the conclusion, and the safe-looking
	// choice — stay silent, the runner posted it — is silent exactly when
	// the runner did not.
	Surfaced bool
}

// clone copies the reason so a published status never shares the mutable
// value the run holds. Nil-safe: a job that parked nothing clones nothing.
//
// THE PUBLISHED STATUS IS IMMUTABLE, which is the whole reason this exists.
// JobRun.Status hands the published pointer to concurrent readers, so a
// status carrying the RUN's own *BlockReason would be a value the run can
// still write to — and marking a park surfaced would then race every reader
// holding that status.
func (b *BlockReason) clone() *BlockReason {
	if b == nil {
		return nil
	}
	copied := *b
	return &copied
}

// JobContext is the view of a running job the engine hands to lifecycle hooks
// and validators. It is built once per run and refreshed before each moment;
// hooks read it and may record the directory they prepared through Prepare's
// return value, never by writing into it.
type JobContext struct {
	JobID     string
	TenantID  string
	ProjectID string

	// TaskID and Attempt bind the job to the task it is an attempt at; both
	// are empty for a job started directly.
	TaskID  string
	Attempt int

	// Iteration is the current review round.
	Iteration int

	Config   *JobConfig
	Workflow *WorkflowDefinition

	// Prompts is the prompt set the job was launched with; nil for a runner
	// that renders none. A validator that prompts an agent renders from it.
	Prompts *prompts.ResolvedSet

	// WorkDir is the directory Prepare returned; empty before Prepare ran
	// or when the workflow runs no hooks. It is a DAEMON directory.
	WorkDir string

	// Container is the containerized worker session the job's work happens
	// in, nil for a job that spawned none. A validator that judges the WORK
	// — a build, a test run — runs its command through it, in the checkout
	// the container's own entrypoint made.
	Container *JobContainer

	// Artifacts are the task outputs gathered for validation and review.
	Artifacts []*TaskResult

	// Memory is the job's accumulated context across iterations.
	Memory *JobMemory

	// Agents is the service a validator prompts an agent through.
	Agents AgentService

	// Guard is the operator's command allowlist.
	Guard ExecutionGuard
}

// JobLifecycleHooks is the set of five moments a module attaches to.
type JobLifecycleHooks interface {
	// Prepare runs on initializing, before any session spawns, and returns
	// the directory worker sessions are spawned into. Empty leaves the
	// workflow's own workspace settings in force.
	Prepare(ctx context.Context, job *JobContext) (workDir string, err error)

	// Reviewable runs on entering reviewing.
	Reviewable(ctx context.Context, job *JobContext) error

	// Complete runs on completing.
	Complete(ctx context.Context, job *JobContext) error

	// Discard runs on cleanup, on a detached context with its own timeout;
	// completed says whether the job reached completed.
	Discard(ctx context.Context, job *JobContext, completed bool) error

	// Blocked runs on escalating, with the reason the task is parked.
	Blocked(ctx context.Context, job *JobContext, reason BlockReason) error
}

// =============================================================================
// Registry
// =============================================================================

// JobHooksRegistry holds hook sets by name. Registration is append-only.
type JobHooksRegistry struct {
	mu     sync.RWMutex
	byName map[string]JobLifecycleHooks
}

// NewJobHooksRegistry creates an empty registry.
func NewJobHooksRegistry() *JobHooksRegistry {
	return &JobHooksRegistry{byName: make(map[string]JobLifecycleHooks)}
}

// Register adds hooks under name.
func (r *JobHooksRegistry) Register(name string, hooks JobLifecycleHooks) error {
	if name == "" {
		return &JobHooksRegistrationError{Message: "name must not be empty"}
	}
	if hooks == nil {
		return &JobHooksRegistrationError{Name: name, Message: "hooks must not be nil"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[name]; exists {
		return &JobHooksAlreadyRegisteredError{Name: name}
	}
	r.byName[name] = hooks
	return nil
}

// Lookup returns the hooks registered under name.
func (r *JobHooksRegistry) Lookup(name string) (JobLifecycleHooks, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	hooks, ok := r.byName[name]
	return hooks, ok
}

// jobHooks is the engine's registry: the one a module registers into and the
// one a workflow's Hooks setting is resolved against.
var jobHooks = NewJobHooksRegistry()

// RegisterJobHooks registers a hook set in the engine's registry under the
// name a workflow selects it by.
func RegisterJobHooks(name string, hooks JobLifecycleHooks) error {
	return jobHooks.Register(name, hooks)
}

// LookupJobHooks returns the engine-registered hook set under name.
func LookupJobHooks(name string) (JobLifecycleHooks, bool) {
	return jobHooks.Lookup(name)
}

// ValidateJobHooksName checks that name is empty or names a registered hook
// set. It is what workflow validation calls for WorkflowSettings.Hooks.
func ValidateJobHooksName(name string) error {
	if name == "" {
		return nil
	}
	if _, ok := jobHooks.Lookup(name); !ok {
		return &UnknownJobHooksError{Name: name}
	}
	return nil
}

// =============================================================================
// Stage timeouts and the lifecycle policy
// =============================================================================

// StageTimeouts bounds each lifecycle moment individually. A job's own
// context has no deadline, so an unbounded hook that hangs would hold the
// job forever with nothing naming which moment was holding it.
type StageTimeouts struct {
	Prepare    time.Duration
	Reviewable time.Duration
	Complete   time.Duration
	Discard    time.Duration
	Blocked    time.Duration
}

// DefaultStageTimeouts is the engine's defaults layer for the bounds; a zero
// field in a configured StageTimeouts takes the value here.
var DefaultStageTimeouts = StageTimeouts{
	Prepare:    10 * time.Minute,
	Reviewable: 10 * time.Minute,
	Complete:   10 * time.Minute,
	Discard:    2 * time.Minute,
	Blocked:    1 * time.Minute,
}

// WithDefaults fills every zero field from DefaultStageTimeouts.
func (t StageTimeouts) WithDefaults() StageTimeouts {
	out := t
	if out.Prepare <= 0 {
		out.Prepare = DefaultStageTimeouts.Prepare
	}
	if out.Reviewable <= 0 {
		out.Reviewable = DefaultStageTimeouts.Reviewable
	}
	if out.Complete <= 0 {
		out.Complete = DefaultStageTimeouts.Complete
	}
	if out.Discard <= 0 {
		out.Discard = DefaultStageTimeouts.Discard
	}
	if out.Blocked <= 0 {
		out.Blocked = DefaultStageTimeouts.Blocked
	}
	return out
}

// For returns the bound for a stage, a zero bound taking that stage's
// default; an unknown stage takes the discard bound, the shortest, so
// nothing runs longer for being unnamed.
func (t StageTimeouts) For(stage JobStage) time.Duration {
	filled := t.WithDefaults()
	bounds := map[JobStage]time.Duration{
		JobStagePrepare:    filled.Prepare,
		JobStageReviewable: filled.Reviewable,
		JobStageComplete:   filled.Complete,
		JobStageDiscard:    filled.Discard,
		JobStageBlocked:    filled.Blocked,
	}
	if d, ok := bounds[stage]; ok {
		return d
	}
	return filled.Discard
}

// JobLifecyclePolicy is the operator's part of a job's lifecycle: the command
// allowlist the command validator enforces and the bound on each moment. It
// is built from daemon configuration and handed to every control loop.
type JobLifecyclePolicy struct {
	Guard         ExecutionGuard
	StageTimeouts StageTimeouts

	// AgentConfigDir is the configuration directory every Claude Code
	// session the engine spawns reads (CLAUDE_CONFIG_DIR), so a session's
	// settings are the engine's rather than the daemon host user's. Empty
	// leaves the backend's scrubbed environment as it is.
	AgentConfigDir string
}

// JobLifecyclePolicyProvider is satisfied by an agent service that carries
// the operator's policy, so a control loop can take it from the service it
// was built over without every construction site having to pass it.
type JobLifecyclePolicyProvider interface {
	JobLifecyclePolicy() JobLifecyclePolicy
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrJobHook indicates a lifecycle hook failed; the job fails with it.
	ErrJobHook = errors.New("orchestration: job lifecycle hook failed")

	// ErrJobHooksRegistration indicates a hook set could not be registered.
	ErrJobHooksRegistration = errors.New("job hooks: registration refused")

	// ErrJobHooksAlreadyRegistered indicates a hook set name is taken.
	ErrJobHooksAlreadyRegistered = errors.New("job hooks: already registered")
)

// JobHookError reports a lifecycle hook that failed. The job it belongs to
// is failed with FailureClassHook.
type JobHookError struct {
	Stage JobStage
	Hooks string
	JobID string
	Cause error
}

// Error implements the error interface.
func (e *JobHookError) Error() string {
	msg := ErrJobHook.Error() + ": stage=" + string(e.Stage) + " hooks=" + e.Hooks + " job=" + e.JobID
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *JobHookError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrJobHook}
	}
	return []error{ErrJobHook, e.Cause}
}

// JobHooksRegistrationError reports a registration the registry refused for
// a reason other than a duplicate name.
type JobHooksRegistrationError struct {
	Name    string
	Message string
}

// Error implements the error interface.
func (e *JobHooksRegistrationError) Error() string {
	if e.Name == "" {
		return ErrJobHooksRegistration.Error() + ": " + e.Message
	}
	return ErrJobHooksRegistration.Error() + ": " + e.Name + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *JobHooksRegistrationError) Unwrap() error {
	return ErrJobHooksRegistration
}

// JobHooksAlreadyRegisteredError reports a second registration under a name
// that is already taken.
type JobHooksAlreadyRegisteredError struct {
	Name string
}

// Error implements the error interface.
func (e *JobHooksAlreadyRegisteredError) Error() string {
	return ErrJobHooksAlreadyRegistered.Error() + ": " + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *JobHooksAlreadyRegisteredError) Unwrap() error {
	return ErrJobHooksAlreadyRegistered
}

// UnknownJobHooksError reports a workflow naming a hook set nothing
// registered. It is a workflow validation failure.
type UnknownJobHooksError struct {
	Name string
}

// Error implements the error interface.
func (e *UnknownJobHooksError) Error() string {
	return ErrWorkflowValidation.Error() + ": hooks: no registered job hooks named " + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownJobHooksError) Unwrap() error {
	return ErrWorkflowValidation
}
