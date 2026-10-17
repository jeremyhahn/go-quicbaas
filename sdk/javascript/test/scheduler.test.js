/**
 * Scheduler Client Tests
 *
 * Request bodies and responses mirror the REST contract in
 * pkg/api/rest/handlers_scheduler.go.
 */

import SchedulerClient, {
  JobType,
  JobStatus,
  OwnerType,
} from '../src/clients/scheduler.js';
import { ValidationError } from '../src/errors/index.js';

const JOBS_PATH = '/api/v1/scheduler/jobs';

// Mock transport: each verb resolves the response registered for its path.
const createMockTransport = (responses = {}) => {
  const verb = (name) => jest.fn().mockImplementation((path) => {
    const byPath = responses[name] || {};
    return Promise.resolve(byPath[path] || {});
  });
  return {
    request: jest.fn(),
    get: verb('get'),
    post: verb('post'),
    put: verb('put'),
    patch: verb('patch'),
    delete: verb('delete'),
  };
};

// A job as SchedulerJobRESTResponse serializes it.
const serverJob = (overrides = {}) => ({
  id: 123,
  name: 'test-job',
  description: 'a job',
  owner_type: 'tenant',
  owner_id: 'tenant-a',
  target_tenant_id: 'tenant-b',
  created_by: 'admin',
  type: 'one-shot',
  schedule: '',
  execute_at: '2026-10-17T12:00:00Z',
  delay: 5_000_000_000,
  handler: 'noop',
  payload: { data: 'test' },
  priority: 3,
  timeout: 30_000_000_000,
  max_retries: 2,
  status: 'pending',
  last_run: '0001-01-01T00:00:00Z',
  next_run: '2026-10-17T12:00:00Z',
  run_count: 4,
  fail_count: 1,
  last_error: 'boom',
  labels: { team: 'core' },
  created_at: '2026-10-17T11:00:00Z',
  updated_at: '2026-10-17T11:30:00Z',
  ...overrides,
});

const baseRequest = (overrides = {}) => ({
  name: 'test-job',
  type: JobType.ONE_SHOT,
  handler: 'noop',
  payload: { data: 'test' },
  ...overrides,
});

describe('SchedulerClient', () => {
  describe('constructor', () => {
    it('should create a client with valid transport', () => {
      const client = new SchedulerClient(createMockTransport());
      expect(client).toBeInstanceOf(SchedulerClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new SchedulerClient()).toThrow(ValidationError);
      expect(() => new SchedulerClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport has no request method', () => {
      expect(() => new SchedulerClient({})).toThrow(ValidationError);
    });
  });

  describe('createJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        post: { [JOBS_PATH]: { job: serverJob(), timestamp: '2026-10-17T11:00:00Z' } },
      });
      client = new SchedulerClient(transport);
    });

    it('should send the minimal one-shot body the server requires', async () => {
      await client.createJob(baseRequest());

      expect(transport.post).toHaveBeenCalledWith(JOBS_PATH, {
        name: 'test-job',
        type: 'one-shot',
        handler: 'noop',
        payload: { data: 'test' },
      });
    });

    it('should map every option onto the server field names', async () => {
      await client.createJob(baseRequest({
        description: 'a job',
        executeAt: new Date('2026-10-17T12:00:00Z'),
        ownerType: OwnerType.TENANT,
        ownerId: 'tenant-a',
        targetTenantId: 'tenant-b',
        priority: 3,
        timeoutSeconds: 30,
        maxRetries: 2,
        nodeAffinity: 'node-1',
        labels: { team: 'core' },
      }));

      expect(transport.post).toHaveBeenCalledWith(JOBS_PATH, {
        name: 'test-job',
        type: 'one-shot',
        handler: 'noop',
        description: 'a job',
        execute_at: '2026-10-17T12:00:00.000Z',
        owner_type: 'tenant',
        owner_id: 'tenant-a',
        target_tenant_id: 'tenant-b',
        payload: { data: 'test' },
        priority: 3,
        timeout_seconds: 30,
        max_retries: 2,
        node_affinity: 'node-1',
        labels: { team: 'core' },
      });
    });

    it('should send schedule for a recurring job', async () => {
      await client.createJob(baseRequest({ type: JobType.RECURRING, schedule: '0 * * * *' }));

      expect(transport.post).toHaveBeenCalledWith(JOBS_PATH, {
        name: 'test-job',
        type: 'recurring',
        handler: 'noop',
        schedule: '0 * * * *',
        payload: { data: 'test' },
      });
    });

    it('should send delay_seconds for a delayed job', async () => {
      await client.createJob(baseRequest({ type: JobType.DELAYED, delaySeconds: 300 }));

      expect(transport.post).toHaveBeenCalledWith(JOBS_PATH, {
        name: 'test-job',
        type: 'delayed',
        handler: 'noop',
        delay_seconds: 300,
        payload: { data: 'test' },
      });
    });

    it('should pass an RFC 3339 executeAt string through unchanged', async () => {
      await client.createJob(baseRequest({ executeAt: '2026-10-17T12:00:00Z' }));

      expect(transport.post.mock.calls[0][1].execute_at).toBe('2026-10-17T12:00:00Z');
    });

    it('should return the created job from the response envelope', async () => {
      const job = await client.createJob(baseRequest());

      expect(job.jobId).toBe('123');
      expect(job.name).toBe('test-job');
      expect(job.handler).toBe('noop');
      expect(job.status).toBe(JobStatus.PENDING);
    });

    it('should throw ValidationError when the response has no job', async () => {
      transport.post.mockResolvedValueOnce({ timestamp: '2026-10-17T11:00:00Z' });
      await expect(client.createJob(baseRequest())).rejects.toThrow(ValidationError);
    });

    it.each([
      ['name', { name: undefined }],
      ['type', { type: undefined }],
      ['handler', { handler: undefined }],
      ['empty handler', { handler: '  ' }],
      ['payload', { payload: undefined }],
      ['invalid type', { type: 'invalid-type' }],
      ['recurring job without schedule', { type: JobType.RECURRING }],
      ['delayed job without delaySeconds', { type: JobType.DELAYED }],
      ['non-positive delaySeconds', { type: JobType.DELAYED, delaySeconds: 0 }],
      ['unparseable executeAt', { executeAt: 'tomorrow-ish' }],
      ['invalid Date executeAt', { executeAt: new Date('nope') }],
      ['non-integer priority', { priority: 1.5 }],
      ['non-positive timeoutSeconds', { timeoutSeconds: 0 }],
      ['negative maxRetries', { maxRetries: -1 }],
      ['labels array', { labels: ['a'] }],
      ['non-string label value', { labels: { team: 1 } }],
      ['invalid ownerType', { ownerType: 'nobody' }],
      ['non-string targetTenantId', { targetTenantId: 7 }],
      ['non-string nodeAffinity', { nodeAffinity: 7 }],
    ])('should reject %s without calling the server', async (_label, overrides) => {
      await expect(client.createJob(baseRequest(overrides))).rejects.toThrow(ValidationError);
      expect(transport.post).not.toHaveBeenCalled();
    });

    it('should reject a missing request', async () => {
      await expect(client.createJob()).rejects.toThrow(ValidationError);
    });
  });

  describe('getJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        get: { [`${JOBS_PATH}/123`]: { job: serverJob(), timestamp: '2026-10-17T11:00:00Z' } },
      });
      client = new SchedulerClient(transport);
    });

    it('should map every SchedulerJobRESTResponse field', async () => {
      const job = await client.getJob('123');

      expect(transport.get).toHaveBeenCalledWith(`${JOBS_PATH}/123`);
      expect(job).toEqual({
        jobId: '123',
        name: 'test-job',
        description: 'a job',
        type: 'one-shot',
        status: 'pending',
        handler: 'noop',
        schedule: '',
        executeAt: '2026-10-17T12:00:00Z',
        delayMs: 5000,
        payload: { data: 'test' },
        priority: 3,
        timeoutMs: 30000,
        maxRetries: 2,
        ownerType: 'tenant',
        ownerId: 'tenant-a',
        targetTenantId: 'tenant-b',
        createdBy: 'admin',
        labels: { team: 'core' },
        createdAt: '2026-10-17T11:00:00Z',
        updatedAt: '2026-10-17T11:30:00Z',
        lastRunAt: '0001-01-01T00:00:00Z',
        nextRunAt: '2026-10-17T12:00:00Z',
        runCount: 4,
        failureCount: 1,
        lastError: 'boom',
      });
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.getJob()).rejects.toThrow(ValidationError);
      await expect(client.getJob('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when the response has no job', async () => {
      await expect(client.getJob('999')).rejects.toThrow(ValidationError);
    });
  });

  describe('updateJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        put: { [`${JOBS_PATH}/123`]: { job: serverJob({ name: 'updated-job' }) } },
      });
      client = new SchedulerClient(transport);
    });

    it('should PUT every option under the server field names', async () => {
      const job = await client.updateJob('123', {
        name: 'updated-job',
        description: 'new',
        schedule: '0 2 * * *',
        executeAt: new Date('2026-10-18T00:00:00Z'),
        handler: 'noop',
        payload: { x: 1 },
        priority: 1,
        timeoutSeconds: 60,
        maxRetries: 5,
        labels: { env: 'test' },
      });

      expect(transport.patch).not.toHaveBeenCalled();
      expect(transport.put).toHaveBeenCalledWith(`${JOBS_PATH}/123`, {
        name: 'updated-job',
        description: 'new',
        schedule: '0 2 * * *',
        execute_at: '2026-10-18T00:00:00.000Z',
        handler: 'noop',
        payload: { x: 1 },
        priority: 1,
        timeout_seconds: 60,
        max_retries: 5,
        labels: { env: 'test' },
      });
      expect(job.name).toBe('updated-job');
    });

    it('should send only the fields given', async () => {
      await client.updateJob('123', { description: 'only' });
      expect(transport.put).toHaveBeenCalledWith(`${JOBS_PATH}/123`, { description: 'only' });
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.updateJob(undefined, { name: 'x' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when updates is not an object', async () => {
      await expect(client.updateJob('123', 'invalid')).rejects.toThrow(ValidationError);
    });

    it.each([
      ['empty name', { name: '' }],
      ['empty handler', { handler: ' ' }],
      ['null payload', { payload: null }],
      ['unparseable executeAt', { executeAt: 'later' }],
      ['non-positive timeoutSeconds', { timeoutSeconds: -5 }],
      ['non-integer priority', { priority: '1' }],
      ['non-string label value', { labels: { a: false } }],
    ])('should reject %s without calling the server', async (_label, updates) => {
      await expect(client.updateJob('123', updates)).rejects.toThrow(ValidationError);
      expect(transport.put).not.toHaveBeenCalled();
    });
  });

  describe('deleteJob', () => {
    it('should map the DeleteSchedulerJobRESTResponse', async () => {
      const transport = createMockTransport({
        delete: { [`${JOBS_PATH}/123`]: { job_id: 123, deleted: true } },
      });
      const client = new SchedulerClient(transport);

      const result = await client.deleteJob('123');

      expect(transport.delete).toHaveBeenCalledWith(`${JOBS_PATH}/123`);
      expect(result).toEqual({ jobId: '123', deleted: true });
    });

    it('should throw ValidationError when jobId is missing', async () => {
      const client = new SchedulerClient(createMockTransport());
      await expect(client.deleteJob()).rejects.toThrow(ValidationError);
    });
  });

  describe('listJobs', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        get: {
          [JOBS_PATH]: {
            jobs: [serverJob(), serverJob({ id: 124, name: 'job2' })],
            next_cursor: 'c2',
            has_more: true,
            total_count: 10,
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should list jobs and map the page fields', async () => {
      const result = await client.listJobs();

      expect(transport.get).toHaveBeenCalledWith(JOBS_PATH, { query: {} });
      expect(result.jobs.map(job => job.jobId)).toEqual(['123', '124']);
      expect(result.totalCount).toBe(10);
      expect(result.nextCursor).toBe('c2');
      expect(result.hasMore).toBe(true);
    });

    it('should send the server query parameter names', async () => {
      await client.listJobs({
        limit: 10,
        cursor: 'c1',
        status: JobStatus.PENDING,
        handler: 'noop',
        ownerType: OwnerType.TENANT,
        ownerId: 'tenant-a',
        targetTenantId: 'tenant-b',
      });

      expect(transport.get).toHaveBeenCalledWith(JOBS_PATH, {
        query: {
          limit: 10,
          cursor: 'c1',
          status: 'pending',
          handler: 'noop',
          owner_type: 'tenant',
          owner_id: 'tenant-a',
          target_tenant_id: 'tenant-b',
        },
      });
    });

    it.each([
      ['invalid status', { status: 'invalid' }],
      ['invalid ownerType', { ownerType: 'invalid' }],
      ['negative limit', { limit: -1 }],
      ['limit above 1000', { limit: 1001 }],
      ['non-string cursor', { cursor: 5 }],
    ])('should reject %s', async (_label, options) => {
      await expect(client.listJobs(options)).rejects.toThrow(ValidationError);
    });
  });

  describe('triggerJob', () => {
    it('should map the TriggerSchedulerJobRESTResponse', async () => {
      const transport = createMockTransport({
        post: {
          [`${JOBS_PATH}/123/trigger`]: {
            job_id: 123,
            status: 'running',
            timestamp: '2026-10-17T11:00:00Z',
          },
        },
      });
      const client = new SchedulerClient(transport);

      const result = await client.triggerJob('123');

      expect(transport.post).toHaveBeenCalledWith(`${JOBS_PATH}/123/trigger`, {});
      expect(result).toEqual({
        jobId: '123',
        status: 'running',
        triggeredAt: '2026-10-17T11:00:00Z',
      });
    });

    it('should throw ValidationError when jobId is missing', async () => {
      const client = new SchedulerClient(createMockTransport());
      await expect(client.triggerJob()).rejects.toThrow(ValidationError);
    });
  });

  describe.each([
    ['pauseJob', 'pause', 'pending', 'paused'],
    ['resumeJob', 'resume', 'paused', 'pending'],
    ['cancelJob', 'cancel', 'pending', 'canceled'],
  ])('%s', (method, action, oldStatus, newStatus) => {
    const path = `${JOBS_PATH}/123/${action}`;

    it(`should POST /${action} and map old_status/new_status`, async () => {
      const transport = createMockTransport({
        post: { [path]: { job_id: 123, old_status: oldStatus, new_status: newStatus } },
      });
      const client = new SchedulerClient(transport);

      const result = await client[method]('123');

      expect(transport.post).toHaveBeenCalledWith(path, {});
      expect(result).toEqual({ jobId: '123', previousStatus: oldStatus, newStatus });
    });

    it('should throw ValidationError when jobId is missing', async () => {
      const client = new SchedulerClient(createMockTransport());
      await expect(client[method]()).rejects.toThrow(ValidationError);
    });
  });

  describe('cancelJob reason', () => {
    it('should send the reason in the body', async () => {
      const transport = createMockTransport();
      const client = new SchedulerClient(transport);

      await client.cancelJob('123', 'No longer needed');

      expect(transport.post).toHaveBeenCalledWith(`${JOBS_PATH}/123/cancel`, {
        reason: 'No longer needed',
      });
    });

    it('should throw ValidationError for a non-string reason', async () => {
      const client = new SchedulerClient(createMockTransport());
      await expect(client.cancelJob('123', 42)).rejects.toThrow(ValidationError);
    });
  });

  describe('getJobHistory', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        get: {
          [`${JOBS_PATH}/123/history`]: {
            executions: [{
              id: 7,
              job_id: 123,
              status: 'completed',
              started_at: '2026-10-17T11:00:00Z',
              ended_at: '2026-10-17T11:00:01Z',
              duration: 1_500_000_000,
              output: 'ok',
            }],
            next_cursor: 'h2',
            has_more: false,
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should map the GetSchedulerJobHistoryRESTResponse', async () => {
      const result = await client.getJobHistory('123');

      expect(transport.get).toHaveBeenCalledWith(`${JOBS_PATH}/123/history`, { query: {} });
      expect(result).toEqual({
        jobId: '123',
        executions: [{
          executionId: '7',
          jobId: '123',
          status: 'completed',
          startedAt: '2026-10-17T11:00:00Z',
          endedAt: '2026-10-17T11:00:01Z',
          durationMs: 1500,
          error: undefined,
          output: 'ok',
        }],
        nextCursor: 'h2',
        hasMore: false,
      });
    });

    it('should send limit and cursor', async () => {
      await client.getJobHistory('123', { limit: 10, cursor: 'h1' });

      expect(transport.get).toHaveBeenCalledWith(`${JOBS_PATH}/123/history`, {
        query: { limit: 10, cursor: 'h1' },
      });
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.getJobHistory()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative limit', async () => {
      await expect(client.getJobHistory('123', { limit: -1 })).rejects.toThrow(ValidationError);
    });
  });

  describe('getStatus', () => {
    it('should map the GetSchedulerStatusRESTResponse', async () => {
      const transport = createMockTransport({
        get: {
          '/api/v1/scheduler/status': {
            running: true,
            stats: {
              job_count: 5,
              running_jobs: 1,
              success_count: 9,
              failure_count: 2,
              last_tick_time: '2026-10-17T11:00:00Z',
              started_at: '2026-10-17T10:00:00Z',
            },
          },
        },
      });
      const client = new SchedulerClient(transport);

      const status = await client.getStatus();

      expect(status).toEqual({
        running: true,
        stats: {
          jobCount: 5,
          runningJobs: 1,
          successCount: 9,
          failureCount: 2,
          lastTickTime: '2026-10-17T11:00:00Z',
          startedAt: '2026-10-17T10:00:00Z',
        },
      });
    });

    it('should report zero counters when the server sends no stats', async () => {
      const transport = createMockTransport({
        get: { '/api/v1/scheduler/status': { running: false } },
      });
      const status = await new SchedulerClient(transport).getStatus();

      expect(status.running).toBe(false);
      expect(status.stats.jobCount).toBe(0);
    });

    it('should throw ValidationError on an empty response', async () => {
      const transport = createMockTransport();
      transport.get.mockResolvedValueOnce(null);
      await expect(new SchedulerClient(transport).getStatus()).rejects.toThrow(ValidationError);
    });
  });
});

describe('Enums', () => {
  it('JobType should match the server job types and be frozen', () => {
    expect(JobType).toEqual({ ONE_SHOT: 'one-shot', RECURRING: 'recurring', DELAYED: 'delayed' });
    expect(Object.isFrozen(JobType)).toBe(true);
  });

  it('JobStatus should match the server job statuses and be frozen', () => {
    expect(JobStatus).toEqual({
      PENDING: 'pending',
      RUNNING: 'running',
      COMPLETED: 'completed',
      FAILED: 'failed',
      PAUSED: 'paused',
      CANCELED: 'canceled',
    });
    expect(Object.isFrozen(JobStatus)).toBe(true);
  });

  it('OwnerType should match the server owner types and be frozen', () => {
    expect(OwnerType).toEqual({ SYSTEM: 'system', TENANT: 'tenant', DELEGATED: 'delegated' });
    expect(Object.isFrozen(OwnerType)).toBe(true);
  });
});
