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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper functions

func createTestSecret(userID, tenantID string) *MFASecret {
	return &MFASecret{
		ID:          "secret-" + userID,
		UserID:      userID,
		TenantID:    tenantID,
		Secret:      testTOTPSecret,
		Algorithm:   MFAAlgorithmSHA1,
		Digits:      6,
		Period:      30,
		Issuer:      "QuicBaaS",
		AccountName: userID + "@quicbaas.io",
		Verified:    false,
		Created:     time.Now(),
	}
}

func createTestEnrollment(id, userID, tenantID string, expired bool) *MFAEnrollment {
	expiresAt := time.Now().Add(10 * time.Minute)
	if expired {
		expiresAt = time.Now().Add(-1 * time.Minute)
	}
	return &MFAEnrollment{
		ID:          id,
		UserID:      userID,
		TenantID:    tenantID,
		Method:      MFAMethodTOTP,
		Secret:      testTOTPSecret,
		QRCodeURI:   "otpauth://totp/test",
		ExpiresAt:   expiresAt,
		Attempts:    0,
		MaxAttempts: 5,
		Created:     time.Now(),
	}
}

func createTestChallenge(id, userID, tenantID string, expired bool) *MFAChallenge {
	expiresAt := time.Now().Add(5 * time.Minute)
	if expired {
		expiresAt = time.Now().Add(-1 * time.Minute)
	}
	return &MFAChallenge{
		ID:           id,
		UserID:       userID,
		TenantID:     tenantID,
		SessionToken: "session-token-" + id,
		Method:       MFAMethodTOTP,
		ExpiresAt:    expiresAt,
		Attempts:     0,
		MaxAttempts:  3,
		Created:      time.Now(),
		IPAddress:    "192.168.1.1",
		UserAgent:    "TestAgent/1.0",
	}
}

func createTestBackupCodes(userID, tenantID string) *BackupCodeSet {
	return &BackupCodeSet{
		ID:       "backup-" + userID,
		UserID:   userID,
		TenantID: tenantID,
		Codes: []BackupCode{
			{Code: "code1", UsedAt: nil},
			{Code: "code2", UsedAt: nil},
			{Code: "code3", UsedAt: nil},
		},
		Created: time.Now(),
	}
}

// TestMemoryMFAStorage_NewMemoryMFAStorage tests the constructor.
func TestMemoryMFAStorage_NewMemoryMFAStorage(t *testing.T) {
	storage := NewMemoryMFAStorage()
	require.NotNil(t, storage)
	assert.NotNil(t, storage.secrets)
	assert.NotNil(t, storage.enrollments)
	assert.NotNil(t, storage.challenges)
	assert.NotNil(t, storage.backupCodes)
	assert.Equal(t, 0, len(storage.secrets))
	assert.Equal(t, 0, len(storage.enrollments))
	assert.Equal(t, 0, len(storage.challenges))
	assert.Equal(t, 0, len(storage.backupCodes))
}

// TestMemoryMFAStorage_SecretOperations tests secret CRUD operations.
func TestMemoryMFAStorage_SecretOperations(t *testing.T) {
	t.Run("save and get secret successfully", func(t *testing.T) {
		storage := NewMemoryMFAStorage()
		ctx := context.Background()
		secret := createTestSecret("user1", "tenant1")

		// Test SaveSecret
		err := storage.SaveSecret(ctx, secret)
		require.NoError(t, err)

		// Test GetSecret
		retrieved, err := storage.GetSecret(ctx, "user1", "tenant1")
		require.NoError(t, err)
		assert.Equal(t, secret.ID, retrieved.ID)
		assert.Equal(t, secret.UserID, retrieved.UserID)
		assert.Equal(t, secret.TenantID, retrieved.TenantID)
		assert.Equal(t, secret.Secret, retrieved.Secret)

		// Verify it's a copy (mutation shouldn't affect stored version)
		retrieved.Secret = "MODIFIED"
		retrieved2, err := storage.GetSecret(ctx, "user1", "tenant1")
		require.NoError(t, err)
		assert.NotEqual(t, "MODIFIED", retrieved2.Secret)
	})

	t.Run("get non-existent secret", func(t *testing.T) {
		storage := NewMemoryMFAStorage()
		ctx := context.Background()

		// Test GetSecret for non-existent secret
		_, err := storage.GetSecret(ctx, "nonexistent", "tenant1")
		assert.ErrorIs(t, err, ErrMFANotEnrolled)
	})

	t.Run("update existing secret", func(t *testing.T) {
		storage := NewMemoryMFAStorage()
		ctx := context.Background()

		// Create initial secret
		secret := createTestSecret("user2", "tenant1")
		err := storage.SaveSecret(ctx, secret)
		require.NoError(t, err)

		// Update the secret
		secret.Verified = true
		err = storage.SaveSecret(ctx, secret)
		require.NoError(t, err)

		// Verify update
		retrieved, err := storage.GetSecret(ctx, "user2", "tenant1")
		require.NoError(t, err)
		assert.True(t, retrieved.Verified)
	})

	t.Run("delete secret", func(t *testing.T) {
		storage := NewMemoryMFAStorage()
		ctx := context.Background()
		secret := createTestSecret("user3", "tenant1")

		// Save secret
		err := storage.SaveSecret(ctx, secret)
		require.NoError(t, err)

		// Delete secret
		err = storage.DeleteSecret(ctx, "user3", "tenant1")
		require.NoError(t, err)

		// Verify deletion
		_, err = storage.GetSecret(ctx, "user3", "tenant1")
		assert.ErrorIs(t, err, ErrMFANotEnrolled)
	})

	t.Run("delete non-existent secret", func(t *testing.T) {
		storage := NewMemoryMFAStorage()
		ctx := context.Background()

		// Delete non-existent secret should not error
		err := storage.DeleteSecret(ctx, "nonexistent", "tenant1")
		assert.NoError(t, err)
	})
}

// TestMemoryMFAStorage_SaveSecret_Validation tests secret validation on save.
func TestMemoryMFAStorage_SaveSecret_Validation(t *testing.T) {
	tests := []struct {
		name    string
		secret  *MFASecret
		wantErr bool
	}{
		{
			name:    "valid secret",
			secret:  createTestSecret("user1", "tenant1"),
			wantErr: false,
		},
		{
			name: "missing ID",
			secret: &MFASecret{
				UserID:   "user1",
				TenantID: "tenant1",
				Secret:   "secret",
				Digits:   6,
				Period:   30,
			},
			wantErr: true,
		},
		{
			name: "missing UserID",
			secret: &MFASecret{
				ID:       "secret1",
				TenantID: "tenant1",
				Secret:   "secret",
				Digits:   6,
				Period:   30,
			},
			wantErr: true,
		},
		{
			name: "missing TenantID",
			secret: &MFASecret{
				ID:     "secret1",
				UserID: "user1",
				Secret: "secret",
				Digits: 6,
				Period: 30,
			},
			wantErr: true,
		},
		{
			name: "missing Secret",
			secret: &MFASecret{
				ID:       "secret1",
				UserID:   "user1",
				TenantID: "tenant1",
				Digits:   6,
				Period:   30,
			},
			wantErr: true,
		},
		{
			name: "invalid Digits too low",
			secret: &MFASecret{
				ID:       "secret1",
				UserID:   "user1",
				TenantID: "tenant1",
				Secret:   "secret",
				Digits:   5,
				Period:   30,
			},
			wantErr: true,
		},
		{
			name: "invalid Digits too high",
			secret: &MFASecret{
				ID:       "secret1",
				UserID:   "user1",
				TenantID: "tenant1",
				Secret:   "secret",
				Digits:   9,
				Period:   30,
			},
			wantErr: true,
		},
		{
			name: "invalid Period too low",
			secret: &MFASecret{
				ID:       "secret1",
				UserID:   "user1",
				TenantID: "tenant1",
				Secret:   "secret",
				Digits:   6,
				Period:   10,
			},
			wantErr: true,
		},
		{
			name: "invalid Period too high",
			secret: &MFASecret{
				ID:       "secret1",
				UserID:   "user1",
				TenantID: "tenant1",
				Secret:   "secret",
				Digits:   6,
				Period:   150,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryMFAStorage()
			err := storage.SaveSecret(context.Background(), tt.secret)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMemoryMFAStorage_EnrollmentOperations tests enrollment CRUD operations.
func TestMemoryMFAStorage_EnrollmentOperations(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	enrollment := createTestEnrollment("enroll1", "user1", "tenant1", false)

	// Test SaveEnrollment
	err := storage.SaveEnrollment(ctx, enrollment)
	require.NoError(t, err)

	// Test GetEnrollment
	retrieved, err := storage.GetEnrollment(ctx, "enroll1")
	require.NoError(t, err)
	assert.Equal(t, enrollment.ID, retrieved.ID)
	assert.Equal(t, enrollment.UserID, retrieved.UserID)
	assert.Equal(t, enrollment.TenantID, retrieved.TenantID)

	// Verify it's a copy
	retrieved.Secret = "MODIFIED"
	retrieved2, err := storage.GetEnrollment(ctx, "enroll1")
	require.NoError(t, err)
	assert.NotEqual(t, "MODIFIED", retrieved2.Secret)

	// Test DeleteEnrollment
	err = storage.DeleteEnrollment(ctx, "enroll1")
	require.NoError(t, err)

	// Verify deletion
	_, err = storage.GetEnrollment(ctx, "enroll1")
	assert.ErrorIs(t, err, ErrMFAEnrollmentNotFound)
}

// TestMemoryMFAStorage_GetEnrollment_NotFound tests getting non-existent enrollment.
func TestMemoryMFAStorage_GetEnrollment_NotFound(t *testing.T) {
	storage := NewMemoryMFAStorage()
	_, err := storage.GetEnrollment(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrMFAEnrollmentNotFound)
}

// TestMemoryMFAStorage_SaveEnrollment_Validation tests enrollment validation.
func TestMemoryMFAStorage_SaveEnrollment_Validation(t *testing.T) {
	tests := []struct {
		name       string
		enrollment *MFAEnrollment
		wantErr    bool
	}{
		{
			name:       "valid enrollment",
			enrollment: createTestEnrollment("enroll1", "user1", "tenant1", false),
			wantErr:    false,
		},
		{
			name: "missing ID",
			enrollment: &MFAEnrollment{
				UserID:   "user1",
				TenantID: "tenant1",
				Secret:   "secret",
			},
			wantErr: true,
		},
		{
			name: "missing UserID",
			enrollment: &MFAEnrollment{
				ID:       "enroll1",
				TenantID: "tenant1",
				Secret:   "secret",
			},
			wantErr: true,
		},
		{
			name: "missing TenantID",
			enrollment: &MFAEnrollment{
				ID:     "enroll1",
				UserID: "user1",
				Secret: "secret",
			},
			wantErr: true,
		},
		{
			name: "missing Secret",
			enrollment: &MFAEnrollment{
				ID:       "enroll1",
				UserID:   "user1",
				TenantID: "tenant1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryMFAStorage()
			err := storage.SaveEnrollment(context.Background(), tt.enrollment)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMemoryMFAStorage_DeleteEnrollmentsByUser tests deleting all enrollments for a user.
func TestMemoryMFAStorage_DeleteEnrollmentsByUser(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	// Create enrollments for different users
	enroll1 := createTestEnrollment("enroll1", "user1", "tenant1", false)
	enroll2 := createTestEnrollment("enroll2", "user1", "tenant1", false)
	enroll3 := createTestEnrollment("enroll3", "user2", "tenant1", false)
	enroll4 := createTestEnrollment("enroll4", "user1", "tenant2", false)

	require.NoError(t, storage.SaveEnrollment(ctx, enroll1))
	require.NoError(t, storage.SaveEnrollment(ctx, enroll2))
	require.NoError(t, storage.SaveEnrollment(ctx, enroll3))
	require.NoError(t, storage.SaveEnrollment(ctx, enroll4))

	// Delete enrollments for user1 in tenant1
	err := storage.DeleteEnrollmentsByUser(ctx, "user1", "tenant1")
	require.NoError(t, err)

	// Verify user1 tenant1 enrollments are deleted
	_, err = storage.GetEnrollment(ctx, "enroll1")
	assert.ErrorIs(t, err, ErrMFAEnrollmentNotFound)
	_, err = storage.GetEnrollment(ctx, "enroll2")
	assert.ErrorIs(t, err, ErrMFAEnrollmentNotFound)

	// Verify other enrollments still exist
	_, err = storage.GetEnrollment(ctx, "enroll3")
	assert.NoError(t, err)
	_, err = storage.GetEnrollment(ctx, "enroll4")
	assert.NoError(t, err)
}

// TestMemoryMFAStorage_ChallengeOperations tests challenge CRUD operations.
func TestMemoryMFAStorage_ChallengeOperations(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	challenge := createTestChallenge("chal1", "user1", "tenant1", false)

	// Test SaveChallenge
	err := storage.SaveChallenge(ctx, challenge)
	require.NoError(t, err)

	// Test GetChallenge
	retrieved, err := storage.GetChallenge(ctx, "chal1")
	require.NoError(t, err)
	assert.Equal(t, challenge.ID, retrieved.ID)
	assert.Equal(t, challenge.UserID, retrieved.UserID)
	assert.Equal(t, challenge.SessionToken, retrieved.SessionToken)

	// Verify it's a copy
	retrieved.SessionToken = "MODIFIED"
	retrieved2, err := storage.GetChallenge(ctx, "chal1")
	require.NoError(t, err)
	assert.NotEqual(t, "MODIFIED", retrieved2.SessionToken)

	// Test DeleteChallenge
	err = storage.DeleteChallenge(ctx, "chal1")
	require.NoError(t, err)

	// Verify deletion
	_, err = storage.GetChallenge(ctx, "chal1")
	assert.ErrorIs(t, err, ErrMFAChallengeNotFound)
}

// TestMemoryMFAStorage_GetChallenge_NotFound tests getting non-existent challenge.
func TestMemoryMFAStorage_GetChallenge_NotFound(t *testing.T) {
	storage := NewMemoryMFAStorage()
	_, err := storage.GetChallenge(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrMFAChallengeNotFound)
}

// TestMemoryMFAStorage_SaveChallenge_Validation tests challenge validation.
func TestMemoryMFAStorage_SaveChallenge_Validation(t *testing.T) {
	tests := []struct {
		name      string
		challenge *MFAChallenge
		wantErr   bool
	}{
		{
			name:      "valid challenge",
			challenge: createTestChallenge("chal1", "user1", "tenant1", false),
			wantErr:   false,
		},
		{
			name: "missing ID",
			challenge: &MFAChallenge{
				UserID:       "user1",
				TenantID:     "tenant1",
				SessionToken: "token",
			},
			wantErr: true,
		},
		{
			name: "missing UserID",
			challenge: &MFAChallenge{
				ID:           "chal1",
				TenantID:     "tenant1",
				SessionToken: "token",
			},
			wantErr: true,
		},
		{
			name: "missing TenantID",
			challenge: &MFAChallenge{
				ID:           "chal1",
				UserID:       "user1",
				SessionToken: "token",
			},
			wantErr: true,
		},
		{
			name: "missing SessionToken",
			challenge: &MFAChallenge{
				ID:       "chal1",
				UserID:   "user1",
				TenantID: "tenant1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryMFAStorage()
			err := storage.SaveChallenge(context.Background(), tt.challenge)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMemoryMFAStorage_DeleteChallengesByUser tests deleting all challenges for a user.
func TestMemoryMFAStorage_DeleteChallengesByUser(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	// Create challenges for different users
	chal1 := createTestChallenge("chal1", "user1", "tenant1", false)
	chal2 := createTestChallenge("chal2", "user1", "tenant1", false)
	chal3 := createTestChallenge("chal3", "user2", "tenant1", false)
	chal4 := createTestChallenge("chal4", "user1", "tenant2", false)

	require.NoError(t, storage.SaveChallenge(ctx, chal1))
	require.NoError(t, storage.SaveChallenge(ctx, chal2))
	require.NoError(t, storage.SaveChallenge(ctx, chal3))
	require.NoError(t, storage.SaveChallenge(ctx, chal4))

	// Delete challenges for user1 in tenant1
	err := storage.DeleteChallengesByUser(ctx, "user1", "tenant1")
	require.NoError(t, err)

	// Verify user1 tenant1 challenges are deleted
	_, err = storage.GetChallenge(ctx, "chal1")
	assert.ErrorIs(t, err, ErrMFAChallengeNotFound)
	_, err = storage.GetChallenge(ctx, "chal2")
	assert.ErrorIs(t, err, ErrMFAChallengeNotFound)

	// Verify other challenges still exist
	_, err = storage.GetChallenge(ctx, "chal3")
	assert.NoError(t, err)
	_, err = storage.GetChallenge(ctx, "chal4")
	assert.NoError(t, err)
}

// TestMemoryMFAStorage_BackupCodesOperations tests backup codes CRUD operations.
func TestMemoryMFAStorage_BackupCodesOperations(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	codes := createTestBackupCodes("user1", "tenant1")

	// Test SaveBackupCodes
	err := storage.SaveBackupCodes(ctx, codes)
	require.NoError(t, err)

	// Test GetBackupCodes
	retrieved, err := storage.GetBackupCodes(ctx, "user1", "tenant1")
	require.NoError(t, err)
	assert.Equal(t, codes.ID, retrieved.ID)
	assert.Equal(t, codes.UserID, retrieved.UserID)
	assert.Equal(t, len(codes.Codes), len(retrieved.Codes))

	// Verify deep copy (mutation shouldn't affect stored version)
	retrieved.Codes[0].Code = "MODIFIED"
	retrieved2, err := storage.GetBackupCodes(ctx, "user1", "tenant1")
	require.NoError(t, err)
	assert.NotEqual(t, "MODIFIED", retrieved2.Codes[0].Code)

	// Test DeleteBackupCodes
	err = storage.DeleteBackupCodes(ctx, "user1", "tenant1")
	require.NoError(t, err)

	// Verify deletion
	_, err = storage.GetBackupCodes(ctx, "user1", "tenant1")
	assert.ErrorIs(t, err, ErrMFANotEnrolled)
}

// TestMemoryMFAStorage_GetBackupCodes_NotFound tests getting non-existent backup codes.
func TestMemoryMFAStorage_GetBackupCodes_NotFound(t *testing.T) {
	storage := NewMemoryMFAStorage()
	_, err := storage.GetBackupCodes(context.Background(), "nonexistent", "tenant1")
	assert.ErrorIs(t, err, ErrMFANotEnrolled)
}

// TestMemoryMFAStorage_SaveBackupCodes_Validation tests backup codes validation.
func TestMemoryMFAStorage_SaveBackupCodes_Validation(t *testing.T) {
	tests := []struct {
		name    string
		codes   *BackupCodeSet
		wantErr bool
	}{
		{
			name:    "valid backup codes",
			codes:   createTestBackupCodes("user1", "tenant1"),
			wantErr: false,
		},
		{
			name: "missing ID",
			codes: &BackupCodeSet{
				UserID:   "user1",
				TenantID: "tenant1",
				Codes:    []BackupCode{{Code: "code1"}},
			},
			wantErr: true,
		},
		{
			name: "missing UserID",
			codes: &BackupCodeSet{
				ID:       "backup1",
				TenantID: "tenant1",
				Codes:    []BackupCode{{Code: "code1"}},
			},
			wantErr: true,
		},
		{
			name: "missing TenantID",
			codes: &BackupCodeSet{
				ID:     "backup1",
				UserID: "user1",
				Codes:  []BackupCode{{Code: "code1"}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryMFAStorage()
			err := storage.SaveBackupCodes(context.Background(), tt.codes)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMemoryMFAStorage_CleanupExpired tests expired item cleanup.
func TestMemoryMFAStorage_CleanupExpired(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	// Create a mix of expired and valid items
	validEnroll := createTestEnrollment("valid-enroll", "user1", "tenant1", false)
	expiredEnroll := createTestEnrollment("expired-enroll", "user2", "tenant1", true)

	validChallenge := createTestChallenge("valid-chal", "user1", "tenant1", false)
	expiredChallenge := createTestChallenge("expired-chal", "user2", "tenant1", true)

	require.NoError(t, storage.SaveEnrollment(ctx, validEnroll))
	require.NoError(t, storage.SaveEnrollment(ctx, expiredEnroll))
	require.NoError(t, storage.SaveChallenge(ctx, validChallenge))
	require.NoError(t, storage.SaveChallenge(ctx, expiredChallenge))

	// Run cleanup
	count, err := storage.CleanupExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify expired items are removed
	_, err = storage.GetEnrollment(ctx, "expired-enroll")
	assert.ErrorIs(t, err, ErrMFAEnrollmentNotFound)
	_, err = storage.GetChallenge(ctx, "expired-chal")
	assert.ErrorIs(t, err, ErrMFAChallengeNotFound)

	// Verify valid items remain
	_, err = storage.GetEnrollment(ctx, "valid-enroll")
	assert.NoError(t, err)
	_, err = storage.GetChallenge(ctx, "valid-chal")
	assert.NoError(t, err)
}

// TestMemoryMFAStorage_CleanupExpired_Empty tests cleanup with no expired items.
func TestMemoryMFAStorage_CleanupExpired_Empty(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	// Create only valid items
	validEnroll := createTestEnrollment("valid-enroll", "user1", "tenant1", false)
	validChallenge := createTestChallenge("valid-chal", "user1", "tenant1", false)

	require.NoError(t, storage.SaveEnrollment(ctx, validEnroll))
	require.NoError(t, storage.SaveChallenge(ctx, validChallenge))

	// Run cleanup
	count, err := storage.CleanupExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify items still exist
	_, err = storage.GetEnrollment(ctx, "valid-enroll")
	assert.NoError(t, err)
	_, err = storage.GetChallenge(ctx, "valid-chal")
	assert.NoError(t, err)
}

// TestMemoryMFAStorage_ContextCancellation tests context cancellation handling.
func TestMemoryMFAStorage_ContextCancellation(t *testing.T) {
	storage := NewMemoryMFAStorage()
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	secret := createTestSecret("user1", "tenant1")
	enrollment := createTestEnrollment("enroll1", "user1", "tenant1", false)
	challenge := createTestChallenge("chal1", "user1", "tenant1", false)
	codes := createTestBackupCodes("user1", "tenant1")

	// Test all operations with canceled context
	t.Run("GetSecret", func(t *testing.T) {
		_, err := storage.GetSecret(canceledCtx, "user1", "tenant1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SaveSecret", func(t *testing.T) {
		err := storage.SaveSecret(canceledCtx, secret)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DeleteSecret", func(t *testing.T) {
		err := storage.DeleteSecret(canceledCtx, "user1", "tenant1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("GetEnrollment", func(t *testing.T) {
		_, err := storage.GetEnrollment(canceledCtx, "enroll1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SaveEnrollment", func(t *testing.T) {
		err := storage.SaveEnrollment(canceledCtx, enrollment)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DeleteEnrollment", func(t *testing.T) {
		err := storage.DeleteEnrollment(canceledCtx, "enroll1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DeleteEnrollmentsByUser", func(t *testing.T) {
		err := storage.DeleteEnrollmentsByUser(canceledCtx, "user1", "tenant1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("GetChallenge", func(t *testing.T) {
		_, err := storage.GetChallenge(canceledCtx, "chal1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SaveChallenge", func(t *testing.T) {
		err := storage.SaveChallenge(canceledCtx, challenge)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DeleteChallenge", func(t *testing.T) {
		err := storage.DeleteChallenge(canceledCtx, "chal1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DeleteChallengesByUser", func(t *testing.T) {
		err := storage.DeleteChallengesByUser(canceledCtx, "user1", "tenant1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("GetBackupCodes", func(t *testing.T) {
		_, err := storage.GetBackupCodes(canceledCtx, "user1", "tenant1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SaveBackupCodes", func(t *testing.T) {
		err := storage.SaveBackupCodes(canceledCtx, codes)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DeleteBackupCodes", func(t *testing.T) {
		err := storage.DeleteBackupCodes(canceledCtx, "user1", "tenant1")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("CleanupExpired", func(t *testing.T) {
		_, err := storage.CleanupExpired(canceledCtx)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// TestMemoryMFAStorage_ConcurrentAccess tests thread safety.
func TestMemoryMFAStorage_ConcurrentAccess(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 4) // 4 operation types

	// Concurrent secret operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			userID := "user-" + string(rune(id%10))
			tenantID := "tenant1"

			for j := 0; j < numOperations; j++ {
				secret := createTestSecret(userID, tenantID)
				_ = storage.SaveSecret(ctx, secret)
				_, _ = storage.GetSecret(ctx, userID, tenantID)
				if j%10 == 0 {
					_ = storage.DeleteSecret(ctx, userID, tenantID)
				}
			}
		}(i)
	}

	// Concurrent enrollment operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				enrollID := "enroll-" + string(rune(id)) + "-" + string(rune(j))
				enroll := createTestEnrollment(enrollID, "user1", "tenant1", false)
				_ = storage.SaveEnrollment(ctx, enroll)
				_, _ = storage.GetEnrollment(ctx, enrollID)
				if j%10 == 0 {
					_ = storage.DeleteEnrollment(ctx, enrollID)
				}
			}
		}(i)
	}

	// Concurrent challenge operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				chalID := "chal-" + string(rune(id)) + "-" + string(rune(j))
				chal := createTestChallenge(chalID, "user1", "tenant1", false)
				_ = storage.SaveChallenge(ctx, chal)
				_, _ = storage.GetChallenge(ctx, chalID)
				if j%10 == 0 {
					_ = storage.DeleteChallenge(ctx, chalID)
				}
			}
		}(i)
	}

	// Concurrent backup code operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			userID := "user-" + string(rune(id%10))
			tenantID := "tenant1"

			for j := 0; j < numOperations; j++ {
				codes := createTestBackupCodes(userID, tenantID)
				_ = storage.SaveBackupCodes(ctx, codes)
				_, _ = storage.GetBackupCodes(ctx, userID, tenantID)
				if j%10 == 0 {
					_ = storage.DeleteBackupCodes(ctx, userID, tenantID)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify no race conditions occurred and data is consistent
	// Run cleanup to verify it works after concurrent access
	_, err := storage.CleanupExpired(ctx)
	assert.NoError(t, err)
}

// TestMemoryMFAStorage_ConcurrentDeleteByUser tests concurrent deletion operations.
func TestMemoryMFAStorage_ConcurrentDeleteByUser(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	// Setup: Create multiple enrollments and challenges
	for i := 0; i < 100; i++ {
		enroll := createTestEnrollment("enroll-"+string(rune(i)), "user1", "tenant1", false)
		chal := createTestChallenge("chal-"+string(rune(i)), "user1", "tenant1", false)
		require.NoError(t, storage.SaveEnrollment(ctx, enroll))
		require.NoError(t, storage.SaveChallenge(ctx, chal))
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Concurrent delete operations
	go func() {
		defer wg.Done()
		_ = storage.DeleteEnrollmentsByUser(ctx, "user1", "tenant1")
	}()

	go func() {
		defer wg.Done()
		_ = storage.DeleteChallengesByUser(ctx, "user1", "tenant1")
	}()

	wg.Wait()

	// Verify all items were deleted
	storage.mu.RLock()
	enrollCount := 0
	chalCount := 0
	for _, e := range storage.enrollments {
		if e.UserID == "user1" && e.TenantID == "tenant1" {
			enrollCount++
		}
	}
	for _, c := range storage.challenges {
		if c.UserID == "user1" && c.TenantID == "tenant1" {
			chalCount++
		}
	}
	storage.mu.RUnlock()

	assert.Equal(t, 0, enrollCount)
	assert.Equal(t, 0, chalCount)
}

// TestMemoryMFAStorage_SecretKey tests the secretKey helper function.
func TestMemoryMFAStorage_SecretKey(t *testing.T) {
	storage := NewMemoryMFAStorage()

	tests := []struct {
		name     string
		userID   string
		tenantID string
		expected string
	}{
		{
			name:     "basic case",
			userID:   "user1",
			tenantID: "tenant1",
			expected: "tenant1:user1",
		},
		{
			name:     "empty userID",
			userID:   "",
			tenantID: "tenant1",
			expected: "tenant1:",
		},
		{
			name:     "empty tenantID",
			userID:   "user1",
			tenantID: "",
			expected: ":user1",
		},
		{
			name:     "both empty",
			userID:   "",
			tenantID: "",
			expected: ":",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := storage.secretKey(tt.userID, tt.tenantID)
			assert.Equal(t, tt.expected, key)
		})
	}
}

// TestMemoryMFAStorage_InterfaceCompliance verifies interface implementation.
func TestMemoryMFAStorage_InterfaceCompliance(t *testing.T) {
	var _ MFAStorage = (*MemoryMFAStorage)(nil)
}

// TestMemoryMFAStorage_MultiTenantIsolation tests data isolation between tenants.
func TestMemoryMFAStorage_MultiTenantIsolation(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	// Create secrets for different tenants
	secret1 := createTestSecret("user1", "tenant1")
	secret2 := createTestSecret("user1", "tenant2")

	require.NoError(t, storage.SaveSecret(ctx, secret1))
	require.NoError(t, storage.SaveSecret(ctx, secret2))

	// Verify tenant isolation
	retrieved1, err := storage.GetSecret(ctx, "user1", "tenant1")
	require.NoError(t, err)
	assert.Equal(t, "tenant1", retrieved1.TenantID)

	retrieved2, err := storage.GetSecret(ctx, "user1", "tenant2")
	require.NoError(t, err)
	assert.Equal(t, "tenant2", retrieved2.TenantID)

	// Delete one tenant's data
	err = storage.DeleteSecret(ctx, "user1", "tenant1")
	require.NoError(t, err)

	// Verify only that tenant's data is deleted
	_, err = storage.GetSecret(ctx, "user1", "tenant1")
	assert.ErrorIs(t, err, ErrMFANotEnrolled)

	_, err = storage.GetSecret(ctx, "user1", "tenant2")
	assert.NoError(t, err)
}

// TestMemoryMFAStorage_CleanupExpired_BoundaryConditions tests cleanup with boundary times.
func TestMemoryMFAStorage_CleanupExpired_BoundaryConditions(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	// Create enrollment that expires exactly now
	now := time.Now()
	enrollment := &MFAEnrollment{
		ID:        "boundary-enroll",
		UserID:    "user1",
		TenantID:  "tenant1",
		Secret:    "secret",
		ExpiresAt: now,
		Created:   now,
	}

	require.NoError(t, storage.SaveEnrollment(ctx, enrollment))

	// Sleep briefly to ensure time has passed
	time.Sleep(1 * time.Millisecond)

	// Run cleanup - should remove the item
	count, err := storage.CleanupExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	_, err = storage.GetEnrollment(ctx, "boundary-enroll")
	assert.ErrorIs(t, err, ErrMFAEnrollmentNotFound)
}
