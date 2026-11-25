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

package websocket

import (
	"context"
	"errors"
)

// ErrOperationNotAuthorized is returned by Session.Authorize when the session
// carries no grant or no authorizer: per-operation checks fail closed.
var ErrOperationNotAuthorized = errors.New("websocket: operation not authorized")

// Grant is what the route that mounts the WebSocket server hands over with an
// authenticated upgrade request: the verified tenant, and an authorizer for
// the operations a connection performs after the upgrade (publishing, for
// example, which the upgrade's own permission does not cover).
type Grant struct {
	// TenantID is the verified tenant of the upgrading principal.
	TenantID string

	// Authorize checks resource:action for the upgrading principal. A nil
	// Authorize denies every operation.
	Authorize func(ctx context.Context, resource, action string) error
}

type grantContextKey struct{}

// WithGrant returns a copy of ctx carrying g.
func WithGrant(ctx context.Context, g Grant) context.Context {
	return context.WithValue(ctx, grantContextKey{}, g)
}

// GrantFromContext returns the grant WithGrant stored in ctx.
func GrantFromContext(ctx context.Context) (Grant, bool) {
	g, ok := ctx.Value(grantContextKey{}).(Grant)
	return g, ok
}
