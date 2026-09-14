// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"strconv"
	"testing"
)

// =============================================================================
// The AutoSpawn quota bypass (go-quicbaas#898)
// =============================================================================
//
// Quotas.MaxConcurrency was enforced only inside the two sweep paths — the
// issue poller's adoptionCapacity and the dispatcher's slot arithmetic — so a
// manually triggered CreateTask(AutoSpawn: true) walked straight past it. A
// project capped at two could be handed twenty jobs by a caller in a loop, and
// the sweeps had nothing to reclaim afterwards: the jobs were already leased
// and running.

const quotaTenant = "tenant-1"

// newQuotaService builds a service whose project is capped at limit.
func newQuotaService(t *testing.T, limit int) (*agentServiceImpl, *mockTaskStore) {
	t.Helper()
	tasks := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetNode("node-a")
	impl.SetTaskStore(tasks)

	projects := NewInMemoryProjectStore()
	if err := projects.Create(context.Background(), &Project{
		ID: testProjectID, TenantID: quotaTenant, Name: "capped",
		Quotas: ProjectQuotas{MaxConcurrency: limit},
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	impl.SetProjectStore(projects)
	return impl, tasks
}

// holdSlots takes n live job leases against the capped project, which is what
// n jobs already running looks like to the quota check.
func holdSlots(t *testing.T, svc *agentServiceImpl, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		ref := "held-" + strconv.Itoa(i)
		lease := &JobLease{
			TenantID: quotaTenant, ProjectID: testProjectID, Ref: ref,
			TaskID: "task-" + ref, JobID: "job-" + ref, Generation: 1, Node: "node-a",
		}
		if err := svc.jobLeases.Acquire(ctx, lease); err != nil {
			t.Fatalf("acquire lease %d: %v", i, err)
		}
	}
}

// quotaTask is a task scoped to the capped project.
func quotaTask() *Task {
	return &Task{ID: "task-new", TenantID: quotaTenant, ProjectID: testProjectID, AutoSpawn: true}
}

// TestCheckProjectConcurrency_RefusesOverTheQuota covers the check at, below and
// over the cap. The boundary is the point: a project capped at two admits a
// third job only once one of the two is gone.
func TestCheckProjectConcurrency_RefusesOverTheQuota(t *testing.T) {
	cases := map[string]struct {
		limit, held int
		wantRefusal bool
	}{
		"an idle project admits":                 {limit: 2, held: 0, wantRefusal: false},
		"a project with one slot left admits":    {limit: 2, held: 1, wantRefusal: false},
		"a full project refuses":                 {limit: 2, held: 2, wantRefusal: true},
		"an oversubscribed project refuses":      {limit: 2, held: 5, wantRefusal: true},
		"a project capped at one refuses at one": {limit: 1, held: 1, wantRefusal: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc, _ := newQuotaService(t, tc.limit)
			holdSlots(t, svc, tc.held)

			err := svc.checkProjectConcurrency(context.Background(), quotaTask())
			var exceeded *ProjectCapacityExceededError
			if !tc.wantRefusal {
				if err != nil {
					t.Fatalf("err = %v, want the dispatch admitted", err)
				}
				return
			}
			if !errors.As(err, &exceeded) {
				t.Fatalf("err = %v, want a *ProjectCapacityExceededError", err)
			}
			if exceeded.Limit != tc.limit || exceeded.Live != tc.held {
				t.Errorf("refusal reports live=%d limit=%d, want live=%d limit=%d",
					exceeded.Live, exceeded.Limit, tc.held, tc.limit)
			}
			if exceeded.RetryAt.IsZero() {
				t.Error("a capacity refusal must name when to come back")
			}
			if !errors.Is(err, ErrProjectCapacityExceeded) {
				t.Error("the refusal does not carry the capacity sentinel, so no fault class is derived from it")
			}
		})
	}
}

// TestCheckProjectConcurrency_AdmitsWhereNoQuotaCanBeDecided is the bad-input
// half, and every case is a deployment in which no quota was ever configured.
// Refusing a dispatch because a limit could not be READ would stop a node that
// has always worked.
func TestCheckProjectConcurrency_AdmitsWhereNoQuotaCanBeDecided(t *testing.T) {
	ctx := context.Background()

	t.Run("a nil task", func(t *testing.T) {
		svc, _ := newQuotaService(t, 1)
		holdSlots(t, svc, 1)
		if err := svc.checkProjectConcurrency(ctx, nil); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a task scoped to no project", func(t *testing.T) {
		svc, _ := newQuotaService(t, 1)
		holdSlots(t, svc, 1)
		task := quotaTask()
		task.ProjectID = ""
		if err := svc.checkProjectConcurrency(ctx, task); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a node with no project store", func(t *testing.T) {
		svc, _ := newQuotaService(t, 1)
		holdSlots(t, svc, 1)
		svc.SetProjectStore(nil)
		if err := svc.checkProjectConcurrency(ctx, quotaTask()); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a node with no lease store", func(t *testing.T) {
		svc, _ := newQuotaService(t, 1)
		svc.jobLeases = nil
		if err := svc.checkProjectConcurrency(ctx, quotaTask()); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a project the store does not hold", func(t *testing.T) {
		svc, _ := newQuotaService(t, 1)
		holdSlots(t, svc, 1)
		task := quotaTask()
		task.ProjectID = "a-project-nobody-created"
		if err := svc.checkProjectConcurrency(ctx, task); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a project with no cap", func(t *testing.T) {
		svc, _ := newQuotaService(t, 0)
		holdSlots(t, svc, 20)
		if err := svc.checkProjectConcurrency(ctx, quotaTask()); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

// failingProjects is a project store that cannot answer, as opposed to one that
// answers "no such project".
type failingProjects struct {
	ProjectStore
	err error
}

func (f *failingProjects) Get(context.Context, string, string) (*Project, error) {
	return nil, f.err
}

// TestCheckProjectConcurrency_SurfacesAStoreThatCannotAnswer is the other half
// of the bad-input case and the opposite answer: a store that ERRORS is not a
// deployment without quotas, and dispatching on a guess about a limit is how
// the fleet was oversubscribed in the first place.
func TestCheckProjectConcurrency_SurfacesAStoreThatCannotAnswer(t *testing.T) {
	svc, _ := newQuotaService(t, 2)
	cause := errors.New("the project store is unreachable")
	svc.SetProjectStore(&failingProjects{ProjectStore: NewInMemoryProjectStore(), err: cause})

	if err := svc.checkProjectConcurrency(context.Background(), quotaTask()); !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the store's own failure", err)
	}
}

// TestCreateTask_AutoSpawnHonoursTheProjectQuota is the bug closed, end to end
// through the path that bypassed it.
func TestCreateTask_AutoSpawnHonoursTheProjectQuota(t *testing.T) {
	svc, tasks := newQuotaService(t, 1)
	holdSlots(t, svc, 1)

	task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  quotaTenant,
		ProjectID: testProjectID,
		Subject:   "one more job than the project allows",
		AutoSpawn: true,
	})
	var exceeded *ProjectCapacityExceededError
	if !errors.As(err, &exceeded) {
		t.Fatalf("err = %v, want the dispatch refused for capacity", err)
	}

	// THE TASK IS STILL CREATED AND STILL PENDING. A refusal is not a
	// rejection: the dispatcher's own sweep starts it as soon as a slot frees,
	// so nothing is lost and the caller has nothing to retry.
	if task == nil {
		t.Fatal("the task was not returned; a refused dispatch must not lose the work")
	}
	if task.SpawnedJobID != "" {
		t.Errorf("a refused dispatch named a job: %q", task.SpawnedJobID)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("status = %q, want pending", task.Status)
	}

	// AND NO ATTEMPT WAS CHARGED, which is why the check runs before the claim:
	// a refusal that had already spent an attempt would exhaust the task's
	// budget on jobs that never ran.
	tasks.mu.Lock()
	created := len(tasks.createCalls)
	tasks.mu.Unlock()
	if created != 1 {
		t.Fatalf("store.CreateTask called %d times, want 1", created)
	}
	if task.Attempts != 0 {
		t.Errorf("attempts = %d, want 0: a task refused for capacity was never attempted", task.Attempts)
	}
}

// TestCreateTask_AutoSpawnAdmitsWithinTheProjectQuota is the regression guard on
// the other side: the quota must not refuse a dispatch the project has room
// for. The spawn still fails here for want of an agent, which is the pre-existing
// behaviour and proves the quota let it through.
func TestCreateTask_AutoSpawnAdmitsWithinTheProjectQuota(t *testing.T) {
	svc, _ := newQuotaService(t, 2)
	holdSlots(t, svc, 1)

	_, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  quotaTenant,
		ProjectID: testProjectID,
		Subject:   "within the quota",
		AutoSpawn: true,
	})
	var exceeded *ProjectCapacityExceededError
	if errors.As(err, &exceeded) {
		t.Fatalf("a dispatch inside the quota was refused for capacity: %v", err)
	}
	if !errors.Is(err, ErrNoMatchingAgent) {
		t.Fatalf("err = %v, want the dispatch to have reached agent matching", err)
	}
}
