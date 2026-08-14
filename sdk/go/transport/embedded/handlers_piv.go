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
)

// =============================================================================
// PIV Handlers
// =============================================================================
//
// The PIV service is the backend-scoped go-xkms PIV surface
// (Security().Auth().PIV()). These embedded handlers delegate to the
// service-layer PIVService and exchange the go-xkms transport wire types
// verbatim — the same request/response structs every other QuicBaaS
// transport carries for the quicbaas.PIV verbs. Verbs without a response
// struct (Store / Delete / Import) return a nil payload on success.

func handlePIVListSlots(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pivSvc := svc.PIV()
	if pivSvc == nil {
		return nil, NewServiceNotConfiguredError("PIV")
	}

	sdkReq, ok := req.(*xkmstransport.ListPIVSlotsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.PIV", "ListPIVSlots",
			"*xkmstransport.ListPIVSlotsRequest",
			typeNameOf(req),
		)
	}

	return pivSvc.ListPIVSlots(ctx, sdkReq)
}

func handlePIVGetCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pivSvc := svc.PIV()
	if pivSvc == nil {
		return nil, NewServiceNotConfiguredError("PIV")
	}

	sdkReq, ok := req.(*xkmstransport.GetPIVCertificateRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.PIV", "GetPIVCertificate",
			"*xkmstransport.GetPIVCertificateRequest",
			typeNameOf(req),
		)
	}

	return pivSvc.GetPIVCertificate(ctx, sdkReq)
}

func handlePIVStoreCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pivSvc := svc.PIV()
	if pivSvc == nil {
		return nil, NewServiceNotConfiguredError("PIV")
	}

	sdkReq, ok := req.(*xkmstransport.StorePIVCertificateRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.PIV", "StorePIVCertificate",
			"*xkmstransport.StorePIVCertificateRequest",
			typeNameOf(req),
		)
	}

	if err := pivSvc.StorePIVCertificate(ctx, sdkReq); err != nil {
		return nil, err
	}
	return nil, nil
}

func handlePIVDeleteCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pivSvc := svc.PIV()
	if pivSvc == nil {
		return nil, NewServiceNotConfiguredError("PIV")
	}

	sdkReq, ok := req.(*xkmstransport.DeletePIVCertificateRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.PIV", "DeletePIVCertificate",
			"*xkmstransport.DeletePIVCertificateRequest",
			typeNameOf(req),
		)
	}

	if err := pivSvc.DeletePIVCertificate(ctx, sdkReq); err != nil {
		return nil, err
	}
	return nil, nil
}

func handlePIVGenerateKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pivSvc := svc.PIV()
	if pivSvc == nil {
		return nil, NewServiceNotConfiguredError("PIV")
	}

	sdkReq, ok := req.(*xkmstransport.GeneratePIVKeyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.PIV", "GeneratePIVKey",
			"*xkmstransport.GeneratePIVKeyRequest",
			typeNameOf(req),
		)
	}

	return pivSvc.GeneratePIVKey(ctx, sdkReq)
}

func handlePIVImportCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pivSvc := svc.PIV()
	if pivSvc == nil {
		return nil, NewServiceNotConfiguredError("PIV")
	}

	sdkReq, ok := req.(*xkmstransport.StorePIVCertificateRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.PIV", "ImportPIVCertificate",
			"*xkmstransport.StorePIVCertificateRequest",
			typeNameOf(req),
		)
	}

	if err := pivSvc.ImportPIVCertificate(ctx, sdkReq); err != nil {
		return nil, err
	}
	return nil, nil
}

func handlePIVExportCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pivSvc := svc.PIV()
	if pivSvc == nil {
		return nil, NewServiceNotConfiguredError("PIV")
	}

	sdkReq, ok := req.(*xkmstransport.GetPIVCertificateRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.PIV", "ExportPIVCertificate",
			"*xkmstransport.GetPIVCertificateRequest",
			typeNameOf(req),
		)
	}

	return pivSvc.ExportPIVCertificate(ctx, sdkReq)
}

func handlePIVGenerateCSR(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	pivSvc := svc.PIV()
	if pivSvc == nil {
		return nil, NewServiceNotConfiguredError("PIV")
	}

	sdkReq, ok := req.(*xkmstransport.GeneratePIVCSRRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.PIV", "GeneratePIVCSR",
			"*xkmstransport.GeneratePIVCSRRequest",
			typeNameOf(req),
		)
	}

	return pivSvc.GeneratePIVCSR(ctx, sdkReq)
}
