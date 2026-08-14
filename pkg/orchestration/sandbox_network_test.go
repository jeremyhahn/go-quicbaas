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
	"strings"
	"testing"
	"time"
)

func TestParseSandboxNetwork(t *testing.T) {
	t.Parallel()

	t.Run("EmptyResolvesToTheIsolatedDefault", func(t *testing.T) {
		t.Parallel()
		got, err := ParseSandboxNetwork("")
		if err != nil {
			t.Fatalf("ParseSandboxNetwork(\"\") error = %v", err)
		}
		if got != SandboxNetworkBridge {
			t.Errorf("default network = %q, want %q", got, SandboxNetworkBridge)
		}
		if !got.IsolatesHost() {
			t.Error("the default network must isolate the host network namespace")
		}
	})

	t.Run("NamedModesAndCustomNetworks", func(t *testing.T) {
		t.Parallel()
		cases := map[string]SandboxNetwork{
			"bridge":            SandboxNetworkBridge,
			"none":              SandboxNetworkNone,
			"host":              SandboxNetworkHost,
			"  bridge  ":        SandboxNetworkBridge,
			"quicbaas-agents":   SandboxNetwork("quicbaas-agents"),
			"agents_net.v1":     SandboxNetwork("agents_net.v1"),
			"0123456789abcdef0": SandboxNetwork("0123456789abcdef0"),
		}
		for input, want := range cases {
			got, err := ParseSandboxNetwork(input)
			if err != nil {
				t.Fatalf("ParseSandboxNetwork(%q) error = %v", input, err)
			}
			if got != want {
				t.Errorf("ParseSandboxNetwork(%q) = %q, want %q", input, got, want)
			}
		}
	})

	t.Run("InvalidNamesAreRejected", func(t *testing.T) {
		t.Parallel()
		for _, input := range []string{
			"-leading-dash",
			"_leading-underscore",
			"has space",
			"--privileged",
			"container:other",
			"net;rm -rf /",
			"net\n--privileged",
		} {
			got, err := ParseSandboxNetwork(input)
			var validErr *InputValidationError
			if !errors.As(err, &validErr) {
				t.Fatalf("ParseSandboxNetwork(%q): expected InputValidationError, got %T: %v", input, err, err)
			}
			// A rejected value must still resolve to the safe default, never
			// to the caller's string.
			if got != SandboxNetworkDefault {
				t.Errorf("ParseSandboxNetwork(%q) = %q on error, want %q", input, got, SandboxNetworkDefault)
			}
		}
	})

	t.Run("OnlyHostSharesTheHostNamespace", func(t *testing.T) {
		t.Parallel()
		if SandboxNetworkHost.IsolatesHost() {
			t.Error("host networking must not report host isolation")
		}
		for _, n := range []SandboxNetwork{SandboxNetworkBridge, SandboxNetworkNone, SandboxNetwork("custom")} {
			if !n.IsolatesHost() {
				t.Errorf("%q must report host isolation", n)
			}
		}
	})
}

// TestACPBackend_SandboxNetwork_DefaultsToIsolated is the regression guard for
// the audit finding: every sandboxed ACP session used to launch with
// --network host, which put the container in the host's network namespace and
// within reach of every loopback-bound service on the machine.
func TestACPBackend_SandboxNetwork_DefaultsToIsolated(t *testing.T) {
	spec := spawnSandboxSpec(t, ACPBackendConfig{Command: "opencode"})

	if spec.Network != SandboxNetworkBridge.String() {
		t.Fatalf("sandboxed session network = %q, want %q", spec.Network, SandboxNetworkBridge)
	}
	if !SandboxNetwork(spec.Network).IsolatesHost() {
		t.Fatalf("sandboxed session shares the host network namespace: %q", spec.Network)
	}
}

// TestACPBackend_SandboxNetwork_HostRequiresOperatorConfig proves the escape
// hatch exists and is reachable ONLY through backend configuration, and that
// taking it is announced in the log.
func TestACPBackend_SandboxNetwork_HostRequiresOperatorConfig(t *testing.T) {
	logger := newRecordingLogger()
	spec := spawnSandboxSpecWithLogger(t, ACPBackendConfig{
		Command:        "opencode",
		SandboxNetwork: SandboxNetworkHost,
	}, logger)

	if spec.Network != SandboxNetworkHost.String() {
		t.Fatalf("configured network = %q, want %q", spec.Network, SandboxNetworkHost)
	}
	if warn := logger.atLevel("WARN"); !strings.Contains(warn, "host network namespace") {
		t.Errorf("expected a warning about host networking, got:\n%s", warn)
	}
}

// TestACPBackend_SandboxNetwork_CustomNetworkIsHonored covers the operator who
// puts agents on a dedicated user-defined network.
func TestACPBackend_SandboxNetwork_CustomNetworkIsHonored(t *testing.T) {
	spec := spawnSandboxSpec(t, ACPBackendConfig{
		Command:        "opencode",
		SandboxNetwork: SandboxNetwork("quicbaas-agents"),
	})

	if spec.Network != "quicbaas-agents" {
		t.Fatalf("network = %q, want quicbaas-agents", spec.Network)
	}
}

// TestACPBackend_SandboxNetwork_InvalidConfigFailsSafe proves a malformed
// configuration value falls back to the isolated default instead of reaching
// the runtime command line, and says so.
func TestACPBackend_SandboxNetwork_InvalidConfigFailsSafe(t *testing.T) {
	logger := newRecordingLogger()
	spec := spawnSandboxSpecWithLogger(t, ACPBackendConfig{
		Command:        "opencode",
		SandboxNetwork: SandboxNetwork("--privileged"),
	}, logger)

	if spec.Network != SandboxNetworkDefault.String() {
		t.Fatalf("network = %q, want the safe default %q", spec.Network, SandboxNetworkDefault)
	}
	if warn := logger.atLevel("WARN"); !strings.Contains(warn, "invalid sandbox_network") {
		t.Errorf("expected an invalid-network warning, got:\n%s", warn)
	}
}

// TestACPBackend_SandboxNetwork_RequestCannotSelectHost proves the network is
// not caller-controlled: a SpawnRequest has no network field, so no combination
// of request values can move a session onto the host namespace.
func TestACPBackend_SandboxNetwork_RequestCannotSelectHost(t *testing.T) {
	runtime := &specCapturingContainerRuntime{t: t}
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode"})
	backend.SetContainerRuntime(runtime)

	ctx, cancel := context.WithTimeout(ContextWithTenant(context.Background(), "system"), 2*time.Second)
	defer cancel()
	sess, err := backend.Spawn(ctx, &SpawnRequest{
		WorkDir:        "/tmp",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "opencode:test",
		// Everything a caller can express about the container.
		Env:         map[string]string{"NETWORK": "host", "QUICBAAS_NETWORK": "host"},
		ExtraArgs:   []string{"--network", "host"},
		ExtraMounts: []ContainerMount{{Source: "/tmp", Target: "/mnt/tmp"}},
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer func() { _ = backend.Close(context.Background(), sess.ID) }()

	if !SandboxNetwork(runtime.gotSpec.Network).IsolatesHost() {
		t.Fatalf("a caller moved the session onto the host namespace: %q", runtime.gotSpec.Network)
	}
}

// spawnSandboxSpec spawns one sandboxed session against a capturing runtime and
// returns the ContainerSpec the backend built.
func spawnSandboxSpec(t *testing.T, cfg ACPBackendConfig) *ContainerSpec {
	t.Helper()
	return spawnSandboxSpecWithLogger(t, cfg, newRecordingLogger())
}

// spawnSandboxSpecWithLogger is spawnSandboxSpec with a caller-supplied logger
// so a test can assert on the advisories the spawn emitted.
func spawnSandboxSpecWithLogger(t *testing.T, cfg ACPBackendConfig, logger *recordingLogger) *ContainerSpec {
	t.Helper()

	runtime := &specCapturingContainerRuntime{t: t}
	backend := NewACPBackend(cfg)
	backend.SetContainerRuntime(runtime)
	backend.SetLogger(logger)

	ctx, cancel := context.WithTimeout(ContextWithTenant(context.Background(), "system"), 2*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{
		WorkDir:        "/tmp",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "opencode:test",
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	if runtime.gotSpec == nil {
		t.Fatal("the runtime never received a ContainerSpec")
	}
	return runtime.gotSpec
}
