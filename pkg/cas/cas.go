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

// Package cas provides guarded writes over go-qrdb's transaction plane: the
// compare-and-swap sequence that lets contending nodes agree on one winner.
//
// # What this is for
//
// Records that ARBITRATE — a scheduler firing claim, a run lease, a work claim
// — are decided by the store, never by the caller. A compare and a write
// evaluated together inside the state machine is a single Raft proposal, and
// that is the only construction under which two nodes racing one record produce
// one winner. A read followed by a write is two proposals with a window between
// them, and that window is the leader-churn race this package exists to close.
//
// # Why it lives here
//
// It was implemented twice before it was implemented once: pkg/scheduler's
// firing claim and pkg/sdlc's coordination leases each grew the same
// create/read/decide/swap sequence, differing only in what they decide and how
// they name their errors. Two spellings of one rule drift, and the drift is
// silent because both spellings keep passing their own tests. Callers supply
// the two things that genuinely differ — a Decide function and an error
// constructor — and share everything else.
//
// # The long-term home is go-qrdb
//
// This primitive belongs to the SDK that owns the transaction plane, tracked as
// go-qrdb#44. When it lands there, this package becomes a thin alias or goes
// away; the call sites should not have to change, which is why Decide and the
// wrap function are the whole contract.
package cas

import (
	"bytes"
	"context"
	"errors"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// Transactor decides a guarded write. It is go-qrdb's own
// transport.TransactionClient, aliased so the dependency reads as what it is.
type Transactor = transport.TransactionClient

// Reader supplies the record a guarded write compares against. It is go-qrdb's
// transport.KVClient.
//
// A separate read is needed because a transaction's branches carry only Put and
// Delete — an OpGet in the failure branch is rejected — so the held record
// cannot be fetched in the same proposal that fails to create it. The read is
// unguarded on purpose: it supplies the value the following transaction
// compares against, and if it is stale that compare fails and the write loses,
// which is the safe direction.
type Reader = transport.KVClient

var (
	// ErrNilTransactor reports a Writer built without a transactor. Such a
	// Writer could still read, so accepting one would let an incompletely
	// wired caller run and silently arbitrate nothing.
	ErrNilTransactor = errors.New("cas: a guarded write needs a transactor")

	// ErrNilReader reports a Writer built without a reader. A guarded write
	// compares against the record it read; without a reader it cannot guard.
	ErrNilReader = errors.New("cas: a guarded write needs a reader")

	// ErrUnreadable reports a record that exists but reads as nothing.
	//
	// The two are contradictory — the create guard refused because the key has
	// been written — and the caller has no basis for writing past a holder it
	// cannot read, so this is always fail-closed.
	ErrUnreadable = errors.New("cas: record exists but could not be read")

	// ErrStalled reports a guarded write refused against a record that had not
	// changed since it was read.
	//
	// Also contradictory: a compare against the exact bytes just read can only
	// fail if something wrote. Reported rather than retried, because retrying
	// would re-read the same bytes forever.
	ErrStalled = errors.New("cas: guarded write refused against an unchanged record")

	// ErrContended reports a takeover interrupted more times than MaxAttempts
	// allows. Fail-closed: the caller does not hold what it was trying to take.
	ErrContended = errors.New("cas: record too contended to take")
)

// MaxAttempts bounds a takeover that keeps being interrupted.
//
// Every attempt observes a record that changed since the last one, so this is
// not a timeout in disguise: reaching it means either pathological contention
// on one subject or a store whose record churns without advancing, and both
// want an operator rather than a silent grant.
const MaxAttempts = 16

// Decision is what a Decide function returns: whether this writer may take the
// record from the holder it just read.
type Decision bool

const (
	// Decline means this writer does not supersede the holder. The Handover
	// ends, reporting a loss rather than an error — losing is the ordinary
	// outcome of contention.
	Decline Decision = false

	// Take means this writer supersedes the holder and the swap should be
	// attempted.
	Take Decision = true
)

// Decide reports whether this writer supersedes the record currently held.
//
// It is the ONLY per-record judgment in the sequence, and the whole difference
// between the two call sites lives here. A high-water mark declines whenever
// the stored mark has reached the value being claimed, which makes a lost swap
// terminal without any special-casing in the loop. A lease compares
// generations, so a lost swap against a still-older holder decides Take again
// and the takeover completes.
//
// Returning an error fails the write closed; it must not be used to signal a
// loss, which is what Decline is for.
type Decide func(held []byte) (Decision, error)

// Wrap builds the caller's own error for a failed step, so each call site keeps
// its error vocabulary. op names the step ("create", "read", "swap").
type Wrap func(op string, cause error) error

// Writer performs guarded writes against one go-qrdb cluster.
type Writer struct {
	tx   Transactor
	kv   Reader
	wrap Wrap
}

// NewWriter builds a Writer, failing closed on a missing handle.
//
// wrap may be nil, in which case causes are returned unwrapped.
func NewWriter(tx Transactor, kv Reader, wrap Wrap) (*Writer, error) {
	if tx == nil {
		return nil, ErrNilTransactor
	}
	if kv == nil {
		return nil, ErrNilReader
	}
	if wrap == nil {
		wrap = func(_ string, cause error) error { return cause }
	}
	return &Writer{tx: tx, kv: kv, wrap: wrap}, nil
}

// Create writes value at key only if the key has never been written, and
// reports whether it did.
//
// The guard is CompareCreateRevision == 0 rather than an existence check,
// because "does this exist" answered by a separate read is the two-proposal
// shape this package exists to avoid.
func (w *Writer) Create(ctx context.Context, key string, value []byte) (bool, error) {
	k := []byte(key)
	result, err := w.tx.ExecuteTransaction(ctx, &transport.TransactionRequest{
		Compares: []transport.Compare{{
			Key:            k,
			Target:         transport.CompareCreateRevision,
			Result:         transport.CompareEqual,
			CreateRevision: 0,
		}},
		SuccessOps: []transport.Op{{Type: transport.OpPut, Key: k, Value: value}},
	})
	if err != nil {
		return false, w.wrap("create", err)
	}
	return result.Succeeded, nil
}

// Swap replaces held with next at key, only if the record still reads exactly
// as held, and reports whether it did.
//
// Value comparison rather than a revision compare because the read path returns
// the value only. That is as strong as a revision compare ONLY where the record
// is strictly monotonic: a value once written can never recur, so there is no
// sequence of writes returning the record to bytes a stale reader still holds —
// the ABA scenario value-compare is otherwise unsafe for. A caller whose record
// can repeat a value must not use this.
func (w *Writer) Swap(ctx context.Context, key string, held, next []byte) (bool, error) {
	k := []byte(key)
	result, err := w.tx.ExecuteTransaction(ctx, &transport.TransactionRequest{
		Compares: []transport.Compare{{
			Key:    k,
			Target: transport.CompareValue,
			Result: transport.CompareEqual,
			Value:  held,
		}},
		SuccessOps: []transport.Op{{Type: transport.OpPut, Key: k, Value: next}},
	})
	if err != nil {
		return false, w.wrap("swap", err)
	}
	return result.Succeeded, nil
}

// Handover creates the record if nobody holds it, and otherwise takes it from
// the holder when decide says this writer supersedes them.
//
// MONOTONIC HANDOVER, NEVER DELETE-AND-RECREATE. The record is never removed
// first and there is no path here that removes it. Deleting a coordination key
// un-decides whatever it decided: a contender that read the old record and then
// stalled — a GC pause, a slow proposal, a healing partition — finds the key
// absent on its next attempt, wins it as a fresh create, and proceeds as though
// it were current.
//
// A LOST GUARD IS RE-DECIDED, NOT RE-ATTEMPTED. The loop re-reads and asks
// decide the SAME question again; it never retries a decision decide already
// declined. That is what lets one loop serve both kinds of record. For a
// high-water mark the re-read shows the mark at or past the value being
// claimed, decide declines, and the call ends in a loss — the no-retry
// behaviour, emerging from the predicate rather than from a special case. For a
// lease the re-read may show a holder still older than this writer, because
// whoever won the swap may itself be superseded; decide says Take again and the
// takeover completes. Standing down there would leave an obsolete holder
// owning the record while the newer writer never ran.
//
// Termination is guaranteed by making progress a precondition: every attempt
// after the first requires the record to have actually changed, so the record
// either reaches this writer (a real loss) or this writer wins the swap.
func (w *Writer) Handover(ctx context.Context, key string, next []byte, decide Decide) (bool, error) {
	created, err := w.Create(ctx, key, next)
	if err != nil {
		return false, err
	}
	if created {
		return true, nil
	}

	var previous []byte
	for attempt := 0; attempt < MaxAttempts; attempt++ {
		held, err := w.kv.Get(ctx, key)
		if err != nil {
			return false, w.wrap("read", err)
		}
		if len(held) == 0 {
			return false, w.wrap("read", ErrUnreadable)
		}
		if previous != nil && bytes.Equal(previous, held) {
			return false, w.wrap("swap", ErrStalled)
		}

		decision, err := decide(held)
		if err != nil {
			return false, err
		}
		if decision == Decline {
			return false, nil
		}

		won, err := w.Swap(ctx, key, held, next)
		if err != nil {
			return false, err
		}
		if won {
			return true, nil
		}
		previous = held
	}
	return false, w.wrap("swap", ErrContended)
}
