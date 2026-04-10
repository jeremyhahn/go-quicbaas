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

package websocket

import (
	"sync"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// SessionManager manages WebSocket sessions using a sharded design for low lock contention.
// Uses 256 shards (matching FastStateMachine and watch.Manager patterns).
type SessionManager struct {
	shards       [256]*sessionShard
	sessionCount uint64 // Atomic count of active sessions
}

// sessionShard holds a subset of sessions to reduce lock contention.
type sessionShard struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewSessionManager creates a new session manager with pre-initialized shards.
func NewSessionManager() *SessionManager {
	sm := &SessionManager{}

	for i := range sm.shards {
		sm.shards[i] = &sessionShard{
			sessions: make(map[string]*Session),
		}
	}

	return sm
}

// Add registers a new session.
func (sm *SessionManager) Add(session *Session) {
	shard := sm.getShard(session.ID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.sessions[session.ID] = session
	atomic.AddUint64(&sm.sessionCount, 1)
}

// Remove removes a session by ID.
func (sm *SessionManager) Remove(sessionID string) {
	shard := sm.getShard(sessionID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, exists := shard.sessions[sessionID]; exists {
		delete(shard.sessions, sessionID)
		atomic.AddUint64(&sm.sessionCount, ^uint64(0)) // Atomic decrement
	}
}

// Get retrieves a session by ID.
func (sm *SessionManager) Get(sessionID string) (*Session, bool) {
	shard := sm.getShard(sessionID)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	session, exists := shard.sessions[sessionID]
	return session, exists
}

// Count returns the number of active sessions.
func (sm *SessionManager) Count() uint64 {
	return atomic.LoadUint64(&sm.sessionCount)
}

// ForEach iterates over all sessions and calls the provided function.
// If the function returns false, iteration stops.
// Note: This locks each shard sequentially, so use sparingly.
func (sm *SessionManager) ForEach(fn func(*Session) bool) {
	for i := range sm.shards {
		shard := sm.shards[i]
		shard.mu.RLock()

		for _, session := range shard.sessions {
			if !fn(session) {
				shard.mu.RUnlock()
				return
			}
		}

		shard.mu.RUnlock()
	}
}

// CloseAll closes all active sessions.
func (sm *SessionManager) CloseAll() {
	for i := range sm.shards {
		shard := sm.shards[i]
		shard.mu.Lock()

		for _, session := range shard.sessions {
			// Best effort: close errors are non-fatal during shutdown
			if err := session.Close(); err != nil {
				continue
			}
		}
		shard.sessions = make(map[string]*Session)

		shard.mu.Unlock()
	}

	atomic.StoreUint64(&sm.sessionCount, 0)
}

// GetByUserID returns all sessions for a given user ID.
// This is O(n) as it must scan all shards.
func (sm *SessionManager) GetByUserID(userID string) []*Session {
	var sessions []*Session

	for i := range sm.shards {
		shard := sm.shards[i]
		shard.mu.RLock()

		for _, session := range shard.sessions {
			if session.UserID == userID {
				sessions = append(sessions, session)
			}
		}

		shard.mu.RUnlock()
	}

	return sessions
}

// GetBySubprotocol returns all sessions using a given subprotocol.
// This is O(n) as it must scan all shards.
func (sm *SessionManager) GetBySubprotocol(subprotocol string) []*Session {
	var sessions []*Session

	for i := range sm.shards {
		shard := sm.shards[i]
		shard.mu.RLock()

		for _, session := range shard.sessions {
			if session.Subprotocol == subprotocol {
				sessions = append(sessions, session)
			}
		}

		shard.mu.RUnlock()
	}

	return sessions
}

// GetSubscribersForChannel returns all sessions subscribed to a channel.
// This is O(n) as it must scan all shards.
func (sm *SessionManager) GetSubscribersForChannel(channel string) []*Session {
	var sessions []*Session

	for i := range sm.shards {
		shard := sm.shards[i]
		shard.mu.RLock()

		for _, session := range shard.sessions {
			if session.IsSubscribed(channel) {
				sessions = append(sessions, session)
			}
		}

		shard.mu.RUnlock()
	}

	return sessions
}

// getShard returns the shard for a given session ID using FNV-1a hash.
func (sm *SessionManager) getShard(sessionID string) *sessionShard {
	hash := fnv1aString(sessionID)
	return sm.shards[hash]
}

// fnv1aString implements the FNV-1a hash function for strings.
// Returns a shard index in the range [0, 255].
func fnv1aString(s string) uint8 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	hash := uint32(offset32)
	for i := 0; i < len(s); i++ {
		hash ^= uint32(s[i])
		hash *= prime32
	}
	return conv.ClampUint32ToUint8(hash % 256)
}

// SessionStats holds session statistics.
type SessionStats struct {
	TotalSessions      uint64
	SessionsByProtocol map[string]int
}

// GetStats returns session statistics.
func (sm *SessionManager) GetStats() SessionStats {
	stats := SessionStats{
		TotalSessions:      atomic.LoadUint64(&sm.sessionCount),
		SessionsByProtocol: make(map[string]int),
	}

	for i := range sm.shards {
		shard := sm.shards[i]
		shard.mu.RLock()

		for _, session := range shard.sessions {
			stats.SessionsByProtocol[session.Subprotocol]++
		}

		shard.mu.RUnlock()
	}

	return stats
}
