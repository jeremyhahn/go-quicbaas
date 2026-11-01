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

package watch

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewManager verifies manager initialization.
func TestNewManager(t *testing.T) {
	mgr := NewManager()
	require.NotNil(t, mgr)

	// Verify all shards are initialized
	for i := range mgr.shards {
		require.NotNil(t, mgr.shards[i])
		require.NotNil(t, mgr.shards[i].exactWatches)
		require.NotNil(t, mgr.shards[i].prefixWatches)
	}

	// Verify initial state
	assert.Equal(t, uint64(0), mgr.idSeq)
	assert.Equal(t, uint64(0), mgr.watchCount)

	// Verify event pool
	event := mgr.eventPool.Get().(*WatchEvent)
	require.NotNil(t, event)
	mgr.eventPool.Put(event)
}

// TestWithPrefix verifies the WithPrefix option.
func TestWithPrefix(t *testing.T) {
	opts := &watchOptions{}
	assert.False(t, opts.prefix)

	// Apply option
	fn := WithPrefix()
	fn(opts)

	assert.True(t, opts.prefix)
}

// TestGetPrefix verifies prefix range calculation.
func TestGetPrefix(t *testing.T) {
	tests := []struct {
		name     string
		prefix   []byte
		expected []byte
	}{
		{
			name:     "empty prefix",
			prefix:   []byte{},
			expected: []byte{},
		},
		{
			name:     "simple prefix",
			prefix:   []byte("foo/"),
			expected: []byte("foo0"),
		},
		{
			name:     "config prefix",
			prefix:   []byte("/config/"),
			expected: []byte("/config0"),
		},
		{
			name:     "single char",
			prefix:   []byte("a"),
			expected: []byte("b"),
		},
		{
			name:     "all 0xff bytes",
			prefix:   []byte{0xff, 0xff, 0xff},
			expected: []byte{},
		},
		{
			name:     "mixed bytes ending in 0xfe",
			prefix:   []byte{0x01, 0x02, 0xfe},
			expected: []byte{0x01, 0x02, 0xff},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getPrefix(tt.prefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFnv1a verifies hash function distribution.
func TestFnv1a(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{name: "simple key", key: []byte("test")},
		{name: "empty key", key: []byte{}},
		{name: "long key", key: []byte("very-long-key-name-for-testing")},
		{name: "numeric key", key: []byte("12345")},
		{name: "path key", key: []byte("/config/server/port")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := fnv1a(tt.key)
			// Hash should be in valid range [0, 255]
			assert.LessOrEqual(t, hash, uint8(255))
		})
	}

	// Verify same key produces same hash
	key := []byte("test-key")
	hash1 := fnv1a(key)
	hash2 := fnv1a(key)
	assert.Equal(t, hash1, hash2)

	// Verify different keys produce different hashes (usually)
	key2 := []byte("test-key2")
	hash3 := fnv1a(key2)
	// While collision is possible, these specific keys should differ
	assert.NotEqual(t, hash1, hash3)
}

// TestGetShard verifies shard selection.
func TestGetShard(t *testing.T) {
	mgr := NewManager()

	key := []byte("test-key")
	shard := mgr.getShard(key)
	require.NotNil(t, shard)

	// Verify same key always returns same shard
	shard2 := mgr.getShard(key)
	assert.Equal(t, shard, shard2)

	// Verify shard is one of the manager's shards
	found := false
	for i := range mgr.shards {
		if mgr.shards[i] == shard {
			found = true
			break
		}
	}
	assert.True(t, found, "returned shard should be from manager's shard array")
}

// TestMatchesRange verifies key range matching logic.
func TestMatchesRange(t *testing.T) {
	mgr := NewManager()

	tests := []struct {
		name     string
		key      []byte
		start    []byte
		end      []byte
		expected bool
	}{
		{
			name:     "exact match with no range",
			key:      []byte("foo"),
			start:    []byte("foo"),
			end:      nil,
			expected: true,
		},
		{
			name:     "key before range",
			key:      []byte("aaa"),
			start:    []byte("bbb"),
			end:      []byte("zzz"),
			expected: false,
		},
		{
			name:     "key after range",
			key:      []byte("zzz"),
			start:    []byte("aaa"),
			end:      []byte("bbb"),
			expected: false,
		},
		{
			name:     "key within range",
			key:      []byte("mmm"),
			start:    []byte("aaa"),
			end:      []byte("zzz"),
			expected: true,
		},
		{
			name:     "key at start of range",
			key:      []byte("aaa"),
			start:    []byte("aaa"),
			end:      []byte("zzz"),
			expected: true,
		},
		{
			name:     "key at end of range (exclusive)",
			key:      []byte("zzz"),
			start:    []byte("aaa"),
			end:      []byte("zzz"),
			expected: false,
		},
		{
			name:     "prefix match - /config/server in /config/",
			key:      []byte("/config/server"),
			start:    []byte("/config/"),
			end:      []byte("/config0"),
			expected: true,
		},
		{
			name:     "prefix match - /config/db in /config/",
			key:      []byte("/config/db"),
			start:    []byte("/config/"),
			end:      []byte("/config0"),
			expected: true,
		},
		{
			name:     "prefix no match - /data/x not in /config/",
			key:      []byte("/data/x"),
			start:    []byte("/config/"),
			end:      []byte("/config0"),
			expected: false,
		},
		{
			name:     "empty range end matches all after start",
			key:      []byte("zzz"),
			start:    []byte("aaa"),
			end:      []byte{}, // empty but non-nil
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mgr.matchesRange(tt.key, tt.start, tt.end)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestAdd verifies that Add and addInternal register exact and prefix watches
// in the key's shard with sequential IDs and the given filters.
func TestAdd(t *testing.T) {
	tests := []struct {
		name     string
		key      []byte
		rangeEnd []byte
		filters  WatchFilters
		count    int
		public   bool
	}{
		{name: "exact watch", key: []byte("test-key"), count: 1},
		{name: "prefix watch", key: []byte("/config/"), rangeEnd: []byte("/config0"), count: 1},
		{name: "multiple watches same key", key: []byte("test-key"), count: 2},
		{name: "filters kept", key: []byte("key"), filters: WatchFilters{NoPut: true}, count: 1},
		{name: "public Add starts delivery", key: []byte("key"), count: 1, public: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager()
			stream := newMockWatchStream()

			var added []*Watch
			for i := 0; i < tt.count; i++ {
				var watch *Watch
				var err error
				if tt.public {
					watch, err = mgr.Add(tt.key, tt.rangeEnd, 0, tt.filters, stream)
				} else {
					watch, err = mgr.addInternal(tt.key, tt.rangeEnd, 0, tt.filters, stream, &watchOptions{}, true)
				}
				require.NoError(t, err)
				require.NotNil(t, watch)
				added = append(added, watch)
			}

			for i, watch := range added {
				assert.Equal(t, uint64(i+1), watch.ID)
				assert.Equal(t, tt.key, watch.Key)
				assert.Equal(t, tt.rangeEnd, watch.RangeEnd)
				assert.Equal(t, int64(0), watch.StartRev)
				assert.Equal(t, tt.filters, watch.Filters)
				assert.NotNil(t, watch.Events)
				assert.Equal(t, !tt.public, watch.skipDeliver)
			}

			shard := mgr.getShard(tt.key)
			shard.mu.RLock()
			registered := shard.exactWatches[string(tt.key)]
			if tt.rangeEnd != nil {
				registered = shard.prefixWatches
			}
			registered = append([]*Watch(nil), registered...)
			shard.mu.RUnlock()

			assert.Equal(t, added, registered)
			assert.Equal(t, uint64(tt.count), atomic.LoadUint64(&mgr.watchCount))

			if tt.public {
				for _, watch := range added {
					watch.cancel()
				}
			}
		})
	}
}

// TestRemove verifies that Remove drops only the named watch from its shard,
// and that an unknown ID leaves every watch in place.
func TestRemove(t *testing.T) {
	tests := []struct {
		name     string
		key      []byte
		rangeEnd []byte
		count    int
		remove   int // index into the added watches; -1 removes an unknown ID
		wantLeft []int
	}{
		{name: "exact watch", key: []byte("test-key"), count: 1, remove: 0},
		{name: "prefix watch", key: []byte("/config/"), rangeEnd: []byte("/config0"), count: 1, remove: 0},
		{name: "one of multiple watches", key: []byte("test-key"), count: 3, remove: 1, wantLeft: []int{0, 2}},
		{name: "unknown watch ID", key: []byte("test-key"), count: 1, remove: -1, wantLeft: []int{0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager()
			stream := newMockWatchStream()

			var added []*Watch
			for i := 0; i < tt.count; i++ {
				watch, err := mgr.addInternal(tt.key, tt.rangeEnd, 0, WatchFilters{}, stream, &watchOptions{}, true)
				require.NoError(t, err)
				added = append(added, watch)
			}

			removeID := uint64(999)
			if tt.remove >= 0 {
				removeID = added[tt.remove].ID
			}
			mgr.Remove(removeID)

			shard := mgr.getShard(tt.key)
			shard.mu.RLock()
			remaining := shard.exactWatches[string(tt.key)]
			if tt.rangeEnd != nil {
				remaining = shard.prefixWatches
			}
			var remainingIDs []uint64
			for _, w := range remaining {
				remainingIDs = append(remainingIDs, w.ID)
			}
			shard.mu.RUnlock()

			var wantIDs []uint64
			for _, i := range tt.wantLeft {
				wantIDs = append(wantIDs, added[i].ID)
			}
			assert.Equal(t, wantIDs, remainingIDs)
			assert.Equal(t, uint64(len(tt.wantLeft)), atomic.LoadUint64(&mgr.watchCount))
			if tt.remove >= 0 {
				assert.Error(t, added[tt.remove].ctx.Err(), "removed watch context must be cancelled")
			}
		})
	}
}

// TestSendEvent_WithFilters verifies event filtering.
func TestSendEvent_WithFilters(t *testing.T) {
	mgr := NewManager()

	tests := []struct {
		name       string
		filters    WatchFilters
		eventType  EventType
		shouldSend bool
	}{
		{
			name:       "no filters - PUT event",
			filters:    WatchFilters{},
			eventType:  PUT,
			shouldSend: true,
		},
		{
			name:       "no filters - DELETE event",
			filters:    WatchFilters{},
			eventType:  DELETE,
			shouldSend: true,
		},
		{
			name:       "filter PUT - PUT event",
			filters:    WatchFilters{NoPut: true},
			eventType:  PUT,
			shouldSend: false,
		},
		{
			name:       "filter PUT - DELETE event",
			filters:    WatchFilters{NoPut: true},
			eventType:  DELETE,
			shouldSend: true,
		},
		{
			name:       "filter DELETE - DELETE event",
			filters:    WatchFilters{NoDelete: true},
			eventType:  DELETE,
			shouldSend: false,
		},
		{
			name:       "filter DELETE - PUT event",
			filters:    WatchFilters{NoDelete: true},
			eventType:  PUT,
			shouldSend: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			watch := &Watch{
				ID:      1,
				Filters: tt.filters,
				Events:  make(chan *WatchEvent, 10),
				ctx:     ctx,
				cancel:  cancel,
			}

			event := &WatchEvent{
				Type: tt.eventType,
				Kv: &KeyValue{
					Key:   []byte("key"),
					Value: []byte("value"),
				},
			}

			mgr.sendEvent(watch, event)

			// Check if event was sent
			select {
			case <-watch.Events:
				if !tt.shouldSend {
					t.Error("Event was sent when it should have been filtered")
				}
			default:
				if tt.shouldSend {
					t.Error("Event was not sent when it should have been")
				}
			}
		})
	}
}

// TestSendEvent_ContextCancelled verifies handling of cancelled context.
func TestSendEvent_ContextCancelled(t *testing.T) {
	mgr := NewManager()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	watch := &Watch{
		ID:      1,
		Filters: WatchFilters{},
		Events:  make(chan *WatchEvent, 1),
		ctx:     ctx,
		cancel:  cancel,
	}

	event := &WatchEvent{
		Type: PUT,
		Kv: &KeyValue{
			Key:   []byte("key"),
			Value: []byte("value"),
		},
	}

	// Should not panic or block - this is the important guarantee
	// The implementation uses a select, so there's a race:
	// - It might send to channel (buffered, so doesn't block)
	// - It might detect cancelled context first
	// Both outcomes are acceptable as long as it doesn't block
	assert.NotPanics(t, func() {
		mgr.sendEvent(watch, event)
	})
}

// TestNotifyPut_NoWatches verifies fast path when no watches exist.
func TestNotifyPut_NoWatches(t *testing.T) {
	mgr := NewManager()

	// Should return immediately without error
	assert.NotPanics(t, func() {
		mgr.NotifyPut([]byte("key"), nil, []byte("value"), 1, 1)
	})

	// Verify watchCount is still 0
	assert.Equal(t, uint64(0), mgr.watchCount)
}

// TestNotifyDelete_NoWatches verifies fast path when no watches exist.
func TestNotifyDelete_NoWatches(t *testing.T) {
	mgr := NewManager()

	// Should return immediately without error
	assert.NotPanics(t, func() {
		mgr.NotifyDelete([]byte("key"), []byte("prev-value"), 1, 1)
	})

	assert.Equal(t, uint64(0), mgr.watchCount)
}

// TestNotify_ExactMatch verifies exact key notification.
func TestNotify_ExactMatch(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := &mockWatchStream{ctx: ctx}
	watch, err := mgr.addInternal([]byte("test-key"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
	require.NoError(t, err)

	// Notify PUT
	mgr.NotifyPut([]byte("test-key"), nil, []byte("test-value"), 1, 1)

	// Check event received
	select {
	case event := <-watch.Events:
		assert.Equal(t, PUT, event.Type)
		assert.Equal(t, []byte("test-key"), event.Kv.Key)
		assert.Equal(t, []byte("test-value"), event.Kv.Value)
		assert.Equal(t, int64(1), event.Revision)
		assert.Equal(t, int64(1), event.Kv.ModRevision)
	default:
		t.Error("Expected event not received")
	}
}

// TestNotify_PrefixMatch verifies prefix key notification.
func TestNotify_PrefixMatch(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := &mockWatchStream{ctx: ctx}
	prefix := []byte("/config/")
	rangeEnd := getPrefix(prefix)

	watch, err := mgr.addInternal(prefix, rangeEnd, 0, WatchFilters{}, stream, &watchOptions{}, true)
	require.NoError(t, err)

	// Notify PUT for key matching prefix
	mgr.NotifyPut([]byte("/config/server"), nil, []byte("localhost"), 1, 1)

	// Check event received
	select {
	case event := <-watch.Events:
		assert.Equal(t, PUT, event.Type)
		assert.Equal(t, []byte("/config/server"), event.Kv.Key)
		assert.Equal(t, []byte("localhost"), event.Kv.Value)
	default:
		t.Error("Expected event not received")
	}
}

// TestNotify_NoMatch verifies non-matching keys don't trigger events.
func TestNotify_NoMatch(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := &mockWatchStream{ctx: ctx}
	watch, err := mgr.addInternal([]byte("key-a"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
	require.NoError(t, err)

	// Notify for different key
	mgr.NotifyPut([]byte("key-b"), nil, []byte("value"), 1, 1)

	// Should not receive event
	select {
	case <-watch.Events:
		t.Error("Should not receive event for non-matching key")
	default:
		// Expected
	}
}

// TestNotify_DeleteEvent verifies DELETE event handling.
func TestNotify_DeleteEvent(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := &mockWatchStream{ctx: ctx}
	watch, err := mgr.addInternal([]byte("test-key"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
	require.NoError(t, err)

	// Notify DELETE
	mgr.NotifyDelete([]byte("test-key"), []byte("old-value"), 2, 2)

	// Check event received
	select {
	case event := <-watch.Events:
		assert.Equal(t, DELETE, event.Type)
		assert.Equal(t, []byte("test-key"), event.Kv.Key)
		assert.Nil(t, event.Kv.Value) // DELETE events have nil value
		assert.NotNil(t, event.PrevKv)
		assert.Equal(t, []byte("old-value"), event.PrevKv.Value)
		assert.Equal(t, int64(2), event.Revision)
	default:
		t.Error("Expected event not received")
	}
}

// TestGetStats verifies statistics collection.
func TestGetStats(t *testing.T) {
	mgr := NewManager()
	stream := newMockWatchStream()

	// Initially no watches
	stats := mgr.GetStats()
	assert.Equal(t, 0, stats.TotalWatches)
	assert.Equal(t, 0, stats.ExactWatches)
	assert.Equal(t, 0, stats.PrefixWatches)

	// Add exact watches
	mgr.addInternal([]byte("key1"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
	mgr.addInternal([]byte("key2"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)

	stats = mgr.GetStats()
	assert.Equal(t, 2, stats.TotalWatches)
	assert.Equal(t, 2, stats.ExactWatches)
	assert.Equal(t, 0, stats.PrefixWatches)

	// Add prefix watch
	mgr.addInternal([]byte("/config/"), []byte("/config0"), 0, WatchFilters{}, stream, &watchOptions{}, true)

	stats = mgr.GetStats()
	assert.Equal(t, 3, stats.TotalWatches)
	assert.Equal(t, 2, stats.ExactWatches)
	assert.Equal(t, 1, stats.PrefixWatches)
}

// TestNoopStream verifies noop stream implementation.
func TestNoopStream(t *testing.T) {
	ctx := context.Background()
	stream := &noopStream{ctx: ctx}

	// Send should not error
	err := stream.Send(&WatchResponse{})
	assert.NoError(t, err)

	// Context should return correct context
	assert.Equal(t, ctx, stream.Context())
}

// TestWatchContextCancellation verifies watch cleanup on context cancel.
func TestWatchContextCancellation(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())

	stream := &mockWatchStream{ctx: ctx}
	watch, err := mgr.addInternal([]byte("key"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
	require.NoError(t, err)

	// Cancel context
	cancel()

	// Watch context should be cancelled
	select {
	case <-watch.ctx.Done():
		// Expected
	default:
		t.Error("Watch context should be cancelled")
	}
}

// TestConcurrentAddRemove verifies thread safety of add/remove operations.
func TestConcurrentAddRemove(t *testing.T) {
	mgr := NewManager()
	stream := newMockWatchStream()

	var wg sync.WaitGroup
	watchIDs := make(chan uint64, 100)

	// Concurrent adds
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := []byte("key")
			watch, err := mgr.addInternal(key, nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
			if err == nil {
				watchIDs <- watch.ID
			}
		}(i)
	}

	wg.Wait()
	close(watchIDs)

	// Verify all watches added
	addedCount := 0
	ids := []uint64{}
	for id := range watchIDs {
		ids = append(ids, id)
		addedCount++
	}
	assert.Equal(t, 50, addedCount)
	assert.Equal(t, uint64(50), mgr.watchCount)

	// Concurrent removes
	for _, id := range ids {
		wg.Add(1)
		go func(watchID uint64) {
			defer wg.Done()
			mgr.Remove(watchID)
		}(id)
	}

	wg.Wait()

	// Verify all watches removed
	assert.Equal(t, uint64(0), mgr.watchCount)
}

// TestEventPool verifies event pooling reduces allocations.
func TestEventPool(t *testing.T) {
	mgr := NewManager()

	// Get event from pool
	event1 := mgr.eventPool.Get().(*WatchEvent)
	require.NotNil(t, event1)

	// Modify and return to pool
	event1.Type = PUT
	event1.Revision = 123
	mgr.eventPool.Put(event1)

	// Get another event - might be the same one (pool behavior)
	event2 := mgr.eventPool.Get().(*WatchEvent)
	require.NotNil(t, event2)

	// Both should be valid WatchEvent instances
	assert.IsType(t, &WatchEvent{}, event1)
	assert.IsType(t, &WatchEvent{}, event2)
}

// TestWatchFilters verifies filter struct.
func TestWatchFilters(t *testing.T) {
	filters := WatchFilters{
		NoPut:    true,
		NoDelete: false,
	}

	assert.True(t, filters.NoPut)
	assert.False(t, filters.NoDelete)
}
