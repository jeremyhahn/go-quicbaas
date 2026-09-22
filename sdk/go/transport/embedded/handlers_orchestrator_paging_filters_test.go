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

package embedded

import (
	"context"
	"testing"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// The narrowings a paged consumer needs, over the in-process transport
// =============================================================================

// TestEmbeddedListJobs_CarriesTheJobIDsFilter proves the embedded transport
// hands the service the ids the caller named. A page of sessions resolves the
// jobs its rows name in one call; a transport that dropped the filter would
// list the whole tenant and look like it worked.
func TestEmbeddedListJobs_CarriesTheJobIDsFilter(t *testing.T) {
	agent := &fakeAgentService{}
	svc := &orchMock{agent: agent}

	out, err := handleOrchestratorListJobs(context.Background(), svc, &orchestratorSDK.ListJobsRequest{
		TenantID: "t-1",
		JobIDs:   []string{"job-1", "job-2"},
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if _, ok := out.(*orchestratorSDK.ListJobsResponse); !ok {
		t.Fatalf("response = %T, want *orchestrator.ListJobsResponse", out)
	}
	if agent.listJobsFilter == nil {
		t.Fatal("no filter reached the service")
	}
	if got := agent.listJobsFilter.JobIDs; len(got) != 2 || got[0] != "job-1" || got[1] != "job-2" {
		t.Errorf("JobIDs = %v, want [job-1 job-2]", got)
	}
}

// TestEmbeddedListTasks_CarriesTheDispatchModeFilter proves the tri-state
// auto_spawn narrowing survives the transport in all three of its states.
func TestEmbeddedListTasks_CarriesTheDispatchModeFilter(t *testing.T) {
	dispatched, queued := true, false

	tests := []struct {
		name string
		ask  *bool
	}{
		{name: "no preference", ask: nil},
		{name: "dispatched only", ask: &dispatched},
		{name: "queued only", ask: &queued},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent := &fakeAgentService{}
			svc := &orchMock{agent: agent}

			if _, err := handleOrchestratorListTasks(context.Background(), svc, &orchestratorSDK.ListTasksRequest{
				TenantID:  "t-1",
				AutoSpawn: tc.ask,
			}); err != nil {
				t.Fatalf("ListTasks: %v", err)
			}
			if agent.listTasksFilter == nil {
				t.Fatal("no filter reached the service")
			}
			got := agent.listTasksFilter.AutoSpawn
			switch {
			case tc.ask == nil && got != nil:
				t.Errorf("AutoSpawn = %v, want nil — an absent preference must narrow nothing", *got)
			case tc.ask != nil && got == nil:
				t.Errorf("AutoSpawn = nil, want %v", *tc.ask)
			case tc.ask != nil && *got != *tc.ask:
				t.Errorf("AutoSpawn = %v, want %v", *got, *tc.ask)
			}
		})
	}
}

// TestEmbeddedListSessions_CarriesTheRoleFields proves the projection keeps
// the three fields the Role badge is built from — the runner that decides the
// role, the job it came from, and the model beside it. The projection dropped
// all three, so the in-process transport rendered no role at all.
func TestEmbeddedListSessions_CarriesTheRoleFields(t *testing.T) {
	svc := &orchMock{agent: &fakeAgentService{}}

	out, err := handleOrchestratorListSessions(context.Background(), svc, &orchestratorSDK.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	resp, ok := out.(*orchestratorSDK.ListSessionsResponse)
	if !ok {
		t.Fatalf("response = %T, want *orchestrator.ListSessionsResponse", out)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("listed %d session(s), want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].JobID != "job-1" {
		t.Errorf("job_id = %q, want job-1", resp.Sessions[0].JobID)
	}
	if resp.Sessions[0].Model != "m" {
		t.Errorf("model = %q, want m", resp.Sessions[0].Model)
	}
	if resp.Sessions[0].Runner != "review" {
		t.Errorf("runner = %q, want review; the Role badge is blank without it", resp.Sessions[0].Runner)
	}
}

// TestEmbeddedListJobs_RejectsTheWrongRequestType proves the transport names
// what it was handed rather than panicking on the type assertion.
func TestEmbeddedListJobs_RejectsTheWrongRequestType(t *testing.T) {
	svc := &orchMock{agent: &fakeAgentService{}}

	if _, err := handleOrchestratorListJobs(context.Background(), svc, &orchestratorSDK.ListTasksRequest{}); err == nil {
		t.Fatal("ListJobs accepted a ListTasksRequest")
	}
}
