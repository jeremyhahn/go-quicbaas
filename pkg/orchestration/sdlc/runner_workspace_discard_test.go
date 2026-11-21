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
	"slices"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// discardingAgents is the local service with the workspace discard the agent
// service provides, recording what each discard was asked to remove.
type discardingAgents struct {
	*fakeAgents

	mu        sync.Mutex
	discarded []string
	jobs      []string
}

func (d *discardingAgents) DiscardWorkspace(_ context.Context, ws *orchestration.SessionWorkspace) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.discarded = append(d.discarded, ws.Key)
	return nil
}

func (d *discardingAgents) DiscardJobWorkspaces(_ context.Context, _ string, jobIDs []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.jobs = append(d.jobs, jobIDs...)
	return &orchestration.WorkspaceRemovalError{JobID: "job-0", Cause: errors.New("the node is gone")}
}

// discardingRig is a runner rig whose task concluded attempts job-0 and job-1
// before this run, job-1 being the attempt the run continues.
func discardingRig(t *testing.T) (*runnerRig, *discardingAgents) {
	t.Helper()
	rig := newRunnerRig(t)
	task := testTaskRecord(25)
	task.Description = issueContract
	task.Metadata = map[string]string{"attempt_history": "job-0,job-1"}
	rig.tasks.replace(task)
	agents := &discardingAgents{fakeAgents: rig.agents}
	rig.agentService = agents
	return rig, agents
}

// TestRunner_ADeliveredAttemptDiscardsEveryWorkspace: once the work is
// delivered nothing runs in any of the task's checkouts again, so this
// attempt's workspace goes with the earlier ones — and an earlier one that
// could not be removed does not fail the delivery.
func TestRunner_ADeliveredAttemptDiscardsEveryWorkspace(t *testing.T) {
	rig, agents := discardingRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pin := job.WorkspacePin()
	if pin == nil {
		t.Fatal("the run provisioned no workspace")
	}
	if !slices.Equal(agents.discarded, []string{pin.Key}) {
		t.Fatalf("discarded %v, want this attempt's %q", agents.discarded, pin.Key)
	}
	if !slices.Equal(agents.jobs, []string{"job-0"}) {
		t.Fatalf("earlier attempts discarded %v, want [job-0]", agents.jobs)
	}
}

// TestRunner_AParkedAttemptKeepsItsWorkspace: an escalation waits on a person,
// who may need the checkout, so only the earlier attempts' workspaces go.
func TestRunner_AParkedAttemptKeepsItsWorkspace(t *testing.T) {
	rig, agents := discardingRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"false"}
		b.MaxReviewRounds = 1
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()

	err := rig.runner.Run(context.Background(), rig.job(t, "false"))
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("err = %v, want an escalation", err)
	}
	if len(agents.discarded) != 0 {
		t.Fatalf("the parked attempt's workspace was discarded: %v", agents.discarded)
	}
	if !slices.Equal(agents.jobs, []string{"job-0"}) {
		t.Fatalf("earlier attempts discarded %v, want [job-0]", agents.jobs)
	}
}
