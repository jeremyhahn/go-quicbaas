package sdlc

import (
	"context"
	"errors"
	"testing"
)

// PutBinding stamps a fresh write token on every put and reports the record
// it replaced: nil on a create, the held record on a replacement.
func TestPutBinding_ReportsPriorAndStampsWriteToken(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := testBinding()
	prior, err := store.PutBinding(ctx, testTenant, first)
	if err != nil {
		t.Fatal(err)
	}
	if prior != nil {
		t.Fatalf("create reported prior %+v, want nil", prior)
	}
	if first.WriteToken == "" {
		t.Fatal("PutBinding stamped no write token")
	}

	second := testBinding()
	second.Strategy = first.Strategy
	second.WriteToken = "sent-in"
	prior, err = store.PutBinding(ctx, testTenant, second)
	if err != nil {
		t.Fatal(err)
	}
	if prior == nil || prior.WriteToken != first.WriteToken {
		t.Fatalf("replace reported prior %+v, want the first write (token %q)", prior, first.WriteToken)
	}
	if second.WriteToken == "" || second.WriteToken == "sent-in" || second.WriteToken == first.WriteToken {
		t.Fatalf("second write token = %q, want a fresh store-drawn one", second.WriteToken)
	}
}

// A bind that withdraws a fresh write removes the binding it created.
func TestWithdrawBinding_RemovesAFreshWrite(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	binding := testBinding()
	prior, err := store.PutBinding(ctx, testTenant, binding)
	if err != nil {
		t.Fatal(err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, binding.WriteToken, prior)
	if err != nil || !withdrawn {
		t.Fatalf("WithdrawBinding = %v, %v; want true, nil", withdrawn, err)
	}
	if _, err := store.GetBinding(ctx, testTenant, testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("binding after withdrawal: %v, want not found", err)
	}
}

// A rebind that withdraws restores the record it replaced.
func TestWithdrawBinding_RebindRestoresPrior(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	original := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, original); err != nil {
		t.Fatal(err)
	}
	rebind := testBinding()
	rebind.MaxReviewRounds = original.MaxReviewRounds + 3
	prior, err := store.PutBinding(ctx, testTenant, rebind)
	if err != nil {
		t.Fatal(err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, rebind.WriteToken, prior)
	if err != nil || !withdrawn {
		t.Fatalf("WithdrawBinding = %v, %v; want true, nil", withdrawn, err)
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if got.WriteToken != original.WriteToken || got.MaxReviewRounds != original.MaxReviewRounds ||
		!got.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatalf("after withdrawal: %+v, want the original record back", got)
	}
}

// A bind replaced by a concurrent bind is not removed by the first bind's
// withdrawal: the token no longer matches.
func TestWithdrawBinding_TokenMismatchLeavesTheLaterBind(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	first := testBinding()
	firstPrior, err := store.PutBinding(ctx, testTenant, first)
	if err != nil {
		t.Fatal(err)
	}
	second := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, second); err != nil {
		t.Fatal(err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, first.WriteToken, firstPrior)
	if err != nil || withdrawn {
		t.Fatalf("WithdrawBinding with a replaced token = %v, %v; want false, nil", withdrawn, err)
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatalf("the later bind was removed: %v", err)
	}
	if got.WriteToken != second.WriteToken {
		t.Fatalf("binding token = %q, want the later bind's %q", got.WriteToken, second.WriteToken)
	}
}

func TestWithdrawBinding_AbsentAndBadInput(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, "tok", nil); err != nil || withdrawn {
		t.Fatalf("WithdrawBinding of an absent binding = %v, %v; want false, nil", withdrawn, err)
	}
	if _, err := store.WithdrawBinding(ctx, testTenant, testProject, "", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("WithdrawBinding with no token = %v, want invalid input", err)
	}
	if _, err := store.WithdrawBinding(ctx, testTenant, "", "tok", nil); err == nil {
		t.Fatal("WithdrawBinding accepted an empty project id")
	}
}
