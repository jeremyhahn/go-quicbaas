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
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Default-account semantics at the operator-facing boundary.
//
// pkg/orchestration owns the rule (accountStore.create claims a vacant scope);
// these tests assert AccountService exposes it faithfully, because that is the
// object every transport — REST, gRPC, QUIC, MCP, Unix, embedded — and the CLI
// call. A create response whose Default flag lagged the durable pointer would
// show an operator the wrong answer on all six.
// =============================================================================

// createAccountForDefaultTest imports one static-token account and returns the
// service's own response account.
func createAccountForDefaultTest(t *testing.T, svc AccountService, tenantID string,
	provider orchestration.AccountProvider, displayName, token string) *orchestration.Account {
	t.Helper()
	resp, err := svc.CreateAccount(callerContext(tenantID), &CreateAccountRequest{
		TenantID:      tenantID,
		Provider:      provider,
		DisplayName:   displayName,
		RefreshPolicy: orchestration.AccountRefreshNone,
		Credential: &orchestration.AccountCredential{
			AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
			AccessToken:       token,
		},
	})
	if err != nil {
		t.Fatalf("CreateAccount(%s) error = %v", displayName, err)
	}
	return resp.Account
}

// listedDefaults returns the IDs ListAccounts reports as default.
func listedDefaults(t *testing.T, svc AccountService, tenantID string, provider orchestration.AccountProvider) []string {
	t.Helper()
	resp, err := svc.ListAccounts(callerContext(tenantID), &ListAccountsRequest{TenantID: tenantID, Provider: provider})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	var ids []string
	for _, a := range resp.Accounts {
		if a.Default {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

func TestAccountService_CreateAccount_FirstBecomesDefault(t *testing.T) {
	svc := newTestAccountService(t)

	first := createAccountForDefaultTest(t, svc, "tenant-a", orchestration.AccountProviderClaude, "anthropic-1", "sk-1")
	if !first.Default {
		t.Fatalf("CreateAccount().Account.Default = false for the first account in the scope")
	}

	// The response is not just optimistic: a fresh read agrees.
	got, err := svc.GetAccount(callerContext("tenant-a"), &GetAccountRequest{TenantID: "tenant-a", AccountID: first.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if !got.Account.Default {
		t.Fatalf("GetAccount().Account.Default = false; the create response disagreed with the durable record")
	}
}

func TestAccountService_CreateAccount_SecondDoesNotBecomeDefault(t *testing.T) {
	svc := newTestAccountService(t)

	first := createAccountForDefaultTest(t, svc, "tenant-a", orchestration.AccountProviderClaude, "anthropic-1", "sk-1")
	second := createAccountForDefaultTest(t, svc, "tenant-a", orchestration.AccountProviderClaude, "anthropic-2", "sk-2")

	if second.Default {
		t.Fatalf("CreateAccount().Account.Default = true for the second account in the scope")
	}
	if got := listedDefaults(t, svc, "tenant-a", orchestration.AccountProviderClaude); len(got) != 1 || got[0] != first.ID {
		t.Fatalf("ListAccounts defaults = %v, want exactly [%s]", got, first.ID)
	}
}

// TestAccountService_SetDefaultAccount_SwitchIsAtomic proves the operator's
// "make this one the default instead" is a single transition: the pool never
// reports two defaults and never reports none.
func TestAccountService_SetDefaultAccount_SwitchIsAtomic(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := callerContext("tenant-a")

	first := createAccountForDefaultTest(t, svc, "tenant-a", orchestration.AccountProviderClaude, "anthropic-1", "sk-1")
	second := createAccountForDefaultTest(t, svc, "tenant-a", orchestration.AccountProviderClaude, "anthropic-2", "sk-2")

	if _, err := svc.SetDefaultAccount(ctx, &SetDefaultAccountRequest{TenantID: "tenant-a", AccountID: second.ID}); err != nil {
		t.Fatalf("SetDefaultAccount() error = %v", err)
	}

	got := listedDefaults(t, svc, "tenant-a", orchestration.AccountProviderClaude)
	if len(got) != 1 || got[0] != second.ID {
		t.Fatalf("defaults after the switch = %v, want exactly [%s] (first was %s)", got, second.ID, first.ID)
	}
}

// TestAccountService_DeleteAccount_DefaultLeavesScopeVacant documents the
// delete decision at the boundary an operator sees.
func TestAccountService_DeleteAccount_DefaultLeavesScopeVacant(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := callerContext("tenant-a")

	first := createAccountForDefaultTest(t, svc, "tenant-a", orchestration.AccountProviderClaude, "anthropic-1", "sk-1")
	createAccountForDefaultTest(t, svc, "tenant-a", orchestration.AccountProviderClaude, "anthropic-2", "sk-2")
	createAccountForDefaultTest(t, svc, "tenant-a", orchestration.AccountProviderClaude, "anthropic-3", "sk-3")

	if _, err := svc.DeleteAccount(ctx, &DeleteAccountRequest{TenantID: "tenant-a", AccountID: first.ID}); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	if got := listedDefaults(t, svc, "tenant-a", orchestration.AccountProviderClaude); len(got) != 0 {
		t.Fatalf("defaults after deleting the default = %v, want none — no survivor is promoted", got)
	}

	// And the vacancy is claimable again by the next import.
	fourth := createAccountForDefaultTest(t, svc, "tenant-a", orchestration.AccountProviderClaude, "anthropic-4", "sk-4")
	if !fourth.Default {
		t.Fatalf("a scope left vacant by a delete did not accept the next account as its default")
	}
}

// TestAccountService_CreateAccount_ConcurrentFirstImports_OneDefault is the
// race requirement seen from the API: two transports importing the tenant's
// first account at the same instant must not both report Default.
func TestAccountService_CreateAccount_ConcurrentFirstImports_OneDefault(t *testing.T) {
	svc := newTestAccountService(t)

	const importers = 8
	results := make([]*orchestration.Account, importers)
	errs := make([]error, importers)

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := 0; i < importers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			resp, err := svc.CreateAccount(callerContext("tenant-a"), &CreateAccountRequest{
				TenantID:      "tenant-a",
				Provider:      orchestration.AccountProviderClaude,
				RefreshPolicy: orchestration.AccountRefreshNone,
				Credential:    &orchestration.AccountCredential{AccessToken: "sk"},
			})
			errs[i] = err
			if err == nil {
				results[i] = resp.Account
			}
		}(i)
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("CreateAccount(#%d) error = %v", i, err)
		}
	}

	var claimed []string
	for _, a := range results {
		if a.Default {
			claimed = append(claimed, a.ID)
		}
	}
	if len(claimed) != 1 {
		t.Fatalf("%d concurrent imports reported Default = true (%v), want exactly 1", len(claimed), claimed)
	}
	if got := listedDefaults(t, svc, "tenant-a", orchestration.AccountProviderClaude); len(got) != 1 || got[0] != claimed[0] {
		t.Fatalf("listed defaults = %v, want exactly [%s]", got, claimed[0])
	}
}

// TestAccountService_CreateAccount_OAuthRefreshWithOperatorEndpoint proves the
// oauth_refresh mode is accepted at write time once the operator supplies the
// two values QuicBaaS deliberately does not ship for Anthropic/OpenAI.
func TestAccountService_CreateAccount_OAuthRefreshWithOperatorEndpoint(t *testing.T) {
	svc := newTestAccountService(t)

	resp, err := svc.CreateAccount(callerContext("tenant-a"), &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		DisplayName:   "anthropic-oauth",
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		Credential: &orchestration.AccountCredential{
			AccessToken:   "sk-access",
			RefreshToken:  "rt-refresh",
			TokenEndpoint: "https://oauth.example.test/token",
			ClientID:      "operator-supplied-client-id",
		},
	})
	if err != nil {
		t.Fatalf("CreateAccount(oauth2) error = %v", err)
	}
	if !resp.Account.Default {
		t.Fatalf("the first oauth2 account is not the default")
	}
	if resp.Account.RefreshPolicy != orchestration.AccountRefreshOAuth2 {
		t.Fatalf("RefreshPolicy = %q, want oauth2", resp.Account.RefreshPolicy)
	}
}
