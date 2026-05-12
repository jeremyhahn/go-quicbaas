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

package scheduler

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Scheduler"
)

// Client provides scheduler operations.
type Client interface {
	// CreateJob creates a new scheduled job.
	CreateJob(ctx context.Context, req *CreateJobRequest) (*CreateJobResponse, error)

	// GetJob retrieves a job by ID.
	GetJob(ctx context.Context, jobID uint64) (*JobInfo, error)

	// UpdateJob updates an existing job.
	UpdateJob(ctx context.Context, req *UpdateJobRequest) (*JobInfo, error)

	// DeleteJob removes a scheduled job.
	DeleteJob(ctx context.Context, jobID uint64) error

	// ListJobs returns jobs matching the query criteria.
	ListJobs(ctx context.Context, opts *ListJobsOptions) (*ListJobsResponse, error)

	// TriggerJob immediately executes a job.
	TriggerJob(ctx context.Context, jobID uint64) (*TriggerJobResponse, error)

	// PauseJob pauses a scheduled job.
	PauseJob(ctx context.Context, jobID uint64) (*PauseJobResponse, error)

	// ResumeJob resumes a paused job.
	ResumeJob(ctx context.Context, jobID uint64) (*ResumeJobResponse, error)

	// CancelJob cancels a scheduled job.
	CancelJob(ctx context.Context, jobID uint64, reason string) (*CancelJobResponse, error)

	// GetJobHistory returns execution history for a job.
	GetJobHistory(ctx context.Context, jobID uint64, opts *HistoryOptions) (*JobHistoryResponse, error)

	// GetStatus returns scheduler status and statistics.
	GetStatus(ctx context.Context) (*SchedulerStatus, error)
}

// JobType represents the type of scheduled job.
type JobType int

const (
	// JobTypeOneShot executes once and completes.
	JobTypeOneShot JobType = iota
	// JobTypeRecurring executes on a cron schedule.
	JobTypeRecurring
	// JobTypeDelayed executes after a specified delay.
	JobTypeDelayed
)

// String returns the string representation of the job type.
func (t JobType) String() string {
	switch t {
	case JobTypeOneShot:
		return "ONE_SHOT"
	case JobTypeRecurring:
		return "RECURRING"
	case JobTypeDelayed:
		return "DELAYED"
	default:
		return "UNKNOWN"
	}
}

// JobStatus represents the current status of a job.
type JobStatus int

const (
	// JobStatusPending indicates the job is scheduled but not yet running.
	JobStatusPending JobStatus = iota
	// JobStatusRunning indicates the job is currently executing.
	JobStatusRunning
	// JobStatusCompleted indicates the job finished successfully.
	JobStatusCompleted
	// JobStatusFailed indicates the job execution failed.
	JobStatusFailed
	// JobStatusPaused indicates the job is paused and will not execute.
	JobStatusPaused
	// JobStatusCanceled indicates the job has been canceled.
	JobStatusCanceled
)

// String returns the string representation of the job status.
func (s JobStatus) String() string {
	switch s {
	case JobStatusPending:
		return "PENDING"
	case JobStatusRunning:
		return "RUNNING"
	case JobStatusCompleted:
		return "COMPLETED"
	case JobStatusFailed:
		return "FAILED"
	case JobStatusPaused:
		return "PAUSED"
	case JobStatusCanceled:
		return "CANCELED"
	default:
		return "UNKNOWN"
	}
}

// OwnerType represents the type of job owner.
type OwnerType int

const (
	// OwnerTypeSystem indicates a system-level job.
	OwnerTypeSystem OwnerType = iota
	// OwnerTypeTenant indicates a tenant-specific job.
	OwnerTypeTenant
	// OwnerTypeDelegated indicates a job delegated to another entity.
	OwnerTypeDelegated
)

// String returns the string representation of the owner type.
func (o OwnerType) String() string {
	switch o {
	case OwnerTypeSystem:
		return "SYSTEM"
	case OwnerTypeTenant:
		return "TENANT"
	case OwnerTypeDelegated:
		return "DELEGATED"
	default:
		return "UNKNOWN"
	}
}

// JobInfo contains information about a scheduled job.
type JobInfo struct {
	JobID          uint64
	Name           string
	Description    string
	JobType        JobType
	Status         JobStatus
	Schedule       string
	Delay          time.Duration
	Payload        []byte
	OwnerType      OwnerType
	OwnerID        string
	MaxRetries     int
	RetryCount     int
	Timeout        time.Duration
	NextRunAt      time.Time
	LastRunAt      time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExecutionCount uint64
	FailureCount   uint64
	Tags           map[string]string
}

// CreateJobRequest is a request to create a scheduled job.
type CreateJobRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	JobType     JobType           `json:"type"`
	Handler     string            `json:"handler"`
	Schedule    string            `json:"schedule,omitempty"`
	ExecuteAt   time.Time         `json:"execute_at,omitempty"`
	Delay       time.Duration     `json:"delay,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	OwnerType   OwnerType         `json:"owner_type"`
	OwnerID     string            `json:"owner_id,omitempty"`
	MaxRetries  int               `json:"max_retries,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`

	// NodeAffinity restricts execution to the node whose
	// SchedulerEngine was constructed with a matching node ID.
	// Empty (default) makes the job cluster-wide — it executes on
	// whichever node is currently the Raft leader. Set to the
	// creating node's ID (OwnerID is a common choice when
	// OwnerType=System) to pin execution to that node regardless of
	// leadership; this is the correct setting for jobs that depend
	// on node-local state (e.g., certs whose private keys live only
	// on the local node).
	NodeAffinity string `json:"node_affinity,omitempty"`
}

// CreateJobResponse is the response from job creation.
type CreateJobResponse struct {
	JobID     uint64
	NextRunAt time.Time
}

// UpdateJobRequest is a request to update an existing job.
type UpdateJobRequest struct {
	JobID       uint64            `json:"job_id"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Schedule    string            `json:"schedule,omitempty"`
	Delay       time.Duration     `json:"delay,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	MaxRetries  int               `json:"max_retries,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// ListJobsOptions specifies filtering options for listing jobs.
type ListJobsOptions struct {
	OwnerType *OwnerType        `json:"owner_type,omitempty"`
	OwnerID   string            `json:"owner_id,omitempty"`
	Status    *JobStatus        `json:"status,omitempty"`
	JobType   *JobType          `json:"job_type,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	Limit     int               `json:"limit,omitempty"`
	Offset    int               `json:"offset,omitempty"`
}

// ListJobsResponse is the response from listing jobs.
type ListJobsResponse struct {
	Jobs       []JobInfo
	TotalCount int
	HasMore    bool
}

// TriggerJobResponse is the response from triggering a job.
type TriggerJobResponse struct {
	JobID       uint64
	ExecutionID uint64
	StartedAt   time.Time
}

// PauseJobResponse is the response from pausing a job.
type PauseJobResponse struct {
	JobID    uint64
	PausedAt time.Time
}

// ResumeJobResponse is the response from resuming a job.
type ResumeJobResponse struct {
	JobID     uint64
	ResumedAt time.Time
	NextRunAt time.Time
}

// CancelJobResponse is the response from canceling a job.
type CancelJobResponse struct {
	JobID      uint64
	CanceledAt time.Time
	Reason     string
}

// HistoryOptions specifies options for retrieving job history.
type HistoryOptions struct {
	Limit     int
	Offset    int
	StartTime *time.Time
	EndTime   *time.Time
	Status    *JobStatus
}

// JobHistoryResponse is the response from getting job history.
type JobHistoryResponse struct {
	JobID      uint64
	Executions []JobExecution
	TotalCount int
	HasMore    bool
}

// JobExecution represents a single execution of a job.
type JobExecution struct {
	ExecutionID uint64
	JobID       uint64
	Status      JobStatus
	StartedAt   time.Time
	CompletedAt time.Time
	Duration    time.Duration
	RetryCount  int
	Error       string
	Result      []byte
}

// SchedulerStatus represents the scheduler's current status and statistics.
type SchedulerStatus struct {
	Healthy         bool
	TotalJobs       uint64
	PendingJobs     uint64
	RunningJobs     uint64
	CompletedJobs   uint64
	FailedJobs      uint64
	PausedJobs      uint64
	CanceledJobs    uint64
	ExecutionsToday uint64
	FailuresToday   uint64
	AverageLatency  time.Duration
	WorkerCount     int
	QueueDepth      int
	LastHeartbeat   time.Time
	UptimeDuration  time.Duration
}

// Internal request/response types

type getJobRequest struct {
	JobID    uint64
	TenantID string `json:"tenant_id,omitempty"`
}

type deleteJobRequest struct {
	JobID    uint64
	TenantID string `json:"tenant_id,omitempty"`
}

type deleteJobResponse struct{}

type triggerJobRequest struct {
	JobID    uint64
	TenantID string `json:"tenant_id,omitempty"`
}

type pauseJobRequest struct {
	JobID    uint64
	TenantID string `json:"tenant_id,omitempty"`
}

type resumeJobRequest struct {
	JobID    uint64
	TenantID string `json:"tenant_id,omitempty"`
}

type cancelJobRequest struct {
	JobID    uint64
	TenantID string `json:"tenant_id,omitempty"`
	Reason   string
}

type getJobHistoryRequest struct {
	JobID    uint64
	TenantID string `json:"tenant_id,omitempty"`
	Options  *HistoryOptions
}

type getStatusRequest struct{}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new scheduler client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// CreateJob creates a new scheduled job.
func (c *client) CreateJob(ctx context.Context, req *CreateJobRequest) (*CreateJobResponse, error) {
	if err := validateCreateJobRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.CreateJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateJob", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("scheduler.CreateJob", "nil response from server")
	}

	createResp, ok := resp.(*CreateJobResponse)
	if !ok {
		return nil, errors.NewInternal("scheduler.CreateJob", "invalid response type")
	}

	return createResp, nil
}

// GetJob retrieves a job by ID.
func (c *client) GetJob(ctx context.Context, jobID uint64) (*JobInfo, error) {
	if jobID == 0 {
		return nil, errors.NewInvalidInput("scheduler.GetJob", "job ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.GetJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetJob", &getJobRequest{JobID: jobID})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("scheduler.GetJob", "job not found")
	}

	jobInfo, ok := resp.(*JobInfo)
	if !ok {
		return nil, errors.NewInternal("scheduler.GetJob", "invalid response type")
	}

	return jobInfo, nil
}

// UpdateJob updates an existing job.
func (c *client) UpdateJob(ctx context.Context, req *UpdateJobRequest) (*JobInfo, error) {
	if err := validateUpdateJobRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.UpdateJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpdateJob", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("scheduler.UpdateJob", "nil response from server")
	}

	jobInfo, ok := resp.(*JobInfo)
	if !ok {
		return nil, errors.NewInternal("scheduler.UpdateJob", "invalid response type")
	}

	return jobInfo, nil
}

// DeleteJob removes a scheduled job.
func (c *client) DeleteJob(ctx context.Context, jobID uint64) error {
	if jobID == 0 {
		return errors.NewInvalidInput("scheduler.DeleteJob", "job ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("scheduler.DeleteJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteJob", &deleteJobRequest{JobID: jobID})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*deleteJobResponse); !ok {
		return errors.NewInternal("scheduler.DeleteJob", "invalid response type")
	}

	return nil
}

// ListJobs returns jobs matching the query criteria.
func (c *client) ListJobs(ctx context.Context, opts *ListJobsOptions) (*ListJobsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.ListJobs", "context is required")
	}

	if err := validateListJobsOptions(opts); err != nil {
		return nil, err
	}

	// Use empty options if nil
	if opts == nil {
		opts = &ListJobsOptions{}
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListJobs", opts)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListJobsResponse{Jobs: []JobInfo{}}, nil
	}

	listResp, ok := resp.(*ListJobsResponse)
	if !ok {
		return nil, errors.NewInternal("scheduler.ListJobs", "invalid response type")
	}

	return listResp, nil
}

// TriggerJob immediately executes a job.
func (c *client) TriggerJob(ctx context.Context, jobID uint64) (*TriggerJobResponse, error) {
	if jobID == 0 {
		return nil, errors.NewInvalidInput("scheduler.TriggerJob", "job ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.TriggerJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "TriggerJob", &triggerJobRequest{JobID: jobID})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("scheduler.TriggerJob", "nil response from server")
	}

	triggerResp, ok := resp.(*TriggerJobResponse)
	if !ok {
		return nil, errors.NewInternal("scheduler.TriggerJob", "invalid response type")
	}

	return triggerResp, nil
}

// PauseJob pauses a scheduled job.
func (c *client) PauseJob(ctx context.Context, jobID uint64) (*PauseJobResponse, error) {
	if jobID == 0 {
		return nil, errors.NewInvalidInput("scheduler.PauseJob", "job ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.PauseJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "PauseJob", &pauseJobRequest{JobID: jobID})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("scheduler.PauseJob", "nil response from server")
	}

	pauseResp, ok := resp.(*PauseJobResponse)
	if !ok {
		return nil, errors.NewInternal("scheduler.PauseJob", "invalid response type")
	}

	return pauseResp, nil
}

// ResumeJob resumes a paused job.
func (c *client) ResumeJob(ctx context.Context, jobID uint64) (*ResumeJobResponse, error) {
	if jobID == 0 {
		return nil, errors.NewInvalidInput("scheduler.ResumeJob", "job ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.ResumeJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ResumeJob", &resumeJobRequest{JobID: jobID})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("scheduler.ResumeJob", "nil response from server")
	}

	resumeResp, ok := resp.(*ResumeJobResponse)
	if !ok {
		return nil, errors.NewInternal("scheduler.ResumeJob", "invalid response type")
	}

	return resumeResp, nil
}

// CancelJob cancels a scheduled job.
func (c *client) CancelJob(ctx context.Context, jobID uint64, reason string) (*CancelJobResponse, error) {
	if jobID == 0 {
		return nil, errors.NewInvalidInput("scheduler.CancelJob", "job ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.CancelJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CancelJob", &cancelJobRequest{JobID: jobID, Reason: reason})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("scheduler.CancelJob", "nil response from server")
	}

	cancelResp, ok := resp.(*CancelJobResponse)
	if !ok {
		return nil, errors.NewInternal("scheduler.CancelJob", "invalid response type")
	}

	return cancelResp, nil
}

// GetJobHistory returns execution history for a job.
func (c *client) GetJobHistory(ctx context.Context, jobID uint64, opts *HistoryOptions) (*JobHistoryResponse, error) {
	if jobID == 0 {
		return nil, errors.NewInvalidInput("scheduler.GetJobHistory", "job ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.GetJobHistory", "context is required")
	}

	if err := validateHistoryOptions(opts); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetJobHistory", &getJobHistoryRequest{JobID: jobID, Options: opts})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &JobHistoryResponse{JobID: jobID, Executions: []JobExecution{}}, nil
	}

	historyResp, ok := resp.(*JobHistoryResponse)
	if !ok {
		return nil, errors.NewInternal("scheduler.GetJobHistory", "invalid response type")
	}

	return historyResp, nil
}

// GetStatus returns scheduler status and statistics.
func (c *client) GetStatus(ctx context.Context) (*SchedulerStatus, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("scheduler.GetStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetStatus", &getStatusRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("scheduler.GetStatus", "nil response from server")
	}

	statusResp, ok := resp.(*SchedulerStatus)
	if !ok {
		return nil, errors.NewInternal("scheduler.GetStatus", "invalid response type")
	}

	return statusResp, nil
}

// Validation functions

func validateCreateJobRequest(req *CreateJobRequest) error {
	if req == nil {
		return errors.NewInvalidInput("scheduler.CreateJob", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("scheduler.CreateJob", "job name is required")
	}

	switch req.JobType {
	case JobTypeOneShot, JobTypeRecurring, JobTypeDelayed:
		// Valid job types
	default:
		return errors.NewInvalidInput("scheduler.CreateJob", "invalid job type").
			WithContext("job_type", int(req.JobType))
	}

	switch req.OwnerType {
	case OwnerTypeSystem, OwnerTypeTenant, OwnerTypeDelegated:
		// Valid owner types
	default:
		return errors.NewInvalidInput("scheduler.CreateJob", "invalid owner type").
			WithContext("owner_type", int(req.OwnerType))
	}

	// Recurring jobs require a schedule
	if req.JobType == JobTypeRecurring && req.Schedule == "" {
		return errors.NewInvalidInput("scheduler.CreateJob", "schedule is required for recurring jobs")
	}

	// Delayed jobs require a delay
	if req.JobType == JobTypeDelayed && req.Delay <= 0 {
		return errors.NewInvalidInput("scheduler.CreateJob", "delay is required for delayed jobs")
	}

	if req.MaxRetries < 0 {
		return errors.NewInvalidInput("scheduler.CreateJob", "max retries cannot be negative")
	}

	if req.Timeout < 0 {
		return errors.NewInvalidInput("scheduler.CreateJob", "timeout cannot be negative")
	}

	return nil
}

func validateUpdateJobRequest(req *UpdateJobRequest) error {
	if req == nil {
		return errors.NewInvalidInput("scheduler.UpdateJob", "request is required")
	}
	if req.JobID == 0 {
		return errors.NewInvalidInput("scheduler.UpdateJob", "job ID is required")
	}

	if req.MaxRetries < 0 {
		return errors.NewInvalidInput("scheduler.UpdateJob", "max retries cannot be negative")
	}

	if req.Timeout < 0 {
		return errors.NewInvalidInput("scheduler.UpdateJob", "timeout cannot be negative")
	}

	return nil
}

func validateListJobsOptions(opts *ListJobsOptions) error {
	if opts == nil {
		return nil
	}

	if opts.OwnerType != nil {
		switch *opts.OwnerType {
		case OwnerTypeSystem, OwnerTypeTenant, OwnerTypeDelegated:
			// Valid owner types
		default:
			return errors.NewInvalidInput("scheduler.ListJobs", "invalid owner type filter").
				WithContext("owner_type", int(*opts.OwnerType))
		}
	}

	if opts.Status != nil {
		switch *opts.Status {
		case JobStatusPending, JobStatusRunning, JobStatusCompleted,
			JobStatusFailed, JobStatusPaused, JobStatusCanceled:
			// Valid statuses
		default:
			return errors.NewInvalidInput("scheduler.ListJobs", "invalid status filter").
				WithContext("status", int(*opts.Status))
		}
	}

	if opts.JobType != nil {
		switch *opts.JobType {
		case JobTypeOneShot, JobTypeRecurring, JobTypeDelayed:
			// Valid job types
		default:
			return errors.NewInvalidInput("scheduler.ListJobs", "invalid job type filter").
				WithContext("job_type", int(*opts.JobType))
		}
	}

	if opts.Limit < 0 {
		return errors.NewInvalidInput("scheduler.ListJobs", "limit cannot be negative")
	}

	if opts.Offset < 0 {
		return errors.NewInvalidInput("scheduler.ListJobs", "offset cannot be negative")
	}

	return nil
}

func validateHistoryOptions(opts *HistoryOptions) error {
	if opts == nil {
		return nil
	}

	if opts.Limit < 0 {
		return errors.NewInvalidInput("scheduler.GetJobHistory", "limit cannot be negative")
	}

	if opts.Offset < 0 {
		return errors.NewInvalidInput("scheduler.GetJobHistory", "offset cannot be negative")
	}

	if opts.StartTime != nil && opts.EndTime != nil {
		if opts.StartTime.After(*opts.EndTime) {
			return errors.NewInvalidInput("scheduler.GetJobHistory", "start time cannot be after end time")
		}
	}

	if opts.Status != nil {
		switch *opts.Status {
		case JobStatusPending, JobStatusRunning, JobStatusCompleted,
			JobStatusFailed, JobStatusPaused, JobStatusCanceled:
			// Valid statuses
		default:
			return errors.NewInvalidInput("scheduler.GetJobHistory", "invalid status filter").
				WithContext("status", int(*opts.Status))
		}
	}

	return nil
}
