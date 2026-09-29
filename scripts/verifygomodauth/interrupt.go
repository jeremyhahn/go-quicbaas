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

package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// interruptCase is a target interrupted while its image build is in progress
// -- a Ctrl-C, or a cancelled job -- which must still empty the staged file.
type interruptCase struct {
	target string
	// hang is the docker call the fake holds open until the signal lands.
	hang string
}

var interruptCases = []interruptCase{
	{target: "integration-bench", hang: "up -d"},
	{target: "test-e2e-docker", hang: "build e2e"},
}

// interrupt runs ic's target, waits until the fake reports the build call in
// progress, and sends SIGINT to make's whole process group, as a terminal's
// Ctrl-C does. reached reports whether the build call was ever made; when it
// was not, make has already exited on its own and the outcome says why.
func interrupt(root, work string, ic interruptCase, tokensFile string) (out outcome, reached bool, err error) {
	inv := invocation{targets: []string{ic.target}, tokensFile: tokensFile, env: gateOriginEnv, hangMatch: ic.hang}
	dir, staged, err := prepareCase(work, ic.target+"-interrupted", inv)
	if err != nil {
		return outcome{}, false, err
	}
	fifo := filepath.Join(dir, gate.HangFIFO)
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		return outcome{}, false, &WorkspaceError{Err: err}
	}

	// Held open read-write for the whole run: on Linux that never blocks, the
	// fake's write-only open succeeds at once, and this side can post its own
	// line to release the reader when make exits without reaching the call.
	pipe, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		return outcome{}, false, &WorkspaceError{Err: err}
	}
	defer func() {
		if cerr := pipe.Close(); cerr != nil && err == nil {
			err = &WorkspaceError{Err: cerr}
		}
	}()

	cmd := makeCommand(root, dir, work, staged, inv)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var sink strings.Builder
	cmd.Stdout = &sink
	cmd.Stderr = &sink
	if err := cmd.Start(); err != nil {
		return outcome{}, false, err
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	lines := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(pipe).ReadString('\n')
		lines <- readResult{line: line, err: err}
	}()

	var runErr error
	select {
	case got := <-lines:
		if got.err != nil {
			return outcome{}, false, &WorkspaceError{Err: got.err}
		}
		reached = true
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
			return outcome{}, false, err
		}
		runErr = <-exited
	case runErr = <-exited:
		if _, err := pipe.WriteString(exitedSentinel + "\n"); err != nil {
			return outcome{}, false, &WorkspaceError{Err: err}
		}
		got := <-lines
		reached = got.err == nil && strings.TrimSpace(got.line) != exitedSentinel
	}

	status, err := exitStatus(runErr)
	if err != nil {
		return outcome{}, reached, err
	}
	out, err = collect(dir, staged, status, sink.String())
	return out, reached, err
}

// exitedSentinel is the line the gate posts to its own FIFO when make exits
// before the fake announces anything.
const exitedSentinel = "gate: make exited"

type readResult struct {
	line string
	err  error
}
