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
	"time"
)

// debouncer collects events and deduplicates them per path. A path's event is
// ready once delay has passed since the last event for that path; now is the
// clock the window is measured against (time.Now outside tests).
type debouncer struct {
	delay  time.Duration
	now    func() time.Time
	mu     sync.Mutex
	events map[string]*pendingEvent // keyed by path
}

// pendingEvent is the newest event for a path and the time it was added.
type pendingEvent struct {
	Event
	lastSeen time.Time
}

// newDebouncer creates a new debouncer with the given delay.
func newDebouncer(delay time.Duration) *debouncer {
	return &debouncer{
		delay:  delay,
		now:    time.Now,
		events: make(map[string]*pendingEvent),
	}
}

// Add adds an event to the debouncer.
// If an event for the same path already exists, it's replaced with the new one.
func (d *debouncer) Add(event Event) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Replace any existing event for this path and restart its window.
	d.events[event.Path] = &pendingEvent{Event: event, lastSeen: d.now()}
}

// Flush returns all accumulated events, ready or not, and clears the buffer.
func (d *debouncer) Flush() []Event {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.events) == 0 {
		return nil
	}

	// Convert map to slice
	events := make([]Event, 0, len(d.events))
	for _, pending := range d.events {
		events = append(events, pending.Event)
	}

	// Clear the map
	d.events = make(map[string]*pendingEvent)

	return events
}

// FlushReady returns and removes the events whose path has seen no new event
// for at least the debounce delay. Events still inside their window stay
// buffered so later changes to the same path keep coalescing into them.
func (d *debouncer) FlushReady() []Event {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.events) == 0 {
		return nil
	}

	now := d.now()
	var events []Event
	for path, pending := range d.events {
		if now.Sub(pending.lastSeen) < d.delay {
			continue
		}
		events = append(events, pending.Event)
		delete(d.events, path)
	}

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
	d.events = make(map[string]*pendingEvent)
}
