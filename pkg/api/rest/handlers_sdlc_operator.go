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
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Operator verbs on a bound project's forge
// =============================================================================
//
//   POST /api/v1/sdlc/projects/{projectID}/issues          sdlc:issue_create
//   GET  /api/v1/sdlc/projects/{projectID}/collaborators   sdlc:read

// SDLCFileIssueRequest is the body of POST /api/v1/sdlc/projects/{projectID}/issues.
type SDLCFileIssueRequest struct {
	Title  string   `json:"title"`
	Body   string   `json:"body,omitempty"`
	Labels []string `json:"labels,omitempty"`
	Parent int      `json:"parent,omitempty"`
}

// SDLCCollaboratorsResponse lists the accounts that may rule on a bound
// repository: its owner first, then every collaborator.
type SDLCCollaboratorsResponse struct {
	Users []sdlc.ForgeUser `json:"users"`
	Count int              `json:"count"`
}

// sdlcProjectTarget resolves the module, the tenant and the project an
// operator route acts on, or answers the refusal.
func (s *Server) sdlcProjectTarget(w http.ResponseWriter, r *http.Request) (*service.SDLCService, string, string, bool) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return nil, "", "", false
	}
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		RespondBadRequest(w, r, "project ID is required")
		return nil, "", "", false
	}
	tenantID, ok := sdlcRequestTenant(w, r)
	if !ok {
		return nil, "", "", false
	}
	return svc, tenantID, projectID, true
}

// HandleFileSDLCIssue serves POST /api/v1/sdlc/projects/{projectID}/issues:
// an operator filing an issue on the project's bound repository. It answers
// 201 with the issue as the forge stored it.
func (s *Server) HandleFileSDLCIssue(w http.ResponseWriter, r *http.Request) {
	svc, tenantID, projectID, ok := s.sdlcProjectTarget(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()
	var req SDLCFileIssueRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	issue, err := svc.FileIssue(r.Context(), tenantID, projectID, sdlc.IssueFiling{
		Title: req.Title, Body: req.Body, Labels: req.Labels, Parent: req.Parent,
	})
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusCreated, issue)
}

// HandleListSDLCCollaborators serves
// GET /api/v1/sdlc/projects/{projectID}/collaborators: the accounts a ruler
// may be picked from. It changes nothing.
func (s *Server) HandleListSDLCCollaborators(w http.ResponseWriter, r *http.Request) {
	svc, tenantID, projectID, ok := s.sdlcProjectTarget(w, r)
	if !ok {
		return
	}
	users, err := svc.Collaborators(r.Context(), tenantID, projectID)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, SDLCCollaboratorsResponse{Users: users, Count: len(users)})
}
