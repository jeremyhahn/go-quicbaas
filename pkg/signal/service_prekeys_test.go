//go:build quicbaas_signal

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package signal

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/jeremyhahn/go-signal/pkg/keys"
	signalstore "github.com/jeremyhahn/go-signal/pkg/store"
)

const preKeyTestAccount = "0b6d8d3e-6a8e-4f4c-9d1e-5a1f0c2e7b10"

// oneTimeKeyBundle uploads a signed pre-key, the given one-time EC and KEM
// key IDs, and a last-resort KEM key with ID lastResortID.
func oneTimeKeyBundle(ecIDs, kemIDs []uint64, lastResortID uint64) *keys.SetKeysRequest {
	req := &keys.SetKeysRequest{
		SignedPreKey: &keys.SignedPreKey{KeyID: 1, PublicKey: []byte("spk"), Signature: []byte("sig")},
		KEMLastResortPreKey: &keys.KEMSignedPreKey{
			KeyID: lastResortID, PublicKey: []byte("kem-lr"), Signature: []byte("sig"), IsLastResort: true,
		},
	}
	for _, id := range ecIDs {
		req.PreKeys = append(req.PreKeys, &keys.PreKey{KeyID: id, PublicKey: []byte("ec")})
	}
	for _, id := range kemIDs {
		req.KEMPreKeys = append(req.KEMPreKeys, &keys.KEMSignedPreKey{KeyID: id, PublicKey: []byte("kem"), Signature: []byte("sig")})
	}
	return req
}

// TestSignalService_GetPreKeys_ConsumesOneTimeKeys drives one-time pre-key
// consumption through the service's tenant adapter: each EC and KEM one-time
// key is handed out once, exhaustion falls back to no EC key and the
// last-resort KEM key, and another tenant's keys stay untouched.
func TestSignalService_GetPreKeys_ConsumesOneTimeKeys(t *testing.T) {
	svc, _ := newEnabledService(t)
	provisionTestTenant(t, svc, "tenant-a")
	provisionTestTenant(t, svc, "tenant-b")
	ctxA, ctxB := tenantCtx("tenant-a"), tenantCtx("tenant-b")

	if err := svc.StorePreKeys(ctxA, preKeyTestAccount, 1, oneTimeKeyBundle([]uint64{11, 12}, []uint64{21}, 29)); err != nil {
		t.Fatalf("StorePreKeys tenant-a: %v", err)
	}
	if err := svc.StorePreKeys(ctxB, preKeyTestAccount, 1, oneTimeKeyBundle([]uint64{111}, []uint64{121}, 129)); err != nil {
		t.Fatalf("StorePreKeys tenant-b: %v", err)
	}

	seenEC := map[uint64]bool{}
	wantKEM := []uint64{21, 29, 29} // one-time first, then last resort (never consumed)
	for i, kemID := range wantKEM {
		item, err := svc.GetPreKeys(ctxA, preKeyTestAccount, 1)
		if err != nil {
			t.Fatalf("GetPreKeys #%d: %v", i, err)
		}
		if item.KEMPreKey == nil || item.KEMPreKey.KeyID != kemID {
			t.Fatalf("GetPreKeys #%d: want KEM key %d, got %+v", i, kemID, item.KEMPreKey)
		}
		if i < 2 {
			if item.PreKey == nil {
				t.Fatalf("GetPreKeys #%d: want a one-time EC key, got none", i)
			}
			if seenEC[item.PreKey.KeyID] {
				t.Fatalf("GetPreKeys #%d: EC key %d handed out twice", i, item.PreKey.KeyID)
			}
			seenEC[item.PreKey.KeyID] = true
		} else if item.PreKey != nil {
			t.Fatalf("GetPreKeys #%d: EC keys are exhausted, got %+v", i, item.PreKey)
		}
	}
	if !seenEC[11] || !seenEC[12] {
		t.Fatalf("want EC keys 11 and 12 consumed, got %v", seenEC)
	}

	item, err := svc.GetPreKeys(ctxB, preKeyTestAccount, 1)
	if err != nil {
		t.Fatalf("GetPreKeys tenant-b: %v", err)
	}
	if item.PreKey == nil || item.PreKey.KeyID != 111 {
		t.Fatalf("tenant-b EC key: want 111, got %+v", item.PreKey)
	}
	if item.KEMPreKey == nil || item.KEMPreKey.KeyID != 121 {
		t.Fatalf("tenant-b KEM key: want one-time 121, got %+v", item.KEMPreKey)
	}
}

// TestSignalService_GetPreKeys_TakeFailurePropagates checks that a failed
// atomic delete fails the call instead of returning a bundle that silently
// omits the device's one-time keys, and that the keys survive for a retry.
func TestSignalService_GetPreKeys_TakeFailurePropagates(t *testing.T) {
	kv := newServiceTestKVClient()
	txn := newMapTxn(kv.data)
	svc := NewSignalService(kv, txn, newTestLogger(t), &SignalConfig{Enabled: true})
	provisionTestTenant(t, svc, "tenant-a")
	ctx := tenantCtx("tenant-a")

	if err := svc.StorePreKeys(ctx, preKeyTestAccount, 1, oneTimeKeyBundle([]uint64{11}, []uint64{21}, 29)); err != nil {
		t.Fatalf("StorePreKeys: %v", err)
	}

	boom := errors.New("raft unavailable")
	txn.err = boom
	item, err := svc.GetPreKeys(ctx, preKeyTestAccount, 1)
	if !errors.Is(err, boom) {
		t.Fatalf("want the transaction failure, got item=%+v err=%v", item, err)
	}

	txn.err = nil
	item, err = svc.GetPreKeys(ctx, preKeyTestAccount, 1)
	if err != nil {
		t.Fatalf("GetPreKeys after recovery: %v", err)
	}
	if item.PreKey == nil || item.PreKey.KeyID != 11 {
		t.Fatalf("EC key: want 11 still available, got %+v", item.PreKey)
	}
	if item.KEMPreKey == nil || item.KEMPreKey.KeyID != 21 {
		t.Fatalf("KEM key: want one-time 21 still available, got %+v", item.KEMPreKey)
	}
}

// TestSignalService_NoTransactionClientRefused checks that a service built
// without a transaction client refuses tenant operations rather than
// consuming one-time pre-keys without an atomic delete.
func TestSignalService_NoTransactionClientRefused(t *testing.T) {
	kv := newServiceTestKVClient()
	svc := NewSignalService(kv, nil, newTestLogger(t), &SignalConfig{Enabled: true})
	provisionTestTenant(t, svc, "tenant-a")
	ctx := tenantCtx("tenant-a")

	if _, err := svc.GetPreKeys(ctx, preKeyTestAccount, 1); !errors.Is(err, ErrNoTransactions) {
		t.Fatalf("GetPreKeys: want ErrNoTransactions, got %v", err)
	}
	if err := svc.StorePreKeys(ctx, preKeyTestAccount, 1, oneTimeKeyBundle(nil, nil, 29)); !errors.Is(err, ErrNoTransactions) {
		t.Fatalf("StorePreKeys: want ErrNoTransactions, got %v", err)
	}
}

// stealingKVClient deletes the keys whose suffix is in steal just before
// their first Get, as a concurrent GetPreKeys that consumed them between
// this request's List and Get would.
type stealingKVClient struct {
	*serviceTestKVClient

	mu    sync.Mutex
	steal map[string]bool
}

func (c *stealingKVClient) Get(ctx context.Context, key string, opts ...qrdbtransport.ReadOption) ([]byte, error) {
	c.mu.Lock()
	for suffix := range c.steal {
		if strings.HasSuffix(key, suffix) {
			delete(c.steal, suffix)
			delete(c.data, key)
		}
	}
	c.mu.Unlock()
	return c.serviceTestKVClient.Get(ctx, key, opts...)
}

// TestSignalService_GetPreKeys_KeyTakenBetweenListAndGet checks that a
// one-time key consumed by a concurrent request after this request listed it
// is skipped like go-signal intends, not reported as a backend failure that
// would discard the bundle (and a KEM key consumed for it).
func TestSignalService_GetPreKeys_KeyTakenBetweenListAndGet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		steal   []string
		wantEC  uint64 // 0: no EC key left
		wantKEM uint64
	}{
		{name: "next EC candidate", steal: []string{"/11"}, wantEC: 12, wantKEM: 21},
		{name: "all EC candidates", steal: []string{"/11", "/12"}, wantEC: 0, wantKEM: 21},
		{name: "KEM candidate", steal: []string{"/21"}, wantEC: 11, wantKEM: 29},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kv := &stealingKVClient{serviceTestKVClient: newServiceTestKVClient(), steal: map[string]bool{}}
			svc := NewSignalService(kv, newMapTxn(kv.data), newTestLogger(t), &SignalConfig{Enabled: true})
			provisionTestTenant(t, svc, "tenant-a")
			ctx := tenantCtx("tenant-a")
			if err := svc.StorePreKeys(ctx, preKeyTestAccount, 1, oneTimeKeyBundle([]uint64{11, 12}, []uint64{21}, 29)); err != nil {
				t.Fatalf("StorePreKeys: %v", err)
			}

			kv.mu.Lock()
			for _, s := range tc.steal {
				kv.steal[s] = true
			}
			kv.mu.Unlock()

			item, err := svc.GetPreKeys(ctx, preKeyTestAccount, 1)
			if err != nil {
				t.Fatalf("GetPreKeys: a concurrently consumed key must be skipped, got %v", err)
			}
			switch {
			case tc.wantEC == 0 && item.PreKey != nil:
				t.Fatalf("EC key: want none, got %+v", item.PreKey)
			case tc.wantEC != 0 && (item.PreKey == nil || item.PreKey.KeyID != tc.wantEC):
				t.Fatalf("EC key: want %d, got %+v", tc.wantEC, item.PreKey)
			}
			if item.KEMPreKey == nil || item.KEMPreKey.KeyID != tc.wantKEM {
				t.Fatalf("KEM key: want %d, got %+v", tc.wantKEM, item.KEMPreKey)
			}
			kv.mu.Lock()
			defer kv.mu.Unlock()
			if len(kv.steal) != 0 {
				t.Fatalf("keys never fetched, so the race was not exercised: %v", kv.steal)
			}
		})
	}
}

// TestKVAdapter_TranslatesNotFound checks that the adapter reports a missing
// key as go-signal's store.KeyNotFoundError on Get and Delete, and passes
// other errors through unchanged.
func TestKVAdapter_TranslatesNotFound(t *testing.T) {
	ctx := context.Background()
	kv := newServiceTestKVClient()
	adapter, err := NewTenantKVAdapter(kv, newMapTxn(kv.data), "tenant-a")
	if err != nil {
		t.Fatalf("NewTenantKVAdapter: %v", err)
	}

	var notFound *signalstore.KeyNotFoundError
	if _, err := adapter.Get(ctx, "missing"); !errors.As(err, &notFound) || notFound.Key != "missing" {
		t.Fatalf("Get: want store.KeyNotFoundError{missing}, got %T %v", err, err)
	}
	if err := adapter.Delete(ctx, "missing"); err != nil && !errors.As(err, &notFound) {
		t.Fatalf("Delete: want nil or store.KeyNotFoundError, got %T %v", err, err)
	}

	faulty := newFaultyKVClient()
	faulty.failGet, faulty.failDelete = true, true
	adapter, err = NewTenantKVAdapter(faulty, newMapTxn(faulty.data), "tenant-a")
	if err != nil {
		t.Fatalf("NewTenantKVAdapter: %v", err)
	}
	if _, err := adapter.Get(ctx, "k"); !errors.Is(err, errKVBackend) {
		t.Fatalf("Get: want the backend error, got %v", err)
	}
	if err := adapter.Delete(ctx, "k"); !errors.Is(err, errKVBackend) {
		t.Fatalf("Delete: want the backend error, got %v", err)
	}
}
