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

package iam

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Batch operation error types.
var (
	// ErrBatchOperationFailed is returned when a batch operation fails.
	ErrBatchOperationFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.batch",
		Err:  errBatchOperationFailed,
	}

	// ErrBatchTimeout is returned when a batch operation times out.
	ErrBatchTimeout = &dberrors.QBError{
		Code: dberrors.ErrTimeout,
		Op:   "iam.batch",
		Err:  errBatchTimeout,
	}

	// ErrBatchValidationFailed is returned when batch validation fails.
	ErrBatchValidationFailed = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.batch",
		Err:  errBatchValidationFailed,
	}

	// ErrBatchCanceled is returned when a batch operation is canceled.
	ErrBatchCanceled = &dberrors.QBError{
		Code: dberrors.ErrCanceled,
		Op:   "iam.batch",
		Err:  errBatchCanceled,
	}

	// ErrBatchEmpty is returned when a batch operation has no items.
	ErrBatchEmpty = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.batch",
		Err:  errBatchEmpty,
	}

	// ErrInvalidBatchOptions is returned when batch options are invalid.
	ErrInvalidBatchOptions = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.batch",
		Err:  errInvalidBatchOptions,
	}
)

// Sentinel errors for batch operations.
var (
	errBatchOperationFailed  = &batchError{msg: "batch operation failed"}
	errBatchTimeout          = &batchError{msg: "batch operation timed out"}
	errBatchValidationFailed = &batchError{msg: "batch validation failed"}
	errBatchCanceled         = &batchError{msg: "batch operation canceled"}
	errBatchEmpty            = &batchError{msg: "batch operation has no items"}
	errInvalidBatchOptions   = &batchError{msg: "invalid batch options"}
)

// batchError is a simple error type for batch operations.
type batchError struct {
	msg string
}

func (e *batchError) Error() string {
	return e.msg
}

// Default batch configuration values.
const (
	DefaultBatchSize   = 100
	DefaultParallelism = 4
	DefaultTimeout     = 5 * time.Minute
	MaxBatchSize       = 10000
	MaxParallelism     = 32
)

// BatchManager manages batch import/export operations for IAM resources.
type BatchManager struct {
	identityStore *IdentityStore
	policyEngine  *PolicyEngine
	tenantMgr     TenantManager
	batchSize     int
	parallelism   int
}

// BatchManagerOptions configures the BatchManager.
type BatchManagerOptions struct {
	IdentityStore *IdentityStore
	PolicyEngine  *PolicyEngine
	TenantManager TenantManager
	BatchSize     int
	Parallelism   int
}

// NewBatchManager creates a new BatchManager with the provided options.
func NewBatchManager(opts BatchManagerOptions) *BatchManager {
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if batchSize > MaxBatchSize {
		batchSize = MaxBatchSize
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = DefaultParallelism
	}
	if parallelism > MaxParallelism {
		parallelism = MaxParallelism
	}

	return &BatchManager{
		identityStore: opts.IdentityStore,
		policyEngine:  opts.PolicyEngine,
		tenantMgr:     opts.TenantManager,
		batchSize:     batchSize,
		parallelism:   parallelism,
	}
}

// BatchImportRequest represents a request to import IAM resources in bulk.
type BatchImportRequest struct {
	TenantID string
	Users    []*UserImport
	Roles    []*RoleImport
	Policies []*PolicyImport
	Options  BatchImportOptions
}

// BatchImportOptions configures batch import operation behavior.
type BatchImportOptions struct {
	ContinueOnError bool          // Continue processing even if some items fail
	UpdateExisting  bool          // Update existing items instead of skipping
	DryRun          bool          // Validate without making changes
	Timeout         time.Duration // Overall timeout for batch operation
	BatchSize       int           // Number of items to process per batch
	Parallelism     int           // Number of parallel workers
	ProgressFunc    ProgressFunc  // Optional callback for progress updates
}

// ProgressFunc is a callback function for reporting batch import operation progress.
type ProgressFunc func(progress BatchImportProgress)

// BatchImportProgress represents the current progress of a batch import operation.
type BatchImportProgress struct {
	TotalItems      int
	ProcessedItems  int
	SuccessCount    int
	FailureCount    int
	CurrentType     string // "user", "role", "policy"
	PercentComplete float64
}

// BatchImportResult represents the result of a batch import operation.
type BatchImportResult struct {
	TotalItems     int
	SuccessCount   int
	FailureCount   int
	SkippedCount   int
	Errors         []BatchError
	Duration       time.Duration
	ProcessedItems map[string][]string // Type -> IDs processed
	DryRun         bool
}

// BatchError represents an error that occurred during batch processing.
type BatchError struct {
	Index int
	Type  string // "user", "role", "policy"
	ID    string
	Error error
}

// UserImport represents user data for batch import.
type UserImport struct {
	ID         string
	Email      string
	Username   string
	Name       string
	Attributes map[string]string
	Roles      []string

	// Lifecycle is the account state the import asserts. Empty means the import
	// says nothing, leaving a new user in the default pending.
	//
	// It carries what Enabled cannot. Enabled is a single bool over six states,
	// so an export of a PENDING account and an export of a DISABLED one both
	// reduce to false, and re-importing either produced a disabled account — a
	// round trip that quietly turned "waiting for its owner to activate" into
	// "an administrator turned this off". Enabled remains for importers that
	// genuinely know only that much.
	Lifecycle Lifecycle

	// Enabled states whether the imported user may authenticate. Nil means
	// the import says nothing, which leaves a new user enabled and an
	// existing user's current state untouched.
	//
	// It is a pointer for the same reason CreateUserOptions.Enabled is: a
	// plain bool cannot tell "this import says disabled" from "this import
	// does not mention it", and the zero value of the wrong one of those is
	// the destructive one. As a plain bool it took the false path on every
	// record that omitted the field, so re-importing an existing user without
	// mentioning Enabled SUSPENDED them.
	Enabled *bool
}

// RoleImport represents role data for batch import.
type RoleImport struct {
	ID          string
	Name        string
	Description string
	Permissions []string
	Attributes  map[string]interface{}
}

// PolicyImport represents policy data for batch import.
type PolicyImport struct {
	Name        string
	Description string
	Priority    int
	Subjects    SubjectSelector
	Rules       []Rule
	Variables   map[string]string
}

// BatchExportRequest represents a request to export IAM resources in bulk.
type BatchExportRequest struct {
	TenantID        string
	IncludeUsers    bool
	IncludeRoles    bool
	IncludePolicies bool
	Filter          ExportFilter
}

// ExportFilter defines filtering options for exports.
type ExportFilter struct {
	UserIDs       []string
	RoleIDs       []string
	PolicyNames   []string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	UpdatedAfter  *time.Time
	UpdatedBefore *time.Time
}

// BatchExportResult represents the result of a batch export operation.
type BatchExportResult struct {
	TenantID string
	Users    []*User
	Roles    []*Role
	Policies []*Policy
	Duration time.Duration
}

// Validate validates the BatchImportRequest.
func (r *BatchImportRequest) Validate() error {
	if r.TenantID == "" {
		return &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}

	totalItems := len(r.Users) + len(r.Roles) + len(r.Policies)
	if totalItems == 0 {
		return ErrBatchEmpty
	}

	return r.Options.Validate()
}

// Validate validates BatchImportOptions.
func (o *BatchImportOptions) Validate() error {
	if o.BatchSize < 0 {
		return &ValidationError{Field: "batch_size", Message: "batch size cannot be negative"}
	}
	if o.BatchSize > MaxBatchSize {
		return &ValidationError{Field: "batch_size", Message: "batch size exceeds maximum"}
	}
	if o.Parallelism < 0 {
		return &ValidationError{Field: "parallelism", Message: "parallelism cannot be negative"}
	}
	if o.Parallelism > MaxParallelism {
		return &ValidationError{Field: "parallelism", Message: "parallelism exceeds maximum"}
	}
	return nil
}

// Validate validates the BatchExportRequest.
func (r *BatchExportRequest) Validate() error {
	if r.TenantID == "" {
		return &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}

	// At least one resource type must be requested
	if !r.IncludeUsers && !r.IncludeRoles && !r.IncludePolicies {
		return &ValidationError{Field: "include_*", Message: "at least one resource type must be included"}
	}

	return nil
}

// workerPool manages a pool of workers for parallel batch processing.
type workerPool struct {
	workers    int
	jobChan    chan workerJob
	resultChan chan workerResult
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// workerJob represents a single job for the worker pool.
type workerJob struct {
	index    int
	itemType string
	itemID   string
	fn       func(context.Context) error
}

// workerResult represents the result of a worker job.
type workerResult struct {
	index    int
	itemType string
	itemID   string
	err      error
}

// newWorkerPool creates a new worker pool with the specified number of workers.
func newWorkerPool(ctx context.Context, workers int) *workerPool {
	poolCtx, cancel := context.WithCancel(ctx)
	wp := &workerPool{
		workers:    workers,
		jobChan:    make(chan workerJob, workers*2),
		resultChan: make(chan workerResult, workers*2),
		ctx:        poolCtx,
		cancel:     cancel,
	}
	return wp
}

// start starts the worker pool.
func (wp *workerPool) start() {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

// worker processes jobs from the job channel.
func (wp *workerPool) worker() {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.ctx.Done():
			return
		case job, ok := <-wp.jobChan:
			if !ok {
				return
			}

			var err error
			select {
			case <-wp.ctx.Done():
				err = wp.ctx.Err()
			default:
				err = job.fn(wp.ctx)
			}

			select {
			case wp.resultChan <- workerResult{
				index:    job.index,
				itemType: job.itemType,
				itemID:   job.itemID,
				err:      err,
			}:
			case <-wp.ctx.Done():
				return
			}
		}
	}
}

// submit submits a job to the worker pool.
func (wp *workerPool) submit(job workerJob) bool {
	select {
	case wp.jobChan <- job:
		return true
	case <-wp.ctx.Done():
		return false
	}
}

// stop stops the worker pool.
func (wp *workerPool) stop() {
	wp.cancel()
	close(wp.jobChan)
	wp.wg.Wait()
	close(wp.resultChan)
}

// progressTracker tracks batch import operation progress.
type progressTracker struct {
	totalItems     int64
	processedItems int64
	successCount   int64
	failureCount   int64
	currentType    atomic.Value
	progressFunc   ProgressFunc
}

// newProgressTracker creates a new progress tracker.
func newProgressTracker(totalItems int, progressFunc ProgressFunc) *progressTracker {
	pt := &progressTracker{
		totalItems:   int64(totalItems),
		progressFunc: progressFunc,
	}
	pt.currentType.Store("")
	return pt
}

// setCurrentType sets the current item type being processed.
func (pt *progressTracker) setCurrentType(itemType string) {
	pt.currentType.Store(itemType)
}

// recordSuccess records a successful item processing.
func (pt *progressTracker) recordSuccess() {
	atomic.AddInt64(&pt.processedItems, 1)
	atomic.AddInt64(&pt.successCount, 1)
	pt.notifyProgress()
}

// recordFailure records a failed item processing.
func (pt *progressTracker) recordFailure() {
	atomic.AddInt64(&pt.processedItems, 1)
	atomic.AddInt64(&pt.failureCount, 1)
	pt.notifyProgress()
}

// notifyProgress calls the progress callback if set.
func (pt *progressTracker) notifyProgress() {
	if pt.progressFunc == nil {
		return
	}

	total := atomic.LoadInt64(&pt.totalItems)
	processed := atomic.LoadInt64(&pt.processedItems)
	success := atomic.LoadInt64(&pt.successCount)
	failure := atomic.LoadInt64(&pt.failureCount)
	// The tracker stores a string on every update; before the first store
	// Load returns nil and the zero value is the right answer.
	currentType, _ := pt.currentType.Load().(string)

	var percentComplete float64
	if total > 0 {
		percentComplete = float64(processed) / float64(total) * 100
	}

	pt.progressFunc(BatchImportProgress{
		TotalItems:      int(total),
		ProcessedItems:  int(processed),
		SuccessCount:    int(success),
		FailureCount:    int(failure),
		CurrentType:     currentType,
		PercentComplete: percentComplete,
	})
}

// getProgress returns the current progress.
func (pt *progressTracker) getProgress() BatchImportProgress {
	total := atomic.LoadInt64(&pt.totalItems)
	processed := atomic.LoadInt64(&pt.processedItems)
	success := atomic.LoadInt64(&pt.successCount)
	failure := atomic.LoadInt64(&pt.failureCount)
	// The tracker stores a string on every update; before the first store
	// Load returns nil and the zero value is the right answer.
	currentType, _ := pt.currentType.Load().(string)

	var percentComplete float64
	if total > 0 {
		percentComplete = float64(processed) / float64(total) * 100
	}

	return BatchImportProgress{
		TotalItems:      int(total),
		ProcessedItems:  int(processed),
		SuccessCount:    int(success),
		FailureCount:    int(failure),
		CurrentType:     currentType,
		PercentComplete: percentComplete,
	}
}
