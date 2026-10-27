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

// debouncer coalesces events per path and holds each path back until it has
// been quiet for the configured delay, so a file still being written is
// emitted once, after its last change.
type debouncer struct {
	delay  time.Duration
	now    func() time.Time
	mu     sync.Mutex
	events map[string]pendingEvent // keyed by path
}

// pendingEvent is the newest event buffered for a path and when it arrived.
type pendingEvent struct {
	event   Event
	arrived time.Time
}

// newDebouncer creates a new debouncer with the given delay.
func newDebouncer(delay time.Duration) *debouncer {
	return &debouncer{
		delay:  delay,
		now:    time.Now,
		events: make(map[string]pendingEvent),
	}
}

// Add adds an event to the debouncer.
// If an event for the same path already exists, it is replaced and the
// path's delay starts again.
func (d *debouncer) Add(event Event) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.events[event.Path] = pendingEvent{event: event, arrived: d.now()}
}

// Flush returns and removes the events whose path has been quiet for at least
// the delay. Paths changed more recently stay pending for a later flush.
func (d *debouncer) Flush() []Event {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	var events []Event
	for path, pending := range d.events {
		if now.Sub(pending.arrived) < d.delay {
			continue
		}
		events = append(events, pending.event)
		delete(d.events, path)
	}

	return events
}

// Drain returns and removes every pending event, ignoring the delay. It is
// used on shutdown so that events still inside their delay are not lost.
func (d *debouncer) Drain() []Event {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.events) == 0 {
		return nil
	}

	events := make([]Event, 0, len(d.events))
	for _, pending := range d.events {
		events = append(events, pending.event)
	}
	d.events = make(map[string]pendingEvent)

	return events
}

// Count returns the number of pending events.
func (d *debouncer) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.events)
}

// Clear removes all pending events.
func (d *debouncer) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = make(map[string]pendingEvent)
}
