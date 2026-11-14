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

package server

import (
	ggrpc "google.golang.org/grpc"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
)

// registerFIDO2Service registers the FIDO2Service (WebAuthn ceremony) gRPC
// handlers. There is no availability gate: the FIDO2 composition service is
// always constructed and reports its own typed not-configured error until
// the xkms-backed WebAuthn manager is wired. Registering unconditionally
// keeps the gRPC ceremony surface symmetric with the REST
// /api/v1/iam/{tenant}/webauthn routes and the QUIC/MCP/Unix/embedded
// transports.
func (s *Server) registerFIDO2Service(grpcServer *ggrpc.Server, quicbaasService *grpcapi.Server) {
	if s.quicBaaSService == nil {
		s.logger.Info("QuicBaaS service not available, skipping FIDO2Service gRPC registration")
		return
	}
	grpcapi.RegisterFIDO2ServiceServer(grpcServer, quicbaasService)
	s.logger.Info("Registered FIDO2Service gRPC service")
}

// registerPIVService registers the PIVService gRPC handlers. There is no
// availability gate: the PIV composition service is always constructed and
// delegates to the composed go-xkms service, whose typed errors report the
// not-initialized case. Registering unconditionally keeps the gRPC PIV
// surface symmetric with the REST /api/v1/xkms/piv routes and the
// QUIC/MCP/Unix/embedded transports.
func (s *Server) registerPIVService(grpcServer *ggrpc.Server, quicbaasService *grpcapi.Server) {
	if s.quicBaaSService == nil {
		s.logger.Info("QuicBaaS service not available, skipping PIVService gRPC registration")
		return
	}
	grpcapi.RegisterPIVServiceServer(grpcServer, quicbaasService)
	s.logger.Info("Registered PIVService gRPC service")
}

// registerTrustedCAService registers the TrustedCAService gRPC handlers, the
// gRPC and Unix side of the re-exported go-xkms CAService (#1330). There is
// no availability gate: each verb answers Unimplemented until the server
// wires the go-xkms CA (wireTrustedCA).
func (s *Server) registerTrustedCAService(grpcServer *ggrpc.Server, quicbaasService *grpcapi.Server) {
	if s.quicBaaSService == nil {
		s.logger.Info("QuicBaaS service not available, skipping TrustedCAService gRPC registration")
		return
	}
	grpcapi.RegisterTrustedCAServiceServer(grpcServer, quicbaasService)
	s.logger.Info("Registered TrustedCAService gRPC service")
}
