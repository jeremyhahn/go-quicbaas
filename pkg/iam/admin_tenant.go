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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// AssignmentInTenant reports whether a belongs to tenantID alone: its
// TenantIDs is exactly [tenantID], or it is empty and tenantID is the
// system tenant (empty TenantIDs is system-tenant scope).
//
// An assignment that names several tenants belongs to none of them,
// because no single tenant may read or change a grant that spans another
// tenant. This is the ownership test the delegated-admin handlers use; it
// is not assignmentMatchesTenant, which decides whether a grant applies to
// a permission check.
func AssignmentInTenant(a *AdminAssignment, tenantID string) bool {
	if a == nil || tenantID == "" {
		return false
	}
	switch len(a.TenantIDs) {
	case 0:
		return qrdbsdk.IsSystemTenant(tenantID)
	case 1:
		return a.TenantIDs[0] == tenantID
	default:
		return false
	}
}

// GrantableTenantIDs resolves the TenantIDs a caller may write into an
// assignment it creates or updates. A tenant caller may grant only its own
// tenant (an empty request becomes [own]); a system caller only empty
// (system scope) or [system]. A request naming any other tenant is refused
// with the tenantctx cross-tenant error, and an unbound caller with the
// tenantctx missing-tenant error.
func GrantableTenantIDs(ctx context.Context, op string, requested []string) ([]string, error) {
	caller, err := tenantctx.FromContext(ctx, op)
	if err != nil {
		return nil, err
	}
	switch len(requested) {
	case 0:
		if tenantctx.IsSystem(caller) {
			return nil, nil
		}
		return []string{caller}, nil
	case 1:
		resolved, err := tenantctx.Target(ctx, op, requested[0])
		if err != nil {
			return nil, err
		}
		return []string{resolved}, nil
	default:
		for _, tid := range requested {
			if tid != caller {
				if _, err := tenantctx.Target(ctx, op, tid); err != nil {
					return nil, err
				}
			}
		}
		return []string{caller}, nil
	}
}
