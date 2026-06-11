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

package iam

import (
	"context"
	"testing"
)

// TestLogDualIdentity_CarriesBothIdentities verifies the audit entry stamps
// BOTH the service identity (cert SPIFFE/DN) and the user identity
// (sub/tenant_id/scp/roles/jti), and that the tenant is stamped on the event
// (AU-2/AU-3).
func TestLogDualIdentity_CarriesBothIdentities(t *testing.T) {
	logger, err := NewAuditLogger()
	if err != nil {
		t.Fatalf("NewAuditLogger() error = %v", err)
	}
	defer logger.Close()

	ctx := context.Background()
	id := DualIdentity{
		ServiceIdentity: "spiffe://example.org/tenant/acme/svc/resource-server",
		Subject:         "user-9",
		Username:        "Nine",
		TenantID:        "acme",
		ClientID:        "app_acme_rs",
		JTI:             "jti-xyz",
		Scopes:          []string{"agent:runtime:spawn"},
		Roles:           []string{"operator"},
	}

	if err := LogDualIdentity(ctx, logger, id,
		AuditEventAuthorization, AuditCategorySecurity,
		"oauth2.introspect", AuditOutcomeSuccess,
		map[string]interface{}{"token_type": "Bearer"}); err != nil {
		t.Fatalf("LogDualIdentity() error = %v", err)
	}

	events, total, err := logger.Query(ctx, &AuditFilter{TenantID: "acme"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("expected 1 event, got total=%d len=%d", total, len(events))
	}

	ev := events[0]
	if ev.TenantID != "acme" {
		t.Errorf("event tenant_id = %q, want acme", ev.TenantID)
	}
	if ev.Actor == nil {
		t.Fatalf("event actor must not be nil")
	}
	// Service identity.
	if ev.Actor.ServiceIdentity != id.ServiceIdentity {
		t.Errorf("service identity = %q, want %q", ev.Actor.ServiceIdentity, id.ServiceIdentity)
	}
	// User identity.
	if ev.Actor.UserID != "user-9" {
		t.Errorf("user id = %q, want user-9", ev.Actor.UserID)
	}
	if ev.Actor.JTI != "jti-xyz" {
		t.Errorf("jti = %q, want jti-xyz", ev.Actor.JTI)
	}
	if len(ev.Actor.Scopes) != 1 || ev.Actor.Scopes[0] != "agent:runtime:spawn" {
		t.Errorf("scopes = %v, want [agent:runtime:spawn]", ev.Actor.Scopes)
	}
	if len(ev.Actor.Roles) != 1 || ev.Actor.Roles[0] != "operator" {
		t.Errorf("roles = %v, want [operator]", ev.Actor.Roles)
	}
	if ev.Action != "oauth2.introspect" {
		t.Errorf("action = %q", ev.Action)
	}
	if ev.Outcome != AuditOutcomeSuccess {
		t.Errorf("outcome = %q", ev.Outcome)
	}
}

// TestLogDualIdentity_TenantFromContext verifies the tenant falls back to the
// context tenant when the DualIdentity does not carry one, so the event still
// routes to a tenant partition.
func TestLogDualIdentity_TenantFromContext(t *testing.T) {
	logger, err := NewAuditLogger()
	if err != nil {
		t.Fatalf("NewAuditLogger() error = %v", err)
	}
	defer logger.Close()

	ctx := ContextWithTenant(context.Background(), "ctx-tenant")
	id := DualIdentity{
		ServiceIdentity: "CN=rs.example.com",
		Subject:         "user-ctx",
	}

	if err := LogDualIdentity(ctx, logger, id,
		AuditEventAuthentication, AuditCategoryAuthentication,
		"iam.login.finish", AuditOutcomeSuccess, nil); err != nil {
		t.Fatalf("LogDualIdentity() error = %v", err)
	}

	events, _, err := logger.Query(ctx, &AuditFilter{TenantID: "ctx-tenant"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event in ctx-tenant, got %d", len(events))
	}
	if events[0].TenantID != "ctx-tenant" {
		t.Errorf("tenant = %q, want ctx-tenant", events[0].TenantID)
	}
}

// TestLogDualIdentity_Errors verifies fail-closed behaviour: a nil logger and a
// missing tenant both return errors rather than dropping the security event.
func TestLogDualIdentity_Errors(t *testing.T) {
	// Nil logger.
	if err := LogDualIdentity(context.Background(), nil, DualIdentity{TenantID: "t"},
		AuditEventAuthentication, AuditCategoryAuthentication,
		"x", AuditOutcomeSuccess, nil); err == nil {
		t.Errorf("expected error for nil logger")
	}

	// No tenant anywhere.
	logger, err := NewAuditLogger()
	if err != nil {
		t.Fatalf("NewAuditLogger() error = %v", err)
	}
	defer logger.Close()
	if err := LogDualIdentity(context.Background(), logger, DualIdentity{Subject: "u"},
		AuditEventAuthentication, AuditCategoryAuthentication,
		"x", AuditOutcomeSuccess, nil); err == nil {
		t.Errorf("expected error when no tenant is available")
	}
}

// TestLogDualIdentity_CarriesNetworkSource verifies the audit entry stamps the
// network source (client IP) and user agent supplied on the DualIdentity, so a
// dual-identity event records WHERE the request originated (AU-3 network
// source) in addition to WHO performed it.
func TestLogDualIdentity_CarriesNetworkSource(t *testing.T) {
	logger, err := NewAuditLogger()
	if err != nil {
		t.Fatalf("NewAuditLogger() error = %v", err)
	}
	defer logger.Close()

	ctx := context.Background()
	id := DualIdentity{
		ServiceIdentity: "CN=rs.example.com",
		Subject:         "user-net",
		TenantID:        "acme",
		IPAddress:       "203.0.113.7",
		UserAgent:       "quicbaas-agent/1.2.3",
	}

	if err := LogDualIdentity(ctx, logger, id,
		AuditEventAuthentication, AuditCategoryAuthentication,
		"oauth2.token", AuditOutcomeSuccess, nil); err != nil {
		t.Fatalf("LogDualIdentity() error = %v", err)
	}

	events, _, err := logger.Query(ctx, &AuditFilter{TenantID: "acme"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].IPAddress != "203.0.113.7" {
		t.Errorf("ip address = %q, want 203.0.113.7", events[0].IPAddress)
	}
	if events[0].UserAgent != "quicbaas-agent/1.2.3" {
		t.Errorf("user agent = %q, want quicbaas-agent/1.2.3", events[0].UserAgent)
	}
}

// TestLogDualIdentity_NetworkSourceOptional verifies that omitting the network
// source leaves the fields empty rather than failing (the fields are optional).
func TestLogDualIdentity_NetworkSourceOptional(t *testing.T) {
	logger, err := NewAuditLogger()
	if err != nil {
		t.Fatalf("NewAuditLogger() error = %v", err)
	}
	defer logger.Close()

	ctx := context.Background()
	id := DualIdentity{Subject: "user-x", TenantID: "acme"}

	if err := LogDualIdentity(ctx, logger, id,
		AuditEventAuthentication, AuditCategoryAuthentication,
		"oauth2.token", AuditOutcomeSuccess, nil); err != nil {
		t.Fatalf("LogDualIdentity() error = %v", err)
	}

	events, _, err := logger.Query(ctx, &AuditFilter{TenantID: "acme"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].IPAddress != "" {
		t.Errorf("ip address = %q, want empty", events[0].IPAddress)
	}
	if events[0].UserAgent != "" {
		t.Errorf("user agent = %q, want empty", events[0].UserAgent)
	}
}
