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

package server

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Helpers
// =============================================================================

// newPool returns a remoteBackendPool with a no-op registry and logger suitable
// for unit testing pickDaemon directly without any network or storage.
func newPool() *remoteBackendPool {
	return &remoteBackendPool{
		entries: map[string]*pooledDaemon{},
	}
}

// daemonReg constructs a minimal daemon-kind FleetNode for tests.
func daemonReg(name, host, backend string, caps ...string) *orchestration.FleetNode {
	return &orchestration.FleetNode{
		ID:           name + "-id",
		Name:         name,
		Host:         host,
		Backend:      backend,
		Kind:         orchestration.FleetNodeKindDaemon,
		Status:       orchestration.FleetNodeStatusIdle,
		Capabilities: caps,
	}
}

// =============================================================================
// pickDaemon tests
// =============================================================================

// TestPickAgent_GpuRequired_FiltersOutCpuOnlyAgents verifies that when
// gpuRequired=true, only agents advertising "gpu" in Capabilities are
// considered, and CPU-only agents are skipped.
func TestPickAgent_GpuRequired_FiltersOutCpuOnlyAgents(t *testing.T) {
	t.Parallel()
	pool := newPool()

	cpuAgent := daemonReg("cpu-host", "192.0.2.10:9090", "ollama", "ollama")
	gpuAgent := daemonReg("gpu-host", "192.0.2.20:9090", "ollama", "ollama", "gpu")

	regs := []*orchestration.FleetNode{cpuAgent, gpuAgent}

	got, constraint := pool.pickDaemon(regs, "ollama", "", true, "")
	if got == nil {
		t.Fatalf("expected a GPU agent to be selected, got nil (failedConstraint=%q)", constraint)
	}
	if got.Name != "gpu-host" {
		t.Errorf("expected gpu-host to be selected, got %q", got.Name)
	}
	if constraint != "" {
		t.Errorf("expected no failed constraint, got %q", constraint)
	}
}

// TestPickAgent_GpuRequired_NoCpuAgentsReturnsGpuConstraint verifies that
// when no GPU-capable agent exists the failed constraint is reported as "gpu".
func TestPickAgent_GpuRequired_NoCpuAgentsReturnsGpuConstraint(t *testing.T) {
	t.Parallel()
	pool := newPool()

	cpuAgent := daemonReg("cpu-host", "192.0.2.10:9090", "ollama", "ollama")
	regs := []*orchestration.FleetNode{cpuAgent}

	got, constraint := pool.pickDaemon(regs, "ollama", "", true, "")
	if got != nil {
		t.Fatalf("expected nil when no GPU agents available, got %+v", got)
	}
	if constraint != "gpu" {
		t.Errorf("expected failed constraint to be %q, got %q", "gpu", constraint)
	}
}

// TestPickAgent_HostAffinity_PrefersExactMatch verifies that when a host
// affinity is specified, only agents whose Name or Host matches are selected.
func TestPickAgent_HostAffinity_PrefersExactMatch(t *testing.T) {
	t.Parallel()
	pool := newPool()

	agentA := daemonReg("gpu-host", "192.0.2.10:9090", "ollama", "ollama")
	agentB := daemonReg("other", "192.0.2.20:9090", "ollama", "ollama")
	regs := []*orchestration.FleetNode{agentB, agentA} // B first to ensure affinity wins

	// Match by Name.
	got, constraint := pool.pickDaemon(regs, "ollama", "gpu-host", false, "")
	if got == nil {
		t.Fatalf("expected gpu-host agent to be selected, got nil (constraint=%q)", constraint)
	}
	if got.Name != "gpu-host" {
		t.Errorf("expected gpu-host, got %q", got.Name)
	}
}

// TestPickAgent_HostAffinity_PrefixMatch verifies that host affinity matches
// when the affinity is a hostname prefix of the registered host:port.
func TestPickAgent_HostAffinity_PrefixMatch(t *testing.T) {
	t.Parallel()
	pool := newPool()

	agent := daemonReg("node1", "192.0.2.50:9090", "ollama", "ollama")
	regs := []*orchestration.FleetNode{agent}

	got, constraint := pool.pickDaemon(regs, "ollama", "192.0.2.50", false, "")
	if got == nil {
		t.Fatalf("expected agent to be matched by host prefix, got nil (constraint=%q)", constraint)
	}
	if constraint != "" {
		t.Errorf("expected no failed constraint, got %q", constraint)
	}
}

// TestPickAgent_NoMatchingAgent_ReturnsTypedError verifies that when no agent
// satisfies the host affinity filter, the failed constraint reports
// "host_affinity" and buildNoMatchError produces a NoMatchingFleetNodeError that
// wraps ErrNoMatchingFleetNode.
func TestPickAgent_NoMatchingAgent_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	pool := newPool()

	agent := daemonReg("existing", "10.0.0.1:9090", "ollama", "ollama")
	regs := []*orchestration.FleetNode{agent}

	got, constraint := pool.pickDaemon(regs, "ollama", "nonexistent-host", false, "")
	if got != nil {
		t.Fatalf("expected nil for non-matching host affinity, got %+v", got)
	}
	if constraint != "host_affinity" {
		t.Errorf("expected failed constraint %q, got %q", "host_affinity", constraint)
	}

	// Verify buildNoMatchError produces a correctly wrapped error.
	err := buildNoMatchError("ollama", "nonexistent-host", false, constraint)
	if !errors.Is(err, orchestration.ErrNoMatchingFleetNode) {
		t.Errorf("expected errors.Is to find ErrNoMatchingFleetNode, got %v", err)
	}
	if err.Constraint != "host_affinity" {
		t.Errorf("expected Constraint=%q, got %q", "host_affinity", err.Constraint)
	}
}

// TestPickAgent_GpuRequiredAndHostAffinity_CombineCorrectly verifies that both
// filters apply simultaneously: an agent must match host affinity AND have GPU.
func TestPickAgent_GpuRequiredAndHostAffinity_CombineCorrectly(t *testing.T) {
	t.Parallel()
	pool := newPool()

	// Right host, no GPU.
	rightHostNoCPU := daemonReg("target", "10.0.0.1:9090", "ollama", "ollama")
	// Right host, has GPU — should win.
	rightHostGPU := daemonReg("target-gpu", "10.0.0.1:9091", "ollama", "ollama", "gpu")
	// Different host, has GPU — should not be selected.
	wrongHostGPU := daemonReg("other-gpu", "10.0.0.2:9090", "ollama", "ollama", "gpu")

	regs := []*orchestration.FleetNode{rightHostNoCPU, wrongHostGPU, rightHostGPU}

	got, constraint := pool.pickDaemon(regs, "ollama", "target-gpu", true, "")
	if got == nil {
		t.Fatalf("expected target-gpu agent to be selected, got nil (constraint=%q)", constraint)
	}
	if got.Name != "target-gpu" {
		t.Errorf("expected target-gpu, got %q", got.Name)
	}
	if constraint != "" {
		t.Errorf("expected no failed constraint, got %q", constraint)
	}
}

// TestPickAgent_BackendFilter_SkipsNonDaemonKind verifies that non-daemon
// registrations are always skipped by pickDaemon regardless of other fields.
func TestPickAgent_BackendFilter_SkipsNonDaemonKind(t *testing.T) {
	t.Parallel()
	pool := newPool()

	external := &orchestration.FleetNode{
		ID:           "ext-1",
		Name:         "external-agent",
		Host:         "10.0.0.5:9090",
		Backend:      "ollama",
		Kind:         orchestration.FleetNodeKindExternal,
		Status:       orchestration.FleetNodeStatusIdle,
		Capabilities: []string{"ollama", "gpu"},
	}
	regs := []*orchestration.FleetNode{external}

	got, constraint := pool.pickDaemon(regs, "ollama", "", false, "")
	if got != nil {
		t.Errorf("expected non-daemon agent to be skipped, got %+v", got)
	}
	// No daemon-kind agents pass the first filter.
	if constraint != "backend" {
		t.Errorf("expected constraint %q, got %q", "backend", constraint)
	}
}

// TestBuildNoMatchError_IncludesAllConstraints verifies the error message
// includes backend, host affinity, and gpu information when all are set.
func TestBuildNoMatchError_IncludesAllConstraints(t *testing.T) {
	t.Parallel()
	err := buildNoMatchError("ollama", "my-host", true, "gpu")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	msg := err.Error()
	for _, want := range []string{"ollama", "my-host", "gpu"} {
		if len(msg) == 0 {
			t.Fatalf("empty error message")
		}
		found := false
		for i := 0; i <= len(msg)-len(want); i++ {
			if msg[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error message to contain %q, got: %s", want, msg)
		}
	}
}
