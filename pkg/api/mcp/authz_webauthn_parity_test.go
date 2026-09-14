//go:build mcp

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

import "testing"

// TestWebAuthnCredentialMCPPermissions pins the MCP gate on the WebAuthn
// credential tools to the same grants the other transports require.
//
// These three verbs are served on four transports, each with its own gate: the
// frozen route table for REST, grpcMethodPermissions plus the
// (qrdb.v1.permission) annotation for gRPC and the Unix socket, and
// qrdbToolExactMapping here. If they drift, the same principal calling the same
// verb is admitted on one transport and denied on another — an authorization
// outcome that depends on which wire the call took.
//
// None of the three tables is exported to the others, so each plane pins itself
// to the same literals in its own package:
//
//	REST: pkg/api/rest/routes_iam_webauthn_test.go
//	gRPC: pkg/api/grpc/authz_webauthn_parity_test.go
//
// Changing one means changing all three deliberately.
func TestWebAuthnCredentialMCPPermissions(t *testing.T) {
	want := map[string]qrdbToolResource{
		"quicbaas_iam_webauthn_credentials_list":       {"users", "read"},
		"quicbaas_iam_webauthn_credential_delete":      {"users", "delete"},
		"quicbaas_iam_webauthn_credentials_delete_all": {"users", "delete"},
	}

	for tool, expected := range want {
		got, ok := qrdbToolExactMapping[tool]
		if !ok {
			t.Errorf("tool %q has no permission mapping: the authz gate is default-deny, "+
				"so it would be invisible in tools/list", tool)
			continue
		}
		if got != expected {
			t.Errorf("tool %q is gated %v, but REST and gRPC gate the same verb %v; "+
				"the grant must not depend on which transport carried the call",
				tool, got, expected)
		}
	}
}
