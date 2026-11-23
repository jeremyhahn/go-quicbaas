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

package iam

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

const selfRegClaimTenant = "tenant-selfreg-claim"

// selfRegClaimStores names the storages a claim runs against: the in-memory
// storage, and BackendStorage over the qrdb in-memory store, which performs
// the compare-and-swap a claim needs.
func selfRegClaimStores() map[string]func() selfRegistrationClaimTestStorage {
	return map[string]func() selfRegistrationClaimTestStorage{
		"memory":  func() selfRegistrationClaimTestStorage { return NewMemoryStorage() },
		"backend": func() selfRegistrationClaimTestStorage { return NewBackendStorage(qrdbsdk.NewMemoryKVStore()) },
	}
}

type selfRegistrationClaimTestStorage interface {
	TenantStorage
	selfRegistrationClaimStorage
}

// seedSelfRegTenant stores a tenant with an unfinished registration.
func seedSelfRegTenant(t *testing.T, s TenantStorage, withRegistration bool) {
	t.Helper()
	tenant := &Tenant{ID: selfRegClaimTenant, Name: selfRegClaimTenant}
	if withRegistration {
		tenant.SelfRegistration = &TenantSelfRegistration{Expires: time.Now().Add(time.Hour)}
	}
	if err := s.CreateTenant(context.Background(), tenant); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
}

func requireClaimReason(t *testing.T, err error, reason string) *SelfRegistrationClaimError {
	t.Helper()
	var claimErr *SelfRegistrationClaimError
	if !errors.As(err, &claimErr) {
		t.Fatalf("want *SelfRegistrationClaimError with reason %q, got %v", reason, err)
	}
	if claimErr.Reason != reason {
		t.Fatalf("claim refused with reason %q, want %q", claimErr.Reason, reason)
	}
	return claimErr
}

func forEachClaimStore(t *testing.T, fn func(t *testing.T, s selfRegistrationClaimTestStorage)) {
	for name, mk := range selfRegClaimStores() {
		t.Run(name, func(t *testing.T) {
			fn(t, mk())
		})
	}
}

func TestClaimSelfRegistration_FirstClaimWins(t *testing.T) {
	forEachClaimStore(t, func(t *testing.T, s selfRegistrationClaimTestStorage) {
		ctx := context.Background()
		seedSelfRegTenant(t, s, true)
		got, err := s.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationDiscarding, Holder: "A", Lease: time.Minute})
		if err != nil {
			t.Fatalf("first claim: %v", err)
		}
		if got.SelfRegistration.State != SelfRegistrationDiscarding || got.SelfRegistration.Holder != "A" {
			t.Fatalf("first claim returned %+v", got.SelfRegistration)
		}
		_, err = s.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationFinishing, Holder: "B", Lease: time.Minute})
		held := requireClaimReason(t, err, SelfRegistrationClaimHeld)
		if held.HeldBy != SelfRegistrationDiscarding {
			t.Fatalf("held by %q, want %q", held.HeldBy, SelfRegistrationDiscarding)
		}

		stored, err := s.GetTenant(ctx, selfRegClaimTenant)
		if err != nil {
			t.Fatalf("GetTenant: %v", err)
		}
		if stored.SelfRegistration.Holder != "A" || stored.SelfRegistration.Expires.IsZero() {
			t.Fatalf("stored registration %+v, want holder A and the expiry kept", stored.SelfRegistration)
		}
	})
}

func TestClaimSelfRegistration_HolderMovesItsOwnClaim(t *testing.T) {
	forEachClaimStore(t, func(t *testing.T, s selfRegistrationClaimTestStorage) {
		ctx := context.Background()
		seedSelfRegTenant(t, s, true)
		if _, err := s.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationFinishing, Holder: "A", Lease: time.Minute}); err != nil {
			t.Fatalf("finishing claim: %v", err)
		}
		got, err := s.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationDiscarding, Holder: "A", Lease: time.Minute})
		if err != nil {
			t.Fatalf("the holder moving its own claim: %v", err)
		}
		if got.SelfRegistration.State != SelfRegistrationDiscarding {
			t.Fatalf("state %q, want %q", got.SelfRegistration.State, SelfRegistrationDiscarding)
		}
	})
}

func TestClaimSelfRegistration_LapsedLeaseIsTaken(t *testing.T) {
	forEachClaimStore(t, func(t *testing.T, s selfRegistrationClaimTestStorage) {
		ctx := context.Background()
		seedSelfRegTenant(t, s, true)
		now := time.Now()
		if _, err := s.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationFinishing, Holder: "A", Lease: time.Minute, Now: now}); err != nil {
			t.Fatalf("claim by A: %v", err)
		}
		got, err := s.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationDiscarding, Holder: "B", Lease: time.Minute, Now: now.Add(2 * time.Minute)})
		if err != nil {
			t.Fatalf("taking over a lapsed lease: %v", err)
		}
		if got.SelfRegistration.Holder != "B" {
			t.Fatalf("holder %q, want B", got.SelfRegistration.Holder)
		}
	})
}

func TestClaimSelfRegistration_FinishedRecordRefuses(t *testing.T) {
	forEachClaimStore(t, func(t *testing.T, s selfRegistrationClaimTestStorage) {
		ctx := context.Background()
		seedSelfRegTenant(t, s, false)
		_, err := s.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationDiscarding, Holder: "A", Lease: time.Minute})
		requireClaimReason(t, err, SelfRegistrationClaimFinished)

		// A tenant that is gone holds no registration either.
		_, err = s.ClaimSelfRegistration(ctx, "no-such-tenant", SelfRegistrationClaim{State: SelfRegistrationDiscarding, Holder: "A", Lease: time.Minute})
		requireClaimReason(t, err, SelfRegistrationClaimFinished)
	})
}

func TestClaimSelfRegistration_ConcurrentClaimsOneWinner(t *testing.T) {
	forEachClaimStore(t, func(t *testing.T, s selfRegistrationClaimTestStorage) {
		ctx := context.Background()
		seedSelfRegTenant(t, s, true)
		const claimants = 8
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			winners int
		)
		for i := range claimants {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationFinishing, Holder: fmt.Sprintf("holder-%d", i), Lease: time.Minute})
				if err == nil {
					mu.Lock()
					winners++
					mu.Unlock()
					return
				}
				var claimErr *SelfRegistrationClaimError
				if !errors.As(err, &claimErr) || claimErr.Reason != SelfRegistrationClaimHeld {
					t.Errorf("a losing claim: want held, got %v", err)
				}
			}()
		}
		wg.Wait()
		if winners != 1 {
			t.Fatalf("%d claims succeeded, want exactly 1", winners)
		}
	})
}

// casLessKVStore hides the compare-and-swap of the store it wraps, standing in
// for a store that cannot arbitrate a claim.
type casLessKVStore struct {
	qrdbsdk.KVStore
}

func TestClaimSelfRegistration_StoreWithoutCASRefuses(t *testing.T) {
	ctx := context.Background()
	inner := qrdbsdk.NewMemoryKVStore()
	s := NewBackendStorage(casLessKVStore{KVStore: inner})
	seedSelfRegTenant(t, s, true)
	_, err := s.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationDiscarding, Holder: "A", Lease: time.Minute})
	requireClaimReason(t, err, SelfRegistrationClaimUnsupported)

	stored, err := s.GetTenant(ctx, selfRegClaimTenant)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if stored.SelfRegistration.State != "" {
		t.Fatalf("a refused claim wrote state %q", stored.SelfRegistration.State)
	}
}

func TestDefaultTenantManager_ClaimSelfRegistrationDelegates(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	seedSelfRegTenant(t, storage, true)
	mgr := NewDefaultTenantManager(storage)

	got, err := mgr.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationFinishing, Holder: "A", Lease: time.Minute})
	if err != nil {
		t.Fatalf("ClaimSelfRegistration: %v", err)
	}
	if got.SelfRegistration.State != SelfRegistrationFinishing {
		t.Fatalf("state %q, want %q", got.SelfRegistration.State, SelfRegistrationFinishing)
	}
	_, err = mgr.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationDiscarding, Holder: "B", Lease: time.Minute})
	requireClaimReason(t, err, SelfRegistrationClaimHeld)

	// A storage without the claim refuses it as unsupported.
	plain := NewDefaultTenantManager(tenantStorageOnly{TenantStorage: storage})
	_, err = plain.ClaimSelfRegistration(ctx, selfRegClaimTenant, SelfRegistrationClaim{State: SelfRegistrationDiscarding, Holder: "B", Lease: time.Minute})
	requireClaimReason(t, err, SelfRegistrationClaimUnsupported)
}

// tenantStorageOnly hides every method but TenantStorage's.
type tenantStorageOnly struct {
	TenantStorage
}
