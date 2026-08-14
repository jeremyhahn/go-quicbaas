//go:build quic

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

package quic

import "testing"

// TestDynamicSecretsOpcodeParity pins the server opcodes to the byte values the
// SDK sends (sdk/go/transport/quic/protocol.go OpDynamicSecrets*). The SDK-side
// test of the same name asserts the identical literals, so the two files
// together form the byte-parity contract for this range.
func TestDynamicSecretsOpcodeParity(t *testing.T) {
	want := map[string]byte{
		"CreateRole":  0x64,
		"GetRole":     0x65,
		"ListRoles":   0x66,
		"DeleteRole":  0x67,
		"Generate":    0x68,
		"RenewLease":  0x69,
		"RevokeLease": 0x6A,
		"ListLeases":  0x6B,
	}
	got := map[string]byte{
		"CreateRole":  OpDynamicSecretsCreateRole,
		"GetRole":     OpDynamicSecretsGetRole,
		"ListRoles":   OpDynamicSecretsListRoles,
		"DeleteRole":  OpDynamicSecretsDeleteRole,
		"Generate":    OpDynamicSecretsGenerate,
		"RenewLease":  OpDynamicSecretsRenewLease,
		"RevokeLease": OpDynamicSecretsRevokeLease,
		"ListLeases":  OpDynamicSecretsListLeases,
	}
	for name, expected := range want {
		if got[name] != expected {
			t.Errorf("opcode %s = 0x%02X, want 0x%02X", name, got[name], expected)
		}
	}
}

// TestDynamicSecretsOpcodesAreTopLevel proves the dynamic secrets verbs occupy
// their own top-level opcodes rather than riding the shared OpOrchestrator
// sub-op space, and that nothing else claims a byte in 0x64-0x6B.
func TestDynamicSecretsOpcodesAreTopLevel(t *testing.T) {
	claimed := map[byte]string{
		OpDynamicSecretsCreateRole:  "DynamicSecretsCreateRole",
		OpDynamicSecretsGetRole:     "DynamicSecretsGetRole",
		OpDynamicSecretsListRoles:   "DynamicSecretsListRoles",
		OpDynamicSecretsDeleteRole:  "DynamicSecretsDeleteRole",
		OpDynamicSecretsGenerate:    "DynamicSecretsGenerate",
		OpDynamicSecretsRenewLease:  "DynamicSecretsRenewLease",
		OpDynamicSecretsRevokeLease: "DynamicSecretsRevokeLease",
		OpDynamicSecretsListLeases:  "DynamicSecretsListLeases",
	}
	if len(claimed) != 8 {
		t.Fatalf("dynamic secrets opcodes collide: %d unique values, want 8", len(claimed))
	}

	for opcode, want := range claimed {
		if opcode == OpOrchestrator {
			t.Errorf("%s aliases the OpOrchestrator dispatch byte", want)
		}
		if got := opcodeNames[opcode]; got != want {
			t.Errorf("opcode 0x%02X named %q, want %q", opcode, got, want)
		}
	}
}

// TestDynamicSecretsOpcodesMapped asserts every dynamic secrets opcode carries
// an authorization tuple. The QUIC PEP is default-deny, so an unmapped opcode
// is unreachable rather than unprotected — either way it is a wiring bug.
func TestDynamicSecretsOpcodesMapped(t *testing.T) {
	want := map[byte][2]string{
		OpDynamicSecretsCreateRole:  {"dynamic_secrets", "role_create"},
		OpDynamicSecretsGetRole:     {"dynamic_secrets", "role_read"},
		OpDynamicSecretsListRoles:   {"dynamic_secrets", "role_list"},
		OpDynamicSecretsDeleteRole:  {"dynamic_secrets", "role_delete"},
		OpDynamicSecretsGenerate:    {"dynamic_secrets", "generate"},
		OpDynamicSecretsRenewLease:  {"dynamic_secrets", "lease_renew"},
		OpDynamicSecretsRevokeLease: {"dynamic_secrets", "lease_revoke"},
		OpDynamicSecretsListLeases:  {"dynamic_secrets", "lease_list"},
	}
	for opcode, expected := range want {
		got, ok := quicOpcodePermissions[opcode]
		if !ok {
			t.Errorf("opcode 0x%02X has no qrdb permission mapping", opcode)
			continue
		}
		if got.resource != expected[0] || got.action != expected[1] {
			t.Errorf("opcode 0x%02X maps to %s:%s, want %s:%s",
				opcode, got.resource, got.action, expected[0], expected[1])
		}
	}
}
