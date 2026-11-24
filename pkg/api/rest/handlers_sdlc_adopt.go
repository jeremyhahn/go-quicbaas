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
// Adopting an issue
// =============================================================================
//
//   POST /api/v1/sdlc/projects/{projectID}/adopt   orchestrator:task_create
//
// An operator verb: it creates the task for one forge issue — resuming a named
// pull request on that pull request's own branch — and hands the issue back
// to the factory. The permission is task creation's, because that is what it
// does; see sdlc.IssueAdopter.

// SDLCAdoptIssueRequest is the body of an adoption.
type SDLCAdoptIssueRequest struct {
	// Issue is the issue number on the project's repository.
	Issue int `json:"issue"`

	// PullRequest, when set, is the open pull request the task resumes.
	PullRequest int `json:"pull_request,omitempty"`

	// Ruling, when set, is appended to the task's contract and posted on the
	// issue.
	Ruling string `json:"ruling,omitempty"`
}

// HandleAdoptSDLCIssue serves POST /api/v1/sdlc/projects/{projectID}/adopt.
// It answers 201 with the adoption: the task created, the pull request and
// branch it resumes, and the finished task it replaced.
func (s *Server) HandleAdoptSDLCIssue(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	tenantID, ok := sdlcRequestTenant(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()
	var req SDLCAdoptIssueRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	adoption, err := svc.AdoptIssue(r.Context(), tenantID, sdlc.AdoptIssueRequest{
		ProjectID: chi.URLParam(r, "projectID"), Issue: req.Issue, PullRequest: req.PullRequest, Ruling: req.Ruling,
	})
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusCreated, adoption)
}
