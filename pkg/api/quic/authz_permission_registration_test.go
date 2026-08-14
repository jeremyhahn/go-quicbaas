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

import (
	"fmt"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
)

// quicPermissionTables is the complete set of opcode/sub-op tables the QUIC
// authz gate resolves through getQRDBPermission. Every table a future surface
// adds MUST be listed here, or its permissions escape the registration guard.
func quicPermissionTables() map[string]map[byte]qrdbPermission {
	return map[string]map[byte]qrdbPermission{
		"quicOpcodePermissions":       quicOpcodePermissions,
		"barrierSubOpPermissions":     barrierSubOpPermissions,
		"ceremonySubOpPermissions":    ceremonySubOpPermissions,
		"accountSubOpPermissions":     accountSubOpPermissions,
		"rotateSubOpPermissions":      rotateSubOpPermissions,
		"projectSubOpPermissions":     projectSubOpPermissions,
		"attestationSubOpPermissions": attestationSubOpPermissions,
		"sshSubOpPermissions":         sshSubOpPermissions,
		"certExtSubOpPermissions":     certExtSubOpPermissions,
	}
}

// registeredPermissionSet returns every permission the qrdb policy decision
// point can resolve, from BOTH contributors to the single registry:
//
//   - go-qrdb's own families, registered by init() in go-qrdb/pkg/permission;
//   - the broker-domain families QuicBaaS contributes through
//     qrdbsdk.RegisterPermissions (pkg/permission), which production performs
//     once at the composition root in server.NewServer.
//
// The helper calls quicbaaspermission.Register itself rather than importing
// pkg/server (which imports this package — an import cycle). Register is
// idempotent, so calling it here is free and keeps the assertion honest.
func registeredPermissionSet(t *testing.T) map[string]struct{} {
	t.Helper()

	quicbaaspermission.Register()

	registered := make(map[string]struct{})
	for _, perm := range qrdbsdk.AllPermissions() {
		registered[string(perm)] = struct{}{}
	}
	require.NotEmpty(t, registered, "qrdb permission registry is empty")
	return registered
}

// TestQUICPermissions_EveryMappingIsRegistered asserts every permission the
// QUIC opcode and sub-op tables require actually exists in the permission
// registry.
//
// It is the QUIC half of the dead-endpoint guard whose MCP twin is
// TestMCPPermissions_EveryMappingIsRegistered. Mapping an opcode onto an
// unregistered permission is strictly worse than leaving it unmapped: go-qrdb's
// role store rejects an unregistered permission on role create/update
// (pkg/role.validatePermissions -> InvalidPermissionError), so NO role can ever
// hold the grant and the opcode is permanently denied under RequireAuthz — yet
// the table makes it look wired, so it survives review.
func TestQUICPermissions_EveryMappingIsRegistered(t *testing.T) {
	t.Parallel()

	registered := registeredPermissionSet(t)

	for name, table := range quicPermissionTables() {
		for opcode, perm := range table {
			_, ok := registered[perm.resource+":"+perm.action]
			assert.Truef(t, ok,
				"%s[0x%02x] maps to %s:%s, which neither go-qrdb nor pkg/permission "+
					"registers; no role can hold that grant, so the opcode is unreachable "+
					"despite looking wired. Register the permission in pkg/permission or "+
					"fix the mapping.",
				name, opcode, perm.resource, perm.action)
		}
	}
}

// TestQUICPermissions_IAMOpcodesAreRegistered covers the IAM opcodes, which
// resolve through a switch (getIAMQRDBPermission) rather than a table and so
// are invisible to the table sweep above. The whole opcode space is probed so a
// newly handled opcode cannot slip past.
func TestQUICPermissions_IAMOpcodesAreRegistered(t *testing.T) {
	t.Parallel()

	registered := registeredPermissionSet(t)

	handled := 0
	for op := 0; op <= 0xFF; op++ {
		resource, action, ok := getIAMQRDBPermission(byte(op))
		if !ok {
			continue
		}
		handled++
		_, found := registered[resource+":"+action]
		assert.Truef(t, found,
			"IAM opcode 0x%02x maps to %s:%s, which neither go-qrdb nor pkg/permission "+
				"registers; no role can hold that grant, so the opcode is unreachable "+
				"despite looking wired.",
			op, resource, action)
	}
	require.NotZero(t, handled, "getIAMQRDBPermission handled no opcode; the sweep is vacuous")
}

// TestQUICPermissions_AttestationSubOpsResolve pins the attestation sub-ops
// end to end: each resolves through the live dispatcher (getQRDBPermission with
// the sub-op in payload[0], the shape the handler sees) onto a permission that
// is registered. The generic table sweep proves registration; this proves the
// dispatch path actually reaches the table.
func TestQUICPermissions_AttestationSubOpsResolve(t *testing.T) {
	t.Parallel()

	registered := registeredPermissionSet(t)

	cases := []struct {
		subOp byte
		want  string
	}{
		{SubOpAttestationVerify, string(quicbaaspermission.AttestationVerify)},
		{SubOpAttestationVerifyChain, string(quicbaaspermission.AttestationVerify)},
		{SubOpAttestationAttestContainer, string(quicbaaspermission.AttestationAttest)},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("0x%02x", tc.subOp), func(t *testing.T) {
			resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{tc.subOp})
			require.Truef(t, ok, "sub-op 0x%02x resolved no permission", tc.subOp)
			assert.Equal(t, tc.want, resource+":"+action)
			_, found := registered[resource+":"+action]
			assert.Truef(t, found, "permission %s:%s is not registered", resource, action)
		})
	}
}

// TestQUICPermissions_NonAttestationSubOpKeepsOrchestratorPermission is the
// boundary twin: a sub-op just outside the attestation range (0x70-0x72) must
// NOT acquire an attestation permission. Widening isAttestationSubOp by one
// would silently route an orchestrator verb through the host-TPM grant, or an
// attestation verb through the much broader orchestrator surface permission.
func TestQUICPermissions_NonAttestationSubOpKeepsOrchestratorPermission(t *testing.T) {
	t.Parallel()

	for _, subOp := range []byte{SubOpAttestationVerify - 1, SubOpAttestationAttestContainer + 1} {
		t.Run(fmt.Sprintf("0x%02x", subOp), func(t *testing.T) {
			assert.False(t, isAttestationSubOp(subOp), "sub-op is inside the attestation range")

			resource, _, ok := getQRDBPermission(OpOrchestrator, []byte{subOp})
			if !ok {
				// Unmapped is a safe outcome under default-deny.
				return
			}
			assert.NotEqual(t, "attestation", resource,
				"non-attestation sub-op 0x%02x resolved an attestation permission", subOp)
		})
	}
}
