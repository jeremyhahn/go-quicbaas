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
 * Agent fleet state store.
 *
 * Maintains a live list of registered agent daemons. Background polling is
 * handled centrally by the orchestratorStore poller
 * (startOrchestratorPolling). This store provides a local SDK-typed view of
 * agent data for components that need the richer AgentRegistration type from
 * the SDK package (as opposed to the plain orchestratorStore.agents shape).
 *
 * The activeTenantID is inherited from the orchestratorStore to keep tenant
 * context consistent across the app.
 */

import type { AgentRegistration } from '@quicbaas/sdk';
import { getSDKClient } from './sdkClient.ts';
import { orchestratorStore } from './orchestrator.svelte.ts';

// ---------------------------------------------------------------------------
// Reactive state
// ---------------------------------------------------------------------------

let agents: AgentRegistration[] = $state([]);
let agentsLoading = $state(false);

// ---------------------------------------------------------------------------
// Public store
// ---------------------------------------------------------------------------

export const agentStore = {
  get agents() { return agents; },
  get agentsLoading() { return agentsLoading; },

  /** Derived: only agents currently online. */
  get onlineAgents() {
    return (Array.isArray(agents) ? agents : []).filter(
      (a) => a.status === 'running' || a.status === 'idle' || a.status === 'registered',
    );
  },
};

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

/** Fetch agents for the active tenant. */
export async function refreshAgents(): Promise<void> {
  agentsLoading = true;
  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.listAgents({
      tenant_id: orchestratorStore.activeTenantID,
    });
    const raw = resp.agents ?? [];
    const seen = new Set<string>();
    agents = raw.filter((a) => {
      if (seen.has(a.id)) return false;
      seen.add(a.id);
      return true;
    });
  } catch {
    // Silently ignore transient errors; the next poll will retry.
  } finally {
    agentsLoading = false;
  }
}
