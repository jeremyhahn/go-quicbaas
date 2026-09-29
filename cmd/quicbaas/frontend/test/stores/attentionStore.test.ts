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
 * attentionStore — the complete inbox, every page of it, and nothing else.
 *
 * go-quicbaas#1020 review: the store fetched one page of 100, so the 101st
 * parked task was nowhere in the UI, and it wrote a FILTERED fetch into the
 * same items the badge, the dashboard and the alert banner read, so choosing
 * "parked tasks" hid every environmental alert. These drive the store through
 * the real SDK client against a stubbed server and assert the request and
 * what lands.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import { setAuthenticated, setUnauthenticated } from '../../src/stores/auth.svelte.ts';
import { resetSDKClient } from '../../src/stores/sdkClient.ts';
import { resetOrchestratorStore } from '../../src/stores/orchestrator.svelte.ts';
import {
  attentionStore,
  refreshAttention,
  resolveAttentionItem,
} from '../../src/stores/attentionStore.svelte.ts';

function sessionToken(tenantID: string): string {
  const b64url = (o: unknown): string =>
    btoa(JSON.stringify(o)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  const claims = {
    sub: 'usr_1',
    tenant_id: tenantID,
    roles: ['operator'],
    sid: 'sid_1',
    exp: Math.floor(Date.now() / 1000) + 3600,
  };
  return `${b64url({ alg: 'none' })}.${b64url(claims)}.sig`;
}

/** The complete inbox the stubbed server holds. */
let inbox: Record<string, unknown>[] = [];

/** Every attention listing URL the SDK issued. */
let listings: URL[] = [];

/** Every resolve the SDK posted, as task ids. */
let resolved: string[] = [];

function parked(n: number): Record<string, unknown> {
  return { id: `task-${n}`, kind: 'parked_task', project_id: 'p1', subject: `task ${n}`, actions: ['readmit'] };
}

const ALERT = { id: 'agent_offline:a1', kind: 'agent_offline', subject: 'agent a1 is offline', severity: 'critical' };

function stubFetch(): void {
  const spy = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const raw = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const url = new URL(raw, window.location.origin);
    const json = (body: unknown): Response =>
      new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
    if (url.pathname.endsWith('/resolve') && init?.method === 'POST') {
      const taskID = decodeURIComponent(url.pathname.split('/').slice(-2)[0]!);
      resolved.push(taskID);
      inbox = inbox.filter((item) => item.id !== taskID);
      return json({ id: taskID, kind: 'parked_task', subject: '' });
    }
    if (url.pathname.endsWith('/orchestrator/attention')) {
      listings.push(url);
      const limit = Number(url.searchParams.get('limit') ?? '25');
      const offset = Number(url.searchParams.get('offset') ?? '0');
      const page = inbox.slice(offset, offset + limit);
      const next = offset + limit < inbox.length ? offset + limit : undefined;
      const kinds: Record<string, number> = {};
      for (const item of inbox) kinds[item.kind as string] = (kinds[item.kind as string] ?? 0) + 1;
      return json({ items: page, total: inbox.length, limit, offset, next_offset: next, facets: { kind: kinds } });
    }
    return json({});
  });
  Object.defineProperty(window, 'fetch', { writable: true, configurable: true, value: spy });
}

describe('attentionStore', () => {
  beforeEach(() => {
    inbox = [];
    listings = [];
    resolved = [];
    resetOrchestratorStore();
    resetSDKClient();
    setUnauthenticated();
    stubFetch();
    setAuthenticated({ access_token: sessionToken('burnin') });
  });

  afterEach(() => {
    resetOrchestratorStore();
    setUnauthenticated();
    vi.restoreAllMocks();
  });

  it('follows every page, so the 101st parked task is reachable and counted', async () => {
    inbox = [...Array.from({ length: 101 }, (_, i) => parked(i + 1)), ALERT];

    await refreshAttention();

    expect(attentionStore.items.length).toBe(102);
    expect(attentionStore.items.some((item) => item.id === 'task-101')).toBe(true);
    expect(attentionStore.total).toBe(102);
    expect(attentionStore.visibleAlerts.map((a) => a.id)).toEqual(['agent_offline:a1']);
    // The second page is asked for by offset (the first omits offset 0).
    expect(listings.some((u) => u.searchParams.get('offset') === '100')).toBe(true);
  });

  it('never narrows the fetch, so the banner keeps every alert whatever the inbox filters', async () => {
    inbox = [parked(1), ALERT];

    await refreshAttention();

    for (const url of listings) {
      expect(url.searchParams.get('kind')).toBeNull();
      expect(url.searchParams.get('project_id')).toBeNull();
    }
    expect(attentionStore.visibleAlerts.length).toBe(1);
  });

  it('a background refresh replaces the set with the complete current one', async () => {
    inbox = [parked(1), ALERT];
    await refreshAttention();
    inbox = [parked(1), parked(2), ALERT];

    await refreshAttention();

    expect(attentionStore.items.map((i) => i.id)).toEqual(['task-1', 'task-2', 'agent_offline:a1']);
    expect(attentionStore.total).toBe(3);
  });

  it('a resolution re-reads the complete set, keeping the alerts', async () => {
    inbox = [parked(1), parked(2), ALERT];
    await refreshAttention();

    const ok = await resolveAttentionItem('task-1', 'readmit');

    expect(ok).toBe(true);
    expect(resolved).toEqual(['task-1']);
    expect(attentionStore.items.map((i) => i.id)).toEqual(['task-2', 'agent_offline:a1']);
    expect(attentionStore.visibleAlerts.length).toBe(1);
  });
});
