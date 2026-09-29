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

// The liveness fixtures' user ids. A user id is a qrdb Snowflake rendered in
// decimal, so it can only be a number — the constant name carries the lifecycle
// the fixture stands for.
const (
	liveUserID       = "61"
	unsetStateUserID = "62"
	suspUserID       = "63"
	lockUserID       = "64"
	disUserID        = "65"
	revUserID        = "66"
	introspectSuspID = "67"
	introspectLiveID = "68"
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

// seedLivenessUser creates a user in the provider's identity store in the given
// lifecycle state so TokenLiveness can be evaluated against it.
func seedLivenessUser(t *testing.T, provider *OIDCProvider, tenantID, userID string, state Lifecycle) {
	t.Helper()
	user := &User{
		ID:        userID,
		TenantID:  tenantID,
		Email:     userID + "@" + tenantID + ".example.com",
		Name:      "Liveness " + userID,
		Lifecycle: state,
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
	seedLivenessUser(t, provider, "tenant-a", liveUserID, LifecycleActive)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "", liveUserID, "", "jti-live")
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if !live {
		t.Fatalf("enabled user must be live")
	}
}

// TestTokenLiveness_UnsetStateIsNotLive records the removal of a fail-open.
//
// A record with no lifecycle used to be treated as live, mirroring qrdb's own
// fallback for rows written before the field existed. That is a fail-open on an
// authentication gate, and its only justification was protecting a population of
// legacy records the pre-release policy says we do not carry. An absent state
// now denies, like every other non-active state: a value nobody gave a state to
// is not evidence that it may authenticate.
func TestTokenLiveness_UnsetStateIsNotLive(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", unsetStateUserID, "")

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "", unsetStateUserID, "", "jti-legacy")
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if live {
		t.Fatalf("a record with no lifecycle must not be live")
	}
}

// TestTokenLiveness_SuspendedUser verifies a suspended user is not live.
func TestTokenLiveness_SuspendedUser(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", suspUserID, LifecycleSuspended)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "", suspUserID, "", "jti-susp")
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
	seedLivenessUser(t, provider, "tenant-a", lockUserID, LifecycleLocked)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "", lockUserID, "", "jti-lock")
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
	seedLivenessUser(t, provider, "tenant-a", disUserID, LifecycleDisabled)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "", disUserID, "", "jti-dis")
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
// TestTokenLiveness_ServiceTokenIsLive: a service principal SAYS SO, through
// the ptyp claim, and is live without a backing user.
//
// It used to assert the same outcome through a different rule — a subject that
// resolved to nobody was INFERRED to be a service principal. That inference is
// gone with the legacy path (#390): it also meant a renamed user's sessions
// were silently promoted to service principals, which is why the positive claim
// exists.
func TestTokenLiveness_ServiceTokenIsLive(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "app_default_runtime", "", PrincipalTypeService, "jti-svc")
	if err != nil {
		t.Fatalf("TokenLiveness() error = %v", err)
	}
	if !live {
		t.Fatalf("a service principal must remain live without a backing user")
	}
}

// TestTokenLiveness_Revoked verifies a revoked jti is not live.
func TestTokenLiveness_Revoked(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", revUserID, LifecycleActive)

	ctx := context.Background()
	// Mint and revoke a token so its jti is revoked in the store.
	token, jti := mintIntrospectToken(t, provider, "tenant-a", revUserID, revUserID, time.Hour)
	if err := provider.tokenService.RevokeToken(ctx, token); err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}

	live, err := provider.TokenLiveness(ctx, "tenant-a", "", revUserID, "", jti)
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

	live, err := provider.TokenLiveness(context.Background(), "tenant-a", "", "any-user", "", "jti-x")
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
		UserStorage: &erroringUserStorage{MemoryStorage: NewMemoryStorage()},
		RoleStorage: NewMemoryStorage(),
	})

	live, gerr := provider.TokenLiveness(context.Background(), "tenant-a", "", "user-x", "", "jti-y")
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
	seedLivenessUser(t, provider, "tenant-a", introspectSuspID, LifecycleSuspended)
	ctx := context.Background()

	token, _ := mintIntrospectToken(t, provider, "tenant-a", introspectSuspID, introspectSuspID, time.Hour)
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
	seedLivenessUser(t, provider, "tenant-a", introspectLiveID, LifecycleActive)
	ctx := context.Background()

	token, jti := mintIntrospectToken(t, provider, "tenant-a", introspectLiveID, introspectLiveID, time.Hour)
	resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if !resp.Active {
		t.Fatalf("enabled user token must be active")
	}
	if resp.Subject != introspectLiveID {
		t.Errorf("sub = %q, want %q", resp.Subject, introspectLiveID)
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

	token, _ := mintIntrospectToken(t, provider, "tenant-a", "app_default_svc", "", time.Hour)
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
	token, _ := mintIntrospectToken(t, provider, "tenant-a", "fc-user", "", time.Hour)

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
