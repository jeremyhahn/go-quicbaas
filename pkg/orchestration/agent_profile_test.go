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
