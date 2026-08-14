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
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Context key types for type safety.
type tenantContextKey struct{}
type userContextKey struct{}
type vectorContextKey struct{}
type superAdminContextKey struct{}

// TenantFromContext extracts the tenant ID from the context.
// Returns empty string if not set.
func TenantFromContext(ctx context.Context) string {
	if v := ctx.Value(tenantContextKey{}); v != nil {
		if tenantID, ok := v.(string); ok {
			return tenantID
		}
	}
	return ""
}

// ContextWithTenant returns a new context with the tenant ID set.
func ContextWithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// UserFromContext extracts the user ID from the context.
// Returns empty string if not set.
func UserFromContext(ctx context.Context) string {
	if v := ctx.Value(userContextKey{}); v != nil {
		if userID, ok := v.(string); ok {
			return userID
		}
	}
	return ""
}

// ContextWithUser returns a new context with the user ID set.
func ContextWithUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userContextKey{}, userID)
}

// VectorContext contains vector-specific context for ABAC evaluation.
type VectorContext struct {
	// Namespace is the target namespace for this operation.
	Namespace string

	// VectorID is the target vector ID (if applicable).
	VectorID string

	// Operation is the operation being performed (upsert, search, delete, etc.).
	Operation string

	// BatchSize is the number of vectors in batch operations.
	BatchSize int

	// VectorMetadata contains the vector's metadata (for filtering).
	VectorMetadata map[string]interface{}
}

// VectorContextFromContext extracts the vector context from the context.
// Returns nil if not set.
func VectorContextFromContext(ctx context.Context) *VectorContext {
	if v := ctx.Value(vectorContextKey{}); v != nil {
		if vc, ok := v.(*VectorContext); ok {
			return vc
		}
	}
	return nil
}

// ContextWithVectorContext returns a new context with the vector context set.
func ContextWithVectorContext(ctx context.Context, vc *VectorContext) context.Context {
	return context.WithValue(ctx, vectorContextKey{}, vc)
}

// TenantInfo contains tenant information extracted from a request.
type TenantInfo struct {
	// ID is the tenant identifier.
	ID string

	// Name is the tenant name (if available).
	Name string

	// Validated indicates whether the tenant has been validated.
	Validated bool
}

// TenantInfoFromContext extracts tenant info from the context.
// This provides more details than TenantFromContext.
func TenantInfoFromContext(ctx context.Context) *TenantInfo {
	tenantID := TenantFromContext(ctx)
	if tenantID == "" {
		return nil
	}
	return &TenantInfo{
		ID:        tenantID,
		Validated: true,
	}
}

// IsSameTenant checks if two tenant IDs match.
// Empty tenants match (single-tenant mode).
func IsSameTenant(tenantA, tenantB string) bool {
	if tenantA == "" && tenantB == "" {
		return true // Single-tenant mode
	}
	return tenantA == tenantB
}

// RequiresTenantIsolation returns true if tenant isolation is required.
// Returns false for the system tenant — the empty string or the literal
// SystemTenantID — since both denote the structurally privileged
// control-plane scope rather than an isolated tenant namespace. See
// qrdbsdk.IsSystemTenant (go-qrdb/sdk/go) for the canonical predicate.
func RequiresTenantIsolation(tenantID string) bool {
	return !qrdbsdk.IsSystemTenant(tenantID)
}

// SuperAdminContext contains information about a SuperAdmin's cross-tenant access.
// This is used for audit logging and access control when a SuperAdmin switches
// to operate on a different tenant's data.
type SuperAdminContext struct {
	// OriginalTenantID is the SuperAdmin's original tenant context.
	OriginalTenantID string

	// TargetTenantID is the tenant that the SuperAdmin is operating on.
	TargetTenantID string

	// Justification is the reason for the cross-tenant access.
	Justification string

	// JustificationID is a unique identifier for this justification.
	JustificationID string

	// SwitchedAt is the timestamp when the tenant switch occurred.
	SwitchedAt time.Time

	// AssignmentID is an optional reference to a support ticket or work order.
	AssignmentID string

	// UserID is the SuperAdmin's user ID.
	UserID string
}

// IsCrossTenantOperation returns true if operating on a different tenant.
func (sac *SuperAdminContext) IsCrossTenantOperation() bool {
	if sac == nil {
		return false
	}
	return sac.OriginalTenantID != sac.TargetTenantID
}

// SuperAdminContextFromContext extracts the SuperAdmin context from the context.
// Returns nil if not set (normal operation, not a SuperAdmin switch).
func SuperAdminContextFromContext(ctx context.Context) *SuperAdminContext {
	if v := ctx.Value(superAdminContextKey{}); v != nil {
		if sac, ok := v.(*SuperAdminContext); ok {
			return sac
		}
	}
	return nil
}

// ContextWithSuperAdmin returns a new context with the SuperAdmin context set.
func ContextWithSuperAdmin(ctx context.Context, sac *SuperAdminContext) context.Context {
	return context.WithValue(ctx, superAdminContextKey{}, sac)
}

// SwitchTenantContext switches the tenant context for a SuperAdmin operation.
// This creates a new context with:
// 1. The target tenant ID set as the current tenant
// 2. A SuperAdminContext recording the switch for audit purposes
//
// Returns an error if:
// - targetTenantID is empty
// - justification is empty (required for audit)
// - the current context has no user (SuperAdmin must be identified).
func SwitchTenantContext(ctx context.Context, targetTenantID, justification string) (context.Context, error) {
	if targetTenantID == "" {
		return nil, errors.NewMissingTenantContextError("SwitchTenantContext")
	}
	if justification == "" {
		return nil, errors.NewMissingJustificationError("SwitchTenantContext")
	}

	// Get the current user (SuperAdmin must be identified)
	userID := UserFromContext(ctx)
	if userID == "" {
		return nil, errors.NewMissingUserContextError("SwitchTenantContext")
	}

	// Get the original tenant context
	originalTenantID := TenantFromContext(ctx)

	// Create the SuperAdmin context for audit
	sac := &SuperAdminContext{
		OriginalTenantID: originalTenantID,
		TargetTenantID:   targetTenantID,
		Justification:    justification,
		JustificationID:  generateJustificationID(),
		SwitchedAt:       time.Now(),
		UserID:           userID,
	}

	// Create new context with target tenant and SuperAdmin context
	newCtx := ContextWithTenant(ctx, targetTenantID)
	newCtx = ContextWithSuperAdmin(newCtx, sac)

	return newCtx, nil
}

// SwitchTenantContextWithAssignment is like SwitchTenantContext but includes
// an assignment ID (e.g., a support ticket number or work order).
func SwitchTenantContextWithAssignment(ctx context.Context, targetTenantID, justification, assignmentID string) (context.Context, error) {
	newCtx, err := SwitchTenantContext(ctx, targetTenantID, justification)
	if err != nil {
		return nil, err
	}

	// Update the SuperAdmin context with the assignment ID
	sac := SuperAdminContextFromContext(newCtx)
	if sac != nil {
		sac.AssignmentID = assignmentID
	}

	return newCtx, nil
}

// IsSuperAdminOperation returns true if the current context is a SuperAdmin
// cross-tenant operation.
func IsSuperAdminOperation(ctx context.Context) bool {
	sac := SuperAdminContextFromContext(ctx)
	return sac != nil && sac.IsCrossTenantOperation()
}

// generateJustificationID creates a unique ID for the justification.
// This is used for audit trail correlation.
func generateJustificationID() string {
	return time.Now().UTC().Format("20060102150405.000000")
}
