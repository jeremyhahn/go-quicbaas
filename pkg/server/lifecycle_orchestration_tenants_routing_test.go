//go:build !integration

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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// A fleet registration arrives on a context bound to the registering tenant,
// but the tenant record lives in the tenant directory's control-plane scope.
// TenantExists must read the directory there, not in the caller's own scope,
// where no tenant record is stored (#1605).
func TestIAMTenantLister_TenantExistsUnderATenantBoundContext(t *testing.T) {
	resolver := newFakeStoreResolver(kvstore.NewMemory())
	resolver.barrierTenant("acme")
	manager := iam.NewDefaultTenantManager(iam.NewBackendStorage(newTenantRoutingKVStore(resolver)))
	if _, err := manager.CreateTenant(tenantctx.WithSystem(context.Background()),
		iam.CreateTenantOptions{ID: "acme", Name: "Acme"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	tenants := &iamTenantLister{tenants: manager}

	contexts := []struct {
		name string
		ctx  context.Context
	}{
		{"orchestration tenant", orchestration.ContextWithTenant(context.Background(), "acme")},
		{"IAM tenant", tenantctx.WithTenant(context.Background(), "acme")},
	}
	tests := []struct {
		name     string
		tenantID string
		want     bool
	}{
		{"created tenant", "acme", true},
		{"unknown tenant", "globex", false},
	}
	for _, c := range contexts {
		for _, tt := range tests {
			t.Run(c.name+"/"+tt.name, func(t *testing.T) {
				got, err := tenants.TenantExists(c.ctx, tt.tenantID)
				if err != nil || got != tt.want {
					t.Fatalf("TenantExists(%q) = %v, %v; want %v", tt.tenantID, got, err, tt.want)
				}
			})
		}
	}
}
