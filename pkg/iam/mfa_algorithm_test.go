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
	"crypto/fips140"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-auth/pkg/otp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceFIPSPolicy makes the algorithm policy behave as it does in FIPS 140-3
// mode for the duration of the test, so both policies are covered on any host.
//
// Only true is offered on purpose: a test that forces the policy off and then
// computes an HMAC-SHA1 code would panic in a FIPS-only runtime.
func forceFIPSPolicy(t *testing.T) {
	t.Helper()
	original := fipsEnabled
	fipsEnabled = func() bool { return true }
	t.Cleanup(func() { fipsEnabled = original })
}

// wantDefaultMFAAlgorithm is the algorithm a defaulted configuration must carry
// in the mode this test binary runs in: SHA256 under FIPS, the RFC 6238
// baseline SHA1 otherwise. Derived from the runtime flag rather than from
// defaultMFAAlgorithm so the expectation is independent of the code it checks.
func wantDefaultMFAAlgorithm() MFAAlgorithm {
	if fips140.Enabled() {
		return MFAAlgorithmSHA256
	}
	return MFAAlgorithmSHA1
}

// TestDefaultMFAAlgorithm verifies the default enrollment algorithm matches the
// mode the process is running in.
func TestDefaultMFAAlgorithm(t *testing.T) {
	assert.Equal(t, wantDefaultMFAAlgorithm(), defaultMFAAlgorithm())

	forceFIPSPolicy(t)
	assert.Equal(t, MFAAlgorithmSHA256, defaultMFAAlgorithm(),
		"FIPS mode must not default new enrollments to HMAC-SHA1")
}

// TestConvertAlgorithmApproved verifies the algorithms allowed in every mode
// map onto their otp equivalents.
func TestConvertAlgorithmApproved(t *testing.T) {
	tests := []struct {
		input    MFAAlgorithm
		expected otp.Algorithm
	}{
		{MFAAlgorithmSHA256, otp.AlgorithmSHA256},
		{MFAAlgorithmSHA512, otp.AlgorithmSHA512},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result, err := convertAlgorithm("test.convert", tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)

			forceFIPSPolicy(t)
			result, err = convertAlgorithm("test.convert", tt.input)
			require.NoError(t, err, "%s is FIPS 140-3 approved", tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertAlgorithmSHA1OutsideFIPS verifies SHA1 - and the empty resting
// value that predates the field and means SHA1 - still resolve to HMAC-SHA1
// when the process is not in FIPS mode. No HMAC is computed here, so this is
// safe to assert in either runtime.
func TestConvertAlgorithmSHA1OutsideFIPS(t *testing.T) {
	original := fipsEnabled
	fipsEnabled = func() bool { return false }
	t.Cleanup(func() { fipsEnabled = original })

	for _, input := range []MFAAlgorithm{MFAAlgorithmSHA1, ""} {
		t.Run(string(input), func(t *testing.T) {
			result, err := convertAlgorithm("test.convert", input)
			require.NoError(t, err)
			assert.Equal(t, otp.AlgorithmSHA1, result)
		})
	}
}

// TestConvertAlgorithmRejectsSHA1UnderFIPS verifies a non-approved algorithm
// produces a typed error rather than reaching crypto/hmac, which panics on
// HMAC-SHA1 under GODEBUG=fips140=only.
func TestConvertAlgorithmRejectsSHA1UnderFIPS(t *testing.T) {
	forceFIPSPolicy(t)

	for _, input := range []MFAAlgorithm{MFAAlgorithmSHA1, ""} {
		t.Run(string(input), func(t *testing.T) {
			_, err := convertAlgorithm("test.convert", input)
			require.Error(t, err)

			var notApproved *MFAAlgorithmNotApprovedError
			require.True(t, errors.As(err, &notApproved), "want *MFAAlgorithmNotApprovedError, got %T", err)
			assert.Equal(t, MFAAlgorithmSHA1, notApproved.Algorithm)
			assert.Contains(t, notApproved.Error(), "re-enroll")
		})
	}
}

// TestBeginEnrollmentRejectsSHA1UnderFIPS verifies a deployment that pins SHA1
// in configuration is refused a new enrollment in FIPS mode instead of issuing
// a secret it could never verify.
func TestBeginEnrollmentRejectsSHA1UnderFIPS(t *testing.T) {
	m, err := NewMFAManager(WithMFAConfig(MFAConfig{Algorithm: MFAAlgorithmSHA1}))
	require.NoError(t, err)

	forceFIPSPolicy(t)

	resp, err := m.BeginEnrollment(context.Background(), "user-123", "tenant-123", MFAMethodTOTP)
	require.Error(t, err)
	assert.Nil(t, resp)

	var notApproved *MFAAlgorithmNotApprovedError
	assert.True(t, errors.As(err, &notApproved), "want *MFAAlgorithmNotApprovedError, got %T", err)
}

// TestEnrollAndVerifyUsesModeAlgorithm walks a full enrollment and verification
// with the default algorithm for this runtime. Under GODEBUG=fips140=only that
// default is SHA256, so this is the end-to-end proof the MFA path works in FIPS
// mode without panicking.
func TestEnrollAndVerifyUsesModeAlgorithm(t *testing.T) {
	m, err := NewMFAManager()
	require.NoError(t, err)

	ctx := context.Background()
	enrollment, err := m.BeginEnrollment(ctx, "user-123", "tenant-123", MFAMethodTOTP)
	require.NoError(t, err)

	code := generateValidTOTPCode(t, enrollment.Secret, m.config)
	require.NoError(t, m.CompleteEnrollment(ctx, enrollment.EnrollmentID, code))

	stored, err := m.storage.GetSecret(ctx, "user-123", "tenant-123")
	require.NoError(t, err)
	assert.Equal(t, defaultMFAAlgorithm(), stored.Algorithm, "the algorithm must be pinned onto the stored secret")

	require.NoError(t, m.VerifyCode(ctx, "user-123", "tenant-123", generateValidTOTPCode(t, stored.Secret, m.config)))
}

// TestVerifyCodeStoredSHA1Secret covers the enrollment that already exists when
// the default changes: outside FIPS mode it keeps verifying against the pinned
// SHA1 it was provisioned with, and in FIPS mode it fails with the typed error
// that tells the operator the user must re-enroll - it never panics, and it is
// never silently downgraded.
func TestVerifyCodeStoredSHA1Secret(t *testing.T) {
	m, err := NewMFAManager(WithMFAConfig(MFAConfig{Algorithm: MFAAlgorithmSHA256}))
	require.NoError(t, err)

	ctx := context.Background()
	legacy := &MFASecret{
		ID:          "secret-legacy",
		UserID:      "user-legacy",
		TenantID:    "tenant-123",
		Secret:      testTOTPSecret,
		Algorithm:   MFAAlgorithmSHA1,
		Digits:      6,
		Period:      30,
		Issuer:      "QuicBaaS",
		AccountName: "legacy@quicbaas.io",
		Verified:    true,
		Created:     time.Now(),
	}
	require.NoError(t, m.storage.SaveSecret(ctx, legacy))

	if fips140.Enabled() {
		err = m.VerifyCode(ctx, "user-legacy", "tenant-123", "000000")
		require.Error(t, err)

		var notApproved *MFAAlgorithmNotApprovedError
		require.True(t, errors.As(err, &notApproved), "want *MFAAlgorithmNotApprovedError, got %T", err)
		assert.Equal(t, MFAAlgorithmSHA1, notApproved.Algorithm)
		return
	}

	code := generateValidTOTPCode(t, legacy.Secret, MFAConfig{
		Digits:    legacy.Digits,
		Period:    legacy.Period,
		Algorithm: legacy.Algorithm,
		Skew:      m.config.Skew,
	})
	require.NoError(t, m.VerifyCode(ctx, "user-legacy", "tenant-123", code),
		"an enrollment provisioned with SHA1 must keep verifying outside FIPS mode")
}

// TestConvertAlgorithmRejectsUnknown verifies an unrecognized identifier fails
// closed in every mode instead of acquiring SHA1's meaning.
func TestConvertAlgorithmRejectsUnknown(t *testing.T) {
	for _, input := range []MFAAlgorithm{"INVALID", "MD5", "SHA3-256"} {
		t.Run(string(input), func(t *testing.T) {
			_, err := convertAlgorithm("test.convert", input)
			require.Error(t, err)

			var unsupported *MFAAlgorithmUnsupportedError
			require.True(t, errors.As(err, &unsupported), "want *MFAAlgorithmUnsupportedError, got %T", err)
			assert.Equal(t, input, unsupported.Algorithm)
			assert.Contains(t, unsupported.Error(), "unsupported TOTP algorithm "+string(input))

			forceFIPSPolicy(t)
			_, err = convertAlgorithm("test.convert", input)
			require.Error(t, err)
			assert.True(t, errors.As(err, &unsupported))
		})
	}
}

// TestMFASecretValidateRejectsUnknownAlgorithm verifies a corrupted record
// cannot be persisted. An empty algorithm predates the field and stays valid.
func TestMFASecretValidateRejectsUnknownAlgorithm(t *testing.T) {
	secret := func(algo MFAAlgorithm) *MFASecret {
		return &MFASecret{
			ID: "secret-1", UserID: "user-123", TenantID: "tenant-123",
			Secret: testTOTPSecret, Algorithm: algo, Digits: 6, Period: 30,
		}
	}

	require.NoError(t, secret("").Validate(), "an empty algorithm is a legacy record, not a corrupt one")
	require.NoError(t, secret(MFAAlgorithmSHA1).Validate())
	require.NoError(t, secret(MFAAlgorithmSHA256).Validate())

	err := secret("MD5").Validate()
	require.Error(t, err)
	var unsupported *MFAAlgorithmUnsupportedError
	assert.True(t, errors.As(err, &unsupported), "want *MFAAlgorithmUnsupportedError, got %T", err)
}

// TestCompleteEnrollmentUsesProvisionedParameters verifies a pending enrollment
// is completed against the parameters encoded in the URI the authenticator
// scanned, not against a configuration that changed in the meantime - a
// restart, a rolling deployment, or a move into FIPS mode during the ten
// minute enrollment window.
func TestCompleteEnrollmentUsesProvisionedParameters(t *testing.T) {
	storage := NewMemoryMFAStorage()
	ctx := context.Background()

	provisioning := MFAConfig{Issuer: "QuicBaaS", Digits: 6, Period: 30, Algorithm: MFAAlgorithmSHA256, Skew: 1, BackupCodes: 10}
	issuer, err := NewMFAManager(WithMFAStorage(storage), WithMFAConfig(provisioning))
	require.NoError(t, err)

	enrollment, err := issuer.BeginEnrollment(ctx, "user-123", "tenant-123", MFAMethodTOTP)
	require.NoError(t, err)
	code := generateValidTOTPCode(t, enrollment.Secret, provisioning)

	// A second manager over the same store, configured differently.
	completer, err := NewMFAManager(WithMFAStorage(storage), WithMFAConfig(MFAConfig{
		Issuer: "QuicBaaS", Digits: 8, Period: 60, Algorithm: MFAAlgorithmSHA512, Skew: 1, BackupCodes: 10,
	}))
	require.NoError(t, err)

	require.NoError(t, completer.CompleteEnrollment(ctx, enrollment.EnrollmentID, code),
		"the code the authenticator was provisioned to produce must be accepted")

	stored, err := storage.GetSecret(ctx, "user-123", "tenant-123")
	require.NoError(t, err)
	assert.Equal(t, MFAAlgorithmSHA256, stored.Algorithm)
	assert.Equal(t, 6, stored.Digits)
	assert.Equal(t, 30, stored.Period)
}

// TestCompleteEnrollmentLegacyEnrollmentUsesConfig verifies an enrollment
// persisted before the provisioning parameters were recorded still completes,
// using the manager's configuration as it did before.
func TestCompleteEnrollmentLegacyEnrollmentUsesConfig(t *testing.T) {
	m, err := NewMFAManager()
	require.NoError(t, err)

	ctx := context.Background()
	legacy := &MFAEnrollment{
		ID:          "enrollment-legacy",
		UserID:      "user-123",
		TenantID:    "tenant-123",
		Method:      MFAMethodTOTP,
		Secret:      testTOTPSecret,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		MaxAttempts: 5,
		Created:     time.Now(),
	}
	require.NoError(t, m.storage.SaveEnrollment(ctx, legacy))

	code := generateValidTOTPCode(t, legacy.Secret, m.config)
	require.NoError(t, m.CompleteEnrollment(ctx, legacy.ID, code))

	stored, err := m.storage.GetSecret(ctx, "user-123", "tenant-123")
	require.NoError(t, err)
	assert.Equal(t, m.config.Algorithm, stored.Algorithm)
}

// TestVerifyChallengeSurfacesAlgorithmRejection verifies the authentication API
// - not just VerifyCode - reports the re-enrollment requirement, and that the
// refusal does not consume one of the user's verification attempts.
func TestVerifyChallengeSurfacesAlgorithmRejection(t *testing.T) {
	m, err := NewMFAManager(WithMFAConfig(MFAConfig{Algorithm: MFAAlgorithmSHA256}))
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, m.storage.SaveSecret(ctx, &MFASecret{
		ID: "secret-legacy", UserID: "user-legacy", TenantID: "tenant-123",
		Secret: testTOTPSecret, Algorithm: MFAAlgorithmSHA1, Digits: 6, Period: 30,
		Issuer: "QuicBaaS", AccountName: "legacy@quicbaas.io", Verified: true, Created: time.Now(),
	}))

	challenge, err := m.BeginChallenge(ctx, "user-legacy", "tenant-123", "session-token")
	require.NoError(t, err)

	forceFIPSPolicy(t)

	resp, err := m.VerifyChallenge(ctx, challenge.ID, "000000")
	require.Error(t, err)
	assert.Nil(t, resp)

	var notApproved *MFAAlgorithmNotApprovedError
	require.True(t, errors.As(err, &notApproved), "want *MFAAlgorithmNotApprovedError, got %T", err)
	assert.NotErrorIs(t, err, ErrMFACodeInvalid, "a policy refusal must not masquerade as a wrong code")

	unchanged, err := m.storage.GetChallenge(ctx, challenge.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, unchanged.Attempts, "a policy refusal must not consume an attempt")
}
