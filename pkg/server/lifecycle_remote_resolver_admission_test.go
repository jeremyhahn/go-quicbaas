// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// placingContext is a spawn being placed for tenant-a, with a holder the
// resolver reports its refusal through.
func placingContext() (context.Context, *orchestration.ResolveErrorHolder) {
	holder := orchestration.NewResolveErrorHolder()
	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")
	ctx = orchestration.WithResolveErrorHolder(ctx, holder)
	return orchestration.WithSpawnPlacement(ctx), holder
}

// refusedPlacement resolves one spawn the fleet must refuse, and returns the
// refusals it reported.
func refusedPlacement(t *testing.T, pool *remoteBackendPool) []orchestration.FleetNodeRefusal {
	t.Helper()
	ctx, holder := placingContext()
	if backend, ok := pool.resolve(ctx, "codex", ""); ok || backend != nil {
		t.Fatal("the spawn was placed; want it refused")
	}
	var refused *orchestration.FleetCapacityRefusedError
	if !errors.As(holder.Err(), &refused) {
		t.Fatalf("resolve error = %v, want a FleetCapacityRefusedError", holder.Err())
	}
	return refused.Refusals
}

func TestRemoteResolver_AnUnmeasuredAgentIsNotPlacedOn(t *testing.T) {
	t.Parallel()
	agent := seenAgo("agent-02", "vs02:9091", time.Second)
	agent.HostMetrics = nil
	refusals := refusedPlacement(t, balancingPool(map[string]int{}, agent))
	if len(refusals) != 1 || refusals[0].Health != orchestration.FleetNodeHealthUnmeasured {
		t.Fatalf("refusals = %+v, want the agent refused as unmeasured", refusals)
	}
}

// A heartbeat proves the agent is alive, not that its sample is current.
func TestRemoteResolver_AnAgentWithAStaleSampleIsNotPlacedOn(t *testing.T) {
	t.Parallel()
	agent := seenAgo("agent-02", "vs02:9091", time.Second)
	agent.HostMetrics = roomyHostMetrics(placementClock.Add(-5 * time.Minute))
	refusals := refusedPlacement(t, balancingPool(map[string]int{}, agent))
	if len(refusals) != 1 || refusals[0].Health != orchestration.FleetNodeHealthStale {
		t.Fatalf("refusals = %+v, want the agent refused as stale", refusals)
	}
}

func TestRemoteResolver_AnUnhealthyAgentIsPassedOverForAHealthyOne(t *testing.T) {
	t.Parallel()
	saturated := seenAgo("agent-02", "vs02:9091", time.Second)
	saturated.HostMetrics.CPUUtilization = orchestration.Derived(0.97)
	healthy := seenAgo("agent-03", "vs03:9091", time.Second)
	// The saturated agent is the less loaded one; health still decides.
	pool := balancingPool(map[string]int{"vs03:9091": 3}, saturated, healthy)
	ctx, _ := placingContext()
	for range 3 {
		if host := placeOnce(t, pool, ctx); host != healthy.Host {
			t.Fatalf("placed on %s, want only the healthy %s", host, healthy.Host)
		}
	}
}

// An idle sample does not lift the declared MaxConcurrentJobs.
func TestRemoteResolver_PlacementNeverExceedsTheDeclaredWidth(t *testing.T) {
	t.Parallel()
	agent := seenAgo("agent-02", "vs02:9091", time.Second)
	agent.HostMetrics.CPUUtilization = orchestration.Derived(0)
	agent.MaxConcurrentJobs = 2
	refusals := refusedPlacement(t, balancingPool(map[string]int{"vs02:9091": 2}, agent))
	if len(refusals) != 1 || refusals[0].Reason != orchestration.FleetAdmissionAtWidth {
		t.Fatalf("refusals = %+v, want the agent refused at its declared width", refusals)
	}
}

// The sample cannot show what was placed after it; placement counts it.
func TestRemoteResolver_SpawnsPlacedSinceTheSampleUseUpItsRoom(t *testing.T) {
	t.Parallel()
	agent := seenAgo("agent-02", "vs02:9091", time.Second)
	// 10% of 16 cores below the ceiling: room for one job.
	agent.HostMetrics.CPUUtilization = orchestration.Derived(0.80)
	pool := balancingPool(map[string]int{}, agent)

	ctx, _ := placingContext()
	if host := placeOnce(t, pool, ctx); host != agent.Host {
		t.Fatalf("placed on %s, want %s", host, agent.Host)
	}
	refusals := refusedPlacement(t, pool)
	if len(refusals) != 1 || refusals[0].Reason != orchestration.FleetAdmissionNoRoom {
		t.Fatalf("refusals = %+v, want the agent refused with no measured room", refusals)
	}
}

// A pinned spawn keeps its job's slot but not an unhealthy agent.
func TestRemoteResolver_ASpawnWhoseJobHoldsASlotIsJudgedOnHealth(t *testing.T) {
	t.Parallel()
	full := seenAgo("agent-03", "vs03:9091", time.Second)
	full.MaxConcurrentJobs = 1
	pool := balancingPool(map[string]int{"vs03:9091": 1}, full)
	slots := orchestration.JobSlotHosts{"vs03:9091": {}}
	got, constraint, refusals := pool.chooseDaemon([]*orchestration.FleetNode{full}, "codex", "agent-03", false, "", "", slots, true)
	if got != full || constraint != "" || refusals != nil {
		t.Fatalf("chooseDaemon = (%v, %q, %+v), want the job's healthy agent at its width", got, constraint, refusals)
	}

	unmeasured := seenAgo("agent-05", "vs05:9091", time.Second)
	unmeasured.HostMetrics = nil
	slots = orchestration.JobSlotHosts{"vs05:9091": {}}
	got, constraint, refusals = pool.chooseDaemon([]*orchestration.FleetNode{unmeasured}, "codex", "agent-05", false, "", "", slots, true)
	if got != nil || constraint != "capacity" || len(refusals) != 1 || refusals[0].Health != orchestration.FleetNodeHealthUnmeasured {
		t.Fatalf("chooseDaemon = (%v, %q, %+v), want the job's unmeasured agent refused", got, constraint, refusals)
	}
}

// An affinity names where a spawn may run, not a slot it already holds: a new
// job targeted at a full agent is refused like any other new job.
func TestRemoteResolver_AnAffinityAloneDoesNotBypassCapacity(t *testing.T) {
	t.Parallel()
	full := seenAgo("agent-03", "vs03:9091", time.Second)
	full.MaxConcurrentJobs = 1
	pool := balancingPool(map[string]int{"vs03:9091": 1}, full)
	got, constraint, refusals := pool.chooseDaemon([]*orchestration.FleetNode{full}, "codex", "agent-03", false, "", "", nil, true)
	if got != nil || constraint != "capacity" || len(refusals) != 1 || refusals[0].Reason != orchestration.FleetAdmissionAtWidth {
		t.Fatalf("chooseDaemon = (%v, %q, %+v), want the full agent refused at its width", got, constraint, refusals)
	}
}

// The slot reaches placement on the spawn's context.
func TestRemoteResolver_ResolvePlacesAJobOnTheFullAgentHoldingItsSlot(t *testing.T) {
	t.Parallel()
	full := seenAgo("agent-03", "vs03:9091", time.Second)
	full.MaxConcurrentJobs = 1
	pool := balancingPool(map[string]int{"vs03:9091": 1}, full)

	ctx, _ := placingContext()
	ctx = orchestration.WithHostAffinity(ctx, "agent-03")
	ctx = orchestration.WithJobSlotHosts(ctx, orchestration.JobSlotHosts{"vs03:9091": {}})
	if host := placeOnce(t, pool, ctx); host != full.Host {
		t.Fatalf("placed on %s, want %s", host, full.Host)
	}
}

// Selection without placement (pickDaemon) is not admission: it ranks.
func TestRemoteResolver_SelectionWithoutPlacementIsNotAdmitted(t *testing.T) {
	t.Parallel()
	agent := seenAgo("agent-02", "vs02:9091", time.Second)
	agent.HostMetrics = nil
	pool := balancingPool(map[string]int{}, agent)
	if got, constraint := pool.pickDaemon([]*orchestration.FleetNode{agent}, "codex", "", false, ""); got != agent {
		t.Fatalf("pickDaemon = (%v, %q), want the agent", got, constraint)
	}
}

func TestRemoteResolver_TheEmbeddedAgentIsMeasuredByThisHost(t *testing.T) {
	t.Parallel()
	embedded := onlineDaemon("embedded", "agent-04", "codex")
	embedded.Kind = orchestration.FleetNodeKindEmbedded
	pool := balancingPool(map[string]int{})
	ctx := context.Background()

	regs := pool.measureEmbedded(ctx, []*orchestration.FleetNode{embedded})
	if got, _, refusals := pool.chooseDaemon(regs, "codex", "", false, "", "", nil, true); got != nil || len(refusals) != 1 {
		t.Fatalf("chooseDaemon without a sampler = (%v, %+v), want the unmeasured embedded agent refused", got, refusals)
	}

	pool.embeddedMetrics = func(context.Context) *orchestration.HostMetrics {
		return roomyHostMetrics(placementClock)
	}
	regs = pool.measureEmbedded(ctx, []*orchestration.FleetNode{embedded, nil})
	got, constraint, _ := pool.chooseDaemon(regs, "codex", "", false, "", "", nil, true)
	if got == nil || got.ID != embedded.ID {
		t.Fatalf("chooseDaemon with a sampler = (%v, %q), want the embedded agent", got, constraint)
	}
	if embedded.HostMetrics != nil {
		t.Error("measuring the embedded agent wrote to its registration")
	}
}

// Placement measures what a job costs from the agents it ranks.
func TestRemoteResolver_PlacementRecalibratesTheJobProfile(t *testing.T) {
	t.Parallel()
	// 25% of 16 cores and 16 GiB in use across 8 jobs: half a core, 2 GiB.
	agent := seenAgo("agent-02", "vs02:9091", time.Second)
	pool := balancingPool(map[string]int{"vs02:9091": 8}, agent)
	if got, _, _ := pool.chooseDaemon([]*orchestration.FleetNode{agent}, "codex", "", false, "", "", nil, true); got != agent {
		t.Fatalf("chooseDaemon = %v, want the agent", got)
	}
	profile := pool.admission.Profile()
	if !profile.Measured || profile.CPUCores != 0.5 || profile.MemoryBytes != 2<<30 || profile.SampleSize != 8 {
		t.Fatalf("profile = %+v, want 0.5 cores and 2 GiB measured over 8 jobs", profile)
	}
}

// A host placed on since its sample counts jobs the sample never saw: dividing
// the sampled usage by them would cost each job too little and widen every
// agent's room.
func TestRemoteResolver_RecalibrationSkipsAHostPlacedOnSinceItsSample(t *testing.T) {
	t.Parallel()
	agent := seenAgo("agent-02", "vs02:9091", time.Second)
	pool := balancingPool(map[string]int{"vs02:9091": 8}, agent)
	pool.placements = map[string][]time.Time{agent.Host: {placementClock}}
	if got, _, _ := pool.chooseDaemon([]*orchestration.FleetNode{agent}, "codex", "", false, "", "", nil, true); got != agent {
		t.Fatalf("chooseDaemon = %v, want the agent", got)
	}
	if profile := pool.admission.Profile(); profile.Measured {
		t.Fatalf("profile = %+v, want the default kept: the only host's sample predates a placement", profile)
	}
}

func TestRemoteResolver_AdmissionUnavailableRefusesEveryAgent(t *testing.T) {
	t.Parallel()
	agents := []*orchestration.FleetNode{
		seenAgo("agent-02", "vs02:9091", time.Second),
		seenAgo("agent-03", "vs03:9091", time.Second),
	}
	refusals := unavailableAdmissionRefusals(agents, orchestration.ErrFleetJobProfileInvalid)
	if len(refusals) != len(agents) {
		t.Fatalf("refusals = %+v, want one per agent", refusals)
	}
	for i, refusal := range refusals {
		if refusal.NodeID != agents[i].ID || refusal.Reason != orchestration.FleetAdmissionUnhealthy {
			t.Errorf("refusal %d = %+v, want agent %s refused", i, refusal, agents[i].ID)
		}
	}
}

func TestEmbeddedHostMetricsSampler_SamplesWhereTheEmbeddedAgentWorks(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Storage.DataDir = t.TempDir()
	s := &Server{config: cfg}
	sampler, err := s.embeddedHostMetricsSampler(strictNoopLogger{})
	if err != nil {
		t.Fatalf("embeddedHostMetricsSampler: %v", err)
	}
	if sample := sampler.Sample(context.Background()); sample == nil {
		t.Fatal("Sample = nil, want a host sample")
	}
}

func TestEmbeddedHostMetricsSampler_RefusesWithNowhereToMeasure(t *testing.T) {
	t.Parallel()
	s := &Server{config: &config.Config{}}
	if _, err := s.embeddedHostMetricsSampler(strictNoopLogger{}); err == nil {
		t.Fatal("embeddedHostMetricsSampler with no data directory = nil error, want a refusal")
	}
}
