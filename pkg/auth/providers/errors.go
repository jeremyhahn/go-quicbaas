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

package providers

import "errors"

// Sentinel errors for the providers package.
var (
	// ErrNilRoleManager is returned when attempting to create an RBAC provider with a nil RoleManager.
	ErrNilRoleManager = errors.New("role manager cannot be nil")

	// ErrNilRBACProvider is returned when attempting to create a hybrid provider with a nil RBAC provider.
	ErrNilRBACProvider = errors.New("RBAC provider cannot be nil")

	// ErrNilRequest is returned when an authorization request is nil.
	ErrNilRequest = errors.New("authorization request cannot be nil")

	// ErrProviderClosed is returned when operations are attempted on a closed provider.
	ErrProviderClosed = errors.New("provider is closed")
)
