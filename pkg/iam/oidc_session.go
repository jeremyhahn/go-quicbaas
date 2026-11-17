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
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
)

// SessionIdentity is the authenticated principal handed to the OIDC provider
// by the unified login ceremony (mTLS | WebAuthn | PIV). It carries only the
// claims the ceremony has already verified; the provider mints an OIDC access
// token from it. This is the OIDC layering point: qrdb runs the factor
// ceremony, quicbaas issues the bearer.
type SessionIdentity struct {
	// Subject is the authenticated principal (qrdb user subject).
	Subject string
	// TenantID is the tenant namespace the principal authenticated into.
	TenantID string
	// Roles are the roles resolved for the principal.
	Roles []string
	// Groups are the domain groups resolved for the principal.
	Groups []string
	// UserID is the qrdb user id go-qrdb bound to the verified login factor
	// (the factor record's owner). Zero when the login carries no factor.
	UserID uint64
	// AuthMethod records the factor used (mtls, fido2, piv).
	AuthMethod string
	// MFAVerified reports whether the login satisfied multi-factor assurance.
	MFAVerified bool

	// AttestLevel and AttestAAGUID REPLAY what the factor's enrollment proved
	// about where its key lives — they are read off the stored factor record,
	// never derived from the login.
	//
	// A login proves possession of a credential; it does not re-prove the
	// credential's provenance, because the attestation statement is presented
	// once, at registration. So the only honest source for these on a login is
	// the record written then, and the only honest thing a login can do with
	// them is copy them. That is the invariant #385 established for refresh and
	// this extends to the third hardware path (#394): assurance is carried
	// forward, never manufactured.
	//
	// Empty means the login established nothing about the authenticator, which
	// is what every WebAuthn login used to say regardless of what its factor
	// had proven.
	AttestLevel  string
	AttestAAGUID string
	// DPoPProof, when non-empty, is an RFC 9449 DPoP proof JWT presented with
	// the login. The session bearer is then bound to the proof key via cnf.jkt
	// (sender-constrained). DPoPMethod and DPoPURI supply the proof htm/htu
	// binding (the login endpoint method + canonical URI).
	DPoPProof  string
	DPoPMethod string
	DPoPURI    string
}

// IssueSessionToken mints an OIDC access token for an authenticated login
// identity. It is the issuance half of the SessionIssuer bridge used by the
// unified login ceremony: a verified factor login yields a self-audienced
// OIDC bearer carrying the principal's tenant, roles, and groups.
//
// The ceremony proves POSSESSION, not entitlement. qrdb resolves its principal
// from the factor lookup — it knows which credential was proven, not what the
// holder may do — so the identity handed here carries no authorization state.
// The IAM user record is the authority on roles, and joining it is what makes
// the minted bearer usable: without the join every claim below is faithfully
// populated from an empty set, and the session authorizes nothing (go-quicbaas
// #274 — masked for as long as the system tenant, which passes RBAC through
// IsSystemTenant special-casing, was the only tenant anyone logged into).
func (p *OIDCProvider) IssueSessionToken(ctx context.Context, identity SessionIdentity) (string, time.Time, error) {
	user, err := p.resolveSessionPrincipal(ctx, identity)
	if err != nil {
		return "", time.Time{}, err
	}

	// Bind a presented DPoP proof (RFC 9449) into cnf.jkt. A login with no proof
	// yields a plain bearer (back-compat); a presented-but-invalid proof fails
	// closed.
	cnf, err := p.bindSessionDPoP(identity)
	if err != nil {
		return "", time.Time{}, err
	}

	groups, err := p.identityStore.RoleNames(ctx, user)
	if err != nil {
		return "", time.Time{}, err
	}

	now := time.Now()
	expiresAt := now.Add(p.config.AccessTokenTTL)
	// The user record is authoritative for roles and groups, so it REPLACES
	// what the ceremony supplied rather than merging with it: a role the record
	// no longer grants must not survive into a bearer because a stale
	// factor-side principal still mentioned it.
	jti, err := generateJTI()
	if err != nil {
		return "", time.Time{}, err
	}

	claims := &Claims{
		Issuer:       p.config.Issuer,
		Subject:      identity.Subject,
		Audience:     BuildAudience(p.config.Issuer, ""),
		ExpiresAt:    NewUnixTime(expiresAt),
		IssuedAt:     NewUnixTime(now),
		NotBefore:    NewUnixTime(now),
		JTI:          jti,
		TenantID:     identity.TenantID,
		UserID:       user.ID,
		Roles:        user.Roles,
		Groups:       groups,
		Email:        user.Email,
		Name:         user.Name,
		AuthMethod:   identity.AuthMethod,
		MFAVerified:  identity.MFAVerified,
		AttestLevel:  identity.AttestLevel,
		AttestAAGUID: identity.AttestAAGUID,
		Confirmation: cnf,
	}
	token, err := p.tokenService.GenerateAccessToken(ctx, claims)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// resolveSessionPrincipal joins a verified login identity to its IAM user
// record and returns the record the session's claims are minted from.
//
// The subject is resolved the way the CEREMONY KEYS IT, which is by email: the
// WebAuthn/xkms credential is looked up by the email handle, so that is what
// arrives here. Resolving by username alone locked out every user whose display
// name is not their email — i.e. everyone created normally over REST, since the
// broker's qrdb username derives from User.Name and the #289 split made Name and
// Email genuinely different fields (go-quicbaas #296). The username lookup is
// kept as a SECONDARY for a non-email-shaped subject, because a subject that is
// not an email cannot be an email lookup and something has to resolve it.
//
// It never tries the subject as a user ID. That was an escalation: nothing
// constrains the id and username namespaces apart — validateUserID accepts any
// non-empty string and the username charset admits UUID-shaped names — so a
// user who named themselves after another user's id would authenticate honestly
// as themselves and be handed that user's roles.
//
// AMBIGUITY IS REFUSED, which is what keeps the second lookup from reopening
// that hole in a new shape. If the subject resolves to two DIFFERENT records —
// one user's email equal to another user's name — neither is authoritative and
// the login is refused, so an email-shaped subject can never be resolved as
// somebody else's username. Because no id lookup happens at all, the id side
// stays closed by construction.
//
// The email index is GLOBAL (it resolves its own owning tenant), so a match is
// accepted only when it lands in the tenant the ceremony authenticated into.
// Without that check an email could mint a session in a tenant its owner does
// not belong to.
//
// It FAILS CLOSED in every direction. A subject with no resolvable user record
// is refused rather than issued a roles-less bearer: the ceremony verified a
// factor, but there is no record to say what that factor's holder may do, and a
// permissionless session is a login that appears to succeed and then denies
// every request — the exact failure mode this join exists to remove. A record
// that is suspended, locked, or disabled is refused too, at mint time, because
// TokenLiveness cannot retroactively un-issue a bearer.
func (p *OIDCProvider) resolveSessionPrincipal(ctx context.Context, identity SessionIdentity) (*User, error) {
	if identity.Subject == "" || identity.TenantID == "" {
		return nil, &SessionPrincipalUnresolvedError{
			Subject:  identity.Subject,
			TenantID: identity.TenantID,
		}
	}

	user, err := p.resolveSubject(ctx, identity)
	if err != nil {
		return nil, &SessionPrincipalUnresolvedError{
			Subject:  identity.Subject,
			TenantID: identity.TenantID,
			Cause:    err,
		}
	}
	if user == nil {
		return nil, &SessionPrincipalUnresolvedError{
			Subject:  identity.Subject,
			TenantID: identity.TenantID,
		}
	}
	if !userEnabled(user) {
		return nil, &SessionPrincipalDisabledError{
			Subject:  identity.Subject,
			TenantID: identity.TenantID,
		}
	}
	return user, nil
}

// resolveSubject returns the single user record the ceremony's subject names,
// or a nil user when nothing does. A store failure is returned as an error so
// the caller fails closed rather than treating an outage as "no such user".
//
// The email index is the only subject login key (go-quicbaas#377). A username
// is never a second authentication key: an email-shaped username could
// otherwise resolve as another user's account.
//
// The lookup is tenant-scoped. The email index used to be global, with the
// owning tenant checked afterwards; scoping the lookup itself means a
// foreign-tenant record is never read, rather than read and then discarded.
func (p *OIDCProvider) resolveSubject(ctx context.Context, identity SessionIdentity) (*User, error) {
	// A go-qrdb-verified factor login names its user by the id go-qrdb bound
	// to the factor, so the subject (a username for a passkey) is never the
	// key (ruling 254 option 1). The lookup stays inside the login's tenant.
	if identity.UserID != 0 {
		return principalOrMiss(p.identityStore.GetUser(ctx, identity.TenantID, identity.UserID))
	}
	// A non-email subject is unresolvable, not a cue to try another index: a
	// username is the handle an administrator types for RBAC, never a
	// credential.
	if validateEmail(identity.Subject) != nil {
		return nil, nil
	}
	return principalOrMiss(p.identityStore.GetUserByEmail(ctx, identity.TenantID, identity.Subject))
}

// principalOrMiss maps a principal lookup's not-found to (nil, nil), so the
// caller reports an unresolved principal rather than a store failure.
func principalOrMiss(user *User, err error) (*User, error) {
	switch {
	case err == nil:
		return user, nil
	case isPrincipalLookupMiss(err):
		return nil, nil
	default:
		return nil, err
	}
}

// isPrincipalLookupMiss reports whether err means "this lookup has nothing to
// say about that subject" — the record is absent, or the subject's shape does
// not fit the index at all — as opposed to a store failure, which must deny.
//
// The CLASS is the intent here, so it asks dberrors.IsNotFound outright. It
// used to OR that with errors.Is(err, ErrUserNotFound), which read as a
// narrower sentinel check and was not one: errors.Is compares the QBError code,
// so the term matched exactly the set IsNotFound already covers and could never
// add a case (go-quicbaas#434).
func isPrincipalLookupMiss(err error) bool {
	var validation *ValidationError
	return dberrors.IsNotFound(err) || errors.As(err, &validation)
}

// RevokeSessionToken revokes a session bearer previously issued by
// IssueSessionToken. Unlike Revoke (the RFC 7009 OAuth2 endpoint, which
// authenticates an OAuth client), this is the internal logout path for a
// ceremony-issued session token: it revokes by token value directly.
func (p *OIDCProvider) RevokeSessionToken(ctx context.Context, token string) error {
	return p.tokenService.RevokeToken(ctx, token)
}

// bindSessionDPoP verifies a DPoP proof presented with a factor login and
// returns the cnf claim binding the session bearer to the proof key. It returns
// (nil, nil) when no proof was presented.
func (p *OIDCProvider) bindSessionDPoP(identity SessionIdentity) (*ConfirmationClaim, error) {
	if identity.DPoPProof == "" {
		return nil, nil
	}
	verified, err := dpop.VerifyProof(identity.DPoPProof, dpop.VerifyOptions{
		HTM:         identity.DPoPMethod,
		HTU:         identity.DPoPURI,
		ReplayCache: p.dpopReplay,
	})
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidDPoPProof, "invalid DPoP proof")
	}
	return &ConfirmationClaim{Jkt: verified.Jkt}, nil
}
