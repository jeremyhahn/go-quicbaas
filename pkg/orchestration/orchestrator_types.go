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

	// ListSessions returns all active sessions, optionally filtered by backend.
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

	// RegisterAgent registers an agent with the fleet registry.
	// Requires permission: agent:admin
	RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*RegisterAgentResponse, error)

	// GetAgent retrieves a registered agent by ID.
	// Requires permission: agent:read
	GetAgent(ctx context.Context, tenantID, agentID string) (*AgentRegistration, error)

	// ListAgents lists all registered agents for a tenant.
	// Requires permission: agent:read
	ListAgents(ctx context.Context, tenantID string, filter *AgentFilter) ([]*AgentRegistration, error)

	// RemoveAgent removes an agent from the fleet registry.
	// Requires permission: agent:admin
	RemoveAgent(ctx context.Context, tenantID, agentID string) error

	// AgentHeartbeat updates an agent's last seen timestamp.
	AgentHeartbeat(ctx context.Context, tenantID, agentID string) error

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

	// ListJobs returns all jobs for a tenant, sorted by start time descending.
	// Requires permission: agent:job
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - tenantID: The tenant namespace
	// Returns:
	//   - []*JobStatus: All jobs for the tenant
	//   - error: ErrTenantIDRequired
	ListJobs(ctx context.Context, tenantID string) ([]*JobStatus, error)

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

	// ErrAgentIDRequired indicates an agent ID is required.
	ErrAgentIDRequired = errors.New("agent: agent ID is required")

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
	Backend      string // backend name to use for the lead agent
	Model        string // model to use
	SystemPrompt string // system prompt for orchestration
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
	Backend         string            `json:"backend"`
	ProjectID       string            `json:"project_id"`
	Model           string            `json:"model,omitempty"`
	SystemPrompt    string            `json:"system_prompt,omitempty"`
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
	// field on the AgentRegistration). Empty means "any available daemon"
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
	// returns NoMatchingAgentError instead of falling back to any available
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
	SessionID   string `json:"session_id"`
	Backend     string `json:"backend"`
	Model       string `json:"model,omitempty"`
	ContainerID string `json:"container_id,omitempty"` // set when sandboxed
	Timestamp   int64  `json:"timestamp"`
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
	SessionID    string       `json:"session_id"`
	State        SessionState `json:"state"`
	LastActivity int64        `json:"last_activity"`
	Uptime       int64        `json:"uptime"`
	Timestamp    int64        `json:"timestamp"`
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

// ListSessionsRequest contains parameters for listing active sessions.
type ListSessionsRequest struct {
	Backend string `json:"backend,omitempty"`
}

// ListSessionsResponse contains the result of listing sessions.
type ListSessionsResponse struct {
	Sessions  []*SessionInfo `json:"sessions"`
	Timestamp int64          `json:"timestamp"`
}

// SessionInfo contains summary information about an active session.
type SessionInfo struct {
	SessionID    string       `json:"session_id"`
	ProjectID    string       `json:"project_id"`
	Backend      string       `json:"backend"`
	Model        string       `json:"model,omitempty"`
	State        SessionState `json:"state"`
	CreatedAt    int64        `json:"created_at"`
	LastActivity int64        `json:"last_activity"`
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
	// ClaimTask enforces the project's AgentRoles access matrix against
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
	// and ErrNoMatchingAgent is returned.
	AutoSpawn bool `json:"auto_spawn,omitempty"`

	// Backend filters agent selection during auto-spawn. Only agents whose
	// Backend field matches this value are considered. Empty means any backend.
	// Ignored when AutoSpawn is false.
	Backend string `json:"backend,omitempty"`
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

// JobConfig configures an orchestration job.
type JobConfig struct {
	TenantID      string        `json:"tenant_id"`
	Description   string        `json:"description"`
	LeadBackend   string        `json:"lead_backend,omitempty"`
	LeadModel     string        `json:"lead_model,omitempty"`
	ProjectID     string        `json:"project_id,omitempty"`
	PollInterval  time.Duration `json:"poll_interval,omitempty"`
	MaxIterations int           `json:"max_iterations,omitempty"`
	WorkflowName  string        `json:"workflow_name,omitempty"`

	// LeadAgentID is the registered fleet agent ID that leads this job.
	// Set when the job is started via SpawnChildJob or when an agent
	// authenticates via SpawnChildJob on behalf of a parent. Used to
	// authorize SpawnChildJob calls: only the lead may fan out children.
	LeadAgentID string `json:"lead_agent_id,omitempty"`
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

	// Sub-job fan-out. ParentJobID is empty for root jobs.
	// ChildJobIDs is appended atomically as child jobs are spawned.
	ParentJobID string   `json:"parent_job_id,omitempty"`
	ChildJobIDs []string `json:"child_job_ids,omitempty"`
	SpawnedBy   string   `json:"spawned_by,omitempty"` // agent_id that spawned this child
	SpawnedAt   int64    `json:"spawned_at,omitempty"` // Unix nanos
}

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
	TenantID      string `json:"tenant_id"`
	Description   string `json:"description"`
	LeadBackend   string `json:"lead_backend,omitempty"`
	LeadModel     string `json:"lead_model,omitempty"`
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
	// if the named host is offline the request fails with NoMatchingAgentError
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
func jobPersistenceKey(tenantID, jobID string) string {
	return "job:" + tenantID + ":" + jobID
}
