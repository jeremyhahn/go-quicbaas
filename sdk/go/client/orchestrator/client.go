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

package orchestrator

import (
	"context"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Orchestrator"
)

// Client provides orchestrator management operations including session,
// task, backend, and fleet management.
type Client interface {
	// SpawnSession creates a new agent session.
	SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error)

	// PromptSession sends a prompt to an existing session.
	PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error)

	// SessionStatus returns the status of a session.
	SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error)

	// CloseSession terminates a session.
	CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error)

	// ListSessions returns all active sessions.
	ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error)

	// CreateTask creates a new task.
	CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error)

	// GetTask returns a task by ID.
	GetTask(ctx context.Context, req *GetTaskRequest) (*Task, error)

	// ListTasks returns tasks matching the filter criteria.
	ListTasks(ctx context.Context, req *ListTasksRequest) (*ListTasksResponse, error)

	// ClaimTask claims a pending task for execution.
	ClaimTask(ctx context.Context, req *ClaimTaskRequest) (*ClaimTaskResponse, error)

	// CompleteTask marks a task as completed.
	CompleteTask(ctx context.Context, req *CompleteTaskRequest) (*CompleteTaskResponse, error)

	// ListBackends returns all available backends.
	ListBackends(ctx context.Context) (*ListBackendsResponse, error)

	// Health returns the orchestrator health check report.
	Health(ctx context.Context) (*HealthCheckReport, error)

	// RegisterAgent registers a new agent with the fleet.
	RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*RegisterAgentResponse, error)

	// GetAgent returns an agent by ID.
	GetAgent(ctx context.Context, req *GetAgentRequest) (*AgentRegistration, error)

	// ListAgents returns agents matching the filter criteria.
	ListAgents(ctx context.Context, req *ListAgentsRequest) (*ListAgentsResponse, error)

	// RemoveAgent removes an agent from the fleet.
	RemoveAgent(ctx context.Context, req *RemoveAgentRequest) (*RemoveAgentResponse, error)

	// AgentHeartbeat sends a heartbeat for an agent.
	AgentHeartbeat(ctx context.Context, req *AgentHeartbeatRequest) (*AgentHeartbeatResponse, error)

	// DecomposeTask decomposes a task into structured work items using an agent.
	DecomposeTask(ctx context.Context, req *DecomposeTaskRequest) (*DecomposeTaskResponse, error)

	// StartJob starts an autonomous orchestration control loop.
	StartJob(ctx context.Context, req *StartJobRequest) (*StartJobResponse, error)

	// GetJobStatus returns the current status of an orchestration job.
	GetJobStatus(ctx context.Context, req *GetJobStatusRequest) (*JobStatus, error)

	// ListJobs returns all jobs for a tenant.
	ListJobs(ctx context.Context, req *ListJobsRequest) (*ListJobsResponse, error)

	// CancelJob cancels a running orchestration job.
	CancelJob(ctx context.Context, req *CancelJobRequest) (*CancelJobResponse, error)
	// SubscribeJobEvents returns a channel of real-time events for a running job.
	SubscribeJobEvents(ctx context.Context, req *SubscribeJobEventsRequest) (<-chan *JobEvent, error)
}

// Session management types

// SpawnSessionRequest is a request to create a new agent session.
type SpawnSessionRequest struct {
	Backend   string            `json:"backend"`
	ProjectID string            `json:"project_id,omitempty"`
	Model     string            `json:"model,omitempty"`
	WorkDir   string            `json:"work_dir,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Sandboxed bool              `json:"sandboxed,omitempty"`
}

// SpawnSessionResponse is the response from creating a new session.
type SpawnSessionResponse struct {
	SessionID   string `json:"session_id"`
	Backend     string `json:"backend"`
	ContainerID string `json:"container_id,omitempty"`
}

// PromptSessionRequest is a request to send a prompt to a session.
type PromptSessionRequest struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
}

// PromptSessionResponse is the response from prompting a session.
type PromptSessionResponse struct {
	SessionID string `json:"session_id"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
}

// SessionStatusRequest is a request to check session status.
type SessionStatusRequest struct {
	SessionID string `json:"session_id"`
}

// SessionStatusResponse is the response from checking session status.
type SessionStatusResponse struct {
	SessionID   string `json:"session_id"`
	Backend     string `json:"backend"`
	Status      string `json:"status"`
	ContainerID string `json:"container_id,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	LastActive  int64  `json:"last_active"`
}

// CloseSessionRequest is a request to close a session.
type CloseSessionRequest struct {
	SessionID string `json:"session_id"`
}

// CloseSessionResponse is the response from closing a session.
type CloseSessionResponse struct {
	SessionID string `json:"session_id"`
	Closed    bool   `json:"closed"`
}

// ListSessionsRequest is a request to list sessions.
type ListSessionsRequest struct {
	Backend string `json:"backend,omitempty"`
	Status  string `json:"status,omitempty"`
}

// ListSessionsResponse is the response from listing sessions.
type ListSessionsResponse struct {
	Sessions []*SessionStatusResponse `json:"sessions"`
	Count    int                      `json:"count"`
}

// Task management types

// TaskResult represents the result of a completed task.
type TaskResult struct {
	Output   string            `json:"output"`
	Files    []string          `json:"files,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Task represents a task in the orchestrator.
type Task struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	TeamName     string            `json:"team_name"`
	Subject      string            `json:"subject"`
	Description  string            `json:"description,omitempty"`
	Status       string            `json:"status"`
	AssignedTo   string            `json:"assigned_to,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
	CreatedAt    int64             `json:"created_at"`
	ClaimedAt    int64             `json:"claimed_at,omitempty"`
	CompletedAt  int64             `json:"completed_at,omitempty"`
	Version      uint64            `json:"version"`
	Result       *TaskResult       `json:"result,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// CreateTaskRequest is a request to create a new task.
type CreateTaskRequest struct {
	TenantID     string            `json:"tenant_id"`
	TeamName     string            `json:"team_name,omitempty"`
	Subject      string            `json:"subject"`
	Description  string            `json:"description,omitempty"`
	AssignedTo   string            `json:"assigned_to,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// GetTaskRequest is a request to get a task by ID.
type GetTaskRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`
}

// ListTasksRequest is a request to list tasks.
type ListTasksRequest struct {
	TenantID   string `json:"tenant_id"`
	Status     string `json:"status,omitempty"`
	TeamName   string `json:"team_name,omitempty"`
	AssignedTo string `json:"assigned_to,omitempty"`
}

// ListTasksResponse is the response from listing tasks.
type ListTasksResponse struct {
	Tasks []*Task `json:"tasks"`
	Count int     `json:"count"`
}

// ClaimTaskRequest is a request to claim a task for execution.
type ClaimTaskRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`
	AgentID  string `json:"agent_id"`
}

// ClaimTaskResponse is the response from claiming a task.
type ClaimTaskResponse struct {
	TaskID  string `json:"task_id"`
	AgentID string `json:"agent_id"`
	Claimed bool   `json:"claimed"`
}

// CompleteTaskRequest is a request to mark a task as completed.
type CompleteTaskRequest struct {
	TenantID string      `json:"tenant_id"`
	TaskID   string      `json:"task_id"`
	Result   *TaskResult `json:"result,omitempty"`
}

// CompleteTaskResponse is the response from completing a task.
type CompleteTaskResponse struct {
	TaskID    string `json:"task_id"`
	Completed bool   `json:"completed"`
}

// Backend management types

// BackendInfo represents information about an available backend.
type BackendInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Available bool   `json:"available"`
}

// ListBackendsResponse is the response from listing backends.
type ListBackendsResponse struct {
	Backends []*BackendInfo `json:"backends"`
	Count    int            `json:"count"`
}

// HealthCheckReport represents the orchestrator health check report.
type HealthCheckReport struct {
	Healthy    bool              `json:"healthy"`
	Status     string            `json:"status"`
	Components map[string]string `json:"components,omitempty"`
	Timestamp  int64             `json:"timestamp"`
}

// Fleet management types

// RegisterAgentRequest is a request to register a new agent.
type RegisterAgentRequest struct {
	TenantID  string            `json:"tenant_id"`
	Name      string            `json:"name"`
	Host      string            `json:"host"`
	AgentType string            `json:"agent_type"`
	Backend   string            `json:"backend,omitempty"`
	Model     string            `json:"model,omitempty"`
	WorkDir   string            `json:"work_dir,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// RegisterAgentResponse is the response from registering an agent.
type RegisterAgentResponse struct {
	Agent *AgentRegistration `json:"agent"`
}

// AgentRegistration represents a registered agent.
type AgentRegistration struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Backend      string            `json:"backend"`
	Model        string            `json:"model,omitempty"`
	AgentType    string            `json:"agent_type"`
	Status       string            `json:"status"`
	WorkDir      string            `json:"work_dir,omitempty"`
	LastSeen     int64             `json:"last_seen"`
	RegisteredAt int64             `json:"registered_at"`
	Version      uint64            `json:"version"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// GetAgentRequest is a request to get an agent by ID.
type GetAgentRequest struct {
	TenantID string `json:"tenant_id"`
	AgentID  string `json:"agent_id"`
}

// ListAgentsRequest is a request to list agents.
type ListAgentsRequest struct {
	TenantID  string `json:"tenant_id"`
	Host      string `json:"host,omitempty"`
	Status    string `json:"status,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	Backend   string `json:"backend,omitempty"`
	Model     string `json:"model,omitempty"`
}

// ListAgentsResponse is the response from listing agents.
type ListAgentsResponse struct {
	Agents []*AgentRegistration `json:"agents"`
	Count  int                  `json:"count"`
}

// RemoveAgentRequest is a request to remove an agent.
type RemoveAgentRequest struct {
	TenantID string `json:"tenant_id"`
	AgentID  string `json:"agent_id"`
}

// RemoveAgentResponse is the response from removing an agent.
type RemoveAgentResponse struct {
	AgentID string `json:"agent_id"`
	Removed bool   `json:"removed"`
}

// AgentHeartbeatRequest is a request to send an agent heartbeat.
type AgentHeartbeatRequest struct {
	TenantID string `json:"tenant_id"`
	AgentID  string `json:"agent_id"`
}

// AgentHeartbeatResponse is the response from sending an agent heartbeat.
type AgentHeartbeatResponse struct {
	AgentID   string `json:"agent_id"`
	Heartbeat bool   `json:"heartbeat"`
}

// Task decomposition types

// DecomposeTaskRequest is a request to decompose a task into work items.
type DecomposeTaskRequest struct {
	Backend    string   `json:"backend,omitempty"`
	Model      string   `json:"model,omitempty"`
	Task       string   `json:"task"`
	ProjectIDs []string `json:"project_ids,omitempty"`
}

// DecomposeTaskResponse is the response from decomposing a task.
type DecomposeTaskResponse struct {
	WorkItems []*WorkItem `json:"work_items"`
	Timestamp int64       `json:"timestamp"`
}

// WorkItem represents a decomposed unit of work.
type WorkItem struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"project_id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Priority     int      `json:"priority"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// Internal request types for parameterless methods.
type healthRequest struct{}
type listBackendsRequest struct{}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new orchestrator client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// SpawnSession creates a new agent session.
func (c *client) SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error) {
	if err := validateSpawnSessionRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SpawnSession", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SpawnSession", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.SpawnSession", "nil response from server")
	}

	result, ok := resp.(*SpawnSessionResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.SpawnSession", "invalid response type")
	}

	return result, nil
}

// PromptSession sends a prompt to an existing session.
func (c *client) PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error) {
	if err := validatePromptSessionRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.PromptSession", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "PromptSession", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.PromptSession", "nil response from server")
	}

	result, ok := resp.(*PromptSessionResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.PromptSession", "invalid response type")
	}

	return result, nil
}

// SessionStatus returns the status of a session.
func (c *client) SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
	if err := validateSessionStatusRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SessionStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SessionStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.SessionStatus", "nil response from server")
	}

	result, ok := resp.(*SessionStatusResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.SessionStatus", "invalid response type")
	}

	return result, nil
}

// CloseSession terminates a session.
func (c *client) CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error) {
	if err := validateCloseSessionRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CloseSession", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CloseSession", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CloseSession", "nil response from server")
	}

	result, ok := resp.(*CloseSessionResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CloseSession", "invalid response type")
	}

	return result, nil
}

// ListSessions returns all active sessions.
func (c *client) ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListSessions", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListSessions", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListSessions", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListSessionsResponse{Sessions: []*SessionStatusResponse{}, Count: 0}, nil
	}

	result, ok := resp.(*ListSessionsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListSessions", "invalid response type")
	}

	return result, nil
}

// CreateTask creates a new task.
func (c *client) CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error) {
	if err := validateCreateTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CreateTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CreateTask", "nil response from server")
	}

	result, ok := resp.(*Task)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CreateTask", "invalid response type")
	}

	return result, nil
}

// GetTask returns a task by ID.
func (c *client) GetTask(ctx context.Context, req *GetTaskRequest) (*Task, error) {
	if err := validateGetTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetTask", "nil response from server")
	}

	result, ok := resp.(*Task)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetTask", "invalid response type")
	}

	return result, nil
}

// ListTasks returns tasks matching the filter criteria.
func (c *client) ListTasks(ctx context.Context, req *ListTasksRequest) (*ListTasksResponse, error) {
	if err := validateListTasksRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListTasks", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListTasks", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListTasksResponse{Tasks: []*Task{}, Count: 0}, nil
	}

	result, ok := resp.(*ListTasksResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListTasks", "invalid response type")
	}

	return result, nil
}

// ClaimTask claims a pending task for execution.
func (c *client) ClaimTask(ctx context.Context, req *ClaimTaskRequest) (*ClaimTaskResponse, error) {
	if err := validateClaimTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ClaimTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ClaimTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.ClaimTask", "nil response from server")
	}

	result, ok := resp.(*ClaimTaskResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ClaimTask", "invalid response type")
	}

	return result, nil
}

// CompleteTask marks a task as completed.
func (c *client) CompleteTask(ctx context.Context, req *CompleteTaskRequest) (*CompleteTaskResponse, error) {
	if err := validateCompleteTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CompleteTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CompleteTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CompleteTask", "nil response from server")
	}

	result, ok := resp.(*CompleteTaskResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CompleteTask", "invalid response type")
	}

	return result, nil
}

// ListBackends returns all available backends.
func (c *client) ListBackends(ctx context.Context) (*ListBackendsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListBackends", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListBackends", &listBackendsRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListBackendsResponse{Backends: []*BackendInfo{}, Count: 0}, nil
	}

	result, ok := resp.(*ListBackendsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListBackends", "invalid response type")
	}

	return result, nil
}

// Health returns the orchestrator health check report.
func (c *client) Health(ctx context.Context) (*HealthCheckReport, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.Health", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Health", &healthRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.Health", "nil response from server")
	}

	result, ok := resp.(*HealthCheckReport)
	if !ok {
		return nil, errors.NewInternal("orchestrator.Health", "invalid response type")
	}

	return result, nil
}

// RegisterAgent registers a new agent with the fleet.
func (c *client) RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*RegisterAgentResponse, error) {
	if err := validateRegisterAgentRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.RegisterAgent", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RegisterAgent", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.RegisterAgent", "nil response from server")
	}

	result, ok := resp.(*RegisterAgentResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.RegisterAgent", "invalid response type")
	}

	return result, nil
}

// GetAgent returns an agent by ID.
func (c *client) GetAgent(ctx context.Context, req *GetAgentRequest) (*AgentRegistration, error) {
	if err := validateGetAgentRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetAgent", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetAgent", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetAgent", "nil response from server")
	}

	result, ok := resp.(*AgentRegistration)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetAgent", "invalid response type")
	}

	return result, nil
}

// ListAgents returns agents matching the filter criteria.
func (c *client) ListAgents(ctx context.Context, req *ListAgentsRequest) (*ListAgentsResponse, error) {
	if err := validateListAgentsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListAgents", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListAgents", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListAgentsResponse{Agents: []*AgentRegistration{}, Count: 0}, nil
	}

	result, ok := resp.(*ListAgentsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListAgents", "invalid response type")
	}

	return result, nil
}

// RemoveAgent removes an agent from the fleet.
func (c *client) RemoveAgent(ctx context.Context, req *RemoveAgentRequest) (*RemoveAgentResponse, error) {
	if err := validateRemoveAgentRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.RemoveAgent", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RemoveAgent", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.RemoveAgent", "nil response from server")
	}

	result, ok := resp.(*RemoveAgentResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.RemoveAgent", "invalid response type")
	}

	return result, nil
}

// AgentHeartbeat sends a heartbeat for an agent.
func (c *client) AgentHeartbeat(ctx context.Context, req *AgentHeartbeatRequest) (*AgentHeartbeatResponse, error) {
	if err := validateAgentHeartbeatRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.AgentHeartbeat", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "AgentHeartbeat", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.AgentHeartbeat", "nil response from server")
	}

	result, ok := resp.(*AgentHeartbeatResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.AgentHeartbeat", "invalid response type")
	}

	return result, nil
}

// DecomposeTask decomposes a task into structured work items using an agent.
func (c *client) DecomposeTask(ctx context.Context, req *DecomposeTaskRequest) (*DecomposeTaskResponse, error) {
	if err := validateDecomposeTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.DecomposeTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DecomposeTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.DecomposeTask", "nil response from server")
	}

	result, ok := resp.(*DecomposeTaskResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.DecomposeTask", "invalid response type")
	}

	return result, nil
}

// Validation functions

func validateSpawnSessionRequest(req *SpawnSessionRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.SpawnSession", "request is required")
	}
	if req.Backend == "" {
		return errors.NewInvalidInput("orchestrator.SpawnSession", "backend is required")
	}
	return nil
}

func validatePromptSessionRequest(req *PromptSessionRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.PromptSession", "request is required")
	}
	if req.SessionID == "" {
		return errors.NewInvalidInput("orchestrator.PromptSession", "session ID is required")
	}
	if req.Prompt == "" {
		return errors.NewInvalidInput("orchestrator.PromptSession", "prompt is required")
	}
	return nil
}

func validateSessionStatusRequest(req *SessionStatusRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.SessionStatus", "request is required")
	}
	if req.SessionID == "" {
		return errors.NewInvalidInput("orchestrator.SessionStatus", "session ID is required")
	}
	return nil
}

func validateCloseSessionRequest(req *CloseSessionRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.CloseSession", "request is required")
	}
	if req.SessionID == "" {
		return errors.NewInvalidInput("orchestrator.CloseSession", "session ID is required")
	}
	return nil
}

func validateCreateTaskRequest(req *CreateTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.CreateTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.CreateTask", "tenant ID is required")
	}
	if req.Subject == "" {
		return errors.NewInvalidInput("orchestrator.CreateTask", "subject is required")
	}
	return nil
}

func validateGetTaskRequest(req *GetTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.GetTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.GetTask", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.GetTask", "task ID is required")
	}
	return nil
}

func validateListTasksRequest(req *ListTasksRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ListTasks", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.ListTasks", "tenant ID is required")
	}
	return nil
}

func validateClaimTaskRequest(req *ClaimTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ClaimTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.ClaimTask", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.ClaimTask", "task ID is required")
	}
	if req.AgentID == "" {
		return errors.NewInvalidInput("orchestrator.ClaimTask", "agent ID is required")
	}
	return nil
}

func validateCompleteTaskRequest(req *CompleteTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.CompleteTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.CompleteTask", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.CompleteTask", "task ID is required")
	}
	return nil
}

func validateRegisterAgentRequest(req *RegisterAgentRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.RegisterAgent", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.RegisterAgent", "tenant ID is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("orchestrator.RegisterAgent", "name is required")
	}
	if req.Host == "" {
		return errors.NewInvalidInput("orchestrator.RegisterAgent", "host is required")
	}
	if req.AgentType == "" {
		return errors.NewInvalidInput("orchestrator.RegisterAgent", "agent type is required")
	}
	return nil
}

func validateGetAgentRequest(req *GetAgentRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.GetAgent", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.GetAgent", "tenant ID is required")
	}
	if req.AgentID == "" {
		return errors.NewInvalidInput("orchestrator.GetAgent", "agent ID is required")
	}
	return nil
}

func validateListAgentsRequest(req *ListAgentsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ListAgents", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.ListAgents", "tenant ID is required")
	}
	return nil
}

func validateRemoveAgentRequest(req *RemoveAgentRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.RemoveAgent", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.RemoveAgent", "tenant ID is required")
	}
	if req.AgentID == "" {
		return errors.NewInvalidInput("orchestrator.RemoveAgent", "agent ID is required")
	}
	return nil
}

func validateAgentHeartbeatRequest(req *AgentHeartbeatRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.AgentHeartbeat", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.AgentHeartbeat", "tenant ID is required")
	}
	if req.AgentID == "" {
		return errors.NewInvalidInput("orchestrator.AgentHeartbeat", "agent ID is required")
	}
	return nil
}

func validateDecomposeTaskRequest(req *DecomposeTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.DecomposeTask", "request is required")
	}
	if req.Task == "" {
		return errors.NewInvalidInput("orchestrator.DecomposeTask", "task is required")
	}
	return nil
}

// StartJob starts an autonomous orchestration control loop.
func (c *client) StartJob(ctx context.Context, req *StartJobRequest) (*StartJobResponse, error) {
	if err := validateStartJobRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.StartJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "StartJob", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.StartJob", "nil response from server")
	}

	result, ok := resp.(*StartJobResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.StartJob", "invalid response type")
	}

	return result, nil
}

// GetJobStatus returns the current status of an orchestration job.
func (c *client) GetJobStatus(ctx context.Context, req *GetJobStatusRequest) (*JobStatus, error) {
	if err := validateGetJobStatusRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetJobStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetJobStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetJobStatus", "nil response from server")
	}

	result, ok := resp.(*JobStatus)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetJobStatus", "invalid response type")
	}

	return result, nil
}

// ListJobs returns all jobs for a tenant.
func (c *client) ListJobs(ctx context.Context, req *ListJobsRequest) (*ListJobsResponse, error) {
	if err := validateListJobsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListJobs", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListJobs", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.ListJobs", "nil response from server")
	}

	result, ok := resp.(*ListJobsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListJobs", "invalid response type")
	}

	return result, nil
}

// CancelJob cancels a running orchestration job.
func (c *client) CancelJob(ctx context.Context, req *CancelJobRequest) (*CancelJobResponse, error) {
	if err := validateCancelJobRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CancelJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CancelJob", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CancelJob", "nil response from server")
	}

	result, ok := resp.(*CancelJobResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CancelJob", "invalid response type")
	}

	return result, nil
}

// SubscribeJobEvents subscribes to real-time events for an orchestration job.
func (c *client) SubscribeJobEvents(ctx context.Context, req *SubscribeJobEventsRequest) (<-chan *JobEvent, error) {
	if err := validateSubscribeJobEventsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SubscribeJobEvents", "context is required")
	}

	stream, err := c.transport.RequestStream(ctx, serviceName, "SubscribeJobEvents", req)
	if err != nil {
		return nil, err
	}

	sub := &jobEventSubscription{
		stream: stream,
		events: make(chan *JobEvent, 64),
	}

	go sub.receiveLoop(ctx)

	return sub.events, nil
}

// jobEventSubscription manages the lifecycle of a job event stream.
type jobEventSubscription struct {
	stream transport.Stream
	events chan *JobEvent
	closed atomic.Bool
}

// receiveLoop reads events from the stream and forwards them to the channel.
func (s *jobEventSubscription) receiveLoop(ctx context.Context) {
	defer func() {
		close(s.events)
		s.stream.Close()
		s.closed.Store(true)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := s.stream.Recv()
			if err != nil {
				return
			}

			event, ok := msg.(*JobEvent)
			if !ok {
				continue
			}

			select {
			case s.events <- event:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Validation functions for job operations.

func validateStartJobRequest(req *StartJobRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.StartJob", "request is required")
	}
	if req.Description == "" {
		return errors.NewInvalidInput("orchestrator.StartJob", "description is required")
	}
	return nil
}

func validateGetJobStatusRequest(req *GetJobStatusRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.GetJobStatus", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.GetJobStatus", "tenant ID is required")
	}
	if req.JobID == "" {
		return errors.NewInvalidInput("orchestrator.GetJobStatus", "job ID is required")
	}
	return nil
}

func validateListJobsRequest(req *ListJobsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ListJobs", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.ListJobs", "tenant ID is required")
	}
	return nil
}

func validateCancelJobRequest(req *CancelJobRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.CancelJob", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.CancelJob", "tenant ID is required")
	}
	if req.JobID == "" {
		return errors.NewInvalidInput("orchestrator.CancelJob", "job ID is required")
	}
	return nil
}

func validateSubscribeJobEventsRequest(req *SubscribeJobEventsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.SubscribeJobEvents", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.SubscribeJobEvents", "tenant ID is required")
	}
	if req.JobID == "" {
		return errors.NewInvalidInput("orchestrator.SubscribeJobEvents", "job ID is required")
	}
	return nil
}

// =============================================================================
// Job Orchestration Types
// =============================================================================

// StartJobRequest starts an autonomous orchestration job.
type StartJobRequest struct {
	TenantID      string `json:"tenant_id"`
	Description   string `json:"description"`
	LeadBackend   string `json:"lead_backend,omitempty"`
	LeadModel     string `json:"lead_model,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	MaxIterations int    `json:"max_iterations,omitempty"`
	PollInterval  string `json:"poll_interval,omitempty"`
	WorkflowName  string `json:"workflow_name,omitempty"`
	WorkflowFile  string `json:"workflow_file,omitempty"`
}

// StartJobResponse contains the result of starting an orchestration job.
type StartJobResponse struct {
	JobID     string `json:"job_id"`
	Timestamp int64  `json:"timestamp"`
}

// GetJobStatusRequest is a request to get the status of a job.
type GetJobStatusRequest struct {
	TenantID string `json:"tenant_id"`
	JobID    string `json:"job_id"`
}

// JobStatus reports the current state of an orchestration job.
type JobStatus struct {
	JobID       string          `json:"job_id"`
	State       string          `json:"state"`
	Iteration   int             `json:"iteration"`
	TaskCount   int             `json:"task_count"`
	Completed   int             `json:"completed"`
	Failed      int             `json:"failed"`
	Description string          `json:"description"`
	StartedAt   int64           `json:"started_at"`
	UpdatedAt   int64           `json:"updated_at"`
	Error       string          `json:"error,omitempty"`
	Events      []JobAuditEvent `json:"events,omitempty"`
}

// JobAuditEvent records a single event in the job's complete audit trail.
type JobAuditEvent struct {
	Timestamp int64  `json:"timestamp"`
	Type      string `json:"type"`
	Details   string `json:"details"`
}

// CancelJobRequest is a request to cancel a running job.
type CancelJobRequest struct {
	TenantID string `json:"tenant_id"`
	JobID    string `json:"job_id"`
}

// CancelJobResponse contains the result of cancelling a job.
type CancelJobResponse struct {
	JobID     string `json:"job_id"`
	Cancelled bool   `json:"cancelled"`
}

// ListJobsRequest is a request to list all jobs for a tenant.
type ListJobsRequest struct {
	TenantID string `json:"tenant_id"`
}

// ListJobsResponse contains the list of jobs for a tenant.
type ListJobsResponse struct {
	Jobs []JobSummary `json:"jobs"`
}

// JobSummary provides a summary of an orchestration job.
type JobSummary struct {
	JobID       string `json:"job_id"`
	State       string `json:"state"`
	Iteration   int    `json:"iteration"`
	TaskCount   int    `json:"task_count"`
	Completed   int    `json:"completed"`
	Failed      int    `json:"failed"`
	Description string `json:"description"`
	StartedAt   int64  `json:"started_at"`
	UpdatedAt   int64  `json:"updated_at"`
	Error       string `json:"error,omitempty"`
	EventCount  int    `json:"event_count"`
}

// SubscribeJobEventsRequest is a request to subscribe to real-time job events.
type SubscribeJobEventsRequest struct {
	TenantID string `json:"tenant_id"`
	JobID    string `json:"job_id"`
}

// JobEvent represents a real-time event from a running orchestration job.
type JobEvent struct {
	JobID     string            `json:"job_id"`
	Type      string            `json:"type"`
	State     string            `json:"state"`
	Message   string            `json:"message"`
	Iteration int               `json:"iteration"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp int64             `json:"timestamp"`
}
