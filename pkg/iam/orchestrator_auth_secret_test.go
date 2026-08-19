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
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// legacyHash reproduces the pre-KDF storage format: the client secret itself,
// hex-encoded. Accounts written by that code hold a recoverable credential.
func legacyHash(secret string) string {
	return hex.EncodeToString([]byte(secret))
}

// seedLegacyAccount creates a service account and rewrites its stored hash into
// the legacy format, standing in for a record persisted before the KDF landed.
func seedLegacyAccount(t *testing.T, svc *OrchestratorAuthService, name string) (*ServiceAccount, string) {
	t.Helper()

	req := validCreateRequest()
	req.Name = name

	account, creds, err := svc.CreateServiceAccount(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	account.SecretHash = legacyHash(creds.ClientSecret)
	if err := svc.store.PutServiceAccount(context.Background(), account); err != nil {
		t.Fatalf("PutServiceAccount: %v", err)
	}

	return account, creds.ClientSecret
}

// =============================================================================
// hashSecret / verifySecret / isLegacySecretHash
// =============================================================================

func TestHashSecret_ProducesNonInvertibleDigest(t *testing.T) {
	const secret = "s3cr3t-client-secret"

	digest, err := hashSecret(secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(digest, secret) {
		t.Error("digest must not contain the plaintext secret")
	}
	if digest == legacyHash(secret) {
		t.Error("digest must not be the hex-encoded plaintext")
	}
	parsed, parseErr := parseSecretHash(digest)
	if parseErr != nil {
		t.Fatalf("digest is not a PBKDF2 hash: %v", parseErr)
	}
	if parsed.iterations != pbkdf2Iterations {
		t.Errorf("iterations = %d, want %d", parsed.iterations, pbkdf2Iterations)
	}
	if len(parsed.salt) != pbkdf2SaltLength {
		t.Errorf("salt = %d bytes, want %d — FIPS requires at least 128 bits", len(parsed.salt), pbkdf2SaltLength)
	}
	if len(parsed.key) != pbkdf2KeyLength {
		t.Errorf("derived key = %d bytes, want %d", len(parsed.key), pbkdf2KeyLength)
	}
	if !verifySecret(digest, secret) {
		t.Error("digest must verify against the secret it was derived from")
	}

	// Every derivation draws a fresh salt, so the same secret hashes
	// differently each time.
	second, err := hashSecret(secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest == second {
		t.Error("two derivations of the same secret must not be identical")
	}
}

func TestHashSecret_AcceptsSecretsOfAnyLength(t *testing.T) {
	// PBKDF2 folds the whole input through HMAC and has no length ceiling.
	// This is not incidental: bcrypt, which this replaced, errors above 72
	// bytes, and generateRandomHex(32) already emits a 64-character secret —
	// eight characters short of that ceiling. Widening the secret at all
	// would have broken creation outright.
	for _, length := range []int{1, 64, 73, 4096} {
		secret := strings.Repeat("x", length)

		digest, err := hashSecret(secret)
		if err != nil {
			t.Fatalf("length %d: unexpected error: %v", length, err)
		}
		if !verifySecret(digest, secret) {
			t.Errorf("length %d: digest must verify against its own secret", length)
		}
	}
}

func TestParseSecretHash_RejectsMalformedDigests(t *testing.T) {
	valid, err := hashSecret("s3cr3t-client-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fields := strings.Split(valid, "$")

	cases := []struct {
		name   string
		stored string
	}{
		{"empty", ""},
		{"too few fields", "$pbkdf2-sha256$i=600000$c2FsdA"},
		{"no leading delimiter", strings.TrimPrefix(valid, "$")},
		{"unknown scheme", "$scrypt$i=600000$" + fields[3] + "$" + fields[4]},
		{"missing iteration prefix", "$pbkdf2-sha256$600000$" + fields[3] + "$" + fields[4]},
		{"non-numeric iterations", "$pbkdf2-sha256$i=many$" + fields[3] + "$" + fields[4]},
		{"zero iterations", "$pbkdf2-sha256$i=0$" + fields[3] + "$" + fields[4]},
		{"negative iterations", "$pbkdf2-sha256$i=-1$" + fields[3] + "$" + fields[4]},
		{"iterations above the accepted factor", "$pbkdf2-sha256$i=" + strconv.Itoa(pbkdf2Iterations+1) + "$" + fields[3] + "$" + fields[4]},
		{"iterations below the accepted factor", "$pbkdf2-sha256$i=1000$" + fields[3] + "$" + fields[4]},
		{"iterations at MaxInt32", "$pbkdf2-sha256$i=2147483647$" + fields[3] + "$" + fields[4]},
		{"salt is not base64", "$pbkdf2-sha256$i=600000$!!!!$" + fields[4]},
		{"salt below 128 bits", "$pbkdf2-sha256$i=600000$c2hvcnQ$" + fields[4]},
		{"salt above the accepted length", "$pbkdf2-sha256$i=600000$" + secretHashEncoding.EncodeToString(make([]byte, pbkdf2SaltLength+1)) + "$" + fields[4]},
		{"key is not base64", "$pbkdf2-sha256$i=600000$" + fields[3] + "$!!!!"},
		{"key is the wrong length", "$pbkdf2-sha256$i=600000$" + fields[3] + "$c2hvcnQ"},
	}

	for _, tc := range cases {
		digest, err := parseSecretHash(tc.stored)
		if err == nil {
			t.Errorf("%s: expected a parse error, got digest %+v", tc.name, digest)
			continue
		}
		if !errors.Is(err, ErrSecretHashMalformed) {
			t.Errorf("%s: expected ErrSecretHashMalformed, got: %v", tc.name, err)
		}
		if strings.Contains(err.Error(), tc.stored) && tc.stored != "" {
			t.Errorf("%s: error message echoes the stored value, which may be a live secret", tc.name)
		}
		if !isLegacySecretHash(tc.stored) {
			t.Errorf("%s: an unparseable value must be classified as legacy so it fails closed", tc.name)
		}
		if verifySecret(tc.stored, "s3cr3t-client-secret") {
			t.Errorf("%s: an unparseable value must never verify", tc.name)
		}
	}
}

func TestCompareSecretHash_RejectsEveryWorkFactorButTheAccepted(t *testing.T) {
	// A digest is verified at one work factor and one only. Accepting a
	// cheaper stored count would be the whole non-enumeration property gone:
	// an ineligible account is checked against timingEqualizerDigest at
	// pbkdf2Iterations, so an account holding a cheaper digest would answer
	// measurably sooner than an unknown client ID and reveal that it exists.
	const secret = "s3cr3t-client-secret"

	salt := []byte("sixteen-byte-slt")

	for _, iterations := range []int{1, 1000, pbkdf2Iterations - 1, pbkdf2Iterations + 1, 10 * pbkdf2Iterations} {
		key, err := pbkdf2.Key(sha256.New, secret, salt, iterations, pbkdf2KeyLength)
		if err != nil {
			t.Fatalf("i=%d: unexpected error: %v", iterations, err)
		}
		stored := formatSecretHash(&secretDigest{iterations: iterations, salt: salt, key: key})

		if compareSecretHash(stored, secret) {
			t.Errorf("i=%d: a digest at a non-accepted work factor must not verify, even for the right secret", iterations)
		}
		if !isLegacySecretHash(stored) {
			t.Errorf("i=%d: a digest at a non-accepted work factor must be classified legacy so it fails closed", iterations)
		}
	}

	// The accepted work factor still round-trips, or the above would be
	// vacuously true.
	digest, err := hashSecret(secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !compareSecretHash(digest, secret) {
		t.Fatal("a digest at the accepted work factor must verify")
	}
}

func TestCompareSecretHash_RefusesAnOversizedSaltWithoutRunningTheKDF(t *testing.T) {
	// PBKDF2 hashes the salt once per derived block, so a tampered record
	// carrying a huge but well-formed salt would cost more to verify than the
	// fixed-cost equalizer — reopening the timing oracle from the salt side
	// rather than the iteration side, and letting stored data dictate CPU and
	// allocation. This package always writes exactly pbkdf2SaltLength, so
	// anything else is refused during parsing.
	digest, err := hashSecret("s3cr3t-client-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fields := strings.Split(digest, "$")

	start := time.Now()
	if !compareSecretHash(digest, "s3cr3t-client-secret") {
		t.Fatal("the honest digest must verify")
	}
	honest := time.Since(start)

	for _, saltLen := range []int{pbkdf2SaltLength + 1, 4 << 20} {
		oversized := "$pbkdf2-sha256$i=" + strconv.Itoa(pbkdf2Iterations) + "$" +
			secretHashEncoding.EncodeToString(make([]byte, saltLen)) + "$" + fields[4]

		start := time.Now()
		matched := compareSecretHash(oversized, "s3cr3t-client-secret")
		elapsed := time.Since(start)

		if matched {
			t.Errorf("salt=%d bytes: an off-length salt must never verify", saltLen)
		}
		if elapsed > honest {
			t.Errorf("salt=%d bytes: rejection took %v, longer than one honest derivation (%v) — the KDF ran",
				saltLen, elapsed, honest)
		}
		if !isLegacySecretHash(oversized) {
			t.Errorf("salt=%d bytes: an off-length salt must be classified legacy so it fails closed", saltLen)
		}
	}
}

func TestCompareSecretHash_RefusesAnOversizedWorkFactorWithoutRunningTheKDF(t *testing.T) {
	// A stored iteration count is attacker-influenced the moment the store is
	// corrupted or tampered with, and compareSecretHash would otherwise feed it
	// straight to the KDF. i=2147483647 would occupy a request worker for
	// minutes on every attempt against that account.
	//
	// The rejection has to happen during parsing, so what this test really
	// asserts is the elapsed time: a verification that ran even a fraction of
	// the requested work would take far longer than deriving one honest digest.
	digest, err := hashSecret("s3cr3t-client-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fields := strings.Split(digest, "$")

	// Calibrate against one real derivation rather than a wall-clock constant,
	// so the bound holds on a slow or loaded runner.
	start := time.Now()
	if !compareSecretHash(digest, "s3cr3t-client-secret") {
		t.Fatal("the honest digest must verify")
	}
	honest := time.Since(start)

	for _, iterations := range []int{10 * pbkdf2Iterations, 1 << 30, 2147483647} {
		oversized := "$pbkdf2-sha256$i=" + strconv.Itoa(iterations) + "$" + fields[3] + "$" + fields[4]

		start := time.Now()
		matched := compareSecretHash(oversized, "s3cr3t-client-secret")
		elapsed := time.Since(start)

		if matched {
			t.Errorf("i=%d: an oversized work factor must never verify", iterations)
		}
		if elapsed > honest {
			t.Errorf("i=%d: rejection took %v, longer than one honest derivation (%v) — the KDF ran",
				iterations, elapsed, honest)
		}
		if !isLegacySecretHash(oversized) {
			t.Errorf("i=%d: an oversized work factor must be classified as legacy so it fails closed", iterations)
		}
	}
}

func TestVerifySecret_RejectsWrongAndLegacyValues(t *testing.T) {
	const secret = "s3cr3t-client-secret"

	digest, err := hashSecret(secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if verifySecret(digest, "wrong-secret") {
		t.Error("a different secret must not verify")
	}
	if verifySecret(legacyHash(secret), secret) {
		t.Error("a legacy hex hash must never verify")
	}
	if verifySecret("", secret) {
		t.Error("an empty stored hash must never verify")
	}
}

func TestIsLegacySecretHash_ClassifiesStoredForms(t *testing.T) {
	digest, err := hashSecret("s3cr3t-client-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if isLegacySecretHash(digest) {
		t.Error("a PBKDF2 digest must not be classified as legacy")
	}
	if !isLegacySecretHash(legacyHash("s3cr3t-client-secret")) {
		t.Error("a hex-encoded plaintext must be classified as legacy")
	}
	if !isLegacySecretHash("") {
		t.Error("an empty value is not a PBKDF2 digest")
	}
}

// =============================================================================
// Stored form written by the service
// =============================================================================

func TestCreateServiceAccount_StoresKDFDigest(t *testing.T) {
	svc := newTestAuthService(t)

	account, creds, err := svc.CreateServiceAccount(context.Background(), validCreateRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if account.SecretHash == creds.ClientSecret || account.SecretHash == legacyHash(creds.ClientSecret) {
		t.Fatal("stored hash must not be a recoverable form of the client secret")
	}
	if isLegacySecretHash(account.SecretHash) {
		t.Fatal("stored hash must be a bcrypt digest")
	}
	if !verifySecret(account.SecretHash, creds.ClientSecret) {
		t.Error("stored digest must verify against the issued secret")
	}
}

func TestRotateCredentials_RehashesAndRetiresOldSecret(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	account, creds, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	rotated, err := svc.RotateCredentials(ctx, account.ID)
	if err != nil {
		t.Fatalf("RotateCredentials: %v", err)
	}
	if rotated.ClientSecret == creds.ClientSecret {
		t.Fatal("rotation must issue a different secret")
	}

	if _, err := svc.AuthenticateServiceAccount(ctx, account.ClientID, creds.ClientSecret); err == nil {
		t.Error("the retired secret must no longer authenticate")
	}
	if _, err := svc.AuthenticateServiceAccount(ctx, account.ClientID, rotated.ClientSecret); err != nil {
		t.Errorf("the rotated secret must authenticate: %v", err)
	}
}

// =============================================================================
// AuthenticateServiceAccount
// =============================================================================

func TestAuthenticateServiceAccount_Success(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	account, creds, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	authenticated, err := svc.AuthenticateServiceAccount(ctx, creds.ClientID, creds.ClientSecret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authenticated.ID != account.ID {
		t.Errorf("expected account %s, got %s", account.ID, authenticated.ID)
	}
}

func TestAuthenticateServiceAccount_RejectsBadInput(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	_, creds, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	cases := []struct {
		name         string
		clientID     string
		clientSecret string
	}{
		{"empty client id", "", creds.ClientSecret},
		{"empty client secret", creds.ClientID, ""},
		{"unknown client id", "no-such-client", creds.ClientSecret},
		{"wrong secret", creds.ClientID, "wrong-secret"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account, err := svc.AuthenticateServiceAccount(ctx, tc.clientID, tc.clientSecret)
			if err == nil {
				t.Fatal("expected an authentication failure")
			}
			if account != nil {
				t.Error("no account may be returned on failure")
			}
			if !errors.Is(err, ErrServiceAccountAuthFailed) {
				t.Errorf("expected ErrServiceAccountAuthFailed, got: %v", err)
			}
			var authErr *ServiceAccountAuthFailedError
			if !errors.As(err, &authErr) {
				t.Errorf("expected *ServiceAccountAuthFailedError, got: %T", err)
			}
		})
	}
}

func TestAuthenticateServiceAccount_RejectsLegacyHash(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	account, secret := seedLegacyAccount(t, svc, "legacy-worker")

	_, err := svc.AuthenticateServiceAccount(ctx, account.ClientID, secret)
	if err == nil {
		t.Fatal("a legacy stored hash must never authenticate")
	}
	if !errors.Is(err, ErrServiceAccountAuthFailed) {
		t.Errorf("expected ErrServiceAccountAuthFailed, got: %v", err)
	}
}

func TestAuthenticateServiceAccount_RejectsRevokedAndExpired(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	revoked, revokedCreds, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if err := svc.RevokeServiceAccount(ctx, revoked.ID); err != nil {
		t.Fatalf("RevokeServiceAccount: %v", err)
	}
	if _, err := svc.AuthenticateServiceAccount(ctx, revokedCreds.ClientID, revokedCreds.ClientSecret); !errors.Is(err, ErrServiceAccountAuthFailed) {
		t.Errorf("a revoked account must not authenticate, got: %v", err)
	}

	expiredReq := validCreateRequest()
	expiredReq.Name = "expired-worker"
	expired, expiredCreds, err := svc.CreateServiceAccount(ctx, expiredReq)
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	past := time.Now().UTC().Add(-time.Minute)
	expired.ExpiresAt = &past
	if err := svc.store.PutServiceAccount(ctx, expired); err != nil {
		t.Fatalf("PutServiceAccount: %v", err)
	}
	if _, err := svc.AuthenticateServiceAccount(ctx, expiredCreds.ClientID, expiredCreds.ClientSecret); !errors.Is(err, ErrServiceAccountAuthFailed) {
		t.Errorf("an expired account must not authenticate, got: %v", err)
	}
}

func TestAuthenticateServiceAccount_CancelledContext(t *testing.T) {
	svc := newTestAuthService(t)

	_, creds, err := svc.CreateServiceAccount(context.Background(), validCreateRequest())
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.AuthenticateServiceAccount(ctx, creds.ClientID, creds.ClientSecret); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// =============================================================================
// InvalidateLegacySecretHashes
// =============================================================================

func TestInvalidateLegacySecretHashes_ClearsOnlyLegacyRecords(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	legacy, legacySecret := seedLegacyAccount(t, svc, "legacy-worker")

	currentReq := validCreateRequest()
	currentReq.Name = "current-worker"
	current, currentCreds, err := svc.CreateServiceAccount(ctx, currentReq)
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	invalidated, err := svc.InvalidateLegacySecretHashes(ctx, legacy.TenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if invalidated != 1 {
		t.Fatalf("expected 1 invalidated account, got %d", invalidated)
	}

	stored, err := svc.store.GetServiceAccount(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	if stored.SecretHash != "" {
		t.Errorf("legacy hash must be cleared, got %q", stored.SecretHash)
	}

	untouched, err := svc.store.GetServiceAccount(ctx, current.ID)
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	if !verifySecret(untouched.SecretHash, currentCreds.ClientSecret) {
		t.Error("a current bcrypt digest must be left untouched")
	}

	// The invalidated credential stays unusable until it is rotated, and the
	// rotated secret authenticates against the new digest.
	if _, err := svc.AuthenticateServiceAccount(ctx, legacy.ClientID, legacySecret); !errors.Is(err, ErrServiceAccountAuthFailed) {
		t.Errorf("an invalidated account must not authenticate, got: %v", err)
	}
	rotated, err := svc.RotateCredentials(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("RotateCredentials: %v", err)
	}
	if _, err := svc.AuthenticateServiceAccount(ctx, legacy.ClientID, rotated.ClientSecret); err != nil {
		t.Errorf("the rotated secret must authenticate: %v", err)
	}

	// A second pass finds nothing left to clear.
	again, err := svc.InvalidateLegacySecretHashes(ctx, legacy.TenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if again != 0 {
		t.Errorf("expected 0 invalidated accounts on the second pass, got %d", again)
	}
}

func TestInvalidateLegacySecretHashes_RejectsBadInput(t *testing.T) {
	svc := newTestAuthService(t)

	if _, err := svc.InvalidateLegacySecretHashes(context.Background(), ""); !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.InvalidateLegacySecretHashes(ctx, "tenant-1"); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// =============================================================================
// Store failure paths
// =============================================================================

// failingStore wraps a ServiceAccountStore and fails the operations the
// migration path depends on, so their error branches are exercised.
type failingStore struct {
	ServiceAccountStore
	listErr  error
	putErr   error
	clearErr error
}

func (s *failingStore) ListServiceAccountsIncludingRevoked(ctx context.Context, tenantID string) ([]*ServiceAccount, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.ServiceAccountStore.ListServiceAccountsIncludingRevoked(ctx, tenantID)
}

func (s *failingStore) PutServiceAccount(ctx context.Context, account *ServiceAccount) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.ServiceAccountStore.PutServiceAccount(ctx, account)
}

func (s *failingStore) ClearSecretHashIfUnchanged(ctx context.Context, accountID, expected string) (bool, error) {
	if s.clearErr != nil {
		return false, s.clearErr
	}
	return s.ServiceAccountStore.ClearSecretHashIfUnchanged(ctx, accountID, expected)
}

func TestInvalidateLegacySecretHashes_PropagatesStoreErrors(t *testing.T) {
	ctx := context.Background()
	listErr := errors.New("list failed")
	clearErr := errors.New("clear failed")

	// The listing failure aborts before any account is inspected.
	store := &failingStore{ServiceAccountStore: NewInMemoryServiceAccountStore(), listErr: listErr}
	svc, err := NewOrchestratorAuthService(store, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("NewOrchestratorAuthService: %v", err)
	}
	count, err := svc.InvalidateLegacySecretHashes(ctx, "tenant-1")
	if !errors.Is(err, listErr) {
		t.Errorf("expected the list error, got: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 invalidated accounts, got %d", count)
	}

	// A write failure stops the sweep and reports what was already cleared.
	writeStore := &failingStore{ServiceAccountStore: NewInMemoryServiceAccountStore()}
	writeSvc, err := NewOrchestratorAuthService(writeStore, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("NewOrchestratorAuthService: %v", err)
	}
	legacy, legacySecret := seedLegacyAccount(t, writeSvc, "legacy-worker")
	writeStore.clearErr = clearErr

	count, err = writeSvc.InvalidateLegacySecretHashes(ctx, "tenant-1")
	if !errors.Is(err, clearErr) {
		t.Errorf("expected the clear error, got: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 invalidated accounts, got %d", count)
	}

	// A failed write must leave the stored record exactly as it was, so the
	// reported count describes what actually changed.
	stored, err := writeStore.GetServiceAccount(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	if stored.SecretHash != legacyHash(legacySecret) {
		t.Errorf("stored hash changed despite the write failure: %q", stored.SecretHash)
	}
}

// interleavingStore runs a hook between the listing and the conditional clear,
// which is exactly the window where a concurrent writer would be lost. It makes
// the race deterministic rather than hoping the scheduler reproduces it.
type interleavingStore struct {
	ServiceAccountStore
	beforeClear func()
}

func (s *interleavingStore) ClearSecretHashIfUnchanged(ctx context.Context, accountID, expected string) (bool, error) {
	if s.beforeClear != nil {
		hook := s.beforeClear
		s.beforeClear = nil // fire once, so the hook's own writes are not re-entered
		hook()
	}
	return s.ServiceAccountStore.ClearSecretHashIfUnchanged(ctx, accountID, expected)
}

func TestInvalidateLegacySecretHashes_DoesNotClobberAConcurrentRotation(t *testing.T) {
	// Invalidation necessarily decides from a record it listed earlier. If it
	// wrote back unconditionally, a secret issued in that window would be
	// wiped and the operator would be holding credentials that never worked.
	ctx := context.Background()
	store := &interleavingStore{ServiceAccountStore: NewInMemoryServiceAccountStore()}
	svc, err := NewOrchestratorAuthService(store, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("NewOrchestratorAuthService: %v", err)
	}

	legacy, _ := seedLegacyAccount(t, svc, "legacy-worker")

	var rotated *ClientCredentials
	store.beforeClear = func() {
		creds, rotateErr := svc.RotateCredentials(ctx, legacy.ID)
		if rotateErr != nil {
			t.Errorf("RotateCredentials: %v", rotateErr)
			return
		}
		rotated = creds
	}

	count, err := svc.InvalidateLegacySecretHashes(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("InvalidateLegacySecretHashes: %v", err)
	}
	if rotated == nil {
		t.Fatal("the interleaved rotation did not run")
	}

	// The record moved underneath, so it must be skipped and not counted.
	if count != 0 {
		t.Errorf("invalidated = %d, want 0 — the record changed underneath and must be left alone", count)
	}

	stored, err := store.GetServiceAccount(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	if stored.SecretHash == "" {
		t.Fatal("the freshly rotated secret was cleared by the invalidation sweep")
	}
	if !verifySecret(stored.SecretHash, rotated.ClientSecret) {
		t.Error("the stored digest no longer matches the rotated secret")
	}

	// And the rotated credential must actually authenticate.
	if _, err := svc.AuthenticateServiceAccount(ctx, rotated.ClientID, rotated.ClientSecret); err != nil {
		t.Errorf("the rotated credential must authenticate: %v", err)
	}
}

func TestInvalidateLegacySecretHashes_DoesNotResurrectAConcurrentRevocation(t *testing.T) {
	// The stale copy carries every field, not just SecretHash, so an
	// unconditional write-back would restore Revoked=false and bring an
	// account somebody had just shut off back to life.
	ctx := context.Background()
	store := &interleavingStore{ServiceAccountStore: NewInMemoryServiceAccountStore()}
	svc, err := NewOrchestratorAuthService(store, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("NewOrchestratorAuthService: %v", err)
	}

	legacy, _ := seedLegacyAccount(t, svc, "legacy-worker")

	store.beforeClear = func() {
		if revokeErr := svc.RevokeServiceAccount(ctx, legacy.ID); revokeErr != nil {
			t.Errorf("RevokeServiceAccount: %v", revokeErr)
		}
	}

	if _, err := svc.InvalidateLegacySecretHashes(ctx, "tenant-1"); err != nil {
		t.Fatalf("InvalidateLegacySecretHashes: %v", err)
	}

	stored, err := store.GetServiceAccount(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	if !stored.Revoked {
		t.Error("the invalidation sweep resurrected a concurrently revoked account")
	}
}

func TestInvalidateLegacySecretHashes_DoesNotResurrectAConcurrentDeletion(t *testing.T) {
	// Deletion is a writer too. If it were left outside the store's write
	// lock, a delete landing between the conditional clear's read and its
	// write would be undone by the store that follows, bringing back a record
	// deletion had already reported gone.
	ctx := context.Background()
	store := &interleavingStore{ServiceAccountStore: NewInMemoryServiceAccountStore()}
	svc, err := NewOrchestratorAuthService(store, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("NewOrchestratorAuthService: %v", err)
	}

	legacy, _ := seedLegacyAccount(t, svc, "legacy-worker")
	survivor, _ := seedLegacyAccount(t, svc, "surviving-worker")

	store.beforeClear = func() {
		if delErr := store.DeleteServiceAccount(ctx, legacy.ID); delErr != nil {
			t.Errorf("DeleteServiceAccount: %v", delErr)
		}
	}

	// A record deleted underneath is not an error: there is no longer a
	// credential to invalidate, which is the outcome the sweep wanted. It must
	// not abort the sweep either — the other account still needs clearing.
	count, err := svc.InvalidateLegacySecretHashes(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("a concurrent deletion must not fail the sweep: %v", err)
	}

	if _, err := store.GetServiceAccount(ctx, legacy.ID); !errors.Is(err, ErrServiceAccountNotFound) {
		t.Error("the invalidation sweep resurrected a concurrently deleted account")
	}

	// The sweep continued past the deleted record.
	stored, err := store.GetServiceAccount(ctx, survivor.ID)
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	if stored.SecretHash != "" {
		t.Error("the sweep stopped at the deleted record and left a legacy hash behind")
	}
	if count != 1 {
		t.Errorf("invalidated = %d, want 1 — only the surviving record was cleared", count)
	}
}

func TestServiceAccountStore_DeletionIsNotUndoneByAConcurrentClear(t *testing.T) {
	// The interleaving that matters happens INSIDE the store, between the
	// conditional clear's read and its write, so it cannot be injected from
	// outside once the lock is correct. Drive both writers at each other
	// instead and assert the invariant: a delete that reported success must
	// stay done. Run under -race this also flags the unsynchronised access
	// directly.
	ctx := context.Background()
	const accounts = 64

	store := NewInMemoryServiceAccountStore()
	svc, err := NewOrchestratorAuthService(store, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("NewOrchestratorAuthService: %v", err)
	}

	// Written straight into the store rather than through
	// CreateServiceAccount: this test is about two writers colliding, and
	// paying a 600,000-iteration derivation per account would make it slow
	// without making it stricter.
	ids := make([]string, 0, accounts)
	for i := range accounts {
		id := "concurrent-worker-" + strconv.Itoa(i)
		account := &ServiceAccount{
			ID:         id,
			TenantID:   "tenant-1",
			Name:       id,
			ClientID:   id,
			SecretHash: legacyHash("secret-" + strconv.Itoa(i)),
			CreatedAt:  time.Now().UTC(),
		}
		if err := store.PutServiceAccount(ctx, account); err != nil {
			t.Fatalf("PutServiceAccount: %v", err)
		}
		ids = append(ids, id)
	}

	// Both goroutines start together so the delete lands in the middle of the
	// sweep rather than before or after it.
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	deleted := make(chan string, accounts)
	go func() {
		defer wg.Done()
		<-start
		for _, id := range ids {
			if err := store.DeleteServiceAccount(ctx, id); err == nil {
				deleted <- id
			}
		}
		close(deleted)
	}()

	go func() {
		defer wg.Done()
		<-start
		// Sweep repeatedly so the two keep colliding for the whole run.
		for range 8 {
			if _, err := svc.InvalidateLegacySecretHashes(ctx, "tenant-1"); err != nil {
				t.Errorf("InvalidateLegacySecretHashes: %v", err)
				return
			}
		}
	}()

	close(start)
	wg.Wait()

	for id := range deleted {
		if _, err := store.GetServiceAccount(ctx, id); !errors.Is(err, ErrServiceAccountNotFound) {
			t.Fatalf("account %s was deleted successfully but came back: %v", id, err)
		}
	}
}

func TestAuthenticateServiceAccount_SurfacesStoreFailures(t *testing.T) {
	// A store outage answered with the opaque credential error would bury an
	// operational failure under what reads as a wave of bad passwords — and
	// bill a key derivation for each one. Only a genuinely missing record is a
	// credential verdict.
	ctx := context.Background()
	outage := errors.New("store unavailable")

	store := &lookupFailingStore{ServiceAccountStore: NewInMemoryServiceAccountStore(), lookupErr: outage}
	svc, err := NewOrchestratorAuthService(store, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("NewOrchestratorAuthService: %v", err)
	}

	_, err = svc.AuthenticateServiceAccount(ctx, "client-id", "client-secret")
	if !errors.Is(err, outage) {
		t.Errorf("expected the store error to surface, got: %v", err)
	}
	if errors.Is(err, ErrServiceAccountAuthFailed) {
		t.Error("a store outage must not be reported as an authentication failure")
	}

	// A missing record still fails closed and opaquely.
	notFound := &lookupFailingStore{
		ServiceAccountStore: NewInMemoryServiceAccountStore(),
		lookupErr:           &ServiceAccountNotFoundError{AccountID: "client_id=client-id"},
	}
	notFoundSvc, err := NewOrchestratorAuthService(notFound, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("NewOrchestratorAuthService: %v", err)
	}
	_, err = notFoundSvc.AuthenticateServiceAccount(ctx, "client-id", "client-secret")
	if !errors.Is(err, ErrServiceAccountAuthFailed) {
		t.Errorf("an unknown client must still fail closed and opaquely, got: %v", err)
	}
}

// lookupFailingStore fails only the client-ID lookup.
type lookupFailingStore struct {
	ServiceAccountStore
	lookupErr error
}

func (s *lookupFailingStore) GetServiceAccountByClientID(ctx context.Context, clientID string) (*ServiceAccount, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.ServiceAccountStore.GetServiceAccountByClientID(ctx, clientID)
}

func TestInvalidateLegacySecretHashes_ClearsRevokedRecords(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	legacy, _ := seedLegacyAccount(t, svc, "revoked-legacy-worker")
	if err := svc.RevokeServiceAccount(ctx, legacy.ID); err != nil {
		t.Fatalf("RevokeServiceAccount: %v", err)
	}

	invalidated, err := svc.InvalidateLegacySecretHashes(ctx, legacy.TenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if invalidated != 1 {
		t.Fatalf("expected the revoked account to be cleared, got %d", invalidated)
	}

	// Revocation stops authentication, but the recoverable secret has to go
	// from the store as well.
	stored, err := svc.store.GetServiceAccount(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	if stored.SecretHash != "" {
		t.Errorf("revoked account kept its legacy hash: %q", stored.SecretHash)
	}
	if !stored.Revoked {
		t.Error("clearing the hash must not un-revoke the account")
	}
}

// =============================================================================
// Error types
// =============================================================================

func TestServiceAccountAuthFailedError(t *testing.T) {
	err := &ServiceAccountAuthFailedError{}

	if err.Error() != ErrServiceAccountAuthFailed.Error() {
		t.Errorf("message must carry no detail beyond the sentinel, got: %s", err.Error())
	}
	if !errors.Is(err, ErrServiceAccountAuthFailed) {
		t.Error("expected the error to unwrap to ErrServiceAccountAuthFailed")
	}
}

func TestSecretHashError(t *testing.T) {
	cause := errors.New("kdf exploded")
	err := &SecretHashError{Err: cause}

	if !strings.Contains(err.Error(), cause.Error()) {
		t.Errorf("message should carry the cause, got: %s", err.Error())
	}
	if !errors.Is(err, ErrSecretHashFailed) || !errors.Is(err, cause) {
		t.Error("expected the error to unwrap to both the sentinel and the cause")
	}

	// A zero value must describe itself rather than panic.
	if (&SecretHashError{}).Error() != ErrSecretHashFailed.Error() {
		t.Error("a nil cause should degrade to the sentinel message")
	}
}

// =============================================================================
// Non-enumeration guarantees
// =============================================================================

// authFailureCases builds one service account per failure mode and returns the
// credentials that must all fail identically.
func authFailureCases(t *testing.T, svc *OrchestratorAuthService) []struct {
	name         string
	clientID     string
	clientSecret string
} {
	t.Helper()
	ctx := context.Background()

	newAccount := func(name string) (*ServiceAccount, *ClientCredentials) {
		req := validCreateRequest()
		req.Name = name
		account, creds, err := svc.CreateServiceAccount(ctx, req)
		if err != nil {
			t.Fatalf("CreateServiceAccount(%s): %v", name, err)
		}
		return account, creds
	}

	live, liveCreds := newAccount("live-worker")
	_ = live

	revoked, revokedCreds := newAccount("revoked-worker")
	if err := svc.RevokeServiceAccount(ctx, revoked.ID); err != nil {
		t.Fatalf("RevokeServiceAccount: %v", err)
	}

	expired, expiredCreds := newAccount("expiring-worker")
	past := time.Now().UTC().Add(-time.Minute)
	expired.ExpiresAt = &past
	if err := svc.store.PutServiceAccount(ctx, expired); err != nil {
		t.Fatalf("PutServiceAccount: %v", err)
	}

	legacy, legacySecret := seedLegacyAccount(t, svc, "legacy-worker")

	cleared, clearedCreds := newAccount("cleared-worker")
	cleared.SecretHash = ""
	if err := svc.store.PutServiceAccount(ctx, cleared); err != nil {
		t.Fatalf("PutServiceAccount: %v", err)
	}

	return []struct {
		name         string
		clientID     string
		clientSecret string
	}{
		{"unknown client", "no-such-client", liveCreds.ClientSecret},
		{"wrong secret", liveCreds.ClientID, "wrong-secret"},
		{"revoked account", revokedCreds.ClientID, revokedCreds.ClientSecret},
		{"expired account", expiredCreds.ClientID, expiredCreds.ClientSecret},
		{"legacy hash", legacy.ClientID, legacySecret},
		{"cleared hash", clearedCreds.ClientID, clearedCreds.ClientSecret},
	}
}

func TestAuthenticateServiceAccount_FailuresAreIndistinguishable(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	want := (&ServiceAccountAuthFailedError{}).Error()
	for _, tc := range authFailureCases(t, svc) {
		account, err := svc.AuthenticateServiceAccount(ctx, tc.clientID, tc.clientSecret)
		if err == nil {
			t.Fatalf("%s: expected an authentication failure", tc.name)
		}
		if account != nil {
			t.Errorf("%s: no account may be returned on failure", tc.name)
		}
		if err.Error() != want {
			t.Errorf("%s: message distinguishes this failure: %q", tc.name, err.Error())
		}
		if err != errServiceAccountAuthFailed {
			t.Errorf("%s: expected the shared opaque error value, got %#v", tc.name, err)
		}
	}
}

func TestAuthenticateServiceAccount_VerifiesExactlyOncePerAttempt(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	cases := authFailureCases(t, svc)

	// Also cover the success path: it must do the same single comparison.
	req := validCreateRequest()
	req.Name = "success-worker"
	_, successCreds, err := svc.CreateServiceAccount(ctx, req)
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	cases = append(cases, struct {
		name         string
		clientID     string
		clientSecret string
	}{"valid credentials", successCreds.ClientID, successCreds.ClientSecret})

	real := verifySecret
	t.Cleanup(func() { verifySecret = real })

	for _, tc := range cases {
		calls := 0
		var comparedAgainst []string
		verifySecret = func(stored, secret string) bool {
			calls++
			comparedAgainst = append(comparedAgainst, stored)
			return real(stored, secret)
		}

		if _, err := svc.AuthenticateServiceAccount(ctx, tc.clientID, tc.clientSecret); err != nil && tc.name == "valid credentials" {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}

		if calls != 1 {
			t.Errorf("%s: expected exactly 1 key derivation, got %d", tc.name, calls)
		}
		for _, stored := range comparedAgainst {
			if isLegacySecretHash(stored) && stored != timingEqualizerDigest {
				t.Errorf("%s: compared against an unparseable stored value %q", tc.name, stored)
			}
		}
	}
}

func TestAuthenticateServiceAccount_CostsTheSameWhateverTheStoredState(t *testing.T) {
	// Non-enumeration is a claim about cost, so this asserts cost directly
	// rather than by stopwatch: whatever the account's state, the digest handed
	// to the verifier must carry the accepted work factor. If any path could
	// reach the KDF with some other count, that account would answer at a
	// different speed than an unknown client ID and its existence would leak.
	//
	// A work-factor assertion is used instead of elapsed time deliberately —
	// a timing assertion on a shared runner is a flaky test, and PBKDF2 cost is
	// determined entirely by the iteration count, so this measures the same
	// property deterministically.
	svc := newTestAuthService(t)
	ctx := context.Background()

	cases := authFailureCases(t, svc)

	req := validCreateRequest()
	req.Name = "cost-worker"
	_, successCreds, err := svc.CreateServiceAccount(ctx, req)
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	cases = append(cases, struct {
		name         string
		clientID     string
		clientSecret string
	}{"valid credentials", successCreds.ClientID, successCreds.ClientSecret})

	real := verifySecret
	t.Cleanup(func() { verifySecret = real })

	for _, tc := range cases {
		var compared []string
		verifySecret = func(stored, secret string) bool {
			compared = append(compared, stored)
			return real(stored, secret)
		}

		if _, err := svc.AuthenticateServiceAccount(ctx, tc.clientID, tc.clientSecret); err != nil && tc.name == "valid credentials" {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}

		if len(compared) != 1 {
			t.Errorf("%s: expected exactly 1 comparison, got %d", tc.name, len(compared))
			continue
		}

		digest, parseErr := parseSecretHash(compared[0])
		if parseErr != nil {
			t.Errorf("%s: compared against a value that is not a digest: %v", tc.name, parseErr)
			continue
		}
		if digest.iterations != pbkdf2Iterations {
			t.Errorf("%s: compared at %d iterations, want %d — this attempt costs a different amount than an unknown client ID",
				tc.name, digest.iterations, pbkdf2Iterations)
		}
	}
}

func TestTimingEqualizerDigest_MatchesCurrentParameters(t *testing.T) {
	// The equalizer only equalizes if verifying it costs what verifying a real
	// digest costs. If this fails because pbkdf2Iterations moved, regenerate
	// the constant rather than relaxing the check.
	digest, err := parseSecretHash(timingEqualizerDigest)
	if err != nil {
		t.Fatalf("equalizer digest is not a PBKDF2 hash: %v", err)
	}
	if digest.iterations != pbkdf2Iterations {
		t.Errorf("equalizer iterations = %d, want %d — it must cost the same as a real verification",
			digest.iterations, pbkdf2Iterations)
	}
	if len(digest.salt) != pbkdf2SaltLength {
		t.Errorf("equalizer salt = %d bytes, want %d", len(digest.salt), pbkdf2SaltLength)
	}
	if len(digest.key) != pbkdf2KeyLength {
		t.Errorf("equalizer key = %d bytes, want %d", len(digest.key), pbkdf2KeyLength)
	}
	if isLegacySecretHash(timingEqualizerDigest) {
		t.Error("the equalizer must parse as a digest; otherwise it would itself be treated as legacy")
	}
	if verifySecret(timingEqualizerDigest, "") {
		t.Error("the equalizer digest must not match an empty secret")
	}
}
