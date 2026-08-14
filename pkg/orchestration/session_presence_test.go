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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SessionPresence decides what a live attach writes into the durable
// transcript. The behaviour under test is the reason operators saw
// operator_left / operator_joined pairs every time a stream reconnected: a
// presence event must describe a person watching a session, never the TCP
// connection carrying the stream.

// manualScheduler replaces time.AfterFunc so the linger window is driven by
// the test rather than by wall-clock time.
type manualScheduler struct {
	pending []*manualTimer
}

type manualTimer struct {
	fn      func()
	stopped bool
	fired   bool
}

// Stop implements presenceTimer.
func (t *manualTimer) Stop() bool {
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	return true
}

// schedule records the deferred call instead of running it.
func (m *manualScheduler) schedule(_ time.Duration, fn func()) presenceTimer {
	timer := &manualTimer{fn: fn}
	m.pending = append(m.pending, timer)
	return timer
}

// elapse fires every timer that has not been stopped, simulating the linger
// window expiring.
func (m *manualScheduler) elapse() {
	pending := m.pending
	m.pending = nil
	for _, timer := range pending {
		if timer.stopped || timer.fired {
			continue
		}
		timer.fired = true
		timer.fn()
	}
}

// newManualPresence builds a tracker whose linger is driven by the returned
// scheduler.
func newManualPresence(onError func(error)) (*SessionPresence, *manualScheduler) {
	scheduler := &manualScheduler{}
	presence := NewSessionPresence(time.Minute, onError)
	presence.schedule = scheduler.schedule
	return presence, scheduler
}

// presenceKinds reads the transcript and returns just the presence event
// kinds, in order — the shape an operator actually sees in a transcript.
func presenceKinds(t *testing.T, log SessionEventLog, tenantID, sessionID string) []SessionEventKind {
	t.Helper()
	events, _, err := log.Range(context.Background(), tenantID, sessionID, 1, 0)
	require.NoError(t, err)
	kinds := make([]SessionEventKind, 0, len(events))
	for _, ev := range events {
		if ev.Kind == KindOperatorJoined || ev.Kind == KindOperatorLeft {
			kinds = append(kinds, ev.Kind)
		}
	}
	return kinds
}

func TestSessionPresence_JoinAndLeaveRecordOnePairEach(t *testing.T) {
	log := NewMemorySessionEventLog()
	presence, scheduler := newManualPresence(nil)

	leave := presence.Join(context.Background(), log, "system", "s1", "alice", "cli")
	assert.Equal(t, []SessionEventKind{KindOperatorJoined}, presenceKinds(t, log, "system", "s1"),
		"the first attach announces the operator")

	leave()
	assert.Equal(t, []SessionEventKind{KindOperatorJoined}, presenceKinds(t, log, "system", "s1"),
		"departure is deferred until the linger window closes")

	scheduler.elapse()
	assert.Equal(t, []SessionEventKind{KindOperatorJoined, KindOperatorLeft},
		presenceKinds(t, log, "system", "s1"), "a genuine departure is recorded")
	assert.Zero(t, presence.attachmentCount("system", "s1", "alice"))
}

// TestSessionPresence_ReconnectWithinLingerWritesNothing is the regression
// test for the churn in the bug report: an SSE client that drops and resumes
// must leave the transcript untouched.
func TestSessionPresence_ReconnectWithinLingerWritesNothing(t *testing.T) {
	log := NewMemorySessionEventLog()
	presence, scheduler := newManualPresence(nil)

	leave := presence.Join(context.Background(), log, "system", "s1", "alice", "web")
	require.Equal(t, []SessionEventKind{KindOperatorJoined}, presenceKinds(t, log, "system", "s1"))

	leave() // connection dropped
	rejoin := presence.Join(context.Background(), log, "system", "s1", "alice", "web")
	scheduler.elapse() // the deferred departure must have been cancelled

	assert.Equal(t, []SessionEventKind{KindOperatorJoined}, presenceKinds(t, log, "system", "s1"),
		"a resumed stream must not produce operator_left/operator_joined churn")
	assert.Equal(t, 1, presence.attachmentCount("system", "s1", "alice"))

	rejoin()
	scheduler.elapse()
	assert.Equal(t, []SessionEventKind{KindOperatorJoined, KindOperatorLeft},
		presenceKinds(t, log, "system", "s1"), "the eventual real departure still lands")
}

func TestSessionPresence_ConcurrentAttachmentsAnnounceOnce(t *testing.T) {
	log := NewMemorySessionEventLog()
	presence, scheduler := newManualPresence(nil)

	first := presence.Join(context.Background(), log, "system", "s1", "alice", "web")
	second := presence.Join(context.Background(), log, "system", "s1", "alice", "cli")
	assert.Equal(t, 2, presence.attachmentCount("system", "s1", "alice"))
	assert.Equal(t, []SessionEventKind{KindOperatorJoined}, presenceKinds(t, log, "system", "s1"),
		"a second tab is the same operator watching, not a second arrival")

	first()
	scheduler.elapse()
	assert.Equal(t, []SessionEventKind{KindOperatorJoined}, presenceKinds(t, log, "system", "s1"),
		"the operator is still watching through the other attachment")

	second()
	scheduler.elapse()
	assert.Equal(t, []SessionEventKind{KindOperatorJoined, KindOperatorLeft},
		presenceKinds(t, log, "system", "s1"))
}

func TestSessionPresence_DistinctOperatorsTrackedIndependently(t *testing.T) {
	log := NewMemorySessionEventLog()
	presence, scheduler := newManualPresence(nil)

	leaveAlice := presence.Join(context.Background(), log, "system", "s1", "alice", "web")
	presence.Join(context.Background(), log, "system", "s1", "bob", "web")
	assert.Equal(t, []SessionEventKind{KindOperatorJoined, KindOperatorJoined},
		presenceKinds(t, log, "system", "s1"))

	leaveAlice()
	scheduler.elapse()
	events, _, err := log.Range(context.Background(), "system", "s1", 1, 0)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, KindOperatorLeft, events[2].Kind)
	assert.Equal(t, "alice", events[2].OperatorLeft.OperatorID, "only alice left")
	assert.Equal(t, 1, presence.attachmentCount("system", "s1", "bob"))
}

// TestSessionPresence_LeaveRaceWithRejoinIsSuppressed covers the timer that
// fires and then blocks on the tracker mutex while a reconnect wins the race.
// Without the generation guard it would commit a departure for an operator who
// is demonstrably still attached.
func TestSessionPresence_LeaveRaceWithRejoinIsSuppressed(t *testing.T) {
	log := NewMemorySessionEventLog()
	presence, scheduler := newManualPresence(nil)

	leave := presence.Join(context.Background(), log, "system", "s1", "alice", "web")
	leave()
	require.Len(t, scheduler.pending, 1)
	stale := scheduler.pending[0]

	// The reconnect lands first and cancels the pending departure.
	presence.Join(context.Background(), log, "system", "s1", "alice", "web")
	// The already-fired timer now runs anyway; it must be a no-op.
	stale.fn()

	assert.Equal(t, []SessionEventKind{KindOperatorJoined}, presenceKinds(t, log, "system", "s1"))
	assert.Equal(t, 1, presence.attachmentCount("system", "s1", "alice"))
}

func TestSessionPresence_CloseCommitsOutstandingDepartures(t *testing.T) {
	log := NewMemorySessionEventLog()
	presence, _ := newManualPresence(nil)

	// One operator still attached, one with a departure pending.
	presence.Join(context.Background(), log, "system", "s1", "alice", "web")
	leaveBob := presence.Join(context.Background(), log, "system", "s1", "bob", "cli")
	leaveBob()

	presence.Close()

	kinds := presenceKinds(t, log, "system", "s1")
	assert.Equal(t, 2, countKind(kinds, KindOperatorJoined))
	assert.Equal(t, 2, countKind(kinds, KindOperatorLeft),
		"shutdown records a departure for every tracked operator")

	// Close is idempotent and a post-close Join is inert.
	presence.Close()
	assert.Equal(t, 4, len(presenceKinds(t, log, "system", "s1")))
	presence.Join(context.Background(), log, "system", "s1", "carol", "web")()
	assert.Equal(t, 4, len(presenceKinds(t, log, "system", "s1")))
}

// countKind counts occurrences of one kind in a slice.
func countKind(kinds []SessionEventKind, want SessionEventKind) int {
	n := 0
	for _, kind := range kinds {
		if kind == want {
			n++
		}
	}
	return n
}

// TestSessionPresence_InvalidInputYieldsInertLeave asserts the tracker never
// panics or writes when it is unconfigured or under-specified — presence is a
// decoration on a stream and must not be able to break one.
func TestSessionPresence_InvalidInputYieldsInertLeave(t *testing.T) {
	log := NewMemorySessionEventLog()
	presence, scheduler := newManualPresence(nil)

	cases := []struct {
		name       string
		presence   *SessionPresence
		log        SessionEventLog
		sessionID  string
		operatorID string
	}{
		{name: "nil tracker", presence: nil, log: log, sessionID: "s1", operatorID: "alice"},
		{name: "nil log", presence: presence, log: nil, sessionID: "s1", operatorID: "alice"},
		{name: "empty operator", presence: presence, log: log, sessionID: "s1", operatorID: ""},
		{name: "empty session", presence: presence, log: log, sessionID: "", operatorID: "alice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leave := tc.presence.Join(context.Background(), tc.log, "system", tc.sessionID, tc.operatorID, "web")
			require.NotNil(t, leave, "Join must always return a callable leave")
			assert.NotPanics(t, func() { leave(); leave() }, "leave is idempotent")
		})
	}
	scheduler.elapse()
	assert.Empty(t, presenceKinds(t, log, "system", "s1"))
}

// failingSessionLog fails every Append so the error path is exercised with a
// real SessionEventLog contract rather than a partial stub.
type failingSessionLog struct {
	SessionEventLog
	err error
}

func (f *failingSessionLog) Append(context.Context, *SessionEvent) error { return f.err }

func TestSessionPresence_AppendFailureIsReportedNotSwallowed(t *testing.T) {
	boom := errors.New("store unavailable")
	log := &failingSessionLog{SessionEventLog: NewMemorySessionEventLog(), err: boom}

	var reported []error
	presence, scheduler := newManualPresence(func(err error) { reported = append(reported, err) })

	leave := presence.Join(context.Background(), log, "system", "s1", "alice", "web")
	require.Len(t, reported, 1, "a failed join append must be reported")

	var appendErr *PresenceAppendError
	require.True(t, errors.As(reported[0], &appendErr))
	assert.Equal(t, KindOperatorJoined, appendErr.Kind)
	assert.Equal(t, "alice", appendErr.OperatorID)
	assert.Equal(t, "s1", appendErr.SessionID)
	assert.ErrorIs(t, appendErr, boom, "the store failure stays reachable through Unwrap")
	assert.Contains(t, appendErr.Error(), "alice")

	leave()
	scheduler.elapse()
	require.Len(t, reported, 2, "a failed departure append must be reported too")
}

// TestSessionPresence_NilErrorSinkIsSafe asserts a tracker built without a
// reporter still functions when appends fail.
func TestSessionPresence_NilErrorSinkIsSafe(t *testing.T) {
	log := &failingSessionLog{SessionEventLog: NewMemorySessionEventLog(), err: errors.New("boom")}
	presence, scheduler := newManualPresence(nil)

	assert.NotPanics(t, func() {
		leave := presence.Join(context.Background(), log, "system", "s1", "alice", "web")
		leave()
		scheduler.elapse()
	})
}

// TestNewSessionPresence_DefaultsLinger asserts a non-positive linger falls
// back to the documented default rather than committing departures instantly.
func TestNewSessionPresence_DefaultsLinger(t *testing.T) {
	for _, linger := range []time.Duration{0, -time.Second} {
		presence := NewSessionPresence(linger, nil)
		assert.Equal(t, DefaultPresenceLinger, presence.linger)
		presence.Close()
	}

	presence := NewSessionPresence(2*time.Second, nil)
	assert.Equal(t, 2*time.Second, presence.linger)
	presence.Close()
}

// TestSessionPresence_DefaultSchedulerDefersDeparture exercises the real
// time.AfterFunc path (the manual scheduler replaces it everywhere else) to
// prove production wiring actually defers rather than writing immediately.
func TestSessionPresence_DefaultSchedulerDefersDeparture(t *testing.T) {
	log := NewMemorySessionEventLog()
	presence := NewSessionPresence(20*time.Millisecond, nil)
	t.Cleanup(presence.Close)

	leave := presence.Join(context.Background(), log, "system", "s1", "alice", "web")
	leave()
	assert.Equal(t, []SessionEventKind{KindOperatorJoined}, presenceKinds(t, log, "system", "s1"))

	require.Eventually(t, func() bool {
		return len(presenceKinds(t, log, "system", "s1")) == 2
	}, 2*time.Second, 2*time.Millisecond, "the real timer must eventually commit the departure")
}
