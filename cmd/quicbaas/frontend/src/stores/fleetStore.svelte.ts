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
 * Fleet state store.
 *
 * Maintains a live list of registered fleet nodes. Background polling is
 * handled centrally by the orchestratorStore poller
 * (startOrchestratorPolling). This store provides a local SDK-typed view of
 * node data for components that need the richer FleetNode type from
 * the SDK package (as opposed to the plain orchestratorStore.fleetNodes shape).
 *
 * The activeTenantID is inherited from the orchestratorStore to keep tenant
 * context consistent across the app.
 */

import type { FleetNode } from '@quicbaas/sdk';
import { getSDKClient } from './sdkClient.ts';
import { orchestratorStore, tenantScope } from './orchestrator.svelte.ts';
import { authStore, onSessionScopeChange } from './auth.svelte.ts';

// ---------------------------------------------------------------------------
// Reactive state
// ---------------------------------------------------------------------------

let nodes: FleetNode[] = $state([]);
let nodesLoading = $state(false);

/**
 * Session and tenant the cached nodes belong to; '' before anything is
 * fetched. This cache is separate from orchestratorStore's, so it needs the
 * same protection: without it the fleet an operator was looking at stays on
 * screen for whoever signs in next (go-quicbaas#309).
 */
let scope = $state('');

/** Stable empty array for out-of-scope reads — see orchestrator.svelte.ts. */
const NO_NODES: FleetNode[] = [];

/** Whether the cached nodes still belong to the session and tenant in effect. */
function inScope(): boolean {
  return scope === tenantScope();
}

/** Let go of a fleet belonging to a session or tenant no longer in effect. */
function releaseDepartedScope(): void {
  if (scope === '' || scope === tenantScope()) return;
  scope = '';
  nodes = [];
}

onSessionScopeChange(() => {
  releaseDepartedScope();
  // Refetch for the scope now in effect, so a tenant switch fills the fleet
  // straight away instead of leaving it blank until the next 30-second tick.
  if (authStore.status === 'authenticated') void refreshFleet();
});

// ---------------------------------------------------------------------------
// Public store
// ---------------------------------------------------------------------------

export const fleetStore = {
  get nodes() { return inScope() ? nodes : NO_NODES; },
  get nodesLoading() { return nodesLoading; },

  /** Derived: only nodes currently online. */
  get onlineNodes() {
    const current = inScope() ? nodes : NO_NODES;
    return (Array.isArray(current) ? current : []).filter(
      (a) => a.status === 'running' || a.status === 'idle' || a.status === 'registered',
    );
  },
};

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

/** Fetch nodes for the active tenant. */
export async function refreshFleet(): Promise<void> {
  releaseDepartedScope();
  // Both pinned before any await, so the request and the decision to keep its
  // answer describe the same session and tenant.
  const asked = tenantScope();
  const tenantID = orchestratorStore.activeTenantID;
  nodesLoading = true;
  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.listFleetNodes({ tenant_id: tenantID });
    // A late answer for a session that has been replaced is discarded rather
    // than committed under whoever is signed in now.
    if (asked !== tenantScope()) return;
    const raw = resp.nodes ?? [];
    const seen = new Set<string>();
    nodes = raw.filter((a) => {
      if (seen.has(a.id)) return false;
      seen.add(a.id);
      return true;
    });
    scope = asked;
  } catch {
    // Silently ignore transient errors; the next poll will retry.
  } finally {
    nodesLoading = false;
  }
}
