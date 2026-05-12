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

// Package auth provides login attempt throttling tests.
package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// TestLoginAttemptTracker_NewWithDefaults verifies that the tracker uses
// default values when negative values are provided for maxAttempts.
func TestLoginAttemptTracker_NewWithDefaults(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(-1, 0, log)

	assert.Equal(t, DefaultMaxLoginAttempts, tracker.maxAttempts)
	assert.Equal(t, DefaultLoginLockoutDuration, tracker.lockoutDuration)
	assert.False(t, tracker.IsDisabled())
}

// TestLoginAttemptTracker_NewWithNegativeValues verifies that the tracker uses
// default values when negative values are provided.
func TestLoginAttemptTracker_NewWithNegativeValues(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(-1, -1*time.Minute, log)

	assert.Equal(t, DefaultMaxLoginAttempts, tracker.maxAttempts)
	assert.Equal(t, DefaultLoginLockoutDuration, tracker.lockoutDuration)
	assert.False(t, tracker.IsDisabled())
}

// TestLoginAttemptTracker_NewWithCustomValues verifies that the tracker uses
// custom values when provided.
func TestLoginAttemptTracker_NewWithCustomValues(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(3, 5*time.Minute, log)

	assert.Equal(t, 3, tracker.maxAttempts)
	assert.Equal(t, 5*time.Minute, tracker.lockoutDuration)
	assert.False(t, tracker.IsDisabled())
}

// TestLoginAttemptTracker_DisabledWithZero verifies that passing maxAttempts=0
// disables throttling entirely. All operations become no-ops.
func TestLoginAttemptTracker_DisabledWithZero(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(0, 5*time.Minute, log)

	assert.True(t, tracker.IsDisabled())
	assert.Equal(t, 0, tracker.maxAttempts)

	// All operations should be no-ops when disabled
	assert.NoError(t, tracker.CheckLocked("anyuser"))
	assert.NoError(t, tracker.RecordFailure("anyuser"))
	assert.NoError(t, tracker.RecordFailure("anyuser"))
	assert.NoError(t, tracker.RecordFailure("anyuser"))
	assert.Equal(t, int64(0), tracker.FailedAttempts("anyuser"))
	assert.False(t, tracker.IsLocked("anyuser"))

	// RecordSuccess should not panic
	tracker.RecordSuccess("anyuser")
}

// TestLoginAttemptTracker_DisabledNeverLocks verifies that a disabled tracker
// never locks accounts regardless of how many failures are recorded.
func TestLoginAttemptTracker_DisabledNeverLocks(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(0, 1*time.Millisecond, log)

	// Record many failures - none should cause a lock
	for i := 0; i < 100; i++ {
		err := tracker.RecordFailure("admin")
		assert.NoError(t, err)
	}

	assert.False(t, tracker.IsLocked("admin"))
	assert.NoError(t, tracker.CheckLocked("admin"))
	assert.Equal(t, int64(0), tracker.FailedAttempts("admin"))
}

// TestLoginAttemptTracker_CheckLockedUnknownUser verifies that CheckLocked
// returns nil for an unknown username.
func TestLoginAttemptTracker_CheckLockedUnknownUser(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(3, 5*time.Minute, log)

	err := tracker.CheckLocked("nonexistent")
	assert.NoError(t, err)
}

// TestLoginAttemptTracker_RecordFailureIncrementsCounter verifies that
// RecordFailure increments the failed attempt counter for a username.
func TestLoginAttemptTracker_RecordFailureIncrementsCounter(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(5, 5*time.Minute, log)

	assert.Equal(t, int64(0), tracker.FailedAttempts("user1"))

	err := tracker.RecordFailure("user1")
	assert.NoError(t, err)
	assert.Equal(t, int64(1), tracker.FailedAttempts("user1"))

	err = tracker.RecordFailure("user1")
	assert.NoError(t, err)
	assert.Equal(t, int64(2), tracker.FailedAttempts("user1"))
}

// TestLoginAttemptTracker_RecordFailureDoesNotAffectOtherUsers verifies that
// failures for one user do not affect the counter for another user.
func TestLoginAttemptTracker_RecordFailureDoesNotAffectOtherUsers(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(5, 5*time.Minute, log)

	err := tracker.RecordFailure("user1")
	assert.NoError(t, err)
	err = tracker.RecordFailure("user1")
	assert.NoError(t, err)

	assert.Equal(t, int64(2), tracker.FailedAttempts("user1"))
	assert.Equal(t, int64(0), tracker.FailedAttempts("user2"))
}

// TestLoginAttemptTracker_LockoutAfterMaxAttempts verifies that the account
// is locked after the maximum number of failed attempts is reached.
func TestLoginAttemptTracker_LockoutAfterMaxAttempts(t *testing.T) {
	log := createTestLogger(t)
	maxAttempts := 3
	tracker := NewLoginAttemptTracker(maxAttempts, 5*time.Minute, log)

	// First two failures should not lock the account
	for i := 0; i < maxAttempts-1; i++ {
		err := tracker.RecordFailure("lockme")
		assert.NoError(t, err, "attempt %d should not trigger lockout", i+1)
		assert.False(t, tracker.IsLocked("lockme"))
	}

	// Third failure should trigger lockout
	err := tracker.RecordFailure("lockme")
	assert.Error(t, err)
	assert.True(t, api.IsAccountLocked(err))
	assert.True(t, tracker.IsLocked("lockme"))

	// Subsequent check should also report locked
	err = tracker.CheckLocked("lockme")
	assert.Error(t, err)
	assert.True(t, api.IsAccountLocked(err))
	assert.Contains(t, err.Error(), "account locked")
	assert.Contains(t, err.Error(), "lockme")
}

// TestLoginAttemptTracker_LockedAccountRejectsRecordFailure verifies that
// RecordFailure returns an AccountLockedError when the account is already locked.
func TestLoginAttemptTracker_LockedAccountRejectsRecordFailure(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(2, 5*time.Minute, log)

	// Lock the account
	_ = tracker.RecordFailure("locked_user")
	err := tracker.RecordFailure("locked_user")
	assert.Error(t, err)
	assert.True(t, api.IsAccountLocked(err))

	// Additional failure attempts should still return locked
	err = tracker.RecordFailure("locked_user")
	assert.Error(t, err)
	assert.True(t, api.IsAccountLocked(err))
}

// TestLoginAttemptTracker_RecordSuccessResetsCounter verifies that a
// successful login resets the failed attempt counter.
func TestLoginAttemptTracker_RecordSuccessResetsCounter(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(5, 5*time.Minute, log)

	// Accumulate some failures
	_ = tracker.RecordFailure("user1")
	_ = tracker.RecordFailure("user1")
	assert.Equal(t, int64(2), tracker.FailedAttempts("user1"))

	// Success resets the counter
	tracker.RecordSuccess("user1")
	assert.Equal(t, int64(0), tracker.FailedAttempts("user1"))
	assert.False(t, tracker.IsLocked("user1"))
}

// TestLoginAttemptTracker_RecordSuccessOnUnknownUser verifies that
// RecordSuccess is a no-op for an unknown username.
func TestLoginAttemptTracker_RecordSuccessOnUnknownUser(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(5, 5*time.Minute, log)

	// Should not panic or error
	tracker.RecordSuccess("never_seen")
	assert.Equal(t, int64(0), tracker.FailedAttempts("never_seen"))
}

// TestLoginAttemptTracker_AutoUnlockAfterDuration verifies that the account
// is automatically unlocked after the lockout duration expires. Uses a very
// short lockout duration to avoid time.Sleep.
func TestLoginAttemptTracker_AutoUnlockAfterDuration(t *testing.T) {
	log := createTestLogger(t)
	// Use a 1ms lockout so it expires nearly immediately
	tracker := NewLoginAttemptTracker(2, 1*time.Millisecond, log)

	// Lock the account
	_ = tracker.RecordFailure("autouser")
	err := tracker.RecordFailure("autouser")
	assert.Error(t, err)
	assert.True(t, api.IsAccountLocked(err))

	// Spin-wait (no time.Sleep) until the lockout expires.
	// With a 1ms lockout, this completes almost instantly.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if tracker.CheckLocked("autouser") == nil {
			break
		}
	}

	// Account should now be unlocked
	err = tracker.CheckLocked("autouser")
	assert.NoError(t, err)
	assert.False(t, tracker.IsLocked("autouser"))

	// Counter should be reset after auto-unlock
	assert.Equal(t, int64(0), tracker.FailedAttempts("autouser"))
}

// TestLoginAttemptTracker_AutoUnlockViaRecordFailure verifies that
// RecordFailure auto-clears an expired lockout and starts a fresh counter.
func TestLoginAttemptTracker_AutoUnlockViaRecordFailure(t *testing.T) {
	log := createTestLogger(t)
	// Use 1ms lockout
	tracker := NewLoginAttemptTracker(2, 1*time.Millisecond, log)

	// Lock the account
	_ = tracker.RecordFailure("user_relock")
	err := tracker.RecordFailure("user_relock")
	assert.Error(t, err)
	assert.True(t, api.IsAccountLocked(err))

	// Spin-wait for lockout to expire
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !tracker.IsLocked("user_relock") {
			break
		}
	}

	// Now a new failure should start fresh (count = 1, not locked yet)
	err = tracker.RecordFailure("user_relock")
	assert.NoError(t, err)
	assert.Equal(t, int64(1), tracker.FailedAttempts("user_relock"))
	assert.False(t, tracker.IsLocked("user_relock"))
}

// TestLoginAttemptTracker_IsLockedUnknownUser verifies that IsLocked
// returns false for an unknown username.
func TestLoginAttemptTracker_IsLockedUnknownUser(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(3, 5*time.Minute, log)

	assert.False(t, tracker.IsLocked("ghost"))
}

// TestLoginAttemptTracker_IsLockedNotLockedUser verifies that IsLocked
// returns false for a user with failures below the threshold.
func TestLoginAttemptTracker_IsLockedNotLockedUser(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(5, 5*time.Minute, log)

	_ = tracker.RecordFailure("partial")
	_ = tracker.RecordFailure("partial")

	assert.False(t, tracker.IsLocked("partial"))
}

// TestLoginAttemptTracker_FailedAttemptsUnknownUser verifies that
// FailedAttempts returns 0 for an unknown username.
func TestLoginAttemptTracker_FailedAttemptsUnknownUser(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(3, 5*time.Minute, log)

	assert.Equal(t, int64(0), tracker.FailedAttempts("nobody"))
}

// TestLoginAttemptTracker_ConcurrentAccess verifies that the tracker is
// safe for concurrent use from multiple goroutines. The maxAttempts is set
// above the total attempts (500) to prevent lockout from interfering with
// the atomic counter verification.
func TestLoginAttemptTracker_ConcurrentAccess(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(1000, 5*time.Minute, log)

	var wg sync.WaitGroup
	const goroutines = 50
	const attemptsPerGoroutine = 10

	// Run concurrent failures for the same user
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < attemptsPerGoroutine; j++ {
				_ = tracker.RecordFailure("concurrent_user")
			}
		}()
	}
	wg.Wait()

	// Total failures should equal goroutines * attemptsPerGoroutine
	totalExpected := int64(goroutines * attemptsPerGoroutine)
	assert.Equal(t, totalExpected, tracker.FailedAttempts("concurrent_user"))
}

// TestLoginAttemptTracker_ConcurrentMixedOps verifies that the tracker
// handles concurrent failures and successes without data races.
func TestLoginAttemptTracker_ConcurrentMixedOps(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(1000, 5*time.Minute, log)

	var wg sync.WaitGroup
	const goroutines = 20

	// Half goroutines record failures, half record successes
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		if i%2 == 0 {
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					_ = tracker.RecordFailure("mixed_user")
				}
			}()
		} else {
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					tracker.RecordSuccess("mixed_user")
				}
			}()
		}
	}
	wg.Wait()

	// We just verify no panics or data races occurred.
	// The exact count is non-deterministic due to interleaving.
	_ = tracker.FailedAttempts("mixed_user")
	_ = tracker.IsLocked("mixed_user")
	_ = tracker.CheckLocked("mixed_user")
}

// TestLoginAttemptTracker_ConcurrentDifferentUsers verifies concurrent
// tracking across multiple independent users.
func TestLoginAttemptTracker_ConcurrentDifferentUsers(t *testing.T) {
	log := createTestLogger(t)
	tracker := NewLoginAttemptTracker(10, 5*time.Minute, log)

	var wg sync.WaitGroup
	users := []string{"alice", "bob", "charlie", "dave", "eve"}

	wg.Add(len(users))
	for _, user := range users {
		go func(u string) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_ = tracker.RecordFailure(u)
			}
		}(user)
	}
	wg.Wait()

	for _, user := range users {
		assert.Equal(t, int64(5), tracker.FailedAttempts(user))
	}
}

// TestAuthenticateWithThrottling_SuccessResetsCounter verifies that
// the InMemoryAuthManager resets the attempt counter on successful login.
func TestAuthenticateWithThrottling_SuccessResetsCounter(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:              true,
		JWTSecret:            "test-secret-key",
		TokenTTL:             1 * time.Hour,
		AllowPasswordAuth:    true,
		MaxLoginAttempts:     5,
		LoginLockoutDuration: 15 * time.Minute,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create a user with password (must be >= MinPasswordLength)
	_, err = am.CreateUser("testuser", "correct-password-long-enough", []string{RoleNameReadWrite})
	require.NoError(t, err)

	ctx := context.Background()

	// Fail a few times with wrong password
	_, err = am.Authenticate(ctx, "testuser", passwordBuffer(t, "wrong-password-definitely"))
	assert.Error(t, err)
	_, err = am.Authenticate(ctx, "testuser", passwordBuffer(t, "wrong-password-definitely"))
	assert.Error(t, err)

	assert.Equal(t, int64(2), am.GetAttemptTracker().FailedAttempts("testuser"))

	// Successful login resets the counter
	token, err := am.Authenticate(ctx, "testuser", passwordBuffer(t, "correct-password-long-enough"))
	assert.NoError(t, err)
	assert.NotNil(t, token)

	assert.Equal(t, int64(0), am.GetAttemptTracker().FailedAttempts("testuser"))
}

// TestAuthenticateWithThrottling_LockoutAfterMaxAttempts verifies that
// the InMemoryAuthManager locks the account after max failed attempts.
func TestAuthenticateWithThrottling_LockoutAfterMaxAttempts(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:              true,
		JWTSecret:            "test-secret-key",
		TokenTTL:             1 * time.Hour,
		AllowPasswordAuth:    true,
		MaxLoginAttempts:     3,
		LoginLockoutDuration: 15 * time.Minute,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	_, err = am.CreateUser("lockuser", "password-long-enough-123", []string{RoleNameReadWrite})
	require.NoError(t, err)

	ctx := context.Background()

	// Fail up to the max
	for i := 0; i < 3; i++ {
		_, err = am.Authenticate(ctx, "lockuser", passwordBuffer(t, "wrong-password-attempt"))
		assert.Error(t, err)
	}

	// Account should now be locked - even correct password should fail
	_, err = am.Authenticate(ctx, "lockuser", passwordBuffer(t, "password-long-enough-123"))
	assert.Error(t, err)
	assert.True(t, api.IsAccountLocked(err))
	assert.Contains(t, err.Error(), "account locked")
}

// TestAuthenticateWithThrottling_LockoutAutoExpires verifies that
// the account auto-unlocks after the lockout duration.
func TestAuthenticateWithThrottling_LockoutAutoExpires(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:              true,
		JWTSecret:            "test-secret-key",
		TokenTTL:             1 * time.Hour,
		AllowPasswordAuth:    true,
		MaxLoginAttempts:     2,
		LoginLockoutDuration: 1 * time.Millisecond, // Very short for testing
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	_, err = am.CreateUser("expireuser", "password-correct-123", []string{RoleNameReadWrite})
	require.NoError(t, err)

	ctx := context.Background()

	// Lock the account with wrong attempts
	_, _ = am.Authenticate(ctx, "expireuser", passwordBuffer(t, "wrong-password-attempt"))
	_, err = am.Authenticate(ctx, "expireuser", passwordBuffer(t, "wrong-password-attempt"))
	assert.Error(t, err)

	// Spin-wait for lockout to expire
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !am.GetAttemptTracker().IsLocked("expireuser") {
			break
		}
	}

	// Should be able to login again with correct password
	token, err := am.Authenticate(ctx, "expireuser", passwordBuffer(t, "password-correct-123"))
	assert.NoError(t, err)
	assert.NotNil(t, token)
}

// TestAuthenticateWithThrottling_DisabledAuthBypassesThrottling verifies
// that the throttle is not invoked when auth is disabled.
func TestAuthenticateWithThrottling_DisabledAuthBypassesThrottling(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:          false,
		MaxLoginAttempts: 3,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = am.Authenticate(ctx, "anyuser", passwordBuffer(t, "anypassword-long"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "authentication is not enabled")

	// No attempts should be recorded since auth is disabled
	assert.Equal(t, int64(0), am.GetAttemptTracker().FailedAttempts("anyuser"))
}

// TestAuthenticateWithThrottling_PasswordAuthDisabledBypassesThrottling
// verifies that the throttle is not invoked when password auth is disabled.
func TestAuthenticateWithThrottling_PasswordAuthDisabledBypassesThrottling(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret",
		AllowPasswordAuth: false,
		MaxLoginAttempts:  3,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = am.Authenticate(ctx, "anyuser", passwordBuffer(t, "anypassword-long"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "password-based authentication is disabled")

	// No attempts should be recorded
	assert.Equal(t, int64(0), am.GetAttemptTracker().FailedAttempts("anyuser"))
}

// TestAuthenticateWithThrottling_DisabledThrottling verifies that when
// MaxLoginAttempts is 0, the throttling is disabled and accounts are never
// locked regardless of how many failed login attempts occur.
func TestAuthenticateWithThrottling_DisabledThrottling(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key-long-enough-32chars",
		TokenTTL:          1 * time.Hour,
		AllowPasswordAuth: true,
		MaxLoginAttempts:  0, // Disabled
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	_, err = am.CreateUser("admin", "correct-password-long", []string{RoleNameAdmin})
	require.NoError(t, err)

	ctx := context.Background()

	// Fail many times - account should never lock
	for i := 0; i < 20; i++ {
		_, err = am.Authenticate(ctx, "admin", passwordBuffer(t, "wrong-password-attempt"))
		assert.Error(t, err)
		assert.False(t, api.IsAccountLocked(err), "account should never lock when throttling is disabled")
	}

	// Tracker should report disabled state
	assert.True(t, am.GetAttemptTracker().IsDisabled())
	assert.Equal(t, int64(0), am.GetAttemptTracker().FailedAttempts("admin"))
	assert.False(t, am.GetAttemptTracker().IsLocked("admin"))

	// Correct password should still work after many failures
	token, err := am.Authenticate(ctx, "admin", passwordBuffer(t, "correct-password-long"))
	assert.NoError(t, err)
	assert.NotNil(t, token)
}

// TestAccountLockedError_Message verifies the error message format.
func TestAccountLockedError_Message(t *testing.T) {
	err := api.NewAccountLockedError("auth.Authenticate", "testuser", 15*time.Minute)
	assert.Contains(t, err.Error(), "account locked for user testuser")
	assert.Contains(t, err.Error(), "too many failed login attempts")
	assert.Contains(t, err.Error(), "retry after")
}

// TestAccountLockedError_IsAccountLocked verifies the IsAccountLocked helper.
func TestAccountLockedError_IsAccountLocked(t *testing.T) {
	t.Run("nil error returns false", func(t *testing.T) {
		assert.False(t, api.IsAccountLocked(nil))
	})

	t.Run("account locked error returns true", func(t *testing.T) {
		err := api.NewAccountLockedError("auth.Authenticate", "user1", 5*time.Minute)
		assert.True(t, api.IsAccountLocked(err))
	})

	t.Run("other error returns false", func(t *testing.T) {
		err := api.NewInvalidCredentialsError("auth.Authenticate")
		assert.False(t, api.IsAccountLocked(err))
	})
}
