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
	"strings"

	"github.com/go-chi/chi/v5"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Route Registration
// =============================================================================

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
// within and stamps it onto the returned context under both keys the request
// reaches: go-qrdb's central key (tenantctx) and the orchestrator's own
// (orchestration.ContextWithTenant), which its tenant guard
// (orchestration.agentServiceImpl.validateTenant) reads. It is the single
// tenant-derivation point for every orchestrator REST handler.
//
// THE ?tenant_id= PARAMETER IS NOT TRUSTED. It is honoured only as
// tenantctx.Target allows: a caller may name only its own tenant — the system
// tenant gets no cross-tenant lift through it — and every other naming is
// refused (go-quicbaas#1150).
// Omitting it means the caller's own tenant, read from the tenant the
// authentication layer bound — go-qrdb's AuthMiddleware on the unified
// server, injectPrincipal on the platform-token path — not from a fallback to
// the system tenant. A request with no tenant bound is refused.
//
// On refusal it answers 403 and returns ok=false; the caller returns.
func resolveOrchestratorTenant(w http.ResponseWriter, r *http.Request) (context.Context, string, bool) {
	ctx, tenantID, err := tenantctx.ContextForTarget(r.Context(), "rest.Orchestrator", r.URL.Query().Get("tenant_id"))
	if err != nil {
		RespondError(w, r, http.StatusForbidden, err)
		return r.Context(), "", false
	}
	return orchestration.ContextWithTenant(ctx, tenantID), tenantID, true
}

// =============================================================================
// Session Management Handlers
// =============================================================================

// HandleOrchestratorSpawn handles POST /api/v1/orchestrator/sessions - Spawn a new agent session.
func (s *Server) HandleOrchestratorSpawn(w http.ResponseWriter, r *http.Request) {
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

	if req.Backend == "" && req.AgentID == "" && req.PoolID == "" {
		RespondBadRequest(w, r, "an agent, a pool or a backend is required")
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
	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	// Per-request strict bit overrides the operator-level
	// orchestration.host_affinity.mode default: when set to true, an offline
	// host_affinity surfaces NoMatchingFleetNodeError (400) instead of falling
	// through to BackendNotFoundError (404).
	if req.HostAffinityStrict {
		ctx = orchestration.WithHostAffinityStrict(ctx)
	}

	// Install a holder so the remote backend resolver can stash a typed
	// NoMatchingFleetNodeError when strict mode rejects an offline host.
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
	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

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
	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

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
	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

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
	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

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
	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	backend := r.URL.Query().Get("backend")

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	resp, err := agentSvc.ListSessions(ctx, &orchestration.ListSessionsRequest{
		Backend: backend,
		// The SDK has sent ?status= since it gained the field; the handler
		// dropped it, so the filter was accepted and ignored (#792).
		Status:    orchestration.SessionState(r.URL.Query().Get("status")),
		ProjectID: r.URL.Query().Get("project_id"),
		JobID:     r.URL.Query().Get("job_id"),
		// runner is the session's ROLE — sdlc, review, triage, workflow —
		// read off the job it belongs to, and agent_id is the daemon that
		// ran it. Neither is on the session record; both are resolved for
		// the listing before it is counted and cut.
		Runner:  r.URL.Query().Get("runner"),
		AgentID: r.URL.Query().Get("agent_id"),
		Page:    page,
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

// HandleOrchestratorDeleteTask handles DELETE /api/v1/orchestrator/tasks/{taskID} - Delete a task.
//
// Removing the record also releases the task's source reservation, so the
// issue or ticket it was adopted from can be adopted again. A task a job holds
// a live lease on is refused with 409.
func (s *Server) HandleOrchestratorDeleteTask(w http.ResponseWriter, r *http.Request) {
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

	if err := agentSvc.DeleteTask(ctx, tenantID, taskID); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"task_id": taskID,
		"deleted": true,
	})
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

	// A STATUS THE ENGINE DOES NOT HAVE IS REFUSED, not matched against
	// nothing: `statuses=complete` is a typo, and answering "there are no
	// such tasks" would report the typo as a fact about the tenant. Every
	// other narrowing here names a record that may simply not exist.
	statuses, err := orchestration.ParseTaskStatuses("statuses", r.URL.Query().Get("statuses"))
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	filter := &orchestration.TaskFilter{
		TeamName:   r.URL.Query().Get("team_name"),
		AssignedTo: r.URL.Query().Get("assigned_to"),
		CreatedBy:  r.URL.Query().Get("created_by"),
		ProjectID:  r.URL.Query().Get("project_id"),
		Statuses:   statuses,
		// agent_id and job_id resolve through the task's LATEST ATTEMPT: the
		// Tasks view's "which agent is running this" and a job page's "which
		// task is this an attempt at" are the same binding read both ways.
		AgentID: r.URL.Query().Get("agent_id"),
		JobID:   r.URL.Query().Get("job_id"),
		// source_kind and source_ref ask which task holds one source
		// reservation in project_id's scope.
		SourceKind: orchestration.TaskSourceKind(r.URL.Query().Get("source_kind")),
		SourceRef:  r.URL.Query().Get("source_ref"),
	}

	statusParam := r.URL.Query().Get("status")
	if statusParam != "" {
		filter.Status = orchestration.TaskStatus(statusParam)
	}

	autoSpawn, err := orchestration.ParseOptionalBool("auto_spawn", r.URL.Query().Get("auto_spawn"))
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}
	filter.AutoSpawn = autoSpawn

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	tasks, err := agentSvc.ListTasksPage(ctx, tenantID, filter, page)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, tasks.Envelope("tasks"))
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

// parseTaskStatuses splits the comma-separated `statuses` query parameter
// into task statuses, dropping empty segments. An empty parameter yields nil
// so the filter stays unset.
func parseTaskStatuses(raw string) []orchestration.TaskStatus {
	return parseCSVList[orchestration.TaskStatus](raw)
}

// parseCSVList reads a comma-separated list filter — ?statuses=, ?job_ids= —
// into its values. Blank entries are dropped, so "executing,,failed" and
// "executing,failed" ask the same question, and a wholly blank value narrows
// NOTHING rather than narrowing to nothing — an empty list means "every
// value" everywhere it is read.
func parseCSVList[T ~string](raw string) []T {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	states := make([]T, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			states = append(states, T(trimmed))
		}
	}
	if len(states) == 0 {
		return nil
	}
	return states
}

// OrchestratorBlockTaskRequest is the request body for blocking a task.
type OrchestratorBlockTaskRequest struct {
	Kind   orchestration.TaskBlockKind `json:"kind"`
	Reason string                      `json:"reason"`
}

// HandleOrchestratorBlockTask handles POST /api/v1/orchestrator/tasks/{taskID}/block - Park a task until a person readmits it.
func (s *Server) HandleOrchestratorBlockTask(w http.ResponseWriter, r *http.Request) {
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

	var req OrchestratorBlockTaskRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Kind == "" {
		RespondBadRequest(w, r, "kind is required")
		return
	}
	if req.Reason == "" {
		RespondBadRequest(w, r, "reason is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if err := agentSvc.BlockTask(ctx, tenantID, taskID, req.Kind, req.Reason); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"task_id": taskID,
		"kind":    req.Kind,
		"blocked": true,
	})
}

// HandleOrchestratorReadmitTask handles POST /api/v1/orchestrator/tasks/{taskID}/readmit - Return a blocked task to the queue.
func (s *Server) HandleOrchestratorReadmitTask(w http.ResponseWriter, r *http.Request) {
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

	if err := agentSvc.ReadmitTask(ctx, tenantID, taskID); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"task_id":    taskID,
		"readmitted": true,
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
	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

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

	// A job driven by a module runner takes its work from the task it is an
	// attempt at, not from a description; only a job with neither has
	// nothing to run. The runner refuses what it cannot drive.
	if req.Description == "" && req.TaskID == "" {
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
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	jobID := chi.URLParam(r, "jobID")
	if jobID == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	jobStatus, err := agentSvc.GetJobStatus(ctx, tenantID, jobID)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, jobStatus)
}

// HandleOrchestratorListJobs handles GET /api/v1/orchestrator/jobs - list the
// tenant's jobs, narrowed to the attempts at one task when task_id is given.
func (s *Server) HandleOrchestratorListJobs(w http.ResponseWriter, r *http.Request) {
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	// task_id is HONOURED, not decorative. It was accepted and ignored until
	// go-quicbaas#822, so a task page asking for its own attempts was handed
	// every job in the tenant and had to re-filter an answer it could not
	// verify. statuses, runner and project_id narrow the same listing the
	// same way, so the ~2,000-record job history of a burn-in tenant can be
	// asked a question instead of downloaded whole.
	states, err := orchestration.ParseJobStates("statuses", r.URL.Query().Get("statuses"))
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	filter := &orchestration.JobFilter{
		TaskID:    r.URL.Query().Get("task_id"),
		Runner:    r.URL.Query().Get("runner"),
		ProjectID: r.URL.Query().Get("project_id"),
		AgentID:   r.URL.Query().Get("agent_id"),
		Statuses:  states,
		// job_ids answers a page of something else: the sessions page names
		// a job per row and renders its runner's role, and asks for exactly
		// those jobs in one call rather than 25.
		JobIDs: orchestration.SplitCSVFilter(r.URL.Query().Get("job_ids")),
	}

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	jobs, err := agentSvc.ListJobsPage(ctx, tenantID, filter, page)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, jobs.Envelope("jobs"))
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
			_ = enc.Encode(evt)
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
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	backends, err := orchestration.ListBackendsPage(ctx, agentSvc, page)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, backends.Envelope("backends"))
}

// HandleListFleetNodeBackendModels handles
// GET /api/v1/fleet/{nodeID}/backends/{backendName}/models
//
// Returns the model names available on the named backend as seen from the
// specified agent. For Ollama this is the set of locally-pulled models;
// for ACP backends (claude, codex, opencode) it returns their curated catalog.
// The agentID is used as a host affinity hint when a remote backend resolver
// is active; it is ignored for locally-registered backends.
func (s *Server) HandleListFleetNodeBackendModels(w http.ResponseWriter, r *http.Request) {
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

	// Thread the fleet node ID as host affinity so the remote backend
	// resolver can forward the request to the correct node when needed.
	nodeID := chi.URLParam(r, "nodeID")
	if nodeID != "" && nodeID != "any" {
		ctx = orchestration.WithHostAffinity(ctx, nodeID)
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
		if respondDaemonRoutingError(w, r, err) {
			return
		}
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
// Job Runner Handlers
// =============================================================================

// OrchestratorJobStateSpecResponse is one state a runner drives a job
// through, as a consumer sees it. IdleThreshold is deliberately absent: it is
// the stall detector's tuning, not something a stepper renders.
type OrchestratorJobStateSpecResponse struct {
	// State is the state as it appears on JobStatus.
	State string `json:"state"`

	// Label is how the state is named to a person.
	Label string `json:"label"`

	// SessionBacked says an agent session is doing the work in this state.
	// Always serialized so a consumer never has to guess the default.
	SessionBacked bool `json:"session_backed"`
}

// OrchestratorJobRunnerResponse is one registered runner and the ordered,
// non-terminal states it declares.
type OrchestratorJobRunnerResponse struct {
	// Name is what JobConfig.Runner selects the runner by.
	Name string `json:"name"`

	// States are the runner's states in the order it drives them.
	States []OrchestratorJobStateSpecResponse `json:"states"`
}

// OrchestratorListRunnersResponse is the runners envelope, matching the
// list convention the other orchestrator collections use.
type OrchestratorListRunnersResponse struct {
	// Runners is every registered runner, ordered by name.
	Runners []OrchestratorJobRunnerResponse `json:"runners"`

	// Count is len(Runners).
	Count int `json:"count"`

	Total      int `json:"total"`
	Limit      int `json:"limit"`
	Offset     int `json:"offset"`
	NextOffset int `json:"next_offset,omitempty"`
}

// HandleOrchestratorListRunners handles GET /api/v1/orchestrator/runners —
// every registered JobRunner and the states it declares.
//
// This is REGISTRY data, not tenant data: the runners a daemon can drive are
// the same for every caller, so there is no tenant derivation here. It is
// still gated on the orchestrator service being present, because a node that
// runs no orchestrator drives no jobs and answering "these are the runners"
// there would describe a capability the node does not have.
//
// Consumers that used to hardcode the engine's thirteen job states — the UI's
// stepper above all — read them from here instead.
func (s *Server) HandleOrchestratorListRunners(w http.ResponseWriter, r *http.Request) {
	if agentSvc := s.getAgentService(w, r); agentSvc == nil {
		return
	}

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	result, err := orchestration.ListJobRunnersPage(page)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	runners := make([]OrchestratorJobRunnerResponse, 0, len(result.Items))
	for _, runner := range result.Items {
		states := make([]OrchestratorJobStateSpecResponse, 0, len(runner.States))
		for _, spec := range runner.States {
			states = append(states, OrchestratorJobStateSpecResponse{
				State:         string(spec.State),
				Label:         spec.Label,
				SessionBacked: spec.SessionBacked,
			})
		}
		runners = append(runners, OrchestratorJobRunnerResponse{
			Name:   runner.Name,
			States: states,
		})
	}

	RespondJSON(w, r, http.StatusOK, OrchestratorListRunnersResponse{
		Runners:    runners,
		Count:      len(runners),
		Total:      result.Total,
		Limit:      result.Limit,
		Offset:     result.Offset,
		NextOffset: result.NextOffset,
	})
}

// =============================================================================
// Project Discovery Handlers
// =============================================================================

// HandleOrchestratorDiscover handles POST /api/v1/orchestrator/discover - Discover projects.
func (s *Server) HandleOrchestratorDiscover(w http.ResponseWriter, r *http.Request) {
	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

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
// Fleet Management Handlers
// =============================================================================

// HandleRegisterFleetNode handles POST /api/v1/fleet - Register a fleet node.
func (s *Server) HandleRegisterFleetNode(w http.ResponseWriter, r *http.Request) {
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

	var req orchestration.RegisterFleetNodeRequest
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

	if req.NodeType == "" {
		RespondBadRequest(w, r, "node_type is required")
		return
	}

	if req.TenantID == "" {
		req.TenantID = r.URL.Query().Get("tenant_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, req.TenantID)

	resp, err := agentSvc.RegisterFleetNode(ctx, &req)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondCreated(w, r, resp)
}

// HandleListFleetNodes handles GET /api/v1/fleet - List registered fleet nodes.
func (s *Server) HandleListFleetNodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	filter := &orchestration.FleetNodeFilter{
		Host:     r.URL.Query().Get("host"),
		NodeType: r.URL.Query().Get("node_type"),
		Backend:  r.URL.Query().Get("backend"),
	}

	statusParam := r.URL.Query().Get("status")
	if statusParam != "" {
		filter.Status = orchestration.FleetNodeStatus(statusParam)
	}

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	nodes, err := agentSvc.ListFleetNodesPage(ctx, tenantID, filter, page)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	// "count" is how many records are IN THIS RESPONSE, which is what it has
	// always been; "total" is how many matched. They differ for the first
	// time now that a fleet larger than a page can be asked for, and the
	// envelope names both rather than overloading one.
	envelope := nodes.Envelope("nodes")
	envelope["nodes"] = orchestration.FleetNodeViews(nodes.Items)
	envelope["count"] = len(nodes.Items)

	RespondJSON(w, r, http.StatusOK, envelope)
}

// HandleGetFleetNode handles GET /api/v1/fleet/{nodeID} - Get a fleet node.
func (s *Server) HandleGetFleetNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	nodeID := chi.URLParam(r, "nodeID")
	if nodeID == "" {
		RespondBadRequest(w, r, "node ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	agent, err := agentSvc.GetFleetNode(ctx, tenantID, nodeID)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, orchestration.NewFleetNodeView(agent))
}

// HandleRemoveFleetNode handles DELETE /api/v1/fleet/{nodeID} - Remove a fleet node.
func (s *Server) HandleRemoveFleetNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	nodeID := chi.URLParam(r, "nodeID")
	if nodeID == "" {
		RespondBadRequest(w, r, "node ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if err := agentSvc.RemoveFleetNode(ctx, tenantID, nodeID); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"node_id": nodeID,
		"removed": true,
	})
}

// HandleFleetNodeHeartbeat handles POST /api/v1/fleet/{nodeID}/heartbeat - Fleet node heartbeat.
//
// Self-healing recovery contract: when the agent is unknown to the registry
// (typical case: central daemon restart lost its in-memory map) AND the
// server has a fleetHeartbeatRecoverer wired, the handler asks the
// recoverer to re-publish the agent from its mTLS peer certificate before
// surfacing 404. The recoverer cross-checks the cert subject against the
// supplied nodeID and refuses any cert that does not own the identity.
// Recovery is idempotent and is logged at INFO. When the recoverer is nil
// (unit tests, builds without iam) or the agent is not recoverable, the
// handler falls through to the original 404 path so the agent's client-side
// fallback can re-enroll explicitly.
func (s *Server) HandleFleetNodeHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	nodeID := chi.URLParam(r, "nodeID")
	if nodeID == "" {
		RespondBadRequest(w, r, "node ID is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	// Drain the body once so both the heartbeat path and the recoverer
	// see the same bytes. The body is small (≤ a few KB) and is only
	// used for diagnostic-style fields today (bind_addr for recovery,
	// active_sessions / sandbox_policy / backends for future
	// telemetry). An empty body is a valid bare heartbeat; an unreadable or
	// malformed one is the caller's fault and answers 400. net/http closes
	// the request body when the handler returns.
	var payload heartbeatBody
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			RespondBadRequest(w, r, "heartbeat body unreadable")
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				RespondBadRequest(w, r, "heartbeat body is not valid JSON")
				return
			}
		}
	}

	// The served list the body carries is the daemon's running truth about
	// itself, and passing it here is what lets the record be CORRECTED after
	// registration: a daemon restarted with a different --backends, or
	// upgraded in place from the pre-#681 transport vocabulary, converges on
	// the next heartbeat instead of keeping the names it enrolled with
	// forever (go-quicbaas#699).
	observed := payload.observation()

	err := agentSvc.FleetNodeHeartbeat(ctx, tenantID, nodeID, observed)
	if err != nil && isOrchNotFoundError(err) {
		// Try idempotent recovery — if the recoverer accepts the cert
		// it has already re-published the agent, so the retry below
		// hits the live entry and succeeds.
		if recovered := s.tryRecoverFleetHeartbeat(ctx, r, tenantID, nodeID, &payload); recovered {
			err = agentSvc.FleetNodeHeartbeat(ctx, tenantID, nodeID, observed)
		}
	}
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"node_id":   nodeID,
		"heartbeat": true,
	})
}

// heartbeatBody is the JSON shape the daemon POSTs to the heartbeat
// endpoint. Only fields the central currently consumes are decoded;
// unknown fields are ignored so older / newer agents stay
// interoperable.
type heartbeatBody struct {
	BindAddr       string `json:"bind_addr,omitempty"`
	SandboxPolicy  string `json:"sandbox_policy,omitempty"`
	HasContainerRT bool   `json:"has_container_runtime,omitempty"`

	// FleetHeartbeatReport is what the daemon reports about itself: served
	// backends, slot count, signing traits, images, device telemetry and
	// host metrics. Shared with every other transport so none decodes a
	// heartbeat differently.
	orchestration.FleetHeartbeatReport
}

// observation returns what the daemon reported about itself on this
// heartbeat, or nil when it reported nothing at all.
func (b *heartbeatBody) observation() *orchestration.FleetNodeObservation {
	if b == nil {
		return nil
	}
	return b.Observation()
}

// =============================================================================
// Error Handling
// =============================================================================

// handleOrchestratorError maps orchestration errors to appropriate HTTP responses.
func handleOrchestratorError(w http.ResponseWriter, r *http.Request, err error) {
	if respondReferenceError(w, r, err) {
		return
	}
	// A spawn or a job naming an Agent or pool is refused in the catalog's
	// own vocabulary; a pool every Agent of which refused is exhausted.
	if fault, ok := orchestration.ClassifyAgentCatalogError(err); ok {
		agentCatalogResponders[fault](w, r, err.Error())
		return
	}
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
		isQBForbiddenError(err) ||
		isGRPCPermissionDenied(err)
}

func isQBForbiddenError(err error) bool {
	return qerrors.Code(err) == qerrors.ErrForbidden
}

// isGRPCPermissionDenied reports whether err carries a gRPC
// codes.PermissionDenied status. A spawn dispatched to a remote fleet node daemon
// is policy-checked a second time on that daemon (pkg/orchestration/fleet Runtime.Spawn),
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
	var agentNotFound *orchestration.FleetNodeNotFoundError
	var jobNotFound *orchestration.JobNotFoundError
	var backendNotFound *orchestration.BackendNotFoundError
	var workflowNotFound *orchestration.WorkflowNotFoundError
	var artifactNotFound *orchestration.ArtifactNotFoundError
	var parentJobNotFound *orchestration.ParentJobNotFoundError
	var sessionNotFound *orchestration.SessionNotFoundError
	return stderrors.Is(err, orchestration.ErrTaskNotFound) ||
		stderrors.Is(err, orchestration.ErrProjectNotFound) ||
		stderrors.Is(err, orchestration.ErrAgentStoreNotFound) ||
		stderrors.Is(err, orchestration.ErrFleetNodeNotFound) ||
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
// remote fleet node daemon returns NotFound for an unknown session (see
// pkg/orchestration/fleet/spawn_server.go mapRuntimeError); RemoteACPBackend wraps it in a
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
//
// ErrSessionStateUnknown is classified here so a misspelled ?status= answers
// 400 rather than falling through to the 500 default: the caller named a state
// this daemon does not use, which is a bad request and not a server fault.
//
// NoMatchingFleetNodeError is classified here (not as not-found) so strict
// host_affinity offline failures surface as 400 no_matching_agent — the
// resource (backend) exists, the operator just can't satisfy the
// constraint set on this specific request.
func isOrchInvalidInputError(err error) bool {
	var noMatch *orchestration.NoMatchingFleetNodeError
	return isPageInputError(err) ||
		stderrors.Is(err, orchestration.ErrInvalidInput) ||
		stderrors.Is(err, orchestration.ErrWorkflowValidation) ||
		stderrors.Is(err, orchestration.ErrWorkflowRetiredField) ||
		stderrors.Is(err, orchestration.ErrSessionStateUnknown) ||
		stderrors.Is(err, orchestration.ErrBackendRequired) ||
		stderrors.Is(err, orchestration.ErrPromptRequired) ||
		stderrors.Is(err, orchestration.ErrSessionIDRequired) ||
		stderrors.Is(err, orchestration.ErrRootPathRequired) ||
		stderrors.Is(err, orchestration.ErrProjectNameRequired) ||
		stderrors.Is(err, orchestration.ErrProjectPathRequired) ||
		stderrors.Is(err, orchestration.ErrTaskRequired) ||
		stderrors.Is(err, orchestration.ErrTaskStoreInvalidInput) ||
		stderrors.Is(err, orchestration.ErrFleetNodeNameRequired) ||
		stderrors.Is(err, orchestration.ErrFleetNodeHostRequired) ||
		stderrors.Is(err, orchestration.ErrFleetNodeTypeRequired) ||
		stderrors.Is(err, orchestration.ErrFleetRegistryInvalidInput) ||
		stderrors.Is(err, orchestration.ErrTenantRequired) ||
		stderrors.Is(err, orchestration.ErrUnsupportedOperation) ||
		stderrors.Is(err, orchestration.ErrNoMatchingFleetNode) ||
		stderrors.As(err, &noMatch)
}

// isOrchConflictError checks if the error is a conflict type.
func isOrchConflictError(err error) bool {
	var alreadyClaimed *orchestration.TaskAlreadyClaimedError
	var alreadyExists *orchestration.TaskAlreadyExistsError
	var versionConflict *orchestration.TaskVersionConflictError
	var agentAlreadyExists *orchestration.FleetNodeAlreadyExistsError
	var workflowAlreadyExists *orchestration.WorkflowAlreadyExistsError
	// A session that may not rotate is a state conflict, not a bad request:
	// the caller named a real session and only its policy stands in the way.
	var notRotatable *orchestration.SessionNotRotatableError
	// A task a running job holds a lease on is a state conflict for the same
	// reason: the caller named a real task and only the job in flight stands
	// in the way, so the answer is "not now", not "never" or "no such task".
	var leased *orchestration.TaskLeasedError
	// A REFUSED STATE TRANSITION IS THE CALLER'S CONDITION, NOT THE SERVER'S.
	// It fell through to RespondInternalError, so readmitting a task the
	// transition table would not move answered a bare 500 naming neither state.
	// That is how go-quicbaas#945 stayed invisible for five days: the one verb
	// that recovers a task told the operator nothing about why it had not.
	var invalidState *orchestration.TaskInvalidStateError
	return stderrors.Is(err, orchestration.ErrTaskLeased) ||
		stderrors.As(err, &leased) ||
		stderrors.As(err, &invalidState) ||
		stderrors.Is(err, orchestration.ErrTaskAlreadyClaimed) ||
		stderrors.Is(err, orchestration.ErrTaskAlreadyExists) ||
		stderrors.Is(err, orchestration.ErrTaskVersionConflict) ||
		stderrors.Is(err, orchestration.ErrFleetNodeAlreadyExists) ||
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
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	filter := &orchestration.WorkflowFilter{
		ProjectID: r.URL.Query().Get("project_id"),
		Mode:      orchestration.WorkflowMode(r.URL.Query().Get("mode")),
	}

	wfs, err := agentSvc.ListWorkflowsPage(ctx, filter, page)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, wfs.Envelope("workflows"))
}

// HandleOrchestratorGetWorkflow handles GET /api/v1/orchestrator/workflows/{name}
// Returns a single workflow definition by name.
func (s *Server) HandleOrchestratorGetWorkflow(w http.ResponseWriter, r *http.Request) {
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "workflow name is required")
		return
	}

	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	wf, err := agentSvc.GetWorkflow(ctx, name)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"workflow": wf,
	})
}

// workflowRequestSource names a workflow definition sent in a request body in
// a retired-field refusal.
const workflowRequestSource = "request body"

// HandleOrchestratorUpdateWorkflow handles PUT /api/v1/orchestrator/workflows/{name}
// Replaces an existing workflow definition. Returns 404 if the workflow does not exist.
func (s *Server) HandleOrchestratorUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
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
	if err := orchestration.RefuseRetiredWorkflowFields(workflowRequestSource, body); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	// URL path name takes precedence over embedded name.
	def.Name = name

	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

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
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "workflow name is required")
		return
	}

	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

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
	var raw struct {
		Definition json.RawMessage `json:"definition"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	if err := orchestration.RefuseRetiredWorkflowFields(workflowRequestSource, raw.Definition); err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	// Name in the URL body takes precedence over the embedded definition name.
	req.Definition.Name = req.Name

	// The body's tenant_id wins; otherwise derive the tenant exactly as every
	// other orchestrator handler does (query, then token claim, then system).
	ctx, _, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}
	if req.TenantID != "" {
		ctx = orchestration.ContextWithTenant(r.Context(), req.TenantID)
	}

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
// The caller_agent_id in the request body is the fleet node identity of the spawning lead.
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
	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondBadRequest(w, r, "task ID is required")
		return
	}

	ctx, tenantID, tenantOK := resolveOrchestratorTenant(w, r)
	if !tenantOK {
		return
	}

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	artifacts, err := orchestration.ListTaskArtifactsPage(ctx, agentSvc, tenantID, taskID, page)
	if err != nil {
		handleOrchestratorError(w, r, err)
		return
	}

	envelope := artifacts.Envelope("artifacts")
	envelope["task_id"] = taskID
	RespondJSON(w, r, http.StatusOK, envelope)
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
