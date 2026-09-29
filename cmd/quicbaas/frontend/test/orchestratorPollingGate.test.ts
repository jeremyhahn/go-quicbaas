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
 * Regression coverage for the fourth piece of the "UI login refused"
 * investigation (2026-09-25): MainApp's orchestrator/aux polling effect ran
 * unconditionally on every mount, on every route — including the public ones
 * (/login, /register, /invite-activate) — because it was never gated on
 * authStore.status. Reproduced live: the invite page showed "Could not load
 * the attention inbox: authorization header is missing" repeatedly, seconds
 * apart, before any login had happened.
 *
 * The same gap meant polling that was ALREADY RUNNING when a session ended —
 * expiry, a rejected renewal, an explicit sign-out — kept firing every 30 s
 * afterwards with no token to attach, which is what turned a single
 * session-ending event into the sustained, repeating "bearer token refused"
 * pattern observed at the daemon.
 *
 * Fix: the polling effect in MainApp.svelte now reads authStore.status and
 * no-ops (via its own cleanup) whenever it is not 'authenticated'.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, cleanup } from '@testing-library/svelte';

const listJobs = vi.fn().mockResolvedValue([]);
const listFleetNodes = vi.fn().mockResolvedValue({ agents: [] });
const listSessions = vi.fn().mockResolvedValue({ sessions: [] });
const listTasks = vi.fn().mockResolvedValue([]);
const healthCheck = vi.fn().mockResolvedValue({ status: 'ok' });
const mockAuthValidate = vi.fn().mockResolvedValue(true);
const mockListTenants = vi.fn().mockResolvedValue({ tenants: [] });

const mockClient = {
  orchestrator: {
    listJobs,
    listFleetNodes,
    listSessions,
    listTasks,
    healthCheck,
    subscribeJobEvents: vi.fn().mockReturnValue(() => { /* cleanup noop */ }),
  },
  connection: {
    connect: vi.fn().mockResolvedValue(undefined),
    disconnect: vi.fn().mockResolvedValue(undefined),
    isConnected: vi.fn().mockReturnValue(true),
    getAddress: vi.fn().mockReturnValue('localhost:9443'),
    getTransport: vi.fn().mockReturnValue('rest'),
  },
  auth: {
    authValidate: mockAuthValidate,
    logout: vi.fn().mockResolvedValue(undefined),
  },
  iam: {
    listTenants: mockListTenants,
  },
};

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockImplementation(() => Promise.resolve(mockClient)),
  resetSDKClient: vi.fn(),
}));

vi.mock('@quicbaas/sdk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@quicbaas/sdk')>();
  return {
    ...actual,
    isWailsRuntime: vi.fn().mockReturnValue(false),
  };
});

vi.mock('../src/stores/fleetStore.svelte.ts', () => ({
  fleetStore: { nodes: [], nodesLoading: false, onlineNodes: [] },
  startAgentsAutoRefresh: vi.fn().mockReturnValue(() => { /* cleanup noop */ }),
  refreshFleet: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: { sessions: [], sessionsLoading: false, activeSession: null, events: [], historyExhausted: false },
  startSessionsAutoRefresh: vi.fn().mockReturnValue(() => { /* cleanup noop */ }),
  refreshSessions: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../src/views/SystemDashboard.svelte', () => ({
  default: vi.fn(() => ({})),
}));

import MainApp from '../src/views/MainApp.svelte';
import { resetOrchestratorStore, stopOrchestratorPolling } from '../src/stores/orchestrator.svelte.ts';
import { navigateTo, routerStore } from '../src/stores/router.svelte.ts';
import { setAuthenticated, setAuthStatus, setUnauthenticated } from '../src/stores/auth.svelte.ts';

async function goto(path: string): Promise<void> {
  navigateTo(path);
  await vi.waitFor(() => {
    expect(routerStore.hash).toBe(`#${path}`);
  });
}

describe('orchestrator/scheduler polling is gated on an authenticated session', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuthValidate.mockResolvedValue(true);
    resetOrchestratorStore();
    stopOrchestratorPolling();
    setUnauthenticated();
  });

  afterEach(() => {
    cleanup();
    stopOrchestratorPolling();
    setUnauthenticated();
  });

  it('never fires an orchestrator poll on a public route with no session', async () => {
    await goto('/login');
    setAuthStatus('unauthenticated');

    render(MainApp);

    // Give every pending microtask/effect a chance to run.
    await vi.waitFor(() => {
      expect(routerStore.hash).toBe('#/login');
    });
    await new Promise((resolve) => setTimeout(resolve, 20));

    expect(listJobs).not.toHaveBeenCalled();
    expect(listFleetNodes).not.toHaveBeenCalled();
    expect(listSessions).not.toHaveBeenCalled();
    expect(listTasks).not.toHaveBeenCalled();
  });

  it('never fires an orchestrator poll while the session is still pending validation', async () => {
    await goto('/dashboard');
    setAuthStatus('pending');

    render(MainApp);
    await new Promise((resolve) => setTimeout(resolve, 20));

    expect(listJobs).not.toHaveBeenCalled();
    expect(listFleetNodes).not.toHaveBeenCalled();
    expect(listSessions).not.toHaveBeenCalled();
    expect(listTasks).not.toHaveBeenCalled();
  });

  it('polls once authenticated, and stops immediately when the session ends', async () => {
    await goto('/dashboard');
    setAuthenticated({ access_token: 'test-access-token' });

    render(MainApp);

    await vi.waitFor(() => {
      expect(listJobs).toHaveBeenCalled();
    });
    expect(listTasks).toHaveBeenCalled();

    const jobsCallsAtSessionEnd = listJobs.mock.calls.length;
    setUnauthenticated('test teardown');

    // A tick for the effect's cleanup (stopPolling()) to actually run.
    await new Promise((resolve) => setTimeout(resolve, 20));

    // No further poll ticks after the session ended — the interval that
    // would fire them was torn down along with everything else the effect's
    // cleanup owns.
    listJobs.mockClear();
    listFleetNodes.mockClear();
    listSessions.mockClear();
    listTasks.mockClear();
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(listJobs).not.toHaveBeenCalled();
    expect(jobsCallsAtSessionEnd).toBeGreaterThan(0);
  });
});
