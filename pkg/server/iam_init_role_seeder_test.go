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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestWireTenantRoleSeeder_EngineSeedsWhenItProvisions pins #1727: when the
// qrdb TenantServicer provisions tenants it seeds their built-in roles in the
// same operation, so quicbaas wires no second seeder. A second seed runs
// roles:create through the embedded client as the service principal, which go-qrdb
// refuses, and that refusal rolled every tenant creation back.
func TestWireTenantRoleSeeder_EngineSeedsWhenItProvisions(t *testing.T) {
	storage := iam.NewMemoryStorage()
	manager := iam.NewDefaultTenantManager(storage)
	manager.SetQRDBTenantProvisioner(newQRDBTenantProvisionerAdapter(nil))

	wireTenantRoleSeeder(manager, iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: storage, RoleStorage: storage}))

	assert.Nil(t, manager.RoleSeeder(), "a role seeder was wired although the engine seeds the roles")
}

// TestWireTenantRoleSeeder_SeedsWithoutAnEngineProvisioner is the other half:
// with no engine provisioner nothing else seeds a new tenant's roles, so the
// seeder stays wired (#269).
func TestWireTenantRoleSeeder_SeedsWithoutAnEngineProvisioner(t *testing.T) {
	storage := iam.NewMemoryStorage()
	manager := iam.NewDefaultTenantManager(storage)

	wireTenantRoleSeeder(manager, iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: storage, RoleStorage: storage}))

	assert.NotNil(t, manager.RoleSeeder(), "a tenant created without an engine provisioner would have no roles")
}
