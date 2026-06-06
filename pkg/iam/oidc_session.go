//go:build iam

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
	"time"

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
	// AuthMethod records the factor used (mtls, fido2, piv).
	AuthMethod string
	// MFAVerified reports whether the login satisfied multi-factor assurance.
	MFAVerified bool
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
func (p *OIDCProvider) IssueSessionToken(ctx context.Context, identity SessionIdentity) (string, time.Time, error) {
	// Bind a presented DPoP proof (RFC 9449) into cnf.jkt. A login with no proof
	// yields a plain bearer (back-compat); a presented-but-invalid proof fails
	// closed.
	cnf, err := p.bindSessionDPoP(identity)
	if err != nil {
		return "", time.Time{}, err
	}

	now := time.Now()
	expiresAt := now.Add(p.config.AccessTokenTTL)
	claims := &Claims{
		Issuer:       p.config.Issuer,
		Subject:      identity.Subject,
		Audience:     BuildAudience(p.config.Issuer, ""),
		ExpiresAt:    NewUnixTime(expiresAt),
		IssuedAt:     NewUnixTime(now),
		NotBefore:    NewUnixTime(now),
		JTI:          generateJTI(),
		TenantID:     identity.TenantID,
		Roles:        identity.Roles,
		Groups:       identity.Groups,
		AuthMethod:   identity.AuthMethod,
		MFAVerified:  identity.MFAVerified,
		Confirmation: cnf,
	}
	token, err := p.tokenService.GenerateAccessToken(ctx, claims)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
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
