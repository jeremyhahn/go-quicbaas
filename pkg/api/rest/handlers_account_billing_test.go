//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestAccountBillingOverREST is go-quicbaas#1058 over REST: billing is set on
// create, replaced on update only when present, and rendered on the record.
func TestAccountBillingOverREST(t *testing.T) {
	var created *service.CreateAccountRequest
	var updates []*service.UpdateAccountRequest
	account := sampleAccount()
	account.Billing = orchestration.AccountBilling{Mode: orchestration.BillingSubscription, MonthlyFeeMicros: 200_000_000}
	srv := newAccountTestServer(&mocks.MockAccountService{
		CreateAccountFunc: func(_ context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			created = req
			return &service.CreateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
		UpdateAccountFunc: func(_ context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			updates = append(updates, req)
			return &service.UpdateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
	})

	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts?tenant_id=tenant-1", map[string]interface{}{
		"tenant_id": "tenant-1", "provider": "claude", "credential": sampleCredentialBody(),
		"billing": map[string]interface{}{"mode": "subscription", "monthly_fee_micros": 200_000_000},
	})
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", w.Code, w.Body.String())
	}
	if created == nil || created.Billing != account.Billing {
		t.Fatalf("service saw %+v, want the subscription", created)
	}
	var body struct {
		Account struct {
			Billing *AccountBillingREST `json:"billing"`
		} `json:"account"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Account.Billing == nil || body.Account.Billing.Mode != "subscription" || body.Account.Billing.MonthlyFeeMicros != 200_000_000 {
		t.Fatalf("record billing = %+v, want the subscription", body.Account.Billing)
	}

	for _, update := range []map[string]interface{}{
		{"display_name": "x"},
		{"billing": map[string]interface{}{"mode": "api"}},
	} {
		if w := doAccountRequest(srv, http.MethodPut, "/api/v1/agent-accounts/acct-1?tenant_id=tenant-1", update); w.Code != http.StatusOK {
			t.Fatalf("update status = %d; body=%s", w.Code, w.Body.String())
		}
	}
	if updates[0].Billing != nil {
		t.Fatalf("an update without billing sent %+v", updates[0].Billing)
	}
	if updates[1].Billing == nil || updates[1].Billing.Mode != orchestration.BillingAPI {
		t.Fatalf("update billing = %+v, want api", updates[1].Billing)
	}
}

// TestAccountToREST_UnconfiguredBillingIsAbsent asserts the record carries no
// billing object for an account nobody configured.
func TestAccountToREST_UnconfiguredBillingIsAbsent(t *testing.T) {
	if got := accountToREST(sampleAccount()).Billing; got != nil {
		t.Fatalf("billing = %+v, want absent", got)
	}
}

// TestAccountSpendOverREST is go-quicbaas#781 over REST: the spend policy is
// set on create, replaced on update only when present, and rendered on the
// record.
func TestAccountSpendOverREST(t *testing.T) {
	var created *service.CreateAccountRequest
	var updates []*service.UpdateAccountRequest
	account := sampleAccount()
	account.Spend = orchestration.AccountSpendPolicy{Quota: orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{
		{Name: "daily", Size: 24 * time.Hour, MaxEquivalentMicros: 50_000_000},
	}}}
	srv := newAccountTestServer(&mocks.MockAccountService{
		CreateAccountFunc: func(_ context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			created = req
			return &service.CreateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
		UpdateAccountFunc: func(_ context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			updates = append(updates, req)
			return &service.UpdateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
	})
	spendBody := map[string]interface{}{"quota": map[string]interface{}{"windows": []interface{}{
		map[string]interface{}{"name": "daily", "size": int64(24 * time.Hour), "max_equivalent_micros": 50_000_000},
	}}}
	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts?tenant_id=tenant-1", map[string]interface{}{
		"tenant_id": "tenant-1", "provider": "claude", "credential": sampleCredentialBody(), "spend": spendBody,
	})
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", w.Code, w.Body.String())
	}
	if created == nil || len(created.Spend.Quota.Windows) != 1 || created.Spend.Quota.Windows[0].MaxEquivalentMicros != 50_000_000 {
		t.Fatalf("service saw %+v", created)
	}
	var body struct {
		Account struct {
			Spend *orchestration.AccountSpendPolicy `json:"spend"`
		} `json:"account"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Account.Spend == nil || body.Account.Spend.Quota.Windows[0].Size != 24*time.Hour {
		t.Fatalf("record spend = %+v", body.Account.Spend)
	}
	for _, update := range []map[string]interface{}{{"display_name": "x"}, {"spend": map[string]interface{}{}}} {
		if w := doAccountRequest(srv, http.MethodPut, "/api/v1/agent-accounts/acct-1?tenant_id=tenant-1", update); w.Code != http.StatusOK {
			t.Fatalf("update status = %d; body=%s", w.Code, w.Body.String())
		}
	}
	if updates[0].Spend != nil || updates[1].Spend == nil || updates[1].Spend.Metered() {
		t.Fatalf("updates = %+v / %+v, want absent then a present unmetered policy", updates[0].Spend, updates[1].Spend)
	}
	if accountToREST(sampleAccount()).Spend != nil {
		t.Fatal("an unmetered account must render no spend")
	}
}
