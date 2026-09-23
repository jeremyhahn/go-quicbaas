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
	"slices"
	"strconv"
	"testing"
)

// =============================================================================
// The privilege dimension of the sandbox (#1040)
// =============================================================================
//
// Every sandboxed agent container runs with --cap-drop ALL, the add-back set
// sudo and apk need, and a pids bound — and nothing that loosens the runtime's
// seccomp/AppArmor confinement. These tests pin the policy itself and then the
// exact `docker run` arguments on every path that builds a sandbox spec.

// defaultSecurityRunArgs is the contiguous run-argument sequence the default
// policy must produce: the pids bound, the five add-backs in sorted order, and
// the drop of everything else.
func defaultSecurityRunArgs() []string {
	return []string{
		"--pids-limit", strconv.FormatInt(DefaultSandboxPidsLimit, 10),
		"--cap-add", "CHOWN",
		"--cap-add", "DAC_OVERRIDE",
		"--cap-add", "FOWNER",
		"--cap-add", "SETGID",
		"--cap-add", "SETUID",
		"--cap-drop", "ALL",
	}
}

// interactiveRunArgs renders spec exactly as the docker runtime launches an
// interactive (CLI and JSON-RPC) session.
func interactiveRunArgs(t *testing.T, spec *ContainerSpec) []string {
	t.Helper()
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
	return rt.buildInteractiveRunArgs(spec, "quicbaas-acp-test", "")
}

// detachedRunArgs renders spec exactly as the docker runtime launches a
// detached container (the orchestrator's sandbox, the tool container).
func detachedRunArgs(t *testing.T, spec *ContainerSpec) []string {
	t.Helper()
	rt := &ExecContainerRuntime{command: RuntimeDocker, logger: newContainerTestLogger(t)}
	return rt.buildRunArgs(spec, "")
}

// requireSecureRunArgs fails unless args carry want as one contiguous run and
// carry nothing that widens the boundary again: no --privileged, and no
// --security-opt at all (which is how seccomp/AppArmor would be disabled, and
// how no-new-privileges would break setuid sudo).
func requireSecureRunArgs(t *testing.T, args, want []string) {
	t.Helper()
	found := false
	for i := 0; i+len(want) <= len(args); i++ {
		if slices.Equal(args[i:i+len(want)], want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("run args %q do not carry %q", args, want)
	}
	for _, forbidden := range []string{"--privileged", "--security-opt"} {
		if slices.Contains(args, forbidden) {
			t.Fatalf("run args %q carry %s", args, forbidden)
		}
	}
}

func TestSandboxSecurity_ZeroValueIsTheSecureDefault(t *testing.T) {
	spec := &ContainerSpec{Image: "worker:1"}
	if err := (SandboxSecurity{}).Apply(spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !slices.Equal(spec.CapDrop, []string{"ALL"}) {
		t.Errorf("CapDrop = %v, want [ALL]", spec.CapDrop)
	}
	if want := []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID"}; !slices.Equal(spec.CapAdd, want) {
		t.Errorf("CapAdd = %v, want %v", spec.CapAdd, want)
	}
	if spec.PidsLimit != DefaultSandboxPidsLimit {
		t.Errorf("PidsLimit = %d, want %d", spec.PidsLimit, DefaultSandboxPidsLimit)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("the secured spec does not validate: %v", err)
	}
}

// TestSandboxSecurity_OverridesWhatTheSpecCarried: a caller-built spec cannot
// keep --privileged, a wider capability grant or its own pids bound.
func TestSandboxSecurity_OverridesWhatTheSpecCarried(t *testing.T) {
	spec := &ContainerSpec{
		Image:      "worker:1",
		Privileged: true,
		CapAdd:     []string{"SYS_ADMIN", "NET_RAW"},
		CapDrop:    []string{"MKNOD"},
		PidsLimit:  1 << 20,
	}
	if err := (SandboxSecurity{}).Apply(spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if spec.Privileged {
		t.Error("Privileged survived the policy")
	}
	if slices.Contains(spec.CapAdd, "SYS_ADMIN") || slices.Contains(spec.CapAdd, "NET_RAW") {
		t.Errorf("CapAdd = %v kept the caller's grant", spec.CapAdd)
	}
	if !slices.Equal(spec.CapDrop, []string{"ALL"}) {
		t.Errorf("CapDrop = %v, want [ALL]", spec.CapDrop)
	}
	if spec.PidsLimit != DefaultSandboxPidsLimit {
		t.Errorf("PidsLimit = %d, want the policy's %d", spec.PidsLimit, DefaultSandboxPidsLimit)
	}
}

// TestSandboxSecurity_ConfiguredValuesReplaceTheDefaults: an operator's list
// replaces the default set (normalized, deduplicated, sorted) and an operator's
// pids limit replaces the default bound.
func TestSandboxSecurity_ConfiguredValuesReplaceTheDefaults(t *testing.T) {
	policy := SandboxSecurity{
		CapAdd:    []string{"cap_setuid", "SETGID", "CAP_SETUID", " kill "},
		PidsLimit: 512,
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	spec := &ContainerSpec{Image: "worker:1"}
	if err := policy.Apply(spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"KILL", "SETGID", "SETUID"}; !slices.Equal(spec.CapAdd, want) {
		t.Errorf("CapAdd = %v, want %v", spec.CapAdd, want)
	}
	if spec.PidsLimit != 512 {
		t.Errorf("PidsLimit = %d, want 512", spec.PidsLimit)
	}
}

func TestSandboxSecurity_RefusesAPolicyThatCannotBeApplied(t *testing.T) {
	cases := map[string]SandboxSecurity{
		"NegativePidsLimit": {PidsLimit: -1},
		"AddBackAll":        {CapAdd: []string{"all"}},
		"AddBackCapAll":     {CapAdd: []string{"CAP_ALL"}},
		"EmptyName":         {CapAdd: []string{"SETUID", "  "}},
		"Malformed":         {CapAdd: []string{"NET_RAW;rm"}},
	}
	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			if err := policy.Validate(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Validate() = %v, want ErrInvalidInput", err)
			}
			spec := &ContainerSpec{Image: "worker:1", CapAdd: []string{"UNTOUCHED"}}
			if err := policy.Apply(spec); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Apply() = %v, want ErrInvalidInput", err)
			}
			if !slices.Equal(spec.CapAdd, []string{"UNTOUCHED"}) || spec.CapDrop != nil {
				t.Fatalf("a refused Apply modified the spec: %+v", spec)
			}
		})
	}
}

func TestSandboxSecurity_ApplyRefusesANilSpec(t *testing.T) {
	if err := (SandboxSecurity{}).Apply(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Apply(nil) = %v, want ErrInvalidInput", err)
	}
}

func TestContainerSpec_RejectsANegativePidsLimit(t *testing.T) {
	spec := &ContainerSpec{Image: "worker:1", PidsLimit: -1}
	if err := spec.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Validate() = %v, want ErrInvalidInput", err)
	}
}

// TestRunArgs_PidsLimitIsEmittedOnlyWhenSet keeps every spec that never set a
// bound launching the command line it always did.
func TestRunArgs_PidsLimitIsEmittedOnlyWhenSet(t *testing.T) {
	unbounded := detachedRunArgs(t, &ContainerSpec{Image: "alpine"})
	if slices.Contains(unbounded, "--pids-limit") {
		t.Fatalf("an unbounded spec launched with --pids-limit: %q", unbounded)
	}
	bounded := detachedRunArgs(t, &ContainerSpec{Image: "alpine", PidsLimit: 256})
	i := slices.Index(bounded, "--pids-limit")
	if i < 0 || i+1 >= len(bounded) || bounded[i+1] != "256" {
		t.Fatalf("run args %q do not carry --pids-limit 256", bounded)
	}
}

// TestSandboxSpec_EveryTransportGetsThePolicy: sandboxSpec is the one builder
// the CLI, JSON-RPC and exec transports share, so the policy is asserted there
// once for the default and once for an operator's configuration.
func TestSandboxSpec_EveryTransportGetsThePolicy(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		backend := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})
		spec, err := backend.sandboxSpec(&SpawnRequest{WorkDir: "/srv/ws"}, "worker:1", []string{"claude"})
		if err != nil {
			t.Fatalf("sandboxSpec: %v", err)
		}
		requireSecureRunArgs(t, interactiveRunArgs(t, spec), defaultSecurityRunArgs())
	})
	t.Run("Configured", func(t *testing.T) {
		backend := NewACPBackend(ACPBackendConfig{
			Command: "claude", CLIMode: true,
			SandboxSecurity: SandboxSecurity{CapAdd: []string{"SETUID", "SETGID"}, PidsLimit: 1024},
		})
		spec, err := backend.sandboxSpec(&SpawnRequest{WorkDir: "/srv/ws"}, "worker:1", []string{"claude"})
		if err != nil {
			t.Fatalf("sandboxSpec: %v", err)
		}
		requireSecureRunArgs(t, interactiveRunArgs(t, spec), []string{
			"--pids-limit", "1024",
			"--cap-add", "SETGID",
			"--cap-add", "SETUID",
			"--cap-drop", "ALL",
		})
	})
	t.Run("InvalidPolicyRefusesTheSpawn", func(t *testing.T) {
		backend := NewACPBackend(ACPBackendConfig{
			Command: "claude", CLIMode: true,
			SandboxSecurity: SandboxSecurity{CapAdd: []string{"ALL"}},
		})
		if _, err := backend.sandboxSpec(&SpawnRequest{}, "worker:1", []string{"claude"}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("sandboxSpec() = %v, want ErrInvalidInput", err)
		}
	})
}

// TestSpawnContainerCLI_RunArgsDropEveryCapability drives the real CLI spawn —
// the Software Factory worker's path — and pins the arguments it launches with.
func TestSpawnContainerCLI_RunArgsDropEveryCapability(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "quicbaas-agent-sec"}
	backend := newContainerCLIBackend(t, rt)

	sess, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
	if err != nil {
		t.Fatalf("required CLI spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	requireSecureRunArgs(t, interactiveRunArgs(t, rt.lastSpec()), defaultSecurityRunArgs())
}

// TestSpawnContainerJSONRPC_RunArgsDropEveryCapability is the same pin on the
// ACP JSON-RPC container path.
func TestSpawnContainerJSONRPC_RunArgsDropEveryCapability(t *testing.T) {
	rt := &specCapturingRuntime{inner: &ctxBoundContainerRuntime{t: t}}
	backend := NewACPBackend(ACPBackendConfig{
		Command:      "opencode",
		Args:         []string{"acp"},
		DefaultModel: "test-model",
	})
	backend.SetContainerRuntime(rt)

	sess, err := backend.Spawn(t.Context(), &SpawnRequest{
		ProjectID:      "p",
		WorkDir:        "/tmp",
		Model:          "test-model",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "img:latest",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	specs := rt.captured()
	if len(specs) != 1 {
		t.Fatalf("launches = %d, want 1", len(specs))
	}
	requireSecureRunArgs(t, interactiveRunArgs(t, specs[0]), defaultSecurityRunArgs())
}

// TestSpawnSession_TheOrchestratorsSandboxGetsThePolicy covers the detached
// container the orchestrator launches for a backend that does not
// containerize itself — including a caller-supplied ContainerConfig that asked
// for more.
func TestSpawnSession_TheOrchestratorsSandboxGetsThePolicy(t *testing.T) {
	t.Run("BuiltFromTheRequest", func(t *testing.T) {
		h := newSpawnMountHarness(t)
		if _, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
			Backend:        "mock",
			Sandboxed:      true,
			ContainerImage: "ghcr.io/example/agent:v1",
			WorkDir:        h.workspacePath(),
		}); err != nil {
			t.Fatalf("SpawnSession: %v", err)
		}
		requireSecureRunArgs(t, detachedRunArgs(t, h.runtime.runCalls[0].Spec), defaultSecurityRunArgs())
	})
	t.Run("CallerContainerConfigCannotWidenIt", func(t *testing.T) {
		h := newSpawnMountHarness(t)
		if _, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
			Backend:   "mock",
			Sandboxed: true,
			WorkDir:   h.workspacePath(),
			ContainerConfig: &ContainerSpec{
				Image:      "ghcr.io/example/agent:v1",
				Privileged: true,
				CapAdd:     []string{"SYS_ADMIN"},
			},
		}); err != nil {
			t.Fatalf("SpawnSession: %v", err)
		}
		requireSecureRunArgs(t, detachedRunArgs(t, h.runtime.runCalls[0].Spec), defaultSecurityRunArgs())
	})
}

// TestOpenVINOBackend_TheToolContainerGetsThePolicy covers the triage tool
// container, which agents exec into and which carries the same sudo.
func TestOpenVINOBackend_TheToolContainerGetsThePolicy(t *testing.T) {
	runtime := &mockContainerRuntime{available: true, runID: "ctr-sec"}
	backend := NewOpenVINOBackend(OpenVINOBackendConfig{
		BaseURL:         "http://gpu-host.invalid:9000",
		DefaultModel:    "qwen3-8b",
		SandboxSecurity: SandboxSecurity{PidsLimit: 2048},
	})
	backend.SetContainerRuntime(runtime)

	if _, err := backend.Spawn(context.Background(), &SpawnRequest{
		Model:          "qwen3-8b",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "worker:test",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(runtime.runCalls) != 1 {
		t.Fatalf("container runs = %d, want exactly one", len(runtime.runCalls))
	}
	want := defaultSecurityRunArgs()
	want[1] = "2048"
	requireSecureRunArgs(t, detachedRunArgs(t, runtime.runCalls[0].Spec), want)
}

// TestSpawnSession_TheDetachedSandboxKeepsTheBackendsConfiguredPolicy: a
// backend that does not launch its own container still has its operator
// policy honoured on the orchestrator's detached path — not replaced by the
// default. A local OpenVINO backend is exactly that case.
func TestSpawnSession_TheDetachedSandboxKeepsTheBackendsConfiguredPolicy(t *testing.T) {
	configured := SandboxSecurity{CapAdd: []string{"SETUID", "SETGID"}, PidsLimit: 128}
	want := []string{
		"--pids-limit", "128",
		"--cap-add", "SETGID",
		"--cap-add", "SETUID",
		"--cap-drop", "ALL",
	}

	cases := map[string]AgentBackend{
		"OpenVINO": NewOpenVINOBackend(OpenVINOBackendConfig{
			BaseURL:         "http://gpu-host.invalid:9000",
			DefaultModel:    "qwen3-8b",
			SandboxSecurity: configured,
		}),
		// An ACP backend with no runtime of its own is also launched by the
		// orchestrator.
		"ACPWithoutRuntime": NewACPBackend(ACPBackendConfig{
			Command:         "opencode",
			Args:            []string{"acp"},
			DefaultModel:    "test-model",
			SandboxSecurity: configured,
		}),
	}
	for name, backend := range cases {
		t.Run(name, func(t *testing.T) {
			if got := sandboxSecurityFor(backend); got.PidsLimit != 128 {
				t.Fatalf("sandboxSecurityFor = %+v, want the configured policy", got)
			}

			orchestratorRuntime := &mockContainerRuntime{available: true, runID: "detached"}
			svc, err := NewAgentService(NewInMemoryAgentStore(),
				map[string]AgentBackend{"b": backend}, &agentTestLogger{})
			if err != nil {
				t.Fatalf("NewAgentService: %v", err)
			}
			impl, ok := svc.(*agentServiceImpl)
			if !ok {
				t.Fatal("agent service is not the package implementation")
			}
			impl.SetContainerRuntime(orchestratorRuntime)

			if _, err := svc.SpawnSession(t.Context(), &SpawnSessionRequest{
				Backend:        "b",
				Model:          "qwen3-8b",
				Sandboxed:      true,
				ContainerImage: "worker:test",
			}); err != nil {
				t.Fatalf("SpawnSession: %v", err)
			}
			if len(orchestratorRuntime.runCalls) != 1 {
				t.Fatalf("detached launches = %d, want 1", len(orchestratorRuntime.runCalls))
			}
			requireSecureRunArgs(t, detachedRunArgs(t, orchestratorRuntime.runCalls[0].Spec), want)
		})
	}
}
