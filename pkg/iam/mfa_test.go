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
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- RFC 6238 reference derivation for test vectors
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-auth/pkg/otp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTOTPSecret is a 160-bit base32 TOTP secret - the size
// otp.GenerateProtectedSecret issues for a real enrollment. FIPS 140-only mode
// rejects HMAC keys shorter than 112 bits, so a fixture secret must not be
// shorter than production's.
const testTOTPSecret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"

// referenceHashes maps the algorithm the manager resolved onto the standard
// library hash the reference derivation uses.
var referenceHashes = map[otp.Algorithm]func() hash.Hash{
	otp.AlgorithmSHA1:   sha1.New,
	otp.AlgorithmSHA256: sha256.New,
	otp.AlgorithmSHA512: sha512.New,
}

// referenceTOTP is RFC 6238 written out from the RFC over the standard
// library: HMAC of the big-endian step counter, RFC 4226 §5.3 dynamic
// truncation, reduced to digits decimal digits. It is independent of go-auth,
// so a code it derives was produced by something other than the code under
// test. TestReferenceTOTPMatchesRFC6238Vectors pins it to the RFC.
func referenceTOTP(t *testing.T, key []byte, newHash func() hash.Hash, counter uint64, digits int) string {
	t.Helper()

	mac := hmac.New(newHash, key)
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	_, err := mac.Write(message[:])
	require.NoError(t, err)
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	modulus := uint32(1)
	for range digits {
		modulus *= 10
	}
	return fmt.Sprintf("%0*d", digits, truncated%modulus)
}

// generateTOTPCodeAt derives the code an enrolled authenticator app would show
// for secret at the instant `at`.
//
// It derives the code with referenceTOTP rather than with the manager's own
// OTP library, so a code accepted here was produced by something other than
// the code under test. Deriving the expected code with the same call the
// manager makes would only prove that call is self-consistent.
func generateTOTPCodeAt(t *testing.T, secret string, config MFAConfig, at time.Time) string {
	t.Helper()

	algorithm, err := convertAlgorithm("test.totp.generate", config.Algorithm)
	require.NoError(t, err)
	newHash, known := referenceHashes[algorithm]
	require.True(t, known, "no reference hash for %q", algorithm)

	// Match the defaults the OTP library applies to an unset field, so a
	// config the caller has not run ApplyDefaults on still derives the code
	// the manager will check against.
	digits := config.Digits
	if digits == 0 {
		digits = 6
	}
	period := config.Period
	if period == 0 {
		period = 30
	}

	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	require.NoError(t, err)

	return referenceTOTP(t, key, newHash, uint64(at.Unix()/int64(period)), digits) // #nosec G115 -- test instants are after the epoch
}

// TestReferenceTOTPMatchesRFC6238Vectors pins referenceTOTP to the RFC 6238
// Appendix B test vectors, so the oracle the MFA tests check the manager
// against is itself known to be right.
func TestReferenceTOTPMatchesRFC6238Vectors(t *testing.T) {
	seeds := map[otp.Algorithm][]byte{
		otp.AlgorithmSHA1:   []byte("12345678901234567890"),
		otp.AlgorithmSHA256: []byte("12345678901234567890123456789012"),
		otp.AlgorithmSHA512: []byte("1234567890123456789012345678901234567890123456789012345678901234"),
	}
	vectors := []struct {
		at        int64
		algorithm otp.Algorithm
		want      string
	}{
		{59, otp.AlgorithmSHA1, "94287082"},
		{59, otp.AlgorithmSHA256, "46119246"},
		{59, otp.AlgorithmSHA512, "90693936"},
		{1111111109, otp.AlgorithmSHA1, "07081804"},
		{1111111109, otp.AlgorithmSHA256, "68084774"},
		{1111111109, otp.AlgorithmSHA512, "25091201"},
		{20000000000, otp.AlgorithmSHA1, "65353130"},
	}
	for _, v := range vectors {
		got := referenceTOTP(t, seeds[v.algorithm], referenceHashes[v.algorithm], uint64(v.at/30), 8)
		assert.Equal(t, v.want, got, "T=%d %s", v.at, v.algorithm)
	}

	// A wrong key must not reproduce the vector: the oracle is not a constant.
	wrongKey := []byte("09876543210987654321")
	assert.NotEqual(t, "94287082", referenceTOTP(t, wrongKey, sha1.New, 1, 8))
}

// generateValidTOTPCode derives the code for the current instant. It is enough
// for tests that feed the code back to VerifyMFA, because verification allows
// MFAConfig.Skew steps of drift (1 by default) and so absorbs a step boundary
// crossed between generating and verifying. A test that compares this value
// against one produced by a SEPARATE clock read has no such tolerance and must
// use totpCodesForBracket instead.
func generateValidTOTPCode(t *testing.T, secret string, config MFAConfig) string {
	t.Helper()
	return generateTOTPCodeAt(t, secret, config, time.Now().UTC())
}

// totpCodesForBracket returns the code for every TOTP step the interval
// [before, after] touches.
//
// It exists for assertions against a value produced by code that samples its
// own clock, which for TOTP cannot be pinned: otp.Authenticator.Generate takes
// a counter only for HOTP and always uses time.Now() for TOTP. Bracketing that
// call with two clock reads bounds the instant it sampled, so the set of codes
// it can legitimately have produced is known exactly — normally one, and two
// when a step boundary falls between the reads.
//
// This states what is knowable rather than loosening the assertion: away from a
// boundary it yields a single code and the comparison stays exact.
func totpCodesForBracket(t *testing.T, secret string, config MFAConfig, before, after time.Time) []string {
	t.Helper()

	period := int64(config.Period)
	if period == 0 {
		period = 30
	}

	first, last := before.Unix()/period, after.Unix()/period
	require.LessOrEqual(t, last-first, int64(1),
		"the bracketed call spanned more than one %ds TOTP step: the process stalled, "+
			"which is a fault to report rather than a boundary to absorb", period)

	codes := make([]string, 0, 2)
	for counter := first; counter <= last; counter++ {
		codes = append(codes, generateTOTPCodeAt(t, secret, config, time.Unix(counter*period, 0).UTC()))
	}
	return codes
}

// TestNewMFAManager tests creating a new MFA manager
// createMFATestUser stores user 123 of tenant-123, with an email, in the
// manager's user storage, so enrollment can label the account with it.
func createMFATestUser(t *testing.T, m *DefaultMFAManager) {
	t.Helper()
	user := &User{
		ID:       123,
		TenantID: "tenant-123",
		Email:    "test@example.com",
		Name:     "Test User",
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, m.userStorage.CreateUser(context.Background(), user))
}

func TestNewMFAManager(t *testing.T) {
	customStorage := NewMemoryMFAStorage()
	tests := []struct {
		name        string
		opts        []MFAManagerOption
		expectError bool
		validate    func(t *testing.T, m *DefaultMFAManager)
	}{
		{
			name:        "default configuration",
			opts:        nil,
			expectError: false,
			validate: func(t *testing.T, m *DefaultMFAManager) {
				assert.NotNil(t, m)
				assert.NotNil(t, m.storage)
				assert.Equal(t, "QuicBaaS", m.config.Issuer)
				assert.Equal(t, 6, m.config.Digits)
				assert.Equal(t, 30, m.config.Period)
				assert.Equal(t, wantDefaultMFAAlgorithm(), m.config.Algorithm)
				assert.Equal(t, 1, m.config.Skew)
				assert.Equal(t, 10, m.config.BackupCodes)
			},
		},
		{
			name: "custom configuration",
			opts: []MFAManagerOption{
				WithMFAConfig(MFAConfig{
					Issuer:      "TestApp",
					Digits:      8,
					Period:      60,
					Algorithm:   MFAAlgorithmSHA256,
					Skew:        2,
					BackupCodes: 5,
				}),
			},
			expectError: false,
			validate: func(t *testing.T, m *DefaultMFAManager) {
				assert.Equal(t, "TestApp", m.config.Issuer)
				assert.Equal(t, 8, m.config.Digits)
				assert.Equal(t, 60, m.config.Period)
				assert.Equal(t, MFAAlgorithmSHA256, m.config.Algorithm)
				assert.Equal(t, 2, m.config.Skew)
				assert.Equal(t, 5, m.config.BackupCodes)
			},
		},
		{
			name: "custom storage",
			opts: []MFAManagerOption{
				WithMFAStorage(customStorage),
			},
			expectError: false,
			validate: func(t *testing.T, m *DefaultMFAManager) {
				assert.Same(t, customStorage, m.storage)
			},
		},
		{
			name: "invalid configuration - bad digits",
			opts: []MFAManagerOption{
				WithMFAConfig(MFAConfig{
					Issuer: "TestApp",
					Digits: 5, // invalid
					Period: 30,
				}),
			},
			expectError: true,
		},
		{
			name: "invalid configuration - bad period",
			opts: []MFAManagerOption{
				WithMFAConfig(MFAConfig{
					Issuer: "TestApp",
					Digits: 6,
					Period: 10, // invalid (< 15)
				}),
			},
			expectError: true,
		},
		{
			name: "invalid configuration - bad algorithm",
			opts: []MFAManagerOption{
				WithMFAConfig(MFAConfig{
					Issuer:    "TestApp",
					Digits:    6,
					Period:    30,
					Algorithm: "MD5", // invalid
				}),
			},
			expectError: true,
		},
		{
			name: "invalid configuration - bad skew",
			opts: []MFAManagerOption{
				WithMFAConfig(MFAConfig{
					Issuer:    "TestApp",
					Digits:    6,
					Period:    30,
					Algorithm: defaultMFAAlgorithm(),
					Skew:      10, // invalid (> 5)
				}),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager(tt.opts...)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, m)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, m)
				if tt.validate != nil {
					tt.validate(t, m)
				}
			}
		})
	}
}

// TestBeginEnrollment tests starting MFA enrollment
func TestBeginEnrollment(t *testing.T) {
	tests := []struct {
		name        string
		userID      uint64
		tenantID    string
		method      MFAMethod
		opts        []MFAManagerOption
		setup       func(t *testing.T, m *DefaultMFAManager)
		expectError error
		validate    func(t *testing.T, resp *MFAEnrollResponse)
	}{
		{
			name:        "successful enrollment",
			userID:      123,
			tenantID:    "tenant-123",
			method:      MFAMethodTOTP,
			expectError: nil,
			validate: func(t *testing.T, resp *MFAEnrollResponse) {
				assert.NotEmpty(t, resp.EnrollmentID)
				assert.NotEmpty(t, resp.QRCodeURI)
				assert.NotEmpty(t, resp.Secret)
				assert.True(t, resp.ExpiresAt.After(time.Now()))
				assert.Contains(t, resp.QRCodeURI, "otpauth://totp/")
			},
		},
		{
			name:     "configured issuer and stored user email label the QR code",
			userID:   123,
			tenantID: "tenant-123",
			method:   MFAMethodTOTP,
			opts: []MFAManagerOption{
				WithMFAConfig(MFAConfig{
					Issuer:      "TestIssuer",
					Digits:      6,
					Period:      30,
					Algorithm:   MFAAlgorithmSHA256,
					Skew:        1,
					BackupCodes: 10,
				}),
				WithUserStorage(NewMemoryStorage()),
			},
			setup: createMFATestUser,
			validate: func(t *testing.T, resp *MFAEnrollResponse) {
				assert.Contains(t, resp.QRCodeURI, "TestIssuer")
				assert.Contains(t, resp.QRCodeURI, "example.com")
			},
		},
		{
			name:        "missing user ID",
			userID:      0,
			tenantID:    "tenant-123",
			method:      MFAMethodTOTP,
			expectError: ErrMFAInvalid,
		},
		{
			name:        "missing tenant ID",
			userID:      123,
			tenantID:    "",
			method:      MFAMethodTOTP,
			expectError: ErrMFAInvalid,
		},
		{
			name:        "unsupported method",
			userID:      123,
			tenantID:    "tenant-123",
			method:      MFAMethodWebAuthn,
			expectError: ErrMFAMethodNotAllowed,
		},
		{
			name:     "already enrolled",
			userID:   123,
			tenantID: "tenant-123",
			method:   MFAMethodTOTP,
			setup: func(t *testing.T, m *DefaultMFAManager) {
				// Create a verified secret
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)
			},
			expectError: ErrMFAAlreadyEnrolled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager(tt.opts...)
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(t, m)
			}

			resp, err := m.BeginEnrollment(context.Background(), tt.userID, tt.tenantID, tt.method)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
		})
	}
}

// TestCompleteEnrollment tests completing MFA enrollment
func TestCompleteEnrollment(t *testing.T) {
	tests := []struct {
		name        string
		opts        []MFAManagerOption
		setup       func(t *testing.T, m *DefaultMFAManager) (enrollmentID, code string)
		expectError error
		// after checks stored state once CompleteEnrollment has returned.
		after func(t *testing.T, m *DefaultMFAManager, enrollmentID string)
	}{
		{
			name: "successful enrollment completion",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				resp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)
				code := generateValidTOTPCode(t, resp.Secret, m.config)
				return resp.EnrollmentID, code
			},
			expectError: nil,
			after: func(t *testing.T, m *DefaultMFAManager, enrollmentID string) {
				_, err := m.storage.GetEnrollment(context.Background(), enrollmentID)
				assert.ErrorIs(t, err, ErrMFAEnrollmentNotFound, "a completed enrollment is deleted")
			},
		},
		{
			name: "stored user email names the enrolled account",
			opts: []MFAManagerOption{WithUserStorage(NewMemoryStorage())},
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				createMFATestUser(t, m)
				resp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)
				return resp.EnrollmentID, generateValidTOTPCode(t, resp.Secret, m.config)
			},
			after: func(t *testing.T, m *DefaultMFAManager, enrollmentID string) {
				secret, err := m.storage.GetSecret(context.Background(), 123, "tenant-123")
				require.NoError(t, err)
				assert.Equal(t, "test@example.com", secret.AccountName)
			},
		},
		{
			name: "missing enrollment ID",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				return "", "123456"
			},
			expectError: ErrMFAInvalid,
		},
		{
			name: "missing code",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				resp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)
				return resp.EnrollmentID, ""
			},
			expectError: ErrMFACodeInvalid,
		},
		{
			name: "enrollment not found",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				return "invalid-enrollment-id", "123456"
			},
			expectError: ErrMFAEnrollmentNotFound,
		},
		{
			name: "expired enrollment",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				resp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)

				// Manually expire the enrollment
				enrollment, err := m.storage.GetEnrollment(context.Background(), resp.EnrollmentID)
				require.NoError(t, err)
				enrollment.ExpiresAt = time.Now().Add(-1 * time.Minute)
				_ = m.storage.SaveEnrollment(context.Background(), enrollment)

				return resp.EnrollmentID, "123456"
			},
			expectError: ErrMFAEnrollmentExpired,
		},
		{
			name: "invalid code",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				resp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)
				return resp.EnrollmentID, "000000" // invalid code
			},
			expectError: ErrMFACodeInvalid,
			after: func(t *testing.T, m *DefaultMFAManager, enrollmentID string) {
				enrollment, err := m.storage.GetEnrollment(context.Background(), enrollmentID)
				require.NoError(t, err)
				assert.Equal(t, 1, enrollment.Attempts, "a wrong code counts an attempt")
			},
		},
		{
			name: "max attempts exceeded",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				resp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)

				// Exhaust attempts
				enrollment, err := m.storage.GetEnrollment(context.Background(), resp.EnrollmentID)
				require.NoError(t, err)
				enrollment.Attempts = enrollment.MaxAttempts
				_ = m.storage.SaveEnrollment(context.Background(), enrollment)

				return resp.EnrollmentID, "123456"
			},
			expectError: ErrMFAMaxAttemptsExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager(tt.opts...)
			require.NoError(t, err)

			enrollmentID, code := tt.setup(t, m)

			err = m.CompleteEnrollment(context.Background(), 123, "tenant-123", enrollmentID, code)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
			} else {
				assert.NoError(t, err)

				// Verify secret was saved
				secret, err := m.storage.GetSecret(context.Background(), 123, "tenant-123")
				assert.NoError(t, err)
				assert.True(t, secret.Verified)

				// Verify backup codes were generated
				backupCodes, err := m.storage.GetBackupCodes(context.Background(), 123, "tenant-123")
				assert.NoError(t, err)
				assert.Len(t, backupCodes.Codes, m.config.BackupCodes)
			}
			if tt.after != nil {
				tt.after(t, m, enrollmentID)
			}
		})
	}
}

// TestCancelEnrollment tests canceling MFA enrollment
func TestCancelEnrollment(t *testing.T) {
	tests := []struct {
		name         string
		enrollmentID string
		setup        func(t *testing.T, m *DefaultMFAManager) string
		expectError  error
	}{
		{
			name: "successful cancellation",
			setup: func(t *testing.T, m *DefaultMFAManager) string {
				resp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)
				return resp.EnrollmentID
			},
			expectError: nil,
		},
		{
			name:         "missing enrollment ID",
			enrollmentID: "",
			expectError:  ErrMFAInvalid,
		},
		{
			name:         "enrollment not found",
			enrollmentID: "invalid-enrollment-id",
			expectError:  ErrMFAEnrollmentNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			var enrollmentID string
			if tt.setup != nil {
				enrollmentID = tt.setup(t, m)
			} else {
				enrollmentID = tt.enrollmentID
			}

			err = m.CancelEnrollment(context.Background(), 123, "tenant-123", enrollmentID)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBeginChallenge tests starting an MFA challenge
func TestBeginChallenge(t *testing.T) {
	tests := []struct {
		name         string
		userID       uint64
		tenantID     string
		sessionToken string
		setup        func(m *DefaultMFAManager)
		expectError  error
		validate     func(t *testing.T, challenge *MFAChallenge)
	}{
		{
			name:         "successful challenge",
			userID:       123,
			tenantID:     "tenant-123",
			sessionToken: "session-token",
			setup: func(m *DefaultMFAManager) {
				// Enroll MFA first
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)
			},
			expectError: nil,
			validate: func(t *testing.T, challenge *MFAChallenge) {
				assert.NotEmpty(t, challenge.ID)
				assert.Equal(t, uint64(123), challenge.UserID)
				assert.Equal(t, "tenant-123", challenge.TenantID)
				assert.Equal(t, "session-token", challenge.SessionToken)
				assert.Equal(t, MFAMethodTOTP, challenge.Method)
				assert.True(t, challenge.ExpiresAt.After(time.Now()))
				assert.Equal(t, 0, challenge.Attempts)
				assert.Equal(t, 3, challenge.MaxAttempts)
			},
		},
		{
			name:         "missing user ID",
			userID:       0,
			tenantID:     "tenant-123",
			sessionToken: "session-token",
			expectError:  ErrMFAInvalid,
		},
		{
			name:         "missing tenant ID",
			tenantID:     "",
			sessionToken: "session-token",
			expectError:  ErrMFAInvalid,
		},
		{
			name:         "missing session token",
			userID:       123,
			tenantID:     "tenant-123",
			sessionToken: "",
			expectError:  ErrMFAInvalid,
		},
		{
			name:         "MFA not enrolled",
			userID:       123,
			tenantID:     "tenant-123",
			sessionToken: "session-token",
			expectError:  ErrMFANotEnrolled,
		},
		{
			name:         "MFA not verified",
			userID:       123,
			tenantID:     "tenant-123",
			sessionToken: "session-token",
			setup: func(m *DefaultMFAManager) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  false, // not verified
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)
			},
			expectError: ErrMFANotEnrolled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(m)
			}

			challenge, err := m.BeginChallenge(context.Background(), tt.userID, tt.tenantID, tt.sessionToken)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
				assert.Nil(t, challenge)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, challenge)
				if tt.validate != nil {
					tt.validate(t, challenge)
				}
			}
		})
	}
}

// TestVerifyChallenge tests verifying an MFA challenge
func TestVerifyChallenge(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, m *DefaultMFAManager) (challengeID, code string)
		expectError error
		validate    func(t *testing.T, resp *MFAVerifyResponse)
		// after checks stored state once VerifyChallenge has returned.
		after func(t *testing.T, m *DefaultMFAManager, challengeID string)
	}{
		{
			name: "successful verification with TOTP",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				// Enroll MFA
				enrollResp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)
				validCode := generateValidTOTPCode(t, enrollResp.Secret, m.config)
				err = m.CompleteEnrollment(context.Background(), 123, "tenant-123", enrollResp.EnrollmentID, validCode)
				require.NoError(t, err)

				// Create challenge
				challenge, err := m.BeginChallenge(context.Background(), 123, "tenant-123", "session-token")
				require.NoError(t, err)

				// Get secret to generate valid code
				secret, err := m.storage.GetSecret(context.Background(), 123, "tenant-123")
				require.NoError(t, err)
				code := generateValidTOTPCode(t, secret.Secret, m.config)

				return challenge.ID, code
			},
			expectError: nil,
			validate: func(t *testing.T, resp *MFAVerifyResponse) {
				assert.True(t, resp.Success)
				assert.Equal(t, "session-token", resp.Token)
			},
			after: func(t *testing.T, m *DefaultMFAManager, challengeID string) {
				_, err := m.storage.GetChallenge(context.Background(), challengeID)
				assert.ErrorIs(t, err, ErrMFAChallengeNotFound, "a verified challenge is deleted")
			},
		},
		{
			name: "missing challenge ID",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				return "", "123456"
			},
			expectError: ErrMFAInvalid,
		},
		{
			name: "missing code",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				// Setup MFA
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				challenge, err := m.BeginChallenge(context.Background(), 123, "tenant-123", "session-token")
				require.NoError(t, err)

				return challenge.ID, ""
			},
			expectError: ErrMFACodeInvalid,
		},
		{
			name: "challenge not found",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				return "invalid-challenge-id", "123456"
			},
			expectError: ErrMFAChallengeNotFound,
		},
		{
			name: "expired challenge",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				challenge, err := m.BeginChallenge(context.Background(), 123, "tenant-123", "session-token")
				require.NoError(t, err)

				// Expire the challenge
				challenge.ExpiresAt = time.Now().Add(-1 * time.Minute)
				_ = m.storage.SaveChallenge(context.Background(), challenge)

				return challenge.ID, "123456"
			},
			expectError: ErrMFAChallengeExpired,
		},
		{
			name: "invalid code",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				challenge, err := m.BeginChallenge(context.Background(), 123, "tenant-123", "session-token")
				require.NoError(t, err)

				return challenge.ID, "000000" // invalid code
			},
			expectError: ErrMFACodeInvalid,
			after: func(t *testing.T, m *DefaultMFAManager, challengeID string) {
				challenge, err := m.storage.GetChallenge(context.Background(), challengeID)
				require.NoError(t, err)
				assert.Equal(t, 1, challenge.Attempts, "a wrong code counts an attempt")
			},
		},
		{
			name: "no enrolled secret and no backup codes",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				challenge := &MFAChallenge{
					ID:           "challenge-1",
					UserID:       123,
					TenantID:     "tenant-123",
					SessionToken: "session-token",
					Method:       MFAMethodTOTP,
					ExpiresAt:    time.Now().Add(5 * time.Minute),
					MaxAttempts:  3,
				}
				require.NoError(t, m.storage.SaveChallenge(context.Background(), challenge))
				return challenge.ID, "123456"
			},
			expectError: ErrMFACodeInvalid,
			after: func(t *testing.T, m *DefaultMFAManager, challengeID string) {
				challenge, err := m.storage.GetChallenge(context.Background(), challengeID)
				require.NoError(t, err)
				assert.Equal(t, 1, challenge.Attempts, "a failed lookup counts an attempt")
			},
		},
		{
			name: "max attempts exceeded",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				challenge, err := m.BeginChallenge(context.Background(), 123, "tenant-123", "session-token")
				require.NoError(t, err)

				// Exhaust attempts
				challenge.Attempts = challenge.MaxAttempts
				_ = m.storage.SaveChallenge(context.Background(), challenge)

				return challenge.ID, "123456"
			},
			expectError: ErrMFAMaxAttemptsExceeded,
		},
		{
			name: "successful verification with backup code",
			setup: func(t *testing.T, m *DefaultMFAManager) (string, string) {
				// Enroll MFA
				enrollResp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)
				validCode := generateValidTOTPCode(t, enrollResp.Secret, m.config)
				err = m.CompleteEnrollment(context.Background(), 123, "tenant-123", enrollResp.EnrollmentID, validCode)
				require.NoError(t, err)

				// Generate backup codes
				codes, err := m.GenerateBackupCodes(context.Background(), 123, "tenant-123")
				require.NoError(t, err)

				// Create challenge
				challenge, err := m.BeginChallenge(context.Background(), 123, "tenant-123", "session-token")
				require.NoError(t, err)

				return challenge.ID, codes[0]
			},
			expectError: nil,
			validate: func(t *testing.T, resp *MFAVerifyResponse) {
				assert.True(t, resp.Success)
				assert.Equal(t, "session-token", resp.Token)
				assert.Equal(t, 9, resp.BackupCodesRemaining) // 10 - 1 used
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			challengeID, code := tt.setup(t, m)

			resp, err := m.VerifyChallenge(context.Background(), 123, "tenant-123", challengeID, code)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
			if tt.after != nil {
				tt.after(t, m, challengeID)
			}
		})
	}
}

// TestVerifyCode tests verifying a TOTP code
func TestVerifyCode(t *testing.T) {
	tests := []struct {
		name        string
		userID      uint64
		tenantID    string
		code        string
		setup       func(t *testing.T, m *DefaultMFAManager) string
		expectError error
		// after checks stored state once VerifyCode has returned.
		after func(t *testing.T, m *DefaultMFAManager)
	}{
		{
			name:     "successful verification",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(t *testing.T, m *DefaultMFAManager) string {
				enrollResp, err := m.BeginEnrollment(context.Background(), 123, "tenant-123", MFAMethodTOTP)
				require.NoError(t, err)
				validCode := generateValidTOTPCode(t, enrollResp.Secret, m.config)
				err = m.CompleteEnrollment(context.Background(), 123, "tenant-123", enrollResp.EnrollmentID, validCode)
				require.NoError(t, err)

				secret, err := m.storage.GetSecret(context.Background(), 123, "tenant-123")
				require.NoError(t, err)
				return generateValidTOTPCode(t, secret.Secret, m.config)
			},
			expectError: nil,
			after: func(t *testing.T, m *DefaultMFAManager) {
				secret, err := m.storage.GetSecret(context.Background(), 123, "tenant-123")
				require.NoError(t, err)
				assert.NotNil(t, secret.LastUsed, "a verified code records its use")
			},
		},
		{
			name:        "missing user ID",
			userID:      0,
			tenantID:    "tenant-123",
			code:        "123456",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "missing tenant ID",
			userID:      123,
			tenantID:    "",
			code:        "123456",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "missing code",
			userID:      123,
			tenantID:    "tenant-123",
			code:        "",
			expectError: ErrMFACodeInvalid,
		},
		{
			name:        "MFA not enrolled",
			userID:      123,
			tenantID:    "tenant-123",
			code:        "123456",
			expectError: ErrMFANotEnrolled,
		},
		{
			name:     "MFA not verified",
			userID:   123,
			tenantID: "tenant-123",
			code:     "123456",
			setup: func(t *testing.T, m *DefaultMFAManager) string {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  false,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)
				return "123456"
			},
			expectError: ErrMFANotEnrolled,
		},
		{
			name:     "invalid code",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(t *testing.T, m *DefaultMFAManager) string {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)
				return "000000"
			},
			expectError: ErrMFACodeInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			var code string
			if tt.setup != nil {
				code = tt.setup(t, m)
			} else {
				code = tt.code
			}

			err = m.VerifyCode(context.Background(), tt.userID, tt.tenantID, code)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
			} else {
				assert.NoError(t, err)
			}
			if tt.after != nil {
				tt.after(t, m)
			}
		})
	}
}

// TestDisableMFA tests disabling MFA
func TestDisableMFA(t *testing.T) {
	tests := []struct {
		name        string
		userID      uint64
		tenantID    string
		setup       func(t *testing.T, m *DefaultMFAManager)
		expectError error
		// after checks stored state once DisableMFA has returned.
		after func(t *testing.T, m *DefaultMFAManager)
	}{
		{
			name:     "successful disable removes secret, enrollment, challenge and backup codes",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(t *testing.T, m *DefaultMFAManager) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				require.NoError(t, m.storage.SaveSecret(context.Background(), secret))

				enrollment := &MFAEnrollment{
					ID:          "enrollment-1",
					UserID:      123,
					TenantID:    "tenant-123",
					Method:      MFAMethodTOTP,
					Secret:      "test-secret",
					ExpiresAt:   time.Now().Add(10 * time.Minute),
					MaxAttempts: 3,
				}
				require.NoError(t, m.storage.SaveEnrollment(context.Background(), enrollment))

				challenge := &MFAChallenge{
					ID:           "challenge-1",
					UserID:       123,
					TenantID:     "tenant-123",
					SessionToken: "session-token",
					Method:       MFAMethodTOTP,
					ExpiresAt:    time.Now().Add(5 * time.Minute),
					MaxAttempts:  3,
				}
				require.NoError(t, m.storage.SaveChallenge(context.Background(), challenge))

				_, err := m.GenerateBackupCodes(context.Background(), 123, "tenant-123")
				require.NoError(t, err)
			},
			expectError: nil,
			after: func(t *testing.T, m *DefaultMFAManager) {
				_, err := m.storage.GetSecret(context.Background(), 123, "tenant-123")
				assert.ErrorIs(t, err, ErrMFANotEnrolled)

				_, err = m.storage.GetEnrollment(context.Background(), "enrollment-1")
				assert.ErrorIs(t, err, ErrMFAEnrollmentNotFound)

				_, err = m.storage.GetChallenge(context.Background(), "challenge-1")
				assert.ErrorIs(t, err, ErrMFAChallengeNotFound)

				_, err = m.storage.GetBackupCodes(context.Background(), 123, "tenant-123")
				assert.ErrorIs(t, err, ErrMFANotEnrolled)

				status, err := m.GetMFAStatus(context.Background(), 123, "tenant-123")
				require.NoError(t, err)
				assert.False(t, status.Enabled)
			},
		},
		{
			name:        "missing user ID",
			userID:      0,
			tenantID:    "tenant-123",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "missing tenant ID",
			userID:      123,
			tenantID:    "",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "MFA not enrolled",
			userID:      123,
			tenantID:    "tenant-123",
			expectError: nil, // DisableMFA doesn't error if not enrolled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(t, m)
			}

			err = m.DisableMFA(context.Background(), tt.userID, tt.tenantID)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
			} else {
				assert.NoError(t, err)
			}
			if tt.after != nil {
				tt.after(t, m)
			}
		})
	}
}

// TestGetMFAStatus tests getting MFA status
func TestGetMFAStatus(t *testing.T) {
	tests := []struct {
		name        string
		userID      uint64
		tenantID    string
		setup       func(m *DefaultMFAManager)
		expectError error
		validate    func(t *testing.T, status *MFAStatus)
	}{
		{
			name:     "MFA enabled",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) {
				now := time.Now()
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   now,
					LastUsed:  &now,
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				// Add backup codes
				backupSet := &BackupCodeSet{
					ID:       "backup-1",
					UserID:   123,
					TenantID: "tenant-123",
					Codes: []BackupCode{
						{Code: "code1"},
						{Code: "code2"},
						{Code: "code3", UsedAt: &now},
					},
					Created: now,
				}
				_ = m.storage.SaveBackupCodes(context.Background(), backupSet)
			},
			expectError: nil,
			validate: func(t *testing.T, status *MFAStatus) {
				assert.True(t, status.Enabled)
				assert.Equal(t, MFAMethodTOTP, status.Method)
				assert.NotNil(t, status.EnrolledAt)
				assert.NotNil(t, status.LastVerified)
				assert.Equal(t, 3, status.BackupCodesTotal)
				assert.Equal(t, 2, status.BackupCodesRemaining)
			},
		},
		{
			name:        "MFA not enabled",
			userID:      123,
			tenantID:    "tenant-123",
			expectError: nil,
			validate: func(t *testing.T, status *MFAStatus) {
				assert.False(t, status.Enabled)
			},
		},
		{
			name:        "missing user ID",
			userID:      0,
			tenantID:    "tenant-123",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "missing tenant ID",
			userID:      123,
			tenantID:    "",
			expectError: ErrMFAInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(m)
			}

			status, err := m.GetMFAStatus(context.Background(), tt.userID, tt.tenantID)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
				assert.Nil(t, status)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, status)
				if tt.validate != nil {
					tt.validate(t, status)
				}
			}
		})
	}
}

// TestIsMFARequired tests checking if MFA is required
func TestIsMFARequired(t *testing.T) {
	tests := []struct {
		name     string
		userID   uint64
		tenantID string
		setup    func(m *DefaultMFAManager)
		expected bool
	}{
		{
			name:     "MFA enrolled and verified",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)
			},
			expected: true,
		},
		{
			name:     "MFA not enrolled",
			userID:   123,
			tenantID: "tenant-123",
			expected: false,
		},
		{
			name:     "MFA not verified",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  false,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(m)
			}

			required, err := m.IsMFARequired(context.Background(), tt.userID, tt.tenantID)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, required)
		})
	}
}

// TestGenerateBackupCodes tests generating backup codes
func TestGenerateBackupCodes(t *testing.T) {
	tests := []struct {
		name        string
		userID      uint64
		tenantID    string
		setup       func(m *DefaultMFAManager)
		expectError error
		validate    func(t *testing.T, codes []string, m *DefaultMFAManager)
	}{
		{
			name:     "successful generation",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)
			},
			expectError: nil,
			validate: func(t *testing.T, codes []string, m *DefaultMFAManager) {
				assert.Len(t, codes, m.config.BackupCodes)
			},
		},
		{
			name:        "missing user ID",
			userID:      0,
			tenantID:    "tenant-123",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "missing tenant ID",
			userID:      123,
			tenantID:    "",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "MFA not enrolled",
			userID:      123,
			tenantID:    "tenant-123",
			expectError: ErrMFANotEnrolled,
		},
		{
			name:     "MFA not verified",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  false,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)
			},
			expectError: ErrMFANotEnrolled,
		},
		{
			name:     "regeneration",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				// Create existing backup codes
				backupSet := &BackupCodeSet{
					ID:       "backup-1",
					UserID:   123,
					TenantID: "tenant-123",
					Codes:    []BackupCode{{Code: "old-code"}},
					Created:  time.Now().Add(-1 * time.Hour),
				}
				_ = m.storage.SaveBackupCodes(context.Background(), backupSet)
			},
			expectError: nil,
			validate: func(t *testing.T, codes []string, m *DefaultMFAManager) {
				assert.Len(t, codes, m.config.BackupCodes)

				// Verify regenerated timestamp is set
				backupSet, err := m.storage.GetBackupCodes(context.Background(), 123, "tenant-123")
				assert.NoError(t, err)
				assert.NotNil(t, backupSet.Regenerated)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(m)
			}

			codes, err := m.GenerateBackupCodes(context.Background(), tt.userID, tt.tenantID)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
				assert.Nil(t, codes)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, codes)
				if tt.validate != nil {
					tt.validate(t, codes, m)
				}
			}
		})
	}
}

// TestVerifyBackupCode tests verifying backup codes
func TestVerifyBackupCode(t *testing.T) {
	tests := []struct {
		name        string
		userID      uint64
		tenantID    string
		code        string
		setup       func(m *DefaultMFAManager) []string
		expectError error
		validate    func(t *testing.T, m *DefaultMFAManager)
	}{
		{
			name:     "successful verification",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) []string {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				codes, _ := m.GenerateBackupCodes(context.Background(), 123, "tenant-123")
				return codes
			},
			expectError: nil,
			validate: func(t *testing.T, m *DefaultMFAManager) {
				backupSet, err := m.storage.GetBackupCodes(context.Background(), 123, "tenant-123")
				assert.NoError(t, err)
				assert.Equal(t, 9, backupSet.RemainingCodes()) // 10 - 1 used
			},
		},
		{
			name:        "missing user ID",
			userID:      0,
			tenantID:    "tenant-123",
			code:        "AAAA-AAAA",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "missing tenant ID",
			code:        "AAAA-AAAA",
			userID:      123,
			tenantID:    "",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "missing code",
			userID:      123,
			tenantID:    "tenant-123",
			code:        "",
			expectError: ErrMFABackupCodeInvalid,
		},
		{
			name:     "invalid code",
			userID:   123,
			tenantID: "tenant-123",
			code:     "INVALID-CODE",
			setup: func(m *DefaultMFAManager) []string {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				codes, _ := m.GenerateBackupCodes(context.Background(), 123, "tenant-123")
				return codes
			},
			expectError: ErrMFABackupCodeInvalid,
		},
		{
			name:     "already used code",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) []string {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				codes, _ := m.GenerateBackupCodes(context.Background(), 123, "tenant-123")
				// Use the first code
				_ = m.VerifyBackupCode(context.Background(), 123, "tenant-123", codes[0])
				return codes
			},
			expectError: ErrMFABackupCodeInvalid,
		},
		{
			name:     "exhausted codes",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) []string {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				// Create backup set with all codes used
				now := time.Now()
				hash := sha256.Sum256([]byte("test-code"))
				backupSet := &BackupCodeSet{
					ID:       "backup-1",
					UserID:   123,
					TenantID: "tenant-123",
					Codes: []BackupCode{
						{Code: hex.EncodeToString(hash[:]), UsedAt: &now},
					},
					Created: now,
				}
				_ = m.storage.SaveBackupCodes(context.Background(), backupSet)
				return nil
			},
			code:        "test-code",
			expectError: ErrMFABackupCodesExhausted,
		},
		{
			name:        "backup codes not generated",
			userID:      123,
			tenantID:    "tenant-123",
			code:        "AAAA-AAAA",
			expectError: ErrMFANotEnrolled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			var code string
			if tt.setup != nil {
				codes := tt.setup(m)
				if len(codes) > 0 && tt.code == "" {
					code = codes[0]
				}
			}
			if tt.code != "" {
				code = tt.code
			}

			err = m.VerifyBackupCode(context.Background(), tt.userID, tt.tenantID, code)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, m)
				}
			}
		})
	}
}

// TestGetBackupCodeStatus tests getting backup code status
func TestGetBackupCodeStatus(t *testing.T) {
	tests := []struct {
		name        string
		userID      uint64
		tenantID    string
		setup       func(m *DefaultMFAManager)
		expectError error
		validate    func(t *testing.T, status *BackupCodeStatus)
	}{
		{
			name:     "success",
			userID:   123,
			tenantID: "tenant-123",
			setup: func(m *DefaultMFAManager) {
				secret := &MFASecret{
					ID:        "secret-1",
					UserID:    123,
					TenantID:  "tenant-123",
					Secret:    testTOTPSecret,
					Algorithm: defaultMFAAlgorithm(),
					Digits:    6,
					Period:    30,
					Verified:  true,
					Created:   time.Now(),
				}
				_ = m.storage.SaveSecret(context.Background(), secret)

				now := time.Now()
				hash := sha256.Sum256([]byte("test-code"))
				backupSet := &BackupCodeSet{
					ID:       "backup-1",
					UserID:   123,
					TenantID: "tenant-123",
					Codes: []BackupCode{
						{Code: hex.EncodeToString(hash[:])},
						{Code: hex.EncodeToString(hash[:])},
						{Code: hex.EncodeToString(hash[:]), UsedAt: &now},
					},
					Created: now,
				}
				_ = m.storage.SaveBackupCodes(context.Background(), backupSet)
			},
			expectError: nil,
			validate: func(t *testing.T, status *BackupCodeStatus) {
				assert.Equal(t, 3, status.Total)
				assert.Equal(t, 2, status.Remaining)
				assert.Equal(t, 1, status.Used)
			},
		},
		{
			name:        "missing user ID",
			userID:      0,
			tenantID:    "tenant-123",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "missing tenant ID",
			userID:      123,
			tenantID:    "",
			expectError: ErrMFAInvalid,
		},
		{
			name:        "backup codes not generated",
			userID:      123,
			tenantID:    "tenant-123",
			expectError: ErrMFANotEnrolled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMFAManager()
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(m)
			}

			status, err := m.GetBackupCodeStatus(context.Background(), tt.userID, tt.tenantID)

			if tt.expectError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectError)
				assert.Nil(t, status)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, status)
				if tt.validate != nil {
					tt.validate(t, status)
				}
			}
		})
	}
}

// TestGenerateBackupCode tests the backup code generation function
func TestGenerateBackupCode(t *testing.T) {
	// Generate multiple codes to ensure they're unique and properly formatted
	codes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		code, err := generateBackupCode()
		assert.NoError(t, err)
		assert.Len(t, code, 9) // XXXX-XXXX format
		assert.Equal(t, "-", code[4:5])

		// Ensure uniqueness
		assert.False(t, codes[code], "duplicate code generated: %s", code)
		codes[code] = true

		// Verify characters are from allowed charset
		for _, ch := range code {
			if ch != '-' {
				assert.Contains(t, "ABCDEFGHJKLMNPQRSTUVWXYZ23456789", string(ch))
			}
		}
	}
}

// TestContextCancellation tests that MFA operations respect context cancellation
func TestMFAContextCancellation(t *testing.T) {
	m, err := NewMFAManager()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Test BeginEnrollment
	_, err = m.BeginEnrollment(ctx, 123, "tenant-123", MFAMethodTOTP)
	assert.Error(t, err)

	// Test BeginChallenge
	_, err = m.BeginChallenge(ctx, 123, "tenant-123", "session-token")
	assert.Error(t, err)

	// Test GetMFAStatus - returns status (not error) when storage fails
	// This is acceptable since it's a read-only status check
	status, err := m.GetMFAStatus(ctx, 123, "tenant-123")
	assert.NoError(t, err)
	assert.False(t, status.Enabled) // No MFA when context cancelled

	// Test VerifyCode
	err = m.VerifyCode(ctx, 123, "tenant-123", "123456")
	assert.Error(t, err)

	// Test DisableMFA
	err = m.DisableMFA(ctx, 123, "tenant-123")
	assert.Error(t, err)

	// Test GenerateBackupCodes
	_, err = m.GenerateBackupCodes(ctx, 123, "tenant-123")
	assert.Error(t, err)

	// Test VerifyBackupCode
	err = m.VerifyBackupCode(ctx, 123, "tenant-123", "AAAA-AAAA")
	assert.Error(t, err)

	// Test GetBackupCodeStatus
	_, err = m.GetBackupCodeStatus(ctx, 123, "tenant-123")
	assert.Error(t, err)
}
