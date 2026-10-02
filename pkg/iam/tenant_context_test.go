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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestUserFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "empty context returns empty string",
			ctx:      context.Background(),
			expected: "",
		},
		{
			name:     "context with user returns user ID",
			ctx:      ContextWithUser(context.Background(), "user-789"),
			expected: "user-789",
		},
		{
			name:     "context with empty user returns empty string",
			ctx:      ContextWithUser(context.Background(), ""),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UserFromContext(tt.ctx)
			if result != tt.expected {
				t.Errorf("UserFromContext() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestContextWithUser(t *testing.T) {
	userID := "test-user-123"
	ctx := ContextWithUser(context.Background(), userID)

	result := UserFromContext(ctx)
	if result != userID {
		t.Errorf("ContextWithUser() user = %q, want %q", result, userID)
	}
}

func TestVectorContextFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected *VectorContext
	}{
		{
			name:     "empty context returns nil",
			ctx:      context.Background(),
			expected: nil,
		},
		{
			name: "context with vector context returns vector context",
			ctx: ContextWithVectorContext(context.Background(), &VectorContext{
				Namespace: "test-ns",
				Operation: "search",
				BatchSize: 10,
			}),
			expected: &VectorContext{
				Namespace: "test-ns",
				Operation: "search",
				BatchSize: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VectorContextFromContext(tt.ctx)
			if tt.expected == nil && result != nil {
				t.Errorf("VectorContextFromContext() = %v, want nil", result)
			}
			if tt.expected != nil && result == nil {
				t.Errorf("VectorContextFromContext() = nil, want %v", tt.expected)
			}
			if tt.expected != nil && result != nil {
				if result.Namespace != tt.expected.Namespace {
					t.Errorf("Namespace = %q, want %q", result.Namespace, tt.expected.Namespace)
				}
				if result.Operation != tt.expected.Operation {
					t.Errorf("Operation = %q, want %q", result.Operation, tt.expected.Operation)
				}
				if result.BatchSize != tt.expected.BatchSize {
					t.Errorf("BatchSize = %d, want %d", result.BatchSize, tt.expected.BatchSize)
				}
			}
		})
	}
}

func TestIsSameTenant(t *testing.T) {
	tests := []struct {
		name     string
		tenantA  string
		tenantB  string
		expected bool
	}{
		{
			name:     "both empty returns true (single-tenant mode)",
			tenantA:  "",
			tenantB:  "",
			expected: true,
		},
		{
			name:     "same tenant returns true",
			tenantA:  "tenant-123",
			tenantB:  "tenant-123",
			expected: true,
		},
		{
			name:     "different tenants returns false",
			tenantA:  "tenant-123",
			tenantB:  "tenant-456",
			expected: false,
		},
		{
			name:     "one empty one not returns false",
			tenantA:  "tenant-123",
			tenantB:  "",
			expected: false,
		},
		{
			name:     "one empty one not (reversed) returns false",
			tenantA:  "",
			tenantB:  "tenant-123",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSameTenant(tt.tenantA, tt.tenantB)
			if result != tt.expected {
				t.Errorf("IsSameTenant(%q, %q) = %v, want %v", tt.tenantA, tt.tenantB, result, tt.expected)
			}
		})
	}
}

func TestRequiresTenantIsolation(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		expected bool
	}{
		{
			name:     "empty tenant does not require isolation",
			tenantID: "",
			expected: false,
		},
		{
			name:     "literal system tenant does not require isolation",
			tenantID: SystemTenantID,
			expected: false,
		},
		{
			name:     "non-empty tenant requires isolation",
			tenantID: "tenant-123",
			expected: true,
		},
		{
			name:     "different-case lookalike still requires isolation",
			tenantID: "System",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RequiresTenantIsolation(tt.tenantID)
			if result != tt.expected {
				t.Errorf("RequiresTenantIsolation(%q) = %v, want %v", tt.tenantID, result, tt.expected)
			}
		})
	}
}

func TestCombinedContext(t *testing.T) {
	// Test that multiple context values can coexist
	ctx := context.Background()
	ctx = tenantctx.WithTenant(ctx, "tenant-multi")
	ctx = ContextWithUser(ctx, "user-multi")
	ctx = ContextWithVectorContext(ctx, &VectorContext{
		Namespace: "ns-multi",
		Operation: "upsert",
	})

	if got := contextTenant(ctx); got != "tenant-multi" {
		t.Errorf("contextTenant() = %q, want %q", got, "tenant-multi")
	}
	if got := UserFromContext(ctx); got != "user-multi" {
		t.Errorf("UserFromContext() = %q, want %q", got, "user-multi")
	}
	vc := VectorContextFromContext(ctx)
	if vc == nil {
		t.Error("VectorContextFromContext() = nil, want non-nil")
	} else if vc.Namespace != "ns-multi" {
		t.Errorf("VectorContext.Namespace = %q, want %q", vc.Namespace, "ns-multi")
	}
}

// TestContextTenant pins the login plane's read of go-qrdb's central tenant
// binding: the bound tenant when there is one, and "" — "not yet known", never
// the system tenant — when there is none.
func TestContextTenant(t *testing.T) {
	if got := contextTenant(context.Background()); got != "" {
		t.Errorf("contextTenant() unbound = %q, want empty string", got)
	}
	if got := contextTenant(tenantctx.WithTenant(context.Background(), "")); got != "" {
		t.Errorf("contextTenant() bound to empty = %q, want empty string", got)
	}
	if got := contextTenant(tenantctx.WithTenant(context.Background(), "acme")); got != "acme" {
		t.Errorf("contextTenant() = %q, want acme", got)
	}
}

func TestContextWithWrongType(t *testing.T) {
	// Test that wrong type in context returns empty/nil
	ctx := context.WithValue(context.Background(), userContextKey{}, 12345) // wrong type
	if got := UserFromContext(ctx); got != "" {
		t.Errorf("UserFromContext() with wrong type = %q, want empty string", got)
	}

	ctx = context.WithValue(context.Background(), vectorContextKey{}, "wrong type") // wrong type
	if got := VectorContextFromContext(ctx); got != nil {
		t.Errorf("VectorContextFromContext() with wrong type = %v, want nil", got)
	}
}

// Tests for SuperAdminContext

func TestSuperAdminContext_IsCrossTenantOperation(t *testing.T) {
	tests := []struct {
		name     string
		sac      *SuperAdminContext
		expected bool
	}{
		{
			name:     "nil context returns false",
			sac:      nil,
			expected: false,
		},
		{
			name: "same tenant returns false",
			sac: &SuperAdminContext{
				OriginalTenantID: "tenant-a",
				TargetTenantID:   "tenant-a",
			},
			expected: false,
		},
		{
			name: "different tenant returns true",
			sac: &SuperAdminContext{
				OriginalTenantID: "tenant-a",
				TargetTenantID:   "tenant-b",
			},
			expected: true,
		},
		{
			name: "empty original tenant returns true when target is set",
			sac: &SuperAdminContext{
				OriginalTenantID: "",
				TargetTenantID:   "tenant-b",
			},
			expected: true,
		},
		{
			name: "both empty returns false",
			sac: &SuperAdminContext{
				OriginalTenantID: "",
				TargetTenantID:   "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sac.IsCrossTenantOperation()
			if result != tt.expected {
				t.Errorf("IsCrossTenantOperation() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSuperAdminContextFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		wantNil  bool
		wantUser string
	}{
		{
			name:    "empty context returns nil",
			ctx:     context.Background(),
			wantNil: true,
		},
		{
			name: "context with SuperAdmin context returns it",
			ctx: ContextWithSuperAdmin(context.Background(), &SuperAdminContext{
				OriginalTenantID: "tenant-a",
				TargetTenantID:   "tenant-b",
				UserID:           "admin-123",
			}),
			wantNil:  false,
			wantUser: "admin-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SuperAdminContextFromContext(tt.ctx)
			if tt.wantNil {
				if result != nil {
					t.Errorf("SuperAdminContextFromContext() = %v, want nil", result)
				}
			} else {
				if result == nil {
					t.Error("SuperAdminContextFromContext() = nil, want non-nil")
				} else if result.UserID != tt.wantUser {
					t.Errorf("SuperAdminContext.UserID = %q, want %q", result.UserID, tt.wantUser)
				}
			}
		})
	}
}

func TestContextWithSuperAdmin(t *testing.T) {
	sac := &SuperAdminContext{
		OriginalTenantID: "original-tenant",
		TargetTenantID:   "target-tenant",
		Justification:    "support ticket #12345",
		UserID:           "super-admin-1",
	}

	ctx := ContextWithSuperAdmin(context.Background(), sac)
	result := SuperAdminContextFromContext(ctx)

	if result == nil {
		t.Fatal("SuperAdminContextFromContext() = nil, want non-nil")
	}

	if result.OriginalTenantID != "original-tenant" {
		t.Errorf("OriginalTenantID = %q, want %q", result.OriginalTenantID, "original-tenant")
	}
	if result.TargetTenantID != "target-tenant" {
		t.Errorf("TargetTenantID = %q, want %q", result.TargetTenantID, "target-tenant")
	}
	if result.Justification != "support ticket #12345" {
		t.Errorf("Justification = %q, want %q", result.Justification, "support ticket #12345")
	}
	if result.UserID != "super-admin-1" {
		t.Errorf("UserID = %q, want %q", result.UserID, "super-admin-1")
	}
}

func TestSuperAdminContextFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), superAdminContextKey{}, "wrong type")
	result := SuperAdminContextFromContext(ctx)
	if result != nil {
		t.Errorf("SuperAdminContextFromContext() with wrong type = %v, want nil", result)
	}
}

func TestIsSuperAdminOperation(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected bool
	}{
		{
			name:     "empty context returns false",
			ctx:      context.Background(),
			expected: false,
		},
		{
			name: "context with same tenant SuperAdmin returns false",
			ctx: ContextWithSuperAdmin(context.Background(), &SuperAdminContext{
				OriginalTenantID: "tenant-a",
				TargetTenantID:   "tenant-a",
			}),
			expected: false,
		},
		{
			name: "context with cross-tenant SuperAdmin returns true",
			ctx: ContextWithSuperAdmin(context.Background(), &SuperAdminContext{
				OriginalTenantID: "tenant-a",
				TargetTenantID:   "tenant-b",
			}),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSuperAdminOperation(tt.ctx)
			if result != tt.expected {
				t.Errorf("IsSuperAdminOperation() = %v, want %v", result, tt.expected)
			}
		})
	}
}
