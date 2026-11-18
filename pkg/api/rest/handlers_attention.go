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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// =============================================================================
// The attention inbox — GET .../attention and POST .../attention/{id}/resolve
// =============================================================================
//
//   GET  /api/v1/orchestrator/attention                  orchestrator:task_list
//   POST /api/v1/orchestrator/attention/{taskID}/resolve orchestrator:task_readmit
//
// Both routes are served by the SDLC module's AttentionService — the module
// that already owns the forge credentials a resolution posts a ruling
// through — and answer 503 the same way every other SDLC route does when the
// module is not running on this node.

// HandleListAttention handles GET /api/v1/orchestrator/attention — every
// parked task and environmental alert the tenant carries, paged and faceted
// like every other orchestrator listing.
func (s *Server) HandleListAttention(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	attention := svc.Attention()
	if attention == nil {
		RespondServiceUnavailable(w, r, "the attention inbox is not available on this node")
		return
	}

	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	filter := sdlc.AttentionFilter{
		ProjectID: r.URL.Query().Get("project_id"),
		Kind:      sdlc.AttentionKind(r.URL.Query().Get("kind")),
	}

	result, err := attention.List(ctx, tenantID, filter, page)
	if err != nil {
		respondAttentionError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, result.Envelope("items"))
}

// AttentionResolveRequest is the request body for POST .../attention/{taskID}/resolve.
type AttentionResolveRequest struct {
	// Action is one of approve_recommendation, answer, allow_another_round,
	// close_as_done, readmit, note. Required.
	Action string `json:"action"`

	// Answer carries the operator's free text. Required for "answer" and
	// "note"; optional context for "close_as_done" and optional round scope
	// for "allow_another_round"; ignored otherwise.
	Answer string `json:"answer,omitempty"`
}

// HandleResolveAttention handles POST /api/v1/orchestrator/attention/{taskID}/resolve
// — the one write verb the inbox exposes: it applies an operator's decision
// to a blocked task (or, for a note, to any live task), posting the ruling
// to the forge issue before the task moves (see sdlc.AttentionService.Resolve
// for the exact ordering).
func (s *Server) HandleResolveAttention(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	attention := svc.Attention()
	if attention == nil {
		RespondServiceUnavailable(w, r, "the attention inbox is not available on this node")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondBadRequest(w, r, "task ID is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req AttentionResolveRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	actor := ""
	if ac := AuthorizedContextFromRequest(ctx); ac != nil {
		actor = ac.Subject
	}

	item, err := attention.Resolve(ctx, tenantID, taskID, sdlc.AttentionResolution{
		Action: sdlc.AttentionAction(req.Action), Answer: req.Answer, ActorID: actor,
	})
	if err != nil {
		respondAttentionError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, item)
}

// respondAttentionError maps the attention service's errors onto statuses,
// reusing the SDLC module's own error vocabulary — the inbox is a view over
// the same tasks and the same forge every other SDLC route serves.
func respondAttentionError(w http.ResponseWriter, r *http.Request, err error) {
	respondSDLCError(w, r, err)
}
