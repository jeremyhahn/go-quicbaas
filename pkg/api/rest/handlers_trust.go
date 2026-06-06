//go:build rest

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

package rest

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Trust Service Route Registration
// =============================================================================

// registerTrustRoutes registers the Trust composition routes
// (cross-sign / AIA) under /api/v1/trust. These delegate to the
// service-layer TrustService, which composes the xkms TrustedCA. DANE
// publication is intentionally NOT registered here (it is pending a
// separate DNS-provider hook design).
func registerTrustRoutes(s *Server, r chi.Router) {
	r.Route("/trust", func(r chi.Router) {
		r.Get("/cross-signed-roots", s.HandleTrustCrossSignedRootPool)
		r.Post("/auto-import-aia", s.HandleTrustAutoImportAIA)
	})
}

// =============================================================================
// Request/Response Types
// =============================================================================

// TrustCrossSignedRootPoolRESTResponse is the JSON body for
// GET /api/v1/trust/cross-signed-roots. Each element is a PEM-encoded
// certificate.
type TrustCrossSignedRootPoolRESTResponse struct {
	RootPEMs [][]byte `json:"root_pems"`
}

// TrustAutoImportAIARESTRequest is the JSON body for
// POST /api/v1/trust/auto-import-aia.
type TrustAutoImportAIARESTRequest struct {
	ChainDER [][]byte `json:"chain_der"`
}

// TrustAutoImportAIARESTResponse is the JSON body returned by
// POST /api/v1/trust/auto-import-aia.
type TrustAutoImportAIARESTResponse struct {
	Imported int `json:"imported"`
}

// =============================================================================
// Handler Functions
// =============================================================================

// HandleTrustCrossSignedRootPool handles GET /api/v1/trust/cross-signed-roots —
// returns the active CA's cross-sign-aware root set in PEM form.
func (s *Server) HandleTrustCrossSignedRootPool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	trustSvc := s.service.Trust()
	if trustSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrTrustServiceUnavailable)
		return
	}

	svcResp, err := trustSvc.CrossSignedRootPool(ctx, &service.CrossSignedRootPoolRequest{})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &TrustCrossSignedRootPoolRESTResponse{
		RootPEMs: svcResp.RootPEMs,
	})
}

// HandleTrustAutoImportAIA handles POST /api/v1/trust/auto-import-aia —
// walks the AIA chain of the supplied DER-encoded certificates and imports
// the referenced issuing CAs.
func (s *Server) HandleTrustAutoImportAIA(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req TrustAutoImportAIARESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	if len(req.ChainDER) == 0 {
		RespondBadRequest(w, r, "chain_der is required")
		return
	}

	trustSvc := s.service.Trust()
	if trustSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrTrustServiceUnavailable)
		return
	}

	svcResp, err := trustSvc.AutoImportAIA(ctx, &service.AutoImportAIARequest{
		ChainDER: req.ChainDER,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &TrustAutoImportAIARESTResponse{
		Imported: svcResp.Imported,
	})
}
