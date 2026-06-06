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
	"crypto/sha256"
	"crypto/x509"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
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

// mtlsLookupKey returns the canonical login lookup key for an mtls factor:
// SHA-256 over the certificate's DER encoding. This MUST match the key the
// factor was enrolled with — go-qrdb pkg/auth.CredentialKey(cert.Raw) — so the
// resolver's LookupByCredentialKey finds the enrolled mtls factor.
func mtlsLookupKey(cert *x509.Certificate) []byte {
	sum := sha256.Sum256(cert.Raw)
	return sum[:]
}

// LoginServicer is the transport-agnostic login-ceremony surface. Every
// QuicBaaS transport routes its BeginLogin / FinishLogin verb through this
// interface so the security model lives in exactly one place.
type LoginServicer interface {
	// BeginLogin opens a login attempt and returns the server-issued
	// challenge (nonce + method-specific metadata).
	BeginLogin(ctx context.Context, req *qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error)

	// FinishLogin consumes the proof and mints a bearer. For an mtls factor
	// the lookup key is derived from the verified peer certificate in ctx,
	// not from the request body (see the package security model).
	FinishLogin(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error)
}

// LoginService is the concrete LoginServicer. It forwards to the in-process
// qrdb node LoginClient supplied via SetLoginClient. When no client is wired
// (login disabled / IAM absent) every method returns LoginCeremonyUnavailableError.
type LoginService struct {
	client qrdbtransport.LoginClient
}

var _ LoginServicer = (*LoginService)(nil)

// NewLoginService constructs a LoginService backed by the supplied in-process
// LoginClient. A nil client yields a facade that fails closed with
// LoginCeremonyUnavailableError until SetLoginClient installs a live client.
func NewLoginService(client qrdbtransport.LoginClient) *LoginService {
	return &LoginService{client: client}
}

// SetLoginClient installs (or replaces) the backing in-process LoginClient.
// Called by the server once the qrdb node is constructed.
func (s *LoginService) SetLoginClient(client qrdbtransport.LoginClient) {
	s.client = client
}

// BeginLogin forwards the begin step to the node LoginClient. BeginLogin needs
// no peer-cert handling: it only issues a challenge. For an mtls factor there
// is no separate hardware step, but BeginLogin still scopes the attempt and
// returns an expiry the client echoes on FinishLogin.
func (s *LoginService) BeginLogin(ctx context.Context, req *qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	if req == nil {
		return nil, &LoginRequestNilError{Op: "BeginLogin"}
	}
	if s.client == nil {
		return nil, &LoginCeremonyUnavailableError{}
	}
	return s.client.BeginLogin(ctx, *req)
}

// FinishLogin forwards the finish step to the node LoginClient after enforcing
// the mTLS security model. See the package-level SECURITY MODEL comment.
func (s *LoginService) FinishLogin(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	if req == nil {
		return nil, &LoginRequestNilError{Op: "FinishLogin"}
	}
	if s.client == nil {
		return nil, &LoginCeremonyUnavailableError{}
	}

	// mTLS: derive the lookup key from the verified peer certificate. Never
	// trust the client-supplied LookupKey on a network transport.
	if req.Type == qrdbtransport.FactorTypeMTLS {
		cert := VerifiedPeerCertFromContext(ctx)
		switch {
		case cert != nil:
			// Authoritative: overwrite whatever the client sent.
			out := *req
			out.LookupKey = mtlsLookupKey(cert)
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
