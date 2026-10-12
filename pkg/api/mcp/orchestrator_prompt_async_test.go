//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newAsyncTurnOrchestrator builds a real orchestrator wired to asyncTestBackend
// so the MCP tools exercise the production acknowledge-then-poll path rather
// than a hand-written stand-in. withLog controls whether a durable session log
// is installed, which is what makes an asynchronous turn possible at all.
func newAsyncTurnOrchestrator(t *testing.T, withLog bool) orchestration.AgentService {
	t.Helper()
	base, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	require.NoError(t, err)

	agentSvc, err := orchestration.NewAgentService(
		orchestration.NewInMemoryAgentStore(),
		map[string]orchestration.AgentBackend{"test": &asyncTestBackend{}},
		logger.NewAdapter(base),
	)
	require.NoError(t, err)

	if withLog {
		installer, ok := agentSvc.(interface {
			SetSessionEventLog(log orchestration.SessionEventLog)
		})
		require.True(t, ok, "agent service must accept a session event log")
		installer.SetSessionEventLog(orchestration.NewMemorySessionEventLog())
	}
	return agentSvc
}

// asyncTestBackend is a minimal AgentBackend that answers a prompt with one
// text block. It exists so the MCP tools can be exercised against a real
// orchestrator — the acknowledge-then-poll contract is only meaningful when
// something actually writes the transcript the poll reads back.
type asyncTestBackend struct{}

func (b *asyncTestBackend) Type() string { return "test" }
func (b *asyncTestBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{}
}
func (b *asyncTestBackend) ToolGuidance() string { return "" }
func (b *asyncTestBackend) Spawn(_ context.Context, _ *orchestration.SpawnRequest) (*orchestration.Session, error) {
	return &orchestration.Session{ID: "sess-1"}, nil
}
func (b *asyncTestBackend) Prompt(_ context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{
		SessionID:  req.SessionID,
		Content:    []orchestration.ContentBlock{{Type: "text", Text: "the answer"}},
		StopReason: orchestration.StopReasonEndTurn,
	}, nil
}
func (b *asyncTestBackend) Close(_ context.Context, _ string) error { return nil }
func (b *asyncTestBackend) Status(_ context.Context, _ string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{State: orchestration.SessionStateActive}, nil
}

// turnDrainer is satisfied by the orchestrator implementation; it lets the test
// wait for the background turn to write its terminal event without polling.
type turnDrainer interface{ WaitForTurns() }

// TestMCPPromptAcknowledgesThenTranscriptIsPollable pins the MCP contract.
//
// MCP is request/response only — there is no server-push channel — so the
// prompt tool cannot deliver the agent's output. It returns the acceptance
// acknowledgement, and the caller reads the turn back from the durable
// transcript with quicbaas_orchestrator_session_history, starting at the
// sequence the acknowledgement returned, until the turn's terminal event
// carrying the same turn_id appears.
func TestMCPPromptAcknowledgesThenTranscriptIsPollable(t *testing.T) {
	agentSvc := newAsyncTurnOrchestrator(t, true)
	reg := newFleetRegistry(t, agentSvc)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	// The prompt tool returns only the acknowledgement — never the agent's
	// output, which MCP has no way to stream.
	raw, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_prompt", map[string]interface{}{
		"tenant_id":  "tenant-1",
		"session_id": "sess-1",
		"backend":    "test",
		"prompt":     "do the thing",
	})
	require.NoError(t, err)

	ack, ok := raw.(map[string]interface{})
	require.True(t, ok, "result type = %T", raw)
	assert.Equal(t, orchestration.TurnStatusAccepted, ack["status"])
	assert.Equal(t, "sess-1", ack["session_id"])
	turnID, _ := ack["turn_id"].(string)
	assert.NotEmpty(t, turnID, "the caller needs the turn ID to recognise the turn's terminal event")
	assert.NotContains(t, ack, "content", "MCP cannot stream a turn; it must not pretend to return one")
	fromSeq, ok := ack["sequence"].(float64)
	require.True(t, ok, "the acknowledgement must carry the sequence to poll from")

	// Let the background turn finish, then poll the transcript exactly as an
	// MCP client would.
	agentSvc.(turnDrainer).WaitForTurns()

	raw, err = reg.executeAgentTool(ctx, "quicbaas_orchestrator_session_history", map[string]interface{}{
		"tenant_id":     "tenant-1",
		"session_id":    "sess-1",
		"from_sequence": fromSeq,
	})
	require.NoError(t, err)

	page, ok := raw.(map[string]interface{})
	require.True(t, ok, "result type = %T", raw)
	events, ok := page["events"].([]interface{})
	require.True(t, ok, "history must carry an events array")

	var sawOutput, sawCompletion bool
	for _, item := range events {
		ev, isMap := item.(map[string]interface{})
		require.True(t, isMap)
		switch ev["kind"] {
		case string(orchestration.KindAssistantChunk):
			chunk := ev["assistant_chunk"].(map[string]interface{})
			assert.Equal(t, "the answer", chunk["text"])
			sawOutput = true
		case string(orchestration.KindTurnCompleted):
			completed := ev["turn_completed"].(map[string]interface{})
			assert.Equal(t, turnID, completed["turn_id"],
				"the terminal event must be tied to the acknowledged turn")
			sawCompletion = true
		}
	}
	assert.True(t, sawOutput, "the agent's output must be readable from the transcript")
	assert.True(t, sawCompletion, "the poll must be able to see the turn end")
}

// TestMCPSessionHistoryRejectsBadInput asserts the transcript tool refuses a
// request it cannot scope, rather than returning another tenant's events or an
// empty page that reads like "the session has nothing to say".
func TestMCPSessionHistoryRejectsBadInput(t *testing.T) {
	reg := newFleetRegistry(t, newAsyncTurnOrchestrator(t, true))

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "tenant-1"), "quicbaas_orchestrator_session_history",
		map[string]interface{}{"tenant_id": "tenant-1"})
	require.Error(t, err, "a transcript read without a session must be refused")

	_, err = reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "tenant-1"), "quicbaas_orchestrator_session_history",
		map[string]interface{}{"session_id": "sess-1"})
	require.Error(t, err, "a transcript read without a tenant must be refused")
}

// TestMCPSessionHistoryWithoutEventLog asserts the tool reports the missing
// capability instead of silently returning nothing when the orchestrator was
// built without a durable log.
func TestMCPSessionHistoryWithoutEventLog(t *testing.T) {
	reg := newFleetRegistry(t, newAsyncTurnOrchestrator(t, false))

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "tenant-1"), "quicbaas_orchestrator_session_history",
		map[string]interface{}{"tenant_id": "tenant-1", "session_id": "sess-1"})
	require.Error(t, err, "a transcript read with no event log installed must be refused")
}
