//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// quicbaas_orchestrator_session_conversation
// =============================================================================

// TestMCPSessionConversationReadsTheTurns is the MCP half of the issue's
// acceptance check: after a spawn and a prompt, the tool must return the
// conversation — the operator's question and the agent's answer — rather than
// the raw event log. go-quicbaas#793.
func TestMCPSessionConversationReadsTheTurns(t *testing.T) {
	agentSvc := newAsyncTurnOrchestrator(t, true)
	reg := newFleetRegistry(t, agentSvc)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	_, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_prompt", map[string]interface{}{
		"tenant_id":  "tenant-1",
		"session_id": "sess-1",
		"backend":    "test",
		"prompt":     "do the thing",
	})
	require.NoError(t, err)
	agentSvc.(turnDrainer).WaitForTurns()

	raw, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_session_conversation", map[string]interface{}{
		"tenant_id":  "tenant-1",
		"session_id": "sess-1",
	})
	require.NoError(t, err)

	page, ok := raw.(map[string]interface{})
	require.True(t, ok, "result type = %T", raw)
	turns, ok := page["turns"].([]interface{})
	require.True(t, ok, "a conversation page must carry a turns array")

	var sawPrompt, sawAnswer bool
	for _, item := range turns {
		turn, isMap := item.(map[string]interface{})
		require.True(t, isMap, "turn type = %T", item)
		switch turn["role"] {
		case string(orchestration.SessionTurnRoleUser):
			if turn["text"] == "do the thing" {
				sawPrompt = true
			}
		case string(orchestration.SessionTurnRoleAssistant):
			if turn["text"] == "the answer" {
				sawAnswer = true
			}
		}
	}
	assert.True(t, sawPrompt, "the operator's prompt is missing from the conversation: %v", turns)
	assert.True(t, sawAnswer, "the agent's answer is missing from the conversation: %v", turns)
}

// TestMCPSessionConversationRejectsBadInput asserts an unaddressed request and
// a deployment with no transcript are typed refusals rather than an empty
// conversation, which an MCP client would read as a session that said nothing.
func TestMCPSessionConversationRejectsBadInput(t *testing.T) {
	t.Run("no session id", func(t *testing.T) {
		reg := newFleetRegistry(t, newAsyncTurnOrchestrator(t, true))
		_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "tenant-1"), "quicbaas_orchestrator_session_conversation",
			map[string]interface{}{"tenant_id": "tenant-1"})
		require.Error(t, err)
	})

	t.Run("no tenant id", func(t *testing.T) {
		reg := newFleetRegistry(t, newAsyncTurnOrchestrator(t, true))
		_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "tenant-1"), "quicbaas_orchestrator_session_conversation",
			map[string]interface{}{"session_id": "sess-1"})
		require.Error(t, err)
	})

	t.Run("no transcript configured", func(t *testing.T) {
		reg := newFleetRegistry(t, newAsyncTurnOrchestrator(t, false))
		_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "tenant-1"), "quicbaas_orchestrator_session_conversation",
			map[string]interface{}{"tenant_id": "tenant-1", "session_id": "sess-1"})
		require.Error(t, err)
	})
}

// TestMCPSessionConversationToolIsRegistered pins the tool's name and its
// required arguments. A drift here makes the verb unreachable over MCP with
// "unknown tool" as the only symptom.
func TestMCPSessionConversationToolIsRegistered(t *testing.T) {
	reg := newFleetRegistry(t, newAsyncTurnOrchestrator(t, true))
	tool := toolByName(t, reg, "quicbaas_orchestrator_session_conversation")
	assert.ElementsMatch(t, []string{"tenant_id", "session_id"}, requiredArgs(t, tool))
}
