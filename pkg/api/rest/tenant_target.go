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
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// urlTenant resolves the tenant a request names in its URL parameter param,
// for the REST IAM routes. The parameter is caller-supplied, so it is
// honoured only as tenantctx.ContextForTarget allows: every caller, the system
// tenant included, names only its own tenant, and a request with no tenant
// bound is refused. On success it returns the context with the resolved
// tenant bound, and also binds it into r. The caller must use the returned
// context for every downstream call. On refusal it answers 403 through
// handleServiceError, returns ok=false and leaves r's context unchanged.
func (s *Server) urlTenant(w http.ResponseWriter, r *http.Request, op, param string) (context.Context, string, bool) {
	ctx, tenantID, err := tenantctx.ContextForTarget(r.Context(), op, chi.URLParam(r, param))
	if err != nil {
		s.handleServiceError(w, r, err)
		return nil, "", false
	}
	*r = *r.WithContext(ctx)
	return ctx, tenantID, true
}

// authorizePermissionInTenant renders resource:action for the request's
// authenticated identity in tenantID through the node's RBAC authorizer. It
// fails closed: no authorizer wired answers 503, no identity 401, and a
// denial 403. It reports whether the request may proceed; on false the
// response has been written.
func (s *Server) authorizePermissionInTenant(w http.ResponseWriter, r *http.Request, tenantID, resource, action string) bool {
	var authz qrdbsdk.Authorizer
	if s != nil {
		authz = s.barrierScopeAuthorizer()
	}
	if authz == nil {
		RespondError(w, r, http.StatusServiceUnavailable,
			&BarrierScopeAuthorizerUnavailableError{Reason: "not configured"})
		return false
	}
	identity := qrdbsdk.IdentityFromContext(r.Context())
	if identity == nil {
		RespondError(w, r, http.StatusUnauthorized, &UnauthorizedError{Message: "authentication required"})
		return false
	}

	ctx := qrdbsdk.ContextWithTenant(r.Context(), tenantID)
	if err := authz.Authorize(ctx, identity, resource, action); err != nil {
		var unavailable *BarrierScopeAuthorizerUnavailableError
		if errors.As(err, &unavailable) {
			RespondError(w, r, http.StatusServiceUnavailable, unavailable)
			return false
		}
		RespondError(w, r, http.StatusForbidden, &ForbiddenError{
			Reason: resource + ":" + action + " is required in the requested tenant",
		})
		return false
	}
	return true
}
