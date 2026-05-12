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

package orchestration

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
)

// typedNotFoundKVStore is a memKVStore that returns the typed
// QBError{Code: ErrNotFound} on missing keys instead of (nil, nil),
// matching the production PebbleDB / qrdb-backed stores.
type typedNotFoundKVStore struct{ memKVStore }

func newTypedNotFoundKVStore() *typedNotFoundKVStore {
	s := &typedNotFoundKVStore{}
	s.m = map[string][]byte{}
	return s
}

func (s *typedNotFoundKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := s.memKVStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, &qerrors.QBError{Code: qerrors.ErrNotFound, Op: "kv.get"}
	}
	return v, nil
}

// memKVStore is a trivial in-memory KVStore for tests. Values are stored
// by copy so callers can mutate their input slices without corrupting
// stored state (matches production stores' write semantics).
type memKVStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func newMemKVStore() *memKVStore { return &memKVStore{m: map[string][]byte{}} }

func (s *memKVStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := make([]byte, len(value))
	copy(c, value)
	s.m[key] = c
	return nil
}

func (s *memKVStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[key]
	if !ok {
		return nil, nil
	}
	c := make([]byte, len(v))
	copy(c, v)
	return c, nil
}

func (s *memKVStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *memKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	s.mu.RLock()
	// Snapshot into a sorted slice so iteration order is deterministic.
	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	vals := make([][]byte, len(keys))
	for i, k := range keys {
		v := s.m[k]
		c := make([]byte, len(v))
		copy(c, v)
		vals[i] = c
	}
	s.mu.RUnlock()
	// Sort for determinism across test runs.
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
				vals[i], vals[j] = vals[j], vals[i]
			}
		}
	}
	for i, k := range keys {
		if err := fn(k, vals[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *memKVStore) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for k := range s.m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *memKVStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.m[key]
	return ok, nil
}

// =============================================================================
// stubDEK — deterministic TenantDEKManager for tests
// =============================================================================

type stubDEK struct {
	mu   sync.Mutex
	keys map[string][]byte
}

func newStubDEK() *stubDEK { return &stubDEK{keys: map[string][]byte{}} }

func (s *stubDEK) GetOrCreateTenantDEK(_ context.Context, tenantID string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.keys[tenantID]; ok {
		return append([]byte(nil), k...), nil
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	s.keys[tenantID] = k
	return append([]byte(nil), k...), nil
}

func (s *stubDEK) GetTenantDEK(_ context.Context, tenantID string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[tenantID]
	if !ok {
		return nil, errors.New("stubDEK: no key for tenant")
	}
	return append([]byte(nil), k...), nil
}

func (s *stubDEK) RotateTenantDEK(ctx context.Context, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return err
	}
	s.keys[tenantID] = k
	return nil
}

func (s *stubDEK) DeleteTenantDEK(_ context.Context, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, tenantID)
	return nil
}

func (s *stubDEK) GetTenantDEKStatus(_ context.Context, tenantID string) (*security.TenantDEKStatus, error) {
	return &security.TenantDEKStatus{TenantID: tenantID, Version: 1, Algorithm: "AES-256-GCM", KeyBytes: 32}, nil
}

func (s *stubDEK) ListTenantDEKs(_ context.Context) ([]string, error) { return nil, nil }

func (s *stubDEK) Close() error { return nil }

// =============================================================================
// Tests
// =============================================================================

func TestKVSessionEventLog_PlaintextRoundTrip(t *testing.T) {
	t.Parallel()
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		if err := log.Append(ctx, newPromptEvent("t", "s", uint64(i), "p")); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	page, next, err := log.Range(ctx, "t", "s", 1, 0)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	if len(page) != 5 || next != 0 {
		t.Errorf("range result: len=%d next=%d", len(page), next)
	}
	for i, e := range page {
		if e.Sequence != uint64(i+1) {
			t.Errorf("page[%d] seq: got %d, want %d", i, e.Sequence, i+1)
		}
	}

	latest, _ := log.LatestSequence(ctx, "t", "s")
	if latest != 5 {
		t.Errorf("latest: got %d, want 5", latest)
	}
}

func TestKVSessionEventLog_GapDetection(t *testing.T) {
	t.Parallel()
	log, _ := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	ctx := context.Background()

	_ = log.Append(ctx, newPromptEvent("t", "s", 1, "a"))
	if err := log.Append(ctx, newPromptEvent("t", "s", 3, "skip")); !errors.Is(err, ErrSessionLogGap) {
		t.Fatalf("got %v, want ErrSessionLogGap", err)
	}
	// Duplicates also fail.
	if err := log.Append(ctx, newPromptEvent("t", "s", 1, "dup")); !errors.Is(err, ErrSessionLogGap) {
		t.Fatalf("got %v, want ErrSessionLogGap on duplicate", err)
	}
}

func TestKVSessionEventLog_EncryptedAtRest(t *testing.T) {
	t.Parallel()
	kv := newMemKVStore()
	dek := newStubDEK()
	log, _ := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv, DEK: dek})
	ctx := context.Background()

	_ = log.Append(ctx, newPromptEvent("t", "s", 1, "very secret prompt"))

	// The raw stored bytes must NOT contain the plaintext.
	var stored []byte
	_ = kv.Scan(ctx, "sessions/t/s/events/", func(_ string, v []byte) error {
		stored = v
		return nil
	})
	if len(stored) == 0 {
		t.Fatal("no stored payload found")
	}
	if bytes.Contains(stored, []byte("very secret prompt")) {
		t.Fatalf("plaintext leaked into stored bytes: %q", stored)
	}
	// Must begin with the encryption marker.
	if len(stored) < 2 || stored[0] != 0xE1 || stored[1] != 0x07 {
		t.Errorf("encryption marker missing: % x", stored[:min(8, len(stored))])
	}

	// And decrypt via Range returns the plaintext.
	page, _, _ := log.Range(ctx, "t", "s", 1, 0)
	if len(page) != 1 || page[0].Prompt.Prompt != "very secret prompt" {
		t.Errorf("round-trip failed: %+v", page)
	}
}

func TestKVSessionEventLog_EncryptionAADMismatch(t *testing.T) {
	t.Parallel()
	// Encrypt under session A, then rename the key to session B. Decrypt
	// must fail because session ID is bound into the AEAD additional data.
	kv := newMemKVStore()
	dek := newStubDEK()
	log, _ := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv, DEK: dek})
	ctx := context.Background()

	_ = log.Append(ctx, newPromptEvent("t", "sessA", 1, "secret"))

	// Relocate the key under sessB's prefix.
	var origKey string
	var val []byte
	_ = kv.Scan(ctx, "sessions/t/sessA/events/", func(k string, v []byte) error {
		origKey = k
		val = append([]byte(nil), v...)
		return nil
	})
	newKey := strings.Replace(origKey, "/sessA/", "/sessB/", 1)
	_ = kv.Put(ctx, newKey, val)
	_ = kv.Delete(ctx, origKey)
	// Meta still under sessA but session B now has its event; simulate
	// "this event was tampered from session A into session B".
	_, _, err := log.Range(ctx, "t", "sessB", 1, 0)
	if err == nil {
		t.Fatal("expected AEAD decrypt failure on session mismatch")
	}
}

func TestKVSessionEventLog_MetaLifecycle(t *testing.T) {
	t.Parallel()
	log, _ := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	ctx := context.Background()

	if _, err := log.GetMeta(ctx, "t", "s"); !errors.Is(err, ErrSessionLogNotFound) {
		t.Fatalf("got %v, want ErrSessionLogNotFound", err)
	}

	created := time.Now().UTC()
	meta := &SessionMeta{
		SessionID: "s",
		TenantID:  "t",
		Backend:   "ollama",
		Model:     "qwen3:1.7b",
		Status:    SessionStateActive,
		CreatedAt: created,
	}
	_ = log.PutMeta(ctx, meta)
	got, err := log.GetMeta(ctx, "t", "s")
	if err != nil {
		t.Fatalf("get meta: %v", err)
	}
	if got.Backend != "ollama" {
		t.Errorf("roundtrip: %+v", got)
	}
}

func TestKVSessionEventLog_ListSessionsScan(t *testing.T) {
	t.Parallel()
	log, _ := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	ctx := context.Background()
	t0 := time.Now().UTC()

	for i, id := range []string{"s1", "s2", "s3"} {
		meta := &SessionMeta{
			SessionID: id,
			TenantID:  "t",
			Backend:   "ollama",
			Status:    SessionStateActive,
			CreatedAt: t0.Add(time.Duration(i) * time.Minute),
		}
		_ = log.PutMeta(ctx, meta)
	}
	// Another tenant's session shouldn't leak.
	_ = log.PutMeta(ctx, &SessionMeta{SessionID: "x", TenantID: "t2", Backend: "claude", Status: SessionStateActive, CreatedAt: t0})

	// Also add an event so the scan has to skip non-meta keys.
	_ = log.Append(ctx, newPromptEvent("t", "s1", 1, "hi"))

	out, err := log.ListSessions(ctx, "t", nil, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("got %d sessions, want 3", len(out))
	}
	// Newest first.
	if out[0].SessionID != "s3" || out[2].SessionID != "s1" {
		t.Errorf("order wrong: %v", idsOf(out))
	}
}

func TestKVSessionEventLog_GetMeta_TranslatesTypedNotFound(t *testing.T) {
	t.Parallel()
	// Production KV stores (Pebble, qrdb) return *QBError{ErrNotFound}
	// on missing keys, NOT (nil, nil). GetMeta must translate that to
	// the orchestration sentinel so callers' errors.Is checks work
	// regardless of the backing store. Regression test for the
	// PushEvents-rejected loop on sessionless inference.
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newTypedNotFoundKVStore()})
	if err != nil {
		t.Fatalf("new log: %v", err)
	}
	ctx := context.Background()
	if _, err := log.GetMeta(ctx, "tenant", "missing"); !errors.Is(err, ErrSessionLogNotFound) {
		t.Fatalf("got %v, want ErrSessionLogNotFound", err)
	}
	// And LatestSequence on the same store must NOT propagate the
	// typed not-found — fresh sessions report seq=0.
	seq, err := log.LatestSequence(ctx, "tenant", "missing")
	if err != nil {
		t.Fatalf("latest sequence: %v", err)
	}
	if seq != 0 {
		t.Fatalf("got %d, want 0", seq)
	}
}

func TestKVSessionEventLog_LatestSequenceWithoutMeta(t *testing.T) {
	t.Parallel()
	log, _ := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	ctx := context.Background()

	// No PutMeta yet — LatestSequence must fall back to scanning events.
	for i := 1; i <= 3; i++ {
		_ = log.Append(ctx, newPromptEvent("t", "s", uint64(i), "x"))
	}
	seq, err := log.LatestSequence(ctx, "t", "s")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if seq != 3 {
		t.Errorf("got %d, want 3", seq)
	}
}

func TestKVSessionEventLog_SubscribeDeliversLive(t *testing.T) {
	t.Parallel()
	log, _ := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	ctx := context.Background()
	ch, unsub, _ := log.Subscribe(ctx, "t", "s")
	defer unsub()

	go func() {
		for i := 1; i <= 3; i++ {
			_ = log.Append(ctx, newPromptEvent("t", "s", uint64(i), "p"))
		}
	}()

	var got []uint64
	for i := 0; i < 3; i++ {
		select {
		case ev := <-ch:
			got = append(got, ev.Sequence)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	}
	if len(got) != 3 || got[0] != 1 {
		t.Errorf("live delivery: %v", got)
	}
}

func TestKVSessionEventLog_NoKVStoreErrors(t *testing.T) {
	t.Parallel()
	if _, err := NewKVSessionEventLog(KVSessionEventLogConfig{}); err == nil {
		t.Fatal("expected error without KV store")
	}
}

// Tiny min used by the encryption-at-rest test.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
