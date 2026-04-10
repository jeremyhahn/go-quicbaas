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
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Route Registration
// =============================================================================

// registerSchedulerRoutes registers all scheduler REST API routes.
func registerSchedulerRoutes(s *Server, r chi.Router) {
	r.Route("/scheduler", func(r chi.Router) {
		// Job management routes
		r.Post("/jobs", s.HandleCreateSchedulerJob)                 // Create job
		r.Get("/jobs", s.HandleListSchedulerJobs)                   // List jobs
		r.Get("/jobs/{id}", s.HandleGetSchedulerJob)                // Get job
		r.Put("/jobs/{id}", s.HandleUpdateSchedulerJob)             // Update job
		r.Delete("/jobs/{id}", s.HandleDeleteSchedulerJob)          // Delete job
		r.Post("/jobs/{id}/trigger", s.HandleTriggerSchedulerJob)   // Trigger immediately
		r.Post("/jobs/{id}/pause", s.HandlePauseSchedulerJob)       // Pause job
		r.Post("/jobs/{id}/resume", s.HandleResumeSchedulerJob)     // Resume job
		r.Post("/jobs/{id}/cancel", s.HandleCancelSchedulerJob)     // Cancel job
		r.Get("/jobs/{id}/history", s.HandleGetSchedulerJobHistory) // Execution history

		// Scheduler status
		r.Get("/status", s.HandleGetSchedulerStatus) // Scheduler status
	})
}

// =============================================================================
// Request/Response Types for Scheduler REST API
// =============================================================================

// CreateSchedulerJobRESTRequest represents the JSON request body for creating a job.
type CreateSchedulerJobRESTRequest struct {
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	OwnerType      string            `json:"owner_type,omitempty"`
	OwnerID        string            `json:"owner_id,omitempty"`
	TargetTenantID string            `json:"target_tenant_id,omitempty"`
	Type           string            `json:"type"`
	Schedule       string            `json:"schedule,omitempty"`
	ExecuteAt      string            `json:"execute_at,omitempty"`
	DelaySeconds   int64             `json:"delay_seconds,omitempty"`
	Delay          time.Duration     `json:"delay,omitempty"` // Alternative: accepts time.Duration directly
	Handler        string            `json:"handler"`
	Payload        json.RawMessage   `json:"payload,omitempty"`
	Priority       int32             `json:"priority,omitempty"`
	TimeoutSeconds int64             `json:"timeout_seconds,omitempty"`
	Timeout        time.Duration     `json:"timeout,omitempty"` // Alternative: accepts time.Duration directly
	MaxRetries     int32             `json:"max_retries,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// UpdateSchedulerJobRESTRequest represents the JSON request body for updating a job.
type UpdateSchedulerJobRESTRequest struct {
	Name           string            `json:"name,omitempty"`
	Description    string            `json:"description,omitempty"`
	Schedule       string            `json:"schedule,omitempty"`
	ExecuteAt      string            `json:"execute_at,omitempty"`
	Handler        string            `json:"handler,omitempty"`
	Payload        json.RawMessage   `json:"payload,omitempty"`
	Priority       int32             `json:"priority,omitempty"`
	TimeoutSeconds int64             `json:"timeout_seconds,omitempty"`
	MaxRetries     int32             `json:"max_retries,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// CancelSchedulerJobRESTRequest represents the JSON request body for canceling a job.
type CancelSchedulerJobRESTRequest struct {
	Reason string `json:"reason,omitempty"`
}

// SchedulerJobRESTResponse represents a job in REST response.
type SchedulerJobRESTResponse struct {
	ID             uint64            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	OwnerType      string            `json:"owner_type"`
	OwnerID        string            `json:"owner_id,omitempty"`
	TargetTenantID string            `json:"target_tenant_id,omitempty"`
	CreatedBy      string            `json:"created_by,omitempty"`
	Type           string            `json:"type"`
	Schedule       string            `json:"schedule,omitempty"`
	ExecuteAt      time.Time         `json:"execute_at,omitempty"`
	Delay          time.Duration     `json:"delay,omitempty"`
	Handler        string            `json:"handler"`
	Payload        json.RawMessage   `json:"payload,omitempty"`
	Priority       int32             `json:"priority"`
	Timeout        time.Duration     `json:"timeout"`
	MaxRetries     int32             `json:"max_retries"`
	Status         string            `json:"status"`
	LastRun        time.Time         `json:"last_run,omitempty"`
	NextRun        time.Time         `json:"next_run,omitempty"`
	RunCount       uint64            `json:"run_count"`
	FailCount      uint64            `json:"fail_count"`
	LastError      string            `json:"last_error,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// CreateSchedulerJobRESTResponse represents the JSON response for job creation.
type CreateSchedulerJobRESTResponse struct {
	Job       *SchedulerJobRESTResponse `json:"job"`
	Timestamp time.Time                 `json:"timestamp"`
}

// GetSchedulerJobRESTResponse represents the JSON response for getting a job.
type GetSchedulerJobRESTResponse struct {
	Job       *SchedulerJobRESTResponse `json:"job"`
	Timestamp time.Time                 `json:"timestamp"`
}

// UpdateSchedulerJobRESTResponse represents the JSON response for updating a job.
type UpdateSchedulerJobRESTResponse struct {
	Job       *SchedulerJobRESTResponse `json:"job"`
	Timestamp time.Time                 `json:"timestamp"`
}

// DeleteSchedulerJobRESTResponse represents the JSON response for deleting a job.
type DeleteSchedulerJobRESTResponse struct {
	JobID     uint64    `json:"job_id"`
	Deleted   bool      `json:"deleted"`
	Timestamp time.Time `json:"timestamp"`
}

// ListSchedulerJobsRESTResponse represents the JSON response for listing jobs.
type ListSchedulerJobsRESTResponse struct {
	Jobs       []*SchedulerJobRESTResponse `json:"jobs"`
	NextCursor string                      `json:"next_cursor,omitempty"`
	HasMore    bool                        `json:"has_more"`
	TotalCount int                         `json:"total_count,omitempty"`
	Timestamp  time.Time                   `json:"timestamp"`
}

// TriggerSchedulerJobRESTResponse represents the JSON response for triggering a job.
type TriggerSchedulerJobRESTResponse struct {
	JobID     uint64    `json:"job_id"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// PauseSchedulerJobRESTResponse represents the JSON response for pausing a job.
type PauseSchedulerJobRESTResponse struct {
	JobID     uint64    `json:"job_id"`
	OldStatus string    `json:"old_status"`
	NewStatus string    `json:"new_status"`
	Timestamp time.Time `json:"timestamp"`
}

// ResumeSchedulerJobRESTResponse represents the JSON response for resuming a job.
type ResumeSchedulerJobRESTResponse struct {
	JobID     uint64    `json:"job_id"`
	OldStatus string    `json:"old_status"`
	NewStatus string    `json:"new_status"`
	Timestamp time.Time `json:"timestamp"`
}

// CancelSchedulerJobRESTResponse represents the JSON response for canceling a job.
type CancelSchedulerJobRESTResponse struct {
	JobID     uint64    `json:"job_id"`
	OldStatus string    `json:"old_status"`
	NewStatus string    `json:"new_status"`
	Timestamp time.Time `json:"timestamp"`
}

// SchedulerJobExecutionRESTResponse represents a job execution record in REST response.
type SchedulerJobExecutionRESTResponse struct {
	ID        uint64        `json:"id"`
	JobID     uint64        `json:"job_id"`
	Status    string        `json:"status"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	Error     string        `json:"error,omitempty"`
	Output    string        `json:"output,omitempty"`
}

// GetSchedulerJobHistoryRESTResponse represents the JSON response for getting job history.
type GetSchedulerJobHistoryRESTResponse struct {
	Executions []*SchedulerJobExecutionRESTResponse `json:"executions"`
	NextCursor string                               `json:"next_cursor,omitempty"`
	HasMore    bool                                 `json:"has_more"`
	Timestamp  time.Time                            `json:"timestamp"`
}

// SchedulerStatsRESTResponse represents scheduler statistics in REST response.
type SchedulerStatsRESTResponse struct {
	JobCount     uint64    `json:"job_count"`
	RunningJobs  uint64    `json:"running_jobs"`
	SuccessCount uint64    `json:"success_count"`
	FailureCount uint64    `json:"failure_count"`
	LastTickTime time.Time `json:"last_tick_time,omitempty"`
	StartedAt    time.Time `json:"started_at"`
}

// GetSchedulerStatusRESTResponse represents the JSON response for scheduler status.
type GetSchedulerStatusRESTResponse struct {
	Running   bool                        `json:"running"`
	Stats     *SchedulerStatsRESTResponse `json:"stats,omitempty"`
	Timestamp time.Time                   `json:"timestamp"`
}

// =============================================================================
// Handler Functions
// =============================================================================

// HandleCreateSchedulerJob handles POST /api/v1/scheduler/jobs - Create a new job.
func (s *Server) HandleCreateSchedulerJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req CreateSchedulerJobRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Name == "" {
		RespondBadRequest(w, r, "job name is required")
		return
	}

	if req.Handler == "" {
		RespondBadRequest(w, r, "job handler is required")
		return
	}

	if req.Type == "" {
		RespondBadRequest(w, r, "job type is required")
		return
	}

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	// Parse execute_at timestamp if provided
	var executeAt time.Time
	if req.ExecuteAt != "" {
		executeAt, err = time.Parse(time.RFC3339, req.ExecuteAt)
		if err != nil {
			RespondBadRequest(w, r, "invalid execute_at format, expected RFC3339")
			return
		}
	}

	// Calculate delay from either delay_seconds or delay (time.Duration)
	delay := time.Duration(req.DelaySeconds) * time.Second
	if delay == 0 && req.Delay > 0 {
		delay = req.Delay
	}

	// Calculate timeout from either timeout_seconds or timeout (time.Duration)
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 && req.Timeout > 0 {
		timeout = req.Timeout
	}

	// Build service request
	svcReq := &service.CreateSchedulerJobRequest{
		Name:           req.Name,
		Description:    req.Description,
		OwnerType:      service.SchedulerOwnerType(req.OwnerType),
		OwnerID:        req.OwnerID,
		TargetTenantID: req.TargetTenantID,
		Type:           service.SchedulerJobType(req.Type),
		Schedule:       req.Schedule,
		ExecuteAt:      executeAt,
		Delay:          delay,
		Handler:        req.Handler,
		Payload:        req.Payload,
		Priority:       req.Priority,
		Timeout:        timeout,
		MaxRetries:     req.MaxRetries,
		Labels:         req.Labels,
	}

	svcResp, err := schedulerService.CreateJob(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &CreateSchedulerJobRESTResponse{
		Job:       schedulerJobToREST(svcResp.Job),
		Timestamp: svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleListSchedulerJobs handles GET /api/v1/scheduler/jobs - List jobs.
func (s *Server) HandleListSchedulerJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	// Parse query parameters
	svcReq := &service.ListSchedulerJobsRequest{}

	if ownerType := r.URL.Query().Get("owner_type"); ownerType != "" {
		svcReq.OwnerType = service.SchedulerOwnerType(ownerType)
	}
	if ownerID := r.URL.Query().Get("owner_id"); ownerID != "" {
		svcReq.OwnerID = ownerID
	}
	if targetTenantID := r.URL.Query().Get("target_tenant_id"); targetTenantID != "" {
		svcReq.TargetTenantID = targetTenantID
	}
	if status := r.URL.Query().Get("status"); status != "" {
		svcReq.Status = service.SchedulerJobStatus(status)
	}
	if handler := r.URL.Query().Get("handler"); handler != "" {
		svcReq.Handler = handler
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err == nil && limit > 0 && limit <= 1000 {
			svcReq.Limit = limit
		}
	}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		svcReq.Cursor = cursor
	}

	svcResp, err := schedulerService.ListJobs(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	jobs := make([]*SchedulerJobRESTResponse, 0, len(svcResp.Jobs))
	for _, job := range svcResp.Jobs {
		jobs = append(jobs, schedulerJobToREST(job))
	}

	resp := &ListSchedulerJobsRESTResponse{
		Jobs:       jobs,
		NextCursor: svcResp.NextCursor,
		HasMore:    svcResp.HasMore,
		TotalCount: svcResp.TotalCount,
		Timestamp:  svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetSchedulerJob handles GET /api/v1/scheduler/jobs/{id} - Get job by ID.
func (s *Server) HandleGetSchedulerJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobIDStr := chi.URLParam(r, "id")
	if jobIDStr == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	jobID, err := strconv.ParseUint(jobIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid job ID format")
		return
	}

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	svcReq := &service.GetSchedulerJobRequest{
		JobID:    jobID,
		TenantID: r.URL.Query().Get("tenant_id"),
	}

	svcResp, err := schedulerService.GetJob(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &GetSchedulerJobRESTResponse{
		Job:       schedulerJobToREST(svcResp.Job),
		Timestamp: svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleUpdateSchedulerJob handles PUT /api/v1/scheduler/jobs/{id} - Update job.
func (s *Server) HandleUpdateSchedulerJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobIDStr := chi.URLParam(r, "id")
	if jobIDStr == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	jobID, err := strconv.ParseUint(jobIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid job ID format")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req UpdateSchedulerJobRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	// Parse execute_at timestamp if provided
	var executeAt time.Time
	if req.ExecuteAt != "" {
		executeAt, err = time.Parse(time.RFC3339, req.ExecuteAt)
		if err != nil {
			RespondBadRequest(w, r, "invalid execute_at format, expected RFC3339")
			return
		}
	}

	svcReq := &service.UpdateSchedulerJobRequest{
		JobID:       jobID,
		TenantID:    r.URL.Query().Get("tenant_id"),
		Name:        req.Name,
		Description: req.Description,
		Schedule:    req.Schedule,
		ExecuteAt:   executeAt,
		Handler:     req.Handler,
		Payload:     req.Payload,
		Priority:    req.Priority,
		Timeout:     time.Duration(req.TimeoutSeconds) * time.Second,
		MaxRetries:  req.MaxRetries,
		Labels:      req.Labels,
	}

	svcResp, err := schedulerService.UpdateJob(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &UpdateSchedulerJobRESTResponse{
		Job:       schedulerJobToREST(svcResp.Job),
		Timestamp: svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteSchedulerJob handles DELETE /api/v1/scheduler/jobs/{id} - Delete job.
func (s *Server) HandleDeleteSchedulerJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobIDStr := chi.URLParam(r, "id")
	if jobIDStr == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	jobID, err := strconv.ParseUint(jobIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid job ID format")
		return
	}

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	// Parse optional force flag from query or body
	force := r.URL.Query().Get("force") == "true"

	svcReq := &service.DeleteSchedulerJobRequest{
		JobID:    jobID,
		TenantID: r.URL.Query().Get("tenant_id"),
		Force:    force,
	}

	svcResp, err := schedulerService.DeleteJob(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &DeleteSchedulerJobRESTResponse{
		JobID:     svcResp.JobID,
		Deleted:   svcResp.Deleted,
		Timestamp: svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleTriggerSchedulerJob handles POST /api/v1/scheduler/jobs/{id}/trigger - Trigger job immediately.
func (s *Server) HandleTriggerSchedulerJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobIDStr := chi.URLParam(r, "id")
	if jobIDStr == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	jobID, err := strconv.ParseUint(jobIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid job ID format")
		return
	}

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	svcReq := &service.TriggerSchedulerJobRequest{
		JobID:    jobID,
		TenantID: r.URL.Query().Get("tenant_id"),
	}

	svcResp, err := schedulerService.TriggerJob(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &TriggerSchedulerJobRESTResponse{
		JobID:     svcResp.JobID,
		Status:    string(svcResp.Status),
		Timestamp: svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandlePauseSchedulerJob handles POST /api/v1/scheduler/jobs/{id}/pause - Pause job.
func (s *Server) HandlePauseSchedulerJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobIDStr := chi.URLParam(r, "id")
	if jobIDStr == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	jobID, err := strconv.ParseUint(jobIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid job ID format")
		return
	}

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	svcReq := &service.PauseSchedulerJobRequest{
		JobID:    jobID,
		TenantID: r.URL.Query().Get("tenant_id"),
	}

	svcResp, err := schedulerService.PauseJob(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &PauseSchedulerJobRESTResponse{
		JobID:     svcResp.JobID,
		OldStatus: string(svcResp.OldStatus),
		NewStatus: string(svcResp.NewStatus),
		Timestamp: svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleResumeSchedulerJob handles POST /api/v1/scheduler/jobs/{id}/resume - Resume job.
func (s *Server) HandleResumeSchedulerJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobIDStr := chi.URLParam(r, "id")
	if jobIDStr == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	jobID, err := strconv.ParseUint(jobIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid job ID format")
		return
	}

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	svcReq := &service.ResumeSchedulerJobRequest{
		JobID:    jobID,
		TenantID: r.URL.Query().Get("tenant_id"),
	}

	svcResp, err := schedulerService.ResumeJob(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &ResumeSchedulerJobRESTResponse{
		JobID:     svcResp.JobID,
		OldStatus: string(svcResp.OldStatus),
		NewStatus: string(svcResp.NewStatus),
		Timestamp: svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCancelSchedulerJob handles POST /api/v1/scheduler/jobs/{id}/cancel - Cancel job.
func (s *Server) HandleCancelSchedulerJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobIDStr := chi.URLParam(r, "id")
	if jobIDStr == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	jobID, err := strconv.ParseUint(jobIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid job ID format")
		return
	}

	// Parse optional reason from body
	var cancelReq CancelSchedulerJobRESTRequest
	body, err := io.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		if jsonErr := json.Unmarshal(body, &cancelReq); jsonErr != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}
	defer r.Body.Close()

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	svcReq := &service.CancelSchedulerJobRequest{
		JobID:    jobID,
		TenantID: r.URL.Query().Get("tenant_id"),
		Reason:   cancelReq.Reason,
	}

	svcResp, err := schedulerService.CancelJob(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &CancelSchedulerJobRESTResponse{
		JobID:     svcResp.JobID,
		OldStatus: string(svcResp.OldStatus),
		NewStatus: string(svcResp.NewStatus),
		Timestamp: svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetSchedulerJobHistory handles GET /api/v1/scheduler/jobs/{id}/history - Get job execution history.
func (s *Server) HandleGetSchedulerJobHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	jobIDStr := chi.URLParam(r, "id")
	if jobIDStr == "" {
		RespondBadRequest(w, r, "job ID is required")
		return
	}

	jobID, err := strconv.ParseUint(jobIDStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "invalid job ID format")
		return
	}

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	svcReq := &service.GetSchedulerJobHistoryRequest{
		JobID:    jobID,
		TenantID: r.URL.Query().Get("tenant_id"),
	}

	// Parse pagination parameters
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, parseErr := strconv.Atoi(limitStr)
		if parseErr == nil && limit > 0 && limit <= 1000 {
			svcReq.Limit = limit
		}
	}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		svcReq.Cursor = cursor
	}

	svcResp, err := schedulerService.GetJobHistory(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	executions := make([]*SchedulerJobExecutionRESTResponse, 0, len(svcResp.Executions))
	for _, exec := range svcResp.Executions {
		executions = append(executions, schedulerExecutionToREST(exec))
	}

	resp := &GetSchedulerJobHistoryRESTResponse{
		Executions: executions,
		NextCursor: svcResp.NextCursor,
		HasMore:    svcResp.HasMore,
		Timestamp:  svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetSchedulerStatus handles GET /api/v1/scheduler/status - Get scheduler status.
func (s *Server) HandleGetSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	schedulerService := s.service.Scheduler()
	if schedulerService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrSchedulerServiceUnavailable)
		return
	}

	svcResp, err := schedulerService.GetStatus(ctx)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	var statsResp *SchedulerStatsRESTResponse
	if svcResp.Stats != nil {
		statsResp = &SchedulerStatsRESTResponse{
			JobCount:     svcResp.Stats.JobCount,
			RunningJobs:  svcResp.Stats.RunningJobs,
			SuccessCount: svcResp.Stats.SuccessCount,
			FailureCount: svcResp.Stats.FailureCount,
			LastTickTime: svcResp.Stats.LastTickTime,
			StartedAt:    svcResp.Stats.StartedAt,
		}
	}

	resp := &GetSchedulerStatusRESTResponse{
		Running:   svcResp.Running,
		Stats:     statsResp,
		Timestamp: svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Conversion Helpers
// =============================================================================

// schedulerJobToREST converts a service SchedulerJob to REST response format.
func schedulerJobToREST(job *service.SchedulerJob) *SchedulerJobRESTResponse {
	if job == nil {
		return nil
	}

	return &SchedulerJobRESTResponse{
		ID:             job.ID,
		Name:           job.Name,
		Description:    job.Description,
		OwnerType:      string(job.OwnerType),
		OwnerID:        job.OwnerID,
		TargetTenantID: job.TargetTenantID,
		CreatedBy:      job.CreatedBy,
		Type:           string(job.Type),
		Schedule:       job.Schedule,
		ExecuteAt:      job.ExecuteAt,
		Delay:          job.Delay,
		Handler:        job.Handler,
		Payload:        job.Payload,
		Priority:       job.Priority,
		Timeout:        job.Timeout,
		MaxRetries:     job.MaxRetries,
		Status:         string(job.Status),
		LastRun:        job.LastRun,
		NextRun:        job.NextRun,
		RunCount:       job.RunCount,
		FailCount:      job.FailCount,
		LastError:      job.LastError,
		Labels:         job.Labels,
		CreatedAt:      job.CreatedAt,
		UpdatedAt:      job.UpdatedAt,
	}
}

// schedulerExecutionToREST converts a service SchedulerJobExecution to REST response format.
func schedulerExecutionToREST(exec *service.SchedulerJobExecution) *SchedulerJobExecutionRESTResponse {
	if exec == nil {
		return nil
	}

	return &SchedulerJobExecutionRESTResponse{
		ID:        exec.ID,
		JobID:     exec.JobID,
		Status:    string(exec.Status),
		StartedAt: exec.StartedAt,
		EndedAt:   exec.EndedAt,
		Duration:  exec.Duration,
		Error:     exec.Error,
		Output:    exec.Output,
	}
}
