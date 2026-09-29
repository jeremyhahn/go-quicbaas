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
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// REST Request/Response Types
// =============================================================================

// DisableFleetNodeRESTResponse is returned after a successful fleet node disable.
type DisableFleetNodeRESTResponse struct {
	NodeID     string    `json:"node_id"`
	Disabled   bool      `json:"disabled"`
	DisabledAt time.Time `json:"disabled_at"`
	Timestamp  time.Time `json:"timestamp"`
}

// EnableFleetNodeRESTResponse is returned after a successful fleet node enable.
type EnableFleetNodeRESTResponse struct {
	NodeID    string    `json:"node_id"`
	Enabled   bool      `json:"enabled"`
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Handlers
// =============================================================================

// HandleDisableFleetNode handles POST /api/v1/fleet/{nodeID}/disable.
// Administratively disables the fleet node globally, preventing all work claims
// and authentication until re-enabled.
func (s *Server) HandleDisableFleetNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodeID := chi.URLParam(r, "nodeID")
	if nodeID == "" {
		RespondBadRequest(w, r, "node id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	adminSvc := s.service.FleetAdmin()
	if adminSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("fleet admin service unavailable"))
		return
	}

	resp, err := adminSvc.DisableFleetNode(ctx, &service.DisableFleetNodeRequest{
		TenantID: tenantID,
		NodeID:   nodeID,
	})
	if err != nil {
		handleFleetAdminError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &DisableFleetNodeRESTResponse{
		NodeID:     resp.NodeID,
		Disabled:   resp.Disabled,
		DisabledAt: resp.DisabledAt,
		Timestamp:  resp.Timestamp,
	})
}

// HandleEnableFleetNode handles POST /api/v1/fleet/{nodeID}/enable.
// Re-activates a previously disabled fleet node, allowing it to authenticate
// and claim work again.
func (s *Server) HandleEnableFleetNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodeID := chi.URLParam(r, "nodeID")
	if nodeID == "" {
		RespondBadRequest(w, r, "node id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	adminSvc := s.service.FleetAdmin()
	if adminSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("fleet admin service unavailable"))
		return
	}

	resp, err := adminSvc.EnableFleetNode(ctx, &service.EnableFleetNodeRequest{
		TenantID: tenantID,
		NodeID:   nodeID,
	})
	if err != nil {
		handleFleetAdminError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &EnableFleetNodeRESTResponse{
		NodeID:    resp.NodeID,
		Enabled:   resp.Enabled,
		Timestamp: resp.Timestamp,
	})
}

// =============================================================================
// Error Handling
// =============================================================================

// handleFleetAdminError maps service-layer fleet admin errors to HTTP status codes.
func handleFleetAdminError(w http.ResponseWriter, r *http.Request, err error) {
	var notFound *service.FleetAdminNotFoundError
	var alreadyDisabled *service.FleetNodeAlreadyDisabledError
	var notDisabled *service.FleetNodeNotDisabledError
	var invalid *service.ProjectInvalidError // reuses the project invalid error type

	switch {
	case errors.As(err, &notFound):
		RespondError(w, r, http.StatusNotFound, err)
	case errors.As(err, &alreadyDisabled):
		RespondError(w, r, http.StatusConflict, err)
	case errors.As(err, &notDisabled):
		RespondError(w, r, http.StatusConflict, err)
	case errors.As(err, &invalid):
		RespondError(w, r, http.StatusBadRequest, err)
	default:
		RespondError(w, r, http.StatusInternalServerError, err)
	}
}
