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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// GET /api/v1/orchestrator/sessions/{sessionID}/history
// =============================================================================

// seedConversation writes one exchange to a session's transcript.
func seedConversation(t *testing.T, log orchestration.SessionEventLog, tenantID, sessionID string) {
	t.Helper()
	ctx := tenantctx.WithSystem(context.Background())
	require.NoError(t, log.PutMeta(ctx, &orchestration.SessionMeta{
		SessionID: sessionID, TenantID: tenantID,
	}))
	require.NoError(t, orchestration.AppendNextSessionEvent(ctx, log, &orchestration.SessionEvent{
		SessionID: sessionID, TenantID: tenantID,
		Kind:   orchestration.KindPromptSubmitted,
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "do the work"},
	}))
	require.NoError(t, orchestration.AppendNextSessionEvent(ctx, log, &orchestration.SessionEvent{
		SessionID: sessionID, TenantID: tenantID,
		Kind: orchestration.KindAssistantChunk,
		AssistantChunk: &orchestration.AssistantChunkPayload{
			Text:       "done",
			StopReason: "end_turn",
			Model:      "m1",
			Usage:      &orchestration.TokenUsage{InputTokens: 7, OutputTokens: 8},
		},
	}))
}

// TestHandleOrchestratorSessionHistory_ReturnsTheConversation is the REST half
// of the issue's acceptance check: a session that has been prompted and
// answered must read back as a conversation, with the answer's cost on it.
// go-quicbaas#793.
func TestHandleOrchestratorSessionHistory_ReturnsTheConversation(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()
	seedConversation(t, log, qrdbsdk.SystemTenantID, "s1")

	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionHistory(w, sessionEventsReq(http.MethodGet, "/x", "s1"))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var page orchestration.SessionHistoryPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Turns, 2)
	assert.Equal(t, orchestration.SessionTurnRoleUser, page.Turns[0].Role)
	assert.Equal(t, "do the work", page.Turns[0].Text)
	assert.Equal(t, orchestration.SessionTurnRoleAssistant, page.Turns[1].Role)
	assert.Equal(t, "done", page.Turns[1].Text)
	require.NotNil(t, page.Turns[1].Usage)
	assert.Equal(t, int64(8), page.Turns[1].Usage.OutputTokens)
}

// TestHandleOrchestratorSessionHistory_PagesWithACursor asserts the cursor is
// honoured and echoed on both the body and the response header, so a client
// can resume without re-reading the page it already has.
func TestHandleOrchestratorSessionHistory_PagesWithACursor(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()
	seedConversation(t, log, qrdbsdk.SystemTenantID, "s1")

	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionHistory(w, sessionEventsReq(http.MethodGet, "/x?limit=1", "s1"))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var first orchestration.SessionHistoryPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.Len(t, first.Turns, 1)
	require.NotZero(t, first.NextCursor, "a truncated page must say where the next one starts")
	assert.Equal(t, w.Header().Get("X-Next-Cursor"), jsonUint(first.NextCursor))

	w2 := httptest.NewRecorder()
	server.HandleOrchestratorSessionHistory(w2,
		sessionEventsReq(http.MethodGet, "/x?cursor="+jsonUint(first.NextCursor), "s1"))
	require.Equal(t, http.StatusOK, w2.Code)

	var second orchestration.SessionHistoryPage
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &second))
	require.NotEmpty(t, second.Turns)
	assert.Greater(t, second.Turns[0].Sequence, first.Turns[0].Sequence,
		"the second page repeated a turn the first already delivered")
}

// TestHandleOrchestratorSessionHistory_TenantIsolation asserts a caller naming
// another tenant reads nothing. The transcript keyspace is tenant-prefixed, so
// this is structural rather than a filter someone can forget.
func TestHandleOrchestratorSessionHistory_TenantIsolation(t *testing.T) {
	server, log, cleanup := setupSessionEventsServer(t, true)
	defer cleanup()
	seedConversation(t, log, "tenant-owner", "s1")

	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionHistory(w,
		sessionEventsReq(http.MethodGet, "/x?tenant_id=tenant-intruder", "s1"))
	require.Equal(t, http.StatusOK, w.Code)

	var page orchestration.SessionHistoryPage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	assert.Empty(t, page.Turns, "a second tenant read another tenant's conversation")
}

// TestHandleOrchestratorSessionHistory_RejectsBadInput asserts every unusable
// request is refused with a status that says so, rather than served as an
// empty conversation.
func TestHandleOrchestratorSessionHistory_RejectsBadInput(t *testing.T) {
	tests := []struct {
		name      string
		withLog   bool
		target    string
		sessionID string
		want      int
	}{
		{"no session id", true, "/x", "", http.StatusBadRequest},
		{"invalid limit", true, "/x?limit=notanumber", "s1", http.StatusBadRequest},
		{"negative limit", true, "/x?limit=-1", "s1", http.StatusBadRequest},
		{"invalid cursor", true, "/x?cursor=notanumber", "s1", http.StatusBadRequest},
		{"no transcript configured", false, "/x", "s1", http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, cleanup := setupSessionEventsServer(t, tt.withLog)
			defer cleanup()

			w := httptest.NewRecorder()
			server.HandleOrchestratorSessionHistory(w, sessionEventsReq(http.MethodGet, tt.target, tt.sessionID))
			assert.Equal(t, tt.want, w.Code, "body: %s", w.Body.String())
		})
	}
}

// jsonUint renders a uint64 the way the header does, so the two can be
// compared without importing strconv into every assertion.
func jsonUint(v uint64) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
