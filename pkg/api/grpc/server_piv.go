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

package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// Compile-time assertion that *Server satisfies the generated PIVService
// server interface. The PIV handlers live on the same *Server that backs
// the QuicBaaS facade; every verb delegates to service.PIV(), which
// forwards to the composed go-xkms service, so registration requires no
// separate backing type.
var _ pb.PIVServiceServer = (*Server)(nil)

// RegisterPIVServiceServer registers the PIVService handlers with a gRPC
// server. The handlers delegate to the QuicBaaS service's PIV()
// sub-service (the go-xkms PIVService contract verbatim), keeping the gRPC
// PIV surface symmetric with the REST /api/v1/xkms/piv routes and the
// QUIC/MCP/Unix/embedded transports.
func RegisterPIVServiceServer(server *grpc.Server, srv *Server) {
	pb.RegisterPIVServiceServer(server, srv)
}

// ListPIVSlots returns the status of all PIV slots in the requested backend.
func (s *Server) ListPIVSlots(ctx context.Context, req *pb.PIVListSlotsRequest) (*pb.PIVListSlotsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	pivService := s.service.PIV()
	if pivService == nil {
		return nil, status.Error(codes.Unimplemented, "piv service not available")
	}

	wireReq := &xkmstransport.ListPIVSlotsRequest{
		Backend:  req.Backend,
		TenantID: req.TenantId,
	}
	if req.Page != nil {
		wireReq.Page = int(req.Page.Page)
		wireReq.PageSize = int(req.Page.PageSize)
		wireReq.SortField = req.Page.SortField
		wireReq.SortOrder = int(req.Page.SortOrder)
	}

	resp, err := pivService.ListPIVSlots(ctx, wireReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	out := &pb.PIVListSlotsResponse{
		Slots: make([]*pb.PIVSlotStatus, 0, len(resp.Slots)),
		Page: &pb.PIVPageResponse{
			Page:     int32(resp.Page),
			PageSize: int32(resp.PageSize),
			Total:    int32(resp.Total),
			HasMore:  resp.HasMore,
		},
	}
	for i := range resp.Slots {
		slot := &resp.Slots[i]
		out.Slots = append(out.Slots, &pb.PIVSlotStatus{
			Slot:        slot.Slot,
			Name:        slot.Name,
			Description: slot.Description,
			HasCert:     slot.HasCert,
			HasKey:      slot.HasKey,
			Subject:     slot.Subject,
			Issuer:      slot.Issuer,
			Algorithm:   slot.Algorithm,
			KeySize:     int32(slot.KeySize),
			NotAfter:    slot.NotAfter,
			Fingerprint: slot.Fingerprint,
			Backend:     slot.Backend,
		})
	}
	return out, nil
}

// GetPIVCertificate retrieves the certificate stored in a PIV slot.
func (s *Server) GetPIVCertificate(ctx context.Context, req *pb.PIVGetCertificateRequest) (*pb.PIVGetCertificateResponse, error) {
	return s.pivCertificate(ctx, req, false)
}

// ExportPIVCertificate exports the certificate from a PIV slot.
func (s *Server) ExportPIVCertificate(ctx context.Context, req *pb.PIVGetCertificateRequest) (*pb.PIVGetCertificateResponse, error) {
	return s.pivCertificate(ctx, req, true)
}

// pivCertificate is the shared body of GetPIVCertificate and
// ExportPIVCertificate — the two verbs exchange identical wire shapes and
// differ only in which service verb they invoke.
func (s *Server) pivCertificate(ctx context.Context, req *pb.PIVGetCertificateRequest, export bool) (*pb.PIVGetCertificateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	pivService := s.service.PIV()
	if pivService == nil {
		return nil, status.Error(codes.Unimplemented, "piv service not available")
	}

	wireReq := &xkmstransport.GetPIVCertificateRequest{
		Backend:  req.Backend,
		Slot:     req.Slot,
		Format:   req.Format,
		TenantID: req.TenantId,
	}

	var (
		resp *xkmstransport.GetPIVCertificateResponse
		err  error
	)
	if export {
		resp, err = pivService.ExportPIVCertificate(ctx, wireReq)
	} else {
		resp, err = pivService.GetPIVCertificate(ctx, wireReq)
	}
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.PIVGetCertificateResponse{
		Slot:        resp.Slot,
		Certificate: resp.Certificate,
		Format:      resp.Format,
	}, nil
}

// StorePIVCertificate stores a certificate in a PIV slot.
func (s *Server) StorePIVCertificate(ctx context.Context, req *pb.PIVStoreCertificateRequest) (*pb.PIVStoreCertificateResponse, error) {
	return s.pivStoreCertificate(ctx, req, false)
}

// ImportPIVCertificate imports a certificate into a PIV slot.
func (s *Server) ImportPIVCertificate(ctx context.Context, req *pb.PIVStoreCertificateRequest) (*pb.PIVStoreCertificateResponse, error) {
	return s.pivStoreCertificate(ctx, req, true)
}

// pivStoreCertificate is the shared body of StorePIVCertificate and
// ImportPIVCertificate.
func (s *Server) pivStoreCertificate(ctx context.Context, req *pb.PIVStoreCertificateRequest, importCert bool) (*pb.PIVStoreCertificateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	pivService := s.service.PIV()
	if pivService == nil {
		return nil, status.Error(codes.Unimplemented, "piv service not available")
	}

	wireReq := &xkmstransport.StorePIVCertificateRequest{
		Backend:     req.Backend,
		Slot:        req.Slot,
		Certificate: req.Certificate,
		Format:      req.Format,
		TenantID:    req.TenantId,
	}

	var err error
	if importCert {
		err = pivService.ImportPIVCertificate(ctx, wireReq)
	} else {
		err = pivService.StorePIVCertificate(ctx, wireReq)
	}
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.PIVStoreCertificateResponse{}, nil
}

// DeletePIVCertificate removes the certificate from a PIV slot.
func (s *Server) DeletePIVCertificate(ctx context.Context, req *pb.PIVDeleteCertificateRequest) (*pb.PIVDeleteCertificateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	pivService := s.service.PIV()
	if pivService == nil {
		return nil, status.Error(codes.Unimplemented, "piv service not available")
	}

	if err := pivService.DeletePIVCertificate(ctx, &xkmstransport.DeletePIVCertificateRequest{
		Backend:  req.Backend,
		Slot:     req.Slot,
		TenantID: req.TenantId,
	}); err != nil {
		return nil, toProtoError(err)
	}
	return &pb.PIVDeleteCertificateResponse{}, nil
}

// GeneratePIVKey generates a new key pair in a PIV slot.
func (s *Server) GeneratePIVKey(ctx context.Context, req *pb.PIVGenerateKeyRequest) (*pb.PIVGenerateKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	pivService := s.service.PIV()
	if pivService == nil {
		return nil, status.Error(codes.Unimplemented, "piv service not available")
	}

	resp, err := pivService.GeneratePIVKey(ctx, &xkmstransport.GeneratePIVKeyRequest{
		Backend:      req.Backend,
		Slot:         req.Slot,
		Algorithm:    req.Algorithm,
		Subject:      req.Subject,
		Organization: req.Organization,
		Country:      req.Country,
		OutputFormat: req.OutputFormat,
		ValidityDays: int(req.ValidityDays),
		TenantID:     req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.PIVGenerateKeyResponse{
		Slot:        resp.Slot,
		Certificate: resp.Certificate,
		PublicKey:   resp.PublicKey,
		Csr:         resp.CSR,
		ChainPem:    resp.ChainPEM,
	}, nil
}

// GeneratePIVCSR generates a certificate signing request for a PIV slot key.
func (s *Server) GeneratePIVCSR(ctx context.Context, req *pb.PIVGenerateCSRRequest) (*pb.PIVGenerateCSRResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	pivService := s.service.PIV()
	if pivService == nil {
		return nil, status.Error(codes.Unimplemented, "piv service not available")
	}

	resp, err := pivService.GeneratePIVCSR(ctx, &xkmstransport.GeneratePIVCSRRequest{
		Backend:            req.Backend,
		Slot:               req.Slot,
		Subject:            req.Subject,
		Organization:       req.Organization,
		OrganizationalUnit: req.OrganizationalUnit,
		Country:            req.Country,
		State:              req.State,
		Locality:           req.Locality,
		TenantID:           req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.PIVGenerateCSRResponse{
		Slot: resp.Slot,
		Csr:  resp.CSR,
	}, nil
}
