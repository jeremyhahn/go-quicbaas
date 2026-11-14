//go:build rest

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

package rest

import (
	"strings"
	"testing"
)

// tenantUsersPrefix is the URL subtree of the routes that operate on one
// tenant's users: the users themselves, their MFA, GDPR, account links,
// sessions, WebAuthn credentials and role assignments.
const tenantUsersPrefix = "/api/v1/tenants/{tenant_id}/users/"

// TestTenantUserRoutesNeverTakeATenantPermission asserts a policy property of
// the frozen table, not its agreement with a derivation (go-quicbaas#341).
//
// The table once took every route's resource from the first path segment, so
// every route under /tenants/{tenant_id}/users/ inherited "tenants": anyone
// holding tenants:create could issue MFA backup codes and grant roles, and
// tenants:delete reached GDPR deletion and MFA removal. A route in the users
// subtree operates on a user, so it takes a users:* or roles:* grant. Any
// route added there later is held to the same rule.
func TestTenantUserRoutesNeverTakeATenantPermission(t *testing.T) {
	seen := 0
	for key, perm := range frozenRoutePermissions {
		_, pattern, ok := strings.Cut(key, " ")
		if !ok || !strings.HasPrefix(pattern, tenantUsersPrefix) {
			continue
		}
		seen++
		if perm.auth != RouteAuthPermission {
			t.Errorf("route %q is not permission-gated (auth %v)", key, perm.auth)
			continue
		}
		if perm.resource != "users" && perm.resource != "roles" {
			t.Errorf("route %q operates on a user but is gated %s:%s; "+
				"a tenant permission must not reach a user's data or credentials",
				key, perm.resource, perm.action)
		}
	}
	if seen == 0 {
		t.Fatalf("no frozen route under %s; the test asserts nothing", tenantUsersPrefix)
	}
}

// TestTenantUserPermissionsMatchOtherTransports pins each user-subtree route
// to the grant the same verb carries on MCP (pkg/api/mcp/authz_qrdb.go
// quicbaas_iam_*) and gRPC (pkg/api/grpc/authz_mapping.go), so the transports
// cannot disagree about who may do what (go-quicbaas#341).
func TestTenantUserPermissionsMatchOtherTransports(t *testing.T) {
	const user = tenantUsersPrefix + "{user_id}"
	perm := func(resource, action string) frozenPermission {
		return frozenPermission{auth: RouteAuthPermission, resource: resource, action: action}
	}

	want := map[string]frozenPermission{
		// quicbaas_iam_user_create / _list / _get / _update / _delete.
		"POST " + tenantUsersPrefix: perm("users", "create"),
		"GET " + tenantUsersPrefix:  perm("users", "list"),
		"GET " + user:               perm("users", "read"),
		"PUT " + user:               perm("users", "update"),
		"DELETE " + user:            perm("users", "delete"),

		// quicbaas_iam_user_assign_role / _revoke_role.
		"POST " + user + "/roles":          perm("roles", "assign"),
		"DELETE " + user + "/roles/{role}": perm("roles", "unassign"),

		// quicbaas_iam_session_* (users:read / users:update).
		"GET " + user + "/sessions/":    perm("users", "read"),
		"POST " + user + "/sessions/":   perm("users", "update"),
		"DELETE " + user + "/sessions/": perm("users", "update"),

		// FederationService LinkAccount / GetUserLinks / UnlinkAccount.
		"POST " + user + "/links/":                perm("users", "update"),
		"GET " + user + "/links/":                 perm("users", "read"),
		"DELETE " + user + "/links/{provider_id}": perm("users", "update"),

		// MFA: reading a user's factors is users:read; enrolling, verifying
		// and issuing backup codes change the user's credentials
		// (users:update); removing a factor is a credential deletion, as
		// WebAuthn credential deletion is on every transport (users:delete).
		"GET " + user + "/mfa/":                          perm("users", "read"),
		"GET " + user + "/mfa/backup-codes":              perm("users", "read"),
		"POST " + user + "/mfa/backup-codes":             perm("users", "update"),
		"POST " + user + "/mfa/enroll":                   perm("users", "update"),
		"POST " + user + "/mfa/verify":                   perm("users", "update"),
		"POST " + user + "/mfa/challenge":                perm("users", "update"),
		"POST " + user + "/mfa/verify-challenge":         perm("users", "update"),
		"DELETE " + user + "/mfa/":                       perm("users", "delete"),
		"DELETE " + user + "/mfa/enroll/{enrollment_id}": perm("users", "delete"),

		// GDPR: an export reads the user's data; deletion (and cancelling
		// one) is users:delete; consent records are user updates.
		"POST " + user + "/gdpr/export":                     perm("users", "read"),
		"GET " + user + "/gdpr/export/{export_id}":          perm("users", "read"),
		"GET " + user + "/gdpr/export/{export_id}/download": perm("users", "read"),
		"POST " + user + "/gdpr/deletion":                   perm("users", "delete"),
		"GET " + user + "/gdpr/deletion/{deletion_id}":      perm("users", "read"),
		"DELETE " + user + "/gdpr/deletion/{deletion_id}":   perm("users", "delete"),
		"POST " + user + "/gdpr/consent":                    perm("users", "update"),
		"GET " + user + "/gdpr/consents":                    perm("users", "read"),
		"GET " + user + "/gdpr/consent/{purpose}":           perm("users", "read"),
		"DELETE " + user + "/gdpr/consent/{purpose}":        perm("users", "update"),
		"PUT " + user + "/gdpr/consent/{purpose}/withdraw":  perm("users", "update"),
	}

	for key, expected := range want {
		got, ok := frozenRoutePermissions[key]
		if !ok {
			t.Errorf("route %q has no frozen permission entry", key)
			continue
		}
		if got != expected {
			t.Errorf("route %q is gated %s:%s, want %s:%s (the grant the same verb "+
				"carries on MCP and gRPC)", key, got.resource, got.action,
				expected.resource, expected.action)
		}
	}
}
