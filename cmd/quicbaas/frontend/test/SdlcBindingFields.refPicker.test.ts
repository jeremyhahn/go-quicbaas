/**
 * SdlcBindingFields.refPicker.test.ts
 *
 * Verifies the binding editor's forge token/webhook secret fields, and the
 * default reviewer profile's token field, are the universal RefPicker
 * (kind="secret") rather than free-text inputs — the exact fields ruling 4
 * of the reference-picker plan names by file.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

const { mockGetSDKClient } = vi.hoisted(() => ({ mockGetSDKClient: vi.fn() }));
vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: mockGetSDKClient }));

import SdlcBindingFields from '../src/components/orchestrator/SdlcBindingFields.svelte';
import { emptyBindingForm, emptyReviewerProfile } from '../src/lib/sdlcBinding.ts';

describe('SdlcBindingFields — secret references', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetSDKClient.mockResolvedValue({
      secrets: {
        listSecrets: vi.fn().mockResolvedValue({
          secrets: [
            { path: 'sdlc/reviewer/go-truststrap/token', version: 1, created_at: '', updated_at: '' },
            { path: 'forge-token', version: 1, created_at: '', updated_at: '' },
          ],
          count: 2,
        }),
        getSecret: vi.fn().mockRejectedValue(new Error('not found')),
      },
    });
  });

  it('renders the forge token and webhook secret fields as RefPicker combobox controls', async () => {
    const binding = emptyBindingForm();
    render(SdlcBindingFields, { props: { idPrefix: 'test', binding, availableForges: [] } });

    const tokenInput = document.getElementById('test-sdlc-token-secret-ref') as HTMLInputElement;
    const webhookInput = document.getElementById('test-sdlc-webhook-secret-ref') as HTMLInputElement;
    expect(tokenInput).toHaveAttribute('role', 'combobox');
    expect(webhookInput).toHaveAttribute('role', 'combobox');

    await fireEvent.focus(tokenInput);
    await waitFor(() => {
      expect(screen.getByTestId('ref-picker-secret-option-forge-token')).toBeInTheDocument();
    });
  });

  it("renders the default reviewer profile's token secret field as a RefPicker", async () => {
    const binding = emptyBindingForm();
    binding.reviewerProfiles = [
      { ...emptyReviewerProfile(), backend: 'codex', login: 'codex', countsTowardMerge: true },
    ];
    render(SdlcBindingFields, { props: { idPrefix: 'test', binding, availableForges: [] } });

    // Open the profile editor to reveal its fields.
    await fireEvent.click(screen.getByLabelText(/Edit default/i));

    const tokenInput = document.getElementById('test-reviewer-0-token') as HTMLInputElement;
    expect(tokenInput).toHaveAttribute('role', 'combobox');
  });
});
