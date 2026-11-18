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

package fleet

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// namedContainerRuntime is a fakeContainerRuntime that also names the engine
// command behind it, which is what the resolver logs at boot.
type namedContainerRuntime struct {
	fakeContainerRuntime
	command string
}

func (n *namedContainerRuntime) Command() string { return n.command }

// errEngineMissing stands in for "docker/podman is not installed here" so the
// selector table runs identically on a host with an engine and without one.
var errEngineMissing = errors.New("engine not present in this test")

// recordingRuntimeFactory records what the resolver asked it to build and
// answers from a fixed script, so the selector table never touches the host.
type recordingRuntimeFactory struct {
	// commands records each engine name withCommand was asked for.
	commands []string
	// detectCalls counts auto-detection attempts.
	detectCalls int
	// available lists the engines this fake host has; detect answers with
	// the first entry.
	available []string
}

// factory returns the injectable factory backed by this recorder.
func (r *recordingRuntimeFactory) factory() containerRuntimeFactory {
	return containerRuntimeFactory{
		withCommand: func(command string, _ interfaces.Logger) (orchestration.ContainerRuntime, error) {
			r.commands = append(r.commands, command)
			for _, have := range r.available {
				if have == command {
					return &namedContainerRuntime{fakeContainerRuntime: fakeContainerRuntime{available: true}, command: command}, nil
				}
			}
			return nil, errEngineMissing
		},
		detect: func(_ interfaces.Logger) (orchestration.ContainerRuntime, error) {
			r.detectCalls++
			if len(r.available) == 0 {
				return nil, errEngineMissing
			}
			return &namedContainerRuntime{fakeContainerRuntime: fakeContainerRuntime{available: true}, command: r.available[0]}, nil
		},
	}
}

// TestResolveContainerRuntime_SelectorTable is the regression for
// go-quicbaas#732: every --container-runtime value must resolve to the engine
// the operator named, because a daemon that resolves none refuses every
// sandboxed spawn.
func TestResolveContainerRuntime_SelectorTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		selector  ContainerRuntimeSelector
		available []string

		wantCommand  string // "" means no runtime expected
		wantCommands []string
		wantDetect   int
		wantLog      string
	}{
		{
			name:        "docker on a host that has it",
			selector:    ContainerRuntimeDocker,
			available:   []string{"docker"},
			wantCommand: "docker", wantCommands: []string{"docker"},
			wantLog: "container runtime: docker (available)",
		},
		{
			name:        "podman on a host that has it",
			selector:    ContainerRuntimePodman,
			available:   []string{"podman"},
			wantCommand: "podman", wantCommands: []string{"podman"},
			wantLog: "container runtime: podman (available)",
		},
		{
			name:         "docker named but absent degrades to no runtime",
			selector:     ContainerRuntimeDocker,
			available:    nil,
			wantCommands: []string{"docker"},
			wantLog:      "docker selected but unavailable",
		},
		{
			name:        "auto takes the first engine detection finds",
			selector:    ContainerRuntimeAuto,
			available:   []string{"podman"},
			wantCommand: "podman", wantDetect: 1,
			wantLog: "container runtime: podman (available)",
		},
		{
			name:       "auto on a host with no engine resolves to none",
			selector:   ContainerRuntimeAuto,
			available:  nil,
			wantDetect: 1,
			wantLog:    "auto-detection found no engine",
		},
		{
			name:      "empty selector defaults to auto",
			selector:  "",
			available: []string{"docker"},
			// The default must probe, not silently disable sandboxing.
			wantCommand: "docker", wantDetect: 1,
			wantLog: "container runtime: docker (available)",
		},
		{
			name:      "none never probes the host",
			selector:  ContainerRuntimeNone,
			available: []string{"docker"},
			wantLog:   "container runtime: none (disabled by configuration)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := &recordingRuntimeFactory{available: tc.available}
			logger := newSignalLogger(t, "")

			rt, err := resolveContainerRuntime(tc.selector, logger, recorder.factory())
			if err != nil {
				t.Fatalf("resolveContainerRuntime: %v", err)
			}

			if tc.wantCommand == "" {
				if rt != nil {
					t.Fatalf("expected no container runtime, got %T", rt)
				}
			} else {
				if rt == nil {
					t.Fatal("expected a container runtime, got none")
				}
				if got := containerRuntimeName(rt); got != tc.wantCommand {
					t.Fatalf("engine = %q, want %q", got, tc.wantCommand)
				}
				if !rt.IsAvailable() {
					t.Fatal("resolved runtime reports itself unavailable")
				}
			}

			if len(recorder.commands) != len(tc.wantCommands) {
				t.Fatalf("withCommand calls = %v, want %v", recorder.commands, tc.wantCommands)
			}
			for i, want := range tc.wantCommands {
				if recorder.commands[i] != want {
					t.Fatalf("withCommand[%d] = %q, want %q", i, recorder.commands[i], want)
				}
			}
			if recorder.detectCalls != tc.wantDetect {
				t.Fatalf("detect calls = %d, want %d", recorder.detectCalls, tc.wantDetect)
			}
			if logger.count(tc.wantLog) != 1 {
				t.Fatalf("expected exactly one boot log line containing %q, got %d", tc.wantLog, logger.count(tc.wantLog))
			}
		})
	}
}

// TestResolveContainerRuntime_UnknownSelectorIsTypedError asserts that a
// selector nobody implements is refused rather than silently treated as
// "none", which would leave a daemon quietly unable to sandbox.
func TestResolveContainerRuntime_UnknownSelectorIsTypedError(t *testing.T) {
	t.Parallel()

	recorder := &recordingRuntimeFactory{available: []string{"docker"}}
	rt, err := resolveContainerRuntime(ContainerRuntimeSelector("runc"), &testLogger{t: t}, recorder.factory())
	if rt != nil {
		t.Fatalf("expected no runtime for an unknown selector, got %T", rt)
	}
	if !errors.Is(err, ErrConfigUnknownRuntime) {
		t.Fatalf("expected ErrConfigUnknownRuntime, got %v", err)
	}
	var detail *SentinelDetailError
	if !errors.As(err, &detail) {
		t.Fatalf("expected *SentinelDetailError, got %T", err)
	}
	if detail.Detail != `"runc"` {
		t.Fatalf("detail = %q, want %q", detail.Detail, `"runc"`)
	}
	if recorder.detectCalls != 0 || len(recorder.commands) != 0 {
		t.Fatal("an unknown selector must not probe the host")
	}
}

// TestResolveContainerRuntime_NilLoggerIsTypedError covers the other bad
// input: the resolver reports its outcome through the logger, so it refuses
// to run without one instead of panicking at the first log line.
func TestResolveContainerRuntime_NilLoggerIsTypedError(t *testing.T) {
	t.Parallel()

	rt, err := ResolveContainerRuntime(ContainerRuntimeNone, nil)
	if rt != nil {
		t.Fatalf("expected no runtime, got %T", rt)
	}
	if !errors.Is(err, ErrRuntimeNoLogger) {
		t.Fatalf("expected ErrRuntimeNoLogger, got %v", err)
	}
}

// TestResolveContainerRuntime_ProductionFactory exercises the exported entry
// point on the two selectors whose answer does not depend on what is installed
// on the host running the test.
func TestResolveContainerRuntime_ProductionFactory(t *testing.T) {
	t.Parallel()

	logger := newSignalLogger(t, "")
	rt, err := ResolveContainerRuntime(ContainerRuntimeNone, logger)
	if err != nil {
		t.Fatalf("ResolveContainerRuntime(none): %v", err)
	}
	if rt != nil {
		t.Fatalf("selector none must resolve to no runtime, got %T", rt)
	}
	if logger.count("container runtime: none (disabled by configuration)") != 1 {
		t.Fatal("selector none must announce itself in the boot log")
	}

	if _, err := ResolveContainerRuntime(ContainerRuntimeSelector("kubernetes"), logger); !errors.Is(err, ErrConfigUnknownRuntime) {
		t.Fatalf("expected ErrConfigUnknownRuntime, got %v", err)
	}
}

// TestContainerRuntimeName_UnnamedRuntime covers the logging fallback for a
// driver that does not name an engine command.
func TestContainerRuntimeName_UnnamedRuntime(t *testing.T) {
	t.Parallel()

	if got := containerRuntimeName(&fakeContainerRuntime{available: true}); got != "configured" {
		t.Fatalf("containerRuntimeName = %q, want %q", got, "configured")
	}
	if got := containerRuntimeName(&namedContainerRuntime{command: "docker"}); got != "docker" {
		t.Fatalf("containerRuntimeName = %q, want %q", got, "docker")
	}
}
