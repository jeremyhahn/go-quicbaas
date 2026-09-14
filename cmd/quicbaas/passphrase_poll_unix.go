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

//go:build unix
// +build unix

package main

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// pollSupported reports whether waitReadable can bound a read on this
// platform. See passphrase_poll_stub.go for the platforms where it cannot.
const pollSupported = true

// waitReadable blocks until fd has data available or deadline passes,
// returning os.ErrDeadlineExceeded in the latter case.
//
// This is what makes a passphrase read on process stdin genuinely
// interruptible. os.Stdin is not registered with the Go netpoller — the
// runtime leaves fd 0 blocking — so SetReadDeadline is unavailable and a
// read that has begun cannot be stopped. Waiting for readability BEFORE
// each read inverts that: the blocking happens in poll(2), which takes a
// timeout, and the read that follows is known not to block. Nothing has to
// be abandoned, so no goroutine and no protected buffer outlives the call.
func waitReadable(fd uintptr, deadline time.Time) error {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return os.ErrDeadlineExceeded
		}

		// poll(2) takes whole milliseconds. Round UP: truncating would end
		// the wait before the deadline the caller asked for — a 1.9ms
		// remainder polled for 1ms and then reported as expired gives back
		// 0.9ms the caller was entitled to. Rounding up can only overshoot
		// by less than a millisecond, and the loop re-checks the deadline
		// before waiting again.
		ms := int((remaining + time.Millisecond - 1) / time.Millisecond)
		if ms < 1 {
			ms = 1
		}

		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, ms)
		if err != nil {
			if err == unix.EINTR {
				// A signal, not a result. The deadline is recomputed at the
				// top of the loop, so this cannot extend the budget.
				continue
			}
			return err
		}
		if n == 0 {
			return os.ErrDeadlineExceeded
		}
		// Readable, or hung up / errored — let the read report which.
		return nil
	}
}
