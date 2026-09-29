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
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// jobReadKV counts the prefix scans made through it and can fail the
// point read of one key, so a test can tell a listing answered by key from
// one answered by scan, and see what a failed read does to the answer.
type jobReadKV struct {
	interfaces.KVStore
	scans   atomic.Int64
	failKey string
}

var errPointReadFailed = errors.New("point read failed")

func (k *jobReadKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	k.scans.Add(1)
	return k.KVStore.Scan(ctx, prefix, fn)
}

func (k *jobReadKV) Get(ctx context.Context, key string) ([]byte, error) {
	if key == k.failKey {
		return nil, errPointReadFailed
	}
	return k.KVStore.Get(ctx, key)
}

// pointReadService is a service whose job records live in kv, with one
// persisted job per id in ids.
func pointReadService(t *testing.T, kv *jobReadKV, tenantID string, ids ...string) *agentServiceImpl {
	t.Helper()
	svc := newTestService(t)
	svc.SetKVStore(kv)
	for i, id := range ids {
		raw, err := json.Marshal(&JobStatus{
			JobID:     id,
			TenantID:  tenantID,
			State:     JobStateCompleted,
			AgentID:   "agent-" + id,
			StartedAt: int64(i + 1),
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := kv.KVStore.Put(context.Background(), jobPersistenceKey(tenantID, id), raw); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	return svc
}

func TestListJobs_JobIDsAreReadByKeyNotScanned(t *testing.T) {
	t.Parallel()
	const tenantID = "acme"
	kv := &jobReadKV{KVStore: kvstore.NewMemory()}
	svc := pointReadService(t, kv, tenantID, "job-a", "job-b", "job-c")

	// A running job is answered from memory and must not be read again, and a
	// duplicate, an empty and an unknown id must neither fail nor repeat.
	live := newJobRun(JobRunConfig{
		JobID:  "job-live",
		Runner: JobRunnerWorkflow,
		Config: &JobConfig{TenantID: tenantID},
		Logger: &controlLoopMockLogger{},
	})
	live.snapshot(JobStateExecuting)
	svc.jobs.Store(tenantID+"/job-live", &runningJob{run: live, cancel: func() {}})

	ctx := ContextWithTenant(context.Background(), tenantID)
	jobs, err := svc.ListJobs(ctx, tenantID, &JobFilter{
		JobIDs: []string{"job-c", "job-a", "job-a", "", "job-unknown", "job-live"},
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}

	if got := kv.scans.Load(); got != 0 {
		t.Fatalf("a listing that names its jobs scanned the store %d time(s)", got)
	}
	got := make(map[string]string, len(jobs))
	for _, job := range jobs {
		if _, repeated := got[job.JobID]; repeated {
			t.Fatalf("job %s listed twice", job.JobID)
		}
		got[job.JobID] = job.AgentID
	}
	want := map[string]string{"job-a": "agent-job-a", "job-c": "agent-job-c", "job-live": ""}
	if len(got) != len(want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
	for id, agent := range want {
		if got[id] != agent {
			t.Fatalf("job %s: agent %q, want %q (listing %v)", id, got[id], agent, got)
		}
	}
}

func TestListJobs_JobIDsLeaveOutWhatCannotBeRead(t *testing.T) {
	t.Parallel()
	const tenantID = "acme"
	kv := &jobReadKV{KVStore: kvstore.NewMemory(), failKey: jobPersistenceKey(tenantID, "job-broken")}
	svc := pointReadService(t, kv, tenantID, "job-ok", "job-broken")
	if err := kv.KVStore.Put(context.Background(), jobPersistenceKey(tenantID, "job-garbled"), []byte("{not json")); err != nil {
		t.Fatalf("put: %v", err)
	}

	ctx := ContextWithTenant(context.Background(), tenantID)
	jobs, err := svc.ListJobs(ctx, tenantID, &JobFilter{JobIDs: []string{"job-broken", "job-garbled", "job-ok"}})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].JobID != "job-ok" {
		t.Fatalf("listed %+v, want only job-ok: an unreadable or garbled record is left out, not fatal", jobs)
	}
}

func TestListJobs_JobIDsStillApplyTheRestOfTheFilter(t *testing.T) {
	t.Parallel()
	const tenantID = "acme"
	kv := &jobReadKV{KVStore: kvstore.NewMemory()}
	svc := pointReadService(t, kv, tenantID, "job-a", "job-b")

	ctx := ContextWithTenant(context.Background(), tenantID)
	jobs, err := svc.ListJobs(ctx, tenantID, &JobFilter{
		JobIDs:   []string{"job-a", "job-b"},
		Statuses: []JobState{JobStateFailed},
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("listed %d completed job(s) for a failed-only filter", len(jobs))
	}
}

func TestListJobs_NoJobIDsStillScans(t *testing.T) {
	t.Parallel()
	const tenantID = "acme"
	kv := &jobReadKV{KVStore: kvstore.NewMemory()}
	svc := pointReadService(t, kv, tenantID, "job-a", "job-b")

	ctx := ContextWithTenant(context.Background(), tenantID)
	jobs, err := svc.ListJobs(ctx, tenantID, nil)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 2 || kv.scans.Load() != 1 {
		t.Fatalf("listed %d job(s) in %d scan(s), want 2 in 1", len(jobs), kv.scans.Load())
	}
	if jobs[0].JobID != "job-b" {
		t.Fatalf("first job %s, want the most recently started (job-b)", jobs[0].JobID)
	}
}

func TestListTasksPage_ResolvesAgentsWithoutScanningJobs(t *testing.T) {
	t.Parallel()
	f := newListFixture(t, listVolume{jobs: 120, tasks: 6})
	counting := &jobReadKV{KVStore: f.svc.kvStore}
	f.svc.SetKVStore(counting)

	page, err := f.svc.ListTasksPage(f.ctx, benchTenant, &TaskFilter{}, &Page{Limit: 5})
	if err != nil {
		t.Fatalf("ListTasksPage: %v", err)
	}
	if got := counting.scans.Load(); got != 0 {
		t.Fatalf("the tasks page scanned the job records %d time(s)", got)
	}
	for _, task := range page.Items {
		if task.AgentID == "" {
			t.Fatalf("task %s: no agent resolved from its latest attempt", task.ID)
		}
	}
}

func TestListSessions_ResolvesRolesWithoutScanningJobs(t *testing.T) {
	t.Parallel()
	f := newListFixture(t, listVolume{jobs: 120, sessions: 10})
	counting := &jobReadKV{KVStore: f.svc.kvStore}
	f.svc.SetKVStore(counting)

	resp, err := f.svc.ListSessions(f.ctx, &ListSessionsRequest{Runner: "review", Page: &Page{Limit: 5}})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := counting.scans.Load(); got != 0 {
		t.Fatalf("the sessions page scanned the job records %d time(s)", got)
	}
	if resp.Total != 10 || len(resp.Sessions) != 5 {
		t.Fatalf("total %d, page %d; want every session's role resolved (10) and a page of 5", resp.Total, len(resp.Sessions))
	}
	for _, session := range resp.Sessions {
		if session.Runner != "review" || session.AgentID == "" {
			t.Fatalf("session %s: runner %q agent %q, want both read off its job", session.SessionID, session.Runner, session.AgentID)
		}
	}
}
