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

package sdlc

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// rerunMarksIn counts the rerun marks the tenant's store holds.
func rerunMarksIn(t *testing.T, resolver *memoryResolver) int {
	t.Helper()
	resolver.mu.Lock()
	kv := resolver.stores[testTenant]
	resolver.mu.Unlock()
	marks := 0
	err := kv.Scan(context.Background(), triageRerunEntityType+"/", func(string, []byte) error {
		marks++
		return nil
	})
	if err != nil {
		t.Fatalf("scan marks: %v", err)
	}
	return marks
}

// newRerunScheduler builds a scheduler whose every start loses the lease,
// over a real Store that keeps the marks.
func newRerunScheduler(t *testing.T) (*TriageScheduler, *Store, *memoryResolver) {
	t.Helper()
	resolver := newMemoryResolver()
	store, err := NewStore(resolver)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.PutBinding(context.Background(), testTenant, testBinding()); err != nil {
		t.Fatalf("put binding: %v", err)
	}
	starter := &fakeJobStarter{err: &orchestration.JobLeaseTakenError{JobID: "job-2", HeldBy: "job-1"}}
	scheduler, err := NewTriageScheduler(TriageSchedulerConfig{Bindings: store, Jobs: starter, Reruns: store})
	if err != nil {
		t.Fatalf("NewTriageScheduler: %v", err)
	}
	return scheduler, store, resolver
}

func TestTriageScheduler_ALostLeaseLeavesOneRerunMark(t *testing.T) {
	scheduler, store, resolver := newRerunScheduler(t)
	ctx := context.Background()

	for nudge := 1; nudge <= 2; nudge++ {
		if err := scheduler.TriageProject(ctx, testTenant, testProject); err != nil {
			t.Fatalf("nudge %d: a project already being triaged is not an error: %v", nudge, err)
		}
		if marks := rerunMarksIn(t, resolver); marks != 1 {
			t.Fatalf("after nudge %d the project holds %d marks, want exactly 1", nudge, marks)
		}
	}
	taken, err := store.TakeTriageRerun(ctx, testTenant, testProject)
	if err != nil || !taken {
		t.Fatalf("TakeTriageRerun = %v, %v; want the mark the nudges left", taken, err)
	}
}

func TestTriageScheduler_AMarkItCannotWriteIsReported(t *testing.T) {
	scheduler, _, resolver := newRerunScheduler(t)
	resolver.mu.Lock()
	resolver.stores[testTenant] = plainKV{KVStore: resolver.stores[testTenant]}
	resolver.mu.Unlock()

	err := scheduler.TriageProject(context.Background(), testTenant, testProject)
	var unrecorded *TriageRerunUnrecordedError
	if !errors.As(err, &unrecorded) || unrecorded.ProjectID != testProject {
		t.Fatalf("TriageProject = %v, want a TriageRerunUnrecordedError for %s", err, testProject)
	}
	if !errors.Is(err, ErrTriageRerunUnrecorded) || !errors.Is(err, ErrGuardedWriteUnsupported) {
		t.Fatalf("the error must name the lost nudge and the store's refusal: %v", err)
	}
}

func TestTriageScheduler_UnwiredMarksDropTheNudgeAsBefore(t *testing.T) {
	scheduler, starter, _ := newTriageScheduler(t)
	starter.err = &orchestration.JobLeaseTakenError{JobID: "job-2", HeldBy: "job-1"}

	if err := scheduler.TriageProject(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("with no marks wired a lost lease is still quiet: %v", err)
	}
}

func TestTakeTriageRerun_NoMarkIsNotAnError(t *testing.T) {
	store := newTestStore(t)
	taken, err := store.TakeTriageRerun(context.Background(), testTenant, testProject)
	if err != nil || taken {
		t.Fatalf("TakeTriageRerun with no mark = %v, %v; want false, nil", taken, err)
	}
}

func TestTakeTriageRerun_TakesAMarkOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.MarkTriageRerun(ctx, testTenant, testProject); err != nil {
		t.Fatalf("MarkTriageRerun: %v", err)
	}
	for take, want := range []bool{true, false} {
		taken, err := store.TakeTriageRerun(ctx, testTenant, testProject)
		if err != nil || taken != want {
			t.Fatalf("take %d = %v, %v; want %v, nil", take+1, taken, err, want)
		}
	}
}

func TestTriageRerunMarks_RefuseWhatTheyCannotKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	var keyErr *KeySegmentError
	if err := store.MarkTriageRerun(ctx, testTenant, ""); !errors.As(err, &keyErr) {
		t.Fatalf("MarkTriageRerun with no project = %v, want a KeySegmentError", err)
	}
	if _, err := store.TakeTriageRerun(ctx, testTenant, "a/b"); !errors.As(err, &keyErr) {
		t.Fatalf("TakeTriageRerun with a separator = %v, want a KeySegmentError", err)
	}
}

func TestTriageRunner_AMarkSetBeforeTheListingIsSatisfiedByIt(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(501, "issue")
	rig.answers("SDLC-TRIAGE: needs-info")
	if err := rig.store.MarkTriageRerun(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("MarkTriageRerun: %v", err)
	}

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if listings := rig.issueListings(); listings != 1 {
		t.Fatalf("a mark set before the listing costs no second one, got %d listings", listings)
	}
	if spawns := rig.agents.sessionCount(); spawns != 1 {
		t.Fatalf("one pass holds one session, got %d", spawns)
	}
	if taken, err := rig.store.TakeTriageRerun(context.Background(), testTenant, testProject); err != nil || taken {
		t.Fatalf("the run consumes the mark; TakeTriageRerun = %v, %v", taken, err)
	}
}

func TestTriageRunner_ANudgeDuringThePassIsListedAgain(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(501, "issue")
	rig.agents.mu.Lock()
	rig.agents.replyFn = func(turn int) *orchestration.PromptSessionResponse {
		if turn == 1 {
			// An issue opened while the first pass is judging, and the nudge
			// it caused losing the lease to this very job.
			rig.forge.putIssue(502, "opened mid-pass")
			if err := rig.store.MarkTriageRerun(context.Background(), testTenant, testProject); err != nil {
				t.Errorf("MarkTriageRerun: %v", err)
			}
		}
		return workerSaid("SDLC-TRIAGE: needs-info")
	}
	rig.agents.mu.Unlock()

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if listings := rig.issueListings(); listings != 2 {
		t.Fatalf("the mark sends the job round again, got %d listings", listings)
	}
	if spawns := rig.agents.sessionCount(); spawns != 2 {
		t.Fatalf("each pass holds a fresh session, got %d", spawns)
	}
	if !promptedAbout(rig, "opened mid-pass") {
		t.Fatal("the second listing must judge the issue opened during the first pass")
	}
	rig.agents.mu.Lock()
	closed := len(rig.agents.closed)
	rig.agents.mu.Unlock()
	if closed != 2 {
		t.Fatalf("every session is given back, got %d closes", closed)
	}
}

// A rerun reads the head again. When that read fails, the round must not
// inherit the previous round's head: its verdicts would be skipped against a
// tree it never checked out, and stamped with that tree.
func TestTriageRunner_ARerunThatCannotReadTheHeadDoesNotReuseThePreviousOne(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(501, "issue")
	rig.agents.mu.Lock()
	rig.agents.replyFn = func(turn int) *orchestration.PromptSessionResponse {
		if turn == 1 {
			// The branch read starts failing after the first round judged
			// the issue at fakeForgeHeadSHA, and a nudge sends the job round.
			rig.forge.mu.Lock()
			rig.forge.getBranchErr = &NotFoundError{Entity: "branch", ID: "main"}
			rig.forge.mu.Unlock()
			if err := rig.store.MarkTriageRerun(context.Background(), testTenant, testProject); err != nil {
				t.Errorf("MarkTriageRerun: %v", err)
			}
			return workerSaid("Out of scope.\n\nSDLC-TRIAGE: not-actionable")
		}
		return workerSaid("Out of scope for a second reason.\n\nSDLC-TRIAGE: not-actionable")
	}
	rig.agents.mu.Unlock()

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if listings := rig.issueListings(); listings != 2 {
		t.Fatalf("the mark sends the job round again, got %d listings", listings)
	}
	comments := rig.comments(501)
	if len(comments) != 2 {
		t.Fatalf("the rerun must re-judge the issue rather than trust the old head's verdict, got %d comments", len(comments))
	}
	if head := triageVerdictHead(comments[0]); !strings.EqualFold(head, fakeForgeHeadSHA) {
		t.Fatalf("the first verdict records head %q, want %q", head, fakeForgeHeadSHA)
	}
	if head := triageVerdictHead(comments[1]); head != "" {
		t.Fatalf("a verdict formed without a readable head is stamped %q, want unstamped", head)
	}
}

func TestTriageRunner_StopsGoingRoundAfterTheBound(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(501, "issue")
	rig.agents.mu.Lock()
	rig.agents.replyFn = func(turn int) *orchestration.PromptSessionResponse {
		// A project nudged on every pass: each pass sees a new issue and a
		// new mark.
		rig.forge.putIssue(600+turn, "nudge "+strconv.Itoa(turn))
		if err := rig.store.MarkTriageRerun(context.Background(), testTenant, testProject); err != nil {
			t.Errorf("MarkTriageRerun: %v", err)
		}
		return workerSaid("SDLC-TRIAGE: needs-info")
	}
	rig.agents.mu.Unlock()

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if listings := rig.issueListings(); listings != 1+triageRerunRounds {
		t.Fatalf("one job lists at most %d times, got %d", 1+triageRerunRounds, listings)
	}
	taken, err := rig.store.TakeTriageRerun(context.Background(), testTenant, testProject)
	if err != nil || !taken {
		t.Fatalf("past the bound the mark is left for the next job; TakeTriageRerun = %v, %v", taken, err)
	}
}

// issueListings is how many times the run listed the project's issues.
func (r *triageRig) issueListings() int {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	return r.forge.issueListings
}

// promptedAbout reports whether any triage prompt carried text.
func promptedAbout(rig *triageRig, text string) bool {
	for _, prompt := range rig.agents.promptTexts() {
		if strings.Contains(prompt, text) {
			return true
		}
	}
	return false
}
