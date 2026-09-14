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

package cas_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// These tests run against a real qrdb node, under the Unit Test Contract's
// consensus-arbitration carve-out: the property under test is that the STORE
// decides a race, and a double that agrees with this package would prove
// nothing about it.

// TestMain tears the shared node down when the package is finished with it.
func TestMain(m *testing.M) {
	code := m.Run()
	qrdbnode.CloseShared()
	os.Exit(code)
}

// keys hands out an identifier no other test in this package uses.
var keys atomic.Uint64

func uniqueKey(prefix string) string {
	return fmt.Sprintf("__sys:cas-test/%s-%d", prefix, keys.Add(1))
}

// writer builds a Writer over the shared node.
func writer(t *testing.T) *cas.Writer {
	t.Helper()
	n := qrdbnode.Shared(t)
	w, err := cas.NewWriter(n.Transaction, n.KV, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	return w
}

// alwaysTake supersedes whatever it is shown.
func alwaysTake(_ []byte) (cas.Decision, error) { return cas.Take, nil }

// =============================================================================
// Construction
// =============================================================================

// TestNewWriter_FailsClosedOnAMissingPlane proves an incompletely wired Writer
// is refused rather than accepted and degraded — one that could read but not
// transact would arbitrate nothing while looking functional.
func TestNewWriter_FailsClosedOnAMissingPlane(t *testing.T) {
	n := qrdbnode.Shared(t)

	if _, err := cas.NewWriter(nil, n.KV, nil); !errors.Is(err, cas.ErrNilTransactor) {
		t.Errorf("a Writer with no transactor was accepted (err = %v)", err)
	}
	if _, err := cas.NewWriter(n.Transaction, nil, nil); !errors.Is(err, cas.ErrNilReader) {
		t.Errorf("a Writer with no reader was accepted (err = %v)", err)
	}
}

// =============================================================================
// Create and Swap
// =============================================================================

// TestCreate_OnlyTheFirstWriterWins covers the create guard against the real
// transaction plane.
func TestCreate_OnlyTheFirstWriterWins(t *testing.T) {
	w := writer(t)
	ctx := context.Background()
	key := uniqueKey("create")

	won, err := w.Create(ctx, key, []byte("first"))
	if err != nil || !won {
		t.Fatalf("first create: won=%v err=%v", won, err)
	}

	won, err = w.Create(ctx, key, []byte("second"))
	if err != nil {
		t.Fatalf("a refused create is a verdict, not an error: %v", err)
	}
	if won {
		t.Error("a second create of the same key reported success")
	}
}

// TestCreate_ConcurrentWritersProduceExactlyOneWinner is the race the whole
// package exists for.
func TestCreate_ConcurrentWritersProduceExactlyOneWinner(t *testing.T) {
	w := writer(t)
	ctx := context.Background()
	key := uniqueKey("race")

	const contenders = 8
	var wins, failures atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			won, err := w.Create(ctx, key, []byte(fmt.Sprintf("writer-%d", n)))
			if err != nil {
				failures.Add(1)
				return
			}
			if won {
				wins.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if got := failures.Load(); got != 0 {
		t.Errorf("%d writers errored; contention is a verdict, not a failure", got)
	}
	if got := wins.Load(); got != 1 {
		t.Fatalf("%d writers won the same key, want exactly 1", got)
	}
}

// TestSwap_RefusesAgainstBytesThatMoved proves the guard is the value, so a
// writer holding a stale view loses.
func TestSwap_RefusesAgainstBytesThatMoved(t *testing.T) {
	w := writer(t)
	ctx := context.Background()
	key := uniqueKey("swap")

	if _, err := w.Create(ctx, key, []byte("v1")); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	won, err := w.Swap(ctx, key, []byte("v1"), []byte("v2"))
	if err != nil || !won {
		t.Fatalf("swap against the current value: won=%v err=%v", won, err)
	}

	// "v1" is what a stale reader would still be holding.
	won, err = w.Swap(ctx, key, []byte("v1"), []byte("v3"))
	if err != nil {
		t.Fatalf("a refused swap is a verdict, not an error: %v", err)
	}
	if won {
		t.Error("a swap guarded on stale bytes succeeded")
	}
}

// =============================================================================
// Handover
// =============================================================================

// TestHandover_CreatesWhenNobodyHolds covers the create path, and proves decide
// is not consulted when there is no holder to supersede.
func TestHandover_CreatesWhenNobodyHolds(t *testing.T) {
	w := writer(t)
	consulted := 0
	won, err := w.Handover(context.Background(), uniqueKey("handover"), []byte("v1"),
		func([]byte) (cas.Decision, error) {
			consulted++
			return cas.Take, nil
		})
	if err != nil || !won {
		t.Fatalf("handover on an unheld key: won=%v err=%v", won, err)
	}
	if consulted != 0 {
		t.Errorf("decide was consulted %d times with no holder to supersede", consulted)
	}
}

// TestHandover_DeclineIsTerminalAndIsNotAnError pins the behaviour a high-water
// mark depends on: a decline ends the call, reports a loss, and never retries.
//
// This is what lets one loop serve both record kinds. The scheduler's firing
// claim gets its no-retry rule from here — by declining — rather than from a
// special case in the loop.
func TestHandover_DeclineIsTerminalAndIsNotAnError(t *testing.T) {
	w := writer(t)
	ctx := context.Background()
	key := uniqueKey("decline")

	if _, err := w.Create(ctx, key, []byte("held")); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	consulted := 0
	won, err := w.Handover(ctx, key, []byte("challenger"), func([]byte) (cas.Decision, error) {
		consulted++
		return cas.Decline, nil
	})
	if err != nil {
		t.Fatalf("a decline is a verdict, not an error: %v", err)
	}
	if won {
		t.Fatal("a declining writer was granted the record")
	}
	if consulted != 1 {
		t.Errorf("decide was consulted %d times; a decline must end the call", consulted)
	}
}

// TestHandover_DecideErrorFailsClosed proves an undecidable handover grants
// nothing.
func TestHandover_DecideErrorFailsClosed(t *testing.T) {
	w := writer(t)
	ctx := context.Background()
	key := uniqueKey("undecidable")
	if _, err := w.Create(ctx, key, []byte("held")); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	sentinel := errors.New("cannot decide")
	won, err := w.Handover(ctx, key, []byte("challenger"), func([]byte) (cas.Decision, error) {
		return cas.Take, sentinel
	})
	if won {
		t.Fatal("a record was taken on a decision that could not be made")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want the decide function's own error", err)
	}
}

// stubbornReader serves prepared answers for one key and passes the rest
// through, so the interrupted-takeover paths are driven rather than raced.
type stubbornReader struct {
	cas.Reader

	key     string
	answers [][]byte
	served  int
}

func (r *stubbornReader) Get(ctx context.Context, key string, opts ...transport.ReadOption) ([]byte, error) {
	if key == r.key && r.served < len(r.answers) {
		answer := r.answers[r.served]
		r.served++
		return answer, nil
	}
	return r.Reader.Get(ctx, key, opts...)
}

// TestHandover_AnInterruptedTakeoverCompletes is the property a lease needs and
// a mark does not.
//
// The writer reads "v1", but by the time its swap is proposed another writer
// has moved the record to "v2". Its guard fails. Standing down there would
// leave the record with a holder this writer supersedes, so the loop re-reads
// and asks decide again.
func TestHandover_AnInterruptedTakeoverCompletes(t *testing.T) {
	n := qrdbnode.Shared(t)
	ctx := context.Background()
	key := uniqueKey("interrupted")

	live, err := cas.NewWriter(n.Transaction, n.KV, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := live.Create(ctx, key, []byte("v1")); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := live.Swap(ctx, key, []byte("v1"), []byte("v2")); err != nil {
		t.Fatalf("advancing: %v", err)
	}

	interrupted, err := cas.NewWriter(n.Transaction,
		&stubbornReader{Reader: n.KV, key: key, answers: [][]byte{[]byte("v1")}}, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	won, err := interrupted.Handover(ctx, key, []byte("v3"), alwaysTake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !won {
		t.Fatal("the writer stood down after one interrupted swap, leaving a holder it supersedes")
	}
}

// TestHandover_AnUnchangingRecordFailsClosed covers the stall guard.
//
// A guard compared against the exact bytes just read can only fail if something
// wrote, so a refusal against an unchanged record is a contradiction. The loop
// stops, and stops without granting.
func TestHandover_AnUnchangingRecordFailsClosed(t *testing.T) {
	n := qrdbnode.Shared(t)
	ctx := context.Background()
	key := uniqueKey("stalled")

	live, err := cas.NewWriter(n.Transaction, n.KV, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := live.Create(ctx, key, []byte("v1")); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := live.Swap(ctx, key, []byte("v1"), []byte("v2")); err != nil {
		t.Fatalf("advancing: %v", err)
	}

	answers := make([][]byte, cas.MaxAttempts+1)
	for i := range answers {
		answers[i] = []byte("v1")
	}
	stalled, err := cas.NewWriter(n.Transaction,
		&stubbornReader{Reader: n.KV, key: key, answers: answers}, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	won, err := stalled.Handover(ctx, key, []byte("v3"), alwaysTake)
	if won {
		t.Fatal("a record was granted without a write ever succeeding")
	}
	if !errors.Is(err, cas.ErrStalled) {
		t.Fatalf("error = %v, want ErrStalled", err)
	}
}

// TestHandover_AnEndlesslyInterruptedTakeoverGivesUp pins the attempt bound.
//
// Every attempt sees a record that genuinely changed, so the stall guard never
// fires and only the bound stops the loop — fail-closed, because a writer that
// never landed its write does not hold the record.
func TestHandover_AnEndlesslyInterruptedTakeoverGivesUp(t *testing.T) {
	n := qrdbnode.Shared(t)
	ctx := context.Background()
	key := uniqueKey("contended")

	live, err := cas.NewWriter(n.Transaction, n.KV, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := live.Create(ctx, key, []byte("current")); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	answers := make([][]byte, cas.MaxAttempts+1)
	for i := range answers {
		answers[i] = []byte(fmt.Sprintf("stale-%d", i))
	}
	contended, err := cas.NewWriter(n.Transaction,
		&stubbornReader{Reader: n.KV, key: key, answers: answers}, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	won, err := contended.Handover(ctx, key, []byte("next"), alwaysTake)
	if won {
		t.Fatal("a record was granted without a write ever succeeding")
	}
	if !errors.Is(err, cas.ErrContended) {
		t.Fatalf("error = %v, want ErrContended", err)
	}
}

// TestHandover_AnEmptyRecordFailsClosed covers the contradiction where the
// create guard says the key exists but it reads as nothing.
func TestHandover_AnEmptyRecordFailsClosed(t *testing.T) {
	n := qrdbnode.Shared(t)
	ctx := context.Background()
	key := uniqueKey("empty")

	live, err := cas.NewWriter(n.Transaction, n.KV, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := live.Create(ctx, key, []byte("held")); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	blind, err := cas.NewWriter(n.Transaction,
		&stubbornReader{Reader: n.KV, key: key, answers: [][]byte{{}}}, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	won, err := blind.Handover(ctx, key, []byte("next"), alwaysTake)
	if won {
		t.Fatal("a record was taken from a holder that could not be read")
	}
	if !errors.Is(err, cas.ErrUnreadable) {
		t.Fatalf("error = %v, want ErrUnreadable", err)
	}
}

// =============================================================================
// Error vocabulary
// =============================================================================

// TestWriter_WrapKeepsTheCallerSErrorVocabulary proves each call site can keep
// its own error type: the wrap function is given the step that failed and the
// cause, and its result is what the caller sees.
func TestWriter_WrapKeepsTheCallerSErrorVocabulary(t *testing.T) {
	n := qrdbnode.Shared(t)
	ctx := context.Background()
	key := uniqueKey("wrapped")

	seed, err := cas.NewWriter(n.Transaction, n.KV, nil)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := seed.Create(ctx, key, []byte("held")); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	wrapped, err := cas.NewWriter(n.Transaction,
		&stubbornReader{Reader: n.KV, key: key, answers: [][]byte{{}}},
		func(op string, cause error) error {
			return fmt.Errorf("house-style[%s]: %w", op, cause)
		})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	_, err = wrapped.Handover(ctx, key, []byte("next"), alwaysTake)
	if !errors.Is(err, cas.ErrUnreadable) {
		t.Fatalf("the wrapped error lost its cause: %v", err)
	}
	if !strings.Contains(err.Error(), "house-style[read]") {
		t.Errorf("error %v does not carry the caller's vocabulary or name the failing step", err)
	}
}

// =============================================================================
// Backend failures
// =============================================================================

// brokenTransactor fails every proposal.
type brokenTransactor struct{ err error }

func (b *brokenTransactor) ExecuteTransaction(_ context.Context, _ *transport.TransactionRequest, _ ...transport.WriteOption) (*transport.TransactionResponse, error) {
	return nil, b.err
}

// brokenReader fails every read and passes nothing through.
type brokenReader struct {
	cas.Reader
	err error
}

func (b *brokenReader) Get(_ context.Context, _ string, _ ...transport.ReadOption) ([]byte, error) {
	return nil, b.err
}

// TestWriter_ABackendFailureIsNeverReportedAsAVerdict is the fail-open this
// package must not have.
//
// "won=false, err=nil" tells a caller it lost a race that was decided. A store
// that could not be reached decided nothing, and collapsing the two would let a
// caller treat an outage as an orderly loss — or, worse in the create path,
// leave it unable to tell a refused create from one that never happened.
func TestWriter_ABackendFailureIsNeverReportedAsAVerdict(t *testing.T) {
	n := qrdbnode.Shared(t)
	ctx := context.Background()
	unreachable := errors.New("store unreachable")

	t.Run("create", func(t *testing.T) {
		w, err := cas.NewWriter(&brokenTransactor{err: unreachable}, n.KV, nil)
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}
		won, err := w.Create(ctx, uniqueKey("broken"), []byte("v1"))
		if won {
			t.Fatal("an unreachable store reported a successful create")
		}
		if !errors.Is(err, unreachable) {
			t.Errorf("error = %v, want the backend's own failure", err)
		}
	})

	t.Run("swap", func(t *testing.T) {
		w, err := cas.NewWriter(&brokenTransactor{err: unreachable}, n.KV, nil)
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}
		won, err := w.Swap(ctx, uniqueKey("broken"), []byte("held"), []byte("next"))
		if won {
			t.Fatal("an unreachable store reported a successful swap")
		}
		if !errors.Is(err, unreachable) {
			t.Errorf("error = %v, want the backend's own failure", err)
		}
	})

	t.Run("handover read", func(t *testing.T) {
		key := uniqueKey("broken")
		seed, err := cas.NewWriter(n.Transaction, n.KV, nil)
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}
		if _, err := seed.Create(ctx, key, []byte("held")); err != nil {
			t.Fatalf("seeding: %v", err)
		}

		// The create is refused because the key exists, so the handover falls
		// through to the read — which is the step that fails here.
		w, err := cas.NewWriter(n.Transaction, &brokenReader{Reader: n.KV, err: unreachable}, nil)
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}
		won, err := w.Handover(ctx, key, []byte("next"), alwaysTake)
		if won {
			t.Fatal("a record was taken while its holder could not be read")
		}
		if !errors.Is(err, unreachable) {
			t.Errorf("error = %v, want the backend's own failure", err)
		}
	})
}
