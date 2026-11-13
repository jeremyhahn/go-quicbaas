/**
 * PromptsView.test.ts
 *
 * Verifies that PromptsView, over the SDK's PromptsClient:
 *   - lists a set's prompts at the chosen scope;
 *   - shows a selected prompt's effective text and its history;
 *   - shows the diff against the parent scope's body;
 *   - puts an edit with expected_version, and on a 409 shows the server's
 *     message while keeping the edit;
 *   - rolls back to a revision with a note;
 *   - resets the override only after a confirmation;
 *   - surfaces a 403 or 503 APIError status in a toast and keeps the list;
 *   - shows a typed "not available on this transport" state when the
 *     transport refuses prompts.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/svelte';
import type {
  EffectivePrompt,
  PromptAddress,
  PromptDiff,
  PromptHistory,
  PromptListResponse,
  PromptRecord,
} from '@quicbaas/sdk';
import { APIError, TransportNotSupportedError } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const listing: PromptListResponse = {
  set: 'workflow',
  scope: 'tenant',
  prompts: [
    {
      name: 'plan',
      description: 'Plans the work',
      overridden: true,
      version: 3,
      effective: { set: 'workflow', name: 'plan', scope: 'tenant', version: 3, hash: 'h3' },
    },
    {
      name: 'review',
      overridden: false,
      effective: { set: 'workflow', name: 'review', scope: 'embedded', version: 0, hash: 'he' },
    },
  ],
};

const effective: EffectivePrompt = {
  set: 'workflow',
  name: 'plan',
  scope: 'tenant',
  content: 'Plan the change carefully.',
  origin: { set: 'workflow', name: 'plan', scope: 'tenant', version: 3, hash: 'h3' },
  chain: [
    { scope: 'tenant', present: true, version: 3, hash: 'h3', answered: true },
    { scope: 'embedded', present: true, answered: false },
  ],
};

const history: PromptHistory = {
  set: 'workflow',
  name: 'plan',
  scope: 'tenant',
  revisions: [
    { version: 1, content_hash: 'h1', author: 'alice', note: 'first cut', follow_parent: false, created_at: '2026-10-01T00:00:00Z' },
    { version: 2, content_hash: 'h2', author: 'bob', follow_parent: false, created_at: '2026-10-02T00:00:00Z' },
    { version: 3, content_hash: 'h3', author: 'alice', note: 'tighten', follow_parent: false, created_at: '2026-10-03T00:00:00Z' },
  ],
};

const diff: PromptDiff = {
  set: 'workflow',
  name: 'plan',
  scope: 'tenant',
  origin: { set: 'workflow', name: 'plan', scope: 'tenant', version: 3, hash: 'h3' },
  parent_origin: { set: 'workflow', name: 'plan', scope: 'embedded', version: 0, hash: 'he' },
  same: false,
  lines: [
    { op: 'delete', text: 'Plan the change.' },
    { op: 'insert', text: 'Plan the change carefully.' },
  ],
};

const record: PromptRecord = {
  tenant_id: 'default',
  scope: 'tenant',
  set: 'workflow',
  name: 'plan',
  version: 4,
  content_hash: 'h4',
  content: 'edited',
  follow_parent: false,
  updated_at: '2026-10-04T00:00:00Z',
};

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const { prompts, mockGetSDKClient, mockShowToast } = vi.hoisted(() => ({
  prompts: {
    list: vi.fn(),
    get: vi.fn(),
    effective: vi.fn(),
    diff: vi.fn(),
    history: vi.fn(),
    put: vi.fn(),
    rollback: vi.fn(),
    reset: vi.fn(),
  },
  mockGetSDKClient: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
}));

const { routerMock } = vi.hoisted(() => ({
  routerMock: { name: 'prompts', params: {} as Record<string, string>, hash: '#/prompts' },
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routerStore: routerMock,
}));

import PromptsView from '../src/views/orchestrator/PromptsView.svelte';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async function renderAndSelectPlan(): Promise<void> {
  render(PromptsView);
  await fireEvent.click(await screen.findByRole('button', { name: /Open prompt plan/i }));
  await screen.findByText('Plan the change carefully.', { selector: 'pre' });
}

describe('PromptsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    routerMock.params = {};
    prompts.list.mockResolvedValue(listing);
    prompts.effective.mockResolvedValue(effective);
    prompts.history.mockResolvedValue(history);
    prompts.diff.mockResolvedValue(diff);
    prompts.put.mockResolvedValue(record);
    prompts.rollback.mockResolvedValue({ ...record, content: 'first' });
    prompts.reset.mockResolvedValue({ ...record, follow_parent: true });
    mockGetSDKClient.mockResolvedValue({ prompts });
  });

  it('lists the prompts of the workflow set at tenant scope', async () => {
    render(PromptsView);
    expect(await screen.findByText('plan')).toBeInTheDocument();
    expect(screen.getByText('review')).toBeInTheDocument();
    expect(prompts.list).toHaveBeenCalledWith(
      expect.objectContaining({ tenant_id: 'default', scope: 'tenant', set: 'workflow' }),
    );
  });

  it('lists the chosen set when the set selector changes', async () => {
    render(PromptsView);
    await screen.findByText('plan');
    await fireEvent.change(screen.getByLabelText('Prompt set'), { target: { value: 'sdlc' } });
    await waitFor(() => {
      expect(prompts.list).toHaveBeenLastCalledWith(expect.objectContaining({ set: 'sdlc' }));
    });
  });

  it('shows the effective text and the history of the selected prompt', async () => {
    await renderAndSelectPlan();
    expect(prompts.effective).toHaveBeenCalledWith(
      expect.objectContaining({ set: 'workflow', name: 'plan', scope: 'tenant', tenant_id: 'default' }),
    );
    const historyTable = screen.getByRole('table', { name: /Prompt history/i });
    expect(within(historyTable).getByText('first cut')).toBeInTheDocument();
    expect(within(historyTable).getByText('tighten')).toBeInTheDocument();
    expect(within(historyTable).getAllByRole('row')).toHaveLength(4);
  });

  it('shows the diff against the parent scope', async () => {
    await renderAndSelectPlan();
    await fireEvent.click(screen.getByRole('button', { name: /Diff against parent/i }));
    const diffView = await screen.findByRole('region', { name: /Prompt diff/i });
    expect(within(diffView).getByText('v3 (tenant) against v0 (embedded)')).toBeInTheDocument();
    expect(within(diffView).getByText('Plan the change.')).toHaveClass('diff-delete');
    expect(within(diffView).getByText('Plan the change carefully.')).toHaveClass('diff-insert');
  });

  it('puts an edit with the scope record version as expected_version', async () => {
    await renderAndSelectPlan();
    await fireEvent.click(screen.getByRole('button', { name: /Edit prompt/i }));
    await fireEvent.input(screen.getByLabelText('Prompt body'), { target: { value: 'edited' } });
    await fireEvent.input(screen.getByLabelText('Edit note'), { target: { value: 'why' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Save$/ }));
    await waitFor(() => {
      expect(prompts.put).toHaveBeenCalledWith(
        expect.objectContaining({
          set: 'workflow',
          name: 'plan',
          scope: 'tenant',
          content: 'edited',
          note: 'why',
          expected_version: 3,
        }),
      );
    });
    expect(mockShowToast).toHaveBeenCalledWith('success', expect.stringContaining('v4'));
  });

  it('keeps the edit and shows the server message on a 409 conflict', async () => {
    prompts.put.mockRejectedValue(new APIError(409, 'prompt workflow/plan is at version 5, not 3'));
    await renderAndSelectPlan();
    await fireEvent.click(screen.getByRole('button', { name: /Edit prompt/i }));
    await fireEvent.input(screen.getByLabelText('Prompt body'), { target: { value: 'my edit' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Save$/ }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('prompt workflow/plan is at version 5, not 3');
    expect(screen.getByLabelText('Prompt body')).toHaveValue('my edit');
  });

  it('saves with the version the edit started from after a refresh reads a newer one', async () => {
    await renderAndSelectPlan();
    await fireEvent.click(screen.getByRole('button', { name: /Edit prompt/i }));
    await fireEvent.input(screen.getByLabelText('Prompt body'), { target: { value: 'my edit' } });

    // Another operator saved plan meanwhile: the refreshed list names v4.
    prompts.list.mockResolvedValue({
      ...listing,
      prompts: (listing.prompts ?? []).map((p) => (p.name === 'plan' ? { ...p, version: 4 } : p)),
    });
    await fireEvent.click(screen.getByRole('button', { name: /Refresh prompts/i }));
    await waitFor(() => {
      expect(prompts.list).toHaveBeenCalledTimes(2);
      expect(screen.getByRole('button', { name: /Refresh prompts/i })).toBeEnabled();
    });

    await fireEvent.click(screen.getByRole('button', { name: /^Save$/ }));
    await waitFor(() => {
      expect(prompts.put).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'plan', content: 'my edit', expected_version: 3 }),
      );
    });
  });

  it('ignores a detail read that answers after another prompt was selected', async () => {
    const reviewEffective: EffectivePrompt = {
      ...effective,
      name: 'review',
      scope: 'embedded',
      content: 'Review the change.',
    };
    let answerPlan: (eff: EffectivePrompt) => void = () => {};
    prompts.effective.mockImplementation((addr: PromptAddress) =>
      addr.name === 'plan'
        ? new Promise<EffectivePrompt>((resolve) => {
            answerPlan = resolve;
          })
        : Promise.resolve(reviewEffective),
    );

    render(PromptsView);
    await fireEvent.click(await screen.findByRole('button', { name: /Open prompt plan/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Open prompt review/i }));
    await screen.findByText('Review the change.', { selector: 'pre' });

    // plan's read answers last; let every pending continuation run.
    answerPlan(effective);
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.getByText('Review the change.', { selector: 'pre' })).toBeInTheDocument();

    await fireEvent.click(screen.getByRole('button', { name: /Edit prompt/i }));
    await fireEvent.click(screen.getByRole('button', { name: /^Save$/ }));
    await waitFor(() => {
      expect(prompts.put).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'review', content: 'Review the change.' }),
      );
    });
  });

  it('rolls back to a revision with a note', async () => {
    await renderAndSelectPlan();
    await fireEvent.click(screen.getByRole('button', { name: /Roll back to v1/i }));
    const dialog = screen.getByRole('dialog', { name: /Confirm rollback/i });
    await fireEvent.input(within(dialog).getByLabelText('Rollback note'), {
      target: { value: 'revert the tightening' },
    });
    await fireEvent.click(within(dialog).getByRole('button', { name: /^Roll back$/ }));
    await waitFor(() => {
      expect(prompts.rollback).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'plan',
          to_version: 1,
          note: 'revert the tightening',
          expected_version: 3,
        }),
      );
    });
  });

  it('resets the override only after the confirmation', async () => {
    await renderAndSelectPlan();
    await fireEvent.click(screen.getByRole('button', { name: /Reset to parent/i }));
    expect(prompts.reset).not.toHaveBeenCalled();
    const dialog = screen.getByRole('dialog', { name: /Confirm reset/i });
    await fireEvent.click(within(dialog).getByRole('button', { name: /^Reset$/ }));
    await waitFor(() => {
      expect(prompts.reset).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'plan', scope: 'tenant', expected_version: 3 }),
      );
    });
  });

  it('cancelling the reset confirmation resets nothing', async () => {
    await renderAndSelectPlan();
    await fireEvent.click(screen.getByRole('button', { name: /Reset to parent/i }));
    const dialog = screen.getByRole('dialog', { name: /Confirm reset/i });
    await fireEvent.click(within(dialog).getByRole('button', { name: /Cancel/i }));
    expect(screen.queryByRole('dialog', { name: /Confirm reset/i })).not.toBeInTheDocument();
    expect(prompts.reset).not.toHaveBeenCalled();
  });

  it.each([403, 503])('a %i on refresh toasts the status and keeps the list', async (status) => {
    render(PromptsView);
    await screen.findByText('plan');
    prompts.list.mockRejectedValueOnce(new APIError(status, 'refused'));
    await fireEvent.click(screen.getByRole('button', { name: /Refresh prompts/i }));
    await waitFor(() => {
      expect(mockShowToast).toHaveBeenCalledWith('error', expect.stringContaining(`(${status})`));
    });
    expect(screen.getByText('plan')).toBeInTheDocument();
    expect(screen.getByText('review')).toBeInTheDocument();
  });

  it('shows a not-available state when the transport refuses prompts', async () => {
    prompts.list.mockRejectedValue(
      new TransportNotSupportedError('grpc-web', 'prompts', 'served over REST only', 'list'),
    );
    render(PromptsView);
    const state = await screen.findByRole('status', { name: /Prompts unavailable/i });
    expect(state).toHaveTextContent(/not available on the grpc-web transport/i);
    expect(screen.queryByRole('button', { name: /Open prompt/i })).not.toBeInTheDocument();
  });

  describe('opened from a link to one prompt version', () => {
    it('opens on the linked set, scope and project and opens the prompt', async () => {
      routerMock.params = { set: 'sdlc', scope: 'project', project: 'proj-1', name: 'plan', version: '0' };
      render(PromptsView);
      await screen.findByText('Plan the change carefully.', { selector: 'pre' });

      expect(prompts.list).toHaveBeenCalledWith(
        expect.objectContaining({ set: 'sdlc', scope: 'project', project_id: 'proj-1' }),
      );
      expect(prompts.effective).toHaveBeenCalledWith(
        expect.objectContaining({ set: 'sdlc', scope: 'project', project_id: 'proj-1', name: 'plan' }),
      );
      expect(screen.getByLabelText('Prompt set')).toHaveValue('sdlc');
      expect(screen.getByLabelText('Prompt scope')).toHaveValue('project');
      // Version 0 names no stored revision: nothing to read or mark.
      expect(prompts.get).not.toHaveBeenCalled();
    });

    it('reads the linked revision, shows its body and marks it in the history', async () => {
      prompts.get.mockResolvedValue({ ...record, version: 2, content_hash: 'h2', content: 'Plan the change.' });
      routerMock.params = { set: 'workflow', scope: 'tenant', name: 'plan', version: '2' };
      render(PromptsView);

      const linkedSection = await screen.findByRole('region', { name: /Linked version/i });
      expect(linkedSection).toHaveTextContent('Linked version v2 (tenant)');
      expect(within(linkedSection).getByText('Plan the change.')).toBeInTheDocument();
      expect(prompts.get).toHaveBeenCalledWith(
        expect.objectContaining({ set: 'workflow', scope: 'tenant', name: 'plan', tenant_id: 'default', version: 2 }),
      );

      const historyTable = screen.getByRole('table', { name: /Prompt history/i });
      const current = within(historyTable).getAllByRole('row').filter((r) => r.getAttribute('aria-current') === 'true');
      expect(current).toHaveLength(1);
      expect(current[0]).toHaveTextContent('v2');
    });

    it('reports a revision it cannot read and still shows the prompt', async () => {
      prompts.get.mockRejectedValue(new APIError(404, 'no such revision'));
      routerMock.params = { set: 'workflow', scope: 'tenant', name: 'plan', version: '9' };
      render(PromptsView);

      await screen.findByText('Plan the change carefully.', { selector: 'pre' });
      await waitFor(() => {
        expect(mockShowToast).toHaveBeenCalledWith('error', expect.stringContaining('Loading plan v9 failed (404)'));
      });
      expect(screen.queryByRole('region', { name: /Linked version/i })).not.toBeInTheDocument();
    });

    it('opens nothing for a prompt the set does not list', async () => {
      routerMock.params = { set: 'workflow', scope: 'tenant', name: 'missing', version: '1' };
      render(PromptsView);
      await screen.findByText('plan');
      expect(prompts.effective).not.toHaveBeenCalled();
      expect(prompts.get).not.toHaveBeenCalled();
    });

    it('ignores a link to a scope the page does not browse', async () => {
      routerMock.params = { set: 'workflow', scope: 'embedded', name: 'plan', version: '1' };
      render(PromptsView);
      await screen.findByText('plan');
      expect(prompts.list).toHaveBeenCalledWith(expect.objectContaining({ scope: 'tenant', set: 'workflow' }));
      expect(prompts.effective).not.toHaveBeenCalled();
    });
  });
});
