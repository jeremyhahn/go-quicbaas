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

// Package auth provides a unified auth factory that auto-detects storage backend
package auth

import (
	"fmt"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Note: Both InMemoryAuthManager and PersistentAuthManager implement compatible interfaces
// We use the concrete types directly rather than defining a new interface

// NewAuthManager creates the appropriate auth manager based on storage type
//
// Behavior:
//   - If storage is nil or in-memory: Uses in-memory auth (ephemeral)
//   - If storage is persistent: Uses database-backed auth (survives restarts)
//
// This provides a unified API that automatically adapts to your deployment mode.
//
// Returns interface{} which will be either *InMemoryAuthManager or *PersistentAuthManager.
// Both types have the same methods, so you can use type assertion if needed.
func NewAuthManager(cfg *AuthConfig, storage interfaces.Storage, log *logger.Logger) (interface{}, error) {
	if cfg == nil {
		return nil, api.NewNilConfigError("auth.NewAuthManager")
	}

	if log == nil {
		return nil, api.NewNilLoggerError("auth.NewAuthManager")
	}

	// Detect storage type
	isMemoryOnly := isMemoryStorage(storage)

	if isMemoryOnly {
		// In-memory mode: Use ephemeral auth (no persistence)
		log.Info("Using in-memory authentication (ephemeral)")
		return NewInMemoryAuthManager(cfg, log)
	} else {
		// Persistent mode: Use database-backed auth
		log.Info("Using persistent authentication (database-backed)")

		if storage == nil {
			return nil, api.NewNilStorageError("auth.NewAuthManager")
		}

		return NewPersistentAuthManager(cfg, storage, log)
	}
}

// isMemoryStorage detects if the storage backend is in-memory only.
func isMemoryStorage(storage interfaces.Storage) bool {
	if storage == nil {
		return true // No storage = in-memory mode
	}

	// Check type name
	typeName := reflect.TypeOf(storage).String()

	// Memory storage indicators
	memoryIndicators := []string{
		"*memory.MemoryStorage",
		"memory.MemoryStorage",
		"MemoryStorage",
	}

	for _, indicator := range memoryIndicators {
		if typeName == indicator {
			return true
		}
	}

	return false
}

// IsMemoryOnlyMode is a helper to check if auth is running in memory-only mode.
func IsMemoryOnlyMode(storage interfaces.Storage) bool {
	return isMemoryStorage(storage)
}

// MustBootstrap bootstraps the auth system based on the manager type
//
// Usage:
//
//	authManager, _ := auth.NewUnifiedAuthManager(cfg, storage, log)
//	result, _ := auth.MustBootstrap(authManager, &auth.BootstrapConfig{
//	    CreateAdminUser: true,
//	})
//
// Behavior:
//   - Persistent manager: Runs full bootstrap with database persistence
//   - In-memory manager: Creates admin user in memory (ephemeral)
func MustBootstrap(authManager interface{}, config *BootstrapConfig) (*BootstrapResult, error) {
	// Check if this is a persistent auth manager
	if pam, ok := authManager.(*PersistentAuthManager); ok {
		return Bootstrap(pam, config)
	}

	// In-memory mode - create admin user record directly. Users authenticate
	// via IAM (mTLS, PIV, WebAuthn, OIDC); this creates the record for role assignment.
	am, ok := authManager.(*InMemoryAuthManager)
	if !ok {
		return nil, api.NewUnknownAuthManagerTypeError(
			"auth.MustBootstrap",
			fmt.Sprintf("%T", authManager),
		)
	}

	if config != nil && config.CreateAdminUser {
		username := config.AdminUsername
		if username == "" {
			username = "admin"
		}

		user, err := am.CreateUser(username, []string{RoleNameAdmin})
		if err != nil {
			return nil, api.NewAdminUserCreateError("auth.MustBootstrap", err)
		}

		return &BootstrapResult{
			AdminUserCreated: true,
			AdminUsername:    user.Username,
			AdminUserID:      user.ID,
			Message:          "In-memory admin user created (ephemeral - lost on restart)",
		}, nil
	}

	return &BootstrapResult{
		Message: "In-memory mode - no bootstrap performed",
	}, nil
}
