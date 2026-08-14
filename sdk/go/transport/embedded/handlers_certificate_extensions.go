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
	"reflect"

	certclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Certificate Extension Handlers (CrossSign / Chain / IssueDeviceIdentity)
// =============================================================================
//
// These embedded handlers back Security().CertificateExt(). They accept
// either the canonical pkg/service request type or the leaf client/certificate
// wire type the SDK dispatches, and always return the leaf client/certificate
// response type the SDK type-asserts on.

func handleCertCrossSign(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	var svcReq *service.CrossSignRequest
	switch r := req.(type) {
	case *service.CrossSignRequest:
		svcReq = r
	case *certclient.CrossSignRequest:
		svcReq = &service.CrossSignRequest{ForeignRootPEM: r.ForeignRootPEM}
	default:
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "CrossSign",
			"*service.CrossSignRequest or *certificate.CrossSignRequest",
			reflect.TypeOf(req).String(),
		)
	}

	resp, err := certSvc.CrossSign(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return &certclient.CrossSignResponse{CrossSignedPEM: resp.CrossSignedPEM}, nil
}

func handleCertChain(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	var svcReq *service.CertificateChainRequest
	switch r := req.(type) {
	case *service.CertificateChainRequest:
		svcReq = r
	case *certclient.ChainRequest:
		svcReq = &service.CertificateChainRequest{CertificatePEM: r.CertificatePEM}
	default:
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "Chain",
			"*service.CertificateChainRequest or *certificate.ChainRequest",
			reflect.TypeOf(req).String(),
		)
	}

	resp, err := certSvc.Chain(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return &certclient.ChainResponse{ChainPEM: resp.ChainPEM}, nil
}

func handleCertIssueDeviceIdentity(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	var svcReq *service.IssueDeviceIdentityRequest
	switch r := req.(type) {
	case *service.IssueDeviceIdentityRequest:
		svcReq = r
	case *certclient.DeviceIdentityRequest:
		svcReq = DeviceIdentityServiceRequest(r)
	default:
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "IssueDeviceIdentity",
			"*service.IssueDeviceIdentityRequest or *certificate.DeviceIdentityRequest",
			reflect.TypeOf(req).String(),
		)
	}

	resp, err := certSvc.IssueDeviceIdentity(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return DeviceIdentityClientResponse(resp), nil
}

// DeviceIdentityServiceRequest converts the leaf client wire request into the
// canonical service request. It is exported so the other transports share one
// conversion instead of each repeating the field list.
func DeviceIdentityServiceRequest(r *certclient.DeviceIdentityRequest) *service.IssueDeviceIdentityRequest {
	if r == nil {
		return nil
	}
	return &service.IssueDeviceIdentityRequest{
		Type:           string(r.Type),
		DeviceID:       r.DeviceID,
		CSRPEM:         r.CSRPEM,
		EKPub:          r.EKPub,
		AKPub:          r.AKPub,
		QualifyingData: r.QualifyingData,
		ValidityDays:   r.ValidityDays,
		TenantID:       r.TenantID,
	}
}

// DeviceIdentityClientResponse converts the canonical service response into
// the leaf client wire response the SDK type-asserts on.
func DeviceIdentityClientResponse(resp *service.IssueDeviceIdentityResponse) *certclient.DeviceIdentityResponse {
	if resp == nil {
		return nil
	}
	return &certclient.DeviceIdentityResponse{
		CertificatePEM: resp.CertificatePEM,
		ChainPEM:       resp.ChainPEM,
		SerialNumber:   resp.SerialNumber,
		NotBefore:      resp.NotBefore,
		NotAfter:       resp.NotAfter,
	}
}
