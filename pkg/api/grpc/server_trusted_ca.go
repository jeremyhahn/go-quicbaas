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
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// trustedCAServer serves the generated TrustedCAService on its own type: its
// verbs share names with the CertificateService handlers on *Server. Every
// verb delegates to service.TrustedCA(), the go-xkms CA adapter the REST
// /api/v1/xkms/ca/* routes and the MCP xkms.ca.* tools serve (#1330).
type trustedCAServer struct {
	service service.QuicBaaS
}

var _ pb.TrustedCAServiceServer = (*trustedCAServer)(nil)

// RegisterTrustedCAServiceServer registers the TrustedCAService handlers with
// a gRPC server, the gRPC (and Unix) side of Security().TrustedCA().
func RegisterTrustedCAServiceServer(server *grpc.Server, srv *Server) {
	pb.RegisterTrustedCAServiceServer(server, &trustedCAServer{service: srv.service})
}

// trustedCAService returns the wired go-xkms CA or the gRPC error to answer.
func (s *trustedCAServer) trustedCAService(reqNil bool) (service.TrustedCAService, error) {
	if reqNil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	ca := s.service.TrustedCA()
	if ca == nil {
		return nil, status.Error(codes.Unimplemented, "trusted ca not available")
	}
	return ca, nil
}

// GetCABundle returns the CA certificate chain.
func (s *trustedCAServer) GetCABundle(ctx context.Context, req *pb.TrustedCAGetCABundleRequest) (*pb.TrustedCAGetCABundleResponse, error) {
	ca, err := s.trustedCAService(req == nil)
	if err != nil {
		return nil, err
	}
	resp, err := ca.GetCABundle(ctx, &xkmstransport.GetCABundleRequest{
		StoreType: req.StoreType, Algorithm: req.Algorithm, TenantID: req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.TrustedCAGetCABundleResponse{
		BundlePem: resp.BundlePEM, Certificates: resp.Certificates, ContentType: resp.ContentType,
	}, nil
}

// GetCACertificate returns the issuing CA certificate.
func (s *trustedCAServer) GetCACertificate(ctx context.Context, req *pb.TrustedCAGetCACertificateRequest) (*pb.TrustedCAGetCACertificateResponse, error) {
	ca, err := s.trustedCAService(req == nil)
	if err != nil {
		return nil, err
	}
	resp, err := ca.GetCACertificate(ctx, &xkmstransport.GetCACertificateRequest{
		Identity: req.Identity, TenantID: req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.TrustedCAGetCACertificateResponse{
		CertificatePem: resp.CertificatePEM, Subject: resp.Subject, Issuer: resp.Issuer,
		SerialNumber: resp.SerialNumber, NotBefore: resp.NotBefore, NotAfter: resp.NotAfter, IsCa: resp.IsCA,
	}, nil
}

// SignCSR signs a PKCS#10 certificate signing request.
func (s *trustedCAServer) SignCSR(ctx context.Context, req *pb.TrustedCASignCSRRequest) (*pb.TrustedCASignCSRResponse, error) {
	ca, err := s.trustedCAService(req == nil)
	if err != nil {
		return nil, err
	}
	resp, err := ca.SignCSR(ctx, &xkmstransport.SignCSRRequest{
		CSRPEM: req.CsrPem, Profile: req.Profile, ValidityDays: int(req.ValidityDays), TenantID: req.TenantId,
		AttestationFormat: req.AttestationFormat, AttestationChain: req.AttestationChain,
		AttestationData: req.AttestationData, AttestationSig: req.AttestationSig, AttestationNonce: req.AttestationNonce,
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.TrustedCASignCSRResponse{
		CertificatePem: resp.CertificatePEM, ChainPem: resp.ChainPEM, SerialNumber: resp.SerialNumber,
	}, nil
}

// IssueCertificate generates a key pair in the CA keystore and issues a
// certificate for it.
func (s *trustedCAServer) IssueCertificate(ctx context.Context, req *pb.TrustedCAIssueCertificateRequest) (*pb.TrustedCAIssueCertificateResponse, error) {
	ca, err := s.trustedCAService(req == nil)
	if err != nil {
		return nil, err
	}
	resp, err := ca.IssueCertificate(ctx, &xkmstransport.IssueCertificateRequest{
		Profile: req.Profile, CommonName: req.CommonName, Organization: req.Organization, SANs: req.Sans,
		ValidityDays: int(req.ValidityDays), Algorithm: req.Algorithm, TenantID: req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.TrustedCAIssueCertificateResponse{
		CertificatePem: resp.CertificatePEM, ChainPem: resp.ChainPEM,
		PrivateKeyPem: resp.PrivateKeyPEM, SerialNumber: resp.SerialNumber,
	}, nil
}

// RevokeCertificate revokes a certificate by serial number.
func (s *trustedCAServer) RevokeCertificate(ctx context.Context, req *pb.TrustedCARevokeCertificateRequest) (*pb.TrustedCARevokeCertificateResponse, error) {
	ca, err := s.trustedCAService(req == nil)
	if err != nil {
		return nil, err
	}
	resp, err := ca.RevokeCertificate(ctx, &xkmstransport.RevokeCertificateRequest{
		SerialNumber: req.SerialNumber, Reason: int(req.Reason), TenantID: req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.TrustedCARevokeCertificateResponse{Success: resp.Success, Message: resp.Message}, nil
}

// GenerateCRL produces a signed CRL of the revoked certificates.
func (s *trustedCAServer) GenerateCRL(ctx context.Context, req *pb.TrustedCAGenerateCRLRequest) (*pb.TrustedCAGenerateCRLResponse, error) {
	ca, err := s.trustedCAService(req == nil)
	if err != nil {
		return nil, err
	}
	resp, err := ca.GenerateCRL(ctx, &xkmstransport.GenerateCRLRequest{TenantID: req.TenantId})
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.TrustedCAGenerateCRLResponse{CrlPem: resp.CRLPEM}, nil
}

// IsRevoked reports whether a serial number is revoked.
func (s *trustedCAServer) IsRevoked(ctx context.Context, req *pb.TrustedCAIsRevokedRequest) (*pb.TrustedCAIsRevokedResponse, error) {
	ca, err := s.trustedCAService(req == nil)
	if err != nil {
		return nil, err
	}
	resp, err := ca.IsRevoked(ctx, &xkmstransport.IsRevokedRequest{
		SerialNumber: req.SerialNumber, TenantID: req.TenantId,
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.TrustedCAIsRevokedResponse{Revoked: resp.Revoked, Reason: int32(resp.Reason), Message: resp.Message}, nil
}
