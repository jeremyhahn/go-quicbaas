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
)

// CredentialStorage defines the interface for WebAuthn credential persistence operations.
// Implementations must provide thread-safe, atomic operations for credential data.
type CredentialStorage interface {
	// CreateCredential stores a new WebAuthn credential.
	// Returns ErrAlreadyExists if a credential with the same ID exists.
	CreateCredential(ctx context.Context, cred *WebAuthnCredential) error

	// GetCredential retrieves a credential by ID.
	// Returns ErrCredentialNotFound if the credential does not exist.
	GetCredential(ctx context.Context, tenantID, credentialID string) (*WebAuthnCredential, error)

	// GetCredentialsByUser retrieves all credentials for a user.
	// Returns an empty slice if the user has no credentials.
	GetCredentialsByUser(ctx context.Context, tenantID, userID string) ([]*WebAuthnCredential, error)

	// UpdateCredential updates an existing credential (primarily for sign count).
	// Returns ErrCredentialNotFound if the credential does not exist.
	UpdateCredential(ctx context.Context, cred *WebAuthnCredential) error

	// DeleteCredential removes a credential.
	// Returns ErrCredentialNotFound if the credential does not exist.
	DeleteCredential(ctx context.Context, tenantID, credentialID string) error

	// StoreChallenge stores a WebAuthn challenge with TTL.
	// The challenge should be automatically deleted after expiration.
	StoreChallenge(ctx context.Context, challenge *WebAuthnChallenge) error

	// GetChallenge retrieves a challenge by ID.
	// Returns ErrChallengeNotFound if the challenge does not exist or has expired.
	GetChallenge(ctx context.Context, challengeID string) (*WebAuthnChallenge, error)

	// DeleteChallenge removes a challenge.
	// Returns ErrChallengeNotFound if the challenge does not exist.
	DeleteChallenge(ctx context.Context, challengeID string) error
}

// SessionStorage defines the interface for session persistence operations.
// Implementations must provide thread-safe, atomic operations for session data.
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
