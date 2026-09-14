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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestTaskFromProto_CarriesLifecycleFields(t *testing.T) {
	got := taskFromProto(&pb.Task{
		Id: "t1", Status: pb.TaskStatus_TASK_STATUS_BLOCKED, ProjectId: "proj-1",
		Source:                       &pb.TaskSource{Kind: "operator", Ref: "42", Url: "u"},
		Priority:                     3,
		Attempts:                     2,
		AttemptBudget:                5,
		EnvironmentFaults:            1,
		ConsecutiveEnvironmentFaults: 1,
		RetryNotBefore:               99,
		BlockedReason:                "why",
		BlockKind:                    "escalated",
		StatusChangedAt:              7,
		AutoSpawn:                    true,
		SpawnedJobId:                 "job-1",
	})
	require.NotNil(t, got)
	assert.Equal(t, "TASK_STATUS_BLOCKED", got.Status)
	assert.Equal(t, "proj-1", got.ProjectID)
	require.NotNil(t, got.Source)
	assert.Equal(t, "operator", got.Source.Kind)
	assert.Equal(t, "42", got.Source.Ref)
	assert.Equal(t, "u", got.Source.URL)
	assert.Equal(t, 3, got.Priority)
	assert.Equal(t, 2, got.Attempts)
	assert.Equal(t, 5, got.AttemptBudget)
	assert.Equal(t, 1, got.EnvironmentFaults)
	assert.Equal(t, 1, got.ConsecutiveEnvironmentFaults)
	assert.Equal(t, int64(99), got.RetryNotBefore)
	assert.Equal(t, "why", got.BlockedReason)
	assert.Equal(t, "escalated", got.BlockKind)
	assert.Equal(t, int64(7), got.StatusChangedAt)
	assert.True(t, got.AutoSpawn)
	assert.Equal(t, "job-1", got.SpawnedJobID)

	assert.Nil(t, taskFromProto(&pb.Task{Id: "bare"}).Source)
	assert.Nil(t, taskSourceToProto(nil))
	assert.Equal(t, &pb.TaskSource{Kind: "job", Ref: "j1"}, taskSourceToProto(&orchestrator.TaskSource{Kind: "job", Ref: "j1"}))
}

// TestOrchestratorHandler_DeleteTask_RoundTrips drives the SDK request through
// a real gRPC server and asserts the DECODED response, which the all-methods
// table (orchestrator_happy_test.go) only checks for the absence of an error.
func TestOrchestratorHandler_DeleteTask_RoundTrips(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)

	raw, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "DeleteTask",
		&orchestrator.DeleteTaskRequest{TenantID: "t1", TaskID: "t1"})
	require.NoError(t, err)

	resp, ok := raw.(*orchestrator.DeleteTaskResponse)
	require.True(t, ok, "handler returned %T", raw)
	assert.True(t, resp.Deleted)
	assert.Equal(t, "t1", resp.TaskID)
}

func TestOrchestratorHandler_DeleteTask_WrongRequestType(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)
	_, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "DeleteTask", "nope")
	require.Error(t, err)
}
