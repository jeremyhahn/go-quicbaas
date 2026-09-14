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
 * MainApp.tenantSelector.test.ts
 *
 * Header tenant chip (go-quicbaas#309).
 *
 * `listTenants` is gated on `tenants:list`, which a tenant-scoped operator is
 * correctly refused. The chip used to let that refusal decide the list, so the
 * operator was shown 'system' — a tenant they are not in — with no option for
 * their own and no way to switch to it.
 *
 * The rule these pin: the session's tenant is always present and always
 * selected; `listTenants` only ADDS entries, for the principals permitted to
 * enumerate them.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, cleanup, screen } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// SDK client mock — registered before the component import so vi.mock hoisting
// applies. Mirrors MainApp.authShell.test.ts's shape.
// ---------------------------------------------------------------------------

const mockAuthValidate = vi.fn().mockResolvedValue(true);
const mockListTenants = vi.fn().mockResolvedValue({ tenants: [] });
const mockListAgents = vi.fn().mockResolvedValue({ agents: [] });
const mockListSessions = vi.fn().mockResolvedValue({ sessions: [] });
const mockListJobs = vi.fn().mockResolvedValue([]);

function makeMockClient() {
  return {
    orchestrator: {
      listJobs:           mockListJobs,
      listAgents:         mockListAgents,
      listSessions:       mockListSessions,
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
      logout:       vi.fn().mockResolvedValue(undefined),
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

// agentStore and sessionStore are deliberately NOT mocked here. They are the
// stores the tenant-switch cases below are about: mocking them away leaves
// those cases asserting only that the ORCHESTRATOR store was refreshed, which
// was already true before the fix and proves nothing about the defect.

vi.mock('../src/views/SystemDashboard.svelte', () => ({
  default: vi.fn(() => ({})),
}));

import MainApp from '../src/views/MainApp.svelte';
import { agentStore } from '../src/stores/agentStore.svelte.ts';
import { sessionStore } from '../src/stores/sessionStore.svelte.ts';
import { resetOrchestratorStore } from '../src/stores/orchestrator.svelte.ts';
import { navigateTo, routerStore } from '../src/stores/router.svelte.ts';
import { setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function sessionToken(tenantID: string, roles: string[]): string {
  const header = btoa(JSON.stringify({ alg: 'none', typ: 'at+jwt' }));
  const payload = btoa(
    JSON.stringify({
      sub: 'operator@example.test',
      uid: 'usr_01HZ',
      tenant_id: tenantID,
      roles,
      exp: Math.floor(Date.now() / 1000) + 900,
    }),
  );
  return `${header}.${payload}.sig`;
}

function signIn(tenantID: string, roles: string[] = ['operator']): void {
  setAuthenticated({ access_token: sessionToken(tenantID, roles), refresh_token: 'refresh-1' });
}

async function goto(path: string): Promise<void> {
  navigateTo(path);
  await vi.waitFor(() => {
    expect(routerStore.hash).toBe(`#${path}`);
  });
}

/** The header tenant chip, once the authenticated shell has rendered. */
async function tenantChip(): Promise<HTMLSelectElement> {
  return vi.waitFor(() => {
    const el = screen.getByLabelText(/active tenant/i);
    expect(el).toBeInTheDocument();
    return el as HTMLSelectElement;
  });
}

/** Let the scope-change listeners' unawaited refetches finish. */
async function settle(): Promise<void> {
  for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
}

function optionValues(select: HTMLSelectElement): string[] {
  return Array.from(select.options).map((o) => o.value);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('MainApp — tenant chip reflects the session, not a literal', () => {
  beforeEach(async () => {
    vi.clearAllMocks();
    mockAuthValidate.mockResolvedValue(true);
    mockListTenants.mockResolvedValue({ tenants: [] });
    mockListAgents.mockResolvedValue({ agents: [] });
    mockListSessions.mockResolvedValue({ sessions: [] });
    mockListJobs.mockResolvedValue([]);
    resetOrchestratorStore();
    setUnauthenticated();
    await goto('/dashboard');
  });

  afterEach(() => {
    cleanup();
    setUnauthenticated();
    resetOrchestratorStore();
  });

  it('shows the operator their own tenant when they may not enumerate tenants', async () => {
    // What the server actually answers a tenant-scoped principal.
    mockListTenants.mockRejectedValue(new Error('403 Forbidden: tenants:list'));
    signIn('burnin');

    render(MainApp);

    const chip = await tenantChip();
    await vi.waitFor(() => {
      expect(chip.value).toBe('burnin');
    });
    expect(optionValues(chip)).toContain('burnin');
    // The refusal must not put someone else's tenant in front of them.
    expect(optionValues(chip)).not.toContain('system');
  });

  it('offers a system admin every tenant they may list, own tenant included', async () => {
    mockListTenants.mockResolvedValue({
      tenants: [
        { id: 'system', name: 'System' },
        { id: 'burnin', name: 'Burn-in' },
        { id: 'acme', name: 'Acme' },
      ],
    });
    signIn('system', ['admin', 'operator']);

    render(MainApp);

    const chip = await tenantChip();
    await vi.waitFor(() => {
      expect(optionValues(chip)).toHaveLength(3);
    });
    expect(optionValues(chip).sort()).toEqual(['acme', 'burnin', 'system']);
    expect(chip.value).toBe('system');
  });

  it('discards a tenant listing that arrives after a different principal signs in', async () => {
    // The admin's listing is still in flight when they sign out and a
    // tenant-scoped operator signs in. Landing it now would put the admin's
    // tenant names in front of someone with no right to enumerate them.
    let release: (value: { tenants: Array<{ id: string; name: string }> }) => void = () => {};
    // The admin's call hangs; the operator's own call is refused, as the server
    // refuses it in production. Only the stale answer can supply 'acme', so the
    // assertion below fails if a stale answer is allowed to land.
    mockListTenants
      .mockImplementationOnce(
        () =>
          new Promise<{ tenants: Array<{ id: string; name: string }> }>((resolve) => {
            release = resolve;
          }),
      )
      .mockRejectedValue(new Error('403 Forbidden: tenants:list'));
    signIn('system', ['admin']);

    render(MainApp);
    const chip = await tenantChip();

    signIn('burnin');
    await vi.waitFor(() => {
      expect(chip.value).toBe('burnin');
    });

    release({
      tenants: [
        { id: 'system', name: 'System' },
        { id: 'acme', name: 'Acme' },
      ],
    });
    // Let the resolved promise, its continuation, and Svelte's effect flush all
    // run. Without this the assertion would pass simply because the stale write
    // had not happened yet, which tests nothing.
    for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));

    expect(optionValues(chip)).toEqual(['burnin']);
    expect(optionValues(chip)).not.toContain('acme');
  });

  it('keeps the session tenant listed even when listTenants omits it', async () => {
    // Defensive: a listing that answers without the caller's own tenant must
    // still leave them able to select it.
    mockListTenants.mockResolvedValue({ tenants: [{ id: 'acme', name: 'Acme' }] });
    signIn('burnin', ['admin']);

    render(MainApp);

    const chip = await tenantChip();
    await vi.waitFor(() => {
      expect(optionValues(chip)).toContain('acme');
    });
    expect(optionValues(chip)).toContain('burnin');
    expect(chip.value).toBe('burnin');
  });
});

// ---------------------------------------------------------------------------
// Switching tenant, driven through the actual <select> rather than through
// setActiveTenant(). The earlier tests here called the store directly, so they
// could not see that the handler notified only its own store.
//
// These are a FLOW check, not the proof of the mechanism. Inside a mounted
// MainApp the sibling stores also refresh by accident: refreshAgents reads
// activeTenantID synchronously, so the auxiliary $effect that calls it re-runs
// on a tenant change. These cases therefore pass even with the scope-change
// listeners removed. The guarantee itself is pinned in
// orchestratorSessionTenant.test.ts, with no component mounted, where only the
// bus can produce it.
// ---------------------------------------------------------------------------

describe('MainApp — switching tenant refills every view, not just the jobs list', () => {
  beforeEach(async () => {
    vi.clearAllMocks();
    mockAuthValidate.mockResolvedValue(true);
    mockListAgents.mockResolvedValue({ agents: [] });
    mockListSessions.mockResolvedValue({ sessions: [] });
    mockListJobs.mockResolvedValue([]);
    mockListTenants.mockResolvedValue({
      tenants: [
        { id: 'system', name: 'System' },
        { id: 'burnin', name: 'Burn-in' },
      ],
    });
    resetOrchestratorStore();
    setUnauthenticated();
    await goto('/dashboard');
  });

  afterEach(() => {
    cleanup();
    setUnauthenticated();
    resetOrchestratorStore();
  });

  it('asks every store for the newly selected tenant', async () => {
    signIn('system', ['admin']);
    render(MainApp);
    const chip = await tenantChip();
    await vi.waitFor(() => {
      expect(optionValues(chip)).toContain('burnin');
    });

    // Everything the login itself triggered is not evidence about the switch.
    await settle();
    mockListAgents.mockClear();
    mockListSessions.mockClear();
    mockListJobs.mockClear();

    // Drive the real control.
    chip.value = 'burnin';
    chip.dispatchEvent(new Event('change', { bubbles: true }));
    await settle();

    // The orchestrator store was always refreshed by the handler...
    expect(mockListJobs).toHaveBeenCalledWith('burnin');
    // ...and these two were the ones left waiting on the 30-second poll.
    expect(mockListAgents).toHaveBeenCalledWith(
      expect.objectContaining({ tenant_id: 'burnin' }),
    );
    expect(mockListSessions).toHaveBeenCalledWith(
      expect.objectContaining({ tenantID: 'burnin' }),
    );
  });

  it('shows the new tenant\'s fleet and sessions rather than blanking them', async () => {
    signIn('system', ['admin']);
    render(MainApp);
    const chip = await tenantChip();
    await settle();

    mockListAgents.mockResolvedValue({
      agents: [{ id: 'agent-burnin', tenant_id: 'burnin', status: 'running' }],
    });
    mockListSessions.mockResolvedValue({
      sessions: [{ session_id: 'sess-burnin', backend: 'claude' }],
    });

    chip.value = 'burnin';
    chip.dispatchEvent(new Event('change', { bubbles: true }));
    await settle();

    // Not empty: the switch refilled them, in the same tick rather than 30
    // seconds later.
    expect(agentStore.agents.map((a) => a.id)).toEqual(['agent-burnin']);
    expect(sessionStore.sessions.map((x) => x.session_id)).toEqual(['sess-burnin']);
  });
});
