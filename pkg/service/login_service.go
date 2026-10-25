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

package service

import (
	"context"
	"crypto/x509"
	"sync/atomic"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Login Service — transport-agnostic facade over go-qrdb's LoginClient.
// =============================================================================
//
// The unified login ceremony (BeginLogin / FinishLogin) is owned by the
// composed go-qrdb node and is EMBEDDED-ONLY: only the in-process qrdb SDK
// client exposes a transport.LoginClient. Network qrdb sub-clients do not.
//
// LoginService is the single home for the ceremony's business logic inside
// QuicBaaS. Every transport (REST, gRPC, QUIC, MCP, Unix, embedded) funnels
// its decoded BeginLogin / FinishLogin request through here; the facade
// forwards to the in-process node LoginClient. Putting the logic here once —
// rather than in each transport handler — keeps the security-critical mTLS
// decision (below) in a single auditable place.
//
// SECURITY MODEL (mTLS factor):
//
//	For an mtls factor the "possession proof" is the TLS handshake itself —
//	the peer authenticated the connection with its client certificate. The
//	canonical login lookup key for an mtls factor is SHA-256(cert DER) (see
//	go-qrdb pkg/auth.CredentialKey + AuthFactor docs). A client MUST NOT be
//	able to assert "log me in as the holder of cert X" by simply putting
//	X's key in the request body — that would let any caller impersonate any
//	enrolled certificate.
//
//	Therefore, for FinishLogin with Type == mtls the facade DERIVES the
//	lookup key from the VERIFIED peer certificate carried in the context
//	(VerifiedPeerCertFromContext), NEVER from the client-supplied
//	FinishLoginRequest.LookupKey. The client-supplied value is ignored and
//	overwritten.
//
//	Network transports MUST populate the verified peer certificate into the
//	context from their already-verified TLS chain before calling the facade.
//	If an mtls request arrives with no verified peer cert AND the context is
//	not marked embedded-trusted, the facade FAILS CLOSED with
//	MTLSPeerCertRequiredError. This makes a missing cert on a network
//	transport unconditionally fatal — it can never fall through to trusting
//	a client claim.
//
//	The in-process embedded transport sets the embedded-trusted marker
//	(WithEmbeddedTrustedContext). For embedded callers the request-supplied
//	LookupKey is trusted (in-process trust boundary): there is no network
//	peer and the caller already runs inside the daemon's address space.
//
//	PIV and WebAuthn (fido2) factors carry their own cryptographic proof
//	(signature / assertion) which the node's FactorVerifiers validate; no
//	peer certificate is required and their requests are forwarded unchanged.
// =============================================================================

// loginContextKey is the unexported type for login-related context keys so
// they cannot collide with keys defined in other packages.
type loginContextKey int

const (
	// verifiedPeerCertContextKey carries the *x509.Certificate that a network
	// transport extracted from its already-verified TLS handshake.
	verifiedPeerCertContextKey loginContextKey = iota

	// embeddedTrustedContextKey marks a context as originating from the
	// in-process embedded transport, where the caller-supplied lookup key is
	// trusted because there is no network boundary to spoof.
	embeddedTrustedContextKey
)

// WithVerifiedPeerCert returns a child context carrying the verified peer
// certificate. NETWORK transports call this with the leaf certificate from
// their verified TLS chain (gRPC TLSInfo.State.VerifiedChains[0][0] /
// PeerCertificates[0]; REST r.TLS.VerifiedChains[0][0]; QUIC connection TLS
// state; Unix peer cert) before invoking the facade. A nil cert is a no-op so
// the failure surfaces as MTLSPeerCertRequiredError at the facade, not a panic.
func WithVerifiedPeerCert(ctx context.Context, cert *x509.Certificate) context.Context {
	if cert == nil {
		return ctx
	}
	return context.WithValue(ctx, verifiedPeerCertContextKey, cert)
}

// VerifiedPeerCertFromContext returns the verified peer certificate stored by
// WithVerifiedPeerCert, or nil when none is present.
func VerifiedPeerCertFromContext(ctx context.Context) *x509.Certificate {
	if ctx == nil {
		return nil
	}
	cert, _ := ctx.Value(verifiedPeerCertContextKey).(*x509.Certificate)
	return cert
}

// WithEmbeddedTrustedContext marks a context as in-process embedded-trusted.
// ONLY the embedded transport calls this. It is the single, explicit switch
// that lets an mtls request proceed without a verified peer certificate; a
// network transport must never call it.
func WithEmbeddedTrustedContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, embeddedTrustedContextKey, true)
}

// isEmbeddedTrusted reports whether the context was marked embedded-trusted.
func isEmbeddedTrusted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	trusted, _ := ctx.Value(embeddedTrustedContextKey).(bool)
	return trusted
}

// mtlsLookupKey returns the legacy login lookup key for an mtls factor:
// SHA-256 over the certificate's DER encoding. It delegates to
// iam.MTLSCredentialKey — the SINGLE definition shared with the enrollment side
// (iam.CertLoginFactorEnroller writes exactly this blob as the factor's
// Credential) — so the resolver's LookupByCredentialKey cannot miss an enrolled
// mtls factor because the two sides drifted.
func mtlsLookupKey(cert *x509.Certificate) []byte {
	return iam.MTLSCredentialKey(cert)
}

// mtlsSPKILookupKey returns the renewal-stable login lookup key for an mtls
// factor: SHA-256 over the certificate's SubjectPublicKeyInfo. It delegates to
// iam.MTLSSPKICredentialKey, the same derivation the enrollment side writes as
// the factor's SPKICredential, so a certificate renewed with the same keypair
// keeps resolving to its factor after the cert.Raw key changed.
func mtlsSPKILookupKey(cert *x509.Certificate) []byte {
	return iam.MTLSSPKICredentialKey(cert)
}

// SessionRefreshIssuer mints the renewal credential for a session whose bearer
// the login ceremony has just issued. It is implemented by iam.OIDCProvider —
// the same authority that minted the bearer.
//
// WHY THE CEREMONY NEEDS THIS SEAM: qrdb owns BeginLogin/FinishLogin and its
// FinishLoginResponse carries only (bearer, identity, expiry, factor). Refresh
// is an OAuth2 concept that belongs to QuicBaaS, not to qrdb's factor ceremony,
// so the renewal credential is layered on HERE — once, in the facade every
// transport already funnels through — rather than in each transport handler or,
// worse, in each client.
type SessionRefreshIssuer interface {
	// IssueRefreshForSessionBearer returns the opaque refresh credential and
	// its absolute expiry for the session identified by the given bearer.
	IssueRefreshForSessionBearer(ctx context.Context, bearer string) (string, time.Time, error)
}

// FinishLoginResult is the QuicBaaS-owned outcome of a login ceremony: qrdb's
// response plus the refresh credential QuicBaaS layers on top. The embedded
// response is promoted, so Bearer / Identity / ExpiresAt / FactorID read
// exactly as before.
type FinishLoginResult struct {
	*qrdbtransport.FinishLoginResponse

	// RefreshToken renews the session after the short-lived bearer expires.
	// Empty when no refresh issuer is wired (refresh disabled).
	RefreshToken string `json:"refreshToken,omitempty"`

	// RefreshExpiresAt is the session's absolute deadline. Renewal cannot move
	// it: after this instant the factor ceremony must run again.
	RefreshExpiresAt time.Time `json:"refreshExpiresAt,omitempty"`
}

// LoginServicer is the transport-agnostic login-ceremony surface. Every
// QuicBaaS transport routes its BeginLogin / FinishLogin verb through this
// interface so the security model lives in exactly one place.
type LoginServicer interface {
	// BeginLogin opens a login attempt and returns the server-issued
	// challenge (nonce + method-specific metadata).
	BeginLogin(ctx context.Context, req *qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error)

	// FinishLogin consumes the proof and mints a bearer plus its renewal
	// credential. For an mtls factor the lookup key is derived from the
	// verified peer certificate in ctx, not from the request body (see the
	// package security model).
	FinishLogin(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*FinishLoginResult, error)
}

// LoginService is the concrete LoginServicer. It forwards to the in-process
// qrdb node LoginClient supplied via SetLoginClient. When no client is wired
// (login disabled / IAM absent) every method returns LoginCeremonyUnavailableError.
type LoginService struct {
	client        qrdbtransport.LoginClient
	refreshIssuer SessionRefreshIssuer

	// auditor receives the server-side account of every REFUSED ceremony.
	// It is late-bound (the audit plane is composed after the node) and read
	// on every rejection, so it is held in an atomic pointer rather than
	// behind a mutex: reporting a rejection must never contend with the
	// request path it is reporting on.
	auditor atomic.Pointer[LoginCeremonyAuditor]
}

var _ LoginServicer = (*LoginService)(nil)

// NewLoginService constructs a LoginService backed by the supplied in-process
// LoginClient. A nil client yields a facade that fails closed with
// LoginCeremonyUnavailableError until SetLoginClient installs a live client.
func NewLoginService(client qrdbtransport.LoginClient) *LoginService {
	return &LoginService{client: client}
}

// RefuseTenantLogin is the one point a login for a tenant other than the
// system tenant is refused, before its session bearer is issued. Issuing it
// reads the tenant's roles and permissions with no authenticated caller yet,
// which needs go-qrdb v0.54.0-alpha (go-qrdb #1084, #1089); go-quicbaas #1816
// re-pins go-qrdb and removes this refusal. It returns a PreconditionFailed
// QBError wrapping sdkerrors.TenantLoginRefusedError, nil for the system tenant.
func RefuseTenantLogin(tenantID string) error {
	return refuseNonSystemTenant("login.IssueSession", tenantID)
}

// refuseNonSystemTenant returns the PreconditionFailed QBError wrapping
// sdkerrors.TenantLoginRefusedError for any tenant but the system tenant, and
// nil for the system tenant. Every refusal that waits on go-quicbaas #1816
// is built here, so each one maps to the same status on every transport.
func refuseNonSystemTenant(op, tenantID string) error {
	if tenantID == qrdbsdk.SystemTenantID {
		return nil
	}
	return &dberrors.QBError{
		Code: dberrors.ErrPreconditionFailed,
		Op:   op,
		Err:  &sdkerrors.TenantLoginRefusedError{},
	}
}

// SetLoginClient installs (or replaces) the backing in-process LoginClient.
// Called by the server once the qrdb node is constructed.
func (s *LoginService) SetLoginClient(client qrdbtransport.LoginClient) {
	s.client = client
}

// SetSessionRefreshIssuer installs (or replaces) the refresh-credential issuer.
// Called by the server once the OIDC provider is initialized. Until it is set
// the ceremony still issues bearers; they simply cannot be renewed.
func (s *LoginService) SetSessionRefreshIssuer(issuer SessionRefreshIssuer) {
	s.refreshIssuer = issuer
}

// SetCeremonyAuditor installs (or replaces) the sink that receives the
// server-side account of a refused login. Called by the server when it wires
// the facade. Until it is set a rejection is reported nowhere — which is
// precisely the state go-quicbaas#484 was diagnosed in, so the wiring site
// installs it in the same statement that constructs the facade.
func (s *LoginService) SetCeremonyAuditor(auditor LoginCeremonyAuditor) {
	if auditor == nil {
		s.auditor.Store(nil)
		return
	}
	s.auditor.Store(&auditor)
}

// reportRejection hands a refused ceremony to the auditor. It is a no-op when
// no auditor is wired and never returns an error: the report is an
// observation of the outcome, not part of it.
func (s *LoginService) reportRejection(ctx context.Context, rejection LoginRejection) {
	auditor := s.auditor.Load()
	if auditor == nil || *auditor == nil {
		return
	}
	(*auditor).RecordLoginRejection(ctx, rejection)
}

// BeginLogin forwards the begin step to the node LoginClient. BeginLogin needs
// no peer-cert handling: it only issues a challenge. For an mtls factor there
// is no separate hardware step, but BeginLogin still scopes the attempt and
// returns an expiry the client echoes on FinishLogin.
func (s *LoginService) BeginLogin(ctx context.Context, req *qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	if req == nil {
		err := &LoginRequestNilError{Op: "BeginLogin"}
		s.reportRejection(ctx, describeBeginRejection(nil, err))
		return nil, err
	}
	if s.client == nil {
		err := &LoginCeremonyUnavailableError{}
		s.reportRejection(ctx, describeBeginRejection(req, err))
		return nil, err
	}
	resp, err := s.client.BeginLogin(ctx, *req)
	if err != nil {
		s.reportRejection(ctx, describeBeginRejection(req, err))
		return nil, err
	}
	return resp, nil
}

// FinishLogin forwards the finish step to the node LoginClient after enforcing
// the mTLS security model, then layers on the session's renewal credential.
// See the package-level SECURITY MODEL comment.
func (s *LoginService) FinishLogin(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*FinishLoginResult, error) {
	if req == nil {
		err := &LoginRequestNilError{Op: "FinishLogin"}
		s.reportRejection(ctx, describeFinishRejection(nil, err))
		return nil, err
	}
	if s.client == nil {
		err := &LoginCeremonyUnavailableError{}
		s.reportRejection(ctx, describeFinishRejection(req, err))
		return nil, err
	}

	resp, err := s.finishCeremony(ctx, req)
	if err != nil {
		s.reportRejection(ctx, describeFinishRejection(req, err))
		return nil, err
	}
	// A refresh-issuance failure rejects the login (see withSessionRefresh),
	// so it is a rejection like any other and is reported like one. Without
	// this the one failure mode that happens AFTER a valid proof would be
	// the only silent one left — and it is also the only one whose record
	// can name the authenticated subject, because the proof already passed.
	result, err := s.withSessionRefresh(ctx, resp)
	if err != nil {
		s.reportRejection(ctx, describeVerifiedFinishRejection(req, resp, err))
		return nil, err
	}
	return result, nil
}

// finishCeremony runs the qrdb half of the finish step, enforcing the mTLS
// anti-spoofing model.
func (s *LoginService) finishCeremony(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	// mTLS: derive the lookup key from the verified peer certificate. Never
	// trust the client-supplied LookupKey on a network transport.
	if req.Type == qrdbtransport.FactorTypeMTLS {
		cert := VerifiedPeerCertFromContext(ctx)
		switch {
		case cert != nil:
			// Authoritative: overwrite whatever the client sent. Try the
			// renewal-stable SPKI key first, then fall back to the legacy
			// SHA-256(cert.Raw) key for factors enrolled before the SPKI
			// index existed. Both keys ride in ONE FinishLogin call because
			// the node consumes the login session before resolution.
			out := *req
			out.LookupKey = mtlsSPKILookupKey(cert)
			out.AltLookupKeys = [][]byte{mtlsLookupKey(cert)}
			return s.client.FinishLogin(ctx, out)
		case isEmbeddedTrusted(ctx):
			// In-process trust: the caller runs inside the daemon. The
			// request-supplied LookupKey is accepted as-is.
			return s.client.FinishLogin(ctx, *req)
		default:
			// Network transport with no verified peer cert: fail closed.
			return nil, &MTLSPeerCertRequiredError{}
		}
	}

	// PIV / WebAuthn (fido2): the signature/assertion is the proof; the node's
	// FactorVerifiers validate it. Forward the request unchanged.
	return s.client.FinishLogin(ctx, *req)
}

// withSessionRefresh attaches the renewal credential to a completed login.
//
// A failure to mint the refresh credential FAILS THE LOGIN. Returning a bearer
// with no way to renew it is precisely the defect this path exists to remove:
// the client would appear signed in and then die silently at the 15-minute
// boundary. Better to reject the login and let the caller retry.
func (s *LoginService) withSessionRefresh(ctx context.Context, resp *qrdbtransport.FinishLoginResponse) (*FinishLoginResult, error) {
	if resp == nil {
		return nil, &LoginCeremonyUnavailableError{}
	}
	result := &FinishLoginResult{FinishLoginResponse: resp}
	if s.refreshIssuer == nil || resp.Bearer == "" {
		return result, nil
	}
	refresh, expiresAt, err := s.refreshIssuer.IssueRefreshForSessionBearer(ctx, resp.Bearer)
	if err != nil {
		return nil, &LoginRefreshIssuanceError{Cause: err}
	}
	result.RefreshToken = refresh
	result.RefreshExpiresAt = expiresAt
	return result, nil
}
