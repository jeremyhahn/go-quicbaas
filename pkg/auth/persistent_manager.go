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

// Package auth provides persistent authentication manager
package auth

import (
	"context"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// PersistentAuthManager manages authentication with database-backed persistence.
type PersistentAuthManager struct {
	config     *AuthConfig
	logger     *logger.Logger
	storage    interfaces.Storage
	tokenStore TokenStoreInterface
	userStore  UserStoreInterface
}

// UserStoreInterface abstracts user storage for both in-memory and persistent implementations.
type UserStoreInterface interface {
	AddUser(user *User) error
	GetByID(userID string) *User
	GetByUsername(username string) *User
	UpdateUser(user *User) error
	DeleteUser(userID string) error
	ListUsers() []*User
}

// TokenStoreInterface abstracts token storage for both in-memory and persistent implementations.
type TokenStoreInterface interface {
	AddRefreshToken(userID, token string)
	IsValidRefreshToken(userID, token string) bool
	Revoke(token string)
	IsRevoked(token string) bool
	RevokeUserTokens(userID string)
}

// NewPersistentAuthManager creates a new authentication manager with database persistence.
func NewPersistentAuthManager(cfg *AuthConfig, storage interfaces.Storage, log *logger.Logger) (*PersistentAuthManager, error) {
	if cfg == nil {
		return nil, api.NewNilInputError("auth.NewPersistentAuthManager", "config")
	}

	if storage == nil {
		return nil, api.NewNilInputError("auth.NewPersistentAuthManager", "storage")
	}

	if log == nil {
		return nil, api.NewNilInputError("auth.NewPersistentAuthManager", "logger")
	}

	if cfg.Enabled && cfg.JWTSecret == "" && cfg.JWTSigningKeyID == "" {
		return nil, api.NewRequiredFieldError("JWT secret or signing key ID")
	}

	// Default the signing algorithm to ES256 when a signing key ID is configured
	if cfg.JWTSigningKeyID != "" && cfg.JWTSigningAlgorithm == "" {
		cfg.JWTSigningAlgorithm = "ES256"
	}

	// Set defaults
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = 1 * 60 * 60 * 1000000000 // 1 hour in nanoseconds
	}
	if cfg.RefreshTokenTTL == 0 {
		cfg.RefreshTokenTTL = 7 * 24 * 60 * 60 * 1000000000 // 7 days
	}
	if cfg.RefreshWindow == 0 {
		cfg.RefreshWindow = 5 * 60 * 1000000000 // 5 minutes
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 10
	}

	// Create persistent stores
	persistentUserStore, err := NewPersistentUserStore(storage, log)
	if err != nil {
		return nil, api.NewOperationFailedError("create persistent user store", err)
	}

	persistentTokenStore, err := NewPersistentTokenStore(storage, log)
	if err != nil {
		return nil, api.NewOperationFailedError("create persistent token store", err)
	}

	pam := &PersistentAuthManager{
		config:     cfg,
		logger:     log,
		storage:    storage,
		tokenStore: persistentTokenStore,
		userStore:  persistentUserStore,
	}

	log.Info("Persistent authentication manager initialized")
	return pam, nil
}

// IssueTokenForUser issues JWT tokens for a user who has been authenticated
// via mTLS certificate, FIDO2/WebAuthn, PIV, OIDC, or other IAM provider.
// This should be called after successful authentication verification.
func (pam *PersistentAuthManager) IssueTokenForUser(ctx context.Context, user interfaces.UserInfo) (*Token, error) {
	if !pam.config.Enabled {
		return nil, api.NewAuthenticationDisabledError("auth.IssueTokenForUser")
	}

	if user == nil {
		return nil, api.NewNilInputError("auth.IssueTokenForUser", "user")
	}

	// Type assert to *User for token generation
	authUser, ok := user.(*User)
	if !ok {
		// If it's not our User type, try to get by username
		// PersistentUserStore.GetByUsername returns *User directly
		authUser = pam.userStore.GetByUsername(user.GetUsername())
		if authUser == nil {
			return nil, api.NewNotFoundError("user", user.GetUsername())
		}
	}

	// Update last authentication time
	authUser.LastAuth = time.Now()
	if err := pam.userStore.UpdateUser(authUser); err != nil {
		pam.logger.WithError(err).Warn("Failed to update user last auth time")
	}

	// Generate tokens
	token, err := pam.generateTokens(authUser)
	if err != nil {
		return nil, err
	}

	pam.logger.Infof("Token issued for user: %s (authenticated via external provider)", authUser.Username)
	return token, nil
}

// Authorize checks if a token has permission to perform an action on a resource.
//
// The authorization DECISION belongs to qrdb's policy decision point, reached
// through the SDK platform-token validator at the transport layer. The in-process
// auth manager carries no authorization authority of its own, so when
// authentication is enabled it fails closed and returns
// AuthorizerNotConfiguredError — it never makes a local RBAC decision.
func (pam *PersistentAuthManager) Authorize(ctx context.Context, token, resource, action string) error {
	if !pam.config.Enabled {
		return nil // Authentication disabled, allow all
	}

	// Fail closed: the manager has no authority to authorize against. The
	// authorization decision belongs to qrdb via the platform-token validator at
	// the transport layer, not to this in-process manager.
	return api.NewAuthorizerNotConfiguredError("auth.Authorize")
}

// RefreshToken generates a new access token from a refresh token.
func (pam *PersistentAuthManager) RefreshToken(ctx context.Context, refreshToken string) (*Token, error) {
	if !pam.config.Enabled {
		return nil, api.NewAuthenticationDisabledError("auth.RefreshToken")
	}

	// Validate refresh token
	claims, err := pam.validateToken(refreshToken)
	if err != nil {
		return nil, err
	}

	// Check if refresh token is in store
	if !pam.tokenStore.IsValidRefreshToken(claims.UserID, refreshToken) {
		return nil, api.NewInvalidRefreshTokenError("auth.RefreshToken")
	}

	// Get user
	user := pam.userStore.GetByID(claims.UserID)
	if user == nil {
		return nil, api.NewNotFoundError("user", claims.UserID)
	}

	// Generate new access token (keep same refresh token)
	token, err := pam.generateTokens(user)
	if err != nil {
		return nil, err
	}

	pam.logger.Infof("Token refreshed for user: %s", user.Username)
	return token, nil
}

// RevokeToken revokes an authentication token.
func (pam *PersistentAuthManager) RevokeToken(ctx context.Context, token string) error {
	if !pam.config.Enabled {
		return nil
	}

	pam.tokenStore.Revoke(token)
	pam.logger.Info("Token revoked")
	return nil
}

// CreateUser creates a new user record with specified roles.
//
// The user record carries identity for token issuance and RBAC. Authentication
// is performed by an IAM factor (mTLS, PIV, FIDO2/WebAuthn, OIDC); no password
// is stored.
func (pam *PersistentAuthManager) CreateUser(username string, roles []string) (*User, error) {
	if username == "" {
		return nil, api.NewRequiredFieldError("username")
	}

	// Check if user already exists
	if pam.userStore.GetByUsername(username) != nil {
		return nil, api.NewAlreadyExistsError("auth.CreateUser", "user", username)
	}

	userID, err := generateUserID()
	if err != nil {
		return nil, api.NewOperationFailedError("generate user ID", err)
	}

	user := &User{
		ID:       userID,
		Username: username,
		Roles:    roles,
		Created:  time.Now(),
	}

	if err := pam.userStore.AddUser(user); err != nil {
		return nil, err
	}

	pam.logger.Infof("User created: %s with roles: %v", username, roles)
	return user, nil
}

// DeleteUser deletes a user.
func (pam *PersistentAuthManager) DeleteUser(userID string) error {
	// Revoke all user tokens first
	pam.tokenStore.RevokeUserTokens(userID)

	// Delete user
	if err := pam.userStore.DeleteUser(userID); err != nil {
		return err
	}

	pam.logger.Infof("User deleted: %s", userID)
	return nil
}

// UpdateUserRoles updates a user's roles.
func (pam *PersistentAuthManager) UpdateUserRoles(userID string, roles []string) error {
	user := pam.userStore.GetByID(userID)
	if user == nil {
		return api.NewNotFoundError("user", userID)
	}

	user.Roles = roles
	return pam.userStore.UpdateUser(user)
}

// GetUserStore returns the user store.
func (pam *PersistentAuthManager) GetUserStore() UserStoreInterface {
	return pam.userStore
}

// GetTokenStore returns the token store.
func (pam *PersistentAuthManager) GetTokenStore() TokenStoreInterface {
	return pam.tokenStore
}

// generateTokens generates access and refresh tokens.
func (pam *PersistentAuthManager) generateTokens(user *User) (*Token, error) {
	return generateTokensForUser(user, pam.config, pam.tokenStore)
}

// validateToken validates a JWT token and returns claims.
// Supports both asymmetric and symmetric signing methods for migration.
func (pam *PersistentAuthManager) validateToken(tokenString string) (*Claims, error) {
	return validateJWTToken(tokenString, pam.config)
}

// Close closes the persistent auth manager and releases resources.
func (pam *PersistentAuthManager) Close() error {
	// Close token store cleanup goroutine
	if pts, ok := pam.tokenStore.(*PersistentTokenStore); ok {
		return pts.Close()
	}
	return nil
}

// ValidateToken validates a token and returns validation information.
// This is the public API for token validation.
func (pam *PersistentAuthManager) ValidateToken(ctx context.Context, token string) (*TokenValidationResult, error) {
	if !pam.config.Enabled {
		return &TokenValidationResult{
			Valid: false,
		}, api.NewAuthenticationDisabledError("auth.ValidateToken")
	}

	if token == "" {
		return &TokenValidationResult{
			Valid: false,
		}, api.NewRequiredFieldError("token")
	}

	// Parse and validate JWT token
	claims, err := pam.validateToken(token)
	if err != nil {
		return &TokenValidationResult{
			Valid: false,
		}, err
	}

	// Check if token is revoked
	isRevoked := pam.tokenStore.IsRevoked(token)

	result := &TokenValidationResult{
		Valid:     !isRevoked,
		UserID:    claims.UserID,
		Username:  claims.Username,
		TenantID:  claims.TenantID,
		Roles:     claims.Roles,
		ExpiresAt: claims.ExpiresAt.Time,
		IsRevoked: isRevoked,
	}

	if isRevoked {
		pam.logger.Warnf("Token validation failed: token revoked for user %s", claims.Username)
	}

	return result, nil
}
