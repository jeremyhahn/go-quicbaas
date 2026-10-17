/**
 * QuicBaaS JavaScript SDK - Scheduler Client
 *
 * Provides job scheduling operations including creation, management,
 * execution control, and status monitoring.
 *
 * @module @quicbaas/client/clients/scheduler
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  requiredString,
  required,
  requiredObject,
  nonNegativeInteger,
  positiveInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * API base path for scheduler endpoints.
 * @constant {string}
 */
const SCHEDULER_API_PATH = '/api/v1/scheduler';

/**
 * Job type values indicating how a job is scheduled.
 * @readonly
 * @enum {string}
 */
export const JobType = Object.freeze({
  /** Job runs once at a specified time */
  ONE_SHOT: 'one-shot',
  /** Job runs repeatedly on a schedule */
  RECURRING: 'recurring',
  /** Job runs once after a delay */
  DELAYED: 'delayed',
});

/**
 * Job status values.
 * @readonly
 * @enum {string}
 */
export const JobStatus = Object.freeze({
  /** Job is waiting to be executed */
  PENDING: 'pending',
  /** Job is currently executing */
  RUNNING: 'running',
  /** Job has completed successfully */
  COMPLETED: 'completed',
  /** Job execution failed */
  FAILED: 'failed',
  /** Job is paused and will not execute */
  PAUSED: 'paused',
  /** Job has been canceled */
  CANCELED: 'canceled',
});

/**
 * Owner type values indicating job ownership context.
 * @readonly
 * @enum {string}
 */
export const OwnerType = Object.freeze({
  /** System-level job */
  SYSTEM: 'system',
  /** Tenant-owned job */
  TENANT: 'tenant',
  /** Delegated job ownership */
  DELEGATED: 'delegated',
});

/**
 * @typedef {Object} CreateJobRequest
 * @property {string} name - Name for the job
 * @property {string} type - Job type (from JobType enum)
 * @property {string} [description] - Optional job description
 * @property {string} [schedule] - Cron expression for recurring jobs
 * @property {number} [delayMs] - Delay in milliseconds for delayed jobs
 * @property {number} [runAt] - Unix timestamp for one-shot jobs
 * @property {Object} payload - Job payload data
 * @property {string} [ownerType] - Owner type (from OwnerType enum)
 * @property {string} [ownerId] - Owner identifier
 * @property {Object} [metadata] - Custom metadata key-value pairs
 * @property {number} [maxRetries] - Maximum retry attempts on failure
 * @property {number} [retryDelayMs] - Delay between retries in milliseconds
 * @property {number} [timeoutMs] - Job execution timeout in milliseconds
 * @property {string[]} [tags] - Tags for categorization
 */

/**
 * @typedef {Object} CreateJobResponse
 * @property {string} jobId - Unique identifier for the created job
 * @property {string} name - Job name
 * @property {string} status - Initial job status
 * @property {number} createdAt - Creation timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} JobInfo
 * @property {string} jobId - Unique job identifier
 * @property {string} name - Job name
 * @property {string} type - Job type
 * @property {string} status - Current job status
 * @property {string} [description] - Job description
 * @property {string} [schedule] - Cron schedule expression
 * @property {Object} payload - Job payload data
 * @property {string} ownerType - Owner type
 * @property {string} [ownerId] - Owner identifier
 * @property {number} createdAt - Creation timestamp (Unix nanoseconds)
 * @property {number} updatedAt - Last update timestamp (Unix nanoseconds)
 * @property {number} [lastRunAt] - Last execution timestamp (Unix nanoseconds)
 * @property {number} [nextRunAt] - Next scheduled execution timestamp (Unix nanoseconds)
 * @property {number} runCount - Total execution count
 * @property {number} failureCount - Total failure count
 */

/**
 * @typedef {Object} UpdateJobRequest
 * @property {string} [name] - New job name
 * @property {string} [description] - New description
 * @property {string} [schedule] - New cron schedule
 * @property {Object} [payload] - New payload data
 * @property {Object} [metadata] - Custom metadata to merge
 * @property {number} [maxRetries] - Maximum retry attempts
 * @property {number} [retryDelayMs] - Delay between retries
 * @property {number} [timeoutMs] - Execution timeout
 * @property {string[]} [tags] - New tags
 */

/**
 * @typedef {Object} ListJobsOptions
 * @property {number} [limit] - Maximum number of jobs to return
 * @property {number} [offset] - Number of jobs to skip
 * @property {string} [status] - Filter by job status
 * @property {string} [type] - Filter by job type
 * @property {string} [ownerType] - Filter by owner type
 * @property {string} [ownerId] - Filter by owner ID
 * @property {string[]} [tags] - Filter by tags
 */

/**
 * @typedef {Object} ListJobsResponse
 * @property {JobInfo[]} jobs - List of jobs
 * @property {number} totalCount - Total number of jobs available
 */

/**
 * @typedef {Object} TriggerJobResponse
 * @property {string} jobId - Job identifier
 * @property {string} executionId - Unique identifier for this execution
 * @property {string} status - Execution status
 * @property {number} triggeredAt - Trigger timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} JobActionResponse
 * @property {boolean} success - Whether the action was successful
 * @property {string} message - Additional information
 * @property {string} [previousStatus] - Previous job status
 * @property {string} [newStatus] - New job status
 */

/**
 * @typedef {Object} JobExecution
 * @property {string} executionId - Unique execution identifier
 * @property {string} jobId - Job identifier
 * @property {string} status - Execution status
 * @property {number} startedAt - Start timestamp (Unix nanoseconds)
 * @property {number} [completedAt] - Completion timestamp (Unix nanoseconds)
 * @property {number} [durationMs] - Execution duration in milliseconds
 * @property {string} [error] - Error message if failed
 * @property {Object} [result] - Execution result data
 * @property {number} attemptNumber - Retry attempt number
 */

/**
 * @typedef {Object} JobHistoryOptions
 * @property {number} [limit] - Maximum number of executions to return
 * @property {number} [offset] - Number of executions to skip
 * @property {string} [status] - Filter by execution status
 * @property {number} [startTime] - Filter executions after this timestamp
 * @property {number} [endTime] - Filter executions before this timestamp
 */

/**
 * @typedef {Object} JobHistoryResponse
 * @property {string} jobId - Job identifier
 * @property {JobExecution[]} executions - List of executions
 * @property {number} totalCount - Total number of executions
 */

/**
 * @typedef {Object} SchedulerStatus
 * @property {boolean} running - Whether the scheduler is running
 * @property {number} activeJobs - Number of active jobs
 * @property {number} pendingJobs - Number of pending jobs
 * @property {number} runningJobs - Number of currently running jobs
 * @property {number} completedJobs - Number of completed jobs
 * @property {number} failedJobs - Number of failed jobs
 * @property {number} pausedJobs - Number of paused jobs
 * @property {number} uptime - Scheduler uptime in milliseconds
 * @property {string} version - Scheduler version
 */

/**
 * Scheduler client for QuicBaaS.
 *
 * Provides methods for managing scheduled jobs including creation,
 * execution control, pausing, resuming, and history tracking.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import SchedulerClient, { JobType, JobStatus } from '@quicbaas/client/clients/scheduler';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const scheduler = new SchedulerClient(transport);
 *
 * // Create a recurring job
 * const { jobId } = await scheduler.createJob({
 *   name: 'daily-backup',
 *   type: JobType.RECURRING,
 *   schedule: '0 0 * * *',
 *   payload: { target: 'database' }
 * });
 *
 * // Trigger immediate execution
 * await scheduler.triggerJob(jobId);
 *
 * // Get job history
 * const { executions } = await scheduler.getJobHistory(jobId, { limit: 10 });
 */
export class SchedulerClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new SchedulerClient instance.
   *
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided or invalid
   */
  constructor(transport) {
    if (!transport) {
      throw new ValidationError('transport is required', 'transport');
    }
    if (typeof transport.request !== 'function') {
      throw new ValidationError('transport must have a request method', 'transport');
    }

    this.#transport = transport;
  }

  /**
   * Create a new scheduled job.
   *
   * @param {CreateJobRequest} request - Job creation request
   * @returns {Promise<CreateJobResponse>} Response with jobId and initial status
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * // Create a one-shot job
   * const { jobId } = await scheduler.createJob({
   *   name: 'send-report',
   *   type: JobType.ONE_SHOT,
   *   runAt: Date.now() + 3600000, // 1 hour from now
   *   payload: { reportId: 'monthly-sales' }
   * });
   *
   * // Create a recurring job with cron schedule
   * const { jobId } = await scheduler.createJob({
   *   name: 'cleanup-temp-files',
   *   type: JobType.RECURRING,
   *   schedule: '0 0 * * *', // Daily at midnight
   *   payload: { directory: '/tmp' },
   *   maxRetries: 3
   * });
   */
  async createJob(request) {
    this.#validateCreateJobRequest(request);

    const requestBody = {
      name: request.name,
      type: request.type,
      payload: request.payload,
    };

    if (request.description !== undefined) {
      requestBody.description = request.description;
    }
    if (request.schedule !== undefined) {
      requestBody.schedule = request.schedule;
    }
    if (request.delayMs !== undefined) {
      requestBody.delay_ms = request.delayMs;
    }
    if (request.runAt !== undefined) {
      requestBody.run_at = request.runAt;
    }
    if (request.ownerType !== undefined) {
      requestBody.owner_type = request.ownerType;
    }
    if (request.ownerId !== undefined) {
      requestBody.owner_id = request.ownerId;
    }
    if (request.metadata !== undefined) {
      requestBody.metadata = request.metadata;
    }
    if (request.maxRetries !== undefined) {
      requestBody.max_retries = request.maxRetries;
    }
    if (request.retryDelayMs !== undefined) {
      requestBody.retry_delay_ms = request.retryDelayMs;
    }
    if (request.timeoutMs !== undefined) {
      requestBody.timeout_ms = request.timeoutMs;
    }
    if (request.tags !== undefined) {
      requestBody.tags = request.tags;
    }

    const response = await this.#transport.post(
      `${SCHEDULER_API_PATH}/jobs`,
      requestBody,
    );

    return this.#transformCreateJobResponse(response);
  }

  /**
   * Get information about a job.
   *
   * @param {string} jobId - Unique job identifier
   * @returns {Promise<JobInfo>} Job information
   * @throws {ValidationError} If jobId is empty
   * @throws {NotFoundError} If job does not exist
   *
   * @example
   * const job = await scheduler.getJob('job-12345');
   * console.log(`Job ${job.name} is ${job.status}`);
   */
  async getJob(jobId) {
    requiredString(jobId, 'jobId');

    const response = await this.#transport.get(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}`,
    );

    return this.#transformJobInfo(response);
  }

  /**
   * Update a job's configuration.
   *
   * @param {string} jobId - Unique job identifier
   * @param {UpdateJobRequest} updates - Fields to update
   * @returns {Promise<JobInfo>} Updated job information
   * @throws {ValidationError} If jobId is empty or updates are invalid
   * @throws {NotFoundError} If job does not exist
   *
   * @example
   * const updated = await scheduler.updateJob('job-12345', {
   *   description: 'Updated description',
   *   schedule: '0 0 * * 1', // Changed to weekly
   *   maxRetries: 5
   * });
   */
  async updateJob(jobId, updates) {
    requiredString(jobId, 'jobId');
    requiredObject(updates, 'updates');
    this.#validateUpdateJobRequest(updates);

    const requestBody = {};

    if (updates.name !== undefined) {
      requestBody.name = updates.name;
    }
    if (updates.description !== undefined) {
      requestBody.description = updates.description;
    }
    if (updates.schedule !== undefined) {
      requestBody.schedule = updates.schedule;
    }
    if (updates.payload !== undefined) {
      requestBody.payload = updates.payload;
    }
    if (updates.metadata !== undefined) {
      requestBody.metadata = updates.metadata;
    }
    if (updates.maxRetries !== undefined) {
      requestBody.max_retries = updates.maxRetries;
    }
    if (updates.retryDelayMs !== undefined) {
      requestBody.retry_delay_ms = updates.retryDelayMs;
    }
    if (updates.timeoutMs !== undefined) {
      requestBody.timeout_ms = updates.timeoutMs;
    }
    if (updates.tags !== undefined) {
      requestBody.tags = updates.tags;
    }

    const response = await this.#transport.patch(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}`,
      requestBody,
    );

    return this.#transformJobInfo(response);
  }

  /**
   * Delete a job permanently.
   *
   * @param {string} jobId - Unique job identifier
   * @returns {Promise<JobActionResponse>} Response indicating success
   * @throws {ValidationError} If jobId is empty
   * @throws {NotFoundError} If job does not exist
   *
   * @example
   * const { success, message } = await scheduler.deleteJob('job-12345');
   * if (success) {
   *   console.log('Job deleted successfully');
   * }
   */
  async deleteJob(jobId) {
    requiredString(jobId, 'jobId');

    const response = await this.#transport.delete(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}`,
    );

    return this.#transformJobActionResponse(response);
  }

  /**
   * List all jobs with optional filtering and pagination.
   *
   * @param {ListJobsOptions} [options={}] - List options
   * @returns {Promise<ListJobsResponse>} Response with jobs and total count
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * // List all pending jobs
   * const { jobs, totalCount } = await scheduler.listJobs({
   *   status: JobStatus.PENDING,
   *   limit: 10
   * });
   *
   * // List recurring jobs by owner
   * const { jobs } = await scheduler.listJobs({
   *   type: JobType.RECURRING,
   *   ownerId: 'tenant-123'
   * });
   */
  async listJobs(options = {}) {
    this.#validateListJobsOptions(options);

    const query = {};

    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.offset !== undefined) {
      query.offset = options.offset;
    }
    if (options.status !== undefined) {
      query.status = options.status;
    }
    if (options.type !== undefined) {
      query.type = options.type;
    }
    if (options.ownerType !== undefined) {
      query.owner_type = options.ownerType;
    }
    if (options.ownerId !== undefined) {
      query.owner_id = options.ownerId;
    }
    if (options.tags !== undefined) {
      query.tags = options.tags.join(',');
    }

    const response = await this.#transport.get(
      `${SCHEDULER_API_PATH}/jobs`,
      { query },
    );

    return this.#transformListJobsResponse(response);
  }

  /**
   * Trigger immediate execution of a job.
   *
   * @param {string} jobId - Unique job identifier
   * @returns {Promise<TriggerJobResponse>} Response with execution details
   * @throws {ValidationError} If jobId is empty
   * @throws {NotFoundError} If job does not exist
   *
   * @example
   * const { executionId, status } = await scheduler.triggerJob('job-12345');
   * console.log(`Triggered execution ${executionId}`);
   */
  async triggerJob(jobId) {
    requiredString(jobId, 'jobId');

    const response = await this.#transport.post(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}/trigger`,
      {},
    );

    return this.#transformTriggerJobResponse(response, jobId);
  }

  /**
   * Pause a job to prevent scheduled executions.
   *
   * @param {string} jobId - Unique job identifier
   * @returns {Promise<JobActionResponse>} Response indicating success
   * @throws {ValidationError} If jobId is empty
   * @throws {NotFoundError} If job does not exist
   *
   * @example
   * const { success, newStatus } = await scheduler.pauseJob('job-12345');
   * console.log(`Job is now ${newStatus}`);
   */
  async pauseJob(jobId) {
    requiredString(jobId, 'jobId');

    const response = await this.#transport.post(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}/pause`,
      {},
    );

    return this.#transformJobActionResponse(response, JobStatus.PAUSED);
  }

  /**
   * Resume a paused job.
   *
   * @param {string} jobId - Unique job identifier
   * @returns {Promise<JobActionResponse>} Response indicating success
   * @throws {ValidationError} If jobId is empty
   * @throws {NotFoundError} If job does not exist
   *
   * @example
   * const { success, newStatus } = await scheduler.resumeJob('job-12345');
   * console.log(`Job is now ${newStatus}`);
   */
  async resumeJob(jobId) {
    requiredString(jobId, 'jobId');

    const response = await this.#transport.post(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}/resume`,
      {},
    );

    return this.#transformJobActionResponse(response, JobStatus.PENDING);
  }

  /**
   * Cancel a job with an optional reason.
   *
   * @param {string} jobId - Unique job identifier
   * @param {string} [reason] - Optional cancellation reason
   * @returns {Promise<JobActionResponse>} Response indicating success
   * @throws {ValidationError} If jobId is empty
   * @throws {NotFoundError} If job does not exist
   *
   * @example
   * const { success } = await scheduler.cancelJob('job-12345', 'No longer needed');
   * if (success) {
   *   console.log('Job canceled successfully');
   * }
   */
  async cancelJob(jobId, reason) {
    requiredString(jobId, 'jobId');

    if (reason !== undefined && typeof reason !== 'string') {
      throw new ValidationError('reason must be a string', 'reason');
    }

    const requestBody = {};
    if (reason !== undefined) {
      requestBody.reason = reason;
    }

    const response = await this.#transport.post(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}/cancel`,
      requestBody,
    );

    return this.#transformJobActionResponse(response, JobStatus.CANCELED);
  }

  /**
   * Get execution history for a job.
   *
   * @param {string} jobId - Unique job identifier
   * @param {JobHistoryOptions} [options={}] - History options
   * @returns {Promise<JobHistoryResponse>} Response with execution history
   * @throws {ValidationError} If jobId is empty or options are invalid
   * @throws {NotFoundError} If job does not exist
   *
   * @example
   * // Get recent executions
   * const { executions } = await scheduler.getJobHistory('job-12345', {
   *   limit: 10
   * });
   *
   * // Get failed executions in a time range
   * const { executions } = await scheduler.getJobHistory('job-12345', {
   *   status: JobStatus.FAILED,
   *   startTime: Date.now() - 86400000,
   *   endTime: Date.now()
   * });
   */
  async getJobHistory(jobId, options = {}) {
    requiredString(jobId, 'jobId');
    this.#validateJobHistoryOptions(options);

    const query = {};

    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.offset !== undefined) {
      query.offset = options.offset;
    }
    if (options.status !== undefined) {
      query.status = options.status;
    }
    if (options.startTime !== undefined) {
      query.start_time = options.startTime;
    }
    if (options.endTime !== undefined) {
      query.end_time = options.endTime;
    }

    const response = await this.#transport.get(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}/history`,
      { query },
    );

    return this.#transformJobHistoryResponse(response, jobId);
  }

  /**
   * Get the current status of the scheduler.
   *
   * @returns {Promise<SchedulerStatus>} Scheduler status information
   *
   * @example
   * const status = await scheduler.getStatus();
   * console.log(`Scheduler running: ${status.running}`);
   * console.log(`Active jobs: ${status.activeJobs}`);
   * console.log(`Running jobs: ${status.runningJobs}`);
   */
  async getStatus() {
    const response = await this.#transport.get(
      `${SCHEDULER_API_PATH}/status`,
    );

    return this.#transformSchedulerStatus(response);
  }

  // Validation methods

  /**
   * Validate create job request.
   *
   * @param {CreateJobRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateCreateJobRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.name, 'request.name');
    requiredString(request.type, 'request.type');
    oneOf(request.type, Object.values(JobType), 'request.type');
    required(request.payload, 'request.payload');

    if (request.type === JobType.RECURRING && request.schedule === undefined) {
      throw new ValidationError('request.schedule is required for recurring jobs', 'request.schedule');
    }

    if (request.type === JobType.DELAYED && request.delayMs === undefined) {
      throw new ValidationError('request.delayMs is required for delayed jobs', 'request.delayMs');
    }

    if (request.description !== undefined && typeof request.description !== 'string') {
      throw new ValidationError('request.description must be a string', 'request.description');
    }
    if (request.schedule !== undefined && typeof request.schedule !== 'string') {
      throw new ValidationError('request.schedule must be a string', 'request.schedule');
    }
    if (request.delayMs !== undefined) {
      nonNegativeInteger(request.delayMs, 'request.delayMs');
    }
    if (request.runAt !== undefined) {
      nonNegativeInteger(request.runAt, 'request.runAt');
    }
    if (request.ownerType !== undefined) {
      oneOf(request.ownerType, Object.values(OwnerType), 'request.ownerType');
    }
    if (request.ownerId !== undefined && typeof request.ownerId !== 'string') {
      throw new ValidationError('request.ownerId must be a string', 'request.ownerId');
    }
    if (request.metadata !== undefined && (typeof request.metadata !== 'object' || Array.isArray(request.metadata))) {
      throw new ValidationError('request.metadata must be an object', 'request.metadata');
    }
    if (request.maxRetries !== undefined) {
      nonNegativeInteger(request.maxRetries, 'request.maxRetries');
    }
    if (request.retryDelayMs !== undefined) {
      nonNegativeInteger(request.retryDelayMs, 'request.retryDelayMs');
    }
    if (request.timeoutMs !== undefined) {
      positiveInteger(request.timeoutMs, 'request.timeoutMs');
    }
    if (request.tags !== undefined) {
      this.#validateTagsArray(request.tags, 'request.tags');
    }
  }

  /**
   * Validate update job request.
   *
   * @param {UpdateJobRequest} updates - Updates to validate
   * @throws {ValidationError} If updates are invalid
   * @private
   */
  #validateUpdateJobRequest(updates) {
    if (updates.name !== undefined && (typeof updates.name !== 'string' || updates.name.trim() === '')) {
      throw new ValidationError('updates.name must be a non-empty string', 'updates.name');
    }
    if (updates.description !== undefined && typeof updates.description !== 'string') {
      throw new ValidationError('updates.description must be a string', 'updates.description');
    }
    if (updates.schedule !== undefined && typeof updates.schedule !== 'string') {
      throw new ValidationError('updates.schedule must be a string', 'updates.schedule');
    }
    if (updates.payload !== undefined && updates.payload === null) {
      throw new ValidationError('updates.payload cannot be null', 'updates.payload');
    }
    if (updates.metadata !== undefined && (typeof updates.metadata !== 'object' || Array.isArray(updates.metadata))) {
      throw new ValidationError('updates.metadata must be an object', 'updates.metadata');
    }
    if (updates.maxRetries !== undefined) {
      nonNegativeInteger(updates.maxRetries, 'updates.maxRetries');
    }
    if (updates.retryDelayMs !== undefined) {
      nonNegativeInteger(updates.retryDelayMs, 'updates.retryDelayMs');
    }
    if (updates.timeoutMs !== undefined) {
      positiveInteger(updates.timeoutMs, 'updates.timeoutMs');
    }
    if (updates.tags !== undefined) {
      this.#validateTagsArray(updates.tags, 'updates.tags');
    }
  }

  /**
   * Validate list jobs options.
   *
   * @param {ListJobsOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListJobsOptions(options) {
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }
    if (options.offset !== undefined) {
      nonNegativeInteger(options.offset, 'options.offset');
    }
    if (options.status !== undefined) {
      oneOf(options.status, Object.values(JobStatus), 'options.status');
    }
    if (options.type !== undefined) {
      oneOf(options.type, Object.values(JobType), 'options.type');
    }
    if (options.ownerType !== undefined) {
      oneOf(options.ownerType, Object.values(OwnerType), 'options.ownerType');
    }
    if (options.ownerId !== undefined && typeof options.ownerId !== 'string') {
      throw new ValidationError('options.ownerId must be a string', 'options.ownerId');
    }
    if (options.tags !== undefined) {
      this.#validateTagsArray(options.tags, 'options.tags');
    }
  }

  /**
   * Validate job history options.
   *
   * @param {JobHistoryOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateJobHistoryOptions(options) {
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }
    if (options.offset !== undefined) {
      nonNegativeInteger(options.offset, 'options.offset');
    }
    if (options.status !== undefined) {
      oneOf(options.status, Object.values(JobStatus), 'options.status');
    }
    if (options.startTime !== undefined) {
      nonNegativeInteger(options.startTime, 'options.startTime');
    }
    if (options.endTime !== undefined) {
      nonNegativeInteger(options.endTime, 'options.endTime');
    }
    if (options.startTime !== undefined && options.endTime !== undefined && options.startTime > options.endTime) {
      throw new ValidationError('options.startTime cannot be greater than options.endTime', 'options.startTime');
    }
  }

  /**
   * Validate tags array.
   *
   * @param {string[]} tags - Tags array to validate
   * @param {string} fieldName - Field name for error messages
   * @throws {ValidationError} If tags are invalid
   * @private
   */
  #validateTagsArray(tags, fieldName) {
    if (!Array.isArray(tags)) {
      throw new ValidationError(`${fieldName} must be an array`, fieldName);
    }

    for (let i = 0; i < tags.length; i++) {
      if (typeof tags[i] !== 'string' || tags[i].trim() === '') {
        throw new ValidationError(
          `${fieldName}[${i}] must be a non-empty string`,
          `${fieldName}[${i}]`,
        );
      }
    }
  }

  // Response transformation methods

  /**
   * Transform create job response.
   *
   * @param {Object} response - Raw server response
   * @returns {CreateJobResponse}
   * @private
   */
  #transformCreateJobResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      jobId: response.job_id || response.jobId || '',
      name: response.name || '',
      status: response.status || JobStatus.PENDING,
      createdAt: response.created_at || response.createdAt || 0,
    };
  }

  /**
   * Transform job info from server response.
   *
   * @param {Object} job - Raw job object from server
   * @returns {JobInfo}
   * @private
   */
  #transformJobInfo(job) {
    if (!job) {
      throw new ValidationError('Invalid job data from server', 'job');
    }

    return {
      jobId: job.job_id || job.jobId || '',
      name: job.name || '',
      type: job.type || JobType.ONE_SHOT,
      status: job.status || JobStatus.PENDING,
      description: job.description,
      schedule: job.schedule,
      payload: job.payload || {},
      ownerType: job.owner_type || job.ownerType || OwnerType.SYSTEM,
      ownerId: job.owner_id || job.ownerId,
      createdAt: job.created_at || job.createdAt || 0,
      updatedAt: job.updated_at || job.updatedAt || 0,
      lastRunAt: job.last_run_at || job.lastRunAt,
      nextRunAt: job.next_run_at || job.nextRunAt,
      runCount: job.run_count || job.runCount || 0,
      failureCount: job.failure_count || job.failureCount || 0,
    };
  }

  /**
   * Transform list jobs response.
   *
   * @param {Object} response - Raw server response
   * @returns {ListJobsResponse}
   * @private
   */
  #transformListJobsResponse(response) {
    const jobs = (response?.jobs || []).map(job => this.#transformJobInfo(job));

    return {
      jobs,
      totalCount: response?.total_count || response?.totalCount || jobs.length,
    };
  }

  /**
   * Transform trigger job response.
   *
   * @param {Object} response - Raw server response
   * @param {string} jobId - Job identifier
   * @returns {TriggerJobResponse}
   * @private
   */
  #transformTriggerJobResponse(response, jobId) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      jobId: response.job_id || response.jobId || jobId,
      executionId: response.execution_id || response.executionId || '',
      status: response.status || JobStatus.RUNNING,
      triggeredAt: response.triggered_at || response.triggeredAt || 0,
    };
  }

  /**
   * Transform job action response.
   *
   * @param {Object} response - Raw server response
   * @param {string} [expectedNewStatus] - Expected new status
   * @returns {JobActionResponse}
   * @private
   */
  #transformJobActionResponse(response, expectedNewStatus) {
    return {
      success: response?.success ?? true,
      message: response?.message || '',
      previousStatus: response?.previous_status || response?.previousStatus,
      newStatus: response?.new_status || response?.newStatus || expectedNewStatus,
    };
  }

  /**
   * Transform job execution from server response.
   *
   * @param {Object} execution - Raw execution object from server
   * @returns {JobExecution}
   * @private
   */
  #transformJobExecution(execution) {
    return {
      executionId: execution.execution_id || execution.executionId || '',
      jobId: execution.job_id || execution.jobId || '',
      status: execution.status || JobStatus.PENDING,
      startedAt: execution.started_at || execution.startedAt || 0,
      completedAt: execution.completed_at || execution.completedAt,
      durationMs: execution.duration_ms || execution.durationMs,
      error: execution.error,
      result: execution.result,
      attemptNumber: execution.attempt_number || execution.attemptNumber || 1,
    };
  }

  /**
   * Transform job history response.
   *
   * @param {Object} response - Raw server response
   * @param {string} jobId - Job identifier
   * @returns {JobHistoryResponse}
   * @private
   */
  #transformJobHistoryResponse(response, jobId) {
    const executions = (response?.executions || []).map(exec => this.#transformJobExecution(exec));

    return {
      jobId: response?.job_id || response?.jobId || jobId,
      executions,
      totalCount: response?.total_count || response?.totalCount || executions.length,
    };
  }

  /**
   * Transform scheduler status response.
   *
   * @param {Object} response - Raw server response
   * @returns {SchedulerStatus}
   * @private
   */
  #transformSchedulerStatus(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      running: response.running ?? false,
      activeJobs: response.active_jobs || response.activeJobs || 0,
      pendingJobs: response.pending_jobs || response.pendingJobs || 0,
      runningJobs: response.running_jobs || response.runningJobs || 0,
      completedJobs: response.completed_jobs || response.completedJobs || 0,
      failedJobs: response.failed_jobs || response.failedJobs || 0,
      pausedJobs: response.paused_jobs || response.pausedJobs || 0,
      uptime: response.uptime || 0,
      version: response.version || '',
    };
  }
}

export default SchedulerClient;
