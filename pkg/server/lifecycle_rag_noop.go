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

//go:build !rag

package server

import (
	"errors"

	ggrpc "google.golang.org/grpc"
)

// ErrRAGNotCompiled is returned when RAG operations are attempted
// but the binary was not compiled with the rag build tag.
var ErrRAGNotCompiled = errors.New("rag support not compiled - build with -tags rag")

// startRAG is a no-op when RAG support is not compiled.
func (s *Server) startRAG() error {
	s.logger.Debug("RAG not compiled, skipping initialization")
	return nil
}

// stopRAG is a no-op when RAG support is not compiled.
func (s *Server) stopRAG() error {
	return nil
}

// startOllama is a no-op when RAG support is not compiled.
func (s *Server) startOllama() error {
	s.logger.Debug("Ollama not compiled (requires rag build tag), skipping initialization")
	return nil
}

// stopOllama is a no-op when RAG support is not compiled.
func (s *Server) stopOllama() error {
	return nil
}

// registerOllamaService is a no-op when RAG support is not compiled.
func (s *Server) registerOllamaService(grpcServer *ggrpc.Server) {
	// Ollama requires RAG build tag
}
