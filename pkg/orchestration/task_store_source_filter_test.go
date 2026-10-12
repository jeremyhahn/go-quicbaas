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

package orchestration

import (
	"context"
	"errors"
	"testing"
)

// =============================================================================
// Task listing by source (go-quicbaas#730): "which task holds this source" is
// answered through the source reservation index.
// =============================================================================

// seedSourcedTasks creates, in tenant-a, a task adopted from owner/repo#12 in
// proj-1, one adopted from the same ref in proj-2, and one with no source.
func seedSourcedTasks(t *testing.T, store TaskStore) {
	t.Helper()
	ctx := context.Background()
	for _, task := range []*Task{
		{ID: "held", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "a",
			Source: &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "owner/repo#12"}},
		{ID: "other-project", TenantID: "tenant-a", ProjectID: "proj-2", Subject: "b",
			Source: &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "owner/repo#12"}},
		{ID: "unsourced", TenantID: "tenant-a", ProjectID: "proj-1", Subject: "c"},
	} {
		if err := store.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask(%s): %v", task.ID, err)
		}
	}
}

func TestListTasks_SourceFilterReturnsTheHolder(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	seedSourcedTasks(t, store)

	tasks, err := store.ListTasks(context.Background(), "tenant-a", &TaskFilter{
		ProjectID: "proj-1", SourceKind: TaskSourceKindOperator, SourceRef: "owner/repo#12",
	})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "held" {
		t.Fatalf("tasks = %v, want only the proj-1 holder", taskIDs(tasks))
	}
}

func TestListTasks_SourceFilterOnAnUnheldSourceListsNothing(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	seedSourcedTasks(t, store)

	for name, filter := range map[string]*TaskFilter{
		"unknown ref":       {ProjectID: "proj-1", SourceKind: TaskSourceKindOperator, SourceRef: "owner/repo#99"},
		"other kind":        {ProjectID: "proj-1", SourceKind: TaskSourceKindJob, SourceRef: "owner/repo#12"},
		"unscoped":          {SourceKind: TaskSourceKindOperator, SourceRef: "owner/repo#12"},
		"no such project":   {ProjectID: "proj-9", SourceKind: TaskSourceKindOperator, SourceRef: "owner/repo#12"},
		"holder filtered":   {ProjectID: "proj-1", SourceKind: TaskSourceKindOperator, SourceRef: "owner/repo#12", Status: TaskStatusCompleted},
		"tenant not holder": {ProjectID: "proj-1", SourceKind: TaskSourceKindOperator, SourceRef: "owner/repo#12", AssignedTo: "nobody"},
	} {
		tasks, err := store.ListTasks(context.Background(), "tenant-a", filter)
		if err != nil {
			t.Fatalf("%s: ListTasks: %v, want an empty listing", name, err)
		}
		if tasks == nil || len(tasks) != 0 {
			t.Fatalf("%s: tasks = %v, want an empty, non-nil listing", name, taskIDs(tasks))
		}
	}
}

func TestListTasks_SourceFilterDoesNotCrossTenants(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	seedSourcedTasks(t, store)

	tasks, err := store.ListTasks(context.Background(), "tenant-b", &TaskFilter{
		ProjectID: "proj-1", SourceKind: TaskSourceKindOperator, SourceRef: "owner/repo#12",
	})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("tasks = %v, want nothing: tenant-b holds no source", taskIDs(tasks))
	}
}

func TestListTasks_SourceFilterAfterTheHolderIsDeletedListsNothing(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	seedSourcedTasks(t, store)
	if err := store.DeleteTask(context.Background(), "tenant-a", "held"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	tasks, err := store.ListTasks(context.Background(), "tenant-a", &TaskFilter{
		ProjectID: "proj-1", SourceKind: TaskSourceKindOperator, SourceRef: "owner/repo#12",
	})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("tasks = %v, want nothing once the holder is gone", taskIDs(tasks))
	}
}

func TestListTasks_SourceFilterNeedsKindAndRef(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	seedSourcedTasks(t, store)

	for name, filter := range map[string]*TaskFilter{
		"ref without kind": {ProjectID: "proj-1", SourceRef: "owner/repo#12"},
		"kind without ref": {ProjectID: "proj-1", SourceKind: TaskSourceKindOperator},
		"unknown kind":     {ProjectID: "proj-1", SourceKind: "bogus", SourceRef: "owner/repo#12"},
	} {
		if _, err := store.ListTasks(context.Background(), "tenant-a", filter); !errors.Is(err, ErrTaskStoreInvalidInput) {
			t.Fatalf("%s: err = %v, want ErrTaskStoreInvalidInput", name, err)
		}
	}
}

func TestTaskFilterFacetBase_KeepsTheProjectOfASourceQuery(t *testing.T) {
	t.Parallel()
	sourced := (&TaskFilter{ProjectID: "proj-1", SourceKind: TaskSourceKindOperator, SourceRef: "r"}).FacetBase()
	if sourced.ProjectID != "proj-1" {
		t.Fatalf("FacetBase dropped the project of a source query: %+v", sourced)
	}
	plain := (&TaskFilter{ProjectID: "proj-1"}).FacetBase()
	if plain.ProjectID != "" {
		t.Fatalf("FacetBase kept the project facet of a plain listing: %+v", plain)
	}
}

func taskIDs(tasks []*Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}
