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

package server

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
)

// restBarrierScopeAuthorizer is the qrdbsdk.Authorizer the REST server
// consults for POST /v1/barrier/enable-encryption, whose scope is chosen by a
// body tenant_id (go-quicbaas#990). It delegates to the embedded qrdb node's
// composed RBAC[+ABAC] authorizer, recorded at node construction by
// nodeAuthorizerCapture — the same instance qrdb's REST middleware renders
// every registration-site permission with — so the decision honours
// store-resolved role grants, a platform token's scp-derived
// ScopePermissions, the system-tenant cross-scope rule and the SO deny-list.
// The identity is passed through verbatim: an app token whose authority is
// only its scp claim is decided exactly as on the path-scoped routes.
//
// The authorizer is resolved per call because the node is built later in the
// lifecycle than the REST server. Until it exists the decision fails closed
// with restapi.TenantAuthorizerUnavailableError (503).
type restBarrierScopeAuthorizer struct {
	server *Server
}

// Compile-time interface check.
var _ qrdbsdk.Authorizer = (*restBarrierScopeAuthorizer)(nil)

// Authorize implements qrdbsdk.Authorizer. The target tenant is the one
// bound to ctx (qrdbsdk.ContextWithTenant); the REST handler binds it.
func (a *restBarrierScopeAuthorizer) Authorize(ctx context.Context, identity *qrdbsdk.Identity, resource, action string) error {
	authz := a.server.capturedNodeAuthorizer()
	if authz == nil {
		return &restapi.TenantAuthorizerUnavailableError{Reason: "qrdb node authorizer is not yet available"}
	}
	return authz.Authorize(ctx, identity, resource, action)
}
