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

// Package auth provides authentication provider interfaces and implementations
package auth

import (
	"context"
)

// AuthProvider defines the interface for external authentication providers.
type AuthProvider interface {
	// Authenticate validates credentials against the external provider
	// Returns user information including username and roles on success
	Authenticate(ctx context.Context, username, password string) (*ExternalUserInfo, error)

	// Close cleans up provider resources
	Close() error
}

// ExternalUserInfo represents user information from external auth providers.
type ExternalUserInfo struct {
	Username string
	Email    string
	FullName string
	Roles    []string
	Groups   []string
}

// ProviderConfig contains common configuration for auth providers.
type ProviderConfig struct {
	ServerURL   string
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string
	Timeout     int // Timeout in seconds
}
