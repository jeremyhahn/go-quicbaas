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
	"testing"
)

// TestFileStateStore_ReopenAndCopyIsolation persists conversation,
// account, and a populated invocation map, reopens on the same path, and
// verifies the returned records are deep copies that cannot corrupt the
// store when mutated by the caller.
func TestFileStateStore_ReopenAndCopyIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	store, err := NewFileStateStore(path, true)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	conv := &ConversationState{
		ConversationID:       "conv-1",
		Kind:                 ConversationDM,
		AttachedSessionID:    "sess-1",
		InvocationTimestamps: map[string]int64{"inv-1": 99},
	}
	if err := store.SaveConversation(conv); err != nil {
		t.Fatalf("save conv: %v", err)
	}
	if err := store.SaveAccount(&SignalAccount{ACI: "aci-1", Password: "pw"}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := NewFileStateStore(path, true)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := reopened.LoadConversation("conv-1")
	if err != nil {
		t.Fatalf("load conv: %v", err)
	}
	if got.AttachedSessionID != "sess-1" || got.InvocationTimestamps["inv-1"] != 99 {
		t.Fatalf("loaded conv wrong: %+v", got)
	}
	got.InvocationTimestamps["inv-1"] = 0
	again, _ := reopened.LoadConversation("conv-1")
	if again.InvocationTimestamps["inv-1"] != 99 {
		t.Fatalf("store record was mutated through returned copy")
	}
	acct, err := reopened.LoadAccount()
	if err != nil || acct.ACI != "aci-1" {
		t.Fatalf("account = %+v err %v", acct, err)
	}
	list, err := reopened.ListConversations()
	if err != nil || len(list) != 1 {
		t.Fatalf("list len = %d err %v", len(list), err)
	}
}

// TestFileStateStore_LoadEmptyFile treats a zero-byte file as empty.
func TestFileStateStore_LoadEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewFileStateStore(path, false); err != nil {
		t.Fatalf("empty file should load cleanly: %v", err)
	}
}

// TestFileStateStore_LoadCorruptFile surfaces a storage decode error.
func TestFileStateStore_LoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewFileStateStore(path, false); !IsKind(err, ErrKindStorage) {
		t.Fatalf("want storage, got %v", err)
	}
}

// TestFileStateStore_LoadVersionMismatch discards state from an older
// schema version rather than refusing to open.
func TestFileStateStore_LoadVersionMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.json")
	old := persistedState{
		Version:       persistedStateVersion + 1,
		Conversations: map[string]*ConversationState{"c": {ConversationID: "c"}},
		Account:       &SignalAccount{ACI: "stale"},
	}
	data, _ := json.Marshal(&old)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store, err := NewFileStateStore(path, false)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := store.LoadConversation("c"); !IsKind(err, ErrKindNotFound) {
		t.Fatalf("stale conversation should be discarded, got %v", err)
	}
	if _, err := store.LoadAccount(); !IsKind(err, ErrKindNotFound) {
		t.Fatalf("stale account should be discarded, got %v", err)
	}
}

// TestFileStateStore_SaveConversation_Invalid rejects nil and empty IDs.
func TestFileStateStore_SaveConversation_Invalid(t *testing.T) {
	store, _ := NewFileStateStore(filepath.Join(t.TempDir(), "s.json"), true)
	if err := store.SaveConversation(nil); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("nil: want invalid_input, got %v", err)
	}
	if err := store.SaveConversation(&ConversationState{}); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("empty id: want invalid_input, got %v", err)
	}
}

// TestFileStateStore_SaveAccount_Invalid rejects nil.
func TestFileStateStore_SaveAccount_Invalid(t *testing.T) {
	store, _ := NewFileStateStore(filepath.Join(t.TempDir(), "s.json"), true)
	if err := store.SaveAccount(nil); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid_input, got %v", err)
	}
}

// TestFileStateStore_BatchModeFlushesOnClose verifies that with
// flushOnWrite=false nothing hits disk until Close.
func TestFileStateStore_BatchModeFlushesOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "batch.json")
	store, err := NewFileStateStore(path, false)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := store.SaveConversation(&ConversationState{ConversationID: "c"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("file should not exist before Close")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("file should exist after Close: %v", statErr)
	}
}

// TestFileStateStore_FlushNotDirty is a no-op on a clean store.
func TestFileStateStore_FlushNotDirty(t *testing.T) {
	store, _ := NewFileStateStore(filepath.Join(t.TempDir(), "clean.json"), false)
	if err := store.Close(); err != nil {
		t.Fatalf("close clean store: %v", err)
	}
}

// TestFileStateStore_ListConversations enumerates persisted records.
func TestFileStateStore_ListConversations(t *testing.T) {
	store, _ := NewFileStateStore(filepath.Join(t.TempDir(), "list.json"), true)
	for _, id := range []string{"a", "b", "c"} {
		if err := store.SaveConversation(&ConversationState{ConversationID: id}); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}
	list, err := store.ListConversations()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}
}

// TestMemoryStateStore_CopyIsolation ensures returned records are copies.
func TestMemoryStateStore_CopyIsolation(t *testing.T) {
	store := NewMemoryStateStore()
	conv := &ConversationState{ConversationID: "c", InvocationTimestamps: map[string]int64{"i": 1}}
	if err := store.SaveConversation(conv); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.LoadConversation("c")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got.InvocationTimestamps["i"] = 0
	again, _ := store.LoadConversation("c")
	if again.InvocationTimestamps["i"] != 1 {
		t.Fatalf("memory store record mutated through copy")
	}
	list, err := store.ListConversations()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %d %v", len(list), err)
	}
}
