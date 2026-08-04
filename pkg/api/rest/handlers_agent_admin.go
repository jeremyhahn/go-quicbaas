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

// DisableAgentRESTResponse is returned after a successful agent disable.
type DisableAgentRESTResponse struct {
	AgentID    string    `json:"agent_id"`
	Disabled   bool      `json:"disabled"`
	DisabledAt time.Time `json:"disabled_at"`
	Timestamp  time.Time `json:"timestamp"`
}

// EnableAgentRESTResponse is returned after a successful agent enable.
type EnableAgentRESTResponse struct {
	AgentID   string    `json:"agent_id"`
	Enabled   bool      `json:"enabled"`
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Handlers
// =============================================================================

// HandleDisableAgent handles POST /api/v1/orchestrator/agents/{agentID}/disable.
// Administratively disables the agent globally, preventing all work claims
// and authentication until re-enabled.
func (s *Server) HandleDisableAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		RespondBadRequest(w, r, "agent id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	adminSvc := s.service.AgentAdmin()
	if adminSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("agent admin service unavailable"))
		return
	}

	resp, err := adminSvc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	if err != nil {
		handleAgentAdminError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &DisableAgentRESTResponse{
		AgentID:    resp.AgentID,
		Disabled:   resp.Disabled,
		DisabledAt: resp.DisabledAt,
		Timestamp:  resp.Timestamp,
	})
}

// HandleEnableAgent handles POST /api/v1/orchestrator/agents/{agentID}/enable.
// Re-activates a previously disabled agent, allowing it to authenticate
// and claim work again.
func (s *Server) HandleEnableAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		RespondBadRequest(w, r, "agent id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	adminSvc := s.service.AgentAdmin()
	if adminSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("agent admin service unavailable"))
		return
	}

	resp, err := adminSvc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	if err != nil {
		handleAgentAdminError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &EnableAgentRESTResponse{
		AgentID:   resp.AgentID,
		Enabled:   resp.Enabled,
		Timestamp: resp.Timestamp,
	})
}

// =============================================================================
// Error Handling
// =============================================================================

// handleAgentAdminError maps service-layer agent admin errors to HTTP status codes.
func handleAgentAdminError(w http.ResponseWriter, r *http.Request, err error) {
	var notFound *service.AgentAdminNotFoundError
	var alreadyDisabled *service.AgentAlreadyDisabledError
	var notDisabled *service.AgentNotDisabledError
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
