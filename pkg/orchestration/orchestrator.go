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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// agentServiceImpl implements the AgentService interface.
type agentServiceImpl struct {
	store            AgentStore
	backends         sync.Map // map[string]AgentBackend
	projects         sync.Map // map[string]*RegisteredProject
	sessions         sync.Map // map[string]*sessionRecord
	tenantGuard      *TenantGuard
	taskStore        TaskStore          // distributed task store
	channelServer    *ChannelServer     // MCP channel for Claude Code events
	containerRuntime ContainerRuntime   // OCI container runtime
	messaging        MessagingPublisher // pub/sub for cross-host coordination
	leadAgent        *LeadAgentConfig   // optional lead agent for auto-orchestration
	leadSessionID    string             // session ID for the lead agent
	agentRegistry    AgentRegistry      // fleet agent registry
	jobs             sync.Map           // map[string]*runningJob
	kvStore          interfaces.KVStore // KV store for job persistence
	workflowLoader   *WorkflowLoader    // workflow loader for job resumption
	logger           interfaces.Logger
	eventSubscribers sync.Map // map[string]*subscriberList keyed by tenantID/jobID
	sandboxPolicy    SandboxPolicy
	tenantPolicies   TenantPolicyProvider  // optional resolver for per-tenant overrides
	remoteResolver   RemoteBackendResolver // optional: routes daemon-kind registrations to RemoteACPBackend
	sessionLog       SessionEventLog       // optional: append-only per-session event log for attach/replay
	projectAuthz     *ProjectAuthz         // optional: enforces per-project agent role matrix
}

// TenantPolicyProvider supplies per-tenant policy overrides that can tighten
// (but never loosen) the server-level SandboxPolicy. Implementations are
// typically backed by the IAM subsystem.
type TenantPolicyProvider interface {
	// SandboxPolicy returns the tenant's sandbox policy override if one is
	// configured, or nil to defer to the server-level policy.
	SandboxPolicy(tenantID string) *SandboxPolicy
}

// RemoteBackendResolver lets the BackendDispatcher route a backend name to a
// remote daemon (e.g., RemoteACPBackend) before falling back to locally
// registered backends. Returning (backend, true) wins; (nil, false) defers
// to the local lookup. Implementations are typically driven by the
// AgentRegistry, matching registrations where kind=daemon and backend=name.
//
// Routing hints beyond backend+projectID (e.g. host affinity) are threaded
// via the context using WithHostAffinity / HostAffinityFromContext so this
// function signature stays stable as we add more selectors.
type RemoteBackendResolver func(ctx context.Context, backend, projectID string) (AgentBackend, bool)

// hostAffinityKey is the unexported context key used to thread a requested
// host affinity from SpawnSession down into a RemoteBackendResolver.
type hostAffinityKey struct{}

// WithHostAffinity returns a context carrying the given host affinity hint.
// Resolvers use HostAffinityFromContext to read it back.
func WithHostAffinity(ctx context.Context, host string) context.Context {
	if host == "" {
		return ctx
	}
	return context.WithValue(ctx, hostAffinityKey{}, host)
}

// HostAffinityFromContext returns the host affinity hint, or "" when none
// was set. Safe to call on any context.
func HostAffinityFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(hostAffinityKey{}).(string); ok {
		return v
	}
	return ""
}

// gpuRequiredKey is the unexported context key used to thread a GPU
// requirement from Spawn / Infer requests into the RemoteBackendResolver.
type gpuRequiredKey struct{}

// WithGpuRequired returns a context that signals the resolver must restrict
// candidate daemons to those advertising "gpu" in their Capabilities slice.
func WithGpuRequired(ctx context.Context) context.Context {
	return context.WithValue(ctx, gpuRequiredKey{}, true)
}

// GpuRequiredFromContext returns true when the context carries a GPU
// requirement set via WithGpuRequired. Safe to call on any context.
func GpuRequiredFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(gpuRequiredKey{}).(bool)
	return v
}

// runningJob tracks a running control loop and its cancellation function.
type runningJob struct {
	loop   *ControlLoop
	cancel context.CancelFunc
}

// eventSubscriber holds a subscriber channel and its done signal.
type eventSubscriber struct {
	ch   chan *JobEvent
	done chan struct{}
}

// subscriberList wraps a slice of event subscribers. Using a pointer to this
// struct as the sync.Map value enables CompareAndSwap since pointers are comparable.
type subscriberList struct {
	subs []*eventSubscriber
}

// sessionRecord tracks which backend owns a session in memory.
// This provides fast in-memory lookup for active sessions while the
// AgentStore handles durable persistence.
type sessionRecord struct {
	Backend     string
	ProjectID   string
	Model       string
	TenantID    string
	ContainerID string
	CreatedAt   int64
}

// NewAgentService creates a new AgentService with the given store, backends, and logger.
func NewAgentService(
	store AgentStore,
	backends map[string]AgentBackend,
	logger interfaces.Logger,
) (AgentService, error) {
	if store == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentStoreRequired,
			Message:  "agent store must not be nil",
		}
	}
	if logger == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "logger must not be nil",
		}
	}

	svc := &agentServiceImpl{
		store:  store,
		logger: logger,
	}

	for name, backend := range backends {
		svc.backends.Store(name, backend)
	}

	return svc, nil
}

// SetTenantGuard configures the tenant guard for strict tenant isolation.
// When set, all public methods enforce tenant boundaries and block
// SuperAdmin cross-tenant access for orchestration operations.
func (s *agentServiceImpl) SetTenantGuard(guard *TenantGuard) {
	s.tenantGuard = guard
}

// SetTaskStore configures the distributed task store for task management.
// When set, task creation, claiming, and completion operations become available.
func (s *agentServiceImpl) SetTaskStore(store TaskStore) {
	s.taskStore = store
}

// SetChannelServer configures the MCP channel server for pushing events
// into Claude Code sessions. When set, task lifecycle events are forwarded
// to the channel server as notifications.
func (s *agentServiceImpl) SetChannelServer(cs *ChannelServer) {
	s.channelServer = cs
}

// SetContainerRuntime configures the OCI container runtime for sandboxed
// agent sessions. When set, SpawnSession requests with Sandboxed=true
// will launch containers instead of spawning directly via the backend.
func (s *agentServiceImpl) SetContainerRuntime(rt ContainerRuntime) {
	s.containerRuntime = rt
}

// SetMessaging configures the messaging publisher for cross-host event
// coordination. When set, session and task lifecycle events are published
// to messaging channels for consumption by other nodes or services.
func (s *agentServiceImpl) SetMessaging(pub MessagingPublisher) {
	s.messaging = pub
}

// SetLeadAgent configures an optional LLM lead agent for intelligent
// task orchestration. When set, the lead agent decomposes tasks and
// delegates to teammates. When nil, tasks are managed programmatically.
func (s *agentServiceImpl) SetLeadAgent(config *LeadAgentConfig) {
	s.leadAgent = config
}

// SetLeadSessionID stores the session ID of the persistent lead agent session.
// This session is used for intelligent task decomposition when no explicit
// backend is specified in DecomposeTask requests.
func (s *agentServiceImpl) SetLeadSessionID(id string) {
	s.leadSessionID = id
}

// SetAgentRegistry configures the agent fleet registry for tracking
// and managing agents across hosts. When set, agent registration,
// heartbeat, and fleet management operations become available.
func (s *agentServiceImpl) SetAgentRegistry(registry AgentRegistry) {
	s.agentRegistry = registry
}

// SetKVStore configures the KV store for durable job persistence.
// When set, job status is persisted to the KV store on every state
// transition and can be recovered on startup.
func (s *agentServiceImpl) SetKVStore(kv interfaces.KVStore) {
	s.kvStore = kv
}

// SetWorkflowLoader configures the workflow loader for job resumption.
// When set, recovered jobs can reload their workflow definitions by name.
func (s *agentServiceImpl) SetWorkflowLoader(wl *WorkflowLoader) {
	s.workflowLoader = wl
}

// ListWorkflows returns all registered workflow definitions sorted by name.
func (s *agentServiceImpl) ListWorkflows(_ context.Context) ([]*WorkflowDefinition, error) {
	if s.workflowLoader == nil {
		return []*WorkflowDefinition{}, nil
	}
	return s.workflowLoader.ListDefinitions(), nil
}

// GetWorkflow returns a workflow definition by name.
func (s *agentServiceImpl) GetWorkflow(_ context.Context, name string) (*WorkflowDefinition, error) {
	if s.workflowLoader == nil {
		return nil, &WorkflowNotFoundError{Name: name}
	}
	return s.workflowLoader.Get(name)
}

// RegisterWorkflow validates and registers a workflow definition.
func (s *agentServiceImpl) RegisterWorkflow(_ context.Context, def *WorkflowDefinition) error {
	if s.workflowLoader == nil {
		s.workflowLoader = NewWorkflowLoader()
	}
	return s.workflowLoader.Register(def)
}

// UpdateWorkflow replaces an existing workflow definition by name.
func (s *agentServiceImpl) UpdateWorkflow(_ context.Context, def *WorkflowDefinition) error {
	if s.workflowLoader == nil {
		return &WorkflowNotFoundError{Name: def.Name}
	}
	return s.workflowLoader.Update(def)
}

// DeleteWorkflow removes a workflow definition by name.
func (s *agentServiceImpl) DeleteWorkflow(_ context.Context, name string) error {
	if s.workflowLoader == nil {
		return &WorkflowNotFoundError{Name: name}
	}
	return s.workflowLoader.Delete(name)
}

// SpawnChildJob fans out a child job from a parent job's lead agent.
// The calling agent (identified via req.CallerAgentID populated by the gRPC
// handler from mTLS context) must be the registered lead of the parent job.
// Any other agent receives NotLeadOfParentError (mapped to HTTP 403).
//
// Pattern for agentic fan-out:
//
//	Agent A (lead) decomposes work and calls SpawnChildJob for each sub-task.
//	Child jobs run on different hosts; their status is retrievable via GetJobStatus.
//	The parent's ChildJobIDs slice is updated atomically after each successful spawn.
func (s *agentServiceImpl) SpawnChildJob(ctx context.Context, req *SpawnChildJobRequest) (*SpawnChildJobResponse, error) {
	if req == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "spawn child job request must not be nil",
		}
	}

	if req.ParentJobID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrParentJobNotFound,
			Message:  "parent_job_id is required",
		}
	}

	// Resolve tenant from context.
	tenantID := TenantFromContext(ctx)
	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required",
		}
	}

	// Load parent job status from memory or KV.
	parentStatus, err := s.GetJobStatus(ctx, tenantID, req.ParentJobID)
	if err != nil {
		return nil, &ParentJobNotFoundError{ParentJobID: req.ParentJobID}
	}

	// Enforce lead-only constraint: the caller must match the parent's LeadAgentID.
	if parentStatus.Config != nil && parentStatus.Config.LeadAgentID != "" {
		if req.CallerAgentID != parentStatus.Config.LeadAgentID {
			return nil, &NotLeadOfParentError{
				ParentJobID:   req.ParentJobID,
				CallerAgentID: req.CallerAgentID,
				LeadAgentID:   parentStatus.Config.LeadAgentID,
			}
		}
	}

	// Build the description for the child job from inline task specs.
	description := "child of " + req.ParentJobID
	if len(req.Tasks) > 0 {
		description = req.Tasks[0].Description
		if len(req.Tasks) > 1 {
			description += " (+ " + itoa(len(req.Tasks)-1) + " more tasks)"
		}
	}

	childReq := &StartJobRequest{
		TenantID:     tenantID,
		Description:  description,
		ProjectID:    req.ProjectID,
		WorkflowName: req.WorkflowID,
	}

	resp, startErr := s.StartJob(ctx, childReq)
	if startErr != nil {
		return nil, startErr
	}

	childJobID := resp.JobID

	// Tag the child job with parent lineage via its persisted status.
	// We update the KV store directly since the job has already started.
	if s.kvStore != nil {
		if childStatus, loadErr := s.loadJobStatus(tenantID, childJobID); loadErr == nil {
			childStatus.ParentJobID = req.ParentJobID
			childStatus.SpawnedBy = req.CallerAgentID
			childStatus.SpawnedAt = time.Now().UnixNano()
			s.persistJobStatus(tenantID, childStatus)
		}
	}

	// Atomically append the child ID to the parent's ChildJobIDs.
	s.appendChildJobID(ctx, tenantID, req.ParentJobID, childJobID)

	s.logger.Infof("orchestration: spawned child job %s from parent %s by agent %s",
		childJobID, req.ParentJobID, req.CallerAgentID)

	return &SpawnChildJobResponse{ChildJobID: childJobID}, nil
}

// appendChildJobID atomically appends childID to the parent job's ChildJobIDs.
// It updates both the in-memory runningJob status and the KV store.
func (s *agentServiceImpl) appendChildJobID(ctx context.Context, tenantID, parentJobID, childJobID string) {
	jobKey := tenantID + "/" + parentJobID

	// Update in-memory running job if present.
	if val, ok := s.jobs.Load(jobKey); ok {
		if rj, ok := val.(*runningJob); ok {
			if st := rj.loop.Status(); st != nil {
				st.ChildJobIDs = append(st.ChildJobIDs, childJobID)
			}
		}
	}

	// Persist to KV store for durability and cross-restart visibility.
	if s.kvStore == nil {
		return
	}
	if parentStatus, err := s.loadJobStatus(tenantID, parentJobID); err == nil {
		parentStatus.ChildJobIDs = append(parentStatus.ChildJobIDs, childJobID)
		s.persistJobStatus(tenantID, parentStatus)
	}
}

// SubmitTaskArtifacts delegates to the task store's artifact submission.
func (s *agentServiceImpl) SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []Artifact) error {
	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired, Message: "task store is required for artifact submission"}
	}
	return s.taskStore.SubmitTaskArtifacts(ctx, tenantID, taskID, artifacts)
}

// GetTaskArtifact retrieves a named artifact, resolving KV refs to bytes.
func (s *agentServiceImpl) GetTaskArtifact(ctx context.Context, tenantID, taskID, name string) (*Artifact, error) {
	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired, Message: "task store is required for artifact retrieval"}
	}
	return s.taskStore.GetTaskArtifact(ctx, tenantID, taskID, name)
}

// ListTaskArtifacts returns output artifact metadata for a task.
func (s *agentServiceImpl) ListTaskArtifacts(ctx context.Context, tenantID, taskID string) ([]Artifact, error) {
	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired, Message: "task store is required for artifact listing"}
	}
	return s.taskStore.ListTaskArtifacts(ctx, tenantID, taskID)
}

// SetSandboxPolicy sets the server-level policy that governs how
// SandboxModeDefault is resolved and which explicit SandboxMode values are
// permitted. Unset policies default to SandboxPolicyRequired.
func (s *agentServiceImpl) SetSandboxPolicy(policy SandboxPolicy) {
	s.sandboxPolicy = policy
}

// SetTenantPolicyProvider registers a resolver for per-tenant SandboxPolicy
// overrides. Passing nil removes the provider.
func (s *agentServiceImpl) SetTenantPolicyProvider(p TenantPolicyProvider) {
	s.tenantPolicies = p
}

// SetRemoteBackendResolver installs a resolver that routes daemon-kind
// registrations to their RemoteACPBackend before the locally registered
// backends are consulted. Passing nil disables remote routing.
func (s *agentServiceImpl) SetRemoteBackendResolver(r RemoteBackendResolver) {
	s.remoteResolver = r
}

// SetSessionEventLog installs the append-only session event log. When set,
// session lifecycle events (spawn, prompt submission, close) are appended
// for attacher consumption. Passing nil disables the log — useful for
// embedded deployments that don't need scrollback.
// SetProjectAuthz configures the project-level authorization helper. When set,
// ClaimTask enforces the project's AgentRoles matrix for scoped tasks.
// Unscoped tasks (empty ProjectID) bypass authorization unchanged.
func (s *agentServiceImpl) SetProjectAuthz(authz *ProjectAuthz) {
	s.projectAuthz = authz
}

func (s *agentServiceImpl) SetSessionEventLog(log SessionEventLog) {
	s.sessionLog = log
}

// SessionEventLog returns the currently-installed session event log, or
// nil when unset. Exposed so the REST/gRPC handlers can Range and Subscribe
// on the same instance the orchestrator appends to.
func (s *agentServiceImpl) SessionEventLog() SessionEventLog {
	return s.sessionLog
}

// appendSessionEvent persists a session event on behalf of the orchestrator
// control loop. Errors are logged but never propagated — append is a
// side effect of the main operation (spawn, prompt, close) and must not
// fail those operations on its own.
func (s *agentServiceImpl) appendSessionEvent(ctx context.Context, event *SessionEvent) {
	if s.sessionLog == nil {
		return
	}
	if err := s.sessionLog.Append(ctx, event); err != nil {
		s.logger.Warnf("orchestration: append session event %s seq=%d: %v", event.SessionID, event.Sequence, err)
	}
}

// nextSessionSequence returns the next sequence number to assign to an
// event appended on behalf of the orchestrator. Falls back to 1 on lookup
// error so the first event of a fresh session starts at 1 as expected.
func (s *agentServiceImpl) nextSessionSequence(ctx context.Context, tenantID, sessionID string) uint64 {
	if s.sessionLog == nil {
		return 0
	}
	latest, err := s.sessionLog.LatestSequence(ctx, tenantID, sessionID)
	if err != nil {
		return 1
	}
	return latest + 1
}

// sessionCreator extracts a friendly identifier for the operator who
// initiated this request. Used to stamp SessionMeta.CreatedBy so attachers
// see who started each session. Falls back to the tenant ID when no
// richer context exists; refined by downstream IAM scope propagation.
func sessionCreator(ctx context.Context) string {
	if creator, ok := ctx.Value(creatorContextKey{}).(string); ok && creator != "" {
		return creator
	}
	return TenantFromContext(ctx)
}

// creatorContextKey threads the calling operator's identity down into
// orchestrator handlers. Used by gRPC/REST interceptors; defined here so
// the orchestrator can read it without a cross-package dependency.
type creatorContextKey struct{}

// WithSessionCreator returns a context stamped with the operator's identity
// for SessionMeta.CreatedBy. Interceptors call this; downstream orchestrator
// code calls sessionCreator() to read it back.
func WithSessionCreator(ctx context.Context, operatorID string) context.Context {
	if operatorID == "" {
		return ctx
	}
	return context.WithValue(ctx, creatorContextKey{}, operatorID)
}

// resolveSandboxMode turns a request's SandboxMode into the effective mode
// the backend should honor, given the server policy, any tenant override,
// and whether the orchestrator or its selected backend has access to a
// container runtime.
func (s *agentServiceImpl) resolveSandboxMode(
	ctx context.Context,
	req *SpawnSessionRequest,
	backendHasRuntime bool,
) (SandboxMode, error) {
	var tenantOverride *SandboxPolicy
	if s.tenantPolicies != nil {
		tenantOverride = s.tenantPolicies.SandboxPolicy(TenantFromContext(ctx))
	}
	hasRuntime := backendHasRuntime ||
		(s.containerRuntime != nil && s.containerRuntime.IsAvailable())
	return ResolveSandboxMode(req.EffectiveSandboxMode(), s.sandboxPolicy, tenantOverride, hasRuntime)
}

// validateTenant checks tenant access if the guard is configured.
// Returns nil if no guard is set (backward compatible).
func (s *agentServiceImpl) validateTenant(ctx context.Context) error {
	if s.tenantGuard == nil {
		return nil
	}
	tenantID := TenantFromContext(ctx)
	if tenantID == "" {
		return &TenantRequiredError{Operation: "orchestration"}
	}
	return s.tenantGuard.ValidateTenantAccess(ctx, tenantID)
}

// DiscoverProjects walks the root path to find agent-enabled projects.
func (s *agentServiceImpl) DiscoverProjects(ctx context.Context, req *DiscoverProjectsRequest) (*DiscoverProjectsResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.RootPath == "" {
		return nil, &AgentServiceError{Sentinel: ErrRootPathRequired}
	}

	info, err := os.Stat(req.RootPath)
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrRootPathRequired,
			Message:  "path does not exist: " + req.RootPath,
		}
	}
	if !info.IsDir() {
		return nil, &AgentServiceError{
			Sentinel: ErrRootPathRequired,
			Message:  "path is not a directory: " + req.RootPath,
		}
	}

	absRoot, err := filepath.Abs(req.RootPath)
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrRootPathRequired,
			Message:  "cannot resolve absolute path: " + err.Error(),
		}
	}

	var projects []*DiscoveredProject

	entries, err := os.ReadDir(absRoot)
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "cannot read directory: " + err.Error(),
		}
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(absRoot, entry.Name())
		project := s.scanProjectDir(dirPath, entry.Name())
		if project != nil {
			projects = append(projects, project)
		}
	}

	// Also check the root itself
	rootProject := s.scanProjectDir(absRoot, filepath.Base(absRoot))
	if rootProject != nil {
		projects = append(projects, rootProject)
	}

	return &DiscoverProjectsResponse{
		Projects:  projects,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// scanProjectDir checks a directory for CLAUDE.md and .claude/agents/ files.
func (s *agentServiceImpl) scanProjectDir(dirPath, name string) *DiscoveredProject {
	hasClaudeMD := false
	hasAgents := false
	var agentFiles []string

	// Check for CLAUDE.md
	claudeMDPath := filepath.Join(dirPath, "CLAUDE.md")
	if _, err := os.Stat(claudeMDPath); err == nil {
		hasClaudeMD = true
	}

	// Check for .claude/agents/ directory
	agentsDir := filepath.Join(dirPath, ".claude", "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				hasAgents = true
				agentFiles = append(agentFiles, filepath.Join(agentsDir, entry.Name()))
			}
		}
	}

	if !hasClaudeMD && !hasAgents {
		return nil
	}

	return &DiscoveredProject{
		Name:        name,
		Path:        dirPath,
		HasClaudeMD: hasClaudeMD,
		HasAgents:   hasAgents,
		AgentFiles:  agentFiles,
	}
}

// RegisterProject registers a project for agent orchestration.
func (s *agentServiceImpl) RegisterProject(ctx context.Context, req *RegisterProjectRequest) (*RegisterProjectResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, &AgentServiceError{Sentinel: ErrProjectNameRequired}
	}
	if req.Path == "" {
		return nil, &AgentServiceError{Sentinel: ErrProjectPathRequired}
	}

	projectID, err := generateID()
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to generate project ID: " + err.Error(),
		}
	}

	now := time.Now().UnixNano()

	project := &RegisteredProject{
		ProjectID:      projectID,
		Name:           req.Name,
		Path:           req.Path,
		Dependencies:   req.Dependencies,
		DefaultBackend: req.DefaultBackend,
		DefaultModel:   req.DefaultModel,
		Labels:         req.Labels,
		CreatedAt:      now,
	}

	s.projects.Store(projectID, project)

	s.logger.Debugf("registered project: id=%s name=%s path=%s", projectID, req.Name, req.Path)

	return &RegisterProjectResponse{
		ProjectID: projectID,
		Timestamp: now,
	}, nil
}

// ListProjects returns all registered projects sorted by name.
func (s *agentServiceImpl) ListProjects(ctx context.Context, req *ListProjectsRequest) (*ListProjectsResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	var projects []*RegisteredProject

	s.projects.Range(func(key, value interface{}) bool {
		if project, ok := value.(*RegisteredProject); ok {
			projects = append(projects, project)
		}
		return true
	})

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name < projects[j].Name
	})

	// Apply pagination if requested
	if req.PageSize > 0 && len(projects) > req.PageSize {
		startIdx := 0
		if req.PageToken != "" {
			for i, p := range projects {
				if p.ProjectID == req.PageToken {
					startIdx = i
					break
				}
			}
		}

		endIdx := startIdx + req.PageSize
		if endIdx > len(projects) {
			endIdx = len(projects)
		}

		nextToken := ""
		if endIdx < len(projects) {
			nextToken = projects[endIdx].ProjectID
		}

		return &ListProjectsResponse{
			Projects:      projects[startIdx:endIdx],
			NextPageToken: nextToken,
			Timestamp:     time.Now().UnixNano(),
		}, nil
	}

	return &ListProjectsResponse{
		Projects:  projects,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// SpawnSession creates a persistent agent session on a backend.
// When req.Sandboxed is true and a container runtime is configured,
// the session is launched inside a container. Otherwise the backend
// spawns the session directly.
func (s *agentServiceImpl) SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.Backend == "" {
		return nil, &AgentServiceError{Sentinel: ErrBackendRequired}
	}

	// Thread host-affinity and GPU requirement into the context so the
	// remote-backend resolver can filter candidate daemons accordingly.
	ctx = WithHostAffinity(ctx, req.HostAffinity)
	if req.GpuRequired {
		ctx = WithGpuRequired(ctx)
	}

	backend, err := s.lookupBackendCtx(ctx, req.Backend, req.ProjectID)
	if err != nil {
		return nil, err
	}

	// Resolve model: request > project default > empty (let backend decide)
	model := req.Model
	if model == "" && req.ProjectID != "" {
		if project, ok := s.lookupProject(req.ProjectID); ok {
			if model == "" {
				model = project.DefaultModel
			}
		}
	}

	// Resolve work directory: request > project path > current directory
	workDir := req.WorkDir
	if workDir == "" && req.ProjectID != "" {
		if project, ok := s.lookupProject(req.ProjectID); ok {
			workDir = project.Path
		}
	}
	if workDir == "" {
		workDir = "."
	}

	tenantID := TenantFromContext(ctx)
	now := time.Now().UnixNano()

	// Sandboxed container path — only used for backends that don't handle
	// containerization internally (e.g., non-ACP backends). ACP backends
	// with SetContainerRuntime handle their own container lifecycle.
	acpBackend, isACP := backend.(*ACPBackend)
	backendHandlesContainers := isACP && acpBackend.containerRuntime != nil

	// Resolve sandbox mode against server policy + tenant override + runtime
	// availability. This fails fast before any side effects (container spawn
	// or backend call), giving callers a deterministic policy violation error.
	effectiveMode, modeErr := s.resolveSandboxMode(ctx, req, backendHandlesContainers)
	if modeErr != nil {
		return nil, modeErr
	}

	if effectiveMode == SandboxModeRequired && !backendHandlesContainers && s.containerRuntime != nil && s.containerRuntime.IsAvailable() {
		containerID, spawnErr := s.spawnSandboxedSession(ctx, req, workDir, model)
		if spawnErr != nil {
			return nil, spawnErr
		}

		sessionID, idErr := generateID()
		if idErr != nil {
			return nil, &AgentServiceError{
				Sentinel: ErrAgentServiceUnavailable,
				Message:  "failed to generate session ID: " + idErr.Error(),
			}
		}

		s.sessions.Store(sessionID, &sessionRecord{
			Backend:     req.Backend,
			ProjectID:   req.ProjectID,
			Model:       model,
			TenantID:    tenantID,
			ContainerID: containerID,
			CreatedAt:   now,
		})

		record := &SessionRecord{
			SessionID:    sessionID,
			ProjectID:    req.ProjectID,
			Backend:      req.Backend,
			Model:        model,
			WorkDir:      workDir,
			Status:       SessionStateActive,
			CreatedAt:    now,
			LastActivity: now,
		}
		if storeErr := s.store.PutSession(ctx, record); storeErr != nil {
			s.sessions.Delete(sessionID)
			return nil, &AgentServiceError{
				Sentinel: ErrAgentServiceUnavailable,
				Message:  "failed to persist session: " + storeErr.Error(),
			}
		}

		s.logger.Debugf("spawned sandboxed session: id=%s backend=%s container=%s",
			sessionID, req.Backend, truncateID(containerID))

		s.publishEvent(ctx, tenantID, "session_spawned", map[string]string{
			"session_id":   sessionID,
			"backend":      req.Backend,
			"container_id": containerID,
			"sandboxed":    "true",
		})

		return &SpawnSessionResponse{
			SessionID:   sessionID,
			Backend:     req.Backend,
			Model:       model,
			ContainerID: containerID,
			Timestamp:   now,
		}, nil
	}

	// Standard backend spawn path. Sandboxed is derived from the resolved
	// effective mode for legacy backends that still consult it.
	spawnReq := &SpawnRequest{
		ProjectID:      req.ProjectID,
		WorkDir:        workDir,
		Model:          model,
		MCPServers:     req.MCPServers,
		SystemPrompt:   req.SystemPrompt,
		Env:            req.Env,
		ExtraArgs:      req.ExtraArgs,
		Persistent:     req.Persistent,
		SandboxMode:    effectiveMode,
		Sandboxed:      effectiveMode == SandboxModeRequired,
		ContainerImage: req.ContainerImage,
	}

	session, err := backend.Spawn(ctx, spawnReq)
	if err != nil {
		return nil, err
	}

	// Track the session in memory for fast lookup
	s.sessions.Store(session.ID, &sessionRecord{
		Backend:   req.Backend,
		ProjectID: req.ProjectID,
		Model:     model,
		TenantID:  tenantID,
		CreatedAt: session.CreatedAt,
	})

	// Persist session metadata to the store
	record := &SessionRecord{
		SessionID:    session.ID,
		ProjectID:    req.ProjectID,
		Backend:      req.Backend,
		Model:        model,
		WorkDir:      workDir,
		Status:       SessionStateActive,
		CreatedAt:    session.CreatedAt,
		LastActivity: now,
	}
	if err := s.store.PutSession(ctx, record); err != nil {
		// Roll back in-memory tracking on store failure
		s.sessions.Delete(session.ID)
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to persist session: " + err.Error(),
		}
	}

	s.logger.Debugf("spawned session: id=%s backend=%s project=%s", session.ID, req.Backend, req.ProjectID)

	s.publishEvent(ctx, tenantID, "session_spawned", map[string]string{
		"session_id": session.ID,
		"backend":    req.Backend,
	})

	// Register the session in the event log so attachers can list + scroll
	// back from seq=1. Log presence is optional; no-op when log is nil.
	if s.sessionLog != nil {
		_ = s.sessionLog.PutMeta(ctx, &SessionMeta{
			SessionID:    session.ID,
			TenantID:     tenantID,
			ProjectID:    req.ProjectID,
			Backend:      req.Backend,
			Model:        model,
			Status:       SessionStateActive,
			CreatedAt:    time.Unix(0, now).UTC(),
			CreatedBy:    sessionCreator(ctx),
			LastActivity: time.Unix(0, now).UTC(),
		})
		s.appendSessionEvent(ctx, &SessionEvent{
			Sequence:  s.nextSessionSequence(ctx, tenantID, session.ID),
			SessionID: session.ID,
			TenantID:  tenantID,
			Timestamp: time.Unix(0, now).UTC(),
			Source:    "system:orchestrator",
			Kind:      KindSystemMessage,
			System: &SystemMessagePayload{
				Level:   "info",
				Message: "session spawned",
				Code:    "session_spawned",
			},
		})
	}

	return &SpawnSessionResponse{
		SessionID: session.ID,
		Backend:   req.Backend,
		Model:     session.Model,
		Timestamp: now,
	}, nil
}

// spawnSandboxedSession builds a ContainerSpec and runs the container.
func (s *agentServiceImpl) spawnSandboxedSession(
	ctx context.Context,
	req *SpawnSessionRequest,
	workDir string,
	model string,
) (string, error) {
	// Use the provided container config, or build one from request fields.
	var spec *ContainerSpec
	if req.ContainerConfig != nil {
		spec = req.ContainerConfig
	} else {
		image := req.ContainerImage
		if image == "" {
			return "", &AgentServiceError{
				Sentinel: ErrContainerImageRequired,
				Message:  "sandboxed session requires a container image",
			}
		}
		spec = &ContainerSpec{
			Image:      image,
			Env:        req.Env,
			WorkDir:    workDir,
			AutoRemove: true,
			Labels: map[string]string{
				"quicbaas.backend": req.Backend,
				"quicbaas.model":   model,
			},
		}
	}

	// Add project directory as a mount if a work dir is set and no mounts configured.
	if workDir != "" && len(spec.Mounts) == 0 {
		spec.Mounts = []ContainerMount{{
			Source: workDir,
			Target: workDir,
		}}
	}

	containerID, err := s.containerRuntime.Run(ctx, spec)
	if err != nil {
		return "", err
	}

	return containerID, nil
}

// PromptSession sends a prompt to an agent session or backend.
func (s *agentServiceImpl) PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.Prompt == "" {
		return nil, &AgentServiceError{Sentinel: ErrPromptRequired}
	}

	// Determine which backend to use
	backendName := req.Backend
	if backendName == "" && req.SessionID != "" {
		// Find backend from session record
		if rec, ok := s.sessions.Load(req.SessionID); ok {
			if sr, ok := rec.(*sessionRecord); ok {
				backendName = sr.Backend
			}
		}
	}

	if backendName == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrBackendRequired,
			Message:  "either backend or session_id must be provided",
		}
	}

	backend, err := s.lookupBackendCtx(ctx, backendName, req.ProjectID)
	if err != nil {
		return nil, err
	}

	// Resolve model
	model := req.Model
	if model == "" && req.ProjectID != "" {
		if project, ok := s.lookupProject(req.ProjectID); ok {
			model = project.DefaultModel
		}
	}

	promptReq := &PromptRequest{
		SessionID:     req.SessionID,
		ProjectID:     req.ProjectID,
		Prompt:        req.Prompt,
		Model:         model,
		SystemPrompt:  req.SystemPrompt,
		EventCallback: req.EventCallback,
	}

	// Append a PromptSubmitted event BEFORE dispatching so attachers see
	// the prompt even if the backend call fails.
	tenantID := TenantFromContext(ctx)
	if s.sessionLog != nil && req.SessionID != "" {
		now := time.Now().UTC()
		s.appendSessionEvent(ctx, &SessionEvent{
			Sequence:  s.nextSessionSequence(ctx, tenantID, req.SessionID),
			SessionID: req.SessionID,
			TenantID:  tenantID,
			Timestamp: now,
			Source:    "operator:" + sessionCreator(ctx),
			Kind:      KindPromptSubmitted,
			Prompt: &PromptSubmittedPayload{
				Prompt:       req.Prompt,
				Model:        model,
				SystemPrompt: req.SystemPrompt,
			},
		})
	}

	resp, err := backend.Prompt(ctx, promptReq)
	if err != nil {
		return nil, err
	}

	// Update last activity in the store for persistent sessions
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = resp.SessionID
	}
	if sessionID != "" {
		if updateErr := s.store.UpdateSessionActivity(ctx, sessionID); updateErr != nil {
			s.logger.Warnf("failed to update session activity: session=%s err=%s", sessionID, updateErr.Error())
		}
	}

	// Append assistant output as a single AssistantChunk with stop_reason.
	// Backend-side streaming will eventually produce multiple chunks; for
	// non-streaming responses (claude/codex/opencode's current shape) we
	// still emit exactly one event with the full text.
	if s.sessionLog != nil && sessionID != "" {
		for _, block := range resp.Content {
			if block.Type != "text" || block.Text == "" {
				continue
			}
			s.appendSessionEvent(ctx, &SessionEvent{
				Sequence:  s.nextSessionSequence(ctx, tenantID, sessionID),
				SessionID: sessionID,
				TenantID:  tenantID,
				Timestamp: time.Now().UTC(),
				Source:    "agent:" + backendName,
				Kind:      KindAssistantChunk,
				AssistantChunk: &AssistantChunkPayload{
					Text:       block.Text,
					StopReason: resp.StopReason,
					Model:      resp.Model,
				},
			})
		}
	}

	return &PromptSessionResponse{
		SessionID:  resp.SessionID,
		Content:    resp.Content,
		Model:      resp.Model,
		StopReason: resp.StopReason,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

// Infer runs a stateless fleet-routed inference call against the named
// backend. The backend is resolved via the RemoteBackendResolver (which
// applies host-affinity and tenant filtering) so it follows the same
// routing path as Spawn and Prompt. The call is rejected with
// InferUnsupportedError when the resolved backend's Capabilities show
// PersistentSessions=true, since those backends require a Spawn first.
func (s *agentServiceImpl) Infer(ctx context.Context, req *InferRequest) (<-chan *InferChunk, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, &AgentServiceError{Sentinel: ErrBackendRequired, Message: "request is required"}
	}
	if req.Backend == "" {
		return nil, &AgentServiceError{Sentinel: ErrBackendRequired}
	}
	if req.Prompt == "" {
		return nil, &AgentServiceError{Sentinel: ErrPromptRequired}
	}

	// Thread host affinity and GPU requirement through the context so the
	// RemoteBackendResolver can filter candidate daemons accordingly.
	inferCtx := ctx
	if req.HostAffinity != "" {
		inferCtx = WithHostAffinity(inferCtx, req.HostAffinity)
	}
	if req.GpuRequired {
		inferCtx = WithGpuRequired(inferCtx)
	}

	backend, err := s.lookupBackendCtx(inferCtx, req.Backend, "")
	if err != nil {
		return nil, err
	}

	// RemoteACPBackend implements Infer natively; the remote daemon
	// enforces its own per-backend Infer-support check (Runtime.Infer
	// rejects Spawn-required backends). Local sessionless backends
	// (OllamaBackend) are wrapped: call Prompt and emit the result as a
	// single final chunk so callers have a uniform channel interface.
	type inferrer interface {
		Infer(ctx context.Context, req *InferRequest) (<-chan *InferChunk, error)
	}

	if r, ok := backend.(inferrer); ok {
		return r.Infer(inferCtx, req)
	}

	// Local-only path: reject session-based backends that require Spawn
	// first. Remote daemons never reach this branch because the remote
	// backend adapter implements Infer above.
	if backend.Capabilities().PersistentSessions {
		return nil, &InferUnsupportedError{Backend: req.Backend, Operation: "infer"}
	}

	// Local backend fallback: call Prompt and bridge the result to a channel.
	promptReq := &PromptRequest{
		Prompt:       req.Prompt,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
	}
	resp, err := backend.Prompt(inferCtx, promptReq)
	if err != nil {
		return nil, &InferBackendError{Op: "Prompt", Err: err}
	}

	ch := make(chan *InferChunk, 1)
	ch <- &InferChunk{
		Delta:      resp.Content,
		Model:      resp.Model,
		StopReason: resp.StopReason,
		Final:      true,
	}
	close(ch)
	return ch, nil
}

// SessionStatus returns the status of an active session.
// It first checks in-memory sessions for active backend connections,
// then falls back to the persistent store for session metadata.
func (s *agentServiceImpl) SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.SessionID == "" {
		return nil, &AgentServiceError{Sentinel: ErrSessionIDRequired}
	}

	// Try in-memory sessions first (active backend connections)
	backendName, err := s.findSessionBackend(req.SessionID)
	if err != nil {
		// Not in memory; check the persistent store
		record, storeErr := s.store.GetSession(ctx, req.SessionID)
		if storeErr != nil {
			// Return the original session-not-found error
			return nil, err
		}

		return &SessionStatusResponse{
			SessionID:    record.SessionID,
			State:        record.Status,
			LastActivity: record.LastActivity,
			Uptime:       0,
			Timestamp:    time.Now().UnixNano(),
		}, nil
	}

	backend, err := s.lookupBackendCtx(ctx, backendName, "")
	if err != nil {
		return nil, err
	}

	status, err := backend.Status(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	return &SessionStatusResponse{
		SessionID:    status.SessionID,
		State:        status.State,
		LastActivity: status.LastActivity,
		Uptime:       status.Uptime,
		Timestamp:    time.Now().UnixNano(),
	}, nil
}

// CloseSession terminates an active session.
func (s *agentServiceImpl) CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.SessionID == "" {
		return nil, &AgentServiceError{Sentinel: ErrSessionIDRequired}
	}

	backendName, err := s.findSessionBackend(req.SessionID)
	if err != nil {
		return nil, err
	}

	backend, err := s.lookupBackendCtx(ctx, backendName, "")
	if err != nil {
		return nil, err
	}

	if err := backend.Close(ctx, req.SessionID); err != nil {
		return nil, err
	}

	// If the session had a container, stop it.
	if val, ok := s.sessions.Load(req.SessionID); ok {
		if rec, ok := val.(*sessionRecord); ok && rec.ContainerID != "" {
			if s.containerRuntime != nil {
				if stopErr := s.containerRuntime.Stop(ctx, rec.ContainerID); stopErr != nil {
					s.logger.Warnf("failed to stop container for session %s: %s", req.SessionID, stopErr.Error())
				}
			}
		}
	}

	s.sessions.Delete(req.SessionID)

	// Update the persistent store to mark the session as closed
	if updateErr := s.store.UpdateSessionStatus(ctx, req.SessionID, SessionStateClosed); updateErr != nil {
		s.logger.Warnf("failed to update session status in store: session=%s err=%s", req.SessionID, updateErr.Error())
	}

	s.logger.Debugf("closed session: id=%s backend=%s", req.SessionID, backendName)

	tenantID := TenantFromContext(ctx)
	s.publishEvent(ctx, tenantID, "session_closed", map[string]string{
		"session_id": req.SessionID,
		"backend":    backendName,
	})

	// Append the terminal SessionClosed event so attachers see the lifecycle
	// end in their replay stream.
	if s.sessionLog != nil {
		now := time.Now().UTC()
		s.appendSessionEvent(ctx, &SessionEvent{
			Sequence:  s.nextSessionSequence(ctx, tenantID, req.SessionID),
			SessionID: req.SessionID,
			TenantID:  tenantID,
			Timestamp: now,
			Source:    "operator:" + sessionCreator(ctx),
			Kind:      KindSessionClosed,
			Closed: &SessionClosedPayload{
				Reason: "operator",
			},
		})
		if meta, metaErr := s.sessionLog.GetMeta(ctx, tenantID, req.SessionID); metaErr == nil {
			meta.Status = SessionStateClosed
			meta.LastActivity = now
			_ = s.sessionLog.PutMeta(ctx, meta)
		}
	}

	return &CloseSessionResponse{
		SessionID: req.SessionID,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// ListSessions returns all sessions, merging live in-memory state with
// persisted store records. In-memory sessions are treated as active;
// store records provide historical and metadata context.
// When a TenantGuard is configured, sessions are filtered by the
// requesting tenant.
func (s *agentServiceImpl) ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
	// ListSessions can work without tenant context (returns all sessions).
	// When tenant is provided, results are filtered to that tenant.
	requestingTenant := TenantFromContext(ctx)

	// Build a set of in-memory (live) session IDs
	liveSet := make(map[string]struct{})
	sessions := make([]*SessionInfo, 0)

	s.sessions.Range(func(key, value interface{}) bool {
		sessionID, ok := key.(string)
		if !ok {
			return true
		}
		rec, ok := value.(*sessionRecord)
		if !ok {
			return true
		}

		// Apply backend filter if specified
		if req.Backend != "" && rec.Backend != req.Backend {
			return true
		}

		// Apply tenant filter when guard is active
		if s.tenantGuard != nil && requestingTenant != "" && rec.TenantID != requestingTenant {
			return true
		}

		liveSet[sessionID] = struct{}{}

		info := &SessionInfo{
			SessionID: sessionID,
			ProjectID: rec.ProjectID,
			Backend:   rec.Backend,
			Model:     rec.Model,
			State:     SessionStateActive,
			CreatedAt: rec.CreatedAt,
		}

		// Enrich with store data if available
		if record, err := s.store.GetSession(ctx, sessionID); err == nil {
			info.LastActivity = record.LastActivity
		}

		sessions = append(sessions, info)
		return true
	})

	// Merge in persisted sessions not currently live
	records, err := s.store.ListSessions(ctx)
	if err != nil {
		s.logger.Warnf("failed to list sessions from store: %s", err.Error())
	} else {
		for _, record := range records {
			if _, isLive := liveSet[record.SessionID]; isLive {
				continue
			}

			// Apply backend filter
			if req.Backend != "" && record.Backend != req.Backend {
				continue
			}

			sessions = append(sessions, &SessionInfo{
				SessionID:    record.SessionID,
				ProjectID:    record.ProjectID,
				Backend:      record.Backend,
				Model:        record.Model,
				State:        record.Status,
				CreatedAt:    record.CreatedAt,
				LastActivity: record.LastActivity,
			})
		}
	}

	// Sort by creation time for deterministic output
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt < sessions[j].CreatedAt
	})

	return &ListSessionsResponse{
		Sessions:  sessions,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// DecomposeTask uses an agent to decompose a task into work items.
// When req.Backend is empty and a lead agent session is configured, the lead
// session is used for decomposition via PromptSession. When req.Backend is
// provided, the named backend is called directly.
func (s *agentServiceImpl) DecomposeTask(ctx context.Context, req *DecomposeTaskRequest) (*DecomposeTaskResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.Task == "" {
		return nil, &AgentServiceError{Sentinel: ErrTaskRequired}
	}

	// Build the decomposition prompt used by both paths.
	// Fetch available backends to include in the decomposition prompt.
	availableBackends, _ := s.ListBackends(ctx)
	prompt := buildDecompositionPrompt(req.Task, req.ProjectIDs, availableBackends...)

	// Lead agent session path: when no backend is specified and a persistent
	// lead session exists, route the decomposition through PromptSession.
	if req.Backend == "" && s.leadSessionID != "" {
		sessionResp, err := s.PromptSession(ctx, &PromptSessionRequest{
			SessionID:    s.leadSessionID,
			Prompt:       prompt,
			Model:        req.Model,
			SystemPrompt: "You are a project planning assistant. Respond ONLY with valid JSON.",
		})
		if err != nil {
			return nil, err
		}

		// Convert PromptSessionResponse to PromptResponse for shared parsing.
		backendResp := &PromptResponse{
			SessionID:  sessionResp.SessionID,
			Content:    sessionResp.Content,
			Model:      sessionResp.Model,
			StopReason: sessionResp.StopReason,
		}

		return s.buildDecomposeResponse(backendResp)
	}

	// Direct backend path: resolve backend name, defaulting to the first
	// available backend when none is specified.
	backendName := req.Backend
	if backendName == "" {
		backendName = s.defaultBackendName()
		if backendName == "" {
			return nil, &AgentServiceError{Sentinel: ErrBackendRequired}
		}
	}

	backend, err := s.lookupBackend(backendName)
	if err != nil {
		return nil, err
	}

	promptReq := &PromptRequest{
		Prompt:       prompt,
		Model:        req.Model,
		SystemPrompt: "You are a project planning assistant. Respond ONLY with valid JSON.",
	}

	resp, err := backend.Prompt(ctx, promptReq)
	if err != nil {
		return nil, err
	}

	return s.buildDecomposeResponse(resp)
}

// buildDecomposeResponse parses a backend prompt response into a
// DecomposeTaskResponse containing structured work items.
func (s *agentServiceImpl) buildDecomposeResponse(resp *PromptResponse) (*DecomposeTaskResponse, error) {
	workItems, err := parseWorkItems(resp)
	if err != nil {
		s.logger.Warnf("failed to parse decomposition response as JSON, returning raw: %s", err.Error())
		// Return a single work item with the raw response.
		workItems = []*WorkItem{{
			ID:          "raw-1",
			Title:       "Unparsed task",
			Description: extractTextContent(resp),
			Priority:    1,
		}}
	}

	return &DecomposeTaskResponse{
		WorkItems: workItems,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// BackendToolGuidance returns backend-specific tool usage instructions.
func (s *agentServiceImpl) BackendToolGuidance(backendName string) string {
	val, ok := s.backends.Load(backendName)
	if !ok {
		return ""
	}
	backend, ok := val.(AgentBackend)
	if !ok {
		return ""
	}
	return backend.ToolGuidance()
}

// ListBackendModels returns model names available on the named backend.
// Backends that implement ModelLister are queried directly; others return
// ErrBackendNotFound if unknown, or an empty slice when no lister is available.
func (s *agentServiceImpl) ListBackendModels(ctx context.Context, backendName string) ([]string, error) {
	backend, err := s.lookupBackendCtx(ctx, backendName, "")
	if err != nil {
		return nil, err
	}

	lister, ok := backend.(ModelLister)
	if !ok {
		// Backend exists but does not expose a model catalog.
		return []string{}, nil
	}

	return lister.ListModels(ctx)
}

// ListBackends returns all registered agent backends.
func (s *agentServiceImpl) ListBackends(ctx context.Context) ([]*AgentBackendInfo, error) {
	// ListBackends is a global operation - backends are not tenant-scoped.
	// No tenant validation required.

	backends := make([]*AgentBackendInfo, 0)

	s.backends.Range(func(key, value interface{}) bool {
		name, ok := key.(string)
		if !ok {
			return true
		}
		backend, ok := value.(AgentBackend)
		if !ok {
			return true
		}
		caps := backend.Capabilities()

		backends = append(backends, &AgentBackendInfo{
			Name:         name,
			Type:         backend.Type(),
			Capabilities: caps,
		})

		return true
	})

	// Sort by name for deterministic output
	sort.Slice(backends, func(i, j int) bool {
		return backends[i].Name < backends[j].Name
	})

	return backends, nil
}

// RegisterBackend adds a new agent backend to the service.
func (s *agentServiceImpl) RegisterBackend(ctx context.Context, req *RegisterBackendRequest) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if req.Name == "" {
		return &AgentServiceError{Sentinel: ErrBackendNameRequired}
	}
	if req.Backend == nil {
		return &AgentServiceError{Sentinel: ErrBackendNilRequired}
	}

	s.backends.Store(req.Name, req.Backend)

	s.logger.Debugf("registered backend: name=%s type=%s", req.Name, req.Backend.Type())

	return nil
}

// HealthCheck performs a health check on all active agent sessions by
// creating an ephemeral AgentHealthMonitor and delegating to CheckAll.
func (s *agentServiceImpl) HealthCheck(ctx context.Context) (*HealthCheckReport, error) {
	// HealthCheck is a global operation - not tenant-scoped.
	// No tenant validation required.

	monitor := NewAgentHealthMonitor(s, s.logger)
	report := monitor.CheckAll(ctx)
	return report, nil
}

// LoadPersistedSessions loads existing sessions from the store on startup.
// It iterates persisted session records and restores in-memory tracking for
// sessions that are still in an active state and have a registered backend.
func (s *agentServiceImpl) LoadPersistedSessions(ctx context.Context) error {
	records, err := s.store.ListSessions(ctx)
	if err != nil {
		return &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to load persisted sessions: " + err.Error(),
		}
	}

	restored := 0
	for _, record := range records {
		// Only restore active sessions
		if record.Status != SessionStateActive && record.Status != SessionStateIdle {
			continue
		}

		// Verify the backend is still registered
		if _, backendErr := s.lookupBackend(record.Backend); backendErr != nil {
			s.logger.Warnf("skipping persisted session %s: backend %s not available", record.SessionID, record.Backend)
			continue
		}

		s.sessions.Store(record.SessionID, &sessionRecord{
			Backend:   record.Backend,
			ProjectID: record.ProjectID,
			Model:     record.Model,
			CreatedAt: record.CreatedAt,
		})

		restored++
	}

	if restored > 0 {
		s.logger.Debugf("restored %d persisted sessions", restored)
	}

	return nil
}

// =============================================================================
// Task Management
// =============================================================================

// CreateTask creates a new orchestration task via the task store.
// It validates the tenant, generates a task ID, persists the task, and
// publishes lifecycle events to messaging and channel server when configured.
//
// When req.AutoSpawn is true, the task is immediately dispatched as a
// one-shot job on the best-matching agent. The task is always persisted first;
// if no matching agent exists, it is returned with empty SpawnedJobID and
// ErrNoMatchingAgent is returned.
func (s *agentServiceImpl) CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if req.TenantID == "" {
		return nil, &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if req.Subject == "" {
		return nil, &AgentServiceError{Sentinel: ErrTaskSubjectRequired}
	}

	taskID, err := GenerateTaskID()
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to generate task ID: " + err.Error(),
		}
	}

	// Merge scheduling hints into metadata so the task scheduler and
	// downstream consumers can read them without a schema change.
	meta := req.Metadata
	if req.HostAffinity != "" || req.GpuRequired || req.Backend != "" {
		if meta == nil {
			meta = make(map[string]string)
		}
		if req.HostAffinity != "" {
			meta["host_affinity"] = req.HostAffinity
		}
		if req.GpuRequired {
			meta["gpu_required"] = "true"
		}
		if req.Backend != "" {
			meta["backend"] = req.Backend
		}
	}

	task := &Task{
		ID:           taskID,
		TenantID:     req.TenantID,
		TeamName:     req.TeamName,
		Subject:      req.Subject,
		Description:  req.Description,
		AssignedTo:   req.AssignedTo,
		Dependencies: req.Dependencies,
		Metadata:     meta,
		Status:       TaskStatusPending,
		AutoSpawn:    req.AutoSpawn,
		ProjectID:    req.ProjectID,
	}

	if err := s.taskStore.CreateTask(ctx, task); err != nil {
		return nil, err
	}

	s.logger.Debugf("created task: id=%s tenant=%s subject=%s auto_spawn=%v", taskID, req.TenantID, req.Subject, req.AutoSpawn)

	s.publishEvent(ctx, req.TenantID, "task_created", map[string]string{
		"task_id":    taskID,
		"subject":    req.Subject,
		"auto_spawn": boolStr(req.AutoSpawn),
	})

	// When AutoSpawn is requested, dispatch the task immediately as a one-shot job.
	// The task is already persisted; spawn errors are returned to the caller while
	// the task remains queryable with empty SpawnedJobID.
	if req.AutoSpawn {
		spawnErr := s.autoSpawnTask(ctx, req, task)
		if spawnErr != nil {
			return task, spawnErr
		}
	}

	return task, nil
}

// autoSpawnTask dispatches a task immediately as a one-shot orchestration job.
// It selects the best matching agent using the same filters as the control loop
// (backend → gpu → host_affinity). If a matching agent is found, a StartJob call
// is issued, and the task is updated with SpawnedJobID and SpawnedAt. If no
// agent matches, the task is returned unchanged and ErrNoMatchingAgent is set.
// The task is always persisted before this is called, so callers always get
// a valid task back even when spawn fails.
func (s *agentServiceImpl) autoSpawnTask(ctx context.Context, req *CreateTaskRequest, task *Task) error {
	if s.agentRegistry == nil {
		return &NoMatchingAgentError{
			Constraint: "agent_registry",
			Detail:     "agent registry not configured for auto-spawn",
		}
	}

	// Build the work item for matchAgentForTask using request routing hints.
	item := &WorkItem{
		ID:          task.ID,
		Title:       task.Subject,
		Description: task.Description,
		Backend:     req.Backend,
		HostAffinity: req.HostAffinity,
	}
	if req.GpuRequired {
		item.RequiredCapabilities = []string{"gpu"}
	}

	// List all online agents for the tenant; online = not offline/error status.
	agents, listErr := s.agentRegistry.ListAgents(ctx, req.TenantID, &AgentFilter{})
	if listErr != nil {
		return &NoMatchingAgentError{
			Constraint: "agent_list",
			Detail:     "failed to list agents: " + listErr.Error(),
		}
	}

	// Filter to online agents only.
	online := make([]*AgentRegistration, 0, len(agents))
	for _, a := range agents {
		if a.Status != AgentStatusOffline && a.Status != AgentStatusError {
			online = append(online, a)
		}
	}

	// Use the control loop's matching logic (backend → gpu → host_affinity).
	// We instantiate a temporary loop purely for the matching helper.
	cl := NewControlLoop(s, s.logger)
	backends, _ := s.ListBackends(ctx)

	agentID, _, matchErr := cl.matchAgentForTask(item, online, backends)
	if matchErr != nil {
		return matchErr
	}
	if agentID == "" {
		return &NoMatchingAgentError{
			Constraint: "agent_selection",
			Detail:     "no online agents satisfy backend/gpu/host_affinity constraints",
		}
	}

	// Build the job description from the task.
	desc := task.Subject
	if task.Description != "" {
		desc = task.Subject + ": " + task.Description
	}

	jobReq := &StartJobRequest{
		TenantID:    req.TenantID,
		Description: desc,
		LeadBackend: req.Backend,
	}
	if req.GpuRequired {
		// Embed gpu hint so the lead job targets the right backend.
		jobReq.LeadBackend = req.Backend
	}

	jobResp, jobErr := s.StartJob(ctx, jobReq)
	if jobErr != nil {
		return &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "auto-spawn: failed to start job: " + jobErr.Error(),
		}
	}

	// Update the task with spawned job metadata.
	// task.Version is the version returned by CreateTask; UpdateTask expects
	// the caller to pass the current version and handles the increment itself.
	now := time.Now().UnixNano()
	task.SpawnedJobID = jobResp.JobID
	task.SpawnedAt = now

	if updateErr := s.taskStore.UpdateTask(ctx, task); updateErr != nil {
		// Non-fatal: task is valid, job is running. Log and continue.
		s.logger.Warnf("auto-spawn: task=%s job=%s spawned but task update failed: %s",
			task.ID, jobResp.JobID, updateErr.Error())
		// Reset in-memory fields so the returned task matches persisted state.
		task.SpawnedJobID = ""
		task.SpawnedAt = 0
	} else {
		s.logger.Infof("auto-spawn: task=%s job=%s dispatched to agent=%s", task.ID, jobResp.JobID, agentID)
	}

	// Subscribe to job events in a background goroutine and mirror job terminal
	// status back to the parent task when the job completes or fails.
	go s.mirrorJobStatusToTask(req.TenantID, task.ID, jobResp.JobID)

	s.publishEvent(ctx, req.TenantID, "task_auto_spawned", map[string]string{
		"task_id": task.ID,
		"job_id":  jobResp.JobID,
		"agent":   agentID,
	})

	return nil
}

// mirrorJobStatusToTask watches a job's event stream and updates the parent task
// to completed or failed when the job reaches a terminal state. It is launched
// as a goroutine by autoSpawnTask and runs until the job terminates or the
// subscription channel is closed.
func (s *agentServiceImpl) mirrorJobStatusToTask(tenantID, taskID, jobID string) {
	ctx := ContextWithTenant(context.Background(), tenantID)

	events, subErr := s.SubscribeJobEvents(ctx, tenantID, jobID)
	if subErr != nil {
		s.logger.Warnf("auto-spawn mirror: task=%s job=%s subscribe failed: %s", taskID, jobID, subErr.Error())
		return
	}

	for ev := range events {
		if !IsTerminalJobState(ev.State) {
			continue
		}

		switch ev.State {
		case JobStateCompleted:
			if err := s.taskStore.CompleteTask(ctx, tenantID, taskID, &TaskResult{
				Output: ev.Message,
				Metadata: map[string]string{
					"job_id":     jobID,
					"job_status": string(ev.State),
				},
			}); err != nil {
				s.logger.Warnf("auto-spawn mirror: task=%s job=%s complete failed: %s", taskID, jobID, err.Error())
			} else {
				s.logger.Debugf("auto-spawn mirror: task=%s marked completed from job=%s", taskID, jobID)
			}
		case JobStateFailed:
			if err := s.taskStore.FailTask(ctx, tenantID, taskID, "job "+jobID+" failed: "+ev.Message); err != nil {
				s.logger.Warnf("auto-spawn mirror: task=%s job=%s fail update failed: %s", taskID, jobID, err.Error())
			} else {
				s.logger.Debugf("auto-spawn mirror: task=%s marked failed from job=%s", taskID, jobID)
			}
		}

		s.publishEvent(ctx, tenantID, "task_auto_spawn_completed", map[string]string{
			"task_id":    taskID,
			"job_id":     jobID,
			"job_status": string(ev.State),
		})
		return
	}
}

// boolStr converts a bool to "true" or "false" for metadata maps.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// GetTask retrieves a task by ID within a tenant namespace.
func (s *agentServiceImpl) GetTask(ctx context.Context, tenantID, taskID string) (*Task, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return nil, &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if taskID == "" {
		return nil, &AgentServiceError{Sentinel: ErrTaskIDRequired}
	}

	return s.taskStore.GetTask(ctx, tenantID, taskID)
}

// ListTasks lists tasks matching the filter within a tenant.
func (s *agentServiceImpl) ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return nil, &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}

	return s.taskStore.ListTasks(ctx, tenantID, filter)
}

// ClaimTask atomically claims a task for an agent using compare-and-swap.
// When the task is scoped to a project (Task.ProjectID non-empty) and a
// ProjectAuthz helper is configured, the claiming agent must hold at least
// the worker role in that project. Agents with the denied or observer role,
// or agents absent from a scoped project's role matrix, are rejected with
// ProjectAccessDeniedError. Empty AgentRoles (legacy unscoped project) allows all.
func (s *agentServiceImpl) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if taskID == "" {
		return &AgentServiceError{Sentinel: ErrTaskIDRequired}
	}
	if agentID == "" {
		return &AgentServiceError{Sentinel: ErrAgentIDRequired}
	}

	// Enforce project-level access when the task is project-scoped.
	if s.projectAuthz != nil {
		task, err := s.taskStore.GetTask(ctx, tenantID, taskID)
		if err != nil {
			return err
		}
		if task.ProjectID != "" {
			if authzErr := s.projectAuthz.AuthorizeAgentForTenant(
				ctx, tenantID, task.ProjectID, agentID, ProjectRoleWorker,
			); authzErr != nil {
				return authzErr
			}
		}
	}

	if err := s.taskStore.ClaimTask(ctx, tenantID, taskID, agentID); err != nil {
		return err
	}

	s.logger.Debugf("claimed task: id=%s tenant=%s agent=%s", taskID, tenantID, agentID)

	s.publishEvent(ctx, tenantID, "task_claimed", map[string]string{
		"task_id":  taskID,
		"agent_id": agentID,
	})

	return nil
}

// CompleteTask marks a task as completed with a result.
func (s *agentServiceImpl) CompleteTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if taskID == "" {
		return &AgentServiceError{Sentinel: ErrTaskIDRequired}
	}

	if err := s.taskStore.CompleteTask(ctx, tenantID, taskID, result); err != nil {
		return err
	}

	s.logger.Debugf("completed task: id=%s tenant=%s", taskID, tenantID)

	s.publishEvent(ctx, tenantID, "task_completed", map[string]string{
		"task_id": taskID,
	})

	return nil
}

// =============================================================================
// Event Publishing
// =============================================================================

// publishEvent publishes an orchestration event to both the messaging service
// and the channel server when configured. Errors are logged but do not
// propagate to the caller since event publishing is best-effort.
func (s *agentServiceImpl) publishEvent(ctx context.Context, tenantID, eventType string, metadata map[string]string) {
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["type"] = eventType

	// Publish to messaging service
	if s.messaging != nil {
		event := struct {
			Type     string            `json:"type"`
			TenantID string            `json:"tenant_id"`
			Metadata map[string]string `json:"metadata"`
		}{
			Type:     eventType,
			TenantID: tenantID,
			Metadata: metadata,
		}
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			s.logger.Warnf("failed to marshal event: %s", marshalErr.Error())
		} else {
			channel := "orchestrator:events"
			if tenantID != "" {
				channel = "orchestrator:events:" + tenantID
			}
			if pubErr := s.messaging.Publish(ctx, channel, payload); pubErr != nil {
				s.logger.Warnf("failed to publish event: %s", pubErr.Error())
			}
		}
	}

	// Push to channel server
	if s.channelServer != nil {
		content := eventType
		if subject, ok := metadata["subject"]; ok {
			content = eventType + ": " + subject
		}
		if taskID, ok := metadata["task_id"]; ok && content == eventType {
			content = eventType + ": " + taskID
		}
		pushErr := s.channelServer.PushEvent(ctx, &ChannelEvent{
			Content:  content,
			Metadata: metadata,
		})
		if pushErr != nil {
			s.logger.Warnf("failed to push channel event: %s", pushErr.Error())
		}
	}

	// Fan-out to per-job event subscribers.
	jobID, hasJobID := metadata["job_id"]
	if hasJobID && jobID != "" {
		jobKey := tenantID + "/" + jobID
		if val, ok := s.eventSubscribers.Load(jobKey); ok {
			sl, ok := val.(*subscriberList)
			if !ok {
				return
			}
			evt := &JobEvent{
				JobID:     jobID,
				Type:      eventType,
				State:     JobState(metadata["state"]),
				Message:   metadata["details"],
				Iteration: 0,
				Metadata:  metadata,
				Timestamp: time.Now().UnixNano(),
			}
			for _, sub := range sl.subs {
				select {
				case sub.ch <- evt:
				default:
					// Drop if subscriber is slow; non-blocking.
				}
			}
		}
	}
}

// =============================================================================
// Job Persistence
// =============================================================================

// persistJobStatus writes a job status to the KV store. This is best-effort:
// if the KV write fails, the error is logged but not propagated. The in-memory
// state remains authoritative while the control loop is running.
func (s *agentServiceImpl) persistJobStatus(tenantID string, status *JobStatus) {
	if s.kvStore == nil {
		return
	}

	key := jobPersistenceKey(tenantID, status.JobID)
	data, err := json.Marshal(status)
	if err != nil {
		s.logger.Warnf("orchestration: failed to marshal job status for persistence: job=%s: %s",
			status.JobID, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if putErr := s.kvStore.Put(ctx, key, data); putErr != nil {
		s.logger.Warnf("orchestration: failed to persist job status: job=%s: %s",
			status.JobID, putErr.Error())
	}
}

// loadJobStatus reads a job status from the KV store by tenant and job ID.
func (s *agentServiceImpl) loadJobStatus(tenantID, jobID string) (*JobStatus, error) {
	if s.kvStore == nil {
		return nil, &JobPersistenceError{
			JobID:   jobID,
			Message: "KV store not configured",
		}
	}

	key := jobPersistenceKey(tenantID, jobID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := s.kvStore.Get(ctx, key)
	if err != nil {
		return nil, &JobPersistenceError{
			JobID:   jobID,
			Message: "failed to read from KV store",
			Cause:   err,
		}
	}

	var status JobStatus
	if unmarshalErr := json.Unmarshal(data, &status); unmarshalErr != nil {
		return nil, &JobPersistenceError{
			JobID:   jobID,
			Message: "failed to unmarshal job status",
			Cause:   unmarshalErr,
		}
	}

	return &status, nil
}

// recoverJobs scans the KV store for persisted jobs with non-terminal states.
// For each found, it attempts to resume the job from a safe checkpoint state
// using the persisted config, work items, and memory. If resumption fails,
// the job is marked as failed.
func (s *agentServiceImpl) recoverJobs(ctx context.Context) error {
	if s.kvStore == nil {
		return nil
	}

	prefix := "job:"
	var recoveredCount int

	scanErr := s.kvStore.Scan(ctx, prefix, func(key string, value []byte) error {
		var status JobStatus
		if unmarshalErr := json.Unmarshal(value, &status); unmarshalErr != nil {
			s.logger.Warnf("orchestration: skipping corrupted job record: key=%s: %s",
				key, unmarshalErr.Error())
			return nil
		}

		// Skip jobs that already reached a terminal state.
		if IsTerminalJobState(status.State) {
			return nil
		}

		// Attempt to resume the job from persisted state.
		if resumeErr := s.resumeJob(ctx, &status); resumeErr != nil {
			s.logger.Warnf("orchestration: failed to resume job %s, marking as failed: %s",
				status.JobID, resumeErr.Error())

			// Mark as failed since resumption failed.
			status.State = JobStateFailed
			status.Error = "failed to resume after restart: " + resumeErr.Error()
			status.UpdatedAt = time.Now().UnixNano()

			data, marshalErr := json.Marshal(&status)
			if marshalErr != nil {
				s.logger.Warnf("orchestration: failed to marshal failed job status: job=%s: %s",
					status.JobID, marshalErr.Error())
				return nil
			}

			if putErr := s.kvStore.Put(ctx, key, data); putErr != nil {
				s.logger.Warnf("orchestration: failed to persist failed job status: job=%s: %s",
					status.JobID, putErr.Error())
			}
			return nil
		}

		recoveredCount++
		return nil
	})

	if scanErr != nil {
		return &JobRecoveryError{
			Message: "failed to scan KV store for recoverable jobs",
			Cause:   scanErr,
		}
	}

	if recoveredCount > 0 {
		s.logger.Infof("orchestration: resumed %d interrupted jobs", recoveredCount)
	}

	return nil
}

// RecoverJobs is the exported entry point for job recovery on startup.
// It delegates to the internal recoverJobs method.
func (s *agentServiceImpl) RecoverJobs(ctx context.Context) error {
	return s.recoverJobs(ctx)
}

// resumeJob reconstructs a ControlLoop from persisted job status and launches
// it in a background goroutine from a safe checkpoint state. The checkpoint is
// determined by resolveResumeState, which maps the interrupted state to the
// nearest safe restart point. Persisted work items, config, and memory are
// restored so the resumed job has accumulated context from prior iterations.
func (s *agentServiceImpl) resumeJob(ctx context.Context, status *JobStatus) error {
	// Validate that we have enough persisted state to resume.
	if status.Config == nil {
		return &JobResumeError{
			JobID:   status.JobID,
			Message: "no config in persisted state",
		}
	}

	if status.JobID == "" {
		return &JobResumeError{
			Message: "no job ID in persisted state",
		}
	}

	config := status.Config

	// Determine safe restart state based on what was interrupted.
	resumeState := resolveResumeState(status.State)

	// If resolve returned a terminal state (e.g., escalating -> failed),
	// persist the terminal state and return without launching a goroutine.
	if IsTerminalJobState(resumeState) {
		status.State = resumeState
		status.UpdatedAt = time.Now().UnixNano()
		s.persistJobStatus(config.TenantID, status)
		s.logger.Infof("orchestration: job %s resolved to terminal state %s, not resuming",
			status.JobID, resumeState)
		return nil
	}

	// Create new ControlLoop with the same service and logger.
	loop := NewControlLoop(s, s.logger)

	// Restore accumulated context from persisted memory.
	if status.Memory != nil {
		loop.jobMemory = status.Memory
		// Rebuild dedup sets from the deserialized memory since they are
		// not serialized (unexported map fields).
		loop.jobMemory.RebuildDedupSets()
	}

	// Restore work items if resuming past decomposition.
	if len(status.WorkItems) > 0 {
		loop.workItems = status.WorkItems
	}

	// Restore iteration count so limits are respected.
	loop.iteration = status.Iteration

	// Load workflow if name is set and a loader is available.
	if status.WorkflowName != "" && s.workflowLoader != nil {
		wf, wfErr := s.workflowLoader.Get(status.WorkflowName)
		if wfErr != nil {
			s.logger.Warnf("orchestration: could not reload workflow %q for resumed job %s: %s",
				status.WorkflowName, status.JobID, wfErr.Error())
			// Not fatal: the job can still run without a workflow definition.
		} else {
			loop.SetWorkflow(wf)
		}
	}

	// Set persistence callback for durable job status storage.
	tenantID := config.TenantID
	loop.persistFn = func(st *JobStatus) {
		st.TenantID = tenantID
		s.persistJobStatus(tenantID, st)
	}

	// Set audit callback for event trail.
	loop.auditFn = func(eventType, details string) {
		s.publishEvent(ctx, tenantID, eventType, map[string]string{
			"details": details,
			"job_id":  status.JobID,
		})
	}

	// Create a cancellable context for the background goroutine.
	jobCtx, jobCancel := context.WithCancel(ctx)

	// Store the running job keyed by tenantID/jobID.
	jobKey := tenantID + "/" + status.JobID
	s.jobs.Store(jobKey, &runningJob{
		loop:   loop,
		cancel: jobCancel,
	})

	// Launch in background goroutine.
	go func() {
		loopErr := loop.RunFromState(jobCtx, config, status.JobID, resumeState)
		if loopErr != nil {
			s.logger.Warnf("orchestration: resumed job %s completed with error: %s",
				status.JobID, loopErr.Error())
		}
		jobCancel()
		// Clean up event subscribers after resumed job completes.
		s.cleanupJobSubscribers(tenantID, status.JobID)
	}()

	s.logger.Infof("orchestration: resumed job %s from state %s (original: %s)",
		status.JobID, resumeState, status.State)

	return nil
}

// =============================================================================
// Internal Helpers
// =============================================================================

// lookupBackend retrieves a backend by name from the locally registered set.
// Callers that have request context should prefer lookupBackendCtx so a
// remote-kind registration can preempt the local lookup.
func (s *agentServiceImpl) lookupBackend(name string) (AgentBackend, error) {
	val, ok := s.backends.Load(name)
	if !ok {
		return nil, &BackendNotFoundError{Backend: name}
	}
	backend, ok := val.(AgentBackend)
	if !ok {
		return nil, &BackendNotFoundError{Backend: name}
	}
	return backend, nil
}

// lookupBackendCtx resolves a backend for the given request context. It first
// consults any registered RemoteBackendResolver; if the resolver returns a
// backend, it is used. Otherwise the locally registered backend table is
// consulted. This is the routing primitive used by the BackendDispatcher.
func (s *agentServiceImpl) lookupBackendCtx(ctx context.Context, name, projectID string) (AgentBackend, error) {
	if s.remoteResolver != nil {
		if backend, ok := s.remoteResolver(ctx, name, projectID); ok && backend != nil {
			return backend, nil
		}
	}
	return s.lookupBackend(name)
}

// defaultBackendName returns the name of the default backend to use when none
// is specified. Prefers the lead agent backend if configured, then "ollama",
// then falls back to the first registered backend. Returns empty if no
// backends are registered.
func (s *agentServiceImpl) defaultBackendName() string {
	if s.leadAgent != nil && s.leadAgent.Backend != "" {
		return s.leadAgent.Backend
	}
	if _, ok := s.backends.Load("ollama"); ok {
		return "ollama"
	}
	var first string
	s.backends.Range(func(key, _ interface{}) bool {
		first, _ = key.(string)
		return false // stop after first
	})
	return first
}

// lookupProject retrieves a project by ID.
func (s *agentServiceImpl) lookupProject(projectID string) (*RegisteredProject, bool) {
	val, ok := s.projects.Load(projectID)
	if !ok {
		return nil, false
	}
	proj, ok := val.(*RegisteredProject)
	if !ok {
		return nil, false
	}
	return proj, true
}

// findSessionBackend finds which backend owns a session.
func (s *agentServiceImpl) findSessionBackend(sessionID string) (string, error) {
	val, ok := s.sessions.Load(sessionID)
	if !ok {
		return "", &SessionNotFoundError{SessionID: sessionID}
	}
	rec, ok := val.(*sessionRecord)
	if !ok {
		return "", &SessionNotFoundError{SessionID: sessionID}
	}
	return rec.Backend, nil
}

// generateID produces a random hex-encoded ID.
func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// buildDecompositionPrompt creates a structured prompt for task decomposition.
// When backends are provided, the prompt includes available backends and their
// capabilities so the AI can assign work items to appropriate backends.
func buildDecompositionPrompt(task string, projectIDs []string, backends ...*AgentBackendInfo) string {
	var sb strings.Builder
	sb.WriteString("Decompose the following task into discrete work items.\n\n")
	sb.WriteString("Task: ")
	sb.WriteString(task)
	sb.WriteString("\n\n")

	if len(projectIDs) > 0 {
		sb.WriteString("Target projects: ")
		sb.WriteString(strings.Join(projectIDs, ", "))
		sb.WriteString("\n\n")
	}

	if len(backends) > 0 {
		sb.WriteString("Available backends:\n")
		for _, b := range backends {
			sb.WriteString("  - ")
			sb.WriteString(b.Name)
			sb.WriteString(" (type: ")
			sb.WriteString(b.Type)
			sb.WriteString(", capabilities: ")
			caps := make([]string, 0, 5)
			if b.Capabilities.PersistentSessions {
				caps = append(caps, "persistent_sessions")
			}
			if b.Capabilities.ToolUse {
				caps = append(caps, "tool_use")
			}
			if b.Capabilities.FileAccess {
				caps = append(caps, "file_access")
			}
			if b.Capabilities.Streaming {
				caps = append(caps, "streaming")
			}
			if b.Capabilities.ACP {
				caps = append(caps, "acp")
			}
			if len(caps) == 0 {
				sb.WriteString("none")
			} else {
				sb.WriteString(strings.Join(caps, ", "))
			}
			sb.WriteString(")\n")
		}
		sb.WriteString("\n")
		sb.WriteString("Assign each work item to the most appropriate backend. ")
		sb.WriteString("Use the \"backend\" field to specify the backend name. ")
		sb.WriteString("Use \"required_capabilities\" to list capabilities the work item needs.\n\n")
	}

	sb.WriteString(`Respond with a JSON array of work items:
[
  {
    "id": "unique-id",
    "project_id": "target-project-id",
    "title": "Short title",
    "description": "Detailed description",
    "priority": 1,
    "dependencies": ["id-of-dependency"],
    "backend": "backend-name",
    "required_capabilities": ["persistent_sessions"]
  }
]`)

	return sb.String()
}

// parseWorkItems extracts work items from a prompt response.
func parseWorkItems(resp *PromptResponse) ([]*WorkItem, error) {
	text := extractTextContent(resp)
	if text == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "empty response from backend",
		}
	}

	// Try to extract JSON from the response
	jsonStr := extractJSON(text)
	if jsonStr == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "no JSON found in response",
		}
	}

	var items []*WorkItem
	if err := json.Unmarshal([]byte(jsonStr), &items); err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to parse work items JSON: " + err.Error(),
		}
	}

	return items, nil
}

// extractTextContent concatenates all text blocks from a prompt response.
func extractTextContent(resp *PromptResponse) string {
	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}

// extractJSON finds the first JSON array in a string.
func extractJSON(text string) string {
	start := strings.Index(text, "[")
	if start == -1 {
		return ""
	}

	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}

	return ""
}

// RegisterAgent registers an agent with the fleet registry.
func (s *agentServiceImpl) RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*RegisterAgentResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if s.agentRegistry == nil {
		return nil, &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	if req.Name == "" {
		return nil, &AgentServiceError{Sentinel: ErrAgentNameRequired}
	}
	if req.Host == "" {
		return nil, &AgentServiceError{Sentinel: ErrAgentHostRequired}
	}
	if req.AgentType == "" {
		return nil, &AgentServiceError{Sentinel: ErrAgentTypeRequired}
	}

	id, err := generateID()
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to generate agent ID: " + err.Error(),
		}
	}

	agent := &AgentRegistration{
		ID:           id,
		TenantID:     req.TenantID,
		Name:         req.Name,
		Host:         req.Host,
		Backend:      req.Backend,
		Model:        req.Model,
		AgentType:    req.AgentType,
		Capabilities: req.Capabilities,
		WorkDir:      req.WorkDir,
		Status:       AgentStatusRegistered,
		Metadata:     req.Metadata,
	}

	if err := s.agentRegistry.RegisterAgent(ctx, agent); err != nil {
		return nil, err
	}

	s.logger.Infof("registered agent: id=%s tenant=%s name=%s host=%s type=%s",
		agent.ID, agent.TenantID, agent.Name, agent.Host, agent.AgentType)

	s.publishEvent(ctx, agent.TenantID, "agent_registered", map[string]string{
		"agent_id":   agent.ID,
		"agent_name": agent.Name,
		"host":       agent.Host,
		"agent_type": agent.AgentType,
	})

	return &RegisterAgentResponse{Agent: agent}, nil
}

// GetAgent retrieves a registered agent by ID.
func (s *agentServiceImpl) GetAgent(ctx context.Context, tenantID, agentID string) (*AgentRegistration, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if s.agentRegistry == nil {
		return nil, &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	return s.agentRegistry.GetAgent(ctx, tenantID, agentID)
}

// ListAgents lists all registered agents for a tenant.
func (s *agentServiceImpl) ListAgents(ctx context.Context, tenantID string, filter *AgentFilter) ([]*AgentRegistration, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if s.agentRegistry == nil {
		return nil, &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	return s.agentRegistry.ListAgents(ctx, tenantID, filter)
}

// RemoveAgent removes an agent from the fleet registry.
func (s *agentServiceImpl) RemoveAgent(ctx context.Context, tenantID, agentID string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}
	if s.agentRegistry == nil {
		return &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	if err := s.agentRegistry.RemoveAgent(ctx, tenantID, agentID); err != nil {
		return err
	}

	s.logger.Infof("removed agent: tenant=%s agent=%s", tenantID, agentID)

	s.publishEvent(ctx, tenantID, "agent_removed", map[string]string{
		"agent_id": agentID,
	})

	return nil
}

// AgentHeartbeat updates an agent's last seen timestamp.
func (s *agentServiceImpl) AgentHeartbeat(ctx context.Context, tenantID, agentID string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}
	if s.agentRegistry == nil {
		return &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	return s.agentRegistry.Heartbeat(ctx, tenantID, agentID)
}

// =============================================================================
// Job Orchestration (Phase 3 - stubs for interface compliance)
// =============================================================================

// StartJob starts an autonomous orchestration control loop.
// It creates a ControlLoop, launches it in a goroutine, and stores a reference
// in the jobs map for status polling and cancellation.
func (s *agentServiceImpl) StartJob(ctx context.Context, req *StartJobRequest) (*StartJobResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "start job request must not be nil",
		}
	}

	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = TenantFromContext(ctx)
	}
	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to start a job",
		}
	}

	if req.Description == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTaskRequired,
			Message:  "job description is required",
		}
	}

	// Parse poll interval, defaulting to 5s.
	var pollInterval time.Duration
	if req.PollInterval != "" {
		var parseErr error
		pollInterval, parseErr = time.ParseDuration(req.PollInterval)
		if parseErr != nil {
			return nil, &AgentServiceError{
				Sentinel: ErrInvalidInput,
				Message:  "invalid poll interval: " + parseErr.Error(),
			}
		}
	}

	maxIter := req.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	config := &JobConfig{
		TenantID:      tenantID,
		Description:   req.Description,
		LeadBackend:   req.LeadBackend,
		LeadModel:     req.LeadModel,
		LeadAgentID:   req.LeadAgentID,
		ProjectID:     req.ProjectID,
		PollInterval:  pollInterval,
		MaxIterations: maxIter,
		WorkflowName:  req.WorkflowName,
	}

	loop := NewControlLoop(s, s.logger)

	// Load workflow from file if a workflow file path is specified.
	// This always runs first so the workflow is registered in the loader
	// before name-based lookup below.
	if req.WorkflowFile != "" && s.workflowLoader != nil {
		wf, wfErr := s.workflowLoader.LoadFromFile(req.WorkflowFile)
		if wfErr == nil {
			if req.WorkflowName == "" {
				req.WorkflowName = wf.Name
			}
			s.logger.Infof("orchestration: loaded workflow from file %q (name=%s)", req.WorkflowFile, wf.Name)
		} else {
			s.logger.Warnf("orchestration: failed to load workflow file %q: %s", req.WorkflowFile, wfErr.Error())
		}
	}

	// Load workflow definition if a workflow name is specified.
	if req.WorkflowName != "" && s.workflowLoader != nil {
		wf, wfErr := s.workflowLoader.Get(req.WorkflowName)
		if wfErr != nil {
			return nil, &AgentServiceError{
				Sentinel: ErrInvalidInput,
				Message:  "workflow not found: " + req.WorkflowName,
			}
		}
		loop.SetWorkflow(wf)
		s.logger.Infof("orchestration: loaded workflow %q (mode=%s, agents=%d)",
			wf.Name, wf.Mode, len(wf.Agents))

		// If the request did not explicitly set lead backend/model,
		// derive them from the workflow's "lead" agent role.
		for i := range wf.Agents {
			if wf.Agents[i].Role == "lead" {
				if config.LeadBackend == "" {
					config.LeadBackend = wf.Agents[i].Backend
				}
				if config.LeadModel == "" {
					config.LeadModel = wf.Agents[i].Model
				}
				s.logger.Infof("orchestration: lead agent backend=%s model=%s",
					config.LeadBackend, config.LeadModel)
				break
			}
		}
	} else if req.WorkflowName != "" {
		s.logger.Warnf("orchestration: workflow %q requested but no workflow loader available", req.WorkflowName)
	} else if s.workflowLoader != nil && config.LeadBackend != "" {
		// Fallback: when no workflow name is provided (e.g., gRPC field not
		// propagated), search loaded workflows for one matching the lead backend/model.
		// Prefer workflows with more agent roles (more specific match) over
		// simpler workflows that happen to share the same lead backend.
		var bestMatch *WorkflowDefinition
		var bestScore int
		for _, wf := range s.workflowLoader.ListDefinitions() {
			for i := range wf.Agents {
				if wf.Agents[i].Role == "lead" &&
					wf.Agents[i].Backend == config.LeadBackend &&
					(config.LeadModel == "" || wf.Agents[i].Model == config.LeadModel) {
					// Score: number of agent roles (prefer workflows with more roles)
					score := len(wf.Agents)
					if score > bestScore {
						bestScore = score
						bestMatch = wf
					}
					break
				}
			}
		}
		if bestMatch != nil {
			loop.SetWorkflow(bestMatch)
			config.WorkflowName = bestMatch.Name
			s.logger.Infof("orchestration: auto-matched workflow %q from lead backend=%s model=%s (score=%d)",
				bestMatch.Name, config.LeadBackend, config.LeadModel, bestScore)
		}
	}

	// Set persistence callback for durable job status storage.
	if s.kvStore != nil {
		loop.persistFn = func(status *JobStatus) {
			status.TenantID = tenantID
			s.persistJobStatus(tenantID, status)
		}
	}

	// Create a cancellable context detached from the request context
	// so the job survives after the caller's request completes.
	jobCtx, cancel := context.WithCancel(context.Background())
	jobCtx = ContextWithTenant(jobCtx, tenantID)

	// jobID is set by the audit callback once the control loop assigns it.
	// Using atomic pointer so the callback always has the latest value.
	var jobIDRef atomic.Value
	jobIDRef.Store("")

	// Set audit callback for event trail via existing messaging/event system.
	// The callback captures jobIDRef by reference so it always includes the
	// current job ID, even for the earliest events fired before the ID is known.
	loop.auditFn = func(eventType, details string) {
		s.logger.Infof("orchestration: [%s] %s", eventType, details)
		currentJobID, _ := jobIDRef.Load().(string)
		s.publishEvent(jobCtx, tenantID, eventType, map[string]string{
			"details": details,
			"job_id":  currentJobID,
		})
	}

	// Start the control loop in a background goroutine.
	go func() {
		loopErr := loop.Run(jobCtx, config)
		if loopErr != nil {
			s.logger.Warnf("orchestration: job completed with error: %s", loopErr.Error())
		}
		// Cancel to release resources even on normal completion.
		cancel()
		// Clean up event subscribers after job completes.
		if status := loop.Status(); status != nil && status.JobID != "" {
			s.cleanupJobSubscribers(tenantID, status.JobID)
		}
	}()

	// Wait briefly for the job ID to be assigned by the control loop.
	// The Run method generates the job ID synchronously before entering
	// the state machine loop, so it is available almost immediately.
	waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
	defer waitCancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var jobID string
	for jobID == "" {
		select {
		case <-waitCtx.Done():
			cancel()
			return nil, &AgentServiceError{
				Sentinel: ErrControlLoopFailed,
				Message:  "timed out waiting for job ID assignment",
			}
		case <-ticker.C:
			status := loop.Status()
			if status != nil && status.JobID != "" {
				jobID = status.JobID
				jobIDRef.Store(jobID)
			}
		}
	}

	// Store the running job keyed by tenantID/jobID.
	jobKey := tenantID + "/" + jobID
	s.jobs.Store(jobKey, &runningJob{
		loop:   loop,
		cancel: cancel,
	})

	return &StartJobResponse{
		JobID:     jobID,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// GetJobStatus returns the current status of a running orchestration job.
func (s *agentServiceImpl) GetJobStatus(ctx context.Context, tenantID, jobID string) (*JobStatus, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to get job status",
		}
	}
	if jobID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTaskIDRequired,
			Message:  "job ID is required to get job status",
		}
	}

	jobKey := tenantID + "/" + jobID
	val, ok := s.jobs.Load(jobKey)
	if ok {
		job, castOk := val.(*runningJob)
		if !castOk {
			return nil, &AgentServiceError{
				Sentinel: ErrControlLoopFailed,
				Message:  "invalid job record type",
			}
		}

		status := job.loop.Status()
		if status == nil {
			return nil, &AgentServiceError{
				Sentinel: ErrControlLoopFailed,
				Message:  "job status not yet available",
			}
		}

		return status, nil
	}

	// Fallback: job not in memory (finished and cleaned up), try KV store.
	if s.kvStore != nil {
		status, kvErr := s.loadJobStatus(tenantID, jobID)
		if kvErr != nil {
			s.logger.Warnf("orchestration: KV fallback for job %s failed: %s", jobID, kvErr.Error())
			return nil, &JobNotFoundError{JobID: jobID}
		}
		return status, nil
	}

	return nil, &JobNotFoundError{JobID: jobID}
}

// ListJobs returns all jobs for a tenant, sorted by start time descending (most recent first).
// It merges in-memory running jobs with persisted completed/failed jobs from the KV store.
func (s *agentServiceImpl) ListJobs(ctx context.Context, tenantID string) ([]*JobStatus, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to list jobs",
		}
	}

	// Collect jobs from in-memory running jobs map.
	seen := make(map[string]struct{})
	var jobs []*JobStatus

	prefix := tenantID + "/"
	s.jobs.Range(func(key, val interface{}) bool {
		keyStr, ok := key.(string)
		if !ok || !strings.HasPrefix(keyStr, prefix) {
			return true
		}

		job, ok := val.(*runningJob)
		if !ok {
			return true
		}

		status := job.loop.Status()
		if status == nil {
			return true
		}

		seen[status.JobID] = struct{}{}
		jobs = append(jobs, status)
		return true
	})

	// Merge persisted jobs from KV store.
	if s.kvStore != nil {
		kvPrefix := "job:" + tenantID + ":"
		scanErr := s.kvStore.Scan(ctx, kvPrefix, func(key string, value []byte) error {
			var status JobStatus
			if unmarshalErr := json.Unmarshal(value, &status); unmarshalErr != nil {
				s.logger.Warnf("orchestration: failed to unmarshal persisted job %s: %s", key, unmarshalErr.Error())
				return nil
			}
			if _, exists := seen[status.JobID]; !exists {
				jobs = append(jobs, &status)
			}
			return nil
		})
		if scanErr != nil {
			s.logger.Warnf("orchestration: KV scan for tenant %s jobs failed: %s", tenantID, scanErr.Error())
		}
	}

	// Sort by start time descending (most recent first).
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].StartedAt > jobs[j].StartedAt
	})

	return jobs, nil
}

// CancelJob cancels a running orchestration job and removes it from the jobs map.
func (s *agentServiceImpl) CancelJob(ctx context.Context, tenantID, jobID string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if tenantID == "" {
		return &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to cancel a job",
		}
	}
	if jobID == "" {
		return &AgentServiceError{
			Sentinel: ErrTaskIDRequired,
			Message:  "job ID is required to cancel a job",
		}
	}

	jobKey := tenantID + "/" + jobID
	val, ok := s.jobs.Load(jobKey)
	if !ok {
		return &JobNotFoundError{JobID: jobID}
	}

	job, ok := val.(*runningJob)
	if !ok {
		return &AgentServiceError{
			Sentinel: ErrControlLoopFailed,
			Message:  "invalid job record type",
		}
	}

	job.cancel()
	s.jobs.Delete(jobKey)

	// Clean up event subscribers for the cancelled job.
	s.cleanupJobSubscribers(tenantID, jobID)

	// Persist final cancelled status (best-effort).
	if s.kvStore != nil {
		cancelledStatus := &JobStatus{
			JobID:     jobID,
			TenantID:  tenantID,
			State:     JobStateFailed,
			Error:     "cancelled by user",
			UpdatedAt: time.Now().UnixNano(),
		}
		// Preserve existing status fields if available.
		if existing := job.loop.Status(); existing != nil {
			cancelledStatus.Iteration = existing.Iteration
			cancelledStatus.TaskCount = existing.TaskCount
			cancelledStatus.Completed = existing.Completed
			cancelledStatus.Failed = existing.Failed
			cancelledStatus.Description = existing.Description
			cancelledStatus.StartedAt = existing.StartedAt
		}
		s.persistJobStatus(tenantID, cancelledStatus)
	}

	return nil
}

// =============================================================================
// Job Event Subscriptions
// =============================================================================

// SubscribeJobEvents returns a channel of real-time events for a running job.
// The channel is closed when the context is cancelled or the job completes.
func (s *agentServiceImpl) SubscribeJobEvents(ctx context.Context, tenantID, jobID string) (<-chan *JobEvent, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to subscribe to job events",
		}
	}
	if jobID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrJobNotFound,
			Message:  "job ID is required",
		}
	}

	// Verify the job exists in memory or in the KV store.
	jobKey := tenantID + "/" + jobID
	if _, ok := s.jobs.Load(jobKey); !ok {
		if s.kvStore != nil {
			if _, kvErr := s.loadJobStatus(tenantID, jobID); kvErr != nil {
				return nil, &JobNotFoundError{JobID: jobID}
			}
		} else {
			return nil, &JobNotFoundError{JobID: jobID}
		}
	}

	sub := &eventSubscriber{
		ch:   make(chan *JobEvent, 64),
		done: make(chan struct{}),
	}

	// Add subscriber to the map using a CAS loop for lock-free append.
	for {
		val, loaded := s.eventSubscribers.Load(jobKey)
		var existing *subscriberList
		if loaded {
			existing, _ = val.(*subscriberList)
		}
		var oldSubs []*eventSubscriber
		if existing != nil {
			oldSubs = existing.subs
		}
		newList := &subscriberList{
			subs: make([]*eventSubscriber, len(oldSubs)+1),
		}
		copy(newList.subs, oldSubs)
		newList.subs[len(oldSubs)] = sub
		if loaded {
			if s.eventSubscribers.CompareAndSwap(jobKey, val, newList) {
				break
			}
		} else {
			if _, swapped := s.eventSubscribers.LoadOrStore(jobKey, newList); !swapped {
				break
			}
		}
	}

	// Clean up subscriber when context is cancelled or job completes.
	go func() {
		select {
		case <-ctx.Done():
		case <-sub.done:
		}
		s.removeSubscriber(jobKey, sub)
		close(sub.ch)
	}()

	return sub.ch, nil
}

// removeSubscriber removes a subscriber from the per-job subscriber list.
func (s *agentServiceImpl) removeSubscriber(jobKey string, target *eventSubscriber) {
	for {
		val, ok := s.eventSubscribers.Load(jobKey)
		if !ok {
			return
		}
		sl, ok := val.(*subscriberList)
		if !ok {
			return
		}
		var filtered []*eventSubscriber
		for _, sub := range sl.subs {
			if sub != target {
				filtered = append(filtered, sub)
			}
		}
		if len(filtered) == 0 {
			if s.eventSubscribers.CompareAndDelete(jobKey, val) {
				return
			}
		} else {
			newList := &subscriberList{subs: filtered}
			if s.eventSubscribers.CompareAndSwap(jobKey, val, newList) {
				return
			}
		}
	}
}

// cleanupJobSubscribers closes all subscriber done channels for a completed job
// and removes the subscriber list from the map.
func (s *agentServiceImpl) cleanupJobSubscribers(tenantID, jobID string) {
	jobKey := tenantID + "/" + jobID
	val, ok := s.eventSubscribers.LoadAndDelete(jobKey)
	if !ok {
		return
	}
	sl, ok := val.(*subscriberList)
	if !ok {
		return
	}
	for _, sub := range sl.subs {
		select {
		case <-sub.done:
		default:
			close(sub.done)
		}
	}
}
