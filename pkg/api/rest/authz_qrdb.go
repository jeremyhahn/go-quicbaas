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

package rest

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// QRDBAuthorizer is the narrow authorization contract the REST PEP depends on.
// It is satisfied by pkg/auth/qrdbauthz.Adapter, which delegates the decision to
// qrdb's policy decision point. The middleware stays decoupled from the
// iam-tagged adapter: it depends only on this seam plus an adapter-supplied
// subject builder.
//
// resource and action are qrdb-canonical (the join "resource:action" is a
// registered qrdb permission). Authorize returns nil to allow and a typed error
// to deny.
type QRDBAuthorizer interface {
	AuthorizeSubject(ctx context.Context, subject interfaces.Subject, resource, action string) error
}

// authorizeViaQRDB runs the qrdb-backed decision: it maps the request to a
// qrdb-canonical (resource, action) and delegates to the QRDB authorizer. It is
// the qrdb PEP path of the Authorize gate, active only when a QRDBAuthorizer was
// installed (RequireAuthz=true). A request whose path has no canonical mapping
// is denied under this posture — there is no registered qrdb permission to admit
// it, and the gate is default-deny by construction.
func (m *AuthzMiddleware) authorizeViaQRDB(ctx context.Context, subject interfaces.Subject, path, method string) error {
	resource, action, ok := mapRequestToPermission(path, method)
	if !ok {
		return &UnmappedResourceError{Path: path}
	}
	return m.qrdbAuthorizer.AuthorizeSubject(ctx, subject, resource, action)
}

// UnmappedResourceError is returned by the qrdb PEP when a protected path has no
// qrdb-canonical permission mapping. Under RequireAuthz the gate is default-deny,
// so an unmapped protected surface is denied rather than admitted.
type UnmappedResourceError struct {
	Path string
}

func (e *UnmappedResourceError) Error() string {
	return "no qrdb permission mapping for path: " + e.Path
}
