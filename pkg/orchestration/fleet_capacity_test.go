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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fleetClock is the fixed instant every capacity measurement in this file is
// taken at, so "alive" and "stale" are decided by the record's own timestamp
// rather than by how long the test took to run.
var fleetClock = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

// erroringFleetRegistry answers every listing with one failure. It is the
// undecidable case: a registry that cannot be read has not reported an empty
// fleet.
type erroringFleetRegistry struct {
	FleetRegistry
	err error
}

func (r *erroringFleetRegistry) ListFleetNodes(context.Context, string, *FleetNodeFilter) ([]*FleetNode, error) {
	return nil, r.err
}

// staticFleetRegistry answers listings from records the test wrote verbatim.
//
// A DOUBLE RATHER THAN THE REAL REGISTRY, and for one specific reason: both
// RegisterFleetNode and UpdateFleetNode stamp LastSeen with the wall clock, so a record
// whose heartbeat age is the thing under test cannot be written through them.
// The liveness rule is what these cases exercise, and it reads the field.
type staticFleetRegistry struct {
	FleetRegistry
	byTenant map[string][]*FleetNode
}

func (r *staticFleetRegistry) ListFleetNodes(_ context.Context, tenantID string, _ *FleetNodeFilter) ([]*FleetNode, error) {
	return r.byTenant[tenantID], nil
}

// fleetDaemonRecord is one daemon's registration with a reported slot count and
// a heartbeat age relative to fleetClock.
func fleetDaemonRecord(tenantID, nodeID string, slots int, lastSeenAgo time.Duration) *FleetNode {
	agent := enrolledAgent(tenantID, nodeID, "cn-"+nodeID, "10.0.0.1:9090", "claude")
	agent.MaxConcurrentJobs = slots
	agent.Status = FleetNodeStatusIdle
	agent.LastSeen = fleetClock.Add(-lastSeenAgo).UnixNano()
	return agent
}

// staticFleet assembles a registry over the records given, keyed by tenant.
func staticFleet(agents ...*FleetNode) FleetRegistry {
	byTenant := make(map[string][]*FleetNode, len(agents))
	for _, agent := range agents {
		byTenant[agent.TenantID] = append(byTenant[agent.TenantID], agent)
	}
	return &staticFleetRegistry{byTenant: byTenant}
}

// =============================================================================
// The operator's declaration
// =============================================================================

// TestNewDeclaredFleetCapacity_AnsweesTheConfiguredCount is the ordinary case: a
// number an operator set is reported as theirs, so a refusal names the setting
// they would change.
func TestNewDeclaredFleetCapacity_AnswersTheConfiguredCount(t *testing.T) {
	t.Parallel()

	reader, err := NewDeclaredFleetCapacity(8)
	require.NoError(t, err)

	got, err := reader.FleetCapacity(context.Background(), fleetClock)
	require.NoError(t, err)
	assert.Equal(t, 8, got.Slots)
	assert.Equal(t, FleetCapacityOperator, got.Source)
	assert.True(t, got.Known())
	assert.Contains(t, got.Describe(), "operator")
}

// TestNewDeclaredFleetCapacity_ZeroLeavesTheAxisUndeclared: the shipped default.
// Zero is "nobody counted the slots", and it must not read as "there are none" —
// that reading refuses every job in the deployment.
func TestNewDeclaredFleetCapacity_ZeroLeavesTheAxisUndeclared(t *testing.T) {
	t.Parallel()

	reader, err := NewDeclaredFleetCapacity(0)
	require.NoError(t, err)

	got, err := reader.FleetCapacity(context.Background(), fleetClock)
	require.NoError(t, err)
	assert.False(t, got.Known(), "an unset capacity must not bound anything")
	assert.Equal(t, FleetCapacityUndeclared, got.Source)
	assert.Contains(t, got.Describe(), "undeclared")
}

// TestNewDeclaredFleetCapacity_RefusesANegativeCount: a bound below zero is a
// number somebody wrote on purpose and got wrong, and it cannot be told apart
// downstream from the undeclared case.
func TestNewDeclaredFleetCapacity_RefusesANegativeCount(t *testing.T) {
	t.Parallel()

	_, err := NewDeclaredFleetCapacity(-1)
	assert.ErrorIs(t, err, ErrAdmissionConfig)
}

// =============================================================================
// The fleet's own report
// =============================================================================

// TestAgentFleetCapacity_SumsTheLiveDaemonsSlots is the measurement this axis
// exists for: the fleet's bound is what its hosts say they can run, summed, and
// it tracks a host being added with nothing to keep in step by hand.
func TestAgentFleetCapacity_SumsTheLiveDaemonsSlots(t *testing.T) {
	t.Parallel()

	registry := staticFleet(
		fleetDaemonRecord("t1", "agent-1", 3, time.Second),
		fleetDaemonRecord("t1", "agent-2", 2, 10*time.Second),
		fleetDaemonRecord("t2", "agent-3", 1, time.Second),
	)

	reader, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1", "t2"}}, registry, time.Minute)
	require.NoError(t, err)

	got, err := reader.FleetCapacity(context.Background(), fleetClock)
	require.NoError(t, err)
	assert.Equal(t, 6, got.Slots, "the fleet is the sum over every tenant's live daemons")
	assert.Equal(t, FleetCapacityFleet, got.Source)
	assert.Equal(t, 3, got.Agents)
	assert.Zero(t, got.Unreported)
	assert.Contains(t, got.Describe(), "3 live daemon(s)")
}

// TestAgentFleetCapacity_ADaemonThatReportedNothingLeavesTheAxisUndeclared is the
// rule that decides every edge in this file: AN UNMEASURED VALUE IS NOT ZERO. One
// daemon short of a report makes the sum a LOWER BOUND on the fleet, and
// enforcing a lower bound would refuse work the fleet would have served.
func TestAgentFleetCapacity_ADaemonThatReportedNothingLeavesTheAxisUndeclared(t *testing.T) {
	t.Parallel()

	registry := staticFleet(
		fleetDaemonRecord("t1", "agent-1", 3, time.Second),
		fleetDaemonRecord("t1", "agent-2", 0, time.Second),
	)

	reader, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1"}}, registry, time.Minute)
	require.NoError(t, err)

	got, err := reader.FleetCapacity(context.Background(), fleetClock)
	require.NoError(t, err)
	assert.False(t, got.Known(), "a partially measured fleet is not a measured fleet")
	assert.Equal(t, 2, got.Agents)
	assert.Equal(t, 1, got.Unreported, "the count says which daemons an operator has to upgrade")
}

// TestAgentFleetCapacity_AnEmptyRegistryIsUndeclaredNotZero: a deployment whose
// work runs beside the control plane registers no daemon at all, and reading that
// as "no slots" would halt it.
func TestAgentFleetCapacity_AnEmptyRegistryIsUndeclaredNotZero(t *testing.T) {
	t.Parallel()

	reader, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1"}}, NewInMemoryFleetRegistry(), 0)
	require.NoError(t, err)

	got, err := reader.FleetCapacity(context.Background(), fleetClock)
	require.NoError(t, err)
	assert.False(t, got.Known())
	assert.Zero(t, got.Agents)
}

// TestAgentFleetCapacity_StopsCountingADaemonThatLeft covers all three ways a
// registration stops contributing. Each one is a host that will not take work, so
// counting its slots would hand the fleet capacity it does not have.
func TestAgentFleetCapacity_StopsCountingADaemonThatLeft(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*FleetNode){
		"stale heartbeat": func(a *FleetNode) { a.LastSeen = fleetClock.Add(-time.Hour).UnixNano() },
		"never seen":      func(a *FleetNode) { a.LastSeen = 0 },
		"disabled":        func(a *FleetNode) { a.Status = FleetNodeStatusDisabled },
		"disabled_at":     func(a *FleetNode) { a.DisabledAt = fleetClock.UnixNano() },
		"offline":         func(a *FleetNode) { a.Status = FleetNodeStatusOffline },
		"errored":         func(a *FleetNode) { a.Status = FleetNodeStatusError },
	}
	for name, leave := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			alive := fleetDaemonRecord("t1", "agent-1", 4, time.Second)
			gone := fleetDaemonRecord("t1", "agent-2", 9, time.Second)
			leave(gone)

			reader, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1"}},
				staticFleet(alive, gone), time.Minute)
			require.NoError(t, err)

			got, err := reader.FleetCapacity(context.Background(), fleetClock)
			require.NoError(t, err)
			assert.Equal(t, alive.MaxConcurrentJobs, got.Slots, "only the live daemon's slots count")
			assert.Equal(t, 1, got.Agents)
		})
	}
}

// TestAgentFleetCapacity_FailsClosedOnAnUnreadableStore: a partial sum is a
// smaller fleet than the real one, so a store that would not answer produces an
// error and NO number — never a number that happens to be low.
func TestAgentFleetCapacity_FailsClosedOnAnUnreadableStore(t *testing.T) {
	t.Parallel()

	broken := errors.New("registry down")
	reader, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1"}},
		&erroringFleetRegistry{err: broken}, time.Minute)
	require.NoError(t, err)

	got, err := reader.FleetCapacity(context.Background(), fleetClock)
	assert.ErrorIs(t, err, ErrAdmissionUndecidable)
	assert.ErrorIs(t, err, broken)
	assert.Zero(t, got.Slots)
}

// TestAgentFleetCapacity_FailsClosedOnAnUnreadableTenantList is the same rule one
// layer up: the set of tenants to measure over must be known before anything is
// summed.
func TestAgentFleetCapacity_FailsClosedOnAnUnreadableTenantList(t *testing.T) {
	t.Parallel()

	broken := errors.New("tenants down")
	reader, err := NewRegistryFleetCapacity(&fakeTenantLister{err: broken}, NewInMemoryFleetRegistry(), time.Minute)
	require.NoError(t, err)

	_, err = reader.FleetCapacity(context.Background(), fleetClock)
	assert.ErrorIs(t, err, ErrAdmissionUndecidable)
	assert.ErrorIs(t, err, broken)
}

// TestAgentFleetCapacity_HonoursACancelledContext: a measurement walks every
// tenant, and a cancelled sweep must stop walking rather than finish a reading
// nobody will use.
func TestAgentFleetCapacity_HonoursACancelledContext(t *testing.T) {
	t.Parallel()

	reader, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1", "t2"}}, NewInMemoryFleetRegistry(), time.Minute)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reader.FleetCapacity(ctx, fleetClock)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestNewAgentFleetCapacity_RefusesAnUnusableWiring: a reader with no tenant
// list, no registry, or a negative staleness bound cannot measure anything, and
// building it anyway would produce an axis that silently measured nothing.
func TestNewAgentFleetCapacity_RefusesAnUnusableWiring(t *testing.T) {
	t.Parallel()

	tenants := &fakeTenantLister{ids: []string{"t1"}}
	registry := NewInMemoryFleetRegistry()

	cases := map[string]func() (FleetCapacityReader, error){
		"no tenants":  func() (FleetCapacityReader, error) { return NewRegistryFleetCapacity(nil, registry, time.Minute) },
		"no registry": func() (FleetCapacityReader, error) { return NewRegistryFleetCapacity(tenants, nil, time.Minute) },
		"negative staleness": func() (FleetCapacityReader, error) {
			return NewRegistryFleetCapacity(tenants, registry, -time.Second)
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := build()
			assert.ErrorIs(t, err, ErrAdmissionConfig)
		})
	}
}

// =============================================================================
// The combination
// =============================================================================

// TestNewFleetCapacity_TheTighterBoundBinds is the combining rule: two bounds
// that both hold means the smaller one is the one in force.
func TestNewFleetCapacity_TheTighterBoundBinds(t *testing.T) {
	t.Parallel()

	registry := staticFleet(fleetDaemonRecord("t1", "agent-1", 6, time.Second))
	measured, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1"}}, registry, time.Minute)
	require.NoError(t, err)

	t.Run("the operator asked for less than the hardware could serve", func(t *testing.T) {
		t.Parallel()
		combined, err := NewFleetCapacity(2, measured)
		require.NoError(t, err)
		got, err := combined.FleetCapacity(context.Background(), fleetClock)
		require.NoError(t, err)
		assert.Equal(t, 2, got.Slots)
		assert.Equal(t, FleetCapacityOperator, got.Source)
		assert.Equal(t, 1, got.Agents, "the measurement rides along so a log line can say what was left on the table")
	})

	t.Run("the hardware is smaller than the operator's ceiling", func(t *testing.T) {
		t.Parallel()
		combined, err := NewFleetCapacity(100, measured)
		require.NoError(t, err)
		got, err := combined.FleetCapacity(context.Background(), fleetClock)
		require.NoError(t, err)
		assert.Equal(t, 6, got.Slots)
		assert.Equal(t, FleetCapacityFleet, got.Source)
	})
}

// TestNewFleetCapacity_OneSourceIsEnough covers each half answering alone, which
// is what a real deployment looks like before every daemon reports a count.
func TestNewFleetCapacity_OneSourceIsEnough(t *testing.T) {
	t.Parallel()

	t.Run("no measurement wired", func(t *testing.T) {
		t.Parallel()
		combined, err := NewFleetCapacity(5, nil)
		require.NoError(t, err)
		got, err := combined.FleetCapacity(context.Background(), fleetClock)
		require.NoError(t, err)
		assert.Equal(t, 5, got.Slots)
		assert.Equal(t, FleetCapacityOperator, got.Source)
	})

	t.Run("no operator ceiling", func(t *testing.T) {
		t.Parallel()
		registry := staticFleet(fleetDaemonRecord("t1", "agent-1", 4, time.Second))
		measured, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1"}}, registry, time.Minute)
		require.NoError(t, err)

		combined, err := NewFleetCapacity(0, measured)
		require.NoError(t, err)
		got, err := combined.FleetCapacity(context.Background(), fleetClock)
		require.NoError(t, err)
		assert.Equal(t, 4, got.Slots)
		assert.Equal(t, FleetCapacityFleet, got.Source)
	})

	t.Run("neither answers", func(t *testing.T) {
		t.Parallel()
		measured, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1"}}, NewInMemoryFleetRegistry(), time.Minute)
		require.NoError(t, err)
		combined, err := NewFleetCapacity(0, measured)
		require.NoError(t, err)
		got, err := combined.FleetCapacity(context.Background(), fleetClock)
		require.NoError(t, err)
		assert.False(t, got.Known(), "no source answering leaves the fleet axis out, exactly as before it existed")
	})
}

// TestNewFleetCapacity_AnUnmeasurableFleetFallsBackToTheOperatorsNumber: the
// daemons could not be summed, so the only bound left is the configured one — and
// the daemon counts are carried anyway so the operator is told what to fix.
func TestNewFleetCapacity_AnUnmeasurableFleetFallsBackToTheOperatorsNumber(t *testing.T) {
	t.Parallel()

	registry := staticFleet(fleetDaemonRecord("t1", "agent-1", 0, time.Second))
	measured, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1"}}, registry, time.Minute)
	require.NoError(t, err)

	combined, err := NewFleetCapacity(7, measured)
	require.NoError(t, err)
	got, err := combined.FleetCapacity(context.Background(), fleetClock)
	require.NoError(t, err)
	assert.Equal(t, 7, got.Slots)
	assert.Equal(t, FleetCapacityOperator, got.Source)
	assert.Equal(t, 1, got.Unreported)
}

// TestNewFleetCapacity_PropagatesAMeasurementFailure: an unreadable registry is
// not a fleet with no slots, and the combination must not quietly substitute the
// operator's number for a reading that FAILED — the two are different facts.
func TestNewFleetCapacity_PropagatesAMeasurementFailure(t *testing.T) {
	t.Parallel()

	broken := errors.New("registry down")
	measured, err := NewRegistryFleetCapacity(&fakeTenantLister{ids: []string{"t1"}},
		&erroringFleetRegistry{err: broken}, time.Minute)
	require.NoError(t, err)

	combined, err := NewFleetCapacity(7, measured)
	require.NoError(t, err)
	_, err = combined.FleetCapacity(context.Background(), fleetClock)
	assert.ErrorIs(t, err, broken)
}

// TestNewFleetCapacity_RefusesANegativeCeiling: the same refusal the declared
// reader makes, at the seam the server actually calls.
func TestNewFleetCapacity_RefusesANegativeCeiling(t *testing.T) {
	t.Parallel()

	_, err := NewFleetCapacity(-3, nil)
	assert.ErrorIs(t, err, ErrAdmissionConfig)
}

// =============================================================================
// The heartbeat's capacity axis
// =============================================================================

// TestHeartbeat_ReconcilesTheReportedCapacity: a host resized under a running
// daemon converges on its next heartbeat, with no re-registration.
func TestHeartbeat_ReconcilesTheReportedCapacity(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()
	agent := enrolledAgent("t", "agent-1", "cn-1", "10.0.0.1:9090", "claude")
	agent.MaxConcurrentJobs = 2
	require.NoError(t, registry.RegisterFleetNode(ctx, agent))

	require.NoError(t, registry.Heartbeat(ctx, "t", "agent-1",
		&FleetNodeObservation{Backends: []string{"claude"}, MaxConcurrentJobs: 5}))

	stored, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)
	assert.Equal(t, 5, stored.MaxConcurrentJobs)
}

// TestHeartbeat_AnUnreportedCapacityLeavesTheRecordAlone is the asymmetry the
// backend axis has for a nil observation: zero means the daemon said nothing, and
// taking it as an instruction would drop a host out of the fleet sum on its first
// heartbeat from an older build.
func TestHeartbeat_AnUnreportedCapacityLeavesTheRecordAlone(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()
	agent := enrolledAgent("t", "agent-1", "cn-1", "10.0.0.1:9090", "claude")
	agent.MaxConcurrentJobs = 3
	require.NoError(t, registry.RegisterFleetNode(ctx, agent))

	require.NoError(t, registry.Heartbeat(ctx, "t", "agent-1",
		&FleetNodeObservation{Backends: []string{"claude"}}))

	stored, err := registry.GetFleetNode(ctx, "t", "agent-1")
	require.NoError(t, err)
	assert.Equal(t, 3, stored.MaxConcurrentJobs, "an unreported count must not erase a reported one")
}

// TestReconcileAgentCapacity_IgnoresWhatItCannotAct uses the reconciler directly
// for the shapes the registry never reaches it with, so a future caller cannot
// discover them the hard way.
func TestReconcileAgentCapacity_IgnoresWhatItCannotAct(t *testing.T) {
	t.Parallel()

	assert.False(t, reconcileFleetNodeCapacity(nil, &FleetNodeObservation{MaxConcurrentJobs: 4}))
	assert.False(t, reconcileFleetNodeCapacity(&FleetNode{}, nil))
	assert.False(t, reconcileFleetNodeCapacity(&FleetNode{MaxConcurrentJobs: 4},
		&FleetNodeObservation{MaxConcurrentJobs: -1}), "a negative count is not a capacity")
	assert.False(t, reconcileFleetNodeCapacity(&FleetNode{MaxConcurrentJobs: 4},
		&FleetNodeObservation{MaxConcurrentJobs: 4}), "reporting the count already held changes nothing")
}

// TestAgentObservation_RefusesANegativeCapacity: the contract's own refusal. A
// negative slot count cannot be a capacity, and clamping it would turn a caller's
// bug into a daemon that silently stopped counting.
func TestAgentObservation_RefusesANegativeCapacity(t *testing.T) {
	t.Parallel()

	observed := &FleetNodeObservation{Backends: []string{"claude"}, MaxConcurrentJobs: -2}
	assert.ErrorIs(t, observed.Validate(), ErrFleetRegistryInvalidInput)
}

// TestAgentObservation_AcceptsAnUnreportedCapacity is the other half: zero is the
// legitimate "this daemon reports no count" value and must validate.
func TestAgentObservation_AcceptsAnUnreportedCapacity(t *testing.T) {
	t.Parallel()

	observed := &FleetNodeObservation{Backends: []string{"claude"}}
	assert.NoError(t, observed.Validate())
}

// TestFleetCapacity_KnownRejectsAnEmptySource guards the zero value of the
// struct itself: a FleetCapacity nobody filled in must not bound anything.
func TestFleetCapacity_KnownRejectsAnEmptySource(t *testing.T) {
	t.Parallel()

	assert.False(t, FleetCapacity{}.Known())
	assert.False(t, FleetCapacity{Slots: 4}.Known(), "slots with no source is a number from nowhere")
	assert.False(t, FleetCapacity{Source: FleetCapacityFleet}.Known(), "a source with no slots bounds nothing")
	assert.True(t, FleetCapacity{Slots: 1, Source: FleetCapacityFleet}.Known())
}

// TestFleetCapacitySource_String pins the rendering an operator reads in a log
// line and a refusal message.
func TestFleetCapacitySource_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "operator", FleetCapacityOperator.String())
	assert.Equal(t, "fleet", FleetCapacityFleet.String())
	assert.Equal(t, "undeclared", FleetCapacityUndeclared.String())
}

// TestFleetAgentAlive_ToleratesANilRecord: a listing that carried a nil is a
// store fault, not a live daemon, and a measurement that dereferenced it would
// end every sweep in the deployment.
func TestFleetAgentAlive_ToleratesANilRecord(t *testing.T) {
	t.Parallel()

	assert.False(t, FleetNodeAlive(nil, fleetClock, time.Minute))
}

// TestFleetAgentAlive_CountsADaemonHeartbeatingNow is the positive case the
// negatives are measured against, asserted directly so the predicate's own
// contract is pinned rather than inferred from a sum.
func TestFleetAgentAlive_CountsADaemonHeartbeatingNow(t *testing.T) {
	t.Parallel()

	assert.True(t, FleetNodeAlive(
		fleetDaemonRecord("t1", "agent-1", 2, time.Second), fleetClock, time.Minute))
}

// TestAdmissionController_FleetCapacityWithNoReaderIsUndeclared: a controller
// built with no capacity reader leaves the fleet axis out, which is the
// pre-existing behaviour — and must not report a bound of zero.
func TestAdmissionController_FleetCapacityWithNoReaderIsUndeclared(t *testing.T) {
	t.Parallel()

	rig := newAdmissionRig(t)
	controller, err := NewAdmissionController(AdmissionControllerConfig{
		Tenants: rig.tenants, Projects: rig.projects, Queue: rig.queue,
		Leases: rig.leases, LeaseTTL: testPollerLeaseTTL,
	})
	require.NoError(t, err)

	got, err := controller.FleetCapacity(context.Background(), fleetClock)
	require.NoError(t, err)
	assert.False(t, got.Known())
	assert.Equal(t, FleetCapacityUndeclared, got.Source)
}

// TestPlan_NoPlanWhenNeitherNewAxisIsInForce: a plan's price is that one
// unreadable project fails the whole plan, and when there is nothing to divide
// and nothing to defer on that price buys nothing. The controller answers "no
// plan" so the sweeps keep their per-project arithmetic — and keep a store
// outage confined to the project whose store failed.
func TestPlan_NoPlanWhenNeitherNewAxisIsInForce(t *testing.T) {
	t.Parallel()

	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "p1", 2)

	plan, err := rig.controller(t, 0).Plan(context.Background())
	require.NoError(t, err)
	assert.Nil(t, plan, "an undeclared fleet and no metered pool leave nothing for a plan to decide")
}

// TestPlan_AMeteredPoolAloneIsEnoughForAPlan is the other half: the provider axis
// can defer a project on its own, so a plan is produced even with no fleet
// capacity declared.
func TestPlan_AMeteredPoolAloneIsEnoughForAPlan(t *testing.T) {
	t.Parallel()

	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "p1", 2)
	rig.headroom = &fakePoolHeadroom{}

	plan, err := rig.controller(t, 0).Plan(context.Background())
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.False(t, plan.Capacity.Known(), "the fleet axis is still out; the provider axis is what produced the plan")
}
