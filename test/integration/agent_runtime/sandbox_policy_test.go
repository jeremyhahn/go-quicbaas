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

//go:build integration && agent_runtime && legacy_enrollment_token

package agent_runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestIntegration_SandboxPolicy is a table-driven test that drives every
// combination of:
//
//	3 daemon sandbox policies × 3 request modes × 2 tenant-override options × 2 runtime-availability states
//
// = 36 combinations (some produce identical outcomes and validate the OR of paths).
//
// For each combination the test asserts either:
//   - The spawn succeeds and the resolved SandboxMode matches the expected value.
//   - The spawn fails with *orchestration.SandboxModeViolationError.
func TestIntegration_SandboxPolicy(t *testing.T) {
	req := func(mode orchestration.SandboxMode) *orchestration.SpawnRequest {
		return &orchestration.SpawnRequest{
			WorkDir:        t.TempDir(),
			SandboxMode:    mode,
			ContainerImage: "ghcr.io/test/acp:latest", // pre-set so image lookup doesn't fail
		}
	}

	type tc struct {
		name           string
		daemonPolicy   orchestration.SandboxPolicy
		requestMode    orchestration.SandboxMode
		tenantOverride *orchestration.SandboxPolicy
		hasContainerRT bool

		// wantErr is true when a SandboxModeViolationError is expected.
		wantErr bool
		// wantMode is the resolved mode expected on success.
		wantMode orchestration.SandboxMode
	}

	policyOptIn := orchestration.SandboxPolicyOptIn
	policyPref := orchestration.SandboxPolicyPreferred
	policyRequired := orchestration.SandboxPolicyRequired

	cases := []tc{
		// ── SandboxPolicyOptIn ──────────────────────────────────────────────
		{
			name:         "optin/default/no-override/no-rt",
			daemonPolicy: policyOptIn, requestMode: orchestration.SandboxModeDefault,
			tenantOverride: nil, hasContainerRT: false,
			wantErr: false, wantMode: orchestration.SandboxModeNone,
		},
		{
			name:         "optin/default/no-override/has-rt",
			daemonPolicy: policyOptIn, requestMode: orchestration.SandboxModeDefault,
			tenantOverride: nil, hasContainerRT: true,
			wantErr: false, wantMode: orchestration.SandboxModeNone,
		},
		{
			name:         "optin/required/no-override/has-rt",
			daemonPolicy: policyOptIn, requestMode: orchestration.SandboxModeRequired,
			tenantOverride: nil, hasContainerRT: true,
			wantErr: false, wantMode: orchestration.SandboxModeRequired,
		},
		{
			name:         "optin/required/no-override/no-rt",
			daemonPolicy: policyOptIn, requestMode: orchestration.SandboxModeRequired,
			tenantOverride: nil, hasContainerRT: false,
			wantErr: true, // no container runtime → violation
		},
		{
			name:         "optin/none/no-override/no-rt",
			daemonPolicy: policyOptIn, requestMode: orchestration.SandboxModeNone,
			tenantOverride: nil, hasContainerRT: false,
			wantErr: false, wantMode: orchestration.SandboxModeNone,
		},
		// tenant override tightens to Required
		{
			name:         "optin/default/tenant-required/has-rt",
			daemonPolicy: policyOptIn, requestMode: orchestration.SandboxModeDefault,
			tenantOverride: &policyRequired, hasContainerRT: true,
			wantErr: false, wantMode: orchestration.SandboxModeRequired,
		},
		{
			name:         "optin/none/tenant-required/has-rt",
			daemonPolicy: policyOptIn, requestMode: orchestration.SandboxModeNone,
			tenantOverride: &policyRequired, hasContainerRT: true,
			wantErr: true, // override requires sandbox; NONE is forbidden
		},

		// ── SandboxPolicyPreferred ──────────────────────────────────────────
		{
			name:         "preferred/default/no-override/has-rt",
			daemonPolicy: policyPref, requestMode: orchestration.SandboxModeDefault,
			tenantOverride: nil, hasContainerRT: true,
			wantErr: false, wantMode: orchestration.SandboxModeRequired,
		},
		{
			name:         "preferred/default/no-override/no-rt",
			daemonPolicy: policyPref, requestMode: orchestration.SandboxModeDefault,
			tenantOverride: nil, hasContainerRT: false,
			// Preferred falls back to None when no runtime available.
			wantErr: false, wantMode: orchestration.SandboxModeNone,
		},
		{
			name:         "preferred/required/no-override/has-rt",
			daemonPolicy: policyPref, requestMode: orchestration.SandboxModeRequired,
			tenantOverride: nil, hasContainerRT: true,
			wantErr: false, wantMode: orchestration.SandboxModeRequired,
		},
		{
			name:         "preferred/none/no-override/has-rt",
			daemonPolicy: policyPref, requestMode: orchestration.SandboxModeNone,
			tenantOverride: nil, hasContainerRT: true,
			wantErr: false, wantMode: orchestration.SandboxModeNone,
		},
		{
			name:         "preferred/default/tenant-optin/has-rt",
			daemonPolicy: policyPref, requestMode: orchestration.SandboxModeDefault,
			tenantOverride: &policyOptIn, hasContainerRT: true,
			// Tenant override drops policy to OptIn → default resolves to None.
			wantErr: false, wantMode: orchestration.SandboxModeNone,
		},
		{
			name:         "preferred/none/tenant-required/has-rt",
			daemonPolicy: policyPref, requestMode: orchestration.SandboxModeNone,
			tenantOverride: &policyRequired, hasContainerRT: true,
			wantErr: true, // tenant required policy; NONE forbidden
		},

		// ── SandboxPolicyRequired ───────────────────────────────────────────
		{
			name:         "required/default/no-override/has-rt",
			daemonPolicy: policyRequired, requestMode: orchestration.SandboxModeDefault,
			tenantOverride: nil, hasContainerRT: true,
			wantErr: false, wantMode: orchestration.SandboxModeRequired,
		},
		{
			name:         "required/default/no-override/no-rt",
			daemonPolicy: policyRequired, requestMode: orchestration.SandboxModeDefault,
			tenantOverride: nil, hasContainerRT: false,
			wantErr: true, // required policy + no runtime → violation
		},
		{
			name:         "required/required/no-override/has-rt",
			daemonPolicy: policyRequired, requestMode: orchestration.SandboxModeRequired,
			tenantOverride: nil, hasContainerRT: true,
			wantErr: false, wantMode: orchestration.SandboxModeRequired,
		},
		{
			name:         "required/none/no-override/has-rt",
			daemonPolicy: policyRequired, requestMode: orchestration.SandboxModeNone,
			tenantOverride: nil, hasContainerRT: true,
			wantErr: true, // NONE forbidden under Required policy
		},
		{
			name:         "required/none/no-override/no-rt",
			daemonPolicy: policyRequired, requestMode: orchestration.SandboxModeNone,
			tenantOverride: nil, hasContainerRT: false,
			wantErr: true,
		},
		// tenant override relaxes to Preferred
		{
			name:         "required/none/tenant-preferred/has-rt",
			daemonPolicy: policyRequired, requestMode: orchestration.SandboxModeNone,
			tenantOverride: &policyPref, hasContainerRT: true,
			// Override wins: Preferred allows explicit None.
			wantErr: false, wantMode: orchestration.SandboxModeNone,
		},
		{
			name:         "required/default/tenant-optin/has-rt",
			daemonPolicy: policyRequired, requestMode: orchestration.SandboxModeDefault,
			tenantOverride: &policyOptIn, hasContainerRT: true,
			wantErr: false, wantMode: orchestration.SandboxModeNone,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runtime, err := agent.NewRuntime(agent.RuntimeConfig{
				Backends: map[string]orchestration.AgentBackend{
					"acp": &stubAgentBackend{backendType: "acp"},
				},
				ContainerRuntime: &stubContainerRuntime{available: tc.hasContainerRT},
				SandboxPolicy:    tc.daemonPolicy,
				ImageMap:         map[string]string{"acp": "ghcr.io/test/acp:latest"},
				Logger:           &testLogger{t: t},
			})
			require.NoError(t, err)

			spawnReq := req(tc.requestMode)

			_, spawnErr := runtime.Spawn(
				context.Background(),
				"tenant-test",
				tc.tenantOverride,
				"acp",
				spawnReq,
			)

			if tc.wantErr {
				require.Error(t, spawnErr, "expected a violation error but got nil")
				var violation *orchestration.SandboxModeViolationError
				assert.True(t, errors.As(spawnErr, &violation),
					"error must be *SandboxModeViolationError, got %T: %v", spawnErr, spawnErr)
			} else {
				require.NoError(t, spawnErr, "expected success but got error")
				assert.Equal(t, tc.wantMode, spawnReq.SandboxMode,
					"resolved sandbox mode mismatch")
			}
		})
	}
}

// TestIntegration_SandboxPolicy_DefaultDaemonPolicy verifies that a daemon
// constructed without an explicit SandboxPolicy defaults to the zero value
// (SandboxPolicyOptIn) and honours it.
func TestIntegration_SandboxPolicy_DefaultDaemonPolicy(t *testing.T) {
	runtime, err := agent.NewRuntime(agent.RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			"acp": &stubAgentBackend{backendType: "acp"},
		},
		ContainerRuntime: &stubContainerRuntime{available: false},
		// SandboxPolicy zero value = SandboxPolicyOptIn
		Logger: &testLogger{t: t},
	})
	require.NoError(t, err)

	assert.Equal(t, orchestration.SandboxPolicyOptIn, runtime.SandboxResolver().Policy(),
		"default policy must be SandboxPolicyOptIn")
}
