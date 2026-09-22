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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestEmbedded_BlockTask_ForwardsToAgentService(t *testing.T) {
	fake := &fakeAgentService{}
	svc := &orchMock{agent: fake}

	resp, err := handleOrchestratorBlockTask(context.Background(), svc, &orchestratorSDK.BlockTaskRequest{
		TenantID: "t1", TaskID: "task-1", Kind: "needs_info", Reason: "which branch?",
	})
	require.NoError(t, err)
	out, ok := resp.(*orchestratorSDK.BlockTaskResponse)
	require.True(t, ok)
	assert.True(t, out.Blocked)
	assert.Equal(t, "task-1", out.TaskID)
	assert.Equal(t, "needs_info", out.Kind)
	require.Len(t, fake.blockCalls, 1)
	assert.Equal(t, orchestration.TaskBlockKindNeedsInfo, fake.blockCalls[0].Kind)
	assert.Equal(t, "which branch?", fake.blockCalls[0].Reason)
	assert.Equal(t, "t1", fake.blockCalls[0].TenantID)
}

func TestEmbedded_BlockTask_Errors(t *testing.T) {
	t.Run("wrong request type", func(t *testing.T) {
		_, err := handleOrchestratorBlockTask(context.Background(), &orchMock{agent: &fakeAgentService{}}, "nope")
		require.Error(t, err)
	})
	t.Run("no agent service", func(t *testing.T) {
		_, err := handleOrchestratorBlockTask(context.Background(), &orchMock{}, &orchestratorSDK.BlockTaskRequest{})
		require.Error(t, err)
	})
	t.Run("service error propagates", func(t *testing.T) {
		_, err := handleOrchestratorBlockTask(context.Background(), &orchMock{agent: &fakeAgentService{fail: true}}, &orchestratorSDK.BlockTaskRequest{TaskID: "x"})
		require.ErrorIs(t, err, errOrch)
	})
}

func TestEmbedded_ReadmitTask(t *testing.T) {
	t.Run("forwards", func(t *testing.T) {
		fake := &fakeAgentService{}
		resp, err := handleOrchestratorReadmitTask(context.Background(), &orchMock{agent: fake}, &orchestratorSDK.ReadmitTaskRequest{TenantID: "t1", TaskID: "task-1"})
		require.NoError(t, err)
		out, ok := resp.(*orchestratorSDK.ReadmitTaskResponse)
		require.True(t, ok)
		assert.True(t, out.Readmitted)
		assert.Equal(t, "task-1", out.TaskID)
		require.Len(t, fake.readmitCalls, 1)
		assert.Equal(t, "task-1", fake.readmitCalls[0].TaskID)
	})
	t.Run("wrong request type", func(t *testing.T) {
		_, err := handleOrchestratorReadmitTask(context.Background(), &orchMock{agent: &fakeAgentService{}}, 42)
		require.Error(t, err)
	})
	t.Run("no agent service", func(t *testing.T) {
		_, err := handleOrchestratorReadmitTask(context.Background(), &orchMock{}, &orchestratorSDK.ReadmitTaskRequest{})
		require.Error(t, err)
	})
	t.Run("service error propagates", func(t *testing.T) {
		_, err := handleOrchestratorReadmitTask(context.Background(), &orchMock{agent: &fakeAgentService{fail: true}}, &orchestratorSDK.ReadmitTaskRequest{TaskID: "x"})
		require.ErrorIs(t, err, errOrch)
	})
}

func TestEmbedded_ConvertTask_CarriesLifecycleFields(t *testing.T) {
	task := &orchestration.Task{
		ID: "t1", Status: orchestration.TaskStatusBlocked, ProjectID: "proj-1",
		Source:                       &orchestration.TaskSourceRef{Kind: orchestration.TaskSourceKindOperator, Ref: "42", URL: "u"},
		Priority:                     3,
		Attempts:                     2,
		AttemptBudget:                5,
		EnvironmentFaults:            1,
		ConsecutiveEnvironmentFaults: 1,
		RetryNotBefore:               99,
		BlockedReason:                "why",
		BlockKind:                    orchestration.TaskBlockKindEscalated,
		StatusChangedAt:              7,
		OutputArtifacts:              []orchestration.Artifact{{Name: "a.txt", MIMEType: "text/plain", Size: 3}},
	}
	got := convertTask(task)
	assert.Equal(t, "blocked", got.Status)
	assert.Equal(t, "proj-1", got.ProjectID)
	require.NotNil(t, got.Source)
	assert.Equal(t, "operator", got.Source.Kind)
	assert.Equal(t, "42", got.Source.Ref)
	assert.Equal(t, 3, got.Priority)
	assert.Equal(t, 2, got.Attempts)
	assert.Equal(t, 5, got.AttemptBudget)
	assert.Equal(t, 1, got.EnvironmentFaults)
	assert.Equal(t, 1, got.ConsecutiveEnvironmentFaults)
	assert.Equal(t, int64(99), got.RetryNotBefore)
	assert.Equal(t, "why", got.BlockedReason)
	assert.Equal(t, "escalated", got.BlockKind)
	assert.Equal(t, int64(7), got.StatusChangedAt)
	require.Len(t, got.OutputArtifacts, 1)
	assert.Equal(t, "a.txt", got.OutputArtifacts[0].Name)

	bare := convertTask(&orchestration.Task{ID: "t2"})
	assert.Nil(t, bare.Source)
	assert.Nil(t, bare.OutputArtifacts)
	assert.Nil(t, convertTask(nil))
}

func TestEmbedded_ConvertJobStatus_CarriesTaskBindingAndSessions(t *testing.T) {
	got := convertJobStatus(&orchestration.JobStatus{
		JobID: "j1", TaskID: "task-1", Attempt: 2, Node: "n", HeartbeatAt: 5, FailureClass: "environment",
		LeadSessionID: "lead", SessionIDs: []string{"lead", "w1"}, Stage: "complete",
	})
	assert.Equal(t, "task-1", got.TaskID)
	assert.Equal(t, "complete", got.Stage)
	assert.Equal(t, 2, got.Attempt)
	assert.Equal(t, "n", got.Node)
	assert.Equal(t, int64(5), got.HeartbeatAt)
	assert.Equal(t, "environment", got.FailureClass)
	assert.Equal(t, "lead", got.LeadSessionID)
	assert.Equal(t, []string{"lead", "w1"}, got.SessionIDs)
}

func TestEmbedded_TaskSourceAndStatusConversions(t *testing.T) {
	assert.Nil(t, taskSourceFromSDK(nil))
	assert.Nil(t, taskSourceToSDK(nil))
	src := taskSourceFromSDK(&orchestratorSDK.TaskSource{Kind: "operator", Ref: "alice", URL: "u"})
	assert.Equal(t, orchestration.TaskSourceKindOperator, src.Kind)
	assert.Equal(t, "alice", src.Ref)
	// The status conversion is the SHARED, VALIDATING one now: an absent
	// filter narrows nothing, a known status converts, and one the engine
	// does not have is refused rather than matched against nothing.
	empty, err := orchestration.TaskStatusesFrom("statuses", nil)
	assert.NoError(t, err)
	assert.Nil(t, empty)

	known, err := orchestration.TaskStatusesFrom("statuses", []string{"blocked", "pending"})
	assert.NoError(t, err)
	assert.Equal(t, []orchestration.TaskStatus{orchestration.TaskStatusBlocked, orchestration.TaskStatusPending}, known)

	refused, err := orchestration.TaskStatusesFrom("statuses", []string{"blokced"})
	assert.Nil(t, refused)
	assert.ErrorIs(t, err, orchestration.ErrFilterParamInvalid)
}
