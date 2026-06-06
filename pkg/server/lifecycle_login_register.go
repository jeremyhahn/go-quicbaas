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

// registerLoginService registers the unified Login ceremony gRPC service. The
// handler extracts the verified mTLS peer certificate from the gRPC TLS
// handshake and delegates to the QuicBaaS LoginCeremony() facade, which
// forwards to the embedded go-qrdb node's in-process LoginClient.
//
// The service is registered with whatever LoginCeremony() returns — when login
// is unavailable (no qrdb node / IAM absent) the handler short-circuits with
// codes.Unavailable, keeping the gRPC surface symmetric with the other
// transports.
func (s *Server) registerLoginService(grpcServer *ggrpc.Server, _ *grpcapi.Server) {
	if s.quicBaaSService == nil {
		s.logger.Info("QuicBaaS service not available, skipping Login gRPC registration")
		return
	}
	grpcapi.RegisterLoginServer(grpcServer, s.quicBaaSService.LoginCeremony(), s.logger)
	s.logger.Info("Registered unified Login ceremony gRPC service")
}
