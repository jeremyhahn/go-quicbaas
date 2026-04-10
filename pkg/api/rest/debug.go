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
func registerDebug(s *Server, r chi.Router) {
	if os.Getenv("QUICBAAS_ENABLE_DEBUG_ROUTES") != "1" {
		return
	}
	s.logger.Info("Registering debug routes (enabled by env)")
	r.Route("/api/v1/debug", func(r chi.Router) {
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
