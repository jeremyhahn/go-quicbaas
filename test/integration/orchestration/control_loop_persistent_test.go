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

//go:build integration && rag && persistent

package orchestration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// requirePersistent skips the test if persistent tests are not enabled.
func requirePersistent(t *testing.T) {
	t.Helper()
	if os.Getenv("QUICBAAS_TEST_PERSISTENT") != "true" {
		t.Skip("persistent tests disabled: set QUICBAAS_TEST_PERSISTENT=true to enable")
	}
}

// =============================================================================
// Claude Session Lifecycle
// =============================================================================

// TestControlLoop_Persistent_SpawnClaudeSession verifies that a Claude session
// can be spawned, checked for status, and closed.
func TestControlLoop_Persistent_SpawnClaudeSession(t *testing.T) {
	requirePersistent(t)

	svc := newAgentService(t)
	tenantID := "persistent-claude-session"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Spawn a claude session.
	spawnCtx, spawnCancel := context.WithTimeout(ctx, 60*time.Second)
	defer spawnCancel()

	resp, err := svc.SpawnSession(spawnCtx, &orchestration.SpawnSessionRequest{
		Backend: "claude",
	})
	require.NoError(t, err, "SpawnSession for claude should succeed")
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.SessionID, "session ID must not be empty")

	sessionID := resp.SessionID

	// Verify the session is active.
	statusResp, err := svc.SessionStatus(ctx, &orchestration.SessionStatusRequest{
		SessionID: sessionID,
	})
	require.NoError(t, err, "SessionStatus should succeed")
	require.NotNil(t, statusResp)
	assert.Equal(t, orchestration.SessionStateActive, statusResp.State,
		"session should be active")

	// Close the session.
	_, err = svc.CloseSession(ctx, &orchestration.CloseSessionRequest{
		SessionID: sessionID,
	})
	require.NoError(t, err, "CloseSession should succeed")

	// Verify the session is closed.
	statusResp, err = svc.SessionStatus(ctx, &orchestration.SessionStatusRequest{
		SessionID: sessionID,
	})
	if err == nil {
		assert.NotEqual(t, orchestration.SessionStateActive, statusResp.State,
			"session should no longer be active")
	}
	// An error is also acceptable: session may be fully removed after close.
}

// TestControlLoop_Persistent_SpawnClaudeSession_InvalidBackend verifies that
// spawning a session with an invalid backend returns an error.
func TestControlLoop_Persistent_SpawnClaudeSession_InvalidBackend(t *testing.T) {
	requirePersistent(t)

	svc := newAgentService(t)
	tenantID := "persistent-invalid-backend"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, err := svc.SpawnSession(ctx, &orchestration.SpawnSessionRequest{
		Backend: "nonexistent-backend",
	})
	require.Error(t, err, "SpawnSession with invalid backend should fail")
}

// =============================================================================
// Codex Session Lifecycle
// =============================================================================

// TestControlLoop_Persistent_SpawnCodexSession verifies that a Codex session
// can be spawned, checked for status, and closed.
func TestControlLoop_Persistent_SpawnCodexSession(t *testing.T) {
	requirePersistent(t)

	svc := newAgentService(t)
	tenantID := "persistent-codex-session"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	spawnCtx, spawnCancel := context.WithTimeout(ctx, 60*time.Second)
	defer spawnCancel()

	resp, err := svc.SpawnSession(spawnCtx, &orchestration.SpawnSessionRequest{
		Backend: "codex",
	})
	require.NoError(t, err, "SpawnSession for codex should succeed")
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.SessionID, "session ID must not be empty")

	sessionID := resp.SessionID

	// Verify the session is active.
	statusResp, err := svc.SessionStatus(ctx, &orchestration.SessionStatusRequest{
		SessionID: sessionID,
	})
	require.NoError(t, err, "SessionStatus should succeed")
	require.NotNil(t, statusResp)
	assert.Equal(t, orchestration.SessionStateActive, statusResp.State,
		"session should be active")

	// Close the session.
	_, err = svc.CloseSession(ctx, &orchestration.CloseSessionRequest{
		SessionID: sessionID,
	})
	require.NoError(t, err, "CloseSession should succeed")
}

// =============================================================================
// Simple Prompt
// =============================================================================

// TestControlLoop_Persistent_SimplePrompt verifies that a spawned Claude session
// can process a minimal prompt and return a non-empty response.
func TestControlLoop_Persistent_SimplePrompt(t *testing.T) {
	requirePersistent(t)

	svc := newAgentService(t)
	tenantID := "persistent-simple-prompt"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	// Spawn a claude session.
	spawnCtx, spawnCancel := context.WithTimeout(ctx, 60*time.Second)
	defer spawnCancel()

	spawnResp, err := svc.SpawnSession(spawnCtx, &orchestration.SpawnSessionRequest{
		Backend: "claude",
	})
	require.NoError(t, err, "SpawnSession should succeed")
	require.NotNil(t, spawnResp)

	sessionID := spawnResp.SessionID

	// Send a minimal prompt - intentionally simple to minimize token usage.
	promptCtx, promptCancel := context.WithTimeout(ctx, promptTimeout)
	defer promptCancel()

	promptResp, err := svc.PromptSession(promptCtx, &orchestration.PromptSessionRequest{
		SessionID: sessionID,
		Backend:   "claude",
		Prompt:    "respond with hello",
	})
	require.NoError(t, err, "PromptSession should succeed")
	require.NotNil(t, promptResp)
	require.NotEmpty(t, promptResp.Content, "response content must not be empty")

	// Verify we got some text back.
	hasText := false
	for _, block := range promptResp.Content {
		if block.Type == "text" && block.Text != "" {
			hasText = true
			break
		}
	}
	assert.True(t, hasText, "response should contain at least one text block")

	// Clean up - close the session.
	_, err = svc.CloseSession(ctx, &orchestration.CloseSessionRequest{
		SessionID: sessionID,
	})
	require.NoError(t, err, "CloseSession should succeed")
}

// TestControlLoop_Persistent_SimplePrompt_NoSession verifies that prompting
// a non-existent session returns an error.
func TestControlLoop_Persistent_SimplePrompt_NoSession(t *testing.T) {
	requirePersistent(t)

	svc := newAgentService(t)
	tenantID := "persistent-prompt-no-session"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		SessionID: "nonexistent-session-id",
		Backend:   "claude",
		Prompt:    "this should fail",
	})
	require.Error(t, err, "PromptSession with invalid session should fail")
}
