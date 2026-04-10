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

// Package quicbaas provides scheduler job storage using QuicBaaS's KVStore interface.
package quicbaas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// Key prefixes for storage partitioning.
const (
	// SystemJobsPrefix stores system-owned jobs.
	SystemJobsPrefix = "sys:scheduler/jobs/"

	// TenantJobsPrefixFmt is the format for tenant job keys: t:{tenantID}:scheduler/jobs/.
	TenantJobsPrefixFmt = "t:%s:scheduler/jobs/"

	// ExecutionsPrefix stores job execution history.
	ExecutionsPrefix = "sys:scheduler/executions/"

	// TenantExecutionsPrefixFmt is the format for tenant execution keys: t:{tenantID}:scheduler/executions/.
	TenantExecutionsPrefixFmt = "t:%s:scheduler/executions/"

	// JobIDCounterKey stores the atomic job ID counter for persistence.
	JobIDCounterKey = "sys:scheduler/meta/job_id_counter"
)

// Storage implements scheduler.JobStorage using QuicBaaS's KVStore interface.
type Storage struct {
	kv     interfaces.KVStore
	nextID atomic.Uint64
}

// Verify Storage implements JobStorage interface.
var _ scheduler.JobStorage = (*Storage)(nil)

// NewStorage creates a new QuicBaaS-backed job storage.
func NewStorage(kv interfaces.KVStore) *Storage {
	return &Storage{
		kv: kv,
	}
}

// keyForJob returns the storage key for a job based on ownership.
func keyForJob(job *scheduler.Job) string {
	switch job.OwnerType {
	case scheduler.OwnerTypeSystem, scheduler.OwnerTypeDelegated:
		return fmt.Sprintf("%s%d", SystemJobsPrefix, job.ID)
	case scheduler.OwnerTypeTenant:
		return fmt.Sprintf(TenantJobsPrefixFmt+"%d", job.OwnerID, job.ID)
	default:
		return fmt.Sprintf("%s%d", SystemJobsPrefix, job.ID)
	}
}

// keyForJobByID returns the storage key for a job ID with tenant context.
func keyForJobByID(id uint64, tenantID string) string {
	if tenantID == "" {
		return fmt.Sprintf("%s%d", SystemJobsPrefix, id)
	}
	return fmt.Sprintf(TenantJobsPrefixFmt+"%d", tenantID, id)
}

// keyForExecution returns the storage key for an execution record.
func keyForExecution(exec *scheduler.JobExecution, tenantID string) string {
	if tenantID == "" {
		return fmt.Sprintf("%s%d/%d", ExecutionsPrefix, exec.JobID, exec.ID)
	}
	return fmt.Sprintf(TenantExecutionsPrefixFmt+"%d/%d", tenantID, exec.JobID, exec.ID)
}

// CreateJob stores a new job.
func (s *Storage) CreateJob(ctx context.Context, job *scheduler.Job) error {
	if job == nil {
		return &scheduler.ErrStorageError{Op: "CreateJob", Cause: ErrNilJob}
	}

	// Assign ID if not set
	if job.ID == 0 {
		job.ID = s.nextID.Add(1)
	}

	// Set timestamps
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now

	// Marshal job to JSON
	data, err := json.Marshal(job)
	if err != nil {
		return &scheduler.ErrStorageError{Op: "CreateJob.marshal", Cause: err}
	}

	// Store in QuicBaaS
	key := keyForJob(job)
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &scheduler.ErrStorageError{Op: "CreateJob.put", Cause: err}
	}

	return nil
}

// GetJob retrieves a job by ID.
func (s *Storage) GetJob(ctx context.Context, id uint64, tenantID string) (*scheduler.Job, error) {
	key := keyForJobByID(id, tenantID)

	data, err := s.kv.Get(ctx, key)
	if err != nil {
		// Check if it's a not found error
		return nil, &scheduler.JobNotFoundError{JobID: id}
	}

	if len(data) == 0 {
		return nil, &scheduler.JobNotFoundError{JobID: id}
	}

	var job scheduler.Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, &scheduler.ErrStorageError{Op: "GetJob.unmarshal", Cause: err}
	}

	return &job, nil
}

// UpdateJob updates an existing job.
func (s *Storage) UpdateJob(ctx context.Context, job *scheduler.Job) error {
	if job == nil {
		return &scheduler.ErrStorageError{Op: "UpdateJob", Cause: ErrNilJob}
	}

	job.UpdatedAt = time.Now()

	data, err := json.Marshal(job)
	if err != nil {
		return &scheduler.ErrStorageError{Op: "UpdateJob.marshal", Cause: err}
	}

	key := keyForJob(job)
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &scheduler.ErrStorageError{Op: "UpdateJob.put", Cause: err}
	}

	return nil
}

// DeleteJob removes a job.
func (s *Storage) DeleteJob(ctx context.Context, id uint64, tenantID string) error {
	key := keyForJobByID(id, tenantID)

	if err := s.kv.Delete(ctx, key); err != nil {
		return &scheduler.ErrStorageError{Op: "DeleteJob.delete", Cause: err}
	}

	return nil
}

// ListJobs returns jobs matching the query.
func (s *Storage) ListJobs(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
	if query == nil {
		query = &scheduler.JobQuery{}
	}

	// Determine prefix based on query
	var prefix string
	switch {
	case query.OwnerID != "":
		prefix = fmt.Sprintf(TenantJobsPrefixFmt, query.OwnerID)
	case query.OwnerType == scheduler.OwnerTypeSystem || query.OwnerType == scheduler.OwnerTypeDelegated:
		prefix = SystemJobsPrefix
	default:
		prefix = SystemJobsPrefix // Default to system jobs
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}

	jobs := make([]*scheduler.Job, 0)
	var scanErr error

	err := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var job scheduler.Job
		if err := json.Unmarshal(value, &job); err != nil {
			// Skip malformed entries
			return nil
		}

		// Apply filters
		if query.Status != "" && job.Status != query.Status {
			return nil
		}
		if query.Handler != "" && job.Handler != query.Handler {
			return nil
		}
		if query.TargetTenantID != "" && job.TargetTenantID != query.TargetTenantID {
			return nil
		}

		// Apply label filters
		if len(query.Labels) > 0 {
			matches := true
			for k, v := range query.Labels {
				if job.Labels == nil || job.Labels[k] != v {
					matches = false
					break
				}
			}
			if !matches {
				return nil
			}
		}

		jobs = append(jobs, &job)

		// Stop if we've reached the limit
		if len(jobs) >= limit {
			return ErrLimitReached
		}

		return nil
	})

	if err != nil && !errors.Is(err, ErrLimitReached) {
		return nil, "", &scheduler.ErrStorageError{Op: "ListJobs.scan", Cause: err}
	}
	if scanErr != nil {
		return nil, "", &scheduler.ErrStorageError{Op: "ListJobs.scan", Cause: scanErr}
	}

	// Generate cursor for pagination if we hit the limit
	var nextCursor string
	if len(jobs) >= limit && len(jobs) > 0 {
		lastJob := jobs[len(jobs)-1]
		nextCursor = fmt.Sprintf("%d", lastJob.ID)
	}

	return jobs, nextCursor, nil
}

// GetJobsDueForExecution returns jobs that need to run.
func (s *Storage) GetJobsDueForExecution(ctx context.Context, before time.Time, limit int) ([]*scheduler.Job, error) {
	if limit <= 0 {
		limit = 100
	}

	// Scan system jobs prefix for pending jobs
	dueJobs := make([]*scheduler.Job, 0)

	err := s.kv.Scan(ctx, SystemJobsPrefix, func(key string, value []byte) error {
		var job scheduler.Job
		if err := json.Unmarshal(value, &job); err != nil {
			// Skip malformed entries
			return nil
		}

		// Check if job is due for execution
		isDue := job.Status == scheduler.JobStatusPending &&
			!job.NextRun.IsZero() &&
			job.NextRun.Before(before)

		if isDue {
			dueJobs = append(dueJobs, &job)
			if len(dueJobs) >= limit {
				return ErrLimitReached
			}
		}

		return nil
	})

	if err != nil && !errors.Is(err, ErrLimitReached) {
		return nil, &scheduler.ErrStorageError{Op: "GetJobsDueForExecution.scan", Cause: err}
	}

	return dueJobs, nil
}

// SaveExecution stores a job execution record.
func (s *Storage) SaveExecution(ctx context.Context, exec *scheduler.JobExecution) error {
	if exec == nil {
		return &scheduler.ErrStorageError{Op: "SaveExecution", Cause: ErrNilExecution}
	}

	if exec.ID == 0 {
		// Use nanoseconds for unique IDs. UnixNano returns int64 which is always
		// positive for valid timestamps, making the uint64 conversion safe.
		exec.ID = uint64(time.Now().UnixNano()) //nolint:gosec // UnixNano is positive for valid timestamps
	}

	data, err := json.Marshal(exec)
	if err != nil {
		return &scheduler.ErrStorageError{Op: "SaveExecution.marshal", Cause: err}
	}

	// Determine tenant ID from job ownership if we need to store in tenant keyspace
	// For now, store in system keyspace
	key := keyForExecution(exec, "")

	if err := s.kv.Put(ctx, key, data); err != nil {
		return &scheduler.ErrStorageError{Op: "SaveExecution.put", Cause: err}
	}

	return nil
}

// GetExecutions retrieves execution history for a job.
func (s *Storage) GetExecutions(ctx context.Context, jobID uint64, tenantID string, limit int) ([]*scheduler.JobExecution, error) {
	if limit <= 0 {
		limit = 100
	}

	var prefix string
	if tenantID == "" {
		prefix = fmt.Sprintf("%s%d/", ExecutionsPrefix, jobID)
	} else {
		prefix = fmt.Sprintf(TenantExecutionsPrefixFmt+"%d/", tenantID, jobID)
	}

	executions := make([]*scheduler.JobExecution, 0)

	err := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var exec scheduler.JobExecution
		if err := json.Unmarshal(value, &exec); err != nil {
			// Skip malformed entries
			return nil
		}

		executions = append(executions, &exec)
		if len(executions) >= limit {
			return ErrLimitReached
		}

		return nil
	})

	if err != nil && !errors.Is(err, ErrLimitReached) {
		return nil, &scheduler.ErrStorageError{Op: "GetExecutions.scan", Cause: err}
	}

	return executions, nil
}

// InitializeJobIDCounter loads the persisted job ID counter from storage.
// This should be called during storage initialization to ensure
// job IDs are monotonically increasing across restarts.
func (s *Storage) InitializeJobIDCounter(ctx context.Context) error {
	data, err := s.kv.Get(ctx, JobIDCounterKey)
	if err != nil {
		// Counter doesn't exist yet, start from 0
		return nil
	}

	if len(data) == 0 {
		return nil
	}

	var counter uint64
	if err := json.Unmarshal(data, &counter); err != nil {
		return &scheduler.ErrStorageError{Op: "InitializeJobIDCounter.unmarshal", Cause: err}
	}

	s.nextID.Store(counter)
	return nil
}

// PersistJobIDCounter saves the current job ID counter to storage.
// This should be called periodically to ensure durability.
func (s *Storage) PersistJobIDCounter(ctx context.Context) error {
	counter := s.nextID.Load()

	data, err := json.Marshal(counter)
	if err != nil {
		return &scheduler.ErrStorageError{Op: "PersistJobIDCounter.marshal", Cause: err}
	}

	if err := s.kv.Put(ctx, JobIDCounterKey, data); err != nil {
		return &scheduler.ErrStorageError{Op: "PersistJobIDCounter.put", Cause: err}
	}

	return nil
}

// Close performs cleanup operations.
// Currently a no-op as KVStore lifecycle is managed externally.
func (s *Storage) Close() error {
	return nil
}
