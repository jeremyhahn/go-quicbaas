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

package orchestration

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

// AgentStore provides persistence operations for agent session data.
// Implementations may be backed by in-memory storage, a KV store, or
// other persistent backends.
type AgentStore interface {
	// PutSession persists a session record, creating or updating it.
	PutSession(ctx context.Context, record *SessionRecord) error

	// GetSession retrieves a session record by session ID.
	GetSession(ctx context.Context, sessionID string) (*SessionRecord, error)

	// DeleteSession removes a session record by session ID.
	DeleteSession(ctx context.Context, sessionID string) error

	// ListSessions returns all persisted session records.
	ListSessions(ctx context.Context) ([]*SessionRecord, error)

	// UpdateSessionStatus updates the status of a persisted session.
	UpdateSessionStatus(ctx context.Context, sessionID string, status SessionState) error

	// UpdateSessionActivity updates the last activity timestamp of a session.
	UpdateSessionActivity(ctx context.Context, sessionID string) error
}

// SessionRecord contains persisted metadata for an agent session.
type SessionRecord struct {
	SessionID    string       `json:"session_id"`
	ProjectID    string       `json:"project_id"`
	Backend      string       `json:"backend"`
	Model        string       `json:"model,omitempty"`
	WorkDir      string       `json:"work_dir"`
	Status       SessionState `json:"status"`
	CreatedAt    int64        `json:"created_at"`
	LastActivity int64        `json:"last_activity"`
}

// inMemoryAgentStore implements AgentStore using sync.Map for lock-free
// concurrent access. This is the default store used when no external
// persistence backend is configured.
type inMemoryAgentStore struct {
	sessions sync.Map // map[string][]byte (JSON-encoded SessionRecord)
	count    atomic.Int64
}

// NewInMemoryAgentStore creates a new in-memory agent store.
func NewInMemoryAgentStore() AgentStore {
	return &inMemoryAgentStore{}
}

// PutSession persists a session record in memory.
func (s *inMemoryAgentStore) PutSession(ctx context.Context, record *SessionRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if record == nil {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreInvalidInput,
			Message:  "session record must not be nil",
		}
	}
	if record.SessionID == "" {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreInvalidInput,
			Message:  "session ID must not be empty",
		}
	}

	data, err := json.Marshal(record)
	if err != nil {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreMarshal,
			Message:  "failed to marshal session record: " + err.Error(),
		}
	}

	_, loaded := s.sessions.LoadOrStore(record.SessionID, data)
	if loaded {
		// Update existing entry
		s.sessions.Store(record.SessionID, data)
	} else {
		s.count.Add(1)
	}

	return nil
}

// GetSession retrieves a session record from memory by session ID.
func (s *inMemoryAgentStore) GetSession(ctx context.Context, sessionID string) (*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreInvalidInput,
			Message:  "session ID must not be empty",
		}
	}

	val, ok := s.sessions.Load(sessionID)
	if !ok {
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreNotFound,
			Message:  "session not found: " + sessionID,
		}
	}

	data, ok := val.([]byte)
	if !ok {
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreMarshal,
			Message:  "session value is not []byte",
		}
	}
	var record SessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreMarshal,
			Message:  "failed to unmarshal session record: " + err.Error(),
		}
	}

	return &record, nil
}

// DeleteSession removes a session record from memory.
func (s *inMemoryAgentStore) DeleteSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if sessionID == "" {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreInvalidInput,
			Message:  "session ID must not be empty",
		}
	}

	_, loaded := s.sessions.LoadAndDelete(sessionID)
	if !loaded {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreNotFound,
			Message:  "session not found: " + sessionID,
		}
	}

	s.count.Add(-1)
	return nil
}

// ListSessions returns all persisted session records.
func (s *inMemoryAgentStore) ListSessions(ctx context.Context) ([]*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var records []*SessionRecord
	var unmarshalErr error

	s.sessions.Range(func(key, value interface{}) bool {
		if ctx.Err() != nil {
			return false
		}

		data, ok := value.([]byte)
		if !ok {
			return true // skip non-byte entries
		}
		var record SessionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			unmarshalErr = &AgentStoreError{
				Sentinel: ErrAgentStoreMarshal,
				Message:  "failed to unmarshal session record: " + err.Error(),
			}
			return false
		}
		records = append(records, &record)
		return true
	})

	if unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return records, nil
}

// UpdateSessionStatus updates the status field of a persisted session.
func (s *inMemoryAgentStore) UpdateSessionStatus(ctx context.Context, sessionID string, status SessionState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if sessionID == "" {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreInvalidInput,
			Message:  "session ID must not be empty",
		}
	}

	record, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	record.Status = status
	record.LastActivity = time.Now().UnixNano()

	return s.PutSession(ctx, record)
}

// UpdateSessionActivity updates the last activity timestamp of a session.
func (s *inMemoryAgentStore) UpdateSessionActivity(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if sessionID == "" {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreInvalidInput,
			Message:  "session ID must not be empty",
		}
	}

	record, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	record.LastActivity = time.Now().UnixNano()

	return s.PutSession(ctx, record)
}

// Compile-time interface satisfaction check.
var _ AgentStore = (*inMemoryAgentStore)(nil)
