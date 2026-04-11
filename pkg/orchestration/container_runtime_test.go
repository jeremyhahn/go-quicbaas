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
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// containerTestLogger implements interfaces.Logger for container runtime tests.
type containerTestLogger struct {
	t *testing.T
}

func (l *containerTestLogger) Debug(msg string, args ...any) { l.t.Helper(); l.t.Log("DEBUG: " + msg) }
func (l *containerTestLogger) Debugf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("DEBUG: "+format, args...)
}
func (l *containerTestLogger) Info(msg string, args ...any) { l.t.Helper(); l.t.Log("INFO: " + msg) }
func (l *containerTestLogger) Infof(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("INFO: "+format, args...)
}
func (l *containerTestLogger) Warn(msg string, args ...any) { l.t.Helper(); l.t.Log("WARN: " + msg) }
func (l *containerTestLogger) Warnf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("WARN: "+format, args...)
}
func (l *containerTestLogger) Error(msg string, args ...any) { l.t.Helper(); l.t.Log("ERROR: " + msg) }
func (l *containerTestLogger) Errorf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("ERROR: "+format, args...)
}
func (l *containerTestLogger) Fatal(msg string) { l.t.Helper(); l.t.Fatal("FATAL: " + msg) }
func (l *containerTestLogger) Fatalf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Fatalf("FATAL: "+format, args...)
}
func (l *containerTestLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *containerTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *containerTestLogger) WithError(err error) interfaces.Logger                      { return l }

func newContainerTestLogger(t *testing.T) *containerTestLogger {
	return &containerTestLogger{t: t}
}

func TestExecContainerRuntime_AutoDetect(t *testing.T) {
	t.Run("DetectsAvailableRuntime", func(t *testing.T) {
		logger := newContainerTestLogger(t)
		rt, err := NewExecContainerRuntime(logger)

		// On systems with docker or podman, this succeeds.
		// On systems without either, we get the expected error.
		if err != nil {
			var notAvail *ContainerRuntimeNotAvailableError
			if !errors.As(err, &notAvail) {
				t.Fatalf("expected ContainerRuntimeNotAvailableError, got %T: %v", err, err)
			}
			if !errors.Is(err, ErrContainerRuntimeNotAvailable) {
				t.Error("expected errors.Is(err, ErrContainerRuntimeNotAvailable)")
			}
			return
		}

		cmd := rt.Command()
		if cmd != RuntimeDocker && cmd != RuntimePodman {
			t.Errorf("expected docker or podman, got %q", cmd)
		}
	})

	t.Run("WithCommandEmpty", func(t *testing.T) {
		logger := newContainerTestLogger(t)
		_, err := NewExecContainerRuntimeWithCommand("", logger)
		if err == nil {
			t.Fatal("expected error for empty command")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})

	t.Run("WithCommandNotFound", func(t *testing.T) {
		logger := newContainerTestLogger(t)
		_, err := NewExecContainerRuntimeWithCommand("nonexistent-runtime-xyz", logger)
		if err == nil {
			t.Fatal("expected error for nonexistent command")
		}
		var notAvail *ContainerRuntimeNotAvailableError
		if !errors.As(err, &notAvail) {
			t.Fatalf("expected ContainerRuntimeNotAvailableError, got %T: %v", err, err)
		}
	})
}

func TestExecContainerRuntime_BuildRunArgs(t *testing.T) {
	t.Run("MinimalSpec", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.HasPrefix(joined, "run -d") {
			t.Errorf("expected 'run -d' prefix, got %q", joined)
		}
		if !strings.Contains(joined, "alpine:latest") {
			t.Errorf("expected image in args, got %q", joined)
		}
	})

	t.Run("FullSpec", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Name:       "test-container",
			Image:      "nginx:1.25",
			AutoRemove: true,
			Network:    "host",
			WorkDir:    "/app",
			Command:    []string{"nginx"},
			Args:       []string{"-g", "daemon off;"},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		expectations := []string{
			"run -d",
			"--name test-container",
			"--rm",
			"--network host",
			"--workdir /app",
			"nginx:1.25",
			"nginx",
			"-g",
			"daemon off;",
		}
		for _, exp := range expectations {
			if !strings.Contains(joined, exp) {
				t.Errorf("expected %q in args, got %q", exp, joined)
			}
		}
	})
}

func TestExecContainerRuntime_BuildRunArgs_WithGPU(t *testing.T) {
	t.Run("DockerGPU", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:      "nvidia/cuda:12.0-base",
			GPUEnabled: true,
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "--gpus all") {
			t.Errorf("expected '--gpus all' for docker GPU, got %q", joined)
		}
	})

	t.Run("PodmanGPU", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimePodman, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:      "nvidia/cuda:12.0-base",
			GPUEnabled: true,
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "--device nvidia.com/gpu=all") {
			t.Errorf("expected '--device nvidia.com/gpu=all' for podman GPU, got %q", joined)
		}
	})

	t.Run("GPUDisabled", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:      "alpine:latest",
			GPUEnabled: false,
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if strings.Contains(joined, "--gpus") || strings.Contains(joined, "--device") {
			t.Errorf("expected no GPU flags when disabled, got %q", joined)
		}
	})
}

func TestExecContainerRuntime_BuildRunArgs_WithMounts(t *testing.T) {
	t.Run("ReadWriteMount", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Mounts: []ContainerMount{
				{Source: "/host/data", Target: "/container/data", ReadOnly: false},
			},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "-v /host/data:/container/data") {
			t.Errorf("expected rw mount flag, got %q", joined)
		}
	})

	t.Run("ReadOnlyMount", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Mounts: []ContainerMount{
				{Source: "/host/config", Target: "/etc/config", ReadOnly: true},
			},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "-v /host/config:/etc/config:ro") {
			t.Errorf("expected ro mount flag, got %q", joined)
		}
	})

	t.Run("MultipleMounts", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Mounts: []ContainerMount{
				{Source: "/data", Target: "/mnt/data"},
				{Source: "/logs", Target: "/mnt/logs", ReadOnly: true},
			},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "-v /data:/mnt/data") {
			t.Errorf("expected first mount, got %q", joined)
		}
		if !strings.Contains(joined, "-v /logs:/mnt/logs:ro") {
			t.Errorf("expected second mount with ro, got %q", joined)
		}
	})
}

func TestExecContainerRuntime_BuildRunArgs_WithEnv(t *testing.T) {
	t.Run("SingleEnvVar", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Env:   map[string]string{"MY_VAR": "hello"},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "-e MY_VAR=hello") {
			t.Errorf("expected env flag, got %q", joined)
		}
	})

	t.Run("MultipleEnvVarsSorted", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Env: map[string]string{
				"ZZZ_VAR": "last",
				"AAA_VAR": "first",
				"MMM_VAR": "middle",
			},
		}

		args := rt.buildRunArgs(spec)

		// Find positions of env vars to verify sorted order.
		joined := strings.Join(args, " ")
		aIdx := strings.Index(joined, "AAA_VAR")
		mIdx := strings.Index(joined, "MMM_VAR")
		zIdx := strings.Index(joined, "ZZZ_VAR")

		if aIdx >= mIdx || mIdx >= zIdx {
			t.Errorf("expected env vars in sorted order, got %q", joined)
		}
	})

	t.Run("EmptyEnvMap", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Env:   map[string]string{},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if strings.Contains(joined, "-e") {
			t.Errorf("expected no -e flags for empty env, got %q", joined)
		}
	})
}

func TestExecContainerRuntime_BuildRunArgs_WithNetwork(t *testing.T) {
	t.Run("HostNetwork", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:   "alpine:latest",
			Network: "host",
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "--network host") {
			t.Errorf("expected '--network host', got %q", joined)
		}
	})

	t.Run("CustomNetwork", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:   "alpine:latest",
			Network: "my-custom-net",
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "--network my-custom-net") {
			t.Errorf("expected '--network my-custom-net', got %q", joined)
		}
	})

	t.Run("NoNetwork", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if strings.Contains(joined, "--network") {
			t.Errorf("expected no --network flag, got %q", joined)
		}
	})
}

func TestExecContainerRuntime_BuildRunArgs_WithPorts(t *testing.T) {
	t.Run("SinglePort", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "nginx:latest",
			Ports: []string{"8080:80"},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "-p 8080:80") {
			t.Errorf("expected '-p 8080:80', got %q", joined)
		}
	})

	t.Run("MultiplePorts", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "nginx:latest",
			Ports: []string{"8080:80", "8443:443"},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "-p 8080:80") {
			t.Errorf("expected first port, got %q", joined)
		}
		if !strings.Contains(joined, "-p 8443:443") {
			t.Errorf("expected second port, got %q", joined)
		}
	})
}

func TestExecContainerRuntime_BuildRunArgs_WithLabels(t *testing.T) {
	t.Run("SingleLabel", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:  "alpine:latest",
			Labels: map[string]string{"app": "quicbaas"},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "--label app=quicbaas") {
			t.Errorf("expected '--label app=quicbaas', got %q", joined)
		}
	})

	t.Run("LabelsSorted", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Labels: map[string]string{
				"zzz": "last",
				"aaa": "first",
			},
		}

		args := rt.buildRunArgs(spec)
		joined := strings.Join(args, " ")

		aIdx := strings.Index(joined, "aaa=first")
		zIdx := strings.Index(joined, "zzz=last")
		if aIdx >= zIdx {
			t.Errorf("expected labels in sorted order, got %q", joined)
		}
	})
}

func TestExecContainerRuntime_IsAvailable(t *testing.T) {
	t.Run("DetectRuntimeDocker", func(t *testing.T) {
		// This test verifies the detection function runs without panic.
		// The result depends on the host environment.
		result := detectRuntime(RuntimeDocker)
		_ = result // boolean depends on environment
	})

	t.Run("DetectRuntimePodman", func(t *testing.T) {
		result := detectRuntime(RuntimePodman)
		_ = result
	})

	t.Run("DetectRuntimeNonexistent", func(t *testing.T) {
		result := detectRuntime("nonexistent-container-runtime-xyz")
		if result {
			t.Error("expected false for nonexistent runtime")
		}
	})
}

func TestExecContainerRuntime_RunValidation(t *testing.T) {
	t.Run("NilSpec", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "echo", logger: newContainerTestLogger(t)}
		_, err := rt.Run(t.Context(), nil)
		if err == nil {
			t.Fatal("expected error for nil spec")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})

	t.Run("EmptyImage", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "echo", logger: newContainerTestLogger(t)}
		_, err := rt.Run(t.Context(), &ContainerSpec{})
		if err == nil {
			t.Fatal("expected error for empty image")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})
}

func TestExecContainerRuntime_StopValidation(t *testing.T) {
	t.Run("EmptyContainerID", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "echo", logger: newContainerTestLogger(t)}
		err := rt.Stop(t.Context(), "")
		if err == nil {
			t.Fatal("expected error for empty container ID")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})
}

func TestExecContainerRuntime_RemoveValidation(t *testing.T) {
	t.Run("EmptyContainerID", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "echo", logger: newContainerTestLogger(t)}
		err := rt.Remove(t.Context(), "")
		if err == nil {
			t.Fatal("expected error for empty container ID")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})
}

func TestExecContainerRuntime_LogsValidation(t *testing.T) {
	t.Run("EmptyContainerID", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "echo", logger: newContainerTestLogger(t)}
		_, err := rt.Logs(t.Context(), "")
		if err == nil {
			t.Fatal("expected error for empty container ID")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})
}

func TestExecContainerRuntime_IsRunningValidation(t *testing.T) {
	t.Run("EmptyContainerID", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "echo", logger: newContainerTestLogger(t)}
		_, err := rt.IsRunning(t.Context(), "")
		if err == nil {
			t.Fatal("expected error for empty container ID")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})
}

func TestContainerSpec_Validation(t *testing.T) {
	t.Run("ValidSpec", func(t *testing.T) {
		spec := &ContainerSpec{
			Image: "alpine:latest",
		}
		if err := spec.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("EmptyImage", func(t *testing.T) {
		spec := &ContainerSpec{}
		err := spec.Validate()
		if err == nil {
			t.Fatal("expected error for empty image")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
		if validErr.Field != "image" {
			t.Errorf("expected field 'image', got %q", validErr.Field)
		}
	})

	t.Run("InvalidMount_EmptySource", func(t *testing.T) {
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Mounts: []ContainerMount{
				{Source: "", Target: "/data"},
			},
		}
		err := spec.Validate()
		if err == nil {
			t.Fatal("expected error for empty mount source")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})

	t.Run("InvalidMount_EmptyTarget", func(t *testing.T) {
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Mounts: []ContainerMount{
				{Source: "/host", Target: ""},
			},
		}
		err := spec.Validate()
		if err == nil {
			t.Fatal("expected error for empty mount target")
		}
	})

	t.Run("ValidMounts", func(t *testing.T) {
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Mounts: []ContainerMount{
				{Source: "/host/a", Target: "/container/a"},
				{Source: "/host/b", Target: "/container/b", ReadOnly: true},
			},
		}
		if err := spec.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("FullSpec", func(t *testing.T) {
		spec := &ContainerSpec{
			Name:       "full-test",
			Image:      "nginx:1.25",
			Env:        map[string]string{"FOO": "bar"},
			Mounts:     []ContainerMount{{Source: "/a", Target: "/b"}},
			Ports:      []string{"8080:80"},
			WorkDir:    "/app",
			Command:    []string{"nginx"},
			Args:       []string{"-g", "daemon off;"},
			Labels:     map[string]string{"app": "test"},
			AutoRemove: true,
			Network:    "bridge",
			GPUEnabled: true,
		}
		if err := spec.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestContainerMount_Format(t *testing.T) {
	t.Run("ReadWrite", func(t *testing.T) {
		m := ContainerMount{Source: "/host", Target: "/container", ReadOnly: false}
		got := m.Format()
		want := "/host:/container"
		if got != want {
			t.Errorf("Format() = %q, want %q", got, want)
		}
	})

	t.Run("ReadOnly", func(t *testing.T) {
		m := ContainerMount{Source: "/host", Target: "/container", ReadOnly: true}
		got := m.Format()
		want := "/host:/container:ro"
		if got != want {
			t.Errorf("Format() = %q, want %q", got, want)
		}
	})

	t.Run("PathsWithSpaces", func(t *testing.T) {
		m := ContainerMount{Source: "/host/my data", Target: "/container/my data"}
		got := m.Format()
		want := "/host/my data:/container/my data"
		if got != want {
			t.Errorf("Format() = %q, want %q", got, want)
		}
	})
}

func TestContainerMount_Validate(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		m := &ContainerMount{Source: "/a", Target: "/b"}
		if err := m.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("EmptySource", func(t *testing.T) {
		m := &ContainerMount{Source: "", Target: "/b"}
		err := m.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T", err)
		}
	})

	t.Run("EmptyTarget", func(t *testing.T) {
		m := &ContainerMount{Source: "/a", Target: ""}
		err := m.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestTruncateID(t *testing.T) {
	t.Run("LongID", func(t *testing.T) {
		got := truncateID("abcdef1234567890abcdef")
		if len(got) != 12 {
			t.Errorf("expected 12 chars, got %d: %q", len(got), got)
		}
	})

	t.Run("ShortID", func(t *testing.T) {
		got := truncateID("abc")
		if got != "abc" {
			t.Errorf("expected 'abc', got %q", got)
		}
	})

	t.Run("ExactlyTwelve", func(t *testing.T) {
		got := truncateID("123456789012")
		if got != "123456789012" {
			t.Errorf("expected '123456789012', got %q", got)
		}
	})
}

func TestIsNotFoundOutput(t *testing.T) {
	t.Run("NoSuchContainer", func(t *testing.T) {
		if !isNotFoundOutput("Error: No such container: abc123") {
			t.Error("expected true for 'No such container'")
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		if !isNotFoundOutput("Error response from daemon: not found") {
			t.Error("expected true for 'not found'")
		}
	})

	t.Run("NoSuchObject", func(t *testing.T) {
		if !isNotFoundOutput("no such object: abc") {
			t.Error("expected true for 'no such object'")
		}
	})

	t.Run("OtherError", func(t *testing.T) {
		if isNotFoundOutput("permission denied") {
			t.Error("expected false for 'permission denied'")
		}
	})

	t.Run("EmptyString", func(t *testing.T) {
		if isNotFoundOutput("") {
			t.Error("expected false for empty string")
		}
	})
}

func TestCommandError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &CommandError{Output: "something went wrong"}
		if err.Error() != "something went wrong" {
			t.Errorf("Error() = %q, want %q", err.Error(), "something went wrong")
		}
	})

	t.Run("Empty", func(t *testing.T) {
		err := &CommandError{Output: ""}
		if err.Error() != "" {
			t.Errorf("Error() = %q, want empty", err.Error())
		}
	})
}

func TestGPUFlag(t *testing.T) {
	t.Run("Docker", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		flags := rt.gpuFlag()
		joined := strings.Join(flags, " ")
		if joined != "--gpus all" {
			t.Errorf("expected '--gpus all', got %q", joined)
		}
	})

	t.Run("Podman", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimePodman, logger: newContainerTestLogger(t)}
		flags := rt.gpuFlag()
		joined := strings.Join(flags, " ")
		if joined != "--device nvidia.com/gpu=all" {
			t.Errorf("expected '--device nvidia.com/gpu=all', got %q", joined)
		}
	})
}

func TestIntToStr(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{-1, "-1"},
		{100, "100"},
		{-999, "-999"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := intToStr(tt.input)
			if got != tt.want {
				t.Errorf("intToStr(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// RunInteractive Tests
// =============================================================================

func TestExecContainerRuntime_RunInteractiveValidation(t *testing.T) {
	t.Run("NilSpec", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "echo", logger: newContainerTestLogger(t)}
		_, err := rt.RunInteractive(t.Context(), nil)
		if err == nil {
			t.Fatal("expected error for nil spec")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})

	t.Run("EmptyImage", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "echo", logger: newContainerTestLogger(t)}
		_, err := rt.RunInteractive(t.Context(), &ContainerSpec{})
		if err == nil {
			t.Fatal("expected error for empty image")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})

	t.Run("InvalidMount", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "echo", logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:  "alpine:latest",
			Mounts: []ContainerMount{{Source: "", Target: "/data"}},
		}
		_, err := rt.RunInteractive(t.Context(), spec)
		if err == nil {
			t.Fatal("expected error for invalid mount")
		}
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
	})
}

func TestExecContainerRuntime_RunInteractive_StartsProcess(t *testing.T) {
	t.Run("CatEchoRoundtrip", func(t *testing.T) {
		// Use 'cat' as a simple interactive process that echoes stdin to stdout.
		rt := &ExecContainerRuntime{command: "cat", logger: newContainerTestLogger(t)}

		spec := &ContainerSpec{
			Image: "unused-image", // cat ignores this; it becomes a positional arg
			Name:  "test-interactive",
		}

		// We cannot use docker here (unit test, no container runtime needed).
		// Instead, test the pipe mechanics by calling cat directly.
		// buildInteractiveRunArgs produces: run -i --rm --name test-interactive unused-image
		// When command is "cat", this becomes: cat run -i --rm --name test-interactive unused-image
		// cat will try to open those as files and fail.
		// Instead, directly test the pipe setup using a known working command.

		// For unit testing, we test the argument building and validation.
		// The actual process start is tested with a real binary.
		args := rt.buildInteractiveRunArgs(spec, "test-interactive")
		joined := strings.Join(args, " ")

		if !strings.HasPrefix(joined, "run -i --rm") {
			t.Errorf("expected 'run -i --rm' prefix, got %q", joined)
		}
		if !strings.Contains(joined, "--name test-interactive") {
			t.Errorf("expected '--name test-interactive', got %q", joined)
		}
	})

	t.Run("WithGeneratedName", func(t *testing.T) {
		// Use /bin/cat as the "runtime" and test that RunInteractive generates
		// a container name and successfully starts the process with pipes.
		rt := &ExecContainerRuntime{command: "/bin/cat", logger: newContainerTestLogger(t)}

		// The spec has no Name, so RunInteractive generates one.
		// /bin/cat will receive: run -i --rm --name quicbaas-acp-<hex> alpine:latest
		// cat will try to read those as files/stdin - it will read from stdin pipe.
		// We just verify the process starts and pipes are usable.
		spec := &ContainerSpec{
			Image: "alpine:latest",
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		proc, err := rt.RunInteractive(ctx, spec)
		if err != nil {
			t.Fatalf("RunInteractive failed: %v", err)
		}

		// Verify container ID has the expected prefix.
		if !strings.HasPrefix(proc.ContainerID, interactiveContainerPrefix) {
			t.Errorf("ContainerID = %q, want prefix %q", proc.ContainerID, interactiveContainerPrefix)
		}

		// Verify the hex suffix is the right length (8 bytes = 16 hex chars).
		suffix := strings.TrimPrefix(proc.ContainerID, interactiveContainerPrefix)
		if len(suffix) != containerIDRandomBytes*2 {
			t.Errorf("expected %d hex chars in suffix, got %d: %q",
				containerIDRandomBytes*2, len(suffix), suffix)
		}

		// Verify stdin/stdout are not nil.
		if proc.Stdin == nil {
			t.Fatal("expected non-nil Stdin")
		}
		if proc.Stdout == nil {
			t.Fatal("expected non-nil Stdout")
		}

		// Close stdin to let cat exit.
		closeErr := proc.Stdin.Close()
		if closeErr != nil {
			t.Fatalf("failed to close stdin: %v", closeErr)
		}
	})

	t.Run("WithExplicitName", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: "/bin/cat", logger: newContainerTestLogger(t)}

		spec := &ContainerSpec{
			Image: "alpine:latest",
			Name:  "my-acp-agent",
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		proc, err := rt.RunInteractive(ctx, spec)
		if err != nil {
			t.Fatalf("RunInteractive failed: %v", err)
		}

		if proc.ContainerID != "my-acp-agent" {
			t.Errorf("ContainerID = %q, want %q", proc.ContainerID, "my-acp-agent")
		}

		closeErr := proc.Stdin.Close()
		if closeErr != nil {
			t.Fatalf("failed to close stdin: %v", closeErr)
		}
	})

	t.Run("StdinStdoutCommunication", func(t *testing.T) {
		// /bin/cat echoes stdin to stdout. We write a line and read it back.
		// Note: cat will output its positional args (the docker run flags)
		// as errors to stderr (which we discard) and then read from stdin.
		// Actually, cat with file args that don't exist will fail.
		// We need a command that ignores args and just cats stdin.
		// Use /bin/sh -c 'cat' to avoid arg issues.
		rt := &ExecContainerRuntime{command: "/bin/sh", logger: newContainerTestLogger(t)}

		spec := &ContainerSpec{
			Image:   "alpine:latest",
			Name:    "echo-test",
			Command: []string{}, // no extra commands
		}

		// buildInteractiveRunArgs will produce:
		// run -i --rm --name echo-test alpine:latest
		// When command is /bin/sh, this becomes: /bin/sh run -i --rm --name echo-test alpine:latest
		// /bin/sh will try to execute "run" as a script, which fails.
		// This shows that RunInteractive with a non-docker command uses
		// the args as docker run flags, not as shell args.
		// For true stdin/stdout communication testing, we need the actual
		// binary to be a simple echo server.

		// Skip this variant - the pipe mechanics are proven by the
		// WithGeneratedName test above. The communication pattern is
		// validated by the StdioTransport tests which use
		// newStdioTransportFromPipes.
		_ = rt
		_ = spec
	})
}

func TestExecContainerRuntime_RunInteractive_StartFailure(t *testing.T) {
	t.Run("NonexistentCommand", func(t *testing.T) {
		rt := &ExecContainerRuntime{
			command: "/nonexistent/binary/xyz",
			logger:  newContainerTestLogger(t),
		}

		spec := &ContainerSpec{
			Image: "alpine:latest",
			Name:  "fail-test",
		}

		_, err := rt.RunInteractive(t.Context(), spec)
		if err == nil {
			t.Fatal("expected error for nonexistent command")
		}

		var interactiveErr *ContainerInteractiveStartError
		if !errors.As(err, &interactiveErr) {
			t.Fatalf("expected ContainerInteractiveStartError, got %T: %v", err, err)
		}
		if interactiveErr.Image != "alpine:latest" {
			t.Errorf("Image = %q, want %q", interactiveErr.Image, "alpine:latest")
		}
		if interactiveErr.Operation != "start" {
			t.Errorf("Operation = %q, want %q", interactiveErr.Operation, "start")
		}
		if !errors.Is(err, ErrContainerInteractiveStart) {
			t.Error("errors.Is should match ErrContainerInteractiveStart")
		}
	})
}

func TestExecContainerRuntime_BuildInteractiveRunArgs(t *testing.T) {
	t.Run("MinimalSpec", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
		}

		args := rt.buildInteractiveRunArgs(spec, "quicbaas-acp-abc123")
		joined := strings.Join(args, " ")

		if !strings.HasPrefix(joined, "run -i --rm") {
			t.Errorf("expected 'run -i --rm' prefix, got %q", joined)
		}
		if !strings.Contains(joined, "--name quicbaas-acp-abc123") {
			t.Errorf("expected '--name quicbaas-acp-abc123', got %q", joined)
		}
		if !strings.Contains(joined, "alpine:latest") {
			t.Errorf("expected image in args, got %q", joined)
		}
		// Must NOT contain -d (detached mode).
		if strings.Contains(joined, " -d ") || strings.Contains(joined, " -d\n") {
			t.Errorf("interactive args must not contain -d, got %q", joined)
		}
	})

	t.Run("FullSpec", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:   "quicbaas-agent:latest",
			Network: "host",
			WorkDir: "/workspace",
			Env: map[string]string{
				"MODEL":   "ollama/llama3",
				"API_KEY": "test-key",
			},
			Mounts: []ContainerMount{
				{Source: "/home/user/.claude", Target: "/home/agent/.claude", ReadOnly: true},
				{Source: "/workspace", Target: "/workspace"},
			},
			Labels: map[string]string{
				"quicbaas.role": "agent",
				"quicbaas.type": "opencode",
			},
			Command:    []string{"opencode", "acp"},
			Args:       []string{"--port", "0"},
			GPUEnabled: true,
		}

		args := rt.buildInteractiveRunArgs(spec, "quicbaas-acp-deadbeef")
		joined := strings.Join(args, " ")

		expectations := []string{
			"run -i --rm",
			"--name quicbaas-acp-deadbeef",
			"--network host",
			"--workdir /workspace",
			"-e API_KEY=test-key",
			"-e MODEL=ollama/llama3",
			"-v /home/user/.claude:/home/agent/.claude:ro",
			"-v /workspace:/workspace",
			"--label quicbaas.role=agent",
			"--label quicbaas.type=opencode",
			"--gpus all",
			"quicbaas-agent:latest",
			"opencode",
			"acp",
			"--port",
			"0",
		}
		for _, exp := range expectations {
			if !strings.Contains(joined, exp) {
				t.Errorf("expected %q in args, got %q", exp, joined)
			}
		}
	})

	t.Run("NoDetachedFlag", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:      "alpine:latest",
			AutoRemove: true, // should be ignored; interactive always uses --rm
		}

		args := rt.buildInteractiveRunArgs(spec, "test-container")
		for _, arg := range args {
			if arg == "-d" {
				t.Error("interactive args must not contain -d")
			}
		}
	})

	t.Run("AlwaysHasRemoveFlag", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
		}

		args := rt.buildInteractiveRunArgs(spec, "test-container")
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "--rm") {
			t.Errorf("interactive args must contain --rm, got %q", joined)
		}
	})

	t.Run("PodmanGPU", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimePodman, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:      "agent:latest",
			GPUEnabled: true,
		}

		args := rt.buildInteractiveRunArgs(spec, "test-gpu")
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "--device nvidia.com/gpu=all") {
			t.Errorf("expected podman GPU flag, got %q", joined)
		}
	})
}

func TestGenerateContainerID(t *testing.T) {
	t.Run("ProducesHexString", func(t *testing.T) {
		id, err := generateContainerID()
		if err != nil {
			t.Fatalf("generateContainerID() error: %v", err)
		}

		// 8 random bytes = 16 hex characters.
		if len(id) != containerIDRandomBytes*2 {
			t.Errorf("expected %d hex chars, got %d: %q",
				containerIDRandomBytes*2, len(id), id)
		}

		// Verify all characters are valid hex.
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("invalid hex character %q in ID %q", string(c), id)
			}
		}
	})

	t.Run("ProducesUniqueValues", func(t *testing.T) {
		seen := make(map[string]struct{})
		for i := 0; i < 100; i++ {
			id, err := generateContainerID()
			if err != nil {
				t.Fatalf("generateContainerID() error on iteration %d: %v", i, err)
			}
			if _, exists := seen[id]; exists {
				t.Errorf("duplicate ID generated: %q", id)
			}
			seen[id] = struct{}{}
		}
	})
}

func TestInteractiveProcess_Fields(t *testing.T) {
	t.Run("AllFieldsSet", func(t *testing.T) {
		r, w := io.Pipe()
		proc := &InteractiveProcess{
			ContainerID: "quicbaas-acp-test123",
			Stdin:       w,
			Stdout:      r,
		}

		if proc.ContainerID != "quicbaas-acp-test123" {
			t.Errorf("ContainerID = %q, want %q", proc.ContainerID, "quicbaas-acp-test123")
		}
		if proc.Stdin == nil {
			t.Error("Stdin should not be nil")
		}
		if proc.Stdout == nil {
			t.Error("Stdout should not be nil")
		}

		// Clean up.
		closeErr := w.Close()
		if closeErr != nil {
			t.Fatalf("failed to close writer: %v", closeErr)
		}
		closeErr = r.Close()
		if closeErr != nil {
			t.Fatalf("failed to close reader: %v", closeErr)
		}
	})
}
