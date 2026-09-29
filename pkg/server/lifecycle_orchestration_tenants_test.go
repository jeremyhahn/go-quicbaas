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

func TestIAMTenantLister_TenantExistsReportsAnUnansweringDirectory(t *testing.T) {
	tenants := &iamTenantLister{tenants: failingTenantManager{}}
	exists, err := tenants.TenantExists(context.Background(), "acme")
	if !errors.Is(err, errTenantDirectorySealed) || exists {
		t.Fatalf("TenantExists = %v, %v; want the directory failure, never a false \"absent\"", exists, err)
	}
}
