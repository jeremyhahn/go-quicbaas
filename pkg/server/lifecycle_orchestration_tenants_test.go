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
	ids map[string]bool
}

func (m catalogTenantManager) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	if bound, ok := tenantctx.Lookup(ctx); !ok || !tenantctx.IsSystem(bound) || !m.ids[id] {
		return nil, iam.NewTenantNotFoundError(id)
	}
	return &iam.Tenant{ID: id}, nil
}

// TestIAMTenantLister_TenantExistsReadsTheSystemCatalog is #1609: a caller
// bound to a non-system tenant still finds the tenant IAM created, and an
// unknown tenant is still absent.
func TestIAMTenantLister_TenantExistsReadsTheSystemCatalog(t *testing.T) {
	tenants := &iamTenantLister{tenants: catalogTenantManager{ids: map[string]bool{"acme": true}}}
	ctx := orchestration.ContextWithTenant(tenantctx.WithTenant(context.Background(), "acme"), "acme")

	tests := []struct {
		name     string
		tenantID string
		want     bool
	}{
		{"the caller's own tenant", "acme", true},
		{"an unknown tenant", "globex", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tenants.TenantExists(ctx, tt.tenantID)
			if err != nil || got != tt.want {
				t.Fatalf("TenantExists(%q) from inside tenant acme = %v, %v; want %v", tt.tenantID, got, err, tt.want)
			}
		})
	}
}

func TestIAMTenantLister_TenantExistsReportsAnUnansweringDirectory(t *testing.T) {
	tenants := &iamTenantLister{tenants: failingTenantManager{}}
	exists, err := tenants.TenantExists(context.Background(), "acme")
	if !errors.Is(err, errTenantDirectorySealed) || exists {
		t.Fatalf("TenantExists = %v, %v; want the directory failure, never a false \"absent\"", exists, err)
	}
}
