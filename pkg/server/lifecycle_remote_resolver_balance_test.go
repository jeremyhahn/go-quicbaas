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

package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// balancingPool is a placement pool that can construct backends (so resolve
// runs end to end) and reports the given live load.
func balancingPool(load map[string]int, agents ...*orchestration.FleetNode) *remoteBackendPool {
	pool := placementPool(agents...)
	pool.centralCred = &OrchestratorDaemonClient{ClientID: "test-id", ClientSecret: "test-secret"}
	pool.tokenURL = "https://127.0.0.1:19999/oauth2/token" // construction only, never dialled
	pool.liveLoad = func() map[string]int { return load }
	return pool
}

// placeOnce resolves one spawn placement and answers the host it landed on.
func placeOnce(t *testing.T, pool *remoteBackendPool, ctx context.Context) string {
	t.Helper()
	backend, ok := pool.resolve(orchestration.WithSpawnPlacement(ctx), "codex", "")
	if !ok || backend == nil {
		t.Fatal("the spawn was placed nowhere")
	}
	remote, isRemote := backend.(*orchestration.RemoteACPBackend)
	if !isRemote {
		t.Fatalf("placed on %T, want a remote backend", backend)
	}
	return remote.ResolvedHost()
}

// TestRemoteResolver_SpawnsSpreadAcrossEveryEligibleAgent is the fleet that
// received nothing: three agents registered, online and eligible, and every
// spawn landed on the first one listed. Placed one after another with no live
// session yet to count, the spawns go round the fleet — the recent-placement
// tie-break — rather than onto one host.
func TestRemoteResolver_SpawnsSpreadAcrossEveryEligibleAgent(t *testing.T) {
	t.Parallel()
	agents := []*orchestration.FleetNode{
		seenAgo("agent-02", "vs02:9091", time.Second),
		seenAgo("agent-03", "vs03:9091", time.Second),
		seenAgo("agent-05", "vs05:9091", time.Second),
	}
	pool := balancingPool(map[string]int{}, agents...)
	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")

	placed := map[string]int{}
	for range 6 {
		placed[placeOnce(t, pool, ctx)]++
	}
	for _, agent := range agents {
		if placed[agent.Host] != 2 {
			t.Fatalf("placements = %v, want two on each of the three agents", placed)
		}
	}
}

// TestRemoteResolver_TheLeastLoadedAgentRelativeToItsCapacityWins: live jobs
// are weighed against each agent's advertised capacity, so a big host with more
// running can still be the emptier one.
func TestRemoteResolver_TheLeastLoadedAgentRelativeToItsCapacityWins(t *testing.T) {
	t.Parallel()
	small := seenAgo("small", "small:9091", time.Second)
	small.MaxConcurrentJobs = 2
	big := seenAgo("big", "big:9091", time.Second)
	big.MaxConcurrentJobs = 8
	pool := balancingPool(map[string]int{"small:9091": 1, "big:9091": 3}, small, big)

	got, constraint := pool.pickDaemon([]*orchestration.FleetNode{small, big}, "codex", "", false, "")
	if got != big || constraint != "" {
		t.Fatalf("picked %v (%q), want big: 3/8 is emptier than 1/2", got, constraint)
	}
}

// TestRemoteResolver_AFullAgentIsSkipped: an agent already running its
// advertised MaxConcurrentJobs takes no more, and a fleet that is full
// everywhere names capacity as the constraint rather than overfilling a host.
func TestRemoteResolver_AFullAgentIsSkipped(t *testing.T) {
	t.Parallel()
	full := seenAgo("full", "full:9091", time.Second)
	full.MaxConcurrentJobs = 2
	free := seenAgo("free", "free:9091", time.Second)
	free.MaxConcurrentJobs = 2
	pool := balancingPool(map[string]int{"full:9091": 2, "free:9091": 1}, full, free)

	if got, _ := pool.pickDaemon([]*orchestration.FleetNode{full, free}, "codex", "", false, ""); got != free {
		t.Fatalf("picked %v, want the agent with a free slot", got)
	}

	pool.liveLoad = func() map[string]int { return map[string]int{"full:9091": 2, "free:9091": 2} }
	got, constraint := pool.pickDaemon([]*orchestration.FleetNode{full, free}, "codex", "", false, "")
	if got != nil || constraint != "capacity" {
		t.Fatalf("picked %v (%q), want nothing and the capacity constraint", got, constraint)
	}
	if detail := buildNoMatchError("codex", "", false, constraint).Error(); !strings.Contains(detail, "free job slot") {
		t.Errorf("the refusal does not say why: %q", detail)
	}

	// An agent that advertises no capacity is never full.
	unbounded := seenAgo("unbounded", "unbounded:9091", time.Second)
	pool.liveLoad = func() map[string]int { return map[string]int{"unbounded:9091": 50} }
	if got, _ := pool.pickDaemon([]*orchestration.FleetNode{unbounded}, "codex", "", false, ""); got != unbounded {
		t.Fatalf("an agent with no advertised capacity was skipped: %v", got)
	}
}

// TestRemoteResolver_HostAffinityStillPinsAFullAgent: a pinned workspace goes
// to the host that holds it whatever the load — its job already holds its slot
// there — and never to the emptier agent beside it.
func TestRemoteResolver_HostAffinityStillPinsAFullAgent(t *testing.T) {
	t.Parallel()
	pinned := seenAgo("agent-03", "vs03:9091", time.Second)
	pinned.MaxConcurrentJobs = 1
	idle := seenAgo("agent-05", "vs05:9091", time.Second)
	idle.MaxConcurrentJobs = 4
	pool := balancingPool(map[string]int{"vs03:9091": 1}, pinned, idle)

	got, constraint := pool.pickDaemon([]*orchestration.FleetNode{pinned, idle}, "codex", "agent-03", false, "")
	if got != pinned || constraint != "" {
		t.Fatalf("picked %v (%q), want the pinned host even at capacity", got, constraint)
	}
}

// TestRemoteResolver_EmbeddedAndRemoteAreBalancedAlike: the embedded agent is a
// candidate like any other, not a default; the busier of the two loses.
func TestRemoteResolver_EmbeddedAndRemoteAreBalancedAlike(t *testing.T) {
	t.Parallel()
	embedded := onlineDaemon("embedded", "agent-04", "codex")
	embedded.Kind = orchestration.FleetNodeKindEmbedded
	embedded.MaxConcurrentJobs = 4
	remote := seenAgo("agent-02", "vs02:9091", time.Second)
	remote.MaxConcurrentJobs = 4

	pool := balancingPool(map[string]int{"agent-04": 3}, embedded, remote)
	if got, _ := pool.pickDaemon([]*orchestration.FleetNode{embedded, remote}, "codex", "", false, ""); got != remote {
		t.Fatalf("picked %v, want the idle remote over the busy embedded agent", got)
	}
	pool.liveLoad = func() map[string]int { return map[string]int{"vs02:9091": 3} }
	if got, _ := pool.pickDaemon([]*orchestration.FleetNode{embedded, remote}, "codex", "", false, ""); got != embedded {
		t.Fatalf("picked %v, want the idle embedded agent over the busy remote", got)
	}
}

// TestRemoteResolver_ASigningSpawnIsPlacedOnARelayCapableAgent: with no agent
// co-located with this daemon, a spawn that mounts its signing socket is
// placed on a remote fleet node advertising the signing relay (go-quicbaas#1013),
// and among several such agents the least loaded wins.
func TestRemoteResolver_ASigningSpawnIsPlacedOnARelayCapableAgent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	id, err := orchestration.EnsureSigningRootID(root)
	if err != nil {
		t.Fatal(err)
	}
	cannot := seenAgo("agent-09", "agent-09:9091", time.Second)
	busyRelay := seenAgo("agent-02", "vs02:9091", time.Second, orchestration.FleetNodeTraitSigningRelay)
	idleRelay := seenAgo("agent-03", "vs03:9091", time.Second, orchestration.FleetNodeTraitSigningRelay)

	pool := balancingPool(map[string]int{"vs02:9091": 2}, cannot, busyRelay, idleRelay)
	pool.signingRoot = root
	trait := orchestration.SigningAgentTrait(id)
	if got := pool.localSigningTrait(); got != trait {
		t.Fatalf("localSigningTrait = %q, want %q", got, trait)
	}

	got, constraint := pool.pickDaemon([]*orchestration.FleetNode{cannot, busyRelay, idleRelay}, "codex", "", false, trait)
	if got != idleRelay || constraint != "" {
		t.Fatalf("picked %v (%q), want the idle relay-capable agent", got, constraint)
	}

	// An agent that can neither mount nor relay is never placed on.
	got, constraint = pool.pickDaemon([]*orchestration.FleetNode{cannot}, "codex", "", false, trait)
	if got != nil || constraint != "signing_agent" {
		t.Fatalf("picked %v (%q), want nothing and the signing_agent constraint", got, constraint)
	}
	if detail := buildNoMatchError("codex", "", false, constraint).Error(); !strings.Contains(detail, "mount or relay") {
		t.Errorf("the refusal does not name the relay: %q", detail)
	}

	// No marker yet: no trait to compare, and a relay is still acceptable.
	unmarked := placementPool()
	unmarked.signingRoot = t.TempDir()
	if got := unmarked.localSigningTrait(); got != "" {
		t.Fatalf("an unmarked root yields trait %q", got)
	}
}

// TestRemoteResolver_RecentPlacementsAgeOut: the tie-break counts only the
// recent window, so a host that took a burst an hour ago is not penalised now.
func TestRemoteResolver_RecentPlacementsAgeOut(t *testing.T) {
	t.Parallel()
	now := placementClock
	pool := placementPool()
	pool.now = func() time.Time { return now }
	pool.placeMu.Lock()
	pool.notePlacementLocked("a:1")
	pool.notePlacementLocked("a:1")
	recent := pool.recentPlacementsLocked()["a:1"]
	pool.placeMu.Unlock()
	if recent != 2 {
		t.Fatalf("recent = %d, want 2", recent)
	}
	pool.forgetPlacement("a:1")
	pool.placeMu.Lock()
	recent = pool.recentPlacementsLocked()["a:1"]
	pool.placeMu.Unlock()
	if recent != 1 {
		t.Fatalf("recent after forgetting one = %d, want 1", recent)
	}
	now = now.Add(recentPlacementWindow + time.Second)
	pool.placeMu.Lock()
	defer pool.placeMu.Unlock()
	if got := pool.recentPlacementsLocked(); len(got) != 0 {
		t.Fatalf("recent = %v after the window, want none", got)
	}
}

// TestRemoteResolver_ConcurrentPlacementsSpreadEvenly: a burst of spawns
// placed at once — the factory's own dispatch shape — goes evenly across equal
// agents. Ranking and recording were separate, so every concurrent spawn
// ranked an unrecorded fleet and all of them chose the same agent.
func TestRemoteResolver_ConcurrentPlacementsSpreadEvenly(t *testing.T) {
	t.Parallel()
	const perAgent = 4
	agents := []*orchestration.FleetNode{
		seenAgo("agent-02", "vs02:9091", time.Second),
		seenAgo("agent-03", "vs03:9091", time.Second),
		seenAgo("agent-05", "vs05:9091", time.Second),
	}
	pool := balancingPool(map[string]int{}, agents...)
	ctx := orchestration.WithSpawnPlacement(orchestration.ContextWithTenant(context.Background(), "tenant-a"))

	start := make(chan struct{})
	hosts := make(chan string, perAgent*len(agents))
	var wg sync.WaitGroup
	for range perAgent * len(agents) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			backend, ok := pool.resolve(ctx, "codex", "")
			if !ok {
				hosts <- ""
				return
			}
			hosts <- backend.(*orchestration.RemoteACPBackend).ResolvedHost()
		}()
	}
	close(start)
	wg.Wait()
	close(hosts)

	placed := map[string]int{}
	for host := range hosts {
		placed[host]++
	}
	for _, agent := range agents {
		if placed[agent.Host] != perAgent {
			t.Fatalf("placements = %v, want %d on each agent", placed, perAgent)
		}
	}
}

// TestRemoteResolver_ASpawnGoesOnlyWhereItsImageIs is the 2026-09-23
// regression: every review placed on a fleet node without the reviewer image
// failed at image pull. An agent that reported its images and lacks the
// spawn's is skipped; one that never reported is still eligible (unknown).
func TestRemoteResolver_ASpawnGoesOnlyWhereItsImageIs(t *testing.T) {
	t.Parallel()
	const image = "worker:0.10.0"
	lacking := seenAgo("agent-02", "vs02:9091", time.Second)
	lacking.Images = []string{"worker:0.11.0"}
	having := seenAgo("agent-03", "vs03:9091", time.Second)
	having.Images = []string{"worker:0.10.0", "worker:0.11.0"}
	pool := balancingPool(map[string]int{}, lacking, having)
	ctx := orchestration.WithContainerImage(orchestration.ContextWithTenant(context.Background(), "tenant-a"), image)

	for range 4 {
		if host := placeOnce(t, pool, ctx); host != having.Host {
			t.Fatalf("placed on %s, which does not have %s", host, image)
		}
	}

	unknown := seenAgo("agent-05", "vs05:9091", time.Second)
	if reg, constraint, _ := pool.chooseDaemon([]*orchestration.FleetNode{unknown}, "codex", "", false, "", image, nil, false); reg == nil {
		t.Fatalf("an agent that never reported its images was refused (%s); unknown is not empty", constraint)
	}
	if reg, constraint, _ := pool.chooseDaemon([]*orchestration.FleetNode{lacking}, "codex", "", false, "", image, nil, false); reg != nil || constraint != "image" {
		t.Fatalf("chooseDaemon = (%v, %q), want no agent and the image constraint", reg, constraint)
	}
}
