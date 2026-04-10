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

// Package auth provides login attempt throttling and account lockout.
package auth

import (
	"sync"
	"sync/atomic"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// DefaultMaxLoginAttempts is the default maximum number of failed login
// attempts before an account is temporarily locked.
const DefaultMaxLoginAttempts = 5

// DefaultLoginLockoutDuration is the default duration for which an account
// remains locked after exceeding the maximum failed login attempts.
const DefaultLoginLockoutDuration = 15 * time.Minute

// LoginRecordTypeError is returned when the internal sync.Map
// contains a value that is not a *loginAttemptRecord.
type LoginRecordTypeError struct{}

// Error implements the error interface.
func (e *LoginRecordTypeError) Error() string {
	return "login throttle: unexpected record type in sync.Map"
}

// loginAttemptRecord tracks failed login attempts for a single username.
// It uses atomic operations to be safe for concurrent access without mutexes.
type loginAttemptRecord struct {
	// failedCount tracks the number of consecutive failed login attempts.
	failedCount atomic.Int64

	// lockedUntilNano stores the Unix nanosecond timestamp when the lockout
	// expires. A value of 0 means the account is not locked.
	lockedUntilNano atomic.Int64
}

// LoginAttemptTracker provides lock-free tracking of failed login attempts
// per username. It uses sync.Map for concurrent map access and atomic
// operations for per-user counters, avoiding mutex contention.
//
// When disabled (maxAttempts == 0), all tracking operations are no-ops
// and accounts are never locked. This is useful for integration tests
// where failed auth attempts from protocol probing should not cascade
// into account lockouts that break subsequent tests.
type LoginAttemptTracker struct {
	// records maps username (string) to *loginAttemptRecord.
	records sync.Map

	maxAttempts     int
	lockoutDuration time.Duration
	logger          *logger.Logger
	disabled        bool
}

// NewLoginAttemptTracker creates a new LoginAttemptTracker with the given
// configuration.
//
// Behavior:
//   - maxAttempts == 0: Throttling is disabled; all operations are no-ops.
//   - maxAttempts < 0:  Uses DefaultMaxLoginAttempts (5).
//   - maxAttempts > 0:  Uses the provided value.
//
// If lockoutDuration is 0 or negative, DefaultLoginLockoutDuration is used.
func NewLoginAttemptTracker(maxAttempts int, lockoutDuration time.Duration, log *logger.Logger) *LoginAttemptTracker {
	// maxAttempts == 0 means disabled
	if maxAttempts == 0 {
		return &LoginAttemptTracker{
			maxAttempts:     0,
			lockoutDuration: 0,
			logger:          log,
			disabled:        true,
		}
	}

	if maxAttempts < 0 {
		maxAttempts = DefaultMaxLoginAttempts
	}
	if lockoutDuration <= 0 {
		lockoutDuration = DefaultLoginLockoutDuration
	}
	return &LoginAttemptTracker{
		maxAttempts:     maxAttempts,
		lockoutDuration: lockoutDuration,
		logger:          log,
		disabled:        false,
	}
}

// asRecord safely asserts a sync.Map value to *loginAttemptRecord.
// Panics with LoginRecordTypeError if the stored value has an unexpected type,
// which indicates a programming error.
func asRecord(val any) *loginAttemptRecord {
	record, ok := val.(*loginAttemptRecord)
	if !ok {
		panic(&LoginRecordTypeError{})
	}
	return record
}

// getOrCreateRecord returns the loginAttemptRecord for the given username,
// creating one if it does not exist.
func (t *LoginAttemptTracker) getOrCreateRecord(username string) *loginAttemptRecord {
	val, _ := t.records.LoadOrStore(username, &loginAttemptRecord{})
	return asRecord(val)
}

// CheckLocked checks if the given username is currently locked out.
// If the lockout duration has expired, the lock is automatically cleared
// and nil is returned. Returns an AccountLockedError if the account is
// still locked. Returns nil immediately when throttling is disabled.
func (t *LoginAttemptTracker) CheckLocked(username string) error {
	if t.disabled {
		return nil
	}

	val, ok := t.records.Load(username)
	if !ok {
		return nil
	}

	record := asRecord(val)
	lockedUntil := record.lockedUntilNano.Load()
	if lockedUntil == 0 {
		return nil
	}

	now := time.Now().UnixNano()
	if now >= lockedUntil {
		// Lockout has expired. Reset the record atomically.
		record.lockedUntilNano.Store(0)
		record.failedCount.Store(0)
		return nil
	}

	retryAfter := time.Duration(lockedUntil - now)
	return api.NewAccountLockedError("auth.Authenticate", username, retryAfter)
}

// RecordFailure records a failed login attempt for the given username.
// If the failure count reaches the maximum, the account is locked for
// the configured lockout duration. Returns an error if the account
// becomes locked as a result of this failure.
// Returns nil immediately when throttling is disabled.
func (t *LoginAttemptTracker) RecordFailure(username string) error {
	if t.disabled {
		return nil
	}

	record := t.getOrCreateRecord(username)

	// Check if already locked (another goroutine may have locked it).
	lockedUntil := record.lockedUntilNano.Load()
	if lockedUntil > 0 {
		now := time.Now().UnixNano()
		if now < lockedUntil {
			retryAfter := time.Duration(lockedUntil - now)
			return api.NewAccountLockedError("auth.Authenticate", username, retryAfter)
		}
		// Lock expired, reset.
		record.lockedUntilNano.Store(0)
		record.failedCount.Store(0)
	}

	count := record.failedCount.Add(1)

	t.logger.Warnf("Failed login attempt for user %s (attempt %d/%d)",
		username, count, t.maxAttempts)

	if count >= int64(t.maxAttempts) {
		lockUntil := time.Now().Add(t.lockoutDuration)
		record.lockedUntilNano.Store(lockUntil.UnixNano())

		t.logger.Warnf("Account locked for user %s: exceeded %d failed login attempts, locked until %s",
			username, t.maxAttempts, lockUntil.Format(time.RFC3339))

		return api.NewAccountLockedError("auth.Authenticate", username, t.lockoutDuration)
	}

	return nil
}

// RecordSuccess resets the failed attempt counter for the given username,
// clearing any lockout state. This should be called after a successful login.
// No-op when throttling is disabled.
func (t *LoginAttemptTracker) RecordSuccess(username string) {
	if t.disabled {
		return
	}

	val, ok := t.records.Load(username)
	if !ok {
		return
	}

	record := asRecord(val)
	record.failedCount.Store(0)
	record.lockedUntilNano.Store(0)
}

// FailedAttempts returns the current number of failed login attempts for
// the given username. Returns 0 if no attempts have been recorded or
// when throttling is disabled.
func (t *LoginAttemptTracker) FailedAttempts(username string) int64 {
	if t.disabled {
		return 0
	}

	val, ok := t.records.Load(username)
	if !ok {
		return 0
	}
	return asRecord(val).failedCount.Load()
}

// IsLocked returns true if the given username is currently locked out.
// Always returns false when throttling is disabled.
func (t *LoginAttemptTracker) IsLocked(username string) bool {
	if t.disabled {
		return false
	}

	val, ok := t.records.Load(username)
	if !ok {
		return false
	}

	record := asRecord(val)
	lockedUntil := record.lockedUntilNano.Load()
	if lockedUntil == 0 {
		return false
	}

	return time.Now().UnixNano() < lockedUntil
}

// IsDisabled returns true if login attempt throttling is disabled.
func (t *LoginAttemptTracker) IsDisabled() bool {
	return t.disabled
}
