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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// noopLogger discards every log call; used only where a CostTracker
// dependency requires a non-nil interfaces.Logger.
type noopLogger struct{}

func (noopLogger) Debug(string, ...any)                                  {}
func (noopLogger) Debugf(string, ...interface{})                         {}
func (noopLogger) Info(string, ...any)                                   {}
func (noopLogger) Infof(string, ...interface{})                          {}
func (noopLogger) Warn(string, ...any)                                   {}
func (noopLogger) Warnf(string, ...interface{})                          {}
func (noopLogger) Error(string, ...any)                                  {}
func (noopLogger) Errorf(string, ...interface{})                         {}
func (noopLogger) Fatal(string)                                          {}
func (noopLogger) Fatalf(string, ...interface{})                         {}
func (l noopLogger) WithField(string, interface{}) interfaces.Logger     { return l }
func (l noopLogger) WithFields(map[string]interface{}) interfaces.Logger { return l }
func (l noopLogger) WithError(error) interfaces.Logger                   { return l }

// newTestAccountService creates an AccountService backed by in-memory stores.
func newTestAccountService(t *testing.T) AccountService {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	return NewAccountService(orchestration.NewInMemoryAccountStore(), orchestration.NewAccountSecretStore(mgr))
}

func TestAccountService_CreateAccount_HappyPath(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()

	resp, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID:    "tenant-a",
		Provider:    orchestration.AccountProviderClaude,
		DisplayName: "alice",
		Credential:  &orchestration.AccountCredential{AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok-1"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if resp.Account.ID == "" {
		t.Fatalf("CreateAccount() did not assign an ID")
	}
	if resp.Account.DisplayName != "alice" {
		t.Fatalf("CreateAccount().Account.DisplayName = %s, want alice", resp.Account.DisplayName)
	}
}

func TestAccountService_CreateAccount_MissingCredential(t *testing.T) {
	svc := newTestAccountService(t)
	_, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID: "tenant-a",
		Provider: orchestration.AccountProviderClaude,
	})
	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("CreateAccount() error = %v (%T), want *AccountInvalidError", err, err)
	}
}

func TestAccountService_CreateAccount_MissingTenantID(t *testing.T) {
	svc := newTestAccountService(t)
	_, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		Provider:   orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err == nil {
		t.Fatalf("CreateAccount() error = nil, want a validation error for missing tenant_id")
	}
}

func TestAccountService_GetAccount_HappyPath(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderCodex, Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	got, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if got.Account.ID != created.Account.ID {
		t.Fatalf("GetAccount().Account.ID = %s, want %s", got.Account.ID, created.Account.ID)
	}
}

func TestAccountService_GetAccount_NotFound(t *testing.T) {
	svc := newTestAccountService(t)
	_, err := svc.GetAccount(context.Background(), &GetAccountRequest{TenantID: "tenant-a", AccountID: "missing"})
	var notFound *AccountNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("GetAccount() error = %v (%T), want *AccountNotFoundError", err, err)
	}
}

// TestAccountService_GetAccount_TenantIsolation proves tenant B cannot read
// tenant A's account metadata even by guessing its ID.
func TestAccountService_GetAccount_TenantIsolation(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	_, err = svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-b", AccountID: created.Account.ID})
	if err == nil {
		t.Fatalf("GetAccount() across tenants succeeded — tenant isolation broken")
	}
}

func TestAccountService_ListAccounts_FiltersByProvider(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	for _, provider := range []orchestration.AccountProvider{orchestration.AccountProviderClaude, orchestration.AccountProviderClaude, orchestration.AccountProviderCodex} {
		if _, err := svc.CreateAccount(ctx, &CreateAccountRequest{
			TenantID: "tenant-a", Provider: provider, Credential: &orchestration.AccountCredential{AccessToken: "x"},
		}); err != nil {
			t.Fatalf("CreateAccount() error = %v", err)
		}
	}

	resp, err := svc.ListAccounts(ctx, &ListAccountsRequest{TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("ListAccounts(claude) Total = %d, want 2", resp.Total)
	}
}

func TestAccountService_UpdateAccount_DisplayName(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, DisplayName: "old",
		Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	updated, err := svc.UpdateAccount(ctx, &UpdateAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID, DisplayName: "new"})
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	if updated.Account.DisplayName != "new" {
		t.Fatalf("UpdateAccount().Account.DisplayName = %s, want new", updated.Account.DisplayName)
	}
}

func TestAccountService_RotateAccountCredential(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "old-token"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	_, err = svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: created.Account.ID,
		Credential: &orchestration.AccountCredential{AccessToken: "new-token"},
	})
	if err != nil {
		t.Fatalf("RotateAccountCredential() error = %v", err)
	}

	got, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if got.Account.Status != orchestration.AccountStatusActive {
		t.Fatalf("Account.Status = %s after rotation, want active", got.Account.Status)
	}
}

func TestAccountService_RotateAccountCredential_UnknownAccount(t *testing.T) {
	svc := newTestAccountService(t)
	_, err := svc.RotateAccountCredential(context.Background(), &RotateAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: "missing", Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err == nil {
		t.Fatalf("RotateAccountCredential() error = nil, want AccountNotFoundError")
	}
}

func TestAccountService_RevokeAccount(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	resp, err := svc.RevokeAccount(ctx, &RevokeAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID, Reason: "compromised"})
	if err != nil {
		t.Fatalf("RevokeAccount() error = %v", err)
	}
	if resp.Account.Status != orchestration.AccountStatusRevoked {
		t.Fatalf("RevokeAccount().Account.Status = %s, want revoked", resp.Account.Status)
	}
}

func TestAccountService_DeleteAccount_RemovesMetadataAndCredential(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	if _, err := svc.DeleteAccount(ctx, &DeleteAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID}); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	if _, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID}); err == nil {
		t.Fatalf("GetAccount() after DeleteAccount() succeeded")
	}
}

func TestAccountService_DeleteAccount_NotFound(t *testing.T) {
	svc := newTestAccountService(t)
	_, err := svc.DeleteAccount(context.Background(), &DeleteAccountRequest{TenantID: "tenant-a", AccountID: "missing"})
	if err == nil {
		t.Fatalf("DeleteAccount() error = nil, want AccountNotFoundError")
	}
}

func TestAccountService_SetDefaultAccount(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	if _, err := svc.SetDefaultAccount(ctx, &SetDefaultAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID}); err != nil {
		t.Fatalf("SetDefaultAccount() error = %v", err)
	}
	got, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if !got.Account.Default {
		t.Fatalf("Account.Default = false after SetDefaultAccount()")
	}
}

func TestAccountService_GetAccountUsage_NotConfigured(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	_, err = svc.GetAccountUsage(ctx, &GetAccountUsageRequest{TenantID: "tenant-a", AccountID: created.Account.ID})
	var notConfigured *AccountUsageTrackerNotConfiguredError
	if !errors.As(err, &notConfigured) {
		t.Fatalf("GetAccountUsage() error = %v (%T), want *AccountUsageTrackerNotConfiguredError", err, err)
	}
}

func TestAccountService_GetAccountUsage_WithTracker(t *testing.T) {
	store := orchestration.NewInMemoryAccountStore()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	secretStore := orchestration.NewAccountSecretStore(mgr)
	svc := NewAccountService(store, secretStore).(*accountServiceImpl)

	tracker := orchestration.NewCostTracker(kvstore.NewMemory(), noopLogger{})
	svc.SetUsageTracker(tracker)

	ctx := context.Background()
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Credential: &orchestration.AccountCredential{AccessToken: "x"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if err := tracker.RecordTurnUsage(ctx, orchestration.TurnUsage{TenantID: "tenant-a", AccountID: created.Account.ID, Tokens: 77}); err != nil {
		t.Fatalf("RecordTurnUsage() error = %v", err)
	}

	resp, err := svc.GetAccountUsage(ctx, &GetAccountUsageRequest{TenantID: "tenant-a", AccountID: created.Account.ID})
	if err != nil {
		t.Fatalf("GetAccountUsage() error = %v", err)
	}
	if resp.Usage.TotalTokens != 77 {
		t.Fatalf("GetAccountUsage().Usage.TotalTokens = %d, want 77", resp.Usage.TotalTokens)
	}
}

// TestAccountService_EveryCredentialWriteAdvancesTheGeneration: an import is an
// account's first generation and a rotation is its next one, and both are
// mirrored onto the metadata record the moment they are written.
//
// The generation is what every live session's materialized copy is compared
// against: an import that recorded none would leave the pool unable to say
// which copy it holds, and a rotation that did not advance it would leave every
// session on the account holding a credential the provider has stopped
// honouring while the pool still described it as current (go-quicbaas#997).
func TestAccountService_EveryCredentialWriteAdvancesTheGeneration(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()

	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "old-token"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	imported := created.Account.CredentialGeneration
	if imported == 0 {
		t.Fatal("CreateAccount() recorded no credential generation; the pool cannot say which copy it holds")
	}

	stored, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if stored.Account.CredentialGeneration != imported {
		t.Fatalf("the persisted generation = %d, want the one the import reported (%d)",
			stored.Account.CredentialGeneration, imported)
	}

	if _, err := svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: created.Account.ID,
		Credential: &orchestration.AccountCredential{AccessToken: "new-token"},
	}); err != nil {
		t.Fatalf("RotateAccountCredential() error = %v", err)
	}

	rotated, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if rotated.Account.CredentialGeneration <= imported {
		t.Errorf("CredentialGeneration = %d after a rotation, want strictly greater than the imported %d",
			rotated.Account.CredentialGeneration, imported)
	}
	if rotated.Account.CredentialRotatedAt.IsZero() {
		t.Error("a rotation did not move CredentialRotatedAt")
	}
}
