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
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// debugRoutesEnvVar gates the shard-routing introspection mount on both the
// legacy listener (registerDebug) and the unified-API-server module surface
// (routes_iam.go debugModuleRouteSpecs), so the two registration paths agree
// on exactly when the surface is reachable.
const debugRoutesEnvVar = "QUICBAAS_ENABLE_DEBUG_ROUTES"

// debugRoutesEnabled reports whether the debug mount is enabled by env.
func debugRoutesEnabled() bool {
	return os.Getenv(debugRoutesEnvVar) == "1"
}

// registerDebug conditionally registers debug routes when enabled via env.
//
// The group is registered on the ROOT router of the legacy in-process
// listener, which the unified-API-server architecture never starts a
// listener for (pkg/server/lifecycle_rest.go startRESTServer); it carries no
// authorization gate here. Live enforcement is the module-only
// GroupDebugModule RouteSpec (routes_iam.go debugModuleRouteSpecs, frozen to
// admin:status) served through go-qrdb's unified-server authorizer via BindRoute. The env gate
// keeps the surface absent entirely in normal deployments.
func registerDebug(s *Server, r chi.Router) {
	if !debugRoutesEnabled() {
		return
	}
	s.logger.Info("Registering debug routes (enabled by env)")
	r.Route("/api/v1/debug", func(r chi.Router) {
		r.Get("/route/{type}/{id}", s.handleDebugRoute)
	})
}

// handleDebugRoute resolves the shard owning (entity type, id) and writes it
// as plain text. Shared by the legacy listener (registerDebug) and the
// unified-API-server module surface (routes_iam.go), so both surfaces answer
// identically.
func (s *Server) handleDebugRoute(w http.ResponseWriter, req *http.Request) {
	et := chi.URLParam(req, "type")
	idStr := chi.URLParam(req, "id")
	var id uint64
	if idStr != "" {
		if v, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			id = v
		}
	}
	shard := s.shardRouter.Route(et, id)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(strconv.FormatUint(shard, 10)))
}
