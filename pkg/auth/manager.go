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

// Package auth provides JWT-based authentication and role-based access control (RBAC)
// for QuicBaaS. It includes token generation, validation, user authentication, and
// integration with external authentication providers (LDAP, OAuth).
//
// JWT signing supports two modes:
//   - Asymmetric (ES256/RS256 via KMS): Set JWTSigningKeyID in AuthConfig. This is
//     the recommended mode for production deployments.
//   - Symmetric (HS256): Set JWTSecret in AuthConfig. This is the legacy mode
//     retained for backward compatibility and development convenience.
//
// During migration, both modes can be active simultaneously. Tokens signed with
// either method are accepted for verification if the corresponding key material
// is configured.
package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
	"golang.org/x/crypto/bcrypt"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// MinPasswordLength is the minimum number of characters required for passwords
// when password-based authentication is enabled.
const MinPasswordLength = 12

// bcryptPrefix is the standard prefix for bcrypt hashes, used to distinguish
// bcrypt hashes from legacy SHA-256 hex hashes during migration.
const bcryptPrefix = "$2"

// InMemoryAuthManager manages authentication and authorization for QuicBaaS
// This is the in-memory (ephemeral) implementation - users are lost on restart.
// For persistent auth, use PersistentAuthManager via NewAuthManager().
type InMemoryAuthManager struct {
	config         *AuthConfig
	logger         *logger.Logger
	roleManager    *RoleManager
	tokenStore     *TokenStore
	userStore      *UserStore
	attemptTracker *LoginAttemptTracker
}

// AuthManager is an alias for InMemoryAuthManager for backwards compatibility.
type AuthManager = InMemoryAuthManager

// AuthConfig configures authentication behavior.
type AuthConfig struct {
	Enabled              bool          // Enable authentication
	JWTSecret            string        // Secret for HS256 JWT signing (legacy, use JWTSigningKeyID for production)
	JWTSigningKeyID      string        // KMS key ID for asymmetric JWT signing (e.g., "quicbaas-jwt-signing")
	JWTSigningAlgorithm  string        // Asymmetric algorithm: "ES256" (default), "ES384", "RS256", etc.
	TokenTTL             time.Duration // Access token TTL
	RefreshTokenTTL      time.Duration // Refresh token TTL
	RefreshWindow        time.Duration // Window before expiry when refresh is allowed
	MaxTokens            int           // Max active tokens per user
	AllowAnonymous       bool          // Allow unauthenticated access
	AllowPasswordAuth    bool          // Allow password-based authentication (for testing/dev environments)
	MaxLoginAttempts     int           // Max failed login attempts before lockout (0=disabled, <0=default of 5)
	LoginLockoutDuration time.Duration // Duration of account lockout after max attempts (default: 15m)
	InsecureDevMode      bool          // Allow short passwords and other insecure configurations for testing

	// jwtSigner is the KMS-backed private key for asymmetric token signing.
	// Set via SetAsymmetricSigning when JWTSigningKeyID is configured.
	jwtSigner crypto.Signer

	// jwtPublicKey is the corresponding public key for asymmetric token verification.
	// Set via SetAsymmetricSigning or SetPublicKey.
	jwtPublicKey crypto.PublicKey
}

// Token represents an authentication token.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

// User represents an authenticated user.
type User struct {
	ID       string    `json:"id"`
	Username string    `json:"username"`
	TenantID string    `json:"tenant_id,omitempty"` // Tenant ID for multi-tenant isolation
	Roles    []string  `json:"roles"`
	password string    // Hashed password (not exported)
	Created  time.Time `json:"created"`
	LastAuth time.Time `json:"last_auth"`
}

// GetID returns the user ID.
func (u *User) GetID() string {
	return u.ID
}

// GetUsername returns the username.
func (u *User) GetUsername() string {
	return u.Username
}

// GetTenantID returns the tenant ID.
func (u *User) GetTenantID() string {
	return u.TenantID
}

// GetRoles returns the user's roles.
func (u *User) GetRoles() []string {
	return u.Roles
}

// GetCreated returns the creation time.
func (u *User) GetCreated() time.Time {
	return u.Created
}

// GetLastAuth returns the last authentication time.
func (u *User) GetLastAuth() time.Time {
	return u.LastAuth
}

// Claims represents JWT token claims.
type Claims struct {
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	TenantID string   `json:"tenant_id,omitempty"` // Tenant ID for multi-tenant isolation
	Roles    []string `json:"roles"`
	Nonce    string   `json:"nonce"` // Unique nonce for token uniqueness
	jwt.RegisteredClaims
}

// TokenValidationResult contains the result of token validation.
type TokenValidationResult struct {
	Valid     bool      `json:"valid"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	TenantID  string    `json:"tenant_id,omitempty"` // Tenant ID for multi-tenant isolation
	Roles     []string  `json:"roles"`
	ExpiresAt time.Time `json:"expires_at"`
	IsRevoked bool      `json:"is_revoked"`
}

// GetValid returns the Valid field for interface compatibility.
func (r *TokenValidationResult) GetValid() bool {
	return r.Valid
}

// IsValid returns whether the token is valid for interface compatibility.
func (r *TokenValidationResult) IsValid() bool {
	return r.Valid
}

// GetUserID returns the UserID field for interface compatibility.
func (r *TokenValidationResult) GetUserID() string {
	return r.UserID
}

// GetUsername returns the Username field for interface compatibility.
func (r *TokenValidationResult) GetUsername() string {
	return r.Username
}

// GetTenantID returns the TenantID field for interface compatibility.
func (r *TokenValidationResult) GetTenantID() string {
	return r.TenantID
}

// GetRoles returns the Roles field for interface compatibility.
func (r *TokenValidationResult) GetRoles() []string {
	return r.Roles
}

// NewInMemoryAuthManager creates a new in-memory authentication manager.
// When JWTSigningKeyID is set, asymmetric signing is enabled and JWTSecret
// is optional (used only for verifying legacy HS256 tokens during migration).
// When JWTSigningKeyID is empty, JWTSecret is required for HS256 signing.
func NewInMemoryAuthManager(cfg *AuthConfig, log *logger.Logger) (*InMemoryAuthManager, error) {
	if cfg == nil {
		return nil, api.NewNilInputError("auth.NewAuthManager", "config")
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
		cfg.TokenTTL = 1 * time.Hour
	}
	if cfg.RefreshTokenTTL == 0 {
		cfg.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	if cfg.RefreshWindow == 0 {
		cfg.RefreshWindow = 5 * time.Minute
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 10
	}

	am := &InMemoryAuthManager{
		config:         cfg,
		logger:         log,
		roleManager:    NewRoleManager(log),
		tokenStore:     NewTokenStore(),
		userStore:      NewUserStore(),
		attemptTracker: NewLoginAttemptTracker(cfg.MaxLoginAttempts, cfg.LoginLockoutDuration, log),
	}

	if cfg.JWTSigningKeyID != "" {
		log.Infof("Authentication manager initialized with asymmetric signing (algorithm=%s, keyID=%s)",
			cfg.JWTSigningAlgorithm, cfg.JWTSigningKeyID)
	} else {
		log.Info("Authentication manager initialized with symmetric HS256 signing")
	}
	return am, nil
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
// the failure counter. Set MaxLoginAttempts=0 to disable throttling.
func (am *InMemoryAuthManager) Authenticate(ctx context.Context, username string, password *secure.Buffer) (*Token, error) {
	// Destroy the caller-owned password buffer on every return path, including
	// early-exit error paths. Destroy is idempotent and nil-safe.
	defer func() {
		if password != nil {
			password.Destroy()
		}
	}()

	if !am.config.Enabled {
		return nil, api.NewAuthenticationDisabledError("auth.Authenticate")
	}

	// Check if password authentication is allowed
	if !am.config.AllowPasswordAuth {
		return nil, api.NewPasswordAuthDisabledError("auth.Authenticate")
	}

	// Check if account is locked before attempting credential validation
	if err := am.attemptTracker.CheckLocked(username); err != nil {
		return nil, err
	}

	// Validate credentials
	user, err := am.validateCredentials(ctx, username, password)
	if err != nil {
		// Record the failed attempt (may trigger lockout)
		if lockErr := am.attemptTracker.RecordFailure(username); lockErr != nil {
			return nil, lockErr
		}
		return nil, err
	}

	// Successful login: reset the attempt counter
	am.attemptTracker.RecordSuccess(username)

	// Update last authentication time
	user.LastAuth = time.Now()
	am.userStore.UpdateUser(user)

	// Generate tokens
	token, err := am.generateTokens(user)
	if err != nil {
		return nil, err
	}

	am.logger.Infof("User authenticated: %s", username)
	return token, nil
}

// GetAttemptTracker returns the login attempt tracker for testing and monitoring.
func (am *InMemoryAuthManager) GetAttemptTracker() *LoginAttemptTracker {
	return am.attemptTracker
}

// IssueTokenForUser issues JWT tokens for a user who has been authenticated
// via mTLS certificate, FIDO2/WebAuthn, or other IAM provider.
// This should be called after successful authentication verification.
func (am *InMemoryAuthManager) IssueTokenForUser(ctx context.Context, user interfaces.UserInfo) (*Token, error) {
	if !am.config.Enabled {
		return nil, api.NewAuthenticationDisabledError("auth.IssueTokenForUser")
	}

	if user == nil {
		return nil, api.NewNilInputError("auth.IssueTokenForUser", "user")
	}

	// Type assert to *User for token generation
	authUser, ok := user.(*User)
	if !ok {
		// If it's not our User type, try to get by username
		existingUser := am.userStore.GetByUsername(user.GetUsername())
		if existingUser == nil {
			return nil, api.NewNotFoundError("user", user.GetUsername())
		}
		authUser, ok = existingUser.(*User)
		if !ok {
			return nil, api.NewInvalidUserTypeError("auth.IssueTokenForUser")
		}
	}

	// Update last authentication time
	authUser.LastAuth = time.Now()
	am.userStore.UpdateUser(authUser)

	// Generate tokens
	token, err := am.generateTokens(authUser)
	if err != nil {
		return nil, err
	}

	am.logger.Infof("Token issued for user: %s (authenticated via external provider)", authUser.Username)
	return token, nil
}

// Authorize checks if a token has permission to perform an action on a resource.
func (am *InMemoryAuthManager) Authorize(ctx context.Context, token, resource, action string) error {
	if !am.config.Enabled {
		return nil // Authentication disabled, allow all
	}

	if am.config.AllowAnonymous && token == "" {
		// Check if resource allows anonymous access
		if am.roleManager.AllowsAnonymous(resource, action) {
			return nil
		}
	}

	if token == "" {
		return api.NewTokenRequiredError("auth.Authorize")
	}

	// 1. Validate token
	claims, err := am.validateToken(token)
	if err != nil {
		return err
	}

	// 2. Check if token is revoked
	if am.tokenStore.IsRevoked(token) {
		return api.NewTokenRevokedError("auth.Authorize")
	}

	// 3. Check permissions
	if !am.roleManager.HasPermission(claims.Roles, resource, action) {
		am.logger.Warnf("Authorization denied: user=%s resource=%s action=%s",
			claims.Username, resource, action)
		return api.NewInsufficientPermissionsError("auth.Authorize")
	}

	return nil
}

// RefreshToken generates a new access token from a refresh token.
func (am *InMemoryAuthManager) RefreshToken(ctx context.Context, refreshToken string) (*Token, error) {
	if !am.config.Enabled {
		return nil, api.NewAuthenticationDisabledError("auth.RefreshToken")
	}

	// Validate refresh token
	claims, err := am.validateToken(refreshToken)
	if err != nil {
		return nil, err
	}

	// Check if refresh token is in store
	if !am.tokenStore.IsValidRefreshToken(claims.UserID, refreshToken) {
		return nil, api.NewInvalidRefreshTokenError("auth.RefreshToken")
	}

	// Get user
	userInfo := am.userStore.GetByID(claims.UserID)
	if userInfo == nil {
		return nil, api.NewNotFoundError("user", claims.UserID)
	}

	// Type assert to get the concrete User type
	user, ok := userInfo.(*User)
	if !ok {
		return nil, api.NewInvalidUserTypeError("auth.RefreshToken")
	}

	// Generate new access token (keep same refresh token)
	token, err := am.generateTokens(user)
	if err != nil {
		return nil, err
	}

	am.logger.Infof("Token refreshed for user: %s", user.Username)
	return token, nil
}

// RevokeToken revokes an authentication token.
func (am *InMemoryAuthManager) RevokeToken(ctx context.Context, token string) error {
	if !am.config.Enabled {
		return nil
	}

	am.tokenStore.Revoke(token)
	am.logger.Info("Token revoked")
	return nil
}

// ValidateToken validates a token and returns validation information
// This is the public API for token validation, used by gRPC handlers.
func (am *InMemoryAuthManager) ValidateToken(ctx context.Context, token string) (*TokenValidationResult, error) {
	if !am.config.Enabled {
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
	claims, err := am.validateToken(token)
	if err != nil {
		return &TokenValidationResult{
			Valid: false,
		}, err
	}

	// Check if token is revoked
	isRevoked := am.tokenStore.IsRevoked(token)

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
		am.logger.Warnf("Token validation failed: token revoked for user %s", claims.Username)
	}

	return result, nil
}

// CreateUser creates a new user with specified roles.
// For multi-tenant deployments, use CreateUserWithTenant instead.
func (am *InMemoryAuthManager) CreateUser(username, password string, roles []string) (*User, error) {
	return am.CreateUserWithTenant(username, password, "", roles)
}

// CreateUserWithTenant creates a new user with specified tenant and roles.
// This is the primary method for creating users in multi-tenant deployments.
// The tenantID will be included in JWT tokens to enable tenant isolation.
//
// NOTE: The password parameter is only used when AllowPasswordAuth is enabled.
// In production, password-based authentication should be disabled and
// IAM (WebAuthn, OIDC, OAuth) should be used for authentication instead.
func (am *InMemoryAuthManager) CreateUserWithTenant(username, password, tenantID string, roles []string) (*User, error) {
	if username == "" {
		return nil, api.NewRequiredFieldError("username")
	}

	// Check if user already exists
	if am.userStore.GetByUsername(username) != nil {
		return nil, api.NewAlreadyExistsError("auth.CreateUser", "user", username)
	}

	userID, err := generateUserID()
	if err != nil {
		return nil, api.NewOperationFailedError("generate user ID", err)
	}

	user := &User{
		ID:       userID,
		Username: username,
		TenantID: tenantID,
		Roles:    roles,
		Created:  time.Now(),
	}

	// Only set password if password authentication is allowed and password is provided
	if am.config.AllowPasswordAuth && password != "" {
		if !am.config.InsecureDevMode {
			if err := validatePasswordLength(password); err != nil {
				return nil, err
			}
		}
		hashedPassword, err := hashPassword(password)
		if err != nil {
			return nil, err
		}
		user.password = hashedPassword
	}

	am.userStore.AddUser(user)
	if tenantID != "" {
		am.logger.Infof("User created: %s with tenant: %s and roles: %v", username, tenantID, roles)
	} else {
		am.logger.Infof("User created: %s with roles: %v", username, roles)
	}
	return user, nil
}

// validateCredentials validates user credentials for password-based authentication.
// This method is only called when AllowPasswordAuth is enabled.
func (am *InMemoryAuthManager) validateCredentials(ctx context.Context, username string, password *secure.Buffer) (*User, error) {
	if username == "" || password == nil || password.Size() == 0 {
		return nil, api.NewCredentialsRequiredError("auth.validateCredentials")
	}

	userInfo := am.userStore.GetByUsername(username)
	if userInfo == nil {
		return nil, api.NewInvalidCredentialsError("auth.validateCredentials")
	}

	user, ok := userInfo.(*User)
	if !ok {
		return nil, api.NewInvalidUserTypeError("auth.validateCredentials")
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
		am.userStore.UpdateUser(user)
	}

	return user, nil
}

// generateTokens generates access and refresh tokens with unique nonces.
// Uses asymmetric signing when JWTSigningKeyID is configured, otherwise
// falls back to HS256 with the shared JWTSecret.
func (am *InMemoryAuthManager) generateTokens(user *User) (*Token, error) {
	now := time.Now()
	expiresAt := now.Add(am.config.TokenTTL)

	// Generate unique nonce for access token
	nonce, err := generateTokenNonce()
	if err != nil {
		return nil, api.NewOperationFailedError("generate nonce", err)
	}

	// Create access token with nonce for uniqueness
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		TenantID: user.TenantID,
		Roles:    user.Roles,
		Nonce:    nonce,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "quicbaas",
			Subject:   user.ID,
		},
	}

	accessTokenString, err := signToken(claims, am.config)
	if err != nil {
		return nil, api.NewOperationFailedError("sign access token", err)
	}

	// Generate separate nonce for refresh token
	refreshNonce, err := generateTokenNonce()
	if err != nil {
		return nil, api.NewOperationFailedError("generate refresh nonce", err)
	}

	// Create refresh token with its own nonce
	refreshClaims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		TenantID: user.TenantID,
		Roles:    user.Roles,
		Nonce:    refreshNonce,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(am.config.RefreshTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "quicbaas",
			Subject:   user.ID,
		},
	}

	refreshTokenString, err := signToken(refreshClaims, am.config)
	if err != nil {
		return nil, api.NewOperationFailedError("sign refresh token", err)
	}

	// Store refresh token
	am.tokenStore.AddRefreshToken(user.ID, refreshTokenString)

	return &Token{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresAt:    expiresAt,
		TokenType:    "Bearer",
	}, nil
}

// validateToken validates a JWT token and returns claims.
// Supports both asymmetric and symmetric signing methods for migration.
func (am *InMemoryAuthManager) validateToken(tokenString string) (*Claims, error) {
	return validateJWTToken(tokenString, am.config)
}

// hashToken hashes a token using SHA-256 for secure storage.
// This is used for storing tokens in the database, not for password hashing.
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// validatePasswordLength validates that a password meets the minimum length requirement.
func validatePasswordLength(password string) error {
	if len(password) < MinPasswordLength {
		return api.NewPasswordTooShortError("auth.validatePassword", MinPasswordLength, len(password))
	}
	return nil
}

// isBcryptHash returns true if the hash string is a bcrypt hash.
// Bcrypt hashes always start with "$2a$", "$2b$", or "$2y$".
func isBcryptHash(hash string) bool {
	return strings.HasPrefix(hash, bcryptPrefix)
}

// hashPassword hashes a password using bcrypt with the default cost factor.
// This provides salted, adaptive hashing with constant-time comparison.
func hashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", api.NewPasswordHashError("auth.hashPassword", err)
	}
	return string(hashed), nil
}

// verifyPassword verifies a password against a stored hash.
// It supports both bcrypt hashes (preferred) and legacy SHA-256 hashes for
// backward compatibility during migration. When a legacy hash matches, the
// caller receives the upgraded bcrypt hash to store.
//
// Returns:
//   - matched: true if the password matches the stored hash
//   - rehashed: non-empty bcrypt hash if a legacy SHA-256 hash was matched and
//     needs to be upgraded in storage; empty string if already bcrypt or no match
func verifyPassword(storedHash, password string) (matched bool, rehashed string) {
	// Primary path: bcrypt verification (constant-time comparison built-in)
	if isBcryptHash(storedHash) {
		err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password))
		return err == nil, ""
	}

	// Legacy fallback: SHA-256 hex hash (for pre-migration passwords)
	legacyHash := hashToken(password)
	if storedHash != legacyHash {
		return false, ""
	}

	// Password matched via legacy hash; upgrade to bcrypt
	upgraded, err := hashPassword(password)
	if err != nil {
		// Legacy hash matched but bcrypt upgrade failed; still authenticate
		// but skip the upgrade. The hash will be upgraded on next login.
		return true, ""
	}
	return true, upgraded
}

// verifyPasswordBuffer verifies a password supplied as an mlocked
// *secure.Buffer against a stored hash. It mirrors verifyPassword but
// takes raw bytes from the Buffer so the plaintext never becomes a Go
// string. Callers still own the Buffer and are responsible for its
// destruction.
//
// Returns:
//   - matched: true if the password matches the stored hash
//   - rehashed: non-empty bcrypt hash if a legacy SHA-256 hash was
//     matched and needs to be upgraded in storage; empty string if
//     already bcrypt or no match
func verifyPasswordBuffer(storedHash string, password *secure.Buffer) (matched bool, rehashed string) {
	if password == nil {
		return false, ""
	}
	pwBytes := password.Bytes()

	// Primary path: bcrypt verification (constant-time comparison built-in)
	if isBcryptHash(storedHash) {
		err := bcrypt.CompareHashAndPassword([]byte(storedHash), pwBytes)
		return err == nil, ""
	}

	// Legacy fallback: SHA-256 hex hash (for pre-migration passwords)
	hash := sha256.Sum256(pwBytes)
	legacyHash := hex.EncodeToString(hash[:])
	if storedHash != legacyHash {
		return false, ""
	}

	// Password matched via legacy hash; upgrade to bcrypt directly from the
	// mlocked bytes without copying into a string.
	upgraded, err := bcrypt.GenerateFromPassword(pwBytes, bcrypt.DefaultCost)
	if err != nil {
		// Legacy hash matched but bcrypt upgrade failed; still authenticate
		// but skip the upgrade. The hash will be upgraded on next login.
		return true, ""
	}
	return true, string(upgraded)
}

// generateUserID generates a unique user ID.
func generateUserID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// generateTokenNonce generates a unique nonce for token uniqueness.
func generateTokenNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// GetRoleManager returns the role manager.
func (am *InMemoryAuthManager) GetRoleManager() interfaces.RoleManager {
	return am.roleManager
}

// GetUserStore returns the user store.
func (am *InMemoryAuthManager) GetUserStore() interfaces.UserStore {
	return am.userStore
}

// Close cleans up resources used by the auth manager.
func (am *InMemoryAuthManager) Close() error {
	return nil
}
