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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestOrchestratorListSessions_ForwardsTheStatusFilter is the REST half of the
// contract the SDK has been relying on: it has sent `?status=` since it gained
// the field, and the handler read only `backend` — so the filter was accepted
// and ignored. Invisible while the service effectively listed live sessions;
// once the listing merged durable records, a caller asking for ACTIVE sessions
// got CLOSED ones back (go-quicbaas#792).
func TestOrchestratorListSessions_ForwardsTheStatusFilter(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var captured *orchestration.ListSessionsRequest
	agentSvc.listSessionsFunc = func(_ context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
		captured = req
		return &orchestration.ListSessionsResponse{}, nil
	}

	req := systemRequest(http.MethodGet,
		"/api/v1/orchestrator/sessions?backend=claude&status=active", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, captured)
	assert.Equal(t, orchestration.SessionStateActive, captured.Status,
		"the status filter must reach the service; accepting and ignoring it is worse than refusing it")
	assert.Equal(t, "claude", captured.Backend)
}

// TestOrchestratorListSessions_NoStatusMeansEveryState pins the other half:
// omitting the filter must not narrow anything, which is what every caller
// that predates it does.
func TestOrchestratorListSessions_NoStatusMeansEveryState(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var captured *orchestration.ListSessionsRequest
	agentSvc.listSessionsFunc = func(_ context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
		captured = req
		return &orchestration.ListSessionsResponse{}, nil
	}

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/sessions", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, captured)
	assert.Equal(t, orchestration.SessionState(""), captured.Status)
}

// TestOrchestratorListSessions_UnknownStatusIsABadRequest holds the REST
// classifier to the same answer gRPC gives.
//
// The service refuses an unknown state with ErrSessionStateUnknown wrapped in
// an AgentServiceError, which matched none of the classifiers and fell to the
// 500 default. A misspelled `?status=` is the caller's mistake: answering 500
// tells an operator to go looking at the daemon for a fault that is in their
// own query string, and tells a client to retry a request that can never
// succeed.
func TestOrchestratorListSessions_UnknownStatusIsABadRequest(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.listSessionsFunc = func(_ context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
		return nil, &orchestration.AgentServiceError{
			Sentinel: orchestration.ErrSessionStateUnknown,
			Message:  string(req.Status) + "; known states are active, closed, error, idle, resumable",
		}
	}

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/sessions?status=actve", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"a state the daemon does not use is a bad request, not a server fault")
	assert.Contains(t, rec.Body.String(), "known states are",
		"the refusal must name the states that do exist, or the caller cannot fix it")
}

// TestOrchestratorListSessions_ServiceFailureIsStillAServerError is the other
// side of that classification: teaching the classifier one new sentinel must
// not turn every AgentServiceError into a 400.
func TestOrchestratorListSessions_ServiceFailureIsStillAServerError(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.listSessionsFunc = func(context.Context, *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
		return nil, &orchestration.AgentServiceError{
			Sentinel: orchestration.ErrTurnNotAccepted,
			Message:  "the session store fell over",
		}
	}

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/sessions", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
