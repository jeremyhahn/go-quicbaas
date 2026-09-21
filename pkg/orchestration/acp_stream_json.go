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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// =============================================================================
// The stream-json protocol, independent of where the agent runs
// =============================================================================
//
// A persistent CLI-mode session is the agent's stream-json protocol spoken over
// one process's stdio. WHICH process is a sandbox decision, not a protocol one:
// spawnCLI starts it on the daemon host, spawnContainerCLI starts it as the
// command of a container. Everything between those two facts — the argument
// vector, the opening handshake, the init record, draining the handshake reply
// — is identical, and is written once here.
//
// It is written once because the alternative was measured: while the container
// path did not exist, `--sandbox-policy required` on a CLI backend ran the
// agent on the host and said nothing (#741). Two copies of this exchange would
// be two chances for the sandboxed one to drift into a different protocol from
// the one the agent actually speaks, and a sandbox nobody can use is a sandbox
// that gets turned off.

// streamJSONInitTimeout bounds the wait for the agent's init record. Claude
// Code can take 30+ seconds to start (gopls, plugins), and a containerized one
// additionally pays for the container's own start, so the bound is generous;
// what it must not be is absent.
const streamJSONInitTimeout = 5 * time.Minute

// streamJSONDrainTimeout bounds the wait for the handshake reply. Expiring is
// not fatal: the session is usable, and the reply still owed is settled by the
// first prompt before it writes (see streamJSONReader).
const streamJSONDrainTimeout = 2 * time.Minute

// streamJSONProc is a started agent process reduced to what the stream-json
// protocol needs of it: the two pipes, and a way to say why its output ended.
//
// It deliberately does NOT carry the *exec.Cmd. A containerized agent has no
// host process at all — the `docker run -i` process belongs to the container
// runtime, which hands back pipes and a container id — so a core written
// against exec.Cmd could not have served both paths.
type streamJSONProc struct {
	// stdin carries newline-delimited stream-json user messages to the agent.
	stdin io.WriteCloser

	// stdout is the agent's newline-delimited stream-json output.
	stdout io.Reader

	// gone explains why stdout ended before the exchange completed. It is
	// called only on a clean EOF, and only by the caller of openStreamJSON's
	// reader goroutine. The host path reaps the subprocess and reports its
	// exit code and stderr tail; the container path reports the container it
	// could no longer hear from. It must not return nil.
	gone func() error
}

// streamJSONOpen is what the opening exchange established.
type streamJSONOpen struct {
	// reader is THE ONE reader of the agent's stdout for the life of the
	// session. See streamJSONReader for why there is exactly one.
	reader *streamJSONReader

	// pending counts the prompts written to the agent whose terminal result
	// the reader has not yet handed out: 1 when the handshake reply was still
	// in flight when the open returned, 0 when it was consumed. The first
	// real prompt settles it before it writes. See acpSession.pending.
	pending int

	// sessionID is the identifier the session is registered under: the one the
	// plan demanded (a resume), else the agent's own, else a generated one.
	sessionID string

	// model is what the agent reported it is running, falling back to the
	// model the caller asked for when the init record named none.
	model string
}

// streamJSONReader is the single goroutine that reads an agent's stream-json
// stdout, and the channel every consumer takes records from.
//
// WHY EXACTLY ONE. A bufio.Scanner is not safe for concurrent use, and the
// stream is a sequence whose ORDER carries meaning: a `result` record belongs
// to the prompt written before it. The open used to start one goroutine to
// wait for the init record, a second to drain the handshake reply, and then
// every prompt started a fresh goroutine per line — and each of those
// returned to its caller on a timeout while the goroutine it had started was
// still blocked in Scan. On the burn-in fleet (2026-09-21, go-quicbaas #53
// and #62) a handshake whose API calls were being retried for four minutes
// outlived the two-minute drain bound, the first prompt's reader then raced
// the drain goroutine on the same scanner, and the prompt's own `result`
// went to a channel nobody was reading. Both jobs held their slots for an
// hour, until the agent was stopped.
//
// One goroutine, one channel, and a count of replies still owed keep the
// sequence intact across every timeout: a consumer that gives up leaves the
// records where they are, and the next consumer settles what is owed before
// it asks for anything of its own.
type streamJSONReader struct {
	records chan *streamJSONMessage

	// err is why the stream ended, readable once records is closed: the
	// scanner's own error, or the process-gone explanation the opener gave.
	err  error
	done chan struct{}
}

// streamJSONReaderBuffer bounds how many records the reader holds for a
// consumer that is slower than the agent; a delivery turn emits thousands of
// tool records, and the consumer must never be the reason the agent's stdout
// blocks.
const streamJSONReaderBuffer = 4096

// newStreamJSONReader starts the reader on stdout. gone explains a clean end
// of the stream; it must not return nil.
func newStreamJSONReader(stdout io.Reader, gone func() error) *streamJSONReader {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, streamJSONScannerBufferSize), streamJSONScannerBufferSize)
	r := &streamJSONReader{
		records: make(chan *streamJSONMessage, streamJSONReaderBuffer),
		done:    make(chan struct{}),
	}
	go func() {
		defer close(r.done)
		defer close(r.records)
		for scanner.Scan() {
			var msg streamJSONMessage
			if jsonErr := json.Unmarshal(scanner.Bytes(), &msg); jsonErr != nil {
				continue // an unparseable line is not a record
			}
			r.records <- &msg
		}
		if scanErr := scanner.Err(); scanErr != nil {
			r.err = scanErr
			return
		}
		r.err = gone()
	}()
	return r
}

// next hands out the next record, or the reason the stream ended once it has,
// or ctx's error. A deadline is the caller's to add to ctx.
func (r *streamJSONReader) next(ctx context.Context) (*streamJSONMessage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-r.records:
		if !ok {
			<-r.done
			return nil, r.err
		}
		return msg, nil
	}
}

// nextResult hands out records up to and including the next terminal
// `result`, calling observe on each one it passes, and returns that result.
func (r *streamJSONReader) nextResult(ctx context.Context, observe func(*streamJSONMessage)) (*streamJSONMessage, error) {
	for {
		msg, err := r.next(ctx)
		if err != nil {
			return nil, err
		}
		if observe != nil {
			observe(msg)
		}
		if msg.Type == "result" {
			return msg, nil
		}
	}
}

// cliArgs builds the argument vector for a persistent stream-json agent CLI.
//
// Shared by the host and container paths so a sandboxed session speaks EXACTLY
// the command line an unsandboxed one does. The only difference between the two
// invocations is the process boundary; if the flags could differ, "it works
// unsandboxed" would stop being evidence about the sandboxed case.
func (b *ACPBackend) cliArgs(req *SpawnRequest, plan spawnPlan, model string) []string {
	args := make([]string, len(b.config.Args))
	copy(args, b.config.Args)

	// Extra args from the spawn request (e.g. --dangerously-skip-permissions).
	args = append(args, req.ExtraArgs...)

	args = applySessionPersistenceFlag(args, b.config.NativeSessionResume)

	// A native resume hands the conversation back to the agent's own session
	// store; nothing is re-sent by us.
	if plan.resumeID != "" {
		args = append(args, "--resume", plan.resumeID)
	}

	if b.config.TeamMode {
		args = append(args, "--team")
	}
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	return args
}

// openStreamJSON performs the opening exchange every persistent stream-json
// session pays for, over whatever stdio pair it is given.
//
// It writes the plan's handshake message (the agent emits its init record only
// after receiving input), reads the init record for the session id and model,
// and drains the handshake reply so the scanner is positioned for the first
// real prompt.
//
// model is the caller's resolved choice, used when the agent's init record
// names none. teardown is invoked on every failure path; what it means for the
// underlying process is the caller's business, because only the caller knows
// whether that process is a subprocess or a container.
func (b *ACPBackend) openStreamJSON(
	ctx context.Context,
	proc *streamJSONProc,
	plan spawnPlan,
	model string,
	teardown func(),
) (*streamJSONOpen, error) {
	reader := newStreamJSONReader(proc.stdout, proc.gone)

	handshake, handshakeErr := encodeStreamJSONUserMessage(plan.handshakeContent())
	if handshakeErr != nil {
		teardown()
		return nil, &SpawnError{Command: b.config.Command, Cause: handshakeErr}
	}
	if _, writeErr := proc.stdin.Write(handshake); writeErr != nil {
		teardown()
		// A broken pipe on the very first write means the agent is not there
		// at all. Report why it is gone rather than the pipe error, which names
		// nothing an operator can act on — the same substitution promptStreamJSON
		// makes for a mid-session write failure (agentGoneError).
		return nil, &SpawnError{Command: b.config.Command, Cause: proc.gone()}
	}

	// The init record, within the bound and the caller's ctx. Records before
	// it (there are none from the CLI, but the reader passes whatever comes)
	// are skipped.
	initCtx, cancelInit := context.WithTimeout(ctx, b.streamJSONInitBound())
	var initMsg *streamJSONMessage
	for initMsg == nil {
		msg, err := reader.next(initCtx)
		if err != nil {
			cancelInit()
			teardown()
			if ctx.Err() != nil {
				return nil, &SpawnError{Command: b.config.Command, Cause: ctx.Err()}
			}
			if errors.Is(err, context.DeadlineExceeded) {
				// AUTHORED, so it is not StreamJSONError: that type's Result is
				// the agent's own words and is withheld (review 1330). This
				// sentence is ours, no process wrote it, and withholding it
				// would cost an operator the one fact that matters — how long
				// we waited.
				return nil, &SpawnError{
					Command: b.config.Command,
					Cause:   errors.New("timed out waiting for init message after " + b.streamJSONInitBound().String()),
				}
			}
			return nil, &SpawnError{Command: b.config.Command, Cause: err}
		}
		if msg.Type == "system" && msg.Subtype == "init" {
			initMsg = msg
		}
	}
	cancelInit()

	sessionID := initMsg.SessionID
	if sessionID == "" {
		generated, genErr := generateSessionID()
		if genErr != nil {
			teardown()
			return nil, &SpawnError{Command: b.config.Command, Cause: genErr}
		}
		sessionID = generated
	}
	// A resumed session keeps the identifier the operator already holds, so the
	// agent's own reported session ID never leaks into the registry.
	if plan.registerID != "" {
		sessionID = plan.registerID
	}

	if initMsg.Model != "" {
		model = initMsg.Model
	}

	// The handshake reply (assistant + result records) is consumed within its
	// own bound so the first real prompt starts on a settled stream. Expiring
	// is not fatal: the reply is still owed, the reader still holds the
	// stream, and the session records ONE pending result for the first prompt
	// to settle before it writes. Nothing is read past the bound behind the
	// caller's back.
	//
	// A stream that ENDS inside the handshake reply is not refused here
	// either: the session is handed over with the reply owed, and the first
	// prompt's settle reads the closed stream and reports the agent gone with
	// its exit code and stderr — the same report a prompt has always given
	// for a process that died after spawn, which is what the caller expects.
	drainCtx, cancelDrain := context.WithTimeout(ctx, b.streamJSONDrainBound())
	defer cancelDrain()
	pending := 1
	if _, err := reader.nextResult(drainCtx, nil); err == nil {
		pending = 0
	} else if ctx.Err() != nil {
		teardown()
		return nil, &SpawnError{Command: b.config.Command, Cause: ctx.Err()}
	}

	return &streamJSONOpen{reader: reader, pending: pending, sessionID: sessionID, model: model}, nil
}

// streamJSONInitBound is how long an open waits for the init record: the
// backend's own setting when one was given, else streamJSONInitTimeout.
func (b *ACPBackend) streamJSONInitBound() time.Duration {
	if b.streamJSONInit > 0 {
		return b.streamJSONInit
	}
	return streamJSONInitTimeout
}

// streamJSONDrainBound is how long an open waits for the handshake reply
// before handing the session over with that reply still owed.
func (b *ACPBackend) streamJSONDrainBound() time.Duration {
	if b.streamJSONDrain > 0 {
		return b.streamJSONDrain
	}
	return streamJSONDrainTimeout
}
