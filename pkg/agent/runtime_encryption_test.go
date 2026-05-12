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

package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
)

// fakeTenantDEKManager returns deterministic 32-byte keys per tenant without
// involving an actual KMS. It satisfies security.TenantDEKManager for tests
// that exercise the at-rest encryption pathway without introducing KMS state.
type fakeTenantDEKManager struct {
	mu   sync.Mutex
	keys map[string][]byte
	// forceMissing tenants cause GetTenantDEK to return an error, simulating
	// a DEK rotation or operator-induced mismatch.
	forceMissing map[string]bool
}

func newFakeDEKManager(t *testing.T, tenants ...string) *fakeTenantDEKManager {
	t.Helper()
	m := &fakeTenantDEKManager{keys: map[string][]byte{}}
	for _, tenant := range tenants {
		buf := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, buf); err != nil {
			t.Fatalf("rand: %v", err)
		}
		m.keys[tenant] = buf
	}
	return m
}

func (f *fakeTenantDEKManager) GetOrCreateTenantDEK(_ context.Context, tenantID string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.keys[tenantID]; ok {
		out := make([]byte, len(existing))
		copy(out, existing)
		return out, nil
	}
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, err
	}
	f.keys[tenantID] = buf
	out := make([]byte, 32)
	copy(out, buf)
	return out, nil
}

func (f *fakeTenantDEKManager) GetTenantDEK(_ context.Context, tenantID string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forceMissing != nil && f.forceMissing[tenantID] {
		return nil, errors.New("dek missing")
	}
	existing, ok := f.keys[tenantID]
	if !ok {
		return nil, errors.New("tenant dek not found")
	}
	out := make([]byte, len(existing))
	copy(out, existing)
	return out, nil
}

func (f *fakeTenantDEKManager) RotateTenantDEK(_ context.Context, tenantID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return err
	}
	f.keys[tenantID] = buf
	return nil
}

func (f *fakeTenantDEKManager) DeleteTenantDEK(_ context.Context, tenantID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.keys, tenantID)
	return nil
}

func (f *fakeTenantDEKManager) GetTenantDEKStatus(_ context.Context, tenantID string) (*security.TenantDEKStatus, error) {
	return &security.TenantDEKStatus{TenantID: tenantID, Algorithm: "AES-256-GCM", KeyBytes: 32}, nil
}

func (f *fakeTenantDEKManager) ListTenantDEKs(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.keys))
	for k := range f.keys {
		out = append(out, k)
	}
	return out, nil
}

func (f *fakeTenantDEKManager) Close() error { return nil }

// Compile-time interface check.
var _ security.TenantDEKManager = (*fakeTenantDEKManager)(nil)

// newTestRuntimeWithDEK builds a Runtime whose session persistence path is
// wired to a tenant DEK manager and a temp work directory.
func newTestRuntimeWithDEK(t *testing.T, dek security.TenantDEKManager) (*Runtime, string) {
	t.Helper()
	workDir := t.TempDir()
	rt, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:           &testLogger{t: t},
		TenantDEKManager: dek,
		WorkDir:          workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt, workDir
}

func TestRuntime_PersistSession_EncryptsOnDisk(t *testing.T) {
	t.Parallel()

	dek := newFakeDEKManager(t, "tenant-a")
	rt, workDir := newTestRuntimeWithDEK(t, dek)

	sess, err := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: filepath.Join(workDir, "project")})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	path := filepath.Join(workDir, "sessions", sess.ID+".json.enc")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted: %v", err)
	}
	if !bytes.HasPrefix(raw, persistedSessionMagic) {
		t.Fatalf("expected magic prefix %x, got %x", persistedSessionMagic, raw[:4])
	}
	// Plaintext session_id must never appear in the ciphertext body.
	if bytes.Contains(raw[len(persistedSessionMagic):], []byte(sess.ID)) {
		t.Fatalf("session id leaked into ciphertext: %x", raw)
	}
	if bytes.Contains(raw[len(persistedSessionMagic):], []byte("workspace_path")) {
		t.Fatalf("JSON key leaked into ciphertext")
	}
}

func TestRuntime_LoadPersistedSessions_RoundTrip(t *testing.T) {
	t.Parallel()

	dek := newFakeDEKManager(t, "tenant-a")
	rt, workDir := newTestRuntimeWithDEK(t, dek)

	sess, err := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: filepath.Join(workDir, "project")})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Simulate daemon restart by constructing a fresh Runtime with the same
	// work directory + DEK manager.
	rt2, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:           &testLogger{t: t},
		TenantDEKManager: dek,
		WorkDir:          workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
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

func TestRuntime_LoadPersistedSessions_CorruptFileIsQuarantined(t *testing.T) {
	t.Parallel()

	dek := newFakeDEKManager(t, "tenant-a")
	rt, workDir := newTestRuntimeWithDEK(t, dek)

	sess, err := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: filepath.Join(workDir, "project")})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	encPath := filepath.Join(workDir, "sessions", sess.ID+".json.enc")

	// Corrupt the ciphertext so GCM auth fails.
	raw, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	raw[len(raw)-1] ^= 0xFF
	if err := os.WriteFile(encPath, raw, 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	loaded, loadErr := rt.LoadPersistedSessions(context.Background())
	var corruptErr *PersistedSessionCorruptError
	if !errors.As(loadErr, &corruptErr) {
		t.Fatalf("expected PersistedSessionCorruptError, got %v", loadErr)
	}
	if corruptErr.Count != 1 {
		t.Fatalf("corrupt count: got %d want 1", corruptErr.Count)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected 0 loaded sessions when sole record is corrupt, got %d", len(loaded))
	}

	// The corrupted file must have been renamed to .corrupt and NOT deleted.
	if _, err := os.Stat(encPath + ".corrupt"); err != nil {
		t.Fatalf("expected .corrupt quarantine file: %v", err)
	}
	if _, err := os.Stat(encPath); !os.IsNotExist(err) {
		t.Fatalf("original encrypted file should have been renamed, got err=%v", err)
	}
}

func TestRuntime_PersistSession_NilDEKFallsBackToPlaintextWithWarning(t *testing.T) {
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
	// No DEK manager → plaintext JSON is written.
	path := filepath.Join(workDir, "sessions", sess.ID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plaintext: %v", err)
	}
	if !bytes.Contains(raw, []byte(sess.ID)) {
		t.Fatal("expected session id in plaintext body")
	}
	if !bytes.Contains(raw, []byte("workspace_path")) {
		t.Fatal("expected JSON field in plaintext body")
	}

	loaded, err := rt.LoadPersistedSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if len(loaded) != 1 || loaded[0].SessionID != sess.ID {
		t.Fatalf("loaded: %+v", loaded)
	}
}

func TestRuntime_Close_RemovesPersistedSession(t *testing.T) {
	t.Parallel()

	dek := newFakeDEKManager(t, "tenant-a")
	rt, workDir := newTestRuntimeWithDEK(t, dek)

	sess, err := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: workDir})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	path := filepath.Join(workDir, "sessions", sess.ID+".json.enc")
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

// helper to silence unused-import warnings in edge cases where a test only
// depends on a subset of time symbols.
var _ = time.Now
