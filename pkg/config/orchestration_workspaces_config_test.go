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
)

func TestOrchestrationConfig_EffectiveProjectsRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		projectsRoot string
		dataDir      string
		want         string
	}{
		{
			name:         "explicit root wins",
			projectsRoot: "/srv/projects",
			dataDir:      "/var/lib/quicbaas",
			want:         "/srv/projects",
		},
		{
			name:    "empty derives from data dir",
			dataDir: "/var/lib/quicbaas",
			want:    "/var/lib/quicbaas/projects",
		},
		{
			name: "empty root and empty data dir",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := OrchestrationConfig{ProjectsRoot: tt.projectsRoot}
			if got := cfg.EffectiveProjectsRoot(tt.dataDir); got != tt.want {
				t.Errorf("EffectiveProjectsRoot(%q) = %q, want %q", tt.dataDir, got, tt.want)
			}
		})
	}
}

func TestValidateOrchestration_ProjectsRootAndMountRoots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*OrchestrationConfig)
		wantError string
	}{
		{
			name:   "absolute projects root is valid",
			mutate: func(c *OrchestrationConfig) { c.ProjectsRoot = "/srv/projects" },
		},
		{
			name:   "empty projects root is valid",
			mutate: func(c *OrchestrationConfig) { c.ProjectsRoot = "" },
		},
		{
			name:      "relative projects root is rejected",
			mutate:    func(c *OrchestrationConfig) { c.ProjectsRoot = "relative/projects" },
			wantError: "orchestration.projects_root",
		},
		{
			name: "absolute mount roots are valid",
			mutate: func(c *OrchestrationConfig) {
				c.Sandbox.AllowedMountRoots = []string{"/data/shared", "/opt/models"}
			},
		},
		{
			name: "relative mount root is rejected",
			mutate: func(c *OrchestrationConfig) {
				c.Sandbox.AllowedMountRoots = []string{"/data/shared", "not/absolute"}
			},
			wantError: "orchestration.sandbox.allowed_mount_roots[1]",
		},
		{
			name: "empty mount root is rejected",
			mutate: func(c *OrchestrationConfig) {
				c.Sandbox.AllowedMountRoots = []string{""}
			},
			wantError: "orchestration.sandbox.allowed_mount_roots[0]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := OrchestrationConfig{}
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
			found := false
			for _, e := range v.errors {
				if strings.Contains(e.Field, tt.wantError) {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected validation error on field %q, got %v", tt.wantError, v.errors)
			}
		})
	}
}

// TestValidateOrchestration_TrustPolicy pins the trust-policy vocabulary. An
// unrecognized value must FAIL the config: the fallback is the weakest policy,
// so a typo would silently disable the integrity floor the operator configured.
// It also guards the restatement of orchestration.ParseTrustPolicy's vocabulary
// in this package — a policy added there and not here would be rejected as a
// config value while being perfectly valid to the orchestrator.
func TestValidateOrchestration_TrustPolicy(t *testing.T) {
	t.Parallel()

	valid := append([]string{""}, OrchestrationTrustPolicies...)
	for _, policy := range valid {
		t.Run("valid/"+policy, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.validateOrchestration(&OrchestrationConfig{TrustPolicy: policy})
			if len(v.errors) != 0 {
				t.Fatalf("trust_policy %q: expected no validation errors, got %v", policy, v.errors)
			}
		})
	}

	for _, policy := range []string{"attest", "PINNED", "none", "opt in", "measured "} {
		t.Run("invalid/"+policy, func(t *testing.T) {
			t.Parallel()
			v := NewValidator()
			v.validateOrchestration(&OrchestrationConfig{TrustPolicy: policy})
			if len(v.errors) == 0 {
				t.Fatalf("trust_policy %q: expected a validation error, got none", policy)
			}
			if !strings.Contains(v.errors[0].Field, "orchestration.trust_policy") {
				t.Fatalf("expected the error on orchestration.trust_policy, got %v", v.errors)
			}
		})
	}
}
