/**
 * RefPicker.test.ts
 *
 * Verifies the universal reference picker:
 *   - opens on focus and lists a provider's options;
 *   - filters as the operator types;
 *   - selecting an option commits its value and closes the popover;
 *   - the trailing "Create secret `<path>`" row opens the inline panel and,
 *     on submit, calls the provider's create and selects the result;
 *   - an unregistered kind renders the "no picker registered" fallback
 *     instead of throwing.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

const { mockGetSDKClient } = vi.hoisted(() => ({ mockGetSDKClient: vi.fn() }));
vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: mockGetSDKClient }));

import RefPicker from '../src/components/core/RefPicker.svelte';

function secretsClient(paths: string[]) {
  return {
    secrets: {
      listSecrets: vi.fn().mockResolvedValue({
        secrets: paths.map((path) => ({ path, version: 1, created_at: '', updated_at: '' })),
        count: paths.length,
      }),
      getSecret: vi.fn().mockRejectedValue(new Error('not found')),
      storeSecret: vi.fn().mockResolvedValue(undefined),
    },
  };
}

describe('RefPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('opens on focus and lists the provider options', async () => {
    mockGetSDKClient.mockResolvedValue(secretsClient(['app/db/password', 'sdlc/reviewer/repo/token']));

    render(RefPicker, { props: { kind: 'secret', value: '' } });

    const input = screen.getByRole('combobox');
    await fireEvent.focus(input);

    await waitFor(() => {
      expect(screen.getByTestId('ref-picker-secret-option-app/db/password')).toBeInTheDocument();
    });
    expect(screen.getByTestId('ref-picker-secret-option-sdlc/reviewer/repo/token')).toBeInTheDocument();
  });

  it('selecting an option commits its value and closes the popover', async () => {
    mockGetSDKClient.mockResolvedValue(secretsClient(['app/db/password']));

    render(RefPicker, { props: { kind: 'secret', value: '' } });

    const input = screen.getByRole('combobox');
    await fireEvent.focus(input);
    await waitFor(() => expect(screen.getByTestId('ref-picker-secret-option-app/db/password')).toBeInTheDocument());

    await fireEvent.click(screen.getByTestId('ref-picker-secret-option-app/db/password'));

    await waitFor(() => {
      expect(screen.queryByTestId('ref-picker-secret-popover')).not.toBeInTheDocument();
    });
    expect((input as HTMLInputElement).value).toBe('app/db/password');
  });

  it('offers "Create secret" for an unmatched typed path and creates it inline', async () => {
    const client = secretsClient([]);
    mockGetSDKClient.mockResolvedValue(client);

    render(RefPicker, { props: { kind: 'secret', value: '' } });

    const input = screen.getByRole('combobox');
    await fireEvent.focus(input);
    await fireEvent.input(input, { target: { value: 'sdlc/reviewer/new-project/token' } });

    await waitFor(() => {
      expect(screen.getByTestId('ref-picker-secret-create-row')).toBeInTheDocument();
    });
    expect(screen.getByTestId('ref-picker-secret-create-row')).toHaveTextContent(
      'sdlc/reviewer/new-project/token',
    );

    await fireEvent.click(screen.getByTestId('ref-picker-secret-create-row'));

    const valueField = await screen.findByLabelText(/Value/i);
    await fireEvent.input(valueField, { target: { value: 'ghp_supersecret' } });

    await fireEvent.click(screen.getByRole('button', { name: /^Create$/ }));

    await waitFor(() => {
      expect(client.secrets.storeSecret).toHaveBeenCalledWith({
        path: 'sdlc/reviewer/new-project/token',
        data: { value: 'ghp_supersecret' },
      });
    });
    await waitFor(() => {
      expect((input as HTMLInputElement).value).toBe('sdlc/reviewer/new-project/token');
    });
  });

  it('renders the fallback notice for an unregistered kind instead of throwing', () => {
    // @ts-expect-error — deliberately asking for a kind with no provider.
    render(RefPicker, { props: { kind: 'not-a-kind', value: '' } });
    expect(screen.getByRole('alert')).toHaveTextContent(/No picker is registered/);
  });
});
