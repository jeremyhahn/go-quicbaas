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
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// =============================================================================
// Limit holds: exclusion while held, lazy return to the pool once lapsed.
// =============================================================================

// seedPoolAccount creates one account plus a usable credential envelope.
func seedPoolAccount(t *testing.T, store AccountStore, secrets *AccountSecretStore, account *Account) {
	t.Helper()
	ctx := context.Background()
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create(%s): %v", account.ID, err)
	}
	if err := secrets.Put(ctx, account.TenantID, account.ID, &AccountCredential{
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
		AccessToken:       "tok-" + account.ID,
	}); err != nil {
		t.Fatalf("Put(%s): %v", account.ID, err)
	}
}

// holdAccountUntil marks an account limited until availableAt.
func holdAccountUntil(t *testing.T, store AccountStore, tenantID, accountID string, availableAt time.Time) {
	t.Helper()
	if err := store.Touch(context.Background(), tenantID, accountID, TouchAccountOptions{
		SetStatusValid:      true,
		SetStatus:           AccountStatusLimited,
		SetAvailableAt:      availableAt,
		SetAvailableAtValid: true,
		SetLastError:        "usage limit reached",
	}); err != nil {
		t.Fatalf("Touch(%s): %v", accountID, err)
	}
}

func TestAccountResolver_HeldAccountIsExcludedFromSelection(t *testing.T) {
	resolver, store, secrets := setupResolver(t)
	ctx := context.Background()

	seedPoolAccount(t, store, secrets, &Account{ID: "acct-held", TenantID: "tenant-a", Provider: AccountProviderClaude})
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-free", TenantID: "tenant-a", Provider: AccountProviderClaude})
	holdAccountUntil(t, store, "tenant-a", "acct-held", time.Now().UTC().Add(time.Hour))

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
	})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if resolved.AccountID != "acct-free" {
		t.Fatalf("Resolve() picked %q, want acct-free — an account under a live limit hold must not be selectable",
			resolved.AccountID)
	}
}

func TestAccountResolver_LapsedHoldReturnsAccountToPool(t *testing.T) {
	resolver, store, secrets := setupResolver(t)
	ctx := context.Background()

	seedPoolAccount(t, store, secrets, &Account{ID: "acct-solo", TenantID: "tenant-a", Provider: AccountProviderClaude})
	// The hold has already lapsed: nothing swept it, and nothing needs to.
	holdAccountUntil(t, store, "tenant-a", "acct-solo", time.Now().UTC().Add(-time.Minute))

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
	})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if resolved.AccountID != "acct-solo" {
		t.Fatalf("Resolve() = %q, want acct-solo — a lapsed hold must return the account to the pool on read",
			resolved.AccountID)
	}

	// And the recovery is written back, so an operator listing the pool sees
	// "active" rather than a record that only looks recovered to the resolver.
	got, err := store.Get(ctx, "tenant-a", "acct-solo")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Status != AccountStatusActive {
		t.Fatalf("Status = %q, want active after the hold lapsed", got.Status)
	}
	if !got.AvailableAt.IsZero() {
		t.Fatalf("AvailableAt = %s, want zero once the hold lapsed", got.AvailableAt)
	}
}

func TestAccountResolver_HeldAccountWithNoAvailableAtStaysHeld(t *testing.T) {
	resolver, store, secrets := setupResolver(t)
	ctx := context.Background()

	seedPoolAccount(t, store, secrets, &Account{ID: "acct-solo", TenantID: "tenant-a", Provider: AccountProviderClaude})
	// Limited with no recovery time: nothing promised when it comes back, so
	// guessing "immediately" would put it straight back into the failure.
	if err := store.Touch(ctx, "tenant-a", "acct-solo", TouchAccountOptions{
		SetStatusValid: true, SetStatus: AccountStatusLimited,
	}); err != nil {
		t.Fatalf("Touch(): %v", err)
	}

	_, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
	})
	if !errors.Is(err, ErrNoAccountAvailable) {
		t.Fatalf("Resolve() error = %v, want ErrNoAccountAvailable", err)
	}
}

func TestAccountResolver_PinnedHeldAccountFailsLoudly(t *testing.T) {
	resolver, store, secrets := setupResolver(t)
	ctx := context.Background()

	seedPoolAccount(t, store, secrets, &Account{ID: "acct-held", TenantID: "tenant-a", Provider: AccountProviderClaude})
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-free", TenantID: "tenant-a", Provider: AccountProviderClaude})
	availableAt := time.Now().UTC().Add(90 * time.Minute).Truncate(time.Second)
	holdAccountUntil(t, store, "tenant-a", "acct-held", availableAt)

	_, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-held",
	})
	var limited *AccountLimitedError
	if !errors.As(err, &limited) {
		t.Fatalf("Resolve() error = %v, want AccountLimitedError — a pin must fail loudly, never be silently swapped", err)
	}
	if !limited.AvailableAt.Equal(availableAt) {
		t.Fatalf("AccountLimitedError.AvailableAt = %s, want %s", limited.AvailableAt, availableAt)
	}
	if !errors.Is(err, ErrAccountLimited) {
		t.Fatalf("errors.Is(err, ErrAccountLimited) = false")
	}
}

// =============================================================================
// Rotation policy: allow-list and exclusions
// =============================================================================

func TestAccountResolver_AllowListRestrictsSelection(t *testing.T) {
	resolver, store, secrets := setupResolver(t)
	ctx := context.Background()

	seedPoolAccount(t, store, secrets, &Account{ID: "acct-in", TenantID: "tenant-a", Provider: AccountProviderClaude})
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-out", TenantID: "tenant-a", Provider: AccountProviderClaude, Default: true})

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
		AllowedAccountIDs: []string{"acct-in"},
	})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if resolved.AccountID != "acct-in" {
		t.Fatalf("Resolve() = %q, want acct-in — the allow-list must beat the pool default", resolved.AccountID)
	}
}

func TestAccountResolver_AllowListWithNoEligibleMemberFails(t *testing.T) {
	resolver, store, secrets := setupResolver(t)
	ctx := context.Background()

	seedPoolAccount(t, store, secrets, &Account{ID: "acct-other", TenantID: "tenant-a", Provider: AccountProviderClaude})

	_, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
		AllowedAccountIDs: []string{"acct-not-in-pool"},
	})
	if !errors.Is(err, ErrNoAccountAvailable) {
		t.Fatalf("Resolve() error = %v, want ErrNoAccountAvailable — an allow-list is a filter, never a source", err)
	}
}

func TestAccountResolver_ExclusionGuaranteesADifferentAccount(t *testing.T) {
	resolver, store, secrets := setupResolver(t)
	ctx := context.Background()

	// The excluded account is the pool DEFAULT, so a resolution that ignored
	// the exclusion would pick it.
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-spent", TenantID: "tenant-a", Provider: AccountProviderClaude, Default: true})
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-fresh", TenantID: "tenant-a", Provider: AccountProviderClaude})

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
		ExcludeAccountIDs: []string{"acct-spent"},
	})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if resolved.AccountID != "acct-fresh" {
		t.Fatalf("Resolve() = %q, want acct-fresh", resolved.AccountID)
	}
}

// =============================================================================
// Store semantics
// =============================================================================

func TestAccountStore_TouchClearsHoldWhenStatusLeavesLimited(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	holdAccountUntil(t, store, "tenant-a", "acct-1", time.Now().UTC().Add(time.Hour))

	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{
		SetStatusValid: true, SetStatus: AccountStatusRevoked,
	}); err != nil {
		t.Fatalf("Touch(): %v", err)
	}

	got, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if got.Status != AccountStatusRevoked {
		t.Fatalf("Status = %q, want revoked", got.Status)
	}
	if !got.AvailableAt.IsZero() {
		t.Fatalf("AvailableAt = %s, want zero — a stale hold on a non-limited account reads as held forever", got.AvailableAt)
	}
}

func TestAccountStore_TouchAppliesCredentialExpiryAndValidation(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	expiry := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{
		MarkValidated:            true,
		SetCredentialExpiresAt:   expiry,
		SetCredentialExpiryValid: true,
	}); err != nil {
		t.Fatalf("Touch(): %v", err)
	}

	got, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if !got.CredentialExpiresAt.Equal(expiry) {
		t.Fatalf("CredentialExpiresAt = %s, want %s", got.CredentialExpiresAt, expiry)
	}
	if got.LastValidatedAt.IsZero() {
		t.Fatalf("LastValidatedAt not advanced by MarkValidated")
	}
}

func TestAccountStore_ListNormalizesLapsedHolds(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()
	if err := store.Create(ctx, &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}); err != nil {
		t.Fatalf("Create(): %v", err)
	}
	holdAccountUntil(t, store, "tenant-a", "acct-1", time.Now().UTC().Add(-time.Second))

	accounts, err := store.ListByTenant(ctx, "tenant-a", "", "")
	if err != nil {
		t.Fatalf("ListByTenant(): %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("ListByTenant() returned %d accounts, want 1", len(accounts))
	}
	if accounts[0].Status != AccountStatusActive {
		t.Fatalf("Status = %q, want active — a list must not describe an account as limited past its own AvailableAt",
			accounts[0].Status)
	}
}

func TestAccountStore_DerivedHealthIsNotPersisted(t *testing.T) {
	kv := NewMemoryTaskKV()
	store := NewAccountStore(kv)
	ctx := context.Background()
	account := &Account{
		ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude,
		CredentialExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create(): %v", err)
	}

	recordKey, keyErr := accountRecordKey("tenant-a", "acct-1")
	if keyErr != nil {
		t.Fatalf("accountRecordKey(): %v", keyErr)
	}
	raw, err := kv.Get(ctx, recordKey)
	if err != nil {
		t.Fatalf("kv.Get(): %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := decoded["credential_health"]; present {
		t.Fatalf("durable record carries credential_health: %s — a derived verdict written to storage is stale the moment it lands", raw)
	}
}

func TestAccount_MarshalJSON_CarriesDerivedHealth(t *testing.T) {
	account := &Account{
		ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude,
		CredentialExpiresAt: time.Now().UTC().Add(2 * time.Hour),
	}
	encoded, err := json.Marshal(account)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		CredentialHealth string `json:"credential_health"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.CredentialHealth != string(AccountCredentialHealthExpiring) {
		t.Fatalf("credential_health = %q, want expiring", decoded.CredentialHealth)
	}
}

func TestAccount_CredentialHealth(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name    string
		account *Account
		want    AccountCredentialHealth
	}{
		{"nil receiver", nil, AccountCredentialHealthUnknown},
		{"no expiry recorded", &Account{}, AccountCredentialHealthUnknown},
		{"expired", &Account{CredentialExpiresAt: now.Add(-time.Second)}, AccountCredentialHealthExpired},
		{"expiring inside the warning window", &Account{CredentialExpiresAt: now.Add(time.Hour)}, AccountCredentialHealthExpiring},
		{"valid beyond the warning window", &Account{CredentialExpiresAt: now.Add(72 * time.Hour)}, AccountCredentialHealthValid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.account.CredentialHealth(); got != tt.want {
				t.Fatalf("CredentialHealth() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAccount_Selectable(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{"nil", nil, false},
		{"active", &Account{Status: AccountStatusActive}, true},
		{"revoked", &Account{Status: AccountStatusRevoked}, false},
		{"error", &Account{Status: AccountStatusError}, false},
		{"limited, hold live", &Account{Status: AccountStatusLimited, AvailableAt: now.Add(time.Minute)}, false},
		{"limited, hold lapsed", &Account{Status: AccountStatusLimited, AvailableAt: now.Add(-time.Minute)}, true},
		{"limited, no recovery time", &Account{Status: AccountStatusLimited}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.account.Selectable(now); got != tt.want {
				t.Fatalf("Selectable() = %v, want %v", got, tt.want)
			}
		})
	}
}
