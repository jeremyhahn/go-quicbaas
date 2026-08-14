// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestOrchestratorPromptSession_AcceptsSessionTurn pins the embedded prompt
// contract for a session turn: the transport acknowledges the turn instead of
// waiting for it, exactly as the remote transports do. Running in-process is no
// reason to diverge — a caller that switches transports must not see the turn
// semantics change underneath it.
func TestOrchestratorPromptSession_AcceptsSessionTurn(t *testing.T) {
	tr := newOrchTransport(t, false)

	result, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "PromptSession",
		&orchestratorSDK.PromptSessionRequest{SessionID: "s1", Prompt: "hi"})
	require.NoError(t, err)

	resp, ok := result.(*orchestratorSDK.PromptSessionResponse)
	require.True(t, ok, "result type = %T", result)
	assert.True(t, resp.Accepted, "a session turn must be acknowledged, not awaited")
	assert.False(t, resp.Done, "an accepted turn has not finished")
	assert.Equal(t, "turn-test", resp.TurnID, "the caller needs the turn ID to follow the turn")
	assert.Empty(t, resp.Response, "the turn's output arrives on the session event stream")
	assert.Equal(t, "s1", resp.SessionID)
}

// TestOrchestratorPromptSession_StatelessTurnIsSynchronous pins the other half
// of the contract: a prompt with no session has no event log to publish to, so
// the completed turn is returned inline with its text intact and reported as
// done. The embedded transport used to derive Done from the stop reason, which
// the claude stream-json backend leaves empty on every completed turn, so a
// real answer was reported as unfinished.
func TestOrchestratorPromptSession_StatelessTurnIsSynchronous(t *testing.T) {
	tr := newOrchTransport(t, false)

	result, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "PromptSession",
		&orchestratorSDK.PromptSessionRequest{Prompt: "hi"})
	require.NoError(t, err)

	resp, ok := result.(*orchestratorSDK.PromptSessionResponse)
	require.True(t, ok, "result type = %T", result)
	assert.False(t, resp.Accepted, "a stateless turn is not deferred")
	assert.Equal(t, "hello", resp.Response, "the turn's text must reach the caller")
	assert.True(t, resp.Done, "a completed turn must not be reported as unfinished")
}
