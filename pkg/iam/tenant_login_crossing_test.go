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

package iam

import (
	"context"
	"strings"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// =============================================================================
// go-quicbaas #529 — no user in a non-system tenant could log in.
//
// The measured failure: a tenant user's passkey registered, stored and was
// offered at login; the assertion verified; and then POST
// /api/v1/auth/login/finish answered 401 with
//
//	iam.TokenService.resolveEffectivePermissions: internal: failed to resolve
//	effective permissions: role: not found: 5759406e-...
//
// The role EXISTS — in the tenant's own store. go-qrdb resolves the role store
// from the CONTEXT, never from Identity.TenantID, and the unified login
// ceremony issues from the login request's context, which is unauthenticated
// and carries no tenant. So every issuance for a non-system tenant resolved its
// roles in the SYSTEM store and refused.
//
// These tests cross the registration -> login boundary in a NON-system tenant,
// which is the crossing no test made before — the gap that let this and
// go-xkms#350 through. Each is written against a role store that partitions by
// go-qrdb's OWN tenant binding, because that fidelity is the whole point: a
// double that ignores the context passes with or without the fix.
// =============================================================================

const (
	// crossingTenant is deliberately not the system tenant: the system tenant
	// is where every unscoped read lands, so it cannot distinguish a scoped
	// resolve from an unscoped one.
	crossingTenant = "automatethethings"
	crossingUser   = "factory-ui@automatethethings.com"
	// crossingRoleID is a role that exists ONLY in crossingTenant, exactly as
	// a tenant's materialized builtin operator role does.
	crossingRoleID = "5759406e-2f93-8e8c-bf5d-0582ab9d704c"
)

// tenantPartitionedRBAC models go-qrdb's role resolution: the tenant comes from
// the CONTEXT (qrdbsdk.ContextWithTenant), the identity's TenantID field is not
// consulted, and a role absent from the resolved tenant's partition is an
// error — the same "role: not found" that refused issuance in production.
//
// permsByTenantRole is keyed [tenantID][roleID]; the empty tenant key is the
// system partition an unbound context resolves to.
type tenantPartitionedRBAC struct {
	permsByTenantRole map[string]map[string][]string

	// resolvedTenant records the tenant the last ListPermissions resolved
	// through, so a test can assert WHICH store was read rather than only
	// that the call succeeded.
	resolvedTenant string
}

func (r *tenantPartitionedRBAC) Authorize(context.Context, *qrdbtransport.Identity, string, string) error {
	return nil
}

func (r *tenantPartitionedRBAC) ListPermissions(ctx context.Context, identity *qrdbtransport.Identity) ([]qrdbtransport.PermissionInfo, error) {
	tenantID, _ := qrdbsdk.TenantFromContext(ctx)
	r.resolvedTenant = tenantID

	partition := r.permsByTenantRole[tenantID]
	var out []qrdbtransport.PermissionInfo
	for _, roleID := range identity.Roles {
		names, ok := partition[roleID]
		if !ok {
			return nil, &roleNotFoundInTenantError{TenantID: tenantID, RoleID: roleID}
		}
		for _, name := range names {
			out = append(out, qrdbtransport.PermissionInfo{Name: name})
		}
	}
	return out, nil
}

// roleNotFoundInTenantError mirrors go-qrdb's "role: not found: <uuid>", which
// is what the resolver returns for a role that is real but filed under a
// different tenant.
type roleNotFoundInTenantError struct {
	// TenantID is the partition that was searched.
	TenantID string
	// RoleID is the role that was not in it.
	RoleID string
}

func (e *roleNotFoundInTenantError) Error() string {
	return "role: not found: " + e.RoleID + " (searched tenant " + e.TenantID + ")"
}

// newCrossingRBAC returns a resolver that knows crossingRoleID in
// crossingTenant and NOTHING in the system partition.
func newCrossingRBAC() *tenantPartitionedRBAC {
	return &tenantPartitionedRBAC{
		permsByTenantRole: map[string]map[string][]string{
			crossingTenant: {crossingRoleID: {"orchestrator:status", "project:list"}},
			// The system partition is present and EMPTY of this role, which is
			// the production shape: the system tenant has its own operator
			// role under a different id.
			"": {},
		},
	}
}

// registerPasskeyInTenant runs the real registration write-through for a
// tenant's relying party and returns the qrdb login lookup key the enrolled
// factor is addressed by — the value a login proof reduces to.
func registerPasskeyInTenant(t *testing.T, tenantID string, userID uint64, credID []byte) string {
	t.Helper()

	factors := &fakeUserAuthFactorClient{}
	users := xkmswebauthn.NewMemoryUserStore()
	creds := xkmswebauthn.NewMemoryCredentialStore()
	wt, err := newWebAuthnFactorWriteThrough(creds, users, factors, recorderOf(factors), tenantID, permissiveTestGate(), nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	user := xkmswebauthn.NewDefaultUser(testUserHandle(userID), crossingUser, "factory-ui passkey")
	if err := users.Save(context.Background(), user); err != nil {
		t.Fatalf("seed webauthn user: %v", err)
	}

	if err := wt.Save(context.Background(), testCredential(userID, credID)); err != nil {
		t.Fatalf("registration Save: %v", err)
	}
	if len(factors.added) != 1 {
		t.Fatalf("registration enrolled %d factors, want exactly 1", len(factors.added))
	}
	enrolled := factors.added[0]
	if enrolled.Metadata["tenant_id"] != tenantID {
		t.Fatalf("enrolled factor tenant_id = %q, want %q",
			enrolled.Metadata["tenant_id"], tenantID)
	}
	if _, err := creds.GetByCredentialID(context.Background(), credID); err != nil {
		t.Fatalf("registration did not persist the credential: %v", err)
	}
	return string(enrolled.Credential)
}

// TestTenantUserRegistersThenLogsIn is #529's headline regression: a passkey
// registered against a NON-system tenant's relying party must carry that user
// all the way through login, and login completes only when the session bearer
// is actually minted. Before the fix the ceremony verified the assertion and
// then refused issuance, so this crossing failed at its last step.
func TestTenantUserRegistersThenLogsIn(t *testing.T) {
	// --- registration half -------------------------------------------------
	credID := []byte{0x80, 0x36, 0xfe, 0x36, 0xb8, 0x3b, 0xce, 0xeb}
	lookupKey := registerPasskeyInTenant(t, crossingTenant, 89482037924204544, credID)
	if lookupKey == "" {
		t.Fatal("registration enrolled a factor with an empty login lookup key")
	}

	// --- login half --------------------------------------------------------
	// The proof has verified by this point; what remains is issuance, which is
	// where the ceremony refused. The context is the one the ceremony really
	// has: the login request is unauthenticated and carries NO tenant.
	rbac := newCrossingRBAC()
	provider, storage := newCeremonyProvider(t, &fakePermissionResolver{client: rbac})
	seedCeremonyUser(t, storage, crossingTenant, crossingUser, []string{crossingRoleID})

	bearer, expiresAt, err := provider.IssueSessionToken(context.Background(), SessionIdentity{
		Subject:     crossingUser,
		TenantID:    crossingTenant,
		AuthMethod:  "fido2",
		MFAVerified: true,
	})
	if err != nil {
		t.Fatalf("login/finish must mint a bearer for a tenant user; got: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("bearer expiry %v is not in the future", expiresAt)
	}

	// The role store that was actually read. This is the assertion the defect
	// turns on: an unscoped resolve reads the system partition and cannot see
	// a tenant's roles at all.
	if rbac.resolvedTenant != crossingTenant {
		t.Fatalf("permissions resolved in tenant %q, want %q — an unscoped resolve reads the system store and refuses every tenant role",
			rbac.resolvedTenant, crossingTenant)
	}

	claims, err := provider.ValidateAccessToken(context.Background(), bearer)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.TenantID != crossingTenant {
		t.Errorf("bearer tenant = %q, want %q", claims.TenantID, crossingTenant)
	}
	if !contains(claims.SCP, "orchestrator:status") {
		t.Fatalf("scp = %v, want the tenant role's permissions", claims.SCP)
	}
}

// TestTenantUserLogin_RefusesWhenTheRoleIsAbsentFromItsOwnTenant is the error
// half: scoping the resolve to the token's tenant must not become a way to
// paper over a genuinely unresolvable role. A role missing from the tenant it
// is claimed in still refuses issuance, typed, rather than minting a bearer
// with an empty scp that denies every request it is later presented on.
func TestTenantUserLogin_RefusesWhenTheRoleIsAbsentFromItsOwnTenant(t *testing.T) {
	rbac := newCrossingRBAC()
	provider, storage := newCeremonyProvider(t, &fakePermissionResolver{client: rbac})

	const ghostRole = "00000000-0000-0000-0000-000000000000"
	seedCeremonyUser(t, storage, crossingTenant, crossingUser, []string{ghostRole})

	_, _, err := provider.IssueSessionToken(context.Background(), SessionIdentity{
		Subject:     crossingUser,
		TenantID:    crossingTenant,
		AuthMethod:  "fido2",
		MFAVerified: true,
	})
	if err == nil {
		t.Fatal("issuance succeeded for an unresolvable role; a bearer with no permissions is a login that denies everything afterwards")
	}
	if !strings.Contains(err.Error(), ghostRole) {
		t.Errorf("error %v does not name the unresolvable role %q", err, ghostRole)
	}
	if rbac.resolvedTenant != crossingTenant {
		t.Errorf("the refused resolve ran in tenant %q, want the token's own tenant %q",
			rbac.resolvedTenant, crossingTenant)
	}
}

// TestWithTokenTenant_BindsQRDBsOwnKey pins the #529 regression structurally:
// the tenant binding quicbaas's token storage reads is go-qrdb's own key, so a
// call that crosses into the qrdb SDK can no longer run in the system tenant
// while looking correctly scoped. There is one key (go-quicbaas#1150).
func TestWithTokenTenant_BindsQRDBsOwnKey(t *testing.T) {
	ctx := withTokenTenant(context.Background(), crossingTenant)

	got, ok := qrdbsdk.TenantFromContext(ctx)
	if !ok || got != crossingTenant {
		t.Fatalf("qrdbsdk.TenantFromContext = (%q, %v), want (%q, true)", got, ok, crossingTenant)
	}
	if contextTenant(ctx) != crossingTenant {
		t.Fatalf("contextTenant = %q, want %q: quicbaas and go-qrdb must read one binding", contextTenant(ctx), crossingTenant)
	}
}

// TestWithTokenTenant_EmptyTenantLeavesTheContextAlone is the error-input half:
// an empty tenant must not bind an empty value, because go-qrdb reads an
// explicit empty binding and an absent one the same way but callers cannot
// distinguish them afterwards.
func TestWithTokenTenant_EmptyTenantLeavesTheContextAlone(t *testing.T) {
	base := qrdbsdk.ContextWithTenant(context.Background(), crossingTenant)

	got := withTokenTenant(base, "")
	if tenantID, ok := qrdbsdk.TenantFromContext(got); !ok || tenantID != crossingTenant {
		t.Fatalf("an empty tenant overwrote an existing binding: got (%q, %v)", tenantID, ok)
	}
	if _, ok := qrdbsdk.TenantFromContext(withTokenTenant(context.Background(), "")); ok {
		t.Fatal("an empty tenant bound a tenant onto an unbound context")
	}
}
