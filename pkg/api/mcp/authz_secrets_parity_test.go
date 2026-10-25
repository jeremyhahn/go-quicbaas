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

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/role"
	"github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// secretsVaultMCPGrants is the ONE canonical answer to "what does a caller need
// to operate on a tenant's secret vault over MCP".
//
// The vault is served on five transports, each with its own gate: the frozen
// route table for REST and the QUIC transport that carries those same routes
// over HTTP/3, grpcMethodPermissions plus the (qrdb.v1.permission) annotation
// for gRPC and the Unix socket, and qrdbToolExactMapping here. Before
// go-quicbaas#682 this table named credentials:* while REST named secrets:*,
// so the same principal storing the same secret was admitted over REST and
// denied over MCP — an authorization outcome that depended on which wire the
// call took (go-quicbaas#534 remapped REST; this table and the gRPC one were
// left behind).
//
// The values below are the REST rows, verb for verb:
//
//	REST                              MCP tool                     grant
//	GET    /api/v1/secrets/           quicbaas_secrets_list        secrets:list
//	GET    /api/v1/secrets/*          quicbaas_secrets_get         secrets:read
//	GET    /api/v1/secrets-prefix/*   quicbaas_secrets_get_prefix  secrets:read
//	GET    /api/v1/secrets-versions/* quicbaas_secrets_versions    secrets:read
//	GET    /api/v1/secrets-watch/*    quicbaas_secrets_watch       secrets:read
//	POST   /api/v1/secrets/*          store / ensure / rotate /
//	                                  rollback                     secrets:write
//	PUT    /api/v1/secrets/*          quicbaas_secrets_update_status secrets:write
//	DELETE /api/v1/secrets/*          quicbaas_secrets_delete      secrets:delete
//
// The two rotation-policy tools have no REST route of their own; they take
// read / write of the policy sub-resource on the same family.
//
// Each plane pins itself to these values in its own package, since none of the
// three tables is exported to the others:
//
//	REST: pkg/api/rest/handlers_secrets_tenant_scope_test.go
//	      (TestRoutes_SecretsVaultCarriesTenantScopedPermissions)
//	gRPC: pkg/api/grpc/authz_secrets_parity_test.go
//
// Changing one means changing all three deliberately.
var secretsVaultMCPGrants = map[string]qrdbToolResource{
	"quicbaas_secrets_store":               {"secrets", "write"},
	"quicbaas_secrets_ensure":              {"secrets", "write"},
	"quicbaas_secrets_rotate":              {"secrets", "write"},
	"quicbaas_secrets_rollback":            {"secrets", "write"},
	"quicbaas_secrets_update_status":       {"secrets", "write"},
	"quicbaas_secrets_set_rotation_policy": {"secrets", "write"},
	"quicbaas_secrets_get":                 {"secrets", "read"},
	"quicbaas_secrets_get_prefix":          {"secrets", "read"},
	"quicbaas_secrets_versions":            {"secrets", "read"},
	"quicbaas_secrets_watch":               {"secrets", "read"},
	"quicbaas_secrets_get_rotation_policy": {"secrets", "read"},
	"quicbaas_secrets_list":                {"secrets", "list"},
	"quicbaas_secrets_delete":              {"secrets", "delete"},
}

// secretsVaultToolPrefix is the tool-name prefix of the vault surface. The
// dynamic-secrets tools (quicbaas_dynamic_secrets_*) do NOT share it and keep
// their own dynamic_secrets:* family, which is a different, correct store.
const secretsVaultToolPrefix = "quicbaas_secrets_"

// TestSecretsVaultMCPPermissions pins the MCP gate to the canonical grants, in
// both directions: every canonical tool carries the expected permission, and
// every quicbaas_secrets_* entry in the live table is a canonical tool. The
// reverse direction is what catches a NEW vault tool being added with a
// credentials:* grant copied from a neighbouring block.
func TestSecretsVaultMCPPermissions(t *testing.T) {
	for tool, want := range secretsVaultMCPGrants {
		got, ok := qrdbToolExactMapping[tool]
		if !ok {
			t.Errorf("tool %q has no permission mapping: the authz gate is default-deny, "+
				"so it would be invisible in tools/list", tool)
			continue
		}
		if got != want {
			t.Errorf("tool %q is gated %v, but REST and gRPC gate the same verb %v; "+
				"the grant must not depend on which transport carried the call", tool, got, want)
		}
	}

	// The reverse direction. TestMCPPermissions_EveryToolMappedOrPublic
	// (authz_qrdb_coverage_test.go) separately proves every REGISTERED tool
	// has an entry, so between the two a new vault tool can be neither
	// unmapped nor mapped onto a family the other transports do not use.
	for tool, got := range qrdbToolExactMapping {
		if !strings.HasPrefix(tool, secretsVaultToolPrefix) {
			continue
		}
		want, ok := secretsVaultMCPGrants[tool]
		if !ok {
			t.Errorf("vault tool %q is mapped %v but is not in the canonical grant set; "+
				"add it here and to the REST and gRPC tables, or it authorizes differently per transport",
				tool, got)
			continue
		}
		assert.Equal(t, want, got, tool)
	}
}

// TestSecretsVaultMCPPermissions_UnmappedToolIsDenied is the bad-input half: a
// tool name that is not a real vault tool (a typo, or one removed from the
// registry) must resolve unmapped, so the default-deny gate refuses it rather
// than inheriting a vault grant from a prefix rule.
func TestSecretsVaultMCPPermissions_UnmappedToolIsDenied(t *testing.T) {
	for _, tool := range []string{
		"quicbaas_secrets_destroy", // never existed
		"quicbaas_secret_get",      // singular typo
		secretsVaultToolPrefix,     // bare prefix
		"",
	} {
		resource, action, ok := getToolQRDBPermission(tool)
		assert.Falsef(t, ok, "tool %q must not resolve to a permission (got %s:%s)", tool, resource, action)
	}
}

// secretsVaultRoleServicer is a minimal role.RoleServicer double (nil embed,
// only ResolvePermissions overridden) backing a REAL rbac.RBACAuthorizer. It
// grants exactly one permission, so the scope decision is the only thing that
// can change the outcome.
type secretsVaultRoleServicer struct {
	role.RoleServicer
	granted permission.Permission
}

func (r *secretsVaultRoleServicer) ResolvePermissions(context.Context, []string) ([]permission.Permission, error) {
	return []permission.Permission{r.granted}, nil
}

// authorizeVaultPermission runs go-qrdb's REAL RBAC authorizer — the policy
// decision point the MCP gate's platform-token validator enforces on behalf of
// — for an identity that HOLDS the permission, acting on targetTenant. A fresh
// authorizer per call keeps the per-(tenant, subject) permission cache out of
// the result.
func authorizeVaultPermission(t *testing.T, perm permission.Permission, identityTenant, targetTenant string) error {
	t.Helper()
	resource, action, found := strings.Cut(string(perm), ":")
	require.True(t, found, "permission %q must be a resource:action join", perm)

	authorizer := rbac.New(&secretsVaultRoleServicer{granted: perm})
	identity := &qrdbauth.Identity{Subject: "vault-caller", TenantID: identityTenant, Roles: []string{"admin"}}
	ctx := tenant.WithTenantID(context.Background(), targetTenant)
	return authorizer.Authorize(ctx, identity, resource, action)
}

// TestSecretsVaultMCPPermissions_TenantScopeOutcomes is the reason the remap
// matters, proven against qrdb's own authorizer rather than restated: for every
// tool this transport serves, the permission it now requires admits a tenant
// acting in its OWN vault, refuses that same tenant naming ANOTHER tenant, and
// never lifts the system tenant across tenants — while admitting the system
// tenant to its own vault.
//
// That is qrdb's ScopeSelfStrict posture for secrets:* (go-qrdb#372): every
// identity, system tenant included, reaches its own tenant's vault and no
// other. It closes go-quicbaas#689 upstream — the system tenant has its own
// vault again — without the credentials:* fallback that would reopen the
// cross-tenant reach #534 closed.
//
// The permission comes from the LIVE table, so a tool that drifts back to
// credentials:* (ScopeCross) fails here with the symptom an operator would hit
// — every tenant row denied and the system row admitted — not just with a
// mismatched literal.
func TestSecretsVaultMCPPermissions_TenantScopeOutcomes(t *testing.T) {
	const (
		ownTenant   = "automatethethings"
		otherTenant = "burnin"
	)

	for tool := range secretsVaultMCPGrants {
		resource, action, ok := getToolQRDBPermission(tool)
		require.Truef(t, ok, "vault tool %q is unmapped and therefore default-denied", tool)
		perm := permission.Permission(resource + ":" + action)
		name := strings.TrimPrefix(tool, secretsVaultToolPrefix)

		t.Run(name+"/own_tenant_admitted", func(t *testing.T) {
			assert.NoError(t, authorizeVaultPermission(t, perm, ownTenant, ownTenant),
				"%s requires %s, which must admit a tenant acting in its own vault", tool, perm)
		})

		t.Run(name+"/other_tenant_refused", func(t *testing.T) {
			err := authorizeVaultPermission(t, perm, ownTenant, otherTenant)
			require.Error(t, err,
				"%s requires %s; a tenant naming another tenant must be refused, never misfiled", tool, perm)
			assert.Contains(t, err.Error(), "not in target tenant",
				"the refusal must name the cross-tenant reason: %v", err)
		})

		t.Run(name+"/system_tenant_not_lifted", func(t *testing.T) {
			err := authorizeVaultPermission(t, perm, qrdbsdk.SystemTenantID, ownTenant)
			require.Error(t, err,
				"%s requires %s, which qrdb registers self-strict; the system tenant must not reach a tenant's vault", tool, perm)
			assert.Contains(t, err.Error(), "system tenant is not lifted",
				"the denial must name the self-strict reason: %v", err)
		})

		t.Run(name+"/system_tenant_own_vault_admitted", func(t *testing.T) {
			assert.NoError(t, authorizeVaultPermission(t, perm, qrdbsdk.SystemTenantID, qrdbsdk.SystemTenantID),
				"%s requires %s, which must admit the system tenant acting in its own vault", tool, perm)
		})
	}
}
