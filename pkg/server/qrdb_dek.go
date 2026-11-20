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
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
)

// GetQRDBDEKServicer returns go-qrdb's per-tenant DEK keyring service from the
// embedded engine node's service graph: the servicer composers wire into the
// SDK's embedded qrdb transport (WithEmbeddedQRDB) so Security().TenantDEK()
// is served by go-qrdb's keyring in-process, the same keyring the node's
// network transports serve. Every call acts on the caller's own tenant.
// Returns nil when the node is absent or its service graph is not yet built
// (e.g. while sealed).
func (s *Server) GetQRDBDEKServicer() qrdbservice.DEKServicer {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil {
		return nil
	}
	return svcs.DEK
}
