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

// Package auth provides persistent storage for users, roles, and tokens
package auth

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// PersistentUserStore provides database-backed user storage with in-memory caching.
type PersistentUserStore struct {
	storage    interfaces.Storage
	logger     *logger.Logger
	keyBuilder *KeyBuilder
	cache      map[string]*User  // userID -> User (for fast lookup)
	nameIndex  map[string]string // username -> userID
	mu         sync.RWMutex
}

// NewPersistentUserStore creates a new persistent user store.
func NewPersistentUserStore(storage interfaces.Storage, logger *logger.Logger) (*PersistentUserStore, error) {
	if storage == nil {
		return nil, api.NewNilInputError("auth.NewPersistentUserStore", "storage")
	}

	if logger == nil {
		return nil, api.NewNilInputError("auth.NewPersistentUserStore", "logger")
	}

	ps := &PersistentUserStore{
		storage:    storage,
		logger:     logger,
		keyBuilder: NewKeyBuilder(),
		cache:      make(map[string]*User),
		nameIndex:  make(map[string]string),
	}

	// Load all users into cache on startup
	if err := ps.loadAllUsers(); err != nil {
		return nil, api.NewOperationFailedError("load users", err)
	}

	logger.Infof("Persistent user store initialized with %d users", len(ps.cache))
	return ps, nil
}

// AddUser adds a user to persistent storage.
func (ps *PersistentUserStore) AddUser(user *User) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Check if user already exists
	if _, exists := ps.cache[user.ID]; exists {
		return api.NewAlreadyExistsError("auth.PersistentUserStore.AddUser", "user", user.ID)
	}

	if _, exists := ps.nameIndex[user.Username]; exists {
		return api.NewAlreadyExistsError("auth.PersistentUserStore.AddUser", "username", user.Username)
	}

	// Serialize user data
	userData, err := json.Marshal(user)
	if err != nil {
		return api.NewOperationFailedError("marshal user", err)
	}

	// Create a batch for atomic write
	batch := ps.storage.Batch()
	defer batch.Close()

	// Store user data: __quicbaas:users:{userID}
	userKey := ps.keyBuilder.UserKey(user.ID)
	if err := batch.Set(userKey, userData); err != nil {
		return api.NewOperationFailedError("set user data", err)
	}

	// Store username index: __quicbaas:users:byname:{username} -> userID
	nameKey := ps.keyBuilder.UserByNameKey(user.Username)
	if err := batch.Set(nameKey, []byte(user.ID)); err != nil {
		return api.NewOperationFailedError("set username index", err)
	}

	// Commit batch
	if err := batch.Commit(); err != nil {
		return api.NewOperationFailedError("commit batch", err)
	}

	// Update cache
	ps.cache[user.ID] = user
	ps.nameIndex[user.Username] = user.ID

	ps.logger.Infof("User added to persistent storage: %s (ID: %s)", user.Username, user.ID)
	return nil
}

// GetByID retrieves a user by ID.
func (ps *PersistentUserStore) GetByID(userID string) *User {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	// Check cache first
	if user, exists := ps.cache[userID]; exists {
		return user
	}

	// If not in cache, try loading from storage
	ps.mu.RUnlock()
	ps.mu.Lock()
	defer func() {
		ps.mu.Unlock()
		ps.mu.RLock()
	}()

	// Double-check after acquiring write lock
	if user, exists := ps.cache[userID]; exists {
		return user
	}

	// Load from storage
	userKey := ps.keyBuilder.UserKey(userID)
	userData, err := ps.storage.Get(userKey)
	if err != nil {
		ps.logger.WithError(err).Debugf("User not found in storage: %s", userID)
		return nil
	}

	var user User
	if err := json.Unmarshal(userData, &user); err != nil {
		ps.logger.WithError(err).Errorf("Failed to unmarshal user data: %s", userID)
		return nil
	}

	// Update cache
	ps.cache[userID] = &user
	ps.nameIndex[user.Username] = user.ID

	return &user
}

// GetByUsername retrieves a user by username.
func (ps *PersistentUserStore) GetByUsername(username string) *User {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	// Check name index in cache
	userID, exists := ps.nameIndex[username]
	if exists {
		return ps.cache[userID]
	}

	// If not in cache, try loading from storage
	ps.mu.RUnlock()
	ps.mu.Lock()
	defer func() {
		ps.mu.Unlock()
		ps.mu.RLock()
	}()

	// Double-check after acquiring write lock
	userID, exists = ps.nameIndex[username]
	if exists {
		return ps.cache[userID]
	}

	// Load username index from storage
	nameKey := ps.keyBuilder.UserByNameKey(username)
	userIDBytes, err := ps.storage.Get(nameKey)
	if err != nil {
		ps.logger.WithError(err).Debugf("Username not found in storage: %s", username)
		return nil
	}

	userID = string(userIDBytes)

	// Load user data
	userKey := ps.keyBuilder.UserKey(userID)
	userData, err := ps.storage.Get(userKey)
	if err != nil {
		ps.logger.WithError(err).Errorf("User data not found for ID: %s", userID)
		return nil
	}

	var user User
	if err := json.Unmarshal(userData, &user); err != nil {
		ps.logger.WithError(err).Errorf("Failed to unmarshal user data: %s", userID)
		return nil
	}

	// Update cache
	ps.cache[userID] = &user
	ps.nameIndex[username] = userID

	return &user
}

// UpdateUser updates user information in persistent storage.
func (ps *PersistentUserStore) UpdateUser(user *User) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Verify user exists
	if _, exists := ps.cache[user.ID]; !exists {
		return api.NewNotFoundError("user", user.ID)
	}

	// Serialize user data
	userData, err := json.Marshal(user)
	if err != nil {
		return api.NewOperationFailedError("marshal user", err)
	}

	// Update storage
	userKey := ps.keyBuilder.UserKey(user.ID)
	if err := ps.storage.Set(userKey, userData); err != nil {
		return api.NewOperationFailedError("update user data", err)
	}

	// Update cache
	ps.cache[user.ID] = user

	ps.logger.Debugf("User updated in persistent storage: %s", user.Username)
	return nil
}

// DeleteUser removes a user from persistent storage.
func (ps *PersistentUserStore) DeleteUser(userID string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Get user to find username for index cleanup
	user, exists := ps.cache[userID]
	if !exists {
		return api.NewNotFoundError("user", userID)
	}

	// Create batch for atomic delete
	batch := ps.storage.Batch()
	defer batch.Close()

	// Delete user data
	userKey := ps.keyBuilder.UserKey(userID)
	if err := batch.Delete(userKey); err != nil {
		return api.NewOperationFailedError("delete user data", err)
	}

	// Delete username index
	nameKey := ps.keyBuilder.UserByNameKey(user.Username)
	if err := batch.Delete(nameKey); err != nil {
		return api.NewOperationFailedError("delete username index", err)
	}

	// Commit batch
	if err := batch.Commit(); err != nil {
		return api.NewOperationFailedError("commit batch", err)
	}

	// Update cache
	delete(ps.cache, userID)
	delete(ps.nameIndex, user.Username)

	ps.logger.Infof("User deleted from persistent storage: %s (ID: %s)", user.Username, userID)
	return nil
}

// ListUsers returns all users from the cache.
func (ps *PersistentUserStore) ListUsers() []*User {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	users := make([]*User, 0, len(ps.cache))
	for _, user := range ps.cache {
		users = append(users, user)
	}

	return users
}

// AssignRole assigns a role to a user in persistent storage.
func (ps *PersistentUserStore) AssignRole(userID, roleName string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Get user by ID
	user, exists := ps.cache[userID]
	if !exists {
		return api.NewNotFoundError("user", userID)
	}

	// Check if user already has the role
	for _, existingRole := range user.Roles {
		if existingRole == roleName {
			return api.NewUserHasRoleError("auth.PersistentUserStore.AssignRole", roleName)
		}
	}

	// Add role to user's Roles slice
	user.Roles = append(user.Roles, roleName)

	// Update LastAuth timestamp (used as last modified)
	user.LastAuth = time.Now()

	// Serialize and persist user data
	userData, err := json.Marshal(user)
	if err != nil {
		return api.NewOperationFailedError("marshal user", err)
	}

	userKey := ps.keyBuilder.UserKey(user.ID)
	if err := ps.storage.Set(userKey, userData); err != nil {
		return api.NewOperationFailedError("persist user data", err)
	}

	ps.logger.Infof("Role '%s' assigned to user %s (ID: %s)", roleName, user.Username, userID)
	return nil
}

// RevokeRole revokes a role from a user in persistent storage.
func (ps *PersistentUserStore) RevokeRole(userID, roleName string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Get user by ID
	user, exists := ps.cache[userID]
	if !exists {
		return api.NewNotFoundError("user", userID)
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
		return api.NewUserMissingRoleError("auth.PersistentUserStore.RevokeRole", roleName)
	}

	// Remove role from user's Roles slice
	user.Roles = append(user.Roles[:roleIndex], user.Roles[roleIndex+1:]...)

	// Update LastAuth timestamp (used as last modified)
	user.LastAuth = time.Now()

	// Serialize and persist user data
	userData, err := json.Marshal(user)
	if err != nil {
		return api.NewOperationFailedError("marshal user", err)
	}

	userKey := ps.keyBuilder.UserKey(user.ID)
	if err := ps.storage.Set(userKey, userData); err != nil {
		return api.NewOperationFailedError("persist user data", err)
	}

	ps.logger.Infof("Role '%s' revoked from user %s (ID: %s)", roleName, user.Username, userID)
	return nil
}

// loadAllUsers loads all users from persistent storage into cache.
func (ps *PersistentUserStore) loadAllUsers() error {
	// Create iterator for user keys: __quicbaas:users:*
	// Use LowerBound/UpperBound to create a prefix range
	prefix := []byte(KeyPrefixUsers)
	opts := &storage.IteratorOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	}
	iter := ps.storage.NewIterator(opts)
	defer iter.Close()

	count := 0
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()

		// Skip username index keys (they contain ":byname:")
		if len(key) > len(KeyPrefixUsersByName) &&
			string(key[:len(KeyPrefixUsersByName)]) == KeyPrefixUsersByName {
			continue
		}

		// Skip metadata keys (they contain ":meta:")
		if len(key) > len(KeyPrefixUserMetadata) &&
			string(key[:len(KeyPrefixUserMetadata)]) == KeyPrefixUserMetadata {
			continue
		}

		var user User
		if err := json.Unmarshal(value, &user); err != nil {
			ps.logger.WithError(err).Warnf("Failed to unmarshal user data for key: %s", string(key))
			continue
		}

		ps.cache[user.ID] = &user
		ps.nameIndex[user.Username] = user.ID
		count++
	}

	if err := iter.Error(); err != nil {
		return api.NewIteratorError("auth.PersistentUserStore.loadAllUsers", err)
	}

	ps.logger.Infof("Loaded %d users from persistent storage", count)
	return nil
}

// Count returns the number of users in the store.
func (ps *PersistentUserStore) Count() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.cache)
}

// PersistentTokenStore provides database-backed token storage.
type PersistentTokenStore struct {
	storage    interfaces.Storage
	logger     *logger.Logger
	keyBuilder *KeyBuilder
	cache      struct {
		refreshTokens map[string]map[string]time.Time // userID -> {tokenHash -> expiry}
		revokedTokens map[string]time.Time            // tokenHash -> revocationTime
		mu            sync.RWMutex
	}
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewPersistentTokenStore creates a new persistent token store.
func NewPersistentTokenStore(storage interfaces.Storage, logger *logger.Logger) (*PersistentTokenStore, error) {
	if storage == nil {
		return nil, api.NewNilInputError("auth.NewPersistentTokenStore", "storage")
	}

	if logger == nil {
		return nil, api.NewNilInputError("auth.NewPersistentTokenStore", "logger")
	}

	pts := &PersistentTokenStore{
		storage:    storage,
		logger:     logger,
		keyBuilder: NewKeyBuilder(),
		stopCh:     make(chan struct{}),
	}
	pts.cache.refreshTokens = make(map[string]map[string]time.Time)
	pts.cache.revokedTokens = make(map[string]time.Time)

	// Load tokens from storage
	if err := pts.loadTokens(); err != nil {
		return nil, api.NewOperationFailedError("load tokens", err)
	}

	// Start background cleanup goroutine
	pts.wg.Add(1)
	go pts.cleanupExpiredTokens()

	logger.Info("Persistent token store initialized")
	return pts, nil
}

// AddRefreshToken stores a refresh token in persistent storage.
func (pts *PersistentTokenStore) AddRefreshToken(userID, token string) {
	tokenHash := hashToken(token)
	expiry := time.Now().Add(7 * 24 * time.Hour)

	// Store in database
	key := pts.keyBuilder.RefreshTokenKey(userID, tokenHash)
	expiryBytes, _ := expiry.MarshalBinary()
	if err := pts.storage.Set(key, expiryBytes); err != nil {
		pts.logger.WithError(err).Error("Failed to persist refresh token")
		return
	}

	// Update cache
	pts.cache.mu.Lock()
	if _, exists := pts.cache.refreshTokens[userID]; !exists {
		pts.cache.refreshTokens[userID] = make(map[string]time.Time)
	}
	pts.cache.refreshTokens[userID][tokenHash] = expiry
	pts.cache.mu.Unlock()

	pts.logger.Debugf("Refresh token persisted for user: %s", userID)
}

// IsValidRefreshToken checks if a refresh token is valid.
func (pts *PersistentTokenStore) IsValidRefreshToken(userID, token string) bool {
	tokenHash := hashToken(token)

	pts.cache.mu.RLock()
	defer pts.cache.mu.RUnlock()

	userTokens, exists := pts.cache.refreshTokens[userID]
	if !exists {
		return false
	}

	expiry, exists := userTokens[tokenHash]
	if !exists {
		return false
	}

	return time.Now().Before(expiry)
}

// Revoke marks a token as revoked in persistent storage.
func (pts *PersistentTokenStore) Revoke(token string) {
	tokenHash := hashToken(token)
	now := time.Now()

	// Store in database
	key := pts.keyBuilder.RevokedTokenKey(tokenHash)
	nowBytes, _ := now.MarshalBinary()
	if err := pts.storage.Set(key, nowBytes); err != nil {
		pts.logger.WithError(err).Error("Failed to persist token revocation")
		return
	}

	// Update cache
	pts.cache.mu.Lock()
	pts.cache.revokedTokens[tokenHash] = now
	pts.cache.mu.Unlock()

	pts.logger.Debug("Token revoked and persisted")
}

// IsRevoked checks if a token has been revoked.
func (pts *PersistentTokenStore) IsRevoked(token string) bool {
	tokenHash := hashToken(token)

	pts.cache.mu.RLock()
	defer pts.cache.mu.RUnlock()

	_, exists := pts.cache.revokedTokens[tokenHash]
	return exists
}

// RevokeUserTokens revokes all tokens for a user.
func (pts *PersistentTokenStore) RevokeUserTokens(userID string) {
	pts.cache.mu.Lock()
	userTokens, exists := pts.cache.refreshTokens[userID]
	if !exists {
		pts.cache.mu.Unlock()
		return
	}

	now := time.Now()
	nowBytes, _ := now.MarshalBinary()

	// Create batch for atomic revocation
	batch := pts.storage.Batch()
	defer batch.Close()

	// Revoke all user tokens
	for tokenHash := range userTokens {
		// Add to revoked list
		key := pts.keyBuilder.RevokedTokenKey(tokenHash)
		_ = batch.Set(key, nowBytes)
		pts.cache.revokedTokens[tokenHash] = now

		// Delete from refresh tokens
		refreshKey := pts.keyBuilder.RefreshTokenKey(userID, tokenHash)
		_ = batch.Delete(refreshKey)
	}

	// Clear user's refresh tokens from cache
	delete(pts.cache.refreshTokens, userID)
	pts.cache.mu.Unlock()

	if err := batch.Commit(); err != nil {
		pts.logger.WithError(err).Error("Failed to revoke user tokens")
		return
	}

	pts.logger.Infof("All tokens revoked for user: %s", userID)
}

// Close stops background cleanup and releases resources.
func (pts *PersistentTokenStore) Close() error {
	close(pts.stopCh)
	pts.wg.Wait()
	return nil
}

// loadTokens loads all tokens from persistent storage.
func (pts *PersistentTokenStore) loadTokens() error {
	ctx := context.Background()

	// Load refresh tokens
	refreshPrefix := []byte(KeyPrefixRefreshTokens)
	refreshOpts := &storage.IteratorOptions{
		LowerBound: refreshPrefix,
		UpperBound: prefixUpperBound(refreshPrefix),
	}
	refreshIter := pts.storage.NewIterator(refreshOpts)
	defer refreshIter.Close()

	for refreshIter.SeekToFirst(); refreshIter.Valid(); refreshIter.Next() {
		key := string(refreshIter.Key())
		value := refreshIter.Value()

		// Parse key: __quicbaas:tokens:refresh:{userID}:{tokenHash}
		prefix := KeyPrefixRefreshTokens
		if len(key) <= len(prefix) {
			continue
		}
		remainder := key[len(prefix):]
		// Find the separator between userID and tokenHash
		sepIdx := -1
		for i := len(remainder) - 1; i >= 0; i-- {
			if remainder[i] == ':' {
				sepIdx = i
				break
			}
		}
		if sepIdx == -1 {
			continue
		}

		userID := remainder[:sepIdx]
		tokenHash := remainder[sepIdx+1:]

		var expiry time.Time
		if err := expiry.UnmarshalBinary(value); err != nil {
			pts.logger.WithError(err).Warn("Failed to unmarshal token expiry")
			continue
		}

		// Only load non-expired tokens
		if time.Now().Before(expiry) {
			if _, exists := pts.cache.refreshTokens[userID]; !exists {
				pts.cache.refreshTokens[userID] = make(map[string]time.Time)
			}
			pts.cache.refreshTokens[userID][tokenHash] = expiry
		}
	}

	// Load revoked tokens
	revokedPrefix := []byte(KeyPrefixRevokedTokens)
	revokedOpts := &storage.IteratorOptions{
		LowerBound: revokedPrefix,
		UpperBound: prefixUpperBound(revokedPrefix),
	}
	revokedIter := pts.storage.NewIterator(revokedOpts)
	defer revokedIter.Close()

	for revokedIter.SeekToFirst(); revokedIter.Valid(); revokedIter.Next() {
		key := string(revokedIter.Key())
		value := revokedIter.Value()

		// Parse key: __quicbaas:tokens:revoked:{tokenHash}
		prefix := KeyPrefixRevokedTokens
		if len(key) <= len(prefix) {
			continue
		}
		tokenHash := key[len(prefix):]

		var revokedAt time.Time
		if err := revokedAt.UnmarshalBinary(value); err != nil {
			pts.logger.WithError(err).Warn("Failed to unmarshal revocation time")
			continue
		}

		// Only load recent revocations (within 7 days)
		if time.Since(revokedAt) < 7*24*time.Hour {
			pts.cache.revokedTokens[tokenHash] = revokedAt
		}
	}

	_ = ctx // suppress unused warning
	pts.logger.Info("Tokens loaded from persistent storage")
	return nil
}

// cleanupExpiredTokens periodically removes expired tokens.
func (pts *PersistentTokenStore) cleanupExpiredTokens() {
	defer pts.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pts.performCleanup()
		case <-pts.stopCh:
			return
		}
	}
}

// performCleanup removes expired tokens from storage and cache.
func (pts *PersistentTokenStore) performCleanup() {
	now := time.Now()
	batch := pts.storage.Batch()
	defer batch.Close()

	pts.cache.mu.Lock()
	defer pts.cache.mu.Unlock()

	// Clean expired refresh tokens
	for userID, tokens := range pts.cache.refreshTokens {
		for tokenHash, expiry := range tokens {
			if now.After(expiry) {
				key := pts.keyBuilder.RefreshTokenKey(userID, tokenHash)
				_ = batch.Delete(key)
				delete(tokens, tokenHash)
			}
		}
		if len(tokens) == 0 {
			delete(pts.cache.refreshTokens, userID)
		}
	}

	// Clean old revoked tokens (older than 7 days)
	for tokenHash, revokedAt := range pts.cache.revokedTokens {
		if now.Sub(revokedAt) > 7*24*time.Hour {
			key := pts.keyBuilder.RevokedTokenKey(tokenHash)
			_ = batch.Delete(key)
			delete(pts.cache.revokedTokens, tokenHash)
		}
	}

	if err := batch.Commit(); err != nil {
		pts.logger.WithError(err).Error("Failed to cleanup expired tokens")
	} else {
		pts.logger.Debug("Token cleanup completed")
	}
}

// Note: hashToken is now defined in manager.go and provides SHA-256 hashing
// for secure token storage. This package uses that function from the same package.

// prefixUpperBound returns the upper bound for a prefix scan
// This creates an exclusive upper bound by incrementing the last byte.
func prefixUpperBound(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}

	// Make a copy and increment the last byte
	end := make([]byte, len(prefix))
	copy(end, prefix)

	// Find the last non-0xFF byte and increment it
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] < 0xFF {
			end[i]++
			return end[:i+1]
		}
	}

	// All bytes are 0xFF, no upper bound needed
	return nil
}
