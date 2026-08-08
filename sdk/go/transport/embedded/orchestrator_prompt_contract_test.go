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

// TestOrchestratorPromptSession_ReturnsCompletedTurn pins the embedded prompt
// contract: the service turn is flattened into the SDK response with its text
// intact and reported as done. The embedded transport used to derive Done from
// the stop reason, which the claude stream-json backend leaves empty on every
// completed turn, so a real answer was reported as unfinished.
func TestOrchestratorPromptSession_ReturnsCompletedTurn(t *testing.T) {
	tr := newOrchTransport(t, false)

	result, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "PromptSession",
		&orchestratorSDK.PromptSessionRequest{SessionID: "s1", Prompt: "hi"})
	require.NoError(t, err)

	resp, ok := result.(*orchestratorSDK.PromptSessionResponse)
	require.True(t, ok, "result type = %T", result)
	assert.Equal(t, "hello", resp.Response, "the turn's text must reach the caller")
	assert.True(t, resp.Done, "a completed turn must not be reported as unfinished")
	assert.Equal(t, "s1", resp.SessionID)
}
