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
	"io"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// Orchestrator service prefix for routing.
const orchestratorServicePrefix = "quicbaas.Orchestrator"

// isOrchestratorService checks if the service name is an Orchestrator service.
func isOrchestratorService(svc string) bool {
	return strings.HasPrefix(svc, orchestratorServicePrefix)
}

// orchestratorHandlerFunc converts an SDK request to a proto request, invokes
// gRPC, and converts the proto response back to an SDK response type.
type orchestratorHandlerFunc func(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error)

// orchestratorMethodMap maps SDK method names to handler functions for O(1) dispatch.
var orchestratorMethodMap = map[string]orchestratorHandlerFunc{
	"Health":               handleOrchestratorHealth,
	"ListBackends":         handleOrchestratorListBackends,
	"SpawnSession":         handleOrchestratorSpawnSession,
	"PromptSession":        handleOrchestratorPromptSession,
	"SessionStatus":        handleOrchestratorSessionStatus,
	"CloseSession":         handleOrchestratorCloseSession,
	"RotateSessionAccount": handleOrchestratorRotateSessionAccount,
	"ListSessions":         handleOrchestratorListSessions,
	"CreateTask":           handleOrchestratorCreateTask,
	"GetTask":              handleOrchestratorGetTask,
	"ListTasks":            handleOrchestratorListTasks,
	"ClaimTask":            handleOrchestratorClaimTask,
	"CompleteTask":         handleOrchestratorCompleteTask,
	"RegisterAgent":        handleOrchestratorRegisterAgent,
	"GetAgent":             handleOrchestratorGetAgent,
	"ListAgents":           handleOrchestratorListAgents,
	"RemoveAgent":          handleOrchestratorRemoveAgent,
	"AgentHeartbeat":       handleOrchestratorAgentHeartbeat,
	"DecomposeTask":        handleOrchestratorDecomposeTask,
	"SpawnChildJob":        handleOrchestratorSpawnChildJob,
	"StartJob":             handleOrchestratorStartJob,
	"GetJobStatus":         handleOrchestratorGetJobStatus,
	"ListJobs":             handleOrchestratorListJobs,
	"CancelJob":            handleOrchestratorCancelJob,
	"GetSessionHistory":    handleOrchestratorGetSessionHistory,
	"Inference":            handleOrchestratorInference,
}

// handleOrchestratorGetSessionHistory is the one-shot gRPC handler for a
// page of session event history. For now we implement this by calling the
// streaming SubscribeSessionEvents with follow=false and collecting until
// the stream closes — spares us from introducing a second RPC while the
// event surface is still maturing. When history pagination becomes a hot
// path the server-side can add a dedicated non-streaming RPC.
func handleOrchestratorGetSessionHistory(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetSessionHistoryRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.GetSessionHistory", "invalid request type")
	}
	tenantID := transport.TenantFromContext(ctx)
	if sdkReq.TenantID == "" {
		sdkReq.TenantID = tenantID
	}
	protoReq := &pb.SubscribeSessionRequest{
		TenantId:     sdkReq.TenantID,
		SessionId:    sdkReq.SessionID,
		FromSequence: sdkReq.FromSequence,
		Follow:       false,
	}
	desc := &grpc.StreamDesc{StreamName: "SubscribeSessionEvents", ServerStreams: true}
	cs, err := conn.NewStream(ctx, desc, pb.OrchestratorService_SubscribeSessionEvents_FullMethodName)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.GetSessionHistory")
	}
	if err := cs.SendMsg(protoReq); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.GetSessionHistory")
	}
	if err := cs.CloseSend(); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.GetSessionHistory")
	}
	out := &orchestrator.GetSessionHistoryResponse{}
	limit := sdkReq.Limit
	for {
		var env pb.SessionEventEnvelope
		if err := cs.RecvMsg(&env); err != nil {
			if err == io.EOF {
				break // EOF ends the history page normally
			}
			// A genuine stream error must surface, not be silently treated
			// as end-of-page (which would return a truncated history).
			return nil, convertGRPCError(err, "grpc.orchestrator.GetSessionHistory")
		}
		item := &orchestrator.SessionEventEnvelope{
			SessionID:   env.SessionId,
			TenantID:    env.TenantId,
			Sequence:    env.Sequence,
			PayloadJSON: env.PayloadJson,
			FromReplay:  env.FromReplay,
		}
		if env.Timestamp != nil {
			item.Timestamp = env.Timestamp.AsTime()
		}
		out.Events = append(out.Events, item)
		if limit > 0 && len(out.Events) >= limit {
			out.NextSeq = env.Sequence + 1
			break
		}
	}
	return out, nil
}

// orchestratorHandler handles Orchestrator service requests by converting between
// SDK types and protobuf types for native gRPC invocation.
type orchestratorHandler struct {
	conn *grpc.ClientConn
}

// newOrchestratorHandler creates a new orchestrator handler.
func newOrchestratorHandler(conn *grpc.ClientConn) *orchestratorHandler {
	return &orchestratorHandler{conn: conn}
}

// handleRequest routes Orchestrator requests by dispatching to the appropriate
// handler function which performs SDK-to-proto conversion, gRPC invocation,
// and proto-to-SDK conversion.
func (h *orchestratorHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	handler, ok := orchestratorMethodMap[method]
	if !ok {
		return nil, errors.NewUnimplemented("grpc.orchestrator."+method,
			"unsupported orchestrator method: "+method)
	}
	return handler(ctx, h.conn, req)
}

// =============================================================================
// Health
// =============================================================================

func handleOrchestratorHealth(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.OrchestratorHealthRequest{
		TenantId: tenantID,
	}
	protoResp := &pb.OrchestratorHealthResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_Health_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.Health")
	}

	return healthReportFromProto(protoResp.GetReport()), nil
}

// =============================================================================
// ListBackends
// =============================================================================

func handleOrchestratorListBackends(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.OrchestratorListBackendsRequest{
		TenantId: tenantID,
	}
	protoResp := &pb.OrchestratorListBackendsResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_ListBackends_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.ListBackends")
	}

	backends := make([]*orchestrator.BackendInfo, 0, len(protoResp.GetBackends()))
	for _, b := range protoResp.GetBackends() {
		backends = append(backends, &orchestrator.BackendInfo{
			Name:      b.GetName(),
			Type:      b.GetType(),
			Status:    "available",
			Available: true,
		})
	}

	return &orchestrator.ListBackendsResponse{
		Backends: backends,
		Count:    len(backends),
	}, nil
}

// =============================================================================
// Session Operations
// =============================================================================

func handleOrchestratorSpawnSession(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SpawnSessionRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.SpawnSession", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.SpawnSessionRequest{
		Backend:            sdkReq.Backend,
		ProjectId:          sdkReq.ProjectID,
		Model:              sdkReq.Model,
		WorkDir:            sdkReq.WorkDir,
		Env:                sdkReq.Env,
		SandboxMode:        pb.SandboxMode(sdkReq.SandboxMode),
		Sandboxed:          sdkReq.Sandboxed,
		HostAffinity:       sdkReq.HostAffinity,
		HostAffinityStrict: sdkReq.HostAffinityStrict,
		TenantId:           tenantID,

		// Pooled provider account selection. AccountAuto is the only way to
		// reach the dynamic (least-usage) strategy, so it must cross the wire.
		AccountId:       sdkReq.AccountID,
		AccountAuto:     sdkReq.AccountAuto,
		AccountStrategy: sdkReq.AccountStrategy,
		// AccountAllow is the second thing that authorizes rotation, so it has
		// to cross the wire alongside AccountAuto.
		AccountAllow: sdkReq.AccountAllow,
	}
	protoResp := &pb.SpawnSessionResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_SpawnSession_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.SpawnSession")
	}

	return &orchestrator.SpawnSessionResponse{
		SessionID:   protoResp.GetSessionId(),
		Backend:     protoResp.GetBackend(),
		ContainerID: protoResp.GetContainerId(),
	}, nil
}

func handleOrchestratorPromptSession(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.PromptSessionRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.PromptSession", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.PromptSessionRequest{
		SessionId: sdkReq.SessionID,
		Prompt:    sdkReq.Prompt,
		TenantId:  tenantID,
	}
	protoResp := &pb.PromptSessionResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_PromptSession_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.PromptSession")
	}

	// Dispatch on status through the shared converters so gRPC applies the
	// same acceptance and turn-completion rules as the JSON transports.
	if protoResp.GetStatus() == orchestrator.TurnStatusAccepted {
		return orchestrator.NewPromptSessionAccepted(
			protoResp.GetSessionId(),
			protoResp.GetTurnId(),
			protoResp.GetSequence(),
			protoResp.GetModel(),
		)
	}

	blocks := make([]orchestrator.PromptContentBlock, 0, len(protoResp.GetContent()))
	for _, block := range protoResp.GetContent() {
		blocks = append(blocks, orchestrator.PromptContentBlock{
			Type: block.GetType(),
			Text: block.GetText(),
		})
	}

	return orchestrator.NewPromptSessionResponse(
		protoResp.GetSessionId(),
		blocks,
		protoResp.GetModel(),
		protoResp.GetStopReason(),
	)
}

func handleOrchestratorSessionStatus(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SessionStatusRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.SessionStatus", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.SessionStatusRequest{
		SessionId: sdkReq.SessionID,
		TenantId:  tenantID,
	}
	protoResp := &pb.SessionStatusResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_SessionStatus_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.SessionStatus")
	}

	return &orchestrator.SessionStatusResponse{
		SessionID:  protoResp.GetSessionId(),
		Status:     protoResp.GetState().String(),
		CreatedAt:  timestampToUnix(protoResp.GetTimestamp()),
		LastActive: timestampToUnix(protoResp.GetLastActivity()),
	}, nil
}

func handleOrchestratorCloseSession(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.CloseSessionRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.CloseSession", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.CloseSessionRequest{
		SessionId: sdkReq.SessionID,
		TenantId:  tenantID,
	}
	protoResp := &pb.CloseSessionResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_CloseSession_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.CloseSession")
	}

	return &orchestrator.CloseSessionResponse{
		SessionID: protoResp.GetSessionId(),
		Closed:    true,
	}, nil
}

// handleOrchestratorRotateSessionAccount moves a session onto a different
// pooled provider account. AvailableAt crosses the wire as Unix seconds
// (available_at_unix), matching how the account surface already carries the
// same value; the zero time stays zero so "unset" survives the round trip.
func handleOrchestratorRotateSessionAccount(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.RotateSessionAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.RotateSessionAccount", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.RotateSessionAccountRequest{
		SessionId:       sdkReq.SessionID,
		TenantId:        tenantID,
		Reason:          sdkReq.Reason,
		HoldOutgoing:    sdkReq.HoldOutgoing,
		AvailableAtUnix: unixOrZero(sdkReq.AvailableAt),
	}
	protoResp := &pb.RotateSessionAccountResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_RotateSessionAccount_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.RotateSessionAccount")
	}

	return &orchestrator.RotateSessionAccountResponse{
		SessionID:         protoResp.GetSessionId(),
		PreviousAccountID: protoResp.GetPreviousAccountId(),
		AccountID:         protoResp.GetAccountId(),
		Strategy:          protoResp.GetStrategy(),
		Mode:              protoResp.GetMode(),
	}, nil
}

// unixOrZero is the inverse of timeFromUnix (account_handler.go): the zero
// time stays 0 on the wire so "unset" is not sent as 1970-01-01.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func handleOrchestratorListSessions(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListSessionsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.ListSessions", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.ListSessionsRequest{
		Backend:  sdkReq.Backend,
		TenantId: tenantID,
	}
	protoResp := &pb.ListSessionsResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_ListSessions_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.ListSessions")
	}

	sessions := make([]*orchestrator.SessionStatusResponse, 0, len(protoResp.GetSessions()))
	for _, s := range protoResp.GetSessions() {
		sessions = append(sessions, &orchestrator.SessionStatusResponse{
			SessionID:  s.GetSessionId(),
			Backend:    s.GetBackend(),
			Status:     s.GetState().String(),
			CreatedAt:  timestampToUnix(s.GetCreatedAt()),
			LastActive: timestampToUnix(s.GetLastActivity()),
		})
	}

	return &orchestrator.ListSessionsResponse{
		Sessions: sessions,
		Count:    len(sessions),
	}, nil
}

// =============================================================================
// Task Operations
// =============================================================================

func handleOrchestratorCreateTask(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.CreateTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.CreateTask", "invalid request type")
	}

	protoReq := &pb.CreateTaskRequest{
		TenantId:     sdkReq.TenantID,
		TeamName:     sdkReq.TeamName,
		Subject:      sdkReq.Subject,
		Description:  sdkReq.Description,
		AssignedTo:   sdkReq.AssignedTo,
		Dependencies: sdkReq.Dependencies,
		Metadata:     sdkReq.Metadata,
		ProjectId:    sdkReq.ProjectID,
		AutoSpawn:    sdkReq.AutoSpawn,
		Backend:      sdkReq.Backend,
		GpuRequired:  sdkReq.GpuRequired,
	}
	protoResp := &pb.CreateTaskResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_CreateTask_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.CreateTask")
	}

	return taskFromProto(protoResp.GetTask()), nil
}

func handleOrchestratorGetTask(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.GetTask", "invalid request type")
	}

	protoReq := &pb.GetTaskRequest{
		TenantId: sdkReq.TenantID,
		TaskId:   sdkReq.TaskID,
	}
	protoResp := &pb.GetTaskResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_GetTask_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.GetTask")
	}

	return taskFromProto(protoResp.GetTask()), nil
}

func handleOrchestratorListTasks(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListTasksRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.ListTasks", "invalid request type")
	}

	protoReq := &pb.ListTasksRequest{
		TenantId: sdkReq.TenantID,
	}

	// Build filter if any filter fields are set.
	if sdkReq.Status != "" || sdkReq.TeamName != "" || sdkReq.AssignedTo != "" {
		filter := &pb.TaskFilter{
			TeamName:   sdkReq.TeamName,
			AssignedTo: sdkReq.AssignedTo,
		}
		if sdkReq.Status != "" {
			if val, exists := pb.TaskStatus_value[taskStatusToProtoEnum(sdkReq.Status)]; exists {
				filter.Status = pb.TaskStatus(val)
			}
		}
		protoReq.Filter = filter
	}

	protoResp := &pb.ListTasksResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_ListTasks_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.ListTasks")
	}

	tasks := make([]*orchestrator.Task, 0, len(protoResp.GetTasks()))
	for _, t := range protoResp.GetTasks() {
		tasks = append(tasks, taskFromProto(t))
	}

	return &orchestrator.ListTasksResponse{
		Tasks: tasks,
		Count: len(tasks),
	}, nil
}

func handleOrchestratorClaimTask(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ClaimTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.ClaimTask", "invalid request type")
	}

	protoReq := &pb.ClaimTaskRequest{
		TenantId: sdkReq.TenantID,
		TaskId:   sdkReq.TaskID,
		AgentId:  sdkReq.AgentID,
	}
	protoResp := &pb.ClaimTaskResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_ClaimTask_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.ClaimTask")
	}

	return &orchestrator.ClaimTaskResponse{
		TaskID:  protoResp.GetTaskId(),
		AgentID: protoResp.GetAgentId(),
		Claimed: protoResp.GetClaimed(),
	}, nil
}

func handleOrchestratorCompleteTask(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.CompleteTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.CompleteTask", "invalid request type")
	}

	protoReq := &pb.CompleteTaskRequest{
		TenantId: sdkReq.TenantID,
		TaskId:   sdkReq.TaskID,
	}
	if sdkReq.Result != nil {
		protoReq.Result = &pb.TaskResult{
			Output:   sdkReq.Result.Output,
			Files:    sdkReq.Result.Files,
			Metadata: sdkReq.Result.Metadata,
		}
	}
	protoResp := &pb.CompleteTaskResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_CompleteTask_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.CompleteTask")
	}

	return &orchestrator.CompleteTaskResponse{
		TaskID:    protoResp.GetTaskId(),
		Completed: protoResp.GetCompleted(),
	}, nil
}

// =============================================================================
// Agent Fleet Operations
// =============================================================================

func handleOrchestratorRegisterAgent(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.RegisterAgentRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.RegisterAgent", "invalid request type")
	}

	protoReq := &pb.RegisterAgentRequest{
		TenantId:  sdkReq.TenantID,
		Name:      sdkReq.Name,
		Host:      sdkReq.Host,
		Backend:   sdkReq.Backend,
		AgentType: sdkReq.AgentType,
		WorkDir:   sdkReq.WorkDir,
		Metadata:  sdkReq.Metadata,
	}
	protoResp := &pb.RegisterAgentResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_RegisterAgent_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.RegisterAgent")
	}

	return &orchestrator.RegisterAgentResponse{
		Agent: agentRegistrationFromProto(protoResp.GetAgent()),
	}, nil
}

func handleOrchestratorGetAgent(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetAgentRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.GetAgent", "invalid request type")
	}

	protoReq := &pb.GetAgentRequest{
		TenantId: sdkReq.TenantID,
		AgentId:  sdkReq.AgentID,
	}
	protoResp := &pb.GetAgentResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_GetAgent_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.GetAgent")
	}

	return agentRegistrationFromProto(protoResp.GetAgent()), nil
}

func handleOrchestratorListAgents(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListAgentsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.ListAgents", "invalid request type")
	}

	protoReq := &pb.ListAgentsRequest{
		TenantId: sdkReq.TenantID,
	}

	// Build filter if any filter fields are set.
	if sdkReq.Host != "" || sdkReq.Status != "" || sdkReq.AgentType != "" || sdkReq.Backend != "" {
		filter := &pb.AgentFilter{
			Host:      sdkReq.Host,
			AgentType: sdkReq.AgentType,
			Backend:   sdkReq.Backend,
		}
		if sdkReq.Status != "" {
			if val, exists := pb.AgentStatus_value[agentStatusToProtoEnum(sdkReq.Status)]; exists {
				filter.Status = pb.AgentStatus(val)
			}
		}
		protoReq.Filter = filter
	}

	protoResp := &pb.ListAgentsResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_ListAgents_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.ListAgents")
	}

	agents := make([]*orchestrator.AgentRegistration, 0, len(protoResp.GetAgents()))
	for _, a := range protoResp.GetAgents() {
		agents = append(agents, agentRegistrationFromProto(a))
	}

	return &orchestrator.ListAgentsResponse{
		Agents: agents,
		Count:  len(agents),
	}, nil
}

func handleOrchestratorRemoveAgent(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.RemoveAgentRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.RemoveAgent", "invalid request type")
	}

	protoReq := &pb.RemoveAgentRequest{
		TenantId: sdkReq.TenantID,
		AgentId:  sdkReq.AgentID,
	}
	protoResp := &pb.RemoveAgentResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_RemoveAgent_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.RemoveAgent")
	}

	return &orchestrator.RemoveAgentResponse{
		AgentID: protoResp.GetAgentId(),
		Removed: protoResp.GetRemoved(),
	}, nil
}

func handleOrchestratorAgentHeartbeat(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.AgentHeartbeatRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.AgentHeartbeat", "invalid request type")
	}

	protoReq := &pb.AgentHeartbeatRequest{
		TenantId: sdkReq.TenantID,
		AgentId:  sdkReq.AgentID,
	}
	protoResp := &pb.AgentHeartbeatResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_AgentHeartbeat_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.AgentHeartbeat")
	}

	return &orchestrator.AgentHeartbeatResponse{
		AgentID:   protoResp.GetAgentId(),
		Heartbeat: protoResp.GetAcknowledged(),
	}, nil
}

// =============================================================================
// Task Decomposition
// =============================================================================

func handleOrchestratorDecomposeTask(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.DecomposeTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.DecomposeTask", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.DecomposeTaskRequest{
		Backend:    sdkReq.Backend,
		Model:      sdkReq.Model,
		Task:       sdkReq.Task,
		ProjectIds: sdkReq.ProjectIDs,
		TenantId:   tenantID,
	}
	protoResp := &pb.DecomposeTaskResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_DecomposeTask_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.DecomposeTask")
	}

	workItems := make([]*orchestrator.WorkItem, 0, len(protoResp.GetWorkItems()))
	for _, item := range protoResp.GetWorkItems() {
		workItems = append(workItems, &orchestrator.WorkItem{
			ID:           item.GetId(),
			ProjectID:    item.GetProjectId(),
			Title:        item.GetTitle(),
			Description:  item.GetDescription(),
			Priority:     int(item.GetPriority()),
			Dependencies: item.GetDependencies(),
		})
	}

	return &orchestrator.DecomposeTaskResponse{
		WorkItems: workItems,
		Timestamp: timestampToUnix(protoResp.GetTimestamp()),
	}, nil
}

// =============================================================================
// Job Operations
// =============================================================================

// handleOrchestratorSpawnChildJob converts an SDK SpawnChildJobRequest to proto,
// invokes the gRPC SpawnChildJob RPC, and returns the child job ID.
func handleOrchestratorSpawnChildJob(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SpawnChildJobRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.SpawnChildJob", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	var tasks []*pb.TaskSpec
	for _, t := range sdkReq.Tasks {
		tasks = append(tasks, &pb.TaskSpec{
			Title:       t.Title,
			Description: t.Description,
		})
	}

	protoReq := &pb.SpawnChildJobRequest{
		ParentJobId:        sdkReq.ParentJobID,
		TenantId:           tenantID,
		WorkflowId:         sdkReq.WorkflowID,
		ProjectId:          sdkReq.ProjectID,
		Tasks:              tasks,
		HostAffinity:       sdkReq.HostAffinity,
		GpuRequired:        sdkReq.GpuRequired,
		HostAffinityStrict: sdkReq.HostAffinityStrict,
	}
	protoResp := &pb.SpawnChildJobResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_SpawnChildJob_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.SpawnChildJob")
	}

	return &orchestrator.SpawnChildJobResponse{
		ChildJobID: protoResp.GetChildJobId(),
	}, nil
}

// handleOrchestratorStartJob converts an SDK StartJobRequest to proto, invokes
// the gRPC StartJob RPC, and converts the proto response back to the SDK type.
func handleOrchestratorStartJob(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.StartJobRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.StartJob", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.OrchestratorStartJobRequest{
		TenantId:      tenantID,
		Description:   sdkReq.Description,
		LeadBackend:   sdkReq.LeadBackend,
		LeadModel:     sdkReq.LeadModel,
		LeadAgentId:   sdkReq.LeadAgentID,
		ProjectId:     sdkReq.ProjectID,
		MaxIterations: int32(sdkReq.MaxIterations), //nolint:gosec // G115: bounded count
		PollInterval:  sdkReq.PollInterval,
		WorkflowName:  sdkReq.WorkflowName,
		WorkflowFile:  sdkReq.WorkflowFile,
	}
	protoResp := &pb.OrchestratorStartJobResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_StartJob_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.StartJob")
	}

	return &orchestrator.StartJobResponse{
		JobID:     protoResp.GetJobId(),
		Timestamp: timestampToUnix(protoResp.GetTimestamp()),
	}, nil
}

// handleOrchestratorGetJobStatus converts an SDK GetJobStatusRequest to proto,
// invokes the gRPC GetJobStatus RPC, and converts the proto response back to
// the SDK JobStatus type.
func handleOrchestratorGetJobStatus(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetJobStatusRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.GetJobStatus", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.OrchestratorGetJobStatusRequest{
		TenantId: tenantID,
		JobId:    sdkReq.JobID,
	}
	protoResp := &pb.OrchestratorGetJobStatusResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_GetJobStatus_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.GetJobStatus")
	}

	// Map audit events from proto to SDK types.
	var events []orchestrator.JobAuditEvent
	if pbEvents := protoResp.GetEvents(); len(pbEvents) > 0 {
		events = make([]orchestrator.JobAuditEvent, len(pbEvents))
		for i, e := range pbEvents {
			events[i] = orchestrator.JobAuditEvent{
				Timestamp: e.GetTimestamp(),
				Type:      e.GetType(),
				Details:   e.GetDetails(),
			}
		}
	}

	return &orchestrator.JobStatus{
		JobID:       protoResp.GetJobId(),
		State:       protoResp.GetState(),
		Iteration:   int(protoResp.GetIteration()),
		TaskCount:   int(protoResp.GetTaskCount()),
		Completed:   int(protoResp.GetCompleted()),
		Failed:      int(protoResp.GetFailed()),
		Description: protoResp.GetDescription(),
		StartedAt:   protoResp.GetStartedAt(),
		UpdatedAt:   protoResp.GetUpdatedAt(),
		Error:       protoResp.GetError(),
		Events:      events,
	}, nil
}

// handleOrchestratorListJobs converts an SDK ListJobsRequest to proto,
// invokes the gRPC ListJobs RPC, and converts the proto response back to
// the SDK ListJobsResponse type.
func handleOrchestratorListJobs(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListJobsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.ListJobs", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.OrchestratorListJobsRequest{
		TenantId: tenantID,
	}
	_ = sdkReq // tenantID comes from context

	protoResp := &pb.OrchestratorListJobsResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_ListJobs_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.ListJobs")
	}

	jobs := make([]orchestrator.JobSummary, len(protoResp.GetJobs()))
	for i, j := range protoResp.GetJobs() {
		jobs[i] = orchestrator.JobSummary{
			JobID:       j.GetJobId(),
			State:       j.GetState(),
			Iteration:   int(j.GetIteration()),
			TaskCount:   int(j.GetTaskCount()),
			Completed:   int(j.GetCompleted()),
			Failed:      int(j.GetFailed()),
			Description: j.GetDescription(),
			StartedAt:   j.GetStartedAt(),
			UpdatedAt:   j.GetUpdatedAt(),
			Error:       j.GetError(),
			EventCount:  int(j.GetEventCount()),
		}
	}

	return &orchestrator.ListJobsResponse{
		Jobs: jobs,
	}, nil
}

// handleOrchestratorCancelJob converts an SDK CancelJobRequest to proto,
// invokes the gRPC CancelJob RPC, and converts the proto response back to
// the SDK CancelJobResponse type.
func handleOrchestratorCancelJob(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.CancelJobRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.CancelJob", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.OrchestratorCancelJobRequest{
		TenantId: tenantID,
		JobId:    sdkReq.JobID,
	}
	protoResp := &pb.OrchestratorCancelJobResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_CancelJob_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.CancelJob")
	}

	return &orchestrator.CancelJobResponse{
		JobID:     protoResp.GetJobId(),
		Cancelled: protoResp.GetCancelled(),
	}, nil
}

// =============================================================================
// Proto-to-SDK Conversion Helpers
// =============================================================================

// taskFromProto converts a proto Task to an SDK Task.
func taskFromProto(t *pb.Task) *orchestrator.Task {
	if t == nil {
		return nil
	}

	sdkTask := &orchestrator.Task{
		ID:           t.GetId(),
		TenantID:     t.GetTenantId(),
		TeamName:     t.GetTeamName(),
		Subject:      t.GetSubject(),
		Description:  t.GetDescription(),
		Status:       t.GetStatus().String(),
		AssignedTo:   t.GetAssignedTo(),
		Dependencies: t.GetDependencies(),
		CreatedAt:    timestampToUnix(t.GetCreatedAt()),
		ClaimedAt:    timestampToUnix(t.GetClaimedAt()),
		CompletedAt:  timestampToUnix(t.GetCompletedAt()),
		Version:      t.GetVersion(),
		Metadata:     t.GetMetadata(),
	}

	if t.GetResult() != nil {
		sdkTask.Result = &orchestrator.TaskResult{
			Output:   t.GetResult().GetOutput(),
			Files:    t.GetResult().GetFiles(),
			Metadata: t.GetResult().GetMetadata(),
		}
	}

	return sdkTask
}

// agentRegistrationFromProto converts a proto AgentRegistration to an SDK AgentRegistration.
func agentRegistrationFromProto(a *pb.AgentRegistration) *orchestrator.AgentRegistration {
	if a == nil {
		return nil
	}

	return &orchestrator.AgentRegistration{
		ID:           a.GetId(),
		TenantID:     a.GetTenantId(),
		Name:         a.GetName(),
		Host:         a.GetHost(),
		Backend:      a.GetBackend(),
		AgentType:    a.GetAgentType(),
		Status:       a.GetStatus().String(),
		WorkDir:      a.GetWorkDir(),
		LastSeen:     timestampToUnix(a.GetLastSeen()),
		RegisteredAt: timestampToUnix(a.GetRegisteredAt()),
		Version:      a.GetVersion(),
		Metadata:     a.GetMetadata(),
	}
}

// healthReportFromProto converts a proto HealthCheckReport to an SDK HealthCheckReport.
func healthReportFromProto(r *pb.HealthCheckReport) *orchestrator.HealthCheckReport {
	if r == nil {
		return nil
	}

	components := make(map[string]string, len(r.GetResults()))
	for _, result := range r.GetResults() {
		status := "healthy"
		if !result.GetHealthy() {
			status = "unhealthy"
			if result.GetError() != "" {
				status = result.GetError()
			}
		}
		components[result.GetSessionId()] = status
	}

	return &orchestrator.HealthCheckReport{
		Healthy:    r.GetUnhealthy() == 0,
		Status:     healthStatusString(r.GetUnhealthy()),
		Components: components,
		Timestamp:  timestampToUnix(r.GetTimestamp()),
	}
}

// =============================================================================
// Utility Functions
// =============================================================================

// timestampToUnix converts a protobuf Timestamp to Unix epoch seconds.
// Returns 0 for nil timestamps.
func timestampToUnix(ts *timestamppb.Timestamp) int64 {
	if ts == nil {
		return 0
	}
	return ts.AsTime().Unix()
}

// unixToTimestamp converts a Unix epoch seconds value to a protobuf Timestamp.
// Returns nil for zero values.
func unixToTimestamp(unix int64) *timestamppb.Timestamp {
	if unix == 0 {
		return nil
	}
	return timestamppb.New(time.Unix(unix, 0))
}

// taskStatusToProtoEnum converts an SDK status string to a proto enum name.
func taskStatusToProtoEnum(status string) string {
	return "TASK_STATUS_" + strings.ToUpper(strings.ReplaceAll(status, " ", "_"))
}

// agentStatusToProtoEnum converts an SDK status string to a proto enum name.
func agentStatusToProtoEnum(status string) string {
	return "AGENT_STATUS_" + strings.ToUpper(strings.ReplaceAll(status, " ", "_"))
}

// healthStatusString returns a human-readable health status.
func healthStatusString(unhealthy int32) string {
	if unhealthy == 0 {
		return "healthy"
	}
	return "degraded"
}

// =============================================================================
// Inference (unary aggregate)
// =============================================================================

// handleOrchestratorInference calls the gRPC Inference RPC and converts the
// response to the SDK InferenceResponse type.
func handleOrchestratorInference(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.InferenceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.Inference", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.InferenceRequest{
		Backend:      sdkReq.Backend,
		Model:        sdkReq.Model,
		Prompt:       sdkReq.Prompt,
		SystemPrompt: sdkReq.SystemPrompt,
		HostAffinity: sdkReq.HostAffinity,
		TenantId:     tenantID,
		Metadata:     sdkReq.Metadata,
	}
	protoResp := &pb.InferenceResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_Inference_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.Inference")
	}

	content := make([]orchestrator.InferenceContentBlock, 0, len(protoResp.GetContent()))
	for _, b := range protoResp.GetContent() {
		content = append(content, orchestrator.InferenceContentBlock{
			Type: b.GetType(),
			Text: b.GetText(),
		})
	}

	return &orchestrator.InferenceResponse{
		Content:    content,
		Model:      protoResp.GetModel(),
		StopReason: protoResp.GetStopReason(),
	}, nil
}

// =============================================================================
// Streaming Operations
// =============================================================================

// handleStream routes streaming orchestrator requests, converting SDK types
// to proto types for the gRPC call and wrapping the response stream to convert
// proto types back to SDK types.
func (h *orchestratorHandler) handleStream(ctx context.Context, method string, req interface{}, t *Transport, conn *grpc.ClientConn) (transport.Stream, error) {
	switch method {
	case "SubscribeJobEvents":
		return h.handleSubscribeJobEventsStream(ctx, req, t, conn)
	case "SubscribeSessionEvents":
		return h.handleSubscribeSessionEventsStream(ctx, req, t, conn)
	case "InferenceStream":
		return h.handleInferenceStream(ctx, req, t, conn)
	default:
		return nil, errors.NewUnimplemented("grpc.orchestrator."+method,
			"unsupported orchestrator streaming method: "+method)
	}
}

// handleSubscribeSessionEventsStream initiates the server-streaming RPC
// for session events. The server streams SessionEventEnvelope messages;
// this helper wraps the gRPC stream in a transport.Stream that converts
// proto types to SDK types on Recv.
func (h *orchestratorHandler) handleSubscribeSessionEventsStream(ctx context.Context, req interface{}, t *Transport, conn *grpc.ClientConn) (transport.Stream, error) {
	sdkReq, ok := req.(*orchestrator.SubscribeSessionEventsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.SubscribeSessionEvents", "invalid request type")
	}
	tenantID := transport.TenantFromContext(ctx)
	protoReq := &pb.SubscribeSessionRequest{
		TenantId:     tenantID,
		SessionId:    sdkReq.SessionID,
		FromSequence: sdkReq.FromSequence,
		Follow:       sdkReq.Follow,
	}
	desc := &grpc.StreamDesc{
		StreamName:    "SubscribeSessionEvents",
		ServerStreams: true,
	}
	cs, err := conn.NewStream(ctx, desc, pb.OrchestratorService_SubscribeSessionEvents_FullMethodName)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.SubscribeSessionEvents")
	}
	if err := cs.SendMsg(protoReq); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.SubscribeSessionEvents")
	}
	if err := cs.CloseSend(); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.SubscribeSessionEvents")
	}
	return &sessionEventStreamAdapter{stream: cs}, nil
}

// sessionEventStreamAdapter converts proto SessionEventEnvelope to SDK
// orchestrator.SessionEventEnvelope on each Recv. Mirrors
// orchestratorEventStream but for the session events RPC.
type sessionEventStreamAdapter struct {
	stream grpc.ClientStream
	closed atomic.Bool
}

func (s *sessionEventStreamAdapter) Send(msg interface{}) error {
	return errors.NewUnimplemented("grpc.orchestrator.session.Send", "send not supported on server-streaming RPC")
}

func (s *sessionEventStreamAdapter) Recv() (interface{}, error) {
	if s.closed.Load() {
		return nil, errors.NewConnection("grpc.orchestrator.session.Recv", "stream is closed")
	}
	var env pb.SessionEventEnvelope
	if err := s.stream.RecvMsg(&env); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.session.Recv")
	}
	out := &orchestrator.SessionEventEnvelope{
		SessionID:   env.SessionId,
		TenantID:    env.TenantId,
		Sequence:    env.Sequence,
		PayloadJSON: env.PayloadJson,
		FromReplay:  env.FromReplay,
	}
	if env.Timestamp != nil {
		out.Timestamp = env.Timestamp.AsTime()
	}
	return out, nil
}

func (s *sessionEventStreamAdapter) CloseSend() error {
	// Server-streaming RPC — send side is already closed after the initial
	// request. This call is a no-op but required to satisfy transport.Stream.
	return nil
}

func (s *sessionEventStreamAdapter) Close() error {
	s.closed.Store(true)
	return nil
}

// handleSubscribeJobEventsStream initiates a server-streaming gRPC call for
// SubscribeJobEvents, converting the SDK request to proto and wrapping the
// response stream to convert proto JobEvent messages to SDK JobEvent types.
func (h *orchestratorHandler) handleSubscribeJobEventsStream(ctx context.Context, req interface{}, t *Transport, conn *grpc.ClientConn) (transport.Stream, error) {
	sdkReq, ok := req.(*orchestrator.SubscribeJobEventsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.SubscribeJobEvents", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.SubscribeJobEventsRequest{
		TenantId: tenantID,
		JobId:    sdkReq.JobID,
	}

	desc := &grpc.StreamDesc{
		StreamName:    "SubscribeJobEvents",
		ServerStreams: true,
	}

	clientStream, err := conn.NewStream(ctx, desc, pb.OrchestratorService_SubscribeJobEvents_FullMethodName)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.SubscribeJobEvents")
	}

	if err := clientStream.SendMsg(protoReq); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.SubscribeJobEvents")
	}

	if err := clientStream.CloseSend(); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.SubscribeJobEvents")
	}

	return &orchestratorEventStream{
		stream:    clientStream,
		transport: t,
		conn:      conn,
	}, nil
}

// handleInferenceStream opens the InferenceStream server-streaming RPC and
// wraps the gRPC stream in a transport.Stream that converts proto InferenceChunk
// messages to SDK InferenceChunk types on Recv.
func (h *orchestratorHandler) handleInferenceStream(ctx context.Context, req interface{}, t *Transport, conn *grpc.ClientConn) (transport.Stream, error) {
	sdkReq, ok := req.(*orchestrator.InferenceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.InferenceStream", "invalid request type")
	}

	tenantID := transport.TenantFromContext(ctx)

	protoReq := &pb.InferenceRequest{
		Backend:      sdkReq.Backend,
		Model:        sdkReq.Model,
		Prompt:       sdkReq.Prompt,
		SystemPrompt: sdkReq.SystemPrompt,
		HostAffinity: sdkReq.HostAffinity,
		TenantId:     tenantID,
		Metadata:     sdkReq.Metadata,
	}

	desc := &grpc.StreamDesc{
		StreamName:    "InferenceStream",
		ServerStreams: true,
	}

	cs, err := conn.NewStream(ctx, desc, pb.OrchestratorService_InferenceStream_FullMethodName)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.InferenceStream")
	}
	if err := cs.SendMsg(protoReq); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.InferenceStream")
	}
	if err := cs.CloseSend(); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.InferenceStream")
	}

	return &inferenceStreamAdapter{
		stream:    cs,
		transport: t,
		conn:      conn,
	}, nil
}

// inferenceStreamAdapter converts proto InferenceChunk messages to SDK
// InferenceChunk types on Recv. Mirrors orchestratorEventStream.
type inferenceStreamAdapter struct {
	stream    grpc.ClientStream
	transport *Transport
	conn      *grpc.ClientConn
	closed    atomic.Bool
}

// Send is not supported; InferenceStream is server-streaming.
func (s *inferenceStreamAdapter) Send(_ interface{}) error {
	return errors.NewUnimplemented("grpc.orchestrator.InferenceStream.Send",
		"send not supported on server-streaming RPC")
}

// Recv receives a proto InferenceChunk and converts it to an SDK InferenceChunk.
func (s *inferenceStreamAdapter) Recv() (interface{}, error) {
	if s.closed.Load() {
		return nil, errors.NewConnection("grpc.orchestrator.InferenceStream.Recv", "stream is closed")
	}
	var chunk pb.InferenceChunk
	if err := s.stream.RecvMsg(&chunk); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.InferenceStream.Recv")
	}
	delta := make([]orchestrator.InferenceContentBlock, 0, len(chunk.GetDelta()))
	for _, b := range chunk.GetDelta() {
		delta = append(delta, orchestrator.InferenceContentBlock{
			Type: b.GetType(),
			Text: b.GetText(),
		})
	}
	return &orchestrator.InferenceChunk{
		Delta:      delta,
		Model:      chunk.GetModel(),
		StopReason: chunk.GetStopReason(),
		Final:      chunk.GetFinal(),
	}, nil
}

// CloseSend is a no-op; the send side is already closed after the initial request.
func (s *inferenceStreamAdapter) CloseSend() error { return nil }

// Close releases the connection back to the pool.
func (s *inferenceStreamAdapter) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.transport.returnConn(s.conn)
	return nil
}

// orchestratorEventStream wraps a gRPC client stream and converts proto
// JobEvent messages to SDK JobEvent types on Recv.
type orchestratorEventStream struct {
	stream    grpc.ClientStream
	transport *Transport
	conn      *grpc.ClientConn
	closed    atomic.Bool
}

// Send is not supported for server-streaming RPCs.
func (s *orchestratorEventStream) Send(msg interface{}) error {
	return errors.NewUnimplemented("grpc.orchestrator.stream.Send",
		"send not supported on server-streaming RPC")
}

// Recv receives a proto JobEvent from the stream and converts it to an SDK JobEvent.
func (s *orchestratorEventStream) Recv() (interface{}, error) {
	if s.closed.Load() {
		return nil, errors.NewConnection("grpc.orchestrator.stream.Recv", "stream is closed")
	}

	var protoEvt pb.JobEvent
	if err := s.stream.RecvMsg(&protoEvt); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.stream.Recv")
	}

	return &orchestrator.JobEvent{
		JobID:     protoEvt.GetJobId(),
		Type:      protoEvt.GetType(),
		State:     protoEvt.GetState(),
		Message:   protoEvt.GetMessage(),
		Iteration: int(protoEvt.GetIteration()),
		Metadata:  protoEvt.GetMetadata(),
		Timestamp: protoEvt.GetTimestamp(),
	}, nil
}

// CloseSend closes the send side of the stream.
func (s *orchestratorEventStream) CloseSend() error {
	return s.stream.CloseSend()
}

// Close closes the stream entirely.
func (s *orchestratorEventStream) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.transport.returnConn(s.conn)
	return nil
}
