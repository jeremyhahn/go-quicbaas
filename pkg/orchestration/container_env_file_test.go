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
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// credentialFixture is a value shaped like the provider credentials
// resolveAccount merges into a spawn's environment. Every assertion in this
// file is "this string must not appear anywhere an unprivileged local user or a
// log reader can see it".
const credentialFixture = "sk-ant-api03-LEAK-CANARY-0123456789"

// recordingLogger captures formatted log lines, with their level, so a test can
// assert on exactly what was emitted. Modelled on the "token never in argv"
// guard in pkg/service (TestGitProvisioner_TokenAuth_TokenNeverInArgs), which
// proves the same property for the git credential path.
type recordingLogger struct {
	mu    sync.Mutex
	lines []string
}

func newRecordingLogger() *recordingLogger { return &recordingLogger{} }

func (l *recordingLogger) record(line string) {
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
}

// all returns every recorded line joined by newlines.
func (l *recordingLogger) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// atLevel returns only the lines recorded at the given level prefix.
func (l *recordingLogger) atLevel(level string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var kept []string
	for _, line := range l.lines {
		if strings.HasPrefix(line, level+": ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func (l *recordingLogger) Debug(msg string, args ...any) { l.record("DEBUG: " + msg) }
func (l *recordingLogger) Debugf(format string, args ...interface{}) {
	l.record("DEBUG: " + fmt.Sprintf(format, args...))
}
func (l *recordingLogger) Info(msg string, args ...any) { l.record("INFO: " + msg) }
func (l *recordingLogger) Infof(format string, args ...interface{}) {
	l.record("INFO: " + fmt.Sprintf(format, args...))
}
func (l *recordingLogger) Warn(msg string, args ...any) { l.record("WARN: " + msg) }
func (l *recordingLogger) Warnf(format string, args ...interface{}) {
	l.record("WARN: " + fmt.Sprintf(format, args...))
}
func (l *recordingLogger) Error(msg string, args ...any) { l.record("ERROR: " + msg) }
func (l *recordingLogger) Errorf(format string, args ...interface{}) {
	l.record("ERROR: " + fmt.Sprintf(format, args...))
}
func (l *recordingLogger) Fatal(msg string) { l.record("FATAL: " + msg) }
func (l *recordingLogger) Fatalf(format string, args ...interface{}) {
	l.record("FATAL: " + fmt.Sprintf(format, args...))
}
func (l *recordingLogger) WithField(string, interface{}) interfaces.Logger     { return l }
func (l *recordingLogger) WithFields(map[string]interface{}) interfaces.Logger { return l }
func (l *recordingLogger) WithError(error) interfaces.Logger                   { return l }

var _ interfaces.Logger = (*recordingLogger)(nil)

// credentialEnvSpec is a sandbox spec carrying a pooled account credential in
// its environment, as spawnContainerJSONRPC builds it.
func credentialEnvSpec() *ContainerSpec {
	return &ContainerSpec{
		Image: "quicbaas-agent:latest",
		Name:  "leak-canary",
		Env: map[string]string{
			"ANTHROPIC_API_KEY":     credentialFixture,
			"QUICBAAS_ACCOUNT_ID":   "acct-1",
			"HOME":                  "/tmp",
			"QUICBAAS_OAUTH_ACCESS": credentialFixture + "-oauth",
		},
	}
}

// TestContainerEnv_ValueNeverReachesArgv is the primary guard: no environment
// value may appear in the docker/podman argument vector, because
// /proc/<pid>/cmdline is world readable on a default Linux host.
//
// Both launch shapes are covered, for both runtimes: the same
// appendCommonRunArgs builds them, so a regression in either would be a
// regression in all four.
func TestContainerEnv_ValueNeverReachesArgv(t *testing.T) {
	t.Parallel()

	for _, command := range []string{RuntimeDocker, RuntimePodman} {
		t.Run(command, func(t *testing.T) {
			rt := &ExecContainerRuntime{command: command, logger: newRecordingLogger()}
			spec := credentialEnvSpec()

			envFile, err := writeContainerEnvFile(spec.Env)
			if err != nil {
				t.Fatalf("writeContainerEnvFile() error = %v", err)
			}
			defer envFile.Cleanup()

			vectors := map[string][]string{
				"detached":    rt.buildRunArgs(spec, envFile.Path),
				"interactive": rt.buildInteractiveRunArgs(spec, "leak-canary", envFile.Path),
			}
			for shape, args := range vectors {
				for i, arg := range args {
					// The env file PATH is an argument by design; it is the
					// one argument that is not a value. Everything else is
					// scanned for every value in the environment.
					if i > 0 && args[i-1] == envFileFlag {
						continue
					}
					for key, value := range spec.Env {
						if strings.Contains(arg, value) {
							t.Fatalf("%s: environment value for %s leaked into argv: %q", shape, key, arg)
						}
					}
				}
				joined := strings.Join(args, " ")
				if !strings.Contains(joined, envFileFlag+" "+envFile.Path) {
					t.Errorf("%s: expected %s %s in args, got %q", shape, envFileFlag, envFile.Path, joined)
				}
				if strings.Contains(joined, " -e ") {
					t.Errorf("%s: inline -e flag reintroduced: %q", shape, joined)
				}
			}
		})
	}
}

// TestContainerEnv_NoEnvFileEmitsNoValue proves the failure mode is closed: a
// spec whose environment was not materialized loses the environment rather
// than falling back to inline arguments.
func TestContainerEnv_NoEnvFileEmitsNoValue(t *testing.T) {
	t.Parallel()

	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newRecordingLogger()}
	joined := strings.Join(rt.buildRunArgs(credentialEnvSpec(), ""), " ")

	if strings.Contains(joined, credentialFixture) {
		t.Fatalf("credential inlined when no env file was supplied: %q", joined)
	}
	if strings.Contains(joined, envFileFlag) {
		t.Errorf("expected no %s reference without a materialized file, got %q", envFileFlag, joined)
	}
}

// TestExecContainerRuntime_Run_CredentialNeverLogged proves the detached launch
// neither logs a credential nor leaves its env file behind.
func TestExecContainerRuntime_Run_CredentialNeverLogged(t *testing.T) {
	t.Parallel()

	logger := newRecordingLogger()
	// /bin/true stands in for the container runtime binary: it accepts any
	// argument vector and exits 0, so the launch path runs end to end without
	// a container runtime installed.
	rt := &ExecContainerRuntime{command: "/bin/true", logger: logger}

	if _, err := rt.Run(t.Context(), credentialEnvSpec()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	out := logger.all()
	if strings.Contains(out, credentialFixture) {
		t.Fatalf("credential leaked into logs:\n%s", out)
	}
	if !strings.Contains(out, envFileFlag) {
		t.Errorf("expected the env file reference in the launch log, got:\n%s", out)
	}

	// The detached runtime process has exited, so the file must be gone.
	if path := loggedEnvFilePath(t, out); path != "" {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("env file %s survived a detached Run: stat err = %v", path, statErr)
		}
	}
}

// TestExecContainerRuntime_RunInteractive_CredentialNeverLogged proves the
// sandboxed ACP path — the one that actually carries pooled credentials — keeps
// them out of the log, and that the argument vector is logged at DEBUG rather
// than INFO.
func TestExecContainerRuntime_RunInteractive_CredentialNeverLogged(t *testing.T) {
	t.Parallel()

	logger := newRecordingLogger()
	// /bin/cat ignores its arguments and reads stdin, which is exactly the
	// shape RunInteractive needs from `docker run -i`.
	rt := &ExecContainerRuntime{command: "/bin/cat", logger: logger}

	proc, err := rt.RunInteractive(t.Context(), credentialEnvSpec())
	if err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}
	defer func() {
		_ = proc.Stdin.Close()
		if proc.Cleanup != nil {
			proc.Cleanup()
		}
	}()

	if out := logger.all(); strings.Contains(out, credentialFixture) {
		t.Fatalf("credential leaked into logs:\n%s", out)
	}
	// The full argument vector is a debug-level diagnostic. Keeping it out of
	// INFO limits how far a future regression could travel.
	if info := logger.atLevel("INFO"); strings.Contains(info, envFileFlag) || strings.Contains(info, "run -i") {
		t.Errorf("argument vector logged at INFO level:\n%s", info)
	}
	if debug := logger.atLevel("DEBUG"); !strings.Contains(debug, envFileFlag) {
		t.Errorf("expected the argument vector at DEBUG level, got:\n%s", debug)
	}
}

// TestExecContainerRuntime_RunInteractive_EnvFileLifetime proves the file the
// long-lived runtime process reads from is handed to the caller and released by
// it — the container outlives RunInteractive, so the runtime cannot delete it.
func TestExecContainerRuntime_RunInteractive_EnvFileLifetime(t *testing.T) {
	t.Parallel()

	logger := newRecordingLogger()
	rt := &ExecContainerRuntime{command: "/bin/cat", logger: logger}

	proc, err := rt.RunInteractive(t.Context(), credentialEnvSpec())
	if err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}
	defer func() { _ = proc.Stdin.Close() }()

	if proc.Cleanup == nil {
		t.Fatal("InteractiveProcess.Cleanup is nil: the env file would leak for the daemon's lifetime")
	}

	path := loggedEnvFilePath(t, logger.all())
	if path == "" {
		t.Fatal("no env file path in the launch log")
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("env file must exist while the container runs: %v", statErr)
	}
	if perm := info.Mode().Perm(); perm != envFileMode {
		t.Errorf("env file mode = %v, want %v", perm, envFileMode)
	}

	proc.Cleanup()
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("Cleanup did not remove %s: stat err = %v", path, statErr)
	}
	// Cleanup is fired from both the teardown path and Close; a second call
	// must not panic or report an error.
	proc.Cleanup()
}

// loggedEnvFilePath extracts the env file path from captured log output, which
// carries it either as an "--env-file <path>" argument or as an "env_file=<path>"
// field.
func loggedEnvFilePath(t *testing.T, out string) string {
	t.Helper()
	marker := strings.TrimSuffix(envFileDirPrefix, "*")
	for _, field := range strings.Fields(out) {
		field = strings.Trim(field, "(),")
		if _, after, found := strings.Cut(field, "="); found {
			field = after
		}
		if strings.Contains(field, marker) && strings.HasSuffix(field, "/"+envFileName) {
			return field
		}
	}
	return ""
}

func TestWriteContainerEnvFile(t *testing.T) {
	t.Parallel()

	t.Run("WritesSortedPrivateFile", func(t *testing.T) {
		t.Parallel()
		envFile, err := writeContainerEnvFile(map[string]string{
			"ZED":               "last",
			"ANTHROPIC_API_KEY": credentialFixture,
			"ALPHA":             "first",
		})
		if err != nil {
			t.Fatalf("writeContainerEnvFile() error = %v", err)
		}
		defer envFile.Cleanup()

		content, readErr := os.ReadFile(envFile.Path)
		if readErr != nil {
			t.Fatalf("ReadFile() error = %v", readErr)
		}
		want := "ALPHA=first\nANTHROPIC_API_KEY=" + credentialFixture + "\nZED=last\n"
		if string(content) != want {
			t.Errorf("env file content = %q, want %q", content, want)
		}

		info, statErr := os.Stat(envFile.Path)
		if statErr != nil {
			t.Fatalf("Stat() error = %v", statErr)
		}
		if perm := info.Mode().Perm(); perm != envFileMode {
			t.Errorf("file mode = %v, want %v", perm, envFileMode)
		}
		// The containing directory must be private too: a world-readable
		// directory would let another user learn the file name even if it
		// could not read the contents.
		dirInfo, dirErr := os.Stat(strings.TrimSuffix(envFile.Path, "/"+envFileName))
		if dirErr != nil {
			t.Fatalf("Stat(dir) error = %v", dirErr)
		}
		if perm := dirInfo.Mode().Perm(); perm != 0o700 {
			t.Errorf("dir mode = %v, want 0700", perm)
		}
	})

	t.Run("EmptyEnvWritesNothing", func(t *testing.T) {
		t.Parallel()
		envFile, err := writeContainerEnvFile(nil)
		if err != nil {
			t.Fatalf("writeContainerEnvFile(nil) error = %v", err)
		}
		if envFile.Path != "" || envFile.Cleanup != nil {
			t.Errorf("expected the zero containerEnvFile for an empty env, got %+v", envFile)
		}
	})

	t.Run("UnrepresentableEntryIsRejected", func(t *testing.T) {
		t.Parallel()
		_, err := writeContainerEnvFile(map[string]string{
			"TOKEN": credentialFixture + "\nINJECTED=1",
		})
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
		if strings.Contains(err.Error(), credentialFixture) {
			t.Errorf("the rejection message quoted the credential: %v", err)
		}
	})
}

// TestWriteContainerEnvFile_UnwritableTempDirFailsClosed proves the launch fails
// when the private file cannot be created: there is no argv fallback to fall
// back TO. It is a standalone test because t.Setenv forbids a parallel ancestor.
func TestWriteContainerEnvFile_UnwritableTempDirFailsClosed(t *testing.T) {
	t.Setenv("TMPDIR", "/nonexistent-quicbaas-envfile-dir")

	_, err := writeContainerEnvFile(map[string]string{"TOKEN": credentialFixture})
	var envErr *ContainerEnvFileError
	if !errors.As(err, &envErr) {
		t.Fatalf("expected ContainerEnvFileError, got %T: %v", err, err)
	}
	if envErr.Operation != "mkdir" {
		t.Errorf("Operation = %q, want mkdir", envErr.Operation)
	}
	if strings.Contains(err.Error(), credentialFixture) {
		t.Errorf("the error message quoted the credential: %v", err)
	}

	rt := &ExecContainerRuntime{command: "/bin/true", logger: newRecordingLogger()}
	spec := &ContainerSpec{Image: "alpine:latest", Env: map[string]string{"TOKEN": credentialFixture}}
	if _, runErr := rt.Run(t.Context(), spec); !errors.As(runErr, &envErr) {
		t.Fatalf("Run() must fail closed, got %T: %v", runErr, runErr)
	}
	if _, runErr := rt.RunInteractive(t.Context(), spec); !errors.As(runErr, &envErr) {
		t.Fatalf("RunInteractive() must fail closed, got %T: %v", runErr, runErr)
	}
}

// TestValidateEnvEntry covers the entries the line-oriented env-file format
// cannot carry. The newline case is the important one: it would inject a
// variable the spec never declared and the configuration digest never measured.
func TestValidateEnvEntry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{name: "OrdinaryEntry", key: "ANTHROPIC_API_KEY", value: credentialFixture},
		{name: "EmptyValueIsFine", key: "EMPTY", value: ""},
		{name: "ValueWithEqualsAndHashIsFine", key: "JSON", value: `{"a":"b=c#d"}`},
		{name: "EmptyKey", key: "", value: "x", wantErr: true},
		{name: "KeyWithEquals", key: "A=B", value: "x", wantErr: true},
		{name: "KeyWithSpace", key: "A B", value: "x", wantErr: true},
		{name: "KeyWithNewline", key: "A\nB", value: "x", wantErr: true},
		{name: "CommentKey", key: "#A", value: "x", wantErr: true},
		{name: "ValueWithNewline", key: "A", value: "x\nB=y", wantErr: true},
		{name: "ValueWithCarriageReturn", key: "A", value: "x\rB=y", wantErr: true},
		{name: "ValueWithNUL", key: "A", value: "x\x00y", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateEnvEntry(tc.key, tc.value)
			if tc.wantErr {
				var validErr *InputValidationError
				if !errors.As(err, &validErr) {
					t.Fatalf("expected InputValidationError, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestContainerSpec_Validate_RejectsUnsafeEnv proves the guard is reached
// through the launch path's own validation, not only by direct call.
func TestContainerSpec_Validate_RejectsUnsafeEnv(t *testing.T) {
	t.Parallel()

	spec := &ContainerSpec{
		Image: "alpine:latest",
		Env:   map[string]string{"TOKEN": "a\nSMUGGLED=1"},
	}
	var validErr *InputValidationError
	if err := spec.Validate(); !errors.As(err, &validErr) {
		t.Fatalf("expected InputValidationError, got %T: %v", err, err)
	}

	rt := &ExecContainerRuntime{command: "/bin/true", logger: newRecordingLogger()}
	if _, err := rt.Run(t.Context(), spec); !errors.As(err, &validErr) {
		t.Fatalf("Run() must reject the spec: got %T: %v", err, err)
	}
	if _, err := rt.RunInteractive(t.Context(), spec); !errors.As(err, &validErr) {
		t.Fatalf("RunInteractive() must reject the spec: got %T: %v", err, err)
	}
}

// TestRedactRunArgs is the second line of defense: even if an inline
// environment pair were reintroduced, the log formatter must not print its
// value.
func TestRedactRunArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "SeparateEnvFlag",
			args: []string{"run", "-e", "ANTHROPIC_API_KEY=" + credentialFixture, "img"},
			want: "run -e ANTHROPIC_API_KEY=[redacted] img",
		},
		{
			name: "LongEnvFlag",
			args: []string{"run", "--env", "TOKEN=" + credentialFixture, "img"},
			want: "run --env TOKEN=[redacted] img",
		},
		{
			name: "InlineLongEnvFlag",
			args: []string{"run", "--env=TOKEN=" + credentialFixture, "img"},
			want: "run --env=TOKEN=[redacted] img",
		},
		{
			name: "ByReferenceEnvFlagHasNoValueToHide",
			args: []string{"run", "-e", "PATH", "img"},
			want: "run -e PATH img",
		},
		{
			name: "EnvFilePathIsKept",
			args: []string{"run", envFileFlag, "/tmp/quicbaas-container-env-1/env", "img"},
			want: "run " + envFileFlag + " /tmp/quicbaas-container-env-1/env img",
		},
		{
			name: "DanglingEnvFlag",
			args: []string{"run", "-e"},
			want: "run -e",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactRunArgs(tc.args)
			if got != tc.want {
				t.Errorf("redactRunArgs() = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, credentialFixture) {
				t.Errorf("redactRunArgs() published the credential: %q", got)
			}
		})
	}
}

func TestContainerEnvFileError(t *testing.T) {
	t.Parallel()

	err := &ContainerEnvFileError{Operation: "write", Cause: os.ErrPermission}
	if !errors.Is(err, ErrContainerEnvFile) {
		t.Error("expected errors.Is(err, ErrContainerEnvFile)")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Error("expected the cause to be unwrapped")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("Error() = %q, want it to name the failed operation", err.Error())
	}

	bare := &ContainerEnvFileError{}
	if !errors.Is(bare, ErrContainerEnvFile) {
		t.Error("expected a causeless error to still match the sentinel")
	}
}
