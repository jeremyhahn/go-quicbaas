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
	"slices"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// pinningContainerRuntime is a Tier 0 runtime: it satisfies the same
// VerifyPinnedImage contract ExecContainerRuntime does, so the orchestrator
// credits it with digest pinning and nothing more.
type pinningContainerRuntime struct {
	*mockContainerRuntime
}

func (r *pinningContainerRuntime) VerifyPinnedImage(_ context.Context, _ *ContainerSpec, _ string) error {
	return nil
}

// attestingContainerRuntime advertises all three tiers, standing in for
// TPMAttestingRuntime without requiring a TPM.
type attestingContainerRuntime struct {
	*mockContainerRuntime
}

func (r *attestingContainerRuntime) TrustCapabilities() TrustCapabilities {
	return TrustCapabilities{DigestPinning: true, HostTPM: true, MeasuredLaunch: true}
}

func (r *attestingContainerRuntime) Attest(
	_ context.Context, spec *ContainerSpec, containerID string) (*ContainerAttestation, error) {
	return &ContainerAttestation{ContainerID: containerID, TrustLevel: spec.TrustLevel}, nil
}

// Compile-time interface checks.
var (
	_ pinVerifyingRuntime       = (*pinningContainerRuntime)(nil)
	_ AttestingContainerRuntime = (*attestingContainerRuntime)(nil)
	_ TenantTrustPolicyProvider = (*fixedTenantPolicies)(nil)
	_ TenantPolicyProvider      = (*fixedTenantPolicies)(nil)
)

// fixedTenantPolicies returns one pair of per-tenant overrides for every
// tenant. It implements both policy provider interfaces so the orchestrator's
// optional-interface probe is exercised.
type fixedTenantPolicies struct {
	sandbox *SandboxPolicy
	trust   *TrustPolicy
}

func (p *fixedTenantPolicies) SandboxPolicy(string) *SandboxPolicy { return p.sandbox }

func (p *fixedTenantPolicies) TrustPolicy(string) *TrustPolicy { return p.trust }

// newTrustHarness builds a SpawnSession fixture with the given server trust
// policy and container runtime, and records what reached the runtime and the
// backend.
func newTrustHarness(
	t *testing.T,
	policy TrustPolicy,
	runtime ContainerRuntime,
) (AgentService, *agentServiceImpl, *[]*SpawnRequest) {
	t.Helper()

	spawns := new([]*SpawnRequest)
	backend := &mockAgentBackend{
		spawnFn: func(_ context.Context, req *SpawnRequest) (*Session, error) {
			*spawns = append(*spawns, req)
			return &Session{
				ID:        "session-trust",
				Backend:   "mock",
				Status:    SessionStateActive,
				CreatedAt: 1,
			}, nil
		},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(),
		map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService error: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatal("agent service is not the package implementation")
	}
	impl.SetTrustPolicy(policy)
	if runtime != nil {
		impl.SetContainerRuntime(runtime)
	}
	return svc, impl, spawns
}

// TestSpawnSession_TrustPolicyFloorApplies is the regression test for the
// unreachable trust policy: with an operator-configured floor, a request that
// names NO level (every transport's default) must run at the floor rather than
// at no integrity guarantee at all.
func TestSpawnSession_TrustPolicyFloorApplies(t *testing.T) {
	t.Parallel()

	runtime := &pinningContainerRuntime{mockContainerRuntime: &mockContainerRuntime{available: true}}
	svc, _, spawns := newTrustHarness(t, TrustPolicyPinned, runtime)

	if _, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend: "mock",
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	if len(*spawns) != 1 {
		t.Fatalf("backend spawn calls = %d, want 1", len(*spawns))
	}
	if got := (*spawns)[0].TrustLevel; got != TrustLevelPinned {
		t.Fatalf("backend TrustLevel = %s, want %s (the configured floor)",
			got, TrustLevelPinned)
	}
}

// TestSpawnSession_TrustPolicyRejectsWeakerRequest proves a caller cannot opt
// out of the operator's floor by naming a weaker tier.
func TestSpawnSession_TrustPolicyRejectsWeakerRequest(t *testing.T) {
	t.Parallel()

	runtime := &pinningContainerRuntime{mockContainerRuntime: &mockContainerRuntime{available: true}}
	svc, _, spawns := newTrustHarness(t, TrustPolicyPinned, runtime)

	_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:    "mock",
		TrustLevel: TrustLevelNone,
	})
	var violation *TrustLevelViolationError
	if !errors.As(err, &violation) {
		t.Fatalf("expected TrustLevelViolationError, got %T: %v", err, err)
	}
	if violation.Requested != TrustLevelNone {
		t.Errorf("violation.Requested = %s, want %s", violation.Requested, TrustLevelNone)
	}
	if violation.Policy != TrustPolicyPinned {
		t.Errorf("violation.Policy = %s, want %s", violation.Policy, TrustPolicyPinned)
	}
	if len(*spawns) != 0 {
		t.Error("backend spawn was called despite the policy violation")
	}
}

// TestSpawnSession_TrustLevelUnprovable_Refused proves the tier is refused
// rather than silently downgraded when the host cannot prove it: a plain
// container runtime provides Tier 0 and nothing above it, and a runtime that
// cannot even verify a pin provides nothing.
func TestSpawnSession_TrustLevelUnprovable_Refused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		policy  TrustPolicy
		runtime ContainerRuntime
	}{
		{
			name:    "attested floor on a pin-only host",
			policy:  TrustPolicyAttested,
			runtime: &pinningContainerRuntime{mockContainerRuntime: &mockContainerRuntime{available: true}},
		},
		{
			name:    "pinned floor on a host that cannot verify a pin",
			policy:  TrustPolicyPinned,
			runtime: &mockContainerRuntime{available: true},
		},
		{
			name:    "pinned floor with no runtime at all",
			policy:  TrustPolicyPinned,
			runtime: nil,
		},
		{
			name:    "pinned floor with an unavailable runtime",
			policy:  TrustPolicyPinned,
			runtime: &pinningContainerRuntime{mockContainerRuntime: &mockContainerRuntime{available: false}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, _, spawns := newTrustHarness(t, tc.policy, tc.runtime)
			_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "mock"})
			var violation *TrustLevelViolationError
			if !errors.As(err, &violation) {
				t.Fatalf("expected TrustLevelViolationError, got %T: %v", err, err)
			}
			if len(*spawns) != 0 {
				t.Error("backend spawn was called despite the unprovable tier")
			}
		})
	}
}

// TestSpawnSession_TrustPolicyOptIn_Unchanged is the compatibility half: the
// default policy resolves to TrustLevelNone and refuses nothing, so a
// deployment that has never configured trusted computing behaves exactly as it
// did before the axis was wired.
func TestSpawnSession_TrustPolicyOptIn_Unchanged(t *testing.T) {
	t.Parallel()

	svc, _, spawns := newTrustHarness(t, TrustPolicyOptIn, nil)
	if _, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend: "mock",
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	if got := (*spawns)[0].TrustLevel; got != TrustLevelNone {
		t.Fatalf("backend TrustLevel = %s, want %s", got, TrustLevelNone)
	}
}

// TestSpawnSession_ResolvedTrustLevelOnContainerSpec proves the RESOLVED tier —
// not the caller's — reaches the container spec, including when the caller
// supplied a full spec naming a weaker one.
func TestSpawnSession_ResolvedTrustLevelOnContainerSpec(t *testing.T) {
	t.Parallel()

	base := &mockContainerRuntime{available: true, runID: "container-trust"}
	runtime := &attestingContainerRuntime{mockContainerRuntime: base}
	svc, impl, _ := newTrustHarness(t, TrustPolicyPinned, runtime)
	impl.SetMountPolicy(&MountPolicy{})

	if _, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		Sandboxed: true,
		ContainerConfig: &ContainerSpec{
			Image:      "ghcr.io/example/agent@" + testDigest,
			TrustLevel: TrustLevelNone,
		},
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	if len(base.runCalls) != 1 {
		t.Fatalf("container runtime run calls = %d, want 1", len(base.runCalls))
	}
	if got := base.runCalls[0].Spec.TrustLevel; got != TrustLevelPinned {
		t.Fatalf("container spec TrustLevel = %s, want the resolved %s", got, TrustLevelPinned)
	}
}

// TestSpawnSession_TrustLevelPersistedForResume proves the resolved tier is
// durable: a resume must not silently restart the agent at the zero value.
func TestSpawnSession_TrustLevelPersistedForResume(t *testing.T) {
	t.Parallel()

	runtime := &pinningContainerRuntime{mockContainerRuntime: &mockContainerRuntime{available: true}}
	store := NewInMemoryAgentStore()
	backend := &mockAgentBackend{}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService error: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatal("agent service is not the package implementation")
	}
	impl.SetTrustPolicy(TrustPolicyPinned)
	impl.SetContainerRuntime(runtime)

	resp, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "mock"})
	if err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	record, err := store.GetSession(context.Background(), SystemTenantID, resp.SessionID)
	if err != nil {
		t.Fatalf("GetSession error: %v", err)
	}
	if record.TrustLevel != TrustLevelPinned {
		t.Fatalf("record.TrustLevel = %s, want %s", record.TrustLevel, TrustLevelPinned)
	}
	if replayed := spawnRequestFromRecord(record); replayed.TrustLevel != TrustLevelPinned {
		t.Fatalf("replayed TrustLevel = %s, want %s", replayed.TrustLevel, TrustLevelPinned)
	}
}

// TestResolveTrustLevel_TenantOverrideOnlyTightens is the policy-precedence
// contract: a tenant record may raise the operator's floor and may never lower
// it.
func TestResolveTrustLevel_TenantOverrideOnlyTightens(t *testing.T) {
	t.Parallel()

	caps := TrustCapabilities{DigestPinning: true, HostTPM: true, MeasuredLaunch: true}
	optIn := TrustPolicyOptIn
	measured := TrustPolicyMeasured

	// A weaker tenant override is ignored: the server floor still applies.
	level, err := ResolveTrustLevel(TrustLevelDefault, TrustPolicyAttested, &optIn, caps)
	if err != nil {
		t.Fatalf("ResolveTrustLevel error: %v", err)
	}
	if level != TrustLevelAttested {
		t.Fatalf("level = %s, want the server floor %s", level, TrustLevelAttested)
	}
	if _, err := ResolveTrustLevel(TrustLevelNone, TrustPolicyAttested, &optIn, caps); err == nil {
		t.Fatal("a weaker override must not let a request drop below the server floor")
	}

	// A stricter tenant override raises the floor.
	level, err = ResolveTrustLevel(TrustLevelDefault, TrustPolicyOptIn, &measured, caps)
	if err != nil {
		t.Fatalf("ResolveTrustLevel error: %v", err)
	}
	if level != TrustLevelMeasured {
		t.Fatalf("level = %s, want the tenant floor %s", level, TrustLevelMeasured)
	}
}

// TestResolveSandboxMode_TenantOverrideOnlyTightens is the same contract on the
// sandbox axis.
func TestResolveSandboxMode_TenantOverrideOnlyTightens(t *testing.T) {
	t.Parallel()

	optIn := SandboxPolicyOptIn
	required := SandboxPolicyRequired

	// A weaker override cannot buy raw exec out of a Required deployment.
	if _, err := ResolveSandboxMode(SandboxModeNone, SandboxPolicyRequired, &optIn, true); err == nil {
		t.Fatal("a weaker override must not permit raw exec under a required policy")
	}
	mode, err := ResolveSandboxMode(SandboxModeDefault, SandboxPolicyRequired, &optIn, true)
	if err != nil {
		t.Fatalf("ResolveSandboxMode error: %v", err)
	}
	if mode != SandboxModeRequired {
		t.Fatalf("mode = %s, want %s", mode, SandboxModeRequired)
	}

	// A stricter override still tightens an opt-in deployment.
	if _, err := ResolveSandboxMode(SandboxModeNone, SandboxPolicyOptIn, &required, true); err == nil {
		t.Fatal("a stricter override must forbid raw exec")
	}
}

// TestSpawnSession_TenantTrustOverrideTightens proves the optional
// TenantTrustPolicyProvider is actually consulted at the spawn seam.
func TestSpawnSession_TenantTrustOverrideTightens(t *testing.T) {
	t.Parallel()

	runtime := &pinningContainerRuntime{mockContainerRuntime: &mockContainerRuntime{available: true}}
	svc, impl, spawns := newTrustHarness(t, TrustPolicyOptIn, runtime)
	pinned := TrustPolicyPinned
	impl.SetTenantPolicyProvider(&fixedTenantPolicies{trust: &pinned})

	if _, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend: "mock",
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	if got := (*spawns)[0].TrustLevel; got != TrustLevelPinned {
		t.Fatalf("backend TrustLevel = %s, want the tenant floor %s", got, TrustLevelPinned)
	}
}

// TestTrustCapabilities_BackendRuntimeWins checks that an ACP backend holding
// its own container runtime answers for the host, since it is the component
// that launches the agent container.
func TestTrustCapabilities_BackendRuntimeWins(t *testing.T) {
	t.Parallel()

	svc, impl, _ := newTrustHarness(t, TrustPolicyPinned, nil)
	if svc == nil {
		t.Fatal("nil service")
	}
	acp := NewACPBackend(ACPBackendConfig{Command: "true"})
	acp.SetContainerRuntime(&pinningContainerRuntime{
		mockContainerRuntime: &mockContainerRuntime{available: true},
	})

	if caps := impl.trustCapabilities(acp); !caps.DigestPinning {
		t.Fatal("expected the backend's own runtime to supply digest pinning")
	}
	// A backend with no runtime falls back to the orchestrator's, which is nil
	// here, so nothing is claimed.
	if caps := impl.trustCapabilities(&mockAgentBackend{}); caps.DigestPinning {
		t.Fatal("expected no capability with no runtime wired")
	}
}

// TestTrustPolicyVocabularyMatchesConfig pins the two restatements of the
// trust-policy vocabulary together. pkg/config cannot import this package, so
// it lists the accepted orchestration.trust_policy values itself; a policy
// added on either side and not the other would be valid to one and rejected by
// the other.
func TestTrustPolicyVocabularyMatchesConfig(t *testing.T) {
	t.Parallel()

	// Every value the config accepts parses here, and round-trips.
	for _, name := range config.OrchestrationTrustPolicies {
		policy, err := ParseTrustPolicy(name)
		if err != nil {
			t.Fatalf("ParseTrustPolicy(%q) error = %v; config accepts a policy this package does not",
				name, err)
		}
		if policy.String() != name {
			t.Errorf("ParseTrustPolicy(%q).String() = %q, want the same canonical name",
				name, policy.String())
		}
	}

	// Every policy this package defines is a value the config accepts.
	for policy := TrustPolicyOptIn; policy <= TrustPolicyMeasured; policy++ {
		if !slices.Contains(config.OrchestrationTrustPolicies, policy.String()) {
			t.Errorf("trust policy %q is missing from config.OrchestrationTrustPolicies",
				policy.String())
		}
	}
}
