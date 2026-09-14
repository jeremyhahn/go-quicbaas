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
import { orchestratorStore, tenantScope } from './orchestrator.svelte.ts';
import { authStore, onSessionScopeChange } from './auth.svelte.ts';

// ---------------------------------------------------------------------------
// Reactive state
// ---------------------------------------------------------------------------

let agents: AgentRegistration[] = $state([]);
let agentsLoading = $state(false);

/**
 * Session and tenant the cached agents belong to; '' before anything is
 * fetched. This cache is separate from orchestratorStore's, so it needs the
 * same protection: without it the fleet an operator was looking at stays on
 * screen for whoever signs in next (go-quicbaas#309).
 */
let scope = $state('');

/** Stable empty array for out-of-scope reads — see orchestrator.svelte.ts. */
const NO_AGENTS: AgentRegistration[] = [];

/** Whether the cached agents still belong to the session and tenant in effect. */
function inScope(): boolean {
  return scope === tenantScope();
}

/** Let go of a fleet belonging to a session or tenant no longer in effect. */
function releaseDepartedScope(): void {
  if (scope === '' || scope === tenantScope()) return;
  scope = '';
  agents = [];
}

onSessionScopeChange(() => {
  releaseDepartedScope();
  // Refetch for the scope now in effect, so a tenant switch fills the fleet
  // straight away instead of leaving it blank until the next 30-second tick.
  if (authStore.status === 'authenticated') void refreshAgents();
});

// ---------------------------------------------------------------------------
// Public store
// ---------------------------------------------------------------------------

export const agentStore = {
  get agents() { return inScope() ? agents : NO_AGENTS; },
  get agentsLoading() { return agentsLoading; },

  /** Derived: only agents currently online. */
  get onlineAgents() {
    const current = inScope() ? agents : NO_AGENTS;
    return (Array.isArray(current) ? current : []).filter(
      (a) => a.status === 'running' || a.status === 'idle' || a.status === 'registered',
    );
  },
};

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

/** Fetch agents for the active tenant. */
export async function refreshAgents(): Promise<void> {
  releaseDepartedScope();
  // Both pinned before any await, so the request and the decision to keep its
  // answer describe the same session and tenant.
  const asked = tenantScope();
  const tenantID = orchestratorStore.activeTenantID;
  agentsLoading = true;
  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.listAgents({ tenant_id: tenantID });
    // A late answer for a session that has been replaced is discarded rather
    // than committed under whoever is signed in now.
    if (asked !== tenantScope()) return;
    const raw = resp.agents ?? [];
    const seen = new Set<string>();
    agents = raw.filter((a) => {
      if (seen.has(a.id)) return false;
      seen.add(a.id);
      return true;
    });
    scope = asked;
  } catch {
    // Silently ignore transient errors; the next poll will retry.
  } finally {
    agentsLoading = false;
  }
}
