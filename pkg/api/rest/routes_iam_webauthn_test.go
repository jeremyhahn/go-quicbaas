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

import "testing"

// TestWebAuthnCredentialPermissionsMatchOtherTransports pins the grants on the
// WebAuthn credential routes to the ones the same verbs carry on the other
// transports that serve them.
//
// WHY THIS TEST EXISTS. These three verbs are reachable over REST, gRPC (and
// the Unix socket that delegates to it), the embedded transport and MCP. gRPC
// enforces the (qrdb.v1.permission) annotation on the RPC; MCP enforces
// qrdbToolExactMapping. If REST's frozen table drifts from those, the same
// principal calling the same verb is admitted on one transport and denied on
// another — an authorization outcome that depends on which wire the call took.
// That is not a policy anyone can reason about, and it is invisible until a
// real operator hits it mid-incident, which is exactly when this surface is
// used.
//
// The expected values are duplicated here as literals ON PURPOSE, and each of
// the other two planes pins itself to the same literals in its own package —
// none of the three tables is exported to the others, and importing one would
// only make this test agree with whatever that package currently says. A
// literal on each side forces a human to reconcile all three deliberately, and
// drift in any one plane fails that plane's own test:
//
//	gRPC: pkg/api/grpc/authz_webauthn_parity_test.go
//	      (plus TestGRPCPermissions_ProtoOptionMatchesAuthzMapping, which ties
//	      grpcMethodPermissions to the (qrdb.v1.permission) annotation in
//	      api/proto/iam/iam.proto)
//	MCP:  pkg/api/mcp/authz_webauthn_parity_test.go
func TestWebAuthnCredentialPermissionsMatchOtherTransports(t *testing.T) {
	const credentials = "/api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials"

	want := map[string]frozenPermission{
		"GET " + credentials + "/": {
			auth: RouteAuthPermission, resource: "users", action: "read",
		},
		"DELETE " + credentials + "/": {
			auth: RouteAuthPermission, resource: "users", action: "delete",
		},
		"DELETE " + credentials + "/{credential_id}": {
			auth: RouteAuthPermission, resource: "users", action: "delete",
		},
	}

	for key, expected := range want {
		got, ok := frozenRoutePermissions[key]
		if !ok {
			t.Errorf("route %q has no frozen permission entry", key)
			continue
		}
		if got != expected {
			t.Errorf("route %q is gated %s:%s, but the same verb is %s:%s on gRPC and MCP; "+
				"the grant must not depend on which transport carried the call",
				key, got.resource, got.action, expected.resource, expected.action)
		}
	}
}

// TestWebAuthnCredentialRoutesAreNotIdentityGated keeps these verbs on an
// explicit permission rather than the identity-gated default. They administer
// ANOTHER user's authenticators; a caller holding only its own identity must
// not reach them.
func TestWebAuthnCredentialRoutesAreNotIdentityGated(t *testing.T) {
	for key, perm := range frozenRoutePermissions {
		if !containsWebAuthnCredentials(key) {
			continue
		}
		if perm.auth != RouteAuthPermission {
			t.Errorf("route %q is %v, want RouteAuthPermission: it administers another user's authenticators",
				key, perm.auth)
		}
		if perm.resource == "" || perm.action == "" {
			t.Errorf("route %q carries no resource:action", key)
		}
	}
}

// containsWebAuthnCredentials reports whether a frozen-table key addresses the
// credential administration surface.
func containsWebAuthnCredentials(key string) bool {
	const marker = "/webauthn/credentials"
	for i := 0; i+len(marker) <= len(key); i++ {
		if key[i:i+len(marker)] == marker {
			return true
		}
	}
	return false
}
