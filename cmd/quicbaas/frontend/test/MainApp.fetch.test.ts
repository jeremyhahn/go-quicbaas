/**
 * MainApp.fetch.test.ts
 *
 * Regression guard: ensures MainApp.svelte routes ALL data calls through the
 * SDK client and never calls window.fetch() directly.
 *
 * Two assertions matter here:
 *   1. SDK methods (listJobs, listFleetNodes, listSessions, listTasks, healthCheck,
 *      authValidate) are invoked with the correct arguments on mount.
 *   2. window.fetch is called ZERO times — the anti-regression that prevents
 *      anyone from sneaking a raw fetch() back in.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, cleanup } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mock window.fetch before anything else runs.
// ---------------------------------------------------------------------------

const fetchSpy = vi.fn();
Object.defineProperty(window, 'fetch', {
  writable: true,
  configurable: true,
  value: fetchSpy,
});

// ---------------------------------------------------------------------------
// SDK client mock — must be set up BEFORE the component is imported so that
// vi.mock hoisting works correctly.
// ---------------------------------------------------------------------------

const mockListJobs        = vi.fn().mockResolvedValue([]);
const mockListAgents      = vi.fn().mockResolvedValue({ nodes: [] });
const mockListSessions    = vi.fn().mockResolvedValue({ sessions: [] });
const mockListTasks       = vi.fn().mockResolvedValue([]);
const mockHealthCheck     = vi.fn().mockResolvedValue({ status: 'ok' });
const mockAuthValidate    = vi.fn().mockResolvedValue(true);
const mockSubscribeJobEvents = vi.fn().mockReturnValue(() => { /* cleanup noop */ });

function makeMockClient() {
  return {
    orchestrator: {
      listJobs:           mockListJobs,
      listFleetNodes:         mockListAgents,
      listSessions:       mockListSessions,
      listTasks:          mockListTasks,
      healthCheck:        mockHealthCheck,
      subscribeJobEvents: mockSubscribeJobEvents,
      // Stubs for methods not exercised by MainApp mount:
      getJobStatus:       vi.fn().mockResolvedValue({}),
      startJob:           vi.fn().mockResolvedValue({}),
      cancelJob:          vi.fn().mockResolvedValue(undefined),
      listBackends:       vi.fn().mockResolvedValue([]),
      getFleetNode:     vi.fn().mockResolvedValue({}),
      listTasks2:         vi.fn().mockResolvedValue([]),
      getTask:            vi.fn().mockResolvedValue({}),
      spawnSession:       vi.fn().mockResolvedValue({}),
      promptSession:      vi.fn().mockResolvedValue({}),
      closeSession:       vi.fn().mockResolvedValue({}),
      getSessionHistory:  vi.fn().mockResolvedValue({ events: [], next_seq: 0 }),
      subscribeSession:   vi.fn().mockReturnValue((async function* () {})()),
    },
    connection: {
      connect:      vi.fn().mockResolvedValue(undefined),
      disconnect:   vi.fn().mockResolvedValue(undefined),
      isConnected:  vi.fn().mockReturnValue(true),
      getAddress:   vi.fn().mockReturnValue('localhost:9443'),
      getTransport: vi.fn().mockReturnValue('rest'),
    },
    auth: {
      authValidate: mockAuthValidate,
    },
  };
}

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient:   vi.fn().mockImplementation(() => Promise.resolve(makeMockClient())),
  resetSDKClient: vi.fn(),
}));

// Also stub the SDK isWailsRuntime so the auth guard runs (non-Wails path).
vi.mock('@quicbaas/sdk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@quicbaas/sdk')>();
  return {
    ...actual,
    isWailsRuntime: vi.fn().mockReturnValue(false),
  };
});

// Stub the hash router so MainApp doesn't error on window.location access.
vi.mock('../src/stores/router.svelte.ts', () => ({
  routerStore: {
    get route() {
      return { name: 'dashboard', params: {}, hash: '#/dashboard' };
    },
  },
  routeLogin:         vi.fn(),
  routeSessions:      vi.fn(),
  routeDashboard:     vi.fn(),
  routeJobs:          vi.fn(),
  routeFleet:         vi.fn(),
  routeTasks:         vi.fn(),
  routeEvents:        vi.fn(),
  routeCost:          vi.fn(),
  routeSessionDetail: vi.fn(),
  navigateTo:         vi.fn(),
}));

// Stub the fleetStore and sessionStore auto-refresh so they don't fire
// additional SDK calls that interfere with our assertions.
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

// Stub SystemDashboard so it doesn't mount and fire its own fetch() probes.
// The MainApp anti-regression test only cares that MainApp itself doesn't
// issue raw fetch() calls — SystemDashboard's health probes are covered by
// its own test suite (SystemDashboard.test.ts).
// Svelte 5 components are functions; the mock must be callable.
vi.mock('../src/views/SystemDashboard.svelte', () => ({
  default: vi.fn(() => ({})),
}));

// Import the component under test AFTER mocks are declared.
import MainApp from '../src/views/MainApp.svelte';
import { resetOrchestratorStore, stopOrchestratorPolling } from '../src/stores/orchestrator.svelte.ts';
import { authStore, setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('MainApp — no direct fetch() calls', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    fetchSpy.mockReset();
    // The orchestrator store's poller and stale-while-revalidate timestamps are
    // module-level, so without a reset the second test's mount sees "fresh"
    // data and skips the fetch. resetOrchestratorStore() clears both.
    resetOrchestratorStore();
    stopOrchestratorPolling();
  });

  afterEach(() => {
    cleanup();
    stopOrchestratorPolling();
    setUnauthenticated();
  });

  it('calls listJobs via SDK with the active tenant ID on mount', async () => {
    // Polling is gated on an authenticated session (go-quicbaas UI-login-
    // refused investigation, 2026-09-25) — it no longer fires for an
    // anonymous/unauthenticated tab, so establish one the same way a real
    // sign-in does before mounting.
    setAuthenticated({ access_token: 'test-token-123' });
    render(MainApp);

    // Allow microtasks and effect queue to drain.
    await vi.waitFor(() => {
      expect(mockListJobs).toHaveBeenCalled();
    }, { timeout: 2000 });

    // No session is stored in this case, so the store falls back to 'system',
    // qrdb's canonical control-plane tenant. When a session IS present the
    // tenant comes from its tenant_id claim instead — see
    // orchestratorSessionTenant.test.ts.
    expect(mockListJobs).toHaveBeenCalledWith('system');
  });

  it('calls listFleetNodes via SDK with the active tenant ID on mount', async () => {
    setAuthenticated({ access_token: 'test-token-123' });
    render(MainApp);

    await vi.waitFor(() => {
      expect(mockListAgents).toHaveBeenCalled();
    }, { timeout: 2000 });

    expect(mockListAgents).toHaveBeenCalledWith(
      expect.objectContaining({ tenant_id: 'system' }),
    );
  });

  it('calls listSessions via SDK with the active tenant ID on mount', async () => {
    setAuthenticated({ access_token: 'test-token-123' });
    render(MainApp);

    await vi.waitFor(() => {
      expect(mockListSessions).toHaveBeenCalled();
    }, { timeout: 2000 });

    // Signed out, so the 'system' fallback applies (see above).
    expect(mockListSessions).toHaveBeenCalledWith(
      expect.objectContaining({ tenantID: 'system' }),
    );

    // Defense: must never be called with an empty object (no tenantID).
    const calls = mockListSessions.mock.calls;
    for (const [arg] of calls) {
      expect(arg).toHaveProperty('tenantID');
      expect(typeof (arg as { tenantID: unknown }).tenantID).toBe('string');
      expect((arg as { tenantID: string }).tenantID.length).toBeGreaterThan(0);
    }
  });

  it('calls listTasks via SDK with the active tenant ID on mount', async () => {
    setAuthenticated({ access_token: 'test-token-123' });
    render(MainApp);

    await vi.waitFor(() => {
      expect(mockListTasks).toHaveBeenCalled();
    }, { timeout: 2000 });

    expect(mockListTasks).toHaveBeenCalledWith('system');
  });

  // NOTE: health is no longer fetched by MainApp — it only renders the health
  // badge from orchestratorStore.health. The SDK health fetch moved to
  // SystemDashboard (client.orchestrator.healthCheck()) and is covered by
  // SystemDashboard.test.ts, which is why SystemDashboard is mocked out here.

  it('calls authValidate with stored token via SDK auth client on mount (non-Wails)', async () => {
    // The auth guard only calls authValidate when a token is stored in localStorage.
    localStorage.setItem('quicbaas_token', 'test-token-123');
    try {
      render(MainApp);

      await vi.waitFor(() => {
        expect(mockAuthValidate).toHaveBeenCalledWith('test-token-123');
      }, { timeout: 2000 });
    } finally {
      localStorage.removeItem('quicbaas_token');
    }
  });

  it('skips authValidate when no token is stored (non-Wails)', async () => {
    // Without a stored token the guard is a no-op; authValidate must NOT fire.
    localStorage.removeItem('quicbaas_token');
    render(MainApp);

    // Wait for validateSession()'s no-token branch to settle instead of the
    // orchestrator poller: polling is now gated on an authenticated session
    // (go-quicbaas UI-login-refused investigation, 2026-09-25) and never
    // fires here at all, so mockListJobs is not a usable settle point.
    await vi.waitFor(() => {
      expect(authStore.status).toBe('unauthenticated');
    }, { timeout: 2000 });

    expect(mockAuthValidate).not.toHaveBeenCalled();
    expect(mockListJobs).not.toHaveBeenCalled();
  });

  it('never calls window.fetch() directly — anti-regression guard', async () => {
    setAuthenticated({ access_token: 'test-token-123' });
    render(MainApp);

    // Wait long enough for all $effect callbacks to have fired.
    await vi.waitFor(() => {
      expect(mockListJobs).toHaveBeenCalled();
    }, { timeout: 2000 });

    // This is the critical assertion: zero direct fetch calls allowed.
    expect(fetchSpy).toHaveBeenCalledTimes(0);
  });
});
