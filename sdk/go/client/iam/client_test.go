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
	"testing"
)

func TestNewClient_NilTransport(t *testing.T) {
	client := NewClient(nil)
	if client != nil {
		t.Error("NewClient(nil) should return nil")
	}
}

func TestNewClient_ValidTransport(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client with valid transport")
	}
}

func TestClient_Tenant(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	tenantClient := client.Tenant()
	if tenantClient == nil {
		t.Error("Tenant() should return a non-nil TenantClient")
	}
}

func TestClient_User(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	userClient := client.User()
	if userClient == nil {
		t.Error("User() should return a non-nil UserClient")
	}
}

func TestClient_Group(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	groupClient := client.Group()
	if groupClient == nil {
		t.Error("Group() should return a non-nil GroupClient")
	}
}

func TestClient_Role(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	roleClient := client.Role()
	if roleClient == nil {
		t.Error("Role() should return a non-nil RoleClient")
	}
}

func TestClient_Policy(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	policyClient := client.Policy()
	if policyClient == nil {
		t.Error("Policy() should return a non-nil PolicyClient")
	}
}

func TestClient_PEP(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	pepClient := client.PEP()
	if pepClient == nil {
		t.Error("PEP() should return a non-nil PEPClient")
	}
}

func TestClient_App(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	appClient := client.App()
	if appClient == nil {
		t.Error("App() should return a non-nil AppClient")
	}
}

func TestClient_Session(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	sessionClient := client.Session()
	if sessionClient == nil {
		t.Error("Session() should return a non-nil SessionClient")
	}
}

func TestClient_Audit(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	auditClient := client.Audit()
	if auditClient == nil {
		t.Error("Audit() should return a non-nil AuditClient")
	}
}

func TestClient_GDPR(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	gdprClient := client.GDPR()
	if gdprClient == nil {
		t.Error("GDPR() should return a non-nil GDPRClient")
	}
}

func TestClient_Admin(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	adminClient := client.Admin()
	if adminClient == nil {
		t.Error("Admin() should return a non-nil AdminClient")
	}
}

func TestClient_AllAccessorsReturnConsistentInstances(t *testing.T) {
	mt := &mockTransport{}
	client := NewClient(mt)
	if client == nil {
		t.Fatal("NewClient should return a non-nil client")
	}

	// Verify that calling accessors multiple times returns the same instance
	tenant1 := client.Tenant()
	tenant2 := client.Tenant()
	if tenant1 != tenant2 {
		t.Error("Tenant() should return the same instance on multiple calls")
	}

	user1 := client.User()
	user2 := client.User()
	if user1 != user2 {
		t.Error("User() should return the same instance on multiple calls")
	}

	group1 := client.Group()
	group2 := client.Group()
	if group1 != group2 {
		t.Error("Group() should return the same instance on multiple calls")
	}

	role1 := client.Role()
	role2 := client.Role()
	if role1 != role2 {
		t.Error("Role() should return the same instance on multiple calls")
	}

	policy1 := client.Policy()
	policy2 := client.Policy()
	if policy1 != policy2 {
		t.Error("Policy() should return the same instance on multiple calls")
	}

	pep1 := client.PEP()
	pep2 := client.PEP()
	if pep1 != pep2 {
		t.Error("PEP() should return the same instance on multiple calls")
	}

	app1 := client.App()
	app2 := client.App()
	if app1 != app2 {
		t.Error("App() should return the same instance on multiple calls")
	}

	session1 := client.Session()
	session2 := client.Session()
	if session1 != session2 {
		t.Error("Session() should return the same instance on multiple calls")
	}

	audit1 := client.Audit()
	audit2 := client.Audit()
	if audit1 != audit2 {
		t.Error("Audit() should return the same instance on multiple calls")
	}

	gdpr1 := client.GDPR()
	gdpr2 := client.GDPR()
	if gdpr1 != gdpr2 {
		t.Error("GDPR() should return the same instance on multiple calls")
	}

	admin1 := client.Admin()
	admin2 := client.Admin()
	if admin1 != admin2 {
		t.Error("Admin() should return the same instance on multiple calls")
	}
}
