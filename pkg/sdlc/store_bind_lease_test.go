package sdlc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBindLease_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	token, err := store.AcquireBindLease(ctx, fenceTenant, "p1")
	if err != nil || token == "" {
		t.Fatalf("AcquireBindLease = %q, %v", token, err)
	}

	_, err = store.AcquireBindLease(ctx, fenceTenant, "p1")
	var inProgress *BindInProgressError
	if !errors.As(err, &inProgress) || inProgress.ProjectID != "p1" || inProgress.TenantID != fenceTenant {
		t.Fatalf("second AcquireBindLease err = %v, want *BindInProgressError", err)
	}
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("BindInProgressError does not unwrap to ErrVersionConflict: %v", err)
	}

	// Another project's lease is independent.
	if _, err := store.AcquireBindLease(ctx, fenceTenant, "p2"); err != nil {
		t.Fatalf("AcquireBindLease on another project = %v", err)
	}

	if err := store.ReleaseBindLease(ctx, fenceTenant, "p1", "not-the-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcquireBindLease(ctx, fenceTenant, "p1"); !errors.As(err, &inProgress) {
		t.Fatalf("a release with the wrong token freed the lease: err = %v", err)
	}

	if err := store.ReleaseBindLease(ctx, fenceTenant, "p1", token); err != nil {
		t.Fatal(err)
	}
	again, err := store.AcquireBindLease(ctx, fenceTenant, "p1")
	if err != nil || again == "" || again == token {
		t.Fatalf("AcquireBindLease after release = %q, %v", again, err)
	}

	// Releasing an absent lease is not an error.
	if err := store.ReleaseBindLease(ctx, fenceTenant, "p3", "any"); err != nil {
		t.Fatalf("ReleaseBindLease of an absent lease = %v", err)
	}
}

func TestBindLease_StaleLeaseReplaced(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return base }

	crashed, err := store.AcquireBindLease(ctx, fenceTenant, "p1")
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return base.Add(BindLeaseTTL - time.Second) }
	var inProgress *BindInProgressError
	if _, err := store.AcquireBindLease(ctx, fenceTenant, "p1"); !errors.As(err, &inProgress) {
		t.Fatalf("AcquireBindLease over a live lease = %v, want *BindInProgressError", err)
	}

	store.now = func() time.Time { return base.Add(BindLeaseTTL) }
	second, err := store.AcquireBindLease(ctx, fenceTenant, "p1")
	if err != nil || second == "" || second == crashed {
		t.Fatalf("AcquireBindLease over a stale lease = %q, %v", second, err)
	}
	// The crashed holder's late release leaves the new lease alone.
	if err := store.ReleaseBindLease(ctx, fenceTenant, "p1", crashed); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcquireBindLease(ctx, fenceTenant, "p1"); !errors.As(err, &inProgress) {
		t.Fatalf("the crashed holder's token released the live lease: err = %v", err)
	}
}

func TestBindLease_RefusesABadProjectID(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.AcquireBindLease(context.Background(), fenceTenant, ""); err == nil {
		t.Fatal("AcquireBindLease accepted an empty project id")
	}
	if err := store.ReleaseBindLease(context.Background(), fenceTenant, "", "t"); err == nil {
		t.Fatal("ReleaseBindLease accepted an empty project id")
	}
}
