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
// (ServicePrincipalIdentity, cmd/quicbaas/client.go), and its qrdb sub-client
// installs no AuthMiddleware for it. This records why: go-quicbaas's
// production grant set, which the tenant-hosting node's principal holds,
// covers the KV and tenant verbs but none of the barrier, ceremony or
// credential verbs the CLI drives through Security(), so the node's
// middleware would refuse them. The service-principal path stays as it is
// until those grants are settled (Codex review 7296 P1 1 on #1841).
func TestEmbeddedQRDB_ServicePrincipalGrantsLackTheCLISecurityVerbs(t *testing.T) {
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
		permSealStatus:                       false,
		qrdbsdk.PermissionSealUnseal:         false,
		qrdbsdk.PermissionSealSeal:           false,
		qrdbsdk.PermissionSealRekey:          false,
		qrdbsdk.PermissionCeremonyInitialize: false,
		qrdbsdk.PermissionCredentialsRotate:  false,
	}
	for perm, allowed := range cases {
		err := mw.AuthorizeTenant(ctx, qrdbsdk.SystemTenantID, perm)
		if allowed != (err == nil) {
			t.Errorf("service principal %s: error = %v, want allowed=%v", perm, err, allowed)
		}
	}

	// Without the middleware the sub-client runs a verb the grants refuse.
	barrier := &tenantRecordingBarrier{}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:      &tenantRecordingKV{memKV: newMemKV()},
		Admin:   stubAdminService{},
		Barrier: barrier,
	})
	if err := WithEmbeddedIdentity(ServicePrincipalIdentity(host.Node))(c.opts); err != nil {
		t.Fatalf("WithEmbeddedIdentity: %v", err)
	}
	b, err := c.Security().Barrier()
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
