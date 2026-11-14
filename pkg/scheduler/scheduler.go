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
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
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

	// InitializeJobIDCounter makes the store's durable job ID counter ready
	// before the engine allocates from it. The engine calls it at Start and
	// refuses to start when it fails: a counter that restarts from a guess
	// reissues the IDs of jobs that already exist, and creating a job under a
	// reissued ID overwrites the job that holds it. It is the store's open
	// step, so a store also completes its one-time migrations here.
	InitializeJobIDCounter(ctx context.Context) error
}

// FiringClaimer arbitrates which node executes a given firing.
//
// It is separate from JobStorage because it is required only in a clustered
// deployment. The double-fire it prevents needs two nodes disagreeing about
// leadership, so a single-node scheduler has no race to guard and is not asked
// for a claim at all — that is a different topology, not a disabled safeguard.
// A clustered scheduler, by contrast, cannot be constructed without one.
type FiringClaimer interface {
	// ClaimFiring atomically claims the exclusive right to execute the
	// firing identified by claim, returning true only for the caller that
	// won it. The comparison and the write must be one indivisible
	// operation in the store — an Exists check followed by a Put is the bug
	// this method exists to remove, because the two nodes racing here are
	// on different hosts and no in-process lock separates them.
	//
	// A claim that is already held is reported as (false, nil): losing is a
	// verdict, not a failure. Only an undecidable claim returns an error,
	// and the engine treats that as "do not execute".
	ClaimFiring(ctx context.Context, claim *FiringClaim) (bool, error)
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

	// claims arbitrates firings across nodes. Non-nil exactly when this
	// engine was built for a cluster; a single-node engine has no race to
	// arbitrate and never consults it.
	claims FiringClaimer

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
	triggerCh chan triggerRequest

	// Semaphore for limiting concurrent jobs
	semaphore chan struct{}

	// Last tick time for stats
	lastTickTime atomic.Pointer[time.Time]
	startedAt    time.Time

	// Cron parser for recurring schedules
	cronParser cron.Parser

	// Storage error counter for monitoring
	storageErrors atomic.Uint64

	// Firing-claim counters. They are kept apart from storageErrors
	// because they answer a different operational question: a rising
	// claimsLost is healthy churn arbitration, while a rising claimErrors
	// means firings are being suppressed and the cluster is silently
	// under-executing.
	claimsWon   atomic.Uint64
	claimsLost  atomic.Uint64
	claimErrors atomic.Uint64

	// staleDispatches counts dispatches abandoned because the job moved to a
	// different firing between the claim and the execution.
	staleDispatches atomic.Uint64

	// jobLocks stripes serialize read-modify-write status transitions
	// per job (cancel/pause/resume/update/delete vs. asynchronous
	// execution-completion writes). A fixed stripe array bounds memory;
	// stripe collisions only cause harmless extra serialization.
	jobLocks [jobLockStripes]sync.Mutex

	// systemJobs holds the system job definitions registered with
	// RegisterSystemJob that are not yet installed in storage. The run loop
	// installs them and removes each one once the store has accepted it.
	systemJobs   map[uint64]*Job
	systemJobsMu sync.Mutex
}

// jobLockStripes is the number of striped per-job mutexes. Power of
// two so the stripe index reduces to a bit mask.
const jobLockStripes = 64

// Verify SchedulerEngine implements Scheduler interface.
var _ Scheduler = (*SchedulerEngine)(nil)

// jobLock returns the stripe mutex that serializes status
// transitions for the given job ID.
func (s *SchedulerEngine) jobLock(jobID uint64) *sync.Mutex {
	return &s.jobLocks[jobID&(jobLockStripes-1)]
}

// JobTenantID returns the tenant scope used for storage lookups of job. System
// and delegated jobs live in the system namespace (empty tenant ID); only
// tenant-owned jobs are tenant-scoped. job must not be nil.
//
// EXPORTED BECAUSE THE SCOPE IS PART OF A JOB'S IDENTITY, NOT A STORAGE
// DETAIL, and anything outside this package that addresses a job by id needs
// this exact rule rather than a copy of it. A job id is unique only WITHIN a
// scope: id 7 names a system job and a job in every tenant, so an id carried
// without its scope names several records and resolves to whichever the caller
// happens to reach.
//
// The cost of a copy that drifts is not a wrong answer somewhere visible. It is
// silent and total, and it has already happened once here: the firing-claim
// mark was first keyed with a hand-copied version of this rule, and deleting a
// tenant's job 7 erased the SYSTEM job 7's claim mark — un-deciding a firing
// that had been decided (see storage/quicbaas/firing_claim.go, keyForClaim).
// The remedy then was to share this function rather than re-derive it, and the
// same remedy is why callers outside the package get it rather than four lines
// they are invited to reimplement.
func JobTenantID(job *Job) string {
	if job.OwnerType == OwnerTypeTenant {
		return job.OwnerID
	}
	return ""
}

// isJobNotFound reports whether err indicates a missing job record.
// Storage implementations return JobNotFoundError by value or by
// pointer, so both forms are matched.
func isJobNotFound(err error) bool {
	var v JobNotFoundError
	if errors.As(err, &v) {
		return true
	}
	var p *JobNotFoundError
	return errors.As(err, &p)
}

// New creates a single-node scheduler engine.
//
// A single-node engine takes no firing claims. The double-fire claims prevent
// requires two nodes disagreeing about Raft leadership, which cannot happen
// here: there is one scheduler, its dispatch is already serialized by the tick
// loop, and there is no peer to race. Cluster deployments use
// NewWithClustering, which requires a claim arbiter and refuses to build
// without one.
func New(config *SchedulerConfig, storage JobStorage) *SchedulerEngine {
	return newEngine(config, storage, "", nil, nil)
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
// The table above describes which node is ELIGIBLE to fire. Eligibility is not
// exclusivity: during leader churn two nodes are eligible for the same
// cluster-wide job at the same time. Every due job is therefore also arbitrated
// by a FiringClaim, which storage must supply — so storage MUST implement
// FiringClaimer, and construction fails with ClaimUnsupportedError when it does
// not. Refusing at startup is the point: a cluster that silently ran without
// claims would look healthy until the first leadership change, which is exactly
// when a duplicate certificate gets issued.
func NewWithClustering(config *SchedulerConfig, storage JobStorage, nodeID string, leader LeaderChecker) (*SchedulerEngine, error) {
	claimer, ok := storage.(FiringClaimer)
	if !ok {
		return nil, ClaimUnsupportedError{Backend: fmt.Sprintf("%T", storage)}
	}
	return newEngine(config, storage, nodeID, leader, claimer), nil
}

// newEngine is the shared constructor behind New and NewWithClustering.
func newEngine(config *SchedulerConfig, storage JobStorage, nodeID string, leader LeaderChecker, claims FiringClaimer) *SchedulerEngine {
	if config == nil {
		config = DefaultConfig()
	}

	return &SchedulerEngine{
		config:     config,
		storage:    storage,
		nodeID:     nodeID,
		leader:     leader,
		claims:     claims,
		handlers:   make(map[string]JobHandler),
		triggerCh:  make(chan triggerRequest, 100),
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

	// The job ID counter is loaded before anything can be scheduled. A store
	// that cannot produce it fails the start: the alternative is allocating
	// from zero and reissuing the IDs of jobs that already exist.
	if err := s.initializeJobIDs(ctx); err != nil {
		s.running.Store(false)
		return err
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

// initializeJobIDs loads the store's job ID counter, bounded by the
// configured storage timeout.
func (s *SchedulerEngine) initializeJobIDs(ctx context.Context) error {
	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()
	if err := s.storage.InitializeJobIDCounter(storageCtx); err != nil {
		return ErrStorageError{Op: "InitializeJobIDCounter", Cause: err}
	}
	return nil
}

// Stop gracefully stops the scheduler.
//
// Stop is terminal even when the scheduler is not running yet. A Stop that
// arrives BEFORE the matching Start still records the stop, so the later
// Start refuses instead of bringing a poll loop up against storage the
// stopper has already closed. Without that, "stop an engine that never
// started" was a silent no-op and the ordering decided whether a goroutine
// leaked: an owner that tore down between constructing the engine and
// starting it -- which is exactly what Shutdown after a failed Start does --
// got its Stop rejected with "not running" and then watched the engine come
// up anyway. The error it returns in that case is unchanged; only the
// recorded state is.
func (s *SchedulerEngine) Stop(ctx context.Context) error {
	if !s.running.CompareAndSwap(true, false) {
		// Not running: either never started, or already stopped. Either way
		// the caller's intent is that this engine must not run from here on,
		// so make it stick before reporting that there was nothing to stop.
		s.stopped.Store(true)
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
	// The ID range at and above SystemJobIDFloor belongs to system jobs,
	// which are installed by RegisterSystemJob. Letting a scheduled job take
	// one would overwrite the system job that owns it.
	if job.ID >= SystemJobIDFloor {
		return InvalidJobError{Field: "id", Message: "IDs at or above SystemJobIDFloor are reserved for system jobs"}
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

	// Serialize with concurrent execution-completion writes so a
	// successful cancel cannot be clobbered (see jobLocks).
	lock := s.jobLock(jobID)
	lock.Lock()
	defer lock.Unlock()

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

	// Serialize with concurrent execution-completion writes so a
	// successful cancel cannot be clobbered (see jobLocks).
	lock := s.jobLock(jobID)
	lock.Lock()
	defer lock.Unlock()

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

	// Send to trigger channel (non-blocking with buffer).
	//
	// THE SCOPE TRAVELS WITH THE ID, derived by the scheduler's own rule
	// (JobTenantID) from the job just loaded — never the caller's raw
	// argument, and never dropped. A job id is unique only WITHIN a scope:
	// id 7 names a system job and a job in every tenant. This queue used to
	// carry the bare id and the drain reloaded with the system scope, so a
	// tenant job's trigger either found nothing or — the silent and total
	// case — executed the SYSTEM job that happened to share its id. That is
	// the same keyForClaim lesson JobTenantID exists to prevent, one channel
	// over.
	select {
	case s.triggerCh <- triggerRequest{jobID: jobID, scope: JobTenantID(job)}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// A FULL QUEUE IS A REFUSAL, NOT A SUCCESS. This used to return nil
		// on the strength of "the next tick will pick it up" — which is true
		// only of a job that is DUE. A paused job, or a stranded firing at or
		// below its monotonic claim mark, is never picked up by the tick, so
		// the dropped trigger was a repair reported done that never happened.
		// The caller decides whether to retry; this reports the truth.
		return TriggerQueueFullError{JobID: jobID}
	}
}

// RetriggerStrandedFiring dispatches a job whose firing the claim mark has
// made unclaimable, BOUND to the firing that was observed stranded.
//
// The binding is the difference from TriggerJob, and it is the double-fire
// guard: between the caller's liveness observation ("no run is executing
// this firing") and the dispatch actually running, the original claim winner
// can begin executing after all, or the job can complete and move to a new
// firing. An unbound manual trigger would then run the handler CONCURRENTLY
// with the legitimate execution — for the non-idempotent handlers the claim
// exists to protect, the exact outcome the whole mechanism prevents. Carrying
// the observed firing makes executeJob re-verify, under the stripe lock that
// guards the running-status write, that the job is still Pending at exactly
// that firing; anything else declines as a stale dispatch.
func (s *SchedulerEngine) RetriggerStrandedFiring(ctx context.Context, jobID uint64, tenantID string, firing time.Time) error {
	if !s.running.Load() {
		return SchedulerStoppedError{}
	}
	if firing.IsZero() {
		return InvalidJobError{
			Field:   "firing",
			Message: "a stranded-firing repair binds to the firing that was observed stranded; zero binds to none",
		}
	}

	job, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return ErrStorageError{Op: "GetJob", Cause: err}
	}
	if job == nil {
		return JobNotFoundError{JobID: jobID}
	}
	if job.Status != JobStatusPending || !job.NextRun.Equal(firing) {
		// The observation is already stale: the firing moved, or the job is
		// not sitting Pending on it. Refusing here is cheap; the binding in
		// executeJob remains the authoritative check.
		return InvalidJobError{
			Field:   "firing",
			Message: "the job is no longer pending on the observed firing; the repair's observation is stale",
		}
	}

	select {
	case s.triggerCh <- triggerRequest{jobID: jobID, scope: JobTenantID(job), firing: firing}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return TriggerQueueFullError{JobID: jobID}
	}
}

// TriggerQueueFullError reports a manual trigger or repair the engine could
// not accept because its trigger queue is full. Nothing was dispatched and
// nothing will retry it; the caller owns the decision.
type TriggerQueueFullError struct {
	JobID uint64
}

// Error implements the error interface.
func (e TriggerQueueFullError) Error() string {
	return "scheduler: the trigger queue is full; job " + strconv.FormatUint(e.JobID, 10) +
		" was NOT dispatched and the tick path will not retry a manual trigger"
}

// PauseJob pauses a scheduled job.
func (s *SchedulerEngine) PauseJob(ctx context.Context, jobID uint64, tenantID string) error {
	if !s.running.Load() {
		return SchedulerStoppedError{}
	}

	lock := s.jobLock(jobID)
	lock.Lock()
	defer lock.Unlock()

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

	lock := s.jobLock(jobID)
	lock.Lock()
	defer lock.Unlock()

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

	lock := s.jobLock(jobID)
	lock.Lock()
	defer lock.Unlock()

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
		if err := s.rescheduleResumed(job); err != nil {
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

	lock := s.jobLock(jobID)
	lock.Lock()
	defer lock.Unlock()

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
		if err := s.rescheduleResumed(job); err != nil {
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

	lock := s.jobLock(jobID)
	lock.Lock()
	defer lock.Unlock()

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
		// A recurring job's new schedule starts a new firing
		if job.Type == JobTypeRecurring {
			if err := s.startNextFiring(job); err != nil {
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

	lock := s.jobLock(jobID)
	lock.Lock()
	defer lock.Unlock()

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

// FiringClaimsWon returns how many scheduled firings this node has claimed and
// therefore executed.
func (s *SchedulerEngine) FiringClaimsWon() uint64 {
	return s.claimsWon.Load()
}

// FiringClaimsLost returns how many scheduled firings this node stood down from
// because another node already held the claim. A non-zero value is the expected
// signature of leader churn, not a fault.
func (s *SchedulerEngine) FiringClaimsLost() uint64 {
	return s.claimsLost.Load()
}

// FiringClaimErrors returns how many firings this node suppressed because the
// store could not decide the claim. Unlike a lost claim this means the firing
// may not have run anywhere, so it is the counter to alert on.
func (s *SchedulerEngine) FiringClaimErrors() uint64 {
	return s.claimErrors.Load()
}

// StaleDispatches returns how many dispatches were abandoned because the job
// had moved to a different firing between the claim and the execution. A
// non-zero value is correct behaviour, not a fault: it is the count of
// double-fires this check prevented.
func (s *SchedulerEngine) StaleDispatches() uint64 {
	return s.staleDispatches.Load()
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
			s.installSystemJobs(s.ctx)
			s.processDueJobs(s.ctx)

		case req := <-s.triggerCh:
			s.triggerJobByID(s.ctx, req)
		}
	}
}

// sweepLogPrefix begins every record the due-job sweep emits.
const sweepLogPrefix = "scheduler due-job sweep"

// dueJobIDs returns the IDs of the jobs a sweep found, for its log record.
func dueJobIDs(jobs []*Job) []uint64 {
	ids := make([]uint64, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	return ids
}

// processDueJobs fetches and executes jobs that are due.
func (s *SchedulerEngine) processDueJobs(ctx context.Context) {
	now := time.Now()

	// Create a context with timeout for storage operations
	// This is required for Raft-based storage which needs a deadline
	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	jobs, err := s.storage.GetJobsDueForExecution(storageCtx, now, s.config.MaxConcurrentJobs)
	if err != nil {
		s.logError(sweepLogPrefix+" failed: component=scheduler error=%q", err.Error())
		s.storageErrors.Add(1)
		return
	}

	// A sweep that finds nothing carries no information: the tick runs
	// every TickInterval, so logging it would bury every other record.
	if len(jobs) > 0 {
		s.logDebug(sweepLogPrefix+" found due jobs: component=scheduler due=%d job_ids=%v",
			len(jobs), dueJobIDs(jobs))
	}

	for _, job := range jobs {
		if !s.shouldExecute(job) {
			continue
		}

		// Take the concurrency slot before claiming. A claim is a
		// one-shot right to fire, so claiming a job this tick cannot
		// dispatch would retire the firing without ever running it.
		select {
		case s.semaphore <- struct{}{}:
		default:
			// Semaphore full, skip this job for now
			s.logWarn("Semaphore full, skipping job %d for now", job.ID)
			continue
		}

		// The leader gate above is advisory — during leader churn more
		// than one node passes it for the same job. The claim is
		// authoritative.
		//
		// KNOWN ACCEPTED WINDOW (docs/18 V4): a won claim is never
		// released, and the job only moves to "running" later inside
		// executeJob. A winner that dies in between — or whose
		// running-transition write fails — strands that firing instead of
		// retrying next tick. Releasing on decline would reopen the
		// double-fire, so the fix needs a liveness signal: the SDLC
		// module's WP10 stall detection and lease reaping, which also
		// covers the pre-existing stranding of jobs stuck in "running".
		// Interim recovery is TriggerJob, which bypasses the claim.
		if !s.claimFiring(storageCtx, job) {
			<-s.semaphore
			continue
		}

		s.logDebug("Executing job %d: %s", job.ID, job.Name)
		s.wg.Add(1)
		go func(j *Job) {
			defer s.wg.Done()
			defer func() { <-s.semaphore }()
			s.executeJob(ctx, j, j.NextRun)
		}(job)
	}
}

// claimFiring asks the claim arbiter for the exclusive right to execute job's
// currently scheduled firing, and reports whether this node may proceed.
//
// The claim identity is the job's own NextRun, read from the replicated job
// record rather than from this node's clock, so every node contending for the
// same firing presents the same identity to the store. The store decides.
//
// Three outcomes, two of which stop the dispatch:
//
//   - won: this node executes the firing.
//   - already held: another node owns it. Normal churn arbitration, counted
//     but not logged as a failure.
//   - undecidable: the store could not answer. Execution is suppressed. This
//     is deliberately fail-closed — the handlers this gate exists to protect
//     are the non-idempotent ones (certificate issuance, platform
//     automation), where a duplicate firing costs more than a missed cycle.
//     The job record is left untouched, so the firing is recoverable by
//     operator action once the store is reachable again.
func (s *SchedulerEngine) claimFiring(ctx context.Context, job *Job) bool {
	// Single-node engines have no peer to race and take no claims.
	if s.claims == nil {
		return true
	}

	claim := &FiringClaim{
		JobID:      job.ID,
		TenantID:   JobTenantID(job),
		FiringTime: job.NextRun,
		Owner:      s.nodeID,
		ClaimedAt:  time.Now(),
	}

	won, err := s.claims.ClaimFiring(ctx, claim)
	switch {
	case err != nil:
		s.claimErrors.Add(1)
		s.logError("Skipping job %d (%s): firing claim for %s undecided: %v",
			job.ID, job.Name, job.NextRun, err)
		return false
	case !won:
		s.claimsLost.Add(1)
		s.logDebug("Skipping job %d (%s): firing %s already claimed by another node",
			job.ID, job.Name, job.NextRun)
		return false
	default:
		s.claimsWon.Add(1)
		return true
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

// triggerRequest is one queued manual trigger: the job, the scope it was
// resolved in, and — for a stranded-firing repair — the firing it is bound
// to. See TriggerJob for the scope and RetriggerStrandedFiring for the
// binding.
type triggerRequest struct {
	jobID uint64
	scope string

	// firing, when non-zero, binds the dispatch to ONE observed firing:
	// executeJob re-verifies, under the stripe lock, that the job is still
	// Pending at exactly this NextRun before the handler runs. Zero is an
	// operator's "run now", bound to no particular firing.
	firing time.Time
}

// triggerJobByID immediately executes a specific job by ID, reloading it in
// the scope the trigger resolved it under.
func (s *SchedulerEngine) triggerJobByID(ctx context.Context, req triggerRequest) {
	// Create a context with timeout for storage operations
	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	job, err := s.storage.GetJob(storageCtx, req.jobID, req.scope)
	if err != nil {
		s.storageErrors.Add(1)
		return
	}
	if job == nil {
		return
	}
	firing := req.firing

	// Acquire semaphore
	select {
	case s.semaphore <- struct{}{}:
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-s.semaphore }()
			// A repair carries the firing it was bound to; an operator's
			// "run now" carries zero and is bound to no particular firing.
			s.executeJob(ctx, job, firing)
		}()
	case <-ctx.Done():
		return
	}
}

// executeJob executes a job with timeout, error handling, and retries.
// executeJob runs one job.
//
// claimedFiring is the scheduled firing this dispatch was authorised for — the
// NextRun the job carried when it was read from the due list and claimed. A
// zero value means "no particular firing", which is what a manual trigger is.
func (s *SchedulerEngine) executeJob(ctx context.Context, job *Job, claimedFiring time.Time) {
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

	// Re-check the job under its stripe lock before executing: a
	// concurrent cancel, pause, or delete between dispatch and
	// execution must win. The running-status write happens under the
	// same lock so it cannot clobber a concurrent transition.
	lock := s.jobLock(job.ID)
	lock.Lock()
	current, err := s.storage.GetJob(storageCtx, job.ID, JobTenantID(job))
	if err != nil || current == nil {
		lock.Unlock()
		if err != nil && !isJobNotFound(err) {
			s.storageErrors.Add(1)
		}
		return
	}
	if current.Status == JobStatusCanceled || current.Status == JobStatusCompleted || current.Status == JobStatusPaused {
		lock.Unlock()
		s.logDebug("Skipping job %d (%s): status is %s", current.ID, current.Name, current.Status)
		return
	}
	// A RUNNING JOB IS NEVER DOUBLE-RUN, whatever dispatched us. Claimed
	// dispatches already require Pending below; this closes the manual and
	// repair paths, whose TriggerJob-time precheck races the execution that
	// can begin in between. Without it, a zero-firing dispatch would write
	// Running over Running and invoke the handler concurrently with the
	// live execution.
	if current.Status == JobStatusRunning {
		lock.Unlock()
		s.staleDispatches.Add(1)
		s.logDebug("Skipping job %d (%s): already running", current.ID, current.Name)
		return
	}

	// The claim authorised ONE firing. Between the due-list read and here the
	// job may have moved on — completed and rescheduled, retried onto a new
	// backoff time, or been updated — and the record now in front of us is a
	// DIFFERENT firing that another node may already have claimed and be
	// running. Executing it would double-fire through the back door: the claim
	// was won honestly, for a firing that no longer exists.
	//
	// So the firing identity is re-checked here, under the same lock that
	// guards the running-status write, and a mismatch declines. Found by
	// review; the claim alone is not sufficient because claiming and executing
	// are separated by a goroutine hop.
	if !claimedFiring.IsZero() {
		if current.Status != JobStatusPending || !current.NextRun.Equal(claimedFiring) {
			lock.Unlock()
			s.staleDispatches.Add(1)
			s.logDebug("Skipping job %d (%s): claimed firing %s, record now %s at %s",
				current.ID, current.Name, claimedFiring, current.Status, current.NextRun)
			return
		}
	}

	job = current

	// Update job status to running
	job.Status = JobStatusRunning
	job.UpdatedAt = time.Now()
	if err := s.storage.UpdateJob(storageCtx, job); err != nil {
		lock.Unlock()
		s.storageErrors.Add(1)
		return
	}
	lock.Unlock()

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

	recordResult := func(res struct {
		result *JobResult
		err    error
	}) {
		if res.err != nil {
			// A handler that honours cancellation returns its context's
			// error the moment the engine stops, and that return races the
			// Done case above. Either way the run was cut short by the
			// shutdown, so it is the same outcome whichever case wins.
			if ctx.Err() != nil {
				s.handleInterruptedRun(ctx, job, exec)
				return
			}
			s.handleJobError(ctx, job, exec, res.err)
			return
		}
		s.handleJobSuccess(ctx, job, exec, res.result)
	}
	recordResultUnlessTimedOut := func(res struct {
		result *JobResult
		err    error
	}) {
		if execCtx.Err() == context.DeadlineExceeded {
			s.handleJobTimeout(ctx, job, exec, timeout)
			return
		}
		recordResult(res)
	}

	// Wait for result or timeout
	select {
	case <-execCtx.Done():
		switch execCtx.Err() {
		case context.DeadlineExceeded:
			s.handleJobTimeout(ctx, job, exec, timeout)
			return
		case context.Canceled:
			if ctx.Err() != nil {
				select {
				case res := <-resultCh:
					recordResult(res)
					return
				default:
				}
				s.handleInterruptedRun(ctx, job, exec)
				return
			}
		}
		s.handleJobFailure(ctx, job, execCtx.Err())
		return

	case res := <-resultCh:
		recordResultUnlessTimedOut(res)
	}
}

// outcomeContext returns the context a run's outcome is written under.
//
// DETACHED FROM THE ENGINE'S CANCELLATION, because the outcome of a run that
// ends as the engine stops still has to reach the store. Derived from the
// engine context, the write would be refused by any store that honours
// cancellation — the job would stay Running, a status no tick ever picks up,
// and the run would be lost without a trace. Stop waits for these writes (they
// run inside the engine's wait group), and DefaultTimeout bounds each one.
func (s *SchedulerEngine) outcomeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), s.config.DefaultTimeout)
}

// handleInterruptedRun concludes a run that the engine's shutdown cut short.
//
// AN INTERRUPTED RUN IS NOT A FAILED RUN. The handler did not fail; the
// process stopped under it. Recording it as a failure charged a retry and, for
// a user recurring job, left it Failed for good — only system jobs are
// re-armed at boot. So the run is treated as not having happened: the job
// returns to Pending with its retry budget, RunCount and LastRun untouched, and
// the interrupted firing runs again on the next tick of whichever node picks it
// up.
//
// The firing is re-armed at now rather than at its original time when that
// time has passed. On a cluster the original firing has been claimed, and the
// claim mark only moves forward, so re-arming at the same instant would leave
// the firing at its mark — unclaimable, and never run again. Now is past the
// mark and already due. A firing still in the future (a manual trigger ran the
// job early) keeps its time.
//
// The execution record says what happened, with status Canceled, so the
// history shows the partial run a non-idempotent handler may have begun.
func (s *SchedulerEngine) handleInterruptedRun(ctx context.Context, job *Job, exec *JobExecution) {
	now := time.Now()
	exec.EndedAt = now
	exec.Duration = exec.EndedAt.Sub(exec.StartedAt)
	exec.Status = JobStatusCanceled
	exec.Error = RunInterruptedError{JobID: job.ID}.Error()

	job.Status = JobStatusPending
	if job.NextRun.Before(now) {
		job.NextRun = now
	}
	job.UpdatedAt = now

	storageCtx, cancel := s.outcomeContext(ctx)
	defer cancel()

	s.logWarn("Job %d (%s) interrupted by scheduler shutdown; re-armed for %s",
		job.ID, job.Name, job.NextRun)
	s.persistJobOutcome(storageCtx, job)
	if err := s.storage.SaveExecution(storageCtx, exec); err != nil {
		s.storageErrors.Add(1)
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
	job.FailCount = 0 // a successful run ends its firing's retry budget
	job.UpdatedAt = now

	// For recurring jobs, move on to the next firing
	if job.Type == JobTypeRecurring {
		job.Status = JobStatusPending
		if err := s.startNextFiring(job); err != nil {
			job.Status = JobStatusFailed
			job.LastError = err.Error()
		}
	} else {
		job.Status = JobStatusCompleted
	}

	// The outcome is written even when the engine is stopping; see
	// outcomeContext.
	storageCtx, cancel := s.outcomeContext(ctx)
	defer cancel()

	// Persist the outcome under the job's stripe lock so a concurrent
	// cancel or delete cannot be clobbered by this write.
	s.persistJobOutcome(storageCtx, job)
	if err := s.storage.SaveExecution(storageCtx, exec); err != nil {
		s.storageErrors.Add(1)
	}
	s.successCount.Add(1)
}

// persistJobOutcome writes an execution outcome for job while
// honoring concurrent lifecycle transitions. It runs under the job's
// stripe lock and, atomically with respect to the other status
// writers:
//
//   - preserves a concurrently set terminal status (canceled or
//     completed) instead of overwriting it, and
//   - skips the write entirely when the job record no longer exists,
//     so completed executions never resurrect a deleted job.
//
// Writes remain best effort (errors are tracked via storageErrors),
// matching the background-processing semantics of the callers.
func (s *SchedulerEngine) persistJobOutcome(ctx context.Context, job *Job) {
	lock := s.jobLock(job.ID)
	lock.Lock()
	defer lock.Unlock()

	current, getErr := s.storage.GetJob(ctx, job.ID, JobTenantID(job))
	switch {
	case isJobNotFound(getErr), getErr == nil && current == nil:
		// Job was deleted concurrently; do not resurrect it.
		return
	case getErr == nil && (current.Status == JobStatusCanceled || current.Status == JobStatusCompleted):
		// Preserve the terminal status set by a concurrent transition.
		job.Status = current.Status
	}

	if err := s.storage.UpdateJob(ctx, job); err != nil {
		s.storageErrors.Add(1)
	}
}

// applyRunFailure records one failed run on job and decides whether it retries.
//
// The retry budget (MaxRetries, or the configured DefaultRetries) is PER
// FIRING, not per job lifetime. Within a firing each failure is retried with
// backoff until the budget is spent. A spent budget fails a one-shot or
// delayed job; a recurring job instead moves on to its next scheduled firing,
// and startNextFiring gives that firing a fresh budget. Without that reset a
// recurring job accumulated failures across its whole life and retired itself
// after DefaultRetries failures spread over any length of time.
//
// It mutates job and writes nothing: the caller owns persistence, because the
// two callers persist differently — handleJobError goes through
// persistJobOutcome, which takes the job's stripe lock itself, while
// RecoverAbandonedRun is ALREADY holding that lock and writes directly.
//
// SHARED RATHER THAN COPIED, and that is the point of it existing. The retry
// budget and the backoff schedule are policy, and a second copy of a policy is
// a policy that will differ from the first one eventually — silently, since
// both would keep working. A job abandoned by a dead node and a job whose
// handler returned an error are the same event as far as the retry budget is
// concerned: a run that did not produce a result.
func (s *SchedulerEngine) applyRunFailure(job *Job, reason string, now time.Time) {
	job.LastRun = now
	job.RunCount++
	job.FailCount++
	job.LastError = reason
	job.UpdatedAt = now

	// Determine max retries with safe bounds check.
	// Use uint64 for comparison to avoid signed/unsigned issues.
	var maxRetriesU64 uint64
	if job.MaxRetries > 0 {
		maxRetriesU64 = uint64(job.MaxRetries)
	} else {
		// Use default retries; a negative default means none.
		maxRetriesU64 = conv.ClampIntToUint64(s.config.DefaultRetries)
	}

	// Retry within this firing while its budget lasts.
	if job.FailCount < maxRetriesU64 {
		job.Status = JobStatusPending
		// Calculate next retry time with backoff.
		// Cap fail count so the multiplication cannot overflow a Duration.
		backoffMultiplier := job.FailCount
		retryBackoffU64 := conv.ClampInt64ToUint64(int64(s.config.RetryBackoff))
		if retryBackoffU64 > 0 && backoffMultiplier > math.MaxInt64/retryBackoffU64 {
			backoffMultiplier = math.MaxInt64 / retryBackoffU64
		}
		job.NextRun = now.Add(s.config.RetryBackoff * time.Duration(conv.ClampUint64ToInt64(backoffMultiplier)))
		return
	}

	// This firing's budget is spent. A recurring job is not retired by one bad
	// firing: it moves on to its next scheduled firing with a fresh budget.
	// Every other job type has no next firing, so it fails.
	if job.Type != JobTypeRecurring {
		job.Status = JobStatusFailed
		return
	}
	if err := s.startNextFiring(job); err != nil {
		job.Status = JobStatusFailed
		job.LastError = err.Error()
		return
	}
	job.Status = JobStatusPending
}

// handleJobError processes a job execution error with retry logic.
func (s *SchedulerEngine) handleJobError(ctx context.Context, job *Job, exec *JobExecution, err error) {
	now := time.Now()
	exec.EndedAt = now
	exec.Duration = exec.EndedAt.Sub(exec.StartedAt)
	exec.Status = JobStatusFailed
	exec.Error = err.Error()

	s.applyRunFailure(job, err.Error(), now)

	// The outcome is written even when the engine is stopping; see
	// outcomeContext.
	storageCtx, cancel := s.outcomeContext(ctx)
	defer cancel()

	// Persist the outcome under the job's stripe lock so a concurrent
	// cancel or delete cannot be clobbered by this write.
	s.persistJobOutcome(storageCtx, job)
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

	// The outcome is written even when the engine is stopping; see
	// outcomeContext.
	storageCtx, cancel := s.outcomeContext(ctx)
	defer cancel()

	// Persist the outcome under the job's stripe lock so a concurrent
	// cancel or delete cannot be clobbered by this write.
	s.persistJobOutcome(storageCtx, job)
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

// calculateNextRun calculates the next run time for a job. A recurring job's
// next firing follows its last run, or now when it has never run.
func (s *SchedulerEngine) calculateNextRun(job *Job) error {
	return s.calculateNextRunAfter(job, job.LastRun)
}

// calculateNextRunAfter calculates the next run time for a job, taking a
// recurring job's next firing as the schedule's first occurrence strictly
// after the given instant — or after now, when the instant is zero.
func (s *SchedulerEngine) calculateNextRunAfter(job *Job, after time.Time) error {
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
		if after.IsZero() {
			after = now
		}
		job.NextRun = sched.Next(after)
	}

	return nil
}

// rescheduleResumed recalculates the next run of a resumed job whose scheduled
// run passed while it was paused. For a recurring job that skips to a new
// firing, which starts with a fresh retry budget.
//
// A RESUMED RECURRING JOB COUNTS FROM THE RESUME, NEVER FROM ITS LAST RUN. The
// last run can be arbitrarily old — the job sat paused — and the schedule's
// next occurrence after it is then already in the past: for an "@every"
// schedule the job would fire on the very next tick instead of on its
// schedule. See resumeBase for the one case the last run still wins.
func (s *SchedulerEngine) rescheduleResumed(job *Job) error {
	if job.Type == JobTypeRecurring {
		return s.startFiringAfter(job, resumeBase(job.LastRun, time.Now()))
	}
	return s.calculateNextRun(job)
}

// resumeBase returns the instant a resumed recurring job's next firing is
// computed from: now, or the last run when that is later.
//
// The last run wins only when it is AHEAD of this node's clock. It was stamped
// by whichever node ran the job, and if that node's clock leads this one, the
// schedule's next occurrence after this node's now can be the very firing that
// just ran — at or below its claim mark, and so unclaimable forever.
func resumeBase(lastRun, now time.Time) time.Time {
	if lastRun.After(now) {
		return lastRun
	}
	return now
}

// startNextFiring schedules job's next firing and gives it a fresh retry
// budget. The retry budget belongs to a firing, so every path that starts a
// new one — a successful recurring run, a recurring firing whose budget is
// spent, a schedule change, a resume past a missed firing, a system job
// re-armed at install — resets FailCount here rather than inheriting the
// failures of firings that are over.
func (s *SchedulerEngine) startNextFiring(job *Job) error {
	return s.startFiringAfter(job, job.LastRun)
}

// startFiringAfter is startNextFiring with the instant the firing is computed
// from made explicit. It is the one place a new firing's retry budget is reset.
func (s *SchedulerEngine) startFiringAfter(job *Job, after time.Time) error {
	if err := s.calculateNextRunAfter(job, after); err != nil {
		return err
	}
	job.FailCount = 0
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
