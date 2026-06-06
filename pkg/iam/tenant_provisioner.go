//go:build iam

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
