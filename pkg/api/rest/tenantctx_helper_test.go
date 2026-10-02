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

package rest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"

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

// systemRequest is httptest.NewRequest for an authenticated caller, bound the
// way go-qrdb's AuthMiddleware (or injectPrincipal) binds one. The caller is
// the tenant the URL's tenant_id names — a request may name only its own
// tenant (go-quicbaas#1150) — and the system tenant when it names none.
// Handlers no longer read an unbound request as the system tenant, so tests
// that mean a caller say which one.
func systemRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
		return r.WithContext(tenantctx.WithTenant(r.Context(), tenantID))
	}
	return r.WithContext(tenantctx.WithSystem(r.Context()))
}

// tenantRequest is httptest.NewRequest for a caller authenticated to tenantID.
func tenantRequest(tenantID, method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	return r.WithContext(tenantctx.WithTenant(r.Context(), tenantID))
}
