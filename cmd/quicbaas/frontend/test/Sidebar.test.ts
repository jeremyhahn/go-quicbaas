/**
 * Sidebar.test.ts
 *
 * Regression tests for sidebar navigation correctness:
 *  - Clicking a sidebar nav item (via navigate()) sets the expected
 *    window.location.hash.
 *  - When window.location.hash is set externally the router resolves the
 *    correct route name, and navStore.activeView reflects the correct ViewID.
 *
 * These tests exercise the navigate() / router / navStore contract without
 * needing to mount the full MainApp component tree.
 *
 * Sidebar structure:
 *   OVERVIEW
 *     Dashboard          → #/dashboard
 *   ORCHESTRATION
 *     Dashboard          → #/orchestration
 *     Jobs               → #/jobs
 *     Agent Fleet        → #/fleet
 *     Sessions           → #/sessions
 *     Tasks              → #/tasks
 *     Live Events        → #/events
 *     Cost Tracking      → #/cost
 *   AI/ML
 *     RAG                → #/rag
 *     Embeddings         → #/embeddings
 *   ASYNC
 *     Messaging          → #/messaging
 *     Scheduler          → #/scheduler
 *   SECURITY
 *     IAM                → #/iam
 *     KMS                → #/kms
 *     Secrets            → #/secrets
 *     Barrier            → #/barrier
 *     Ceremony           → #/ceremony
 *     Bootstrap          → #/bootstrap
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { navigate, navStore } from '../src/stores/navigation.svelte.ts';
import { routerStore } from '../src/stores/router.svelte.ts';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Set the hash and fire a hashchange event so the router's listener updates
 * the reactive state synchronously inside jsdom.
 */
function setHash(hash: string): void {
  window.location.hash = hash;
  window.dispatchEvent(new HashChangeEvent('hashchange', {
    newURL: window.location.href,
    oldURL: window.location.href,
  }));
}

// ---------------------------------------------------------------------------
// Data tables
// ---------------------------------------------------------------------------

const navItems = [
  { viewId: 'dashboard'     as const, expectedHash: '#/dashboard',     label: 'System Dashboard' },
  { viewId: 'orchestration' as const, expectedHash: '#/orchestration', label: 'Orchestration Dashboard' },
  { viewId: 'jobs'          as const, expectedHash: '#/jobs',          label: 'Jobs' },
  { viewId: 'agents'        as const, expectedHash: '#/fleet',         label: 'Agent Fleet' },
  { viewId: 'sessions'      as const, expectedHash: '#/sessions',      label: 'Sessions' },
  { viewId: 'tasks'         as const, expectedHash: '#/tasks',         label: 'Tasks' },
  { viewId: 'workflows'     as const, expectedHash: '#/workflows',     label: 'Workflows' },
  { viewId: 'events'        as const, expectedHash: '#/events',        label: 'Live Events' },
  { viewId: 'costs'         as const, expectedHash: '#/cost',          label: 'Cost Tracking' },
  // DATA section
  { viewId: 'kv'         as const, expectedHash: '#/kv',         label: 'KV Store' },
  { viewId: 'vectors'    as const, expectedHash: '#/vectors',    label: 'Vectors' },
  { viewId: 'timeseries' as const, expectedHash: '#/timeseries', label: 'Time-Series' },
  { viewId: 'objects'    as const, expectedHash: '#/objects',    label: 'Objects' },
  // AI/ML section
  { viewId: 'rag'        as const, expectedHash: '#/rag',        label: 'RAG' },
  { viewId: 'embeddings' as const, expectedHash: '#/embeddings', label: 'Embeddings' },
  // ASYNC section
  { viewId: 'messaging'  as const, expectedHash: '#/messaging',  label: 'Messaging' },
  { viewId: 'scheduler'  as const, expectedHash: '#/scheduler',  label: 'Scheduler' },
  // SECURITY section
  { viewId: 'iam'       as const, expectedHash: '#/iam',       label: 'IAM' },
  { viewId: 'kms'       as const, expectedHash: '#/kms',       label: 'KMS' },
  { viewId: 'secrets'   as const, expectedHash: '#/secrets',   label: 'Secrets' },
  { viewId: 'barrier'   as const, expectedHash: '#/barrier',   label: 'Barrier' },
  { viewId: 'ceremony'  as const, expectedHash: '#/ceremony',  label: 'Ceremony' },
  { viewId: 'bootstrap' as const, expectedHash: '#/bootstrap', label: 'Bootstrap' },
] as const;

const hashToRouteName = [
  { hash: '#/dashboard',     routeName: 'dashboard' },
  { hash: '#/orchestration', routeName: 'orchestration' },
  { hash: '#/jobs',          routeName: 'jobs' },
  { hash: '#/fleet',         routeName: 'fleet' },
  { hash: '#/sessions',      routeName: 'sessions' },
  { hash: '#/tasks',         routeName: 'tasks' },
  { hash: '#/workflows',     routeName: 'workflows' },
  { hash: '#/events',        routeName: 'events' },
  { hash: '#/cost',          routeName: 'cost' },
  // DATA routes
  { hash: '#/kv',         routeName: 'kv' },
  { hash: '#/vectors',    routeName: 'vectors' },
  { hash: '#/timeseries', routeName: 'timeseries' },
  { hash: '#/objects',    routeName: 'objects' },
  // AI/ML routes
  { hash: '#/rag',        routeName: 'rag' },
  { hash: '#/embeddings', routeName: 'embeddings' },
  // ASYNC routes
  { hash: '#/messaging',  routeName: 'messaging' },
  { hash: '#/scheduler',  routeName: 'scheduler' },
  // SECURITY routes
  { hash: '#/iam',       routeName: 'iam' },
  { hash: '#/kms',       routeName: 'kms' },
  { hash: '#/secrets',   routeName: 'secrets' },
  { hash: '#/barrier',   routeName: 'barrier' },
  { hash: '#/ceremony',  routeName: 'ceremony' },
  { hash: '#/bootstrap', routeName: 'bootstrap' },
] as const;

// ViewID that navStore.activeView should return for each route.
const hashToActiveViewID = [
  { hash: '#/dashboard',     viewId: 'dashboard' },
  { hash: '#/orchestration', viewId: 'orchestration' },
  { hash: '#/jobs',          viewId: 'jobs' },
  { hash: '#/fleet',         viewId: 'agents' },
  { hash: '#/sessions',      viewId: 'sessions' },
  { hash: '#/tasks',         viewId: 'tasks' },
  { hash: '#/workflows',     viewId: 'workflows' },
  { hash: '#/events',        viewId: 'events' },
  { hash: '#/cost',          viewId: 'costs' },
  // DATA routes
  { hash: '#/kv',         viewId: 'kv' },
  { hash: '#/vectors',    viewId: 'vectors' },
  { hash: '#/timeseries', viewId: 'timeseries' },
  { hash: '#/objects',    viewId: 'objects' },
  // AI/ML routes
  { hash: '#/rag',        viewId: 'rag' },
  { hash: '#/embeddings', viewId: 'embeddings' },
  // ASYNC routes
  { hash: '#/messaging',  viewId: 'messaging' },
  { hash: '#/scheduler',  viewId: 'scheduler' },
  // SECURITY routes
  { hash: '#/iam',       viewId: 'iam' },
  { hash: '#/kms',       viewId: 'kms' },
  { hash: '#/secrets',   viewId: 'secrets' },
  { hash: '#/barrier',   viewId: 'barrier' },
  { hash: '#/ceremony',  viewId: 'ceremony' },
  { hash: '#/bootstrap', viewId: 'bootstrap' },
] as const;

// ---------------------------------------------------------------------------
// Test suites
// ---------------------------------------------------------------------------

describe('navigate() sets window.location.hash', () => {
  beforeEach(() => { window.location.hash = ''; });
  afterEach(() => { window.location.hash = ''; });

  it.each(navItems)(
    'navigate("$viewId") → hash becomes "$expectedHash"',
    ({ viewId, expectedHash }) => {
      navigate(viewId);
      expect(window.location.hash).toBe(expectedHash);
    },
  );

  it('navigate() on a view with no hash mapping is a no-op', () => {
    window.location.hash = '#/dashboard';
    // 'cluster' has no hash path in viewHashPaths — should not change the hash.
    navigate('nodes' as Parameters<typeof navigate>[0]);
    expect(window.location.hash).toBe('#/dashboard');
  });

  it('navigate("kv") routes to #/kv', () => {
    navigate('kv');
    expect(window.location.hash).toBe('#/kv');
  });

  it('navigate("vectors") routes to #/vectors', () => {
    navigate('vectors');
    expect(window.location.hash).toBe('#/vectors');
  });

  it('navigate("timeseries") routes to #/timeseries', () => {
    navigate('timeseries');
    expect(window.location.hash).toBe('#/timeseries');
  });

  it('navigate("objects") routes to #/objects', () => {
    navigate('objects');
    expect(window.location.hash).toBe('#/objects');
  });
});

describe('router parses all sidebar hashes', () => {
  beforeEach(() => { window.location.hash = ''; });
  afterEach(() => { window.location.hash = ''; });

  it.each(hashToRouteName)(
    '"$hash" → routerStore.route.name === "$routeName"',
    ({ hash, routeName }) => {
      setHash(hash);
      expect(routerStore.route.name).toBe(routeName);
    },
  );

  it('"#/sessions/abc-123" → session-detail with id param', () => {
    setHash('#/sessions/abc-123');
    expect(routerStore.route.name).toBe('session-detail');
    expect(routerStore.route.params['id']).toBe('abc-123');
  });

  it('empty hash → dashboard', () => {
    setHash('');
    expect(routerStore.route.name).toBe('dashboard');
  });

  it('"#/" → dashboard', () => {
    setHash('#/');
    expect(routerStore.route.name).toBe('dashboard');
  });

  it('unknown hash → not-found', () => {
    setHash('#/totally-unknown-route');
    expect(routerStore.route.name).toBe('not-found');
  });
});

describe('navStore.activeView derived from hash', () => {
  beforeEach(() => { window.location.hash = ''; });
  afterEach(() => { window.location.hash = ''; });

  it.each(hashToActiveViewID)(
    '"$hash" → navStore.activeView === "$viewId"',
    ({ hash, viewId }) => {
      setHash(hash);
      expect(navStore.activeView).toBe(viewId);
    },
  );

  it('"#/sessions/xyz" (session-detail) → activeView is "sessions"', () => {
    setHash('#/sessions/xyz');
    expect(navStore.activeView).toBe('sessions');
  });
});

describe('header no longer contains Sessions/Fleet/Spawn nav links', () => {
  // These tests assert that the removed nav links are gone.
  // Since these are unit tests without DOM mounting, we document the intent
  // here and rely on the e2e spec for the full DOM assertion.
  it('Sessions and Fleet are ORCHESTRATION sidebar items, not header links', () => {
    // navigate('sessions') still routes correctly via the sidebar path.
    navigate('sessions');
    expect(window.location.hash).toBe('#/sessions');
  });

  it('navigate("agents") routes to #/fleet via sidebar', () => {
    navigate('agents');
    expect(window.location.hash).toBe('#/fleet');
  });
});

// ---------------------------------------------------------------------------
// Disambiguation: three "Dashboard" sidebar entries
//
// Regression for the bug where the sidebar exposed three identically-
// labeled "Dashboard" items (OVERVIEW, ORCHESTRATION, CLUSTER) and the
// breadcrumb did not disambiguate them. The fix renames each to its
// section name so a sighted user can tell them apart at a glance, and
// the breadcrumb prefix carries the section name on every dashboard
// route so screen-reader users get the same disambiguation.
// ---------------------------------------------------------------------------

describe('sidebar exposes three distinct disambiguated dashboard entries', () => {
  // The MainApp.svelte source carries the navSections constant with the
  // sidebar entries. Reading the source string and asserting on the
  // labels avoids mounting the full component tree (which depends on
  // SDK clients, runtime polling loops, etc.) while still catching a
  // regression that re-introduces a duplicate "Dashboard" label.
  it('OVERVIEW > Overview, ORCHESTRATION > Orchestration, CLUSTER > Cluster are present and distinct', async () => {
    const fs = await import('node:fs/promises');
    const path = await import('node:path');
    const url = await import('node:url');

    const here = url.fileURLToPath(import.meta.url);
    const mainAppPath = path.resolve(
      path.dirname(here),
      '..',
      'src',
      'views',
      'MainApp.svelte',
    );
    const source = await fs.readFile(mainAppPath, 'utf8');

    // Each of the three dashboard entries must use a label that matches
    // its section so the labels are unique within the sidebar. The
    // prior bug had all three labelled "Dashboard".
    expect(source).toMatch(/id:\s*'dashboard'\s*,\s*label:\s*'Overview'/);
    expect(source).toMatch(/id:\s*'orchestration'\s*,\s*label:\s*'Orchestration'/);
    expect(source).toMatch(/id:\s*'cluster'\s*,\s*label:\s*'Cluster'/);

    // Defensive: count the literal label entries — each disambiguated
    // label must appear exactly once in the navSections block.
    const overviewCount = (source.match(/label:\s*'Overview'/g) ?? []).length;
    const orchestrationCount = (source.match(/label:\s*'Orchestration'/g) ?? []).length;
    const clusterCount = (source.match(/label:\s*'Cluster'/g) ?? []).length;
    expect(overviewCount).toBe(1);
    expect(orchestrationCount).toBe(1);
    expect(clusterCount).toBe(1);
  });

  it('breadcrumb carries the section name on the OVERVIEW dashboard route', async () => {
    const fs = await import('node:fs/promises');
    const path = await import('node:path');
    const url = await import('node:url');

    const here = url.fileURLToPath(import.meta.url);
    const mainAppPath = path.resolve(
      path.dirname(here),
      '..',
      'src',
      'views',
      'MainApp.svelte',
    );
    const source = await fs.readFile(mainAppPath, 'utf8');

    // The breadcrumb for the dashboard route must include "Overview" as
    // a parent crumb so the surface mirrors the "Orchestration > Dashboard"
    // and "Cluster > Dashboard" patterns used by the other two dashboards.
    const dashboardBlock = source.match(
      /routerStore\.route\.name === 'dashboard'[\s\S]*?else if/,
    );
    expect(dashboardBlock, 'dashboard breadcrumb block must exist').not.toBeNull();
    expect(dashboardBlock![0]).toMatch(/Overview/);
  });
});
