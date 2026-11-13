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
	"sync"
)

// debouncer collects events within a time window and deduplicates them.
type debouncer struct {
	mu     sync.Mutex
	events map[string]*Event // keyed by path
}

// newDebouncer creates an empty debouncer. Its only window is the caller's
// flush cadence (Config.BatchInterval): events for a path coalesce, last
// write wins, until the next Flush.
func newDebouncer() *debouncer {
	return &debouncer{
		events: make(map[string]*Event),
	}
}

// Add adds an event to the debouncer.
// If an event for the same path already exists, it's replaced with the new one.
func (d *debouncer) Add(event Event) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Replace any existing event for this path
	d.events[event.Path] = &event
}

// Flush returns all accumulated events and clears the buffer.
func (d *debouncer) Flush() []Event {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.events) == 0 {
		return nil
	}

	// Convert map to slice
	events := make([]Event, 0, len(d.events))
	for _, event := range d.events {
		events = append(events, *event)
	}

	// Clear the map
	d.events = make(map[string]*Event)

	return events
}

// Count returns the number of pending events.
func (d *debouncer) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.events)
}

// Clear removes all pending events without returning them.
func (d *debouncer) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = make(map[string]*Event)
}
