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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
)

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
// idempotent, so calling it here is free and keeps the assertion honest: the
// guard passes only when the permission is genuinely in the registry, never
// because the test special-cased it.
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

// TestGRPCPermissions_EveryMappingIsRegistered asserts every permission
// grpcMethodPermissions requires actually exists in the permission registry.
//
// It is the gRPC half of the dead-endpoint guard whose MCP twin is
// TestMCPPermissions_EveryMappingIsRegistered. Mapping a method onto an
// unregistered permission is strictly worse than leaving it unmapped: go-qrdb's
// role store rejects an unregistered permission on role create/update
// (pkg/role.validatePermissions -> InvalidPermissionError), so NO role can ever
// hold the grant and the method is permanently 403 under RequireAuthz — yet the
// mapping table makes it look wired, so it survives review.
//
// That is exactly how attestation:verify / attestation:attest sat in this
// table, in the QUIC sub-op table, in the REST PEP and in the MCP tool map
// while being unreachable on all four.
func TestGRPCPermissions_EveryMappingIsRegistered(t *testing.T) {
	t.Parallel()

	registered := registeredPermissionSet(t)

	for method, perm := range grpcMethodPermissions {
		_, ok := registered[perm.resource+":"+perm.action]
		assert.Truef(t, ok,
			"gRPC method %q maps to %s:%s, which neither go-qrdb nor pkg/permission "+
				"registers; no role can hold that grant, so the method is unreachable "+
				"despite looking wired. Register the permission in pkg/permission or fix "+
				"the mapping.",
			method, perm.resource, perm.action)
	}
}

// TestGRPCPermissions_AttestationFamilyIsRegistered pins the attestation family
// specifically. The generic guard above would also catch a regression here, but
// only while the mapping still references it: deleting the AttestationService
// entries would silence the generic guard and leave the surface unreachable for
// a different reason. This test fails if either verb stops being registered,
// independently of the mapping table.
func TestGRPCPermissions_AttestationFamilyIsRegistered(t *testing.T) {
	t.Parallel()

	registered := registeredPermissionSet(t)

	for _, perm := range []qrdbsdk.Permission{
		quicbaaspermission.AttestationVerify,
		quicbaaspermission.AttestationAttest,
	} {
		_, ok := registered[string(perm)]
		assert.Truef(t, ok, "permission %q is not registered", perm)
	}
}
