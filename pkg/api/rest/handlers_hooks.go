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
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Route Registration
// =============================================================================

// registerHookRoutes registers all Claude Code hook REST API routes.
func registerHookRoutes(s *Server, r chi.Router) {
	r.Route("/hooks", func(r chi.Router) {
		r.Post("/task-created", s.HandleHookTaskCreated)
		r.Post("/task-completed", s.HandleHookTaskCompleted)
		r.Post("/teammate-idle", s.HandleHookTeammateIdle)
	})
}

// =============================================================================
// Request/Response Types
// =============================================================================

// HookPayload is the common payload from Claude Code hooks.
type HookPayload struct {
	HookEventName   string `json:"hook_event_name"`
	TaskID          string `json:"task_id,omitempty"`
	TaskSubject     string `json:"task_subject,omitempty"`
	TaskDescription string `json:"task_description,omitempty"`
	TeammateName    string `json:"teammate_name,omitempty"`
	TeamName        string `json:"team_name,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	CWD             string `json:"cwd,omitempty"`
}

// HookResponse controls Claude Code behavior.
// Exit code 0 (or empty) = allow. Exit code 2 = block with feedback.
type HookResponse struct {
	ExitCode int    `json:"exit_code,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// =============================================================================
// Error Types
// =============================================================================

// HookPayloadError indicates an invalid or missing required field in a hook payload.
type HookPayloadError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *HookPayloadError) Error() string {
	return "hook payload error: " + e.Field + ": " + e.Message
}

// HookProcessingError indicates a failure during hook event processing
// (e.g. task store write failure, messaging publish failure).
type HookProcessingError struct {
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *HookProcessingError) Error() string {
	return "hook processing error: " + e.Operation + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As support.
func (e *HookProcessingError) Unwrap() error {
	return e.Cause
}

// HookServiceNotConfiguredError indicates the hook service dependencies are not configured.
type HookServiceNotConfiguredError struct {
	Component string
}

// Error implements the error interface.
func (e *HookServiceNotConfiguredError) Error() string {
	return "hook service not configured: " + e.Component
}

// =============================================================================
// Validation
// =============================================================================

// validateTaskCreatedPayload validates required fields for TaskCreated hooks.
func validateTaskCreatedPayload(p *HookPayload) error {
	if p.HookEventName == "" {
		return &HookPayloadError{Field: "hook_event_name", Message: "required"}
	}
	if p.TaskID == "" {
		return &HookPayloadError{Field: "task_id", Message: "required"}
	}
	if p.TeamName == "" {
		return &HookPayloadError{Field: "team_name", Message: "required"}
	}
	if p.TeammateName == "" {
		return &HookPayloadError{Field: "teammate_name", Message: "required"}
	}
	return nil
}

// validateTaskCompletedPayload validates required fields for TaskCompleted hooks.
func validateTaskCompletedPayload(p *HookPayload) error {
	if p.HookEventName == "" {
		return &HookPayloadError{Field: "hook_event_name", Message: "required"}
	}
	if p.TaskID == "" {
		return &HookPayloadError{Field: "task_id", Message: "required"}
	}
	if p.TeamName == "" {
		return &HookPayloadError{Field: "team_name", Message: "required"}
	}
	return nil
}

// validateTeammateIdlePayload validates required fields for TeammateIdle hooks.
func validateTeammateIdlePayload(p *HookPayload) error {
	if p.HookEventName == "" {
		return &HookPayloadError{Field: "hook_event_name", Message: "required"}
	}
	if p.TeammateName == "" {
		return &HookPayloadError{Field: "teammate_name", Message: "required"}
	}
	if p.TeamName == "" {
		return &HookPayloadError{Field: "team_name", Message: "required"}
	}
	return nil
}

// =============================================================================
// Service Accessors
// =============================================================================

// getHookTaskStore returns the task store from the Agent service, or responds
// with 501 Not Implemented when not configured.
func (s *Server) getHookTaskStore(w http.ResponseWriter, r *http.Request) orchestration.TaskStore {
	agentSvc := s.service.Agent()
	if agentSvc == nil {
		RespondError(w, r, http.StatusNotImplemented, &HookServiceNotConfiguredError{
			Component: "agent service",
		})
		return nil
	}

	// AgentService is available but TaskStore is accessed through the orchestrator.
	// Use the in-memory task store as a fallback via the hookTaskStore field.
	store := s.hookTaskStore
	if store == nil {
		RespondError(w, r, http.StatusNotImplemented, &HookServiceNotConfiguredError{
			Component: "task store",
		})
		return nil
	}
	return store
}

// getHookMessagingService returns the messaging service, or responds
// with 501 Not Implemented when not configured.
func (s *Server) getHookMessagingService(w http.ResponseWriter, r *http.Request) messaging.Service {
	messagingSvc := s.service.Messaging()
	if messagingSvc == nil {
		RespondError(w, r, http.StatusNotImplemented, &HookServiceNotConfiguredError{
			Component: "messaging service",
		})
		return nil
	}
	return messagingSvc
}

// =============================================================================
// Handler Functions
// =============================================================================

// HandleHookTaskCreated handles POST /api/v1/hooks/task-created.
// It receives Claude Code TaskCreated hook payloads, stores the task,
// and publishes a notification to the orchestrator messaging channel.
// Returns 200 OK to allow task creation, or 200 with exit_code=2 to block.
func (s *Server) HandleHookTaskCreated(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	taskStore := s.getHookTaskStore(w, r)
	if taskStore == nil {
		return
	}

	messagingSvc := s.getHookMessagingService(w, r)
	if messagingSvc == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var payload HookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if err := validateTaskCreatedPayload(&payload); err != nil {
		var hookErr *HookPayloadError
		if errors.As(err, &hookErr) {
			RespondBadRequest(w, r, hookErr.Error())
			return
		}
		RespondBadRequest(w, r, err.Error())
		return
	}

	// Extract tenant from auth context header or use team name as tenant namespace.
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		tenantID = payload.TeamName
	}

	now := time.Now().Unix()
	task := &orchestration.Task{
		ID:          payload.TaskID,
		TenantID:    tenantID,
		TeamName:    payload.TeamName,
		Subject:     payload.TaskSubject,
		Description: payload.TaskDescription,
		Status:      orchestration.TaskStatusPending,
		CreatedBy:   payload.TeammateName,
		Metadata: map[string]string{
			"session_id":      payload.SessionID,
			"cwd":             payload.CWD,
			"hook_event_name": payload.HookEventName,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := taskStore.CreateTask(ctx, task); err != nil {
		// If task already exists, block creation with feedback.
		if errors.Is(err, orchestration.ErrTaskAlreadyExists) {
			RespondJSON(w, r, http.StatusOK, &HookResponse{
				ExitCode: 2,
				Stderr:   "task already exists: " + payload.TaskID,
			})
			return
		}
		RespondInternalError(w, r, &HookProcessingError{
			Operation: "create task",
			Cause:     err,
		})
		return
	}

	// Publish task created event to messaging channel.
	channelName := "orchestrator:tasks:" + payload.TeamName
	eventPayload, err := json.Marshal(payload)
	if err != nil {
		RespondInternalError(w, r, &HookProcessingError{
			Operation: "marshal event payload",
			Cause:     err,
		})
		return
	}

	_, publishErr := messagingSvc.Publish(ctx, channelName, eventPayload)
	if publishErr != nil {
		// Log but do not fail the hook - task is stored successfully.
		s.logger.Warn("failed to publish task created event: channel=" + channelName + " task_id=" + payload.TaskID + " error=" + publishErr.Error())
	}

	// Return 200 OK with empty body to allow task creation (exit code 0).
	RespondJSON(w, r, http.StatusOK, &HookResponse{})
}

// HandleHookTaskCompleted handles POST /api/v1/hooks/task-completed.
// It receives Claude Code TaskCompleted hook payloads, marks the task as
// completed in the task store, and publishes a completion event.
func (s *Server) HandleHookTaskCompleted(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	taskStore := s.getHookTaskStore(w, r)
	if taskStore == nil {
		return
	}

	messagingSvc := s.getHookMessagingService(w, r)
	if messagingSvc == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var payload HookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if err := validateTaskCompletedPayload(&payload); err != nil {
		var hookErr *HookPayloadError
		if errors.As(err, &hookErr) {
			RespondBadRequest(w, r, hookErr.Error())
			return
		}
		RespondBadRequest(w, r, err.Error())
		return
	}

	// Extract tenant from auth context header or use team name as tenant namespace.
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		tenantID = payload.TeamName
	}

	// Complete the task in the store.
	result := &orchestration.TaskResult{
		Metadata: map[string]string{
			"session_id":      payload.SessionID,
			"teammate_name":   payload.TeammateName,
			"hook_event_name": payload.HookEventName,
		},
	}

	if err := taskStore.CompleteTask(ctx, tenantID, payload.TaskID, result); err != nil {
		if errors.Is(err, orchestration.ErrTaskNotFound) {
			RespondNotFound(w, r, "task: "+payload.TaskID)
			return
		}
		RespondInternalError(w, r, &HookProcessingError{
			Operation: "complete task",
			Cause:     err,
		})
		return
	}

	// Publish task completed event to messaging channel.
	channelName := "orchestrator:tasks:" + payload.TeamName
	eventPayload, err := json.Marshal(payload)
	if err != nil {
		RespondInternalError(w, r, &HookProcessingError{
			Operation: "marshal event payload",
			Cause:     err,
		})
		return
	}

	_, publishErr := messagingSvc.Publish(ctx, channelName, eventPayload)
	if publishErr != nil {
		s.logger.Warn("failed to publish task completed event: channel=" + channelName + " task_id=" + payload.TaskID + " error=" + publishErr.Error())
	}

	RespondJSON(w, r, http.StatusOK, &HookResponse{})
}

// HandleHookTeammateIdle handles POST /api/v1/hooks/teammate-idle.
// It receives Claude Code TeammateIdle hook payloads, checks for unclaimed
// tasks, and returns exit_code=2 to prevent idling when tasks are available.
func (s *Server) HandleHookTeammateIdle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	taskStore := s.getHookTaskStore(w, r)
	if taskStore == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var payload HookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if err := validateTeammateIdlePayload(&payload); err != nil {
		var hookErr *HookPayloadError
		if errors.As(err, &hookErr) {
			RespondBadRequest(w, r, hookErr.Error())
			return
		}
		RespondBadRequest(w, r, err.Error())
		return
	}

	// Extract tenant from auth context header or use team name as tenant namespace.
	tenantID := r.Header.Get("X-Tenant-ID")
	if tenantID == "" {
		tenantID = payload.TeamName
	}

	// Check for unclaimed (pending) tasks for this team.
	tasks, err := taskStore.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
		TeamName: payload.TeamName,
		Status:   orchestration.TaskStatusPending,
		Limit:    1,
	})
	if err != nil {
		RespondInternalError(w, r, &HookProcessingError{
			Operation: "list pending tasks",
			Cause:     err,
		})
		return
	}

	// If there are pending tasks, block idle with feedback.
	if len(tasks) > 0 {
		subject := tasks[0].Subject
		if subject == "" {
			subject = tasks[0].ID
		}
		RespondJSON(w, r, http.StatusOK, &HookResponse{
			ExitCode: 2,
			Stderr:   "New task available: " + subject,
		})
		return
	}

	// No pending tasks - allow idle.
	RespondJSON(w, r, http.StatusOK, &HookResponse{})
}
