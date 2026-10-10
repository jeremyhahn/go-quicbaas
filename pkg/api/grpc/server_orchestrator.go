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
	stderrors "errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// OrchestratorServer wraps the Agent orchestration service for gRPC endpoints.
type OrchestratorServer struct {
	pb.UnimplementedOrchestratorServiceServer
	agentSvc orchestration.AgentService
	logger   *logger.Logger
}

// NewOrchestratorServer creates a new gRPC server for orchestrator operations.
func NewOrchestratorServer(agentSvc orchestration.AgentService, log *logger.Logger) *OrchestratorServer {
	return &OrchestratorServer{
		agentSvc: agentSvc,
		logger:   log.WithField("component", "orchestrator-grpc-server"),
	}
}

// =============================================================================
// Session Operations
// =============================================================================

// SpawnSession creates a new agent session.
func (s *OrchestratorServer) SpawnSession(ctx context.Context, req *pb.SpawnSessionRequest) (*pb.SpawnSessionResponse, error) {
	s.logger.WithField("backend", req.Backend).Debug("Processing SpawnSession request")

	if req.Backend == "" && req.GetAgentId() == "" && req.GetPoolId() == "" {
		return nil, status.Error(codes.InvalidArgument, "an agent, a pool or a backend is required")
	}

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.SpawnSessionRequest{
		AgentID:            req.GetAgentId(),
		PoolID:             req.GetPoolId(),
		Role:               orchestration.AgentRole(req.GetRole()),
		Backend:            req.Backend,
		ProjectID:          req.ProjectId,
		Model:              req.Model,
		SystemPrompt:       req.SystemPrompt,
		PromptAppend:       req.GetPromptAppend(),
		MCPServers:         protoMCPServersToService(req.McpServers),
		Env:                req.Env,
		WorkDir:            req.WorkDir,
		SandboxMode:        orchestration.SandboxMode(req.SandboxMode),
		Sandboxed:          req.Sandboxed,
		ContainerImage:     req.ContainerImage,
		ContainerConfig:    protoContainerSpecToService(req.ContainerConfig),
		HostAffinity:       req.HostAffinity,
		HostAffinityStrict: req.HostAffinityStrict,
		// extra_args is DELIBERATELY NOT MAPPED HERE. The field carries argv
		// for the agent program, and this is the surface an external client
		// reaches: forwarding it would let a caller add any flag the CLI takes
		// — including the permission grants the SDLC module exists to decide. It is
		// carried on the internal control-plane-to-daemon spawn instead
		// (pkg/fleet/spawn_server.go), where the sender is the SDLC module itself.

		// Without this the gRPC transport silently spawns a one-shot session:
		// the backend runs a fresh process per prompt, the agent keeps no
		// conversation context, and the session is not a resume candidate
		// after a daemon restart. REST and MCP already carry the flag.
		Persistent: req.Persistent,

		// Pooled provider account selection. AccountAuto is what makes the
		// dynamic (least-usage) strategy requestable at all, so dropping it
		// here would leave the strategy unreachable from outside the process.
		AccountID:       req.AccountId,
		AccountAuto:     req.AccountAuto,
		AccountStrategy: req.AccountStrategy,
		AccountAllow:    req.AccountAllow,
	}

	// Mirror the REST handler: thread strict on context so the
	// RemoteBackendResolver can stash a typed NoMatchingFleetNodeError when the
	// host is offline.
	if req.HostAffinityStrict {
		tenantCtx = orchestration.WithHostAffinityStrict(tenantCtx)
	}

	resp, err := s.agentSvc.SpawnSession(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.SpawnSessionResponse{
		SessionId:       resp.SessionID,
		Backend:         resp.Backend,
		Model:           resp.Model,
		ContainerId:     resp.ContainerID,
		Timestamp:       unixNanoTimestamp(resp.Timestamp),
		ResolvedAgentId: resp.ResolvedAgentID,
		Failovers:       agentAttemptsToProto(resp.Failovers),
	}, nil
}

// PromptSession sends a prompt to an agent session.
//
// A prompt naming a session is dispatched asynchronously: the orchestrator
// records the prompt and a turn_started marker on the session event log, runs
// the turn on a context detached from this RPC, and answers immediately with
// the turn ID. The agent's output streams over SubscribeSessionEvents and the
// turn ends with a turn_completed or turn_failed event carrying that ID. A
// real agent turn runs for minutes, which no unary RPC deadline accommodates.
//
// A prompt with no session is a stateless one-shot against the named backend.
// It has no event log to publish to, so it stays synchronous and returns the
// completed turn inline.
func (s *OrchestratorServer) PromptSession(ctx context.Context, req *pb.PromptSessionRequest) (*pb.PromptSessionResponse, error) {
	s.logger.Debug("Processing PromptSession request")

	if req.Prompt == "" {
		return nil, status.Error(codes.InvalidArgument, "prompt is required")
	}

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.PromptSessionRequest{
		Backend:      req.Backend,
		SessionID:    req.SessionId,
		ProjectID:    req.ProjectId,
		Prompt:       req.Prompt,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
		UseRAG:       req.UseRag,
	}

	if req.SessionId != "" {
		ack, ackErr := s.agentSvc.PromptSessionAsync(tenantCtx, svcReq)
		if ackErr != nil {
			return nil, convertOrchestratorError(ackErr)
		}
		return &pb.PromptSessionResponse{
			SessionId: ack.SessionID,
			TurnId:    ack.TurnID,
			Status:    ack.Status,
			Sequence:  ack.Sequence,
			Model:     ack.Model,
			Timestamp: unixNanoTimestamp(ack.Timestamp),
		}, nil
	}

	resp, err := s.agentSvc.PromptSession(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.PromptSessionResponse{
		SessionId:  resp.SessionID,
		Content:    serviceContentBlocksToProto(resp.Content),
		Model:      resp.Model,
		StopReason: resp.StopReason,
		Timestamp:  unixNanoTimestamp(resp.Timestamp),
	}, nil
}

// SessionStatus returns the status of an active session.
func (s *OrchestratorServer) SessionStatus(ctx context.Context, req *pb.SessionStatusRequest) (*pb.SessionStatusResponse, error) {
	s.logger.WithField("session_id", req.SessionId).Debug("Processing SessionStatus request")

	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.SessionStatusRequest{
		SessionID: req.SessionId,
	}

	resp, err := s.agentSvc.SessionStatus(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.SessionStatusResponse{
		SessionId:     resp.SessionID,
		State:         serviceSessionStateToProto(resp.State),
		LastActivity:  unixNanoTimestamp(resp.LastActivity),
		UptimeSeconds: resp.Uptime,
		Timestamp:     unixNanoTimestamp(resp.Timestamp),
	}, nil
}

// CloseSession terminates an active session.
func (s *OrchestratorServer) CloseSession(ctx context.Context, req *pb.CloseSessionRequest) (*pb.CloseSessionResponse, error) {
	s.logger.WithField("session_id", req.SessionId).Debug("Processing CloseSession request")

	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.CloseSessionRequest{
		SessionID: req.SessionId,
	}

	resp, err := s.agentSvc.CloseSession(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.CloseSessionResponse{
		SessionId: resp.SessionID,
		Timestamp: unixNanoTimestamp(resp.Timestamp),
	}, nil
}

// RotateSessionAccount moves a session onto a different pooled provider
// account.
//
// The same verb runs automatically when a turn fails on a classified provider
// limit; this is the operator's door to it — "this seat is bad, move my session
// now". It authorizes as orchestrator:spawn rather than orchestrator:close or
// :status: the resume path literally closes the agent process and reconstitutes
// it against a newly selected pooled credential, which is the authority a spawn
// carries and no weaker verb does.
func (s *OrchestratorServer) RotateSessionAccount(ctx context.Context, req *pb.RotateSessionAccountRequest) (*pb.RotateSessionAccountResponse, error) {
	s.logger.WithField("session_id", req.GetSessionId()).Debug("Processing RotateSessionAccount request")

	if req.GetSessionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	rotator, err := orchestration.RotationServiceFrom(s.agentSvc, "grpc")
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	result, err := rotator.RotateSessionAccount(tenantCtx, &orchestration.RotateSessionAccountRequest{
		SessionID:    req.GetSessionId(),
		Reason:       req.GetReason(),
		HoldOutgoing: req.GetHoldOutgoing(),
		AvailableAt:  timeFromUnixSeconds(req.GetAvailableAtUnix()),
	})
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.RotateSessionAccountResponse{
		SessionId:         result.SessionID,
		PreviousAccountId: result.PreviousAccountID,
		AccountId:         result.AccountID,
		Strategy:          result.Strategy,
		Mode:              string(result.Mode),
		Timestamp:         timestamppb.Now(),
	}, nil
}

// timeFromUnixSeconds converts a Unix-seconds wire value to a time.Time,
// mapping zero to the zero time so "unset" survives the round trip rather than
// arriving as 1970.
func timeFromUnixSeconds(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// orchestratorTenant resolves the tenant an orchestrator RPC acts for and
// binds it under both keys the request reaches: go-qrdb's central key
// (tenantctx) and the orchestrator's own (orchestration.ContextWithTenant),
// which its tenant guard reads.
//
// THE TENANT_ID FIELD IS NOT TRUSTED. It is honoured only as tenantctx.Target
// allows: a caller may name only its own tenant (the system tenant gets no
// cross-tenant lift through it), and every other naming is refused with PermissionDenied before the
// orchestrator is reached (go-quicbaas#1150). An omitted field means the
// caller's own tenant — never "every tenant": AgentService.ListSessions reads
// an empty tenant as the embedded single-tenant case and answers with every
// tenant's sessions. A call with no tenant bound is refused rather than
// trusted to name one.
func orchestratorTenant(ctx context.Context, requested string) (context.Context, string, error) {
	tenantCtx, tenantID, err := tenantctx.ContextForTarget(ctx, "grpc.Orchestrator", requested)
	if err != nil {
		return ctx, "", toProtoError(err)
	}
	return orchestration.ContextWithTenant(tenantCtx, tenantID), tenantID, nil
}

// ListSessions returns every session, in any state, optionally filtered by
// backend or status.
func (s *OrchestratorServer) ListSessions(ctx context.Context, req *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	s.logger.Debug("Processing ListSessions request")

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	page, err := pageFromRPC(req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.ListSessionsRequest{
		Backend:   req.GetBackend(),
		Status:    orchestration.SessionState(req.GetStatus()),
		ProjectID: req.GetProjectId(),
		JobID:     req.GetJobId(),
		Runner:    req.GetRunner(),
		AgentID:   req.GetAgentId(),
		Page:      page,
	}

	resp, err := s.agentSvc.ListSessions(tenantCtx, svcReq)
	if err != nil {
		return nil, convertListError(err, convertOrchestratorError)
	}

	sessions := make([]*pb.SessionInfo, len(resp.Sessions))
	for i, sess := range resp.Sessions {
		sessions[i] = serviceSessionInfoToProto(sess)
	}

	return &pb.ListSessionsResponse{
		Sessions:   sessions,
		Timestamp:  unixNanoTimestamp(resp.Timestamp),
		Total:      pageInt32(resp.Total),
		Limit:      pageInt32(resp.Limit),
		Offset:     pageInt32(resp.Offset),
		NextOffset: pageInt32(resp.NextOffset),
	}, nil
}

// =============================================================================
// Task Operations
// =============================================================================

// CreateTask creates a new orchestration task.
func (s *OrchestratorServer) CreateTask(ctx context.Context, req *pb.CreateTaskRequest) (*pb.CreateTaskResponse, error) {
	s.logger.Debug("Processing CreateTask request")

	if req.Subject == "" {
		return nil, status.Error(codes.InvalidArgument, "task subject is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.CreateTaskRequest{
		TenantID:      tenantID,
		TeamName:      req.TeamName,
		Subject:       req.Subject,
		Description:   req.Description,
		AssignedTo:    req.AssignedTo,
		Dependencies:  req.Dependencies,
		Metadata:      req.Metadata,
		ProjectID:     req.GetProjectId(),
		AutoSpawn:     req.GetAutoSpawn(),
		Backend:       req.GetBackend(),
		GpuRequired:   req.GetGpuRequired(),
		HostAffinity:  req.GetHostAffinity(),
		Source:        protoTaskSourceToService(req.GetSource()),
		Priority:      int(req.GetPriority()),
		AttemptBudget: int(req.GetAttemptBudget()),
		Runner:        req.GetRunner(),
		PromptAppend:  req.GetPromptAppend(),
	}

	task, err := s.agentSvc.CreateTask(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.CreateTaskResponse{
		Task:      serviceTaskToProto(task),
		Timestamp: timestamppb.Now(),
	}, nil
}

// GetTask retrieves a task by ID.
func (s *OrchestratorServer) GetTask(ctx context.Context, req *pb.GetTaskRequest) (*pb.GetTaskResponse, error) {
	s.logger.WithField("task_id", req.TaskId).Debug("Processing GetTask request")

	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	task, err := s.agentSvc.GetTask(tenantCtx, tenantID, req.TaskId)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.GetTaskResponse{
		Task:      serviceTaskToProto(task),
		Timestamp: timestamppb.Now(),
	}, nil
}

// ListTasks lists tasks with optional filtering.
func (s *OrchestratorServer) ListTasks(ctx context.Context, req *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	s.logger.Debug("Processing ListTasks request")

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	filter := &orchestration.TaskFilter{}
	if req.Filter != nil {
		filter = &orchestration.TaskFilter{
			TeamName:   req.Filter.GetTeamName(),
			Status:     protoTaskStatusToService(req.Filter.GetStatus()),
			AssignedTo: req.Filter.GetAssignedTo(),
			CreatedBy:  req.Filter.GetCreatedBy(),
			Limit:      int(req.Filter.GetLimit()),
			ProjectID:  req.Filter.GetProjectId(),
			Statuses:   protoTaskStatusesToService(req.Filter.GetStatuses()),
			AutoSpawn:  req.Filter.AutoSpawn,
			AgentID:    req.Filter.GetAgentId(),
			JobID:      req.Filter.GetJobId(),
		}
	}

	page, err := pageFromRPC(req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ())
	if err != nil {
		return nil, err
	}

	result, err := s.agentSvc.ListTasksPage(tenantCtx, tenantID, filter, page)
	if err != nil {
		return nil, convertListError(err, convertOrchestratorError)
	}

	pbTasks := make([]*pb.Task, len(result.Items))
	for i, task := range result.Items {
		pbTasks[i] = serviceTaskToProto(task)
	}

	// total_count predates paging and has always meant "how many tasks
	// matched", so it carries total rather than the size of this page.
	return &pb.ListTasksResponse{
		Tasks:      pbTasks,
		TotalCount: pageInt32(result.Total),
		Timestamp:  timestamppb.Now(),
		Total:      pageInt32(result.Total),
		Limit:      pageInt32(result.Limit),
		Offset:     pageInt32(result.Offset),
		NextOffset: pageInt32(result.NextOffset),
	}, nil
}

// ClaimTask claims a task for an agent.
func (s *OrchestratorServer) ClaimTask(ctx context.Context, req *pb.ClaimTaskRequest) (*pb.ClaimTaskResponse, error) {
	s.logger.WithField("task_id", req.TaskId).Debug("Processing ClaimTask request")

	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	if err := s.agentSvc.ClaimTask(tenantCtx, tenantID, req.TaskId, req.AgentId); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.ClaimTaskResponse{
		TaskId:    req.TaskId,
		AgentId:   req.AgentId,
		Claimed:   true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// BlockTask parks a task until a person readmits it.
func (s *OrchestratorServer) BlockTask(ctx context.Context, req *pb.BlockTaskRequest) (*pb.BlockTaskResponse, error) {
	s.logger.WithField("task_id", req.GetTaskId()).Debug("Processing BlockTask request")

	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.GetKind() == "" {
		return nil, status.Error(codes.InvalidArgument, "kind is required")
	}
	if req.GetReason() == "" {
		return nil, status.Error(codes.InvalidArgument, "reason is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	if err := s.agentSvc.BlockTask(tenantCtx, tenantID, req.GetTaskId(), orchestration.TaskBlockKind(req.GetKind()), req.GetReason()); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.BlockTaskResponse{
		TaskId:    req.GetTaskId(),
		Blocked:   true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// ReadmitTask returns a blocked task to the pending queue.
func (s *OrchestratorServer) ReadmitTask(ctx context.Context, req *pb.ReadmitTaskRequest) (*pb.ReadmitTaskResponse, error) {
	s.logger.WithField("task_id", req.GetTaskId()).Debug("Processing ReadmitTask request")

	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	if err := s.agentSvc.ReadmitTask(tenantCtx, tenantID, req.GetTaskId()); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.ReadmitTaskResponse{
		TaskId:     req.GetTaskId(),
		Readmitted: true,
		Timestamp:  timestamppb.Now(),
	}, nil
}

// DeleteTask removes a task and releases its source reservation.
func (s *OrchestratorServer) DeleteTask(ctx context.Context, req *pb.DeleteTaskRequest) (*pb.DeleteTaskResponse, error) {
	s.logger.WithField("task_id", req.GetTaskId()).Debug("Processing DeleteTask request")

	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	if err := s.agentSvc.DeleteTask(tenantCtx, tenantID, req.GetTaskId()); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.DeleteTaskResponse{
		TaskId:    req.GetTaskId(),
		Deleted:   true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// CompleteTask marks a task as completed.
func (s *OrchestratorServer) CompleteTask(ctx context.Context, req *pb.CompleteTaskRequest) (*pb.CompleteTaskResponse, error) {
	s.logger.WithField("task_id", req.TaskId).Debug("Processing CompleteTask request")

	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	var result *orchestration.TaskResult
	if req.Result != nil {
		result = &orchestration.TaskResult{
			Output:   req.Result.Output,
			Files:    req.Result.Files,
			Metadata: req.Result.Metadata,
		}
	}

	if err := s.agentSvc.CompleteTask(tenantCtx, tenantID, req.TaskId, result); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.CompleteTaskResponse{
		TaskId:    req.TaskId,
		Completed: true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// DecomposeTask decomposes a task into structured work items using an agent.
func (s *OrchestratorServer) DecomposeTask(ctx context.Context, req *pb.DecomposeTaskRequest) (*pb.DecomposeTaskResponse, error) {
	s.logger.Debug("Processing DecomposeTask request")

	if req.Task == "" {
		return nil, status.Error(codes.InvalidArgument, "task description is required")
	}

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.DecomposeTaskRequest{
		Backend:    req.Backend,
		Model:      req.Model,
		Task:       req.Task,
		ProjectIDs: req.ProjectIds,
		UseRAG:     req.UseRag,
	}

	resp, err := s.agentSvc.DecomposeTask(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	pbWorkItems := make([]*pb.OrchestratorWorkItem, len(resp.WorkItems))
	for i, item := range resp.WorkItems {
		pbWorkItems[i] = &pb.OrchestratorWorkItem{
			Id:           item.ID,
			ProjectId:    item.ProjectID,
			Title:        item.Title,
			Description:  item.Description,
			Priority:     int32(item.Priority), //nolint:gosec // G115: bounded count
			Dependencies: item.Dependencies,
		}
	}

	return &pb.DecomposeTaskResponse{
		WorkItems: pbWorkItems,
		Timestamp: unixNanoTimestamp(resp.Timestamp),
	}, nil
}

// =============================================================================
// Job Operations
// =============================================================================

// StartJob starts an autonomous orchestration control loop.
func (s *OrchestratorServer) StartJob(ctx context.Context, req *pb.OrchestratorStartJobRequest) (*pb.OrchestratorStartJobResponse, error) {
	s.logger.Debug("Processing StartJob request")

	if req.Description == "" {
		return nil, status.Error(codes.InvalidArgument, "description is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.StartJobRequest{
		TenantID:      tenantID,
		Description:   req.Description,
		Lead:          AgentRefFromProto(req.GetLead()),
		LeadAgentID:   req.GetLeadAgentId(),
		ProjectID:     req.ProjectId,
		MaxIterations: int(req.MaxIterations),
		PollInterval:  req.PollInterval,
		WorkflowName:  req.GetWorkflowName(),
		WorkflowFile:  req.GetWorkflowFile(),
		Runner:        req.GetRunner(),
		TaskID:        req.GetTaskId(),
		Attempt:       int(req.GetAttempt()),
		PromptAppend:  req.GetPromptAppend(),
	}

	resp, err := s.agentSvc.StartJob(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.OrchestratorStartJobResponse{
		JobId:     resp.JobID,
		Timestamp: unixNanoTimestamp(resp.Timestamp),
	}, nil
}

// GetJobStatus returns the current status of an orchestration job.
func (s *OrchestratorServer) GetJobStatus(ctx context.Context, req *pb.OrchestratorGetJobStatusRequest) (*pb.OrchestratorGetJobStatusResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing GetJobStatus request")

	if req.JobId == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	jobStatus, err := s.agentSvc.GetJobStatus(tenantCtx, tenantID, req.JobId)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	// Map audit events to proto.
	var pbEvents []*pb.OrchestratorAuditEvent
	if len(jobStatus.Events) > 0 {
		pbEvents = make([]*pb.OrchestratorAuditEvent, len(jobStatus.Events))
		for i, evt := range jobStatus.Events {
			pbEvents[i] = &pb.OrchestratorAuditEvent{
				Timestamp: evt.Timestamp,
				Type:      evt.Type,
				Details:   evt.Details,
			}
		}
	}

	return &pb.OrchestratorGetJobStatusResponse{
		JobId:       jobStatus.JobID,
		State:       string(jobStatus.State),
		Iteration:   int32(jobStatus.Iteration), //nolint:gosec // G115: bounded count
		TaskCount:   int32(jobStatus.TaskCount), //nolint:gosec // G115: bounded count
		Completed:   int32(jobStatus.Completed), //nolint:gosec // G115: bounded count
		Failed:      int32(jobStatus.Failed),    //nolint:gosec // G115: bounded count
		Description: jobStatus.Description,
		StartedAt:   jobStatus.StartedAt,
		UpdatedAt:   jobStatus.UpdatedAt,
		Error:       jobStatus.Error,
		Timestamp:   timestamppb.Now(),
		Events:      pbEvents,

		// The task binding and the loop's liveness fields are int64 and
		// strings on the wire, so they widen from the domain's int without
		// narrowing.
		TaskId:        jobStatus.TaskID,
		Attempt:       int64(jobStatus.Attempt),
		Node:          jobStatus.Node,
		HeartbeatAt:   jobStatus.HeartbeatAt,
		FailureClass:  jobStatus.FailureClass,
		LeadSessionId: jobStatus.LeadSessionID,
		SessionIds:    jobStatus.SessionIDs,
		Stage:         jobStatus.Stage,
		Runner:        jobStatus.Runner,
		PromptAppend:  jobStatus.PromptAppend,

		// Provenance: the same facts the REST job shape carries, so a
		// consumer on this transport reads the run without a second call.
		AgentId:        jobStatus.AgentID,
		Source:         taskSourceToProto(jobStatus.Source),
		Stages:         jobStagesToProto(jobStatus.Stages),
		Attempts:       jobAttemptsToProto(jobStatus.Attempts),
		AttemptBudget:  int32(jobStatus.AttemptBudget), //nolint:gosec // G115: bounded count
		TerminalReason: string(jobStatus.TerminalReason),
		ReadmitAllowed: jobStatus.ReadmitAllowed,
		Delivery:       jobDeliveryToProto(jobStatus.Delivery),
		Verification:   jobVerificationToProto(jobStatus.Verification),
	}, nil
}

// jobVerificationToProto maps the last validator pipeline's results.
func jobVerificationToProto(results []orchestration.ValidationResult) []*pb.OrchestratorJobVerification {
	if len(results) == 0 {
		return nil
	}
	out := make([]*pb.OrchestratorJobVerification, len(results))
	for i, result := range results {
		out[i] = &pb.OrchestratorJobVerification{
			Name:    result.Name,
			Passed:  result.Passed,
			Message: result.Message,
			Details: result.Details,
		}
	}
	return out
}

// jobStagesToProto maps the lifecycle moments a job entered.
func jobStagesToProto(stages []orchestration.JobStageRecord) []*pb.OrchestratorJobStage {
	if len(stages) == 0 {
		return nil
	}
	out := make([]*pb.OrchestratorJobStage, len(stages))
	for i, stage := range stages {
		out[i] = &pb.OrchestratorJobStage{
			Stage:     string(stage.Stage),
			EnteredAt: stage.EnteredAt,
		}
	}
	return out
}

// jobAttemptsToProto maps the attempts at the job's task.
func jobAttemptsToProto(attempts []orchestration.JobAttemptRecord) []*pb.OrchestratorJobAttempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]*pb.OrchestratorJobAttempt, len(attempts))
	for i, attempt := range attempts {
		out[i] = &pb.OrchestratorJobAttempt{
			Attempt: int32(attempt.Attempt), //nolint:gosec // G115: bounded count
			JobId:   attempt.JobID,
			Current: attempt.Current,
		}
	}
	return out
}

// jobDeliveryToProto maps the git side of the attempt, nil for a runner that
// published none.
func jobDeliveryToProto(delivery *orchestration.JobDelivery) *pb.OrchestratorJobDelivery {
	if delivery == nil {
		return nil
	}
	reviews := make([]*pb.OrchestratorJobReviewRound, len(delivery.Reviews))
	for i, review := range delivery.Reviews {
		reviews[i] = &pb.OrchestratorJobReviewRound{
			Round:       int32(review.Round), //nolint:gosec // G115: bounded count
			Reviewer:    review.Reviewer,
			Verdict:     review.Verdict,
			HeadSha:     review.HeadSHA,
			Findings:    review.Findings,
			SubmittedAt: review.SubmittedAt,
		}
	}
	return &pb.OrchestratorJobDelivery{
		Branch:            delivery.Branch,
		HeadSha:           delivery.HeadSHA,
		PullRequestNumber: int32(delivery.PullRequestNumber), //nolint:gosec // G115: bounded count
		PullRequestUrl:    delivery.PullRequestURL,
		PullRequestState:  delivery.PullRequestState,
		Reviews:           reviews,
	}
}

// CancelJob cancels a running orchestration job.
func (s *OrchestratorServer) CancelJob(ctx context.Context, req *pb.OrchestratorCancelJobRequest) (*pb.OrchestratorCancelJobResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing CancelJob request")

	if req.JobId == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	if err := s.agentSvc.CancelJob(tenantCtx, tenantID, req.JobId); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.OrchestratorCancelJobResponse{
		JobId:     req.JobId,
		Cancelled: true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// ListJobs returns the tenant's jobs, narrowed to one task when the request
// names one.
//
// It reads the paged verb, ListJobsPage, which also fills each row's Source —
// the forge issue behind the job's task (go-quicbaas#976). The request's
// limit, offset, sort and q are parsed by the same ParsePage every other
// transport uses (go-quicbaas#973).
func (s *OrchestratorServer) ListJobs(ctx context.Context, req *pb.OrchestratorListJobsRequest) (*pb.OrchestratorListJobsResponse, error) {
	s.logger.Debug("Processing ListJobs request")

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	page, err := pageFromRPC(req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ())
	if err != nil {
		return nil, err
	}
	states, err := orchestration.JobStatesFrom("statuses", req.GetStatuses())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	result, err := s.agentSvc.ListJobsPage(tenantCtx, tenantID, &orchestration.JobFilter{
		TaskID:    req.GetTaskId(),
		Runner:    req.GetRunner(),
		ProjectID: req.GetProjectId(),
		AgentID:   req.GetAgentId(),
		Statuses:  states,
		JobIDs:    req.GetJobIds(),
	}, page)
	if err != nil {
		return nil, convertListError(err, convertOrchestratorError)
	}
	jobs := result.Items

	pbJobs := make([]*pb.OrchestratorJobSummary, len(jobs))
	for i, j := range jobs {
		pbJobs[i] = &pb.OrchestratorJobSummary{
			JobId:       j.JobID,
			State:       string(j.State),
			Iteration:   int32(j.Iteration), //nolint:gosec // G115: bounded count
			TaskCount:   int32(j.TaskCount), //nolint:gosec // G115: bounded count
			Completed:   int32(j.Completed), //nolint:gosec // G115: bounded count
			Failed:      int32(j.Failed),    //nolint:gosec // G115: bounded count
			Description: j.Description,
			StartedAt:   j.StartedAt,
			UpdatedAt:   j.UpdatedAt,
			Error:       j.Error,
			EventCount:  int32(len(j.Events)), //nolint:gosec // G115: bounded count
		}
	}

	bindJobSummaries(pbJobs, jobs)
	return &pb.OrchestratorListJobsResponse{
		Jobs:       pbJobs,
		Total:      pageInt32(result.Total),
		Limit:      pageInt32(result.Limit),
		Offset:     pageInt32(result.Offset),
		NextOffset: pageInt32(result.NextOffset),
	}, nil
}

// bindJobSummaries copies each job's task binding onto its summary — the
// task it is an attempt at, which attempt it was, and where that task came
// from. The attempt ordinal is int64 on the wire, so it widens from the
// domain's int without narrowing.
func bindJobSummaries(summaries []*pb.OrchestratorJobSummary, jobs []*orchestration.JobStatus) {
	for i, j := range jobs {
		if i >= len(summaries) || summaries[i] == nil {
			return
		}
		summaries[i].TaskId = j.TaskID
		summaries[i].Attempt = int64(j.Attempt)
		summaries[i].Source = taskSourceToProto(j.Source)
	}
}

// =============================================================================
// Event Streaming
// =============================================================================

// SubscribeJobEvents streams real-time events for an orchestration job.
func (s *OrchestratorServer) SubscribeJobEvents(req *pb.SubscribeJobEventsRequest, stream grpc.ServerStreamingServer[pb.JobEvent]) error {
	s.logger.WithField("job_id", req.JobId).Debug("Processing SubscribeJobEvents request")

	if req.JobId == "" {
		return status.Error(codes.InvalidArgument, "job_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(stream.Context(), req.GetTenantId())
	if err != nil {
		return err
	}

	eventCh, err := s.agentSvc.SubscribeJobEvents(tenantCtx, tenantID, req.JobId)
	if err != nil {
		return convertOrchestratorError(err)
	}

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case evt, ok := <-eventCh:
			if !ok {
				return nil // Channel closed, job completed
			}
			protoEvt := &pb.JobEvent{
				JobId:     evt.JobID,
				Type:      evt.Type,
				State:     string(evt.State),
				Message:   evt.Message,
				Iteration: int32(evt.Iteration), //nolint:gosec // G115: bounded count
				Metadata:  evt.Metadata,
				Timestamp: evt.Timestamp,
			}
			if sendErr := stream.Send(protoEvt); sendErr != nil {
				return sendErr
			}
		}
	}
}

// =============================================================================
// Backend Operations
// =============================================================================

// ListBackends returns all registered agent backends.
func (s *OrchestratorServer) ListBackends(ctx context.Context, req *pb.OrchestratorListBackendsRequest) (*pb.OrchestratorListBackendsResponse, error) {
	s.logger.Debug("Processing ListBackends request")

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	backends, err := s.agentSvc.ListBackends(tenantCtx)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	pbBackends := make([]*pb.AgentBackendInfo, len(backends))
	for i, b := range backends {
		pbBackends[i] = orchestratorBackendInfoToProto(b)
	}

	return &pb.OrchestratorListBackendsResponse{
		Backends:  pbBackends,
		Timestamp: timestamppb.Now(),
	}, nil
}

// =============================================================================
// Runner Registry Operations
// =============================================================================

// ListRunners returns every registered JobRunner and the ordered, non-terminal
// states it declares.
//
// REGISTRY data, not tenant data: the runners a daemon can drive are the same
// for every caller, so no tenant is derived here — exactly as the REST route
// GET /api/v1/orchestrator/runners behaves. It is still gated on the
// orchestrator service being present, because a node that runs no orchestrator
// drives no jobs, and answering "these are the runners" there would describe a
// capability the node does not have.
func (s *OrchestratorServer) ListRunners(_ context.Context, _ *pb.OrchestratorListRunnersRequest) (*pb.OrchestratorListRunnersResponse, error) {
	s.logger.Debug("Processing ListRunners request")

	if s.agentSvc == nil {
		return nil, status.Error(codes.Unavailable, "orchestrator service not available")
	}

	registered := orchestration.JobRunners()
	runners := make([]*pb.OrchestratorJobRunner, 0, len(registered))
	for _, runner := range registered {
		specs := runner.States()
		states := make([]*pb.OrchestratorJobStateSpec, 0, len(specs))
		for _, spec := range specs {
			states = append(states, &pb.OrchestratorJobStateSpec{
				State:         string(spec.State),
				Label:         spec.Label,
				SessionBacked: spec.SessionBacked,
			})
		}
		runners = append(runners, &pb.OrchestratorJobRunner{Name: runner.Name(), States: states})
	}

	return &pb.OrchestratorListRunnersResponse{
		Runners: runners,
		Count:   int32(len(runners)),
	}, nil
}

// =============================================================================
// Health Operations
// =============================================================================

// Health performs a health check on the orchestrator.
func (s *OrchestratorServer) Health(ctx context.Context, req *pb.OrchestratorHealthRequest) (*pb.OrchestratorHealthResponse, error) {
	s.logger.Debug("Processing Health request")

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	report, err := s.agentSvc.HealthCheck(tenantCtx)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.OrchestratorHealthResponse{
		Report:    serviceHealthReportToProto(report),
		Timestamp: timestamppb.Now(),
	}, nil
}

// =============================================================================
// Agent Fleet Management Operations
// =============================================================================

// RegisterFleetNode registers a new agent in the fleet registry.
func (s *OrchestratorServer) RegisterFleetNode(ctx context.Context, req *pb.RegisterFleetNodeRequest) (*pb.RegisterFleetNodeResponse, error) {
	s.logger.Debug("Processing RegisterFleetNode request")

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.RegisterFleetNodeRequest{
		TenantID:     tenantID,
		Name:         req.Name,
		Host:         req.Host,
		Backend:      req.Backend,
		NodeType:     req.NodeType,
		Capabilities: req.Capabilities,
		WorkDir:      req.WorkDir,
		Metadata:     req.Metadata,
	}

	resp, err := s.agentSvc.RegisterFleetNode(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.RegisterFleetNodeResponse{
		Node:      serviceFleetNodeToProto(resp.Node),
		Timestamp: timestamppb.Now(),
	}, nil
}

// GetFleetNode retrieves a registered agent by ID.
func (s *OrchestratorServer) GetFleetNode(ctx context.Context, req *pb.GetFleetNodeRequest) (*pb.GetFleetNodeResponse, error) {
	s.logger.WithField("agent_id", req.NodeId).Debug("Processing GetFleetNode request")

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	agent, err := s.agentSvc.GetFleetNode(tenantCtx, tenantID, req.NodeId)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.GetFleetNodeResponse{
		Node:      serviceFleetNodeToProto(agent),
		Timestamp: timestamppb.Now(),
	}, nil
}

// ListFleetNodes lists agents matching optional filter criteria within a tenant.
func (s *OrchestratorServer) ListFleetNodes(ctx context.Context, req *pb.ListFleetNodesRequest) (*pb.ListFleetNodesResponse, error) {
	s.logger.Debug("Processing ListFleetNodes request")

	var filter *orchestration.FleetNodeFilter
	if req.Filter != nil {
		filter = &orchestration.FleetNodeFilter{
			Host:     req.Filter.Host,
			Status:   protoFleetNodeStatusToService(req.Filter.Status),
			NodeType: req.Filter.NodeType,
			Backend:  req.Filter.Backend,
			Limit:    int(req.Filter.Limit),
		}
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	page, err := pageFromRPC(req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ())
	if err != nil {
		return nil, err
	}

	result, err := s.agentSvc.ListFleetNodesPage(tenantCtx, tenantID, filter, page)
	if err != nil {
		return nil, convertListError(err, convertOrchestratorError)
	}

	pbAgents := make([]*pb.FleetNode, len(result.Items))
	for i, agent := range result.Items {
		pbAgents[i] = serviceFleetNodeToProto(agent)
	}

	// count is how many agents are IN THIS RESPONSE, as it is on REST;
	// total is how many matched.
	return &pb.ListFleetNodesResponse{
		Nodes:      pbAgents,
		Count:      pageInt32(len(result.Items)),
		Timestamp:  timestamppb.Now(),
		Total:      pageInt32(result.Total),
		Limit:      pageInt32(result.Limit),
		Offset:     pageInt32(result.Offset),
		NextOffset: pageInt32(result.NextOffset),
	}, nil
}

// RemoveFleetNode removes an agent from the fleet registry.
func (s *OrchestratorServer) RemoveFleetNode(ctx context.Context, req *pb.RemoveFleetNodeRequest) (*pb.RemoveFleetNodeResponse, error) {
	s.logger.WithField("agent_id", req.NodeId).Debug("Processing RemoveFleetNode request")

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	if err := s.agentSvc.RemoveFleetNode(tenantCtx, tenantID, req.NodeId); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.RemoveFleetNodeResponse{
		NodeId:    req.NodeId,
		Removed:   true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// FleetNodeHeartbeat updates an agent's last seen timestamp.
func (s *OrchestratorServer) FleetNodeHeartbeat(ctx context.Context, req *pb.FleetNodeHeartbeatRequest) (*pb.FleetNodeHeartbeatResponse, error) {
	s.logger.WithField("agent_id", req.NodeId).Debug("Processing FleetNodeHeartbeat request")

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	// The gRPC heartbeat request carries no served-backend list, so it
	// observes nothing and reconciles nothing. Only the REST verb the daemon
	// itself calls reports what its runtime is serving.
	if err := s.agentSvc.FleetNodeHeartbeat(tenantCtx, tenantID, req.NodeId, nil); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.FleetNodeHeartbeatResponse{
		NodeId:       req.NodeId,
		Acknowledged: true,
		Timestamp:    timestamppb.Now(),
	}, nil
}

// =============================================================================
// Type Conversion Helpers - Proto to Service
// =============================================================================

// protoMCPServersToService converts proto MCPServerConfig slices to domain types.
func protoMCPServersToService(servers []*pb.MCPServerConfig) []orchestration.MCPServerConfig {
	if len(servers) == 0 {
		return nil
	}
	result := make([]orchestration.MCPServerConfig, len(servers))
	for i, srv := range servers {
		result[i] = orchestration.MCPServerConfig{
			Name: srv.Name,
			URL:  srv.Url,
			Env:  srv.Env,
			// Command mirrors URL for callers that still read the legacy field.
			Command: srv.Url,
		}
	}
	return result
}

// protoContainerSpecToService converts a proto ContainerSpec to the domain type.
func protoContainerSpecToService(spec *pb.ContainerSpec) *orchestration.ContainerSpec {
	if spec == nil {
		return nil
	}
	return &orchestration.ContainerSpec{
		Name:    spec.Name,
		Image:   spec.Image,
		Env:     spec.Env,
		WorkDir: spec.WorkDir,
		Command: spec.Command,
		Network: spec.Network,
	}
}

// protoTaskStatusToService converts a proto TaskStatus to the domain TaskStatus.
func protoTaskStatusToService(ts pb.TaskStatus) orchestration.TaskStatus {
	switch ts {
	case pb.TaskStatus_TASK_STATUS_PENDING:
		return orchestration.TaskStatusPending
	case pb.TaskStatus_TASK_STATUS_CLAIMED:
		return orchestration.TaskStatusClaimed
	case pb.TaskStatus_TASK_STATUS_IN_PROGRESS:
		return orchestration.TaskStatusInProgress
	case pb.TaskStatus_TASK_STATUS_COMPLETED:
		return orchestration.TaskStatusCompleted
	case pb.TaskStatus_TASK_STATUS_FAILED:
		return orchestration.TaskStatusFailed
	case pb.TaskStatus_TASK_STATUS_CANCELLED:
		return orchestration.TaskStatusCancelled
	case pb.TaskStatus_TASK_STATUS_BLOCKED:
		return orchestration.TaskStatusBlocked
	default:
		return ""
	}
}

// protoTaskStatusesToService converts a list of proto TaskStatus values,
// dropping unspecified entries so an empty filter stays empty.
func protoTaskStatusesToService(statuses []pb.TaskStatus) []orchestration.TaskStatus {
	if len(statuses) == 0 {
		return nil
	}
	out := make([]orchestration.TaskStatus, 0, len(statuses))
	for _, ts := range statuses {
		if converted := protoTaskStatusToService(ts); converted != "" {
			out = append(out, converted)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// protoTaskSourceToService converts a proto TaskSource; nil stays nil so a
// task created without provenance records none.
func protoTaskSourceToService(src *pb.TaskSource) *orchestration.TaskSourceRef {
	if src == nil {
		return nil
	}
	return &orchestration.TaskSourceRef{
		Kind: orchestration.TaskSourceKind(src.GetKind()),
		Ref:  src.GetRef(),
		URL:  src.GetUrl(),
	}
}

// taskSourceToProto converts a recorded origin the other way; nil stays nil
// so a record without provenance emits no source rather than an empty one a
// consumer would render as a chip pointing nowhere. It serves the task shape
// and the two job shapes, which carry the same origin.
func taskSourceToProto(src *orchestration.TaskSourceRef) *pb.TaskSource {
	if src == nil {
		return nil
	}
	return &pb.TaskSource{
		Kind: string(src.Kind),
		Ref:  src.Ref,
		Url:  src.URL,
	}
}

// protoFleetNodeStatusToService converts a proto FleetNodeStatus to the domain FleetNodeStatus.
func protoFleetNodeStatusToService(as pb.FleetNodeStatus) orchestration.FleetNodeStatus {
	switch as {
	case pb.FleetNodeStatus_FLEET_NODE_STATUS_ONLINE:
		return orchestration.FleetNodeStatusRunning
	case pb.FleetNodeStatus_FLEET_NODE_STATUS_OFFLINE:
		return orchestration.FleetNodeStatusOffline
	case pb.FleetNodeStatus_FLEET_NODE_STATUS_BUSY:
		return orchestration.FleetNodeStatusRunning
	case pb.FleetNodeStatus_FLEET_NODE_STATUS_ERROR:
		return orchestration.FleetNodeStatusError
	case pb.FleetNodeStatus_FLEET_NODE_STATUS_DRAINING:
		return orchestration.FleetNodeStatusIdle
	default:
		return ""
	}
}

// =============================================================================
// Type Conversion Helpers - Service to Proto
// =============================================================================

// serviceSessionStateToProto converts a domain SessionState to proto SessionState.
func serviceSessionStateToProto(ss orchestration.SessionState) pb.SessionState {
	switch ss {
	case orchestration.SessionStateActive:
		return pb.SessionState_SESSION_STATE_ACTIVE
	case orchestration.SessionStateIdle:
		return pb.SessionState_SESSION_STATE_IDLE
	case orchestration.SessionStateClosed:
		return pb.SessionState_SESSION_STATE_CLOSED
	case orchestration.SessionStateError:
		return pb.SessionState_SESSION_STATE_ERROR
	case orchestration.SessionStateResumable:
		return pb.SessionState_SESSION_STATE_RESUMABLE
	default:
		return pb.SessionState_SESSION_STATE_UNSPECIFIED
	}
}

// serviceTaskStatusToProto converts a domain TaskStatus to proto TaskStatus.
func serviceTaskStatusToProto(ts orchestration.TaskStatus) pb.TaskStatus {
	switch ts {
	case orchestration.TaskStatusPending:
		return pb.TaskStatus_TASK_STATUS_PENDING
	case orchestration.TaskStatusClaimed:
		return pb.TaskStatus_TASK_STATUS_CLAIMED
	case orchestration.TaskStatusInProgress:
		return pb.TaskStatus_TASK_STATUS_IN_PROGRESS
	case orchestration.TaskStatusCompleted:
		return pb.TaskStatus_TASK_STATUS_COMPLETED
	case orchestration.TaskStatusFailed:
		return pb.TaskStatus_TASK_STATUS_FAILED
	case orchestration.TaskStatusCancelled:
		return pb.TaskStatus_TASK_STATUS_CANCELLED
	case orchestration.TaskStatusBlocked:
		return pb.TaskStatus_TASK_STATUS_BLOCKED
	default:
		return pb.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}

// serviceFleetNodeStatusToProto converts a domain FleetNodeStatus to proto FleetNodeStatus.
func serviceFleetNodeStatusToProto(as orchestration.FleetNodeStatus) pb.FleetNodeStatus {
	switch as {
	case orchestration.FleetNodeStatusRegistered:
		return pb.FleetNodeStatus_FLEET_NODE_STATUS_ONLINE
	case orchestration.FleetNodeStatusRunning:
		return pb.FleetNodeStatus_FLEET_NODE_STATUS_BUSY
	case orchestration.FleetNodeStatusIdle:
		return pb.FleetNodeStatus_FLEET_NODE_STATUS_ONLINE
	case orchestration.FleetNodeStatusOffline:
		return pb.FleetNodeStatus_FLEET_NODE_STATUS_OFFLINE
	case orchestration.FleetNodeStatusError:
		return pb.FleetNodeStatus_FLEET_NODE_STATUS_ERROR
	default:
		return pb.FleetNodeStatus_FLEET_NODE_STATUS_UNSPECIFIED
	}
}

// serviceContentBlocksToProto converts domain ContentBlocks to proto ContentBlocks.
func serviceContentBlocksToProto(blocks []orchestration.ContentBlock) []*pb.ContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	result := make([]*pb.ContentBlock, len(blocks))
	for i, b := range blocks {
		result[i] = &pb.ContentBlock{
			Type: b.Type,
			Text: b.Text,
		}
	}
	return result
}

// serviceSessionInfoToProto converts a domain SessionInfo to proto SessionInfo.
func serviceSessionInfoToProto(info *orchestration.SessionInfo) *pb.SessionInfo {
	if info == nil {
		return nil
	}
	return &pb.SessionInfo{
		SessionId:    info.SessionID,
		ProjectId:    info.ProjectID,
		Backend:      info.Backend,
		Model:        info.Model,
		State:        serviceSessionStateToProto(info.State),
		CreatedAt:    unixNanoTimestamp(info.CreatedAt),
		LastActivity: unixNanoTimestamp(info.LastActivity),
		JobId:        info.JobID,
		ContainerId:  info.ContainerID,
		Runner:       info.Runner,
		AgentId:      info.AgentID,
	}
}

// unixNanoTimestamp renders a Unix-NANOSECOND instant as a protobuf
// timestamp.
//
// THE UNIT IS THE WHOLE POINT. A session's times are nanoseconds, and reading
// them as seconds put the instant some fifty billion years out — far outside
// the range a protobuf timestamp can carry, so what reached the operator was
// not a wrong date but no date at all. An unset instant stays unset rather
// than becoming the epoch, because "this session has never been active" and
// "this session was active on 1 January 1970" are different claims.
func unixNanoTimestamp(nanos int64) *timestamppb.Timestamp {
	if nanos <= 0 {
		return nil
	}
	return timestamppb.New(time.Unix(0, nanos).UTC())
}

// serviceTaskToProto converts a domain Task to proto Task.
func serviceTaskToProto(task *orchestration.Task) *pb.Task {
	if task == nil {
		return nil
	}

	pbTask := &pb.Task{
		Id:           task.ID,
		TenantId:     task.TenantID,
		TeamName:     task.TeamName,
		Subject:      task.Subject,
		Description:  task.Description,
		Status:       serviceTaskStatusToProto(task.Status),
		AssignedTo:   task.AssignedTo,
		CreatedBy:    task.CreatedBy,
		Dependencies: task.Dependencies,
		Blocks:       task.Blocks,
		Metadata:     task.Metadata,
		CreatedAt:    unixNanoTimestamp(task.CreatedAt),
		UpdatedAt:    unixNanoTimestamp(task.UpdatedAt),
		Version:      task.Version,
	}

	if task.Result != nil {
		pbTask.Result = &pb.TaskResult{
			Output:   task.Result.Output,
			Files:    task.Result.Files,
			Metadata: task.Result.Metadata,
		}
	}

	if task.ClaimedAt != 0 {
		pbTask.ClaimedAt = unixNanoTimestamp(task.ClaimedAt)
	}

	if task.CompletedAt != 0 {
		pbTask.CompletedAt = unixNanoTimestamp(task.CompletedAt)
	}

	if task.AutoSpawn {
		pbTask.AutoSpawn = true
	}
	if task.SpawnedJobID != "" {
		pbTask.SpawnedJobId = task.SpawnedJobID
	}
	if task.SpawnedAt != 0 {
		pbTask.SpawnedAt = unixNanoTimestamp(task.SpawnedAt)
	}

	pbTask.ProjectId = task.ProjectID
	pbTask.Source = taskSourceToProto(task.Source)
	pbTask.Priority = int64(task.Priority)
	pbTask.Attempts = int64(task.Attempts)
	pbTask.AttemptBudget = int64(task.AttemptBudget)
	pbTask.EnvironmentFaults = int64(task.EnvironmentFaults)
	pbTask.QuestionAttempts = int64(task.QuestionAttempts)
	pbTask.ConsecutiveEnvironmentFaults = int64(task.ConsecutiveEnvironmentFaults)
	pbTask.RetryNotBefore = task.RetryNotBefore
	pbTask.BlockedReason = task.BlockedReason
	pbTask.BlockKind = string(task.BlockKind)
	pbTask.StatusChangedAt = task.StatusChangedAt
	pbTask.Runner = task.Runner
	pbTask.PromptAppend = task.PromptAppend
	pbTask.Routing = &pb.TaskRouting{
		Backend:      task.Routing.Backend,
		HostAffinity: task.Routing.HostAffinity,
		GpuRequired:  task.Routing.GpuRequired,
	}
	pbTask.DispatchReason = task.DispatchReason
	pbTask.BlockFaultClass = task.BlockFaultClass
	pbTask.ConclusionReason = string(task.ConclusionReason)
	// Derived on read, exactly as the REST representation derives them, so
	// the two transports cannot disagree about a task's accounting.
	pbTask.ChargedAttempts = int64(task.ChargedAttempts())
	pbTask.EffectiveBudget = int64(task.EffectiveAttemptBudget())
	pbTask.FaultClass = task.FaultClass()
	pbTask.FaultOrigin = string(task.FaultOrigin())

	return pbTask
}

// orchestratorBackendInfoToProto converts a domain AgentBackendInfo to proto AgentBackendInfo.
func orchestratorBackendInfoToProto(info *orchestration.AgentBackendInfo) *pb.AgentBackendInfo {
	if info == nil {
		return nil
	}

	pbInfo := &pb.AgentBackendInfo{
		Name:         info.Name,
		Type:         info.Type,
		DefaultModel: info.DefaultModel,
	}

	pbInfo.Capabilities = &pb.BackendCapabilities{
		SupportsSessions:  info.Capabilities.PersistentSessions,
		SupportsStreaming: info.Capabilities.Streaming,
		SupportsMcp:       info.Capabilities.ACP,
		SupportsTools:     info.Capabilities.ToolUse,
	}

	return pbInfo
}

// serviceHealthReportToProto converts a domain HealthCheckReport to proto HealthCheckReport.
func serviceHealthReportToProto(report *orchestration.HealthCheckReport) *pb.HealthCheckReport {
	if report == nil {
		return nil
	}

	results := make([]*pb.SessionHealthResult, len(report.Results))
	for i, r := range report.Results {
		results[i] = &pb.SessionHealthResult{
			SessionId: r.SessionID,
			Backend:   r.Backend,
			State:     serviceSessionStateToProto(r.State),
			Healthy:   r.Healthy,
			Error:     r.Error,
		}
	}

	return &pb.HealthCheckReport{
		Timestamp:     unixNanoTimestamp(report.Timestamp),
		TotalSessions: int32(report.TotalSessions), //nolint:gosec // G115: bounded count
		Healthy:       int32(report.Healthy),       //nolint:gosec // G115: bounded count
		Unhealthy:     int32(report.Unhealthy),     //nolint:gosec // G115: bounded count
		Closed:        int32(report.Closed),        //nolint:gosec // G115: bounded count
		Results:       results,
	}
}

// serviceFleetNodeToProto converts a domain FleetNode to proto FleetNode.
func serviceFleetNodeToProto(agent *orchestration.FleetNode) *pb.FleetNode {
	if agent == nil {
		return nil
	}
	return &pb.FleetNode{
		Id:           agent.ID,
		TenantId:     agent.TenantID,
		Name:         agent.Name,
		Host:         agent.Host,
		Backend:      agent.Backend,
		NodeType:     agent.NodeType,
		Status:       serviceFleetNodeStatusToProto(agent.Status),
		Capabilities: agent.Capabilities,
		WorkDir:      agent.WorkDir,
		LastSeen:     unixNanoTimestamp(agent.LastSeen),
		RegisteredAt: unixNanoTimestamp(agent.RegisteredAt),
		Version:      agent.Version,
		Metadata:     agent.Metadata,
	}
}

// =============================================================================
// Error Handling
// =============================================================================

// convertOrchestratorError converts orchestration errors to gRPC status errors.
func convertOrchestratorError(err error) error {
	if err == nil {
		return nil
	}
	if mapped, ok := referenceStatus(err); ok {
		return mapped
	}

	// Check if it's already a gRPC status error
	if _, ok := status.FromError(err); ok {
		return err
	}

	// Spawn-policy refusals (bind mount outside every allowed root, forbidden
	// sandbox mode or network, integrity tier below the operator's floor) are
	// authorization decisions about the request, not server faults. They must
	// arrive as PermissionDenied with their actionable message intact rather
	// than as the Internal fallback at the bottom of this function.
	if orchestration.IsPolicyDenial(err) {
		return status.Error(codes.PermissionDenied, err.Error())
	}

	// A spawn or a job naming an Agent or pool is refused in the catalog's
	// own vocabulary; a pool every Agent of which refused is exhausted.
	if isAgentCatalogError(err) {
		return agentCatalogStatusError(err)
	}

	// Check orchestration-specific typed errors
	var agentNotFoundErr *orchestration.FleetNodeNotFoundError
	if stderrors.As(err, &agentNotFoundErr) {
		return status.Error(codes.NotFound, agentNotFoundErr.Error())
	}

	var agentAlreadyExistsErr *orchestration.FleetNodeAlreadyExistsError
	if stderrors.As(err, &agentAlreadyExistsErr) {
		return status.Error(codes.AlreadyExists, agentAlreadyExistsErr.Error())
	}

	var taskNotFoundErr *orchestration.TaskNotFoundError
	if stderrors.As(err, &taskNotFoundErr) {
		return status.Error(codes.NotFound, taskNotFoundErr.Error())
	}

	var taskAlreadyExistsErr *orchestration.TaskAlreadyExistsError
	if stderrors.As(err, &taskAlreadyExistsErr) {
		return status.Error(codes.AlreadyExists, taskAlreadyExistsErr.Error())
	}

	var taskAlreadyClaimedErr *orchestration.TaskAlreadyClaimedError
	if stderrors.As(err, &taskAlreadyClaimedErr) {
		return status.Error(codes.FailedPrecondition, taskAlreadyClaimedErr.Error())
	}

	// A task a running job holds a live lease on: the caller named a real
	// task and only the attempt in flight stands in the way, so this is a
	// precondition that will clear on its own, not an argument fault.
	var taskLeasedErr *orchestration.TaskLeasedError
	if stderrors.As(err, &taskLeasedErr) {
		return status.Error(codes.FailedPrecondition, taskLeasedErr.Error())
	}

	var taskVersionConflictErr *orchestration.TaskVersionConflictError
	if stderrors.As(err, &taskVersionConflictErr) {
		return status.Error(codes.Aborted, taskVersionConflictErr.Error())
	}

	var taskDependencyBlockedErr *orchestration.TaskDependencyBlockedError
	if stderrors.As(err, &taskDependencyBlockedErr) {
		return status.Error(codes.FailedPrecondition, taskDependencyBlockedErr.Error())
	}

	var taskInvalidStateErr *orchestration.TaskInvalidStateError
	if stderrors.As(err, &taskInvalidStateErr) {
		return status.Error(codes.FailedPrecondition, taskInvalidStateErr.Error())
	}

	var projectNotFoundErr *orchestration.ProjectNotFoundError
	if stderrors.As(err, &projectNotFoundErr) {
		return status.Error(codes.NotFound, projectNotFoundErr.Error())
	}

	var jobNotFoundErr *orchestration.JobNotFoundError
	if stderrors.As(err, &jobNotFoundErr) {
		return status.Error(codes.NotFound, jobNotFoundErr.Error())
	}

	// An unknown session is a lookup miss on every other transport — REST's
	// isOrchNotFoundError already answers 404 for it — so gRPC says NotFound
	// rather than dropping it into the Internal fallback below.
	var sessionNotFoundErr *orchestration.SessionNotFoundError
	if stderrors.As(err, &sessionNotFoundErr) {
		return status.Error(codes.NotFound, sessionNotFoundErr.Error())
	}

	// A session that may not rotate is a state problem, not a bad request: the
	// caller named a real session and the only thing wrong is its policy.
	var notRotatableErr *orchestration.SessionNotRotatableError
	if stderrors.As(err, &notRotatableErr) {
		return status.Error(codes.FailedPrecondition, notRotatableErr.Error())
	}

	// No pooled-account rotation on this deployment at all — nothing the
	// caller can change about the request would help.
	var rotationUnsupportedErr *orchestration.SessionRotationUnsupportedError
	if stderrors.As(err, &rotationUnsupportedErr) {
		return status.Error(codes.Unimplemented, rotationUnsupportedErr.Error())
	}

	// A LISTING FILTER THAT NAMED A STATE THIS DAEMON DOES NOT USE IS THE
	// CALLER'S MISTAKE. It arrives wrapped in an AgentServiceError, so it has
	// to be answered BEFORE the branch below, which would otherwise classify a
	// misspelled `status` as Internal — telling an operator their own typo was
	// a server fault, and telling a client to retry something that will never
	// succeed. The message already names the states that do exist.
	if stderrors.Is(err, orchestration.ErrSessionStateUnknown) {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// Every refusal of the request's own shape — an InputValidationError, an
	// oversized prompt append — unwraps to ErrInvalidInput and is the
	// caller's to correct, never a server fault.
	if stderrors.Is(err, orchestration.ErrInvalidInput) {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	var agentSvcErr *orchestration.AgentServiceError
	if stderrors.As(err, &agentSvcErr) {
		return status.Error(codes.Internal, agentSvcErr.Error())
	}

	var agentStoreErr *orchestration.AgentStoreError
	if stderrors.As(err, &agentStoreErr) {
		return status.Error(codes.Internal, agentStoreErr.Error())
	}

	var agentRegistryErr *orchestration.FleetRegistryError
	if stderrors.As(err, &agentRegistryErr) {
		return status.Error(codes.Internal, agentRegistryErr.Error())
	}

	var taskStoreErr *orchestration.TaskStoreError
	if stderrors.As(err, &taskStoreErr) {
		return status.Error(codes.Internal, taskStoreErr.Error())
	}

	// Check sentinel errors
	if stderrors.Is(err, orchestration.ErrAgentServiceUnavailable) {
		return status.Error(codes.Unavailable, err.Error())
	}

	if stderrors.Is(err, orchestration.ErrBackendRequired) ||
		stderrors.Is(err, orchestration.ErrPromptRequired) ||
		stderrors.Is(err, orchestration.ErrSessionIDRequired) ||
		stderrors.Is(err, orchestration.ErrTaskSubjectRequired) ||
		stderrors.Is(err, orchestration.ErrTenantIDRequired) ||
		stderrors.Is(err, orchestration.ErrTaskIDRequired) ||
		stderrors.Is(err, orchestration.ErrAgentIDRequired) ||
		stderrors.Is(err, orchestration.ErrAgentStoreInvalidInput) ||
		stderrors.Is(err, orchestration.ErrFleetRegistryInvalidInput) ||
		stderrors.Is(err, orchestration.ErrTaskStoreInvalidInput) {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	if stderrors.Is(err, orchestration.ErrAgentStoreNotFound) ||
		stderrors.Is(err, orchestration.ErrFleetNodeNotFound) ||
		stderrors.Is(err, orchestration.ErrTaskNotFound) ||
		stderrors.Is(err, orchestration.ErrProjectNotFound) ||
		stderrors.Is(err, orchestration.ErrJobNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}

	if stderrors.Is(err, orchestration.ErrFleetNodeAlreadyExists) ||
		stderrors.Is(err, orchestration.ErrTaskAlreadyExists) {
		return status.Error(codes.AlreadyExists, err.Error())
	}

	if stderrors.Is(err, orchestration.ErrTaskAlreadyClaimed) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}

	if stderrors.Is(err, orchestration.ErrTaskVersionConflict) {
		return status.Error(codes.Aborted, err.Error())
	}

	if stderrors.Is(err, orchestration.ErrTaskInvalidState) ||
		stderrors.Is(err, orchestration.ErrTaskDependencyBlocked) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}

	if stderrors.Is(err, orchestration.ErrJobAlreadyRunning) {
		return status.Error(codes.AlreadyExists, err.Error())
	}

	// Fall back to QBError handling
	var dErr *errors.QBError
	if !stderrors.As(err, &dErr) {
		return status.Error(codes.Internal, err.Error())
	}

	switch dErr.Code {
	case errors.ErrNotFound:
		return status.Error(codes.NotFound, dErr.Error())
	case errors.ErrAlreadyExists:
		return status.Error(codes.AlreadyExists, dErr.Error())
	case errors.ErrInvalidInput:
		return status.Error(codes.InvalidArgument, dErr.Error())
	case errors.ErrUnauthorized:
		return status.Error(codes.Unauthenticated, dErr.Error())
	case errors.ErrForbidden:
		return status.Error(codes.PermissionDenied, dErr.Error())
	case errors.ErrTimeout:
		return status.Error(codes.DeadlineExceeded, dErr.Error())
	case errors.ErrResourceExhausted:
		return status.Error(codes.ResourceExhausted, dErr.Error())
	case errors.ErrUnavailable:
		return status.Error(codes.Unavailable, dErr.Error())
	case errors.ErrCanceled:
		return status.Error(codes.Canceled, dErr.Error())
	default:
		return status.Error(codes.Internal, dErr.Error())
	}
}

// =============================================================================
// Sub-job Spawning
// =============================================================================

// SpawnChildJob fans out a child job from a parent job's lead agent.
// The CallerAgentID is extracted from the gRPC request field; production
// deployments should populate it from the mTLS peer certificate CN via an
// interceptor — the service layer enforces the lead-only invariant.
func (s *OrchestratorServer) SpawnChildJob(ctx context.Context, req *pb.SpawnChildJobRequest) (*pb.SpawnChildJobResponse, error) {
	s.logger.WithField("parent_job_id", req.GetParentJobId()).Debug("Processing SpawnChildJob request")

	if req.GetParentJobId() == "" {
		return nil, status.Error(codes.InvalidArgument, "parent_job_id is required")
	}

	tenantCtx, _, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	svcReq := &orchestration.SpawnChildJobRequest{
		ParentJobID:        req.GetParentJobId(),
		WorkflowID:         req.GetWorkflowId(),
		ProjectID:          req.GetProjectId(),
		HostAffinity:       req.GetHostAffinity(),
		GpuRequired:        req.GetGpuRequired(),
		HostAffinityStrict: req.GetHostAffinityStrict(),
	}

	for _, t := range req.GetTasks() {
		svcReq.Tasks = append(svcReq.Tasks, &orchestration.TaskSpec{
			Title:                t.GetTitle(),
			Description:          t.GetDescription(),
			Dependencies:         t.GetDependencies(),
			RequiredCapabilities: t.GetRequiredCapabilities(),
		})
	}

	resp, err := s.agentSvc.SpawnChildJob(tenantCtx, svcReq)
	if err != nil {
		// NotLeadOfParentError → PermissionDenied.
		if stderrors.Is(err, orchestration.ErrNotLeadOfParent) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if stderrors.Is(err, orchestration.ErrParentJobNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, convertOrchestratorError(err)
	}

	return &pb.SpawnChildJobResponse{ChildJobId: resp.ChildJobID}, nil
}

// =============================================================================
// Artifact Operations
// =============================================================================

// SubmitTaskArtifacts attaches typed artifacts to a task.
func (s *OrchestratorServer) SubmitTaskArtifacts(ctx context.Context, req *pb.SubmitTaskArtifactsRequest) (*pb.SubmitTaskArtifactsResponse, error) {
	s.logger.WithField("task_id", req.GetTaskId()).Debug("Processing SubmitTaskArtifacts request")

	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	artifacts := protoArtifactsToService(req.GetArtifacts())

	if err := s.agentSvc.SubmitTaskArtifacts(tenantCtx, tenantID, req.GetTaskId(), artifacts); err != nil {
		return nil, convertArtifactError(err)
	}

	return &pb.SubmitTaskArtifactsResponse{
		TaskId:    req.GetTaskId(),
		Stored:    int32(len(artifacts)),
		Timestamp: timestamppb.Now(),
	}, nil
}

// ListTaskArtifacts returns artifact metadata for a task.
func (s *OrchestratorServer) ListTaskArtifacts(ctx context.Context, req *pb.ListTaskArtifactsRequest) (*pb.ListTaskArtifactsResponse, error) {
	s.logger.WithField("task_id", req.GetTaskId()).Debug("Processing ListTaskArtifacts request")

	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	artifacts, err := s.agentSvc.ListTaskArtifacts(tenantCtx, tenantID, req.GetTaskId())
	if err != nil {
		return nil, convertArtifactError(err)
	}

	return &pb.ListTaskArtifactsResponse{
		Artifacts: serviceArtifactsToProto(artifacts),
		Timestamp: timestamppb.Now(),
	}, nil
}

// GetTaskArtifact retrieves a single named artifact with content resolved.
func (s *OrchestratorServer) GetTaskArtifact(ctx context.Context, req *pb.GetTaskArtifactRequest) (*pb.GetTaskArtifactResponse, error) {
	s.logger.WithField("task_id", req.GetTaskId()).Debug("Processing GetTaskArtifact request")

	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	tenantCtx, tenantID, err := orchestratorTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	artifact, err := s.agentSvc.GetTaskArtifact(tenantCtx, tenantID, req.GetTaskId(), req.GetName())
	if err != nil {
		return nil, convertArtifactError(err)
	}

	return &pb.GetTaskArtifactResponse{
		Artifact:  serviceArtifactToProto(artifact),
		Timestamp: timestamppb.Now(),
	}, nil
}

// protoArtifactsToService converts proto Artifact slices to service types.
func protoArtifactsToService(pbArtifacts []*pb.Artifact) []orchestration.Artifact {
	if len(pbArtifacts) == 0 {
		return nil
	}
	result := make([]orchestration.Artifact, len(pbArtifacts))
	for i, a := range pbArtifacts {
		result[i] = orchestration.Artifact{
			Kind:     orchestration.ArtifactKind(a.GetKind()),
			Name:     a.GetName(),
			MIMEType: a.GetMimeType(),
			Size:     a.GetSize(),
			Content:  a.GetContent(),
			Ref: orchestration.ArtifactRef{
				AgentID: a.GetRef().GetAgentId(),
				Path:    a.GetRef().GetPath(),
				SHA:     a.GetRef().GetSha(),
				Branch:  a.GetRef().GetBranch(),
			},
			Metadata: a.GetMetadata(),
		}
	}
	return result
}

// serviceArtifactsToProto converts a service artifact slice to proto.
func serviceArtifactsToProto(artifacts []orchestration.Artifact) []*pb.Artifact {
	if len(artifacts) == 0 {
		return nil
	}
	result := make([]*pb.Artifact, len(artifacts))
	for i, a := range artifacts {
		result[i] = serviceArtifactToProto(&a)
	}
	return result
}

// serviceArtifactToProto converts a single service Artifact to proto.
func serviceArtifactToProto(a *orchestration.Artifact) *pb.Artifact {
	if a == nil {
		return nil
	}
	return &pb.Artifact{
		Kind:     string(a.Kind),
		Name:     a.Name,
		MimeType: a.MIMEType,
		Size:     a.Size,
		Content:  a.Content,
		Ref: &pb.ArtifactRef{
			AgentId: a.Ref.AgentID,
			Path:    a.Ref.Path,
			Sha:     a.Ref.SHA,
			Branch:  a.Ref.Branch,
		},
		Metadata: a.Metadata,
	}
}

// convertArtifactError maps artifact errors to gRPC status codes.
func convertArtifactError(err error) error {
	if stderrors.Is(err, orchestration.ErrArtifactNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if stderrors.Is(err, orchestration.ErrArtifactKindInvalid) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if stderrors.Is(err, orchestration.ErrArtifactRefInvalid) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if stderrors.Is(err, orchestration.ErrArtifactTooLarge) {
		return status.Error(codes.ResourceExhausted, err.Error())
	}
	if stderrors.Is(err, orchestration.ErrTaskNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

// Ensure OrchestratorServer implements the interface.
var _ pb.OrchestratorServiceServer = (*OrchestratorServer)(nil)
