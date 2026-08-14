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
)

func TestTenantFromContext(t *testing.T) {
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
			name:     "context with tenant returns tenant ID",
			ctx:      ContextWithTenant(context.Background(), "tenant-123"),
			expected: "tenant-123",
		},
		{
			name:     "context with empty tenant returns empty string",
			ctx:      ContextWithTenant(context.Background(), ""),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TenantFromContext(tt.ctx)
			if result != tt.expected {
				t.Errorf("TenantFromContext() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestContextWithTenant(t *testing.T) {
	tenantID := "test-tenant-456"
	ctx := ContextWithTenant(context.Background(), tenantID)

	result := TenantFromContext(ctx)
	if result != tenantID {
		t.Errorf("ContextWithTenant() tenant = %q, want %q", result, tenantID)
	}
}

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

func TestTenantInfoFromContext(t *testing.T) {
	tests := []struct {
		name          string
		ctx           context.Context
		expectedNil   bool
		expectedID    string
		expectedValid bool
	}{
		{
			name:        "empty context returns nil",
			ctx:         context.Background(),
			expectedNil: true,
		},
		{
			name:          "context with tenant returns TenantInfo",
			ctx:           ContextWithTenant(context.Background(), "tenant-abc"),
			expectedNil:   false,
			expectedID:    "tenant-abc",
			expectedValid: true,
		},
		{
			name:        "context with empty tenant returns nil",
			ctx:         ContextWithTenant(context.Background(), ""),
			expectedNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TenantInfoFromContext(tt.ctx)
			if tt.expectedNil {
				if result != nil {
					t.Errorf("TenantInfoFromContext() = %v, want nil", result)
				}
				return
			}
			if result == nil {
				t.Error("TenantInfoFromContext() = nil, want non-nil")
				return
			}
			if result.ID != tt.expectedID {
				t.Errorf("TenantInfo.ID = %q, want %q", result.ID, tt.expectedID)
			}
			if result.Validated != tt.expectedValid {
				t.Errorf("TenantInfo.Validated = %v, want %v", result.Validated, tt.expectedValid)
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
	ctx = ContextWithTenant(ctx, "tenant-multi")
	ctx = ContextWithUser(ctx, "user-multi")
	ctx = ContextWithVectorContext(ctx, &VectorContext{
		Namespace: "ns-multi",
		Operation: "upsert",
	})

	if got := TenantFromContext(ctx); got != "tenant-multi" {
		t.Errorf("TenantFromContext() = %q, want %q", got, "tenant-multi")
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

func TestContextWithWrongType(t *testing.T) {
	// Test that wrong type in context returns empty/nil
	ctx := context.WithValue(context.Background(), tenantContextKey{}, 12345) // wrong type
	if got := TenantFromContext(ctx); got != "" {
		t.Errorf("TenantFromContext() with wrong type = %q, want empty string", got)
	}

	ctx = context.WithValue(context.Background(), userContextKey{}, 12345) // wrong type
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

func TestSwitchTenantContext_Success(t *testing.T) {
	// Create a context with user and tenant
	ctx := context.Background()
	ctx = ContextWithUser(ctx, "admin-user-123")
	ctx = ContextWithTenant(ctx, "original-tenant")

	// Switch to target tenant
	newCtx, err := SwitchTenantContext(ctx, "target-tenant", "investigating support issue #789")
	if err != nil {
		t.Fatalf("SwitchTenantContext() error = %v, want nil", err)
	}

	// Verify the context was switched
	if got := TenantFromContext(newCtx); got != "target-tenant" {
		t.Errorf("TenantFromContext() = %q, want %q", got, "target-tenant")
	}

	// Verify the SuperAdmin context was set
	sac := SuperAdminContextFromContext(newCtx)
	if sac == nil {
		t.Fatal("SuperAdminContextFromContext() = nil, want non-nil")
	}

	if sac.OriginalTenantID != "original-tenant" {
		t.Errorf("OriginalTenantID = %q, want %q", sac.OriginalTenantID, "original-tenant")
	}
	if sac.TargetTenantID != "target-tenant" {
		t.Errorf("TargetTenantID = %q, want %q", sac.TargetTenantID, "target-tenant")
	}
	if sac.Justification != "investigating support issue #789" {
		t.Errorf("Justification = %q, want %q", sac.Justification, "investigating support issue #789")
	}
	if sac.UserID != "admin-user-123" {
		t.Errorf("UserID = %q, want %q", sac.UserID, "admin-user-123")
	}
	if sac.JustificationID == "" {
		t.Error("JustificationID should not be empty")
	}
	if sac.SwitchedAt.IsZero() {
		t.Error("SwitchedAt should not be zero")
	}
}

func TestSwitchTenantContext_MissingTargetTenant(t *testing.T) {
	ctx := ContextWithUser(context.Background(), "admin-user")

	_, err := SwitchTenantContext(ctx, "", "some justification")
	if err == nil {
		t.Error("SwitchTenantContext() with empty target tenant should return error")
	}
}

func TestSwitchTenantContext_MissingJustification(t *testing.T) {
	ctx := ContextWithUser(context.Background(), "admin-user")

	_, err := SwitchTenantContext(ctx, "target-tenant", "")
	if err == nil {
		t.Error("SwitchTenantContext() with empty justification should return error")
	}
}

func TestSwitchTenantContext_MissingUser(t *testing.T) {
	ctx := context.Background() // No user

	_, err := SwitchTenantContext(ctx, "target-tenant", "some justification")
	if err == nil {
		t.Error("SwitchTenantContext() without user should return error")
	}
}

func TestSwitchTenantContext_NoOriginalTenant(t *testing.T) {
	// SuperAdmin might not have an original tenant (e.g., platform admin)
	ctx := ContextWithUser(context.Background(), "platform-admin")

	newCtx, err := SwitchTenantContext(ctx, "target-tenant", "customer support")
	if err != nil {
		t.Fatalf("SwitchTenantContext() error = %v, want nil", err)
	}

	sac := SuperAdminContextFromContext(newCtx)
	if sac == nil {
		t.Fatal("SuperAdminContextFromContext() = nil, want non-nil")
	}

	if sac.OriginalTenantID != "" {
		t.Errorf("OriginalTenantID = %q, want empty string", sac.OriginalTenantID)
	}
	if !sac.IsCrossTenantOperation() {
		t.Error("IsCrossTenantOperation() should be true for platform admin accessing tenant")
	}
}

func TestSwitchTenantContextWithAssignment_Success(t *testing.T) {
	ctx := ContextWithUser(context.Background(), "support-agent")
	ctx = ContextWithTenant(ctx, "support-tenant")

	newCtx, err := SwitchTenantContextWithAssignment(ctx, "customer-tenant", "emergency escalation", "TICKET-2024-001")
	if err != nil {
		t.Fatalf("SwitchTenantContextWithAssignment() error = %v, want nil", err)
	}

	sac := SuperAdminContextFromContext(newCtx)
	if sac == nil {
		t.Fatal("SuperAdminContextFromContext() = nil, want non-nil")
	}

	if sac.AssignmentID != "TICKET-2024-001" {
		t.Errorf("AssignmentID = %q, want %q", sac.AssignmentID, "TICKET-2024-001")
	}
}

func TestSwitchTenantContextWithAssignment_ErrorPropagation(t *testing.T) {
	// Missing user should still return error
	ctx := context.Background()

	_, err := SwitchTenantContextWithAssignment(ctx, "target", "justification", "assignment")
	if err == nil {
		t.Error("SwitchTenantContextWithAssignment() without user should return error")
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

func TestGenerateJustificationID(t *testing.T) {
	// Just verify it produces non-empty IDs
	id1 := generateJustificationID()
	if id1 == "" {
		t.Error("generateJustificationID() should not return empty string")
	}

	// Wait a tiny bit and generate another to ensure uniqueness
	id2 := generateJustificationID()
	if id2 == "" {
		t.Error("generateJustificationID() should not return empty string")
	}

	// Note: IDs might be same if called very quickly, which is acceptable
	// The format should be a timestamp "20060102150405.000000" = 21 chars
	if len(id1) != 21 {
		t.Errorf("generateJustificationID() length = %d, want 21", len(id1))
	}
}

func TestSuperAdminContextPreservesPreviousContextValues(t *testing.T) {
	// Create context with multiple values
	ctx := context.Background()
	ctx = ContextWithTenant(ctx, "original-tenant")
	ctx = ContextWithUser(ctx, "admin-user")
	ctx = ContextWithVectorContext(ctx, &VectorContext{
		Namespace: "vectors-ns",
		Operation: "search",
	})

	// Switch tenant context
	newCtx, err := SwitchTenantContext(ctx, "target-tenant", "testing")
	if err != nil {
		t.Fatalf("SwitchTenantContext() error = %v", err)
	}

	// User context should be preserved
	if got := UserFromContext(newCtx); got != "admin-user" {
		t.Errorf("UserFromContext() = %q, want %q", got, "admin-user")
	}

	// Vector context should be preserved
	vc := VectorContextFromContext(newCtx)
	if vc == nil {
		t.Error("VectorContextFromContext() = nil, want non-nil")
	} else if vc.Namespace != "vectors-ns" {
		t.Errorf("VectorContext.Namespace = %q, want %q", vc.Namespace, "vectors-ns")
	}

	// Tenant should be switched to target
	if got := TenantFromContext(newCtx); got != "target-tenant" {
		t.Errorf("TenantFromContext() = %q, want %q (should be switched)", got, "target-tenant")
	}
}
