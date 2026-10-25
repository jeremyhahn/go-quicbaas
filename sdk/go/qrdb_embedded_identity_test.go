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

package quicbaas

import (
	"context"
	"crypto/x509"
	stderrors "errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// delegationTenantKey is the context key grantBinder binds a delegation's
// tenant under, so a recording servicer sees the tenant a call reached it in.
type delegationTenantKey struct{}

// boundTenant returns the tenant a binder bound ctx to, or "".
func boundTenant(ctx context.Context) string {
	tenant, _ := ctx.Value(delegationTenantKey{}).(string)
	return tenant
}

// tenantRecordingKV records the tenant every Get reached the servicer with.
type tenantRecordingKV struct {
	*memKV
	tenants []string
}

func (k *tenantRecordingKV) Get(ctx context.Context, key string) ([]byte, error) {
	k.tenants = append(k.tenants, boundTenant(ctx))
	return k.memKV.Get(ctx, key)
}

// GetWithConsistency is the read the embedded transport issues when the
// KV store resolves a read consistency.
func (k *tenantRecordingKV) GetWithConsistency(ctx context.Context, key string, _ qrdbsdk.ReadOptions) ([]byte, error) {
	return k.Get(ctx, key)
}

func (k *tenantRecordingKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	k.tenants = append(k.tenants, boundTenant(ctx))
	return k.memKV.PutIfAbsent(ctx, key, value)
}

func (k *tenantRecordingKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	k.tenants = append(k.tenants, boundTenant(ctx))
	return k.memKV.CompareAndSwap(ctx, key, expected, next)
}

func (k *tenantRecordingKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	k.tenants = append(k.tenants, boundTenant(ctx))
	return k.memKV.CompareAndDelete(ctx, key, expected)
}

// atomicDataStore returns c's DataStore as the AtomicKVStore a DAO's
// SaveIfAbsent and conditional writes assert it to be.
func atomicDataStore(t *testing.T, c *client) qrdbsdk.AtomicKVStore {
	t.Helper()
	store, err := c.DataStore()
	if err != nil {
		t.Fatalf("DataStore: %v", err)
	}
	atomic, ok := store.(qrdbsdk.AtomicKVStore)
	if !ok {
		t.Fatalf("DataStore %T is not an AtomicKVStore", store)
	}
	return atomic
}

// tenantRecordingBarrier records the tenant every BarrierStatus reached the
// servicer with.
type tenantRecordingBarrier struct {
	stubBarrierService
	tenants []string
}

func (b *tenantRecordingBarrier) BarrierStatus(ctx context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	b.tenants = append(b.tenants, boundTenant(ctx))
	return &qrdbsdk.BarrierStatusInfo{}, nil
}

// newDelegatedQRDBClient builds an embedded client whose qrdb sub-client runs
// over kv and barrier, as the delegation binder binds it.
func newDelegatedQRDBClient(t *testing.T, binder EmbeddedIdentityBinder, kv *tenantRecordingKV, barrier *tenantRecordingBarrier) *client {
	t.Helper()
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:      kv,
		Admin:   stubAdminService{},
		Barrier: barrier,
	})
	if err := WithEmbeddedIdentity(DelegatedIdentity(binder, []*x509.Certificate{{}}))(c.opts); err != nil {
		t.Fatalf("WithEmbeddedIdentity: %v", err)
	}
	return c
}

func TestEmbeddedQRDB_RefusedDelegationNeverDispatches(t *testing.T) {
	kv := &tenantRecordingKV{memKV: newMemKV()}
	barrier := &tenantRecordingBarrier{}
	c := newRefusedGrantClient(t, kv, barrier)

	store, err := c.DataStore()
	if err != nil {
		t.Fatalf("DataStore: %v", err)
	}
	if _, err := store.Get(context.Background(), "k"); !stderrors.Is(err, bindRefusedError{}) {
		t.Fatalf("DataStore Get err = %v, want the binder's refusal", err)
	}

	b, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Barrier: %v", err)
	}
	if _, err := b.BarrierStatus(context.Background()); !stderrors.Is(err, bindRefusedError{}) {
		t.Fatalf("BarrierStatus err = %v, want the binder's refusal", err)
	}

	if len(kv.tenants) != 0 || len(barrier.tenants) != 0 {
		t.Fatalf("a refused delegation reached the servicers: kv %v, barrier %v", kv.tenants, barrier.tenants)
	}
}

func TestEmbeddedQRDB_DelegationCarriesItsTenant(t *testing.T) {
	kv := &tenantRecordingKV{memKV: newMemKV()}
	barrier := &tenantRecordingBarrier{}
	c := newGrantClient(t, []string{"kv:get", permSealStatus}, kv, barrier)

	store, err := c.DataStore()
	if err != nil {
		t.Fatalf("DataStore: %v", err)
	}
	if _, err := store.Get(context.Background(), "k"); err != nil {
		t.Fatalf("DataStore Get: %v", err)
	}
	b, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Barrier: %v", err)
	}
	if _, err := b.BarrierStatus(context.Background()); err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}

	if len(kv.tenants) != 1 || kv.tenants[0] != "acme" {
		t.Fatalf("kv saw tenants %q, want [acme]", kv.tenants)
	}
	if len(barrier.tenants) != 1 || barrier.tenants[0] != "acme" {
		t.Fatalf("barrier saw tenants %q, want [acme]", barrier.tenants)
	}
}

func TestEmbeddedQRDB_DelegationGuardedWritesCarryItsTenant(t *testing.T) {
	kv := &tenantRecordingKV{memKV: newMemKV()}
	c := newGrantClient(t, []string{permKVPut, "kv:delete"}, kv, &tenantRecordingBarrier{})
	store := atomicDataStore(t, c)
	ctx := context.Background()

	if stored, err := store.PutIfAbsent(ctx, "k", []byte("v1")); err != nil || !stored {
		t.Fatalf("PutIfAbsent = %v, %v; want true, nil", stored, err)
	}
	if swapped, err := store.CompareAndSwap(ctx, "k", []byte("v1"), []byte("v2")); err != nil || !swapped {
		t.Fatalf("CompareAndSwap = %v, %v; want true, nil", swapped, err)
	}
	if deleted, err := store.CompareAndDelete(ctx, "k", []byte("v2")); err != nil || !deleted {
		t.Fatalf("CompareAndDelete = %v, %v; want true, nil", deleted, err)
	}

	want := []string{"acme", "acme", "acme"}
	if len(kv.tenants) != len(want) {
		t.Fatalf("kv saw tenants %q, want %q", kv.tenants, want)
	}
	for i := range want {
		if kv.tenants[i] != want[i] {
			t.Fatalf("kv saw tenants %q, want %q", kv.tenants, want)
		}
	}
}

func TestEmbeddedQRDB_RefusedDelegationNeverDispatchesGuardedWrites(t *testing.T) {
	kv := &tenantRecordingKV{memKV: newMemKV()}
	c := newRefusedGrantClient(t, kv, &tenantRecordingBarrier{})
	store := atomicDataStore(t, c)
	ctx := context.Background()

	if _, err := store.PutIfAbsent(ctx, "k", []byte("v")); !stderrors.Is(err, bindRefusedError{}) {
		t.Fatalf("PutIfAbsent err = %v, want the binder's refusal", err)
	}
	if _, err := store.CompareAndSwap(ctx, "k", []byte("v"), []byte("w")); !stderrors.Is(err, bindRefusedError{}) {
		t.Fatalf("CompareAndSwap err = %v, want the binder's refusal", err)
	}
	if _, err := store.CompareAndDelete(ctx, "k", []byte("v")); !stderrors.Is(err, bindRefusedError{}) {
		t.Fatalf("CompareAndDelete err = %v, want the binder's refusal", err)
	}
	if len(kv.tenants) != 0 {
		t.Fatalf("a refused delegation reached the kv servicer: %v", kv.tenants)
	}
}
