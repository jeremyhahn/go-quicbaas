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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// emptySessionStore has no session records; every slot is a reservation.
type emptySessionStore struct{}

func (emptySessionStore) ListSessions(context.Context, string) ([]*orchestration.SessionRecord, error) {
	return nil, nil
}

func reservingService(t *testing.T) (*accountServiceImpl, *orchestration.Account) {
	t.Helper()
	svc := newTestAccountService(t).(*accountServiceImpl)
	table, err := orchestration.NewAccountSessionCounter(emptySessionStore{})
	if err != nil {
		t.Fatalf("NewAccountSessionCounter: %v", err)
	}
	svc.SetSessionReserver(table)
	return svc, createCappedAccount(t, svc, 1)
}

func TestAccountService_ReserveHonoursTheStoredCap(t *testing.T) {
	svc, account := reservingService(t)
	ctx := context.Background()

	first, err := svc.ReserveAccountSession(ctx, &ReserveAccountSessionRequest{TenantID: "tenant-a", AccountID: account.ID})
	if err != nil || first.ReservationID == "" {
		t.Fatalf("ReserveAccountSession = (%+v, %v), want a reservation", first, err)
	}
	if _, err := svc.ReserveAccountSession(ctx, &ReserveAccountSessionRequest{TenantID: "tenant-a", AccountID: account.ID}); !errors.Is(err, orchestration.ErrAccountsSaturated) {
		t.Fatalf("second ReserveAccountSession error = %v, want saturated", err)
	}
	released, err := svc.ReleaseAccountSession(ctx, &ReleaseAccountSessionRequest{
		TenantID: "tenant-a", AccountID: account.ID, ReservationID: first.ReservationID,
	})
	if err != nil || released.ReservationID != first.ReservationID {
		t.Fatalf("ReleaseAccountSession = (%+v, %v)", released, err)
	}
	if _, err := svc.ReserveAccountSession(ctx, &ReserveAccountSessionRequest{TenantID: "tenant-a", AccountID: account.ID}); err != nil {
		t.Fatalf("ReserveAccountSession after release: %v", err)
	}
}

func TestAccountService_ReserveCountsUnmeteredAccountsAndHonoursTheExpectedCount(t *testing.T) {
	svc, _ := reservingService(t)
	unmetered := createCappedAccount(t, svc, 0)
	ctx := context.Background()
	zero := 0
	req := &ReserveAccountSessionRequest{TenantID: "tenant-a", AccountID: unmetered.ID, ExpectedLive: &zero}
	if resp, err := svc.ReserveAccountSession(ctx, req); err != nil || resp.ReservationID == "" {
		t.Fatalf("first reserve = (%+v, %v), want an unmetered account reserved", resp, err)
	}
	if _, err := svc.ReserveAccountSession(ctx, req); !errors.Is(err, orchestration.ErrAccountSessionCountChanged) {
		t.Fatalf("second reserve at a stale count = %v, want the count-changed refusal", err)
	}
}

func TestAccountService_ReservationRejectsBadInput(t *testing.T) {
	svc, account := reservingService(t)
	ctx := context.Background()
	var invalid *AccountInvalidError
	for name, call := range map[string]func() error{
		"reserve nil": func() error { _, err := svc.ReserveAccountSession(ctx, nil); return err },
		"reserve no account": func() error {
			_, err := svc.ReserveAccountSession(ctx, &ReserveAccountSessionRequest{TenantID: "tenant-a"})
			return err
		},
		"release nil": func() error { _, err := svc.ReleaseAccountSession(ctx, nil); return err },
		"release no reservation": func() error {
			_, err := svc.ReleaseAccountSession(ctx, &ReleaseAccountSessionRequest{TenantID: "tenant-a", AccountID: account.ID})
			return err
		},
	} {
		if err := call(); !errors.As(err, &invalid) {
			t.Errorf("%s: error = %v, want AccountInvalidError", name, err)
		}
	}
	var notFound *AccountNotFoundError
	if _, err := svc.ReserveAccountSession(ctx, &ReserveAccountSessionRequest{TenantID: "tenant-a", AccountID: "nope"}); !errors.As(err, &notFound) {
		t.Errorf("reserve unknown account: error = %v, want not found", err)
	}
}

func TestAccountService_ReservationWithoutATableIsUnavailable(t *testing.T) {
	svc := newTestAccountService(t)
	var notConfigured *AccountSessionReserverNotConfiguredError
	if _, err := svc.ReserveAccountSession(context.Background(), &ReserveAccountSessionRequest{TenantID: "t", AccountID: "a"}); !errors.As(err, &notConfigured) {
		t.Errorf("Reserve error = %v, want not configured", err)
	}
	if _, err := svc.ReleaseAccountSession(context.Background(), &ReleaseAccountSessionRequest{TenantID: "t", AccountID: "a", ReservationID: "r"}); !errors.As(err, &notConfigured) {
		t.Errorf("Release error = %v, want not configured", err)
	}
	if notConfigured.Error() == "" {
		t.Error("the error must say what is missing")
	}
}
