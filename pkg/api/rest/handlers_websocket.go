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

import "net/http"

// HandleWebSocket handles GET /api/v1/ws, the WebSocket upgrade. The route
// carries the messaging:subscribe permission, so the request is authenticated
// and authorized before it reaches here. The handler is late-bound through
// SetWebSocketHandler; while it is unset (for example in FIPS-only mode, which
// refuses the WebSocket server) the route answers 503.
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.webSocketHandler
	s.mu.RUnlock()

	if h == nil {
		http.Error(w, "the websocket server is not running on this node", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}
