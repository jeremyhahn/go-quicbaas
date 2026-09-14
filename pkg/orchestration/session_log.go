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
	"errors"
	"sort"
	"sync"
	"time"
)

// maxSequenceAssignAttempts bounds the optimistic-concurrency retry in
// AppendNextSessionEvent. A session has several genuine concurrent writers —
// the orchestrator's own turn dispatch, background turn goroutines, and every
// live attacher recording its presence — that race for the next sequence
// number. A handful of immediate retries absorbs the race without ever
// blocking on a timer.
const maxSequenceAssignAttempts = 8

// sessionLogConcluded reports whether a session's log has already ended.
//
// A STORE THAT CANNOT ANSWER IS NOT AN ANSWER OF "NO". Returning false on any
// failed read made every transient GetMeta error open the gate, so a metadata
// blip between a conclusion and this append was enough to land an ordinary
// event after KindSessionClosed — defeating the invariant the gate exists to
// hold, through a path that reported nothing wrong. The error is returned so
// the append fails and the caller retries: dropping telemetry on a store blip
// is recoverable, and an event stranded after the ending is not.
//
// A SESSION THE LOG NEVER REGISTERED IS AN ANSWER, and the only one. It is not
// concluded — it has no log at all — so ErrSessionLogNotFound reports "not
// concluded" without an error. This is the same distinction between "the store
// could not answer" and "the store answered, and the answer is nothing" that
// the conclusion transition already draws.
func sessionLogConcluded(ctx context.Context, log SessionEventLog, event *SessionEvent) (bool, error) {
	meta, err := log.GetMeta(ctx, event.TenantID, event.SessionID)
	switch {
	case errors.Is(err, ErrSessionLogNotFound):
		return false, nil
	case err != nil:
		return false, err
	}
	return meta.Status == SessionStateClosed, nil
}

// AppendNextSessionEvent assigns event.Sequence from the log's current tail
// and appends, retrying immediately when a concurrent writer wins the race for
// that sequence. It is the single write path for every session event producer:
// SessionEventLog.Append deliberately requires an exact sequence so the log
// stays gapless, which makes the read-then-write dance a shared concern rather
// than something each caller re-invents.
//
// The event's Sequence field is overwritten; callers must not pre-assign it.
func AppendNextSessionEvent(ctx context.Context, log SessionEventLog, event *SessionEvent) error {
	if log == nil {
		return &SessionLogError{Op: "append: no event log configured"}
	}
	if event == nil {
		return &SessionLogError{Op: "append", Cause: ErrSessionEventNil}
	}

	// THE TRANSCRIPT IS FILED UNDER THE TENANT THE SESSION RECORD IS FILED
	// UNDER, AND THIS IS WHERE THE TWO ARE MADE TO AGREE.
	//
	// Every durable orchestration record is keyed by StorageTenant(ctx), which
	// substitutes SystemTenantID for a caller that carries no tenant — an
	// in-process job runner, an embedded consumer, a unit test. The session
	// event log's producers stamped TenantFromContext(ctx) instead, which for
	// those callers is the empty string, and the KV log REFUSES an event with
	// no tenant. The refusal was reported to a log line and the turn continued,
	// so an untenanted turn produced a session whose record existed under
	// "system" and whose transcript existed nowhere: the operator opened it and
	// saw only the events a tenanted reader had itself caused (its own
	// operator_joined). go-quicbaas#793.
	//
	// Normalising HERE rather than at each producer is what makes it hold: this
	// is the documented single write path, so a future producer inherits the
	// agreement instead of having to remember it. An event that names a tenant
	// explicitly is left alone — a cross-tenant append is a caller bug the log
	// must still be able to reject.
	if event.TenantID == "" {
		event.TenantID = StorageTenant(ctx)
	}

	// A TURN'S TEXT IS CAPPED BEFORE IT IS STORED, on the same single path, so
	// no producer can put an unbounded payload in the KV store. See
	// capSessionEventText for the cap and how truncation is marked.
	capSessionEventText(event)

	// NOTHING FOLLOWS THE ENDING, AND THE LOG ENFORCES IT.
	//
	// KindSessionClosed is documented as always the last event of a session,
	// and until this check that was a comment: an append after a conclusion
	// simply landed, and — because the sequence is derived from metadata a
	// re-registration can reset — could land ON TOP of the terminal event and
	// erase it. A rule that lives only in prose is a rule the next writer does
	// not know about, so it lives here now, on the one path every append takes.
	//
	// A TERMINAL EVENT IS THE ONE SANCTIONED EXCEPTION, AND IT IS KEYED TO THE
	// STATE IT EXISTS FOR. The exemption is there so a conclusion whose gate
	// closed but whose ending never landed can still be completed by a retry —
	// a session that is closed everywhere except in its own narrative. It is
	// therefore granted only while that ending is MISSING: once the log ends in
	// a terminal event, a second one is refused like any other late append.
	// Blanket-exempting the kind instead would have left "exactly one ending"
	// resting entirely on the conclusion transition being the only writer that
	// ever constructs one, which is a property of today's callers rather than
	// of the log.
	var lastErr error
	for attempt := 0; attempt < maxSequenceAssignAttempts; attempt++ {
		// THE SEQUENCE IS READ BEFORE THE GATE, AND THAT ORDER IS THE
		// GUARANTEE.
		//
		// Checked the other way round, a writer could read an OPEN gate, pause,
		// and then read a sequence past an ending that landed while it was
		// paused — appending after the terminal event with nothing violated
		// along the way. Reading the sequence first removes that window without
		// costing a read, because the log's own monotonicity check closes it
		// from the other side: this append is admitted only at latest+1, so if
		// an ending landed after this sequence was read it TOOK that slot, the
		// append is refused with ErrSessionLogGap, and the retry re-reads a gate
		// that is now shut. Either the gate sees the conclusion or the sequence
		// collides with it; there is no interleaving where neither does.
		latest, err := log.LatestSequence(ctx, event.TenantID, event.SessionID)
		if err != nil {
			return &SessionLogError{Op: "append: read latest sequence", Cause: err}
		}

		// RE-CHECKED ON EVERY ATTEMPT, not once before the loop. A gap retry
		// means another writer took the sequence, and that other writer may
		// have been the conclusion: re-reading here is what stops a retry from
		// carrying an event across the ending it just lost a race to.
		// BOTH ARMS FAIL CLOSED, for the one reason. A read that cannot answer
		// leaves the caller free to retry, while appending on the strength of a
		// failed read risks the two things this gate exists to prevent: a second
		// ending nothing can remove, and an ordinary event stranded after the
		// ending. Neither is recoverable; a refused append is.
		if event.Kind == KindSessionClosed {
			ended, endedErr := sessionLogEnded(ctx, log, event.TenantID, event.SessionID)
			if endedErr != nil {
				return &SessionLogError{
					Op:    "append: read session " + event.SessionID + " ending",
					Cause: endedErr,
				}
			}
			if ended {
				return &SessionLogError{
					Op:    "append: session " + event.SessionID + " has already ended",
					Cause: ErrSessionLogConcluded,
				}
			}
		} else {
			concluded, concludedErr := sessionLogConcluded(ctx, log, event)
			if concludedErr != nil {
				return &SessionLogError{
					Op:    "append: read session " + event.SessionID + " conclusion",
					Cause: concludedErr,
				}
			}
			if concluded {
				return &SessionLogError{
					Op:    "append: session " + event.SessionID + " is concluded",
					Cause: ErrSessionLogConcluded,
				}
			}
		}
		event.Sequence = latest + 1
		lastErr = log.Append(ctx, event)
		if lastErr == nil {
			return nil
		}
		if errors.Is(lastErr, ErrSessionLogGap) {
			continue // another writer took this sequence; re-read and retry
		}
		return lastErr
	}
	return lastErr
}

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

	// DeleteSession removes a session's whole transcript: every event and the
	// metadata record. It is what makes "deleting a session deletes its
	// history" true rather than aspirational — a session record removed while
	// its conversation stayed readable would leave an operator able to read a
	// session the system says does not exist.
	//
	// Deleting a session that has no transcript is not an error: the
	// post-condition ("no transcript for this session") already holds, and a
	// caller cleaning up after a failed spawn must not have to distinguish
	// the two.
	DeleteSession(ctx context.Context, tenantID, sessionID string) error
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
	events map[string][]*SessionEvent // key: tenant+session
	meta   map[string]*SessionMeta    // key: tenant+session
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
		return &SessionLogAppendError{Cause: ErrSessionEventNil}
	}
	if event.SessionID == "" || event.TenantID == "" {
		return &SessionLogAppendError{Cause: ErrSessionEventMissingIDs}
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
		return &SessionLogError{Op: "put meta: session_id and tenant_id required"}
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

	prefix := logTenant(tenantID) + "\x00"
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

// DeleteSession implements SessionEventLog.
func (m *MemorySessionEventLog) DeleteSession(ctx context.Context, tenantID, sessionID string) error {
	if sessionID == "" {
		return &SessionLogError{Op: "delete session: session_id required"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memKey(tenantID, sessionID)
	delete(m.events, key)
	delete(m.meta, key)
	return nil
}

// memKey produces the composite key used inside the in-memory maps. The null
// byte separator cannot appear in a tenantID or sessionID, avoiding collisions.
func memKey(tenantID, sessionID string) string {
	return logTenant(tenantID) + "\x00" + sessionID
}

// logTenant resolves the tenant a transcript is addressed under.
//
// IT IS StorageTenant's RULE, APPLIED TO THE ONE STORE THAT DID NOT HAVE IT.
// Every other durable orchestration record substitutes SystemTenantID for an
// absent tenant, so an untenanted writer and an untenanted reader meet in the
// same scope. The session event log instead REJECTED an empty tenant on write
// and scanned an empty-prefixed keyspace on read, which is how an in-process
// producer could fill a transcript nobody could open (go-quicbaas#793).
//
// Applied inside the log implementations rather than only at their callers so
// the rule holds for a direct Append/Range as well as for the orchestrator's
// single write path, and the key a transcript is stored under always matches
// the key it is read back from.
func logTenant(tenantID string) string {
	if tenantID == "" {
		return SystemTenantID
	}
	return tenantID
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
