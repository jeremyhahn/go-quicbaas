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

// Package scheduler provides a unified job scheduling system for QuicBaaS.
//
// # Clustering
//
// In a Raft cluster, scheduler storage is replicated on the Platform
// shard. Every node's SchedulerEngine polls the same replicated state.
// To prevent duplicate execution of jobs, the engine consults a
// per-job dispatch gate before firing (see shouldExecute). The gate
// uses two engine-level settings (nodeID + LeaderChecker) and one
// per-job field (Job.NodeAffinity) to classify each due job:
//
//   - NodeAffinity == ""      -> cluster-wide, fires on Raft leader only
//   - NodeAffinity == nodeID  -> affine to this node, fires here
//   - NodeAffinity != nodeID  -> skipped (belongs to another node)
//
// See docs/scheduler-clustering.md for the full behavior matrix and
// design rationale. Constructors:
//
//   - New(config, storage)                  -- single-node / test mode
//   - NewWithClustering(config, storage,
//     nodeID, leader)                   -- cluster deployment
//
// Existing idempotent handlers (audit_cleanup, kms_grace,
// db_compaction) work unchanged under both constructors.
package scheduler

import (
	"context"
	"encoding/json"
	"time"
)

// JobType defines the type of scheduling for a job.
type JobType string

const (
	JobTypeOneShot   JobType = "one-shot"  // Execute once at specific time
	JobTypeRecurring JobType = "recurring" // Execute on cron schedule
	JobTypeDelayed   JobType = "delayed"   // Execute after delay from creation
)

// JobStatus represents the current state of a job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"   // Awaiting execution
	JobStatusRunning   JobStatus = "running"   // Currently executing
	JobStatusCompleted JobStatus = "completed" // Successfully finished
	JobStatusFailed    JobStatus = "failed"    // Execution failed
	JobStatusPaused    JobStatus = "paused"    // Temporarily suspended
	JobStatusCanceled  JobStatus = "canceled"  // Permanently stopped
)

// OwnerType defines who owns a job.
type OwnerType string

const (
	OwnerTypeSystem    OwnerType = "system"    // Internal system jobs (DB maintenance)
	OwnerTypeTenant    OwnerType = "tenant"    // User-created tenant jobs
	OwnerTypeDelegated OwnerType = "delegated" // System job operating on tenant data
)

// JobHandler is the function signature for job execution handlers.
type JobHandler func(ctx context.Context, job *Job) (*JobResult, error)

// Job represents a scheduled task in the system.
type Job struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Ownership
	OwnerType      OwnerType `json:"owner_type"`
	OwnerID        string    `json:"owner_id,omitempty"`         // TenantID for tenant jobs
	TargetTenantID string    `json:"target_tenant_id,omitempty"` // For delegated jobs
	CreatedBy      string    `json:"created_by,omitempty"`

	// Schedule
	Type      JobType       `json:"type"`
	Schedule  string        `json:"schedule,omitempty"` // Cron expression for recurring
	ExecuteAt time.Time     `json:"execute_at"`
	Delay     time.Duration `json:"delay,omitempty"`

	// Execution
	Handler    string          `json:"handler"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Priority   int32           `json:"priority"`
	Timeout    time.Duration   `json:"timeout"`
	MaxRetries int32           `json:"max_retries"`

	// NodeAffinity restricts execution to a specific node. When empty
	// (default), the job is cluster-wide and executes on the Raft
	// leader only. When non-empty, only the node whose SchedulerEngine
	// was constructed with a matching NodeID executes the job,
	// regardless of leadership. Node-affine jobs are the correct
	// choice for workloads that depend on node-local state (e.g.,
	// renewing certs whose private keys live only on the local node).
	NodeAffinity string `json:"node_affinity,omitempty"`

	// State
	Status    JobStatus `json:"status"`
	LastRun   time.Time `json:"last_run,omitempty"`
	NextRun   time.Time `json:"next_run,omitempty"`
	RunCount  uint64    `json:"run_count"`
	FailCount uint64    `json:"fail_count"`
	LastError string    `json:"last_error,omitempty"`

	// Metadata
	Labels    map[string]string `json:"labels,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// JobExecution represents a single execution of a job.
type JobExecution struct {
	ID        uint64        `json:"id"`
	JobID     uint64        `json:"job_id"`
	Status    JobStatus     `json:"status"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	Error     string        `json:"error,omitempty"`
	Output    string        `json:"output,omitempty"`
}

// JobResult is returned by job handlers after execution.
type JobResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Output  []byte `json:"output,omitempty"`
}

// JobQuery defines filters for listing jobs.
type JobQuery struct {
	OwnerType      OwnerType         `json:"owner_type,omitempty"`
	OwnerID        string            `json:"owner_id,omitempty"`
	TargetTenantID string            `json:"target_tenant_id,omitempty"`
	Status         JobStatus         `json:"status,omitempty"`
	Handler        string            `json:"handler,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Limit          int               `json:"limit,omitempty"`
	Cursor         string            `json:"cursor,omitempty"`
}

// JobUpdateParams contains fields that can be updated on an existing job.
type JobUpdateParams struct {
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Schedule    string            `json:"schedule,omitempty"`
	ExecuteAt   time.Time         `json:"execute_at,omitempty"`
	Handler     string            `json:"handler,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	Priority    int32             `json:"priority,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	MaxRetries  int32             `json:"max_retries,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// SchedulerStats contains scheduler runtime statistics.
type SchedulerStats struct {
	JobCount     uint64    `json:"job_count"`
	RunningJobs  uint64    `json:"running_jobs"`
	SuccessCount uint64    `json:"success_count"`
	FailureCount uint64    `json:"failure_count"`
	LastTickTime time.Time `json:"last_tick_time,omitempty"`
	StartedAt    time.Time `json:"started_at"`
}

// Logger defines a minimal logging interface for the scheduler.
// This allows the scheduler to log debug information without requiring
// a specific logging implementation.
type Logger interface {
	// Debugf logs a debug message.
	Debugf(format string, args ...interface{})
	// Warnf logs a warning message.
	Warnf(format string, args ...interface{})
	// Errorf logs an error message.
	Errorf(format string, args ...interface{})
}

// LeaderChecker reports whether this node is currently the Raft
// leader for the scheduler's storage shard. A nil LeaderChecker on a
// SchedulerEngine is interpreted as "always leader" — the correct
// behavior for single-node deployments and memory-backed tests
// where no Raft group exists.
//
// Implementations must be safe for concurrent use and should return
// quickly (the scheduler calls IsLeader once per tick per due job).
type LeaderChecker interface {
	// IsLeader returns true when this node is the Raft leader. The
	// scheduler uses this to gate execution of cluster-wide jobs
	// (Job.NodeAffinity == "") so that such jobs fire exactly once
	// per tick across the cluster. Returning an error is equivalent
	// to returning false; the scheduler will not execute cluster-wide
	// jobs while leadership cannot be determined.
	IsLeader() (bool, error)
}

// SchedulerConfig contains scheduler configuration.
type SchedulerConfig struct {
	// TickInterval is how often the scheduler checks for jobs to run
	TickInterval time.Duration `json:"tick_interval"`

	// MaxConcurrentJobs limits parallel job execution
	MaxConcurrentJobs int `json:"max_concurrent_jobs"`

	// DefaultTimeout for jobs without explicit timeout
	DefaultTimeout time.Duration `json:"default_timeout"`

	// DefaultRetries for jobs without explicit retry count
	DefaultRetries int `json:"default_retries"`

	// RetryBackoff duration between retry attempts
	RetryBackoff time.Duration `json:"retry_backoff"`

	// Logger is an optional logger for debug output.
	// If nil, no logging will be performed.
	Logger Logger `json:"-"`
}

// DefaultConfig returns a scheduler configuration with sensible defaults.
func DefaultConfig() *SchedulerConfig {
	return &SchedulerConfig{
		TickInterval:      time.Second,
		MaxConcurrentJobs: 10,
		DefaultTimeout:    5 * time.Minute,
		DefaultRetries:    3,
		RetryBackoff:      time.Second * 5,
	}
}

// Scheduler defines the interface for job scheduling operations.
type Scheduler interface {
	// RegisterHandler registers a job handler for a given name.
	RegisterHandler(name string, handler JobHandler) error

	// UnregisterHandler removes a registered handler.
	UnregisterHandler(name string) error

	// ScheduleJob schedules a new job for execution.
	ScheduleJob(ctx context.Context, job *Job) error

	// CancelJob cancels a scheduled job.
	CancelJob(ctx context.Context, jobID uint64) error

	// GetJob retrieves a job by ID.
	GetJob(ctx context.Context, jobID uint64) (*Job, error)

	// ListJobs lists jobs matching the query criteria.
	ListJobs(ctx context.Context, query *JobQuery) ([]*Job, error)

	// Start begins the scheduler's job processing loop.
	Start(ctx context.Context) error

	// Stop gracefully stops the scheduler.
	Stop(ctx context.Context) error

	// Stats returns current scheduler statistics.
	Stats() *SchedulerStats
}
