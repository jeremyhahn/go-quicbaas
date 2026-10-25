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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// The CLI's embedded client runs as the server's service principal
// (ServicePrincipalIdentity, cmd/quicbaas/client.go). Ruling 179: the node's
// authorization middleware bounds it, and go-quicbaas's production grant set,
// which the tenant-hosting node's principal holds, carries the barrier,
// ceremony and credential verbs the CLI drives through Security().

// newServicePrincipalQRDBClient builds an embedded client whose qrdb
// sub-client runs as the tenant-hosting node's service principal over
// barrier, with stub KV and admin servicers.
func newServicePrincipalQRDBClient(t *testing.T, barrier *tenantRecordingBarrier, index QRDBIndexServicer) *client {
	t.Helper()
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:      &tenantRecordingKV{memKV: newMemKV()},
		Admin:   stubAdminService{},
		Barrier: barrier,
		Index:   index,
	})
	if err := WithEmbeddedIdentity(ServicePrincipalIdentity(qrdbnode.TenantHost(t).Node))(c.opts); err != nil {
		t.Fatalf("WithEmbeddedIdentity: %v", err)
	}
	return c
}

func TestEmbeddedQRDB_ServicePrincipalHoldsTheCLISecurityVerbs(t *testing.T) {
	host := qrdbnode.TenantHost(t)
	mw := nodeAuthMiddleware(t)
	ctx, err := host.Node.ServiceContext(context.Background())
	if err != nil {
		t.Fatalf("ServiceContext: %v", err)
	}

	cases := map[qrdbsdk.Permission]bool{
		"kv:get":                             true,
		permKVPut:                            true,
		qrdbsdk.PermissionTenantsCreate:      true,
		permSealStatus:                       true,
		qrdbsdk.PermissionSealUnseal:         true,
		qrdbsdk.PermissionSealSeal:           true,
		qrdbsdk.PermissionSealRekey:          true,
		qrdbsdk.PermissionCeremonyInitialize: true,
		qrdbsdk.PermissionCredentialsRotate:  true,
		"index:drop":                         false,
	}
	for perm, allowed := range cases {
		err := mw.AuthorizeTenant(ctx, qrdbsdk.SystemTenantID, perm)
		if allowed != (err == nil) {
			t.Errorf("service principal %s: error = %v, want allowed=%v", perm, err, allowed)
		}
	}
}

// A verb the service principal holds reaches the servicer through the
// node's middleware.
func TestEmbeddedQRDB_ServicePrincipalRunsAGrantedVerb(t *testing.T) {
	barrier := &tenantRecordingBarrier{}
	b, err := newServicePrincipalQRDBClient(t, barrier, nil).Security().Barrier()
	if err != nil {
		t.Fatalf("Barrier: %v", err)
	}
	if _, err := b.BarrierStatus(context.Background()); err != nil {
		t.Fatalf("BarrierStatus as the service principal: %v", err)
	}
	if len(barrier.tenants) != 1 {
		t.Fatalf("barrier servicer reached %d times, want 1", len(barrier.tenants))
	}
}

// droppingIndex records the indexes an IndexDrop reaches it with. Only the
// drops are served; the test calls nothing else.
type droppingIndex struct {
	qrdbsdk.IndexServicer
	dropped []string
}

func (x *droppingIndex) DropIndex(_ context.Context, name string) error {
	x.dropped = append(x.dropped, name)
	return nil
}

func (x *droppingIndex) DropOwnedIndex(_ context.Context, name, _ string) error {
	x.dropped = append(x.dropped, name)
	return nil
}

// A verb the service principal does not hold (index:drop) is refused by the
// node's middleware before it reaches the servicer: the path is not exempt.
func TestEmbeddedQRDB_ServicePrincipalUngrantedVerbIsRefused(t *testing.T) {
	index := &droppingIndex{}
	qc, err := newServicePrincipalQRDBClient(t, &tenantRecordingBarrier{}, index).qrdbClient()
	if err != nil {
		t.Fatalf("qrdbClient: %v", err)
	}
	if err := qc.Index().IndexDrop(context.Background(), "users_by_email"); err == nil {
		t.Fatal("IndexDrop as the service principal succeeded, want a refusal")
	}
	if len(index.dropped) != 0 {
		t.Fatalf("index servicer dropped %v, want the node's middleware to refuse the call", index.dropped)
	}
}
