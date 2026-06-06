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
	"errors"
	"testing"
)

// flakyStore is a StateStore whose ListConversations and SaveConversation
// can be made to fail. Save failures are surfaced by the SessionManager so
// the caller learns a state mutation was not durably persisted.
type flakyStore struct {
	MemoryStateStore
	failList bool
	failSave bool
	listRecs []*ConversationState
}

func (s *flakyStore) ListConversations() ([]*ConversationState, error) {
	if s.failList {
		return nil, errors.New("list boom")
	}
	if s.listRecs != nil {
		return s.listRecs, nil
	}
	return s.MemoryStateStore.ListConversations()
}

func (s *flakyStore) SaveConversation(state *ConversationState) error {
	if s.failSave {
		return errors.New("save boom")
	}
	return s.MemoryStateStore.SaveConversation(state)
}

func newFlakyStore() *flakyStore {
	return &flakyStore{MemoryStateStore: *NewMemoryStateStore()}
}

// TestNewSessionManager_ListError propagates a store list failure.
func TestNewSessionManager_ListError(t *testing.T) {
	store := newFlakyStore()
	store.failList = true
	if _, err := NewSessionManager(store, DefaultVerbosityPrefs()); err == nil {
		t.Fatalf("expected error when store.ListConversations fails")
	}
}

// TestNewSessionManager_WarmsCache loads persisted conversations into the
// in-memory cache at construction so a restart resumes seamlessly.
func TestNewSessionManager_WarmsCache(t *testing.T) {
	store := newFlakyStore()
	store.listRecs = []*ConversationState{
		{ConversationID: "warm", AttachedSessionID: "sess-w", Kind: ConversationDM},
	}
	mgr, err := NewSessionManager(store, DefaultVerbosityPrefs())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	if mgr.Get("warm", ConversationDM).AttachedSessionID != "sess-w" {
		t.Fatalf("cache not warmed from persisted state")
	}
	if got := mgr.AttachedSessions(); len(got) != 1 || got[0] != "sess-w" {
		t.Fatalf("AttachedSessions = %v", got)
	}
}

// TestSessionManager_Attach_SaveError surfaces a persistence failure.
func TestSessionManager_Attach_SaveError(t *testing.T) {
	store := newFlakyStore()
	store.failSave = true
	mgr, err := NewSessionManager(store, DefaultVerbosityPrefs())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	if err := mgr.Attach("c", "sess", ConversationDM, OperatorIdentity{}); err == nil {
		t.Fatalf("expected save error to surface")
	}
}

// TestSessionManager_Detach_SaveError surfaces a persistence failure when
// an attached conversation exists.
func TestSessionManager_Detach_SaveError(t *testing.T) {
	store := newFlakyStore()
	mgr, err := NewSessionManager(store, DefaultVerbosityPrefs())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	if err := mgr.Attach("c", "sess", ConversationDM, OperatorIdentity{}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	store.failSave = true
	if err := mgr.Detach("c"); err == nil {
		t.Fatalf("expected detach save error to surface")
	}
}

// TestSessionManager_Detach_Unknown is a no-op for an unknown conversation.
func TestSessionManager_Detach_Unknown(t *testing.T) {
	mgr, _ := NewSessionManager(NewMemoryStateStore(), DefaultVerbosityPrefs())
	if err := mgr.Detach("never-seen"); err != nil {
		t.Fatalf("detach unknown should be nil, got %v", err)
	}
}

// TestSessionManager_RecordAndReadInvocation round-trips an invocation
// timestamp and tolerates unknown conversations.
func TestSessionManager_RecordAndReadInvocation(t *testing.T) {
	mgr, _ := NewSessionManager(NewMemoryStateStore(), DefaultVerbosityPrefs())
	// Unknown conversation: record is a no-op, read returns 0.
	if err := mgr.RecordInvocation("unknown", "inv", 5); err != nil {
		t.Fatalf("record unknown should be nil: %v", err)
	}
	if got := mgr.InvocationTimestamp("unknown", "inv"); got != 0 {
		t.Fatalf("unknown invocation timestamp = %d, want 0", got)
	}
	// Attach to populate the cache, then record + read.
	if err := mgr.Attach("c", "sess", ConversationDM, OperatorIdentity{}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := mgr.RecordInvocation("c", "inv-1", 123); err != nil {
		t.Fatalf("record: %v", err)
	}
	if got := mgr.InvocationTimestamp("c", "inv-1"); got != 123 {
		t.Fatalf("invocation timestamp = %d, want 123", got)
	}
}

// TestSessionManager_RecordInvocation_SaveError surfaces a persistence
// failure.
func TestSessionManager_RecordInvocation_SaveError(t *testing.T) {
	store := newFlakyStore()
	mgr, _ := NewSessionManager(store, DefaultVerbosityPrefs())
	if err := mgr.Attach("c", "sess", ConversationDM, OperatorIdentity{}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	store.failSave = true
	if err := mgr.RecordInvocation("c", "inv", 1); err == nil {
		t.Fatalf("expected save error to surface")
	}
}

// TestSessionManager_AdvanceWatermark_Unknown and stale sequences are
// no-ops.
func TestSessionManager_AdvanceWatermark_NoOps(t *testing.T) {
	mgr, _ := NewSessionManager(NewMemoryStateStore(), DefaultVerbosityPrefs())
	if err := mgr.AdvanceWatermark("unknown", 5); err != nil {
		t.Fatalf("unknown conversation should be nil: %v", err)
	}
	if err := mgr.Attach("c", "sess", ConversationDM, OperatorIdentity{}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := mgr.AdvanceWatermark("c", 10); err != nil {
		t.Fatalf("advance: %v", err)
	}
	// Stale (<=) sequence is a no-op even with a failing store, because no
	// Save is attempted.
	if err := mgr.AdvanceWatermark("c", 10); err != nil {
		t.Fatalf("stale advance should be nil: %v", err)
	}
}

// TestSessionManager_AdvanceWatermark_SaveError surfaces a persistence
// failure on a real advance.
func TestSessionManager_AdvanceWatermark_SaveError(t *testing.T) {
	store := newFlakyStore()
	mgr, _ := NewSessionManager(store, DefaultVerbosityPrefs())
	if err := mgr.Attach("c", "sess", ConversationDM, OperatorIdentity{}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	store.failSave = true
	if err := mgr.AdvanceWatermark("c", 99); err == nil {
		t.Fatalf("expected save error to surface")
	}
}

// TestSessionManager_UpdateRollingMessage records and clears the marker.
func TestSessionManager_UpdateRollingMessage(t *testing.T) {
	mgr, _ := NewSessionManager(NewMemoryStateStore(), DefaultVerbosityPrefs())
	if err := mgr.UpdateRollingMessage("c", ConversationDM, 42); err != nil {
		t.Fatalf("update: %v", err)
	}
	if mgr.Get("c", ConversationDM).RollingMsgTimestamp != 42 {
		t.Fatalf("rolling marker not set")
	}
	if err := mgr.UpdateRollingMessage("c", ConversationDM, 0); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if mgr.Get("c", ConversationDM).RollingMsgTimestamp != 0 {
		t.Fatalf("rolling marker not cleared")
	}
}
