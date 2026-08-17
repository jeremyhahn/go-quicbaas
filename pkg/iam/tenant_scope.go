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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// TenantScopeMismatchError is returned when a caller requests access to a
// tenant_id other than its own authenticated tenant without holding
// cross-tenant (system-tenant) scope.
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
// a caller-supplied tenant_id: the caller reads/writes its own
// authenticated tenant (TenantFromContext, bound by the REST/gRPC auth
// middleware from the verified identity — never client-supplied) unless
// requestedTenant is empty (defaults to the caller's own tenant) or the
// caller holds cross-tenant scope (the system-tenant rule already used
// throughout this stack: qrdbsdk.IsSystemTenant(callerTenant)).
//
// This is the ONE place that rule is stated; pkg/api/grpc and pkg/api/rest
// both call it rather than carrying their own copies, so the read path
// (Query/Get/Delete) and the write path (Record) can never drift apart.
func ResolveRequestTenant(ctx context.Context, requestedTenant string) (string, error) {
	callerTenant := TenantFromContext(ctx)
	if requestedTenant == "" {
		return callerTenant, nil
	}
	if requestedTenant == callerTenant || qrdbsdk.IsSystemTenant(callerTenant) {
		return requestedTenant, nil
	}
	return "", &TenantScopeMismatchError{CallerTenant: callerTenant, RequestedTenant: requestedTenant}
}
