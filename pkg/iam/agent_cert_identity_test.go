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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// newAgentEnrollmentStore returns an identity store over the in-memory
// storage, plus that storage so a test can inspect what was written.
func newAgentEnrollmentStore(t *testing.T) (*iam.IdentityStore, *iam.MemoryStorage) {
	t.Helper()
	memory := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memory,
		RoleStorage: memory,
	})
	return store, memory
}

// TestEnrollAgentCertIdentity_ConfersTheGrantOnTheCertPrincipal is the
// regression guard for go-quicbaas#663. go-qrdb authenticates a request that
// carries a client certificate AS THE CERTIFICATE'S SUBJECT and resolves that
// principal's roles from the user record its Subject names. Registration
// created no such record, so the daemon's every control-plane call was refused
// 403. After enrollment the principal exists, is active, is restricted to
// mTLS, and holds exactly the agent runtime grant.
func TestEnrollAgentCertIdentity_ConfersTheGrantOnTheCertPrincipal(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	cert := newTestCert(t, "agent-agent-04", nil)

	user, err := store.EnrollFleetNodeCertIdentity(context.Background(), "automatethethings",
		iam.FleetNodeCertIdentityOptions{
			Certificate: cert,
			Permissions: iam.FleetRuntimeGrant(),
		})
	if err != nil {
		t.Fatalf("EnrollFleetNodeCertIdentity() error = %v", err)
	}

	// The username MUST be the certificate CN verbatim: that is the subject
	// go-qrdb's mTLS authenticator resolves the user record by.
	if user.Username != "agent-agent-04" {
		t.Fatalf("username = %q, want the certificate CN %q", user.Username, "agent-agent-04")
	}
	if user.TenantID != "automatethethings" {
		t.Fatalf("tenant = %q, want automatethethings", user.TenantID)
	}
	if !user.CanAuthenticate() {
		t.Fatalf("an enrolled agent must be able to authenticate, lifecycle = %q", user.Lifecycle)
	}
	if len(user.AuthMethods) != 1 || user.AuthMethods[0] != iam.AuthMethodMTLS {
		t.Fatalf("auth methods = %v, want exactly [%s]", user.AuthMethods, iam.AuthMethodMTLS)
	}
	if user.Email != "" {
		t.Fatalf("email = %q, want none: a daemon holds no mailbox and must not occupy the login-key index", user.Email)
	}

	// The certificate is bound by its canonical SPKI thumbprint.
	want := qrdbsdk.ComputeSPKIThumbprint(cert)
	if len(user.CertBindings) != 1 {
		t.Fatalf("cert bindings = %d, want exactly 1", len(user.CertBindings))
	}
	if user.CertBindings[0].Thumbprint != want {
		t.Fatalf("bound thumbprint = %q, want %q", user.CertBindings[0].Thumbprint, want)
	}
	if user.CertBindings[0].Kind != qrdbsdk.CertBindingSPKIThumbprint {
		t.Fatalf("binding kind = %v, want the canonical SPKI kind", user.CertBindings[0].Kind)
	}
	if !boundToCert(user, cert) {
		t.Fatal("the enrolled record must be bound to the certificate it was enrolled from")
	}

	// The role the record holds carries the grant and nothing else.
	roleID := iam.FleetRuntimeRoleID("automatethethings")
	if len(user.Roles) != 1 || user.Roles[0] != roleID {
		t.Fatalf("roles = %v, want exactly [%s]", user.Roles, roleID)
	}
	role, err := memory.GetRole(context.Background(), "automatethethings", roleID)
	if err != nil {
		t.Fatalf("GetRole() error = %v", err)
	}
	assertSamePermissions(t, role.Permissions, iam.FleetRuntimeGrant())
}

// TestEnrollAgentCertIdentity_GrantsNothingBeyondTheAgentRuntimeGrant is the
// negative half: the enrolled principal must not be able to administer the
// platform that runs it. The role is a dedicated one — never a built-in — and
// carries no wildcard and no verb outside the pinned grant.
func TestEnrollAgentCertIdentity_GrantsNothingBeyondTheAgentRuntimeGrant(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	if _, err := store.EnrollFleetNodeCertIdentity(context.Background(), "acme",
		iam.FleetNodeCertIdentityOptions{
			Certificate: newTestCert(t, "agent-runner-1", nil),
			Permissions: iam.FleetRuntimeGrant(),
		}); err != nil {
		t.Fatalf("EnrollFleetNodeCertIdentity() error = %v", err)
	}

	role, err := memory.GetRole(context.Background(), "acme", iam.FleetRuntimeRoleID("acme"))
	if err != nil {
		t.Fatalf("GetRole() error = %v", err)
	}
	if role.Name != iam.FleetRuntimeRoleName {
		t.Fatalf("role name = %q, want %q", role.Name, iam.FleetRuntimeRoleName)
	}

	granted := make(map[string]bool, len(role.Permissions))
	for _, permission := range role.Permissions {
		granted[permission] = true
	}
	forbidden := []string{
		"*:*",
		"orchestrator:*",
		"users:create",
		"roles:create",
		"tenants:create",
		"secrets:read",
		"kms:sign",
		"barrier:unseal",
		"orchestrator:task_create",
		"orchestrator:job_create",
		"orchestrator:agent_register",
	}
	for _, permission := range forbidden {
		if granted[permission] {
			t.Fatalf("an agent runtime must not hold %q", permission)
		}
	}
}

// TestEnrollAgentCertIdentity_Idempotent proves a daemon that re-registers on
// every restart converges on the record it already has: one user, one role, no
// duplicate identity per registration.
func TestEnrollAgentCertIdentity_Idempotent(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	cert := newTestCert(t, "agent-agent-04", nil)
	opts := iam.FleetNodeCertIdentityOptions{Certificate: cert, Permissions: iam.FleetRuntimeGrant()}

	first, err := store.EnrollFleetNodeCertIdentity(context.Background(), "automatethethings", opts)
	if err != nil {
		t.Fatalf("first enrollment: %v", err)
	}
	second, err := store.EnrollFleetNodeCertIdentity(context.Background(), "automatethethings", opts)
	if err != nil {
		t.Fatalf("second enrollment: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("re-registration minted a second identity: %q then %q", first.ID, second.ID)
	}

	users, err := memory.ListUsers(context.Background(), "automatethethings", iam.ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users = %d, want 1 after two registrations of one daemon", len(users))
	}
}

// TestEnrollAgentCertIdentity_RenewedCertificateRePins proves a rotated
// certificate replaces the binding rather than accumulating one, so a
// superseded credential stops naming the identity — the same re-pin the
// self_signed_tls_client_auth application performs.
func TestEnrollAgentCertIdentity_RenewedCertificateRePins(t *testing.T) {
	t.Parallel()

	store, _ := newAgentEnrollmentStore(t)
	original := newTestCert(t, "agent-agent-04", nil)
	if _, err := store.EnrollFleetNodeCertIdentity(context.Background(), "automatethethings",
		iam.FleetNodeCertIdentityOptions{Certificate: original, Permissions: iam.FleetRuntimeGrant()}); err != nil {
		t.Fatalf("first enrollment: %v", err)
	}

	// A renewal onto a NEW key: a new SPKI, so a new canonical identity.
	renewedKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	renewed := newTestCert(t, "agent-agent-04", renewedKey)

	user, err := store.EnrollFleetNodeCertIdentity(context.Background(), "automatethethings",
		iam.FleetNodeCertIdentityOptions{Certificate: renewed, Permissions: iam.FleetRuntimeGrant()})
	if err != nil {
		t.Fatalf("re-enrollment: %v", err)
	}
	if len(user.CertBindings) != 1 {
		t.Fatalf("cert bindings = %d, want exactly 1 after a renewal", len(user.CertBindings))
	}
	if !boundToCert(user, renewed) {
		t.Fatal("the renewed certificate must resolve the identity")
	}
	if boundToCert(user, original) {
		t.Fatal("the superseded certificate must no longer resolve the identity")
	}
}

// TestEnrollAgentCertIdentity_ConvergesADriftedGrant proves the running
// build's grant is the authority: a role left carrying an older permission set
// is rewritten at the next registration, so a deployed fleet repairs itself
// without operator action.
func TestEnrollAgentCertIdentity_ConvergesADriftedGrant(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()

	// A role from an older build: only the heartbeat half of the grant.
	if err := memory.CreateRole(ctx, &iam.Role{
		ID:          iam.FleetRuntimeRoleID("acme"),
		TenantID:    "acme",
		Name:        iam.FleetRuntimeRoleName,
		Permissions: []string{iam.FleetScopeControlHeartbeat},
	}); err != nil {
		t.Fatalf("seed role: %v", err)
	}

	if _, err := store.EnrollFleetNodeCertIdentity(ctx, "acme", iam.FleetNodeCertIdentityOptions{
		Certificate: newTestCert(t, "agent-runner-1", nil),
		Permissions: iam.FleetRuntimeGrant(),
	}); err != nil {
		t.Fatalf("EnrollFleetNodeCertIdentity() error = %v", err)
	}

	role, err := memory.GetRole(ctx, "acme", iam.FleetRuntimeRoleID("acme"))
	if err != nil {
		t.Fatalf("GetRole() error = %v", err)
	}
	assertSamePermissions(t, role.Permissions, iam.FleetRuntimeGrant())
}

// TestEnrollAgentCertIdentity_TwoDaemonsShareTheTenantRole proves a tenant can
// run more than one agent: each daemon gets its own identity, both hold the
// one tenant-scoped role, and neither is refused for want of an email address
// it does not have.
func TestEnrollAgentCertIdentity_TwoDaemonsShareTheTenantRole(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()

	for _, cn := range []string{"agent-agent-04", "agent-gpu-host"} {
		user, err := store.EnrollFleetNodeCertIdentity(ctx, "automatethethings", iam.FleetNodeCertIdentityOptions{
			Certificate: newTestCert(t, cn, nil),
			Permissions: iam.FleetRuntimeGrant(),
		})
		if err != nil {
			t.Fatalf("enrolling %s: %v", cn, err)
		}
		if user.Roles[0] != iam.FleetRuntimeRoleID("automatethethings") {
			t.Fatalf("%s holds role %v, want the tenant's agent runtime role", cn, user.Roles)
		}
	}

	users, err := memory.ListUsers(ctx, "automatethethings", iam.ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("users = %d, want 2: a tenant may run more than one agent", len(users))
	}
}

// TestEnrollAgentCertIdentity_TenantIsolation proves the identity lands in the
// tenant the certificate names and nowhere else.
func TestEnrollAgentCertIdentity_TenantIsolation(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()

	if _, err := store.EnrollFleetNodeCertIdentity(ctx, "acme", iam.FleetNodeCertIdentityOptions{
		Certificate: newTestCert(t, "agent-runner-1", nil),
		Permissions: iam.FleetRuntimeGrant(),
	}); err != nil {
		t.Fatalf("EnrollFleetNodeCertIdentity() error = %v", err)
	}

	if _, err := memory.GetUserByUsername(ctx, "other", "agent-runner-1"); err == nil {
		t.Fatal("an agent enrolled in one tenant must not resolve in another")
	}
	if _, err := memory.GetRole(ctx, "other", iam.FleetRuntimeRoleID("other")); err == nil {
		t.Fatal("enrolling in one tenant must not create a role in another")
	}
}

// TestEnrollAgentCertIdentity_RejectsBadInput covers the refusal half: every
// input that could produce an identity nobody meant to create is refused
// before anything is written.
func TestEnrollAgentCertIdentity_RejectsBadInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		tenantID string
		opts     iam.FleetNodeCertIdentityOptions
	}{
		{
			name:     "no tenant",
			tenantID: "",
			opts: iam.FleetNodeCertIdentityOptions{
				Certificate: newTestCert(t, "agent-runner-1", nil),
				Permissions: iam.FleetRuntimeGrant(),
			},
		},
		{
			name:     "no certificate",
			tenantID: "acme",
			opts:     iam.FleetNodeCertIdentityOptions{Permissions: iam.FleetRuntimeGrant()},
		},
		{
			name:     "certificate carries no common name",
			tenantID: "acme",
			opts: iam.FleetNodeCertIdentityOptions{
				Certificate: newTestCert(t, "", nil),
				Permissions: iam.FleetRuntimeGrant(),
			},
		},
		{
			name:     "empty grant",
			tenantID: "acme",
			opts: iam.FleetNodeCertIdentityOptions{
				Certificate: newTestCert(t, "agent-runner-1", nil),
			},
		},
		{
			name:     "blank permission",
			tenantID: "acme",
			opts: iam.FleetNodeCertIdentityOptions{
				Certificate: newTestCert(t, "agent-runner-1", nil),
				Permissions: []string{"  "},
			},
		},
		{
			name:     "wildcard grant",
			tenantID: "acme",
			opts: iam.FleetNodeCertIdentityOptions{
				Certificate: newTestCert(t, "agent-runner-1", nil),
				Permissions: []string{"orchestrator:*"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store, memory := newAgentEnrollmentStore(t)
			if _, err := store.EnrollFleetNodeCertIdentity(context.Background(), tc.tenantID, tc.opts); err == nil {
				t.Fatal("expected the enrollment to be refused")
			}

			users, err := memory.ListUsers(context.Background(), "acme", iam.ListOptions{})
			if err != nil {
				t.Fatalf("ListUsers() error = %v", err)
			}
			if len(users) != 0 {
				t.Fatalf("a refused enrollment wrote %d user records", len(users))
			}
		})
	}
}

// boundToCert reports whether the record holds a binding for the
// certificate's canonical SPKI thumbprint.
func boundToCert(user *iam.User, cert *x509.Certificate) bool {
	thumbprint := qrdbsdk.ComputeSPKIThumbprint(cert)
	for _, binding := range user.CertBindings {
		if binding.Thumbprint == thumbprint {
			return true
		}
	}
	return false
}

// assertSamePermissions fails unless got and want hold the same members.
func assertSamePermissions(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("permissions = %v, want %v", got, want)
	}
	held := make(map[string]bool, len(got))
	for _, permission := range got {
		held[permission] = true
	}
	for _, permission := range want {
		if !held[permission] {
			t.Fatalf("permissions = %v, missing %q", got, permission)
		}
	}
}

// TestEnrollAgentCertIdentity_WillNotResurrectAWithdrawnIdentity proves an
// administrative disable survives the daemon's next restart. A registration is
// authenticated by the certificate alone, so an enrollment that re-activated a
// disabled record would let an agent undo its own revocation.
func TestEnrollAgentCertIdentity_WillNotResurrectAWithdrawnIdentity(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "agent-agent-04", nil)
	opts := iam.FleetNodeCertIdentityOptions{Certificate: cert, Permissions: iam.FleetRuntimeGrant()}

	enrolled, err := store.EnrollFleetNodeCertIdentity(ctx, "automatethethings", opts)
	if err != nil {
		t.Fatalf("first enrollment: %v", err)
	}

	// The operator withdraws the agent from service.
	enrolled.Lifecycle = iam.LifecycleDisabled
	if err := memory.UpdateUser(ctx, enrolled); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if _, err := store.EnrollFleetNodeCertIdentity(ctx, "automatethethings", opts); err == nil {
		t.Fatal("re-registration re-enabled an identity an administrator disabled")
	}

	after, err := memory.GetUserByUsername(ctx, "automatethethings", "agent-agent-04")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if after.CanAuthenticate() {
		t.Fatalf("the disabled identity is authenticating again, lifecycle = %q", after.Lifecycle)
	}
}
