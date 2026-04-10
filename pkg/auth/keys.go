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

// Package auth provides internal key schema for persistent authentication storage
package auth

import (
	"fmt"
)

// Internal key prefixes for auth storage
// These keys are reserved and cannot be used by user applications.
const (
	// KeyPrefixInternal is the root prefix for all internal QuicBaaS keys.
	KeyPrefixInternal = "__quicbaas:"

	// User storage keys.
	KeyPrefixUsers        = KeyPrefixInternal + "users:"        // __quicbaas:users:{userID}
	KeyPrefixUsersByName  = KeyPrefixInternal + "users:byname:" // __quicbaas:users:byname:{username} -> userID
	KeyPrefixUserMetadata = KeyPrefixInternal + "users:meta:"   // __quicbaas:users:meta:{userID}

	// Role storage keys.
	KeyPrefixRoles        = KeyPrefixInternal + "roles:"      // __quicbaas:roles:{roleName}
	KeyPrefixRoleMetadata = KeyPrefixInternal + "roles:meta:" // __quicbaas:roles:meta:{roleName}

	// Token storage keys.
	KeyPrefixTokens        = KeyPrefixInternal + "tokens:"         // __quicbaas:tokens:{tokenHash}
	KeyPrefixRefreshTokens = KeyPrefixInternal + "tokens:refresh:" // __quicbaas:tokens:refresh:{userID}:{tokenHash}
	KeyPrefixRevokedTokens = KeyPrefixInternal + "tokens:revoked:" // __quicbaas:tokens:revoked:{tokenHash}

	// Bootstrap/system keys.
	KeyBootstrapComplete = KeyPrefixInternal + "bootstrap:complete" // __quicbaas:bootstrap:complete -> timestamp
	KeyAdminUser         = KeyPrefixInternal + "bootstrap:admin"    // __quicbaas:bootstrap:admin -> userID
)

// KeyBuilder provides utilities for constructing internal auth keys.
type KeyBuilder struct{}

// NewKeyBuilder creates a new key builder.
func NewKeyBuilder() *KeyBuilder {
	return &KeyBuilder{}
}

// UserKey returns the key for storing user data
// Format: __quicbaas:users:{userID}.
func (kb *KeyBuilder) UserKey(userID string) []byte {
	return []byte(KeyPrefixUsers + userID)
}

// UserByNameKey returns the key for username -> userID lookup
// Format: __quicbaas:users:byname:{username}.
func (kb *KeyBuilder) UserByNameKey(username string) []byte {
	return []byte(KeyPrefixUsersByName + username)
}

// UserMetadataKey returns the key for user metadata
// Format: __quicbaas:users:meta:{userID}.
func (kb *KeyBuilder) UserMetadataKey(userID string) []byte {
	return []byte(KeyPrefixUserMetadata + userID)
}

// RoleKey returns the key for storing role data
// Format: __quicbaas:roles:{roleName}.
func (kb *KeyBuilder) RoleKey(roleName string) []byte {
	return []byte(KeyPrefixRoles + roleName)
}

// RoleMetadataKey returns the key for role metadata
// Format: __quicbaas:roles:meta:{roleName}.
func (kb *KeyBuilder) RoleMetadataKey(roleName string) []byte {
	return []byte(KeyPrefixRoleMetadata + roleName)
}

// RefreshTokenKey returns the key for refresh token storage
// Format: __quicbaas:tokens:refresh:{userID}:{tokenHash}.
func (kb *KeyBuilder) RefreshTokenKey(userID, tokenHash string) []byte {
	return []byte(fmt.Sprintf("%s%s:%s", KeyPrefixRefreshTokens, userID, tokenHash))
}

// RevokedTokenKey returns the key for revoked token tracking
// Format: __quicbaas:tokens:revoked:{tokenHash}.
func (kb *KeyBuilder) RevokedTokenKey(tokenHash string) []byte {
	return []byte(KeyPrefixRevokedTokens + tokenHash)
}

// BootstrapCompleteKey returns the key for bootstrap completion flag.
func (kb *KeyBuilder) BootstrapCompleteKey() []byte {
	return []byte(KeyBootstrapComplete)
}

// AdminUserKey returns the key for the bootstrap admin user.
func (kb *KeyBuilder) AdminUserKey() []byte {
	return []byte(KeyAdminUser)
}

// IsInternalKey checks if a key is an internal auth key
// This prevents user applications from accidentally overwriting auth data.
func IsInternalKey(key []byte) bool {
	keyStr := string(key)
	return len(keyStr) >= len(KeyPrefixInternal) &&
		keyStr[:len(KeyPrefixInternal)] == KeyPrefixInternal
}

// ValidateUserKey ensures a key is safe for user data
// Returns error if the key would conflict with internal auth keys.
func ValidateUserKey(key []byte) error {
	if IsInternalKey(key) {
		return &ReservedKeyPrefixError{Prefix: KeyPrefixInternal}
	}
	return nil
}
