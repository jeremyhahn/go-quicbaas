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

package iam

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestAssignmentInTenant(t *testing.T) {
	sys := tenantctx.SystemTenantID
	tests := []struct {
		name   string
		a      *AdminAssignment
		tenant string
		want   bool
	}{
		{"single tenant in own", &AdminAssignment{TenantIDs: []string{"a"}}, "a", true},
		{"single tenant not in other", &AdminAssignment{TenantIDs: []string{"a"}}, "b", false},
		{"empty in system", &AdminAssignment{}, sys, true},
		{"empty not in tenant", &AdminAssignment{}, "a", false},
		{"multi not in first", &AdminAssignment{TenantIDs: []string{"a", "b"}}, "a", false},
		{"multi not in second", &AdminAssignment{TenantIDs: []string{"a", "b"}}, "b", false},
		{"nil not in tenant", nil, "a", false},
		{"nil not in system", nil, sys, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AssignmentInTenant(tt.a, tt.tenant); got != tt.want {
				t.Fatalf("AssignmentInTenant = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGrantableTenantIDs(t *testing.T) {
	const op = "test.grant"
	sys := tenantctx.SystemTenantID
	tenantCtx := tenantctx.WithTenant(context.Background(), "a")
	systemCtx := tenantctx.WithSystem(context.Background())

	ok := []struct {
		name      string
		ctx       context.Context
		requested []string
		want      []string
	}{
		{"tenant empty becomes own", tenantCtx, nil, []string{"a"}},
		{"tenant own", tenantCtx, []string{"a"}, []string{"a"}},
		{"tenant own duplicated", tenantCtx, []string{"a", "a"}, []string{"a"}},
		{"system empty stays empty", systemCtx, nil, nil},
		{"system own", systemCtx, []string{sys}, []string{sys}},
	}
	for _, tt := range ok {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GrantableTenantIDs(tt.ctx, op, tt.requested)
			if err != nil {
				t.Fatalf("GrantableTenantIDs error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("GrantableTenantIDs = %v, want %v", got, tt.want)
			}
		})
	}

	cross := []struct {
		name      string
		ctx       context.Context
		requested []string
	}{
		{"tenant other", tenantCtx, []string{"b"}},
		{"tenant own and other", tenantCtx, []string{"a", "b"}},
		{"system names tenant", systemCtx, []string{"a"}},
	}
	for _, tt := range cross {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GrantableTenantIDs(tt.ctx, op, tt.requested)
			var crossErr *tenantctx.CrossTenantError
			if !errors.As(err, &crossErr) {
				t.Fatalf("GrantableTenantIDs = %v, %v; want a cross-tenant error", got, err)
			}
		})
	}

	t.Run("unbound caller", func(t *testing.T) {
		got, err := GrantableTenantIDs(context.Background(), op, nil)
		var missing *tenantctx.MissingTenantError
		if !errors.As(err, &missing) {
			t.Fatalf("GrantableTenantIDs = %v, %v; want a missing-tenant error", got, err)
		}
	})
}
