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

// Package main: coverage for the orchestrator_attach.go render dispatch
// and truncateForPretty helpers.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// truncateForPretty
// =============================================================================

func TestTruncateForPretty_NoTruncation(t *testing.T) {
	assert.Equal(t, "hello", truncateForPretty("hello", 10))
}

func TestTruncateForPretty_Truncates(t *testing.T) {
	assert.Equal(t, "hel…", truncateForPretty("hello world", 3))
}

func TestTruncateForPretty_ExactBoundary(t *testing.T) {
	assert.Equal(t, "hello", truncateForPretty("hello", 5))
}

// =============================================================================
// pickAttachRenderer dispatch
// =============================================================================

func TestPickAttachRenderer_Pretty(t *testing.T) {
	r, err := pickAttachRenderer("pretty", os.Stdout)
	require.NoError(t, err)
	_, ok := r.(*prettyAttachRenderer)
	assert.True(t, ok)
}

func TestPickAttachRenderer_DefaultsToPretty(t *testing.T) {
	r, err := pickAttachRenderer("", os.Stdout)
	require.NoError(t, err)
	_, ok := r.(*prettyAttachRenderer)
	assert.True(t, ok)
}

func TestPickAttachRenderer_JSON(t *testing.T) {
	r, err := pickAttachRenderer("json", os.Stdout)
	require.NoError(t, err)
	_, ok := r.(*jsonAttachRenderer)
	assert.True(t, ok)
}

func TestPickAttachRenderer_NDJSON(t *testing.T) {
	r, err := pickAttachRenderer("ndjson", os.Stdout)
	require.NoError(t, err)
	_, ok := r.(*jsonAttachRenderer)
	assert.True(t, ok)
}

func TestPickAttachRenderer_Unknown(t *testing.T) {
	_, err := pickAttachRenderer("yaml", os.Stdout)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --format")
}

// =============================================================================
// prettyAttachRenderer.Render — exercises every "kind" branch
// =============================================================================

// mkEnvelope wraps a JSON payload into a SessionEventEnvelope for tests.
func mkEnvelope(t *testing.T, sessionID string, seq uint64, payload map[string]interface{}) *orchestratorSDK.SessionEventEnvelope {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return &orchestratorSDK.SessionEventEnvelope{
		SessionID:   sessionID,
		TenantID:    "t1",
		Sequence:    seq,
		Timestamp:   time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
		PayloadJSON: body,
	}
}

// makeTempFile returns a writable *os.File rooted in t.TempDir().
func makeTempFile(t *testing.T) *os.File {
	t.Helper()
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "out.txt"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// readBack returns the contents of the test file written by a renderer.
func readBack(t *testing.T, f *os.File) string {
	t.Helper()
	require.NoError(t, f.Sync())
	got, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	return string(got)
}

func TestPrettyAttachRenderer_PromptSubmitted(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 1, map[string]interface{}{
		"kind": "prompt_submitted",
		"prompt": map[string]interface{}{
			"prompt": "hello",
			"model":  "claude",
		},
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "prompt_submitted")
	assert.Contains(t, got, "hello")
}

func TestPrettyAttachRenderer_AssistantChunk(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 2, map[string]interface{}{
		"kind": "assistant_chunk",
		"assistant_chunk": map[string]interface{}{
			"text":        "response text",
			"stop_reason": "end_turn",
		},
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "assistant_chunk")
	assert.Contains(t, got, "response text")
	assert.Contains(t, got, "end_turn")
}

func TestPrettyAttachRenderer_ToolInvocation(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 3, map[string]interface{}{
		"kind": "tool_invocation",
		"tool_invocation": map[string]interface{}{
			"name": "bash",
		},
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "tool_invocation")
	assert.Contains(t, got, "bash")
}

func TestPrettyAttachRenderer_ToolResultError(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 4, map[string]interface{}{
		"kind": "tool_result",
		"tool_result": map[string]interface{}{
			"content":  "fail output",
			"is_error": true,
		},
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "ERROR")
	assert.Contains(t, got, "fail output")
}

func TestPrettyAttachRenderer_ToolResultOK(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 5, map[string]interface{}{
		"kind": "tool_result",
		"tool_result": map[string]interface{}{
			"content": "ok output",
		},
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "ok output")
}

func TestPrettyAttachRenderer_FileOp(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 6, map[string]interface{}{
		"kind": "file_op",
		"file_op": map[string]interface{}{
			"op":   "write",
			"path": "/tmp/x",
		},
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "write")
	assert.Contains(t, got, "/tmp/x")
}

func TestPrettyAttachRenderer_SystemMessage(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 7, map[string]interface{}{
		"kind": "system_message",
		"system": map[string]interface{}{
			"level":   "warn",
			"message": "low disk",
		},
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "warn")
	assert.Contains(t, got, "low disk")
}

func TestPrettyAttachRenderer_SessionClosed(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 8, map[string]interface{}{
		"kind": "session_closed",
		"closed": map[string]interface{}{
			"reason": "timeout",
		},
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "timeout")
}

func TestPrettyAttachRenderer_SessionClosedDefault(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 9, map[string]interface{}{
		"kind": "session_closed",
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "unknown")
}

func TestPrettyAttachRenderer_UnknownKind(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 10, map[string]interface{}{
		"kind": "novel_kind",
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, "novel_kind")
}

func TestPrettyAttachRenderer_BadJSON(t *testing.T) {
	f := makeTempFile(t)
	r := &prettyAttachRenderer{out: f}
	env := &orchestratorSDK.SessionEventEnvelope{
		SessionID:   "s1",
		Sequence:    1,
		Timestamp:   time.Now(),
		PayloadJSON: []byte("{not valid json"),
	}
	err := r.Render(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode payload")
}

// =============================================================================
// jsonAttachRenderer.Render
// =============================================================================

func TestJSONAttachRenderer_Happy(t *testing.T) {
	f := makeTempFile(t)
	r := &jsonAttachRenderer{out: f}
	env := mkEnvelope(t, "s1", 1, map[string]interface{}{
		"kind":   "prompt_submitted",
		"prompt": map[string]interface{}{"prompt": "p"},
	})
	require.NoError(t, r.Render(env))
	got := readBack(t, f)
	assert.Contains(t, got, `"session_id":"s1"`)
	assert.Contains(t, got, `"sequence":1`)
	// timestamp + event are nested objects
	assert.Contains(t, got, `"event"`)
	assert.True(t, strings.HasSuffix(strings.TrimSpace(got), "}"))
}

func TestJSONAttachRenderer_BadJSON(t *testing.T) {
	f := makeTempFile(t)
	r := &jsonAttachRenderer{out: f}
	env := &orchestratorSDK.SessionEventEnvelope{
		PayloadJSON: []byte("{nope"),
	}
	err := r.Render(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode payload")
}
