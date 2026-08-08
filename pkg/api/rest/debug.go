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

// registerDebug conditionally registers debug routes when enabled via env.
//
// The group is registered on the ROOT router, so — like every other root mount
// — it installs the shared protected-API authorization chain itself
// (useAPIAuthzMiddleware). Shard-routing introspection is cluster status, so it
// authorizes as admin:status (see the "/api/v1/debug" entries in
// authz_pep_mapping.go and authz_default_deny.go). The env gate keeps the
// surface absent entirely in normal deployments; the authz chain keeps it
// gated when an operator does enable it.
func registerDebug(s *Server, r chi.Router) {
	if os.Getenv("QUICBAAS_ENABLE_DEBUG_ROUTES") != "1" {
		return
	}
	s.logger.Info("Registering debug routes (enabled by env)")
	r.Route("/api/v1/debug", func(r chi.Router) {
		s.useAPIAuthzMiddleware(r)

		r.Get("/route/{type}/{id}", func(w http.ResponseWriter, req *http.Request) {
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
		})
	})
}
