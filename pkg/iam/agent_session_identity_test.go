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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// go-quicbaas#901 — a spawned agent session's certificate must carry authority,
// and must stop carrying it the moment the session closes.
// =============================================================================

// TestEnrollAgentSessionIdentity_ConfersTheFloorOnTheCertPrincipal is the
// regression guard. go-qrdb authenticates a request carrying a client
// certificate AS THE CERTIFICATE'S SUBJECT and resolves that principal's roles
// from the user record its Subject names. Nothing enrolled the per-session
// certificate, so every spawned agent authenticated and held no roles, and the
// default-deny MCP surface refused it everything.
func TestEnrollAgentSessionIdentity_ConfersTheFloorOnTheCertPrincipal(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	cert := newTestCert(t, "agent-ses-7f3a91", nil)

	principal, err := store.EnrollAgentSessionIdentity(context.Background(), "automatethethings", cert)
	if err != nil {
		t.Fatalf("EnrollAgentSessionIdentity() error = %v", err)
	}

	// The username MUST be the certificate CN verbatim: that is the subject
	// go-qrdb's mTLS authenticator resolves the user record by.
	if principal.Username != "agent-ses-7f3a91" {
		t.Fatalf("username = %q, want the certificate CN", principal.Username)
	}
	if principal.TenantID != "automatethethings" {
		t.Fatalf("tenant = %q, want automatethethings", principal.TenantID)
	}
	if !principal.CanAuthenticate() {
		t.Fatalf("an enrolled session must be able to authenticate, lifecycle = %q", principal.Lifecycle)
	}
	if len(principal.AuthMethods) != 1 || principal.AuthMethods[0] != iam.AuthMethodMTLS {
		t.Fatalf("auth methods = %v, want exactly [%s]", principal.AuthMethods, iam.AuthMethodMTLS)
	}
	if principal.Email != "" {
		t.Fatalf("email = %q, want none: a session holds no mailbox", principal.Email)
	}

	// Bound by the stack's one canonical mTLS identifier.
	if len(principal.CertBindings) != 1 {
		t.Fatalf("cert bindings = %d, want exactly 1", len(principal.CertBindings))
	}
	if !boundToCert(principal, cert) {
		t.Fatal("the enrolled record must be bound to the certificate it was enrolled from")
	}
	if principal.CertBindings[0].Kind != qrdbsdk.CertBindingSPKIThumbprint {
		t.Fatalf("binding kind = %v, want the canonical SPKI kind", principal.CertBindings[0].Kind)
	}
	if !principal.CertBindings[0].NotAfter.Equal(cert.NotAfter) {
		t.Errorf("binding expiry = %v, want the certificate's NotAfter %v",
			principal.CertBindings[0].NotAfter, cert.NotAfter)
	}

	// Exactly the session role, carrying exactly the floor.
	roleID := iam.AgentSessionRoleID("automatethethings")
	if len(principal.Roles) != 1 || principal.Roles[0] != roleID {
		t.Fatalf("roles = %v, want exactly [%s]", principal.Roles, roleID)
	}
	role, err := memory.GetRole(context.Background(), "automatethethings", roleID)
	if err != nil {
		t.Fatalf("GetRole() error = %v", err)
	}
	if role.Name != iam.AgentSessionRoleName {
		t.Errorf("role name = %q, want %q", role.Name, iam.AgentSessionRoleName)
	}
	assertSamePermissions(t, role.Permissions, iam.AgentSessionGrant())
}

// TestEnrollAgentSessionIdentity_RejectsBadInput covers the refusals. Every
// identifying value of the principal is derived FROM the certificate, so a
// missing or unusable one cannot be papered over — and a refused enrollment must
// leave no record behind, or the spawn would be told it failed while a live
// principal existed.
func TestEnrollAgentSessionIdentity_RejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tenantID string
		// commonName is "" to mean "pass a nil certificate".
		commonName string
	}{
		{name: "nil certificate", tenantID: "automatethethings", commonName: ""},
		{name: "empty tenant", tenantID: "", commonName: "agent-ses-1"},
		{name: "certificate with no common name", tenantID: "automatethethings", commonName: " "},
		{name: "common name too short", tenantID: "automatethethings", commonName: "ab"},
		{name: "common name with a path separator", tenantID: "automatethethings", commonName: "agent/ses-1"},
		{name: "common name with a colon", tenantID: "automatethethings", commonName: "agent:ses-1"},
		{name: "common name over 64 characters", tenantID: "automatethethings", commonName: "agent-" + strings.Repeat("x", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, memory := newAgentEnrollmentStore(t)
			var cert = newTestCert(t, strings.TrimSpace(tt.commonName), nil)
			if tt.commonName == "" {
				cert = nil
			}

			if _, err := store.EnrollAgentSessionIdentity(context.Background(), tt.tenantID, cert); err == nil {
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

// TestEnrollAgentSessionIdentity_ConvergesOnRepeatedEnrollment covers a spawn
// whose identifier repeats (a retried spawn, a resumed session): the record
// converges rather than duplicating, and the grant is rewritten to this build's
// floor rather than accumulating.
func TestEnrollAgentSessionIdentity_ConvergesOnRepeatedEnrollment(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "agent-ses-again", nil)

	first, err := store.EnrollAgentSessionIdentity(ctx, "acme", cert)
	if err != nil {
		t.Fatalf("first enrollment: %v", err)
	}

	// An operator (or an older build) widens the role behind the enrollment's
	// back. The next enrollment must take it back to the floor.
	widened := append(iam.AgentSessionGrant(), "secrets:read")
	if _, err := store.UpdateRole(ctx, "acme", iam.AgentSessionRoleID("acme"), iam.UpdateRoleOptions{
		Permissions: &widened,
	}); err != nil {
		t.Fatalf("widen role: %v", err)
	}

	second, err := store.EnrollAgentSessionIdentity(ctx, "acme", cert)
	if err != nil {
		t.Fatalf("second enrollment: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("repeated enrollment minted a second record: %s then %s", first.ID, second.ID)
	}

	users, err := memory.ListUsers(ctx, "acme", iam.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("tenant holds %d session principals, want exactly 1", len(users))
	}

	role, err := memory.GetRole(ctx, "acme", iam.AgentSessionRoleID("acme"))
	if err != nil {
		t.Fatalf("GetRole() error = %v", err)
	}
	assertSamePermissions(t, role.Permissions, iam.AgentSessionGrant())
}

// TestEnrollAgentSessionIdentity_ConcurrentSpawnsShareOneRole covers many
// sessions starting at once in one tenant. They all find the tenant's session
// role absent and race to create it; the loser is told it exists, which is the
// role it wanted, so every session must end up holding it.
func TestEnrollAgentSessionIdentity_ConcurrentSpawnsShareOneRole(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()

	const spawns = 12
	errs := make(chan error, spawns)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < spawns; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			cert := newTestCert(t, "agent-ses-"+string(rune('a'+i)), nil)
			_, err := store.EnrollAgentSessionIdentity(ctx, "tenant-race", cert)
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent enrollment: %v", err)
		}
	}

	users, err := memory.ListUsers(ctx, "tenant-race", iam.ListOptions{Limit: spawns + 1})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != spawns {
		t.Fatalf("enrolled %d principals, want %d", len(users), spawns)
	}
	roleID := iam.AgentSessionRoleID("tenant-race")
	for _, u := range users {
		if len(u.Roles) != 1 || u.Roles[0] != roleID {
			t.Fatalf("%s roles = %v, want exactly [%s]", u.Username, u.Roles, roleID)
		}
	}
	roles, err := memory.ListRoles(ctx, "tenant-race", iam.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	sessionRoles := 0
	for _, role := range roles {
		if role.Name == iam.AgentSessionRoleName {
			sessionRoles++
			assertSamePermissions(t, role.Permissions, iam.AgentSessionGrant())
		}
	}
	if sessionRoles != 1 {
		t.Fatalf("tenant holds %d session roles, want exactly 1", sessionRoles)
	}
}

// TestEnrollAgentSessionIdentity_IsIsolatedPerTenant covers two tenants spawning
// a session with the SAME identifier. Each gets its own principal under its own
// tenant-scoped role; neither can reach the other.
func TestEnrollAgentSessionIdentity_IsIsolatedPerTenant(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "agent-ses-collide", nil)

	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if _, err := store.EnrollAgentSessionIdentity(ctx, tenant, cert); err != nil {
			t.Fatalf("enroll into %s: %v", tenant, err)
		}
	}
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		principal, err := memory.GetUserByUsername(ctx, tenant, "agent-ses-collide")
		if err != nil {
			t.Fatalf("%s principal missing: %v", tenant, err)
		}
		wantRole := iam.AgentSessionRoleID(tenant)
		if len(principal.Roles) != 1 || principal.Roles[0] != wantRole {
			t.Fatalf("%s roles = %v, want exactly [%s]", tenant, principal.Roles, wantRole)
		}
	}
	if iam.AgentSessionRoleID("tenant-a") == iam.AgentSessionRoleID("tenant-b") {
		t.Fatal("two tenants share a session role ID")
	}
}

// TestEnrollAgentSessionIdentity_WillNotResurrectAWithdrawnIdentity proves an
// administrative disable survives a repeated spawn under the same identifier. A
// spawn is authenticated by nothing the operator sees, so an enrollment that
// re-activated a disabled record would let an agent undo its own revocation.
func TestEnrollAgentSessionIdentity_WillNotResurrectAWithdrawnIdentity(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "agent-ses-withdrawn", nil)

	enrolled, err := store.EnrollAgentSessionIdentity(ctx, "automatethethings", cert)
	if err != nil {
		t.Fatalf("first enrollment: %v", err)
	}
	enrolled.Lifecycle = iam.LifecycleDisabled
	if err := memory.UpdateUser(ctx, enrolled); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if _, err := store.EnrollAgentSessionIdentity(ctx, "automatethethings", cert); err == nil {
		t.Fatal("a repeated spawn re-enabled an identity an administrator disabled")
	}
	after, err := memory.GetUserByUsername(ctx, "automatethethings", "agent-ses-withdrawn")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if after.CanAuthenticate() {
		t.Fatalf("the disabled identity is authenticating again, lifecycle = %q", after.Lifecycle)
	}
}

// =============================================================================
// The tenant's agent role: one definition, two enrollments, every store answer
// =============================================================================

// scriptedRoleStorage answers the three role verbs an agent enrollment drives
// from a script, delegating the rest to the real in-memory store. It exists
// because ensureAgentRole is now the ONE place both the daemon and the session
// enrollment converge a role, so every answer a degraded store can give has to
// be a decided outcome rather than an untested branch.
type scriptedRoleStorage struct {
	iam.RoleStorage

	getErr    error
	createErr error
	updateErr error
}

func (s *scriptedRoleStorage) GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.RoleStorage.GetRole(ctx, tenantID, roleID)
}

func (s *scriptedRoleStorage) CreateRole(ctx context.Context, role *iam.Role) error {
	if s.createErr != nil {
		return s.createErr
	}
	return s.RoleStorage.CreateRole(ctx, role)
}

func (s *scriptedRoleStorage) UpdateRole(ctx context.Context, role *iam.Role) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	return s.RoleStorage.UpdateRole(ctx, role)
}

// newScriptedRoleStore builds an identity store whose role storage is scripted.
func newScriptedRoleStore(t *testing.T, script *scriptedRoleStorage) *iam.IdentityStore {
	t.Helper()
	memory := iam.NewMemoryStorage()
	script.RoleStorage = memory
	return iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memory,
		RoleStorage: script,
	})
}

// TestEnrollAgentSessionIdentity_PropagatesARoleStoreFailure covers the store
// answers that are neither "here is the role" nor "there is no such role". The
// enrollment must fail rather than continue: a principal created without its role
// is authenticated and refused everything, which is the defect this exists to fix
// rather than a state worth writing.
func TestEnrollAgentSessionIdentity_PropagatesARoleStoreFailure(t *testing.T) {
	t.Parallel()

	t.Run("the role lookup fails", func(t *testing.T) {
		t.Parallel()

		down := errors.New("raft not ready")
		store := newScriptedRoleStore(t, &scriptedRoleStorage{getErr: down})
		_, err := store.EnrollAgentSessionIdentity(context.Background(), "acme",
			newTestCert(t, "agent-ses-1", nil))
		if err == nil {
			t.Fatal("an enrollment whose role cannot be resolved must fail")
		}
	})

	t.Run("the role create fails", func(t *testing.T) {
		t.Parallel()

		down := errors.New("store unavailable")
		store := newScriptedRoleStore(t, &scriptedRoleStorage{createErr: down})
		_, err := store.EnrollAgentSessionIdentity(context.Background(), "acme",
			newTestCert(t, "agent-ses-1", nil))
		if err == nil {
			t.Fatal("an enrollment whose role cannot be created must fail")
		}
	})

	t.Run("the role rewrite fails", func(t *testing.T) {
		t.Parallel()

		// Seed the role with a grant this build does not name, so the
		// enrollment takes the REWRITE path rather than the create path.
		memory := iam.NewMemoryStorage()
		script := &scriptedRoleStorage{RoleStorage: memory}
		store := iam.NewIdentityStore(iam.IdentityStoreOptions{
			UserStorage: memory,
			RoleStorage: script,
		})
		if _, err := store.CreateRole(context.Background(), "acme", iam.CreateRoleOptions{
			ID:          iam.AgentSessionRoleID("acme"),
			Name:        iam.AgentSessionRoleName,
			Permissions: []string{"kv:get"},
		}); err != nil {
			t.Fatalf("seed role: %v", err)
		}
		script.updateErr = errors.New("store unavailable")

		if _, err := store.EnrollAgentSessionIdentity(context.Background(), "acme",
			newTestCert(t, "agent-ses-1", nil)); err == nil {
			t.Fatal("an enrollment whose role cannot be rewritten to the current grant must fail")
		}
	})
}

// TestEnrollAgentSessionIdentity_TreatsALostRoleRaceAsSuccess covers the loser of
// a create race. Two sessions starting at once both find the tenant's role absent
// and both try to create it; the loser is told it already exists. That IS the role
// this enrollment wanted, so the conflict is the success it was racing for — and
// failing there would make a concurrent spawn refuse for no reason a caller could
// act on.
func TestEnrollAgentSessionIdentity_TreatsALostRoleRaceAsSuccess(t *testing.T) {
	t.Parallel()

	memory := iam.NewMemoryStorage()
	script := &scriptedRoleStorage{RoleStorage: memory}
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memory,
		RoleStorage: script,
	})

	// The role the winner committed, holding exactly this build's grant, so the
	// enrollment reaches CreateRole and is refused with a conflict.
	if _, err := store.CreateRole(context.Background(), "acme", iam.CreateRoleOptions{
		ID:          iam.AgentSessionRoleID("acme"),
		Name:        iam.AgentSessionRoleName,
		Permissions: iam.AgentSessionGrant(),
	}); err != nil {
		t.Fatalf("seed the winner's role: %v", err)
	}
	// The store answers "no such role", so the enrollment reaches CreateRole and
	// is refused with the conflict the winner already committed.
	script.getErr = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "GetRole",
		Err:  errors.New("role not found"),
	}

	principal, err := store.EnrollAgentSessionIdentity(context.Background(), "acme",
		newTestCert(t, "agent-ses-loser", nil))
	if err != nil {
		t.Fatalf("the loser of a role race must converge, got %v", err)
	}
	roleID := iam.AgentSessionRoleID("acme")
	if len(principal.Roles) != 1 || principal.Roles[0] != roleID {
		t.Fatalf("roles = %v, want exactly [%s]", principal.Roles, roleID)
	}
}

// =============================================================================
// Revocation
// =============================================================================

// TestRevokeAgentSessionIdentity_RemovesThePrincipal proves a closed session's
// certificate stops resolving to any authority: the record go-qrdb resolves roles
// from is gone, so the principal is unenriched and default-deny refuses it.
func TestRevokeAgentSessionIdentity_RemovesThePrincipal(t *testing.T) {
	t.Parallel()

	store, memory := newAgentEnrollmentStore(t)
	ctx := context.Background()
	cert := newTestCert(t, "agent-ses-close", nil)

	if _, err := store.EnrollAgentSessionIdentity(ctx, "automatethethings", cert); err != nil {
		t.Fatalf("EnrollAgentSessionIdentity() error = %v", err)
	}
	if err := store.RevokeAgentSessionIdentity(ctx, "automatethethings", "agent-ses-close"); err != nil {
		t.Fatalf("RevokeAgentSessionIdentity() error = %v", err)
	}

	if _, err := memory.GetUserByUsername(ctx, "automatethethings", "agent-ses-close"); err == nil {
		t.Fatal("the session principal survived revocation")
	}

	// The tenant's role stays: it is the grant definition shared by every
	// session in the tenant, and churning it per close would rewrite live
	// sessions' authority.
	if _, err := memory.GetRole(ctx, "automatethethings", iam.AgentSessionRoleID("automatethethings")); err != nil {
		t.Fatalf("the tenant's session role must survive one session's close: %v", err)
	}
}

// TestRevokeAgentSessionIdentity_IsIdempotent covers a cleanup that runs twice
// and a cleanup that runs for a session whose enrollment never landed. Both are
// the state the caller asked for, so both succeed — a Close must not fail because
// it already happened.
func TestRevokeAgentSessionIdentity_IsIdempotent(t *testing.T) {
	t.Parallel()

	store, _ := newAgentEnrollmentStore(t)
	ctx := context.Background()

	t.Run("never enrolled", func(t *testing.T) {
		if err := store.RevokeAgentSessionIdentity(ctx, "automatethethings", "agent-ses-ghost"); err != nil {
			t.Fatalf("revoking an absent principal must succeed, got %v", err)
		}
	})

	t.Run("twice", func(t *testing.T) {
		cert := newTestCert(t, "agent-ses-twice", nil)
		if _, err := store.EnrollAgentSessionIdentity(ctx, "automatethethings", cert); err != nil {
			t.Fatalf("EnrollAgentSessionIdentity() error = %v", err)
		}
		for i := 0; i < 2; i++ {
			if err := store.RevokeAgentSessionIdentity(ctx, "automatethethings", "agent-ses-twice"); err != nil {
				t.Fatalf("revocation %d: %v", i+1, err)
			}
		}
	})
}

// TestRevokeAgentSessionIdentity_RefusesWhatIsNotASessionIdentity is the guard
// that makes the username safe to take from a certificate CommonName. A
// CommonName is not a capability: without this, a session whose CN collided with
// a human account or a fleet node daemon would DELETE that account on close.
func TestRevokeAgentSessionIdentity_RefusesWhatIsNotASessionIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a fleet node daemon", func(t *testing.T) {
		t.Parallel()

		store, memory := newAgentEnrollmentStore(t)
		cert := newTestCert(t, "agent-agent-04", nil)
		if _, err := store.EnrollFleetNodeCertIdentity(ctx, "automatethethings", iam.FleetNodeCertIdentityOptions{
			Certificate: cert,
			Permissions: iam.FleetRuntimeGrant(),
		}); err != nil {
			t.Fatalf("EnrollFleetNodeCertIdentity() error = %v", err)
		}

		err := store.RevokeAgentSessionIdentity(ctx, "automatethethings", "agent-agent-04")
		if err == nil {
			t.Fatal("a session close deleted the fleet node daemon's identity")
		}
		var notSession *iam.NotAnAgentSessionIdentityError
		if !errors.As(err, &notSession) {
			t.Fatalf("error type = %T (%v), want *iam.NotAnAgentSessionIdentityError", err, err)
		}
		if notSession.Username != "agent-agent-04" {
			t.Errorf("reported username = %q, want agent-agent-04", notSession.Username)
		}
		if _, err := memory.GetUserByUsername(ctx, "automatethethings", "agent-agent-04"); err != nil {
			t.Fatalf("the daemon identity was deleted anyway: %v", err)
		}
	})

	t.Run("a session that also holds another role", func(t *testing.T) {
		t.Parallel()

		store, memory := newAgentEnrollmentStore(t)
		cert := newTestCert(t, "agent-ses-elevated", nil)
		enrolled, err := store.EnrollAgentSessionIdentity(ctx, "automatethethings", cert)
		if err != nil {
			t.Fatalf("EnrollAgentSessionIdentity() error = %v", err)
		}
		enrolled.Roles = append(enrolled.Roles, "role-operator-automatethethings")
		if err := memory.UpdateUser(ctx, enrolled); err != nil {
			t.Fatalf("UpdateUser() error = %v", err)
		}

		if err := store.RevokeAgentSessionIdentity(ctx, "automatethethings", "agent-ses-elevated"); err == nil {
			t.Fatal("a session close deleted a record holding a role it did not grant")
		}
		if _, err := memory.GetUserByUsername(ctx, "automatethethings", "agent-ses-elevated"); err != nil {
			t.Fatalf("the record was deleted anyway: %v", err)
		}
	})

	t.Run("a record not restricted to mtls", func(t *testing.T) {
		t.Parallel()

		store, memory := newAgentEnrollmentStore(t)
		cert := newTestCert(t, "agent-ses-human", nil)
		enrolled, err := store.EnrollAgentSessionIdentity(ctx, "automatethethings", cert)
		if err != nil {
			t.Fatalf("EnrollAgentSessionIdentity() error = %v", err)
		}
		enrolled.AuthMethods = []string{"webauthn"}
		if err := memory.UpdateUser(ctx, enrolled); err != nil {
			t.Fatalf("UpdateUser() error = %v", err)
		}

		if err := store.RevokeAgentSessionIdentity(ctx, "automatethethings", "agent-ses-human"); err == nil {
			t.Fatal("a session close deleted a record that can authenticate by another method")
		}
		if _, err := memory.GetUserByUsername(ctx, "automatethethings", "agent-ses-human"); err != nil {
			t.Fatalf("the record was deleted anyway: %v", err)
		}
	})
}

// TestRevokeAgentSessionIdentity_RejectsBadInput covers the validation. A
// revocation that accepted a blank or malformed subject would either address
// nothing or address something by a name the enrollment could never have
// written.
func TestRevokeAgentSessionIdentity_RejectsBadInput(t *testing.T) {
	t.Parallel()

	store, _ := newAgentEnrollmentStore(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		tenantID string
		username string
	}{
		{name: "empty tenant", tenantID: "", username: "agent-ses-1"},
		{name: "empty username", tenantID: "automatethethings", username: ""},
		{name: "blank username", tenantID: "automatethethings", username: "   "},
		{name: "username too short", tenantID: "automatethethings", username: "ab"},
		{name: "username with a path separator", tenantID: "automatethethings", username: "agent/ses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.RevokeAgentSessionIdentity(ctx, tt.tenantID, tt.username); err == nil {
				t.Fatal("expected the revocation to be refused")
			}
		})
	}
}

// =============================================================================
// Revocation against a store that will not answer
// =============================================================================

// lookupScriptedUserStorage answers the username lookup a revocation starts with
// from a script, delegating every other verb to the real in-memory store. It
// covers the two answers a working store never gives and a degraded one does: a
// transient failure, and "no error, no record".
type lookupScriptedUserStorage struct {
	iam.UserStorage
	err        error
	nilNoError bool
}

func (s *lookupScriptedUserStorage) GetUserByUsername(_ context.Context, _, _ string) (*iam.User, error) {
	if s.nilNoError {
		return nil, nil
	}
	return nil, s.err
}

// deleteScriptedUserStorage fails only the deletion, leaving every other verb
// working — the shape that distinguishes "this store cannot delete" from "this
// store is broken".
type deleteScriptedUserStorage struct {
	iam.UserStorage
	err error
}

func (s *deleteScriptedUserStorage) DeleteUser(_ context.Context, _, _ string) error {
	return s.err
}

// newScriptedRevocationStore builds an identity store whose user storage is the
// supplied wrapper around a fresh in-memory store.
func newScriptedRevocationStore(t *testing.T, wrap func(iam.UserStorage) iam.UserStorage) (*iam.IdentityStore, *iam.MemoryStorage) {
	t.Helper()
	memory := iam.NewMemoryStorage()
	return iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: wrap(memory),
		RoleStorage: memory,
	}), memory
}

// TestRevokeAgentSessionIdentity_PropagatesAStoreFailure covers a store that
// cannot answer whether the principal exists. It must NOT be read as "already
// gone": reporting success there would tell the caller a live principal had been
// revoked, and the caller logs the failure precisely so an operator learns a
// session's identity outlived it.
func TestRevokeAgentSessionIdentity_PropagatesAStoreFailure(t *testing.T) {
	t.Parallel()

	t.Run("the lookup fails", func(t *testing.T) {
		t.Parallel()

		down := errors.New("raft not ready")
		store, _ := newScriptedRevocationStore(t, func(inner iam.UserStorage) iam.UserStorage {
			return &lookupScriptedUserStorage{UserStorage: inner, err: down}
		})
		err := store.RevokeAgentSessionIdentity(context.Background(), "automatethethings", "agent-ses-1")
		if !errors.Is(err, down) {
			t.Fatalf("error = %v, want it to unwrap to the store failure", err)
		}
	})

	t.Run("the delete fails", func(t *testing.T) {
		t.Parallel()

		down := errors.New("store unavailable")
		store, memory := newScriptedRevocationStore(t, func(inner iam.UserStorage) iam.UserStorage {
			return &deleteScriptedUserStorage{UserStorage: inner, err: down}
		})
		cert := newTestCert(t, "agent-ses-stuck", nil)
		if _, err := store.EnrollAgentSessionIdentity(context.Background(), "automatethethings", cert); err != nil {
			t.Fatalf("EnrollAgentSessionIdentity() error = %v", err)
		}

		if err := store.RevokeAgentSessionIdentity(context.Background(), "automatethethings", "agent-ses-stuck"); err == nil {
			t.Fatal("a revocation whose delete failed must report it, not claim success")
		}
		// The record is left CLOSED even though the delete failed: DeleteUser
		// marks the account terminal before sweeping anything, so a failed
		// revocation still stops the certificate authenticating.
		after, err := memory.GetUserByUsername(context.Background(), "automatethethings", "agent-ses-stuck")
		if err != nil {
			t.Fatalf("GetUserByUsername() error = %v", err)
		}
		if after.CanAuthenticate() {
			t.Fatalf("the session can still authenticate after a failed revocation, lifecycle = %q", after.Lifecycle)
		}
	})
}

// TestRevokeAgentSessionIdentity_TreatsAnAbsentRecordAsRevoked covers a store
// that reports neither a record nor an error — the answer a close gets when
// another close already won the race. That is the state this asked for, so it
// succeeds.
func TestRevokeAgentSessionIdentity_TreatsAnAbsentRecordAsRevoked(t *testing.T) {
	t.Parallel()

	store, _ := newScriptedRevocationStore(t, func(inner iam.UserStorage) iam.UserStorage {
		return &lookupScriptedUserStorage{UserStorage: inner, nilNoError: true}
	})
	if err := store.RevokeAgentSessionIdentity(context.Background(), "automatethethings", "agent-ses-1"); err != nil {
		t.Fatalf("an absent principal is the state the caller asked for, got %v", err)
	}
}

// =============================================================================
// The grant and the role identity
// =============================================================================

// TestAgentSessionGrant_IsTheLeastPrivilegeFloor pins the set itself. Widening it
// widens every spawned agent in every tenant at once, so the absences are
// asserted, not assumed.
func TestAgentSessionGrant_IsTheLeastPrivilegeFloor(t *testing.T) {
	t.Parallel()

	grant := iam.AgentSessionGrant()
	want := []string{
		iam.AgentSessionScopeInference,
		iam.AgentSessionScopeKVGet,
		iam.AgentSessionScopeKVPut,
		iam.AgentSessionScopeKVList,
	}
	assertSamePermissions(t, grant, want)

	held := make(map[string]bool, len(grant))
	for _, permission := range grant {
		if strings.Contains(permission, "*") {
			t.Fatalf("the session grant must never contain a wildcard, got %q", permission)
		}
		if strings.TrimSpace(permission) != permission || permission == "" {
			t.Fatalf("permission %q is not a clean resource:action literal", permission)
		}
		held[permission] = true
	}
	for _, forbidden := range []string{
		"kv:delete", "kv:scan", "kv:txn",
		"secrets:read", "kms:sign", "iam:users:create",
		"barrier:unseal", "tenant:create", "cluster:admin",
		"orchestrator:agent_heartbeat",
	} {
		if held[forbidden] {
			t.Errorf("a spawned agent session must not hold %q", forbidden)
		}
	}

	// The returned slice is the caller's: mutating it must not reach the next
	// caller, or one enrollment could widen the next.
	grant[0] = "secrets:read"
	if iam.AgentSessionGrant()[0] == "secrets:read" {
		t.Fatal("AgentSessionGrant returns shared state a caller can mutate")
	}
}

// TestAgentRoleID_IsDeterministicAndTenantScoped pins the identifier scheme a
// repeated enrollment converges on. Role IDs are globally unique while a role's
// NAME is unique only within its tenant, so the tenant must be part of the ID.
func TestAgentRoleID_IsDeterministicAndTenantScoped(t *testing.T) {
	t.Parallel()

	if got := iam.AgentRoleID("agent-session", "acme"); got != iam.AgentRoleID("agent-session", "acme") {
		t.Fatal("AgentRoleID is not deterministic")
	}
	if iam.AgentRoleID("agent-session", "acme") == iam.AgentRoleID("agent-session", "other") {
		t.Fatal("two tenants share a role ID")
	}
	if iam.AgentRoleID("agent-session", "acme") == iam.AgentRoleID("agent-runtime", "acme") {
		t.Fatal("two roles in one tenant share a role ID")
	}
	if got, want := iam.AgentSessionRoleID("acme"), iam.AgentRoleID(iam.AgentSessionRoleName, "acme"); got != want {
		t.Fatalf("AgentSessionRoleID = %q, want %q", got, want)
	}
	if got, want := iam.FleetRuntimeRoleID("acme"), iam.AgentRoleID(iam.FleetRuntimeRoleName, "acme"); got != want {
		t.Fatalf("FleetRuntimeRoleID = %q, want %q", got, want)
	}
}

// TestNotAnAgentSessionIdentityError_Message pins the diagnostic an operator
// reads when a close is refused: it must name the subject, the tenant and the
// roles the record actually held, because that is what identifies the collision.
func TestNotAnAgentSessionIdentityError_Message(t *testing.T) {
	t.Parallel()

	err := &iam.NotAnAgentSessionIdentityError{
		Username: "agent-agent-04",
		TenantID: "automatethethings",
		Roles:    []string{"role-agent-runtime-automatethethings"},
	}
	message := err.Error()
	for _, want := range []string{"agent-agent-04", "automatethethings", "role-agent-runtime-automatethethings"} {
		if !strings.Contains(message, want) {
			t.Errorf("error message %q does not name %q", message, want)
		}
	}
}
