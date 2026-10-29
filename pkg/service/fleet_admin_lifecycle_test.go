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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// adminWithDoubles builds a fleet admin service over an in-memory registry
// holding agent, with a recording revoker and audit logger wired in.
func adminWithDoubles(t *testing.T, agent *orchestration.FleetNode) (*fleetAdminService, *mockAppRevoker, *mockAuditLogger) {
	t.Helper()
	svc := buildAdminSvcInternal(t, agent)
	revoker := &mockAppRevoker{}
	audit := &mockAuditLogger{}
	svc.SetAppClientRevoker(revoker)
	svc.SetAuditLogger(audit)
	return svc, revoker, audit
}

// countActions returns how many recorded audit events carry action.
func countActions(audit *mockAuditLogger, action string) int {
	n := 0
	for _, ev := range audit.events {
		if ev.Action == action {
			n++
		}
	}
	return n
}

func TestDisableAgent_RegistrationReadsDisabledAndAuditNamesTheAgent(t *testing.T) {
	ctx := context.Background()
	svc, _, audit := adminWithDoubles(t, newAgentWithMeta("t1", "agent-d", nil))

	if _, err := svc.DisableFleetNode(ctx, &DisableFleetNodeRequest{TenantID: "t1", NodeID: "agent-d"}); err != nil {
		t.Fatalf("DisableFleetNode: %v", err)
	}

	got, err := svc.GetFleetNodeRegistration(ctx, &GetFleetNodeRegistrationRequest{TenantID: "t1", NodeID: "agent-d"})
	if err != nil {
		t.Fatalf("GetFleetNodeRegistration: %v", err)
	}
	if got.Agent.Status != orchestration.FleetNodeStatusDisabled {
		t.Fatalf("status = %q, want %q", got.Agent.Status, orchestration.FleetNodeStatusDisabled)
	}

	for _, ev := range audit.events {
		if ev.Action != "agent.disabled" {
			continue
		}
		if ev.Resource == nil || ev.Resource.Type != "agent" || ev.Resource.ID != "agent-d" {
			t.Fatalf("agent.disabled resource = %+v, want type agent, id agent-d", ev.Resource)
		}
		return
	}
	t.Fatal("no agent.disabled audit event was emitted")
}

func TestDisableAgent_AlreadyDisabledErrorNamesTheNode(t *testing.T) {
	agent := newAgentWithMeta("t1", "agent-twice", nil)
	agent.Status = orchestration.FleetNodeStatusDisabled
	svc, _, _ := adminWithDoubles(t, agent)

	_, err := svc.DisableFleetNode(context.Background(), &DisableFleetNodeRequest{TenantID: "t1", NodeID: "agent-twice"})
	var already *FleetNodeAlreadyDisabledError
	if !errors.As(err, &already) {
		t.Fatalf("want FleetNodeAlreadyDisabledError, got %T: %v", err, err)
	}
	if already.TenantID != "t1" || already.NodeID != "agent-twice" {
		t.Fatalf("error names tenant %q node %q, want t1 agent-twice", already.TenantID, already.NodeID)
	}
}

// Disabling an unknown agent must have no side effects: nothing is revoked
// and nothing is audited as disabled.
func TestDisableAgent_UnknownAgentHasNoSideEffects(t *testing.T) {
	svc, revoker, audit := adminWithDoubles(t, nil)

	_, err := svc.DisableFleetNode(context.Background(), &DisableFleetNodeRequest{TenantID: "t1", NodeID: "ghost"})
	var notFound *FleetAdminNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("want FleetAdminNotFoundError, got %T: %v", err, err)
	}
	if len(revoker.calls) != 0 {
		t.Fatalf("revoked %v for an unknown agent", revoker.calls)
	}
	if n := countActions(audit, "agent.disabled"); n != 0 {
		t.Fatalf("%d agent.disabled events for an unknown agent", n)
	}
}

// A disable, enable, disable cycle audits each successful transition once.
func TestDisableEnableCycle_AuditsEachTransition(t *testing.T) {
	ctx := context.Background()
	svc, _, audit := adminWithDoubles(t, newAgentWithMeta("t1", "agent-c", nil))

	if _, err := svc.DisableFleetNode(ctx, &DisableFleetNodeRequest{TenantID: "t1", NodeID: "agent-c"}); err != nil {
		t.Fatalf("first disable: %v", err)
	}
	if _, err := svc.EnableFleetNode(ctx, &EnableFleetNodeRequest{TenantID: "t1", NodeID: "agent-c"}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := svc.DisableFleetNode(ctx, &DisableFleetNodeRequest{TenantID: "t1", NodeID: "agent-c"}); err != nil {
		t.Fatalf("second disable: %v", err)
	}

	if n := countActions(audit, "agent.disabled"); n != 2 {
		t.Fatalf("agent.disabled events = %d, want 2", n)
	}
	if n := countActions(audit, "agent.enabled"); n != 1 {
		t.Fatalf("agent.enabled events = %d, want 1", n)
	}
}
