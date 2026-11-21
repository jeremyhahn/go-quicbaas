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
	d := newDebouncer()

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
	d := newDebouncer()

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
	d := newDebouncer()

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
	d := newDebouncer()

	events := d.Flush()
	assert.Nil(t, events)
	assert.Equal(t, 0, d.Count())
}

func TestDebouncer_Count(t *testing.T) {
	d := newDebouncer()

	assert.Equal(t, 0, d.Count())

	d.Add(Event{Path: "/file1.txt"})
	assert.Equal(t, 1, d.Count())

	d.Add(Event{Path: "/file2.txt"})
	assert.Equal(t, 2, d.Count())

	d.Flush()
	assert.Equal(t, 0, d.Count())
}

func TestDebouncer_Clear(t *testing.T) {
	d := newDebouncer()

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
	d := newDebouncer()

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
