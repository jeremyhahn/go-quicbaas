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
	"time"

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
	// PromptSession sends a prompt to an agent session. The call returns as
	// soon as the server has recorded and dispatched the turn; the agent's
	// output arrives on the session event stream. See PromptSessionResponse
	// for the two response shapes.
	PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error)

	// SessionStatus returns the status of a session.
	SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error)

	// CloseSession terminates a session.
	CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error)

	// ListSessions returns all active sessions.
	ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error)

	// RotateSessionAccount moves a session onto a different pooled provider
	// account, keeping its identifier, transcript and cost history. The same
	// verb runs automatically when a turn fails on a classified provider
	// limit; this is the operator's door to it.
	//
	// Only a session spawned with AccountAuto or AccountAllow may rotate — a
	// bare AccountID pin is honored and answered with a conflict.
	RotateSessionAccount(ctx context.Context, req *RotateSessionAccountRequest) (*RotateSessionAccountResponse, error)

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

	// ListBackendModels returns the model names available on the named backend
	// as seen from the specified agent. AgentID is used as a host affinity hint
	// for the remote backend resolver; pass an empty string or "any" to skip
	// host affinity. The agent need not be online; if the backend exposes no
	// model lister or the agent is unknown, the response is an empty list with
	// no error.
	ListBackendModels(ctx context.Context, req *ListBackendModelsRequest) (*ListBackendModelsResponse, error)

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

	// SpawnChildJob fans out a child job from a parent job's lead agent.
	// Returns ErrPermissionDenied if the caller is not the lead agent of the parent job.
	SpawnChildJob(ctx context.Context, req *SpawnChildJobRequest) (*SpawnChildJobResponse, error)

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

	// GetSessionHistory fetches a page of historical session events. Used
	// by the CLI `orchestrator attach` command and the web UI's scrollback
	// pager to page backward through an in-progress session's transcript.
	GetSessionHistory(ctx context.Context, req *GetSessionHistoryRequest) (*GetSessionHistoryResponse, error)

	// SubscribeSessionEvents returns a live stream of session events. When
	// req.FromSequence > 0 the stream begins with a historical replay
	// from that sequence, then transitions to live tail when Follow is
	// true. The returned channel closes when the stream is done or the
	// context is cancelled.
	SubscribeSessionEvents(ctx context.Context, req *SubscribeSessionEventsRequest) (<-chan *SessionEventEnvelope, error)

	// Inference runs a stateless fleet-routed inference call against a named
	// backend without requiring a prior session Spawn. Returns the aggregated
	// response after the backend finishes.
	Inference(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error)

	// InferenceStream runs a stateless fleet-routed inference call and returns
	// a channel of InferenceChunk values streamed from the backend. The
	// channel is closed when the backend finishes or the context is cancelled.
	InferenceStream(ctx context.Context, req *InferenceRequest) (<-chan *InferenceChunk, error)

	// CreateWorkflow registers a new named workflow definition.
	CreateWorkflow(ctx context.Context, req *CreateWorkflowRequest) (*WorkflowDefinition, error)

	// ListWorkflows returns all registered workflow definitions for a tenant.
	ListWorkflows(ctx context.Context, req *ListWorkflowsRequest) ([]*WorkflowDefinition, error)

	// GetWorkflow returns a single workflow definition by name.
	GetWorkflow(ctx context.Context, req *GetWorkflowRequest) (*WorkflowDefinition, error)

	// UpdateWorkflow replaces an existing workflow definition by name.
	// Returns ErrNotFound if the workflow does not exist.
	UpdateWorkflow(ctx context.Context, req *UpdateWorkflowRequest) (*WorkflowDefinition, error)

	// DeleteWorkflow removes a workflow definition by name.
	// Returns ErrNotFound if the workflow does not exist.
	DeleteWorkflow(ctx context.Context, req *DeleteWorkflowRequest) error

	// DisableAgent administratively disables an agent globally. Revokes the
	// agent's OAuth2 credentials and mTLS pin so subsequent token requests fail.
	DisableAgent(ctx context.Context, req *DisableAgentRequest) (*DisableAgentResponse, error)

	// EnableAgent re-activates a previously disabled agent.
	EnableAgent(ctx context.Context, req *EnableAgentRequest) (*EnableAgentResponse, error)

	// SubmitTaskArtifacts stores typed output artifacts for a task.
	SubmitTaskArtifacts(ctx context.Context, req *SubmitTaskArtifactsRequest) (*SubmitTaskArtifactsResponse, error)

	// GetTaskArtifact retrieves a named artifact from a task.
	GetTaskArtifact(ctx context.Context, req *GetTaskArtifactRequest) (*TaskArtifact, error)

	// ListTaskArtifacts returns all output artifact metadata for a task.
	ListTaskArtifacts(ctx context.Context, req *ListTaskArtifactsRequest) (*ListTaskArtifactsResponse, error)
}

// GetSessionHistoryRequest pages through historical session events.
type GetSessionHistoryRequest struct {
	TenantID     string `json:"tenant_id,omitempty"`
	SessionID    string `json:"session_id"`
	FromSequence uint64 `json:"from_sequence,omitempty"` // inclusive lower bound; 0 = from start
	Limit        int    `json:"limit,omitempty"`         // <=0 = no limit (server enforces)
}

// GetSessionHistoryResponse carries one page of session events plus the
// cursor for the next page.
type GetSessionHistoryResponse struct {
	Events  []*SessionEventEnvelope `json:"events"`
	NextSeq uint64                  `json:"next_seq,omitempty"` // 0 when end-of-history
}

// SubscribeSessionEventsRequest opens a live stream over an existing
// session's event log.
type SubscribeSessionEventsRequest struct {
	TenantID     string `json:"tenant_id,omitempty"`
	SessionID    string `json:"session_id"`
	FromSequence uint64 `json:"from_sequence,omitempty"` // 0 = only new events; 1+ = replay-then-tail
	Follow       bool   `json:"follow"`                  // true = continue streaming after replay
}

// SessionEventEnvelope wraps one event delivered over a subscribe stream.
// PayloadJSON is the raw encoded orchestration.SessionEvent — clients
// json.Unmarshal this themselves, or use a downstream helper that dispatches
// on Kind. Exposing the raw payload keeps the SDK stable as new event kinds
// are added.
type SessionEventEnvelope struct {
	SessionID   string    `json:"session_id"`
	TenantID    string    `json:"tenant_id,omitempty"`
	Sequence    uint64    `json:"sequence"`
	PayloadJSON []byte    `json:"payload_json"`
	Timestamp   time.Time `json:"timestamp"`
	FromReplay  bool      `json:"from_replay,omitempty"`
}

// Session management types

// SandboxMode mirrors orchestration.SandboxMode for SDK callers so the SDK
// does not force a dependency on the server-side orchestration package.
type SandboxMode int32

const (
	// SandboxModeDefault defers to the server's configured policy.
	SandboxModeDefault SandboxMode = 0
	// SandboxModeRequired forces container-sandboxed execution.
	SandboxModeRequired SandboxMode = 1
	// SandboxModeNone forces raw process execution, subject to policy.
	SandboxModeNone SandboxMode = 2
)

// SpawnSessionRequest is a request to create a new agent session.
type SpawnSessionRequest struct {
	Backend      string            `json:"backend"`
	ProjectID    string            `json:"project_id,omitempty"`
	Model        string            `json:"model,omitempty"`
	WorkDir      string            `json:"work_dir,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	SandboxMode  SandboxMode       `json:"sandbox_mode,omitempty"`
	Sandboxed    bool              `json:"sandboxed,omitempty"`     // deprecated: use SandboxMode
	HostAffinity string            `json:"host_affinity,omitempty"` // pin to a specific agent daemon (Name or Host)

	// HostAffinityStrict, when true, forces hard host_affinity semantics:
	// the server returns NoMatchingAgentError (HTTP 400) if the named host
	// is offline rather than falling back to any available daemon. Defers
	// to the operator-level orchestration.host_affinity.mode default when
	// false.
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`

	// MCPServers preconfigures MCP servers for the spawned agent (e.g. the
	// QuicBaaS MCP endpoint, giving the agent cert-authenticated access to
	// platform tools). Serialized as mcp_servers; the server forwards each
	// server's connection material to the spawned agent's environment.
	MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`

	// --- Pooled provider account selection (see sdk/go/client/account) -----

	// AccountID pins the session to a specific pooled provider account.
	// Leaving both AccountID and AccountAuto unset preserves the pre-account
	// behavior: the backend authenticates however its CLI already does.
	AccountID string `json:"account_id,omitempty"`

	// AccountAuto requests strategy-based account selection when AccountID
	// is empty. This is the only way to reach the dynamic (least-usage)
	// strategy from outside the process.
	AccountAuto bool `json:"account_auto,omitempty"`

	// AccountStrategy names the selection strategy ("static",
	// "least_usage"). Empty selects the registry default. Ignored unless
	// AccountAuto is true.
	AccountStrategy string `json:"account_strategy,omitempty"`

	// AccountAllow optionally names the accounts this session may use and
	// rotate through — "these three seats, nothing else". It is an additive
	// filter, never a source: an ID the tenant does not own is simply not a
	// candidate.
	//
	// Together with AccountAuto it is what authorizes rotation. A session with
	// neither is treated as a deliberate pin and is never moved off its
	// account, so a caller that wants RotateSessionAccount to work on a
	// specific set of seats sets this.
	AccountAllow []string `json:"account_allow,omitempty"`
}

// RotateSessionAccountRequest asks the server to move one session onto a
// different pooled provider account.
type RotateSessionAccountRequest struct {
	// SessionID is the session to move. Required.
	SessionID string `json:"session_id"`

	// Reason is recorded on the closed account segment and in the session
	// transcript. Optional.
	Reason string `json:"reason,omitempty"`

	// HoldOutgoing holds the account being left out of the pool until
	// AvailableAt, so the next selection — for this session or any other —
	// does not walk straight back into the seat that was just condemned. An
	// operator rotating for a reason of their own (rebalancing, retiring a
	// seat) leaves it false.
	HoldOutgoing bool `json:"hold_outgoing,omitempty"`

	// AvailableAt is when a held outgoing account rejoins the pool. Ignored
	// unless HoldOutgoing is set; the zero time applies the server's default
	// backoff.
	AvailableAt time.Time `json:"available_at,omitempty"`
}

// RotateSessionAccountResponse reports the outcome of a rotation.
type RotateSessionAccountResponse struct {
	// SessionID is the session, unchanged — a rotation never renames one.
	SessionID string `json:"session_id"`

	// PreviousAccountID is the account the session was moved off.
	PreviousAccountID string `json:"previous_account_id,omitempty"`

	// AccountID is the replacement now in force.
	AccountID string `json:"account_id"`

	// Strategy names the policy that selected the replacement.
	Strategy string `json:"strategy,omitempty"`

	// Mode reports which mechanism moved the session: "in_place" swapped the
	// stored credential configuration with no process restart and no replay;
	// "resume" closed the agent process and reconstituted it against the
	// replacement, replaying the transcript.
	Mode string `json:"mode"`
}

// MCPServerConfig declares an MCP server made available to a spawned agent.
// Mirrors orchestration.MCPServerConfig: an HTTP server (such as the QuicBaaS
// MCP endpoint) is described by URL + Env; a stdio server by Command + Args.
// Env carries connection material (e.g. the agent's mTLS cert/key file paths)
// and round-trips losslessly to the spawned agent.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
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
//
// Prompting an existing session is asynchronous: the call returns as soon as
// the server has recorded the turn and dispatched it, long before the agent
// has finished. Such a response has Accepted true, Done false, an empty
// Response, and a TurnID — the turn's output arrives on the session event
// stream (SubscribeSessionEvents), and the turn ends when a turn_completed or
// turn_failed event carrying the same TurnID appears there.
//
// Prompting a backend with no session is a stateless one-shot and stays
// synchronous: Done is true and Response holds the whole turn.
//
// Failures are reported as errors, never as a zero-valued response. Build the
// response with NewPromptSessionAccepted or NewPromptSessionResponse so the
// flattening and completion rules stay in one place.
type PromptSessionResponse struct {
	// SessionID identifies the session that produced the turn.
	SessionID string `json:"session_id"`

	// Response is the concatenated text of the turn's content blocks. Empty
	// for an accepted asynchronous turn, which has produced no output yet.
	Response string `json:"response"`

	// Done reports that the agent's turn completed. Never true for an
	// accepted asynchronous turn; observe its terminal state on the session
	// event stream instead.
	Done bool `json:"done"`

	// Accepted reports that the server took the turn for background
	// execution. Mutually exclusive with Done.
	Accepted bool `json:"accepted,omitempty"`

	// TurnID correlates an accepted turn with its turn_started event and with
	// the turn_completed / turn_failed event that closes it.
	TurnID string `json:"turn_id,omitempty"`

	// Sequence is the session-log sequence of the accepted turn's
	// turn_started event. Pass it as SubscribeSessionEventsRequest.FromSequence
	// to receive the whole turn with no gap and no duplicates.
	Sequence uint64 `json:"sequence,omitempty"`

	// Model is the model that produced the turn, when the backend reports it.
	Model string `json:"model,omitempty"`

	// StopReason is why the agent stopped ("end_turn", "max_tokens", ...).
	// Empty for an accepted turn, which has not stopped yet.
	StopReason string `json:"stop_reason,omitempty"`
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

	// AutoSpawn is true when the task was created with immediate dispatch.
	AutoSpawn bool `json:"auto_spawn,omitempty"`

	// SpawnedJobID is the ID of the auto-spawned job, empty when not spawned.
	SpawnedJobID string `json:"spawned_job_id,omitempty"`

	// SpawnedAt is a nanosecond Unix timestamp of when the job was dispatched.
	SpawnedAt int64 `json:"spawned_at,omitempty"`
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

	// ProjectID scopes the task to a registered project. When non-empty,
	// ClaimTask enforces the project's AgentRoles access matrix against the
	// claiming agent. Empty means the task is unscoped (legacy: allow all).
	ProjectID string `json:"project_id,omitempty"`

	// AutoSpawn, when true, dispatches a one-shot job on the best-matching
	// agent immediately. The task is persisted regardless; if no agent matches
	// the routing constraints, ErrNoMatchingAgent is returned with an empty
	// SpawnedJobID on the returned task.
	AutoSpawn bool `json:"auto_spawn,omitempty"`

	// Backend restricts agent selection during auto-spawn to agents whose
	// Backend field matches this value. Ignored when AutoSpawn is false.
	Backend string `json:"backend,omitempty"`

	// GpuRequired restricts auto-spawn dispatch to agents that advertise
	// "gpu" in their capabilities. Ignored when AutoSpawn is false.
	GpuRequired bool `json:"gpu_required,omitempty"`
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

// ListBackendModelsRequest is a request to list the models available on a
// specific backend, as seen from a specific agent. AgentID is used as a
// host affinity hint by the remote backend resolver; pass empty or "any"
// to skip host affinity. TenantID defaults to "default" on the server when
// omitted; setting it explicitly is required when the resolver dispatches
// to a multi-tenant fleet daemon.
type ListBackendModelsRequest struct {
	AgentID     string `json:"agent_id,omitempty"`
	BackendName string `json:"backend_name"`
	TenantID    string `json:"tenant_id,omitempty"`
}

// ListBackendModelsResponse is the response from listing models for a backend.
// Models is always non-nil; an empty slice means the backend exposes no model
// lister or the agent has no models loaded yet.
type ListBackendModelsResponse struct {
	Backend string   `json:"backend"`
	Models  []string `json:"models"`
	Count   int      `json:"count"`
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

// InferenceRequest is the SDK request for a stateless fleet-routed inference
// call. Maps to POST /api/v1/orchestrator/inference with stream=false or
// stream=true.
type InferenceRequest struct {
	// Backend is the named backend to route to (e.g., "ollama"). Required.
	Backend string `json:"backend"`

	// Model is the model to use. When empty the backend's default is used.
	Model string `json:"model,omitempty"`

	// Prompt is the user prompt text. Required.
	Prompt string `json:"prompt"`

	// SystemPrompt is an optional system message.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// HostAffinity pins the call to a specific agent daemon by Name or Host.
	HostAffinity string `json:"host_affinity,omitempty"`

	// HostAffinityStrict, when true, forces hard host_affinity semantics:
	// the server returns NoMatchingAgentError (HTTP 400) if the named host
	// is offline. Defers to the operator-level
	// orchestration.host_affinity.mode default when false.
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`

	// Metadata is an opaque string map forwarded to the backend unchanged.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// InferenceContentBlock is one content fragment from a model response.
type InferenceContentBlock struct {
	// Type is the content type ("text", "tool_use", etc.).
	Type string `json:"type"`

	// Text holds the text content when Type is "text".
	Text string `json:"text,omitempty"`
}

// InferenceResponse is the aggregated response from a non-streaming
// Inference call.
type InferenceResponse struct {
	// Content holds all content blocks from the response.
	Content []InferenceContentBlock `json:"content"`

	// Model is the model that produced the response.
	Model string `json:"model,omitempty"`

	// StopReason is the reason the model stopped generating
	// (e.g., "end_turn", "max_tokens").
	StopReason string `json:"stop_reason,omitempty"`
}

// InferenceChunk is one delta frame from a streaming InferenceStream call.
type InferenceChunk struct {
	// Delta is the content emitted in this chunk.
	Delta []InferenceContentBlock `json:"delta"`

	// Model is set on the final chunk.
	Model string `json:"model,omitempty"`

	// StopReason is set on the final chunk.
	StopReason string `json:"stop_reason,omitempty"`

	// Final is true on the last chunk.
	Final bool `json:"final,omitempty"`
}

// =============================================================================
// Workflow Management Types
// =============================================================================

// WorkflowMode mirrors orchestration.WorkflowMode for SDK callers.
type WorkflowMode string

const (
	// WorkflowModeCanned runs a fully predetermined workflow with fixed steps.
	WorkflowModeCanned WorkflowMode = "canned"

	// WorkflowModeConstrained allows the lead agent to decompose tasks but within
	// defined agent roles and capabilities from the workflow definition.
	WorkflowModeConstrained WorkflowMode = "constrained"

	// WorkflowModeAutonomous gives the lead agent full control over task
	// decomposition, agent spawning, and workflow execution.
	WorkflowModeAutonomous WorkflowMode = "autonomous"
)

// AgentRoleSpec describes a single agent role within a workflow definition.
type AgentRoleSpec struct {
	Role         string            `json:"role"`
	Backend      string            `json:"backend"`
	Model        string            `json:"model,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
}

// WorkflowSettings holds operational tuning parameters for a workflow.
type WorkflowSettings struct {
	MaxIterations int    `json:"max_iterations,omitempty"`
	Topology      string `json:"topology,omitempty"`
	Isolation     string `json:"isolation,omitempty"`
	PeerMessaging bool   `json:"peer_messaging,omitempty"`
	FileLocking   bool   `json:"file_locking,omitempty"`
	VectorDB      bool   `json:"vector_db,omitempty"`
	Verbosity     string `json:"verbosity,omitempty"`
	GlobalNumCtx  int    `json:"global_num_ctx,omitempty"`
}

// WorkflowDefinition describes a complete orchestration workflow including
// agent roles, capabilities, and operational settings.
type WorkflowDefinition struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Mode        WorkflowMode     `json:"mode"`
	Agents      []AgentRoleSpec  `json:"agents"`
	Settings    WorkflowSettings `json:"settings,omitempty"`
}

// CreateWorkflowRequest is a request to register a new workflow definition.
type CreateWorkflowRequest struct {
	TenantID   string              `json:"tenant_id"`
	Definition *WorkflowDefinition `json:"definition"`
}

// ListWorkflowsRequest is a request to list workflow definitions.
type ListWorkflowsRequest struct {
	TenantID string `json:"tenant_id"`
}

// ListWorkflowsResponse is the response from listing workflow definitions.
type ListWorkflowsResponse struct {
	Workflows []*WorkflowDefinition `json:"workflows"`
	Count     int                   `json:"count"`
}

// GetWorkflowRequest is a request to get a workflow definition by name.
type GetWorkflowRequest struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
}

// UpdateWorkflowRequest is a request to replace an existing workflow definition.
type UpdateWorkflowRequest struct {
	TenantID   string              `json:"tenant_id"`
	Name       string              `json:"name"`
	Definition *WorkflowDefinition `json:"definition"`
}

// DeleteWorkflowRequest is a request to remove a workflow definition by name.
type DeleteWorkflowRequest struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
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

// RotateSessionAccount moves a session onto a different pooled provider account.
func (c *client) RotateSessionAccount(ctx context.Context, req *RotateSessionAccountRequest) (*RotateSessionAccountResponse, error) {
	if err := validateRotateSessionAccountRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.RotateSessionAccount", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RotateSessionAccount", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.RotateSessionAccount", "nil response from server")
	}

	result, ok := resp.(*RotateSessionAccountResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.RotateSessionAccount", "invalid response type")
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

// ListBackendModels returns the model names available on the named backend
// as seen from the specified agent. The transport layer translates this into
// a REST GET against /api/v1/orchestrator/agents/{agent_id}/backends/{backend_name}/models
// or the equivalent gRPC RPC; both paths return an empty slice (not an error)
// when the agent is unknown or the backend exposes no model lister.
func (c *client) ListBackendModels(ctx context.Context, req *ListBackendModelsRequest) (*ListBackendModelsResponse, error) {
	if err := validateListBackendModelsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListBackendModels", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListBackendModels", req)
	if err != nil {
		return nil, err
	}

	// A nil response is treated as "no models" rather than an error so callers
	// can render an empty list without special-casing missing agents/backends.
	if resp == nil {
		return &ListBackendModelsResponse{Backend: req.BackendName, Models: []string{}, Count: 0}, nil
	}

	result, ok := resp.(*ListBackendModelsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListBackendModels", "invalid response type")
	}

	if result.Models == nil {
		result.Models = []string{}
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

// CreateWorkflow registers a new named workflow definition.
func (c *client) CreateWorkflow(ctx context.Context, req *CreateWorkflowRequest) (*WorkflowDefinition, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "tenant ID is required")
	}
	if req.Definition == nil {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "definition is required")
	}
	if req.Definition.Name == "" {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "workflow name is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateWorkflow", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CreateWorkflow", "nil response from server")
	}
	result, ok := resp.(*WorkflowDefinition)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CreateWorkflow", "invalid response type")
	}
	return result, nil
}

// ListWorkflows returns all registered workflow definitions for a tenant.
func (c *client) ListWorkflows(ctx context.Context, req *ListWorkflowsRequest) ([]*WorkflowDefinition, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListWorkflows", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListWorkflows", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.ListWorkflows", "tenant ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListWorkflows", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return []*WorkflowDefinition{}, nil
	}
	result, ok := resp.(*ListWorkflowsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListWorkflows", "invalid response type")
	}
	return result.Workflows, nil
}

// GetWorkflow returns a single workflow definition by name.
func (c *client) GetWorkflow(ctx context.Context, req *GetWorkflowRequest) (*WorkflowDefinition, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetWorkflow", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetWorkflow", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetWorkflow", "tenant ID is required")
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetWorkflow", "workflow name is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetWorkflow", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetWorkflow", "nil response from server")
	}
	result, ok := resp.(*WorkflowDefinition)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetWorkflow", "invalid response type")
	}
	return result, nil
}

// UpdateWorkflow replaces an existing workflow definition by name.
func (c *client) UpdateWorkflow(ctx context.Context, req *UpdateWorkflowRequest) (*WorkflowDefinition, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "tenant ID is required")
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "workflow name is required")
	}
	if req.Definition == nil {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "definition is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpdateWorkflow", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.UpdateWorkflow", "nil response from server")
	}
	result, ok := resp.(*WorkflowDefinition)
	if !ok {
		return nil, errors.NewInternal("orchestrator.UpdateWorkflow", "invalid response type")
	}
	return result, nil
}

// DeleteWorkflow removes a workflow definition by name.
func (c *client) DeleteWorkflow(ctx context.Context, req *DeleteWorkflowRequest) error {
	if ctx == nil {
		return errors.NewInvalidInput("orchestrator.DeleteWorkflow", "context is required")
	}
	if req == nil {
		return errors.NewInvalidInput("orchestrator.DeleteWorkflow", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.DeleteWorkflow", "tenant ID is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("orchestrator.DeleteWorkflow", "workflow name is required")
	}

	_, err := c.transport.Request(ctx, serviceName, "DeleteWorkflow", req)
	return err
}

// DisableAgent administratively disables an agent globally.
func (c *client) DisableAgent(ctx context.Context, req *DisableAgentRequest) (*DisableAgentResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.DisableAgent", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.DisableAgent", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.DisableAgent", "tenant ID is required")
	}
	if req.AgentID == "" {
		return nil, errors.NewInvalidInput("orchestrator.DisableAgent", "agent ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DisableAgent", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.DisableAgent", "nil response from server")
	}

	result, ok := resp.(*DisableAgentResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.DisableAgent", "invalid response type")
	}
	return result, nil
}

// EnableAgent re-activates a previously disabled agent.
func (c *client) EnableAgent(ctx context.Context, req *EnableAgentRequest) (*EnableAgentResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.EnableAgent", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.EnableAgent", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.EnableAgent", "tenant ID is required")
	}
	if req.AgentID == "" {
		return nil, errors.NewInvalidInput("orchestrator.EnableAgent", "agent ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "EnableAgent", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.EnableAgent", "nil response from server")
	}

	result, ok := resp.(*EnableAgentResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.EnableAgent", "invalid response type")
	}
	return result, nil
}

// SubmitTaskArtifacts stores typed output artifacts for a task.
func (c *client) SubmitTaskArtifacts(ctx context.Context, req *SubmitTaskArtifactsRequest) (*SubmitTaskArtifactsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SubmitTaskArtifacts", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.SubmitTaskArtifacts", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.SubmitTaskArtifacts", "tenant ID is required")
	}
	if req.TaskID == "" {
		return nil, errors.NewInvalidInput("orchestrator.SubmitTaskArtifacts", "task ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SubmitTaskArtifacts", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.SubmitTaskArtifacts", "nil response from server")
	}

	result, ok := resp.(*SubmitTaskArtifactsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.SubmitTaskArtifacts", "invalid response type")
	}
	return result, nil
}

// GetTaskArtifact retrieves a named artifact from a task.
func (c *client) GetTaskArtifact(ctx context.Context, req *GetTaskArtifactRequest) (*TaskArtifact, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "tenant ID is required")
	}
	if req.TaskID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "task ID is required")
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "artifact name is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetTaskArtifact", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetTaskArtifact", "nil response from server")
	}

	result, ok := resp.(*TaskArtifact)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetTaskArtifact", "invalid response type")
	}
	return result, nil
}

// ListTaskArtifacts returns all output artifact metadata for a task.
func (c *client) ListTaskArtifacts(ctx context.Context, req *ListTaskArtifactsRequest) (*ListTaskArtifactsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListTaskArtifacts", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListTaskArtifacts", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.ListTaskArtifacts", "tenant ID is required")
	}
	if req.TaskID == "" {
		return nil, errors.NewInvalidInput("orchestrator.ListTaskArtifacts", "task ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListTaskArtifacts", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &ListTaskArtifactsResponse{TaskID: req.TaskID, Artifacts: []*TaskArtifact{}, Count: 0}, nil
	}

	result, ok := resp.(*ListTaskArtifactsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListTaskArtifacts", "invalid response type")
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

func validateRotateSessionAccountRequest(req *RotateSessionAccountRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.RotateSessionAccount", "request is required")
	}
	if req.SessionID == "" {
		return errors.NewInvalidInput("orchestrator.RotateSessionAccount", "session ID is required")
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

func validateListBackendModelsRequest(req *ListBackendModelsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ListBackendModels", "request is required")
	}
	if req.BackendName == "" {
		return errors.NewInvalidInput("orchestrator.ListBackendModels", "backend name is required")
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
func (c *client) SpawnChildJob(ctx context.Context, req *SpawnChildJobRequest) (*SpawnChildJobResponse, error) {
	if err := validateSpawnChildJobRequest(req); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SpawnChildJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SpawnChildJob", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.SpawnChildJob", "nil response from server")
	}
	result, ok := resp.(*SpawnChildJobResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.SpawnChildJob", "invalid response type")
	}
	return result, nil
}

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

// Inference runs a stateless fleet-routed inference call against the named
// backend. Waits for the full aggregated response before returning. For
// interactive streaming use InferenceStream.
func (c *client) Inference(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.Inference", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.Inference", "request is required")
	}
	if req.Backend == "" {
		return nil, errors.NewInvalidInput("orchestrator.Inference", "backend is required")
	}
	if req.Prompt == "" {
		return nil, errors.NewInvalidInput("orchestrator.Inference", "prompt is required")
	}

	// The transport handler decorates with stream=false and POSTs to
	// /api/v1/orchestrator/inference.
	resp, err := c.transport.Request(ctx, serviceName, "Inference", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.Inference", "nil response from server")
	}
	result, ok := resp.(*InferenceResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.Inference", "invalid response type")
	}
	return result, nil
}

// InferenceStream runs a stateless fleet-routed inference call and returns a
// channel of InferenceChunk values as the backend streams them. The channel
// is closed when the backend finishes or the context is cancelled.
func (c *client) InferenceStream(ctx context.Context, req *InferenceRequest) (<-chan *InferenceChunk, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.InferenceStream", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.InferenceStream", "request is required")
	}
	if req.Backend == "" {
		return nil, errors.NewInvalidInput("orchestrator.InferenceStream", "backend is required")
	}
	if req.Prompt == "" {
		return nil, errors.NewInvalidInput("orchestrator.InferenceStream", "prompt is required")
	}

	stream, err := c.transport.RequestStream(ctx, serviceName, "InferenceStream", req)
	if err != nil {
		return nil, err
	}

	sub := &inferenceStreamSubscription{
		stream: stream,
		chunks: make(chan *InferenceChunk, 64),
	}
	go sub.receiveLoop(ctx)
	return sub.chunks, nil
}

// inferenceStreamSubscription manages the lifecycle of an inference stream.
type inferenceStreamSubscription struct {
	stream transport.Stream
	chunks chan *InferenceChunk
	closed atomic.Bool
}

// receiveLoop reads chunks from the transport stream and forwards them to the
// channel. Terminates when the stream closes, errors, or the context is done.
func (s *inferenceStreamSubscription) receiveLoop(ctx context.Context) {
	defer func() {
		close(s.chunks)
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
			chunk, ok := msg.(*InferenceChunk)
			if !ok {
				continue
			}
			select {
			case s.chunks <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}
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

// =============================================================================
// Session Event Subscription + History (Phase 4)
// =============================================================================

// GetSessionHistory implements Client.GetSessionHistory. One-shot REST/gRPC
// call for a page of historical session events.
func (c *client) GetSessionHistory(ctx context.Context, req *GetSessionHistoryRequest) (*GetSessionHistoryResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetSessionHistory", "context is required")
	}
	if req == nil || req.SessionID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetSessionHistory", "session_id is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "GetSessionHistory", req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*GetSessionHistoryResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetSessionHistory", "invalid response type")
	}
	return out, nil
}

// SubscribeSessionEvents implements Client.SubscribeSessionEvents. Opens a
// streaming subscription (gRPC stream or SSE depending on transport) and
// returns a buffered channel that receives each event in order.
func (c *client) SubscribeSessionEvents(ctx context.Context, req *SubscribeSessionEventsRequest) (<-chan *SessionEventEnvelope, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SubscribeSessionEvents", "context is required")
	}
	if req == nil || req.SessionID == "" {
		return nil, errors.NewInvalidInput("orchestrator.SubscribeSessionEvents", "session_id is required")
	}
	stream, err := c.transport.RequestStream(ctx, serviceName, "SubscribeSessionEvents", req)
	if err != nil {
		return nil, err
	}
	sub := &sessionEventSubscription{
		stream: stream,
		events: make(chan *SessionEventEnvelope, 64),
	}
	go sub.receiveLoop(ctx)
	return sub.events, nil
}

// sessionEventSubscription wraps a transport.Stream and forwards each
// received SessionEventEnvelope to its buffered channel. The channel is
// closed when the stream errors or the context is cancelled; consumers
// detect end-of-stream via the closed-channel signal.
type sessionEventSubscription struct {
	stream transport.Stream
	events chan *SessionEventEnvelope
	closed atomic.Bool
}

func (s *sessionEventSubscription) receiveLoop(ctx context.Context) {
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
		}
		msg, err := s.stream.Recv()
		if err != nil {
			return
		}
		env, ok := msg.(*SessionEventEnvelope)
		if !ok {
			continue
		}
		select {
		case s.events <- env:
		case <-ctx.Done():
			return
		}
	}
}

// Validation functions for job operations.

func validateSpawnChildJobRequest(req *SpawnChildJobRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.SpawnChildJob", "request is required")
	}
	if req.ParentJobID == "" {
		return errors.NewInvalidInput("orchestrator.SpawnChildJob", "parent_job_id is required")
	}
	return nil
}

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
	TenantID    string `json:"tenant_id"`
	Description string `json:"description"`
	LeadBackend string `json:"lead_backend,omitempty"`
	LeadModel   string `json:"lead_model,omitempty"`
	// LeadAgentID pins the job's lead agent to a specific registered agent.
	// Empty means the orchestrator selects a lead based on backend/model and
	// the project's DefaultLeadAgent.
	LeadAgentID   string `json:"lead_agent_id,omitempty"`
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

// TaskSpec is an inline task definition supplied when spawning a child job.
type TaskSpec struct {
	Title                string   `json:"title,omitempty"`
	Description          string   `json:"description,omitempty"`
	Dependencies         []string `json:"dependencies,omitempty"`
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
}

// SpawnChildJobRequest fans out a child job from the calling lead agent.
type SpawnChildJobRequest struct {
	// ParentJobID is the job whose lead agent is initiating the spawn. Required.
	ParentJobID string `json:"parent_job_id"`
	// CallerAgentID is the fleet agent ID of the calling agent. Required.
	CallerAgentID string `json:"caller_agent_id,omitempty"`
	// WorkflowID optionally names an existing workflow definition.
	WorkflowID string `json:"workflow_id,omitempty"`
	// ProjectID scopes the child job to a registered project.
	ProjectID string `json:"project_id,omitempty"`
	// Tasks provides inline task specs when no workflow is referenced.
	Tasks []*TaskSpec `json:"tasks,omitempty"`
	// HostAffinity pins the child to a specific agent daemon.
	HostAffinity string `json:"host_affinity,omitempty"`
	// GpuRequired restricts dispatch to GPU-capable daemons.
	GpuRequired bool `json:"gpu_required,omitempty"`
	// HostAffinityStrict treats HostAffinity as a hard constraint.
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`
}

// SpawnChildJobResponse is returned after the child job has been accepted.
type SpawnChildJobResponse struct {
	ChildJobID string `json:"child_job_id"`
}

// =============================================================================
// Agent Admin Types
// =============================================================================

// DisableAgentRequest is a request to administratively disable an agent.
type DisableAgentRequest struct {
	TenantID string `json:"tenant_id"`
	AgentID  string `json:"agent_id"`
}

// DisableAgentResponse is returned after an agent has been disabled.
type DisableAgentResponse struct {
	AgentID    string    `json:"agent_id"`
	Disabled   bool      `json:"disabled"`
	DisabledAt time.Time `json:"disabled_at"`
	Timestamp  time.Time `json:"timestamp"`
}

// EnableAgentRequest is a request to re-enable a previously disabled agent.
type EnableAgentRequest struct {
	TenantID string `json:"tenant_id"`
	AgentID  string `json:"agent_id"`
}

// EnableAgentResponse is returned after an agent has been re-enabled.
type EnableAgentResponse struct {
	AgentID   string    `json:"agent_id"`
	Enabled   bool      `json:"enabled"`
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Task Artifact Types
// =============================================================================

// TaskArtifact represents a typed output artifact produced by a task.
type TaskArtifact struct {
	// Name is the unique artifact name within the task.
	Name string `json:"name"`
	// ContentType is the MIME type of the artifact (e.g., "text/plain").
	ContentType string `json:"content_type,omitempty"`
	// Data is the inline artifact payload for small artifacts.
	Data []byte `json:"data,omitempty"`
	// Ref is the KV store reference for large artifacts (mutually exclusive with Data).
	Ref string `json:"ref,omitempty"`
	// Size is the total byte length of the artifact.
	Size int64 `json:"size,omitempty"`
}

// SubmitTaskArtifactsRequest stores typed artifacts for a task.
type SubmitTaskArtifactsRequest struct {
	TenantID  string          `json:"tenant_id"`
	TaskID    string          `json:"task_id"`
	Artifacts []*TaskArtifact `json:"artifacts"`
}

// SubmitTaskArtifactsResponse confirms how many artifacts were stored.
type SubmitTaskArtifactsResponse struct {
	TaskID string `json:"task_id"`
	Stored int    `json:"stored"`
}

// GetTaskArtifactRequest retrieves a named artifact from a task.
type GetTaskArtifactRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`
	Name     string `json:"name"`
}

// ListTaskArtifactsRequest lists all artifacts for a task.
type ListTaskArtifactsRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`
}

// ListTaskArtifactsResponse carries artifact metadata (no inline Data resolved).
type ListTaskArtifactsResponse struct {
	TaskID    string          `json:"task_id"`
	Artifacts []*TaskArtifact `json:"artifacts"`
	Count     int             `json:"count"`
}
