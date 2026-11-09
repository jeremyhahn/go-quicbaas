//go:build rest

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
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// urlTenant resolves the tenant a request names in its URL parameter param,
// for the REST IAM routes. The parameter is caller-supplied, so it is
// honoured only as tenantctx.AdminTarget allows: a tenant caller names its
// own tenant, the system admin acts in the tenant the URL names
// (go-quicbaas#2212), and a request with no tenant bound is refused
// (go-quicbaas#1150). On success it returns the context with the resolved
// tenant bound, and also binds it into r. The caller must use the returned
// context for every downstream call: a ctx it captured from r.Context()
// before this call still carries the unresolved caller. On refusal it
// answers 403 through handleServiceError and returns ok=false.
func (s *Server) urlTenant(w http.ResponseWriter, r *http.Request, op, param string) (context.Context, string, bool) {
	ctx, tenantID, err := tenantctx.AdminTarget(r.Context(), op, chi.URLParam(r, param))
	if err != nil {
		s.handleServiceError(w, r, err)
		return nil, "", false
	}
	if !s.admitTenant(w, r, tenantID) {
		return nil, "", false
	}
	*r = *r.WithContext(ctx)
	return ctx, tenantID, true
}

// admitTenant answers 503 and returns false when Config.TenantAdmission
// refuses tenantID: the tenant is still running its unseal checks, so no
// request may reach its data yet. A nil TenantAdmission admits every tenant.
func (s *Server) admitTenant(w http.ResponseWriter, r *http.Request, tenantID string) bool {
	if s.config == nil || s.config.TenantAdmission == nil {
		return true
	}
	if err := s.config.TenantAdmission(tenantID); err != nil {
		RespondServiceUnavailable(w, r, err.Error())
		return false
	}
	return true
}
