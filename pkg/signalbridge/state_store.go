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
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StateStore persists per-conversation state so the bot can resume
// delivery watermarks across restarts. The initial implementation is
// in-memory with optional JSON file persistence; a KV-backed
// implementation can slot in later by satisfying this interface.
type StateStore interface {
	LoadConversation(conversationID string) (*ConversationState, error)
	SaveConversation(state *ConversationState) error
	ListConversations() ([]*ConversationState, error)
	LoadAccount() (*SignalAccount, error)
	SaveAccount(account *SignalAccount) error
	Close() error
}

// FileStateStore is a JSON-on-disk StateStore. All reads/writes are
// serialized under a single mutex — the bot does not need high
// throughput on state reads; correctness is the priority.
type FileStateStore struct {
	mu             sync.Mutex
	path           string
	conversations  map[string]*ConversationState
	account        *SignalAccount
	dirty          bool
	flushOnWrite   bool
}

// NewFileStateStore opens (or creates) a state file at path. When
// flushOnWrite is true every Save* flushes to disk synchronously.
// Callers that want batch-mode can set flushOnWrite=false and invoke
// Close (or a future explicit Flush) before shutdown.
func NewFileStateStore(path string, flushOnWrite bool) (*FileStateStore, error) {
	const op = "signalbridge.NewFileStateStore"
	if path == "" {
		return nil, newError(op, ErrKindInvalidInput, "path is required")
	}
	store := &FileStateStore{
		path:          path,
		conversations: make(map[string]*ConversationState),
		flushOnWrite:  flushOnWrite,
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

// persistedState is the on-disk shape. A bump in Version invalidates
// existing files gracefully — the loader starts fresh rather than
// refusing to open.
type persistedState struct {
	Version       int                           `json:"version"`
	UpdatedAt     time.Time                     `json:"updated_at"`
	Account       *SignalAccount                `json:"account,omitempty"`
	Conversations map[string]*ConversationState `json:"conversations"`
}

const persistedStateVersion = 1

// load reads the backing file into memory. Missing file is not an
// error — we treat it as an empty store.
func (s *FileStateStore) load() error {
	const op = "signalbridge.FileStateStore.load"
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return wrapError(op, ErrKindStorage, err, "read state file")
	}
	if len(data) == 0 {
		return nil
	}
	var ps persistedState
	if err := json.Unmarshal(data, &ps); err != nil {
		return wrapError(op, ErrKindStorage, err, "decode state file")
	}
	if ps.Version != persistedStateVersion {
		// Silent reset; persisted state from an older version is
		// discarded rather than attempted to be migrated in place.
		return nil
	}
	if ps.Conversations != nil {
		s.conversations = ps.Conversations
	}
	if ps.Account != nil {
		s.account = ps.Account
	}
	return nil
}

// flush writes the in-memory state to disk. Called eagerly under
// flushOnWrite and always on Close.
func (s *FileStateStore) flush() error {
	const op = "signalbridge.FileStateStore.flush"
	if !s.dirty {
		return nil
	}
	ps := persistedState{
		Version:       persistedStateVersion,
		UpdatedAt:     time.Now().UTC(),
		Account:       s.account,
		Conversations: s.conversations,
	}
	data, err := json.MarshalIndent(&ps, "", "  ")
	if err != nil {
		return wrapError(op, ErrKindStorage, err, "encode state file")
	}
	dir := filepath.Dir(s.path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return wrapError(op, ErrKindStorage, err, "mkdir state directory")
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return wrapError(op, ErrKindStorage, err, "write state file")
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return wrapError(op, ErrKindStorage, err, "rename state file")
	}
	s.dirty = false
	return nil
}

// LoadConversation returns a shallow copy of the persisted record, or
// an Error with Kind==ErrKindNotFound if absent.
func (s *FileStateStore) LoadConversation(conversationID string) (*ConversationState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.conversations[conversationID]
	if !ok {
		return nil, &Error{Op: "signalbridge.FileStateStore.LoadConversation", Kind: ErrKindNotFound, Msg: conversationID}
	}
	cp := *rec
	if rec.InvocationTimestamps != nil {
		cp.InvocationTimestamps = make(map[string]int64, len(rec.InvocationTimestamps))
		for k, v := range rec.InvocationTimestamps {
			cp.InvocationTimestamps[k] = v
		}
	}
	return &cp, nil
}

// SaveConversation upserts the record and optionally flushes.
func (s *FileStateStore) SaveConversation(state *ConversationState) error {
	const op = "signalbridge.FileStateStore.SaveConversation"
	if state == nil || state.ConversationID == "" {
		return newError(op, ErrKindInvalidInput, "state is required and must have ConversationID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state.UpdatedAt = time.Now().UTC()
	s.conversations[state.ConversationID] = state
	s.dirty = true
	if s.flushOnWrite {
		return s.flush()
	}
	return nil
}

// ListConversations returns copies of all persisted records.
func (s *FileStateStore) ListConversations() ([]*ConversationState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ConversationState, 0, len(s.conversations))
	for _, rec := range s.conversations {
		cp := *rec
		out = append(out, &cp)
	}
	return out, nil
}

// LoadAccount returns the persisted account or ErrKindNotFound.
func (s *FileStateStore) LoadAccount() (*SignalAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.account == nil {
		return nil, &Error{Op: "signalbridge.FileStateStore.LoadAccount", Kind: ErrKindNotFound, Msg: "no account on file"}
	}
	cp := *s.account
	return &cp, nil
}

// SaveAccount upserts the account record.
func (s *FileStateStore) SaveAccount(account *SignalAccount) error {
	const op = "signalbridge.FileStateStore.SaveAccount"
	if account == nil {
		return newError(op, ErrKindInvalidInput, "account is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *account
	s.account = &cp
	s.dirty = true
	if s.flushOnWrite {
		return s.flush()
	}
	return nil
}

// Close flushes pending writes and releases the file.
func (s *FileStateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush()
}

// =============================================================================
// MemoryStateStore — simpler, used in tests.
// =============================================================================

// MemoryStateStore is a pure in-memory StateStore with no file
// persistence. Created via NewMemoryStateStore. Safe for concurrent
// use.
type MemoryStateStore struct {
	mu            sync.Mutex
	conversations map[string]*ConversationState
	account       *SignalAccount
}

// NewMemoryStateStore constructs an empty in-memory store.
func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{conversations: make(map[string]*ConversationState)}
}

// LoadConversation implements StateStore.
func (s *MemoryStateStore) LoadConversation(conversationID string) (*ConversationState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.conversations[conversationID]
	if !ok {
		return nil, &Error{Op: "signalbridge.MemoryStateStore.LoadConversation", Kind: ErrKindNotFound, Msg: conversationID}
	}
	cp := *rec
	if rec.InvocationTimestamps != nil {
		cp.InvocationTimestamps = make(map[string]int64, len(rec.InvocationTimestamps))
		for k, v := range rec.InvocationTimestamps {
			cp.InvocationTimestamps[k] = v
		}
	}
	return &cp, nil
}

// SaveConversation implements StateStore.
func (s *MemoryStateStore) SaveConversation(state *ConversationState) error {
	if state == nil || state.ConversationID == "" {
		return newError("signalbridge.MemoryStateStore.SaveConversation", ErrKindInvalidInput, "state required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state.UpdatedAt = time.Now().UTC()
	s.conversations[state.ConversationID] = state
	return nil
}

// ListConversations implements StateStore.
func (s *MemoryStateStore) ListConversations() ([]*ConversationState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ConversationState, 0, len(s.conversations))
	for _, r := range s.conversations {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

// LoadAccount implements StateStore.
func (s *MemoryStateStore) LoadAccount() (*SignalAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.account == nil {
		return nil, &Error{Op: "signalbridge.MemoryStateStore.LoadAccount", Kind: ErrKindNotFound}
	}
	cp := *s.account
	return &cp, nil
}

// SaveAccount implements StateStore.
func (s *MemoryStateStore) SaveAccount(account *SignalAccount) error {
	if account == nil {
		return newError("signalbridge.MemoryStateStore.SaveAccount", ErrKindInvalidInput, "account required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *account
	s.account = &cp
	return nil
}

// Close implements StateStore.
func (s *MemoryStateStore) Close() error { return nil }

// Compile-time interface compliance.
var (
	_ StateStore = (*FileStateStore)(nil)
	_ StateStore = (*MemoryStateStore)(nil)
)
