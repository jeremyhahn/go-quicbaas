// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

/**
 * Worker image refresh store (go-quicbaas#1021).
 *
 * Holds the refresh loop's status and drives "Check for updates now": the
 * request queues a pass on the daemon and returns at once, and this store
 * then polls the status until THAT pass has finished, so the panel can show
 * its progress and outcome. A pass builds and tests a whole image, so it is
 * polled for up to half an hour by default before the store says it is still
 * running and stops watching — the pass itself carries on on the daemon.
 */

import type { WorkerImageStatus } from '@quicbaas/sdk';
import { getSDKClient } from './sdkClient.ts';
import { requestedPassFinished, refreshErrorMessage } from '../lib/workerImage.ts';
import { errorText } from '../lib/tasks.ts';

export interface WorkerImageState {
  status: WorkerImageStatus | null;
  loading: boolean;
  /** Why the status could not be read; '' when it could. */
  error: string;
  /** A "Check for updates now" is being followed. */
  checking: boolean;
  /** Why the last check could not be followed to its end; '' otherwise. */
  checkError: string;
}

export const workerImageStore: WorkerImageState = $state({
  status: null,
  loading: false,
  error: '',
  checking: false,
  checkError: '',
});

/** The default cadence and patience of a followed check. */
export const DEFAULT_POLL_MS = 5_000;
/** How often a mounted panel re-reads the status while no pass is running. */
export const DEFAULT_IDLE_POLL_MS = 60_000;
export const DEFAULT_CHECK_TIMEOUT_MS = 30 * 60_000;

/** loadWorkerImageStatus reads the loop's status once. */
export async function loadWorkerImageStatus(): Promise<void> {
  workerImageStore.loading = true;
  try {
    const client = await getSDKClient();
    workerImageStore.status = await client.orchestrator.getWorkerImageStatus();
    workerImageStore.error = '';
  } catch (err) {
    workerImageStore.error = errorText(err);
  } finally {
    workerImageStore.loading = false;
  }
}

/**
 * watchWorkerImageStatus reads the status now and keeps re-reading it until
 * the returned stop function is called: every pollMs while a pass is running
 * or queued — whoever started it, this panel, another operator, the daily
 * schedule — and every idlePollMs otherwise, so a pass that begins elsewhere
 * shows up and one already running is followed to its outcome. While this
 * store is following a requested check it leaves the polling to that.
 */
export function watchWorkerImageStatus(
  opts: { pollMs?: number; idlePollMs?: number } = {},
): () => void {
  const pollMs = opts.pollMs ?? DEFAULT_POLL_MS;
  const idlePollMs = opts.idlePollMs ?? DEFAULT_IDLE_POLL_MS;
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const schedule = () => {
    if (stopped) return;
    const status = workerImageStore.status;
    const busy = workerImageStore.checking || !!status?.running || !!status?.queued;
    timer = setTimeout(tick, busy ? pollMs : idlePollMs);
  };
  const tick = async () => {
    if (stopped) return;
    if (!workerImageStore.checking) await loadWorkerImageStatus();
    schedule();
  };
  void loadWorkerImageStatus().then(schedule);
  return () => {
    stopped = true;
    if (timer !== undefined) clearTimeout(timer);
  };
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * checkForUpdates requests a pass and follows it until it finishes, the
 * patience runs out, or the status can no longer be read. It never throws:
 * every failure lands in checkError, where the panel shows it.
 */
export async function checkForUpdates(
  opts: { pollMs?: number; timeoutMs?: number } = {},
): Promise<void> {
  if (workerImageStore.checking) return;
  const pollMs = opts.pollMs ?? DEFAULT_POLL_MS;
  const timeoutMs = opts.timeoutMs ?? DEFAULT_CHECK_TIMEOUT_MS;
  workerImageStore.checking = true;
  workerImageStore.checkError = '';
  const requestedAt = Date.now();
  try {
    const client = await getSDKClient();
    try {
      workerImageStore.status = await client.orchestrator.refreshWorkerImage();
    } catch (err) {
      workerImageStore.checkError = refreshErrorMessage(err);
      return;
    }
    for (;;) {
      await delay(pollMs);
      const status = await client.orchestrator.getWorkerImageStatus();
      workerImageStore.status = status;
      if (requestedPassFinished(status, requestedAt)) return;
      if (Date.now() - requestedAt >= timeoutMs) {
        workerImageStore.checkError =
          'The check is still running on the daemon. Its result will appear here when it finishes.';
        return;
      }
    }
  } catch (err) {
    workerImageStore.checkError = errorText(err);
  } finally {
    workerImageStore.checking = false;
  }
}
