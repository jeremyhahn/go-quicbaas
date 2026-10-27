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

package apimodule

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/permscope"
)

// TestNewModule_RegistersRoleGrants proves module construction contributes
// the module's gRPC-declared permission set into qrdb's registries, and does
// so WITHOUT overriding go-qrdb's own FIPS 140-3 AS10.03-curated built-in
// role placements (see module_grants.go's doc comment for the full
// rationale): a permission some built-in role already holds is left exactly
// as qrdb placed it, and only a permission held by NO built-in role is
// filled in for Admin/Owner. Every assertion holds whatever ran earlier in
// the process; what the module itself contributes is proved by the
// roleGrantGaps tests.
func TestNewModule_RegistersRoleGrants(t *testing.T) {
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	// keys:sign is declared by ssh.proto (this module's SSHService) and is
	// already held by a built-in role, so this module never grants it.
	// Whether Admin holds it depends on what else the process composed (go-xkms
	// grants it to Admin); TestComposedModules_AdminHoldsKeysSign pins that.
	assert.True(t, qrdbsdk.IsPermissionRegistered("keys:sign"),
		"keys:sign must be registered so role creation never fails on it")
	assert.True(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleOperator, "keys:sign"),
		"keys:sign must still be reachable via the Operator role qrdb granted it to")

	// embedding:embed is declared by embedding.proto (this module's
	// EmbeddingService) and go-qrdb v0.5.1-alpha places it on NO built-in
	// role: a genuine gap the module must fill for Admin/Owner so its own
	// EmbeddingService is reachable by the built-in administrator.
	//
	// No credentials:* permission is in this list any more. The account
	// surface moved to QuicBaaS's own tenant-scoped agent_accounts:* family —
	// the reads for go-quicbaas#734, the writes for go-quicbaas#1355 — because
	// credentials:* is registered ScopeCross (system tenant only) and therefore
	// denied every tenant identity. No proto in this module declares a
	// credentials:* permission now, so asserting its grant would pin a
	// permission the module no longer references.
	for _, perm := range []qrdbsdk.Permission{"embedding:embed"} {
		assert.True(t, qrdbsdk.IsPermissionRegistered(perm), "%s must be registered", perm)
		assert.True(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleAdmin, perm),
			"admin must hold %s (unclaimed gap fill)", perm)
		assert.True(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.RoleNameOwner, perm),
			"owner must hold %s (unclaimed gap fill)", perm)
	}

	// The SoD invariant: the Security Officer role receives nothing from
	// this contribution, gap-fill included.
	assert.False(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleSO, "embedding:embed"))
	assert.False(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleSO, "keys:sign"))
}

// TestRoleGrantGaps_KeepsOnlyUnheldPermissions proves the gap computation
// keeps exactly the permissions no built-in role holds, in input order.
func TestRoleGrantGaps_KeepsOnlyUnheldPermissions(t *testing.T) {
	held := map[qrdbsdk.Permission]bool{"keys:sign": true}
	perms := []qrdbsdk.Permission{"credentials:store", "keys:sign", "embedding:embed"}

	got := roleGrantGaps(perms, func(p qrdbsdk.Permission) bool { return held[p] })

	assert.Equal(t, []qrdbsdk.Permission{"credentials:store", "embedding:embed"}, got)
}

// TestRoleGrantGaps_EmptyAndAllHeldInputs pins the degenerate inputs: no
// permissions, or every permission already held, leaves nothing to grant.
func TestRoleGrantGaps_EmptyAndAllHeldInputs(t *testing.T) {
	allHeld := func(qrdbsdk.Permission) bool { return true }
	noneHeld := func(qrdbsdk.Permission) bool { return false }

	assert.Nil(t, roleGrantGaps(nil, noneHeld))
	assert.Nil(t, roleGrantGaps([]qrdbsdk.Permission{"credentials:store", "keys:sign"}, allHeld))
}

// TestRoleGrantGaps_ModuleContributionAgainstQRDBPlacements proves what this
// module grants Admin and Owner, whatever ran earlier in the process. The
// predicate reads only the built-in roles neither this module nor go-xkms
// grants into, so another module's Admin grants cannot change the answer:
// keys:sign, which qrdb places on Operator, is never a gap, and
// credentials:store, which qrdb places on no role, is one.
func TestRoleGrantGaps_ModuleContributionAgainstQRDBPlacements(t *testing.T) {
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	perms := derivedPermissions()
	require.Contains(t, perms, qrdbsdk.Permission("keys:sign"), "ssh.proto declares keys:sign")
	require.Contains(t, perms, qrdbsdk.Permission("embedding:embed"), "embedding.proto declares embedding:embed")
	require.NotContains(t, perms, qrdbsdk.Permission("credentials:store"),
		"account.proto takes agent_accounts:write, not the ScopeCross credentials:store (#1355)")

	nonAdminRoles := []string{
		qrdbsdk.PermissionRoleSO,
		qrdbsdk.PermissionRoleOperator,
		qrdbsdk.PermissionRoleUser,
		qrdbsdk.PermissionRoleAuditor,
	}
	heldByNonAdminRole := func(p qrdbsdk.Permission) bool {
		for _, role := range nonAdminRoles {
			if qrdbsdk.HasPermissionRoleGrant(role, p) {
				return true
			}
		}
		return false
	}

	gaps := roleGrantGaps(perms, heldByNonAdminRole)
	assert.NotContains(t, gaps, qrdbsdk.Permission("keys:sign"), "keys:sign is held by Operator; the module must not grant it")
	assert.Contains(t, gaps, qrdbsdk.Permission("embedding:embed"), "embedding:embed is held by no built-in role")
	for _, p := range gaps {
		assert.False(t, heldByNonAdminRole(p), "%s is already held by a built-in role", p)
	}
}

// TestComposedModules_AdminHoldsKeysSign pins the owner ruling of 2026-10-17:
// Admin holds every permission outside the Security Officer set, keys:sign
// included. It composes both modules as pkg/server/qrdb_node.go does; go-xkms
// grants keys:sign to Admin, while qrdb's own placement on Operator and the
// Security Officer's separation of duties stand.
func TestComposedModules_AdminHoldsKeysSign(t *testing.T) {
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)
	_, err = xkmssdk.NewServiceModule(func() *xkmssdk.XKMSService { return nil })
	require.NoError(t, err)

	assert.True(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleAdmin, "keys:sign"),
		"admin must hold keys:sign in the composed server")
	assert.True(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleOperator, "keys:sign"),
		"operator must still hold keys:sign")
	assert.False(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleSO, "keys:sign"),
		"the security officer must not hold keys:sign")
}

// TestNewModule_RegistersRoleGrants_NewBrokerDataPlaneServices proves
// registerRoleGrants's contributeGRPCPermissions pass — which iterates
// grpcRegistrations generically by resolving each entry's proto
// FileDescriptor rather than importing one File_..._proto variable per
// service (module_grants.go's doc comment) — picks up the permission
// vocabulary declared by the five broker data-plane services this module
// newly registers (parameter_store.proto, dynamic_secrets.proto,
// objectstorage.proto, ollama.proto) without requiring any edit to
// module_grants.go itself.
func TestNewModule_RegistersRoleGrants_NewBrokerDataPlaneServices(t *testing.T) {
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	for _, perm := range []qrdbsdk.Permission{
		"parameters:read",
		"parameters:write",
		"dynamic_secrets:generate",
		"objstore:put",
		"ollama:embed",
	} {
		assert.True(t, qrdbsdk.IsPermissionRegistered(perm),
			"%s must be registered by the new broker data-plane services' proto permission options", perm)
	}
}

// TestNewModule_RegistersRoleGrants_CryptoWireServices proves the
// permission vocabulary of the quicbaas.v1 crypto services the node serves
// for the SDK's gRPC transport (KeyManagement, DEKManagement, Crypto,
// CertificateService, TrustService, AttestationService, PIVService — #1960)
// reaches the role grants, so the node's PDP can admit their callers.
//
// keys:create/keys:get/keys:list are deliberately absent from this list:
// ssh.proto declares the identical strings for its own SSH-key operations,
// so their presence proves nothing about kms.proto. ca:create/
// certs:generate/certs:read/attestation:verify/piv:read are exclusive to
// certificate.proto/piv.proto/attestation.proto, so they are a clean signal.
func TestNewModule_RegistersRoleGrants_CryptoWireServices(t *testing.T) {
	set := map[string]struct{}{}
	contributeGRPCPermissions(set)

	for _, perm := range []string{
		"ca:create",
		"certs:generate",
		"certs:read",
		"attestation:verify",
		"piv:read",
	} {
		_, ok := set[perm]
		assert.True(t, ok,
			"%s must be contributed by quicbaas's module: the node serves the service that declares it", perm)
	}
}

// TestNewModule_PreservesUpstreamPermissionScopes is the regression guard for
// the widest hole this module could open.
//
// Most of the module's derived vocabulary is go-qrdb's own, and qrdb declares
// each family's tenancy reach deliberately: credentials:* is ScopeCross
// because the credential store has no tenant namespacing at all, the
// objstore:* / tenant_barrier:* families are ScopeTenantOnly because a platform
// (system-tenant) operator must never reach a tenant's objects or per-tenant
// barrier key custody, and secrets:* is qrdb's self-strict scope: every
// identity reaches its own tenant's vault and no identity is lifted across
// tenants (go-qrdb#372).
//
// RegisterPermissionScope OVERWRITES. Declaring the whole derived set
// self-scope — which is what this module did until the #963 review caught it
// — would therefore not merely be redundant: self-scope is the one reach the
// system-tenant rule lifts across EVERY tenant, so it would hand a platform
// admin exactly the cross-tenant reach those declarations withhold, silently,
// at process start. The module declares a scope only for a permission it is
// the first to name.
func TestNewModule_PreservesUpstreamPermissionScopes(t *testing.T) {
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	resolver, err := permscope.New()
	require.NoError(t, err)

	cases := []struct {
		permission qrdbsdk.Permission
		want       qrdbsdk.PermissionScope
		why        string
	}{
		{"credentials:store", qrdbsdk.PermissionScopeCross,
			"the credential store is not tenant-namespaced; qrdb confines the family to the system tenant"},
		{"credentials:rotate", qrdbsdk.PermissionScopeCross, "same family, same reason"},
		{"objstore:put", qrdbsdk.PermissionScopeTenantOnly, "same broker family, same reason"},
		{"tenant_barrier:unseal", qrdbsdk.PermissionScopeTenantOnly,
			"per-tenant barrier key custody is the one thing a platform operator must not hold"},
	}
	for _, tc := range cases {
		t.Run(string(tc.permission), func(t *testing.T) {
			got, err := resolver.Scope(context.Background(), tc.permission)
			require.NoError(t, err)
			assert.Equalf(t, tc.want, got, "%s: %s", tc.permission, tc.why)
		})
	}

	// secrets:* is self-strict upstream, a scope the qrdb SDK does not name.
	// What this module must not do is overwrite it with a reach that lifts a
	// platform operator across tenants.
	for _, p := range []qrdbsdk.Permission{"secrets:read", "secrets:write"} {
		got, err := resolver.Scope(context.Background(), p)
		require.NoError(t, err)
		assert.NotEqualf(t, qrdbsdk.PermissionScopeSelf, got, "%s was widened to self-scope", p)
		assert.NotEqualf(t, qrdbsdk.PermissionScopeCross, got, "%s was widened to cross-scope", p)
	}
}

// TestNewModule_DeclaresAScopeForEveryDerivedPermission is the other half:
// leaving upstream declarations alone must not leave anything UNdeclared.
// go-qrdb#504 made an undeclared scope a deny on every call, so a derived
// permission the module names first and then says nothing about would deny
// the module's own route.
func TestNewModule_DeclaresAScopeForEveryDerivedPermission(t *testing.T) {
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	resolver, err := permscope.New()
	require.NoError(t, err)

	set := map[string]struct{}{}
	if restRouteContributor != nil {
		restRouteContributor(set)
	}
	if mcpToolContributor != nil {
		mcpToolContributor(set)
	}
	contributeGRPCPermissions(set)
	require.NotEmpty(t, set, "the derived set must not be empty, or this proves nothing")

	for name := range set {
		_, err := resolver.Scope(context.Background(), qrdbsdk.Permission(name))
		assert.NoErrorf(t, err,
			"%s is derived by this module but has no declared scope, so the authorizer denies it on every call", name)
	}
}

// TestUndeclaredPermissions_SkipsWhatIsAlreadyRegistered is the unit behind
// both guards above: a permission some layer already registered arrives with
// its author's scope declaration attached and must not be reported as this
// module's to declare.
func TestUndeclaredPermissions_SkipsWhatIsAlreadyRegistered(t *testing.T) {
	novel := qrdbsdk.Permission("apimodule_test:never_registered_anywhere")
	require.False(t, qrdbsdk.IsPermissionRegistered(novel),
		"the probe must genuinely be absent from the registry")

	got := undeclaredPermissions([]qrdbsdk.Permission{
		"credentials:store",
		"secrets:read",
		"keys:sign",
		novel,
	})

	assert.Equal(t, []qrdbsdk.Permission{novel}, got,
		"only the permission nobody has registered is this module's to scope")
}

// TestUndeclaredPermissions_EmptyAndAllKnownInputs covers the two boundaries:
// nothing to classify, and nothing novel to report. Both must answer nil
// rather than an empty non-nil slice, because the caller gates its
// RegisterPermissionScope call on the length.
func TestUndeclaredPermissions_EmptyAndAllKnownInputs(t *testing.T) {
	assert.Nil(t, undeclaredPermissions(nil))
	assert.Nil(t, undeclaredPermissions([]qrdbsdk.Permission{}))
	assert.Nil(t, undeclaredPermissions([]qrdbsdk.Permission{"credentials:store", "keys:sign"}))
}
