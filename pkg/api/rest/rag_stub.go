//go:build rest && !rag

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

package rest

import (
	"github.com/go-chi/chi/v5"
)

// registerRAGRoutes is a no-op when RAG is disabled.
func registerRAGRoutes(s *Server, r chi.Router) {
	// RAG is not enabled - routes not registered
	s.logger.Debug("RAG support not enabled - skipping RAG route registration")
}
