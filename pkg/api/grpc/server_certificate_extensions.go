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

// server_certificate_extensions.go implements the three CertificateService
// RPCs that back the SDK's Security().CertificateExt() surface: CrossSign,
// Chain and IssueDeviceIdentity. They delegate to the same CertificateService
// the rest of the gRPC certificate surface uses, which composes the xkms
// TrustedCA / TCGCA through pkg/server/ca_service_adapter_extensions.go.

package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// CrossSign cross-signs an external root certificate with the local CA.
func (s *Server) CrossSign(ctx context.Context, req *pb.CrossSignRequest) (*pb.CrossSignResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if len(req.ForeignRootPem) == 0 {
		return nil, status.Error(codes.InvalidArgument, "foreign_root_pem is required")
	}

	certService := s.service.Certificate()
	if certService == nil {
		return nil, status.Error(codes.Unimplemented, "certificate service not available")
	}

	resp, err := certService.CrossSign(ctx, &service.CrossSignRequest{
		ForeignRootPEM: req.ForeignRootPem,
		TenantID:       req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CrossSignResponse{
		CrossSignedPem: resp.CrossSignedPEM,
		Timestamp:      timestamppb.New(time.Now()),
	}, nil
}

// Chain resolves the issuer chain for a certificate, ordered issuer-first and
// root-last.
func (s *Server) Chain(ctx context.Context, req *pb.CertificateChainRequest) (*pb.CertificateChainResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if len(req.CertificatePem) == 0 {
		return nil, status.Error(codes.InvalidArgument, "certificate_pem is required")
	}

	certService := s.service.Certificate()
	if certService == nil {
		return nil, status.Error(codes.Unimplemented, "certificate service not available")
	}

	resp, err := certService.Chain(ctx, &service.CertificateChainRequest{
		CertificatePEM: req.CertificatePem,
		TenantID:       req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CertificateChainResponse{
		ChainPem:  resp.ChainPEM,
		Timestamp: timestamppb.New(time.Now()),
	}, nil
}

// IssueDeviceIdentity issues a TCG device identity certificate (EK, AK or
// IDevID) for the requested device.
func (s *Server) IssueDeviceIdentity(ctx context.Context, req *pb.IssueDeviceIdentityRequest) (*pb.IssueDeviceIdentityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.Type == "" {
		return nil, status.Error(codes.InvalidArgument, "type is required")
	}
	if req.DeviceId == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}

	certService := s.service.Certificate()
	if certService == nil {
		return nil, status.Error(codes.Unimplemented, "certificate service not available")
	}

	resp, err := certService.IssueDeviceIdentity(ctx, &service.IssueDeviceIdentityRequest{
		Type:           req.Type,
		DeviceID:       req.DeviceId,
		CSRPEM:         req.CsrPem,
		EKPub:          req.EkPub,
		AKPub:          req.AkPub,
		QualifyingData: req.QualifyingData,
		ValidityDays:   int(req.ValidityDays),
		TenantID:       req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.IssueDeviceIdentityResponse{
		CertificatePem: resp.CertificatePEM,
		ChainPem:       resp.ChainPEM,
		SerialNumber:   resp.SerialNumber,
		NotBefore:      timestamppb.New(resp.NotBefore),
		NotAfter:       timestamppb.New(resp.NotAfter),
		Timestamp:      timestamppb.New(time.Now()),
	}, nil
}
