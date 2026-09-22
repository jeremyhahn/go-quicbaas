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
	"errors"
	"sync"
	"testing"
)

// =============================================================================
// The spawn claim (#670)
// =============================================================================

// spawnClaimTask seeds one pending, undispatched task and returns the store.
func spawnClaimTask(t *testing.T, tenantID, taskID string) TaskStore {
	t.Helper()
	store := NewInMemoryTaskStore()
	if err := store.CreateTask(context.Background(), &Task{
		ID: taskID, TenantID: tenantID, Subject: "claimable",
		Status: TaskStatusPending, AutoSpawn: true,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return store
}

// countRunningJobs reports how many jobs the service has launched. launchJob
// records a job before it starts its goroutine, so this is exact rather than
// a race with one.
func countRunningJobs(svc *agentServiceImpl) int {
	jobs := 0
	svc.jobs.Range(func(_, _ any) bool {
		jobs++
		return true
	})
	return jobs
}

// TestClaimTaskSpawn_OneClaimWinsAndTheLoserObservesIt is the property the
// claim exists for: two spawners released together against one pending task
// produce ONE claim, one job identity and one charged attempt, and the loser
// is handed the winner's job rather than an error.
func TestClaimTaskSpawn_OneClaimWinsAndTheLoserObservesIt(t *testing.T) {
	t.Parallel()
	const tenantID, taskID = "tenant-claim", "task-claim"
	store := spawnClaimTask(t, tenantID, taskID)
	ctx := context.Background()

	// Both spawners hold the snapshot they read BEFORE either claimed, which
	// is the state the race is made of.
	first, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	second, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	var (
		ready   sync.WaitGroup
		done    sync.WaitGroup
		release = make(chan struct{})
		claims  [2]TaskSpawnClaim
		errs    [2]error
		jobIDs  = [2]string{"job-a", "job-b"}
		tasks   = [2]*Task{first, second}
	)
	ready.Add(2)
	done.Add(2)
	for i := range tasks {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-release
			claims[i], errs[i] = ClaimTaskSpawn(ctx, store, tasks[i], jobIDs[i], int64(1_000+i))
		}(i)
	}
	ready.Wait()
	close(release)
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("spawner %d: ClaimTaskSpawn: %v", i, err)
		}
	}
	won := 0
	for _, claim := range claims {
		if claim.Claimed {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("exactly one spawner may claim the task, %d did: %+v", won, claims)
	}
	if claims[0].JobID != claims[1].JobID {
		t.Fatalf("both spawners must report the task's one open attempt: %q and %q", claims[0].JobID, claims[1].JobID)
	}

	stored, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.SpawnedJobID != claims[0].JobID {
		t.Fatalf("the record names job %q, the claims named %q", stored.SpawnedJobID, claims[0].JobID)
	}
	if stored.Attempts != 1 {
		t.Fatalf("one dispatch charges one attempt, got %d", stored.Attempts)
	}
	if stored.SpawnedAt == 0 {
		t.Fatal("a claimed attempt records when it was claimed")
	}
	for i, task := range tasks {
		if task.SpawnedJobID != stored.SpawnedJobID || task.Version != stored.Version {
			t.Fatalf("spawner %d's copy must be refreshed from the record: %+v", i, task)
		}
	}
}

// TestClaimTaskSpawn_ClearsTheReasonAndChargesFromTheRecord proves the claim
// reads the attempt ordinal off the stored record rather than the caller's
// snapshot, and that a task which is now dispatched stops advertising why it
// was waiting.
func TestClaimTaskSpawn_ClearsTheReasonAndChargesFromTheRecord(t *testing.T) {
	t.Parallel()
	const tenantID, taskID = "tenant-charge", "task-charge"
	store := spawnClaimTask(t, tenantID, taskID)
	ctx := context.Background()

	// The record has moved on since the caller read it: an attempt was
	// charged and a refusal recorded.
	current, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	stale := *current
	current.Attempts = 2
	current.DispatchReason = "project at capacity"
	if err := store.UpdateTask(ctx, current); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	claim, err := ClaimTaskSpawn(ctx, store, &stale, "job-c", 4242)
	if err != nil {
		t.Fatalf("ClaimTaskSpawn: %v", err)
	}
	if !claim.Claimed || claim.JobID != "job-c" {
		t.Fatalf("an unclaimed task is claimable: %+v", claim)
	}
	if claim.Attempt != 3 {
		t.Fatalf("the attempt ordinal follows the record (2 charged), got %d", claim.Attempt)
	}
	stored, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Attempts != 3 || stored.DispatchReason != "" {
		t.Fatalf("a dispatched task is charged once and waits for nothing: %+v", stored)
	}
}

// TestClaimTaskSpawn_RefusesWhatItCannotDecide is the invalid-input half:
// every way of asking for a claim that cannot be decided is reported typed,
// and nothing is claimed.
func TestClaimTaskSpawn_RefusesWhatItCannotDecide(t *testing.T) {
	t.Parallel()
	const tenantID, taskID = "tenant-refuse", "task-refuse"
	store := spawnClaimTask(t, tenantID, taskID)
	ctx := context.Background()
	task := &Task{ID: taskID, TenantID: tenantID}

	if _, err := ClaimTaskSpawn(ctx, nil, task, "job-x", 1); !errors.Is(err, ErrTaskSpawnClaim) {
		t.Fatalf("a claim with no store: got %v", err)
	}
	if _, err := ClaimTaskSpawn(ctx, store, nil, "job-x", 1); !errors.Is(err, ErrTaskSpawnClaim) {
		t.Fatalf("a claim with no task: got %v", err)
	}
	if _, err := ClaimTaskSpawn(ctx, store, task, "", 1); !errors.Is(err, ErrTaskSpawnClaim) {
		t.Fatalf("a claim with no job: got %v", err)
	}

	// A task the store cannot read is the store's error, not a claim.
	missing := &Task{ID: "task-missing", TenantID: tenantID}
	if _, err := ClaimTaskSpawn(ctx, store, missing, "job-x", 1); err == nil {
		t.Fatal("a claim on a task that is not there must be reported")
	}

	// A record that changes under every attempt is fail-closed: no claim.
	stubborn := &alwaysConflictingTaskStore{TaskStore: store}
	claim, err := ClaimTaskSpawn(ctx, stubborn, task, "job-x", 1)
	if !errors.Is(err, ErrTaskSpawnClaim) || claim.Claimed {
		t.Fatalf("a contended record grants nothing: claim=%+v err=%v", claim, err)
	}
	if !errors.Is(err, ErrTaskVersionConflict) {
		t.Fatalf("the refusal must carry what the store said, got %v", err)
	}
	stored, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.SpawnedJobID != "" || stored.Attempts != 0 {
		t.Fatalf("a refused claim charges nothing: %+v", stored)
	}
}

// TestReleaseTaskSpawnClaim_RefundsTheAttempt proves a claim whose job never
// started is given back whole: no open attempt, no charge, and the task is
// dispatchable again.
func TestReleaseTaskSpawnClaim_RefundsTheAttempt(t *testing.T) {
	t.Parallel()
	const tenantID, taskID = "tenant-release", "task-release"
	store := spawnClaimTask(t, tenantID, taskID)
	ctx := context.Background()

	task, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := ClaimTaskSpawn(ctx, store, task, "job-doomed", 99); err != nil {
		t.Fatalf("ClaimTaskSpawn: %v", err)
	}
	if err := ReleaseTaskSpawnClaim(ctx, store, task, "job-doomed"); err != nil {
		t.Fatalf("ReleaseTaskSpawnClaim: %v", err)
	}

	stored, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.SpawnedJobID != "" || stored.SpawnedAt != 0 || stored.Attempts != 0 {
		t.Fatalf("a released claim leaves no attempt behind: %+v", stored)
	}
	if task.SpawnedJobID != "" || task.Attempts != 0 {
		t.Fatalf("the caller's copy must match the record: %+v", task)
	}

	// The task is claimable again, by the next sweep or by anyone.
	claim, err := ClaimTaskSpawn(ctx, store, task, "job-next", 100)
	if err != nil || !claim.Claimed || claim.Attempt != 1 {
		t.Fatalf("a released task is dispatchable again: claim=%+v err=%v", claim, err)
	}
}

// TestReleaseTaskSpawnClaim_LeavesWhatIsNotItsOwn is the invalid-input half:
// a release must not take the task's open attempt away from another claim,
// and a release that names nothing is reported typed.
func TestReleaseTaskSpawnClaim_LeavesWhatIsNotItsOwn(t *testing.T) {
	t.Parallel()
	const tenantID, taskID = "tenant-foreign", "task-foreign"
	store := spawnClaimTask(t, tenantID, taskID)
	ctx := context.Background()

	task, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := ClaimTaskSpawn(ctx, store, task, "job-winner", 7); err != nil {
		t.Fatalf("ClaimTaskSpawn: %v", err)
	}

	// A loser releasing "its" claim would hand the winner's attempt away.
	if err := ReleaseTaskSpawnClaim(ctx, store, task, "job-loser"); err != nil {
		t.Fatalf("releasing a claim the task does not hold is a no-op: %v", err)
	}
	stored, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.SpawnedJobID != "job-winner" || stored.Attempts != 1 {
		t.Fatalf("the winner's attempt must survive a foreign release: %+v", stored)
	}

	if err := ReleaseTaskSpawnClaim(ctx, nil, task, "job-winner"); !errors.Is(err, ErrTaskSpawnClaim) {
		t.Fatalf("a release with no store: got %v", err)
	}
	if err := ReleaseTaskSpawnClaim(ctx, store, nil, "job-winner"); !errors.Is(err, ErrTaskSpawnClaim) {
		t.Fatalf("a release with no task: got %v", err)
	}
	if err := ReleaseTaskSpawnClaim(ctx, store, task, ""); !errors.Is(err, ErrTaskSpawnClaim) {
		t.Fatalf("a release with no job: got %v", err)
	}
	if err := ReleaseTaskSpawnClaim(ctx, store, &Task{ID: "nope", TenantID: tenantID}, "job-winner"); err == nil {
		t.Fatal("a release on a task that is not there must be reported")
	}
	stubborn := &alwaysConflictingTaskStore{TaskStore: store}
	if err := ReleaseTaskSpawnClaim(ctx, stubborn, task, "job-winner"); !errors.Is(err, ErrTaskSpawnClaim) {
		t.Fatalf("a contended record is reported rather than assumed released: %v", err)
	}
}

// TestAgentService_SpawnTask_TwoSpawnersStartOneJob is #670 end to end: the
// dispatcher's sweep and auto-spawn both reach SpawnTask holding a snapshot
// taken before either dispatched. Exactly one job must exist, both callers
// must be told the same job, and the task must be charged exactly one attempt.
func TestAgentService_SpawnTask_TwoSpawnersStartOneJob(t *testing.T) {
	const tenantID = "tenant-spawn-race"
	impl, taskStore := newSpawnerService(t, tenantID)
	ctx := context.Background()

	release := make(chan struct{})
	defer close(release)
	runner := &fakeRunner{
		name:   "spawn-race-test",
		states: []JobStateSpec{{State: JobStateInitializing, Label: "Initializing"}},
		run: func(ctx context.Context, _ *JobRun) error {
			// The attempt stays open until the test is done looking at it.
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		},
	}
	if err := RegisterJobRunner(runner); err != nil {
		t.Fatalf("RegisterJobRunner: %v", err)
	}
	defer cancelAllJobs(impl)

	created, err := impl.CreateTask(ctx, &CreateTaskRequest{
		TenantID: tenantID, Subject: "raced", Runner: runner.Name(),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	first, err := taskStore.GetTask(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	second, err := taskStore.GetTask(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	var (
		ready    sync.WaitGroup
		done     sync.WaitGroup
		go2      = make(chan struct{})
		jobIDs   [2]string
		spawnErr [2]error
		tasks    = [2]*Task{first, second}
	)
	ready.Add(2)
	done.Add(2)
	for i := range tasks {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-go2
			jobIDs[i], spawnErr[i] = impl.SpawnTask(ctx, tasks[i])
		}(i)
	}
	ready.Wait()
	close(go2)
	done.Wait()

	for i, err := range spawnErr {
		if err != nil {
			t.Fatalf("spawner %d: SpawnTask: %v", i, err)
		}
	}
	if jobIDs[0] == "" || jobIDs[0] != jobIDs[1] {
		t.Fatalf("both spawners must be told the task's one job: %q and %q", jobIDs[0], jobIDs[1])
	}
	if n := countRunningJobs(impl); n != 1 {
		t.Fatalf("one pending task starts one job, %d were started", n)
	}
	stored, err := taskStore.GetTask(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.SpawnedJobID != jobIDs[0] {
		t.Fatalf("the record names job %q, the spawners were told %q", stored.SpawnedJobID, jobIDs[0])
	}
	if stored.Attempts != 1 {
		t.Fatalf("two spawners must not charge two attempts, got %d", stored.Attempts)
	}
}

// TestTaskSpawnClaimError_MessageAndSentinel: the claim's error names the
// task and the store failure under it, and unwraps to both.
func TestTaskSpawnClaimError_MessageAndSentinel(t *testing.T) {
	t.Parallel()
	bare := &TaskSpawnClaimError{Message: "a claim needs a task store"}
	if !errors.Is(bare, ErrTaskSpawnClaim) {
		t.Fatalf("%v must unwrap to its sentinel", bare)
	}
	if bare.Error() != "task spawn claim: undecided: a claim needs a task store" {
		t.Fatalf("unexpected message: %q", bare.Error())
	}

	cause := errors.New("kv down")
	full := &TaskSpawnClaimError{TaskID: "task-1", Message: "too contended", Cause: cause}
	if !errors.Is(full, ErrTaskSpawnClaim) || !errors.Is(full, cause) {
		t.Fatalf("%v must unwrap to the sentinel and the cause", full)
	}
	if full.Error() != "task spawn claim: undecided: too contended: task=task-1: kv down" {
		t.Fatalf("unexpected message: %q", full.Error())
	}
}

// TestClaimTaskSpawn_ClaimsTheTaskForTheJob is the claim's other half (#966):
// the job the engine dispatches is the task's claimant, so the claim moves the
// record off the queue exactly as an agent's ClaimTask does. A task left
// pending for the life of its job reads as queued to every consumer of
// Task.Status and makes a healthy run indistinguishable from a stall.
func TestClaimTaskSpawn_ClaimsTheTaskForTheJob(t *testing.T) {
	t.Parallel()
	const tenantID, taskID = "tenant-claimed", "task-claimed"
	store := spawnClaimTask(t, tenantID, taskID)
	ctx := context.Background()

	task, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	claim, err := ClaimTaskSpawn(ctx, store, task, "job-live", 4242)
	if err != nil {
		t.Fatalf("ClaimTaskSpawn: %v", err)
	}
	if !claim.Claimed {
		t.Fatalf("a pending task is claimable: %+v", claim)
	}

	stored, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Status != TaskStatusInProgress {
		t.Fatalf("the dispatched task reads %q, want in_progress", stored.Status)
	}
	if stored.ClaimedAt != 4242 || stored.SpawnedAt != 4242 {
		t.Fatalf("the claim and the dispatch share one instant: %+v", stored)
	}
	if stored.StatusChangedAt == 0 {
		t.Fatalf("the edge is stamped, so an idle-age report reads the age of the RUN: %+v", stored)
	}
	if task.Status != TaskStatusInProgress {
		t.Fatalf("the caller's copy must match the record: %+v", task)
	}

	// A SECOND SPAWNER OBSERVES THE CLAIM rather than re-claiming it: the
	// record is already in_progress, which no transition admits a second
	// claim out of.
	observer, err := ClaimTaskSpawn(ctx, store, task, "job-second", 5000)
	if err != nil {
		t.Fatalf("an observer reports the open attempt: %v", err)
	}
	if observer.Claimed || observer.JobID != "job-live" {
		t.Fatalf("the second spawner must observe the winner's job: %+v", observer)
	}
}

// TestClaimTaskSpawn_RefusesATaskThatIsNotQueued is the invalid-state half:
// the engine dispatches out of the QUEUE, so a task in any other state is
// refused typed and charged nothing.
//
// A CLAIMED TASK IS REFUSED TOO, and it is the case the transition table
// cannot state: claimed -> in_progress is admissible because it is an AGENT
// starting the work it claimed. A task with an owner is not the engine's to
// dispatch, and standing down is what leaves one claimant.
func TestClaimTaskSpawn_RefusesATaskThatIsNotQueued(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for name, park := range map[string]func(t *testing.T, store TaskStore, tenantID, taskID string) TaskStatus{
		"parked": func(t *testing.T, store TaskStore, tenantID, taskID string) TaskStatus {
			t.Helper()
			if err := store.BlockTask(ctx, tenantID, taskID, TaskBlockKindNeedsInfo, "which branch?"); err != nil {
				t.Fatalf("BlockTask: %v", err)
			}
			return TaskStatusBlocked
		},
		"claimed by an agent": func(t *testing.T, store TaskStore, tenantID, taskID string) TaskStatus {
			t.Helper()
			if err := store.ClaimTask(ctx, tenantID, taskID, "agent-a"); err != nil {
				t.Fatalf("ClaimTask: %v", err)
			}
			return TaskStatusClaimed
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tenantID, taskID := "tenant-"+name, "task-"+name
			store := spawnClaimTask(t, tenantID, taskID)
			want := park(t, store, tenantID, taskID)
			task, err := store.GetTask(ctx, tenantID, taskID)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}

			claim, err := ClaimTaskSpawn(ctx, store, task, "job-refused", 9)
			var invalid *TaskInvalidStateError
			if !errors.As(err, &invalid) || !errors.Is(err, ErrTaskInvalidState) {
				t.Fatalf("a task that is not queued cannot be claimed for a job: %v", err)
			}
			if claim.Claimed {
				t.Fatalf("a refused claim claims nothing: %+v", claim)
			}
			if invalid.CurrentState != want || invalid.TargetState != TaskStatusInProgress {
				t.Fatalf("the refusal must name the edge it refused: %+v", invalid)
			}
			stored, err := store.GetTask(ctx, tenantID, taskID)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if stored.Status != want || stored.SpawnedJobID != "" || stored.Attempts != 0 {
				t.Fatalf("a refused claim writes nothing: %+v", stored)
			}
		})
	}
}

// TestReleaseTaskSpawnClaim_ReturnsTheTaskToTheQueue is the release's half of
// the same statement: a claim whose job never started gives the STATUS back
// too, because a task left in_progress with no job is exactly the stranded
// binding the dispatcher's sweep has to repair, and the next sweep lists the
// queue.
func TestReleaseTaskSpawnClaim_ReturnsTheTaskToTheQueue(t *testing.T) {
	t.Parallel()
	const tenantID, taskID = "tenant-requeued", "task-requeued"
	store := spawnClaimTask(t, tenantID, taskID)
	ctx := context.Background()

	task, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := ClaimTaskSpawn(ctx, store, task, "job-doomed", 11); err != nil {
		t.Fatalf("ClaimTaskSpawn: %v", err)
	}
	if err := ReleaseTaskSpawnClaim(ctx, store, task, "job-doomed"); err != nil {
		t.Fatalf("ReleaseTaskSpawnClaim: %v", err)
	}

	stored, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Status != TaskStatusPending || stored.ClaimedAt != 0 {
		t.Fatalf("a released claim leaves the task in the queue: %+v", stored)
	}
	if task.Status != TaskStatusPending {
		t.Fatalf("the caller's copy must match the record: %+v", task)
	}

	// A RELEASE DOES NOT UNDO A DECISION TAKEN SINCE THE CLAIM. A task parked
	// while the release was in flight keeps its park: the claim's status is
	// not the release's to restore from anything but its own.
	fresh, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := ClaimTaskSpawn(ctx, store, fresh, "job-parked-mid-flight", 12); err != nil {
		t.Fatalf("ClaimTaskSpawn: %v", err)
	}
	if err := store.BlockTask(ctx, tenantID, taskID, TaskBlockKindBlocked, "the operator held it"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	if err := ReleaseTaskSpawnClaim(ctx, store, fresh, "job-parked-mid-flight"); err != nil {
		t.Fatalf("ReleaseTaskSpawnClaim: %v", err)
	}
	parked, err := store.GetTask(ctx, tenantID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if parked.Status != TaskStatusBlocked || parked.BlockedReason != "the operator held it" {
		t.Fatalf("a release must not resurrect a parked task: %+v", parked)
	}
	if parked.SpawnedJobID != "" || parked.Attempts != 0 {
		t.Fatalf("the attempt is still refunded: %+v", parked)
	}
}
