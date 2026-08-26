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
	"fmt"
	"strings"
	"testing"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// legacyBcryptFixture is a real bcrypt digest at bcrypt.DefaultCost, of
// "sk_legacy-fixture", frozen here as a constant.
//
// It is a constant rather than a generated value for a compliance reason, not
// a convenience one: x/crypto/bcrypt bypasses Go's FIPS gate, so calling
// GenerateFromPassword would execute Blowfish inside a suite running under
// GODEBUG=fips140=only -- the very primitive these tests assert production
// never runs (Codex review of #118). Since the legacy form is now refused
// without ever being verified, no test needs the digest to match any secret;
// it only needs to be a genuine bcrypt digest so the refusal is exercised
// against the real shape.
const legacyBcryptFixture = "$2a$10$G2ObqqMeJAJQ.cl.mWdTweYWrKSlVzhfzjS50Io9pvoy4i23Bfx6K"

// currentSecretHash produces the PBKDF2-HMAC-SHA256 digest a client secret is
// stored as since the #37 migration. Tests whose subject is a token or OIDC
// flow seed with this rather than a legacy digest: the credential form is
// incidental to them, and a legacy seed now makes them fail closed for a
// reason that has nothing to do with what they assert.
func currentSecretHash(t *testing.T, secret string) string {
	t.Helper()
	h, err := deriveSecretHash(secret)
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}
	return h
}

// hashChosenSecret provisions a chosen client secret the way a real caller
// must: hold it protected, hand it over, destroy it. The helper exists so the
// tests exercise the protected-memory contract rather than routing around it.
func hashChosenSecret(t *testing.T, secret string) (string, error) {
	t.Helper()
	sec, err := secure.NewSecret([]byte(secret))
	if err != nil {
		t.Fatalf("secure.NewSecret: %v", err)
	}
	defer sec.Destroy()
	return HashClientSecret(sec)
}

// assertRotationRequired asserts a refusal that names the remedy: the record is
// obsolete, not the credential wrong.
func assertRotationRequired(t *testing.T, ok bool, err error) {
	t.Helper()
	if ok {
		t.Fatal("a legacy record must never authenticate")
	}
	if !errors.Is(err, ErrSecretRotationRequired) {
		t.Fatalf("error = %v, want ErrSecretRotationRequired", err)
	}
}

// TestVerifyClientSecret_FormsAndFailures pins the post-ruling contract: the
// PBKDF2 digest is verified, and every other stored form is refused as
// rotation-required rather than reported as a wrong secret.
func TestVerifyClientSecret_FormsAndFailures(t *testing.T) {
	const secret = "sk_correct-horse"

	pbkdf2Hash, err := deriveSecretHash(secret)
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}

	cases := []struct {
		name            string
		stored          string
		plaintext       string
		wantOK          bool
		wantRotationErr bool
	}{
		{"pbkdf2 correct", pbkdf2Hash, secret, true, false},
		{"pbkdf2 wrong", pbkdf2Hash, "sk_wrong", false, false},
		// The legacy digest is refused without being verified, so the right
		// secret fares no better than the wrong one.
		{"legacy right secret", legacyBcryptFixture, "sk_legacy-fixture", false, true},
		{"legacy wrong secret", legacyBcryptFixture, "sk_wrong", false, true},
		// Anything that does not parse is unrecognized, not mismatched.
		{"empty stored", "", secret, false, true},
		{"garbage stored", "not-a-hash", secret, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := verifyClientSecret(tc.stored, tc.plaintext)
			if ok != tc.wantOK {
				t.Fatalf("verifyClientSecret() ok = %v, want %v", ok, tc.wantOK)
			}
			if got := errors.Is(err, ErrSecretRotationRequired); got != tc.wantRotationErr {
				t.Fatalf("errors.Is(err, ErrSecretRotationRequired) = %v, want %v (err = %v)", got, tc.wantRotationErr, err)
			}
		})
	}
}

// TestVerifyClientSecret_LegacyRefusedInEveryMode is the point of the ruling:
// the legacy path is gone, so the refusal does not depend on FIPS being on.
// Before, bcrypt authenticated off the FIPS path and failed closed under it;
// now there is one behaviour, and this test would catch a reintroduced
// mode-dependent fallback in whichever mode CI happened to run.
func TestVerifyClientSecret_LegacyRefusedInEveryMode(t *testing.T) {
	ok, err := verifyClientSecret(legacyBcryptFixture, "sk_legacy-fixture")
	assertRotationRequired(t, ok, err)
	t.Logf("refused with fips140=only enabled: %v", fips140.Enabled())
}

func TestMemoryStorage_ValidateClientSecret_LegacyRefusedAndNotRewritten(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()
	app := &Application{ClientID: "app_t1_mem", TenantID: "t1", ClientSecret: legacyBcryptFixture}
	if err := store.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	ok, err := store.ValidateClientSecret(ctx, "app_t1_mem", "sk_legacy-fixture")
	assertRotationRequired(t, ok, err)

	// Nothing authenticated, so nothing may be rewritten. A digest that changed
	// here would mean the refused record had been honoured and rehashed.
	got, err := store.GetApp(ctx, "app_t1_mem")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got.ClientSecret != legacyBcryptFixture {
		t.Fatalf("a refused legacy record must not be rewritten; stored changed to %s", got.ClientSecret)
	}
}

func TestMemoryStorage_ValidateClientSecret_WrongSecretIsNotRotationRequired(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()
	hash, err := deriveSecretHash("sk_right")
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}
	app := &Application{ClientID: "app_t1_wrong", TenantID: "t1", ClientSecret: hash}
	if err := store.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	// The distinction the typed error exists for: a current record with a wrong
	// secret is a credential problem, not a rotation problem.
	ok, err := store.ValidateClientSecret(ctx, "app_t1_wrong", "sk_wrong")
	if ok {
		t.Fatal("wrong secret must not authenticate")
	}
	if err != nil {
		t.Fatalf("a wrong secret against a current digest must not report an error, got %v", err)
	}
}

func TestMemoryStorage_ValidateClientSecret_PBKDF2NotRewritten(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()
	const secret = "sk_modern"
	hash, err := deriveSecretHash(secret)
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}
	app := &Application{ClientID: "app_t1_modern", TenantID: "t1", ClientSecret: hash}
	if err := store.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	if ok, err := store.ValidateClientSecret(ctx, "app_t1_modern", secret); err != nil || !ok {
		t.Fatalf("ValidateClientSecret pbkdf2 = (%v,%v), want (true,nil)", ok, err)
	}

	got, err := store.GetApp(ctx, "app_t1_modern")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got.ClientSecret != hash {
		t.Fatalf("a PBKDF2 secret must not be rewritten; was %s now %s", hash, got.ClientSecret)
	}
}

func TestBackendStorage_ValidateClientSecret_LegacyRefused(t *testing.T) {
	ctx := context.Background()
	store := NewBackendStorage(newMockKVStore())
	app := &Application{ClientID: "app_t1_backend", TenantID: "t1", ClientSecret: legacyBcryptFixture}
	if err := store.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	ok, err := store.ValidateClientSecret(ctx, "app_t1_backend", "sk_legacy-fixture")
	assertRotationRequired(t, ok, err)

	got, err := store.GetApp(ctx, "app_t1_backend")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got.ClientSecret != legacyBcryptFixture {
		t.Fatalf("a refused legacy record must not be rewritten; stored changed to %s", got.ClientSecret)
	}
}

// TestBackendStorage_ValidateClientSecret_PBKDF2 confirms the persistent path
// verifies a modern PBKDF2 secret in every mode.
func TestBackendStorage_ValidateClientSecret_PBKDF2(t *testing.T) {
	ctx := context.Background()
	store := NewBackendStorage(newMockKVStore())
	const secret = "sk_modern-backend"
	hash, err := deriveSecretHash(secret)
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}
	app := &Application{ClientID: "app_t1_modern-backend", TenantID: "t1", ClientSecret: hash}
	if err := store.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	if ok, err := store.ValidateClientSecret(ctx, "app_t1_modern-backend", secret); err != nil || !ok {
		t.Fatalf("ValidateClientSecret pbkdf2 = (%v,%v), want (true,nil)", ok, err)
	}
}

// TestValidateClient_DoesNotLeakRotationRequired guards the other half of the
// contract. The typed error is for operators and logs; the caller-facing
// response must stay the single opaque message every authentication failure
// returns, or a legacy record becomes an oracle telling an unauthenticated
// caller that this client ID exists and how its secret is stored.
func TestValidateClient_DoesNotLeakRotationRequired(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()
	app := &Application{ClientID: "app_t1_leak", TenantID: "t1", ClientSecret: legacyBcryptFixture}
	if err := store.CreateApp(ctx, app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	mgr := &AppManager{storage: store}
	_, err := mgr.ValidateClient(ctx, "app_t1_leak", "sk_legacy-fixture")
	if err == nil {
		t.Fatal("a legacy record must not authenticate")
	}
	if errors.Is(err, ErrSecretRotationRequired) {
		t.Fatalf("the rotation-required detail reached the caller: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid client credentials") {
		t.Fatalf("caller-facing error = %q, want the opaque credentials message", err.Error())
	}
}

// TestHashClientSecret_ProducesVerifiableStoredForm is the happy path for the
// exported helper: a caller that provisions an application with a secret it
// already holds gets a digest the normal verification path accepts.
func TestHashClientSecret_ProducesVerifiableStoredForm(t *testing.T) {
	const secret = "sk_operator-chosen"

	stored, err := hashChosenSecret(t, secret)
	if err != nil {
		t.Fatalf("HashClientSecret: %v", err)
	}
	if !strings.HasPrefix(stored, "$pbkdf2-sha256$") {
		t.Fatalf("stored form is not a PBKDF2 digest: %s", stored)
	}

	// The digest verifies the secret it was derived from, in every mode, and is
	// not treated as a rotation case — it is the current form, not a legacy one.
	ok, err := verifyClientSecret(stored, secret)
	if !ok || err != nil {
		t.Fatalf("verifyClientSecret(HashClientSecret(s), s) = (%v,%v), want (true,nil)", ok, err)
	}

	// Salted: the same secret hashed twice must not collide, or a stolen store
	// would reveal which clients share a secret.
	again, err := hashChosenSecret(t, secret)
	if err != nil {
		t.Fatalf("HashClientSecret second call: %v", err)
	}
	if again == stored {
		t.Fatal("two derivations of the same secret produced an identical digest; the salt is not random")
	}
	if ok, err := verifyClientSecret(again, secret); !ok || err != nil {
		t.Fatalf("the second digest does not verify the same secret: (%v,%v)", ok, err)
	}
}

// TestHashClientSecret_WrongSecretRejected is the error path: a digest must not
// authenticate anything other than the secret it was derived from.
func TestHashClientSecret_WrongSecretRejected(t *testing.T) {
	stored, err := hashChosenSecret(t, "sk_right")
	if err != nil {
		t.Fatalf("HashClientSecret: %v", err)
	}

	// A wrong secret against a current digest is a mismatch, never a rotation
	// case: the record is fine, the credential is not.
	for _, wrong := range []string{"sk_wrong", "", "sk_right "} {
		ok, err := verifyClientSecret(stored, wrong)
		if ok {
			t.Fatalf("verifyClientSecret(stored, %q) authenticated", wrong)
		}
		if err != nil {
			t.Fatalf("verifyClientSecret(stored, %q) err = %v, want nil", wrong, err)
		}
	}
}

// TestValidateClientSecret_CostsTheSameWhateverTheStoredState is the timing
// half of non-enumeration. Collapsing every failure to one error message hides
// WHAT went wrong; it does nothing about HOW LONG it took, and a legacy record
// that returned before deriving would answer in microseconds while a current
// one costs pbkdf2Iterations. That latency difference tells an unauthenticated
// caller which client IDs exist and how their secrets are stored.
//
// Asserted by counting derivations through the verifySecret seam and checking
// the work factor of what was compared against, not by stopwatch: a timing
// assertion on a shared runner is flaky, and PBKDF2 cost is determined entirely
// by the iteration count, so this measures the same property deterministically.
func TestValidateClientSecret_CostsTheSameWhateverTheStoredState(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()

	current, err := deriveSecretHash("sk_current")
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}
	seed := map[string]string{
		"app_current": current,
		"app_legacy":  legacyBcryptFixture,
		"app_garbage": "not-a-hash",
		"app_empty":   "",
	}
	for id, hash := range seed {
		if err := store.CreateApp(ctx, &Application{ClientID: id, TenantID: "t1", ClientSecret: hash}); err != nil {
			t.Fatalf("CreateApp %s: %v", id, err)
		}
	}

	real := verifySecret
	t.Cleanup(func() { verifySecret = real })

	for _, clientID := range []string{"app_current", "app_legacy", "app_garbage", "app_empty", "app_does_not_exist"} {
		t.Run(clientID, func(t *testing.T) {
			calls := 0
			var comparedAgainst []string
			verifySecret = func(stored, secret string) bool {
				calls++
				comparedAgainst = append(comparedAgainst, stored)
				return real(stored, secret)
			}

			_, _ = store.ValidateClientSecret(ctx, clientID, "sk_guess")

			if calls != 1 {
				t.Fatalf("expected exactly 1 key derivation, got %d", calls)
			}
			// Whatever was compared against must carry the accepted work
			// factor, or this record answers at a different speed than the
			// others and its state leaks. parseSecretHash succeeding IS that
			// assertion: it rejects any count other than pbkdf2Iterations
			// (secret_kdf.go, "iteration count is not the accepted work
			// factor"), so a separate digest.iterations comparison here could
			// never fire and only read as coverage it did not provide.
			for _, stored := range comparedAgainst {
				if _, err := parseSecretHash(stored); err != nil {
					t.Fatalf("compared against %q, which is not a digest at the accepted work factor: %v", stored, err)
				}
			}
		})
	}
}

// TestHashClientSecret_RefusesEmpty closes the provisioning half, and records
// WHERE the guarantee actually lives.
//
// Taking *secure.Secret rather than a string made an empty secret
// unrepresentable: secure.NewSecret refuses a zero-length input, so the
// value can never be constructed, let alone hashed. That is a stronger
// guarantee than the string signature could offer, where "" was a perfectly
// ordinary argument. HashClientSecret keeps its own guard anyway — a Secret
// built by another route could still Open() to nothing — and the nil case is
// checked because a nil pointer is the one empty input the type does permit.
func TestHashClientSecret_RefusesEmpty(t *testing.T) {
	t.Run("an empty secret cannot be constructed at all", func(t *testing.T) {
		sec, err := secure.NewSecret([]byte(""))
		if err == nil {
			sec.Destroy()
			t.Fatal("secure.NewSecret accepted an empty secret; the provisioning guarantee this test documents is gone")
		}
	})

	t.Run("nil is refused", func(t *testing.T) {
		got, err := HashClientSecret(nil)
		if !errors.Is(err, ErrSecretEmpty) {
			t.Fatalf("HashClientSecret(nil) error = %v, want ErrSecretEmpty", err)
		}
		if got != "" {
			t.Fatalf("HashClientSecret(nil) returned a digest %q; an empty secret must never be storable", got)
		}
	})
}

// TestValidateClientSecret_RefusesEmptyPresentedSecret is the end-to-end half:
// even against a record whose stored digest happens to be a digest of "", an
// empty presented secret never authenticates.
func TestValidateClientSecret_RefusesEmptyPresentedSecret(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()

	// Provisioned around the guard, as a pre-guard record would have been.
	emptyDigest, err := deriveSecretHash("")
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}
	if err := store.CreateApp(ctx, &Application{ClientID: "app_empty_secret", TenantID: "t1", ClientSecret: emptyDigest}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	ok, err := store.ValidateClientSecret(ctx, "app_empty_secret", "")
	if ok {
		t.Fatal("an empty secret must never authenticate, even against a digest of the empty string")
	}
	if !errors.Is(err, ErrSecretEmpty) {
		t.Fatalf("error = %v, want ErrSecretEmpty", err)
	}
}

// failingKVStore fails every read with an operational error, standing in for a
// backend outage as distinct from a record that is simply absent.
type failingKVStore struct {
	interfaces.KVStore
	err error
}

func (f *failingKVStore) Get(context.Context, string) ([]byte, error) { return nil, f.err }

// TestBackendStorage_ValidateClientSecret_EqualizesOnlyNotFound draws the line
// the equalizer has to respect. A missing record must cost a derivation, or
// which client IDs exist leaks through latency. Every other lookup failure
// must cost nothing: the derivation is 600,000 uncancellable iterations, so
// deriving for a caller who has already gone away burns CPU for nobody, and
// deriving during a storage outage adds that cost to every failing
// authentication — amplifying the incident it is reacting to. Neither case
// hides anything either, since a backend failure is not a per-client signal.
func TestBackendStorage_ValidateClientSecret_EqualizesOnlyNotFound(t *testing.T) {
	real := verifySecret
	t.Cleanup(func() { verifySecret = real })

	count := func(t *testing.T, run func()) int {
		t.Helper()
		calls := 0
		verifySecret = func(stored, secret string) bool {
			calls++
			return real(stored, secret)
		}
		run()
		verifySecret = real
		return calls
	}

	outage := &dberrors.QBError{Code: dberrors.ErrInternal, Op: "mock.get"}

	t.Run("not found derives", func(t *testing.T) {
		store := NewBackendStorage(newMockKVStore())
		got := count(t, func() {
			_, _ = store.ValidateClientSecret(context.Background(), "app_t1_absent", "sk_guess")
		})
		if got != 1 {
			t.Fatalf("an absent client must cost exactly 1 derivation, got %d", got)
		}
	})

	t.Run("cancelled context does not derive", func(t *testing.T) {
		store := NewBackendStorage(newMockKVStore())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var err error
		got := count(t, func() {
			_, err = store.ValidateClientSecret(ctx, "app_t1_absent", "sk_guess")
		})
		if got != 0 {
			t.Fatalf("a cancelled request must not buy a derivation, got %d", got)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled propagated", err)
		}
	})

	t.Run("backend outage does not derive", func(t *testing.T) {
		store := NewBackendStorage(&failingKVStore{KVStore: newMockKVStore(), err: outage})
		var err error
		got := count(t, func() {
			_, err = store.ValidateClientSecret(context.Background(), "app_t1_absent", "sk_guess")
		})
		if got != 0 {
			t.Fatalf("a backend outage must not add a derivation per attempt, got %d", got)
		}
		if dberrors.IsNotFound(err) {
			t.Fatalf("an operational failure must not be reported as not-found: %v", err)
		}
	})
}

// TestMemoryStorage_ValidateClientSecret_CancelledContext pins the guard that
// F4 found unpinned: deleting MemoryStorage's ctx.Err() check left the entire
// pkg/iam suite green, so the r6 regression — buying an uncancellable 300ms
// derivation for a caller that has already gone away — could return unnoticed.
// The BackendStorage mirror of this exists; this store had none.
func TestMemoryStorage_ValidateClientSecret_CancelledContext(t *testing.T) {
	store := NewMemoryStorage()
	hash, err := deriveSecretHash("sk_present")
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}
	if err := store.CreateApp(context.Background(), &Application{ClientID: "app_t1_cancel", TenantID: "t1", ClientSecret: hash}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	real := verifySecret
	t.Cleanup(func() { verifySecret = real })
	calls := 0
	verifySecret = func(stored, secret string) bool {
		calls++
		return real(stored, secret)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, err := store.ValidateClientSecret(ctx, "app_t1_cancel", "sk_present")
	if ok {
		t.Fatal("a cancelled request must not authenticate")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled propagated", err)
	}
	if calls != 0 {
		t.Fatalf("a cancelled request bought %d derivations, want 0", calls)
	}
}

// TestBackendStorage_ValidateClientSecret_RefusesEmptyPresentedSecret pins the
// guard F5 found unpinned: deleting BackendStorage's empty-secret check left
// the whole suite green, because only the MemoryStorage mirror existed. An
// empty secret must never authenticate on either store.
func TestBackendStorage_ValidateClientSecret_RefusesEmptyPresentedSecret(t *testing.T) {
	ctx := context.Background()
	store := NewBackendStorage(newMockKVStore())

	// Provisioned around HashClientSecret's guard, as a pre-guard record was.
	emptyDigest, err := deriveSecretHash("")
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}
	if err := store.CreateApp(ctx, &Application{ClientID: "app_t1_backend_empty", TenantID: "t1", ClientSecret: emptyDigest}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	ok, err := store.ValidateClientSecret(ctx, "app_t1_backend_empty", "")
	if ok {
		t.Fatal("an empty secret must never authenticate, even against a digest of the empty string")
	}
	if !errors.Is(err, ErrSecretEmpty) {
		t.Fatalf("error = %v, want ErrSecretEmpty", err)
	}
}

// TestOIDCCallSites_DoNotLeakRotationRequired pins the opaque-response property
// at EVERY call site, not just one.
//
// The typed rotation error is for operators and logs. If any caller let it
// reach the wire, a legacy record would become an oracle telling an
// unauthenticated caller that this client ID exists and how its secret is
// stored. Six call sites consume ValidateClientSecret and all six collapse
// (err != nil || !valid) into one opaque response — but until now only
// AppManager.ValidateClient was pinned, so the other five were correct by
// construction and unprotected against a future edit that "improves" the error
// by passing it through. Each is asserted here by the property, not by
// inspection (#118, advisory 2).
func TestOIDCCallSites_DoNotLeakRotationRequired(t *testing.T) {
	const clientID = "app_tenant-a_legacy"
	const secret = "sk_legacy-fixture"

	// assertOpaque fails if the rotation detail survived into what the caller
	// would see, and also if the call unexpectedly succeeded.
	assertOpaque := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("a legacy record must not authenticate")
		}
		if errors.Is(err, ErrSecretRotationRequired) {
			t.Fatalf("the rotation-required detail reached the caller: %v", err)
		}
		if strings.Contains(strings.ToLower(err.Error()), "rotat") {
			t.Fatalf("the caller-facing message names rotation: %q", err.Error())
		}
	}

	newProviderWithLegacyApp := func(t *testing.T) *OIDCProvider {
		t.Helper()
		provider, _, appStorage, _, _ := createTestOIDCProvider(t)
		appStorage.apps[clientID] = &Application{
			ClientID:     clientID,
			TenantID:     "tenant-a",
			ClientSecret: legacyBcryptFixture,
			GrantTypes:   []string{GrantTypeClientCredentials, GrantTypeAuthorizationCode, GrantTypeRefreshToken},
		}
		return provider
	}

	ctx := context.Background()

	t.Run("AuthenticateClient", func(t *testing.T) {
		assertOpaque(t, newProviderWithLegacyApp(t).AuthenticateClient(ctx, clientID, secret))
	})

	t.Run("Revoke", func(t *testing.T) {
		err := newProviderWithLegacyApp(t).Revoke(ctx, &RevokeRequest{
			ClientID: clientID, ClientSecret: secret, Token: "irrelevant",
		})
		assertOpaque(t, err)
	})

	for _, grant := range []struct {
		name string
		req  *TokenRequest
	}{
		{"client_credentials", &TokenRequest{GrantType: GrantTypeClientCredentials, ClientID: clientID, ClientSecret: secret}},
		{"authorization_code", &TokenRequest{GrantType: GrantTypeAuthorizationCode, ClientID: clientID, ClientSecret: secret, Code: "irrelevant", RedirectURI: "https://example.com/cb"}},
		{"refresh_token", &TokenRequest{GrantType: GrantTypeRefreshToken, ClientID: clientID, ClientSecret: secret, RefreshToken: "irrelevant"}},
	} {
		t.Run("Token/"+grant.name, func(t *testing.T) {
			_, err := newProviderWithLegacyApp(t).Token(ctx, grant.req)
			assertOpaque(t, err)
		})
	}
}

// capturingLogger records Warnf calls so a test can assert an operator-facing
// event actually fired.
type capturingLogger struct{ warnings []string }

func (c *capturingLogger) Warnf(format string, args ...interface{}) {
	c.warnings = append(c.warnings, fmt.Sprintf(format, args...))
}

// TestValidateClientSecret_WarnsRotationRequired pins F6. The typed error is
// deliberately invisible to the caller, so without this warning an operator
// upgrading a deployment that still holds legacy records sees a wave of
// invalid_client with nothing anywhere naming the remedy. Asserting the
// warning fires — and names the client and the remedy — is what stops the
// signal being silently dropped by a later refactor.
func TestValidateClientSecret_WarnsRotationRequired(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		build func(*capturingLogger) AppStorage
	}{
		{"MemoryStorage", func(l *capturingLogger) AppStorage { return NewMemoryStorage(WithStorageLogger(l)) }},
		{"BackendStorage", func(l *capturingLogger) AppStorage {
			return NewBackendStorage(newMockKVStore(), WithStorageLogger(l))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &capturingLogger{}
			store := tc.build(log)
			if err := store.CreateApp(ctx, &Application{ClientID: "app_t1_warn", TenantID: "t1", ClientSecret: legacyBcryptFixture}); err != nil {
				t.Fatalf("CreateApp: %v", err)
			}

			if _, err := store.ValidateClientSecret(ctx, "app_t1_warn", "sk_legacy-fixture"); !errors.Is(err, ErrSecretRotationRequired) {
				t.Fatalf("error = %v, want ErrSecretRotationRequired", err)
			}

			if len(log.warnings) != 1 {
				t.Fatalf("got %d warnings, want exactly 1: %v", len(log.warnings), log.warnings)
			}
			w := log.warnings[0]
			if !strings.Contains(w, "app_t1_warn") {
				t.Errorf("warning does not name the client: %q", w)
			}
			if !strings.Contains(strings.ToLower(w), "rotation required") {
				t.Errorf("warning does not name the remedy: %q", w)
			}
		})
	}

	t.Run("a current secret warns about nothing", func(t *testing.T) {
		log := &capturingLogger{}
		store := NewMemoryStorage(WithStorageLogger(log))
		hash, err := deriveSecretHash("sk_current")
		if err != nil {
			t.Fatalf("deriveSecretHash: %v", err)
		}
		if err := store.CreateApp(ctx, &Application{ClientID: "app_t1_quiet", TenantID: "t1", ClientSecret: hash}); err != nil {
			t.Fatalf("CreateApp: %v", err)
		}
		if _, err := store.ValidateClientSecret(ctx, "app_t1_quiet", "sk_wrong"); err != nil {
			t.Fatalf("a wrong secret against a current digest must not error: %v", err)
		}
		if len(log.warnings) != 0 {
			t.Fatalf("an ordinary bad-credentials attempt logged %v", log.warnings)
		}
	})
}
