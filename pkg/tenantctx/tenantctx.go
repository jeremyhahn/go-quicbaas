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

// Package tenantctx is the one place go-quicbaas reads and binds the tenant a
// request acts for. It holds no tenant key of its own: the tenant lives under
// go-qrdb's central key (qrdbsdk.ContextWithTenant / TenantFromContext), which
// go-qrdb's AuthMiddleware binds from the authenticated identity on every
// transport the unified server runs. A go-quicbaas transport that
// authenticates a caller itself binds the caller's tenant here, and every
// service that scopes records by tenant reads it here.
//
// AN ABSENT TENANT IS NOT THE SYSTEM TENANT. FromContext refuses a context
// with no tenant bound (MissingTenantError) instead of reading the absence as
// system reach. The system tenant is a binding like any other: an in-process
// caller that genuinely acts for the platform says so with WithSystem.
package tenantctx

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// SystemTenantID is the system tenant's identifier, go-qrdb's
// qrdbsdk.SystemTenantID.
const SystemTenantID = qrdbsdk.SystemTenantID

// MissingTenantError reports a context with no tenant bound. It is wrapped in
// an errors.QBError carrying errors.ErrForbidden, so every transport maps it to
// 403 / PermissionDenied: an operation whose tenant is unknown is refused.
type MissingTenantError struct {
	// Operation names the operation that needed the tenant.
	Operation string
}

// Error implements the error interface.
func (e *MissingTenantError) Error() string {
	return "tenantctx: " + e.Operation + ": no tenant is bound to the request"
}

// CrossTenantError reports a caller naming a tenant it may not act for. It is
// wrapped in an errors.QBError carrying errors.ErrForbidden, so every
// transport maps it to 403 / PermissionDenied.
type CrossTenantError struct {
	// Operation names the refused operation.
	Operation string
	// CallerTenant is the tenant the caller authenticated to.
	CallerTenant string
	// TargetTenant is the tenant the caller named.
	TargetTenant string
}

// Error implements the error interface.
func (e *CrossTenantError) Error() string {
	return "tenantctx: " + e.Operation + ": a caller of tenant " + e.CallerTenant +
		" may not act for tenant " + e.TargetTenant
}

// forbidden wraps a tenant refusal in the code every transport maps to 403.
func forbidden(op string, err error) error {
	return &errors.QBError{Code: errors.ErrForbidden, Op: op, Err: err}
}

// WithTenant binds tenantID as the request's tenant under go-qrdb's central
// key. An empty tenantID binds nothing, so FromContext still refuses the
// context: an unknown tenant must not be laundered into system reach.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return qrdbsdk.ContextWithTenant(ctx, tenantID)
}

// WithSystem binds the system tenant. It is for in-process callers that act
// for the platform itself (boot, background jobs, the embedded transport),
// never for a request whose caller's tenant is unknown.
func WithSystem(ctx context.Context) context.Context {
	return WithTenant(ctx, SystemTenantID)
}

// FromContext returns the authenticated tenant of the request, or a
// MissingTenantError (as an ErrForbidden QBError) when there is none. op names
// the calling operation.
//
// It reads, in order, the tenant bound under go-qrdb's central key and then
// the tenant of the authenticated go-qrdb identity. go-qrdb's AuthMiddleware
// binds both from the same identity; the identity is read too so that a path
// carrying only the identity is still confined to its tenant rather than
// refused. Neither is ever request-supplied. There is no third source: the
// go-quicbaas-private iam key is deleted, and every go-quicbaas authenticating
// path (the legacy gRPC/Unix interceptor, injectPrincipal, the MCP upstream
// projection) binds go-qrdb's key through WithTenant.
func FromContext(ctx context.Context, op string) (string, error) {
	if tenantID, ok := qrdbsdk.TenantFromContext(ctx); ok {
		return tenantID, nil
	}
	if identity := qrdbsdk.IdentityFromContext(ctx); identity != nil && identity.TenantID != "" {
		return identity.TenantID, nil
	}
	return "", forbidden(op, &MissingTenantError{Operation: op})
}

// Lookup returns the tenant bound to ctx and whether one is. It is for code
// that legitimately runs before a tenant is known — login and home-realm
// discovery — and must not be used to scope records: an absent tenant there
// is "not yet known", not "system".
func Lookup(ctx context.Context) (string, bool) {
	return qrdbsdk.TenantFromContext(ctx)
}

// IsSystem reports whether tenantID is the system tenant. Only an explicit
// binding produces it: FromContext never returns the empty string.
func IsSystem(tenantID string) bool {
	return tenantID == SystemTenantID
}

// Target resolves the tenant a request acts for when the caller names one
// (a tenant_id field, an X-Tenant-ID header). The caller's own tenant comes
// from ctx and is required.
//
//   - An empty requested tenant means the caller's own tenant.
//   - A caller may name its own tenant.
//   - Any other naming is refused with a CrossTenantError — for every caller,
//     the system tenant included. A request field is not an authorization:
//     the system tenant gets no cross-tenant lift through it, matching
//     go-qrdb, which confines tenant-scoped operations to the caller's own
//     tenant (go-quicbaas#1150).
func Target(ctx context.Context, op, requested string) (string, error) {
	caller, err := FromContext(ctx, op)
	if err != nil {
		return "", err
	}
	if requested == "" || requested == caller {
		return caller, nil
	}
	return "", forbidden(op, &CrossTenantError{Operation: op, CallerTenant: caller, TargetTenant: requested})
}

// ContextForTarget resolves Target and binds the result onto ctx, so the
// services the request reaches scope to the tenant it was authorized for.
func ContextForTarget(ctx context.Context, op, requested string) (context.Context, string, error) {
	tenantID, err := Target(ctx, op, requested)
	if err != nil {
		return ctx, "", err
	}
	return WithTenant(ctx, tenantID), tenantID, nil
}
