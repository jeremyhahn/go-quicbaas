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

/**
 * Job event subscriptions.
 *
 * Every server-sent event stream for a job is opened here and routed into the
 * orchestrator store: `appendEvent` for the event log the Live Events view and
 * the dashboard read, `applyJobEvent` for the job record itself, so a state
 * change shows immediately rather than at the next 30 s poll.
 *
 * Two callers, one set of streams:
 *
 *  - a Job detail view PINS the job it is showing (`startEventStream`), and
 *    releases it on unmount (`stopEventStream`);
 *  - the shell reconciles streams for the jobs that are still running
 *    (`syncActiveJobStreams`), capped so a busy tenant cannot open an
 *    unbounded number of connections.
 *
 * A pinned job is never dropped by a sync, and a job the sync wants is not
 * closed when the detail view unpins it. Both funnel through `ensureStream`,
 * so a job watched by both holds exactly one connection.
 *
 * Callers are effects, so nothing here may make its caller depend on state
 * it writes. Every exported entry point runs its body under `untrack`: the
 * detail view's effect used to read `activeJobID` through startEventStream,
 * write it in the same call, and reset it from its own cleanup — each run
 * scheduled the next until Svelte aborted with effect_update_depth_exceeded
 * and the app stopped responding to navigation.
 *
 * A stream the server refuses or cannot serve (a 403, a non-SSE answer, a
 * dropped connection the browser gives up on) is not reopened in a loop. It
 * settles as FAILED: the entry stays in the map as a tombstone, so neither a
 * sync nor a re-pin opens it again, and `eventSubStore.liveUpdatesUnavailable`
 * tells the view to say so. `retryEventStream` is the operator's way back.
 */

import { untrack } from 'svelte';
import { appendEvent, applyJobEvent, isTerminalJobState } from './orchestrator.svelte.ts';
import type { JobEvent, JobStatus } from './orchestrator.svelte.ts';
import { getSDKClient } from './sdkClient.ts';

/**
 * The most streams a sync will hold open at once. Each is an EventSource
 * against the daemon, and browsers cap concurrent connections per origin —
 * a tenant running fifty jobs must not starve the rest of the UI of sockets.
 * The detail view's pinned stream is additional to this budget: it is the one
 * the operator is actually looking at.
 */
export const MAX_ACTIVE_JOB_STREAMS = 10;

/** One open (or opening) subscription. */
interface Stream {
  /** Closes the SDK subscription; null while the client promise is in flight. */
  unsubscribe: (() => void) | null;
  /** Set when the stream was closed before its client promise resolved. */
  cancelled: boolean;
  /** Set when the stream ended in error; the entry is kept as a tombstone. */
  failed: boolean;
}

const streams = new Map<string, Stream>();

/** Job ids the shell's last sync asked for. */
let syncedJobIDs = new Set<string>();

/** The job a detail view is showing, '' when none. */
let activeJobID = $state('');
let subscriptionCount = $state(0);
/** Jobs whose stream ended in error and has not been retried. */
let failedJobIDs = $state<readonly string[]>([]);

export const eventSubStore = {
  /** The job the detail view pinned, '' when no detail view is open. */
  get activeJobID() { return activeJobID; },
  /** True while any job stream is open. */
  get subscriptionActive() { return subscriptionCount > 0; },
  /** How many job streams are open. */
  get subscriptionCount() { return subscriptionCount; },
  /** True when a job's live stream failed and the page is no longer updating on its own. */
  liveUpdatesUnavailable(jobID: string): boolean { return failedJobIDs.includes(jobID); },
};

function markFailed(jobID: string): void {
  if (!failedJobIDs.includes(jobID)) failedJobIDs = [...failedJobIDs, jobID];
}

function clearFailed(jobID: string): void {
  if (failedJobIDs.includes(jobID)) failedJobIDs = failedJobIDs.filter((id) => id !== jobID);
}

/**
 * ensureStream opens a stream for one job unless one is already open. The
 * entry is recorded before the client promise resolves so two calls in the
 * same tick cannot open two connections to the same job.
 */
function ensureStream(tenantID: string, jobID: string): void {
  if (jobID === '' || streams.has(jobID)) return;

  const stream: Stream = { unsubscribe: null, cancelled: false, failed: false };
  streams.set(jobID, stream);
  subscriptionCount = streams.size;

  void getSDKClient().then((client) => {
    const unsubscribe = client.orchestrator.subscribeJobEvents(
      tenantID,
      jobID,
      (event: JobEvent) => {
        appendEvent(event);
        applyJobEvent(event);
      },
      () => { failStream(jobID, stream); },
    );
    // The stream was closed while the client was being built; honour that
    // rather than leaving a subscription nobody holds a handle to.
    if (stream.cancelled) {
      unsubscribe();
      return;
    }
    stream.unsubscribe = unsubscribe;
  }).catch(() => {
    failStream(jobID, stream);
  });
}

/**
 * failStream settles a stream that ended in error: the connection is closed,
 * the entry stays as a tombstone so it is not reopened, and the job is
 * reported as having no live updates. A stream that was already closed or
 * replaced is left alone.
 */
function failStream(jobID: string, stream: Stream): void {
  if (streams.get(jobID) !== stream || stream.cancelled || stream.failed) return;
  stream.failed = true;
  if (stream.unsubscribe !== null) stream.unsubscribe();
  stream.unsubscribe = null;
  markFailed(jobID);
}

/** closeStream tears one stream down, whether or not it finished opening. */
function closeStream(jobID: string): void {
  const stream = streams.get(jobID);
  if (stream === undefined) return;
  stream.cancelled = true;
  if (stream.unsubscribe !== null) stream.unsubscribe();
  streams.delete(jobID);
  syncedJobIDs.delete(jobID);
  subscriptionCount = streams.size;
  clearFailed(jobID);
}

/**
 * startEventStream opens (or adopts) the stream for the job a detail view is
 * showing and pins it, so the shell's reconciliation cannot close it while
 * the view is open. Any previously pinned job is released first.
 */
export function startEventStream(tenantID: string, jobID: string): void {
  untrack(() => {
    if (activeJobID === jobID) return;
    releasePin();
    activeJobID = jobID;
    ensureStream(tenantID, jobID);
  });
}

/**
 * stopEventStream releases the pinned job. Its stream stays open only if the
 * shell's last sync also asked for it.
 */
export function stopEventStream(): void {
  untrack(releasePin);
}

function releasePin(): void {
  const pinned = activeJobID;
  if (pinned === '') return;
  activeJobID = '';
  if (!syncedJobIDs.has(pinned)) closeStream(pinned);
}

/**
 * retryEventStream reopens a job's stream after it failed. It is the
 * operator's explicit retry; nothing reopens a failed stream on its own.
 */
export function retryEventStream(tenantID: string, jobID: string): void {
  untrack(() => {
    const stream = streams.get(jobID);
    if (stream === undefined || !stream.failed) return;
    streams.delete(jobID);
    clearFailed(jobID);
    ensureStream(tenantID, jobID);
  });
}

/**
 * syncActiveJobStreams reconciles the open streams against the jobs that are
 * still running: it opens one for each of the first MAX_ACTIVE_JOB_STREAMS
 * non-terminal jobs and closes the streams for jobs that finished or left the
 * list. The pinned job is never closed here.
 *
 * Call it whenever the job list changes. It is idempotent: an unchanged list
 * opens and closes nothing.
 */
export function syncActiveJobStreams(tenantID: string, jobs: JobStatus[] | null | undefined): void {
  // The shell calls this from an effect whose dependency must be the job
  // list alone — the job ids are read here, under untrack, so the effect
  // does not also re-run on the stream bookkeeping this writes.
  const list = Array.isArray(jobs) ? jobs.map((job) => ({ job_id: job?.job_id, state: job?.state })) : [];
  untrack(() => { syncStreams(tenantID, list); });
}

function syncStreams(tenantID: string, jobs: ReadonlyArray<{ job_id?: string; state?: JobStatus['state'] }>): void {
  const wanted = new Set<string>();
  for (const job of jobs) {
    if (wanted.size >= MAX_ACTIVE_JOB_STREAMS) break;
    if (typeof job.job_id !== 'string' || job.job_id === '') continue;
    if (isTerminalJobState(job.state as JobStatus['state'])) continue;
    wanted.add(job.job_id);
  }

  for (const jobID of [...streams.keys()]) {
    if (wanted.has(jobID) || jobID === activeJobID) continue;
    closeStream(jobID);
  }

  syncedJobIDs = wanted;
  for (const jobID of wanted) ensureStream(tenantID, jobID);
}

/** stopAllJobStreams closes every stream, pinned or synced. */
export function stopAllJobStreams(): void {
  untrack(() => {
    for (const jobID of [...streams.keys()]) closeStream(jobID);
    syncedJobIDs = new Set<string>();
    activeJobID = '';
  });
}
