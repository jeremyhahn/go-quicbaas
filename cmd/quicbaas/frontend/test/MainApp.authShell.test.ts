/**
 * MainApp.authShell.test.ts
 *
 * Security regression guard: the app shell (header controls, tenant/user
 * menu, and — most importantly — the full feature sidebar) must NEVER render
 * before a session is confirmed authenticated by the server. Rendering it
 * pre-auth discloses the application's structure/feature set to anonymous
 * visitors, and every sidebar click fires an authenticated API call that
 * 401s.
 *
 * MainApp.svelte gates the entire shell behind a single `showAppShell`
 * derived value (authStatus === 'authenticated' && route is not public).
 * These tests pin that gate for all four states the auth store can be in
 * relative to a route:
 *   - unauthenticated + public route (login)      -> minimal shell, no nav
 *   - authenticated   + protected route (dashboard)-> full shell, nav present
 *   - authenticated -> logout                      -> nav disappears in-tab
 *   - pending (hydrating) + protected route         -> no nav flash
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, cleanup, screen } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// SDK client mock — set up before the component import so vi.mock hoisting
// works. Mirrors MainApp.fetch.test.ts's shape plus the iam/auth.logout
// surface this suite exercises.
// ---------------------------------------------------------------------------

const mockAuthValidate = vi.fn().mockResolvedValue(false);
const mockLogout = vi.fn().mockResolvedValue(undefined);
const mockListTenants = vi.fn().mockResolvedValue({ tenants: [] });

function makeMockClient() {
  return {
    orchestrator: {
      listJobs:           vi.fn().mockResolvedValue([]),
      listAgents:         vi.fn().mockResolvedValue({ agents: [] }),
      listSessions:       vi.fn().mockResolvedValue({ sessions: [] }),
      listTasks:          vi.fn().mockResolvedValue([]),
      healthCheck:        vi.fn().mockResolvedValue({ status: 'ok' }),
      subscribeJobEvents: vi.fn().mockReturnValue(() => { /* cleanup noop */ }),
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
      logout:       mockLogout,
    },
    iam: {
      listTenants: mockListTenants,
    },
  };
}

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient:   vi.fn().mockImplementation(() => Promise.resolve(makeMockClient())),
  resetSDKClient: vi.fn(),
}));

vi.mock('@quicbaas/sdk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@quicbaas/sdk')>();
  return {
    ...actual,
    isWailsRuntime: vi.fn().mockReturnValue(false),
  };
});

// Keep the auxiliary agent/session pollers out of the way — their own SDK
// traffic is irrelevant to the shell-gating assertions in this file.
vi.mock('../src/stores/agentStore.svelte.ts', () => ({
  agentStore: { agents: [], agentsLoading: false, onlineAgents: [] },
  startAgentsAutoRefresh: vi.fn().mockReturnValue(() => { /* cleanup noop */ }),
  refreshAgents: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: { sessions: [], sessionsLoading: false, activeSession: null, events: [], historyExhausted: false },
  startSessionsAutoRefresh: vi.fn().mockReturnValue(() => { /* cleanup noop */ }),
  refreshSessions: vi.fn().mockResolvedValue(undefined),
}));

// SystemDashboard is stubbed so mounting the authenticated 'dashboard' route
// doesn't fire its own health-probe traffic — irrelevant to shell gating.
vi.mock('../src/views/SystemDashboard.svelte', () => ({
  default: vi.fn(() => ({})),
}));

// Import everything under test AFTER the mocks above are registered.
import MainApp from '../src/views/MainApp.svelte';
import { resetOrchestratorStore } from '../src/stores/orchestrator.svelte.ts';
import { routerStore, navigateTo } from '../src/stores/router.svelte.ts';
import {
  authStore,
  setAuthenticated,
  setAuthStatus,
  setUnauthenticated,
  TOKEN_KEY,
} from '../src/stores/auth.svelte.ts';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Navigate the real hash router and wait for routerStore to reflect it. */
async function goto(path: string): Promise<void> {
  navigateTo(path);
  await vi.waitFor(() => {
    expect(routerStore.hash).toBe(`#${path}`);
  });
}

function expectNoNav(): void {
  expect(screen.queryByRole('navigation')).toBeNull();
  expect(document.querySelector('aside')).toBeNull();
  expect(screen.queryByLabelText(/toggle sidebar/i)).toBeNull();
  expect(screen.queryByLabelText(/active tenant/i)).toBeNull();
}

function expectNavPresent(): void {
  expect(screen.getByRole('navigation')).toBeInTheDocument();
  expect(document.querySelector('aside')).not.toBeNull();
}

describe('MainApp — app-shell auth gate (security regression)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuthValidate.mockResolvedValue(false);
    mockListTenants.mockResolvedValue({ tenants: [] });
    resetOrchestratorStore();
    setUnauthenticated();
  });

  afterEach(() => {
    cleanup();
    setUnauthenticated();
  });

  it('renders the login route WITHOUT sidebar/nav landmarks when logged out', async () => {
    await goto('/login');

    render(MainApp);

    // The auth guard resolves synchronously to 'unauthenticated' when no
    // token is stored — wait for the login card to mount as our settle point.
    await vi.waitFor(() => {
      expect(screen.getByText(/hardware-backed sign-in/i)).toBeInTheDocument();
    });

    expectNoNav();
    // No "Sign out" / "Sign in" header control either — the whole header
    // chrome is absent in the minimal shell.
    expect(screen.queryByRole('button', { name: /sign out/i })).toBeNull();
  });

  it('renders an authenticated protected route WITH sidebar/nav landmarks', async () => {
    mockAuthValidate.mockResolvedValue(true);
    await goto('/dashboard');
    setAuthenticated({ access_token: 'test-access-token' });

    render(MainApp);

    await vi.waitFor(() => {
      expect(screen.getByRole('button', { name: /sign out/i })).toBeInTheDocument();
    });

    expectNavPresent();
    expect(authStore.status).toBe('authenticated');
  });

  it('removes the sidebar/nav immediately on logout, in-tab, without a reload', async () => {
    mockAuthValidate.mockResolvedValue(true);
    await goto('/dashboard');
    setAuthenticated({ access_token: 'test-access-token' });

    render(MainApp);

    await vi.waitFor(() => {
      expect(screen.getByRole('button', { name: /sign out/i })).toBeInTheDocument();
    });
    expectNavPresent();

    await screen.getByRole('button', { name: /sign out/i }).click();

    await vi.waitFor(() => {
      expect(authStore.status).toBe('unauthenticated');
    });
    await vi.waitFor(() => {
      expectNoNav();
    });
    expect(window.localStorage.getItem(TOKEN_KEY)).toBeNull();
  });

  it('never flashes the sidebar/nav while the session is pending validation', async () => {
    // Simulate a hydrating tab: a token is on disk but the server hasn't
    // confirmed it yet (authValidate is still in flight).
    let resolveValidate: (ok: boolean) => void = () => {};
    mockAuthValidate.mockReturnValue(
      new Promise<boolean>((resolve) => { resolveValidate = resolve; }),
    );
    window.localStorage.setItem(TOKEN_KEY, 'unconfirmed-token');
    await goto('/dashboard');
    // A real cold boot starts in 'pending' (the auth store's module default)
    // before the mount effect's validateSession() call resolves either way —
    // reproduce that starting point explicitly since beforeEach normalizes to
    // 'unauthenticated' for the other cases in this suite.
    setAuthStatus('pending');

    render(MainApp);

    // Give the mount effect a tick to kick off validateSession(), but the
    // promise above is still unresolved — status must still be 'pending'.
    await vi.waitFor(() => {
      expect(mockAuthValidate).toHaveBeenCalled();
    });
    expect(authStore.status).toBe('pending');
    expectNoNav();

    // Resolve the validation as successful — the shell should now appear
    // without any additional navigation or reload.
    resolveValidate(true);
    await vi.waitFor(() => {
      expect(screen.getByRole('button', { name: /sign out/i })).toBeInTheDocument();
    });
    expectNavPresent();
  });

  it('shows the invite-activation page WITHOUT sidebar/nav landmarks', async () => {
    // BOTH SEGMENTS: the invite API is tenant-scoped, so a token issued for
    // one tenant is absent from another's store and the link has to carry the
    // tenant too (router.svelte.ts). This test asserted the tenant-less shape
    // 1f95595c removed, so it had been resolving to not-found and failing.
    await goto(
      '/invite/automatethethings/deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef',
    );

    render(MainApp);

    await vi.waitFor(() => {
      expect(screen.getByRole('heading', { name: /activate your account/i })).toBeInTheDocument();
    });

    expectNoNav();
  });
});
