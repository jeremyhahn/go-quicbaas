//go:build !rag

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

package apimodule

import (
	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// buildOllamaServer is a no-op when RAG support is not compiled:
// grpcapi.NewOllamaServer's concrete type requires the rag build tag, so the
// module's OllamaService registration stays lazily-bound to a permanently
// nil implementation — every RPC answers Unavailable regardless of
// configuration, exactly matching pkg/server/lifecycle_rag_noop.go's
// registerOllamaService no-op on the legacy gRPC transport.
func buildOllamaServer(_ service.OllamaService, _ *logger.Logger) pb.OllamaServiceServer {
	return nil
}
