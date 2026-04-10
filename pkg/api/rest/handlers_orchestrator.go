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
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

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

		// Task management
		r.Post("/tasks", s.HandleOrchestratorCreateTask)
		r.Get("/tasks/{taskID}", s.HandleOrchestratorGetTask)
		r.Get("/tasks", s.HandleOrchestratorListTasks)
		r.Post("/tasks/{taskID}/claim", s.HandleOrchestratorClaimTask)
		r.Post("/tasks/{taskID}/complete", s.HandleOrchestratorCompleteTask)
		r.Post("/tasks/decompose", s.HandleOrchestratorDecomposeTask)

		// Job management
		r.Post("/jobs", s.HandleOrchestratorStartJob)
		r.Get("/jobs", s.HandleOrchestratorListJobs)
		r.Get("/jobs/{jobID}", s.HandleOrchestratorGetJobStatus)
		r.Delete("/jobs/{jobID}", s.HandleOrchestratorCancelJob)
		r.Get("/jobs/{jobID}/events", s.HandleOrchestratorSubscribeJobEvents)

		// Backend management
		r.Get("/backends", s.HandleOrchestratorListBackends)

		// Project discovery
		r.Post("/discover", s.HandleOrchestratorDiscover)

		// Webhook hook events
		r.Post("/hooks", s.HandleOrchestratorHookEvent)

		// Health
		r.Get("/health", s.HandleOrchestratorHealth)

		// Agent fleet management
		r.Post("/agents", s.HandleOrchestratorRegisterAgent)
		r.Get("/agents", s.HandleOrchestratorListAgents)
		r.Get("/agents/{agentID}", s.HandleOrchestratorGetAgent)
		r.Delete("/agents/{agentID}", s.HandleOrchestratorRemoveAgent)
		r.Post("/agents/{agentID}/heartbeat", s.HandleOrchestratorAgentHeartbeat)
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

	resp, err := agentSvc.SpawnSession(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondCreated(w, r, resp)
}

// HandleOrchestratorPrompt handles POST /api/v1/orchestrator/sessions/{sessionID}/prompt - Prompt a session.
func (s *Server) HandleOrchestratorPrompt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	resp, err := agentSvc.PromptSession(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleOrchestratorSessionStatus handles GET /api/v1/orchestrator/sessions/{sessionID} - Get session status.
func (s *Server) HandleOrchestratorSessionStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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
	ctx := r.Context()

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

// HandleOrchestratorListSessions handles GET /api/v1/orchestrator/sessions - List active sessions.
func (s *Server) HandleOrchestratorListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

// =============================================================================
// Project Discovery Handlers
// =============================================================================

// HandleOrchestratorDiscover handles POST /api/v1/orchestrator/discover - Discover projects.
func (s *Server) HandleOrchestratorDiscover(w http.ResponseWriter, r *http.Request) {
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

	if err := agentSvc.AgentHeartbeat(ctx, tenantID, agentID); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"agent_id":  agentID,
		"heartbeat": true,
	})
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
	default:
		RespondInternalError(w, r, err)
	}
}

// isOrchNotFoundError checks if the error is a not-found type.
func isOrchNotFoundError(err error) bool {
	var taskNotFound *orchestration.TaskNotFoundError
	var projectNotFound *orchestration.ProjectNotFoundError
	var agentNotFound *orchestration.AgentNotFoundError
	var jobNotFound *orchestration.JobNotFoundError
	return stderrors.Is(err, orchestration.ErrTaskNotFound) ||
		stderrors.Is(err, orchestration.ErrProjectNotFound) ||
		stderrors.Is(err, orchestration.ErrAgentStoreNotFound) ||
		stderrors.Is(err, orchestration.ErrAgentRegistryNotFound) ||
		stderrors.Is(err, orchestration.ErrJobNotFound) ||
		stderrors.As(err, &taskNotFound) ||
		stderrors.As(err, &projectNotFound) ||
		stderrors.As(err, &agentNotFound) ||
		stderrors.As(err, &jobNotFound)
}

// isOrchInvalidInputError checks if the error is an invalid input type.
func isOrchInvalidInputError(err error) bool {
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
		stderrors.Is(err, orchestration.ErrAgentRegistryInvalidInput)
}

// isOrchConflictError checks if the error is a conflict type.
func isOrchConflictError(err error) bool {
	var alreadyClaimed *orchestration.TaskAlreadyClaimedError
	var alreadyExists *orchestration.TaskAlreadyExistsError
	var versionConflict *orchestration.TaskVersionConflictError
	var agentAlreadyExists *orchestration.AgentAlreadyExistsError
	return stderrors.Is(err, orchestration.ErrTaskAlreadyClaimed) ||
		stderrors.Is(err, orchestration.ErrTaskAlreadyExists) ||
		stderrors.Is(err, orchestration.ErrTaskVersionConflict) ||
		stderrors.Is(err, orchestration.ErrAgentRegistryAlreadyExists) ||
		stderrors.Is(err, orchestration.ErrJobAlreadyRunning) ||
		stderrors.As(err, &alreadyClaimed) ||
		stderrors.As(err, &alreadyExists) ||
		stderrors.As(err, &versionConflict) ||
		stderrors.As(err, &agentAlreadyExists)
}
