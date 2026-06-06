// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMethodToOpcode_SecurityServices asserts the DEK, Secrets and Crypto
// service/method pairs resolve to the opcodes the QUIC server actually
// dispatches on (pkg/api/quic/protocol.go). A mismatch here would route the
// request to the wrong server handler, so these are wire-contract guards.
func TestMethodToOpcode_SecurityServices(t *testing.T) {
	cases := []struct {
		service string
		method  string
		opcode  byte
	}{
		// DEK (server OpKMSDEK* range).
		{"quicbaas.DEK", "CreateDEK", 0xD8},
		{"quicbaas.DEK", "GetDEK", 0xD9},
		{"quicbaas.DEK", "ListDEKs", 0xDA},
		{"quicbaas.DEK", "RotateDEK", 0xDB},
		{"quicbaas.DEK", "DeleteDEK", 0xDC},
		{"quicbaas.DEK", "GetDEKStatus", 0xDD},
		{"quicbaas.DEK", "SetDefaultDEK", 0xDE},
		{"quicbaas.DEK", "SetDEKSafety", 0xDF},
		{"quicbaas.DEK", "ClearDEKSafety", 0xE7},
		{"quicbaas.DEK", "StartReencryption", 0x07},
		{"quicbaas.DEK", "GetReencryptionProgress", 0x08},

		// Secrets (server OpSecrets* range).
		{"quicbaas.Secrets", "Store", 0xE0},
		{"quicbaas.Secrets", "Get", 0xE1},
		{"quicbaas.Secrets", "List", 0xE2},
		{"quicbaas.Secrets", "Delete", 0xE3},
		{"quicbaas.Secrets", "ListVersions", 0xE4},
		{"quicbaas.Secrets", "Rollback", 0xE5},
		{"quicbaas.Secrets", "Rotate", 0xE6},
		{"quicbaas.Secrets", "GetVersion", 0xEC},
		{"quicbaas.Secrets", "SetRotationPolicy", 0xED},
		{"quicbaas.Secrets", "GetRotationPolicy", 0xEE},
		{"quicbaas.Secrets", "UpdateStatus", 0xEF},

		// Crypto / transit (server OpCrypto* range).
		{"quicbaas.Crypto", "Encrypt", 0xF0},
		{"quicbaas.Crypto", "Decrypt", 0xF1},
		{"quicbaas.Crypto", "Rewrap", 0xF2},
		{"quicbaas.Crypto", "Sign", 0xF3},
		{"quicbaas.Crypto", "Verify", 0xF4},
		{"quicbaas.Crypto", "Hash", 0xF5},
		{"quicbaas.Crypto", "GenerateRandom", 0xF6},
		{"quicbaas.Crypto", "GenerateDataKey", 0xF7},
		{"quicbaas.Crypto", "GenerateHMAC", 0xF8},
		{"quicbaas.Crypto", "VerifyHMAC", 0xF9},

		// KMS pairs the client actually issues over QUIC.
		{"quicbaas.KMS", "GenerateKey", 0xD0},
		{"quicbaas.KMS", "GetKey", 0xD2},
		{"quicbaas.KMS", "ListKeys", 0xD3},
		{"quicbaas.KMS", "ListKeyVersions", 0xEA},
	}

	for _, tc := range cases {
		t.Run(tc.service+"/"+tc.method, func(t *testing.T) {
			op, ok := MethodToOpcode(tc.service, tc.method)
			require.True(t, ok, "no opcode for %s/%s", tc.service, tc.method)
			assert.Equalf(t, tc.opcode, op, "%s/%s opcode", tc.service, tc.method)
		})
	}
}
