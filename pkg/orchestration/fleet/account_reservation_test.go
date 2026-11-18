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

package fleet

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// centralWithTable is a fake control plane whose /reservations routes write a
// real reservation table (every fake central has one; the name says why the
// test wants it).
func centralWithTable(t *testing.T) *fakeCentral {
	t.Helper()
	return newFakeCentral(t, "tenant-a")
}

func claim(account *orchestration.Account) orchestration.SessionReservationRequest {
	return orchestration.SessionReservationRequest{Account: account}
}

func TestAccountAPIClient_ReserveAndReleaseRoundTrip(t *testing.T) {
	central := centralWithTable(t)
	account := central.addAccount("claude1", "claude", "", true)
	account.record.MaxConcurrentSessions = 1
	client := newBalancingClient(t, central)
	ctx := context.Background()

	rid, err := client.Reserve(ctx, "tenant-a", claim(account.record))
	if err != nil || rid == "" {
		t.Fatalf("Reserve = (%q, %v), want a reservation", rid, err)
	}
	// The one slot is the control plane's now; a second host is refused.
	if _, err := client.Reserve(ctx, "tenant-a", claim(account.record)); !errors.Is(err, orchestration.ErrAccountsSaturated) {
		t.Fatalf("second Reserve error = %v, want saturated rebuilt from the 409", err)
	}
	if err := client.Release(ctx, "tenant-a", "claude1", rid); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := client.Reserve(ctx, "tenant-a", claim(account.record)); err != nil {
		t.Fatalf("Reserve after release: %v", err)
	}
}

// TestAccountAPIClient_AnUnmeteredAccountIsReservedToo: occupancy is counted
// on the control plane for every account, so balancing across hosts sees
// in-flight spawns.
func TestAccountAPIClient_AnUnmeteredAccountIsReservedToo(t *testing.T) {
	central := centralWithTable(t)
	account := central.addAccount("claude1", "claude", "", true)
	client := newBalancingClient(t, central)
	rid, err := client.Reserve(context.Background(), "tenant-a", claim(account.record))
	if err != nil || rid == "" {
		t.Fatalf("Reserve = (%q, %v), want a reservation", rid, err)
	}
	live, err := central.reserver.LiveSessions(context.Background(), "tenant-a", []string{"claude1"})
	if err != nil || live["claude1"] != 1 {
		t.Fatalf("control-plane count = (%v, %v), want the reservation counted", live, err)
	}
}

// TestAccountAPIClient_AMovedCountIsRebuiltTyped: a conditional claim that
// lost its race comes back as the typed error the resolver re-selects on.
func TestAccountAPIClient_AMovedCountIsRebuiltTyped(t *testing.T) {
	central := centralWithTable(t)
	account := central.addAccount("claude1", "claude", "", true)
	client := newBalancingClient(t, central)
	conditional := orchestration.SessionReservationRequest{Account: account.record, ExpectLive: true, ExpectedLive: 0}
	if _, err := client.Reserve(context.Background(), "tenant-a", conditional); err != nil {
		t.Fatalf("first conditional Reserve: %v", err)
	}
	if _, err := client.Reserve(context.Background(), "tenant-a", conditional); !errors.Is(err, orchestration.ErrAccountSessionCountChanged) {
		t.Fatalf("second conditional Reserve error = %v, want the moved count rebuilt from the 412", err)
	}
	if _, err := client.Reserve(context.Background(), "tenant-a", orchestration.SessionReservationRequest{}); err == nil {
		t.Fatal("a claim on no account must be refused")
	}
}

func TestAccountAPIClient_ReservationRefusesAnotherTenant(t *testing.T) {
	central := centralWithTable(t)
	account := central.addAccount("claude1", "claude", "", true)
	account.record.MaxConcurrentSessions = 1
	client := newBalancingClient(t, central)
	var mismatch *AccountAPITenantMismatchError
	if _, err := client.Reserve(context.Background(), "tenant-b", claim(account.record)); !errors.As(err, &mismatch) {
		t.Errorf("Reserve error = %v, want a tenant mismatch", err)
	}
	if err := client.Release(context.Background(), "tenant-b", "claude1", "r1"); !errors.As(err, &mismatch) {
		t.Errorf("Release error = %v, want a tenant mismatch", err)
	}
	if err := client.Release(context.Background(), "tenant-a", "claude1", ""); err != nil {
		t.Errorf("Release of no reservation = %v, want nil", err)
	}
}

// TestAccountPool_ConcurrentSpawnsOnTwoHostsShareOneCap is the P1 regression
// across the daemon boundary: two daemons (two pools, one control plane)
// resolving at the same moment against a cap of one get exactly one slot.
func TestAccountPool_ConcurrentSpawnsOnTwoHostsShareOneCap(t *testing.T) {
	central := centralWithTable(t)
	account := central.addAccount("claude1", "claude", "", true)
	account.record.MaxConcurrentSessions = 1
	hosts := []*AccountPool{
		newTestAccountPool(t, central, &staticTokenSource{}),
		newTestAccountPool(t, central, &staticTokenSource{}),
	}

	const spawns = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, spawns)
	for i := 0; i < spawns; i++ {
		wg.Add(1)
		go func(host *AccountPool) {
			defer wg.Done()
			<-start
			_, err := host.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
				TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Auto: true, JobID: "job",
			})
			results <- err
		}(hosts[i%2])
	}
	close(start)
	wg.Wait()
	close(results)

	won := 0
	for err := range results {
		switch {
		case err == nil:
			won++
		case errors.Is(err, orchestration.ErrAccountsSaturated):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d spawns got the account, want exactly 1 under a cap of 1", won)
	}
}
