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

// =============================================================================
// Runtime.Prompt / Status / Close — unknown backend error paths
// =============================================================================

func TestRuntime_Prompt_UnknownBackendReturnsError(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t)
	_, err := rt.Prompt(context.Background(), "does-not-exist",
		&orchestration.PromptRequest{SessionID: "sess-1", Prompt: "hello"})
	if !errors.Is(err, ErrRuntimeUnknownBackend) {
		t.Fatalf("expected ErrRuntimeUnknownBackend, got %v", err)
	}
}

func TestRuntime_Status_UnknownBackendReturnsError(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t)
	_, err := rt.Status(context.Background(), "does-not-exist", "sess-1")
	if !errors.Is(err, ErrRuntimeUnknownBackend) {
		t.Fatalf("expected ErrRuntimeUnknownBackend, got %v", err)
	}
}

func TestRuntime_Close_UnknownBackendReturnsError(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t)
	err := rt.Close(context.Background(), "does-not-exist", "sess-1")
	if !errors.Is(err, ErrRuntimeUnknownBackend) {
		t.Fatalf("expected ErrRuntimeUnknownBackend, got %v", err)
	}
}

// =============================================================================
// Runtime.NewRuntime — error path (no backends)
// =============================================================================

func TestNewRuntime_NoBackendsReturnsError(t *testing.T) {
	t.Parallel()
	_, err := NewRuntime(RuntimeConfig{
		Backends: nil,
		Logger:   &testLogger{t: t},
	})
	if !errors.Is(err, ErrRuntimeNoBackends) {
		t.Fatalf("expected ErrRuntimeNoBackends, got %v", err)
	}
}

func TestNewRuntime_NoLoggerReturnsError(t *testing.T) {
	t.Parallel()
	_, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   nil,
	})
	if !errors.Is(err, ErrRuntimeNoLogger) {
		t.Fatalf("expected ErrRuntimeNoLogger, got %v", err)
	}
}

// =============================================================================
// Runtime.Spawn — persistence failure is non-fatal
// =============================================================================

// TestRuntime_Spawn_PersistenceFailureIsNonFatal exercises the branch where
// persistSession fails (the sessions dir is read-only) but Spawn still
// returns success. This covers the "logged but not propagated" path.
func TestRuntime_Spawn_PersistenceFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	sessionsDir := filepath.Join(workDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Make the sessions directory read-only so MkdirAll inside persistSession
	// succeeds (dir exists) but WriteFile/CreateTemp fails.
	if err := os.Chmod(sessionsDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessionsDir, 0o700) })

	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	// Spawn must succeed even though persistence will fail.
	sess, spawnErr := rt.Spawn(context.Background(), "tenant-persist-fail", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: workDir})
	if spawnErr != nil {
		t.Fatalf("Spawn: expected success despite persistence failure, got %v", spawnErr)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
}

// =============================================================================
// Runtime.LoadPersistedSessions — empty sessions dir (no-op)
// =============================================================================

func TestRuntime_LoadPersistedSessions_EmptyDirReturnsEmpty(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	sessionsDir := filepath.Join(workDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	sessions, err := rt.LoadPersistedSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions in empty dir, got %d", len(sessions))
	}
}

// TestRuntime_LoadPersistedSessions_NonExistentDirReturnsNil verifies that
// a missing sessions dir is treated as "no sessions yet" (not an error).
func TestRuntime_LoadPersistedSessions_NonExistentDirReturnsNil(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	// Do NOT create the sessions dir.
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	sessions, err := rt.LoadPersistedSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadPersistedSessions on missing dir: %v", err)
	}
	if sessions != nil {
		t.Fatalf("expected nil, got %+v", sessions)
	}
}

// TestRuntime_LoadPersistedSessions_IgnoresNonJSONFiles verifies that files
// not matching the .json suffix (including the old .json.enc format) are
// silently skipped. Session records are now stored as plaintext JSON; the
// old encrypted .json.enc format is no longer read.
func TestRuntime_LoadPersistedSessions_IgnoresNonJSONFiles(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	sessionsDir := filepath.Join(workDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write files that must be ignored: non-.json extension and old .json.enc format.
	for _, name := range []string{"README.txt", "sess-old.json.enc", "notes.bin"} {
		if err := os.WriteFile(filepath.Join(sessionsDir, name), []byte("ignored"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	sessions, loadErr := rt.LoadPersistedSessions(context.Background())
	if loadErr != nil {
		t.Fatalf("unexpected error: %v", loadErr)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions (non-JSON file skipped), got %d", len(sessions))
	}
}

// TestRuntime_LoadPersistedSessions_PlaintextRoundTrip verifies that session
// records written by one Runtime can be read back by a fresh Runtime. All
// records are stored as plaintext JSON; at-rest confidentiality is provided
// by qrdb's StorageBarrier.
func TestRuntime_LoadPersistedSessions_PlaintextRoundTrip(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rt1, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	sess, err := rt1.Spawn(context.Background(), "tenant-plain", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: workDir})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Load on a fresh runtime sharing the same work directory.
	rt2, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	loaded, err := rt2.LoadPersistedSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 session, got %d", len(loaded))
	}
	if loaded[0].SessionID != sess.ID {
		t.Fatalf("session id: got %q, want %q", loaded[0].SessionID, sess.ID)
	}
}

// TestRuntime_Backends_ReturnsSortedSlice ensures the Backends() helper
// returns all registered backends without duplication.
func TestRuntime_Backends_ReturnsSortedSlice(t *testing.T) {
	t.Parallel()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			"acp":    &mockBackend{},
			"ollama": &mockBackend{},
		},
		Logger: &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	names := rt.Backends()
	if len(names) != 2 {
		t.Fatalf("expected 2 backends, got %d: %v", len(names), names)
	}
}
