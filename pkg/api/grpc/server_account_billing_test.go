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
	"context"
	"testing"
	"time"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestAccountServer_BillingCrossesTheWire is go-quicbaas#1058 over gRPC: an
// account's billing is set on create, replaced on update only when present,
// and rendered on the record.
func TestAccountServer_BillingCrossesTheWire(t *testing.T) {
	var created *service.CreateAccountRequest
	var updates []*service.UpdateAccountRequest
	account := grpcSampleAccount()
	account.Billing = orchestration.AccountBilling{Mode: orchestration.BillingSubscription, MonthlyFeeMicros: 200_000_000}
	svc := &mocks.MockAccountService{
		CreateAccountFunc: func(_ context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			created = req
			return &service.CreateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
		UpdateAccountFunc: func(_ context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			updates = append(updates, req)
			return &service.UpdateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
	}
	srv := newAccountServer(svc)
	ctx := context.Background()

	resp, err := srv.CreateAccount(ctx, &pb.CreateAccountRequest{
		TenantId: "tenant-1", Provider: "claude", Credential: grpcSampleCredential(),
		Billing: &pb.AccountBilling{Mode: "subscription", MonthlyFeeMicros: 200_000_000},
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if created.Billing != account.Billing {
		t.Fatalf("service saw billing %+v, want %+v", created.Billing, account.Billing)
	}
	if got := resp.GetAccount().GetBilling(); got.GetMode() != "subscription" || got.GetMonthlyFeeMicros() != 200_000_000 {
		t.Fatalf("record billing = %+v, want the subscription", got)
	}

	if _, err := srv.UpdateAccount(ctx, &pb.UpdateAccountRequest{TenantId: "tenant-1", AccountId: "acct-1", DisplayName: "x"}); err != nil {
		t.Fatalf("UpdateAccount(rename): %v", err)
	}
	if _, err := srv.UpdateAccount(ctx, &pb.UpdateAccountRequest{TenantId: "tenant-1", AccountId: "acct-1",
		Billing: &pb.AccountBilling{Mode: "api"}}); err != nil {
		t.Fatalf("UpdateAccount(billing): %v", err)
	}
	if updates[0].Billing != nil {
		t.Fatalf("an update without billing sent %+v; absent must leave it unchanged", updates[0].Billing)
	}
	if updates[1].Billing == nil || updates[1].Billing.Mode != orchestration.BillingAPI {
		t.Fatalf("update billing = %+v, want api", updates[1].Billing)
	}
}

// TestAccountToProto_UnconfiguredBillingIsAbsent asserts an account nobody
// configured billing for carries no billing message rather than an empty one.
func TestAccountToProto_UnconfiguredBillingIsAbsent(t *testing.T) {
	if got := accountToProto(grpcSampleAccount()).GetBilling(); got != nil {
		t.Fatalf("billing = %+v, want absent", got)
	}
}

// TestAccountServer_SpendPolicyCrossesTheWire is go-quicbaas#781 over gRPC:
// an account's spend policy — windows and breaker — is set on create,
// replaced on update only when present, and rendered on the record.
func TestAccountServer_SpendPolicyCrossesTheWire(t *testing.T) {
	policy := orchestration.AccountSpendPolicy{
		Quota: orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{
			{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 2_000_000, MaxEquivalentMicros: 30_000_000},
		}},
		Breaker: &orchestration.BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: 30 * time.Minute},
	}
	var created *service.CreateAccountRequest
	var updates []*service.UpdateAccountRequest
	account := grpcSampleAccount()
	account.Spend = policy
	srv := newAccountServer(&mocks.MockAccountService{
		CreateAccountFunc: func(_ context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			created = req
			return &service.CreateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
		UpdateAccountFunc: func(_ context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			updates = append(updates, req)
			return &service.UpdateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
	})
	ctx := context.Background()

	resp, err := srv.CreateAccount(ctx, &pb.CreateAccountRequest{
		TenantId: "tenant-1", Provider: "claude", Credential: grpcSampleCredential(),
		Spend: accountSpendPolicyToProto(policy),
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if created.Spend.Quota.Windows[0] != policy.Quota.Windows[0] || *created.Spend.Breaker != *policy.Breaker {
		t.Fatalf("service saw %+v, want %+v", created.Spend, policy)
	}
	if got := resp.GetAccount().GetSpend(); got.GetBreaker().GetFailureThreshold() != 3 || len(got.GetQuota().GetWindows()) != 1 {
		t.Fatalf("record spend = %+v", got)
	}

	for _, req := range []*pb.UpdateAccountRequest{
		{TenantId: "tenant-1", AccountId: "acct-1", DisplayName: "x"},
		{TenantId: "tenant-1", AccountId: "acct-1", Spend: &pb.AccountSpendPolicy{}},
	} {
		if _, err := srv.UpdateAccount(ctx, req); err != nil {
			t.Fatalf("UpdateAccount: %v", err)
		}
	}
	if updates[0].Spend != nil {
		t.Fatalf("an update without spend sent %+v", updates[0].Spend)
	}
	if updates[1].Spend == nil || updates[1].Spend.Metered() {
		t.Fatalf("an empty spend update = %+v, want a present unmetered policy", updates[1].Spend)
	}
	if accountSpendPolicyToProto(orchestration.AccountSpendPolicy{}) != nil {
		t.Fatal("an unmetered policy must be absent on the wire")
	}
}
