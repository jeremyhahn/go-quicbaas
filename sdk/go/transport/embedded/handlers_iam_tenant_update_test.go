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
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// mockQuicBaaSForTenants exposes a real DefaultTenantManager through the
// tenantManagerProvider seam the embedded transport uses.
type mockQuicBaaSForTenants struct {
	service.QuicBaaS
	tenants iam.TenantManager
}

func (m *mockQuicBaaSForTenants) GetTenantManager(_ context.Context) (iam.TenantManager, error) {
	return m.tenants, nil
}

// embeddedUpdateTenantEnvelope mirrors the SDK client's {TenantID, Request}
// transport wrapper for Tenant/Update.
type embeddedUpdateTenantEnvelope struct {
	TenantID string
	Request  *iamsdk.UpdateTenantRequest
}

// TestHandleIAMTenantUpdate_StoresDisplayName proves an update of
// DisplayName lands in the tenant's display name, leaves its Name alone and
// keeps every other setting (#1518).
func TestHandleIAMTenantUpdate_StoresDisplayName(t *testing.T) {
	ctx := context.Background()
	mgr := iam.NewDefaultTenantManager(iam.NewMemoryStorage())
	if _, err := mgr.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:       "acme",
		Name:     "acme",
		Settings: &iam.TenantSettings{MaxUsers: 42, DisplayName: "Old Name"},
	}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	svc := &mockQuicBaaSForTenants{tenants: mgr}

	out, err := handleIAMTenantUpdate(ctx, svc, &embeddedUpdateTenantEnvelope{
		TenantID: "acme",
		Request:  &iamsdk.UpdateTenantRequest{DisplayName: "Acme Corp"},
	})
	if err != nil {
		t.Fatalf("handleIAMTenantUpdate: %v", err)
	}
	if got := out.(*iamsdk.Tenant); got.DisplayName != "Acme Corp" || got.Name != "acme" {
		t.Fatalf("update returned Name=%q DisplayName=%q, want acme / Acme Corp", got.Name, got.DisplayName)
	}

	read, err := handleIAMTenantGet(ctx, svc, &embeddedUpdateTenantEnvelope{TenantID: "acme"})
	if err != nil {
		t.Fatalf("handleIAMTenantGet: %v", err)
	}
	if got := read.(*iamsdk.Tenant); got.DisplayName != "Acme Corp" || got.Name != "acme" {
		t.Fatalf("get returned Name=%q DisplayName=%q, want acme / Acme Corp", got.Name, got.DisplayName)
	}
	stored, err := mgr.GetTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if stored.Settings.MaxUsers != 42 {
		t.Fatalf("MaxUsers = %d after a DisplayName update, want 42", stored.Settings.MaxUsers)
	}
}

// TestHandleIAMTenantUpdate_UnknownTenant proves an update of a tenant that
// does not exist returns the manager's typed not-found error.
func TestHandleIAMTenantUpdate_UnknownTenant(t *testing.T) {
	svc := &mockQuicBaaSForTenants{tenants: iam.NewDefaultTenantManager(iam.NewMemoryStorage())}

	_, err := handleIAMTenantUpdate(context.Background(), svc, &embeddedUpdateTenantEnvelope{
		TenantID: "missing",
		Request:  &iamsdk.UpdateTenantRequest{DisplayName: "Nobody"},
	})
	var notFound *iam.TenantNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v, want *iam.TenantNotFoundError", err)
	}
}
