/**
 * Scheduler Client Tests
 */

import SchedulerClient, {
  JobType,
  JobStatus,
  OwnerType,
} from '../src/clients/scheduler.js';
import { ValidationError } from '../src/errors/index.js';

// Mock transport for testing
const createMockTransport = (responses = {}) => ({
  request: jest.fn(),
  get: jest.fn().mockImplementation((path) => {
    if (responses.get && responses.get[path]) {
      return Promise.resolve(responses.get[path]);
    }
    return Promise.resolve({});
  }),
  post: jest.fn().mockImplementation((path) => {
    if (responses.post && responses.post[path]) {
      return Promise.resolve(responses.post[path]);
    }
    return Promise.resolve({});
  }),
  put: jest.fn().mockImplementation((path) => {
    if (responses.put && responses.put[path]) {
      return Promise.resolve(responses.put[path]);
    }
    return Promise.resolve({});
  }),
  patch: jest.fn().mockImplementation((path) => {
    if (responses.patch && responses.patch[path]) {
      return Promise.resolve(responses.patch[path]);
    }
    return Promise.resolve({});
  }),
  delete: jest.fn().mockImplementation((path) => {
    if (responses.delete && responses.delete[path]) {
      return Promise.resolve(responses.delete[path]);
    }
    return Promise.resolve({});
  }),
});

describe('SchedulerClient', () => {
  describe('constructor', () => {
    it('should create a client with valid transport', () => {
      const transport = createMockTransport();
      const client = new SchedulerClient(transport);
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
        post: {
          '/api/v1/scheduler/jobs': {
            job_id: '123',
            name: 'test-job',
            status: 'pending',
            job_type: 'one-shot',
            created_at: Date.now(),
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should create a one-shot job', async () => {
      const result = await client.createJob({
        name: 'test-job',
        type: JobType.ONE_SHOT,
        payload: { data: 'test' },
      });

      expect(result).toBeDefined();
      expect(result.jobId).toBe('123');
      expect(transport.post).toHaveBeenCalled();
    });

    it('should create a recurring job with schedule', async () => {
      const result = await client.createJob({
        name: 'recurring-job',
        type: JobType.RECURRING,
        schedule: '0 * * * *',
        payload: { data: 'test' },
      });

      expect(result).toBeDefined();
      expect(transport.post).toHaveBeenCalled();
    });

    it('should create a delayed job with delay', async () => {
      const result = await client.createJob({
        name: 'delayed-job',
        type: JobType.DELAYED,
        delayMs: 300000, // 5 minutes in ms
        payload: { data: 'test' },
      });

      expect(result).toBeDefined();
      expect(transport.post).toHaveBeenCalled();
    });

    it('should throw ValidationError when name is missing', async () => {
      await expect(
        client.createJob({
          type: JobType.ONE_SHOT,
          payload: { data: 'test' },
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when type is missing', async () => {
      await expect(
        client.createJob({
          name: 'test-job',
          payload: { data: 'test' },
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when payload is missing', async () => {
      await expect(
        client.createJob({
          name: 'test-job',
          type: JobType.ONE_SHOT,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when recurring job has no schedule', async () => {
      await expect(
        client.createJob({
          name: 'test-job',
          type: JobType.RECURRING,
          payload: { data: 'test' },
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when delayed job has no delay', async () => {
      await expect(
        client.createJob({
          name: 'test-job',
          type: JobType.DELAYED,
          payload: { data: 'test' },
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid type', async () => {
      await expect(
        client.createJob({
          name: 'test-job',
          type: 'invalid',
          payload: { data: 'test' },
        }),
      ).rejects.toThrow(ValidationError);
    });
  });

  describe('getJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        get: {
          '/api/v1/scheduler/jobs/123': {
            job_id: '123',
            name: 'test-job',
            status: 'pending',
            job_type: 'one-shot',
            owner_type: 'system',
            created_at: Date.now(),
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should get a job by ID', async () => {
      const result = await client.getJob('123');

      expect(result).toBeDefined();
      expect(result.jobId).toBe('123');
      expect(result.name).toBe('test-job');
      expect(result.status).toBe('pending');
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.getJob()).rejects.toThrow(ValidationError);
      await expect(client.getJob('')).rejects.toThrow(ValidationError);
    });
  });

  describe('updateJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        patch: {
          '/api/v1/scheduler/jobs/123': {
            job_id: '123',
            name: 'updated-job',
            status: 'pending',
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should update a job', async () => {
      const result = await client.updateJob('123', {
        name: 'updated-job',
        description: 'Updated description',
      });

      expect(result).toBeDefined();
      expect(transport.patch).toHaveBeenCalled();
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(
        client.updateJob('', { name: 'updated' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when updates is not an object', async () => {
      await expect(client.updateJob('123', null)).rejects.toThrow(ValidationError);
      await expect(client.updateJob('123', 'invalid')).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        delete: {
          '/api/v1/scheduler/jobs/123': { success: true },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should delete a job', async () => {
      const result = await client.deleteJob('123');

      expect(result).toBeDefined();
      expect(result.success).toBe(true);
      expect(transport.delete).toHaveBeenCalled();
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.deleteJob()).rejects.toThrow(ValidationError);
      await expect(client.deleteJob('')).rejects.toThrow(ValidationError);
    });
  });

  describe('listJobs', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        get: {
          '/api/v1/scheduler/jobs': {
            jobs: [
              { job_id: '1', name: 'job-1', status: 'pending' },
              { job_id: '2', name: 'job-2', status: 'running' },
            ],
            total_count: 2,
            has_more: false,
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should list all jobs', async () => {
      const result = await client.listJobs();

      expect(result).toBeDefined();
      expect(result.jobs).toHaveLength(2);
      expect(result.totalCount).toBe(2);
    });

    it('should list jobs with filters', async () => {
      await client.listJobs({
        status: JobStatus.PENDING,
        ownerType: OwnerType.SYSTEM,
        limit: 10,
        offset: 0,
      });

      expect(transport.get).toHaveBeenCalled();
    });

    it('should throw ValidationError for invalid status', async () => {
      await expect(
        client.listJobs({ status: 'invalid-status' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid ownerType', async () => {
      await expect(
        client.listJobs({ ownerType: 'invalid-owner' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative limit', async () => {
      await expect(client.listJobs({ limit: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative offset', async () => {
      await expect(client.listJobs({ offset: -1 })).rejects.toThrow(ValidationError);
    });
  });

  describe('triggerJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        post: {
          '/api/v1/scheduler/jobs/123/trigger': {
            job_id: '123',
            execution_id: '456',
            triggered_at: Date.now(),
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should trigger a job immediately', async () => {
      const result = await client.triggerJob('123');

      expect(result).toBeDefined();
      expect(result.jobId).toBe('123');
      expect(result.executionId).toBe('456');
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.triggerJob()).rejects.toThrow(ValidationError);
      await expect(client.triggerJob('')).rejects.toThrow(ValidationError);
    });
  });

  describe('pauseJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        post: {
          '/api/v1/scheduler/jobs/123/pause': {
            job_id: '123',
            previous_status: 'pending',
            new_status: 'paused',
            paused_at: Date.now(),
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should pause a job', async () => {
      const result = await client.pauseJob('123');

      expect(result).toBeDefined();
      expect(result.success).toBe(true);
      expect(result.newStatus).toBe('paused');
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.pauseJob()).rejects.toThrow(ValidationError);
      await expect(client.pauseJob('')).rejects.toThrow(ValidationError);
    });
  });

  describe('resumeJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        post: {
          '/api/v1/scheduler/jobs/123/resume': {
            job_id: '123',
            previous_status: 'paused',
            new_status: 'pending',
            resumed_at: Date.now(),
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should resume a paused job', async () => {
      const result = await client.resumeJob('123');

      expect(result).toBeDefined();
      expect(result.success).toBe(true);
      expect(result.newStatus).toBe('pending');
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.resumeJob()).rejects.toThrow(ValidationError);
      await expect(client.resumeJob('')).rejects.toThrow(ValidationError);
    });
  });

  describe('cancelJob', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        post: {
          '/api/v1/scheduler/jobs/123/cancel': {
            job_id: '123',
            previous_status: 'pending',
            new_status: 'canceled',
            canceled_at: Date.now(),
            reason: 'No longer needed',
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should cancel a job with reason', async () => {
      const result = await client.cancelJob('123', 'No longer needed');

      expect(result).toBeDefined();
      expect(result.success).toBe(true);
      expect(result.newStatus).toBe('canceled');
    });

    it('should cancel a job without reason', async () => {
      const result = await client.cancelJob('123');

      expect(result).toBeDefined();
      expect(result.success).toBe(true);
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.cancelJob()).rejects.toThrow(ValidationError);
      await expect(client.cancelJob('')).rejects.toThrow(ValidationError);
    });
  });

  describe('getJobHistory', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        get: {
          '/api/v1/scheduler/jobs/123/history': {
            job_id: '123',
            executions: [
              {
                execution_id: '1',
                status: 'completed',
                started_at: Date.now() - 3600000,
                completed_at: Date.now() - 3500000,
              },
              {
                execution_id: '2',
                status: 'failed',
                started_at: Date.now() - 7200000,
                completed_at: Date.now() - 7100000,
                error: 'Connection timeout',
              },
            ],
            total_count: 2,
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should get job execution history', async () => {
      const result = await client.getJobHistory('123');

      expect(result).toBeDefined();
      expect(result.jobId).toBe('123');
      expect(result.executions).toHaveLength(2);
    });

    it('should get history with options', async () => {
      await client.getJobHistory('123', {
        limit: 10,
        offset: 0,
        status: JobStatus.COMPLETED,
      });

      expect(transport.get).toHaveBeenCalled();
    });

    it('should throw ValidationError when jobId is missing', async () => {
      await expect(client.getJobHistory()).rejects.toThrow(ValidationError);
      await expect(client.getJobHistory('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative limit', async () => {
      await expect(
        client.getJobHistory('123', { limit: -1 }),
      ).rejects.toThrow(ValidationError);
    });
  });

  describe('getStatus', () => {
    let client;
    let transport;

    beforeEach(() => {
      transport = createMockTransport({
        get: {
          '/api/v1/scheduler/status': {
            running: true,
            active_jobs: 100,
            pending_jobs: 20,
            running_jobs: 5,
            completed_jobs: 70,
            failed_jobs: 5,
            paused_jobs: 2,
            uptime: 86400000,
            version: '1.0.0',
          },
        },
      });
      client = new SchedulerClient(transport);
    });

    it('should get scheduler status', async () => {
      const result = await client.getStatus();

      expect(result).toBeDefined();
      expect(result.running).toBe(true);
      expect(result.activeJobs).toBe(100);
      expect(result.pendingJobs).toBe(20);
      expect(result.runningJobs).toBe(5);
      expect(result.version).toBe('1.0.0');
    });
  });
});

describe('Enums', () => {
  describe('JobType', () => {
    it('should have correct values', () => {
      expect(JobType.ONE_SHOT).toBe('one-shot');
      expect(JobType.RECURRING).toBe('recurring');
      expect(JobType.DELAYED).toBe('delayed');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(JobType)).toBe(true);
    });
  });

  describe('JobStatus', () => {
    it('should have correct values', () => {
      expect(JobStatus.PENDING).toBe('pending');
      expect(JobStatus.RUNNING).toBe('running');
      expect(JobStatus.COMPLETED).toBe('completed');
      expect(JobStatus.FAILED).toBe('failed');
      expect(JobStatus.PAUSED).toBe('paused');
      expect(JobStatus.CANCELED).toBe('canceled');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(JobStatus)).toBe(true);
    });
  });

  describe('OwnerType', () => {
    it('should have correct values', () => {
      expect(OwnerType.SYSTEM).toBe('system');
      expect(OwnerType.TENANT).toBe('tenant');
      expect(OwnerType.DELEGATED).toBe('delegated');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(OwnerType)).toBe(true);
    });
  });
});
