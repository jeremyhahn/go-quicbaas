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

package embedded

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// mockQuicBaaSForApps exposes a real AppManager through the appManagerProvider
// seam the production embedded transport uses.
type mockQuicBaaSForApps struct {
	service.QuicBaaS
	apps *iam.AppManager
}

func (m *mockQuicBaaSForApps) GetAppManager(_ context.Context) (*iam.AppManager, error) {
	return m.apps, nil
}

// embeddedAppRequest mirrors the SDK app client's {TenantID, ClientID}
// requests; the handlers read both fields via reflection.
type embeddedAppRequest struct {
	TenantID string
	ClientID string
}

// embeddedRegisterAppRequest mirrors the SDK app client's register envelope.
type embeddedRegisterAppRequest struct {
	TenantID string
	App      *iamsdk.RegisterAppRequest
}

func isCrossTenant(err error) bool {
	var crossTenant *tenantctx.CrossTenantError
	return errors.As(err, &crossTenant)
}

// TestHandleIAMApp_StaysInCallerTenant proves the embedded app verbs act only
// inside the tenant the call is bound to: naming another tenant is refused,
// another tenant's client id is not found and its app survives, and the
// owning tenant still succeeds.
func TestHandleIAMApp_StaysInCallerTenant(t *testing.T) {
	t.Parallel()

	apps := iam.NewAppManager(iam.NewMemoryStorage(), nil)
	svc := &mockQuicBaaSForApps{apps: apps}
	tenantA := tenantctx.WithTenant(context.Background(), "tenant-a")
	tenantB := tenantctx.WithTenant(context.Background(), "tenant-b")

	victim, err := apps.RegisterApp(context.Background(), "tenant-b", iam.RegisterAppOptions{
		Name:         "Victim",
		RedirectURIs: []string{"https://victim.example/callback"},
	})
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	register := &embeddedRegisterAppRequest{
		TenantID: "tenant-b",
		App:      &iamsdk.RegisterAppRequest{Name: "Planted", RedirectURIs: []string{"https://planted.example/cb"}},
	}
	if _, err := handleIAMAppRegister(tenantA, svc, register); !isCrossTenant(err) {
		t.Fatalf("register naming another tenant: want cross-tenant refusal, got %v", err)
	}
	if _, err := handleIAMAppList(tenantA, svc, &embeddedAppRequest{TenantID: "tenant-b"}); !isCrossTenant(err) {
		t.Fatalf("list naming another tenant: want cross-tenant refusal, got %v", err)
	}
	if _, err := handleIAMAppGet(tenantA, svc, &embeddedAppRequest{TenantID: "tenant-b", ClientID: victim.ClientID}); !isCrossTenant(err) {
		t.Fatalf("get naming another tenant: want cross-tenant refusal, got %v", err)
	}
	if _, err := handleIAMAppGet(tenantA, svc, &embeddedAppRequest{TenantID: "tenant-a", ClientID: victim.ClientID}); err == nil {
		t.Fatal("get of another tenant's client id under the caller's tenant succeeded")
	}
	if _, err := handleIAMAppDelete(tenantA, svc, &embeddedAppRequest{TenantID: "tenant-b", ClientID: victim.ClientID}); !isCrossTenant(err) {
		t.Fatalf("delete naming another tenant: want cross-tenant refusal, got %v", err)
	}
	if _, err := handleIAMAppDelete(tenantA, svc, &embeddedAppRequest{ClientID: victim.ClientID}); err == nil {
		t.Fatal("delete of another tenant's client id under the caller's tenant succeeded")
	}

	listed, err := handleIAMAppList(tenantB, svc, &embeddedAppRequest{TenantID: "tenant-b"})
	if err != nil {
		t.Fatalf("owner list: %v", err)
	}
	if got := listed.(*iamsdk.ListAppsResponse).TotalCount; got != 1 {
		t.Fatalf("owner list: want only the victim app, got %d apps", got)
	}
	if _, err := handleIAMAppGet(tenantB, svc, &embeddedAppRequest{TenantID: "tenant-b", ClientID: victim.ClientID}); err != nil {
		t.Fatalf("owner get: %v", err)
	}
	if _, err := handleIAMAppRegister(tenantB, svc, register); err != nil {
		t.Fatalf("owner register: %v", err)
	}
	if _, err := handleIAMAppDelete(tenantB, svc, &embeddedAppRequest{ClientID: victim.ClientID}); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
}

// TestHandleIAMApp_RefusesAnUnboundContext proves a call with no tenant bound
// is refused before the app store is touched.
func TestHandleIAMApp_RefusesAnUnboundContext(t *testing.T) {
	t.Parallel()

	svc := &mockQuicBaaSForApps{apps: iam.NewAppManager(iam.NewMemoryStorage(), nil)}
	if _, err := handleIAMAppGet(context.Background(), svc, &embeddedAppRequest{ClientID: "c1"}); err == nil {
		t.Fatal("get with no tenant bound succeeded")
	}
	if _, err := handleIAMAppList(context.Background(), svc, &embeddedAppRequest{}); err == nil {
		t.Fatal("list with no tenant bound succeeded")
	}
}
