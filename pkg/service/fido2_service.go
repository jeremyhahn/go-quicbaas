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
	ErrFIDO2NilRequest = &errors.QBError{Code: errors.ErrInvalidInput, Op: "FIDO2Service", Err: errFIDO2NilRequest}

	// ErrFIDO2TenantRequired is returned when the tenant scope is empty.
	ErrFIDO2TenantRequired = &errors.QBError{Code: errors.ErrInvalidInput, Op: "FIDO2Service", Err: errFIDO2TenantRequired}

	// ErrFIDO2NotConfigured is returned when the xkms-backed multi-tenant
	// WebAuthn manager is not wired (legacy manager or IAM disabled).
	ErrFIDO2NotConfigured = &errors.QBError{Code: errors.ErrUnavailable, Op: "FIDO2Service", Err: errFIDO2NotConfigured}
)

var (
	errFIDO2NilRequest     = stderrors.New("request must not be nil")
	errFIDO2TenantRequired = stderrors.New("tenant is required")
	errFIDO2NotConfigured  = stderrors.New("xkms-backed WebAuthn manager is not configured")
)

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
	if tenantID == "" {
		return nil, ErrFIDO2TenantRequired
	}
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
	factory := xkmsMgr.Factory()
	if factory == nil {
		return nil, ErrFIDO2NotConfigured
	}
	svc, err := factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return xkmssdk.NewWebAuthnFIDO2Servicer(svc), nil
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
	svc, err := f.servicerFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return svc.FinishRegistration(ctx, req)
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
