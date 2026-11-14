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

// Package poll is the bounded polling helper that replaces time.Sleep in
// tests (testing convention "No test sleeps", issue #448).
//
// A test that sleeps for background work passes or fails depending on how
// busy the machine is. A test that polls for the state it asserts only takes
// longer on a busy machine. The package imports nothing but the standard
// library, so any package's internal tests can use it without an import
// cycle.
package poll

import (
	"fmt"
	"testing"
	"time"
)

// Interval is how often a condition is re-evaluated.
const Interval = 5 * time.Millisecond

// Wait evaluates cond until it returns true or timeout passes, and reports
// whether cond held. cond is always evaluated at least once, and once more
// at the deadline, so a condition that becomes true just as the bound
// passes is not reported as a timeout.
func Wait(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		wait := Interval
		if left := time.Until(deadline); left < wait {
			wait = left
		}
		time.Sleep(wait) //nolint:forbidigo // the one sanctioned sleep: the poll interval
	}
}

// Until polls cond until it returns true, and fails the test when timeout
// passes first. msgAndArgs, when given, is a format string and its arguments
// describing what was awaited. Call it from the test's goroutine.
func Until(t testing.TB, timeout time.Duration, cond func() bool, msgAndArgs ...any) {
	t.Helper()
	if Wait(timeout, cond) {
		return
	}
	t.Fatalf("poll: condition not met within %v: %s", timeout, describe(msgAndArgs))
}

func describe(msgAndArgs []any) string {
	if len(msgAndArgs) == 0 {
		return "(no description)"
	}
	if format, ok := msgAndArgs[0].(string); ok {
		return fmt.Sprintf(format, msgAndArgs[1:]...)
	}
	return fmt.Sprint(msgAndArgs...)
}
