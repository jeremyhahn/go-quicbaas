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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Helpers
// =============================================================================

// enrolledAgent builds the registration the mTLS register handler produces for
// a daemon: a record owned by the certificate CommonName it authenticated as.
func enrolledAgent(tenantID, nodeID, principal, host string, backends ...string) *FleetNode {
	primary := ""
	if len(backends) > 0 {
		primary = backends[0]
	}
	return &FleetNode{
		ID:           nodeID,
		TenantID:     tenantID,
		Name:         nodeID,
		Host:         host,
		Backend:      primary,
		Capabilities: backends,
		NodeType:     "agent-runtime",
		Kind:         FleetNodeKindDaemon,
		Status:       FleetNodeStatusRegistered,
		Metadata: map[string]string{
			FleetNodePrincipalMetadataKey: principal,
		},
	}
}

// =============================================================================
// RefreshFleetNode — the re-registration path (go-quicbaas#646)
// =============================================================================

// TestRefreshAgent_ReRegistrationConvergesTheRecord is the burn-in failure at
// its source.
//
// A daemon enrolled under the pre-#681 transport vocabulary recorded
// Backend="acp"; restarted on a new address and serving claude, codex and
// opencode, it registered again, the guarded create answered "already exists",
// and the record kept "acp" forever. The remote resolver's
// ListFleetNodes(Backend:"claude") therefore matched nothing, the spawn fell
// through to the central's in-process backend and the sdlc runner refused it
// for having no container.
func TestRefreshAgent_ReRegistrationConvergesTheRecord(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()

	first := enrolledAgent("automatethethings", "agent-att-agent-04",
		"agent-agent-04", "10.0.0.4:9090", "acp")
	require.NoError(t, registry.RegisterFleetNode(ctx, first))
	registeredAt := first.RegisteredAt
	require.NotZero(t, registeredAt)

	// The daemon comes back on a new address serving what it actually runs.
	again := enrolledAgent("automatethethings", "agent-att-agent-04",
		"agent-agent-04", "10.0.0.9:9091", "claude", "codex", "opencode")
	again.Metadata["oauth2_client_id"] = "client-2"
	require.ErrorIs(t, registry.RegisterFleetNode(ctx, again), ErrFleetNodeAlreadyExists,
		"registration must still never overwrite; the refresh is the second verb")
	require.NoError(t, registry.RefreshFleetNode(ctx, again))

	stored, err := registry.GetFleetNode(ctx, "automatethethings", "agent-att-agent-04")
	require.NoError(t, err)

	assert.Equal(t, "10.0.0.9:9091", stored.Host, "the address a dispatcher dials must be the current one")
	assert.Equal(t, "claude", stored.Backend)
	assert.Equal(t, []string{"claude", "codex", "opencode"}, stored.Capabilities)
	assert.Equal(t, "client-2", stored.Metadata["oauth2_client_id"],
		"the client an operator revokes through must be the one the daemon now uses")
	assert.Equal(t, FleetNodeStatusRegistered, stored.Status)

	// Identity and history survive: the record is the same record.
	assert.Equal(t, "agent-att-agent-04", stored.ID)
	assert.Equal(t, registeredAt, stored.RegisteredAt, "a re-registration is not a new join")
	assert.Greater(t, stored.Version, first.Version)

	// The property the dispatcher actually depends on.
	matched, err := registry.ListFleetNodes(ctx, "automatethethings", &FleetNodeFilter{Backend: "claude"})
	require.NoError(t, err)
	require.Len(t, matched, 1, "a backend the daemon serves must find it")

	stale, err := registry.ListFleetNodes(ctx, "automatethethings", &FleetNodeFilter{Backend: "acp"})
	require.NoError(t, err)
	assert.Empty(t, stale, "the transport name it no longer advertises must find nothing")
}

// TestRefreshAgent_MetadataIsReplacedNotMerged pins the wholesale write: a
// merge would keep a key nothing writes any more forever, which is the
// staleness the refresh exists to end.
func TestRefreshAgent_MetadataIsReplacedNotMerged(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()

	first := enrolledAgent("t", "agent-1", "cn-1", "host-1", "acp")
	first.Metadata["mtls_client_id"] = "gone-in-the-next-build"
	require.NoError(t, registry.RegisterFleetNode(ctx, first))

	again := enrolledAgent("t", "agent-1", "cn-1", "host-2", "claude")
	require.NoError(t, registry.RefreshFleetNode(ctx, again))

	stored, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)
	assert.NotContains(t, stored.Metadata, "mtls_client_id")
}

// TestRefreshAgent_RefusesADifferentPrincipal is the identity-takeover guard.
//
// The agent ID is a SANITISED derivation of the certificate CommonName
// (iam.SanitizeAppName folds space, slash, colon and backslash to "-"), so two
// distinct certificate subjects can address one record. Refreshing in place
// would let the second rewrite the first's host and backends; it is refused,
// and nothing is written.
func TestRefreshAgent_RefusesADifferentPrincipal(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()

	owner := enrolledAgent("t", "agent-a-b", "a.b", "10.0.0.1:9090", "claude")
	require.NoError(t, registry.RegisterFleetNode(ctx, owner))

	// "a b" and "a.b" both sanitise to the same agent id.
	intruder := enrolledAgent("t", "agent-a-b", "a b", "10.6.6.6:9090", "codex")
	err := registry.RefreshFleetNode(ctx, intruder)
	require.ErrorIs(t, err, ErrFleetNodePrincipalMismatch)

	var mismatch *FleetNodePrincipalMismatchError
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, "a.b", mismatch.Recorded)
	assert.Equal(t, "a b", mismatch.Presented)

	stored, getErr := registry.GetFleetNode(ctx, "t", "agent-a-b")
	require.NoError(t, getErr)
	assert.Equal(t, "10.0.0.1:9090", stored.Host, "the owner's record must be untouched")
	assert.Equal(t, "claude", stored.Backend)
	assert.Equal(t, owner.Version, stored.Version, "a refused refresh writes nothing at all")
}

// TestRefreshAgent_RefusesARecordThatNamesNoPrincipal keeps an
// operator-created registration out of reach of every certificate: a record
// bound to nobody is owned by nobody, and fail-closed is the only safe answer.
func TestRefreshAgent_RefusesARecordThatNamesNoPrincipal(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()

	operatorMade := &FleetNode{
		ID: "agent-1", TenantID: "t", Name: "agent-1",
		Host: "10.0.0.1:9090", Backend: "ollama",
	}
	require.NoError(t, registry.RegisterFleetNode(ctx, operatorMade))

	err := registry.RefreshFleetNode(ctx, enrolledAgent("t", "agent-1", "cn-1", "10.6.6.6:9090", "claude"))
	require.ErrorIs(t, err, ErrFleetNodePrincipalMismatch)

	stored, getErr := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, getErr)
	assert.Equal(t, "ollama", stored.Backend)
}

// TestRefreshAgent_RefusesADisabledAgent keeps DisableFleetNode meaningful: a
// daemon that keeps restarting must not be able to re-enroll its way back into
// service, the same rule #674 established for the identity enrollment.
func TestRefreshAgent_RefusesADisabledAgent(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()

	agent := enrolledAgent("t", "agent-1", "cn-1", "10.0.0.1:9090", "claude")
	require.NoError(t, registry.RegisterFleetNode(ctx, agent))

	agent.Status = FleetNodeStatusDisabled
	agent.DisabledAt = 42
	require.NoError(t, registry.UpdateFleetNode(ctx, agent))

	err := registry.RefreshFleetNode(ctx, enrolledAgent("t", "agent-1", "cn-1", "10.0.0.2:9090", "codex"))
	require.ErrorIs(t, err, ErrFleetNodeDisabled)

	stored, getErr := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, getErr)
	assert.Equal(t, FleetNodeStatusDisabled, stored.Status)
	assert.Equal(t, "claude", stored.Backend)
}

// TestRefreshAgent_BadInput covers every refusal that precedes the store.
func TestRefreshAgent_BadInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := map[string]struct {
		registry FleetRegistry
		agent    *FleetNode
		sentinel error
	}{
		"nil record": {
			registry: NewInMemoryFleetRegistry(),
			agent:    nil,
			sentinel: ErrFleetRegistryInvalidInput,
		},
		"no store": {
			registry: NewFleetRegistry(nil),
			agent:    enrolledAgent("t", "agent-1", "cn-1", "h", "claude"),
			sentinel: ErrFleetRegistryRequired,
		},
		"unusable agent id": {
			registry: NewInMemoryFleetRegistry(),
			agent:    enrolledAgent("t", "", "cn-1", "h", "claude"),
			sentinel: ErrFleetRegistryInvalidInput,
		},
		"agent that was never registered": {
			registry: NewInMemoryFleetRegistry(),
			agent:    enrolledAgent("t", "ghost", "cn-1", "h", "claude"),
			sentinel: ErrFleetNodeNotFound,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, tt.registry.RefreshFleetNode(ctx, tt.agent), tt.sentinel)
		})
	}
}

// TestRefreshAgent_CancelledContext refuses before touching the store.
func TestRefreshAgent_CancelledContext(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := registry.RefreshFleetNode(ctx, enrolledAgent("t", "agent-1", "cn-1", "h", "claude"))
	require.ErrorIs(t, err, context.Canceled)
}

// TestRefreshAgent_Audits pins the trail an auditor reviews: a re-registration
// and its refusals are attributable, and under their own action so an operator
// change is never mistaken for an agent re-enrolling itself.
func TestRefreshAgent_Audits(t *testing.T) {
	t.Parallel()

	recorder := &agentAuditRecorder{}
	registry := NewFleetRegistry(NewMemoryTaskKV(), WithStoreAuditor(recorder))
	ctx := context.Background()

	require.NoError(t, registry.RegisterFleetNode(ctx, enrolledAgent("t", "agent-1", "cn-1", "h1", "acp")))
	require.NoError(t, registry.RefreshFleetNode(ctx, enrolledAgent("t", "agent-1", "cn-1", "h2", "claude")))
	require.Error(t, registry.RefreshFleetNode(ctx, enrolledAgent("t", "agent-1", "intruder", "h3", "codex")))

	var succeeded, refused int
	for _, event := range recorder.snapshot() {
		if event.Action != AuditActionReregister {
			continue
		}
		assert.Equal(t, AuditResourceFleetNode, event.Resource)
		assert.Equal(t, "agent-1", event.ResourceID)
		if event.Success {
			succeeded++
		} else {
			refused++
		}
	}
	assert.Equal(t, 1, succeeded)
	assert.Equal(t, 1, refused)
}

// =============================================================================
// Heartbeat reconciliation (go-quicbaas#699)
// =============================================================================

// TestHeartbeat_ReconcilesTheBackendAxis is the correction path: a daemon
// restarted with a different --backends converges without a re-registration.
func TestHeartbeat_ReconcilesTheBackendAxis(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()

	agent := enrolledAgent("t", "agent-1", "cn-1", "10.0.0.1:9090", "acp", FleetNodeTraitGPU)
	require.NoError(t, registry.RegisterFleetNode(ctx, agent))

	// Runtime.Backends() names backends only; the gpu trait rides on the
	// register payload alone.
	observed := &FleetNodeObservation{Backends: []string{"claude", "codex", "opencode"}}
	require.NoError(t, registry.Heartbeat(ctx, "t", "agent-1", observed))

	stored, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "claude", stored.Backend)
	assert.Equal(t, []string{"claude", "codex", "opencode", FleetNodeTraitGPU}, stored.Capabilities,
		"the gpu trait must survive a reconciliation that rewrites the backend axis")

	matched, err := registry.ListFleetNodes(ctx, "t", &FleetNodeFilter{Backend: "opencode"})
	require.NoError(t, err)
	assert.Len(t, matched, 1)
}

// TestHeartbeat_ReconcilesBothWays pins that a SHRINKING served set is applied
// too — a daemon restarted with fewer backends must stop matching the ones it
// dropped, or dispatch routes work it cannot run.
func TestHeartbeat_ReconcilesBothWays(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()

	require.NoError(t, registry.RegisterFleetNode(ctx,
		enrolledAgent("t", "agent-1", "cn-1", "h", "claude", "codex", "opencode")))

	require.NoError(t, registry.Heartbeat(ctx, "t", "agent-1", &FleetNodeObservation{Backends: []string{"ollama"}}))

	stored, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "ollama", stored.Backend)
	assert.Equal(t, []string{"ollama"}, stored.Capabilities)

	dropped, err := registry.ListFleetNodes(ctx, "t", &FleetNodeFilter{Backend: "claude"})
	require.NoError(t, err)
	assert.Empty(t, dropped, "a backend it no longer serves must not match")
}

// TestHeartbeat_IdenticalListWritesNoBackendChange pins the no-op: the routine
// heartbeat must not rewrite the backend axis or emit a reconciliation event
// on every tick.
func TestHeartbeat_IdenticalListWritesNoBackendChange(t *testing.T) {
	t.Parallel()

	recorder := &agentAuditRecorder{}
	registry := NewFleetRegistry(NewMemoryTaskKV(), WithStoreAuditor(recorder))
	ctx := context.Background()

	require.NoError(t, registry.RegisterFleetNode(ctx,
		enrolledAgent("t", "agent-1", "cn-1", "h", "claude", "codex")))
	before, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)

	observed := &FleetNodeObservation{Backends: []string{"claude", "codex"}}
	require.NoError(t, registry.Heartbeat(ctx, "t", "agent-1", observed))

	after, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)
	assert.Equal(t, before.Backend, after.Backend)
	assert.Equal(t, before.Capabilities, after.Capabilities)

	for _, event := range recorder.snapshot() {
		assert.NotEqual(t, AuditActionUpdate, event.Action,
			"an unchanged served list is not a reconciliation")
	}
}

// TestHeartbeat_NilObservationReconcilesNothing keeps the transports that
// carry no served list — gRPC, MCP, the SDK's embedded verb — from being read
// as reporting emptiness.
func TestHeartbeat_NilObservationReconcilesNothing(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()

	require.NoError(t, registry.RegisterFleetNode(ctx,
		enrolledAgent("t", "agent-1", "cn-1", "h", "claude", FleetNodeTraitGPU)))

	require.NoError(t, registry.Heartbeat(ctx, "t", "agent-1", nil))

	stored, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "claude", stored.Backend)
	assert.Equal(t, []string{"claude", FleetNodeTraitGPU}, stored.Capabilities)
	assert.Equal(t, FleetNodeStatusRunning, stored.Status, "liveness is still recorded")
}

// TestHeartbeat_RefusesAnObservationThatServesNothing surfaces the fault
// instead of quietly retiring a fleet member. It is refused BEFORE the store,
// so a daemon reporting nothing cannot even refresh its own last-seen.
func TestHeartbeat_RefusesAnObservationThatServesNothing(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()

	require.NoError(t, registry.RegisterFleetNode(ctx,
		enrolledAgent("t", "agent-1", "cn-1", "h", "claude")))
	before, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)

	for name, observed := range map[string]*FleetNodeObservation{
		"an empty list":      {Backends: []string{}},
		"a nil list":         {Backends: nil},
		"a blank entry":      {Backends: []string{"claude", "  "}},
		"only blank entries": {Backends: []string{""}},
	} {
		t.Run(name, func(t *testing.T) {
			hbErr := registry.Heartbeat(ctx, "t", "agent-1", observed)
			require.ErrorIs(t, hbErr, ErrFleetRegistryInvalidInput)
		})
	}

	after, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)
	assert.Equal(t, before.Version, after.Version, "a refused heartbeat writes nothing")
}

// TestAgentPrincipal covers the accessor's absent cases, which are what the
// takeover guard fails closed on.
func TestAgentPrincipal(t *testing.T) {
	t.Parallel()

	assert.Empty(t, FleetNodePrincipal(nil))
	assert.Empty(t, FleetNodePrincipal(&FleetNode{}))
	assert.Empty(t, FleetNodePrincipal(&FleetNode{Metadata: map[string]string{"other": "x"}}))
	assert.Equal(t, "cn-1", FleetNodePrincipal(&FleetNode{
		Metadata: map[string]string{FleetNodePrincipalMetadataKey: "cn-1"},
	}))
}

// TestAgentPrincipalMismatchError_Message names both principals, including the
// absent one, so an operator can tell a colliding CommonName from an attack.
func TestAgentPrincipalMismatchError_Message(t *testing.T) {
	t.Parallel()

	err := &FleetNodePrincipalMismatchError{TenantID: "t", NodeID: "a", Recorded: "", Presented: "cn-1"}
	assert.Contains(t, err.Error(), "recorded=(none)")
	assert.Contains(t, err.Error(), `presented="cn-1"`)
	assert.True(t, errors.Is(err, ErrFleetNodePrincipalMismatch))
}
