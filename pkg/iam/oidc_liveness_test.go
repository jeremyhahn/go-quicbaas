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

// errLivenessStore is a sentinel store failure used to assert TokenLiveness is
// fail-closed when a backing store errors.
var errLivenessStore = errors.New("liveness test: store failure")

// erroringTokenStorage embeds the in-memory token mock and forces IsTokenRevoked
// to fail, so the revocation-store fail-closed path can be exercised.
type erroringTokenStorage struct {
	*mockTokenStorage
}

func (e *erroringTokenStorage) IsTokenRevoked(_ context.Context, _ string) (bool, error) {
	return false, errLivenessStore
}

// erroringUserStorage embeds the in-memory storage and forces GetUser to fail
// with a non-not-found error, so the GetUser fail-closed path can be exercised.
type erroringUserStorage struct {
	*MemoryStorage
}

func (e *erroringUserStorage) GetUser(_ context.Context, _, _ string) (*User, error) {
	return nil, errLivenessStore
}

// seedLivenessUser creates a user in the provider's identity store with the
// given disable signals so TokenLiveness can be evaluated against it.
func seedLivenessUser(t *testing.T, provider *OIDCProvider, tenantID, userID string, suspended, locked bool, state UserState) {
	t.Helper()
	user := &User{
		ID:        userID,
		TenantID:  tenantID,
		Email:     userID + "@" + tenantID + ".example.com",
		Name:      "Liveness " + userID,
		Suspended: suspended,
		Locked:    locked,
		State:     state,
		Created:   time.Now().UTC(),
		Updated:   time.Now().UTC(),
	}
	if err := provider.identityStore.userStorage.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("seed user %q: %v", userID, err)
	}
}

// TestTokenLiveness_EnabledUser verifies an unrevoked token whose subject is an
// enabled user is live.
func TestTokenLiveness_EnabledUser(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", "live-user", false, false, UserStateActive)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "live-user", "jti-live")
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if !live {
		t.Fatalf("enabled user must be live")
	}
}

// TestTokenLiveness_LegacyUnsetState verifies a user with no persisted State
// (legacy) and no disable signals is still live.
func TestTokenLiveness_LegacyUnsetState(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", "legacy-user", false, false, "")

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "legacy-user", "jti-legacy")
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if !live {
		t.Fatalf("legacy user with unset state must be live")
	}
}

// TestTokenLiveness_SuspendedUser verifies a suspended user is not live.
func TestTokenLiveness_SuspendedUser(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", "susp-user", true, false, UserStateActive)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "susp-user", "jti-susp")
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if live {
		t.Fatalf("suspended user must not be live")
	}
}

// TestTokenLiveness_LockedUser verifies a locked user is not live.
func TestTokenLiveness_LockedUser(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", "lock-user", false, true, UserStateActive)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "lock-user", "jti-lock")
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if live {
		t.Fatalf("locked user must not be live")
	}
}

// TestTokenLiveness_DisabledState verifies a user in the disabled state is not
// live.
func TestTokenLiveness_DisabledState(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", "dis-user", false, false, UserStateDisabled)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "dis-user", "jti-dis")
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if live {
		t.Fatalf("disabled user must not be live")
	}
}

// TestTokenLiveness_ServiceTokenUserNotFound verifies a subject that is not a
// user (e.g. a client_credentials service token) is live once revocation
// passes. This is the regression guard that keeps service tokens authorized.
func TestTokenLiveness_ServiceTokenUserNotFound(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "app_default_runtime", "jti-svc")
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if !live {
		t.Fatalf("service token (no backing user) must remain live")
	}
}

// TestTokenLiveness_Revoked verifies a revoked jti is not live.
func TestTokenLiveness_Revoked(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", "rev-user", false, false, UserStateActive)

	ctx := context.Background()
	// Mint and revoke a token so its jti is revoked in the store.
	token, jti := mintIntrospectToken(t, provider, "tenant-a", "rev-user", time.Hour)
	if err := provider.tokenService.RevokeToken(ctx, token); err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}

	live, err := provider.TokenLiveness(ctx, "tenant-a", "rev-user", jti)
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if live {
		t.Fatalf("revoked token must not be live")
	}
}

// TestTokenLiveness_RevocationStoreError verifies a revocation-store error is
// fail-closed: (false, err).
func TestTokenLiveness_RevocationStoreError(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	// Swap in a token storage whose IsTokenRevoked always errors.
	provider.tokenService.storage = &erroringTokenStorage{mockTokenStorage: newMockTokenStorage()}

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "any-user", "jti-x")
	if !errors.Is(err, errLivenessStore) {
		t.Fatalf("error = %v, want errLivenessStore", err)
	}
	if live {
		t.Fatalf("revocation-store error must be fail-closed (not live)")
	}
}

// TestTokenLiveness_GetUserError verifies a non-not-found GetUser error is
// fail-closed: (false, err).
func TestTokenLiveness_GetUserError(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	// Swap in an identity store whose GetUser always errors with a non-not-found
	// error so the fail-closed path (not the service-token path) is exercised.
	provider.identityStore = NewIdentityStore(IdentityStoreOptions{
		UserStorage:  &erroringUserStorage{MemoryStorage: NewMemoryStorage()},
		GroupStorage: NewMemoryStorage(),
		RoleStorage:  NewMemoryStorage(),
	})

	live, gerr := provider.TokenLiveness(context.Background(), "tenant-a", "user-x", "jti-y")
	if !errors.Is(gerr, errLivenessStore) {
		t.Fatalf("error = %v, want errLivenessStore", gerr)
	}
	if live {
		t.Fatalf("GetUser error must be fail-closed (not live)")
	}
}

// TestOIDCIntrospect_DisabledUser verifies Introspect reports active=false for
// an otherwise-valid token whose subject is a suspended user.
func TestOIDCIntrospect_DisabledUser(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", "introspect-susp", true, false, UserStateActive)
	ctx := context.Background()

	token, _ := mintIntrospectToken(t, provider, "tenant-a", "introspect-susp", time.Hour)
	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if resp.Active {
		t.Fatalf("suspended user token must be inactive")
	}
	if resp.Subject != "" {
		t.Errorf("inactive response must not leak claims")
	}
}

// TestOIDCIntrospect_EnabledUser verifies Introspect reports active=true with
// claims for a valid token whose subject is an enabled user.
func TestOIDCIntrospect_EnabledUser(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", "introspect-live", false, false, UserStateActive)
	ctx := context.Background()

	token, jti := mintIntrospectToken(t, provider, "tenant-a", "introspect-live", time.Hour)
	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if !resp.Active {
		t.Fatalf("enabled user token must be active")
	}
	if resp.Subject != "introspect-live" {
		t.Errorf("sub = %q, want introspect-live", resp.Subject)
	}
	if resp.JTI != jti {
		t.Errorf("jti = %q, want %q", resp.JTI, jti)
	}
}

// TestOIDCIntrospect_ServiceToken verifies Introspect reports active=true for a
// valid token whose subject has no backing user (service token regression).
func TestOIDCIntrospect_ServiceToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	token, _ := mintIntrospectToken(t, provider, "tenant-a", "app_default_svc", time.Hour)
	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if !resp.Active {
		t.Fatalf("service token (no backing user) must be active")
	}
	if resp.Subject != "app_default_svc" {
		t.Errorf("sub = %q, want app_default_svc", resp.Subject)
	}
}

// TestOIDCIntrospect_RevocationStoreErrorFailClosed verifies the Introspect
// rewire is fail-closed on a revocation-store error: active=false (not the
// previous fail-open behavior).
func TestOIDCIntrospect_RevocationStoreErrorFailClosed(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	// Mint a valid token while the store still works.
	token, _ := mintIntrospectToken(t, provider, "tenant-a", "fc-user", time.Hour)

	// Now swap in an erroring revocation store.
	provider.tokenService.storage = &erroringTokenStorage{mockTokenStorage: newMockTokenStorage()}

	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if resp.Active {
		t.Fatalf("revocation-store error must report active=false (fail-closed)")
	}
}
