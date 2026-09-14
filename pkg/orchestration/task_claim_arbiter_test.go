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

// =============================================================================
// Node-backed fixture
// =============================================================================
//
// These tests boot a real in-memory go-qrdb node and use the real
// ExecuteTransaction, because the property under test is that a transaction
// arbitrates a race. A hand-written double would prove nothing about what
// ships: it would be free to mirror whatever behaviour the test expected, and
// the tests that matter most here are the ones that fail when the arbitration
// is not real. pkg/sdlc and pkg/scheduler's firing-claim tests take the same
// fixture for the same reason.
//
// The node needs no seal and no barrier — the transaction plane is independent
// of both, which is why claim marks live on it.

// claimNode returns the package's shared qrdb node, booted on first use.
func claimNode(t *testing.T) *qrdbnode.Node {
	t.Helper()
	return qrdbnode.Shared(t)
}

// newTransactionArbiter builds an arbiter over the shared node.
func newTransactionArbiter(t *testing.T) TaskClaimArbiter {
	t.Helper()
	n := claimNode(t)
	arbiter, err := NewTransactionClaimArbiter(n.Transaction, n.KV)
	if err != nil {
		t.Fatalf("NewTransactionClaimArbiter: %v", err)
	}
	return arbiter
}

// claimSubjects hands out a distinct task per test so tests sharing the node
// cannot collide on one claim mark.
var claimSubjects atomic.Uint64

// uniqueTaskID returns a task identifier no other test in this package uses.
func uniqueTaskID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, claimSubjects.Add(1))
}

// TestMain tears the shared qrdb node down once the package is finished with
// it. Booting is lazy, so a run that touches none of these tests starts
// nothing.
func TestMain(m *testing.M) {
	code := m.Run()
	qrdbnode.CloseShared()
	os.Exit(code)
}

// =============================================================================
// The race
// =============================================================================

// TestTransactionClaimArbiter_NineWorkersAdmitExactlyOne is the arbitration
// half of the go-frost#6 regression: nine agents, one task, one version, all
// asking at once against a REAL transaction plane.
//
// It asserts the reason, not only the count. Every loser must be told who
// holds the task, because that is what lets a dispatcher stand down instead of
// retrying — and because a verdict of "no" with an empty HeldBy would pass a
// count-only assertion while telling the caller nothing.
func TestTransactionClaimArbiter_NineWorkersAdmitExactlyOne(t *testing.T) {
	arbiter := newTransactionArbiter(t)
	ctx := context.Background()
	taskID := uniqueTaskID("frost-6")

	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		verdicts = make([]TaskClaimVerdict, nineWorkerRace)
		failures = make([]error, nineWorkerRace)
	)
	for i := 0; i < nineWorkerRace; i++ {
		wg.Add(1)
		go func(node int) {
			defer wg.Done()
			<-start
			verdicts[node], failures[node] = arbiter.Admit(ctx, &TaskClaim{
				TenantID: "acme",
				TaskID:   taskID,
				AgentID:  claimRaceAgent(node),
				Version:  1,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	winner := -1
	for node, verdict := range verdicts {
		if failures[node] != nil {
			t.Fatalf("node %d could not decide its claim: %v", node, failures[node])
		}
		if !verdict.Admitted {
			continue
		}
		if winner >= 0 {
			t.Fatalf("two nodes were admitted for one task: %s and %s",
				claimRaceAgent(winner), claimRaceAgent(node))
		}
		winner = node
	}
	if winner < 0 {
		t.Fatalf("no node was admitted; verdicts: %v", verdicts)
	}

	for node, verdict := range verdicts {
		if node == winner {
			continue
		}
		if verdict.HeldBy != claimRaceAgent(winner) {
			t.Errorf("node %d was told the task is held by %q, but %q won it",
				node, verdict.HeldBy, claimRaceAgent(winner))
		}
	}
}

// =============================================================================
// The mark's ordering
// =============================================================================

// TestTransactionClaimArbiter_AdmitsALaterVersion proves the mark is a
// high-water mark rather than a one-shot lock: a task legitimately returned to
// a claimable state is claimable again at its next version, by a different
// agent.
func TestTransactionClaimArbiter_AdmitsALaterVersion(t *testing.T) {
	arbiter := newTransactionArbiter(t)
	ctx := context.Background()
	taskID := uniqueTaskID("reclaim")

	first, err := arbiter.Admit(ctx, &TaskClaim{TenantID: "acme", TaskID: taskID, AgentID: "agent-first", Version: 1})
	if err != nil {
		t.Fatalf("first Admit: %v", err)
	}
	if !first.Admitted {
		t.Fatalf("the first claim on an unmarked task was refused, held by %q", first.HeldBy)
	}

	second, err := arbiter.Admit(ctx, &TaskClaim{TenantID: "acme", TaskID: taskID, AgentID: "agent-second", Version: 3})
	if err != nil {
		t.Fatalf("second Admit: %v", err)
	}
	if !second.Admitted {
		t.Fatalf("a claim at a later version was refused, held by %q", second.HeldBy)
	}
}

// TestTransactionClaimArbiter_RefusesAStalledContender proves the branch that
// makes a stalled contender stand down: an agent still holding an old version
// loses to the mark, and is told who holds the task.
//
// This is the case a lock with a timeout gets wrong and a high-water mark gets
// right. The stalled agent is not merely late — it is claiming a version the
// task has already moved past.
func TestTransactionClaimArbiter_RefusesAStalledContender(t *testing.T) {
	arbiter := newTransactionArbiter(t)
	ctx := context.Background()
	taskID := uniqueTaskID("stalled")

	if _, err := arbiter.Admit(ctx, &TaskClaim{TenantID: "acme", TaskID: taskID, AgentID: "agent-live", Version: 4}); err != nil {
		t.Fatalf("seeding the mark: %v", err)
	}

	for _, version := range []uint64{1, 4} {
		verdict, err := arbiter.Admit(ctx, &TaskClaim{
			TenantID: "acme", TaskID: taskID, AgentID: "agent-stalled", Version: version,
		})
		if err != nil {
			t.Fatalf("Admit at version %d: %v", version, err)
		}
		if verdict.Admitted {
			t.Errorf("a claim at version %d was admitted against a mark at 4", version)
		}
		if verdict.HeldBy != "agent-live" {
			t.Errorf("a refused claim at version %d names %q as the holder, want agent-live",
				version, verdict.HeldBy)
		}
	}
}

// =============================================================================
// Fail-closed paths
// =============================================================================

// seedRawMark writes bytes directly to a task's claim key, bypassing the
// arbiter, so the fail-closed branches can be reached with a mark this store
// would never write itself.
func seedRawMark(t *testing.T, tenantID, taskID string, value []byte) {
	t.Helper()
	key, err := taskClaimKey(tenantID, taskID)
	if err != nil {
		t.Fatalf("taskClaimKey: %v", err)
	}
	if err := claimNode(t).KV.Put(context.Background(), key, value); err != nil {
		t.Fatalf("seeding raw mark: %v", err)
	}
}

// TestTransactionClaimArbiter_RefusesAMeaninglessMark proves a mark that
// decodes but does not say where the task stands is refused rather than
// treated as "behind every claim".
//
// "{}" unmarshals happily into a zero mark. Admitting past it would hand the
// task to whoever asked next, which is the exact admission the mark exists to
// refuse — so the test asserts the typed cause, not merely that something
// failed.
func TestTransactionClaimArbiter_RefusesAMeaninglessMark(t *testing.T) {
	arbiter := newTransactionArbiter(t)
	ctx := context.Background()
	taskID := uniqueTaskID("meaningless")
	seedRawMark(t, "acme", taskID, []byte(`{}`))

	verdict, err := arbiter.Admit(ctx, &TaskClaim{
		TenantID: "acme", TaskID: taskID, AgentID: "agent-x", Version: 9,
	})
	if err == nil {
		t.Fatalf("a meaningless mark was decided rather than refused: %+v", verdict)
	}
	if !errors.Is(err, ErrTaskClaimUnreadable) {
		t.Fatalf("expected ErrTaskClaimUnreadable, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrTaskStoreBackend) {
		t.Errorf("the refusal should carry the store's backend vocabulary, got %v", err)
	}
	if verdict.Admitted {
		t.Error("a refused claim reported Admitted")
	}
}

// TestTransactionClaimArbiter_RefusesAnotherTasksMark proves a mark whose body
// names a different task is refused. The key encodes the subject, so this
// cannot arise from the arbiter's own writes; the failure it guards against is
// two tasks sharing one mark, which arbitrates the wrong thing silently.
func TestTransactionClaimArbiter_RefusesAnotherTasksMark(t *testing.T) {
	arbiter := newTransactionArbiter(t)
	ctx := context.Background()
	taskID := uniqueTaskID("mismatch")

	foreign, err := json.Marshal(&taskClaimMark{
		TenantID: "acme", TaskID: "some-other-task", Version: 1, AgentID: "agent-elsewhere",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	seedRawMark(t, "acme", taskID, foreign)

	_, err = arbiter.Admit(ctx, &TaskClaim{
		TenantID: "acme", TaskID: taskID, AgentID: "agent-x", Version: 9,
	})
	if !errors.Is(err, ErrTaskClaimSubjectMismatch) {
		t.Fatalf("expected ErrTaskClaimSubjectMismatch, got %T: %v", err, err)
	}
}

// TestTransactionClaimArbiter_RefusesAnUndecodableMark proves a mark that does
// not decode at all fails closed, exactly like one that decodes to nothing
// meaningful.
func TestTransactionClaimArbiter_RefusesAnUndecodableMark(t *testing.T) {
	arbiter := newTransactionArbiter(t)
	ctx := context.Background()
	taskID := uniqueTaskID("undecodable")
	seedRawMark(t, "acme", taskID, []byte("not json"))

	verdict, err := arbiter.Admit(ctx, &TaskClaim{
		TenantID: "acme", TaskID: taskID, AgentID: "agent-x", Version: 9,
	})
	if err == nil {
		t.Fatalf("an undecodable mark was decided rather than refused: %+v", verdict)
	}
	if !errors.Is(err, ErrTaskStoreBackend) {
		t.Fatalf("expected the store's backend vocabulary, got %T: %v", err, err)
	}
}

// =============================================================================
// Construction and validation
// =============================================================================

// TestNewTransactionClaimArbiter_RefusesAMissingHandle proves the constructor
// fails closed. An arbiter with no transactor could still read marks, so
// accepting one would let an incompletely wired store run and silently
// arbitrate nothing — the defect this type exists to fix, one layer up.
func TestNewTransactionClaimArbiter_RefusesAMissingHandle(t *testing.T) {
	n := claimNode(t)

	for _, tc := range []struct {
		name string
		tx   cas.Transactor
		kv   cas.Reader
	}{
		{name: "no transactor", tx: nil, kv: n.KV},
		{name: "no reader", tx: n.Transaction, kv: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			arbiter, err := NewTransactionClaimArbiter(tc.tx, tc.kv)
			if err == nil {
				t.Fatalf("a store with %s was accepted: %v", tc.name, arbiter)
			}
			if !errors.Is(err, ErrTaskStoreInvalidInput) {
				t.Fatalf("expected ErrTaskStoreInvalidInput, got %T: %v", err, err)
			}
			if arbiter != nil {
				t.Error("a refused constructor returned an arbiter")
			}
		})
	}
}

// TestTransactionClaimArbiter_RefusesAnUnidentifiableClaim proves the fields a
// verdict depends on are required rather than defaulted. An unnamed agent
// leaves a loser unable to tell who won; a zero version claims a record
// position that does not exist.
func TestTransactionClaimArbiter_RefusesAnUnidentifiableClaim(t *testing.T) {
	arbiter := newTransactionArbiter(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		claim *TaskClaim
	}{
		{name: "nil claim", claim: nil},
		{name: "no agent", claim: &TaskClaim{TenantID: "acme", TaskID: "t", Version: 1}},
		{name: "zero version", claim: &TaskClaim{TenantID: "acme", TaskID: "t", AgentID: "a"}},
		{name: "no tenant", claim: &TaskClaim{TaskID: "t", AgentID: "a", Version: 1}},
		{name: "no task", claim: &TaskClaim{TenantID: "acme", AgentID: "a", Version: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, err := arbiter.Admit(ctx, tc.claim)
			if err == nil {
				t.Fatalf("%s was accepted: %+v", tc.name, verdict)
			}
			if !errors.Is(err, ErrTaskStoreInvalidInput) {
				t.Fatalf("expected ErrTaskStoreInvalidInput, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskClaimKey_RefusesASegmentThatEscapesItsSubtree proves the claim key
// validates its segments exactly as the tenant-plane keys do. Without it a
// crafted task ID could address another task's mark and arbitrate the wrong
// subject.
func TestTaskClaimKey_RefusesASegmentThatEscapesItsSubtree(t *testing.T) {
	t.Parallel()

	if _, err := taskClaimKey("acme", "a/b"); !errors.Is(err, ErrInvalidKeySegment) {
		t.Fatalf("a task ID containing the separator was accepted: %v", err)
	}
	if _, err := taskClaimKey("", "t"); !errors.Is(err, ErrInvalidKeySegment) {
		t.Fatalf("an empty tenant was accepted: %v", err)
	}

	key, err := taskClaimKey("acme", "t-1")
	if err != nil {
		t.Fatalf("taskClaimKey: %v", err)
	}
	if want := TaskClaimPrefix + "acme/t-1"; key != want {
		t.Fatalf("claim key is %q, want %q", key, want)
	}
}

// =============================================================================
// Mark lifecycle through the store
// =============================================================================

// markExists reports whether a task's claim mark is present on the transaction
// plane.
func markExists(t *testing.T, tenantID, taskID string) bool {
	t.Helper()
	key, err := taskClaimKey(tenantID, taskID)
	if err != nil {
		t.Fatalf("taskClaimKey: %v", err)
	}
	data, err := claimNode(t).KV.Get(context.Background(), key)
	if err != nil {
		var missing *transport.KeyNotFoundError
		if errors.As(err, &missing) {
			return false
		}
		t.Fatalf("reading mark: %v", err)
	}
	return len(data) > 0
}

// newArbitratedStore builds a task store whose claims are decided on the real
// transaction plane, over its own in-process record KV.
func newArbitratedStore(t *testing.T) TaskStore {
	t.Helper()
	return NewTaskStore(NewMemoryTaskKV(), newTransactionArbiter(t))
}

// TestDeleteTask_ForgetsTheClaimMark proves a deleted task takes its mark with
// it.
//
// A mark outliving its record is debt nobody can ever collect: the task it
// names is gone, so nothing will look at it again, and it accumulates one key
// per task the SDLC module has ever run. Deleting it is safe for the reason
// DeleteJob gives for the firing mark — the RECORD authorises work, so a
// contender holding a claim for a deleted task fails its record load long
// before it reaches an arbiter.
func TestDeleteTask_ForgetsTheClaimMark(t *testing.T) {
	ctx := context.Background()
	store := newArbitratedStore(t)
	taskID := uniqueTaskID("swept")

	seedTask(t, store, "acme", taskID)
	if err := store.ClaimTask(ctx, "acme", taskID, "agent-a"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if !markExists(t, "acme", taskID) {
		t.Fatal("a won claim wrote no mark, so nothing arbitrated it")
	}

	if err := store.DeleteTask(ctx, "acme", taskID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if markExists(t, "acme", taskID) {
		t.Error("the deleted task's mark outlived its record")
	}
}

// TestDeleteTask_ForgetsAnUnclaimedTaskWithoutFailing proves the sweep tolerates
// a task that was never claimed.
//
// A task deleted before anyone claimed it has no mark. Treating that absence as
// a failure would abort the delete — and inside DeleteTenantTasks it would
// abandon the rest of the cascade at the first never-claimed task, which is the
// common case.
func TestDeleteTask_ForgetsAnUnclaimedTaskWithoutFailing(t *testing.T) {
	ctx := context.Background()
	store := newArbitratedStore(t)
	taskID := uniqueTaskID("unclaimed")

	seedTask(t, store, "acme", taskID)
	if err := store.DeleteTask(ctx, "acme", taskID); err != nil {
		t.Fatalf("deleting a never-claimed task failed on its absent mark: %v", err)
	}
}

// TestDeleteTenantTasks_ForgetsEveryMark proves the tenant cascade sweeps marks
// too, and that one never-claimed task in the middle does not stop it.
func TestDeleteTenantTasks_ForgetsEveryMark(t *testing.T) {
	ctx := context.Background()
	store := newArbitratedStore(t)
	claimed := uniqueTaskID("cascade-claimed")
	bare := uniqueTaskID("cascade-bare")
	alsoClaimed := uniqueTaskID("cascade-claimed-2")

	for _, id := range []string{claimed, bare, alsoClaimed} {
		seedTask(t, store, "cascade", id)
	}
	for _, id := range []string{claimed, alsoClaimed} {
		if err := store.ClaimTask(ctx, "cascade", id, "agent-a"); err != nil {
			t.Fatalf("ClaimTask %s: %v", id, err)
		}
	}

	if err := store.DeleteTenantTasks(ctx, "cascade"); err != nil {
		t.Fatalf("DeleteTenantTasks: %v", err)
	}
	for _, id := range []string{claimed, bare, alsoClaimed} {
		if markExists(t, "cascade", id) {
			t.Errorf("task %s kept its mark through the tenant cascade", id)
		}
	}
}

// TestClaimTask_SpendsTheVersionItWasAdmittedFor proves the guarantee the doc
// comment states: a given record version is admitted at most once cluster-wide.
//
// A second store over the SAME record KV and the SAME transaction plane — a
// second node — cannot re-claim a task whose record was rewound to pending at
// the version already spent. This is the stalled-contender branch reached
// through the store rather than the arbiter, and it is the case a lock with a
// timeout gets wrong.
func TestClaimTask_SpendsTheVersionItWasAdmittedFor(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	first := NewTaskStore(shared, newTransactionArbiter(t))
	second := NewTaskStore(shared, newTransactionArbiter(t))
	taskID := uniqueTaskID("spent")

	seedTask(t, first, "acme", taskID)
	if err := first.ClaimTask(ctx, "acme", taskID, "agent-first"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	// Rewind the record to pending at the version that was already claimed,
	// which is what a stalled contender's snapshot looks like.
	task, err := first.GetTask(ctx, "acme", taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	task.Status = TaskStatusPending
	task.AssignedTo = ""
	task.Version = 1
	rewound, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	key, err := taskRecordKey("acme", taskID)
	if err != nil {
		t.Fatalf("taskRecordKey: %v", err)
	}
	if err := shared.Put(ctx, key, rewound); err != nil {
		t.Fatalf("rewinding the record: %v", err)
	}

	err = second.ClaimTask(ctx, "acme", taskID, "agent-second")
	var alreadyClaimed *TaskAlreadyClaimedError
	if !errors.As(err, &alreadyClaimed) {
		t.Fatalf("a spent version was re-claimable: got %T: %v", err, err)
	}
	if alreadyClaimed.ClaimedBy != "agent-first" {
		t.Errorf("the refusal names %q as holder, want agent-first", alreadyClaimed.ClaimedBy)
	}
}

// TestForget_RefusesAnUnusableKey proves Forget validates its segments rather
// than deleting whatever key they happen to spell. A task ID carrying the
// separator would otherwise address another task's mark and forget a live
// claim.
func TestForget_RefusesAnUnusableKey(t *testing.T) {
	arbiter := newTransactionArbiter(t)

	err := arbiter.Forget(context.Background(), "acme", "a/b")
	if !errors.Is(err, ErrInvalidKeySegment) {
		t.Fatalf("expected ErrInvalidKeySegment, got %T: %v", err, err)
	}
}

// TestTaskClaimError_ReportsBothContracts proves the dual-contract error reads
// as a store failure while still matching its specific cause.
//
// Both halves are load-bearing: the REST and gRPC layers map the store
// vocabulary onto a response, and an operator diagnosing a stuck claim needs
// the cause. A single-contract error would force one of them to match on text.
func TestTaskClaimError_ReportsBothContracts(t *testing.T) {
	t.Parallel()

	err := claimError("claim.read", ErrTaskClaimUnreadable)
	if !errors.Is(err, ErrTaskStoreBackend) {
		t.Error("the store-vocabulary half is unreachable")
	}
	if !errors.Is(err, ErrTaskClaimUnreadable) {
		t.Error("the cause half is unreachable")
	}
	var claim *TaskClaimError
	if !errors.As(err, &claim) {
		t.Fatalf("expected *TaskClaimError, got %T", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "claim.read") {
		t.Errorf("the message does not name the step that failed: %q", msg)
	}
}

// =============================================================================
// The single-process arbiter
// =============================================================================

// TestProcessClaimArbiter_AdmitsWhatTheRecordAccepted proves the
// single-process arbiter adds no second decision. It is consulted only after
// the record's own preconditions passed, and in a one-process domain the
// store's per-key mutex has already serialized the contenders.
func TestProcessClaimArbiter_AdmitsWhatTheRecordAccepted(t *testing.T) {
	t.Parallel()

	arbiter := NewProcessClaimArbiter()
	verdict, err := arbiter.Admit(context.Background(), &TaskClaim{
		TenantID: "acme", TaskID: "t", AgentID: "a", Version: 1,
	})
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !verdict.Admitted {
		t.Fatal("the single-process arbiter refused a claim the record accepted")
	}
}

// TestProcessClaimArbiter_RefusesANilClaim proves even the permissive arbiter
// refuses an attempt it cannot describe, so a caller bug surfaces as an error
// rather than as an admission.
func TestProcessClaimArbiter_RefusesANilClaim(t *testing.T) {
	t.Parallel()

	if _, err := NewProcessClaimArbiter().Admit(context.Background(), nil); !errors.Is(err, ErrTaskStoreInvalidInput) {
		t.Fatalf("expected ErrTaskStoreInvalidInput, got %T: %v", err, err)
	}
}
