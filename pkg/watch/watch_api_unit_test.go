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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWatch_ExactKey verifies watching exact keys.
func TestWatch_ExactKey(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start watching
	eventCh, err := mgr.Watch(ctx, "test-key")
	require.NoError(t, err)
	require.NotNil(t, eventCh)

	// Trigger event in background
	go func() {
		time.Sleep(10 * time.Millisecond)
		mgr.NotifyPut([]byte("test-key"), nil, []byte("test-value"), 1, 1)
	}()

	// Receive event
	select {
	case event := <-eventCh:
		assert.Equal(t, EventTypePut, event.Type)
		assert.Equal(t, []byte("test-key"), event.Key)
		assert.Equal(t, []byte("test-value"), event.Value)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for event")
	}
}

// TestWatch_WithPrefix verifies prefix watching.
func TestWatch_WithPrefix(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start watching with prefix
	eventCh, err := mgr.Watch(ctx, "/config/", WithPrefix())
	require.NoError(t, err)
	require.NotNil(t, eventCh)

	// Trigger event in background
	go func() {
		time.Sleep(10 * time.Millisecond)
		mgr.NotifyPut([]byte("/config/server"), nil, []byte("localhost"), 1, 1)
	}()

	// Receive event
	select {
	case event := <-eventCh:
		assert.Equal(t, EventTypePut, event.Type)
		assert.Equal(t, []byte("/config/server"), event.Key)
		assert.Equal(t, []byte("localhost"), event.Value)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for event")
	}
}

// TestWatch_DeleteEvent verifies DELETE event conversion.
func TestWatch_DeleteEvent(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start watching
	eventCh, err := mgr.Watch(ctx, "test-key")
	require.NoError(t, err)

	// Trigger DELETE event
	go func() {
		time.Sleep(10 * time.Millisecond)
		mgr.NotifyDelete([]byte("test-key"), []byte("old-value"), 2, 2)
	}()

	// Receive event
	select {
	case event := <-eventCh:
		assert.Equal(t, EventTypeDelete, event.Type)
		assert.Equal(t, []byte("test-key"), event.Key)
		assert.Equal(t, []byte("old-value"), event.Value) // Previous value
	case <-ctx.Done():
		t.Fatal("Timeout waiting for event")
	}
}

// TestWatch_ContextCancellation verifies cleanup on cancel.
func TestWatch_ContextCancellation(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())

	// Start watching
	eventCh, err := mgr.Watch(ctx, "test-key")
	require.NoError(t, err)

	// Cancel context
	cancel()

	// Channel should close
	timeout := time.After(1 * time.Second)
	select {
	case _, ok := <-eventCh:
		if ok {
			t.Error("Channel should be closed")
		}
		// Expected - channel closed
	case <-timeout:
		t.Fatal("Timeout waiting for channel close")
	}
}

// TestWatch_MultipleEvents verifies handling multiple events.
func TestWatch_MultipleEvents(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start watching
	eventCh, err := mgr.Watch(ctx, "/data/", WithPrefix())
	require.NoError(t, err)

	// Send multiple events
	go func() {
		time.Sleep(10 * time.Millisecond)
		mgr.NotifyPut([]byte("/data/key1"), nil, []byte("value1"), 1, 1)
		mgr.NotifyPut([]byte("/data/key2"), nil, []byte("value2"), 2, 2)
		mgr.NotifyDelete([]byte("/data/key1"), []byte("value1"), 3, 3)
	}()

	// Receive events
	receivedEvents := []Event{}
	timeout := time.After(500 * time.Millisecond)

	for i := 0; i < 3; i++ {
		select {
		case event := <-eventCh:
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			t.Fatalf("Timeout waiting for event %d", i+1)
		}
	}

	require.Len(t, receivedEvents, 3)

	// Verify events
	assert.Equal(t, EventTypePut, receivedEvents[0].Type)
	assert.Equal(t, []byte("/data/key1"), receivedEvents[0].Key)

	assert.Equal(t, EventTypePut, receivedEvents[1].Type)
	assert.Equal(t, []byte("/data/key2"), receivedEvents[1].Key)

	assert.Equal(t, EventTypeDelete, receivedEvents[2].Type)
	assert.Equal(t, []byte("/data/key1"), receivedEvents[2].Key)
}

// TestConvertEvents_PutEvent verifies PUT event conversion.
func TestConvertEvents_PutEvent(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create watch manually to test convertEvents
	stream := &noopStream{ctx: ctx}
	watch, err := mgr.addInternal([]byte("key"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
	require.NoError(t, err)

	eventCh := make(chan Event, 10)

	// Start convertEvents
	go mgr.convertEvents(watch, eventCh)

	// Send WatchEvent
	watchEvent := &WatchEvent{
		Type: PUT,
		Kv: &KeyValue{
			Key:   []byte("key"),
			Value: []byte("value"),
		},
		Revision: 1,
	}
	watch.Events <- watchEvent

	// Receive converted event
	timeout := time.After(500 * time.Millisecond)
	select {
	case event := <-eventCh:
		assert.Equal(t, EventTypePut, event.Type)
		assert.Equal(t, []byte("key"), event.Key)
		assert.Equal(t, []byte("value"), event.Value)
	case <-timeout:
		t.Fatal("Timeout waiting for converted event")
	}
}

// TestConvertEvents_DeleteEventWithPrevValue verifies DELETE with previous value.
func TestConvertEvents_DeleteEventWithPrevValue(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := &noopStream{ctx: ctx}
	watch, err := mgr.addInternal([]byte("key"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
	require.NoError(t, err)

	eventCh := make(chan Event, 10)
	go mgr.convertEvents(watch, eventCh)

	// Send DELETE with PrevKv
	watchEvent := &WatchEvent{
		Type: DELETE,
		Kv: &KeyValue{
			Key:   []byte("key"),
			Value: nil,
		},
		PrevKv: &KeyValue{
			Key:   []byte("key"),
			Value: []byte("old-value"),
		},
		Revision: 2,
	}
	watch.Events <- watchEvent

	timeout := time.After(500 * time.Millisecond)
	select {
	case event := <-eventCh:
		assert.Equal(t, EventTypeDelete, event.Type)
		assert.Equal(t, []byte("key"), event.Key)
		assert.Equal(t, []byte("old-value"), event.Value)
	case <-timeout:
		t.Fatal("Timeout waiting for converted event")
	}
}

// TestConvertEvents_DeleteEventNoPrevValue verifies DELETE without previous value.
func TestConvertEvents_DeleteEventNoPrevValue(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := &noopStream{ctx: ctx}
	watch, err := mgr.addInternal([]byte("key"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
	require.NoError(t, err)

	eventCh := make(chan Event, 10)
	go mgr.convertEvents(watch, eventCh)

	// Send DELETE without PrevKv
	watchEvent := &WatchEvent{
		Type: DELETE,
		Kv: &KeyValue{
			Key:   []byte("key"),
			Value: nil,
		},
		PrevKv:   nil,
		Revision: 2,
	}
	watch.Events <- watchEvent

	timeout := time.After(500 * time.Millisecond)
	select {
	case event := <-eventCh:
		assert.Equal(t, EventTypeDelete, event.Type)
		assert.Equal(t, []byte("key"), event.Key)
		assert.Nil(t, event.Value)
	case <-timeout:
		t.Fatal("Timeout waiting for converted event")
	}
}

// TestConvertEvents_ContextCancellation verifies cleanup on context cancel.
func TestConvertEvents_ContextCancellation(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())

	stream := &noopStream{ctx: ctx}
	watch, err := mgr.addInternal([]byte("key"), nil, 0, WatchFilters{}, stream, &watchOptions{}, true)
	require.NoError(t, err)

	eventCh := make(chan Event, 10)
	go mgr.convertEvents(watch, eventCh)

	// Cancel context
	cancel()

	// Channel should close
	timeout := time.After(500 * time.Millisecond)
	select {
	case _, ok := <-eventCh:
		if ok {
			t.Error("Channel should be closed")
		}
	case <-timeout:
		t.Fatal("Timeout waiting for channel close")
	}
}

// TestWatch_NoMatchingEvents verifies no events for non-matching keys.
func TestWatch_NoMatchingEvents(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Watch specific key
	eventCh, err := mgr.Watch(ctx, "/config/server")
	require.NoError(t, err)

	// Notify different key
	go func() {
		time.Sleep(10 * time.Millisecond)
		mgr.NotifyPut([]byte("/config/client"), nil, []byte("value"), 1, 1)
	}()

	// Should not receive event
	select {
	case event := <-eventCh:
		t.Errorf("Should not receive event for non-matching key, got: %+v", event)
	case <-ctx.Done():
		// Expected - timeout
	}
}

// TestWatch_EmptyPrefix verifies empty prefix watches all keys.
func TestWatch_EmptyPrefix(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Watch with empty prefix (should match everything)
	eventCh, err := mgr.Watch(ctx, "", WithPrefix())
	require.NoError(t, err)

	// Notify any key
	go func() {
		time.Sleep(10 * time.Millisecond)
		mgr.NotifyPut([]byte("any-key"), nil, []byte("any-value"), 1, 1)
	}()

	// Should receive event
	select {
	case event := <-eventCh:
		assert.Equal(t, EventTypePut, event.Type)
		assert.Equal(t, []byte("any-key"), event.Key)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for event")
	}
}

// TestEventTypes verifies event type constants.
func TestEventTypes(t *testing.T) {
	assert.Equal(t, "PUT", EventTypePut)
	assert.Equal(t, "DELETE", EventTypeDelete)
}

// TestWatchResponse_Structure verifies WatchResponse struct.
func TestWatchResponse_Structure(t *testing.T) {
	resp := &WatchResponse{
		WatchID:  123,
		Revision: 456,
		Events: []*WatchEvent{
			{
				Type: PUT,
				Kv: &KeyValue{
					Key:   []byte("key"),
					Value: []byte("value"),
				},
			},
		},
	}

	assert.Equal(t, int64(123), resp.WatchID)
	assert.Equal(t, int64(456), resp.Revision)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, PUT, resp.Events[0].Type)
}

// TestEvent_Structure verifies Event struct.
func TestEvent_Structure(t *testing.T) {
	event := Event{
		Type:  EventTypePut,
		Key:   []byte("test-key"),
		Value: []byte("test-value"),
	}

	assert.Equal(t, EventTypePut, event.Type)
	assert.Equal(t, []byte("test-key"), event.Key)
	assert.Equal(t, []byte("test-value"), event.Value)
}
