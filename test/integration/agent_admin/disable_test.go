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

//go:build integration

package agent_admin_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Test doubles
// =============================================================================

// fakeAppRevoker records DeleteApp calls so the test can assert that the
// agent's OAuth2 client (and mTLS thumbprint app) were revoked when the agent
// is disabled.
type fakeAppRevoker struct {
	mu      sync.Mutex
	deleted []string
	err     error
}

// DeleteApp records the clientID and returns the configured error (if any).
func (f *fakeAppRevoker) DeleteApp(_ context.Context, clientID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, clientID)
	return f.err
}

func (f *fakeAppRevoker) deletedCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.deleted))
	copy(out, f.deleted)
	return out
}

// fakeAuditLogger captures audit events emitted by the service so tests can
// assert that agent.disabled / agent.enabled events were emitted with the
// correct outcome.
type fakeAuditLogger struct {
	mu     sync.Mutex
	events []*iam.AuditEvent
	err    error
}

func (f *fakeAuditLogger) Log(_ context.Context, ev *iam.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return f.err
}

func (f *fakeAuditLogger) LogAsync(ev *iam.AuditEvent) {
	_ = f.Log(context.Background(), ev)
}

func (f *fakeAuditLogger) Query(_ context.Context, _ *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	return nil, 0, nil
}

func (f *fakeAuditLogger) GetEvent(_ context.Context, _, _ string) (*iam.AuditEvent, error) {
	return nil, nil
}

func (f *fakeAuditLogger) DeleteOlderThan(_ context.Context, _ string, _ time.Time) (int, error) {
	return 0, nil
}

func (f *fakeAuditLogger) Close() error {
	return nil
}

func (f *fakeAuditLogger) eventsByAction(action string) []*iam.AuditEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*iam.AuditEvent, 0, len(f.events))
	for _, ev := range f.events {
		if ev.Action == action {
			out = append(out, ev)
		}
	}
	return out
}

// =============================================================================
// Test fixture builder
// =============================================================================

// adminFixture bundles together the service under test with the registry and
// captured side-effect doubles. Each test gets a fresh fixture so state does
// not leak between cases.
type adminFixture struct {
	registry orchestration.AgentRegistry
	revoker  *fakeAppRevoker
	audit    *fakeAuditLogger
	svc      service.AgentAdminService
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	registry := orchestration.NewInMemoryAgentRegistry()
	revoker := &fakeAppRevoker{}
	audit := &fakeAuditLogger{}

	svc := service.NewAgentAdminService(registry)
	wirer, ok := svc.(interface {
		SetAppClientRevoker(service.AppClientRevoker)
		SetAuditLogger(iam.AuditLogger)
	})
	require.True(t, ok, "service must support side-effect wirers")
	wirer.SetAppClientRevoker(revoker)
	wirer.SetAuditLogger(audit)

	return &adminFixture{
		registry: registry,
		revoker:  revoker,
		audit:    audit,
		svc:      svc,
	}
}

// registerAgent inserts a fresh agent registration into the registry and
// returns it. The mTLS app id is derived from the agent ID so the disable
// path exercises both the OAuth2 and mTLS revocation branches.
func (f *adminFixture) registerAgent(t *testing.T, tenantID, agentID string) *orchestration.AgentRegistration {
	t.Helper()
	agent := &orchestration.AgentRegistration{
		ID:        agentID,
		TenantID:  tenantID,
		Name:      "agent-" + agentID,
		Host:      "host-1",
		Backend:   "ollama",
		AgentType: "worker",
		Status:    orchestration.AgentStatusRegistered,
		Metadata: map[string]string{
			"oauth2_client_id": agentID + "-oauth",
			"mtls_client_id":   agentID + "-mtls",
		},
	}
	err := f.registry.RegisterAgent(context.Background(), agent)
	require.NoError(t, err, "registry.RegisterAgent")
	return agent
}

// =============================================================================
// Tests — DisableAgent / EnableAgent state machine + side-effects
// =============================================================================

// TestDisableAgent_HappyPath verifies that DisableAgent flips the agent state,
// revokes the OAuth2 client_id and the mTLS thumbprint app, and emits the
// agent.disabled audit event.
func TestDisableAgent_HappyPath(t *testing.T) {
	fx := newAdminFixture(t)
	ctx := context.Background()

	tenantID := uniqueSuffix("tenant")
	agentID := uniqueSuffix("agent")
	fx.registerAgent(t, tenantID, agentID)

	// Sanity: the agent starts as registered (not disabled).
	preGet, err := fx.svc.GetAgentRegistration(ctx, &service.GetAgentRegistrationRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	require.NoError(t, err)
	require.NotNil(t, preGet.Agent)
	assert.Equal(t, orchestration.AgentStatusRegistered, preGet.Agent.Status)

	// Disable.
	resp, err := fx.svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	require.NoError(t, err, "DisableAgent should succeed")
	require.NotNil(t, resp)
	assert.True(t, resp.Disabled)
	assert.Equal(t, agentID, resp.AgentID)
	assert.False(t, resp.DisabledAt.IsZero(), "DisabledAt should be set")

	// Status now reads as disabled.
	postGet, err := fx.svc.GetAgentRegistration(ctx, &service.GetAgentRegistrationRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	require.NoError(t, err)
	require.NotNil(t, postGet.Agent)
	assert.Equal(t, orchestration.AgentStatusDisabled, postGet.Agent.Status,
		"agent status should be disabled after DisableAgent")

	// Verify side-effects: both the OAuth2 app and mTLS app were revoked.
	deleted := fx.revoker.deletedCopy()
	assert.Contains(t, deleted, agentID+"-oauth", "OAuth2 client_id should be revoked")
	assert.Contains(t, deleted, agentID+"-mtls", "mTLS thumbprint app should be revoked")

	// Verify audit event was emitted with success outcome.
	disabledEvents := fx.audit.eventsByAction("agent.disabled")
	require.Len(t, disabledEvents, 1, "exactly one agent.disabled event expected")
	assert.Equal(t, iam.AuditOutcomeSuccess, disabledEvents[0].Outcome)
	require.NotNil(t, disabledEvents[0].Resource)
	assert.Equal(t, "agent", disabledEvents[0].Resource.Type)
	assert.Equal(t, agentID, disabledEvents[0].Resource.ID)
}

// TestDisableAgent_AlreadyDisabled verifies that disabling an already-disabled
// agent returns the typed *AgentAlreadyDisabledError.
func TestDisableAgent_AlreadyDisabled(t *testing.T) {
	fx := newAdminFixture(t)
	ctx := context.Background()

	tenantID := uniqueSuffix("tenant")
	agentID := uniqueSuffix("agent")
	fx.registerAgent(t, tenantID, agentID)

	_, err := fx.svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	require.NoError(t, err)

	// Second call must return AgentAlreadyDisabledError.
	_, err = fx.svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	require.Error(t, err, "second DisableAgent should error")

	var alreadyDisabled *service.AgentAlreadyDisabledError
	assert.True(t, errors.As(err, &alreadyDisabled),
		"expected *AgentAlreadyDisabledError, got %T: %v", err, err)
	if alreadyDisabled != nil {
		assert.Equal(t, tenantID, alreadyDisabled.TenantID)
		assert.Equal(t, agentID, alreadyDisabled.AgentID)
	}
}

// TestEnableAgent_HappyPath verifies that EnableAgent flips a disabled agent
// back to registered and emits the agent.enabled audit event.
func TestEnableAgent_HappyPath(t *testing.T) {
	fx := newAdminFixture(t)
	ctx := context.Background()

	tenantID := uniqueSuffix("tenant")
	agentID := uniqueSuffix("agent")
	fx.registerAgent(t, tenantID, agentID)

	_, err := fx.svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	require.NoError(t, err)

	// Re-enable.
	resp, err := fx.svc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	require.NoError(t, err, "EnableAgent should succeed")
	require.NotNil(t, resp)
	assert.True(t, resp.Enabled)
	assert.Equal(t, agentID, resp.AgentID)

	// Status flips back to registered.
	postGet, err := fx.svc.GetAgentRegistration(ctx, &service.GetAgentRegistrationRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	require.NoError(t, err)
	require.NotNil(t, postGet.Agent)
	assert.Equal(t, orchestration.AgentStatusRegistered, postGet.Agent.Status,
		"status should be registered again after EnableAgent")

	enabledEvents := fx.audit.eventsByAction("agent.enabled")
	require.Len(t, enabledEvents, 1, "exactly one agent.enabled event expected")
	assert.Equal(t, iam.AuditOutcomeSuccess, enabledEvents[0].Outcome)
}

// TestEnableAgent_NotDisabled verifies that EnableAgent on a registered
// (not disabled) agent returns *AgentNotDisabledError.
func TestEnableAgent_NotDisabled(t *testing.T) {
	fx := newAdminFixture(t)
	ctx := context.Background()

	tenantID := uniqueSuffix("tenant")
	agentID := uniqueSuffix("agent")
	fx.registerAgent(t, tenantID, agentID)

	_, err := fx.svc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	require.Error(t, err)

	var notDisabled *service.AgentNotDisabledError
	assert.True(t, errors.As(err, &notDisabled),
		"expected *AgentNotDisabledError, got %T: %v", err, err)
}

// TestDisableAgent_UnknownAgent verifies that disabling an unknown agent
// returns *AgentAdminNotFoundError without emitting an audit event.
func TestDisableAgent_UnknownAgent(t *testing.T) {
	fx := newAdminFixture(t)
	ctx := context.Background()

	_, err := fx.svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: uniqueSuffix("tenant"),
		AgentID:  uniqueSuffix("ghost"),
	})
	require.Error(t, err)

	var notFound *service.AgentAdminNotFoundError
	assert.True(t, errors.As(err, &notFound),
		"expected *AgentAdminNotFoundError, got %T: %v", err, err)
	// No agent.disabled events should have been emitted.
	assert.Empty(t, fx.audit.eventsByAction("agent.disabled"),
		"no audit events should be emitted for unknown agents")
	// And no apps were revoked.
	assert.Empty(t, fx.revoker.deletedCopy(),
		"no revocations should occur for unknown agents")
}

// TestDisableEnableCycle covers the full disable -> re-enable -> redisable
// cycle to confirm idempotent state transitions and clean audit history.
func TestDisableEnableCycle(t *testing.T) {
	fx := newAdminFixture(t)
	ctx := context.Background()

	tenantID := uniqueSuffix("tenant")
	agentID := uniqueSuffix("agent")
	fx.registerAgent(t, tenantID, agentID)

	// Disable.
	_, err := fx.svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: tenantID, AgentID: agentID,
	})
	require.NoError(t, err)

	// Enable.
	_, err = fx.svc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: tenantID, AgentID: agentID,
	})
	require.NoError(t, err)

	// Disable again — should succeed (state is no longer "disabled").
	_, err = fx.svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: tenantID, AgentID: agentID,
	})
	require.NoError(t, err, "Disable after re-enable should succeed")

	disabledEvents := fx.audit.eventsByAction("agent.disabled")
	enabledEvents := fx.audit.eventsByAction("agent.enabled")
	assert.Len(t, disabledEvents, 2, "two disable cycles -> two events")
	assert.Len(t, enabledEvents, 1, "one re-enable -> one event")
}
