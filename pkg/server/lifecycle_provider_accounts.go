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

package server

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// seedDefaultProviderAccounts creates each tenant's default provider
// accounts: now for every tenant it can read, again for each tenant whose
// barrier is unsealed later, and for each tenant created after startup,
// whatever its isolation. A failure is logged, never fatal: a missing default
// account is something the user can add by hand, and must neither stop the
// server nor fail or undo a tenant's creation.
func (s *Server) seedDefaultProviderAccounts(ctx context.Context, store orchestration.AccountStore, providers *orchestration.AccountProviderRegistry) {
	for _, tenantID := range s.agentMigrationTenants(ctx) {
		s.seedTenantProviderAccounts(ctx, store, providers, tenantID)
	}
	s.tenantUnsealHooks.Register(tenantUnsealFunc(func(ctx context.Context, tenantID string) {
		s.seedTenantProviderAccounts(ctx, store, providers, tenantID)
	}))
	s.tenantCreatedHooks.Register(tenantCreatedFunc(func(ctx context.Context, tenantID string) {
		s.seedTenantProviderAccounts(ctx, store, providers, tenantID)
	}))
}

// seedTenantProviderAccounts seeds one tenant's default provider accounts and
// logs what it created, each awaiting the credential its user sets at first
// login.
func (s *Server) seedTenantProviderAccounts(ctx context.Context, store orchestration.AccountStore, providers *orchestration.AccountProviderRegistry, tenantID string) {
	created, err := orchestration.SeedDefaultProviderAccounts(migrationContext(ctx, tenantID), store, providers, tenantID)
	if err != nil {
		s.logger.Warnf("default provider accounts: tenant %s: %v", tenantID, err)
	}
	for _, a := range created {
		s.logger.Infof("default provider accounts: tenant %s: created %s (%s), awaiting its credential", tenantID, a.ID, a.Provider)
	}
}
