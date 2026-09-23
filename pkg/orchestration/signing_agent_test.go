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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureSigningRootID_WritesOnceAndKeeps: the first call writes a fresh
// identifier, a later one — a restarted daemon — reads the same one back.
func TestEnsureSigningRootID_WritesOnceAndKeeps(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first, err := EnsureSigningRootID(root)
	if err != nil {
		t.Fatalf("EnsureSigningRootID: %v", err)
	}
	if !validSigningRootID(first) {
		t.Fatalf("identifier %q is not the written shape", first)
	}
	info, err := os.Stat(filepath.Join(root, SigningRootMarkerFile))
	if err != nil || info.Mode().Perm() != signingRootMarkerMode {
		t.Fatalf("marker = %v, %v; want mode %v", info, err, signingRootMarkerMode)
	}
	again, err := EnsureSigningRootID(root)
	if err != nil || again != first {
		t.Fatalf("second call = %q, %v; want the kept %q", again, err, first)
	}
}

// TestSigningRootID_Refusals: a missing root, a marker holding something that
// is not an identifier, and an unwritable root are each typed.
func TestSigningRootID_Refusals(t *testing.T) {
	t.Parallel()
	if _, err := ReadSigningRootID(t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an unmarked root: %v, want ErrNotExist", err)
	}

	planted := t.TempDir()
	if err := os.WriteFile(filepath.Join(planted, SigningRootMarkerFile), []byte("not-an-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var marker *SigningRootMarkerError
	if _, err := ReadSigningRootID(planted); !errors.As(err, &marker) {
		t.Fatalf("a planted marker: %v, want *SigningRootMarkerError", err)
	}
	if _, err := EnsureSigningRootID(planted); !errors.As(err, &marker) {
		t.Fatalf("ensuring over a planted marker: %v, want it refused, never overwritten", err)
	}

	if _, err := EnsureSigningRootID(filepath.Join(t.TempDir(), "absent")); !errors.As(err, &marker) {
		t.Fatalf("a root that does not exist: %v, want *SigningRootMarkerError", err)
	}
	if !strings.Contains(marker.Error(), "could not be created") {
		t.Fatalf("the refusal must say what failed: %q", marker.Error())
	}
}

// TestDiscoverSigningRootIDs finds a sockets root at an admitted root itself
// and one level below it — the <runtime dir>/quicbaas/<instance> shape — and
// nothing deeper, unreadable, or relative. The answer is sorted and unique.
func TestDiscoverSigningRootIDs(t *testing.T) {
	t.Parallel()
	direct := t.TempDir()
	directID, err := EnsureSigningRootID(direct)
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	instance := filepath.Join(parent, "a1b2c3")
	if err := os.Mkdir(instance, 0o700); err != nil {
		t.Fatal(err)
	}
	childID, err := EnsureSigningRootID(instance)
	if err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(parent, "x", "y")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSigningRootID(deep); err != nil {
		t.Fatal(err)
	}

	got := DiscoverSigningRootIDs([]string{parent, direct, direct, "relative", "", filepath.Join(parent, "missing")})
	want := []string{childID, directID}
	if want[0] > want[1] {
		want[0], want[1] = want[1], want[0]
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("discovered %v, want %v (and never the root two levels down)", got, want)
	}
	if ids := DiscoverSigningRootIDs(nil); ids == nil || len(ids) != 0 {
		t.Fatalf("no roots must report an empty, non-nil list: %#v", ids)
	}
}

// TestSigningAgentPlacementPrimitives: the trait shape, the context mark, and
// the spawn predicate that reads the requirement off the mount itself.
func TestSigningAgentPlacementPrimitives(t *testing.T) {
	t.Parallel()
	if got := SigningAgentTrait("abc"); got != "signing-agent:abc" || !isSigningAgentTrait(got) {
		t.Fatalf("trait = %q", got)
	}
	if isSigningAgentTrait(AgentTraitGPU) {
		t.Fatal("gpu is not a signing-agent trait")
	}
	if SigningAgentRequiredFromContext(context.Background()) || SigningAgentRequiredFromContext(nil) { //nolint:staticcheck // nil is the case under test
		t.Fatal("an unmarked context requires nothing")
	}
	if !SigningAgentRequiredFromContext(WithSigningAgentRequired(context.Background())) {
		t.Fatal("the mark did not survive")
	}
	signing := &SpawnSessionRequest{ExtraMounts: []ContainerMount{{Source: "/s/agent.sock", Target: WorkspaceSigningAgentSocket}}}
	if !spawnNeedsSigningAgent(signing) {
		t.Fatal("a spawn mounting the signing socket needs the signing agent")
	}
	if spawnNeedsSigningAgent(&SpawnSessionRequest{ExtraMounts: []ContainerMount{{Source: "/d", Target: WorkspaceSpecDir}}}) {
		t.Fatal("a spawn mounting no signing socket needs none")
	}
	err := &SigningAgentUnavailableError{Backend: "claude", Cause: errors.New("no backend")}
	if !errors.Is(err, ErrSigningAgentUnavailable) || !strings.Contains(err.Error(), "claude") ||
		!strings.Contains(err.Error(), "no backend") {
		t.Fatalf("error = %v", err)
	}
	if bare := (&SigningAgentUnavailableError{Backend: "codex"}); !errors.Is(bare, ErrSigningAgentUnavailable) {
		t.Fatal("a causeless refusal must still unwrap to the sentinel")
	}
}

// TestReconcileAgentBackends_SigningTraits: a heartbeat that reports the list
// replaces the record's signing traits, an empty list clears them, and one
// that reports nothing keeps them — with the gpu trait kept throughout.
func TestReconcileAgentBackends_SigningTraits(t *testing.T) {
	t.Parallel()
	const oldID, newID = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"
	record := func() *AgentRegistration {
		return &AgentRegistration{Backend: "codex", Capabilities: []string{"codex", AgentTraitGPU, SigningAgentTrait(oldID)}}
	}
	cases := map[string]struct {
		signing []string
		want    []string
	}{
		"unreported keeps": {signing: nil, want: []string{"codex", AgentTraitGPU, SigningAgentTrait(oldID)}},
		"empty clears":     {signing: []string{}, want: []string{"codex", AgentTraitGPU}},
		"list replaces":    {signing: []string{newID}, want: []string{"codex", AgentTraitGPU, SigningAgentTrait(newID)}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			agent := record()
			reconcileAgentBackends(agent, &AgentObservation{Backends: []string{"codex"}, SigningAgents: tc.signing})
			if strings.Join(agent.Capabilities, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("capabilities = %v, want %v", agent.Capabilities, tc.want)
			}
		})
	}
	bad := &AgentObservation{Backends: []string{"codex"}, SigningAgents: []string{"not-an-id"}}
	if err := bad.Validate(); !errors.Is(err, ErrAgentRegistryInvalidInput) {
		t.Fatalf("a malformed signing identifier: %v, want refused", err)
	}
}

// TestSpawnSession_ASigningSpawnIsPlacedWhereTheSocketIs: a spawn that mounts
// the daemon's signing socket resolves with the requirement on its context,
// and one nothing can take — no capable agent, no in-process backend — is the
// typed refusal rather than a backend-not-found.
func TestSpawnSession_ASigningSpawnIsPlacedWhereTheSocketIs(t *testing.T) {
	t.Parallel()
	signingSpawn := func(backend string) *SpawnSessionRequest {
		return &SpawnSessionRequest{
			Backend:     backend,
			ExtraMounts: []ContainerMount{{Source: "/run/q/s1/agent.sock", Target: WorkspaceSigningAgentSocket, ReadOnly: true}},
		}
	}

	var required []bool
	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{}, &agentTestLogger{})
	if err != nil {
		t.Fatal(err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetRemoteBackendResolver(func(ctx context.Context, _, _ string) (AgentBackend, bool) {
		required = append(required, SigningAgentRequiredFromContext(ctx))
		return nil, false
	})

	_, err = impl.SpawnSession(tenantCtx("t1"), signingSpawn("claude"))
	var unavailable *SigningAgentUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Backend != "claude" {
		t.Fatalf("err = %v, want *SigningAgentUnavailableError", err)
	}
	if len(required) != 1 || !required[0] {
		t.Fatalf("the resolver was not told the spawn needs the signing socket: %v", required)
	}

	// An unsigned spawn carries no requirement and keeps the ordinary refusal.
	required = nil
	_, err = impl.SpawnSession(tenantCtx("t1"), &SpawnSessionRequest{Backend: "claude"})
	if errors.Is(err, ErrSigningAgentUnavailable) || err == nil {
		t.Fatalf("an unsigned spawn: %v, want the ordinary backend refusal", err)
	}
	if len(required) != 1 || required[0] {
		t.Fatalf("an unsigned spawn must not require the signing agent: %v", required)
	}
}
