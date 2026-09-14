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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

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

	// The session times are Unix NANOSECONDS on the domain type. Reading them
	// as seconds put the instant tens of billions of years out, past what a
	// protobuf timestamp can carry, so a gRPC caller got no creation time at
	// all — go-quicbaas#792.
	t.Run("full_mapping", func(t *testing.T) {
		out := serviceSessionInfoToProto(&orchestration.SessionInfo{
			SessionID:    "sess-1",
			ProjectID:    "proj-1",
			Backend:      "acp",
			Model:        "claude",
			State:        orchestration.SessionStateActive,
			CreatedAt:    1700000000_000000000,
			LastActivity: 1700000100_000000000,
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

	// An unset instant stays unset: "never active" and "active at the epoch"
	// are different claims, and rendering the first as the second is how a
	// fresh session looked six decades old.
	t.Run("unset_instants_stay_unset", func(t *testing.T) {
		out := serviceSessionInfoToProto(&orchestration.SessionInfo{SessionID: "sess-2"})
		require.NotNil(t, out)
		assert.Nil(t, out.CreatedAt)
		assert.Nil(t, out.LastActivity)
	})
}

func TestUnixNanoTimestamp(t *testing.T) {
	t.Run("nanoseconds_round_trip", func(t *testing.T) {
		ts := unixNanoTimestamp(1700000000_123456789)
		require.NotNil(t, ts)
		assert.Equal(t, int64(1700000000), ts.AsTime().Unix())
		assert.Equal(t, 123456789, ts.AsTime().Nanosecond())
	})

	t.Run("non_positive_is_nil", func(t *testing.T) {
		assert.Nil(t, unixNanoTimestamp(0))
		assert.Nil(t, unixNanoTimestamp(-1))
	})
}

// =============================================================================
// Session response timestamps are nanoseconds on every RPC, not just the list
// =============================================================================
//
// Every orchestration session response stamps `time.Now().UnixNano()`, and the
// gRPC layer converted each one with `time.Unix(value, 0)` — reading
// nanoseconds as seconds, which lands tens of billions of years out, past what
// a protobuf timestamp can carry. Fixing only the `SessionInfo` entries left
// spawn, prompt, status, close and the list's own envelope still wrong, so the
// whole session surface is pinned here. go-quicbaas#792 review, #818.
func TestOrchestratorServer_SessionResponseTimestampsAreNanoseconds(t *testing.T) {
	const nanos int64 = 1700000000_123456789
	const wantSec int64 = 1700000000

	svc := &grpcTestAgentService{
		spawnSessionFunc: func(context.Context, *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			return &orchestration.SpawnSessionResponse{SessionID: "s-1", Timestamp: nanos}, nil
		},
		promptSessionFunc: func(context.Context, *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
			return &orchestration.PromptSessionResponse{SessionID: "s-1", Timestamp: nanos}, nil
		},
		sessionStatusFunc: func(context.Context, *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
			return &orchestration.SessionStatusResponse{
				SessionID: "s-1", State: orchestration.SessionStateActive,
				LastActivity: nanos, Timestamp: nanos,
			}, nil
		},
		closeSessionFunc: func(context.Context, *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
			return &orchestration.CloseSessionResponse{SessionID: "s-1", Timestamp: nanos}, nil
		},
		listSessionsFunc: func(context.Context, *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
			return &orchestration.ListSessionsResponse{Timestamp: nanos}, nil
		},
	}
	server := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	spawn, err := server.SpawnSession(ctx, &pb.SpawnSessionRequest{Backend: "claude", TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, wantSec, spawn.Timestamp.AsTime().Unix(), "SpawnSession timestamp")

	// No SessionId: the SYNCHRONOUS branch, which returns the completed turn
	// inline. A prompt that names a session takes the async ack path instead,
	// covered below.
	prompt, err := server.PromptSession(ctx, &pb.PromptSessionRequest{Backend: "claude", Prompt: "hi", TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, wantSec, prompt.Timestamp.AsTime().Unix(), "PromptSession timestamp")

	st, err := server.SessionStatus(ctx, &pb.SessionStatusRequest{SessionId: "s-1", TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, wantSec, st.Timestamp.AsTime().Unix(), "SessionStatus timestamp")
	assert.Equal(t, wantSec, st.LastActivity.AsTime().Unix(), "SessionStatus last_activity")

	closed, err := server.CloseSession(ctx, &pb.CloseSessionRequest{SessionId: "s-1", TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, wantSec, closed.Timestamp.AsTime().Unix(), "CloseSession timestamp")

	list, err := server.ListSessions(ctx, &pb.ListSessionsRequest{TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, wantSec, list.Timestamp.AsTime().Unix(), "ListSessions timestamp")
}

// TestOrchestratorServer_SessionResponseTimestampsUnsetStayUnset is the bad-input
// half: a response that carries no instant must not claim one. Rendering an
// unset value as the epoch is how a fresh session looked six decades old.
func TestOrchestratorServer_SessionResponseTimestampsUnsetStayUnset(t *testing.T) {
	svc := &grpcTestAgentService{
		spawnSessionFunc: func(context.Context, *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			return &orchestration.SpawnSessionResponse{SessionID: "s-1"}, nil
		},
		sessionStatusFunc: func(context.Context, *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
			return &orchestration.SessionStatusResponse{SessionID: "s-1", State: orchestration.SessionStateActive}, nil
		},
	}
	server := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	spawn, err := server.SpawnSession(ctx, &pb.SpawnSessionRequest{Backend: "claude", TenantId: "t1"})
	require.NoError(t, err)
	assert.Nil(t, spawn.Timestamp, "an unset spawn instant must not become the epoch")

	st, err := server.SessionStatus(ctx, &pb.SessionStatusRequest{SessionId: "s-1", TenantId: "t1"})
	require.NoError(t, err)
	assert.Nil(t, st.Timestamp, "an unset status instant must not become the epoch")
	assert.Nil(t, st.LastActivity, "a session that has never been active must not report 1970")
}

// TestOrchestratorServer_ListSessionsForwardsTheStatusFilter is the gRPC half
// of the same contract as the REST test: the two transports must narrow the
// listing identically, or an operator's answer depends on which one they
// happened to use. go-quicbaas#792.
func TestOrchestratorServer_ListSessionsForwardsTheStatusFilter(t *testing.T) {
	var captured *orchestration.ListSessionsRequest
	svc := &grpcTestAgentService{
		listSessionsFunc: func(_ context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
			captured = req
			return &orchestration.ListSessionsResponse{}, nil
		},
	}
	server := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	_, err := server.ListSessions(context.Background(), &pb.ListSessionsRequest{
		TenantId: "t1", Backend: "claude", Status: "active",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, orchestration.SessionStateActive, captured.Status)
	assert.Equal(t, "claude", captured.Backend)

	// Omitted means every state, exactly as over REST.
	_, err = server.ListSessions(context.Background(), &pb.ListSessionsRequest{TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, orchestration.SessionState(""), captured.Status)
}

// TestOrchestratorServer_UnknownListStatusIsABadRequest holds the classifier
// honest about whose fault a misspelled filter is.
//
// The service refuses an unknown state with ErrSessionStateUnknown wrapped in
// an AgentServiceError, and the AgentServiceError branch of the converter maps
// to Internal. Sentinel-first ordering is therefore load-bearing: without it a
// caller's typo came back as a server fault, which tells an operator to go
// looking at the daemon and tells a client to retry something that can never
// succeed.
func TestOrchestratorServer_UnknownListStatusIsABadRequest(t *testing.T) {
	svc := &grpcTestAgentService{
		listSessionsFunc: func(_ context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
			return nil, &orchestration.AgentServiceError{
				Sentinel: orchestration.ErrSessionStateUnknown,
				Message:  string(req.Status) + "; known states are active, closed, error, idle, resumable",
			}
		},
	}
	server := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	_, err := server.ListSessions(context.Background(), &pb.ListSessionsRequest{
		TenantId: "t1", Status: "actve",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"a state the daemon does not use is the caller's mistake, not a server fault")
	assert.Contains(t, err.Error(), "known states are",
		"the refusal must name the states that do exist, or the caller cannot fix it")
}

// TestOrchestratorServer_AgentServiceFailureIsStillInternal is the other side
// of the same ordering: adding the sentinel check above the AgentServiceError
// branch must not reclassify a genuine service failure as a bad request.
func TestOrchestratorServer_AgentServiceFailureIsStillInternal(t *testing.T) {
	svc := &grpcTestAgentService{
		listSessionsFunc: func(context.Context, *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
			return nil, &orchestration.AgentServiceError{
				Sentinel: orchestration.ErrTurnNotAccepted,
				Message:  "the session store fell over",
			}
		},
	}
	server := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	_, err := server.ListSessions(context.Background(), &pb.ListSessionsRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// TestOrchestratorServer_ListSessionsWithNoTenantDoesNotSpanTenants is a
// cross-tenant exposure, not a tidiness point.
//
// AgentService.ListSessions reads an EMPTY tenant as the embedded
// single-tenant case and answers with ListAllSessions — every tenant's
// sessions. Passing the proto field straight through therefore turned an
// omitted tenant_id on a REMOTE gRPC call into a tenant-wide enumeration,
// while the identical omission over REST resolved to the caller's own tenant
// and then to the system tenant. The wire surface now resolves the same way.
func TestOrchestratorServer_ListSessionsWithNoTenantDoesNotSpanTenants(t *testing.T) {
	var captured string
	svc := &grpcTestAgentService{
		listSessionsFunc: func(ctx context.Context, _ *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
			captured = orchestration.TenantFromContext(ctx)
			return &orchestration.ListSessionsResponse{}, nil
		},
	}
	server := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	_, err := server.ListSessions(context.Background(), &pb.ListSessionsRequest{})
	require.NoError(t, err)
	assert.NotEmpty(t, captured,
		"an omitted tenant_id reached the service as the cross-tenant mode, which lists every tenant")
	assert.Equal(t, qrdbsdk.SystemTenantID, captured,
		"an unauthenticated caller that names no tenant gets the system tenant, as over REST")
}

// TestOrchestratorServer_ListSessionsHonoursTheExplicitTenant is the converse:
// resolving a missing tenant must not start overriding one the caller supplied.
func TestOrchestratorServer_ListSessionsHonoursTheExplicitTenant(t *testing.T) {
	var captured string
	svc := &grpcTestAgentService{
		listSessionsFunc: func(ctx context.Context, _ *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
			captured = orchestration.TenantFromContext(ctx)
			return &orchestration.ListSessionsResponse{}, nil
		},
	}
	server := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	_, err := server.ListSessions(context.Background(), &pb.ListSessionsRequest{TenantId: "tenant-a"})
	require.NoError(t, err)
	assert.Equal(t, "tenant-a", captured)
}

// =============================================================================
// One unit for every instant the orchestrator converts (go-quicbaas#855)
// =============================================================================
//
// The session converter was fixed for this in #792 and its neighbours were
// not, so the same file carried both readings — serviceTaskToProto decoded
// Task.SpawnedAt with time.Unix(0, nanos) on one line and Task.CreatedAt with
// time.Unix(seconds, 0) four lines above it. The store writes every one of
// these from time.Now().UnixNano(), so the seconds reading put the instant
// tens of billions of years out and a protobuf timestamp cannot carry it: a
// gRPC caller got a task with no creation time rather than a wrong one.
//
// These tests pin the contract per converter. Each asserts the instant, not
// merely that a timestamp was produced, because a wrong instant is what the
// defect looked like.

func TestServiceTaskToProtoInstantsAreNanoseconds(t *testing.T) {
	// One moment, expressed the way the task store expresses it.
	const nanos = int64(1700000000_123456789)
	const seconds = int64(1700000000)

	t.Run("every_instant_reads_back_as_the_same_moment", func(t *testing.T) {
		out := serviceTaskToProto(&orchestration.Task{
			ID:          "task-1",
			TenantID:    "tenant-a",
			Subject:     "Add the widget",
			Status:      orchestration.TaskStatusCompleted,
			CreatedAt:   nanos,
			UpdatedAt:   nanos,
			ClaimedAt:   nanos,
			CompletedAt: nanos,
			SpawnedAt:   nanos,
		})
		require.NotNil(t, out)
		require.NotNil(t, out.CreatedAt)
		require.NotNil(t, out.UpdatedAt)
		require.NotNil(t, out.ClaimedAt)
		require.NotNil(t, out.CompletedAt)
		require.NotNil(t, out.SpawnedAt)

		assert.Equal(t, seconds, out.CreatedAt.AsTime().Unix())
		assert.Equal(t, seconds, out.UpdatedAt.AsTime().Unix())
		assert.Equal(t, seconds, out.ClaimedAt.AsTime().Unix())
		assert.Equal(t, seconds, out.CompletedAt.AsTime().Unix())

		// SpawnedAt was already correct; it is asserted so the two readings
		// can never drift apart again without a test saying so.
		assert.Equal(t, seconds, out.SpawnedAt.AsTime().Unix())
		assert.Equal(t, out.SpawnedAt.AsTime(), out.CreatedAt.AsTime(),
			"two instants of one task written from one clock must convert alike")
	})

	// An absent instant must stay absent. "Never claimed" and "claimed at the
	// epoch" are different claims about the task.
	t.Run("unset_instants_stay_unset", func(t *testing.T) {
		out := serviceTaskToProto(&orchestration.Task{ID: "task-2", TenantID: "tenant-a"})
		require.NotNil(t, out)
		assert.Nil(t, out.CreatedAt)
		assert.Nil(t, out.ClaimedAt)
		assert.Nil(t, out.CompletedAt)
		assert.Nil(t, out.SpawnedAt)
	})
}

func TestServiceAgentRegistrationToProtoInstantsAreNanoseconds(t *testing.T) {
	const nanos = int64(1700000000_123456789)

	t.Run("last_seen_and_registered_at_read_back_as_the_same_moment", func(t *testing.T) {
		out := serviceAgentRegistrationToProto(&orchestration.AgentRegistration{
			ID:           "agent-1",
			TenantID:     "tenant-a",
			Name:         "vm-host-4",
			Backend:      "acp",
			LastSeen:     nanos,
			RegisteredAt: nanos,
		})
		require.NotNil(t, out)
		require.NotNil(t, out.LastSeen)
		require.NotNil(t, out.RegisteredAt)
		assert.Equal(t, int64(1700000000), out.LastSeen.AsTime().Unix())
		assert.Equal(t, int64(1700000000), out.RegisteredAt.AsTime().Unix())
	})

	// An agent registered but never seen is not an agent last seen in 1970:
	// the heartbeat view reads this to decide whether a daemon is alive.
	t.Run("unset_instants_stay_unset", func(t *testing.T) {
		out := serviceAgentRegistrationToProto(&orchestration.AgentRegistration{ID: "agent-2"})
		require.NotNil(t, out)
		assert.Nil(t, out.LastSeen)
		assert.Nil(t, out.RegisteredAt)
	})
}

func TestServiceHealthReportToProtoTimestampIsNanoseconds(t *testing.T) {
	const nanos = int64(1700000000_123456789)

	t.Run("timestamp_reads_back_as_the_same_moment", func(t *testing.T) {
		out := serviceHealthReportToProto(&orchestration.HealthCheckReport{
			Timestamp:     nanos,
			TotalSessions: 2,
			Healthy:       1,
			Unhealthy:     1,
		})
		require.NotNil(t, out)
		require.NotNil(t, out.Timestamp)
		assert.Equal(t, int64(1700000000), out.Timestamp.AsTime().Unix())
	})

	t.Run("unset_timestamp_stays_unset", func(t *testing.T) {
		out := serviceHealthReportToProto(&orchestration.HealthCheckReport{})
		require.NotNil(t, out)
		assert.Nil(t, out.Timestamp)
	})
}
