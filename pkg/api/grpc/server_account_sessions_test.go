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

func TestAccountServer_UpdateCarriesTheSessionCapOnlyWhenSet(t *testing.T) {
	var captured []*service.UpdateAccountRequest
	server := newAccountServer(&mocks.MockAccountService{
		UpdateAccountFunc: func(_ context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			captured = append(captured, req)
			account := grpcSampleAccount()
			account.MaxConcurrentSessions = 4
			return &service.UpdateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
	})
	ctx := context.Background()

	if _, err := server.UpdateAccount(ctx, &pb.UpdateAccountRequest{TenantId: "t", AccountId: "a", DisplayName: "x"}); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	four := int32(4)
	resp, err := server.UpdateAccount(ctx, &pb.UpdateAccountRequest{TenantId: "t", AccountId: "a", MaxConcurrentSessions: &four})
	if err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if captured[0].MaxConcurrentSessions != nil {
		t.Errorf("an absent cap reached the service as %d", *captured[0].MaxConcurrentSessions)
	}
	if captured[1].MaxConcurrentSessions == nil || *captured[1].MaxConcurrentSessions != 4 {
		t.Errorf("cap = %v, want 4", captured[1].MaxConcurrentSessions)
	}
	if resp.Account.GetMaxConcurrentSessions() != 4 {
		t.Errorf("response cap = %d, want 4", resp.Account.GetMaxConcurrentSessions())
	}
}

func TestAccountServer_UsageReportsLiveSessions(t *testing.T) {
	server := newAccountServer(&mocks.MockAccountService{
		GetAccountUsageFunc: func(context.Context, *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			return &service.GetAccountUsageResponse{
				Usage: &orchestration.UsageReport{TotalTokens: 5}, LiveSessions: 3, Timestamp: time.Now().UTC(),
			}, nil
		},
	})
	resp, err := server.GetAccountUsage(context.Background(), &pb.GetAccountUsageRequest{TenantId: "t", AccountId: "a"})
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	if resp.GetLiveSessions() != 3 {
		t.Fatalf("live_sessions = %d, want 3", resp.GetLiveSessions())
	}
}

// TestAccountServer_UsageCarriesTheSpendVerdict asserts the spend gate's
// verdict crosses the wire, and stays unset when no gate is wired.
func TestAccountServer_UsageCarriesTheSpendVerdict(t *testing.T) {
	freesAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	verdict := &orchestration.SpendHeadroom{Refusal: orchestration.SpendRefusalBreaker, Failures: 3, FreesAt: &freesAt}
	server := newAccountServer(&mocks.MockAccountService{
		GetAccountUsageFunc: func(_ context.Context, req *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			resp := &service.GetAccountUsageResponse{Usage: &orchestration.UsageReport{}, Timestamp: time.Now().UTC()}
			if req.AccountID == "spent" {
				resp.Spend = verdict
			}
			return resp, nil
		},
	})
	resp, err := server.GetAccountUsage(context.Background(), &pb.GetAccountUsageRequest{TenantId: "t", AccountId: "spent"})
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	spend := resp.GetSpend()
	if spend == nil || spend.GetAdmits() || spend.GetRefusal() != orchestration.SpendRefusalBreaker ||
		spend.GetFailures() != 3 || !spend.GetFreesAt().AsTime().Equal(freesAt) {
		t.Fatalf("spend = %+v, want the open breaker", spend)
	}
	resp, err = server.GetAccountUsage(context.Background(), &pb.GetAccountUsageRequest{TenantId: "t", AccountId: "free"})
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	if resp.GetSpend() != nil {
		t.Fatalf("spend = %+v with no gate wired, want unset", resp.GetSpend())
	}
}

func TestOptionalInt(t *testing.T) {
	if optionalInt(nil) != nil {
		t.Fatal("nil must stay nil: unchanged")
	}
	zero := int32(0)
	if got := optionalInt(&zero); got == nil || *got != 0 {
		t.Fatalf("optionalInt(&0) = %v, want a pointer to 0", got)
	}
}
