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
