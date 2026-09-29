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

package main

import (
	"context"
	"testing"
	"time"

	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// TestRunAgentAccountUpdate_SpendFlagsChangeOnlyWhatTheyName is
// go-quicbaas#781 at the CLI: new windows are applied to the account's CURRENT
// policy, so its breaker survives.
func TestRunAgentAccountUpdate_SpendFlagsChangeOnlyWhatTheyName(t *testing.T) {
	var sent *accountSDK.UpdateAccountRequest
	mc := &mockAccountClient{
		getFn: func(context.Context, string, string) (*accountSDK.AccountInfo, error) {
			return &accountSDK.AccountInfo{ID: "acct-1", Spend: spend.AccountPolicy{
				Breaker: &spend.BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: time.Minute},
			}}, nil
		},
		updateFn: func(_ context.Context, req *accountSDK.UpdateAccountRequest) (*accountSDK.AccountInfo, error) {
			sent = req
			return &accountSDK.AccountInfo{ID: "acct-1"}, nil
		},
	}
	withMockAccount(t, mc)
	cmd := newAccountUpdateCommand()
	spendWindowValues, spendClear = nil, false
	if err := cmd.Flags().Set("spend-window", "name=five-hour,size=5h,tokens=2000000"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := runAccountUpdate(cmd, []string{"acct-1"}); err != nil {
		t.Fatalf("runAccountUpdate: %v", err)
	}
	if sent == nil || sent.Spend == nil || len(sent.Spend.Quota.Windows) != 1 || sent.Spend.Breaker == nil {
		t.Fatalf("sent spend = %+v, want the new window and the kept breaker", sent)
	}
}

// TestRunAgentAccountUpdate_NoSpendFlagsSendsNoSpend asserts an update that
// names no spend flag neither reads nor sends a policy.
func TestRunAgentAccountUpdate_NoSpendFlagsSendsNoSpend(t *testing.T) {
	var sent *accountSDK.UpdateAccountRequest
	mc := &mockAccountClient{
		getFn: func(context.Context, string, string) (*accountSDK.AccountInfo, error) {
			t.Fatal("the account was read though no spend flag was given")
			return nil, nil
		},
		updateFn: func(_ context.Context, req *accountSDK.UpdateAccountRequest) (*accountSDK.AccountInfo, error) {
			sent = req
			return &accountSDK.AccountInfo{ID: "acct-1"}, nil
		},
	}
	withMockAccount(t, mc)
	cmd := newAccountUpdateCommand()
	spendWindowValues, spendClear = nil, false
	if err := runAccountUpdate(cmd, []string{"acct-1"}); err != nil {
		t.Fatalf("runAccountUpdate: %v", err)
	}
	if sent == nil || sent.Spend != nil {
		t.Fatalf("sent = %+v, want no spend", sent)
	}
}
