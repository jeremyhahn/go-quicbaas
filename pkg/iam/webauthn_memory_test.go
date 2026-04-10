//go:build iam

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

package iam

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCredentialStorage_CreateCredential(t *testing.T) {
	storage := NewMemoryCredentialStorage()
	ctx := context.Background()

	cred := &WebAuthnCredential{
		ID:        "cred123",
		UserID:    "user123",
		TenantID:  "tenant123",
		Name:      "My Authenticator",
		PublicKey: []byte("publickey"),
		CreatedAt: time.Now(),
	}

	// Test successful creation
	err := storage.CreateCredential(ctx, cred)
	if err != nil {
		t.Fatalf("CreateCredential() error = %v, want nil", err)
	}

	// Test duplicate creation
	err = storage.CreateCredential(ctx, cred)
	if err == nil {
		t.Error("CreateCredential() with duplicate should return error")
	}
}

func TestMemoryCredentialStorage_GetCredential(t *testing.T) {
	storage := NewMemoryCredentialStorage()
	ctx := context.Background()

	cred := &WebAuthnCredential{
		ID:        "cred123",
		UserID:    "user123",
		TenantID:  "tenant123",
		Name:      "My Authenticator",
		PublicKey: []byte("publickey"),
		CreatedAt: time.Now(),
	}

	// Store credential
	if err := storage.CreateCredential(ctx, cred); err != nil {
		t.Fatalf("CreateCredential() error = %v", err)
	}

	// Test successful retrieval
	retrieved, err := storage.GetCredential(ctx, "tenant123", "cred123")
	if err != nil {
		t.Fatalf("GetCredential() error = %v, want nil", err)
	}

	if retrieved.ID != cred.ID {
		t.Errorf("GetCredential() ID = %s, want %s", retrieved.ID, cred.ID)
	}
	if retrieved.UserID != cred.UserID {
		t.Errorf("GetCredential() UserID = %s, want %s", retrieved.UserID, cred.UserID)
	}

	// Test not found
	_, err = storage.GetCredential(ctx, "tenant123", "nonexistent")
	if err == nil {
		t.Error("GetCredential() with non-existent ID should return error")
	}
}

func TestMemoryCredentialStorage_GetCredentialsByUser(t *testing.T) {
	storage := NewMemoryCredentialStorage()
	ctx := context.Background()

	// Create multiple credentials for same user
	cred1 := &WebAuthnCredential{
		ID:        "cred1",
		UserID:    "user123",
		TenantID:  "tenant123",
		PublicKey: []byte("pk1"),
	}
	cred2 := &WebAuthnCredential{
		ID:        "cred2",
		UserID:    "user123",
		TenantID:  "tenant123",
		PublicKey: []byte("pk2"),
	}
	cred3 := &WebAuthnCredential{
		ID:        "cred3",
		UserID:    "user456",
		TenantID:  "tenant123",
		PublicKey: []byte("pk3"),
	}

	storage.CreateCredential(ctx, cred1)
	storage.CreateCredential(ctx, cred2)
	storage.CreateCredential(ctx, cred3)

	// Get credentials for user123
	creds, err := storage.GetCredentialsByUser(ctx, "tenant123", "user123")
	if err != nil {
		t.Fatalf("GetCredentialsByUser() error = %v", err)
	}

	if len(creds) != 2 {
		t.Errorf("GetCredentialsByUser() returned %d credentials, want 2", len(creds))
	}

	// Get credentials for user with no credentials
	creds, err = storage.GetCredentialsByUser(ctx, "tenant123", "nonexistent")
	if err != nil {
		t.Fatalf("GetCredentialsByUser() error = %v", err)
	}

	if len(creds) != 0 {
		t.Errorf("GetCredentialsByUser() returned %d credentials, want 0", len(creds))
	}
}

func TestMemoryCredentialStorage_UpdateCredential(t *testing.T) {
	storage := NewMemoryCredentialStorage()
	ctx := context.Background()

	cred := &WebAuthnCredential{
		ID:        "cred123",
		UserID:    "user123",
		TenantID:  "tenant123",
		Name:      "Original Name",
		PublicKey: []byte("publickey"),
		SignCount: 0,
	}

	storage.CreateCredential(ctx, cred)

	// Update credential
	cred.Name = "Updated Name"
	cred.SignCount = 5

	err := storage.UpdateCredential(ctx, cred)
	if err != nil {
		t.Fatalf("UpdateCredential() error = %v", err)
	}

	// Verify update
	updated, _ := storage.GetCredential(ctx, "tenant123", "cred123")
	if updated.Name != "Updated Name" {
		t.Errorf("UpdateCredential() Name = %s, want 'Updated Name'", updated.Name)
	}
	if updated.SignCount != 5 {
		t.Errorf("UpdateCredential() SignCount = %d, want 5", updated.SignCount)
	}

	// Test update non-existent
	nonExistent := &WebAuthnCredential{
		ID:        "nonexistent",
		UserID:    "user123",
		TenantID:  "tenant123",
		PublicKey: []byte("pk"),
	}

	err = storage.UpdateCredential(ctx, nonExistent)
	if err == nil {
		t.Error("UpdateCredential() with non-existent ID should return error")
	}
}

func TestMemoryCredentialStorage_DeleteCredential(t *testing.T) {
	storage := NewMemoryCredentialStorage()
	ctx := context.Background()

	cred := &WebAuthnCredential{
		ID:        "cred123",
		UserID:    "user123",
		TenantID:  "tenant123",
		PublicKey: []byte("publickey"),
	}

	storage.CreateCredential(ctx, cred)

	// Delete credential
	err := storage.DeleteCredential(ctx, "tenant123", "cred123")
	if err != nil {
		t.Fatalf("DeleteCredential() error = %v", err)
	}

	// Verify deletion
	_, err = storage.GetCredential(ctx, "tenant123", "cred123")
	if err == nil {
		t.Error("GetCredential() after delete should return error")
	}

	// Verify removed from user index
	creds, _ := storage.GetCredentialsByUser(ctx, "tenant123", "user123")
	if len(creds) != 0 {
		t.Errorf("GetCredentialsByUser() after delete returned %d credentials, want 0", len(creds))
	}

	// Test delete non-existent
	err = storage.DeleteCredential(ctx, "tenant123", "nonexistent")
	if err == nil {
		t.Error("DeleteCredential() with non-existent ID should return error")
	}
}

func TestMemoryCredentialStorage_Challenges(t *testing.T) {
	storage := NewMemoryCredentialStorage()
	ctx := context.Background()

	challenge := &WebAuthnChallenge{
		ID:        "challenge123",
		UserID:    "user123",
		TenantID:  "tenant123",
		Challenge: []byte("challenge"),
		Type:      ChallengeTypeRegistration,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Minute),
	}

	// Store challenge
	err := storage.StoreChallenge(ctx, challenge)
	if err != nil {
		t.Fatalf("StoreChallenge() error = %v", err)
	}

	// Get challenge
	retrieved, err := storage.GetChallenge(ctx, "challenge123")
	if err != nil {
		t.Fatalf("GetChallenge() error = %v", err)
	}

	if retrieved.ID != challenge.ID {
		t.Errorf("GetChallenge() ID = %s, want %s", retrieved.ID, challenge.ID)
	}

	// Delete challenge
	err = storage.DeleteChallenge(ctx, "challenge123")
	if err != nil {
		t.Fatalf("DeleteChallenge() error = %v", err)
	}

	// Verify deletion
	_, err = storage.GetChallenge(ctx, "challenge123")
	if err == nil {
		t.Error("GetChallenge() after delete should return error")
	}
}

func TestMemoryCredentialStorage_ExpiredChallenge(t *testing.T) {
	storage := NewMemoryCredentialStorage()
	ctx := context.Background()

	challenge := &WebAuthnChallenge{
		ID:        "challenge123",
		UserID:    "user123",
		TenantID:  "tenant123",
		Challenge: []byte("challenge"),
		Type:      ChallengeTypeRegistration,
		CreatedAt: time.Now().Add(-2 * time.Minute),
		ExpiresAt: time.Now().Add(-1 * time.Minute), // Expired
	}

	storage.StoreChallenge(ctx, challenge)

	// Get expired challenge should return error
	_, err := storage.GetChallenge(ctx, "challenge123")
	if err == nil {
		t.Error("GetChallenge() with expired challenge should return error")
	}
}

func TestMemoryCredentialStorage_CleanupExpiredChallenges(t *testing.T) {
	storage := NewMemoryCredentialStorage()
	ctx := context.Background()

	// Create expired challenge
	expiredChallenge := &WebAuthnChallenge{
		ID:        "expired",
		UserID:    "user123",
		TenantID:  "tenant123",
		Challenge: []byte("challenge"),
		Type:      ChallengeTypeRegistration,
		CreatedAt: time.Now().Add(-2 * time.Minute),
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	}

	// Create valid challenge
	validChallenge := &WebAuthnChallenge{
		ID:        "valid",
		UserID:    "user123",
		TenantID:  "tenant123",
		Challenge: []byte("challenge"),
		Type:      ChallengeTypeRegistration,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	storage.StoreChallenge(ctx, expiredChallenge)
	storage.StoreChallenge(ctx, validChallenge)

	// Cleanup expired challenges
	count := storage.CleanupExpiredChallenges(ctx)
	if count != 1 {
		t.Errorf("CleanupExpiredChallenges() removed %d challenges, want 1", count)
	}

	// Verify valid challenge still exists
	_, err := storage.GetChallenge(ctx, "valid")
	if err != nil {
		t.Error("GetChallenge() for valid challenge should not return error after cleanup")
	}
}

func TestMemorySessionStorage_CreateSession(t *testing.T) {
	storage := NewMemorySessionStorage()
	ctx := context.Background()

	session := &Session{
		ID:       "session123",
		UserID:   "user123",
		TenantID: "tenant123",
		Created:  time.Now(),
		Expires:  time.Now().Add(1 * time.Hour),
	}

	// Test successful creation
	err := storage.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("CreateSession() error = %v, want nil", err)
	}

	// Test duplicate creation
	err = storage.CreateSession(ctx, session)
	if err == nil {
		t.Error("CreateSession() with duplicate should return error")
	}
}

func TestMemorySessionStorage_GetSession(t *testing.T) {
	storage := NewMemorySessionStorage()
	ctx := context.Background()

	session := &Session{
		ID:       "session123",
		UserID:   "user123",
		TenantID: "tenant123",
		Created:  time.Now(),
		Expires:  time.Now().Add(1 * time.Hour),
	}

	storage.CreateSession(ctx, session)

	// Test successful retrieval
	retrieved, err := storage.GetSession(ctx, "session123")
	if err != nil {
		t.Fatalf("GetSession() error = %v, want nil", err)
	}

	if retrieved.ID != session.ID {
		t.Errorf("GetSession() ID = %s, want %s", retrieved.ID, session.ID)
	}

	// Test not found
	_, err = storage.GetSession(ctx, "nonexistent")
	if err == nil {
		t.Error("GetSession() with non-existent ID should return error")
	}
}

func TestMemorySessionStorage_GetSessionsByUser(t *testing.T) {
	storage := NewMemorySessionStorage()
	ctx := context.Background()

	// Create multiple sessions for same user
	session1 := &Session{
		ID:       "session1",
		UserID:   "user123",
		TenantID: "tenant123",
		Created:  time.Now(),
		Expires:  time.Now().Add(1 * time.Hour),
	}
	session2 := &Session{
		ID:       "session2",
		UserID:   "user123",
		TenantID: "tenant123",
		Created:  time.Now(),
		Expires:  time.Now().Add(1 * time.Hour),
	}
	expiredSession := &Session{
		ID:          "session3",
		UserID:      "user123",
		TenantID:    "tenant123",
		Created:     time.Now().Add(-2 * time.Hour),
		Expires:     time.Now().Add(-1 * time.Hour), // Expired
		IdleTimeout: 0,
	}

	storage.CreateSession(ctx, session1)
	storage.CreateSession(ctx, session2)
	storage.CreateSession(ctx, expiredSession)

	// Get sessions for user123 (should only return active sessions)
	sessions, err := storage.GetSessionsByUser(ctx, "tenant123", "user123")
	if err != nil {
		t.Fatalf("GetSessionsByUser() error = %v", err)
	}

	if len(sessions) != 2 {
		t.Errorf("GetSessionsByUser() returned %d sessions, want 2 (expired session should be filtered)", len(sessions))
	}
}

func TestMemorySessionStorage_UpdateSession(t *testing.T) {
	storage := NewMemorySessionStorage()
	ctx := context.Background()

	session := &Session{
		ID:           "session123",
		UserID:       "user123",
		TenantID:     "tenant123",
		Created:      time.Now(),
		Expires:      time.Now().Add(1 * time.Hour),
		LastActivity: time.Now(),
	}

	storage.CreateSession(ctx, session)

	// Update session
	newActivity := time.Now().Add(10 * time.Minute)
	session.LastActivity = newActivity

	err := storage.UpdateSession(ctx, session)
	if err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	// Verify update
	updated, _ := storage.GetSession(ctx, "session123")
	if !updated.LastActivity.Equal(newActivity) {
		t.Errorf("UpdateSession() LastActivity = %v, want %v", updated.LastActivity, newActivity)
	}
}

func TestMemorySessionStorage_DeleteSession(t *testing.T) {
	storage := NewMemorySessionStorage()
	ctx := context.Background()

	session := &Session{
		ID:       "session123",
		UserID:   "user123",
		TenantID: "tenant123",
		Created:  time.Now(),
		Expires:  time.Now().Add(1 * time.Hour),
	}

	storage.CreateSession(ctx, session)

	// Delete session
	err := storage.DeleteSession(ctx, "session123")
	if err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	// Verify deletion
	_, err = storage.GetSession(ctx, "session123")
	if err == nil {
		t.Error("GetSession() after delete should return error")
	}
}

func TestMemorySessionStorage_DeleteExpiredSessions(t *testing.T) {
	storage := NewMemorySessionStorage()
	ctx := context.Background()

	// Create expired session
	expiredSession := &Session{
		ID:       "expired",
		UserID:   "user123",
		TenantID: "tenant123",
		Created:  time.Now().Add(-2 * time.Hour),
		Expires:  time.Now().Add(-1 * time.Hour),
	}

	// Create valid session
	validSession := &Session{
		ID:       "valid",
		UserID:   "user123",
		TenantID: "tenant123",
		Created:  time.Now(),
		Expires:  time.Now().Add(1 * time.Hour),
	}

	storage.CreateSession(ctx, expiredSession)
	storage.CreateSession(ctx, validSession)

	// Delete expired sessions
	count, err := storage.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions() error = %v", err)
	}

	if count != 1 {
		t.Errorf("DeleteExpiredSessions() removed %d sessions, want 1", count)
	}

	// Verify valid session still exists
	_, err = storage.GetSession(ctx, "valid")
	if err != nil {
		t.Error("GetSession() for valid session should not return error after cleanup")
	}

	// Verify expired session was removed
	_, err = storage.GetSession(ctx, "expired")
	if err == nil {
		t.Error("GetSession() for expired session should return error after cleanup")
	}
}
