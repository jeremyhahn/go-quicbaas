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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// isOrchestratorService
// =============================================================================

func TestIsOrchestratorService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{
			name:     "orchestrator service",
			service:  "quicbaas.Orchestrator",
			expected: true,
		},
		{
			name:     "orchestrator service with suffix",
			service:  "quicbaas.Orchestrator.Session",
			expected: true,
		},
		{
			name:     "kv service",
			service:  "quicbaas.KV",
			expected: false,
		},
		{
			name:     "iam service",
			service:  "quicbaas.IAM.User",
			expected: false,
		},
		{
			name:     "empty string",
			service:  "",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isOrchestratorService(tc.service)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// newOrchestratorHandler
// =============================================================================

func TestNewOrchestratorHandler(t *testing.T) {
	t.Run("creates handler with nil conn", func(t *testing.T) {
		handler := newOrchestratorHandler(nil)
		require.NotNil(t, handler)
		assert.Nil(t, handler.conn)
	})
}

// =============================================================================
// handleRequest dispatch
// =============================================================================

func TestOrchestratorHandleRequest_UnsupportedMethod(t *testing.T) {
	handler := newOrchestratorHandler(nil)
	_, err := handler.handleRequest(context.Background(), "quicbaas.Orchestrator", "NonExistentMethod", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported orchestrator method")
}

func TestOrchestratorHandleRequest_AllMethodsMapped(t *testing.T) {
	expectedMethods := []string{
		"Health", "ListBackends",
		"SpawnSession", "PromptSession", "SessionStatus", "CloseSession", "ListSessions",
		"CreateTask", "GetTask", "ListTasks", "ClaimTask", "CompleteTask",
		"RegisterFleetNode", "GetFleetNode", "ListFleetNodes", "RemoveFleetNode", "FleetNodeHeartbeat",
	}
	for _, method := range expectedMethods {
		t.Run(method, func(t *testing.T) {
			_, ok := orchestratorMethodMap[method]
			assert.True(t, ok, "method %s should be in orchestratorMethodMap", method)
		})
	}
}

// =============================================================================
// Type assertion error paths
// =============================================================================

func TestOrchestratorSpawnSession_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorSpawnSession(context.Background(), nil, "not-a-request")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorPromptSession_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorPromptSession(context.Background(), nil, 42)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorSessionStatus_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorSessionStatus(context.Background(), nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorCloseSession_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorCloseSession(context.Background(), nil, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorListSessions_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorListSessions(context.Background(), nil, 123)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorCreateTask_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorCreateTask(context.Background(), nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorGetTask_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorGetTask(context.Background(), nil, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorListTasks_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorListTasks(context.Background(), nil, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorClaimTask_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorClaimTask(context.Background(), nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorCompleteTask_InvalidRequestType(t *testing.T) {
	_, err := handleOrchestratorCompleteTask(context.Background(), nil, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorRegisterAgent_InvalidRequestType(t *testing.T) {
	_, err := handleFleetRegister(context.Background(), nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorGetAgent_InvalidRequestType(t *testing.T) {
	_, err := handleFleetGet(context.Background(), nil, struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorListAgents_InvalidRequestType(t *testing.T) {
	_, err := handleFleetList(context.Background(), nil, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorRemoveAgent_InvalidRequestType(t *testing.T) {
	_, err := handleFleetRemove(context.Background(), nil, 3.14)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorAgentHeartbeat_InvalidRequestType(t *testing.T) {
	_, err := handleFleetHeartbeat(context.Background(), nil, []byte{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

// =============================================================================
// Proto-to-SDK Conversion: taskFromProto
// =============================================================================

func TestTaskFromProto_NilTask(t *testing.T) {
	result := taskFromProto(nil)
	assert.Nil(t, result)
}

func TestTaskFromProto_CompleteTask(t *testing.T) {
	now := timestamppb.Now()
	claimed := timestamppb.New(now.AsTime().Add(-time.Hour))
	completed := timestamppb.New(now.AsTime().Add(-time.Minute))

	protoTask := &pb.Task{
		Id:           "task-123",
		TenantId:     "tenant-abc",
		TeamName:     "team-alpha",
		Subject:      "fix the widget",
		Description:  "the widget is broken",
		Status:       pb.TaskStatus_TASK_STATUS_COMPLETED,
		AssignedTo:   "agent-1",
		Dependencies: []string{"task-100", "task-101"},
		CreatedAt:    now,
		ClaimedAt:    claimed,
		CompletedAt:  completed,
		Version:      5,
		Metadata:     map[string]string{"priority": "high"},
		Result: &pb.TaskResult{
			Output:   "widget fixed",
			Files:    []string{"fix.patch"},
			Metadata: map[string]string{"lines_changed": "42"},
		},
	}

	result := taskFromProto(protoTask)
	require.NotNil(t, result)
	assert.Equal(t, "task-123", result.ID)
	assert.Equal(t, "tenant-abc", result.TenantID)
	assert.Equal(t, "team-alpha", result.TeamName)
	assert.Equal(t, "fix the widget", result.Subject)
	assert.Equal(t, "the widget is broken", result.Description)
	assert.Contains(t, result.Status, "COMPLETED")
	assert.Equal(t, "agent-1", result.AssignedTo)
	assert.Equal(t, []string{"task-100", "task-101"}, result.Dependencies)
	assert.Equal(t, now.AsTime().Unix(), result.CreatedAt)
	assert.Equal(t, claimed.AsTime().Unix(), result.ClaimedAt)
	assert.Equal(t, completed.AsTime().Unix(), result.CompletedAt)
	assert.Equal(t, uint64(5), result.Version)
	assert.Equal(t, "high", result.Metadata["priority"])
	require.NotNil(t, result.Result)
	assert.Equal(t, "widget fixed", result.Result.Output)
	assert.Equal(t, []string{"fix.patch"}, result.Result.Files)
	assert.Equal(t, "42", result.Result.Metadata["lines_changed"])
}

func TestTaskFromProto_NoResult(t *testing.T) {
	protoTask := &pb.Task{
		Id:       "task-456",
		TenantId: "tenant-xyz",
		Subject:  "no result task",
		Status:   pb.TaskStatus_TASK_STATUS_PENDING,
	}

	result := taskFromProto(protoTask)
	require.NotNil(t, result)
	assert.Equal(t, "task-456", result.ID)
	assert.Nil(t, result.Result)
}

// TestTaskFromProto_CarriesFaultAccounting: every fault and budget field the
// server derives must survive the decode (go-quicbaas#947). A field the SDK
// dropped would read as "no fault" on a task that has one.
func TestTaskFromProto_CarriesFaultAccounting(t *testing.T) {
	result := taskFromProto(&pb.Task{
		Id: "task-1", Status: pb.TaskStatus_TASK_STATUS_BLOCKED, Attempts: 3,
		BlockKind: "blocked", BlockFaultClass: "forge_unreachable",
		ConclusionReason: "fault_bounded", ChargedAttempts: 1, EffectiveBudget: 4,
		FaultClass: "forge_unreachable", FaultOrigin: "environment",
	})
	require.NotNil(t, result)
	assert.Equal(t, "forge_unreachable", result.BlockFaultClass)
	assert.Equal(t, "fault_bounded", result.ConclusionReason)
	assert.Equal(t, 1, result.ChargedAttempts)
	assert.Equal(t, 4, result.EffectiveBudget)
	assert.Equal(t, "forge_unreachable", result.FaultClass)
	assert.Equal(t, "environment", result.FaultOrigin)

	open := taskFromProto(&pb.Task{Id: "task-2", Status: pb.TaskStatus_TASK_STATUS_PENDING})
	assert.Empty(t, open.FaultOrigin)
	assert.Empty(t, open.ConclusionReason)
}

// =============================================================================
// Proto-to-SDK Conversion: fleetNodeFromProto
// =============================================================================

func TestAgentRegistrationFromProto_NilAgent(t *testing.T) {
	result := fleetNodeFromProto(nil)
	assert.Nil(t, result)
}

func TestAgentRegistrationFromProto_CompleteAgent(t *testing.T) {
	now := timestamppb.Now()
	registered := timestamppb.New(now.AsTime().Add(-24 * time.Hour))

	protoAgent := &pb.FleetNode{
		Id:           "agent-001",
		TenantId:     "tenant-abc",
		Name:         "test-agent",
		Host:         "host-1.example.com",
		Backend:      "claude",
		NodeType:     "coding",
		Status:       pb.FleetNodeStatus_FLEET_NODE_STATUS_ONLINE,
		WorkDir:      "/workspace",
		LastSeen:     now,
		RegisteredAt: registered,
		Version:      3,
		Metadata:     map[string]string{"region": "us-east-1"},
	}

	result := fleetNodeFromProto(protoAgent)
	require.NotNil(t, result)
	assert.Equal(t, "agent-001", result.ID)
	assert.Equal(t, "tenant-abc", result.TenantID)
	assert.Equal(t, "test-agent", result.Name)
	assert.Equal(t, "host-1.example.com", result.Host)
	assert.Equal(t, "claude", result.Backend)
	assert.Equal(t, "coding", result.NodeType)
	assert.Contains(t, result.Status, "ONLINE")
	assert.Equal(t, "/workspace", result.WorkDir)
	assert.Equal(t, now.AsTime().Unix(), result.LastSeen)
	assert.Equal(t, registered.AsTime().Unix(), result.RegisteredAt)
	assert.Equal(t, uint64(3), result.Version)
	assert.Equal(t, "us-east-1", result.Metadata["region"])
}

// =============================================================================
// Proto-to-SDK Conversion: healthReportFromProto
// =============================================================================

func TestHealthReportFromProto_NilReport(t *testing.T) {
	result := healthReportFromProto(nil)
	assert.Nil(t, result)
}

func TestHealthReportFromProto_AllHealthy(t *testing.T) {
	now := timestamppb.Now()
	report := &pb.HealthCheckReport{
		Timestamp:     now,
		TotalSessions: 3,
		Healthy:       3,
		Unhealthy:     0,
		Results: []*pb.SessionHealthResult{
			{SessionId: "s1", Backend: "claude", Healthy: true},
			{SessionId: "s2", Backend: "gemini", Healthy: true},
		},
	}

	result := healthReportFromProto(report)
	require.NotNil(t, result)
	assert.True(t, result.Healthy)
	assert.Equal(t, "healthy", result.Status)
	assert.Equal(t, now.AsTime().Unix(), result.Timestamp)
	assert.Equal(t, "healthy", result.Components["s1"])
	assert.Equal(t, "healthy", result.Components["s2"])
}

func TestHealthReportFromProto_WithUnhealthy(t *testing.T) {
	now := timestamppb.Now()
	report := &pb.HealthCheckReport{
		Timestamp:     now,
		TotalSessions: 2,
		Healthy:       1,
		Unhealthy:     1,
		Results: []*pb.SessionHealthResult{
			{SessionId: "s1", Backend: "claude", Healthy: true},
			{SessionId: "s2", Backend: "gemini", Healthy: false, Error: "connection refused"},
		},
	}

	result := healthReportFromProto(report)
	require.NotNil(t, result)
	assert.False(t, result.Healthy)
	assert.Equal(t, "degraded", result.Status)
	assert.Equal(t, "healthy", result.Components["s1"])
	assert.Equal(t, "connection refused", result.Components["s2"])
}

func TestHealthReportFromProto_UnhealthyNoError(t *testing.T) {
	report := &pb.HealthCheckReport{
		TotalSessions: 1,
		Unhealthy:     1,
		Results: []*pb.SessionHealthResult{
			{SessionId: "s1", Healthy: false},
		},
	}

	result := healthReportFromProto(report)
	require.NotNil(t, result)
	assert.False(t, result.Healthy)
	assert.Equal(t, "unhealthy", result.Components["s1"])
}

// =============================================================================
// Utility Functions
// =============================================================================

func TestTimestampToUnix_NilTimestamp(t *testing.T) {
	result := timestampToUnix(nil)
	assert.Equal(t, int64(0), result)
}

func TestTimestampToUnix_ValidTimestamp(t *testing.T) {
	ts := timestamppb.New(time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC))
	result := timestampToUnix(ts)
	assert.Equal(t, time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC).Unix(), result)
}

func TestUnixToTimestamp_Zero(t *testing.T) {
	result := unixToTimestamp(0)
	assert.Nil(t, result)
}

func TestUnixToTimestamp_ValidUnix(t *testing.T) {
	unix := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC).Unix()
	result := unixToTimestamp(unix)
	require.NotNil(t, result)
	assert.Equal(t, unix, result.AsTime().Unix())
}

func TestTaskStatusToProtoEnum(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"pending", "TASK_STATUS_PENDING"},
		{"completed", "TASK_STATUS_COMPLETED"},
		{"in progress", "TASK_STATUS_IN_PROGRESS"},
		{"CLAIMED", "TASK_STATUS_CLAIMED"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, taskStatusToProtoEnum(tc.input))
		})
	}
}

func TestAgentStatusToProtoEnum(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"online", "AGENT_STATUS_ONLINE"},
		{"offline", "AGENT_STATUS_OFFLINE"},
		{"BUSY", "AGENT_STATUS_BUSY"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, fleetNodeStatusToProtoEnum(tc.input))
		})
	}
}

func TestHealthStatusString(t *testing.T) {
	assert.Equal(t, "healthy", healthStatusString(0))
	assert.Equal(t, "degraded", healthStatusString(1))
	assert.Equal(t, "degraded", healthStatusString(5))
}

// =============================================================================
// Map dispatch completeness
// =============================================================================

func TestOrchestratorMethodMap_NoNilHandlers(t *testing.T) {
	for method, handler := range orchestratorMethodMap {
		t.Run(method, func(t *testing.T) {
			assert.NotNil(t, handler, "handler for %s should not be nil", method)
		})
	}
}

// =============================================================================
// SDK request type conversion validation
// =============================================================================

func TestOrchestratorSpawnSession_RequestFieldMapping(t *testing.T) {
	// Verify the handler rejects nil typed assertions correctly.
	// A real gRPC call cannot be tested without a server, so we
	// validate type assertion and error path coverage.
	var wrongType *orchestrator.PromptSessionRequest
	_, err := handleOrchestratorSpawnSession(context.Background(), nil, wrongType)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestOrchestratorRegisterAgent_RequestFieldMapping(t *testing.T) {
	var wrongType *orchestrator.ClaimTaskRequest
	_, err := handleFleetRegister(context.Background(), nil, wrongType)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}
