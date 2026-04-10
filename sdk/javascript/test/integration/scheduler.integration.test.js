/**
 * Scheduler SDK Integration Tests
 *
 * These tests run against a real QuicBaaS server.
 * Set QUICBAAS_URL environment variable (default: http://localhost:58080)
 *
 * @module @quicbaas/client/test/integration/scheduler
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { QuicBaaS, JobType, JobStatus, OwnerType } from '../../src/index.js';
import { NotFoundError } from '../../src/errors/index.js';

const QUICBAAS_URL = process.env.QUICBAAS_URL || 'http://localhost:58080';
const TIMEOUT = 30000;

/**
 * Check if the QuicBaaS server is available by hitting the health endpoint.
 * @returns {Promise<boolean>} True if server is available
 */
const checkServerAvailable = async () => {
  try {
    const response = await fetch(`${QUICBAAS_URL}/api/v1/health`, {
      signal: AbortSignal.timeout(5000),
    });
    return response.ok;
  } catch {
    return false;
  }
};

/**
 * Generate a unique job name with timestamp to avoid conflicts.
 * @param {string} prefix - Prefix for the job name
 * @returns {string} Unique job name
 */
const generateJobName = (prefix = 'test-job') => {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).substring(2, 8)}`;
};

describe('Scheduler Integration Tests', () => {
  let db;
  let scheduler;
  let serverAvailable = false;
  const createdJobIds = [];

  beforeAll(async () => {
    serverAvailable = await checkServerAvailable();
    if (!serverAvailable) {
      console.warn(
        `QuicBaaS server not available at ${QUICBAAS_URL}, skipping integration tests`
      );
      return;
    }
    db = new QuicBaaS(QUICBAAS_URL);
    scheduler = db.scheduler();
  }, TIMEOUT);

  afterAll(async () => {
    if (!serverAvailable || !scheduler) {
      return;
    }

    // Clean up all created jobs
    for (const jobId of createdJobIds) {
      try {
        await scheduler.deleteJob(jobId);
      } catch {
        // Ignore cleanup errors - job may already be deleted
      }
    }
  }, TIMEOUT);

  /**
   * Helper to conditionally run tests based on server availability.
   * @param {Function} fn - Test function to run
   * @returns {Function} Test function or no-op
   */
  const skipIfNoServer = (fn) => {
    return serverAvailable ? fn : () => {};
  };

  /**
   * Helper to create a job and track it for cleanup.
   * @param {Object} request - Job creation request
   * @returns {Promise<Object>} Created job response
   */
  const createTrackedJob = async (request) => {
    const result = await scheduler.createJob(request);
    if (result.jobId) {
      createdJobIds.push(result.jobId);
    }
    return result;
  };

  describe('Scheduler Status', () => {
    it(
      'should get scheduler status',
      skipIfNoServer(async () => {
        const status = await scheduler.getStatus();

        expect(status).toBeDefined();
        expect(typeof status.running).toBe('boolean');
        expect(typeof status.activeJobs).toBe('number');
        expect(typeof status.pendingJobs).toBe('number');
        expect(typeof status.runningJobs).toBe('number');
        expect(typeof status.completedJobs).toBe('number');
        expect(typeof status.failedJobs).toBe('number');
        expect(typeof status.pausedJobs).toBe('number');
      }),
      TIMEOUT
    );
  });

  describe('Job Lifecycle', () => {
    it(
      'should create, get, update, and delete a one-shot job',
      skipIfNoServer(async () => {
        const jobName = generateJobName('one-shot');

        // Create job
        const createResult = await createTrackedJob({
          name: jobName,
          type: JobType.ONE_SHOT,
          description: 'Integration test one-shot job',
          runAt: Date.now() + 3600000, // 1 hour from now
          payload: { action: 'test', data: { value: 42 } },
          metadata: { environment: 'test', version: '1.0.0' },
          maxRetries: 3,
          retryDelayMs: 1000,
          timeoutMs: 30000,
          tags: ['integration-test', 'one-shot'],
        });

        expect(createResult).toBeDefined();
        expect(createResult.jobId).toBeDefined();
        expect(createResult.name).toBe(jobName);
        expect(createResult.status).toBeDefined();

        const jobId = createResult.jobId;

        // Get job
        const job = await scheduler.getJob(jobId);

        expect(job).toBeDefined();
        expect(job.jobId).toBe(jobId);
        expect(job.name).toBe(jobName);
        expect(job.type).toBe(JobType.ONE_SHOT);
        expect(job.description).toBe('Integration test one-shot job');
        expect(job.payload).toEqual({ action: 'test', data: { value: 42 } });

        // Update job
        const updatedDescription = 'Updated integration test description';
        const updatedJob = await scheduler.updateJob(jobId, {
          description: updatedDescription,
          maxRetries: 5,
          tags: ['integration-test', 'one-shot', 'updated'],
        });

        expect(updatedJob).toBeDefined();
        expect(updatedJob.description).toBe(updatedDescription);

        // Delete job
        const deleteResult = await scheduler.deleteJob(jobId);

        expect(deleteResult).toBeDefined();
        expect(deleteResult.success).toBe(true);

        // Remove from tracked jobs since we deleted it
        const index = createdJobIds.indexOf(jobId);
        if (index > -1) {
          createdJobIds.splice(index, 1);
        }

        // Verify job is deleted
        await expect(scheduler.getJob(jobId)).rejects.toThrow();
      }),
      TIMEOUT
    );

    it(
      'should create a recurring job with cron schedule',
      skipIfNoServer(async () => {
        const jobName = generateJobName('recurring');

        const createResult = await createTrackedJob({
          name: jobName,
          type: JobType.RECURRING,
          description: 'Integration test recurring job',
          schedule: '*/5 * * * *', // Every 5 minutes
          payload: { action: 'heartbeat' },
          ownerType: OwnerType.SYSTEM,
          tags: ['integration-test', 'recurring'],
        });

        expect(createResult).toBeDefined();
        expect(createResult.jobId).toBeDefined();

        const job = await scheduler.getJob(createResult.jobId);

        expect(job.type).toBe(JobType.RECURRING);
        expect(job.schedule).toBe('*/5 * * * *');
      }),
      TIMEOUT
    );

    it(
      'should create a delayed job',
      skipIfNoServer(async () => {
        const jobName = generateJobName('delayed');

        const createResult = await createTrackedJob({
          name: jobName,
          type: JobType.DELAYED,
          description: 'Integration test delayed job',
          delayMs: 60000, // 1 minute delay
          payload: { action: 'delayed-task' },
          tags: ['integration-test', 'delayed'],
        });

        expect(createResult).toBeDefined();
        expect(createResult.jobId).toBeDefined();

        const job = await scheduler.getJob(createResult.jobId);

        expect(job.type).toBe(JobType.DELAYED);
      }),
      TIMEOUT
    );
  });

  describe('Job Control Operations', () => {
    let controlJobId;

    beforeAll(
      skipIfNoServer(async () => {
        if (!serverAvailable) return;

        const jobName = generateJobName('control');
        const result = await createTrackedJob({
          name: jobName,
          type: JobType.RECURRING,
          schedule: '0 * * * *', // Every hour
          payload: { action: 'control-test' },
          tags: ['integration-test', 'control'],
        });
        controlJobId = result.jobId;
      }),
      TIMEOUT
    );

    it(
      'should pause a job',
      skipIfNoServer(async () => {
        const result = await scheduler.pauseJob(controlJobId);

        expect(result).toBeDefined();
        expect(result.success).toBe(true);
        expect(result.newStatus).toBe(JobStatus.PAUSED);

        // Verify job is paused
        const job = await scheduler.getJob(controlJobId);
        expect(job.status).toBe(JobStatus.PAUSED);
      }),
      TIMEOUT
    );

    it(
      'should resume a paused job',
      skipIfNoServer(async () => {
        // Ensure job is paused first
        await scheduler.pauseJob(controlJobId);

        const result = await scheduler.resumeJob(controlJobId);

        expect(result).toBeDefined();
        expect(result.success).toBe(true);
        expect(result.newStatus).toBe(JobStatus.PENDING);

        // Verify job is resumed
        const job = await scheduler.getJob(controlJobId);
        expect(job.status).toBe(JobStatus.PENDING);
      }),
      TIMEOUT
    );

    it(
      'should trigger a job for immediate execution',
      skipIfNoServer(async () => {
        const result = await scheduler.triggerJob(controlJobId);

        expect(result).toBeDefined();
        expect(result.jobId).toBe(controlJobId);
        expect(result.executionId).toBeDefined();
        expect(result.triggeredAt).toBeDefined();
      }),
      TIMEOUT
    );

    it(
      'should cancel a job with reason',
      skipIfNoServer(async () => {
        // Create a new job specifically for cancellation
        const jobName = generateJobName('cancel');
        const createResult = await createTrackedJob({
          name: jobName,
          type: JobType.ONE_SHOT,
          runAt: Date.now() + 7200000, // 2 hours from now
          payload: { action: 'to-be-canceled' },
          tags: ['integration-test', 'cancel'],
        });

        const result = await scheduler.cancelJob(
          createResult.jobId,
          'Integration test cancellation'
        );

        expect(result).toBeDefined();
        expect(result.success).toBe(true);
        expect(result.newStatus).toBe(JobStatus.CANCELED);

        // Verify job is canceled
        const job = await scheduler.getJob(createResult.jobId);
        expect(job.status).toBe(JobStatus.CANCELED);
      }),
      TIMEOUT
    );
  });

  describe('Job Listing and Filtering', () => {
    const testTagPrefix = `list-test-${Date.now()}`;

    beforeAll(
      skipIfNoServer(async () => {
        if (!serverAvailable) return;

        // Create several jobs for listing tests
        const jobConfigs = [
          {
            name: generateJobName('list-pending'),
            type: JobType.ONE_SHOT,
            runAt: Date.now() + 3600000,
            payload: { index: 1 },
            tags: [testTagPrefix, 'pending'],
          },
          {
            name: generateJobName('list-recurring'),
            type: JobType.RECURRING,
            schedule: '0 0 * * *',
            payload: { index: 2 },
            tags: [testTagPrefix, 'recurring'],
          },
          {
            name: generateJobName('list-delayed'),
            type: JobType.DELAYED,
            delayMs: 120000,
            payload: { index: 3 },
            tags: [testTagPrefix, 'delayed'],
          },
        ];

        for (const config of jobConfigs) {
          await createTrackedJob(config);
        }
      }),
      TIMEOUT
    );

    it(
      'should list all jobs',
      skipIfNoServer(async () => {
        const result = await scheduler.listJobs();

        expect(result).toBeDefined();
        expect(Array.isArray(result.jobs)).toBe(true);
        expect(typeof result.totalCount).toBe('number');
      }),
      TIMEOUT
    );

    it(
      'should list jobs with pagination',
      skipIfNoServer(async () => {
        const result = await scheduler.listJobs({
          limit: 2,
          offset: 0,
        });

        expect(result).toBeDefined();
        expect(result.jobs.length).toBeLessThanOrEqual(2);
      }),
      TIMEOUT
    );

    it(
      'should list jobs filtered by status',
      skipIfNoServer(async () => {
        const result = await scheduler.listJobs({
          status: JobStatus.PENDING,
        });

        expect(result).toBeDefined();
        expect(Array.isArray(result.jobs)).toBe(true);
        for (const job of result.jobs) {
          expect(job.status).toBe(JobStatus.PENDING);
        }
      }),
      TIMEOUT
    );

    it(
      'should list jobs filtered by type',
      skipIfNoServer(async () => {
        const result = await scheduler.listJobs({
          type: JobType.RECURRING,
        });

        expect(result).toBeDefined();
        expect(Array.isArray(result.jobs)).toBe(true);
        for (const job of result.jobs) {
          expect(job.type).toBe(JobType.RECURRING);
        }
      }),
      TIMEOUT
    );

    it(
      'should list jobs filtered by owner type',
      skipIfNoServer(async () => {
        const result = await scheduler.listJobs({
          ownerType: OwnerType.SYSTEM,
        });

        expect(result).toBeDefined();
        expect(Array.isArray(result.jobs)).toBe(true);
      }),
      TIMEOUT
    );

    it(
      'should list jobs filtered by tags',
      skipIfNoServer(async () => {
        const result = await scheduler.listJobs({
          tags: [testTagPrefix],
        });

        expect(result).toBeDefined();
        expect(Array.isArray(result.jobs)).toBe(true);
      }),
      TIMEOUT
    );
  });

  describe('Job History', () => {
    let historyJobId;

    beforeAll(
      skipIfNoServer(async () => {
        if (!serverAvailable) return;

        const jobName = generateJobName('history');
        const result = await createTrackedJob({
          name: jobName,
          type: JobType.ONE_SHOT,
          runAt: Date.now() - 1000, // Set in the past to potentially trigger
          payload: { action: 'history-test' },
          tags: ['integration-test', 'history'],
        });
        historyJobId = result.jobId;

        // Trigger the job to generate some history
        try {
          await scheduler.triggerJob(historyJobId);
        } catch {
          // Job may already be running or completed
        }
      }),
      TIMEOUT
    );

    it(
      'should get job history',
      skipIfNoServer(async () => {
        const result = await scheduler.getJobHistory(historyJobId);

        expect(result).toBeDefined();
        expect(result.jobId).toBe(historyJobId);
        expect(Array.isArray(result.executions)).toBe(true);
        expect(typeof result.totalCount).toBe('number');
      }),
      TIMEOUT
    );

    it(
      'should get job history with pagination',
      skipIfNoServer(async () => {
        const result = await scheduler.getJobHistory(historyJobId, {
          limit: 5,
          offset: 0,
        });

        expect(result).toBeDefined();
        expect(result.executions.length).toBeLessThanOrEqual(5);
      }),
      TIMEOUT
    );

    it(
      'should get job history filtered by time range',
      skipIfNoServer(async () => {
        const now = Date.now();
        const result = await scheduler.getJobHistory(historyJobId, {
          startTime: now - 86400000, // Last 24 hours
          endTime: now,
        });

        expect(result).toBeDefined();
        expect(Array.isArray(result.executions)).toBe(true);
      }),
      TIMEOUT
    );
  });

  describe('Error Handling', () => {
    it(
      'should handle job not found error',
      skipIfNoServer(async () => {
        const nonExistentJobId = 'non-existent-job-id-12345';

        await expect(scheduler.getJob(nonExistentJobId)).rejects.toThrow();
      }),
      TIMEOUT
    );

    it(
      'should handle delete of non-existent job',
      skipIfNoServer(async () => {
        const nonExistentJobId = 'non-existent-job-id-67890';

        await expect(scheduler.deleteJob(nonExistentJobId)).rejects.toThrow();
      }),
      TIMEOUT
    );

    it(
      'should handle update of non-existent job',
      skipIfNoServer(async () => {
        const nonExistentJobId = 'non-existent-job-id-11111';

        await expect(
          scheduler.updateJob(nonExistentJobId, { description: 'Updated' })
        ).rejects.toThrow();
      }),
      TIMEOUT
    );

    it(
      'should handle pause of non-existent job',
      skipIfNoServer(async () => {
        const nonExistentJobId = 'non-existent-job-id-22222';

        await expect(scheduler.pauseJob(nonExistentJobId)).rejects.toThrow();
      }),
      TIMEOUT
    );

    it(
      'should handle resume of non-existent job',
      skipIfNoServer(async () => {
        const nonExistentJobId = 'non-existent-job-id-33333';

        await expect(scheduler.resumeJob(nonExistentJobId)).rejects.toThrow();
      }),
      TIMEOUT
    );

    it(
      'should handle cancel of non-existent job',
      skipIfNoServer(async () => {
        const nonExistentJobId = 'non-existent-job-id-44444';

        await expect(scheduler.cancelJob(nonExistentJobId)).rejects.toThrow();
      }),
      TIMEOUT
    );

    it(
      'should handle trigger of non-existent job',
      skipIfNoServer(async () => {
        const nonExistentJobId = 'non-existent-job-id-55555';

        await expect(scheduler.triggerJob(nonExistentJobId)).rejects.toThrow();
      }),
      TIMEOUT
    );

    it(
      'should handle history of non-existent job',
      skipIfNoServer(async () => {
        const nonExistentJobId = 'non-existent-job-id-66666';

        await expect(scheduler.getJobHistory(nonExistentJobId)).rejects.toThrow();
      }),
      TIMEOUT
    );
  });

  describe('Job with Different Owner Types', () => {
    it(
      'should create a tenant-owned job',
      skipIfNoServer(async () => {
        const jobName = generateJobName('tenant');
        const tenantId = 'test-tenant-123';

        const result = await createTrackedJob({
          name: jobName,
          type: JobType.ONE_SHOT,
          runAt: Date.now() + 3600000,
          payload: { action: 'tenant-task' },
          ownerType: OwnerType.TENANT,
          ownerId: tenantId,
          tags: ['integration-test', 'tenant'],
        });

        expect(result).toBeDefined();
        expect(result.jobId).toBeDefined();

        const job = await scheduler.getJob(result.jobId);
        expect(job.ownerType).toBe(OwnerType.TENANT);
        expect(job.ownerId).toBe(tenantId);
      }),
      TIMEOUT
    );

    it(
      'should create a delegated job',
      skipIfNoServer(async () => {
        const jobName = generateJobName('delegated');

        const result = await createTrackedJob({
          name: jobName,
          type: JobType.ONE_SHOT,
          runAt: Date.now() + 3600000,
          payload: { action: 'delegated-task' },
          ownerType: OwnerType.DELEGATED,
          ownerId: 'delegated-owner-456',
          tags: ['integration-test', 'delegated'],
        });

        expect(result).toBeDefined();
        expect(result.jobId).toBeDefined();

        const job = await scheduler.getJob(result.jobId);
        expect(job.ownerType).toBe(OwnerType.DELEGATED);
      }),
      TIMEOUT
    );
  });

  describe('Job with Complex Payload', () => {
    it(
      'should handle complex nested payload',
      skipIfNoServer(async () => {
        const jobName = generateJobName('complex-payload');

        const complexPayload = {
          action: 'process-data',
          config: {
            source: {
              type: 'database',
              connection: {
                host: 'localhost',
                port: 5432,
                database: 'test',
              },
            },
            transforms: [
              { type: 'filter', condition: 'active = true' },
              { type: 'map', fields: ['id', 'name', 'email'] },
              { type: 'aggregate', groupBy: 'department' },
            ],
            destination: {
              type: 's3',
              bucket: 'output-bucket',
              prefix: 'processed/',
            },
          },
          options: {
            batchSize: 1000,
            parallelism: 4,
            retryOnFailure: true,
          },
        };

        const result = await createTrackedJob({
          name: jobName,
          type: JobType.ONE_SHOT,
          runAt: Date.now() + 3600000,
          payload: complexPayload,
          tags: ['integration-test', 'complex'],
        });

        expect(result).toBeDefined();
        expect(result.jobId).toBeDefined();

        const job = await scheduler.getJob(result.jobId);
        expect(job.payload).toEqual(complexPayload);
      }),
      TIMEOUT
    );
  });

  describe('Job Update Operations', () => {
    let updateJobId;

    beforeAll(
      skipIfNoServer(async () => {
        if (!serverAvailable) return;

        const jobName = generateJobName('update');
        const result = await createTrackedJob({
          name: jobName,
          type: JobType.RECURRING,
          schedule: '0 0 * * *',
          description: 'Original description',
          payload: { version: 1 },
          metadata: { key1: 'value1' },
          maxRetries: 3,
          retryDelayMs: 1000,
          timeoutMs: 30000,
          tags: ['integration-test', 'update', 'original'],
        });
        updateJobId = result.jobId;
      }),
      TIMEOUT
    );

    it(
      'should update job name',
      skipIfNoServer(async () => {
        const newName = generateJobName('renamed');
        const result = await scheduler.updateJob(updateJobId, {
          name: newName,
        });

        expect(result).toBeDefined();
        expect(result.name).toBe(newName);
      }),
      TIMEOUT
    );

    it(
      'should update job schedule',
      skipIfNoServer(async () => {
        const result = await scheduler.updateJob(updateJobId, {
          schedule: '0 */2 * * *', // Every 2 hours
        });

        expect(result).toBeDefined();
        expect(result.schedule).toBe('0 */2 * * *');
      }),
      TIMEOUT
    );

    it(
      'should update job payload',
      skipIfNoServer(async () => {
        const newPayload = { version: 2, updated: true };
        const result = await scheduler.updateJob(updateJobId, {
          payload: newPayload,
        });

        expect(result).toBeDefined();
        expect(result.payload).toEqual(newPayload);
      }),
      TIMEOUT
    );

    it(
      'should update job metadata',
      skipIfNoServer(async () => {
        const newMetadata = { key1: 'updated', key2: 'new' };
        const result = await scheduler.updateJob(updateJobId, {
          metadata: newMetadata,
        });

        expect(result).toBeDefined();
      }),
      TIMEOUT
    );

    it(
      'should update job retry settings',
      skipIfNoServer(async () => {
        const result = await scheduler.updateJob(updateJobId, {
          maxRetries: 5,
          retryDelayMs: 2000,
          timeoutMs: 60000,
        });

        expect(result).toBeDefined();
      }),
      TIMEOUT
    );

    it(
      'should update job tags',
      skipIfNoServer(async () => {
        const newTags = ['integration-test', 'update', 'modified'];
        const result = await scheduler.updateJob(updateJobId, {
          tags: newTags,
        });

        expect(result).toBeDefined();
      }),
      TIMEOUT
    );
  });
});
