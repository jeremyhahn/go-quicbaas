package sdlc

import (
	"context"
	"errors"
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

// A second delete is refused while the first's fence is live: replacing it
// would let the first's clear drop the fence the second relies on.
func TestDeleteFence_SecondDeleteRefusedWhileLive(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first, err := store.FenceProjectDelete(ctx, fenceTenant, "p1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.FenceProjectDelete(ctx, fenceTenant, "p1")
	var inProgress *DeleteInProgressError
	if !errors.As(err, &inProgress) || !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("second FenceProjectDelete = %v, want *DeleteInProgressError (a version conflict)", err)
	}
	if inProgress.TenantID != fenceTenant || inProgress.ProjectID != "p1" {
		t.Fatalf("DeleteInProgressError = %+v", inProgress)
	}
	// The first delete's fence is untouched and still clears with its token.
	if err := store.ClearProjectDeleteFence(ctx, fenceTenant, "p1", first); err != nil {
		t.Fatal(err)
	}
	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || fenced {
		t.Fatalf("after the first delete cleared: fenced=%v err=%v", fenced, err)
	}
}

// A stale fence (a crashed delete's) is replaced, and the crashed delete's
// token no longer clears the fence that replaced it.
func TestDeleteFence_StaleFenceReplaced(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return base }

	crashed, err := store.FenceProjectDelete(ctx, fenceTenant, "p1")
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return base.Add(DeleteFenceTTL) }
	second, err := store.FenceProjectDelete(ctx, fenceTenant, "p1")
	if err != nil || second == "" || second == crashed {
		t.Fatalf("FenceProjectDelete over a stale fence = %q, %v", second, err)
	}
	if err := store.ClearProjectDeleteFence(ctx, fenceTenant, "p1", crashed); err != nil {
		t.Fatal(err)
	}
	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || !fenced {
		t.Fatalf("the crashed delete's token lifted the live fence: fenced=%v err=%v", fenced, err)
	}
}

// TestDeleteFence_RenewKeepsItLiveAndRefusesALostFence: a renew restamps a
// live fence, so it holds past the original TTL; a renew under another
// token, of a stale fence or of a tombstone is refused.
func TestDeleteFence_RenewKeepsItLiveAndRefusesALostFence(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return base }

	token, err := store.FenceProjectDelete(ctx, fenceTenant, "p1")
	if err != nil {
		t.Fatal(err)
	}
	renewed := base.Add(DeleteFenceTTL - time.Second)
	store.now = func() time.Time { return renewed }
	if err := store.RenewProjectDeleteFence(ctx, fenceTenant, "p1", token); err != nil {
		t.Fatalf("renew of a live fence: %v", err)
	}

	store.now = func() time.Time { return base.Add(DeleteFenceTTL + DeleteFenceTTL/2) }
	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || !fenced {
		t.Fatalf("renewed fence past the original TTL: fenced=%v err=%v, want held", fenced, err)
	}
	var inProgress *DeleteInProgressError
	if _, err := store.FenceProjectDelete(ctx, fenceTenant, "p1"); !errors.As(err, &inProgress) {
		t.Fatalf("second fence over a renewed fence = %v, want *DeleteInProgressError", err)
	}
	var conflict *VersionConflictError
	if err := store.RenewProjectDeleteFence(ctx, fenceTenant, "p1", "not-the-token"); !errors.As(err, &conflict) {
		t.Fatalf("renew with another token = %v, want *VersionConflictError", err)
	}

	store.now = func() time.Time { return renewed.Add(DeleteFenceTTL) }
	if err := store.RenewProjectDeleteFence(ctx, fenceTenant, "p1", token); !errors.As(err, &conflict) {
		t.Fatalf("renew of a stale fence = %v, want *VersionConflictError", err)
	}
	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || fenced {
		t.Fatalf("stale fence after a refused renew: fenced=%v err=%v, want absent", fenced, err)
	}

	tomb, err := store.FenceProjectDelete(ctx, fenceTenant, "p2")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkProjectDeleted(ctx, fenceTenant, "p2", tomb); err != nil {
		t.Fatal(err)
	}
	if err := store.RenewProjectDeleteFence(ctx, fenceTenant, "p2", tomb); !errors.As(err, &conflict) {
		t.Fatalf("renew of a tombstone = %v, want *VersionConflictError", err)
	}
}

// A completed delete leaves a tombstone: it holds whatever its age, no clear
// removes it, and a later fence attempt reports the project gone.
func TestDeleteFence_TombstoneHoldsForever(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return base }

	token, err := store.FenceProjectDelete(ctx, fenceTenant, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkProjectDeleted(ctx, fenceTenant, "p1", token); err != nil {
		t.Fatalf("MarkProjectDeleted: %v", err)
	}
	// Marking again with the same token is a no-op.
	if err := store.MarkProjectDeleted(ctx, fenceTenant, "p1", token); err != nil {
		t.Fatalf("second MarkProjectDeleted: %v", err)
	}
	if err := store.ClearProjectDeleteFence(ctx, fenceTenant, "p1", token); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return base.Add(100 * DeleteFenceTTL) }
	if fenced, err := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); err != nil || !fenced {
		t.Fatalf("tombstone: fenced=%v err=%v, want true whatever its age and after a clear", fenced, err)
	}
	if _, err := store.FenceProjectDelete(ctx, fenceTenant, "p1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FenceProjectDelete over a tombstone = %v, want not found", err)
	}
}

// Only the caller's own fence becomes a tombstone.
func TestDeleteFence_MarkChecksTheToken(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.MarkProjectDeleted(ctx, fenceTenant, "p1", "nobody"); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("MarkProjectDeleted with no fence = %v, want a version conflict", err)
	}
	if _, err := store.FenceProjectDelete(ctx, fenceTenant, "p1"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkProjectDeleted(ctx, fenceTenant, "p1", "someone-else"); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("MarkProjectDeleted with another token = %v, want a version conflict", err)
	}
	base := time.Now().UTC()
	store.now = func() time.Time { return base.Add(DeleteFenceTTL) }
	if fenced, _ := store.ProjectDeleteFenced(ctx, fenceTenant, "p1"); fenced {
		t.Fatal("a refused mark wrote a tombstone")
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
