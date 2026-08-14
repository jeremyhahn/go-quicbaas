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

package orchestration

import (
	"context"
	"errors"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Sentinel errors for tenant guard operations.
var (
	// ErrTenantAccessDenied indicates cross-tenant access was blocked.
	ErrTenantAccessDenied = errors.New("orchestration: tenant access denied")

	// ErrTenantRequired indicates tenant ID is missing from context.
	ErrTenantRequired = errors.New("orchestration: tenant ID required")

	// ErrSuperAdminBlocked indicates a SuperAdmin tried to access orchestration data.
	ErrSuperAdminBlocked = errors.New("orchestration: super admin access blocked for orchestration")

	// ErrInvalidTenantPrefix indicates a key does not have a valid tenant prefix.
	ErrInvalidTenantPrefix = errors.New("orchestration: invalid tenant prefix")
)

// TenantAccessDeniedError indicates cross-tenant access was blocked.
type TenantAccessDeniedError struct {
	RequestingTenant string
	TargetTenant     string
}

// Error implements the error interface.
func (e *TenantAccessDeniedError) Error() string {
	return ErrTenantAccessDenied.Error() + ": requesting=" + e.RequestingTenant + " target=" + e.TargetTenant
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TenantAccessDeniedError) Unwrap() error {
	return ErrTenantAccessDenied
}

// TenantRequiredError indicates tenant ID is missing from context.
type TenantRequiredError struct {
	Operation string
}

// Error implements the error interface.
func (e *TenantRequiredError) Error() string {
	return ErrTenantRequired.Error() + ": " + e.Operation
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TenantRequiredError) Unwrap() error {
	return ErrTenantRequired
}

// SuperAdminBlockedError indicates a SuperAdmin tried to access orchestration data.
type SuperAdminBlockedError struct {
	Operation string
}

// Error implements the error interface.
func (e *SuperAdminBlockedError) Error() string {
	return ErrSuperAdminBlocked.Error() + ": " + e.Operation
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SuperAdminBlockedError) Unwrap() error {
	return ErrSuperAdminBlocked
}

// InvalidTenantPrefixError indicates a key has an invalid or cross-tenant prefix.
type InvalidTenantPrefixError struct {
	Key      string
	TenantID string
}

// Error implements the error interface.
func (e *InvalidTenantPrefixError) Error() string {
	return ErrInvalidTenantPrefix.Error() + ": key=" + e.Key + " tenant=" + e.TenantID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidTenantPrefixError) Unwrap() error {
	return ErrInvalidTenantPrefix
}

// tenantContextKey is a context key for the tenant ID.
// This is local to the orchestration package to avoid IAM build tag dependency.
type tenantContextKey struct{}

// superAdminContextKey is a context key for super admin status.
type superAdminContextKey struct{}

// TenantFromContext extracts the tenant ID from the orchestration context.
func TenantFromContext(ctx context.Context) string {
	if v := ctx.Value(tenantContextKey{}); v != nil {
		if tenantID, ok := v.(string); ok {
			return tenantID
		}
	}
	return ""
}

// SystemTenantID is the reserved tenant a record is filed under when the
// caller supplied none. It mirrors qrdb's system tenant, which the REST, gRPC
// and MCP transports already substitute before a request reaches the
// orchestrator, so an untenanted in-process caller and an untenanted request
// file their records in the same scope rather than in two different ones.
//
// The value is duplicated as a literal (rather than imported from
// qrdbsdk.SystemTenantID) because this package is reachable from the
// dependency-light standalone agent binary (cmd/quicbaas agent-serve ->
// pkg/agent -> pkg/orchestration), which deliberately imports neither
// go-qrdb/sdk/go nor pkg/iam. A drift between the two literals is caught by
// TestOrchestrationSystemTenantIDMatchesQRDB in tenant_guard_test.go.
const SystemTenantID = "system"

// StorageTenant returns the tenant a durable orchestration record belongs to.
//
// Every record key begins with its owning tenant, so an empty segment would
// collapse distinct tenants' keyspaces. Callers that reach the orchestrator
// without a tenant — in-process embedded use, and unit tests — are therefore
// resolved to the system tenant, the same identifier the transports already
// substitute for an untenanted request.
func StorageTenant(ctx context.Context) string {
	if tenantID := TenantFromContext(ctx); tenantID != "" {
		return tenantID
	}
	return SystemTenantID
}

// ContextWithTenant returns a new context with the tenant ID set for orchestration.
func ContextWithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// IsSuperAdmin checks if the context indicates a super admin.
func IsSuperAdmin(ctx context.Context) bool {
	if v := ctx.Value(superAdminContextKey{}); v != nil {
		if isAdmin, ok := v.(bool); ok {
			return isAdmin
		}
	}
	return false
}

// ContextWithSuperAdmin returns a new context with the super admin flag set.
func ContextWithSuperAdmin(ctx context.Context, isAdmin bool) context.Context {
	return context.WithValue(ctx, superAdminContextKey{}, isAdmin)
}

// TenantGuard enforces strict tenant isolation for all orchestration operations.
// Unlike other QuicBaaS services, SuperAdmin CANNOT access LLM agent data
// across tenants. This is enforced at code level, not policy level.
type TenantGuard struct {
	logger interfaces.Logger
}

// NewTenantGuard creates a new TenantGuard.
func NewTenantGuard(logger interfaces.Logger) *TenantGuard {
	return &TenantGuard{logger: logger}
}

// ValidateTenantAccess checks that the requesting tenant matches the target resource's tenant.
// Returns an error if:
//   - Tenant ID is empty (unauthenticated)
//   - Target tenant differs from requesting tenant (cross-tenant access)
//   - The request has SuperAdmin context (SuperAdmin is blocked for orchestration)
func (g *TenantGuard) ValidateTenantAccess(ctx context.Context, targetTenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Block SuperAdmin access for orchestration operations
	if IsSuperAdmin(ctx) {
		g.logger.Warnf("super admin access blocked for orchestration: target=%s", targetTenantID)
		return &SuperAdminBlockedError{Operation: "ValidateTenantAccess"}
	}

	requestingTenant := TenantFromContext(ctx)
	if requestingTenant == "" {
		return &TenantRequiredError{Operation: "ValidateTenantAccess"}
	}

	if targetTenantID == "" {
		return &TenantRequiredError{Operation: "ValidateTenantAccess (target)"}
	}

	if requestingTenant != targetTenantID {
		g.logger.Warnf("cross-tenant access blocked: requesting=%s target=%s", requestingTenant, targetTenantID)
		return &TenantAccessDeniedError{
			RequestingTenant: requestingTenant,
			TargetTenant:     targetTenantID,
		}
	}

	return nil
}

// IsSuperAdminBlocked returns true always for orchestration operations.
// This function exists to make the policy explicit and testable.
func (g *TenantGuard) IsSuperAdminBlocked() bool {
	return true
}

// EnforceTenantPrefix ensures a KV key is prefixed with the tenant ID.
// Prevents agents from accessing data outside their tenant namespace.
// If the key already has the correct tenant prefix, it is returned as-is.
// If the key has no tenant prefix, the tenant prefix is prepended.
// Keys are always guaranteed to start with "tenantID/" on success.
func (g *TenantGuard) EnforceTenantPrefix(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Block SuperAdmin
	if IsSuperAdmin(ctx) {
		return "", &SuperAdminBlockedError{Operation: "EnforceTenantPrefix"}
	}

	tenantID := TenantFromContext(ctx)
	if tenantID == "" {
		return "", &TenantRequiredError{Operation: "EnforceTenantPrefix"}
	}

	if key == "" {
		return "", &InvalidTenantPrefixError{Key: key, TenantID: tenantID}
	}

	// Reject keys containing path traversal sequences
	if strings.Contains(key, "..") {
		return "", &InvalidTenantPrefixError{Key: key, TenantID: tenantID}
	}

	prefix := tenantID + "/"

	// Key already has the correct prefix; return as-is
	if strings.HasPrefix(key, prefix) {
		return key, nil
	}

	// Always prepend the tenant prefix to guarantee namespace isolation.
	// Any key not already prefixed gets the tenant prefix added, ensuring
	// all data is stored under the calling tenant's namespace.
	return prefix + key, nil
}
