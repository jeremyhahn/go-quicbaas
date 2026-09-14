/**
 * taskEmptyStates.test.ts
 *
 * The sentences an EMPTY panel on the task page is allowed to say.
 *
 * These are not cosmetic. An empty Jobs panel is a claim about the task, and
 * the page used to make one claim for all of them: "Dispatch requested; no
 * agent has matched this task yet." go-quicbaas#822 measured it rendered on a
 * task that had dispatched three times, run three jobs and ended `failed`.
 * Each case below is one fact that must be distinguishable from the others.
 */

import { describe, it, expect } from 'vitest';
import {
  taskHasSettled,
  taskJobsEmptyMessage,
  taskJobsEmptyState,
  taskPullRequestEmptyMessage,
  taskPullRequestEmptyState,
} from '../src/lib/tasks.ts';

describe('taskHasSettled', () => {
  it('is true for every status no job will advance on its own', () => {
    for (const status of ['completed', 'failed', 'blocked', 'cancelled']) {
      expect(taskHasSettled({ status })).toBe(true);
    }
  });

  it('is false while the task can still move', () => {
    for (const status of ['pending', 'queued', 'in_progress', '']) {
      expect(taskHasSettled({ status })).toBe(false);
    }
  });
});

describe('taskJobsEmptyState', () => {
  it('reports records-unavailable when attempts ran and no job is held', () => {
    expect(taskJobsEmptyState({ status: 'failed', attempts: 3 }, 0, false)).toBe('records-unavailable');
  });

  it('reports records-unavailable when a job id is on the task but no record came back', () => {
    expect(taskJobsEmptyState({ status: 'in_progress', attempts: 0, spawned_job_id: 'job-1' }, 0, false))
      .toBe('records-unavailable');
  });

  it('reports awaiting-dispatch for a live task that has never run', () => {
    expect(taskJobsEmptyState({ status: 'pending', attempts: 0, auto_spawn: true }, 0, false))
      .toBe('awaiting-dispatch');
  });

  it('reports never-dispatched for a settled task that never ran', () => {
    expect(taskJobsEmptyState({ status: 'failed', attempts: 0 }, 0, false)).toBe('never-dispatched');
  });

  it('reports loading while the read is in flight, whatever the task says', () => {
    expect(taskJobsEmptyState({ status: 'failed', attempts: 3 }, 0, true)).toBe('loading');
  });

  it('treats an absent attempt count as no attempts', () => {
    expect(taskJobsEmptyState({ status: 'pending' }, 0, false)).toBe('awaiting-dispatch');
  });
});

describe('taskJobsEmptyMessage', () => {
  // THE MEASURED DEFECT, pinned as a negative: this exact sentence must never
  // appear for a task that has dispatched.
  it('never claims a task with charged attempts is waiting for an agent', () => {
    const message = taskJobsEmptyMessage({ status: 'failed', attempts: 3, auto_spawn: true }, 0, false);
    expect(message).not.toContain('no agent has matched this task yet');
    expect(message).toContain('3 attempts ran for this task');
    expect(message).toContain('no job record for them could be read');
  });

  it('uses the singular for exactly one attempt', () => {
    expect(taskJobsEmptyMessage({ status: 'failed', attempts: 1 }, 0, false)).toContain('1 attempt ran');
  });

  it('says a dispatch was requested only when one actually was', () => {
    expect(taskJobsEmptyMessage({ status: 'pending', attempts: 0, auto_spawn: true }, 0, false))
      .toBe('Dispatch requested; no agent has matched this task yet.');
    expect(taskJobsEmptyMessage({ status: 'pending', attempts: 0 }, 0, false))
      .toBe('No job has been dispatched for this task.');
  });

  it('names the status a task ended in when it never dispatched', () => {
    expect(taskJobsEmptyMessage({ status: 'blocked', attempts: 0 }, 0, false))
      .toBe('This task ended blocked without a job ever being dispatched.');
  });

  it('says it is still reading rather than asserting anything', () => {
    expect(taskJobsEmptyMessage({ status: 'failed', attempts: 3 }, 0, true))
      .toBe('Loading this task’s jobs…');
  });
});

describe('taskPullRequestEmptyState', () => {
  it('reports nothing-ran when no attempt was charged', () => {
    expect(taskPullRequestEmptyState({ status: 'pending', attempts: 0 }, 0)).toBe('nothing-ran');
  });

  it('reports settled-without-publishing when the task ended having run', () => {
    expect(taskPullRequestEmptyState({ status: 'failed', attempts: 2 }, 0))
      .toBe('settled-without-publishing');
  });

  it('reports not-yet-published while the task is still running', () => {
    expect(taskPullRequestEmptyState({ status: 'in_progress', attempts: 1 }, 0))
      .toBe('not-yet-published');
  });
});

describe('taskPullRequestEmptyMessage', () => {
  it('distinguishes failing before publish from completing without one', () => {
    expect(taskPullRequestEmptyMessage({ status: 'failed', attempts: 3 }, 0))
      .toBe('This task ended failed before publishing a pull request (3 attempts ran).');
    expect(taskPullRequestEmptyMessage({ status: 'completed', attempts: 1 }, 0))
      .toBe('This task completed without publishing a pull request (1 attempt ran).');
  });

  it('does not claim anything was attempted when nothing was', () => {
    expect(taskPullRequestEmptyMessage({ status: 'pending', attempts: 0 }, 0))
      .toBe('No attempt has run, so nothing has been published.');
  });

  it('says not yet for a task still in flight', () => {
    expect(taskPullRequestEmptyMessage({ status: 'in_progress', attempts: 2 }, 0))
      .toBe('No pull request has been published yet.');
  });
});
