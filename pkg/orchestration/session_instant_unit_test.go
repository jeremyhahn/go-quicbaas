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

package orchestration

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// Every session instant is Unix nanoseconds (go-quicbaas#855)
// =============================================================================
//
// THE DEFECT THIS PINS was two fields of ONE session record disagreeing about
// their unit. Measured live on session bafb0b7d:
//
//	created_at    = 1789185149            seconds
//	last_activity = 1789185263664211875   nanoseconds
//
// created_at came from the codex-exec backend, which reported
// sess.created.Unix(); last_activity came from the orchestrator's own clock,
// which is UnixNano. The browser's formatter detects the unit by magnitude,
// so it rendered both plausibly — and that is the worst outcome available,
// because a reader believes a timestamp and has nothing to tell them one of
// the two is six decades adrift.
//
// A test that asserts "the value is large" would pass for a producer that
// multiplied seconds by a thousand. These assert the INSTANT: the value read
// back with time.Unix(0, n) must be the moment the call was made. That is the
// only property the consumers actually depend on.

// nanosNameTheMomentOf reports whether nanos, read as Unix nanoseconds, lands
// inside the window the call was made in. A seconds value fails it by ~55
// years and a milliseconds value by ~54; nothing rounds into passing.
func nanosNameTheMomentOf(t *testing.T, nanos int64, before, after time.Time) {
	t.Helper()
	if nanos == 0 {
		t.Fatalf("instant is unset; the caller observed a real moment")
	}
	got := time.Unix(0, nanos)
	if got.Before(before) || got.After(after) {
		t.Fatalf("instant %d read as Unix nanoseconds is %s, outside the call window [%s, %s] — the producer used another unit",
			nanos, got.UTC(), before.UTC(), after.UTC())
	}
}

func TestCodexExecBackendSessionInstantsAreNanoseconds(t *testing.T) {
	// The spawn instant. Reported in seconds, this is the field that made one
	// record carry two units.
	t.Run("spawn_reports_created_at_in_nanoseconds", func(t *testing.T) {
		backend := NewCodexExecBackend(CodexExecBackendConfig{})
		before := time.Now().Add(-time.Second)
		session, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: t.TempDir()})
		if err != nil {
			t.Fatalf("spawn: %s", err)
		}
		after := time.Now().Add(time.Second)
		nanosNameTheMomentOf(t, session.CreatedAt, before, after)
	})

	// The activity instant, from the same backend. Both are asserted because
	// the defect was not "a wrong field" but "two fields that disagree".
	t.Run("status_reports_last_activity_in_nanoseconds", func(t *testing.T) {
		backend := NewCodexExecBackend(CodexExecBackendConfig{})
		session, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: t.TempDir()})
		if err != nil {
			t.Fatalf("spawn: %s", err)
		}
		before := time.Now().Add(-time.Second)
		status, err := backend.Status(context.Background(), session.ID)
		if err != nil {
			t.Fatalf("status: %s", err)
		}
		after := time.Now().Add(time.Second)
		nanosNameTheMomentOf(t, status.LastActivity, before, after)
	})

	// THE PROPERTY THE ISSUE IS ABOUT: one session, two instants, one unit.
	// A record whose two timestamps describe the same moment must say so in
	// the same unit — the difference between them has to be a plausible
	// elapsed time, not five decades.
	t.Run("the_two_instants_of_one_session_agree_about_their_unit", func(t *testing.T) {
		backend := NewCodexExecBackend(CodexExecBackendConfig{})
		session, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: t.TempDir()})
		if err != nil {
			t.Fatalf("spawn: %s", err)
		}
		status, err := backend.Status(context.Background(), session.ID)
		if err != nil {
			t.Fatalf("status: %s", err)
		}
		elapsed := time.Unix(0, status.LastActivity).Sub(time.Unix(0, session.CreatedAt))
		if elapsed < 0 || elapsed > time.Minute {
			t.Fatalf("created_at=%d and last_activity=%d are %s apart; two instants of one session, recorded moments apart, are in different units",
				session.CreatedAt, status.LastActivity, elapsed)
		}
	})

	// A session that never existed has no instants to report, and Status must
	// say so rather than answer for the epoch.
	t.Run("status_refuses_an_unknown_session", func(t *testing.T) {
		backend := NewCodexExecBackend(CodexExecBackendConfig{})
		if _, err := backend.Status(context.Background(), "no-such-session"); err == nil {
			t.Fatal("expected an error for a session the backend never spawned")
		}
	})

	// A spawn with no work directory is refused, so no session — and no
	// instant — is invented for it.
	t.Run("spawn_refuses_a_request_without_a_work_dir", func(t *testing.T) {
		backend := NewCodexExecBackend(CodexExecBackendConfig{})
		if _, err := backend.Spawn(context.Background(), &SpawnRequest{}); err == nil {
			t.Fatal("expected an error for a spawn with no work directory")
		}
	})
}

// =============================================================================
// The HOST ACP backend, which is the one the dispatcher actually uses
// =============================================================================
//
// THE TEST ABOVE PASSED WHILE THE PRIMARY BACKEND WAS STILL WRONG, and that is
// the more interesting defect. It drives NewCodexExecBackend only, so it proved
// the unit for the backend that had just been converted and said nothing about
// the four other spawn paths. Caught in review of PR#859.
//
// The host ACP backend has five spawn paths. Two already used UnixNano
// (spawnContainerJSONRPC, spawnOneShotCLI); three used Unix — spawnJSONRPC,
// which is the dispatcher's default for SandboxModeNone, spawnCLI, and
// spawnContainerCLI.
//
// Converting Prompt's lastActivity to nanoseconds WITHOUT those three was
// worse than converting none of them: a spawnJSONRPC session then carried
// createdAt in seconds and, after its first prompt, lastActivity in
// nanoseconds. Two fields of one record in two units is not a step toward
// fixing go-quicbaas#855 — it IS go-quicbaas#855, reintroduced through the
// door built to close it.
//
// These cases drive Spawn through the package's own in-process ACP responder
// (mockTransportFactory in acp_backend_test.go), so the real spawnJSONRPC code
// path runs without a real agent process or a subprocess of any kind.

func TestACPBackendSessionInstantsAreNanoseconds(t *testing.T) {
	t.Run("spawn_reports_created_at_in_nanoseconds", func(t *testing.T) {
		backend, cleanup := newTestACPBackendWithMock(t)
		defer cleanup()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		before := time.Now().Add(-time.Second)
		session, err := backend.Spawn(ctx, &SpawnRequest{ProjectID: "p", WorkDir: t.TempDir(), Model: "m"})
		if err != nil {
			t.Fatalf("spawn: %s", err)
		}
		defer backend.Close(context.Background(), session.ID)
		after := time.Now().Add(time.Second)

		nanosNameTheMomentOf(t, session.CreatedAt, before, after)
	})

	t.Run("status_reports_last_activity_in_nanoseconds", func(t *testing.T) {
		backend, cleanup := newTestACPBackendWithMock(t)
		defer cleanup()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		before := time.Now().Add(-time.Second)
		session, err := backend.Spawn(ctx, &SpawnRequest{ProjectID: "p", WorkDir: t.TempDir(), Model: "m"})
		if err != nil {
			t.Fatalf("spawn: %s", err)
		}
		defer backend.Close(context.Background(), session.ID)

		status, err := backend.Status(ctx, session.ID)
		if err != nil {
			t.Fatalf("status: %s", err)
		}
		after := time.Now().Add(time.Second)

		nanosNameTheMomentOf(t, status.LastActivity, before, after)
	})

	// THE REGRESSION THE REVIEW FOUND. A prompt writes lastActivity from its
	// own clock; the spawn wrote createdAt from another. If the two clocks
	// disagree about their unit, this is the record that proves it — and it is
	// the record the browser renders side by side.
	t.Run("the_two_instants_of_one_session_agree_after_a_prompt", func(t *testing.T) {
		backend, cleanup := newTestACPBackendWithMock(t)
		defer cleanup()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		session, err := backend.Spawn(ctx, &SpawnRequest{ProjectID: "p", WorkDir: t.TempDir(), Model: "m"})
		if err != nil {
			t.Fatalf("spawn: %s", err)
		}
		defer backend.Close(context.Background(), session.ID)

		if _, err := backend.Prompt(ctx, &PromptRequest{SessionID: session.ID, Prompt: "hello"}); err != nil {
			t.Fatalf("prompt: %s", err)
		}

		status, err := backend.Status(ctx, session.ID)
		if err != nil {
			t.Fatalf("status: %s", err)
		}

		elapsed := time.Unix(0, status.LastActivity).Sub(time.Unix(0, session.CreatedAt))
		if elapsed < 0 || elapsed > time.Minute {
			t.Fatalf("created_at=%d and last_activity=%d are %s apart; the spawn and the prompt wrote one session's instants in different units",
				session.CreatedAt, status.LastActivity, elapsed)
		}
	})

	// Uptime is a duration in SECONDS — see the contract on SessionStatus. A
	// seconds clock less a nanosecond instant is a negative number the width
	// of the epoch, which is what subtracting them directly produced.
	//
	// THE SESSION IS BACKDATED, not slept on. A session spawned a moment ago
	// is ~0 seconds old AND ~0 milliseconds old, so asserting "small" against
	// a fresh session cannot tell the two units apart — it would pass whatever
	// the producer did. Backdating the record by a known age makes the unit
	// observable and keeps the test instant. The field is private and this
	// test is in the same package, so this reaches real state rather than
	// standing in for it.
	t.Run("status_reports_uptime_in_seconds_for_a_session_of_known_age", func(t *testing.T) {
		backend, cleanup := newTestACPBackendWithMock(t)
		defer cleanup()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		session, err := backend.Spawn(ctx, &SpawnRequest{ProjectID: "p", WorkDir: t.TempDir(), Model: "m"})
		if err != nil {
			t.Fatalf("spawn: %s", err)
		}
		defer backend.Close(context.Background(), session.ID)

		const age = 90 * time.Second
		val, ok := backend.sessions.Load(session.ID)
		if !ok {
			t.Fatalf("the backend does not hold the session it just spawned")
		}
		sess, ok := val.(*acpSession)
		if !ok {
			t.Fatalf("session record has type %T", val)
		}
		sess.createdAt = time.Now().Add(-age).UnixNano()

		status, err := backend.Status(ctx, session.ID)
		if err != nil {
			t.Fatalf("status: %s", err)
		}
		// 90 seconds, not 90_000 milliseconds and not 9e10 nanoseconds.
		if status.Uptime < 89 || status.Uptime > 95 {
			t.Fatalf("a session %s old reported uptime %d; the contract is a duration in SECONDS", age, status.Uptime)
		}
	})

	// A session the backend never spawned has no instants to report.
	t.Run("status_refuses_an_unknown_session", func(t *testing.T) {
		backend, cleanup := newTestACPBackendWithMock(t)
		defer cleanup()
		if _, err := backend.Status(context.Background(), "no-such-session"); err == nil {
			t.Fatal("expected an error for a session the backend never spawned")
		}
	})
}

// TestCodexExecBackendUptimeIsSeconds pins the field the review's second
// finding was about. The codex-exec backend reported Uptime in milliseconds —
// which is how the contract comment came to CLAIM milliseconds, inferred from
// the one producer that disobeyed it rather than from the REST handler that
// multiplies it by time.Second.
func TestCodexExecBackendUptimeIsSeconds(t *testing.T) {
	// Backdated for the same reason as the ACP case above: at ~0 age the two
	// candidate units are indistinguishable, so only a session of known age
	// can pin which one the producer used.
	t.Run("a_session_of_known_age_reports_that_age_in_seconds", func(t *testing.T) {
		backend := NewCodexExecBackend(CodexExecBackendConfig{})
		session, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: t.TempDir()})
		if err != nil {
			t.Fatalf("spawn: %s", err)
		}

		const age = 90 * time.Second
		val, ok := backend.sessions.Load(session.ID)
		if !ok {
			t.Fatalf("the backend does not hold the session it just spawned")
		}
		sess, ok := val.(*codexExecSession)
		if !ok {
			t.Fatalf("session record has type %T", val)
		}
		sess.created = time.Now().Add(-age)

		status, err := backend.Status(context.Background(), session.ID)
		if err != nil {
			t.Fatalf("status: %s", err)
		}
		// 90, not 90_000: this backend reported Milliseconds(), which is how
		// the contract comment came to claim milliseconds.
		if status.Uptime < 89 || status.Uptime > 95 {
			t.Fatalf("a session %s old reported uptime %d; the contract is a duration in SECONDS", age, status.Uptime)
		}
	})
}
