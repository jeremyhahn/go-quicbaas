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
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// go-quicbaas #274 — the unified login ceremony's session bridge minted bearers
// with no roles, so every login into a real (non-system) tenant was
// permissionless. These tests pin the join to the IAM user record.
//
// The tenant here is deliberately NOT the system tenant: the defect was masked
// for as long as the system tenant, which passes RBAC through IsSystemTenant
// special-casing rather than through token roles, was the only tenant anyone
// had logged into.
// =============================================================================

const (
	burninTenant   = "burnin"
	burninOperator = "burnin-operator@automatethethings.com"
	operatorRoleID = "a55d3b7d-6148-8c4e-b352-509ff32e00ab"
	adminRoleID    = "cf874202-130a-8c77-9645-e8170b65568e"
)

// newCeremonyProvider builds an OIDC provider over the real MemoryStorage
// identity store, optionally with a qrdb permission resolver so the scp claim
// can be asserted end to end.
func newCeremonyProvider(t *testing.T, resolver PermissionResolver) (*OIDCProvider, *MemoryStorage) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	storage := NewMemoryStorage()
	provider, err := NewOIDCProvider(&OIDCConfig{
		Issuer:           "https://ceremony.example.com",
		SigningAlgorithm: "RS256",
		SigningKey:       key,
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  7 * 24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}, OIDCDependencies{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{
			UserStorage: storage,
			RoleStorage: storage,
		}),
		AppStorage:         newMockAppStorage(),
		TokenStorage:       storage,
		PermissionResolver: resolver,
	})
	if err != nil {
		t.Fatalf("create OIDC provider: %v", err)
	}
	return provider, storage
}

// seedCeremonyUser writes the IAM user record a ceremony login resolves to. The
// ceremony's subject is the tenant-scoped qrdb USERNAME, which the broker
// mirrors as User.Name — so Name is what the join looks the record up by.
func seedCeremonyUser(t *testing.T, storage *MemoryStorage, tenantID, username string, roles []string) *User {
	t.Helper()

	// No ID: the identity is the Snowflake the store assigns, exactly as it is
	// for a user qrdb creates. A hand-written id would have to be a canonical
	// Snowflake anyway, and inventing one per fixture only invites two fixtures
	// to share it.
	user := &User{
		TenantID: tenantID,
		// The ceremony's subject is the tenant-scoped qrdb USERNAME, so that is
		// the field the join looks the record up by. Name is the display name
		// and resolves nothing.
		Username:  username,
		Name:      username,
		Email:     username,
		Roles:     roles,
		Lifecycle: LifecycleActive,
	}
	if err := storage.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
	return user
}

// TestIssueSessionToken_JoinsIAMRolesInNonSystemTenant is the regression named
// by #274: a ceremony login in a non-system tenant, for a user holding the
// materialized builtin operator role, must mint a bearer whose claims carry
// that role — and the qrdb permission resolver must see it, so the token's scp
// grants the orchestrator verbs the login was denied before.
func TestIssueSessionToken_JoinsIAMRolesInNonSystemTenant(t *testing.T) {
	rbac := &fakeRBACClient{permsByRole: map[string][]string{
		operatorRoleID: {"orchestrator:status", "orchestrator:list"},
		adminRoleID:    {"iam:read"},
	}}
	provider, storage := newCeremonyProvider(t, &fakePermissionResolver{client: rbac})
	seedCeremonyUser(t, storage, burninTenant, burninOperator, []string{adminRoleID, operatorRoleID})
	ctx := context.Background()

	// What qrdb hands the bridge: a factor-resolved principal with NO roles.
	// That is the input the defect faithfully propagated into the bearer.
	token, expiresAt, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:     burninOperator,
		TenantID:    burninTenant,
		AuthMethod:  "fido2",
		MFAVerified: true,
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expiry %v is not in the future", expiresAt)
	}

	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if len(claims.Roles) != 2 {
		t.Fatalf("bearer roles = %v, want the record's two role UUIDs (a roles-less bearer is #274)", claims.Roles)
	}
	if !contains(claims.Roles, operatorRoleID) || !contains(claims.Roles, adminRoleID) {
		t.Fatalf("bearer roles = %v, want %v", claims.Roles, []string{adminRoleID, operatorRoleID})
	}
	if claims.Email != burninOperator {
		t.Errorf("email claim = %q, want %q", claims.Email, burninOperator)
	}
	// The subject stays the ceremony's authenticated identity. qrdb's
	// authorization middleware resolves the token subject with
	// GetUserByUsername, so a user-id subject would miss the lookup and
	// default-deny the very requests this fix exists to authorize.
	if claims.Subject != burninOperator {
		t.Errorf("subject = %q, want the ceremony identity %q", claims.Subject, burninOperator)
	}

	// The chain the issue names: the roles must reach resolveEffectivePermissions,
	// which is what turns them into the scp the authorize gate reads.
	if rbac.lastIdentity == nil {
		t.Fatal("the permission resolver was never consulted")
	}
	if !contains(rbac.lastIdentity.Roles, operatorRoleID) {
		t.Fatalf("resolver saw roles %v, want the operator role", rbac.lastIdentity.Roles)
	}
	if !contains(claims.SCP, "orchestrator:status") {
		t.Fatalf("scp = %v, want it to grant orchestrator:status", claims.SCP)
	}
}

// TestIssueSessionToken_RefusesUnresolvableSubject proves the join fails closed.
// A verified factor whose principal has no IAM user record yields no roles, and
// a roles-less bearer is a login that appears to succeed and then denies every
// request — so the login is refused instead.
func TestIssueSessionToken_RefusesUnresolvableSubject(t *testing.T) {
	provider, _ := newCeremonyProvider(t, nil)

	_, _, err := provider.IssueSessionToken(context.Background(), SessionIdentity{
		Subject:  "ghost@automatethethings.com",
		TenantID: burninTenant,
	})
	var unresolved *SessionPrincipalUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("err = %v, want SessionPrincipalUnresolvedError", err)
	}
}

// TestIssueSessionToken_UsernameEqualToAnotherUsersIDDoesNotEscalate proves the
// join resolves by username ONLY. Nothing keeps the id and username namespaces
// apart — validateUserID accepts any non-empty string and the username charset
// admits the all-digit shape a qrdb Snowflake renders as — so a join that tried
// the subject as a user id first would hand a user who named themselves after
// the admin's id the admin's roles, off a login they authenticated honestly as
// themselves.
func TestIssueSessionToken_UsernameEqualToAnotherUsersIDDoesNotEscalate(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	// The privileged account, whose ID the attacker will impersonate by name.
	privileged := &User{
		ID:        "58",
		TenantID:  burninTenant,
		Username:  "admin",
		Name:      "Admin",
		Email:     "admin@automatethethings.com",
		Roles:     []string{adminRoleID},
		Lifecycle: LifecycleActive,
	}
	if err := storage.CreateUser(ctx, privileged); err != nil {
		t.Fatalf("seed privileged user: %v", err)
	}

	// The attacker: an ordinary account whose USERNAME is the privileged
	// account's ID. They hold no roles at all.
	attacker := &User{
		ID:       "51",
		TenantID: burninTenant,
		// The attacker's HANDLE is the privileged account's id — the handle is
		// what the join resolves through, so that is where the collision has to
		// be for this test to mean anything.
		Username:  privileged.ID,
		Name:      "Attacker",
		Email:     "attacker@automatethethings.com",
		Lifecycle: LifecycleActive,
	}
	if err := storage.CreateUser(ctx, attacker); err != nil {
		t.Fatalf("seed attacker: %v", err)
	}

	token, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:  attacker.LoginName(),
		TenantID: burninTenant,
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if contains(claims.Roles, adminRoleID) {
		t.Fatalf("the session was joined to the privileged record by id collision: roles = %v", claims.Roles)
	}
	if claims.Email != attacker.Email {
		t.Fatalf("email claim = %q, want the attacker's own %q", claims.Email, attacker.Email)
	}
}

// TestIssueSessionToken_RefusesDisabledPrincipal proves a suspended, locked, or
// disabled record cannot be turned into a session. The check belongs at mint
// time because TokenLiveness resolves a subject by ID alone: a ceremony
// bearer's username subject misses there, is read as a non-user service token,
// and is reported live. Before the roles join that gap was harmless; a
// fully-privileged bearer for a disabled account would not be.
func TestIssueSessionToken_RefusesDisabledPrincipal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*User)
	}{
		{"suspended", func(u *User) { u.Lifecycle = LifecycleSuspended }},
		{"locked", func(u *User) { u.Lifecycle = LifecycleLocked }},
		{"disabled", func(u *User) { u.Lifecycle = LifecycleDisabled }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, storage := newCeremonyProvider(t, nil)
			user := seedCeremonyUser(t, storage, burninTenant, burninOperator, []string{operatorRoleID})
			tc.apply(user)
			if err := storage.UpdateUser(context.Background(), user); err != nil {
				t.Fatalf("update user: %v", err)
			}

			_, _, err := provider.IssueSessionToken(context.Background(), SessionIdentity{
				Subject:  burninOperator,
				TenantID: burninTenant,
			})
			var disabled *SessionPrincipalDisabledError
			if !errors.As(err, &disabled) {
				t.Fatalf("err = %v, want SessionPrincipalDisabledError", err)
			}
		})
	}
}

// TestRevokeSessionToken_WorksForABarrierTenantCeremonyBearer proves logout is
// effective for a tenant login.
//
// The ceremony issues from the login request's context, which carries no
// tenant, while revocation scopes itself from the bearer's own tenant_id claim.
// Storing the token metadata under the caller's context therefore filed it in
// one store and looked for it in another — and a token record that cannot be
// found reads as "not revoked", so logout returned success and the bearer kept
// working until it expired. The metadata is now stored under the token's tenant.
func TestRevokeSessionToken_WorksForABarrierTenantCeremonyBearer(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant(burninTenant)
	tokenStorage := NewBackendStorage(kv)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	users := NewMemoryStorage()
	provider, err := NewOIDCProvider(&OIDCConfig{
		Issuer:           "https://ceremony.example.com",
		SigningAlgorithm: "RS256",
		SigningKey:       key,
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  7 * 24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}, OIDCDependencies{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{
			UserStorage: users, RoleStorage: users,
		}),
		AppStorage:   newMockAppStorage(),
		TokenStorage: tokenStorage,
	})
	if err != nil {
		t.Fatalf("create OIDC provider: %v", err)
	}
	seedCeremonyUser(t, users, burninTenant, burninOperator, []string{operatorRoleID})

	// The login context carries no tenant, exactly as the REST ceremony's does.
	ctx := context.Background()
	bearer, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:  burninOperator,
		TenantID: burninTenant,
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	if _, err := provider.ValidateAccessToken(ctx, bearer); err != nil {
		t.Fatalf("the bearer must validate before logout: %v", err)
	}

	if err := provider.RevokeSessionToken(ctx, bearer); err != nil {
		t.Fatalf("RevokeSessionToken: %v", err)
	}
	if _, err := provider.ValidateAccessToken(ctx, bearer); err == nil {
		t.Fatal("the bearer still validates after logout; revocation was filed where nobody looks")
	}
}

// TestDisablingAUserEndsAnIssuedCeremonySession is the liveness half of #274.
//
// Minting real roles into a ceremony bearer changed what the pre-existing
// liveness bypass was guarding. TokenLiveness resolved the subject by ID only;
// a ceremony bearer's username subject missed, was read as a non-user service
// token, and reported live forever — so disabling an account did not end its
// session, and its refresh credential kept minting fresh bearers replaying the
// frozen roles. Harmless while the bearer carried nothing; an authorization
// bypass once it carries the user's real authority.
//
// Every gate must reject after the account is disabled: request-time liveness,
// RFC 7662 introspection, and renewal.
func TestDisablingAUserEndsAnIssuedCeremonySession(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*User)
	}{
		{"suspended", func(u *User) { u.Lifecycle = LifecycleSuspended }},
		{"locked", func(u *User) { u.Lifecycle = LifecycleLocked }},
		{"disabled", func(u *User) { u.Lifecycle = LifecycleDisabled }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, storage := newCeremonyProvider(t, nil)
			user := seedCeremonyUser(t, storage, burninTenant, burninOperator, []string{adminRoleID, operatorRoleID})
			ctx := context.Background()

			bearer, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
				Subject:     burninOperator,
				TenantID:    burninTenant,
				AuthMethod:  "fido2",
				MFAVerified: true,
			})
			if err != nil {
				t.Fatalf("IssueSessionToken: %v", err)
			}
			refresh, _, err := provider.IssueRefreshForSessionBearer(ctx, bearer)
			if err != nil {
				t.Fatalf("IssueRefreshForSessionBearer: %v", err)
			}

			claims, err := provider.ValidateAccessToken(ctx, bearer)
			if err != nil {
				t.Fatalf("ValidateAccessToken: %v", err)
			}
			if !contains(claims.Roles, operatorRoleID) {
				t.Fatalf("precondition: the bearer must carry real authority, got %v", claims.Roles)
			}
			live, err := provider.TokenLiveness(ctx, burninTenant, claims.Subject, claims.UserID, claims.PrincipalType, claims.JTI)
			if err != nil || !live {
				t.Fatalf("precondition: the session must be live before disablement (live=%v err=%v)", live, err)
			}

			// The account is disabled AFTER the session was issued.
			tc.apply(user)
			if err := storage.UpdateUser(ctx, user); err != nil {
				t.Fatalf("update user: %v", err)
			}

			live, err = provider.TokenLiveness(ctx, burninTenant, claims.Subject, claims.UserID, claims.PrincipalType, claims.JTI)
			if err != nil {
				t.Fatalf("TokenLiveness: %v", err)
			}
			if live {
				t.Error("the bearer is still live after the account was disabled")
			}

			resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: bearer})
			if err != nil {
				t.Fatalf("Introspect: %v", err)
			}
			if resp.Active {
				t.Error("introspection reports the bearer active after the account was disabled")
			}

			if _, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: refresh}); err == nil {
				t.Error("a disabled account renewed its session; refresh must die with the principal")
			}
		})
	}
}

// TestOAuthRefreshGrant_RefusesADisabledUser proves the OTHER refresh path
// applies the same gate. Two refresh paths disagreeing about what a disabled
// account may do is how a closed hole reopens: the ceremony grant would refuse
// while the OAuth2 grant kept minting.
func TestOAuthRefreshGrant_RefusesADisabledUser(t *testing.T) {
	t.Parallel()

	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	user := &User{
		ID:        "53",
		TenantID:  burninTenant,
		Name:      "oauth-user",
		Email:     "oauth@automatethethings.com",
		Roles:     []string{operatorRoleID},
		Lifecycle: LifecycleActive,
	}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	const clientID, clientSecret = "app_burnin_client", "s3cret-for-the-test"
	hashed := currentSecretHash(t, clientSecret)
	if err := provider.appStorage.CreateApp(ctx, &Application{
		ClientID:     clientID,
		ClientSecret: hashed,
		TenantID:     burninTenant,
	}); err != nil {
		t.Fatalf("register app: %v", err)
	}

	refresh, err := provider.tokenService.GenerateRefreshToken(ctx, SessionRefreshOptions{
		Subject:  user.ID,
		TenantID: user.TenantID,
		ClientID: clientID,
		Scopes:   []string{"openid"},
	})
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}

	user.Lifecycle = LifecycleSuspended
	if err := storage.UpdateUser(ctx, user); err != nil {
		t.Fatalf("update user: %v", err)
	}

	_, err = provider.handleRefreshTokenGrant(ctx, &TokenRequest{
		GrantType:    "refresh_token",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refresh,
	})
	if err == nil {
		t.Fatal("the OAuth2 refresh grant renewed a suspended account's session")
	}
	// Not merely "an error". This test seeds its own client, so a fixture that
	// the client-auth path rejects would fail it before the grant ever reached
	// the lifecycle gate -- and it would keep passing with that gate deleted.
	// That is not hypothetical: porting #37 to this branch changed the accepted
	// secret digest, and until the fixture was migrated this test was passing
	// on invalid_client. The rejection under test stays deliberately opaque
	// (invalid_grant, so account state does not leak), so naming what it must
	// NOT be is what keeps the assertion honest.
	if strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("the grant failed at client authentication, not the lifecycle gate, "+
			"so this test is not exercising suspension: %v", err)
	}
}

// TestRenamingThenDisablingEndsAnIssuedCeremonySession is the regression for the
// residual that survived resolving ceremony subjects by username.
//
// A ceremony subject is a MUTABLE username. Resolve liveness by the subject and
// a rename detaches the session from its account: both lookups miss, the token
// is reclassified as a service principal, and disabling the account no longer
// ends it. The bearer now carries an IMMUTABLE uid, and liveness resolves a user
// session by that alone, so the rename is irrelevant — every gate must still
// reject once the account is disabled or locked.
func TestRenamingThenDisablingEndsAnIssuedCeremonySession(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*User)
	}{
		{"rename then disable", func(u *User) { u.Lifecycle = LifecycleDisabled }},
		{"rename then lock", func(u *User) { u.Lifecycle = LifecycleLocked }},
		{"rename then suspend", func(u *User) { u.Lifecycle = LifecycleSuspended }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, storage := newCeremonyProvider(t, nil)
			user := seedCeremonyUser(t, storage, burninTenant, burninOperator, []string{adminRoleID, operatorRoleID})
			ctx := context.Background()

			bearer, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
				Subject:     burninOperator,
				TenantID:    burninTenant,
				AuthMethod:  "fido2",
				MFAVerified: true,
			})
			if err != nil {
				t.Fatalf("IssueSessionToken: %v", err)
			}
			refresh, _, err := provider.IssueRefreshForSessionBearer(ctx, bearer)
			if err != nil {
				t.Fatalf("IssueRefreshForSessionBearer: %v", err)
			}
			claims, err := provider.ValidateAccessToken(ctx, bearer)
			if err != nil {
				t.Fatalf("ValidateAccessToken: %v", err)
			}
			if claims.UserID != user.ID {
				t.Fatalf("bearer uid = %q, want the immutable id %q", claims.UserID, user.ID)
			}

			// RENAME the account out from under the live session, so the
			// subject frozen in the token resolves to nobody...
			user.Username = "renamed-operator"
			if err := storage.UpdateUser(ctx, user); err != nil {
				t.Fatalf("rename user: %v", err)
			}
			if _, err := storage.GetUserByUsername(ctx, burninTenant, burninOperator); err == nil {
				t.Fatal("precondition: the old username must no longer resolve")
			}
			// ...and only then disable it.
			tc.apply(user)
			if err := storage.UpdateUser(ctx, user); err != nil {
				t.Fatalf("disable user: %v", err)
			}

			live, err := provider.TokenLiveness(ctx, claims.TenantID, claims.Subject, claims.UserID, claims.PrincipalType, claims.JTI)
			if err != nil {
				t.Fatalf("TokenLiveness: %v", err)
			}
			if live {
				t.Error("a renamed-then-disabled account kept a live bearer")
			}

			resp, err := provider.Introspect(ctx, &IntrospectRequest{Token: bearer})
			if err != nil {
				t.Fatalf("Introspect: %v", err)
			}
			if resp.Active {
				t.Error("introspection reports the bearer active after rename-then-disable")
			}

			if _, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: refresh}); err == nil {
				t.Error("a renamed-then-disabled account renewed its session")
			}
		})
	}
}

// TestTokenLiveness_DeletedUserIsNotPromotedToAServicePrincipal proves the
// inference that caused the bypass is gone. A uid that no longer resolves means
// the account the token names is gone — which denies, rather than being read as
// "no user record, therefore a service token, therefore live".
func TestTokenLiveness_DeletedUserIsNotPromotedToAServicePrincipal(t *testing.T) {
	provider, _ := newCeremonyProvider(t, nil)

	live, err := provider.TokenLiveness(context.Background(), burninTenant,
		burninOperator, "usr-deleted", "", "jti-1")
	if err != nil {
		t.Fatalf("TokenLiveness: %v", err)
	}
	if live {
		t.Fatal("a token naming a nonexistent user was treated as a live service principal")
	}
}

// TestTokenLiveness_ServiceMarkerSurvivesWithNoUserRecord proves a service
// principal stays live on the strength of its POSITIVE marker, with no user
// record anywhere — and that the marker also dissolves the over-denial the
// subject-based rule carried, where a client id colliding with a disabled
// user's name refused a token that was never that user's.
func TestTokenLiveness_ServiceMarkerSurvivesWithNoUserRecord(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	const clientID = "app_burnin_runtime"

	// A disabled user deliberately named after the service client id. Under the
	// subject-based rule this collision denied the service token.
	if err := storage.CreateUser(ctx, &User{
		ID:        "52",
		TenantID:  burninTenant,
		Name:      clientID,
		Email:     "collides@automatethethings.com",
		Lifecycle: LifecycleSuspended,
	}); err != nil {
		t.Fatalf("seed colliding user: %v", err)
	}

	live, err := provider.TokenLiveness(ctx, burninTenant, clientID, "", PrincipalTypeService, "jti-svc")
	if err != nil {
		t.Fatalf("TokenLiveness: %v", err)
	}
	if !live {
		t.Fatal("a service token with a positive marker was denied; client_credentials would stop working")
	}
}

// TestOAuthRefreshTokenRedeemedAtTheSessionEndpointKeepsTheImmutableHandle
// proves every newly issued token carries the immutable handle.
//
// Refresh credentials from both issuance paths live in ONE store, so an
// OAuth-minted one can be presented to the session-refresh endpoint. If its
// frozen session carried no uid, that renewal would mint a brand-new access
// token with no immutable handle — and principalLive DENIES a token carrying
// neither uid nor a service marker, so the session would be dead at its next
// liveness check.
func TestOAuthRefreshTokenRedeemedAtTheSessionEndpointKeepsTheImmutableHandle(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	user := &User{
		ID:        "54",
		TenantID:  burninTenant,
		Name:      "oauth-session-user",
		Email:     "oauth-session@automatethethings.com",
		Roles:     []string{operatorRoleID},
		Lifecycle: LifecycleActive,
	}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	resp, err := provider.generateTokenResponse(ctx, user, "app_burnin_client", []string{"openid"}, "", "", nil)
	if err != nil {
		t.Fatalf("generateTokenResponse: %v", err)
	}

	renewed, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: resp.RefreshToken})
	if err != nil {
		t.Fatalf("RefreshSession on an OAuth-minted credential: %v", err)
	}
	claims, err := provider.ValidateAccessToken(ctx, renewed.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != user.ID {
		t.Fatalf("renewed token uid = %q, want %q — a token minted today carries no immutable handle, so principalLive will deny it and the session is dead on its next liveness check", claims.UserID, user.ID)
	}
}

// TestHardwareSessionDoesNotOutliveADeletedUser is the bypass the round-4
// review found: hardware login froze a refresh session without the immutable
// handle, so renewal fell to the legacy subject rule where a deleted account
// resolves to nobody and is read as a service principal. Deleting the user then
// did not end the session, and every renewed token inherited the same rule for
// the rest of the refresh lifetime.
func TestHardwareSessionDoesNotOutliveADeletedUser(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	user := seedCeremonyUser(t, storage, burninTenant, "hardware-operator", []string{operatorRoleID})

	set, err := provider.IssueHardwareTokens(ctx, HardwareTokenOptions{
		User:     user,
		ClientID: "app_burnin_client",
		AMR:      []string{"webauthn", "hwk"},
		ACR:      ACRHardwareAttested,
		// The hardware ACR now requires proof: IssueHardwareTokens refuses to
		// mint it from a caller's assertion alone.
		Attestation: testHardwareAttestation("cb69481e"),
	})
	if err != nil {
		t.Fatalf("IssueHardwareTokens: %v", err)
	}

	// The account is deleted after the session was issued.
	if err := storage.DeleteUser(ctx, user.TenantID, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	if _, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: set.RefreshToken}); err == nil {
		t.Fatal("a deleted user's hardware session renewed itself")
	}
}

// TestTokenLiveness_DisabledUserDeniesDespiteAnEnabledIDCollision proves the
// widening did not import the id/username collision as a bypass. A disabled
// principal whose USERNAME equals an enabled user's ID must not borrow that
// user's enablement to keep an authorized session alive.
func TestTokenLiveness_DisabledUserDeniesDespiteAnEnabledIDCollision(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	enabled := &User{
		ID:        "58",
		TenantID:  burninTenant,
		Username:  "admin",
		Name:      "Admin",
		Email:     "admin@automatethethings.com",
		Lifecycle: LifecycleActive,
	}
	if err := storage.CreateUser(ctx, enabled); err != nil {
		t.Fatalf("seed enabled user: %v", err)
	}
	suspended := &User{
		ID:        "56",
		TenantID:  burninTenant,
		Username:  enabled.ID, // username == the enabled user's id
		Name:      "Suspended",
		Email:     "suspended@automatethethings.com",
		Lifecycle: LifecycleSuspended,
	}
	if err := storage.CreateUser(ctx, suspended); err != nil {
		t.Fatalf("seed suspended user: %v", err)
	}

	live, err := provider.TokenLiveness(ctx, burninTenant, suspended.LoginName(), suspended.ID, "", "jti-1")
	if err != nil {
		t.Fatalf("TokenLiveness: %v", err)
	}
	if live {
		t.Fatal("a suspended principal stayed live by colliding with an enabled user's id")
	}
}

// TestCeremonySessionRefresh_RenewsWithTheSameRoles is the second regression
// named by #274: the refresh credential the ceremony hands back must be
// redeemable, and the renewed bearer must carry the same roles as the login
// bearer. Without the join the renewal faithfully reproduced an empty set.
func TestCeremonySessionRefresh_RenewsWithTheSameRoles(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	seedCeremonyUser(t, storage, burninTenant, burninOperator, []string{adminRoleID, operatorRoleID})
	ctx := context.Background()

	bearer, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:     burninOperator,
		TenantID:    burninTenant,
		AuthMethod:  "fido2",
		MFAVerified: true,
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	refresh, _, err := provider.IssueRefreshForSessionBearer(ctx, bearer)
	if err != nil {
		t.Fatalf("IssueRefreshForSessionBearer: %v", err)
	}

	// The refresh endpoint is public: it carries the opaque secret and nothing
	// else, so redemption runs with no tenant bound to the context.
	renewed, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: refresh})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	claims, err := provider.ValidateAccessToken(ctx, renewed.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken on the renewed bearer: %v", err)
	}
	if !contains(claims.Roles, operatorRoleID) || !contains(claims.Roles, adminRoleID) {
		t.Fatalf("renewed bearer roles = %v, want the same roles the login carried", claims.Roles)
	}
	if claims.TenantID != burninTenant {
		t.Errorf("renewed tenant = %q, want %q", claims.TenantID, burninTenant)
	}
	if !claims.MFAVerified || claims.AuthMethod != "fido2" {
		t.Errorf("renewal lost the login's authentication context: mfa=%v method=%q", claims.MFAVerified, claims.AuthMethod)
	}
}

// =============================================================================
// go-quicbaas #296 — the ceremony keys its subject by EMAIL, and #275's join
// resolved it by username. Every user whose display name is not their email —
// i.e. everyone created normally over REST, after the #289 Name/Email split —
// was locked out fail-closed at issuance, with the login BEGIN still returning
// 200 so the failure looked like a bad credential.
//
// These tests create the user through IdentityStore.CreateUser, the path REST
// uses, rather than hand-building a record. #275's regression test passed only
// because its fixture set Name == Email and so could not see the split at all.
// =============================================================================

// createRESTUser creates a user the way the REST surface does: a display name
// and an email that are genuinely different fields.
func createRESTUser(t *testing.T, storage *MemoryStorage, store *IdentityStore, tenantID, username, email string, roles []string) *User {
	t.Helper()

	// CreateUser pre-validates every role reference, as the REST path does.
	for _, id := range roles {
		err := storage.CreateRole(context.Background(), &Role{ID: id, Name: id, TenantID: tenantID})
		if err != nil && !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("seed role %q: %v", id, err)
		}
	}

	user, err := store.CreateUser(context.Background(), tenantID, CreateUserOptions{
		Email:    email,
		Username: username,
		Roles:    roles,
	})
	if err != nil {
		t.Fatalf("CreateUser(%q/%q): %v", username, email, err)
	}
	// A created user is PENDING, and pending does not authenticate — active is
	// the only state that does. The REST path this fixture stands in for issues
	// an invite whose activation performs exactly this transition, so the
	// fixture performs it too rather than pretending a fresh account can log in.
	active := LifecycleActive
	user, err = store.UpdateUser(context.Background(), tenantID, user.ID, UpdateUserOptions{Lifecycle: &active})
	if err != nil {
		t.Fatalf("activate(%q): %v", username, err)
	}
	if user.Name == user.Email {
		t.Fatalf("fixture defeats the test: Name == Email (%q) hides the very split #296 is about", user.Name)
	}
	return user
}

// TestCeremonyLoginResolvesASubjectKeyedByEmail is the #296 regression: the
// ceremony hands over the email, the user's name is something else, and the
// login must succeed with the record's roles and its immutable uid.
func TestCeremonyLoginResolvesASubjectKeyedByEmail(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: storage, RoleStorage: storage,
	})
	ctx := context.Background()

	user := createRESTUser(t, storage, identityStore, burninTenant,
		"burnin_operator", "burnin-operator@automatethethings.com", []string{adminRoleID, operatorRoleID})

	// What the ceremony hands the bridge: the EMAIL it keyed the credential by.
	token, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:     user.Email,
		TenantID:    burninTenant,
		AuthMethod:  "fido2",
		MFAVerified: true,
	})
	if err != nil {
		t.Fatalf("ceremony login for an email-keyed subject: %v (this is the #296 lockout)", err)
	}

	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != user.ID {
		t.Errorf("uid = %q, want the immutable id %q", claims.UserID, user.ID)
	}
	if !contains(claims.Roles, operatorRoleID) || !contains(claims.Roles, adminRoleID) {
		t.Errorf("roles = %v, want the record's %v", claims.Roles, user.Roles)
	}
}

// TestCeremonyLoginStillResolvesANonEmailSubject proves the username lookup
// survives as the secondary: a subject that is not an email shape cannot be an
// email lookup, and something must still resolve it.
func TestCeremonyLoginStillResolvesANonEmailSubject(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: storage, RoleStorage: storage,
	})
	ctx := context.Background()

	user := createRESTUser(t, storage, identityStore, burninTenant,
		"burnin_operator", "burnin-operator@automatethethings.com", []string{operatorRoleID})

	if _, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:  user.Name,
		TenantID: burninTenant,
	}); err != nil {
		t.Fatalf("ceremony login for a username-shaped subject: %v", err)
	}
}

// TestCeremonyLoginResolvesTheEmailOwnerDespiteANameCollision is the READ-path
// proof that the collision class is now closed by CONSTRUCTION rather than by a
// guard.
//
// The records here are forced straight into storage to stand in for pre-existing
// data: one user owns the shared string as their EMAIL, another carries it as
// their display NAME. There used to be an ambiguity refusal for exactly this,
// because both lookups ran on every subject and could answer with different
// records. They no longer can: an email-shaped subject is resolved by the email
// index and only the email index, so the attacker's name is not consulted at all
// and the owner authenticates normally.
//
// Refusing would be the wrong outcome now. It would let anyone deny a victim
// their login by naming themselves after that victim's address.
func TestCeremonyLoginResolvesTheEmailOwnerDespiteANameCollision(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	const shared = "collide@automatethethings.com"

	// The victim, whose EMAIL is the shared string.
	if err := storage.CreateUser(ctx, &User{
		ID: "57", TenantID: burninTenant,
		Name: "victim", Email: shared,
		Roles: []string{adminRoleID}, Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed victim: %v", err)
	}
	// The attacker, whose NAME is the same string.
	if err := storage.CreateUser(ctx, &User{
		ID: "51", TenantID: burninTenant,
		Name: shared, Email: "attacker@automatethethings.com",
		Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed attacker: %v", err)
	}

	token, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:  shared,
		TenantID: burninTenant,
	})
	if err != nil {
		t.Fatalf("the email's owner was denied their login: %v", err)
	}
	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != "57" {
		t.Fatalf("UserID = %q, want the email's owner %q", claims.UserID, "57")
	}
	if !contains(claims.Roles, adminRoleID) {
		t.Fatalf("roles = %v, want the victim's %q — the attacker must not be resolved", claims.Roles, adminRoleID)
	}
}

// TestCeremonyLoginRefusesAnEmailOwnedByAnotherTenant proves the global email
// index cannot mint a session outside the tenant the ceremony authenticated
// into. GetUserByEmail resolves its own owning tenant, so without the check a
// match anywhere would answer for a login here.
func TestCeremonyLoginRefusesAnEmailOwnedByAnotherTenant(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	const email = "elsewhere@automatethethings.com"
	if err := storage.CreateUser(ctx, &User{
		ID: "55", TenantID: "some-other-tenant",
		Name: "elsewhere", Email: email,
		Roles: []string{adminRoleID}, Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed foreign-tenant user: %v", err)
	}

	_, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:  email,
		TenantID: burninTenant,
	})
	var unresolved *SessionPrincipalUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("err = %v, want the login refused: the email belongs to another tenant", err)
	}
}

// TestEmailShapedDisplayNameIsRefusedAtWrite closes the takeover at its root,
// and is the reason the guard exists rather than being tidied away.
//
// The qrdb username is DERIVED from Name, and a ceremony's subject is that
// username. So an email-shaped Name lets its holder present a subject that
// resolveSubject — seeing an email shape — resolves through the EMAIL index, to
// a DIFFERENT account: the attacker authenticates honestly with their own
// factor and is handed the victim's id and roles.
//
// Barring the shape is the other half of the disjoint-shape rule. That rule is
// only sound because the two namespaces cannot overlap: qrdb usernames already
// cannot contain "@", and this keeps display-derived ones from doing so either.
func TestEmailShapedDisplayNameIsRefusedAtWrite(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: storage, RoleStorage: storage,
	})
	ctx := context.Background()
	const victimEmail = "victim@automatethethings.com"

	if err := storage.CreateRole(ctx, &Role{ID: operatorRoleID, Name: operatorRoleID, TenantID: burninTenant}); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	victim, err := store.CreateUser(ctx, burninTenant, CreateUserOptions{
		Email: victimEmail, Username: "victim_display",
		Roles: []string{operatorRoleID},
	})
	if err != nil {
		t.Fatalf("create victim: %v", err)
	}
	active := LifecycleActive
	if _, err := store.UpdateUser(ctx, burninTenant, victim.ID, UpdateUserOptions{Lifecycle: &active}); err != nil {
		t.Fatalf("activate victim: %v", err)
	}

	t.Run("at creation", func(t *testing.T) {
		_, err := store.CreateUser(ctx, burninTenant, CreateUserOptions{
			Email: "attacker@automatethethings.com", Username: "attacker",
			Name: victimEmail,
		})
		var invalid *ValidationError
		if !errors.As(err, &invalid) {
			t.Fatalf("err = %v, want the email-shaped display name refused", err)
		}
	})

	t.Run("at update", func(t *testing.T) {
		attacker, err := store.CreateUser(ctx, burninTenant, CreateUserOptions{
			Email: "attacker2@automatethethings.com", Username: "attacker2",
		})
		if err != nil {
			t.Fatalf("create attacker: %v", err)
		}
		name := victimEmail
		_, err = store.UpdateUser(ctx, burninTenant, attacker.ID, UpdateUserOptions{Name: &name})
		var invalid *ValidationError
		if !errors.As(err, &invalid) {
			t.Fatalf("err = %v, want the rename to an email-shaped name refused", err)
		}
	})

	// And the point of all of it: the email's owner still logs in, as themselves.
	token, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject: victimEmail, TenantID: burninTenant, AuthMethod: "fido2", MFAVerified: true,
	})
	if err != nil {
		t.Fatalf("the email's owner was denied their login: %v", err)
	}
	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != victim.ID {
		t.Fatalf("uid = %q, want the victim's own %q", claims.UserID, victim.ID)
	}
	if !contains(claims.Roles, operatorRoleID) {
		t.Fatalf("roles = %v, want the owner's %q", claims.Roles, operatorRoleID)
	}
}
