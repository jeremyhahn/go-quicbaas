//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Decode-error paths (malformed body after the sub-opcode byte)
// =============================================================================

func TestHandleOrchestratorSession_DecodeErrors(t *testing.T) {
	handler := createOrchestratorHandler(&testAgentService{})
	ctx := context.Background()
	bad := []byte("{not-json")

	subOps := []byte{
		SubOpOrchestratorPrompt,
		SubOpOrchestratorSessionStatus,
		SubOpOrchestratorCloseSession,
		SubOpOrchestratorListSessions,
	}
	for _, subOp := range subOps {
		resp := handler.HandleRequest(ctx, buildOrchestratorRequest(subOp, bad))
		assert.NotEqual(t, StatusOK, resp.Status, "subOp %d should reject malformed body", subOp)
	}
}

// =============================================================================
// Service-error paths
// =============================================================================

func TestHandleOrchestratorSession_ServiceErrors(t *testing.T) {
	sentinel := errors.New("orchestrator boom")
	agentSvc := &testAgentService{
		promptSessionFunc: func(context.Context, *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
			return nil, sentinel
		},
		// A session-scoped prompt is dispatched asynchronously, so the error
		// surfaces from the acceptance call rather than the turn itself.
		promptAsyncFunc: func(context.Context, *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error) {
			return nil, sentinel
		},
		sessionStatusFunc: func(context.Context, *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
			return nil, sentinel
		},
		closeSessionFunc: func(context.Context, *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
			return nil, sentinel
		},
		listSessionsFunc: func(context.Context, *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
			return nil, sentinel
		},
	}
	handler := createOrchestratorHandler(agentSvc)
	ctx := context.Background()

	enc := func(v interface{}) []byte {
		b, err := EncodePayload(v)
		require.NoError(t, err)
		return b
	}

	cases := []struct {
		name  string
		subOp byte
		body  []byte
	}{
		{"prompt", SubOpOrchestratorPrompt, enc(&orchestration.PromptSessionRequest{SessionID: "s1"})},
		{"session-status", SubOpOrchestratorSessionStatus, enc(&orchestration.SessionStatusRequest{SessionID: "s1"})},
		{"close-session", SubOpOrchestratorCloseSession, enc(&orchestration.CloseSessionRequest{SessionID: "s1"})},
		{"list-sessions", SubOpOrchestratorListSessions, enc(&orchestration.ListSessionsRequest{})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, buildOrchestratorRequest(tc.subOp, tc.body))
			assert.NotEqual(t, StatusOK, resp.Status)
		})
	}
}

// =============================================================================
// Nil-service paths (service does not provide an agent)
// =============================================================================

func TestHandleOrchestratorSession_NilService(t *testing.T) {
	handler := createOrchestratorHandler(nil) // Agent() returns nil
	ctx := context.Background()

	subOps := []byte{
		SubOpOrchestratorPrompt,
		SubOpOrchestratorSessionStatus,
		SubOpOrchestratorCloseSession,
		SubOpOrchestratorListSessions,
		SubOpOrchestratorListBackends,
	}
	for _, subOp := range subOps {
		resp := handler.HandleRequest(ctx, buildOrchestratorRequest(subOp, []byte("{}")))
		assert.NotEqual(t, StatusOK, resp.Status, "subOp %d should fail with nil agent", subOp)
	}
}

// ListBackends service-error path.
func TestHandleOrchestratorListBackends_ServiceError(t *testing.T) {
	agentSvc := &testAgentService{
		listBackendsFunc: func(context.Context) ([]*orchestration.AgentBackendInfo, error) {
			return nil, errors.New("backends boom")
		},
	}
	handler := createOrchestratorHandler(agentSvc)
	resp := handler.HandleRequest(context.Background(),
		buildOrchestratorRequest(SubOpOrchestratorListBackends, []byte{}))
	assert.NotEqual(t, StatusOK, resp.Status)
}
