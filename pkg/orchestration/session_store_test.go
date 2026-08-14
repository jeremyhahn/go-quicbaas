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
	"errors"
	"testing"
)

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewInMemoryAgentStore(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

// =============================================================================
// PutSession Tests
// =============================================================================

func TestInMemoryAgentStore_PutSession(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	record := &SessionRecord{
		SessionID: "session-1",
		ProjectID: "proj-1",
		Backend:   "mock",
		Model:     "llama3",
		Status:    SessionStateActive,
		CreatedAt: 1000,
	}

	err := store.PutSession(context.Background(), record)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify it was stored
	got, err := store.GetSession(context.Background(), "", "session-1")
	if err != nil {
		t.Fatalf("expected no error on get, got %v", err)
	}
	if got.SessionID != "session-1" {
		t.Errorf("expected session ID 'session-1', got %q", got.SessionID)
	}
	if got.Backend != "mock" {
		t.Errorf("expected backend 'mock', got %q", got.Backend)
	}
}

func TestInMemoryAgentStore_PutSession_NilRecord(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	err := store.PutSession(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil record")
	}
	if !errors.Is(err, ErrAgentStoreInvalidInput) {
		t.Fatalf("expected ErrAgentStoreInvalidInput, got %v", err)
	}
}

func TestInMemoryAgentStore_PutSession_EmptySessionID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	err := store.PutSession(context.Background(), &SessionRecord{SessionID: ""})
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
	if !errors.Is(err, ErrAgentStoreInvalidInput) {
		t.Fatalf("expected ErrAgentStoreInvalidInput, got %v", err)
	}
}

func TestInMemoryAgentStore_PutSession_Update(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	record := &SessionRecord{
		SessionID: "session-1",
		ProjectID: "proj-1",
		Backend:   "mock",
		Status:    SessionStateActive,
		CreatedAt: 1000,
	}

	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("failed first put: %v", err)
	}

	// Update the record
	record.Status = SessionStateClosed
	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("failed update put: %v", err)
	}

	got, err := store.GetSession(context.Background(), "", "session-1")
	if err != nil {
		t.Fatalf("failed get: %v", err)
	}
	if got.Status != SessionStateClosed {
		t.Errorf("expected closed status, got %q", got.Status)
	}
}

func TestInMemoryAgentStore_PutSession_CancelledContext(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.PutSession(ctx, &SessionRecord{SessionID: "session-1"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// GetSession Tests
// =============================================================================

func TestInMemoryAgentStore_GetSession(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	record := &SessionRecord{
		SessionID:    "session-get",
		ProjectID:    "proj-1",
		Backend:      "mock",
		Model:        "llama3",
		WorkDir:      "/tmp/work",
		Status:       SessionStateActive,
		CreatedAt:    1000,
		LastActivity: 2000,
	}

	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("failed to put: %v", err)
	}

	got, err := store.GetSession(context.Background(), "", "session-get")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.ProjectID != "proj-1" {
		t.Errorf("expected project ID 'proj-1', got %q", got.ProjectID)
	}
	if got.Model != "llama3" {
		t.Errorf("expected model 'llama3', got %q", got.Model)
	}
	if got.WorkDir != "/tmp/work" {
		t.Errorf("expected work dir '/tmp/work', got %q", got.WorkDir)
	}
	if got.LastActivity != 2000 {
		t.Errorf("expected last activity 2000, got %d", got.LastActivity)
	}
}

func TestInMemoryAgentStore_GetSession_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	_, err := store.GetSession(context.Background(), "", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrAgentStoreNotFound) {
		t.Fatalf("expected ErrAgentStoreNotFound, got %v", err)
	}
}

func TestInMemoryAgentStore_GetSession_EmptyID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	_, err := store.GetSession(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
	if !errors.Is(err, ErrAgentStoreInvalidInput) {
		t.Fatalf("expected ErrAgentStoreInvalidInput, got %v", err)
	}
}

func TestInMemoryAgentStore_GetSession_CancelledContext(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.GetSession(ctx, "", "session-1")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// DeleteSession Tests
// =============================================================================

func TestInMemoryAgentStore_DeleteSession(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	record := &SessionRecord{
		SessionID: "session-del",
		Backend:   "mock",
		Status:    SessionStateActive,
	}

	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("failed to put: %v", err)
	}

	err := store.DeleteSession(context.Background(), "", "session-del")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify it was deleted
	_, err = store.GetSession(context.Background(), "", "session-del")
	if !errors.Is(err, ErrAgentStoreNotFound) {
		t.Fatalf("expected ErrAgentStoreNotFound after delete, got %v", err)
	}
}

func TestInMemoryAgentStore_DeleteSession_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	err := store.DeleteSession(context.Background(), "", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrAgentStoreNotFound) {
		t.Fatalf("expected ErrAgentStoreNotFound, got %v", err)
	}
}

func TestInMemoryAgentStore_DeleteSession_EmptyID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	err := store.DeleteSession(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
	if !errors.Is(err, ErrAgentStoreInvalidInput) {
		t.Fatalf("expected ErrAgentStoreInvalidInput, got %v", err)
	}
}

func TestInMemoryAgentStore_DeleteSession_CancelledContext(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.DeleteSession(ctx, "", "session-1")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// ListSessions Tests
// =============================================================================

func TestInMemoryAgentStore_ListSessions(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()

	records := []*SessionRecord{
		{SessionID: "session-1", Backend: "mock", Status: SessionStateActive, CreatedAt: 1000},
		{SessionID: "session-2", Backend: "mock", Status: SessionStateClosed, CreatedAt: 2000},
		{SessionID: "session-3", Backend: "ollama", Status: SessionStateActive, CreatedAt: 3000},
	}

	for _, r := range records {
		if err := store.PutSession(context.Background(), r); err != nil {
			t.Fatalf("failed to put: %v", err)
		}
	}

	result, err := store.ListSessions(context.Background(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(result))
	}
}

func TestInMemoryAgentStore_ListSessions_Empty(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	result, err := store.ListSessions(context.Background(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(result))
	}
}

func TestInMemoryAgentStore_ListSessions_CancelledContext(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.ListSessions(ctx, "")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// UpdateSessionStatus Tests
// =============================================================================

func TestInMemoryAgentStore_UpdateSessionStatus(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	record := &SessionRecord{
		SessionID: "session-status",
		Backend:   "mock",
		Status:    SessionStateActive,
		CreatedAt: 1000,
	}

	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("failed to put: %v", err)
	}

	err := store.UpdateSessionStatus(context.Background(), "", "session-status", SessionStateClosed)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got, err := store.GetSession(context.Background(), "", "session-status")
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if got.Status != SessionStateClosed {
		t.Errorf("expected closed status, got %q", got.Status)
	}
	if got.LastActivity == 0 {
		t.Error("expected non-zero last activity after status update")
	}
}

func TestInMemoryAgentStore_UpdateSessionStatus_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	err := store.UpdateSessionStatus(context.Background(), "", "nonexistent", SessionStateClosed)
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrAgentStoreNotFound) {
		t.Fatalf("expected ErrAgentStoreNotFound, got %v", err)
	}
}

func TestInMemoryAgentStore_UpdateSessionStatus_EmptyID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	err := store.UpdateSessionStatus(context.Background(), "", "", SessionStateClosed)
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
	if !errors.Is(err, ErrAgentStoreInvalidInput) {
		t.Fatalf("expected ErrAgentStoreInvalidInput, got %v", err)
	}
}

// =============================================================================
// UpdateSessionActivity Tests
// =============================================================================

func TestInMemoryAgentStore_UpdateSessionActivity(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	record := &SessionRecord{
		SessionID:    "session-activity",
		Backend:      "mock",
		Status:       SessionStateActive,
		CreatedAt:    1000,
		LastActivity: 1000,
	}

	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("failed to put: %v", err)
	}

	err := store.UpdateSessionActivity(context.Background(), "", "session-activity")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got, err := store.GetSession(context.Background(), "", "session-activity")
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if got.LastActivity <= 1000 {
		t.Errorf("expected last activity to be updated beyond 1000, got %d", got.LastActivity)
	}
}

func TestInMemoryAgentStore_UpdateSessionActivity_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	err := store.UpdateSessionActivity(context.Background(), "", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrAgentStoreNotFound) {
		t.Fatalf("expected ErrAgentStoreNotFound, got %v", err)
	}
}

func TestInMemoryAgentStore_UpdateSessionActivity_EmptyID(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	err := store.UpdateSessionActivity(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
	if !errors.Is(err, ErrAgentStoreInvalidInput) {
		t.Fatalf("expected ErrAgentStoreInvalidInput, got %v", err)
	}
}
