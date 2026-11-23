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

package iam_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/permission"
)

func TestTenantAgentGrant_IsPinned(t *testing.T) {
	t.Parallel()

	grant := iam.TenantAgentGrant()
	want := append(iam.AgentSessionGrant(), "agents:read", "sdlc:read", "spend_report:read")
	if len(want) != 7 {
		t.Fatalf("the session floor changed size; re-pin this test deliberately: %v", want)
	}
	if len(grant) != len(want) {
		t.Fatalf("grant = %v, want %v", grant, want)
	}
	for i := range want {
		if grant[i] != want[i] {
			t.Fatalf("grant[%d] = %q, want %q (grant %v)", i, grant[i], want[i], grant)
		}
	}

	held := make(map[string]bool, len(grant))
	for _, p := range grant {
		held[p] = true
	}
	for _, p := range iam.AgentSessionGrant() {
		if !held[p] {
			t.Errorf("grant lacks the session floor entry %q", p)
		}
	}

	extras := grant[len(iam.AgentSessionGrant()):]
	registered := []string{
		string(permission.AgentsRead),
		string(permission.SDLCRead),
		string(permission.SpendReportRead),
	}
	for i, p := range registered {
		if extras[i] != p {
			t.Errorf("extra[%d] = %q, want the registered %q", i, extras[i], p)
		}
	}

	for _, p := range grant {
		for _, suffix := range []string{":write", ":delete", ":bind", ":unbind", ":issue_create"} {
			if strings.HasSuffix(p, suffix) {
				t.Errorf("grant holds the write verb %q", p)
			}
		}
		for _, prefix := range []string{"iam:", "secrets:", "kms:", "tenant:", "agent_accounts:", "barrier:"} {
			if strings.HasPrefix(p, prefix) {
				t.Errorf("grant holds the administrative verb %q", p)
			}
		}
	}
}

func TestTenantAgentRoleID_IsPerTenant(t *testing.T) {
	t.Parallel()

	if got := iam.TenantAgentRoleID("automatethethings"); got != "role-tenant-agent-automatethethings" {
		t.Fatalf("TenantAgentRoleID = %q", got)
	}
	if iam.TenantAgentRoleID("tenant-a") == iam.TenantAgentRoleID("tenant-b") {
		t.Fatal("two tenants share one Tenant Agent role ID")
	}
	if iam.TenantAgentRoleID("tenant-a") == iam.AgentSessionRoleID("tenant-a") {
		t.Fatal("the Tenant Agent role ID collides with the session role ID")
	}
}

func TestEnrollTenantAgentIdentity_ConfersTheGrantOnTheCertPrincipal(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "tenant-agent-automatethethings", nil)

	principal, err := store.EnrollTenantAgentIdentity(ctx, "automatethethings", cert)
	if err != nil {
		t.Fatalf("EnrollTenantAgentIdentity() error = %v", err)
	}
	if principal.Username != "tenant-agent-automatethethings" {
		t.Fatalf("username = %q, want the certificate CN", principal.Username)
	}
	if principal.TenantID != "automatethethings" {
		t.Fatalf("tenant = %q, want automatethethings", principal.TenantID)
	}
	roleID := iam.TenantAgentRoleID("automatethethings")
	if len(principal.Roles) != 1 || principal.Roles[0] != roleID {
		t.Fatalf("roles = %v, want exactly [%s]", principal.Roles, roleID)
	}
	if len(principal.AuthMethods) != 1 || principal.AuthMethods[0] != iam.AuthMethodMTLS {
		t.Fatalf("auth methods = %v, want exactly [%s]", principal.AuthMethods, iam.AuthMethodMTLS)
	}
	if !boundToCert(principal, cert) {
		t.Fatal("the record must be bound to the certificate it was enrolled from")
	}

	role, err := memory.GetRole(ctx, "automatethethings", roleID)
	if err != nil {
		t.Fatalf("GetRole() error = %v", err)
	}
	if role.Name != iam.TenantAgentRoleName {
		t.Errorf("role name = %q, want %q", role.Name, iam.TenantAgentRoleName)
	}
	assertSamePermissions(t, role.Permissions, iam.TenantAgentGrant())
}

func TestEnrollTenantAgentIdentity_RejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tenantID string
		// commonName is "" to mean "pass a nil certificate".
		commonName string
	}{
		{name: "empty tenant", tenantID: "", commonName: "tenant-agent-1"},
		{name: "nil certificate", tenantID: "automatethethings", commonName: ""},
		{name: "certificate with no common name", tenantID: "automatethethings", commonName: " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, memory := newAgentEnrollmentStore(t)
			cert := newTestCert(t, strings.TrimSpace(tt.commonName), nil)
			if tt.commonName == "" {
				cert = nil
			}
			if _, err := store.EnrollTenantAgentIdentity(context.Background(), tt.tenantID, cert); err == nil {
				t.Fatal("expected the enrollment to be refused")
			}
			users, err := memory.ListUsers(context.Background(), "automatethethings", iam.ListOptions{Limit: 10})
			if err != nil {
				t.Fatalf("ListUsers() error = %v", err)
			}
			if len(users) != 0 {
				t.Fatalf("a refused enrollment wrote %d user records", len(users))
			}
		})
	}
}

func TestEnrollTenantAgentIdentity_Converges(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "tenant-agent-converge", nil)

	first, err := store.EnrollTenantAgentIdentity(ctx, "automatethethings", cert)
	if err != nil {
		t.Fatalf("first enrollment: %v", err)
	}
	second, err := store.EnrollTenantAgentIdentity(ctx, "automatethethings", cert)
	if err != nil {
		t.Fatalf("second enrollment: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("user IDs differ: %d then %d", first.ID, second.ID)
	}
	if len(second.Roles) != 1 || second.Roles[0] != iam.TenantAgentRoleID("automatethethings") {
		t.Fatalf("roles = %v, want exactly the Tenant Agent role", second.Roles)
	}
	roles, err := memory.ListRoles(ctx, "automatethethings", iam.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	count := 0
	for _, role := range roles {
		if role.Name == iam.TenantAgentRoleName {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("tenant holds %d Tenant Agent roles, want exactly 1", count)
	}
}

func TestEnrollTenantAgentIdentity_RefusesToPromoteAnotherPrincipal(t *testing.T) {
	t.Parallel()

	t.Run("a session principal", func(t *testing.T) {
		t.Parallel()

		store, memory := newAgentEnrollmentStore(t)
		ctx := context.Background()
		cert := newTestCert(t, "tenant-agent-collide", nil)
		if _, err := store.EnrollAgentSessionIdentity(ctx, "automatethethings", cert); err != nil {
			t.Fatalf("EnrollAgentSessionIdentity() error = %v", err)
		}

		_, err := store.EnrollTenantAgentIdentity(ctx, "automatethethings", cert)
		var notTA *iam.NotATenantAgentIdentityError
		if !errors.As(err, &notTA) {
			t.Fatalf("error = %v, want *NotATenantAgentIdentityError", err)
		}
		after, err := memory.GetUserByUsername(ctx, "automatethethings", "tenant-agent-collide")
		if err != nil {
			t.Fatalf("GetUserByUsername() error = %v", err)
		}
		sessionRole := iam.AgentSessionRoleID("automatethethings")
		if len(after.Roles) != 1 || after.Roles[0] != sessionRole {
			t.Fatalf("roles = %v, want exactly [%s]", after.Roles, sessionRole)
		}
	})

	t.Run("a fleet runtime principal", func(t *testing.T) {
		t.Parallel()

		store, memory := newAgentEnrollmentStore(t)
		ctx := context.Background()
		cert := newTestCert(t, "tenant-agent-fleet", nil)
		if _, err := store.EnrollFleetNodeCertIdentity(ctx, "automatethethings", iam.FleetNodeCertIdentityOptions{
			Certificate: cert,
			Permissions: iam.FleetRuntimeGrant(),
		}); err != nil {
			t.Fatalf("EnrollFleetNodeCertIdentity() error = %v", err)
		}

		_, err := store.EnrollTenantAgentIdentity(ctx, "automatethethings", cert)
		var notTA *iam.NotATenantAgentIdentityError
		if !errors.As(err, &notTA) {
			t.Fatalf("error = %v, want *NotATenantAgentIdentityError", err)
		}
		after, err := memory.GetUserByUsername(ctx, "automatethethings", "tenant-agent-fleet")
		if err != nil {
			t.Fatalf("GetUserByUsername() error = %v", err)
		}
		runtimeRole := iam.FleetRuntimeRoleID("automatethethings")
		if len(after.Roles) != 1 || after.Roles[0] != runtimeRole {
			t.Fatalf("roles = %v, want exactly [%s]", after.Roles, runtimeRole)
		}
	})
}

func TestEnrollTenantAgentIdentity_IsIsolatedPerTenant(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "tenant-agent-shared", nil)

	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if _, err := store.EnrollTenantAgentIdentity(ctx, tenant, cert); err != nil {
			t.Fatalf("enroll into %s: %v", tenant, err)
		}
	}
	a, err := memory.GetUserByUsername(ctx, "tenant-a", "tenant-agent-shared")
	if err != nil {
		t.Fatalf("tenant-a lookup: %v", err)
	}
	b, err := memory.GetUserByUsername(ctx, "tenant-b", "tenant-agent-shared")
	if err != nil {
		t.Fatalf("tenant-b lookup: %v", err)
	}
	if a.ID == b.ID {
		t.Fatal("two tenants share one Tenant Agent record")
	}
	if len(a.Roles) != 1 || a.Roles[0] != iam.TenantAgentRoleID("tenant-a") {
		t.Fatalf("tenant-a roles = %v", a.Roles)
	}
	if len(b.Roles) != 1 || b.Roles[0] != iam.TenantAgentRoleID("tenant-b") {
		t.Fatalf("tenant-b roles = %v", b.Roles)
	}
}

func TestEnrollTenantAgentIdentity_WillNotResurrectAWithdrawnIdentity(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "tenant-agent-withdrawn", nil)

	enrolled, err := store.EnrollTenantAgentIdentity(ctx, "automatethethings", cert)
	if err != nil {
		t.Fatalf("first enrollment: %v", err)
	}
	enrolled.Lifecycle = iam.LifecycleDisabled
	if err := memory.UpdateUser(ctx, enrolled); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if _, err := store.EnrollTenantAgentIdentity(ctx, "automatethethings", cert); err == nil {
		t.Fatal("a repeated enrollment re-enabled an identity an administrator disabled")
	}
	after, err := memory.GetUserByUsername(ctx, "automatethethings", "tenant-agent-withdrawn")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if after.CanAuthenticate() {
		t.Fatalf("the disabled identity is authenticating again, lifecycle = %q", after.Lifecycle)
	}
}

func TestRevokeTenantAgentIdentity(t *testing.T) {
	t.Parallel()

	t.Run("removes the record", func(t *testing.T) {
		t.Parallel()

		store, memory := newAgentEnrollmentStore(t)
		ctx := context.Background()
		cert := newTestCert(t, "tenant-agent-revoke", nil)
		if _, err := store.EnrollTenantAgentIdentity(ctx, "automatethethings", cert); err != nil {
			t.Fatalf("enroll: %v", err)
		}
		if err := store.RevokeTenantAgentIdentity(ctx, "automatethethings", "tenant-agent-revoke"); err != nil {
			t.Fatalf("RevokeTenantAgentIdentity() error = %v", err)
		}
		if _, err := memory.GetUserByUsername(ctx, "automatethethings", "tenant-agent-revoke"); err == nil {
			t.Fatal("the revoked record still exists")
		}
	})

	t.Run("is idempotent when the record is missing", func(t *testing.T) {
		t.Parallel()

		store, _ := newAgentEnrollmentStore(t)
		if err := store.RevokeTenantAgentIdentity(context.Background(), "automatethethings", "tenant-agent-absent"); err != nil {
			t.Fatalf("RevokeTenantAgentIdentity() error = %v, want nil", err)
		}
	})

	t.Run("refuses and keeps a session principal", func(t *testing.T) {
		t.Parallel()

		store, memory := newAgentEnrollmentStore(t)
		ctx := context.Background()
		cert := newTestCert(t, "tenant-agent-session", nil)
		if _, err := store.EnrollAgentSessionIdentity(ctx, "automatethethings", cert); err != nil {
			t.Fatalf("enroll session: %v", err)
		}
		err := store.RevokeTenantAgentIdentity(ctx, "automatethethings", "tenant-agent-session")
		var notTA *iam.NotATenantAgentIdentityError
		if !errors.As(err, &notTA) {
			t.Fatalf("error = %v, want *NotATenantAgentIdentityError", err)
		}
		if _, err := memory.GetUserByUsername(ctx, "automatethethings", "tenant-agent-session"); err != nil {
			t.Fatalf("the session principal was removed: %v", err)
		}
	})

	t.Run("refuses a record with an extra role", func(t *testing.T) {
		t.Parallel()

		store, memory := newAgentEnrollmentStore(t)
		ctx := context.Background()
		cert := newTestCert(t, "tenant-agent-extra", nil)
		enrolled, err := store.EnrollTenantAgentIdentity(ctx, "automatethethings", cert)
		if err != nil {
			t.Fatalf("enroll: %v", err)
		}
		enrolled.Roles = append(enrolled.Roles, "role-admin-automatethethings")
		if err := memory.UpdateUser(ctx, enrolled); err != nil {
			t.Fatalf("add role: %v", err)
		}
		err = store.RevokeTenantAgentIdentity(ctx, "automatethethings", "tenant-agent-extra")
		var notTA *iam.NotATenantAgentIdentityError
		if !errors.As(err, &notTA) {
			t.Fatalf("error = %v, want *NotATenantAgentIdentityError", err)
		}
		if _, err := memory.GetUserByUsername(ctx, "automatethethings", "tenant-agent-extra"); err != nil {
			t.Fatalf("the record was removed: %v", err)
		}
	})
}

func TestEnrollTenantAgentIdentity_Concurrent(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "tenant-agent-race", nil)

	const enrollments = 8
	type result struct {
		id  uint64
		err error
	}
	results := make(chan result, enrollments)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < enrollments; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			user, err := store.EnrollTenantAgentIdentity(ctx, "tenant-race", cert)
			if err != nil {
				results <- result{err: err}
				return
			}
			results <- result{id: user.ID}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	ids := map[uint64]bool{}
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent enrollment: %v", r.err)
		}
		ids[r.id] = true
	}
	if len(ids) != 1 {
		t.Fatalf("concurrent enrollments returned %d user IDs, want 1", len(ids))
	}
	users, err := memory.ListUsers(ctx, "tenant-race", iam.ListOptions{Limit: enrollments + 1})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("enrolled %d principals, want 1", len(users))
	}
}
