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

package backup

import "fmt"

// SchedulerError represents an error related to backup scheduling operations.
type SchedulerError struct {
	Op      string // Operation that failed (e.g., "parse", "schedule", "start")
	Message string // Human-readable description
	Cause   error  // Underlying error
}

// Error returns the error message.
func (e *SchedulerError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("scheduler %s: %s: %v", e.Op, e.Message, e.Cause)
	}
	return fmt.Sprintf("scheduler %s: %s", e.Op, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *SchedulerError) Unwrap() error {
	return e.Cause
}

// BackupExecutionError represents an error during scheduled backup execution.
type BackupExecutionError struct {
	BackupType BackupType // Type of backup that failed
	Schedule   string     // Cron expression of the schedule
	Message    string     // Human-readable description
	Cause      error      // Underlying error
}

// Error returns the error message.
func (e *BackupExecutionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("scheduled backup failed (type=%s, schedule=%s): %s: %v",
			e.BackupType, e.Schedule, e.Message, e.Cause)
	}
	return fmt.Sprintf("scheduled backup failed (type=%s, schedule=%s): %s",
		e.BackupType, e.Schedule, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *BackupExecutionError) Unwrap() error {
	return e.Cause
}

// RetentionPolicyError represents an error applying retention policy.
type RetentionPolicyError struct {
	Op      string // Operation that failed
	Message string // Human-readable description
	Cause   error  // Underlying error
}

// Error returns the error message.
func (e *RetentionPolicyError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("retention policy %s: %s: %v", e.Op, e.Message, e.Cause)
	}
	return fmt.Sprintf("retention policy %s: %s", e.Op, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *RetentionPolicyError) Unwrap() error {
	return e.Cause
}

// ManagerClosedError indicates an operation was attempted on a closed manager.
type ManagerClosedError struct {
	Op string // Operation that was attempted
}

// Error returns the error message.
func (e *ManagerClosedError) Error() string {
	return "backup manager is closed: cannot perform " + e.Op
}

// InvalidConfigError represents an invalid configuration error.
type InvalidConfigError struct {
	Field   string // Configuration field that is invalid
	Message string // Human-readable description
	Cause   error  // Underlying error
}

// Error returns the error message.
func (e *InvalidConfigError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("invalid config field %q: %s: %v", e.Field, e.Message, e.Cause)
	}
	return fmt.Sprintf("invalid config field %q: %s", e.Field, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *InvalidConfigError) Unwrap() error {
	return e.Cause
}

// UnsupportedCompressionError is returned when an unsupported compression type is specified.
type UnsupportedCompressionError struct {
	Type string
}

// Error implements the error interface.
func (e *UnsupportedCompressionError) Error() string {
	return "unsupported compression type: " + e.Type
}

// NonceWriteError indicates a failure writing the nonce during encryption setup.
type NonceWriteError struct {
	Cause error // Underlying write error
}

// Error implements the error interface.
func (e *NonceWriteError) Error() string {
	if e.Cause != nil {
		return "failed to write nonce: " + e.Cause.Error()
	}
	return "failed to write nonce"
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *NonceWriteError) Unwrap() error {
	return e.Cause
}

// NonceGenerateError indicates a failure generating a random nonce.
type NonceGenerateError struct {
	Cause error // Underlying error from crypto/rand
}

// Error implements the error interface.
func (e *NonceGenerateError) Error() string {
	if e.Cause != nil {
		return "failed to generate nonce: " + e.Cause.Error()
	}
	return "failed to generate nonce"
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *NonceGenerateError) Unwrap() error {
	return e.Cause
}

// NonceTooShortError indicates the input data is too short to contain a nonce.
type NonceTooShortError struct {
	Expected int // Expected minimum size (nonce size)
	Actual   int // Actual data size
}

// Error implements the error interface.
func (e *NonceTooShortError) Error() string {
	return fmt.Sprintf("data too short for nonce: expected at least %d bytes, got %d", e.Expected, e.Actual)
}
