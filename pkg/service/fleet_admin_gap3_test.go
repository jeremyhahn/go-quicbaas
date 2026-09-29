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
// erroringRegistry is a FleetRegistry whose read methods fail, so the
// service-layer error-translation branches (GetFleetNode / ListFleetNodes) can be
// exercised. Only the methods used by the admin read paths are meaningful.
// =============================================================================

type erroringRegistry struct {
	getErr  error
	listErr error
}

func (r *erroringRegistry) RegisterFleetNode(_ context.Context, _ *orchestration.FleetNode) error {
	return nil
}

func (r *erroringRegistry) GetFleetNode(_ context.Context, _, _ string) (*orchestration.FleetNode, error) {
	return nil, r.getErr
}

func (r *erroringRegistry) ListFleetNodes(_ context.Context, _ string, _ *orchestration.FleetNodeFilter) ([]*orchestration.FleetNode, error) {
	return nil, r.listErr
}

func (r *erroringRegistry) UpdateFleetNode(_ context.Context, _ *orchestration.FleetNode) error {
	return nil
}

func (r *erroringRegistry) RefreshFleetNode(_ context.Context, _ *orchestration.FleetNode) error {
	return nil
}

func (r *erroringRegistry) RemoveFleetNode(_ context.Context, _, _ string) error { return nil }

func (r *erroringRegistry) Heartbeat(_ context.Context, _, _ string, _ *orchestration.FleetNodeObservation) error {
	return nil
}

func (r *erroringRegistry) DeleteTenantFleetNodes(_ context.Context, _ string) error { return nil }

// =============================================================================
// GetFleetNodeRegistration validation + error branches.
// =============================================================================

func TestGetAgentRegistration_ValidationAndErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		svc := &fleetAdminService{registry: orchestration.NewInMemoryFleetRegistry()}
		_, err := svc.GetFleetNodeRegistration(ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty tenant", func(t *testing.T) {
		svc := &fleetAdminService{registry: orchestration.NewInMemoryFleetRegistry()}
		_, err := svc.GetFleetNodeRegistration(ctx, &GetFleetNodeRegistrationRequest{NodeID: "a1"})
		require.Error(t, err)
	})

	t.Run("empty agent", func(t *testing.T) {
		svc := &fleetAdminService{registry: orchestration.NewInMemoryFleetRegistry()}
		_, err := svc.GetFleetNodeRegistration(ctx, &GetFleetNodeRegistrationRequest{TenantID: "t1"})
		require.Error(t, err)
	})

	t.Run("registry not-found is translated", func(t *testing.T) {
		svc := &fleetAdminService{registry: &erroringRegistry{
			getErr: &orchestration.FleetNodeNotFoundError{TenantID: "t1", NodeID: "a1"},
		}}
		_, err := svc.GetFleetNodeRegistration(ctx, &GetFleetNodeRegistrationRequest{TenantID: "t1", NodeID: "a1"})
		require.Error(t, err)
		var notFound *FleetAdminNotFoundError
		assert.True(t, errors.As(err, &notFound))
	})

	t.Run("registry generic error is passed through", func(t *testing.T) {
		sentinel := errors.New("registry boom")
		svc := &fleetAdminService{registry: &erroringRegistry{getErr: sentinel}}
		_, err := svc.GetFleetNodeRegistration(ctx, &GetFleetNodeRegistrationRequest{TenantID: "t1", NodeID: "a1"})
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
	})
}

// =============================================================================
// ListFleetNodeRegistrations validation + error branches.
// =============================================================================

func TestListAgentRegistrations_ValidationAndErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		svc := &fleetAdminService{registry: orchestration.NewInMemoryFleetRegistry()}
		_, err := svc.ListFleetNodeRegistrations(ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty tenant", func(t *testing.T) {
		svc := &fleetAdminService{registry: orchestration.NewInMemoryFleetRegistry()}
		_, err := svc.ListFleetNodeRegistrations(ctx, &ListFleetNodeRegistrationsRequest{})
		require.Error(t, err)
	})

	t.Run("registry error is passed through", func(t *testing.T) {
		sentinel := errors.New("list boom")
		svc := &fleetAdminService{registry: &erroringRegistry{listErr: sentinel}}
		_, err := svc.ListFleetNodeRegistrations(ctx, &ListFleetNodeRegistrationsRequest{TenantID: "t1"})
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
	})

	t.Run("success", func(t *testing.T) {
		reg := orchestration.NewInMemoryFleetRegistry()
		require.NoError(t, reg.RegisterFleetNode(ctx, newAgentWithMeta("t1", "a1", nil)))
		svc := &fleetAdminService{registry: reg}
		resp, err := svc.ListFleetNodeRegistrations(ctx, &ListFleetNodeRegistrationsRequest{TenantID: "t1"})
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
		svc := &fleetAdminService{}
		require.NoError(t, svc.emitAuditEvent(ctx, "agent.disable", "t1", "a1", true))
	})

	t.Run("failure outcome recorded", func(t *testing.T) {
		logger := &mockAuditLogger{}
		svc := &fleetAdminService{auditLogger: logger}
		require.NoError(t, svc.emitAuditEvent(ctx, "agent.disable", "t1", "a1", false))
		require.Len(t, logger.events, 1)
		assert.Equal(t, "agent.disable", logger.events[0].Action)
	})
}
