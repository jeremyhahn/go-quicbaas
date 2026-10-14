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
	"strconv"
	"sync"
	"time"
)

// MFAStorage defines the storage interface for MFA data.
type MFAStorage interface {
	// Secret operations
	GetSecret(ctx context.Context, userID uint64, tenantID string) (*MFASecret, error)
	SaveSecret(ctx context.Context, secret *MFASecret) error
	DeleteSecret(ctx context.Context, userID uint64, tenantID string) error

	// Enrollment operations
	GetEnrollment(ctx context.Context, enrollmentID string) (*MFAEnrollment, error)
	SaveEnrollment(ctx context.Context, enrollment *MFAEnrollment) error
	DeleteEnrollment(ctx context.Context, enrollmentID string) error
	DeleteEnrollmentsByUser(ctx context.Context, userID uint64, tenantID string) error

	// HasActiveEnrollment checks whether the user has a non-expired enrollment.
	HasActiveEnrollment(ctx context.Context, userID uint64, tenantID string) (bool, error)

	// Challenge operations
	GetChallenge(ctx context.Context, challengeID string) (*MFAChallenge, error)
	SaveChallenge(ctx context.Context, challenge *MFAChallenge) error
	DeleteChallenge(ctx context.Context, challengeID string) error
	DeleteChallengesByUser(ctx context.Context, userID uint64, tenantID string) error

	// Backup code operations
	GetBackupCodes(ctx context.Context, userID uint64, tenantID string) (*BackupCodeSet, error)
	SaveBackupCodes(ctx context.Context, codes *BackupCodeSet) error
	DeleteBackupCodes(ctx context.Context, userID uint64, tenantID string) error

	// Cleanup
	CleanupExpired(ctx context.Context) (int, error)
}

// MemoryMFAStorage implements MFAStorage using in-memory storage.
type MemoryMFAStorage struct {
	mu          sync.RWMutex
	secrets     map[string]*MFASecret     // key: tenantID:userID
	enrollments map[string]*MFAEnrollment // key: enrollmentID
	challenges  map[string]*MFAChallenge  // key: challengeID
	backupCodes map[string]*BackupCodeSet // key: tenantID:userID
}

// NewMemoryMFAStorage creates a new in-memory MFA storage.
func NewMemoryMFAStorage() *MemoryMFAStorage {
	return &MemoryMFAStorage{
		secrets:     make(map[string]*MFASecret),
		enrollments: make(map[string]*MFAEnrollment),
		challenges:  make(map[string]*MFAChallenge),
		backupCodes: make(map[string]*BackupCodeSet),
	}
}

func (m *MemoryMFAStorage) secretKey(userID uint64, tenantID string) string {
	return tenantID + ":" + strconv.FormatUint(userID, 10)
}

// GetSecret retrieves an MFA secret for a user.
func (m *MemoryMFAStorage) GetSecret(ctx context.Context, userID uint64, tenantID string) (*MFASecret, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	secret, ok := m.secrets[m.secretKey(userID, tenantID)]
	if !ok {
		return nil, ErrMFANotEnrolled
	}

	// Return a copy to prevent mutation
	secretCopy := *secret
	return &secretCopy, nil
}

// SaveSecret stores an MFA secret.
func (m *MemoryMFAStorage) SaveSecret(ctx context.Context, secret *MFASecret) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := secret.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a copy
	secretCopy := *secret
	m.secrets[m.secretKey(secret.UserID, secret.TenantID)] = &secretCopy
	return nil
}

// DeleteSecret removes an MFA secret.
func (m *MemoryMFAStorage) DeleteSecret(ctx context.Context, userID uint64, tenantID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.secrets, m.secretKey(userID, tenantID))
	return nil
}

// GetEnrollment retrieves an MFA enrollment.
func (m *MemoryMFAStorage) GetEnrollment(ctx context.Context, enrollmentID string) (*MFAEnrollment, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	enrollment, ok := m.enrollments[enrollmentID]
	if !ok {
		return nil, ErrMFAEnrollmentNotFound
	}

	// Return a copy
	enrollmentCopy := *enrollment
	return &enrollmentCopy, nil
}

// SaveEnrollment stores an MFA enrollment.
func (m *MemoryMFAStorage) SaveEnrollment(ctx context.Context, enrollment *MFAEnrollment) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := enrollment.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a copy
	enrollmentCopy := *enrollment
	m.enrollments[enrollment.ID] = &enrollmentCopy
	return nil
}

// DeleteEnrollment removes an MFA enrollment.
func (m *MemoryMFAStorage) DeleteEnrollment(ctx context.Context, enrollmentID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.enrollments, enrollmentID)
	return nil
}

// DeleteEnrollmentsByUser removes all enrollments for a user.
func (m *MemoryMFAStorage) DeleteEnrollmentsByUser(ctx context.Context, userID uint64, tenantID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, enrollment := range m.enrollments {
		if enrollment.UserID == userID && enrollment.TenantID == tenantID {
			delete(m.enrollments, id)
		}
	}
	return nil
}

// HasActiveEnrollment checks whether the user has a non-expired enrollment.
func (m *MemoryMFAStorage) HasActiveEnrollment(ctx context.Context, userID uint64, tenantID string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	for _, enrollment := range m.enrollments {
		if enrollment.UserID == userID && enrollment.TenantID == tenantID && !enrollment.IsExpired() && now.Before(enrollment.ExpiresAt) {
			return true, nil
		}
	}
	return false, nil
}

// GetChallenge retrieves an MFA challenge.
func (m *MemoryMFAStorage) GetChallenge(ctx context.Context, challengeID string) (*MFAChallenge, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	challenge, ok := m.challenges[challengeID]
	if !ok {
		return nil, ErrMFAChallengeNotFound
	}

	// Return a copy
	challengeCopy := *challenge
	return &challengeCopy, nil
}

// SaveChallenge stores an MFA challenge.
func (m *MemoryMFAStorage) SaveChallenge(ctx context.Context, challenge *MFAChallenge) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := challenge.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a copy
	challengeCopy := *challenge
	m.challenges[challenge.ID] = &challengeCopy
	return nil
}

// DeleteChallenge removes an MFA challenge.
func (m *MemoryMFAStorage) DeleteChallenge(ctx context.Context, challengeID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.challenges, challengeID)
	return nil
}

// DeleteChallengesByUser removes all challenges for a user.
func (m *MemoryMFAStorage) DeleteChallengesByUser(ctx context.Context, userID uint64, tenantID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, challenge := range m.challenges {
		if challenge.UserID == userID && challenge.TenantID == tenantID {
			delete(m.challenges, id)
		}
	}
	return nil
}

// GetBackupCodes retrieves backup codes for a user.
func (m *MemoryMFAStorage) GetBackupCodes(ctx context.Context, userID uint64, tenantID string) (*BackupCodeSet, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	codes, ok := m.backupCodes[m.secretKey(userID, tenantID)]
	if !ok {
		return nil, ErrMFANotEnrolled
	}

	// Return a deep copy
	codesCopy := BackupCodeSet{
		ID:          codes.ID,
		UserID:      codes.UserID,
		TenantID:    codes.TenantID,
		Codes:       make([]BackupCode, len(codes.Codes)),
		Created:     codes.Created,
		Regenerated: codes.Regenerated,
	}
	copy(codesCopy.Codes, codes.Codes)
	return &codesCopy, nil
}

// SaveBackupCodes stores backup codes.
func (m *MemoryMFAStorage) SaveBackupCodes(ctx context.Context, codes *BackupCodeSet) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if codes.ID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "backup code set ID is required", nil)
	}
	if codes.UserID == 0 {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "user ID is required", nil)
	}
	if codes.TenantID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "tenant ID is required", nil)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a deep copy
	codesCopy := BackupCodeSet{
		ID:          codes.ID,
		UserID:      codes.UserID,
		TenantID:    codes.TenantID,
		Codes:       make([]BackupCode, len(codes.Codes)),
		Created:     codes.Created,
		Regenerated: codes.Regenerated,
	}
	copy(codesCopy.Codes, codes.Codes)
	m.backupCodes[m.secretKey(codes.UserID, codes.TenantID)] = &codesCopy
	return nil
}

// DeleteBackupCodes removes backup codes for a user.
func (m *MemoryMFAStorage) DeleteBackupCodes(ctx context.Context, userID uint64, tenantID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.backupCodes, m.secretKey(userID, tenantID))
	return nil
}

// CleanupExpired removes expired enrollments and challenges.
func (m *MemoryMFAStorage) CleanupExpired(ctx context.Context) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	count := 0

	// Clean up expired enrollments
	for id, enrollment := range m.enrollments {
		if enrollment.IsExpired() || now.After(enrollment.ExpiresAt) {
			delete(m.enrollments, id)
			count++
		}
	}

	// Clean up expired challenges
	for id, challenge := range m.challenges {
		if challenge.IsExpired() || now.After(challenge.ExpiresAt) {
			delete(m.challenges, id)
			count++
		}
	}

	return count, nil
}

// Ensure MemoryMFAStorage implements MFAStorage.
var _ MFAStorage = (*MemoryMFAStorage)(nil)
