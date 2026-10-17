package orchestration

import (
	"sync"
	"testing"
)

// A workspace pin recorded while the run publishes a snapshot must reach the
// published status. The snapshot read the pin before the pin was recorded;
// were its store to land after the pin's republish, it would overwrite the
// republished status and the pin would be missing until the next publish.
// Each round reads the status straight after the race, with no later publish
// to hide a lost pin.
func TestJobRun_APublishDoesNotOverwriteAConcurrentWorkspacePin(t *testing.T) {
	t.Parallel()

	const rounds = 500
	for i := 0; i < rounds; i++ {
		run := newEnvelopeRun(t, JobRunConfig{})
		run.snapshot(JobStateInitializing)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := run.recordWorkspace(WorkspacePlacement{AgentHost: "agent-a", Key: "k", Dir: "/w"}); err != nil {
				t.Errorf("round %d: recordWorkspace: %v", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			run.snapshot(JobStateExecuting)
		}()
		wg.Wait()

		if got := run.Status().WorkspacePin; got == nil || got.AgentHost != "agent-a" {
			t.Fatalf("round %d: the published status lost the workspace pin: got %+v", i, got)
		}
	}
}

// A park surfaced while the run publishes a snapshot must stay surfaced on
// the published status, for the reason the workspace pin must stay pinned.
func TestJobRun_APublishDoesNotOverwriteAConcurrentParkSurfaced(t *testing.T) {
	t.Parallel()

	const rounds = 500
	for i := 0; i < rounds; i++ {
		run := newEnvelopeRun(t, JobRunConfig{})
		run.Park(TaskBlockKindNeedsInfo, "which store?", "")
		run.snapshot(JobStateEscalating)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			run.ParkSurfaced()
		}()
		go func() {
			defer wg.Done()
			run.snapshot(JobStateEscalating)
		}()
		wg.Wait()

		if got := run.Status().Park; got == nil || !got.Surfaced {
			t.Fatalf("round %d: the published status lost the surfaced park: got %+v", i, got)
		}
	}
}

// Parked is read on the conclusion goroutine while the runner re-parks and
// surfaces: it must hand out a snapshot, never the run's own park, so a
// caller's read cannot race ParkSurfaced or Park and its copy does not
// change underneath it.
func TestJobRun_ParkedReturnsASnapshotUnderConcurrentParkAndSurface(t *testing.T) {
	t.Parallel()

	const rounds = 200
	for i := 0; i < rounds; i++ {
		run := newEnvelopeRun(t, JobRunConfig{})
		run.Park(TaskBlockKindNeedsInfo, "which store?", "")
		run.snapshot(JobStateEscalating)

		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			run.ParkSurfaced()
		}()
		go func() {
			defer wg.Done()
			run.Park(TaskBlockKindNeedsInfo, "which bucket?", "")
		}()
		var seen *BlockReason
		go func() {
			defer wg.Done()
			if park := run.Parked(); park != nil {
				cp := *park
				seen = &cp
			}
		}()
		wg.Wait()

		if seen == nil {
			t.Fatalf("round %d: Parked returned nil for a parked run", i)
		}
		first := run.Parked()
		first.Surfaced = !first.Surfaced
		first.Message = "mutated by caller"
		if again := run.Parked(); again.Message == "mutated by caller" {
			t.Fatalf("round %d: Parked handed out the run's own park", i)
		}
	}
}

func TestJobRun_ParkedIsNilWithoutAPark(t *testing.T) {
	t.Parallel()

	run := newEnvelopeRun(t, JobRunConfig{})
	if got := run.Parked(); got != nil {
		t.Fatalf("Parked on an unparked run = %+v, want nil", got)
	}
}
