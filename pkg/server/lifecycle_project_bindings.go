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

package server

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// wireProjectBindings binds the SDLC binding store onto the project service,
// so deleting a bound project is refused (go-quicbaas#891).
//
// It runs whether or not the SDLC module is enabled: bindings persist in the
// tenant keyspace, and a node with the module off still serves deletes of
// projects another node, or an earlier boot, bound. A node with no qrdb node,
// or one with no tenant store resolver, has no binding store, so no binding
// can exist and the reader is left unwired.
func (s *Server) wireProjectBindings(svc service.QuicBaaS) error {
	node := s.getQRDBNode()
	if node == nil || svc == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		return nil
	}
	store, err := sdlc.NewStore(s.admittedStoreResolver(svcs.StoreResolver))
	if err != nil {
		return &SDLCInitError{Component: "project binding reader", Cause: err}
	}
	svc.SetProjectBindingReader(store)
	return nil
}
