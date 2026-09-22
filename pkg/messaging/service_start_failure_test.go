// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// service_start_failure_test.go pins the half of PushService.Start that only
// runs when Start fails: it leaves NOTHING behind (go-quicbaas#62).
//
// Start launches the hub goroutine before it can still fail, and both of its
// failure exits used to return with that goroutine on the stack and its
// context uncancelled. Nor could the caller clean up afterwards — ps.running
// is stored only on success and Stop returns immediately while it is false,
// so the disciplined Stop-on-error got a no-op, and Server.startMessaging
// assigns s.messagingService only after Start returns, so the server's own
// Shutdown found a nil field. The hub was unreachable from every teardown
// path there was.
//
// The tests below drive the readiness-wait failure and assert the goroutine
// is gone by the time Start returns — not merely told to stop.
package messaging

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hubRunFrame is the frame of the goroutine PushService.Start launches. It is
// the observable that proves the hub loop is still alive, as opposed to
// merely still referenced.
const hubRunFrame = "messaging.(*NotificationHub).Run"

// startJoinFrame is the frame of Start itself. Combined with the chan-receive
// state it identifies a Start parked on the join in its failure unwind, which
// is the only channel receive Start ever performs (its readiness wait is a
// select, which parks in a different state).
const startJoinFrame = "messaging.(*PushService).Start"

// goroutineBlocks splits a full process stack dump into per-goroutine blocks.
// Each block begins with a header line of the form
// "goroutine 42 [chan receive]:", so a block carries both the state and the
// frames, which is what lets the helpers below ask about them together.
func goroutineBlocks() []string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Split(string(buf[:n]), "\n\n")
		}
		buf = make([]byte, 2*len(buf))
	}
}

// countGoroutinesInFrame returns how many goroutines process-wide have frame
// on their stack. Tests compare it against a baseline taken in the same test
// rather than against zero: other tests in this package leave hubs running
// for the length of their own case, and a count is only meaningful as a
// delta.
func countGoroutinesInFrame(frame string) int {
	var n int
	for _, block := range goroutineBlocks() {
		if strings.Contains(block, frame) {
			n++
		}
	}
	return n
}

// aGoroutineIsParkedIn reports whether some goroutine is blocked in state
// while frame is on its stack.
func aGoroutineIsParkedIn(frame, state string) bool {
	for _, block := range goroutineBlocks() {
		header, _, ok := strings.Cut(block, "\n")
		if !ok {
			continue
		}
		if strings.Contains(header, state) && strings.Contains(block, frame) {
			return true
		}
	}
	return false
}

// wedgeHubClaim stops the hub from ever coming up, by taking the very lock
// Run acquires as the first statement of its preamble. Run therefore parks
// BEFORE the compare-and-swap that would publish it, so IsRunning() stays
// false for as long as the wedge is held and PushService.Start's readiness
// wait cannot succeed.
//
// This is what makes the failure deterministic rather than a race against a
// goroutine that normally starts in microseconds. Returned release is safe to
// call more than once, so a failed assertion cannot leave the hub wedged for
// the rest of the run.
func wedgeHubClaim(t *testing.T, hub *NotificationHub) (release func()) {
	t.Helper()

	hub.lifecycleMu.Lock()
	var once sync.Once
	release = func() { once.Do(hub.lifecycleMu.Unlock) }
	t.Cleanup(release)
	return release
}

// TestPushService_Start_ReadinessWaitCancelled_JoinsHubGoroutine is the
// verification go-quicbaas#62 asks for on the messaging path: force the
// readiness wait to fail and assert no goroutine survives the Start that
// returned the error — WITHOUT any teardown call from the caller, because
// there is no teardown call that would work.
//
// Deterministic by construction, with no timing anywhere in it. The hub is
// wedged before its claim, so the readiness wait provably cannot succeed; the
// context is cancelled before Start is called, so the wait provably fails on
// its first pass rather than after a hundred of them. The wedge is released
// only once Start is observed parked on the join, which is both the gate that
// keeps the hub from coming up late and the assertion that Start really does
// wait for the goroutine rather than just signalling it.
func TestPushService_Start_ReadinessWaitCancelled_JoinsHubGoroutine(t *testing.T) {
	ps := NewPushService(newServiceTestLogger(t), nil)

	baseline := countGoroutinesInFrame(hubRunFrame)
	release := wedgeHubClaim(t, ps.hub)

	// Cancelled before Start, so the readiness wait's first select has its
	// ctx.Done arm already ready while IsRunning() is held false by the wedge.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ps.Start(ctx) }()

	// Start must reach its join and WAIT there. Until the wedge is released
	// the hub goroutine cannot exit, so a Start that merely cancelled and
	// returned would be sitting in errCh by now instead of parked here.
	require.Eventually(t,
		func() bool { return aGoroutineIsParkedIn(startJoinFrame, "[chan receive]") },
		testTimeout, testPollInterval,
		"a failed Start must block joining the hub goroutine it launched, not abandon it")

	select {
	case err := <-errCh:
		t.Fatalf("Start returned %v while its hub goroutine was still wedged: it did not join", err)
	default:
	}

	release()

	err := <-errCh
	require.ErrorIs(t, err, context.Canceled,
		"Start must surface the cancellation that aborted its readiness wait")

	// The point of the join: the goroutine is gone by the time the caller has
	// the error, so a caller that does nothing further leaks nothing.
	assert.Equal(t, baseline, countGoroutinesInFrame(hubRunFrame),
		"the hub goroutine a failed Start launched must be gone once Start has returned")
	assert.False(t, ps.hub.IsRunning(), "the hub must not be left running by a failed Start")
	assert.False(t, ps.running.Load(), "a failed Start must not mark the service running")

	// Teardown after a failed Start stays a no-op, which is the property that
	// made the leak unreachable and must not start hiding a panic instead.
	require.NoError(t, ps.Stop(context.Background()),
		"Stop after a failed Start must remain a safe no-op")
}

// TestPushService_Start_Success_LeavesHubRunning guards the unwind against
// the mirror defect it could introduce. The failure paths now cancel the
// service context and join the hub; a cancel that escaped onto the success
// path would take the hub down with it and every published message after a
// perfectly good Start would be refused.
func TestPushService_Start_Success_LeavesHubRunning(t *testing.T) {
	ps := NewPushService(newServiceTestLogger(t), nil)

	require.NoError(t, ps.Start(context.Background()), "an unobstructed Start must succeed")
	require.True(t, ps.hub.IsRunning(), "a successful Start must leave the hub running")
	require.True(t, ps.running.Load(), "a successful Start must mark the service running")
	require.NoError(t, ps.ctx.Err(), "a successful Start must not have cancelled its own context")

	require.NoError(t, ps.Stop(context.Background()))
	assert.False(t, ps.running.Load(), "Stop must clear the running flag")
}
