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

package server

import (
	ggrpc "google.golang.org/grpc"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
)

// registerCertificateService registers the CertificateService handlers on the
// gRPC listener when the QuicBaaS service exposes a Certificate sub-service.
// The handlers are implemented on the same grpcapi.Server that backs the
// QuicBaaS facade and delegate to the xkms-composed TrustedCA via
// service.Certificate(). Wiring this keeps the gRPC certificate surface
// symmetric with the REST /api/v1/ca routes and the QUIC/MCP/Unix transports.
func (s *Server) registerCertificateService(grpcServer *ggrpc.Server, quicbaasService *grpcapi.Server) {
	if s.quicBaaSService == nil || s.quicBaaSService.Certificate() == nil {
		s.logger.Info("Certificate service not available, skipping CertificateService gRPC registration")
		return
	}
	grpcapi.RegisterCertificateServiceServer(grpcServer, quicbaasService)
	s.logger.Info("Registered CertificateService gRPC service")
}

// registerTrustService registers the TrustService handlers on the gRPC
// listener when the QuicBaaS service exposes a Trust sub-service. The
// handlers are implemented on the same grpcapi.Server that backs the
// QuicBaaS facade and delegate to the xkms-composed TrustedCA via
// service.Trust(). Wiring this keeps the gRPC trust surface symmetric with
// the REST /api/v1/trust routes and the QUIC/MCP/Unix/embedded transports.
func (s *Server) registerTrustService(grpcServer *ggrpc.Server, quicbaasService *grpcapi.Server) {
	if s.quicBaaSService == nil || s.quicBaaSService.Trust() == nil {
		s.logger.Info("Trust service not available, skipping TrustService gRPC registration")
		return
	}
	grpcapi.RegisterTrustServiceServer(grpcServer, quicbaasService)
	s.logger.Info("Registered TrustService gRPC service")
}

// registerAttestationService registers the AttestationService handlers on the
// gRPC listener. Unlike the certificate and trust services there is no
// availability gate: the verifier half composes go-xkms pkg/attestation and
// is always present, and the orchestration half reports its own typed
// not-configured error, so registering unconditionally is what keeps the gRPC
// surface symmetric with REST /api/v1/attestation and the
// QUIC/MCP/Unix/embedded transports.
func (s *Server) registerAttestationService(grpcServer *ggrpc.Server, quicbaasService *grpcapi.Server) {
	if s.quicBaaSService == nil {
		s.logger.Info("QuicBaaS service not available, skipping AttestationService gRPC registration")
		return
	}
	grpcapi.RegisterAttestationServiceServer(grpcServer, quicbaasService)
	s.logger.Info("Registered AttestationService gRPC service")
}

// registerRegistryService registers the service registry on the gRPC
// listener (and so on the Unix-domain socket, which shares it). Like
// attestation it is always present: the service layer resolves the platform
// shard per call and reports an unreachable store as a typed error, so
// registering unconditionally keeps the gRPC surface symmetric with REST
// /api/v1/registry and the QUIC/MCP/embedded transports.
func (s *Server) registerRegistryService(grpcServer *ggrpc.Server) {
	if s.quicBaaSService == nil {
		s.logger.Info("QuicBaaS service not available, skipping RegistryService gRPC registration")
		return
	}
	grpcapi.RegisterRegistryServiceServer(grpcServer, grpcapi.NewRegistryServer(s.quicBaaSService.Registry()))
	s.logger.Info("Registered RegistryService gRPC service")
}
