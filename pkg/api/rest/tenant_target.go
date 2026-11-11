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

// routePermission is the registration-site permission of the route a request
// was bound for (BindRoute). urlTenant authorizes it once more, in the URL's
// tenant, before a system caller acts there (go-quicbaas#2212).
type routePermission struct{ resource, action string }

type routePermissionKey struct{}

// withRoutePermission returns ctx carrying the route's permission. A route
// with no permission (public or identity-gated) carries none.
func withRoutePermission(ctx context.Context, resource, action string) context.Context {
	if resource == "" || action == "" {
		return ctx
	}
	return context.WithValue(ctx, routePermissionKey{}, routePermission{resource: resource, action: action})
}

// routePermissionFromContext returns the route's permission carried by ctx,
// and false when the request was bound for a route with none.
func routePermissionFromContext(ctx context.Context) (routePermission, bool) {
	perm, ok := ctx.Value(routePermissionKey{}).(routePermission)
	return perm, ok
}

// urlTenant resolves the tenant a request names in its URL parameter param,
// for the REST IAM routes. The parameter is caller-supplied, so it is
// honoured only as tenantctx.AdminTarget allows: a tenant caller names its
// own tenant, the system admin acts in the tenant the URL names
// (go-quicbaas#2212), and a request with no tenant bound is refused
// (go-quicbaas#1150). A tenant that Config.TenantAdmission refuses (still
// running its unseal checks) is answered 503 (admitTenant). A system admin
// that names another tenant acts there only after authorizeTenantLift allowed
// the route's own permission in that tenant; a caller acting in its own
// tenant gets no second decision. On success it returns the context with the
// resolved tenant bound, and also binds it into r. The caller must use the
// returned context for every downstream call: a ctx it captured from
// r.Context() before this call still carries the unresolved caller. On
// refusal it answers 403 through handleServiceError, or as admitTenant or
// authorizeTenantLift answers, returns ok=false and leaves r's context
// unresolved.
func (s *Server) urlTenant(w http.ResponseWriter, r *http.Request, op, param string) (context.Context, string, bool) {
	ctx, tenantID, err := tenantctx.AdminTarget(r.Context(), op, chi.URLParam(r, param))
	if err != nil {
		s.handleServiceError(w, r, err)
		return nil, "", false
	}
	callerTenant, err := tenantctx.FromContext(r.Context(), op)
	if err != nil {
		s.handleServiceError(w, r, err)
		return nil, "", false
	}
	if !s.admitTenant(w, r, tenantID) {
		return nil, "", false
	}
	if tenantID != callerTenant && !s.authorizeTenantLift(w, r, tenantID) {
		return nil, "", false
	}
	*r = *r.WithContext(ctx)
	return ctx, tenantID, true
}

// authorizeTenantLift renders the route's own permission once more, in the
// tenant the URL names, before a system caller acts there
// (go-quicbaas#2212). go-qrdb authorized the route against the caller's own
// tenant at the registration site; the URL's tenant has not decided yet, and
// its deny policies must apply. The identity is the request's own,
// unchanged: only the target tenant bound for the decision differs. It fails
// closed: a request that carries no route permission is refused 403, no
// wired authorizer answers 503 and no authenticated identity answers 401. On
// refusal it writes the response and returns false.
func (s *Server) authorizeTenantLift(w http.ResponseWriter, r *http.Request, tenantID string) bool {
	perm, ok := routePermissionFromContext(r.Context())
	if !ok {
		RespondError(w, r, http.StatusForbidden, &ForbiddenError{
			Reason: "the route carries no permission to authorize in the requested tenant",
		})
		return false
	}
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
	if err := authz.Authorize(ctx, identity, perm.resource, perm.action); err != nil {
		var unavailable *BarrierScopeAuthorizerUnavailableError
		if errors.As(err, &unavailable) {
			RespondError(w, r, http.StatusServiceUnavailable, unavailable)
			return false
		}
		RespondError(w, r, http.StatusForbidden, &ForbiddenError{
			Reason: perm.resource + ":" + perm.action + " is required in the requested tenant",
		})
		return false
	}
	return true
}

// policyRepairTenant resolves the URL's tenant like urlTenant, and is the one
// path to a held tenant's data: for its ABAC policies only, for the system
// admin only (go-quicbaas#2212). A tenant whose stored policy cannot load is
// held at unseal until that policy is fixed, so when admission would refuse the
// tenant and the caller is bound to the system tenant, the system admin's
// lift into that tenant is authorized as urlTenant authorizes it
// (authorizeTenantLift), the returned ctx is marked tenantctx.WithPolicyRepair
// for that tenant, bound into r, and repair is true. Every other caller of a
// held tenant is answered 503 as urlTenant answers it, and an admitted tenant
// resolves exactly as urlTenant resolves it.
func (s *Server) policyRepairTenant(w http.ResponseWriter, r *http.Request, op, param string) (ctx context.Context, tenantID string, repair bool, ok bool) {
	if s != nil && s.config != nil && s.config.TenantAdmission != nil {
		ctx, tenantID, err := tenantctx.AdminTarget(r.Context(), op, chi.URLParam(r, param))
		if err != nil {
			s.handleServiceError(w, r, err)
			return nil, "", false, false
		}
		if s.config.TenantAdmission(tenantID) != nil {
			caller, callerErr := tenantctx.FromContext(r.Context(), op)
			if callerErr == nil && tenantctx.IsSystem(caller) && !tenantctx.IsSystem(tenantID) {
				ctx = tenantctx.WithPolicyRepair(ctx, tenantID)
				prev := r.Context()
				*r = *r.WithContext(ctx)
				if !s.authorizeTenantLift(w, r, tenantID) {
					*r = *r.WithContext(prev)
					return nil, "", false, false
				}
				return ctx, tenantID, true, true
			}
		}
	}
	ctx, tenantID, ok = s.urlTenant(w, r, op, param)
	return ctx, tenantID, false, ok
}

// admitTenant answers 503 and returns false when Config.TenantAdmission
// refuses tenantID: the tenant is still running its unseal checks, so no
// request may reach its data yet. A nil TenantAdmission admits every tenant.
func (s *Server) admitTenant(w http.ResponseWriter, r *http.Request, tenantID string) bool {
	if s == nil || s.config == nil || s.config.TenantAdmission == nil {
		return true
	}
	if err := s.config.TenantAdmission(tenantID); err != nil {
		RespondServiceUnavailable(w, r, err.Error())
		return false
	}
	return true
}
