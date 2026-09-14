// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

// TestOrchestrationConfig_SeamDefaults pins the shipped defaults for the
// task-source sweep, the command allowlist and the per-moment bounds. The
// bounds are pinned to the engine's own defaults layer from the engine's
// side (pkg/orchestration, TestStageTimeouts_ConfigDefaultsMatchEngine),
// which this package cannot import without a cycle.
func TestOrchestrationConfig_SeamDefaults(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	o := cfg.Orchestration

	if o.SweepInterval != 30*time.Second || o.EffectiveSweepInterval() != 30*time.Second {
		t.Fatalf("sweep_interval default = %v (effective %v), want 30s", o.SweepInterval, o.EffectiveSweepInterval())
	}
	want := []string{"make test", "make build", "make lint", "make security"}
	if strings.Join(o.VerificationCommands, "|") != strings.Join(want, "|") {
		t.Fatalf("verification_commands default = %v, want %v", o.VerificationCommands, want)
	}
	got := o.StageTimeouts
	if got.Prepare != 10*time.Minute || got.Reviewable != 10*time.Minute || got.Complete != 10*time.Minute ||
		got.Cleanup != 2*time.Minute || got.Blocked != time.Minute {
		t.Fatalf("stage_timeouts default = %+v", got)
	}
}

// TestOrchestrationConfig_EffectiveSweepInterval: zero means unset and takes
// the default; a configured value is honoured.
func TestOrchestrationConfig_EffectiveSweepInterval(t *testing.T) {
	t.Parallel()
	var unset OrchestrationConfig
	if unset.EffectiveSweepInterval() != DefaultOrchestrationSweepInterval {
		t.Fatalf("unset sweep interval = %v, want the default", unset.EffectiveSweepInterval())
	}
	set := OrchestrationConfig{SweepInterval: 5 * time.Second}
	if set.EffectiveSweepInterval() != 5*time.Second {
		t.Fatalf("configured sweep interval = %v, want 5s", set.EffectiveSweepInterval())
	}
}

// TestValidateOrchestration_Seams refuses a negative sweep interval, an
// empty allowlist entry and a negative stage bound, and accepts zero bounds
// as "take the default".
func TestValidateOrchestration_Seams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*OrchestrationConfig)
		wantError string
	}{
		{
			name:   "zero sweep interval takes the default",
			mutate: func(c *OrchestrationConfig) { c.SweepInterval = 0 },
		},
		{
			name:      "negative sweep interval is refused",
			mutate:    func(c *OrchestrationConfig) { c.SweepInterval = -time.Second },
			wantError: "orchestration.sweep_interval",
		},
		{
			name:   "an explicit allowlist is valid",
			mutate: func(c *OrchestrationConfig) { c.VerificationCommands = []string{"make test"} },
		},
		{
			name:      "an empty allowlist entry is refused",
			mutate:    func(c *OrchestrationConfig) { c.VerificationCommands = []string{"make test", "  "} },
			wantError: "orchestration.verification_commands[1]",
		},
		{
			name:   "zero stage bounds take the default",
			mutate: func(c *OrchestrationConfig) { c.StageTimeouts = OrchestrationStageTimeouts{} },
		},
		{
			name:      "a negative stage bound is refused",
			mutate:    func(c *OrchestrationConfig) { c.StageTimeouts.Cleanup = -time.Minute },
			wantError: "orchestration.stage_timeouts.cleanup",
		},
		{
			name:   "an absolute agent config dir is valid",
			mutate: func(c *OrchestrationConfig) { c.AgentConfigDir = "/var/lib/quicbaas/agents/claude" },
		},
		{
			name:      "a relative agent config dir is refused",
			mutate:    func(c *OrchestrationConfig) { c.AgentConfigDir = "agents/claude" },
			wantError: "orchestration.agent_config_dir",
		},
		{
			name:      "a negative prepare bound is refused",
			mutate:    func(c *OrchestrationConfig) { c.StageTimeouts.Prepare = -time.Second },
			wantError: "orchestration.stage_timeouts.prepare",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultConfig().Orchestration
			tt.mutate(&cfg)
			v := NewValidator()
			v.validateOrchestration(&cfg)
			if tt.wantError == "" {
				if len(v.errors) != 0 {
					t.Fatalf("expected no validation errors, got %v", v.errors)
				}
				return
			}
			if len(v.errors) == 0 {
				t.Fatalf("expected a validation error on %s, got none", tt.wantError)
			}
			if v.errors[0].Field != tt.wantError {
				t.Fatalf("expected the error on %s, got %v", tt.wantError, v.errors)
			}
		})
	}
}

// TestValidateOrchestrationStageTimeouts_ReportsEveryBadBoundSorted: two
// bad bounds are both reported, in a stable order.
func TestValidateOrchestrationStageTimeouts_ReportsEveryBadBoundSorted(t *testing.T) {
	t.Parallel()
	v := NewValidator()
	v.validateOrchestrationStageTimeouts(OrchestrationStageTimeouts{Reviewable: -1, Blocked: -1})
	if len(v.errors) != 2 {
		t.Fatalf("expected 2 errors, got %v", v.errors)
	}
	if v.errors[0].Field != "orchestration.stage_timeouts.blocked" || v.errors[1].Field != "orchestration.stage_timeouts.reviewable" {
		t.Fatalf("expected blocked then reviewable, got %v", v.errors)
	}
}
