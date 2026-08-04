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

// TestMapToToolName_KMSRegisteredNames pins every KMS admin method onto the
// tool name the MCP server actually registers (several elide the "Key"
// segment) — a mismatch here makes the verb silently unreachable over MCP.
func TestMapToToolName_KMSRegisteredNames(t *testing.T) {
	tr := &Transport{}
	cases := map[string]string{
		"GenerateKey":             "quicbaas_kms_generate_key",
		"GetKeyMetadata":          "quicbaas_kms_get_metadata",
		"UpdateKeyMetadata":       "quicbaas_kms_update_metadata",
		"SetKeyPolicy":            "quicbaas_kms_set_policy",
		"GetKeyPolicy":            "quicbaas_kms_get_policy",
		"SetKeyRotationPolicy":    "quicbaas_kms_set_rotation_policy",
		"GetKeyRotationPolicy":    "quicbaas_kms_get_rotation_policy",
		"DeleteKeyRotationPolicy": "quicbaas_kms_delete_rotation_policy",
		"RevokeKey":               "quicbaas_kms_revoke_key",
		"GetPublicKey":            "quicbaas_kms_get_public_key",
		"GetSupportedAlgorithms":  "quicbaas_kms_get_supported_algorithms",
	}
	for method, want := range cases {
		if got := tr.mapToToolName("quicbaas.KMS", method); got != want {
			t.Errorf("mapToToolName(KMS, %s) = %q, want %q", method, got, want)
		}
	}
}

// TestMapToToolName_KMSUnknownFallsBack verifies unmapped methods still get
// the generic derivation.
func TestMapToToolName_KMSUnknownFallsBack(t *testing.T) {
	tr := &Transport{}
	if got := tr.mapToToolName("quicbaas.KMS", "SomeFutureVerb"); got != "quicbaas_kms_some_future_verb" {
		t.Errorf("fallback = %q", got)
	}
}
