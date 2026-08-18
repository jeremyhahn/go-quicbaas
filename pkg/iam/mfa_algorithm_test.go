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

// TestConvertAlgorithmSHA1OutsideFIPS verifies SHA1 - and the empty and
// unrecognized resting values that historically meant SHA1 - still resolve to
// HMAC-SHA1 when the process is not in FIPS mode. No HMAC is computed here, so
// this is safe to assert in either runtime.
func TestConvertAlgorithmSHA1OutsideFIPS(t *testing.T) {
	original := fipsEnabled
	fipsEnabled = func() bool { return false }
	t.Cleanup(func() { fipsEnabled = original })

	for _, input := range []MFAAlgorithm{MFAAlgorithmSHA1, "", "INVALID"} {
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

	for _, input := range []MFAAlgorithm{MFAAlgorithmSHA1, "", "INVALID"} {
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
