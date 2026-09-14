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

package orchestration

import (
	"strconv"
	"sync"
	"testing"
)

// publicationRounds is how many snapshots and fan-out republications the
// publication tests race against each other. It is large enough that the
// two goroutines overlap on every run and small enough to stay in memory.
const publicationRounds = 300

// TestJobRun_PersistHookCannotMutateThePublishedStatus proves the invariant
// republishFanout relies on: a status the run has published is immutable.
// The persist hook the envelope installs writes the tenant onto the status
// it is handed, and it runs after the status is published, so a hook that
// received the published pointer would be writing a field another goroutine
// copies out of it. Run under -race this fails with a DATA RACE report when
// the hook is handed the published pointer.
func TestJobRun_PersistHookCannotMutateThePublishedStatus(t *testing.T) {
	t.Parallel()
	run := newEnvelopeRun(t, JobRunConfig{
		// Mirrors launchJob's hook, which scopes every persisted record to
		// the tenant the job runs for.
		PersistFn: func(status *JobStatus) { status.TenantID = "acme" },
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < publicationRounds; i++ {
			run.snapshot(JobStateExecuting)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < publicationRounds; i++ {
			run.AddChildJob("child-" + strconv.Itoa(i))
		}
	}()
	wg.Wait()

	status := run.Status()
	if status == nil {
		t.Fatal("Status() = nil after publication; every snapshot publishes")
	}
	if status.TenantID != "acme" {
		t.Fatalf("published status tenant = %q, want %q: the published status must carry the tenant without the hook writing it", status.TenantID, "acme")
	}
	if len(status.ChildJobIDs) != publicationRounds {
		t.Fatalf("published status children = %d, want %d: no fan-out republication may be lost", len(status.ChildJobIDs), publicationRounds)
	}
}

// TestJobRun_PersistHookMutationDoesNotReachThePublishedStatus proves the
// hook is handed a value of its own: a hook that overwrites a field cannot
// change what a reader of the published status sees.
func TestJobRun_PersistHookMutationDoesNotReachThePublishedStatus(t *testing.T) {
	t.Parallel()
	var persisted *JobStatus
	run := newEnvelopeRun(t, JobRunConfig{
		PersistFn: func(status *JobStatus) {
			status.Error = "written by the hook"
			persisted = status
		},
	})

	published := run.snapshot(JobStateExecuting)
	if persisted == nil {
		t.Fatal("the persist hook must be called with the snapshot")
	}
	if published.Error != "" {
		t.Fatalf("published status error = %q, want empty: the hook must not reach the published status", published.Error)
	}
	if persisted == published {
		t.Fatal("the persist hook must not be handed the published pointer")
	}
	if persisted.JobID != published.JobID || persisted.State != published.State {
		t.Fatalf("persisted status (%s/%s) must otherwise equal the published one (%s/%s)",
			persisted.JobID, persisted.State, published.JobID, published.State)
	}
}
