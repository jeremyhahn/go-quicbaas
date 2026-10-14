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

package service

import (
	"context"
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TenantTargetDeniedError is a caller authenticated to one tenant naming
// another as the tenant to act on. It unwraps to
// orchestration.ErrAccountTenantScope, which every transport maps to 403 /
// PermissionDenied.
type TenantTargetDeniedError struct {
	CallerTenant string
	TargetTenant string
}

// Error implements the error interface.
func (e *TenantTargetDeniedError) Error() string {
	return "service: a caller of tenant " + e.CallerTenant + " may not act on tenant " + e.TargetTenant
}

// Unwrap returns orchestration.ErrAccountTenantScope.
func (e *TenantTargetDeniedError) Unwrap() error { return orchestration.ErrAccountTenantScope }

// targetTenantContext authorizes acting on target, then scopes ctx to it
// under both tenant keys (accountTenantContext).
//
// THE AUTHORIZATION COMES FIRST, AGAINST THE ORIGINAL IDENTITY. Once both keys
// name the target, every store's scope check passes by construction, so a
// check made after the switch checks nothing (go-quicbaas#1058 review). The
// decision is tenantctx.Target's, the one rule every transport applies to a
// caller-supplied tenant:
//
//   - a caller of the target tenant acts on its own records;
//   - a context with no tenant bound is refused: an unknown caller is not the
//     system (go-quicbaas#1150);
//   - anyone else, the system tenant included, is refused with
//     TenantTargetDeniedError. A request field is not an authorization, so the
//     system tenant gets no cross-tenant lift through it (go-quicbaas#1150,
//     the 2026-10-02 no-lift ruling).
func targetTenantContext(ctx context.Context, target string) (context.Context, error) {
	tenantID, err := tenantctx.Target(ctx, "service.targetTenant", target)
	if err != nil {
		var cross *tenantctx.CrossTenantError
		if errors.As(err, &cross) {
			return ctx, &TenantTargetDeniedError{CallerTenant: cross.CallerTenant, TargetTenant: cross.TargetTenant}
		}
		return ctx, err
	}
	return accountTenantContext(ctx, tenantID), nil
}
