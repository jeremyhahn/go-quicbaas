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
	"strconv"
	"strings"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	// TaskStatusPending indicates a task is awaiting assignment.
	TaskStatusPending TaskStatus = "pending"

	// TaskStatusClaimed indicates a task has been claimed by an agent.
	TaskStatusClaimed TaskStatus = "claimed"

	// TaskStatusInProgress indicates a task is actively being worked on.
	TaskStatusInProgress TaskStatus = "in_progress"

	// TaskStatusCompleted indicates a task has been finished successfully.
	TaskStatusCompleted TaskStatus = "completed"

	// TaskStatusFailed indicates a task has failed.
	TaskStatusFailed TaskStatus = "failed"

	// TaskStatusCancelled indicates a task has been cancelled.
	TaskStatusCancelled TaskStatus = "cancelled"

	// TaskStatusBlocked indicates a task is parked and will not be dispatched
	// until it is readmitted. A task blocks because a person must answer
	// something, because it is waiting on an external condition, or because
	// the engine escalated it; BlockKind says which and BlockedReason says
	// why. ReadmitTask returns it to pending; FailTask retires it.
	TaskStatusBlocked TaskStatus = "blocked"
)

// taskStatuses is every task status, in lifecycle order. It is the ONE list
// the enum is validated and documented from, so a status added above is
// filterable the moment it exists rather than when someone remembers a second
// list.
var taskStatuses = []TaskStatus{
	TaskStatusPending,
	TaskStatusClaimed,
	TaskStatusInProgress,
	TaskStatusCompleted,
	TaskStatusFailed,
	TaskStatusCancelled,
	TaskStatusBlocked,
}

// TaskStatuses returns every task status, in lifecycle order.
func TaskStatuses() []TaskStatus {
	return append([]TaskStatus(nil), taskStatuses...)
}

// TaskStatusNames returns every task status as a string, for the message a
// refused filter carries back to the caller.
func TaskStatusNames() []string {
	names := make([]string, 0, len(taskStatuses))
	for _, status := range taskStatuses {
		names = append(names, string(status))
	}
	return names
}

// Valid reports whether the status is one the engine has.
func (s TaskStatus) Valid() bool {
	for _, known := range taskStatuses {
		if s == known {
			return true
		}
	}
	return false
}

// TaskSourceKind names the place a task originated from. It is provenance,
// not behaviour: the engine never branches on it beyond validating it and
// using it as an index segment that keeps one origin from being adopted as
// two tasks.
//
// THE SET IS OPEN. The engine declares only the kinds it produces itself
// (scheduler, operator, job); every other kind is declared by the TaskSource
// that emits it, at registration (RegisterTaskSource). A kind is therefore
// something a source says about its own tasks, never something the engine
// enumerates on a source's behalf.
type TaskSourceKind string

const (
	// TaskSourceKindScheduler is a task raised by a schedule.
	TaskSourceKindScheduler TaskSourceKind = "scheduler"

	// TaskSourceKindOperator is a task created directly by a person.
	TaskSourceKindOperator TaskSourceKind = "operator"

	// TaskSourceKindJob is a task raised by another job.
	TaskSourceKindJob TaskSourceKind = "job"
)

// builtinTaskSourceKinds are the kinds the engine itself emits.
var builtinTaskSourceKinds = map[TaskSourceKind]struct{}{
	TaskSourceKindScheduler: {},
	TaskSourceKindOperator:  {},
	TaskSourceKindJob:       {},
}

// Validate reports whether k is a built-in kind or one a registered
// TaskSource declared.
func (k TaskSourceKind) Validate() error {
	if _, ok := builtinTaskSourceKinds[k]; ok {
		return nil
	}
	if taskSources.kindRegistered(k) {
		return nil
	}
	return &InvalidTaskSourceKindError{Kind: k}
}

// TaskSourceRef records where a task came from. Kind and Ref together
// identify the origin uniquely within a tenant and project; the task store
// refuses a second task for the same origin. URL is informational.
type TaskSourceRef struct {
	Kind TaskSourceKind `json:"kind"`
	Ref  string         `json:"ref"`
	URL  string         `json:"url,omitempty"`
}

// Validate checks that the source names a known kind and a non-empty
// reference. A nil source is valid: it means the task has no recorded origin.
func (src *TaskSourceRef) Validate() error {
	if src == nil {
		return nil
	}
	if err := src.Kind.Validate(); err != nil {
		return err
	}
	if src.Ref == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task source ref must not be empty",
		}
	}
	return nil
}

// TaskBlockKind classifies why a task is blocked.
type TaskBlockKind string

const (
	// TaskBlockKindNeedsInfo means a person must supply something before the
	// task can proceed.
	TaskBlockKindNeedsInfo TaskBlockKind = "needs_info"

	// TaskBlockKindBlocked means the task waits on an external condition.
	TaskBlockKindBlocked TaskBlockKind = "blocked"

	// TaskBlockKindEscalated means the engine gave up on the task and handed
	// it to a person.
	TaskBlockKindEscalated TaskBlockKind = "escalated"
)

// taskBlockKinds is the closed enum, used for O(1) validation.
var taskBlockKinds = map[TaskBlockKind]struct{}{
	TaskBlockKindNeedsInfo: {},
	TaskBlockKindBlocked:   {},
	TaskBlockKindEscalated: {},
}

// Validate reports whether k is a member of the closed enum.
func (k TaskBlockKind) Validate() error {
	if _, ok := taskBlockKinds[k]; !ok {
		return &InvalidTaskBlockKindError{Kind: k}
	}
	return nil
}

// Task represents an orchestration work item managed by the task store.
type Task struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	TeamName     string            `json:"team_name"`
	Subject      string            `json:"subject"`
	Description  string            `json:"description"`
	Status       TaskStatus        `json:"status"`
	AssignedTo   string            `json:"assigned_to,omitempty"`
	CreatedBy    string            `json:"created_by,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
	Blocks       []string          `json:"blocks,omitempty"`
	Result       *TaskResult       `json:"result,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    int64             `json:"created_at"`
	UpdatedAt    int64             `json:"updated_at"`
	ClaimedAt    int64             `json:"claimed_at,omitempty"`
	CompletedAt  int64             `json:"completed_at,omitempty"`
	Version      uint64            `json:"version"`

	// AgentID is the fleet node the task's LATEST ATTEMPT ran on, empty
	// when it has had none or when the attempt ran in-process.
	//
	// DERIVED ON READ, NEVER PERSISTED. It is the job's fact, not the task's
	// — a copy written down here would be stale for every attempt after the
	// one that wrote it — so the paged listing resolves it and marshalTask
	// zeroes it, exactly as Account.Default is derived and zeroed. It is on
	// the record rather than beside it because the filter, the facet counts
	// and the UI column all read it through the same PageSpec.
	AgentID string `json:"agent_id,omitempty"`

	// ProjectID scopes this task to a registered project. When non-empty,
	// ClaimTask enforces the project's FleetNodeRoles access matrix.
	// Empty means the task is unscoped (legacy behaviour — allow all).
	ProjectID string `json:"project_id,omitempty"`

	// OutputArtifacts are the typed artifacts produced by this task.
	// Small artifacts (≤ InlineLimit) are stored inline; larger ones are
	// promoted to the KV store and tracked via ArtifactRef.
	OutputArtifacts []Artifact `json:"output_artifacts,omitempty"`

	// InputArtifacts are references to artifacts from upstream tasks that
	// this task reads as input. Downstream agents resolve refs to bytes
	// before execution.
	InputArtifacts []Artifact `json:"input_artifacts,omitempty"`

	// AutoSpawn indicates the task was created with immediate dispatch requested.
	// When true, the orchestrator attempted to spawn a one-shot job immediately
	// on task creation. False means the task entered the queue and waits for claim.
	AutoSpawn bool `json:"auto_spawn,omitempty"`

	// SpawnedJobID is populated when AutoSpawn succeeded and a job was dispatched.
	// Empty when AutoSpawn is false or when no matching agent was available at
	// creation time.
	SpawnedJobID string `json:"spawned_job_id,omitempty"`

	// SpawnedAt records when the auto-spawned job was dispatched.
	// Zero when SpawnedJobID is empty.
	SpawnedAt int64 `json:"spawned_at,omitempty"`

	// Source records where the task came from. Nil means no recorded origin.
	// When set, the store keeps a per-tenant, per-project index on
	// (Kind, Ref) so the same origin is never adopted twice.
	Source *TaskSourceRef `json:"source,omitempty"`

	// Priority orders tasks for dispatch; a higher value is dispatched first.
	Priority int `json:"priority,omitempty"`

	// PromptAppend is run-time prompt material every attempt at the task
	// carries to the sessions it spawns (JobConfig.PromptAppend).
	PromptAppend string `json:"prompt_append,omitempty"`

	// Attempts counts the jobs dispatched for this task so far.
	Attempts int `json:"attempts"`

	// AttemptBudget bounds Attempts. Zero means the engine default applies.
	AttemptBudget int `json:"attempt_budget,omitempty"`

	// EnvironmentFaults counts attempts that ended because of the
	// environment rather than the work; such attempts are refunded against
	// the budget.
	EnvironmentFaults int `json:"environment_faults"`

	// QuestionAttempts counts attempts that ended with the agent asking a
	// person a question rather than judging the work; such attempts are
	// refunded against the budget.
	//
	// SEPARATE FROM EnvironmentFaults BECAUSE THE OPERATOR IS TOLD WHICH IT
	// WAS. The blocked-comment renderer prints "N of them environment
	// faults, not charged", so folding questions into that counter would
	// describe a worker that stopped to ask the owner a ruling as a broken
	// deployment. Both are refunded; only one is the factory's fault.
	QuestionAttempts int `json:"question_attempts"`

	// ConsecutiveEnvironmentFaults counts environment faults since the last
	// attempt that got past the environment. It resets on any other outcome.
	ConsecutiveEnvironmentFaults int `json:"consecutive_environment_faults,omitempty"`

	// RetryNotBefore is a Unix-nanosecond instant before which the task must
	// not be dispatched again. Zero means it may be dispatched now.
	RetryNotBefore int64 `json:"retry_not_before,omitempty"`

	// BlockedReason says why the task is blocked. Set with Status blocked.
	BlockedReason string `json:"blocked_reason"`

	// BlockKind classifies the block. Set with Status blocked.
	BlockKind TaskBlockKind `json:"block_kind"`

	// BlockFaultClass names the environment fault class a block came from —
	// one of the classes classifyEnvironmentFault produces — and is empty for
	// an operator hold or an engine escalation that no fault classified. Set
	// with Status blocked and BlockKind blocked. It is what lets a re-adoption
	// tell a transient environment hold, which a person resolves and re-arms
	// by re-labelling, from a deliberate operator hold, which stays held.
	BlockFaultClass string `json:"block_fault_class"`

	// StatusChangedAt is the Unix-nanosecond instant of the last Status
	// transition.
	StatusChangedAt int64 `json:"status_changed_at,omitempty"`

	// Runner names the JobRunner that drives an attempt at this task. It is
	// resolved when the task is created and never empty afterwards, so a
	// task requeued after a failed attempt is re-dispatched by the same
	// runner that first ran it.
	Runner string `json:"runner,omitempty"`

	// Routing are the placement hints a dispatch reads. They are kept on the
	// record rather than only on the request that created it, so a requeue
	// dispatches under the same constraints as the first attempt.
	Routing TaskRouting `json:"routing,omitzero"`

	// DispatchReason says why the last dispatch of this task produced no
	// job. A successful dispatch clears it, so a non-empty value on a
	// pending task is the reason it is still waiting.
	DispatchReason string `json:"dispatch_reason,omitempty"`

	// ConclusionReason is the typed reason the task stopped, from the closed
	// vocabulary in task_conclusion.go. It is written by the same write that
	// concluded the task and cleared by a requeue or a readmit, so a
	// non-empty value always describes the record it is on.
	//
	// PERSISTED rather than re-derived: the reason is decided by
	// applyConclusion from a job outcome that is gone by the time anything
	// reads the task, and a reader guessing it back from the resting status
	// would confuse an abandoned attempt with an operator's hold — the two
	// differ only in what produced them. go-quicbaas#796.
	ConclusionReason TaskConclusionReason `json:"conclusion_reason"`

	// ReviewRoundBonus extends the review-round budget pkg/orchestration/sdlc's runner
	// checks (binding.MaxReviewRounds) by this many rounds for THIS task
	// only. It is how a person's "allow another round" — granted from the
	// attention inbox after a review that did not converge — reaches the
	// runner without changing the project's binding for every other task.
	//
	// BOUNDED (see MaxReviewRoundBonus) and ADDITIVE: it is never decreased,
	// so a task granted two extra rounds across two separate escalations
	// keeps both. It is NOT cleared by ReadmitTask — the round it bought is
	// spent by the runner reaching it, not by the readmit that unparked the
	// task for it.
	ReviewRoundBonus int `json:"review_round_bonus,omitempty"`
}

// MaxReviewRoundBonus bounds ReviewRoundBonus. A person may grant a task
// extra review rounds one at a time from the attention inbox; this is the
// ceiling past which "allow another round" is refused rather than turning
// into an unbounded budget nothing ever exhausts.
const MaxReviewRoundBonus = 5

// TaskRouting are the placement hints a dispatch reads when it decides where
// an attempt at a task runs: the backend that may run it, the host it is
// pinned to, and whether it needs a GPU. The zero value constrains nothing.
type TaskRouting struct {
	// Backend restricts the attempt to agents running this backend. Empty
	// means any backend.
	Backend string `json:"backend,omitempty"`

	// HostAffinity pins the attempt to an agent whose name or host matches.
	// Empty means any host.
	HostAffinity string `json:"host_affinity,omitempty"`

	// GpuRequired restricts the attempt to agents advertising "gpu".
	GpuRequired bool `json:"gpu_required,omitempty"`
}

// TaskResult contains the output of a completed task.
type TaskResult struct {
	Output   string            `json:"output,omitempty"`
	Files    []string          `json:"files,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// TaskFilter specifies criteria for listing tasks.
type TaskFilter struct {
	TeamName   string     `json:"team_name,omitempty"`
	Status     TaskStatus `json:"status,omitempty"`
	AssignedTo string     `json:"assigned_to,omitempty"`
	CreatedBy  string     `json:"created_by,omitempty"`
	Limit      int        `json:"limit,omitempty"`

	// ProjectID restricts the listing to tasks scoped to one project.
	ProjectID string `json:"project_id,omitempty"`

	// Statuses restricts the listing to tasks in any of the given statuses.
	// It composes with Status: a task must satisfy both when both are set.
	Statuses []TaskStatus `json:"statuses,omitempty"`

	// AutoSpawn restricts the listing to the tasks that asked for immediate
	// dispatch (true) or to the ones queued for a claim (false). Nil is "do
	// not narrow on it", which is why this is a pointer and not a bool: a
	// bool could not tell "queued only" from "no preference", and the Tasks
	// view offers all three. The narrowing belongs here rather than in the
	// client because the listing is paged — a client filtering a page of 25
	// answers "how many queued tasks are there" with a number it cannot know.
	AutoSpawn *bool `json:"auto_spawn,omitempty"`

	// JobID restricts the listing to the task whose LATEST ATTEMPT ran as
	// that job — the reverse of JobFilter.TaskID, and the lookup a job page
	// makes to name the task it is an attempt at.
	JobID string `json:"job_id,omitempty"`

	// AgentID restricts the listing to the tasks whose latest attempt ran on
	// one fleet node.
	//
	// THE AGENT IS THE JOB'S, NOT THE TASK'S. A task record names no agent —
	// AssignedTo is filled only when an agent CLAIMS a task, and the engine
	// dispatches most of them — so the answer lives on the attempt's job
	// (JobStatus.AgentID) and is resolved onto the task when the listing is
	// paged. See agentServiceImpl.ListTasksPage.
	AgentID string `json:"agent_id,omitempty"`

	// SourceKind and SourceRef restrict the listing to the task that holds
	// one source reservation: the answer to "which task adopted this issue".
	// They are set together; one without the other is refused.
	//
	// THE ANSWER COMES FROM THE RESERVATION INDEX, not a scan. A source is
	// reserved per project, so ProjectID names the scope the source is looked
	// up in — an empty ProjectID asks for the unscoped reservation — and is
	// part of the source's key rather than a facet when a source is set. A
	// source nobody holds lists nothing; it is not an error.
	SourceKind TaskSourceKind `json:"source_kind,omitempty"`
	SourceRef  string         `json:"source_ref,omitempty"`
}

// Source returns the source reservation the filter asks for, or nil when it
// asks for none.
func (f *TaskFilter) Source() *TaskSourceRef {
	if f == nil || (f.SourceKind == "" && f.SourceRef == "") {
		return nil
	}
	return &TaskSourceRef{Kind: f.SourceKind, Ref: f.SourceRef}
}

// TaskStore manages orchestration tasks backed by QuicBaaS KV store.
// When QuicBaaS runs in Raft cluster mode, tasks are automatically
// replicated across nodes. In standalone mode, tasks are stored in PebbleDB.
type TaskStore interface {
	// CreateTask creates a new task. Returns error if task ID already exists.
	CreateTask(ctx context.Context, task *Task) error

	// GetTask retrieves a task by ID within a tenant namespace.
	GetTask(ctx context.Context, tenantID, taskID string) (*Task, error)

	// UpdateTask updates an existing task. Uses CAS for conflict detection.
	UpdateTask(ctx context.Context, task *Task) error

	// DeleteTask removes a task.
	DeleteTask(ctx context.Context, tenantID, taskID string) error

	// ListTasks lists tasks matching the filter within a tenant. A record
	// that cannot be decoded is left out so it cannot hide the rest of the
	// work queue; a caller that decides anything from a task's ABSENCE must
	// use ListTaskRecords instead, which reports what it could not read.
	ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error)

	// ListTaskRecords lists tasks matching the filter within a tenant, as
	// ListTasks does, and names every record it could not decode. The
	// readable records are always returned; see TaskListing.
	ListTaskRecords(ctx context.Context, tenantID string, filter *TaskFilter) (*TaskListing, error)

	// ClaimTask atomically claims a task for an agent using compare-and-swap.
	// Returns error if task is already claimed by another agent.
	ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error

	// CompleteTask marks a task as completed with a result.
	CompleteTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error

	// FailTask marks a task as failed with a reason.
	FailTask(ctx context.Context, tenantID, taskID, reason string) error

	// BlockTask parks a pending, claimed or in-progress task with a kind and
	// a reason. A blocked task is not dispatched until ReadmitTask returns it
	// to pending; FailTask may retire it.
	BlockTask(ctx context.Context, tenantID, taskID string, kind TaskBlockKind, reason string) error

	// ReadmitTask returns a blocked task to pending, clearing its block,
	// assignment and retry hold so it can be claimed again.
	ReadmitTask(ctx context.Context, tenantID, taskID string) error

	// CloseBlockedTask completes a blocked task without readmitting it — an
	// operator ruling that no further work is needed. Any status other than
	// blocked is a TaskInvalidStateError; CompleteTask refuses a blocked task.
	CloseBlockedTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error

	// BumpReviewRoundBudget raises a blocked task's ReviewRoundBonus by one,
	// bounded by MaxReviewRoundBonus, and returns the resulting value. The
	// task must be blocked; any other status is a TaskInvalidStateError. It
	// does not readmit — see AgentService.BumpTaskReviewRoundBudget for the
	// verb the attention inbox's "allow another round" actually calls.
	BumpReviewRoundBudget(ctx context.Context, tenantID, taskID string) (int, error)

	// SubmitTaskArtifacts appends output artifacts to a task. Artifacts
	// larger than the configured InlineLimit are stored in the KV store and
	// the task record is updated with a kv_key Ref. At-most-once per name:
	// submitting an artifact whose Name already exists overwrites the prior value.
	SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []Artifact) error

	// GetTaskArtifact retrieves a named artifact from a task, resolving kv_key
	// Refs to their full byte content before returning.
	GetTaskArtifact(ctx context.Context, tenantID, taskID, name string) (*Artifact, error)

	// ListTaskArtifacts returns all output artifacts for a task without
	// resolving large Refs to their content (returns metadata + Ref only).
	ListTaskArtifacts(ctx context.Context, tenantID, taskID string) ([]Artifact, error)

	// DeleteTenantTasks deletes ALL tasks for a tenant (cascade delete).
	DeleteTenantTasks(ctx context.Context, tenantID string) error
}

// =============================================================================
// Task Store Sentinel Errors
// =============================================================================

var (
	// ErrTaskNotFound indicates a task was not found.
	ErrTaskNotFound = errors.New("task store: not found")

	// ErrTaskAlreadyExists indicates a task with the given ID already exists.
	ErrTaskAlreadyExists = errors.New("task store: task already exists")

	// ErrTaskAlreadyClaimed indicates another agent already claimed the task.
	ErrTaskAlreadyClaimed = errors.New("task store: task already claimed")

	// ErrTaskVersionConflict indicates a CAS conflict due to a stale version.
	ErrTaskVersionConflict = errors.New("task store: version conflict")

	// ErrTaskDependencyBlocked indicates a task has unresolved dependencies.
	ErrTaskDependencyBlocked = errors.New("task store: dependency blocked")

	// ErrTaskInvalidState indicates an invalid state transition.
	ErrTaskInvalidState = errors.New("task store: invalid state transition")

	// ErrTaskStoreInvalidInput indicates invalid input to the task store.
	ErrTaskStoreInvalidInput = errors.New("task store: invalid input")

	// ErrTaskStoreMarshal indicates a serialization or deserialization failure.
	ErrTaskStoreMarshal = errors.New("task store: marshal error")

	// ErrTaskSourceAlreadyAdopted indicates a task already exists for the
	// same source within the tenant and project.
	ErrTaskSourceAlreadyAdopted = errors.New("task store: source already adopted")
)

// =============================================================================
// Task Store Typed Errors
// =============================================================================

// TaskNotFoundError indicates a task was not found by ID.
type TaskNotFoundError struct {
	TenantID string
	TaskID   string
}

// Error implements the error interface.
func (e *TaskNotFoundError) Error() string {
	return ErrTaskNotFound.Error() + ": tenant=" + e.TenantID + " task=" + e.TaskID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskNotFoundError) Unwrap() error {
	return ErrTaskNotFound
}

// TaskAlreadyExistsError indicates a task with the given ID already exists.
type TaskAlreadyExistsError struct {
	TaskID string
}

// Error implements the error interface.
func (e *TaskAlreadyExistsError) Error() string {
	return ErrTaskAlreadyExists.Error() + ": " + e.TaskID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskAlreadyExistsError) Unwrap() error {
	return ErrTaskAlreadyExists
}

// TaskAlreadyClaimedError indicates another agent already claimed the task.
type TaskAlreadyClaimedError struct {
	TaskID       string
	ClaimedBy    string
	RequestingBy string
}

// Error implements the error interface.
func (e *TaskAlreadyClaimedError) Error() string {
	return ErrTaskAlreadyClaimed.Error() + ": task=" + e.TaskID + " claimed_by=" + e.ClaimedBy + " requested_by=" + e.RequestingBy
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskAlreadyClaimedError) Unwrap() error {
	return ErrTaskAlreadyClaimed
}

// TaskVersionConflictError indicates a CAS conflict due to a stale version.
type TaskVersionConflictError struct {
	TaskID          string
	ExpectedVersion uint64
	ActualVersion   uint64
}

// Error implements the error interface.
func (e *TaskVersionConflictError) Error() string {
	return ErrTaskVersionConflict.Error() + ": task=" + e.TaskID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskVersionConflictError) Unwrap() error {
	return ErrTaskVersionConflict
}

// TaskDependencyBlockedError indicates a task has unresolved dependencies.
type TaskDependencyBlockedError struct {
	TaskID               string
	UnresolvedDependency string
}

// Error implements the error interface.
func (e *TaskDependencyBlockedError) Error() string {
	return ErrTaskDependencyBlocked.Error() + ": task=" + e.TaskID + " blocked_by=" + e.UnresolvedDependency
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskDependencyBlockedError) Unwrap() error {
	return ErrTaskDependencyBlocked
}

// TaskInvalidStateError indicates an invalid state transition was attempted.
type TaskInvalidStateError struct {
	TaskID       string
	CurrentState TaskStatus
	TargetState  TaskStatus
}

// Error implements the error interface.
func (e *TaskInvalidStateError) Error() string {
	return ErrTaskInvalidState.Error() + ": task=" + e.TaskID + " current=" + string(e.CurrentState) + " target=" + string(e.TargetState)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskInvalidStateError) Unwrap() error {
	return ErrTaskInvalidState
}

// TaskStoreError wraps a sentinel error with an additional message
// for task store operations.
type TaskStoreError struct {
	Sentinel error
	Message  string
}

// Error implements the error interface.
func (e *TaskStoreError) Error() string {
	if e.Message != "" {
		return e.Sentinel.Error() + ": " + e.Message
	}
	return e.Sentinel.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskStoreError) Unwrap() error {
	return e.Sentinel
}

// ErrTaskListingIncomplete indicates a task listing met records it could not
// decode, so a task missing from it may exist.
var ErrTaskListingIncomplete = errors.New("task store: listing incomplete")

// TaskListing is the result of ListTaskRecords: the readable records that
// matched the filter and the IDs of the records that could not be read at all.
//
// AN UNREADABLE RECORD IS NOT AN ABSENT ONE. Its fields cannot be decoded, so
// the filter cannot be applied to it: Unreadable names every undecodable record
// the scan met under the tenant, whichever project or status it may belong to.
// A caller that concludes something from a task NOT being listed — an orphan, a
// free slot, a count — must refuse when Unreadable is non-empty; a caller that
// only acts on the tasks it can see may use Tasks regardless. A listing cut
// short by filter.Limit reports only the unreadable records met before it
// stopped.
type TaskListing struct {
	Tasks      []*Task
	Unreadable []string
}

// Complete reports whether every record the scan met could be read.
func (l *TaskListing) Complete() bool {
	return l == nil || len(l.Unreadable) == 0
}

// Err returns a *TaskListingIncompleteError when the listing is not complete,
// and nil when it is, for a caller that fails closed on an incomplete listing.
func (l *TaskListing) Err(tenantID string) error {
	if l.Complete() {
		return nil
	}
	return &TaskListingIncompleteError{TenantID: tenantID, TaskIDs: append([]string(nil), l.Unreadable...)}
}

// TaskListingIncompleteError reports a task listing that met records it could
// not decode. TaskIDs names them so an operator can repair or remove them.
type TaskListingIncompleteError struct {
	TenantID string
	TaskIDs  []string
}

// Error implements the error interface.
func (e *TaskListingIncompleteError) Error() string {
	return ErrTaskListingIncomplete.Error() + ": tenant " + e.TenantID + " has " +
		strconv.Itoa(len(e.TaskIDs)) + " unreadable task record(s): " + strings.Join(e.TaskIDs, ", ")
}

// Unwrap returns the sentinel for errors.Is support.
func (e *TaskListingIncompleteError) Unwrap() error {
	return ErrTaskListingIncomplete
}

// TaskSourceAlreadyAdoptedError indicates a task already exists for the same
// (Kind, Ref) source within the tenant and project. TaskID names the holder so
// a caller can attach to the existing task instead of creating a second one,
// and HolderStatus says what state that holder is in — without it a refused
// adoption reads as a silent poller failure, which is what made #652
// undiagnosable from the sweep log. HolderBlockFaultClass names the
// environment fault a blocked holder is held on, so a reporter can tell a
// transient hold a re-label supersedes from a deliberate operator hold that
// binds the source.
type TaskSourceAlreadyAdoptedError struct {
	TenantID              string
	ProjectID             string
	Kind                  TaskSourceKind
	Ref                   string
	TaskID                string
	HolderStatus          TaskStatus
	HolderBlockFaultClass string
}

// Error implements the error interface.
func (e *TaskSourceAlreadyAdoptedError) Error() string {
	msg := ErrTaskSourceAlreadyAdopted.Error() + ": tenant=" + e.TenantID +
		" project=" + e.ProjectID + " kind=" + string(e.Kind) + " ref=" + e.Ref +
		" task=" + e.TaskID
	if e.HolderStatus != "" {
		msg += " holder_status=" + string(e.HolderStatus)
	}
	if e.HolderBlockFaultClass != "" {
		msg += " holder_block_fault_class=" + e.HolderBlockFaultClass
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskSourceAlreadyAdoptedError) Unwrap() error {
	return ErrTaskSourceAlreadyAdopted
}

// InvalidTaskSourceKindError reports a TaskSourceKind that is neither built in
// nor declared by a registered TaskSource.
type InvalidTaskSourceKindError struct {
	Kind TaskSourceKind
}

// Error implements the error interface.
func (e *InvalidTaskSourceKindError) Error() string {
	return ErrTaskStoreInvalidInput.Error() + ": unknown task source kind " + string(e.Kind)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidTaskSourceKindError) Unwrap() error {
	return ErrTaskStoreInvalidInput
}

// ReviewRoundBudgetExhaustedError reports a task whose ReviewRoundBonus is
// already at MaxReviewRoundBonus: granting another round is refused rather
// than turning the bonus into a budget nothing ever exhausts.
type ReviewRoundBudgetExhaustedError struct {
	TaskID string
	Max    int
}

// Error implements the error interface.
func (e *ReviewRoundBudgetExhaustedError) Error() string {
	return "task store: review round bonus already at its maximum (" +
		strconv.Itoa(e.Max) + "): task=" + e.TaskID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ReviewRoundBudgetExhaustedError) Unwrap() error { return ErrTaskStoreInvalidInput }

// InvalidTaskBlockKindError reports a TaskBlockKind outside the closed enum.
type InvalidTaskBlockKindError struct {
	Kind TaskBlockKind
}

// Error implements the error interface.
func (e *InvalidTaskBlockKindError) Error() string {
	return ErrTaskStoreInvalidInput.Error() + ": unknown task block kind " + string(e.Kind)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidTaskBlockKindError) Unwrap() error {
	return ErrTaskStoreInvalidInput
}
