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
	stderrors "errors"
	"log/slog"

	"github.com/go-webauthn/webauthn/protocol"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// FIDO2Service is the tenant-scoped WebAuthn ceremony surface QuicBaaS
// exposes under Security().Auth().FIDO2(). The verb shape is the go-xkms
// FIDO2 transport contract verbatim (BeginRegistration / FinishRegistration /
// BeginAuthentication / FinishAuthentication over the xkms wire types); the
// only QuicBaaS addition is the explicit tenant scope, because QuicBaaS runs
// one relying-party service per tenant while xkms runs exactly one.
//
// The ceremony adaptation itself (options marshaling, attestation/assertion
// parsing, user resolution) is NOT reimplemented here: each per-tenant
// relying-party service is wrapped in go-xkms's canonical FIDO2 servicer
// adapter (xkmssdk.NewWebAuthnFIDO2Servicer), the same code path the xkms
// singleton serves on its own transports.
type FIDO2Service interface {
	// BeginRegistration starts a WebAuthn registration ceremony for the
	// given tenant's relying party.
	BeginRegistration(ctx context.Context, tenantID string, req *xkmstransport.BeginRegistrationRequest) (*xkmstransport.BeginRegistrationResponse, error)

	// FinishRegistration completes a WebAuthn registration ceremony.
	FinishRegistration(ctx context.Context, tenantID string, req *xkmstransport.FinishRegistrationRequest) (*xkmstransport.FinishRegistrationResponse, error)

	// BeginAuthentication starts a WebAuthn authentication ceremony.
	BeginAuthentication(ctx context.Context, tenantID string, req *xkmstransport.BeginAuthenticationRequest) (*xkmstransport.BeginAuthenticationResponse, error)

	// FinishAuthentication completes a WebAuthn authentication ceremony.
	FinishAuthentication(ctx context.Context, tenantID string, req *xkmstransport.FinishAuthenticationRequest) (*xkmstransport.FinishAuthenticationResponse, error)
}

// FIDO2 service typed errors.
var (
	// ErrFIDO2NilRequest is returned when a ceremony verb receives a nil
	// request.
	ErrFIDO2NilRequest = &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "FIDO2Service",
		Err:  &FIDO2RequestInvalidError{Reason: "request must not be nil"},
	}

	// ErrFIDO2TenantRequired is returned when the tenant scope is empty.
	ErrFIDO2TenantRequired = &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "FIDO2Service",
		Err:  &FIDO2RequestInvalidError{Reason: "tenant is required"},
	}

	// ErrFIDO2NotConfigured is returned when the xkms-backed multi-tenant
	// WebAuthn manager is not wired (legacy manager or IAM disabled).
	ErrFIDO2NotConfigured = &errors.QBError{
		Code: errors.ErrUnavailable,
		Op:   "FIDO2Service",
		Err:  &FIDO2ServiceUnavailableError{},
	}
)

// The two types below exist so callers can name THESE conditions rather than
// their error codes. QBError is an alias for go-qrdb's QRDBError, whose Is
// method compares CODES, so errors.Is(err, ErrFIDO2TenantRequired) is true for
// every invalid-input error that reaches the caller -- including refusals
// raised inside a ceremony. On the REST bridge that mattered: the two
// sentinel cases sit ahead of the rule that a failed authentication answers
// with an opaque 401, so any invalid-input failure took the 400 branch and
// described itself (go-quicbaas#434).

// FIDO2RequestInvalidError reports a request this service rejects on its own,
// before any relying party sees it: a nil request body, or an empty tenant
// scope. Reason carries which.
type FIDO2RequestInvalidError struct {
	Reason string
}

// Error implements the error interface.
func (e *FIDO2RequestInvalidError) Error() string { return e.Reason }

// FIDO2ServiceUnavailableError reports that the xkms-backed multi-tenant
// WebAuthn manager is not wired, so no ceremony can run at all.
type FIDO2ServiceUnavailableError struct{}

// Error implements the error interface.
func (e *FIDO2ServiceUnavailableError) Error() string {
	return "xkms-backed WebAuthn manager is not configured"
}

// IsFIDO2RequestInvalid reports whether err is (or wraps) one of this
// service's own request-shape refusals, as opposed to any other invalid-input
// failure raised further down the ceremony.
func IsFIDO2RequestInvalid(err error) bool {
	var target *FIDO2RequestInvalidError
	return stderrors.As(err, &target)
}

// IsFIDO2ServiceUnavailable reports whether err is (or wraps)
// ErrFIDO2NotConfigured, as opposed to any other unavailable-coded failure.
func IsFIDO2ServiceUnavailable(err error) bool {
	var target *FIDO2ServiceUnavailableError
	return stderrors.As(err, &target)
}

// webAuthnManagerGetter resolves the WebAuthn manager the FIDO2 service
// wraps. It is satisfied by *quicBaaS (GetWebAuthnManager) and keeps the
// service constructible in tests without a full service graph.
type webAuthnManagerGetter interface {
	GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
}

// fido2Service resolves the per-tenant xkms relying-party service through
// the IAM WebAuthn factory and delegates each ceremony verb to go-xkms's
// canonical FIDO2 servicer adapter.
type fido2Service struct {
	manager webAuthnManagerGetter
}

// NewFIDO2Service returns the tenant-scoped FIDO2 ceremony composition
// service backed by the supplied WebAuthn manager getter.
func NewFIDO2Service(manager webAuthnManagerGetter) FIDO2Service {
	return &fido2Service{manager: manager}
}

// servicerFor resolves the tenant's relying-party service and wraps it in
// the canonical xkms FIDO2 adapter. Every failure is a typed error: the
// manager being absent or non-xkms yields ErrFIDO2NotConfigured.
func (f *fido2Service) servicerFor(ctx context.Context, tenantID string) (xkmstransport.FIDO2Service, error) {
	svc, _, err := f.servicerAndFactoryFor(ctx, tenantID)
	return svc, err
}

// servicerAndFactoryFor resolves the tenant servicer AND the factory it was
// built from, in ONE lookup.
//
// FinishRegistration needs both: the servicer to run the ceremony, and the
// factory that owns the attestation trust store to verify the statement. It
// used to resolve them separately, and the second lookup failing was not
// treated as an error — the registration proceeded with the attestation proof
// CLEARED, so a real hardware key was recorded as software (or an admin was
// refused) because of a transient failure in the verification path rather than
// anything about the credential.
//
// Two independent lookups that must agree is the defect. Making them fail
// together would be a patch; there is now one lookup, so the question cannot
// arise.
func (f *fido2Service) servicerAndFactoryFor(
	ctx context.Context,
	tenantID string,
) (xkmstransport.FIDO2Service, *iam.WebAuthnServiceFactory, error) {
	if tenantID == "" {
		return nil, nil, ErrFIDO2TenantRequired
	}
	factory, err := f.webAuthnFactory(ctx)
	if err != nil {
		return nil, nil, err
	}
	if factory == nil {
		return nil, nil, ErrFIDO2NotConfigured
	}
	svc, err := factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	return xkmssdk.NewWebAuthnFIDO2Servicer(svc), factory, nil
}

// BeginRegistration starts a WebAuthn registration ceremony for the tenant.
func (f *fido2Service) BeginRegistration(ctx context.Context, tenantID string, req *xkmstransport.BeginRegistrationRequest) (*xkmstransport.BeginRegistrationResponse, error) {
	if req == nil {
		return nil, ErrFIDO2NilRequest
	}
	svc, err := f.servicerFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return svc.BeginRegistration(ctx, req)
}

// FinishRegistration completes a WebAuthn registration ceremony for the
// tenant.
func (f *fido2Service) FinishRegistration(ctx context.Context, tenantID string, req *xkmstransport.FinishRegistrationRequest) (*xkmstransport.FinishRegistrationResponse, error) {
	if req == nil {
		return nil, ErrFIDO2NilRequest
	}
	svc, factory, err := f.servicerAndFactoryFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// Verify the attestation trust path and carry the result to the assurance
	// gate at the credential-store choke point. This is the third and last
	// surface that can finish a registration — the multi-protocol FIDO2 service
	// behind REST, gRPC, QUIC, MCP and Unix — and the attestation object exists
	// only in this request, never on the persisted credential.
	//
	// The factory comes from the SAME resolution that produced the servicer, so
	// the trust roots applied here are the ones the credential store was built
	// with, and there is no second lookup whose failure could quietly cost this
	// registration its proof.
	ctx = f.verifyAttestationChain(ctx, factory, tenantID, req.AttestationJSON)
	return svc.FinishRegistration(ctx, req)
}

// verifyAttestationChain verifies the registration's attestation trust path and
// returns a context carrying the result.
//
// A failure to verify is not an error: whether THIS registrant must prove
// hardware belongs to the assurance gate, which knows the registrant's roles.
// The context then carries no verified chain, which the gate reads as "not
// proven" and refuses only where hardware is required.
func (f *fido2Service) verifyAttestationChain(
	ctx context.Context,
	factory *iam.WebAuthnServiceFactory,
	tenantID, attestationJSON string,
) context.Context {
	// EVERY exit from this function either carries a proof this call just
	// produced or carries none. Returning the incoming context on an early exit
	// would let a caller that planted a chain — an embedded, in-process SDK
	// consumer, say — skip verification by sending an absent or unparseable
	// attestation and keep the planted proof.
	if attestationJSON == "" {
		return iam.ClearAttestationChain(ctx)
	}
	parsed, parseErr := protocol.ParseCredentialCreationResponseBytes([]byte(attestationJSON))
	if parseErr != nil {
		// xkms reports the canonical parse failure; nothing to verify here.
		return iam.ClearAttestationChain(ctx)
	}
	verified, verifyErr := factory.VerifyAttestationChain(ctx, parsed)
	if verifyErr != nil {
		slog.Default().Debug("fido2: attestation trust path not verified",
			"tenant_id", tenantID,
			"error", verifyErr)
	}
	// VerifyAttestationChain clears the proof itself when it fails, so the
	// returned context is correct on both paths.
	return verified
}

// webAuthnFactory resolves the per-tenant WebAuthn factory that owns the
// attestation trust store. It reuses the same manager lookup servicerFor
// performs, so the trust roots applied here are the ones the credential store
// was built with.
func (f *fido2Service) webAuthnFactory(ctx context.Context) (*iam.WebAuthnServiceFactory, error) {
	if f.manager == nil {
		return nil, ErrFIDO2NotConfigured
	}
	mgr, err := f.manager.GetWebAuthnManager(ctx)
	if err != nil {
		return nil, err
	}
	xkmsMgr, ok := mgr.(*iam.XKMSWebAuthnManager)
	if !ok || xkmsMgr == nil {
		return nil, ErrFIDO2NotConfigured
	}
	return xkmsMgr.Factory(), nil
}

// BeginAuthentication starts a WebAuthn authentication ceremony for the
// tenant.
func (f *fido2Service) BeginAuthentication(ctx context.Context, tenantID string, req *xkmstransport.BeginAuthenticationRequest) (*xkmstransport.BeginAuthenticationResponse, error) {
	if req == nil {
		return nil, ErrFIDO2NilRequest
	}
	svc, err := f.servicerFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return svc.BeginAuthentication(ctx, req)
}

// FinishAuthentication completes a WebAuthn authentication ceremony for the
// tenant.
func (f *fido2Service) FinishAuthentication(ctx context.Context, tenantID string, req *xkmstransport.FinishAuthenticationRequest) (*xkmstransport.FinishAuthenticationResponse, error) {
	if req == nil {
		return nil, ErrFIDO2NilRequest
	}
	svc, err := f.servicerFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return svc.FinishAuthentication(ctx, req)
}
