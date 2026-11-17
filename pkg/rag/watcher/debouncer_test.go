//go:build rag

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

package watcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDebouncer_Add(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	event1 := Event{
		Type: EventCreate,
		Path: "/path/to/file1.txt",
	}

	event2 := Event{
		Type: EventModify,
		Path: "/path/to/file2.txt",
	}

	d.Add(event1)
	d.Add(event2)

	assert.Equal(t, 2, d.Count())
}

func TestDebouncer_AddSamePath(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	event1 := Event{
		Type: EventCreate,
		Path: "/path/to/file.txt",
	}

	event2 := Event{
		Type: EventModify,
		Path: "/path/to/file.txt",
	}

	d.Add(event1)
	d.Add(event2)

	// Should only have one event (the latest)
	assert.Equal(t, 1, d.Count())

	events := d.Flush()
	assert.Len(t, events, 1)
	assert.Equal(t, EventModify, events[0].Type)
}

func TestDebouncer_Flush(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	event1 := Event{
		Type: EventCreate,
		Path: "/path/to/file1.txt",
	}

	event2 := Event{
		Type: EventModify,
		Path: "/path/to/file2.txt",
	}

	d.Add(event1)
	d.Add(event2)

	events := d.Flush()

	assert.Len(t, events, 2)
	assert.Equal(t, 0, d.Count())

	// Paths should be in the results (order doesn't matter)
	paths := make(map[string]bool)
	for _, e := range events {
		paths[e.Path] = true
	}

	assert.True(t, paths["/path/to/file1.txt"])
	assert.True(t, paths["/path/to/file2.txt"])
}

func TestDebouncer_FlushEmpty(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	events := d.Flush()
	assert.Nil(t, events)
	assert.Equal(t, 0, d.Count())
}

func TestDebouncer_Count(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	assert.Equal(t, 0, d.Count())

	d.Add(Event{Path: "/file1.txt"})
	assert.Equal(t, 1, d.Count())

	d.Add(Event{Path: "/file2.txt"})
	assert.Equal(t, 2, d.Count())

	d.Flush()
	assert.Equal(t, 0, d.Count())
}

func TestDebouncer_Clear(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	d.Add(Event{Path: "/file1.txt"})
	d.Add(Event{Path: "/file2.txt"})
	assert.Equal(t, 2, d.Count())

	d.Clear()
	assert.Equal(t, 0, d.Count())

	// Flush should return empty
	events := d.Flush()
	assert.Nil(t, events)
}

func TestDebouncer_ConcurrentAccess(t *testing.T) {
	d := newDebouncer(100 * time.Millisecond)

	done := make(chan bool)

	// Add events concurrently
	go func() {
		for i := 0; i < 100; i++ {
			d.Add(Event{Path: "/file1.txt"})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			d.Add(Event{Path: "/file2.txt"})
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 10; i++ {
			d.Flush()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Should not panic
	d.Clear()
}

// fakeClock is a manually advanced clock for driving the debounce window.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newClockedDebouncer returns a debouncer whose window is measured against a
// fake clock the test advances by hand.
func newClockedDebouncer(delay time.Duration) (*debouncer, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	d := newDebouncer(delay)
	d.now = clock.now
	return d, clock
}

func TestDebouncer_FlushReady_CoalescesInsideWindow(t *testing.T) {
	d, clock := newClockedDebouncer(100 * time.Millisecond)
	path := "/path/to/file.txt"

	d.Add(Event{Type: EventCreate, Path: path, Size: 1})
	clock.advance(60 * time.Millisecond)
	d.Add(Event{Type: EventModify, Path: path, Size: 2})
	clock.advance(60 * time.Millisecond)

	// 120ms after the first event but only 60ms after the last one: the
	// second event restarted the window, so nothing is ready yet.
	assert.Empty(t, d.FlushReady())
	assert.Equal(t, 1, d.Count(), "both events must have coalesced into one")
}

func TestDebouncer_FlushReady_ReadyAfterDelay(t *testing.T) {
	d, clock := newClockedDebouncer(100 * time.Millisecond)
	path := "/path/to/file.txt"

	d.Add(Event{Type: EventCreate, Path: path, Size: 1})
	clock.advance(60 * time.Millisecond)
	d.Add(Event{Type: EventModify, Path: path, Size: 2})

	clock.advance(99 * time.Millisecond)
	assert.Empty(t, d.FlushReady(), "one tick short of the delay is not ready")

	clock.advance(time.Millisecond)
	events := d.FlushReady()
	assert.Len(t, events, 1)
	assert.Equal(t, EventModify, events[0].Type)
	assert.Equal(t, int64(2), events[0].Size, "the newest event must win")
	assert.Equal(t, 0, d.Count(), "a flushed event must leave the buffer")
	assert.Empty(t, d.FlushReady())
}

func TestDebouncer_FlushReady_ZeroDelayIsImmediate(t *testing.T) {
	d, _ := newClockedDebouncer(0)

	d.Add(Event{Type: EventCreate, Path: "/a"})
	d.Add(Event{Type: EventCreate, Path: "/b"})

	assert.Len(t, d.FlushReady(), 2)
	assert.Equal(t, 0, d.Count())
}

func TestDebouncer_FlushReady_PathsDebounceIndependently(t *testing.T) {
	d, clock := newClockedDebouncer(100 * time.Millisecond)

	d.Add(Event{Type: EventCreate, Path: "/a"})
	clock.advance(70 * time.Millisecond)
	d.Add(Event{Type: EventCreate, Path: "/b"})
	clock.advance(30 * time.Millisecond)

	// /a has been quiet for 100ms; /b only for 30ms.
	events := d.FlushReady()
	assert.Len(t, events, 1)
	assert.Equal(t, "/a", events[0].Path)
	assert.Equal(t, 1, d.Count())

	clock.advance(70 * time.Millisecond)
	events = d.FlushReady()
	assert.Len(t, events, 1)
	assert.Equal(t, "/b", events[0].Path)
}

func TestDebouncer_FlushReady_Empty(t *testing.T) {
	d, _ := newClockedDebouncer(100 * time.Millisecond)
	assert.Nil(t, d.FlushReady())
}

func TestDebouncer_Flush_IgnoresWindow(t *testing.T) {
	d, _ := newClockedDebouncer(time.Hour)

	d.Add(Event{Type: EventCreate, Path: "/a"})

	assert.Empty(t, d.FlushReady())
	assert.Len(t, d.Flush(), 1, "Flush drains events still inside their window")
	assert.Equal(t, 0, d.Count())
}
