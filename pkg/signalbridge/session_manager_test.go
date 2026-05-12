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
	"path/filepath"
	"testing"
)

// TestSessionManager_Attach verifies the attach-detach-reattach flow
// and rejects conflicting attach attempts.
func TestSessionManager_Attach(t *testing.T) {
	mgr, err := NewSessionManager(NewMemoryStateStore(), DefaultVerbosityPrefs())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	op := OperatorIdentity{TenantID: "t", SubjectID: "u"}

	if err := mgr.Attach("c1", "sess-A", ConversationDM, op); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	if err := mgr.Attach("c1", "sess-A", ConversationDM, op); err != nil {
		t.Fatalf("idempotent reattach: %v", err)
	}
	if err := mgr.Attach("c1", "sess-B", ConversationDM, op); err == nil {
		t.Fatalf("expected conflict on different session")
	} else if !IsKind(err, ErrKindConflict) {
		t.Fatalf("want ErrKindConflict, got %v", err)
	}
	if err := mgr.Detach("c1"); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if err := mgr.Attach("c1", "sess-B", ConversationDM, op); err != nil {
		t.Fatalf("attach after detach: %v", err)
	}
	state := mgr.Get("c1", ConversationDM)
	if state.AttachedSessionID != "sess-B" {
		t.Fatalf("expected sess-B, got %q", state.AttachedSessionID)
	}
}

// TestSessionManager_Attach_Errors covers bad inputs.
func TestSessionManager_Attach_Errors(t *testing.T) {
	mgr, err := NewSessionManager(NewMemoryStateStore(), DefaultVerbosityPrefs())
	if err != nil {
		t.Fatalf("mgr: %v", err)
	}
	if err := mgr.Attach("c", "", ConversationDM, OperatorIdentity{}); err == nil {
		t.Fatalf("expected error on empty sessionID")
	}
}

// TestSessionManager_Watermark advances only forward.
func TestSessionManager_Watermark(t *testing.T) {
	store := NewMemoryStateStore()
	mgr, err := NewSessionManager(store, DefaultVerbosityPrefs())
	if err != nil {
		t.Fatalf("mgr: %v", err)
	}
	op := OperatorIdentity{TenantID: "t", SubjectID: "u"}
	if err := mgr.Attach("c", "sess", ConversationDM, op); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := mgr.AdvanceWatermark("c", 5); err != nil {
		t.Fatalf("advance 5: %v", err)
	}
	if err := mgr.AdvanceWatermark("c", 3); err != nil {
		t.Fatalf("lower shouldn't error: %v", err)
	}
	if mgr.Get("c", ConversationDM).LastDeliveredSeq != 5 {
		t.Fatalf("watermark regressed: %d", mgr.Get("c", ConversationDM).LastDeliveredSeq)
	}
}

// TestSessionManager_ConversationsForSession returns only matching
// conversations.
func TestSessionManager_ConversationsForSession(t *testing.T) {
	mgr, _ := NewSessionManager(NewMemoryStateStore(), DefaultVerbosityPrefs())
	op := OperatorIdentity{TenantID: "t", SubjectID: "u"}
	_ = mgr.Attach("c1", "sess-1", ConversationDM, op)
	_ = mgr.Attach("c2", "sess-1", ConversationGroup, op)
	_ = mgr.Attach("c3", "sess-2", ConversationDM, op)
	got := mgr.ConversationsForSession("sess-1")
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
}

// TestSessionManager_SetVerbosity replaces preferences.
func TestSessionManager_SetVerbosity(t *testing.T) {
	mgr, _ := NewSessionManager(NewMemoryStateStore(), DefaultVerbosityPrefs())
	custom := VerbosityPrefs{StreamTokens: false, ToolDetails: false}
	if err := mgr.SetVerbosity("c", ConversationDM, custom); err != nil {
		t.Fatalf("set: %v", err)
	}
	got := mgr.Get("c", ConversationDM)
	if got.Verbosity.StreamTokens != false {
		t.Fatalf("not applied: %+v", got.Verbosity)
	}
}

// TestNewSessionManager_Errors on nil store.
func TestNewSessionManager_Errors(t *testing.T) {
	if _, err := NewSessionManager(nil, VerbosityPrefs{}); err == nil {
		t.Fatalf("expected error on nil store")
	}
}

// TestFileStateStore_RoundTrip verifies JSON persistence.
func TestFileStateStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := NewFileStateStore(path, true)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	want := &ConversationState{
		ConversationID:   "c",
		Kind:             ConversationDM,
		AttachedSessionID: "sess",
		LastDeliveredSeq: 42,
		Verbosity:        DefaultVerbosityPrefs(),
	}
	if err := s.SaveConversation(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	acct := &SignalAccount{ACI: "aci-1", DeviceID: 1, Number: "+15551234567"}
	if err := s.SaveAccount(acct); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and verify.
	s2, err := NewFileStateStore(path, true)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := s2.LoadConversation("c")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.LastDeliveredSeq != 42 || got.AttachedSessionID != "sess" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	acct2, err := s2.LoadAccount()
	if err != nil {
		t.Fatalf("load account: %v", err)
	}
	if acct2.ACI != "aci-1" {
		t.Fatalf("account aci mismatch: %+v", acct2)
	}
}

// TestFileStateStore_NotFound returns ErrKindNotFound on missing
// conversation or account.
func TestFileStateStore_NotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStateStore(filepath.Join(dir, "s.json"), true)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := s.LoadConversation("missing"); !IsKind(err, ErrKindNotFound) {
		t.Fatalf("want ErrKindNotFound got %v", err)
	}
	if _, err := s.LoadAccount(); !IsKind(err, ErrKindNotFound) {
		t.Fatalf("want ErrKindNotFound got %v", err)
	}
}

// TestFileStateStore_InvalidPath surfaces storage errors.
func TestFileStateStore_InvalidPath(t *testing.T) {
	if _, err := NewFileStateStore("", true); err == nil {
		t.Fatalf("expected error on empty path")
	}
}

// TestMemoryStateStore_RoundTrip mirrors the file test for memory mode.
func TestMemoryStateStore_RoundTrip(t *testing.T) {
	s := NewMemoryStateStore()
	want := &ConversationState{
		ConversationID:   "c",
		LastDeliveredSeq: 7,
		Verbosity:        DefaultVerbosityPrefs(),
	}
	if err := s.SaveConversation(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.LoadConversation("c")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.LastDeliveredSeq != 7 {
		t.Fatalf("mismatch: %+v", got)
	}
}

// TestMemoryStateStore_Errors on nil state.
func TestMemoryStateStore_Errors(t *testing.T) {
	s := NewMemoryStateStore()
	if err := s.SaveConversation(nil); err == nil {
		t.Fatalf("expected error on nil state")
	}
	if err := s.SaveAccount(nil); err == nil {
		t.Fatalf("expected error on nil account")
	}
}
