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

package iam

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/database"
)

// SetProvisioner sets the tenant provisioner on the DefaultTenantManager.
// When set, CreateTenant will call ProvisionTenant after successful creation,
// and DeleteTenant will call DeprovisionTenant before removal.
func (m *DefaultTenantManager) SetProvisioner(p database.TenantProvisioner) {
	m.provisioner = p
}

// Provisioner returns the configured tenant provisioner, or nil.
func (m *DefaultTenantManager) Provisioner() database.TenantProvisioner {
	return m.provisioner
}

// SetQRDBTenantProvisioner wires the qrdb engine's tenant provisioner.
// When set, CreateTenant calls p.CreateTenant BEFORE writing BaaS metadata or
// firing the database provisioner, ensuring barrier init, per-tenant DEK, and
// quota allocation all complete atomically on the engine side first. On failure
// the BaaS-layer creation is aborted. DeleteTenant calls p.DeleteTenant AFTER
// deprovisioning database infrastructure so the engine cleans up last.
func (m *DefaultTenantManager) SetQRDBTenantProvisioner(p QRDBTenantProvisioner) {
	m.qrdbProvisioner = p
}

// QRDBTenantProvisioner returns the configured qrdb tenant provisioner, or nil.
func (m *DefaultTenantManager) QRDBProvisioner() QRDBTenantProvisioner {
	return m.qrdbProvisioner
}

// SetUserStorage wires the routed (qrdb-backed) user store at server init so
// DeleteTenant can deprovision the tenant's users through it — purging the
// broker profile, IDMap, and email/legacy-PIV index records that the legacy
// BackendStorage cascade no longer reaches. When unset, DeleteTenant's
// user-deprovision step is a no-op.
func (m *DefaultTenantManager) SetUserStorage(s UserStorage) {
	m.userStorage = s
}

// SetWebAuthnCredentialReaper wires the credential revocation DeleteTenant runs
// as it deprovisions each of the tenant's users. Tenant teardown deletes users
// through the user store directly rather than through IdentityStore.DeleteUser,
// so without this the tenant's WebAuthn credentials would outlive the tenant.
// When unset, the revocation step is a no-op.
func (m *DefaultTenantManager) SetWebAuthnCredentialReaper(r WebAuthnCredentialReaper) {
	m.webauthnCredentials = r
}

// UserStorage returns the configured user store, or nil.
func (m *DefaultTenantManager) UserStorage() UserStorage {
	return m.userStorage
}

// SetRoleSeeder wires the seeder that materializes qrdb's built-in roles in
// each newly created tenant. When set, CreateTenant seeds the roles as its
// last step and rolls the whole creation back if seeding fails, so a tenant
// that exists is always a tenant a user can be created in. Call during server
// init, before the manager is shared.
func (m *DefaultTenantManager) SetRoleSeeder(s TenantRoleSeeder) {
	m.roleSeeder = s
}

// RoleSeeder returns the configured built-in role seeder, or nil.
func (m *DefaultTenantManager) RoleSeeder() TenantRoleSeeder {
	return m.roleSeeder
}

// SetPromptSeeder wires the seeder that copies the system prompt set into
// each newly created tenant. When set, CreateTenant seeds the prompts after
// the built-in roles and rolls the whole creation back if seeding fails. Call
// during server init, before the manager is shared.
func (m *DefaultTenantManager) SetPromptSeeder(s TenantPromptSeeder) {
	m.promptSeeder = s
}

// PromptSeeder returns the configured prompt seeder, or nil.
func (m *DefaultTenantManager) PromptSeeder() TenantPromptSeeder {
	return m.promptSeeder
}
