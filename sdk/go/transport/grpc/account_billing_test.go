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

package grpc

import (
	"testing"
	"time"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// TestAccountBillingProtoRoundTrip is go-quicbaas#1058 on the SDK's gRPC
// transport: billing crosses in both directions, and an absent one stays
// absent — which on an update is what leaves the account's billing alone.
func TestAccountBillingProtoRoundTrip(t *testing.T) {
	billing := &account.Billing{Mode: account.BillingSubscription, MonthlyFeeMicros: 200_000_000}
	wire := accountBillingToProto(billing)
	if wire.GetMode() != "subscription" || wire.GetMonthlyFeeMicros() != 200_000_000 {
		t.Fatalf("wire = %+v, want the subscription", wire)
	}
	if back := accountBillingFromProto(wire); back == nil || *back != *billing {
		t.Fatalf("round trip = %+v, want %+v", back, billing)
	}
	if accountBillingToProto(nil) != nil || accountBillingFromProto(nil) != nil {
		t.Fatal("an absent billing must stay absent in both directions")
	}
	if got := accountFromProto(&pb.Account{Id: "a1", Billing: wire}).Billing; got == nil || got.Mode != account.BillingSubscription {
		t.Fatalf("record billing = %+v, want the subscription", got)
	}
}

// TestAccountSpendPolicyProtoConversions is go-quicbaas#781 on the SDK's gRPC
// transport: windows and breaker cross both ways, and nil stays absent.
func TestAccountSpendPolicyProtoConversions(t *testing.T) {
	policy := &spend.AccountPolicy{
		Quota:   spend.Limits{Windows: []spend.Window{{Name: "daily", Size: 24 * time.Hour, MaxTokens: 5}}},
		Breaker: &spend.BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: time.Minute},
	}
	back := accountSpendPolicyFromProto(accountSpendPolicyToProto(policy))
	if len(back.Quota.Windows) != 1 || back.Quota.Windows[0] != policy.Quota.Windows[0] || *back.Breaker != *policy.Breaker {
		t.Fatalf("round trip = %+v, want %+v", back, policy)
	}
	if accountSpendPolicyToProto(nil) != nil {
		t.Fatal("nil must stay absent")
	}
	if cleared := accountSpendPolicyToProto(&spend.AccountPolicy{}); cleared == nil {
		t.Fatal("an empty policy on update must be a present clear")
	}
	if got := accountFromProto(&pb.Account{Id: "a1", Spend: accountSpendPolicyToProto(policy)}).Spend; got.Breaker == nil {
		t.Fatalf("record spend = %+v", got)
	}
}
