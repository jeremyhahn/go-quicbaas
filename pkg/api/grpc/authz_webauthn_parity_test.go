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

import "testing"

// webAuthnCredentialGrants is the ONE canonical answer to "what does a caller
// need to administer another user's WebAuthn authenticators".
//
// These three verbs are served on four transports, each with its own gate:
// this Go map and the (qrdb.v1.permission) annotation on the RPC for gRPC (and
// the Unix socket that delegates to it), the frozen route table for REST, and
// qrdbToolExactMapping for MCP. If they drift, the same principal calling the
// same verb is admitted on one transport and denied on another — an
// authorization outcome that depends on which wire the call took, invisible
// until an operator hits it mid-incident, which is exactly when this surface
// is used.
//
// Each plane pins itself to these values in its own package, since none of the
// three tables is exported to the others:
//
//	REST: pkg/api/rest/routes_iam_webauthn_test.go
//	MCP:  pkg/api/mcp/authz_webauthn_parity_test.go
//	gRPC: this file, plus TestGRPCPermissions_ProtoOptionMatchesAuthzMapping
//	      which proves the proto annotation agrees with the map below.
//
// Changing one means changing all three deliberately.
var webAuthnCredentialGrants = map[string]qrdbToolResourceLike{
	"/quicbaas.iam.v1.IAMService/ListWebAuthnCredentials":   {"users", "read"},
	"/quicbaas.iam.v1.IAMService/DeleteWebAuthnCredential":  {"users", "delete"},
	"/quicbaas.iam.v1.IAMService/DeleteWebAuthnCredentials": {"users", "delete"},
}

// qrdbToolResourceLike mirrors the resource/action pair the mapping stores,
// declared locally so the expectation does not read from the table it checks.
type qrdbToolResourceLike struct {
	resource string
	action   string
}

// TestWebAuthnCredentialGRPCPermissions pins the gRPC gate to the canonical
// grants. TestGRPCPermissions_ProtoOptionMatchesAuthzMapping separately proves
// the proto annotation matches this map, so the two gRPC enforcement points
// cannot drift from each other either.
func TestWebAuthnCredentialGRPCPermissions(t *testing.T) {
	for method, want := range webAuthnCredentialGrants {
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
}
