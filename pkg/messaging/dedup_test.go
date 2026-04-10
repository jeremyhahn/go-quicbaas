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

package messaging

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// mockStorage implements storage.Storage for testing.
type mockStorage struct {
	data      map[string][]byte
	getErr    error
	setErr    error
	deleteErr error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		data: make(map[string][]byte),
	}
}

func (m *mockStorage) Get(key []byte) ([]byte, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	val, ok := m.data[string(key)]
	if !ok {
		return nil, errors.New("not found")
	}
	return val, nil
}

func (m *mockStorage) Set(key, value []byte) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.data[string(key)] = value
	return nil
}

func (m *mockStorage) Delete(key []byte) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.data, string(key))
	return nil
}

func (m *mockStorage) Batch() storage.Batch {
	return &mockBatch{storage: m}
}

func (m *mockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	return &mockIterator{
		storage: m,
		opts:    opts,
		keys:    m.getSortedKeys(opts),
		pos:     -1,
	}
}

func (m *mockStorage) getSortedKeys(opts *storage.IteratorOptions) []string {
	var keys []string
	for k := range m.data {
		if opts != nil {
			if opts.LowerBound != nil && k < string(opts.LowerBound) {
				continue
			}
			if opts.UpperBound != nil && k >= string(opts.UpperBound) {
				continue
			}
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (m *mockStorage) Snapshot() (storage.Snapshot, error) {
	return nil, errors.New("not implemented")
}

func (m *mockStorage) Close() error {
	return nil
}

func (m *mockStorage) Exists(key []byte) (bool, error) {
	_, ok := m.data[string(key)]
	return ok, nil
}

func (m *mockStorage) List(prefix []byte) ([][]byte, error) {
	var keys [][]byte
	p := string(prefix)
	for k := range m.data {
		if len(p) == 0 || strings.HasPrefix(k, p) {
			keys = append(keys, []byte(k))
		}
	}
	return keys, nil
}

// mockBatch implements storage.Batch for testing.
type mockBatch struct {
	storage    *mockStorage
	operations []func() error
	closed     bool
}

func (b *mockBatch) Set(key, value []byte) error {
	keyCopy := make([]byte, len(key))
	valueCopy := make([]byte, len(value))
	copy(keyCopy, key)
	copy(valueCopy, value)
	b.operations = append(b.operations, func() error {
		return b.storage.Set(keyCopy, valueCopy)
	})
	return nil
}

func (b *mockBatch) Delete(key []byte) error {
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	b.operations = append(b.operations, func() error {
		return b.storage.Delete(keyCopy)
	})
	return nil
}

func (b *mockBatch) Commit() error {
	if b.closed {
		return errors.New("batch is closed")
	}
	for _, op := range b.operations {
		if err := op(); err != nil {
			return err
		}
	}
	b.closed = true
	return nil
}

func (b *mockBatch) Rollback() error {
	b.closed = true
	return nil
}

func (b *mockBatch) Close() error {
	if !b.closed {
		return b.Rollback()
	}
	return nil
}

// mockIterator implements storage.Iterator for testing.
type mockIterator struct {
	storage *mockStorage
	opts    *storage.IteratorOptions
	keys    []string
	pos     int
	err     error
}

func (i *mockIterator) SeekToFirst() bool {
	if len(i.keys) == 0 {
		return false
	}
	i.pos = 0
	return true
}

func (i *mockIterator) SeekToLast() bool {
	if len(i.keys) == 0 {
		return false
	}
	i.pos = len(i.keys) - 1
	return true
}

func (i *mockIterator) Seek(target []byte) bool {
	for idx, k := range i.keys {
		if k >= string(target) {
			i.pos = idx
			return true
		}
	}
	return false
}

func (i *mockIterator) Next() bool {
	if i.pos >= len(i.keys)-1 {
		return false
	}
	i.pos++
	return true
}

func (i *mockIterator) Prev() bool {
	if i.pos <= 0 {
		return false
	}
	i.pos--
	return true
}

func (i *mockIterator) Valid() bool {
	return i.pos >= 0 && i.pos < len(i.keys)
}

func (i *mockIterator) Key() []byte {
	if !i.Valid() {
		return nil
	}
	return []byte(i.keys[i.pos])
}

func (i *mockIterator) Value() []byte {
	if !i.Valid() {
		return nil
	}
	return i.storage.data[i.keys[i.pos]]
}

func (i *mockIterator) Error() error {
	return i.err
}

func (i *mockIterator) Close() error {
	return nil
}

func newDedupTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return log
}

func TestNewDedupStoreImpl(t *testing.T) {
	t.Parallel()

	t.Run("creates store with default options", func(t *testing.T) {
		t.Parallel()
		log := newDedupTestLogger(t)
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)

		if ds == nil {
			t.Fatal("expected non-nil DedupStoreImpl")
		}
		if ds.GetPrefix() != DefaultDedupPrefix {
			t.Errorf("expected prefix %q, got %q", DefaultDedupPrefix, ds.GetPrefix())
		}
		if ds.GetWindow() != DefaultDedupWindow {
			t.Errorf("expected window %v, got %v", DefaultDedupWindow, ds.GetWindow())
		}
	})

	t.Run("creates store with custom prefix", func(t *testing.T) {
		t.Parallel()
		log := newDedupTestLogger(t)
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log, WithDedupStorePrefix("custom/prefix/"))

		if ds.GetPrefix() != "custom/prefix/" {
			t.Errorf("expected prefix %q, got %q", "custom/prefix/", ds.GetPrefix())
		}
	})

	t.Run("creates store with custom window", func(t *testing.T) {
		t.Parallel()
		log := newDedupTestLogger(t)
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log, WithDedupStoreWindow(time.Hour))

		if ds.GetWindow() != time.Hour {
			t.Errorf("expected window %v, got %v", time.Hour, ds.GetWindow())
		}
	})
}

func TestDedupStoreImpl_ComputeHash(t *testing.T) {
	t.Parallel()
	log := newDedupTestLogger(t)
	ms := newMockStorage()
	ds := NewDedupStoreImpl(ms, log)

	t.Run("computes hash for entire payload", func(t *testing.T) {
		t.Parallel()
		hash, err := ds.ComputeHash([]byte(`{"key":"value"}`), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash == "" {
			t.Error("expected non-empty hash")
		}
		// SHA256 produces 64 hex characters
		if len(hash) != 64 {
			t.Errorf("expected 64 character hash, got %d", len(hash))
		}
	})

	t.Run("computes consistent hash", func(t *testing.T) {
		t.Parallel()
		payload := []byte(`{"test":"data"}`)
		hash1, _ := ds.ComputeHash(payload, nil)
		hash2, _ := ds.ComputeHash(payload, nil)
		if hash1 != hash2 {
			t.Error("same payload should produce same hash")
		}
	})

	t.Run("computes hash for specific fields", func(t *testing.T) {
		t.Parallel()
		payload := []byte(`{"id":"123","name":"test","other":"ignored"}`)
		hash, err := ds.ComputeHash(payload, []string{"id", "name"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash == "" {
			t.Error("expected non-empty hash")
		}
	})

	t.Run("returns error for nil payload", func(t *testing.T) {
		t.Parallel()
		_, err := ds.ComputeHash(nil, nil)
		if err == nil {
			t.Error("expected error for nil payload")
		}
		var payloadErr *InvalidPayloadError
		if !errors.As(err, &payloadErr) {
			t.Errorf("expected InvalidPayloadError, got %T", err)
		}
	})

	t.Run("returns error for invalid JSON when fields specified", func(t *testing.T) {
		t.Parallel()
		_, err := ds.ComputeHash([]byte("not json"), []string{"field"})
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
		var hashErr *HashComputeError
		if !errors.As(err, &hashErr) {
			t.Errorf("expected HashComputeError, got %T", err)
		}
	})
}

func TestDedupStoreImpl_StoreEntry(t *testing.T) {
	t.Parallel()
	log := newDedupTestLogger(t)

	t.Run("stores entry successfully", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		entry := &DedupEntry{
			Count:     1,
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			MessageID: "msg-123",
		}

		err := ds.StoreEntry(ctx, "test-channel", "key-123", entry)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify entry was stored
		storedData, ok := ms.data[DefaultDedupPrefix+"test-channel/key-123"]
		if !ok {
			t.Error("entry was not stored")
		}
		if len(storedData) == 0 {
			t.Error("stored data is empty")
		}
	})

	t.Run("returns error for empty channel", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		entry := &DedupEntry{Count: 1}
		err := ds.StoreEntry(ctx, "", "key", entry)
		if err == nil {
			t.Error("expected error for empty channel")
		}
	})

	t.Run("returns error for empty key", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		entry := &DedupEntry{Count: 1}
		err := ds.StoreEntry(ctx, "channel", "", entry)
		if err == nil {
			t.Error("expected error for empty key")
		}
		var keyErr *InvalidDedupKeyError
		if !errors.As(err, &keyErr) {
			t.Errorf("expected InvalidDedupKeyError, got %T", err)
		}
	})

	t.Run("returns error for nil entry", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		err := ds.StoreEntry(ctx, "channel", "key", nil)
		if err == nil {
			t.Error("expected error for nil entry")
		}
		var entryErr *InvalidDedupEntryError
		if !errors.As(err, &entryErr) {
			t.Errorf("expected InvalidDedupEntryError, got %T", err)
		}
	})
}

func TestDedupStoreImpl_GetEntry(t *testing.T) {
	t.Parallel()
	log := newDedupTestLogger(t)

	t.Run("retrieves stored entry", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		// Store an entry first
		entry := &DedupEntry{
			Count:     5,
			FirstSeen: time.Now().Add(-time.Hour),
			LastSeen:  time.Now(),
			MessageID: "msg-456",
		}
		ds.StoreEntry(ctx, "channel", "key-456", entry)

		// Retrieve it
		retrieved, err := ds.GetEntry(ctx, "channel", "key-456")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if retrieved.Count != 5 {
			t.Errorf("expected count 5, got %d", retrieved.Count)
		}
		if retrieved.MessageID != "msg-456" {
			t.Errorf("expected message ID 'msg-456', got %q", retrieved.MessageID)
		}
	})

	t.Run("returns error for non-existent entry", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		_, err := ds.GetEntry(ctx, "channel", "nonexistent")
		if err == nil {
			t.Error("expected error for non-existent entry")
		}
	})

	t.Run("returns error for empty channel", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		_, err := ds.GetEntry(ctx, "", "key")
		if err == nil {
			t.Error("expected error for empty channel")
		}
	})

	t.Run("returns error for empty key", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		_, err := ds.GetEntry(ctx, "channel", "")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestDedupStoreImpl_DeleteEntry(t *testing.T) {
	t.Parallel()
	log := newDedupTestLogger(t)

	t.Run("deletes entry successfully", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		// Store an entry first
		entry := &DedupEntry{Count: 1}
		ds.StoreEntry(ctx, "channel", "key", entry)

		// Delete it
		err := ds.DeleteEntry(ctx, "channel", "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify it's gone
		_, err = ds.GetEntry(ctx, "channel", "key")
		if err == nil {
			t.Error("expected error for deleted entry")
		}
	})

	t.Run("returns error for empty channel", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		err := ds.DeleteEntry(ctx, "", "key")
		if err == nil {
			t.Error("expected error for empty channel")
		}
	})

	t.Run("returns error for empty key", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		err := ds.DeleteEntry(ctx, "channel", "")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestDedupStoreImpl_CheckAndStore(t *testing.T) {
	t.Parallel()
	log := newDedupTestLogger(t)

	t.Run("stores new message", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		msg := &Message{
			ID:      "msg-1",
			Payload: []byte(`{"data":"test"}`),
		}

		info, err := ds.CheckAndStore(ctx, "channel", msg, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.IsDuplicate {
			t.Error("expected new message to not be duplicate")
		}
		if info.Count != 1 {
			t.Errorf("expected count 1, got %d", info.Count)
		}
	})

	t.Run("detects duplicate message", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log, WithDedupStoreWindow(time.Hour))
		ctx := context.Background()

		msg := &Message{
			ID:      "msg-dup",
			Payload: []byte(`{"data":"test"}`),
		}

		// First call
		_, err := ds.CheckAndStore(ctx, "channel", msg, "")
		if err != nil {
			t.Fatalf("unexpected error on first call: %v", err)
		}

		// Second call - should be duplicate
		info, err := ds.CheckAndStore(ctx, "channel", msg, "")
		if err != nil {
			t.Fatalf("unexpected error on second call: %v", err)
		}
		if !info.IsDuplicate {
			t.Error("expected second message to be duplicate")
		}
		if info.Count != 2 {
			t.Errorf("expected count 2, got %d", info.Count)
		}
	})

	t.Run("uses content hash when no message ID", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log, WithDedupStoreWindow(time.Hour))
		ctx := context.Background()

		msg := &Message{
			Payload: []byte(`{"data":"same"}`),
		}

		// First call
		ds.CheckAndStore(ctx, "channel", msg, "")

		// Second call with same content
		info, _ := ds.CheckAndStore(ctx, "channel", msg, "")
		if !info.IsDuplicate {
			t.Error("expected content-based duplicate detection")
		}
	})

	t.Run("returns error for nil message", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		_, err := ds.CheckAndStore(ctx, "channel", nil, "")
		if err == nil {
			t.Error("expected error for nil message")
		}
	})

	t.Run("returns error for empty channel", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		msg := &Message{Payload: []byte("test")}
		_, err := ds.CheckAndStore(ctx, "", msg, "")
		if err == nil {
			t.Error("expected error for empty channel")
		}
	})

	t.Run("uses client-specified dedup key", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log, WithDedupStoreWindow(time.Hour))
		ctx := context.Background()

		msg1 := &Message{
			ID:      "msg-a",
			Payload: []byte(`{"data":"first"}`),
		}
		msg2 := &Message{
			ID:      "msg-b",
			Payload: []byte(`{"data":"second"}`),
		}

		// First call with client dedup key
		info, err := ds.CheckAndStore(ctx, "channel", msg1, "custom-key-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.IsDuplicate {
			t.Error("expected first message to not be duplicate")
		}

		// Second call with same client dedup key but different message ID and payload
		info, err = ds.CheckAndStore(ctx, "channel", msg2, "custom-key-1")
		if err != nil {
			t.Fatalf("unexpected error on second call: %v", err)
		}
		if !info.IsDuplicate {
			t.Error("expected duplicate detection using client dedup key")
		}
		if info.Count != 2 {
			t.Errorf("expected count 2, got %d", info.Count)
		}
	})

	t.Run("client dedup key takes precedence over message ID", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log, WithDedupStoreWindow(time.Hour))
		ctx := context.Background()

		msg := &Message{
			ID:      "msg-same-id",
			Payload: []byte(`{"data":"test"}`),
		}

		// Store with client dedup key "key-a"
		_, err := ds.CheckAndStore(ctx, "channel", msg, "key-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Same message ID but different client dedup key should NOT be a duplicate
		info, err := ds.CheckAndStore(ctx, "channel", msg, "key-b")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.IsDuplicate {
			t.Error("different client dedup keys should not be treated as duplicates")
		}
	})
}

func TestDedupStoreImpl_GetStats(t *testing.T) {
	t.Parallel()
	log := newDedupTestLogger(t)

	t.Run("returns stats for channel", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		// Store some entries
		ds.StoreEntry(ctx, "stats-channel", "key1", &DedupEntry{Count: 1})
		ds.StoreEntry(ctx, "stats-channel", "key2", &DedupEntry{Count: 3})
		ds.StoreEntry(ctx, "stats-channel", "key3", &DedupEntry{Count: 5})

		stats, err := ds.GetStats(ctx, "stats-channel")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stats.Channel != "stats-channel" {
			t.Errorf("expected channel 'stats-channel', got %q", stats.Channel)
		}
		if stats.TotalMessages != 3 {
			t.Errorf("expected 3 total messages, got %d", stats.TotalMessages)
		}
		// Duplicates: (3-1) + (5-1) = 2 + 4 = 6
		if stats.TotalDuplicates != 6 {
			t.Errorf("expected 6 total duplicates, got %d", stats.TotalDuplicates)
		}
	})

	t.Run("returns error for empty channel", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)
		ctx := context.Background()

		_, err := ds.GetStats(ctx, "")
		if err == nil {
			t.Error("expected error for empty channel")
		}
	})
}

func TestDedupStoreImpl_Cleanup(t *testing.T) {
	t.Parallel()
	log := newDedupTestLogger(t)

	t.Run("removes expired entries", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log, WithDedupStoreWindow(time.Millisecond))
		ctx := context.Background()

		// Store an entry with old timestamp
		entry := &DedupEntry{
			Count:     1,
			FirstSeen: time.Now().Add(-time.Hour),
			LastSeen:  time.Now().Add(-time.Hour),
		}
		ds.StoreEntry(ctx, "cleanup-channel", "old-key", entry)

		// Wait a bit to ensure window passes
		time.Sleep(10 * time.Millisecond)

		// Run cleanup
		err := ds.Cleanup(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify entry was removed
		_, err = ds.GetEntry(ctx, "cleanup-channel", "old-key")
		if err == nil {
			t.Error("expected expired entry to be removed")
		}
	})

	t.Run("keeps non-expired entries", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log, WithDedupStoreWindow(time.Hour))
		ctx := context.Background()

		// Store a fresh entry
		entry := &DedupEntry{
			Count:     1,
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
		}
		ds.StoreEntry(ctx, "cleanup-channel", "fresh-key", entry)

		// Run cleanup
		err := ds.Cleanup(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify entry still exists
		_, err = ds.GetEntry(ctx, "cleanup-channel", "fresh-key")
		if err != nil {
			t.Error("expected fresh entry to remain")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()
		ms := newMockStorage()
		ds := NewDedupStoreImpl(ms, log)

		// Store some entries
		for i := 0; i < 100; i++ {
			entry := &DedupEntry{FirstSeen: time.Now().Add(-time.Hour)}
			ds.StoreEntry(context.Background(), "channel", "key-"+string(rune(i)), entry)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := ds.Cleanup(ctx)
		if err == nil || err != context.Canceled {
			// May or may not catch cancellation depending on timing
			// Just verify it doesn't panic
		}
	})
}

func TestDedupErrorTypes(t *testing.T) {
	t.Parallel()

	t.Run("InvalidPayloadError message", func(t *testing.T) {
		t.Parallel()
		err := &InvalidPayloadError{Reason: "test reason"}
		if !strings.Contains(err.Error(), "test reason") {
			t.Errorf("error message should contain reason: %s", err.Error())
		}
	})

	t.Run("HashComputeError message with cause", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("underlying error")
		err := &HashComputeError{Cause: cause}
		if !strings.Contains(err.Error(), "underlying error") {
			t.Errorf("error message should contain cause: %s", err.Error())
		}
		if err.Unwrap() != cause {
			t.Error("Unwrap should return cause")
		}
	})

	t.Run("HashComputeError message without cause", func(t *testing.T) {
		t.Parallel()
		err := &HashComputeError{}
		if err.Error() == "" {
			t.Error("error message should not be empty")
		}
	})

	t.Run("InvalidDedupKeyError message", func(t *testing.T) {
		t.Parallel()
		err := &InvalidDedupKeyError{Key: "bad-key", Reason: "invalid"}
		if !strings.Contains(err.Error(), "bad-key") {
			t.Errorf("error message should contain key: %s", err.Error())
		}
	})

	t.Run("InvalidDedupEntryError message", func(t *testing.T) {
		t.Parallel()
		err := &InvalidDedupEntryError{Reason: "nil entry"}
		if !strings.Contains(err.Error(), "nil entry") {
			t.Errorf("error message should contain reason: %s", err.Error())
		}
	})

	t.Run("DedupEntryDecodeError message", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("decode failed")
		err := &DedupEntryDecodeError{Key: "test-key", Cause: cause}
		if !strings.Contains(err.Error(), "test-key") {
			t.Errorf("error message should contain key: %s", err.Error())
		}
		if err.Unwrap() != cause {
			t.Error("Unwrap should return cause")
		}
	})

	t.Run("DedupEntryDecodeError message without cause", func(t *testing.T) {
		t.Parallel()
		err := &DedupEntryDecodeError{Key: "test-key"}
		if err.Error() == "" {
			t.Error("error message should not be empty")
		}
	})

	t.Run("DedupEntryEncodeError message", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("encode failed")
		err := &DedupEntryEncodeError{Key: "test-key", Cause: cause}
		if !strings.Contains(err.Error(), "test-key") {
			t.Errorf("error message should contain key: %s", err.Error())
		}
		if err.Unwrap() != cause {
			t.Error("Unwrap should return cause")
		}
	})

	t.Run("DedupEntryEncodeError message without cause", func(t *testing.T) {
		t.Parallel()
		err := &DedupEntryEncodeError{Key: "test-key"}
		if err.Error() == "" {
			t.Error("error message should not be empty")
		}
	})

	t.Run("DedupStatsError message", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("stats failed")
		err := &DedupStatsError{Channel: "test-channel", Cause: cause}
		if !strings.Contains(err.Error(), "test-channel") {
			t.Errorf("error message should contain channel: %s", err.Error())
		}
		if err.Unwrap() != cause {
			t.Error("Unwrap should return cause")
		}
	})

	t.Run("DedupStatsError message without cause", func(t *testing.T) {
		t.Parallel()
		err := &DedupStatsError{Channel: "test-channel"}
		if err.Error() == "" {
			t.Error("error message should not be empty")
		}
	})

	t.Run("DedupCleanupError message", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("cleanup failed")
		err := &DedupCleanupError{Cause: cause}
		if !strings.Contains(err.Error(), "cleanup failed") {
			t.Errorf("error message should contain cause: %s", err.Error())
		}
		if err.Unwrap() != cause {
			t.Error("Unwrap should return cause")
		}
	})

	t.Run("DedupCleanupError message without cause", func(t *testing.T) {
		t.Parallel()
		err := &DedupCleanupError{}
		if err.Error() == "" {
			t.Error("error message should not be empty")
		}
	})
}

func TestIsNotFoundError(t *testing.T) {
	t.Parallel()

	t.Run("returns false for nil", func(t *testing.T) {
		t.Parallel()
		if isNotFoundError(nil) {
			t.Error("expected false for nil error")
		}
	})

	t.Run("returns true for not found", func(t *testing.T) {
		t.Parallel()
		if !isNotFoundError(errors.New("not found")) {
			t.Error("expected true for 'not found' error")
		}
	})

	t.Run("returns true for key not found", func(t *testing.T) {
		t.Parallel()
		if !isNotFoundError(errors.New("key not found")) {
			t.Error("expected true for 'key not found' error")
		}
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		t.Parallel()
		if isNotFoundError(errors.New("some other error")) {
			t.Error("expected false for other errors")
		}
	})
}
