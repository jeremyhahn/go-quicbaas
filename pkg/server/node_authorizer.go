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
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
)

// nodeAuthorizerCapture returns the qrdbnode.WithAuthorizerWrapper option
// that records the embedded node's composed RBAC[+ABAC] authorizer — the ONE
// instance qrdb's transports and its REST middleware decide every
// registration-site permission with — and hands it back UNCHANGED, so the
// node's decisions are exactly what they were without the option.
//
// The recorded authorizer is the qrdb seam that takes a full qrdbsdk.Identity,
// ScopePermissions included. node.Client().RBAC() reaches the same instance
// but through transport.Identity, which has no ScopePermissions, so a
// platform app token whose authority is only its scp claim would be refused
// there (go-quicbaas#990).
func (s *Server) nodeAuthorizerCapture() qrdbnode.NodeOption {
	return qrdbnode.WithAuthorizerWrapper(func(inner qrdbsdk.Authorizer) qrdbsdk.Authorizer {
		if inner != nil {
			s.nodeAuthorizer.Store(&inner)
		}
		return inner
	})
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
