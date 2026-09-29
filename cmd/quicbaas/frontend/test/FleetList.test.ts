/**
 * FleetList.test.ts
 *
 * Regression tests for the fleet card rendering:
 *
 * - The runtime backend pill (ollama / claude / codex) must use a neutral
 *   ghost badge, never a destructive/brand-red style.
 *
 * - Issue #78 regression: The card was displaying "ollama" twice — once as
 *   the backend badge and once in the model label — because the model field
 *   was either set to the backend name or incorrectly rendering the backend
 *   value. The model label must render the actual model string (e.g.
 *   "qwen2.5:0.5b") and the backend badge must render only the backend name.
 *   When model is absent the model label must display "—".
 */

import { describe, it, expect, vi } from 'vitest';
import { render, waitFor } from '@testing-library/svelte';
import FleetList from '../src/components/orchestrator/FleetList.svelte';

// ---------------------------------------------------------------------------
// Mocks — must be self-contained; vi.hoisted is evaluated before imports.
// ---------------------------------------------------------------------------

const { fleetStoreMock, agentWithModel, agentNoModel } = vi.hoisted(() => {
  const agentWithModel = {
    id: 'agent-ollama-1',
    tenant_id: 'default',
    name: 'embedded-agent-02',
    host: 'gpu-node-01.example.com',
    backend: 'ollama',
    model: 'qwen2.5:0.5b',
    node_type: 'inference',
    status: 'running',
    capabilities: ['inference'],
    work_dir: '',
    last_seen: Math.floor(Date.now() / 1000) - 5,
    registered_at: 1700000000,
    version: 1,
    metadata: {},
  };
  const agentNoModel = {
    ...agentWithModel,
    id: 'agent-ollama-2',
    model: '',
  };
  return {
    fleetStoreMock: {
      nodes: [agentWithModel],
      nodesLoading: false,
      onlineNodes: [agentWithModel],
    },
    agentWithModel,
    agentNoModel,
  };
});

vi.mock('../src/stores/fleetStore.svelte.ts', () => ({
  fleetStore: fleetStoreMock,
  refreshFleet: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: {
    activeTenantID: 'default',
  },
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeFleetNode: vi.fn(),
  navigateTo: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    connection: { getTransport: () => 'rest' },
    orchestrator: {
      removeFleetNode: vi.fn().mockResolvedValue({}),
    },
  }),
}));

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('FleetList', () => {
  it('renders the runtime badge as a non-destructive (ghost) pill', async () => {
    const { container } = render(FleetList);

    const badge = await waitFor(() => {
      const el = container.querySelector('[data-testid="agent-runtime-badge"]');
      if (el === null) throw new Error('runtime badge not yet rendered');
      return el as HTMLElement;
    });

    expect(badge.textContent?.trim()).toBe('ollama');

    const cls = badge.className;
    // Destructive / brand-red classes must NOT be applied.
    expect(cls).not.toMatch(/\bbadge-primary\b/);
    expect(cls).not.toMatch(/\bbadge-error\b/);
    expect(cls).not.toMatch(/\bbg-red-/);
    expect(cls).not.toMatch(/\bbg-error\b/);

    // The badge must use a neutral style hook.
    expect(cls).toMatch(/\bbadge-ghost\b|\bbadge-neutral\b/);
  });

  // ---------------------------------------------------------------------------
  // Issue #78 regression: backend and model must each appear exactly once.
  // When model is present the model label shows the model string, not the
  // backend name.  When model is absent the label shows "—".
  // ---------------------------------------------------------------------------

  it('#78: renders backend badge and model label as distinct values', async () => {
    // Store has agentWithModel: backend='ollama', model='qwen2.5:0.5b'
    const { container } = render(FleetList);

    const badge = await waitFor(() => {
      const el = container.querySelector('[data-testid="agent-runtime-badge"]');
      if (el === null) throw new Error('runtime badge not yet rendered');
      return el as HTMLElement;
    });
    const modelLabel = container.querySelector('[data-testid="agent-model-label"]') as HTMLElement;

    expect(badge.textContent?.trim()).toBe('ollama');
    expect(modelLabel.textContent?.trim()).toBe('qwen2.5:0.5b');

    // "ollama" must appear exactly once IN THE CARD — the backend badge.
    //
    // The count is taken over the grid and not the page, because the backend
    // facet above it is also entitled to the word: a chip offering "ollama 1"
    // is the control for finding this agent, not a second copy of its model.
    const card = badge.closest('.card') as HTMLElement;
    const cardText = card.textContent ?? '';
    const ollamaOccurrences = (cardText.match(/\bollama\b/g) ?? []).length;
    // Agent name contains 'embedded-agent-02', status 'running', etc. —
    // "ollama" must appear only in the backend badge, not also in the model label.
    expect(ollamaOccurrences).toBe(1);
  });

  it('#78: renders "—" in model label when model is empty', async () => {
    // Temporarily replace the store data with an agent that has no model.
    fleetStoreMock.nodes = [agentNoModel];
    fleetStoreMock.onlineNodes = [agentNoModel];

    const { container } = render(FleetList);

    const modelLabel = await waitFor(() => {
      const el = container.querySelector('[data-testid="agent-model-label"]');
      if (el === null) throw new Error('model label not yet rendered');
      return el as HTMLElement;
    });

    expect(modelLabel.textContent?.trim()).toBe('—');

    // Restore for subsequent tests.
    fleetStoreMock.nodes = [agentWithModel];
    fleetStoreMock.onlineNodes = [agentWithModel];
  });

  // ---------------------------------------------------------------------------
  // Issue #100: Backend / Model / Runtime pills must each have an inline label
  // ---------------------------------------------------------------------------

  it('#100: renders "Backend:" label exactly once per card', async () => {
    const { container } = render(FleetList);

    await waitFor(() => {
      const el = container.querySelector('[data-testid="agent-backend-label"]');
      if (!el) throw new Error('Backend label not rendered');
    });

    const backendLabels = container.querySelectorAll('[data-testid="agent-backend-label"]');
    // One agent in the store → exactly one card → exactly one Backend label.
    expect(backendLabels.length).toBe(1);
    expect(backendLabels[0]!.textContent?.toLowerCase()).toContain('backend');
  });

  it('#100: renders "Model:" label exactly once per card', async () => {
    const { container } = render(FleetList);

    await waitFor(() => {
      const el = container.querySelector('[data-testid="agent-model-dt-label"]');
      if (!el) throw new Error('Model label not rendered');
    });

    const modelLabels = container.querySelectorAll('[data-testid="agent-model-dt-label"]');
    expect(modelLabels.length).toBe(1);
    expect(modelLabels[0]!.textContent?.toLowerCase()).toContain('model');
  });

  it('#100: renders "Runtime:" label exactly once per card', async () => {
    const { container } = render(FleetList);

    await waitFor(() => {
      const el = container.querySelector('[data-testid="agent-runtime-dt-label"]');
      if (!el) throw new Error('Runtime label not rendered');
    });

    const runtimeLabels = container.querySelectorAll('[data-testid="agent-runtime-dt-label"]');
    expect(runtimeLabels.length).toBe(1);
    expect(runtimeLabels[0]!.textContent?.toLowerCase()).toContain('runtime');
  });

  it('#100: all three labels are distinct (no label text shared between Backend/Model/Runtime)', async () => {
    const { container } = render(FleetList);

    await waitFor(() => {
      const el = container.querySelector('[data-testid="agent-backend-label"]');
      if (!el) throw new Error('labels not yet rendered');
    });

    const backendText = (container.querySelector('[data-testid="agent-backend-label"]') as HTMLElement).textContent ?? '';
    const modelText = (container.querySelector('[data-testid="agent-model-dt-label"]') as HTMLElement).textContent ?? '';
    const runtimeText = (container.querySelector('[data-testid="agent-runtime-dt-label"]') as HTMLElement).textContent ?? '';

    // Each label must be unique.
    expect(backendText.toLowerCase()).not.toBe(modelText.toLowerCase());
    expect(modelText.toLowerCase()).not.toBe(runtimeText.toLowerCase());
    expect(backendText.toLowerCase()).not.toBe(runtimeText.toLowerCase());
  });
});
