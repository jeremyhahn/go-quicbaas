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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// doOrchReq dispatches a request through the full router so chi URL params
// and the orchestrator route tree are exercised exactly as in production.
func doOrchReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = systemRequest(method, path, nil)
	} else {
		r = systemRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)
	return w
}

// flushRecorder wraps httptest.ResponseRecorder and additionally satisfies
// http.Flusher so SSE handlers take the streaming-supported path instead of
// the 500 "streaming not supported" fallback.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

// =============================================================================
// Service-unavailable path: when the server's QuicBaaS does not provide an
// Agent() accessor, every orchestrator handler must return 503.
// =============================================================================

func TestOrchestratorHandlers_ServiceUnavailable(t *testing.T) {
	// setupMockServer wires a plain MockQuicBaaS that does NOT implement
	// orchestratorServiceProvider, so getAgentService short-circuits to 503.
	s, _, cleanup := setupMockServer(t)
	defer cleanup()

	cases := []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/v1/orchestrator/jobs", `{"description":"x"}`},
		{http.MethodGet, "/api/v1/orchestrator/jobs", ""},
		{http.MethodGet, "/api/v1/orchestrator/jobs/j1", ""},
		{http.MethodDelete, "/api/v1/orchestrator/jobs/j1", ""},
		{http.MethodPost, "/api/v1/orchestrator/jobs/j1/children", `{}`},
		{http.MethodPost, "/api/v1/fleet", `{"name":"a","host":"h","node_type":"t"}`},
		{http.MethodGet, "/api/v1/fleet", ""},
		{http.MethodGet, "/api/v1/fleet/a1", ""},
		{http.MethodDelete, "/api/v1/fleet/a1", ""},
		{http.MethodPost, "/api/v1/fleet/a1/heartbeat", `{}`},
		{http.MethodPost, "/api/v1/orchestrator/tasks/t1/artifacts", `[]`},
		{http.MethodGet, "/api/v1/orchestrator/tasks/t1/artifacts", ""},
		{http.MethodGet, "/api/v1/orchestrator/tasks/t1/artifacts/n1", ""},
		{http.MethodPost, "/api/v1/orchestrator/tasks/decompose", `{"task":"x"}`},
		{http.MethodGet, "/api/v1/orchestrator/jobs/j1/events", ""},
	}
	for _, c := range cases {
		w := doOrchReq(t, s, c.method, c.path, c.body)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: expected 503, got %d", c.method, c.path, w.Code)
		}
	}
}

// =============================================================================
// Job management
// =============================================================================

func TestHandleOrchestratorStartJob_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/jobs", `{"description":"build the thing"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestHandleOrchestratorStartJob_MissingDescription(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/jobs", `{"description":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleOrchestratorStartJob_BadJSON(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/jobs", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleOrchestratorListJobs_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleOrchestratorGetJobStatus_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodGet, "/api/v1/orchestrator/jobs/job-1?tenant_id=acme", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleOrchestratorCancelJob_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodDelete, "/api/v1/orchestrator/jobs/job-1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleOrchestratorSpawnChildJob_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/jobs/parent-1/children", `{"description":"child"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestHandleOrchestratorSpawnChildJob_BadJSON(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/jobs/parent-1/children", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestHandleOrchestratorSubscribeJobEvents_StreamingUnsupported drives the
// non-flushable branch: httptest.ResponseRecorder is not an http.Flusher, so
// the handler returns 500 "streaming not supported".
func TestHandleOrchestratorSubscribeJobEvents_StreamingUnsupported(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodGet, "/api/v1/orchestrator/jobs/job-1/events", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// TestHandleOrchestratorSubscribeJobEvents_ClosedStream drives the streaming
// path with a flushable writer. The mock returns an already-closed channel,
// so the handler emits the SSE "done" event and returns.
func TestHandleOrchestratorSubscribeJobEvents_ClosedStream(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	// The custom REST responseWriter middleware does not propagate Flush,
	// so the streaming-supported path is only reachable by calling the
	// handler with a flushable writer directly. Inject the chi route
	// context so chi.URLParam resolves jobID.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", "job-1")
	r := systemRequest(http.MethodGet, "/api/v1/orchestrator/jobs/job-1/events", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := &flushRecorder{httptest.NewRecorder()}
	s.HandleOrchestratorSubscribeJobEvents(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "event: done") {
		t.Fatalf("expected SSE done event, got %q", w.Body.String())
	}
}

// =============================================================================
// Fleet management
// =============================================================================

func TestHandleOrchestratorRegisterAgent_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	body := `{"name":"agent-a","host":"node-1","node_type":"worker","backend":"ollama"}`
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/fleet", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestHandleOrchestratorRegisterAgent_Validation(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	cases := []struct {
		name, body string
	}{
		{"bad json", `{bad`},
		{"missing name", `{"host":"h","node_type":"t"}`},
		{"missing host", `{"name":"a","node_type":"t"}`},
		{"missing type", `{"name":"a","host":"h"}`},
	}
	for _, c := range cases {
		w := doOrchReq(t, s, http.MethodPost, "/api/v1/fleet", c.body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", c.name, w.Code)
		}
	}
}

func TestHandleOrchestratorListAgents_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodGet, "/api/v1/fleet?status=idle&host=node-1&backend=ollama", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleOrchestratorGetAgent_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodGet, "/api/v1/fleet/agent-1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleOrchestratorRemoveAgent_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodDelete, "/api/v1/fleet/agent-1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleOrchestratorAgentHeartbeat_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/fleet/agent-1/heartbeat", `{"bind_addr":"1.2.3.4:9000"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// =============================================================================
// Task artifacts
// =============================================================================

func TestHandleOrchestratorSubmitArtifacts_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/tasks/task-1/artifacts", `[{"name":"a","content_type":"text/plain"}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleOrchestratorSubmitArtifacts_BadJSON(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/tasks/task-1/artifacts", `{not-an-array}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleOrchestratorListArtifacts_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodGet, "/api/v1/orchestrator/tasks/task-1/artifacts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleOrchestratorGetArtifact_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	// The mock returns a nil artifact + nil error; the handler still
	// renders a JSON response. We assert it does not 4xx/5xx.
	w := doOrchReq(t, s, http.MethodGet, "/api/v1/orchestrator/tasks/task-1/artifacts/report.txt", "")
	if w.Code >= 400 {
		t.Fatalf("expected < 400, got %d", w.Code)
	}
}

// =============================================================================
// Decompose task
// =============================================================================

func TestHandleOrchestratorDecomposeTask_OK(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/tasks/decompose", `{"task":"do the work"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleOrchestratorDecomposeTask_Validation(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	for _, body := range []string{`{bad`, `{"task":""}`} {
		w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/tasks/decompose", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d", body, w.Code)
		}
	}
}

// =============================================================================
// Hook events
// =============================================================================

func TestHandleOrchestratorHookEvent_TaskCreated(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/hooks",
		`{"event_type":"task.created","tenant_id":"acme","task":{"subject":"do it"}}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
}

func TestHandleOrchestratorHookEvent_TaskCompleted(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/hooks",
		`{"event_type":"task.completed","tenant_id":"acme","task_id":"t1"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
}

func TestHandleOrchestratorHookEvent_TaskCompletedMissingID(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/hooks",
		`{"event_type":"task.completed","tenant_id":"acme"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleOrchestratorHookEvent_TeammateIdle(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/hooks",
		`{"event_type":"teammate.idle","tenant_id":"acme","agent_id":"a1"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
}

func TestHandleOrchestratorHookEvent_Validation(t *testing.T) {
	s, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	cases := []struct {
		name, body string
	}{
		{"empty body", ``},
		{"bad json", `{bad`},
		{"missing event_type", `{"tenant_id":"acme"}`},
		{"unknown event_type", `{"event_type":"nope"}`},
	}
	for _, c := range cases {
		w := doOrchReq(t, s, http.MethodPost, "/api/v1/orchestrator/hooks", c.body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", c.name, w.Code)
		}
	}
}

// =============================================================================
// Hook-event error-type behavior
// =============================================================================

func TestOrchestratorHookEventTypeError(t *testing.T) {
	if got := (&OrchestratorHookEventTypeError{}).Error(); !strings.Contains(got, "event_type is required") {
		t.Fatalf("empty event type message: %q", got)
	}
	if got := (&OrchestratorHookEventTypeError{EventType: "foo"}).Error(); !strings.Contains(got, "foo") {
		t.Fatalf("unknown event type message should include value: %q", got)
	}
}
