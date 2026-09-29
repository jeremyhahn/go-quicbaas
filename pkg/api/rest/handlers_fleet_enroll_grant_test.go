//go:build rest

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

package rest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// go-quicbaas#653 — the entitlements registration advertises must be granted.
//
// An enrolled agent was refused 403 on every heartbeat: the register handler
// answered with role "agent-runtime" and a scope list, and bound neither to the
// principal. The response was a description, not a grant. These tests pin the
// grant to the application record the token endpoint reads.
// =============================================================================

// heartbeatRoutePermission returns the resource/action the heartbeat route
// enforces, read from the generated permission table rather than restated, so
// a route change that moves the requirement breaks this test instead of
// silently stranding the fleet again.
func heartbeatRoutePermission(t *testing.T) string {
	t.Helper()
	perm, ok := frozenRoutePermissions["POST /api/v1/fleet/{nodeID}/heartbeat"]
	require.True(t, ok, "the heartbeat route has no permission entry")
	return perm.resource + ":" + perm.action
}

// TestHandleAgentRegister_GrantsTheHeartbeatPermission is the #653 regression:
// after enrollment the agent's OAuth2 application must hold the permission the
// heartbeat route enforces, because that grant is the entire authority a
// client_credentials principal has.
func TestHandleAgentRegister_GrantsTheHeartbeatPermission(t *testing.T) {
	f := newAgentOAuthFixture(t)
	resp := f.register(t, "agent-gpu-host")

	ctx := tokenEndpointContext(resp.ClientID)
	app, err := f.storage.GetApp(ctx, resp.ClientID)
	require.NoError(t, err)

	require.Contains(t, app.Scopes, heartbeatRoutePermission(t),
		"the enrolled agent holds no grant for the permission its heartbeat requires")
	require.Contains(t, app.Scopes, "orchestrator:hook",
		"the enrolled agent holds no grant for the gRPC PushEvents stream it forwards events on")

	// The daemon's own spawn server gates inbound orchestrator RPCs on these
	// three, read from the identity its enrollment produced, so dropping them
	// would make the agent undispatchable in the other direction.
	require.Subset(t, app.Scopes, iam.DefaultFleetRuntimeScopes())

	// The response must advertise exactly what was granted: the daemon
	// persists these and sends them as its requested scope, and the token
	// endpoint refuses a request that exceeds the grant.
	require.ElementsMatch(t, iam.FleetRuntimeGrant(), resp.Scopes)
}

// TestHandleAgentRegister_GrantIsNarrow proves the grant is least-privilege: an
// agent runtime heartbeats, forwards events and serves spawn/prompt/status. It
// gets no control-plane authority over tasks, jobs, other agents, or the
// tenant's data and key material.
func TestHandleAgentRegister_GrantIsNarrow(t *testing.T) {
	f := newAgentOAuthFixture(t)
	resp := f.register(t, "agent-gpu-host")

	ctx := tokenEndpointContext(resp.ClientID)
	app, err := f.storage.GetApp(ctx, resp.ClientID)
	require.NoError(t, err)

	forbidden := []string{
		"orchestrator:*",
		"orchestrator:spawn",
		"orchestrator:prompt",
		"orchestrator:task_create",
		"orchestrator:task_claim",
		"orchestrator:task_complete",
		"orchestrator:job_start",
		"orchestrator:agent_register",
		"orchestrator:agent_remove",
		"orchestrator:broadcast",
		"orchestrator:inference",
		"kv:put",
		"secrets:read",
		"keys:get",
		"users:create",
		"roles:assign",
		"tenant_barrier:init",
		"*:*",
	}
	for _, perm := range forbidden {
		require.NotContains(t, app.Scopes, perm,
			"an agent runtime must not hold %s", perm)
	}
	require.Len(t, app.Scopes, len(iam.FleetRuntimeGrant()))
}

// TestHandleAgentRegister_ConvergesTheGrantOnReEnrollment covers the upgrade
// path: an application registered before the grant existed keeps its client id
// and secret rotation, and must acquire the grant on the next enrollment
// rather than staying permanently unable to heartbeat.
func TestHandleAgentRegister_ConvergesTheGrantOnReEnrollment(t *testing.T) {
	f := newAgentOAuthFixture(t)

	// An application as a pre-#653 node left it: right identity, no grant.
	stale := &iam.Application{
		ClientID:       iam.FleetNodeClientID(agentOAuthTestTenant, "agent-gpu-host"),
		Name:           iam.FleetNodeAppName(agentOAuthTestTenant, "agent-gpu-host"),
		TenantID:       agentOAuthTestTenant,
		GrantTypes:     []string{iam.GrantTypeClientCredentials},
		ResponseTypes:  []string{},
		ResourcePrefix: "agent-runtime",
	}
	require.NoError(t, f.storage.CreateApp(
		loginContextWithTenant(context.Background(), agentOAuthTestTenant), stale))

	resp := f.register(t, "agent-gpu-host")
	require.Equal(t, stale.ClientID, resp.ClientID, "re-enrollment must converge on the existing client")

	app, err := f.storage.GetApp(tokenEndpointContext(resp.ClientID), resp.ClientID)
	require.NoError(t, err)
	require.ElementsMatch(t, iam.FleetRuntimeGrant(), app.Scopes,
		"re-enrollment left the stale application without the grant it needs to heartbeat")
}

// TestHandleAgentRegister_GrantsTheMTLSClientTheSameSet pins the second
// application enrollment creates: the daemon prefers /oauth2/token-mtls, so a
// grant that only reached the secret-bearing client would leave every
// certificate-authenticated agent refused.
func TestHandleAgentRegister_GrantsTheMTLSClientTheSameSet(t *testing.T) {
	f := newAgentOAuthFixture(t)
	resp := f.register(t, "agent-gpu-host")
	require.NotEmpty(t, resp.MTLSClientID)

	ctx := loginContextWithTenant(context.Background(), agentOAuthTestTenant)
	app, err := f.storage.GetAppByTenant(ctx, agentOAuthTestTenant, resp.MTLSClientID)
	require.NoError(t, err)
	require.ElementsMatch(t, iam.FleetRuntimeGrant(), app.Scopes)
}
