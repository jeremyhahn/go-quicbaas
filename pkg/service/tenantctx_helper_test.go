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

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// boundTenant returns the tenant bound to ctx under go-qrdb's central key, or
// "" when none is, so a test can assert what a code path bound.
func boundTenant(ctx context.Context) string {
	tenantID, ok := tenantctx.Lookup(ctx)
	if !ok {
		return ""
	}
	return tenantID
}

// systemTenantCtx is a context bound to the system tenant: what a single-tenant or
// platform-scoped caller of the services must now state explicitly, since an
// unbound tenant is refused rather than read as system (go-quicbaas#1150).
func systemTenantCtx() context.Context {
	return tenantctx.WithSystem(context.Background())
}
