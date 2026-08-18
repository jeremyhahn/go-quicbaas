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
	"crypto/subtle"
	"time"
)

// MFAMethod represents the type of MFA being used.
type MFAMethod string

const (
	// MFAMethodTOTP represents Time-based One-Time Password (RFC 6238).
	MFAMethodTOTP MFAMethod = "totp"

	// MFAMethodWebAuthn represents WebAuthn/FIDO2 passkeys.
	MFAMethodWebAuthn MFAMethod = "webauthn"
)

// MFAAlgorithm represents the hash algorithm used for TOTP.
type MFAAlgorithm string

const (
	// MFAAlgorithmSHA1 uses SHA1 hash algorithm.
	MFAAlgorithmSHA1 MFAAlgorithm = "SHA1"

	// MFAAlgorithmSHA256 uses SHA256 hash algorithm.
	MFAAlgorithmSHA256 MFAAlgorithm = "SHA256"

	// MFAAlgorithmSHA512 uses SHA512 hash algorithm.
	MFAAlgorithmSHA512 MFAAlgorithm = "SHA512"
)

// MFAStatus represents the MFA enrollment status for a user.
type MFAStatus struct {
	// Enabled indicates whether MFA is enabled for the user
	Enabled bool `json:"enabled"`

	// Method is the MFA method being used
	Method MFAMethod `json:"method,omitempty"`

	// EnrolledAt is when MFA was enrolled
	EnrolledAt *time.Time `json:"enrolled_at,omitempty"`

	// LastVerified is the last successful MFA verification
	LastVerified *time.Time `json:"last_verified,omitempty"`

	// BackupCodesRemaining is the number of unused backup codes
	BackupCodesRemaining int `json:"backup_codes_remaining,omitempty"`

	// BackupCodesTotal is the total backup codes generated
	BackupCodesTotal int `json:"backup_codes_total,omitempty"`
}

// MFASecret stores encrypted TOTP secrets per user.
type MFASecret struct {
	// ID is the unique secret identifier
	ID string `json:"id"`

	// UserID is the owning user
	UserID string `json:"user_id"`

	// TenantID is the tenant
	TenantID string `json:"tenant_id"`

	// Secret is the base32-encoded TOTP secret (encrypted at rest)
	Secret string `json:"secret"`

	// Algorithm is the hash algorithm (SHA1, SHA256, SHA512)
	Algorithm MFAAlgorithm `json:"algorithm"`

	// Digits is the number of digits in the OTP code (6, 7, or 8)
	Digits int `json:"digits"`

	// Period is the time step in seconds (default: 30)
	Period int `json:"period"`

	// Issuer is the name shown in authenticator apps
	Issuer string `json:"issuer"`

	// AccountName is the account identifier shown in authenticator apps
	AccountName string `json:"account_name"`

	// Verified indicates whether enrollment is complete
	Verified bool `json:"verified"`

	// Created is when the secret was created
	Created time.Time `json:"created"`

	// LastUsed is when the secret was last used for authentication
	LastUsed *time.Time `json:"last_used,omitempty"`
}

// Validate validates an MFASecret for correctness.
func (s *MFASecret) Validate() error {
	if s.ID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_secret.validate", "secret ID is required", nil)
	}
	if s.UserID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_secret.validate", "user ID is required", nil)
	}
	if s.TenantID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_secret.validate", "tenant ID is required", nil)
	}
	if s.Secret == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_secret.validate", "secret is required", nil)
	}
	if s.Digits < 6 || s.Digits > 8 {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_secret.validate", "digits must be 6, 7, or 8", nil)
	}
	if s.Period < 15 || s.Period > 120 {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_secret.validate", "period must be between 15 and 120 seconds", nil)
	}
	return nil
}

// MFAEnrollment tracks pending MFA enrollments.
type MFAEnrollment struct {
	// ID is the unique enrollment identifier
	ID string `json:"id"`

	// UserID is the enrolling user
	UserID string `json:"user_id"`

	// TenantID is the tenant
	TenantID string `json:"tenant_id"`

	// Method is the MFA method being enrolled
	Method MFAMethod `json:"method"`

	// Secret is the base32-encoded TOTP secret (plaintext during enrollment)
	Secret string `json:"secret"`

	// QRCodeURI is the otpauth:// URI for QR code generation
	QRCodeURI string `json:"qr_code_uri"`

	// ExpiresAt is when the enrollment expires
	ExpiresAt time.Time `json:"expires_at"`

	// Attempts is the number of failed verification attempts
	Attempts int `json:"attempts"`

	// MaxAttempts is the maximum allowed verification attempts
	MaxAttempts int `json:"max_attempts"`

	// Created is when the enrollment was started
	Created time.Time `json:"created"`
}

// IsExpired returns true if the enrollment has expired.
func (e *MFAEnrollment) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// Validate validates an MFAEnrollment for correctness.
func (e *MFAEnrollment) Validate() error {
	if e.ID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_enrollment.validate", "enrollment ID is required", nil)
	}
	if e.UserID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_enrollment.validate", "user ID is required", nil)
	}
	if e.TenantID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_enrollment.validate", "tenant ID is required", nil)
	}
	if e.Secret == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_enrollment.validate", "secret is required", nil)
	}
	return nil
}

// MFAChallenge tracks pending MFA verifications during authentication.
type MFAChallenge struct {
	// ID is the unique challenge identifier
	ID string `json:"id"`

	// UserID is the authenticating user
	UserID string `json:"user_id"`

	// TenantID is the tenant
	TenantID string `json:"tenant_id"`

	// SessionToken is the partial authentication token
	SessionToken string `json:"session_token"`

	// Method is the MFA method being used
	Method MFAMethod `json:"method"`

	// ExpiresAt is when the challenge expires
	ExpiresAt time.Time `json:"expires_at"`

	// Attempts is the number of failed verification attempts
	Attempts int `json:"attempts"`

	// MaxAttempts is the maximum allowed verification attempts
	MaxAttempts int `json:"max_attempts"`

	// Created is when the challenge was created
	Created time.Time `json:"created"`

	// IPAddress is the client IP address
	IPAddress string `json:"ip_address,omitempty"`

	// UserAgent is the client user agent
	UserAgent string `json:"user_agent,omitempty"`
}

// IsExpired returns true if the challenge has expired.
func (c *MFAChallenge) IsExpired() bool {
	return time.Now().After(c.ExpiresAt)
}

// Validate validates an MFAChallenge for correctness.
func (c *MFAChallenge) Validate() error {
	if c.ID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_challenge.validate", "challenge ID is required", nil)
	}
	if c.UserID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_challenge.validate", "user ID is required", nil)
	}
	if c.TenantID == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_challenge.validate", "tenant ID is required", nil)
	}
	if c.SessionToken == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_challenge.validate", "session token is required", nil)
	}
	return nil
}

// BackupCode represents a one-time recovery code.
type BackupCode struct {
	// Code is the backup code (hashed for storage)
	Code string `json:"code"`

	// UsedAt is when the code was used (nil if unused)
	UsedAt *time.Time `json:"used_at,omitempty"`
}

// BackupCodeSet represents a user's backup codes.
type BackupCodeSet struct {
	// ID is the unique set identifier
	ID string `json:"id"`

	// UserID is the owning user
	UserID string `json:"user_id"`

	// TenantID is the tenant
	TenantID string `json:"tenant_id"`

	// Codes is the list of backup codes (hashed)
	Codes []BackupCode `json:"codes"`

	// Created is when the codes were generated
	Created time.Time `json:"created"`

	// Regenerated is when the codes were last regenerated
	Regenerated *time.Time `json:"regenerated,omitempty"`
}

// RemainingCodes returns the number of unused backup codes.
func (b *BackupCodeSet) RemainingCodes() int {
	count := 0
	for _, code := range b.Codes {
		if code.UsedAt == nil {
			count++
		}
	}
	return count
}

// UseCode marks a backup code as used if it matches.
// Returns true if the code was valid and unused.
func (b *BackupCodeSet) UseCode(codeHash string) bool {
	for i := range b.Codes {
		if b.Codes[i].UsedAt == nil && subtle.ConstantTimeCompare([]byte(b.Codes[i].Code), []byte(codeHash)) == 1 {
			now := time.Now()
			b.Codes[i].UsedAt = &now
			return true
		}
	}
	return false
}

// BackupCodeStatus provides information about backup code usage.
type BackupCodeStatus struct {
	// Total is the total number of backup codes generated
	Total int `json:"total"`

	// Remaining is the number of unused backup codes
	Remaining int `json:"remaining"`

	// Used is the number of used backup codes
	Used int `json:"used"`

	// GeneratedAt is when the codes were generated
	GeneratedAt time.Time `json:"generated_at"`

	// RegeneratedAt is when the codes were last regenerated
	RegeneratedAt *time.Time `json:"regenerated_at,omitempty"`
}

// MFAPolicy defines tenant-level MFA requirements.
type MFAPolicy struct {
	// Required forces all users to enroll in MFA
	Required bool `json:"required" yaml:"required"`

	// AllowedMethods specifies permitted MFA methods
	AllowedMethods []MFAMethod `json:"allowed_methods,omitempty" yaml:"allowed_methods,omitempty"`

	// GracePeriodDays allows login without MFA for new users
	GracePeriodDays int `json:"grace_period_days,omitempty" yaml:"grace_period_days,omitempty"`

	// RememberDeviceDays allows skipping MFA for trusted devices
	RememberDeviceDays int `json:"remember_device_days,omitempty" yaml:"remember_device_days,omitempty"`

	// MaxBackupCodes is the number of backup codes to generate
	MaxBackupCodes int `json:"max_backup_codes,omitempty" yaml:"max_backup_codes,omitempty"`

	// EnrollmentTimeout is the enrollment expiration in minutes
	EnrollmentTimeout int `json:"enrollment_timeout,omitempty" yaml:"enrollment_timeout,omitempty"`

	// ChallengeTimeout is the challenge expiration in minutes
	ChallengeTimeout int `json:"challenge_timeout,omitempty" yaml:"challenge_timeout,omitempty"`

	// MaxEnrollmentAttempts is the max verification attempts during enrollment
	MaxEnrollmentAttempts int `json:"max_enrollment_attempts,omitempty" yaml:"max_enrollment_attempts,omitempty"`

	// MaxChallengeAttempts is the max verification attempts during authentication
	MaxChallengeAttempts int `json:"max_challenge_attempts,omitempty" yaml:"max_challenge_attempts,omitempty"`
}

// Validate validates an MFAPolicy for correctness.
func (p *MFAPolicy) Validate() error {
	if p.MaxBackupCodes < 0 {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_policy.validate", "max_backup_codes cannot be negative", nil)
	}
	if p.GracePeriodDays < 0 {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_policy.validate", "grace_period_days cannot be negative", nil)
	}
	if p.RememberDeviceDays < 0 {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_policy.validate", "remember_device_days cannot be negative", nil)
	}
	return nil
}

// ApplyDefaults applies default values to the MFA policy.
func (p *MFAPolicy) ApplyDefaults() {
	if len(p.AllowedMethods) == 0 {
		p.AllowedMethods = []MFAMethod{MFAMethodTOTP, MFAMethodWebAuthn}
	}
	if p.MaxBackupCodes == 0 {
		p.MaxBackupCodes = 10
	}
	if p.EnrollmentTimeout == 0 {
		p.EnrollmentTimeout = 10 // 10 minutes
	}
	if p.ChallengeTimeout == 0 {
		p.ChallengeTimeout = 5 // 5 minutes
	}
	if p.MaxEnrollmentAttempts == 0 {
		p.MaxEnrollmentAttempts = 5
	}
	if p.MaxChallengeAttempts == 0 {
		p.MaxChallengeAttempts = 3
	}
}

// MFAConfig holds TOTP configuration options.
type MFAConfig struct {
	// Issuer is the name shown in authenticator apps
	Issuer string `json:"issuer" yaml:"issuer"`

	// Digits is the number of digits in the OTP code
	Digits int `json:"digits" yaml:"digits"`

	// Period is the time step in seconds
	Period int `json:"period" yaml:"period"`

	// Algorithm is the hash algorithm
	Algorithm MFAAlgorithm `json:"algorithm" yaml:"algorithm"`

	// Skew is the number of time periods to allow for clock drift
	Skew int `json:"skew" yaml:"skew"`

	// BackupCodes is the number of backup codes to generate
	BackupCodes int `json:"backup_codes" yaml:"backup_codes"`
}

// ApplyDefaults applies default values to the MFA config.
func (c *MFAConfig) ApplyDefaults() {
	if c.Issuer == "" {
		c.Issuer = "QuicBaaS"
	}
	if c.Digits == 0 {
		c.Digits = 6
	}
	if c.Period == 0 {
		c.Period = 30
	}
	if c.Algorithm == "" {
		c.Algorithm = defaultMFAAlgorithm()
	}
	if c.Skew == 0 {
		c.Skew = 1
	}
	if c.BackupCodes == 0 {
		c.BackupCodes = 10
	}
}

// Validate validates an MFAConfig for correctness.
func (c *MFAConfig) Validate() error {
	if c.Issuer == "" {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_config.validate", "issuer is required", nil)
	}
	if c.Digits < 6 || c.Digits > 8 {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_config.validate", "digits must be 6, 7, or 8", nil)
	}
	if c.Period < 15 || c.Period > 120 {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_config.validate", "period must be between 15 and 120 seconds", nil)
	}
	switch c.Algorithm {
	case MFAAlgorithmSHA1, MFAAlgorithmSHA256, MFAAlgorithmSHA512:
		// Valid
	default:
		return NewIAMError(ErrMFAInvalid, "iam.mfa_config.validate", "algorithm must be SHA1, SHA256, or SHA512", nil)
	}
	if c.Skew < 0 || c.Skew > 5 {
		return NewIAMError(ErrMFAInvalid, "iam.mfa_config.validate", "skew must be between 0 and 5", nil)
	}
	return nil
}

// MFAVerifyRequest represents a request to verify an MFA code.
type MFAVerifyRequest struct {
	// Code is the OTP code to verify
	Code string `json:"code"`

	// ChallengeID is the challenge being verified (for authentication)
	ChallengeID string `json:"challenge_id,omitempty"`

	// EnrollmentID is the enrollment being verified (for enrollment)
	EnrollmentID string `json:"enrollment_id,omitempty"`

	// UseBackupCode indicates whether this is a backup code
	UseBackupCode bool `json:"use_backup_code,omitempty"`
}

// MFAVerifyResponse represents the result of an MFA verification.
type MFAVerifyResponse struct {
	// Success indicates whether verification succeeded
	Success bool `json:"success"`

	// Token is the full authentication token (if challenge verification)
	Token string `json:"token,omitempty"`

	// RemainingAttempts is the number of remaining verification attempts
	RemainingAttempts int `json:"remaining_attempts,omitempty"`

	// BackupCodesRemaining is the number of remaining backup codes
	BackupCodesRemaining int `json:"backup_codes_remaining,omitempty"`
}

// MFAEnrollRequest represents a request to begin MFA enrollment.
type MFAEnrollRequest struct {
	// Method is the MFA method to enroll
	Method MFAMethod `json:"method"`
}

// MFAEnrollResponse represents the result of beginning MFA enrollment.
type MFAEnrollResponse struct {
	// EnrollmentID is the unique enrollment identifier
	EnrollmentID string `json:"enrollment_id"`

	// QRCodeURI is the otpauth:// URI for QR code generation
	QRCodeURI string `json:"qr_code_uri"`

	// Secret is the base32-encoded secret (for manual entry)
	Secret string `json:"secret"`

	// ExpiresAt is when the enrollment expires
	ExpiresAt time.Time `json:"expires_at"`
}
