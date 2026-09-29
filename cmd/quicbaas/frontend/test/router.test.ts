/**
 * router.test.ts
 *
 * Regression: the hash router fell back to "Coming Soon" for hashes like
 * `#/live-events`, a bookmark-friendly alias for the canonical `#/events`
 * route — direct navigation via either path must land on the same view.
 *
 * `#/agents` is deliberately NOT an alias of `#/fleet`: the fleet was
 * renamed, and the word is kept free for AI agent definitions.
 *
 * The router lives in src/stores/router.svelte.ts and exposes parseHash
 * indirectly via the routerStore. We exercise it by setting
 * window.location.hash, dispatching hashchange, and asserting the
 * resolved route name.
 */

import { describe, it, expect, beforeEach } from 'vitest';

// The router module installs a hashchange listener at import time and
// resolves its initial state from window.location.hash. We import it
// once after we've set the hash to a known value to avoid leaking state
// between tests.
async function loadRouter() {
  // Reset the module registry so each test gets a fresh listener bound
  // to the hash we set just before importing.
  const mod = await import('../src/stores/router.svelte.ts');
  return mod;
}

function setHashAndFire(hash: string): void {
  window.location.hash = hash;
  window.dispatchEvent(new HashChangeEvent('hashchange'));
}

describe('router hash aliases', () => {
  beforeEach(() => {
    // Reset to a known hash before each test.
    window.location.hash = '#/dashboard';
  });

  it('#/fleet resolves to the fleet route (canonical)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/fleet');
    expect(routerStore.name).toBe('fleet');
  });

  it('#/agents is not an alias of the fleet', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/agents');
    expect(routerStore.name).not.toBe('fleet');
    setHashAndFire('#/agents/node-xyz-123');
    expect(routerStore.name).not.toBe('fleet-node');
  });

  it('#/fleet/:id resolves to the fleet node detail (parameterised path)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/fleet/node-xyz-123');
    expect(routerStore.name).toBe('fleet-node');
    expect(routerStore.params['id']).toBe('node-xyz-123');
  });

  it('#/events resolves to the events route (canonical)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/events');
    expect(routerStore.name).toBe('events');
  });

  it('#/live-events resolves to the events route (alias)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/live-events');
    expect(routerStore.name).toBe('events');
  });

  it('#/cost resolves to the cost route (canonical)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/cost');
    expect(routerStore.name).toBe('cost');
  });

  it('#/cost-tracking resolves to the cost route (alias)', async () => {
    // Sidebar label is "Cost Tracking" — operators who type the label
    // hyphenated as a URL should land on the same view, not "Coming Soon".
    const { routerStore } = await loadRouter();
    setHashAndFire('#/cost-tracking');
    expect(routerStore.name).toBe('cost');
  });

  it('#/ssh-keys resolves to the ssh-keys route (canonical)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/ssh-keys');
    expect(routerStore.name).toBe('ssh-keys');
  });

  it('#/sshkeys resolves to the ssh-keys route (alias)', async () => {
    // Operators who type the path without a hyphen should land on the
    // same view rather than the placeholder.
    const { routerStore } = await loadRouter();
    setHashAndFire('#/sshkeys');
    expect(routerStore.name).toBe('ssh-keys');
  });

  it('unknown hashes still fall through to not-found', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/totally-not-a-route');
    expect(routerStore.name).toBe('not-found');
  });

  // ---------------------------------------------------------------------------
  // Issue #80 regression: IAM tab deep-links must resolve to the iam route
  // with the correct tab param rather than falling through to not-found.
  // ---------------------------------------------------------------------------

  it('#80: #/iam resolves to iam route with no tab param', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam');
    expect(routerStore.name).toBe('iam');
    expect(routerStore.params['tab']).toBeUndefined();
  });

  it('#80: #/iam/users resolves to iam route with tab=users', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/users');
    expect(routerStore.name).toBe('iam');
    expect(routerStore.params['tab']).toBe('users');
  });

  it('#80: #/iam/tenants resolves to iam route with tab=tenants', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/tenants');
    expect(routerStore.name).toBe('iam');
    expect(routerStore.params['tab']).toBe('tenants');
  });

  it('#80: #/iam/roles resolves to iam route with tab=roles', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/roles');
    expect(routerStore.name).toBe('iam');
    expect(routerStore.params['tab']).toBe('roles');
  });

  it('#80: #/iam/policies resolves to iam route with tab=policies', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/policies');
    expect(routerStore.name).toBe('iam');
    expect(routerStore.params['tab']).toBe('policies');
  });

  it('#80: unknown /iam/subpath does NOT match IAM tab routes', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/unknown-tab');
    expect(routerStore.name).toBe('not-found');
  });
});

// ---------------------------------------------------------------------------
// Software Factory stage 6b (#565): task detail route and generic query params.
// ---------------------------------------------------------------------------

describe('router — task detail and query params', () => {
  beforeEach(() => {
    window.location.hash = '#/dashboard';
  });

  it('#/tasks/:id resolves to task-detail with the id decoded', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/tasks/task%2F42');
    expect(routerStore.name).toBe('task-detail');
    expect(routerStore.params['id']).toBe('task/42');
  });

  it('#/tasks/ alone is the task list, not a detail with an empty id', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/tasks/');
    expect(routerStore.name).toBe('tasks');
    expect(routerStore.params['id']).toBeUndefined();
  });

  it('#/tasks?project=p1 resolves to tasks with the project query param', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/tasks?project=p1');
    expect(routerStore.name).toBe('tasks');
    expect(routerStore.params['project']).toBe('p1');
  });

  it('query params are parsed for every route and decoded', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/jobs?state=executing&q=a%20b');
    expect(routerStore.name).toBe('jobs');
    expect(routerStore.params['state']).toBe('executing');
    expect(routerStore.params['q']).toBe('a b');
  });

  it('a path param wins over a query param of the same name', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/tasks/task-1?id=other');
    expect(routerStore.name).toBe('task-detail');
    expect(routerStore.params['id']).toBe('task-1');
  });

  it('a route with no query has no stray params', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/tasks');
    expect(routerStore.name).toBe('tasks');
    expect(Object.keys(routerStore.params)).toEqual([]);
  });

  it('routeTasks(projectId) and routeTaskDetail(id) build the encoded hashes', async () => {
    const { routeTasks, routeTaskDetail } = await loadRouter();
    // The parameter is the Tasks TABLE's own project facet, so a link built
    // here and one built by clicking the chip are the same link.
    routeTasks('proj 1');
    expect(window.location.hash).toBe('#/tasks?tasks_project=proj%201');
    routeTasks();
    expect(window.location.hash).toBe('#/tasks');
    routeTaskDetail('task/42');
    expect(window.location.hash).toBe('#/tasks/task%2F42');
  });
});
