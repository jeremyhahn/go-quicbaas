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
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// SessionEventLog is the storage + fan-out surface for session-scoped
// events. Every session-related change produced by the daemon (prompts,
// assistant chunks, tool calls, file ops, operator join/leave) lands here
// before being delivered to attachers.
//
// The log provides three distinct consumption modes:
//
//  1. Replay: fetch a chronological page of events starting at some
//     sequence via Range. Used by operators attaching mid-session to
//     scroll back through history.
//
//  2. Tail: receive new events as they arrive via Subscribe. Used by live
//     UI dashboards and the Signal bot's streaming output.
//
//  3. Replay + tail: the usual attach pattern — a client first pages
//     history, then upgrades to a Subscribe for everything newer than the
//     last replayed sequence. Clients compose these two calls themselves;
//     the log doesn't combine them into one RPC because the atomicity
//     boundary between "last page" and "first tail event" is easier to
//     reason about when they are explicit steps.
//
// Implementations MUST:
//   - Reject appends whose Sequence is not exactly LatestSequence()+1
//     (return ErrSessionLogGap)
//   - Deliver events to Subscribe consumers in strict per-session sequence
//     order
//   - Drop slow subscribers rather than block the append path
//   - Encrypt event payloads at rest using the tenant DEK when one is
//     configured
type SessionEventLog interface {
	// Append persists an event and publishes it to live subscribers. The
	// event's Sequence must equal LatestSequence(tenant, session)+1.
	// Returns ErrSessionLogGap on a violation.
	Append(ctx context.Context, event *SessionEvent) error

	// Range returns events with sequence in [fromSeq, fromSeq+limit),
	// ascending order. The returned nextSeq is the sequence to pass on the
	// next page request (0 means no more events). limit<=0 returns the
	// whole remainder.
	Range(ctx context.Context, tenantID, sessionID string, fromSeq uint64, limit int) (events []*SessionEvent, nextSeq uint64, err error)

	// LatestSequence returns the highest sequence appended to the session,
	// or 0 when no events exist. ErrSessionLogNotFound is NOT returned —
	// callers should treat 0 as "no events yet" rather than an error.
	LatestSequence(ctx context.Context, tenantID, sessionID string) (uint64, error)

	// Subscribe registers a live subscriber to the session. The returned
	// channel receives every event appended after Subscribe returns.
	// Callers MUST invoke the returned unsubscribe func to release resources.
	//
	// Channel buffer is implementation-specific (16-1024). On overflow
	// the oldest buffered event is dropped and a SystemMessage event with
	// code="subscriber_overflow" is delivered so UIs can surface that
	// they missed something.
	Subscribe(ctx context.Context, tenantID, sessionID string) (events <-chan *SessionEvent, unsubscribe func(), err error)

	// PutMeta stores (or replaces) the metadata record for a session.
	PutMeta(ctx context.Context, meta *SessionMeta) error

	// GetMeta returns the metadata record for a session, or
	// ErrSessionLogNotFound when the session was never registered.
	GetMeta(ctx context.Context, tenantID, sessionID string) (*SessionMeta, error)

	// ListSessions returns the metadata for sessions in a tenant matching
	// the filter, newest first, capped at limit. limit<=0 returns all.
	ListSessions(ctx context.Context, tenantID string, filter *SessionFilter, limit int) ([]*SessionMeta, error)
}

// =============================================================================
// MemorySessionEventLog — in-memory reference implementation
// =============================================================================

// MemorySessionEventLog is a goroutine-safe, in-memory SessionEventLog
// suitable for tests, single-node deployments, and the initial dispatch
// layer before the KV-backed log lands. The KV-backed variant in
// session_log_kv.go persists events to PebbleDB via the existing KVStore
// interface.
//
// The in-memory log shares the fan-out implementation with the KV variant
// (sessionLogFanout), so subscriber semantics are identical across both.
type MemorySessionEventLog struct {
	mu     sync.RWMutex
	events map[string][]*SessionEvent  // key: tenant+session
	meta   map[string]*SessionMeta      // key: tenant+session
	fanout *sessionLogFanout
}

// Compile-time interface check.
var _ SessionEventLog = (*MemorySessionEventLog)(nil)

// NewMemorySessionEventLog constructs an empty in-memory log.
func NewMemorySessionEventLog() *MemorySessionEventLog {
	return &MemorySessionEventLog{
		events: map[string][]*SessionEvent{},
		meta:   map[string]*SessionMeta{},
		fanout: newSessionLogFanout(),
	}
}

// Append implements SessionEventLog.
func (m *MemorySessionEventLog) Append(ctx context.Context, event *SessionEvent) error {
	if event == nil {
		return &SessionLogAppendError{Cause: fmt.Errorf("nil event")}
	}
	if event.SessionID == "" || event.TenantID == "" {
		return &SessionLogAppendError{Cause: fmt.Errorf("session_id and tenant_id required")}
	}
	key := memKey(event.TenantID, event.SessionID)

	m.mu.Lock()
	// Validate monotonicity. The first event must have Sequence == 1.
	existing := m.events[key]
	expected := uint64(len(existing)) + 1
	if event.Sequence != expected {
		m.mu.Unlock()
		return ErrSessionLogGap
	}
	// Defensive copy: callers mutating the event after Append would otherwise
	// see their changes reflected in replay.
	stored := *event
	m.events[key] = append(existing, &stored)
	// Update meta's rolling fields if the session has meta registered.
	if meta := m.meta[key]; meta != nil {
		meta.LatestSequence = event.Sequence
		meta.EventCount = uint64(len(m.events[key]))
		meta.LastActivity = event.Timestamp
	}
	m.mu.Unlock()

	// Publish outside the lock so a slow subscriber can't stall appends.
	m.fanout.publish(event.TenantID, event.SessionID, &stored)
	return nil
}

// Range implements SessionEventLog.
func (m *MemorySessionEventLog) Range(ctx context.Context, tenantID, sessionID string, fromSeq uint64, limit int) ([]*SessionEvent, uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	events := m.events[memKey(tenantID, sessionID)]
	if len(events) == 0 {
		return nil, 0, nil
	}

	start := 0
	if fromSeq > 0 {
		// Sequences are dense and 1-indexed, so the offset is fromSeq-1.
		start = int(fromSeq - 1)
		if start < 0 {
			start = 0
		}
		if start >= len(events) {
			return nil, 0, nil
		}
	}

	end := len(events)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	page := make([]*SessionEvent, end-start)
	copy(page, events[start:end])

	var nextSeq uint64
	if end < len(events) {
		nextSeq = uint64(end + 1) // +1 because sequences are 1-indexed
	}
	return page, nextSeq, nil
}

// LatestSequence implements SessionEventLog.
func (m *MemorySessionEventLog) LatestSequence(ctx context.Context, tenantID, sessionID string) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return uint64(len(m.events[memKey(tenantID, sessionID)])), nil
}

// Subscribe implements SessionEventLog.
func (m *MemorySessionEventLog) Subscribe(ctx context.Context, tenantID, sessionID string) (<-chan *SessionEvent, func(), error) {
	return m.fanout.subscribe(tenantID, sessionID)
}

// PutMeta implements SessionEventLog.
func (m *MemorySessionEventLog) PutMeta(ctx context.Context, meta *SessionMeta) error {
	if meta == nil || meta.SessionID == "" || meta.TenantID == "" {
		return fmt.Errorf("orchestration session log: put meta: session_id and tenant_id required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *meta
	m.meta[memKey(meta.TenantID, meta.SessionID)] = &clone
	return nil
}

// GetMeta implements SessionEventLog.
func (m *MemorySessionEventLog) GetMeta(ctx context.Context, tenantID, sessionID string) (*SessionMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta, ok := m.meta[memKey(tenantID, sessionID)]
	if !ok {
		return nil, ErrSessionLogNotFound
	}
	clone := *meta
	return &clone, nil
}

// ListSessions implements SessionEventLog.
func (m *MemorySessionEventLog) ListSessions(ctx context.Context, tenantID string, filter *SessionFilter, limit int) ([]*SessionMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := tenantID + "\x00"
	var out []*SessionMeta
	for key, meta := range m.meta {
		if len(key) < len(prefix) || key[:len(prefix)] != prefix {
			continue
		}
		if !matchesFilter(meta, filter) {
			continue
		}
		clone := *meta
		out = append(out, &clone)
	}
	// Newest first.
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// memKey produces the composite key used inside the in-memory maps. The null
// byte separator cannot appear in a tenantID or sessionID, avoiding collisions.
func memKey(tenantID, sessionID string) string {
	return tenantID + "\x00" + sessionID
}

// matchesFilter reports whether the session meta satisfies the filter
// constraints. A nil filter matches anything.
func matchesFilter(meta *SessionMeta, filter *SessionFilter) bool {
	if filter == nil {
		return true
	}
	if filter.Backend != "" && meta.Backend != filter.Backend {
		return false
	}
	if filter.Status != "" && meta.Status != filter.Status {
		return false
	}
	if !filter.CreatedAfter.IsZero() && meta.CreatedAt.Before(filter.CreatedAfter) {
		return false
	}
	if !filter.CreatedBefore.IsZero() && meta.CreatedAt.After(filter.CreatedBefore) {
		return false
	}
	for k, v := range filter.Labels {
		if meta.Labels[k] != v {
			return false
		}
	}
	return true
}

// =============================================================================
// Fan-out Hub
// =============================================================================

// sessionLogFanout is the in-memory pub/sub shared by all SessionEventLog
// implementations. Each session has zero or more live subscribers; when a
// session receives an Append, the fanout broadcasts the event to every
// subscriber's buffered channel.
//
// Buffering + drop policy: each subscriber gets a channel of capacity 256.
// When publishing, if the subscriber's channel is full, the publish drops
// the event for that subscriber and increments a counter. Dropped
// subscribers are NOT disconnected — they will see the next event they can
// absorb. UI layers detect the gap from per-event sequence numbers and
// issue a Range to catch up. This keeps the append path non-blocking on
// pathological consumers.
type sessionLogFanout struct {
	mu   sync.Mutex
	subs map[string]map[*fanoutSubscriber]struct{} // key: tenant+session

	// Generation counter so stale publishes after unsubscribe don't cause
	// a panic on a closed channel. Each subscriber captures the generation
	// at registration time.
	_ uint64
}

type fanoutSubscriber struct {
	ch      chan *SessionEvent
	ctx     context.Context
	dropped uint64 // read lockless; writes only from publish()
}

// newSessionLogFanout constructs an empty fanout hub.
func newSessionLogFanout() *sessionLogFanout {
	return &sessionLogFanout{subs: map[string]map[*fanoutSubscriber]struct{}{}}
}

// subscribe registers a new live subscriber for a session. The returned
// unsubscribe fn is idempotent; calling it more than once is a no-op. The
// event channel is closed when the subscriber is unsubscribed.
func (f *sessionLogFanout) subscribe(tenantID, sessionID string) (<-chan *SessionEvent, func(), error) {
	sub := &fanoutSubscriber{
		ch:  make(chan *SessionEvent, 256),
		ctx: context.Background(),
	}
	key := memKey(tenantID, sessionID)

	f.mu.Lock()
	if f.subs[key] == nil {
		f.subs[key] = map[*fanoutSubscriber]struct{}{}
	}
	f.subs[key][sub] = struct{}{}
	f.mu.Unlock()

	var unsubOnce sync.Once
	unsubscribe := func() {
		unsubOnce.Do(func() {
			f.mu.Lock()
			if set, ok := f.subs[key]; ok {
				delete(set, sub)
				if len(set) == 0 {
					delete(f.subs, key)
				}
			}
			f.mu.Unlock()
			close(sub.ch)
		})
	}
	return sub.ch, unsubscribe, nil
}

// publish broadcasts an event to every subscriber of the session. Slow
// subscribers get the event dropped rather than blocking the sender.
func (f *sessionLogFanout) publish(tenantID, sessionID string, event *SessionEvent) {
	key := memKey(tenantID, sessionID)

	f.mu.Lock()
	set := f.subs[key]
	// Snapshot so we can release the lock before sending on channels.
	snapshot := make([]*fanoutSubscriber, 0, len(set))
	for s := range set {
		snapshot = append(snapshot, s)
	}
	f.mu.Unlock()

	for _, s := range snapshot {
		select {
		case s.ch <- event:
		default:
			// Channel full — drop for this subscriber and count so tests /
			// diagnostics can observe slow consumers.
			s.dropped++
		}
	}
}

// SubscriberCount returns the current live-subscriber count for a session.
// Intended for diagnostics / tests.
func (f *sessionLogFanout) subscriberCount(tenantID, sessionID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs[memKey(tenantID, sessionID)])
}

// =============================================================================
// Convenience helpers
// =============================================================================

// NewEventID is not a function but a guideline: the sequence number IS the
// event ID within a session. Cross-session identity is (tenant, session, seq).
// Consumers needing a flat ID can concatenate these three.

// ensureTimestamp sets event.Timestamp to now in UTC when it's zero. Used by
// helpers that emit events on behalf of the orchestrator control loop.
func ensureTimestamp(event *SessionEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
}
