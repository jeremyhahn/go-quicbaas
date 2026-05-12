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

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// PersistentAuthManager manages authentication with database-backed persistence.
type PersistentAuthManager struct {
	config         *AuthConfig
	logger         *logger.Logger
	storage        interfaces.Storage
	roleManager    RoleManagerInterface
	tokenStore     TokenStoreInterface
	userStore      UserStoreInterface
	attemptTracker *LoginAttemptTracker
}

// RoleManagerInterface abstracts role management for both in-memory and persistent implementations.
type RoleManagerInterface interface {
	HasPermission(userRoles []string, resource, action string) bool
	AllowsAnonymous(resource, action string) bool
	AddRole(role *Role) error
	GetRole(name string) *Role
	ListRoles() []*Role
	DeleteRole(name string) error
	UpdateRole(role *Role) error
	ValidateRoles(roleNames []string) bool
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

	persistentRoleManager, err := NewPersistentRoleManager(storage, log)
	if err != nil {
		return nil, api.NewOperationFailedError("create persistent role manager", err)
	}

	persistentTokenStore, err := NewPersistentTokenStore(storage, log)
	if err != nil {
		return nil, api.NewOperationFailedError("create persistent token store", err)
	}

	pam := &PersistentAuthManager{
		config:         cfg,
		logger:         log,
		storage:        storage,
		roleManager:    persistentRoleManager,
		tokenStore:     persistentTokenStore,
		userStore:      persistentUserStore,
		attemptTracker: NewLoginAttemptTracker(cfg.MaxLoginAttempts, cfg.LoginLockoutDuration, log),
	}

	log.Info("Persistent authentication manager initialized")
	return pam, nil
}

// Authenticate validates credentials and issues authentication tokens.
// Password-based authentication is disabled by default for security.
// Set AllowPasswordAuth=true in AuthConfig to enable (for testing/dev only).
// For production, use mTLS certificates or FIDO2/WebAuthn for authentication,
// then call IssueTokenForUser.
//
// Login attempt throttling: After MaxLoginAttempts consecutive failures for a
// given username, the account is locked for LoginLockoutDuration. The lockout
// automatically expires after the duration elapses. A successful login resets
// the failure counter.
func (pam *PersistentAuthManager) Authenticate(ctx context.Context, username string, password *secure.Buffer) (*Token, error) {
	defer func() {
		if password != nil {
			password.Destroy()
		}
	}()

	if !pam.config.Enabled {
		return nil, api.NewAuthenticationDisabledError("auth.Authenticate")
	}

	// Check if password authentication is allowed
	if !pam.config.AllowPasswordAuth {
		return nil, api.NewPasswordAuthDisabledError("auth.Authenticate")
	}

	// Check if account is locked before attempting credential validation
	if err := pam.attemptTracker.CheckLocked(username); err != nil {
		return nil, err
	}

	// Validate credentials
	user, err := pam.validateCredentials(ctx, username, password)
	if err != nil {
		// Record the failed attempt (may trigger lockout)
		if lockErr := pam.attemptTracker.RecordFailure(username); lockErr != nil {
			return nil, lockErr
		}
		return nil, err
	}

	// Successful login: reset the attempt counter
	pam.attemptTracker.RecordSuccess(username)

	// Update last authentication time
	user.LastAuth = time.Now()
	if err := pam.userStore.UpdateUser(user); err != nil {
		pam.logger.WithError(err).Warn("Failed to update user last auth time")
	}

	// Generate tokens
	token, err := pam.generateTokens(user)
	if err != nil {
		return nil, err
	}

	pam.logger.Infof("User authenticated: %s", username)
	return token, nil
}

// GetAttemptTracker returns the login attempt tracker for testing and monitoring.
func (pam *PersistentAuthManager) GetAttemptTracker() *LoginAttemptTracker {
	return pam.attemptTracker
}

// IssueTokenForUser issues JWT tokens for a user who has been authenticated
// via mTLS certificate, FIDO2/WebAuthn, or other IAM provider.
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
func (pam *PersistentAuthManager) Authorize(ctx context.Context, token, resource, action string) error {
	if !pam.config.Enabled {
		return nil // Authentication disabled, allow all
	}

	if pam.config.AllowAnonymous && token == "" {
		// Check if resource allows anonymous access
		if pam.roleManager.AllowsAnonymous(resource, action) {
			return nil
		}
	}

	if token == "" {
		return api.NewTokenRequiredError("auth.Authorize")
	}

	// 1. Validate token
	claims, err := pam.validateToken(token)
	if err != nil {
		return err
	}

	// 2. Check if token is revoked
	if pam.tokenStore.IsRevoked(token) {
		return api.NewTokenRevokedError("auth.Authorize")
	}

	// 3. Check permissions
	if !pam.roleManager.HasPermission(claims.Roles, resource, action) {
		pam.logger.Warnf("Authorization denied: user=%s resource=%s action=%s",
			claims.Username, resource, action)
		return api.NewInsufficientPermissionsError("auth.Authorize")
	}

	return nil
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

// CreateUser creates a new user with specified roles.
// NOTE: The password parameter is only used when AllowPasswordAuth is enabled.
// In production, password-based authentication should be disabled and
// IAM (WebAuthn, OIDC, OAuth) should be used for authentication instead.
func (pam *PersistentAuthManager) CreateUser(username, password string, roles []string) (*User, error) {
	if username == "" {
		return nil, api.NewRequiredFieldError("username")
	}

	// Check if user already exists
	if pam.userStore.GetByUsername(username) != nil {
		return nil, api.NewAlreadyExistsError("auth.CreateUser", "user", username)
	}

	// Validate roles exist
	if !pam.roleManager.ValidateRoles(roles) {
		return nil, api.NewRolesNotFoundError("auth.CreateUser")
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

	// Only set password if password authentication is allowed and password is provided
	if pam.config.AllowPasswordAuth && password != "" {
		if err := validatePasswordLength(password); err != nil {
			return nil, err
		}
		hashedPassword, err := hashPassword(password)
		if err != nil {
			return nil, err
		}
		user.password = hashedPassword
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
	// Validate roles exist
	if !pam.roleManager.ValidateRoles(roles) {
		return api.NewRolesNotFoundError("auth.UpdateUserRoles")
	}

	user := pam.userStore.GetByID(userID)
	if user == nil {
		return api.NewNotFoundError("user", userID)
	}

	user.Roles = roles
	return pam.userStore.UpdateUser(user)
}

// GetRoleManager returns the role manager.
func (pam *PersistentAuthManager) GetRoleManager() RoleManagerInterface {
	return pam.roleManager
}

// GetUserStore returns the user store.
func (pam *PersistentAuthManager) GetUserStore() UserStoreInterface {
	return pam.userStore
}

// GetTokenStore returns the token store.
func (pam *PersistentAuthManager) GetTokenStore() TokenStoreInterface {
	return pam.tokenStore
}

// validateCredentials validates user credentials for password-based authentication.
// This method is only called when AllowPasswordAuth is enabled.
func (pam *PersistentAuthManager) validateCredentials(ctx context.Context, username string, password *secure.Buffer) (*User, error) {
	if username == "" || password == nil || password.Size() == 0 {
		return nil, api.NewCredentialsRequiredError("auth.validateCredentials")
	}

	user := pam.userStore.GetByUsername(username)
	if user == nil {
		return nil, api.NewInvalidCredentialsError("auth.validateCredentials")
	}

	if user.password == "" {
		return nil, api.NewNoPasswordSetError("auth.validateCredentials")
	}

	matched, rehashed := verifyPasswordBuffer(user.password, password)
	if !matched {
		return nil, api.NewInvalidCredentialsError("auth.validateCredentials")
	}

	// Transparently upgrade legacy SHA-256 hash to bcrypt on successful login
	if rehashed != "" {
		user.password = rehashed
		if err := pam.userStore.UpdateUser(user); err != nil {
			pam.logger.WithError(err).Warn("Failed to persist bcrypt password upgrade")
		}
	}

	return user, nil
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
