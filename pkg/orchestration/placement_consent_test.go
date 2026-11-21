// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	consentTenant      = "tenant-a"
	consentOtherTenant = "tenant-b"
)

func testConsentMove() ConsentMove {
	return ConsentMove{
		SpecName:       "agents",
		SpecGeneration: 3,
		Class:          "agent",
		WorkID:         "session-1",
		SourceHost:     "host-1",
	}
}

// consentKV wraps a guarded KV so a test can fail its reads or act between a
// read and a swap.
type consentKV struct {
	interfaces.KVStore
	getErr     error
	beforeSwap func()
}

func (k *consentKV) Get(ctx context.Context, key string) ([]byte, error) {
	if k.getErr != nil {
		return nil, k.getErr
	}
	return k.KVStore.Get(ctx, key)
}

func (k *consentKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return k.KVStore.(guardedCreator).PutIfAbsent(ctx, key, value)
}

func (k *consentKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	if k.beforeSwap != nil {
		k.beforeSwap()
	}
	return k.KVStore.(guardedSwapper).CompareAndSwap(ctx, key, expected, next)
}

// consentcreateOnlyKV has PutIfAbsent but no CompareAndSwap.
type consentcreateOnlyKV struct {
	interfaces.KVStore
}

func (k *consentcreateOnlyKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return k.KVStore.(guardedCreator).PutIfAbsent(ctx, key, value)
}

// consentplainKV has neither guarded verb.
type consentplainKV struct {
	interfaces.KVStore
}

func newTestConsentLedger(t *testing.T) (ConsentLedger, *consentKV) {
	t.Helper()
	kv := &consentKV{KVStore: NewMemoryTaskKV()}
	ledger, err := NewConsentLedger(kv)
	if err != nil {
		t.Fatalf("NewConsentLedger: %v", err)
	}
	return ledger, kv
}

func TestPlacementConsent_NewLedgerRefusesAnUnguardedStore(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		kv      interfaces.KVStore
		missing string
	}{
		"nil":         {kv: nil, missing: "a store"},
		"no guards":   {kv: &consentplainKV{KVStore: NewMemoryTaskKV()}, missing: "PutIfAbsent"},
		"create only": {kv: &consentcreateOnlyKV{KVStore: NewMemoryTaskKV()}, missing: "CompareAndSwap"},
	}
	for name, tc := range cases {
		_, err := NewConsentLedger(tc.kv)
		var unguarded *PlacementStoreUnguardedError
		if !errors.As(err, &unguarded) || !errors.Is(err, ErrPlacementStoreUnguarded) {
			t.Fatalf("%s: NewConsentLedger = %v, want *PlacementStoreUnguardedError", name, err)
		}
		if unguarded.Missing != tc.missing {
			t.Fatalf("%s: Missing = %q, want %q", name, unguarded.Missing, tc.missing)
		}
	}
}

func TestPlacementConsent_RequestIDIdentifiesTheMove(t *testing.T) {
	t.Parallel()

	base := testConsentMove()
	id := base.RequestID(consentTenant)
	if id == "" || id != base.RequestID(consentTenant) {
		t.Fatalf("RequestID not stable: %q", id)
	}
	if id == base.RequestID(consentOtherTenant) {
		t.Fatal("RequestID ignores the tenant")
	}

	edits := map[string]func(*ConsentMove){
		"spec name":       func(m *ConsentMove) { m.SpecName = "agents2" },
		"spec generation": func(m *ConsentMove) { m.SpecGeneration++ },
		"class":           func(m *ConsentMove) { m.Class = "ci" },
		"work id":         func(m *ConsentMove) { m.WorkID = "session-2" },
		"source host":     func(m *ConsentMove) { m.SourceHost = "host-2" },
	}
	for name, edit := range edits {
		move := base
		edit(&move)
		if move.RequestID(consentTenant) == id {
			t.Fatalf("changing the %s kept the request id", name)
		}
	}

	// Fields that only collide when concatenated without length prefixes.
	a, b := base, base
	a.SpecName, a.Class = "ab", "c"
	b.SpecName, b.Class = "a", "bc"
	if a.RequestID(consentTenant) == b.RequestID(consentTenant) {
		t.Fatal("spec name ab + class c collides with a + bc")
	}
	c, d := base, base
	c.WorkID, c.SourceHost = "w1h", "1"
	d.WorkID, d.SourceHost = "w1", "h1"
	if c.RequestID(consentTenant) == d.RequestID(consentTenant) {
		t.Fatal("work id + source host collide when concatenated")
	}
	if a.RequestID("t") == a.RequestID("t"+"\x00") {
		t.Fatal("tenant boundary collides")
	}
}

func TestPlacementConsent_RaiseRefusesAnIncompleteMove(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestConsentLedger(t)
	ctx := context.Background()

	edits := map[string]func(*ConsentMove){
		"spec name":       func(m *ConsentMove) { m.SpecName = "" },
		"spec generation": func(m *ConsentMove) { m.SpecGeneration = 0 },
		"class":           func(m *ConsentMove) { m.Class = "" },
		"work id":         func(m *ConsentMove) { m.WorkID = "" },
		"source host":     func(m *ConsentMove) { m.SourceHost = "" },
	}
	for name, edit := range edits {
		move := testConsentMove()
		edit(&move)
		if _, err := ledger.Raise(ctx, consentTenant, move); !errors.Is(err, ErrConsentInvalid) {
			t.Fatalf("%s: Raise = %v, want ErrConsentInvalid", name, err)
		}
	}
	if _, err := ledger.Raise(ctx, "", testConsentMove()); err == nil {
		t.Fatal("Raise with no tenant succeeded")
	}
}

func TestPlacementConsent_RaiseCreatesOnceAndKeepsTheAnswer(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestConsentLedger(t)
	ctx := context.Background()
	move := testConsentMove()

	first, err := ledger.Raise(ctx, consentTenant, move)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if first.State != ConsentPending || first.ID != move.RequestID(consentTenant) ||
		first.TenantID != consentTenant || first.Move != move || first.By != "" {
		t.Fatalf("Raise = %+v, want a pending request for the move", first)
	}

	again, err := ledger.Raise(ctx, consentTenant, move)
	if err != nil {
		t.Fatalf("second Raise: %v", err)
	}
	if again.ID != first.ID || again.State != ConsentPending || !again.RaisedAt.Equal(first.RaisedAt) {
		t.Fatalf("second Raise = %+v, want the stored request %+v", again, first)
	}

	if _, err := ledger.Answer(ctx, consentTenant, first.ID, ConsentAnswer{State: ConsentGranted, By: "alice"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	after, err := ledger.Raise(ctx, consentTenant, move)
	if err != nil {
		t.Fatalf("Raise after answer: %v", err)
	}
	if after.State != ConsentGranted || after.By != "alice" {
		t.Fatalf("Raise after answer = %+v, want the granted request", after)
	}
	got, err := ledger.Get(ctx, consentTenant, first.ID)
	if err != nil || got.State != ConsentGranted {
		t.Fatalf("Get after re-raise = %+v, %v; want granted", got, err)
	}
}

func TestPlacementConsent_ConcurrentRaisesStoreOneRequest(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestConsentLedger(t)
	ctx := context.Background()
	move := testConsentMove()

	const raisers = 16
	results := make([]*PlacementConsentRequest, raisers)
	errs := make([]error, raisers)
	var wg sync.WaitGroup
	for i := 0; i < raisers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = ledger.Raise(ctx, consentTenant, move)
		}(i)
	}
	wg.Wait()

	for i := 0; i < raisers; i++ {
		if errs[i] != nil {
			t.Fatalf("raiser %d: %v", i, errs[i])
		}
		if results[i].ID != results[0].ID || !results[i].RaisedAt.Equal(results[0].RaisedAt) {
			t.Fatalf("raiser %d saw %+v, raiser 0 saw %+v", i, results[i], results[0])
		}
	}
	all, err := ledger.List(ctx, consentTenant, "")
	if err != nil || len(all) != 1 {
		t.Fatalf("List = %d requests, %v; want one", len(all), err)
	}
}

func TestPlacementConsent_AnswerSwapsPendingOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, state := range []ConsentState{ConsentGranted, ConsentRefused} {
		ledger, _ := newTestConsentLedger(t)
		req, err := ledger.Raise(ctx, consentTenant, testConsentMove())
		if err != nil {
			t.Fatalf("Raise: %v", err)
		}
		answered, err := ledger.Answer(ctx, consentTenant, req.ID, ConsentAnswer{State: state, By: "alice", Reason: "on call"})
		if err != nil {
			t.Fatalf("Answer %s: %v", state, err)
		}
		if answered.State != state || answered.By != "alice" || answered.Reason != "on call" || answered.AnsweredAt.IsZero() {
			t.Fatalf("Answer %s = %+v", state, answered)
		}
		got, err := ledger.Get(ctx, consentTenant, req.ID)
		if err != nil || got.State != state || got.By != "alice" || got.Reason != "on call" {
			t.Fatalf("Get after %s = %+v, %v", state, got, err)
		}

		// A second answer, either way, is refused and changes nothing.
		for _, again := range []ConsentState{ConsentGranted, ConsentRefused} {
			_, err := ledger.Answer(ctx, consentTenant, req.ID, ConsentAnswer{State: again, By: "bob"})
			var notPending *ConsentNotPendingError
			if !errors.As(err, &notPending) || !errors.Is(err, ErrConsentNotPending) || notPending.State != state {
				t.Fatalf("re-answer %s after %s = %v, want *ConsentNotPendingError", again, state, err)
			}
		}
		got, _ = ledger.Get(ctx, consentTenant, req.ID)
		if got.State != state || got.By != "alice" {
			t.Fatalf("re-answer changed the request: %+v", got)
		}
	}
}

func TestPlacementConsent_AnswerRefusesABadAnswer(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestConsentLedger(t)
	ctx := context.Background()
	req, err := ledger.Raise(ctx, consentTenant, testConsentMove())
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	for name, answer := range map[string]ConsentAnswer{
		"no decider":       {State: ConsentGranted},
		"pending":          {State: ConsentPending, By: "alice"},
		"incoherent":       {State: ConsentIncoherent, By: "alice"},
		"unknown decision": {State: "maybe", By: "alice"},
	} {
		if _, err := ledger.Answer(ctx, consentTenant, req.ID, answer); !errors.Is(err, ErrConsentInvalid) {
			t.Fatalf("%s: Answer = %v, want ErrConsentInvalid", name, err)
		}
	}

	_, err = ledger.Answer(ctx, consentTenant, testConsentMove().RequestID(consentOtherTenant), ConsentAnswer{State: ConsentGranted, By: "alice"})
	var notFound *ConsentNotFoundError
	if !errors.As(err, &notFound) || !errors.Is(err, ErrConsentNotFound) {
		t.Fatalf("Answer of an unknown id = %v, want *ConsentNotFoundError", err)
	}
	if _, err := ledger.Answer(ctx, consentOtherTenant, req.ID, ConsentAnswer{State: ConsentGranted, By: "alice"}); !errors.Is(err, ErrConsentNotFound) {
		t.Fatalf("Answer from another tenant = %v, want ErrConsentNotFound", err)
	}

	got, err := ledger.Get(ctx, consentTenant, req.ID)
	if err != nil || got.State != ConsentPending || got.By != "" {
		t.Fatalf("refused answers changed the request: %+v, %v", got, err)
	}
}

func TestPlacementConsent_AnswerThatLosesTheSwapIsStale(t *testing.T) {
	t.Parallel()
	ledger, kv := newTestConsentLedger(t)
	ctx := context.Background()
	req, err := ledger.Raise(ctx, consentTenant, testConsentMove())
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	// Another answer lands between this answer's read and its swap.
	kv.beforeSwap = func() {
		kv.beforeSwap = nil
		if _, err := ledger.Answer(ctx, consentTenant, req.ID, ConsentAnswer{State: ConsentRefused, By: "bob"}); err != nil {
			t.Errorf("interleaved Answer: %v", err)
		}
	}
	_, err = ledger.Answer(ctx, consentTenant, req.ID, ConsentAnswer{State: ConsentGranted, By: "alice"})
	var stale *ConsentStaleError
	if !errors.As(err, &stale) || !errors.Is(err, ErrConsentStale) {
		t.Fatalf("Answer that lost the swap = %v, want *ConsentStaleError", err)
	}
	got, err := ledger.Get(ctx, consentTenant, req.ID)
	if err != nil || got.State != ConsentRefused || got.By != "bob" {
		t.Fatalf("after the lost swap = %+v, %v; want bob's refusal", got, err)
	}
}

func TestPlacementConsent_NewGenerationVoidsTheGrant(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestConsentLedger(t)
	ctx := context.Background()
	move := testConsentMove()

	req, err := ledger.Raise(ctx, consentTenant, move)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if ok, err := ledger.Granted(ctx, consentTenant, move); ok || err != nil {
		t.Fatalf("Granted while pending = %v, %v", ok, err)
	}
	if _, err := ledger.Answer(ctx, consentTenant, req.ID, ConsentAnswer{State: ConsentGranted, By: "alice"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ok, err := ledger.Granted(ctx, consentTenant, move); !ok || err != nil {
		t.Fatalf("Granted after grant = %v, %v", ok, err)
	}

	next := move
	next.SpecGeneration++
	if next.RequestID(consentTenant) == req.ID {
		t.Fatal("a new generation kept the request id")
	}
	if ok, err := ledger.Granted(ctx, consentTenant, next); ok || err != nil {
		t.Fatalf("Granted for generation N+1 = %v, %v; want false", ok, err)
	}
	raised, err := ledger.Raise(ctx, consentTenant, next)
	if err != nil || raised.State != ConsentPending {
		t.Fatalf("Raise for generation N+1 = %+v, %v; want pending", raised, err)
	}
	if ok, _ := ledger.Granted(ctx, consentTenant, move); !ok {
		t.Fatal("the old generation's grant was lost")
	}
	if ok, err := ledger.Granted(ctx, consentOtherTenant, move); ok || err != nil {
		t.Fatalf("Granted in another tenant = %v, %v; want false", ok, err)
	}
}

func TestPlacementConsent_RecreatedSpecDoesNotInheritAGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestPlacementStore(t)
	ledger, _ := newTestConsentLedger(t)

	created, err := store.DeclarePlacement(ctx, consentTenant, validPlacementSpec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	move := testConsentMove()
	move.SpecName = created.Name
	move.SpecGeneration = created.Generation
	req, err := ledger.Raise(ctx, consentTenant, move)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if _, err := ledger.Answer(ctx, consentTenant, req.ID, ConsentAnswer{State: ConsentGranted, By: "alice"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if err := store.WithdrawPlacement(ctx, consentTenant, created.Name); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	recreated, err := store.DeclarePlacement(ctx, consentTenant, validPlacementSpec())
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}

	again := move
	again.SpecGeneration = recreated.Generation
	if again.RequestID(consentTenant) == req.ID {
		t.Fatalf("the recreated spec (generation %d) kept the old grant's request id", recreated.Generation)
	}
	if ok, err := ledger.Granted(ctx, consentTenant, again); ok || err != nil {
		t.Fatalf("Granted for the recreated spec = %v, %v; want false", ok, err)
	}
}

func TestPlacementConsent_UnreadableLedgerNeverGrants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	move := testConsentMove()

	t.Run("read fails", func(t *testing.T) {
		ledger, kv := newTestConsentLedger(t)
		req, err := ledger.Raise(ctx, consentTenant, move)
		if err != nil {
			t.Fatalf("Raise: %v", err)
		}
		if _, err := ledger.Answer(ctx, consentTenant, req.ID, ConsentAnswer{State: ConsentGranted, By: "alice"}); err != nil {
			t.Fatalf("Answer: %v", err)
		}
		kv.getErr = errors.New("disk on fire")
		if ok, err := ledger.Granted(ctx, consentTenant, move); ok || err == nil {
			t.Fatalf("Granted on a failing read = %v, %v; want false and an error", ok, err)
		}
		if _, err := ledger.Get(ctx, consentTenant, req.ID); err == nil {
			t.Fatal("Get on a failing read succeeded")
		}
	})

	t.Run("bytes do not decode", func(t *testing.T) {
		ledger, kv := newTestConsentLedger(t)
		key, err := consentKey(consentTenant, move.RequestID(consentTenant))
		if err != nil {
			t.Fatalf("consentKey: %v", err)
		}
		if err := kv.Put(ctx, key, []byte("{not json")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if ok, err := ledger.Granted(ctx, consentTenant, move); ok || err == nil {
			t.Fatalf("Granted on undecodable bytes = %v, %v; want false and an error", ok, err)
		}
		if _, err := ledger.Raise(ctx, consentTenant, move); err == nil {
			t.Fatal("Raise over undecodable bytes succeeded")
		}
		if _, err := ledger.List(ctx, consentTenant, ""); err == nil {
			t.Fatal("List over undecodable bytes succeeded")
		}
	})

	t.Run("move does not hash to its id", func(t *testing.T) {
		ledger, kv := newTestConsentLedger(t)
		id := move.RequestID(consentTenant)
		key, err := consentKey(consentTenant, id)
		if err != nil {
			t.Fatalf("consentKey: %v", err)
		}
		forged := move
		forged.WorkID = "someone-else"
		data, err := json.Marshal(&PlacementConsentRequest{
			ID: id, TenantID: consentTenant, Move: forged, State: ConsentGranted, By: "mallory",
		})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if err := kv.Put(ctx, key, data); err != nil {
			t.Fatalf("Put: %v", err)
		}

		ok, err := ledger.Granted(ctx, consentTenant, move)
		if ok || !errors.Is(err, ErrConsentIncoherent) {
			t.Fatalf("Granted on a forged request = %v, %v; want false and ErrConsentIncoherent", ok, err)
		}
		got, err := ledger.Get(ctx, consentTenant, id)
		if err != nil || got.State != ConsentIncoherent {
			t.Fatalf("Get of a forged request = %+v, %v; want incoherent", got, err)
		}
		if _, err := ledger.Raise(ctx, consentTenant, move); !errors.Is(err, ErrConsentIncoherent) {
			t.Fatalf("Raise over a forged request = %v, want ErrConsentIncoherent", err)
		}
		_, err = ledger.Answer(ctx, consentTenant, id, ConsentAnswer{State: ConsentGranted, By: "alice"})
		var notPending *ConsentNotPendingError
		if !errors.As(err, &notPending) || notPending.State != ConsentIncoherent {
			t.Fatalf("Answer of a forged request = %v, want *ConsentNotPendingError (incoherent)", err)
		}
		listed, err := ledger.List(ctx, consentTenant, ConsentIncoherent)
		if err != nil || len(listed) != 1 || listed[0].ID != id {
			t.Fatalf("List incoherent = %+v, %v", listed, err)
		}
	})
}

func TestPlacementConsent_ListFiltersAndIsolatesTenants(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestConsentLedger(t)
	ctx := context.Background()

	var ids []string
	for _, work := range []string{"s1", "s2", "s3"} {
		move := testConsentMove()
		move.WorkID = work
		req, err := ledger.Raise(ctx, consentTenant, move)
		if err != nil {
			t.Fatalf("Raise %s: %v", work, err)
		}
		ids = append(ids, req.ID)
	}
	if _, err := ledger.Raise(ctx, consentOtherTenant, testConsentMove()); err != nil {
		t.Fatalf("Raise in the other tenant: %v", err)
	}
	if _, err := ledger.Answer(ctx, consentTenant, ids[0], ConsentAnswer{State: ConsentGranted, By: "alice"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if _, err := ledger.Answer(ctx, consentTenant, ids[1], ConsentAnswer{State: ConsentRefused, By: "alice"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	all, err := ledger.List(ctx, consentTenant, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("List all = %d, %v; want 3", len(all), err)
	}
	for i := range all {
		if all[i].TenantID != consentTenant {
			t.Fatalf("List returned another tenant's request: %+v", all[i])
		}
		if i > 0 && all[i-1].ID >= all[i].ID {
			t.Fatalf("List not ordered by id: %q then %q", all[i-1].ID, all[i].ID)
		}
	}
	for state, want := range map[ConsentState]string{ConsentGranted: ids[0], ConsentRefused: ids[1], ConsentPending: ids[2]} {
		got, err := ledger.List(ctx, consentTenant, state)
		if err != nil || len(got) != 1 || got[0].ID != want {
			t.Fatalf("List %s = %+v, %v; want %s", state, got, err, want)
		}
	}
	if got, err := ledger.List(ctx, consentTenant, ConsentIncoherent); err != nil || len(got) != 0 {
		t.Fatalf("List incoherent = %+v, %v; want none", got, err)
	}
	if _, err := ledger.List(ctx, consentTenant, "maybe"); !errors.Is(err, ErrConsentInvalid) {
		t.Fatalf("List of an unknown state = %v, want ErrConsentInvalid", err)
	}
	other, err := ledger.List(ctx, consentOtherTenant, "")
	if err != nil || len(other) != 1 || other[0].TenantID != consentOtherTenant {
		t.Fatalf("List other tenant = %+v, %v", other, err)
	}
	if _, err := ledger.Get(ctx, consentOtherTenant, ids[0]); !errors.Is(err, ErrConsentNotFound) {
		t.Fatalf("Get across tenants = %v, want ErrConsentNotFound", err)
	}
}

func TestPlacementConsent_ReturnedRequestsAreCopies(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestConsentLedger(t)
	ctx := context.Background()
	req, err := ledger.Raise(ctx, consentTenant, testConsentMove())
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	req.State = ConsentGranted
	req.By = "mallory"
	if ok, err := ledger.Granted(ctx, consentTenant, testConsentMove()); ok || err != nil {
		t.Fatalf("mutating a returned request granted the move: %v, %v", ok, err)
	}
}
