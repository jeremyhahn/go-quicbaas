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
)

// ErrPIVNilRequest is returned when a PIV verb receives a nil request.
var ErrPIVNilRequest = &errors.QBError{Code: errors.ErrInvalidInput, Op: "PIVService", Err: errPIVNilRequest}

var errPIVNilRequest = stderrors.New("request must not be nil")

// PIVService is the backend-scoped PIV surface QuicBaaS exposes under
// Security().Auth().PIV(). It is the go-xkms PIVService contract verbatim
// (backend-scoped slots: file / pkcs11 / tpm2), aliased so every QuicBaaS
// transport serves exactly the xkms wire shape. QuicBaaS adds no PIV logic
// of its own: the servicer below delegates each verb to the composed
// go-xkms service functions.
type PIVService = xkmstransport.PIVService

// pivService delegates every PIV verb to the composed go-xkms service
// (xkmssdk.Service* package functions, backed by the embedded XKMSService
// singleton). All request validation, backend resolution, and typed errors
// (ServiceErrPIVNotInitialized, ServiceErrPIVBackendNotFound, ...) come
// from go-xkms.
type pivService struct{}

// NewPIVService returns the PIV composition service. It is stateless: the
// backend registry lives inside the composed go-xkms service singleton.
func NewPIVService() PIVService {
	return &pivService{}
}

// ListPIVSlots returns the status of all PIV slots in the requested backend.
func (p *pivService) ListPIVSlots(ctx context.Context, req *xkmstransport.ListPIVSlotsRequest) (*xkmstransport.ListPIVSlotsResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	return xkmssdk.ServiceListPIVSlots(ctx, req)
}

// GetPIVCertificate retrieves the certificate stored in a PIV slot.
func (p *pivService) GetPIVCertificate(ctx context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	return xkmssdk.ServiceGetPIVCertificate(ctx, req)
}

// StorePIVCertificate stores a certificate in a PIV slot.
func (p *pivService) StorePIVCertificate(ctx context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	if req == nil {
		return ErrPIVNilRequest
	}
	return xkmssdk.ServiceStorePIVCertificate(ctx, req)
}

// DeletePIVCertificate removes the certificate from a PIV slot.
func (p *pivService) DeletePIVCertificate(ctx context.Context, req *xkmstransport.DeletePIVCertificateRequest) error {
	if req == nil {
		return ErrPIVNilRequest
	}
	return xkmssdk.ServiceDeletePIVCertificate(ctx, req)
}

// GeneratePIVKey generates a new key pair in a PIV slot with a self-signed
// certificate.
func (p *pivService) GeneratePIVKey(ctx context.Context, req *xkmstransport.GeneratePIVKeyRequest) (*xkmstransport.GeneratePIVKeyResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	return xkmssdk.ServiceGeneratePIVKey(ctx, req)
}

// ImportPIVCertificate imports a certificate into a PIV slot.
func (p *pivService) ImportPIVCertificate(ctx context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	if req == nil {
		return ErrPIVNilRequest
	}
	return xkmssdk.ServiceImportPIVCertificate(ctx, req)
}

// ExportPIVCertificate exports the certificate from a PIV slot.
func (p *pivService) ExportPIVCertificate(ctx context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	return xkmssdk.ServiceExportPIVCertificate(ctx, req)
}

// GeneratePIVCSR generates a certificate signing request for a PIV slot key.
func (p *pivService) GeneratePIVCSR(ctx context.Context, req *xkmstransport.GeneratePIVCSRRequest) (*xkmstransport.GeneratePIVCSRResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	return xkmssdk.ServiceGeneratePIVCSR(ctx, req)
}
