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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jeremyhahn/go-auth/pkg/otp"
	"github.com/jeremyhahn/go-enclave"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The TOTP shared secret is a long-lived key. It reaches the OTP library only
// as Config.Seed, a go-enclave locked region the authenticator borrows for the
// moment a code is derived; the library has no plaintext string path at all.
//
// The tests below pin three things: that the enclave derives the same codes an
// authenticator app enrolled before the change produces, that a corrupt stored
// secret cannot become a key, and that the enclave is destroyed rather than
// left in locked memory for the process lifetime.

// newProtectedSecretTestManager returns a manager with in-memory storage and
// the default TOTP parameters.
func newProtectedSecretTestManager(t *testing.T) *DefaultMFAManager {
	t.Helper()
	m, err := NewMFAManager(WithMFAConfig(MFAConfig{Issuer: "QuicBaaS"}))
	require.NoError(t, err)
	return m
}

// TestProtectedSharedSecretDecodesIntoEnclave covers the success path of
// protectedSharedSecret: a stored base32 secret becomes a sealed key of the
// right size, and an authenticator built on that key derives exactly the code
// an authenticator app derives from the same base32 text.
func TestProtectedSharedSecretDecodesIntoEnclave(t *testing.T) {
	key, err := protectedSharedSecret("test.protected", testTOTPSecret)
	require.NoError(t, err)
	require.NotNil(t, key)

	// 32 base32 characters are 20 bytes: the secret was decoded into the
	// enclave, not stored as its encoded text.
	assert.Equal(t, 20, key.Size())

	algorithm, err := convertAlgorithm("test.protected", MFAAlgorithmSHA1)
	require.NoError(t, err)

	auth, err := otp.NewAuthenticator(otp.Config{
		Type:      otp.TypeTOTP,
		Seed:      key,
		Digits:    6,
		Period:    30,
		Algorithm: algorithm,
	})
	require.NoError(t, err)

	// Generate samples its own clock -- for TOTP it ignores the counter
	// argument entirely -- so the instant it used can only be bounded, not
	// pinned. Bracketing it does that: fromEnclave must be the app's code for
	// one of the steps the two reads span, which is a single code except when
	// a 30-second boundary falls between them. Deriving the expected code from
	// a fresh time.Now() instead compared adjacent counters whenever the clock
	// crossed a boundary here, and failed while the behaviour was correct.
	before := time.Now().UTC()
	fromEnclave, err := auth.Generate()
	require.NoError(t, err)
	after := time.Now().UTC()

	fromApp := totpCodesForBracket(t, testTOTPSecret, MFAConfig{
		Digits:    6,
		Period:    30,
		Algorithm: MFAAlgorithmSHA1,
	}, before, after)
	assert.Contains(t, fromApp, fromEnclave,
		"the sealed key must derive the same code the enrolled authenticator app shows")

	// The authenticator owns the seed and releases it on Close: the locked
	// region is wiped, not merely left for the collector.
	require.True(t, key.IsAlive())
	require.NoError(t, auth.Close())
	assert.False(t, key.IsAlive(), "closing the authenticator must destroy the seed")
	assert.Equal(t, 0, key.Size())
}

// TestTOTPCodesForBracketCoversATimeStepBoundary pins the clock either side of
// a TOTP step boundary, so the boundary behaviour is tested deterministically
// rather than whenever a test run happens to land on one.
//
// It is the mutation evidence for the bracketing in
// TestProtectedSharedSecretDecodesIntoEnclave: the first assertion IS the
// failure that assertion used to have -- two adjacent steps produce different
// codes, so comparing a value from one step against a value derived a moment
// later in the next step fails while nothing is wrong. The third assertion is
// the other half, that bracketing does not degrade into accepting anything.
func TestTOTPCodesForBracketCoversATimeStepBoundary(t *testing.T) {
	config := MFAConfig{Digits: 6, Period: 30, Algorithm: MFAAlgorithmSHA1}

	// 1700000010 is a multiple of 30, so it is exactly a step boundary.
	boundary := time.Unix(1700000010, 0).UTC()
	require.Zero(t, boundary.Unix()%30, "the pinned instant must be a step boundary")

	beforeBoundary := boundary.Add(-time.Second)
	afterBoundary := boundary

	previous := generateTOTPCodeAt(t, testTOTPSecret, config, beforeBoundary)
	next := generateTOTPCodeAt(t, testTOTPSecret, config, afterBoundary)
	require.NotEqual(t, previous, next,
		"one second apart across a boundary must give different codes, otherwise this "+
			"test cannot detect the race it exists for")

	spanning := totpCodesForBracket(t, testTOTPSecret, config, beforeBoundary, afterBoundary)
	assert.ElementsMatch(t, []string{previous, next}, spanning,
		"a bracket spanning a boundary must accept the code from either side")

	within := totpCodesForBracket(t, testTOTPSecret, config, boundary, boundary.Add(29*time.Second))
	assert.Equal(t, []string{next}, within,
		"a bracket inside one step must stay exact, not accept a second code")
}

// TestProtectedSharedSecretRejectsMalformedSecret covers the error path of
// protectedSharedSecret: a stored value that is not base32 is reported as a
// typed IAM error tagged with the operation, not silently turned into a key.
func TestProtectedSharedSecretRejectsMalformedSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{name: "empty", secret: ""},
		{name: "whitespace only", secret: "   "},
		{name: "not base32", secret: "not a base32 secret!!"},
		{name: "base32 alphabet violation", secret: "JBSWY3DPEHPK3PX1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, err := protectedSharedSecret("iam.mfa.verify", tc.secret)
			require.Error(t, err)
			assert.Nil(t, key)
			assert.ErrorIs(t, err, otp.ErrInvalidConfig,
				"the decode failure must survive wrapping")
			assert.Contains(t, err.Error(), "iam.mfa.verify",
				"the error must name the operation that asked for the key")
		})
	}
}

// truncatedBase32Tails are the terminal group lengths that carry no complete
// byte. Base32 packs 5 bits per character, so a group of 1, 3 or 6 characters
// holds 5, 15 or 30 bits — fewer than the 8, 16 or 32 a whole number of bytes
// would need. Go's decoder discards those leftover bits instead of rejecting
// them, so appending such a tail to a valid secret decodes to the SAME key.
// The other remainders (2, 4, 5, 7) do carry complete bytes and are legitimate.
var truncatedBase32Tails = []struct {
	name string
	tail string
}{
	{name: "remainder 1", tail: "A"},
	{name: "remainder 3", tail: "AAA"},
	{name: "remainder 6", tail: "AAAAAA"},
}

// TestProtectedSharedSecretRejectsTruncatedBase32Tail covers a stored value
// that is well-formed base32 for its complete groups but ends in an incomplete
// one.
//
// This is not the alphabet check above: every character here is valid base32,
// and Go's unpadded decoder returns no error. It simply drops the leftover
// bits, so "<valid secret>A" yields exactly the bytes of "<valid secret>" —
// which would make two different persisted strings authenticate the same TOTP.
// A remainder of 1, 3 or 6 is not a recoverable encoding, it is a corrupt
// stored value, so it is refused rather than normalised.
func TestProtectedSharedSecretRejectsTruncatedBase32Tail(t *testing.T) {
	for _, tc := range truncatedBase32Tails {
		t.Run(tc.name, func(t *testing.T) {
			malformed := testTOTPSecret + tc.tail
			require.Contains(t, []int{1, 3, 6}, len(malformed)%8,
				"the test input must actually end in an incomplete group")

			key, err := protectedSharedSecret("iam.mfa.verify", malformed)
			require.Error(t, err, "an incomplete terminal group must not decode to a key")
			assert.Nil(t, key)
			assert.Contains(t, err.Error(), "iam.mfa.verify",
				"the error must name the operation that asked for the key")
		})
	}
}

// TestVerifyCodeRejectsStoredSecretWithTruncatedBase32Tail is the behavioural
// half: the code derived from the VALID PREFIX must not authenticate against a
// stored secret that merely begins with it.
//
// That clause is the whole point. A test that fed garbage in would pass
// against the unguarded decoder too, because garbage fails the alphabet check
// already. Here every character is valid base32 and the decoded key is byte
// identical to the real one, so before the length check this code verified
// successfully against a secret nobody enrolled.
func TestVerifyCodeRejectsStoredSecretWithTruncatedBase32Tail(t *testing.T) {
	ctx := context.Background()

	for _, tc := range truncatedBase32Tails {
		t.Run(tc.name, func(t *testing.T) {
			m := newProtectedSecretTestManager(t)
			stored := &MFASecret{
				ID:          uuid.New().String(),
				UserID:      40050,
				TenantID:    "tenant-truncated",
				Secret:      testTOTPSecret + tc.tail,
				Algorithm:   MFAAlgorithmSHA1,
				Digits:      6,
				Period:      30,
				Issuer:      "QuicBaaS",
				AccountName: "truncated@example.com",
				Verified:    true,
				Created:     time.Now(),
			}
			require.NoError(t, m.storage.SaveSecret(ctx, stored))

			code := generateValidTOTPCode(t, testTOTPSecret, MFAConfig{
				Digits:    stored.Digits,
				Period:    stored.Period,
				Algorithm: stored.Algorithm,
			})

			err := m.VerifyCode(ctx, stored.UserID, stored.TenantID, code)
			require.Error(t, err,
				"a code for the valid prefix must not authenticate a malformed stored secret")
			assert.NotErrorIs(t, err, ErrMFACodeInvalid,
				"the stored secret is the fault, so the error must not blame the submitted code")
		})
	}
}

// storeCorruptSecretWithChallenge enrolls a user with a secret whose base32
// tail is incomplete, and opens a challenge against it. It is the state a
// truncated or concatenated stored value leaves behind.
func storeCorruptSecretWithChallenge(t *testing.T, m *DefaultMFAManager, stored string) *MFAChallenge {
	t.Helper()
	ctx := context.Background()

	require.NoError(t, m.storage.SaveSecret(ctx, &MFASecret{
		ID:          uuid.New().String(),
		UserID:      40003,
		TenantID:    "tenant-corrupt",
		Secret:      stored,
		Algorithm:   MFAAlgorithmSHA1,
		Digits:      6,
		Period:      30,
		Issuer:      "QuicBaaS",
		AccountName: "corrupt@example.com",
		Verified:    true,
		Created:     time.Now(),
	}))

	challenge, err := m.BeginChallenge(ctx, 40003, "tenant-corrupt", "session-token")
	require.NoError(t, err)
	return challenge
}

// TestVerifyChallengeSurfacesCorruptSecret is the challenge-path half of the
// base32 guard, and it is the path callers actually use.
//
// Asserting that VerifyCode refuses a corrupt secret is not enough:
// VerifyChallenge used to treat every VerifyCode error as a wrong code, so the
// refusal arrived at the caller as ErrMFACodeInvalid with one of the user's
// attempts spent. The operator saw a bad login, the user was rate-limited for
// a fault that was not theirs, and the corrupt enrollment stayed invisible.
func TestVerifyChallengeSurfacesCorruptSecret(t *testing.T) {
	// Every way a stored secret fails to become a key, not only the incomplete
	// terminal group. The length case is the one the decoder does not report;
	// the rest are what it does report, and they were falling through to the
	// backup-code fallback for as long as only the length case was typed.
	corruptions := []struct {
		name   string
		stored string
	}{
		{name: "remainder 1", stored: testTOTPSecret + "A"},
		{name: "remainder 3", stored: testTOTPSecret + "AAA"},
		{name: "remainder 6", stored: testTOTPSecret + "AAAAAA"},
		{name: "alphabet violation", stored: "JBSWY3DPEHPK3PX1JBSWY3DPEHPK3PX1"},
		{name: "not base32 at all", stored: "not a base32 secret!!"},
		// An empty secret is not reachable here: SaveSecret validates it away
		// ("secret is required"), so no challenge can exist against one.
		// TestProtectedSharedSecretRejectsMalformedSecret covers it directly.
	}

	for _, tc := range corruptions {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			m := newProtectedSecretTestManager(t)
			challenge := storeCorruptSecretWithChallenge(t, m, tc.stored)

			resp, err := m.VerifyChallenge(ctx, 40003, "tenant-corrupt", challenge.ID, "000000")
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.NotErrorIs(t, err, ErrMFACodeInvalid,
				"a corrupt stored secret must not masquerade as a wrong code")
			assert.Contains(t, err.Error(), "shared secret",
				"the error must say the stored secret is what is wrong")

			var malformed *MFASecretMalformedError
			assert.ErrorAs(t, err, &malformed,
				"every unusable stored secret must carry the same type, whichever check caught it")

			// The attempt counter is the brute-force defence and belongs to
			// wrong codes only: the user submitted nothing wrong here.
			unchanged, err := m.storage.GetChallenge(ctx, challenge.ID)
			require.NoError(t, err)
			assert.Zero(t, unchanged.Attempts,
				"an integrity fault must not consume one of the user's attempts")
		})
	}
}

// TestVerifyChallengeDoesNotHideCorruptSecretBehindBackupCode is the serious
// half. VerifyChallenge tried backup codes whenever VerifyCode failed, so a
// user holding a valid backup code authenticated successfully against a
// corrupt primary secret — and because the call succeeded, nothing ever
// reported that the stored TOTP secret was unusable. The guard was defeated at
// exactly the layer that matters.
func TestVerifyChallengeDoesNotHideCorruptSecretBehindBackupCode(t *testing.T) {
	ctx := context.Background()
	m := newProtectedSecretTestManager(t)

	// Enroll with a WELL-FORMED secret so backup codes can be issued, then
	// corrupt the stored secret underneath them.
	require.NoError(t, m.storage.SaveSecret(ctx, &MFASecret{
		ID: uuid.New().String(), UserID: 40003, TenantID: "tenant-corrupt",
		Secret: testTOTPSecret, Algorithm: MFAAlgorithmSHA1, Digits: 6, Period: 30,
		Issuer: "QuicBaaS", AccountName: "corrupt@example.com", Verified: true, Created: time.Now(),
	}))
	codes, err := m.GenerateBackupCodes(ctx, 40003, "tenant-corrupt")
	require.NoError(t, err)
	require.NotEmpty(t, codes)

	challenge := storeCorruptSecretWithChallenge(t, m, testTOTPSecret+"A")

	resp, err := m.VerifyChallenge(ctx, 40003, "tenant-corrupt", challenge.ID, codes[0])
	require.Error(t, err,
		"a valid backup code must not paper over a corrupt primary secret")
	// Unconditional: `if resp != nil { ... }` would silently skip the success
	// assertion on the very path where it matters most.
	assert.Nil(t, resp,
		"the challenge must not report success while the stored secret is unusable")
	assert.Contains(t, err.Error(), "shared secret",
		"the caller must learn the stored secret is the problem")
}

// TestVerifyChallengeKeepsBackupRecoveryForDisallowedAlgorithm guards the
// boundary of the corrupt-secret short-circuit above.
//
// A policy change that disallows an enrolled algorithm is an ADMINISTRATIVE
// event, not corruption: the stored secret is intact and backup codes are the
// sanctioned way for an already-enrolled user to get in and re-enroll. An
// earlier draft of the corrupt-secret fix returned before the backup-code
// fallback for every unusable enrollment, which turned a tenant enabling FIPS
// into a lockout for every SHA-1 enrollment that had backup codes.
//
// So the short-circuit is narrow by construction, and this test is what keeps
// it narrow: only a malformed stored secret may skip backup recovery.
func TestVerifyChallengeKeepsBackupRecoveryForDisallowedAlgorithm(t *testing.T) {
	ctx := context.Background()
	m := newProtectedSecretTestManager(t)

	require.NoError(t, m.storage.SaveSecret(ctx, &MFASecret{
		ID: uuid.New().String(), UserID: 40004, TenantID: "tenant-sha1",
		Secret: testTOTPSecret, Algorithm: MFAAlgorithmSHA1, Digits: 6, Period: 30,
		Issuer: "QuicBaaS", AccountName: "sha1@example.com", Verified: true, Created: time.Now(),
	}))
	codes, err := m.GenerateBackupCodes(ctx, 40004, "tenant-sha1")
	require.NoError(t, err)
	require.NotEmpty(t, codes)

	challenge, err := m.BeginChallenge(ctx, 40004, "tenant-sha1", "session-token")
	require.NoError(t, err)

	// The policy turns against the enrolled algorithm only now, with the
	// enrollment already in place — the migration case, not a fresh enrollment.
	// Derived while SHA-1 is still allowed, because the generator applies the
	// same policy and would refuse to produce it afterwards.
	totpCode := generateValidTOTPCode(t, testTOTPSecret, MFAConfig{
		Digits: 6, Period: 30, Algorithm: MFAAlgorithmSHA1,
	})

	forceFIPSPolicy(t)

	// PIN THE PRECONDITION, because the recovery assertion below cannot see
	// it. Measured: with forceFIPSPolicy removed this test still PASSED, since
	// a backup code recovers a merely-wrong TOTP code just as well as a
	// policy-refused one -- the outcome is right either way and says nothing
	// about which path produced it. Now, if the policy stops refusing SHA-1 or
	// the helper stops taking effect, this fails here instead of passing for
	// the wrong reason.
	//
	// It does not depend on totpCode still being current: VerifyCode rejects
	// the algorithm before it ever compares a code, so a step boundary between
	// deriving and submitting cannot affect this assertion.
	var notApproved *MFAAlgorithmNotApprovedError
	require.ErrorAs(t, m.VerifyCode(ctx, 40004, "tenant-sha1", totpCode), &notApproved,
		"the policy must actually be refusing this enrollment's algorithm, "+
			"or the backup-code recovery below is not testing the case it names")

	resp, err := m.VerifyChallenge(ctx, 40004, "tenant-sha1", challenge.ID, codes[0])
	require.NoError(t, err,
		"a backup code must still recover an enrollment whose algorithm the policy now refuses")
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

// TestDestroyedSharedSecretFailsClosed pins that the enclave is the only
// source of the key: once the authenticator that owns it is closed, it refuses
// a code that was correct a moment earlier rather than falling back to some
// other copy of the secret.
func TestDestroyedSharedSecretFailsClosed(t *testing.T) {
	ctx := context.Background()

	key, err := protectedSharedSecret("test.protected", testTOTPSecret)
	require.NoError(t, err)

	algorithm, err := convertAlgorithm("test.protected", MFAAlgorithmSHA1)
	require.NoError(t, err)

	auth, err := otp.NewAuthenticator(otp.Config{
		Type:      otp.TypeTOTP,
		Seed:      key,
		Digits:    6,
		Period:    30,
		Algorithm: algorithm,
	})
	require.NoError(t, err)

	code := generateValidTOTPCode(t, testTOTPSecret, MFAConfig{
		Digits:    6,
		Period:    30,
		Algorithm: MFAAlgorithmSHA1,
	})
	submitted, err := enclave.NewMemoryEnclaveFromString(code)
	require.NoError(t, err)
	defer submitted.Destroy()

	require.NoError(t, auth.Authenticate(ctx, submitted),
		"the code must verify while the key is alive")

	require.NoError(t, auth.Close())
	assert.False(t, key.IsAlive(), "closing the authenticator must destroy the seed it owns")
	assert.ErrorIs(t, auth.Authenticate(ctx, submitted), enclave.ErrEnclaveDestroyed,
		"a destroyed key must fail closed, not fall back to another copy of the secret")
}

// TestCheckTOTPCode covers the shared verification path: the right code
// verifies, a wrong code is ErrMFACodeInvalid, and a corrupt stored secret is
// a typed malformed-secret fault rather than a wrong code.
func TestCheckTOTPCode(t *testing.T) {
	ctx := context.Background()
	m := newProtectedSecretTestManager(t)
	provisioned := mfaProvisioningParams{Algorithm: MFAAlgorithmSHA1, Digits: 6, Period: 30}
	config := MFAConfig{Algorithm: MFAAlgorithmSHA1, Digits: 6, Period: 30}

	code := generateValidTOTPCode(t, testTOTPSecret, config)
	require.NoError(t, m.checkTOTPCode(ctx, "test.check", testTOTPSecret, code, provisioned))

	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	assert.True(t, IsMFACodeInvalid(m.checkTOTPCode(ctx, "test.check", testTOTPSecret, wrong, provisioned)))

	var malformed *MFASecretMalformedError
	err := m.checkTOTPCode(ctx, "test.check", testTOTPSecret+"A", code, provisioned)
	require.ErrorAs(t, err, &malformed)
	assert.False(t, IsMFACodeInvalid(err), "a corrupt stored secret is not a wrong code")
}

// TestVerifyCodeAcceptsSecretEnrolledBeforeProtectedMemory is the migration's
// interop proof. The stored record is written exactly as an enrollment
// completed before this change wrote it — the base32 shared secret as a plain
// string — and the code is derived by the authenticator app's implementation.
// A user enrolled the old way must keep authenticating.
func TestVerifyCodeAcceptsSecretEnrolledBeforeProtectedMemory(t *testing.T) {
	ctx := context.Background()
	m := newProtectedSecretTestManager(t)

	stored := &MFASecret{
		ID:          uuid.New().String(),
		UserID:      40002,
		TenantID:    "tenant-legacy",
		Secret:      testTOTPSecret,
		Algorithm:   MFAAlgorithmSHA1,
		Digits:      6,
		Period:      30,
		Issuer:      "QuicBaaS",
		AccountName: "legacy@example.com",
		Verified:    true,
		Created:     time.Now(),
	}
	require.NoError(t, m.storage.SaveSecret(ctx, stored))

	code := generateValidTOTPCode(t, testTOTPSecret, MFAConfig{
		Digits:    stored.Digits,
		Period:    stored.Period,
		Algorithm: stored.Algorithm,
	})

	require.NoError(t, m.VerifyCode(ctx, stored.UserID, stored.TenantID, code))

	// Verification really ran against this secret rather than short-circuiting.
	after, err := m.storage.GetSecret(ctx, stored.UserID, stored.TenantID)
	require.NoError(t, err)
	require.NotNil(t, after.LastUsed)

	// A code derived from a different secret must not verify: the enclave
	// carries this user's key and not just any well-formed key.
	otherSecret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	require.NotEqual(t, testTOTPSecret, otherSecret)
	wrongCode := generateValidTOTPCode(t, otherSecret, MFAConfig{
		Digits:    stored.Digits,
		Period:    stored.Period,
		Algorithm: stored.Algorithm,
	})
	assert.ErrorIs(t, m.VerifyCode(ctx, stored.UserID, stored.TenantID, wrongCode), ErrMFACodeInvalid)
}

// TestCompleteEnrollmentAcceptsCodeFromProvisionedSecret walks the enrollment
// call sites: the secret generated into protected memory is the one encoded
// into the provisioning URI the user scans, and the code their app derives
// from it completes enrollment and then authenticates.
func TestCompleteEnrollmentAcceptsCodeFromProvisionedSecret(t *testing.T) {
	ctx := context.Background()
	m := newProtectedSecretTestManager(t)

	resp, err := m.BeginEnrollment(ctx, 99997, "tenant-enroll", MFAMethodTOTP)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Secret)

	// The URI is rendered from the enclave while the returned secret is the
	// encoding taken before it, so a URI carrying the same secret proves the
	// sealed key and the enrolled text are one key.
	assert.Contains(t, resp.QRCodeURI, "secret="+resp.Secret)

	code := generateValidTOTPCode(t, resp.Secret, m.config)
	require.NoError(t, m.CompleteEnrollment(ctx, 99997, "tenant-enroll", resp.EnrollmentID, code))

	// The completed enrollment verifies through the separate VerifyCode path.
	verifyCode := generateValidTOTPCode(t, resp.Secret, m.config)
	require.NoError(t, m.VerifyCode(ctx, 99997, "tenant-enroll", verifyCode))
}

// TestCompleteEnrollmentRejectsCodeFromAnotherSecret is the enrollment call
// site's error path: a well-formed code derived from a different secret is
// refused and consumes an attempt, so the enclave is not accepting any code
// that merely parses.
func TestCompleteEnrollmentRejectsCodeFromAnotherSecret(t *testing.T) {
	ctx := context.Background()
	m := newProtectedSecretTestManager(t)

	resp, err := m.BeginEnrollment(ctx, 40060, "tenant-wrong", MFAMethodTOTP)
	require.NoError(t, err)
	require.NotEqual(t, testTOTPSecret, resp.Secret)

	wrongCode := generateValidTOTPCode(t, testTOTPSecret, m.config)
	assert.ErrorIs(t, m.CompleteEnrollment(ctx, 40060, "tenant-wrong", resp.EnrollmentID, wrongCode), ErrMFACodeInvalid)

	enrollment, err := m.storage.GetEnrollment(ctx, resp.EnrollmentID)
	require.NoError(t, err)
	assert.Equal(t, 1, enrollment.Attempts)
}

// TestVerifyCodeRejectsCorruptStoredSecret is the stored-secret error path: a
// record whose secret cannot be decoded into protected memory fails as a typed
// IAM error naming the verify operation, rather than being handed to the OTP
// library as a string.
func TestVerifyCodeRejectsCorruptStoredSecret(t *testing.T) {
	ctx := context.Background()
	m := newProtectedSecretTestManager(t)

	stored := &MFASecret{
		ID:        uuid.New().String(),
		UserID:    40003,
		TenantID:  "tenant-corrupt",
		Secret:    "not a base32 secret!!",
		Algorithm: MFAAlgorithmSHA1,
		Digits:    6,
		Period:    30,
		Issuer:    "QuicBaaS",
		Verified:  true,
		Created:   time.Now(),
	}
	require.NoError(t, m.storage.SaveSecret(ctx, stored))

	err := m.VerifyCode(ctx, stored.UserID, stored.TenantID, "123456")
	require.Error(t, err)
	assert.ErrorIs(t, err, otp.ErrInvalidConfig)
	assert.True(t, strings.Contains(err.Error(), "iam.mfa.verify"),
		"the error must name the operation that failed")
}
