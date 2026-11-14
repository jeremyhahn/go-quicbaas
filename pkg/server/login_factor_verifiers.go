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
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// tenantWebAuthnVerifier is the tenant-aware WebAuthn login FactorVerifier
// injected into the composed qrdb node. It bridges the node's in-process login
// ceremony to quicbaas's per-tenant xkms WebAuthn services.
//
// SECURITY: WebAuthn verification is relying-party scoped — an assertion is
// only valid against the RP id/origin of the tenant that owns the credential.
// This verifier therefore resolves the per-tenant *xkmswebauthn.Service for the
// EXACT login tenant and never falls back to a default tenant. The actual
// assertion verification (signature, RP id/origin, sign counter, revoked-factor
// rejection) is performed by the xkms WebAuthnFactorVerifier; this type only
// resolves the per-tenant service and enforces tenant isolation, then delegates.
//
// The factory is LATE-BOUND via an atomic pointer: the qrdb node is constructed
// before IAM init, so the verifier shell is injected first (factory nil) and the
// factory is attached via setFactory once IAM components are ready. Login is a
// runtime operation that happens long after both, so the factory is always set
// by the time the ceremony calls BeginChallenge / VerifyTenant. The atomic
// pointer keeps setFactory / load lock-free.
type tenantWebAuthnVerifier struct {
	factory atomic.Pointer[iam.WebAuthnServiceFactory]
}

// newTenantWebAuthnVerifier creates an unbound WebAuthn login verifier. Call
// setFactory once the per-tenant WebAuthn service factory is available.
func newTenantWebAuthnVerifier() *tenantWebAuthnVerifier {
	return &tenantWebAuthnVerifier{}
}

// setFactory attaches (or replaces) the per-tenant WebAuthn service factory.
func (v *tenantWebAuthnVerifier) setFactory(f *iam.WebAuthnServiceFactory) {
	v.factory.Store(f)
}

// Compile-time checks that the verifier satisfies all three qrdb capabilities.
var (
	_ qrdbembedded.FactorVerifier       = (*tenantWebAuthnVerifier)(nil)
	_ qrdbembedded.FactorChallenger     = (*tenantWebAuthnVerifier)(nil)
	_ qrdbembedded.TenantFactorVerifier = (*tenantWebAuthnVerifier)(nil)
)

// Type returns the FIDO2/WebAuthn factor type this verifier handles.
func (v *tenantWebAuthnVerifier) Type() qrdbtransport.FactorType {
	return qrdbtransport.FactorTypeFIDO2
}

// tenantService resolves the per-tenant xkms WebAuthn verifier for the given
// login tenant. It fails closed when the tenant is empty or the factory is
// unbound — never returning a verifier scoped to a different tenant.
func (v *tenantWebAuthnVerifier) tenantVerifier(ctx context.Context, tenantID string, op string) (*xkmssdk.WebAuthnFactorVerifier, error) {
	if tenantID == "" {
		return nil, &EmptyLoginTenantError{Op: op}
	}
	factory := v.factory.Load()
	if factory == nil {
		return nil, &UnboundWebAuthnFactoryError{}
	}
	svc, err := factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return xkmssdk.NewWebAuthnFactorVerifier(svc)
}

// BeginChallenge opens a tenant-scoped WebAuthn assertion ceremony on the login
// tenant's relying-party service and returns the challenge bytes plus the
// metadata the client needs to drive navigator.credentials.get(). It fails
// closed when the tenant is empty or the factory is unbound.
func (v *tenantWebAuthnVerifier) BeginChallenge(ctx context.Context, tenantID string, factorType qrdbtransport.FactorType, hint string) ([]byte, map[string]string, time.Time, error) {
	verifier, err := v.tenantVerifier(ctx, tenantID, "BeginChallenge")
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	return verifier.BeginChallenge(ctx, tenantID, factorType, hint)
}

// VerifyTenant verifies a WebAuthn assertion against the EXACT login tenant's
// relying-party service. This is the security-critical tenant-isolation point:
// the assertion is verified only against the tenant resolved from the
// authenticating principal, never a default tenant. It fails closed when the
// tenant is empty or the factory is unbound, and delegates the actual
// verification crypto (including revoked-factor rejection) to the xkms verifier.
func (v *tenantWebAuthnVerifier) VerifyTenant(ctx context.Context, tenantID string, factor *qrdbsdk.AuthFactor, challenge qrdbtransport.VerifyChallenge) error {
	verifier, err := v.tenantVerifier(ctx, tenantID, "VerifyTenant")
	if err != nil {
		return err
	}
	return verifier.Verify(ctx, factor, challenge)
}

// Verify is the non-tenant fallback entry point. WebAuthn verification REQUIRES
// the login tenant's relying-party service, so this path fails closed and
// directs callers to the tenant-scoped path. The login ceremony always uses
// VerifyTenant (this type implements TenantFactorVerifier), so this method is a
// defense-in-depth guard against the tenant-less standalone factor path silently
// verifying against an undefined relying party.
func (v *tenantWebAuthnVerifier) Verify(ctx context.Context, factor *qrdbsdk.AuthFactor, challenge qrdbtransport.VerifyChallenge) error {
	return &NonTenantWebAuthnVerifyError{}
}

// lateBoundPIVVerifier is the PIV login FactorVerifier injected into the
// composed qrdb node. It bridges the node's in-process login ceremony to xkms's
// PIV hardware verification path.
//
// The xkms PIVHardwareFactorVerifier requires the CA trust roots at construction
// time. Because the qrdb node is built before the CA roots are guaranteed
// available, the verifier is LATE-BOUND via an atomic pointer: the shell is
// injected first (inner nil) and the real xkms verifier is built and attached
// via setVerifier once the CA trust roots are obtainable (after IAM init). Login
// is a runtime operation, so the verifier is always bound by the time the
// ceremony calls Verify.
//
// All verification crypto — chain validation against the CA roots, signature
// verification over the nonce, and revoked-factor rejection — is performed by
// the xkms verifier; this type only resolves the dependency and fails closed.
type lateBoundPIVVerifier struct {
	inner atomic.Pointer[xkmssdk.PIVHardwareFactorVerifier]
}

// newLateBoundPIVVerifier creates an unbound PIV login verifier. Call
// setVerifier once the xkms PIV verifier is built from the CA trust roots.
func newLateBoundPIVVerifier() *lateBoundPIVVerifier {
	return &lateBoundPIVVerifier{}
}

// setVerifier attaches (or replaces) the xkms PIV verifier.
func (v *lateBoundPIVVerifier) setVerifier(inner *xkmssdk.PIVHardwareFactorVerifier) {
	v.inner.Store(inner)
}

// Compile-time check that the verifier satisfies the qrdb interface.
var _ qrdbembedded.FactorVerifier = (*lateBoundPIVVerifier)(nil)

// Type returns the PIV factor type this verifier handles.
func (v *lateBoundPIVVerifier) Type() qrdbtransport.FactorType {
	return qrdbtransport.FactorTypePIV
}

// Verify delegates to the bound xkms PIV verifier, which validates the slot
// certificate chain against the CA trust roots, verifies the signature over the
// challenge nonce, and rejects revoked factors. It fails closed when the verifier
// is unbound (CA trust roots not yet available) rather than accepting the proof.
func (v *lateBoundPIVVerifier) Verify(ctx context.Context, factor *qrdbsdk.AuthFactor, challenge qrdbtransport.VerifyChallenge) error {
	inner := v.inner.Load()
	if inner == nil {
		return &UnboundPIVVerifierError{}
	}
	return inner.Verify(ctx, factor, challenge)
}

// buildLoginFactorVerifiers constructs the three login verifier shells (PIV +
// tenant-aware WebAuthn + PKCS#11) and stores them on the server so
// initStandaloneQRDBNode / initClusteredQRDBNode can inject them into the qrdb
// node via WithLoginFactorVerifier. Called before node construction; PIV and
// WebAuthn dependencies are attached later by bindLoginCeremonyProvider. The
// PKCS#11 verifier requires no external dependencies and is ready immediately.
// Idempotent.
func (s *Server) buildLoginFactorVerifiers() {
	if len(s.loginFactorVerifiers) > 0 {
		return
	}
	s.loginFactorVerifiers = []qrdbembedded.FactorVerifier{
		newLateBoundPIVVerifier(),
		newTenantWebAuthnVerifier(),
		xkmssdk.NewPKCS11FactorVerifier(),
	}
}

// bindLoginFactorVerifiers attaches the runtime dependencies to the late-bound
// login factor verifiers once IAM components and the CA are initialized:
//
//   - The tenant-aware WebAuthn verifier receives the per-tenant WebAuthn
//     service factory from the IAM webauthn manager.
//   - The PIV verifier receives an xkms PIV verifier built from the CA trust
//     roots (s.xkmsCA.CABundleCertPool()).
//   - The PKCS#11 verifier needs no binding: it is a stateless possession-only
//     verifier whose only inputs are the factor record's enrolled public key and
//     the challenge nonce+signature submitted at login time.
//
// Binding is best-effort per verifier: a missing WebAuthn manager leaves the
// WebAuthn verifier unbound (it fails closed), and an absent CA leaves the PIV
// verifier unbound (it fails closed). It never panics and never binds a verifier
// to the wrong tenant. Called from bindLoginCeremonyProvider after IAM init.
func (s *Server) bindLoginFactorVerifiers(c *iamComponents) {
	for _, v := range s.loginFactorVerifiers {
		switch ver := v.(type) {
		case *tenantWebAuthnVerifier:
			if c != nil && c.webauthnManager != nil {
				if factory := c.webauthnManager.Factory(); factory != nil {
					ver.setFactory(factory)
				}
			}
		case *lateBoundPIVVerifier:
			s.bindPIVLoginVerifier(ver)
		}
	}
}

// bindPIVLoginVerifier builds the xkms PIV verifier from the CA trust roots and
// attaches it to the late-bound PIV login verifier. The trust roots are obtained
// from the CA the server already holds (s.xkmsCA.CABundleCertPool()) — the same
// roots the bootstrap TLS path uses for client-cert validation — so no roots are
// hardcoded. A nil CA or a roots-resolution failure leaves the verifier unbound;
// it then fails closed at verification time.
func (s *Server) bindPIVLoginVerifier(ver *lateBoundPIVVerifier) {
	if s.xkmsCA == nil {
		s.logger.Warn("PIV login verifier left unbound: no CA available for trust roots")
		return
	}
	roots, err := s.xkmsCA.CABundleCertPool()
	if err != nil {
		s.logger.WithError(err).Warn("PIV login verifier left unbound: failed to obtain CA trust roots")
		return
	}
	if roots == nil {
		s.logger.Warn("PIV login verifier left unbound: CA returned nil trust roots")
		return
	}

	// Extend the daemon CA roots with the operator's security.tls_ca_file set
	// so PIV factor logins verify certificates the operator PKI issued: the
	// same anchors the TLS handshake trusts. The file was loaded, and its
	// errors refused, at boot.
	if s.config != nil && s.config.IAM.PIVLogin.Enabled {
		s.clientTrustAnchors.appendTo(roots)
	}

	inner, err := xkmssdk.NewPIVHardwareFactorVerifier(roots, nil)
	if err != nil {
		s.logger.WithError(err).Warn("PIV login verifier left unbound: failed to build xkms PIV verifier")
		return
	}
	ver.setVerifier(inner)
	s.logger.Info("PIV login verifier bound to CA trust roots")
}
