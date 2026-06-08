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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// protoTaskStatusToService / serviceTaskStatusToProto
// =============================================================================

func TestProtoTaskStatusToService(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.TaskStatus
		expected orchestration.TaskStatus
	}{
		{"pending", pb.TaskStatus_TASK_STATUS_PENDING, orchestration.TaskStatusPending},
		{"claimed", pb.TaskStatus_TASK_STATUS_CLAIMED, orchestration.TaskStatusClaimed},
		{"in_progress", pb.TaskStatus_TASK_STATUS_IN_PROGRESS, orchestration.TaskStatusInProgress},
		{"completed", pb.TaskStatus_TASK_STATUS_COMPLETED, orchestration.TaskStatusCompleted},
		{"failed", pb.TaskStatus_TASK_STATUS_FAILED, orchestration.TaskStatusFailed},
		{"cancelled", pb.TaskStatus_TASK_STATUS_CANCELLED, orchestration.TaskStatusCancelled},
		{"unspecified_empty", pb.TaskStatus_TASK_STATUS_UNSPECIFIED, orchestration.TaskStatus("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, protoTaskStatusToService(tt.input))
		})
	}
}

func TestServiceTaskStatusToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    orchestration.TaskStatus
		expected pb.TaskStatus
	}{
		{"pending", orchestration.TaskStatusPending, pb.TaskStatus_TASK_STATUS_PENDING},
		{"claimed", orchestration.TaskStatusClaimed, pb.TaskStatus_TASK_STATUS_CLAIMED},
		{"in_progress", orchestration.TaskStatusInProgress, pb.TaskStatus_TASK_STATUS_IN_PROGRESS},
		{"completed", orchestration.TaskStatusCompleted, pb.TaskStatus_TASK_STATUS_COMPLETED},
		{"failed", orchestration.TaskStatusFailed, pb.TaskStatus_TASK_STATUS_FAILED},
		{"cancelled", orchestration.TaskStatusCancelled, pb.TaskStatus_TASK_STATUS_CANCELLED},
		{"unknown_unspecified", orchestration.TaskStatus("bogus"), pb.TaskStatus_TASK_STATUS_UNSPECIFIED},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, serviceTaskStatusToProto(tt.input))
		})
	}
}

// =============================================================================
// protoAgentStatusToService / serviceAgentStatusToProto
// =============================================================================

func TestProtoAgentStatusToService(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.AgentStatus
		expected orchestration.AgentStatus
	}{
		{"online_running", pb.AgentStatus_AGENT_STATUS_ONLINE, orchestration.AgentStatusRunning},
		{"offline", pb.AgentStatus_AGENT_STATUS_OFFLINE, orchestration.AgentStatusOffline},
		{"busy_running", pb.AgentStatus_AGENT_STATUS_BUSY, orchestration.AgentStatusRunning},
		{"error", pb.AgentStatus_AGENT_STATUS_ERROR, orchestration.AgentStatusError},
		{"draining_idle", pb.AgentStatus_AGENT_STATUS_DRAINING, orchestration.AgentStatusIdle},
		{"unspecified_empty", pb.AgentStatus_AGENT_STATUS_UNSPECIFIED, orchestration.AgentStatus("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, protoAgentStatusToService(tt.input))
		})
	}
}

func TestServiceAgentStatusToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    orchestration.AgentStatus
		expected pb.AgentStatus
	}{
		{"registered_online", orchestration.AgentStatusRegistered, pb.AgentStatus_AGENT_STATUS_ONLINE},
		{"running_busy", orchestration.AgentStatusRunning, pb.AgentStatus_AGENT_STATUS_BUSY},
		{"idle_online", orchestration.AgentStatusIdle, pb.AgentStatus_AGENT_STATUS_ONLINE},
		{"offline", orchestration.AgentStatusOffline, pb.AgentStatus_AGENT_STATUS_OFFLINE},
		{"error", orchestration.AgentStatusError, pb.AgentStatus_AGENT_STATUS_ERROR},
		{"unknown_unspecified", orchestration.AgentStatus("bogus"), pb.AgentStatus_AGENT_STATUS_UNSPECIFIED},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, serviceAgentStatusToProto(tt.input))
		})
	}
}

// =============================================================================
// serviceSessionStateToProto
// =============================================================================

func TestServiceSessionStateToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    orchestration.SessionState
		expected pb.SessionState
	}{
		{"active", orchestration.SessionStateActive, pb.SessionState_SESSION_STATE_ACTIVE},
		{"idle", orchestration.SessionStateIdle, pb.SessionState_SESSION_STATE_IDLE},
		{"closed", orchestration.SessionStateClosed, pb.SessionState_SESSION_STATE_CLOSED},
		{"error", orchestration.SessionStateError, pb.SessionState_SESSION_STATE_ERROR},
		{"unknown_unspecified", orchestration.SessionState("bogus"), pb.SessionState_SESSION_STATE_UNSPECIFIED},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, serviceSessionStateToProto(tt.input))
		})
	}
}

// =============================================================================
// protoMCPServersToService
// =============================================================================

func TestProtoMCPServersToService(t *testing.T) {
	t.Run("empty_returns_nil", func(t *testing.T) {
		assert.Nil(t, protoMCPServersToService(nil))
		assert.Nil(t, protoMCPServersToService([]*pb.MCPServerConfig{}))
	})

	t.Run("maps_name_and_url_to_command", func(t *testing.T) {
		out := protoMCPServersToService([]*pb.MCPServerConfig{
			{Name: "fs", Url: "stdio://fs"},
			{Name: "git", Url: "stdio://git"},
		})
		require.Len(t, out, 2)
		assert.Equal(t, "fs", out[0].Name)
		assert.Equal(t, "stdio://fs", out[0].Command)
		assert.Equal(t, "git", out[1].Name)
		assert.Equal(t, "stdio://git", out[1].Command)
	})
}

// =============================================================================
// serviceSessionInfoToProto
// =============================================================================

func TestServiceSessionInfoToProto(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		assert.Nil(t, serviceSessionInfoToProto(nil))
	})

	t.Run("full_mapping", func(t *testing.T) {
		out := serviceSessionInfoToProto(&orchestration.SessionInfo{
			SessionID:    "sess-1",
			ProjectID:    "proj-1",
			Backend:      "acp",
			Model:        "claude",
			State:        orchestration.SessionStateActive,
			CreatedAt:    1700000000,
			LastActivity: 1700000100,
		})
		require.NotNil(t, out)
		assert.Equal(t, "sess-1", out.SessionId)
		assert.Equal(t, "proj-1", out.ProjectId)
		assert.Equal(t, "acp", out.Backend)
		assert.Equal(t, "claude", out.Model)
		assert.Equal(t, pb.SessionState_SESSION_STATE_ACTIVE, out.State)
		assert.Equal(t, int64(1700000000), out.CreatedAt.AsTime().Unix())
		assert.Equal(t, int64(1700000100), out.LastActivity.AsTime().Unix())
	})
}
