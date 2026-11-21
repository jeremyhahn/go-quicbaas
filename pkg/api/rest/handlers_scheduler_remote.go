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
	"errors"
	"io"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Remote handlers (go-quicbaas#1955) over REST: a worker lists the open runs
// of its remote: handlers and reports each run's result.

var errRemoteRunsUnserved = errors.New("this scheduler does not serve remote handlers")

// remoteRunService is the scheduler's remote-run half, or a response written.
func (s *Server) remoteRunService(w http.ResponseWriter, r *http.Request) (service.RemoteRunService, bool) {
	jobs := s.service.Scheduler()
	if jobs == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return nil, false
	}
	remote, ok := jobs.(service.RemoteRunService)
	if !ok {
		RespondError(w, r, http.StatusNotImplemented, errRemoteRunsUnserved)
		return nil, false
	}
	return remote, true
}

// HandleListRemoteRuns handles GET /api/v1/scheduler/remote-runs?handler=...:
// the caller's open runs of the named remote handlers.
func (s *Server) HandleListRemoteRuns(w http.ResponseWriter, r *http.Request) {
	handlers := r.URL.Query()["handler"]
	if len(handlers) == 0 {
		RespondBadRequest(w, r, "at least one handler is required")
		return
	}
	remote, ok := s.remoteRunService(w, r)
	if !ok {
		return
	}
	resp, err := remote.ListRemoteRuns(r.Context(), &service.ListRemoteRunsRequest{
		TenantID: r.URL.Query().Get("tenant_id"),
		Handlers: handlers,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCompleteRemoteRun handles POST /api/v1/scheduler/remote-runs/complete:
// a worker's result for one of the caller's runs.
func (s *Server) HandleCompleteRemoteRun(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.CompleteRemoteRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	if req.Run.JobID == 0 {
		RespondBadRequest(w, r, "run.job_id is required")
		return
	}
	// The run is the caller's: the service scopes it to the caller's tenant.
	req.TenantID = r.URL.Query().Get("tenant_id")
	req.Run.TenantID = ""

	remote, ok := s.remoteRunService(w, r)
	if !ok {
		return
	}
	resp, err := remote.CompleteRemoteRun(r.Context(), &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, resp)
}
