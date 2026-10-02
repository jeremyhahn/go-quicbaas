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

package iam

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TenantScopeMismatchError is returned when a caller requests access to a
// tenant_id other than its own authenticated tenant.
type TenantScopeMismatchError struct {
	// CallerTenant is the caller's own authenticated tenant.
	CallerTenant string
	// RequestedTenant is the tenant_id the caller asked to operate against.
	RequestedTenant string
}

// Error implements the error interface.
func (e *TenantScopeMismatchError) Error() string {
	return "tenant scope: caller tenant " + e.CallerTenant + " may not access tenant " + e.RequestedTenant
}

// ResolveRequestTenant enforces the single tenant-scoping contract every
// audit surface (Query/Get/Delete/Record — REST and gRPC alike) applies to
// a caller-supplied tenant_id: the caller reads/writes ONLY its own
// authenticated tenant (TenantFromContext, bound by the REST/gRPC auth
// middleware from the verified identity — never client-supplied). An empty
// requestedTenant defaults to it; any other value must equal it.
//
// There is deliberately NO system-tenant lift here. qrdb's permission
// scope model is authoritative (go-qrdb pkg/permission: ScopeSelf /
// ScopeCross / ScopeTenantOnly): a system-tenant principal reaches another
// tenant's data only through a surface gated on a permission explicitly
// registered ScopeCross (e.g. qrdb's audit:read:cross), and no surface
// routed through this helper is gated on one — the REST PEP maps the
// audit routes onto the self-scoped audit:query / audit:read /
// audit_events:record (plus the handler-level audit:delete check), and
// the gRPC AuditService methods are unmapped, which is default-deny under
// RequireAuthz. A tenant's audit chain is tenant data, and the platform
// operator holds no implicit right to it — the same operator-isolation
// ruling qrdb's ScopeTenantOnly states as "the platform operator must not
// hold tenant key material". pkg/api/rest's requireTenantSubject applies
// this same rule to the session surface: a system-tenant principal never
// gains implicit reach into a tenant's namespace.
//
// An UNBOUND caller tenant is refused (tenantctx.FromContext): an empty
// tenant must never reach storage as the resolved target — the node-backed
// audit logger applies no tenant equality of its own to an empty filter, so
// passing "" through would turn "default to the caller's tenant" into an
// unscoped read of every chain — and an unknown caller is not the system
// principal either (go-quicbaas#1150).
//
// This is the ONE place the rule is stated; pkg/api/grpc and pkg/api/rest
// both call it rather than carrying their own copies, so the read path
// (Query/Get/Delete) and the write path (Record) can never drift apart. A
// future genuinely cross-tenant surface must be gated on its own
// ScopeCross permission and resolve its target tenant deliberately under
// that gate — never by re-adding a caller-identity lift here.
func ResolveRequestTenant(ctx context.Context, requestedTenant string) (string, error) {
	callerTenant, err := tenantctx.FromContext(ctx, "iam.ResolveRequestTenant")
	if err != nil {
		return "", err
	}
	if requestedTenant == "" {
		return callerTenant, nil
	}
	if requestedTenant == callerTenant {
		return requestedTenant, nil
	}
	return "", &TenantScopeMismatchError{CallerTenant: callerTenant, RequestedTenant: requestedTenant}
}
