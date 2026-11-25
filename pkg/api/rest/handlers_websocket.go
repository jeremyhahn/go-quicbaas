//go:build rest

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

package rest

import (
	"context"
	"net/http"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// HandleWebSocket handles GET /api/v1/ws, the WebSocket upgrade. The route
// carries the messaging:subscribe permission, so the request is authenticated
// and authorized before it reaches here. The handler is late-bound through
// SetWebSocketHandler; while it is unset (for example in FIPS-only mode, which
// refuses the WebSocket server) the route answers 503.
//
// The upgrade is handed over with a ws.Grant: the caller's verified tenant,
// which selects the tenant's own messaging hub, and an authorizer the
// connection uses for every later operation that needs more than
// messaging:subscribe (publishing needs messaging:publish).
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.webSocketHandler
	s.mu.RUnlock()

	if h == nil {
		http.Error(w, "the websocket server is not running on this node", http.StatusServiceUnavailable)
		return
	}

	tenantID, err := tenantctx.FromContext(r.Context(), "rest.HandleWebSocket")
	if err != nil {
		RespondError(w, r, http.StatusForbidden, &ForbiddenError{
			Reason: "a websocket connection needs a verified tenant",
		})
		return
	}

	grant := ws.Grant{
		TenantID:  tenantID,
		Authorize: s.webSocketAuthorizer(r),
	}
	h.ServeHTTP(w, r.WithContext(ws.WithGrant(r.Context(), grant)))
}

// webSocketAuthorizer returns the per-operation check a WebSocket connection
// runs after the upgrade. It captures the request's identity and outlives the
// upgrade request (context.WithoutCancel). It fails closed: with no identity
// or no authorizer wired every operation is denied.
func (s *Server) webSocketAuthorizer(r *http.Request) func(context.Context, string, string) error {
	base := context.WithoutCancel(r.Context())
	identity := qrdbsdk.IdentityFromContext(base)
	authz := s.barrierScopeAuthorizer()
	return func(_ context.Context, resource, action string) error {
		if identity == nil || authz == nil {
			return &ForbiddenError{Reason: resource + ":" + action + " is required"}
		}
		return authz.Authorize(base, identity, resource, action)
	}
}
