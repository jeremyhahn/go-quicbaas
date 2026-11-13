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

package server

import (
	"context"

	authmw "github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// nodeAuthorizerCapture returns the qrdbnode.WithAuthorizerWrapper option
// that records the embedded node's composed RBAC[+ABAC] authorizer — the ONE
// instance qrdb's transports and its REST middleware decide every
// registration-site permission with — UNCHANGED, and hands every native
// transport (REST, gRPC, QUIC and the embedded transport) that authorizer
// wrapped in an admittingAuthorizer, so a request whose target tenant the
// server's unseal hooks hold is refused before the node's own authorizer runs.
//
// The recorded authorizer is the qrdb seam that takes a full qrdbsdk.Identity,
// ScopePermissions included. node.Client().RBAC() reaches the same instance
// but through transport.Identity, which has no ScopePermissions, so a
// platform app token whose authority is only its scp claim would be refused
// there (go-quicbaas#990).
func (s *Server) nodeAuthorizerCapture() qrdbnode.NodeOption {
	return qrdbnode.WithAuthorizerWrapper(s.wrapNodeAuthorizer)
}

// wrapNodeAuthorizer is nodeAuthorizerCapture's wrapper: it records inner
// unchanged and returns it wrapped in an admittingAuthorizer, or nil when
// inner is nil.
func (s *Server) wrapNodeAuthorizer(inner qrdbsdk.Authorizer) qrdbsdk.Authorizer {
	if inner == nil {
		return nil
	}
	s.nodeAuthorizer.Store(&inner)
	return &admittingAuthorizer{inner: inner, hooks: &s.tenantUnsealHooks}
}

// admittingAuthorizer wraps the node's authorizer so that a native request
// whose target tenant the server's unseal hooks hold (its unseal reload is
// still running, or failed) is refused: the node's own User, Role and DEK
// services read their tenant stores through the node's resolver, not through
// admittingStoreResolver, so the hold is enforced here for every native
// transport.
type admittingAuthorizer struct {
	inner qrdbsdk.Authorizer
	hooks *service.TenantUnsealHooks
}

// Authorize refuses a held target tenant without calling the inner
// authorizer, and returns the inner authorizer's decision otherwise. The
// refusal is the hooks' admission error wrapped in the middleware's
// StoreUnavailableError, so every native transport answers it as unavailable
// (503, UNAVAILABLE) and not as a failed credential. The target is the tenant
// bound to ctx, else the identity's tenant. The tenant_barrier resource
// (seal, unseal, status and votes) clears the hold and always reaches the
// inner authorizer, and so does admin:health, the liveness check an SDK
// connection starts with (heldTenantExempt). An unseal gate's own requests
// for the tenant it is admitting (service.InUnsealGate), the token-liveness
// reads of a held tenant (tenantctx.InAuthRecordRead) and the ABAC sync's
// reads of its policies (tenantctx.InPolicySyncRead) reach it too, as they do
// in admittingStoreResolver.Resolve.
func (a *admittingAuthorizer) Authorize(ctx context.Context, identity *qrdbsdk.Identity, resource, action string) error {
	target, ok := qrdbsdk.TenantFromContext(ctx)
	if !ok && identity != nil {
		target = identity.TenantID
	}
	if target != "" && !heldTenantExempt(resource, action) &&
		!service.InUnsealGate(ctx, target) && !tenantctx.InAuthRecordRead(ctx, target) && !tenantctx.InPolicySyncRead(ctx, target) {
		if err := a.hooks.Admit(target); err != nil {
			return &authmw.StoreUnavailableError{Err: err}
		}
	}
	return a.inner.Authorize(ctx, identity, resource, action)
}

// heldTenantExempt reports whether a native request reaches the node's
// authorizer although its target tenant is held. The tenant_barrier resource
// (seal, unseal, status and votes) clears the hold. admin:health is the
// node-liveness check every go-qrdb SDK transport makes when it connects
// (it takes PermissionDenied as reachable, not Unavailable), so a held
// tenant's admin must pass admission with it to open the connection its
// unseal retry runs on. It reads no tenant data, and the node's authorizer
// still authenticates and authorizes it.
func heldTenantExempt(resource, action string) bool {
	return resource == "tenant_barrier" || (resource == "admin" && action == "health")
}

// capturedNodeAuthorizer returns the node authorizer recorded by
// nodeAuthorizerCapture, or nil before the node has been built (or when it
// was built without a role service, in which case qrdb composes none).
func (s *Server) capturedNodeAuthorizer() qrdbsdk.Authorizer {
	p := s.nodeAuthorizer.Load()
	if p == nil {
		return nil
	}
	return *p
}
