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
// Test helpers
// =============================================================================

// newTaskWithID creates and stores a task in the given store, returning the task.
func newTaskWithID(t *testing.T, store TaskStore, tenantID, taskID string) *Task {
	t.Helper()
	task := &Task{ID: taskID, TenantID: tenantID, Subject: "test task"}
	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

// =============================================================================
// validateArtifact Tests
// =============================================================================

func TestValidateArtifact_InlineValid(t *testing.T) {
	t.Parallel()
	a := &Artifact{Kind: ArtifactKindInline, Name: "output.txt", Content: []byte("hello")}
	if err := validateArtifact(a); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateArtifact_MissingName(t *testing.T) {
	t.Parallel()
	a := &Artifact{Kind: ArtifactKindInline, Name: "", Content: []byte("data")}
	err := validateArtifact(a)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	var inv *ArtifactRefInvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("expected *ArtifactRefInvalidError, got %T: %v", err, err)
	}
}

func TestValidateArtifact_InvalidKind(t *testing.T) {
	t.Parallel()
	a := &Artifact{Kind: "bogus", Name: "x"}
	err := validateArtifact(a)
	if err == nil {
		t.Fatal("expected ArtifactKindInvalidError")
	}
	var inv *ArtifactKindInvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("expected *ArtifactKindInvalidError, got %T: %v", err, err)
	}
}

func TestValidateArtifact_FileRequiresRef(t *testing.T) {
	t.Parallel()
	// file kind with empty Ref.Path must fail.
	a := &Artifact{Kind: ArtifactKindFile, Name: "file.log"}
	err := validateArtifact(a)
	if err == nil {
		t.Fatal("expected ArtifactRefInvalidError for missing file ref")
	}
}

func TestValidateArtifact_URLValid(t *testing.T) {
	t.Parallel()
	a := &Artifact{Kind: ArtifactKindURL, Name: "report", Ref: ArtifactRef{Path: "https://example.com/report.html"}}
	if err := validateArtifact(a); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// SubmitTaskArtifacts Tests
// =============================================================================

func TestSubmitTaskArtifacts_InlineArtifact(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	newTaskWithID(t, store, "ten", "task-art-1")

	arts := []Artifact{
		{Kind: ArtifactKindInline, Name: "result.json", Content: []byte(`{"ok":true}`)},
	}
	if err := store.SubmitTaskArtifacts(context.Background(), "ten", "task-art-1", arts); err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	got, err := store.GetTaskArtifact(context.Background(), "ten", "task-art-1", "result.json")
	if err != nil {
		t.Fatalf("GetTaskArtifact: %v", err)
	}
	if string(got.Content) != `{"ok":true}` {
		t.Errorf("content = %q, want {\"ok\":true}", string(got.Content))
	}
}

func TestSubmitTaskArtifacts_OverwriteExisting(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	newTaskWithID(t, store, "ten", "task-art-overwrite")

	submit := func(content string) {
		err := store.SubmitTaskArtifacts(context.Background(), "ten", "task-art-overwrite", []Artifact{
			{Kind: ArtifactKindInline, Name: "out.txt", Content: []byte(content)},
		})
		if err != nil {
			t.Fatalf("SubmitTaskArtifacts(%q): %v", content, err)
		}
	}

	submit("v1")
	submit("v2")

	list, err := store.ListTaskArtifacts(context.Background(), "ten", "task-art-overwrite")
	if err != nil {
		t.Fatalf("ListTaskArtifacts: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(list))
	}
	got, err := store.GetTaskArtifact(context.Background(), "ten", "task-art-overwrite", "out.txt")
	if err != nil {
		t.Fatalf("GetTaskArtifact: %v", err)
	}
	if string(got.Content) != "v2" {
		t.Errorf("expected v2, got %q", string(got.Content))
	}
}

func TestSubmitTaskArtifacts_TaskNotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	err := store.SubmitTaskArtifacts(context.Background(), "ten", "no-such-task", []Artifact{
		{Kind: ArtifactKindInline, Name: "x", Content: []byte("y")},
	})
	if err == nil {
		t.Fatal("expected TaskNotFoundError")
	}
	var notFound *TaskNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *TaskNotFoundError, got %T: %v", err, err)
	}
}

func TestSubmitTaskArtifacts_InvalidKindRejected(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	newTaskWithID(t, store, "ten", "task-bad-kind")

	err := store.SubmitTaskArtifacts(context.Background(), "ten", "task-bad-kind", []Artifact{
		{Kind: "nonsense", Name: "x"},
	})
	if err == nil {
		t.Fatal("expected ArtifactKindInvalidError")
	}
	var inv *ArtifactKindInvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("expected *ArtifactKindInvalidError, got %T: %v", err, err)
	}
}

// =============================================================================
// Large artifact promotion Tests
// =============================================================================

func TestSubmitTaskArtifacts_LargeInlinePromotedToKV(t *testing.T) {
	t.Parallel()

	kv := NewMemoryTaskKV()
	// Set an artificially low inline limit of 10 bytes to trigger promotion.
	store := NewInMemoryTaskStoreWithKV(kv, 10)
	newTaskWithID(t, store, "ten", "task-kv-1")

	bigContent := make([]byte, 20) // > 10 bytes
	for i := range bigContent {
		bigContent[i] = byte('a' + i%26)
	}

	err := store.SubmitTaskArtifacts(context.Background(), "ten", "task-kv-1", []Artifact{
		{Kind: ArtifactKindInline, Name: "large.bin", Content: bigContent},
	})
	if err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	// GetTaskArtifact must resolve the KV ref and return the original content.
	got, err := store.GetTaskArtifact(context.Background(), "ten", "task-kv-1", "large.bin")
	if err != nil {
		t.Fatalf("GetTaskArtifact: %v", err)
	}
	if string(got.Content) != string(bigContent) {
		t.Errorf("resolved content mismatch: got len=%d, want len=%d", len(got.Content), len(bigContent))
	}

	// ListTaskArtifacts must return the artifact as kv_key with Content omitted.
	list, err := store.ListTaskArtifacts(context.Background(), "ten", "task-kv-1")
	if err != nil {
		t.Fatalf("ListTaskArtifacts: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 artifact in list, got %d", len(list))
	}
	if list[0].Kind != ArtifactKindKVKey {
		t.Errorf("listed kind = %q, want kv_key", list[0].Kind)
	}
	if list[0].Content != nil {
		t.Error("ListTaskArtifacts must not return Content for kv_key artifacts")
	}
}

// A task store with no KV has nowhere to keep either task records or artifact
// payloads, so it must fail closed on every verb rather than panic or silently
// accept writes it cannot durably hold.
//
// This replaces the former "oversized inline artifact with no KV backend is
// rejected" case: task records now live in the KV alongside artifacts, so a
// store without one cannot reach the artifact path at all. Promotion of an
// artifact above the inline limit is covered in task_store_test.go.
func TestTaskStore_NilKV_FailsClosed(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStoreWithKV(nil, 10)
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "create task",
			call: func() error {
				return store.CreateTask(ctx, &Task{ID: "task-no-kv", TenantID: "ten", Subject: "s"})
			},
		},
		{
			name: "get task",
			call: func() error {
				_, err := store.GetTask(ctx, "ten", "task-no-kv")
				return err
			},
		},
		{
			name: "submit artifacts",
			call: func() error {
				return store.SubmitTaskArtifacts(ctx, "ten", "task-no-kv", []Artifact{
					{Kind: ArtifactKindInline, Name: "large.bin", Content: make([]byte, 20)},
				})
			},
		},
		{
			name: "list tasks",
			call: func() error {
				_, err := store.ListTasks(ctx, "ten", nil)
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("a store with no KV must not report success")
			}
			var storeErr *TaskStoreError
			if !errors.As(err, &storeErr) {
				t.Fatalf("want *TaskStoreError, got %T: %v", err, err)
			}
		})
	}
}

// =============================================================================
// GetTaskArtifact Tests
// =============================================================================

func TestGetTaskArtifact_NotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	newTaskWithID(t, store, "ten", "task-get-404")

	_, err := store.GetTaskArtifact(context.Background(), "ten", "task-get-404", "missing")
	if err == nil {
		t.Fatal("expected ArtifactNotFoundError")
	}
	var notFound *ArtifactNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *ArtifactNotFoundError, got %T: %v", err, err)
	}
}

func TestGetTaskArtifact_TaskNotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	_, err := store.GetTaskArtifact(context.Background(), "ten", "no-task", "art")
	if err == nil {
		t.Fatal("expected TaskNotFoundError")
	}
}

// =============================================================================
// ListTaskArtifacts Tests
// =============================================================================

func TestListTaskArtifacts_Empty(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	newTaskWithID(t, store, "ten", "task-list-empty")

	list, err := store.ListTaskArtifacts(context.Background(), "ten", "task-list-empty")
	if err != nil {
		t.Fatalf("ListTaskArtifacts: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestListTaskArtifacts_MultipleArtifacts(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTaskStore()
	newTaskWithID(t, store, "ten", "task-list-multi")

	arts := []Artifact{
		{Kind: ArtifactKindInline, Name: "a1.txt", Content: []byte("one")},
		{Kind: ArtifactKindInline, Name: "a2.txt", Content: []byte("two")},
		{Kind: ArtifactKindURL, Name: "report", Ref: ArtifactRef{Path: "https://example.com"}},
	}
	if err := store.SubmitTaskArtifacts(context.Background(), "ten", "task-list-multi", arts); err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	list, err := store.ListTaskArtifacts(context.Background(), "ten", "task-list-multi")
	if err != nil {
		t.Fatalf("ListTaskArtifacts: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(list))
	}

	names := make(map[string]bool, 3)
	for _, a := range list {
		names[a.Name] = true
	}
	for _, want := range []string{"a1.txt", "a2.txt", "report"} {
		if !names[want] {
			t.Errorf("artifact %q not found in list", want)
		}
	}
}
