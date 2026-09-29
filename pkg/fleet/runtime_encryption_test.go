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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestRuntime_PersistSession_WritesPlaintextJSON asserts that session records
// are written as plaintext JSON. At-rest confidentiality is delegated to the
// qrdb StorageBarrier; no application-level encryption is applied.
func TestRuntime_PersistSession_WritesPlaintextJSON(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	sess, err := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: filepath.Join(workDir, "project")})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Session file must be a plaintext JSON file.
	path := filepath.Join(workDir, "sessions", sess.ID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted: %v", err)
	}

	// JSON record must contain the session ID in plaintext.
	if !bytes.Contains(raw, []byte(sess.ID)) {
		t.Fatalf("expected session id in plaintext body: %q", raw)
	}
	if !bytes.Contains(raw, []byte("workspace_path")) {
		t.Fatalf("expected JSON field in plaintext body: %q", raw)
	}
}

// TestRuntime_LoadPersistedSessions_RoundTrip asserts that a session written
// by one Runtime can be read back by a fresh Runtime sharing the same workDir.
func TestRuntime_LoadPersistedSessions_RoundTrip(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	sess, err := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: filepath.Join(workDir, "project")})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Simulate daemon restart by constructing a fresh Runtime with the same work directory.
	rt2, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime rt2: %v", err)
	}
	loaded, err := rt2.LoadPersistedSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded count: got %d want 1", len(loaded))
	}
	got := loaded[0]
	if got.SessionID != sess.ID {
		t.Fatalf("session id: got %q want %q", got.SessionID, sess.ID)
	}
	if got.TenantID != "tenant-a" {
		t.Fatalf("tenant id: got %q", got.TenantID)
	}
	if got.Backend != "acp" {
		t.Fatalf("backend: got %q", got.Backend)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("created_at should be populated")
	}
}

// TestRuntime_LoadPersistedSessions_CorruptFileIsQuarantined verifies that a
// file whose content is not valid JSON is renamed to ".corrupt" and the error
// count is returned.
func TestRuntime_LoadPersistedSessions_CorruptFileIsQuarantined(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	sess, err := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: filepath.Join(workDir, "project")})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	jsonPath := filepath.Join(workDir, "sessions", sess.ID+".json")

	// Corrupt the JSON so unmarshal fails.
	if err := os.WriteFile(jsonPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	loaded, loadErr := rt.LoadPersistedSessions(context.Background())
	var corruptErr *PersistedSessionCorruptError
	if !isCorruptError(loadErr, &corruptErr) {
		t.Fatalf("expected PersistedSessionCorruptError, got %v", loadErr)
	}
	if corruptErr.Count != 1 {
		t.Fatalf("corrupt count: got %d want 1", corruptErr.Count)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected 0 loaded sessions when sole record is corrupt, got %d", len(loaded))
	}

	// The corrupted file must have been renamed to .corrupt and NOT deleted.
	if _, err := os.Stat(jsonPath + ".corrupt"); err != nil {
		t.Fatalf("expected .corrupt quarantine file: %v", err)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("original file should have been renamed, got err=%v", err)
	}
}

// TestRuntime_Close_RemovesPersistedSession asserts that Close removes the
// on-disk session record.
func TestRuntime_Close_RemovesPersistedSession(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	sess, err := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: workDir})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	path := filepath.Join(workDir, "sessions", sess.ID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected persisted file: %v", err)
	}

	if err := rt.Close(context.Background(), "acp", sess.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected persisted file removed after Close, got err=%v", err)
	}
}

// TestRuntime_LoadPersistedSessions_NoWorkDirNoOp verifies that an empty
// WorkDir makes LoadPersistedSessions a no-op.
func TestRuntime_LoadPersistedSessions_NoWorkDirNoOp(t *testing.T) {
	t.Parallel()

	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	sessions, err := rt.LoadPersistedSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if sessions != nil {
		t.Fatalf("expected nil sessions with no WorkDir, got %+v", sessions)
	}
}

// isCorruptError is a helper to avoid importing errors in this file.
func isCorruptError(err error, target **PersistedSessionCorruptError) bool {
	if err == nil {
		return false
	}
	if c, ok := err.(*PersistedSessionCorruptError); ok {
		*target = c
		return true
	}
	return false
}

// helper to silence unused-import warnings in edge cases where a test only
// depends on a subset of time symbols.
var _ = time.Now
