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

package permission

import (
	"context"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/permscope"
)

// TestRegister_AddsEveryPermissionToTheQRDBRegistry is the happy path: after
// Register, every contributed permission resolves through the qrdb registry the
// policy decision point and the role store both consult.
func TestRegister_AddsEveryPermissionToTheQRDBRegistry(t *testing.T) {
	Register()

	registered := make(map[qrdbsdk.Permission]struct{})
	for _, perm := range qrdbsdk.AllPermissions() {
		registered[perm] = struct{}{}
	}

	for _, perm := range All() {
		_, ok := registered[perm]
		assert.Truef(t, ok, "permission %q missing from qrdb registry after Register", perm)
		assert.Truef(t, qrdbsdk.IsPermissionRegistered(perm),
			"IsPermissionRegistered(%q) is false after Register", perm)
	}
}

// TestRegister_IsIdempotent guards the property every call site relies on:
// Register may run more than once (composition root plus tests) without
// duplicating entries or panicking.
func TestRegister_IsIdempotent(t *testing.T) {
	Register()
	before := len(qrdbsdk.AllPermissions())

	Register()
	Register()

	assert.Equal(t, before, len(qrdbsdk.AllPermissions()),
		"repeated Register calls changed the registry size")
}

// TestAll_IsWellFormed rejects the malformed entries that would make a
// contributed permission unusable: an entry that does not parse as
// "resource:action" cannot be granted, and a whole-segment wildcard must never
// be registered as a concrete permission (qrdb admits wildcards as role grants
// without registration, so registering one would create a phantom permission
// in every permission picker).
func TestAll_IsWellFormed(t *testing.T) {
	perms := All()
	require.NotEmpty(t, perms, "All returned no permissions")

	seen := make(map[qrdbsdk.Permission]struct{}, len(perms))
	for _, perm := range perms {
		parsed, err := qrdbsdk.ParsePermission(string(perm))
		require.NoErrorf(t, err, "permission %q is not resource:action", perm)
		assert.Equal(t, perm, parsed)

		resource := qrdbsdk.PermissionResource(perm)
		action := qrdbsdk.PermissionAction(perm)
		assert.NotEqualf(t, "*", resource, "permission %q registers a wildcard resource", perm)
		assert.NotEqualf(t, "*", action, "permission %q registers a wildcard action", perm)
		assert.Equalf(t, strings.ToLower(string(perm)), string(perm),
			"permission %q is not lowercase", perm)

		_, dup := seen[perm]
		assert.Falsef(t, dup, "permission %q listed twice in All", perm)
		seen[perm] = struct{}{}
	}
}

// TestAll_DoesNotShadowAQRDBPermission asserts QuicBaaS only ever ADDS to the
// vocabulary. A contributed permission whose name go-qrdb already defines would
// mean two layers claim the same grant, which is precisely the forked-vocabulary
// failure the single-PDP architecture exists to prevent. The check runs against
// a registry snapshot taken before Register so it cannot pass trivially.
func TestAll_DoesNotShadowAQRDBPermission(t *testing.T) {
	contributed := make(map[qrdbsdk.Permission]struct{}, len(All()))
	for _, perm := range All() {
		contributed[perm] = struct{}{}
	}

	// go-qrdb registers its own families from init(), so anything already in
	// the registry that is NOT ours came from qrdb — unless a previous test in
	// this package already ran Register, in which case ours are present too.
	// Compare by resource family instead: no contributed permission may live in
	// a resource family qrdb already owns.
	for _, perm := range qrdbsdk.AllPermissions() {
		if _, ours := contributed[perm]; ours {
			continue
		}
		for _, mine := range All() {
			assert.NotEqualf(t, qrdbsdk.PermissionResource(perm), qrdbsdk.PermissionResource(mine),
				"contributed permission %q lives in resource family %q, which go-qrdb "+
					"already owns (%q); extend qrdb's family instead of forking it",
				mine, qrdbsdk.PermissionResource(mine), perm)
		}
	}
}

// TestAttestationSplit pins the read/write split of the attestation family.
// Both verbs sharing an action, or the attest verb being downgraded to a read,
// would let a principal that may only verify a caller-supplied statement drive
// the host TPM.
func TestAttestationSplit(t *testing.T) {
	assert.Equal(t, "attestation", qrdbsdk.PermissionResource(AttestationVerify))
	assert.Equal(t, "attestation", qrdbsdk.PermissionResource(AttestationAttest))
	assert.Equal(t, "verify", qrdbsdk.PermissionAction(AttestationVerify))
	assert.Equal(t, "attest", qrdbsdk.PermissionAction(AttestationAttest))
	assert.NotEqual(t, AttestationVerify, AttestationAttest)
}

// TestRegister_PlacesPermissionsInBuiltInRoles is the end-to-end assertion for
// the role grants: after the SAME entry point production calls, qrdb's built-in
// role projection — the one pkg/role.BuiltInRoles materializes at role-seed
// time — actually contains the contributed permissions. It reads the live qrdb
// registry rather than re-declaring the grants, so a grant dropped from
// contributions fails here instead of being restated by the test.
func TestRegister_PlacesPermissionsInBuiltInRoles(t *testing.T) {
	Register()

	operator := permissionSet(t, qrdbsdk.PermissionRoleOperator)
	admin := permissionSet(t, qrdbsdk.PermissionRoleAdmin)

	assert.Containsf(t, operator, AttestationVerify,
		"operator is missing %q; the day-to-day verify verb is reachable only "+
			"through a hand-authored custom role", AttestationVerify)
	assert.NotContainsf(t, operator, AttestationAttest,
		"operator holds %q; driving the host TPM is an Admin-only mutation",
		AttestationAttest)

	assert.Containsf(t, admin, AttestationAttest,
		"admin is missing %q; no built-in role can drive the host TPM",
		AttestationAttest)
	assert.Containsf(t, admin, AttestationVerify,
		"admin is missing %q; qrdb's built-in roles are flat sets, so admin does "+
			"not inherit operator's grants and needs the explicit verify grant",
		AttestationVerify)
}

// TestRegister_DeclaresTheDutyOfEveryContribution reads the duty back out of
// qrdb's registry after the entry point production calls, so the classification
// is proven to have REACHED the registry the Security-Officer guard consults
// rather than merely being written in a struct literal here.
//
// attestation:attest is declared crypto use explicitly; attestation:verify is
// deliberately left undeclared, which in qrdb is the fail-safe state rather
// than a permissive one (see TestRegister_KeepsVerifyOutsideTheSecurityOfficerDuties).
func TestRegister_DeclaresTheDutyOfEveryContribution(t *testing.T) {
	Register()

	assert.Equalf(t, qrdbsdk.PermissionDutyCryptoUse,
		qrdbsdk.LookupPermissionDuty(AttestationAttest),
		"%q is not declared crypto use; the classification would then rest on "+
			"qrdb's action-verb backstop alone", AttestationAttest)
	assert.Truef(t, qrdbsdk.IsPermissionCryptoUse(AttestationAttest),
		"qrdb does not classify %q as cryptographic use, so the Security "+
			"Officer guard and the runtime RBAC deny-list would both admit it",
		AttestationAttest)

	assert.Equalf(t, qrdbsdk.PermissionDutyUndeclared,
		qrdbsdk.LookupPermissionDuty(AttestationVerify),
		"%q carries a duty declaration; verifying caller-supplied material is "+
			"neither module governance nor use of a module-held key", AttestationVerify)
	assert.Falsef(t, qrdbsdk.IsPermissionCryptoUse(AttestationVerify),
		"qrdb classifies %q as cryptographic use, which would bar it from the "+
			"Operator role it is meant to serve", AttestationVerify)
}

// TestRegister_IsIdempotentForDutyDeclarations guards the composition root's
// right to call Register more than once. qrdb's duty declarations are immutable:
// re-declaring the SAME duty is a no-op, but a conflicting re-declaration panics
// with PermissionDutyConflictError, so a Register that computed the duty
// differently on a second pass would take the process down at startup.
func TestRegister_IsIdempotentForDutyDeclarations(t *testing.T) {
	Register()

	assert.NotPanics(t, func() {
		Register()
		Register()
	}, "repeated Register calls conflict with their own duty declarations")

	assert.Equal(t, qrdbsdk.PermissionDutyCryptoUse,
		qrdbsdk.LookupPermissionDuty(AttestationAttest))
}

// TestRegister_RefusesAttestGrantToTheSecurityOfficer exercises the guard
// itself rather than trusting the declaration: granting attestation:attest to
// the Security Officer is refused by qrdb, in a family qrdb does not own, so the
// FIPS 140-3 AS10.03 exclusion cannot be undone from this layer by editing the
// roles list. The refusal is a panic (a registration-time programmer error with
// no recovery path), and it happens before any grant is recorded.
func TestRegister_RefusesAttestGrantToTheSecurityOfficer(t *testing.T) {
	Register()

	recovered := recoverFrom(t, func() {
		qrdbsdk.RegisterPermissionRoleGrant(qrdbsdk.PermissionRoleSO, AttestationAttest)
	})
	require.NotNilf(t, recovered,
		"qrdb accepted the Security Officer grant of %q without panicking",
		AttestationAttest)

	violation, ok := recovered.(*qrdbsdk.PermissionSoDViolationError)
	require.Truef(t, ok, "guard panicked with %T (%v), want *PermissionSoDViolationError",
		recovered, recovered)
	assert.Equal(t, qrdbsdk.PermissionRoleSO, violation.Role)
	assert.Equal(t, AttestationAttest, violation.Permission)

	assert.Falsef(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleSO, AttestationAttest),
		"the refused grant of %q still reached the Security Officer role",
		AttestationAttest)
}

// TestRegister_KeepsVerifyOutsideTheSecurityOfficerDuties proves the
// attestation:verify decision is fail-safe. Leaving the duty undeclared is not
// a silent permission: qrdb refuses a Security Officer grant of an unclassified
// permission with PermissionUndeclaredDutyError, so the read verb stays off the
// Security Officer role until an author deliberately declares it governance.
// Had it been declared governance for tidiness, this grant would SUCCEED.
func TestRegister_KeepsVerifyOutsideTheSecurityOfficerDuties(t *testing.T) {
	Register()

	recovered := recoverFrom(t, func() {
		qrdbsdk.RegisterPermissionRoleGrant(qrdbsdk.PermissionRoleSO, AttestationVerify)
	})
	require.NotNilf(t, recovered,
		"qrdb accepted the Security Officer grant of undeclared %q", AttestationVerify)

	undeclared, ok := recovered.(*qrdbsdk.PermissionUndeclaredDutyError)
	require.Truef(t, ok, "guard panicked with %T (%v), want *PermissionUndeclaredDutyError",
		recovered, recovered)
	assert.Equal(t, qrdbsdk.PermissionRoleSO, undeclared.Role)
	assert.Equal(t, AttestationVerify, undeclared.Permission)

	assert.Falsef(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleSO, AttestationVerify),
		"the refused grant of %q still reached the Security Officer role",
		AttestationVerify)
}

// TestRegister_WithholdsAttestFromReadOnlyAndSORoles pins the exclusions that
// carry the security argument. The SO exclusion is enforced structurally by qrdb
// — RegisterRoleGrant panics for a permission declared DutyCryptoUse, as
// attestation:attest is, and for any permission whose action names an
// unambiguous cryptographic primitive in ANY resource family, "attest" among
// them (see TestRegister_RefusesAttestGrantToTheSecurityOfficer, which drives
// that guard). The User and Auditor exclusions have no such backstop: they rest
// on this package's declaration alone, which is why they need a regression test
// rather than trust. This test records all three from qrdb's live registry.
func TestRegister_WithholdsAttestFromReadOnlyAndSORoles(t *testing.T) {
	Register()

	for _, role := range []string{
		qrdbsdk.PermissionRoleSO,
		qrdbsdk.PermissionRoleUser,
		qrdbsdk.PermissionRoleAuditor,
	} {
		t.Run(role, func(t *testing.T) {
			assert.Falsef(t, qrdbsdk.HasPermissionRoleGrant(role, AttestationAttest),
				"role %q holds %q: a quote is a host mutation and a signing "+
					"operation, granted to Admin only", role, AttestationAttest)
			assert.NotContains(t, permissionSet(t, role), AttestationAttest)
		})
	}
}

// TestRegister_GrantsOnlyConcretePermissions guards against a contributor
// reaching for an "attestation:*" whole-segment wildcard to place the family in
// a role. qrdb's RBAC authorizer expands such a wildcard against concrete
// checks, which would silently hand every holder the write verb and collapse
// the read/write split all four transports encode.
func TestRegister_GrantsOnlyConcretePermissions(t *testing.T) {
	Register()

	declared := make(map[qrdbsdk.Permission]struct{}, len(All()))
	for _, perm := range All() {
		declared[perm] = struct{}{}
	}

	for _, role := range []string{
		qrdbsdk.PermissionRoleSO,
		qrdbsdk.PermissionRoleAdmin,
		qrdbsdk.PermissionRoleOperator,
		qrdbsdk.PermissionRoleUser,
		qrdbsdk.PermissionRoleAuditor,
	} {
		for _, granted := range qrdbsdk.PermissionsForRole(role) {
			if qrdbsdk.PermissionResource(granted) != "attestation" {
				continue
			}
			_, ok := declared[granted]
			assert.Truef(t, ok,
				"role %q holds attestation grant %q, which is not a concrete "+
					"permission from All(); wildcard grants collapse the "+
					"read/write split", role, granted)
		}
	}
}

// TestRegister_IsIdempotentForRoleGrants guards the property the composition
// root relies on: Register may run more than once (NewServer plus tests)
// without changing which roles hold which permissions.
func TestRegister_IsIdempotentForRoleGrants(t *testing.T) {
	Register()
	before := permissionSet(t, qrdbsdk.PermissionRoleAdmin)

	Register()
	Register()

	assert.Equal(t, before, permissionSet(t, qrdbsdk.PermissionRoleAdmin),
		"repeated Register calls changed the admin role projection")
}

// TestContributions_DeclareARolePlacementAndADuty rejects the two omissions
// this package's declaration unit exists to prevent:
//
//   - No role placement: a permission contributed to the vocabulary with no
//     default role grant lands in none of the five built-in roles and is
//     reachable only through a hand-authored custom role.
//   - No duty: the FIPS 140-3 AS10.03 classification is unstated. It is
//     detectable here only because this package spells the duty in its own
//     vocabulary, whose zero value (dutyUnstated) is invalid — a field of
//     qrdb's type would make "nobody decided" indistinguishable from the
//     legitimate "not the Security Officer's business".
func TestContributions_DeclareARolePlacementAndADuty(t *testing.T) {
	Register()

	builtIn := map[string]struct{}{
		qrdbsdk.PermissionRoleSO:       {},
		qrdbsdk.PermissionRoleAdmin:    {},
		qrdbsdk.PermissionRoleOperator: {},
		qrdbsdk.PermissionRoleUser:     {},
		qrdbsdk.PermissionRoleAuditor:  {},
	}

	for _, c := range contributions {
		require.NotEmptyf(t, c.roles, "permission %q declares no built-in role",
			c.permission)
		for _, role := range c.roles {
			_, ok := builtIn[role]
			assert.Truef(t, ok, "permission %q names role %q, which is not one of "+
				"qrdb's five built-in roles", c.permission, role)
			assert.Truef(t, qrdbsdk.HasPermissionRoleGrant(role, c.permission),
				"Register did not record the declared %q grant for %q",
				role, c.permission)
		}

		require.NotEqualf(t, dutyUnstated, c.duty,
			"permission %q states no separated duty; declare dutyGovernance, "+
				"dutyCryptoUse, or dutyOutsideSecurityOfficer for a permission "+
				"that is neither", c.permission)

		qrdbDuty, declared := qrdbDuties[c.duty]
		if !declared {
			assert.Equalf(t, qrdbsdk.PermissionDutyUndeclared,
				qrdbsdk.LookupPermissionDuty(c.permission),
				"permission %q is declared outside the Security Officer's duties "+
					"but carries a duty in qrdb's registry", c.permission)
			continue
		}
		assert.Equalf(t, qrdbDuty, qrdbsdk.LookupPermissionDuty(c.permission),
			"Register did not declare the stated duty of %q to qrdb", c.permission)
	}
}

// TestQRDBDuties_MapsOnlyDeclarableDuties pins the mapping's error path: qrdb's
// RegisterPermissionDuty panics with PermissionInvalidDutyError on
// PermissionDutyUndeclared, because "undeclared" must be expressed by omitting
// the call. Both local duties that mean "say nothing" must therefore be absent
// from the map, or Register would panic at startup.
func TestQRDBDuties_MapsOnlyDeclarableDuties(t *testing.T) {
	for _, d := range []duty{dutyUnstated, dutyOutsideSecurityOfficer} {
		_, declared := qrdbDuties[d]
		assert.Falsef(t, declared, "duty %d maps to a qrdb declaration; "+
			"RegisterPermissionDuty would panic on it", d)
	}

	governance, ok := qrdbDuties[dutyGovernance]
	require.True(t, ok, "dutyGovernance maps to no qrdb duty")
	assert.Equal(t, qrdbsdk.PermissionDutyGovernance, governance)

	cryptoUse, ok := qrdbDuties[dutyCryptoUse]
	require.True(t, ok, "dutyCryptoUse maps to no qrdb duty")
	assert.Equal(t, qrdbsdk.PermissionDutyCryptoUse, cryptoUse)
}

// TestAll_ProjectsEveryContribution keeps All and contributions from drifting:
// All is what Register writes to the vocabulary and what the four transport
// guard tests diff against, so a contribution missing from it would register a
// role grant for a permission no transport can resolve.
func TestAll_ProjectsEveryContribution(t *testing.T) {
	perms := All()
	require.Len(t, perms, len(contributions))
	for i, c := range contributions {
		assert.Equal(t, c.permission, perms[i], "All lost contributions[%d] ordering", i)
	}
}

// permissionSet reads a built-in role's live projection from qrdb's grant
// registry as a set, so assertions read as containment rather than slice
// ordering.
func permissionSet(t *testing.T, role string) map[qrdbsdk.Permission]struct{} {
	t.Helper()
	granted := qrdbsdk.PermissionsForRole(role)
	require.NotEmptyf(t, granted, "built-in role %q projects no permissions", role)
	set := make(map[qrdbsdk.Permission]struct{}, len(granted))
	for _, perm := range granted {
		set[perm] = struct{}{}
	}
	return set
}

// recoverFrom runs fn and returns the value it panicked with, or nil when fn
// returned normally. qrdb reports a separation-of-duties violation by panicking
// — it is a registration-time programmer error discovered before any request is
// served — so asserting on that guard means asserting on the panic value's
// concrete type.
func recoverFrom(t *testing.T, fn func()) (recovered any) {
	t.Helper()
	defer func() { recovered = recover() }()
	fn()
	return nil
}

// TestSDLC_PermissionsAreRegisteredWithTheRightRolePlacement pins the SDLC
// module's verbs (go-quicbaas#569): three permissions, the read to Admin and
// Operator, bind and unbind to Admin alone, all outside the Security
// Officer's duties.
func TestSDLC_PermissionsAreRegisteredWithTheRightRolePlacement(t *testing.T) {
	contributed := make(map[qrdbsdk.Permission]contribution)
	for _, c := range contributions {
		contributed[c.permission] = c
	}
	reads := []qrdbsdk.Permission{SDLCRead}
	writes := []qrdbsdk.Permission{SDLCBind, SDLCUnbind}

	for _, perm := range append(append([]qrdbsdk.Permission{}, reads...), writes...) {
		c, ok := contributed[perm]
		require.Truef(t, ok, "%s is not contributed", perm)
		assert.Equalf(t, dutyOutsideSecurityOfficer, c.duty, "%s carries a governance or crypto duty", perm)
		assert.NotContainsf(t, c.roles, qrdbsdk.PermissionRoleSO, "%s granted to the Security Officer", perm)
	}
	for _, perm := range reads {
		assert.Contains(t, contributed[perm].roles, qrdbsdk.PermissionRoleAdmin, perm)
		assert.Contains(t, contributed[perm].roles, qrdbsdk.PermissionRoleOperator, perm)
	}
	for _, perm := range writes {
		assert.Contains(t, contributed[perm].roles, qrdbsdk.PermissionRoleAdmin, perm)
		assert.NotContainsf(t, contributed[perm].roles, qrdbsdk.PermissionRoleOperator,
			"%s is a configuration write and must not be Operator's", perm)
	}
	// The registration-era verbs are gone: a stale grant must not resolve.
	for _, gone := range []qrdbsdk.Permission{"sdlc:list", "sdlc:register", "sdlc:update", "sdlc:deregister"} {
		_, ok := contributed[gone]
		assert.Falsef(t, ok, "%s is still contributed", gone)
	}
}

// TestEveryContributedPermissionStatesItsScope is the source-level gate:
// every entry in contributions must say how far its permission reaches.
//
// go-qrdb#504 removed the implicit self-scope default, so forgetting a scope
// does not fail open — it denies every request that needs the permission, at
// runtime, in whichever transport reaches it first. The test reads the table
// rather than the registry on purpose: a scope field typed
// qrdbsdk.PermissionScope would have made this assertion VACUOUS, because
// qrdb's zero value IS PermissionScopeSelf, so an omitted field would
// register self-scope and read back as self-scope in perfect agreement. The
// local scope type's zero value is scopeUnstated, which nothing maps, so the
// omission has nowhere to hide.
func TestEveryContributedPermissionStatesItsScope(t *testing.T) {
	for _, c := range contributions {
		assert.NotEqualf(t, scopeUnstated, c.scope,
			"%s: no scope stated; add one to its contributions entry", c.permission)
		_, mapped := qrdbScopes[c.scope]
		assert.Truef(t, mapped,
			"%s: scope %d has no qrdbScopes row, so Register would leave it undeclared",
			c.permission, c.scope)
	}
}

// TestRegisterDeclaresTheStatedScope proves the PRODUCTION registration path
// is what makes a contributed permission resolvable — not a test-local
// RegisterPermissionScope call. This is the gap the #963 review caught: a
// synthetic widgets:* pair had been declared inside a test while the real
// vocabulary went undeclared.
//
// The read-back goes through the SDK's per-tenant scope store
// (pkg/testutil/permscope), which falls through to the process-global
// declaration layer for a tenant that has defined nothing — the locked SDK
// boundary holds in tests too, so go-qrdb/pkg/permission is not imported here.
func TestRegisterDeclaresTheStatedScope(t *testing.T) {
	Register()

	resolver, err := permscope.New()
	require.NoError(t, err)

	for _, c := range contributions {
		got, err := resolver.Scope(context.Background(), c.permission)
		if !assert.NoErrorf(t, err, "%s: Register() left it without a scope", c.permission) {
			continue
		}
		assert.Equalf(t, qrdbScopes[c.scope], got,
			"%s: registry and contributions disagree", c.permission)
	}
}

// TestRegisterLeavesNoContributedPermissionUndeclared walks All() rather than
// contributions, so a permission that reaches the vocabulary by some other
// route than a contributions entry is still held to the same rule.
func TestRegisterLeavesNoContributedPermissionUndeclared(t *testing.T) {
	Register()

	resolver, err := permscope.New()
	require.NoError(t, err)

	for _, p := range All() {
		_, err := resolver.Scope(context.Background(), p)
		assert.NoErrorf(t, err, "%s is in All() but has no declared scope", p)
	}
}
