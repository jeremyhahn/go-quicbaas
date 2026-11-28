/**
 * Copyright (c) 2026 Jeremy Hahn
 * Copyright (c) 2026 Automate The Things, LLC
 *
 * This file is part of go-quicbaas.
 *
 * go-quicbaas is dual-licensed:
 *
 * 1. GNU Affero General Public License v3.0 (AGPL-3.0)
 * 2. Commercial License
 */

/**
 * MainApp.desktopRoutes.test.ts
 *
 * The security pages that call the SDK's iam or attestation client are
 * web-only: the desktop (Wails) transport does not provide those clients, so
 * on desktop the navigation disables them and a direct route shows a notice
 * instead of mounting a page that would fail. My Sessions is withheld on every
 * runtime: its route falls through to the Coming Soon placeholder.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, cleanup, screen } from '@testing-library/svelte';

const runtime = vi.hoisted(() => ({ wails: false }));

const mockAuthValidate = vi.fn().mockResolvedValue(true);
const mockLogout = vi.fn().mockResolvedValue(undefined);
const mockListTenants = vi.fn().mockResolvedValue({ tenants: [] });

function makeMockClient() {
  return {
    orchestrator: {
      listJobs:           vi.fn().mockResolvedValue([]),
      listFleetNodes:     vi.fn().mockResolvedValue({ nodes: [] }),
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
  return { ...actual, isWailsRuntime: vi.fn(() => runtime.wails) };
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

// The web case mounts OAuth Apps; stubbed so it sends no IAM traffic.
vi.mock('../src/views/security/OAuthAppsView.svelte', () => ({
  default: vi.fn(() => ({})),
}));

import MainApp from '../src/views/MainApp.svelte';
import { resetOrchestratorStore } from '../src/stores/orchestrator.svelte.ts';
import { routerStore, navigateTo } from '../src/stores/router.svelte.ts';
import { setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';

async function goto(path: string): Promise<void> {
  navigateTo(path);
  await vi.waitFor(() => {
    expect(routerStore.hash).toBe(`#${path}`);
  });
}

function navButton(label: string): HTMLElement {
  const nav = screen.getByRole('navigation');
  const button = Array.from(nav.querySelectorAll('button')).find((b) => b.textContent?.includes(label));
  expect(button).toBeDefined();
  return button as HTMLElement;
}

describe('MainApp — web-only security routes and withheld My Sessions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuthValidate.mockResolvedValue(true);
    mockListTenants.mockResolvedValue({ tenants: [] });
    resetOrchestratorStore();
  });

  afterEach(() => {
    cleanup();
    setUnauthenticated();
    runtime.wails = false;
  });

  it('disables a web-only page on desktop and shows a notice on its direct route', async () => {
    runtime.wails = true;
    await goto('/oauth-apps');
    setAuthenticated({ access_token: 'test-access-token' });
    render(MainApp);

    await vi.waitFor(() => {
      expect(screen.getByRole('navigation')).toBeInTheDocument();
    });
    const button = navButton('OAuth Apps');
    expect(button).toHaveClass('opacity-40');
    expect(button.textContent).toContain('soon');
    expect(screen.getByTestId('web-only-notice').textContent).toContain('OAuth Apps');
  });

  it('disables the Key Store on desktop, where the transport has no xkms clients', async () => {
    runtime.wails = true;
    await goto('/xkms-keys');
    setAuthenticated({ access_token: 'test-access-token' });
    render(MainApp);

    await vi.waitFor(() => {
      expect(screen.getByRole('navigation')).toBeInTheDocument();
    });
    const button = navButton('Key Store');
    expect(button).toHaveClass('opacity-40');
    expect(button.textContent).toContain('soon');
    expect(screen.getByTestId('web-only-notice').textContent).toContain('Key Store');
  });

  it('keeps the web-only page enabled in the web console', async () => {
    runtime.wails = false;
    await goto('/oauth-apps');
    setAuthenticated({ access_token: 'test-access-token' });
    render(MainApp);

    await vi.waitFor(() => {
      expect(screen.getByRole('navigation')).toBeInTheDocument();
    });
    expect(navButton('OAuth Apps')).not.toHaveClass('opacity-40');
    expect(screen.queryByTestId('web-only-notice')).toBeNull();
  });

  it('withholds My Sessions: the nav item is not offered and the route shows Coming Soon', async () => {
    runtime.wails = false;
    await goto('/my-sessions');
    setAuthenticated({ access_token: 'test-access-token' });
    render(MainApp);

    await vi.waitFor(() => {
      expect(screen.getByRole('navigation')).toBeInTheDocument();
    });
    expect(navButton('My Sessions').textContent).toContain('soon');
    expect(screen.getByText('This feature is under development')).toBeInTheDocument();
  });
});
