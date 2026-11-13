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

package quicbaas

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// =============================================================================
// Node-backed fixtures
// =============================================================================
//
// The claim rides go-qrdb's transaction plane, which hangs off the SDK client
// rather than off a tenant store, so these tests boot a real in-memory qrdb node
// and use the real ExecuteTransaction. That is deliberate: the property under
// test is that the transaction arbitrates a race, and only go-qrdb's own
// implementation can demonstrate it. A hand-written double would prove nothing
// about what ships, and would be free to mirror whatever bug the test expected.
//
// The node needs no seal and no barrier — the transaction plane is independent
// of both, which is precisely why the claim was moved onto it.

// node returns the package's shared qrdb node, booted on first use by
// pkg/testutil/qrdbnode. The fixture lives there because pkg/orchestration/sdlc's coordination tests
// need exactly the same thing, and its port is assigned by the kernel rather
// than hand-picked — see qrdbnode.Shared.
func node(t *testing.T) *qrdbnode.Node {
	t.Helper()
	return qrdbnode.Shared(t)
}

// newClusteredFixture builds a clustered store over the shared node.
func newClusteredFixture(t *testing.T) *ClusteredStorage {
	t.Helper()
	n := node(t)
	storage, err := NewClusteredStorage(newMockKVStore(), n.Transaction, n.KV)
	if err != nil {
		t.Fatalf("NewClusteredStorage: %v", err)
	}
	return storage
}

// jobIDs hands out a distinct job ID per test so tests sharing the node cannot
// collide on a claim mark.
var jobIDs atomic64

type atomic64 struct {
	mu sync.Mutex
	n  uint64
}

func (a *atomic64) next() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	return a.n
}

func firingClaimAt(jobID uint64, firing time.Time, owner string) *scheduler.FiringClaim {
	return &scheduler.FiringClaim{
		JobID:      jobID,
		FiringTime: firing,
		Owner:      owner,
		ClaimedAt:  time.Now(),
	}
}

// =============================================================================
// The live CAS
// =============================================================================

// TestClaimFiring_FirstCallerWins covers the create path against the real
// transaction plane.
func TestClaimFiring_FirstCallerWins(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	firing := time.Unix(1_700_000_000, 0).UTC()

	won, err := s.ClaimFiring(context.Background(), firingClaimAt(jobID, firing, "node-A"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !won {
		t.Fatal("the first claimant of an unclaimed firing must win")
	}
}

// TestClaimFiring_SecondCallerLoses proves the refusal is a verdict, not an
// error: a lost claim is the normal outcome of leader churn, and reporting it as
// a failure would make the engine count it as an undecided claim and alert.
func TestClaimFiring_SecondCallerLoses(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	firing := time.Unix(1_700_000_000, 0).UTC()
	ctx := context.Background()

	if _, err := s.ClaimFiring(ctx, firingClaimAt(jobID, firing, "node-A")); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, firing, "node-B"))
	if err != nil {
		t.Fatalf("losing a claim must not be an error, got: %v", err)
	}
	if won {
		t.Fatal("a firing already claimed must not be granted twice")
	}
}

// TestClaimFiring_ConcurrentClaimantsYieldExactlyOneWinner is the property the
// whole mechanism exists for. Counting winners under real concurrency is the
// only assertion that distinguishes a genuine compare-and-swap from a
// check-then-write that happens to pass when callers are serialized.
func TestClaimFiring_ConcurrentClaimantsYieldExactlyOneWinner(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	firing := time.Unix(1_700_000_000, 0).UTC()
	ctx := context.Background()

	const claimants = 16
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(claimants)
	done.Add(claimants)

	results := make([]bool, claimants)
	errs := make([]error, claimants)

	for i := 0; i < claimants; i++ {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start
			results[i], errs[i] = s.ClaimFiring(ctx, firingClaimAt(jobID, firing, "node"))
		}(i)
	}

	ready.Wait()
	close(start)
	done.Wait()

	winners := 0
	for i, won := range results {
		if errs[i] != nil {
			t.Fatalf("claimant %d errored: %v", i, errs[i])
		}
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1 across %d concurrent claimants", winners, claimants)
	}
}

// TestClaimFiring_MarkAdvancesAcrossFirings proves a recurring job keeps firing:
// the mark moves forward, so each new scheduled firing is claimable.
func TestClaimFiring_MarkAdvancesAcrossFirings(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()
	first := time.Unix(1_700_000_000, 0).UTC()

	for i, firing := range []time.Time{first, first.Add(time.Minute), first.Add(2 * time.Minute)} {
		won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, firing, "node-A"))
		if err != nil {
			t.Fatalf("firing %d: %v", i, err)
		}
		if !won {
			t.Fatalf("firing %d must be claimable once the mark has advanced past the previous one", i)
		}
	}
}

// TestClaimFiring_StaleContenderLosesAfterTheMarkMoves is the regression for the
// defect that killed the per-firing design: a contender holding an old firing
// must lose even though nothing is holding that firing's own key.
//
// The scenario is a healing partition. Node B reads job J at firing T and
// stalls. Node A claims T, runs it, the job advances, and A claims T+1. B wakes
// and presents T. Under a per-firing record whose old keys were pruned, B would
// find T absent and re-run it. Under the mark, B loses because the mark is past
// T.
func TestClaimFiring_StaleContenderLosesAfterTheMarkMoves(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()
	stale := time.Unix(1_700_000_000, 0).UTC()

	if won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, stale, "node-A")); err != nil || !won {
		t.Fatalf("node-A claiming T: won=%v err=%v", won, err)
	}
	if won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, stale.Add(time.Minute), "node-A")); err != nil || !won {
		t.Fatalf("node-A claiming T+1: won=%v err=%v", won, err)
	}

	won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, stale, "node-B"))
	if err != nil {
		t.Fatalf("a stale claim must lose, not error: %v", err)
	}
	if won {
		t.Fatal("a contender holding a firing the mark has passed must lose — " +
			"this is the double-fire that eager pruning reopened")
	}
}

// TestClaimFiring_DistinctJobsDoNotCollide proves two jobs due at the same
// instant claim independently.
func TestClaimFiring_DistinctJobsDoNotCollide(t *testing.T) {
	s := newClusteredFixture(t)
	ctx := context.Background()
	firing := time.Unix(1_700_000_000, 0).UTC()
	first, second := jobIDs.next(), jobIDs.next()

	if won, err := s.ClaimFiring(ctx, firingClaimAt(first, firing, "node-A")); err != nil || !won {
		t.Fatalf("job %d: won=%v err=%v", first, won, err)
	}
	if won, err := s.ClaimFiring(ctx, firingClaimAt(second, firing, "node-A")); err != nil || !won {
		t.Fatalf("job %d must claim independently: won=%v err=%v", second, won, err)
	}
}

// =============================================================================
// Construction and validation
// =============================================================================

// TestNewClusteredStorage_RequiresAnArbiter pins the fail-closed construction
// rule. A clustered store with nothing to decide a contended firing must refuse
// to exist, because the alternative is a cluster that looks healthy until the
// first leadership change.
func TestNewClusteredStorage_RequiresAnArbiter(t *testing.T) {
	storage, err := NewClusteredStorage(newMockKVStore(), nil, nil)
	if err == nil {
		t.Fatal("expected construction to fail without a transaction client")
	}
	if storage != nil {
		t.Error("a rejected store must not be returned")
	}
	var unsupported scheduler.ClaimUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected ClaimUnsupportedError, got %T: %v", err, err)
	}
}

// TestStorage_DoesNotSatisfyFiringClaimer is the compile-time half of the
// fail-closed guard, asserted at runtime so the intent is visible.
//
// scheduler.NewWithClustering decides whether a store can arbitrate by type
// assertion. If a single-node Storage satisfied FiringClaimer, that assertion
// would pass for a store with no arbiter and the refusal would move from startup
// to the first contended firing — where it is useless.
func TestStorage_DoesNotSatisfyFiringClaimer(t *testing.T) {
	var store any = NewStorage(newMockKVStore())
	if _, ok := store.(scheduler.FiringClaimer); ok {
		t.Fatal("a single-node Storage must NOT satisfy FiringClaimer, or the " +
			"clustered scheduler's startup guard silently accepts it")
	}

	var clustered any = &ClusteredStorage{}
	if _, ok := clustered.(scheduler.FiringClaimer); !ok {
		t.Fatal("ClusteredStorage must satisfy FiringClaimer")
	}
}

// TestClaimFiring_RejectsClaimsThatCannotIdentifyAFiring covers the degenerate
// identities. Both would collapse distinct firings onto one claim, which is the
// double-fire bug wearing a different hat, so they are refused before the
// transaction plane is consulted.
func TestClaimFiring_RejectsClaimsThatCannotIdentifyAFiring(t *testing.T) {
	s := newClusteredFixture(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		claim *scheduler.FiringClaim
		field string
	}{
		{"nil claim", nil, ""},
		{"zero job id", &scheduler.FiringClaim{FiringTime: time.Now()}, "job_id"},
		{"zero firing time", &scheduler.FiringClaim{JobID: 7}, "firing_time"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			won, err := s.ClaimFiring(ctx, tc.claim)
			if err == nil {
				t.Fatal("expected an error")
			}
			if won {
				t.Fatal("an unidentifiable firing must never be granted")
			}
			if tc.field == "" {
				var storageErr *scheduler.ErrStorageError
				if !errors.As(err, &storageErr) {
					t.Fatalf("expected ErrStorageError, got %T: %v", err, err)
				}
				return
			}
			var invalid scheduler.InvalidFiringClaimError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected InvalidFiringClaimError, got %T: %v", err, err)
			}
			if invalid.Field != tc.field {
				t.Errorf("Field = %q, want %q", invalid.Field, tc.field)
			}
		})
	}
}

// =============================================================================
// Key derivation
// =============================================================================

// TestFiringKey_IsAPureFunctionOfTheFiringIdentity is the property the whole
// claim rests on: two nodes that read the same job record must derive the same
// identity, and any change to job or firing must change it. If this ever stopped
// holding, each node would compute its own identity, the transaction would
// arbitrate between values that never collide, and every node would win.
func TestFiringKey_IsAPureFunctionOfTheFiringIdentity(t *testing.T) {
	firing := time.Unix(1_700_000_000, 123).UTC()

	// Same instant, independently constructed — as two nodes would have after
	// decoding the same replicated record.
	nodeA := firingKey(7, firing)
	nodeB := firingKey(7, time.Unix(1_700_000_000, 123).In(time.FixedZone("elsewhere", 3600)))
	if nodeA != nodeB {
		t.Errorf("two nodes derived different identities for one firing: %q vs %q — a\n"+
			"wall-clock or zone-dependent rendering would let both win", nodeA, nodeB)
	}

	if firingKey(8, firing) == nodeA {
		t.Error("different jobs must not share a firing identity")
	}
	if firingKey(7, firing.Add(time.Nanosecond)) == nodeA {
		t.Error("different firings of one job must not share a firing identity")
	}
}

// TestKeyForClaim_IsPerJob documents that the claim key is per job, not per
// firing — the change that removed pruning.
func TestKeyForClaim_IsPerJobAndPerScope(t *testing.T) {
	if keyForClaim(7, "") == keyForClaim(8, "") {
		t.Error("distinct jobs must have distinct claim marks")
	}

	// The scope half is the load-bearing one. Job IDs are namespaced per tenant
	// (see keyForJobByID), so system job 7 and tenant acme's job 7 are
	// different jobs. Sharing a mark would let one job's claim decide the
	// other's firing, and would let deleting one erase the other's mark —
	// un-deciding a decided firing.
	if keyForClaim(7, "") == keyForClaim(7, "acme") {
		t.Error("system job 7 and a tenant's job 7 are different jobs and must " +
			"not share a claim mark")
	}
	if keyForClaim(7, "acme") == keyForClaim(7, "globex") {
		t.Error("two tenants' job 7 must not share a claim mark")
	}
	// The claim key carries the scope TOKEN; the job key carries the record
	// NAMESPACE. Those were one function until the V24 remediation, which split
	// them because they answer different questions — see
	// TestScopeTokenAndNamespace_AnswerDifferentQuestions. What must survive the
	// split is the property V4 was actually about: each key stays exactly as
	// unique as the job it names, so no two scopes can share one.
	for _, tenant := range []string{"", "acme"} {
		claim := keyForClaim(7, tenant)
		job := keyForJobByID(7, tenant)
		if !strings.HasPrefix(claim, ClaimsPrefix+scopeToken(tenant)) {
			t.Errorf("claim key %q does not carry the job scope %q", claim, scopeToken(tenant))
		}
		if !strings.HasPrefix(job, recordNamespace(tenant)) {
			t.Errorf("job key %q does not carry its record namespace %q", job, recordNamespace(tenant))
		}
	}
}

// TestMain tears the shared qrdb node down once the package is finished with
// it. Booting is lazy, so a run that touches none of these tests starts nothing.
func TestMain(m *testing.M) {
	code := m.Run()
	qrdbnode.CloseShared()
	os.Exit(code)
}

// TestClaimFiring_UnreadableMarkFailsClosed covers the case where the mark key
// exists but carries nothing readable.
//
// The compare says the key has been written, so the create is refused, but there
// is then no way to tell where the job has got to. Claiming past an unknown mark
// would be guessing, so the claim fails rather than proceeding.
func TestClaimFiring_UnreadableMarkFailsClosed(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()

	// Plant an empty value under the job's mark key through the real client.
	if err := node(t).KV.Put(ctx, keyForClaim(jobID, ""), []byte{}); err != nil {
		t.Fatalf("planting an empty mark: %v", err)
	}

	won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, time.Unix(1_700_000_000, 0), "node-A"))
	if won {
		t.Fatal("a claim must not be granted past a mark that cannot be read")
	}
	if !errors.Is(err, ErrUnreadableClaimMark) {
		t.Fatalf("expected ErrUnreadableClaimMark, got %v", err)
	}
}

// TestClaimFiring_CorruptMarkFailsClosed covers a mark that is present and
// non-empty but not decodable — a stored record from a future or broken writer.
// Same reasoning: an undecodable mark is an unknown mark.
func TestClaimFiring_CorruptMarkFailsClosed(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()

	if err := node(t).KV.Put(ctx, keyForClaim(jobID, ""), []byte("not json")); err != nil {
		t.Fatalf("planting a corrupt mark: %v", err)
	}

	won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, time.Unix(1_700_000_000, 0), "node-A"))
	if won {
		t.Fatal("a claim must not be granted past a mark that cannot be decoded")
	}
	var storageErr *scheduler.ErrStorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected ErrStorageError, got %T: %v", err, err)
	}
}

// TestDeleteJob_RemovesTheClaimMark covers condition 2 of the mark design: one
// mark per job means the mark outlives the job unless deleted with it.
func TestDeleteJob_RemovesTheClaimMark(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()

	if won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, time.Unix(1_700_000_000, 0), "node-A")); err != nil || !won {
		t.Fatalf("seeding a mark: won=%v err=%v", won, err)
	}

	if err := s.DeleteJob(ctx, jobID, ""); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}

	// The mark is gone: a fresh claim for the same job takes the create path
	// rather than losing to a leftover mark.
	won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, time.Unix(1_600_000_000, 0), "node-A"))
	if err != nil {
		t.Fatalf("claiming after deletion: %v", err)
	}
	if !won {
		t.Fatal("the job's mark must be deleted with the job; an orphaned mark " +
			"would refuse every future claim for a recreated job ID")
	}
}

// TestClaimFiring_CountsStrandedFirings covers condition 3: the monotonic mark
// silently makes a firing unclaimable, so the occurrence is counted.
//
// A job left pending at a firing the mark has already passed can never run
// again — the WP10 invariant. Nothing else surfaces it, which is why the count
// exists.
func TestClaimFiring_CountsStrandedFirings(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()
	firing := time.Unix(1_700_000_000, 0).UTC()

	// The job is pending at a firing the mark will pass.
	job := createTestJob("stranded", scheduler.OwnerTypeSystem, "")
	job.ID = jobID
	job.Status = scheduler.JobStatusPending
	job.NextRun = firing
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, firing, "node-A")); err != nil || !won {
		t.Fatalf("first claim: won=%v err=%v", won, err)
	}
	before := s.StrandedFirings()

	// A second node presents the same firing while the job is still pending at
	// it: that firing is now unrunnable anywhere.
	won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, firing, "node-B"))
	if err != nil || won {
		t.Fatalf("second claim must lose cleanly: won=%v err=%v", won, err)
	}

	if got := s.StrandedFirings(); got != before+1 {
		t.Errorf("StrandedFirings = %d, want %d — a firing the mark has passed "+
			"while its job is still pending must be counted", got, before+1)
	}
}

// TestClaimFiring_AdvancedJobIsNotCountedAsStranded is the other half: a job
// that moved on is normal arbitration, not a stranded firing, and must not
// inflate the alerting counter.
func TestClaimFiring_AdvancedJobIsNotCountedAsStranded(t *testing.T) {
	s := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()
	firing := time.Unix(1_700_000_000, 0).UTC()

	job := createTestJob("advanced", scheduler.OwnerTypeSystem, "")
	job.ID = jobID
	job.Status = scheduler.JobStatusPending
	job.NextRun = firing.Add(time.Hour) // already moved past the contested firing
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, firing, "node-A")); err != nil || !won {
		t.Fatalf("first claim: won=%v err=%v", won, err)
	}
	before := s.StrandedFirings()

	if won, _ := s.ClaimFiring(ctx, firingClaimAt(jobID, firing, "node-B")); won {
		t.Fatal("the second claim must lose")
	}

	if got := s.StrandedFirings(); got != before {
		t.Errorf("StrandedFirings = %d, want %d — ordinary churn arbitration is "+
			"not a stranded firing", got, before)
	}
}

// TestDeleteJob_JobDeletionFailureLeavesTheMark proves ordering: when the job
// record cannot be deleted, the mark is left in place.
//
// The order matters for safety. A mark removed while its job is still live
// would leave a firing that two nodes could each claim; an orphaned mark after
// a successful job delete is only debt. So the job goes first, and its failure
// stops the whole operation.
func TestDeleteJob_JobDeletionFailureLeavesTheMark(t *testing.T) {
	kv := newMockKVStore()
	n := node(t)
	storage, err := NewClusteredStorage(kv, n.Transaction, n.KV)
	if err != nil {
		t.Fatalf("NewClusteredStorage: %v", err)
	}
	jobID := jobIDs.next()
	ctx := context.Background()

	if won, err := storage.ClaimFiring(ctx, firingClaimAt(jobID, time.Unix(1_700_000_000, 0), "node-A")); err != nil || !won {
		t.Fatalf("seeding a mark: won=%v err=%v", won, err)
	}

	kv.deleteErr = errors.New("store unavailable")
	if err := storage.DeleteJob(ctx, jobID, ""); err == nil {
		t.Fatal("a failed job deletion must surface")
	}

	// The mark survives: a later claim for an older firing still loses to it.
	kv.deleteErr = nil
	won, err := storage.ClaimFiring(ctx, firingClaimAt(jobID, time.Unix(1_600_000_000, 0), "node-B"))
	if err != nil {
		t.Fatalf("claiming after the failed delete: %v", err)
	}
	if won {
		t.Error("the mark must remain when the job could not be deleted — " +
			"removing it while the job is live would let two nodes claim one firing")
	}
}

// TestClaimFiring_ScopesDoNotShareAMark is the regression for the tenant-scope
// bug: job IDs are namespaced per tenant, so the same ID names different jobs.
//
// Sharing a mark would make one job's claim decide another job's firing, and
// deleting one job would erase the other's mark — un-deciding a decided firing,
// which is the double-fire arriving through the delete path.
func TestClaimFiring_ScopesDoNotShareAMark(t *testing.T) {
	s := newClusteredFixture(t)
	ctx := context.Background()
	jobID := jobIDs.next()
	firing := time.Unix(1_700_000_000, 0).UTC()

	system := firingClaimAt(jobID, firing, "node-A")
	tenant := firingClaimAt(jobID, firing, "node-A")
	tenant.TenantID = "acme"

	if won, err := s.ClaimFiring(ctx, system); err != nil || !won {
		t.Fatalf("system job: won=%v err=%v", won, err)
	}
	if won, err := s.ClaimFiring(ctx, tenant); err != nil || !won {
		t.Fatalf("the tenant's job of the same ID is a different job and must "+
			"claim independently: won=%v err=%v", won, err)
	}

	// Deleting the tenant's job must not disturb the system job's mark.
	if err := s.DeleteJob(ctx, jobID, "acme"); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, firing, "node-B"))
	if err != nil {
		t.Fatalf("re-claiming the system firing: %v", err)
	}
	if won {
		t.Fatal("deleting a TENANT's job erased the SYSTEM job's mark — a decided " +
			"firing became claimable again")
	}
}

// TestClaimFiring_ConcurrentAdvanceYieldsOneWinner exercises the ADVANCE path
// under contention, which the first-create race does not reach.
//
// Advance is read-then-CAS, so it is the branch where an unguarded write would
// hide: with the mark already present, every claimant reads the same bytes and
// they all propose. Only the compare stops them.
func TestClaimFiring_ConcurrentAdvanceYieldsOneWinner(t *testing.T) {
	s := newClusteredFixture(t)
	ctx := context.Background()
	jobID := jobIDs.next()
	first := time.Unix(1_700_000_000, 0).UTC()

	// Seed the mark so every racing claim takes the advance path, not create.
	if won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, first, "node-A")); err != nil || !won {
		t.Fatalf("seeding the mark: won=%v err=%v", won, err)
	}

	const claimants = 12
	next := first.Add(time.Minute)
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(claimants)
	done.Add(claimants)

	results := make([]bool, claimants)
	errs := make([]error, claimants)
	for i := 0; i < claimants; i++ {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start
			results[i], errs[i] = s.ClaimFiring(ctx, firingClaimAt(jobID, next, "node"))
		}(i)
	}
	ready.Wait()
	close(start)
	done.Wait()

	winners := 0
	for i, won := range results {
		if errs[i] != nil {
			t.Fatalf("claimant %d errored: %v", i, errs[i])
		}
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1 — the advance guard is what stops "+
			"the other %d, and an unguarded read-then-write would let several through",
			winners, claimants-1)
	}
}

// TestClaimFiring_SemanticallyEmptyMarkFailsClosed covers a mark that decodes
// cleanly but says nothing: "{}" unmarshals to a zero firing.
//
// Treating that as "behind every firing" would let any claim advance past it,
// so it is refused exactly like a mark that cannot be decoded at all.
func TestClaimFiring_SemanticallyEmptyMarkFailsClosed(t *testing.T) {
	s := newClusteredFixture(t)
	ctx := context.Background()
	jobID := jobIDs.next()

	if err := node(t).KV.Put(ctx, keyForClaim(jobID, ""), []byte("{}")); err != nil {
		t.Fatalf("planting an empty-object mark: %v", err)
	}

	won, err := s.ClaimFiring(ctx, firingClaimAt(jobID, time.Unix(1_700_000_000, 0), "node-A"))
	if won {
		t.Fatal("a mark that decodes but states no firing must not be claimed past")
	}
	if !errors.Is(err, ErrUnreadableClaimMark) {
		t.Fatalf("expected ErrUnreadableClaimMark, got %v", err)
	}
}
