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
package scheduler

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
)

// JobStorage defines the interface for job persistence.
type JobStorage interface {
	CreateJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, id uint64, tenantID string) (*Job, error)
	UpdateJob(ctx context.Context, job *Job) error
	DeleteJob(ctx context.Context, id uint64, tenantID string) error
	ListJobs(ctx context.Context, query *JobQuery) ([]*Job, string, error)
	GetJobsDueForExecution(ctx context.Context, before time.Time, limit int) ([]*Job, error)
	SaveExecution(ctx context.Context, exec *JobExecution) error
	GetExecutions(ctx context.Context, jobID uint64, tenantID string, limit int) ([]*JobExecution, error)
}

// SchedulerEngine manages job scheduling and execution.
// It implements the Scheduler interface with lock-free operations.
type SchedulerEngine struct {
	config  *SchedulerConfig
	storage JobStorage

	// nodeID identifies this node for NodeAffinity matching. Empty
	// string disables affinity-based gating (all jobs are eligible
	// regardless of affinity) — appropriate for single-node and
	// test-mode deployments.
	nodeID string

	// leader reports Raft leadership for the scheduler's storage
	// shard. Nil means "always leader" (single-node / test mode);
	// when non-nil, cluster-wide jobs (Job.NodeAffinity == "") only
	// execute when IsLeader() returns true.
	leader LeaderChecker

	// Map-based dispatch for O(1) handler lookup
	handlers   map[string]JobHandler
	handlersMu sync.RWMutex

	// Lock-free state using atomic operations
	running atomic.Bool
	stopped atomic.Bool

	// Atomic counters for statistics
	jobCount     atomic.Uint64
	runningJobs  atomic.Uint64
	successCount atomic.Uint64
	failureCount atomic.Uint64

	// Channel-based coordination (no time.Sleep)
	ctx      context.Context
	cancelFn context.CancelFunc
	wg       sync.WaitGroup

	// startedCh is closed after Start() finishes writing all shared state
	// (ctx, cancelFn, ticker, tickCh, wg.Add). Stop() waits on this channel
	// before reading those fields to prevent data races when Start() is called
	// from a separate goroutine.
	startedCh chan struct{}

	// Ticker for periodic checks
	tickCh <-chan time.Time
	ticker *time.Ticker

	// Trigger channel for immediate job execution
	triggerCh chan uint64

	// Semaphore for limiting concurrent jobs
	semaphore chan struct{}

	// Last tick time for stats
	lastTickTime atomic.Pointer[time.Time]
	startedAt    time.Time

	// Cron parser for recurring schedules
	cronParser cron.Parser

	// Storage error counter for monitoring
	storageErrors atomic.Uint64
}

// Verify SchedulerEngine implements Scheduler interface.
var _ Scheduler = (*SchedulerEngine)(nil)

// New creates a new scheduler engine with the given configuration.
// Single-node behavior (nil LeaderChecker, empty NodeID) is preserved
// for backward compatibility. Cluster callers use NewWithClustering
// to inject Raft leader gating and node affinity.
func New(config *SchedulerConfig, storage JobStorage) *SchedulerEngine {
	return NewWithClustering(config, storage, "", nil)
}

// NewWithClustering creates a scheduler engine wired for Raft-cluster
// deployment. nodeID identifies this node for Job.NodeAffinity
// matching; leader gates cluster-wide job execution to the Raft
// leader only.
//
// Behavior matrix:
//
//	| nodeID | leader     | Cluster-wide job (Affinity=="")         | Affine job (Affinity=="X")
//	|--------|------------|-----------------------------------------|---------------------------
//	| ""     | nil        | always fires                            | always fires (affinity ignored)
//	| "N"    | nil        | always fires                            | fires only when X == N
//	| ""     | non-nil    | fires only when leader.IsLeader()       | always fires (affinity ignored)
//	| "N"    | non-nil    | fires only when leader.IsLeader()       | fires only when X == N
//
// Pass empty nodeID + nil leader for single-node / test mode. Pass
// both for clustered production deployments.
func NewWithClustering(config *SchedulerConfig, storage JobStorage, nodeID string, leader LeaderChecker) *SchedulerEngine {
	if config == nil {
		config = DefaultConfig()
	}

	return &SchedulerEngine{
		config:     config,
		storage:    storage,
		nodeID:     nodeID,
		leader:     leader,
		handlers:   make(map[string]JobHandler),
		triggerCh:  make(chan uint64, 100),
		semaphore:  make(chan struct{}, config.MaxConcurrentJobs),
		startedCh:  make(chan struct{}),
		cronParser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
	}
}

// logDebug logs a debug message if logger is configured.
func (s *SchedulerEngine) logDebug(format string, args ...interface{}) {
	if s.config != nil && s.config.Logger != nil {
		s.config.Logger.Debugf(format, args...)
	}
}

// logWarn logs a warning message if logger is configured.
func (s *SchedulerEngine) logWarn(format string, args ...interface{}) {
	if s.config != nil && s.config.Logger != nil {
		s.config.Logger.Warnf(format, args...)
	}
}

// logError logs an error message if logger is configured.
func (s *SchedulerEngine) logError(format string, args ...interface{}) {
	if s.config != nil && s.config.Logger != nil {
		s.config.Logger.Errorf(format, args...)
	}
}

// RegisterHandler registers a handler for a given handler name.
// Returns error if handler name already registered.
func (s *SchedulerEngine) RegisterHandler(name string, handler JobHandler) error {
	if name == "" {
		return InvalidJobError{Field: "handler", Message: "handler name cannot be empty"}
	}
	if handler == nil {
		return InvalidJobError{Field: "handler", Message: "handler function cannot be nil"}
	}

	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()

	if _, exists := s.handlers[name]; exists {
		return JobAlreadyExistsError{Name: name}
	}

	s.handlers[name] = handler
	return nil
}

// UnregisterHandler removes a handler registration.
func (s *SchedulerEngine) UnregisterHandler(name string) error {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()
	delete(s.handlers, name)
	return nil
}

// getHandler retrieves a handler by name with read lock.
func (s *SchedulerEngine) getHandler(name string) (JobHandler, bool) {
	s.handlersMu.RLock()
	defer s.handlersMu.RUnlock()
	handler, ok := s.handlers[name]
	return handler, ok
}

// Start begins the scheduler's main loop.
func (s *SchedulerEngine) Start(ctx context.Context) error {
	// Use compare-and-swap for lock-free state transition
	if !s.running.CompareAndSwap(false, true) {
		return &SchedulerError{
			Code:    ErrCodeSchedulerStopped,
			Op:      "Start",
			Message: "scheduler is already running",
		}
	}

	if s.stopped.Load() {
		s.running.Store(false)
		return &SchedulerError{
			Code:    ErrCodeSchedulerStopped,
			Op:      "Start",
			Message: "scheduler has been stopped and cannot be restarted",
		}
	}

	s.ctx, s.cancelFn = context.WithCancel(ctx) // #nosec G118 -- cancelFn stored in scheduler, called in Stop()
	s.startedAt = time.Now()
	s.ticker = time.NewTicker(s.config.TickInterval)
	s.tickCh = s.ticker.C

	s.logDebug("Scheduler starting with tick interval %v, max concurrent jobs %d",
		s.config.TickInterval, s.config.MaxConcurrentJobs)

	// Add to WaitGroup before signaling readiness so Stop() can safely
	// call wg.Wait() after receiving on startedCh.
	s.wg.Add(1)
	go s.run()

	// Signal that all shared state is fully initialized (ctx, cancelFn,
	// ticker, tickCh, wg). Stop() waits on this channel before accessing
	// any of these fields, preventing data races when Start() is called
	// from a separate goroutine.
	close(s.startedCh)

	return nil
}

// Stop gracefully stops the scheduler.
func (s *SchedulerEngine) Stop(ctx context.Context) error {
	if !s.running.CompareAndSwap(true, false) {
		return &SchedulerError{
			Code:    ErrCodeSchedulerStopped,
			Op:      "Stop",
			Message: "scheduler is not running",
		}
	}

	s.stopped.Store(true)

	// Wait for Start() to finish writing all shared state (ctx, cancelFn,
	// ticker, wg.Add). This prevents data races when Start() is called
	// from a separate goroutine.
	select {
	case <-s.startedCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	if s.cancelFn != nil {
		s.cancelFn()
	}

	if s.ticker != nil {
		s.ticker.Stop()
	}

	// Wait for all goroutines to finish with context timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ScheduleJob creates a new job in the scheduler.
func (s *SchedulerEngine) ScheduleJob(ctx context.Context, job *Job) error {
	if !s.running.Load() {
		return SchedulerStoppedError{}
	}

	if err := s.validateJob(job); err != nil {
		return err
	}

	// Set defaults
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now
	job.Status = JobStatusPending

	// Calculate next run time based on job type
	if err := s.calculateNextRun(job); err != nil {
		return err
	}

	if err := s.storage.CreateJob(ctx, job); err != nil {
		return ErrStorageError{Op: "CreateJob", Cause: err}
	}

	s.jobCount.Add(1)
	return nil
}

// CancelJob cancels a scheduled job.
func (s *SchedulerEngine) CancelJob(ctx context.Context, jobID uint64) error {
	if !s.running.Load() {
		return SchedulerStoppedError{}
	}

	job, err := s.storage.GetJob(ctx, jobID, "")
	if err != nil {
		return ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return JobNotFoundError{JobID: jobID}
	}

	// Only pending or paused jobs can be canceled
	if job.Status == JobStatusRunning {
		return InvalidJobError{
			Field:   "status",
			Message: "cannot cancel a running job",
		}
	}

	if job.Status == JobStatusCanceled || job.Status == JobStatusCompleted {
		return InvalidJobError{
			Field:   "status",
			Message: "job is already in terminal state",
		}
	}

	job.Status = JobStatusCanceled
	job.UpdatedAt = time.Now()

	if err := s.storage.UpdateJob(ctx, job); err != nil {
		return ErrStorageError{Op: "UpdateJob", Cause: err}
	}

	return nil
}

// CancelJobWithTenant cancels a scheduled job with tenant context.
func (s *SchedulerEngine) CancelJobWithTenant(ctx context.Context, jobID uint64, tenantID string, reason string) (JobStatus, error) {
	if !s.running.Load() {
		return "", SchedulerStoppedError{}
	}

	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return "", ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return "", JobNotFoundError{JobID: jobID}
	}

	oldStatus := job.Status

	// Only pending or paused jobs can be canceled
	if job.Status == JobStatusRunning {
		return oldStatus, InvalidJobError{
			Field:   "status",
			Message: "cannot cancel a running job",
		}
	}

	if job.Status == JobStatusCanceled || job.Status == JobStatusCompleted {
		return oldStatus, InvalidJobError{
			Field:   "status",
			Message: "job is already in terminal state",
		}
	}

	job.Status = JobStatusCanceled
	job.UpdatedAt = time.Now()
	if reason != "" {
		job.LastError = reason
	}

	if err := s.storage.UpdateJob(ctx, job); err != nil {
		return oldStatus, ErrStorageError{Op: "UpdateJob", Cause: err}
	}

	return oldStatus, nil
}

// TriggerJob immediately executes a job.
func (s *SchedulerEngine) TriggerJob(ctx context.Context, jobID uint64, tenantID string) error {
	if !s.running.Load() {
		return SchedulerStoppedError{}
	}

	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return JobNotFoundError{JobID: jobID}
	}

	// Only pending or paused jobs can be triggered
	if job.Status != JobStatusPending && job.Status != JobStatusPaused {
		return InvalidJobError{
			Field:   "status",
			Message: "job must be pending or paused to trigger",
		}
	}

	// Send to trigger channel (non-blocking with buffer)
	select {
	case s.triggerCh <- jobID:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Channel full, job will be picked up in next tick
		return nil
	}
}

// PauseJob pauses a scheduled job.
func (s *SchedulerEngine) PauseJob(ctx context.Context, jobID uint64, tenantID string) error {
	if !s.running.Load() {
		return SchedulerStoppedError{}
	}

	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return JobNotFoundError{JobID: jobID}
	}

	if job.Status != JobStatusPending {
		return InvalidJobError{
			Field:   "status",
			Message: "only pending jobs can be paused",
		}
	}

	job.Status = JobStatusPaused
	job.UpdatedAt = time.Now()

	if err := s.storage.UpdateJob(ctx, job); err != nil {
		return ErrStorageError{Op: "UpdateJob", Cause: err}
	}

	return nil
}

// PauseJobWithStatus pauses a scheduled job and returns the old status.
func (s *SchedulerEngine) PauseJobWithStatus(ctx context.Context, jobID uint64, tenantID string) (JobStatus, error) {
	if !s.running.Load() {
		return "", SchedulerStoppedError{}
	}

	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return "", ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return "", JobNotFoundError{JobID: jobID}
	}

	oldStatus := job.Status

	if job.Status != JobStatusPending {
		return oldStatus, InvalidJobError{
			Field:   "status",
			Message: "only pending jobs can be paused",
		}
	}

	job.Status = JobStatusPaused
	job.UpdatedAt = time.Now()

	if err := s.storage.UpdateJob(ctx, job); err != nil {
		return oldStatus, ErrStorageError{Op: "UpdateJob", Cause: err}
	}

	return oldStatus, nil
}

// ResumeJob resumes a paused job.
func (s *SchedulerEngine) ResumeJob(ctx context.Context, jobID uint64, tenantID string) error {
	if !s.running.Load() {
		return SchedulerStoppedError{}
	}

	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return JobNotFoundError{JobID: jobID}
	}

	if job.Status != JobStatusPaused {
		return InvalidJobError{
			Field:   "status",
			Message: "only paused jobs can be resumed",
		}
	}

	job.Status = JobStatusPending
	job.UpdatedAt = time.Now()

	// Recalculate next run if needed
	if job.NextRun.Before(time.Now()) {
		if err := s.calculateNextRun(job); err != nil {
			return err
		}
	}

	if err := s.storage.UpdateJob(ctx, job); err != nil {
		return ErrStorageError{Op: "UpdateJob", Cause: err}
	}

	return nil
}

// ResumeJobWithStatus resumes a paused job and returns the old status.
func (s *SchedulerEngine) ResumeJobWithStatus(ctx context.Context, jobID uint64, tenantID string) (JobStatus, error) {
	if !s.running.Load() {
		return "", SchedulerStoppedError{}
	}

	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return "", ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return "", JobNotFoundError{JobID: jobID}
	}

	oldStatus := job.Status

	if job.Status != JobStatusPaused {
		return oldStatus, InvalidJobError{
			Field:   "status",
			Message: "only paused jobs can be resumed",
		}
	}

	job.Status = JobStatusPending
	job.UpdatedAt = time.Now()

	// Recalculate next run if needed
	if job.NextRun.Before(time.Now()) {
		if err := s.calculateNextRun(job); err != nil {
			return oldStatus, err
		}
	}

	if err := s.storage.UpdateJob(ctx, job); err != nil {
		return oldStatus, ErrStorageError{Op: "UpdateJob", Cause: err}
	}

	return oldStatus, nil
}

// GetJob retrieves a job by ID.
func (s *SchedulerEngine) GetJob(ctx context.Context, jobID uint64) (*Job, error) {
	job, err := s.storage.GetJob(ctx, jobID, "")
	if err != nil {
		return nil, ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return nil, JobNotFoundError{JobID: jobID}
	}
	return job, nil
}

// GetJobWithTenant retrieves a job by ID with tenant context.
func (s *SchedulerEngine) GetJobWithTenant(ctx context.Context, jobID uint64, tenantID string) (*Job, error) {
	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return nil, ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return nil, JobNotFoundError{JobID: jobID}
	}
	return job, nil
}

// UpdateJob updates an existing job's mutable fields.
func (s *SchedulerEngine) UpdateJob(ctx context.Context, jobID uint64, tenantID string, updates *JobUpdateParams) (*Job, error) {
	if !s.running.Load() {
		return nil, SchedulerStoppedError{}
	}

	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return nil, ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return nil, JobNotFoundError{JobID: jobID}
	}

	// Apply updates to mutable fields
	if updates.Name != "" {
		job.Name = updates.Name
	}
	if updates.Description != "" {
		job.Description = updates.Description
	}
	if updates.Schedule != "" {
		// Validate new cron schedule
		if _, err := s.cronParser.Parse(updates.Schedule); err != nil {
			return nil, InvalidScheduleError{Schedule: updates.Schedule, Cause: err}
		}
		job.Schedule = updates.Schedule
		// Recalculate next run for recurring jobs
		if job.Type == JobTypeRecurring {
			if err := s.calculateNextRun(job); err != nil {
				return nil, err
			}
		}
	}
	if !updates.ExecuteAt.IsZero() {
		job.ExecuteAt = updates.ExecuteAt
		if job.Type == JobTypeOneShot {
			job.NextRun = updates.ExecuteAt
		}
	}
	if updates.Handler != "" {
		// Handler existence is validated at execution time, not update time
		job.Handler = updates.Handler
	}
	if updates.Payload != nil {
		job.Payload = json.RawMessage(updates.Payload)
	}
	if updates.Priority != 0 {
		job.Priority = updates.Priority
	}
	if updates.Timeout != 0 {
		job.Timeout = updates.Timeout
	}
	if updates.MaxRetries != 0 {
		job.MaxRetries = updates.MaxRetries
	}
	if updates.Labels != nil {
		job.Labels = updates.Labels
	}

	job.UpdatedAt = time.Now()

	if err := s.storage.UpdateJob(ctx, job); err != nil {
		return nil, ErrStorageError{Op: "UpdateJob", Cause: err}
	}

	return job, nil
}

// DeleteJob removes a job from the scheduler.
func (s *SchedulerEngine) DeleteJob(ctx context.Context, jobID uint64, tenantID string, force bool) error {
	if !s.running.Load() {
		return SchedulerStoppedError{}
	}

	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return JobNotFoundError{JobID: jobID}
	}

	// Don't allow deleting running jobs unless force is set
	if job.Status == JobStatusRunning && !force {
		return InvalidJobError{
			Field:   "status",
			Message: "cannot delete a running job without force flag",
		}
	}

	if err := s.storage.DeleteJob(ctx, jobID, tenantID); err != nil {
		return ErrStorageError{Op: "DeleteJob", Cause: err}
	}

	// Decrement job count if we were tracking it
	// Use CAS loop to safely decrement
	for {
		current := s.jobCount.Load()
		if current == 0 {
			break
		}
		if s.jobCount.CompareAndSwap(current, current-1) {
			break
		}
	}

	return nil
}

// GetJobHistory retrieves execution history for a job.
func (s *SchedulerEngine) GetJobHistory(ctx context.Context, jobID uint64, tenantID string, limit int) ([]*JobExecution, error) {
	// Verify job exists
	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return nil, ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return nil, JobNotFoundError{JobID: jobID}
	}

	executions, err := s.storage.GetExecutions(ctx, jobID, tenantID, limit)
	if err != nil {
		return nil, ErrStorageError{Op: "GetExecutions", Cause: err}
	}

	return executions, nil
}

// ListJobs returns jobs matching the query.
func (s *SchedulerEngine) ListJobs(ctx context.Context, query *JobQuery) ([]*Job, error) {
	jobs, _, err := s.storage.ListJobs(ctx, query)
	if err != nil {
		return nil, ErrStorageError{Op: "ListJobs", Cause: err}
	}
	return jobs, nil
}

// ListJobsWithCursor returns jobs matching the query with pagination cursor.
func (s *SchedulerEngine) ListJobsWithCursor(ctx context.Context, query *JobQuery) ([]*Job, string, error) {
	jobs, cursor, err := s.storage.ListJobs(ctx, query)
	if err != nil {
		return nil, "", ErrStorageError{Op: "ListJobs", Cause: err}
	}
	return jobs, cursor, nil
}

// Stats returns current scheduler statistics.
func (s *SchedulerEngine) Stats() *SchedulerStats {
	stats := &SchedulerStats{
		JobCount:     s.jobCount.Load(),
		RunningJobs:  s.runningJobs.Load(),
		SuccessCount: s.successCount.Load(),
		FailureCount: s.failureCount.Load(),
		StartedAt:    s.startedAt,
	}

	if lastTick := s.lastTickTime.Load(); lastTick != nil {
		stats.LastTickTime = *lastTick
	}

	return stats
}

// IsRunning returns whether the scheduler is running.
func (s *SchedulerEngine) IsRunning() bool {
	return s.running.Load()
}

// StorageErrorCount returns the number of storage errors encountered.
func (s *SchedulerEngine) StorageErrorCount() uint64 {
	return s.storageErrors.Load()
}

// run is the main loop using select on ticker, trigger, and context.Done.
func (s *SchedulerEngine) run() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return

		case t := <-s.tickCh:
			s.lastTickTime.Store(&t)
			s.processDueJobs(s.ctx)

		case jobID := <-s.triggerCh:
			s.triggerJobByID(s.ctx, jobID)
		}
	}
}

// processDueJobs fetches and executes jobs that are due.
func (s *SchedulerEngine) processDueJobs(ctx context.Context) {
	now := time.Now()
	s.logDebug("[SCHEDULER TICK] Checking for due jobs at %v", now)

	// Create a context with timeout for storage operations
	// This is required for Raft-based storage which needs a deadline
	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	jobs, err := s.storage.GetJobsDueForExecution(storageCtx, now, s.config.MaxConcurrentJobs)
	if err != nil {
		s.logError("GetJobsDueForExecution failed: %v", err)
		s.storageErrors.Add(1)
		return
	}

	s.logDebug("[SCHEDULER TICK] Found %d due jobs", len(jobs))
	if len(jobs) > 0 {
		for _, j := range jobs {
			s.logDebug("  Job %d: %s, NextRun=%v, Status=%s, Handler=%s",
				j.ID, j.Name, j.NextRun, j.Status, j.Handler)
		}
	}

	for _, job := range jobs {
		if !s.shouldExecute(job) {
			continue
		}
		// Check if we can acquire semaphore
		select {
		case s.semaphore <- struct{}{}:
			s.logDebug("Executing job %d: %s", job.ID, job.Name)
			s.wg.Add(1)
			go func(j *Job) {
				defer s.wg.Done()
				defer func() { <-s.semaphore }()
				s.executeJob(ctx, j)
			}(job)
		default:
			// Semaphore full, skip this job for now
			s.logWarn("Semaphore full, skipping job %d for now", job.ID)
			continue
		}
	}
}

// shouldExecute applies the cluster-dispatch gate to a due job.
// Returns true iff this node should execute the job right now.
//
// Rules (see NewWithClustering godoc for the full behavior matrix):
//
//   - If Job.NodeAffinity is set and this engine has a non-empty
//     nodeID, execute only when the two match. Affine jobs ignore
//     leadership: the affine node is the only one that can perform
//     the work (local state), regardless of whether it is the Raft
//     leader.
//
//   - If Job.NodeAffinity is empty (cluster-wide job), execute only
//     when this node is the leader. A nil LeaderChecker disables
//     the gate (single-node / test mode); the scheduler behaves as
//     the sole leader.
//
//   - If the engine has no nodeID (empty string), affinity matching
//     is disabled and affine jobs always fire — preserving
//     pre-clustering behavior for single-node deployments.
func (s *SchedulerEngine) shouldExecute(job *Job) bool {
	if job.NodeAffinity != "" && s.nodeID != "" {
		if job.NodeAffinity != s.nodeID {
			s.logDebug("Skipping job %d (%s): NodeAffinity=%q, this node=%q",
				job.ID, job.Name, job.NodeAffinity, s.nodeID)
			return false
		}
		// Affinity matches: always execute regardless of leadership.
		return true
	}
	// Cluster-wide job: gate on leadership.
	if s.leader == nil {
		return true
	}
	isLeader, err := s.leader.IsLeader()
	if err != nil {
		s.logWarn("Skipping job %d (%s): leader check failed: %v",
			job.ID, job.Name, err)
		return false
	}
	if !isLeader {
		s.logDebug("Skipping job %d (%s): not leader", job.ID, job.Name)
		return false
	}
	return true
}

// triggerJobByID immediately executes a specific job by ID.
func (s *SchedulerEngine) triggerJobByID(ctx context.Context, jobID uint64) {
	// Create a context with timeout for storage operations
	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	job, err := s.storage.GetJob(storageCtx, jobID, "")
	if err != nil {
		s.storageErrors.Add(1)
		return
	}
	if job == nil {
		return
	}

	// Acquire semaphore
	select {
	case s.semaphore <- struct{}{}:
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-s.semaphore }()
			s.executeJob(ctx, job)
		}()
	case <-ctx.Done():
		return
	}
}

// executeJob executes a job with timeout, error handling, and retries.
func (s *SchedulerEngine) executeJob(ctx context.Context, job *Job) {
	s.runningJobs.Add(1)
	defer s.runningJobs.Add(^uint64(0)) // Atomic decrement

	// Create a context with timeout for storage operations
	storageCtx, storageCancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer storageCancel()

	// Get handler
	handler, ok := s.getHandler(job.Handler)
	if !ok {
		s.handleJobFailure(storageCtx, job, HandlerNotFoundError{Handler: job.Handler})
		return
	}

	// Update job status to running
	job.Status = JobStatusRunning
	job.UpdatedAt = time.Now()
	if err := s.storage.UpdateJob(storageCtx, job); err != nil {
		s.storageErrors.Add(1)
		return
	}

	// Create execution record
	exec := &JobExecution{
		JobID:     job.ID,
		Status:    JobStatusRunning,
		StartedAt: time.Now(),
	}

	// Determine timeout
	timeout := job.Timeout
	if timeout == 0 {
		timeout = s.config.DefaultTimeout
	}

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Channel for result
	resultCh := make(chan struct {
		result *JobResult
		err    error
	}, 1)

	go func() {
		result, err := handler(execCtx, job)
		resultCh <- struct {
			result *JobResult
			err    error
		}{result, err}
	}()

	// Wait for result or timeout
	select {
	case <-execCtx.Done():
		if execCtx.Err() == context.DeadlineExceeded {
			s.handleJobTimeout(ctx, job, exec, timeout)
			return
		}
		s.handleJobFailure(ctx, job, execCtx.Err())
		return

	case res := <-resultCh:
		if res.err != nil {
			s.handleJobError(ctx, job, exec, res.err)
			return
		}
		s.handleJobSuccess(ctx, job, exec, res.result)
	}
}

// handleJobSuccess processes a successful job execution.
func (s *SchedulerEngine) handleJobSuccess(ctx context.Context, job *Job, exec *JobExecution, result *JobResult) {
	now := time.Now()
	exec.EndedAt = now
	exec.Duration = exec.EndedAt.Sub(exec.StartedAt)
	exec.Status = JobStatusCompleted
	if result != nil {
		exec.Output = result.Message
	}

	job.LastRun = now
	job.RunCount++
	job.UpdatedAt = now

	// For recurring jobs, calculate next run
	if job.Type == JobTypeRecurring {
		job.Status = JobStatusPending
		if err := s.calculateNextRun(job); err != nil {
			job.Status = JobStatusFailed
			job.LastError = err.Error()
		}
	} else {
		job.Status = JobStatusCompleted
	}

	// Create timeout context for storage operations (required for Raft-based storage)
	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	// Check if the job was canceled concurrently; if so, preserve the terminal status
	if current, getErr := s.storage.GetJob(storageCtx, job.ID, job.OwnerID); getErr == nil && current != nil {
		if current.Status == JobStatusCanceled || current.Status == JobStatusCompleted {
			job.Status = current.Status
		}
	}

	// Best-effort storage updates for background processing.
	// Errors are tracked via storageErrors counter for monitoring.
	if err := s.storage.UpdateJob(storageCtx, job); err != nil {
		s.storageErrors.Add(1)
	}
	if err := s.storage.SaveExecution(storageCtx, exec); err != nil {
		s.storageErrors.Add(1)
	}
	s.successCount.Add(1)
}

// handleJobError processes a job execution error with retry logic.
func (s *SchedulerEngine) handleJobError(ctx context.Context, job *Job, exec *JobExecution, err error) {
	now := time.Now()
	exec.EndedAt = now
	exec.Duration = exec.EndedAt.Sub(exec.StartedAt)
	exec.Status = JobStatusFailed
	exec.Error = err.Error()

	job.LastRun = now
	job.RunCount++
	job.FailCount++
	job.LastError = err.Error()
	job.UpdatedAt = now

	// Determine max retries with safe bounds check.
	// Use uint64 for comparison to avoid signed/unsigned issues.
	var maxRetriesU64 uint64
	if job.MaxRetries > 0 {
		maxRetriesU64 = uint64(job.MaxRetries)
	} else {
		// Use default retries, clamped to valid range.
		// Safe conversion: retries is checked to be >= 0 before conversion.
		retries := s.config.DefaultRetries
		if retries < 0 {
			retries = 0
		}
		maxRetriesU64 = uint64(retries) //nolint:gosec // Safe: retries >= 0 after bounds check above
	}

	// Check if we should retry
	if job.FailCount < maxRetriesU64 {
		job.Status = JobStatusPending
		// Calculate next retry time with backoff.
		// Cap fail count to prevent overflow in duration calculation.
		// Safe conversions: RetryBackoff is time.Duration (int64), and we check
		// backoffMultiplier against MaxInt64 to ensure the multiplication won't overflow.
		backoffMultiplier := job.FailCount
		retryBackoffU64 := uint64(s.config.RetryBackoff) //nolint:gosec // Safe: Duration values are bounded
		if retryBackoffU64 > 0 && backoffMultiplier > math.MaxInt64/retryBackoffU64 {
			backoffMultiplier = math.MaxInt64 / retryBackoffU64
		}
		job.NextRun = now.Add(s.config.RetryBackoff * time.Duration(backoffMultiplier)) //nolint:gosec // Safe: backoffMultiplier is bounded above
	} else {
		job.Status = JobStatusFailed
	}

	// Create timeout context for storage operations (required for Raft-based storage)
	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	// Check if the job was canceled concurrently; if so, preserve the terminal status
	if current, getErr := s.storage.GetJob(storageCtx, job.ID, job.OwnerID); getErr == nil && current != nil {
		if current.Status == JobStatusCanceled || current.Status == JobStatusCompleted {
			job.Status = current.Status
		}
	}

	// Best-effort storage updates for background processing.
	// Errors are tracked via storageErrors counter for monitoring.
	if storageErr := s.storage.UpdateJob(storageCtx, job); storageErr != nil {
		s.storageErrors.Add(1)
	}
	if storageErr := s.storage.SaveExecution(storageCtx, exec); storageErr != nil {
		s.storageErrors.Add(1)
	}
	s.failureCount.Add(1)
}

// handleJobTimeout processes a job that exceeded its timeout.
func (s *SchedulerEngine) handleJobTimeout(ctx context.Context, job *Job, exec *JobExecution, timeout time.Duration) {
	err := JobTimeoutError{JobID: job.ID, Timeout: timeout}
	s.handleJobError(ctx, job, exec, err)
}

// handleJobFailure processes a job failure without execution context.
func (s *SchedulerEngine) handleJobFailure(ctx context.Context, job *Job, err error) {
	now := time.Now()
	job.Status = JobStatusFailed
	job.LastError = err.Error()
	job.FailCount++
	job.UpdatedAt = now

	// Create timeout context for storage operations (required for Raft-based storage)
	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	// Check if the job was canceled concurrently; if so, preserve the terminal status
	if current, getErr := s.storage.GetJob(storageCtx, job.ID, job.OwnerID); getErr == nil && current != nil {
		if current.Status == JobStatusCanceled || current.Status == JobStatusCompleted {
			job.Status = current.Status
		}
	}

	// Best-effort storage update for background processing.
	// Errors are tracked via storageErrors counter for monitoring.
	if storageErr := s.storage.UpdateJob(storageCtx, job); storageErr != nil {
		s.storageErrors.Add(1)
	}
	s.failureCount.Add(1)
}

// validateJob validates a job definition.
func (s *SchedulerEngine) validateJob(job *Job) error {
	if job == nil {
		return InvalidJobError{Field: "job", Message: "job cannot be nil"}
	}

	if job.Name == "" {
		return InvalidJobError{Field: "name", Message: "job name is required"}
	}

	if job.Handler == "" {
		return InvalidJobError{Field: "handler", Message: "handler is required"}
	}

	// Handler existence is validated at execution time, not creation time.
	// This allows jobs to be created for handlers that may be registered later.

	// Validate based on job type
	switch job.Type {
	case JobTypeOneShot:
		if job.ExecuteAt.IsZero() {
			return InvalidJobError{Field: "execute_at", Message: "execute_at is required for one-shot jobs"}
		}
	case JobTypeRecurring:
		if job.Schedule == "" {
			return InvalidJobError{Field: "schedule", Message: "schedule is required for recurring jobs"}
		}
		// Validate cron expression
		if _, err := s.cronParser.Parse(job.Schedule); err != nil {
			return InvalidScheduleError{Schedule: job.Schedule, Cause: err}
		}
	case JobTypeDelayed:
		if job.Delay <= 0 {
			return InvalidJobError{Field: "delay", Message: "delay must be positive for delayed jobs"}
		}
	default:
		return InvalidJobError{Field: "type", Message: "invalid job type"}
	}

	return nil
}

// calculateNextRun calculates the next run time for a job.
func (s *SchedulerEngine) calculateNextRun(job *Job) error {
	now := time.Now()

	switch job.Type {
	case JobTypeOneShot:
		job.NextRun = job.ExecuteAt
	case JobTypeDelayed:
		job.NextRun = now.Add(job.Delay)
	case JobTypeRecurring:
		sched, err := s.cronParser.Parse(job.Schedule)
		if err != nil {
			return InvalidScheduleError{Schedule: job.Schedule, Cause: err}
		}
		if job.LastRun.IsZero() {
			job.NextRun = sched.Next(now)
		} else {
			job.NextRun = sched.Next(job.LastRun)
		}
	}

	return nil
}

// HandlerCount returns the number of registered handlers.
func (s *SchedulerEngine) HandlerCount() int {
	s.handlersMu.RLock()
	defer s.handlersMu.RUnlock()
	return len(s.handlers)
}

// Config returns the scheduler configuration.
func (s *SchedulerEngine) Config() *SchedulerConfig {
	return s.config
}
