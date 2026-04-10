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
 * Job event subscription store.
 *
 * Manages the lifecycle of a server-sent event (SSE) stream for a specific
 * job, routing received events into the orchestratorStore event log.
 */

import { appendEvent } from './orchestrator.svelte.ts';
import type { JobEvent } from './orchestrator.svelte.ts';

let activeJobID = $state('');
let subscriptionActive = $state(false);
let eventSource: EventSource | null = null;

export const eventSubStore = {
  get activeJobID() { return activeJobID; },
  get subscriptionActive() { return subscriptionActive; },
};

/**
 * Starts an SSE stream for the given job. Stops any existing stream first.
 */
export function startEventStream(tenantID: string, jobID: string): void {
  stopEventStream();

  activeJobID = jobID;
  subscriptionActive = true;

  const url = `/v1/tenants/${encodeURIComponent(tenantID)}/jobs/${encodeURIComponent(jobID)}/events`;
  const es = new EventSource(url);

  es.onmessage = (evt) => {
    try {
      const event = JSON.parse(evt.data as string) as JobEvent;
      appendEvent(event);
    } catch {
      // Ignore malformed events.
    }
  };

  es.onerror = () => {
    subscriptionActive = false;
  };

  eventSource = es;
}

/**
 * Stops the active event stream and cleans up resources.
 */
export function stopEventStream(): void {
  if (eventSource !== null) {
    eventSource.close();
    eventSource = null;
  }
  activeJobID = '';
  subscriptionActive = false;
}
