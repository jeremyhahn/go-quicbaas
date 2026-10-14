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
	"testing"
	"time"
)

// newSessionRefreshProvider builds an OIDC provider backed by the REAL
// MemoryStorage token store. Using the production storage (rather than a test
// double) is deliberate: the defect this suite guards against was a value
// index keyed by JTI instead of by the token value, which a hand-written
// double would have papered over.
func newSessionRefreshProvider(t *testing.T, accessTTL, refreshTTL time.Duration) (*OIDCProvider, *MemoryStorage) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}

	storage := NewMemoryStorage()
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: storage,
	})

	// The principal these sessions name. It has to EXIST: a refresh is redeemed
	// only while its principal is live, and liveness resolves the uid claim.
	//
	// These tests used to seed nothing and pass, because a subject that resolved
	// to nobody was inferred to be a service principal and service principals
	// are live. That inference is gone (#390) — it also meant a renamed user's
	// sessions were silently promoted — so a session that names no live
	// principal is now correctly refused, and the fixture names one.
	if err := storage.CreateUser(context.Background(), &User{
		ID:        refreshUserID,
		TenantID:  "acme",
		Username:  "user-42",
		Name:      "Refresh User",
		Email:     "user42@example.com",
		Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed refresh principal: %v", err)
	}

	provider, err := NewOIDCProvider(&OIDCConfig{
		Issuer:           "https://refresh.example.com",
		SigningAlgorithm: "RS256",
		SigningKey:       key,
		AccessTokenTTL:   accessTTL,
		RefreshTokenTTL:  refreshTTL,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}, OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    newMockAppStorage(),
		TokenStorage:  storage,
	})
	if err != nil {
		t.Fatalf("create OIDC provider: %v", err)
	}
	return provider, storage
}

// loginSession is the options set a factor login would freeze into the refresh
// credential.
// refreshUserID is the id of the principal the refresh fixtures name. A user id
// is a qrdb Snowflake in decimal, so it can only be a number.
const refreshUserID = 71

func loginSession() SessionRefreshOptions {
	return SessionRefreshOptions{
		Subject:  "user-42",
		TenantID: "acme",
		Scopes:   []string{"openid", "profile"},
		Session: TokenSessionContext{
			// Carried exactly as IssueRefreshForSessionBearer carries it: read
			// off the bearer, so the refresh record names the same account the
			// access token does.
			UserID:      refreshUserID,
			Roles:       []string{"admin"},
			Groups:      []string{"platform"},
			Audience:    []string{"https://refresh.example.com"},
			Email:       "user42@example.com",
			AuthMethod:  "fido2",
			MFAVerified: true,
			AMR:         []string{"webauthn", "hwk"},
			ACR:         ACRHardwareAttested,
		},
	}
}

// =============================================================================
// Issuance
// =============================================================================

func TestIssueSessionRefresh_MintsResolvableCredential(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()

	value, expiresAt, err := provider.IssueSessionRefresh(ctx, loginSession())
	if err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}
	if value == "" {
		t.Fatal("IssueSessionRefresh returned an empty credential")
	}
	if expiresAt.Before(time.Now().Add(6 * 24 * time.Hour)) {
		t.Fatalf("refresh expiry %v is not ~RefreshTokenTTL away", expiresAt)
	}

	// The regression this asserts: the credential the client holds must resolve
	// its stored record. Indexing by JTI made this lookup fail every time.
	stored, err := storage.GetTokenByValue(ctx, value)
	if err != nil {
		t.Fatalf("GetTokenByValue on the issued credential: %v", err)
	}
	if stored.TokenType != TokenTypeRefresh {
		t.Fatalf("stored token type = %q, want %q", stored.TokenType, TokenTypeRefresh)
	}
	if stored.Subject != "user-42" || stored.TenantID != "acme" {
		t.Fatalf("stored principal = %s/%s, want user-42/acme", stored.TenantID, stored.Subject)
	}
	if stored.Session == nil || stored.Session.ACR != ACRHardwareAttested {
		t.Fatal("stored record did not freeze the login's authentication context")
	}
	// The secret itself must never be persisted verbatim.
	if stored.ValueHash == value {
		t.Fatal("stored ValueHash is the raw secret; it must be a hash")
	}
	if stored.ValueHash != HashTokenValue(value) {
		t.Fatal("stored ValueHash does not match the hash of the issued value")
	}
}

func TestIssueSessionRefresh_RejectsMissingSubject(t *testing.T) {
	provider, _ := newSessionRefreshProvider(t, 15*time.Minute, time.Hour)

	opts := loginSession()
	opts.Subject = ""
	_, _, err := provider.IssueSessionRefresh(context.Background(), opts)

	var want *SessionRefreshSubjectRequiredError
	if !errors.As(err, &want) {
		t.Fatalf("error = %v, want SessionRefreshSubjectRequiredError", err)
	}
}

// =============================================================================
// Redemption
// =============================================================================

func TestRefreshSession_RenewsAndRotates(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()

	value, originalExpiry, err := provider.IssueSessionRefresh(ctx, loginSession())
	if err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}

	tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	if tokens.AccessToken == "" {
		t.Fatal("RefreshSession returned no access token")
	}
	if tokens.RefreshToken == "" || tokens.RefreshToken == value {
		t.Fatal("RefreshSession did not rotate the refresh credential")
	}
	if !tokens.ExpiresAt.After(time.Now()) {
		t.Fatalf("renewed access token already expired at %v", tokens.ExpiresAt)
	}
	// The absolute session deadline must not move: rotation renews, it does
	// not extend.
	if !tokens.RefreshExpiresAt.Equal(originalExpiry) {
		t.Fatalf("refresh expiry moved from %v to %v", originalExpiry, tokens.RefreshExpiresAt)
	}

	// The renewed access token must reproduce the original session's claims.
	claims, err := provider.ValidateAccessToken(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatalf("renewed access token failed validation: %v", err)
	}
	if claims.Subject != "user-42" || claims.TenantID != "acme" {
		t.Fatalf("renewed claims principal = %s/%s", claims.TenantID, claims.Subject)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "admin" {
		t.Fatalf("renewed roles = %v, want [admin]", claims.Roles)
	}
	// mfa_verified / auth_method are the assurance claims an access token
	// carries (acr/amr are ID-token claims); a renewal must not drop them.
	if !claims.MFAVerified || claims.AuthMethod != "fido2" {
		t.Fatalf("renewed access token downgraded the login's assurance: mfa=%v method=%q",
			claims.MFAVerified, claims.AuthMethod)
	}

	// And the presented credential must now be dead.
	old, err := storage.GetTokenByValue(ctx, value)
	if err != nil {
		t.Fatalf("stored record disappeared: %v", err)
	}
	if !old.Revoked {
		t.Fatal("presented refresh credential was not revoked by rotation")
	}
}

func TestRefreshSession_ReplayOfRotatedCredentialIsRejected(t *testing.T) {
	provider, _ := newSessionRefreshProvider(t, 15*time.Minute, time.Hour)
	ctx := context.Background()

	value, _, err := provider.IssueSessionRefresh(ctx, loginSession())
	if err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}
	if _, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value}); err != nil {
		t.Fatalf("first RefreshSession: %v", err)
	}

	// Replaying the consumed credential is the stolen-token signal.
	_, err = provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value})
	var rejected *SessionRefreshRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("replay error = %v, want SessionRefreshRejectedError", err)
	}
}

func TestRefreshSession_RejectsBadInput(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, time.Hour)
	ctx := context.Background()

	t.Run("empty credential", func(t *testing.T) {
		_, err := provider.RefreshSession(ctx, SessionRefreshRequest{})
		var want *SessionRefreshTokenRequiredError
		if !errors.As(err, &want) {
			t.Fatalf("error = %v, want SessionRefreshTokenRequiredError", err)
		}
	})

	t.Run("unknown credential", func(t *testing.T) {
		_, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: "not-a-real-token"})
		var want *SessionRefreshRejectedError
		if !errors.As(err, &want) {
			t.Fatalf("error = %v, want SessionRefreshRejectedError", err)
		}
	})

	t.Run("expired credential", func(t *testing.T) {
		value, _, err := provider.IssueSessionRefresh(ctx, loginSession())
		if err != nil {
			t.Fatalf("IssueSessionRefresh: %v", err)
		}
		record, err := storage.GetTokenByValue(ctx, value)
		if err != nil {
			t.Fatalf("GetTokenByValue: %v", err)
		}
		record.Expires = time.Now().Add(-time.Minute)
		if err := storage.StoreToken(ctx, record); err != nil {
			t.Fatalf("StoreToken: %v", err)
		}

		_, err = provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value})
		var want *SessionRefreshRejectedError
		if !errors.As(err, &want) {
			t.Fatalf("error = %v, want SessionRefreshRejectedError", err)
		}
	})

	t.Run("access token presented as refresh credential", func(t *testing.T) {
		// The principal is already seeded by newSessionRefreshProvider; seeding
		// it again collides on the tenant-unique handle.
		accessToken, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
			Subject:  "user-42",
			TenantID: "acme",
		})
		if err != nil {
			t.Fatalf("IssueSessionToken: %v", err)
		}
		_, err = provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: accessToken})
		var want *SessionRefreshRejectedError
		if !errors.As(err, &want) {
			t.Fatalf("error = %v, want SessionRefreshRejectedError", err)
		}
	})
}

// =============================================================================
// DPoP sender-constraint
// =============================================================================

func TestRefreshSession_DPoPBoundSessionRequiresProof(t *testing.T) {
	provider, _ := newSessionRefreshProvider(t, 15*time.Minute, time.Hour)
	ctx := context.Background()

	opts := loginSession()
	opts.Session.Jkt = "not-the-key-you-hold"
	value, _, err := provider.IssueSessionRefresh(ctx, opts)
	if err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}

	t.Run("no proof", func(t *testing.T) {
		_, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value})
		var want *SessionRefreshDPoPRequiredError
		if !errors.As(err, &want) {
			t.Fatalf("error = %v, want SessionRefreshDPoPRequiredError", err)
		}
	})

	t.Run("malformed proof", func(t *testing.T) {
		_, err := provider.RefreshSession(ctx, SessionRefreshRequest{
			RefreshToken: value,
			DPoPProof:    "not.a.jwt",
			DPoPMethod:   "POST",
			DPoPURI:      "https://refresh.example.com/api/v1/auth/refresh",
		})
		var want *SessionRefreshDPoPRequiredError
		if !errors.As(err, &want) {
			t.Fatalf("error = %v, want SessionRefreshDPoPRequiredError", err)
		}
	})
}

// =============================================================================
// Ceremony bridge
// =============================================================================

func TestIssueRefreshForSessionBearer_MirrorsTheIssuedSession(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 24*time.Hour)
	ctx := context.Background()
	seedCeremonyUser(t, storage, "system", "ceremony-user", []string{"operator"})

	bearer, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:     "ceremony-user",
		TenantID:    "system",
		AuthMethod:  "piv",
		MFAVerified: true,
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	value, _, err := provider.IssueRefreshForSessionBearer(ctx, bearer)
	if err != nil {
		t.Fatalf("IssueRefreshForSessionBearer: %v", err)
	}

	record, err := storage.GetTokenByValue(ctx, value)
	if err != nil {
		t.Fatalf("GetTokenByValue: %v", err)
	}
	if record.Subject != "ceremony-user" || record.TenantID != "system" {
		t.Fatalf("refresh record principal = %s/%s", record.TenantID, record.Subject)
	}
	if record.Session == nil || record.Session.AuthMethod != "piv" || !record.Session.MFAVerified {
		t.Fatal("refresh record lost the ceremony's authentication context")
	}

	// End to end: the credential renews the same session.
	tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	claims, err := provider.ValidateAccessToken(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatalf("renewed token failed validation: %v", err)
	}
	if claims.Subject != "ceremony-user" || claims.AuthMethod != "piv" {
		t.Fatalf("renewed claims = %+v", claims)
	}
}

func TestIssueRefreshForSessionBearer_RejectsBadBearer(t *testing.T) {
	provider, _ := newSessionRefreshProvider(t, 15*time.Minute, time.Hour)
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		_, _, err := provider.IssueRefreshForSessionBearer(ctx, "")
		var want *SessionRefreshTokenRequiredError
		if !errors.As(err, &want) {
			t.Fatalf("error = %v, want SessionRefreshTokenRequiredError", err)
		}
	})

	t.Run("not a token this provider issued", func(t *testing.T) {
		if _, _, err := provider.IssueRefreshForSessionBearer(ctx, "eyJ.bogus.sig"); err == nil {
			t.Fatal("expected an error for an unverifiable bearer")
		}
	})
}

// =============================================================================
// Value hashing
// =============================================================================

func TestHashTokenValue(t *testing.T) {
	a := HashTokenValue("secret")
	if a == "secret" {
		t.Fatal("hash returned the input verbatim")
	}
	if a != HashTokenValue("secret") {
		t.Fatal("hash is not deterministic")
	}
	if a == HashTokenValue("secret ") {
		t.Fatal("hash collided across distinct inputs")
	}
	if len(a) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(a))
	}
}

// TestRefreshSession_RenewsAfterTheAccessTokenExpired is the condition the SPA
// actually meets in production and that no other test in this file reproduces:
// the bearer is ALREADY DEAD when the renewal is attempted. Every other case
// here refreshes a session whose access token is still valid, so a renewal path
// that quietly depended on a live bearer would pass them all.
//
// The access TTL is negative so the minted bearer is expired the instant it
// exists — deterministic, and no sleep. The test first proves the bearer is
// genuinely rejected (the control: without it, "the renewal worked" says
// nothing about expiry) and only then asserts the refresh credential still
// renews the session.
func TestRefreshSession_RenewsAfterTheAccessTokenExpired(t *testing.T) {
	provider, _ := newSessionRefreshProvider(t, -1*time.Second, 7*24*time.Hour)
	ctx := context.Background()

	value, _, err := provider.IssueSessionRefresh(ctx, loginSession())
	if err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}

	// Control: mint a bearer for this session and confirm it is already dead.
	// If this ever starts passing validation the rest of the test is vacuous.
	expired, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value})
	if err != nil {
		t.Fatalf("RefreshSession (to obtain an expired bearer): %v", err)
	}
	if _, err := provider.ValidateAccessToken(ctx, expired.AccessToken); err == nil {
		t.Fatal("control failed: the access token minted with a negative TTL validated successfully")
	}

	// The session is past its access-token boundary. The rotated refresh
	// credential must still renew it — that is the whole point of a
	// short-lived bearer, and its absence logs the operator out every TTL.
	renewed, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: expired.RefreshToken})
	if err != nil {
		t.Fatalf("renewal across the access-token boundary failed: %v", err)
	}
	if renewed.AccessToken == "" {
		t.Fatal("renewal across the expiry boundary returned no access token")
	}
	if renewed.RefreshToken == "" || renewed.RefreshToken == expired.RefreshToken {
		t.Fatal("renewal across the expiry boundary did not rotate the credential")
	}
}
