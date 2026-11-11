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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// ErrPIVNilRequest is returned when a PIV verb receives a nil request.
var ErrPIVNilRequest = &errors.QBError{Code: errors.ErrInvalidInput, Op: "PIVService", Err: errPIVNilRequest}

var errPIVNilRequest = stderrors.New("request must not be nil")

// PIVService is the backend-scoped PIV surface QuicBaaS exposes under
// Security().Auth().PIV(). It is the go-xkms PIVService contract verbatim
// (backend-scoped slots: file / pkcs11 / tpm2), aliased so every QuicBaaS
// transport serves exactly the xkms wire shape. QuicBaaS adds no PIV logic
// of its own: the servicer below delegates each verb to the composed
// go-xkms XKMSService, whose PIV methods are platform-scoped.
type PIVService = xkmstransport.PIVService

// pivService delegates every PIV verb to the method of the same name on the
// composed go-xkms XKMSService. The PIV card is platform-global state, so each
// of those methods admits only a cross-tenant (system) identity and refuses a
// tenant-scoped caller with go-xkms's TenantBindingError (go-xkms#1227). All
// request validation, backend resolution, and typed errors come from go-xkms.
type pivService struct {
	// resolve returns the composed go-xkms service, or nil while it is not
	// yet composed. It is late-bound: the service exists only after startup.
	resolve func() *xkmssdk.XKMSService
}

// NewPIVService returns the PIV composition service over the go-xkms service
// resolve returns at call time.
func NewPIVService(resolve func() *xkmssdk.XKMSService) PIVService {
	return &pivService{resolve: resolve}
}

// composedXKMSService returns the process-global go-xkms service, or nil while
// it is not initialized.
func composedXKMSService() *xkmssdk.XKMSService {
	svc, err := xkmssdk.Get()
	if err != nil {
		return nil
	}
	return svc
}

// platformService returns the composed go-xkms service, or go-xkms's typed
// not-initialized error while there is none.
func (p *pivService) platformService() (*xkmssdk.XKMSService, error) {
	if p.resolve == nil {
		return nil, xkmssdk.ServiceErrPIVNotInitialized
	}
	svc := p.resolve()
	if svc == nil {
		return nil, xkmssdk.ServiceErrPIVNotInitialized
	}
	return svc, nil
}

// platformScoped projects the tenant bound to ctx into the go-qrdb identity
// that go-xkms's platform-scope check reads. That check sees only the qrdb
// Identity and reads a missing one as system scope, while an in-process
// caller (the embedded transport) binds its tenant only through tenantctx.
// An identity a transport already authenticated is authoritative and is kept;
// a context bound to no tenant, or to the system tenant, is left unchanged.
func platformScoped(ctx context.Context) context.Context {
	if qrdbsdk.IdentityFromContext(ctx) != nil {
		return ctx
	}
	tenantID, ok := tenantctx.Lookup(ctx)
	if !ok || tenantctx.IsSystem(tenantID) {
		return ctx
	}
	return qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{TenantID: tenantID})
}

// ListPIVSlots returns the status of all PIV slots in the requested backend.
func (p *pivService) ListPIVSlots(ctx context.Context, req *xkmstransport.ListPIVSlotsRequest) (*xkmstransport.ListPIVSlotsResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	svc, err := p.platformService()
	if err != nil {
		return nil, err
	}
	return svc.ListPIVSlots(platformScoped(ctx), req)
}

// GetPIVCertificate retrieves the certificate stored in a PIV slot.
func (p *pivService) GetPIVCertificate(ctx context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	svc, err := p.platformService()
	if err != nil {
		return nil, err
	}
	return svc.GetPIVCertificate(platformScoped(ctx), req)
}

// StorePIVCertificate stores a certificate in a PIV slot.
func (p *pivService) StorePIVCertificate(ctx context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	if req == nil {
		return ErrPIVNilRequest
	}
	svc, err := p.platformService()
	if err != nil {
		return err
	}
	return svc.StorePIVCertificate(platformScoped(ctx), req)
}

// DeletePIVCertificate removes the certificate from a PIV slot.
func (p *pivService) DeletePIVCertificate(ctx context.Context, req *xkmstransport.DeletePIVCertificateRequest) error {
	if req == nil {
		return ErrPIVNilRequest
	}
	svc, err := p.platformService()
	if err != nil {
		return err
	}
	return svc.DeletePIVCertificate(platformScoped(ctx), req)
}

// GeneratePIVKey generates a new key pair in a PIV slot with a self-signed
// certificate.
func (p *pivService) GeneratePIVKey(ctx context.Context, req *xkmstransport.GeneratePIVKeyRequest) (*xkmstransport.GeneratePIVKeyResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	svc, err := p.platformService()
	if err != nil {
		return nil, err
	}
	return svc.GeneratePIVKey(platformScoped(ctx), req)
}

// ImportPIVCertificate imports a certificate into a PIV slot.
func (p *pivService) ImportPIVCertificate(ctx context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	if req == nil {
		return ErrPIVNilRequest
	}
	svc, err := p.platformService()
	if err != nil {
		return err
	}
	return svc.ImportPIVCertificate(platformScoped(ctx), req)
}

// ExportPIVCertificate exports the certificate from a PIV slot.
func (p *pivService) ExportPIVCertificate(ctx context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	svc, err := p.platformService()
	if err != nil {
		return nil, err
	}
	return svc.ExportPIVCertificate(platformScoped(ctx), req)
}

// GeneratePIVCSR generates a certificate signing request for a PIV slot key.
func (p *pivService) GeneratePIVCSR(ctx context.Context, req *xkmstransport.GeneratePIVCSRRequest) (*xkmstransport.GeneratePIVCSRResponse, error) {
	if req == nil {
		return nil, ErrPIVNilRequest
	}
	svc, err := p.platformService()
	if err != nil {
		return nil, err
	}
	return svc.GeneratePIVCSR(platformScoped(ctx), req)
}
