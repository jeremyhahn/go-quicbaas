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

package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/role"
	"github.com/jeremyhahn/go-qrdb/pkg/tenant"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// secretsVaultGRPCGrants is the ONE canonical answer to "what does a caller
// need to operate on a tenant's secret vault over gRPC".
//
// The vault is served on five transports, each with its own gate: this map
// plus the (qrdb.v1.permission) annotation on the RPC for gRPC and the Unix
// socket that delegates to it, the frozen route table for REST and the QUIC
// transport that carries those same routes over HTTP/3, and
// qrdbToolExactMapping for MCP. Before go-quicbaas#682 this table named
// credentials:* while REST named secrets:*, so the same principal storing the
// same secret was admitted over REST and denied over gRPC — an authorization
// outcome that depended on which wire the call took (go-quicbaas#534 remapped
// REST; this table and the MCP one were left behind).
//
// The values below are the REST rows, verb for verb:
//
//	REST                             gRPC
//	GET    /api/v1/secrets/          ListSecrets                 secrets:list
//	GET    /api/v1/secrets/*         GetSecret, GetSecretVersion secrets:read
//	GET    /api/v1/secrets-prefix/*  GetSecretsByPrefix          secrets:read
//	GET    /api/v1/secrets-versions/* ListSecretVersions         secrets:read
//	GET    /api/v1/secrets-watch/*   WatchSecretsByPrefix        secrets:read
//	POST   /api/v1/secrets/*         StoreSecret, EnsureSecret,
//	                                 RotateSecret, RollbackSecret secrets:write
//	PUT    /api/v1/secrets/*         UpdateSecretStatus          secrets:write
//	DELETE /api/v1/secrets/*         DeleteSecret                secrets:delete
//
// The two rotation-policy verbs have no REST route of their own; they take
// read / write of the policy sub-resource on the same family.
//
// Each plane pins itself to these values in its own package, since none of the
// three tables is exported to the others:
//
//	REST: pkg/api/rest/handlers_secrets_tenant_scope_test.go
//	      (TestRoutes_SecretsVaultCarriesTenantScopedPermissions)
//	MCP:  pkg/api/mcp/authz_secrets_parity_test.go
//	gRPC: this file, plus TestGRPCPermissions_ProtoOptionMatchesAuthzMapping
//	      which proves the proto annotation agrees with the map below.
//
// Changing one means changing all three deliberately.
var secretsVaultGRPCGrants = map[string]qrdbToolResourceLike{
	"/quicbaas.kms.v1.SecretsManagement/StoreSecret":          {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/EnsureSecret":         {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/RotateSecret":         {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/RollbackSecret":       {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/UpdateSecretStatus":   {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/SetRotationPolicy":    {"secrets", "write"},
	"/quicbaas.kms.v1.SecretsManagement/GetSecret":            {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/GetSecretVersion":     {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/ListSecretVersions":   {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/GetSecretsByPrefix":   {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/WatchSecretsByPrefix": {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/GetRotationPolicy":    {"secrets", "read"},
	"/quicbaas.kms.v1.SecretsManagement/ListSecrets":          {"secrets", "list"},
	"/quicbaas.kms.v1.SecretsManagement/DeleteSecret":         {"secrets", "delete"},
}

// secretsManagementMethodPrefix is the FullMethod prefix of the vault service.
const secretsManagementMethodPrefix = "/quicbaas.kms.v1.SecretsManagement/"

// TestSecretsVaultGRPCPermissions pins the gRPC gate to the canonical grants,
// in both directions: every canonical verb carries the expected permission, and
// every SecretsManagement entry in the live table is a canonical verb. The
// reverse direction is what catches a NEW vault RPC being added with a
// credentials:* grant copied from a neighbouring block.
func TestSecretsVaultGRPCPermissions(t *testing.T) {
	for method, want := range secretsVaultGRPCGrants {
		got, ok := grpcMethodPermissions[method]
		if !ok {
			t.Errorf("method %q has no permission mapping: under RequireAuthz it is default-denied, "+
				"so the verb would be unreachable over gRPC and the Unix socket", method)
			continue
		}
		if got.resource != want.resource || got.action != want.action {
			t.Errorf("method %q is gated %s:%s, but REST and MCP gate the same verb %s:%s; "+
				"the grant must not depend on which transport carried the call",
				method, got.resource, got.action, want.resource, want.action)
		}
	}

	for method, got := range grpcMethodPermissions {
		if !strings.HasPrefix(method, secretsManagementMethodPrefix) {
			continue
		}
		want, ok := secretsVaultGRPCGrants[method]
		if !ok {
			t.Errorf("vault method %q is mapped %s:%s but is not in the canonical grant set; "+
				"add it here and to the REST and MCP tables, or it authorizes differently per transport",
				method, got.resource, got.action)
			continue
		}
		assert.Equal(t, want.resource, got.resource, method)
		assert.Equal(t, want.action, got.action, method)
	}
}

// TestSecretsVaultGRPCPermissions_UnmappedMethodIsDenied is the bad-input half:
// a FullMethod that is not a real vault RPC (a typo, or a verb removed from the
// proto) must resolve unmapped, so the interceptor default-denies it rather
// than inheriting a vault grant from some prefix rule.
func TestSecretsVaultGRPCPermissions_UnmappedMethodIsDenied(t *testing.T) {
	for _, method := range []string{
		secretsManagementMethodPrefix + "StoreSecrets",  // plural typo
		secretsManagementMethodPrefix + "DestroySecret", // never existed
		secretsManagementMethodPrefix,                   // service, not a method
		"",
	} {
		resource, action, ok := grpcResourceAndAction(method)
		assert.Falsef(t, ok, "method %q must not resolve to a permission (got %s:%s)", method, resource, action)
	}
}

// secretsVaultRoleServicer is a minimal role.RoleServicer double (nil embed,
// only ResolvePermissions overridden) backing a REAL rbac.RBACAuthorizer. It
// grants exactly one permission, so the scope decision below is the only thing
// that can change the outcome.
type secretsVaultRoleServicer struct {
	role.RoleServicer
	granted permission.Permission
}

func (r *secretsVaultRoleServicer) ResolvePermissions(context.Context, []string) ([]permission.Permission, error) {
	return []permission.Permission{r.granted}, nil
}

// authorizeVaultPermission runs go-qrdb's REAL RBAC authorizer — the policy
// decision point the gRPC PEP's platform-token validator enforces on behalf of
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

// TestSecretsVaultGRPCPermissions_TenantScopeOutcomes is the reason the remap
// matters, proven against qrdb's own authorizer rather than restated: for every
// verb this transport serves, the permission it now requires admits a tenant
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
// Under the old credentials:* mapping every case inverts: credentials:* is
// ScopeCross, so the tenant rows would be denied and the system row admitted.
// That is what makes this test load-bearing rather than a restatement of the
// table.
func TestSecretsVaultGRPCPermissions_TenantScopeOutcomes(t *testing.T) {
	const (
		ownTenant   = "automatethethings"
		otherTenant = "burnin"
	)

	for method := range secretsVaultGRPCGrants {
		// The permission comes from the LIVE table, not the canonical map, so
		// this test reports the outcomes the transport actually produces: a
		// table entry that drifts back to credentials:* fails here with the
		// symptom an operator would hit, not just with a mismatched literal.
		resource, action, ok := grpcResourceAndAction(method)
		require.Truef(t, ok, "vault method %q is unmapped and therefore default-denied", method)
		perm := permission.Permission(resource + ":" + action)
		name := strings.TrimPrefix(method, secretsManagementMethodPrefix)

		t.Run(name+"/own_tenant_admitted", func(t *testing.T) {
			assert.NoError(t, authorizeVaultPermission(t, perm, ownTenant, ownTenant),
				"%s requires %s, which must admit a tenant acting in its own vault", method, perm)
		})

		t.Run(name+"/other_tenant_refused", func(t *testing.T) {
			err := authorizeVaultPermission(t, perm, ownTenant, otherTenant)
			require.Error(t, err,
				"%s requires %s; a tenant naming another tenant must be refused, never misfiled", method, perm)
			assert.Contains(t, err.Error(), "not in target tenant",
				"the refusal must name the cross-tenant reason: %v", err)
		})

		t.Run(name+"/system_tenant_not_lifted", func(t *testing.T) {
			err := authorizeVaultPermission(t, perm, tenant.SystemTenantID, ownTenant)
			require.Error(t, err,
				"%s requires %s, which qrdb registers self-strict; the system tenant must not reach a tenant's vault", method, perm)
			assert.Contains(t, err.Error(), "system tenant is not lifted",
				"the denial must name the self-strict reason: %v", err)
		})

		t.Run(name+"/system_tenant_own_vault_admitted", func(t *testing.T) {
			assert.NoError(t, authorizeVaultPermission(t, perm, tenant.SystemTenantID, tenant.SystemTenantID),
				"%s requires %s, which must admit the system tenant acting in its own vault", method, perm)
		})
	}
}

// TestUnaryAuthorizationInterceptor_SecretsVaultGrant drives the REAL gRPC
// interceptor for one write and one read verb: a token carrying the vault's
// secrets:* grant is admitted, and a token carrying the credentials:* grant the
// table used to require is NOT — the old grant does not survive as a second way
// in.
func TestUnaryAuthorizationInterceptor_SecretsVaultGrant(t *testing.T) {
	cases := []struct {
		name      string
		method    string
		perm      string
		staleGant string
	}{
		{"store", secretsManagementMethodPrefix + "StoreSecret", "secrets:write", "credentials:store"},
		{"get", secretsManagementMethodPrefix + "GetSecret", "secrets:read", "credentials:retrieve"},
		{"delete", secretsManagementMethodPrefix + "DeleteSecret", "secrets:delete", "credentials:delete"},
		{"list", secretsManagementMethodPrefix + "ListSecrets", "secrets:list", "credentials:list"},
	}

	for _, c := range cases {
		t.Run(c.name+"_admits_secrets_grant", func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: c.perm,
				authzCtx: &platformtoken.AuthorizedContext{
					Subject:  "u1",
					Username: "tenant-admin",
					TenantID: "automatethethings",
					Roles:    []string{"admin"},
					Scopes:   []string{c.perm},
				},
			}
			interceptor := UnaryAuthorizationInterceptor(authorizer)

			resp, err := interceptor(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: c.method},
				func(context.Context, interface{}) (interface{}, error) { return "ok", nil })
			require.NoError(t, err, c.method)
			assert.Equal(t, "ok", resp)
			assert.Equal(t, c.perm, authorizer.lastPerm, "required permission join for %s", c.method)
		})

		t.Run(c.name+"_denies_stale_credentials_grant", func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: c.staleGant,
				authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "automatethethings"},
			}
			interceptor := UnaryAuthorizationInterceptor(authorizer)

			_, err := interceptor(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: c.method},
				func(context.Context, interface{}) (interface{}, error) {
					t.Errorf("handler ran for %s on a %s token; the vault must require %s", c.method, c.staleGant, c.perm)
					return nil, nil
				})
			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err), "%s on %s", c.method, c.staleGant)
		})
	}
}
