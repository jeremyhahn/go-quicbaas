//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestAccountBillingOverMCP is go-quicbaas#1058 over MCP: billing is set on
// create and replaced on update only when the caller sends it.
func TestAccountBillingOverMCP(t *testing.T) {
	var created *service.CreateAccountRequest
	var updates []*service.UpdateAccountRequest
	reg := newAccountRegistry(t, &mocks.MockAccountService{
		CreateAccountFunc: func(_ context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			created = req
			return &service.CreateAccountResponse{Account: mcpSampleAccount()}, nil
		},
		UpdateAccountFunc: func(_ context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			updates = append(updates, req)
			return &service.UpdateAccountResponse{Account: mcpSampleAccount()}, nil
		},
	})
	ctx := context.Background()

	if _, err := reg.ExecuteTool(ctx, "quicbaas_agent_account_create", map[string]interface{}{
		"tenant_id": "t1", "provider": "claude",
		"credential": map[string]interface{}{"access_token": "tok", "access_token_env_key": "CLAUDE_CODE_OAUTH_TOKEN"},
		"billing":    map[string]interface{}{"mode": "subscription", "monthly_fee_micros": float64(200_000_000)},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	want := orchestration.AccountBilling{Mode: orchestration.BillingSubscription, MonthlyFeeMicros: 200_000_000}
	if created == nil || created.Billing != want {
		t.Fatalf("service saw %+v, want %+v", created, want)
	}

	for _, args := range []map[string]interface{}{
		{"tenant_id": "t1", "account_id": "acct-1", "display_name": "x"},
		{"tenant_id": "t1", "account_id": "acct-1", "billing": map[string]interface{}{"mode": "api"}},
	} {
		if _, err := reg.ExecuteTool(ctx, "quicbaas_agent_account_update", args); err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	if updates[0].Billing != nil {
		t.Fatalf("an update without billing sent %+v", updates[0].Billing)
	}
	if updates[1].Billing == nil || updates[1].Billing.Mode != orchestration.BillingAPI {
		t.Fatalf("update billing = %+v, want api", updates[1].Billing)
	}
}

// TestAccountBillingUpdateRefusesAMalformedBilling is the go-quicbaas#1058
// review regression: a billing of the wrong shape used to read as "not
// configured", and an update then replaced the saved billing with nothing.
// Every malformed shape must be refused before the service is called.
func TestAccountBillingUpdateRefusesAMalformedBilling(t *testing.T) {
	cases := map[string]struct {
		billing interface{}
		field   string
	}{
		"not an object":       {"subscription", "billing"},
		"mode not a string":   {map[string]interface{}{"mode": float64(1)}, "billing.mode"},
		"fee not a number":    {map[string]interface{}{"mode": "api", "monthly_fee_micros": "lots"}, "billing.monthly_fee_micros"},
		"fee not whole":       {map[string]interface{}{"mode": "api", "monthly_fee_micros": 1.5}, "billing.monthly_fee_micros"},
		"fee out of range":    {map[string]interface{}{"mode": "api", "monthly_fee_micros": 1e300}, "billing.monthly_fee_micros"},
		"array not an object": {[]interface{}{"api"}, "billing"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			called := false
			reg := newAccountRegistry(t, &mocks.MockAccountService{
				UpdateAccountFunc: func(_ context.Context, _ *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
					called = true
					return &service.UpdateAccountResponse{Account: mcpSampleAccount()}, nil
				},
				CreateAccountFunc: func(_ context.Context, _ *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
					called = true
					return &service.CreateAccountResponse{Account: mcpSampleAccount()}, nil
				},
			})
			ctx := context.Background()

			_, err := reg.ExecuteTool(ctx, "quicbaas_agent_account_update", map[string]interface{}{
				"tenant_id": "t1", "account_id": "acct-1", "billing": tc.billing,
			})
			assertBillingArgumentError(t, err, tc.field)

			_, err = reg.ExecuteTool(ctx, "quicbaas_agent_account_create", map[string]interface{}{
				"tenant_id": "t1", "provider": "claude",
				"credential": map[string]interface{}{"access_token": "tok", "access_token_env_key": "CLAUDE_CODE_OAUTH_TOKEN"},
				"billing":    tc.billing,
			})
			assertBillingArgumentError(t, err, tc.field)

			if called {
				t.Fatal("the service was called with a malformed billing; the saved billing would be replaced")
			}
		})
	}
}

// TestAccountBillingFromArgsReadsAWholeFee asserts the accepted shapes decode
// exactly, including a null billing read as absent.
func TestAccountBillingFromArgsReadsAWholeFee(t *testing.T) {
	got, err := accountBillingFromArgs(map[string]interface{}{"mode": "subscription", "monthly_fee_micros": float64(20_000_000)})
	if err != nil {
		t.Fatalf("accountBillingFromArgs: %v", err)
	}
	if want := (orchestration.AccountBilling{Mode: orchestration.BillingSubscription, MonthlyFeeMicros: 20_000_000}); got != want {
		t.Fatalf("billing = %+v, want %+v", got, want)
	}
	absent, err := optionalAccountBillingFromArgs(map[string]interface{}{"billing": nil})
	if err != nil || absent != nil {
		t.Fatalf("a null billing = %+v, %v; want nil, nil (unchanged)", absent, err)
	}
}

func assertBillingArgumentError(t *testing.T, err error, field string) {
	t.Helper()
	var argErr *AccountBillingArgumentError
	if !errors.As(err, &argErr) || argErr.Field != field {
		t.Fatalf("want an AccountBillingArgumentError naming %s, got %v", field, err)
	}
}
