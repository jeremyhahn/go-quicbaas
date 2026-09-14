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

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
)

// =============================================================================
// Claim arbitration
// =============================================================================
//
// A task transition is an ordinary record write; a task CLAIM is a decision
// about which of several contenders may make it. The two are separated here
// because they belong to different planes and have different failure modes.
//
// The task record is tenant data: it lives at orch/task/{tenant}/{taskID} in
// the per-tenant store the routing KV resolves, which for a barrier-isolated
// tenant is that tenant's own encrypted store. The claim mark is coordination
// metadata: it names which agent holds a task and nothing else, so it lives in
// go-qrdb's reserved "__sys:" namespace on the transaction plane, next to the
// scheduler's firing claims and the sdlc leases. Putting the mark on the
// tenant plane would have meant arbitrating in a keyspace whose location
// depends on the tenant's isolation mode; putting the RECORD on the system
// plane would have meant moving tenant work items out of the tenant barrier.
// Neither is acceptable, and separating them costs nothing.

// TaskClaim identifies one attempt by one agent to take one task at one
// version of its record.
//
// Version is what makes the attempt identifiable rather than merely repeated.
// Every contender reads the same record before claiming, so every contender
// derives the same version without consulting a local clock — the property the
// whole mechanism rests on, and the same one firingKey gets from a job's
// replicated NextRun.
type TaskClaim struct {
	TenantID string
	TaskID   string
	AgentID  string
	Version  uint64
}

// TaskClaimVerdict is an arbiter's answer.
//
// HeldBy is populated only when Admitted is false, and it is the reason a
// loser can be told WHO holds the task rather than merely that it lost. A
// dispatcher that knows the holder stands down; one that knows only "no"
// cannot tell contention from a broken store.
type TaskClaimVerdict struct {
	Admitted bool
	HeldBy   string
}

// TaskClaimArbiter decides which contender takes a task.
//
// It is consulted only after the task record's own preconditions have passed
// — pending, unclaimed, dependencies resolved — so an arbiter never sees an
// attempt the record itself would have refused. That ordering is what keeps
// the mark meaningful: it advances on genuine admissions and on nothing else.
//
// An error means the arbiter could not decide, and callers must treat that as
// a refusal. Losing is reported as a verdict, never as an error.
type TaskClaimArbiter interface {
	Admit(ctx context.Context, claim *TaskClaim) (TaskClaimVerdict, error)

	// Forget discards the mark for a task that no longer exists.
	//
	// This is NOT a release, and there is deliberately no release. A release
	// would return a live task to an unclaimed state, and the mark is what a
	// stalled contender compares against: resetting it hands that contender
	// the task a second time. Forget is called only when the task RECORD is
	// gone, which is a different thing entirely — the record, not the mark, is
	// what authorises work, so a contender still holding a claim for a deleted
	// task fails its record load before it ever reaches an arbiter.
	//
	// Forgetting an unmarked task is not an error. A task deleted before it
	// was ever claimed has no mark, and a cascade that treated that as a
	// failure would abandon the rest of the sweep.
	Forget(ctx context.Context, tenantID, taskID string) error
}

// =============================================================================
// The claim mark
// =============================================================================

// TaskClaimPrefix holds one claim mark per task.
//
// The "__sys:" prefix is go-qrdb's reserved internal namespace. It is
// load-bearing rather than decorative: keys under it are excluded from
// user-facing List and Scan and refused by MVCC restore, so claim state cannot
// be enumerated by an ordinary tenant sweep or rolled back through a restore.
// It is a DISCOVERY guard, not a confidentiality boundary and not a write ACL
// — go-qrdb documents the filter as read-only — and it is claimed here for the
// enumeration protection alone.
const TaskClaimPrefix = "__sys:orch/taskclaim/"

// taskClaimKey builds the key holding one task's claim mark.
//
// One key per task, not per claim. A key per claim would grow without bound
// and would have to be pruned, and pruning a claim key un-decides a decided
// claim: a contender that read version N and then stalled — a GC pause, a slow
// proposal, a healing partition — finds the key absent on its next attempt and
// wins it as a fresh create. The mark removes that instead of bounding it.
//
// Segments are validated exactly as the tenant-plane keys are, so a crafted
// identifier cannot escape its subtree and collide with another task's mark.
func taskClaimKey(tenantID, taskID string) (string, error) {
	if err := validateKeySegment("tenant_id", tenantID); err != nil {
		return "", taskKeyError(err)
	}
	if err := validateKeySegment("task_id", taskID); err != nil {
		return "", taskKeyError(err)
	}
	return TaskClaimPrefix + tenantID + keySeparator + taskID, nil
}

// taskClaimMark is the high-water mark of claims admitted for one task: the
// latest record version any node has been allowed to claim, and the agent that
// was allowed to.
//
// The mark only moves forward, which is what makes it safe never to delete and
// what makes a stalled contender stand down. The cost, stated because it is
// real: a task whose record version is ever moved BELOW its mark becomes
// unclaimable. That errs toward a miss rather than a duplicate, which is the
// direction this whole mechanism is chosen for, and it cannot arise from this
// store's own writes — Version is monotonic per record.
type taskClaimMark struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`

	// Version is the record version this claim was admitted for, and the only
	// field any decision reads.
	Version uint64 `json:"version"`

	// AgentID names the holder. Diagnostic to the decision, load-bearing to
	// the loser: it is what fills TaskAlreadyClaimedError.ClaimedBy.
	AgentID string `json:"agent_id"`

	ClaimedAt time.Time `json:"claimed_at"`
}

// ErrTaskClaimUnreadable reports a claim mark that exists but does not say
// where the task stands.
//
// Always fail-closed. A mark that decodes to a zero version — "{}" unmarshals
// happily — would otherwise read as "behind every claim" and let any contender
// advance past it, which is precisely the admission the mark exists to refuse.
var ErrTaskClaimUnreadable = errors.New("task store: claim mark exists but could not be read")

// ErrTaskClaimSubjectMismatch reports a claim mark whose body names a
// different task than the key that addressed it.
//
// The key encodes the subject, so this cannot arise from this store's own
// writes. It is checked anyway because the failure it guards against is two
// tasks sharing one mark, which arbitrates the wrong thing silently.
var ErrTaskClaimSubjectMismatch = errors.New("task store: claim mark describes a different task")

// TaskClaimError reports a claim the arbiter could not decide.
//
// It bridges two contracts that must both hold, the way TaskKeySegmentError
// does for the key layout. A caller handling store failures generically
// matches the store's vocabulary (ErrTaskStoreBackend / *TaskStoreError, which
// the REST and gRPC layers already map onto a server error), while a caller
// diagnosing WHY arbitration failed matches the specific cause. Unwrap returns
// both, so errors.Is succeeds against either without this arbiter having to
// choose which caller to serve.
//
// The bridge is needed because TaskStoreError folds its cause into a message
// string rather than wrapping it, so a cause routed through backendError alone
// would be reachable only by matching text — which is what the package's typed
// errors exist to avoid.
type TaskClaimError struct {
	// Failure restates the refusal in the task store's vocabulary.
	Failure *TaskStoreError

	// Cause is the specific reason arbitration failed closed.
	Cause error
}

// Error implements the error interface.
func (e *TaskClaimError) Error() string { return e.Failure.Error() }

// Unwrap exposes both wrapped contracts to errors.Is and errors.As.
func (e *TaskClaimError) Unwrap() []error { return []error{e.Failure, e.Cause} }

// claimError builds the dual-contract refusal for a fail-closed arbitration.
func claimError(op string, cause error) error {
	var failure *TaskStoreError
	if !errors.As(backendError(op, cause), &failure) {
		// backendError constructs a *TaskStoreError unconditionally; this
		// branch exists so a future change there cannot silently drop the
		// store-vocabulary half of the contract. errors.As rather than a
		// bare assertion, so a future backendError that WRAPS the store
		// error keeps the contract instead of silently taking this branch.
		return cause
	}
	return &TaskClaimError{Failure: failure, Cause: cause}
}

// =============================================================================
// The transaction-plane arbiter
// =============================================================================

// transactionClaimArbiter decides claims through go-qrdb's transaction plane.
//
// This is the only construction under which two nodes racing one task produce
// one winner. A compare and a write evaluated together inside the state
// machine is a single Raft proposal; a read followed by a write is two
// proposals with a window between them, and that window is the race that put
// nine workers on go-frost#6.
type transactionClaimArbiter struct {
	writer *cas.Writer

	// kv serves the mark deletion Forget performs. Reads during arbitration
	// belong to the writer, which holds its own handle.
	kv cas.Reader
}

// Compile-time interface satisfaction check.
var _ TaskClaimArbiter = (*transactionClaimArbiter)(nil)

// NewTransactionClaimArbiter builds an arbiter over go-qrdb's transaction and
// KV planes, failing closed on a missing handle.
//
// A nil handle is refused rather than accepted and degraded: an arbiter with no
// transactor could still read marks, so an incompletely wired store would run
// and silently arbitrate nothing — which is the defect this type exists to fix,
// reintroduced one layer up.
func NewTransactionClaimArbiter(tx cas.Transactor, kv cas.Reader) (TaskClaimArbiter, error) {
	writer, err := cas.NewWriter(tx, kv, func(op string, cause error) error {
		return backendError("claim."+op, cause)
	})
	switch {
	case errors.Is(err, cas.ErrNilTransactor):
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "a claim is arbitrated by transaction; an arbiter without a transactor decides nothing",
		}
	case errors.Is(err, cas.ErrNilReader):
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "a guarded write compares against the mark it read; an arbiter without a reader cannot guard",
		}
	case err != nil:
		return nil, backendError("NewTransactionClaimArbiter", err)
	}
	return &transactionClaimArbiter{writer: writer, kv: kv}, nil
}

// Admit takes the task's claim mark for this contender, or reports who holds
// it.
//
// A lost guard is RE-DECIDED, not re-attempted: cas.Handover re-reads and asks
// the same question again. For this record the re-read can only decline —
// whoever won the swap advanced the mark to at or past this version — so the
// call ends in a loss without any special case in the loop. That is the same
// property the scheduler's firing claim relies on, and the reason both can
// share one primitive.
func (a *transactionClaimArbiter) Admit(ctx context.Context, claim *TaskClaim) (TaskClaimVerdict, error) {
	if claim == nil {
		return TaskClaimVerdict{}, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "claim must not be nil",
		}
	}
	if claim.AgentID == "" {
		return TaskClaimVerdict{}, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "a mark must name the agent holding it, or a loser cannot tell who won",
		}
	}
	if claim.Version == 0 {
		return TaskClaimVerdict{}, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "a claim must name the record version it was admitted for",
		}
	}

	key, err := taskClaimKey(claim.TenantID, claim.TaskID)
	if err != nil {
		return TaskClaimVerdict{}, err
	}
	next, err := json.Marshal(&taskClaimMark{
		TenantID:  claim.TenantID,
		TaskID:    claim.TaskID,
		Version:   claim.Version,
		AgentID:   claim.AgentID,
		ClaimedAt: time.Now().UTC(),
	})
	if err != nil {
		return TaskClaimVerdict{}, backendError("claim.encode", err)
	}

	// Captured from the decline branch so the loser's error can name the
	// holder. Handover reports a decline as (false, nil), and the only path
	// that declines is the one that read the holder.
	var heldBy string

	won, err := a.writer.Handover(ctx, key, next, func(held []byte) (cas.Decision, error) {
		var current taskClaimMark
		if err := json.Unmarshal(held, &current); err != nil {
			return cas.Decline, backendError("claim.decode", err)
		}
		if current.Version == 0 || current.AgentID == "" {
			return cas.Decline, claimError("claim.read", ErrTaskClaimUnreadable)
		}
		if current.TenantID != claim.TenantID || current.TaskID != claim.TaskID {
			return cas.Decline, claimError("claim.read", ErrTaskClaimSubjectMismatch)
		}
		if current.Version >= claim.Version {
			// Another node was admitted for this version or a later one. This
			// is the branch a stalled contender lands in, and the branch that
			// makes the shared handover loop behave the way a claim must:
			// declining is terminal, so a lost swap re-reads, declines again,
			// and the call ends in a loss.
			heldBy = current.AgentID
			return cas.Decline, nil
		}
		return cas.Take, nil
	})
	if err != nil {
		return TaskClaimVerdict{}, err
	}
	if !won {
		return TaskClaimVerdict{Admitted: false, HeldBy: heldBy}, nil
	}
	return TaskClaimVerdict{Admitted: true}, nil
}

// Forget removes the deleted task's mark.
//
// Safe here in a way a release never is, and for the reason DeleteJob gives for
// the firing mark: the TASK RECORD, not the mark, is what authorises work. A
// contender still holding a claim for this task fails its record load and
// returns before it reaches an arbiter, so a mark removed here cannot resurrect
// a claim. The caller deletes the record first; if this then fails, the mark is
// orphaned, which is bounded debt, whereas the reverse order could briefly
// leave a live record with no mark — a task claimable twice.
func (a *transactionClaimArbiter) Forget(ctx context.Context, tenantID, taskID string) error {
	key, err := taskClaimKey(tenantID, taskID)
	if err != nil {
		return err
	}
	if err := a.kv.Delete(ctx, key); err != nil {
		var missing *transport.KeyNotFoundError
		if errors.As(err, &missing) {
			// A task deleted before it was ever claimed has no mark. Treating
			// that as a failure would abandon the rest of a tenant cascade.
			return nil
		}
		return claimError("claim.forget", err)
	}
	return nil
}

// =============================================================================
// The single-process arbiter
// =============================================================================

// processClaimArbiter admits every attempt the task record already accepted.
//
// THIS ARBITRATES NOTHING ACROSS NODES, AND IT IS NOT A FALLBACK FOR ONE THAT
// DOES. It is the correct and complete arbiter for a store whose consistency
// domain is one process — a memory-backed store, an embedded single-node
// deployment — because in that domain the store's own per-key mutex has
// already serialized the contenders and the record's status field has already
// refused the losers. There is no second decision left to make.
//
// It exists so that a caller must SAY which domain its store serves, at the
// call site, in a line a reviewer can see. An arbiter chosen by default is the
// silent degradation this whole change removes: a fleet store that quietly got
// process-local arbitration would report success and dispatch nine workers,
// which is exactly what happened to go-frost#6.
type processClaimArbiter struct{}

// Compile-time interface satisfaction check.
var _ TaskClaimArbiter = processClaimArbiter{}

// NewProcessClaimArbiter returns the arbiter for a single-process store.
//
// Use it when the backing KV lives in this process and nowhere else. Passing it
// to a store backed by a replicated KV is a correctness bug that no test in
// this package can catch, because the defect appears only when a second process
// exists.
func NewProcessClaimArbiter() TaskClaimArbiter { return processClaimArbiter{} }

// Admit accepts unconditionally. See the type comment for why that is the
// complete answer in a single-process domain and a bug outside one.
func (processClaimArbiter) Admit(_ context.Context, claim *TaskClaim) (TaskClaimVerdict, error) {
	if claim == nil {
		return TaskClaimVerdict{}, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "claim must not be nil",
		}
	}
	return TaskClaimVerdict{Admitted: true}, nil
}

// Forget has nothing to discard: a single-process arbiter writes no mark.
func (processClaimArbiter) Forget(_ context.Context, _, _ string) error { return nil }
