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
	"strconv"
)

// =============================================================================
// FileLock Types
// =============================================================================

// FileLock represents an active lock on a file path.
type FileLock struct {
	LockKey      string `json:"lock_key"`
	AgentID      string `json:"agent_id"`
	TenantID     string `json:"tenant_id"`
	ProjectID    string `json:"project_id"`
	FilePath     string `json:"file_path"`
	LeaseID      string `json:"lease_id"`
	FencingToken uint64 `json:"fencing_token"`
	AcquiredAt   int64  `json:"acquired_at"`
	Isolation    string `json:"isolation"`
}

// =============================================================================
// FileLockService Interface
// =============================================================================

// FileLockService manages distributed file locks for agent coordination.
type FileLockService interface {
	// Acquire obtains a lock on the specified file path for the given agent.
	Acquire(ctx context.Context, tenantID, projectID, filePath, agentID string) (*FileLock, error)

	// Release releases a previously acquired lock.
	Release(ctx context.Context, tenantID, lockKey string) error

	// Renew extends the lease on an existing lock.
	Renew(ctx context.Context, tenantID, lockKey string) error

	// IsLocked checks whether the file path is currently locked.
	IsLocked(ctx context.Context, tenantID, projectID, filePath string) (*FileLock, bool, error)

	// WaitForRelease blocks until the lock is released or the context is cancelled.
	WaitForRelease(ctx context.Context, tenantID, lockKey string) error

	// ListLocks returns all active locks for the given tenant and project.
	ListLocks(ctx context.Context, tenantID, projectID string) ([]*FileLock, error)
}

// =============================================================================
// Sentinel Errors
// =============================================================================

var (
	// ErrFileLocked indicates a file is already locked by another agent.
	ErrFileLocked = errors.New("filelock: file is locked")

	// ErrLockNotFound indicates the requested lock does not exist.
	ErrLockNotFound = errors.New("filelock: lock not found")

	// ErrStaleLock indicates a lock's fencing token is stale (term has advanced).
	ErrStaleLock = errors.New("filelock: stale lock")

	// ErrLockReleaseNotOwner indicates only the lock holder can release the lock.
	ErrLockReleaseNotOwner = errors.New("filelock: release denied, not owner")

	// ErrFileLockValidation indicates a file lock validation failure.
	ErrFileLockValidation = errors.New("filelock: validation failed")
)

// =============================================================================
// Typed Errors
// =============================================================================

// FileLockedError indicates a file is already locked by another agent.
type FileLockedError struct {
	AgentID    string
	FilePath   string
	AcquiredAt int64
}

// Error implements the error interface.
func (e *FileLockedError) Error() string {
	return ErrFileLocked.Error() + ": agent=" + e.AgentID +
		" path=" + e.FilePath +
		" acquired_at=" + strconv.FormatInt(e.AcquiredAt, 10)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FileLockedError) Unwrap() error {
	return ErrFileLocked
}

// LockNotFoundError indicates the requested lock does not exist.
type LockNotFoundError struct {
	LockKey string
}

// Error implements the error interface.
func (e *LockNotFoundError) Error() string {
	return ErrLockNotFound.Error() + ": " + e.LockKey
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *LockNotFoundError) Unwrap() error {
	return ErrLockNotFound
}

// StaleLockError indicates a lock's fencing token is behind the current Raft term.
type StaleLockError struct {
	LockKey     string
	LockTerm    uint64
	CurrentTerm uint64
}

// Error implements the error interface.
func (e *StaleLockError) Error() string {
	return ErrStaleLock.Error() + ": key=" + e.LockKey +
		" lock_term=" + strconv.FormatUint(e.LockTerm, 10) +
		" current_term=" + strconv.FormatUint(e.CurrentTerm, 10)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *StaleLockError) Unwrap() error {
	return ErrStaleLock
}

// LockReleaseNotOwnerError indicates a release was attempted by a non-owner.
type LockReleaseNotOwnerError struct {
	LockKey  string
	OwnerID  string
	CallerID string
}

// Error implements the error interface.
func (e *LockReleaseNotOwnerError) Error() string {
	return ErrLockReleaseNotOwner.Error() + ": key=" + e.LockKey +
		" owner=" + e.OwnerID +
		" caller=" + e.CallerID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *LockReleaseNotOwnerError) Unwrap() error {
	return ErrLockReleaseNotOwner
}

// FileLockValidationError indicates a missing or invalid field in a file lock operation.
type FileLockValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *FileLockValidationError) Error() string {
	return ErrFileLockValidation.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FileLockValidationError) Unwrap() error {
	return ErrFileLockValidation
}
