package orchestration

import (
	"sync"
	"testing"
)

// TestJobRun_APublishDoesNotOverwriteAConcurrentAddChildJob races one
// AddChildJob against one snapshot per round and reads the published status
// straight after, with no later publish to paper over a lost update. A
// publish that stored a status built before the child was added, after
// AddChildJob had already republished, would drop the child (#1474).
func TestJobRun_APublishDoesNotOverwriteAConcurrentAddChildJob(t *testing.T) {
	t.Parallel()
	run := newEnvelopeRun(t, JobRunConfig{})
	run.snapshot(JobStateInitializing)

	const rounds = 500
	for i := 0; i < rounds; i++ {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			run.AddChildJob("child-" + itoa(i))
		}()
		go func() {
			defer wg.Done()
			run.snapshot(JobStateExecuting)
		}()
		wg.Wait()

		if got := run.Status().ChildJobIDs; len(got) != i+1 {
			t.Fatalf("round %d: the published status lost a child: got %d IDs, want %d", i, len(got), i+1)
		}
	}
}

// TestJobRun_AddChildJobBeforeTheFirstPublishIsCarriedByIt checks the
// nothing-published-yet path: the lineage recorded before the first
// snapshot reaches the status that snapshot publishes.
func TestJobRun_AddChildJobBeforeTheFirstPublishIsCarriedByIt(t *testing.T) {
	t.Parallel()
	run := newEnvelopeRun(t, JobRunConfig{})
	run.AddChildJob("child-early")
	run.AddChildJob("")

	run.snapshot(JobStateInitializing)
	got := run.Status().ChildJobIDs
	if len(got) != 1 || got[0] != "child-early" {
		t.Fatalf("the first publish must carry the early child and skip the empty ID: got %v", got)
	}
}
