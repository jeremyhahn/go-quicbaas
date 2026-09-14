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

// Package main provides tests for the shared passphrase input surface:
// --passphrase-stdin, QUICBAAS_BARRIER_PASSPHRASE and --passphrase.
package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// =============================================================================
// trimOneTrailingNewline
// =============================================================================

// TestTrimOneTrailingNewline covers the exact-one-terminator contract that
// makes `printf '%s'` and `echo` deliver the same passphrase.
func TestTrimOneTrailingNewline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"printf no newline", "s3cret", "s3cret"},
		{"echo one newline", "s3cret\n", "s3cret"},
		{"crlf", "s3cret\r\n", "s3cret"},
		{"only the last newline goes", "s3cret\n\n", "s3cret\n"},
		{"bare cr is content", "s3cret\r", "s3cret\r"},
		{"interior newline survives", "line1\nline2\n", "line1\nline2"},
		{"trailing spaces survive", "s3cret  \n", "s3cret  "},
		{"empty", "", ""},
		{"newline only", "\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, string(trimOneTrailingNewline([]byte(tc.in))))
		})
	}
}

// =============================================================================
// barrier unseal — the three input paths
// =============================================================================

// TestBarrierUnseal_PassphraseFromStdin unseals with the passphrase read
// from standard input, the way `printf '%s' "$PP" | quicbaas ...` supplies it.
func TestBarrierUnseal_PassphraseFromStdin(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(strings.NewReader("from-stdin"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastUnsealReq)
	assert.Equal(t, "from-stdin", fc.lastUnsealReq.Secret)
}

// TestBarrierUnseal_PassphraseFromStdinTrimsOneNewline proves the `echo`
// form and the `printf '%s'` form reach the server identically.
func TestBarrierUnseal_PassphraseFromStdinTrimsOneNewline(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(strings.NewReader("echoed\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastUnsealReq)
	assert.Equal(t, "echoed", fc.lastUnsealReq.Secret)
}

// TestBarrierUnseal_PassphraseFromEnv unseals with the passphrase in
// QUICBAAS_BARRIER_PASSPHRASE and no flag at all.
func TestBarrierUnseal_PassphraseFromEnv(t *testing.T) {
	defer resetBarrierFlags()
	t.Setenv(barrierPassphraseEnv, "from-env")
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastUnsealReq)
	assert.Equal(t, "from-env", fc.lastUnsealReq.Secret)
}

// TestBarrierUnseal_PassphrasePrecedence asserts stdin beats the
// environment and the environment beats the flag. Every source carries a
// distinct value, so the assertion can only pass for one ordering.
func TestBarrierUnseal_PassphrasePrecedence(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		stdin string
		env   string
		want  string
	}{
		{
			name:  "stdin beats env and flag",
			args:  []string{"--passphrase-stdin", "--passphrase", "from-flag"},
			stdin: "from-stdin\n",
			env:   "from-env",
			want:  "from-stdin",
		},
		{
			name: "env beats flag",
			args: []string{"--passphrase", "from-flag"},
			env:  "from-env",
			want: "from-env",
		},
		{
			name: "flag is used when it is the only source",
			args: []string{"--passphrase", "from-flag"},
			want: "from-flag",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetBarrierFlags()
			if tc.env != "" {
				t.Setenv(barrierPassphraseEnv, tc.env)
			}
			fc := &fakeBarrierClient{}
			defer withFakeBarrier(fc)()

			cmd := newBarrierUnsealCommand()
			cmd.SetArgs(tc.args)
			cmd.SetIn(strings.NewReader(tc.stdin))
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)

			require.NoError(t, cmd.Execute())
			require.NotNil(t, fc.lastUnsealReq)
			assert.Equal(t, tc.want, fc.lastUnsealReq.Secret)
		})
	}
}

// TestBarrierUnseal_EmptyStdinIsStillMissing keeps an empty pipe from
// unsealing with an empty passphrase.
func TestBarrierUnseal_EmptyStdinIsStillMissing(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(strings.NewReader("\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--passphrase is required")
	assert.Nil(t, fc.lastUnsealReq, "no unseal may be attempted without a passphrase")
}

// TestBarrierUnseal_MissingNamesEveryInputPath makes the "required" error
// point at the preferred path rather than only at the exposed one.
func TestBarrierUnseal_MissingNamesEveryInputPath(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--passphrase-stdin")
	assert.Contains(t, err.Error(), barrierPassphraseEnv)
}

// =============================================================================
// Bounded, cancellable stdin read
// =============================================================================

// TestBarrierUnseal_StdinOverLimitIsRejected pins the size cap. Without it
// `cat /dev/zero | quicbaas barrier unseal --passphrase-stdin` grows memory
// without bound; the read must fail closed instead, and send nothing.
func TestBarrierUnseal_StdinOverLimitIsRejected(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(strings.NewReader(strings.Repeat("a", maxPassphraseBytes+1)))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_input")
	assert.Contains(t, err.Error(), "exceeds")
	assert.Nil(t, fc.lastUnsealReq, "an over-limit read must not unseal")
}

// TestBarrierUnseal_StdinAtLimitIsAccepted is the other side of the cap: the
// boundary itself is valid input, so the limit rejects only what is over it.
// Without this the first test would also pass an implementation that rejected
// every passphrase.
//
// The line-terminator cases are the ones that actually bite. A maximum-length
// passphrase typed at a prompt arrives as 4096 bytes AND its terminator, and
// an implementation that measures before trimming rejects it — which is
// exactly the defect this covers.
func TestBarrierUnseal_StdinAtLimitIsAccepted(t *testing.T) {
	atLimit := strings.Repeat("a", maxPassphraseBytes)
	cases := []struct {
		name  string
		stdin string
	}{
		{"no terminator", atLimit},
		{"lf terminator", atLimit + "\n"},
		{"crlf terminator", atLimit + "\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetBarrierFlags()
			fc := &fakeBarrierClient{}
			defer withFakeBarrier(fc)()

			cmd := newBarrierUnsealCommand()
			cmd.SetArgs([]string{"--passphrase-stdin"})
			cmd.SetIn(strings.NewReader(tc.stdin))
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)

			require.NoError(t, cmd.Execute())
			require.NotNil(t, fc.lastUnsealReq)
			assert.Equal(t, atLimit, fc.lastUnsealReq.Secret)
		})
	}
}

// TestBarrierUnseal_StdinOverLimitWithTerminator keeps the trim from being a
// way round the cap: 4097 content bytes are over the limit whether or not a
// newline follows them.
func TestBarrierUnseal_StdinOverLimitWithTerminator(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(strings.NewReader(strings.Repeat("a", maxPassphraseBytes+1) + "\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
	assert.Nil(t, fc.lastUnsealReq)
}

// TestReadPassphraseLimited_OverLimitStallDoesNotWait pins the early bail.
// A writer that sends more than the cap and then stalls has already made
// the input invalid — no byte that could still arrive brings the trimmed
// length back under the limit — so the answer must be the over-limit error
// now, not a timeout later. Before the bail this blocked until the
// deadline, turning a deterministic rejection into a hang.
func TestReadPassphraseLimited_OverLimitStallDoesNotWait(t *testing.T) {
	// No deadline is set here: readPassphraseLimited is called directly, so
	// the only thing that can end this read is the over-limit bail itself.
	r := &blockingReader{partial: []byte(strings.Repeat("a", maxPassphraseBytes+1))}

	done := make(chan error, 1)
	go func() {
		buf, err := readPassphraseLimited(r, "barrier.unseal")
		buf.Destroy()
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds")
	case <-time.After(3 * time.Second):
		t.Fatal("an already-over-limit read must not wait for more input")
	}
}

// TestReadPassphraseLimited_CRLFAtTheLimitStillCompletes is the control for
// the bail: a 4096-byte passphrase whose terminator arrives as a split
// "\r\n" crosses the cap mid-terminator and must NOT be rejected. An
// over-eager bail that fired on any byte past the cap would break this.
func TestReadPassphraseLimited_CRLFAtTheLimitStillCompletes(t *testing.T) {
	atLimit := strings.Repeat("a", maxPassphraseBytes)

	buf, err := readPassphraseLimited(strings.NewReader(atLimit+"\r\n"), "barrier.unseal")
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Destroy()
	assert.Equal(t, maxPassphraseBytes, buf.Size())
}

// openLineReader delivers a newline-terminated line and then stays open
// forever without EOF, which is what a terminal in canonical mode does when
// the operator presses Enter but not Ctrl-D. A read that waited for EOF
// would hang here until the deadline; this test fails by timing out rather
// than by asserting, which is the honest failure for that defect.
type openLineReader struct {
	line    string
	sent    int
	release chan struct{}
}

// Read hands back the line one chunk at a time, then blocks forever.
func (r *openLineReader) Read(p []byte) (int, error) {
	if r.sent < len(r.line) {
		n := copy(p, r.line[r.sent:])
		r.sent += n
		return n, nil
	}
	<-r.release
	return 0, io.EOF
}

// TestBarrierUnseal_StdinAcceptsALineWithoutEOF is the interactive-terminal
// case: Enter must complete the read.
func TestBarrierUnseal_StdinAcceptsALineWithoutEOF(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	// A generous budget: if the read only completes because the deadline
	// fired, the command fails rather than passing slowly.
	orig := globalTimeout
	globalTimeout = 10 * time.Second
	defer func() { globalTimeout = orig }()

	r := &openLineReader{line: "typed-at-a-prompt\n", release: make(chan struct{})}
	defer close(r.release)

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(r)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	start := time.Now()
	require.NoError(t, cmd.Execute())
	assert.Less(t, time.Since(start), 5*time.Second,
		"the line must complete the read; waiting for EOF would take the full budget")
	require.NotNil(t, fc.lastUnsealReq)
	assert.Equal(t, "typed-at-a-prompt", fc.lastUnsealReq.Secret)
}

// TestReadPassphraseLimited_StopsAtTheFirstNewline proves the read does not
// consume bytes past the line it took. Over-reading would swallow input
// belonging to whatever reads stdin next.
func TestReadPassphraseLimited_StopsAtTheFirstNewline(t *testing.T) {
	r := strings.NewReader("first-line\nsecond-line\n")

	buf, err := readPassphraseLimited(r, "barrier.unseal")
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Destroy()
	// A test fixture, not a live secret, so reading it back is fine — and
	// checking the bytes rather than only the length is what catches a read
	// that returns the right number of wrong bytes.
	assert.Equal(t, "first-line", string(buf.Bytes()))

	rest, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "second-line\n", string(rest),
		"everything after the first newline must be left for the next reader")
}

// TestReadPassphraseLimited_EmptyInput covers both empty forms: a closed
// pipe and a bare newline. Neither is a passphrase, and neither is an error
// here — the caller's "required" check reports them.
func TestReadPassphraseLimited_EmptyInput(t *testing.T) {
	for _, in := range []string{"", "\n", "\r\n"} {
		t.Run("input "+strconv.Quote(in), func(t *testing.T) {
			buf, err := readPassphraseLimited(strings.NewReader(in), "barrier.unseal")
			require.NoError(t, err)
			assert.Nil(t, buf)
		})
	}
}

// blockingReader delivers a partial passphrase and then blocks, standing
// in for a pipe whose writer stopped mid-secret. Delivering bytes first is
// the point: that is the case where a read in flight is holding plaintext.
//
// It implements SetReadDeadline, so it is genuinely cancellable — the read
// returns os.ErrDeadlineExceeded by itself rather than being abandoned.
// That mirrors what boundedReader arranges for a real descriptor (a
// deadline directly, or poll(2) for process stdin), so the tests exercise
// the same shape the CLI does instead of a shape only tests can produce.
type blockingReader struct {
	partial  []byte
	sent     int
	deadline time.Time
}

// SetReadDeadline makes this reader bounded, which is what boundedReader
// looks for first.
func (r *blockingReader) SetReadDeadline(t time.Time) error {
	r.deadline = t
	return nil
}

// Read drains the partial secret — the caller reads a byte at a time, so
// the cursor must be a position, not a flag — and then blocks until the
// deadline, which it honours rather than blocking forever.
func (r *blockingReader) Read(p []byte) (int, error) {
	if r.sent < len(r.partial) {
		n := copy(p, r.partial[r.sent:])
		r.sent += n
		return n, nil
	}
	if r.deadline.IsZero() {
		return 0, io.EOF
	}
	if d := time.Until(r.deadline); d > 0 {
		time.Sleep(d)
	}
	return 0, os.ErrDeadlineExceeded
}

// fdOnlyReader exposes a descriptor but hides SetReadDeadline, forcing
// boundedReader down the poll(2) path — the one process stdin actually
// takes, and the one that would otherwise never be covered because
// os.Stdin cannot be swapped out inside a test binary.
type fdOnlyReader struct{ f *os.File }

// Read delegates to the file.
func (r *fdOnlyReader) Read(p []byte) (int, error) { return r.f.Read(p) }

// Fd exposes the descriptor for polling.
func (r *fdOnlyReader) Fd() uintptr { return r.f.Fd() }

// TestBarrierUnseal_StdinNeverEOFTimesOut pins the deadline. A pipe that
// never reaches EOF must not hold the CLI open indefinitely — before the
// timeout existed, this test would hang rather than fail.
func TestBarrierUnseal_StdinNeverEOFTimesOut(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	// Shrink the command budget the read inherits; restore it afterwards so
	// the package-level flag variable does not leak into the next test.
	orig := globalTimeout
	globalTimeout = 150 * time.Millisecond
	defer func() { globalTimeout = orig }()

	r := &blockingReader{partial: []byte("half-a-pass")}

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(r)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	start := time.Now()
	err := cmd.Execute()
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Nil(t, fc.lastUnsealReq, "a timed-out read must not unseal")
	assert.Less(t, elapsed, 10*time.Second,
		"the read must be bounded by the command timeout, not by EOF")
}

// TestReadPassphrase_DeadlineReaderUnblocksItself covers the path the real
// CLI takes. os.Stdin is a pipe here, which is pollable, so the deadline
// interrupts the blocked read in place: it unwinds normally and its own
// deferred Destroy wipes the partial secret, rather than being abandoned
// with plaintext nobody can reach. The fallback goroutine path cannot do
// that, which is why the deadline path exists.
func TestReadPassphrase_DeadlineReaderUnblocksItself(t *testing.T) {
	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = pr.Close() }()
	defer func() { _ = pw.Close() }()

	// Hold the write end open so the reader never sees EOF, and put a
	// partial secret in the pipe so the blocked read is holding plaintext.
	_, err = pw.WriteString("half-a-pass")
	require.NoError(t, err)

	start := time.Now()
	buf, err := readPassphrase(pr, "barrier.unseal", 150*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Nil(t, buf)
	assert.Contains(t, err.Error(), "timed out")
	assert.Contains(t, err.Error(), "timeout")
	assert.Less(t, elapsed, 5*time.Second, "the deadline must interrupt the read in place")

	// A deadline left on the file would make the next read fail instantly.
	// This one is given a live writer that closes, so it can only succeed if
	// the previous deadline was cleared.
	_, err = pw.WriteString("rest\n")
	require.NoError(t, err)
	require.NoError(t, pw.Close())

	buf2, err := readPassphrase(pr, "barrier.unseal", 2*time.Second)
	require.NoError(t, err)
	require.NotNil(t, buf2)
	defer buf2.Destroy()
	assert.Equal(t, len("rest"), buf2.Size(),
		"a stale read deadline would have failed this second read")
}

// settledGoroutines returns the goroutine count once it has stopped
// falling, so an unrelated goroutine winding down cannot skew a baseline.
func settledGoroutines(within time.Duration) int {
	deadline := time.Now().Add(within)
	last := runtime.NumGoroutine()
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		n := runtime.NumGoroutine()
		if n >= last {
			return n
		}
		last = n
	}
	return last
}

// stalledDescriptor returns a reader over a pipe holding a partial secret
// whose writer stays open, plus a cleanup. It exposes Fd() and hides
// SetReadDeadline, so it forces the poll path — the one process stdin
// takes, and the one the previous design could only handle by abandoning a
// worker goroutine. Using a deadline-capable reader here would prove
// nothing: the old design served those synchronously too.
func stalledDescriptor(t *testing.T) (*fdOnlyReader, func()) {
	t.Helper()
	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	_, err = pw.WriteString("half-a-pass")
	require.NoError(t, err)
	return &fdOnlyReader{f: pr}, func() {
		_ = pw.Close()
		_ = pr.Close()
	}
}

// TestReadPassphrase_TimeoutLeavesNoGoroutine is the lifecycle property.
// A timed-out read must leave NOTHING running: no worker parked on an
// uninterruptible read, and therefore no protected buffer held by one.
//
// It runs over a descriptor that cannot take a deadline, which is what
// makes it asymmetric — that is precisely the case the previous design
// handed to a worker and then abandoned, leaving the count above baseline
// until the reader was released by hand. There is no release step here:
// the read returns by itself, so anything still running is a leak.
func TestReadPassphrase_TimeoutLeavesNoGoroutine(t *testing.T) {
	if !pollSupported {
		t.Skip("descriptor polling is not wired up on this platform")
	}
	before := settledGoroutines(500 * time.Millisecond)

	r, cleanup := stalledDescriptor(t)
	defer cleanup()
	require.NotImplements(t, (*deadlineReader)(nil), r,
		"this test is meaningless unless the reader forces the poll path")

	start := time.Now()
	buf, err := readPassphrase(r, "barrier.unseal", 150*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Nil(t, buf, "a timed-out read must hand back no passphrase")
	assert.Contains(t, err.Error(), "timed out")
	assert.Less(t, elapsed, 5*time.Second, "poll must bound the read")

	assert.LessOrEqual(t, runtime.NumGoroutine(), before,
		"a timed-out read must leave no goroutine behind, and so no live protected buffer")
}

// TestReadPassphrase_RepeatedTimeoutsDoNotAccumulate is the exhaustion case
// from the review: many timed-out reads must not pile up goroutines or
// mlocked buffers. With a leak of one goroutine and one locked buffer per
// call, 50 iterations over the poll path show up plainly.
func TestReadPassphrase_RepeatedTimeoutsDoNotAccumulate(t *testing.T) {
	if !pollSupported {
		t.Skip("descriptor polling is not wired up on this platform")
	}
	before := settledGoroutines(500 * time.Millisecond)

	for i := 0; i < 50; i++ {
		func() {
			r, cleanup := stalledDescriptor(t)
			defer cleanup()
			buf, err := readPassphrase(r, "barrier.unseal", 5*time.Millisecond)
			require.Error(t, err)
			require.Nil(t, buf)
		}()
	}

	assert.LessOrEqual(t, runtime.NumGoroutine(), before,
		"repeated timeouts must not accumulate goroutines or locked buffers")
}

// withoutPollSupport simulates a build whose waitReadable cannot bound a
// read — Windows today. The predicate is a variable precisely so this
// branch is covered on a platform that DOES support polling; otherwise the
// fail-closed path would only ever run on the builds least likely to be
// exercised.
func withoutPollSupport(t *testing.T) {
	t.Helper()
	orig := pollBoundedReads
	pollBoundedReads = false
	t.Cleanup(func() { pollBoundedReads = orig })
}

// TestReadPassphrase_UnboundableDescriptorIsRefused is the fail-closed
// property. On a build with no bounded wait, a descriptor read cannot
// honour the timeout at all, so it must be refused BEFORE it starts rather
// than begun and left to run forever. A hang is the worse outcome: the
// operator gets no error, no output, and no way to tell what happened.
func TestReadPassphrase_UnboundableDescriptorIsRefused(t *testing.T) {
	withoutPollSupport(t)

	r, cleanup := stalledDescriptor(t)
	defer cleanup()

	start := time.Now()
	buf, err := readPassphrase(r, "barrier.unseal", 10*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Nil(t, buf)
	// Refused before any read: it cannot have waited on the stalled writer.
	assert.Less(t, elapsed, time.Second,
		"the refusal must come before the read, not after a timeout")

	assert.Contains(t, err.Error(), "invalid_input")
	assert.Contains(t, err.Error(), runtime.GOOS,
		"the error must name the platform the operator is on")
	assert.Contains(t, err.Error(), barrierPassphraseEnv,
		"the error must point at an input path that still works")
	assert.Contains(t, err.Error(), "--passphrase",
		"the error must point at the argv fallback too")
}

// TestReadPassphrase_DeadlineReadersWorkWithoutPollSupport is the control:
// the refusal is scoped to descriptors that cannot be bounded. A reader
// that accepts a deadline is bounded by the deadline itself and must keep
// working on every platform, so the fail-closed branch must not swallow it.
func TestReadPassphrase_DeadlineReadersWorkWithoutPollSupport(t *testing.T) {
	withoutPollSupport(t)

	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = pr.Close() }()
	defer func() { _ = pw.Close() }()

	_, err = pw.WriteString("still-works\n")
	require.NoError(t, err)

	buf, err := readPassphrase(pr, "barrier.unseal", 10*time.Second)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Destroy()
	assert.Equal(t, "still-works", string(buf.Bytes()))
}

// TestReadPassphrase_NonBlockingReadersWorkWithoutPollSupport keeps the
// refusal from over-reaching: a reader that is not a descriptor at all
// cannot block, so there is nothing to bound and nothing to refuse.
func TestReadPassphrase_NonBlockingReadersWorkWithoutPollSupport(t *testing.T) {
	withoutPollSupport(t)

	buf, err := readPassphrase(strings.NewReader("plain-reader\n"), "barrier.unseal", 10*time.Second)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Destroy()
	assert.Equal(t, "plain-reader", string(buf.Bytes()))
}

// TestBarrierUnseal_UnboundableStdinRefusedEndToEnd checks the operator
// actually sees the refusal, and that nothing is sent to the server.
func TestBarrierUnseal_UnboundableStdinRefusedEndToEnd(t *testing.T) {
	defer resetBarrierFlags()
	withoutPollSupport(t)
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	r, cleanup := stalledDescriptor(t)
	defer cleanup()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(r)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be bounded by a timeout")
	assert.Nil(t, fc.lastUnsealReq, "a refused read must not unseal")
}

// TestReadPassphrase_PollPathDeliversALine is the control for the poll
// path: bounding the read must not stop it working. A line written to the
// pipe completes the read with no EOF and well inside the budget.
func TestReadPassphrase_PollPathDeliversALine(t *testing.T) {
	if !pollSupported {
		t.Skip("descriptor polling is not wired up on this platform")
	}
	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = pr.Close() }()
	defer func() { _ = pw.Close() }()

	_, err = pw.WriteString("polled-passphrase\n")
	require.NoError(t, err)

	buf, err := readPassphrase(&fdOnlyReader{f: pr}, "barrier.unseal", 10*time.Second)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Destroy()
	assert.Equal(t, "polled-passphrase", string(buf.Bytes()))
}

// TestReadPassphrase_DeliversWithABudget pins the ordinary path: given a
// deadline the read comfortably beats, the passphrase comes back whole.
// Without it, an implementation that always timed out would satisfy the
// leak tests above.
func TestReadPassphrase_DeliversWithABudget(t *testing.T) {
	buf, err := readPassphrase(strings.NewReader("delivered-whole\n"), "barrier.unseal", 10*time.Second)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Destroy()
	assert.Equal(t, "delivered-whole", string(buf.Bytes()))
}

// TestPassphraseReadTimeout_UsesTheCommandBudget asserts the fallback rather
// than just that some duration comes back.
//
// This used to assert a two-variable precedence — `timeout` first, then
// globalTimeout — which encoded the #246 defect as intended behaviour: the
// first branch was unreachable-by-default only in the sense that it ALWAYS
// won, since cobra wrote 30s into `timeout` at flag-registration time and
// nothing on a barrier verb ever wrote it again. There is one variable now,
// and the root --timeout is what writes it.
func TestPassphraseReadTimeout_UsesTheCommandBudget(t *testing.T) {
	orig := globalTimeout
	defer func() { globalTimeout = orig }()

	globalTimeout = 7 * time.Second
	assert.Equal(t, 7*time.Second, passphraseReadTimeout())

	globalTimeout = 0
	assert.Equal(t, defaultPassphraseReadTimeout, passphraseReadTimeout())
}

// =============================================================================
// Protected memory
// =============================================================================

// TestResolvePassphrase_ReturnsProtectedMemory asserts the resolver hands
// back a live protected Buffer for each source rather than a Go string. It
// checks the size, not the content: conventions/testing-guidelines.md
// forbids copying a secret out of protected storage to assert on it.
func TestResolvePassphrase_ReturnsProtectedMemory(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		stdin string
		env   string
		size  int
	}{
		{name: "stdin", args: []string{"--passphrase-stdin"}, stdin: "abcdef\n", size: 6},
		{name: "env", args: []string{}, env: "abcdefgh", size: 8},
		{name: "flag", args: []string{"--passphrase", "abc"}, size: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetBarrierFlags()
			if tc.env != "" {
				t.Setenv(barrierPassphraseEnv, tc.env)
			}
			cmd := newBarrierUnsealCommand()
			require.NoError(t, cmd.Flags().Parse(tc.args))
			cmd.SetIn(strings.NewReader(tc.stdin))

			buf, err := resolvePassphrase(cmd, "barrier.unseal", barrierSecret)
			require.NoError(t, err)
			require.NotNil(t, buf)
			assert.Equal(t, tc.size, buf.Size())

			buf.Destroy()
			assert.Equal(t, 0, buf.Size(), "Destroy must release the buffer")
		})
	}
}

// TestPassphraseForStrategy_DestroysTheDroppedBuffer proves the dropped
// passphrase is zeroized rather than merely unreferenced — leaving it to the
// garbage collector is the exact failure protected memory exists to prevent.
func TestPassphraseForStrategy_DestroysTheDroppedBuffer(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierEnableEncryptionCommand()
	require.NoError(t, cmd.Flags().Parse([]string{"--strategy", "shamir"}))

	buf, err := secure.NewBufferFromBytes([]byte("ambient-passphrase"))
	require.NoError(t, err)
	require.NotZero(t, buf.Size())

	got := passphraseForStrategy(cmd, "shamir", buf)
	assert.Nil(t, got, "a strategy that takes no passphrase gets none")
	assert.Equal(t, 0, buf.Size(), "the dropped passphrase must be destroyed, not just dropped")
}

// TestPassphraseForStrategy_KeepsTheBufferItReturns is the control: when the
// strategy does take a passphrase, the same buffer comes back alive. Without
// it, an implementation that destroyed everything would pass the test above.
func TestPassphraseForStrategy_KeepsTheBufferItReturns(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierEnableEncryptionCommand()
	require.NoError(t, cmd.Flags().Parse([]string{"--strategy", "passphrase"}))

	buf, err := secure.NewBufferFromBytes([]byte("real-passphrase"))
	require.NoError(t, err)
	defer buf.Destroy()

	got := passphraseForStrategy(cmd, "passphrase", buf)
	require.NotNil(t, got)
	assert.Equal(t, len("real-passphrase"), got.Size())
}

// TestPassphraseString_EmptyOnNilAndDestroyed keeps the transport-boundary
// conversion from turning an absent or released passphrase into anything but
// the empty string.
func TestPassphraseString_EmptyOnNilAndDestroyed(t *testing.T) {
	assert.Equal(t, "", passphraseString(nil))

	buf, err := secure.NewBufferFromBytes([]byte("gone"))
	require.NoError(t, err)
	buf.Destroy()
	assert.Equal(t, "", passphraseString(buf))
}

// =============================================================================
// Help text
// =============================================================================

// TestPassphraseHelpNamesTheExposure asserts every verb that accepts a
// passphrase on argv warns that argv is world-readable, and offers the
// stdin path. A verb added later without addPassphraseFlags fails here.
func TestPassphraseHelpNamesTheExposure(t *testing.T) {
	cases := []struct {
		name string
		cmd  func() *cobra.Command
	}{
		{"barrier init", newBarrierInitCommand},
		{"barrier init-shamir", newBarrierInitShamirCommand},
		{"barrier unseal", newBarrierUnsealCommand},
		{"barrier enable-encryption", newBarrierEnableEncryptionCommand},
		{"tenant barrier init", newTenantBarrierInitCommand},
		{"tenant barrier unseal", newTenantBarrierUnsealCommand},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetBarrierFlags()
			defer resetTenantBarrierFlags()
			cmd := tc.cmd()

			flag := cmd.Flags().Lookup(passphraseFlag)
			require.NotNil(t, flag, "--passphrase must exist for compatibility")
			assert.Contains(t, flag.Usage, "visible in the process list",
				"--passphrase help must warn about the process list")

			stdinFlag := cmd.Flags().Lookup(passphraseStdinFlag)
			require.NotNil(t, stdinFlag, "--passphrase-stdin must exist")

			assert.Contains(t, cmd.Long, "--passphrase-stdin",
				"long help must document the preferred path")
			assert.Contains(t, cmd.Long, barrierPassphraseEnv,
				"long help must document the environment variable")
		})
	}
}

// =============================================================================
// The other passphrase verbs
// =============================================================================

// TestBarrierInit_PassphraseFromStdin initializes with a piped passphrase.
func TestBarrierInit_PassphraseFromStdin(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierInitCommand()
	cmd.SetArgs([]string{"--barrier-strategy", "passphrase", "--passphrase-stdin"})
	cmd.SetIn(strings.NewReader("init-from-stdin\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitReq)
	assert.Equal(t, "init-from-stdin", fc.lastInitReq.Secret)
}

// TestBarrierInit_NoneIgnoresAmbientEnv guards the design choice that the
// "--passphrase is not allowed" rejection keys off what the operator typed:
// an exported QUICBAAS_BARRIER_PASSPHRASE left over from an unseal must not
// break `--barrier-strategy=none`, and must not reach the server either.
func TestBarrierInit_NoneIgnoresAmbientEnv(t *testing.T) {
	defer resetBarrierFlags()
	t.Setenv(barrierPassphraseEnv, "left-over")
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierInitCommand()
	cmd.SetArgs([]string{"--barrier-strategy", "none"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitReq)
	assert.Empty(t, fc.lastInitReq.Secret)
}

// TestBarrierInit_NoneRejectsStdinPassphrase keeps the explicit form of the
// rejection: asking for a passphrase on a strategy that forbids one is an
// operator error however the passphrase was going to arrive.
func TestBarrierInit_NoneRejectsStdinPassphrase(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierInitCommand()
	cmd.SetArgs([]string{"--barrier-strategy", "none", "--passphrase-stdin"})
	cmd.SetIn(strings.NewReader("nope\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed when --barrier-strategy=none")
	assert.Nil(t, fc.lastInitReq)
}

// TestBarrierInitShamir_PassphraseFromEnv seeds the Shamir split from the
// environment.
func TestBarrierInitShamir_PassphraseFromEnv(t *testing.T) {
	defer resetBarrierFlags()
	t.Setenv(barrierPassphraseEnv, "shamir-seed")
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierInitShamirCommand()
	cmd.SetArgs([]string{"--threshold", "2", "--total", "3"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitShamirReq)
	assert.Equal(t, "shamir-seed", fc.lastInitShamirReq.Secret)
}

// TestBarrierEnableEncryption_PassphraseFromStdin migrates with a piped
// passphrase.
func TestBarrierEnableEncryption_PassphraseFromStdin(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierEnableEncryptionCommand()
	cmd.SetArgs([]string{"--strategy", "passphrase", "--passphrase-stdin"})
	cmd.SetIn(strings.NewReader("migrate-pp\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastEnableEncReq)
	assert.Equal(t, "migrate-pp", fc.lastEnableEncReq.Passphrase)
}

// TestBarrierEnableEncryption_ShamirIgnoresAmbientEnv pins the leak this
// surface would otherwise open: --strategy=shamir takes no passphrase, so an
// exported QUICBAAS_BARRIER_PASSPHRASE left over from an unseal must not ride
// along with the migration request.
func TestBarrierEnableEncryption_ShamirIgnoresAmbientEnv(t *testing.T) {
	defer resetBarrierFlags()
	t.Setenv(barrierPassphraseEnv, "left-over")
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierEnableEncryptionCommand()
	cmd.SetArgs([]string{"--strategy", "shamir", "--threshold", "2", "--total", "3"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastEnableEncReq)
	assert.Empty(t, fc.lastEnableEncReq.Passphrase,
		"an ambient passphrase must not be sent with a Shamir migration")
}

// TestBarrierEnableEncryption_ShamirKeepsATypedPassphrase is the other half
// of the rule: only the AMBIENT source is dropped. A passphrase the operator
// typed on this invocation still reaches the server, which stays the
// authority on which strategies accept one.
func TestBarrierEnableEncryption_ShamirKeepsATypedPassphrase(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierEnableEncryptionCommand()
	cmd.SetArgs([]string{
		"--strategy", "shamir", "--threshold", "2", "--total", "3",
		"--passphrase", "typed-here",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastEnableEncReq)
	assert.Equal(t, "typed-here", fc.lastEnableEncReq.Passphrase)
}

// TestTenantBarrierInit_ShamirIgnoresAmbientEnv is the same guard on the
// per-tenant surface.
func TestTenantBarrierInit_ShamirIgnoresAmbientEnv(t *testing.T) {
	defer resetTenantBarrierFlags()
	t.Setenv(barrierPassphraseEnv, "left-over")
	fc := &fakeTenantBarrierClient{}
	defer withFakeTenantBarrier(fc)()
	tenantBarrierTenantID = "acme"

	cmd := newTenantBarrierInitCommand()
	cmd.SetArgs([]string{"--strategy", "shamir", "--threshold", "2", "--shares", "3"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitReq)
	assert.Empty(t, fc.lastInitReq.Passphrase,
		"an ambient passphrase must not be sent with a Shamir tenant barrier")
}

// TestTenantBarrierUnseal_PassphraseFromStdin unseals a tenant barrier with
// a piped passphrase.
func TestTenantBarrierUnseal_PassphraseFromStdin(t *testing.T) {
	defer resetTenantBarrierFlags()

	// The key is captured DURING the call. Reading it afterwards is not a
	// test artefact to work around: the command wipes its copy on return, so
	// a value still readable after Execute would mean the passphrase had
	// been left lying in unprotected memory.
	var seen string
	fc := &fakeTenantBarrierClient{
		unsealFunc: func(_ context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error {
			seen = string(req.Key)
			return nil
		},
	}
	defer withFakeTenantBarrier(fc)()
	tenantBarrierTenantID = "acme"

	cmd := newTenantBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(strings.NewReader("tenant-pp\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "tenant-pp", seen)
}

// TestTenantBarrierUnseal_KeyStaysInProtectedMemory pins the property that
// makes this the one transport field with no residue: the passphrase is
// handed over as the mlocked slice itself, never copied into ordinary heap
// memory and never turned into a Go string.
//
// The assertion has to happen DURING the call, and that is the contract,
// not a test workaround. req.Key aliases a guard-paged buffer the command
// destroys when it returns, so reading it afterwards is a use-after-free —
// it faults rather than returning stale bytes. An earlier revision of this
// test asserted after Execute and reproduced exactly that as a SIGSEGV.
// Nothing may read req.Key outside the call; the real transports do not,
// which is why passing the protected slice is safe (see the trace at
// runTenantBarrierUnsealCommand).
func TestTenantBarrierUnseal_KeyStaysInProtectedMemory(t *testing.T) {
	defer resetTenantBarrierFlags()

	var seen string
	var sawKey bool
	fc := &fakeTenantBarrierClient{
		unsealFunc: func(_ context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error {
			sawKey = len(req.Key) > 0
			seen = string(req.Key)
			return nil
		},
	}
	defer withFakeTenantBarrier(fc)()
	tenantBarrierTenantID = "acme"

	cmd := newTenantBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase-stdin"})
	cmd.SetIn(strings.NewReader("tenant-pp\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.True(t, sawKey, "the request must carry the passphrase during the call")
	assert.Equal(t, "tenant-pp", seen)
	assert.NotContains(t, out.String(), "tenant-pp",
		"the passphrase must never reach the terminal")
}

// TestTenantBarrierInit_PassphraseFromEnv initializes a tenant barrier from
// the environment.
func TestTenantBarrierInit_PassphraseFromEnv(t *testing.T) {
	defer resetTenantBarrierFlags()
	t.Setenv(barrierPassphraseEnv, "tenant-env-pp")
	fc := &fakeTenantBarrierClient{}
	defer withFakeTenantBarrier(fc)()
	tenantBarrierTenantID = "acme"

	cmd := newTenantBarrierInitCommand()
	cmd.SetArgs([]string{"--strategy", "passphrase"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitReq)
	assert.Equal(t, "tenant-env-pp", fc.lastInitReq.Passphrase)
}
