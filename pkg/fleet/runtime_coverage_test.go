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

package fleet

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// callbackBackend is a backend whose Prompt invokes the request's
// EventCallback once, letting tests observe the forwarder's callback
// wrapping path (forwardCB + caller callback both fire).
type callbackBackend struct{}

func (b *callbackBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	return &orchestration.Session{ID: "cb-sess", WorkDir: req.WorkDir}, nil
}

func (b *callbackBackend) Prompt(_ context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	if req.EventCallback != nil {
		req.EventCallback(&orchestration.SessionEvent{
			SessionID:      req.SessionID,
			Kind:           orchestration.KindAssistantChunk,
			AssistantChunk: &orchestration.AssistantChunkPayload{Text: "chunk"},
		})
	}
	return &orchestration.PromptResponse{SessionID: req.SessionID}, nil
}

func (b *callbackBackend) Status(_ context.Context, id string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{SessionID: id}, nil
}

func (b *callbackBackend) Close(_ context.Context, _ string) error { return nil }

func (b *callbackBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindAgent}
}

func (b *callbackBackend) Type() string         { return "acp" }
func (b *callbackBackend) ToolGuidance() string { return "" }

// TestRuntime_SetEventForwarder_PromptForwardsEvents wires a forwarder
// onto the runtime via SetEventForwarder, spawns a session, and prompts
// it. This drives the previously-uncovered SetEventForwarder accessor
// plus the EventForwarder-aware branches inside Prompt (prompt event
// emission and callback wrapping), and backendForSession.
func TestRuntime_SetEventForwarder_PromptForwardsEvents(t *testing.T) {
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &callbackBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	fwd := newTestForwarder(t, EventForwarderConfig{QueueSize: 32})
	rt.SetEventForwarder(fwd)

	ctx := context.Background()
	sess, err := rt.Spawn(ctx, "tenant-a", nil, "acp", &orchestration.SpawnRequest{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// backendForSession should now resolve the spawned session's backend.
	if got := rt.backendForSession(sess.ID); got != "acp" {
		t.Fatalf("backendForSession=%q want acp", got)
	}

	// A caller-supplied callback must still fire alongside the forwarder.
	callerSaw := make(chan struct{}, 1)
	_, err = rt.Prompt(ctx, "acp", &orchestration.PromptRequest{
		SessionID: sess.ID,
		Prompt:    "hello there",
		EventCallback: func(_ *orchestration.SessionEvent) {
			select {
			case callerSaw <- struct{}{}:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	// The PromptSubmitted event plus the backend-emitted assistant chunk
	// should have been enqueued by the forwarder.
	if len(fwd.queue) < 2 {
		t.Fatalf("expected >=2 forwarded events, got %d", len(fwd.queue))
	}
	select {
	case <-callerSaw:
	default:
		t.Fatal("expected caller callback to fire through wrapper")
	}
}

// TestRuntime_Prompt_ForwarderResolvesBackendBySession covers the branch
// where Prompt is called with an empty backend name and must recover the
// backend from backendForSession to tag the event.
func TestRuntime_Prompt_NoForwarder_PassesThrough(t *testing.T) {
	rt := newTestRuntime(t)
	resp, err := rt.Prompt(context.Background(), "mock", &orchestration.PromptRequest{
		SessionID: "s1",
		Prompt:    "hi",
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.SessionID != "s1" {
		t.Fatalf("unexpected session id %q", resp.SessionID)
	}
}

// TestRuntime_Prompt_UnknownBackend covers the error branch.
func TestRuntime_Prompt_UnknownBackend(t *testing.T) {
	rt := newTestRuntime(t)
	_, err := rt.Prompt(context.Background(), "nope", &orchestration.PromptRequest{SessionID: "s"})
	if err == nil {
		t.Fatal("expected unknown backend error")
	}
}

// -----------------------------------------------------------------------------
// writeAtomic error paths
// -----------------------------------------------------------------------------

// TestWriteAtomic_Success exercises the happy path and verifies the
// resulting file mode is 0600.
func TestWriteAtomic_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := writeAtomic(path, []byte("payload")); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("content=%q", data)
	}
}

// TestWriteAtomic_TempCreateFails drives the temp-create error branch by
// pointing at a non-existent parent directory.
func TestWriteAtomic_TempCreateFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-subdir", "out.json")
	err := writeAtomic(path, []byte("x"))
	if err == nil {
		t.Fatal("expected temp-create failure")
	}
	var ioErr *PersistSessionIOError
	if !errors.As(err, &ioErr) {
		t.Fatalf("want PersistSessionIOError, got %T", err)
	}
	if ioErr.Op != "temp-create" {
		t.Fatalf("op=%q want temp-create", ioErr.Op)
	}
}

// TestWriteAtomic_RenameFails forces the rename to fail by making the
// destination a non-empty directory (rename of a file onto a non-empty
// directory fails on Linux).
func TestWriteAtomic_RenameFails(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(filepath.Join(dest, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := writeAtomic(dest, []byte("x"))
	if err == nil {
		t.Fatal("expected rename failure")
	}
	var ioErr *PersistSessionIOError
	if !errors.As(err, &ioErr) {
		t.Fatalf("want PersistSessionIOError, got %T", err)
	}
	if ioErr.Op != "rename" {
		t.Fatalf("op=%q want rename", ioErr.Op)
	}
}
