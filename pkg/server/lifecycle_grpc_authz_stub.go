//go:build !iam

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
)

// grpcAuthorizationInterceptors is a no-op when the iam build tag is absent.
// Without IAM there is no OIDC provider to validate tokens and no qrdb
// authorizer adapter, so the gRPC chain adds no authorization gate — the same
// behaviour as before authz wiring existed.
func (s *Server) grpcAuthorizationInterceptors() (ggrpc.UnaryServerInterceptor, ggrpc.StreamServerInterceptor) {
	return nil, nil
}
