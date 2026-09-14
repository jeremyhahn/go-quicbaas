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
	"fmt"
	"time"
)

// Error codes for scheduler errors.
const (
	ErrCodeJobNotFound        = "SCHEDULER_JOB_NOT_FOUND"
	ErrCodeJobAlreadyExists   = "SCHEDULER_JOB_EXISTS"
	ErrCodeInvalidJob         = "SCHEDULER_INVALID_JOB"
	ErrCodeInvalidSchedule    = "SCHEDULER_INVALID_SCHEDULE"
	ErrCodeHandlerNotFound    = "SCHEDULER_HANDLER_NOT_FOUND"
	ErrCodeHandlerFailed      = "SCHEDULER_HANDLER_FAILED"
	ErrCodeJobTimeout         = "SCHEDULER_JOB_TIMEOUT"
	ErrCodeMaxRetriesExceeded = "SCHEDULER_MAX_RETRIES"
	ErrCodeSchedulerStopped   = "SCHEDULER_STOPPED"
	ErrCodeUnauthorized       = "SCHEDULER_UNAUTHORIZED"
	ErrCodeStorageError       = "SCHEDULER_STORAGE_ERROR"
	ErrCodeClaimUnsupported   = "SCHEDULER_CLAIM_UNSUPPORTED"
	ErrCodeInvalidFiringClaim = "SCHEDULER_INVALID_FIRING_CLAIM"
)

// SchedulerError is the base error type for scheduler operations.
type SchedulerError struct {
	Code    string
	Op      string
	Message string
	Err     error
}

func (e *SchedulerError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %s: %v", e.Code, e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Op, e.Message)
}

func (e *SchedulerError) Unwrap() error {
	return e.Err
}

// Specific error types for common scenarios

// JobNotFoundError indicates the requested job does not exist.
type JobNotFoundError struct {
	JobID uint64
}

func (e JobNotFoundError) Error() string {
	return fmt.Sprintf("job not found: %d", e.JobID)
}

// JobAlreadyExistsError indicates a job with the same ID already exists.
type JobAlreadyExistsError struct {
	JobID uint64
	Name  string
}

func (e JobAlreadyExistsError) Error() string {
	if e.Name != "" {
		return fmt.Sprintf("job already exists: %s (ID: %d)", e.Name, e.JobID)
	}
	return fmt.Sprintf("job already exists: %d", e.JobID)
}

// InvalidJobError indicates the job definition is invalid.
type InvalidJobError struct {
	Field   string
	Message string
}

func (e InvalidJobError) Error() string {
	return fmt.Sprintf("invalid job: %s: %s", e.Field, e.Message)
}

// InvalidScheduleError indicates the cron schedule is invalid.
type InvalidScheduleError struct {
	Schedule string
	Cause    error
}

func (e InvalidScheduleError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("invalid schedule '%s': %v", e.Schedule, e.Cause)
	}
	return fmt.Sprintf("invalid schedule: %s", e.Schedule)
}

func (e InvalidScheduleError) Unwrap() error {
	return e.Cause
}

// HandlerNotFoundError indicates no handler is registered for the job type.
type HandlerNotFoundError struct {
	Handler string
}

func (e HandlerNotFoundError) Error() string {
	return fmt.Sprintf("handler not found: %s", e.Handler)
}

// HandlerFailedError indicates the handler returned an error.
type HandlerFailedError struct {
	Handler string
	JobID   uint64
	Cause   error
}

func (e HandlerFailedError) Error() string {
	return fmt.Sprintf("handler '%s' failed for job %d: %v", e.Handler, e.JobID, e.Cause)
}

func (e HandlerFailedError) Unwrap() error {
	return e.Cause
}

// JobTimeoutError indicates the job exceeded its timeout.
type JobTimeoutError struct {
	JobID   uint64
	Timeout time.Duration
}

func (e JobTimeoutError) Error() string {
	return fmt.Sprintf("job %d timed out after %s", e.JobID, e.Timeout)
}

// MaxRetriesExceededError indicates the job exceeded its retry limit.
type MaxRetriesExceededError struct {
	JobID      uint64
	MaxRetries int32
	LastError  error
}

func (e MaxRetriesExceededError) Error() string {
	return fmt.Sprintf("job %d exceeded max retries (%d): %v", e.JobID, e.MaxRetries, e.LastError)
}

func (e MaxRetriesExceededError) Unwrap() error {
	return e.LastError
}

// SchedulerStoppedError indicates the scheduler is not running.
type SchedulerStoppedError struct{}

func (e SchedulerStoppedError) Error() string {
	return "scheduler is stopped"
}

// UnauthorizedError indicates the caller lacks permission for the operation.
type UnauthorizedError struct {
	TenantID string
	JobID    uint64
	Op       string
}

func (e UnauthorizedError) Error() string {
	return fmt.Sprintf("unauthorized: tenant '%s' cannot %s job %d", e.TenantID, e.Op, e.JobID)
}

// ClaimUnsupportedError indicates a job store was constructed over a backing
// store that cannot perform an atomic compare-and-swap. Without one there is no
// way to decide a firing claim, and the scheduler would silently fall back to
// the double-fire behavior the claim exists to prevent — so this is returned at
// construction time, where it stops a node from starting, rather than at the
// first firing.
type ClaimUnsupportedError struct {
	Backend string
}

func (e ClaimUnsupportedError) Error() string {
	return fmt.Sprintf("job store backing %s cannot compare-and-swap; firing claims require an atomic store", e.Backend)
}

// InvalidFiringClaimError indicates a claim that cannot identify a firing.
// Every field of the identity is load-bearing: a zero job ID or a zero firing
// time would collapse distinct firings onto one claim key.
type InvalidFiringClaimError struct {
	Field   string
	Message string
}

func (e InvalidFiringClaimError) Error() string {
	return fmt.Sprintf("invalid firing claim: %s: %s", e.Field, e.Message)
}

// ErrStorageError indicates a storage operation failed.
type ErrStorageError struct {
	Op    string
	Cause error
}

func (e ErrStorageError) Error() string {
	return fmt.Sprintf("storage error during %s: %v", e.Op, e.Cause)
}

func (e ErrStorageError) Unwrap() error {
	return e.Cause
}
