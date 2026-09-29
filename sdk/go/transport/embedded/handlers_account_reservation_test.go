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
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

func TestAccountHandlers_ReserveAndReleaseSession(t *testing.T) {
	var reserved *service.ReserveAccountSessionRequest
	var released *service.ReleaseAccountSessionRequest
	tr := newAccountTransport(t, &mocks.MockAccountService{
		ReserveAccountSessionFunc: func(_ context.Context, req *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			reserved = req
			return &service.ReserveAccountSessionResponse{ReservationID: "r1"}, nil
		},
		ReleaseAccountSessionFunc: func(_ context.Context, req *service.ReleaseAccountSessionRequest) (*service.ReleaseAccountSessionResponse, error) {
			released = req
			return &service.ReleaseAccountSessionResponse{ReservationID: req.ReservationID}, nil
		},
	})
	ctx := context.Background()

	resp, err := tr.Request(ctx, accountServiceName, "ReserveSession",
		&accountSDK.ReserveSessionRequest{TenantID: "tenant-1", AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("ReserveSession: %v", err)
	}
	if r, ok := resp.(*accountSDK.SessionReservation); !ok || r.ReservationID != "r1" {
		t.Fatalf("ReserveSession = %#v", resp)
	}
	if reserved == nil || reserved.TenantID != "tenant-1" {
		t.Fatalf("service asked %+v", reserved)
	}
	if _, err := tr.Request(ctx, accountServiceName, "ReleaseSession",
		&accountSDK.ReleaseSessionRequest{TenantID: "tenant-1", AccountID: "acct-1", ReservationID: "r1"}); err != nil {
		t.Fatalf("ReleaseSession: %v", err)
	}
	if released == nil || released.ReservationID != "r1" {
		t.Fatalf("service asked %+v", released)
	}
}

// TestAccountHandlers_ReservationsAreBoundToTheSubject is the round-3 P1 guard
// in process: with a tenant-scoped subject on the context, a request naming
// another tenant is refused and an omitted one is the subject's.
func TestAccountHandlers_ReservationsAreBoundToTheSubject(t *testing.T) {
	var reserved *service.ReserveAccountSessionRequest
	reached := false
	tr := newAccountTransport(t, &mocks.MockAccountService{
		ReserveAccountSessionFunc: func(_ context.Context, req *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			reserved = req
			return &service.ReserveAccountSessionResponse{ReservationID: "r1"}, nil
		},
		ReleaseAccountSessionFunc: func(_ context.Context, req *service.ReleaseAccountSessionRequest) (*service.ReleaseAccountSessionResponse, error) {
			reached = true
			return &service.ReleaseAccountSessionResponse{}, nil
		},
	})
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "agent", TenantID: "tenant-1", Authenticated: true})

	if _, err := tr.Request(ctx, accountServiceName, "ReserveSession",
		&accountSDK.ReserveSessionRequest{TenantID: "tenant-2", AccountID: "b"}); err == nil || reserved != nil {
		t.Fatalf("cross-tenant reserve = %v (service asked %+v), want a refusal", err, reserved)
	}
	if _, err := tr.Request(ctx, accountServiceName, "ReleaseSession",
		&accountSDK.ReleaseSessionRequest{TenantID: "tenant-2", AccountID: "b", ReservationID: "r"}); err == nil || reached {
		t.Fatalf("cross-tenant release = %v, want a refusal", err)
	}
	if _, err := tr.Request(ctx, accountServiceName, "ReserveSession",
		&accountSDK.ReserveSessionRequest{AccountID: "a"}); err != nil || reserved == nil || reserved.TenantID != "tenant-1" {
		t.Fatalf("reserve with no tenant = %v (asked %+v), want the subject's tenant", err, reserved)
	}
}

func TestAccountHandlers_ReservationRejectsTheWrongRequestType(t *testing.T) {
	tr := newAccountTransport(t, &mocks.MockAccountService{})
	for _, method := range []string{"ReserveSession", "ReleaseSession"} {
		if _, err := tr.Request(context.Background(), accountServiceName, method, "not-a-request"); err == nil {
			t.Errorf("%s accepted a wrong-typed request", method)
		}
	}
}
