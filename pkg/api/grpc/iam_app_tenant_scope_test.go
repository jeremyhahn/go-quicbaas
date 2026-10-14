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

package grpc

import (
	"context"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestIAMServiceAdapter_AppVerbsStayInTenant proves the gRPC app verbs look
// the client up inside the tenant the handler resolved: another tenant's
// client id answers not-found and leaves that tenant's app untouched, while
// the owning tenant succeeds.
func TestIAMServiceAdapter_AppVerbsStayInTenant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	appManager := iam.NewAppManager(iam.NewMemoryStorage(), nil)
	adapter := NewIAMServiceAdapter(nil, nil, nil, appManager, nil, nil)

	victim, err := appManager.RegisterApp(ctx, "tenant-b", iam.RegisterAppOptions{
		Name:         "Victim",
		RedirectURIs: []string{"https://victim.example/callback"},
	})
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	if _, err := adapter.GetApp(ctx, "tenant-a", victim.ClientID); !dberrors.IsNotFound(err) {
		t.Fatalf("cross-tenant GetApp: want not-found, got %v", err)
	}
	if _, err := adapter.UpdateApp(ctx, &iam.Application{TenantID: "tenant-a", ClientID: victim.ClientID, Name: "Hijacked"}); !dberrors.IsNotFound(err) {
		t.Fatalf("cross-tenant UpdateApp: want not-found, got %v", err)
	}
	if _, err := adapter.RotateAppSecret(ctx, "tenant-a", victim.ClientID); !dberrors.IsNotFound(err) {
		t.Fatalf("cross-tenant RotateAppSecret: want not-found, got %v", err)
	}
	if err := adapter.DeleteApp(ctx, "tenant-a", victim.ClientID); !dberrors.IsNotFound(err) {
		t.Fatalf("cross-tenant DeleteApp: want not-found, got %v", err)
	}
	if _, err := appManager.ValidateClient(ctx, victim.ClientID, victim.ClientSecret); err != nil {
		t.Fatalf("refused cross-tenant verbs must leave the secret valid: %v", err)
	}

	got, err := adapter.GetApp(ctx, "tenant-b", victim.ClientID)
	if err != nil {
		t.Fatalf("owner GetApp: %v", err)
	}
	if got.Name != "Victim" {
		t.Fatalf("refused cross-tenant update renamed the app to %q", got.Name)
	}
	if _, err := adapter.UpdateApp(ctx, &iam.Application{TenantID: "tenant-b", ClientID: victim.ClientID, Name: "Renamed"}); err != nil {
		t.Fatalf("owner UpdateApp: %v", err)
	}
	if _, err := adapter.RotateAppSecret(ctx, "tenant-b", victim.ClientID); err != nil {
		t.Fatalf("owner RotateAppSecret: %v", err)
	}
	if err := adapter.DeleteApp(ctx, "tenant-b", victim.ClientID); err != nil {
		t.Fatalf("owner DeleteApp: %v", err)
	}
}
