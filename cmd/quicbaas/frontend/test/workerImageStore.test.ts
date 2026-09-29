/**
 * workerImageStore.test.ts — "Check for updates now" requests a pass and
 * follows it to its end (go-quicbaas#1021).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { WorkerImageStatus } from '@quicbaas/sdk';

const { getWorkerImageStatus, refreshWorkerImage } = vi.hoisted(() => ({
  getWorkerImageStatus: vi.fn(),
  refreshWorkerImage: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({ orchestrator: { getWorkerImageStatus, refreshWorkerImage } }),
}));

import { workerImageStore, loadWorkerImageStatus, checkForUpdates, watchWorkerImageStatus } from '../src/stores/workerImage.svelte.ts';

function finished(outcome: 'current' | 'promoted' | 'failed'): WorkerImageStatus {
  const now = new Date(Date.now() + 1000).toISOString();
  return {
    serving: true, running: false, queued: false,
    last: { trigger: 'manual', outcome, started_at: now, finished_at: now, latest: { claude_code: '2.1.280', codex: '0.156.0' } },
  };
}

beforeEach(() => {
  getWorkerImageStatus.mockReset();
  refreshWorkerImage.mockReset();
  workerImageStore.status = null;
  workerImageStore.error = '';
  workerImageStore.checkError = '';
  workerImageStore.checking = false;
});

describe('loadWorkerImageStatus', () => {
  it('holds the status it read', async () => {
    getWorkerImageStatus.mockResolvedValue(finished('current'));
    await loadWorkerImageStatus();
    expect(workerImageStore.status?.last?.outcome).toBe('current');
    expect(workerImageStore.error).toBe('');
    expect(workerImageStore.loading).toBe(false);
  });

  it('keeps the reason a read failed', async () => {
    getWorkerImageStatus.mockRejectedValue(new Error('forbidden'));
    await loadWorkerImageStatus();
    expect(workerImageStore.error).toBe('forbidden');
  });
});

describe('checkForUpdates', () => {
  it('requests a pass and polls until that pass has finished', async () => {
    refreshWorkerImage.mockResolvedValue({ serving: true, running: false, queued: true });
    getWorkerImageStatus
      .mockResolvedValueOnce({ serving: true, running: true, queued: false })
      .mockResolvedValueOnce(finished('promoted'));

    await checkForUpdates({ pollMs: 1, timeoutMs: 10_000 });

    expect(refreshWorkerImage).toHaveBeenCalledTimes(1);
    expect(getWorkerImageStatus).toHaveBeenCalledTimes(2);
    expect(workerImageStore.status?.last?.outcome).toBe('promoted');
    expect(workerImageStore.checking).toBe(false);
    expect(workerImageStore.checkError).toBe('');
  });

  it('says a refused request in words and does not poll', async () => {
    refreshWorkerImage.mockRejectedValue(Object.assign(new Error('conflict'), { status: 409 }));
    await checkForUpdates({ pollMs: 1 });
    expect(workerImageStore.checkError).toMatch(/already running or queued/);
    expect(getWorkerImageStatus).not.toHaveBeenCalled();
    expect(workerImageStore.checking).toBe(false);
  });

  it('stops watching, and says so, when its patience runs out', async () => {
    refreshWorkerImage.mockResolvedValue({ serving: true, running: false, queued: true });
    getWorkerImageStatus.mockResolvedValue({ serving: true, running: true, queued: false });
    await checkForUpdates({ pollMs: 1, timeoutMs: 5 });
    expect(workerImageStore.checkError).toMatch(/still running on the daemon/);
    expect(workerImageStore.checking).toBe(false);
  });

  it('reports a status that can no longer be read', async () => {
    refreshWorkerImage.mockResolvedValue({ serving: true, running: false, queued: true });
    getWorkerImageStatus.mockRejectedValue(new Error('connection lost'));
    await checkForUpdates({ pollMs: 1 });
    expect(workerImageStore.checkError).toBe('connection lost');
  });

  it('does not start a second follow while one is running', async () => {
    workerImageStore.checking = true;
    await checkForUpdates({ pollMs: 1 });
    expect(refreshWorkerImage).not.toHaveBeenCalled();
  });
});

describe('watchWorkerImageStatus', () => {
  it('follows a running pass at the busy cadence until it finishes, then idles', async () => {
    getWorkerImageStatus
      .mockResolvedValueOnce({ serving: true, running: true, queued: false })
      .mockResolvedValueOnce({ serving: true, running: false, queued: true })
      .mockResolvedValue(finished('promoted'));

    const stop = watchWorkerImageStatus({ pollMs: 1, idlePollMs: 60_000 });
    try {
      await vi.waitFor(() => expect(workerImageStore.status?.last?.outcome).toBe('promoted'));
      const calls = getWorkerImageStatus.mock.calls.length;
      // Idle now: the next read is a minute away.
      await new Promise((resolve) => setTimeout(resolve, 20));
      expect(getWorkerImageStatus.mock.calls.length).toBe(calls);
    } finally {
      stop();
    }
  });

  it('leaves the polling to a check it is already following', async () => {
    getWorkerImageStatus.mockResolvedValue({ serving: true, running: true, queued: false });
    workerImageStore.checking = true;
    const stop = watchWorkerImageStatus({ pollMs: 1, idlePollMs: 1 });
    try {
      await vi.waitFor(() => expect(getWorkerImageStatus).toHaveBeenCalledTimes(1));
      await new Promise((resolve) => setTimeout(resolve, 20));
      // Only the first read: every later tick deferred to the followed check.
      expect(getWorkerImageStatus).toHaveBeenCalledTimes(1);
    } finally {
      stop();
      workerImageStore.checking = false;
    }
  });

  it('reads nothing after it is stopped', async () => {
    getWorkerImageStatus.mockResolvedValue({ serving: true, running: true, queued: false });
    const stop = watchWorkerImageStatus({ pollMs: 1, idlePollMs: 1 });
    await vi.waitFor(() => expect(getWorkerImageStatus.mock.calls.length).toBeGreaterThanOrEqual(2));
    stop();
    await new Promise((resolve) => setTimeout(resolve, 10));
    const calls = getWorkerImageStatus.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(getWorkerImageStatus.mock.calls.length).toBe(calls);
  });
});
