/**
 * SpawnDialog.test.ts
 *
 * Bug #59 regression: the Backend dropdown must pre-populate from the
 * orchestrator's registered backends list and default to the first
 * REGISTERED backend — never a hardcoded "claude (acp)" fallback. When zero
 * backends are registered, the dialog renders a "No backends registered"
 * banner and disables the submit button.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { AgentBackendInfo, Project, Account } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const { mockGetSDKClient, mockListBackends, mockListProjects, mockListAccounts } = vi.hoisted(() => ({
  mockGetSDKClient: vi.fn(),
  mockListBackends: vi.fn(),
  mockListProjects: vi.fn(),
  mockListAccounts: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
}));

vi.mock('../src/stores/fleetStore.svelte.ts', () => ({
  fleetStore: { onlineNodes: [] },
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeSessionDetail: vi.fn(),
  routerStore: { route: { name: 'sessions', params: {}, hash: '#/sessions' } },
}));

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  refreshSessions: vi.fn().mockResolvedValue(undefined),
}));

import SpawnDialog from '../src/components/orchestrator/SpawnDialog.svelte';

function makeClient(backends: AgentBackendInfo[], projects: Project[] = [], accounts: Account[] = []): unknown {
  mockListBackends.mockResolvedValue(backends);
  mockListProjects.mockResolvedValue({ projects, total: projects.length });
  mockListAccounts.mockResolvedValue({ accounts, total: accounts.length });
  return {
    orchestrator: {
      listBackends: mockListBackends,
      spawnSession: vi.fn().mockResolvedValue({ session_id: 'sess-1' }),
      promptSession: vi.fn().mockResolvedValue(undefined),
    },
    projects: {
      listProjects: mockListProjects,
    },
    accounts: {
      listAccounts: mockListAccounts,
    },
  };
}

describe('SpawnDialog — bug #59 backend pre-population', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListProjects.mockResolvedValue({ projects: [] });
    mockListAccounts.mockResolvedValue({ accounts: [], total: 0 });
  });

  it('defaults the Backend dropdown to the first REGISTERED backend (ollama, not claude)', async () => {
    const backends: AgentBackendInfo[] = [
      { name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo,
    ];
    mockGetSDKClient.mockResolvedValue(makeClient(backends));

    render(SpawnDialog, { props: { open: true } });

    // Wait for the backends to load and the select to render.
    const select = (await waitFor(() =>
      screen.getByLabelText(/Select backend/i),
    )) as HTMLSelectElement;
    expect(select.value).toBe('ollama');
    expect(select.value).not.toBe('claude');

    // The select should contain only the registered options.
    const options = Array.from(select.querySelectorAll('option')).map((o) => o.value);
    expect(options).toEqual(['ollama']);
  });

  it('renders "No backends registered" banner and disables Spawn when zero are registered', async () => {
    mockGetSDKClient.mockResolvedValue(makeClient([]));
    const { container } = render(SpawnDialog, { props: { open: true } });

    // Banner appears.
    const banner = await waitFor(() => screen.getByTestId('no-backends-banner'));
    expect(banner).toBeInTheDocument();
    expect(banner.textContent).toMatch(/No backends registered/i);

    // Spawn submit button must be disabled. Native <dialog> hides its
    // contents from the a11y tree when not open via showModal(), so query
    // through the rendered DOM directly.
    const spawnBtn = container.querySelector(
      'button[aria-label="Spawn session"]',
    ) as HTMLButtonElement | null;
    expect(spawnBtn).not.toBeNull();
    expect(spawnBtn?.disabled).toBe(true);
  });

  it('uses the first backend when multiple are registered (in registration order)', async () => {
    const backends: AgentBackendInfo[] = [
      { name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo,
      { name: 'codex', type: 'acp', healthy: true } as AgentBackendInfo,
    ];
    mockGetSDKClient.mockResolvedValue(makeClient(backends));
    render(SpawnDialog, { props: { open: true } });

    const select = (await waitFor(() =>
      screen.getByLabelText(/Select backend/i),
    )) as HTMLSelectElement;
    expect(select.value).toBe('ollama');
  });
});

describe('SpawnDialog — QuicBaaS MCP endpoint preset', () => {
  const mockSpawn = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockSpawn.mockResolvedValue({ session_id: 'sess-mcp' });
    mockListBackends.mockResolvedValue([
      { name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo,
    ]);
    mockListProjects.mockResolvedValue({ projects: [] });
    mockListAccounts.mockResolvedValue({ accounts: [], total: 0 });
    mockGetSDKClient.mockResolvedValue({
      orchestrator: {
        listBackends: mockListBackends,
        spawnSession: mockSpawn,
        promptSession: vi.fn().mockResolvedValue(undefined),
      },
      projects: {
        listProjects: mockListProjects,
      },
      accounts: {
        listAccounts: mockListAccounts,
      },
    });
  });

  async function openAndSpawn(container: HTMLElement, attachMCP: boolean): Promise<void> {
    // Wait for the backend select so the form is fully mounted.
    await waitFor(() => screen.getByLabelText(/Select backend/i));
    if (attachMCP) {
      const checkbox = screen.getByLabelText(/Attach QuicBaaS MCP endpoint/i) as HTMLInputElement;
      await fireEvent.click(checkbox);
    }
    const spawnBtn = container.querySelector(
      'button[aria-label="Spawn session"]',
    ) as HTMLButtonElement;
    await fireEvent.click(spawnBtn);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
  }

  it('omits mcp_servers when the preset is not selected', async () => {
    const { container } = render(SpawnDialog, { props: { open: true } });
    await openAndSpawn(container, false);
    const arg = mockSpawn.mock.calls[0][0] as { mcp_servers?: unknown };
    expect(arg.mcp_servers).toBeUndefined();
  });

  it('sends the quicbaas MCP server when the preset is selected', async () => {
    const { container } = render(SpawnDialog, { props: { open: true } });
    await openAndSpawn(container, true);
    const arg = mockSpawn.mock.calls[0][0] as {
      mcp_servers?: Array<{ name: string; url?: string }>;
    };
    expect(arg.mcp_servers).toHaveLength(1);
    expect(arg.mcp_servers?.[0]?.name).toBe('quicbaas');
    // The SPA leaves url empty so the daemon resolves its own MCP endpoint and
    // injects the agent's cert/key material — the browser never has to know the
    // operator's MCP host/port.
    expect(arg.mcp_servers?.[0]?.url).toBe('');
  });

  it('checkbox is present and unchecked by default (opt-in)', async () => {
    render(SpawnDialog, { props: { open: true } });
    const checkbox = (await waitFor(() =>
      screen.getByLabelText(/Attach QuicBaaS MCP endpoint/i),
    )) as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
  });
});

describe('SpawnDialog — project selector', () => {
  const mockSpawn = vi.fn();
  const projects: Project[] = [
    { id: 'proj-1', tenant_id: 'default', name: 'alpha', workspace_path: '/work/alpha' } as Project,
    { id: 'proj-2', tenant_id: 'default', name: 'beta' } as Project,
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    mockSpawn.mockResolvedValue({ session_id: 'sess-proj' });
    mockListBackends.mockResolvedValue([
      { name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo,
    ]);
    mockListProjects.mockResolvedValue({ projects, total: projects.length });
    mockListAccounts.mockResolvedValue({ accounts: [], total: 0 });
    mockGetSDKClient.mockResolvedValue({
      orchestrator: {
        listBackends: mockListBackends,
        spawnSession: mockSpawn,
        promptSession: vi.fn().mockResolvedValue(undefined),
      },
      projects: {
        listProjects: mockListProjects,
      },
      accounts: {
        listAccounts: mockListAccounts,
      },
    });
  });

  it('populates the project select with "None" plus each loaded project', async () => {
    render(SpawnDialog, { props: { open: true } });
    const select = screen.getByLabelText(/Select project/i) as HTMLSelectElement;
    await waitFor(() => {
      const options = Array.from(select.querySelectorAll('option')).map((o) => o.textContent);
      expect(options).toEqual(['None', 'alpha', 'beta']);
    });
  });

  it('omits project_id from the spawn request when "None" is selected', async () => {
    const { container } = render(SpawnDialog, { props: { open: true } });
    const select = screen.getByLabelText(/Select project/i) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option').length).toBe(3));
    const spawnBtn = container.querySelector(
      'button[aria-label="Spawn session"]',
    ) as HTMLButtonElement;
    await fireEvent.click(spawnBtn);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
    const arg = mockSpawn.mock.calls[0][0] as { project_id?: string };
    expect(arg.project_id).toBeUndefined();
  });

  it('sends project_id and shows the workspace path hint when a project is selected', async () => {
    const { container } = render(SpawnDialog, { props: { open: true } });
    const select = screen.getByLabelText(/Select project/i) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option').length).toBe(3));
    await fireEvent.change(select, { target: { value: 'proj-1' } });

    await waitFor(() => expect(screen.getByText(/\/work\/alpha/)).toBeInTheDocument());

    const spawnBtn = container.querySelector(
      'button[aria-label="Spawn session"]',
    ) as HTMLButtonElement;
    await fireEvent.click(spawnBtn);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
    const arg = mockSpawn.mock.calls[0][0] as { project_id?: string };
    expect(arg.project_id).toBe('proj-1');
  });

  it('renders gracefully with only "None" when listProjects fails', async () => {
    mockListProjects.mockRejectedValue(new Error('boom'));
    render(SpawnDialog, { props: { open: true } });
    const select = (await waitFor(() =>
      screen.getByLabelText(/Select project/i),
    )) as HTMLSelectElement;
    const options = Array.from(select.querySelectorAll('option')).map((o) => o.textContent);
    expect(options).toEqual(['None']);
  });
});

describe('SpawnDialog — account selector', () => {
  const mockSpawn = vi.fn();
  const accounts: Account[] = [
    { id: 'acct-claude', tenant_id: 'default', provider: 'claude', display_name: 'alice-claude', default: true, status: 'active', refresh_policy: 'oauth_refresh', created_at: '', updated_at: '' } as Account,
    { id: 'acct-codex', tenant_id: 'default', provider: 'codex', display_name: 'bob-codex', default: false, status: 'active', refresh_policy: 'static_token', created_at: '', updated_at: '' } as Account,
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    mockSpawn.mockResolvedValue({ session_id: 'sess-acct' });
    mockListProjects.mockResolvedValue({ projects: [] });
    mockListAccounts.mockResolvedValue({ accounts, total: accounts.length });
    mockGetSDKClient.mockResolvedValue({
      orchestrator: {
        listBackends: mockListBackends,
        spawnSession: mockSpawn,
        promptSession: vi.fn().mockResolvedValue(undefined),
      },
      projects: {
        listProjects: mockListProjects,
      },
      accounts: {
        listAccounts: mockListAccounts,
      },
    });
  });

  it('offers Unset, Automatic, plus every account for a backend with no known provider mapping', async () => {
    mockListBackends.mockResolvedValue([{ name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo]);
    render(SpawnDialog, { props: { open: true } });
    const select = (await waitFor(() => screen.getByLabelText(/Select account/i))) as HTMLSelectElement;
    await waitFor(() => {
      const labels = Array.from(select.querySelectorAll('option')).map((o) => o.textContent);
      expect(labels).toEqual([
        'Unset (backend default)',
        'Automatic (strategy)',
        'alice-claude — Claude (Anthropic)',
        'bob-codex — Codex (OpenAI)',
      ]);
    });
  });

  it('filters the account list to the matching provider for a known backend name (claude)', async () => {
    mockListBackends.mockResolvedValue([{ name: 'claude', type: 'acp', healthy: true } as AgentBackendInfo]);
    render(SpawnDialog, { props: { open: true } });
    const select = (await waitFor(() => screen.getByLabelText(/Select account/i))) as HTMLSelectElement;
    await waitFor(() => {
      const labels = Array.from(select.querySelectorAll('option')).map((o) => o.textContent);
      expect(labels).toEqual(['Unset (backend default)', 'Automatic (strategy)', 'alice-claude — Claude (Anthropic)']);
      expect(labels).not.toContain('bob-codex — Codex (OpenAI)');
    });
  });

  it('sends neither account_id nor account_auto when unset', async () => {
    mockListBackends.mockResolvedValue([{ name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo]);
    const { container } = render(SpawnDialog, { props: { open: true } });
    // Wait for the backend select to finish loading — the Account select
    // renders immediately (unlike Backend, which shows a loading placeholder
    // until the fetch resolves), so waiting on it alone would race the Spawn
    // button's disabled={backends.length === 0} state.
    await waitFor(() => screen.getByLabelText(/Select backend/i));
    const select = (await waitFor(() => screen.getByLabelText(/Select account/i))) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option').length).toBe(4));
    const spawnBtn = container.querySelector('button[aria-label="Spawn session"]') as HTMLButtonElement;
    await fireEvent.click(spawnBtn);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
    const arg = mockSpawn.mock.calls[0][0] as { account_id?: string; account_auto?: boolean; account_strategy?: string };
    expect(arg.account_id).toBeUndefined();
    expect(arg.account_auto).toBeUndefined();
    expect(arg.account_strategy).toBeUndefined();
  });

  it('sends account_id when a specific account is selected', async () => {
    mockListBackends.mockResolvedValue([{ name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo]);
    const { container } = render(SpawnDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Select backend/i));
    const select = (await waitFor(() => screen.getByLabelText(/Select account/i))) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option').length).toBe(4));
    await fireEvent.change(select, { target: { value: 'acct-claude' } });
    const spawnBtn = container.querySelector('button[aria-label="Spawn session"]') as HTMLButtonElement;
    await fireEvent.click(spawnBtn);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
    const arg = mockSpawn.mock.calls[0][0] as { account_id?: string; account_auto?: boolean };
    expect(arg.account_id).toBe('acct-claude');
    expect(arg.account_auto).toBeUndefined();
  });

  it('sends account_auto plus account_strategy when Automatic + a strategy are selected', async () => {
    mockListBackends.mockResolvedValue([{ name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo]);
    const { container } = render(SpawnDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Select backend/i));
    const select = (await waitFor(() => screen.getByLabelText(/Select account/i))) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option').length).toBe(4));
    await fireEvent.change(select, { target: { value: '__auto__' } });
    const strategySelect = (await waitFor(() =>
      screen.getByLabelText(/Select account strategy/i),
    )) as HTMLSelectElement;
    await fireEvent.change(strategySelect, { target: { value: 'least_usage' } });
    const spawnBtn = container.querySelector('button[aria-label="Spawn session"]') as HTMLButtonElement;
    await fireEvent.click(spawnBtn);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
    const arg = mockSpawn.mock.calls[0][0] as { account_id?: string; account_auto?: boolean; account_strategy?: string };
    expect(arg.account_id).toBeUndefined();
    expect(arg.account_auto).toBe(true);
    expect(arg.account_strategy).toBe('least_usage');
  });

  it('renders gracefully with only Unset/Automatic when listAccounts fails', async () => {
    mockListBackends.mockResolvedValue([{ name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo]);
    mockListAccounts.mockRejectedValue(new Error('boom'));
    render(SpawnDialog, { props: { open: true } });
    const select = (await waitFor(() => screen.getByLabelText(/Select account/i))) as HTMLSelectElement;
    const labels = Array.from(select.querySelectorAll('option')).map((o) => o.textContent);
    expect(labels).toEqual(['Unset (backend default)', 'Automatic (strategy)']);
  });
});

describe('SpawnDialog — account_allow rotation list', () => {
  const mockSpawn = vi.fn();
  const accounts: Account[] = [
    { id: 'acct-claude', tenant_id: 'default', provider: 'claude', display_name: 'alice-claude', default: true, status: 'active', refresh_policy: 'oauth_refresh', created_at: '', updated_at: '' } as Account,
    { id: 'acct-claude-2', tenant_id: 'default', provider: 'claude', display_name: 'bob-claude', default: false, status: 'active', refresh_policy: 'oauth_refresh', created_at: '', updated_at: '' } as Account,
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    mockSpawn.mockResolvedValue({ session_id: 'sess-allow' });
    mockListBackends.mockResolvedValue([{ name: 'ollama', type: 'native', healthy: true } as AgentBackendInfo]);
    mockListProjects.mockResolvedValue({ projects: [] });
    mockListAccounts.mockResolvedValue({ accounts, total: accounts.length });
    mockGetSDKClient.mockResolvedValue({
      orchestrator: {
        listBackends: mockListBackends,
        spawnSession: mockSpawn,
        promptSession: vi.fn().mockResolvedValue(undefined),
      },
      projects: {
        listProjects: mockListProjects,
      },
      accounts: {
        listAccounts: mockListAccounts,
      },
    });
  });

  async function selectAccountDropdown(value: string): Promise<HTMLSelectElement> {
    await waitFor(() => screen.getByLabelText(/Select backend/i));
    const select = (await waitFor(() => screen.getByLabelText(/Select account/i))) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option').length).toBeGreaterThan(1));
    await fireEvent.change(select, { target: { value } });
    return select;
  }

  it('does not show the allow-list or the not-rotatable hint when unset', async () => {
    render(SpawnDialog, { props: { open: true } });
    await selectAccountDropdown('');
    expect(screen.queryByTestId('account-allow-list')).toBeNull();
    expect(screen.queryByTestId('account-not-rotatable-hint')).toBeNull();
  });

  it('does not show the allow-list or the not-rotatable hint for Automatic selection', async () => {
    render(SpawnDialog, { props: { open: true } });
    await selectAccountDropdown('__auto__');
    expect(screen.queryByTestId('account-allow-list')).toBeNull();
    expect(screen.queryByTestId('account-not-rotatable-hint')).toBeNull();
  });

  it('shows the not-rotatable hint for a bare pin with no allow-list entries checked', async () => {
    render(SpawnDialog, { props: { open: true } });
    await selectAccountDropdown('acct-claude');
    expect(await screen.findByTestId('account-not-rotatable-hint')).toBeInTheDocument();
  });

  it('shows the allow-list checkboxes once a specific account is pinned', async () => {
    render(SpawnDialog, { props: { open: true } });
    await selectAccountDropdown('acct-claude');
    const list = await screen.findByTestId('account-allow-list');
    expect(list).toBeInTheDocument();
    expect(screen.getByLabelText('Allow rotation to alice-claude')).toBeInTheDocument();
    expect(screen.getByLabelText('Allow rotation to bob-claude')).toBeInTheDocument();
  });

  it('clears the not-rotatable hint once an allow-list account is checked', async () => {
    render(SpawnDialog, { props: { open: true } });
    await selectAccountDropdown('acct-claude');
    await screen.findByTestId('account-not-rotatable-hint');
    const checkbox = screen.getByLabelText('Allow rotation to bob-claude') as HTMLInputElement;
    await fireEvent.click(checkbox);
    expect(screen.queryByTestId('account-not-rotatable-hint')).toBeNull();
  });

  it('omits account_allow from the spawn request when no accounts are checked', async () => {
    const { container } = render(SpawnDialog, { props: { open: true } });
    await selectAccountDropdown('acct-claude');
    const spawnBtn = container.querySelector('button[aria-label="Spawn session"]') as HTMLButtonElement;
    await fireEvent.click(spawnBtn);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
    const arg = mockSpawn.mock.calls[0][0] as { account_allow?: string[] };
    expect(arg.account_allow).toBeUndefined();
  });

  it('sends account_allow with the checked account IDs when populated', async () => {
    const { container } = render(SpawnDialog, { props: { open: true } });
    await selectAccountDropdown('acct-claude');
    const checkbox = screen.getByLabelText('Allow rotation to bob-claude') as HTMLInputElement;
    await fireEvent.click(checkbox);
    const spawnBtn = container.querySelector('button[aria-label="Spawn session"]') as HTMLButtonElement;
    await fireEvent.click(spawnBtn);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
    const arg = mockSpawn.mock.calls[0][0] as { account_id?: string; account_allow?: string[] };
    expect(arg.account_id).toBe('acct-claude');
    expect(arg.account_allow).toEqual(['acct-claude-2']);
  });

  it('omits account_allow when the operator switches back to Automatic after checking accounts', async () => {
    const { container } = render(SpawnDialog, { props: { open: true } });
    const select = await selectAccountDropdown('acct-claude');
    const checkbox = screen.getByLabelText('Allow rotation to bob-claude') as HTMLInputElement;
    await fireEvent.click(checkbox);
    await fireEvent.change(select, { target: { value: '__auto__' } });
    const spawnBtn = container.querySelector('button[aria-label="Spawn session"]') as HTMLButtonElement;
    await fireEvent.click(spawnBtn);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
    const arg = mockSpawn.mock.calls[0][0] as { account_auto?: boolean; account_allow?: string[] };
    expect(arg.account_auto).toBe(true);
    expect(arg.account_allow).toBeUndefined();
  });
});

describe('SpawnDialog — run-time prompt material', () => {
  const mockSpawn = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockSpawn.mockResolvedValue({ session_id: 'sess-append' });
    mockListBackends.mockResolvedValue([{ name: 'claude', type: 'acp', healthy: true } as AgentBackendInfo]);
    mockListProjects.mockResolvedValue({ projects: [] });
    mockListAccounts.mockResolvedValue({ accounts: [], total: 0 });
    mockGetSDKClient.mockResolvedValue({
      orchestrator: { listBackends: mockListBackends, spawnSession: mockSpawn, promptSession: vi.fn() },
      projects: { listProjects: mockListProjects },
      accounts: { listAccounts: mockListAccounts },
    });
  });

  async function spawn(container: HTMLElement): Promise<Record<string, unknown>> {
    await fireEvent.click(container.querySelector('button[aria-label="Spawn session"]') as HTMLButtonElement);
    await waitFor(() => expect(mockSpawn).toHaveBeenCalled());
    return mockSpawn.mock.calls[0][0] as Record<string, unknown>;
  }

  it('sends what the operator typed as prompt_append', async () => {
    const { container } = render(SpawnDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Select backend/i));
    await fireEvent.input(screen.getByLabelText(/Prompt to append/i), { target: { value: 'work on pkg/cache only' } });
    expect((await spawn(container)).prompt_append).toBe('work on pkg/cache only');
  });

  it('sends no prompt_append when the field is blank', async () => {
    const { container } = render(SpawnDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Select backend/i));
    await fireEvent.input(screen.getByLabelText(/Prompt to append/i), { target: { value: '   ' } });
    expect((await spawn(container)).prompt_append).toBeUndefined();
  });
});
