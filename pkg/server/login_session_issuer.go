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

package server

import (
	"context"
	"sync/atomic"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// authMethodMetadataKey is the identity-metadata key under which the unified
// login ceremony records the factor used (mtls, fido2, piv).
const authMethodMetadataKey = "factor_type"

// oidcSessionIssuer adapts the quicbaas OIDC provider to qrdb's login
// SessionIssuer. It is the bridge that makes a successful unified login
// (BeginLogin/FinishLogin over the composed qrdb node's in-process ceremony)
// mint an OIDC access token as its bearer: qrdb resolves the principal and
// verifies the factor proof, then calls Issue here to obtain the session
// token. Token internals (signing, claims, revocation) stay encapsulated in
// the OIDC service; this type only translates between qrdb's auth.Identity and
// the OIDC provider.
//
// The provider is LATE-BOUND: the qrdb node is constructed before IAM init, so
// the issuer is injected first (provider nil) and the provider is attached via
// SetProvider once IAM components are ready. Login is a runtime operation that
// happens long after both, so the provider is always set by the time Issue is
// called. An atomic pointer keeps SetProvider/load lock-free.
type oidcSessionIssuer struct {
	provider atomic.Pointer[iam.OIDCProvider]
}

// newOIDCSessionIssuer creates an unbound issuer. Call SetProvider once the
// OIDC provider is available.
func newOIDCSessionIssuer() *oidcSessionIssuer {
	return &oidcSessionIssuer{}
}

// SetProvider attaches (or replaces) the OIDC provider backing this issuer.
func (i *oidcSessionIssuer) SetProvider(p *iam.OIDCProvider) {
	i.provider.Store(p)
}

// Compile-time check that oidcSessionIssuer satisfies the qrdb interface.
var _ qrdbembedded.SessionIssuer = (*oidcSessionIssuer)(nil)

// Issue mints an OIDC access token for a verified login identity.
func (i *oidcSessionIssuer) Issue(ctx context.Context, identity *qrdbsdk.Identity) (string, time.Time, error) {
	if identity == nil {
		return "", time.Time{}, &NilLoginIdentityError{}
	}
	provider := i.provider.Load()
	if provider == nil {
		return "", time.Time{}, &LoginCeremonyUnavailableError{}
	}
	session := iam.SessionIdentity{
		Subject:     identity.Subject,
		TenantID:    identity.TenantID,
		Roles:       identity.Roles,
		MFAVerified: true,
	}
	if identity.Metadata != nil {
		session.AuthMethod = identity.Metadata[authMethodMetadataKey]
	}
	// A DPoP proof presented on the login-finish request (threaded on the context
	// by the transport handler) sender-constrains the session bearer via cnf.jkt.
	// qrdb passes the caller's context here unchanged, so no ceremony signature
	// change is needed.
	if d := iam.LoginDPoPFromContext(ctx); d != nil {
		session.DPoPProof = d.Proof
		session.DPoPMethod = d.Method
		session.DPoPURI = d.URI
	}
	return provider.IssueSessionToken(ctx, session)
}

// Resolve validates a previously issued bearer and reconstructs the identity.
func (i *oidcSessionIssuer) Resolve(ctx context.Context, bearer string) (*qrdbsdk.Identity, error) {
	provider := i.provider.Load()
	if provider == nil {
		return nil, &LoginCeremonyUnavailableError{}
	}
	claims, err := provider.ValidateAccessToken(ctx, bearer)
	if err != nil {
		return nil, err
	}
	identity := &qrdbsdk.Identity{
		Subject:  claims.Subject,
		TenantID: claims.TenantID,
		Roles:    claims.Roles,
	}
	if claims.AuthMethod != "" {
		identity.Metadata = map[string]string{authMethodMetadataKey: claims.AuthMethod}
	}
	return identity, nil
}

// Revoke invalidates a previously issued bearer (logout).
func (i *oidcSessionIssuer) Revoke(ctx context.Context, bearer string) error {
	provider := i.provider.Load()
	if provider == nil {
		return &LoginCeremonyUnavailableError{}
	}
	return provider.RevokeSessionToken(ctx, bearer)
}

// buildLoginSessionIssuer creates the late-bound OIDC session issuer and stores
// it on the server so initStandaloneQRDBNode / initClusteredQRDBNode can inject
// it into the qrdb node via WithSessionIssuer. Called before node construction;
// the provider is bound later by bindLoginCeremonyProvider.
func (s *Server) buildLoginSessionIssuer() {
	if s.loginSessionIssuer == nil {
		s.loginSessionIssuer = newOIDCSessionIssuer()
	}
}

// bindLoginCeremonyProvider attaches the OIDC provider to the late-bound
// session issuer once IAM components are initialized. No-op when the issuer or
// IAM components are absent.
func (s *Server) bindLoginCeremonyProvider(components service.IAMComponents) {
	c, ok := components.(*iamComponents)
	if !ok || c == nil {
		return
	}

	// Bind the OIDC provider to the late-bound session issuer.
	if issuer, ok := s.loginSessionIssuer.(*oidcSessionIssuer); ok && issuer != nil && c.oidcProvider != nil {
		issuer.SetProvider(c.oidcProvider)
	}

	// Bind the runtime dependencies (per-tenant WebAuthn factory + CA trust
	// roots) to the late-bound login factor verifiers so the node's login
	// ceremony can verify PIV and WebAuthn proofs.
	s.bindLoginFactorVerifiers(c)
}
