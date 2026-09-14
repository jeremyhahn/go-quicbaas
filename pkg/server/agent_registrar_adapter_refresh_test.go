//go:build !integration

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

// agent_registrar_adapter_refresh_test.go covers the re-registration half of
// the registrar contract (go-quicbaas#646): a daemon already in the fleet
// registry is converged onto what it reports now, under its own principal.

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// enrollmentRequest builds the registrar request the mTLS register handler
// produces for a daemon presenting a certificate with the given CommonName.
func enrollmentRequest(agentID, principal, host string, backends ...string) *iam.AgentRegistrarRequest {
	return &iam.AgentRegistrarRequest{
		AgentID:   agentID,
		TenantID:  "automatethethings",
		Name:      agentID,
		Host:      host,
		Backends:  backends,
		AgentType: "agent-runtime",
		Kind:      "daemon",
		Metadata: map[string]string{
			orchestration.AgentPrincipalMetadataKey: principal,
		},
	}
}

// TestAgentRegistrarAdapter_ReRegistrationRefreshesTheRecord is the live
// burn-in failure driven through the seam that produced it.
//
// The daemon enrolled advertising its TRANSPORT ("acp"), was restarted from a
// build that advertises the backends it serves, and re-registered. The guarded
// create answered "agent already exists", the register handler logged it, and
// the record kept "acp" — so ListAgents(Backend:"claude") found nothing, the
// spawn fell to the central's in-process backend and the sdlc runner refused
// it for having no container.
func TestAgentRegistrarAdapter_ReRegistrationRefreshesTheRecord(t *testing.T) {
	registry := orchestration.NewInMemoryAgentRegistry()
	adapter := newAgentRegistrarAdapter(registry)
	ctx := context.Background()

	require.NoError(t, adapter.RegisterAgent(ctx,
		enrollmentRequest("agent-att-vm-host-4", "agent-vm-host-4", "10.0.0.4:9090", "acp")))

	first, err := registry.GetAgent(ctx, "automatethethings", "agent-att-vm-host-4")
	require.NoError(t, err)

	// The daemon re-enrolls from a new address, serving what it actually runs.
	require.NoError(t, adapter.RegisterAgent(ctx,
		enrollmentRequest("agent-att-vm-host-4", "agent-vm-host-4", "10.0.0.9:9091",
			"claude", "codex", "opencode")),
		"a daemon already in the registry must be refreshed, not refused")

	matched, err := registry.ListAgents(ctx, "automatethethings", &orchestration.AgentFilter{Backend: "claude"})
	require.NoError(t, err)
	require.Len(t, matched, 1, "the resolver's own filter must find the daemon")
	assert.Equal(t, "10.0.0.9:9091", matched[0].Host)
	assert.Equal(t, "claude", matched[0].Backend)
	assert.Equal(t, []string{"claude", "codex", "opencode"}, matched[0].Capabilities)
	assert.Equal(t, first.RegisteredAt, matched[0].RegisteredAt, "a re-registration is not a new join")

	stale, err := registry.ListAgents(ctx, "automatethethings", &orchestration.AgentFilter{Backend: "acp"})
	require.NoError(t, err)
	assert.Empty(t, stale)
}

// TestAgentRegistrarAdapter_RefusesADifferentPrincipal keeps the refresh
// bounded to the identity the certificate proves. The agent ID is a sanitised
// derivation of the CommonName, so distinct subjects can address one record.
func TestAgentRegistrarAdapter_RefusesADifferentPrincipal(t *testing.T) {
	registry := orchestration.NewInMemoryAgentRegistry()
	adapter := newAgentRegistrarAdapter(registry)
	ctx := context.Background()

	require.NoError(t, adapter.RegisterAgent(ctx,
		enrollmentRequest("agent-att-a-b", "a.b", "10.0.0.1:9090", "claude")))

	err := adapter.RegisterAgent(ctx,
		enrollmentRequest("agent-att-a-b", "a b", "10.6.6.6:9090", "codex"))
	require.ErrorIs(t, err, orchestration.ErrAgentPrincipalMismatch)

	stored, getErr := registry.GetAgent(ctx, "automatethethings", "agent-att-a-b")
	require.NoError(t, getErr)
	assert.Equal(t, "10.0.0.1:9090", stored.Host, "the owner's record must be untouched")
	assert.Equal(t, "claude", stored.Backend)
}

// TestAgentRegistrarAdapter_RemovedBetweenCreateAndRefresh reports the race
// rather than registering the record back into existence: a removal is a
// deliberate operator act and the registrar must not undo it.
func TestAgentRegistrarAdapter_RemovedBetweenCreateAndRefresh(t *testing.T) {
	registry := orchestration.NewInMemoryAgentRegistry()
	adapter := newAgentRegistrarAdapter(registry)
	ctx := context.Background()

	err := adapter.RegisterAgent(ctx, enrollmentRequest("ghost", "cn-1", "h", "claude"))
	require.NoError(t, err)
	require.NoError(t, registry.RemoveAgent(ctx, "automatethethings", "ghost"))

	// Drive the refresh branch directly: the record the create would lose to
	// is gone by the time the refresh reads it.
	refreshErr := registry.RefreshAgent(ctx, &orchestration.AgentRegistration{
		ID: "ghost", TenantID: "automatethethings",
		Metadata: map[string]string{orchestration.AgentPrincipalMetadataKey: "cn-1"},
	})
	require.ErrorIs(t, refreshErr, orchestration.ErrAgentRegistryNotFound)
}
