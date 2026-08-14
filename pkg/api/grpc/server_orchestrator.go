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

	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	svcReq := &orchestration.SpawnSessionRequest{
		Backend:            req.Backend,
		ProjectID:          req.ProjectId,
		Model:              req.Model,
		SystemPrompt:       req.SystemPrompt,
		MCPServers:         protoMCPServersToService(req.McpServers),
		Env:                req.Env,
		WorkDir:            req.WorkDir,
		SandboxMode:        orchestration.SandboxMode(req.SandboxMode),
		Sandboxed:          req.Sandboxed,
		ContainerImage:     req.ContainerImage,
		ContainerConfig:    protoContainerSpecToService(req.ContainerConfig),
		HostAffinity:       req.HostAffinity,
		HostAffinityStrict: req.HostAffinityStrict,
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
	// RemoteBackendResolver can stash a typed NoMatchingAgentError when the
	// host is offline.
	if req.HostAffinityStrict {
		tenantCtx = orchestration.WithHostAffinityStrict(tenantCtx)
	}

	resp, err := s.agentSvc.SpawnSession(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.SpawnSessionResponse{
		SessionId:   resp.SessionID,
		Backend:     resp.Backend,
		Model:       resp.Model,
		ContainerId: resp.ContainerID,
		Timestamp:   timestamppb.New(time.Unix(resp.Timestamp, 0)),
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

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
			Timestamp: timestamppb.New(time.Unix(0, ack.Timestamp)),
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
		Timestamp:  timestamppb.New(time.Unix(resp.Timestamp, 0)),
	}, nil
}

// SessionStatus returns the status of an active session.
func (s *OrchestratorServer) SessionStatus(ctx context.Context, req *pb.SessionStatusRequest) (*pb.SessionStatusResponse, error) {
	s.logger.WithField("session_id", req.SessionId).Debug("Processing SessionStatus request")

	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

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
		LastActivity:  timestamppb.New(time.Unix(resp.LastActivity, 0)),
		UptimeSeconds: resp.Uptime,
		Timestamp:     timestamppb.New(time.Unix(resp.Timestamp, 0)),
	}, nil
}

// CloseSession terminates an active session.
func (s *OrchestratorServer) CloseSession(ctx context.Context, req *pb.CloseSessionRequest) (*pb.CloseSessionResponse, error) {
	s.logger.WithField("session_id", req.SessionId).Debug("Processing CloseSession request")

	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	svcReq := &orchestration.CloseSessionRequest{
		SessionID: req.SessionId,
	}

	resp, err := s.agentSvc.CloseSession(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.CloseSessionResponse{
		SessionId: resp.SessionID,
		Timestamp: timestamppb.New(time.Unix(resp.Timestamp, 0)),
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

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

// ListSessions returns all active sessions.
func (s *OrchestratorServer) ListSessions(ctx context.Context, req *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	s.logger.Debug("Processing ListSessions request")

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	svcReq := &orchestration.ListSessionsRequest{
		Backend: req.Backend,
	}

	resp, err := s.agentSvc.ListSessions(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	sessions := make([]*pb.SessionInfo, len(resp.Sessions))
	for i, sess := range resp.Sessions {
		sessions[i] = serviceSessionInfoToProto(sess)
	}

	return &pb.ListSessionsResponse{
		Sessions:  sessions,
		Timestamp: timestamppb.New(time.Unix(resp.Timestamp, 0)),
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	svcReq := &orchestration.CreateTaskRequest{
		TenantID:     req.TenantId,
		TeamName:     req.TeamName,
		Subject:      req.Subject,
		Description:  req.Description,
		AssignedTo:   req.AssignedTo,
		Dependencies: req.Dependencies,
		Metadata:     req.Metadata,
		ProjectID:    req.GetProjectId(),
		AutoSpawn:    req.GetAutoSpawn(),
		Backend:      req.GetBackend(),
		GpuRequired:  req.GetGpuRequired(),
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	task, err := s.agentSvc.GetTask(tenantCtx, req.TenantId, req.TaskId)
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	var filter *orchestration.TaskFilter
	if req.Filter != nil {
		filter = &orchestration.TaskFilter{
			TeamName:   req.Filter.TeamName,
			Status:     protoTaskStatusToService(req.Filter.Status),
			AssignedTo: req.Filter.AssignedTo,
			CreatedBy:  req.Filter.CreatedBy,
			Limit:      int(req.Filter.Limit),
		}
	}

	tasks, err := s.agentSvc.ListTasks(tenantCtx, req.TenantId, filter)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	pbTasks := make([]*pb.Task, len(tasks))
	for i, task := range tasks {
		pbTasks[i] = serviceTaskToProto(task)
	}

	return &pb.ListTasksResponse{
		Tasks:      pbTasks,
		TotalCount: int32(len(tasks)), //nolint:gosec // G115: bounded count
		Timestamp:  timestamppb.Now(),
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	if err := s.agentSvc.ClaimTask(tenantCtx, req.TenantId, req.TaskId, req.AgentId); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.ClaimTaskResponse{
		TaskId:    req.TaskId,
		AgentId:   req.AgentId,
		Claimed:   true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// CompleteTask marks a task as completed.
func (s *OrchestratorServer) CompleteTask(ctx context.Context, req *pb.CompleteTaskRequest) (*pb.CompleteTaskResponse, error) {
	s.logger.WithField("task_id", req.TaskId).Debug("Processing CompleteTask request")

	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	var result *orchestration.TaskResult
	if req.Result != nil {
		result = &orchestration.TaskResult{
			Output:   req.Result.Output,
			Files:    req.Result.Files,
			Metadata: req.Result.Metadata,
		}
	}

	if err := s.agentSvc.CompleteTask(tenantCtx, req.TenantId, req.TaskId, result); err != nil {
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

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
		Timestamp: timestamppb.New(time.Unix(resp.Timestamp, 0)),
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	svcReq := &orchestration.StartJobRequest{
		TenantID:      req.TenantId,
		Description:   req.Description,
		LeadBackend:   req.LeadBackend,
		LeadModel:     req.LeadModel,
		LeadAgentID:   req.GetLeadAgentId(),
		ProjectID:     req.ProjectId,
		MaxIterations: int(req.MaxIterations),
		PollInterval:  req.PollInterval,
		WorkflowName:  req.GetWorkflowName(),
		WorkflowFile:  req.GetWorkflowFile(),
	}

	resp, err := s.agentSvc.StartJob(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.OrchestratorStartJobResponse{
		JobId:     resp.JobID,
		Timestamp: timestamppb.New(time.Unix(resp.Timestamp, 0)),
	}, nil
}

// GetJobStatus returns the current status of an orchestration job.
func (s *OrchestratorServer) GetJobStatus(ctx context.Context, req *pb.OrchestratorGetJobStatusRequest) (*pb.OrchestratorGetJobStatusResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing GetJobStatus request")

	if req.JobId == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	jobStatus, err := s.agentSvc.GetJobStatus(tenantCtx, req.TenantId, req.JobId)
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
	}, nil
}

// CancelJob cancels a running orchestration job.
func (s *OrchestratorServer) CancelJob(ctx context.Context, req *pb.OrchestratorCancelJobRequest) (*pb.OrchestratorCancelJobResponse, error) {
	s.logger.WithField("job_id", req.JobId).Debug("Processing CancelJob request")

	if req.JobId == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	if err := s.agentSvc.CancelJob(tenantCtx, req.TenantId, req.JobId); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.OrchestratorCancelJobResponse{
		JobId:     req.JobId,
		Cancelled: true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// ListJobs returns all jobs for a tenant.
func (s *OrchestratorServer) ListJobs(ctx context.Context, req *pb.OrchestratorListJobsRequest) (*pb.OrchestratorListJobsResponse, error) {
	s.logger.Debug("Processing ListJobs request")

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

	jobs, err := s.agentSvc.ListJobs(tenantCtx, req.GetTenantId())
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

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

	return &pb.OrchestratorListJobsResponse{
		Jobs: pbJobs,
	}, nil
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

	tenantCtx := orchestration.ContextWithTenant(stream.Context(), req.GetTenantId())

	eventCh, err := s.agentSvc.SubscribeJobEvents(tenantCtx, req.TenantId, req.JobId)
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

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
// Health Operations
// =============================================================================

// Health performs a health check on the orchestrator.
func (s *OrchestratorServer) Health(ctx context.Context, req *pb.OrchestratorHealthRequest) (*pb.OrchestratorHealthResponse, error) {
	s.logger.Debug("Processing Health request")

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

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

// RegisterAgent registers a new agent in the fleet registry.
func (s *OrchestratorServer) RegisterAgent(ctx context.Context, req *pb.RegisterAgentRequest) (*pb.RegisterAgentResponse, error) {
	s.logger.Debug("Processing RegisterAgent request")

	svcReq := &orchestration.RegisterAgentRequest{
		TenantID:     req.TenantId,
		Name:         req.Name,
		Host:         req.Host,
		Backend:      req.Backend,
		AgentType:    req.AgentType,
		Capabilities: req.Capabilities,
		WorkDir:      req.WorkDir,
		Metadata:     req.Metadata,
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.TenantId)

	resp, err := s.agentSvc.RegisterAgent(tenantCtx, svcReq)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.RegisterAgentResponse{
		Agent:     serviceAgentRegistrationToProto(resp.Agent),
		Timestamp: timestamppb.Now(),
	}, nil
}

// GetAgent retrieves a registered agent by ID.
func (s *OrchestratorServer) GetAgent(ctx context.Context, req *pb.GetAgentRequest) (*pb.GetAgentResponse, error) {
	s.logger.WithField("agent_id", req.AgentId).Debug("Processing GetAgent request")

	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.TenantId)

	agent, err := s.agentSvc.GetAgent(tenantCtx, req.TenantId, req.AgentId)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.GetAgentResponse{
		Agent:     serviceAgentRegistrationToProto(agent),
		Timestamp: timestamppb.Now(),
	}, nil
}

// ListAgents lists agents matching optional filter criteria within a tenant.
func (s *OrchestratorServer) ListAgents(ctx context.Context, req *pb.ListAgentsRequest) (*pb.ListAgentsResponse, error) {
	s.logger.Debug("Processing ListAgents request")

	var filter *orchestration.AgentFilter
	if req.Filter != nil {
		filter = &orchestration.AgentFilter{
			Host:      req.Filter.Host,
			Status:    protoAgentStatusToService(req.Filter.Status),
			AgentType: req.Filter.AgentType,
			Backend:   req.Filter.Backend,
			Limit:     int(req.Filter.Limit),
		}
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.TenantId)

	agents, err := s.agentSvc.ListAgents(tenantCtx, req.TenantId, filter)
	if err != nil {
		return nil, convertOrchestratorError(err)
	}

	pbAgents := make([]*pb.AgentRegistration, len(agents))
	for i, agent := range agents {
		pbAgents[i] = serviceAgentRegistrationToProto(agent)
	}

	return &pb.ListAgentsResponse{
		Agents:    pbAgents,
		Count:     int32(len(agents)), //nolint:gosec // G115: bounded count
		Timestamp: timestamppb.Now(),
	}, nil
}

// RemoveAgent removes an agent from the fleet registry.
func (s *OrchestratorServer) RemoveAgent(ctx context.Context, req *pb.RemoveAgentRequest) (*pb.RemoveAgentResponse, error) {
	s.logger.WithField("agent_id", req.AgentId).Debug("Processing RemoveAgent request")

	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.TenantId)

	if err := s.agentSvc.RemoveAgent(tenantCtx, req.TenantId, req.AgentId); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.RemoveAgentResponse{
		AgentId:   req.AgentId,
		Removed:   true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// AgentHeartbeat updates an agent's last seen timestamp.
func (s *OrchestratorServer) AgentHeartbeat(ctx context.Context, req *pb.AgentHeartbeatRequest) (*pb.AgentHeartbeatResponse, error) {
	s.logger.WithField("agent_id", req.AgentId).Debug("Processing AgentHeartbeat request")

	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	tenantCtx := orchestration.ContextWithTenant(ctx, req.TenantId)

	if err := s.agentSvc.AgentHeartbeat(tenantCtx, req.TenantId, req.AgentId); err != nil {
		return nil, convertOrchestratorError(err)
	}

	return &pb.AgentHeartbeatResponse{
		AgentId:      req.AgentId,
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
	default:
		return ""
	}
}

// protoAgentStatusToService converts a proto AgentStatus to the domain AgentStatus.
func protoAgentStatusToService(as pb.AgentStatus) orchestration.AgentStatus {
	switch as {
	case pb.AgentStatus_AGENT_STATUS_ONLINE:
		return orchestration.AgentStatusRunning
	case pb.AgentStatus_AGENT_STATUS_OFFLINE:
		return orchestration.AgentStatusOffline
	case pb.AgentStatus_AGENT_STATUS_BUSY:
		return orchestration.AgentStatusRunning
	case pb.AgentStatus_AGENT_STATUS_ERROR:
		return orchestration.AgentStatusError
	case pb.AgentStatus_AGENT_STATUS_DRAINING:
		return orchestration.AgentStatusIdle
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
	default:
		return pb.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}

// serviceAgentStatusToProto converts a domain AgentStatus to proto AgentStatus.
func serviceAgentStatusToProto(as orchestration.AgentStatus) pb.AgentStatus {
	switch as {
	case orchestration.AgentStatusRegistered:
		return pb.AgentStatus_AGENT_STATUS_ONLINE
	case orchestration.AgentStatusRunning:
		return pb.AgentStatus_AGENT_STATUS_BUSY
	case orchestration.AgentStatusIdle:
		return pb.AgentStatus_AGENT_STATUS_ONLINE
	case orchestration.AgentStatusOffline:
		return pb.AgentStatus_AGENT_STATUS_OFFLINE
	case orchestration.AgentStatusError:
		return pb.AgentStatus_AGENT_STATUS_ERROR
	default:
		return pb.AgentStatus_AGENT_STATUS_UNSPECIFIED
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
		CreatedAt:    timestamppb.New(time.Unix(info.CreatedAt, 0)),
		LastActivity: timestamppb.New(time.Unix(info.LastActivity, 0)),
	}
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
		CreatedAt:    timestamppb.New(time.Unix(task.CreatedAt, 0)),
		UpdatedAt:    timestamppb.New(time.Unix(task.UpdatedAt, 0)),
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
		pbTask.ClaimedAt = timestamppb.New(time.Unix(task.ClaimedAt, 0))
	}

	if task.CompletedAt != 0 {
		pbTask.CompletedAt = timestamppb.New(time.Unix(task.CompletedAt, 0))
	}

	if task.AutoSpawn {
		pbTask.AutoSpawn = true
	}
	if task.SpawnedJobID != "" {
		pbTask.SpawnedJobId = task.SpawnedJobID
	}
	if task.SpawnedAt != 0 {
		pbTask.SpawnedAt = timestamppb.New(time.Unix(0, task.SpawnedAt))
	}

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
		Timestamp:     timestamppb.New(time.Unix(report.Timestamp, 0)),
		TotalSessions: int32(report.TotalSessions), //nolint:gosec // G115: bounded count
		Healthy:       int32(report.Healthy),       //nolint:gosec // G115: bounded count
		Unhealthy:     int32(report.Unhealthy),     //nolint:gosec // G115: bounded count
		Closed:        int32(report.Closed),        //nolint:gosec // G115: bounded count
		Results:       results,
	}
}

// serviceAgentRegistrationToProto converts a domain AgentRegistration to proto AgentRegistration.
func serviceAgentRegistrationToProto(agent *orchestration.AgentRegistration) *pb.AgentRegistration {
	if agent == nil {
		return nil
	}
	return &pb.AgentRegistration{
		Id:           agent.ID,
		TenantId:     agent.TenantID,
		Name:         agent.Name,
		Host:         agent.Host,
		Backend:      agent.Backend,
		AgentType:    agent.AgentType,
		Status:       serviceAgentStatusToProto(agent.Status),
		Capabilities: agent.Capabilities,
		ContainerId:  agent.ContainerID,
		WorkDir:      agent.WorkDir,
		LastSeen:     timestamppb.New(time.Unix(agent.LastSeen, 0)),
		RegisteredAt: timestamppb.New(time.Unix(agent.RegisteredAt, 0)),
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

	// Check orchestration-specific typed errors
	var agentNotFoundErr *orchestration.AgentNotFoundError
	if stderrors.As(err, &agentNotFoundErr) {
		return status.Error(codes.NotFound, agentNotFoundErr.Error())
	}

	var agentAlreadyExistsErr *orchestration.AgentAlreadyExistsError
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

	var agentSvcErr *orchestration.AgentServiceError
	if stderrors.As(err, &agentSvcErr) {
		return status.Error(codes.Internal, agentSvcErr.Error())
	}

	var agentStoreErr *orchestration.AgentStoreError
	if stderrors.As(err, &agentStoreErr) {
		return status.Error(codes.Internal, agentStoreErr.Error())
	}

	var agentRegistryErr *orchestration.AgentRegistryError
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
		stderrors.Is(err, orchestration.ErrAgentRegistryInvalidInput) ||
		stderrors.Is(err, orchestration.ErrTaskStoreInvalidInput) {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	if stderrors.Is(err, orchestration.ErrAgentStoreNotFound) ||
		stderrors.Is(err, orchestration.ErrAgentRegistryNotFound) ||
		stderrors.Is(err, orchestration.ErrTaskNotFound) ||
		stderrors.Is(err, orchestration.ErrProjectNotFound) ||
		stderrors.Is(err, orchestration.ErrJobNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}

	if stderrors.Is(err, orchestration.ErrAgentRegistryAlreadyExists) ||
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

	tenantCtx := orchestration.ContextWithTenant(ctx, req.GetTenantId())

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

	tenantID := req.GetTenantId()
	tenantCtx := orchestration.ContextWithTenant(ctx, tenantID)

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

	tenantID := req.GetTenantId()
	tenantCtx := orchestration.ContextWithTenant(ctx, tenantID)

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

	tenantID := req.GetTenantId()
	tenantCtx := orchestration.ContextWithTenant(ctx, tenantID)

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
