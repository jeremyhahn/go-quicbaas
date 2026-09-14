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
// not fatal: the session is usable, the scanner is merely positioned somewhere
// inside a reply that no caller asked for.
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
	// scanner is positioned immediately after the handshake reply, ready for
	// the first real prompt's response.
	scanner *bufio.Scanner

	// sessionID is the identifier the session is registered under: the one the
	// plan demanded (a resume), else the agent's own, else a generated one.
	sessionID string

	// model is what the agent reported it is running, falling back to the
	// model the caller asked for when the init record named none.
	model string
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
	scanner := bufio.NewScanner(proc.stdout)
	scanner.Buffer(make([]byte, streamJSONScannerBufferSize), streamJSONScannerBufferSize)

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

	type initResult struct {
		msg streamJSONMessage
		err error
	}
	initCh := make(chan initResult, 1)
	go func() {
		for scanner.Scan() {
			var msg streamJSONMessage
			if jsonErr := json.Unmarshal(scanner.Bytes(), &msg); jsonErr != nil {
				continue
			}
			if msg.Type == "system" && msg.Subtype == "init" {
				initCh <- initResult{msg: msg}
				return
			}
		}
		// Scanner exhausted before the init record: the agent went away during
		// startup. Ask the caller why — it owns the process and is the only one
		// that can say.
		scanErr := scanner.Err()
		if scanErr == nil {
			scanErr = proc.gone()
		}
		initCh <- initResult{err: scanErr}
	}()

	var initMsg streamJSONMessage
	select {
	case <-ctx.Done():
		teardown()
		return nil, &SpawnError{Command: b.config.Command, Cause: ctx.Err()}
	case <-time.After(streamJSONInitTimeout):
		teardown()
		// AUTHORED, so it is not StreamJSONError: that type's Result is the
		// agent's own words and is withheld (review 1330). This sentence is
		// ours, no process wrote it, and withholding it would cost an operator
		// the one fact that matters — how long we waited.
		return nil, &SpawnError{
			Command: b.config.Command,
			Cause:   errors.New("timed out waiting for init message after " + streamJSONInitTimeout.String()),
		}
	case result := <-initCh:
		if result.err != nil {
			teardown()
			return nil, &SpawnError{Command: b.config.Command, Cause: result.err}
		}
		initMsg = result.msg
	}

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

	// Drain the handshake response (assistant + result messages) so the scanner
	// is positioned for the first real prompt's response.
	drainCh := make(chan struct{}, 1)
	go func() {
		for scanner.Scan() {
			var msg streamJSONMessage
			if jsonErr := json.Unmarshal(scanner.Bytes(), &msg); jsonErr != nil {
				continue
			}
			if msg.Type == "result" {
				break
			}
		}
		drainCh <- struct{}{}
	}()
	select {
	case <-drainCh:
		// Handshake response consumed.
	case <-time.After(streamJSONDrainTimeout):
		// Handshake timed out, but the session is still usable.
	case <-ctx.Done():
		teardown()
		return nil, &SpawnError{Command: b.config.Command, Cause: ctx.Err()}
	}

	return &streamJSONOpen{scanner: scanner, sessionID: sessionID, model: model}, nil
}
