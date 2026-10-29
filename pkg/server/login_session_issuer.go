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
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// authMethodMetadataKey is the identity-metadata key under which the unified
// login ceremony records the factor used (mtls, fido2, piv).
const authMethodMetadataKey = "factor_type"

// factorIDMetadataKey is the identity-metadata key under which qrdb's default
// identity hydrator records the STABLE id of the factor the login verified. It
// is the handle this issuer replays the factor's enrolled attestation from.
//
// The hydrator is qrdb's (transport/embedded defaultIdentityHydrator) and the
// node exposes no seam to replace it — WithSessionIssuer and
// WithLoginFactorVerifier are the only login options — so the factor's own
// metadata does not travel on the identity. The id does, which is enough: the
// record it names is the authority.
const factorIDMetadataKey = "factor_id"

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

	// factors reads the stored auth-factor record so a login can REPLAY the
	// attestation its factor recorded at enrollment. Late-bound for the same
	// reason the provider is: the node is constructed before its own client
	// exists. Nil until wired, and a nil reader simply means no attestation is
	// replayed — see attestationFor.
	factors atomic.Pointer[authFactorReader]
}

// authFactorReader is the narrow slice of qrdb's user-auth-factor client this
// issuer needs: one lookup by id. Held as an interface value in a pointer so it
// can be swapped atomically.
type authFactorReader struct {
	get func(ctx context.Context, req *qrdbtransport.AuthFactorGetRequest) (*qrdbtransport.AuthFactorGetResponse, error)
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

// SetAuthFactorClient attaches the client this issuer replays enrolled
// attestation through. Passing nil detaches it, which leaves logins issuing
// sessions with no attestation — the behaviour before #394.
func (i *oidcSessionIssuer) SetAuthFactorClient(c qrdbtransport.UserAuthFactorClient) {
	if c == nil {
		i.factors.Store(nil)
		return
	}
	i.factors.Store(&authFactorReader{get: c.GetAuthFactor})
}

// attestationFor replays the attestation recorded on the factor this login
// verified. It returns empty strings when there is nothing to replay.
//
// UNDERSTATING IS THE SAFE DIRECTION and is the only direction this can fail
// in. A session that carries no attestation is treated as unattested by
// everything downstream, so a missing reader, an absent factor id, a lookup
// failure or a factor with no recorded level all produce a session that claims
// LESS than the credential proved — never more. The alternative, failing the
// login, would turn a transient factor-store blip into an outage for every
// hardware user, to protect a claim whose absence already denies.
//
// It never DERIVES: the values come from the stored record and nothing else. A
// login cannot manufacture assurance it did not prove, which is the invariant
// #385 established for refresh.
func (i *oidcSessionIssuer) attestationFor(ctx context.Context, identity *qrdbsdk.Identity) (level, aaguid string, userID uint64) {
	reader := i.factors.Load()
	if reader == nil || identity.Metadata == nil {
		return "", "", 0
	}
	factorID := identity.Metadata[factorIDMetadataKey]
	if factorID == "" {
		return "", "", 0
	}
	resp, err := reader.get(ctx, &qrdbtransport.AuthFactorGetRequest{FactorID: factorID})
	// A record without attestation metadata still names its owner: reading a
	// nil map yields "" for the attestation, never a dropped user id.
	if err != nil || resp == nil {
		return "", "", 0
	}
	return resp.Factor.Metadata[iam.AuthFactorMetadataAttestLevel],
		resp.Factor.Metadata[iam.AuthFactorMetadataAttestAAGUID],
		resp.Factor.UserID
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
	// Replay what this factor's enrollment proved. Before #394 a WebAuthn login
	// issued a session carrying nothing, so the assurance a deployment had on
	// the REGISTERING session it did not have on any subsequent login with the
	// same key — while the evidence sat in the factor record, written and never
	// read.
	// The same record names the user go-qrdb bound to the factor, which is how
	// the session resolves its principal (ruling 254 option 1).
	session.AttestLevel, session.AttestAAGUID, session.UserID = i.attestationFor(ctx, identity)
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
	// sub is the email or username the ceremony authenticated; the account is
	// the verified uid claim, never sub read as an ID. A token without a uid
	// marks no account.
	return auth.MarkTokenSubject(identity, claims.UserID), nil
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

	// Bind the OIDC provider to the login facade as the session-refresh issuer.
	// The ceremony mints a 15-minute bearer; without this the session cannot be
	// renewed and every ceremony client (CLI, MCP, SPA) dies at that boundary.
	// The provider is the same authority that minted the bearer, so it can
	// freeze the exact session context into the refresh credential.
	s.bindLoginSessionRefreshIssuer(c)

	// Bind the runtime dependencies (per-tenant WebAuthn factory + CA trust
	// roots) to the late-bound login factor verifiers so the node's login
	// ceremony can verify PIV and WebAuthn proofs.
	s.bindLoginFactorVerifiers(c)

	// Publish the same WebAuthn factory to the composed go-xkms module's
	// credential-store decorator, so a passkey registered through the
	// module's own ceremony routes enrolls the login factor the ceremony
	// above resolves.
	s.bindXKMSModuleWebAuthnFactory(c)
}

// bindLoginSessionRefreshIssuer attaches the OIDC provider to the login
// ceremony facade so a completed login also yields a renewal credential. No-op
// when the facade or the provider is absent.
func (s *Server) bindLoginSessionRefreshIssuer(c *iamComponents) {
	if c == nil || c.oidcProvider == nil || s.quicBaaSService == nil {
		return
	}
	login, ok := s.quicBaaSService.LoginCeremony().(*service.LoginService)
	if !ok || login == nil {
		return
	}
	login.SetSessionRefreshIssuer(c.oidcProvider)
}
