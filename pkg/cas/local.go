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

package cas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// Process-local arbitration
// =============================================================================
//
// ONE IMPLEMENTATION OF AN EXISTING SEAM, NOT A SECOND SPELLING OF THE LOGIC.
// Writer builds every guarded write in this package — the create predicate, the
// monotonic handover, the swap — and emits them as transactions. This type only
// DECIDES those transactions, by evaluating the compares under a mutex instead
// of through Raft. The predicates stay in exactly one place; only the
// arbitration substrate differs.
//
// WHY IT EXISTS. go-qrdb gates the transaction plane behind clustered mode
// under its run-modes split, so a standalone or embedded boot has no
// transaction to arbitrate with. That is not a degraded deployment: a
// single-node mode has exactly one process, so in-process exclusivity is the
// COMPLETE answer there rather than an approximation of a better one. It is the
// same reasoning orchestration's process claim arbiter is built on, arriving at
// its second consumer.
//
// WHAT IT DOES NOT GUARANTEE, stated plainly because the whole family of
// defects this package exists to remove came from comments that overclaimed:
// NOTHING ACROSS PROCESSES. Two processes holding their own LocalTransactor
// over the same store will both decide the same contested write in their own
// favour, exactly as two process-local mutexes always have. It is sound when
// there is one process and unsound the moment there are two, and nothing in the
// type can detect the difference — which is why the choice belongs to the
// caller's DEPLOYMENT MODE and not to whichever handles happen to be present.
// A cluster-like path constructing this is a blocking review finding.

// ErrLocalTransactorUnsupported reports a transaction this arbiter will not
// decide.
//
// It refuses rather than guessing. The compares Writer emits are a closed set,
// and a transaction outside it means either a new guarded primitive that has
// not been taught here or a caller reaching past Writer — and evaluating an
// unfamiliar predicate as "true" would silently admit a write nobody guarded.
var ErrLocalTransactorUnsupported = errors.New("cas: local transactor cannot decide this transaction")

// LocalWritableKV is the store a LocalTransactor reads and writes.
//
// go-qrdb's transport.KVClient satisfies it. Declared narrowly so the arbiter
// is testable without a node and cannot reach operations it has no business
// performing.
type LocalWritableKV interface {
	Get(ctx context.Context, key string, opts ...transport.ReadOption) ([]byte, error)
	Put(ctx context.Context, key string, value []byte, opts ...transport.WriteOption) error
	Delete(ctx context.Context, key string, opts ...transport.WriteOption) error
}

// LocalTransactor decides guarded writes in one process.
//
// It satisfies Transactor (go-qrdb's transport.TransactionClient), so Writer
// and every store above it are unchanged by the substitution.
type LocalTransactor struct {
	// mu serializes the compare-and-apply sequence.
	//
	// ONE MUTEX FOR THE WHOLE ARBITER rather than one per key. Coordination
	// records are low-volume by construction — leases, claims, route entries —
	// so the contention this gives up is not worth the second mechanism a
	// keyed lock would introduce, and a single lock is obviously correct where
	// a keyed one has to be argued.
	mu sync.Mutex
	kv LocalWritableKV
}

// Compile-time proof that the substitution is exact.
var _ Transactor = (*LocalTransactor)(nil)

// NewLocalTransactor returns an arbiter over kv.
func NewLocalTransactor(kv LocalWritableKV) (*LocalTransactor, error) {
	if kv == nil {
		return nil, errors.New("cas: local transactor requires a store")
	}
	return &LocalTransactor{kv: kv}, nil
}

// ExecuteTransaction evaluates the compares and applies the matching branch.
//
// The mutex is held across BOTH the evaluation and the write, which is the
// entire point: a read followed by an unguarded write under no lock is the
// defect this package removes, and doing it here would reintroduce it beneath
// every guarded primitive at once.
func (t *LocalTransactor) ExecuteTransaction(
	ctx context.Context,
	req *transport.TransactionRequest,
	_ ...transport.WriteOption,
) (*transport.TransactionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrLocalTransactorUnsupported)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	satisfied, err := t.evaluate(ctx, req.Compares)
	if err != nil {
		return nil, err
	}

	ops := req.SuccessOps
	if !satisfied {
		ops = req.FailureOps
	}
	if err := t.apply(ctx, ops); err != nil {
		return nil, err
	}
	return &transport.TransactionResponse{Succeeded: satisfied}, nil
}

// evaluate reports whether every compare holds.
//
// Only the predicates Writer emits are understood — key-absent and
// value-equals. Anything else is refused by name rather than approximated.
func (t *LocalTransactor) evaluate(ctx context.Context, compares []transport.Compare) (bool, error) {
	for _, cmp := range compares {
		if cmp.Result != transport.CompareEqual {
			return false, fmt.Errorf("%w: compare result %v", ErrLocalTransactorUnsupported, cmp.Result)
		}
		current, err := t.kv.Get(ctx, string(cmp.Key))
		if err != nil {
			var missing *transport.KeyNotFoundError
			if !errors.As(err, &missing) {
				// A store that could not answer is a FAULT, never "the
				// predicate is false". Reading it as false would let a backend
				// outage decide a contested write.
				return false, err
			}
			current = nil
		}

		switch cmp.Target {
		case transport.CompareCreateRevision:
			// Writer uses CreateRevision == 0 to mean "the key does not
			// exist". Any other revision is a comparison this substrate has no
			// revisions to make.
			if cmp.CreateRevision != 0 {
				return false, fmt.Errorf("%w: create revision %d", ErrLocalTransactorUnsupported, cmp.CreateRevision)
			}
			if len(current) != 0 {
				return false, nil
			}
		case transport.CompareValue:
			if !bytes.Equal(current, cmp.Value) {
				return false, nil
			}
		default:
			return false, fmt.Errorf("%w: compare target %v", ErrLocalTransactorUnsupported, cmp.Target)
		}
	}
	return true, nil
}

// apply performs the branch's operations.
func (t *LocalTransactor) apply(ctx context.Context, ops []transport.Op) error {
	for _, op := range ops {
		switch op.Type {
		case transport.OpPut:
			if err := t.kv.Put(ctx, string(op.Key), op.Value); err != nil {
				return err
			}
		case transport.OpDelete:
			if err := t.kv.Delete(ctx, string(op.Key)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: op type %v", ErrLocalTransactorUnsupported, op.Type)
		}
	}
	return nil
}
