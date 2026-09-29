//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The forge issue behind a job, on both MCP job reads (go-quicbaas#976)
//
// THE LISTING AND THE DETAIL READ MUST AGREE ABOUT THE SAME JOB. A model that
// lists the jobs, names an issue from a row, then reads that job for its
// delivery must not watch the issue vanish between the two calls — which is
// exactly what happened while only the listing carried the field.
// =============================================================================

// issueSourceFixture is the origin both reads are checked against.
func issueSourceFixture() *orchestration.TaskSourceRef {
	return &orchestration.TaskSourceRef{
		Kind: "forge_issue",
		Ref:  "jeremyhahn/go-quicbaas#62",
		URL:  "https://forge.invalid/jeremyhahn/go-quicbaas/issues/62",
	}
}

// jobStatusResult runs the detail tool and returns its decoded result object.
func jobStatusResult(t *testing.T, svc orchestration.AgentService) map[string]interface{} {
	t.Helper()

	reg := newFleetRegistry(t, svc)
	ctx := orchestration.ContextWithTenant(context.Background(), "t1")
	result, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_get_job_status",
		map[string]interface{}{"job_id": "job-1"})
	require.NoError(t, err)

	decoded, ok := result.(map[string]interface{})
	require.True(t, ok, "tool result is %T, want a JSON object", result)
	return decoded
}

// requireIssueSource checks one decoded `source` object against the fixture.
func requireIssueSource(t *testing.T, raw interface{}) {
	t.Helper()

	source, ok := raw.(map[string]interface{})
	require.True(t, ok, "source is %T, want a JSON object", raw)
	require.Equal(t, "forge_issue", source["kind"])
	require.Equal(t, "jeremyhahn/go-quicbaas#62", source["ref"])
	require.Equal(t, "https://forge.invalid/jeremyhahn/go-quicbaas/issues/62", source["url"])
}

// TestMCPGetJobStatus_CarriesTaskSource pins the forge issue onto the DETAIL
// read. It was on the listing and missing here, so the two tools disagreed
// about the same job.
func TestMCPGetJobStatus_CarriesTaskSource(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.getJobStatusFn = func(_ context.Context, _, jobID string) (*orchestration.JobStatus, error) {
		return &orchestration.JobStatus{
			JobID:  jobID,
			State:  orchestration.JobStateCompleted,
			TaskID: "task-a",
			Source: issueSourceFixture(),
		}, nil
	}

	decoded := jobStatusResult(t, svc)
	raw, present := decoded["source"]
	require.True(t, present, "the detail read dropped the forge issue behind the job's task")
	requireIssueSource(t, raw)
}

// TestMCPGetJobStatus_WithoutTaskSourceOmitsIt is the other half: a job whose
// task records no origin must leave the key OUT, so a reader can tell "no
// issue recorded" from "the issue is blank".
func TestMCPGetJobStatus_WithoutTaskSourceOmitsIt(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.getJobStatusFn = func(_ context.Context, _, jobID string) (*orchestration.JobStatus, error) {
		return &orchestration.JobStatus{JobID: jobID, State: orchestration.JobStateCompleted}, nil
	}

	decoded := jobStatusResult(t, svc)
	_, present := decoded["source"]
	require.False(t, present, "a job with no recorded origin must emit no source at all")
}

// TestMCPListJobs_CarriesTaskSource is the listing half of the same contract:
// every row names its issue, which is what a paged consumer has no second
// request to resolve.
func TestMCPListJobs_CarriesTaskSource(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.listJobs = []*orchestration.JobStatus{
		{JobID: "job-1", TaskID: "task-a", Attempt: 2, Source: issueSourceFixture()},
		{JobID: "job-2", TaskID: "task-b", Attempt: 1},
	}

	decoded := listJobsResult(t, svc, map[string]interface{}{})
	rows, ok := decoded["jobs"].([]interface{})
	require.True(t, ok, "jobs is %T, want an array", decoded["jobs"])
	require.Len(t, rows, 2)

	byID := make(map[string]map[string]interface{}, len(rows))
	for _, row := range rows {
		job, isObject := row.(map[string]interface{})
		require.True(t, isObject, "row is %T, want a JSON object", row)
		jobID, isString := job["job_id"].(string)
		require.True(t, isString, "job_id is %T, want a string", job["job_id"])
		byID[jobID] = job
	}

	raw, present := byID["job-1"]["source"]
	require.True(t, present, "the row dropped the forge issue behind its task")
	requireIssueSource(t, raw)

	_, present = byID["job-2"]["source"]
	require.False(t, present, "a job with no recorded origin must emit no source at all")
}
