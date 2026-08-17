//go:build rag

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
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// buildOllamaServer constructs the Ollama gRPC handler around ollamaSvc,
// exactly as pkg/server/lifecycle_rag.go's registerOllamaService constructs
// it for the legacy gRPC transport (grpcapi.NewOllamaServer, whose concrete
// type only exists under this rag build tag). nil-gated on ollamaSvc so an
// unconfigured Ollama integration (Config.Ollama.Enabled=false, or
// startOllama failing to reach the Ollama daemon) answers Unavailable on the
// module surface exactly like any other disabled broker capability.
func buildOllamaServer(ollamaSvc service.OllamaService, log *logger.Logger) pb.OllamaServiceServer {
	if ollamaSvc == nil {
		return nil
	}
	return grpcapi.NewOllamaServer(ollamaSvc, log)
}
