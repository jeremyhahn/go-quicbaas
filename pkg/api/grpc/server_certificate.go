// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Compile-time assertion that the QuicBaaS gRPC Server satisfies the generated
// CertificateService server interface. The certificate handler methods live on
// the same *Server that backs the QuicBaaS facade, so registering the service
// requires no separate backing type.
var _ pb.CertificateServiceServer = (*Server)(nil)

// RegisterCertificateServiceServer registers the CertificateService handlers
// with a gRPC server. The handlers are implemented directly on *Server and
// delegate to the QuicBaaS service's Certificate() sub-service, which composes
// the xkms TrustedCA. This keeps the gRPC certificate surface symmetric with
// the REST /api/v1/ca routes and the QUIC/MCP/Unix certificate transports.
func RegisterCertificateServiceServer(server *grpc.Server, srv *Server) {
	pb.RegisterCertificateServiceServer(server, srv)
}

// IssueCertificate generates a new X.509 certificate with the specified parameters.
func (s *Server) IssueCertificate(ctx context.Context, req *pb.IssueCertificateRequest) (*pb.IssueCertificateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.CommonName == "" {
		return nil, status.Error(codes.InvalidArgument, "common_name is required")
	}

	// Get certificate service
	certService := s.service.Certificate()
	if certService == nil {
		return nil, status.Error(codes.Unimplemented, "certificate service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.IssueCertificateRequest{
		CommonName:         req.CommonName,
		Organization:       req.Organization,
		OrganizationalUnit: req.OrganizationalUnit,
		Country:            req.Country,
		Province:           req.Province,
		Locality:           req.Locality,
		DNSNames:           req.DnsNames,
		IPAddresses:        req.IpAddresses,
		EmailAddresses:     req.EmailAddresses,
		URIs:               req.Uris,
		ValidityDays:       int(req.ValidityDays),
		Usage:              protoCertUsageToString(req.Usage),
		IsCA:               req.IsCa,
		MaxPathLen:         int(req.MaxPathLen),
		KeyAlgorithm:       protoCertKeyAlgToString(req.KeyAlgorithm),
		KeySize:            int(req.KeySize),
		Metadata:           req.Metadata,
	}

	// Call service
	resp, err := certService.IssueCertificate(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.IssueCertificateResponse{
		CertificatePem:   resp.CertificatePEM,
		PrivateKeyPem:    resp.PrivateKeyPEM,
		CaCertificatePem: resp.CACertificatePEM,
		ChainPem:         resp.ChainPEM,
		SerialNumber:     resp.SerialNumber,
		NotBefore:        timestamppb.New(resp.NotBefore),
		NotAfter:         timestamppb.New(resp.NotAfter),
		Timestamp:        timestamppb.New(now),
	}, nil
}

// SignCSR signs a Certificate Signing Request with the CA.
func (s *Server) SignCSR(ctx context.Context, req *pb.SignCSRRequest) (*pb.SignCSRResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.CsrPem == "" {
		return nil, status.Error(codes.InvalidArgument, "csr_pem is required")
	}

	// Get certificate service
	certService := s.service.Certificate()
	if certService == nil {
		return nil, status.Error(codes.Unimplemented, "certificate service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.SignCSRRequest{
		CSRPEM:       req.CsrPem,
		ValidityDays: int(req.ValidityDays),
		Usage:        protoCertUsageToString(req.Usage),
	}

	// Call service
	resp, err := certService.SignCSR(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.SignCSRResponse{
		CertificatePem:   resp.CertificatePEM,
		CaCertificatePem: resp.CACertificatePEM,
		ChainPem:         resp.ChainPEM,
		SerialNumber:     resp.SerialNumber,
		NotBefore:        timestamppb.New(resp.NotBefore),
		NotAfter:         timestamppb.New(resp.NotAfter),
		Timestamp:        timestamppb.New(now),
	}, nil
}

// RevokeCertificate marks a certificate as revoked by serial number.
func (s *Server) RevokeCertificate(ctx context.Context, req *pb.RevokeCertificateRequest) (*pb.RevokeCertificateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.SerialNumber == "" {
		return nil, status.Error(codes.InvalidArgument, "serial_number is required")
	}

	// Get certificate service
	certService := s.service.Certificate()
	if certService == nil {
		return nil, status.Error(codes.Unimplemented, "certificate service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.RevokeCertificateRequest{
		SerialNumber: req.SerialNumber,
	}

	// Call service
	resp, err := certService.RevokeCertificate(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.RevokeCertificateResponse{
		Success:   resp.Success,
		Timestamp: timestamppb.New(now),
	}, nil
}

// VerifyCertificate checks if a certificate is valid and not revoked.
func (s *Server) VerifyCertificate(ctx context.Context, req *pb.VerifyCertificateRequest) (*pb.VerifyCertificateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.CertificatePem == "" {
		return nil, status.Error(codes.InvalidArgument, "certificate_pem is required")
	}

	// Get certificate service
	certService := s.service.Certificate()
	if certService == nil {
		return nil, status.Error(codes.Unimplemented, "certificate service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.VerifyCertificateRequest{
		CertificatePEM: req.CertificatePem,
	}

	// Call service
	resp, err := certService.VerifyCertificate(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.VerifyCertificateResponse{
		Valid:        resp.Valid,
		ErrorMessage: resp.ErrorMessage,
		Timestamp:    timestamppb.New(now),
	}, nil
}

// GetCACertificate returns the CA certificate and bundle.
func (s *Server) GetCACertificate(ctx context.Context, req *pb.GetCACertificateRequest) (*pb.GetCACertificateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get certificate service
	certService := s.service.Certificate()
	if certService == nil {
		return nil, status.Error(codes.Unimplemented, "certificate service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.GetCACertificateRequest{}

	// Call service
	resp, err := certService.GetCACertificate(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.GetCACertificateResponse{
		CaCertificatePem: resp.CACertificatePEM,
		CaBundlePem:      resp.CABundlePEM,
		Identity:         resp.Identity,
		Timestamp:        timestamppb.New(now),
	}, nil
}

// GetTLSConfig returns a TLS configuration for secure communications.
func (s *Server) GetTLSConfig(ctx context.Context, req *pb.GetTLSConfigRequest) (*pb.GetTLSConfigResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get certificate service
	certService := s.service.Certificate()
	if certService == nil {
		return nil, status.Error(codes.Unimplemented, "certificate service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.GetTLSConfigRequest{
		ServerName:        req.ServerName,
		RequireClientCert: req.RequireClientCert,
		IsServer:          req.IsServer,
		MinVersion:        protoTLSVersionToString(req.MinVersion),
	}

	// Call service
	resp, err := certService.GetTLSConfig(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	// Note: The actual tls.Config is constructed client-side from these parameters
	// We return the certificate PEMs and configuration options
	return &pb.GetTLSConfigResponse{
		ServerName:        req.ServerName,
		RequireClientCert: req.RequireClientCert,
		IsServer:          req.IsServer,
		MinVersion:        req.MinVersion,
		Timestamp:         timestamppb.New(now),
	}, convertTLSConfigToPEM(resp)
}

// =============================================================================
// Conversion Helpers
// =============================================================================

// protoCertUsageToString converts proto CertificateUsage to service usage string.
func protoCertUsageToString(usage pb.CertificateUsage) string {
	switch usage {
	case pb.CertificateUsage_CERTIFICATE_USAGE_CLIENT:
		return "client"
	case pb.CertificateUsage_CERTIFICATE_USAGE_SERVER:
		return "server"
	case pb.CertificateUsage_CERTIFICATE_USAGE_BOTH:
		return "both"
	default:
		return ""
	}
}

// protoCertKeyAlgToString converts proto CertKeyAlgorithm to service algorithm string.
func protoCertKeyAlgToString(alg pb.CertKeyAlgorithm) string {
	switch alg {
	case pb.CertKeyAlgorithm_CERT_KEY_ALG_ECDSA:
		return "ecdsa"
	case pb.CertKeyAlgorithm_CERT_KEY_ALG_RSA:
		return "rsa"
	default:
		return "ecdsa"
	}
}

// protoTLSVersionToString converts proto TLSVersion to service version string.
func protoTLSVersionToString(version pb.TLSVersion) string {
	switch version {
	case pb.TLSVersion_TLS_VERSION_1_2:
		return "1.2"
	case pb.TLSVersion_TLS_VERSION_1_3:
		return "1.3"
	default:
		return "1.2"
	}
}

// convertTLSConfigToPEM is a helper that returns an error if PEM conversion fails.
// Currently just returns nil since we don't expose PEM in GetTLSConfigResponse.
func convertTLSConfigToPEM(resp *service.GetTLSConfigResponse) error {
	// TLS config is constructed client-side from the configuration options
	// We could potentially return certificate PEMs here if needed
	if resp == nil || resp.Config == nil {
		return nil
	}
	return nil
}
