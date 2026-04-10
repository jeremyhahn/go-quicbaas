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
	"time"

	"github.com/google/uuid"
)

// SessionManager handles session lifecycle and management operations.
type SessionManager interface {
	// Create creates a new session
	Create(ctx context.Context, session *SessionExt) error

	// Get retrieves a session by ID
	Get(ctx context.Context, sessionID string) (*SessionExt, error)

	// GetByToken retrieves a session by token hash
	GetByToken(ctx context.Context, tokenHash string) (*SessionExt, error)

	// List lists all sessions for a user in a tenant
	List(ctx context.Context, userID, tenantID string) ([]*SessionExt, error)

	// UpdateActivity updates the last activity timestamp for a session
	UpdateActivity(ctx context.Context, sessionID string) error

	// Revoke revokes a specific session
	Revoke(ctx context.Context, sessionID string) error

	// RevokeAllForUser revokes all sessions for a user
	RevokeAllForUser(ctx context.Context, userID, tenantID string) error

	// RevokeAllExcept revokes all sessions for a user except the specified one
	RevokeAllExcept(ctx context.Context, userID, tenantID, exceptSessionID string) error

	// CleanupExpired removes expired sessions and returns count of cleaned sessions
	CleanupExpired(ctx context.Context) (int, error)

	// ValidateSession validates a session against policy and returns error if invalid
	ValidateSession(ctx context.Context, sessionID string) error

	// EnforcePolicy enforces session policy limits for a user
	EnforcePolicy(ctx context.Context, userID, tenantID string, policy *SessionPolicy) error
}

// DefaultSessionManager implements SessionManager.
type DefaultSessionManager struct {
	storage SessionExtStorage
	policy  *SessionPolicy
}

// SessionManagerOption configures the session manager.
type SessionManagerOption func(*DefaultSessionManager)

// WithSessionStorage sets the session storage.
func WithSessionStorage(storage SessionExtStorage) SessionManagerOption {
	return func(m *DefaultSessionManager) {
		m.storage = storage
	}
}

// WithSessionPolicy sets the default session policy.
func WithSessionPolicy(policy *SessionPolicy) SessionManagerOption {
	return func(m *DefaultSessionManager) {
		m.policy = policy
	}
}

// NewSessionManager creates a new session manager.
func NewSessionManager(opts ...SessionManagerOption) (*DefaultSessionManager, error) {
	m := &DefaultSessionManager{
		storage: NewMemorySessionExtStorage(),
		policy:  &SessionPolicy{},
	}

	for _, opt := range opts {
		opt(m)
	}

	// Apply defaults to policy
	m.policy.ApplyDefaults()

	// Validate policy
	if err := m.policy.Validate(); err != nil {
		return nil, err
	}

	return m, nil
}

// Create creates a new session.
// Create creates a new session.
func (m *DefaultSessionManager) Create(ctx context.Context, session *SessionExt) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Generate session ID if not provided
	if session.ID == "" {
		session.ID = uuid.New().String()
	}

	// Validate session
	if err := session.Validate(); err != nil {
		return err
	}

	// Set timestamps
	now := time.Now()
	if session.Created.IsZero() {
		session.Created = now
	}
	if session.LastActive.IsZero() {
		session.LastActive = now
	}

	// Set expiration if not provided
	if session.ExpiresAt.IsZero() {
		if m.policy != nil && m.policy.AbsoluteTimeout > 0 {
			session.ExpiresAt = now.Add(m.policy.AbsoluteTimeout)
		} else {
			session.ExpiresAt = now.Add(24 * time.Hour) // Default 24 hours
		}
	}

	// Check session limit
	if m.policy != nil && m.policy.MaxConcurrent > 0 {
		activeSessions, err := m.storage.ListByUserActive(ctx, session.UserID, session.TenantID)
		if err != nil {
			return err
		}

		if len(activeSessions) >= m.policy.MaxConcurrent {
			return NewIAMError(ErrSessionLimitExceeded, "iam.session.create",
				"maximum concurrent sessions exceeded", nil)
		}
	}

	// Create session in storage
	return m.storage.Create(ctx, session)
}

// Get retrieves a session by ID.
func (m *DefaultSessionManager) Get(ctx context.Context, sessionID string) (*SessionExt, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	session, err := m.storage.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Check if expired
	if session.IsExpired() {
		// Clean up expired session
		_ = m.storage.Delete(ctx, sessionID)
		return nil, ErrSessionExpired
	}

	return session, nil
}

// GetByToken retrieves a session by token hash.
func (m *DefaultSessionManager) GetByToken(ctx context.Context, tokenHash string) (*SessionExt, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	session, err := m.storage.GetByToken(ctx, tokenHash)
	if err != nil {
		return nil, err
	}

	// Check if expired
	if session.IsExpired() {
		// Clean up expired session
		_ = m.storage.Delete(ctx, session.ID)
		return nil, ErrSessionExpired
	}

	// Check if idle
	if m.policy != nil && session.IsIdle(m.policy) {
		// Clean up idle session
		_ = m.storage.Delete(ctx, session.ID)
		return nil, ErrSessionExpired
	}

	return session, nil
}

// List lists all active sessions for a user.
func (m *DefaultSessionManager) List(ctx context.Context, userID, tenantID string) ([]*SessionExt, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if userID == "" {
		return nil, NewIAMError(ErrSessionInvalid, "iam.session.list", "user ID is required", nil)
	}
	if tenantID == "" {
		return nil, NewIAMError(ErrSessionInvalid, "iam.session.list", "tenant ID is required", nil)
	}

	// Get active sessions only
	sessions, err := m.storage.ListByUserActive(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	// Filter out idle sessions if policy is configured
	if m.policy != nil && m.policy.IdleTimeout > 0 {
		activeSessions := make([]*SessionExt, 0, len(sessions))
		for _, session := range sessions {
			if !session.IsIdle(m.policy) {
				activeSessions = append(activeSessions, session)
			}
		}
		return activeSessions, nil
	}

	return sessions, nil
}

// UpdateActivity updates the last activity timestamp for a session.
func (m *DefaultSessionManager) UpdateActivity(ctx context.Context, sessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if sessionID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.update_activity", "session ID is required", nil)
	}

	session, err := m.storage.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	// Check if expired
	if session.IsExpired() {
		_ = m.storage.Delete(ctx, sessionID)
		return ErrSessionExpired
	}

	// Update last activity
	session.LastActive = time.Now()

	return m.storage.Update(ctx, session)
}

// Revoke revokes a specific session.
func (m *DefaultSessionManager) Revoke(ctx context.Context, sessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if sessionID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.revoke", "session ID is required", nil)
	}

	return m.storage.Delete(ctx, sessionID)
}

// RevokeAllForUser revokes all sessions for a user.
func (m *DefaultSessionManager) RevokeAllForUser(ctx context.Context, userID, tenantID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if userID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.revoke_all", "user ID is required", nil)
	}
	if tenantID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.revoke_all", "tenant ID is required", nil)
	}

	return m.storage.DeleteByUser(ctx, userID, tenantID)
}

// RevokeAllExcept revokes all sessions for a user except the specified one.
func (m *DefaultSessionManager) RevokeAllExcept(ctx context.Context, userID, tenantID, exceptSessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if userID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.revoke_all_except", "user ID is required", nil)
	}
	if tenantID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.revoke_all_except", "tenant ID is required", nil)
	}
	if exceptSessionID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.revoke_all_except", "except session ID is required", nil)
	}

	return m.storage.DeleteAllExcept(ctx, userID, tenantID, exceptSessionID)
}

// CleanupExpired removes all expired sessions.
func (m *DefaultSessionManager) CleanupExpired(ctx context.Context) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	return m.storage.CleanupExpired(ctx)
}

// ValidateSession validates a session against policy.
func (m *DefaultSessionManager) ValidateSession(ctx context.Context, sessionID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	session, err := m.storage.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	// Check if expired
	if session.IsExpired() {
		_ = m.storage.Delete(ctx, sessionID)
		return ErrSessionExpired
	}

	// Check if idle
	if m.policy != nil && session.IsIdle(m.policy) {
		_ = m.storage.Delete(ctx, sessionID)
		return ErrSessionExpired
	}

	return nil
}

// EnforcePolicy enforces session policy limits for a user.
func (m *DefaultSessionManager) EnforcePolicy(ctx context.Context, userID, tenantID string, policy *SessionPolicy) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if policy == nil {
		policy = m.policy
	}

	// Get active sessions
	sessions, err := m.storage.ListByUserActive(ctx, userID, tenantID)
	if err != nil {
		return err
	}

	// Enforce max concurrent sessions
	if policy.MaxConcurrent > 0 && len(sessions) > policy.MaxConcurrent {
		// Sort by last activity and keep only the most recent ones
		// For simplicity, delete oldest sessions
		excessCount := len(sessions) - policy.MaxConcurrent

		// Find oldest sessions
		for i := 0; i < excessCount; i++ {
			oldestIdx := 0
			oldestTime := sessions[0].LastActive

			for j := 1; j < len(sessions); j++ {
				if sessions[j].LastActive.Before(oldestTime) {
					oldestIdx = j
					oldestTime = sessions[j].LastActive
				}
			}

			// Delete oldest session
			_ = m.storage.Delete(ctx, sessions[oldestIdx].ID)

			// Remove from list
			sessions = append(sessions[:oldestIdx], sessions[oldestIdx+1:]...)
		}
	}

	return nil
}

// Ensure DefaultSessionManager implements SessionManager
var _ SessionManager = (*DefaultSessionManager)(nil)
