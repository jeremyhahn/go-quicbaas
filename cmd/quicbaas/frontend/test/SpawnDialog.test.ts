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
import type { AgentBackendInfo } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const { mockGetSDKClient, mockListBackends } = vi.hoisted(() => ({
  mockGetSDKClient: vi.fn(),
  mockListBackends: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
}));

vi.mock('../src/stores/agentStore.svelte.ts', () => ({
  agentStore: { onlineAgents: [] },
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeSessionDetail: vi.fn(),
  routerStore: { route: { name: 'sessions', params: {}, hash: '#/sessions' } },
}));

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  refreshSessions: vi.fn().mockResolvedValue(undefined),
}));

import SpawnDialog from '../src/components/orchestrator/SpawnDialog.svelte';

function makeClient(backends: AgentBackendInfo[]): unknown {
  mockListBackends.mockResolvedValue(backends);
  return {
    orchestrator: {
      listBackends: mockListBackends,
      spawnSession: vi.fn().mockResolvedValue({ session_id: 'sess-1' }),
      promptSession: vi.fn().mockResolvedValue(undefined),
    },
  };
}

describe('SpawnDialog — bug #59 backend pre-population', () => {
  beforeEach(() => {
    vi.clearAllMocks();
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
    mockGetSDKClient.mockResolvedValue({
      orchestrator: {
        listBackends: mockListBackends,
        spawnSession: mockSpawn,
        promptSession: vi.fn().mockResolvedValue(undefined),
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
