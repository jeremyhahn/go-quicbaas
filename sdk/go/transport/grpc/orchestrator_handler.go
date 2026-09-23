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
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
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
	"Health":                 handleOrchestratorHealth,
	"ListBackends":           handleOrchestratorListBackends,
	"ListRunners":            handleOrchestratorListRunners,
	"SpawnSession":           handleOrchestratorSpawnSession,
	"PromptSession":          handleOrchestratorPromptSession,
	"SessionStatus":          handleOrchestratorSessionStatus,
	"CloseSession":           handleOrchestratorCloseSession,
	"RotateSessionAccount":   handleOrchestratorRotateSessionAccount,
	"ListSessions":           handleOrchestratorListSessions,
	"CreateTask":             handleOrchestratorCreateTask,
	"GetTask":                handleOrchestratorGetTask,
	"ListTasks":              handleOrchestratorListTasks,
	"ClaimTask":              handleOrchestratorClaimTask,
	"CompleteTask":           handleOrchestratorCompleteTask,
	"BlockTask":              handleOrchestratorBlockTask,
	"DeleteTask":             handleOrchestratorDeleteTask,
	"ReadmitTask":            handleOrchestratorReadmitTask,
	"RegisterAgent":          handleOrchestratorRegisterAgent,
	"GetAgent":               handleOrchestratorGetAgent,
	"ListAgents":             handleOrchestratorListAgents,
	"RemoveAgent":            handleOrchestratorRemoveAgent,
	"AgentHeartbeat":         handleOrchestratorAgentHeartbeat,
	"DecomposeTask":          handleOrchestratorDecomposeTask,
	"SpawnChildJob":          handleOrchestratorSpawnChildJob,
	"StartJob":               handleOrchestratorStartJob,
	"GetJobStatus":           handleOrchestratorGetJobStatus,
	"ListJobs":               handleOrchestratorListJobs,
	"CancelJob":              handleOrchestratorCancelJob,
	"GetSessionHistory":      handleOrchestratorGetSessionHistory,
	"GetSessionConversation": handleOrchestratorGetSessionConversation,
	"GetSpendReport":         handleOrchestratorGetSpendReport,
	"Inference":              handleOrchestratorInference,
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
// ListRunners
// =============================================================================

// handleOrchestratorListRunners invokes the ListRunners RPC: every registered
// JobRunner and the states it declares.
//
// The request carries no tenant because the runner registry is the daemon's,
// not a tenant's — the same reason the REST route derives none.
func handleOrchestratorListRunners(ctx context.Context, conn *grpc.ClientConn, _ interface{}) (interface{}, error) {
	protoResp := &pb.OrchestratorListRunnersResponse{}
	if err := conn.Invoke(ctx, pb.OrchestratorService_ListRunners_FullMethodName,
		&pb.OrchestratorListRunnersRequest{}, protoResp); err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.ListRunners")
	}

	runners := make([]*orchestrator.JobRunnerInfo, 0, len(protoResp.GetRunners()))
	for _, r := range protoResp.GetRunners() {
		states := make([]orchestrator.JobStateSpec, 0, len(r.GetStates()))
		for _, spec := range r.GetStates() {
			states = append(states, orchestrator.JobStateSpec{
				State:         spec.GetState(),
				Label:         spec.GetLabel(),
				SessionBacked: spec.GetSessionBacked(),
			})
		}
		runners = append(runners, &orchestrator.JobRunnerInfo{Name: r.GetName(), States: states})
	}

	return &orchestrator.ListJobRunnersResponse{Runners: runners, Count: len(runners)}, nil
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

// persistentOrDefault resolves the SDK's optional Persistent flag.
//
// Nil is "the caller did not say", and the answer to that is a DURABLE
// session: it is what the REST handler has always done with an omitted field,
// and a transport that answered otherwise would hand gRPC and Unix callers
// one-shot sessions for the identical request. Only an explicit false asks for
// one-shot-per-prompt.
func persistentOrDefault(persistent *bool) bool {
	if persistent == nil {
		return true
	}
	return *persistent
}

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
		// Unset means the deployment's default, which is a durable session --
		// the same answer REST gives an omitted field. See
		// orchestrator.SpawnSessionRequest.Persistent.
		Persistent: persistentOrDefault(sdkReq.Persistent),

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

	// CreatedAt IS DELIBERATELY LEFT UNSET. SessionStatusResponse carries no
	// created_at on the wire; the `timestamp` field it does carry is when the
	// STATUS was produced, so filling CreatedAt from it reported "the session
	// was created just now" on every call — which the CLI prints, verbatim, as
	// the session's creation time. An absent value renders as absent; a
	// confident wrong one does not. The list surface has a real created_at and
	// fills it below.
	return &orchestrator.SessionStatusResponse{
		SessionID:  protoResp.GetSessionId(),
		Status:     sessionStateName(protoResp.GetState()),
		LastActive: timestampToUnixNano(protoResp.GetLastActivity()),
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

// sessionStateNames maps the proto lifecycle enum back onto the lowercase
// state vocabulary every other transport speaks.
//
// THE SDK HAS ONE SESSION-STATE VOCABULARY AND THE PROTO ENUM IS NOT IT.
// The server converts "active" to SESSION_STATE_ACTIVE on the way out
// (serviceSessionStateToProto); rendering that back with the generated
// .String() returned "SESSION_STATE_ACTIVE" to the caller, so a gRPC or Unix
// client that asked for Status "active" was handed rows whose Status could not
// be compared to the value it had just sent, displayed beside a REST client's
// rows, or fed back into the same filter. The states are sourced from
// orchestration rather than spelled again here, so the set cannot drift from
// the one the service validates against.
var sessionStateNames = map[pb.SessionState]string{
	pb.SessionState_SESSION_STATE_ACTIVE:    string(orchestration.SessionStateActive),
	pb.SessionState_SESSION_STATE_IDLE:      string(orchestration.SessionStateIdle),
	pb.SessionState_SESSION_STATE_CLOSED:    string(orchestration.SessionStateClosed),
	pb.SessionState_SESSION_STATE_ERROR:     string(orchestration.SessionStateError),
	pb.SessionState_SESSION_STATE_RESUMABLE: string(orchestration.SessionStateResumable),
}

// sessionStateName renders one proto state in the SDK's vocabulary.
//
// SESSION_STATE_UNSPECIFIED becomes the EMPTY STRING, which is what the other
// transports send for a session whose state the service did not set. Returning
// the enum name would put a value in the field that no filter accepts and that
// SessionState.Valid rejects.
func sessionStateName(state pb.SessionState) string {
	return sessionStateNames[state]
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

	// Status is the lifecycle filter the proto declares and the server
	// consumes. Dropping it here made every gRPC (and Unix, which delegates
	// here) caller's filtered list arrive unfiltered, so closed and error
	// records came back mixed in with the live ones -- the false liveness
	// this filter exists to remove (go-quicbaas#792).
	protoReq := &pb.ListSessionsRequest{
		Backend:  sdkReq.Backend,
		TenantId: tenantID,
		Status:   sdkReq.Status,
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
			Status:     sessionStateName(s.GetState()),
			CreatedAt:  timestampToUnixNano(s.GetCreatedAt()),
			LastActive: timestampToUnixNano(s.GetLastActivity()),
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
		TenantId:      sdkReq.TenantID,
		TeamName:      sdkReq.TeamName,
		Subject:       sdkReq.Subject,
		Description:   sdkReq.Description,
		AssignedTo:    sdkReq.AssignedTo,
		Dependencies:  sdkReq.Dependencies,
		Metadata:      sdkReq.Metadata,
		ProjectId:     sdkReq.ProjectID,
		AutoSpawn:     sdkReq.AutoSpawn,
		Backend:       sdkReq.Backend,
		GpuRequired:   sdkReq.GpuRequired,
		HostAffinity:  sdkReq.HostAffinity,
		Source:        taskSourceToProto(sdkReq.Source),
		Priority:      int64(sdkReq.Priority),
		AttemptBudget: int64(sdkReq.AttemptBudget),
		Runner:        sdkReq.Runner,
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
	if sdkReq.Status != "" || sdkReq.TeamName != "" || sdkReq.AssignedTo != "" || sdkReq.ProjectID != "" || len(sdkReq.Statuses) > 0 {
		filter := &pb.TaskFilter{
			TeamName:   sdkReq.TeamName,
			AssignedTo: sdkReq.AssignedTo,
			ProjectId:  sdkReq.ProjectID,
		}
		if sdkReq.Status != "" {
			if val, exists := pb.TaskStatus_value[taskStatusToProtoEnum(sdkReq.Status)]; exists {
				filter.Status = pb.TaskStatus(val)
			}
		}
		for _, status := range sdkReq.Statuses {
			if val, exists := pb.TaskStatus_value[taskStatusToProtoEnum(status)]; exists {
				filter.Statuses = append(filter.Statuses, pb.TaskStatus(val))
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

func handleOrchestratorBlockTask(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.BlockTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.BlockTask", "invalid request type")
	}

	protoReq := &pb.BlockTaskRequest{
		TenantId: sdkReq.TenantID,
		TaskId:   sdkReq.TaskID,
		Kind:     sdkReq.Kind,
		Reason:   sdkReq.Reason,
	}
	protoResp := &pb.BlockTaskResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_BlockTask_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.BlockTask")
	}

	return &orchestrator.BlockTaskResponse{
		TaskID:  protoResp.GetTaskId(),
		Kind:    sdkReq.Kind,
		Blocked: protoResp.GetBlocked(),
	}, nil
}

func handleOrchestratorDeleteTask(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.DeleteTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.DeleteTask", "invalid request type")
	}

	protoReq := &pb.DeleteTaskRequest{
		TenantId: sdkReq.TenantID,
		TaskId:   sdkReq.TaskID,
	}
	protoResp := &pb.DeleteTaskResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_DeleteTask_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.DeleteTask")
	}

	return &orchestrator.DeleteTaskResponse{
		TaskID:  protoResp.GetTaskId(),
		Deleted: protoResp.GetDeleted(),
	}, nil
}

func handleOrchestratorReadmitTask(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ReadmitTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.ReadmitTask", "invalid request type")
	}

	protoReq := &pb.ReadmitTaskRequest{
		TenantId: sdkReq.TenantID,
		TaskId:   sdkReq.TaskID,
	}
	protoResp := &pb.ReadmitTaskResponse{}

	invokeErr := conn.Invoke(ctx, pb.OrchestratorService_ReadmitTask_FullMethodName, protoReq, protoResp)
	if invokeErr != nil {
		return nil, convertGRPCError(invokeErr, "grpc.orchestrator.ReadmitTask")
	}

	return &orchestrator.ReadmitTaskResponse{
		TaskID:     protoResp.GetTaskId(),
		Readmitted: protoResp.GetReadmitted(),
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
		Runner:        sdkReq.Runner,
		TaskId:        sdkReq.TaskID,
		Attempt:       int64(sdkReq.Attempt),
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

		TaskID:        protoResp.GetTaskId(),
		Attempt:       int(protoResp.GetAttempt()),
		Node:          protoResp.GetNode(),
		HeartbeatAt:   protoResp.GetHeartbeatAt(),
		FailureClass:  protoResp.GetFailureClass(),
		LeadSessionID: protoResp.GetLeadSessionId(),
		SessionIDs:    protoResp.GetSessionIds(),
		Stage:         protoResp.GetStage(),
		Runner:        protoResp.GetRunner(),

		// Provenance — the same facts the REST transport decodes straight
		// off the JSON body. A converter that stops here hands the caller a
		// job with no attempts, no delivery and no reason it stopped.
		AgentID:        protoResp.GetAgentId(),
		Source:         taskSourceFromProto(protoResp.GetSource()),
		Stages:         jobStagesFromProto(protoResp.GetStages()),
		Attempts:       jobAttemptsFromProto(protoResp.GetAttempts()),
		AttemptBudget:  int(protoResp.GetAttemptBudget()),
		TerminalReason: protoResp.GetTerminalReason(),
		ReadmitAllowed: protoResp.GetReadmitAllowed(),
		Delivery:       jobDeliveryFromProto(protoResp.GetDelivery()),
		Verification:   jobVerificationFromProto(protoResp.GetVerification()),
	}, nil
}

// jobStagesFromProto maps the lifecycle moments the job entered.
func jobStagesFromProto(pb []*pb.OrchestratorJobStage) []orchestrator.JobStageRecord {
	if len(pb) == 0 {
		return nil
	}
	out := make([]orchestrator.JobStageRecord, len(pb))
	for i, stage := range pb {
		out[i] = orchestrator.JobStageRecord{
			Stage:     stage.GetStage(),
			EnteredAt: stage.GetEnteredAt(),
		}
	}
	return out
}

// jobAttemptsFromProto maps the attempts at the job's task.
func jobAttemptsFromProto(pb []*pb.OrchestratorJobAttempt) []orchestrator.JobAttemptRecord {
	if len(pb) == 0 {
		return nil
	}
	out := make([]orchestrator.JobAttemptRecord, len(pb))
	for i, attempt := range pb {
		out[i] = orchestrator.JobAttemptRecord{
			Attempt: int(attempt.GetAttempt()),
			JobID:   attempt.GetJobId(),
			Current: attempt.GetCurrent(),
		}
	}
	return out
}

// jobDeliveryFromProto maps the git side of the attempt, nil when the runner
// published none.
func jobDeliveryFromProto(pbDelivery *pb.OrchestratorJobDelivery) *orchestrator.JobDelivery {
	if pbDelivery == nil {
		return nil
	}
	var reviews []orchestrator.JobReviewRound
	if rounds := pbDelivery.GetReviews(); len(rounds) > 0 {
		reviews = make([]orchestrator.JobReviewRound, len(rounds))
		for i, round := range rounds {
			reviews[i] = orchestrator.JobReviewRound{
				Round:       int(round.GetRound()),
				Reviewer:    round.GetReviewer(),
				Verdict:     round.GetVerdict(),
				HeadSHA:     round.GetHeadSha(),
				Findings:    round.GetFindings(),
				SubmittedAt: round.GetSubmittedAt(),
			}
		}
	}
	return &orchestrator.JobDelivery{
		Branch:            pbDelivery.GetBranch(),
		HeadSHA:           pbDelivery.GetHeadSha(),
		PullRequestNumber: int(pbDelivery.GetPullRequestNumber()),
		PullRequestURL:    pbDelivery.GetPullRequestUrl(),
		PullRequestState:  pbDelivery.GetPullRequestState(),
		Reviews:           reviews,
	}
}

// jobVerificationFromProto maps the last validator pipeline's results.
func jobVerificationFromProto(pb []*pb.OrchestratorJobVerification) []orchestrator.JobVerificationResult {
	if len(pb) == 0 {
		return nil
	}
	out := make([]orchestrator.JobVerificationResult, len(pb))
	for i, result := range pb {
		out[i] = orchestrator.JobVerificationResult{
			Name:    result.GetName(),
			Passed:  result.GetPassed(),
			Message: result.GetMessage(),
			Details: result.GetDetails(),
		}
	}
	return out
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

	// The tenant comes from the context, the narrowing from the request:
	// the caller cannot choose its tenant, but it does choose its filter.
	protoReq := &pb.OrchestratorListJobsRequest{
		TenantId: tenantID,
		TaskId:   sdkReq.TaskID,
	}

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
			TaskID:      j.GetTaskId(),
			Attempt:     int(j.GetAttempt()),
			Source:      taskSourceFromProto(j.GetSource()),
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

	sdkTask.AutoSpawn = t.GetAutoSpawn()
	sdkTask.SpawnedJobID = t.GetSpawnedJobId()
	if t.GetSpawnedAt() != nil {
		sdkTask.SpawnedAt = t.GetSpawnedAt().AsTime().UnixNano()
	}
	sdkTask.ProjectID = t.GetProjectId()
	sdkTask.Source = taskSourceFromProto(t.GetSource())
	sdkTask.Priority = int(t.GetPriority())
	sdkTask.Attempts = int(t.GetAttempts())
	sdkTask.AttemptBudget = int(t.GetAttemptBudget())
	sdkTask.EnvironmentFaults = int(t.GetEnvironmentFaults())
	sdkTask.QuestionAttempts = int(t.GetQuestionAttempts())
	sdkTask.ConsecutiveEnvironmentFaults = int(t.GetConsecutiveEnvironmentFaults())
	sdkTask.RetryNotBefore = t.GetRetryNotBefore()
	sdkTask.BlockedReason = t.GetBlockedReason()
	sdkTask.BlockKind = t.GetBlockKind()
	sdkTask.StatusChangedAt = t.GetStatusChangedAt()
	sdkTask.Runner = t.GetRunner()
	if routing := t.GetRouting(); routing != nil {
		sdkTask.Routing = orchestrator.TaskRouting{
			Backend:      routing.GetBackend(),
			HostAffinity: routing.GetHostAffinity(),
			GpuRequired:  routing.GetGpuRequired(),
		}
	}
	sdkTask.DispatchReason = t.GetDispatchReason()

	return sdkTask
}

// taskSourceToProto converts a caller-supplied provenance; nil stays nil.
func taskSourceToProto(src *orchestrator.TaskSource) *pb.TaskSource {
	if src == nil {
		return nil
	}
	return &pb.TaskSource{Kind: src.Kind, Ref: src.Ref, Url: src.URL}
}

// taskSourceFromProto converts a server-recorded provenance back; nil stays
// nil so an absent origin decodes as no source rather than an empty one. It
// serves the task shape and the two job shapes, which carry the same origin.
func taskSourceFromProto(src *pb.TaskSource) *orchestrator.TaskSource {
	if src == nil {
		return nil
	}
	return &orchestrator.TaskSource{Kind: src.GetKind(), Ref: src.GetRef(), URL: src.GetUrl()}
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
// timestampToUnixNano renders a protobuf timestamp as Unix NANOSECONDS.
//
// A SESSION'S INSTANTS ARE NANOSECONDS AND THE SECONDS CONVERTER IS NOT
// INTERCHANGEABLE WITH THIS ONE. The server builds SessionInfo's times with
// unixNanoTimestamp from nanosecond-domain values, and every SDK and CLI
// consumer reads the fields back with time.Unix(0, v) -- so decoding them
// with timestampToUnix handed back seconds, which read as nanoseconds put
// every session's created and last-active time within two seconds of the
// epoch. Sessions are not the only shape on this service: a task's instants
// really are seconds on the wire (serviceTaskToProto builds them with
// time.Unix(sec, 0)), which is why the seconds converter stays and this is a
// second function rather than a change to it.
func timestampToUnixNano(ts *timestamppb.Timestamp) int64 {
	if ts == nil {
		return 0
	}
	return ts.AsTime().UnixNano()
}

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

// handleOrchestratorGetSessionConversation calls the unary
// OrchestratorService.GetSessionConversation RPC and converts one page of
// turns into the SDK shape.
//
// Unlike GetSessionHistory above — which pages the raw event log by reading a
// non-following SubscribeSessionEvents stream to its end — this has a real
// RPC behind it, so a caller can resume from a cursor instead of re-reading
// from the beginning of the transcript every time.
func handleOrchestratorGetSessionConversation(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetSessionConversationRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.GetSessionConversation", "invalid request type")
	}
	if sdkReq.TenantID == "" {
		sdkReq.TenantID = transport.TenantFromContext(ctx)
	}
	client := pb.NewOrchestratorServiceClient(conn)
	resp, err := client.GetSessionConversation(ctx, &pb.GetSessionConversationRequest{
		TenantId:  sdkReq.TenantID,
		SessionId: sdkReq.SessionID,
		Cursor:    sdkReq.Cursor,
		Limit:     uint32(sdkReq.Limit),
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.GetSessionConversation")
	}

	out := &orchestrator.GetSessionConversationResponse{
		SessionID:      resp.GetSessionId(),
		TenantID:       resp.GetTenantId(),
		Turns:          make([]*orchestrator.SessionTurn, 0, len(resp.GetTurns())),
		NextCursor:     resp.GetNextCursor(),
		OldestSequence: resp.GetOldestSequence(),
	}
	for _, wire := range resp.GetTurns() {
		turn, convErr := sessionTurnFromProto(wire)
		if convErr != nil {
			return nil, convErr
		}
		out.Turns = append(out.Turns, turn)
	}
	return out, nil
}

// sessionTurnFromProto converts one wire turn into the SDK type. Token usage
// crosses as JSON — the accounting shape belongs to the server's orchestration
// package and is not forked into a second protobuf definition — so an
// unusable encoding is a decode failure here rather than a silently absent
// cost on the turn.
func sessionTurnFromProto(wire *pb.SessionTurn) (*orchestrator.SessionTurn, error) {
	turn := &orchestrator.SessionTurn{
		Sequence:       wire.GetSequence(),
		SessionID:      wire.GetSessionId(),
		TenantID:       wire.GetTenantId(),
		Role:           orchestrator.SessionTurnRole(wire.GetRole()),
		Text:           wire.GetText(),
		TruncatedBytes: wire.GetTruncatedBytes(),
		Model:          wire.GetModel(),
		StopReason:     wire.GetStopReason(),
	}
	if ts := wire.GetTimestamp(); ts != nil {
		turn.Timestamp = ts.AsTime()
	}
	for _, tool := range wire.GetTools() {
		turn.Tools = append(turn.Tools, orchestrator.SessionToolUse{
			InvocationID: tool.GetInvocationId(),
			Name:         tool.GetName(),
			IsError:      tool.GetIsError(),
			ResultBytes:  tool.GetResultBytes(),
		})
	}
	if raw := wire.GetUsageJson(); len(raw) > 0 {
		var usage orchestrator.SessionTokenUsage
		if err := json.Unmarshal(raw, &usage); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal,
				"grpc.orchestrator.GetSessionConversation", "failed to decode turn usage")
		}
		turn.Usage = &usage
	}
	return turn, nil
}

// handleOrchestratorGetSpendReport calls the unary
// OrchestratorService.GetSpendReport RPC and converts one scope's roll-up
// into the SDK shape.
func handleOrchestratorGetSpendReport(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetSpendReportRequest)
	if !ok {
		return nil, errors.NewInvalidInput("grpc.orchestrator.GetSpendReport", "invalid request type")
	}
	if sdkReq.TenantID == "" {
		sdkReq.TenantID = transport.TenantFromContext(ctx)
	}
	client := pb.NewOrchestratorServiceClient(conn)
	resp, err := client.GetSpendReport(ctx, &pb.GetSpendReportRequest{
		TenantId:  sdkReq.TenantID,
		ProjectId: sdkReq.ProjectID,
		TaskId:    sdkReq.TaskID,
		JobId:     sdkReq.JobID,
		SessionId: sdkReq.SessionID,
		Round:     int32(sdkReq.Round),
		RoundSet:  sdkReq.RoundSet,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.orchestrator.GetSpendReport")
	}
	return spendReportFromProto(resp), nil
}

// spendReportFromProto converts the wire report into the SDK type.
func spendReportFromProto(resp *pb.GetSpendReportResponse) *orchestrator.GetSpendReportResponse {
	out := &orchestrator.GetSpendReportResponse{
		Level:        orchestrator.SpendLevel(resp.GetLevel()),
		TenantID:     resp.GetTenantId(),
		ProjectID:    resp.GetProjectId(),
		TaskID:       resp.GetTaskId(),
		JobID:        resp.GetJobId(),
		SessionID:    resp.GetSessionId(),
		Round:        int(resp.GetRound()),
		Entries:      int(resp.GetEntries()),
		InputTokens:  resp.GetInputTokens(),
		OutputTokens: resp.GetOutputTokens(),
		TotalTokens:  resp.GetTotalTokens(),
		CostMicros:   resp.GetCostMicros(),
		CostMeasured: resp.GetCostMeasured(),
		Successes:    int(resp.GetSuccesses()),
		Failures:     int(resp.GetFailures()),
		FirstAt:      spendTimeFromProto(resp.GetFirstAt()),
		LastAt:       spendTimeFromProto(resp.GetLastAt()),
		ChildLevel:   orchestrator.SpendLevel(resp.GetChildLevel()),
	}
	for _, account := range resp.GetAccounts() {
		out.Accounts = append(out.Accounts, spendAccountFromProto(account))
	}
	for _, model := range resp.GetModels() {
		out.Models = append(out.Models, orchestrator.SpendModel{
			Engine:      model.GetEngine(),
			Model:       model.GetModel(),
			TotalTokens: model.GetTotalTokens(),
		})
	}
	for _, child := range resp.GetChildren() {
		out.Children = append(out.Children, orchestrator.SpendChild{
			ID:           child.GetId(),
			Level:        orchestrator.SpendLevel(child.GetLevel()),
			Unattributed: child.GetUnattributed(),
			TotalTokens:  child.GetTotalTokens(),
		})
	}
	return out
}

// spendAccountFromProto converts one account's contribution and allowance.
func spendAccountFromProto(account *pb.SpendAccount) orchestrator.SpendAccount {
	out := orchestrator.SpendAccount{
		AccountID:    account.GetAccountId(),
		DisplayName:  account.GetDisplayName(),
		Provider:     account.GetProvider(),
		TotalTokens:  account.GetTotalTokens(),
		PolicyStatus: orchestrator.SpendPolicyStatus(account.GetPolicyStatus()),
	}
	for _, window := range account.GetWindows() {
		out.Windows = append(out.Windows, orchestrator.SpendWindowStatus{
			Name:            window.GetName(),
			Size:            window.GetSizeNanos(),
			MaxTokens:       window.GetMaxTokens(),
			UsedTokens:      window.GetUsedTokens(),
			RemainingTokens: window.GetRemainingTokens(),
			Exhausted:       window.GetExhausted(),
			FreesAt:         spendTimeFromProto(window.GetFreesAt()),
		})
	}
	if breaker := account.GetBreaker(); breaker != nil {
		out.Breaker = &orchestrator.SpendBreakerVerdict{
			State:         breaker.GetState(),
			Failures:      int(breaker.GetFailures()),
			Successes:     int(breaker.GetSuccesses()),
			LastFailureAt: spendTimeFromProto(breaker.GetLastFailureAt()),
			LastSuccessAt: spendTimeFromProto(breaker.GetLastSuccessAt()),
			OpenUntil:     spendTimeFromProto(breaker.GetOpenUntil()),
		}
	}
	return out
}

// spendTimeFromProto converts an optional wire timestamp, leaving an absent
// one NIL. An absent moment must not become the year 1, which a renderer would
// show as a date — and because the SDK type is re-marshalled by the desktop
// binding, nil is what keeps `omitempty` able to omit it.
func spendTimeFromProto(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	at := ts.AsTime()
	return &at
}
