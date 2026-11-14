package sdlc

import (
	"context"
	"testing"
	"time"
)

const fenceTenant = "tenant-fence"

func TestDeleteFence_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || fenced {
		t.Fatalf("before any fence: fenced=%v err=%v, want false, nil", fenced, err)
	}
	token, err := store.FenceProjectDelete(ctx, fenceTenant, "p1")
	if err != nil || token == "" {
		t.Fatalf("FenceProjectDelete = %q, %v", token, err)
	}
	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || !fenced {
		t.Fatalf("after fencing: fenced=%v err=%v, want true, nil", fenced, err)
	}
	if fenced, _ := store.ProjectDeleteFenced(ctx, fenceTenant, "p2"); fenced {
		t.Fatal("a fence on p1 fenced p2")
	}
	if err := store.ClearProjectDeleteFence(ctx, fenceTenant, "p1", token); err != nil {
		t.Fatalf("ClearProjectDeleteFence: %v", err)
	}
	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || fenced {
		t.Fatalf("after clearing: fenced=%v err=%v, want false, nil", fenced, err)
	}
	// Clearing an absent fence is not an error.
	if err := store.ClearProjectDeleteFence(ctx, fenceTenant, "p1", token); err != nil {
		t.Fatalf("second clear: %v", err)
	}
}

// A delete that finishes must not lift the fence of a delete that started
// after it: the later one is still between its check and its delete.
func TestDeleteFence_ClearLeavesAnotherDeletesFence(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first, err := store.FenceProjectDelete(ctx, fenceTenant, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FenceProjectDelete(ctx, fenceTenant, "p1"); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearProjectDeleteFence(ctx, fenceTenant, "p1", first); err != nil {
		t.Fatalf("clear with the replaced token: %v", err)
	}
	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || !fenced {
		t.Fatalf("the second delete's fence was lifted: fenced=%v err=%v", fenced, err)
	}
}

// A fence a crashed delete left behind stops holding after DeleteFenceTTL, so
// a project that survived the crash can be bound again.
func TestDeleteFence_GoesStale(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return base }

	if _, err := store.FenceProjectDelete(ctx, fenceTenant, "p1"); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return base.Add(DeleteFenceTTL - time.Second) }
	if fenced, _ := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); !fenced {
		t.Fatal("a fence younger than the TTL did not hold")
	}
	store.now = func() time.Time { return base.Add(DeleteFenceTTL) }
	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || fenced {
		t.Fatalf("a fence at the TTL still held: fenced=%v err=%v", fenced, err)
	}
}

func TestDeleteFence_RefusesABadProjectID(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.FenceProjectDelete(context.Background(), fenceTenant, ""); err == nil {
		t.Fatal("FenceProjectDelete accepted an empty project id")
	}
}
