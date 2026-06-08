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

// Package auth provides token and user storage
package auth

import (
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TokenStore manages active authentication tokens.
type TokenStore struct {
	refreshTokens map[string]map[string]time.Time // userID -> token -> expiry
	revokedTokens map[string]time.Time            // token -> revocation time
	mu            sync.RWMutex
}

// NewTokenStore creates a new token store.
func NewTokenStore() *TokenStore {
	ts := &TokenStore{
		refreshTokens: make(map[string]map[string]time.Time),
		revokedTokens: make(map[string]time.Time),
	}

	// Start background cleanup goroutine
	go ts.cleanupExpiredTokens()

	return ts
}

// AddRefreshToken stores a refresh token for a user.
func (ts *TokenStore) AddRefreshToken(userID, token string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if _, exists := ts.refreshTokens[userID]; !exists {
		ts.refreshTokens[userID] = make(map[string]time.Time)
	}

	ts.refreshTokens[userID][token] = time.Now().Add(7 * 24 * time.Hour) // 7 days
}

// IsValidRefreshToken checks if a refresh token is valid for a user.
func (ts *TokenStore) IsValidRefreshToken(userID, token string) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	userTokens, exists := ts.refreshTokens[userID]
	if !exists {
		return false
	}

	expiry, exists := userTokens[token]
	if !exists {
		return false
	}

	return time.Now().Before(expiry)
}

// Revoke marks a token as revoked.
func (ts *TokenStore) Revoke(token string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.revokedTokens[token] = time.Now()
}

// IsRevoked checks if a token has been revoked.
func (ts *TokenStore) IsRevoked(token string) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	_, exists := ts.revokedTokens[token]
	return exists
}

// RevokeUserTokens revokes all tokens for a user.
func (ts *TokenStore) RevokeUserTokens(userID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	userTokens, exists := ts.refreshTokens[userID]
	if !exists {
		return
	}

	// Mark all user tokens as revoked
	for token := range userTokens {
		ts.revokedTokens[token] = time.Now()
	}

	// Clear user's refresh tokens
	delete(ts.refreshTokens, userID)
}

// cleanupExpiredTokens periodically removes expired tokens.
func (ts *TokenStore) cleanupExpiredTokens() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		ts.mu.Lock()

		now := time.Now()

		// Clean expired refresh tokens
		for userID, tokens := range ts.refreshTokens {
			for token, expiry := range tokens {
				if now.After(expiry) {
					delete(tokens, token)
				}
			}
			if len(tokens) == 0 {
				delete(ts.refreshTokens, userID)
			}
		}

		// Clean old revoked tokens (older than 7 days)
		for token, revokedAt := range ts.revokedTokens {
			if now.Sub(revokedAt) > 7*24*time.Hour {
				delete(ts.revokedTokens, token)
			}
		}

		ts.mu.Unlock()
	}
}

// UserStore manages user accounts.
type UserStore struct {
	users       map[string]*User  // userID -> user
	usernameMap map[string]string // username -> userID
	mu          sync.RWMutex
}

// NewUserStore creates a new user store.
func NewUserStore() *UserStore {
	return &UserStore{
		users:       make(map[string]*User),
		usernameMap: make(map[string]string),
	}
}

// AddUser adds a user to the store.
// Accepts interfaces.UserInfo but expects *User for internal storage.
func (us *UserStore) AddUser(user interfaces.UserInfo) {
	us.mu.Lock()
	defer us.mu.Unlock()

	// Check if it's already a *User (internal use)
	if u, ok := user.(*User); ok {
		us.users[u.ID] = u
		us.usernameMap[u.Username] = u.ID
		return
	}

	// Convert from interface (external use)
	newUser := &User{
		ID:       user.GetID(),
		Username: user.GetUsername(),
		Roles:    user.GetRoles(),
		Created:  user.GetCreated(),
		LastAuth: user.GetLastAuth(),
	}
	us.users[newUser.ID] = newUser
	us.usernameMap[newUser.Username] = newUser.ID
}

// GetByID retrieves a user by ID.
func (us *UserStore) GetByID(userID string) interfaces.UserInfo {
	us.mu.RLock()
	defer us.mu.RUnlock()

	user := us.users[userID]
	if user == nil {
		return nil
	}
	return user
}

// GetByUsername retrieves a user by username.
func (us *UserStore) GetByUsername(username string) interfaces.UserInfo {
	us.mu.RLock()
	defer us.mu.RUnlock()

	userID, exists := us.usernameMap[username]
	if !exists {
		return nil
	}

	user := us.users[userID]
	if user == nil {
		return nil
	}
	return user
}

// UpdateUser updates user information.
// Accepts interfaces.UserInfo but expects *User for internal storage.
func (us *UserStore) UpdateUser(user interfaces.UserInfo) {
	us.mu.Lock()
	defer us.mu.Unlock()

	// Check if it's already a *User (internal use)
	if u, ok := user.(*User); ok {
		us.users[u.ID] = u
		return
	}

	// Convert from interface (external use)
	newUser := &User{
		ID:       user.GetID(),
		Username: user.GetUsername(),
		Roles:    user.GetRoles(),
		Created:  user.GetCreated(),
		LastAuth: user.GetLastAuth(),
	}
	us.users[newUser.ID] = newUser
}

// DeleteUser removes a user.
func (us *UserStore) DeleteUser(userID string) {
	us.mu.Lock()
	defer us.mu.Unlock()

	user, exists := us.users[userID]
	if !exists {
		return
	}

	delete(us.usernameMap, user.Username)
	delete(us.users, userID)
}

// ListUsers returns all users.
func (us *UserStore) ListUsers() []interfaces.UserInfo {
	us.mu.RLock()
	defer us.mu.RUnlock()

	users := make([]interfaces.UserInfo, 0, len(us.users))
	for _, user := range us.users {
		users = append(users, user)
	}

	return users
}

// AssignRole assigns a role to a user.
func (us *UserStore) AssignRole(userID, roleName string) error {
	us.mu.Lock()
	defer us.mu.Unlock()

	// Get user by ID
	user, exists := us.users[userID]
	if !exists {
		return api.NewUserNotFoundError("auth.UserStore.AssignRole", userID)
	}

	// Check if user already has the role
	for _, existingRole := range user.Roles {
		if existingRole == roleName {
			return api.NewUserAlreadyHasRoleError("auth.UserStore.AssignRole", roleName)
		}
	}

	// Add role to user's Roles slice
	user.Roles = append(user.Roles, roleName)

	// Update LastAuth timestamp (used as last modified)
	user.LastAuth = time.Now()

	// User is already in the map, so changes are automatically persisted
	// No need to call us.users[userID] = user since we're modifying the pointer

	return nil
}

// RevokeRole revokes a role from a user.
func (us *UserStore) RevokeRole(userID, roleName string) error {
	us.mu.Lock()
	defer us.mu.Unlock()

	// Get user by ID
	user, exists := us.users[userID]
	if !exists {
		return api.NewUserNotFoundError("auth.UserStore.RevokeRole", userID)
	}

	// Check if user has the role
	roleIndex := -1
	for i, existingRole := range user.Roles {
		if existingRole == roleName {
			roleIndex = i
			break
		}
	}

	if roleIndex == -1 {
		return api.NewUserDoesNotHaveRoleError("auth.UserStore.RevokeRole", roleName)
	}

	// Remove role from user's Roles slice
	user.Roles = append(user.Roles[:roleIndex], user.Roles[roleIndex+1:]...)

	// Update LastAuth timestamp (used as last modified)
	user.LastAuth = time.Now()

	// User is already in the map, so changes are automatically persisted
	// No need to call us.users[userID] = user since we're modifying the pointer

	return nil
}
