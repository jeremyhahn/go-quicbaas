// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

// Package main: branch coverage for renderSessionUpdate, which handles
// session/update notifications from the orchestrator. The existing tests
// cover the happy "session=... {...}" path; these exercise the fallback
// (direct-JSON) and unknown-update-type branches.
package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

const sessionUpdateType = "agent:notification:session/update"

// TestRenderSessionUpdate_FallbackDirectJSON exercises the branch where the
// message is not in "session=... {...}" form but is itself valid JSON; the
// session ID is recovered from event metadata.
func TestRenderSessionUpdate_FallbackDirectJSON(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:     sessionUpdateType,
		Message:  `{"sessionUpdate":"agent_message_chunk","content":{"text":"hi there"}}`,
		Metadata: map[string]string{"session_id": "sess-meta"},
	})

	assert.Contains(t, buf.String(), "hi there")
}

// TestRenderSessionUpdate_NonJSONFallback exercises the branch where the
// message is neither "session=..." nor valid JSON, so it is printed raw.
func TestRenderSessionUpdate_NonJSONFallback(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    sessionUpdateType,
		Message: "plain text not json",
	})

	assert.Contains(t, buf.String(), "plain text not json")
}

// TestRenderSessionUpdate_UnknownTypeRaw exercises the default branch that
// prints "[<sessionUpdate>] <message>" for unrecognised update kinds.
func TestRenderSessionUpdate_UnknownTypeRaw(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    sessionUpdateType,
		Message: `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"plan_update","title":"x"}}}`,
	})

	assert.Contains(t, buf.String(), "[plan_update]")
}

// TestRenderSessionUpdate_AvailableCommandsSkipped verifies the silent-skip
// branch for available_commands_update.
func TestRenderSessionUpdate_AvailableCommandsSkipped(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    sessionUpdateType,
		Message: `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"available_commands_update"}}}`,
	})

	assert.Empty(t, buf.String())
}

// TestRenderSessionUpdate_DirectTypeDispatch exercises the flat-JSON branch
// where the update carries a "type" field instead of "sessionUpdate".
func TestRenderSessionUpdate_DirectTypeDispatch(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:     sessionUpdateType,
		Message:  `{"type":"agent_thought_chunk","content":{"text":"thinking"}}`,
		Metadata: map[string]string{"session_id": "sess-x"},
	})

	assert.Contains(t, buf.String(), "thinking")
}
