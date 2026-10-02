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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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
	r := systemRequest(method, target, nil)
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
		require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
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

// TestHandleOrchestratorSessionEvents_EnvelopeShape is the regression guard for
// the session-events wire contract. The JSON history path MUST emit each event
// as a SessionEventEnvelope — top-level session_id/sequence/timestamp plus the
// full SessionEvent carried verbatim under payload_json (kind discriminator +
// nested per-kind payload). The SPA store parses exactly this shape; emitting a
// flat SessionEvent here is what left the UI showing "0 events loaded".
func TestHandleOrchestratorSessionEvents_EnvelopeShape(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
		Sequence: 1, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted,
		Source: "operator:alice",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "hello there"},
	}))
	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
		Sequence: 2, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindAssistantChunk,
		Source:         "agent:claude",
		AssistantChunk: &orchestration.AssistantChunkPayload{Text: "general kenobi", StopReason: "end_turn"},
	}))

	req := sessionEventsReq(http.MethodGet, "/x", "s1")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var body struct {
		Events []struct {
			SessionID   string                     `json:"session_id"`
			Sequence    uint64                     `json:"sequence"`
			Timestamp   time.Time                  `json:"timestamp"`
			FromReplay  bool                       `json:"from_replay"`
			PayloadJSON orchestration.SessionEvent `json:"payload_json"`
		} `json:"events"`
		NextSeq   uint64 `json:"next_seq"`
		SessionID string `json:"session_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Events, 2)

	// Envelope #1: prompt. Top-level fields lifted; payload_json carries the
	// full nested event the SPA parses.
	e0 := body.Events[0]
	assert.Equal(t, "s1", e0.SessionID)
	assert.Equal(t, uint64(1), e0.Sequence)
	assert.True(t, e0.FromReplay, "history events are flagged from_replay")
	assert.False(t, e0.Timestamp.IsZero())
	assert.Equal(t, orchestration.KindPromptSubmitted, e0.PayloadJSON.Kind)
	require.NotNil(t, e0.PayloadJSON.Prompt)
	assert.Equal(t, "hello there", e0.PayloadJSON.Prompt.Prompt)
	assert.Equal(t, "operator:alice", e0.PayloadJSON.Source)

	// Envelope #2: assistant chunk with stop reason.
	e1 := body.Events[1]
	assert.Equal(t, uint64(2), e1.Sequence)
	assert.Equal(t, orchestration.KindAssistantChunk, e1.PayloadJSON.Kind)
	require.NotNil(t, e1.PayloadJSON.AssistantChunk)
	assert.Equal(t, "general kenobi", e1.PayloadJSON.AssistantChunk.Text)
	assert.Equal(t, "end_turn", e1.PayloadJSON.AssistantChunk.StopReason)
	assert.Equal(t, "agent:claude", e1.PayloadJSON.Source)
}

// Last-Event-ID header resumes from the next sequence (JSON history path).
func TestHandleOrchestratorSessionEvents_LastEventIDResume(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
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
	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
		Sequence: 1, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted, Source: "test",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "system-event"},
	}))
	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
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

	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
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

// TestHandleOrchestratorSessionEvents_SSEEnvelopeShape confirms the SSE data
// frame carries the SAME SessionEventEnvelope shape as the JSON history path,
// so SSE and history readers share a single parse path. The SDK's SSE reader
// spreads the parsed data object into the yielded envelope; a flat or
// differently-keyed payload would silently fail to render.
func TestHandleOrchestratorSessionEvents_SSEEnvelopeShape(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
		Sequence: 1, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted,
		Source: "operator:bob",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "ping"},
	}))

	req := sessionEventsReq(http.MethodGet, "/x?follow=0", "s1")
	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Extract the single `data:` line and decode it as an envelope.
	var dataLine string
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	require.NotEmpty(t, dataLine, "SSE body must contain a data frame: %s", w.Body.String())

	var env struct {
		Sequence    uint64                     `json:"sequence"`
		SessionID   string                     `json:"session_id"`
		FromReplay  bool                       `json:"from_replay"`
		PayloadJSON orchestration.SessionEvent `json:"payload_json"`
	}
	require.NoError(t, json.Unmarshal([]byte(dataLine), &env))
	assert.Equal(t, uint64(1), env.Sequence)
	assert.Equal(t, "s1", env.SessionID)
	assert.True(t, env.FromReplay)
	assert.Equal(t, orchestration.KindPromptSubmitted, env.PayloadJSON.Kind)
	require.NotNil(t, env.PayloadJSON.Prompt)
	assert.Equal(t, "ping", env.PayloadJSON.Prompt.Prompt)
}

// nonFlushResponseWriter is an http.ResponseWriter that deliberately does NOT
// implement http.Flusher (and has no Unwrap), modeling the worst case where the
// connection genuinely cannot flush. The SSE handler must still stream a 200
// rather than refuse with a 500 — flushing is best-effort.
type nonFlushResponseWriter struct {
	header http.Header
	body   strings.Builder
	status int
}

func newNonFlushResponseWriter() *nonFlushResponseWriter {
	return &nonFlushResponseWriter{header: http.Header{}, status: http.StatusOK}
}

func (w *nonFlushResponseWriter) Header() http.Header { return w.header }
func (w *nonFlushResponseWriter) WriteHeader(code int) {
	w.status = code
}
func (w *nonFlushResponseWriter) Write(b []byte) (int, error) { return w.body.Write(b) }

// TestHandleOrchestratorSessionEvents_SSEEmptyNoEvents is the primary regression
// guard for the reported 500: the SSE branch on a session with ZERO events must
// return 200 + text/event-stream headers and close cleanly, never a 500. Range
// on an empty session returns (nil, 0, nil), so an empty replay is normal.
func TestHandleOrchestratorSessionEvents_SSEEmptyNoEvents(t *testing.T) {
	server, _, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	// No Accept: application/json -> SSE path; follow=0 -> replay then close.
	// No events seeded: the empty replay must not error.
	req := sessionEventsReq(http.MethodGet, "/x?follow=0", "empty-session")
	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionEvents(w, req)

	require.Equal(t, http.StatusOK, w.Code, "empty SSE replay must not 500; body: %s", w.Body.String())
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.NotContains(t, w.Body.String(), "streaming unsupported")
}

// TestHandleOrchestratorSessionEvents_SSENonFlushableWriter reproduces the
// production regression: behind the logging middleware the ResponseWriter is
// wrapped and a plain http.Flusher assertion fails. With a writer that cannot
// flush at all, the handler must degrade to best-effort flushing and still emit
// a 200 text/event-stream replay — it must NOT return the old 500.
func TestHandleOrchestratorSessionEvents_SSENonFlushableWriter(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
		Sequence: 1, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted, Source: "test",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "hi"},
	}))

	req := sessionEventsReq(http.MethodGet, "/x?follow=0", "s1")
	w := newNonFlushResponseWriter()
	server.HandleOrchestratorSessionEvents(w, req)

	require.Equal(t, http.StatusOK, w.status, "non-flushable writer must not 500; body: %s", w.body.String())
	assert.Equal(t, "text/event-stream", w.header.Get("Content-Type"))
	assert.Contains(t, w.body.String(), "id: 1", "events must still stream without an explicit flush")
	assert.NotContains(t, w.body.String(), "streaming unsupported")
}

// TestHandleOrchestratorSessionEvents_SSEThroughLoggingWrapper exercises the
// exact production path: the SSE handler invoked behind the *responseWriter that
// the logging middleware installs. The wrapper does not itself declare Flusher;
// its Unwrap method (middleware_flush.go) is what lets http.NewResponseController
// reach the real, flushable recorder. A regression that drops Unwrap/Flush would
// resurface the 500.
func TestHandleOrchestratorSessionEvents_SSEThroughLoggingWrapper(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
		Sequence: 1, TenantID: qrdbsdk.SystemTenantID, SessionID: "s1",
		Timestamp: time.Now().UTC(), Kind: orchestration.KindPromptSubmitted, Source: "test",
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "hi"},
	}))

	rec := httptest.NewRecorder()
	wrapped := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	req := sessionEventsReq(http.MethodGet, "/x?follow=0", "s1")
	server.HandleOrchestratorSessionEvents(wrapped, req)

	require.Equal(t, http.StatusOK, rec.Code, "SSE behind logging wrapper must not 500; body: %s", rec.Body.String())
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "id: 1")
}

// SSE follow path: subscribe, replay history, then exit when the request
// context is cancelled (no live events delivered).
func TestHandleOrchestratorSessionEvents_SSEFollow(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()

	require.NoError(t, log.Append(tenantctx.WithSystem(context.Background()), &orchestration.SessionEvent{
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
