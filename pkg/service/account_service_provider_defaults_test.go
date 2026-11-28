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
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// newSeededAccountService builds an account service over a store that already
// holds tenant-a's default provider accounts, as a default install leaves it.
func newSeededAccountService(t *testing.T) AccountService {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	store := orchestration.NewInMemoryAccountStore()
	svc := NewAccountService(store, orchestration.NewAccountSecretStore(mgr))
	registry, err := orchestration.NewAccountProviderRegistry()
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}
	if _, err := orchestration.SeedDefaultProviderAccounts(callerContext("tenant-a"), store, registry, "tenant-a"); err != nil {
		t.Fatalf("SeedDefaultProviderAccounts() error = %v", err)
	}
	return svc
}

func firstLoginCredentials() map[string]*orchestration.AccountCredential {
	return map[string]*orchestration.AccountCredential{
		"default-claude": {AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok-test"},
		"default-codex":  {Env: map[string]string{"OPENAI_API_KEY": "sk-test"}},
	}
}

func TestAccountService_DefaultProviderAccount_ListsAwaitingCredential(t *testing.T) {
	svc := newSeededAccountService(t)
	resp, err := svc.ListAccounts(callerContext("tenant-a"), &ListAccountsRequest{
		TenantID: "tenant-a", Status: orchestration.AccountStatusAwaitingCredential,
	})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	got := map[string]bool{}
	for _, account := range resp.Accounts {
		got[account.ID] = true
	}
	if len(resp.Accounts) != 2 || !got["default-claude"] || !got["default-codex"] {
		t.Fatalf("awaiting accounts = %v, want default-claude and default-codex", got)
	}
}

func TestAccountService_DefaultProviderAccount_FirstLoginSetsCredential(t *testing.T) {
	svc := newSeededAccountService(t)
	ctx := callerContext("tenant-a")
	for id, credential := range firstLoginCredentials() {
		if _, err := svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
			TenantID: "tenant-a", AccountID: id, Credential: credential,
		}); err != nil {
			t.Fatalf("RotateAccountCredential(%s) error = %v", id, err)
		}
		got, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: id})
		if err != nil {
			t.Fatalf("GetAccount(%s) error = %v", id, err)
		}
		if got.Account.Status != orchestration.AccountStatusActive || !got.Account.Default {
			t.Fatalf("%s after first login = status %q default %v, want active and default",
				id, got.Account.Status, got.Account.Default)
		}
	}
}

func TestAccountService_DefaultProviderAccount_NeverShowsCredential(t *testing.T) {
	svc := newSeededAccountService(t)
	ctx := callerContext("tenant-a")
	for id, credential := range firstLoginCredentials() {
		if _, err := svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
			TenantID: "tenant-a", AccountID: id, Credential: credential,
		}); err != nil {
			t.Fatalf("RotateAccountCredential(%s) error = %v", id, err)
		}
		got, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: id})
		if err != nil {
			t.Fatalf("GetAccount(%s) error = %v", id, err)
		}
		assertNoFirstLoginSecret(t, got)
	}
	list, err := svc.ListAccounts(ctx, &ListAccountsRequest{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	assertNoFirstLoginSecret(t, list)
}

func assertNoFirstLoginSecret(t *testing.T, response any) {
	t.Helper()
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, secret := range []string{"tok-test", "sk-test"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("response %s shows the credential %q", raw, secret)
		}
	}
}

func TestAccountService_DefaultProviderAccount_RejectsInvalidFirstCredential(t *testing.T) {
	svc := newSeededAccountService(t)
	ctx := callerContext("tenant-a")
	if _, err := svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
		TenantID: "tenant-a", AccountID: "default-claude", Credential: &orchestration.AccountCredential{},
	}); err == nil {
		t.Fatal("RotateAccountCredential() with an empty credential succeeded")
	}
	got, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: "default-claude"})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if got.Account.Status != orchestration.AccountStatusAwaitingCredential {
		t.Fatalf("status after a refused first credential = %q, want awaiting_credential", got.Account.Status)
	}
}

func TestAccountService_DefaultProviderAccount_CanBeDeleted(t *testing.T) {
	svc := newSeededAccountService(t)
	if _, err := svc.DeleteAccount(callerContext("tenant-a"), &DeleteAccountRequest{
		TenantID: "tenant-a", AccountID: "default-codex",
	}); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
}
