//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Route Registration
// =============================================================================

// registerOrchestratorRoutes registers all orchestrator REST API routes.
func registerOrchestratorRoutes(s *Server, r chi.Router) {
	r.Route("/orchestrator", func(r chi.Router) {
		// Session management
		r.Post("/sessions", s.HandleOrchestratorSpawn)
		r.Post("/sessions/{sessionID}/prompt", s.HandleOrchestratorPrompt)
		r.Get("/sessions/{sessionID}", s.HandleOrchestratorSessionStatus)
		r.Delete("/sessions/{sessionID}", s.HandleOrchestratorCloseSession)
		r.Get("/sessions", s.HandleOrchestratorListSessions)
		r.Post("/sessions/{sessionID}/rotate-account", s.HandleOrchestratorRotateSessionAccount)

		// Session event stream (Phase 4). SSE-based live tail; the same
		// handler serves scrollback via ?from=N&follow=0.
		r.Get("/sessions/{sessionID}/events", s.HandleOrchestratorSessionEvents)

		// Task management
		r.Post("/tasks", s.HandleOrchestratorCreateTask)
		r.Get("/tasks/{taskID}", s.HandleOrchestratorGetTask)
		r.Get("/tasks", s.HandleOrchestratorListTasks)
		r.Post("/tasks/{taskID}/claim", s.HandleOrchestratorClaimTask)
		r.Post("/tasks/{taskID}/complete", s.HandleOrchestratorCompleteTask)
		r.Post("/tasks/decompose", s.HandleOrchestratorDecomposeTask)

		// Task artifact management
		r.Post("/tasks/{taskID}/artifacts", s.HandleOrchestratorSubmitArtifacts)
		r.Get("/tasks/{taskID}/artifacts", s.HandleOrchestratorListArtifacts)
		r.Get("/tasks/{taskID}/artifacts/{name}", s.HandleOrchestratorGetArtifact)

		// Job management
		r.Post("/jobs", s.HandleOrchestratorStartJob)
		r.Get("/jobs", s.HandleOrchestratorListJobs)
		r.Get("/jobs/{jobID}", s.HandleOrchestratorGetJobStatus)
		r.Delete("/jobs/{jobID}", s.HandleOrchestratorCancelJob)
		r.Get("/jobs/{jobID}/events", s.HandleOrchestratorSubscribeJobEvents)
		r.Post("/jobs/{jobID}/children", s.HandleOrchestratorSpawnChildJob)

		// Backend management
		r.Get("/backends", s.HandleOrchestratorListBackends)

		// Project discovery
		r.Post("/discover", s.HandleOrchestratorDiscover)

		// Webhook hook events
		r.Post("/hooks", s.HandleOrchestratorHookEvent)

		// Workflow management
		r.Get("/workflows", s.HandleOrchestratorListWorkflows)
		r.Post("/workflows", s.HandleOrchestratorCreateWorkflow)
		r.Get("/workflows/{name}", s.HandleOrchestratorGetWorkflow)
		r.Put("/workflows/{name}", s.HandleOrchestratorUpdateWorkflow)
		r.Delete("/workflows/{name}", s.HandleOrchestratorDeleteWorkflow)

		// Health
		r.Get("/health", s.HandleOrchestratorHealth)

		// Stateless fleet-routed inference (sessionless; targets Ollama-style backends)
		r.Post("/inference", s.HandleOrchestratorInference)

		// Agent fleet management
		r.Post("/agents", s.HandleOrchestratorRegisterAgent)
		r.Get("/agents", s.HandleOrchestratorListAgents)
		r.Get("/agents/{agentID}", s.HandleOrchestratorGetAgent)
		r.Delete("/agents/{agentID}", s.HandleOrchestratorRemoveAgent)
		r.Post("/agents/{agentID}/heartbeat", s.HandleOrchestratorAgentHeartbeat)
		r.Get("/agents/{agentID}/backends/{backendName}/models", s.HandleOrchestratorListBackendModels)

		// Agent lifecycle admin — disable/enable are co-located with the rest of
		// the /orchestrator/agents surface so the SDK (which always prepends
		// /api/v1/orchestrator) targets a path the server actually serves.
		r.Post("/agents/{agentID}/disable", s.HandleDisableAgent)
		r.Post("/agents/{agentID}/enable", s.HandleEnableAgent)
	})
}

// =============================================================================
// Service Accessor
// =============================================================================

// orchestratorServiceProvider defines the method to get the agent service.
type orchestratorServiceProvider interface {
	Agent() orchestration.AgentService
}

// getAgentService retrieves the orchestration AgentService from the server's
// QuicBaaS service. Returns nil and writes a 503 response if the agent service
// is not available.
func (s *Server) getAgentService(w http.ResponseWriter, r *http.Request) orchestration.AgentService {
	provider, ok := s.service.(orchestratorServiceProvider)
	if !ok {
		RespondServiceUnavailable(w, r, "orchestrator service not available")
		return nil
	}
	agentSvc := provider.Agent()
	if agentSvc == nil {
		RespondServiceUnavailable(w, r, "orchestrator service not available")
		return nil
	}
	return agentSvc
}

// resolveOrchestratorTenant derives the tenant an orchestrator request operates
// within and stamps it onto the returned context via
// orchestration.ContextWithTenant. It is the single tenant-derivation point for
// every orchestrator REST handler so the orchestrator's tenant guard
// (orchestration.agentServiceImpl.validateTenant) observes a non-empty tenant on
// every code path — the in-process service call and, transitively, the
// embedded-agent gRPC backend call.
//
// Precedence:
//  1. explicit ?tenant_id= query parameter (operator override / cross-tenant
//     control-plane calls),
//  2. the authenticated token's tenant_id claim (the verified
//     platformtoken.AuthorizedContext attached by the authz middleware),
//  3. qrdbsdk.SystemTenantID as the canonical fallback when the request carries
//     neither — preserving the historical single-tenant default.
//
// It returns the derived tenant alongside the enriched context so callers that
// also pass the tenant as an explicit argument (task / job / agent methods)
// stay consistent with the context tenant.
func resolveOrchestratorTenant(r *http.Request) (context.Context, string) {
	ctx := r.Context()

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		if ac := AuthorizedContextFromRequest(ctx); ac != nil && ac.TenantID != "" {
			tenantID = ac.TenantID
		}
	}
	if tenantID == "" {
		tenantID = qrdbsdk.SystemTenantID
	}

	return orchestration.ContextWithTenant(ctx, tenantID), tenantID
}

// =============================================================================
// Session Management Handlers
// =============================================================================

// HandleOrchestratorSpawn handles POST /api/v1/orchestrator/sessions - Spawn a new agent session.
func (s *Server) HandleOrchestratorSpawn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req orchestration.SpawnSessionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Backend == "" {
		RespondBadRequest(w, r, "backend is required")
		return
	}

	// The /orchestrator/sessions endpoint creates conversational, multi-turn
	// sessions (the Sessions UI). These MUST be persistent so CLI-mode backends
	// register a durable session that Prompt/Status/Close can resolve; a
	// one-shot spawn registers no durable session and the immediate prompt
	// fails with "session not found". Default Persistent to true here when the
	// caller did not explicitly set it, so every transport that reaches the
	// orchestrator via REST gets correct multi-turn behavior. Callers that want
	// the stateless one-shot behavior use the Inference path (a separate
	// endpoint), or may opt out explicitly with "persistent": false. A JSON
	// bool cannot distinguish absent from explicit-false, so probe the raw body
	// for an explicit key before applying the default.
	if !spawnRequestHasExplicitPersistent(body) {
		req.Persistent = true
	}

	// Thread tenant into the orchestrator context the same way every other
	// orchestrator handler does — the tenant guard reads it from ctx and
	// rejects the request otherwise. Derivation precedence (query param ->
	// authenticated token tenant -> SystemTenantID) is centralized in
	// resolveOrchestratorTenant so the whole orchestrator surface is consistent.
	ctx, _ = resolveOrchestratorTenant(r)

	// Per-request strict bit overrides the operator-level
	// orchestration.host_affinity.mode default: when set to true, an offline
	// host_affinity surfaces NoMatchingAgentError (400) instead of falling
	// through to BackendNotFoundError (404).
	if req.HostAffinityStrict {
		ctx = orchestration.WithHostAffinityStrict(ctx)
	}

	// Install a holder so the remote backend resolver can stash a typed
	// NoMatchingAgentError when strict mode rejects an offline host.
	holder := orchestration.NewResolveErrorHolder()
	ctx = orchestration.WithResolveErrorHolder(ctx, holder)

	resp, err := agentSvc.SpawnSession(ctx, &req)
	if err != nil {
		if resolveErr := holder.Err(); resolveErr != nil {
			handleOrchestratorError(w, r, resolveErr)
			return
		}
		handleOrchestratorError(w, r, err)
		return
	}

	RespondCreated(w, r, resp)
}

// spawnRequestHasExplicitPersistent reports whether the raw spawn request body
// carries an explicit "persistent" field. It distinguishes an absent field
// (apply the server default) from an explicit "persistent": false (honor the
// caller's opt-out) — a distinction a plain struct decode loses for bool zero
// values. A malformed body is treated as "not explicit"; the surrounding
// handler has already validated the JSON, so this only affects the default.
func spawnRequestHasExplicitPersistent(body []byte) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	_, ok := probe["persistent"]
	return ok
}

// HandleOrchestratorPrompt handles POST /api/v1/orchestrator/sessions/{sessionID}/prompt.
//
// The turn is asynchronous. The handler records the prompt and a turn_started
// marker on the session event log, dispatches the turn on a context detached
// from this request, and answers 202 Accepted with the turn ID — well inside
// the REST server's 25s request cap, which a real multi-minute agent turn
// could never fit under. The agent's output is delivered on the session event
// stream at GET /api/v1/orchestrator/sessions/{sessionID}/events, and the turn
// ends when a turn_completed or turn_failed event carrying the same turn ID
// appears there. A client that disconnects mid-turn does not cancel it.
func (s *Server) HandleOrchestratorPrompt(w http.ResponseWriter, r *http.Request) {
	ctx, _ := resolveOrchestratorTenant(r)

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		RespondBadRequest(w, r, "session ID is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req orchestration.PromptSessionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	req.SessionID = sessionID

	if req.Prompt == "" {
		RespondBadRequest(w, r, "prompt is required")
		return
	}

	ack, err := agentSvc.PromptSessionAsync(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusAccepted, ack)
}

// HandleOrchestratorSessionStatus handles GET /api/v1/orchestrator/sessions/{sessionID} - Get session status.
func (s *Server) HandleOrchestratorSessionStatus(w http.ResponseWriter, r *http.Request) {
	ctx, _ := resolveOrchestratorTenant(r)

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		RespondBadRequest(w, r, "session ID is required")
		return
	}

	resp, err := agentSvc.SessionStatus(ctx, &orchestration.SessionStatusRequest{
		SessionID: sessionID,
	})
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleOrchestratorCloseSession handles DELETE /api/v1/orchestrator/sessions/{sessionID} - Close a session.
func (s *Server) HandleOrchestratorCloseSession(w http.ResponseWriter, r *http.Request) {
	ctx, _ := resolveOrchestratorTenant(r)

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		RespondBadRequest(w, r, "session ID is required")
		return
	}

	resp, err := agentSvc.CloseSession(ctx, &orchestration.CloseSessionRequest{
		SessionID: sessionID,
	})
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleOrchestratorRotateSessionAccount handles
// POST /api/v1/orchestrator/sessions/{sessionID}/rotate-account — move a
// session onto a different pooled provider account.
//
// The same verb runs automatically when a turn fails on a classified provider
// limit; this is the operator's door to it. It authorizes as
// orchestrator:spawn (see pkg/api/rest/authz_pep_mapping.go): the resume path
// closes the agent process and reconstitutes it against a newly selected
// pooled credential, which is a spawn's authority and not a close's.
//
// The body is optional — an empty POST rotates with no reason and no hold.
func (s *Server) HandleOrchestratorRotateSessionAccount(w http.ResponseWriter, r *http.Request) {
	ctx, _ := resolveOrchestratorTenant(r)

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		RespondBadRequest(w, r, "session ID is required")
		return
	}

	rotator, err := orchestration.RotationServiceFrom(agentSvc, "rest")
	if err != nil {
		RespondError(w, r, http.StatusNotImplemented, err)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	req := &orchestration.RotateSessionAccountRequest{}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	// The path segment is the authority on which session moves; a body that
	// names a different one would make the PEP's path-scoped decision a lie.
	req.SessionID = sessionID

	result, err := rotator.RotateSessionAccount(ctx, req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, result)
}

// HandleOrchestratorListSessions handles GET /api/v1/orchestrator/sessions - List active sessions.
func (s *Server) HandleOrchestratorListSessions(w http.ResponseWriter, r *http.Request) {
	ctx, _ := resolveOrchestratorTenant(r)

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	backend := r.URL.Query().Get("backend")

	resp, err := agentSvc.ListSessions(ctx, &orchestration.ListSessionsRequest{
		Backend: backend,
	})
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Task Management Handlers
// =============================================================================

// HandleOrchestratorCreateTask handles POST /api/v1/orchestrator/tasks - Create a new task.
func (s *Server) HandleOrchestratorCreateTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req orchestration.CreateTaskRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Subject == "" {
		RespondBadRequest(w, r, "task subject is required")
		return
	}

	if req.TenantID == "" {
		req.TenantID = r.URL.Query().Get("tenant_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, req.TenantID)

	task, err := agentSvc.CreateTask(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondCreated(w, r, task)
}

// HandleOrchestratorGetTask handles GET /api/v1/orchestrator/tasks/{taskID} - Get a task.
func (s *Server) HandleOrchestratorGetTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondBadRequest(w, r, "task ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	task, err := agentSvc.GetTask(ctx, tenantID, taskID)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, task)
}

// HandleOrchestratorListTasks handles GET /api/v1/orchestrator/tasks - List tasks.
func (s *Server) HandleOrchestratorListTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	filter := &orchestration.TaskFilter{
		TeamName:   r.URL.Query().Get("team_name"),
		AssignedTo: r.URL.Query().Get("assigned_to"),
		CreatedBy:  r.URL.Query().Get("created_by"),
	}

	statusParam := r.URL.Query().Get("status")
	if statusParam != "" {
		filter.Status = orchestration.TaskStatus(statusParam)
	}

	tasks, err := agentSvc.ListTasks(ctx, tenantID, filter)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
	})
}

// OrchestratorClaimTaskRequest is the request body for claiming a task.
type OrchestratorClaimTaskRequest struct {
	AgentID string `json:"agent_id"`
}

// HandleOrchestratorClaimTask handles POST /api/v1/orchestrator/tasks/{taskID}/claim - Claim a task.
func (s *Server) HandleOrchestratorClaimTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondBadRequest(w, r, "task ID is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req OrchestratorClaimTaskRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.AgentID == "" {
		RespondBadRequest(w, r, "agent_id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if err := agentSvc.ClaimTask(ctx, tenantID, taskID, req.AgentID); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"task_id":  taskID,
		"agent_id": req.AgentID,
		"claimed":  true,
	})
}

// OrchestratorCompleteTaskRequest is the request body for completing a task.
type OrchestratorCompleteTaskRequest struct {
	Result *orchestration.TaskResult `json:"result"`
}

// HandleOrchestratorCompleteTask handles POST /api/v1/orchestrator/tasks/{taskID}/complete - Complete a task.
func (s *Server) HandleOrchestratorCompleteTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondBadRequest(w, r, "task ID is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req OrchestratorCompleteTaskRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if err := agentSvc.CompleteTask(ctx, tenantID, taskID, req.Result); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"task_id":   taskID,
		"completed": true,
	})
}

// HandleOrchestratorDecomposeTask handles POST /api/v1/orchestrator/tasks/decompose - Decompose a task into work items.
func (s *Server) HandleOrchestratorDecomposeTask(w http.ResponseWriter, r *http.Request) {
	ctx, _ := resolveOrchestratorTenant(r)

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req orchestration.DecomposeTaskRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Task == "" {
		RespondBadRequest(w, r, "task description is required")
		return
	}

	resp, err := agentSvc.DecomposeTask(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Job Management Handlers
// =============================================================================

// HandleOrchestratorStartJob handles POST /api/v1/orchestrator/jobs - Start an orchestration job.
func (s *Server) HandleOrchestratorStartJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req orchestration.StartJobRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Description == "" {
		RespondBadRequest(w, r, "description is required")
		return
	}

	if req.TenantID == "" {
		req.TenantID = r.URL.Query().Get("tenant_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, req.TenantID)

	resp, err := agentSvc.StartJob(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondCreated(w, r, resp)
}

// HandleOrchestratorGetJobStatus handles GET /api/v1/orchestrator/jobs/{jobID} - Get job status.
func (s *Server) HandleOrchestratorGetJobStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	jobID := chi.URLParam(r, "jobID")
	if jobID == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	jobStatus, err := agentSvc.GetJobStatus(ctx, tenantID, jobID)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, jobStatus)
}

// HandleOrchestratorListJobs handles GET /api/v1/orchestrator/jobs - List all jobs for a tenant.
func (s *Server) HandleOrchestratorListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	jobs, err := agentSvc.ListJobs(ctx, tenantID)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"jobs": jobs,
	})
}

// HandleOrchestratorCancelJob handles DELETE /api/v1/orchestrator/jobs/{jobID} - Cancel an orchestration job.
func (s *Server) HandleOrchestratorCancelJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	jobID := chi.URLParam(r, "jobID")
	if jobID == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if err := agentSvc.CancelJob(ctx, tenantID, jobID); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"job_id":    jobID,
		"cancelled": true,
	})
}

// HandleOrchestratorSubscribeJobEvents handles GET /api/v1/orchestrator/jobs/{jobID}/events
// Uses Server-Sent Events (SSE) for real-time event streaming.
func (s *Server) HandleOrchestratorSubscribeJobEvents(w http.ResponseWriter, r *http.Request) {
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	jobID := chi.URLParam(r, "jobID")
	if jobID == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx := orchestration.ContextWithTenant(r.Context(), tenantID)

	eventCh, err := agentSvc.SubscribeJobEvents(ctx, tenantID, jobID)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		RespondInternalError(w, r, stderrors.New("streaming not supported"))
		return
	}

	enc := json.NewEncoder(w)

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-eventCh:
			if !ok {
				// Channel closed, send completion event
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			fmt.Fprintf(w, "event: job_event\ndata: ")
			enc.Encode(evt)
			fmt.Fprintf(w, "\n")
			flusher.Flush()
		}
	}
}

// =============================================================================
// Backend Management Handlers
// =============================================================================

// HandleOrchestratorListBackends handles GET /api/v1/orchestrator/backends - List agent backends.
func (s *Server) HandleOrchestratorListBackends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	backends, err := agentSvc.ListBackends(ctx)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"backends": backends,
	})
}

// HandleOrchestratorListBackendModels handles
// GET /api/v1/orchestrator/agents/{agentID}/backends/{backendName}/models
//
// Returns the model names available on the named backend as seen from the
// specified agent. For Ollama this is the set of locally-pulled models;
// for ACP backends (claude, codex, opencode) it returns their curated catalog.
// The agentID is used as a host affinity hint when a remote backend resolver
// is active; it is ignored for locally-registered backends.
func (s *Server) HandleOrchestratorListBackendModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	backendName := chi.URLParam(r, "backendName")
	if backendName == "" {
		RespondBadRequest(w, r, "backend name is required")
		return
	}

	// Thread agent ID as host affinity so the remote backend resolver can
	// forward the request to the correct daemon when needed.
	agentID := chi.URLParam(r, "agentID")
	if agentID != "" && agentID != "any" {
		ctx = orchestration.WithHostAffinity(ctx, agentID)
	}

	// Default to the canonical tenant when the caller omits ?tenant_id=...;
	// the remote backend resolver requires a non-empty tenant ID to dispatch,
	// otherwise it bails out and the dispatcher falls through to local-only
	// lookup which doesn't have the agent's backends.
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		tenantID = qrdbsdk.SystemTenantID
	}
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	models, err := agentSvc.ListBackendModels(ctx, backendName)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	if models == nil {
		models = []string{}
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"backend": backendName,
		"models":  models,
		"count":   len(models),
	})
}

// =============================================================================
// Project Discovery Handlers
// =============================================================================

// HandleOrchestratorDiscover handles POST /api/v1/orchestrator/discover - Discover projects.
func (s *Server) HandleOrchestratorDiscover(w http.ResponseWriter, r *http.Request) {
	ctx, _ := resolveOrchestratorTenant(r)

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req orchestration.DiscoverProjectsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.RootPath == "" {
		RespondBadRequest(w, r, "root_path is required")
		return
	}

	resp, err := agentSvc.DiscoverProjects(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Hook Event Handler
// =============================================================================

// OrchestratorHookEventRequest is the unified hook event payload accepted by the
// orchestrator hooks endpoint. The event_type field determines how the event is
// processed.
type OrchestratorHookEventRequest struct {
	EventType string                    `json:"event_type"`
	TenantID  string                    `json:"tenant_id,omitempty"`
	TaskID    string                    `json:"task_id,omitempty"`
	AgentID   string                    `json:"agent_id,omitempty"`
	Task      *OrchestratorHookTaskData `json:"task,omitempty"`
	Result    *orchestration.TaskResult `json:"result,omitempty"`
	Metadata  map[string]string         `json:"metadata,omitempty"`
}

// OrchestratorHookTaskData carries task fields within a hook event.
type OrchestratorHookTaskData struct {
	Subject  string            `json:"subject,omitempty"`
	TeamName string            `json:"team_name,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// OrchestratorHookEventTypeError indicates an unknown or missing event type.
type OrchestratorHookEventTypeError struct {
	EventType string
}

// Error implements the error interface.
func (e *OrchestratorHookEventTypeError) Error() string {
	if e.EventType == "" {
		return "hook event error: event_type is required"
	}
	return "hook event error: unknown event_type: " + e.EventType
}

// hookEventHandler is the function signature for event-type-specific hook handlers.
type hookEventHandler func(s *Server, w http.ResponseWriter, r *http.Request, req *OrchestratorHookEventRequest)

// hookEventHandlers maps event types to their handler functions for O(1) dispatch.
var hookEventHandlers = map[string]hookEventHandler{
	"task.created":   handleHookEventTaskCreated,
	"task.completed": handleHookEventTaskCompleted,
	"teammate.idle":  handleHookEventTeammateIdle,
}

// HandleOrchestratorHookEvent handles POST /api/v1/orchestrator/hooks - Unified hook event receiver.
// It dispatches incoming hook events to the appropriate handler based on event_type.
func (s *Server) HandleOrchestratorHookEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		RespondBadRequest(w, r, "request body is required")
		return
	}

	var req OrchestratorHookEventRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.EventType == "" {
		RespondBadRequest(w, r, (&OrchestratorHookEventTypeError{}).Error())
		return
	}

	handler, ok := hookEventHandlers[req.EventType]
	if !ok {
		RespondBadRequest(w, r, (&OrchestratorHookEventTypeError{EventType: req.EventType}).Error())
		return
	}

	handler(s, w, r, &req)
}

// handleHookEventTaskCreated processes a task.created hook event by creating
// a new task through the AgentService.
func handleHookEventTaskCreated(s *Server, w http.ResponseWriter, r *http.Request, req *OrchestratorHookEventRequest) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	createReq := &orchestration.CreateTaskRequest{
		TenantID: req.TenantID,
	}

	if req.Task != nil {
		createReq.Subject = req.Task.Subject
		createReq.TeamName = req.Task.TeamName
		createReq.Metadata = req.Task.Metadata
	}

	// Merge top-level metadata if task metadata is not set.
	if createReq.Metadata == nil && req.Metadata != nil {
		createReq.Metadata = req.Metadata
	}

	// Mark the source as webhook in metadata.
	if createReq.Metadata == nil {
		createReq.Metadata = make(map[string]string)
	}
	if _, exists := createReq.Metadata["source"]; !exists {
		createReq.Metadata["source"] = "webhook"
	}

	ctx = orchestration.ContextWithTenant(ctx, req.TenantID)

	task, err := agentSvc.CreateTask(ctx, createReq)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusAccepted, task)
}

// handleHookEventTaskCompleted processes a task.completed hook event by completing
// the specified task through the AgentService.
func handleHookEventTaskCompleted(s *Server, w http.ResponseWriter, r *http.Request, req *OrchestratorHookEventRequest) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	if req.TaskID == "" {
		RespondBadRequest(w, r, "task_id is required for task.completed events")
		return
	}

	ctx = orchestration.ContextWithTenant(ctx, req.TenantID)

	if err := agentSvc.CompleteTask(ctx, req.TenantID, req.TaskID, req.Result); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusAccepted, map[string]interface{}{
		"task_id":   req.TaskID,
		"completed": true,
	})
}

// handleHookEventTeammateIdle processes a teammate.idle hook event. The event is
// accepted and acknowledged. The orchestrator can use this signal to assign
// pending tasks to the idle agent.
func handleHookEventTeammateIdle(s *Server, w http.ResponseWriter, r *http.Request, req *OrchestratorHookEventRequest) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	ctx = orchestration.ContextWithTenant(ctx, req.TenantID)

	// Check for pending tasks that could be assigned to the idle agent.
	filter := &orchestration.TaskFilter{
		Status: orchestration.TaskStatusPending,
		Limit:  1,
	}

	tasks, err := agentSvc.ListTasks(ctx, req.TenantID, filter)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	response := map[string]interface{}{
		"agent_id":      req.AgentID,
		"acknowledged":  true,
		"pending_tasks": len(tasks),
	}

	RespondJSON(w, r, http.StatusAccepted, response)
}

// =============================================================================
// Health Handlers
// =============================================================================

// HandleOrchestratorHealth handles GET /api/v1/orchestrator/health - Orchestrator health check.
// This is a service-level health probe that verifies the orchestrator is wired
// and available. It does not require tenant context.
func (s *Server) HandleOrchestratorHealth(w http.ResponseWriter, r *http.Request) {
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	// Service-level health: confirm the orchestrator is wired and responding.
	// Full tenant-scoped health reports are available via authenticated endpoints.
	RespondJSON(w, r, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// =============================================================================
// Agent Fleet Management Handlers
// =============================================================================

// HandleOrchestratorRegisterAgent handles POST /api/v1/orchestrator/agents - Register a new agent.
func (s *Server) HandleOrchestratorRegisterAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req orchestration.RegisterAgentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Name == "" {
		RespondBadRequest(w, r, "name is required")
		return
	}

	if req.Host == "" {
		RespondBadRequest(w, r, "host is required")
		return
	}

	if req.AgentType == "" {
		RespondBadRequest(w, r, "agent_type is required")
		return
	}

	if req.TenantID == "" {
		req.TenantID = r.URL.Query().Get("tenant_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, req.TenantID)

	resp, err := agentSvc.RegisterAgent(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondCreated(w, r, resp)
}

// HandleOrchestratorListAgents handles GET /api/v1/orchestrator/agents - List registered agents.
func (s *Server) HandleOrchestratorListAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	filter := &orchestration.AgentFilter{
		Host:      r.URL.Query().Get("host"),
		AgentType: r.URL.Query().Get("agent_type"),
		Backend:   r.URL.Query().Get("backend"),
	}

	statusParam := r.URL.Query().Get("status")
	if statusParam != "" {
		filter.Status = orchestration.AgentStatus(statusParam)
	}

	agents, err := agentSvc.ListAgents(ctx, tenantID, filter)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	})
}

// HandleOrchestratorGetAgent handles GET /api/v1/orchestrator/agents/{agentID} - Get an agent.
func (s *Server) HandleOrchestratorGetAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		RespondBadRequest(w, r, "agent ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	agent, err := agentSvc.GetAgent(ctx, tenantID, agentID)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, agent)
}

// HandleOrchestratorRemoveAgent handles DELETE /api/v1/orchestrator/agents/{agentID} - Remove an agent.
func (s *Server) HandleOrchestratorRemoveAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		RespondBadRequest(w, r, "agent ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if err := agentSvc.RemoveAgent(ctx, tenantID, agentID); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"agent_id": agentID,
		"removed":  true,
	})
}

// HandleOrchestratorAgentHeartbeat handles POST /api/v1/orchestrator/agents/{agentID}/heartbeat - Agent heartbeat.
//
// Self-healing recovery contract: when the agent is unknown to the registry
// (typical case: central daemon restart lost its in-memory map) AND the
// server has an agentHeartbeatRecoverer wired, the handler asks the
// recoverer to re-publish the agent from its mTLS peer certificate before
// surfacing 404. The recoverer cross-checks the cert subject against the
// supplied agentID and refuses any cert that does not own the identity.
// Recovery is idempotent and is logged at INFO. When the recoverer is nil
// (unit tests, builds without iam) or the agent is not recoverable, the
// handler falls through to the original 404 path so the agent's client-side
// fallback can re-enroll explicitly.
func (s *Server) HandleOrchestratorAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	agentID := chi.URLParam(r, "agentID")
	if agentID == "" {
		RespondBadRequest(w, r, "agent ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	// Drain the body once so both the heartbeat path and the recoverer
	// see the same bytes. The body is small (≤ a few KB) and is only
	// used for diagnostic-style fields today (bind_addr for recovery,
	// active_sessions / sandbox_policy / backends for future
	// telemetry); parse failures are non-fatal because the URL path +
	// peer cert already authenticate the call.
	var payload heartbeatBody
	if r.Body != nil {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		_ = r.Body.Close()
		if len(body) > 0 {
			_ = json.Unmarshal(body, &payload)
		}
	}

	err := agentSvc.AgentHeartbeat(ctx, tenantID, agentID)
	if err != nil && isOrchNotFoundError(err) {
		// Try idempotent recovery — if the recoverer accepts the cert
		// it has already re-published the agent, so the retry below
		// hits the live entry and succeeds.
		if recovered := s.tryRecoverAgentHeartbeat(ctx, r, tenantID, agentID, &payload); recovered {
			err = agentSvc.AgentHeartbeat(ctx, tenantID, agentID)
		}
	}
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"agent_id":  agentID,
		"heartbeat": true,
	})
}

// heartbeatBody is the JSON shape the daemon POSTs to the heartbeat
// endpoint. Only fields the central currently consumes are decoded;
// unknown fields are ignored so older / newer agents stay
// interoperable.
type heartbeatBody struct {
	BindAddr       string   `json:"bind_addr,omitempty"`
	Backends       []string `json:"backends,omitempty"`
	SandboxPolicy  string   `json:"sandbox_policy,omitempty"`
	HasContainerRT bool     `json:"has_container_runtime,omitempty"`
}

// =============================================================================
// Error Handling
// =============================================================================

// handleOrchestratorError maps orchestration errors to appropriate HTTP responses.
func handleOrchestratorError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case isOrchNotFoundError(err):
		RespondNotFound(w, r, err.Error())
	case isOrchInvalidInputError(err):
		RespondBadRequest(w, r, err.Error())
	case isOrchConflictError(err):
		RespondConflict(w, r, err.Error())
	case isOrchForbiddenError(err):
		RespondForbidden(w, r, err.Error())
	default:
		RespondInternalError(w, r, err)
	}
}

// isOrchForbiddenError checks if the error represents a permission denial.
//
// Spawn-policy refusals — a bind mount outside every allowed root, a sandbox
// mode or network the policy forbids, an integrity tier below the operator's
// floor — are classified here rather than dropping to the 500 fallback. Each
// carries an actionable message naming the offending field, and a 500 tells an
// operator nothing about whether they hit a misconfiguration or a server
// fault. See orchestration.IsPolicyDenial.
func isOrchForbiddenError(err error) bool {
	var notLead *orchestration.NotLeadOfParentError
	return stderrors.Is(err, orchestration.ErrNotLeadOfParent) ||
		stderrors.As(err, &notLead) ||
		orchestration.IsPolicyDenial(err) ||
		isGRPCPermissionDenied(err)
}

// isGRPCPermissionDenied reports whether err carries a gRPC
// codes.PermissionDenied status. A spawn dispatched to a remote agent daemon
// is policy-checked a second time on that daemon (pkg/agent Runtime.Spawn),
// and its denial arrives here wrapped in a RemoteBackendRPCError that unwraps
// to the grpc status error. Without this the last line of defence would answer
// 500.
func isGRPCPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := grpcstatus.FromError(err); ok {
		return st.Code() == grpccodes.PermissionDenied
	}
	return false
}

// isOrchNotFoundError checks if the error is a not-found type. Surfacing
// these as 404 preserves the actionable message — "acp: backend not found:
// ollama" — instead of dropping to the opaque 500 that RespondInternalError
// writes for defense-in-depth.
func isOrchNotFoundError(err error) bool {
	var taskNotFound *orchestration.TaskNotFoundError
	var projectNotFound *orchestration.ProjectNotFoundError
	var agentNotFound *orchestration.AgentNotFoundError
	var jobNotFound *orchestration.JobNotFoundError
	var backendNotFound *orchestration.BackendNotFoundError
	var workflowNotFound *orchestration.WorkflowNotFoundError
	var artifactNotFound *orchestration.ArtifactNotFoundError
	var parentJobNotFound *orchestration.ParentJobNotFoundError
	var sessionNotFound *orchestration.SessionNotFoundError
	return stderrors.Is(err, orchestration.ErrTaskNotFound) ||
		stderrors.Is(err, orchestration.ErrProjectNotFound) ||
		stderrors.Is(err, orchestration.ErrAgentStoreNotFound) ||
		stderrors.Is(err, orchestration.ErrAgentRegistryNotFound) ||
		stderrors.Is(err, orchestration.ErrJobNotFound) ||
		stderrors.Is(err, orchestration.ErrBackendNotFound) ||
		stderrors.Is(err, orchestration.ErrWorkflowNotFound) ||
		stderrors.Is(err, orchestration.ErrArtifactNotFound) ||
		stderrors.Is(err, orchestration.ErrParentJobNotFound) ||
		stderrors.Is(err, orchestration.ErrSessionNotFound) ||
		stderrors.As(err, &taskNotFound) ||
		stderrors.As(err, &projectNotFound) ||
		stderrors.As(err, &agentNotFound) ||
		stderrors.As(err, &jobNotFound) ||
		stderrors.As(err, &backendNotFound) ||
		stderrors.As(err, &workflowNotFound) ||
		stderrors.As(err, &artifactNotFound) ||
		stderrors.As(err, &parentJobNotFound) ||
		stderrors.As(err, &sessionNotFound) ||
		isGRPCNotFound(err)
}

// isGRPCNotFound reports whether err carries a gRPC codes.NotFound status. The
// remote agent daemon returns NotFound for an unknown session (see
// pkg/agent/spawn_server.go mapRuntimeError); RemoteACPBackend wraps it in a
// RemoteBackendRPCError that unwraps to the grpc status error, so
// status.FromError on the chain recovers the code.
func isGRPCNotFound(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := grpcstatus.FromError(err); ok {
		return st.Code() == grpccodes.NotFound
	}
	return false
}

// isOrchInvalidInputError checks if the error is an invalid input type.
// NoMatchingAgentError is classified here (not as not-found) so strict
// host_affinity offline failures surface as 400 no_matching_agent — the
// resource (backend) exists, the operator just can't satisfy the
// constraint set on this specific request.
func isOrchInvalidInputError(err error) bool {
	var noMatch *orchestration.NoMatchingAgentError
	return stderrors.Is(err, orchestration.ErrBackendRequired) ||
		stderrors.Is(err, orchestration.ErrPromptRequired) ||
		stderrors.Is(err, orchestration.ErrSessionIDRequired) ||
		stderrors.Is(err, orchestration.ErrRootPathRequired) ||
		stderrors.Is(err, orchestration.ErrProjectNameRequired) ||
		stderrors.Is(err, orchestration.ErrProjectPathRequired) ||
		stderrors.Is(err, orchestration.ErrTaskRequired) ||
		stderrors.Is(err, orchestration.ErrTaskStoreInvalidInput) ||
		stderrors.Is(err, orchestration.ErrAgentNameRequired) ||
		stderrors.Is(err, orchestration.ErrAgentHostRequired) ||
		stderrors.Is(err, orchestration.ErrAgentTypeRequired) ||
		stderrors.Is(err, orchestration.ErrAgentRegistryInvalidInput) ||
		stderrors.Is(err, orchestration.ErrTenantRequired) ||
		stderrors.Is(err, orchestration.ErrUnsupportedOperation) ||
		stderrors.Is(err, orchestration.ErrNoMatchingAgent) ||
		stderrors.As(err, &noMatch)
}

// isOrchConflictError checks if the error is a conflict type.
func isOrchConflictError(err error) bool {
	var alreadyClaimed *orchestration.TaskAlreadyClaimedError
	var alreadyExists *orchestration.TaskAlreadyExistsError
	var versionConflict *orchestration.TaskVersionConflictError
	var agentAlreadyExists *orchestration.AgentAlreadyExistsError
	var workflowAlreadyExists *orchestration.WorkflowAlreadyExistsError
	// A session that may not rotate is a state conflict, not a bad request:
	// the caller named a real session and only its policy stands in the way.
	var notRotatable *orchestration.SessionNotRotatableError
	return stderrors.Is(err, orchestration.ErrTaskAlreadyClaimed) ||
		stderrors.Is(err, orchestration.ErrTaskAlreadyExists) ||
		stderrors.Is(err, orchestration.ErrTaskVersionConflict) ||
		stderrors.Is(err, orchestration.ErrAgentRegistryAlreadyExists) ||
		stderrors.Is(err, orchestration.ErrJobAlreadyRunning) ||
		stderrors.Is(err, orchestration.ErrWorkflowAlreadyExists) ||
		stderrors.As(err, &alreadyClaimed) ||
		stderrors.As(err, &alreadyExists) ||
		stderrors.As(err, &versionConflict) ||
		stderrors.As(err, &agentAlreadyExists) ||
		stderrors.As(err, &workflowAlreadyExists) ||
		stderrors.Is(err, orchestration.ErrSessionNotRotatable) ||
		stderrors.As(err, &notRotatable)
}

// =============================================================================
// Workflow Handlers
// =============================================================================

// HandleOrchestratorListWorkflows handles GET /api/v1/orchestrator/workflows
// Returns all registered workflow definitions sorted by name.
func (s *Server) HandleOrchestratorListWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	wfs, err := agentSvc.ListWorkflows(ctx)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}
	if wfs == nil {
		wfs = []*orchestration.WorkflowDefinition{}
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"workflows": wfs,
	})
}

// HandleOrchestratorGetWorkflow handles GET /api/v1/orchestrator/workflows/{name}
// Returns a single workflow definition by name.
func (s *Server) HandleOrchestratorGetWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "workflow name is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	wf, err := agentSvc.GetWorkflow(ctx, name)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"workflow": wf,
	})
}

// HandleOrchestratorUpdateWorkflow handles PUT /api/v1/orchestrator/workflows/{name}
// Replaces an existing workflow definition. Returns 404 if the workflow does not exist.
func (s *Server) HandleOrchestratorUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "workflow name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var def orchestration.WorkflowDefinition
	if err := json.Unmarshal(body, &def); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// URL path name takes precedence over embedded name.
	def.Name = name

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if err := agentSvc.UpdateWorkflow(ctx, &def); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"workflow": &def,
	})
}

// HandleOrchestratorDeleteWorkflow handles DELETE /api/v1/orchestrator/workflows/{name}
// Removes a workflow definition by name. Returns 404 if the workflow does not exist.
func (s *Server) HandleOrchestratorDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "workflow name is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if err := agentSvc.DeleteWorkflow(ctx, name); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"name":    name,
		"deleted": true,
	})
}

// HandleOrchestratorCreateWorkflow handles POST /api/v1/orchestrator/workflows
// Validates and registers a workflow definition supplied in the request body.
func (s *Server) HandleOrchestratorCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req struct {
		TenantID   string                            `json:"tenant_id"`
		Name       string                            `json:"name"`
		Definition *orchestration.WorkflowDefinition `json:"definition"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	if req.Name == "" {
		RespondBadRequest(w, r, "workflow name is required")
		return
	}
	if req.Definition == nil {
		RespondBadRequest(w, r, "workflow definition is required")
		return
	}

	// Name in the URL body takes precedence over the embedded definition name.
	req.Definition.Name = req.Name

	ctx = orchestration.ContextWithTenant(ctx, req.TenantID)

	if err := agentSvc.RegisterWorkflow(ctx, req.Definition); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, map[string]interface{}{
		"workflow": req.Definition,
	})
}

// =============================================================================
// Sub-job Spawning Handler
// =============================================================================

// HandleOrchestratorSpawnChildJob handles POST /api/v1/orchestrator/jobs/{jobID}/children.
// The caller_agent_id in the request body is the fleet agent identity of the spawning lead.
func (s *Server) HandleOrchestratorSpawnChildJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	parentJobID := chi.URLParam(r, "jobID")
	if parentJobID == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req orchestration.SpawnChildJobRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	req.ParentJobID = parentJobID

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	resp, err := agentSvc.SpawnChildJob(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// =============================================================================
// Task Artifact Handlers
// =============================================================================

// HandleOrchestratorSubmitArtifacts handles POST /api/v1/orchestrator/tasks/{taskID}/artifacts.
func (s *Server) HandleOrchestratorSubmitArtifacts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondBadRequest(w, r, "task ID is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var artifacts []orchestration.Artifact
	if err := json.Unmarshal(body, &artifacts); err != nil {
		RespondBadRequest(w, r, "invalid JSON body: expected array of artifacts")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if err := agentSvc.SubmitTaskArtifacts(ctx, tenantID, taskID, artifacts); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"task_id": taskID,
		"stored":  len(artifacts),
	})
}

// HandleOrchestratorListArtifacts handles GET /api/v1/orchestrator/tasks/{taskID}/artifacts.
func (s *Server) HandleOrchestratorListArtifacts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondBadRequest(w, r, "task ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	artifacts, err := agentSvc.ListTaskArtifacts(ctx, tenantID, taskID)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"task_id":   taskID,
		"artifacts": artifacts,
	})
}

// HandleOrchestratorGetArtifact handles GET /api/v1/orchestrator/tasks/{taskID}/artifacts/{name}.
// Resolves KV refs to full byte content; returns Content as base64 in JSON.
func (s *Server) HandleOrchestratorGetArtifact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	taskID := chi.URLParam(r, "taskID")
	name := chi.URLParam(r, "name")
	if taskID == "" || name == "" {
		RespondBadRequest(w, r, "task ID and artifact name are required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	artifact, err := agentSvc.GetTaskArtifact(ctx, tenantID, taskID, name)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, artifact)
}
