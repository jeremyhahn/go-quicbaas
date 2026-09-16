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
	"strings"
	"testing"
)

// fixedLoad reports a canned live-job count per backend.
type fixedLoad struct {
	live map[string]int
	err  error
}

func (f fixedLoad) LiveJobsForBackend(_ context.Context, _, backend string) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.live[backend], nil
}

func testCatalogue() ProfileCatalogue {
	return ProfileCatalogue{
		Profiles: map[string]AgentProfile{
			"npu-qwen": {
				Name: "npu-qwen", Runtime: "openvino", Model: "qwen3-8b", MaxConcurrent: 2,
				Provider: ProviderEndpoint{Name: "ovms", BaseURL: "http://gpu:9000/v1", WireAPI: "chat"},
			},
			"codex-hosted": {
				Name: "codex-hosted", Runtime: "codex", Model: "gpt-5.5",
			},
		},
		Pools: map[string]AgentPool{
			"triage": {Name: "triage", Profiles: []string{"npu-qwen", "codex-hosted"}},
		},
	}
}

// TestProfileResolver_PrefersTheFirstWithCapacity is the routing policy: order
// is the preference, and load only decides when a profile is full.
//
// LEAST-LOADED WOULD INVERT THE POLICY. Picking the emptiest profile sends work
// to the metered provider the moment the local one has any load at all, which
// is the opposite of what "prefer the local model" means.
func TestProfileResolver_PrefersTheFirstWithCapacity(t *testing.T) {
	r := NewProfileResolver(testCatalogue(), fixedLoad{live: map[string]int{"openvino": 1}})
	got, err := r.Resolve(context.Background(), "acme", "triage")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "npu-qwen" {
		t.Fatalf("with the preferred profile at 1/2 it must still win, got %q", got.Name)
	}
}

// TestProfileResolver_SpillsWhenThePreferredIsFull is the answer to a
// saturated accelerator: the queue moves instead of waiting.
func TestProfileResolver_SpillsWhenThePreferredIsFull(t *testing.T) {
	r := NewProfileResolver(testCatalogue(), fixedLoad{live: map[string]int{"openvino": 2}})
	got, err := r.Resolve(context.Background(), "acme", "triage")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "codex-hosted" {
		t.Fatalf("a full preferred profile must spill to the next, got %q", got.Name)
	}
	// An unbounded profile is never full, however loaded the fleet.
	r2 := NewProfileResolver(testCatalogue(), fixedLoad{live: map[string]int{"openvino": 99, "codex": 99}})
	if got, err := r2.Resolve(context.Background(), "acme", "triage"); err != nil || got.Name != "codex-hosted" {
		t.Fatalf("MaxConcurrent 0 means unbounded: got %q err=%v", got.Name, err)
	}
}

// TestProfileResolver_ExhaustedPoolRefusesAndSaysWhatWasFull keeps a pool from
// becoming decoration: spawning onto a saturated profile anyway would queue the
// work behind the cap with no reason recorded.
func TestProfileResolver_ExhaustedPoolRefusesAndSaysWhatWasFull(t *testing.T) {
	cat := testCatalogue()
	hosted := cat.Profiles["codex-hosted"]
	hosted.MaxConcurrent = 3
	cat.Profiles["codex-hosted"] = hosted

	r := NewProfileResolver(cat, fixedLoad{live: map[string]int{"openvino": 2, "codex": 3}})
	_, err := r.Resolve(context.Background(), "acme", "triage")
	var exhausted *ProfilePoolExhaustedError
	if !errors.As(err, &exhausted) {
		t.Fatalf("want ProfilePoolExhaustedError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "npu-qwen (2/2)") || !strings.Contains(err.Error(), "codex-hosted (3/3)") {
		t.Fatalf("the refusal must name each full profile and its numbers: %s", err.Error())
	}
}

// TestProfileResolver_ANameIsAPoolOfOne means a caller wanting exactly one
// profile needs no second mechanism.
func TestProfileResolver_ANameIsAPoolOfOne(t *testing.T) {
	r := NewProfileResolver(testCatalogue(), fixedLoad{})
	got, err := r.Resolve(context.Background(), "acme", "codex-hosted")
	if err != nil || got.Name != "codex-hosted" {
		t.Fatalf("naming a profile directly must resolve it: %q err=%v", got.Name, err)
	}
	_, err = r.Resolve(context.Background(), "acme", "nonexistent")
	var missing *ProfileNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("want ProfileNotFoundError, got %T: %v", err, err)
	}
}

// TestProfileResolver_NilLoadReaderLooksIdle is the degradation for a
// deployment with no lease store: preference order still holds and the
// concurrency cap still refuses what would overrun.
func TestProfileResolver_NilLoadReaderLooksIdle(t *testing.T) {
	r := NewProfileResolver(testCatalogue(), nil)
	got, err := r.Resolve(context.Background(), "acme", "triage")
	if err != nil || got.Name != "npu-qwen" {
		t.Fatalf("a nil load reader must resolve the preferred profile: %q err=%v", got.Name, err)
	}
}

// TestAgentProfile_SpawnArgsCarryTheProviderPerRuntime pins the dialect that
// decided this design: codex REFUSES wire_api "chat", and the endpoint cannot
// be probed for which it speaks.
func TestAgentProfile_SpawnArgsCarryTheProviderPerRuntime(t *testing.T) {
	p := AgentProfile{
		Runtime: "codex", Model: "qwen3-8b",
		Provider: ProviderEndpoint{Name: "local", BaseURL: "http://gpu:11434/v1", WireAPI: "responses"},
	}
	args := strings.Join(p.SpawnArgs(), " ")
	for _, want := range []string{`model_provider="local"`, `base_url="http://gpu:11434/v1"`,
		`wire_api="responses"`, "requires_openai_auth=false"} {
		if !strings.Contains(args, want) {
			t.Fatalf("argv %q is missing %q", args, want)
		}
	}
	// A hosted profile names no endpoint and must add nothing: argv a CLI does
	// not expect is a startup failure, not a warning.
	if got := (AgentProfile{Runtime: "codex", Model: "gpt-5.5"}).SpawnArgs(); got != nil {
		t.Fatalf("a profile with no provider must add no argv, got %v", got)
	}
	// A runtime whose dialect this does not know gets nothing rather than
	// codex's flags.
	if got := (AgentProfile{Runtime: "claude", Provider: ProviderEndpoint{BaseURL: "http://x/v1", WireAPI: "chat"}}).SpawnArgs(); got != nil {
		t.Fatalf("an unknown runtime must add no argv, got %v", got)
	}
}

// TestAgentProfile_ValidateRefusesAnEndpointWithNoDialect covers the
// configuration mistake that fails at first use rather than at load.
func TestAgentProfile_ValidateRefusesAnEndpointWithNoDialect(t *testing.T) {
	p := AgentProfile{Name: "p", Runtime: "codex", Provider: ProviderEndpoint{BaseURL: "http://gpu:9000/v1"}}
	err := p.Validate()
	if err == nil || !strings.Contains(err.Error(), "dialect") {
		t.Fatalf("an endpoint with no wire_api must be refused at validation: %v", err)
	}
	// The same profile with a dialect is fine, and so is one with no endpoint.
	p.Provider.WireAPI = "chat"
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := (AgentProfile{Name: "h", Runtime: "claude"}).Validate(); err != nil {
		t.Fatalf("a hosted profile needs no endpoint: %v", err)
	}
}

// stubDemand answers what each role has queued.
type stubDemand struct {
	queued map[Role]int
	err    error
}

func (s stubDemand) QueuedForRole(_ context.Context, _ string, role Role) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.queued[role], nil
}

// bigCard is the scarce accelerator: held for review, shared when review is
// idle. small is the always-available fallback.
func rolePolicyCatalogue() ProfileCatalogue {
	return ProfileCatalogue{
		Profiles: map[string]AgentProfile{
			"bigcard": {
				Name: "bigcard", Runtime: "ollama", Model: "review-q4", MaxConcurrent: 2,
				Roles: RolePolicy{Prefer: RoleReview},
			},
			"small": {Name: "small", Runtime: "openvino", Model: "qwen3-8b", MaxConcurrent: 1},
		},
		Pools: map[string]AgentPool{
			"any": {Name: "any", Profiles: []string{"bigcard", "small"}},
		},
	}
}

// TestResolveForRole_IdleCapacityIsSharedNotReserved is the behaviour the
// owner asked for in so many words: prefer one role, but help out when that
// role's queue is empty. Reserving unconditionally would trade one starvation
// for another and idle the hardware.
func TestResolveForRole_IdleCapacityIsSharedNotReserved(t *testing.T) {
	resolver := NewProfileResolver(rolePolicyCatalogue(), nil).
		WithRoleDemand(stubDemand{queued: map[Role]int{RoleReview: 0}})

	got, err := resolver.ResolveForRole(context.Background(), "t", "any", RoleTriage)
	if err != nil {
		t.Fatalf("ResolveForRole: %v", err)
	}
	if got.Name != "bigcard" {
		t.Fatalf("profile = %q, want triage to use the idle preferred card", got.Name)
	}
}

// TestResolveForRole_AQueuedPreferredRoleTakesItsCardBack: the moment review
// has work waiting, triage must spill to the next profile rather than occupy
// the card the operator set aside.
func TestResolveForRole_AQueuedPreferredRoleTakesItsCardBack(t *testing.T) {
	resolver := NewProfileResolver(rolePolicyCatalogue(), nil).
		WithRoleDemand(stubDemand{queued: map[Role]int{RoleReview: 3}})

	got, err := resolver.ResolveForRole(context.Background(), "t", "any", RoleTriage)
	if err != nil {
		t.Fatalf("ResolveForRole: %v", err)
	}
	if got.Name != "small" {
		t.Fatalf("profile = %q, want triage to spill off the contended card", got.Name)
	}
	// The preferred role itself is never yielded to anybody.
	got, err = resolver.ResolveForRole(context.Background(), "t", "any", RoleReview)
	if err != nil {
		t.Fatalf("ResolveForRole(review): %v", err)
	}
	if got.Name != "bigcard" {
		t.Fatalf("profile = %q, want review on the card held for it", got.Name)
	}
}

// TestResolveForRole_ExclusiveRefusesEvenAnEmptyQueue: the escape hatch for a
// profile that genuinely cannot share, and it must not consult demand at all.
func TestResolveForRole_ExclusiveRefusesEvenAnEmptyQueue(t *testing.T) {
	catalogue := rolePolicyCatalogue()
	big := catalogue.Profiles["bigcard"]
	big.Roles = RolePolicy{Prefer: RoleReview, Exclusive: true}
	catalogue.Profiles["bigcard"] = big

	resolver := NewProfileResolver(catalogue, nil).
		WithRoleDemand(stubDemand{queued: map[Role]int{RoleReview: 0}})

	got, err := resolver.ResolveForRole(context.Background(), "t", "any", RoleTriage)
	if err != nil {
		t.Fatalf("ResolveForRole: %v", err)
	}
	if got.Name != "small" {
		t.Fatalf("profile = %q, want an exclusive card refused even when idle", got.Name)
	}
}

// TestResolveForRole_ReserveHoldsSlotsForLatency: a reservation is about
// RESPONSE TIME, so it holds slots empty whatever the queue says.
func TestResolveForRole_ReserveHoldsSlotsForLatency(t *testing.T) {
	catalogue := rolePolicyCatalogue()
	big := catalogue.Profiles["bigcard"]
	big.Roles = RolePolicy{Prefer: RoleReview, Reserve: 1} // 2 slots, 1 held
	catalogue.Profiles["bigcard"] = big

	// One job already running: the only free slot is the reserved one.
	resolver := NewProfileResolver(catalogue, fixedLoad{live: map[string]int{"ollama": 1}}).
		WithRoleDemand(stubDemand{queued: map[Role]int{RoleReview: 0}})

	got, err := resolver.ResolveForRole(context.Background(), "t", "any", RoleTriage)
	if err != nil {
		t.Fatalf("ResolveForRole: %v", err)
	}
	if got.Name != "small" {
		t.Fatalf("profile = %q, want the reserved slot withheld from triage", got.Name)
	}
}

// TestResolveForRole_AnUnreadableQueueAdmitsTheWork: refusing on a failed read
// would stop the fleet on a storage blip, and the concurrency cap still bounds
// what actually starts.
func TestResolveForRole_AnUnreadableQueueAdmitsTheWork(t *testing.T) {
	resolver := NewProfileResolver(rolePolicyCatalogue(), nil).
		WithRoleDemand(stubDemand{err: errors.New("store unavailable")})

	got, err := resolver.ResolveForRole(context.Background(), "t", "any", RoleTriage)
	if err != nil {
		t.Fatalf("ResolveForRole: %v", err)
	}
	if got.Name != "bigcard" {
		t.Fatalf("profile = %q, want an unreadable queue to admit rather than idle the fleet", got.Name)
	}
}

// TestResolveForRole_NoPolicyBehavesExactlyAsBefore pins the compatibility
// claim: a catalogue with no role policy must not change who gets what.
func TestResolveForRole_NoPolicyBehavesExactlyAsBefore(t *testing.T) {
	catalogue := rolePolicyCatalogue()
	big := catalogue.Profiles["bigcard"]
	big.Roles = RolePolicy{}
	catalogue.Profiles["bigcard"] = big

	resolver := NewProfileResolver(catalogue, nil).
		WithRoleDemand(stubDemand{queued: map[Role]int{RoleReview: 99}})

	for _, role := range []Role{RoleTriage, RoleReview, RoleDelivery, ""} {
		got, err := resolver.ResolveForRole(context.Background(), "t", "any", role)
		if err != nil {
			t.Fatalf("ResolveForRole(%q): %v", role, err)
		}
		if got.Name != "bigcard" {
			t.Fatalf("role %q got %q, want the first profile regardless of role", role, got.Name)
		}
	}
}

// TestRolePolicy_ValidateRefusesTermsWithNoSubject: reserving or excluding
// without naming a role is a policy that cannot mean anything.
func TestRolePolicy_ValidateRefusesTermsWithNoSubject(t *testing.T) {
	if err := (RolePolicy{Reserve: 2}).Validate(); err == nil {
		t.Fatal("a reservation with no preferred role was accepted")
	}
	if err := (RolePolicy{Exclusive: true}).Validate(); err == nil {
		t.Fatal("exclusivity with no preferred role was accepted")
	}
	if err := (RolePolicy{Prefer: RoleReview, Reserve: -1}).Validate(); err == nil {
		t.Fatal("a negative reservation was accepted")
	}
	if err := (RolePolicy{}).Validate(); err != nil {
		t.Fatalf("the zero value must be valid: %v", err)
	}
}
