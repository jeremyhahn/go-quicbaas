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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// erroringRegistry is an AgentRegistry whose read methods fail, so the
// service-layer error-translation branches (GetAgent / ListAgents) can be
// exercised. Only the methods used by the admin read paths are meaningful.
// =============================================================================

type erroringRegistry struct {
	getErr  error
	listErr error
}

func (r *erroringRegistry) RegisterAgent(_ context.Context, _ *orchestration.AgentRegistration) error {
	return nil
}

func (r *erroringRegistry) GetAgent(_ context.Context, _, _ string) (*orchestration.AgentRegistration, error) {
	return nil, r.getErr
}

func (r *erroringRegistry) ListAgents(_ context.Context, _ string, _ *orchestration.AgentFilter) ([]*orchestration.AgentRegistration, error) {
	return nil, r.listErr
}

func (r *erroringRegistry) UpdateAgent(_ context.Context, _ *orchestration.AgentRegistration) error {
	return nil
}

func (r *erroringRegistry) RemoveAgent(_ context.Context, _, _ string) error { return nil }

func (r *erroringRegistry) Heartbeat(_ context.Context, _, _ string) error { return nil }

func (r *erroringRegistry) DeleteTenantAgents(_ context.Context, _ string) error { return nil }

// =============================================================================
// GetAgentRegistration validation + error branches.
// =============================================================================

func TestGetAgentRegistration_ValidationAndErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		svc := &agentAdminServiceImpl{registry: orchestration.NewInMemoryAgentRegistry()}
		_, err := svc.GetAgentRegistration(ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty tenant", func(t *testing.T) {
		svc := &agentAdminServiceImpl{registry: orchestration.NewInMemoryAgentRegistry()}
		_, err := svc.GetAgentRegistration(ctx, &GetAgentRegistrationRequest{AgentID: "a1"})
		require.Error(t, err)
	})

	t.Run("empty agent", func(t *testing.T) {
		svc := &agentAdminServiceImpl{registry: orchestration.NewInMemoryAgentRegistry()}
		_, err := svc.GetAgentRegistration(ctx, &GetAgentRegistrationRequest{TenantID: "t1"})
		require.Error(t, err)
	})

	t.Run("registry not-found is translated", func(t *testing.T) {
		svc := &agentAdminServiceImpl{registry: &erroringRegistry{
			getErr: &orchestration.AgentNotFoundError{TenantID: "t1", AgentID: "a1"},
		}}
		_, err := svc.GetAgentRegistration(ctx, &GetAgentRegistrationRequest{TenantID: "t1", AgentID: "a1"})
		require.Error(t, err)
		var notFound *AgentAdminNotFoundError
		assert.True(t, errors.As(err, &notFound))
	})

	t.Run("registry generic error is passed through", func(t *testing.T) {
		sentinel := errors.New("registry boom")
		svc := &agentAdminServiceImpl{registry: &erroringRegistry{getErr: sentinel}}
		_, err := svc.GetAgentRegistration(ctx, &GetAgentRegistrationRequest{TenantID: "t1", AgentID: "a1"})
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
	})
}

// =============================================================================
// ListAgentRegistrations validation + error branches.
// =============================================================================

func TestListAgentRegistrations_ValidationAndErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		svc := &agentAdminServiceImpl{registry: orchestration.NewInMemoryAgentRegistry()}
		_, err := svc.ListAgentRegistrations(ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty tenant", func(t *testing.T) {
		svc := &agentAdminServiceImpl{registry: orchestration.NewInMemoryAgentRegistry()}
		_, err := svc.ListAgentRegistrations(ctx, &ListAgentRegistrationsRequest{})
		require.Error(t, err)
	})

	t.Run("registry error is passed through", func(t *testing.T) {
		sentinel := errors.New("list boom")
		svc := &agentAdminServiceImpl{registry: &erroringRegistry{listErr: sentinel}}
		_, err := svc.ListAgentRegistrations(ctx, &ListAgentRegistrationsRequest{TenantID: "t1"})
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
	})

	t.Run("success", func(t *testing.T) {
		reg := orchestration.NewInMemoryAgentRegistry()
		require.NoError(t, reg.RegisterAgent(ctx, newAgentWithMeta("t1", "a1", nil)))
		svc := &agentAdminServiceImpl{registry: reg}
		resp, err := svc.ListAgentRegistrations(ctx, &ListAgentRegistrationsRequest{TenantID: "t1"})
		require.NoError(t, err)
		assert.Equal(t, 1, resp.Total)
	})
}

// =============================================================================
// emitAuditEvent: failure outcome + no-logger short-circuit.
// =============================================================================

func TestEmitAuditEvent_Branches(t *testing.T) {
	ctx := context.Background()

	t.Run("no logger is a no-op", func(t *testing.T) {
		svc := &agentAdminServiceImpl{}
		require.NoError(t, svc.emitAuditEvent(ctx, "agent.disable", "t1", "a1", true))
	})

	t.Run("failure outcome recorded", func(t *testing.T) {
		logger := &mockAuditLogger{}
		svc := &agentAdminServiceImpl{auditLogger: logger}
		require.NoError(t, svc.emitAuditEvent(ctx, "agent.disable", "t1", "a1", false))
		require.Len(t, logger.events, 1)
		assert.Equal(t, "agent.disable", logger.events[0].Action)
	})
}
