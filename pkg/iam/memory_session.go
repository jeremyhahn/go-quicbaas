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
)

// MemorySessionStorage is an in-memory implementation of SessionStorage for
// testing and embedded deployments. It is NOT suitable for production use as
// data is lost on restart.
//
// The store is concurrency-safe via sync.RWMutex. All accessor methods clone
// stored sessions on the way out so external callers cannot mutate internal
// state.
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

// Compile-time interface assertion.
var _ SessionStorage = (*MemorySessionStorage)(nil)
