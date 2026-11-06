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
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// urlTenant resolves the tenant a request names in its URL parameter param,
// for the REST IAM routes. The parameter is caller-supplied, so it is
// honoured only as tenantctx.AdminTarget allows: a tenant caller names its
// own tenant, the system admin acts in the tenant the URL names
// (go-quicbaas#2212), and a request with no tenant bound is refused
// (go-quicbaas#1150). On success it binds the resolved tenant into r's
// context, so the handler's later r.Context() carries it. On refusal it
// answers 403 through handleServiceError and returns ok=false.
func (s *Server) urlTenant(w http.ResponseWriter, r *http.Request, op, param string) (string, bool) {
	ctx, tenantID, err := tenantctx.AdminTarget(r.Context(), op, chi.URLParam(r, param))
	if err != nil {
		s.handleServiceError(w, r, err)
		return "", false
	}
	*r = *r.WithContext(ctx)
	return tenantID, true
}
