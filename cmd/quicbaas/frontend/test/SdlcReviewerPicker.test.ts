/**
 * SdlcReviewerPicker.test.ts
 *
 * The per-pull-request reviewer select: it lists the binding's profiles,
 * writes the operator's choice through the SDLC module's own verbs, clears it
 * with "Project default", and renders nothing where there is nothing to pick.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

const { sdlcMock } = vi.hoisted(() => ({
  sdlcMock: {
    binding: vi.fn(),
    reviewer: vi.fn(),
    setReviewer: vi.fn(),
    clearReviewer: vi.fn(),
  },
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({ sdlc: sdlcMock }),
}));
vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));

import SdlcReviewerPicker from '../src/components/orchestrator/SdlcReviewerPicker.svelte';

const binding = {
  project_id: 'go-quicbaas',
  reviewer: { agent_id: 'codex-reviewer', login: 'codex', token_secret_ref: 'codex-tok' },
  reviewers: [{ name: 'qwen', agent_id: 'qwen-reviewer', login: 'qwen-reviewer', token_secret_ref: 'qwen-tok' }],
};

function selection(profile: string, overridden: boolean, counts: boolean) {
  return { project_id: 'go-quicbaas', pull_request: 1075, profile, overridden, counts_toward_merge: counts, status_context: '' };
}

beforeEach(() => {
  vi.clearAllMocks();
  sdlcMock.binding.mockResolvedValue(binding);
  sdlcMock.reviewer.mockResolvedValue(selection('default', false, true));
});

function renderPicker() {
  return render(SdlcReviewerPicker, { props: { tenantId: 'acme', projectId: 'go-quicbaas', pullRequest: 1075 } });
}

describe('SdlcReviewerPicker', () => {
  it('lists the project default and every profile, and shows the current role', async () => {
    renderPicker();
    const select = (await screen.findByLabelText('Reviewer')) as HTMLSelectElement;
    const labels = Array.from(select.options).map((o) => o.textContent?.trim());
    expect(labels).toEqual(['Project default (default)', 'default', 'qwen (advisory)']);
    expect(await screen.findByText('Counts toward merge')).toBeTruthy();
    expect(sdlcMock.reviewer).toHaveBeenCalledWith('acme', 'go-quicbaas', 1075);
  });

  it('records an advisory choice and says it cannot land the change', async () => {
    sdlcMock.setReviewer.mockResolvedValue(selection('qwen', true, false));
    renderPicker();
    const select = (await screen.findByLabelText('Reviewer')) as HTMLSelectElement;
    await fireEvent.change(select, { target: { value: 'qwen' } });
    await waitFor(() => { expect(sdlcMock.setReviewer).toHaveBeenCalledWith('acme', 'go-quicbaas', 1075, 'qwen'); });
    expect(await screen.findByText('Advisory')).toBeTruthy();
    expect(screen.getByText(/cannot land this change/)).toBeTruthy();
  });

  it('clears the override when the project default is picked', async () => {
    sdlcMock.reviewer.mockResolvedValue(selection('qwen', true, false));
    sdlcMock.clearReviewer.mockResolvedValue(selection('default', false, true));
    renderPicker();
    const select = (await screen.findByLabelText('Reviewer')) as HTMLSelectElement;
    await waitFor(() => { expect(select.value).toBe('qwen'); });
    await fireEvent.change(select, { target: { value: '' } });
    await waitFor(() => { expect(sdlcMock.clearReviewer).toHaveBeenCalledWith('acme', 'go-quicbaas', 1075); });
  });

  it('reports a refused choice and snaps back to what holds', async () => {
    sdlcMock.setReviewer.mockRejectedValue(new Error('no such profile'));
    renderPicker();
    const select = (await screen.findByLabelText('Reviewer')) as HTMLSelectElement;
    await fireEvent.change(select, { target: { value: 'qwen' } });
    expect(await screen.findByText(/Could not change the reviewer: no such profile/)).toBeTruthy();
    expect(select.value).toBe('');
  });

  it('renders nothing for an unbound project', async () => {
    sdlcMock.binding.mockRejectedValue(Object.assign(new Error('not found'), { status: 404 }));
    const { container } = renderPicker();
    await waitFor(() => { expect(sdlcMock.binding).toHaveBeenCalled(); });
    expect(container.querySelector('[data-testid="reviewer-picker"]')).toBeNull();
    expect(sdlcMock.reviewer).not.toHaveBeenCalled();
  });
});
