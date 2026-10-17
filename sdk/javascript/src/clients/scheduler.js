/**
 * QuicBaaS JavaScript SDK - Scheduler Client
 *
 * Provides job scheduling operations including creation, management,
 * execution control, and status monitoring.
 *
 * Request bodies, query parameters and responses follow the REST contract
 * served by pkg/api/rest/handlers_scheduler.go.
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
 * Nanoseconds per millisecond, for the server's Go time.Duration fields.
 * @constant {number}
 */
const NANOSECONDS_PER_MILLISECOND = 1e6;

/**
 * Job type enumeration.
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
 * Job status enumeration.
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
 * Owner type enumeration.
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
 * Create-job options and the JSON field each is sent as
 * (CreateSchedulerJobRESTRequest).
 * @constant {Object<string, string>}
 */
const CREATE_JOB_FIELDS = Object.freeze({
  name: 'name',
  type: 'type',
  handler: 'handler',
  description: 'description',
  schedule: 'schedule',
  executeAt: 'execute_at',
  delaySeconds: 'delay_seconds',
  ownerType: 'owner_type',
  ownerId: 'owner_id',
  targetTenantId: 'target_tenant_id',
  payload: 'payload',
  priority: 'priority',
  timeoutSeconds: 'timeout_seconds',
  maxRetries: 'max_retries',
  nodeAffinity: 'node_affinity',
  labels: 'labels',
});

/**
 * Update-job options and the JSON field each is sent as
 * (UpdateSchedulerJobRESTRequest).
 * @constant {Object<string, string>}
 */
const UPDATE_JOB_FIELDS = Object.freeze({
  name: 'name',
  description: 'description',
  schedule: 'schedule',
  executeAt: 'execute_at',
  handler: 'handler',
  payload: 'payload',
  priority: 'priority',
  timeoutSeconds: 'timeout_seconds',
  maxRetries: 'max_retries',
  labels: 'labels',
});

/**
 * @typedef {Object} CreateJobRequest
 * @property {string} name - Name for the job
 * @property {string} type - Job type (from JobType enum)
 * @property {string} handler - Name of the server-registered handler that runs the job
 * @property {Object} payload - Job payload data
 * @property {string} [description] - Optional job description
 * @property {string} [schedule] - Cron expression; required for recurring jobs
 * @property {Date|string} [executeAt] - When a one-shot job runs (Date or RFC 3339 string)
 * @property {number} [delaySeconds] - Delay in seconds; required for delayed jobs
 * @property {string} [ownerType] - Owner type (from OwnerType enum)
 * @property {string} [ownerId] - Owner identifier
 * @property {string} [targetTenantId] - Tenant a system job acts on
 * @property {number} [priority] - Execution priority (integer)
 * @property {number} [timeoutSeconds] - Execution timeout in seconds
 * @property {number} [maxRetries] - Maximum retry attempts on failure
 * @property {string} [nodeAffinity] - Node the job is pinned to (empty = cluster-wide)
 * @property {Object<string, string>} [labels] - Labels for categorization
 */

/**
 * @typedef {Object} UpdateJobRequest
 * @property {string} [name] - New job name
 * @property {string} [description] - New description
 * @property {string} [schedule] - New cron schedule
 * @property {Date|string} [executeAt] - New execution time (Date or RFC 3339 string)
 * @property {string} [handler] - New handler name
 * @property {Object} [payload] - New payload data
 * @property {number} [priority] - New priority (integer)
 * @property {number} [timeoutSeconds] - New execution timeout in seconds
 * @property {number} [maxRetries] - New maximum retry attempts
 * @property {Object<string, string>} [labels] - New labels
 */

/**
 * @typedef {Object} JobInfo
 * @property {string} jobId - Unique job identifier
 * @property {string} name - Job name
 * @property {string} [description] - Job description
 * @property {string} type - Job type
 * @property {string} status - Current job status
 * @property {string} handler - Handler name
 * @property {string} [schedule] - Cron schedule expression
 * @property {string} [executeAt] - Execution time (RFC 3339)
 * @property {number} [delayMs] - Delay in milliseconds
 * @property {Object} payload - Job payload data
 * @property {number} priority - Execution priority
 * @property {number} timeoutMs - Execution timeout in milliseconds
 * @property {number} maxRetries - Maximum retry attempts
 * @property {string} ownerType - Owner type
 * @property {string} [ownerId] - Owner identifier
 * @property {string} [targetTenantId] - Tenant a system job acts on
 * @property {string} [createdBy] - Identity that created the job
 * @property {Object<string, string>} labels - Job labels
 * @property {string} createdAt - Creation time (RFC 3339)
 * @property {string} updatedAt - Last update time (RFC 3339)
 * @property {string} [lastRunAt] - Last execution time (RFC 3339)
 * @property {string} [nextRunAt] - Next scheduled execution time (RFC 3339)
 * @property {number} runCount - Total execution count
 * @property {number} failureCount - Total failure count
 * @property {string} [lastError] - Error of the last failed run
 */

/**
 * @typedef {Object} ListJobsOptions
 * @property {number} [limit] - Maximum number of jobs to return (1-1000)
 * @property {string} [cursor] - Cursor from a previous page's nextCursor
 * @property {string} [status] - Filter by job status
 * @property {string} [handler] - Filter by handler name
 * @property {string} [ownerType] - Filter by owner type
 * @property {string} [ownerId] - Filter by owner ID
 * @property {string} [targetTenantId] - Filter by target tenant ID
 */

/**
 * @typedef {Object} ListJobsResponse
 * @property {JobInfo[]} jobs - List of jobs
 * @property {number} totalCount - Total number of jobs the server reports
 * @property {string} [nextCursor] - Cursor for the next page
 * @property {boolean} hasMore - Whether another page exists
 */

/**
 * @typedef {Object} DeleteJobResponse
 * @property {string} jobId - Job identifier
 * @property {boolean} deleted - Whether the job was deleted
 */

/**
 * @typedef {Object} TriggerJobResponse
 * @property {string} jobId - Job identifier
 * @property {string} status - Job status after the trigger
 * @property {string} triggeredAt - Trigger time (RFC 3339)
 */

/**
 * @typedef {Object} JobActionResponse
 * @property {string} jobId - Job identifier
 * @property {string} previousStatus - Job status before the action
 * @property {string} newStatus - Job status after the action
 */

/**
 * @typedef {Object} JobExecution
 * @property {string} executionId - Unique execution identifier
 * @property {string} jobId - Job identifier
 * @property {string} status - Execution status
 * @property {string} startedAt - Start time (RFC 3339)
 * @property {string} [endedAt] - End time (RFC 3339)
 * @property {number} [durationMs] - Execution duration in milliseconds
 * @property {string} [error] - Error message if failed
 * @property {string} [output] - Execution output
 */

/**
 * @typedef {Object} JobHistoryOptions
 * @property {number} [limit] - Maximum number of executions to return (1-1000)
 * @property {string} [cursor] - Cursor from a previous page's nextCursor
 */

/**
 * @typedef {Object} JobHistoryResponse
 * @property {string} jobId - Job identifier
 * @property {JobExecution[]} executions - List of executions
 * @property {string} [nextCursor] - Cursor for the next page
 * @property {boolean} hasMore - Whether another page exists
 */

/**
 * @typedef {Object} SchedulerStats
 * @property {number} jobCount - Number of jobs
 * @property {number} runningJobs - Number of currently running jobs
 * @property {number} successCount - Number of successful executions
 * @property {number} failureCount - Number of failed executions
 * @property {string} [lastTickTime] - Time of the last scheduler tick (RFC 3339)
 * @property {string} [startedAt] - Scheduler start time (RFC 3339)
 */

/**
 * @typedef {Object} SchedulerStatus
 * @property {boolean} running - Whether the scheduler is running
 * @property {SchedulerStats} stats - Scheduler counters
 */

/**
 * Scheduler client for QuicBaaS.
 *
 * Provides methods for managing scheduled jobs including creation,
 * execution control, pausing, resuming, and history tracking.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import SchedulerClient, { JobType } from '@quicbaas/client/clients/scheduler';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const scheduler = new SchedulerClient(transport);
 *
 * // Create a recurring job
 * const { jobId } = await scheduler.createJob({
 *   name: 'daily-backup',
 *   type: JobType.RECURRING,
 *   handler: 'backup',
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
   * The transport layer for making HTTP requests.
   * @type {Object}
   * @private
   */
  #transport;

  /**
   * Creates a new SchedulerClient instance.
   *
   * @param {Object} transport - Transport instance for HTTP communication
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
   * Creates a new scheduled job.
   *
   * @param {CreateJobRequest} request - Job creation parameters
   * @returns {Promise<JobInfo>} The created job
   * @throws {ValidationError} If request parameters are invalid
   * @throws {QuicBaaSError} If the server returns an error
   *
   * @example
   * const job = await scheduler.createJob({
   *   name: 'send-reminder',
   *   type: JobType.DELAYED,
   *   handler: 'email',
   *   delaySeconds: 3600,
   *   payload: { userId: 'user123', message: 'Reminder!' }
   * });
   */
  async createJob(request) {
    this.#validateCreateJobRequest(request);

    const response = await this.#transport.post(
      `${SCHEDULER_API_PATH}/jobs`,
      this.#toRequestBody(request, CREATE_JOB_FIELDS),
    );

    return this.#transformJobInfo(response?.job);
  }

  /**
   * Retrieves information about a specific job.
   *
   * @param {string} jobId - The job identifier
   * @returns {Promise<JobInfo>} Job information
   * @throws {ValidationError} If jobId is invalid
   * @throws {NotFoundError} If the job does not exist
   * @throws {QuicBaaSError} If the server returns an error
   */
  async getJob(jobId) {
    requiredString(jobId, 'jobId');

    const response = await this.#transport.get(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}`,
    );

    return this.#transformJobInfo(response?.job);
  }

  /**
   * Updates an existing job.
   *
   * @param {string} jobId - The job identifier
   * @param {UpdateJobRequest} updates - Fields to update
   * @returns {Promise<JobInfo>} Updated job information
   * @throws {ValidationError} If parameters are invalid
   * @throws {NotFoundError} If the job does not exist
   * @throws {QuicBaaSError} If the server returns an error
   *
   * @example
   * await scheduler.updateJob(jobId, {
   *   schedule: '0 2 * * *',
   *   maxRetries: 5
   * });
   */
  async updateJob(jobId, updates) {
    requiredString(jobId, 'jobId');
    requiredObject(updates, 'updates');
    this.#validateUpdateJobRequest(updates);

    const response = await this.#transport.put(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}`,
      this.#toRequestBody(updates, UPDATE_JOB_FIELDS),
    );

    return this.#transformJobInfo(response?.job);
  }

  /**
   * Deletes a job.
   *
   * @param {string} jobId - The job identifier
   * @returns {Promise<DeleteJobResponse>} Deletion result
   * @throws {ValidationError} If jobId is invalid
   * @throws {NotFoundError} If the job does not exist
   * @throws {QuicBaaSError} If the server returns an error
   */
  async deleteJob(jobId) {
    requiredString(jobId, 'jobId');

    const response = await this.#transport.delete(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}`,
    );

    return {
      jobId: this.#idToString(response?.job_id, jobId),
      deleted: response?.deleted === true,
    };
  }

  /**
   * Lists jobs with optional filtering.
   *
   * @param {ListJobsOptions} [options={}] - Filter and pagination options
   * @returns {Promise<ListJobsResponse>} List of jobs
   * @throws {ValidationError} If options are invalid
   * @throws {QuicBaaSError} If the server returns an error
   *
   * @example
   * const { jobs, nextCursor } = await scheduler.listJobs({
   *   status: JobStatus.PENDING,
   *   limit: 50
   * });
   */
  async listJobs(options = {}) {
    this.#validateListJobsOptions(options);

    const query = {};
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor !== undefined) {
      query.cursor = options.cursor;
    }
    if (options.status !== undefined) {
      query.status = options.status;
    }
    if (options.handler !== undefined) {
      query.handler = options.handler;
    }
    if (options.ownerType !== undefined) {
      query.owner_type = options.ownerType;
    }
    if (options.ownerId !== undefined) {
      query.owner_id = options.ownerId;
    }
    if (options.targetTenantId !== undefined) {
      query.target_tenant_id = options.targetTenantId;
    }

    const response = await this.#transport.get(
      `${SCHEDULER_API_PATH}/jobs`,
      { query },
    );

    const jobs = (response?.jobs || []).map(job => this.#transformJobInfo(job));
    return {
      jobs,
      totalCount: response?.total_count || jobs.length,
      nextCursor: response?.next_cursor,
      hasMore: response?.has_more === true,
    };
  }

  /**
   * Triggers immediate execution of a job.
   *
   * @param {string} jobId - The job identifier
   * @returns {Promise<TriggerJobResponse>} Trigger result
   * @throws {ValidationError} If jobId is invalid
   * @throws {NotFoundError} If the job does not exist
   * @throws {QuicBaaSError} If the server returns an error
   */
  async triggerJob(jobId) {
    requiredString(jobId, 'jobId');

    const response = await this.#transport.post(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}/trigger`,
      {},
    );

    return {
      jobId: this.#idToString(response?.job_id, jobId),
      status: response?.status || '',
      triggeredAt: response?.timestamp || '',
    };
  }

  /**
   * Pauses a job, preventing future executions.
   *
   * @param {string} jobId - The job identifier
   * @returns {Promise<JobActionResponse>} Pause result
   * @throws {ValidationError} If jobId is invalid
   * @throws {NotFoundError} If the job does not exist
   * @throws {QuicBaaSError} If the server returns an error
   */
  async pauseJob(jobId) {
    return this.#jobAction(jobId, 'pause');
  }

  /**
   * Resumes a paused job.
   *
   * @param {string} jobId - The job identifier
   * @returns {Promise<JobActionResponse>} Resume result
   * @throws {ValidationError} If jobId is invalid
   * @throws {NotFoundError} If the job does not exist
   * @throws {QuicBaaSError} If the server returns an error
   */
  async resumeJob(jobId) {
    return this.#jobAction(jobId, 'resume');
  }

  /**
   * Cancels a job permanently.
   *
   * @param {string} jobId - The job identifier
   * @param {string} [reason] - Optional cancellation reason
   * @returns {Promise<JobActionResponse>} Cancellation result
   * @throws {ValidationError} If jobId is invalid
   * @throws {NotFoundError} If the job does not exist
   * @throws {QuicBaaSError} If the server returns an error
   */
  async cancelJob(jobId, reason) {
    if (reason !== undefined && typeof reason !== 'string') {
      throw new ValidationError('reason must be a string', 'reason');
    }

    const requestBody = {};
    if (reason !== undefined) {
      requestBody.reason = reason;
    }

    return this.#jobAction(jobId, 'cancel', requestBody);
  }

  /**
   * Retrieves execution history for a job.
   *
   * @param {string} jobId - The job identifier
   * @param {JobHistoryOptions} [options={}] - Pagination options
   * @returns {Promise<JobHistoryResponse>} Execution history
   * @throws {ValidationError} If parameters are invalid
   * @throws {NotFoundError} If the job does not exist
   * @throws {QuicBaaSError} If the server returns an error
   */
  async getJobHistory(jobId, options = {}) {
    requiredString(jobId, 'jobId');
    this.#validateJobHistoryOptions(options);

    const query = {};
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor !== undefined) {
      query.cursor = options.cursor;
    }

    const response = await this.#transport.get(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}/history`,
      { query },
    );

    return {
      jobId,
      executions: (response?.executions || []).map(exec => this.#transformJobExecution(exec)),
      nextCursor: response?.next_cursor,
      hasMore: response?.has_more === true,
    };
  }

  /**
   * Retrieves the scheduler's status and counters.
   *
   * @returns {Promise<SchedulerStatus>} Scheduler status
   * @throws {QuicBaaSError} If the server returns an error
   */
  async getStatus() {
    const response = await this.#transport.get(
      `${SCHEDULER_API_PATH}/status`,
    );
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    const stats = response.stats || {};
    return {
      running: response.running === true,
      stats: {
        jobCount: stats.job_count || 0,
        runningJobs: stats.running_jobs || 0,
        successCount: stats.success_count || 0,
        failureCount: stats.failure_count || 0,
        lastTickTime: stats.last_tick_time,
        startedAt: stats.started_at,
      },
    };
  }

  /**
   * Posts a pause, resume or cancel action and maps the server's
   * old_status/new_status response.
   *
   * @param {string} jobId - The job identifier
   * @param {string} action - The action path segment
   * @param {Object} [requestBody={}] - Request body
   * @returns {Promise<JobActionResponse>} Action result
   * @private
   */
  async #jobAction(jobId, action, requestBody = {}) {
    requiredString(jobId, 'jobId');

    const response = await this.#transport.post(
      `${SCHEDULER_API_PATH}/jobs/${encodeURIComponent(jobId)}/${action}`,
      requestBody,
    );

    return {
      jobId: this.#idToString(response?.job_id, jobId),
      previousStatus: response?.old_status || '',
      newStatus: response?.new_status || '',
    };
  }

  /**
   * Builds a request body from the options a field table names, converting
   * executeAt to RFC 3339.
   *
   * @param {Object} options - Caller options
   * @param {Object<string, string>} fields - Option name to JSON field name
   * @returns {Object} The request body
   * @private
   */
  #toRequestBody(options, fields) {
    const body = {};
    for (const [option, field] of Object.entries(fields)) {
      const value = options[option];
      if (value === undefined) {
        continue;
      }
      body[field] = option === 'executeAt' ? this.#toRFC3339(value) : value;
    }
    return body;
  }

  /**
   * Converts a Date or RFC 3339 string to an RFC 3339 string.
   *
   * @param {Date|string} value - The time
   * @returns {string} RFC 3339 time
   * @private
   */
  #toRFC3339(value) {
    return value instanceof Date ? value.toISOString() : value;
  }

  /**
   * Validates job creation request.
   *
   * @param {CreateJobRequest} request - Request to validate
   * @throws {ValidationError} If validation fails
   * @private
   */
  #validateCreateJobRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.name, 'request.name');
    requiredString(request.type, 'request.type');
    oneOf(request.type, Object.values(JobType), 'request.type');
    requiredString(request.handler, 'request.handler');
    required(request.payload, 'request.payload');

    if (request.type === JobType.RECURRING && request.schedule === undefined) {
      throw new ValidationError('request.schedule is required for recurring jobs', 'request.schedule');
    }
    if (request.type === JobType.DELAYED && request.delaySeconds === undefined) {
      throw new ValidationError('request.delaySeconds is required for delayed jobs', 'request.delaySeconds');
    }

    this.#validateOptionalString(request.description, 'request.description');
    this.#validateOptionalString(request.schedule, 'request.schedule');
    this.#validateExecuteAt(request.executeAt, 'request.executeAt');
    positiveInteger(request.delaySeconds, 'request.delaySeconds');
    if (request.ownerType !== undefined) {
      oneOf(request.ownerType, Object.values(OwnerType), 'request.ownerType');
    }
    this.#validateOptionalString(request.ownerId, 'request.ownerId');
    this.#validateOptionalString(request.targetTenantId, 'request.targetTenantId');
    this.#validateOptionalString(request.nodeAffinity, 'request.nodeAffinity');
    this.#validateTuning(request, 'request');
  }

  /**
   * Validates job update request.
   *
   * @param {UpdateJobRequest} updates - Updates to validate
   * @throws {ValidationError} If validation fails
   * @private
   */
  #validateUpdateJobRequest(updates) {
    if (updates.name !== undefined && (typeof updates.name !== 'string' || updates.name.trim() === '')) {
      throw new ValidationError('updates.name must be a non-empty string', 'updates.name');
    }
    if (updates.handler !== undefined && (typeof updates.handler !== 'string' || updates.handler.trim() === '')) {
      throw new ValidationError('updates.handler must be a non-empty string', 'updates.handler');
    }
    this.#validateOptionalString(updates.description, 'updates.description');
    this.#validateOptionalString(updates.schedule, 'updates.schedule');
    this.#validateExecuteAt(updates.executeAt, 'updates.executeAt');
    if (updates.payload === null) {
      throw new ValidationError('updates.payload cannot be null', 'updates.payload');
    }
    this.#validateTuning(updates, 'updates');
  }

  /**
   * Validates the priority, timeoutSeconds, maxRetries and labels options
   * that create and update share.
   *
   * @param {Object} options - Options to validate
   * @param {string} prefix - Field-name prefix for error messages
   * @throws {ValidationError} If validation fails
   * @private
   */
  #validateTuning(options, prefix) {
    if (options.priority !== undefined && !Number.isInteger(options.priority)) {
      throw new ValidationError(`${prefix}.priority must be an integer`, `${prefix}.priority`);
    }
    positiveInteger(options.timeoutSeconds, `${prefix}.timeoutSeconds`);
    if (options.maxRetries !== undefined) {
      nonNegativeInteger(options.maxRetries, `${prefix}.maxRetries`);
    }
    if (options.labels !== undefined) {
      this.#validateLabels(options.labels, `${prefix}.labels`);
    }
  }

  /**
   * Validates an optional string field.
   *
   * @param {*} value - Value to validate
   * @param {string} name - Field name for error messages
   * @throws {ValidationError} If the value is set and not a string
   * @private
   */
  #validateOptionalString(value, name) {
    if (value !== undefined && typeof value !== 'string') {
      throw new ValidationError(`${name} must be a string`, name);
    }
  }

  /**
   * Validates an optional execution time: a valid Date or a parseable
   * date-time string.
   *
   * @param {*} value - Value to validate
   * @param {string} name - Field name for error messages
   * @throws {ValidationError} If the value is set and not a valid time
   * @private
   */
  #validateExecuteAt(value, name) {
    if (value === undefined) {
      return;
    }
    const valid = value instanceof Date
      ? !Number.isNaN(value.getTime())
      : typeof value === 'string' && !Number.isNaN(Date.parse(value));
    if (!valid) {
      throw new ValidationError(`${name} must be a Date or an RFC 3339 string`, name);
    }
  }

  /**
   * Validates a labels map: an object of string values.
   *
   * @param {*} labels - Labels to validate
   * @param {string} name - Field name for error messages
   * @throws {ValidationError} If validation fails
   * @private
   */
  #validateLabels(labels, name) {
    if (labels === null || typeof labels !== 'object' || Array.isArray(labels)) {
      throw new ValidationError(`${name} must be an object`, name);
    }
    for (const [key, value] of Object.entries(labels)) {
      if (typeof value !== 'string') {
        throw new ValidationError(`${name}.${key} must be a string`, `${name}.${key}`);
      }
    }
  }

  /**
   * Validates list jobs options.
   *
   * @param {ListJobsOptions} options - Options to validate
   * @throws {ValidationError} If validation fails
   * @private
   */
  #validateListJobsOptions(options) {
    this.#validatePage(options);
    if (options.status !== undefined) {
      oneOf(options.status, Object.values(JobStatus), 'options.status');
    }
    if (options.ownerType !== undefined) {
      oneOf(options.ownerType, Object.values(OwnerType), 'options.ownerType');
    }
    this.#validateOptionalString(options.handler, 'options.handler');
    this.#validateOptionalString(options.ownerId, 'options.ownerId');
    this.#validateOptionalString(options.targetTenantId, 'options.targetTenantId');
  }

  /**
   * Validates job history options.
   *
   * @param {JobHistoryOptions} options - Options to validate
   * @throws {ValidationError} If validation fails
   * @private
   */
  #validateJobHistoryOptions(options) {
    this.#validatePage(options);
  }

  /**
   * Validates the limit and cursor pagination options.
   *
   * @param {Object} options - Options to validate
   * @throws {ValidationError} If validation fails
   * @private
   */
  #validatePage(options) {
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
      if (options.limit > 1000) {
        throw new ValidationError('options.limit cannot exceed 1000', 'options.limit');
      }
    }
    this.#validateOptionalString(options.cursor, 'options.cursor');
  }

  /**
   * Converts a server job ID (a JSON number) to the string the client's
   * methods take.
   *
   * @param {number|undefined} id - Server job ID
   * @param {string} [fallback=''] - Value when the server sent none
   * @returns {string} Job ID string
   * @private
   */
  #idToString(id, fallback = '') {
    return id === undefined || id === null ? fallback : String(id);
  }

  /**
   * Converts a Go time.Duration (nanoseconds) to milliseconds.
   *
   * @param {number|undefined} nanoseconds - Duration in nanoseconds
   * @returns {number|undefined} Duration in milliseconds
   * @private
   */
  #durationToMs(nanoseconds) {
    return nanoseconds === undefined ? undefined : nanoseconds / NANOSECONDS_PER_MILLISECOND;
  }

  /**
   * Transforms a server job (SchedulerJobRESTResponse) to JobInfo.
   *
   * @param {Object} job - Server job
   * @returns {JobInfo} Transformed job info
   * @throws {ValidationError} If the response carries no job
   * @private
   */
  #transformJobInfo(job) {
    if (!job) {
      throw new ValidationError('Invalid job data from server', 'job');
    }

    return {
      jobId: this.#idToString(job.id),
      name: job.name || '',
      description: job.description,
      type: job.type || '',
      status: job.status || '',
      handler: job.handler || '',
      schedule: job.schedule,
      executeAt: job.execute_at,
      delayMs: this.#durationToMs(job.delay),
      payload: job.payload || {},
      priority: job.priority || 0,
      timeoutMs: this.#durationToMs(job.timeout) || 0,
      maxRetries: job.max_retries || 0,
      ownerType: job.owner_type || '',
      ownerId: job.owner_id,
      targetTenantId: job.target_tenant_id,
      createdBy: job.created_by,
      labels: job.labels || {},
      createdAt: job.created_at,
      updatedAt: job.updated_at,
      lastRunAt: job.last_run,
      nextRunAt: job.next_run,
      runCount: job.run_count || 0,
      failureCount: job.fail_count || 0,
      lastError: job.last_error,
    };
  }

  /**
   * Transforms a server execution (SchedulerJobExecutionRESTResponse) to
   * JobExecution.
   *
   * @param {Object} execution - Server execution
   * @returns {JobExecution} Transformed execution
   * @private
   */
  #transformJobExecution(execution) {
    return {
      executionId: this.#idToString(execution.id),
      jobId: this.#idToString(execution.job_id),
      status: execution.status || '',
      startedAt: execution.started_at,
      endedAt: execution.ended_at,
      durationMs: this.#durationToMs(execution.duration),
      error: execution.error,
      output: execution.output,
    };
  }
}

export default SchedulerClient;
