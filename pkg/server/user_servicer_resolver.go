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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
)

// platformUserServicerResolver resolves the embedded qrdb UserServicer that
// owns the canonical user CORE. It is the wiring-layer implementation of
// qrdbadapter.UserServicerResolver: the qrdbadapter package owns the
// user-storage contract but must not depend on the server's Server type, so the
// server hands it this narrow resolver instead. Every user mutation the
// IdentityStore performs is therefore routed to the SAME qrdb user store login
// and the policy decision point read.
type platformUserServicerResolver struct {
	server *Server
}

// newPlatformUserServicerResolver builds the resolver from the server. The
// UserServicer is resolved lazily on each call so the resolver tolerates the
// embedded node being unavailable before Unseal completes.
func newPlatformUserServicerResolver(s *Server) *platformUserServicerResolver {
	return &platformUserServicerResolver{server: s}
}

// UserServicer returns the embedded qrdb UserServicer. It fails closed with a
// typed configuration error when the node (or its user service) is unavailable,
// mirroring platformRoleClientResolver.
func (r *platformUserServicerResolver) UserServicer(_ context.Context) (qrdbsdk.UserServicer, error) {
	if r == nil || r.server == nil {
		return nil, &QRDBClientNotInitializedError{}
	}
	svc := r.server.GetQRDBUserServicer()
	if svc == nil {
		return nil, &QRDBClientNotInitializedError{}
	}
	return svc, nil
}

// Compile-time interface check.
var _ qrdbadapter.UserServicerResolver = (*platformUserServicerResolver)(nil)
