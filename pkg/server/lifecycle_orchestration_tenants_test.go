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

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// failingTenantManager is a tenant directory that cannot answer.
type failingTenantManager struct {
	iam.TenantManager
}

var errTenantDirectorySealed = errors.New("barrier sealed")

func (failingTenantManager) GetTenant(context.Context, string) (*iam.Tenant, error) {
	return nil, errTenantDirectorySealed
}

func TestIAMTenantLister_TenantExists(t *testing.T) {
	ctx := context.Background()
	manager := iam.NewDefaultTenantManager(iam.NewMemoryStorage())
	if _, err := manager.CreateTenant(ctx, iam.CreateTenantOptions{ID: "acme", Name: "Acme"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	tenants := &iamTenantLister{tenants: manager}

	tests := []struct {
		name     string
		tenantID string
		want     bool
	}{
		{"known tenant", "acme", true},
		{"system tenant is qrdb's, not an IAM record", "system", true},
		{"unknown tenant", "globex", false},
		{"malformed id names no tenant", "Not A Tenant!", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tenants.TenantExists(ctx, tt.tenantID)
			if err != nil || got != tt.want {
				t.Fatalf("TenantExists(%q) = %v, %v; want %v", tt.tenantID, got, err, tt.want)
			}
		})
	}
}

// catalogTenantManager answers as the routed IAM store does: the tenant
// catalog is in the system namespace, so a read bound to any other tenant
// finds no record.
type catalogTenantManager struct {
	iam.TenantManager
	known string // the one tenant the catalog holds
}

func (c catalogTenantManager) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	if bound, ok := tenantctx.Lookup(ctx); !ok || !tenantctx.IsSystem(bound) {
		return nil, iam.NewTenantNotFoundError(id)
	}
	if id != c.known {
		return nil, iam.NewTenantNotFoundError(id)
	}
	return &iam.Tenant{ID: id}, nil
}

func TestIAMTenantLister_TenantExistsReadsTheSystemCatalog(t *testing.T) {
	tenants := &iamTenantLister{tenants: catalogTenantManager{known: "acme"}}
	ctx := orchestration.ContextWithTenant(tenantctx.WithTenant(context.Background(), "acme"), "acme")
	exists, err := tenants.TenantExists(ctx, "acme")
	if err != nil || !exists {
		t.Fatalf("TenantExists from inside the tenant = %v, %v; want the catalog record found", exists, err)
	}
}

// An unknown tenant asked about from inside another tenant is still absent:
// reading the system catalog must not make every ID exist (#1609).
func TestIAMTenantLister_TenantExistsUnknownTenantFromInsideATenant(t *testing.T) {
	tenants := &iamTenantLister{tenants: catalogTenantManager{known: "acme"}}
	ctx := orchestration.ContextWithTenant(tenantctx.WithTenant(context.Background(), "acme"), "acme")
	exists, err := tenants.TenantExists(ctx, "globex")
	if err != nil || exists {
		t.Fatalf("TenantExists(globex) from inside acme = %v, %v; want false, nil", exists, err)
	}
}

func TestIAMTenantLister_TenantExistsReportsAnUnansweringDirectory(t *testing.T) {
	tenants := &iamTenantLister{tenants: failingTenantManager{}}
	exists, err := tenants.TenantExists(context.Background(), "acme")
	if !errors.Is(err, errTenantDirectorySealed) || exists {
		t.Fatalf("TenantExists = %v, %v; want the directory failure, never a false \"absent\"", exists, err)
	}
}
