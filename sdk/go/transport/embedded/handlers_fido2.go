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

package embedded

import (
	"context"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// FIDO2 Handlers
// =============================================================================
//
// The FIDO2 service is the tenant-scoped WebAuthn ceremony surface
// (Security().Auth().FIDO2()). These embedded handlers delegate to the
// service-layer FIDO2Service and exchange the go-xkms transport wire types
// verbatim. The xkms wire structs carry no tenant field, so the tenant
// scope rides the context (transport.ContextWithTenant); an empty tenant
// fails closed in the service layer with its canonical typed error
// (service.ErrFIDO2TenantRequired). No trusted-context wrapping is applied,
// matching the other composed-security handlers (Trust / Attestation).

func handleFIDO2BeginRegistration(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	fido2Svc := svc.FIDO2()
	if fido2Svc == nil {
		return nil, NewServiceNotConfiguredError("FIDO2")
	}

	sdkReq, ok := req.(*xkmstransport.BeginRegistrationRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.FIDO2", "BeginRegistration",
			"*xkmstransport.BeginRegistrationRequest",
			typeNameOf(req),
		)
	}

	return fido2Svc.BeginRegistration(ctx, transport.TenantFromContext(ctx), sdkReq)
}

func handleFIDO2FinishRegistration(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	fido2Svc := svc.FIDO2()
	if fido2Svc == nil {
		return nil, NewServiceNotConfiguredError("FIDO2")
	}

	sdkReq, ok := req.(*xkmstransport.FinishRegistrationRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.FIDO2", "FinishRegistration",
			"*xkmstransport.FinishRegistrationRequest",
			typeNameOf(req),
		)
	}

	return fido2Svc.FinishRegistration(ctx, transport.TenantFromContext(ctx), sdkReq)
}

func handleFIDO2BeginAuthentication(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	fido2Svc := svc.FIDO2()
	if fido2Svc == nil {
		return nil, NewServiceNotConfiguredError("FIDO2")
	}

	sdkReq, ok := req.(*xkmstransport.BeginAuthenticationRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.FIDO2", "BeginAuthentication",
			"*xkmstransport.BeginAuthenticationRequest",
			typeNameOf(req),
		)
	}

	return fido2Svc.BeginAuthentication(ctx, transport.TenantFromContext(ctx), sdkReq)
}

func handleFIDO2FinishAuthentication(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	fido2Svc := svc.FIDO2()
	if fido2Svc == nil {
		return nil, NewServiceNotConfiguredError("FIDO2")
	}

	sdkReq, ok := req.(*xkmstransport.FinishAuthenticationRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.FIDO2", "FinishAuthentication",
			"*xkmstransport.FinishAuthenticationRequest",
			typeNameOf(req),
		)
	}

	return fido2Svc.FinishAuthentication(ctx, transport.TenantFromContext(ctx), sdkReq)
}
