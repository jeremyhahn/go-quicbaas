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

// delegationTenantKey is the context key tenantBinder binds a delegation's
// tenant under, standing in for the identity qrdb's DelegatedContext binds.
type delegationTenantKey struct{}

// tenantBinder binds every delegated call to tenant, or refuses it.
type tenantBinder struct {
	tenant string
	refuse error
}

func (b *tenantBinder) ServiceContext(ctx context.Context) (context.Context, error) {
	return b.DelegatedContext(ctx, nil)
}

func (b *tenantBinder) DelegatedContext(ctx context.Context, _ []*x509.Certificate) (context.Context, error) {
	if b.refuse != nil {
		return nil, b.refuse
	}
	return context.WithValue(ctx, delegationTenantKey{}, b.tenant), nil
}

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
	c := newDelegatedQRDBClient(t, &tenantBinder{refuse: bindRefusedError{}}, kv, barrier)

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
	c := newDelegatedQRDBClient(t, &tenantBinder{tenant: "acme"}, kv, barrier)

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
