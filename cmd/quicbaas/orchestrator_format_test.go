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

// Package main: format-function coverage for orchestrator.go. The four
// previously-uncovered format helpers (DecomposeResponse, ListJobs,
// JobStatus, CancelJob) plus the shouldDisplayAuditEvent verbosity
// filter are pinned here.
package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// formatOrchDecomposeResponse
// =============================================================================

func TestFormatOrchDecomposeResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.DecomposeTaskResponse{
		WorkItems: []*orchestratorSDK.WorkItem{
			{ID: "w1", ProjectID: "p1", Priority: 1, Title: "Build", Description: "desc"},
			{ID: "w2", ProjectID: "p1", Priority: 2, Title: "Test", Dependencies: []string{"w1"}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, formatOrchDecomposeResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "w1")
	assert.Contains(t, got, "w2")
	assert.Contains(t, got, "ID")
}

func TestFormatOrchDecomposeResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.DecomposeTaskResponse{
		WorkItems: []*orchestratorSDK.WorkItem{{ID: "w1"}},
	}
	var buf bytes.Buffer
	require.NoError(t, formatOrchDecomposeResponse(resp, "json", &buf))
	assert.Contains(t, buf.String(), `"work_items"`)
}

func TestFormatOrchDecomposeResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.DecomposeTaskResponse{
		WorkItems: []*orchestratorSDK.WorkItem{{ID: "w1"}},
	}
	var buf bytes.Buffer
	require.NoError(t, formatOrchDecomposeResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "w1")
}

func TestFormatOrchDecomposeResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatOrchDecomposeResponse(&orchestratorSDK.DecomposeTaskResponse{}, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatOrchListJobsResponse
// =============================================================================

func TestFormatOrchListJobsResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.ListJobsResponse{
		Jobs: []orchestratorSDK.JobSummary{
			{JobID: "j1", State: "running", Description: "build"},
			{JobID: "j2", State: "completed", Description: "test"},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, formatOrchListJobsResponse(resp, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "j1")
	assert.Contains(t, got, "j2")
}

func TestFormatOrchListJobsResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.ListJobsResponse{
		Jobs: []orchestratorSDK.JobSummary{{JobID: "j1"}},
	}
	var buf bytes.Buffer
	require.NoError(t, formatOrchListJobsResponse(resp, "json", &buf))
	assert.Contains(t, buf.String(), `"jobs"`)
}

func TestFormatOrchListJobsResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.ListJobsResponse{
		Jobs: []orchestratorSDK.JobSummary{{JobID: "j1"}},
	}
	var buf bytes.Buffer
	require.NoError(t, formatOrchListJobsResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "j1")
}

func TestFormatOrchListJobsResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatOrchListJobsResponse(&orchestratorSDK.ListJobsResponse{}, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatOrchJobStatusResponse
// =============================================================================

func TestFormatOrchJobStatusResponse_Table(t *testing.T) {
	status := &orchestratorSDK.JobStatus{
		JobID: "j1", State: "running", Iteration: 2,
		TaskCount: 5, Completed: 3, Failed: 0,
		Description: "build",
	}
	var buf bytes.Buffer
	require.NoError(t, formatOrchJobStatusResponse(status, "table", &buf))
	got := buf.String()
	assert.Contains(t, got, "j1")
	assert.Contains(t, got, "running")
}

func TestFormatOrchJobStatusResponse_JSON(t *testing.T) {
	status := &orchestratorSDK.JobStatus{JobID: "j1"}
	var buf bytes.Buffer
	require.NoError(t, formatOrchJobStatusResponse(status, "json", &buf))
	assert.Contains(t, buf.String(), `"job_id"`)
}

func TestFormatOrchJobStatusResponse_YAML(t *testing.T) {
	status := &orchestratorSDK.JobStatus{JobID: "j1"}
	var buf bytes.Buffer
	require.NoError(t, formatOrchJobStatusResponse(status, "yaml", &buf))
	assert.Contains(t, buf.String(), "j1")
}

func TestFormatOrchJobStatusResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatOrchJobStatusResponse(&orchestratorSDK.JobStatus{}, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// formatOrchCancelJobResponse
// =============================================================================

func TestFormatOrchCancelJobResponse_Table(t *testing.T) {
	resp := &orchestratorSDK.CancelJobResponse{JobID: "j1", Cancelled: true}
	var buf bytes.Buffer
	require.NoError(t, formatOrchCancelJobResponse(resp, "table", &buf))
	assert.Contains(t, buf.String(), "j1")
}

func TestFormatOrchCancelJobResponse_JSON(t *testing.T) {
	resp := &orchestratorSDK.CancelJobResponse{JobID: "j1", Cancelled: true}
	var buf bytes.Buffer
	require.NoError(t, formatOrchCancelJobResponse(resp, "json", &buf))
	assert.Contains(t, buf.String(), `"cancelled"`)
}

func TestFormatOrchCancelJobResponse_YAML(t *testing.T) {
	resp := &orchestratorSDK.CancelJobResponse{JobID: "j1"}
	var buf bytes.Buffer
	require.NoError(t, formatOrchCancelJobResponse(resp, "yaml", &buf))
	assert.Contains(t, buf.String(), "j1")
}

func TestFormatOrchCancelJobResponse_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	err := formatOrchCancelJobResponse(&orchestratorSDK.CancelJobResponse{}, "bogus", &buf)
	require.Error(t, err)
}

// =============================================================================
// shouldDisplayAuditEvent
// =============================================================================

func TestShouldDisplayAuditEvent(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		verbosity string
		want      bool
	}{
		// debug: everything
		{"debug-all", "anything", "debug", true},
		{"debug-agent-msg", "agent:noise", "debug", true},
		// verbose: hides agent:* except specific allowlist
		{"verbose-orchestration", "orchestration:started", "verbose", true},
		{"verbose-agent-other", "agent:noise", "verbose", false},
		{"verbose-agent-assistant", "agent:assistant", "verbose", true},
		{"verbose-agent-result", "agent:result", "verbose", true},
		// info (default): orchestration: + lead: only
		{"info-orchestration", "orchestration:started", "info", true},
		{"info-lead", "lead:elected", "info", true},
		{"info-agent", "agent:assistant", "info", false},
		{"info-unknown", "session:open", "info", false},
		// default branch ("") behaves as info
		{"empty-orchestration", "orchestration:done", "", true},
		{"empty-noise", "noise", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldDisplayAuditEvent(tc.eventType, tc.verbosity)
			assert.Equal(t, tc.want, got)
		})
	}
}
