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

package orchestration

import (
	"context"
	"errors"
	"fmt"
)

// DefaultProviderAccountID is the fixed ID of provider's default account. It
// is deterministic so every node seeding the same tenant writes the same
// record, and a second seed is refused as a duplicate instead of adding one.
func DefaultProviderAccountID(provider AccountProvider) string { return "default-" + string(provider) }

// SeedDefaultProviderAccounts creates, in tenantID, one default account for
// each frontier provider that has no account at all yet. Each is created
// AccountStatusAwaitingCredential with no credential.
// It claims its provider's tenant-wide default only when that is vacant, and
// never displaces an existing default. It returns the accounts it created.
// It is idempotent: a provider that already has any account in the tenant
// is skipped, and losing a race to another seeder counts as seeded.
func SeedDefaultProviderAccounts(ctx context.Context, store AccountStore, providers *AccountProviderRegistry, tenantID string) ([]*Account, error) {
	if store == nil {
		return nil, &AccountInvalidError{Field: "store", Message: "an account store is required"}
	}
	if providers == nil {
		return nil, &AccountInvalidError{Field: "providers", Message: "a provider registry is required"}
	}
	if tenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant id must not be empty"}
	}
	var created []*Account
	for _, provider := range providers.DefaultAccountProviders() {
		existing, err := store.ListByTenant(ctx, tenantID, provider, "")
		if err != nil {
			return created, fmt.Errorf("list %s accounts: %w", provider, err)
		}
		if len(existing) > 0 {
			continue
		}
		displayName := string(provider)
		if descriptor, ok := providers.Descriptor(provider); ok && descriptor.DisplayName != "" {
			displayName = descriptor.DisplayName
		}
		account := &Account{
			ID:            DefaultProviderAccountID(provider),
			TenantID:      tenantID,
			Provider:      provider,
			DisplayName:   displayName + " (default)",
			Status:        AccountStatusAwaitingCredential,
			RefreshPolicy: AccountCredentialModeStaticToken,
		}
		if err := store.Create(ctx, account); err != nil {
			var invalid *AccountInvalidError
			if errors.As(err, &invalid) && invalid.Field == "id" {
				// Another seeder created it first.
				continue
			}
			return created, fmt.Errorf("create %s default account: %w", provider, err)
		}
		created = append(created, account)
	}
	return created, nil
}
