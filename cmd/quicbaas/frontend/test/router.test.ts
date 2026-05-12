/**
 * router.test.ts
 *
 * Regression: the hash router fell back to "Coming Soon" for hashes like
 * `#/agents` and `#/live-events`. These are bookmark-friendly aliases for
 * the canonical `#/fleet` and `#/events` routes — direct navigation via
 * either path must land on the same view.
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
    expect(routerStore.route.name).toBe('fleet');
  });

  it('#/agents resolves to the fleet route (alias)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/agents');
    expect(routerStore.route.name).toBe('fleet');
  });

  it('#/agents/:id still resolves to agent-detail (parameterised path)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/agents/agent-xyz-123');
    expect(routerStore.route.name).toBe('agent-detail');
    expect(routerStore.route.params['id']).toBe('agent-xyz-123');
  });

  it('#/events resolves to the events route (canonical)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/events');
    expect(routerStore.route.name).toBe('events');
  });

  it('#/live-events resolves to the events route (alias)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/live-events');
    expect(routerStore.route.name).toBe('events');
  });

  it('#/cost resolves to the cost route (canonical)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/cost');
    expect(routerStore.route.name).toBe('cost');
  });

  it('#/cost-tracking resolves to the cost route (alias)', async () => {
    // Sidebar label is "Cost Tracking" — operators who type the label
    // hyphenated as a URL should land on the same view, not "Coming Soon".
    const { routerStore } = await loadRouter();
    setHashAndFire('#/cost-tracking');
    expect(routerStore.route.name).toBe('cost');
  });

  it('#/ssh-keys resolves to the ssh-keys route (canonical)', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/ssh-keys');
    expect(routerStore.route.name).toBe('ssh-keys');
  });

  it('#/sshkeys resolves to the ssh-keys route (alias)', async () => {
    // Operators who type the path without a hyphen should land on the
    // same view rather than the placeholder.
    const { routerStore } = await loadRouter();
    setHashAndFire('#/sshkeys');
    expect(routerStore.route.name).toBe('ssh-keys');
  });

  it('unknown hashes still fall through to not-found', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/totally-not-a-route');
    expect(routerStore.route.name).toBe('not-found');
  });

  // ---------------------------------------------------------------------------
  // Issue #80 regression: IAM tab deep-links must resolve to the iam route
  // with the correct tab param rather than falling through to not-found.
  // ---------------------------------------------------------------------------

  it('#80: #/iam resolves to iam route with no tab param', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam');
    expect(routerStore.route.name).toBe('iam');
    expect(routerStore.route.params['tab']).toBeUndefined();
  });

  it('#80: #/iam/users resolves to iam route with tab=users', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/users');
    expect(routerStore.route.name).toBe('iam');
    expect(routerStore.route.params['tab']).toBe('users');
  });

  it('#80: #/iam/tenants resolves to iam route with tab=tenants', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/tenants');
    expect(routerStore.route.name).toBe('iam');
    expect(routerStore.route.params['tab']).toBe('tenants');
  });

  it('#80: #/iam/roles resolves to iam route with tab=roles', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/roles');
    expect(routerStore.route.name).toBe('iam');
    expect(routerStore.route.params['tab']).toBe('roles');
  });

  it('#80: #/iam/policies resolves to iam route with tab=policies', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/policies');
    expect(routerStore.route.name).toBe('iam');
    expect(routerStore.route.params['tab']).toBe('policies');
  });

  it('#80: unknown /iam/subpath does NOT match IAM tab routes', async () => {
    const { routerStore } = await loadRouter();
    setHashAndFire('#/iam/unknown-tab');
    expect(routerStore.route.name).toBe('not-found');
  });
});
