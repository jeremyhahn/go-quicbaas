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
	"errors"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// =============================================================================
// Owning the agent subprocess
// =============================================================================
//
// A persistent CLI session is an OS process this daemon started. Until this
// file existed, nothing owned it after the spawning goroutine returned:
//
//   - cmd.Wait was called only on a spawn-failure teardown, on a mid-flight
//     write/scan failure, and on Close. A session that was SUSPENDED — which is
//     what the SDLC module does to a work item parked on a question — kept its
//     process with nobody waiting on it at all.
//   - So a process that died, or was killed, became a zombie the daemon never
//     observed. Measured 2026-08-26 in the go-truststrap burn-in (#187): the
//     agent was SIGKILLed and stayed <defunct> for 21 minutes, and the daemon
//     went on reporting the session ACTIVE, because Status infers liveness from
//     cmd.ProcessState — a field only Wait populates.
//   - And Close read cmd.Wait directly while the mid-flight failure path could
//     be calling it concurrently, which cmd.Wait does not permit.
//
// The supervisor answers all three with one goroutine per persistent session:
// it performs THE single Wait, records what the process exited with, marks the
// session closed, and releases the detached process context so the exec
// package's own watchCtx goroutine returns. It outlives whatever goroutine
// spawned the session, which is the property the dispatch path could not
// provide — the run goroutine concludes its work item and returns while the
// agent process lives on into the next round.
//
// WAITING IS NOT KILLING. The supervisor never signals the process; it only
// observes it. Termination stays exactly where it was — Close, through
// procCancel — because a supervisor that could end a session would be a second
// opinion about a decision the session binding already arbitrates.

// ProcessExit is the observed termination of a session's agent subprocess.
//
// Recorded rather than merely acted on, because "the agent process is gone" is
// the fact the SDLC module most needs and least had: an operator reading a wedged
// item, the stall detector asking whether a session is live, and the run
// authority deciding whether a claim is backed all want the same answer, and
// each of them was previously left to infer it.
type ProcessExit struct {
	// SessionID is the session whose process this describes.
	SessionID string

	// TenantID owns that session, captured at spawn from the spawning
	// context. Carried because the session RECORD is tenant-keyed and the
	// observer that marks it closed has no request context of its own — the
	// exit arrives long after the spawn returned, on the supervisor's
	// goroutine.
	TenantID string

	// PID is the process that exited, as the daemon knew it.
	PID int

	// ExitCode is the code the process exited with, or -1 when it was killed
	// by a signal or never ran.
	ExitCode int

	// Err is what Wait reported, if anything. A non-nil Err with a zero
	// ExitCode means the wait itself failed rather than the process.
	Err error

	// THERE IS DELIBERATELY NO STDERR FIELD HERE.
	//
	// An agent CLI runs with credential material in its environment — pooled
	// provider accounts, MCP client certificates, forge tokens — and a
	// diagnostic it prints while dying can carry any of it. This record is
	// stored for the life of the session (see acpSession.exit) and handed to an
	// observer, so a stderr tail on it would be plaintext secret material held
	// in ordinary memory, for that whole span, on two copies.
	//
	// Not logging it was never the same as not RETAINING it. Nothing consumed
	// the field, so the retention bought exposure and no capability; if an
	// authorized diagnostic surface ever needs the tail it must come through
	// the stack's protected-secret abstraction with explicit destruction, not
	// through a string parked on a long-lived struct.
	//
	// The tail is still captured where it is actually used: acpSession.stderr
	// feeds the typed exit errors, which is what the account-limit classifier
	// reads.

	// At is when the exit was observed.
	At time.Time
}

// String renders the exit for an operator-facing log line.
//
// METADATA ONLY, deliberately: session, pid, exit code, and a wait failure if
// there was one. Stderr is excluded — see the field. This is the string the
// daemon logs on EVERY supervised exit, ordinary shutdowns included, so it is
// the one rendering that must be safe to write to an unprotected log.
func (e ProcessExit) String() string {
	out := "session " + e.SessionID + " agent process pid " + strconv.Itoa(e.PID) +
		" exited with code " + strconv.Itoa(e.ExitCode)
	if e.Err != nil {
		out += " (wait: " + e.Err.Error() + ")"
	}
	return out
}

// ProcessExitObserver is told when a supervised agent subprocess terminates.
//
// IT MUST RETURN PROMPTLY, on a bound of its own. It runs on the supervisor
// goroutine, after the Wait the shared reaper owns — so an observer that never
// returns does not hold Close (which joins the reap, not the goroutine), but it
// does leak one goroutine per session, permanently. An implementation that
// writes to a store gives that write a deadline rather than leaving the bound
// to a caller that has none.
//
// A port rather than a logger for the reason every other port in this package
// is one: the backend has no session store and no audit sink, and giving it
// either would put the record of an exit in the layer least able to say what
// the exit MEANS. The service layer wires this to mark the persisted session
// record closed; a nil observer simply means nobody asked to be told.
type ProcessExitObserver func(ProcessExit)

// procReaper performs the single cmd.Wait a subprocess is allowed.
//
// A value shared between the spawn's own teardown paths and the session it
// eventually produces, so both halves of the process's life reach it through
// one sync.Once. The alternative — a closure captured in spawnCLI and a second
// Wait on the session — is what the burn-in found: two callers, one of them
// concurrent, on a call the os/exec contract forbids calling twice.
type procReaper struct {
	once sync.Once
	cmd  *exec.Cmd

	// err is what that single Wait reported, published by the Once so every
	// later caller reads the same answer. Kept rather than discarded because
	// the supervisor puts it on the recorded exit: "the process is gone" and
	// "the daemon could not tell how it went" are different facts, and a reap
	// that swallowed the second reports the first with false confidence.
	err error
}

// reap waits for the process exactly once, blocking every later caller until
// that wait returns.
//
// Blocking is what the later callers want: none of Close, the mid-flight
// subprocess-gone path, or the supervisor may proceed on a process that is
// still being reaped, and each of them needs cmd.ProcessState populated by the
// time it looks.
func (r *procReaper) reap() error {
	if r == nil || r.cmd == nil {
		return nil
	}
	r.once.Do(func() { r.err = r.cmd.Wait() })
	return r.err
}

// reapProcess waits for the session's subprocess exactly once.
//
// A session with no reaper (JSON-RPC and one-shot sessions own no persistent
// cmd of their own) is a no-op rather than an error: there is no process here
// to wait for.
func (s *acpSession) reapProcess() error {
	return s.reaper.reap()
}

// recordExit stores what the process exited with and marks the session closed.
//
// Store-once by construction: only the supervisor calls it, exactly once, on
// the far side of the single Wait.
func (s *acpSession) recordExit(exit ProcessExit) {
	s.exit.Store(&exit)
	s.closed.Store(true)
}

// exited returns the recorded exit, or nil while the process is still running.
func (s *acpSession) exited() *ProcessExit {
	return s.exit.Load()
}

// superviseProcess gives the session's subprocess an owner that outlives the
// goroutine which spawned it.
//
// Called once, immediately after the session is stored, so the window in which
// nothing supervises the process is exactly the spawn itself — and every
// failure path inside the spawn already reaps through teardown.
func (b *ACPBackend) superviseProcess(sess *acpSession) {
	if sess == nil || sess.cmd == nil || sess.reaper == nil {
		return
	}
	go func() {
		// THE single Wait. It blocks for the session's whole life, which is the
		// point: the process has an owner for as long as it exists.
		//
		// A non-zero exit status arrives here as an *exec.ExitError, which is
		// not a fault of the wait: the code is read from ProcessState below and
		// the error would only restate it. Anything else IS a fault of the wait
		// and is carried onto the exit, because "the daemon could not tell how
		// the agent went" is a different report from "the agent failed".
		waitErr := sess.reapProcess()
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			waitErr = nil
		}

		exit := ProcessExit{
			SessionID: sess.id,
			TenantID:  sess.tenantID,
			ExitCode:  -1,
			At:        time.Now().UTC(),
		}
		if sess.cmd.Process != nil {
			exit.PID = sess.cmd.Process.Pid
		}
		if state := sess.cmd.ProcessState; state != nil {
			exit.ExitCode = state.ExitCode()
		}
		exit.Err = waitErr
		// The stderr tail is NOT copied onto the exit record — see ProcessExit.
		// sess.stderr keeps feeding the typed exit errors, which is the one
		// place anything reads it.
		sess.recordExit(exit)

		// Release the detached process context. Without this the exec package's
		// watchCtx goroutine parks on a context nothing will ever cancel, which
		// is precisely the goroutine that survived the burn-in incident and made
		// the run look live long after it had concluded.
		if sess.procCancel != nil {
			sess.procCancel()
		}

		// THE OBSERVER IS THE ONLY REPORT, and it is called last.
		//
		// Last because everything above it is state this session's readers
		// depend on. The ONLY report, because a second log line here would
		// duplicate the one the observer already writes — and a duplicate
		// carries a second copy of the agent's stderr, which is one more place
		// a credential in a diagnostic can land.
		if b.config.ProcessExitObserver != nil {
			b.config.ProcessExitObserver(exit)
		}
	}()
}
