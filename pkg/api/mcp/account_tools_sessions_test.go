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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

func TestAccountTools_SessionCapReachesTheService(t *testing.T) {
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

	// JSON numbers arrive as float64.
	if _, err := reg.ExecuteTool(ctx, "quicbaas_agent_account_create", map[string]interface{}{
		"tenant_id": "tenant-1", "provider": "claude", "max_concurrent_sessions": float64(2),
		"credential": map[string]interface{}{"access_token": "x"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if created == nil || created.MaxConcurrentSessions != 2 {
		t.Fatalf("create cap not forwarded: %+v", created)
	}

	if _, err := reg.ExecuteTool(ctx, "quicbaas_agent_account_update", map[string]interface{}{
		"tenant_id": "tenant-1", "account_id": "acct-1", "display_name": "seat",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := reg.ExecuteTool(ctx, "quicbaas_agent_account_update", map[string]interface{}{
		"tenant_id": "tenant-1", "account_id": "acct-1", "max_concurrent_sessions": float64(0),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if updates[0].MaxConcurrentSessions != nil {
		t.Errorf("an omitted cap reached the service as %d", *updates[0].MaxConcurrentSessions)
	}
	if updates[1].MaxConcurrentSessions == nil || *updates[1].MaxConcurrentSessions != 0 {
		t.Errorf("an explicit 0 must reach the service as unmetered: %v", updates[1].MaxConcurrentSessions)
	}
}
