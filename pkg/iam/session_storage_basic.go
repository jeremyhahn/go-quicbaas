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
)

// SessionStorage defines the interface for basic session persistence operations.
// Implementations must provide thread-safe, atomic operations for session data.
//
// This is the lightweight session-storage abstraction used throughout the
// quicbaas IAM domain (gRPC SessionServiceAdapter, IAMServiceProvider, etc.).
// Despite its historical co-location with the WebAuthn package, it operates
// over the general iam.Session type with no WebAuthn imports — it is the
// canonical session-record store for the IAM service layer.
//
// For the richer extended-session abstraction (token hashes, device info,
// active-only filtering) see SessionExtStorage in session_storage.go.
type SessionStorage interface {
	// CreateSession stores a new session.
	// Returns ErrAlreadyExists if a session with the same ID exists.
	CreateSession(ctx context.Context, session *Session) error

	// GetSession retrieves a session by ID.
	// Returns ErrNotFound if the session does not exist.
	GetSession(ctx context.Context, sessionID string) (*Session, error)

	// GetSessionsByUser retrieves all active sessions for a user.
	// Returns an empty slice if the user has no active sessions.
	GetSessionsByUser(ctx context.Context, tenantID, userID string) ([]*Session, error)

	// UpdateSession updates an existing session (primarily for last activity).
	// Returns ErrNotFound if the session does not exist.
	UpdateSession(ctx context.Context, session *Session) error

	// DeleteSession removes a session.
	// Returns ErrNotFound if the session does not exist.
	DeleteSession(ctx context.Context, sessionID string) error

	// DeleteExpiredSessions removes all expired sessions.
	// Returns the number of sessions deleted.
	DeleteExpiredSessions(ctx context.Context) (int, error)
}
