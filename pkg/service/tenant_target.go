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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
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
// caller's tenant is the IAM tenant the authenticated request carries:
//
//   - a caller of the target tenant acts on its own records;
//   - a caller of the system tenant administers every tenant;
//   - a context with no IAM tenant is an in-process caller (the embedded
//     transport), which has no identity to confine;
//   - anyone else is refused with TenantTargetDeniedError.
func targetTenantContext(ctx context.Context, target string) (context.Context, error) {
	caller := iam.TenantFromContext(ctx)
	if caller != "" && caller != target && !qrdbsdk.IsSystemTenant(caller) {
		return ctx, &TenantTargetDeniedError{CallerTenant: caller, TargetTenant: target}
	}
	return accountTenantContext(ctx, target), nil
}
