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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestConsumptionErrorMessagesNameTheirSubject checks what an operator actually
// reads. These strings are how a double charge or a stalled account is
// diagnosed, so each one must carry the identifier the next question is asked
// with — not merely be non-empty.
func TestConsumptionErrorMessagesNameTheirSubject(t *testing.T) {
	cause := stderrors.New("boom")

	tests := []struct {
		name     string
		err      error
		sentinel error
		contains []string
	}{
		{
			name:     "invalid",
			err:      &ConsumptionInvalidError{Field: "occurred_at", Message: "must be set"},
			sentinel: ErrConsumptionInvalid,
			contains: []string{"occurred_at", "must be set"},
		},
		{
			name:     "conflict",
			err:      &ConsumptionConflictError{EntryID: "turn-7", Plane: consumptionPlaneWindow, Key: "orch/costwindow/t/a/x"},
			sentinel: ErrConsumptionConflict,
			contains: []string{"turn-7", consumptionPlaneWindow, "orch/costwindow/t/a/x"},
		},
		{
			name:     "write with cause",
			err:      &ConsumptionWriteError{EntryID: "turn-7", Plane: consumptionPlaneChain, Key: "orch/costchain/t", Cause: cause},
			sentinel: ErrConsumptionWrite,
			contains: []string{"turn-7", consumptionPlaneChain, "orch/costchain/t", "boom"},
		},
		{
			name:     "write without cause",
			err:      &ConsumptionWriteError{EntryID: "turn-7", Plane: consumptionPlaneChain, Key: "orch/costchain/t"},
			sentinel: ErrConsumptionWrite,
			contains: []string{"turn-7", "orch/costchain/t"},
		},
		{
			name:     "read with cause and message",
			err:      &ConsumptionReadError{Key: "orch/costchain/t/p", Message: "subtree scan failed", Cause: cause},
			sentinel: ErrConsumptionRead,
			contains: []string{"orch/costchain/t/p", "subtree scan failed", "boom"},
		},
		{
			name:     "read bare",
			err:      &ConsumptionReadError{Key: "orch/costchain/t/p"},
			sentinel: ErrConsumptionRead,
			contains: []string{"orch/costchain/t/p"},
		},
		{
			name:     "unguarded",
			err:      &ConsumptionUnguardedError{Message: "this one cannot"},
			sentinel: ErrConsumptionUnguarded,
			contains: []string{"this one cannot"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			message := tc.err.Error()
			for _, want := range tc.contains {
				if !strings.Contains(message, want) {
					t.Fatalf("message %q must name %q", message, want)
				}
			}
			if !stderrors.Is(tc.err, tc.sentinel) {
				t.Fatalf("%q must unwrap to its sentinel", message)
			}
			// Each sentinel is distinct: a caller switching on them must not
			// find a write failure matching the read sentinel.
			for _, other := range []error{
				ErrConsumptionInvalid, ErrConsumptionConflict, ErrConsumptionWrite,
				ErrConsumptionRead, ErrConsumptionUnguarded,
			} {
				if other != tc.sentinel && stderrors.Is(tc.err, other) {
					t.Fatalf("%q matches an unrelated sentinel %v", message, other)
				}
			}
		})
	}
}

func TestNewInMemoryConsumptionLedgerIsGuarded(t *testing.T) {
	// The convenience constructor must pick a store that can decide a create;
	// one that could not would make every Record refuse.
	ledger := NewInMemoryConsumptionLedger()
	if err := ledger.Record(context.Background(), validEntry()); err != nil {
		t.Fatalf("in-memory ledger must accept a valid entry, got: %v", err)
	}
}

func TestConsumptionLedgerRefusesNilStore(t *testing.T) {
	ledger := NewConsumptionLedger(nil)
	ctx := context.Background()

	if err := ledger.Record(ctx, validEntry()); err == nil {
		t.Fatal("Record over a nil store must refuse")
	}
	if _, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"}); err == nil {
		t.Fatal("Rollup over a nil store must refuse")
	}
	if _, err := ledger.WindowUsage(ctx, "t1", "a1", ledgerBase, ledgerBase.Add(time.Hour)); err == nil {
		t.Fatal("WindowUsage over a nil store must refuse")
	}
}

// TestConsumptionLedgerRefusesUnreadableExistingRecord covers the narrow path
// where the guarded create reports the key is taken and the record behind it
// cannot then be read.
//
// Neither available answer is safe to guess. Treating it as a duplicate would
// discard a charge that may be different; treating it as a conflict would fail
// a legitimate retry forever. It refuses and names the key, which is the only
// answer that leads somewhere.
func TestConsumptionLedgerRefusesUnreadableExistingRecord(t *testing.T) {
	kv := &takenButUnreadableKV{KVStore: NewMemoryTaskKV()}
	ledger := NewConsumptionLedger(kv)

	err := ledger.Record(context.Background(), validEntry())
	var readErr *ConsumptionReadError
	if !stderrors.As(err, &readErr) {
		t.Fatalf("want ConsumptionReadError, got: %v", err)
	}
	if !strings.Contains(readErr.Message, validEntry().EntryID) {
		t.Fatalf("the refusal must name the entry it could not settle: %q", readErr.Message)
	}
}

// takenButUnreadableKV reports every key as already present and then fails the
// read-back.
type takenButUnreadableKV struct {
	interfaces.KVStore
}

func (k *takenButUnreadableKV) PutIfAbsent(context.Context, string, []byte) (bool, error) {
	return false, nil
}

func (k *takenButUnreadableKV) Get(context.Context, string) ([]byte, error) {
	return nil, stderrors.New("record unreadable")
}
