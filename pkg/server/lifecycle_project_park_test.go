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

package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A parked project's nudge: intake refused, queue still drained
// =============================================================================

const (
	parkNudgeTenant  = "tenant-park-nudge"
	parkNudgeProject = "2f6f3c1a-8d4e-4b6a-9c1d-3e5f7a9b0c11"
)

type parkNudgeTenants struct{}

func (parkNudgeTenants) ListTenantIDs(context.Context) ([]string, error) {
	return []string{parkNudgeTenant}, nil
}

// parkNudgeCreator refuses to be asked: a parked project adopts nothing.
type parkNudgeCreator struct{ calls int }

func (c *parkNudgeCreator) CreateTask(context.Context, *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	c.calls++
	return nil, errors.New("a parked project must adopt nothing")
}

// parkNudgeSpawner records what the dispatcher started.
type parkNudgeSpawner struct {
	mu      sync.Mutex
	spawned []string
}

func (s *parkNudgeSpawner) SpawnTask(_ context.Context, task *orchestration.Task) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawned = append(s.spawned, task.ID)
	return "job-" + task.ID, nil
}

func TestNudgeProject_AParkedProjectDrainsItsQueueAndRefusesIntake(t *testing.T) {
	ctx := orchestration.ContextWithTenant(context.Background(), parkNudgeTenant)
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	logs := logger.NewAdapter(log)

	projects := orchestration.NewInMemoryProjectStore()
	project := &orchestration.Project{ID: parkNudgeProject, TenantID: parkNudgeTenant, Name: "parked", Autonomous: true}
	project.Park("ops", time.Now())
	if err := projects.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	tasks := orchestration.NewInMemoryTaskStore()
	for _, task := range []*orchestration.Task{
		{ID: "task-new", TenantID: parkNudgeTenant, ProjectID: parkNudgeProject, Subject: "new",
			Status: orchestration.TaskStatusPending, AutoSpawn: true},
		{ID: "task-started", TenantID: parkNudgeTenant, ProjectID: parkNudgeProject, Subject: "in flight",
			Status: orchestration.TaskStatusPending, AutoSpawn: true, Attempts: 1},
	} {
		if err := tasks.CreateTask(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}
	leases, err := orchestration.NewProcessJobLeaseStore(time.Minute)
	if err != nil {
		t.Fatalf("leases: %v", err)
	}
	sources := orchestration.NewTaskSourceRegistry()
	if err := sources.Register("park-nudge-kind", parkNudgeSource{}); err != nil {
		t.Fatalf("register source: %v", err)
	}
	creator := &parkNudgeCreator{}
	poller, err := orchestration.NewTaskSourcePoller(orchestration.TaskSourcePollerConfig{
		Sources: sources, Tenants: parkNudgeTenants{}, Projects: projects, Tasks: creator,
		Queue: tasks, Leases: leases, LeaseTTL: time.Minute, Logger: logs,
	})
	if err != nil {
		t.Fatalf("poller: %v", err)
	}
	spawner := &parkNudgeSpawner{}
	dispatcher, err := orchestration.NewTaskDispatcher(orchestration.TaskDispatcherConfig{
		Tenants: parkNudgeTenants{}, Projects: projects, Tasks: tasks,
		Leases: leases, LeaseTTL: time.Minute, Spawner: spawner, Logger: logs,
	})
	if err != nil {
		t.Fatalf("dispatcher: %v", err)
	}
	s := &Server{taskSourcePoller: poller, taskDispatcher: dispatcher}

	err = s.NudgeProject(context.Background(), parkNudgeTenant, parkNudgeProject)
	if !errors.Is(err, orchestration.ErrProjectParked) {
		t.Fatalf("err = %v, want the park refusal reported", err)
	}
	if creator.calls != 0 {
		t.Errorf("a parked project adopted %d tasks", creator.calls)
	}
	spawner.mu.Lock()
	defer spawner.mu.Unlock()
	if len(spawner.spawned) != 1 || spawner.spawned[0] != "task-started" {
		t.Fatalf("dispatched = %v, want only the started task: a parked project still drains", spawner.spawned)
	}
}

// parkNudgeSource offers one new issue, which a parked project must not adopt.
type parkNudgeSource struct{}

func (parkNudgeSource) Name() string { return "park-nudge-source" }

func (parkNudgeSource) Poll(context.Context, *orchestration.Project) ([]orchestration.TaskSourceSpec, error) {
	return []orchestration.TaskSourceSpec{{
		Source:  orchestration.TaskSourceRef{Kind: "park-nudge-kind", Ref: "1"},
		Subject: "a new issue",
	}}, nil
}

// TestSDLCForwarders_DeclareWhichJobsFinishStartedWork: review and merge train
// serve pull requests already open, so a parked project still runs them;
// triage takes in new work, so it does not declare itself.
func TestSDLCForwarders_DeclareWhichJobsFinishStartedWork(t *testing.T) {
	for name, runner := range map[string]orchestration.JobRunner{
		"review":      sdlcReviewRunnerForwarder{},
		"merge train": sdlcTrainForwarder{},
	} {
		finisher, ok := runner.(orchestration.StartedWorkRunner)
		if !ok || !finisher.FinishesStartedWork() {
			t.Errorf("the %s forwarder does not finish started work; a parked project would strand its pull requests", name)
		}
	}
	if _, ok := orchestration.JobRunner(sdlcTriageForwarder{}).(orchestration.StartedWorkRunner); ok {
		t.Error("the triage forwarder declares itself as finishing started work; triage is intake")
	}
	if _, ok := orchestration.JobRunner(sdlcRunnerForwarder{}).(orchestration.StartedWorkRunner); ok {
		t.Error("the delivery forwarder declares itself; its jobs are task-bound and judged by their task")
	}
}
