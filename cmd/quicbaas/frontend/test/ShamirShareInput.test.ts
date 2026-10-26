/**
 * ShamirShareInput.test.ts
 *
 * go-qrdb consumes a quorum challenge on every submission, the refused ones
 * included. After a refusal the component must drop the consumed challenge so
 * the key holder can request a fresh one, and the retry must answer that fresh
 * challenge, never the consumed one.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { ShamirShareInput } from '@quicbaas/svelte-security';
import type { BarrierQuorumChallenge, BarrierStatus } from '@quicbaas/svelte-security';

function makeChallenge(id: string): BarrierQuorumChallenge {
  return {
    challenge_id: id,
    factor: 'mtls',
    nonce: 'bm9uY2U=',
    method_data: { purpose: 'system/unseal/unseal' },
    expires_at: '2026-10-25T00:05:00Z',
  };
}

const status: BarrierStatus = {
  strategy: 'shamir',
  sealed: true,
  initialized: true,
  threshold: 2,
  total_shares: 3,
  shares_provided: 0,
};

async function requestChallenge(username: string): Promise<void> {
  await fireEvent.input(screen.getByLabelText(/Officer username/i), { target: { value: username } });
  await fireEvent.click(screen.getByRole('button', { name: /Request challenge/i }));
  await screen.findByTestId('shamir-challenge');
}

async function submitShare(response: string): Promise<void> {
  await fireEvent.input(screen.getByLabelText(/^Share$/i), { target: { value: 'c2hhcmU=' } });
  await fireEvent.input(screen.getByLabelText(/Signed response/i), { target: { value: response } });
  await fireEvent.click(screen.getByRole('button', { name: /Submit share/i }));
}

describe('ShamirShareInput', () => {
  it.each([
    ['an invalid signature', 'quorum challenge signature is invalid'],
    ['an expired challenge', 'quorum challenge has expired'],
  ])('a submission refused for %s drops the consumed challenge', async (_case, refusal) => {
    const quorumBegin = vi
      .fn()
      .mockResolvedValueOnce(makeChallenge('ch-1'))
      .mockResolvedValueOnce(makeChallenge('ch-2'));
    const quorumSubmit = vi
      .fn()
      .mockRejectedValueOnce(new Error(refusal))
      .mockResolvedValueOnce({ submitted: 1, required: 2, complete: false });
    const onerror = vi.fn();

    render(ShamirShareInput, { props: { client: { quorumBegin, quorumSubmit }, status, onerror } });

    await requestChallenge('officer-1');
    await submitShare('bad-sig');

    expect(await screen.findByRole('alert')).toHaveTextContent(refusal);
    expect(onerror).toHaveBeenCalledOnce();
    // The consumed challenge is gone: the holder can request a fresh one and
    // the username is editable again.
    expect(screen.queryByTestId('shamir-challenge')).toBeNull();
    expect(screen.getByLabelText(/Officer username/i)).toBeEnabled();
    expect(screen.getByRole('button', { name: /Request challenge/i })).toBeEnabled();

    await requestChallenge('officer-1');
    expect(screen.getByTestId('shamir-challenge')).toHaveTextContent('ch-2');
    // The signature over the consumed challenge does not carry over.
    expect(screen.getByLabelText(/Signed response/i)).toHaveValue('');

    await submitShare('good-sig');
    await waitFor(() => {
      expect(screen.getByTestId('shamir-progress-text')).toHaveTextContent('1 / 2');
    });
    expect(quorumBegin).toHaveBeenCalledTimes(2);
    expect(quorumSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({ username: 'officer-1', challenge_id: 'ch-2', response: 'good-sig' }),
    );
  });
});
