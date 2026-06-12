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
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// agentWithLog adds a SessionEventLog to the mock agent service so the
// HandleOrchestratorSessionEvents path resolves the sessionLogAccessor.
type agentWithLog struct {
	*mockAgentService
	log orchestration.SessionEventLog
}

func (a *agentWithLog) SessionEventLog() orchestration.SessionEventLog { return a.log }

func setupSessionEventsServer(t *testing.T, withLog bool) (*Server, orchestration.SessionEventLog, func()) {
	t.Helper()
	server, _, cleanup := setupMockServer(t)

	var agent orchestration.AgentService
	var log orchestration.SessionEventLog
	if withLog {
		log = orchestration.NewMemorySessionEventLog()
		agent = &agentWithLog{mockAgentService: &mockAgentService{}, log: log}
	} else {
		agent = &mockAgentService{}
	}
	server.service = &MockQuicBaaSWithAgent{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentService: agent,
	}
	return server, log, cleanup
}

func sessionEventsReq(method, target, sessionID string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", sessionID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestHandleOrchestratorSessionEvents_MissingSessionID(t *testing.T) {
	server, _, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, sessionEventsReq(http.MethodGet, "/x", ""))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleOrchestratorSessionEvents_InvalidFrom(t *testing.T) {
	server, _, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, sessionEventsReq(http.MethodGet, "/x?from=notanumber", "s1"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleOrchestratorSessionEvents_LogNotAvailable(t *testing.T) {
	// agent service does not implement sessionLogAccessor -> 503.
	server, _, cleanup := setupSessionEventsServer(t, false)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, sessionEventsReq(http.MethodGet, "/x", "s1"))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleOrchestratorSessionEvents_JSONHistory(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	// Seed a couple of events.
	for i := uint64(1); i <= 2; i++ {
		require.NoError(t, log.Append(context.Background(), &orchestration.SessionEvent{
			Sequence: i, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
			Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted, Source: "test",
			Prompt: &orchestration.PromptSubmittedPayload{Prompt: "hi"},
		}))
	}

	req := sessionEventsReq(http.MethodGet, "/x", "s1")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "events")
}

// Last-Event-ID header resumes from the next sequence (JSON history path).
func TestHandleOrchestratorSessionEvents_LastEventIDResume(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	require.NoError(t, log.Append(context.Background(), &orchestration.SessionEvent{
		Sequence: 1, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted, Source: "test",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "hi"},
	}))

	req := sessionEventsReq(http.MethodGet, "/x", "s1")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Last-Event-ID", "1")
	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

// TestHandleOrchestratorSessionEvents_NoTenantDefaultsToSystem is the
// regression guard for the tenant-fallback migration: a request with no
// tenant_id query parameter must resolve to qrdbsdk.SystemTenantID (NOT the
// retired "default" tenant). Events seeded under the system tenant are
// returned for an unscoped request; events seeded under "default" are not.
func TestHandleOrchestratorSessionEvents_NoTenantDefaultsToSystem(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	// Seed one event under the system tenant and one under the retired
	// "default" tenant, both for the same session.
	require.NoError(t, log.Append(context.Background(), &orchestration.SessionEvent{
		Sequence: 1, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted, Source: "test",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "system-event"},
	}))
	require.NoError(t, log.Append(context.Background(), &orchestration.SessionEvent{
		Sequence: 1, TenantID: "default", SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted, Source: "test",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "legacy-event"},
	}))

	// No tenant_id query param → fallback resolves to the system tenant.
	req := sessionEventsReq(http.MethodGet, "/x", "s1")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	body := w.Body.String()
	assert.Contains(t, body, "system-event", "unscoped request must read the system tenant")
	assert.NotContains(t, body, "legacy-event", "unscoped request must NOT read the retired default tenant")
}

// SSE replay path: with a seeded history and follow=false the handler streams
// the events as SSE frames then closes.
func TestHandleOrchestratorSessionEvents_SSEReplay(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	require.NoError(t, log.Append(context.Background(), &orchestration.SessionEvent{
		Sequence: 1, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted, Source: "test",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "hi"},
	}))

	// No Accept: application/json -> SSE path. follow=false -> replay then close.
	req := sessionEventsReq(http.MethodGet, "/x?follow=0", "s1")
	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "id: 1")
}

// SSE follow path: subscribe, replay history, then exit when the request
// context is cancelled (no live events delivered).
func TestHandleOrchestratorSessionEvents_SSEFollow(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	require.NoError(t, log.Append(context.Background(), &orchestration.SessionEvent{
		Sequence: 1, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted, Source: "test",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "hi"},
	}))

	// Build the request with chi params, then attach a pre-cancelled context so
	// the live-tail select exits immediately after the history replay.
	req := sessionEventsReq(http.MethodGet, "/x?follow=1", "s1")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
}
