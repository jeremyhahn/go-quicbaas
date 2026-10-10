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

// Command verifyteardown proves that `make integration-test` tears its Compose
// stack down even when it leaves early.
//
// The Integration workflow used to run `sudo docker system prune -af --volumes`
// on the shared runner, which reclaimed this job's leftovers by deleting every
// unused image and named volume on the host — including other repositories' (#3).
// That prune is gone, so nothing behind this job collects what it leaves: the
// stack has to take itself down on every exit path, not only on the one where the
// tests actually ran. The recipe arms an EXIT trap before it creates anything,
// and this pins that: a build that fails still ends with a
// `compose down --volumes --remove-orphans`.
//
// Docker and nvidia-smi are faked through PATH by this binary impersonating
// itself — see scripts/internal/gate/fakedocker.go — so the verdict lands in a
// second or two without a daemon, a GPU or a single container, and does not
// depend on whether the runner happens to have a GPU.
//
// EXIT: 0 the stack comes down · 1 a check failed · 2 the gate could not run.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// buildMarker is the step of the recipe a case breaks: the image build. It is
// also what the fake refuses, so the same text names the arm and the assertion.
const buildMarker = "build --no-cache"

// teardownMarker is the call the recipe must make on the way out however it
// leaves.
const teardownMarker = "down --volumes --remove-orphans"

const gateRunID = "verify-teardown"

// gomodBackups are the run-scoped paths the recipe copies go.mod to. Whatever
// was there is left as it was.
var gomodBackups = []string{
	"/tmp/quicbaas-verify-teardown-gomod-backup",
	"/tmp/quicbaas-verify-teardown-sdk-gomod-backup",
}

func main() {
	gate.DispatchFake()
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// WorkspaceError reports that the gate could not build the throwaway tree its
// case runs in.
type WorkspaceError struct {
	Err error
}

func (e *WorkspaceError) Error() string { return "cannot prepare the gate workspace: " + e.Err.Error() }

func (e *WorkspaceError) Unwrap() error { return e.Err }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verifyteardown", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root whose integration-test target is verified")
	if err := fs.Parse(args); err != nil {
		return gate.ExitToolError
	}

	work, err := os.MkdirTemp("", "verifyteardown-")
	if err != nil {
		fmt.Fprintln(stderr, "verify-integration-teardown:", (&WorkspaceError{Err: err}).Error())
		return gate.ExitToolError
	}
	defer func() {
		if rerr := os.RemoveAll(work); rerr != nil {
			fmt.Fprintln(stderr, "verify-integration-teardown: cannot remove the workspace:", rerr)
		}
	}()

	// The recipe backs go.mod up to fixed paths under /tmp. Anything it creates
	// that was not there before is removed again; anything that was there is
	// left alone.
	restore := absentBackups()
	defer func() {
		for _, path := range restore {
			if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				fmt.Fprintln(stderr, "verify-integration-teardown: cannot remove", path, rerr)
			}
		}
	}()

	if err := gate.InstallFakes(filepath.Join(work, "bin")); err != nil {
		fmt.Fprintln(stderr, "verify-integration-teardown:", (&WorkspaceError{Err: err}).Error())
		return gate.ExitToolError
	}

	failures, err := check(*root, work, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "verify-integration-teardown:", err)
		return gate.ExitToolError
	}
	if failures != 0 {
		fmt.Fprintf(stderr, "verify-integration-teardown: %d check(s) failed\n", failures)
		return gate.ExitFindings
	}
	fmt.Fprintln(stdout, "verify-integration-teardown: the integration stack is torn down on the early-exit path too")
	return gate.ExitClean
}

// check runs the case and returns how many assertions failed, or an error when
// the gate itself could not run.
func check(root, work string, stdout, stderr io.Writer) (int, error) {
	failures := 0
	fail := func(format string, args ...any) {
		fmt.Fprintf(stderr, "  ✗ "+format+"\n", args...)
		failures++
	}

	fmt.Fprintln(stdout, "case: a failed image build still brings the stack down")
	if err := os.WriteFile(filepath.Join(work, "fail.match"), []byte(buildMarker+"\n"), 0o600); err != nil {
		return 0, &WorkspaceError{Err: err}
	}
	status, calls, output, err := runIntegrationTest(root, work)
	if err != nil {
		return 0, err
	}

	if status == 0 {
		fail("reported success after the image build failed")
	}

	buildAt := lastCallIndex(calls, buildMarker)
	if buildAt < 0 {
		fail("the recipe never reached the image build — this check is no longer exercising it")
		fmt.Fprintln(stderr, "--- recipe output ---")
		fmt.Fprintln(stderr, output)
	} else if firstCallIndexAfter(calls, teardownMarker, buildAt) < 0 {
		fail("the stack was left up after the build failed — teardown must not depend on reaching the end of the recipe (#3)")
		fmt.Fprintln(stderr, "--- docker calls ---")
		fmt.Fprintln(stderr, strings.Join(calls, "\n"))
	}

	for _, call := range calls {
		if strings.Contains(call, "system prune") {
			fail("pruned the host — see #3")
			break
		}
	}
	return failures, nil
}

// runIntegrationTest runs `make integration-test` in root against the faked
// docker and nvidia-smi in work, returning its status, the calls the fake
// recorded, and the recipe's own output for use as a diagnostic.
func runIntegrationTest(root, work string) (int, []string, string, error) {
	callsPath := filepath.Join(work, "calls.log")
	if err := os.WriteFile(callsPath, nil, 0o600); err != nil {
		return 0, nil, "", &WorkspaceError{Err: err}
	}

	cmd := exec.Command("make", "--no-print-directory", "integration-test",
		"RUN_ID="+gateRunID,
		"GOMODAUTH_ENV="+filepath.Join(work, "gomodauth.env"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(work, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		gate.FakeDirEnv+"="+work,
	)
	var sink strings.Builder
	cmd.Stdout = &sink
	cmd.Stderr = &sink
	status := 0
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return 0, nil, "", err
		}
		status = exit.ExitCode()
	}

	raw, err := os.ReadFile(callsPath)
	if err != nil {
		return 0, nil, "", &WorkspaceError{Err: err}
	}
	var calls []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" {
			calls = append(calls, line)
		}
	}
	return status, calls, sink.String(), nil
}

// absentBackups returns the backup paths the recipe writes that do not exist
// yet, so only those are removed afterwards.
func absentBackups() []string {
	var absent []string
	for _, path := range gomodBackups {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			absent = append(absent, path)
		}
	}
	return absent
}

// lastCallIndex returns the index of the last call mentioning text, or -1.
func lastCallIndex(calls []string, text string) int {
	for i := len(calls) - 1; i >= 0; i-- {
		if strings.Contains(calls[i], text) {
			return i
		}
	}
	return -1
}

// firstCallIndexAfter returns the index of the first call after start that
// mentions text, or -1. "After" is what the check is about: a teardown that
// happened before the failure says nothing about the exit path.
func firstCallIndexAfter(calls []string, text string, start int) int {
	for i := start + 1; i < len(calls); i++ {
		if strings.Contains(calls[i], text) {
			return i
		}
	}
	return -1
}
