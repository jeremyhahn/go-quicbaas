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
	"strconv"
	"strings"
	"testing"
)

// The volumes and record sizes below are the burn-in tenant's, measured on
// 2026-09-23: 3,648 persisted jobs (median 875 bytes, a long tail of 30 KB
// records carrying lead memory), 153 tasks averaging 9.6 KB (a 4.7 KB issue
// body and 3.5 KB of output artifacts), and 1,207 durable session records.
const (
	benchTenant   = "bench-tenant"
	benchJobs     = 3648
	benchTasks    = 153
	benchSessions = 1207
)

// listFixture is a service over a memory KV seeded with jobs, tasks and
// sessions shaped like the burn-in tenant's.
type listFixture struct {
	svc *agentServiceImpl
	ctx context.Context
}

// listVolume is how many of each record a listFixture holds.
type listVolume struct{ jobs, tasks, sessions int }

// burnInVolume is the burn-in tenant's volume, for the benchmarks.
var burnInVolume = listVolume{jobs: benchJobs, tasks: benchTasks, sessions: benchSessions}

func newListFixture(tb testing.TB, volume listVolume) *listFixture {
	tb.Helper()
	kv := NewMemoryTaskKV()
	store := NewAgentStore(kv)
	svcIface, err := NewAgentService(store, nil, &agentTestLogger{})
	if err != nil {
		tb.Fatalf("NewAgentService: %v", err)
	}
	svc := svcIface.(*agentServiceImpl)
	svc.SetKVStore(kv)
	svc.SetTaskStore(NewTaskStore(kv, NewProcessClaimArbiter()))

	ctx := ContextWithTenant(context.Background(), benchTenant)

	memory := strings.Repeat("m", 30000)
	for i := 0; i < volume.jobs; i++ {
		jobID := "job-" + strconv.Itoa(i)
		status := &JobStatus{
			JobID:       jobID,
			TenantID:    benchTenant,
			State:       JobStateCompleted,
			Description: "Review jeremyhahn/go-quicbaas#" + strconv.Itoa(i),
			StartedAt:   int64(i),
			UpdatedAt:   int64(i),
			Runner:      "review",
			AgentID:     "agent-" + strconv.Itoa(i%5),
			Config: &JobConfig{
				TenantID:    benchTenant,
				Description: "Review jeremyhahn/go-quicbaas#" + strconv.Itoa(i),
				ProjectID:   "project-" + strconv.Itoa(i%6),
				Runner:      "review",
				Params:      map[string]string{"review.pull_request": strconv.Itoa(i)},
			},
			Events: []JobAuditEvent{{Timestamp: int64(i), Type: "orchestration:job_lease_acquired", Details: "job_id=" + jobID + " generation=1 node=quicbaasd-node-1"}},
		}
		if i%30 == 0 {
			status.Memory = &JobMemory{ReviewFeedback: []string{memory}}
		}
		raw, marshalErr := json.Marshal(status)
		if marshalErr != nil {
			tb.Fatalf("marshal job: %v", marshalErr)
		}
		if putErr := kv.Put(ctx, jobPersistenceKey(benchTenant, jobID), raw); putErr != nil {
			tb.Fatalf("put job: %v", putErr)
		}
	}

	body := strings.Repeat("b", 4700)
	artifact := strings.Repeat("a", 3500)
	for i := 0; i < volume.tasks; i++ {
		taskID := "task-" + strconv.Itoa(i)
		task := &Task{
			ID:              taskID,
			TenantID:        benchTenant,
			Subject:         "task " + strconv.Itoa(i),
			Description:     body,
			Status:          TaskStatusCompleted,
			ProjectID:       "project-" + strconv.Itoa(i%6),
			CreatedAt:       int64(i),
			UpdatedAt:       int64(i),
			SpawnedJobID:    "job-" + strconv.Itoa(i*20%volume.jobs),
			OutputArtifacts: []Artifact{{Kind: ArtifactKindInline, Name: "report", Content: []byte(artifact)}},
		}
		key, keyErr := taskRecordKey(benchTenant, taskID)
		if keyErr != nil {
			tb.Fatalf("task key: %v", keyErr)
		}
		raw, marshalErr := json.Marshal(task)
		if marshalErr != nil {
			tb.Fatalf("marshal task: %v", marshalErr)
		}
		if putErr := kv.Put(ctx, key, raw); putErr != nil {
			tb.Fatalf("put task: %v", putErr)
		}
	}

	prompt := strings.Repeat("p", 8000)
	for i := 0; i < volume.sessions; i++ {
		record := &SessionRecord{
			SessionID:    "session-" + strconv.Itoa(i),
			TenantID:     benchTenant,
			ProjectID:    "project-" + strconv.Itoa(i%6),
			JobID:        "job-" + strconv.Itoa(i*3%volume.jobs),
			Backend:      "claude",
			Model:        "claude-opus-5",
			SystemPrompt: prompt,
			Status:       SessionStateClosed,
			CreatedAt:    int64(i),
		}
		if putErr := store.PutSession(ctx, record); putErr != nil {
			tb.Fatalf("put session: %v", putErr)
		}
	}

	return &listFixture{svc: svc, ctx: ctx}
}

func BenchmarkListJobsPage(b *testing.B) {
	f := newListFixture(b, burnInVolume)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.svc.ListJobsPage(f.ctx, benchTenant, &JobFilter{}, &Page{Limit: 25}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListTasksPage(b *testing.B) {
	f := newListFixture(b, burnInVolume)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.svc.ListTasksPage(f.ctx, benchTenant, &TaskFilter{}, &Page{Limit: 25}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListSessionsPage(b *testing.B) {
	f := newListFixture(b, burnInVolume)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.svc.ListSessions(f.ctx, &ListSessionsRequest{Page: &Page{Limit: 25}}); err != nil {
			b.Fatal(err)
		}
	}
}
