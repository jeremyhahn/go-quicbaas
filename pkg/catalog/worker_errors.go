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

package catalog

import (
	"errors"
	"fmt"
)

// Worker configuration sentinel errors.
var (
	// ErrWorkerConfigNil indicates a nil worker configuration was provided.
	ErrWorkerConfigNil = errors.New("worker config cannot be nil")

	// ErrWorkerNameEmpty indicates an empty worker name was provided.
	ErrWorkerNameEmpty = errors.New("worker name cannot be empty")

	// ErrInvalidWorkerCount indicates an invalid worker count.
	ErrInvalidWorkerCount = errors.New("worker count must be greater than 0")

	// ErrInvalidBatchSize indicates an invalid batch size.
	ErrInvalidBatchSize = errors.New("batch size must be greater than 0")

	// ErrInvalidPollInterval indicates an invalid poll interval.
	ErrInvalidPollInterval = errors.New("poll interval must be greater than 0")

	// ErrInvalidMaxRetries indicates an invalid max retries value.
	ErrInvalidMaxRetries = errors.New("max retries cannot be negative")

	// ErrInvalidRetryDelay indicates an invalid retry delay.
	ErrInvalidRetryDelay = errors.New("retry delay cannot be negative")

	// ErrInvalidBackoffMultiplier indicates an invalid backoff multiplier.
	ErrInvalidBackoffMultiplier = errors.New("backoff multiplier must be >= 1.0")

	// ErrInvalidTaskTimeout indicates an invalid task timeout.
	ErrInvalidTaskTimeout = errors.New("task timeout must be greater than 0")

	// ErrWorkerManagerConfigNil indicates a nil worker manager configuration.
	ErrWorkerManagerConfigNil = errors.New("worker manager config cannot be nil")

	// ErrCatalogManagerNil indicates a nil catalog manager.
	ErrCatalogManagerNil = errors.New("catalog manager cannot be nil")

	// ErrBackendManagerNilForWorker indicates a nil backend manager for worker.
	ErrBackendManagerNilForWorker = errors.New("backend manager cannot be nil")
)

// Worker error codes.
const (
	// ErrCodeWorkerAlreadyRunning indicates the worker is already running.
	ErrCodeWorkerAlreadyRunning ErrorCode = "WORKER_ALREADY_RUNNING"

	// ErrCodeWorkerNotRunning indicates the worker is not running.
	ErrCodeWorkerNotRunning ErrorCode = "WORKER_NOT_RUNNING"

	// ErrCodeWorkerExists indicates a worker with the name already exists.
	ErrCodeWorkerExists ErrorCode = "WORKER_EXISTS"

	// ErrCodeWorkerNotFound indicates the worker was not found.
	ErrCodeWorkerNotFound ErrorCode = "WORKER_NOT_FOUND"

	// ErrCodeTaskProcessingFailed indicates task processing failed.
	ErrCodeTaskProcessingFailed ErrorCode = "TASK_PROCESSING_FAILED"

	// ErrCodeReplicationFailed indicates replication task failed.
	ErrCodeReplicationFailed ErrorCode = "REPLICATION_FAILED"

	// ErrCodeArchiveFailed indicates archive task failed.
	ErrCodeArchiveFailed ErrorCode = "ARCHIVE_FAILED"

	// ErrCodeVerificationFailed indicates verification task failed.
	ErrCodeVerificationFailed ErrorCode = "VERIFICATION_FAILED"

	// ErrCodeRetentionFailed indicates retention task failed.
	ErrCodeRetentionFailed ErrorCode = "RETENTION_FAILED"

	// ErrCodeWorkerManagerClosed indicates the worker manager is closed.
	ErrCodeWorkerManagerClosed ErrorCode = "WORKER_MANAGER_CLOSED"
)

// NewWorkerAlreadyRunningError creates a new error for already running worker.
func NewWorkerAlreadyRunningError(name string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeWorkerAlreadyRunning,
		Message: fmt.Sprintf("worker '%s' is already running", name),
	}
}

// NewWorkerNotRunningError creates a new error for worker not running.
func NewWorkerNotRunningError(name string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeWorkerNotRunning,
		Message: fmt.Sprintf("worker '%s' is not running", name),
	}
}

// NewWorkerExistsError creates a new error for duplicate worker.
func NewWorkerExistsError(name string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeWorkerExists,
		Message: fmt.Sprintf("worker '%s' already exists", name),
	}
}

// NewWorkerNotFoundError creates a new error for missing worker.
func NewWorkerNotFoundError(name string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeWorkerNotFound,
		Message: fmt.Sprintf("worker '%s' not found", name),
	}
}

// NewTaskProcessingError creates a new error for task processing failure.
func NewTaskProcessingError(taskType, key string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeTaskProcessingFailed,
		Message: fmt.Sprintf("failed to process %s task", taskType),
		Key:     key,
		Cause:   cause,
	}
}

// NewReplicationError creates a new error for replication failure.
func NewReplicationError(key, targetStore string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeReplicationFailed,
		Message: fmt.Sprintf("failed to replicate to '%s'", targetStore),
		Key:     key,
		Cause:   cause,
	}
}

// NewArchiveError creates a new error for archive failure.
func NewArchiveError(key, targetStore string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeArchiveFailed,
		Message: fmt.Sprintf("failed to archive to '%s'", targetStore),
		Key:     key,
		Cause:   cause,
	}
}

// NewVerificationError creates a new error for verification failure.
func NewVerificationError(key, store string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeVerificationFailed,
		Message: fmt.Sprintf("verification failed for store '%s'", store),
		Key:     key,
		Cause:   cause,
	}
}

// NewRetentionError creates a new error for retention enforcement failure.
func NewRetentionError(key string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeRetentionFailed,
		Message: "failed to enforce retention policy",
		Key:     key,
		Cause:   cause,
	}
}

// NewWorkerManagerClosedError creates a new error for closed worker manager.
func NewWorkerManagerClosedError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodeWorkerManagerClosed,
		Message: "worker manager is closed",
	}
}

// IsWorkerAlreadyRunning returns true if the error indicates worker is already running.
func IsWorkerAlreadyRunning(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeWorkerAlreadyRunning
	}
	return false
}

// IsWorkerNotFound returns true if the error indicates worker was not found.
func IsWorkerNotFound(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeWorkerNotFound
	}
	return false
}

// IsTaskProcessingError returns true if the error is a task processing error.
func IsTaskProcessingError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		switch ce.Code {
		case ErrCodeTaskProcessingFailed, ErrCodeReplicationFailed,
			ErrCodeArchiveFailed, ErrCodeVerificationFailed, ErrCodeRetentionFailed:
			return true
		}
	}
	return false
}

// IsWorkerManagerClosed returns true if the error indicates worker manager is closed.
func IsWorkerManagerClosed(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeWorkerManagerClosed
	}
	return false
}
