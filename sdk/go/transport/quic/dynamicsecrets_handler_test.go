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

import (
	"encoding/json"
	"testing"
	"time"

	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
)

// TestDynamicSecretsOpcodeParity pins the SDK opcodes to the byte values the
// server dispatches on (pkg/api/quic/protocol.go OpDynamicSecrets*). A drift on
// either side silently routes a verb to an unrelated handler, so the values are
// asserted literally rather than cross-imported.
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

// TestDynamicSecretsOpcodesDisjoint proves the dynamic secrets opcodes collide
// with no other top-level opcode the SDK resolves. The 0x64-0x6B range sits
// between the auth verbs (0x60-0x63) and the KMS rotation-policy verbs
// (0x6C-0x6E), so a future allocation must not creep into it.
func TestDynamicSecretsOpcodesDisjoint(t *testing.T) {
	reserved := map[byte]string{
		OpDynamicSecretsCreateRole:  "quicbaas.DynamicSecrets/CreateRole",
		OpDynamicSecretsGetRole:     "quicbaas.DynamicSecrets/GetRole",
		OpDynamicSecretsListRoles:   "quicbaas.DynamicSecrets/ListRoles",
		OpDynamicSecretsDeleteRole:  "quicbaas.DynamicSecrets/DeleteRole",
		OpDynamicSecretsGenerate:    "quicbaas.DynamicSecrets/GenerateCredential",
		OpDynamicSecretsRenewLease:  "quicbaas.DynamicSecrets/RenewLease",
		OpDynamicSecretsRevokeLease: "quicbaas.DynamicSecrets/RevokeLease",
		OpDynamicSecretsListLeases:  "quicbaas.DynamicSecrets/ListLeases",
	}
	if len(reserved) != 8 {
		t.Fatalf("dynamic secrets opcodes are not distinct: got %d unique values, want 8", len(reserved))
	}

	for method, opcode := range methodToOpcode {
		owner, claimed := reserved[opcode]
		if !claimed {
			continue
		}
		if method != owner {
			t.Errorf("opcode 0x%02X is claimed by both %q and %q", opcode, owner, method)
		}
	}
}

// TestDynamicSecretsMethodResolution verifies every SDK verb resolves to its
// opcode under the fully qualified service name and round-trips back through
// the reverse map.
func TestDynamicSecretsMethodResolution(t *testing.T) {
	methods := []string{
		"CreateRole", "GetRole", "ListRoles", "DeleteRole",
		"GenerateCredential", "RenewLease", "RevokeLease", "ListLeases",
	}
	for _, method := range methods {
		key := "quicbaas.DynamicSecrets/" + method
		opcode, ok := methodToOpcode[key]
		if !ok {
			t.Errorf("%s has no opcode mapping", key)
			continue
		}
		if back := opcodeToMethod[opcode]; back != key {
			t.Errorf("opcode 0x%02X maps back to %q, want %q", opcode, back, key)
		}
	}
}

func TestIsDynamicSecretsService(t *testing.T) {
	tests := []struct {
		svc  string
		want bool
	}{
		{"quicbaas.DynamicSecrets", true},
		{"quicbaas.DynamicSecrets/CreateRole", true},
		{"DynamicSecrets", true},
		{"quicbaas.Secrets", false},
		{"quicbaas.KMS", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isDynamicSecretsService(tt.svc); got != tt.want {
			t.Errorf("isDynamicSecretsService(%q) = %v, want %v", tt.svc, got, tt.want)
		}
	}
}

// TestDecodeDynamicSecretsResponse proves the server's pkg/service wire shapes
// decode losslessly into the leaf SDK types the client type-asserts on.
func TestDecodeDynamicSecretsResponse(t *testing.T) {
	t.Run("CreateRole", func(t *testing.T) {
		payload := mustJSON(t, map[string]interface{}{
			"role": map[string]interface{}{
				"name":           "db-readonly",
				"generator_type": "postgres",
				"config":         map[string]string{"db": "app"},
				"default_ttl":    time.Hour,
				"max_ttl":        24 * time.Hour,
			},
			"created_at": time.Unix(1700000000, 0).UTC(),
		})

		got, err := decodeDynamicSecretsResponse("CreateRole", payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp, ok := got.(*dsclient.CreateRoleResponse)
		if !ok {
			t.Fatalf("got %T, want *dynamicsecrets.CreateRoleResponse", got)
		}
		if resp.Role == nil || resp.Role.Name != "db-readonly" {
			t.Fatalf("role not decoded: %+v", resp.Role)
		}
		if resp.Role.DefaultTTL != time.Hour {
			t.Errorf("DefaultTTL = %v, want 1h", resp.Role.DefaultTTL)
		}
		if resp.Role.MaxTTL != 24*time.Hour {
			t.Errorf("MaxTTL = %v, want 24h", resp.Role.MaxTTL)
		}
		if resp.CreatedAt.Unix() != 1700000000 {
			t.Errorf("CreatedAt = %v", resp.CreatedAt)
		}
	})

	t.Run("GenerateCredential", func(t *testing.T) {
		payload := mustJSON(t, map[string]interface{}{
			"credential": map[string]interface{}{
				"credential_id": "cred-1",
				"type":          "database",
				"data":          map[string]string{"username": "u", "password": "p"},
				"lease_id":      "lease-1",
			},
			"lease": map[string]interface{}{
				"lease_id":    "lease-1",
				"secret_path": "postgres/creds/db-readonly",
				"ttl":         2 * time.Hour,
				"renew_count": 3,
				"renewable":   true,
			},
			"lease_id": "lease-1",
		})

		got, err := decodeDynamicSecretsResponse("GenerateCredential", payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp, ok := got.(*dsclient.GenerateCredentialResponse)
		if !ok {
			t.Fatalf("got %T, want *dynamicsecrets.GenerateCredentialResponse", got)
		}
		if resp.Credential == nil || resp.Credential.CredentialID != "cred-1" {
			t.Fatalf("credential not decoded: %+v", resp.Credential)
		}
		if resp.Credential.Data["password"] != "p" {
			t.Errorf("credential data lost: %+v", resp.Credential.Data)
		}
		if resp.Lease == nil || resp.Lease.SecretPath != "postgres/creds/db-readonly" {
			t.Fatalf("lease not decoded: %+v", resp.Lease)
		}
		if resp.Lease.RenewCount != 3 || !resp.Lease.Renewable {
			t.Errorf("lease renewal fields lost: %+v", resp.Lease)
		}
		if resp.LeaseID != "lease-1" {
			t.Errorf("LeaseID = %q", resp.LeaseID)
		}
	})

	t.Run("ListLeases", func(t *testing.T) {
		payload := mustJSON(t, map[string]interface{}{
			"leases": []map[string]interface{}{
				{"lease_id": "a", "ttl": time.Minute, "metadata": map[string]string{"k": "v"}},
				{"lease_id": "b", "ttl": 2 * time.Minute},
			},
			"count": 2,
		})

		got, err := decodeDynamicSecretsResponse("ListLeases", payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp := got.(*dsclient.ListLeasesResponse)
		if resp.Count != 2 || len(resp.Leases) != 2 {
			t.Fatalf("got count=%d leases=%d", resp.Count, len(resp.Leases))
		}
		if resp.Leases[0].Metadata["k"] != "v" {
			t.Errorf("metadata lost: %+v", resp.Leases[0].Metadata)
		}
	})

	t.Run("empty payload yields nil", func(t *testing.T) {
		got, err := decodeDynamicSecretsResponse("ListRoles", nil)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("unknown method falls back to a map", func(t *testing.T) {
		got, err := decodeDynamicSecretsResponse("NotAVerb", []byte(`{"a":1}`))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := got.(map[string]interface{}); !ok {
			t.Errorf("got %T, want map[string]interface{}", got)
		}
	})

	t.Run("malformed payload errors", func(t *testing.T) {
		if _, err := decodeDynamicSecretsResponse("GetRole", []byte("{not json")); err == nil {
			t.Fatal("expected a decode error")
		}
	})
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
