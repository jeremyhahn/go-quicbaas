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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-signal/pkg/keys"
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
