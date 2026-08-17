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

package apimodule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestNewModule_RegistersRoleGrants proves module construction contributes
// the module's gRPC-declared permission set into qrdb's registries, and does
// so WITHOUT overriding go-qrdb's own FIPS 140-3 AS10.03-curated built-in
// role placements (see module_grants.go's doc comment for the full
// rationale): a permission some built-in role already holds is left exactly
// as qrdb placed it, and only a permission held by NO built-in role is
// filled in for Admin/Owner.
func TestNewModule_RegistersRoleGrants(t *testing.T) {
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	// keys:sign is declared by ssh.proto (this module's SSHService) but is
	// deliberately reserved for Operator/User in go-qrdb's built-in grants
	// (registerAdminGrants withholds it): the module must not widen Admin.
	assert.True(t, qrdbsdk.IsPermissionRegistered("keys:sign"),
		"keys:sign must be registered so role creation never fails on it")
	assert.False(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleAdmin, "keys:sign"),
		"keys:sign must stay withheld from Admin; go-qrdb reserves it for Operator/User")
	assert.True(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleOperator, "keys:sign"),
		"keys:sign must still be reachable via the Operator role qrdb granted it to")

	// credentials:store is declared by account.proto (this module's
	// AccountService) and go-qrdb v0.5.1-alpha places it on NO built-in
	// role: a genuine gap the module must fill for Admin/Owner so its own
	// AccountService is reachable by the built-in administrator.
	for _, perm := range []qrdbsdk.Permission{"credentials:store", "credentials:retrieve"} {
		assert.True(t, qrdbsdk.IsPermissionRegistered(perm), "%s must be registered", perm)
		assert.True(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleAdmin, perm),
			"admin must hold %s (unclaimed gap fill)", perm)
		assert.True(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.RoleNameOwner, perm),
			"owner must hold %s (unclaimed gap fill)", perm)
	}

	// The SoD invariant: the Security Officer role receives nothing from
	// this contribution, gap-fill included.
	assert.False(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleSO, "credentials:store"))
	assert.False(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleSO, "keys:sign"))
}

// TestNewModule_RegistersRoleGrants_NewBrokerDataPlaneServices proves
// registerRoleGrants's contributeGRPCPermissions pass — which iterates
// grpcRegistrations generically by resolving each entry's proto
// FileDescriptor rather than importing one File_..._proto variable per
// service (module_grants.go's doc comment) — picks up the permission
// vocabulary declared by the five broker data-plane services this module
// newly registers (parameter_store.proto, dynamic_secrets.proto,
// objectstorage.proto, ollama.proto) without requiring any edit to
// module_grants.go itself.
func TestNewModule_RegistersRoleGrants_NewBrokerDataPlaneServices(t *testing.T) {
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	for _, perm := range []qrdbsdk.Permission{
		"parameters:read",
		"parameters:write",
		"dynamic_secrets:generate",
		"objstore:put",
		"ollama:embed",
	} {
		assert.True(t, qrdbsdk.IsPermissionRegistered(perm),
			"%s must be registered by the new broker data-plane services' proto permission options", perm)
	}
}

// TestNewModule_RegistersRoleGrants_XKMSWireServices proves
// contributeGRPCPermissions's generic proto-reflection pass also picks up the
// permission vocabulary declared by the eight xkms-wire services this module
// registers (kms.proto's KeyManagement/DEKManagement/SecretsManagement/
// Crypto, certificate.proto's CertificateService/TrustService,
// attestation.proto's AttestationService, piv.proto's PIVService) — the same
// zero-extra-code mechanism TestNewModule_RegistersRoleGrants_NewBrokerDataPlaneServices
// proves for the broker data-plane services, requiring no edit to
// module_grants.go itself.
func TestNewModule_RegistersRoleGrants_XKMSWireServices(t *testing.T) {
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)

	for _, perm := range []qrdbsdk.Permission{
		"keys:create",
		"keys:get",
		"keys:list",
		"ca:create",
		"certs:generate",
		"certs:read",
		"attestation:verify",
		"piv:read",
	} {
		assert.True(t, qrdbsdk.IsPermissionRegistered(perm),
			"%s must be registered by the xkms-wire services' proto permission options", perm)
	}
}
