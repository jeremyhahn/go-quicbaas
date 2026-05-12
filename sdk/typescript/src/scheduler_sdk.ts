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

// ---------------------------------------------------------------------------
// Scheduler domain types
// ---------------------------------------------------------------------------

export interface SchedulerJob {
  id: number;
  name: string;
  description?: string;
  type: string;
  schedule?: string;
  handler: string;
  payload?: unknown;
  priority: number;
  timeout: number;
  max_retries: number;
  status: string;
  last_run?: string;
  next_run?: string;
  run_count: number;
  fail_count: number;
  last_error?: string;
  labels?: Record<string, string>;
  created_at: string;
  updated_at: string;
  owner_type?: string;
  owner_id?: string;
  target_tenant_id?: string;
}

export interface ListJobsRequest {
  status?: string;
  handler?: string;
  limit?: number;
  cursor?: string;
  owner_type?: string;
  owner_id?: string;
}

export interface ListJobsResponse {
  jobs: SchedulerJob[];
  next_cursor: string;
  has_more: boolean;
  total_count: number;
  timestamp: string;
}

export interface CreateJobRequest {
  name: string;
  description?: string;
  type: string;
  schedule?: string;
  handler: string;
  payload?: unknown;
  priority?: number;
  timeout_seconds?: number;
  max_retries?: number;
  labels?: Record<string, string>;
  owner_type?: string;
  owner_id?: string;
  target_tenant_id?: string;
}

export interface CreateJobResponse {
  job: SchedulerJob;
  timestamp: string;
}

export interface DeleteJobResponse {
  job_id: number;
  deleted: boolean;
  timestamp: string;
}

export interface TriggerJobResponse {
  job_id: number;
  status: string;
  timestamp: string;
}

export interface SchedulerStatus {
  running: boolean;
  stats?: {
    job_count: number;
    running_jobs: number;
    success_count: number;
    failure_count: number;
    started_at: string;
  };
  timestamp: string;
}

// ---------------------------------------------------------------------------
// SchedulerClient interface
// ---------------------------------------------------------------------------

// SchedulerClient exposes scheduled job management operations.
// Maps to /api/v1/scheduler/* REST endpoints.
export interface SchedulerClient {
  // listJobs returns all scheduled jobs, optionally filtered.
  // GET /api/v1/scheduler/jobs
  listJobs(req?: ListJobsRequest): Promise<ListJobsResponse>;

  // createJob registers a new scheduled job.
  // POST /api/v1/scheduler/jobs
  createJob(req: CreateJobRequest): Promise<CreateJobResponse>;

  // deleteJob removes a scheduled job by numeric ID.
  // DELETE /api/v1/scheduler/jobs/:id
  deleteJob(id: number): Promise<DeleteJobResponse>;

  // triggerJob runs a job immediately, ignoring its schedule.
  // POST /api/v1/scheduler/jobs/:id/trigger
  triggerJob(id: number): Promise<TriggerJobResponse>;

  // pauseJob suspends execution of a scheduled job without deleting it.
  // POST /api/v1/scheduler/jobs/:id/pause
  pauseJob(id: number): Promise<void>;

  // resumeJob resumes a previously paused job.
  // POST /api/v1/scheduler/jobs/:id/resume
  resumeJob(id: number): Promise<void>;

  // updateJob replaces mutable fields on a scheduled job.
  // PUT /api/v1/scheduler/jobs/:id
  updateJob(id: number, req: Partial<CreateJobRequest>): Promise<SchedulerJob>;

  // getStatus returns the scheduler engine status.
  // GET /api/v1/scheduler/status
  getStatus(): Promise<SchedulerStatus>;
}
