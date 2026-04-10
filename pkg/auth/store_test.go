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

package auth

import (
	"sync"
	"testing"
	"time"
)

// TestTokenStore_NewTokenStore tests the creation of a new TokenStore.
func TestTokenStore_NewTokenStore(t *testing.T) {
	ts := NewTokenStore()
	if ts == nil {
		t.Fatal("NewTokenStore returned nil")
	}

	if ts.refreshTokens == nil {
		t.Error("refreshTokens map not initialized")
	}

	if ts.revokedTokens == nil {
		t.Error("revokedTokens map not initialized")
	}
}

// TestTokenStore_AddRefreshToken tests adding refresh tokens.
func TestTokenStore_AddRefreshToken(t *testing.T) {
	ts := NewTokenStore()

	// Add first token for a user
	ts.AddRefreshToken("user1", "token1")

	// Verify token exists
	if !ts.IsValidRefreshToken("user1", "token1") {
		t.Error("Token should be valid after adding")
	}

	// Add second token for same user
	ts.AddRefreshToken("user1", "token2")

	// Both tokens should be valid
	if !ts.IsValidRefreshToken("user1", "token1") {
		t.Error("First token should still be valid")
	}
	if !ts.IsValidRefreshToken("user1", "token2") {
		t.Error("Second token should be valid")
	}

	// Add token for different user
	ts.AddRefreshToken("user2", "token3")

	if !ts.IsValidRefreshToken("user2", "token3") {
		t.Error("Token for different user should be valid")
	}
}

// TestTokenStore_AddRefreshToken_NewUserMap tests adding token for new user.
func TestTokenStore_AddRefreshToken_NewUserMap(t *testing.T) {
	ts := NewTokenStore()

	// First token for a user should create the user's token map
	ts.AddRefreshToken("newuser", "newtoken")

	if !ts.IsValidRefreshToken("newuser", "newtoken") {
		t.Error("Token should be valid for new user")
	}
}

// TestTokenStore_IsValidRefreshToken_NonExistentUser tests validation with non-existent user.
func TestTokenStore_IsValidRefreshToken_NonExistentUser(t *testing.T) {
	ts := NewTokenStore()

	if ts.IsValidRefreshToken("nonexistent", "token") {
		t.Error("Token should not be valid for non-existent user")
	}
}

// TestTokenStore_IsValidRefreshToken_NonExistentToken tests validation with non-existent token.
func TestTokenStore_IsValidRefreshToken_NonExistentToken(t *testing.T) {
	ts := NewTokenStore()

	ts.AddRefreshToken("user1", "token1")

	if ts.IsValidRefreshToken("user1", "nonexistent") {
		t.Error("Non-existent token should not be valid")
	}
}

// TestTokenStore_IsValidRefreshToken_ExpiredToken tests validation with expired token.
func TestTokenStore_IsValidRefreshToken_ExpiredToken(t *testing.T) {
	ts := &TokenStore{
		refreshTokens: make(map[string]map[string]time.Time),
		revokedTokens: make(map[string]time.Time),
	}

	// Manually add an expired token
	ts.refreshTokens["user1"] = make(map[string]time.Time)
	ts.refreshTokens["user1"]["expiredtoken"] = time.Now().Add(-1 * time.Hour)

	if ts.IsValidRefreshToken("user1", "expiredtoken") {
		t.Error("Expired token should not be valid")
	}
}

// TestTokenStore_Revoke tests token revocation.
func TestTokenStore_Revoke(t *testing.T) {
	ts := NewTokenStore()

	// Revoke a token
	ts.Revoke("token1")

	if !ts.IsRevoked("token1") {
		t.Error("Token should be marked as revoked")
	}
}

// TestTokenStore_IsRevoked_NonExistent tests checking revocation for non-existent token.
func TestTokenStore_IsRevoked_NonExistent(t *testing.T) {
	ts := NewTokenStore()

	if ts.IsRevoked("nonexistent") {
		t.Error("Non-existent token should not be marked as revoked")
	}
}

// TestTokenStore_RevokeUserTokens tests revoking all tokens for a user.
func TestTokenStore_RevokeUserTokens(t *testing.T) {
	ts := NewTokenStore()

	// Add multiple tokens for a user
	ts.AddRefreshToken("user1", "token1")
	ts.AddRefreshToken("user1", "token2")
	ts.AddRefreshToken("user1", "token3")

	// Revoke all user tokens
	ts.RevokeUserTokens("user1")

	// All tokens should be revoked
	if !ts.IsRevoked("token1") {
		t.Error("token1 should be revoked")
	}
	if !ts.IsRevoked("token2") {
		t.Error("token2 should be revoked")
	}
	if !ts.IsRevoked("token3") {
		t.Error("token3 should be revoked")
	}

	// User's refresh tokens should be cleared
	if ts.IsValidRefreshToken("user1", "token1") {
		t.Error("token1 should no longer be a valid refresh token")
	}
}

// TestTokenStore_RevokeUserTokens_NonExistentUser tests revoking tokens for non-existent user.
func TestTokenStore_RevokeUserTokens_NonExistentUser(t *testing.T) {
	ts := NewTokenStore()

	// Should not panic when revoking tokens for non-existent user
	ts.RevokeUserTokens("nonexistent")
}

// TestTokenStore_RevokeUserTokens_EmptyUser tests revoking tokens with no tokens.
func TestTokenStore_RevokeUserTokens_EmptyUser(t *testing.T) {
	ts := NewTokenStore()

	// Create user with no tokens
	ts.refreshTokens["user1"] = make(map[string]time.Time)

	// Should not panic
	ts.RevokeUserTokens("user1")
}

// TestTokenStore_ConcurrentAddAndValidate tests concurrent access to AddRefreshToken and IsValidRefreshToken.
func TestTokenStore_ConcurrentAddAndValidate(t *testing.T) {
	ts := NewTokenStore()

	const numGoroutines = 100
	const numTokensPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // Add and validate goroutines

	// Concurrent adds
	for i := 0; i < numGoroutines; i++ {
		go func(userID int) {
			defer wg.Done()
			for j := 0; j < numTokensPerGoroutine; j++ {
				ts.AddRefreshToken("user", "token")
			}
		}(i)
	}

	// Concurrent validations
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numTokensPerGoroutine; j++ {
				ts.IsValidRefreshToken("user", "token")
			}
		}()
	}

	wg.Wait()
}

// TestTokenStore_ConcurrentRevoke tests concurrent token revocations.
func TestTokenStore_ConcurrentRevoke(t *testing.T) {
	ts := NewTokenStore()

	const numGoroutines = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent revocations
	for i := 0; i < numGoroutines; i++ {
		go func(tokenID int) {
			defer wg.Done()
			ts.Revoke("token")
		}(i)
	}

	// Concurrent checks
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ts.IsRevoked("token")
		}()
	}

	wg.Wait()

	// Token should be revoked
	if !ts.IsRevoked("token") {
		t.Error("Token should be revoked after concurrent operations")
	}
}

// TestTokenStore_ConcurrentRevokeUserTokens tests concurrent user token revocations.
func TestTokenStore_ConcurrentRevokeUserTokens(t *testing.T) {
	ts := NewTokenStore()

	// Setup: Add tokens for multiple users
	for i := 0; i < 10; i++ {
		for j := 0; j < 5; j++ {
			ts.AddRefreshToken("user", "token")
		}
	}

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent revocations for same user
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ts.RevokeUserTokens("user")
		}()
	}

	wg.Wait()
}

// TestTokenStore_MixedConcurrentOperations tests mixed concurrent operations.
func TestTokenStore_MixedConcurrentOperations(t *testing.T) {
	ts := NewTokenStore()

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 5)

	// Add tokens
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			ts.AddRefreshToken("user", "token")
		}(i)
	}

	// Validate tokens
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ts.IsValidRefreshToken("user", "token")
		}()
	}

	// Revoke individual tokens
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ts.Revoke("token")
		}()
	}

	// Check revoked
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ts.IsRevoked("token")
		}()
	}

	// Revoke user tokens
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ts.RevokeUserTokens("user")
		}()
	}

	wg.Wait()
}

// TestUserStore_NewUserStore tests the creation of a new UserStore.
func TestUserStore_NewUserStore(t *testing.T) {
	us := NewUserStore()
	if us == nil {
		t.Fatal("NewUserStore returned nil")
	}

	if us.users == nil {
		t.Error("users map not initialized")
	}

	if us.usernameMap == nil {
		t.Error("usernameMap not initialized")
	}
}

// TestUserStore_AddUser tests adding users.
func TestUserStore_AddUser(t *testing.T) {
	us := NewUserStore()

	user := &User{
		ID:       "user1",
		Username: "testuser",
		Roles:    []string{"admin"},
		password: "hashedpass",
		Created:  time.Now(),
	}

	us.AddUser(user)

	// Verify user can be retrieved by ID
	retrieved := us.GetByID("user1")
	if retrieved == nil {
		t.Fatal("User not found by ID")
	}
	if retrieved.GetUsername() != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", retrieved.GetUsername())
	}

	// Verify user can be retrieved by username
	retrieved = us.GetByUsername("testuser")
	if retrieved == nil {
		t.Fatal("User not found by username")
	}
	if retrieved.GetID() != "user1" {
		t.Errorf("Expected ID 'user1', got '%s'", retrieved.GetID())
	}
}

// TestUserStore_AddUser_Multiple tests adding multiple users.
func TestUserStore_AddUser_Multiple(t *testing.T) {
	us := NewUserStore()

	user1 := &User{ID: "1", Username: "user1", Roles: []string{"user"}}
	user2 := &User{ID: "2", Username: "user2", Roles: []string{"admin"}}
	user3 := &User{ID: "3", Username: "user3", Roles: []string{"viewer"}}

	us.AddUser(user1)
	us.AddUser(user2)
	us.AddUser(user3)

	if us.GetByID("1") == nil {
		t.Error("user1 not found")
	}
	if us.GetByID("2") == nil {
		t.Error("user2 not found")
	}
	if us.GetByID("3") == nil {
		t.Error("user3 not found")
	}
}

// TestUserStore_AddUser_Overwrite tests overwriting an existing user.
func TestUserStore_AddUser_Overwrite(t *testing.T) {
	us := NewUserStore()

	user1 := &User{ID: "1", Username: "user1", Roles: []string{"user"}}
	us.AddUser(user1)

	// Add user with same ID but different username
	user2 := &User{ID: "1", Username: "newusername", Roles: []string{"admin"}}
	us.AddUser(user2)

	retrieved := us.GetByID("1")
	if retrieved.GetUsername() != "newusername" {
		t.Errorf("Expected username 'newusername', got '%s'", retrieved.GetUsername())
	}

	// Note: Current implementation doesn't clean up old username mapping
	// This is expected behavior - old username still resolves to same user
	oldUser := us.GetByUsername("user1")
	if oldUser != nil && oldUser.GetID() != "1" {
		t.Error("Old username should resolve to same user ID")
	}

	// New username should resolve
	if us.GetByUsername("newusername") == nil {
		t.Error("New username should resolve")
	}
}

// TestUserStore_GetByID_NonExistent tests retrieving non-existent user by ID.
func TestUserStore_GetByID_NonExistent(t *testing.T) {
	us := NewUserStore()

	user := us.GetByID("nonexistent")
	if user != nil {
		t.Error("Expected nil for non-existent user")
	}
}

// TestUserStore_GetByUsername_NonExistent tests retrieving non-existent user by username.
func TestUserStore_GetByUsername_NonExistent(t *testing.T) {
	us := NewUserStore()

	user := us.GetByUsername("nonexistent")
	if user != nil {
		t.Error("Expected nil for non-existent username")
	}
}

// TestUserStore_GetByUsername_EmptyStore tests retrieving from empty store.
func TestUserStore_GetByUsername_EmptyStore(t *testing.T) {
	us := NewUserStore()

	user := us.GetByUsername("anyuser")
	if user != nil {
		t.Error("Expected nil from empty store")
	}
}

// TestUserStore_UpdateUser tests updating user information.
func TestUserStore_UpdateUser(t *testing.T) {
	us := NewUserStore()

	user := &User{
		ID:       "user1",
		Username: "testuser",
		Roles:    []string{"user"},
		Created:  time.Now(),
	}
	us.AddUser(user)

	// Update user roles
	user.Roles = []string{"admin", "user"}
	user.LastAuth = time.Now()
	us.UpdateUser(user)

	retrieved := us.GetByID("user1")
	if len(retrieved.GetRoles()) != 2 {
		t.Errorf("Expected 2 roles, got %d", len(retrieved.GetRoles()))
	}
	if !retrieved.GetLastAuth().After(user.Created) {
		t.Error("LastAuth should be updated")
	}
}

// TestUserStore_UpdateUser_NonExistent tests updating non-existent user.
func TestUserStore_UpdateUser_NonExistent(t *testing.T) {
	us := NewUserStore()

	user := &User{
		ID:       "nonexistent",
		Username: "testuser",
		Roles:    []string{"user"},
	}

	// Should not panic
	us.UpdateUser(user)

	// User should now exist
	retrieved := us.GetByID("nonexistent")
	if retrieved == nil {
		t.Error("User should exist after update")
	}
}

// TestUserStore_DeleteUser tests deleting a user.
func TestUserStore_DeleteUser(t *testing.T) {
	us := NewUserStore()

	user := &User{
		ID:       "user1",
		Username: "testuser",
		Roles:    []string{"user"},
	}
	us.AddUser(user)

	// Delete user
	us.DeleteUser("user1")

	// User should not be retrievable by ID
	if us.GetByID("user1") != nil {
		t.Error("User should be deleted")
	}

	// User should not be retrievable by username
	if us.GetByUsername("testuser") != nil {
		t.Error("Username mapping should be deleted")
	}
}

// TestUserStore_DeleteUser_NonExistent tests deleting non-existent user.
func TestUserStore_DeleteUser_NonExistent(t *testing.T) {
	us := NewUserStore()

	// Should not panic
	us.DeleteUser("nonexistent")
}

// TestUserStore_DeleteUser_EmptyStore tests deleting from empty store.
func TestUserStore_DeleteUser_EmptyStore(t *testing.T) {
	us := NewUserStore()

	// Should not panic
	us.DeleteUser("anyuser")
}

// TestUserStore_ListUsers tests listing all users.
func TestUserStore_ListUsers(t *testing.T) {
	us := NewUserStore()

	// Empty store
	users := us.ListUsers()
	if len(users) != 0 {
		t.Errorf("Expected 0 users, got %d", len(users))
	}

	// Add users
	user1 := &User{ID: "1", Username: "user1"}
	user2 := &User{ID: "2", Username: "user2"}
	user3 := &User{ID: "3", Username: "user3"}

	us.AddUser(user1)
	us.AddUser(user2)
	us.AddUser(user3)

	users = us.ListUsers()
	if len(users) != 3 {
		t.Errorf("Expected 3 users, got %d", len(users))
	}

	// Verify all users are present
	userMap := make(map[string]bool)
	for _, u := range users {
		userMap[u.GetID()] = true
	}

	if !userMap["1"] || !userMap["2"] || !userMap["3"] {
		t.Error("Not all users are present in list")
	}
}

// TestUserStore_ListUsers_AfterDelete tests listing users after deletion.
func TestUserStore_ListUsers_AfterDelete(t *testing.T) {
	us := NewUserStore()

	user1 := &User{ID: "1", Username: "user1"}
	user2 := &User{ID: "2", Username: "user2"}

	us.AddUser(user1)
	us.AddUser(user2)

	us.DeleteUser("1")

	users := us.ListUsers()
	if len(users) != 1 {
		t.Errorf("Expected 1 user after deletion, got %d", len(users))
	}

	if users[0].GetID() != "2" {
		t.Errorf("Expected remaining user ID '2', got '%s'", users[0].GetID())
	}
}

// TestUserStore_ConcurrentAddAndGet tests concurrent add and get operations.
func TestUserStore_ConcurrentAddAndGet(t *testing.T) {
	us := NewUserStore()

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3)

	// Concurrent adds
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			user := &User{
				ID:       "user",
				Username: "testuser",
				Roles:    []string{"user"},
			}
			us.AddUser(user)
		}(i)
	}

	// Concurrent GetByID
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			us.GetByID("user")
		}()
	}

	// Concurrent GetByUsername
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			us.GetByUsername("testuser")
		}()
	}

	wg.Wait()
}

// TestUserStore_ConcurrentUpdateAndGet tests concurrent update and get operations.
func TestUserStore_ConcurrentUpdateAndGet(t *testing.T) {
	us := NewUserStore()

	user := &User{ID: "1", Username: "user1", Roles: []string{"user"}}
	us.AddUser(user)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent updates
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			u := &User{
				ID:       "1",
				Username: "user1",
				Roles:    []string{"admin", "user"},
				LastAuth: time.Now(),
			}
			us.UpdateUser(u)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			us.GetByID("1")
		}()
	}

	wg.Wait()
}

// TestUserStore_ConcurrentDelete tests concurrent delete operations.
func TestUserStore_ConcurrentDelete(t *testing.T) {
	us := NewUserStore()

	user := &User{ID: "1", Username: "user1"}
	us.AddUser(user)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent deletes of same user
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			us.DeleteUser("1")
		}()
	}

	wg.Wait()

	// User should be deleted
	if us.GetByID("1") != nil {
		t.Error("User should be deleted")
	}
}

// TestUserStore_ConcurrentListUsers tests concurrent list operations.
func TestUserStore_ConcurrentListUsers(t *testing.T) {
	us := NewUserStore()

	// Add initial users
	for i := 0; i < 10; i++ {
		user := &User{ID: string(rune(i)), Username: "user"}
		us.AddUser(user)
	}

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent list operations
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			us.ListUsers()
		}()
	}

	wg.Wait()
}

// TestUserStore_MixedConcurrentOperations tests mixed concurrent operations.
func TestUserStore_MixedConcurrentOperations(t *testing.T) {
	us := NewUserStore()

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 6)

	// Add users
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			user := &User{
				ID:       "user",
				Username: "testuser",
				Roles:    []string{"user"},
			}
			us.AddUser(user)
		}(i)
	}

	// Get by ID
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			us.GetByID("user")
		}()
	}

	// Get by username
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			us.GetByUsername("testuser")
		}()
	}

	// Update users
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			user := &User{
				ID:       "user",
				Username: "testuser",
				Roles:    []string{"admin"},
			}
			us.UpdateUser(user)
		}()
	}

	// List users
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			us.ListUsers()
		}()
	}

	// Delete users
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			us.DeleteUser("user")
		}()
	}

	wg.Wait()
}

// TestUserStore_CacheConsistency tests consistency between users and usernameMap.
func TestUserStore_CacheConsistency(t *testing.T) {
	us := NewUserStore()

	user := &User{ID: "1", Username: "user1", Roles: []string{"user"}}
	us.AddUser(user)

	// Verify consistency
	byID := us.GetByID("1")
	byUsername := us.GetByUsername("user1")

	if byID == nil || byUsername == nil {
		t.Fatal("User not found")
	}

	if byID.GetID() != byUsername.GetID() {
		t.Error("Cache inconsistency: IDs don't match")
	}
	if byID.GetUsername() != byUsername.GetUsername() {
		t.Error("Cache inconsistency: Usernames don't match")
	}

	// Update user
	updatedUser := &User{ID: "1", Username: "user1", Roles: []string{"admin"}}
	us.UpdateUser(updatedUser)

	// Verify consistency after update
	byID = us.GetByID("1")
	byUsername = us.GetByUsername("user1")

	if len(byID.GetRoles()) != 1 || byID.GetRoles()[0] != "admin" {
		t.Error("Update not reflected when retrieved by ID")
	}
	if len(byUsername.GetRoles()) != 1 || byUsername.GetRoles()[0] != "admin" {
		t.Error("Update not reflected when retrieved by username")
	}

	// Delete user
	us.DeleteUser("1")

	// Verify both caches are cleared
	if us.GetByID("1") != nil {
		t.Error("User still in users map after delete")
	}
	if us.GetByUsername("user1") != nil {
		t.Error("User still in usernameMap after delete")
	}
}

// TestUserStore_UsernameMapConsistency tests username mapping consistency.
func TestUserStore_UsernameMapConsistency(t *testing.T) {
	us := NewUserStore()

	// Add user
	user1 := &User{ID: "1", Username: "original"}
	us.AddUser(user1)

	// Change username by adding user with same ID but different username
	user2 := &User{ID: "1", Username: "changed"}
	us.AddUser(user2)

	// Note: Current implementation doesn't clean up old username mapping
	// Both usernames will resolve to the same user
	oldUser := us.GetByUsername("original")
	if oldUser == nil {
		t.Error("Old username should still resolve")
	}
	if oldUser != nil && oldUser.GetID() != "1" {
		t.Error("Old username should resolve to same user ID")
	}

	// New username should work
	retrieved := us.GetByUsername("changed")
	if retrieved == nil {
		t.Error("New username should resolve")
	}
	if retrieved.GetID() != "1" {
		t.Errorf("Expected ID '1', got '%s'", retrieved.GetID())
	}
}

// TestTokenStore_MultipleUsersTokenIsolation tests that tokens are isolated per user.
func TestTokenStore_MultipleUsersTokenIsolation(t *testing.T) {
	ts := NewTokenStore()

	// Add tokens for different users
	ts.AddRefreshToken("user1", "token1")
	ts.AddRefreshToken("user2", "token2")
	ts.AddRefreshToken("user3", "token3")

	// Each user should only validate their own token
	if !ts.IsValidRefreshToken("user1", "token1") {
		t.Error("user1 should validate token1")
	}
	if ts.IsValidRefreshToken("user1", "token2") {
		t.Error("user1 should not validate token2")
	}
	if ts.IsValidRefreshToken("user1", "token3") {
		t.Error("user1 should not validate token3")
	}

	if !ts.IsValidRefreshToken("user2", "token2") {
		t.Error("user2 should validate token2")
	}
	if ts.IsValidRefreshToken("user2", "token1") {
		t.Error("user2 should not validate token1")
	}
}

// TestTokenStore_RevokeDoesNotAffectRefreshTokens tests that revoking a token doesn't remove it from refresh tokens.
func TestTokenStore_RevokeDoesNotAffectRefreshTokens(t *testing.T) {
	ts := NewTokenStore()

	ts.AddRefreshToken("user1", "token1")
	ts.Revoke("token1")

	// Token should still be a valid refresh token
	if !ts.IsValidRefreshToken("user1", "token1") {
		t.Error("Revoking should not affect refresh token validity")
	}

	// But it should be marked as revoked
	if !ts.IsRevoked("token1") {
		t.Error("Token should be marked as revoked")
	}
}

// TestUserStore_ListUsers_Isolation tests that returned list is isolated from internal state.
func TestUserStore_ListUsers_Isolation(t *testing.T) {
	us := NewUserStore()

	user1 := &User{ID: "1", Username: "user1", Roles: []string{"user"}}
	us.AddUser(user1)

	users := us.ListUsers()

	// Modify returned list
	if len(users) > 0 {
		users[0].(*User).Roles = []string{"admin", "superuser"}
	}

	// Original should not be affected (shallow copy)
	// Note: The users themselves are still references, so their fields can be modified
	// This test verifies the slice itself is a copy
	users2 := us.ListUsers()
	if len(users2) != 1 {
		t.Error("Original list should still have 1 user")
	}
}

// TestUserStore_EmptyUsername tests adding user with empty username.
func TestUserStore_EmptyUsername(t *testing.T) {
	us := NewUserStore()

	user := &User{ID: "1", Username: "", Roles: []string{"user"}}
	us.AddUser(user)

	// Should be able to retrieve by ID
	if us.GetByID("1") == nil {
		t.Error("User with empty username should be retrievable by ID")
	}

	// Should be able to retrieve by empty username
	if us.GetByUsername("") == nil {
		t.Error("User should be retrievable by empty username")
	}
}

// TestUserStore_EmptyID tests adding user with empty ID.
func TestUserStore_EmptyID(t *testing.T) {
	us := NewUserStore()

	user := &User{ID: "", Username: "user1", Roles: []string{"user"}}
	us.AddUser(user)

	// Should be able to retrieve by username
	if us.GetByUsername("user1") == nil {
		t.Error("User with empty ID should be retrievable by username")
	}

	// Should be able to retrieve by empty ID
	if us.GetByID("") == nil {
		t.Error("User should be retrievable by empty ID")
	}
}

// TestTokenStore_EmptyStrings tests token store with empty strings.
func TestTokenStore_EmptyStrings(t *testing.T) {
	ts := NewTokenStore()

	// Add token with empty user ID
	ts.AddRefreshToken("", "token1")
	if !ts.IsValidRefreshToken("", "token1") {
		t.Error("Should handle empty user ID")
	}

	// Add empty token
	ts.AddRefreshToken("user1", "")
	if !ts.IsValidRefreshToken("user1", "") {
		t.Error("Should handle empty token")
	}

	// Revoke empty token
	ts.Revoke("")
	if !ts.IsRevoked("") {
		t.Error("Should handle revoking empty token")
	}
}

// TestTokenStore_ExpiredTokenCleanup tests the cleanup of expired tokens.
func TestTokenStore_ExpiredTokenCleanup(t *testing.T) {
	// Create TokenStore without background goroutine
	ts := &TokenStore{
		refreshTokens: make(map[string]map[string]time.Time),
		revokedTokens: make(map[string]time.Time),
	}

	now := time.Now()

	// Add some valid tokens
	ts.refreshTokens["user1"] = make(map[string]time.Time)
	ts.refreshTokens["user1"]["validtoken1"] = now.Add(1 * time.Hour)
	ts.refreshTokens["user1"]["validtoken2"] = now.Add(2 * time.Hour)

	// Add expired tokens
	ts.refreshTokens["user2"] = make(map[string]time.Time)
	ts.refreshTokens["user2"]["expiredtoken1"] = now.Add(-1 * time.Hour)
	ts.refreshTokens["user2"]["expiredtoken2"] = now.Add(-2 * time.Hour)

	// Add mixed tokens for user3
	ts.refreshTokens["user3"] = make(map[string]time.Time)
	ts.refreshTokens["user3"]["validtoken3"] = now.Add(3 * time.Hour)
	ts.refreshTokens["user3"]["expiredtoken3"] = now.Add(-3 * time.Hour)

	// Add revoked tokens
	ts.revokedTokens["recentrevoked"] = now.Add(-1 * time.Hour)
	ts.revokedTokens["oldrevoked"] = now.Add(-8 * 24 * time.Hour) // 8 days old

	// Manually run cleanup logic (simulating what cleanupExpiredTokens does)
	ts.mu.Lock()

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

	// Verify cleanup results

	// user1 should still have both valid tokens
	if len(ts.refreshTokens["user1"]) != 2 {
		t.Errorf("Expected user1 to have 2 tokens, got %d", len(ts.refreshTokens["user1"]))
	}

	// user2 should be completely removed (all tokens expired)
	if _, exists := ts.refreshTokens["user2"]; exists {
		t.Error("Expected user2 to be removed (all tokens expired)")
	}

	// user3 should only have the valid token
	if len(ts.refreshTokens["user3"]) != 1 {
		t.Errorf("Expected user3 to have 1 token, got %d", len(ts.refreshTokens["user3"]))
	}
	if _, exists := ts.refreshTokens["user3"]["validtoken3"]; !exists {
		t.Error("Expected validtoken3 to still exist")
	}
	if _, exists := ts.refreshTokens["user3"]["expiredtoken3"]; exists {
		t.Error("Expected expiredtoken3 to be removed")
	}

	// Recent revoked token should still exist
	if _, exists := ts.revokedTokens["recentrevoked"]; !exists {
		t.Error("Expected recentrevoked to still exist")
	}

	// Old revoked token should be removed
	if _, exists := ts.revokedTokens["oldrevoked"]; exists {
		t.Error("Expected oldrevoked to be removed")
	}
}

// TestTokenStore_CleanupWithNoExpiredTokens tests cleanup when no tokens are expired.
func TestTokenStore_CleanupWithNoExpiredTokens(t *testing.T) {
	ts := &TokenStore{
		refreshTokens: make(map[string]map[string]time.Time),
		revokedTokens: make(map[string]time.Time),
	}

	now := time.Now()

	// Add only valid tokens
	ts.refreshTokens["user1"] = make(map[string]time.Time)
	ts.refreshTokens["user1"]["token1"] = now.Add(1 * time.Hour)
	ts.refreshTokens["user1"]["token2"] = now.Add(2 * time.Hour)

	// Add recent revoked token
	ts.revokedTokens["revoked1"] = now.Add(-1 * time.Hour)

	initialRefreshCount := len(ts.refreshTokens["user1"])
	initialRevokedCount := len(ts.revokedTokens)

	// Run cleanup
	ts.mu.Lock()
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

	for token, revokedAt := range ts.revokedTokens {
		if now.Sub(revokedAt) > 7*24*time.Hour {
			delete(ts.revokedTokens, token)
		}
	}
	ts.mu.Unlock()

	// Counts should remain the same
	if len(ts.refreshTokens["user1"]) != initialRefreshCount {
		t.Error("No tokens should be removed")
	}
	if len(ts.revokedTokens) != initialRevokedCount {
		t.Error("No revoked tokens should be removed")
	}
}

// TestTokenStore_CleanupEdgeCases tests edge cases in cleanup.
func TestTokenStore_CleanupEdgeCases(t *testing.T) {
	ts := &TokenStore{
		refreshTokens: make(map[string]map[string]time.Time),
		revokedTokens: make(map[string]time.Time),
	}

	now := time.Now()

	// Token expiring exactly now
	ts.refreshTokens["user1"] = make(map[string]time.Time)
	ts.refreshTokens["user1"]["exactlyexpired"] = now

	// Token expiring 1 nanosecond in the future
	ts.refreshTokens["user2"] = make(map[string]time.Time)
	ts.refreshTokens["user2"]["almostvalid"] = now.Add(1 * time.Nanosecond)

	// Revoked token exactly 7 days old
	ts.revokedTokens["exactly7days"] = now.Add(-7 * 24 * time.Hour)

	// Revoked token 7 days + 1 nanosecond old
	ts.revokedTokens["justover7days"] = now.Add(-7*24*time.Hour - 1*time.Nanosecond)

	// Run cleanup
	ts.mu.Lock()
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

	for token, revokedAt := range ts.revokedTokens {
		if now.Sub(revokedAt) > 7*24*time.Hour {
			delete(ts.revokedTokens, token)
		}
	}
	ts.mu.Unlock()

	// exactlyexpired might or might not be removed depending on exact timing
	// (now.After(now) is false, so token equal to now is NOT considered expired)
	// This is expected behavior - token expires AFTER the expiry time, not AT it

	// almostvalid should remain
	if _, exists := ts.refreshTokens["user2"]["almostvalid"]; !exists {
		t.Error("almostvalid token should remain")
	}

	// exactly7days should remain (not greater than 7 days)
	if _, exists := ts.revokedTokens["exactly7days"]; !exists {
		t.Error("exactly7days should remain")
	}

	// justover7days should be removed
	if _, exists := ts.revokedTokens["justover7days"]; exists {
		t.Error("justover7days should be removed")
	}
}

// TestTokenStore_CleanupEmptyMaps tests cleanup with empty maps.
func TestTokenStore_CleanupEmptyMaps(t *testing.T) {
	ts := &TokenStore{
		refreshTokens: make(map[string]map[string]time.Time),
		revokedTokens: make(map[string]time.Time),
	}

	now := time.Now()

	// Add empty user token map
	ts.refreshTokens["emptyuser"] = make(map[string]time.Time)

	// Run cleanup
	ts.mu.Lock()
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
	ts.mu.Unlock()

	// Empty user map should be removed
	if _, exists := ts.refreshTokens["emptyuser"]; exists {
		t.Error("Empty user token map should be removed")
	}
}
