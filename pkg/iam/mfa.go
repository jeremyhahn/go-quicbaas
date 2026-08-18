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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jeremyhahn/go-auth/pkg/otp"
)

// MFAManager handles MFA operations.
type MFAManager interface {
	// Enrollment
	BeginEnrollment(ctx context.Context, userID, tenantID string, method MFAMethod) (*MFAEnrollResponse, error)
	CompleteEnrollment(ctx context.Context, enrollmentID, code string) error
	CancelEnrollment(ctx context.Context, enrollmentID string) error

	// Verification
	BeginChallenge(ctx context.Context, userID, tenantID, sessionToken string) (*MFAChallenge, error)
	VerifyChallenge(ctx context.Context, challengeID, code string) (*MFAVerifyResponse, error)
	VerifyCode(ctx context.Context, userID, tenantID, code string) error

	// Management
	DisableMFA(ctx context.Context, userID, tenantID string) error
	GetMFAStatus(ctx context.Context, userID, tenantID string) (*MFAStatus, error)
	IsMFARequired(ctx context.Context, userID, tenantID string) (bool, error)

	// Backup Codes
	GenerateBackupCodes(ctx context.Context, userID, tenantID string) ([]string, error)
	VerifyBackupCode(ctx context.Context, userID, tenantID, code string) error
	GetBackupCodeStatus(ctx context.Context, userID, tenantID string) (*BackupCodeStatus, error)
}

// DefaultMFAManager implements MFAManager.
type DefaultMFAManager struct {
	config      MFAConfig
	storage     MFAStorage
	userStorage UserStorage
}

// MFAManagerOption configures the MFA manager.
type MFAManagerOption func(*DefaultMFAManager)

// WithMFAConfig sets the MFA configuration.
func WithMFAConfig(config MFAConfig) MFAManagerOption {
	return func(m *DefaultMFAManager) {
		m.config = config
	}
}

// WithMFAStorage sets the MFA storage.
func WithMFAStorage(storage MFAStorage) MFAManagerOption {
	return func(m *DefaultMFAManager) {
		m.storage = storage
	}
}

// WithUserStorage sets the user storage for MFA operations.
func WithUserStorage(storage UserStorage) MFAManagerOption {
	return func(m *DefaultMFAManager) {
		m.userStorage = storage
	}
}

// NewMFAManager creates a new MFA manager.
func NewMFAManager(opts ...MFAManagerOption) (*DefaultMFAManager, error) {
	m := &DefaultMFAManager{
		config:  MFAConfig{},
		storage: NewMemoryMFAStorage(),
	}

	for _, opt := range opts {
		opt(m)
	}

	// Apply defaults
	m.config.ApplyDefaults()

	// Validate config
	if err := m.config.Validate(); err != nil {
		return nil, err
	}

	return m, nil
}

// BeginEnrollment starts MFA enrollment for a user.
func (m *DefaultMFAManager) BeginEnrollment(ctx context.Context, userID, tenantID string, method MFAMethod) (*MFAEnrollResponse, error) {
	if userID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.enroll", "user ID is required", nil)
	}
	if tenantID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.enroll", "tenant ID is required", nil)
	}
	if method != MFAMethodTOTP {
		return nil, NewIAMError(ErrMFAMethodNotAllowed, "iam.mfa.enroll", "only TOTP method is currently supported", nil)
	}

	// Check if already enrolled
	existing, err := m.storage.GetSecret(ctx, userID, tenantID)
	if err == nil && existing != nil && existing.Verified {
		return nil, ErrMFAAlreadyEnrolled
	}

	// Cancel any existing enrollment
	if err := m.storage.DeleteEnrollmentsByUser(ctx, userID, tenantID); err != nil {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.enroll", "failed to cancel existing enrollments", err)
	}

	// Generate secret
	secret, err := otp.GenerateSecret()
	if err != nil {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.enroll", "failed to generate secret", err)
	}

	// Get account name (email) from user storage if available
	accountName := userID
	if m.userStorage != nil {
		user, err := m.userStorage.GetUser(ctx, tenantID, userID)
		if err == nil && user != nil && user.Email != "" {
			accountName = user.Email
		}
	}

	// Create OTP authenticator to get provisioning URI
	algorithm, err := convertAlgorithm("iam.mfa.enroll", m.config.Algorithm)
	if err != nil {
		return nil, err
	}

	otpConfig := otp.Config{
		Type:        otp.TypeTOTP,
		Secret:      secret,
		Issuer:      m.config.Issuer,
		AccountName: accountName,
		Digits:      uint(m.config.Digits), // #nosec G115 -- Digits is bounded config value (6-8)
		Period:      uint(m.config.Period), // #nosec G115 -- Period is bounded config value (typically 30)
		Algorithm:   algorithm,
		Skew:        uint(m.config.Skew), // #nosec G115 -- Skew is bounded config value (typically 1)
	}

	auth, err := otp.NewAuthenticator(otpConfig)
	if err != nil {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.enroll", "failed to create authenticator", err)
	}

	qrCodeURI := auth.GetProvisioningURI()

	// Create enrollment. The provisioning parameters are recorded alongside
	// the secret: they are baked into the URI the authenticator app scanned,
	// so completion must verify against them and not against a configuration
	// that may have changed in the meantime.
	enrollment := &MFAEnrollment{
		ID:          uuid.New().String(),
		UserID:      userID,
		TenantID:    tenantID,
		Method:      method,
		Secret:      secret,
		QRCodeURI:   qrCodeURI,
		Algorithm:   m.config.Algorithm,
		Digits:      m.config.Digits,
		Period:      m.config.Period,
		ExpiresAt:   time.Now().Add(10 * time.Minute), // 10 minute enrollment window
		Attempts:    0,
		MaxAttempts: 5,
		Created:     time.Now(),
	}

	if err := m.storage.SaveEnrollment(ctx, enrollment); err != nil {
		return nil, err
	}

	return &MFAEnrollResponse{
		EnrollmentID: enrollment.ID,
		QRCodeURI:    qrCodeURI,
		Secret:       secret,
		ExpiresAt:    enrollment.ExpiresAt,
	}, nil
}

// CompleteEnrollment verifies and completes MFA enrollment.
func (m *DefaultMFAManager) CompleteEnrollment(ctx context.Context, enrollmentID, code string) error {
	if enrollmentID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.enroll.complete", "enrollment ID is required", nil)
	}
	if code == "" {
		return NewIAMError(ErrMFACodeInvalid, "iam.mfa.enroll.complete", "code is required", nil)
	}

	// Get enrollment
	enrollment, err := m.storage.GetEnrollment(ctx, enrollmentID)
	if err != nil {
		return err
	}

	// Check expiration
	if enrollment.IsExpired() {
		if delErr := m.storage.DeleteEnrollment(ctx, enrollmentID); delErr != nil {
			return NewIAMError(ErrMFAInvalid, "iam.mfa.enroll.complete", "failed to delete expired enrollment", delErr)
		}
		return ErrMFAEnrollmentExpired
	}

	// Check attempts
	if enrollment.Attempts >= enrollment.MaxAttempts {
		if delErr := m.storage.DeleteEnrollment(ctx, enrollmentID); delErr != nil {
			return NewIAMError(ErrMFAInvalid, "iam.mfa.enroll.complete", "failed to delete enrollment after max attempts", delErr)
		}
		return ErrMFAMaxAttemptsExceeded
	}

	// Verify code against the parameters this enrollment was provisioned with.
	provisioned := m.provisioningParams(enrollment)
	algorithm, err := convertAlgorithm("iam.mfa.enroll.complete", provisioned.Algorithm)
	if err != nil {
		return err
	}

	otpConfig := otp.Config{
		Type:      otp.TypeTOTP,
		Secret:    enrollment.Secret,
		Digits:    uint(provisioned.Digits), // #nosec G115 -- Digits is bounded config value (6-8)
		Period:    uint(provisioned.Period), // #nosec G115 -- Period is bounded config value (typically 30)
		Algorithm: algorithm,
		Skew:      uint(m.config.Skew), // #nosec G115 -- Skew is bounded config value (typically 1)
	}

	auth, err := otp.NewAuthenticator(otpConfig)
	if err != nil {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.enroll.complete", "failed to create authenticator", err)
	}

	if err := auth.Authenticate(ctx, code); err != nil {
		// Increment attempt counter
		enrollment.Attempts++
		if saveErr := m.storage.SaveEnrollment(ctx, enrollment); saveErr != nil {
			return NewIAMError(ErrMFAInvalid, "iam.mfa.enroll.complete", "failed to save enrollment attempt counter", saveErr)
		}
		return ErrMFACodeInvalid
	}

	// Get account name for secret storage
	accountName := enrollment.UserID
	if m.userStorage != nil {
		user, err := m.userStorage.GetUser(ctx, enrollment.TenantID, enrollment.UserID)
		if err == nil && user != nil && user.Email != "" {
			accountName = user.Email
		}
	}

	// Save verified secret
	secret := &MFASecret{
		ID:          uuid.New().String(),
		UserID:      enrollment.UserID,
		TenantID:    enrollment.TenantID,
		Secret:      enrollment.Secret,
		Algorithm:   provisioned.Algorithm,
		Digits:      provisioned.Digits,
		Period:      provisioned.Period,
		Issuer:      m.config.Issuer,
		AccountName: accountName,
		Verified:    true,
		Created:     time.Now(),
	}

	if err := m.storage.SaveSecret(ctx, secret); err != nil {
		return err
	}

	// Update user MFA status
	if m.userStorage != nil {
		user, err := m.userStorage.GetUser(ctx, enrollment.TenantID, enrollment.UserID)
		if err == nil && user != nil {
			now := time.Now()
			user.MFAEnabled = true
			user.MFAMethod = MFAMethodTOTP
			user.MFAEnrolledAt = &now
			user.Updated = now
			if updateErr := m.userStorage.UpdateUser(ctx, user); updateErr != nil {
				return NewIAMError(ErrMFAInvalid, "iam.mfa.enroll.complete", "failed to update user MFA status", updateErr)
			}
		}
	}

	// Delete enrollment
	if delErr := m.storage.DeleteEnrollment(ctx, enrollmentID); delErr != nil {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.enroll.complete", "failed to delete completed enrollment", delErr)
	}

	// Generate backup codes
	if _, err := m.GenerateBackupCodes(ctx, enrollment.UserID, enrollment.TenantID); err != nil {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.enroll.complete", "failed to generate backup codes", err)
	}

	return nil
}

// CancelEnrollment cancels a pending MFA enrollment.
func (m *DefaultMFAManager) CancelEnrollment(ctx context.Context, enrollmentID string) error {
	if enrollmentID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.enroll.cancel", "enrollment ID is required", nil)
	}

	return m.storage.DeleteEnrollment(ctx, enrollmentID)
}

// BeginChallenge starts an MFA challenge for authentication.
func (m *DefaultMFAManager) BeginChallenge(ctx context.Context, userID, tenantID, sessionToken string) (*MFAChallenge, error) {
	if userID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.challenge", "user ID is required", nil)
	}
	if tenantID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.challenge", "tenant ID is required", nil)
	}
	if sessionToken == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.challenge", "session token is required", nil)
	}

	// Check if MFA is enrolled
	secret, err := m.storage.GetSecret(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}
	if !secret.Verified {
		return nil, ErrMFANotEnrolled
	}

	// Delete any existing challenges
	if err := m.storage.DeleteChallengesByUser(ctx, userID, tenantID); err != nil {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.challenge", "failed to delete existing challenges", err)
	}

	// Create challenge
	challenge := &MFAChallenge{
		ID:           uuid.New().String(),
		UserID:       userID,
		TenantID:     tenantID,
		SessionToken: sessionToken,
		Method:       MFAMethodTOTP,
		ExpiresAt:    time.Now().Add(5 * time.Minute), // 5 minute challenge window
		Attempts:     0,
		MaxAttempts:  3,
		Created:      time.Now(),
	}

	if err := m.storage.SaveChallenge(ctx, challenge); err != nil {
		return nil, err
	}

	return challenge, nil
}

// VerifyChallenge verifies an MFA challenge.
func (m *DefaultMFAManager) VerifyChallenge(ctx context.Context, challengeID, code string) (*MFAVerifyResponse, error) {
	if challengeID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "challenge ID is required", nil)
	}
	if code == "" {
		return nil, NewIAMError(ErrMFACodeInvalid, "iam.mfa.verify", "code is required", nil)
	}

	// Get challenge
	challenge, err := m.storage.GetChallenge(ctx, challengeID)
	if err != nil {
		return nil, err
	}

	// Check expiration
	if challenge.IsExpired() {
		if delErr := m.storage.DeleteChallenge(ctx, challengeID); delErr != nil {
			return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "failed to delete expired challenge", delErr)
		}
		return nil, ErrMFAChallengeExpired
	}

	// Check attempts
	if challenge.Attempts >= challenge.MaxAttempts {
		if delErr := m.storage.DeleteChallenge(ctx, challengeID); delErr != nil {
			return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "failed to delete challenge after max attempts", delErr)
		}
		return nil, ErrMFAMaxAttemptsExceeded
	}

	// Verify code
	if err := m.VerifyCode(ctx, challenge.UserID, challenge.TenantID, code); err != nil {
		// Check if it's a backup code
		if backupErr := m.VerifyBackupCode(ctx, challenge.UserID, challenge.TenantID, code); backupErr == nil {
			// Backup code was valid
			if delErr := m.storage.DeleteChallenge(ctx, challengeID); delErr != nil {
				return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "failed to delete challenge after backup code verification", delErr)
			}

			// Get backup code status
			backupStatus, statusErr := m.GetBackupCodeStatus(ctx, challenge.UserID, challenge.TenantID)
			remaining := 0
			if statusErr == nil && backupStatus != nil {
				remaining = backupStatus.Remaining
			}

			return &MFAVerifyResponse{
				Success:              true,
				Token:                challenge.SessionToken,
				BackupCodesRemaining: remaining,
			}, nil
		}

		// A refusal to use the enrollment's algorithm is not a failed
		// authentication attempt: surface it so the caller sees that the user
		// has to re-enroll, and do not consume one of their attempts.
		if isMFAAlgorithmPolicyError(err) {
			return nil, err
		}

		// Increment attempt counter
		challenge.Attempts++
		if saveErr := m.storage.SaveChallenge(ctx, challenge); saveErr != nil {
			return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "failed to save challenge attempt counter", saveErr)
		}

		return &MFAVerifyResponse{
			Success:           false,
			RemainingAttempts: challenge.MaxAttempts - challenge.Attempts,
		}, ErrMFACodeInvalid
	}

	// Delete challenge
	if delErr := m.storage.DeleteChallenge(ctx, challengeID); delErr != nil {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "failed to delete verified challenge", delErr)
	}

	// Update secret last used time
	secret, err := m.storage.GetSecret(ctx, challenge.UserID, challenge.TenantID)
	if err == nil && secret != nil {
		now := time.Now()
		secret.LastUsed = &now
		if saveErr := m.storage.SaveSecret(ctx, secret); saveErr != nil {
			return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "failed to update secret last used time", saveErr)
		}
	}

	return &MFAVerifyResponse{
		Success: true,
		Token:   challenge.SessionToken,
	}, nil
}

// VerifyCode verifies a TOTP code for a user.
func (m *DefaultMFAManager) VerifyCode(ctx context.Context, userID, tenantID, code string) error {
	if userID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "user ID is required", nil)
	}
	if tenantID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "tenant ID is required", nil)
	}
	if code == "" {
		return NewIAMError(ErrMFACodeInvalid, "iam.mfa.verify", "code is required", nil)
	}

	// Get secret
	secret, err := m.storage.GetSecret(ctx, userID, tenantID)
	if err != nil {
		return err
	}
	if !secret.Verified {
		return ErrMFANotEnrolled
	}

	// Create authenticator. The algorithm is the one the secret was
	// provisioned with, not the current config: an existing enrollment keeps
	// verifying after the configured default changes.
	algorithm, err := convertAlgorithm("iam.mfa.verify", secret.Algorithm)
	if err != nil {
		return err
	}

	otpConfig := otp.Config{
		Type:      otp.TypeTOTP,
		Secret:    secret.Secret,
		Digits:    uint(secret.Digits), // #nosec G115 -- Digits is bounded config value (6-8)
		Period:    uint(secret.Period), // #nosec G115 -- Period is bounded config value (typically 30)
		Algorithm: algorithm,
		Skew:      uint(m.config.Skew), // #nosec G115 -- Skew is bounded config value (typically 1)
	}

	auth, err := otp.NewAuthenticator(otpConfig)
	if err != nil {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "failed to create authenticator", err)
	}

	if err := auth.Authenticate(ctx, code); err != nil {
		return ErrMFACodeInvalid
	}

	// Update last used time
	now := time.Now()
	secret.LastUsed = &now
	if saveErr := m.storage.SaveSecret(ctx, secret); saveErr != nil {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.verify", "failed to update secret last used time", saveErr)
	}

	return nil
}

// DisableMFA disables MFA for a user.
func (m *DefaultMFAManager) DisableMFA(ctx context.Context, userID, tenantID string) error {
	if userID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.disable", "user ID is required", nil)
	}
	if tenantID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.disable", "tenant ID is required", nil)
	}

	// Delete secret
	if err := m.storage.DeleteSecret(ctx, userID, tenantID); err != nil && !errors.Is(err, ErrMFANotEnrolled) {
		return err
	}

	// Delete any pending enrollments
	if err := m.storage.DeleteEnrollmentsByUser(ctx, userID, tenantID); err != nil {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.disable", "failed to delete enrollments", err)
	}

	// Delete any pending challenges
	if err := m.storage.DeleteChallengesByUser(ctx, userID, tenantID); err != nil {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.disable", "failed to delete challenges", err)
	}

	// Delete backup codes
	if err := m.storage.DeleteBackupCodes(ctx, userID, tenantID); err != nil {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.disable", "failed to delete backup codes", err)
	}

	// Update user MFA status
	if m.userStorage != nil {
		user, err := m.userStorage.GetUser(ctx, tenantID, userID)
		if err == nil && user != nil {
			user.MFAEnabled = false
			user.MFAMethod = ""
			user.MFAEnrolledAt = nil
			user.Updated = time.Now()
			if updateErr := m.userStorage.UpdateUser(ctx, user); updateErr != nil {
				return NewIAMError(ErrMFAInvalid, "iam.mfa.disable", "failed to update user MFA status", updateErr)
			}
		}
	}

	return nil
}

// GetMFAStatus returns the MFA status for a user.
func (m *DefaultMFAManager) GetMFAStatus(ctx context.Context, userID, tenantID string) (*MFAStatus, error) {
	if userID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.status", "user ID is required", nil)
	}
	if tenantID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.status", "tenant ID is required", nil)
	}

	status := &MFAStatus{
		Enabled: false,
	}

	// Get secret
	secret, err := m.storage.GetSecret(ctx, userID, tenantID)
	if err == nil && secret != nil && secret.Verified {
		status.Enabled = true
		status.Method = MFAMethodTOTP
		status.EnrolledAt = &secret.Created
		status.LastVerified = secret.LastUsed

		// Get backup code status
		backupCodes, err := m.storage.GetBackupCodes(ctx, userID, tenantID)
		if err == nil && backupCodes != nil {
			status.BackupCodesTotal = len(backupCodes.Codes)
			status.BackupCodesRemaining = backupCodes.RemainingCodes()
		}
	} else {
		// Check for a pending enrollment (started but not yet verified).
		hasEnrollment, enrollErr := m.storage.HasActiveEnrollment(ctx, userID, tenantID)
		if enrollErr == nil && hasEnrollment {
			status.Method = MFAMethodTOTP
			now := time.Now()
			status.EnrolledAt = &now
		}
	}

	return status, nil
}

// IsMFARequired checks if MFA is required for a user.
func (m *DefaultMFAManager) IsMFARequired(ctx context.Context, userID, tenantID string) (bool, error) {
	// Check if MFA is enrolled
	secret, err := m.storage.GetSecret(ctx, userID, tenantID)
	if err == nil && secret != nil && secret.Verified {
		return true, nil
	}

	return false, nil
}

// GenerateBackupCodes generates new backup codes for a user.
func (m *DefaultMFAManager) GenerateBackupCodes(ctx context.Context, userID, tenantID string) ([]string, error) {
	if userID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "user ID is required", nil)
	}
	if tenantID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "tenant ID is required", nil)
	}

	// Check if MFA is enrolled
	secret, err := m.storage.GetSecret(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}
	if !secret.Verified {
		return nil, ErrMFANotEnrolled
	}

	// Generate backup codes
	numCodes := m.config.BackupCodes
	if numCodes == 0 {
		numCodes = 10
	}

	codes := make([]string, numCodes)
	hashedCodes := make([]BackupCode, numCodes)

	for i := 0; i < numCodes; i++ {
		// Generate 8-character alphanumeric code
		code, err := generateBackupCode()
		if err != nil {
			return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "failed to generate backup code", err)
		}
		codes[i] = code

		// Hash the code for storage
		hash := sha256.Sum256([]byte(code))
		hashedCodes[i] = BackupCode{
			Code: hex.EncodeToString(hash[:]),
		}
	}

	// Check if this is a regeneration
	var regeneratedAt *time.Time
	existing, existErr := m.storage.GetBackupCodes(ctx, userID, tenantID)
	if existErr == nil && existing != nil {
		now := time.Now()
		regeneratedAt = &now
	}

	// Save backup codes
	backupSet := &BackupCodeSet{
		ID:          uuid.New().String(),
		UserID:      userID,
		TenantID:    tenantID,
		Codes:       hashedCodes,
		Created:     time.Now(),
		Regenerated: regeneratedAt,
	}

	if err := m.storage.SaveBackupCodes(ctx, backupSet); err != nil {
		return nil, err
	}

	return codes, nil
}

// VerifyBackupCode verifies and consumes a backup code.
func (m *DefaultMFAManager) VerifyBackupCode(ctx context.Context, userID, tenantID, code string) error {
	if userID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "user ID is required", nil)
	}
	if tenantID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "tenant ID is required", nil)
	}
	if code == "" {
		return NewIAMError(ErrMFABackupCodeInvalid, "iam.mfa.backup", "code is required", nil)
	}

	// Get backup codes
	backupSet, err := m.storage.GetBackupCodes(ctx, userID, tenantID)
	if err != nil {
		return err
	}

	// Check if any codes remaining
	if backupSet.RemainingCodes() == 0 {
		return ErrMFABackupCodesExhausted
	}

	// Hash the provided code
	hash := sha256.Sum256([]byte(code))
	codeHash := hex.EncodeToString(hash[:])

	// Try to use the code
	if !backupSet.UseCode(codeHash) {
		return ErrMFABackupCodeInvalid
	}

	// Save updated backup codes
	if err := m.storage.SaveBackupCodes(ctx, backupSet); err != nil {
		return err
	}

	return nil
}

// GetBackupCodeStatus returns the backup code status for a user.
func (m *DefaultMFAManager) GetBackupCodeStatus(ctx context.Context, userID, tenantID string) (*BackupCodeStatus, error) {
	if userID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "user ID is required", nil)
	}
	if tenantID == "" {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.backup", "tenant ID is required", nil)
	}

	backupSet, err := m.storage.GetBackupCodes(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	remaining := backupSet.RemainingCodes()
	return &BackupCodeStatus{
		Total:         len(backupSet.Codes),
		Remaining:     remaining,
		Used:          len(backupSet.Codes) - remaining,
		GeneratedAt:   backupSet.Created,
		RegeneratedAt: backupSet.Regenerated,
	}, nil
}

// generateBackupCode generates a random 8-character alphanumeric backup code.
func generateBackupCode() (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // Exclude confusing characters: 0, O, I, 1
	const codeLen = 8

	bytes := make([]byte, codeLen)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	code := make([]byte, codeLen)
	for i := 0; i < codeLen; i++ {
		code[i] = charset[int(bytes[i])%len(charset)]
	}

	// Format as XXXX-XXXX for readability
	return fmt.Sprintf("%s-%s", string(code[:4]), string(code[4:])), nil
}

// Ensure DefaultMFAManager implements MFAManager.
var _ MFAManager = (*DefaultMFAManager)(nil)
