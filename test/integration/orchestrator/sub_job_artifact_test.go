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

//go:build integration

package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// startParentJob starts an orchestration job with an explicit lead agent ID
// and waits for the job's status to be loadable. Returns the parent job ID.
//
// The control loop runs asynchronously and may end up in failed state because
// real backends aren't reachable in the test, but the parent's Config is
// populated synchronously by StartJob, which is all SpawnChildJob needs.
func startParentJob(t *testing.T, ctx context.Context, tenantID, leadAgentID string) string {
	t.Helper()
	resp, err := testService.Agent().StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:    tenantID,
		Description: "parent job " + uniqueSuffix(),
		LeadAgentID: leadAgentID,
	})
	require.NoError(t, err, "StartJob")
	require.NotEmpty(t, resp.JobID)

	// Wait until GetJobStatus returns a non-nil status (the control loop has
	// initialised). Use a deadline-bounded loop with a ticker, no time.Sleep.
	deadline := time.Now().Add(3 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		status, gerr := testService.Agent().GetJobStatus(ctx, tenantID, resp.JobID)
		if gerr == nil && status != nil && status.Config != nil && status.Config.LeadAgentID == leadAgentID {
			return resp.JobID
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ctx cancelled while waiting for job: %v", ctx.Err())
		case <-ticker.C:
		}
	}
	t.Fatalf("parent job %s did not expose Config.LeadAgentID within deadline", resp.JobID)
	return ""
}

// TestSpawnChildJob_LeadCanSpawn verifies that the parent job's lead agent is
// allowed to spawn a child job, and that the child ID is recorded on the
// parent's ChildJobIDs list.
func TestSpawnChildJob_LeadCanSpawn(t *testing.T) {
	tenantID := newTenant(t, "tenant-spawn-")
	ctx := tenantCtx(tenantID)

	leadAgent := registerOnlineAgent(t, ctx, tenantID, "lead-"+uniqueSuffix(), "ollama", nil)
	parentJobID := startParentJob(t, ctx, tenantID, leadAgent.ID)

	resp, err := testService.Agent().SpawnChildJob(ctx, &orchestration.SpawnChildJobRequest{
		ParentJobID:   parentJobID,
		CallerAgentID: leadAgent.ID,
		Tasks: []*orchestration.TaskSpec{
			{Title: "child-task-1", Description: "first child task"},
		},
	})
	require.NoError(t, err, "lead-issued SpawnChildJob should succeed")
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.ChildJobID)
	assert.NotEqual(t, parentJobID, resp.ChildJobID)

	// Verify the parent's ChildJobIDs slice was updated.
	parentStatus, err := testService.Agent().GetJobStatus(ctx, tenantID, parentJobID)
	require.NoError(t, err)
	require.NotNil(t, parentStatus)
	assert.Contains(t, parentStatus.ChildJobIDs, resp.ChildJobID,
		"parent should have child registered atomically")
}

// TestSpawnChildJob_NonLeadRejected verifies that an agent that is not the
// parent's lead is rejected with *NotLeadOfParentError.
func TestSpawnChildJob_NonLeadRejected(t *testing.T) {
	tenantID := newTenant(t, "tenant-spawn-noLead-")
	ctx := tenantCtx(tenantID)

	leadAgent := registerOnlineAgent(t, ctx, tenantID, "lead-"+uniqueSuffix(), "ollama", nil)
	workerAgent := registerOnlineAgent(t, ctx, tenantID, "worker-"+uniqueSuffix(), "ollama", nil)
	parentJobID := startParentJob(t, ctx, tenantID, leadAgent.ID)

	_, err := testService.Agent().SpawnChildJob(ctx, &orchestration.SpawnChildJobRequest{
		ParentJobID:   parentJobID,
		CallerAgentID: workerAgent.ID, // not the lead!
		Tasks: []*orchestration.TaskSpec{
			{Title: "rogue-task"},
		},
	})
	require.Error(t, err)

	var notLead *orchestration.NotLeadOfParentError
	require.True(t, errors.As(err, &notLead),
		"expected *NotLeadOfParentError, got %T: %v", err, err)
	assert.Equal(t, parentJobID, notLead.ParentJobID)
	assert.Equal(t, workerAgent.ID, notLead.CallerAgentID)
	assert.Equal(t, leadAgent.ID, notLead.LeadAgentID)
}

// TestArtifact_InlineRoundTrip verifies that a small inline artifact can be
// submitted on a task and round-trips through GetTaskArtifact returning the
// original bytes.
func TestArtifact_InlineRoundTrip(t *testing.T) {
	tenantID := newTenant(t, "tenant-art-inline-")
	ctx := tenantCtx(tenantID)

	task, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID: tenantID,
		Subject:  "task-with-artifact",
	})
	require.NoError(t, err)

	payload := []byte("hello-artifact-" + uniqueSuffix())
	err = testService.Agent().SubmitTaskArtifacts(ctx, tenantID, task.ID, []orchestration.Artifact{
		{
			Kind:     orchestration.ArtifactKindInline,
			Name:     "report.txt",
			MIMEType: "text/plain",
			Content:  payload,
			Size:     int64(len(payload)),
		},
	})
	require.NoError(t, err, "SubmitTaskArtifacts should accept a valid inline artifact")

	got, err := testService.Agent().GetTaskArtifact(ctx, tenantID, task.ID, "report.txt")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "report.txt", got.Name)
	assert.Equal(t, payload, got.Content, "round-tripped content should match")
}

// TestArtifact_FileRefMissingPath verifies that submitting an
// ArtifactKindFile artifact with empty Ref.Path yields the typed
// *ArtifactRefInvalidError.
func TestArtifact_FileRefMissingPath(t *testing.T) {
	tenantID := newTenant(t, "tenant-art-fileref-")
	ctx := tenantCtx(tenantID)

	task, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID: tenantID,
		Subject:  "task-with-bad-fileref",
	})
	require.NoError(t, err)

	err = testService.Agent().SubmitTaskArtifacts(ctx, tenantID, task.ID, []orchestration.Artifact{
		{
			Kind: orchestration.ArtifactKindFile,
			Name: "bad-file",
			Ref: orchestration.ArtifactRef{
				AgentID: "agent-x",
				// Path intentionally missing
			},
		},
	})
	require.Error(t, err)

	var refErr *orchestration.ArtifactRefInvalidError
	require.True(t, errors.As(err, &refErr),
		"expected *ArtifactRefInvalidError, got %T: %v", err, err)
	assert.Equal(t, orchestration.ArtifactKindFile, refErr.Kind)
	assert.Equal(t, "path", refErr.Missing)
}

// TestArtifact_LargeOverflowPromoted verifies that an inline artifact larger
// than the configured InlineLimit (1 MiB) is promoted to its own KV key and
// round-trips intact, rather than being rejected or silently truncated.
//
// The task store is now always KV-backed — task records and artifact payloads
// share the same store — so the "no promotion target available" case this test
// previously asserted (*ArtifactTooLargeError) can no longer occur. The
// guarantee that matters is byte-exact round-tripping of a promoted payload.
func TestArtifact_LargeOverflowPromoted(t *testing.T) {
	tenantID := newTenant(t, "tenant-art-large-")
	ctx := tenantCtx(tenantID)

	task, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID: tenantID,
		Subject:  "task-too-large",
	})
	require.NoError(t, err)

	// 1 MiB + 1 byte payload.
	payload := make([]byte, orchestration.InlineLimit+1)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	err = testService.Agent().SubmitTaskArtifacts(ctx, tenantID, task.ID, []orchestration.Artifact{
		{
			Kind:    orchestration.ArtifactKindInline,
			Name:    "huge.bin",
			Content: payload,
			Size:    int64(len(payload)),
		},
	})
	require.NoError(t, err, "oversized inline artifact must be promoted to the KV store")

	got, err := testService.Agent().GetTaskArtifact(ctx, tenantID, task.ID, "huge.bin")
	require.NoError(t, err, "a promoted artifact must be retrievable")
	assert.Equal(t, payload, got.Content, "promoted artifact must round-trip byte-exactly")
}
