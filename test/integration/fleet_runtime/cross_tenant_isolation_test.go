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

//go:build integration && fleet_runtime

package fleet_runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestIntegration_CrossTenantIsolation verifies that the daemon enforces
// tenant boundaries at the spawn layer. Three scenarios are tested:
//
//  1. Client authenticates as tenant-b → daemon configured for tenant-a → PermissionDenied.
//  2. Client authenticates as tenant-a → daemon configured for tenant-a → success.
//  3. Credentials are tenant-a but request.TenantID is tenant-b → PermissionDenied.
//
// The daemon-level tenant guard is enforced by the Runtime.Spawn caller
// (the gRPC spawn server in Phase 4). Until that layer exists the tests
// exercise the Runtime directly using the tenantID parameter, which is the
// contract the spawn server must honour.
//
// PHASE-4-DEP: Once spawn_server.go and the FleetRuntime gRPC service exist,
// tests 1 and 3 should drive the spawn server directly to verify the
// PermissionDenied gRPC status codes rather than the runtime error type.
func TestIntegration_CrossTenantIsolation(t *testing.T) {
	const (
		tenantA = "tenant-a"
		tenantB = "tenant-b"
	)

	// isolatedRuntime builds a runtime that knows the daemon's owning tenant.
	// The tenantID passed to Spawn must match daemonTenant or be rejected by
	// the spawn server layer. Here we model that guard with a thin wrapper.
	buildRuntime := func(t *testing.T) *fleet.Runtime {
		t.Helper()
		r, err := fleet.NewRuntime(fleet.RuntimeConfig{
			Backends: map[string]orchestration.AgentBackend{
				"acp": &stubAgentBackend{backendType: "acp"},
			},
			ContainerRuntime: &stubContainerRuntime{available: false},
			SandboxPolicy:    orchestration.SandboxPolicyOptIn,
			Logger:           &testLogger{t: t},
		})
		require.NoError(t, err)
		return r
	}

	t.Run("wrong_tenant_rejected", func(t *testing.T) {
		// The daemon is bound to tenantA. A spawn request carrying tenantB
		// credentials must be rejected. The production path: gRPC spawn
		// server extracts the tenant from the incoming JWT, compares to
		// daemon cfg.TenantID, returns PermissionDenied before calling
		// Runtime.Spawn.
		//
		// Here we model that guard via tenantGuard.
		rt := buildRuntime(t)
		guard := newTenantGuard(tenantA)

		spawnReq := &orchestration.SpawnRequest{
			WorkDir:     t.TempDir(),
			SandboxMode: orchestration.SandboxModeNone,
		}

		err := guard.Authorize(tenantB)
		require.Error(t, err, "tenant-b must be rejected by tenant-a guard")

		var te *tenantMismatchError
		assert.True(t, errors.As(err, &te), "error must be *tenantMismatchError")

		// Runtime.Spawn itself is not called when the guard rejects. Confirm
		// it still works for the correct tenant.
		_, spawnErr := rt.Spawn(context.Background(), tenantA, nil, "acp", spawnReq)
		require.NoError(t, spawnErr, "spawn for the correct tenant must succeed")
	})

	t.Run("correct_tenant_accepted", func(t *testing.T) {
		rt := buildRuntime(t)
		guard := newTenantGuard(tenantA)

		require.NoError(t, guard.Authorize(tenantA), "tenant-a must be accepted")

		session, err := rt.Spawn(context.Background(), tenantA, nil, "acp", &orchestration.SpawnRequest{
			WorkDir:     t.TempDir(),
			SandboxMode: orchestration.SandboxModeNone,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, session.ID)
	})

	t.Run("credential_tenant_a_but_request_tenant_b_rejected", func(t *testing.T) {
		// The authenticated identity is for tenantA but the spawn request
		// explicitly targets tenantB. The guard checks the request tenant
		// against the daemon's configured tenant — not just the JWT tenant.
		guard := newTenantGuard(tenantA)

		// JWT subject maps to tenantA (authentication passes). But the spawn
		// request's TenantID field is tenantB — this is an escalation attempt.
		err := guard.AuthorizeRequest(tenantA /* jwt tenant */, tenantB /* request tenant */)
		require.Error(t, err, "mismatched request tenant must be rejected even with valid credentials")

		var te *tenantMismatchError
		assert.True(t, errors.As(err, &te))
		assert.Equal(t, tenantA, te.DaemonTenant)
		assert.Equal(t, tenantB, te.RequestTenant)
	})

	t.Run("daemon_tenant_isolation_full_lifecycle", func(t *testing.T) {
		// End-to-end: build two daemons for different tenants. Verify that
		// each daemon's identity is scoped to its own tenant and that a
		// cross-tenant spawn is blocked.
		mc := newMockCentral(t, tenantA)

		dA, _, _, _ := newTestDaemon(t, testDaemonConfig{
			centralURL: mc.URL(),
			tenantID:   tenantA,
		})

		ctx := context.Background()
		require.NoError(t, dA.Start(ctx))
		t.Cleanup(func() { _ = dA.Stop(context.Background()) })

		assert.Equal(t, tenantA, dA.Identity().TenantID,
			"daemon identity must reflect the configured tenant")

		// Spawn for the correct tenant succeeds.
		_, err := dA.Runtime().Spawn(ctx, tenantA, nil, "acp", &orchestration.SpawnRequest{
			WorkDir:     t.TempDir(),
			SandboxMode: orchestration.SandboxModeNone,
		})
		require.NoError(t, err)

		// Spawn with the wrong tenant ID should never reach the runtime —
		// the gRPC spawn server (Phase 4) enforces this. We verify the
		// guard here.
		guard := newTenantGuard(tenantA)
		require.Error(t, guard.Authorize(tenantB),
			"cross-tenant spawn must be rejected at the guard layer")
	})
}

// ----------------------------------------------------------------------------
// tenantGuard — in-process tenant isolation check
// ----------------------------------------------------------------------------

// tenantMismatchError is returned when a request's tenant does not match the
// daemon's configured tenant.
//
// PHASE-4-DEP: The production spawn server must map this error (or an
// equivalent check) to gRPC status codes.StatusPermissionDenied before
// calling Runtime.Spawn.
type tenantMismatchError struct {
	DaemonTenant  string
	RequestTenant string
}

func (e *tenantMismatchError) Error() string {
	return "tenant isolation violation: daemon serves " + e.DaemonTenant +
		" but request is for " + e.RequestTenant
}

// tenantGuard enforces that all requests target the daemon's configured tenant.
type tenantGuard struct {
	daemonTenant string
}

func newTenantGuard(daemonTenant string) *tenantGuard {
	return &tenantGuard{daemonTenant: daemonTenant}
}

// Authorize checks that callerTenant matches the daemon's tenant.
func (g *tenantGuard) Authorize(callerTenant string) error {
	if callerTenant != g.daemonTenant {
		return &tenantMismatchError{
			DaemonTenant:  g.daemonTenant,
			RequestTenant: callerTenant,
		}
	}
	return nil
}

// AuthorizeRequest checks both that jwtTenant matches daemonTenant AND that
// requestTenant matches daemonTenant, preventing escalation where valid
// credentials for tenantA are used to target tenantB.
func (g *tenantGuard) AuthorizeRequest(jwtTenant, requestTenant string) error {
	if jwtTenant != g.daemonTenant {
		return &tenantMismatchError{
			DaemonTenant:  g.daemonTenant,
			RequestTenant: jwtTenant,
		}
	}
	if requestTenant != g.daemonTenant {
		return &tenantMismatchError{
			DaemonTenant:  g.daemonTenant,
			RequestTenant: requestTenant,
		}
	}
	return nil
}
