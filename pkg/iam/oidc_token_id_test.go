// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"errors"
	"testing"
	"time"
)

// newTokenIDProvider is newSessionIDProvider's fixture, returning the storage
// too: proving that a refused issuance left NOTHING BEHIND needs to count what
// was persisted, not only read what was returned.
func newTokenIDProvider(t *testing.T) (*OIDCProvider, *MemoryStorage) {
	t.Helper()

	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	seedCeremonyUser(t, storage, sessionIDTenant, sessionIDUser, []string{"operator"})
	return provider, storage
}

// storedTokenCount reports how many token records the store holds.
func storedTokenCount(t *testing.T, storage *MemoryStorage) int {
	t.Helper()

	storage.mu.RLock()
	defer storage.mu.RUnlock()
	return len(storage.tokens)
}

// breakTokenIDEntropy fails the jti CSPRNG for the rest of the test and returns
// the error it fails with, so a caller can assert the cause survives wrapping.
//
// It replaces tokenIDEntropy rather than the process-global RNG for the reason
// the seam exists: xkms.Rand is shared with every other test in this package.
func breakTokenIDEntropy(t *testing.T) error {
	t.Helper()

	broken := errors.New("rng unavailable")
	original := tokenIDEntropy
	tokenIDEntropy = func(int) ([]byte, error) { return nil, broken }
	t.Cleanup(func() { tokenIDEntropy = original })
	return broken
}

// TestJTI_IssuanceSucceedsWhenTheCSPRNGDoes is the positive control for the
// fail-closed test below, and it is not decoration: without it, a subtest there
// could pass because its path errored for a reason having nothing to do with
// entropy — a fixture that never reached the issuance code at all would look
// exactly like a fixture that correctly refused. This pins that each path IS
// reachable in this fixture and DOES persist a token when the CSPRNG works.
func TestJTI_IssuanceSucceedsWhenTheCSPRNGDoes(t *testing.T) {
	provider, storage := newTokenIDProvider(t)
	ctx := context.Background()

	before := storedTokenCount(t, storage)

	token, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:    sessionIDUser,
		TenantID:   sessionIDTenant,
		AuthMethod: "fido2",
	})
	if err != nil {
		t.Fatalf("IssueSessionToken with a working CSPRNG: %v", err)
	}
	if token == "" {
		t.Fatal("a successful login returned no bearer")
	}
	if got := storedTokenCount(t, storage); got <= before {
		t.Fatalf("a successful login persisted no token record (%d -> %d)", before, got)
	}

	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("validate the bearer: %v", err)
	}
	// 128 bits, hex encoded. A jti this length did not come from a clock.
	if len(claims.JTI) != 2*tokenIDEntropyBytes {
		t.Fatalf("jti = %q (%d chars), want %d hex chars of CSPRNG output",
			claims.JTI, len(claims.JTI), 2*tokenIDEntropyBytes)
	}
}

// TestJTI_IssuanceFailsClosedWhenTheCSPRNGDoes is the fault-injection half of
// #372.
//
// generateJTI used to answer a CSPRNG failure with time.Now().UnixNano() and
// report success. jti is the REVOCATION HANDLE — IsTokenRevoked resolves by it,
// and logout and RevokeToken are keyed on it — so that shipped a live token
// whose revocation record an attacker can guess, with nothing in the response
// saying generation was degraded.
//
// Every assertion here is about the TOKEN, not only about the error: a path
// that returned an error AND issued a token would satisfy "an error came back"
// and still be the defect. So each case asserts that no credential was returned
// and that the store gained no record.
func TestJTI_IssuanceFailsClosedWhenTheCSPRNGDoes(t *testing.T) {
	t.Run("a login mints no bearer", func(t *testing.T) {
		provider, storage := newTokenIDProvider(t)
		ctx := context.Background()
		broken := breakTokenIDEntropy(t)
		before := storedTokenCount(t, storage)

		token, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
			Subject:    sessionIDUser,
			TenantID:   sessionIDTenant,
			AuthMethod: "fido2",
		})
		if err == nil {
			t.Fatalf("a login succeeded with no usable token identifier and returned %q", token)
		}
		if token != "" {
			t.Fatalf("a failed login still returned a bearer: %q", token)
		}
		if got := storedTokenCount(t, storage); got != before {
			t.Fatalf("a refused login persisted %d token record(s)", got-before)
		}
		var entropy *TokenIDEntropyError
		if !errors.As(err, &entropy) {
			t.Fatalf("error = %v, want TokenIDEntropyError", err)
		}
		if !errors.Is(err, broken) {
			t.Fatalf("error %v does not unwrap to the CSPRNG failure", err)
		}
	})

	t.Run("the service-account path refuses too", func(t *testing.T) {
		// It signs its own claim map rather than going through
		// GenerateAccessToken, so it needs its own proof that it fails closed.
		provider, storage := newTokenIDProvider(t)
		ctx := context.Background()
		breakTokenIDEntropy(t)
		before := storedTokenCount(t, storage)

		set, err := provider.tokenService.IssueServiceAccountTokens(ctx, ServiceAccountTokenOptions{
			ClientID:           "svc-client",
			TenantID:           sessionIDTenant,
			CertThumbprintS256: "thumbprint",
		})
		if err == nil {
			t.Fatal("a service-account token was issued with no usable token identifier")
		}
		if set != nil {
			t.Fatalf("a failed issuance still returned a token set: %+v", set)
		}
		if got := storedTokenCount(t, storage); got != before {
			t.Fatalf("a refused issuance persisted %d token record(s)", got-before)
		}
		var entropy *TokenIDEntropyError
		if !errors.As(err, &entropy) {
			t.Fatalf("error = %v, want TokenIDEntropyError", err)
		}
	})

	t.Run("a hardware-attested login refuses too", func(t *testing.T) {
		provider, storage := newTokenIDProvider(t)
		ctx := context.Background()
		user := seedCeremonyUser(t, storage, sessionIDTenant, "hw@example.test", []string{"operator"})
		breakTokenIDEntropy(t)
		before := storedTokenCount(t, storage)

		set, err := provider.tokenService.IssueHardwareTokens(ctx, HardwareTokenOptions{
			User: user,
			AMR:  []string{"pwd"},
		})
		if err == nil {
			t.Fatal("a hardware login was issued tokens with no usable token identifier")
		}
		if set != nil {
			t.Fatalf("a failed issuance still returned a token set: %+v", set)
		}
		if got := storedTokenCount(t, storage); got != before {
			t.Fatalf("a refused issuance persisted %d token record(s)", got-before)
		}
		var entropy *TokenIDEntropyError
		if !errors.As(err, &entropy) {
			t.Fatalf("error = %v, want TokenIDEntropyError", err)
		}
	})

	t.Run("minting a refresh credential refuses too", func(t *testing.T) {
		provider, storage := newTokenIDProvider(t)
		ctx := context.Background()
		breakTokenIDEntropy(t)
		before := storedTokenCount(t, storage)

		value, _, err := provider.IssueSessionRefresh(ctx, loginSession())
		if err == nil {
			t.Fatalf("a refresh credential was minted with no usable token identifier: %q", value)
		}
		if value != "" {
			t.Fatalf("a failed issuance still returned a credential: %q", value)
		}
		if got := storedTokenCount(t, storage); got != before {
			t.Fatalf("a refused issuance persisted %d token record(s)", got-before)
		}
		var entropy *TokenIDEntropyError
		if !errors.As(err, &entropy) {
			t.Fatalf("error = %v, want TokenIDEntropyError", err)
		}
	})

	// A renewal REPLAYS its login's sid but MINTS a fresh jti. #371's renewal
	// therefore survives a CSPRNG outage and this one cannot — and that is ONE
	// RULE, not two: each identifier refuses exactly when it needs entropy and
	// cannot get it, and a renewal needs entropy only for the value it mints.
	//
	// Stated as "the two renewal paths disagree" it invites someone to make
	// them agree, which means either issuing a bearer whose identifier the
	// operator cannot revoke, or refusing a renewal that never needed
	// randomness. The judgement behind refusing here is that the first of those
	// is worse than a refused renewal.
	t.Run("a renewal mints a fresh jti, so it refuses where the sid path need not", func(t *testing.T) {
		provider, storage := newTokenIDProvider(t)
		ctx := context.Background()

		value, _, err := provider.IssueSessionRefresh(ctx, loginSession())
		if err != nil {
			t.Fatalf("IssueSessionRefresh with a working CSPRNG: %v", err)
		}

		breakTokenIDEntropy(t)
		before := storedTokenCount(t, storage)

		tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value})
		if err == nil {
			t.Fatal("a renewal succeeded with no usable token identifier")
		}
		if tokens != nil {
			t.Fatalf("a failed renewal still returned tokens: %+v", tokens)
		}
		if got := storedTokenCount(t, storage); got != before {
			t.Fatalf("a refused renewal persisted %d token record(s)", got-before)
		}
		var entropy *TokenIDEntropyError
		if !errors.As(err, &entropy) {
			t.Fatalf("error = %v, want TokenIDEntropyError", err)
		}
	})
}
