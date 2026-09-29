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

package orchestration

import (
	"context"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// unmeteredPool seeds three unmetered accounts and returns a resolver over the
// real reservation table.
func unmeteredPool(t *testing.T) AccountResolver {
	t.Helper()
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	for _, id := range []string{"claude1", "claude2", "claude3"} {
		seedPoolAccount(t, store, secrets, cappedAccount(id, 0))
	}
	return reservingResolver(t, store, secrets, reservationTable(t, &mutableSessionLister{}, nil))
}

// TestAccountResolver_ABurstBeforeAnyRecordSpreadsAcrossThePool is round-3 P2:
// resolutions that run before any spawn has written a session record see each
// other's claims, so they spread. Nothing in the test increments a count.
func TestAccountResolver_ABurstBeforeAnyRecordSpreadsAcrossThePool(t *testing.T) {
	resolver := unmeteredPool(t)
	chosen := map[string]int{}
	for i := 0; i < 3; i++ {
		resolved, err := resolver.Resolve(context.Background(), AccountResolveRequest{
			TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
		})
		if err != nil {
			t.Fatalf("Resolve %d: %v", i+1, err)
		}
		chosen[resolved.AccountID]++
	}
	if len(chosen) != 3 {
		t.Fatalf("three resolutions chose %v, want one each across the pool", chosen)
	}
}

// TestAccountResolver_AConcurrentBurstSpreadsEvenly is the same property when
// the resolutions race: selection and claim are one decision, so a resolution
// that selected on a count another claim moved selects again.
func TestAccountResolver_AConcurrentBurstSpreadsEvenly(t *testing.T) {
	resolver := unmeteredPool(t)
	const spawns = 6
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	chosen := map[string]int{}
	for i := 0; i < spawns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resolved, err := resolver.Resolve(context.Background(), AccountResolveRequest{
				TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
			})
			if err != nil {
				t.Errorf("Resolve: %v", err)
				return
			}
			mu.Lock()
			chosen[resolved.AccountID]++
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()
	for _, id := range []string{"claude1", "claude2", "claude3"} {
		if chosen[id] != 2 {
			t.Fatalf("a concurrent burst of %d chose %v, want 2 on each account", spawns, chosen)
		}
	}
}

// TestSpawnSession_TheRecordNamesTheSpawnsReservation closes the correlation
// loop in process: the reservation the resolution took reaches the Session and
// the durable record, and that record then satisfies it.
func TestSpawnSession_TheRecordNamesTheSpawnsReservation(t *testing.T) {
	backend := &reservingPoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-a"}, reservationID: "res-42"}
	kv := kvstore.NewMemory()
	svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{"pool": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawn, err := impl.SpawnSession(ctx, &SpawnSessionRequest{Backend: "pool", WorkDir: "/w", AccountAuto: true, Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	record, err := impl.store.GetSession(ctx, "tenant-a", spawn.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if len(record.ReservationIDs) != 1 || record.ReservationIDs[0] != "res-42" {
		t.Fatalf("ReservationIDs = %v, want the spawn's reservation", record.ReservationIDs)
	}
}

// reservingPoolBackend reports a reservation on every spawn, as ACPBackend
// does once its resolver reserved one.
type reservingPoolBackend struct {
	poolResumeBackend
	reservationID string
}

func (b *reservingPoolBackend) Spawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	sess, err := b.poolResumeBackend.Spawn(ctx, req)
	if err != nil {
		return nil, err
	}
	sess.ReservationID = b.reservationID
	return sess, nil
}

func TestSessionRecord_AddReservation(t *testing.T) {
	record := &SessionRecord{}
	record.AddReservation("r1")
	record.AddReservation("r1")
	record.AddReservation("")
	record.AddReservation("r2")
	if len(record.ReservationIDs) != 2 {
		t.Fatalf("ReservationIDs = %v, want r1 and r2 once each", record.ReservationIDs)
	}
	var none *SessionRecord
	none.AddReservation("r3") // must not panic
}
