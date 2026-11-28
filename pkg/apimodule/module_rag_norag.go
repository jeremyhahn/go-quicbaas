//go:build !rag

// Copyright (c) 2025 QuicBaaS Project
// Licensed under AGPL-3.0

package apimodule

import (
	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// buildRAGServer is nil without -tags rag: grpcapi.NewRAGServer and
// ragservice.Backends exist only under the tag, matching
// lifecycle_rag_noop.go's registerRAGService.
func buildRAGServer(_ any, _ *logger.Logger) pb.RAGServiceServer {
	return nil
}
