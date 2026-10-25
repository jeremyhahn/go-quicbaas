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

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// The permissions the embedded qrdb transport authorizes a Put and a
// BarrierStatus against (pkg/permission KVPut and SealStatus).
const (
	permKVPut      = "kv:put"
	permSealStatus = "seal:status"
)

// grantBinder is the node's delegation binder as a test sees it: it binds
// every delegated call to an identity holding exactly grants inside tenant,
// as the node's DelegatedContext binds a delegation's scope, or refuses it,
// and hands out the node's own authorization middleware.
type grantBinder struct {
	tenant string
	grants []string
	refuse error
	mw     *qrdbsdk.AuthMiddleware
}

func (b *grantBinder) ServiceContext(ctx context.Context) (context.Context, error) {
	return b.DelegatedContext(ctx, nil)
}

func (b *grantBinder) DelegatedContext(ctx context.Context, _ []*x509.Certificate) (context.Context, error) {
	if b.refuse != nil {
		return nil, b.refuse
	}
	ctx = qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{
		Subject:          "delegated-service",
		TenantID:         b.tenant,
		ScopePermissions: b.grants,
	})
	ctx = context.WithValue(ctx, delegationTenantKey{}, b.tenant)
	return qrdbsdk.ContextWithTenant(ctx, b.tenant), nil
}

func (b *grantBinder) AuthMiddleware() *qrdbsdk.AuthMiddleware {
	return b.mw
}

// nodeAuthMiddleware returns the real tenant-hosting node's middleware.
func nodeAuthMiddleware(t *testing.T) *qrdbsdk.AuthMiddleware {
	t.Helper()
	mw := qrdbnode.TenantHost(t).Node.AuthMiddleware()
	if mw == nil {
		t.Fatal("the tenant-hosting node has no AuthMiddleware")
	}
	return mw
}

// newGrantClient builds an embedded client whose qrdb sub-client runs a
// delegation holding grants inside tenant acme over kv and barrier,
// authorized by the real node's middleware.
func newGrantClient(t *testing.T, grants []string, kv *tenantRecordingKV, barrier *tenantRecordingBarrier) *client {
	t.Helper()
	binder := &grantBinder{tenant: "acme", grants: grants, mw: nodeAuthMiddleware(t)}
	return newDelegatedQRDBClient(t, binder, kv, barrier)
}

// newRefusedGrantClient builds an embedded client whose binder refuses
// every delegated call with bindRefusedError.
func newRefusedGrantClient(t *testing.T, kv *tenantRecordingKV, barrier *tenantRecordingBarrier) *client {
	t.Helper()
	binder := &grantBinder{refuse: bindRefusedError{}, mw: nodeAuthMiddleware(t)}
	return newDelegatedQRDBClient(t, binder, kv, barrier)
}

// A delegation that lacks kv:put is refused by the node's middleware before
// DataStore().Put reaches the KV servicer (Codex review 7296 P1 1 on #1841).
func TestEmbeddedQRDB_DelegationWithoutWriteGrantCannotPut(t *testing.T) {
	kv := &tenantRecordingKV{memKV: newMemKV()}
	c := newGrantClient(t, []string{"kv:get"}, kv, &tenantRecordingBarrier{})

	store, err := c.DataStore()
	if err != nil {
		t.Fatalf("DataStore: %v", err)
	}
	if err := store.Put(context.Background(), "k", []byte("v")); err == nil {
		t.Fatal("Put under a read-only delegation succeeded, want a refusal")
	}
	if got, err := kv.memKV.Get(context.Background(), "k"); err != nil || got != nil {
		t.Fatalf("KV servicer holds %q (%v) after a refused Put, want nothing", got, err)
	}
}

// A delegation holding kv:put writes through DataStore().Put.
func TestEmbeddedQRDB_DelegationWithWriteGrantPuts(t *testing.T) {
	kv := &tenantRecordingKV{memKV: newMemKV()}
	c := newGrantClient(t, []string{permKVPut}, kv, &tenantRecordingBarrier{})

	store, err := c.DataStore()
	if err != nil {
		t.Fatalf("DataStore: %v", err)
	}
	if err := store.Put(context.Background(), "k", []byte("v")); err != nil {
		t.Fatalf("Put under a kv:put delegation: %v", err)
	}
	got, err := kv.memKV.Get(context.Background(), "k")
	if err != nil || string(got) != "v" {
		t.Fatalf("KV servicer holds %q (%v), want \"v\"", got, err)
	}
}

// The same holds for a security verb: BarrierStatus through the qrdb
// sub-client needs seal:status.
func TestEmbeddedQRDB_DelegationBarrierStatusNeedsItsGrant(t *testing.T) {
	cases := map[string]struct {
		grants  []string
		allowed bool
	}{
		"without seal:status": {grants: []string{permKVPut}, allowed: false},
		"with seal:status":    {grants: []string{permSealStatus}, allowed: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			barrier := &tenantRecordingBarrier{}
			c := newGrantClient(t, tc.grants, &tenantRecordingKV{memKV: newMemKV()}, barrier)

			b, err := c.Security().Barrier()
			if err != nil {
				t.Fatalf("Barrier: %v", err)
			}
			_, err = b.BarrierStatus(context.Background())
			if tc.allowed != (err == nil) {
				t.Fatalf("BarrierStatus error = %v, want allowed=%v", err, tc.allowed)
			}
			if reached := len(barrier.tenants) == 1; reached != tc.allowed {
				t.Fatalf("barrier servicer reached = %v, want %v", reached, tc.allowed)
			}
		})
	}
}

// A delegation whose binder supplies no authorization middleware never
// builds a qrdb sub-client: a delegation without its grants enforced is
// refused, not run unauthorized.
func TestEmbeddedQRDB_DelegationWithoutMiddlewareIsRefused(t *testing.T) {
	kv := &tenantRecordingKV{memKV: newMemKV()}
	c := newDelegatedQRDBClient(t, &grantBinder{tenant: "acme", grants: []string{permKVPut}}, kv, &tenantRecordingBarrier{})

	_, err := c.DataStore()
	var unauthorized *DelegationWithoutAuthorizationError
	if !stderrors.As(err, &unauthorized) {
		t.Fatalf("DataStore error = %v, want *DelegationWithoutAuthorizationError", err)
	}
	if len(kv.tenants) != 0 {
		t.Fatalf("kv saw %d calls, want none", len(kv.tenants))
	}
}
