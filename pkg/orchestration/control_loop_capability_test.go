package orchestration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// capabilityFleet is ordered so that a matcher which ignores, or only
// partially applies, RequiredCapabilities lands on a different agent than the
// correct one: the first idle agent advertises nothing, and each multi-
// capability holder comes after the agents holding only one of its entries.
func capabilityFleet() []*FleetNode {
	return []*FleetNode{
		{ID: "plain", Backend: "claude", Status: FleetNodeStatusIdle},
		{ID: "gpu", Backend: "claude", Status: FleetNodeStatusIdle, Capabilities: []string{"gpu"}},
		{ID: "docker", Backend: "claude", Status: FleetNodeStatusIdle, Capabilities: []string{"docker"}},
		{ID: "code", Backend: "claude", Status: FleetNodeStatusIdle, Capabilities: []string{"code"}},
		{ID: "docker-code", Backend: "claude", Status: FleetNodeStatusIdle, Capabilities: []string{"docker", "code"}},
	}
}

func TestMatchFleetNodeForTask_RequiredCapabilitiesHardMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		required    []string
		wantAgent   string
		wantMissing []string
	}{
		{name: "empty list", required: nil, wantAgent: "plain"},
		{name: "gpu", required: []string{"gpu"}, wantAgent: "gpu"},
		{name: "docker", required: []string{"docker"}, wantAgent: "docker"},
		{name: "code", required: []string{"code"}, wantAgent: "code"},
		{name: "unknown string", required: []string{"fpga"}, wantMissing: []string{"fpga"}},
		{name: "several at once", required: []string{"docker", "code"}, wantAgent: "docker-code"},
		{name: "several with no single holder", required: []string{"gpu", "docker"}, wantMissing: []string{"gpu", "docker"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			loop := NewControlLoop(nil, nil)
			item := &WorkItem{ID: "wi", RequiredCapabilities: tc.required}
			agentID, _, err := loop.matchFleetNodeForTask(item, capabilityFleet(), newTestBackendsWithPersistent())

			if tc.wantMissing == nil {
				require.NoError(t, err)
				require.Equal(t, tc.wantAgent, agentID)
				return
			}
			require.Empty(t, agentID)
			var noMatch *NoMatchingFleetNodeError
			require.ErrorAs(t, err, &noMatch)
			require.Equal(t, "capability", noMatch.Constraint)
			for _, capability := range tc.wantMissing {
				require.Contains(t, noMatch.Detail, capability)
			}
		})
	}
}

// A docker requirement used to be ignored and dispatched to whichever agent
// was idle (go-quicbaas#761).
func TestMatchFleetNodeForTask_DockerRequirementNeverDispatchesWithoutDocker(t *testing.T) {
	t.Parallel()

	loop := NewControlLoop(nil, nil)
	fleet := []*FleetNode{
		{ID: "bare", Backend: "claude", Status: FleetNodeStatusIdle, Capabilities: []string{"code"}},
	}
	item := &WorkItem{ID: "wi-docker", RequiredCapabilities: []string{"docker"}}

	agentID, _, err := loop.matchFleetNodeForTask(item, fleet, newTestBackendsWithPersistent())

	require.Empty(t, agentID)
	var noMatch *NoMatchingFleetNodeError
	require.ErrorAs(t, err, &noMatch)
	require.Equal(t, "capability", noMatch.Constraint)
	require.Contains(t, noMatch.Detail, "docker")
}

// persistent_sessions is a backend property: agents never advertise it, and
// it must not be matched against their capability strings.
func TestMatchFleetNodeForTask_PersistentSessionsStaysBackendScoped(t *testing.T) {
	t.Parallel()

	loop := NewControlLoop(nil, nil)
	fleet := []*FleetNode{
		{ID: "claude-bare", Backend: "claude", Status: FleetNodeStatusIdle},
		{ID: "ollama-docker", Backend: "ollama", Status: FleetNodeStatusIdle, Capabilities: []string{"docker"}},
		{ID: "claude-docker", Backend: "claude", Status: FleetNodeStatusRunning, Capabilities: []string{"docker"}},
	}
	item := &WorkItem{ID: "wi", RequiredCapabilities: []string{"persistent_sessions", "docker"}}

	agentID, _, err := loop.matchFleetNodeForTask(item, fleet, newTestBackendsWithPersistent())

	require.NoError(t, err)
	require.Equal(t, "claude-docker", agentID)
}

func TestMatchFleetNodeForTask_CapabilityFilterHoldsWithBackend(t *testing.T) {
	t.Parallel()

	loop := NewControlLoop(nil, nil)
	item := &WorkItem{ID: "wi", Backend: "claude", RequiredCapabilities: []string{"docker"}}

	agentID, _, err := loop.matchFleetNodeForTask(item, capabilityFleet(), nil)
	require.NoError(t, err)
	require.Equal(t, "docker", agentID)

	withoutDocker := []*FleetNode{{ID: "plain", Backend: "claude", Status: FleetNodeStatusIdle}}
	agentID, _, err = loop.matchFleetNodeForTask(item, withoutDocker, nil)
	require.Empty(t, agentID)
	var noMatch *NoMatchingFleetNodeError
	require.ErrorAs(t, err, &noMatch)
	require.Equal(t, "capability", noMatch.Constraint)
}

func TestMatchFleetNodeForTask_CapabilityFilterHoldsWithHostAffinity(t *testing.T) {
	t.Parallel()

	loop := NewControlLoop(nil, nil)
	item := &WorkItem{ID: "wi", HostAffinity: "host-a", HostAffinityStrict: true, RequiredCapabilities: []string{"docker"}}
	fleet := []*FleetNode{
		{ID: "a-plain", Host: "host-a", Status: FleetNodeStatusIdle},
		{ID: "b-docker", Host: "host-b", Status: FleetNodeStatusIdle, Capabilities: []string{"docker"}},
		{ID: "a-docker", Host: "host-a", Status: FleetNodeStatusIdle, Capabilities: []string{"docker"}},
	}

	agentID, _, err := loop.matchFleetNodeForTask(item, fleet, nil)
	require.NoError(t, err)
	require.Equal(t, "a-docker", agentID)

	agentID, _, err = loop.matchFleetNodeForTask(item, fleet[:2], nil)
	require.Empty(t, agentID)
	var noMatch *NoMatchingFleetNodeError
	require.ErrorAs(t, err, &noMatch)
}
