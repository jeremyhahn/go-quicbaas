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

package embedded

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// TestEmbeddedAccountBillingConversions is go-quicbaas#1058 on the embedded
// transport: an SDK billing reaches the service, an absent one on update is
// nil (unchanged), and a record's unconfigured billing renders as nil.
func TestEmbeddedAccountBillingConversions(t *testing.T) {
	billing := &accountSDK.Billing{Mode: accountSDK.BillingAPI}
	if got := accountBillingToOrchestration(billing); got.Mode != orchestration.BillingAPI {
		t.Fatalf("to orchestration = %+v, want api", got)
	}
	if got := accountBillingToOrchestration(nil); got != (orchestration.AccountBilling{}) {
		t.Fatalf("nil billing = %+v, want not configured", got)
	}
	if optionalAccountBilling(nil) != nil {
		t.Fatal("an update without billing must leave it unchanged")
	}
	if got := optionalAccountBilling(&accountSDK.Billing{}); got == nil || *got != (orchestration.AccountBilling{}) {
		t.Fatalf("an empty billing on update = %+v, want a present clear", got)
	}
	if accountBillingFromOrchestration(orchestration.AccountBilling{}) != nil {
		t.Fatal("an unconfigured billing must render as nil")
	}
	sub := orchestration.AccountBilling{Mode: orchestration.BillingSubscription, MonthlyFeeMicros: 7}
	if got := accountInfoFromOrchestration(&orchestration.Account{ID: "a1", Billing: sub}).Billing; got == nil || got.MonthlyFeeMicros != 7 {
		t.Fatalf("record billing = %+v, want the subscription", got)
	}
}

// TestEmbeddedAccountSpendConversions is go-quicbaas#781 on the embedded
// transport.
func TestEmbeddedAccountSpendConversions(t *testing.T) {
	policy := &spend.AccountPolicy{
		Quota:   spend.Limits{Windows: []spend.Window{{Name: "daily", Size: 24 * time.Hour, MaxEquivalentMicros: 9}}},
		Breaker: &spend.BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: time.Minute},
	}
	stored := accountSpendPolicyToOrchestration(policy)
	back := accountSpendPolicyFromOrchestration(stored)
	if len(back.Quota.Windows) != 1 || back.Quota.Windows[0] != policy.Quota.Windows[0] || *back.Breaker != *policy.Breaker {
		t.Fatalf("round trip = %+v, want %+v", back, policy)
	}
	if accountSpendPolicyToOrchestration(nil).Metered() {
		t.Fatal("nil must be unmetered")
	}
	if optionalAccountSpendPolicyToOrchestration(nil) != nil {
		t.Fatal("an update without spend must leave it unchanged")
	}
	if got := accountInfoFromOrchestration(&orchestration.Account{ID: "a1", Spend: stored}).Spend; got.Breaker == nil {
		t.Fatalf("record spend = %+v", got)
	}
}
