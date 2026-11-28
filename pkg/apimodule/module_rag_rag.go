//go:build rag

// Copyright (c) 2025 QuicBaaS Project
// Licensed under AGPL-3.0

package apimodule

import (
	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ragservice"
)

// buildRAGServer builds the node's quicbaas.rag.v1.RAGService server over
// the resolved service, as lifecycle_rag.go's registerRAGService does on the
// Unix-socket server. A nil service mounts nothing.
func buildRAGServer(backends ragservice.Backends, log *logger.Logger) pb.RAGServiceServer {
	if backends == nil {
		return nil
	}
	return grpcapi.NewRAGServer(backends, log)
}
