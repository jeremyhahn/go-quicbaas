// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func defaultAdmission(t *testing.T) *FleetAdmission {
	t.Helper()
	admission, err := NewDefaultFleetAdmission(0)
	if err != nil {
		t.Fatalf("NewDefaultFleetAdmission: %v", err)
	}
	return admission
}

// The healthy fixture (healthyHostMetrics) has 16 cores at 25% and 48 of
// 64 GiB available. At the seed profile of 1 core and 2 GiB per job its room
// is min(floor((0.90-0.25)*16), floor((48-6.4)/2)) = min(10, 20) = 10.
const healthyFixtureRoom = 10

func TestFleetAdmission_AdmitsAHealthyNodeWithRoom(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	if refusal := defaultAdmission(t).Admit(node, FleetNodeLoad{}, now); refusal != nil {
		t.Fatalf("Admit = %s, want admitted", refusal)
	}
}

func TestFleetAdmission_RefusesAnUnmeasuredNode(t *testing.T) {
	now := time.Now()
	node := &FleetNode{ID: "n1", Host: "h1", LastSeen: now.UnixNano()}
	refusal := defaultAdmission(t).Admit(node, FleetNodeLoad{}, now)
	if refusal == nil || refusal.Reason != FleetAdmissionUnhealthy || refusal.Health != FleetNodeHealthUnmeasured {
		t.Fatalf("Admit = %+v, want unhealthy (unmeasured)", refusal)
	}
	if refusal.NodeID != "n1" || refusal.Host != "h1" {
		t.Errorf("refusal subject = %s/%s, want n1/h1", refusal.NodeID, refusal.Host)
	}
}

func TestFleetAdmission_RefusesAStaleNode(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now.Add(-2*DefaultFleetHeartbeatTimeout))
	refusal := defaultAdmission(t).Admit(node, FleetNodeLoad{}, now)
	if refusal == nil || refusal.Reason != FleetAdmissionUnhealthy || refusal.Health != FleetNodeHealthStale {
		t.Fatalf("Admit = %+v, want unhealthy (stale)", refusal)
	}
}

func TestFleetAdmission_TheStaleBoundFollowsTheHeartbeatTimeout(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now.Add(-time.Minute))
	admission, err := NewDefaultFleetAdmission(30 * time.Second)
	if err != nil {
		t.Fatalf("NewDefaultFleetAdmission: %v", err)
	}
	refusal := admission.Admit(node, FleetNodeLoad{}, now)
	if refusal == nil || refusal.Health != FleetNodeHealthStale {
		t.Fatalf("Admit = %+v, want stale past a 30s bound", refusal)
	}
}

func TestFleetAdmission_RefusesANodePastAHealthCeiling(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	node.HostMetrics.CPUUtilization = Derived(0.97)
	refusal := defaultAdmission(t).Admit(node, FleetNodeLoad{}, now)
	if refusal == nil || refusal.Health != FleetNodeHealthCPUSaturated {
		t.Fatalf("Admit = %+v, want unhealthy (cpu saturated)", refusal)
	}
}

func TestFleetAdmission_RefusesANilNode(t *testing.T) {
	refusal := defaultAdmission(t).Admit(nil, FleetNodeLoad{}, time.Now())
	if refusal == nil || refusal.Reason != FleetAdmissionUnhealthy {
		t.Fatalf("Admit(nil) = %+v, want unhealthy", refusal)
	}
}

// An idle sample does not lift the operator's declared ceiling.
func TestFleetAdmission_NeverExceedsTheDeclaredWidthHoweverIdle(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	node.HostMetrics.CPUUtilization = Derived(0)
	node.MaxConcurrentJobs = 2

	admission := defaultAdmission(t)
	if refusal := admission.Admit(node, FleetNodeLoad{Live: 1}, now); refusal != nil {
		t.Fatalf("Admit at 1 of 2 = %s, want admitted", refusal)
	}
	refusal := admission.Admit(node, FleetNodeLoad{Live: 2}, now)
	if refusal == nil || refusal.Reason != FleetAdmissionAtWidth {
		t.Fatalf("Admit at 2 of 2 = %+v, want at declared width", refusal)
	}
	if !strings.Contains(refusal.Detail, "2 of 2") {
		t.Errorf("detail = %q, want the slot count", refusal.Detail)
	}
}

// Zero MaxConcurrentJobs is "not reported", not "zero slots": measured room
// alone decides.
func TestFleetAdmission_AnUnreportedWidthIsNotZeroSlots(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	if refusal := defaultAdmission(t).Admit(node, FleetNodeLoad{Live: 5}, now); refusal != nil {
		t.Fatalf("Admit = %s, want admitted with no declared width", refusal)
	}
}

// A continuation keeps its job's slot, so it is judged on health alone.
func TestFleetAdmission_AContinuationIsJudgedOnHealthAlone(t *testing.T) {
	now := time.Now()
	admission := defaultAdmission(t)

	full := measuredNode("n1", now)
	full.MaxConcurrentJobs = 1
	full.HostMetrics.CPUUtilization = Derived(0.85) // healthy, no room
	if refusal := admission.AdmitContinuation(full, now); refusal != nil {
		t.Fatalf("AdmitContinuation on a full healthy node = %s, want admitted", refusal)
	}

	stale := measuredNode("n2", now.Add(-2*DefaultFleetHeartbeatTimeout))
	refusal := admission.AdmitContinuation(stale, now)
	if refusal == nil || refusal.Health != FleetNodeHealthStale {
		t.Fatalf("AdmitContinuation on a stale node = %+v, want unhealthy (stale)", refusal)
	}
}

func TestFleetAdmission_SpawnsPlacedSinceTheSampleConsumeRoom(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	admission := defaultAdmission(t)

	if refusal := admission.Admit(node, FleetNodeLoad{PlacedSinceSample: healthyFixtureRoom - 1}, now); refusal != nil {
		t.Fatalf("Admit with one job of room left = %s, want admitted", refusal)
	}
	refusal := admission.Admit(node, FleetNodeLoad{PlacedSinceSample: healthyFixtureRoom}, now)
	if refusal == nil || refusal.Reason != FleetAdmissionNoRoom {
		t.Fatalf("Admit with room used up = %+v, want no measured room", refusal)
	}
}

func TestFleetAdmission_RefusesAHealthyNodeWithNoMeasuredRoom(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	// Healthy (under 90%) but 0.85*16 leaves 0.8 of a core: not one job.
	node.HostMetrics.CPUUtilization = Derived(0.85)
	refusal := defaultAdmission(t).Admit(node, FleetNodeLoad{}, now)
	if refusal == nil || refusal.Reason != FleetAdmissionNoRoom {
		t.Fatalf("Admit = %+v, want no measured room", refusal)
	}
}

func TestFleetAdmission_MeasuredRoomScalesWithUtilisation(t *testing.T) {
	now := time.Now()
	admission := defaultAdmission(t)
	cases := []struct {
		cpu  float64
		want int
	}{
		{0, 14},    // floor(0.90*16)
		{0.25, 10}, // floor(0.65*16)
		{0.50, 6},  // floor(0.40*16)
		{0.90, 0},
	}
	for _, tc := range cases {
		sample := healthyHostMetrics(now)
		sample.CPUUtilization = Derived(tc.cpu)
		if got := admission.MeasuredRoom(sample); got != tc.want {
			t.Errorf("MeasuredRoom at cpu %.2f = %d, want %d", tc.cpu, got, tc.want)
		}
	}
}

func TestFleetAdmission_MeasuredRoomIsBoundByMemory(t *testing.T) {
	now := time.Now()
	sample := healthyHostMetrics(now)
	// 6.4 GiB reserve (10% of 64 GiB); 12.4 GiB available leaves 6 GiB: 3 jobs.
	sample.MemAvailableBytes = Measured(6.4*(1<<30) + 6*(1<<30))
	if got := defaultAdmission(t).MeasuredRoom(sample); got != 3 {
		t.Fatalf("MeasuredRoom = %d, want 3 by memory", got)
	}
}

func TestFleetAdmission_AnUnreadSignalLeavesNoRoom(t *testing.T) {
	now := time.Now()
	admission := defaultAdmission(t)
	if got := admission.MeasuredRoom(nil); got != 0 {
		t.Errorf("MeasuredRoom(nil) = %d, want 0", got)
	}
	unread := []func(*HostMetrics){
		func(m *HostMetrics) { m.CPUUtilization = Unread() },
		func(m *HostMetrics) { m.CPUCount = Unread() },
		func(m *HostMetrics) { m.MemTotalBytes = Unread() },
		func(m *HostMetrics) { m.MemAvailableBytes = Unread() },
	}
	for i, clear := range unread {
		sample := healthyHostMetrics(now)
		clear(sample)
		if got := admission.MeasuredRoom(sample); got != 0 {
			t.Errorf("case %d: MeasuredRoom = %d, want 0 with a signal unread", i, got)
		}
	}
}

func TestFleetAdmission_ACostlierProfileLeavesLessRoom(t *testing.T) {
	now := time.Now()
	admission := defaultAdmission(t)
	if err := admission.SetProfile(FleetJobProfile{CPUCores: 4, MemoryBytes: 2 << 30}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if got := admission.MeasuredRoom(healthyHostMetrics(now)); got != 2 {
		t.Fatalf("MeasuredRoom at 4 cores a job = %d, want floor(10.4/4) = 2", got)
	}
}

func TestFleetJobProfile_ValidateRefusesAnUnusableProfile(t *testing.T) {
	cases := map[string]FleetJobProfile{
		"zero cores":     {CPUCores: 0, MemoryBytes: 1},
		"negative cores": {CPUCores: -1, MemoryBytes: 1},
		"nan cores":      {CPUCores: math.NaN(), MemoryBytes: 1},
		"inf cores":      {CPUCores: math.Inf(1), MemoryBytes: 1},
		"zero memory":    {CPUCores: 1, MemoryBytes: 0},
	}
	for name, profile := range cases {
		var profileErr *FleetJobProfileError
		if err := profile.Validate(); !errors.As(err, &profileErr) || !errors.Is(err, ErrFleetJobProfileInvalid) {
			t.Errorf("%s: Validate = %v, want FleetJobProfileError", name, err)
		}
	}
	if err := DefaultFleetJobProfile().Validate(); err != nil {
		t.Errorf("default profile: Validate = %v, want nil", err)
	}
}

func TestFleetAdmission_SetProfileKeepsTheOldProfileOnRefusal(t *testing.T) {
	admission := defaultAdmission(t)
	if err := admission.SetProfile(FleetJobProfile{}); !errors.Is(err, ErrFleetJobProfileInvalid) {
		t.Fatalf("SetProfile(zero) = %v, want ErrFleetJobProfileInvalid", err)
	}
	if got := admission.Profile(); got != DefaultFleetJobProfile() {
		t.Errorf("profile = %+v, want the seed kept", got)
	}
}

func TestNewFleetAdmission_RefusesAMissingJudgeOrBadProfile(t *testing.T) {
	if _, err := NewFleetAdmission(nil, DefaultFleetJobProfile()); !errors.Is(err, ErrFleetJobProfileInvalid) {
		t.Errorf("nil judge: err = %v, want ErrFleetJobProfileInvalid", err)
	}
	judge, err := NewFleetHealthJudge(DefaultFleetHealthThresholds())
	if err != nil {
		t.Fatalf("NewFleetHealthJudge: %v", err)
	}
	if _, err := NewFleetAdmission(judge, FleetJobProfile{CPUCores: 1}); !errors.Is(err, ErrFleetJobProfileInvalid) {
		t.Errorf("bad profile: err = %v, want ErrFleetJobProfileInvalid", err)
	}
}

func TestNewDefaultFleetAdmission_DefaultsTheStaleBound(t *testing.T) {
	admission := defaultAdmission(t)
	if got := admission.judge.Thresholds().MaxSampleAge; got != DefaultFleetHeartbeatTimeout {
		t.Errorf("stale bound = %s, want the default heartbeat timeout", got)
	}
	if _, err := NewDefaultFleetAdmission(time.Nanosecond); err != nil {
		t.Errorf("NewDefaultFleetAdmission(1ns) = %v, want any positive bound accepted", err)
	}
}

func TestFleetAdmission_RecalibrateAdoptsTheMeasuredCost(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	node.Host = "h1"
	admission := defaultAdmission(t)

	// 25% of 16 cores and 16 GiB used, across 4 jobs.
	adopted, err := admission.Recalibrate([]*FleetNode{node}, map[string]FleetNodeLoad{"h1": {Live: 4}}, 1, now)
	if err != nil || !adopted {
		t.Fatalf("Recalibrate = %v, %v, want adopted", adopted, err)
	}
	want := FleetJobProfile{CPUCores: 1, MemoryBytes: 4 << 30, Measured: true, SampleSize: 4, UpdatedAt: now}
	if got := admission.Profile(); got != want {
		t.Fatalf("profile = %+v, want %+v", got, want)
	}
}

func TestFleetAdmission_RecalibrateSkipsNodesWithNothingToAttribute(t *testing.T) {
	now := time.Now()
	measured := measuredNode("n1", now)
	measured.Host = "h1"
	unmeasured := &FleetNode{ID: "n2", Host: "h2", LastSeen: now.UnixNano()}
	stale := measuredNode("n3", now.Add(-2*DefaultFleetHeartbeatTimeout))
	stale.Host = "h3"
	// A second node on h1 must not count h1's jobs twice.
	sameHost := measuredNode("n4", now)
	sameHost.Host = "h1"
	idle := measuredNode("n5", now)
	idle.Host = "h5"

	admission := defaultAdmission(t)
	live := map[string]FleetNodeLoad{"h1": {Live: 4}, "h2": {Live: 50}, "h3": {Live: 50}}
	adopted, err := admission.Recalibrate([]*FleetNode{nil, measured, unmeasured, stale, sameHost, idle}, live, 1, now)
	if err != nil || !adopted {
		t.Fatalf("Recalibrate = %v, %v, want adopted", adopted, err)
	}
	if got := admission.Profile(); got.SampleSize != 4 || got.CPUCores != 1 {
		t.Fatalf("profile = %+v, want only h1's 4 jobs measured", got)
	}
}

// A burst of spawns between samples raises a host's live count before its
// sample shows their usage. Measuring it would divide the older usage by the
// larger count and cost each job too little.
func TestFleetAdmission_RecalibrateSkipsAHostPlacedOnSinceItsSample(t *testing.T) {
	now := time.Now()
	burst := measuredNode("n1", now)
	burst.Host = "h1"
	settled := measuredNode("n2", now)
	settled.Host = "h2"

	admission := defaultAdmission(t)
	loads := map[string]FleetNodeLoad{
		"h1": {Live: 8, PlacedSinceSample: 4},
		"h2": {Live: 4},
	}
	adopted, err := admission.Recalibrate([]*FleetNode{burst, settled}, loads, 1, now)
	if err != nil || !adopted {
		t.Fatalf("Recalibrate = %v, %v, want adopted", adopted, err)
	}
	if got := admission.Profile(); got.SampleSize != 4 || got.CPUCores != 1 {
		t.Fatalf("profile = %+v, want only h2's 4 jobs measured", got)
	}

	onlyBurst := defaultAdmission(t)
	adopted, err = onlyBurst.Recalibrate([]*FleetNode{burst}, loads, 1, now)
	if err != nil || adopted || onlyBurst.Profile().Measured {
		t.Fatalf("Recalibrate = %v, %v, profile %+v, want nothing measured", adopted, err, onlyBurst.Profile())
	}
}

func TestFleetAdmission_RecalibrateWithTooFewJobsKeepsTheProfile(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	node.Host = "h1"
	admission := defaultAdmission(t)
	adopted, err := admission.Recalibrate([]*FleetNode{node}, map[string]FleetNodeLoad{"h1": {Live: 2}}, 3, now)
	if err != nil || adopted {
		t.Fatalf("Recalibrate = %v, %v, want not adopted, no error", adopted, err)
	}
	if got := admission.Profile(); got != DefaultFleetJobProfile() {
		t.Errorf("profile = %+v, want the seed kept", got)
	}
}

func TestFleetAdmission_RecalibrateRefusesAZeroCostAndNoEvidence(t *testing.T) {
	now := time.Now()
	node := measuredNode("n1", now)
	node.Host = "h1"
	node.HostMetrics.CPUUtilization = Derived(0)
	admission := defaultAdmission(t)

	if _, err := admission.Recalibrate([]*FleetNode{node}, map[string]FleetNodeLoad{"h1": {Live: 4}}, 1, now); !errors.Is(err, ErrFleetJobProfileInvalid) {
		t.Errorf("zero cpu cost: err = %v, want ErrFleetJobProfileInvalid", err)
	}
	if _, err := admission.Recalibrate(nil, nil, 0, now); !errors.Is(err, ErrFleetJobProfileInvalid) {
		t.Errorf("minimum 0: err = %v, want ErrFleetJobProfileInvalid", err)
	}
	if got := admission.Profile(); got != DefaultFleetJobProfile() {
		t.Errorf("profile = %+v, want the seed kept", got)
	}
}

func TestFleetCapacityRefusedError_CarriesItsRefusalsAndUnwraps(t *testing.T) {
	err := error(&FleetCapacityRefusedError{
		Backend: "claude",
		Refusals: []FleetNodeRefusal{
			{NodeID: "n1", Host: "h1", Reason: FleetAdmissionUnhealthy, Health: FleetNodeHealthStale, Detail: "silent"},
			{NodeID: "n2", Host: "h2", Reason: FleetAdmissionAtWidth, Detail: "4 of 4"},
		},
	})
	if !errors.Is(err, ErrFleetCapacityRefused) {
		t.Fatalf("errors.Is(ErrFleetCapacityRefused) = false")
	}
	for _, want := range []string{"backend=claude", "n1 (h1): unhealthy (stale): silent", "n2 (h2): at_declared_width: 4 of 4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Error() = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// Every transport answers a capacity refusal as it answers an exhausted
// Agent pool, so the classification is the contract.
func TestFleetCapacityRefused_ClassifiesAsAnExhaustedPool(t *testing.T) {
	fault, ok := ClassifyAgentCatalogError(&FleetCapacityRefusedError{Backend: "claude"})
	if !ok || fault != AgentCatalogFaultExhausted {
		t.Fatalf("ClassifyAgentCatalogError = %v, %v, want exhausted", fault, ok)
	}
}

// A fleet that refused a spawn on health or capacity is the answer: the spawn
// fails with the refusal rather than running on the unmeasured local backend.
// The caller need not bring a resolve-error holder.
func TestSpawnSession_AFleetCapacityRefusalNeverFallsThroughToLocal(t *testing.T) {
	refused := &FleetCapacityRefusedError{
		Backend:  "opencode",
		Refusals: []FleetNodeRefusal{{NodeID: "n1", Host: "h1", Reason: FleetAdmissionNoRoom}},
	}
	for name, ctx := range map[string]context.Context{
		"no holder":     ContextWithTenant(context.Background(), "tenant-1"),
		"caller holder": WithResolveErrorHolder(ContextWithTenant(context.Background(), "tenant-1"), NewResolveErrorHolder()),
	} {
		svc, localSpawns := newStrictAffinityService(t, refused)
		_, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "opencode"})
		if !errors.Is(err, ErrFleetCapacityRefused) {
			t.Errorf("%s: SpawnSession() error = %v, want ErrFleetCapacityRefused", name, err)
		}
		if localSpawns.Load() != 0 {
			t.Errorf("%s: the local backend was spawned after the fleet refused", name)
		}
	}
}
