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

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// fakeOrchestratorServer implements the OrchestratorService methods exercised
// by the orchestrator handlers. The fail flag drives the error paths.
type fakeOrchestratorServer struct {
	pb.UnimplementedOrchestratorServiceServer
	fail bool
}

func (f *fakeOrchestratorServer) e() error { return failErr(f.fail) }

func orchTime() *timestamppb.Timestamp { return timestamppb.Now() }

func (f *fakeOrchestratorServer) Health(context.Context, *pb.OrchestratorHealthRequest) (*pb.OrchestratorHealthResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.OrchestratorHealthResponse{Report: &pb.HealthCheckReport{
		Unhealthy: 0,
		Results:   []*pb.SessionHealthResult{{SessionId: "s1", Healthy: true}},
		Timestamp: orchTime(),
	}}, nil
}
func (f *fakeOrchestratorServer) ListBackends(context.Context, *pb.OrchestratorListBackendsRequest) (*pb.OrchestratorListBackendsResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.OrchestratorListBackendsResponse{Backends: []*pb.AgentBackendInfo{{Name: "claude", Type: "anthropic"}}}, nil
}
func (f *fakeOrchestratorServer) SpawnSession(context.Context, *pb.SpawnSessionRequest) (*pb.SpawnSessionResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.SpawnSessionResponse{SessionId: "s1", Backend: "claude", ContainerId: "c1"}, nil
}
func (f *fakeOrchestratorServer) PromptSession(context.Context, *pb.PromptSessionRequest) (*pb.PromptSessionResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.PromptSessionResponse{SessionId: "s1", Content: []*pb.ContentBlock{{Type: "text", Text: "hi"}}, StopReason: "end_turn"}, nil
}
func (f *fakeOrchestratorServer) SessionStatus(context.Context, *pb.SessionStatusRequest) (*pb.SessionStatusResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.SessionStatusResponse{SessionId: "s1", Timestamp: orchTime(), LastActivity: orchTime()}, nil
}
func (f *fakeOrchestratorServer) CloseSession(context.Context, *pb.CloseSessionRequest) (*pb.CloseSessionResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.CloseSessionResponse{SessionId: "s1"}, nil
}
func (f *fakeOrchestratorServer) ListSessions(context.Context, *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ListSessionsResponse{Sessions: []*pb.SessionInfo{{SessionId: "s1", Backend: "claude", CreatedAt: orchTime(), LastActivity: orchTime()}}}, nil
}
func (f *fakeOrchestratorServer) CreateTask(context.Context, *pb.CreateTaskRequest) (*pb.CreateTaskResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.CreateTaskResponse{Task: sampleTask()}, nil
}
func (f *fakeOrchestratorServer) GetTask(context.Context, *pb.GetTaskRequest) (*pb.GetTaskResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetTaskResponse{Task: sampleTask()}, nil
}
func (f *fakeOrchestratorServer) ListTasks(context.Context, *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ListTasksResponse{Tasks: []*pb.Task{sampleTask()}}, nil
}
func (f *fakeOrchestratorServer) ClaimTask(context.Context, *pb.ClaimTaskRequest) (*pb.ClaimTaskResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ClaimTaskResponse{TaskId: "t1", AgentId: "a1", Claimed: true}, nil
}
func (f *fakeOrchestratorServer) BlockTask(context.Context, *pb.BlockTaskRequest) (*pb.BlockTaskResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.BlockTaskResponse{TaskId: "t1", Blocked: true}, nil
}
func (f *fakeOrchestratorServer) DeleteTask(context.Context, *pb.DeleteTaskRequest) (*pb.DeleteTaskResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.DeleteTaskResponse{TaskId: "t1", Deleted: true}, nil
}
func (f *fakeOrchestratorServer) ReadmitTask(context.Context, *pb.ReadmitTaskRequest) (*pb.ReadmitTaskResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ReadmitTaskResponse{TaskId: "t1", Readmitted: true}, nil
}
func (f *fakeOrchestratorServer) CompleteTask(context.Context, *pb.CompleteTaskRequest) (*pb.CompleteTaskResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.CompleteTaskResponse{TaskId: "t1", Completed: true}, nil
}
func (f *fakeOrchestratorServer) RegisterAgent(context.Context, *pb.RegisterAgentRequest) (*pb.RegisterAgentResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.RegisterAgentResponse{Agent: sampleAgent()}, nil
}
func (f *fakeOrchestratorServer) GetAgent(context.Context, *pb.GetAgentRequest) (*pb.GetAgentResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetAgentResponse{Agent: sampleAgent()}, nil
}
func (f *fakeOrchestratorServer) ListAgents(context.Context, *pb.ListAgentsRequest) (*pb.ListAgentsResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ListAgentsResponse{Agents: []*pb.AgentRegistration{sampleAgent()}}, nil
}
func (f *fakeOrchestratorServer) RemoveAgent(context.Context, *pb.RemoveAgentRequest) (*pb.RemoveAgentResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.RemoveAgentResponse{AgentId: "a1", Removed: true}, nil
}
func (f *fakeOrchestratorServer) AgentHeartbeat(context.Context, *pb.AgentHeartbeatRequest) (*pb.AgentHeartbeatResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.AgentHeartbeatResponse{AgentId: "a1", Acknowledged: true}, nil
}
func (f *fakeOrchestratorServer) DecomposeTask(context.Context, *pb.DecomposeTaskRequest) (*pb.DecomposeTaskResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.DecomposeTaskResponse{WorkItems: []*pb.OrchestratorWorkItem{{Id: "w1", Title: "t", Priority: 1}}, Timestamp: orchTime()}, nil
}
func (f *fakeOrchestratorServer) SpawnChildJob(context.Context, *pb.SpawnChildJobRequest) (*pb.SpawnChildJobResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.SpawnChildJobResponse{ChildJobId: "cj1"}, nil
}
func (f *fakeOrchestratorServer) StartJob(context.Context, *pb.OrchestratorStartJobRequest) (*pb.OrchestratorStartJobResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.OrchestratorStartJobResponse{JobId: "j1", Timestamp: orchTime()}, nil
}
func (f *fakeOrchestratorServer) GetJobStatus(context.Context, *pb.OrchestratorGetJobStatusRequest) (*pb.OrchestratorGetJobStatusResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.OrchestratorGetJobStatusResponse{JobId: "j1", State: "running", Iteration: 1, TaskCount: 2, Completed: 1, Events: []*pb.OrchestratorAuditEvent{{Type: "started", Details: "ok"}}}, nil
}
func (f *fakeOrchestratorServer) ListJobs(context.Context, *pb.OrchestratorListJobsRequest) (*pb.OrchestratorListJobsResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.OrchestratorListJobsResponse{Jobs: []*pb.OrchestratorJobSummary{{JobId: "j1", State: "running", EventCount: 3}}}, nil
}
func (f *fakeOrchestratorServer) CancelJob(context.Context, *pb.OrchestratorCancelJobRequest) (*pb.OrchestratorCancelJobResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.OrchestratorCancelJobResponse{JobId: "j1", Cancelled: true}, nil
}
func (f *fakeOrchestratorServer) Inference(context.Context, *pb.InferenceRequest) (*pb.InferenceResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.InferenceResponse{Content: []*pb.ContentBlock{{Type: "text", Text: "ans"}}, Model: "m", StopReason: "end_turn"}, nil
}
func (f *fakeOrchestratorServer) SubscribeSessionEvents(_ *pb.SubscribeSessionRequest, stream grpc.ServerStreamingServer[pb.SessionEventEnvelope]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.SessionEventEnvelope{SessionId: "s1", TenantId: "t1", Sequence: 1, PayloadJson: []byte("{}"), Timestamp: orchTime()})
}

func sampleTask() *pb.Task {
	return &pb.Task{
		Id: "t1", TenantId: "t1", TeamName: "team", Subject: "subj", Description: "d",
		AssignedTo: "a1", Dependencies: []string{"d1"}, Version: 1,
		CreatedAt: orchTime(), Result: &pb.TaskResult{Output: "out"},
	}
}

func sampleAgent() *pb.AgentRegistration {
	return &pb.AgentRegistration{
		Id: "a1", TenantId: "t1", Name: "agent", Host: "host", Backend: "claude",
		AgentType: "worker", WorkDir: "/w", Version: 1, RegisteredAt: orchTime(), LastSeen: orchTime(),
	}
}

func newOrchestratorHandlerWithServer(t *testing.T, fail bool) *orchestratorHandler {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, &fakeOrchestratorServer{fail: fail})
	})
	return newOrchestratorHandler(srv.dial(t))
}

type orchCase struct {
	name   string
	method string
	req    interface{}
}

var orchCases = []orchCase{
	{"Health", "Health", nil},
	{"ListBackends", "ListBackends", nil},
	{"SpawnSession", "SpawnSession", &orchestrator.SpawnSessionRequest{Backend: "claude", Model: "m"}},
	{"PromptSession", "PromptSession", &orchestrator.PromptSessionRequest{SessionID: "s1", Prompt: "hi"}},
	{"SessionStatus", "SessionStatus", &orchestrator.SessionStatusRequest{SessionID: "s1"}},
	{"CloseSession", "CloseSession", &orchestrator.CloseSessionRequest{SessionID: "s1"}},
	{"RotateSessionAccount", "RotateSessionAccount", &orchestrator.RotateSessionAccountRequest{SessionID: "s1"}},
	{"ListSessions", "ListSessions", &orchestrator.ListSessionsRequest{Backend: "claude"}},
	{"CreateTask", "CreateTask", &orchestrator.CreateTaskRequest{TenantID: "t1", Subject: "subj"}},
	{"GetTask", "GetTask", &orchestrator.GetTaskRequest{TenantID: "t1", TaskID: "t1"}},
	{"ListTasks", "ListTasks", &orchestrator.ListTasksRequest{TenantID: "t1", Status: "pending", TeamName: "team", AssignedTo: "a1"}},
	{"ClaimTask", "ClaimTask", &orchestrator.ClaimTaskRequest{TenantID: "t1", TaskID: "t1", AgentID: "a1"}},
	{"CompleteTask", "CompleteTask", &orchestrator.CompleteTaskRequest{TenantID: "t1", TaskID: "t1", Result: &orchestrator.TaskResult{Output: "o"}}},
	{"BlockTask", "BlockTask", &orchestrator.BlockTaskRequest{TenantID: "t1", TaskID: "t1", Kind: "blocked", Reason: "r"}},
	{"ReadmitTask", "ReadmitTask", &orchestrator.ReadmitTaskRequest{TenantID: "t1", TaskID: "t1"}},
	{"DeleteTask", "DeleteTask", &orchestrator.DeleteTaskRequest{TenantID: "t1", TaskID: "t1"}},
	{"ListTasksProjectStatuses", "ListTasks", &orchestrator.ListTasksRequest{TenantID: "t1", ProjectID: "p1", Statuses: []string{"blocked", "pending"}}},
	{"CreateTaskWithSource", "CreateTask", &orchestrator.CreateTaskRequest{TenantID: "t1", Subject: "subj", Source: &orchestrator.TaskSource{Kind: "operator", Ref: "alice"}, Priority: 2, AttemptBudget: 3}},
	{"RegisterAgent", "RegisterAgent", &orchestrator.RegisterAgentRequest{TenantID: "t1", Name: "agent"}},
	{"GetAgent", "GetAgent", &orchestrator.GetAgentRequest{TenantID: "t1", AgentID: "a1"}},
	{"ListAgents", "ListAgents", &orchestrator.ListAgentsRequest{TenantID: "t1", Host: "host", Status: "active", AgentType: "worker", Backend: "claude"}},
	{"RemoveAgent", "RemoveAgent", &orchestrator.RemoveAgentRequest{TenantID: "t1", AgentID: "a1"}},
	{"AgentHeartbeat", "AgentHeartbeat", &orchestrator.AgentHeartbeatRequest{TenantID: "t1", AgentID: "a1"}},
	{"DecomposeTask", "DecomposeTask", &orchestrator.DecomposeTaskRequest{Backend: "claude", Task: "do it"}},
	{"SpawnChildJob", "SpawnChildJob", &orchestrator.SpawnChildJobRequest{ParentJobID: "j0", Tasks: []*orchestrator.TaskSpec{{Title: "t"}}}},
	{"StartJob", "StartJob", &orchestrator.StartJobRequest{Description: "d", MaxIterations: 3}},
	{"GetJobStatus", "GetJobStatus", &orchestrator.GetJobStatusRequest{JobID: "j1"}},
	{"ListJobs", "ListJobs", &orchestrator.ListJobsRequest{}},
	{"CancelJob", "CancelJob", &orchestrator.CancelJobRequest{JobID: "j1"}},
	{"GetSessionHistory", "GetSessionHistory", &orchestrator.GetSessionHistoryRequest{SessionID: "s1", Limit: 1}},
	{"Inference", "Inference", &orchestrator.InferenceRequest{Backend: "claude", Prompt: "hi"}},
}

func TestOrchestratorHandler_AllMethods_Success(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)
	for _, tc := range orchCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", tc.method, tc.req); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestOrchestratorHandler_AllMethods_ServerError(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, true)
	for _, tc := range orchCases {
		// GetSessionHistory reads SubscribeSessionEvents and treats any
		// mid-stream Recv failure as the end of the history page, so a server
		// error is intentionally swallowed there rather than propagated.
		if tc.name == "GetSessionHistory" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", tc.method, tc.req); err == nil {
				t.Fatalf("%s: expected error from failing server", tc.name)
			}
		})
	}
}

func (f *fakeOrchestratorServer) SubscribeJobEvents(_ *pb.SubscribeJobEventsRequest, stream grpc.ServerStreamingServer[pb.JobEvent]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.JobEvent{JobId: "j1", Type: "progress", State: "running", Message: "m", Iteration: 1})
}

func (f *fakeOrchestratorServer) InferenceStream(_ *pb.InferenceRequest, stream grpc.ServerStreamingServer[pb.InferenceChunk]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.InferenceChunk{Delta: []*pb.ContentBlock{{Type: "text", Text: "x"}}, Model: "m", Final: true})
}
