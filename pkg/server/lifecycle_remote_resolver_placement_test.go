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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// placementClock is the fixed instant the placement tests judge liveness at.
var placementClock = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

// seenAgo is a daemon-kind registration whose last heartbeat was ago before
// placementClock.
func seenAgo(name, host string, ago time.Duration, capabilities ...string) *orchestration.AgentRegistration {
	r := onlineDaemon(name, host, "codex")
	r.LastSeen = placementClock.Add(-ago).UnixNano()
	r.Capabilities = append([]string{"codex"}, capabilities...)
	return r
}

func placementPool(agents ...*orchestration.AgentRegistration) *remoteBackendPool {
	pool := newStrictTestPool(config.HostAffinityModeSoft, agents...)
	pool.now = func() time.Time { return placementClock }
	pool.staleAfter = 90 * time.Second
	return pool
}

// TestRemoteResolver_ADaemonSilentPastTheBoundIsNotDispatchedTo is omega's
// failure: its daemon died, its record kept saying "running", and every review
// routed to it was refused at the connection. A registration silent past the
// heartbeat bound is not a candidate, whatever its status says; an embedded
// agent, which heartbeats nothing because it is this process, always is.
func TestRemoteResolver_ADaemonSilentPastTheBoundIsNotDispatchedTo(t *testing.T) {
	t.Parallel()
	dead := seenAgo("omega", "omega:9091", 3*time.Hour)
	dead.Status = orchestration.AgentStatusRunning
	fresh := seenAgo("vs04", "vs04:9092", 10*time.Second)
	embedded := onlineDaemon("embedded", "vm-host-4", "codex")
	embedded.Kind = orchestration.AgentKindEmbedded
	embedded.LastSeen = placementClock.Add(-24 * time.Hour).UnixNano()

	live := placementPool().liveRegistrations([]*orchestration.AgentRegistration{dead, fresh, embedded, nil})
	if len(live) != 2 || live[0] != fresh || live[1] != embedded {
		t.Fatalf("live = %v, want the fresh daemon and the embedded agent only", live)
	}

	// Through resolve: a fleet whose only daemon is dead resolves nothing,
	// so the spawn falls to the in-process backend or is refused typed —
	// never dialled into a refused connection.
	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")
	placing := orchestration.WithSpawnPlacement(ctx)
	if backend, ok := placementPool(dead).resolve(placing, "codex", ""); ok || backend != nil {
		t.Fatalf("a dead daemon was placed on: %v", backend)
	}

	// ROUTING IS NOT PLACEMENT. An existing session's operation still
	// reaches the agent that holds it: a missed heartbeat is not evidence the
	// session is gone, and resolving nothing here would send the operation
	// to a backend that never held it.
	routing := placementPool(dead)
	routing.centralCred = &OrchestratorDaemonClient{ClientID: "test-id", ClientSecret: "test-secret"}
	routing.tokenURL = "https://127.0.0.1:19999/oauth2/token" // construction only, never dialled
	if backend, ok := routing.resolve(ctx, "codex", ""); !ok || backend == nil {
		t.Fatal("an existing session's operation was not routed to its quiet agent")
	}

	// The default bound applies when none is configured.
	pool := placementPool()
	pool.staleAfter = 0
	if got := pool.liveRegistrations([]*orchestration.AgentRegistration{seenAgo("a", "a:1", 2*time.Minute)}); len(got) != 0 {
		t.Fatalf("a daemon silent past the default bound was kept: %v", got)
	}
}

// TestRemoteResolver_ASigningSpawnGoesOnlyWhereTheSocketIs: with the spawn
// marked as mounting this daemon's signing socket, only a daemon advertising
// this daemon's identifier — or the embedded agent, co-located by construction
// — is a candidate; a remote daemon is passed over and, with nothing else, the
// constraint named is signing_agent.
func TestRemoteResolver_ASigningSpawnGoesOnlyWhereTheSocketIs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	id, err := orchestration.EnsureSigningRootID(root)
	if err != nil {
		t.Fatal(err)
	}
	pool := placementPool()
	pool.signingRoot = root
	ctx := orchestration.WithSigningAgentRequired(context.Background())

	trait, err := pool.requiredSigningTrait(ctx)
	if err != nil || trait != orchestration.SigningAgentTrait(id) {
		t.Fatalf("trait = %q, %v; want this daemon's", trait, err)
	}
	if none, _ := pool.requiredSigningTrait(context.Background()); none != "" {
		t.Fatalf("an unsigned spawn requires %q", none)
	}

	remote := seenAgo("omega", "omega:9091", time.Second)
	other := seenAgo("vs03", "vs03:9091", time.Second, orchestration.SigningAgentTrait("ffffffffffffffffffffffffffffffff"))
	colocated := seenAgo("vs04", "vs04:9092", time.Second, orchestration.SigningAgentTrait(id))
	embedded := onlineDaemon("embedded", "vm-host-4", "codex")
	embedded.Kind = orchestration.AgentKindEmbedded

	if got, _ := pool.pickDaemon([]*orchestration.AgentRegistration{remote, other, colocated}, "codex", "", false, trait); got != colocated {
		t.Fatalf("picked %v, want the co-located daemon", got)
	}
	if got, _ := pool.pickDaemon([]*orchestration.AgentRegistration{remote, embedded}, "codex", "", false, trait); got != embedded {
		t.Fatalf("picked %v, want the embedded agent", got)
	}
	got, constraint := pool.pickDaemon([]*orchestration.AgentRegistration{remote, other}, "codex", "", false, trait)
	if got != nil || constraint != "signing_agent" {
		t.Fatalf("picked %v (%q), want none on the signing_agent constraint", got, constraint)
	}
	if detail := buildNoMatchError("codex", "", false, constraint).Detail; !strings.Contains(detail, "signing socket") {
		t.Fatalf("the refusal must name the signing socket: %q", detail)
	}
	// An unsigned spawn still takes the first daemon serving the backend.
	if got, _ := pool.pickDaemon([]*orchestration.AgentRegistration{remote, colocated}, "codex", "", false, ""); got != remote {
		t.Fatalf("an unsigned spawn picked %v, want the first daemon", got)
	}
}

// TestRemoteResolver_AnUnidentifiedDaemonPlacesNoSigningSpawn: a daemon with
// no sockets root, or one whose root carries no marker yet, can prove no agent
// reaches its socket, so no daemon-kind agent qualifies.
func TestRemoteResolver_AnUnidentifiedDaemonPlacesNoSigningSpawn(t *testing.T) {
	t.Parallel()
	ctx := orchestration.WithSigningAgentRequired(context.Background())
	candidate := seenAgo("vs04", "vs04:9092", time.Second, orchestration.SigningAgentTrait("0123456789abcdef0123456789abcdef"))

	for name, root := range map[string]string{"no sockets root": "", "no marker": t.TempDir()} {
		pool := placementPool()
		pool.signingRoot = root
		trait, _ := pool.requiredSigningTrait(ctx)
		if trait != unreachableSigningTrait {
			t.Fatalf("%s: trait = %q, want the unreachable one", name, trait)
		}
		if got, _ := pool.pickDaemon([]*orchestration.AgentRegistration{candidate}, "codex", "", false, trait); got != nil {
			t.Fatalf("%s: placed a signing spawn on %v", name, got)
		}
	}
}
