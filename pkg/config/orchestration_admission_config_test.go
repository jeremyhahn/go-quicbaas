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

package config

import (
	"strings"
	"testing"
	"time"
)

// TestOrchestrationConfig_ReapLeaseTTL pins the default and the effective
// value when unset.
func TestOrchestrationConfig_ReapLeaseTTL(t *testing.T) {
	cfg := Default()
	if cfg.Orchestration.ReapLeaseTTL != DefaultOrchestrationReapLeaseTTL || DefaultOrchestrationReapLeaseTTL != 90*time.Second {
		t.Fatalf("default reap_lease_ttl = %s, want 90s", cfg.Orchestration.ReapLeaseTTL)
	}
	var zero OrchestrationConfig
	if zero.EffectiveReapLeaseTTL() != DefaultOrchestrationReapLeaseTTL {
		t.Fatal("an unset TTL takes the default")
	}
	if (OrchestrationConfig{ReapLeaseTTL: time.Minute}).EffectiveReapLeaseTTL() != time.Minute {
		t.Fatal("a set TTL is kept")
	}
}

// TestValidateOrchestration_ReapLeaseTTL proves a negative TTL is refused and
// zero is accepted as the default.
func TestValidateOrchestration_ReapLeaseTTL(t *testing.T) {
	v := NewValidator()
	v.validateOrchestration(&OrchestrationConfig{ReapLeaseTTL: -time.Second})
	if len(v.errors) != 1 || !strings.Contains(v.errors[0].Error(), "orchestration.reap_lease_ttl") {
		t.Fatalf("expected one reap_lease_ttl error, got %v", v.errors)
	}
	v = NewValidator()
	v.validateOrchestration(&OrchestrationConfig{})
	if len(v.errors) != 0 {
		t.Fatalf("zero must validate, got %v", v.errors)
	}
}

// TestValidateOrchestration_IdleThresholds proves an unknown state is refused
// with the valid set named, and that every watched state — including a
// negative off switch — is accepted.
func TestValidateOrchestration_IdleThresholds(t *testing.T) {
	v := NewValidator()
	v.validateOrchestration(&OrchestrationConfig{IdleThresholds: map[string]time.Duration{"merged": time.Hour}})
	if len(v.errors) != 1 {
		t.Fatalf("expected one error, got %v", v.errors)
	}
	msg := v.errors[0].Error()
	if !strings.Contains(msg, "orchestration.idle_thresholds.merged") || !strings.Contains(msg, "executing") {
		t.Fatalf("the refusal names the field and the valid set: %s", msg)
	}

	all := map[string]time.Duration{}
	for _, state := range OrchestrationWatchedJobStates {
		all[state] = -1
	}
	v = NewValidator()
	v.validateOrchestration(&OrchestrationConfig{IdleThresholds: all})
	if len(v.errors) != 0 {
		t.Fatalf("every watched state validates, got %v", v.errors)
	}
}

// =============================================================================
// Fleet capacity — the deployment-wide bound
// =============================================================================

// TestOrchestrationConfig_FleetCapacityDefaultsToUndeclared. The default is ZERO
// and that is a decision, not an omission: this tree cannot measure how many
// worker slots a deployment has, and a default that guessed would refuse work the
// fleet would have served while looking like a safety bound.
func TestOrchestrationConfig_FleetCapacityDefaultsToUndeclared(t *testing.T) {
	cfg := Default()
	if got := cfg.Orchestration.Fleet.MaxConcurrentJobs; got != 0 {
		t.Fatalf("default orchestration.fleet.max_concurrent_jobs = %d, want 0 (undeclared)", got)
	}
	var zero OrchestrationConfig
	if zero.Fleet.MaxConcurrentJobs != 0 {
		t.Fatal("the zero value must leave the fleet axis undeclared")
	}
}

// TestValidateOrchestration_FleetCapacity: zero is the documented default and a
// negative count is a number somebody wrote on purpose and got wrong — a bound
// below zero cannot be distinguished downstream from the undeclared case.
func TestValidateOrchestration_FleetCapacity(t *testing.T) {
	v := NewValidator()
	v.validateOrchestration(&OrchestrationConfig{
		Fleet: OrchestrationFleetConfig{MaxConcurrentJobs: -1},
	})
	if len(v.errors) != 1 || !strings.Contains(v.errors[0].Error(), "orchestration.fleet.max_concurrent_jobs") {
		t.Fatalf("expected one fleet capacity error naming the field, got %v", v.errors)
	}

	for _, capacity := range []int{0, 1, 8, 4096} {
		v = NewValidator()
		v.validateOrchestration(&OrchestrationConfig{
			Fleet: OrchestrationFleetConfig{MaxConcurrentJobs: capacity},
		})
		if len(v.errors) != 0 {
			t.Fatalf("capacity %d must validate, got %v", capacity, v.errors)
		}
	}
}
