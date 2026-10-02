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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// =============================================================================
// Session refresh — the renewal half of a ceremony/hardware login.
//
// Access tokens are DELIBERATELY short-lived (15m, hard-capped at 60m by
// config.validateAuth) to bound the replay window of a stolen bearer. That is
// only a usable posture if the session can be renewed without re-running the
// factor ceremony, which is what this file provides.
//
// Design:
//
//   - The refresh credential is an OPAQUE, high-entropy secret. It is never a
//     JWT: it must be revocable server-side and must not be independently
//     verifiable by a resource server.
//   - It is stored hashed-by-value (Token.ValueHash) so the presented secret is
//     the lookup handle and the secret itself never becomes a storage key.
//   - It is ROTATED on every use: the presented token is revoked and a new one
//     is issued. A replayed (already-rotated) token therefore resolves to a
//     revoked record and is refused — the standard OAuth 2.1 detection for a
//     stolen refresh token.
//   - Rotation inherits the ORIGINAL absolute expiry. A session cannot be
//     extended indefinitely by refreshing; it lives at most RefreshTokenTTL
//     from the login that created it.
//   - A DPoP-bound session (cnf.jkt recorded at login) requires a matching DPoP
//     proof on every refresh, and the renewed access token stays bound to the
//     same key. A plain-bearer session is never silently upgraded.
//
// A refresh RENEWS a session; it does not re-authenticate and it does not
// re-authorize. The frozen TokenSessionContext is authoritative for the renewed
// access token's claims (see its doc comment for why that is safe).
// =============================================================================

// HashTokenValue returns the lowercase hex SHA-256 of an opaque token value. It
// is the index key under which refresh tokens are stored and resolved, and is
// exported so storage backends share exactly one derivation.
func HashTokenValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// refreshTokenEntropyBytes is the size of the random refresh-token secret. 256
// bits leaves no practical margin for guessing a value that is accepted without
// any other credential.
const refreshTokenEntropyBytes = 32

// SessionRefreshOptions describes the session a refresh credential renews. It
// is populated by the issuance side (login ceremony or hardware login) and
// frozen into the stored token record.
type SessionRefreshOptions struct {
	// Subject is the authenticated principal.
	Subject string
	// TenantID is the tenant namespace the principal authenticated into.
	TenantID string
	// ClientID is the OAuth client, when the session was minted for one.
	ClientID string
	// Scopes are the granted scopes.
	Scopes []string
	// Session is the frozen authentication context replayed onto every renewed
	// access token.
	Session TokenSessionContext
}

// SessionRefreshRequest is a renewal attempt.
type SessionRefreshRequest struct {
	// RefreshToken is the opaque credential previously issued to the client.
	RefreshToken string
	// DPoPProof, when present, is the RFC 9449 proof JWT accompanying the
	// request. It is REQUIRED when the session is DPoP-bound.
	DPoPProof string
	// DPoPMethod / DPoPURI supply the proof's htm/htu binding.
	DPoPMethod string
	DPoPURI    string
}

// SessionTokens is the renewed credential pair handed back to the client.
type SessionTokens struct {
	// AccessToken is the renewed short-lived bearer.
	AccessToken string
	// ExpiresAt is the access token's expiry.
	ExpiresAt time.Time
	// RefreshToken is the ROTATED refresh credential. The caller MUST persist
	// it: the token it presented is now revoked.
	RefreshToken string
	// RefreshExpiresAt is the session's absolute expiry; it does not move.
	RefreshExpiresAt time.Time
	// TokenType is "Bearer", or "DPoP" for a sender-constrained session.
	TokenType string
}

// IssueSessionRefresh mints and stores an opaque refresh credential for a
// freshly authenticated session. The returned secret is the only copy — the
// store keeps a hash.
func (p *OIDCProvider) IssueSessionRefresh(ctx context.Context, opts SessionRefreshOptions) (string, time.Time, error) {
	if opts.Subject == "" {
		return "", time.Time{}, &SessionRefreshSubjectRequiredError{}
	}
	if p.tokenService == nil {
		return "", time.Time{}, &SessionRefreshUnavailableError{}
	}
	return p.tokenService.issueRefreshCredential(ctx, opts, time.Time{})
}

// IssueRefreshForSessionBearer mints the renewal credential for a session whose
// access token was just issued.
//
// It is the entry point for the unified login ceremony. qrdb's SessionIssuer
// contract returns only (bearer, expiry), so the ceremony cannot hand quicbaas
// a structured identity for the refresh side; instead the provider re-reads the
// claims of the bearer IT just minted and freezes them into the refresh record.
// That keeps every token detail — audience, roles, acr/amr, cnf.jkt — inside
// the OIDC layer instead of leaking a claims model into the service facade,
// and guarantees the renewal reproduces exactly the session that was issued.
func (p *OIDCProvider) IssueRefreshForSessionBearer(ctx context.Context, bearer string) (string, time.Time, error) {
	if bearer == "" {
		return "", time.Time{}, &SessionRefreshTokenRequiredError{}
	}
	if p.tokenService == nil {
		return "", time.Time{}, &SessionRefreshUnavailableError{}
	}
	claims, err := p.ValidateAccessToken(ctx, bearer)
	if err != nil {
		return "", time.Time{}, err
	}

	session := TokenSessionContext{
		// Read back off the bearer this provider just minted, so the refresh
		// record names the same login the access token does.
		SessionID:     claims.SessionID,
		UserID:        claims.UserID,
		PrincipalType: claims.PrincipalType,
		Roles:         claims.Roles,
		Groups:        claims.Groups,
		Audience:      claims.Audience,
		Email:         claims.Email,
		Name:          claims.Name,
		AuthMethod:    claims.AuthMethod,
		MFAVerified:   claims.MFAVerified,
		AMR:           claims.AMR,
		ACR:           claims.ACR,
		AttestLevel:   claims.AttestLevel,
		AttestAAGUID:  claims.AttestAAGUID,
	}
	if claims.Confirmation != nil {
		session.Jkt = claims.Confirmation.Jkt
	}

	return p.IssueSessionRefresh(ctx, SessionRefreshOptions{
		Subject:  claims.Subject,
		TenantID: claims.TenantID,
		ClientID: claims.ClientID,
		Scopes:   parseScopes(claims.Scope),
		Session:  session,
	})
}

// RefreshSession redeems a refresh credential: it validates and rotates the
// presented token and mints a renewed access token for the same session.
func (p *OIDCProvider) RefreshSession(ctx context.Context, req SessionRefreshRequest) (*SessionTokens, error) {
	if req.RefreshToken == "" {
		return nil, &SessionRefreshTokenRequiredError{}
	}
	if p.tokenService == nil {
		return nil, &SessionRefreshUnavailableError{}
	}

	stored, err := p.tokenService.storage.GetTokenByValue(ctx, req.RefreshToken)
	if err != nil || stored == nil {
		return nil, &SessionRefreshRejectedError{Reason: "unknown refresh token"}
	}
	if stored.TokenType != TokenTypeRefresh {
		return nil, &SessionRefreshRejectedError{Reason: "not a refresh token"}
	}
	if stored.Revoked {
		return nil, &SessionRefreshRejectedError{Reason: "refresh token revoked"}
	}
	// IsTokenRevoked is the canonical revocation query: a backend may track
	// revocations separately from the stored record, and a rotated credential
	// MUST be refused on replay.
	if revoked, err := p.tokenService.storage.IsTokenRevoked(withTokenTenant(ctx, stored.TenantID), stored.JTI); err == nil && revoked {
		return nil, &SessionRefreshRejectedError{Reason: "refresh token revoked"}
	}
	if !stored.Expires.IsZero() && !time.Now().Before(stored.Expires) {
		return nil, &SessionRefreshRejectedError{Reason: "refresh token expired"}
	}

	session := TokenSessionContext{}
	if stored.Session != nil {
		session = *stored.Session
	}

	// A refresh renews a session; it must not outlive the principal's right to
	// hold one. The renewed access token replays the session's FROZEN roles, so
	// without this gate disabling an account would leave its holder able to mint
	// a fresh, fully-privileged bearer every fifteen minutes for the whole
	// refresh window — the access-token liveness check alone cannot stop that,
	// because each renewal is a brand-new token. The session's frozen UserID is
	// the immutable handle, so a rename between login and renewal changes
	// nothing here.
	live, err := p.principalLive(withTokenTenant(ctx, stored.TenantID), stored.TenantID, stored.Subject, session.UserID, session.PrincipalType)
	if err != nil {
		return nil, &SessionRefreshRejectedError{Reason: "principal liveness could not be established"}
	}
	if !live {
		return nil, &SessionRefreshRejectedError{Reason: "principal is not enabled"}
	}

	// RFC 9449: a sender-constrained session may only be renewed by the holder
	// of the bound key. An unbound session is never upgraded from a presented
	// proof — the binding is established at login, not at renewal.
	cnf, err := p.confirmSessionRefreshDPoP(session, req)
	if err != nil {
		return nil, err
	}

	// Rotate BEFORE minting so a failure downstream cannot leave two live
	// refresh credentials for one session.
	if err := p.tokenService.storage.RevokeToken(withTokenTenant(ctx, stored.TenantID), stored.JTI); err != nil {
		return nil, &SessionRefreshRejectedError{Reason: "refresh token could not be rotated"}
	}

	now := time.Now()
	expiresAt := now.Add(p.config.AccessTokenTTL)
	jti, err := generateJTI()
	if err != nil {
		return nil, err
	}

	claims := &Claims{
		Issuer:    p.config.Issuer,
		Subject:   stored.Subject,
		Audience:  sessionRefreshAudience(session, p.config.Issuer),
		ExpiresAt: NewUnixTime(expiresAt),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       jti,
		// A renewal is the SAME session: it replays the identifier its login
		// established rather than minting one, which is what lets a client tell
		// this apart from a second login by the same principal into the same
		// tenant. A session frozen before the claim existed carries none and
		// GenerateAccessToken mints a fresh one, so an old session degrades to
		// "every renewal looks new" rather than to two logins looking alike.
		SessionID:     session.SessionID,
		TenantID:      stored.TenantID,
		UserID:        session.UserID,
		PrincipalType: session.PrincipalType,
		Roles:         session.Roles,
		Groups:        session.Groups,
		Email:         session.Email,
		Name:          session.Name,
		ClientID:      stored.ClientID,
		Scope:         joinScopes(stored.Scopes),
		AuthMethod:    session.AuthMethod,
		MFAVerified:   session.MFAVerified,
		AMR:           session.AMR,
		ACR:           session.ACR,
		// Replayed, never re-derived: a refresh does not re-run the ceremony,
		// so the renewed bearer asserts exactly what the login proved. A
		// session that proved nothing carries nothing here and renews the same
		// way.
		AttestLevel:  session.AttestLevel,
		AttestAAGUID: session.AttestAAGUID,
		Confirmation: cnf,
	}
	accessToken, err := p.tokenService.GenerateAccessToken(ctx, claims)
	if err != nil {
		return nil, err
	}

	rotated, refreshExpiresAt, err := p.tokenService.issueRefreshCredential(ctx, SessionRefreshOptions{
		Subject:  stored.Subject,
		TenantID: stored.TenantID,
		ClientID: stored.ClientID,
		Scopes:   stored.Scopes,
		Session:  session,
	}, stored.Expires)
	if err != nil {
		return nil, err
	}

	return &SessionTokens{
		AccessToken:      accessToken,
		ExpiresAt:        expiresAt,
		RefreshToken:     rotated,
		RefreshExpiresAt: refreshExpiresAt,
		TokenType:        tokenTypeForConfirmation(cnf),
	}, nil
}

// confirmSessionRefreshDPoP enforces the session's sender-constraint. It
// returns the cnf claim to stamp onto the renewed access token, or nil for an
// unbound session.
func (p *OIDCProvider) confirmSessionRefreshDPoP(session TokenSessionContext, req SessionRefreshRequest) (*ConfirmationClaim, error) {
	if session.Jkt == "" {
		return nil, nil
	}
	if req.DPoPProof == "" {
		return nil, &SessionRefreshDPoPRequiredError{}
	}
	verified, err := dpop.VerifyProof(req.DPoPProof, dpop.VerifyOptions{
		HTM:         req.DPoPMethod,
		HTU:         req.DPoPURI,
		ReplayCache: p.dpopReplay,
	})
	if err != nil {
		return nil, &SessionRefreshDPoPRequiredError{}
	}
	if subtle.ConstantTimeCompare([]byte(verified.Jkt), []byte(session.Jkt)) != 1 {
		return nil, &SessionRefreshDPoPMismatchError{}
	}
	return &ConfirmationClaim{Jkt: session.Jkt}, nil
}

// sessionRefreshAudience returns the audience a renewed access token carries,
// falling back to the issuer's self-audience for sessions minted before an
// audience was recorded.
func sessionRefreshAudience(session TokenSessionContext, issuer string) []string {
	if len(session.Audience) > 0 {
		return session.Audience
	}
	return BuildAudience(issuer, "")
}

// issueRefreshCredential mints, stores, and returns one opaque refresh token.
// A non-zero absoluteExpiry pins the session's original deadline so rotation
// cannot extend the session; a zero value starts a new RefreshTokenTTL window.
func (s *TokenService) issueRefreshCredential(ctx context.Context, opts SessionRefreshOptions, absoluteExpiry time.Time) (string, time.Time, error) {
	raw, err := xkms.Rand(refreshTokenEntropyBytes)
	if err != nil {
		return "", time.Time{}, &SessionRefreshEntropyError{Cause: err}
	}
	value := base64.RawURLEncoding.EncodeToString(raw)

	now := time.Now()
	expires := absoluteExpiry
	if expires.IsZero() {
		expires = now.Add(s.config.RefreshTokenTTL)
	}

	jti, err := generateJTI()
	if err != nil {
		return "", time.Time{}, err
	}

	session := opts.Session
	record := &Token{
		JTI:       jti,
		Subject:   opts.Subject,
		TenantID:  opts.TenantID,
		ClientID:  opts.ClientID,
		Scopes:    opts.Scopes,
		Expires:   expires,
		Issued:    now,
		TokenType: TokenTypeRefresh,
		ValueHash: HashTokenValue(value),
		Session:   &session,
	}
	if err := s.storage.StoreToken(withTokenTenant(ctx, opts.TenantID), record); err != nil {
		return "", time.Time{}, &SessionRefreshStorageError{Cause: err}
	}
	return value, expires, nil
}

// withTokenTenant scopes a context to the token's tenant under go-qrdb's
// central tenant key (tenantctx), which both quicbaas's token storage and
// go-qrdb's store resolvers read. An empty tenant leaves the context
// untouched.
//
// There used to be two helpers here binding two separate keys, and a call
// that crossed into the qrdb SDK carrying only the quicbaas one ran against
// the SYSTEM tenant's stores while looking correctly scoped — how #529
// presented. With one key (go-quicbaas#1150) that split cannot recur.
func withTokenTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return tenantctx.WithTenant(ctx, tenantID)
}
