// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package cli

import (
	"context"
	"sync"
	"time"
)

// =============================================================================
// Stored-session lifecycle.
//
// Access tokens are deliberately short-lived (15 minutes). A CLI invocation, an
// `quicbaas mcp serve --stdio` process that Claude Code keeps alive for hours,
// and any long-running agent therefore MUST renew the stored session rather
// than fail with "authentication required" at the first expiry.
//
// TokenManager owns that policy in one place: read the stored envelope, decide
// whether the access token still has useful life, and — when it does not —
// redeem the refresh credential and persist the ROTATED pair back to disk.
// =============================================================================

// RefreshSkew is how much life an access token must have left to be used
// as-is. Anything inside this window is renewed first so a request cannot
// expire in flight.
const RefreshSkew = 60 * time.Second

// SessionRefresher redeems a refresh credential for a renewed session. It is
// implemented by the CLI against the SDK auth client; keeping it an interface
// keeps this package free of transport concerns and unit-testable.
type SessionRefresher interface {
	// Refresh exchanges the refresh credential for a renewed session. The
	// returned TokenData carries the new access token, its expiry, and the
	// rotated refresh credential.
	Refresh(ctx context.Context, refreshToken string) (*TokenData, error)
}

// SessionRefresherFunc adapts a function to SessionRefresher.
type SessionRefresherFunc func(ctx context.Context, refreshToken string) (*TokenData, error)

// Refresh implements SessionRefresher.
func (f SessionRefresherFunc) Refresh(ctx context.Context, refreshToken string) (*TokenData, error) {
	return f(ctx, refreshToken)
}

// TokenManager resolves a usable access token from the stored session,
// renewing it when needed.
//
// The mutex makes renewal single-flight: concurrent callers serialize, and the
// waiter re-reads the (already renewed) envelope from disk instead of
// redeeming a refresh credential that the first caller has just rotated away.
// Renewal is a rare, IO-bound operation, so serializing it is both correct and
// cheaper than coordinating a shared in-flight future.
type TokenManager struct {
	path      string
	refresher SessionRefresher
	mu        sync.Mutex
}

// NewTokenManager creates a manager for the session stored at path. A nil
// refresher disables renewal: expired tokens then surface as
// StoredSessionExpiredError instead of being silently renewed.
func NewTokenManager(path string, refresher SessionRefresher) *TokenManager {
	return &TokenManager{path: path, refresher: refresher}
}

// AccessToken returns a usable access token for the stored session, renewing
// it first when it is expired or about to expire.
func (m *TokenManager) AccessToken(ctx context.Context) (string, error) {
	token, err := LoadToken(m.path)
	if err != nil {
		return "", &StoredSessionUnavailableError{Path: m.path, Cause: err}
	}
	if tokenUsable(token) {
		return token.AccessToken, nil
	}
	return m.renew(ctx)
}

// renew redeems the refresh credential and persists the rotated session.
func (m *TokenManager) renew(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check under the lock: another caller may have renewed while we
	// waited, in which case the credential we would present is already dead.
	token, err := LoadToken(m.path)
	if err != nil {
		return "", &StoredSessionUnavailableError{Path: m.path, Cause: err}
	}
	if tokenUsable(token) {
		return token.AccessToken, nil
	}

	if m.refresher == nil || token.RefreshToken == "" {
		return "", &StoredSessionExpiredError{Path: m.path}
	}

	renewed, err := m.refresher.Refresh(ctx, token.RefreshToken)
	if err != nil {
		return "", &StoredSessionRefreshFailedError{Path: m.path, Cause: err}
	}
	if renewed == nil || renewed.AccessToken == "" {
		return "", &StoredSessionRefreshFailedError{Path: m.path, Cause: &StoredSessionExpiredError{Path: m.path}}
	}

	// Persist the rotated pair. The server revoked the credential we just
	// presented, so failing to write it back would strand the session at the
	// next renewal — treat a write failure as a refresh failure.
	next := &TokenData{
		AccessToken:  renewed.AccessToken,
		RefreshToken: renewed.RefreshToken,
		ExpiresAt:    renewed.ExpiresAt,
		TokenType:    renewed.TokenType,
	}
	if next.RefreshToken == "" {
		// The server did not rotate (renewal reused the credential); keep the
		// one we hold so the session survives the next expiry.
		next.RefreshToken = token.RefreshToken
	}
	if next.TokenType == "" {
		next.TokenType = token.TokenType
	}
	if err := SaveToken(m.path, next); err != nil {
		return "", &StoredSessionRefreshFailedError{Path: m.path, Cause: err}
	}
	return next.AccessToken, nil
}

// tokenUsable reports whether the stored access token has enough life left to
// be attached to an outgoing request.
func tokenUsable(token *TokenData) bool {
	if token == nil || token.AccessToken == "" {
		return false
	}
	// A zero expiry means the envelope predates expiry tracking; treat it as
	// usable so an existing session is not needlessly discarded.
	if token.ExpiresAt.IsZero() {
		return true
	}
	return time.Now().Add(RefreshSkew).Before(token.ExpiresAt)
}
