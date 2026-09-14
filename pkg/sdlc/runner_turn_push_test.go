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
	"path"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The post-turn publication — the branch is what survives the container
// =============================================================================
//
// On the ephemeral storage tier the worker's checkout is a tmpfs inside its
// container: a restart takes every commit that was never pushed, and the
// attempt resumes from a head that is missing work a person can see was done.
// These tests hold the runner's half of the seam the engine triggers.

// ephemeralWorkspaceTier is the project storage an operator configures to move a
// project's checkouts off the disk.
func ephemeralWorkspaceTier() orchestration.ProjectStorage {
	return orchestration.ProjectStorage{
		WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  2048,
		ContainerMemoryMB: 4096,
	}
}

// forcedPushes is every publication that carried --force-with-lease, which is
// the shape a MID-ATTEMPT publication has and a final one does not.
func (r *runnerRig) forcedPushes() []pushedRef {
	r.container.mu.Lock()
	defer r.container.mu.Unlock()
	var forced []pushedRef
	for _, push := range r.container.pushes {
		if push.Force {
			forced = append(forced, push)
		}
	}
	return forced
}

// TestRunner_AnEphemeralWorkspacePublishesTheTurnThatTouchedTheTree is the
// property the whole tier depends on: the work reaches the forge while the
// container is still alive, not only at teardown.
func TestRunner_AnEphemeralWorkspacePublishesTheTurnThatTouchedTheTree(t *testing.T) {
	rig := newRunnerRig(t)
	rig.storage = ephemeralWorkspaceTier()
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	forced := rig.forcedPushes()
	if len(forced) == 0 {
		t.Fatal("the turn's work was never published; an ephemeral checkout loses it with the container")
	}
	for _, push := range forced {
		if push.Branch == "" {
			t.Errorf("a publication named no branch: %+v", push)
		}
		// A mid-attempt publication must never land on the carve-out's
		// target: the work has not been verified and has not been reviewed.
		if push.RemoteBranch != "" {
			t.Errorf("the turn's work was published to %q, want the attempt's own branch", push.RemoteBranch)
		}
	}
}

// TestRunner_APersistentWorkspacePublishesNothingMidTurn is the inertness half.
// A checkout that is still on the host after the container goes needs no
// per-turn forge round-trip, and every project in the fleet today is this one.
func TestRunner_APersistentWorkspacePublishesNothingMidTurn(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if forced := rig.forcedPushes(); len(forced) != 0 {
		t.Fatalf("a persistent workspace published mid-turn: %+v", forced)
	}
}

// TestRunner_APushFailureUnderAnEphemeralWorkspaceFailsTheJob is the one the
// verification bar names specifically. A publication that could not happen is,
// on this tier, work that will not exist after the next restart — so it must
// surface as a job error rather than be logged past, which would let the
// attempt spend its whole budget building on a head nobody can fetch.
func TestRunner_APushFailureUnderAnEphemeralWorkspaceFailsTheJob(t *testing.T) {
	rig := newRunnerRig(t)
	rig.storage = ephemeralWorkspaceTier()
	rig.forge.putIssue(25, "Fix the thing", "task:ready")
	rig.workerWrote()
	rig.container.pushErr = errors.New("remote rejected: stale info")

	err := rig.runner.Run(context.Background(), rig.job(t))
	if err == nil {
		t.Fatal("Run succeeded although the turn's work could not be published")
	}
	var pushErr *orchestration.TurnPushError
	if !errors.As(err, &pushErr) {
		t.Fatalf("Run() = %T %v, want a TurnPushError", err, err)
	}
	if pushErr.JobID != "job-1" {
		t.Errorf("JobID = %q, want the job whose work is at risk", pushErr.JobID)
	}

	// And the job's own audit trail names it, so the failure is visible where a
	// person looks rather than only in the daemon's log.
	if details := rig.auditDetails("turn:push_failed"); len(details) == 0 {
		t.Fatal("the failed publication is not on the job's audit trail")
	}
}

// TestRunnerRun_PushTurn_ARunOffTheEphemeralTierPublishesNothing covers the gate
// itself, including the project record nothing knows the tier of: a run that
// cannot tell where its work lives must not assume it is about to vanish.
func TestRunnerRun_PushTurn_ARunOffTheEphemeralTierPublishesNothing(t *testing.T) {
	cases := map[string]*orchestration.JobRun{
		"NoProjectRecord": orchestration.NewJobRun(orchestration.JobRunConfig{JobID: "job-1"}),
		"UnsetTier": orchestration.NewJobRun(orchestration.JobRunConfig{
			JobID:   "job-1",
			Project: &orchestration.Project{ID: testProject},
		}),
		"PersistentTier": orchestration.NewJobRun(orchestration.JobRunConfig{
			JobID: "job-1",
			Project: &orchestration.Project{
				ID:      testProject,
				Storage: orchestration.ProjectStorage{WorkspaceTier: orchestration.WorkspaceTierPersistent},
			},
		}),
	}
	for name, job := range cases {
		t.Run(name, func(t *testing.T) {
			// No delivery, no session and no checkout: on the ephemeral tier
			// this would be refused, so a nil return proves the gate answered
			// before anything else was consulted.
			run := &runnerRun{job: job}
			if run.ephemeralWorkspace() {
				t.Fatal("ephemeralWorkspace() = true off the ephemeral tier")
			}
			if err := run.PushTurn(context.Background()); err != nil {
				t.Fatalf("PushTurn: %v", err)
			}
		})
	}
}

// TestRunnerRun_PushTurn_RefusesARunWithNothingToPublishFrom is the bad-input
// twin, and a refusal rather than a shrug on purpose: on this tier a skipped
// publication is silent data loss, so the state that cannot happen must be
// loud if it ever does.
func TestRunnerRun_PushTurn_RefusesARunWithNothingToPublishFrom(t *testing.T) {
	job := orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:   "job-1",
		Config:  &orchestration.JobConfig{TenantID: testTenant, TaskID: testTask, Attempt: 1},
		Project: &orchestration.Project{ID: testProject, Storage: ephemeralWorkspaceTier()},
	})
	run := &runnerRun{job: job}

	err := run.PushTurn(context.Background())
	var session *WorkerSessionError
	if !errors.As(err, &session) {
		t.Fatalf("PushTurn() = %T %v, want a WorkerSessionError", err, err)
	}
	if session.TaskID != testTask {
		t.Errorf("TaskID = %q, want %q", session.TaskID, testTask)
	}
}

// TestRunnerRun_PushTurn_NothingToPublishIsASuccess is the tolerance the seam
// needs. A turn that touched the tree and then put it back — an edit reverted,
// a file formatted to what it already was — leaves the branch exactly where it
// was, and failing the job over that would be refusing a turn that behaved
// correctly.
func TestRunnerRun_PushTurn_NothingToPublishIsASuccess(t *testing.T) {
	checkout := path.Join(orchestration.WorkspaceCheckoutRoot, testOrchProject().Name)
	container := &fakeContainer{checkout: checkout, changed: false, ahead: false, head: "head-prior"}
	agents := &fakeAgents{container: container}
	project := testOrchProject()
	project.Storage = ephemeralWorkspaceTier()
	job := orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:   "job-1",
		Config:  &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject, TaskID: testTask, Attempt: 1},
		Project: project,
		Agents:  agents,
	})
	run := &runnerRun{
		job:          job,
		sessionID:    "session-1",
		containerDir: checkout,
		delivery:     &Delivery{Branch: "issue/25-fix-the-thing", Base: "main"},
		attempt:      &attempt{task: testTaskRecord(25), issue: 25},
	}

	if err := run.PushTurn(context.Background()); err != nil {
		t.Fatalf("PushTurn on a turn with nothing to publish: %v", err)
	}
	if len(container.pushes) != 0 {
		t.Fatalf("an empty publication was pushed: %+v", container.pushes)
	}
}
