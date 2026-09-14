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

// =============================================================================
// The per-login session identifier (sid) — go-quicbaas#326.
//
// The property under test is a pair, and BOTH halves matter:
//
//   - two logins by the same principal into the same tenant carry DIFFERENT
//     sids, so a client can tell them apart when every other claim in the
//     bearer (uid, tenant_id) is byte-identical; and
//   - every renewal of one login carries the SAME sid, so a rotation is not
//     mistaken for a login.
//
// Asserting only the first would pass for "a fresh identifier on every token",
// which is a different claim with a different meaning — and one that would cost
// a client its cache every fifteen minutes.
// =============================================================================

// sessionIDTenant / sessionIDUser are the one principal every case below logs
// in as. They are shared deliberately: the defect is only expressible when the
// two logins agree on both.
const (
	sessionIDTenant = "acme"
	sessionIDUser   = "operator@example.test"
)

// loginTwice performs two complete, independent ceremony logins as the SAME
// principal into the SAME tenant, with no sign-out between them, and returns
// the bearer each one minted. This is tab B logging in while tab A is live.
func loginTwice(t *testing.T, provider *OIDCProvider) (string, string) {
	t.Helper()

	identity := SessionIdentity{
		Subject:     sessionIDUser,
		TenantID:    sessionIDTenant,
		AuthMethod:  "fido2",
		MFAVerified: true,
	}

	first, _, err := provider.IssueSessionToken(context.Background(), identity)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	second, _, err := provider.IssueSessionToken(context.Background(), identity)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	return first, second
}

// sessionIDOf validates a bearer and returns its sid, failing the test when the
// claim is absent — an empty sid is never an acceptable answer from a token
// this provider minted.
func sessionIDOf(t *testing.T, provider *OIDCProvider, bearer string) string {
	t.Helper()

	claims, err := provider.ValidateAccessToken(context.Background(), bearer)
	if err != nil {
		t.Fatalf("validate bearer: %v", err)
	}
	if claims.SessionID == "" {
		t.Fatal("the bearer carries no sid; a client has nothing to key its session scope on")
	}
	return claims.SessionID
}

// newSessionIDProvider builds a provider over the real MemoryStorage with the
// test principal seeded, so the ceremony login path resolves a live user record
// and the refresh path can check its liveness.
func newSessionIDProvider(t *testing.T) *OIDCProvider {
	t.Helper()

	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	seedCeremonyUser(t, storage, sessionIDTenant, sessionIDUser, []string{"operator"})
	return provider
}

// TestSessionID_TwoLoginsBySamePrincipalSameTenantDiffer is the issue's
// scenario stated exactly: same browser, same person, same tenant, second login
// with no sign-out in between. Before the sid claim nothing in the two bearers
// differed except jti and exp — neither of which distinguishes a login from a
// refresh — so a client could not tell the two sessions apart and kept serving
// the first login's cached rows under the second.
func TestSessionID_TwoLoginsBySamePrincipalSameTenantDiffer(t *testing.T) {
	provider := newSessionIDProvider(t)

	first, second := loginTwice(t, provider)

	firstClaims, err := provider.ValidateAccessToken(context.Background(), first)
	if err != nil {
		t.Fatalf("validate first bearer: %v", err)
	}
	secondClaims, err := provider.ValidateAccessToken(context.Background(), second)
	if err != nil {
		t.Fatalf("validate second bearer: %v", err)
	}

	// The premise: everything a client could previously have keyed on is the
	// same across the two logins. If this stops holding the test below is no
	// longer testing what it claims to.
	if firstClaims.UserID != secondClaims.UserID || firstClaims.TenantID != secondClaims.TenantID {
		t.Fatalf("the two logins are not the same principal in the same tenant: %s/%s vs %s/%s",
			firstClaims.TenantID, firstClaims.UserID, secondClaims.TenantID, secondClaims.UserID)
	}

	if firstClaims.SessionID == "" || secondClaims.SessionID == "" {
		t.Fatalf("a login minted no sid: first=%q second=%q", firstClaims.SessionID, secondClaims.SessionID)
	}
	if firstClaims.SessionID == secondClaims.SessionID {
		t.Fatalf("both logins carry sid %q; two logins are indistinguishable and one's cache stays in scope under the other",
			firstClaims.SessionID)
	}
}

// TestSessionID_SurvivesRenewalOfTheSameLogin is the other half. A renewal is
// the same session: it must replay the identifier its login established, or the
// claim would mean "a new token", every rotation would read as a new login, and
// a client would discard its caches every fifteen minutes.
func TestSessionID_SurvivesRenewalOfTheSameLogin(t *testing.T) {
	provider := newSessionIDProvider(t)
	ctx := context.Background()

	bearer, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:    sessionIDUser,
		TenantID:   sessionIDTenant,
		AuthMethod: "fido2",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginSID := sessionIDOf(t, provider, bearer)

	refreshToken, _, err := provider.IssueRefreshForSessionBearer(ctx, bearer)
	if err != nil {
		t.Fatalf("IssueRefreshForSessionBearer: %v", err)
	}

	// Two consecutive rotations, because the second renews a credential that was
	// itself minted by a renewal: the identifier has to survive being copied
	// forward, not merely being read once off the login's own record.
	for round := 1; round <= 2; round++ {
		tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: refreshToken})
		if err != nil {
			t.Fatalf("RefreshSession round %d: %v", round, err)
		}
		if tokens.AccessToken == bearer {
			t.Fatalf("round %d returned the presented bearer rather than a renewed one", round)
		}
		if got := sessionIDOf(t, provider, tokens.AccessToken); got != loginSID {
			t.Fatalf("round %d renewed sid = %q, want the login's %q — a rotation is the same session",
				round, got, loginSID)
		}
		refreshToken = tokens.RefreshToken
	}
}

// TestSessionID_RenewalNeverAdoptsAnotherLoginsIdentifier joins the two halves:
// renewing login 1 must keep login 1's identifier and must never come back
// carrying login 2's, which is what would let a client believe the two are one
// session.
func TestSessionID_RenewalNeverAdoptsAnotherLoginsIdentifier(t *testing.T) {
	provider := newSessionIDProvider(t)
	ctx := context.Background()

	first, second := loginTwice(t, provider)
	firstSID := sessionIDOf(t, provider, first)
	secondSID := sessionIDOf(t, provider, second)

	firstRefresh, _, err := provider.IssueRefreshForSessionBearer(ctx, first)
	if err != nil {
		t.Fatalf("refresh credential for the first login: %v", err)
	}
	renewed, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: firstRefresh})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	renewedSID := sessionIDOf(t, provider, renewed.AccessToken)
	if renewedSID != firstSID {
		t.Fatalf("renewed sid = %q, want the first login's %q", renewedSID, firstSID)
	}
	if renewedSID == secondSID {
		t.Fatalf("the first login's renewal carries the second login's sid %q", secondSID)
	}
}

// TestSessionID_LegacySessionRenewsWithAFreshIdentifier covers the input the
// claim cannot be present on: a refresh record frozen before sid existed. The
// renewed token must still carry one — an empty sid would leave a client with
// nothing to key on and no way to know it — and because the login it belongs to
// cannot be recovered, each renewal is honestly a different one. That reads to
// a client as a new session, which costs a cache and never shares a scope.
func TestSessionID_LegacySessionRenewsWithAFreshIdentifier(t *testing.T) {
	provider := newSessionIDProvider(t)
	ctx := context.Background()

	// A session whose frozen context predates the claim: no SessionID.
	legacy := loginSession()
	legacy.Subject = sessionIDUser
	legacy.TenantID = sessionIDTenant
	if legacy.Session.SessionID != "" {
		t.Fatal("fixture already carries a sid; this case must start without one")
	}

	refreshToken, _, err := provider.IssueSessionRefresh(ctx, legacy)
	if err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}

	firstRenewal, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: refreshToken})
	if err != nil {
		t.Fatalf("first renewal: %v", err)
	}
	secondRenewal, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: firstRenewal.RefreshToken})
	if err != nil {
		t.Fatalf("second renewal: %v", err)
	}

	firstSID := sessionIDOf(t, provider, firstRenewal.AccessToken)
	secondSID := sessionIDOf(t, provider, secondRenewal.AccessToken)
	if firstSID == secondSID {
		t.Fatalf("both renewals of a pre-claim session carry sid %q; the identifier was invented and then treated as authoritative",
			firstSID)
	}
}

// TestGenerateTokenResponseForSession_MintsForALoginAndReplaysForARenewal
// pins the OAuth grant path, where the same function serves both events and the
// caller states which it is. A fresh login gets a new identifier that is frozen
// into the refresh record; a rotation supplies the session's own and must not
// mint a replacement.
func TestGenerateTokenResponseForSession_MintsForALoginAndReplaysForARenewal(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()
	user := seedCeremonyUser(t, storage, sessionIDTenant, sessionIDUser, []string{"operator"})

	login, err := provider.generateTokenResponseForSession(ctx, user, "app_client", []string{"openid"}, "", "", nil, grantSession{})
	if err != nil {
		t.Fatalf("login grant: %v", err)
	}
	loginSID := sessionIDOf(t, provider, login.AccessToken)

	// The refresh record must name the login's session, or the next rotation has
	// nothing to replay.
	stored, err := storage.GetTokenByValue(ctx, login.RefreshToken)
	if err != nil {
		t.Fatalf("resolve the issued refresh credential: %v", err)
	}
	if stored.Session == nil || stored.Session.SessionID != loginSID {
		t.Fatalf("refresh record froze sid %v, want the login's %q", stored.Session, loginSID)
	}

	renewed, err := provider.generateTokenResponseForSession(ctx, user, "app_client", []string{"openid"}, "", "", nil, grantSession{ID: loginSID, Renewal: true})
	if err != nil {
		t.Fatalf("renewal grant: %v", err)
	}
	if got := sessionIDOf(t, provider, renewed.AccessToken); got != loginSID {
		t.Fatalf("renewed sid = %q, want the supplied %q", got, loginSID)
	}
	if renewed.AccessToken == login.AccessToken {
		t.Fatal("the renewal returned the login's own bearer; the case proves nothing")
	}
}

// TestGenerateTokenResponseForSession_LegacyRenewalDoesNotAdoptAMintedIdentifier
// is the OAuth half of the pre-claim case, and the reason grantSession carries
// Renewal rather than just an id.
//
// A rotation of a session recorded before sid existed has no identifier to
// replay, so the access token gets a minted one — unavoidable, and harmless
// while it stays a per-token value. Freezing THAT into the rotated refresh
// record would not be harmless: the session would replay it from then on and
// become indistinguishable from one whose login established an identifier,
// having recovered nothing. The record must stay empty, exactly as RefreshSession
// leaves it, so every renewal of a legacy session keeps reading as a new one.
func TestGenerateTokenResponseForSession_LegacyRenewalDoesNotAdoptAMintedIdentifier(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()
	user := seedCeremonyUser(t, storage, sessionIDTenant, sessionIDUser, []string{"operator"})

	// A renewal that was given no identifier: the pre-claim session.
	legacy := grantSession{Renewal: true}

	first, err := provider.generateTokenResponseForSession(ctx, user, "app_client", []string{"openid"}, "", "", nil, legacy)
	if err != nil {
		t.Fatalf("first legacy renewal: %v", err)
	}
	// The bearer still carries one — a client must never be handed a token with
	// nothing to key on.
	firstSID := sessionIDOf(t, provider, first.AccessToken)

	stored, err := storage.GetTokenByValue(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("resolve the rotated refresh credential: %v", err)
	}
	if stored.Session == nil {
		t.Fatal("the rotated record froze no session context at all")
	}
	if stored.Session.SessionID != "" {
		t.Fatalf("the rotated record adopted sid %q, which no login established; the session would replay a guess from here on",
			stored.Session.SessionID)
	}

	// And the consequence that matters: the next renewal is still a new session.
	second, err := provider.generateTokenResponseForSession(ctx, user, "app_client", []string{"openid"}, "", "", nil,
		grantSession{ID: refreshedSessionID(stored), Renewal: true})
	if err != nil {
		t.Fatalf("second legacy renewal: %v", err)
	}
	if got := sessionIDOf(t, provider, second.AccessToken); got == firstSID {
		t.Fatalf("both legacy renewals carry sid %q; the first rotation's minted value became authoritative", got)
	}
}

// TestRefreshTokenGrant_RenewsTheSessionItWasGiven drives the PUBLIC OAuth
// refresh_token grant, because the two tests above call
// generateTokenResponseForSession directly and so cannot see the one thing that
// went wrong: the CALL SITE. Deleting `Renewal: true` from handleRefreshTokenGrant
// leaves both of them passing while the defect is fully restored.
//
// Both halves in one case, because they are one rule: a record that names a
// login replays it, a record that names none stays nameless.
func TestRefreshTokenGrant_RenewsTheSessionItWasGiven(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()
	user := seedCeremonyUser(t, storage, sessionIDTenant, sessionIDUser, []string{"operator"})

	const clientID, clientSecret = "app_session_id_client", "s3cret-for-the-test"
	// The current PBKDF2-HMAC-SHA256 digest, not bcrypt. Since #37 a stored
	// bcrypt value is refused outright rather than compared, so seeding one
	// here would fail client authentication before this test reached the
	// session behaviour it is about.
	hashed := currentSecretHash(t, clientSecret)
	if err := provider.appStorage.CreateApp(ctx, &Application{
		ClientID:     clientID,
		ClientSecret: hashed,
		TenantID:     sessionIDTenant,
		GrantTypes:   []string{GrantTypeRefreshToken},
	}); err != nil {
		t.Fatalf("register app: %v", err)
	}

	// redeem runs one grant and returns the renewed bearer's sid together with
	// what the rotated record froze.
	redeem := func(t *testing.T, refreshToken string) (string, string) {
		t.Helper()
		resp, err := provider.handleRefreshTokenGrant(ctx, &TokenRequest{
			GrantType:    "refresh_token",
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RefreshToken: refreshToken,
		})
		if err != nil {
			t.Fatalf("refresh_token grant: %v", err)
		}
		rotated, err := storage.GetTokenByValue(ctx, resp.RefreshToken)
		if err != nil {
			t.Fatalf("resolve the rotated credential: %v", err)
		}
		return sessionIDOf(t, provider, resp.AccessToken), refreshedSessionID(rotated)
	}

	newRefresh := func(t *testing.T, session TokenSessionContext) string {
		t.Helper()
		value, err := provider.tokenService.GenerateRefreshToken(ctx, SessionRefreshOptions{
			Subject:  user.ID,
			TenantID: user.TenantID,
			ClientID: clientID,
			Scopes:   []string{"openid"},
			Session:  session,
		})
		if err != nil {
			t.Fatalf("GenerateRefreshToken: %v", err)
		}
		return value
	}

	t.Run("a record naming a login replays it", func(t *testing.T) {
		const loginSID = "sess-established-at-login"
		bearerSID, frozen := redeem(t, newRefresh(t, TokenSessionContext{
			UserID:    user.ID,
			SessionID: loginSID,
		}))
		if bearerSID != loginSID {
			t.Fatalf("renewed bearer sid = %q, want the login's %q", bearerSID, loginSID)
		}
		if frozen != loginSID {
			t.Fatalf("rotated record froze %q, want the login's %q — the next renewal would lose it", frozen, loginSID)
		}
	})

	t.Run("a record naming none stays nameless", func(t *testing.T) {
		bearerSID, frozen := redeem(t, newRefresh(t, TokenSessionContext{UserID: user.ID}))
		if bearerSID == "" {
			t.Fatal("the renewed bearer carries no sid; a client is left with nothing to key on")
		}
		if frozen != "" {
			t.Fatalf("rotated record adopted %q, which no login established", frozen)
		}
	})
}

// TestSessionID_IssuanceFailsClosedWhenTheCSPRNGDoes is the fault-injection
// half, and the reason generateSessionID keeps its own entropy seam rather than
// delegating to generateJTI.
//
// generateJTI answered a CSPRNG failure with time.Now().UnixNano() until #372;
// for a session identifier that was the wrong answer twice over, because the
// value becomes predictable AND two logins minted close enough together share
// it — at which point the first login's cached data is in scope under the
// second, which is the whole defect this claim was added to close. Both helpers
// now refuse: a degraded identifier is worse than no token, so there is no
// token. TestJTI_IssuanceFailsClosedWhenTheCSPRNGDoes is the jti half.
func TestSessionID_IssuanceFailsClosedWhenTheCSPRNGDoes(t *testing.T) {
	provider := newSessionIDProvider(t)
	ctx := context.Background()

	broken := errors.New("rng unavailable")
	original := sessionIDEntropy
	sessionIDEntropy = func(int) ([]byte, error) { return nil, broken }
	t.Cleanup(func() { sessionIDEntropy = original })

	t.Run("a login mints no bearer", func(t *testing.T) {
		token, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
			Subject:    sessionIDUser,
			TenantID:   sessionIDTenant,
			AuthMethod: "fido2",
		})
		if err == nil {
			t.Fatalf("a login succeeded with no usable session identifier and returned %q", token)
		}
		if token != "" {
			t.Fatalf("a failed login still returned a bearer: %q", token)
		}
		var entropy *SessionIDEntropyError
		if !errors.As(err, &entropy) {
			t.Fatalf("error = %v, want SessionIDEntropyError", err)
		}
		if !errors.Is(err, broken) {
			t.Fatalf("error %v does not unwrap to the CSPRNG failure", err)
		}
	})

	t.Run("the service-account path refuses too", func(t *testing.T) {
		// It signs its own claim map rather than going through
		// GenerateAccessToken, so it needs its own proof that it fails closed.
		set, err := provider.tokenService.IssueServiceAccountTokens(ctx, ServiceAccountTokenOptions{
			ClientID:           "svc-client",
			TenantID:           sessionIDTenant,
			CertThumbprintS256: "thumbprint",
		})
		if err == nil {
			t.Fatal("a service-account token was issued with no usable session identifier")
		}
		if set != nil {
			t.Fatalf("a failed issuance still returned a token set: %+v", set)
		}
		var entropy *SessionIDEntropyError
		if !errors.As(err, &entropy) {
			t.Fatalf("error = %v, want SessionIDEntropyError", err)
		}
	})

	t.Run("a renewal replaying an identifier does not need the CSPRNG", func(t *testing.T) {
		// The other half of failing closed: only MINTING needs randomness. A
		// renewal replays what its login established, so a broken CSPRNG must
		// not take down every live session's ability to renew.
		claims := &Claims{
			Issuer:    provider.config.Issuer,
			Subject:   sessionIDUser,
			Audience:  BuildAudience(provider.config.Issuer, ""),
			ExpiresAt: NewUnixTime(time.Now().Add(time.Minute)),
			IssuedAt:  NewUnixTime(time.Now()),
			JTI:       mustGenerateJTI(t),
			SessionID: "sess-established-at-login",
			TenantID:  sessionIDTenant,
		}
		token, err := provider.tokenService.GenerateAccessToken(ctx, claims)
		if err != nil {
			t.Fatalf("a renewal that needed no new identifier was refused: %v", err)
		}
		if got := sessionIDOf(t, provider, token); got != "sess-established-at-login" {
			t.Fatalf("renewed sid = %q, want the replayed one", got)
		}
	})
}

// TestRefreshedSessionID_ReadsNothingFromAnIncompleteRecord is the error half of
// the accessor: a nil token, or one stored without a session context, must
// answer "" so the caller mints a fresh identifier — never panic, and never
// report an identifier that was not recorded.
func TestRefreshedSessionID_ReadsNothingFromAnIncompleteRecord(t *testing.T) {
	if got := refreshedSessionID(nil); got != "" {
		t.Fatalf("refreshedSessionID(nil) = %q, want empty", got)
	}
	if got := refreshedSessionID(&Token{}); got != "" {
		t.Fatalf("a record with no session context yielded %q, want empty", got)
	}
	if got := refreshedSessionID(&Token{Session: &TokenSessionContext{SessionID: "sess-1"}}); got != "sess-1" {
		t.Fatalf("recorded sid = %q, want sess-1", got)
	}
}
