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

// Package main provides the shared passphrase-input path used by every
// QuicBaaS CLI verb that takes a barrier passphrase.
//
// A passphrase passed as --passphrase sits in /proc/<pid>/cmdline for the
// life of the call, where every process of every user on the host can read
// it, and it lands in the shell history besides. The three input paths and
// their precedence match xkey's `unlock --pin-stdin` / XKEY_PIN / --pin
// surface so operators meet one convention across the product family.
//
// # Protected memory
//
// Per conventions/security-review.md §6, a secret read into memory lives in
// protected storage for its whole lifetime and is zeroized on release. This
// file therefore resolves a passphrase into a *secure.Buffer — mlocked,
// guard-paged and wiped on Destroy — and never into a plain Go string. The
// owned abstraction for this layer is the xkms/qrdb `secure` package; the
// raw locked-memory primitive (memguard) is deliberately not imported here.
//
// Two residues are inherent to the sources rather than to this code, and
// are the reason --passphrase-stdin is the documented path:
//
//   - --passphrase arrives as an immutable Go string parsed out of argv,
//     which is already world-readable through /proc.
//   - QUICBAAS_BARRIER_PASSPHRASE arrives as an immutable Go string from the
//     process environment block.
//
// Neither string can be wiped, so both are copied into a Buffer at the
// earliest opportunity and everything downstream is protected. The stdin
// path never forms a Go string at all.
//
// The last conversion happens at the transport boundary: qrdb's
// BarrierUnsealRequest.Secret and friends are Go string fields, so
// passphraseString exists to make that one crossing explicit and auditable.
// That residue cannot be removed from this repo and is filed upstream as
// go-qrdb#184. The one field already typed []byte —
// TenantBarrierUnsealRequest.Key — needs no conversion at all: the
// protected slice crosses directly and the passphrase stays in mlocked
// memory for the whole call. See the note at that call site for why that
// is safe.
package main

import (
	stderrors "errors"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

const (
	// barrierPassphraseEnv names the environment variable that carries the
	// barrier passphrase when --passphrase-stdin is not used.
	barrierPassphraseEnv = "QUICBAAS_BARRIER_PASSPHRASE"

	// passphraseFlag is the compatibility flag that takes the passphrase on
	// argv.
	passphraseFlag = "passphrase"

	// passphraseStdinFlag selects the preferred standard-input path.
	passphraseStdinFlag = "passphrase-stdin"

	// maxPassphraseBytes caps what --passphrase-stdin will read. A barrier
	// passphrase or Shamir seed is a human-scale secret; 4 KiB is far above
	// any real one and far below anything that threatens the process. The
	// cap is what stops `cat /dev/zero | quicbaas barrier unseal
	// --passphrase-stdin` from growing memory without bound.
	maxPassphraseBytes = 4096

	// defaultPassphraseReadTimeout bounds the stdin read when no command
	// timeout is in force, so a pipe that never reaches EOF cannot hold the
	// CLI open forever.
	defaultPassphraseReadTimeout = 30 * time.Second

	// passphraseExposureWarning is appended to every --passphrase help
	// string. The wording is xkey's, verbatim except for the flag name.
	passphraseExposureWarning = "visible in the process list; prefer --passphrase-stdin"

	// passphraseStdinUsage is the help text for --passphrase-stdin.
	passphraseStdinUsage = "Read the passphrase from standard input " +
		"(preferred: keeps it out of the process list and the shell history)"

	// passphraseSourcesHint names every input path. It is appended to the
	// "required" errors so an operator who supplied nothing learns the
	// preferred path rather than only the exposed one.
	passphraseSourcesHint = " (supply it on stdin with --passphrase-stdin, " +
		"in " + barrierPassphraseEnv + ", or on argv with --passphrase)"

	// passphraseInputHelp documents the three input paths in the long help
	// of every verb that takes a passphrase.
	passphraseInputHelp = `The passphrase may be supplied three ways, in precedence order:

  --passphrase-stdin   read it from standard input (preferred: it keeps the
                       passphrase out of the process list and shell history)
  ` + barrierPassphraseEnv + `
                       environment variable
  --passphrase         flag value (visible in 'ps'; local development only)`
)

// addPassphraseFlags registers the passphrase input surface on cmd:
// --passphrase-stdin plus the compatibility --passphrase bound to target.
// desc names what the passphrase protects; the exposure warning is appended
// to it so the warning cannot drift between verbs.
func addPassphraseFlags(cmd *cobra.Command, target *string, desc string) {
	cmd.Flags().StringVar(target, passphraseFlag, "",
		desc+" ("+passphraseExposureWarning+")")
	cmd.Flags().Bool(passphraseStdinFlag, false, passphraseStdinUsage)
}

// passphraseRequested reports whether the operator asked this invocation to
// carry a passphrase at all. It looks at what was typed, not at what was
// resolved, so an ambient QUICBAAS_BARRIER_PASSPHRASE cannot turn a verb
// that forbids a passphrase into an error.
func passphraseRequested(cmd *cobra.Command) bool {
	if cmd.Flags().Changed(passphraseFlag) {
		return true
	}
	fromStdin, err := cmd.Flags().GetBool(passphraseStdinFlag)
	return err == nil && fromStdin
}

// resolvePassphrase returns the passphrase from the highest-precedence
// source supplied: --passphrase-stdin, then QUICBAAS_BARRIER_PASSPHRASE,
// then --passphrase. flagValue is the parsed --passphrase value; op names
// the verb for the error.
//
// The returned Buffer belongs to the caller, which MUST Destroy it on every
// exit path — `defer secret.Destroy()` immediately after the call. A nil
// Buffer means no source supplied one and is left for the caller's own
// "required" check; Destroy and Size are nil-safe, so the caller needs no
// special case for it.
func resolvePassphrase(cmd *cobra.Command, op, flagValue string) (*secure.Buffer, error) {
	// A command without the flag registered simply has no stdin path; that
	// is not an operator error, so it falls through to the env and the flag.
	// Registration itself is asserted per verb in passphrase_test.go.
	fromStdin, err := cmd.Flags().GetBool(passphraseStdinFlag)
	if err != nil {
		fromStdin = false
	}
	if fromStdin {
		return readPassphrase(cmd.InOrStdin(), op, passphraseReadTimeout())
	}
	if fromEnv := os.Getenv(barrierPassphraseEnv); fromEnv != "" {
		return protectExposedPassphrase(fromEnv, op)
	}
	if flagValue != "" {
		return protectExposedPassphrase(flagValue, op)
	}
	return nil, nil
}

// passphraseReadTimeout is the deadline for the stdin read. It reuses the
// command timeout the barrier verbs already give their RPCs so one budget
// covers the whole invocation, and falls back to a fixed default when the
// root --timeout flag has not been registered (as in a unit test that builds
// a bare command).
func passphraseReadTimeout() time.Duration {
	if globalTimeout > 0 {
		return globalTimeout
	}
	return defaultPassphraseReadTimeout
}

// deadlineReader is a reader that can bound its own blocking on a
// deadline — *os.File for a descriptor the Go netpoller owns, and the
// readers the tests inject.
type deadlineReader interface {
	SetReadDeadline(t time.Time) error
}

// fdReader is a reader backed by a file descriptor that can be polled.
// *os.File satisfies it, which is what lets process stdin be bounded even
// though it cannot take a deadline.
type fdReader interface {
	io.Reader
	Fd() uintptr
}

// pollBoundedReads reports whether this build can bound a read on a raw
// descriptor. It mirrors the build-tagged pollSupported constant, but as a
// variable so a test can exercise the refusal path on a platform that does
// support polling — otherwise the only coverage of the fail-closed branch
// would be on the builds least likely to be run.
var pollBoundedReads = pollSupported

// errUnboundedDescriptor is returned when --passphrase-stdin is pointed at
// a descriptor this build cannot bound: no deadline support and no poll.
// Reading anyway would ignore the timeout entirely, so the input path is
// refused instead of quietly becoming unbounded.
var errUnboundedDescriptor = stderrors.New(
	"reading a passphrase from this descriptor cannot be bounded by a timeout on this platform")

// pollingReader bounds each read by waiting for readability first, so the
// blocking happens in poll(2) — which takes a timeout — rather than inside
// a read that cannot be interrupted once it has begun.
type pollingReader struct {
	r fdReader
	// fd is captured once at construction. os.File.Fd has a documented side
	// effect — it takes a poller-managed file out of non-blocking mode —
	// so it is not something to call on every byte. Nothing is harmed here
	// (this path is only reached for a descriptor the poller does not
	// manage, which is how it got here), but calling it once is both
	// cheaper and free of that question.
	fd       uintptr
	deadline time.Time
}

// newPollingReader captures the descriptor and the deadline.
func newPollingReader(r fdReader, deadline time.Time) *pollingReader {
	return &pollingReader{r: r, fd: r.Fd(), deadline: deadline}
}

// Read waits for the descriptor to become readable, then performs a read
// that is known not to block.
//
// The wait and the read are not one atomic operation, so in principle
// another reader of the same descriptor could consume the byte in between
// and leave this read blocking. The CLI owns its own stdin for the length
// of this call, so that does not arise here.
//
// waitReadable returns nil for a hangup or an error condition as well as
// for readable data, which is deliberate: the following Read reports EOF
// or the real error, and each poll is followed by exactly one read, so the
// loop always advances or terminates rather than spinning.
func (p *pollingReader) Read(b []byte) (int, error) {
	if err := waitReadable(p.fd, p.deadline); err != nil {
		return 0, err
	}
	return p.r.Read(b)
}

// boundedReader returns a view of r whose reads will not block past
// deadline, together with a cleanup to run when the read is finished.
//
// Three cases, in order of preference:
//
//  1. The reader takes a deadline. Best case: the kernel interrupts the
//     read itself. Covers os.Pipe, sockets, and the test readers, on every
//     platform.
//  2. The reader is a descriptor that can be polled. This is process stdin
//     on unix, whose fd the Go runtime leaves in blocking mode so case 1
//     is unavailable. Each read waits for readability under the deadline
//     first.
//  3. A descriptor on a build with no bounded wait. Refused — see
//     errUnboundedDescriptor. Reading here would block with no timeout at
//     all, which is worse than not offering the input path.
//  4. Anything else: a reader that cannot block, such as the strings and
//     bytes readers the tests use. Passed through unchanged. No production
//     path reaches this — cmd.InOrStdin() is an *os.File.
//
// The point of every case is the same: the read happens on the CALLER'S
// goroutine and returns by itself. Nothing is handed to a worker that has
// to be abandoned on timeout, so no goroutine and no protected buffer can
// outlive the call.
func boundedReader(r io.Reader, deadline time.Time) (io.Reader, func(), error) {
	noop := func() {}

	if dr, ok := r.(deadlineReader); ok {
		if err := dr.SetReadDeadline(deadline); err == nil {
			// Clear it again so a shared descriptor is not left with a
			// stale deadline for whatever reads it next.
			return r, func() { _ = dr.SetReadDeadline(time.Time{}) }, nil
		}
	}

	if fr, ok := r.(fdReader); ok {
		if !pollBoundedReads {
			return nil, noop, errUnboundedDescriptor
		}
		return newPollingReader(fr, deadline), noop, nil
	}

	return r, noop, nil
}

// readPassphrase reads one passphrase from r, bounded by limit.
//
// It is deliberately synchronous. An earlier revision ran the read on a
// worker goroutine and enforced the deadline by abandoning it, which left
// a blocked goroutine holding a partially filled protected buffer that
// nobody could destroy — the read owns that buffer, and destroying memory
// another goroutine is still writing into is a use-after-free. Bounding
// the read itself removes the worker, and with it the leak.
//
// Where the read cannot be bounded at all it is refused before it starts,
// rather than begun and left to run forever. An operator on such a build
// gets an error naming the platform and the two input paths that still
// work, which is a better outcome than a command that hangs.
func readPassphrase(r io.Reader, op string, limit time.Duration) (*secure.Buffer, error) {
	br, cleanup, err := boundedReader(r, time.Now().Add(limit))
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInvalidInput, op,
			"--%s cannot be bounded by a timeout on %s; supply the passphrase "+
				"in %s, or on argv with --%s (%s)",
			passphraseStdinFlag, runtime.GOOS, barrierPassphraseEnv,
			passphraseFlag, passphraseExposureWarning)
	}
	defer cleanup()
	return readPassphraseLimited(br, op)
}

// readPassphraseLimited reads one passphrase straight into protected
// memory: everything up to the first newline, or to EOF, or to the cap,
// whichever comes first. Nothing is ever copied into an unprotected slice
// or converted to a Go string.
//
// Stopping at the newline is what makes an interactive terminal work. A
// tty in canonical mode delivers a line when the operator presses Enter
// and no EOF until they press Ctrl-D, so a read that waited for EOF would
// hang on every interactive invocation.
//
// The read is one byte at a time and deliberately not buffered. A
// bufio.Reader would do two unacceptable things here: consume bytes past
// the newline that belong to whatever reads stdin next, and stage the
// secret in an ordinary heap buffer it never wipes. That second reason is
// also why the stack's secure.StdinProvider is not used despite being the
// owned provider for this job — it reads through bufio. Reading directly
// into the mlocked allocation is strictly more protective while still
// going through the owned secure.Buffer surface rather than the raw
// locked-memory primitive. The provider's heap residue is filed upstream
// as go-qrdb#184.
func readPassphraseLimited(r io.Reader, op string) (*secure.Buffer, error) {
	// Two bytes over the cap so a maximum-length passphrase and its line
	// terminator both fit and can be told apart from genuine overflow:
	// 4096 content bytes plus "\r\n" is a valid input, not an over-limit
	// one, and the check below runs on the trimmed length.
	scratch, err := secure.NewBuffer(maxPassphraseBytes + 2)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, op,
			"failed to allocate protected memory for the passphrase")
	}
	defer scratch.Destroy()

	dst := scratch.Bytes()
	n := 0
	for n < len(dst) {
		m, rerr := r.Read(dst[n : n+1])
		n += m
		if m > 0 && dst[n-1] == '\n' {
			break
		}
		// Report overflow the moment it is certain rather than blocking for
		// input that could not rescue it. Past the cap, the only thing that
		// can still bring the trimmed length back under it is a "\r\n" that
		// has begun — so a byte over the cap that is not '\r' (and not '\n',
		// which broke above) settles it. Without this, a stalled writer that
		// has already sent too much waits for the deadline instead of
		// getting a deterministic answer.
		if m > 0 && n > maxPassphraseBytes && dst[n-1] != '\r' {
			return nil, errors.Newf(errors.ErrInvalidInput, op,
				"passphrase on stdin exceeds the %d-byte limit", maxPassphraseBytes)
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			// The deadline set by readPassphrase surfaces here. Reporting
			// it as a timeout rather than a read failure keeps the
			// operator-facing message the same on both cancellation paths.
			if stderrors.Is(rerr, os.ErrDeadlineExceeded) {
				return nil, errors.Newf(errors.ErrTimeout, op,
					"timed out waiting for the passphrase on stdin")
			}
			return nil, errors.Wrapf(rerr, errors.ErrInvalidInput, op,
				"failed to read the passphrase from stdin")
		}
	}

	trimmed := trimOneTrailingNewline(dst[:n])
	if len(trimmed) > maxPassphraseBytes {
		return nil, errors.Newf(errors.ErrInvalidInput, op,
			"passphrase on stdin exceeds the %d-byte limit", maxPassphraseBytes)
	}
	if len(trimmed) == 0 {
		// Empty input is a missing passphrase; the caller's own "required"
		// check names every source, so it reports this better than we can.
		return nil, nil
	}

	// NewBufferFromBytes copies into its own mlocked allocation and wipes
	// the source; the source here is a window into scratch, which Destroy
	// then wipes in full.
	buf, err := secure.NewBufferFromBytes(trimmed)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, op,
			"failed to move the passphrase into protected memory")
	}
	return buf, nil
}

// protectExposedPassphrase moves a passphrase that already exists as an
// immutable Go string into protected memory. The environment block and
// argv both deliver one that way and neither can be wiped, so the residual
// string outlives this call — that exposure is inherent to those two
// sources and is exactly why --passphrase-stdin is documented first.
func protectExposedPassphrase(exposed, op string) (*secure.Buffer, error) {
	scratch := []byte(exposed)
	buf, err := secure.NewBufferFromBytes(scratch)
	if err != nil {
		secure.Wipe(scratch)
		return nil, errors.Wrapf(err, errors.ErrInternal, op,
			"failed to move the passphrase into protected memory")
	}
	return buf, nil
}

// passphraseForStrategy drops a passphrase that no source on this
// invocation actually asked to send, destroying it rather than returning
// it. Verbs that select a strategy accept a passphrase only for the
// passphrase strategy, so an exported QUICBAAS_BARRIER_PASSPHRASE left over
// from an unseal must not ride along with a Shamir or hardware operation
// that has nothing to do with it. A passphrase the operator typed here is
// still forwarded — the server stays the authority on which strategies
// accept one.
func passphraseForStrategy(cmd *cobra.Command, strategy string, secret *secure.Buffer) *secure.Buffer {
	if isPassphraseStrategyName(strategy) || passphraseRequested(cmd) {
		return secret
	}
	secret.Destroy()
	return nil
}

// passphraseString converts the protected passphrase for a transport field
// that is typed as a Go string.
//
// This is the single place in the CLI where a passphrase leaves protected
// memory, kept in one function so it stays auditable. It cannot be removed
// from this repo: qrdb's BarrierUnsealRequest.Secret,
// BarrierInitializeRequest.Secret, BarrierInitializeShamirRequest.Secret,
// BarrierEnableEncryptionRequest.Passphrase and
// TenantBarrierInitRequest.Passphrase are all `string` in
// go-qrdb/sdk/go@v0.10.0-alpha, and a Go string is immutable, so the copy
// cannot be wiped. Filed upstream as go-qrdb#184 (type the fields []byte,
// or give them a wipeable field type that never forms a string during
// serialization); until that lands the residue is documented rather than
// claimed to be solved.
//
// A field already typed []byte — TenantBarrierUnsealRequest.Key — needs no
// conversion at all and keeps the passphrase in protected memory for the
// whole call. See the note at that call site.
func passphraseString(secret *secure.Buffer) string {
	b := secret.Bytes()
	if len(b) == 0 {
		return ""
	}
	return string(b)
}

// trimOneTrailingNewline removes exactly one trailing line terminator,
// "\n" or "\r\n", returning a window into b. Trimming one and only one is
// what lets `printf '%s'` and `echo` deliver the same passphrase while a
// passphrase that genuinely ends in a blank line keeps it.
func trimOneTrailingNewline(b []byte) []byte {
	if len(b) == 0 || b[len(b)-1] != '\n' {
		return b
	}
	b = b[:len(b)-1]
	if len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return b
}
