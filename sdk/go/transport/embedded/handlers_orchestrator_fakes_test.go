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
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errOrch is the sentinel error returned by the fake orchestrator services to
// drive the error-propagation branch of every orchestrator/project handler.
var errOrch = errors.New("orchestrator: boom")

// fakeAgentService is a configurable orchestration.AgentService. When fail is
// true every method returns errOrch; otherwise each returns a non-nil
// zero-value response so the handlers reach their conversion code.
type fakeAgentService struct {
	fail         bool
	blockCalls   []blockCall
	readmitCalls []blockCall
	deleteCalls  []blockCall

	// listJobsFilter records the narrowing the last ListJobs carried, so a
	// transport that drops the caller's filter is caught rather than
	// silently listing the whole tenant.
	listJobsFilter  *orchestration.JobFilter
	listTasksFilter *orchestration.TaskFilter

	// listJobs is what ListJobs answers with when a test needs particular
	// records; nil keeps the single bare job the conversion tests rely on.
	listJobs []*orchestration.JobStatus
}

func (f *fakeAgentService) err() error {
	if f.fail {
		return errOrch
	}
	return nil
}

func (f *fakeAgentService) DiscoverProjects(ctx context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error) {
	return &orchestration.DiscoverProjectsResponse{}, f.err()
}

func (f *fakeAgentService) RegisterProject(ctx context.Context, req *orchestration.RegisterProjectRequest) (*orchestration.RegisterProjectResponse, error) {
	return &orchestration.RegisterProjectResponse{}, f.err()
}

func (f *fakeAgentService) ListProjects(ctx context.Context, req *orchestration.ListProjectsRequest) (*orchestration.ListProjectsResponse, error) {
	return &orchestration.ListProjectsResponse{}, f.err()
}

func (f *fakeAgentService) SpawnSession(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.SpawnSessionResponse{SessionID: "s1", Backend: "b"}, nil
}

func (f *fakeAgentService) PromptSession(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.PromptSessionResponse{
		SessionID:  "s1",
		StopReason: "end_turn",
		Content:    []orchestration.ContentBlock{{Text: "hello"}},
	}, nil
}

func (f *fakeAgentService) SessionStatus(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.SessionStatusResponse{SessionID: "s1"}, nil
}

func (f *fakeAgentService) CloseSession(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.CloseSessionResponse{SessionID: "s1"}, nil
}

func (f *fakeAgentService) ListSessions(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.ListSessionsResponse{
		Sessions: []*orchestration.SessionInfo{
			{SessionID: "s1", Backend: "b", Model: "m", JobID: "job-1", Runner: "review"},
		},
	}, nil
}

func (f *fakeAgentService) Infer(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	return nil, f.err()
}

func (f *fakeAgentService) DecomposeTask(ctx context.Context, req *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.DecomposeTaskResponse{
		WorkItems: []*orchestration.WorkItem{{ID: "w1", Title: "t"}},
	}, nil
}

func (f *fakeAgentService) ListBackends(ctx context.Context) ([]*orchestration.AgentBackendInfo, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return []*orchestration.AgentBackendInfo{{Name: "b", Type: "ollama"}}, nil
}

func (f *fakeAgentService) ListBackendModels(ctx context.Context, backendName string) ([]string, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return []string{"m1"}, nil
}

func (f *fakeAgentService) BackendToolGuidance(backendName string) string { return "" }

func (f *fakeAgentService) RegisterBackend(ctx context.Context, req *orchestration.RegisterBackendRequest) error {
	return f.err()
}

func (f *fakeAgentService) RegisterAPIInferenceBackend(ctx context.Context, req *orchestration.RegisterAPIInferenceBackendRequest) error {
	return f.err()
}

func (f *fakeAgentService) HealthCheck(ctx context.Context) (*orchestration.HealthCheckReport, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.HealthCheckReport{
		Results: []*orchestration.SessionHealthResult{{SessionID: "s1", Healthy: true}},
	}, nil
}

func (f *fakeAgentService) RegisterAgent(ctx context.Context, req *orchestration.RegisterAgentRequest) (*orchestration.RegisterAgentResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.RegisterAgentResponse{Agent: &orchestration.AgentRegistration{ID: "a1"}}, nil
}

func (f *fakeAgentService) GetAgent(ctx context.Context, tenantID, agentID string) (*orchestration.AgentRegistration, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.AgentRegistration{ID: agentID}, nil
}

func (f *fakeAgentService) ListAgents(ctx context.Context, tenantID string, filter *orchestration.AgentFilter) ([]*orchestration.AgentRegistration, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return []*orchestration.AgentRegistration{{ID: "a1"}}, nil
}

func (f *fakeAgentService) RemoveAgent(ctx context.Context, tenantID, agentID string) error {
	return f.err()
}

func (f *fakeAgentService) AgentHeartbeat(ctx context.Context, tenantID, agentID string, _ *orchestration.AgentObservation) error {
	return f.err()
}

func (f *fakeAgentService) CreateTask(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.Task{ID: "t1"}, nil
}

func (f *fakeAgentService) GetTask(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.Task{
		ID:     taskID,
		Result: &orchestration.TaskResult{Output: "done"},
	}, nil
}

func (f *fakeAgentService) ListTasks(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	f.listTasksFilter = filter
	return []*orchestration.Task{{ID: "t1"}}, nil
}

func (f *fakeAgentService) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	return f.err()
}

func (f *fakeAgentService) CompleteTask(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
	return f.err()
}

func (f *fakeAgentService) BlockTask(ctx context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error {
	if err := f.err(); err != nil {
		return err
	}
	f.blockCalls = append(f.blockCalls, blockCall{TenantID: tenantID, TaskID: taskID, Kind: kind, Reason: reason})
	return nil
}

func (f *fakeAgentService) ReadmitTask(ctx context.Context, tenantID, taskID string) error {
	if err := f.err(); err != nil {
		return err
	}
	f.readmitCalls = append(f.readmitCalls, blockCall{TenantID: tenantID, TaskID: taskID})
	return nil
}

func (f *fakeAgentService) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	if err := f.err(); err != nil {
		return err
	}
	f.deleteCalls = append(f.deleteCalls, blockCall{TenantID: tenantID, TaskID: taskID})
	return nil
}

// blockCall records one BlockTask / ReadmitTask / DeleteTask invocation.
type blockCall struct {
	TenantID string
	TaskID   string
	Kind     orchestration.TaskBlockKind
	Reason   string
}

func (f *fakeAgentService) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.StartJobResponse{JobID: "j1"}, nil
}

func (f *fakeAgentService) GetJobStatus(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.JobStatus{JobID: jobID}, nil
}

func (f *fakeAgentService) ListJobs(ctx context.Context, tenantID string, filter *orchestration.JobFilter) ([]*orchestration.JobStatus, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	f.listJobsFilter = filter
	if f.listJobs != nil {
		return f.listJobs, nil
	}
	return []*orchestration.JobStatus{{JobID: "j1"}}, nil
}

func (f *fakeAgentService) CancelJob(ctx context.Context, tenantID, jobID string) error {
	return f.err()
}

func (f *fakeAgentService) SubscribeJobEvents(ctx context.Context, tenantID, jobID string) (<-chan *orchestration.JobEvent, error) {
	return nil, f.err()
}

func (f *fakeAgentService) SpawnChildJob(ctx context.Context, req *orchestration.SpawnChildJobRequest) (*orchestration.SpawnChildJobResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.SpawnChildJobResponse{ChildJobID: "cj1"}, nil
}

func (f *fakeAgentService) SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []orchestration.Artifact) error {
	return f.err()
}

func (f *fakeAgentService) GetTaskArtifact(ctx context.Context, tenantID, taskID, name string) (*orchestration.Artifact, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.Artifact{Name: name}, nil
}

func (f *fakeAgentService) ListTaskArtifacts(ctx context.Context, tenantID, taskID string) ([]orchestration.Artifact, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return []orchestration.Artifact{{Name: "a"}}, nil
}

func (f *fakeAgentService) ListWorkflows(ctx context.Context) ([]*orchestration.WorkflowDefinition, error) {
	return nil, f.err()
}

func (f *fakeAgentService) GetWorkflow(ctx context.Context, name string) (*orchestration.WorkflowDefinition, error) {
	return nil, f.err()
}

func (f *fakeAgentService) RegisterWorkflow(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	return f.err()
}

func (f *fakeAgentService) UpdateWorkflow(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	return f.err()
}

func (f *fakeAgentService) DeleteWorkflow(ctx context.Context, name string) error {
	return f.err()
}

// fakeProjectService is a configurable service.ProjectService.
type fakeProjectService struct {
	fail bool
}

func (f *fakeProjectService) err() error {
	if f.fail {
		return errOrch
	}
	return nil
}

func (f *fakeProjectService) CreateProject(ctx context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.CreateProjectResponse{Project: &orchestration.Project{ID: "p1", Name: req.Name}}, nil
}

func (f *fakeProjectService) GetProject(ctx context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.GetProjectResponse{Project: &orchestration.Project{ID: req.ProjectID}}, nil
}

func (f *fakeProjectService) GetProjectByName(ctx context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.GetProjectResponse{Project: &orchestration.Project{Name: req.Name}}, nil
}

func (f *fakeProjectService) UpdateProject(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.UpdateProjectResponse{Project: &orchestration.Project{ID: req.ProjectID}}, nil
}

func (f *fakeProjectService) DeleteProject(ctx context.Context, req *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.DeleteProjectResponse{}, nil
}

func (f *fakeProjectService) ListProjects(ctx context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.ListProjectsResponse{Projects: []*orchestration.Project{{ID: "p1"}}}, nil
}

func (f *fakeProjectService) AuthorizeAgent(ctx context.Context, req *service.AuthorizeAgentRequest) error {
	return f.err()
}

func (f *fakeProjectService) GrantAgentRole(ctx context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.GrantAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID}, nil
}

func (f *fakeProjectService) RevokeAgentRole(ctx context.Context, req *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.RevokeAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID}, nil
}

// fakeAgentAdminService is a configurable service.AgentAdminService.
type fakeAgentAdminService struct {
	fail bool
}

func (f *fakeAgentAdminService) err() error {
	if f.fail {
		return errOrch
	}
	return nil
}

func (f *fakeAgentAdminService) DisableAgent(ctx context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.DisableAgentResponse{AgentID: req.AgentID, Disabled: true}, nil
}

func (f *fakeAgentAdminService) EnableAgent(ctx context.Context, req *service.EnableAgentRequest) (*service.EnableAgentResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &service.EnableAgentResponse{AgentID: req.AgentID, Enabled: true}, nil
}

func (f *fakeAgentAdminService) GetAgentRegistration(ctx context.Context, req *service.GetAgentRegistrationRequest) (*service.GetAgentRegistrationResponse, error) {
	return &service.GetAgentRegistrationResponse{}, f.err()
}

func (f *fakeAgentAdminService) ListAgentRegistrations(ctx context.Context, req *service.ListAgentRegistrationsRequest) (*service.ListAgentRegistrationsResponse, error) {
	return &service.ListAgentRegistrationsResponse{}, f.err()
}

// orchMock embeds the base mock and overrides Agent/Project/AgentAdmin.
type orchMock struct {
	*mockQuicBaaS
	agent orchestration.AgentService
	proj  service.ProjectService
	admin service.AgentAdminService
}

func (m *orchMock) Agent() orchestration.AgentService     { return m.agent }
func (m *orchMock) Project() service.ProjectService       { return m.proj }
func (m *orchMock) Account() service.AccountService       { return nil }
func (m *orchMock) AgentAdmin() service.AgentAdminService { return m.admin }

// PromptSessionAsync mirrors the production contract: the turn is acknowledged
// immediately and its output would flow to the session event log.
func (f *fakeAgentService) PromptSessionAsync(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.PromptTurnAck{
		SessionID: req.SessionID,
		TurnID:    "turn-test",
		Status:    orchestration.TurnStatusAccepted,
		Sequence:  1,
	}, nil
}

// The ListPage methods answer the paged listings by running the real
// orchestration.ApplyPage over what this double already returns, so a transport test
// exercises the shipped paging contract rather than a second copy of it.
func (m *fakeAgentService) ListJobsPage(ctx context.Context, tenantID string, filter *orchestration.JobFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.JobStatus], error) {
	jobs, err := m.ListJobs(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(jobs, page, orchestration.JobPageSpec())
}

func (m *fakeAgentService) ListTasksPage(ctx context.Context, tenantID string, filter *orchestration.TaskFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	tasks, err := m.ListTasks(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(tasks, page, orchestration.TaskPageSpec())
}

func (m *fakeAgentService) ListAgentsPage(ctx context.Context, tenantID string, filter *orchestration.AgentFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.AgentRegistration], error) {
	agents, err := m.ListAgents(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(agents, page, orchestration.AgentPageSpec())
}

func (m *fakeAgentService) ListWorkflowsPage(ctx context.Context, filter *orchestration.WorkflowFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.WorkflowDefinition], error) {
	workflows, err := m.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(workflows, page, orchestration.WorkflowPageSpec())
}
