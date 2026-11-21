package iam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionManager_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("create_session_success", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		session := &SessionExt{
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-123"),
			IPAddress: "192.0.2.1",
			UserAgent: "Mozilla/5.0",
		}

		err = mgr.Create(ctx, session)
		require.NoError(t, err)
		assert.NotEmpty(t, session.ID)
		assert.False(t, session.Created.IsZero())
		assert.False(t, session.LastActive.IsZero())
		assert.False(t, session.ExpiresAt.IsZero())
	})

	t.Run("create_session_with_id", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		sessionID := uuid.New().String()
		session := &SessionExt{
			ID:        sessionID,
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-123"),
		}

		err = mgr.Create(ctx, session)
		require.NoError(t, err)
		assert.Equal(t, sessionID, session.ID)
	})

	t.Run("create_session_validation_error", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		// Missing required fields
		session := &SessionExt{
			UserID: 10001,
		}

		err = mgr.Create(ctx, session)
		assert.Error(t, err)
	})

	t.Run("create_session_exceeds_limit", func(t *testing.T) {
		policy := &SessionPolicy{
			MaxConcurrent:   2,
			IdleTimeout:     30 * time.Minute,
			AbsoluteTimeout: 24 * time.Hour,
		}

		mgr, err := NewSessionManager(WithSessionPolicy(policy))
		require.NoError(t, err)

		// Create max concurrent sessions
		for i := 0; i < 2; i++ {
			session := &SessionExt{
				UserID:    10001,
				TenantID:  "tenant-1",
				TokenHash: hashToken("token-" + string(rune(i))),
			}
			err = mgr.Create(ctx, session)
			require.NoError(t, err)
		}

		// Try to create one more
		session := &SessionExt{
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-overflow"),
		}
		err = mgr.Create(ctx, session)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSessionLimitExceeded)
	})

	t.Run("create_session_with_device_info", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		trustedUntil := time.Now().Add(7 * 24 * time.Hour)
		session := &SessionExt{
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-123"),
			DeviceInfo: &DeviceInfo{
				ID:           "device-1",
				Name:         "Chrome on MacBook",
				Type:         DeviceTypeBrowser,
				Fingerprint:  "fp-123",
				Trusted:      true,
				TrustedUntil: &trustedUntil,
			},
		}

		err = mgr.Create(ctx, session)
		require.NoError(t, err)

		// Retrieve and verify
		retrieved, err := mgr.Get(ctx, session.ID)
		require.NoError(t, err)
		assert.NotNil(t, retrieved.DeviceInfo)
		assert.Equal(t, "device-1", retrieved.DeviceInfo.ID)
		assert.True(t, retrieved.DeviceInfo.Trusted)
	})
}

func TestSessionManager_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("get_session_success", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		session := &SessionExt{
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-123"),
		}

		err = mgr.Create(ctx, session)
		require.NoError(t, err)

		retrieved, err := mgr.Get(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, session.ID, retrieved.ID)
		assert.Equal(t, session.UserID, retrieved.UserID)
		assert.Equal(t, session.TenantID, retrieved.TenantID)
	})

	t.Run("get_session_not_found", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		_, err = mgr.Get(ctx, "non-existent")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSessionNotFound)
	})

	t.Run("get_expired_session", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		session := &SessionExt{
			ID:        uuid.New().String(),
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-123"),
			ExpiresAt: time.Now().Add(-1 * time.Hour), // Already expired
		}
		session.Created = time.Now().Add(-2 * time.Hour)
		session.LastActive = time.Now().Add(-2 * time.Hour)

		err = mgr.storage.Create(ctx, session)
		require.NoError(t, err)

		_, err = mgr.Get(ctx, session.ID)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSessionExpired)
	})
}

func TestSessionManager_GetByToken(t *testing.T) {
	ctx := context.Background()

	t.Run("get_by_token_success", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		tokenHash := hashToken("token-123")
		session := &SessionExt{
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: tokenHash,
		}

		err = mgr.Create(ctx, session)
		require.NoError(t, err)

		retrieved, err := mgr.GetByToken(ctx, tokenHash)
		require.NoError(t, err)
		assert.Equal(t, session.ID, retrieved.ID)
	})

	t.Run("get_by_token_not_found", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		_, err = mgr.GetByToken(ctx, hashToken("non-existent"))
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSessionNotFound)
	})

	t.Run("get_by_refresh_token", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		refreshHash := hashToken("refresh-123")
		session := &SessionExt{
			UserID:      10001,
			TenantID:    "tenant-1",
			TokenHash:   hashToken("token-123"),
			RefreshHash: refreshHash,
		}

		err = mgr.Create(ctx, session)
		require.NoError(t, err)

		retrieved, err := mgr.GetByToken(ctx, refreshHash)
		require.NoError(t, err)
		assert.Equal(t, session.ID, retrieved.ID)
	})

	t.Run("get_by_token_idle_session", func(t *testing.T) {
		policy := &SessionPolicy{
			IdleTimeout:     1 * time.Millisecond,
			AbsoluteTimeout: 24 * time.Hour,
		}
		storage := NewMemorySessionExtStorage()
		mgr, err := NewSessionManager(WithSessionPolicy(policy), WithSessionStorage(storage))
		require.NoError(t, err)

		tokenHash := hashToken("idle-token-123")
		session := &SessionExt{
			ID:         uuid.New().String(),
			UserID:     40041,
			TenantID:   "tenant-idle",
			TokenHash:  tokenHash,
			Created:    time.Now().Add(-2 * time.Hour),
			LastActive: time.Now().Add(-2 * time.Minute),
			ExpiresAt:  time.Now().Add(24 * time.Hour),
		}
		require.NoError(t, storage.Create(ctx, session))

		_, err = mgr.GetByToken(ctx, tokenHash)
		assert.ErrorIs(t, err, ErrSessionExpired)
	})

	t.Run("get_by_token_cancelled_context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()

		mgr, err := NewSessionManager()
		require.NoError(t, err)

		_, err = mgr.GetByToken(cancelled, "some-token-hash")
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestSessionManager_List(t *testing.T) {
	ctx := context.Background()

	t.Run("list_sessions_success", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		// Create multiple sessions for the same user
		for i := 0; i < 3; i++ {
			session := &SessionExt{
				UserID:    10001,
				TenantID:  "tenant-1",
				TokenHash: hashToken("token-" + string(rune(i))),
			}
			err = mgr.Create(ctx, session)
			require.NoError(t, err)
		}

		// Create session for different user
		otherSession := &SessionExt{
			UserID:    10002,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-other"),
		}
		err = mgr.Create(ctx, otherSession)
		require.NoError(t, err)

		// List sessions for user-1
		sessions, err := mgr.List(ctx, 10001, "tenant-1")
		require.NoError(t, err)
		assert.Len(t, sessions, 3)
	})

	t.Run("list_sessions_empty", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		sessions, err := mgr.List(ctx, 10001, "tenant-1")
		require.NoError(t, err)
		assert.Empty(t, sessions)
	})

	t.Run("list_sessions_filters_expired", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		// Create active session
		activeSession := &SessionExt{
			ID:        uuid.New().String(),
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-active"),
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		activeSession.Created = time.Now()
		activeSession.LastActive = time.Now()
		err = mgr.storage.Create(ctx, activeSession)
		require.NoError(t, err)

		// Create expired session
		expiredSession := &SessionExt{
			ID:        uuid.New().String(),
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-expired"),
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		expiredSession.Created = time.Now().Add(-2 * time.Hour)
		expiredSession.LastActive = time.Now().Add(-2 * time.Hour)
		err = mgr.storage.Create(ctx, expiredSession)
		require.NoError(t, err)

		sessions, err := mgr.List(ctx, 10001, "tenant-1")
		require.NoError(t, err)
		assert.Len(t, sessions, 1)
		assert.Equal(t, activeSession.ID, sessions[0].ID)
	})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	errorCases := []struct {
		name     string
		ctx      context.Context
		userID   uint64
		tenantID string
		wantErr  error
	}{
		{"empty_user_id", ctx, 0, "tenant-1", ErrSessionInvalid},
		{"empty_tenant_id", ctx, 10001, "", ErrSessionInvalid},
		{"cancelled_context", cancelled, 10001, "tenant-1", context.Canceled},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := NewSessionManager()
			require.NoError(t, err)

			_, err = mgr.List(tc.ctx, tc.userID, tc.tenantID)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestSessionManager_UpdateActivity(t *testing.T) {
	ctx := context.Background()

	t.Run("update_activity_success", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		originalActivity := time.Now().Add(-1 * time.Minute)
		session := &SessionExt{
			ID:         uuid.New().String(),
			UserID:     10001,
			TenantID:   "tenant-1",
			TokenHash:  hashToken("token-123"),
			Created:    originalActivity,
			LastActive: originalActivity,
			ExpiresAt:  time.Now().Add(1 * time.Hour),
		}
		require.NoError(t, mgr.storage.Create(ctx, session))

		err = mgr.UpdateActivity(ctx, session.ID)
		require.NoError(t, err)

		retrieved, err := mgr.Get(ctx, session.ID)
		require.NoError(t, err)
		assert.True(t, retrieved.LastActive.After(originalActivity))
	})

	t.Run("update_activity_expired_session", func(t *testing.T) {
		storage := NewMemorySessionExtStorage()
		mgr, err := NewSessionManager(WithSessionStorage(storage))
		require.NoError(t, err)

		session := &SessionExt{
			ID:         uuid.New().String(),
			UserID:     40042,
			TenantID:   "tenant-update-expired",
			TokenHash:  hashToken("expired-update-token"),
			ExpiresAt:  time.Now().Add(-1 * time.Hour),
			Created:    time.Now().Add(-2 * time.Hour),
			LastActive: time.Now().Add(-2 * time.Hour),
		}
		require.NoError(t, storage.Create(ctx, session))

		err = mgr.UpdateActivity(ctx, session.ID)
		assert.ErrorIs(t, err, ErrSessionExpired)
	})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	errorCases := []struct {
		name      string
		ctx       context.Context
		sessionID string
		wantErr   error
	}{
		{"not_found", ctx, "non-existent", ErrSessionNotFound},
		{"empty_session_id", ctx, "", ErrSessionInvalid},
		{"cancelled_context", cancelled, "session-123", context.Canceled},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := NewSessionManager()
			require.NoError(t, err)

			err = mgr.UpdateActivity(tc.ctx, tc.sessionID)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestSessionManager_Revoke(t *testing.T) {
	ctx := context.Background()

	t.Run("revoke_session_success", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		session := &SessionExt{
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-123"),
		}

		err = mgr.Create(ctx, session)
		require.NoError(t, err)

		err = mgr.Revoke(ctx, session.ID)
		require.NoError(t, err)

		_, err = mgr.Get(ctx, session.ID)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSessionNotFound)
	})

	t.Run("revoke_session_idempotent", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		err = mgr.Revoke(ctx, "non-existent")
		assert.NoError(t, err) // Should not error
	})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	errorCases := []struct {
		name      string
		ctx       context.Context
		sessionID string
		wantErr   error
	}{
		{"empty_session_id", ctx, "", ErrSessionInvalid},
		{"cancelled_context", cancelled, "session-123", context.Canceled},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := NewSessionManager()
			require.NoError(t, err)

			err = mgr.Revoke(tc.ctx, tc.sessionID)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestSessionManager_RevokeAllForUser(t *testing.T) {
	ctx := context.Background()

	t.Run("revoke_all_success", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		// Create multiple sessions
		for i := 0; i < 3; i++ {
			session := &SessionExt{
				UserID:    10001,
				TenantID:  "tenant-1",
				TokenHash: hashToken("token-" + string(rune(i))),
			}
			err = mgr.Create(ctx, session)
			require.NoError(t, err)
		}

		err = mgr.RevokeAllForUser(ctx, 10001, "tenant-1")
		require.NoError(t, err)

		sessions, err := mgr.List(ctx, 10001, "tenant-1")
		require.NoError(t, err)
		assert.Empty(t, sessions)
	})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	errorCases := []struct {
		name     string
		ctx      context.Context
		userID   uint64
		tenantID string
		wantErr  error
	}{
		{"empty_user_id", ctx, 0, "tenant-1", ErrSessionInvalid},
		{"empty_tenant_id", ctx, 10001, "", ErrSessionInvalid},
		{"cancelled_context", cancelled, 10001, "tenant-1", context.Canceled},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := NewSessionManager()
			require.NoError(t, err)

			err = mgr.RevokeAllForUser(tc.ctx, tc.userID, tc.tenantID)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestSessionManager_RevokeAllExcept(t *testing.T) {
	ctx := context.Background()

	t.Run("revoke_all_except_success", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		var keepSessionID string

		// Create multiple sessions
		for i := 0; i < 3; i++ {
			session := &SessionExt{
				UserID:    10001,
				TenantID:  "tenant-1",
				TokenHash: hashToken("token-" + string(rune(i))),
			}
			err = mgr.Create(ctx, session)
			require.NoError(t, err)

			if i == 1 {
				keepSessionID = session.ID
			}
		}

		err = mgr.RevokeAllExcept(ctx, 10001, "tenant-1", keepSessionID)
		require.NoError(t, err)

		sessions, err := mgr.List(ctx, 10001, "tenant-1")
		require.NoError(t, err)
		assert.Len(t, sessions, 1)
		assert.Equal(t, keepSessionID, sessions[0].ID)
	})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	errorCases := []struct {
		name            string
		ctx             context.Context
		userID          uint64
		tenantID        string
		exceptSessionID string
		wantErr         error
	}{
		{"empty_user_id", ctx, 0, "tenant-1", "session-1", ErrSessionInvalid},
		{"empty_tenant_id", ctx, 10001, "", "session-1", ErrSessionInvalid},
		{"empty_except_session_id", ctx, 10001, "tenant-1", "", ErrSessionInvalid},
		{"cancelled_context", cancelled, 10001, "tenant-1", "session-1", context.Canceled},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := NewSessionManager()
			require.NoError(t, err)

			err = mgr.RevokeAllExcept(tc.ctx, tc.userID, tc.tenantID, tc.exceptSessionID)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestSessionManager_CleanupExpired(t *testing.T) {
	ctx := context.Background()

	t.Run("cleanup_expired_sessions", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		// Create active session
		activeSession := &SessionExt{
			ID:        uuid.New().String(),
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-active"),
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		activeSession.Created = time.Now()
		activeSession.LastActive = time.Now()
		err = mgr.storage.Create(ctx, activeSession)
		require.NoError(t, err)

		// Create expired sessions
		for i := 0; i < 3; i++ {
			expiredSession := &SessionExt{
				ID:        uuid.New().String(),
				UserID:    10001,
				TenantID:  "tenant-1",
				TokenHash: hashToken("token-expired-" + string(rune(i))),
				ExpiresAt: time.Now().Add(-1 * time.Hour),
			}
			expiredSession.Created = time.Now().Add(-2 * time.Hour)
			expiredSession.LastActive = time.Now().Add(-2 * time.Hour)
			err = mgr.storage.Create(ctx, expiredSession)
			require.NoError(t, err)
		}

		count, err := mgr.CleanupExpired(ctx)
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		// Verify active session still exists
		_, err = mgr.Get(ctx, activeSession.ID)
		assert.NoError(t, err)
	})

	t.Run("cleanup_cancelled_context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()

		mgr, err := NewSessionManager()
		require.NoError(t, err)

		_, err = mgr.CleanupExpired(cancelled)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestSessionManager_ValidateSession(t *testing.T) {
	ctx := context.Background()

	t.Run("validate_session_success", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		session := &SessionExt{
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-123"),
		}

		err = mgr.Create(ctx, session)
		require.NoError(t, err)

		err = mgr.ValidateSession(ctx, session.ID)
		assert.NoError(t, err)
	})

	t.Run("validate_expired_session", func(t *testing.T) {
		mgr, err := NewSessionManager()
		require.NoError(t, err)

		session := &SessionExt{
			ID:        uuid.New().String(),
			UserID:    10001,
			TenantID:  "tenant-1",
			TokenHash: hashToken("token-123"),
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		session.Created = time.Now().Add(-2 * time.Hour)
		session.LastActive = time.Now().Add(-2 * time.Hour)

		err = mgr.storage.Create(ctx, session)
		require.NoError(t, err)

		err = mgr.ValidateSession(ctx, session.ID)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrSessionExpired)
	})

	t.Run("validate_idle_session", func(t *testing.T) {
		policy := &SessionPolicy{
			IdleTimeout:     1 * time.Millisecond,
			AbsoluteTimeout: 24 * time.Hour,
		}
		storage := NewMemorySessionExtStorage()
		mgr, err := NewSessionManager(WithSessionPolicy(policy), WithSessionStorage(storage))
		require.NoError(t, err)

		session := &SessionExt{
			ID:         uuid.New().String(),
			UserID:     40043,
			TenantID:   "tenant-validate",
			TokenHash:  hashToken("validate-token"),
			Created:    time.Now().Add(-1 * time.Hour),
			LastActive: time.Now().Add(-1 * time.Hour),
			ExpiresAt:  time.Now().Add(1 * time.Hour),
		}
		require.NoError(t, storage.Create(ctx, session))

		err = mgr.ValidateSession(ctx, session.ID)
		assert.ErrorIs(t, err, ErrSessionExpired)
	})

	t.Run("validate_cancelled_context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()

		mgr, err := NewSessionManager()
		require.NoError(t, err)

		err = mgr.ValidateSession(cancelled, "session-123")
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestSessionManager_EnforcePolicy(t *testing.T) {
	ctx := context.Background()

	t.Run("enforce_max_concurrent", func(t *testing.T) {
		policy := &SessionPolicy{
			MaxConcurrent:   2,
			IdleTimeout:     30 * time.Minute,
			AbsoluteTimeout: 24 * time.Hour,
		}

		mgr, err := NewSessionManager(WithSessionPolicy(policy))
		require.NoError(t, err)

		// Create more sessions than allowed
		for i := 0; i < 5; i++ {
			session := &SessionExt{
				ID:        uuid.New().String(),
				UserID:    10001,
				TenantID:  "tenant-1",
				TokenHash: hashToken("token-" + string(rune(i))),
				ExpiresAt: time.Now().Add(1 * time.Hour),
			}
			session.Created = time.Now()
			session.LastActive = time.Now().Add(time.Duration(-i) * time.Minute)
			err = mgr.storage.Create(ctx, session)
			require.NoError(t, err)
		}

		err = mgr.EnforcePolicy(ctx, 10001, "tenant-1", policy)
		require.NoError(t, err)

		sessions, err := mgr.List(ctx, 10001, "tenant-1")
		require.NoError(t, err)
		assert.LessOrEqual(t, len(sessions), policy.MaxConcurrent)
	})
}

func TestDeviceInfo_Validation(t *testing.T) {
	t.Run("valid_device_info", func(t *testing.T) {
		device := &DeviceInfo{
			ID:          "device-1",
			Name:        "Chrome on MacBook",
			Type:        DeviceTypeBrowser,
			Fingerprint: "fp-123",
		}

		err := device.Validate()
		assert.NoError(t, err)
	})

	t.Run("invalid_device_type", func(t *testing.T) {
		device := &DeviceInfo{
			ID:   "device-1",
			Type: "invalid",
		}

		err := device.Validate()
		assert.Error(t, err)
	})

	t.Run("missing_device_id", func(t *testing.T) {
		device := &DeviceInfo{
			Type: DeviceTypeBrowser,
		}

		err := device.Validate()
		assert.Error(t, err)
	})
}

func TestSessionStorage_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	const numGoroutines = 10
	const numOperations = 100

	done := make(chan bool, numGoroutines)

	// Concurrent creates
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				session := &SessionExt{
					ID:        uuid.New().String(),
					UserID:    10001,
					TenantID:  "tenant-1",
					TokenHash: hashToken("token-" + string(rune(id)) + "-" + string(rune(j))),
					ExpiresAt: time.Now().Add(1 * time.Hour),
				}
				session.Created = time.Now()
				session.LastActive = time.Now()
				_ = storage.Create(ctx, session)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify sessions were created
	sessions, err := storage.ListByUser(ctx, 10001, "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, numGoroutines*numOperations, len(sessions))
}

func TestSessionTenantIsolation(t *testing.T) {
	ctx := context.Background()
	mgr, err := NewSessionManager()
	require.NoError(t, err)

	// Create sessions in different tenants
	session1 := &SessionExt{
		UserID:    10001,
		TenantID:  "tenant-1",
		TokenHash: hashToken("token-a"),
	}
	err = mgr.Create(ctx, session1)
	require.NoError(t, err)

	session2 := &SessionExt{
		UserID:    10001,
		TenantID:  "tenant-2",
		TokenHash: hashToken("token-b"),
	}
	err = mgr.Create(ctx, session2)
	require.NoError(t, err)

	// Verify tenant isolation
	tenant1Sessions, err := mgr.List(ctx, 10001, "tenant-1")
	require.NoError(t, err)
	assert.Len(t, tenant1Sessions, 1)
	assert.Equal(t, "tenant-1", tenant1Sessions[0].TenantID)

	tenant2Sessions, err := mgr.List(ctx, 10001, "tenant-2")
	require.NoError(t, err)
	assert.Len(t, tenant2Sessions, 1)
	assert.Equal(t, "tenant-2", tenant2Sessions[0].TenantID)
}

// Helper functions

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func makeTestSession(id string, userID uint64, tenantID, tokenHash string) *SessionExt {
	now := time.Now()
	return &SessionExt{
		ID:         id,
		UserID:     userID,
		TenantID:   tenantID,
		TokenHash:  tokenHash,
		IPAddress:  "127.0.0.1",
		UserAgent:  "test-agent",
		Created:    now,
		LastActive: now,
		ExpiresAt:  now.Add(1 * time.Hour),
	}
}

func TestMemorySessionExtStorage_Update_Success(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	session := makeTestSession("s1", 20001, "t1", "hash1")
	if err := storage.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update token hash
	session.TokenHash = "hash2"
	session.RefreshHash = "refresh1"
	if err := storage.Update(ctx, session); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify by new token
	result, err := storage.GetByToken(ctx, "hash2")
	if err != nil {
		t.Fatalf("GetByToken failed: %v", err)
	}
	if result.TokenHash != "hash2" {
		t.Fatalf("expected hash2, got %s", result.TokenHash)
	}

	// Old token hash should not work
	_, err = storage.GetByToken(ctx, "hash1")
	if err == nil {
		t.Fatal("expected error getting by old token hash")
	}
}

func TestMemorySessionExtStorage_Update_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	session := makeTestSession("nonexistent", 20001, "t1", "hash1")
	err := storage.Update(ctx, session)
	if err == nil {
		t.Fatal("expected error updating nonexistent session")
	}
}

func TestMemorySessionExtStorage_Update_ChangeRefreshHash(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	session := makeTestSession("s1", 20001, "t1", "hash1")
	session.RefreshHash = "refresh_old"
	if err := storage.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Change refresh hash
	session.RefreshHash = "refresh_new"
	if err := storage.Update(ctx, session); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Should be findable by new refresh hash
	result, err := storage.GetByToken(ctx, "refresh_new")
	if err != nil {
		t.Fatalf("GetByToken with new refresh hash failed: %v", err)
	}
	if result.RefreshHash != "refresh_new" {
		t.Fatalf("expected refresh_new, got %s", result.RefreshHash)
	}
}

func TestMemorySessionExtStorage_GetByToken_Empty(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	_, err := storage.GetByToken(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty token hash")
	}
}

func TestMemorySessionExtStorage_Get_EmptyID(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	_, err := storage.Get(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
}

func TestMemorySessionExtStorage_Delete_EmptyID(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	err := storage.Delete(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
}

func TestMemorySessionExtStorage_Delete_WithRefreshHash(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	session := makeTestSession("s1", 20001, "t1", "hash1")
	session.RefreshHash = "refresh1"
	if err := storage.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := storage.Delete(ctx, "s1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := storage.Get(ctx, "s1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
	if _, err := storage.GetByToken(ctx, "refresh1"); err == nil {
		t.Fatal("expected refresh hash index to be removed after delete")
	}
}

func TestMemorySessionExtStorage_DeleteByUser_Success(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	s1 := makeTestSession("s1", 20001, "t1", "hash1")
	s2 := makeTestSession("s2", 20001, "t1", "hash2")
	s2.RefreshHash = "refresh2"
	s3 := makeTestSession("s3", 20002, "t1", "hash3")

	for _, s := range []*SessionExt{s1, s2, s3} {
		if err := storage.Create(ctx, s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	if err := storage.DeleteByUser(ctx, 20001, "t1"); err != nil {
		t.Fatalf("DeleteByUser failed: %v", err)
	}

	// user1 sessions should be gone
	sessions, err := storage.ListByUser(ctx, 20001, "t1")
	if err != nil {
		t.Fatalf("ListByUser failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions for user1, got %d", len(sessions))
	}

	// user2 session should still exist
	sessions2, err := storage.ListByUser(ctx, 20002, "t1")
	if err != nil {
		t.Fatalf("ListByUser user2 failed: %v", err)
	}
	if len(sessions2) != 1 {
		t.Fatalf("expected 1 session for user2, got %d", len(sessions2))
	}
}

func TestMemorySessionExtStorage_DeleteByUser_NoSessions(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	err := storage.DeleteByUser(ctx, 99997, "t1")
	if err != nil {
		t.Fatalf("DeleteByUser on nonexistent user should not error: %v", err)
	}
}

func TestMemorySessionExtStorage_DeleteAllExcept_Success(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	s1 := makeTestSession("s1", 20001, "t1", "hash1")
	s2 := makeTestSession("s2", 20001, "t1", "hash2")
	s3 := makeTestSession("s3", 20001, "t1", "hash3")
	s3.RefreshHash = "refresh3"

	for _, s := range []*SessionExt{s1, s2, s3} {
		if err := storage.Create(ctx, s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	if err := storage.DeleteAllExcept(ctx, 20001, "t1", "s2"); err != nil {
		t.Fatalf("DeleteAllExcept failed: %v", err)
	}

	// Only s2 should remain
	sessions, err := storage.ListByUser(ctx, 20001, "t1")
	if err != nil {
		t.Fatalf("ListByUser failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "s2" {
		t.Fatalf("expected s2 to remain, got %s", sessions[0].ID)
	}
}

func TestMemorySessionExtStorage_DeleteAllExcept_NoSessions(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	err := storage.DeleteAllExcept(ctx, 99997, "t1", "s1")
	if err != nil {
		t.Fatalf("DeleteAllExcept on nonexistent user should not error: %v", err)
	}
}

func TestMemorySessionExtStorage_CleanupExpired_Success(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	now := time.Now()
	active := &SessionExt{
		ID:         "active",
		UserID:     20001,
		TenantID:   "t1",
		TokenHash:  "hash_active",
		Created:    now,
		LastActive: now,
		ExpiresAt:  now.Add(1 * time.Hour),
	}
	expired := &SessionExt{
		ID:          "expired",
		UserID:      20001,
		TenantID:    "t1",
		TokenHash:   "hash_expired",
		RefreshHash: "refresh_expired",
		Created:     now.Add(-2 * time.Hour),
		LastActive:  now.Add(-2 * time.Hour),
		ExpiresAt:   now.Add(-1 * time.Hour),
	}

	if err := storage.Create(ctx, active); err != nil {
		t.Fatalf("Create active failed: %v", err)
	}
	if err := storage.Create(ctx, expired); err != nil {
		t.Fatalf("Create expired failed: %v", err)
	}

	count, err := storage.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("CleanupExpired failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 expired session removed, got %d", count)
	}

	// Active session should still exist
	_, err = storage.Get(ctx, "active")
	if err != nil {
		t.Fatalf("active session should still exist: %v", err)
	}

	// Expired session should be gone
	_, err = storage.Get(ctx, "expired")
	if err == nil {
		t.Fatal("expired session should have been removed")
	}
}

func TestMemorySessionExtStorage_CopySession_WithDeviceInfoAndMetadata(t *testing.T) {
	ctx := context.Background()
	storage := NewMemorySessionExtStorage()

	session := makeTestSession("s1", 20001, "t1", "hash1")
	session.DeviceInfo = &DeviceInfo{
		ID:   "device1",
		Name: "Chrome",
		Type: DeviceTypeBrowser,
	}
	session.Metadata = map[string]string{
		"source": "web",
		"env":    "test",
	}

	if err := storage.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	result, err := storage.Get(ctx, "s1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// Verify deep copy of device info
	if result.DeviceInfo == nil {
		t.Fatal("expected DeviceInfo to be copied")
	}
	if result.DeviceInfo.ID != "device1" {
		t.Fatalf("expected device ID device1, got %s", result.DeviceInfo.ID)
	}

	// Verify deep copy of metadata
	if result.Metadata == nil {
		t.Fatal("expected Metadata to be copied")
	}
	if result.Metadata["source"] != "web" {
		t.Fatalf("expected source=web, got %s", result.Metadata["source"])
	}
}

func TestSessionExt_Validate_MissingTenantID(t *testing.T) {
	session := &SessionExt{
		ID:        "s1",
		UserID:    20001,
		TokenHash: "hash",
	}
	err := session.Validate()
	if err == nil {
		t.Fatal("expected error for missing tenant ID")
	}
}

// =============================================================================
// Session Manager - RevokeAllForUser, RevokeAllExcept tests
// =============================================================================

func TestOIDCProvider_Token_ClientCredentialsGrantFinal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create RSA key for signing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create dependencies
	memStorage := NewMemoryStorage()
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	appStorage := newMockAppStorage()
	tokenStorage := newMockTokenStorage()

	// Create tenant
	tenantID := "tenant-cc-final"
	tenant := &Tenant{
		ID:      tenantID,
		Name:    "Client Credentials Tenant",
		Created: time.Now(),
		Updated: time.Now(),
	}
	err = memStorage.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create app with client_credentials grant
	// Seed the current PBKDF2 credential form: the storages verify only that
	// form since #37, and a legacy digest is refused rather than checked.
	clientID := "cc-client-" + uuid.New().String()[:8]
	plainSecret := "secret-" + uuid.New().String()
	hashedSecret := hashClientSecretFinal(t, plainSecret)

	app := &Application{
		ClientID:     clientID,
		ClientSecret: hashedSecret,
		TenantID:     tenantID,
		Name:         "CC Test App",
		GrantTypes:   []string{GrantTypeClientCredentials},
		RedirectURIs: []string{"https://example.com/callback"},
		Created:      time.Now(),
		Updated:      time.Now(),
	}
	err = appStorage.CreateApp(ctx, app)
	require.NoError(t, err)

	// Create OIDC provider with full configuration (including SigningAlgorithm)
	oidcConfig := &OIDCConfig{
		Issuer:            "https://auth.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testSigningKeyManager(t, privateKey, "RS256"),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   7 * 24 * time.Hour,
		IDTokenTTL:        1 * time.Hour,
		AuthCodeTTL:       10 * time.Minute,
	}

	deps := OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    appStorage,
		TokenStorage:  tokenStorage,
	}

	provider, err := NewOIDCProvider(oidcConfig, deps)
	require.NoError(t, err)

	// Test client credentials grant with plain text secret
	tokenReq := &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     clientID,
		ClientSecret: plainSecret, // Use plain text secret in request
		Scope:        []string{"read"},
	}

	resp, err := provider.Token(ctx, tokenReq)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.AccessToken)
	assert.Equal(t, TokenTypeBearer, resp.TokenType)
	assert.Greater(t, resp.ExpiresIn, int64(0))
}

func TestOIDCProvider_Token_ClientCredentialsGrant_InvalidClientFinal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	memStorage := NewMemoryStorage()
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	appStorage := newMockAppStorage()
	tokenStorage := newMockTokenStorage()

	oidcConfig := &OIDCConfig{
		Issuer:            "https://auth.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testSigningKeyManager(t, privateKey, "RS256"),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   7 * 24 * time.Hour,
		IDTokenTTL:        1 * time.Hour,
		AuthCodeTTL:       10 * time.Minute,
	}

	deps := OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    appStorage,
		TokenStorage:  tokenStorage,
	}

	provider, err := NewOIDCProvider(oidcConfig, deps)
	require.NoError(t, err)

	tokenReq := &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     "non-existent-client",
		ClientSecret: "wrong-secret",
	}

	_, err = provider.Token(ctx, tokenReq)
	assert.Error(t, err)
	var oauthErr *OAuthError
	assert.ErrorAs(t, err, &oauthErr)
}

func TestOIDCProvider_Token_ClientCredentialsGrant_GrantNotAllowedFinal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	memStorage := NewMemoryStorage()
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	appStorage := newMockAppStorage()
	tokenStorage := newMockTokenStorage()

	// Create tenant
	tenantID := "tenant-no-cc-final"
	tenant := &Tenant{
		ID:      tenantID,
		Name:    "No CC Tenant",
		Created: time.Now(),
		Updated: time.Now(),
	}
	err = memStorage.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create app WITHOUT client_credentials grant
	// Seed the current PBKDF2 credential form (#37).
	clientID := "no-cc-client-" + uuid.New().String()[:8]
	plainSecret := "secret-" + uuid.New().String()
	hashedSecret := hashClientSecretFinal(t, plainSecret)

	app := &Application{
		ClientID:     clientID,
		ClientSecret: hashedSecret,
		TenantID:     tenantID,
		Name:         "No CC App",
		GrantTypes:   []string{GrantTypeAuthorizationCode}, // Only auth code
		RedirectURIs: []string{"https://example.com/callback"},
		Created:      time.Now(),
		Updated:      time.Now(),
	}
	err = appStorage.CreateApp(ctx, app)
	require.NoError(t, err)

	oidcConfig := &OIDCConfig{
		Issuer:            "https://auth.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testSigningKeyManager(t, privateKey, "RS256"),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   7 * 24 * time.Hour,
		IDTokenTTL:        1 * time.Hour,
		AuthCodeTTL:       10 * time.Minute,
	}

	deps := OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    appStorage,
		TokenStorage:  tokenStorage,
	}

	provider, err := NewOIDCProvider(oidcConfig, deps)
	require.NoError(t, err)

	tokenReq := &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     clientID,
		ClientSecret: plainSecret, // Use plain text secret in request
	}

	_, err = provider.Token(ctx, tokenReq)
	assert.Error(t, err)
	var oauthErr *OAuthError
	assert.ErrorAs(t, err, &oauthErr)
	assert.Equal(t, ErrOAuthUnauthorizedClient, oauthErr.ErrorCode)
}

// =============================================================================
// Policy Engine - SimulatePolicy and GetEffectivePermissions tests
// =============================================================================

// simplePolicySimulator is a minimal, self-contained PolicySimulator test
// double: exact subject-ID match, exact action match, and resource
// prefix-wildcard match ("/data/*" matches "/data/test"). It exists purely
// to exercise PolicyEngine.SimulatePolicy's request-shape validation and
// response plumbing in this package's tests without depending on qrdb (the
// real implementation, pkg/server's qrdbPolicySimulator, is exercised by
// pkg/server's own tests and by pkg/iam's TestPEPService_* guard tests via
// a real abac.Decorator).
type simplePolicySimulator struct{}

func (simplePolicySimulator) Simulate(_ context.Context, policy *Policy, req *CheckRequest) (*CheckResponse, error) {
	subjectMatches := false
	if len(policy.Spec.Subjects.Users) == 0 {
		subjectMatches = true
	}
	for _, u := range policy.Spec.Subjects.Users {
		if u == req.Subject.ID {
			subjectMatches = true
		}
	}
	if !subjectMatches {
		return &CheckResponse{Allowed: false, Reason: "subject does not match policy"}, nil
	}

	for _, rule := range policy.Spec.Rules {
		if !matchAction(rule.Actions, req.Action) {
			continue
		}
		for _, pattern := range rule.Resources {
			prefix := strings.TrimSuffix(pattern, "*")
			if !strings.HasPrefix(req.Resource, prefix) {
				continue
			}
			if rule.Effect == EffectDeny {
				return &CheckResponse{
					Allowed: false, MatchedPolicy: policy.Metadata.Name, MatchedRule: rule.Name,
					Reason: "explicit deny",
				}, nil
			}
			return &CheckResponse{
				Allowed: true, MatchedPolicy: policy.Metadata.Name, MatchedRule: rule.Name,
				Reason: "allowed by rule",
			}, nil
		}
	}
	return &CheckResponse{Allowed: false, Reason: "no matching rule"}, nil
}

func TestPolicyEngine_SimulatePolicy_SuccessFinal(t *testing.T) {
	ctx := context.Background()

	storage := newMockPolicyStorage()
	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage:   storage,
		Simulator: simplePolicySimulator{},
	})

	policy := &Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:        "test-sim-policy",
			Tenant:      "tenant-sim-1",
			Description: "Test policy for simulation",
		},
		Spec: PolicySpec{
			Subjects: SubjectSelector{
				Users: []string{"user-sim-123"},
			},
			Rules: []Rule{
				{
					Name:      "allow-read",
					Effect:    EffectAllow,
					Actions:   []string{"read"},
					Resources: []string{"/data/*"},
				},
			},
		},
	}

	req := &PolicySimulationRequest{
		Policy:       policy,
		IncludeTrace: true,
		CheckRequest: &CheckRequest{
			Subject: Subject{
				ID:       "user-sim-123",
				TenantID: "tenant-sim-1",
			},
			Action:   "read",
			Resource: "/data/test",
		},
	}

	result, err := engine.SimulatePolicy(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Allowed)
	assert.NotEmpty(t, result.EvaluationSteps)
}

func TestPolicyEngine_SimulatePolicy_InvalidRequestFinal(t *testing.T) {
	ctx := context.Background()

	policy := &Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:   "test-nil-check-policy",
			Tenant: "tenant-1",
		},
	}

	tests := []struct {
		name string
		req  *PolicySimulationRequest
	}{
		{"nil_request", nil},
		{"nil_policy", &PolicySimulationRequest{Policy: nil, CheckRequest: &CheckRequest{}}},
		{"nil_check_request", &PolicySimulationRequest{Policy: policy, CheckRequest: nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: newMockPolicyStorage(),
			})

			_, err := engine.SimulatePolicy(ctx, tt.req)
			assert.Error(t, err)
		})
	}
}

func TestPolicyEngine_SimulatePolicy_DenyEffectFinal(t *testing.T) {
	ctx := context.Background()

	storage := newMockPolicyStorage()
	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage:   storage,
		Simulator: simplePolicySimulator{},
	})

	policy := &Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:        "deny-policy-final",
			Tenant:      "tenant-deny-1",
			Description: "Policy that denies",
		},
		Spec: PolicySpec{
			Subjects: SubjectSelector{
				Users: []string{"user-deny-456"},
			},
			Rules: []Rule{
				{
					Name:      "deny-delete",
					Effect:    EffectDeny,
					Actions:   []string{"delete"},
					Resources: []string{"/admin/*"},
				},
			},
		},
	}

	req := &PolicySimulationRequest{
		Policy:       policy,
		IncludeTrace: true,
		CheckRequest: &CheckRequest{
			Subject: Subject{
				ID:       "user-deny-456",
				TenantID: "tenant-deny-1",
			},
			Action:   "delete",
			Resource: "/admin/config",
		},
	}

	result, err := engine.SimulatePolicy(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Allowed)
	assert.Equal(t, "explicit deny", result.Reason)
}

func TestPolicyEngine_GetEffectivePermissions_SuccessFinal(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-perms-final"

	storage := newMockPolicyStorage()
	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
	})

	// Create a policy
	policy := &Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:   "resource-policy-final",
			Tenant: tenantID,
		},
		Spec: PolicySpec{
			Subjects: SubjectSelector{
				Users: []string{"user-perms-final"},
			},
			Rules: []Rule{
				{
					Name:      "allow-crud",
					Effect:    EffectAllow,
					Actions:   []string{"create", "read", "update"},
					Resources: []string{"/resources/*"},
				},
			},
		},
	}
	err := storage.CreatePolicy(ctx, tenantID, policy)
	require.NoError(t, err)

	req := &EffectivePermissionsRequest{
		Subject: &Subject{
			ID:       "user-perms-final",
			TenantID: tenantID,
		},
		ResourcePatterns: []string{"/resources/*"},
		Actions:          []string{"create", "read", "update", "delete"},
	}

	result, err := engine.GetEffectivePermissions(ctx, tenantID, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, result.Subject)
	assert.NotNil(t, result.Permissions)
}

func TestPolicyEngine_GetEffectivePermissions_InvalidRequestFinal(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		req  *EffectivePermissionsRequest
	}{
		{"nil_request", nil},
		{"nil_subject", &EffectivePermissionsRequest{Subject: nil, ResourcePatterns: []string{"/test/*"}}},
		{"empty_resource_patterns", &EffectivePermissionsRequest{Subject: &Subject{ID: "user-1"}, ResourcePatterns: []string{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewPolicyEngine(PolicyEngineOptions{
				Storage: newMockPolicyStorage(),
			})

			_, err := engine.GetEffectivePermissions(ctx, "tenant-1", tt.req)
			assert.Error(t, err)
		})
	}
}

// =============================================================================
// Backend Storage - 0% coverage functions tests
// =============================================================================

func TestBackendStorage_ListTenantsFinal(t *testing.T) {
	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	// Create some tenants
	for i := 0; i < 5; i++ {
		tenant := &Tenant{
			ID:      "tenant-list-" + uuid.New().String()[:8],
			Name:    "Test Tenant",
			Created: time.Now(),
			Updated: time.Now(),
		}
		err := storage.CreateTenant(ctx, tenant)
		require.NoError(t, err)
	}

	// List all tenants
	tenants, err := storage.ListTenants(ctx, ListOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(tenants), 5)

	// Test pagination with limit
	tenants, err = storage.ListTenants(ctx, ListOptions{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, tenants, 2)

	// Test pagination with offset
	tenants, err = storage.ListTenants(ctx, ListOptions{Offset: 3})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(tenants), 2)

	// Test offset beyond count
	tenants, err = storage.ListTenants(ctx, ListOptions{Offset: 100})
	require.NoError(t, err)
	assert.Len(t, tenants, 0)
}

func TestBackendStorage_CountTenantsFinal(t *testing.T) {
	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	// Initial count
	count, err := storage.CountTenants(ctx, ListOptions{})
	require.NoError(t, err)
	initialCount := count

	// Create tenants
	for i := 0; i < 3; i++ {
		tenant := &Tenant{
			ID:      "count-tenant-final-" + uuid.New().String()[:8],
			Name:    "Count Test",
			Created: time.Now(),
			Updated: time.Now(),
		}
		err := storage.CreateTenant(ctx, tenant)
		require.NoError(t, err)
	}

	count, err = storage.CountTenants(ctx, ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, initialCount+3, count)
}

func TestBackendStorage_ListRolesFinal(t *testing.T) {
	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)
	tenantID := "roles-tenant-final"

	// Create tenant first
	tenant := &Tenant{ID: tenantID, Name: "Roles Tenant", Created: time.Now(), Updated: time.Now()}
	err := storage.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create roles. Each carries its OWN name: role names are unique within a
	// tenant in every store (the canonical qrdb one refuses a collision with
	// RoleNameExistsError), so three roles sharing one name is a state
	// production cannot reach.
	for i := 0; i < 3; i++ {
		role := &Role{
			ID:       "role-final-" + uuid.New().String()[:8],
			TenantID: tenantID,
			Name:     "Test Role " + strconv.Itoa(i),
			Created:  time.Now(),
			Updated:  time.Now(),
		}
		err := storage.CreateRole(ctx, role)
		require.NoError(t, err)
	}

	// List all roles
	roles, err := storage.ListRoles(ctx, tenantID, ListOptions{})
	require.NoError(t, err)
	assert.Len(t, roles, 3)
}

func TestBackendStorage_ListAppsFinal(t *testing.T) {
	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)
	tenantID := "apps-tenant-final"

	// Create tenant first
	tenant := &Tenant{ID: tenantID, Name: "Apps Tenant", Created: time.Now(), Updated: time.Now()}
	err := storage.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create apps
	for i := 0; i < 3; i++ {
		app := &Application{
			ClientID:     "app-final-" + uuid.New().String()[:8],
			TenantID:     tenantID,
			Name:         "Test App",
			ClientSecret: "hashed-secret",
			Created:      time.Now(),
			Updated:      time.Now(),
		}
		err := storage.CreateApp(ctx, app)
		require.NoError(t, err)
	}

	// List all apps
	apps, err := storage.ListApps(ctx, tenantID, ListOptions{})
	require.NoError(t, err)
	assert.Len(t, apps, 3)
}

func TestBackendStorage_ValidateClientSecretFinal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)
	tenantID := "secret-tenant-final"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Secret Tenant", Created: time.Now(), Updated: time.Now()}
	err := storage.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create app with the current PBKDF2 credential form (#37).
	clientID := "secret-app-final-" + uuid.New().String()[:8]
	plainSecret := "my-secret-password-final"
	hashedSecret := hashClientSecretFinal(t, plainSecret)

	app := &Application{
		ClientID:     clientID,
		TenantID:     tenantID,
		Name:         "Secret App",
		ClientSecret: hashedSecret,
		Created:      time.Now(),
		Updated:      time.Now(),
	}
	err = storage.CreateApp(ctx, app)
	require.NoError(t, err)

	// Test valid secret
	valid, err := storage.ValidateClientSecret(ctx, clientID, plainSecret)
	require.NoError(t, err)
	assert.True(t, valid)

	// Test invalid secret
	valid, err = storage.ValidateClientSecret(ctx, clientID, "wrong-secret")
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestBackendStorage_GetTokenByValueFinal(t *testing.T) {
	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	// Create a refresh token
	jti := uuid.New().String()
	secret := uuid.New().String()
	token := &Token{
		JTI:       jti,
		TokenType: TokenTypeRefresh,
		Subject:   "user-token-value-123",
		TenantID:  "tenant-a",
		Expires:   time.Now().Add(24 * time.Hour),
		ValueHash: HashTokenValue(secret),
	}
	err := storage.StoreToken(ctx, token)
	require.NoError(t, err)

	// Retrieve by the opaque value the client presents.
	retrieved, err := storage.GetTokenByValue(ctx, secret)
	require.NoError(t, err)
	assert.Equal(t, token.JTI, retrieved.JTI)
}

func TestBackendStorage_DeleteExpiredTokensFinal(t *testing.T) {
	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	// Create expired token
	expiredJTI := "expired-final-" + uuid.New().String()[:8]
	expiredToken := &Token{
		JTI:       expiredJTI,
		TokenType: TokenTypeAccess,
		Subject:   "user-expired-123",
		TenantID:  "tenant-expired-1",
		Expires:   time.Now().Add(-1 * time.Hour), // Already expired
	}
	err := storage.StoreToken(ctx, expiredToken)
	require.NoError(t, err)

	// Create valid token
	validJTI := "valid-final-" + uuid.New().String()[:8]
	validToken := &Token{
		JTI:       validJTI,
		TokenType: TokenTypeAccess,
		Subject:   "user-valid-123",
		TenantID:  "tenant-valid-1",
		Expires:   time.Now().Add(1 * time.Hour),
	}
	err = storage.StoreToken(ctx, validToken)
	require.NoError(t, err)

	// Delete expired
	err = storage.DeleteExpiredTokens(ctx)
	require.NoError(t, err)

	// Verify expired token is gone
	_, err = storage.GetToken(ctx, expiredJTI)
	assert.Error(t, err)

	// Verify valid token remains
	retrieved, err := storage.GetToken(ctx, validJTI)
	require.NoError(t, err)
	assert.Equal(t, validJTI, retrieved.JTI)
}

func TestBackendStorage_ListTokensByUserFinal(t *testing.T) {
	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	userID := "token-user-final"

	// Create tokens for user
	for i := 0; i < 3; i++ {
		token := &Token{
			JTI:       "user-token-final-" + uuid.New().String()[:8],
			TokenType: TokenTypeAccess,
			Subject:   userID,
			TenantID:  "tenant-tokens-1",
			Expires:   time.Now().Add(1 * time.Hour),
		}
		err := storage.StoreToken(ctx, token)
		require.NoError(t, err)
	}

	// Create token for different user
	otherToken := &Token{
		JTI:       "other-token-final-" + uuid.New().String()[:8],
		TokenType: TokenTypeAccess,
		Subject:   "other-user-final",
		TenantID:  "tenant-tokens-1",
		Expires:   time.Now().Add(1 * time.Hour),
	}
	err := storage.StoreToken(ctx, otherToken)
	require.NoError(t, err)

	// List tokens by user
	tokens, err := storage.ListTokensByUser(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, tokens, 3)
	for _, token := range tokens {
		assert.Equal(t, userID, token.Subject)
	}
}

func TestBackendStorage_RevokeUserTokensFinal(t *testing.T) {
	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	userID := "token-user-final"

	// Create tokens for user
	var jtis []string
	for i := 0; i < 3; i++ {
		jti := "revoke-token-final-" + uuid.New().String()[:8]
		jtis = append(jtis, jti)
		token := &Token{
			JTI:       jti,
			TokenType: TokenTypeAccess,
			Subject:   userID,
			TenantID:  "tenant-revoke-1",
			Expires:   time.Now().Add(1 * time.Hour),
		}
		err := storage.StoreToken(ctx, token)
		require.NoError(t, err)
	}

	// Revoke all tokens for user
	err := storage.RevokeUserTokens(ctx, userID)
	require.NoError(t, err)

	// Verify all tokens are revoked
	for _, jti := range jtis {
		token, err := storage.GetToken(ctx, jti)
		require.NoError(t, err)
		assert.True(t, token.Revoked)
	}
}

func TestBackendStorage_ListPoliciesFinal(t *testing.T) {
	ctx := context.Background()

	kv := newMockKVStore()
	storage := NewBackendStorage(kv)
	tenantID := "policies-tenant-final"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Policies Tenant", Created: time.Now(), Updated: time.Now()}
	err := storage.CreateTenant(ctx, tenant)
	require.NoError(t, err)

	// Create policies
	for i := 0; i < 4; i++ {
		policy := &Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:   "policy-final-" + uuid.New().String()[:8],
				Tenant: tenantID,
			},
			Spec: PolicySpec{
				Subjects: SubjectSelector{Authenticated: true},
				Rules:    []Rule{{Name: "rule-1", Effect: EffectAllow, Actions: []string{"read"}}},
			},
		}
		err := storage.CreatePolicy(ctx, tenantID, policy)
		require.NoError(t, err)
	}

	// List policies
	policies, err := storage.ListPolicies(ctx, tenantID, ListOptions{})
	require.NoError(t, err)
	assert.Len(t, policies, 4)

	// Test pagination
	policies, err = storage.ListPolicies(ctx, tenantID, ListOptions{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, policies, 2)
}

// =============================================================================
// Session Storage Coverage Improvement Tests
// =============================================================================

// =============================================================================
// SessionExt - IsIdle tests
// =============================================================================

func TestSessionExt_IsIdle_Policies(t *testing.T) {
	tests := []struct {
		name       string
		policy     *SessionPolicy
		lastActive time.Duration
		want       bool
	}{
		{"within_timeout", &SessionPolicy{IdleTimeout: 30 * time.Minute}, -10 * time.Minute, false},
		{"beyond_timeout", &SessionPolicy{IdleTimeout: 30 * time.Minute}, -45 * time.Minute, true},
		{"nil_policy", nil, -2 * time.Hour, false},
		{"zero_timeout", &SessionPolicy{IdleTimeout: 0}, -24 * time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &SessionExt{LastActive: time.Now().Add(tt.lastActive)}
			assert.Equal(t, tt.want, session.IsIdle(tt.policy))
		})
	}
}

// =============================================================================
// Helper functions
// =============================================================================

func hashTokenForTestFinal(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// hashClientSecretFinal produces the PBKDF2 digest BackendStorage.ValidateClientSecret
// expects since the #37 migration. These tests exercise session and token flows,
// so they seed the current credential form rather than a legacy bcrypt digest.
func hashClientSecretFinal(t *testing.T, secret string) string {
	t.Helper()
	return currentSecretHash(t, secret)
}

// =============================================================================
// Tests for session.go - WithSessionStorage option
// =============================================================================

// =============================================================================
// Tests for session_types.go - SessionPolicy.Validate
// =============================================================================

func TestSessionPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  *SessionPolicy
		wantErr bool
	}{
		{
			name:    "valid default policy",
			policy:  &SessionPolicy{},
			wantErr: false,
		},
		{
			name:    "negative max concurrent",
			policy:  &SessionPolicy{MaxConcurrent: -1},
			wantErr: true,
		},
		{
			name:    "negative idle timeout",
			policy:  &SessionPolicy{IdleTimeout: -1 * time.Hour},
			wantErr: true,
		},
		{
			name:    "negative absolute timeout",
			policy:  &SessionPolicy{AbsoluteTimeout: -1 * time.Hour},
			wantErr: true,
		},
		{
			name:    "negative remember device days",
			policy:  &SessionPolicy{RememberDeviceDays: -1},
			wantErr: true,
		},
		{
			name: "valid timeouts",
			policy: &SessionPolicy{
				IdleTimeout:        30 * time.Minute,
				AbsoluteTimeout:    24 * time.Hour,
				MaxConcurrent:      5,
				RememberDeviceDays: 30,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Tests for session_storage.go - MemorySessionExtStorage.Update
// =============================================================================

func TestSessionExtCopyBehavior(t *testing.T) {
	// Test that MemorySessionExtStorage returns copies, not original references
	storage := NewMemorySessionExtStorage()
	ctx := context.Background()

	original := &SessionExt{
		ID:         uuid.New().String(),
		TenantID:   "tenant-1",
		UserID:     10001,
		TokenHash:  "token-hash-12345678901234567890",
		Created:    time.Now(),
		LastActive: time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
		UserAgent:  "test-agent",
		IPAddress:  "127.0.0.1",
	}

	err := storage.Create(ctx, original)
	require.NoError(t, err)

	// Get the session
	retrieved, err := storage.Get(ctx, original.ID)
	require.NoError(t, err)

	// Verify field values match
	assert.Equal(t, original.ID, retrieved.ID)
	assert.Equal(t, original.TenantID, retrieved.TenantID)
	assert.Equal(t, original.UserID, retrieved.UserID)
	assert.Equal(t, original.TokenHash, retrieved.TokenHash)

	// Verify it's a copy, not same reference
	assert.NotSame(t, original, retrieved)

	// Modifying retrieved should not affect stored session
	retrieved.UserAgent = "modified-agent"
	retrievedAgain, err := storage.Get(ctx, original.ID)
	require.NoError(t, err)
	assert.Equal(t, "test-agent", retrievedAgain.UserAgent)
}

// =============================================================================
// Tests for storage_memory.go - Policy CRUD operations
// =============================================================================

func TestMemoryStorage_PolicyCRUD(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenantID := "tenant-policy"
	policy := &Policy{
		APIVersion: "abac.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:   "test-policy",
			Tenant: tenantID,
		},
		Spec: PolicySpec{
			Rules: []Rule{
				{
					Name:      "allow-all",
					Effect:    EffectAllow,
					Actions:   []string{"*"},
					Resources: []string{"*"},
				},
			},
		},
	}

	t.Run("create policy", func(t *testing.T) {
		err := storage.CreatePolicy(ctx, tenantID, policy)
		require.NoError(t, err)
	})

	t.Run("get policy", func(t *testing.T) {
		retrieved, err := storage.GetPolicy(ctx, tenantID, "test-policy")
		require.NoError(t, err)
		assert.Equal(t, policy.Metadata.Name, retrieved.Metadata.Name)
	})

	t.Run("update policy", func(t *testing.T) {
		policy.Spec.Rules[0].Effect = EffectDeny
		err := storage.UpdatePolicy(ctx, tenantID, policy)
		require.NoError(t, err)

		retrieved, err := storage.GetPolicy(ctx, tenantID, "test-policy")
		require.NoError(t, err)
		assert.Equal(t, EffectDeny, retrieved.Spec.Rules[0].Effect)
	})

	t.Run("list policies", func(t *testing.T) {
		policies, err := storage.ListPolicies(ctx, tenantID, ListOptions{})
		require.NoError(t, err)
		assert.Len(t, policies, 1)
	})

	t.Run("delete policy", func(t *testing.T) {
		err := storage.DeletePolicy(ctx, tenantID, "test-policy")
		require.NoError(t, err)

		_, err = storage.GetPolicy(ctx, tenantID, "test-policy")
		assert.Error(t, err)
	})
}

// =============================================================================
// Tests for policy.go - matchAction
// =============================================================================

func TestMatchActionCoverage(t *testing.T) {
	// Note: matchAction only supports exact match or full wildcard "*"
	// It does NOT support prefix wildcards like "read*"
	tests := []struct {
		name    string
		actions []string
		action  string
		match   bool
	}{
		{
			name:    "exact match",
			actions: []string{"read"},
			action:  "read",
			match:   true,
		},
		{
			name:    "wildcard match all",
			actions: []string{"*"},
			action:  "read",
			match:   true,
		},
		{
			name:    "no match",
			actions: []string{"write"},
			action:  "read",
			match:   false,
		},
		{
			name:    "prefix pattern does not match (not supported)",
			actions: []string{"read*"},
			action:  "read:users",
			match:   false,
		},
		{
			name:    "multiple actions with match",
			actions: []string{"write", "read", "delete"},
			action:  "read",
			match:   true,
		},
		{
			name:    "multiple actions no match",
			actions: []string{"write", "delete"},
			action:  "read",
			match:   false,
		},
		{
			name:    "empty actions",
			actions: []string{},
			action:  "read",
			match:   false,
		},
		{
			name:    "wildcard among multiple actions",
			actions: []string{"write", "*", "delete"},
			action:  "any-action",
			match:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchAction(tt.actions, tt.action)
			assert.Equal(t, tt.match, result)
		})
	}
}
