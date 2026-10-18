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
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// TestHandleIAMTenantUpdate_RefusesUnstoredFields proves the embedded
// handler refuses an update that sets description, metadata or status, even
// alongside a DisplayName, and stores nothing (#1518).
func TestHandleIAMTenantUpdate_RefusesUnstoredFields(t *testing.T) {
	status := iamsdk.TenantStatusActive
	cases := map[string]*iamsdk.UpdateTenantRequest{
		"description": {DisplayName: "New", Description: "d"},
		"metadata":    {DisplayName: "New", Metadata: map[string]string{"k": "v"}},
		"status":      {DisplayName: "New", Status: &status},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mgr := iam.NewDefaultTenantManager(iam.NewMemoryStorage())
			if _, err := mgr.CreateTenant(ctx, iam.CreateTenantOptions{
				ID: "acme", Name: "acme", Settings: &iam.TenantSettings{DisplayName: "Old Name"},
			}); err != nil {
				t.Fatalf("CreateTenant: %v", err)
			}
			svc := &mockQuicBaaSForTenants{tenants: mgr}

			_, err := handleIAMTenantUpdate(ctx, svc, &embeddedUpdateTenantEnvelope{TenantID: "acme", Request: req})

			var refused *iam.TenantFieldNotStoredError
			if !errors.As(err, &refused) || len(refused.Fields) != 1 || refused.Fields[0] != name {
				t.Fatalf("err = %v, want TenantFieldNotStoredError{%s}", err, name)
			}
			read, err := handleIAMTenantGet(ctx, svc, &embeddedUpdateTenantEnvelope{TenantID: "acme"})
			if err != nil {
				t.Fatalf("handleIAMTenantGet: %v", err)
			}
			if got := read.(*iamsdk.Tenant).DisplayName; got != "Old Name" {
				t.Fatalf("DisplayName = %q after a refused update, want Old Name", got)
			}
		})
	}
}
