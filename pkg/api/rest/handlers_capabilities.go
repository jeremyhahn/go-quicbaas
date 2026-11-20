//go:build rest

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
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/capabilities"
)

// HandleCapabilities handles GET /api/v1/server/capabilities: the report of which
// subsystems this server serves and has configured. Any authenticated caller
// may read it (RouteAuthSession); it carries no configured value.
func (s *Server) HandleCapabilities(w http.ResponseWriter, r *http.Request) {
	RespondJSON(w, r, http.StatusOK, s.Capabilities())
}

// Capabilities builds this server's capabilities report, probing the CA
// handlers the REST server holds and the time series and RAG handlers its
// build tags compile.
func (s *Server) Capabilities() capabilities.Report {
	return capabilities.Build(s.service, capabilities.Probes{
		CertificateAuthority: func() bool { return s.caHandlers() != nil },
		TimeSeriesServed:     capabilities.TimeSeriesServed,
		RAGServed:            capabilities.RAGServed,
	})
}
