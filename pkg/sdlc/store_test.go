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

package sdlc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStore_BindingRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.GetBinding(ctx, testTenant, testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBinding before put: err=%v, want ErrNotFound", err)
	}
	binding := testBinding()
	if err := store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}
	if binding.CreatedAt.IsZero() || binding.UpdatedAt.IsZero() {
		t.Fatal("timestamps not stamped")
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatalf("GetBinding: %v", err)
	}
	if got.Forge.TriggerLabel != "task:ready" || got.Review.RequiredApprovals != 1 || got.Forge.Labels[LabelMerged] != "factory:merged" {
		t.Fatalf("binding did not round-trip: %+v", got)
	}

	// A replacement keeps the creation time and advances the update time.
	created := got.CreatedAt
	store.now = func() time.Time { return created.Add(time.Hour) }
	binding.Review.RequiredApprovals = 2
	if err := store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatalf("PutBinding again: %v", err)
	}
	got, err = store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatalf("GetBinding after replace: %v", err)
	}
	if got.Review.RequiredApprovals != 2 || !got.CreatedAt.Equal(created) || !got.UpdatedAt.After(created) {
		t.Fatalf("replacement did not keep created/advance updated: %+v", got)
	}

	// Another tenant sees nothing: the resolver confined the store.
	if _, err := store.GetBinding(ctx, "tenant-b", testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read: err=%v, want ErrNotFound", err)
	}

	list, err := store.ListBindings(ctx, testTenant)
	if err != nil || len(list) != 1 || list[0].ProjectID != testProject {
		t.Fatalf("ListBindings = %+v, %v", list, err)
	}
	if err := store.DeleteBinding(ctx, testTenant, testProject); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if err := store.DeleteBinding(ctx, testTenant, testProject); err != nil {
		t.Fatalf("DeleteBinding of an absent binding must be a no-op, got %v", err)
	}
	if _, err := store.GetBinding(ctx, testTenant, testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: err=%v", err)
	}
}

func TestStore_RefusesBadInput(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.GetBinding(ctx, testTenant, "a/b"); !errors.Is(err, ErrInvalidKeySegment) {
		t.Fatalf("slash in project id: %v", err)
	}
	if err := store.PutBinding(ctx, testTenant, &ProjectBinding{ProjectID: testProject}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("incomplete binding: %v", err)
	}
	if err := store.PutBinding(ctx, testTenant, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil binding: %v", err)
	}
	if _, err := store.GetDelivery(ctx, testTenant, ""); !errors.Is(err, ErrInvalidKeySegment) {
		t.Fatalf("empty task id: %v", err)
	}
	if err := store.PutDelivery(ctx, testTenant, &Delivery{TaskID: testTask}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("delivery without attempt/branch: %v", err)
	}
	if _, err := NewStore(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil resolver: %v", err)
	}
}

func TestStore_ResolverFailureIsABackendError(t *testing.T) {
	resolver := newMemoryResolver()
	resolver.err = errors.New("barrier sealed")
	store, err := NewStore(resolver)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.GetBinding(ctx, testTenant, testProject); !errors.Is(err, ErrBackend) {
		t.Fatalf("GetBinding: %v", err)
	}
	if err := store.PutBinding(ctx, testTenant, testBinding()); !errors.Is(err, ErrBackend) {
		t.Fatalf("PutBinding: %v", err)
	}
	if _, err := store.ListBindings(ctx, testTenant); !errors.Is(err, ErrBackend) {
		t.Fatalf("ListBindings: %v", err)
	}
	if err := store.DeleteBinding(ctx, testTenant, testProject); !errors.Is(err, ErrBackend) {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if _, err := store.GetDelivery(ctx, testTenant, testTask); !errors.Is(err, ErrBackend) {
		t.Fatalf("GetDelivery: %v", err)
	}
	d := &Delivery{TaskID: testTask, Attempt: 1, Branch: "b"}
	if err := store.PutDelivery(ctx, testTenant, d); !errors.Is(err, ErrBackend) {
		t.Fatalf("PutDelivery: %v", err)
	}
	if err := store.UpdateDelivery(ctx, testTenant, d); !errors.Is(err, ErrBackend) {
		t.Fatalf("UpdateDelivery: %v", err)
	}
}

func TestStore_DeliveryVersionGuard(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	d := &Delivery{TaskID: testTask, ProjectID: testProject, Attempt: 1, Branch: "issue/1-x", Base: "main"}
	if err := store.PutDelivery(ctx, testTenant, d); err != nil {
		t.Fatalf("PutDelivery: %v", err)
	}
	if d.Version != 1 {
		t.Fatalf("first version = %d, want 1", d.Version)
	}

	// Two readers hold version 1; the first write wins, the second is refused.
	a, err := store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	a.HeadSHA = "aaa"
	if err := store.UpdateDelivery(ctx, testTenant, a); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if a.Version != 2 {
		t.Fatalf("version after update = %d, want 2", a.Version)
	}
	b.HeadSHA = "bbb"
	err = store.UpdateDelivery(ctx, testTenant, b)
	var conflict *VersionConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrVersionConflict) || conflict.Expected != 1 {
		t.Fatalf("stale update: err=%v", err)
	}
	got, err := store.GetDelivery(ctx, testTenant, testTask)
	if err != nil || got.HeadSHA != "aaa" || got.Version != 2 {
		t.Fatalf("stored after conflict: %+v, %v", got, err)
	}

	// A replacement (next attempt) advances the version past every reader.
	next := &Delivery{TaskID: testTask, ProjectID: testProject, Attempt: 2, Branch: "issue/1-x", Base: "main"}
	if err := store.PutDelivery(ctx, testTenant, next); err != nil {
		t.Fatal(err)
	}
	if next.Version != 3 || !next.CreatedAt.Equal(got.CreatedAt) {
		t.Fatalf("replacement version=%d created=%v, want 3 and the original creation time %v", next.Version, next.CreatedAt, got.CreatedAt)
	}
	if err := store.UpdateDelivery(ctx, testTenant, a); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("old reader after replacement: %v", err)
	}
}

func TestStore_DeliveryIsolatedByTenantAndTask(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.PutDelivery(ctx, testTenant, &Delivery{TaskID: testTask, Attempt: 1, Branch: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDelivery(ctx, "tenant-b", testTask); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant: %v", err)
	}
	if _, err := store.GetDelivery(ctx, testTenant, "task-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other task: %v", err)
	}
}
