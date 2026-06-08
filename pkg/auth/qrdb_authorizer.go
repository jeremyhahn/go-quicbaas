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

package auth

import "context"

// QRDBDecisionPoint is the narrow seam the in-process auth managers use to
// route an authorization decision through qrdb's policy decision point instead
// of the forked role manager. It is satisfied by the qrdbauthz adapter (which
// lives behind the iam build tag), so this package carries no dependency on iam:
// the field is nil in non-iam builds and in any deployment that has not opted
// into RequireAuthz, and the managers then preserve the legacy
// roleManager.HasPermission outcome exactly.
//
// resource and action are qrdb-canonical; the join "resource:action" must be a
// registered qrdb permission. Authorize returns nil to allow and a typed error
// to deny.
type QRDBDecisionPoint interface {
	Authorize(ctx context.Context, subject QRDBSubject, resource, action string) error
}

// QRDBSubject is the principal the decision point authorizes, built from the
// validated token claims. It mirrors qrdbauthz.Subject without importing it, so
// the seam stays free of the iam build tag.
type QRDBSubject struct {
	// ID is the qrdb identity subject (the token's user id).
	ID string
	// TenantID is the principal's home tenant; always non-empty for a
	// well-formed identity.
	TenantID string
	// Roles are the qrdb-canonical role names from the token.
	Roles []string
}
