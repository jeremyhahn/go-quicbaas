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
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestMFASecretValidate tests MFASecret.Validate() method.
func TestMFASecretValidate(t *testing.T) {
	tests := []struct {
		name    string
		secret  *MFASecret
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid secret with 6 digits",
			secret: &MFASecret{
				ID:          "secret-1",
				UserID:      10001,
				TenantID:    "tenant-1",
				Secret:      testTOTPSecret,
				Algorithm:   MFAAlgorithmSHA1,
				Digits:      6,
				Period:      30,
				Issuer:      "QuicBaaS",
				AccountName: "user@example.com",
				Verified:    true,
				Created:     time.Now(),
			},
			wantErr: false,
		},
		{
			name: "valid secret with 7 digits",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   7,
				Period:   30,
			},
			wantErr: false,
		},
		{
			name: "valid secret with 8 digits",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   8,
				Period:   30,
			},
			wantErr: false,
		},
		{
			name: "valid secret with minimum period (15 seconds)",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   6,
				Period:   15,
			},
			wantErr: false,
		},
		{
			name: "valid secret with maximum period (120 seconds)",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   6,
				Period:   120,
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			secret: &MFASecret{
				ID:       "",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   6,
				Period:   30,
			},
			wantErr: true,
			errMsg:  "secret ID is required",
		},
		{
			name: "missing UserID",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   0,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   6,
				Period:   30,
			},
			wantErr: true,
			errMsg:  "user ID is required",
		},
		{
			name: "missing TenantID",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "",
				Secret:   testTOTPSecret,
				Digits:   6,
				Period:   30,
			},
			wantErr: true,
			errMsg:  "tenant ID is required",
		},
		{
			name: "missing Secret",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   "",
				Digits:   6,
				Period:   30,
			},
			wantErr: true,
			errMsg:  "secret is required",
		},
		{
			name: "digits below minimum (5)",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   5,
				Period:   30,
			},
			wantErr: true,
			errMsg:  "digits must be 6, 7, or 8",
		},
		{
			name: "digits above maximum (9)",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   9,
				Period:   30,
			},
			wantErr: true,
			errMsg:  "digits must be 6, 7, or 8",
		},
		{
			name: "digits zero value",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   0,
				Period:   30,
			},
			wantErr: true,
			errMsg:  "digits must be 6, 7, or 8",
		},
		{
			name: "period below minimum (14 seconds)",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   6,
				Period:   14,
			},
			wantErr: true,
			errMsg:  "period must be between 15 and 120 seconds",
		},
		{
			name: "period above maximum (121 seconds)",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   6,
				Period:   121,
			},
			wantErr: true,
			errMsg:  "period must be between 15 and 120 seconds",
		},
		{
			name: "period zero value",
			secret: &MFASecret{
				ID:       "secret-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
				Digits:   6,
				Period:   0,
			},
			wantErr: true,
			errMsg:  "period must be between 15 and 120 seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.secret.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("MFASecret.Validate() expected error, got nil")
					return
				}
				iamErr, ok := err.(*dberrors.QBError)
				if !ok {
					t.Errorf("MFASecret.Validate() error is not IAMError type: %T", err)
					return
				}
				if iamErr.Code != dberrors.ErrValidation {
					t.Errorf("MFASecret.Validate() error code = %v, want %v", iamErr.Code, dberrors.ErrValidation)
				}
				if iamErr.Err.Error() != tt.errMsg {
					t.Errorf("MFASecret.Validate() error message = %v, want %v", iamErr.Err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("MFASecret.Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestMFAEnrollmentValidate tests MFAEnrollment.Validate() method.
func TestMFAEnrollmentValidate(t *testing.T) {
	tests := []struct {
		name       string
		enrollment *MFAEnrollment
		wantErr    bool
		errMsg     string
	}{
		{
			name: "valid enrollment",
			enrollment: &MFAEnrollment{
				ID:          "enrollment-1",
				UserID:      10001,
				TenantID:    "tenant-1",
				Method:      MFAMethodTOTP,
				Secret:      testTOTPSecret,
				QRCodeURI:   "otpauth://totp/QuicBaaS:user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=QuicBaaS",
				ExpiresAt:   time.Now().Add(10 * time.Minute),
				Attempts:    0,
				MaxAttempts: 5,
				Created:     time.Now(),
			},
			wantErr: false,
		},
		{
			name: "valid enrollment with WebAuthn",
			enrollment: &MFAEnrollment{
				ID:        "enrollment-1",
				UserID:    10001,
				TenantID:  "tenant-1",
				Method:    MFAMethodWebAuthn,
				Secret:    "webauthn-secret",
				ExpiresAt: time.Now().Add(10 * time.Minute),
				Created:   time.Now(),
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			enrollment: &MFAEnrollment{
				ID:       "",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
			},
			wantErr: true,
			errMsg:  "enrollment ID is required",
		},
		{
			name: "missing UserID",
			enrollment: &MFAEnrollment{
				ID:       "enrollment-1",
				UserID:   0,
				TenantID: "tenant-1",
				Secret:   testTOTPSecret,
			},
			wantErr: true,
			errMsg:  "user ID is required",
		},
		{
			name: "missing TenantID",
			enrollment: &MFAEnrollment{
				ID:       "enrollment-1",
				UserID:   10001,
				TenantID: "",
				Secret:   testTOTPSecret,
			},
			wantErr: true,
			errMsg:  "tenant ID is required",
		},
		{
			name: "missing Secret",
			enrollment: &MFAEnrollment{
				ID:       "enrollment-1",
				UserID:   10001,
				TenantID: "tenant-1",
				Secret:   "",
			},
			wantErr: true,
			errMsg:  "secret is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.enrollment.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("MFAEnrollment.Validate() expected error, got nil")
					return
				}
				iamErr, ok := err.(*dberrors.QBError)
				if !ok {
					t.Errorf("MFAEnrollment.Validate() error is not IAMError type: %T", err)
					return
				}
				if iamErr.Code != dberrors.ErrValidation {
					t.Errorf("MFAEnrollment.Validate() error code = %v, want %v", iamErr.Code, dberrors.ErrValidation)
				}
				if iamErr.Err.Error() != tt.errMsg {
					t.Errorf("MFAEnrollment.Validate() error message = %v, want %v", iamErr.Err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("MFAEnrollment.Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestMFAEnrollmentIsExpired tests MFAEnrollment.IsExpired() method.
func TestMFAEnrollmentIsExpired(t *testing.T) {
	tests := []struct {
		name       string
		enrollment *MFAEnrollment
		want       bool
	}{
		{
			name: "not expired - future expiration",
			enrollment: &MFAEnrollment{
				ExpiresAt: time.Now().Add(10 * time.Minute),
			},
			want: false,
		},
		{
			name: "expired - past expiration",
			enrollment: &MFAEnrollment{
				ExpiresAt: time.Now().Add(-10 * time.Minute),
			},
			want: true,
		},
		{
			name: "expired - exactly now (within margin)",
			enrollment: &MFAEnrollment{
				ExpiresAt: time.Now().Add(-1 * time.Millisecond),
			},
			want: true,
		},
		{
			name: "not expired - far future",
			enrollment: &MFAEnrollment{
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.enrollment.IsExpired()
			if got != tt.want {
				t.Errorf("MFAEnrollment.IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMFAChallengeValidate tests MFAChallenge.Validate() method.
func TestMFAChallengeValidate(t *testing.T) {
	tests := []struct {
		name      string
		challenge *MFAChallenge
		wantErr   bool
		errMsg    string
	}{
		{
			name: "valid challenge",
			challenge: &MFAChallenge{
				ID:           "challenge-1",
				UserID:       10001,
				TenantID:     "tenant-1",
				SessionToken: "session-token-123",
				Method:       MFAMethodTOTP,
				ExpiresAt:    time.Now().Add(5 * time.Minute),
				Attempts:     0,
				MaxAttempts:  3,
				Created:      time.Now(),
				IPAddress:    "192.0.2.1",
				UserAgent:    "Mozilla/5.0",
			},
			wantErr: false,
		},
		{
			name: "valid challenge without optional fields",
			challenge: &MFAChallenge{
				ID:           "challenge-1",
				UserID:       10001,
				TenantID:     "tenant-1",
				SessionToken: "session-token-123",
				Method:       MFAMethodWebAuthn,
				ExpiresAt:    time.Now().Add(5 * time.Minute),
				Created:      time.Now(),
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			challenge: &MFAChallenge{
				ID:           "",
				UserID:       10001,
				TenantID:     "tenant-1",
				SessionToken: "session-token-123",
			},
			wantErr: true,
			errMsg:  "challenge ID is required",
		},
		{
			name: "missing UserID",
			challenge: &MFAChallenge{
				ID:           "challenge-1",
				UserID:       0,
				TenantID:     "tenant-1",
				SessionToken: "session-token-123",
			},
			wantErr: true,
			errMsg:  "user ID is required",
		},
		{
			name: "missing TenantID",
			challenge: &MFAChallenge{
				ID:           "challenge-1",
				UserID:       10001,
				TenantID:     "",
				SessionToken: "session-token-123",
			},
			wantErr: true,
			errMsg:  "tenant ID is required",
		},
		{
			name: "missing SessionToken",
			challenge: &MFAChallenge{
				ID:           "challenge-1",
				UserID:       10001,
				TenantID:     "tenant-1",
				SessionToken: "",
			},
			wantErr: true,
			errMsg:  "session token is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.challenge.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("MFAChallenge.Validate() expected error, got nil")
					return
				}
				iamErr, ok := err.(*dberrors.QBError)
				if !ok {
					t.Errorf("MFAChallenge.Validate() error is not IAMError type: %T", err)
					return
				}
				if iamErr.Code != dberrors.ErrValidation {
					t.Errorf("MFAChallenge.Validate() error code = %v, want %v", iamErr.Code, dberrors.ErrValidation)
				}
				if iamErr.Err.Error() != tt.errMsg {
					t.Errorf("MFAChallenge.Validate() error message = %v, want %v", iamErr.Err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("MFAChallenge.Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestMFAChallengeIsExpired tests MFAChallenge.IsExpired() method.
func TestMFAChallengeIsExpired(t *testing.T) {
	tests := []struct {
		name      string
		challenge *MFAChallenge
		want      bool
	}{
		{
			name: "not expired - future expiration",
			challenge: &MFAChallenge{
				ExpiresAt: time.Now().Add(5 * time.Minute),
			},
			want: false,
		},
		{
			name: "expired - past expiration",
			challenge: &MFAChallenge{
				ExpiresAt: time.Now().Add(-5 * time.Minute),
			},
			want: true,
		},
		{
			name: "expired - exactly now (within margin)",
			challenge: &MFAChallenge{
				ExpiresAt: time.Now().Add(-1 * time.Millisecond),
			},
			want: true,
		},
		{
			name: "not expired - 1 second in future",
			challenge: &MFAChallenge{
				ExpiresAt: time.Now().Add(1 * time.Second),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.challenge.IsExpired()
			if got != tt.want {
				t.Errorf("MFAChallenge.IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMFAPolicyValidate tests MFAPolicy.Validate() method.
func TestMFAPolicyValidate(t *testing.T) {
	tests := []struct {
		name    string
		policy  *MFAPolicy
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid policy with all fields",
			policy: &MFAPolicy{
				Required:              true,
				AllowedMethods:        []MFAMethod{MFAMethodTOTP, MFAMethodWebAuthn},
				GracePeriodDays:       7,
				RememberDeviceDays:    30,
				MaxBackupCodes:        10,
				EnrollmentTimeout:     10,
				ChallengeTimeout:      5,
				MaxEnrollmentAttempts: 5,
				MaxChallengeAttempts:  3,
			},
			wantErr: false,
		},
		{
			name: "valid policy with zero values",
			policy: &MFAPolicy{
				Required:              false,
				GracePeriodDays:       0,
				RememberDeviceDays:    0,
				MaxBackupCodes:        0,
				EnrollmentTimeout:     0,
				ChallengeTimeout:      0,
				MaxEnrollmentAttempts: 0,
				MaxChallengeAttempts:  0,
			},
			wantErr: false,
		},
		{
			name: "valid policy with single method",
			policy: &MFAPolicy{
				AllowedMethods: []MFAMethod{MFAMethodTOTP},
				MaxBackupCodes: 5,
			},
			wantErr: false,
		},
		{
			name: "negative MaxBackupCodes",
			policy: &MFAPolicy{
				MaxBackupCodes: -1,
			},
			wantErr: true,
			errMsg:  "max_backup_codes cannot be negative",
		},
		{
			name: "negative GracePeriodDays",
			policy: &MFAPolicy{
				GracePeriodDays: -1,
				MaxBackupCodes:  10,
			},
			wantErr: true,
			errMsg:  "grace_period_days cannot be negative",
		},
		{
			name: "negative RememberDeviceDays",
			policy: &MFAPolicy{
				RememberDeviceDays: -1,
				MaxBackupCodes:     10,
				GracePeriodDays:    0,
			},
			wantErr: true,
			errMsg:  "remember_device_days cannot be negative",
		},
		{
			name: "large positive values",
			policy: &MFAPolicy{
				GracePeriodDays:    365,
				RememberDeviceDays: 90,
				MaxBackupCodes:     100,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("MFAPolicy.Validate() expected error, got nil")
					return
				}
				iamErr, ok := err.(*dberrors.QBError)
				if !ok {
					t.Errorf("MFAPolicy.Validate() error is not IAMError type: %T", err)
					return
				}
				if iamErr.Code != dberrors.ErrValidation {
					t.Errorf("MFAPolicy.Validate() error code = %v, want %v", iamErr.Code, dberrors.ErrValidation)
				}
				if iamErr.Err.Error() != tt.errMsg {
					t.Errorf("MFAPolicy.Validate() error message = %v, want %v", iamErr.Err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("MFAPolicy.Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestMFAPolicyApplyDefaults tests MFAPolicy.ApplyDefaults() method.
func TestMFAPolicyApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		policy   *MFAPolicy
		expected *MFAPolicy
	}{
		{
			name:   "empty policy gets all defaults",
			policy: &MFAPolicy{},
			expected: &MFAPolicy{
				AllowedMethods:        []MFAMethod{MFAMethodTOTP, MFAMethodWebAuthn},
				MaxBackupCodes:        10,
				EnrollmentTimeout:     10,
				ChallengeTimeout:      5,
				MaxEnrollmentAttempts: 5,
				MaxChallengeAttempts:  3,
			},
		},
		{
			name: "policy with some values keeps them",
			policy: &MFAPolicy{
				AllowedMethods:        []MFAMethod{MFAMethodTOTP},
				MaxBackupCodes:        20,
				EnrollmentTimeout:     15,
				ChallengeTimeout:      0,
				MaxEnrollmentAttempts: 0,
				MaxChallengeAttempts:  0,
			},
			expected: &MFAPolicy{
				AllowedMethods:        []MFAMethod{MFAMethodTOTP},
				MaxBackupCodes:        20,
				EnrollmentTimeout:     15,
				ChallengeTimeout:      5,
				MaxEnrollmentAttempts: 5,
				MaxChallengeAttempts:  3,
			},
		},
		{
			name: "policy with all values unchanged",
			policy: &MFAPolicy{
				AllowedMethods:        []MFAMethod{MFAMethodWebAuthn},
				MaxBackupCodes:        15,
				EnrollmentTimeout:     20,
				ChallengeTimeout:      10,
				MaxEnrollmentAttempts: 10,
				MaxChallengeAttempts:  5,
			},
			expected: &MFAPolicy{
				AllowedMethods:        []MFAMethod{MFAMethodWebAuthn},
				MaxBackupCodes:        15,
				EnrollmentTimeout:     20,
				ChallengeTimeout:      10,
				MaxEnrollmentAttempts: 10,
				MaxChallengeAttempts:  5,
			},
		},
		{
			name: "zero values get defaults, non-zero preserved",
			policy: &MFAPolicy{
				GracePeriodDays:       7,
				RememberDeviceDays:    30,
				MaxBackupCodes:        0,
				EnrollmentTimeout:     0,
				ChallengeTimeout:      0,
				MaxEnrollmentAttempts: 0,
				MaxChallengeAttempts:  0,
			},
			expected: &MFAPolicy{
				AllowedMethods:        []MFAMethod{MFAMethodTOTP, MFAMethodWebAuthn},
				GracePeriodDays:       7,
				RememberDeviceDays:    30,
				MaxBackupCodes:        10,
				EnrollmentTimeout:     10,
				ChallengeTimeout:      5,
				MaxEnrollmentAttempts: 5,
				MaxChallengeAttempts:  3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.policy.ApplyDefaults()

			if len(tt.policy.AllowedMethods) != len(tt.expected.AllowedMethods) {
				t.Errorf("AllowedMethods length = %v, want %v", len(tt.policy.AllowedMethods), len(tt.expected.AllowedMethods))
			} else {
				for i, method := range tt.policy.AllowedMethods {
					if method != tt.expected.AllowedMethods[i] {
						t.Errorf("AllowedMethods[%d] = %v, want %v", i, method, tt.expected.AllowedMethods[i])
					}
				}
			}

			if tt.policy.MaxBackupCodes != tt.expected.MaxBackupCodes {
				t.Errorf("MaxBackupCodes = %v, want %v", tt.policy.MaxBackupCodes, tt.expected.MaxBackupCodes)
			}
			if tt.policy.EnrollmentTimeout != tt.expected.EnrollmentTimeout {
				t.Errorf("EnrollmentTimeout = %v, want %v", tt.policy.EnrollmentTimeout, tt.expected.EnrollmentTimeout)
			}
			if tt.policy.ChallengeTimeout != tt.expected.ChallengeTimeout {
				t.Errorf("ChallengeTimeout = %v, want %v", tt.policy.ChallengeTimeout, tt.expected.ChallengeTimeout)
			}
			if tt.policy.MaxEnrollmentAttempts != tt.expected.MaxEnrollmentAttempts {
				t.Errorf("MaxEnrollmentAttempts = %v, want %v", tt.policy.MaxEnrollmentAttempts, tt.expected.MaxEnrollmentAttempts)
			}
			if tt.policy.MaxChallengeAttempts != tt.expected.MaxChallengeAttempts {
				t.Errorf("MaxChallengeAttempts = %v, want %v", tt.policy.MaxChallengeAttempts, tt.expected.MaxChallengeAttempts)
			}
			if tt.policy.GracePeriodDays != tt.expected.GracePeriodDays {
				t.Errorf("GracePeriodDays = %v, want %v", tt.policy.GracePeriodDays, tt.expected.GracePeriodDays)
			}
			if tt.policy.RememberDeviceDays != tt.expected.RememberDeviceDays {
				t.Errorf("RememberDeviceDays = %v, want %v", tt.policy.RememberDeviceDays, tt.expected.RememberDeviceDays)
			}
		})
	}
}

// TestMFAConfigValidate tests MFAConfig.Validate() method.
func TestMFAConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *MFAConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with SHA1",
			config: &MFAConfig{
				Issuer:      "QuicBaaS",
				Digits:      6,
				Period:      30,
				Algorithm:   MFAAlgorithmSHA1,
				Skew:        1,
				BackupCodes: 10,
			},
			wantErr: false,
		},
		{
			name: "valid config with SHA256",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    30,
				Algorithm: MFAAlgorithmSHA256,
				Skew:      1,
			},
			wantErr: false,
		},
		{
			name: "valid config with SHA512",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    30,
				Algorithm: MFAAlgorithmSHA512,
				Skew:      1,
			},
			wantErr: false,
		},
		{
			name: "valid config with 7 digits",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    7,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: false,
		},
		{
			name: "valid config with 8 digits",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    8,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: false,
		},
		{
			name: "valid config with minimum period (15)",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    15,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: false,
		},
		{
			name: "valid config with maximum period (120)",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    120,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: false,
		},
		{
			name: "valid config with skew 0",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      0,
			},
			wantErr: false,
		},
		{
			name: "valid config with maximum skew (5)",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      5,
			},
			wantErr: false,
		},
		{
			name: "missing issuer",
			config: &MFAConfig{
				Issuer:    "",
				Digits:    6,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: true,
			errMsg:  "issuer is required",
		},
		{
			name: "digits below minimum (5)",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    5,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: true,
			errMsg:  "digits must be 6, 7, or 8",
		},
		{
			name: "digits above maximum (9)",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    9,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: true,
			errMsg:  "digits must be 6, 7, or 8",
		},
		{
			name: "digits zero value",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    0,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: true,
			errMsg:  "digits must be 6, 7, or 8",
		},
		{
			name: "period below minimum (14)",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    14,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: true,
			errMsg:  "period must be between 15 and 120 seconds",
		},
		{
			name: "period above maximum (121)",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    121,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: true,
			errMsg:  "period must be between 15 and 120 seconds",
		},
		{
			name: "period zero value",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    0,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      1,
			},
			wantErr: true,
			errMsg:  "period must be between 15 and 120 seconds",
		},
		{
			name: "invalid algorithm",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    30,
				Algorithm: "MD5",
				Skew:      1,
			},
			wantErr: true,
			errMsg:  "algorithm must be SHA1, SHA256, or SHA512",
		},
		{
			name: "empty algorithm",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    30,
				Algorithm: "",
				Skew:      1,
			},
			wantErr: true,
			errMsg:  "algorithm must be SHA1, SHA256, or SHA512",
		},
		{
			name: "skew below minimum (-1)",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      -1,
			},
			wantErr: true,
			errMsg:  "skew must be between 0 and 5",
		},
		{
			name: "skew above maximum (6)",
			config: &MFAConfig{
				Issuer:    "QuicBaaS",
				Digits:    6,
				Period:    30,
				Algorithm: MFAAlgorithmSHA1,
				Skew:      6,
			},
			wantErr: true,
			errMsg:  "skew must be between 0 and 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("MFAConfig.Validate() expected error, got nil")
					return
				}
				iamErr, ok := err.(*dberrors.QBError)
				if !ok {
					t.Errorf("MFAConfig.Validate() error is not IAMError type: %T", err)
					return
				}
				if iamErr.Code != dberrors.ErrValidation {
					t.Errorf("MFAConfig.Validate() error code = %v, want %v", iamErr.Code, dberrors.ErrValidation)
				}
				if iamErr.Err.Error() != tt.errMsg {
					t.Errorf("MFAConfig.Validate() error message = %v, want %v", iamErr.Err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("MFAConfig.Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestMFAConfigApplyDefaults tests MFAConfig.ApplyDefaults() method.
func TestMFAConfigApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   *MFAConfig
		expected *MFAConfig
	}{
		{
			name:   "empty config gets all defaults",
			config: &MFAConfig{},
			expected: &MFAConfig{
				Issuer:      "QuicBaaS",
				Digits:      6,
				Period:      30,
				Algorithm:   wantDefaultMFAAlgorithm(),
				Skew:        1,
				BackupCodes: 10,
			},
		},
		{
			name: "config with some values keeps them",
			config: &MFAConfig{
				Issuer:      "MyApp",
				Digits:      0,
				Period:      0,
				Algorithm:   "",
				Skew:        0,
				BackupCodes: 0,
			},
			expected: &MFAConfig{
				Issuer:      "MyApp",
				Digits:      6,
				Period:      30,
				Algorithm:   wantDefaultMFAAlgorithm(),
				Skew:        1,
				BackupCodes: 10,
			},
		},
		{
			name: "config with all values unchanged",
			config: &MFAConfig{
				Issuer:      "CustomIssuer",
				Digits:      8,
				Period:      60,
				Algorithm:   MFAAlgorithmSHA512,
				Skew:        2,
				BackupCodes: 15,
			},
			expected: &MFAConfig{
				Issuer:      "CustomIssuer",
				Digits:      8,
				Period:      60,
				Algorithm:   MFAAlgorithmSHA512,
				Skew:        2,
				BackupCodes: 15,
			},
		},
		{
			name: "partial config with custom values",
			config: &MFAConfig{
				Issuer:    "",
				Digits:    7,
				Period:    45,
				Algorithm: MFAAlgorithmSHA256,
				Skew:      0,
			},
			expected: &MFAConfig{
				Issuer:      "QuicBaaS",
				Digits:      7,
				Period:      45,
				Algorithm:   MFAAlgorithmSHA256,
				Skew:        1,
				BackupCodes: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.ApplyDefaults()

			if tt.config.Issuer != tt.expected.Issuer {
				t.Errorf("Issuer = %v, want %v", tt.config.Issuer, tt.expected.Issuer)
			}
			if tt.config.Digits != tt.expected.Digits {
				t.Errorf("Digits = %v, want %v", tt.config.Digits, tt.expected.Digits)
			}
			if tt.config.Period != tt.expected.Period {
				t.Errorf("Period = %v, want %v", tt.config.Period, tt.expected.Period)
			}
			if tt.config.Algorithm != tt.expected.Algorithm {
				t.Errorf("Algorithm = %v, want %v", tt.config.Algorithm, tt.expected.Algorithm)
			}
			if tt.config.Skew != tt.expected.Skew {
				t.Errorf("Skew = %v, want %v", tt.config.Skew, tt.expected.Skew)
			}
			if tt.config.BackupCodes != tt.expected.BackupCodes {
				t.Errorf("BackupCodes = %v, want %v", tt.config.BackupCodes, tt.expected.BackupCodes)
			}
		})
	}
}

// TestBackupCodeSetRemainingCodes tests BackupCodeSet.RemainingCodes() method.
func TestBackupCodeSetRemainingCodes(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		codeSet  *BackupCodeSet
		expected int
	}{
		{
			name: "all codes unused",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{
					{Code: "code1", UsedAt: nil},
					{Code: "code2", UsedAt: nil},
					{Code: "code3", UsedAt: nil},
				},
			},
			expected: 3,
		},
		{
			name: "all codes used",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{
					{Code: "code1", UsedAt: &now},
					{Code: "code2", UsedAt: &now},
					{Code: "code3", UsedAt: &now},
				},
			},
			expected: 0,
		},
		{
			name: "mixed used and unused codes",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{
					{Code: "code1", UsedAt: &now},
					{Code: "code2", UsedAt: nil},
					{Code: "code3", UsedAt: &now},
					{Code: "code4", UsedAt: nil},
					{Code: "code5", UsedAt: nil},
				},
			},
			expected: 3,
		},
		{
			name: "empty code set",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{},
			},
			expected: 0,
		},
		{
			name:     "nil codes slice",
			codeSet:  &BackupCodeSet{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.codeSet.RemainingCodes()
			if got != tt.expected {
				t.Errorf("BackupCodeSet.RemainingCodes() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestBackupCodeSetUseCode tests BackupCodeSet.UseCode() method.
func TestBackupCodeSetUseCode(t *testing.T) {
	tests := []struct {
		name           string
		codeSet        *BackupCodeSet
		codeHash       string
		expectedResult bool
		expectedUsed   int
	}{
		{
			name: "valid unused code",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{
					{Code: "hash1", UsedAt: nil},
					{Code: "hash2", UsedAt: nil},
					{Code: "hash3", UsedAt: nil},
				},
			},
			codeHash:       "hash2",
			expectedResult: true,
			expectedUsed:   1,
		},
		{
			name: "code already used",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{
					{Code: "hash1", UsedAt: nil},
					{Code: "hash2", UsedAt: func() *time.Time { t := time.Now(); return &t }()},
					{Code: "hash3", UsedAt: nil},
				},
			},
			codeHash:       "hash2",
			expectedResult: false,
			expectedUsed:   1,
		},
		{
			name: "invalid code",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{
					{Code: "hash1", UsedAt: nil},
					{Code: "hash2", UsedAt: nil},
					{Code: "hash3", UsedAt: nil},
				},
			},
			codeHash:       "invalid",
			expectedResult: false,
			expectedUsed:   0,
		},
		{
			name: "empty code set",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{},
			},
			codeHash:       "hash1",
			expectedResult: false,
			expectedUsed:   0,
		},
		{
			name: "use first code",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{
					{Code: "hash1", UsedAt: nil},
					{Code: "hash2", UsedAt: nil},
				},
			},
			codeHash:       "hash1",
			expectedResult: true,
			expectedUsed:   1,
		},
		{
			name: "use last code",
			codeSet: &BackupCodeSet{
				Codes: []BackupCode{
					{Code: "hash1", UsedAt: nil},
					{Code: "hash2", UsedAt: nil},
					{Code: "hash3", UsedAt: nil},
				},
			},
			codeHash:       "hash3",
			expectedResult: true,
			expectedUsed:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.codeSet.UseCode(tt.codeHash)
			if got != tt.expectedResult {
				t.Errorf("BackupCodeSet.UseCode() = %v, want %v", got, tt.expectedResult)
			}

			usedCount := 0
			for _, code := range tt.codeSet.Codes {
				if code.UsedAt != nil {
					usedCount++
				}
			}
			if usedCount != tt.expectedUsed {
				t.Errorf("Used codes count = %v, want %v", usedCount, tt.expectedUsed)
			}

			// If successful, verify the specific code was marked as used
			if got && tt.expectedResult {
				found := false
				for _, code := range tt.codeSet.Codes {
					if code.Code == tt.codeHash && code.UsedAt != nil {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Used code was not properly marked as used")
				}
			}
		})
	}
}
