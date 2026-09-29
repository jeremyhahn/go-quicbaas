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

package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// quicbaas.Orchestrator/GetSessionConversation over the in-process transport
// =============================================================================

// loggingFakeAgentService is a fakeAgentService that also exposes a session
// event log. The bare fakeAgentService deliberately does not, which is what
// makes the "deployment with no transcript" branch reachable.
type loggingFakeAgentService struct {
	*fakeAgentService

	log orchestration.SessionEventLog
}

// SessionEventLog implements orchestration.SessionLogAccessor.
func (f *loggingFakeAgentService) SessionEventLog() orchestration.SessionEventLog { return f.log }

var _ orchestration.SessionLogAccessor = (*loggingFakeAgentService)(nil)

// newConversationTransport wires an in-process transport over an agent service
// whose transcript already holds one exchange.
func newConversationTransport(t *testing.T, tenantID, sessionID string) *Transport {
	t.Helper()
	log := orchestration.NewMemorySessionEventLog()
	ctx := context.Background()
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

	tr, err := New(&orchMock{
		mockQuicBaaS: &mockQuicBaaS{},
		agent:        &loggingFakeAgentService{fakeAgentService: &fakeAgentService{}, log: log},
		proj:         &fakeProjectService{},
		admin:        &fakeFleetAdminService{},
	})
	require.NoError(t, err)
	return tr
}

// TestEmbeddedGetSessionConversation_ReadsTheTranscript asserts the in-process
// caller folds the same transcript the remote transports read, into the same
// turns.
func TestEmbeddedGetSessionConversation_ReadsTheTranscript(t *testing.T) {
	const tenantID = "t1"
	const sessionID = "s1"
	tr := newConversationTransport(t, tenantID, sessionID)

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSessionConversation",
		&orchestratorSDK.GetSessionConversationRequest{TenantID: tenantID, SessionID: sessionID})
	require.NoError(t, err)

	page, ok := resp.(*orchestratorSDK.GetSessionConversationResponse)
	require.Truef(t, ok, "response is %T", resp)
	require.Len(t, page.Turns, 2)
	assert.Equal(t, orchestratorSDK.SessionTurnRoleUser, page.Turns[0].Role)
	assert.Equal(t, "do the work", page.Turns[0].Text)

	reply := page.Turns[1]
	assert.Equal(t, orchestratorSDK.SessionTurnRoleAssistant, reply.Role)
	assert.Equal(t, "done", reply.Text)
	assert.Equal(t, "m1", reply.Model)
	require.NotNil(t, reply.Usage, "the turn's cost must reach an in-process caller too")
	assert.Equal(t, int64(8), reply.Usage.OutputTokens)
}

// TestEmbeddedGetSessionConversation_RejectsBadInput asserts a mistyped
// request, an unaddressed one, and a deployment whose agent service keeps no
// transcript are all typed refusals rather than an empty conversation.
func TestEmbeddedGetSessionConversation_RejectsBadInput(t *testing.T) {
	t.Run("wrong request type", func(t *testing.T) {
		tr := newConversationTransport(t, "t1", "s1")
		_, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSessionConversation",
			&orchestratorSDK.CloseSessionRequest{SessionID: "s1"})
		require.Error(t, err)
	})

	t.Run("no session id", func(t *testing.T) {
		tr := newConversationTransport(t, "t1", "s1")
		_, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSessionConversation",
			&orchestratorSDK.GetSessionConversationRequest{TenantID: "t1"})
		require.Error(t, err)
	})

	t.Run("agent service keeps no transcript", func(t *testing.T) {
		tr, err := New(&orchMock{
			mockQuicBaaS: &mockQuicBaaS{},
			agent:        &fakeAgentService{},
			proj:         &fakeProjectService{},
			admin:        &fakeFleetAdminService{},
		})
		require.NoError(t, err)
		_, reqErr := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSessionConversation",
			&orchestratorSDK.GetSessionConversationRequest{TenantID: "t1", SessionID: "s1"})
		require.Error(t, reqErr)
	})
}
