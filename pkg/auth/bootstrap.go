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

// Package auth provides bootstrap utilities for initializing authentication
package auth

import (
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// BootstrapConfig configures the bootstrap process.
type BootstrapConfig struct {
	// CreateAdminUser determines if a default admin user should be created
	CreateAdminUser bool

	// AdminUsername is the username for the default admin user
	// Defaults to "admin" if not specified
	AdminUsername string

	// ForceRecreate will delete and recreate the admin user if it exists
	ForceRecreate bool
}

// BootstrapResult contains information about the bootstrap process.
type BootstrapResult struct {
	// AdminUserCreated indicates if a new admin user was created
	AdminUserCreated bool

	// AdminUsername is the username of the admin user
	AdminUsername string

	// AdminUserID is the ID of the admin user
	AdminUserID string

	// AlreadyBootstrapped indicates if the system was already bootstrapped
	AlreadyBootstrapped bool

	// RolesCreated is the number of roles created
	RolesCreated int

	// Message provides additional information about the bootstrap process
	Message string
}

// Bootstrap initializes the authentication system with default admin user and roles.
func Bootstrap(authManager *PersistentAuthManager, config *BootstrapConfig) (*BootstrapResult, error) {
	if config == nil {
		config = &BootstrapConfig{
			CreateAdminUser: true,
			AdminUsername:   "admin",
		}
	}

	// Set defaults
	if config.AdminUsername == "" {
		config.AdminUsername = "admin"
	}

	result := &BootstrapResult{
		AdminUsername: config.AdminUsername,
	}

	// Check if already bootstrapped
	kb := NewKeyBuilder()
	bootstrapKey := kb.BootstrapCompleteKey()
	bootstrapData, err := authManager.storage.Get(bootstrapKey)
	if err == nil && len(bootstrapData) > 0 {
		result.AlreadyBootstrapped = true

		if !config.ForceRecreate {
			result.Message = "System already bootstrapped. Use ForceRecreate=true to reinitialize."
			return result, nil
		}

		authManager.logger.Warn("Force recreating admin user")
	}

	// Create or recreate admin user
	// NOTE: Password-based authentication has been removed. Admin users authenticate
	// via IAM (WebAuthn, OIDC, OAuth). This creates the user record for role assignment.
	if config.CreateAdminUser {
		// Check if admin user exists
		existingUser := authManager.userStore.GetByUsername(config.AdminUsername)

		if existingUser != nil {
			if config.ForceRecreate {
				// Delete existing admin user
				if err := authManager.DeleteUser(existingUser.ID); err != nil {
					return nil, api.NewAdminUserDeleteError("auth.Bootstrap", err)
				}
				authManager.logger.Infof("Deleted existing admin user: %s", config.AdminUsername)
			} else {
				result.AdminUserID = existingUser.ID
				result.Message = api.NewAdminUserExistsError(config.AdminUsername).Error()
				return result, nil
			}
		}

		// Create admin user (password parameter ignored - use IAM for authentication)
		adminUser, err := authManager.CreateUser(config.AdminUsername, "", []string{RoleNameAdmin})
		if err != nil {
			return nil, api.NewAdminUserCreateError("auth.Bootstrap", err)
		}

		result.AdminUserCreated = true
		result.AdminUserID = adminUser.ID

		// Store admin user ID for reference
		adminKey := kb.AdminUserKey()
		if err := authManager.storage.Set(adminKey, []byte(adminUser.ID)); err != nil {
			authManager.logger.WithError(err).Warn("Failed to store admin user reference")
		}

		authManager.logger.Infof("Admin user created: %s (ID: %s)", config.AdminUsername, adminUser.ID)
	}

	// Mark bootstrap as complete
	bootstrapTimestamp := time.Now().Format(time.RFC3339)
	if err := authManager.storage.Set(bootstrapKey, []byte(bootstrapTimestamp)); err != nil {
		return nil, api.NewBootstrapMarkerError("auth.Bootstrap", err)
	}

	// Count roles
	roleManager := authManager.GetRoleManager()
	if prm, ok := roleManager.(*PersistentRoleManager); ok {
		result.RolesCreated = prm.Count()
	}

	result.Message = "Bootstrap completed successfully"
	authManager.logger.Info("Authentication system bootstrap completed")

	return result, nil
}

// IsBootstrapped checks if the authentication system has been bootstrapped.
func IsBootstrapped(storage interfaces.Storage) (bool, error) {
	kb := NewKeyBuilder()
	bootstrapKey := kb.BootstrapCompleteKey()

	data, err := storage.Get(bootstrapKey)
	if err != nil {
		// Key not found means not bootstrapped
		return false, nil
	}

	return len(data) > 0, nil
}

// GetAdminUserID retrieves the admin user ID from storage.
func GetAdminUserID(storage interfaces.Storage) (string, error) {
	kb := NewKeyBuilder()
	adminKey := kb.AdminUserKey()

	data, err := storage.Get(adminKey)
	if err != nil {
		return "", api.NewAdminUserNotFoundError("auth.GetAdminUserID")
	}

	return string(data), nil
}

// ResetAdminPassword is deprecated and no longer functional.
// Password-based authentication has been removed. Use IAM (WebAuthn, OIDC, OAuth) instead.
func ResetAdminPassword(authManager *PersistentAuthManager, newPassword string) error {
	return api.NewPasswordAuthDeprecatedError("auth.ResetAdminPassword")
}

// CreateDefaultRoles ensures the default roles exist (called by PersistentRoleManager.initializeBuiltInRoles)
// This is exposed for manual invocation if needed.
func CreateDefaultRoles(roleManager *PersistentRoleManager) error {
	return roleManager.initializeBuiltInRoles()
}
