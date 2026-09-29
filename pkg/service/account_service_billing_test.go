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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestAccountService_Billing_SetOnCreateAndChangedOnUpdate is go-quicbaas#1058:
// how an account is billed decides the billed figure every turn carries, so it
// is settable at create and on update, and an update that omits it leaves it
// alone.
func TestAccountService_Billing_SetOnCreateAndChangedOnUpdate(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	subscription := orchestration.AccountBilling{Mode: orchestration.BillingSubscription, MonthlyFeeMicros: 200_000_000}

	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Billing: subscription,
		Credential: &orchestration.AccountCredential{AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok"},
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if created.Account.Billing != subscription {
		t.Fatalf("created billing = %+v, want %+v", created.Account.Billing, subscription)
	}
	id := created.Account.ID

	renamed, err := svc.UpdateAccount(ctx, &UpdateAccountRequest{TenantID: "tenant-a", AccountID: id, DisplayName: "x"})
	if err != nil {
		t.Fatalf("UpdateAccount(rename): %v", err)
	}
	if renamed.Account.Billing != subscription {
		t.Fatalf("an update without billing changed it to %+v", renamed.Account.Billing)
	}

	api := orchestration.AccountBilling{Mode: orchestration.BillingAPI}
	updated, err := svc.UpdateAccount(ctx, &UpdateAccountRequest{TenantID: "tenant-a", AccountID: id, Billing: &api})
	if err != nil {
		t.Fatalf("UpdateAccount(billing): %v", err)
	}
	if updated.Account.Billing != api {
		t.Fatalf("updated billing = %+v, want %+v", updated.Account.Billing, api)
	}

	cleared, err := svc.UpdateAccount(ctx, &UpdateAccountRequest{TenantID: "tenant-a", AccountID: id, Billing: &orchestration.AccountBilling{}})
	if err != nil {
		t.Fatalf("UpdateAccount(clear): %v", err)
	}
	if cleared.Account.Billing != (orchestration.AccountBilling{}) {
		t.Fatalf("cleared billing = %+v, want not configured", cleared.Account.Billing)
	}
}

// TestAccountService_Billing_RefusedWithTheFieldNamed asserts an invalid
// billing reaches every transport as AccountInvalidError naming the field the
// operator wrote.
func TestAccountService_Billing_RefusedWithTheFieldNamed(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()

	_, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude,
		Billing:    orchestration.AccountBilling{Mode: orchestration.BillingAPI, MonthlyFeeMicros: 5},
		Credential: &orchestration.AccountCredential{AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok"},
	})
	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "billing.monthly_fee_micros" {
		t.Fatalf("CreateAccount = %v, want AccountInvalidError on billing.monthly_fee_micros", err)
	}

	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok"},
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	_, err = svc.UpdateAccount(ctx, &UpdateAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID,
		Billing: &orchestration.AccountBilling{Mode: "prepaid"}})
	if !errors.As(err, &invalid) || invalid.Field != "billing.mode" {
		t.Fatalf("UpdateAccount = %v, want AccountInvalidError on billing.mode", err)
	}
}

// TestAccountService_SpendPolicy_SetOnCreateAndChangedOnUpdate is
// go-quicbaas#781: an account's spend policy is settable through the service
// every transport calls, and an update without one leaves it alone.
func TestAccountService_SpendPolicy_SetOnCreateAndChangedOnUpdate(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	policy := orchestration.AccountSpendPolicy{
		Quota:   orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 2_000_000}}},
		Breaker: &orchestration.BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: 30 * time.Minute},
	}
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Spend: policy,
		Credential: &orchestration.AccountCredential{AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok"},
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if len(created.Account.Spend.Quota.Windows) != 1 || created.Account.Spend.Breaker == nil {
		t.Fatalf("created spend = %+v", created.Account.Spend)
	}
	id := created.Account.ID

	renamed, err := svc.UpdateAccount(ctx, &UpdateAccountRequest{TenantID: "tenant-a", AccountID: id, DisplayName: "x"})
	if err != nil || len(renamed.Account.Spend.Quota.Windows) != 1 {
		t.Fatalf("an update without spend changed it: %+v, %v", renamed, err)
	}
	cleared, err := svc.UpdateAccount(ctx, &UpdateAccountRequest{TenantID: "tenant-a", AccountID: id, Spend: &orchestration.AccountSpendPolicy{}})
	if err != nil || cleared.Account.Spend.Metered() {
		t.Fatalf("clear = %+v, %v; want unmetered", cleared, err)
	}
}

// TestAccountService_SpendPolicy_RefusedWithTheFieldNamed asserts a malformed
// window or breaker is refused at the write with the operator's field named.
func TestAccountService_SpendPolicy_RefusedWithTheFieldNamed(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := context.Background()
	tests := []struct {
		name   string
		policy orchestration.AccountSpendPolicy
		field  string
	}{
		{"a window metering nothing", orchestration.AccountSpendPolicy{Quota: orchestration.SpendLimits{
			Windows: []orchestration.QuotaWindow{{Name: "w", Size: time.Hour}}}}, "spend.quota.window.max_tokens"},
		{"a breaker with no window", orchestration.AccountSpendPolicy{Breaker: &orchestration.BreakerPolicy{FailureThreshold: 1, Cooldown: time.Minute}},
			"spend.breaker.window"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateAccount(ctx, &CreateAccountRequest{
				TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Spend: tc.policy,
				Credential: &orchestration.AccountCredential{AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok"},
			})
			var invalid *AccountInvalidError
			if !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Fatalf("CreateAccount = %v, want AccountInvalidError on %s", err, tc.field)
			}
		})
	}
}
