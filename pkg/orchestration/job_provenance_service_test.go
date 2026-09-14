// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// seedProvenanceTask writes a concluded task with a published delivery.
func seedProvenanceTask(t *testing.T, store TaskStore, task *Task) *Task {
	t.Helper()
	task.TenantID = "acme"
	if task.Subject == "" {
		task.Subject = "work"
	}
	if task.Status == "" {
		task.Status = TaskStatusCompleted
	}
	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

func TestWithTaskProvenanceLaysTheTaskFactsOverACopy(t *testing.T) {
	svc, store := newConcludingService(t)
	seedProvenanceTask(t, store, &Task{
		ID:            "task-prov-1",
		Status:        TaskStatusBlocked,
		AttemptBudget: 5,
		Metadata:      map[string]string{attemptHistoryKey: "job-a,job-b"},
		OutputArtifacts: []Artifact{
			// Published by attempt 2, which is the attempt being read below.
			pullRequestArtifactForAttempt(2, "57", "merged", "issue/57", "http://forge/pulls/57"),
		},
		ConclusionReason: TaskConcludedAbandoned,
	})

	original := &JobStatus{JobID: "job-b", TenantID: "acme", TaskID: "task-prov-1", Attempt: 2}
	enriched, err := svc.withTaskProvenance(context.Background(), "acme", original)
	if err != nil {
		t.Fatalf("withTaskProvenance: %v", err)
	}
	if enriched == original {
		t.Fatal("the published status was enriched in place; it is immutable")
	}
	if original.AttemptBudget != 0 || original.Delivery != nil || original.TerminalReason != "" {
		t.Errorf("the original status was written through: %+v", original)
	}
	if enriched.AttemptBudget != 5 {
		t.Errorf("attempt budget = %d, want 5", enriched.AttemptBudget)
	}
	if len(enriched.Attempts) != 2 || !enriched.Attempts[1].Current {
		t.Errorf("attempts = %+v, want two with the second current", enriched.Attempts)
	}
	if enriched.TerminalReason != TaskConcludedAbandoned {
		t.Errorf("terminal reason = %q, want abandoned", enriched.TerminalReason)
	}
	if !enriched.ReadmitAllowed {
		t.Error("a blocked task's job says it may not be readmitted")
	}
	if enriched.Delivery == nil || enriched.Delivery.PullRequestNumber != 57 {
		t.Errorf("delivery = %+v, want pull request 57", enriched.Delivery)
	}
}

func TestWithTaskProvenanceRefusesReadmitOnAFailedTask(t *testing.T) {
	svc, store := newConcludingService(t)
	seedProvenanceTask(t, store, &Task{
		ID:               "task-prov-2",
		Status:           TaskStatusFailed,
		ConclusionReason: TaskConcludedBudgetExhausted,
	})

	enriched, err := svc.withTaskProvenance(context.Background(), "acme",
		&JobStatus{JobID: "job-x", TenantID: "acme", TaskID: "task-prov-2", Attempt: 3})
	if err != nil {
		t.Fatalf("withTaskProvenance: %v", err)
	}
	if enriched.ReadmitAllowed {
		t.Error("a task that failed on its spent budget offers a readmit; failed is terminal")
	}
	if enriched.TerminalReason != TaskConcludedBudgetExhausted {
		t.Errorf("terminal reason = %q, want budget_exhausted", enriched.TerminalReason)
	}
}

func TestWithTaskProvenanceLeavesAJobThatIsNotAnAttemptAlone(t *testing.T) {
	svc, _ := newConcludingService(t)
	status := &JobStatus{JobID: "job-free", TenantID: "acme"}
	enriched, err := svc.withTaskProvenance(context.Background(), "acme", status)
	if err != nil {
		t.Fatalf("withTaskProvenance: %v", err)
	}
	if enriched != status {
		t.Error("a job with no task was copied; there is nothing to lay over it")
	}
}

func TestWithTaskProvenanceSurvivesATaskItCannotRead(t *testing.T) {
	// The job's own record is complete and useful; taking the whole page away
	// over the part of it that is missing would be the worse failure.
	svc, _ := newConcludingService(t)
	status := &JobStatus{JobID: "job-y", TenantID: "acme", TaskID: "no-such-task"}
	enriched, err := svc.withTaskProvenance(context.Background(), "acme", status)
	if err != nil {
		t.Fatalf("withTaskProvenance: %v", err)
	}
	if enriched != status {
		t.Error("an unreadable task produced an enriched copy")
	}
}

func TestWithTaskProvenanceRefusesAnUndecodableDelivery(t *testing.T) {
	svc, store := newConcludingService(t)
	seedProvenanceTask(t, store, &Task{
		ID:     "task-prov-3",
		Status: TaskStatusCompleted,
		OutputArtifacts: []Artifact{
			{Kind: ArtifactKindInline, Name: ArtifactNameReviews, Content: []byte("not json")},
		},
	})

	_, err := svc.withTaskProvenance(context.Background(), "acme",
		&JobStatus{JobID: "job-z", TenantID: "acme", TaskID: "task-prov-3"})
	if !errors.Is(err, ErrJobArtifactDecode) {
		t.Fatalf("error = %v, want ErrJobArtifactDecode", err)
	}
}

func TestWithTaskProvenanceIsANoOpWithoutATaskStore(t *testing.T) {
	svc := newTestService(t)
	status := &JobStatus{JobID: "job-n", TenantID: "acme", TaskID: "task-1"}
	enriched, err := svc.withTaskProvenance(context.Background(), "acme", status)
	if err != nil {
		t.Fatalf("withTaskProvenance: %v", err)
	}
	if enriched != status {
		t.Error("a service with no task store enriched a status from nowhere")
	}
}

func TestWithTaskProvenanceHandlesANilStatus(t *testing.T) {
	svc, _ := newConcludingService(t)
	enriched, err := svc.withTaskProvenance(context.Background(), "acme", nil)
	if err != nil {
		t.Fatalf("withTaskProvenance: %v", err)
	}
	if enriched != nil {
		t.Errorf("a nil status produced %+v", enriched)
	}
}

// =============================================================================
// The conclusion reason travels with the record
// =============================================================================

func TestConcludeAttemptWritesTheTypedReasonOntoTheTask(t *testing.T) {
	svc, store := newConcludingService(t)
	task := seedSpawnedTask(t, store, &Task{ID: "task-reason-1", Status: TaskStatusInProgress})

	err := svc.concludeAttempt(ContextWithTenant(context.Background(), "acme"), "acme", task.ID,
		attemptConclusion{status: &JobStatus{JobID: "job-1", TenantID: "acme", State: JobStateCompleted}})
	if err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}

	stored, getErr := store.GetTask(context.Background(), "acme", task.ID)
	if getErr != nil {
		t.Fatalf("GetTask: %v", getErr)
	}
	if stored.ConclusionReason != TaskConcludedCompleted {
		t.Errorf("conclusion reason = %q, want completed", stored.ConclusionReason)
	}
}

func TestConcludeAttemptClearsTheReasonOnARequeue(t *testing.T) {
	// A requeue is not a conclusion: a stale reason left on the record would
	// have the job page report a task that is running as one that stopped.
	svc, store := newConcludingService(t)
	task := seedSpawnedTask(t, store, &Task{
		ID:               "task-reason-2",
		Status:           TaskStatusInProgress,
		AttemptBudget:    5,
		ConclusionReason: TaskConcludedParkedBlocked,
	})

	err := svc.concludeAttempt(ContextWithTenant(context.Background(), "acme"), "acme", task.ID,
		attemptConclusion{status: &JobStatus{JobID: "job-1", TenantID: "acme", State: JobStateFailed, Error: "the work failed"}})
	if err != nil {
		t.Fatalf("concludeAttempt: %v", err)
	}

	stored, getErr := store.GetTask(context.Background(), "acme", task.ID)
	if getErr != nil {
		t.Fatalf("GetTask: %v", getErr)
	}
	if stored.Status != TaskStatusPending {
		t.Fatalf("task status = %q, want pending", stored.Status)
	}
	if stored.ConclusionReason != "" {
		t.Errorf("conclusion reason = %q, want it cleared by the requeue", stored.ConclusionReason)
	}
}

// TestWithTaskProvenanceGivesEachAttemptItsOwnDelivery is the end of the same
// property, through the service: one task, two attempts that each published,
// and two jobs that must not borrow each other's work.
func TestWithTaskProvenanceGivesEachAttemptItsOwnDelivery(t *testing.T) {
	svc, store := newConcludingService(t)
	seedProvenanceTask(t, store, &Task{
		ID:       "task-attempts",
		Status:   TaskStatusFailed,
		Metadata: map[string]string{attemptHistoryKey: "job-a,job-b"},
		OutputArtifacts: []Artifact{
			pullRequestArtifactForAttempt(1, "40", "closed", "issue/26-first", "http://forge/pulls/40"),
			pullRequestArtifactForAttempt(2, "57", "merged", "issue/26-second", "http://forge/pulls/57"),
		},
		ConclusionReason: TaskConcludedBudgetExhausted,
	})

	firstJob, err := svc.withTaskProvenance(context.Background(), "acme",
		&JobStatus{JobID: "job-a", TenantID: "acme", TaskID: "task-attempts", Attempt: 1})
	if err != nil {
		t.Fatalf("withTaskProvenance(attempt 1): %v", err)
	}
	if firstJob.Delivery == nil || firstJob.Delivery.PullRequestNumber != 40 {
		t.Errorf("attempt 1's job = %+v, want its own pull request 40", firstJob.Delivery)
	}

	secondJob, err := svc.withTaskProvenance(context.Background(), "acme",
		&JobStatus{JobID: "job-b", TenantID: "acme", TaskID: "task-attempts", Attempt: 2})
	if err != nil {
		t.Fatalf("withTaskProvenance(attempt 2): %v", err)
	}
	if secondJob.Delivery == nil || secondJob.Delivery.PullRequestNumber != 57 {
		t.Errorf("attempt 2's job = %+v, want its own pull request 57", secondJob.Delivery)
	}
}

// TestWithTaskProvenanceResolvesOffloadedReviewRounds is the failure mode that
// only shows up on the reviews worth reading.
//
// SubmitTaskArtifacts rewrites an inline artifact past the store's limit to a
// kv_key and clears Content. Decoding the task record alone therefore showed
// every SHORT review and silently dropped every long one — the findings a
// reader most needs, gone with no error anywhere.
func TestWithTaskProvenanceResolvesOffloadedReviewRounds(t *testing.T) {
	// A limit small enough that any real review round crosses it.
	const inlineLimit = 32

	svc := newTestService(t)
	store := NewInMemoryTaskStoreWithKV(NewMemoryTaskKV(), inlineLimit)
	svc.SetTaskStore(store)

	findings := strings.Repeat("the branch needs a regression test. ", 8)
	rounds := `[{"round":1,"reviewer":"codex","verdict":"request_changes","head_sha":"abc","findings":"` +
		findings + `"}]`
	if len(rounds) <= inlineLimit {
		t.Fatalf("fixture is %d bytes, which does not cross the %d-byte limit", len(rounds), inlineLimit)
	}

	ctx := context.Background()
	task := &Task{ID: "task-offload", TenantID: "acme", Subject: "work", Status: TaskStatusCompleted}
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.SubmitTaskArtifacts(ctx, "acme", task.ID, []Artifact{{
		Kind:    ArtifactKindInline,
		Name:    ArtifactNameForAttempt(ArtifactNameReviews, 1),
		Content: []byte(rounds),
	}}); err != nil {
		t.Fatalf("SubmitTaskArtifacts: %v", err)
	}

	// Precondition: the store really did offload it, or this proves nothing.
	stored, err := store.GetTask(ctx, "acme", task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(stored.OutputArtifacts) != 1 || stored.OutputArtifacts[0].Kind != ArtifactKindKVKey {
		t.Fatalf("artifact = %+v, want it offloaded to a kv_key", stored.OutputArtifacts)
	}
	if len(stored.OutputArtifacts[0].Content) != 0 {
		t.Fatal("the offloaded artifact still carries content; the fixture does not reproduce the bug")
	}

	enriched, err := svc.withTaskProvenance(ctx, "acme",
		&JobStatus{JobID: "job-1", TenantID: "acme", TaskID: task.ID, Attempt: 1})
	if err != nil {
		t.Fatalf("withTaskProvenance: %v", err)
	}
	if enriched.Delivery == nil {
		t.Fatal("an offloaded review artifact produced no delivery at all")
	}
	if len(enriched.Delivery.Reviews) != 1 {
		t.Fatalf("review rounds = %+v, want the one that was offloaded", enriched.Delivery.Reviews)
	}
	if enriched.Delivery.Reviews[0].Findings != findings {
		t.Errorf("findings = %q, want the full offloaded body", enriched.Delivery.Reviews[0].Findings)
	}
}

// TestJobDeliveryRefusesAnUnresolvedReference: the backstop. A caller that
// hands over a kv_key artifact without resolving it gets an error, never a
// delivery that quietly omits the rounds.
func TestJobDeliveryRefusesAnUnresolvedReference(t *testing.T) {
	_, err := JobDeliveryFromArtifacts([]Artifact{{
		Kind: ArtifactKindKVKey,
		Name: ArtifactNameForAttempt(ArtifactNameReviews, 1),
		Ref:  ArtifactRef{Path: "tenant/acme/task/t1/artifact/cmV2aWV3cw"},
	}}, 1)
	if !errors.Is(err, ErrJobArtifactDecode) {
		t.Fatalf("error = %v, want ErrJobArtifactDecode", err)
	}
}
