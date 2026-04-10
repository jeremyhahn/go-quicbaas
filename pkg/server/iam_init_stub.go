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

//go:build !iam

package server

import (
	ggrpc "google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// initializeIAMComponents returns nil when IAM is not enabled.
func (s *Server) initializeIAMComponents() (service.IAMComponents, error) {
	return nil, nil
}

// registerIAMService is a no-op when IAM is not enabled.
func (s *Server) registerIAMService(grpcServer *ggrpc.Server) {
	// IAM not enabled - nothing to register
}
