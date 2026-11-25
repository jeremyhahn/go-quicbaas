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

package grpc

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// requireCallerTenant refuses a request whose tenant_id is not the caller's
// own tenant. The tenant_id field is caller-supplied, so it is honoured only
// as tenantctx.Target allows: a tenantless caller is refused, and the system
// tenant gets no lift into another tenant (#1150).
func requireCallerTenant(ctx context.Context, op, tenantID string) error {
	_, err := targetTenant(ctx, op, tenantID)
	return err
}

// requireAdminTarget resolves the tenant a request names with
// tenantctx.AdminTarget, the rule the REST IAM administration routes apply
// (pkg/api/rest/tenant_target.go urlTenant): a system-tenant caller that names
// a tenant acts in it, a tenant caller may name only its own tenant (or none,
// meaning its own), and a call with no tenant bound is refused. It returns the
// context with the resolved tenant bound; the handler must pass that context
// on. Only the Federation service uses it (go-quicbaas#2741); every other
// service keeps requireCallerTenant and tenantctx.Target.
func requireAdminTarget(ctx context.Context, op, tenantID string) (context.Context, error) {
	bound, _, err := tenantctx.AdminTarget(ctx, op, tenantID)
	if err != nil {
		return ctx, toProtoError(err)
	}
	return bound, nil
}

// targetTenant is requireCallerTenant for a handler that also needs the tenant
// it resolved: an empty request names the caller's own tenant, which is
// returned in its place.
func targetTenant(ctx context.Context, op, tenantID string) (string, error) {
	resolved, err := tenantctx.Target(ctx, op, tenantID)
	if err != nil {
		return "", toProtoError(err)
	}
	return resolved, nil
}
