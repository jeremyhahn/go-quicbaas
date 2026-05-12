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
import { getSDKClient } from './sdkClient.ts';

let activeJobID = $state('');
let subscriptionActive = $state(false);
// Cleanup function returned by client.orchestrator.subscribeJobEvents().
let unsubscribe: (() => void) | null = null;

export const eventSubStore = {
  get activeJobID() { return activeJobID; },
  get subscriptionActive() { return subscriptionActive; },
};

/**
 * Starts an SSE stream for the given job via the SDK client.
 * Stops any existing stream first.
 */
export function startEventStream(tenantID: string, jobID: string): void {
  stopEventStream();

  activeJobID = jobID;
  subscriptionActive = true;

  void getSDKClient().then((client) => {
    unsubscribe = client.orchestrator.subscribeJobEvents(
      tenantID,
      jobID,
      (event: JobEvent) => {
        appendEvent(event);
      },
    );
  }).catch(() => {
    subscriptionActive = false;
  });
}

/**
 * Stops the active event stream and cleans up resources.
 */
export function stopEventStream(): void {
  if (unsubscribe !== null) {
    unsubscribe();
    unsubscribe = null;
  }
  activeJobID = '';
  subscriptionActive = false;
}
