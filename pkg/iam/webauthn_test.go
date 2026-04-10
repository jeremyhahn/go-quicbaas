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
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Mock WebAuthnIdentityStore for testing WebAuthn
type mockWebAuthnIdentityStore struct {
	users map[string]*User // key: userID
}

func newMockWebAuthnIdentityStore() *mockWebAuthnIdentityStore {
	return &mockWebAuthnIdentityStore{
		users: make(map[string]*User),
	}
}

func (m *mockWebAuthnIdentityStore) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	user, exists := m.users[userID]
	if !exists {
		return nil, ErrUserNotFound
	}
	if user.TenantID != tenantID {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (m *mockWebAuthnIdentityStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	for _, user := range m.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, ErrUserNotFound
}

func (m *mockWebAuthnIdentityStore) addUser(user *User) {
	m.users[user.ID] = user
}

func TestNewWebAuthnManager(t *testing.T) {
	tests := []struct {
		name    string
		config  *WebAuthnConfig
		deps    WebAuthnDependencies
		wantErr bool
	}{
		{
			name: "valid configuration",
			config: &WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example Corp",
				RPOrigins: []string{"https://example.com"},
			},
			deps: WebAuthnDependencies{
				CredentialStorage: NewMemoryCredentialStorage(),
				IdentityStore:     &IdentityStore{},
				SessionStorage:    NewMemorySessionStorage(),
			},
			wantErr: false,
		},
		{
			name:   "nil config",
			config: nil,
			deps: WebAuthnDependencies{
				CredentialStorage: NewMemoryCredentialStorage(),
				IdentityStore:     &IdentityStore{},
				SessionStorage:    NewMemorySessionStorage(),
			},
			wantErr: true,
		},
		{
			name: "missing RPID",
			config: &WebAuthnConfig{
				RPName:    "Example Corp",
				RPOrigins: []string{"https://example.com"},
			},
			deps: WebAuthnDependencies{
				CredentialStorage: NewMemoryCredentialStorage(),
				IdentityStore:     &IdentityStore{},
				SessionStorage:    NewMemorySessionStorage(),
			},
			wantErr: true,
		},
		{
			name: "missing RPName",
			config: &WebAuthnConfig{
				RPID:      "example.com",
				RPOrigins: []string{"https://example.com"},
			},
			deps: WebAuthnDependencies{
				CredentialStorage: NewMemoryCredentialStorage(),
				IdentityStore:     &IdentityStore{},
				SessionStorage:    NewMemorySessionStorage(),
			},
			wantErr: true,
		},
		{
			name: "missing RPOrigins",
			config: &WebAuthnConfig{
				RPID:   "example.com",
				RPName: "Example Corp",
			},
			deps: WebAuthnDependencies{
				CredentialStorage: NewMemoryCredentialStorage(),
				IdentityStore:     &IdentityStore{},
				SessionStorage:    NewMemorySessionStorage(),
			},
			wantErr: true,
		},
		{
			name: "missing credential storage",
			config: &WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example Corp",
				RPOrigins: []string{"https://example.com"},
			},
			deps: WebAuthnDependencies{
				IdentityStore:  &IdentityStore{},
				SessionStorage: NewMemorySessionStorage(),
			},
			wantErr: true,
		},
		{
			name: "missing identity store",
			config: &WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example Corp",
				RPOrigins: []string{"https://example.com"},
			},
			deps: WebAuthnDependencies{
				CredentialStorage: NewMemoryCredentialStorage(),
				SessionStorage:    NewMemorySessionStorage(),
			},
			wantErr: true,
		},
		{
			name: "missing session storage",
			config: &WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example Corp",
				RPOrigins: []string{"https://example.com"},
			},
			deps: WebAuthnDependencies{
				CredentialStorage: NewMemoryCredentialStorage(),
				IdentityStore:     &IdentityStore{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewWebAuthnManager(tt.config, tt.deps)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewWebAuthnManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && manager == nil {
				t.Error("NewWebAuthnManager() returned nil manager when error was not expected")
			}
		})
	}
}

func TestNewWebAuthnManager_Defaults(t *testing.T) {
	config := &WebAuthnConfig{
		RPID:      "example.com",
		RPName:    "Example Corp",
		RPOrigins: []string{"https://example.com"},
		// Attestation, UserVerification, and Timeout not set
	}

	deps := WebAuthnDependencies{
		CredentialStorage: NewMemoryCredentialStorage(),
		IdentityStore:     &IdentityStore{},
		SessionStorage:    NewMemorySessionStorage(),
	}

	manager, err := NewWebAuthnManager(config, deps)
	if err != nil {
		t.Fatalf("NewWebAuthnManager() error = %v", err)
	}

	// Check defaults
	if manager.config.Attestation != "none" {
		t.Errorf("Default Attestation = %s, want 'none'", manager.config.Attestation)
	}
	if manager.config.UserVerification != "preferred" {
		t.Errorf("Default UserVerification = %s, want 'preferred'", manager.config.UserVerification)
	}
	if manager.config.Timeout != 60*time.Second {
		t.Errorf("Default Timeout = %v, want 60s", manager.config.Timeout)
	}
}

func TestWebAuthnManager_ListCredentials(t *testing.T) {
	ctx := context.Background()

	// Create mock dependencies
	credStorage := NewMemoryCredentialStorage()
	identityStore := newMockWebAuthnIdentityStore()
	sessionStorage := NewMemorySessionStorage()

	// Add test user
	user := &User{
		ID:       "user123",
		Email:    "test@example.com",
		Name:     "Test User",
		TenantID: "tenant123",
	}
	identityStore.addUser(user)

	// Create manager
	manager, err := NewWebAuthnManager(
		&WebAuthnConfig{
			RPID:      "example.com",
			RPName:    "Example",
			RPOrigins: []string{"https://example.com"},
		},
		WebAuthnDependencies{
			CredentialStorage: credStorage,
			IdentityStore:     identityStore,
			SessionStorage:    sessionStorage,
		},
	)
	if err != nil {
		t.Fatalf("NewWebAuthnManager() error = %v", err)
	}

	// Add test credentials
	cred1 := &WebAuthnCredential{
		ID:        "cred1",
		UserID:    "user123",
		TenantID:  "tenant123",
		Name:      "Credential 1",
		PublicKey: []byte("pk1"),
	}
	cred2 := &WebAuthnCredential{
		ID:        "cred2",
		UserID:    "user123",
		TenantID:  "tenant123",
		Name:      "Credential 2",
		PublicKey: []byte("pk2"),
	}

	credStorage.CreateCredential(ctx, cred1)
	credStorage.CreateCredential(ctx, cred2)

	// List credentials
	credentials, err := manager.ListCredentials(ctx, "tenant123", "user123")
	if err != nil {
		t.Fatalf("ListCredentials() error = %v", err)
	}

	if len(credentials) != 2 {
		t.Errorf("ListCredentials() returned %d credentials, want 2", len(credentials))
	}

	// Test with non-existent user
	_, err = manager.ListCredentials(ctx, "tenant123", "nonexistent")
	if err == nil {
		t.Error("ListCredentials() with non-existent user should return error")
	}
}

func TestWebAuthnManager_DeleteCredential(t *testing.T) {
	ctx := context.Background()

	// Create mock dependencies
	credStorage := NewMemoryCredentialStorage()
	identityStore := newMockWebAuthnIdentityStore()
	sessionStorage := NewMemorySessionStorage()

	// Add test user
	user := &User{
		ID:       "user123",
		Email:    "test@example.com",
		TenantID: "tenant123",
	}
	identityStore.addUser(user)

	// Create manager
	manager, err := NewWebAuthnManager(
		&WebAuthnConfig{
			RPID:      "example.com",
			RPName:    "Example",
			RPOrigins: []string{"https://example.com"},
		},
		WebAuthnDependencies{
			CredentialStorage: credStorage,
			IdentityStore:     identityStore,
			SessionStorage:    sessionStorage,
		},
	)
	if err != nil {
		t.Fatalf("NewWebAuthnManager() error = %v", err)
	}

	// Add test credential
	cred := &WebAuthnCredential{
		ID:        "cred123",
		UserID:    "user123",
		TenantID:  "tenant123",
		PublicKey: []byte("pk"),
	}
	credStorage.CreateCredential(ctx, cred)

	// Delete credential
	err = manager.DeleteCredential(ctx, "tenant123", "user123", "cred123")
	if err != nil {
		t.Fatalf("DeleteCredential() error = %v", err)
	}

	// Verify deletion
	_, err = credStorage.GetCredential(ctx, "tenant123", "cred123")
	if err == nil {
		t.Error("GetCredential() after delete should return error")
	}

	// Test delete non-existent credential
	err = manager.DeleteCredential(ctx, "tenant123", "user123", "nonexistent")
	if err == nil {
		t.Error("DeleteCredential() with non-existent credential should return error")
	}

	// Test delete credential belonging to different user
	otherUser := &User{
		ID:       "user456",
		Email:    "other@example.com",
		TenantID: "tenant123",
	}
	identityStore.addUser(otherUser)

	otherCred := &WebAuthnCredential{
		ID:        "othercred",
		UserID:    "user456",
		TenantID:  "tenant123",
		PublicKey: []byte("pk"),
	}
	credStorage.CreateCredential(ctx, otherCred)

	// Try to delete other user's credential
	err = manager.DeleteCredential(ctx, "tenant123", "user123", "othercred")
	if err == nil {
		t.Error("DeleteCredential() should return error when trying to delete another user's credential")
	}
}

func TestWebAuthnManager_RenameCredential(t *testing.T) {
	ctx := context.Background()

	// Create mock dependencies
	credStorage := NewMemoryCredentialStorage()
	identityStore := newMockWebAuthnIdentityStore()
	sessionStorage := NewMemorySessionStorage()

	// Add test user
	user := &User{
		ID:       "user123",
		Email:    "test@example.com",
		TenantID: "tenant123",
	}
	identityStore.addUser(user)

	// Create manager
	manager, err := NewWebAuthnManager(
		&WebAuthnConfig{
			RPID:      "example.com",
			RPName:    "Example",
			RPOrigins: []string{"https://example.com"},
		},
		WebAuthnDependencies{
			CredentialStorage: credStorage,
			IdentityStore:     identityStore,
			SessionStorage:    sessionStorage,
		},
	)
	if err != nil {
		t.Fatalf("NewWebAuthnManager() error = %v", err)
	}

	// Add test credential
	cred := &WebAuthnCredential{
		ID:        "cred123",
		UserID:    "user123",
		TenantID:  "tenant123",
		Name:      "Original Name",
		PublicKey: []byte("pk"),
	}
	credStorage.CreateCredential(ctx, cred)

	// Rename credential
	err = manager.RenameCredential(ctx, "tenant123", "user123", "cred123", "New Name")
	if err != nil {
		t.Fatalf("RenameCredential() error = %v", err)
	}

	// Verify rename
	updated, _ := credStorage.GetCredential(ctx, "tenant123", "cred123")
	if updated.Name != "New Name" {
		t.Errorf("RenameCredential() Name = %s, want 'New Name'", updated.Name)
	}

	// Test rename non-existent credential
	err = manager.RenameCredential(ctx, "tenant123", "user123", "nonexistent", "New Name")
	if err == nil {
		t.Error("RenameCredential() with non-existent credential should return error")
	}

	// Test rename credential belonging to different user
	otherUser := &User{
		ID:       "user456",
		Email:    "other@example.com",
		TenantID: "tenant123",
	}
	identityStore.addUser(otherUser)

	otherCred := &WebAuthnCredential{
		ID:        "othercred",
		UserID:    "user456",
		TenantID:  "tenant123",
		Name:      "Other Cred",
		PublicKey: []byte("pk"),
	}
	credStorage.CreateCredential(ctx, otherCred)

	// Try to rename other user's credential
	err = manager.RenameCredential(ctx, "tenant123", "user123", "othercred", "Hacked Name")
	if err == nil {
		t.Error("RenameCredential() should return error when trying to rename another user's credential")
	}
}

func TestWebAuthnManager_BeginRegistration(t *testing.T) {
	ctx := context.Background()

	t.Run("successful registration start", func(t *testing.T) {
		// Create mock dependencies
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		// Add test user
		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			Name:     "Test User",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		// Create manager
		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
				Timeout:   60 * time.Second,
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		// Begin registration
		creation, err := manager.BeginRegistration(ctx, "tenant123", "user123")
		if err != nil {
			t.Fatalf("BeginRegistration() error = %v", err)
		}

		if creation == nil {
			t.Fatal("BeginRegistration() returned nil creation options")
		}

		// Verify challenge was stored - the session.Challenge is base64url encoded,
		// and stored as []byte(session.Challenge), so we need to match the encoding
		challengeValue := base64.RawURLEncoding.EncodeToString(creation.Response.Challenge)
		challenge, err := credStorage.GetChallengeByValue(ctx, challengeValue)
		if err != nil {
			t.Errorf("Challenge not stored properly: %v", err)
		}

		if challenge.UserID != "user123" {
			t.Errorf("Challenge UserID = %s, want user123", challenge.UserID)
		}
		if challenge.TenantID != "tenant123" {
			t.Errorf("Challenge TenantID = %s, want tenant123", challenge.TenantID)
		}
		if challenge.Type != ChallengeTypeRegistration {
			t.Errorf("Challenge Type = %s, want %s", challenge.Type, ChallengeTypeRegistration)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		_, err = manager.BeginRegistration(ctx, "tenant123", "nonexistent")
		if err == nil {
			t.Error("BeginRegistration() with non-existent user should return error")
		}
	})

	t.Run("with existing credentials", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			Name:     "Test User",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		// Add existing credential
		existingCred := &WebAuthnCredential{
			ID:        "existing-cred",
			UserID:    "user123",
			TenantID:  "tenant123",
			PublicKey: []byte("existing-pk"),
		}
		credStorage.CreateCredential(ctx, existingCred)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		creation, err := manager.BeginRegistration(ctx, "tenant123", "user123")
		if err != nil {
			t.Fatalf("BeginRegistration() error = %v", err)
		}

		if creation == nil {
			t.Fatal("BeginRegistration() returned nil creation options")
		}

		// Should succeed even with existing credentials
	})
}

func TestWebAuthnManager_FinishRegistration(t *testing.T) {
	ctx := context.Background()

	t.Run("expired challenge", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			Name:     "Test User",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		// Create expired challenge
		challengeBytes := []byte("test-challenge-bytes-32chars!!")
		challengeStr := base64.RawURLEncoding.EncodeToString(challengeBytes)

		sessionData := &webauthn.SessionData{
			Challenge: challengeStr,
			UserID:    []byte("user123"),
		}
		sessionBytes, _ := json.Marshal(sessionData)

		expiredChallenge := &WebAuthnChallenge{
			ID:          challengeStr,
			UserID:      "user123",
			TenantID:    "tenant123",
			Challenge:   challengeBytes,
			Type:        ChallengeTypeRegistration,
			SessionData: sessionBytes,
			CreatedAt:   time.Now().Add(-2 * time.Hour),
			ExpiresAt:   time.Now().Add(-1 * time.Hour), // Expired
		}
		credStorage.StoreChallenge(ctx, expiredChallenge)

		// Create mock response with proper structure
		response := &protocol.ParsedCredentialCreationData{
			Response: protocol.ParsedAttestationResponse{
				CollectedClientData: protocol.CollectedClientData{
					Challenge: challengeStr,
					Origin:    "https://example.com",
					Type:      "webauthn.create",
				},
			},
		}

		_, err = manager.FinishRegistration(ctx, "tenant123", "user123", response)
		if err == nil {
			t.Error("FinishRegistration() with expired challenge should return error")
		}
	})

	t.Run("challenge not found", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		response := &protocol.ParsedCredentialCreationData{
			Response: protocol.ParsedAttestationResponse{
				CollectedClientData: protocol.CollectedClientData{
					Challenge: "nonexistent",
					Origin:    "https://example.com",
					Type:      "webauthn.create",
				},
			},
		}

		_, err = manager.FinishRegistration(ctx, "tenant123", "user123", response)
		if err == nil {
			t.Error("FinishRegistration() with non-existent challenge should return error")
		}
	})

	t.Run("user not found", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		response := &protocol.ParsedCredentialCreationData{}
		_, err = manager.FinishRegistration(ctx, "tenant123", "nonexistent", response)
		if err == nil {
			t.Error("FinishRegistration() with non-existent user should return error")
		}
	})
}

func TestWebAuthnManager_BeginLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("successful login start", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			Name:     "Test User",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		// Add credential for user
		cred := &WebAuthnCredential{
			ID:        "cred123",
			UserID:    "user123",
			TenantID:  "tenant123",
			PublicKey: []byte("pk"),
		}
		credStorage.CreateCredential(ctx, cred)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		assertion, err := manager.BeginLogin(ctx, "tenant123", "test@example.com")
		if err != nil {
			t.Fatalf("BeginLogin() error = %v", err)
		}

		if assertion == nil {
			t.Fatal("BeginLogin() returned nil assertion")
		}

		// Verify challenge was stored - the session.Challenge is base64url encoded
		challengeStr := base64.RawURLEncoding.EncodeToString(assertion.Response.Challenge)
		challenge, err := credStorage.GetChallengeByValue(ctx, challengeStr)
		if err != nil {
			t.Errorf("Challenge not stored properly: %v", err)
		}

		if challenge.Type != ChallengeTypeAuthentication {
			t.Errorf("Challenge Type = %s, want %s", challenge.Type, ChallengeTypeAuthentication)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		_, err = manager.BeginLogin(ctx, "tenant123", "nonexistent@example.com")
		if err == nil {
			t.Error("BeginLogin() with non-existent user should return error")
		}
	})

	t.Run("wrong tenant", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant-different",
		}
		identityStore.addUser(user)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		_, err = manager.BeginLogin(ctx, "tenant123", "test@example.com")
		if err == nil {
			t.Error("BeginLogin() with wrong tenant should return error")
		}
	})

	t.Run("no credentials", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		_, err = manager.BeginLogin(ctx, "tenant123", "test@example.com")
		if err == nil {
			t.Error("BeginLogin() with no credentials should return error")
		}
	})
}

func TestWebAuthnManager_FinishLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("credential not found", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		response := &protocol.ParsedCredentialAssertionData{
			ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{
				RawID: []byte("nonexistent-cred"),
			},
		}

		_, err = manager.FinishLogin(ctx, "tenant123", response)
		if err == nil {
			t.Error("FinishLogin() with non-existent credential should return error")
		}
	})

	t.Run("expired challenge", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		// Create credential
		cred := &WebAuthnCredential{
			ID:        base64.RawURLEncoding.EncodeToString([]byte("cred123")),
			UserID:    "user123",
			TenantID:  "tenant123",
			PublicKey: []byte("pk"),
		}
		credStorage.CreateCredential(ctx, cred)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		// Create expired challenge
		challengeBytes := []byte("test-challenge-bytes-32chars!!")
		challengeStr := base64.RawURLEncoding.EncodeToString(challengeBytes)

		sessionData := &webauthn.SessionData{
			Challenge: challengeStr,
			UserID:    []byte("user123"),
		}
		sessionBytes, _ := json.Marshal(sessionData)

		expiredChallenge := &WebAuthnChallenge{
			ID:          challengeStr,
			UserID:      "user123",
			TenantID:    "tenant123",
			Challenge:   challengeBytes,
			Type:        ChallengeTypeAuthentication,
			SessionData: sessionBytes,
			CreatedAt:   time.Now().Add(-2 * time.Hour),
			ExpiresAt:   time.Now().Add(-1 * time.Hour),
		}
		credStorage.StoreChallenge(ctx, expiredChallenge)

		response := &protocol.ParsedCredentialAssertionData{
			ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{
				RawID: []byte("cred123"),
			},
			Response: protocol.ParsedAssertionResponse{
				CollectedClientData: protocol.CollectedClientData{
					Challenge: challengeStr,
					Origin:    "https://example.com",
					Type:      "webauthn.get",
				},
			},
		}

		_, err = manager.FinishLogin(ctx, "tenant123", response)
		if err == nil {
			t.Error("FinishLogin() with expired challenge should return error")
		}
	})

	t.Run("challenge not found", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		cred := &WebAuthnCredential{
			ID:        base64.RawURLEncoding.EncodeToString([]byte("cred123")),
			UserID:    "user123",
			TenantID:  "tenant123",
			PublicKey: []byte("pk"),
		}
		credStorage.CreateCredential(ctx, cred)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		response := &protocol.ParsedCredentialAssertionData{
			ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{
				RawID: []byte("cred123"),
			},
			Response: protocol.ParsedAssertionResponse{
				CollectedClientData: protocol.CollectedClientData{
					Challenge: "nonexistent",
					Origin:    "https://example.com",
					Type:      "webauthn.get",
				},
			},
		}

		_, err = manager.FinishLogin(ctx, "tenant123", response)
		if err == nil {
			t.Error("FinishLogin() with non-existent challenge should return error")
		}
	})
}

func TestWebAuthnManager_findChallengeByValue(t *testing.T) {
	ctx := context.Background()

	t.Run("challenge found", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		// Store challenge
		challengeBytes := []byte("test-challenge-bytes-32chars!!")
		challengeStr := base64.RawURLEncoding.EncodeToString(challengeBytes)

		challenge := &WebAuthnChallenge{
			ID:        challengeStr,
			UserID:    "user123",
			TenantID:  "tenant123",
			Challenge: challengeBytes,
			Type:      ChallengeTypeRegistration,
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		credStorage.StoreChallenge(ctx, challenge)

		// Find challenge
		found, err := manager.findChallengeByValue(ctx, "user123", challengeStr)
		if err != nil {
			t.Fatalf("findChallengeByValue() error = %v", err)
		}

		if found.ID != challengeStr {
			t.Errorf("findChallengeByValue() ID = %s, want %s", found.ID, challengeStr)
		}
	})

	t.Run("challenge not found", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		_, err = manager.findChallengeByValue(ctx, "user123", "nonexistent")
		if err == nil {
			t.Error("findChallengeByValue() with non-existent challenge should return error")
		}
	})
}

func TestGenerateChallenge(t *testing.T) {
	t.Run("generates valid challenge", func(t *testing.T) {
		challenge, err := generateChallenge()
		if err != nil {
			t.Fatalf("generateChallenge() error = %v", err)
		}

		if len(challenge) != 32 {
			t.Errorf("generateChallenge() length = %d, want 32", len(challenge))
		}
	})

	t.Run("generates unique challenges", func(t *testing.T) {
		challenge1, err := generateChallenge()
		if err != nil {
			t.Fatalf("generateChallenge() error = %v", err)
		}

		challenge2, err := generateChallenge()
		if err != nil {
			t.Fatalf("generateChallenge() error = %v", err)
		}

		// Challenges should be different
		if string(challenge1) == string(challenge2) {
			t.Error("generateChallenge() generated identical challenges")
		}
	})
}

// errorCredentialStorage wraps MemoryCredentialStorage to simulate errors
type errorCredentialStorage struct {
	*MemoryCredentialStorage
	getCredentialsByUserErr error
	createCredentialErr     error
	updateCredentialErr     error
}

func newErrorCredentialStorage() *errorCredentialStorage {
	return &errorCredentialStorage{
		MemoryCredentialStorage: NewMemoryCredentialStorage(),
	}
}

func (s *errorCredentialStorage) GetCredentialsByUser(ctx context.Context, tenantID, userID string) ([]*WebAuthnCredential, error) {
	if s.getCredentialsByUserErr != nil {
		return nil, s.getCredentialsByUserErr
	}
	return s.MemoryCredentialStorage.GetCredentialsByUser(ctx, tenantID, userID)
}

func (s *errorCredentialStorage) CreateCredential(ctx context.Context, cred *WebAuthnCredential) error {
	if s.createCredentialErr != nil {
		return s.createCredentialErr
	}
	return s.MemoryCredentialStorage.CreateCredential(ctx, cred)
}

func (s *errorCredentialStorage) UpdateCredential(ctx context.Context, cred *WebAuthnCredential) error {
	if s.updateCredentialErr != nil {
		return s.updateCredentialErr
	}
	return s.MemoryCredentialStorage.UpdateCredential(ctx, cred)
}

// TestWebAuthnManager_FinishRegistration_ErrorPaths tests error paths in FinishRegistration
func TestWebAuthnManager_FinishRegistration_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("GetCredentialsByUser error", func(t *testing.T) {
		credStorage := newErrorCredentialStorage()
		credStorage.getCredentialsByUserErr = NewIAMError(ErrCredentialNotFound, "test", "storage error", nil)

		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		response := &protocol.ParsedCredentialCreationData{
			Response: protocol.ParsedAttestationResponse{
				CollectedClientData: protocol.CollectedClientData{
					Challenge: "test-challenge",
				},
			},
		}

		_, err = manager.FinishRegistration(ctx, "tenant123", "user123", response)
		if err == nil {
			t.Error("FinishRegistration() should return error when GetCredentialsByUser fails")
		}
	})

	t.Run("invalid session JSON", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		// Create a challenge with invalid session data
		challengeBytes := []byte("challenge-bytes-32-chars-here!!")
		challengeStr := base64.RawURLEncoding.EncodeToString(challengeBytes)

		challenge := &WebAuthnChallenge{
			ID:          challengeStr,
			UserID:      "user123",
			TenantID:    "tenant123",
			Challenge:   challengeBytes,
			Type:        ChallengeTypeRegistration,
			SessionData: []byte("invalid json"),
			CreatedAt:   time.Now(),
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		}
		credStorage.StoreChallenge(ctx, challenge)

		response := &protocol.ParsedCredentialCreationData{
			Response: protocol.ParsedAttestationResponse{
				CollectedClientData: protocol.CollectedClientData{
					Challenge: challengeStr,
				},
			},
		}

		_, err = manager.FinishRegistration(ctx, "tenant123", "user123", response)
		if err == nil {
			t.Error("FinishRegistration() should return error for invalid session JSON")
		}
	})

	t.Run("webauthn CreateCredential fails", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		// Begin registration to get valid session
		creation, err := manager.BeginRegistration(ctx, "tenant123", "user123")
		if err != nil {
			t.Fatalf("BeginRegistration() error = %v", err)
		}

		challengeStr := base64.RawURLEncoding.EncodeToString(creation.Response.Challenge)

		// Create response with invalid data that will fail verification
		response := &protocol.ParsedCredentialCreationData{
			ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{
				RawID: []byte("test-cred"),
			},
			Response: protocol.ParsedAttestationResponse{
				CollectedClientData: protocol.CollectedClientData{
					Challenge: challengeStr,
					Origin:    "https://example.com",
					Type:      "webauthn.create",
				},
			},
		}

		_, err = manager.FinishRegistration(ctx, "tenant123", "user123", response)
		if err == nil {
			t.Error("FinishRegistration() should return error when verification fails")
		}
	})
}

// TestWebAuthnManager_FinishLogin_ErrorPaths tests error paths in FinishLogin
func TestWebAuthnManager_FinishLogin_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("GetUser error after credential lookup", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		// Create credential but don't add user
		cred := &WebAuthnCredential{
			ID:        base64.RawURLEncoding.EncodeToString([]byte("cred123")),
			UserID:    "user123",
			TenantID:  "tenant123",
			PublicKey: []byte("pk"),
		}
		credStorage.CreateCredential(ctx, cred)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		response := &protocol.ParsedCredentialAssertionData{
			ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{
				RawID: []byte("cred123"),
			},
		}

		_, err = manager.FinishLogin(ctx, "tenant123", response)
		if err == nil {
			t.Error("FinishLogin() should return error when user not found")
		}
	})

	t.Run("GetCredentialsByUser error", func(t *testing.T) {
		credStorage := newErrorCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		// Create credential
		cred := &WebAuthnCredential{
			ID:        base64.RawURLEncoding.EncodeToString([]byte("cred123")),
			UserID:    "user123",
			TenantID:  "tenant123",
			PublicKey: []byte("pk"),
		}
		credStorage.CreateCredential(ctx, cred)

		// Set error for GetCredentialsByUser
		credStorage.getCredentialsByUserErr = NewIAMError(ErrCredentialNotFound, "test", "storage error", nil)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		response := &protocol.ParsedCredentialAssertionData{
			ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{
				RawID: []byte("cred123"),
			},
		}

		_, err = manager.FinishLogin(ctx, "tenant123", response)
		if err == nil {
			t.Error("FinishLogin() should return error when GetCredentialsByUser fails")
		}
	})

	t.Run("invalid session JSON", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		// Create credential
		cred := &WebAuthnCredential{
			ID:        base64.RawURLEncoding.EncodeToString([]byte("cred123")),
			UserID:    "user123",
			TenantID:  "tenant123",
			PublicKey: []byte("pk"),
		}
		credStorage.CreateCredential(ctx, cred)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		// Create a challenge with invalid session data
		challengeBytes := []byte("challenge-bytes-32-chars-here!!")
		challengeStr := base64.RawURLEncoding.EncodeToString(challengeBytes)

		challenge := &WebAuthnChallenge{
			ID:          challengeStr,
			UserID:      "user123",
			TenantID:    "tenant123",
			Challenge:   challengeBytes,
			Type:        ChallengeTypeAuthentication,
			SessionData: []byte("invalid json"),
			CreatedAt:   time.Now(),
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		}
		credStorage.StoreChallenge(ctx, challenge)

		response := &protocol.ParsedCredentialAssertionData{
			ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{
				RawID: []byte("cred123"),
			},
			Response: protocol.ParsedAssertionResponse{
				CollectedClientData: protocol.CollectedClientData{
					Challenge: challengeStr,
				},
			},
		}

		_, err = manager.FinishLogin(ctx, "tenant123", response)
		if err == nil {
			t.Error("FinishLogin() should return error for invalid session JSON")
		}
	})

	t.Run("webauthn ValidateLogin fails", func(t *testing.T) {
		credStorage := NewMemoryCredentialStorage()
		identityStore := newMockWebAuthnIdentityStore()
		sessionStorage := NewMemorySessionStorage()

		user := &User{
			ID:       "user123",
			Email:    "test@example.com",
			TenantID: "tenant123",
		}
		identityStore.addUser(user)

		// Create credential
		cred := &WebAuthnCredential{
			ID:        base64.RawURLEncoding.EncodeToString([]byte("cred123")),
			UserID:    "user123",
			TenantID:  "tenant123",
			PublicKey: []byte("pk"),
		}
		credStorage.CreateCredential(ctx, cred)

		manager, err := NewWebAuthnManager(
			&WebAuthnConfig{
				RPID:      "example.com",
				RPName:    "Example",
				RPOrigins: []string{"https://example.com"},
			},
			WebAuthnDependencies{
				CredentialStorage: credStorage,
				IdentityStore:     identityStore,
				SessionStorage:    sessionStorage,
			},
		)
		if err != nil {
			t.Fatalf("NewWebAuthnManager() error = %v", err)
		}

		// Begin login to get valid session
		assertion, err := manager.BeginLogin(ctx, "tenant123", "test@example.com")
		if err != nil {
			t.Fatalf("BeginLogin() error = %v", err)
		}

		challengeStr := base64.RawURLEncoding.EncodeToString(assertion.Response.Challenge)

		// Create response with invalid data that will fail validation
		response := &protocol.ParsedCredentialAssertionData{
			ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{
				RawID: []byte("cred123"),
			},
			Response: protocol.ParsedAssertionResponse{
				CollectedClientData: protocol.CollectedClientData{
					Challenge: challengeStr,
					Origin:    "https://example.com",
					Type:      "webauthn.get",
				},
			},
		}

		_, err = manager.FinishLogin(ctx, "tenant123", response)
		if err == nil {
			t.Error("FinishLogin() should return error when validation fails")
		}
	})
}
