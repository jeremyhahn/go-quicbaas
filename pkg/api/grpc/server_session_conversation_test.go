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

package grpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// OrchestratorService.GetSessionConversation
// =============================================================================

// seedConversationLog writes one exchange to a transcript and returns it.
func seedConversationLog(t *testing.T, tenantID, sessionID string) orchestration.SessionEventLog {
	t.Helper()
	ctx := context.Background()
	log := orchestration.NewMemorySessionEventLog()
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
		Kind:           orchestration.KindToolInvocation,
		ToolInvocation: &orchestration.ToolInvocationPayload{InvocationID: "i1", Name: "Read"},
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
	return log
}

// TestGetSessionConversation_ReturnsTurnsWithCost is the gRPC half of the
// issue's acceptance check: the conversation crosses the wire as turns, with
// each answer's tool use and token cost attached. go-quicbaas#793.
func TestGetSessionConversation_ReturnsTurnsWithCost(t *testing.T) {
	const tenantID = "t1"
	srv := newSessionEventsServer(t, seedConversationLog(t, tenantID, "s1"))

	resp, err := srv.GetSessionConversation(context.Background(), &pb.GetSessionConversationRequest{
		TenantId:  tenantID,
		SessionId: "s1",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetTurns(), 2)

	assert.Equal(t, "user", resp.GetTurns()[0].GetRole())
	assert.Equal(t, "do the work", resp.GetTurns()[0].GetText())

	reply := resp.GetTurns()[1]
	assert.Equal(t, "assistant", reply.GetRole())
	assert.Equal(t, "done", reply.GetText())
	assert.Equal(t, "m1", reply.GetModel())
	require.Len(t, reply.GetTools(), 1)
	assert.Equal(t, "Read", reply.GetTools()[0].GetName())

	require.NotEmpty(t, reply.GetUsageJson(), "the turn's cost must cross the wire")
	var usage orchestration.TokenUsage
	require.NoError(t, json.Unmarshal(reply.GetUsageJson(), &usage))
	assert.Equal(t, int64(8), usage.OutputTokens)
}

// TestGetSessionConversation_PagesWithACursor asserts a bounded page reports
// where the next one starts, so a client can resume rather than re-read.
func TestGetSessionConversation_PagesWithACursor(t *testing.T) {
	const tenantID = "t1"
	srv := newSessionEventsServer(t, seedConversationLog(t, tenantID, "s1"))

	first, err := srv.GetSessionConversation(context.Background(), &pb.GetSessionConversationRequest{
		TenantId: tenantID, SessionId: "s1", Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, first.GetTurns(), 1)
	require.NotZero(t, first.GetNextCursor())

	second, err := srv.GetSessionConversation(context.Background(), &pb.GetSessionConversationRequest{
		TenantId: tenantID, SessionId: "s1", Cursor: first.GetNextCursor(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, second.GetTurns())
	assert.Greater(t, second.GetTurns()[0].GetSequence(), first.GetTurns()[0].GetSequence(),
		"the second page repeated a turn the first already delivered")
}

// TestGetSessionConversation_TenantIsolation asserts another tenant naming the
// same session id reads nothing.
func TestGetSessionConversation_TenantIsolation(t *testing.T) {
	srv := newSessionEventsServer(t, seedConversationLog(t, "tenant-owner", "s1"))

	resp, err := srv.GetSessionConversation(context.Background(), &pb.GetSessionConversationRequest{
		TenantId: "tenant-intruder", SessionId: "s1",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.GetTurns(), "a second tenant read another tenant's conversation")
}

// TestGetSessionConversation_RejectsBadInput asserts an unaddressed request
// and a deployment with no transcript are typed refusals rather than an empty
// conversation, which would read as a session that said nothing.
func TestGetSessionConversation_RejectsBadInput(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		srv := newSessionEventsServer(t, orchestration.NewMemorySessionEventLog())
		_, err := srv.GetSessionConversation(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("no session id", func(t *testing.T) {
		srv := newSessionEventsServer(t, orchestration.NewMemorySessionEventLog())
		_, err := srv.GetSessionConversation(context.Background(),
			&pb.GetSessionConversationRequest{TenantId: "t1"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("no transcript configured", func(t *testing.T) {
		srv := newSessionEventsServer(t, nil)
		_, err := srv.GetSessionConversation(context.Background(),
			&pb.GetSessionConversationRequest{TenantId: "t1", SessionId: "s1"})
		require.Error(t, err)
		assert.Equal(t, codes.Unavailable, status.Code(err))
	})
}
