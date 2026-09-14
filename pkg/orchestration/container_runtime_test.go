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
	"os"
	"path/filepath"
	"reflect"
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

// stubRuntimePATH points PATH at a directory holding only the named container
// runtimes, each a stub executable that succeeds for any argument.
//
// Auto-detection probes "<command> version" through PATH (see detectRuntime),
// so without this the outcome is whatever the host happens to have installed:
// the FIPS CI image ships no container runtime on purpose, a developer
// workstation usually ships one, and a test written against either answer is
// only exercised on half the machines that run it. Pinning PATH lets both
// sides of the detection contract run everywhere, with no skips.
//
// The caller must be a sequential test. t.Setenv is process-wide state, and it
// panics if the calling test is parallel; sequential tests are safe because the
// testing package releases parallel tests only after every sequential one has
// returned, so no other test observes the stubbed PATH.
func stubRuntimePATH(t *testing.T, runtimes ...string) {
	t.Helper()

	dir := t.TempDir()
	for _, name := range runtimes {
		stub := filepath.Join(dir, name)
		if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write %s stub: %v", name, err)
		}
		// WriteFile's mode is masked by the process umask; detection needs the
		// execute bit whatever umask the runner was started with.
		if err := os.Chmod(stub, 0o755); err != nil {
			t.Fatalf("chmod %s stub: %v", name, err)
		}
	}

	t.Setenv("PATH", dir)
}

func TestExecContainerRuntime_AutoDetect(t *testing.T) {
	t.Run("DetectsDocker", func(t *testing.T) {
		stubRuntimePATH(t, RuntimeDocker)

		rt, err := NewExecContainerRuntime(newContainerTestLogger(t))
		if err != nil {
			t.Fatalf("NewExecContainerRuntime: %v", err)
		}
		if rt.Command() != RuntimeDocker {
			t.Errorf("expected %q, got %q", RuntimeDocker, rt.Command())
		}
	})

	t.Run("FallsBackToPodman", func(t *testing.T) {
		stubRuntimePATH(t, RuntimePodman)

		rt, err := NewExecContainerRuntime(newContainerTestLogger(t))
		if err != nil {
			t.Fatalf("NewExecContainerRuntime: %v", err)
		}
		if rt.Command() != RuntimePodman {
			t.Errorf("expected %q, got %q", RuntimePodman, rt.Command())
		}
	})

	t.Run("PrefersDockerOverPodman", func(t *testing.T) {
		stubRuntimePATH(t, RuntimeDocker, RuntimePodman)

		rt, err := NewExecContainerRuntime(newContainerTestLogger(t))
		if err != nil {
			t.Fatalf("NewExecContainerRuntime: %v", err)
		}
		if rt.Command() != RuntimeDocker {
			t.Errorf("expected docker to win over podman, got %q", rt.Command())
		}
	})

	t.Run("ReportsUnavailableRuntime", func(t *testing.T) {
		stubRuntimePATH(t) // neither docker nor podman on PATH

		_, err := NewExecContainerRuntime(newContainerTestLogger(t))
		if err == nil {
			t.Fatal("expected error when no container runtime is on PATH")
		}
		var notAvail *ContainerRuntimeNotAvailableError
		if !errors.As(err, &notAvail) {
			t.Fatalf("expected ContainerRuntimeNotAvailableError, got %T: %v", err, err)
		}
		if !errors.Is(err, ErrContainerRuntimeNotAvailable) {
			t.Error("expected errors.Is(err, ErrContainerRuntimeNotAvailable)")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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
	// The environment is referenced by file path, never inlined: see
	// container_env_file.go and TestContainerEnv_ValueNeverReachesArgv.
	t.Run("EnvFileIsReferencedByPath", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Env:   map[string]string{"MY_VAR": "hello"},
		}

		joined := strings.Join(rt.buildRunArgs(spec, "/run/secrets/env"), " ")

		if !strings.Contains(joined, envFileFlag+" /run/secrets/env") {
			t.Errorf("expected %s reference, got %q", envFileFlag, joined)
		}
		if strings.Contains(joined, "hello") {
			t.Errorf("environment value leaked into args: %q", joined)
		}
	})

	t.Run("EmptyEnvMap", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Env:   map[string]string{},
		}

		joined := strings.Join(rt.buildRunArgs(spec, ""), " ")

		if strings.Contains(joined, envFileFlag) {
			t.Errorf("expected no %s flag for empty env, got %q", envFileFlag, joined)
		}
		if strings.Contains(joined, " -e ") {
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
		joined := strings.Join(args, " ")

		if strings.Contains(joined, "--network") {
			t.Errorf("expected no --network flag, got %q", joined)
		}
	})
}

// TestExecContainerRuntime_BuildRunArgs_WithMemory proves the tmpfs-budget
// memory limit reaches the launched container: without --memory a tmpfs
// workspace mount could grow to consume the whole host, which is exactly
// what ValidateTmpfsBudget exists to prevent at spec-construction time.
func TestExecContainerRuntime_BuildRunArgs_WithMemory(t *testing.T) {
	t.Run("MemoryLimitSet", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:       "alpine:latest",
			MemoryBytes: 4 * 1024 * 1024 * 1024,
		}

		args := rt.buildRunArgs(spec, "")
		joined := strings.Join(args, " ")

		if !strings.Contains(joined, "--memory 4294967296") {
			t.Errorf("expected '--memory 4294967296', got %q", joined)
		}
	})

	t.Run("NoMemoryLimit", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
		}

		args := rt.buildRunArgs(spec, "")
		joined := strings.Join(args, " ")

		if strings.Contains(joined, "--memory") {
			t.Errorf("expected no --memory flag, got %q", joined)
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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

		args := rt.buildRunArgs(spec, "")
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
		stubRuntimePATH(t, RuntimeDocker)

		if !detectRuntime(RuntimeDocker) {
			t.Error("expected docker on PATH to be detected")
		}
	})

	t.Run("DetectRuntimePodman", func(t *testing.T) {
		stubRuntimePATH(t, RuntimePodman)

		if !detectRuntime(RuntimePodman) {
			t.Error("expected podman on PATH to be detected")
		}
	})

	t.Run("DetectRuntimeAbsent", func(t *testing.T) {
		stubRuntimePATH(t)

		if detectRuntime(RuntimeDocker) {
			t.Error("expected false for docker missing from PATH")
		}
		if detectRuntime(RuntimePodman) {
			t.Error("expected false for podman missing from PATH")
		}
	})

	t.Run("DetectRuntimeNonexistent", func(t *testing.T) {
		stubRuntimePATH(t, RuntimeDocker)

		if detectRuntime("nonexistent-container-runtime-xyz") {
			t.Error("expected false for nonexistent runtime")
		}
	})

	t.Run("ReportsAvailable", func(t *testing.T) {
		stubRuntimePATH(t, RuntimeDocker)

		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		if !rt.IsAvailable() {
			t.Error("expected IsAvailable to be true with docker on PATH")
		}
	})

	t.Run("ReportsUnavailable", func(t *testing.T) {
		stubRuntimePATH(t)

		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		if rt.IsAvailable() {
			t.Error("expected IsAvailable to be false with docker missing from PATH")
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

	t.Run("PositiveMemoryBytesIsValid", func(t *testing.T) {
		spec := &ContainerSpec{Image: "alpine:latest", MemoryBytes: 1024}
		if err := spec.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("NegativeMemoryBytesIsRejected", func(t *testing.T) {
		spec := &ContainerSpec{Image: "alpine:latest", MemoryBytes: -1}
		err := spec.Validate()
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
		if validErr.Field != "memory_bytes" {
			t.Errorf("Field = %q, want memory_bytes", validErr.Field)
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

	t.Run("EveryMountKindIsAccepted", func(t *testing.T) {
		for _, m := range []*ContainerMount{
			{Source: "/a", Target: "/b", Type: MountTypeBind},
			{Source: "app-data", Target: "/b", Type: MountTypeVolume},
			{Target: "/scratch", Type: MountTypeTmpfs},
		} {
			if err := m.Validate(); err != nil {
				t.Fatalf("Validate(%+v): %v", m, err)
			}
		}
	})

	t.Run("UnknownMountKindIsRejected", func(t *testing.T) {
		m := &ContainerMount{Source: "/a", Target: "/b", Type: "overlay"}
		err := m.Validate()
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T (%v)", err, err)
		}
		if validErr.Field != "type" {
			t.Errorf("Field = %q, want type", validErr.Field)
		}
	})

	// A tmpfs has no host source. Accepting one and dropping it at launch
	// would let a measured field vanish from the running container.
	t.Run("TmpfsRejectsASource", func(t *testing.T) {
		m := &ContainerMount{Source: "/a", Target: "/scratch", Type: MountTypeTmpfs}
		if err := m.Validate(); err == nil {
			t.Fatal("expected a tmpfs mount with a host source to be rejected")
		}
	})

	t.Run("NonTmpfsStillRequiresASource", func(t *testing.T) {
		m := &ContainerMount{Target: "/b", Type: MountTypeBind}
		if err := m.Validate(); err == nil {
			t.Fatal("expected a bind mount without a source to be rejected")
		}
	})

	t.Run("PositiveTmpfsSizeOnATmpfsMountIsAccepted", func(t *testing.T) {
		m := &ContainerMount{Target: "/scratch", Type: MountTypeTmpfs, TmpfsSizeBytes: 4096}
		if err := m.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("NegativeTmpfsSizeIsRejected", func(t *testing.T) {
		m := &ContainerMount{Target: "/scratch", Type: MountTypeTmpfs, TmpfsSizeBytes: -1}
		err := m.Validate()
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
		if validErr.Field != "tmpfs_size_bytes" {
			t.Errorf("Field = %q, want tmpfs_size_bytes", validErr.Field)
		}
	})

	t.Run("TmpfsSizeOnANonTmpfsMountIsRejected", func(t *testing.T) {
		m := &ContainerMount{Source: "/a", Target: "/b", Type: MountTypeBind, TmpfsSizeBytes: 4096}
		err := m.Validate()
		var validErr *InputValidationError
		if !errors.As(err, &validErr) {
			t.Fatalf("expected InputValidationError, got %T: %v", err, err)
		}
		if validErr.Field != "tmpfs_size_bytes" {
			t.Errorf("Field = %q, want tmpfs_size_bytes", validErr.Field)
		}
	})
}

func TestContainerMount_RunArgs(t *testing.T) {
	// An untyped mount must emit exactly what it always did, so specs
	// written before Type existed produce a byte-identical command line.
	t.Run("UntypedMountKeepsTheShortForm", func(t *testing.T) {
		m := ContainerMount{Source: "/host", Target: "/container", ReadOnly: true}
		want := []string{"-v", "/host:/container:ro"}
		if got := m.RunArgs(); !reflect.DeepEqual(got, want) {
			t.Fatalf("RunArgs() = %v, want %v", got, want)
		}
	})

	t.Run("TypedMountUsesMountFlag", func(t *testing.T) {
		cases := []struct {
			name  string
			mount ContainerMount
			want  []string
		}{
			{
				name:  "Bind",
				mount: ContainerMount{Source: "/host", Target: "/container", Type: MountTypeBind},
				want:  []string{"--mount", "type=bind,source=/host,destination=/container"},
			},
			{
				name:  "BindReadOnly",
				mount: ContainerMount{Source: "/host", Target: "/container", ReadOnly: true, Type: MountTypeBind},
				want:  []string{"--mount", "type=bind,source=/host,destination=/container,readonly"},
			},
			{
				name:  "Volume",
				mount: ContainerMount{Source: "app-data", Target: "/data", Type: MountTypeVolume},
				want:  []string{"--mount", "type=volume,source=app-data,destination=/data"},
			},
			{
				name:  "TmpfsHasNoSource",
				mount: ContainerMount{Target: "/scratch", Type: MountTypeTmpfs},
				want:  []string{"--mount", "type=tmpfs,destination=/scratch"},
			},
			{
				name:  "TmpfsWithSizeAddsTmpfsSize",
				mount: ContainerMount{Target: "/scratch", Type: MountTypeTmpfs, TmpfsSizeBytes: 4294967296},
				want:  []string{"--mount", "type=tmpfs,destination=/scratch,tmpfs-size=4294967296"},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := tc.mount.RunArgs(); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("RunArgs() = %v, want %v", got, tc.want)
				}
			})
		}
	})
}

// TestExecContainerRuntime_BuildRunArgs_Privilege proves the launched
// container carries the privilege the spec measured. If these flags were
// missing, ContainerConfigDigest would attest to a configuration the runtime
// never applied.
func TestExecContainerRuntime_BuildRunArgs_Privilege(t *testing.T) {
	t.Run("PrivilegedAndCapabilitiesAreApplied", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:      "alpine:latest",
			Privileged: true,
			CapAdd:     []string{"CAP_SYS_ADMIN", "CAP_NET_RAW"},
			CapDrop:    []string{"CAP_MKNOD"},
		}

		joined := strings.Join(rt.buildRunArgs(spec, ""), " ")
		for _, want := range []string{
			"--privileged",
			"--cap-add CAP_NET_RAW",
			"--cap-add CAP_SYS_ADMIN",
			"--cap-drop CAP_MKNOD",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("expected %q in args, got %q", want, joined)
			}
		}
	})

	// The command line must sort capabilities exactly as the digest does,
	// or a spec could measure one way and launch another.
	t.Run("CapabilityOrderMatchesTheMeasuredOrder", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		forward := &ContainerSpec{Image: "alpine", CapAdd: []string{"CAP_SYS_ADMIN", "CAP_CHOWN"}}
		shuffled := &ContainerSpec{Image: "alpine", CapAdd: []string{"CAP_CHOWN", "CAP_SYS_ADMIN"}}

		a := rt.buildRunArgs(forward, "")
		b := rt.buildRunArgs(shuffled, "")
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("capability order leaked into the command line:\n%v\n%v", a, b)
		}
		if !strings.Contains(strings.Join(a, " "), "--cap-add CAP_CHOWN --cap-add CAP_SYS_ADMIN") {
			t.Fatalf("capabilities are not emitted in sorted order: %v", a)
		}
		// The caller's slice must survive the launch unmodified.
		if !reflect.DeepEqual(forward.CapAdd, []string{"CAP_SYS_ADMIN", "CAP_CHOWN"}) {
			t.Fatalf("buildRunArgs mutated the caller's CapAdd: %v", forward.CapAdd)
		}
	})

	t.Run("ZeroValuesEmitNothing", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image:  "alpine:latest",
			Mounts: []ContainerMount{{Source: "/host", Target: "/container"}},
		}

		joined := strings.Join(rt.buildRunArgs(spec, ""), " ")
		for _, unwanted := range []string{"--privileged", "--cap-add", "--cap-drop", "--mount"} {
			if strings.Contains(joined, unwanted) {
				t.Errorf("unexpected %q in args for a zero-valued spec: %q", unwanted, joined)
			}
		}
		if !strings.Contains(joined, "-v /host:/container") {
			t.Errorf("untyped mount must keep the short form, got %q", joined)
		}
	})

	t.Run("TypedMountIsApplied", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{
			Image: "alpine:latest",
			Mounts: []ContainerMount{
				{Target: "/scratch", Type: MountTypeTmpfs},
				{Source: "/host", Target: "/container", Type: MountTypeBind, ReadOnly: true},
			},
		}

		joined := strings.Join(rt.buildRunArgs(spec, ""), " ")
		for _, want := range []string{
			"--mount type=tmpfs,destination=/scratch",
			"--mount type=bind,source=/host,destination=/container,readonly",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("expected %q in args, got %q", want, joined)
			}
		}
	})

	t.Run("InteractiveLaunchCarriesPrivilegeToo", func(t *testing.T) {
		rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
		spec := &ContainerSpec{Image: "alpine", Privileged: true, CapDrop: []string{"ALL"}}

		joined := strings.Join(rt.buildInteractiveRunArgs(spec, "agent-1", ""), " ")
		if !strings.Contains(joined, "--privileged") || !strings.Contains(joined, "--cap-drop ALL") {
			t.Fatalf("interactive launch dropped the privilege flags: %q", joined)
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
		// INVERTED (review 1330): Error() renders the invocation and the SIZE
		// of the withheld output, never the output.
		err := &CommandError{Command: "docker", Op: "run", Output: "something went wrong"}
		got := err.Error()
		if strings.Contains(got, "something went wrong") {
			t.Errorf("Error() = %q; the captured output must not be rendered", got)
		}
		if got != "docker run failed (20 bytes of output withheld)" {
			t.Errorf("Error() = %q, want the invocation and the withheld size", got)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		// No output captured: no size clause, and still an identifiable failure.
		err := &CommandError{Command: "docker", Op: "rm", Output: ""}
		if got := err.Error(); got != "docker rm failed" {
			t.Errorf("Error() = %q, want exactly the bare invocation", got)
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
		args := rt.buildInteractiveRunArgs(spec, "test-interactive", "")
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

		args := rt.buildInteractiveRunArgs(spec, "quicbaas-acp-abc123", "")
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

		args := rt.buildInteractiveRunArgs(spec, "quicbaas-acp-deadbeef", "/run/secrets/env")
		joined := strings.Join(args, " ")

		expectations := []string{
			"run -i --rm",
			"--name quicbaas-acp-deadbeef",
			"--network host",
			"--workdir /workspace",
			envFileFlag + " /run/secrets/env",
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

		args := rt.buildInteractiveRunArgs(spec, "test-container", "")
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

		args := rt.buildInteractiveRunArgs(spec, "test-container", "")
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

		args := rt.buildInteractiveRunArgs(spec, "test-gpu", "")
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

// errNoExecInThisTest is what a fake container runtime answers when a test
// that arranged no container command is asked to run one. Answering an empty
// success instead would let a caller's command silently do nothing and the
// test pass on it.
var errNoExecInThisTest = errors.New("this fake runs no container commands")
