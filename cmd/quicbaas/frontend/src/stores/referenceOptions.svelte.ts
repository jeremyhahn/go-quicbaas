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
 * The names behind the ids a filter picks: the tenant's projects, its fleet
 * agents, and the backends the daemon has registered.
 *
 * Four lists want the same three vocabularies — Tasks, Jobs, Sessions and
 * Workflows all offer "narrow to a project" — and four copies of the same
 * fetch is four chances to spell the fallback differently. This reads each one
 * ONCE PER TENANT and hands out `FilterOption`s, labelled by name so a chip
 * reads "go-truststrap" rather than a uuid.
 *
 * A READ THAT FAILS COSTS THE LABELS, NOT THE FILTER. Every fetch is caught:
 * an account that may list jobs but not projects still gets a working Jobs
 * page, with a project picker that offers whatever the server counted (see
 * fieldOptions in lib/filters.ts) instead of an empty control or an error.
 */

import type { FilterOption } from '../lib/filters.ts';
import { getSDKClient } from './sdkClient.ts';

/** How many records the pickers offer. The fleet and the project list are
 *  operator-scale; a tenant past this has a search box for the rest. */
const PICKER_LIMIT = 100;

let _projects = $state<FilterOption[]>([]);
let _agents = $state<FilterOption[]>([]);
let _backends = $state<FilterOption[]>([]);
/** The tenant the three lists describe, so a switch re-reads exactly once. */
let loadedTenant = '';
let loading: Promise<void> | null = null;

export const referenceOptions = {
  get projects(): FilterOption[] {
    return _projects;
  },
  get agents(): FilterOption[] {
    return _agents;
  },
  get backends(): FilterOption[] {
    return _backends;
  },
};

/**
 * loadReferenceOptions reads the three vocabularies for a tenant, once.
 *
 * Concurrent callers — four views mounting behind one tenant switch — share
 * the one in-flight read rather than racing three fetches each.
 */
export async function loadReferenceOptions(tenantID: string): Promise<void> {
  if (tenantID === '') return;
  if (tenantID === loadedTenant) {
    if (loading !== null) await loading;
    return;
  }
  loadedTenant = tenantID;
  loading = readAll(tenantID);
  try {
    await loading;
  } finally {
    loading = null;
  }
}

/** forgetReferenceOptions drops the cache — the hook a test or a logout uses. */
export function forgetReferenceOptions(): void {
  loadedTenant = '';
  loading = null;
  _projects = [];
  _agents = [];
  _backends = [];
}

async function readAll(tenantID: string): Promise<void> {
  await Promise.all([readProjects(tenantID), readAgents(tenantID), readBackends()]);
}

async function readProjects(tenantID: string): Promise<void> {
  try {
    const client = await getSDKClient();
    const page = await client.projects.listProjectsPage(tenantID, { limit: PICKER_LIMIT });
    _projects = page.items
      .map((project) => ({ value: project.id, label: project.name || project.id }))
      .sort(byLabel);
  } catch {
    _projects = [];
  }
}

async function readAgents(tenantID: string): Promise<void> {
  try {
    const client = await getSDKClient();
    const page = await client.orchestrator.listAgentsPage(tenantID, { limit: PICKER_LIMIT });
    _agents = page.items
      .map((agent) => ({ value: agent.id, label: agent.name || agent.id }))
      .sort(byLabel);
  } catch {
    _agents = [];
  }
}

async function readBackends(): Promise<void> {
  try {
    const client = await getSDKClient();
    const backends = await client.orchestrator.listBackends();
    _backends = (backends ?? [])
      .map((backend) => ({ value: backend.name, label: backend.name }))
      .sort(byLabel);
  } catch {
    _backends = [];
  }
}

function byLabel(a: FilterOption, b: FilterOption): number {
  return a.label.localeCompare(b.label, undefined, { sensitivity: 'base' });
}
