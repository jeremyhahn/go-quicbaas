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

// buildAdminSvc returns an AgentAdminService with a seeded agent.
func buildAdminSvc(t *testing.T, agent *orchestration.AgentRegistration) service.AgentAdminService {
	t.Helper()
	registry := orchestration.NewInMemoryAgentRegistry()
	if agent != nil {
		if err := registry.RegisterAgent(context.Background(), agent); err != nil {
			t.Fatalf("buildAdminSvc: register: %v", err)
		}
	}
	return service.NewAgentAdminService(registry)
}

func activeAgent(tenantID, agentID string) *orchestration.AgentRegistration {
	return &orchestration.AgentRegistration{
		ID:        agentID,
		TenantID:  tenantID,
		Name:      "test-agent",
		Host:      "localhost:9000",
		AgentType: "test",
		Status:    orchestration.AgentStatusRunning,
	}
}

// ---------------------------------------------------------------------------
// DisableAgent
// ---------------------------------------------------------------------------

func TestAgentAdminService_DisableAgent_Success(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, activeAgent("t1", "agent-1"))

	resp, err := svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-1",
	})
	if err != nil {
		t.Fatalf("DisableAgent: %v", err)
	}
	if !resp.Disabled {
		t.Error("expected Disabled=true")
	}
	if resp.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", resp.AgentID, "agent-1")
	}
	if resp.DisabledAt.IsZero() {
		t.Error("expected non-zero DisabledAt")
	}
}

func TestAgentAdminService_DisableAgent_AlreadyDisabled(t *testing.T) {
	ctx := context.Background()
	agent := activeAgent("t1", "agent-2")
	agent.Status = orchestration.AgentStatusDisabled
	svc := buildAdminSvc(t, agent)

	_, err := svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-2",
	})
	if err == nil {
		t.Fatal("expected AgentAlreadyDisabledError, got nil")
	}
	var already *service.AgentAlreadyDisabledError
	if !errors.As(err, &already) {
		t.Fatalf("expected AgentAlreadyDisabledError, got %T: %v", err, err)
	}
}

func TestAgentAdminService_DisableAgent_AgentNotFound(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, nil)

	_, err := svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "no-such-agent",
	})
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
	var notFound *service.AgentAdminNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected AgentAdminNotFoundError, got %T: %v", err, err)
	}
}

func TestAgentAdminService_DisableAgent_NilRequest(t *testing.T) {
	svc := buildAdminSvc(t, nil)
	_, err := svc.DisableAgent(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

// ---------------------------------------------------------------------------
// EnableAgent
// ---------------------------------------------------------------------------

func TestAgentAdminService_EnableAgent_Success(t *testing.T) {
	ctx := context.Background()
	agent := activeAgent("t1", "agent-3")
	agent.Status = orchestration.AgentStatusDisabled
	svc := buildAdminSvc(t, agent)

	resp, err := svc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-3",
	})
	if err != nil {
		t.Fatalf("EnableAgent: %v", err)
	}
	if !resp.Enabled {
		t.Error("expected Enabled=true")
	}
	if resp.AgentID != "agent-3" {
		t.Errorf("AgentID = %q, want %q", resp.AgentID, "agent-3")
	}

	// Verify the status reverted.
	getResp, err := svc.GetAgentRegistration(ctx, &service.GetAgentRegistrationRequest{
		TenantID: "t1",
		AgentID:  "agent-3",
	})
	if err != nil {
		t.Fatalf("GetAgentRegistration: %v", err)
	}
	if getResp.Agent.Status == orchestration.AgentStatusDisabled {
		t.Error("expected status to be no longer disabled after enable")
	}
}

func TestAgentAdminService_EnableAgent_NotDisabled(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, activeAgent("t1", "agent-4"))

	_, err := svc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-4",
	})
	if err == nil {
		t.Fatal("expected AgentNotDisabledError, got nil")
	}
	var notDisabled *service.AgentNotDisabledError
	if !errors.As(err, &notDisabled) {
		t.Fatalf("expected AgentNotDisabledError, got %T: %v", err, err)
	}
}

func TestAgentAdminService_EnableAgent_AgentNotFound(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, nil)

	_, err := svc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: "t1",
		AgentID:  "no-such-agent",
	})
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
	var notFound *service.AgentAdminNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected AgentAdminNotFoundError, got %T: %v", err, err)
	}
}

func TestAgentAdminService_EnableAgent_NilRequest(t *testing.T) {
	svc := buildAdminSvc(t, nil)
	_, err := svc.EnableAgent(context.Background(), nil)
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
	disableResp, err := svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-5",
	})
	if err != nil {
		t.Fatalf("DisableAgent: %v", err)
	}
	if !disableResp.Disabled {
		t.Fatal("expected Disabled=true")
	}

	// Verify disabled.
	_, err = svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-5",
	})
	var already *service.AgentAlreadyDisabledError
	if !errors.As(err, &already) {
		t.Fatalf("expected AlreadyDisabledError on second disable, got %T: %v", err, err)
	}

	// Re-enable.
	enableResp, err := svc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-5",
	})
	if err != nil {
		t.Fatalf("EnableAgent: %v", err)
	}
	if !enableResp.Enabled {
		t.Fatal("expected Enabled=true")
	}

	// Should now be re-disableable.
	_, err = svc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-5",
	})
	if err != nil {
		t.Fatalf("expected second disable to succeed, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetAgentRegistration / ListAgentRegistrations
// ---------------------------------------------------------------------------

func TestAgentAdminService_GetAgentRegistration_Success(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, activeAgent("t1", "agent-6"))

	resp, err := svc.GetAgentRegistration(ctx, &service.GetAgentRegistrationRequest{
		TenantID: "t1",
		AgentID:  "agent-6",
	})
	if err != nil {
		t.Fatalf("GetAgentRegistration: %v", err)
	}
	if resp.Agent.ID != "agent-6" {
		t.Errorf("ID = %q, want %q", resp.Agent.ID, "agent-6")
	}
}

func TestAgentAdminService_GetAgentRegistration_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := buildAdminSvc(t, nil)

	_, err := svc.GetAgentRegistration(ctx, &service.GetAgentRegistrationRequest{
		TenantID: "t1",
		AgentID:  "missing",
	})
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
}

func TestAgentAdminService_ListAgentRegistrations_Success(t *testing.T) {
	ctx := context.Background()
	registry := orchestration.NewInMemoryAgentRegistry()
	for _, id := range []string{"a1", "a2", "a3"} {
		if err := registry.RegisterAgent(ctx, activeAgent("t1", id)); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	svc := service.NewAgentAdminService(registry)

	resp, err := svc.ListAgentRegistrations(ctx, &service.ListAgentRegistrationsRequest{
		TenantID: "t1",
	})
	if err != nil {
		t.Fatalf("ListAgentRegistrations: %v", err)
	}
	if resp.Total != 3 {
		t.Errorf("Total = %d, want 3", resp.Total)
	}
}
