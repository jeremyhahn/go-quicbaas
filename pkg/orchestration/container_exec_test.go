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
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// Harness
// =============================================================================

// execCanary is a value shaped like the credential an exec carries — the
// forge token the delivery's push authenticates with. Every assertion below
// that mentions it is "this string must not appear where a local unprivileged
// user, or a log reader, can see it".
const execCanary = "ghp-EXEC-LEAK-CANARY-0123456789"

// stubProbeArg asks a stub to exit immediately, recording nothing. It exists
// only for the ETXTBSY probe in execStubRuntime.
const stubProbeArg = "--quicbaas-stub-probe"

// execStubRuntime returns a runtime whose "docker" is a shell script the test
// wrote.
//
// It is stubRuntimePATH's mechanics — write an executable, chmod past the
// umask — without the PATH mutation. stubRuntimePATH exists so auto-detection
// can be exercised, and its stubs are fixed at "exit 0"; these tests need the
// stub to CHOOSE an exit status, print on a chosen stream and record what it
// was handed, so each one supplies its own script and names it directly, the
// way the /bin/true and /bin/cat tests in container_env_file_test.go do.
// Naming the binary also keeps these tests parallel-safe, which a t.Setenv on
// PATH would forbid.
func execStubRuntime(t *testing.T, script string) *ExecContainerRuntime {
	t.Helper()

	// The probe guard is the first thing the script does, so the exec below
	// leaves no trace a test could mistake for the invocation it is asserting
	// about.
	body := "#!/bin/sh\nif [ \"$1\" = \"" + stubProbeArg + "\" ]; then exit 0; fi\n" + script
	path := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write runtime stub: %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod runtime stub: %v", err)
	}

	// A file written and then executed can fail with ETXTBSY: some other
	// process in this test binary forked while the descriptor the stub was
	// written through was still open, inherited it, and has not reached its own
	// exec yet — the descriptor is close-on-exec, so the window shuts the
	// instant that child execs (golang/go#22315). This package's parallel phase
	// forks constantly, and a full-package run did land in that window on this
	// exact stub. It is waited out HERE, before the code under test runs the
	// binary: our own descriptor is already closed, so no later fork can
	// inherit it, and one successful exec proves no earlier one is still
	// pending.
	for attempt := 0; ; attempt++ {
		err := exec.Command(path, stubProbeArg).Run()
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.ETXTBSY) {
			t.Fatalf("runtime stub is not executable: %v", err)
		}
		if attempt == stubProbeAttempts {
			t.Fatalf("runtime stub still busy after %d execs", stubProbeAttempts)
		}
	}

	return &ExecContainerRuntime{command: path, logger: newContainerTestLogger(t)}
}

// stubProbeAttempts bounds the ETXTBSY probe. The window is one fork/exec
// wide, so a stub still busy after this many attempts is a different fault
// and must be reported rather than retried forever.
const stubProbeAttempts = 100

// readStubFile returns what a stub recorded, failing the test when the stub
// never ran far enough to write it.
func readStubFile(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the stub did not record %s: %v", filepath.Base(path), err)
	}
	return string(body)
}

// stubArgv splits an argv dump written by a stub's `printf '%s\n' "$@"` back
// into arguments. The runtime never passes an argument containing a newline —
// an environment value that could is rejected by validateEnvEntry long before
// it reaches argv — so the line split is faithful.
func stubArgv(t *testing.T, path string) []string {
	t.Helper()

	dump := strings.TrimSuffix(readStubFile(t, path), "\n")
	if dump == "" {
		return nil
	}
	return strings.Split(dump, "\n")
}

// =============================================================================
// ExecSpec.Validate
// =============================================================================

// TestExecSpec_Validate covers the commands that cannot be run at all. The
// environment cases matter for the same reason they do at launch: the spec's
// environment is written to a line-oriented --env-file, so an entry that file
// cannot carry has to be refused before one is written.
func TestExecSpec_Validate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		spec    *ExecSpec
		wantErr bool
	}{
		{name: "PlainCommand", spec: &ExecSpec{Cmd: []string{"git", "push"}}},
		{
			name: "FullSpec",
			spec: &ExecSpec{
				Cmd:     []string{"git", "commit", "-m", "subject"},
				Env:     map[string]string{"GIT_TOKEN": execCanary},
				WorkDir: "/workspace",
				Stdin:   []byte("body"),
			},
		},
		{name: "NilSpec", spec: nil, wantErr: true},
		{name: "NilCmd", spec: &ExecSpec{}, wantErr: true},
		{name: "EmptyCmd", spec: &ExecSpec{Cmd: []string{}}, wantErr: true},
		{name: "BlankEntryPoint", spec: &ExecSpec{Cmd: []string{"  "}}, wantErr: true},
		{
			name:    "EnvValueWithNewline",
			spec:    &ExecSpec{Cmd: []string{"git"}, Env: map[string]string{"T": execCanary + "\nINJECTED=1"}},
			wantErr: true,
		},
		{
			name:    "EnvNameWithEquals",
			spec:    &ExecSpec{Cmd: []string{"git"}, Env: map[string]string{"A=B": "x"}},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.spec.Validate()
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			var validErr *InputValidationError
			if !errors.As(err, &validErr) {
				t.Fatalf("Validate() error = %T (%v), want *InputValidationError", err, err)
			}
			if strings.Contains(err.Error(), execCanary) {
				t.Errorf("the rejection message quoted the credential: %v", err)
			}
		})
	}
}

// =============================================================================
// ExecResult
// =============================================================================

// TestExecResult_SucceededAndOutput pins the two readings of a result a caller
// makes: whether the command agreed, and the one-line answer it gave. Both
// must tolerate a nil result, because an Exec that errored returns one.
func TestExecResult_SucceededAndOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		result    *ExecResult
		succeeded bool
		output    string
	}{
		{name: "NilResult", result: nil},
		{name: "CleanExit", result: &ExecResult{}, succeeded: true},
		{
			name:      "AnswerIsTrimmed",
			result:    &ExecResult{Stdout: "  6f2c1ab\n"},
			succeeded: true,
			output:    "6f2c1ab",
		},
		{
			name:   "NonZeroExitStillCarriesItsAnswer",
			result: &ExecResult{ExitCode: 1, Stdout: "no\n", Stderr: "fatal: bad revision"},
			output: "no",
		},
		{name: "NegativeExitIsNotSuccess", result: &ExecResult{ExitCode: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.result.Succeeded(); got != tc.succeeded {
				t.Errorf("Succeeded() = %v, want %v", got, tc.succeeded)
			}
			if got := tc.result.Output(); got != tc.output {
				t.Errorf("Output() = %q, want %q", got, tc.output)
			}
		})
	}
}

// =============================================================================
// Exec: the command's answer
// =============================================================================

// TestExecContainerRuntime_Exec_ReportsExitStatus is the type's central
// contract: the command's status is REPORTED, never raised. A caller asking a
// question by exit status — "git rev-parse --verify" — gets an answer, not an
// error it has to unwrap.
func TestExecContainerRuntime_Exec_ReportsExitStatus(t *testing.T) {
	t.Parallel()

	t.Run("ZeroExit", func(t *testing.T) {
		t.Parallel()

		rt := execStubRuntime(t, "echo '  6f2c1ab  '\nexit 0\n")

		result, err := rt.Exec(t.Context(), "ctr-1", &ExecSpec{Cmd: []string{"git", "rev-parse", "HEAD"}})
		if err != nil {
			t.Fatalf("Exec() error = %v", err)
		}
		if result.ExitCode != 0 || !result.Succeeded() {
			t.Errorf("ExitCode = %d, Succeeded = %v; want a clean exit", result.ExitCode, result.Succeeded())
		}
		if result.Stdout != "  6f2c1ab  \n" {
			t.Errorf("Stdout = %q, want the stub's own output verbatim", result.Stdout)
		}
		if result.Output() != "6f2c1ab" {
			t.Errorf("Output() = %q, want the trimmed answer", result.Output())
		}
	})

	t.Run("NonZeroExitIsNotAnError", func(t *testing.T) {
		t.Parallel()

		// The stderr text is deliberately unremarkable: anything containing
		// "not found" would be classified as a missing container instead.
		rt := execStubRuntime(t, "echo refused >&2\nexit 7\n")

		result, err := rt.Exec(t.Context(), "ctr-1", &ExecSpec{Cmd: []string{"git", "push"}})
		if err != nil {
			t.Fatalf("Exec() error = %v, want the status reported on the result", err)
		}
		if result.ExitCode != 7 {
			t.Errorf("ExitCode = %d, want 7", result.ExitCode)
		}
		if result.Succeeded() {
			t.Error("Succeeded() = true for a command that exited 7")
		}
		if strings.TrimSpace(result.Stderr) != "refused" {
			t.Errorf("Stderr = %q, want the command's own diagnosis kept for the caller", result.Stderr)
		}
	})
}

// TestExecContainerRuntime_Exec_MissingContainer proves the one stderr shape
// that IS classified: a container the runtime cannot find is a
// ContainerNotFoundError, not an exit status, because there was no command
// run to have a status.
func TestExecContainerRuntime_Exec_MissingContainer(t *testing.T) {
	t.Parallel()

	rt := execStubRuntime(t,
		"echo 'Error response from daemon: No such container: ctr-gone' >&2\nexit 1\n")

	result, err := rt.Exec(t.Context(), "ctr-gone", &ExecSpec{Cmd: []string{"git", "status"}})
	if result != nil {
		t.Errorf("Exec() result = %+v, want nil when nothing ran", result)
	}
	var notFound *ContainerNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Exec() error = %T (%v), want *ContainerNotFoundError", err, err)
	}
	if notFound.ContainerID != "ctr-gone" {
		t.Errorf("ContainerID = %q, want the container the caller named", notFound.ContainerID)
	}
	if !errors.Is(err, ErrContainerNotFound) {
		t.Error("expected errors.Is(err, ErrContainerNotFound)")
	}
}

// TestExecContainerRuntime_Exec_RuntimeUnrunnable covers the runtime binary
// that cannot be started at all — an uninstalled docker, a PATH that moved
// under a long-lived daemon. There is no exit status to report, so this is
// the error case.
func TestExecContainerRuntime_Exec_RuntimeUnrunnable(t *testing.T) {
	t.Parallel()

	rt := &ExecContainerRuntime{
		command: filepath.Join(t.TempDir(), "docker-that-was-never-installed"),
		logger:  newContainerTestLogger(t),
	}

	result, err := rt.Exec(t.Context(), "ctr-1", &ExecSpec{Cmd: []string{"git", "push"}})
	if result != nil {
		t.Errorf("Exec() result = %+v, want nil", result)
	}
	var execErr *ContainerExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("Exec() error = %T (%v), want *ContainerExecError", err, err)
	}
	if !errors.Is(err, ErrContainerExec) {
		t.Error("expected errors.Is(err, ErrContainerExec)")
	}
	if execErr.ContainerID != "ctr-1" {
		t.Errorf("ContainerID = %q, want the container the caller named", execErr.ContainerID)
	}
}

// TestExecContainerRuntime_Exec_SignalledProcessIsNotAnAnswer covers the
// negative exit code: a process killed by a signal never chose a status, so
// reporting one would invent an answer the command did not give. It is an
// error, and — like every CommandError — the bytes the dying command printed
// stay on the field instead of being rendered into the message.
func TestExecContainerRuntime_Exec_SignalledProcessIsNotAnAnswer(t *testing.T) {
	t.Parallel()

	rt := execStubRuntime(t, "echo '"+execCanary+"' >&2\nkill -KILL $$\n")

	result, err := rt.Exec(t.Context(), "ctr-1", &ExecSpec{Cmd: []string{"git", "push"}})
	if result != nil {
		t.Errorf("Exec() result = %+v, want nil for a signalled process", result)
	}
	var execErr *ContainerExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("Exec() error = %T (%v), want *ContainerExecError", err, err)
	}
	if strings.Contains(err.Error(), execCanary) {
		t.Fatalf("the command's stderr was rendered into the error string: %v", err)
	}
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("cause = %T, want a *CommandError carrying the evidence", execErr.Cause)
	}
	if cmdErr.Op != "exec" {
		t.Errorf("CommandError.Op = %q, want exec", cmdErr.Op)
	}
	if !strings.Contains(cmdErr.Output, execCanary) {
		t.Errorf("CommandError.Output = %q, want the stderr kept on the field", cmdErr.Output)
	}
}

// =============================================================================
// Exec: refusals before anything runs
// =============================================================================

// TestExecContainerRuntime_Exec_RejectsUnrunnableRequests proves the guards
// fire before a process is started: a request with no container to run in, or
// no command to run, is refused rather than handed to the runtime to reject
// with a message of its own choosing.
func TestExecContainerRuntime_Exec_RejectsUnrunnableRequests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		containerID string
		spec        *ExecSpec
		wantField   string
	}{
		{name: "EmptyContainerID", spec: &ExecSpec{Cmd: []string{"git"}}, wantField: "containerID"},
		{name: "NilSpec", containerID: "ctr-1", wantField: "spec"},
		{name: "NilCmd", containerID: "ctr-1", spec: &ExecSpec{}, wantField: "cmd"},
		{name: "EmptyCmd", containerID: "ctr-1", spec: &ExecSpec{Cmd: []string{}}, wantField: "cmd"},
		{name: "BlankEntryPoint", containerID: "ctr-1", spec: &ExecSpec{Cmd: []string{""}}, wantField: "cmd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The stub records every invocation, so "nothing ran" is asserted
			// rather than assumed.
			ran := filepath.Join(t.TempDir(), "ran")
			rt := execStubRuntime(t, "touch "+ran+"\n")

			result, err := rt.Exec(t.Context(), tc.containerID, tc.spec)
			if result != nil {
				t.Errorf("Exec() result = %+v, want nil", result)
			}
			var validErr *InputValidationError
			if !errors.As(err, &validErr) {
				t.Fatalf("Exec() error = %T (%v), want *InputValidationError", err, err)
			}
			if !strings.HasPrefix(validErr.Field, tc.wantField) {
				t.Errorf("Field = %q, want it to name %q", validErr.Field, tc.wantField)
			}
			if _, statErr := os.Stat(ran); !os.IsNotExist(statErr) {
				t.Error("the runtime was invoked for a request that should have been refused")
			}
		})
	}
}

// TestExecContainerRuntime_Exec_ContextCancelled proves a cancellation is
// reported as one.
//
// The two halves are different code paths and both are wrong in the same way
// if they report an exit status: a context that ended before the command was
// started never produced one, and a command KILLED by the ending context
// exits non-zero for a reason that has nothing to do with what it was asked.
func TestExecContainerRuntime_Exec_ContextCancelled(t *testing.T) {
	t.Parallel()

	t.Run("BeforeStart", func(t *testing.T) {
		t.Parallel()

		ran := filepath.Join(t.TempDir(), "ran")
		rt := execStubRuntime(t, "touch "+ran+"\nexit 0\n")

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		result, err := rt.Exec(ctx, "ctr-1", &ExecSpec{Cmd: []string{"git", "push"}})
		if result != nil {
			t.Errorf("Exec() result = %+v, want nil", result)
		}
		assertCancelledExec(t, err, "ctr-1", context.Canceled)
		if _, statErr := os.Stat(ran); !os.IsNotExist(statErr) {
			t.Error("the command ran under a context that was already over")
		}
	})

	t.Run("MidFlight", func(t *testing.T) {
		t.Parallel()

		// A FIFO rather than a polled file: opening the read end BLOCKS until
		// the stub opens the write end, so "the command is running" is a
		// synchronisation primitive and the test never sleeps (CLAUDE.md).
		// The stub then blocks forever opening the same FIFO for reading,
		// which only the context's kill can end. `exec` replaces the shell so
		// the kill reaches the process actually holding the pipes.
		fifo := filepath.Join(t.TempDir(), "started.fifo")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatalf("Mkfifo: %v", err)
		}
		rt := execStubRuntime(t, "echo started > "+fifo+"\nexec cat "+fifo+"\n")

		type outcome struct {
			result *ExecResult
			err    error
		}
		done := make(chan outcome, 1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			result, err := rt.Exec(ctx, "ctr-1", &ExecSpec{Cmd: []string{"git", "push"}})
			done <- outcome{result: result, err: err}
		}()

		if _, err := os.ReadFile(fifo); err != nil {
			t.Fatalf("the command never started: %v", err)
		}
		cancel()

		guard, stop := context.WithTimeout(t.Context(), 30*time.Second)
		defer stop()
		var got outcome
		select {
		case got = <-done:
		case <-guard.Done():
			t.Fatal("Exec did not return after its context was cancelled")
		}
		if got.result != nil {
			t.Errorf("Exec() result = %+v, want nil: the kill is not the command's answer", got.result)
		}
		assertCancelledExec(t, got.err, "ctr-1", context.Canceled)
	})
}

// assertCancelledExec checks that err reports the cancellation itself rather
// than whatever the killed process happened to leave behind.
func assertCancelledExec(t *testing.T, err error, containerID string, cause error) {
	t.Helper()

	var execErr *ContainerExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("Exec() error = %T (%v), want *ContainerExecError", err, err)
	}
	if execErr.ContainerID != containerID {
		t.Errorf("ContainerID = %q, want %q", execErr.ContainerID, containerID)
	}
	if !errors.Is(err, cause) {
		t.Errorf("expected errors.Is(err, %v), got %v", cause, err)
	}
	if !errors.Is(err, ErrContainerExec) {
		t.Error("expected errors.Is(err, ErrContainerExec)")
	}
}

// =============================================================================
// Exec: what the runtime is actually handed
// =============================================================================

// TestExecContainerRuntime_Exec_EnvNeverReachesArgv is the exec-side twin of
// TestContainerEnv_ValueNeverReachesArgv: the delivery's push carries a forge
// token, and "docker exec -e KEY=VALUE" would publish it in
// /proc/<pid>/cmdline for every local user to read.
//
// The stub proves both halves from inside the process it is asserting about —
// it dumps its own "$@" and copies the file the --env-file argument points
// at — so the test is independent of buildExecArgs' own view of what it built.
func TestExecContainerRuntime_Exec_EnvNeverReachesArgv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	argvDump := filepath.Join(dir, "argv")
	envCopy := filepath.Join(dir, "env-copy")
	rt := execStubRuntime(t, "printf '%s\\n' \"$@\" > "+argvDump+"\n"+
		"while [ $# -gt 0 ]; do\n"+
		"\tif [ \"$1\" = \"--env-file\" ]; then cp \"$2\" "+envCopy+"; fi\n"+
		"\tshift\n"+
		"done\n")

	env := map[string]string{"GIT_TOKEN": execCanary, "GIT_AUTHOR_NAME": "agent"}
	if _, err := rt.Exec(t.Context(), "ctr-1", &ExecSpec{
		Cmd: []string{"git", "push"},
		Env: env,
	}); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	argv := stubArgv(t, argvDump)
	for i, arg := range argv {
		// The env file PATH is an argument by design; it is the one argument
		// that is not a value. Everything else is scanned for every value.
		if i > 0 && argv[i-1] == envFileFlag {
			continue
		}
		for key, value := range env {
			if strings.Contains(arg, value) {
				t.Fatalf("environment value for %s leaked into argv: %q", key, arg)
			}
		}
		if arg == "-e" || arg == "--env" || strings.HasPrefix(arg, "--env=") {
			t.Fatalf("inline environment flag in argv: %v", argv)
		}
	}

	// Proving the absence is only half of it: the environment still has to
	// ARRIVE, or the push would run unauthenticated and the guard above would
	// pass vacuously.
	got := readStubFile(t, envCopy)
	want := "GIT_AUTHOR_NAME=agent\nGIT_TOKEN=" + execCanary + "\n"
	if got != want {
		t.Errorf("env file content = %q, want %q", got, want)
	}

	// The file is the runtime process's to read while it parses its command
	// line, and Exec's alone to release once that process has returned.
	envPath := ""
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == envFileFlag {
			envPath = argv[i+1]
		}
	}
	if envPath == "" {
		t.Fatalf("no %s argument in argv: %v", envFileFlag, argv)
	}
	if _, statErr := os.Stat(envPath); !os.IsNotExist(statErr) {
		t.Errorf("env file %s survived the exec: stat err = %v", envPath, statErr)
	}
}

// TestExecContainerRuntime_Exec_CredentialNeverLogged proves the second
// disclosure channel is closed too: the argument vector is logged, so the
// formatter has to be the redacting one.
func TestExecContainerRuntime_Exec_CredentialNeverLogged(t *testing.T) {
	t.Parallel()

	logger := newRecordingLogger()
	rt := execStubRuntime(t, "exit 0\n")
	rt.logger = logger

	if _, err := rt.Exec(t.Context(), "ctr-1", &ExecSpec{
		Cmd: []string{"git", "push"},
		Env: map[string]string{"GIT_TOKEN": execCanary},
	}); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	out := logger.all()
	if strings.Contains(out, execCanary) {
		t.Fatalf("credential leaked into logs:\n%s", out)
	}
	if info := logger.atLevel("INFO"); strings.Contains(info, envFileFlag) {
		t.Errorf("argument vector logged at INFO level:\n%s", info)
	}
	if debug := logger.atLevel("DEBUG"); !strings.Contains(debug, envFileFlag) {
		t.Errorf("expected the argument vector at DEBUG level, got:\n%s", debug)
	}
}

// TestExecContainerRuntime_Exec_WorkDirAndStdin proves the two fields that
// change how the command sees its world reach the process: the directory it
// runs in, and the bytes it reads. Stdin also has to turn the exec
// interactive — without -i the runtime attaches nothing and the command reads
// EOF, which for "git commit -F -" would silently mean an empty message.
func TestExecContainerRuntime_Exec_WorkDirAndStdin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	argvDump := filepath.Join(dir, "argv")
	stdinDump := filepath.Join(dir, "stdin")
	rt := execStubRuntime(t, "printf '%s\\n' \"$@\" > "+argvDump+"\ncat > "+stdinDump+"\n")

	body := "the commit message\nsecond line\n"
	if _, err := rt.Exec(t.Context(), "ctr-1", &ExecSpec{
		Cmd:     []string{"git", "commit", "-F", "-"},
		WorkDir: "/workspace/repo",
		Stdin:   []byte(body),
	}); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	argv := stubArgv(t, argvDump)
	if !argvHasPair(argv, "--workdir", "/workspace/repo") {
		t.Errorf("argv = %v, want --workdir /workspace/repo", argv)
	}
	if argv[0] != "exec" || argv[1] != "-i" {
		t.Errorf("argv = %v, want the exec subcommand and -i for a command with stdin", argv)
	}
	if got := readStubFile(t, stdinDump); got != body {
		t.Errorf("stdin = %q, want %q", got, body)
	}
}

// TestBuildExecArgs pins the argument vector's SHAPE, which the process-level
// tests above can only assert piecemeal: the order of the flags, and the fact
// that the container id separates the runtime's own flags from the command's
// — an argument on the wrong side of it is parsed by the wrong program.
func TestBuildExecArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		spec    *ExecSpec
		envFile string
		want    []string
	}{
		{
			name: "MinimalSpec",
			spec: &ExecSpec{Cmd: []string{"git", "status"}},
			want: []string{"exec", "ctr-1", "git", "status"},
		},
		{
			name:    "FullSpec",
			spec:    &ExecSpec{Cmd: []string{"git", "commit"}, WorkDir: "/w", Stdin: []byte("x")},
			envFile: "/run/env",
			want:    []string{"exec", "-i", "--workdir", "/w", envFileFlag, "/run/env", "ctr-1", "git", "commit"},
		},
		{
			name: "EmptyStdinIsNotInteractive",
			spec: &ExecSpec{Cmd: []string{"true"}, Stdin: []byte{}},
			want: []string{"exec", "ctr-1", "true"},
		},
		{
			name: "NoEnvFileMeansNoFlag",
			spec: &ExecSpec{Cmd: []string{"true"}, Env: map[string]string{"A": "b"}},
			want: []string{"exec", "ctr-1", "true"},
		},
		{
			name: "CommandFlagsStayOnTheCommandSide",
			spec: &ExecSpec{Cmd: []string{"git", "--workdir", "/not-the-runtimes"}},
			want: []string{"exec", "ctr-1", "git", "--workdir", "/not-the-runtimes"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := buildExecArgs("ctr-1", tc.spec, tc.envFile)
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Errorf("buildExecArgs() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExecContainerRuntime_Exec_EnvFileFailsClosed proves the exec inherits
// the launch path's refusal to fall back: with no writable directory for the
// private env file there is no argv form to degrade to, so the command does
// not run at all.
func TestExecContainerRuntime_Exec_EnvFileFailsClosed(t *testing.T) {
	// The stub and its recording file are materialized BEFORE TMPDIR is
	// repointed: t.TempDir honours TMPDIR too, so building them afterwards
	// would fail the harness instead of the code under test.
	ran := filepath.Join(t.TempDir(), "ran")
	rt := execStubRuntime(t, "touch "+ran+"\n")

	// t.Setenv forbids a parallel ancestor, which is why this is not a subtest
	// of the environment test above.
	t.Setenv("TMPDIR", "/nonexistent-quicbaas-exec-envfile-dir")

	_, err := rt.Exec(t.Context(), "ctr-1", &ExecSpec{
		Cmd: []string{"git", "push"},
		Env: map[string]string{"GIT_TOKEN": execCanary},
	})
	var envErr *ContainerEnvFileError
	if !errors.As(err, &envErr) {
		t.Fatalf("Exec() error = %T (%v), want *ContainerEnvFileError", err, err)
	}
	if strings.Contains(err.Error(), execCanary) {
		t.Errorf("the error message quoted the credential: %v", err)
	}
	if _, statErr := os.Stat(ran); !os.IsNotExist(statErr) {
		t.Error("the command ran with an environment that was never materialized")
	}
}
