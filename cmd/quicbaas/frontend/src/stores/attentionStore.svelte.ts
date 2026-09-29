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
 * Attention inbox state store — everything only an operator can clear.
 *
 * Backs the "Needs you" nav badge, the dashboard card, the environmental
 * alert banner and the inbox page itself. It ALWAYS holds the complete,
 * unfiltered set — every page of it — and nothing else: the inbox's
 * project/kind dropdowns are a view over these items (filterAttention),
 * never a fetch written into them, so choosing a filter can neither hide an
 * alert from the banner nor be overwritten by the next background refresh.
 *
 * Scoped exactly like fleetStore: a tenant switch or a sign-out releases the
 * cached items so the next signed-in operator does not see the previous
 * one's inbox for one render.
 */

import type { AttentionItem } from '@quicbaas/sdk';
import { getSDKClient } from './sdkClient.ts';
import { orchestratorStore, tenantScope } from './orchestrator.svelte.ts';
import { authStore, onSessionScopeChange } from './auth.svelte.ts';
import { showToast } from './toast.svelte.ts';
import {
  ATTENTION_PAGE_LIMIT,
  fetchAllPages,
  oldestAgeInstant,
  readDismissedAlerts,
  writeDismissedAlerts,
  visibleAlerts as visibleAlertsOf,
} from '../lib/attention.ts';

// ---------------------------------------------------------------------------
// Reactive state
// ---------------------------------------------------------------------------

let items: AttentionItem[] = $state([]);
let total = $state(0);
let facets: Record<string, Record<string, number>> = $state({});
let loading = $state(false);
let resolving: string | null = $state(null);

/** Session and tenant the cached items belong to; '' before anything is fetched. */
let scope = $state('');

/** Dismissed environmental alert ids, this browser session only. */
let dismissed: Set<string> = $state(readDismissedAlerts(safeSessionStorage()));

const NO_ITEMS: AttentionItem[] = [];
const NO_FACETS: Record<string, Record<string, number>> = {};

function safeSessionStorage(): Storage | undefined {
  try {
    return typeof window !== 'undefined' ? window.sessionStorage : undefined;
  } catch {
    return undefined;
  }
}

function inScope(): boolean {
  return scope === tenantScope();
}

function releaseDepartedScope(): void {
  if (scope === '' || scope === tenantScope()) return;
  scope = '';
  items = [];
  total = 0;
  facets = {};
}

onSessionScopeChange(() => {
  releaseDepartedScope();
  if (authStore.status === 'authenticated') void refreshAttention();
});

// ---------------------------------------------------------------------------
// Public store
// ---------------------------------------------------------------------------

export const attentionStore = {
  get items() { return inScope() ? items : NO_ITEMS; },
  get total() { return inScope() ? total : 0; },
  get facets() { return inScope() ? facets : NO_FACETS; },
  get loading() { return loading; },
  /** resolvingID is the task id a resolve action is currently in flight for. */
  get resolvingID() { return resolving; },

  /** oldestAgeInstant is what the dashboard card's age line reads. */
  get oldestAgeInstant() { return oldestAgeInstant(inScope() ? items : NO_ITEMS); },

  /** visibleAlerts is the environmental alerts not dismissed this session. */
  get visibleAlerts() { return visibleAlertsOf(inScope() ? items : NO_ITEMS, dismissed); },
};

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

/**
 * refreshAttention fetches the tenant's complete attention inbox, following
 * every page (see fetchAllPages), and replaces the cached set with it.
 */
export async function refreshAttention(): Promise<void> {
  releaseDepartedScope();
  const asked = tenantScope();
  const tenantID = orchestratorStore.activeTenantID;
  loading = true;
  try {
    const client = await getSDKClient();
    const set = await fetchAllPages<AttentionItem>((offset) =>
      client.orchestrator.listAttention(tenantID, { limit: ATTENTION_PAGE_LIMIT, offset }),
    );
    if (asked !== tenantScope()) return;
    items = set.items;
    total = set.total;
    facets = set.facets;
    scope = asked;
  } catch (err) {
    showToast('error', `Could not load the attention inbox: ${errorText(err)}`);
  } finally {
    loading = false;
  }
}

/**
 * resolveAttentionItem applies an operator's decision to one item and
 * refreshes the inbox — the item's forge issue and task state may both have
 * changed, and a partial local patch would drift from what the server
 * actually did.
 */
export async function resolveAttentionItem(
  taskID: string,
  action: string,
  answer?: string,
): Promise<boolean> {
  const tenantID = orchestratorStore.activeTenantID;
  resolving = taskID;
  try {
    const client = await getSDKClient();
    await client.orchestrator.resolveAttention({ tenant_id: tenantID, task_id: taskID, action: action as never, answer });
    showToast('success', 'Ruling posted.');
    await refreshAttention();
    return true;
  } catch (err) {
    showToast('error', `Could not resolve this item: ${errorText(err)}`);
    return false;
  } finally {
    resolving = null;
  }
}

/** dismissAlert hides one environmental alert for the rest of this session. */
export function dismissAlert(id: string): void {
  const next = new Set(dismissed);
  next.add(id);
  dismissed = next;
  writeDismissedAlerts(safeSessionStorage(), dismissed);
}

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
