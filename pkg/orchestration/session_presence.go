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
	"sync"
	"time"
)

// DefaultPresenceLinger is how long an operator's presence survives the loss
// of its last live attachment before operator_left is committed to the
// transcript.
//
// Presence describes a person watching a session, not a TCP connection. A
// transport reconnect — an SSE client resuming after a dropped connection, a
// browser tab reloading, an EventSource retry — interrupts the connection
// while the operator never stops watching, so writing operator_left plus a
// second operator_joined for it would be a lie recorded permanently in the
// transcript.
//
// The value bounds the worst-case client reconnect window: the SDK SSE reader
// retries with exponential backoff for roughly 8 seconds before giving up
// (see sdk/go/transport/rest/orchestrator_stream.go), and browser EventSource
// implementations retry within ~3 seconds. 15 seconds clears both with margin
// while still reporting a genuine departure promptly.
const DefaultPresenceLinger = 15 * time.Second

// presenceTimer is the cancellable delayed call SessionPresence uses to defer
// a departure. *time.Timer satisfies it in production; tests substitute a
// deterministic implementation so no test has to wait on wall-clock time.
type presenceTimer interface {
	// Stop cancels the pending call, reporting whether it had not already run.
	Stop() bool
}

// presenceScheduler runs f after d and returns a handle that can cancel it.
type presenceScheduler func(d time.Duration, f func()) presenceTimer

// presenceKey identifies one operator's presence in one session. Presence is
// per (tenant, session, operator): the same operator attached from two tabs is
// one presence, which is what "who is watching this session" means.
type presenceKey struct {
	tenantID   string
	sessionID  string
	operatorID string
}

// presenceEntry is the live state for one presenceKey.
type presenceEntry struct {
	// attachments counts currently-open live streams for this operator.
	attachments int
	// generation increments whenever a departure is scheduled, so a timer
	// that already fired but is still waiting on the mutex cannot commit a
	// departure that a subsequent Join has cancelled.
	generation uint64
	// pending is the scheduled departure, nil when none is scheduled.
	pending presenceTimer
	// log is the event log the presence events are appended to.
	log SessionEventLog
	// ctx is the join context stripped of cancellation, so a departure
	// committed after the originating request has ended still carries the
	// request's tenant routing values.
	ctx context.Context
	// iface names the client surface ("web", "cli") for the payload.
	iface string
}

// SessionPresence records operator_joined / operator_left in the durable
// session transcript with connection-churn suppression.
//
// A join is announced only when the operator's attachment count rises from
// zero, and a departure is committed only after the count has stayed at zero
// for the linger window. Reconnecting inside that window silently resumes the
// existing presence, so a dropped-and-resumed stream leaves no trace in the
// transcript while a real departure is still recorded.
//
// The zero value is not usable; construct with NewSessionPresence.
type SessionPresence struct {
	mu       sync.Mutex
	linger   time.Duration
	schedule presenceScheduler
	entries  map[presenceKey]*presenceEntry
	closed   bool
	onError  func(error)
}

// PresenceAppendError reports that a presence event could not be appended to
// the session transcript. Presence is an observability side effect of an
// attach, never the attach's purpose, so callers report it and carry on rather
// than failing the stream.
type PresenceAppendError struct {
	// TenantID is the tenant owning the session.
	TenantID string
	// SessionID is the session the presence event belongs to.
	SessionID string
	// OperatorID identifies the operator whose presence changed.
	OperatorID string
	// Kind is the presence event kind that failed to append.
	Kind SessionEventKind
	// Cause is the underlying append failure.
	Cause error
}

// Error implements the error interface.
func (e *PresenceAppendError) Error() string {
	return fmt.Sprintf("orchestration: append %s for operator %s in session %s/%s: %v",
		e.Kind, e.OperatorID, e.TenantID, e.SessionID, e.Cause)
}

// Unwrap exposes the underlying append failure to errors.Is/As.
func (e *PresenceAppendError) Unwrap() error { return e.Cause }

// NewSessionPresence constructs a presence tracker. A linger of zero or less
// selects DefaultPresenceLinger. onError, when non-nil, receives every
// *PresenceAppendError; passing nil discards them, which is appropriate only
// where no logger exists.
func NewSessionPresence(linger time.Duration, onError func(error)) *SessionPresence {
	if linger <= 0 {
		linger = DefaultPresenceLinger
	}
	return &SessionPresence{
		linger:   linger,
		schedule: defaultPresenceScheduler,
		entries:  map[presenceKey]*presenceEntry{},
		onError:  onError,
	}
}

// defaultPresenceScheduler is the production scheduler: a real timer.
func defaultPresenceScheduler(d time.Duration, f func()) presenceTimer {
	return time.AfterFunc(d, f)
}

// Join records that operatorID has a live attachment to a session and returns
// the function that ends it. The returned function is idempotent and must be
// called exactly once per Join (deferring it is the intended use).
//
// operator_joined is appended only if this is the operator's first attachment
// and no departure was pending; a Join that cancels a pending departure is a
// resumed stream and is deliberately silent.
//
// A nil receiver, nil log, or empty operator/session yields a no-op leave, so
// callers never have to branch on presence being unconfigured.
func (p *SessionPresence) Join(ctx context.Context, log SessionEventLog, tenantID, sessionID, operatorID, iface string) func() {
	if p == nil || log == nil || operatorID == "" || sessionID == "" {
		return func() {}
	}
	key := presenceKey{tenantID: tenantID, sessionID: sessionID, operatorID: operatorID}
	// Cancellation is stripped up front: a departure committed after the
	// request ends must still reach the tenant-routed store.
	joinCtx := context.WithoutCancel(ctx)

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return func() {}
	}
	entry := p.entries[key]
	if entry == nil {
		entry = &presenceEntry{log: log, ctx: joinCtx, iface: iface}
		p.entries[key] = entry
	}
	entry.attachments++
	// A pending departure means the operator never logically left: cancel it
	// and stay quiet. Bumping the generation defeats a timer that has already
	// fired and is blocked on this mutex.
	resumed := entry.pending != nil
	if resumed {
		entry.pending.Stop()
		entry.pending = nil
		entry.generation++
	}
	announce := entry.attachments == 1 && !resumed
	p.mu.Unlock()

	if announce {
		p.append(joinCtx, log, key, KindOperatorJoined, iface)
	}

	var once sync.Once
	return func() { once.Do(func() { p.leave(key) }) }
}

// leave drops one attachment. The departure is committed immediately when the
// tracker is closing and otherwise scheduled linger into the future so a
// reconnect can cancel it.
func (p *SessionPresence) leave(key presenceKey) {
	p.mu.Lock()
	entry := p.entries[key]
	if entry == nil {
		p.mu.Unlock()
		return
	}
	entry.attachments--
	if entry.attachments > 0 {
		p.mu.Unlock()
		return
	}
	if p.closed {
		delete(p.entries, key)
		p.mu.Unlock()
		p.append(entry.ctx, entry.log, key, KindOperatorLeft, entry.iface)
		return
	}
	entry.generation++
	generation := entry.generation
	entry.pending = p.schedule(p.linger, func() { p.commitLeave(key, generation) })
	p.mu.Unlock()
}

// commitLeave writes operator_left once the linger window has elapsed with no
// reconnect. generation guards against a rejoin that raced the timer.
func (p *SessionPresence) commitLeave(key presenceKey, generation uint64) {
	p.mu.Lock()
	entry := p.entries[key]
	if entry == nil || entry.generation != generation || entry.attachments > 0 {
		p.mu.Unlock()
		return
	}
	delete(p.entries, key)
	p.mu.Unlock()
	p.append(entry.ctx, entry.log, key, KindOperatorLeft, entry.iface)
}

// Close commits every outstanding departure immediately and stops accepting
// new joins. It is safe to call more than once. Callers invoke it during
// server shutdown so a restart never leaves phantom operators in a transcript.
func (p *SessionPresence) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	pending := make([]struct {
		key   presenceKey
		entry *presenceEntry
	}, 0, len(p.entries))
	for key, entry := range p.entries {
		if entry.pending != nil {
			entry.pending.Stop()
			entry.pending = nil
		}
		pending = append(pending, struct {
			key   presenceKey
			entry *presenceEntry
		}{key, entry})
		delete(p.entries, key)
	}
	p.mu.Unlock()

	for _, item := range pending {
		p.append(item.entry.ctx, item.entry.log, item.key, KindOperatorLeft, item.entry.iface)
	}
}

// append writes one presence event through the package's single sequence-
// assigning write path. Failures are reported to onError, never returned: the
// transcript stream a presence event decorates must survive its loss.
func (p *SessionPresence) append(ctx context.Context, log SessionEventLog, key presenceKey, kind SessionEventKind, iface string) {
	event := &SessionEvent{
		TenantID:  key.tenantID,
		SessionID: key.sessionID,
		Timestamp: time.Now().UTC(),
		Source:    "operator:" + key.operatorID,
		Kind:      kind,
	}
	switch kind {
	case KindOperatorJoined:
		event.OperatorJoined = &OperatorJoinedPayload{OperatorID: key.operatorID, Interface: iface}
	case KindOperatorLeft:
		event.OperatorLeft = &OperatorLeftPayload{OperatorID: key.operatorID, Interface: iface}
	default:
		return
	}
	if err := AppendNextSessionEvent(ctx, log, event); err != nil && p.onError != nil {
		p.onError(&PresenceAppendError{
			TenantID:   key.tenantID,
			SessionID:  key.sessionID,
			OperatorID: key.operatorID,
			Kind:       kind,
			Cause:      err,
		})
	}
}

// attachmentCount reports how many live attachments the operator holds.
// Intended for diagnostics and tests.
func (p *SessionPresence) attachmentCount(tenantID, sessionID, operatorID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.entries[presenceKey{tenantID: tenantID, sessionID: sessionID, operatorID: operatorID}]
	if entry == nil {
		return 0
	}
	return entry.attachments
}
