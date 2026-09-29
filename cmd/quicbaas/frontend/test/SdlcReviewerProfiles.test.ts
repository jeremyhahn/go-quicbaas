/**
 * SdlcReviewerProfiles.test.ts
 *
 * The profiles editor: every row says its merge role in words, a new first
 * profile is the default and counts, and "Make default" reorders without
 * changing what a profile's verdict means.
 */

import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import SdlcReviewerProfiles from '../src/components/orchestrator/SdlcReviewerProfiles.svelte';
import { emptyReviewerProfile, type ReviewerProfileRow } from '../src/lib/sdlcBinding.ts';

function rows(): ReviewerProfileRow[] {
  return [
    { ...emptyReviewerProfile(), agent: 'codex-reviewer', login: 'codex', tokenSecretRef: 'codex-tok', countsTowardMerge: true },
    { ...emptyReviewerProfile(), name: 'qwen', agent: 'qwen-reviewer', login: 'qwen-reviewer', tokenSecretRef: 'qwen-tok' },
  ];
}

describe('SdlcReviewerProfiles', () => {
  it('names each profile, its role and the context it publishes', () => {
    render(SdlcReviewerProfiles, { props: { idPrefix: 't', profiles: rows() } });
    expect(screen.getAllByTestId('reviewer-profile')).toHaveLength(2);
    expect(screen.getByText('Default')).toBeTruthy();
    expect(screen.getByText('Counts toward merge')).toBeTruthy();
    expect(screen.getByText('Advisory')).toBeTruthy();
    expect(screen.getByText('quicbaas/review/qwen')).toBeTruthy();
  });

  it('says a project with no profile is not self-reviewed, and adds a counting default', async () => {
    const profiles: ReviewerProfileRow[] = [];
    render(SdlcReviewerProfiles, { props: { idPrefix: 't', profiles } });
    expect(screen.getByText(/Not self-reviewed/)).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: /Add profile/ }));
    expect(await screen.findByText('Counts toward merge')).toBeTruthy();
    expect(screen.getByLabelText(/Agent/)).toBeTruthy();
  });

  it('makes an alternate the default without promoting it', async () => {
    render(SdlcReviewerProfiles, { props: { idPrefix: 't', profiles: rows() } });
    await fireEvent.click(screen.getByRole('button', { name: 'Make qwen the default reviewer' }));
    const first = screen.getAllByTestId('reviewer-profile')[0]!;
    expect(first.textContent).toContain('qwen');
    expect(first.textContent).toContain('Default');
    expect(first.textContent).toContain('Advisory');
  });

  it('removes a profile', async () => {
    render(SdlcReviewerProfiles, { props: { idPrefix: 't', profiles: rows() } });
    await fireEvent.click(screen.getByRole('button', { name: 'Remove qwen' }));
    expect(screen.getAllByTestId('reviewer-profile')).toHaveLength(1);
  });
});
