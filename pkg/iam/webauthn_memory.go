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
	"sync"
	"time"
)

// MemoryCredentialStorage is an in-memory implementation of CredentialStorage for testing.
// It is NOT suitable for production use as data is lost on restart.
type MemoryCredentialStorage struct {
	mu          sync.RWMutex
	credentials map[string]*WebAuthnCredential // key: tenantID:credentialID
	userCreds   map[string][]string            // key: tenantID:userID, value: credential IDs
	challenges  map[string]*WebAuthnChallenge  // key: challengeID
}

// NewMemoryCredentialStorage creates a new in-memory credential storage.
func NewMemoryCredentialStorage() *MemoryCredentialStorage {
	return &MemoryCredentialStorage{
		credentials: make(map[string]*WebAuthnCredential),
		userCreds:   make(map[string][]string),
		challenges:  make(map[string]*WebAuthnChallenge),
	}
}

// CreateCredential stores a new WebAuthn credential.
func (s *MemoryCredentialStorage) CreateCredential(ctx context.Context, cred *WebAuthnCredential) error {
	if err := cred.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.credKey(cred.TenantID, cred.ID)

	// Check if credential already exists
	if _, exists := s.credentials[key]; exists {
		return NewIAMError(ErrCredentialInvalid, "iam.webauthn.storage.create", "credential already exists", nil)
	}

	// Store credential
	s.credentials[key] = cred

	// Update user credentials index
	userKey := s.userKey(cred.TenantID, cred.UserID)
	s.userCreds[userKey] = append(s.userCreds[userKey], cred.ID)

	return nil
}

// GetCredential retrieves a credential by ID.
func (s *MemoryCredentialStorage) GetCredential(ctx context.Context, tenantID, credentialID string) (*WebAuthnCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.credKey(tenantID, credentialID)
	cred, exists := s.credentials[key]
	if !exists {
		return nil, NewCredentialNotFoundError(tenantID, credentialID)
	}

	// Return a copy to prevent external modification
	credCopy := *cred
	return &credCopy, nil
}

// GetCredentialsByUser retrieves all credentials for a user.
func (s *MemoryCredentialStorage) GetCredentialsByUser(ctx context.Context, tenantID, userID string) ([]*WebAuthnCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	userKey := s.userKey(tenantID, userID)
	credIDs, exists := s.userCreds[userKey]
	if !exists || len(credIDs) == 0 {
		return []*WebAuthnCredential{}, nil
	}

	credentials := make([]*WebAuthnCredential, 0, len(credIDs))
	for _, credID := range credIDs {
		key := s.credKey(tenantID, credID)
		if cred, exists := s.credentials[key]; exists {
			// Return a copy
			credCopy := *cred
			credentials = append(credentials, &credCopy)
		}
	}

	return credentials, nil
}

// UpdateCredential updates an existing credential.
func (s *MemoryCredentialStorage) UpdateCredential(ctx context.Context, cred *WebAuthnCredential) error {
	if err := cred.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.credKey(cred.TenantID, cred.ID)

	// Check if credential exists
	if _, exists := s.credentials[key]; !exists {
		return NewCredentialNotFoundError(cred.TenantID, cred.ID)
	}

	// Update credential
	s.credentials[key] = cred

	return nil
}

// DeleteCredential removes a credential.
func (s *MemoryCredentialStorage) DeleteCredential(ctx context.Context, tenantID, credentialID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.credKey(tenantID, credentialID)

	// Check if credential exists
	cred, exists := s.credentials[key]
	if !exists {
		return NewCredentialNotFoundError(tenantID, credentialID)
	}

	// Remove from credentials map
	delete(s.credentials, key)

	// Remove from user credentials index
	userKey := s.userKey(cred.TenantID, cred.UserID)
	if credIDs, exists := s.userCreds[userKey]; exists {
		newCredIDs := make([]string, 0, len(credIDs)-1)
		for _, id := range credIDs {
			if id != credentialID {
				newCredIDs = append(newCredIDs, id)
			}
		}
		s.userCreds[userKey] = newCredIDs
	}

	return nil
}

// StoreChallenge stores a WebAuthn challenge with TTL.
func (s *MemoryCredentialStorage) StoreChallenge(ctx context.Context, challenge *WebAuthnChallenge) error {
	if err := challenge.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.challenges[challenge.ID] = challenge

	// Start a goroutine to clean up expired challenge
	go s.cleanupChallengeAfterTTL(challenge.ID, time.Until(challenge.ExpiresAt))

	return nil
}

// GetChallenge retrieves a challenge by ID.
func (s *MemoryCredentialStorage) GetChallenge(ctx context.Context, challengeID string) (*WebAuthnChallenge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	challenge, exists := s.challenges[challengeID]
	if !exists {
		return nil, NewChallengeNotFoundError(challengeID)
	}

	// Check if expired
	if challenge.IsExpired() {
		return nil, NewChallengeExpiredError(challengeID)
	}

	// Return a copy
	challengeCopy := *challenge
	return &challengeCopy, nil
}

// DeleteChallenge removes a challenge.
func (s *MemoryCredentialStorage) DeleteChallenge(ctx context.Context, challengeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.challenges[challengeID]; !exists {
		return NewChallengeNotFoundError(challengeID)
	}

	delete(s.challenges, challengeID)
	return nil
}

// cleanupChallengeAfterTTL removes a challenge after its TTL expires.
func (s *MemoryCredentialStorage) cleanupChallengeAfterTTL(challengeID string, ttl time.Duration) {
	<-time.After(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.challenges, challengeID)
}

// credKey generates a storage key for a credential.
func (s *MemoryCredentialStorage) credKey(tenantID, credentialID string) string {
	return tenantID + ":" + credentialID
}

// userKey generates a storage key for user credentials index.
func (s *MemoryCredentialStorage) userKey(tenantID, userID string) string {
	return tenantID + ":" + userID
}

// GetChallengeByValue retrieves a challenge by its challenge value.
// This is primarily for testing purposes.
func (s *MemoryCredentialStorage) GetChallengeByValue(ctx context.Context, challengeValue string) (*WebAuthnChallenge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, challenge := range s.challenges {
		if string(challenge.Challenge) == challengeValue {
			// Check if expired
			if challenge.IsExpired() {
				return nil, NewChallengeExpiredError(challenge.ID)
			}
			// Return a copy
			challengeCopy := *challenge
			return &challengeCopy, nil
		}
	}
	return nil, NewChallengeNotFoundError(challengeValue)
}

// CleanupExpiredChallenges removes all expired challenges.
// This should be called periodically in production systems.
func (s *MemoryCredentialStorage) CleanupExpiredChallenges(ctx context.Context) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	now := time.Now()

	for id, challenge := range s.challenges {
		if now.After(challenge.ExpiresAt) {
			delete(s.challenges, id)
			count++
		}
	}

	return count
}

// MemorySessionStorage is an in-memory implementation of SessionStorage for testing.
// It is NOT suitable for production use as data is lost on restart.
type MemorySessionStorage struct {
	mu           sync.RWMutex
	sessions     map[string]*Session // key: sessionID
	userSessions map[string][]string // key: tenantID:userID, value: session IDs
}

// NewMemorySessionStorage creates a new in-memory session storage.
func NewMemorySessionStorage() *MemorySessionStorage {
	return &MemorySessionStorage{
		sessions:     make(map[string]*Session),
		userSessions: make(map[string][]string),
	}
}

// CreateSession stores a new session.
func (s *MemorySessionStorage) CreateSession(ctx context.Context, session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if session already exists
	if _, exists := s.sessions[session.ID]; exists {
		return NewIAMError(ErrUserInvalid, "iam.session.storage.create", "session already exists", nil)
	}

	// Store session
	s.sessions[session.ID] = session

	// Update user sessions index
	userKey := s.userKey(session.TenantID, session.UserID)
	s.userSessions[userKey] = append(s.userSessions[userKey], session.ID)

	return nil
}

// GetSession retrieves a session by ID.
func (s *MemorySessionStorage) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, NewIAMError(ErrUserNotFound, "iam.session.storage.get", "session not found", nil)
	}

	// Return a copy
	sessionCopy := *session
	return &sessionCopy, nil
}

// GetSessionsByUser retrieves all active sessions for a user.
func (s *MemorySessionStorage) GetSessionsByUser(ctx context.Context, tenantID, userID string) ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	userKey := s.userKey(tenantID, userID)
	sessionIDs, exists := s.userSessions[userKey]
	if !exists || len(sessionIDs) == 0 {
		return []*Session{}, nil
	}

	sessions := make([]*Session, 0, len(sessionIDs))

	for _, sessionID := range sessionIDs {
		if session, exists := s.sessions[sessionID]; exists {
			// Only return active (non-expired) sessions
			if !session.IsExpired() {
				sessionCopy := *session
				sessions = append(sessions, &sessionCopy)
			}
		}
	}

	return sessions, nil
}

// UpdateSession updates an existing session.
func (s *MemorySessionStorage) UpdateSession(ctx context.Context, session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if session exists
	if _, exists := s.sessions[session.ID]; !exists {
		return NewIAMError(ErrUserNotFound, "iam.session.storage.update", "session not found", nil)
	}

	// Update session
	s.sessions[session.ID] = session

	return nil
}

// DeleteSession removes a session.
func (s *MemorySessionStorage) DeleteSession(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return NewIAMError(ErrUserNotFound, "iam.session.storage.delete", "session not found", nil)
	}

	// Remove from sessions map
	delete(s.sessions, sessionID)

	// Remove from user sessions index
	userKey := s.userKey(session.TenantID, session.UserID)
	if sessionIDs, exists := s.userSessions[userKey]; exists {
		newSessionIDs := make([]string, 0, len(sessionIDs)-1)
		for _, id := range sessionIDs {
			if id != sessionID {
				newSessionIDs = append(newSessionIDs, id)
			}
		}
		s.userSessions[userKey] = newSessionIDs
	}

	return nil
}

// DeleteExpiredSessions removes all expired sessions.
func (s *MemorySessionStorage) DeleteExpiredSessions(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	expiredSessions := make([]string, 0)

	// Find expired sessions
	for sessionID, session := range s.sessions {
		if session.IsExpired() {
			expiredSessions = append(expiredSessions, sessionID)
			count++
		}
	}

	// Delete expired sessions
	for _, sessionID := range expiredSessions {
		session := s.sessions[sessionID]
		delete(s.sessions, sessionID)

		// Remove from user sessions index
		userKey := s.userKey(session.TenantID, session.UserID)
		if sessionIDs, exists := s.userSessions[userKey]; exists {
			newSessionIDs := make([]string, 0, len(sessionIDs)-1)
			for _, id := range sessionIDs {
				if id != sessionID {
					newSessionIDs = append(newSessionIDs, id)
				}
			}
			s.userSessions[userKey] = newSessionIDs
		}
	}

	return count, nil
}

// userKey generates a storage key for user sessions index.
func (s *MemorySessionStorage) userKey(tenantID, userID string) string {
	return tenantID + ":" + userID
}
