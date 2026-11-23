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
	stderrors "errors"
	"sync/atomic"
	"testing"
	"time"
)

// assertNoTurnAdmissions checks that acct-1 has no stale admission and no
// admission record at all left in the ledger's store.
func assertNoTurnAdmissions(t *testing.T, ctx context.Context, ledger ConsumptionLedger) {
	t.Helper()
	store, ok := ledger.(turnAdmissionStore)
	if !ok {
		t.Fatalf("ledger %T keeps no turn admissions", ledger)
	}
	n, err := store.StaleTurnAdmissions(ctx, "tenant-a", "acct-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("StaleTurnAdmissions: %v", err)
	}
	if n != 0 {
		t.Errorf("stale turn admissions = %d, want 0", n)
	}
	kv, ok := ledger.(*kvConsumptionLedger)
	if !ok {
		t.Fatalf("ledger %T is not the KV ledger", ledger)
	}
	var keys []string
	if err := kv.kv.Scan(ctx, turnAdmissionKeyspace+"tenant-a/acct-1/", func(key string, _ []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("turn admission records left = %v, want none", keys)
	}
}

func TestPromptSession_OverlappingTurnsThroughTwoNodesAreChargedInTurn(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	harness.service.SetConsumptionLedger(harness.ledger)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)
	// 950 of 1000 spent: the first turn is admitted, and its 150 tokens cross the cap.
	harness.spendAccount1(t, 950)

	nodeB := newTurnSpendNodeB(t, harness, NewAgentStore(harness.kv), sessionID)
	nodeB.SetConsumptionLedger(harness.ledger)
	t.Cleanup(func() {
		harness.service.pendingLedger.stopDrainer()
		nodeB.pendingLedger.stopDrainer()
	})

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var turns atomic.Int32
	harness.backend.beforePrompt = func() {
		if turns.Add(1) == 1 {
			entered <- struct{}{}
			<-release
		}
	}

	first := make(chan error, 1)
	go func() { first <- harness.prompt(ctx, sessionID) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("node a's turn never reached the backend")
	}

	second := make(chan error, 1)
	go func() {
		_, err := nodeB.PromptSession(ctx, &PromptSessionRequest{
			SessionID: sessionID, Backend: "usage", ProjectID: "proj-1", Prompt: "do the thing",
		})
		second <- err
	}()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := turns.Load(); got != 1 {
			close(release)
			t.Fatalf("backend ran %d turns while node a's turn was running — node b admitted its turn before node a's was charged", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)

	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("node a's turn = %v, want it admitted under the cap", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("node a's turn never returned")
	}
	select {
	case err := <-second:
		var exhausted *AccountQuotaExhaustedError
		if !stderrors.As(err, &exhausted) {
			t.Fatalf("node b's turn = %v, want an *AccountQuotaExhaustedError — node a's turn spent the account", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("node b's turn never returned")
	}
	if got := turns.Load(); got != 1 {
		t.Errorf("backend ran %d turns, want 1 — the refused turn must not run", got)
	}
	assertNoTurnAdmissions(t, ctx, harness.ledger)
}

// admissionFaultLedger forwards everything to the KV ledger, but its
// admission store cannot be reached.
type admissionFaultLedger struct {
	ConsumptionLedger
	pendingConsumptionStore
	turnAdmissionStore
}

func (l *admissionFaultLedger) AcquireTurnAdmission(context.Context, string, string, string, string, time.Time) ([]byte, error) {
	return nil, stderrors.New("admission store unreachable")
}

func TestPromptSession_AdmissionStoreFaultRefusesTheTurnUncharged(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	harness.service.SetConsumptionLedger(&admissionFaultLedger{
		ConsumptionLedger:       harness.ledger,
		pendingConsumptionStore: harness.ledger.(pendingConsumptionStore),
		turnAdmissionStore:      harness.ledger.(turnAdmissionStore),
	})
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)

	var turns atomic.Int32
	harness.backend.beforePrompt = func() { turns.Add(1) }

	err := harness.prompt(ctx, sessionID)
	var unavailable *SpendCheckUnavailableError
	if !stderrors.As(err, &unavailable) {
		t.Fatalf("turn with an unreachable admission store = %v, want a *SpendCheckUnavailableError", err)
	}
	if got := turns.Load(); got != 0 {
		t.Errorf("backend ran %d turns, want 0 — a turn that cannot be admitted must not run", got)
	}
}
