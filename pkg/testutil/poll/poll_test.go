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

package poll

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recorder is a testing.TB that records Fatalf instead of stopping the
// goroutine, so Until's failure path can be asserted.
type recorder struct {
	testing.TB
	failed string
}

func (r *recorder) Helper() {}

func (r *recorder) Fatalf(format string, args ...any) {
	r.failed = fmt.Sprintf(format, args...)
}

func TestWait_TrueImmediatelyEvaluatesOnce(t *testing.T) {
	var calls atomic.Int32
	ok := Wait(time.Hour, func() bool { calls.Add(1); return true })
	if !ok || calls.Load() != 1 {
		t.Fatalf("Wait = %v after %d calls, want true after 1", ok, calls.Load())
	}
}

func TestWait_SeesBackgroundWork(t *testing.T) {
	var done atomic.Bool
	go func() { done.Store(true) }()
	if !Wait(10*time.Second, done.Load) {
		t.Fatal("Wait did not observe the goroutine's store")
	}
}

func TestWait_FalseAfterBoundAndChecksAtDeadline(t *testing.T) {
	var calls atomic.Int32
	start := time.Now()
	ok := Wait(30*time.Millisecond, func() bool { calls.Add(1); return false })
	elapsed := time.Since(start)
	if ok {
		t.Fatal("Wait = true for a condition that never holds")
	}
	if elapsed < 30*time.Millisecond {
		t.Fatalf("Wait returned after %v, before its 30ms bound", elapsed)
	}
	if calls.Load() < 2 {
		t.Fatalf("cond evaluated %d times, want at least 2 (first and at deadline)", calls.Load())
	}
}

func TestWait_ZeroTimeoutEvaluatesOnce(t *testing.T) {
	var calls atomic.Int32
	if Wait(0, func() bool { calls.Add(1); return false }) {
		t.Fatal("Wait = true for a false condition")
	}
	if calls.Load() != 1 {
		t.Fatalf("cond evaluated %d times, want 1", calls.Load())
	}
}

func TestUntil_PassesWhenConditionHolds(t *testing.T) {
	r := &recorder{TB: t}
	Until(r, time.Second, func() bool { return true }, "never shown")
	if r.failed != "" {
		t.Fatalf("Until failed a held condition: %s", r.failed)
	}
}

func TestUntil_FailsWithDescription(t *testing.T) {
	r := &recorder{TB: t}
	Until(r, 10*time.Millisecond, func() bool { return false }, "%d events delivered", 3)
	if !strings.Contains(r.failed, "3 events delivered") || !strings.Contains(r.failed, "10ms") {
		t.Fatalf("failure message = %q, want the bound and the description", r.failed)
	}
}

func TestUntil_FailsWithoutDescription(t *testing.T) {
	r := &recorder{TB: t}
	Until(r, 0, func() bool { return false })
	if !strings.Contains(r.failed, "(no description)") {
		t.Fatalf("failure message = %q", r.failed)
	}
}

func TestDescribe_NonStringFirstArgument(t *testing.T) {
	if got := describe([]any{42, "x"}); got != fmt.Sprint(42, "x") {
		t.Fatalf("describe = %q", got)
	}
}
