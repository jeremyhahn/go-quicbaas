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

package platformtoken

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestValidate_AllowsWithoutScopeCheck proves Validate skips the scope decision:
// a token whose scp would be denied by Authorize is still accepted by Validate
// (offline mode, so no liveness call), and the projected context is returned.
func TestValidate_AllowsWithoutScopeCheck(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newAuthzValidator(t, key, now, Options{
		ModeResolver: func(string) AuthzMode { return AuthzModeOffline },
	})

	// Token carries an unrelated scope; Authorize for "keys:get" would deny.
	tok := bearerToken(t, key, now, []string{"something:else"}, "tenant-1")

	if _, err := v.Authorize(context.Background(), tok, "keys:get", "", "", ""); err == nil {
		t.Fatalf("Authorize should deny a token missing keys:get")
	}

	ac, err := v.Validate(context.Background(), tok, "", "", "")
	if err != nil {
		t.Fatalf("Validate should allow (no scope check): %v", err)
	}
	if ac.Subject != "user-1" || ac.TenantID != "tenant-1" {
		t.Fatalf("unexpected context: %+v", ac)
	}
}

// TestValidate_EnforcesLiveness proves Validate still runs the liveness check:
// an inactive (revoked/disabled) token is denied even though no scope is checked.
func TestValidate_EnforcesLiveness(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	live := &stubLiveness{results: []bool{false}}
	v := newAuthzValidator(t, key, now, Options{
		LivenessChecker: live,
		ModeResolver:    func(string) AuthzMode { return AuthzModeOnline },
	})

	tok := bearerToken(t, key, now, nil, "tenant-1")

	_, err := v.Validate(context.Background(), tok, "", "", "")
	var inactive *TokenInactiveError
	if !errors.As(err, &inactive) {
		t.Fatalf("expected TokenInactiveError, got %v", err)
	}
}

// TestValidate_ActiveAllowed proves the happy path: a valid, live token with no
// scope requirement is accepted.
func TestValidate_ActiveAllowed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	live := &stubLiveness{results: []bool{true}}
	v := newAuthzValidator(t, key, now, Options{
		LivenessChecker: live,
		ModeResolver:    func(string) AuthzMode { return AuthzModeOnline },
	})

	tok := bearerToken(t, key, now, nil, "tenant-1")
	if _, err := v.Validate(context.Background(), tok, "", "", ""); err != nil {
		t.Fatalf("Validate active allow: %v", err)
	}
}

// TestValidate_MissingChecker_FailsClosed proves Validate fails closed when a
// liveness-requiring mode has no checker configured (same posture as Authorize).
func TestValidate_MissingChecker_FailsClosed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newAuthzValidator(t, key, now, Options{
		ModeResolver: func(string) AuthzMode { return AuthzModeOnline },
	})

	tok := bearerToken(t, key, now, nil, "tenant-1")
	_, err := v.Validate(context.Background(), tok, "", "", "")
	var cfg *ConfigError
	if !errors.As(err, &cfg) {
		t.Fatalf("expected ConfigError (fail-closed), got %v", err)
	}
}

// TestValidate_RejectsInvalidToken proves the offline verification still runs:
// a garbage token is rejected before any liveness logic.
func TestValidate_RejectsInvalidToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newAuthzValidator(t, key, now, Options{
		ModeResolver: func(string) AuthzMode { return AuthzModeOffline },
	})

	if _, err := v.Validate(context.Background(), "not-a-jwt", "", "", ""); err == nil {
		t.Fatalf("Validate must reject an invalid token")
	}
}
