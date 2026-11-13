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
	"errors"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc/workerimage"
)

// =============================================================================
// The worker image refresh
// =============================================================================
//
//   GET  /api/v1/sdlc/worker-image          worker_images:read     (cross-tenant)
//   POST /api/v1/sdlc/worker-image/refresh  worker_images:refresh  (cross-tenant)
//
// HOST VERBS: the refresh keeps every tenant's bound worker image current, so
// no tenant is derived from the request and the permission family is
// cross-tenant — only a system-tenant operator holds it.

// HandleGetSDLCWorkerImage serves GET /api/v1/sdlc/worker-image: the refresh
// loop's state and what its last pass did.
func (s *Server) HandleGetSDLCWorkerImage(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	status, err := svc.WorkerImageStatus(r.Context())
	if err != nil {
		respondWorkerImageError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, status)
}

// HandleRefreshSDLCWorkerImage serves POST /api/v1/sdlc/worker-image/refresh:
// queue a full pass. 202, because the pass runs on the refresh loop after the
// response — a build takes minutes — and GET .../worker-image reports it.
func (s *Server) HandleRefreshSDLCWorkerImage(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	status, err := svc.RefreshWorkerImage(r.Context())
	if err != nil {
		respondWorkerImageError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusAccepted, status)
}

// respondWorkerImageError maps the refresh's refusals: no loop on this node is
// 503, a pass already running or queued is 409.
func respondWorkerImageError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workerimage.ErrNotRunning):
		RespondError(w, r, http.StatusServiceUnavailable, err)
	case errors.Is(err, workerimage.ErrBusy):
		RespondError(w, r, http.StatusConflict, err)
	default:
		RespondError(w, r, http.StatusInternalServerError, err)
	}
}
