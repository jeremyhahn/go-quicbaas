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

package orchestration

import (
	"context"
	"errors"
	"slices"
	"time"
)

// =============================================================================
// Agent Service - Agent Orchestration Operations
// =============================================================================

// AgentService handles agent orchestration operations.
// This service provides operations for discovering, registering, and managing
// AI agent projects and sessions across multiple backends.
type AgentService interface {
	// DiscoverProjects walks a directory tree to find agent-enabled projects.
	// Requires permission: agent:discover
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Discover request with root path to scan
	// Returns:
	//   - DiscoverProjectsResponse: Contains discovered projects and timestamp
	//   - error: ErrRootPathRequired, ErrAgentServiceUnavailable, ErrUnauthorized
	DiscoverProjects(ctx context.Context, req *DiscoverProjectsRequest) (*DiscoverProjectsResponse, error)

	// RegisterProject registers a project for agent orchestration.
	// Requires permission: agent:register
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Register request with project name, path, and configuration
	// Returns:
	//   - RegisterProjectResponse: Contains generated project ID and timestamp
	//   - error: ErrProjectNameRequired, ErrProjectPathRequired, ErrUnauthorized
	RegisterProject(ctx context.Context, req *RegisterProjectRequest) (*RegisterProjectResponse, error)

	// ListProjects returns all registered projects.
	// Requires permission: agent:discover
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List request with optional pagination
	// Returns:
	//   - ListProjectsResponse: Contains registered projects and pagination info
	//   - error: ErrAgentServiceUnavailable, ErrUnauthorized
	ListProjects(ctx context.Context, req *ListProjectsRequest) (*ListProjectsResponse, error)

	// SpawnSession creates a persistent agent session on a backend.
	// Requires permission: agent:spawn
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Spawn request with backend, project, and session configuration
	// Returns:
	//   - SpawnSessionResponse: Contains session ID, backend, and timestamp
	//   - error: ErrBackendRequired, ErrProjectNotFound, ErrUnauthorized
	SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error)

	// PromptSession sends a prompt to an agent session or backend.
	// Requires permission: agent:prompt
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Prompt request with prompt text and routing information
	// Returns:
	//   - PromptSessionResponse: Contains agent response content and metadata
	//   - error: ErrPromptRequired, ErrBackendRequired, ErrUnauthorized
	PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error)

	// PromptSessionAsync accepts a prompt for an existing agent session,
	// records it in the durable session event log, dispatches the turn on a
	// background context, and returns immediately with an acknowledgement.
	//
	// This is the operator-facing turn contract. Real agent turns run for
	// minutes, which no request/response transport can hold open (REST caps a
	// request at 25s), so the acknowledgement is the whole synchronous answer:
	// the turn's output — assistant chunks, tool calls, and the terminal
	// turn_completed / turn_failed event — is delivered exclusively through
	// the SessionEventLog.
	//
	// Validation (tenant, prompt text, session, backend resolution) happens
	// before the dispatch, so a bad request still fails synchronously with a
	// typed error instead of an acknowledgement. A session ID is required:
	// without one there is no durable stream to publish the turn on.
	//
	// The dispatch runs on a context detached from the caller's, so a client
	// that disconnects — or a transport whose request deadline expires — never
	// cancels the agent.
	PromptSessionAsync(ctx context.Context, req *PromptSessionRequest) (*PromptTurnAck, error)

	// SessionStatus returns the status of an active session.
	// Requires permission: agent:prompt (read access)
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Status request with session ID
	// Returns:
	//   - SessionStatusResponse: Contains session state and activity timestamps
	//   - error: ErrSessionIDRequired, ErrUnauthorized
	SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error)

	// CloseSession terminates an active session.
	// Requires permission: agent:close
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Close request with session ID
	// Returns:
	//   - CloseSessionResponse: Contains session ID and timestamp
	//   - error: ErrSessionIDRequired, ErrUnauthorized
	CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error)

	// ListSessions returns every session, in any state (closed and error
	// included), optionally filtered by backend or status.
	// Requires permission: agent:prompt (read access)
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List request with optional backend filter
	// Returns:
	//   - ListSessionsResponse: Contains session list and timestamp
	//   - error: ErrAgentServiceUnavailable, ErrUnauthorized
	ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error)

	// Infer runs a stateless fleet-routed inference call against a named
	// backend without requiring a prior session Spawn. Intended for backends
	// like Ollama where each prompt is self-contained. The returned channel
	// streams InferChunk values; the channel is closed when the backend
	// finishes or an error occurs. Use AccumulateChunks to get a single
	// aggregated response.
	// Requires permission: agent:prompt
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Inference request with backend, model, prompt, and routing hints
	// Returns:
	//   - <-chan *InferChunk: stream of response fragments
	//   - error: ErrBackendRequired, ErrPromptRequired, ErrInferUnsupported, ErrUnauthorized
	Infer(ctx context.Context, req *InferRequest) (<-chan *InferChunk, error)

	// DecomposeTask uses an agent to decompose a task into work items.
	// Requires permission: agent:decompose
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Decompose request with task description and backend
	// Returns:
	//   - DecomposeTaskResponse: Contains work items and timestamp
	//   - error: ErrBackendRequired, ErrTaskRequired, ErrUnauthorized
	DecomposeTask(ctx context.Context, req *DecomposeTaskRequest) (*DecomposeTaskResponse, error)

	// ListBackends returns all registered agent backends.
	// Requires permission: agent:discover (read access)
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	// Returns:
	//   - []*AgentBackendInfo: Contains backend name, type, and capabilities
	//   - error: ErrAgentServiceUnavailable, ErrUnauthorized
	ListBackends(ctx context.Context) ([]*AgentBackendInfo, error)

	// ListBackendModels returns the model names available on the named backend.
	// For Ollama it queries /api/tags on the agent's local instance; for ACP
	// backends it returns a curated catalog. The agent_id parameter is optional;
	// when non-empty and host affinity routing is active, the call is forwarded
	// to the matching remote daemon. Returns ErrBackendNotFound when the backend
	// is unknown.
	ListBackendModels(ctx context.Context, backendName string) ([]string, error)

	// BackendToolGuidance returns backend-specific tool usage instructions
	// for the named backend. Returns empty string if the backend has no
	// special requirements or is not found.
	BackendToolGuidance(backendName string) string

	// RegisterBackend adds a new agent backend to the service.
	// Requires permission: agent:admin
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Register request with backend name and implementation
	// Returns:
	//   - error: ErrBackendRequired, ErrUnauthorized
	RegisterBackend(ctx context.Context, req *RegisterBackendRequest) error

	// RegisterAPIInferenceBackend adds an API-based frontier-model inference
	// backend (Anthropic, OpenAI, xAI, ...) to the service. The backend is
	// adapted into the AgentBackend interface internally so it routes through
	// the same dispatch path as ACP and model-server backends.
	// Requires permission: agent:admin
	RegisterAPIInferenceBackend(ctx context.Context, req *RegisterAPIInferenceBackendRequest) error

	// HealthCheck performs a health check on all active agent sessions.
	// Requires permission: agent:admin
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	// Returns:
	//   - HealthCheckReport: Contains session health results and summary counts
	//   - error: ErrAgentServiceUnavailable, ErrUnauthorized
	HealthCheck(ctx context.Context) (*HealthCheckReport, error)

	// RegisterFleetNode registers an agent with the fleet registry.
	// Requires permission: agent:admin
	RegisterFleetNode(ctx context.Context, req *RegisterFleetNodeRequest) (*RegisterFleetNodeResponse, error)

	// GetFleetNode retrieves a registered agent by ID.
	// Requires permission: agent:read
	GetFleetNode(ctx context.Context, tenantID, agentID string) (*FleetNode, error)

	// ListFleetNodes lists all registered agents for a tenant.
	// Requires permission: agent:read
	ListFleetNodes(ctx context.Context, tenantID string, filter *FleetNodeFilter) ([]*FleetNode, error)

	// ListFleetNodesPage returns ONE PAGE of the tenant's matching agents,
	// searched and ordered per FleetNodePageSpec.
	// Requires permission: agent:read
	ListFleetNodesPage(ctx context.Context, tenantID string, filter *FleetNodeFilter, page *Page) (*PageResult[*FleetNode], error)

	// RemoveFleetNode removes an agent from the fleet registry.
	// Requires permission: agent:admin
	RemoveFleetNode(ctx context.Context, tenantID, agentID string) error

	// FleetNodeHeartbeat updates an agent's last seen timestamp and reconciles
	// the agent's backend axis against observed, what the daemon reports it
	// is serving right now. A nil observation reconciles nothing.
	FleetNodeHeartbeat(ctx context.Context, tenantID, agentID string, observed *FleetNodeObservation) error

	// CreateTask creates a new orchestration task.
	// Requires permission: agent:task
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - req: Task creation request
	// Returns:
	//   - *Task: The created task with generated ID and timestamps
	//   - error: ErrTaskStoreInvalidInput, ErrAgentServiceUnavailable
	CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error)

	// GetTask retrieves a task by ID within a tenant namespace.
	// Requires permission: agent:task
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - tenantID: The tenant namespace
	//   - taskID: The task identifier
	// Returns:
	//   - *Task: The task if found
	//   - error: ErrTaskNotFound, ErrTaskStoreInvalidInput
	GetTask(ctx context.Context, tenantID, taskID string) (*Task, error)

	// ListTasks lists tasks matching the filter within a tenant.
	// Requires permission: agent:task
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - tenantID: The tenant namespace
	//   - filter: Optional filter criteria
	// Returns:
	//   - []*Task: Matching tasks
	//   - error: ErrTaskStoreInvalidInput, ErrAgentServiceUnavailable
	ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error)

	// ListTasksPage returns ONE PAGE of the tenant's matching tasks,
	// searched and ordered per TaskPageSpec. The page's Total counts the
	// records the filter and search left, so a caller knows how much it has
	// not been sent.
	// Requires permission: agent:task
	ListTasksPage(ctx context.Context, tenantID string, filter *TaskFilter, page *Page) (*PageResult[*Task], error)

	// ClaimTask atomically claims a task for an agent.
	// Requires permission: agent:task
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - tenantID: The tenant namespace
	//   - taskID: The task identifier
	//   - agentID: The agent claiming the task
	// Returns:
	//   - error: ErrTaskNotFound, ErrTaskAlreadyClaimed, ErrTaskInvalidState
	ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error

	// CompleteTask marks a task as completed with a result.
	// Requires permission: agent:task
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - tenantID: The tenant namespace
	//   - taskID: The task identifier
	//   - result: The completion result
	// Returns:
	//   - error: ErrTaskNotFound, ErrTaskInvalidState
	CompleteTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error

	// BlockTask parks a pending, claimed or in-progress task with a kind and
	// a reason so it is not dispatched until a person readmits it.
	// Requires permission: orchestrator:task_block
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - tenantID: The tenant namespace
	//   - taskID: The task identifier
	//   - kind: Why the task is parked (needs_info, blocked, escalated)
	//   - reason: Human-readable reason, surfaced to the person it waits for
	// Returns:
	//   - error: ErrTaskNotFound, ErrTaskInvalidState, ErrTaskStoreInvalidInput
	BlockTask(ctx context.Context, tenantID, taskID string, kind TaskBlockKind, reason string) error

	// DeleteTask removes a task, its artifact payloads, its claim mark and
	// its source reservation, so the task's source can be adopted again.
	// Requires permission: orchestrator:task_create — deleting a task record
	// is the inverse of writing one, and the vocabulary go-qrdb owns has no
	// separate delete verb for the family.
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - tenantID: The tenant namespace
	//   - taskID: The task identifier
	// Returns:
	//   - error: ErrTaskNotFound, ErrTaskLeased, ErrTaskStoreInvalidInput
	DeleteTask(ctx context.Context, tenantID, taskID string) error

	// ReadmitTask returns a blocked task to the pending queue.
	// Requires permission: orchestrator:task_readmit
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - tenantID: The tenant namespace
	//   - taskID: The task identifier
	// Returns:
	//   - error: ErrTaskNotFound, ErrTaskInvalidState
	ReadmitTask(ctx context.Context, tenantID, taskID string) error

	// CloseBlockedTask completes a blocked task without readmitting it. It is
	// the verb behind the attention inbox's "close as done" action: the owner
	// has ruled that no further work is needed.
	// Requires permission: orchestrator:task_readmit
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - tenantID: The tenant namespace
	//   - taskID: The task identifier
	//   - result: The operator's closing note, recorded as the task result
	// Returns:
	//   - error: ErrTaskNotFound, ErrTaskInvalidState
	CloseBlockedTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error

	// BumpTaskReviewRoundBudget raises a blocked task's review-round budget
	// by one extra round (Task.ReviewRoundBonus), bounded by
	// MaxReviewRoundBonus, and returns the resulting bonus. It is the verb
	// behind the attention inbox's "allow another round" action, called
	// before ReadmitTask so the runner sees the raised budget the moment the
	// task is dispatched again.
	// Requires permission: orchestrator:task_readmit
	// Parameters:
	//   - ctx: Request context with timeout and tenant metadata
	//   - tenantID: The tenant namespace
	//   - taskID: The task identifier
	// Returns:
	//   - int: The task's ReviewRoundBonus after the increment
	//   - error: ErrTaskNotFound, ErrTaskInvalidState, ReviewRoundBudgetExhaustedError
	BumpTaskReviewRoundBudget(ctx context.Context, tenantID, taskID string) (int, error)

	// StartJob starts an autonomous orchestration control loop.
	// Requires permission: agent:job
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Job start request with description and configuration
	// Returns:
	//   - *StartJobResponse: Contains job ID and timestamp
	//   - error: ErrJobAlreadyRunning, ErrTenantIDRequired, ErrAgentServiceUnavailable
	StartJob(ctx context.Context, req *StartJobRequest) (*StartJobResponse, error)

	// GetJobStatus returns the current status of an orchestration job.
	// Requires permission: agent:job
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - tenantID: The tenant namespace
	//   - jobID: The job identifier
	// Returns:
	//   - *JobStatus: Current job state and progress
	//   - error: ErrJobNotFound, ErrTenantIDRequired
	GetJobStatus(ctx context.Context, tenantID, jobID string) (*JobStatus, error)

	// ListJobs returns the tenant's jobs matching filter, sorted by start
	// time descending.
	// Requires permission: agent:job
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - tenantID: The tenant namespace
	//   - filter: Narrows the listing; nil lists every job
	// Returns:
	//   - []*JobStatus: The tenant's matching jobs
	//   - error: ErrTenantIDRequired
	ListJobs(ctx context.Context, tenantID string, filter *JobFilter) ([]*JobStatus, error)

	// ListJobsPage returns ONE PAGE of the tenant's matching jobs, searched
	// and ordered per JobPageSpec.
	//
	// IT EXISTS BECAUSE ListJobs CANNOT ANSWER "HOW MANY". A slice return
	// can be truncated but not counted: the caller would be handed 25 jobs
	// with no way to tell whether 25 or 2,000 matched. The envelope carries
	// the count with the page.
	// Requires permission: agent:job
	ListJobsPage(ctx context.Context, tenantID string, filter *JobFilter, page *Page) (*PageResult[*JobStatus], error)

	// CancelJob cancels a running orchestration job.
	// Requires permission: agent:job
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - tenantID: The tenant namespace
	//   - jobID: The job identifier
	// Returns:
	//   - error: ErrJobNotFound, ErrTenantIDRequired
	CancelJob(ctx context.Context, tenantID, jobID string) error

	// SubscribeJobEvents returns a channel that receives real-time events for
	// a running orchestration job. The channel is closed when the job completes
	// or the context is cancelled.
	// Requires permission: agent:job
	// Parameters:
	//   - ctx: Request context; cancelling it unsubscribes
	//   - tenantID: The tenant namespace
	//   - jobID: The job identifier
	// Returns:
	//   - <-chan *JobEvent: Read-only channel of job events
	//   - error: ErrJobNotFound, ErrTenantIDRequired
	SubscribeJobEvents(ctx context.Context, tenantID, jobID string) (<-chan *JobEvent, error)

	// SpawnChildJob spawns a child job on behalf of a lead agent. The calling
	// agent MUST be the registered lead of the parent job; any other caller
	// receives NotLeadOfParentError. The child inherits the parent's tenant.
	// Requires permission: agent:job
	SpawnChildJob(ctx context.Context, req *SpawnChildJobRequest) (*SpawnChildJobResponse, error)

	// SubmitTaskArtifacts appends typed artifacts to a task's output.
	// Artifacts exceeding InlineLimit are offloaded to the KV store.
	// Requires permission: agent:task
	SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []Artifact) error

	// GetTaskArtifact retrieves a named artifact, resolving KV refs to bytes.
	// Requires permission: agent:task
	GetTaskArtifact(ctx context.Context, tenantID, taskID, name string) (*Artifact, error)

	// ListTaskArtifacts returns all output artifact metadata for a task.
	// Requires permission: agent:task
	ListTaskArtifacts(ctx context.Context, tenantID, taskID string) ([]Artifact, error)

	// ListWorkflows returns all registered workflow definitions, sorted by name.
	// Requires permission: agent:discover
	ListWorkflows(ctx context.Context) ([]*WorkflowDefinition, error)

	// ListWorkflowsPage returns ONE PAGE of the context tenant's workflow
	// definitions, searched and ordered per WorkflowPageSpec.
	// Requires permission: agent:discover
	ListWorkflowsPage(ctx context.Context, filter *WorkflowFilter, page *Page) (*PageResult[*WorkflowDefinition], error)

	// GetWorkflow returns a workflow definition by name.
	// Requires permission: agent:discover
	// Returns ErrWorkflowNotFound if the workflow does not exist.
	GetWorkflow(ctx context.Context, name string) (*WorkflowDefinition, error)

	// RegisterWorkflow validates and registers a workflow definition.
	// Requires permission: agent:admin
	// Returns ErrWorkflowAlreadyExists if a workflow with the same name is already registered.
	RegisterWorkflow(ctx context.Context, def *WorkflowDefinition) error

	// UpdateWorkflow replaces an existing workflow definition by name.
	// Requires permission: agent:admin
	// Returns ErrWorkflowNotFound if the workflow does not exist.
	UpdateWorkflow(ctx context.Context, def *WorkflowDefinition) error

	// DeleteWorkflow removes a workflow definition by name.
	// Requires permission: agent:admin
	// Returns ErrWorkflowNotFound if the workflow does not exist.
	DeleteWorkflow(ctx context.Context, name string) error
}

// =============================================================================
// Sentinel Errors
// =============================================================================

var (
	// ErrAgentServiceUnavailable indicates the agent service is not available.
	ErrAgentServiceUnavailable = errors.New("agent service unavailable")

	// ErrProjectNotFound indicates a project was not found.
	ErrProjectNotFound = errors.New("agent: project not found")

	// ErrBackendRequired indicates a backend name is required.
	ErrBackendRequired = errors.New("agent: backend is required")

	// ErrPromptRequired indicates a prompt text is required.
	ErrPromptRequired = errors.New("agent: prompt is required")

	// ErrSessionLogRequired indicates an asynchronous turn was requested on an
	// orchestrator with no session event log installed. The turn's output has
	// nowhere to go, so the request is refused rather than run unobservably.
	ErrSessionLogRequired = errors.New("agent: session event log is required for asynchronous turns")

	// ErrTurnNotAccepted indicates the orchestrator could not record the
	// turn_started marker, so the turn was never dispatched.
	ErrTurnNotAccepted = errors.New("agent: turn was not accepted")

	// ErrTaskRequired indicates a task description is required.
	ErrTaskRequired = errors.New("agent: task description is required")

	// ErrRootPathRequired indicates a root path is required.
	ErrRootPathRequired = errors.New("agent: root path is required")

	// ErrProjectNameRequired indicates a project name is required.
	ErrProjectNameRequired = errors.New("agent: project name is required")

	// ErrProjectPathRequired indicates a project path is required.
	ErrProjectPathRequired = errors.New("agent: project path is required")

	// ErrSessionIDRequired indicates a session ID is required.
	ErrSessionIDRequired = errors.New("agent: session ID is required")

	// ErrSessionConclusionInProgress indicates somebody else is concluding the
	// session and this caller could not wait long enough to learn the outcome.
	// It is deliberately NOT a success: an operator-facing close reports what
	// actually happened to the session, and "another conclusion is running" is
	// not the same fact as "the session is closed".
	ErrSessionConclusionInProgress = errors.New("agent: session conclusion already in progress")

	// ErrBackendNilRequired indicates a non-nil backend is required.
	ErrBackendNilRequired = errors.New("agent: backend must not be nil")

	// ErrBackendNameRequired indicates a backend name is required.
	ErrBackendNameRequired = errors.New("agent: backend name is required")

	// ErrAgentStoreRequired indicates a non-nil agent store is required.
	ErrAgentStoreRequired = errors.New("agent: store must not be nil")

	// ErrAgentStoreNotFound indicates a session record was not found in the store.
	ErrAgentStoreNotFound = errors.New("agent store: not found")

	// ErrAgentStoreInvalidInput indicates invalid input to the agent store.
	ErrAgentStoreInvalidInput = errors.New("agent store: invalid input")

	// ErrAgentStoreMarshal indicates a serialization or deserialization failure.
	ErrAgentStoreMarshal = errors.New("agent store: marshal error")

	// ErrAgentStoreUnavailable indicates the backing key-value store rejected
	// or could not serve a session-record operation.
	ErrAgentStoreUnavailable = errors.New("agent store: unavailable")

	// ErrHealthMonitorAlreadyRunning indicates the health monitor is already running.
	ErrHealthMonitorAlreadyRunning = errors.New("agent: health monitor already running")

	// ErrHealthMonitorNotRunning indicates the health monitor is not running.
	ErrHealthMonitorNotRunning = errors.New("agent: health monitor not running")

	// ErrTaskStoreRequired indicates a task store is required for task operations.
	ErrTaskStoreRequired = errors.New("agent: task store is required")

	// ErrTaskSubjectRequired indicates a task subject is required.
	ErrTaskSubjectRequired = errors.New("agent: task subject is required")

	// ErrTenantIDRequired indicates a tenant ID is required.
	ErrTenantIDRequired = errors.New("agent: tenant ID is required")

	// ErrTaskIDRequired indicates a task ID is required.
	ErrTaskIDRequired = errors.New("agent: task ID is required")

	// ErrJobIDRequired indicates a job ID is required.
	ErrJobIDRequired = errors.New("agent: job ID is required")

	// ErrSessionStateUnknown indicates a listing filter named a session state
	// this daemon does not use.
	ErrSessionStateUnknown = errors.New("agent: unknown session state")

	// ErrSessionConclusionReasonRequired indicates a session conclusion named
	// no known reason. A conclusion without an attribution is the state the
	// reason exists to remove, so it is refused rather than defaulted.
	ErrSessionConclusionReasonRequired = errors.New("agent: session conclusion reason is required")

	// ErrAgentIDRequired indicates an agent ID is required.
	ErrAgentIDRequired = errors.New("agent: agent ID is required")

	// ErrJobLeaseStoreRequired indicates an operation that must reason about
	// which jobs are running has no lease store to read.
	ErrJobLeaseStoreRequired = errors.New("agent: job lease store is required")

	// ErrTaskLeased indicates an operation was refused because a job holds a
	// live lease on the task.
	ErrTaskLeased = errors.New("agent: task is leased by a running job")

	// ErrContainerRuntimeRequired indicates a container runtime is required for sandboxed sessions.
	ErrContainerRuntimeRequired = errors.New("agent: container runtime is required for sandboxed sessions")

	// ErrContainerImageRequired indicates a container image is required for sandboxed sessions.
	ErrContainerImageRequired = errors.New("agent: container image is required for sandboxed sessions")
)

// =============================================================================
// Typed Errors
// =============================================================================

// AgentServiceError wraps a sentinel error with an additional message.
type AgentServiceError struct {
	Sentinel error
	Message  string
}

// Error implements the error interface.
func (e *AgentServiceError) Error() string {
	if e.Message != "" {
		return e.Sentinel.Error() + ": " + e.Message
	}
	return e.Sentinel.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentServiceError) Unwrap() error {
	return e.Sentinel
}

// TaskLeasedError refuses an operation on a task a job is currently running.
// It names the holder so the caller learns what to wait for rather than only
// that it was refused.
type TaskLeasedError struct {
	TenantID string
	TaskID   string
	JobID    string
	Node     string
}

// Error implements the error interface.
func (e *TaskLeasedError) Error() string {
	msg := ErrTaskLeased.Error() + ": task " + e.TaskID + " is held by job " + e.JobID
	if e.Node != "" {
		msg += " on node " + e.Node
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskLeasedError) Unwrap() error {
	return ErrTaskLeased
}

// ProjectNotFoundError indicates a project was not found by ID.
type ProjectNotFoundError struct {
	ProjectID string
}

// Error implements the error interface.
func (e *ProjectNotFoundError) Error() string {
	return ErrProjectNotFound.Error() + ": " + e.ProjectID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProjectNotFoundError) Unwrap() error {
	return ErrProjectNotFound
}

// AgentStoreError wraps a sentinel error with an additional message
// for agent store operations.
type AgentStoreError struct {
	Sentinel error
	Message  string

	// Cause is the underlying failure, when one exists. It is exposed through
	// Unwrap so errors.Is / errors.As reach both the store's sentinel and the
	// backing store's own error.
	Cause error
}

// Error implements the error interface.
func (e *AgentStoreError) Error() string {
	if e.Message != "" {
		return e.Sentinel.Error() + ": " + e.Message
	}
	return e.Sentinel.Error()
}

// Unwrap exposes the sentinel and, when set, the underlying cause.
func (e *AgentStoreError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Sentinel}
	}
	return []error{e.Sentinel, e.Cause}
}

// =============================================================================
// Messaging Interface
// =============================================================================

// MessagingPublisher is the minimal interface for publishing events.
// The full messaging service (pkg/messaging) implements this.
type MessagingPublisher interface {
	Publish(ctx context.Context, channel string, payload []byte) error
}

// =============================================================================
// Lead Agent Configuration
// =============================================================================

// LeadAgentConfig configures an optional LLM lead agent for intelligent orchestration.
// When set, the lead agent decomposes tasks and delegates to teammates.
// When nil, tasks are managed programmatically via the API (manual mode).
type LeadAgentConfig struct {
	// Agent is the system tenant's Agent or pool the lead runs as.
	Agent AgentRef

	// Backend and Model are what Agent resolved to when the lead session
	// was spawned: the backend a request naming none defaults to.
	Backend string
	Model   string
}

// =============================================================================
// Request/Response Types
// =============================================================================

// DiscoverProjectsRequest contains parameters for discovering agent projects.
type DiscoverProjectsRequest struct {
	RootPath string `json:"root_path"`
}

// DiscoverProjectsResponse contains the result of discovering agent projects.
type DiscoverProjectsResponse struct {
	Projects  []*DiscoveredProject `json:"projects"`
	Timestamp int64                `json:"timestamp"`
}

// DiscoveredProject represents a discovered agent-enabled project.
type DiscoveredProject struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Dependencies []string `json:"dependencies,omitempty"`
	HasClaudeMD  bool     `json:"has_claude_md"`
	HasAgents    bool     `json:"has_agents"`
	AgentFiles   []string `json:"agent_files,omitempty"`
}

// RegisterProjectRequest contains parameters for registering a project.
type RegisterProjectRequest struct {
	Name           string            `json:"name"`
	Path           string            `json:"path"`
	Dependencies   []string          `json:"dependencies,omitempty"`
	DefaultBackend string            `json:"default_backend,omitempty"`
	DefaultModel   string            `json:"default_model,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// RegisterProjectResponse contains the result of registering a project.
type RegisterProjectResponse struct {
	ProjectID string `json:"project_id"`
	Timestamp int64  `json:"timestamp"`
}

// ListProjectsRequest contains parameters for listing registered projects.
type ListProjectsRequest struct {
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

// ListProjectsResponse contains the result of listing registered projects.
type ListProjectsResponse struct {
	Projects      []*RegisteredProject `json:"projects"`
	NextPageToken string               `json:"next_page_token,omitempty"`
	Timestamp     int64                `json:"timestamp"`
}

// RegisteredProject represents a registered agent-enabled project.
type RegisteredProject struct {
	ProjectID      string            `json:"project_id"`
	Name           string            `json:"name"`
	Path           string            `json:"path"`
	Dependencies   []string          `json:"dependencies,omitempty"`
	DefaultBackend string            `json:"default_backend,omitempty"`
	DefaultModel   string            `json:"default_model,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	CreatedAt      int64             `json:"created_at"`
}

// SpawnSessionRequest contains parameters for spawning a new agent session.
type SpawnSessionRequest struct {
	// AgentID names the tenant Agent this session runs as. The Agent supplies
	// the backend, the model, the image, the reasoning effort, the provider
	// endpoint, the account policy and — when the spawn names none — the
	// system prompt, so a request naming an Agent names no Backend or Model
	// of its own. Mutually exclusive with PoolID.
	AgentID string `json:"agent_id,omitempty"`

	// PoolID names the tenant pool this session runs from: its Agents are
	// tried in order, and the spawn fails over to the next when one's accounts
	// are exhausted, expired or rate-limited, its backend is unavailable, or
	// it is at capacity. Every switch is audited. Mutually exclusive with
	// AgentID.
	PoolID string `json:"pool_id,omitempty"`

	// Role is the kind of work the session does. It decides whether a pooled
	// Agent held for another role takes it; see RolePolicy.
	Role AgentRole `json:"role,omitempty"`

	Backend      string `json:"backend"`
	ProjectID    string `json:"project_id"`
	JobID        string `json:"job_id,omitempty"` // job the session works for, recorded on its durable record
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`

	// PromptAppend is run-time prompt material appended after the prompt the
	// spawn resolves — SystemPrompt, else the Agent's own prompt — separated
	// by PromptAppendSeparator. At most MaxPromptAppendBytes; see
	// prompt_append.go.
	PromptAppend string `json:"prompt_append,omitempty"`

	MCPServers      []MCPServerConfig `json:"mcp_servers,omitempty"`
	Env             map[string]string `json:"env,omitempty"`              // env vars for agent
	WorkDir         string            `json:"work_dir,omitempty"`         // explicit work directory
	SandboxMode     SandboxMode       `json:"sandbox_mode,omitempty"`     // sandbox-first policy selector; takes precedence over Sandboxed
	Sandboxed       bool              `json:"sandboxed,omitempty"`        // deprecated: use SandboxMode. true = SandboxModeRequired
	ContainerImage  string            `json:"container_image,omitempty"`  // container image override
	ContainerConfig *ContainerSpec    `json:"container_config,omitempty"` // full container spec override
	Persistent      bool              `json:"persistent,omitempty"`       // persistent session with context across prompts
	ExtraArgs       []string          `json:"extra_args,omitempty"`       // Additional CLI args for the agent command

	// TrustLevel is the integrity tier the caller requires of this session
	// (see trust_policy.go). It is the trust-axis counterpart of SandboxMode:
	// TrustLevelDefault — the zero value every legacy caller sends — resolves
	// against the server's orchestration.trust_policy and any tenant override,
	// and a level BELOW that floor is refused rather than honored. A caller can
	// therefore only ever raise its own tier, never opt out of the operator's.
	TrustLevel TrustLevel `json:"trust_level,omitempty"`

	// HostAffinity pins the session to a specific daemon host (Name or Host
	// field on the FleetNode). Empty means "any available daemon"
	// — the dispatcher picks the first online daemon serving the requested
	// backend. Required for GPU-bound workloads that must run on a
	// specific machine.
	HostAffinity string `json:"host_affinity,omitempty"`

	// GpuRequired restricts dispatch to daemons that advertise "gpu" in
	// their Capabilities slice. Use for sessions that require GPU
	// acceleration (e.g. local LLM inference via Ollama on a CUDA host).
	GpuRequired bool `json:"gpu_required,omitempty"`

	// HostAffinityStrict, when true, forces hard host_affinity semantics for
	// this request: if HostAffinity names a host that is offline the resolver
	// returns NoMatchingFleetNodeError instead of falling back to any available
	// agent. When false the operator-level orchestration.host_affinity.mode
	// default applies (soft = warn-and-fallback by default).
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`

	// --- Pooled provider account selection (see account_resolver.go) --------
	//
	// These mirror SpawnRequest's account fields and are the only way an
	// operator-facing caller can reach the account pool: SpawnSession builds
	// the backend SpawnRequest itself, so a field absent here is unreachable
	// no matter what the backend supports.
	//
	// The account a session resolves to is persisted on its SessionRecord and
	// replayed verbatim on resume. Without that, a resumed session would
	// re-run selection and could land on a different provider account than the
	// one it has been billing to — see spawnRequestFromRecord.

	// AccountID pins the session to a specific pooled provider account.
	// Leaving both AccountID and AccountAuto unset preserves the pre-account
	// behavior exactly: the backend authenticates however its CLI already
	// does.
	AccountID string `json:"account_id,omitempty"`

	// AccountAuto requests strategy-based account selection when AccountID is
	// empty.
	AccountAuto bool `json:"account_auto,omitempty"`

	// AccountStrategy names the registered AccountSelectionStrategy to select
	// with ("static", "least_usage", ...). Empty selects the registry default.
	// Ignored unless AccountAuto is true.
	AccountStrategy string `json:"account_strategy,omitempty"`

	// AccountAllow optionally names the accounts this session may use and
	// rotate through — the credential lifecycle policy for one agent. Empty
	// leaves the whole tenant/project/provider pool eligible.
	//
	// Together with AccountAuto it is what authorizes rotation: a session with
	// neither is treated as deliberately pinned and is never moved off its
	// account. See SessionRecord.CanRotateAccount.
	AccountAllow []string `json:"account_allow,omitempty"`

	// ExtraMounts are additional host directories to bind-mount into the
	// session's sandbox container. Every source is validated against the
	// server's mount policy (managed projects root + operator allowlist)
	// before any side effects; a source outside every permitted root fails
	// the spawn with MountNotAllowedError. Ignored for non-sandboxed spawns.
	ExtraMounts []ContainerMount `json:"extra_mounts,omitempty"`

	// Workspace NAMES the directory the session works in, instead of WorkDir
	// giving a host path. The host that executes the session — this daemon's
	// in-process backend or a fleet node — provisions it under its own
	// projects root and reports where on the response. Mutually exclusive
	// with WorkDir. See session_workspace.go.
	Workspace *SessionWorkspace `json:"workspace,omitempty"`

	// prepareCandidate is applied to each Agent a reference resolves to,
	// after the Agent's own settings: the job envelope's per-backend worker
	// environment, which cannot be decided before the backend is known.
	prepareCandidate func(*SpawnSessionRequest)

	// resolvedAgentID is the tenant Agent this request spawns as, set by
	// forAgent once a reference has resolved. Unexported: the Agent a session
	// is attributed to is the catalog's answer, never a caller's claim.
	resolvedAgentID string
}

// AgentRef returns the Agent reference the request names, the zero reference
// when it names a backend instead.
func (r *SpawnSessionRequest) AgentRef() AgentRef {
	return AgentRef{AgentID: r.AgentID, PoolID: r.PoolID}
}

// EffectiveSandboxMode returns the sandbox mode to be resolved against the
// active SandboxPolicy. Legacy callers that set Sandboxed=true without a
// SandboxMode are mapped to SandboxModeRequired.
func (r *SpawnSessionRequest) EffectiveSandboxMode() SandboxMode {
	if r.SandboxMode != SandboxModeDefault {
		return r.SandboxMode
	}
	if r.Sandboxed {
		return SandboxModeRequired
	}
	return SandboxModeDefault
}

// SpawnSessionResponse contains the result of spawning a session.
type SpawnSessionResponse struct {
	SessionID string `json:"session_id"`
	Backend   string `json:"backend"`
	Model     string `json:"model,omitempty"`
	// ContainerID names the container the session runs in, whether the
	// orchestrator launched it or the backend did. Empty for a session that
	// runs as a host process.
	ContainerID string `json:"container_id,omitempty"`

	// AgentID is the fleet node the spawn was dispatched to. Empty when the
	// session runs on this daemon's own in-process backend rather than on a
	// registered agent. A job records it so its page can name — and link —
	// the daemon that did the work (go-quicbaas#796).
	AgentID string `json:"agent_id,omitempty"`

	// Workspace is where the named workspace the spawn asked for was
	// provisioned: the host that holds it and the directory on that host. Nil
	// when the spawn named no workspace. A job pins its later sessions to it;
	// see SessionWorkspace.
	Workspace *WorkspacePlacement `json:"workspace,omitempty"`

	// ResolvedAgentID is the tenant Agent the session runs as, when the spawn
	// named an Agent or a pool. Empty for a spawn that named a backend.
	ResolvedAgentID string `json:"resolved_agent_id,omitempty"`

	// Failovers are the Agents a pooled spawn passed over before this one,
	// and why, in order.
	Failovers []AgentAttempt `json:"failovers,omitempty"`

	Timestamp int64 `json:"timestamp"`
}

// PromptSessionRequest contains parameters for prompting an agent session.
type PromptSessionRequest struct {
	Backend       string               `json:"backend,omitempty"`
	SessionID     string               `json:"session_id,omitempty"`
	ProjectID     string               `json:"project_id,omitempty"`
	Prompt        string               `json:"prompt"`
	Model         string               `json:"model,omitempty"`
	SystemPrompt  string               `json:"system_prompt,omitempty"`
	UseRAG        bool                 `json:"use_rag,omitempty"`
	EventCallback SessionEventCallback `json:"-"` // Real-time event streaming (not serialized)
}

// PromptSessionResponse contains the agent's response.
type PromptSessionResponse struct {
	SessionID  string         `json:"session_id,omitempty"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stop_reason,omitempty"`
	Timestamp  int64          `json:"timestamp"`

	// Usage is the token accounting the agent published for this turn, or nil
	// when its CLI publishes none. The orchestrator has already attributed it
	// to the tenant / account / project / job by the time the caller sees it;
	// it is surfaced here so a caller can show what the turn cost without a
	// second round trip to the usage API.
	Usage *TokenUsage `json:"usage,omitempty"`
}

// PromptTurnAck acknowledges that the orchestrator accepted an agent turn for
// background execution. It is the synchronous answer to PromptSessionAsync on
// every transport; the turn's actual output arrives on the session event log.
type PromptTurnAck struct {
	// SessionID is the session the turn was accepted for.
	SessionID string `json:"session_id"`

	// TurnID correlates the acknowledgement with the turn_started event and
	// with the terminal turn_completed / turn_failed event.
	TurnID string `json:"turn_id"`

	// Status is the turn's state at acknowledgement time, always
	// TurnStatusAccepted. Present so clients can branch on one field rather
	// than inferring intent from an empty content list.
	Status string `json:"status"`

	// Sequence is the session-log sequence of the turn_started event. Attach
	// with this as the starting sequence to receive the whole turn with no
	// gap and no duplicates.
	Sequence uint64 `json:"sequence"`

	// Backend is the agent backend the turn was dispatched to.
	Backend string `json:"backend,omitempty"`

	// Model is the model resolved for the turn, when known up front.
	Model string `json:"model,omitempty"`

	// Timestamp is when the turn was accepted, in Unix nanoseconds.
	Timestamp int64 `json:"timestamp"`
}

// SessionStatusRequest contains parameters for querying session status.
type SessionStatusRequest struct {
	SessionID string `json:"session_id"`
}

// SessionStatusResponse contains session status information.
type SessionStatusResponse struct {
	SessionID string       `json:"session_id"`
	State     SessionState `json:"state"`

	// LastActivity and Timestamp are UNIX NANOSECONDS; Uptime is a duration
	// in milliseconds. See the unit contract in backend.go.
	LastActivity int64 `json:"last_activity"`
	Uptime       int64 `json:"uptime"`
	Timestamp    int64 `json:"timestamp"`
}

// CloseSessionRequest contains parameters for closing a session.
type CloseSessionRequest struct {
	SessionID string `json:"session_id"`
}

// CloseSessionResponse contains the result of closing a session.
type CloseSessionResponse struct {
	SessionID string `json:"session_id"`
	Timestamp int64  `json:"timestamp"`
}

// ListSessionsRequest contains parameters for listing sessions.
type ListSessionsRequest struct {
	Backend string `json:"backend,omitempty"`

	// Status narrows the listing to one lifecycle state. Empty means every
	// state, which is what every caller that predates the filter sends.
	//
	// IT IS LOAD-BEARING NOW THAT THE LISTING MERGES DURABLE RECORDS. While
	// the service effectively listed live sessions the filter's absence was
	// invisible; once reconciliation began writing terminal records, a caller
	// asking for ACTIVE sessions was handed CLOSED ones back. An unrecognised
	// value is REFUSED rather than matched against nothing: a filter the
	// caller set and the engine silently misapplied reads as "there are no
	// such sessions", which is the wrong answer to a question that was never
	// understood.
	Status SessionState `json:"status,omitempty"`

	// ProjectID narrows the listing to one project's sessions. Empty means
	// every project.
	ProjectID string `json:"project_id,omitempty"`

	// JobID narrows the listing to the sessions one job spawned — the lead,
	// the reviewer and each worker. Empty means every job.
	JobID string `json:"job_id,omitempty"`

	// Runner narrows the listing to the sessions whose job one runner drove,
	// which is the ROLE the session played: "sdlc", "review", "triage",
	// "workflow". Empty means every role.
	Runner string `json:"runner,omitempty"`

	// AgentID narrows the listing to the sessions running on one fleet
	// agent. Empty means every agent.
	AgentID string `json:"agent_id,omitempty"`

	// Page narrows the answer to one page of the matching sessions and
	// orders it per SessionPageSpec. Nil asks for the first
	// DefaultPageLimit, newest first.
	Page *Page `json:"page,omitempty"`
}

// ListSessionsResponse contains the result of listing sessions.
//
// The paging fields carry the same envelope every other orchestrator listing
// answers with; Sessions keeps its name and place so a consumer that predates
// paging reads the response unchanged.
type ListSessionsResponse struct {
	Sessions  []*SessionInfo `json:"sessions"`
	Timestamp int64          `json:"timestamp"`

	// Total is the number of sessions the filters and search left, before
	// the page was cut.
	Total int `json:"total"`

	// Limit and Offset are the applied paging values.
	Limit  int `json:"limit"`
	Offset int `json:"offset"`

	// NextOffset is the offset of the following page, absent on the last.
	NextOffset int `json:"next_offset,omitempty"`

	// Facets counts the sessions by backend, state, role and agent over the
	// set the search left, before the faceted narrowings — what a filter bar
	// renders beside each choice. Absent when no page was asked for.
	Facets Facets `json:"facets,omitempty"`
}

// SessionInfo contains summary information about an active session.
type SessionInfo struct {
	SessionID string       `json:"session_id"`
	ProjectID string       `json:"project_id"`
	Backend   string       `json:"backend"`
	Model     string       `json:"model,omitempty"`
	State     SessionState `json:"state"`

	// JobID names the orchestration job the session works for, empty when it
	// belongs to none.
	JobID string `json:"job_id,omitempty"`

	// ContainerID names the container the session's agent runs in, empty
	// when it runs on the host. Read from the session record at list time,
	// like JobID, so the session page can show the container without a
	// second request (go-quicbaas#860).
	ContainerID string `json:"container_id,omitempty"`

	// Runner is the job runner that drove that job — "sdlc", "review",
	// "triage", or "workflow" for the engine's own. Empty when the session
	// belongs to no job, or to one no longer on record.
	//
	// THE ROW CARRIES THE ROLE BECAUSE THE PAGE CANNOT GO AND FETCH IT. A
	// session record says nothing about what the agent was doing, and a
	// worker's transcript discussing "the review's findings" reads exactly
	// like a reviewer's (go-quicbaas#969). The role is the runner of the
	// session's job — which the UI used to resolve against the job list it
	// happened to be holding. Once that list is a PAGE, the lookup misses
	// every session whose job is not on it, and the alternative is one
	// request per row. So the listing resolves the runner once, for the page
	// it is about to answer with, and the row says what it was.
	Runner string `json:"runner,omitempty"`

	// CreatedAt and LastActivity are UNIX NANOSECONDS, both of them. See the
	// unit contract in backend.go: they reached one reader in two different
	// units, and a client that guesses which is a bug that comes back
	// (go-quicbaas#855).
	CreatedAt    int64 `json:"created_at"`
	LastActivity int64 `json:"last_activity"`

	// AgentID is the fleet node the session's job was dispatched to, empty
	// when the session belongs to no job or the work ran in-process.
	//
	// IT IS RESOLVED WITH Runner AND FOR THE SAME REASON. A session record
	// says nothing about which daemon ran it, and "which agent is this
	// session on" is the question an operator asks of a fleet. Both come off
	// the session's job, so both are resolved in the one pass that reads it.
	AgentID string `json:"agent_id,omitempty"`
}

// DecomposeTaskRequest contains parameters for decomposing a task.
type DecomposeTaskRequest struct {
	Backend    string   `json:"backend"`
	Model      string   `json:"model,omitempty"`
	Task       string   `json:"task"`
	ProjectIDs []string `json:"project_ids,omitempty"`
	UseRAG     bool     `json:"use_rag,omitempty"`
}

// DecomposeTaskResponse contains the result of task decomposition.
type DecomposeTaskResponse struct {
	WorkItems []*WorkItem `json:"work_items"`
	Timestamp int64       `json:"timestamp"`
}

// WorkItem represents a decomposed unit of work.
type WorkItem struct {
	ID                   string   `json:"id"`
	ProjectID            string   `json:"project_id"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	Priority             int      `json:"priority"`
	Dependencies         []string `json:"dependencies,omitempty"`
	Backend              string   `json:"backend,omitempty"`
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
	RequiredRole         string   `json:"required_role,omitempty"`
	SecretRefs           []string `json:"secret_refs,omitempty"`
	MaxRetries           int      `json:"max_retries,omitempty"`
	Parallelizable       bool     `json:"parallelizable,omitempty"`
	FallbackBackend      string   `json:"fallback_backend,omitempty"`

	// HostAffinity optionally pins this work item to a specific daemon by
	// Name or Host. When non-empty the control loop restricts agent
	// candidates to those whose Name or Host field matches this value.
	// Empty means any eligible agent may be selected.
	HostAffinity string `json:"host_affinity,omitempty"`

	// HostAffinityStrict, when true, forces hard host_affinity semantics for
	// this work item: control loop scheduling fails if the named host is
	// offline rather than silently falling back to any eligible agent. When
	// false the operator-level orchestration.host_affinity.mode default
	// applies (soft by default).
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`
}

// AgentBackendInfo contains information about a registered agent backend.
type AgentBackendInfo struct {
	Name         string              `json:"name"`
	Type         string              `json:"type"`
	Capabilities BackendCapabilities `json:"capabilities"`
	DefaultModel string              `json:"default_model,omitempty"`
}

// RegisterBackendRequest contains parameters for registering a backend.
type RegisterBackendRequest struct {
	Name    string       `json:"name"`
	Backend AgentBackend `json:"-"`
}

// CreateTaskRequest contains parameters for creating a new orchestration task.
type CreateTaskRequest struct {
	TenantID     string            `json:"tenant_id"`
	TeamName     string            `json:"team_name,omitempty"`
	Subject      string            `json:"subject"`
	Description  string            `json:"description,omitempty"`
	AssignedTo   string            `json:"assigned_to,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`

	// ProjectID scopes the task to a registered project. When non-empty,
	// ClaimTask enforces the project's FleetNodeRoles access matrix against
	// the claiming agent. Empty means the task is unscoped (legacy — allow all).
	ProjectID string `json:"project_id,omitempty"`

	// HostAffinity pins the task to a specific agent name or hostname. When set,
	// the task scheduler restricts assignment to agents whose Name or Host field
	// matches this value. Empty means "any available agent".
	HostAffinity string `json:"host_affinity,omitempty"`

	// GpuRequired indicates the task must execute on an agent that advertises
	// "gpu" in its Capabilities slice. The scheduler skips non-GPU agents when
	// this flag is true.
	GpuRequired bool `json:"gpu_required,omitempty"`

	// AutoSpawn, when true, instructs the orchestrator to immediately spawn a
	// one-shot job on the best-matching agent instead of queuing the task for
	// manual claim. HostAffinity, GpuRequired, and Backend act as routing hints.
	// If no matching agent is online, the task is persisted with empty SpawnedJobID
	// and ErrNoMatchingFleetNode is returned.
	AutoSpawn bool `json:"auto_spawn,omitempty"`

	// Backend filters agent selection during auto-spawn. Only agents whose
	// Backend field matches this value are considered. Empty means any backend.
	// Ignored when AutoSpawn is false.
	Backend string `json:"backend,omitempty"`

	// Source records where the task came from. When set, the store refuses a
	// second task for the same (Kind, Ref) within the tenant and project.
	Source *TaskSourceRef `json:"source,omitempty"`

	// Priority orders the task for dispatch; higher is dispatched first.
	Priority int `json:"priority,omitempty"`

	// AttemptBudget bounds the jobs dispatched for the task. Zero means the
	// engine default applies.
	AttemptBudget int `json:"attempt_budget,omitempty"`

	// Runner names the JobRunner that drives attempts at this task. Empty
	// resolves to the engine's own "workflow" runner — a lead agent
	// decomposing the task into work items. A name nothing registered is
	// refused with UnknownJobRunnerError.
	Runner string `json:"runner,omitempty"`

	// PromptAppend is run-time prompt material every attempt at the task
	// carries to the sessions it spawns — the worker's, for a module runner.
	// At most MaxPromptAppendBytes.
	PromptAppend string `json:"prompt_append,omitempty"`

	// Park, when set, creates the task ALREADY BLOCKED rather than pending,
	// and suppresses the auto-spawn that AutoSpawn would otherwise trigger.
	//
	// ONE WRITE, WHICH IS THE WHOLE REASON IT IS HERE. A caller that knows the
	// work cannot start — a forge issue waiting on another repository's release
	// — would otherwise create the task and then block it, and in the window
	// between those two writes the task is pending with AutoSpawn set and a
	// sweep may start exactly the job the park exists to prevent. One write has
	// no window.
	//
	// The AutoSpawn bit SURVIVES the park, deliberately: it is what makes
	// readmission dispatch the task when its condition clears, so clearing it
	// here would park the work permanently.
	//
	// The reason must be non-empty and the kind must be a member of the closed
	// TaskBlockKind enum — a park that cannot say why it happened cannot be
	// acted on by the person it waits for.
	Park *BlockReason `json:"park,omitempty"`
}

// =============================================================================
// Job Orchestration Types
// =============================================================================

// JobState represents the state of an orchestration job.
type JobState string

const (
	// JobStateInitializing indicates the job is bootstrapping security, memory, and validators.
	JobStateInitializing JobState = "initializing"

	// JobStateDecomposing indicates the lead agent is decomposing the task.
	JobStateDecomposing JobState = "decomposing"

	// JobStateScheduling indicates the DAG scheduler is computing execution waves.
	JobStateScheduling JobState = "scheduling"

	// JobStateAssigning indicates the control loop is assigning work items to agents.
	JobStateAssigning JobState = "assigning"

	// JobStateExecuting indicates agents are executing their assigned work items.
	JobStateExecuting JobState = "executing"

	// JobStateValidating indicates pluggable validators are checking task outputs.
	JobStateValidating JobState = "validating"

	// JobStateReviewing indicates the lead agent is reviewing completed work.
	JobStateReviewing JobState = "reviewing"

	// JobStateRedelegating indicates failed or rejected work is being re-delegated.
	JobStateRedelegating JobState = "re-delegating"

	// JobStateCompleting indicates the job is finalizing results.
	JobStateCompleting JobState = "completing"

	// JobStateCleanup indicates the job is closing sessions and releasing resources.
	JobStateCleanup JobState = "cleanup"

	// JobStateEscalating indicates the job exceeded limits and is escalating to an operator.
	JobStateEscalating JobState = "escalating"

	// JobStateCompleted indicates the orchestration job completed successfully.
	JobStateCompleted JobState = "completed"

	// JobStateFailed indicates the orchestration job failed.
	JobStateFailed JobState = "failed"
)

// engineJobStates are the states the ENGINE itself drives a job through. A
// runner declares its own on top of these (KnownJobStates), which is why
// validity is the union rather than this list: "planning" is a real state to
// filter on wherever the SDLC runner is registered, and refusing it because
// the engine does not name it would make the job list unable to ask about
// most of what the factory does.
var engineJobStates = []JobState{
	JobStateInitializing,
	JobStateDecomposing,
	JobStateScheduling,
	JobStateAssigning,
	JobStateExecuting,
	JobStateValidating,
	JobStateReviewing,
	JobStateRedelegating,
	JobStateCompleting,
	JobStateCleanup,
	JobStateEscalating,
	JobStateCompleted,
	JobStateFailed,
}

// JobStateNames returns every state a job may be filtered on here — the
// engine's own plus every registered runner's — for the message a refused
// filter carries back to the caller.
func JobStateNames() []string {
	names := make([]string, 0, len(engineJobStates))
	seen := make(map[JobState]struct{}, len(engineJobStates))
	for _, state := range engineJobStates {
		seen[state] = struct{}{}
		names = append(names, string(state))
	}
	for _, spec := range KnownJobStates() {
		if _, known := seen[spec.State]; known {
			continue
		}
		seen[spec.State] = struct{}{}
		names = append(names, string(spec.State))
	}
	return names
}

// Valid reports whether the state is one this daemon has: an engine state, or
// one a registered runner declares.
func (s JobState) Valid() bool {
	for _, known := range engineJobStates {
		if s == known {
			return true
		}
	}
	for _, spec := range KnownJobStates() {
		if s == spec.State {
			return true
		}
	}
	return false
}

// JobConfig configures an orchestration job.
type JobConfig struct {
	TenantID    string `json:"tenant_id"`
	Description string `json:"description"`

	// Lead is the Agent or pool the job's lead runs as: the request's, else
	// the project's default lead. Zero for a job that names none.
	Lead AgentRef `json:"lead,omitempty"`

	// LeadBackend and LeadModel are what Lead RESOLVED to when the job
	// started — the backend the job's lease is counted against and the model
	// a stateless lead prompt runs on. They are the job's record, set by the
	// engine, never by a caller.
	LeadBackend   string        `json:"lead_backend,omitempty"`
	LeadModel     string        `json:"lead_model,omitempty"`
	ProjectID     string        `json:"project_id,omitempty"`
	PollInterval  time.Duration `json:"poll_interval,omitempty"`
	MaxIterations int           `json:"max_iterations,omitempty"`
	WorkflowName  string        `json:"workflow_name,omitempty"`

	// Runner names the JobRunner that drives this job's states
	// (job_runner.go). Empty means the engine's own "workflow" runner: a
	// lead agent decomposing the description into a DAG of work items.
	Runner string `json:"runner,omitempty"`

	// LeadAgentID is the registered fleet node ID that leads this job.
	// Set when the job is started via SpawnChildJob or when an agent
	// authenticates via SpawnChildJob on behalf of a parent. Used to
	// authorize SpawnChildJob calls: only the lead may fan out children.
	LeadAgentID string `json:"lead_agent_id,omitempty"`

	// TaskID names the task this job is an attempt at. Empty for a job that
	// was started directly rather than for a task.
	TaskID string `json:"task_id,omitempty"`

	// Attempt is the 1-based ordinal of this job among the task's attempts.
	// Zero when TaskID is empty.
	Attempt int `json:"attempt,omitempty"`

	// PromptAppend is run-time prompt material appended to every session the
	// job spawns, after the session's own prompt and any material its slot
	// adds. It is the job's record of what dynamic content it ran with: the
	// request's, or the task's for a task attempt. See prompt_append.go.
	PromptAppend string `json:"prompt_append,omitempty"`

	// Params is the RUNNER's own configuration for this job: the subject it
	// is about, in the vocabulary of whichever runner drives it.
	//
	// IT IS THE MISSING HALF OF THE JobRunner SEAM. The seam already lets a
	// module own a job's SHAPE, but every field above describes the engine's
	// own shape — a description for a lead to decompose, a task to attempt —
	// and a runner whose job is about something else had nowhere to be told
	// what. The sdlc runner got away with TaskID because a delivery IS an
	// attempt at a task; a review of a pull request is not, and encoding
	// "pull request 41" into Description for a runner to parse back out
	// would make the job's subject a matter of string handling.
	//
	// OPAQUE TO THE ENGINE, which never reads a key and never defaults one.
	// The runner that declared the keys is the only thing that interprets
	// them, and it validates them in Validate before any goroutine or lease
	// exists. Keys are namespaced by their runner so two runners in one
	// build cannot collide.
	//
	// NOT A CREDENTIAL CHANNEL. A JobConfig is persisted with the job's
	// status, returned by GetJobStatus and rendered in the UI; anything put
	// here is visible everywhere those are. Secrets stay in the
	// SecretsService and travel as references.
	Params map[string]string `json:"params,omitempty"`
}

// Param returns the runner parameter under key, and whether it was set.
func (c *JobConfig) Param(key string) (string, bool) {
	if c == nil {
		return "", false
	}
	value, ok := c.Params[key]
	return value, ok
}

// JobAuditEvent records a single event in the job's complete audit trail.
type JobAuditEvent struct {
	Timestamp int64  `json:"timestamp"`
	Type      string `json:"type"`
	Details   string `json:"details"`
}

// JobStatus reports the current state of an orchestration job.
type JobStatus struct {
	JobID       string   `json:"job_id"`
	TenantID    string   `json:"tenant_id"`
	State       JobState `json:"state"`
	Iteration   int      `json:"iteration"`
	TaskCount   int      `json:"task_count"`
	Completed   int      `json:"completed"`
	Failed      int      `json:"failed"`
	Description string   `json:"description"`
	StartedAt   int64    `json:"started_at"`
	UpdatedAt   int64    `json:"updated_at"`
	Error       string   `json:"error,omitempty"`

	Events []JobAuditEvent `json:"events,omitempty"` // Complete audit trail

	// Recoverable state for job resumption after server restart.
	WorkItems    []*WorkItem `json:"work_items,omitempty"`
	Config       *JobConfig  `json:"config,omitempty"`
	Memory       *JobMemory  `json:"memory,omitempty"`
	WorkflowName string      `json:"workflow_name,omitempty"`

	// Runner names the JobRunner that drove this job. Empty on a record
	// written before runners existed, which is a workflow job.
	Runner string `json:"runner,omitempty"`

	// Sub-job fan-out. ParentJobID is empty for root jobs.
	// ChildJobIDs is appended atomically as child jobs are spawned.
	ParentJobID string   `json:"parent_job_id,omitempty"`
	ChildJobIDs []string `json:"child_job_ids,omitempty"`
	SpawnedBy   string   `json:"spawned_by,omitempty"` // agent_id that spawned this child
	SpawnedAt   int64    `json:"spawned_at,omitempty"` // Unix nanos

	// TaskID and Attempt bind the job to the task it is an attempt at; both
	// are copied from Config so a listing can show them without the config.
	TaskID  string `json:"task_id,omitempty"`
	Attempt int    `json:"attempt,omitempty"`

	// PromptAppend is the run-time prompt material the job appends to every
	// session it spawns, copied from Config: the audit of what the job's
	// sessions ran with, beyond their resolved prompts.
	PromptAppend string `json:"prompt_append,omitempty"`

	// Node names the daemon running the job's control loop.
	Node string `json:"node,omitempty"`

	// HeartbeatAt is the Unix-nanosecond instant the running loop last
	// proved it was alive.
	HeartbeatAt int64 `json:"heartbeat_at,omitempty"`

	// FailureClass classifies a failed job's cause so the task's attempt
	// accounting can tell an environment fault from a failure of the work.
	FailureClass string `json:"failure_class,omitempty"`

	// Park is the runner's own judgement that the task must stop and wait
	// for a person, nil when it made none. See JobRun.Park.
	//
	// IT IS ON THE PERSISTED RECORD BECAUSE THE JUDGEMENT MUST OUTLIVE THE
	// PROCESS THAT MADE IT. A park held only on the live JobRun is lost the
	// moment the daemon dies between writing this status and applying the
	// task conclusion: the lease reaper then rebuilds the conclusion from
	// this record alone, stamps it `abandoned`, and the task is requeued —
	// which re-creates the budget-burning loop go-quicbaas#787 exists to
	// remove, across a crash instead of within one run.
	Park *BlockReason `json:"park,omitempty"`

	// Satisfied is the runner's judgement that the task's work was ALREADY
	// DONE before the attempt began. It travels on the status for the same
	// reason Park does: the conclusion is derived from this record, and a
	// judgement left only in the running process is lost to a crash.
	//
	// A satisfied attempt COMPLETES the task — it is not a failure and not a
	// park — but concludes it `already_satisfied` rather than `completed`, so
	// the issue is told it is stale instead of being closed in silence.
	Satisfied bool `json:"satisfied,omitempty"`

	// Superseded is the runner's judgement that the attempt's pull request
	// was closed without merging, so the delivery is over. It travels on the
	// status for the same reason Satisfied does. The task completes and
	// concludes `superseded` (go-quicbaas#1054).
	Superseded bool `json:"superseded,omitempty"`

	// LeadSessionID is the session of the lead agent, empty until spawned.
	LeadSessionID string `json:"lead_session_id,omitempty"`

	// SessionIDs lists every session the job has spawned and not yet closed:
	// the lead, the reviewer and each worker role, in stable order.
	SessionIDs []string `json:"session_ids,omitempty"`

	// Stage is the last lifecycle moment the job passed through (prepare,
	// reviewable, complete, discard, blocked); empty until the first moment
	// runs or when the workflow runs no hooks.
	Stage string `json:"stage,omitempty"`

	// Stages is every lifecycle moment the job entered, in order, with the
	// instant each was entered. Stage is the last of them. See
	// job_provenance.go.
	Stages []JobStageRecord `json:"stages,omitempty"`

	// AgentID is the FLEET agent the job's work was dispatched to — the
	// daemon whose runtime spawned the session, which is not necessarily
	// Node, the daemon whose control loop drove the job. Empty when the work
	// ran on this daemon's own in-process backend, where Node names it.
	AgentID string `json:"agent_id,omitempty"`

	// WorkspacePin is where the job's named workspace was provisioned: the
	// host holding its checkout and the directory on it. Every later session
	// of the job is pinned there. It is on the persisted record so a resumed
	// job respawns where its checkout is rather than cloning afresh on
	// whatever host the dispatcher would pick. Nil for a job that named no
	// workspace.
	WorkspacePin *WorkspacePlacement `json:"workspace_pin,omitempty"`

	// --- Derived from the task when the status is read -------------------
	//
	// These are the TASK's facts, not the run's: they are filled in on read
	// (GetJobStatus) rather than persisted, because a copy taken when the
	// job last wrote its status would be stale for every attempt after it.
	// All are absent for a job that is not an attempt at a task.

	// Source is where the job's TASK came from — the forge issue it was
	// adopted from, carrying the ref (`owner/repo#62`) and the URL the origin
	// recorded. It is the task's own TaskSourceRef, unchanged, so a consumer
	// names a job's issue with the code it already uses for a task's.
	//
	// IT IS ON THE LISTING AND NOT ONLY ON THE DETAIL READ, which is the
	// whole point of it. The jobs view is server-paged: it holds twenty-five
	// jobs and one page of tasks, so resolving each row's issue against the
	// tasks it happens to hold only ever worked for the jobs whose task shared
	// that page, and blanked for every other row (go-quicbaas#976). ListJobsPage
	// fills it for the page it answers, memoised per task, so a page of
	// twenty-five costs at most twenty-five task reads and the consumer needs
	// no second request at all.
	Source *TaskSourceRef `json:"source,omitempty"`

	// Attempts is every attempt at the job's task, oldest first, with the
	// one this status belongs to marked Current.
	Attempts []JobAttemptRecord `json:"attempts,omitempty"`

	// AttemptBudget is the number of charged attempts the task allows.
	AttemptBudget int `json:"attempt_budget,omitempty"`

	// TerminalReason is the typed reason the task stopped, from the closed
	// vocabulary in task_conclusion.go. Empty while the task is still
	// running or requeued.
	TerminalReason TaskConclusionReason `json:"terminal_reason,omitempty"`

	// ReadmitAllowed says whether a person may return the task to the queue.
	// A parked task may be readmitted; a failed one is terminal.
	ReadmitAllowed bool `json:"readmit_allowed,omitempty"`

	// Delivery is the git side of the attempt — branch, head, pull request
	// and review rounds — read off the artifacts the runner published on the
	// task. Nil for a runner that publishes none.
	Delivery *JobDelivery `json:"delivery,omitempty"`

	// Verification is the outcome of the last validator pipeline the job ran
	// — the build, the tests, whatever the project's criteria named. It is
	// recorded by JobRun.Verify and persists with the status, so a failed
	// job still says which command refused it.
	Verification []ValidationResult `json:"verification,omitempty"`
}

// JobFilter narrows a job listing.
//
// A NARROWING THE SERVER PERFORMS, not one the caller is trusted to apply.
// Every transport accepts a task_id, and before go-quicbaas#822 every
// transport also ignored it: the service listed the whole tenant and each
// client re-filtered whatever arrived. A caller that trusted the query — the
// CLI, an MCP tool, any SDK consumer — was handed another task's attempts and
// had no way to tell. The filter belongs here, once, where every transport
// shares it.
//
// A nil filter, and a filter with no field set, both list every job the
// tenant owns: "no narrowing asked for" must never read as "narrow to
// nothing".
type JobFilter struct {
	// TaskID restricts the listing to attempts at one task.
	TaskID string `json:"task_id,omitempty"`

	// Statuses restricts the listing to jobs in any of the given states.
	// Empty lists every state, which is what the listing did before the
	// tenant's job history grew past what a UI can hold.
	Statuses []JobState `json:"statuses,omitempty"`

	// Runner restricts the listing to the jobs one JobRunner drove —
	// "sdlc", "triage", "review". A job recorded before runners existed
	// carries no runner and is a workflow job, so it matches no runner
	// name rather than every one.
	Runner string `json:"runner,omitempty"`

	// ProjectID restricts the listing to jobs launched under one project.
	ProjectID string `json:"project_id,omitempty"`

	// JobIDs restricts the listing to the named jobs.
	//
	// IT IS HOW A PAGE OF SOMETHING ELSE RESOLVES ITS JOBS IN ONE CALL. A
	// page of sessions names a job per row and renders the runner's role from
	// it; a page of tasks names the job its latest attempt ran as. Before
	// this filter the only ways to fill those columns were to list the whole
	// tenant — which paging exists to stop — or to issue one GetJobStatus per
	// row. Empty lists every job, as every other field here does.
	JobIDs []string `json:"job_ids,omitempty"`

	// AgentID restricts the listing to the jobs dispatched to one fleet
	// agent — JobStatus.AgentID, the daemon whose runtime spawned the work,
	// which is not necessarily the daemon that drove the control loop. A job
	// that ran in-process carries none and matches no agent name.
	AgentID string `json:"agent_id,omitempty"`
}

// Matches reports whether status satisfies the filter.
//
// THE TWO NIL ARMS ARE DELIBERATELY NOT SYMMETRIC, which the name alone will
// not tell you, so it is written down here rather than rediscovered:
//
//   - A NIL FILTER matches every non-nil status. "No narrowing asked for"
//     means list everything; ListJobs relies on exactly this to serve an
//     unfiltered request through the same code path as a filtered one.
//   - A NIL STATUS matches nothing, filter or no filter. There is no record
//     to answer for, and reporting a match would put a nil into the caller's
//     result set.
//
// So the first is "the question is unrestricted" and the second is "there is
// nothing to ask about". Collapsing them — making a nil filter match a nil
// status, say — would look tidier and would hand ListJobs a nil entry the
// moment a record failed to decode.
func (f *JobFilter) Matches(status *JobStatus) bool {
	if status == nil {
		return false
	}
	if f == nil {
		return true
	}
	if f.TaskID != "" && jobStatusTaskID(status) != f.TaskID {
		return false
	}
	if f.Runner != "" && jobStatusRunner(status) != f.Runner {
		return false
	}
	if f.ProjectID != "" && jobStatusProjectID(status) != f.ProjectID {
		return false
	}
	if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, status.State) {
		return false
	}
	if len(f.JobIDs) > 0 && !slices.Contains(f.JobIDs, status.JobID) {
		return false
	}
	return true
}

// Empty reports whether the filter narrows nothing.
func (f *JobFilter) Empty() bool {
	return f == nil ||
		(f.TaskID == "" && f.Runner == "" && f.ProjectID == "" &&
			len(f.Statuses) == 0 && len(f.JobIDs) == 0)
}

// jobStatusTaskID is the task a job is an attempt at.
//
// The binding is recorded twice — on the status and on the config it was
// launched from — and the two can disagree: a status persisted before the
// binding was promoted onto JobStatus carries it only in the config. Reading
// the status first and the config second means a filter answers for both
// shapes rather than silently dropping the older one.
func jobStatusTaskID(status *JobStatus) string {
	if status == nil {
		return ""
	}
	if status.TaskID != "" {
		return status.TaskID
	}
	if status.Config != nil {
		return status.Config.TaskID
	}
	return ""
}

// jobStatusRunner is the JobRunner that drove a job, read from the status
// first and the launch config second for the same reason jobStatusTaskID is:
// a record written before the field was promoted onto JobStatus carries it
// only on the config, and a filter that read one shape would drop the other.
func jobStatusRunner(status *JobStatus) string {
	if status == nil {
		return ""
	}
	if status.Runner != "" {
		return status.Runner
	}
	if status.Config != nil {
		return status.Config.Runner
	}
	return ""
}

// jobStatusProjectID is the project a job was launched under. It lives only
// on the launch config -- JobStatus never promoted it -- so the listing reads
// it from there.
func jobStatusProjectID(status *JobStatus) string {
	if status == nil || status.Config == nil {
		return ""
	}
	return status.Config.ProjectID
}

// JobEventTypeStateChange is the JobEvent.Type of an event the control loop
// emits as it moves between states.
const JobEventTypeStateChange = "state_change"

// JobEventTypeStage is the JobEvent.Type of an event the control loop emits
// as a job enters a lifecycle moment; Metadata carries the stage name.
const JobEventTypeStage = "stage"

// JobEvent represents a real-time event from a running orchestration job.
type JobEvent struct {
	JobID     string            `json:"job_id"`
	Type      string            `json:"type"`
	State     JobState          `json:"state"`
	Message   string            `json:"message"`
	Iteration int               `json:"iteration"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp int64             `json:"timestamp"`
}

// StartJobRequest starts an autonomous orchestration job.
type StartJobRequest struct {
	TenantID    string `json:"tenant_id"`
	Description string `json:"description"`

	// Lead is the Agent or pool the job's lead runs as. Zero takes the
	// project's default lead, and a job with neither leads with whatever its
	// workflow's lead role names.
	Lead AgentRef `json:"lead,omitempty"`

	LeadAgentID   string `json:"lead_agent_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	MaxIterations int    `json:"max_iterations,omitempty"`
	PollInterval  string `json:"poll_interval,omitempty"`
	WorkflowName  string `json:"workflow_name,omitempty"`
	WorkflowFile  string `json:"workflow_file,omitempty"`

	// Runner names the JobRunner that drives the job. Empty selects the
	// engine's own "workflow" runner.
	Runner string `json:"runner,omitempty"`

	// TaskID binds the job to the task it is an attempt at; Attempt is the
	// 1-based ordinal of that attempt. Both are copied onto JobConfig.
	TaskID  string `json:"task_id,omitempty"`
	Attempt int    `json:"attempt,omitempty"`

	// PromptAppend is run-time prompt material appended to every session the
	// job spawns; copied onto JobConfig. At most MaxPromptAppendBytes.
	PromptAppend string `json:"prompt_append,omitempty"`

	// Params is the runner's own configuration for this job, copied onto
	// JobConfig verbatim. See JobConfig.Params — in particular that it is
	// persisted and displayed, so it carries no secret.
	Params map[string]string `json:"params,omitempty"`

	// routedBackend is the backend a task's routing restricts its attempt
	// to. It is the engine's own input to a job it starts for a task, never a
	// caller's: a caller names a lead by Agent reference.
	routedBackend string
}

// StartJobResponse contains the result of starting an orchestration job.
type StartJobResponse struct {
	JobID     string `json:"job_id"`
	Timestamp int64  `json:"timestamp"`
}

// ReviewResponse represents the parsed JSON response from a lead agent review.
type ReviewResponse struct {
	Approved     bool        `json:"approved"`
	Feedback     string      `json:"feedback,omitempty"`
	NewWorkItems []*WorkItem `json:"new_work_items,omitempty"`
}

// IsTerminalJobState returns true if the given job state is a terminal state
// (completed or failed). Terminal states indicate the job is no longer running.
func IsTerminalJobState(state JobState) bool {
	return state == JobStateCompleted || state == JobStateFailed
}

// =============================================================================
// Sub-job Spawning Types
// =============================================================================

// TaskSpec is an inline task definition supplied when spawning a child job
// without an existing workflow definition. Each spec becomes a WorkItem in
// the child job's execution plan.
type TaskSpec struct {
	// Title is a short human-readable label for the work item.
	Title string `json:"title"`

	// Description is the full instruction for the agent executing this task.
	Description string `json:"description"`

	// Dependencies lists TaskSpec Titles that must complete before this one.
	Dependencies []string `json:"dependencies,omitempty"`

	// RequiredCapabilities constrains dispatch to agents advertising all listed
	// capability strings (e.g., "gpu", "code", "docker").
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
}

// SpawnChildJobRequest is sent by a lead agent to fan out child jobs to other
// agents on other hosts. The calling agent must be the registered lead of the
// parent job; any other caller receives NotLeadOfParentError.
type SpawnChildJobRequest struct {
	// ParentJobID is the job whose lead agent is spawning this child. Required.
	ParentJobID string `json:"parent_job_id"`

	// CallerAgentID is the agent ID extracted from the mTLS auth context on
	// the server side. Clients do not set this field directly; the gRPC
	// handler populates it from the peer certificate.
	CallerAgentID string `json:"caller_agent_id,omitempty"`

	// WorkflowID optionally names an existing workflow definition to use for
	// the child job's task plan. Mutually exclusive with Tasks.
	WorkflowID string `json:"workflow_id,omitempty"`

	// ProjectID scopes the child job to a registered project.
	ProjectID string `json:"project_id,omitempty"`

	// Tasks provides inline task specs when no workflow is referenced.
	Tasks []*TaskSpec `json:"tasks,omitempty"`

	// HostAffinity is a routing hint to pin the child to a specific agent
	// daemon (Name or Host). Empty means "any available daemon".
	HostAffinity string `json:"host_affinity,omitempty"`

	// GpuRequired restricts dispatch to daemons that advertise "gpu" in
	// their Capabilities slice.
	GpuRequired bool `json:"gpu_required,omitempty"`

	// HostAffinityStrict, when true, treats HostAffinity as a hard constraint:
	// if the named host is offline the request fails with NoMatchingFleetNodeError
	// rather than falling back to any available agent.
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`
}

// SpawnChildJobResponse is returned after a child job has been accepted and
// queued. The ChildJobID can be polled via GetJobStatus.
type SpawnChildJobResponse struct {
	// ChildJobID is the generated ID of the newly spawned child job.
	ChildJobID string `json:"child_job_id"`
}

// jobPersistenceKey builds the KV store key for a persisted job status.
//
// The tenant segment is normalised the same way every other durable
// orchestration record normalises it (StorageTenant): a caller that reached
// the orchestrator without a tenant files under the system tenant rather than
// under an empty segment, so its record is addressable by a reader that asks
// for "system" instead of sitting at a key no listing can name.
func jobPersistenceKey(tenantID, jobID string) string {
	return jobPersistencePrefix(tenantID) + jobID
}

// jobPersistencePrefix is the prefix covering every persisted job record owned
// by tenantID. A scan bounded by it cannot observe another tenant.
func jobPersistencePrefix(tenantID string) string {
	if tenantID == "" {
		tenantID = SystemTenantID
	}
	return "job:" + tenantID + ":"
}
