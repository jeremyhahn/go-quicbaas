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

// Package service internal tests for Gap 2: DisableAgent side-effects.
// This file is in package service (not service_test) to access unexported
// setters (SetAppClientRevoker, SetStreamCloser, SetAuditLogger). The mock
// AuditLogger implements the full pkg/iam.AuditLogger interface (Query,
// GetEvent, DeleteOlderThan, Close), which is only available under the
// `iam` build tag — so this file is gated to match.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Test Doubles
// =============================================================================

// mockAppRevoker records DeleteApp calls for assertion.
type mockAppRevoker struct {
	calls     []string
	returnErr error
}

func (m *mockAppRevoker) DeleteApp(_ context.Context, clientID string) error {
	m.calls = append(m.calls, clientID)
	return m.returnErr
}

// mockStreamCloser records CloseAgentStreams calls.
type mockStreamCloser struct {
	calls     []string
	returnErr error
}

func (m *mockStreamCloser) CloseAgentStreams(_ context.Context, agentID string) error {
	m.calls = append(m.calls, agentID)
	return m.returnErr
}

// mockAuditLogger records Log calls.
type mockAuditLogger struct {
	events []*iam.AuditEvent
}

func (m *mockAuditLogger) Log(_ context.Context, event *iam.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockAuditLogger) LogAsync(event *iam.AuditEvent) {
	m.events = append(m.events, event)
}

func (m *mockAuditLogger) Query(_ context.Context, _ *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	return nil, 0, nil
}

func (m *mockAuditLogger) GetEvent(_ context.Context, _, _ string) (*iam.AuditEvent, error) {
	return nil, nil
}

func (m *mockAuditLogger) DeleteOlderThan(_ context.Context, _ string, _ time.Time) (int, error) {
	return 0, nil
}

func (m *mockAuditLogger) Close() error {
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

func newAgentWithMeta(tenantID, agentID string, meta map[string]string) *orchestration.AgentRegistration {
	return &orchestration.AgentRegistration{
		ID:        agentID,
		TenantID:  tenantID,
		Name:      "agent-" + agentID,
		Host:      "localhost:9001",
		AgentType: "worker",
		Status:    orchestration.AgentStatusRunning,
		Metadata:  meta,
	}
}

func buildAdminSvcInternal(t *testing.T, agent *orchestration.AgentRegistration) *agentAdminServiceImpl {
	t.Helper()
	registry := orchestration.NewInMemoryAgentRegistry()
	if agent != nil {
		if err := registry.RegisterAgent(context.Background(), agent); err != nil {
			t.Fatalf("buildAdminSvcInternal: register: %v", err)
		}
	}
	return &agentAdminServiceImpl{registry: registry}
}

// =============================================================================
// Gap 2 Tests: DisableAgent side-effects
// =============================================================================

// TestDisableAgent_OAuth2Revoke_WithClientID verifies that when an agent has
// oauth2_client_id in metadata, DisableAgent calls DeleteApp with that ID.
func TestDisableAgent_OAuth2Revoke_WithClientID(t *testing.T) {
	agent := newAgentWithMeta("t1", "agent-x", map[string]string{
		"oauth2_client_id": "client-abc",
	})
	svc := buildAdminSvcInternal(t, agent)
	revoker := &mockAppRevoker{}
	svc.SetAppClientRevoker(revoker)

	_, err := svc.DisableAgent(context.Background(), &DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-x",
	})
	if err != nil {
		t.Fatalf("DisableAgent: %v", err)
	}

	if len(revoker.calls) == 0 {
		t.Fatal("expected at least one DeleteApp call, got none")
	}
	if revoker.calls[0] != "client-abc" {
		t.Errorf("expected DeleteApp(%q), got %q", "client-abc", revoker.calls[0])
	}
}

// TestDisableAgent_OAuth2Revoke_FallbackToAgentID verifies that when no
// oauth2_client_id metadata key is set, the agent ID is used as the client ID.
func TestDisableAgent_OAuth2Revoke_FallbackToAgentID(t *testing.T) {
	agent := newAgentWithMeta("t1", "agent-y", nil)
	svc := buildAdminSvcInternal(t, agent)
	revoker := &mockAppRevoker{}
	svc.SetAppClientRevoker(revoker)

	_, err := svc.DisableAgent(context.Background(), &DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-y",
	})
	if err != nil {
		t.Fatalf("DisableAgent: %v", err)
	}

	if len(revoker.calls) == 0 {
		t.Fatal("expected DeleteApp call with agent ID fallback, got none")
	}
	if revoker.calls[0] != "agent-y" {
		t.Errorf("expected DeleteApp(%q), got %q", "agent-y", revoker.calls[0])
	}
}

// TestDisableAgent_MtlsClientID_AlsoRevoked verifies that when an agent has
// mtls_client_id metadata, a second DeleteApp call is made for the mTLS app.
func TestDisableAgent_MtlsClientID_AlsoRevoked(t *testing.T) {
	agent := newAgentWithMeta("t1", "agent-z", map[string]string{
		"oauth2_client_id": "client-main",
		"mtls_client_id":   "client-mtls",
	})
	svc := buildAdminSvcInternal(t, agent)
	revoker := &mockAppRevoker{}
	svc.SetAppClientRevoker(revoker)

	_, err := svc.DisableAgent(context.Background(), &DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-z",
	})
	if err != nil {
		t.Fatalf("DisableAgent: %v", err)
	}

	// Expect two calls: one for oauth2_client_id and one for mtls_client_id.
	if len(revoker.calls) < 2 {
		t.Fatalf("expected >=2 DeleteApp calls (oauth2+mtls), got %d: %v", len(revoker.calls), revoker.calls)
	}
	found := map[string]bool{}
	for _, c := range revoker.calls {
		found[c] = true
	}
	if !found["client-main"] {
		t.Error("expected DeleteApp('client-main')")
	}
	if !found["client-mtls"] {
		t.Error("expected DeleteApp('client-mtls')")
	}
}

// TestDisableAgent_OAuth2RevokeFails_StillDisables verifies that a non-fatal
// error from DeleteApp does not cause DisableAgent to fail.
func TestDisableAgent_OAuth2RevokeFails_StillDisables(t *testing.T) {
	agent := newAgentWithMeta("t1", "agent-w", nil)
	svc := buildAdminSvcInternal(t, agent)
	revoker := &mockAppRevoker{returnErr: errors.New("iam unavailable")}
	svc.SetAppClientRevoker(revoker)

	resp, err := svc.DisableAgent(context.Background(), &DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-w",
	})
	if err != nil {
		t.Fatalf("DisableAgent should succeed despite revoke failure, got: %v", err)
	}
	if !resp.Disabled {
		t.Error("expected Disabled=true even when OAuth2 revoke fails")
	}
}

// TestDisableAgent_StreamCloser_Called verifies that DisableAgent calls
// CloseAgentStreams when a stream closer is configured.
func TestDisableAgent_StreamCloser_Called(t *testing.T) {
	agent := newAgentWithMeta("t1", "agent-s", nil)
	svc := buildAdminSvcInternal(t, agent)
	closer := &mockStreamCloser{}
	svc.SetStreamCloser(closer)

	_, err := svc.DisableAgent(context.Background(), &DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-s",
	})
	if err != nil {
		t.Fatalf("DisableAgent: %v", err)
	}

	if len(closer.calls) == 0 {
		t.Fatal("expected CloseAgentStreams to be called, got none")
	}
	if closer.calls[0] != "agent-s" {
		t.Errorf("CloseAgentStreams called with %q, want %q", closer.calls[0], "agent-s")
	}
}

// TestDisableAgent_AuditEvent_Emitted verifies that an "agent.disabled" audit
// event with AuditOutcomeSuccess is emitted after a successful disable.
func TestDisableAgent_AuditEvent_Emitted(t *testing.T) {
	agent := newAgentWithMeta("t1", "agent-audit", nil)
	svc := buildAdminSvcInternal(t, agent)
	logger := &mockAuditLogger{}
	svc.SetAuditLogger(logger)

	_, err := svc.DisableAgent(context.Background(), &DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-audit",
	})
	if err != nil {
		t.Fatalf("DisableAgent: %v", err)
	}

	if len(logger.events) == 0 {
		t.Fatal("expected at least one audit event, got none")
	}

	// Find the "agent.disabled" event.
	var found bool
	for _, e := range logger.events {
		if e.Action == "agent.disabled" {
			found = true
			if e.Outcome != iam.AuditOutcomeSuccess {
				t.Errorf("expected Outcome=success, got %q", e.Outcome)
			}
			if e.Resource == nil || e.Resource.ID != "agent-audit" {
				t.Errorf("expected resource.ID=%q, got %+v", "agent-audit", e.Resource)
			}
		}
	}
	if !found {
		actions := make([]string, len(logger.events))
		for i, e := range logger.events {
			actions[i] = e.Action
		}
		t.Errorf("'agent.disabled' event not found; got actions: %v", actions)
	}
}

// TestEnableAgent_AuditEvent_Emitted verifies that an "agent.enabled" audit
// event is emitted after a successful enable.
func TestEnableAgent_AuditEvent_Emitted(t *testing.T) {
	agent := newAgentWithMeta("t1", "agent-en", nil)
	agent.Status = orchestration.AgentStatusDisabled
	svc := buildAdminSvcInternal(t, agent)
	logger := &mockAuditLogger{}
	svc.SetAuditLogger(logger)

	_, err := svc.EnableAgent(context.Background(), &EnableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-en",
	})
	if err != nil {
		t.Fatalf("EnableAgent: %v", err)
	}

	var found bool
	for _, e := range logger.events {
		if e.Action == "agent.enabled" {
			found = true
			if e.Outcome != iam.AuditOutcomeSuccess {
				t.Errorf("expected Outcome=success, got %q", e.Outcome)
			}
		}
	}
	if !found {
		t.Error("'agent.enabled' audit event not found")
	}
}

// TestDisableAgent_NoRevokerNoCloser_DoesNotPanic verifies graceful degradation
// when neither OAuth2 revoker nor stream closer is configured.
func TestDisableAgent_NoRevokerNoCloser_DoesNotPanic(t *testing.T) {
	agent := newAgentWithMeta("t1", "agent-bare", nil)
	svc := buildAdminSvcInternal(t, agent)
	// No revoker, no closer, no audit logger.

	resp, err := svc.DisableAgent(context.Background(), &DisableAgentRequest{
		TenantID: "t1",
		AgentID:  "agent-bare",
	})
	if err != nil {
		t.Fatalf("DisableAgent without optionals: %v", err)
	}
	if !resp.Disabled {
		t.Error("expected Disabled=true")
	}
}
