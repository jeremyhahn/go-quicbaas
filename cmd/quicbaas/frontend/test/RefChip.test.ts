/**
 * RefChip.test.ts
 *
 * Verifies the reference chip:
 *   - resolves a known value's label and status, and links to the kind's
 *     detail route;
 *   - a value nothing resolves renders visibly as missing (red dot, the raw
 *     value, and a "missing" label) — never silently blank;
 *   - an empty value renders nothing.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';

const { mockGetSDKClient } = vi.hoisted(() => ({ mockGetSDKClient: vi.fn() }));
vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: mockGetSDKClient }));

import RefChip from '../src/components/core/RefChip.svelte';

describe('RefChip', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it('resolves the label/status of a known secret and links to /secrets', async () => {
    mockGetSDKClient.mockResolvedValue({
      secrets: {
        listSecrets: vi.fn().mockResolvedValue({
          secrets: [{ path: 'sdlc/reviewer/go-truststrap/token', version: 3, created_at: '', updated_at: '' }],
          count: 1,
        }),
      },
    });

    render(RefChip, { props: { kind: 'secret', value: 'sdlc/reviewer/go-truststrap/token' } });

    const chip = await screen.findByTestId('ref-chip-secret');
    expect(chip).toHaveTextContent('sdlc/reviewer/go-truststrap/token');
    await waitFor(() => expect(chip).toHaveAttribute('data-status', 'exists'));
    expect(chip.tagName).toBe('A');
    expect(chip.getAttribute('href')).toBe('#/secrets');
  });

  it('renders a reference nothing resolves as visibly missing', async () => {
    mockGetSDKClient.mockResolvedValue({
      secrets: {
        listSecrets: vi.fn().mockResolvedValue({ secrets: [], count: 0 }),
        getSecret: vi.fn().mockRejectedValue(new Error('not found')),
      },
    });

    render(RefChip, { props: { kind: 'secret', value: 'sdlc/reviewer/gone/token' } });

    const chip = await screen.findByTestId('ref-chip-secret');
    await waitFor(() => expect(chip).toHaveAttribute('data-status', 'missing'));
    expect(chip).toHaveTextContent('missing');
    expect(chip.className).toContain('text-error');
  });

  it('renders nothing for an empty value', () => {
    render(RefChip, { props: { kind: 'secret', value: '' } });
    expect(screen.queryByTestId('ref-chip-secret')).not.toBeInTheDocument();
  });
});
