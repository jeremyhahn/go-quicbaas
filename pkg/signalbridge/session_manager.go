// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"sync"
	"time"
)

// SessionManager owns the per-conversation attachment and verbosity
// state. It is a narrow wrapper over StateStore that enforces the
// invariant that a conversation has at most one attached session at a
// time.
type SessionManager struct {
	store      StateStore
	defaults   VerbosityPrefs

	mu    sync.Mutex
	cache map[string]*ConversationState
}

// NewSessionManager wraps store with an in-process cache layered in
// front of it. The cache is load-through: reads miss the store once
// per conversation, subsequent reads hit the cache.
func NewSessionManager(store StateStore, defaults VerbosityPrefs) (*SessionManager, error) {
	if store == nil {
		return nil, newError("signalbridge.NewSessionManager", ErrKindInvalidInput, "store is required")
	}
	m := &SessionManager{
		store:    store,
		defaults: defaults,
		cache:    make(map[string]*ConversationState),
	}
	// Warm the cache from existing persisted state so restart is a
	// drop-in resume.
	list, err := store.ListConversations()
	if err != nil {
		return nil, err
	}
	for _, rec := range list {
		m.cache[rec.ConversationID] = rec
	}
	return m, nil
}

// Get returns a snapshot of the conversation state, creating a fresh
// record with defaults when none exists.
func (m *SessionManager) Get(conversationID string, kind ConversationKind) *ConversationState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getLocked(conversationID, kind)
}

func (m *SessionManager) getLocked(conversationID string, kind ConversationKind) *ConversationState {
	if rec, ok := m.cache[conversationID]; ok {
		cp := *rec
		if rec.InvocationTimestamps != nil {
			cp.InvocationTimestamps = make(map[string]int64, len(rec.InvocationTimestamps))
			for k, v := range rec.InvocationTimestamps {
				cp.InvocationTimestamps[k] = v
			}
		}
		return &cp
	}
	fresh := &ConversationState{
		ConversationID:       conversationID,
		Kind:                 kind,
		Verbosity:            m.defaults,
		InvocationTimestamps: make(map[string]int64),
		UpdatedAt:            time.Now().UTC(),
	}
	m.cache[conversationID] = fresh
	// Persist asynchronously later via Save, not here: Get is a
	// read-like operation. Callers that mutate must call Save.
	cp := *fresh
	cp.InvocationTimestamps = map[string]int64{}
	return &cp
}

// Attach records that conversationID is now driving sessionID. Returns
// an ErrKindConflict error when a different session is already
// attached.
func (m *SessionManager) Attach(conversationID, sessionID string, kind ConversationKind, operator OperatorIdentity) error {
	const op = "signalbridge.SessionManager.Attach"
	if sessionID == "" {
		return newError(op, ErrKindInvalidInput, "sessionID required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.getLocked(conversationID, kind)
	if rec.AttachedSessionID != "" && rec.AttachedSessionID != sessionID {
		return &Error{Op: op, Kind: ErrKindConflict, Msg: "conversation already attached to session " + rec.AttachedSessionID}
	}
	rec.AttachedSessionID = sessionID
	rec.OperatorSubject = operator.SubjectID
	rec.OperatorTenant = operator.TenantID
	rec.UpdatedAt = time.Now().UTC()
	m.cache[conversationID] = rec
	return m.store.SaveConversation(rec)
}

// Detach clears the attachment and resets the rolling message marker.
// Verbosity preferences are retained.
func (m *SessionManager) Detach(conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.cache[conversationID]
	if !ok {
		return nil
	}
	rec.AttachedSessionID = ""
	rec.RollingMsgTimestamp = 0
	rec.InvocationTimestamps = make(map[string]int64)
	rec.UpdatedAt = time.Now().UTC()
	return m.store.SaveConversation(rec)
}

// SetVerbosity replaces the verbosity preferences for conversationID.
func (m *SessionManager) SetVerbosity(conversationID string, kind ConversationKind, prefs VerbosityPrefs) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.getLocked(conversationID, kind)
	rec.Verbosity = prefs
	m.cache[conversationID] = rec
	return m.store.SaveConversation(rec)
}

// UpdateRollingMessage records the timestamp of an in-progress
// assistant message. timestamp==0 clears the marker (finalizing the
// rolling message).
func (m *SessionManager) UpdateRollingMessage(conversationID string, kind ConversationKind, timestamp int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec := m.getLocked(conversationID, kind)
	rec.RollingMsgTimestamp = timestamp
	m.cache[conversationID] = rec
	return m.store.SaveConversation(rec)
}

// RecordInvocation maps an orchestrator invocation ID to the Signal
// timestamp that was used when the 🔧 line was sent. Used by the
// translator to reply-thread tool results.
func (m *SessionManager) RecordInvocation(conversationID, invocationID string, timestamp int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.cache[conversationID]
	if !ok {
		return nil
	}
	if rec.InvocationTimestamps == nil {
		rec.InvocationTimestamps = make(map[string]int64)
	}
	rec.InvocationTimestamps[invocationID] = timestamp
	rec.UpdatedAt = time.Now().UTC()
	return m.store.SaveConversation(rec)
}

// InvocationTimestamp returns the timestamp recorded by RecordInvocation.
func (m *SessionManager) InvocationTimestamp(conversationID, invocationID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.cache[conversationID]
	if !ok {
		return 0
	}
	return rec.InvocationTimestamps[invocationID]
}

// AdvanceWatermark records the highest event sequence successfully
// forwarded to a conversation, underpinning exactly-once delivery on
// restart.
func (m *SessionManager) AdvanceWatermark(conversationID string, seq uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.cache[conversationID]
	if !ok {
		return nil
	}
	if seq <= rec.LastDeliveredSeq {
		return nil
	}
	rec.LastDeliveredSeq = seq
	rec.UpdatedAt = time.Now().UTC()
	return m.store.SaveConversation(rec)
}

// ConversationsForSession returns all conversations currently
// attached to sessionID. Used by the event forwarder to fan out.
func (m *SessionManager) ConversationsForSession(sessionID string) []*ConversationState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*ConversationState, 0)
	for _, rec := range m.cache {
		if rec.AttachedSessionID == sessionID {
			cp := *rec
			out = append(out, &cp)
		}
	}
	return out
}

// AttachedSessions returns a deduplicated list of session IDs the bot
// currently tracks. Used at startup to resubscribe on restart.
func (m *SessionManager) AttachedSessions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := make(map[string]struct{})
	for _, rec := range m.cache {
		if rec.AttachedSessionID != "" {
			seen[rec.AttachedSessionID] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	return out
}
