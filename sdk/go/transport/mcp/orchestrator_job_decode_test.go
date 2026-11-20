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

package mcp

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// The spawn, start_job, get_job_status and health tool results, in the shape
// the server writes them, reach the SDK types the client asserts
// (go-quicbaas#1644). Before, the raw result came back and the client's type
// assertion failed with "invalid response type".

func TestOrchestratorSpawnSessionDecodes(t *testing.T) {
	out, err := newOrchestratorMCPHandler().transformResponse("SpawnSession",
		`{"session_id":"sess-1","backend":"claude","model":"m","tenant_id":"t1","persistent":true,`+
			`"timestamp":"2026-10-12T08:30:15Z","container_id":"ctr-1","resolved_agent_id":"agent-b",`+
			`"failovers":[{"agent_id":"agent-a","reason":"exhausted"}]}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	resp, ok := out.(*orchestrator.SpawnSessionResponse)
	if !ok {
		t.Fatalf("result is %T, want *orchestrator.SpawnSessionResponse", out)
	}
	if resp.SessionID != "sess-1" || resp.ContainerID != "ctr-1" || resp.ResolvedAgentID != "agent-b" {
		t.Fatalf("decoded %+v", resp)
	}
	if len(resp.Failovers) != 1 || resp.Failovers[0].AgentID != "agent-a" {
		t.Fatalf("failovers = %+v", resp.Failovers)
	}
}

func TestOrchestratorStartJobDecodesTheInstantInNanoseconds(t *testing.T) {
	started := time.Date(2026, 10, 12, 8, 30, 15, 123456789, time.UTC)
	out, err := newOrchestratorMCPHandler().transformResponse("StartJob",
		`{"job_id":"job-1","timestamp":"`+started.Format(time.RFC3339Nano)+`"}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	resp, ok := out.(*orchestrator.StartJobResponse)
	if !ok {
		t.Fatalf("result is %T, want *orchestrator.StartJobResponse", out)
	}
	if resp.JobID != "job-1" || resp.Timestamp != started.UnixNano() {
		t.Fatalf("decoded %+v, want job-1 at %d", resp, started.UnixNano())
	}
}

func TestOrchestratorGetJobStatusDecodes(t *testing.T) {
	out, err := newOrchestratorMCPHandler().transformResponse("GetJobStatus",
		`{"job_id":"job-1","state":"running","started_at":1760257815000000000,`+
			`"prompt_append":"job material","timestamp":"2026-10-12T08:30:15Z"}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	resp, ok := out.(*orchestrator.JobStatus)
	if !ok {
		t.Fatalf("result is %T, want *orchestrator.JobStatus", out)
	}
	if resp.JobID != "job-1" || resp.State != "running" || resp.PromptAppend != "job material" ||
		resp.StartedAt != 1760257815000000000 {
		t.Fatalf("decoded %+v", resp)
	}
}

func TestOrchestratorHealthDecodesTheReport(t *testing.T) {
	at := time.Date(2026, 10, 12, 8, 30, 15, 5, time.UTC)
	out, err := newOrchestratorMCPHandler().transformResponse("Health",
		`{"total_sessions":2,"healthy":1,"unhealthy":1,"closed":0,`+
			`"results":[{"session_id":"s1","healthy":true},{"session_id":"s2","healthy":false}],`+
			`"timestamp":"`+at.Format(time.RFC3339Nano)+`"}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	resp, ok := out.(*orchestrator.HealthCheckReport)
	if !ok {
		t.Fatalf("result is %T, want *orchestrator.HealthCheckReport", out)
	}
	if resp.Healthy || resp.Status != "degraded" || resp.Timestamp != at.UnixNano() {
		t.Fatalf("decoded %+v", resp)
	}
	if resp.Components["s1"] != "healthy" || resp.Components["s2"] != "unhealthy" {
		t.Fatalf("components = %v", resp.Components)
	}
}
