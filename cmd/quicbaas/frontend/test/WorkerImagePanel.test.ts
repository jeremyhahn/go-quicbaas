/**
 * WorkerImagePanel.test.ts — the worker image panel on the Agent Fleet page
 * (go-quicbaas#1021).
 *
 * Verifies the panel shows each bound image with its CLI versions, the npm
 * registry's latest from the last check, and that check's time and outcome;
 * that "Check for updates now" requests a pass and shows its outcome; that
 * the button is unavailable while a pass runs or where the loop does not; and
 * that labels are sentence case.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { WorkerImageStatus } from '@quicbaas/sdk';

const { getWorkerImageStatus, refreshWorkerImage } = vi.hoisted(() => ({
  getWorkerImageStatus: vi.fn(),
  refreshWorkerImage: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({ orchestrator: { getWorkerImageStatus, refreshWorkerImage } }),
}));

import WorkerImagePanel from '../src/components/orchestrator/WorkerImagePanel.svelte';
import { workerImageStore } from '../src/stores/workerImage.svelte.ts';

const lastPass: WorkerImageStatus = {
  serving: true,
  running: false,
  queued: false,
  last: {
    trigger: 'schedule',
    outcome: 'partial',
    started_at: '2026-09-22T09:40:00Z',
    finished_at: '2026-09-22T10:00:00Z',
    latest: { claude_code: '2.1.280', codex: '0.156.0' },
    bound: [
      { image: 'quicbaas-software-factory-worker:0.10.0-alpha', agents: { claude_code: '2.1.278', codex: '0.150.0' }, uses: 2, stale: true },
      { image: 'reg:3000/o/w:0.11.0-alpha-cc2.1.280-cx0.156.0@sha256:abc', agents: { claude_code: '2.1.280', codex: '0.156.0' }, uses: 1, stale: false },
    ],
    reference: 'reg:3000/o/w:0.11.0-alpha-cc2.1.280-cx0.156.0@sha256:abc',
    error: '1 of 2 bindings could not be rebound',
  },
};

beforeEach(() => {
  getWorkerImageStatus.mockReset();
  refreshWorkerImage.mockReset();
  workerImageStore.status = null;
  workerImageStore.error = '';
  workerImageStore.checkError = '';
  workerImageStore.checking = false;
});

describe('WorkerImagePanel', () => {
  it('shows every bound image with its CLI versions, npm latest and the last check', async () => {
    getWorkerImageStatus.mockResolvedValue(lastPass);
    render(WorkerImagePanel);

    const bound = await screen.findByTestId('worker-image-bound');
    expect(bound.textContent).toContain('quicbaas-software-factory-worker:0.10.0-alpha');
    expect(bound.textContent).toContain('Claude Code 2.1.278 · Codex 0.150.0');
    expect(bound.textContent).toContain('Behind');
    expect(bound.textContent).toContain('Current');
    expect(screen.getByTestId('worker-image-latest').textContent).toBe('Claude Code 2.1.280 · Codex 0.156.0');
    expect(screen.getByTestId('worker-image-last').textContent).toContain('Partly updated');
    expect(screen.getByTestId('worker-image-last').textContent).toContain('daily check');
    expect(screen.getByTestId('worker-image-pass-error').textContent).toContain('could not be rebound');
  });

  it('requests a check and shows the outcome of that pass', async () => {
    getWorkerImageStatus.mockResolvedValueOnce(lastPass);
    const started = new Date(Date.now() + 1000).toISOString();
    refreshWorkerImage.mockResolvedValue({ ...lastPass, queued: true });
    getWorkerImageStatus.mockResolvedValue({
      ...lastPass,
      last: { ...lastPass.last!, trigger: 'manual', outcome: 'promoted', started_at: started, finished_at: started, error: undefined },
    });
    render(WorkerImagePanel, { props: { pollMs: 1 } });

    const button = await screen.findByRole('button', { name: 'Check for updates now' });
    await waitFor(() => expect(button.hasAttribute('disabled')).toBe(false));
    await fireEvent.click(button);

    expect(refreshWorkerImage).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.getByTestId('worker-image-last').textContent).toContain('Updated'));
    expect(screen.getByTestId('worker-image-last').textContent).toContain('requested');
  });

  it('offers no check while a pass is running', async () => {
    getWorkerImageStatus.mockResolvedValue({ ...lastPass, running: true, running_since: '2026-09-22T10:05:00Z' });
    render(WorkerImagePanel);
    const button = await screen.findByTestId('worker-image-check');
    await waitFor(() => expect(button.textContent).toContain('Checking'));
    expect(button.hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Checking for updates')).toBeTruthy();
  });

  it('follows a pass already running when it opened to its outcome, without the button', async () => {
    getWorkerImageStatus
      .mockResolvedValueOnce({ ...lastPass, running: true, running_since: '2026-09-22T10:05:00Z' })
      .mockResolvedValueOnce({ ...lastPass, running: true, running_since: '2026-09-22T10:05:00Z' })
      .mockResolvedValue({
        ...lastPass,
        last: { ...lastPass.last!, outcome: 'promoted', error: undefined },
      });
    render(WorkerImagePanel, { props: { pollMs: 1, idlePollMs: 60_000 } });

    const button = await screen.findByTestId('worker-image-check');
    await waitFor(() => expect(button.textContent).toContain('Checking'));
    await waitFor(() => expect(screen.getByTestId('worker-image-last').textContent).toContain('Updated'));
    await waitFor(() => expect(button.hasAttribute('disabled')).toBe(false));
    expect(button.textContent).toContain('Check for updates now');
    expect(refreshWorkerImage).not.toHaveBeenCalled();
  });

  it('stops polling once unmounted', async () => {
    getWorkerImageStatus.mockResolvedValue({ ...lastPass, running: true, running_since: '2026-09-22T10:05:00Z' });
    const { unmount } = render(WorkerImagePanel, { props: { pollMs: 1, idlePollMs: 1 } });
    await waitFor(() => expect(getWorkerImageStatus.mock.calls.length).toBeGreaterThanOrEqual(2));
    unmount();
    // Let any read already in flight settle, then prove nothing new starts.
    await new Promise((resolve) => setTimeout(resolve, 10));
    const settled = getWorkerImageStatus.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(getWorkerImageStatus.mock.calls.length).toBe(settled);
  });

  it('says so on a node without the refresh loop, and offers no check', async () => {
    getWorkerImageStatus.mockResolvedValue({ serving: false, running: false, queued: false });
    render(WorkerImagePanel);
    await screen.findByTestId('worker-image-not-serving');
    expect(screen.getByTestId('worker-image-check').hasAttribute('disabled')).toBe(true);
  });

  it('explains a node that has not checked yet', async () => {
    getWorkerImageStatus.mockResolvedValue({ serving: true, running: false, queued: false });
    render(WorkerImagePanel);
    await screen.findByTestId('worker-image-no-pass');
  });

  it('shows why the status could not be read', async () => {
    getWorkerImageStatus.mockRejectedValue(new Error('forbidden'));
    render(WorkerImagePanel);
    expect((await screen.findByTestId('worker-image-error')).textContent).toContain('forbidden');
  });

  it('uses sentence case, never all caps', async () => {
    getWorkerImageStatus.mockResolvedValue(lastPass);
    const { container } = render(WorkerImagePanel);
    await screen.findByTestId('worker-image-bound');
    expect(screen.getByRole('heading', { name: 'Worker image' })).toBeTruthy();
    expect(container.innerHTML).not.toMatch(/uppercase/);
  });
});
