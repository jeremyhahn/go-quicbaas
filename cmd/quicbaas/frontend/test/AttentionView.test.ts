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
 * AttentionView.test.ts
 *
 * Verifies the inbox mounts, renders a card per item with its issue link,
 * age and recommendation, offers the primary action, and renders the empty
 * state when nothing is waiting. attentionStore is mocked directly (rather
 * than the SDK client several layers below it) because its own contract —
 * what a card reads and what an action calls — is what this view answers
 * for; attentionStore's own fetch/resolve behaviour is covered by
 * attentionLib.test.ts and the Go-side AttentionService tests.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { AttentionItem } from '@quicbaas/sdk';

const { mockRefreshAttention, mockResolveAttentionItem, state } = vi.hoisted(() => ({
  mockRefreshAttention: vi.fn(),
  mockResolveAttentionItem: vi.fn(),
  state: { items: [] as AttentionItem[], total: 0, facets: {} as Record<string, Record<string, number>> },
}));

vi.mock('../src/stores/attentionStore.svelte.ts', () => ({
  attentionStore: {
    get items() { return state.items; },
    get total() { return state.total; },
    get facets() { return state.facets; },
    get loading() { return false; },
    get resolvingID() { return null; },
    get oldestAgeInstant() { return undefined; },
    get visibleAlerts() { return state.items.filter((i) => i.kind !== 'parked_task'); },
  },
  refreshAttention: mockRefreshAttention,
  resolveAttentionItem: mockResolveAttentionItem,
  dismissAlert: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeTaskDetail: vi.fn(),
}));

import AttentionView from '../src/views/orchestrator/AttentionView.svelte';

function parkedTask(overrides: Partial<AttentionItem> = {}): AttentionItem {
  return {
    id: 'task-1',
    kind: 'parked_task',
    project_id: 'p1',
    project_name: 'api',
    subject: 'a container-backed spawn cannot mount the workspace',
    issue_ref: 'acme/api#981',
    issue_url: 'https://forge.test/acme/api/issues/981',
    block_kind: 'needs_info',
    condition: 'owner_ruling',
    question: 'Should remote fleet nodes get a mount fallback?',
    recommendation: 'Route container-backed spawns only to daemon-host agents until #981 lands.',
    status_changed_at: Date.now() * 1_000_000,
    actions: ['approve_recommendation', 'readmit', 'close_as_done'],
    ...overrides,
  };
}

beforeEach(() => {
  state.items = [];
  state.total = 0;
  state.facets = {};
  mockRefreshAttention.mockClear();
  mockResolveAttentionItem.mockClear().mockResolvedValue(true);
});

describe('AttentionView', () => {
  it('refreshes the inbox on mount', async () => {
    render(AttentionView);
    await waitFor(() => expect(mockRefreshAttention).toHaveBeenCalled());
  });

  it('renders the empty state when nothing is waiting', async () => {
    render(AttentionView);
    expect(await screen.findByText('Nothing needs you right now')).toBeInTheDocument();
  });

  it('renders a card with the project, the linked issue, the recommendation and the primary action', async () => {
    state.items = [parkedTask()];
    state.total = 1;
    render(AttentionView);

    expect(await screen.findByText(/a container-backed spawn/)).toBeInTheDocument();
    expect(screen.getByText('api')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /acme\/api#981/ })).toHaveAttribute(
      'href',
      'https://forge.test/acme/api/issues/981',
    );
    expect(screen.getByText(/Route container-backed spawns/)).toBeInTheDocument();
    expect(screen.getByTestId('attention-primary-action')).toHaveTextContent('Approve recommendation');
  });

  it('resolves the item when the primary action is clicked', async () => {
    state.items = [parkedTask()];
    state.total = 1;
    render(AttentionView);

    const button = await screen.findByTestId('attention-primary-action');
    await fireEvent.click(button);

    await waitFor(() =>
      expect(mockResolveAttentionItem).toHaveBeenCalledWith('task-1', 'approve_recommendation', undefined),
    );
  });

  it('opens a textarea for the answer action and posts the typed text', async () => {
    state.items = [parkedTask({ actions: ['answer', 'readmit'] })];
    state.total = 1;
    render(AttentionView);

    const button = await screen.findByTestId('attention-primary-action');
    expect(button).toHaveTextContent('Answer');
    await fireEvent.click(button);

    const textarea = await screen.findByTestId('attention-answer-text');
    await fireEvent.input(textarea, { target: { value: 'Use the daemon-host mount for now.' } });
    await fireEvent.click(screen.getByTestId('attention-answer-submit'));

    await waitFor(() =>
      expect(mockResolveAttentionItem).toHaveBeenCalledWith('task-1', 'answer', 'Use the daemon-host mount for now.'),
    );
  });

  it('opens the text box for Add note and posts the note without readmitting', async () => {
    state.items = [parkedTask({ actions: ['answer', 'readmit', 'close_as_done', 'note'] })];
    state.total = 1;
    render(AttentionView);

    await fireEvent.click(await screen.findByTestId('attention-action-note'));
    const textarea = await screen.findByTestId('attention-answer-text');
    await fireEvent.input(textarea, { target: { value: 'Decided: keep the v1 wire format.' } });
    const submit = screen.getByTestId('attention-answer-submit');
    expect(submit).toHaveTextContent('Post note');
    await fireEvent.click(submit);

    await waitFor(() =>
      expect(mockResolveAttentionItem).toHaveBeenCalledWith('task-1', 'note', 'Decided: keep the v1 wire format.'),
    );
  });

  it('does not post an empty note', async () => {
    state.items = [parkedTask({ actions: ['answer', 'note'] })];
    state.total = 1;
    render(AttentionView);

    await fireEvent.click(await screen.findByTestId('attention-action-note'));
    expect(await screen.findByTestId('attention-answer-submit')).toBeDisabled();
  });

  it('renders an environmental alert card without a resolution button', async () => {
    state.items = [
      {
        id: 'agent_offline:agent-1',
        kind: 'agent_offline',
        subject: 'Fleet node agent-04 is offline',
        message: 'Agent agent-04 (agent-04.local) is offline; work routed to it will not run.',
        severity: 'critical',
      },
    ];
    state.total = 1;
    render(AttentionView);

    expect(await screen.findByText(/Fleet node agent-04 is offline/)).toBeInTheDocument();
    expect(screen.queryByTestId('attention-primary-action')).not.toBeInTheDocument();
  });

  it('filters the cards locally and never re-fetches a narrowed set', async () => {
    state.items = [
      parkedTask(),
      { id: 'agent_offline:agent-1', kind: 'agent_offline', subject: 'Fleet node agent-04 is offline', severity: 'critical' },
    ];
    state.total = 2;
    state.facets = { kind: { parked_task: 1, agent_offline: 1 } };
    render(AttentionView);
    await waitFor(() => expect(mockRefreshAttention).toHaveBeenCalled());

    await fireEvent.change(screen.getByTestId('attention-filter-kind'), { target: { value: 'parked_task' } });

    await waitFor(() => expect(screen.getAllByTestId('attention-card').length).toBe(1));
    expect(screen.queryByText(/Fleet node agent-04/)).not.toBeInTheDocument();
    for (const call of mockRefreshAttention.mock.calls) {
      expect(call).toEqual([]);
    }
  });

  it('shows a filter-specific empty state when the filter excludes everything', async () => {
    state.items = [parkedTask()];
    state.total = 1;
    state.facets = { kind: { parked_task: 1, agent_offline: 0 } };
    render(AttentionView);

    await fireEvent.change(screen.getByTestId('attention-filter-kind'), { target: { value: 'agent_offline' } });

    expect(await screen.findByText('Nothing matches this filter')).toBeInTheDocument();
  });

  it('flags a parked task whose issue already carries the owner\'s ruling', async () => {
    state.items = [parkedTask({ ruling_posted: true }), parkedTask({ id: 'task-2', ruling_posted: false })];
    state.total = 2;
    render(AttentionView);

    await waitFor(() => expect(screen.getAllByTestId('attention-card').length).toBe(2));
    expect(screen.getAllByTestId('attention-ruling-posted').length).toBe(1);
    expect(screen.getByText('Ruling posted')).toBeInTheDocument();
  });

  it('readmits a parked task from its Readmit button', async () => {
    state.items = [parkedTask()];
    state.total = 1;
    render(AttentionView);

    await fireEvent.click(await screen.findByRole('button', { name: 'Readmit' }));

    await waitFor(() => expect(mockResolveAttentionItem).toHaveBeenCalledWith('task-1', 'readmit', undefined));
  });
});
