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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A blocked task publishes nothing (go-quicbaas#1054)
// =============================================================================

// blockTask replaces the rig's task with a blocked copy, which is what the
// engine's block verb leaves in the store.
func blockTask(tasks *fakeTasks, kind orchestration.TaskBlockKind) {
	blocked := *testTaskRecord(25)
	blocked.Description = issueContract
	blocked.Status = orchestration.TaskStatusBlocked
	blocked.BlockKind = kind
	blocked.BlockedReason = "an open pull request already closes this issue"
	tasks.replace(&blocked)
}

// TestRunner_ATaskBlockedMidTurnIsNeitherPushedNorOpened is the incident: the
// task is blocked while the worker is mid-turn. The run must stop, publish
// nothing (not the delivery push, not the teardown's recovery push), open
// no pull request, and still close the worker's container.
func TestRunner_ATaskBlockedMidTurnIsNeitherPushedNorOpened(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		blockTask(rig.tasks, orchestration.TaskBlockKindBlocked)
	}

	err := rig.runner.Run(context.Background(), rig.job(t))

	var withdrawn *AttemptWithdrawnError
	if !errors.As(err, &withdrawn) || withdrawn.Status != orchestration.TaskStatusBlocked {
		t.Fatalf("Run = %v, want the attempt withdrawn by the block", err)
	}
	rig.container.mu.Lock()
	pushes := append([]pushedRef(nil), rig.container.pushes...)
	rig.container.mu.Unlock()
	if len(pushes) != 0 {
		t.Fatalf("a blocked task's work was pushed: %+v", pushes)
	}
	if len(rig.forge.prs) != 0 {
		t.Fatalf("a blocked task opened a pull request: %v", rig.forge.prs)
	}
	if len(rig.agents.closed) != 1 {
		t.Fatalf("closed = %v, want the worker's session torn down", rig.agents.closed)
	}
}

// readmitTask replaces the rig's task with what a block followed by a
// readmission leaves in the store: pending again, with the old attempt's
// binding moved into the history.
func readmitTask(tasks *fakeTasks) {
	readmitted := *testTaskRecord(25)
	readmitted.Description = issueContract
	readmitted.Status = orchestration.TaskStatusPending
	readmitted.SpawnedJobID = ""
	tasks.replace(&readmitted)
}

// TestRunner_ATaskBlockedAndReadmittedMidTurnIsNeitherPushedNorOpened: the
// block and the readmission both land while the worker is mid-turn, so every
// check after the turn sees a pending task. Pending is a status a live attempt
// holds, but the task no longer names this job, so the run must stop exactly
// as it does for a block: nothing pushed, no pull request, container closed.
func TestRunner_ATaskBlockedAndReadmittedMidTurnIsNeitherPushedNorOpened(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		blockTask(rig.tasks, orchestration.TaskBlockKindBlocked)
		readmitTask(rig.tasks)
	}

	err := rig.runner.Run(context.Background(), rig.job(t))

	var withdrawn *AttemptWithdrawnError
	if !errors.As(err, &withdrawn) || withdrawn.Status != orchestration.TaskStatusPending || withdrawn.OpenJobID != "" {
		t.Fatalf("Run = %v, want the attempt withdrawn by the readmission", err)
	}
	rig.container.mu.Lock()
	pushes := append([]pushedRef(nil), rig.container.pushes...)
	rig.container.mu.Unlock()
	if len(pushes) != 0 {
		t.Fatalf("a withdrawn attempt's work was pushed after readmission: %+v", pushes)
	}
	if len(rig.forge.prs) != 0 {
		t.Fatalf("a withdrawn attempt opened a pull request after readmission: %v", rig.forge.prs)
	}
	if len(rig.agents.closed) != 1 {
		t.Fatalf("closed = %v, want the worker's session torn down", rig.agents.closed)
	}
}

// TestHooks_ReviewableOpensNoPullRequestAfterAReadmission: the task was
// blocked and readmitted after the push. The readmitted task no longer names
// this job, so the pull request is not opened for it.
func TestHooks_ReviewableOpensNoPullRequestAfterAReadmission(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, "head-1")
	readmitTask(rig.tasks)

	err := rig.hooks.Reviewable(ctx, job)
	if !errors.Is(err, ErrAttemptWithdrawn) {
		t.Fatalf("Reviewable = %v, want ErrAttemptWithdrawn", err)
	}
	if len(rig.forge.prs) != 0 {
		t.Fatalf("a pull request was opened for a withdrawn attempt: %v", rig.forge.prs)
	}
}

// TestRunner_ATaskAlreadyBlockedTakesNoState is the resume half: a job that
// starts or resumes for a task already blocked, from any path, is refused
// before its first state and never spawns a worker.
func TestRunner_ATaskAlreadyBlockedTakesNoState(t *testing.T) {
	rig := newRunnerRig(t)
	blockTask(rig.tasks, orchestration.TaskBlockKindEscalated)

	err := rig.runner.Run(context.Background(), rig.job(t))
	if !errors.Is(err, ErrAttemptWithdrawn) {
		t.Fatalf("Run = %v, want ErrAttemptWithdrawn", err)
	}
	if len(rig.agents.spawns) != 0 {
		t.Fatalf("a blocked task spawned a worker: %d", len(rig.agents.spawns))
	}
}

// TestHooks_ReviewableOpensNoPullRequestForABlockedTask: the block landed
// after the push and before the pull request. The pull request is the write
// that stops, because it is the one that costs a review.
func TestHooks_ReviewableOpensNoPullRequestForABlockedTask(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, "head-1")
	blockTask(rig.tasks, orchestration.TaskBlockKindBlocked)

	err := rig.hooks.Reviewable(ctx, job)
	if !errors.Is(err, ErrAttemptWithdrawn) {
		t.Fatalf("Reviewable = %v, want ErrAttemptWithdrawn", err)
	}
	if len(rig.forge.prs) != 0 {
		t.Fatalf("a pull request was opened for a blocked task: %v", rig.forge.prs)
	}
}

// TestPublisher_PushRefusesWhenTheTaskIsNoLongerTheAttempts: the push is the
// last moment a block can stop the publication, so no git push runs.
func TestPublisher_PushRefusesWhenTheTaskIsNoLongerTheAttempts(t *testing.T) {
	container := &fakeContainer{}
	pub := newTestPublisher(container, nil)
	pub.admit = func() error {
		return &AttemptWithdrawnError{TaskID: testTask, Status: orchestration.TaskStatusBlocked}
	}
	if _, err := pub.Push(testPublishPlan()); !errors.Is(err, ErrAttemptWithdrawn) {
		t.Fatalf("Push = %v, want ErrAttemptWithdrawn", err)
	}
	for _, command := range container.commandsRun() {
		if strings.Contains(command, "push") {
			t.Fatalf("a withdrawn attempt ran %q", command)
		}
	}
}

// TestPublisher_ABlockDuringVerificationStopsThePush: signature verification
// runs a command in the container, and a block can land on another node while
// it does. The task is re-read after the verification, so no push follows.
func TestPublisher_ABlockDuringVerificationStopsThePush(t *testing.T) {
	container := &fakeContainer{ahead: true}
	pub := newTestPublisher(container, &SigningSession{PublicKey: testSigningKey})
	blocked := false
	verifying := false
	pub.exec = func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		for _, arg := range spec.Cmd {
			if strings.HasPrefix(arg, "gpg.ssh.allowedSignersFile=") {
				// The block lands while the verification runs.
				verifying, blocked = true, true
			}
		}
		return container.exec("container-1", spec)
	}
	pub.admit = func() error {
		if blocked {
			return &AttemptWithdrawnError{TaskID: testTask, Status: orchestration.TaskStatusBlocked}
		}
		return nil
	}

	if _, err := pub.Push(testPublishPlan()); !errors.Is(err, ErrAttemptWithdrawn) {
		t.Fatalf("Push = %v, want ErrAttemptWithdrawn", err)
	}
	if !verifying {
		t.Fatal("precondition: the signature verification never ran")
	}
	for _, command := range container.commandsRun() {
		if strings.Contains(command, " push") {
			t.Fatalf("a task blocked during verification was pushed: %q", command)
		}
	}
}

// TestPublisher_PushWithNoWayToAskIsRefused: a publisher built without the
// admission check pushes nothing rather than assuming the task is its own.
func TestPublisher_PushWithNoWayToAskIsRefused(t *testing.T) {
	container := &fakeContainer{}
	pub := newTestPublisher(container, nil)
	pub.admit = nil
	if _, err := pub.Push(testPublishPlan()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Push = %v, want ErrInvalidInput", err)
	}
	if commands := container.commandsRun(); len(commands) != 0 {
		t.Fatalf("a publisher with no admission check ran %v", commands)
	}
}

func TestAttemptWithdrawn_OnlyALiveAttemptsStatusesAdmit(t *testing.T) {
	cases := map[orchestration.TaskStatus]bool{
		orchestration.TaskStatusPending:    true,
		orchestration.TaskStatusClaimed:    true,
		orchestration.TaskStatusInProgress: true,
		orchestration.TaskStatusBlocked:    false,
		orchestration.TaskStatusCompleted:  false,
		orchestration.TaskStatusFailed:     false,
		orchestration.TaskStatusCancelled:  false,
	}
	for status, admits := range cases {
		task := testTaskRecord(25)
		task.Status = status
		err := attemptWithdrawn(task, "job-1")
		if admits && err != nil {
			t.Errorf("%s: refused: %v", status, err)
		}
		if !admits && !errors.Is(err, ErrAttemptWithdrawn) {
			t.Errorf("%s: err = %v, want ErrAttemptWithdrawn", status, err)
		}
	}
}

func TestAttemptWithdrawn_OnlyTheJobTheTaskNamesIsAdmitted(t *testing.T) {
	cases := map[string]struct {
		bound  string
		admits bool
	}{
		"the task names this job":         {"job-1", true},
		"readmitted: the task names none": {"", false},
		"re-dispatched to another job":    {"job-2", false},
	}
	for name, tc := range cases {
		task := testTaskRecord(25)
		task.Status = orchestration.TaskStatusPending
		task.SpawnedJobID = tc.bound
		err := attemptWithdrawn(task, "job-1")
		if tc.admits && err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
		if !tc.admits {
			var withdrawn *AttemptWithdrawnError
			if !errors.As(err, &withdrawn) || withdrawn.JobID != "job-1" || withdrawn.OpenJobID != tc.bound {
				t.Errorf("%s: err = %v, want the attempt withdrawn naming both jobs", name, err)
			}
		}
	}
}

func TestHooks_AdmitAttemptReportsAnUnreadableTask(t *testing.T) {
	rig := newHooksRig(t)
	err := rig.hooks.admitAttempt(context.Background(), testTenant, "no-such-task", "job-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("admitAttempt = %v, want the read failure, not an admission", err)
	}
}

func TestAttemptWithdrawnError_NamesTheTaskAndWhy(t *testing.T) {
	err := &AttemptWithdrawnError{TaskID: "task-1", Status: orchestration.TaskStatusBlocked, BlockKind: orchestration.TaskBlockKindNeedsInfo}
	if !strings.Contains(err.Error(), "task task-1 is blocked (needs_info)") {
		t.Fatalf("Error() = %q", err.Error())
	}
	unbound := &AttemptWithdrawnError{TaskID: "task-1", JobID: "job-1", Status: orchestration.TaskStatusPending}
	if !strings.Contains(unbound.Error(), "its open attempt is none, not job job-1") {
		t.Fatalf("Error() = %q, want the lost binding named", unbound.Error())
	}
	plain := &AttemptWithdrawnError{TaskID: "task-1", Status: orchestration.TaskStatusCancelled}
	if strings.Contains(plain.Error(), "(") {
		t.Fatalf("a withdrawal with no block kind names one: %q", plain.Error())
	}
}
