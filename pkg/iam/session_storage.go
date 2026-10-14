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
	"strconv"
	"sync"
	"time"
)

// SessionStorage defines the storage interface for session data.
type SessionExtStorage interface {
	// Session CRUD operations
	Create(ctx context.Context, session *SessionExt) error
	Get(ctx context.Context, sessionID string) (*SessionExt, error)
	GetByToken(ctx context.Context, tokenHash string) (*SessionExt, error)
	Update(ctx context.Context, session *SessionExt) error
	Delete(ctx context.Context, sessionID string) error

	// Query operations
	ListByUser(ctx context.Context, userID uint64, tenantID string) ([]*SessionExt, error)
	ListByUserActive(ctx context.Context, userID uint64, tenantID string) ([]*SessionExt, error)

	// Batch operations
	DeleteByUser(ctx context.Context, userID uint64, tenantID string) error
	DeleteAllExcept(ctx context.Context, userID uint64, tenantID, exceptSessionID string) error

	// Maintenance
	CleanupExpired(ctx context.Context) (int, error)
}

// MemorySessionExtStorage implements SessionStorage using in-memory storage.
type MemorySessionExtStorage struct {
	mu sync.RWMutex

	// Primary indexes
	sessions        map[string]*SessionExt // key: sessionID
	sessionsByToken map[string]string      // key: tokenHash -> sessionID
	sessionsByUser  map[string][]string    // key: tenantID:userID -> []sessionID
}

// NewMemorySessionExtStorage creates a new in-memory session storage.
func NewMemorySessionExtStorage() *MemorySessionExtStorage {
	return &MemorySessionExtStorage{
		sessions:        make(map[string]*SessionExt),
		sessionsByToken: make(map[string]string),
		sessionsByUser:  make(map[string][]string),
	}
}

func (m *MemorySessionExtStorage) userKey(userID uint64, tenantID string) string {
	return tenantID + ":" + strconv.FormatUint(userID, 10)
}

// Create stores a new session.
func (m *MemorySessionExtStorage) Create(ctx context.Context, session *SessionExt) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := session.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if session already exists
	if _, exists := m.sessions[session.ID]; exists {
		return NewIAMError(ErrSessionInvalid, "iam.session.create", "session already exists", nil)
	}

	// Store a copy
	sessionCopy := m.copySession(session)
	m.sessions[session.ID] = sessionCopy

	// Update indexes
	m.sessionsByToken[session.TokenHash] = session.ID
	if session.RefreshHash != "" {
		m.sessionsByToken[session.RefreshHash] = session.ID
	}

	userKey := m.userKey(session.UserID, session.TenantID)
	m.sessionsByUser[userKey] = append(m.sessionsByUser[userKey], session.ID)

	return nil
}

// Get retrieves a session by ID.
func (m *MemorySessionExtStorage) Get(ctx context.Context, sessionID string) (*SessionExt, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if sessionID == "" {
		return nil, NewIAMError(ErrSessionInvalid, "iam.session.get", "session ID is required", nil)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	// Return a copy
	return m.copySession(session), nil
}

// GetByToken retrieves a session by token hash.
func (m *MemorySessionExtStorage) GetByToken(ctx context.Context, tokenHash string) (*SessionExt, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if tokenHash == "" {
		return nil, NewIAMError(ErrSessionInvalid, "iam.session.get_by_token", "token hash is required", nil)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	sessionID, ok := m.sessionsByToken[tokenHash]
	if !ok {
		return nil, ErrSessionNotFound
	}

	session, ok := m.sessions[sessionID]
	if !ok {
		// Index inconsistency - clean it up
		delete(m.sessionsByToken, tokenHash)
		return nil, ErrSessionNotFound
	}

	// Return a copy
	return m.copySession(session), nil
}

// Update updates an existing session.
func (m *MemorySessionExtStorage) Update(ctx context.Context, session *SessionExt) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := session.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.sessions[session.ID]
	if !ok {
		return ErrSessionNotFound
	}

	// Update token hash indexes if changed
	if existing.TokenHash != session.TokenHash {
		delete(m.sessionsByToken, existing.TokenHash)
		m.sessionsByToken[session.TokenHash] = session.ID
	}
	if existing.RefreshHash != session.RefreshHash {
		if existing.RefreshHash != "" {
			delete(m.sessionsByToken, existing.RefreshHash)
		}
		if session.RefreshHash != "" {
			m.sessionsByToken[session.RefreshHash] = session.ID
		}
	}

	// Store updated copy
	m.sessions[session.ID] = m.copySession(session)
	return nil
}

// Delete removes a session.
func (m *MemorySessionExtStorage) Delete(ctx context.Context, sessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if sessionID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.delete", "session ID is required", nil)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil // Idempotent delete
	}

	// Remove from indexes
	delete(m.sessionsByToken, session.TokenHash)
	if session.RefreshHash != "" {
		delete(m.sessionsByToken, session.RefreshHash)
	}

	userKey := m.userKey(session.UserID, session.TenantID)
	m.removeFromUserIndex(userKey, sessionID)

	// Remove session
	delete(m.sessions, sessionID)
	return nil
}

// ListByUser lists all sessions for a user.
func (m *MemorySessionExtStorage) ListByUser(ctx context.Context, userID uint64, tenantID string) ([]*SessionExt, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	userKey := m.userKey(userID, tenantID)
	sessionIDs, ok := m.sessionsByUser[userKey]
	if !ok || len(sessionIDs) == 0 {
		return []*SessionExt{}, nil
	}

	sessions := make([]*SessionExt, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if session, ok := m.sessions[sessionID]; ok {
			sessions = append(sessions, m.copySession(session))
		}
	}

	return sessions, nil
}

// ListByUserActive lists all active (non-expired) sessions for a user.
func (m *MemorySessionExtStorage) ListByUserActive(ctx context.Context, userID uint64, tenantID string) ([]*SessionExt, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	userKey := m.userKey(userID, tenantID)
	sessionIDs, ok := m.sessionsByUser[userKey]
	if !ok || len(sessionIDs) == 0 {
		return []*SessionExt{}, nil
	}

	now := time.Now()
	sessions := make([]*SessionExt, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if session, ok := m.sessions[sessionID]; ok {
			if !now.After(session.ExpiresAt) {
				sessions = append(sessions, m.copySession(session))
			}
		}
	}

	return sessions, nil
}

// DeleteByUser removes all sessions for a user.
func (m *MemorySessionExtStorage) DeleteByUser(ctx context.Context, userID uint64, tenantID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	userKey := m.userKey(userID, tenantID)
	sessionIDs, ok := m.sessionsByUser[userKey]
	if !ok || len(sessionIDs) == 0 {
		return nil
	}

	// Remove all sessions and their indexes
	for _, sessionID := range sessionIDs {
		if session, ok := m.sessions[sessionID]; ok {
			delete(m.sessionsByToken, session.TokenHash)
			if session.RefreshHash != "" {
				delete(m.sessionsByToken, session.RefreshHash)
			}
			delete(m.sessions, sessionID)
		}
	}

	// Clear user index
	delete(m.sessionsByUser, userKey)
	return nil
}

// DeleteAllExcept removes all sessions for a user except the specified one.
func (m *MemorySessionExtStorage) DeleteAllExcept(ctx context.Context, userID uint64, tenantID, exceptSessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	userKey := m.userKey(userID, tenantID)
	sessionIDs, ok := m.sessionsByUser[userKey]
	if !ok || len(sessionIDs) == 0 {
		return nil
	}

	// Remove all sessions except the specified one
	newSessionIDs := make([]string, 0, 1)
	for _, sessionID := range sessionIDs {
		if sessionID == exceptSessionID {
			newSessionIDs = append(newSessionIDs, sessionID)
			continue
		}

		if session, ok := m.sessions[sessionID]; ok {
			delete(m.sessionsByToken, session.TokenHash)
			if session.RefreshHash != "" {
				delete(m.sessionsByToken, session.RefreshHash)
			}
			delete(m.sessions, sessionID)
		}
	}

	// Update user index
	if len(newSessionIDs) > 0 {
		m.sessionsByUser[userKey] = newSessionIDs
	} else {
		delete(m.sessionsByUser, userKey)
	}

	return nil
}

// CleanupExpired removes all expired sessions.
func (m *MemorySessionExtStorage) CleanupExpired(ctx context.Context) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	count := 0

	// Collect expired session IDs
	expiredIDs := make([]string, 0)
	for sessionID, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			expiredIDs = append(expiredIDs, sessionID)
		}
	}

	// Remove expired sessions
	for _, sessionID := range expiredIDs {
		session := m.sessions[sessionID]

		// Remove from indexes
		delete(m.sessionsByToken, session.TokenHash)
		if session.RefreshHash != "" {
			delete(m.sessionsByToken, session.RefreshHash)
		}

		userKey := m.userKey(session.UserID, session.TenantID)
		m.removeFromUserIndex(userKey, sessionID)

		// Remove session
		delete(m.sessions, sessionID)
		count++
	}

	return count, nil
}

// Helper methods

func (m *MemorySessionExtStorage) copySession(s *SessionExt) *SessionExt {
	if s == nil {
		return nil
	}

	copy := &SessionExt{
		ID:          s.ID,
		UserID:      s.UserID,
		TenantID:    s.TenantID,
		TokenHash:   s.TokenHash,
		RefreshHash: s.RefreshHash,
		MFAVerified: s.MFAVerified,
		IPAddress:   s.IPAddress,
		UserAgent:   s.UserAgent,
		Created:     s.Created,
		LastActive:  s.LastActive,
		ExpiresAt:   s.ExpiresAt,
	}

	// Copy device info if present
	if s.DeviceInfo != nil {
		deviceCopy := *s.DeviceInfo
		copy.DeviceInfo = &deviceCopy
	}

	// Copy metadata if present
	if s.Metadata != nil {
		copy.Metadata = make(map[string]string, len(s.Metadata))
		for k, v := range s.Metadata {
			copy.Metadata[k] = v
		}
	}

	return copy
}

func (m *MemorySessionExtStorage) removeFromUserIndex(userKey, sessionID string) {
	sessionIDs, ok := m.sessionsByUser[userKey]
	if !ok {
		return
	}

	// Remove session ID from list
	newSessionIDs := make([]string, 0, len(sessionIDs)-1)
	for _, id := range sessionIDs {
		if id != sessionID {
			newSessionIDs = append(newSessionIDs, id)
		}
	}

	if len(newSessionIDs) > 0 {
		m.sessionsByUser[userKey] = newSessionIDs
	} else {
		delete(m.sessionsByUser, userKey)
	}
}

// Ensure MemorySessionExtStorage implements SessionExtStorage.
var _ SessionExtStorage = (*MemorySessionExtStorage)(nil)
