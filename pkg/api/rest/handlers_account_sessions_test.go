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

// captureUpdate sends body to the update route and returns what the service
// was asked.
func captureUpdate(t *testing.T, body map[string]interface{}) *service.UpdateAccountRequest {
	t.Helper()
	var captured *service.UpdateAccountRequest
	srv := newAccountTestServer(&mocks.MockAccountService{
		UpdateAccountFunc: func(_ context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			captured = req
			account := sampleAccount()
			if req.MaxConcurrentSessions != nil {
				account.MaxConcurrentSessions = *req.MaxConcurrentSessions
			}
			return &service.UpdateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
		},
	})
	w := doAccountRequest(srv, http.MethodPut, "/api/v1/agent-accounts/acct-1?tenant_id=tenant-1", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp AccountEnvelopeRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req := captured; req != nil && req.MaxConcurrentSessions != nil &&
		resp.Account.MaxConcurrentSessions != *req.MaxConcurrentSessions {
		t.Fatalf("response cap = %d, want %d", resp.Account.MaxConcurrentSessions, *req.MaxConcurrentSessions)
	}
	return captured
}

func TestHandleUpdateAccount_SessionCap(t *testing.T) {
	if req := captureUpdate(t, map[string]interface{}{"display_name": "seat"}); req.MaxConcurrentSessions != nil {
		t.Fatalf("a body without the cap sent %d", *req.MaxConcurrentSessions)
	}
	req := captureUpdate(t, map[string]interface{}{"max_concurrent_sessions": 0})
	if req.MaxConcurrentSessions == nil || *req.MaxConcurrentSessions != 0 {
		t.Fatalf("an explicit 0 must reach the service as unmetered: %+v", req.MaxConcurrentSessions)
	}
	req = captureUpdate(t, map[string]interface{}{"max_concurrent_sessions": 3})
	if req.MaxConcurrentSessions == nil || *req.MaxConcurrentSessions != 3 {
		t.Fatalf("cap = %v, want 3", req.MaxConcurrentSessions)
	}
}

func TestHandleCreateAccount_SessionCap(t *testing.T) {
	var captured *service.CreateAccountRequest
	srv := newAccountTestServer(&mocks.MockAccountService{
		CreateAccountFunc: func(_ context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			captured = req
			return &service.CreateAccountResponse{Account: sampleAccount(), Timestamp: time.Now().UTC()}, nil
		},
	})
	w := doAccountRequest(srv, http.MethodPost, "/api/v1/agent-accounts?tenant_id=tenant-1", map[string]interface{}{
		"tenant_id": "tenant-1", "provider": "claude", "max_concurrent_sessions": 2,
		"credential": map[string]interface{}{"access_token": "x"},
	})
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	if captured == nil || captured.MaxConcurrentSessions != 2 {
		t.Fatalf("cap not propagated: %+v", captured)
	}
}

func TestHandleGetAccountUsage_ReportsLiveSessions(t *testing.T) {
	srv := newAccountTestServer(&mocks.MockAccountService{
		GetAccountUsageFunc: func(context.Context, *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			return &service.GetAccountUsageResponse{
				Usage: &orchestration.UsageReport{TotalTokens: 10}, LiveSessions: 2, Timestamp: time.Now().UTC(),
			}, nil
		},
	})
	w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts/acct-1/usage?tenant_id=tenant-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp GetAccountUsageRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LiveSessions != 2 {
		t.Fatalf("live_sessions = %d, want 2", resp.LiveSessions)
	}
}

// TestHandleGetAccountUsage_CarriesTheSpendVerdict asserts the spend gate's
// verdict — what a worker daemon fails over on — reaches the REST body.
func TestHandleGetAccountUsage_CarriesTheSpendVerdict(t *testing.T) {
	srv := newAccountTestServer(&mocks.MockAccountService{
		GetAccountUsageFunc: func(context.Context, *service.GetAccountUsageRequest) (*service.GetAccountUsageResponse, error) {
			return &service.GetAccountUsageResponse{
				Usage:     &orchestration.UsageReport{},
				Spend:     &orchestration.SpendHeadroom{Refusal: orchestration.SpendRefusalQuota, Window: "weekly"},
				Timestamp: time.Now().UTC(),
			}, nil
		},
	})
	w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts/acct-1/usage?tenant_id=tenant-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp GetAccountUsageRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Spend == nil || resp.Spend.Admits || resp.Spend.Window != "weekly" {
		t.Fatalf("spend = %+v, want the spent weekly window", resp.Spend)
	}
}
