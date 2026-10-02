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
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestAccountTools_ReservationsTakeTenancyFromTheCaller(t *testing.T) {
	var reserved *service.ReserveAccountSessionRequest
	var released *service.ReleaseAccountSessionRequest
	reg := newAccountRegistry(t, &mocks.MockAccountService{
		ReserveAccountSessionFunc: func(_ context.Context, req *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			reserved = req
			return &service.ReserveAccountSessionResponse{ReservationID: "r1"}, nil
		},
		ReleaseAccountSessionFunc: func(_ context.Context, req *service.ReleaseAccountSessionRequest) (*service.ReleaseAccountSessionResponse, error) {
			released = req
			return &service.ReleaseAccountSessionResponse{ReservationID: req.ReservationID}, nil
		},
	})
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	if _, err := reg.executeAccountTool(ctx, "quicbaas_agent_account_reserve_session", map[string]interface{}{
		"account_id": "acct-1", "expected_live": float64(1),
	}); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if reserved == nil || reserved.TenantID != "tenant-1" || reserved.ExpectedLive == nil || *reserved.ExpectedLive != 1 {
		t.Fatalf("reserve asked %+v, want the caller's tenant and the expected count", reserved)
	}
	if _, err := reg.executeAccountTool(ctx, "quicbaas_agent_account_release_session", map[string]interface{}{
		"account_id": "acct-1", "reservation_id": "r1",
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if released == nil || released.TenantID != "tenant-1" || released.ReservationID != "r1" {
		t.Fatalf("release asked %+v", released)
	}
}

// TestAccountTools_ReservationsRefuseAnotherTenant is the round-3 P1 guard: a
// tenant_id argument naming another tenant is refused, not silently re-pointed.
func TestAccountTools_ReservationsRefuseAnotherTenant(t *testing.T) {
	reached := false
	reg := newAccountRegistry(t, &mocks.MockAccountService{
		ReserveAccountSessionFunc: func(context.Context, *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			reached = true
			return &service.ReserveAccountSessionResponse{}, nil
		},
		ReleaseAccountSessionFunc: func(_ context.Context, req *service.ReleaseAccountSessionRequest) (*service.ReleaseAccountSessionResponse, error) {
			reached = true
			return &service.ReleaseAccountSessionResponse{}, nil
		},
	})
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")
	for _, tool := range []string{"quicbaas_agent_account_reserve_session", "quicbaas_agent_account_release_session"} {
		if _, err := reg.executeAccountTool(ctx, tool, map[string]interface{}{
			"tenant_id": "tenant-2", "account_id": "b-acct", "reservation_id": "r",
		}); err == nil {
			t.Errorf("%s reached another tenant", tool)
		}
	}
	if reached {
		t.Fatal("a cross-tenant call reached the service")
	}
}

func TestAccountTools_ReservationsRefuseACallerWithNoTenant(t *testing.T) {
	reg := newAccountRegistry(t, &mocks.MockAccountService{})
	for _, tool := range []string{"quicbaas_agent_account_reserve_session", "quicbaas_agent_account_release_session"} {
		if _, err := reg.executeAccountTool(context.Background(), tool, map[string]interface{}{"account_id": "a", "reservation_id": "r"}); err == nil {
			t.Errorf("%s answered a caller with no tenant binding", tool)
		}
	}
}
