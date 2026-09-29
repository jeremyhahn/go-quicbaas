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

package scheduler

import (
	"context"
	"time"
)

// SystemJobIDFloor is the lowest job ID reserved for platform-defined system
// jobs.
//
// A system job is declared by the platform code of every node, not created by
// a caller, so every node must name the same record without coordinating. A
// fixed, well-known ID does that: the job lives under one storage key, two
// nodes installing it at once write the same record, and the firing claim —
// keyed by job ID — arbitrates its firings like any other cluster-wide job.
// Storage-assigned IDs count up from 1 and never reach this floor. It sits
// below 2^53 so the ID survives a round trip through JSON number parsers that
// use float64.
const SystemJobIDFloor uint64 = 1 << 52

// RegisterSystemJob declares a platform-owned job that the engine keeps
// installed in storage.
//
// The engine installs the job on its next tick and retries every tick until
// the store accepts it, so registration does not depend on storage being
// writable at boot. Installation is idempotent across nodes and restarts:
//
//   - a missing record is created from the definition;
//   - a record whose definition differs (schedule, handler, timeout, retries)
//     is brought up to date and rescheduled;
//   - a record left Failed — a non-recurring job's exhausted retry budget, a
//     missing handler, or an unschedulable definition — is re-armed to its
//     next firing with a fresh retry budget (a run cut short by shutdown is
//     not a failure and never leaves the record Failed; see
//     handleInterruptedRun);
//   - a record that is Paused or Canceled keeps that status, because it is an
//     operator's decision.
//
// job must be owned by OwnerTypeSystem and carry an ID at or above
// SystemJobIDFloor.
func (s *SchedulerEngine) RegisterSystemJob(job *Job) error {
	if err := s.validateJob(job); err != nil {
		return err
	}
	if job.OwnerType != OwnerTypeSystem {
		return InvalidJobError{Field: "owner_type", Message: "system jobs must be owned by the system"}
	}
	if job.ID < SystemJobIDFloor {
		return InvalidJobError{Field: "id", Message: "system job IDs must be at or above SystemJobIDFloor"}
	}

	s.systemJobsMu.Lock()
	defer s.systemJobsMu.Unlock()
	if s.systemJobs == nil {
		s.systemJobs = make(map[uint64]*Job)
	}
	if _, exists := s.systemJobs[job.ID]; exists {
		return JobAlreadyExistsError{JobID: job.ID, Name: job.Name}
	}
	definition := *job
	s.systemJobs[job.ID] = &definition
	return nil
}

// installSystemJobs installs every registered system job that is not yet
// installed. A job that cannot be installed this tick stays pending and is
// retried on the next one.
func (s *SchedulerEngine) installSystemJobs(ctx context.Context) {
	s.systemJobsMu.Lock()
	pending := make([]*Job, 0, len(s.systemJobs))
	for _, definition := range s.systemJobs {
		pending = append(pending, definition)
	}
	s.systemJobsMu.Unlock()

	if len(pending) == 0 {
		return
	}

	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	for _, definition := range pending {
		installed, err := s.ensureSystemJob(storageCtx, definition)
		if err != nil {
			s.storageErrors.Add(1)
			s.logWarn("system job %d (%s) not installed, retrying next tick: %v",
				definition.ID, definition.Name, err)
			continue
		}
		if !installed {
			continue
		}
		s.systemJobsMu.Lock()
		delete(s.systemJobs, definition.ID)
		s.systemJobsMu.Unlock()
	}
}

// ensureSystemJob brings the stored record for definition into line with it.
// It reports false, without error, when the record is running: rewriting it
// now would be overwritten by the execution's own outcome write, so the
// install is retried once the run has finished.
func (s *SchedulerEngine) ensureSystemJob(ctx context.Context, definition *Job) (bool, error) {
	lock := s.jobLock(definition.ID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now()
	current, err := s.storage.GetJob(ctx, definition.ID, "")
	if isJobNotFound(err) || (err == nil && current == nil) {
		job := *definition
		job.Status = JobStatusPending
		job.CreatedAt = now
		job.UpdatedAt = now
		if err := s.calculateNextRun(&job); err != nil {
			return false, err
		}
		if err := s.storage.CreateJob(ctx, &job); err != nil {
			return false, ErrStorageError{Op: "CreateJob", Cause: err}
		}
		s.jobCount.Add(1)
		return true, nil
	}
	if err != nil {
		return false, ErrStorageError{Op: "GetJob", Cause: err}
	}

	if current.Status == JobStatusRunning {
		return false, nil
	}

	reschedule := current.Status == JobStatusFailed
	if current.Type != definition.Type ||
		current.Schedule != definition.Schedule ||
		current.Handler != definition.Handler ||
		current.Timeout != definition.Timeout ||
		current.MaxRetries != definition.MaxRetries {
		current.Type = definition.Type
		current.Schedule = definition.Schedule
		current.Handler = definition.Handler
		current.Timeout = definition.Timeout
		current.MaxRetries = definition.MaxRetries
		reschedule = true
	}
	if !reschedule {
		return true, nil
	}

	if current.Status == JobStatusFailed {
		current.Status = JobStatusPending
	}
	if err := s.startNextFiring(current); err != nil {
		return false, err
	}
	current.UpdatedAt = now
	if err := s.storage.UpdateJob(ctx, current); err != nil {
		return false, ErrStorageError{Op: "UpdateJob", Cause: err}
	}
	return true, nil
}
