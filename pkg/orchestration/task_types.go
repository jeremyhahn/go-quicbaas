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
)

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

	// ProjectID scopes this task to a registered project. When non-empty,
	// ClaimTask enforces the project's AgentRoles access matrix.
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

	// ListTasks lists tasks matching the filter within a tenant.
	ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error)

	// ClaimTask atomically claims a task for an agent using compare-and-swap.
	// Returns error if task is already claimed by another agent.
	ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error

	// CompleteTask marks a task as completed with a result.
	CompleteTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error

	// FailTask marks a task as failed with a reason.
	FailTask(ctx context.Context, tenantID, taskID, reason string) error

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
