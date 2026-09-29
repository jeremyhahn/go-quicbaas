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

package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// buildAdminSvc returns a FleetAdminService with a seeded agent.
func buildAdminSvc(t *testing.T, agent *orchestration.FleetNode) service.FleetAdminService {
	t.Helper()
	registry := orchestration.NewInMemoryFleetRegistry()
	if agent != nil {
		if err := registry.RegisterFleetNode(context.Background(), agent); err != nil {
			t.Fatalf("buildAdminSvc: register: %v", err)
		}
	}
	return service.NewFleetAdminService(registry)
}

func activeAgent(tenantID, nodeID string) *orchestration.FleetNode {
	return &orchestration.FleetNode{
		ID:       nodeID,
		TenantID: tenantID,
		Name:     "test-agent",
		Host:     "localhost:9000",
		NodeType: "test",
		Status:   orchestration.FleetNodeStatusRunning,
	}
}

// ---------------------------------------------------------------------------
// DisableFleetNode
// ---------------------------------------------------------------------------

func TestAgentAdminService_DisableAgent_Success(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, activeAgent("t1", "agent-1"))

	resp, err := svc.DisableFleetNode(ctx, &service.DisableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-1",
	})
	if err != nil {
		t.Fatalf("DisableFleetNode: %v", err)
	}
	if !resp.Disabled {
		t.Error("expected Disabled=true")
	}
	if resp.NodeID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", resp.NodeID, "agent-1")
	}
	if resp.DisabledAt.IsZero() {
		t.Error("expected non-zero DisabledAt")
	}
}

func TestAgentAdminService_DisableAgent_AlreadyDisabled(t *testing.T) {
	ctx := context.Background()
	agent := activeAgent("t1", "agent-2")
	agent.Status = orchestration.FleetNodeStatusDisabled
	svc := buildAdminSvc(t, agent)

	_, err := svc.DisableFleetNode(ctx, &service.DisableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-2",
	})
	if err == nil {
		t.Fatal("expected FleetNodeAlreadyDisabledError, got nil")
	}
	var already *service.FleetNodeAlreadyDisabledError
	if !errors.As(err, &already) {
		t.Fatalf("expected FleetNodeAlreadyDisabledError, got %T: %v", err, err)
	}
}

func TestAgentAdminService_DisableAgent_AgentNotFound(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, nil)

	_, err := svc.DisableFleetNode(ctx, &service.DisableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "no-such-agent",
	})
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
	var notFound *service.FleetAdminNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected FleetAdminNotFoundError, got %T: %v", err, err)
	}
}

func TestAgentAdminService_DisableAgent_NilRequest(t *testing.T) {
	svc := buildAdminSvc(t, nil)
	_, err := svc.DisableFleetNode(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

// ---------------------------------------------------------------------------
// EnableFleetNode
// ---------------------------------------------------------------------------

func TestAgentAdminService_EnableAgent_Success(t *testing.T) {
	ctx := context.Background()
	agent := activeAgent("t1", "agent-3")
	agent.Status = orchestration.FleetNodeStatusDisabled
	svc := buildAdminSvc(t, agent)

	resp, err := svc.EnableFleetNode(ctx, &service.EnableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-3",
	})
	if err != nil {
		t.Fatalf("EnableFleetNode: %v", err)
	}
	if !resp.Enabled {
		t.Error("expected Enabled=true")
	}
	if resp.NodeID != "agent-3" {
		t.Errorf("AgentID = %q, want %q", resp.NodeID, "agent-3")
	}

	// Verify the status reverted.
	getResp, err := svc.GetFleetNodeRegistration(ctx, &service.GetFleetNodeRegistrationRequest{
		TenantID: "t1",
		NodeID:   "agent-3",
	})
	if err != nil {
		t.Fatalf("GetFleetNodeRegistration: %v", err)
	}
	if getResp.Agent.Status == orchestration.FleetNodeStatusDisabled {
		t.Error("expected status to be no longer disabled after enable")
	}
}

func TestAgentAdminService_EnableAgent_NotDisabled(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, activeAgent("t1", "agent-4"))

	_, err := svc.EnableFleetNode(ctx, &service.EnableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-4",
	})
	if err == nil {
		t.Fatal("expected FleetNodeNotDisabledError, got nil")
	}
	var notDisabled *service.FleetNodeNotDisabledError
	if !errors.As(err, &notDisabled) {
		t.Fatalf("expected FleetNodeNotDisabledError, got %T: %v", err, err)
	}
}

func TestAgentAdminService_EnableAgent_AgentNotFound(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, nil)

	_, err := svc.EnableFleetNode(ctx, &service.EnableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "no-such-agent",
	})
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
	var notFound *service.FleetAdminNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected FleetAdminNotFoundError, got %T: %v", err, err)
	}
}

func TestAgentAdminService_EnableAgent_NilRequest(t *testing.T) {
	svc := buildAdminSvc(t, nil)
	_, err := svc.EnableFleetNode(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

// ---------------------------------------------------------------------------
// Integration: disable then enable round-trip
// ---------------------------------------------------------------------------

func TestAgentAdminService_DisableEnableRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, activeAgent("t1", "agent-5"))

	// Disable.
	disableResp, err := svc.DisableFleetNode(ctx, &service.DisableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-5",
	})
	if err != nil {
		t.Fatalf("DisableFleetNode: %v", err)
	}
	if !disableResp.Disabled {
		t.Fatal("expected Disabled=true")
	}

	// Verify disabled.
	_, err = svc.DisableFleetNode(ctx, &service.DisableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-5",
	})
	var already *service.FleetNodeAlreadyDisabledError
	if !errors.As(err, &already) {
		t.Fatalf("expected AlreadyDisabledError on second disable, got %T: %v", err, err)
	}

	// Re-enable.
	enableResp, err := svc.EnableFleetNode(ctx, &service.EnableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-5",
	})
	if err != nil {
		t.Fatalf("EnableFleetNode: %v", err)
	}
	if !enableResp.Enabled {
		t.Fatal("expected Enabled=true")
	}

	// Should now be re-disableable.
	_, err = svc.DisableFleetNode(ctx, &service.DisableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-5",
	})
	if err != nil {
		t.Fatalf("expected second disable to succeed, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetFleetNodeRegistration / ListFleetNodeRegistrations
// ---------------------------------------------------------------------------

func TestAgentAdminService_GetAgentRegistration_Success(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, activeAgent("t1", "agent-6"))

	resp, err := svc.GetFleetNodeRegistration(ctx, &service.GetFleetNodeRegistrationRequest{
		TenantID: "t1",
		NodeID:   "agent-6",
	})
	if err != nil {
		t.Fatalf("GetFleetNodeRegistration: %v", err)
	}
	if resp.Agent.ID != "agent-6" {
		t.Errorf("ID = %q, want %q", resp.Agent.ID, "agent-6")
	}
}

func TestAgentAdminService_GetAgentRegistration_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, nil)

	_, err := svc.GetFleetNodeRegistration(ctx, &service.GetFleetNodeRegistrationRequest{
		TenantID: "t1",
		NodeID:   "missing",
	})
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
}

func TestAgentAdminService_ListAgentRegistrations_Success(t *testing.T) {
	ctx := context.Background()
	registry := orchestration.NewInMemoryFleetRegistry()
	for _, id := range []string{"a1", "a2", "a3"} {
		if err := registry.RegisterFleetNode(ctx, activeAgent("t1", id)); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	svc := service.NewFleetAdminService(registry)

	resp, err := svc.ListFleetNodeRegistrations(ctx, &service.ListFleetNodeRegistrationsRequest{
		TenantID: "t1",
	})
	if err != nil {
		t.Fatalf("ListFleetNodeRegistrations: %v", err)
	}
	if resp.Total != 3 {
		t.Errorf("Total = %d, want 3", resp.Total)
	}
}
