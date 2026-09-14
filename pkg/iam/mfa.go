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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jeremyhahn/go-auth/pkg/otp"
	authsecret "github.com/jeremyhahn/go-auth/pkg/secret"
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

	// Generate the shared secret into protected memory. The key never has a
	// plaintext form on the heap, and it is the enclave — not the base32
	// string — that the authenticator below derives codes from.
	//
	// The base32 form is still rendered, because enrollment cannot happen
	// without it: the user has to scan or type it, and it has to survive in
	// the enrollment record until they confirm a code. That string cannot be
	// wiped; it is the one unprotected copy this call leaves behind, and it is
	// inherent to enrolling a shared secret rather than an artifact of how the
	// code is derived.
	protectedSecret, err := otp.GenerateProtectedSecret()
	if err != nil {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.enroll", "failed to generate secret", err)
	}
	defer protectedSecret.Destroy()

	secret, err := otp.EncodeSecret(protectedSecret)
	if err != nil {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.enroll", "failed to encode secret", err)
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
		SecretKey:   protectedSecret,
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

	qrCodeURI, err := auth.ProvisioningURI()
	if err != nil {
		return nil, NewIAMError(ErrMFAInvalid, "iam.mfa.enroll", "failed to build provisioning URI", err)
	}

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

	// The enrollment record holds the shared secret as a base32 string, which
	// no code here can wipe. Decode it into protected memory for the
	// verification itself so the key is decrypted only for the moment a code
	// is derived, instead of being handed to a dependency that decodes a fresh
	// plaintext copy on every call. See protectedSharedSecret for what this
	// does and does not close.
	enrollmentKey, err := protectedSharedSecret("iam.mfa.enroll.complete", enrollment.Secret)
	if err != nil {
		return err
	}
	defer enrollmentKey.Destroy()

	otpConfig := otp.Config{
		Type:      otp.TypeTOTP,
		SecretKey: enrollmentKey,
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

	// The verified secret above IS the enrollment. There is deliberately no
	// second write mirroring it onto the user record: GetMFAStatus and
	// IsMFARequired both answer from this store, so a copy on the identity
	// record was read by nothing and could only ever disagree with it.

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
		// ONLY A WRONG CODE IS AN AUTHENTICATION ATTEMPT. VerifyCode reports
		// two different things through one error: that the code the user typed
		// does not match, and that the enrollment cannot be used at all -- a
		// stored secret that is not decodable, an algorithm the policy refuses,
		// storage that will not answer. Collapsing the second into the first
		// costs three things: the operator sees a corrupt enrollment as a bad
		// login, the user loses an attempt they did not spend, and -- because
		// the backup-code fallback below would run first -- a valid backup code
		// returns SUCCESS while the primary secret stays quietly corrupt, which
		// is the state nobody ever discovers.
		//
		// A wrong code must still consume an attempt: that is the brute-force
		// defence. So this is a classification, not a blanket `return err`. The
		// So this names ONE condition, and deliberately not a general "anything
		// that is not a wrong code". The broad form would also swallow
		// ErrMFANotEnrolled and the algorithm-policy refusal, both of which
		// have established behaviour below that is not what this finding is
		// about; redefining those under cover of a security fix is the same
		// mistake in the other direction.
		//
		// A corrupt stored secret is the one case that must not reach the
		// backup-code fallback. Recovering through it would return success and
		// leave the corruption in place, unreported and undiscoverable -- the
		// hiding this exists to stop. The cost is explicit: a user whose stored
		// secret is corrupt cannot log in with a backup code either, and needs
		// an operator to repair or reset the enrollment. That is the intended
		// trade for a data-integrity fault, and it is why this is narrow: an
		// algorithm the policy now refuses is an administrative change, not
		// corruption, and backup codes remain its sanctioned recovery path.
		var malformed *MFASecretMalformedError
		if errors.As(err, &malformed) {
			return nil, err
		}

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
		// has to re-enroll, and do not consume one of their attempts. It sits
		// AFTER the backup-code fallback above, which is what lets a user whose
		// algorithm a policy change has disallowed still recover with a backup
		// code instead of being locked out.
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

	// The stored secret arrives from the barrier as a base32 string, which no
	// code here can wipe. Decode it into protected memory for the
	// verification itself so the key is decrypted only for the moment a code
	// is derived. See protectedSharedSecret for what this does and does not
	// close.
	verifyKey, err := protectedSharedSecret("iam.mfa.verify", secret.Secret)
	if err != nil {
		return err
	}
	defer verifyKey.Destroy()

	otpConfig := otp.Config{
		Type:      otp.TypeTOTP,
		SecretKey: verifyKey,
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

	// Delete secret.
	//
	// coded-sentinel-ok: the CLASS is the intent. Disabling MFA is idempotent,
	// and any not-found from the delete means there was nothing to delete.
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

	// Deleting the secret, challenges and backup codes above IS disabling MFA;
	// see CompleteEnrollment for why no copy is mirrored onto the user record.

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

// protectedSharedSecret decodes a stored base32 TOTP shared secret into
// protected memory, tagging a failure with the operation that asked for it.
//
// MFASecretMalformedError is returned when a stored shared secret cannot be a
// key at all, as opposed to a code failing to match. It is the enrollment that
// is broken, not the authentication attempt, and the two are kept apart so a
// corrupt stored value is never reported as a bad login.
type MFASecretMalformedError struct {
	// Reason describes what is wrong with the stored value. It never contains
	// the secret itself.
	Reason string

	// Cause is the underlying decode failure, when there is one. It is
	// preserved so callers can still match on the decoder's own sentinels.
	Cause error
}

// Error implements the error interface.
func (e *MFASecretMalformedError) Error() string {
	if e.Cause != nil {
		return "stored MFA shared secret is malformed: " + e.Reason + ": " + e.Cause.Error()
	}
	return "stored MFA shared secret is malformed: " + e.Reason
}

// Unwrap returns the underlying decode failure, so wrapping this around the
// decoder's error does not hide it from errors.Is.
func (e *MFASecretMalformedError) Unwrap() error { return e.Cause }

// It closes the last segment of the secret's path: the derivation itself. With
// the returned key in otp.Config.SecretKey the shared secret is sealed in a
// memguard enclave and decrypted only for the moment a code is derived,
// instead of being passed to github.com/pquerna/otp as a string that it
// decodes into a fresh plaintext key on every Generate and Authenticate.
//
// It does not make the whole path protected, and must not be read as if it
// did. base32Secret is a Go string that arrived from the MFA store — read as
// plaintext JSON bytes and unmarshalled into MFASecret.Secret or
// MFAEnrollment.Secret — and a string cannot be wiped. Both of those copies
// stay resident until the collector reclaims them, so this narrows an existing
// exposure rather than removing it. Closing them means the store handing back
// protected memory instead of a string, which is a change to the storage
// interface and to the KV read below it.
//
// The caller borrows the key: destroy it when the authenticator built from it
// is done.
func protectedSharedSecret(op, base32Secret string) (*authsecret.Secret, error) {
	// REFUSE AN INCOMPLETE TERMINAL GROUP BEFORE DECODING, because the decode
	// reports nothing. otp.NewProtectedSecret uses Go's unpadded base32
	// decoder, which accepts a final group of 1, 3 or 6 characters by
	// discarding the bits that do not complete a byte: base32 packs 5 bits per
	// character, so those groups hold 5, 15 or 30 bits and contribute no whole
	// byte. "<secret>A" therefore decodes to exactly the bytes of "<secret>",
	// with no error -- measured, byte identical -- so two different persisted
	// values would derive the same TOTP and both would authenticate.
	//
	// That is a corrupt stored value, not a recoverable encoding, so it is
	// refused rather than normalised: silently accepting the prefix would let
	// a secret nobody enrolled verify against a code for one that was. The
	// other remainders (2, 4, 5, 7) do carry complete bytes and are left alone.
	//
	// Normalisation matches otp.NewProtectedSecret's own trimBase32 -- trim
	// surrounding whitespace, then trailing '=' padding -- so this measures
	// the same string the decoder will. Case does not affect length. The
	// original value is still what gets passed on, so nothing changes for a
	// well-formed secret.
	switch len(strings.TrimRight(strings.TrimSpace(base32Secret), "=")) % 8 {
	case 1, 3, 6:
		return nil, NewIAMError(ErrMFAInvalid, op,
			"shared secret ends in an incomplete base32 group, so it is not a whole number of bytes",
			&MFASecretMalformedError{Reason: "incomplete terminal base32 group"})
	}

	// EVERY way a stored secret can fail to become a key is the same kind of
	// fault, so they all carry the same type. The length check above is only
	// the case the decoder does not report; an empty value, a character
	// outside the base32 alphabet and anything else NewProtectedSecret refuses
	// are equally "this enrollment is unusable", and VerifyChallenge has to be
	// able to tell all of them from a user typing the wrong code. Typing only
	// the length case would have left the commoner corruptions falling through
	// to the backup-code fallback -- the exact hole this type exists to close.
	key, err := otp.NewProtectedSecret(base32Secret)
	if err != nil {
		return nil, NewIAMError(ErrMFAInvalid, op, "failed to load shared secret into protected memory",
			&MFASecretMalformedError{Reason: "not decodable as base32", Cause: err})
	}
	return key, nil
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
