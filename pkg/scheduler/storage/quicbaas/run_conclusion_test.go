package quicbaas

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

func conclusion(jobID uint64, firing, started time.Time, owner string) *scheduler.RunConclusion {
	return &scheduler.RunConclusion{
		JobID: jobID, TenantID: "tenant-a", Firing: firing, StartedAt: started, Owner: owner,
	}
}

// TestConcludeRun_OneWriterPerRun is the guarded transition against the live
// transaction plane: the first writer of a run wins, a second writer of the
// same run loses, and the winner's retry wins again.
// These tests share the package's qrdb node, so each takes its own job ID to keep its mark key fresh on every run.
func TestConcludeRun_OneWriterPerRun(t *testing.T) {
	storage := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()
	firing := time.Unix(1_800_000_000, 0)
	started := firing.Add(time.Second)

	steps := []struct {
		name  string
		owner string
		want  bool
	}{
		{"the first writer concludes the run", "recovery@node-2", true},
		{"another writer of the same run loses", "execution@node-1", false},
		{"the winner's retry wins again", "recovery@node-2", true},
	}
	for _, step := range steps {
		won, err := storage.ConcludeRun(ctx, conclusion(jobID, firing, started, step.owner))
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if won != step.want {
			t.Fatalf("%s: won = %v, want %v", step.name, won, step.want)
		}
	}
}

// TestConcludeRun_TheMarkOnlyMovesForward: a later run of the job concludes
// past an earlier one, including a run re-armed at the same firing, and a
// zombie outcome of an earlier run then loses rather than overwriting.
func TestConcludeRun_TheMarkOnlyMovesForward(t *testing.T) {
	storage := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()
	firing := time.Unix(1_800_000_000, 0)

	first := conclusion(jobID, firing, firing.Add(time.Second), "execution@node-1")
	rearmed := conclusion(jobID, firing, firing.Add(time.Minute), "execution@node-2")
	next := conclusion(jobID, firing.Add(5*time.Minute), firing.Add(5*time.Minute), "execution@node-1")

	for _, run := range []*scheduler.RunConclusion{first, rearmed, next} {
		if won, err := storage.ConcludeRun(ctx, run); err != nil || !won {
			t.Fatalf("a later run (firing %s, started %s) did not conclude: won=%v err=%v",
				run.Firing, run.StartedAt, won, err)
		}
	}
	if won, err := storage.ConcludeRun(ctx, first); err != nil || won {
		t.Fatalf("an earlier run's outcome concluded over a later one: won=%v err=%v", won, err)
	}
}

func TestConcludeRun_RefusesAnIncompleteRun(t *testing.T) {
	storage := newClusteredFixture(t)
	firing := time.Unix(1_800_000_000, 0)
	for name, run := range map[string]*scheduler.RunConclusion{
		"nil":        nil,
		"no job":     conclusion(0, firing, firing, "o"),
		"no firing":  conclusion(9, time.Time{}, firing, "o"),
		"no started": conclusion(9, firing, time.Time{}, "o"),
	} {
		_, err := storage.ConcludeRun(context.Background(), run)
		var storageErr *scheduler.ErrStorageError
		if !errors.As(err, &storageErr) {
			t.Errorf("%s: err = %v, want a storage error", name, err)
		}
	}
}

// TestDeleteJob_RemovesTheConclusionMark: a job deleted and its ID never
// reissued still must not leave coordination state behind for every run.
func TestDeleteJob_RemovesTheConclusionMark(t *testing.T) {
	storage := newClusteredFixture(t)
	jobID := jobIDs.next()
	ctx := context.Background()
	firing := time.Unix(1_800_000_000, 0)
	if won, err := storage.ConcludeRun(ctx, conclusion(jobID, firing, firing, "o")); err != nil || !won {
		t.Fatalf("ConcludeRun: won=%v err=%v", won, err)
	}
	if err := storage.DeleteJob(ctx, jobID, "tenant-a"); err != nil && !isNotFound(err) {
		t.Fatalf("DeleteJob: %v", err)
	}
	held, err := storage.marks.Get(ctx, keyForConclusion(jobID, "tenant-a"))
	if err == nil && len(held) > 0 {
		t.Fatalf("the conclusion mark survived its job: %s", held)
	}
}

func isNotFound(err error) bool {
	var notFound *scheduler.JobNotFoundError
	return errors.As(err, &notFound)
}
