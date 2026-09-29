/**
 * SecretsView.test.ts
 *
 * Verifies that SecretsView:
 *   - Renders the "Secrets" page heading.
 *   - Lists secret paths returned by the API.
 *   - Shows the "+ Store Secret" button.
 *   - Opens the Store Secret dialog.
 *   - Calls sdk.secrets.storeSecret with the correct shape.
 *   - Shows an error alert when listSecrets fails.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { SecretEntry } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockSecrets: SecretEntry[] = [
  { path: 'app/database/password', version: 3, created_at: '2025-01-01T00:00:00Z', updated_at: '2025-03-01T00:00:00Z' },
  { path: 'app/api/key',           version: 1, created_at: '2025-02-01T00:00:00Z', updated_at: '2025-02-01T00:00:00Z' },
];

// ---------------------------------------------------------------------------
// Hoisted mocks (safe to reference inside vi.mock factories)
// ---------------------------------------------------------------------------

const { mockStoreSecret, mockGetSecret, mockDeleteSecret, mockRotateSecret, mockGetSDKClient } = vi.hoisted(() => ({
  mockStoreSecret: vi.fn().mockResolvedValue(undefined),
  mockGetSecret: vi.fn().mockResolvedValue({
    path: 'app/database/password',
    data: { password: 'supersecret' },
    version: 3,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-03-01T00:00:00Z',
  }),
  mockDeleteSecret: vi.fn().mockResolvedValue(undefined),
  mockRotateSecret: vi.fn().mockResolvedValue({ path: 'app/database/password', version: 4, rotated_at: '' }),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import SecretsView from '../src/views/security/SecretsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SecretsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetSDKClient.mockResolvedValue({
      secrets: {
        listSecrets: vi.fn().mockResolvedValue({ secrets: mockSecrets, count: 2 }),
        storeSecret: mockStoreSecret,
        getSecret: mockGetSecret,
        deleteSecret: mockDeleteSecret,
        rotateSecret: mockRotateSecret,
      },
    });
  });

  it('renders the "Secrets" page heading', async () => {
    render(SecretsView);
    await waitFor(() => {
      expect(screen.getByText('Secrets')).toBeInTheDocument();
    });
  });

  it('renders the Lock icon prefix in the page header (#33)', async () => {
    const { container } = render(SecretsView);
    await waitFor(() => {
      // The PageHeader icon is rendered beside the title in the page header.
      const tile = container.querySelector('header.page-header');
      expect(tile, 'Secrets header must include the icon prefix').not.toBeNull();
      expect(tile!.querySelector('svg')).not.toBeNull();
    });
  });

  it('renders secret paths from the API', async () => {
    render(SecretsView);
    await waitFor(() => {
      expect(screen.getByText('app/database/password')).toBeInTheDocument();
      expect(screen.getByText('app/api/key')).toBeInTheDocument();
    });
  });

  it('renders a "+ Store Secret" button', async () => {
    render(SecretsView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Store secret/i })).toBeInTheDocument();
    });
  });

  it('opens the Store Secret dialog when clicked', async () => {
    render(SecretsView);
    await waitFor(() => screen.getByRole('button', { name: /Store secret/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Store secret/i }));
    expect(screen.getByRole('dialog', { name: /Store secret dialog/i })).toBeInTheDocument();
  });

  it('calls secrets.storeSecret with the correct shape on submit', async () => {
    render(SecretsView);
    await waitFor(() => screen.getByRole('button', { name: /Store secret/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Store secret/i }));

    await fireEvent.input(screen.getByLabelText('Path'), { target: { value: 'prod/api/key' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Store$/ }));

    await waitFor(() => {
      expect(mockStoreSecret).toHaveBeenCalledWith(
        expect.objectContaining({ path: 'prod/api/key' }),
      );
    });
  });

  it('shows validation error when path is empty on store', async () => {
    render(SecretsView);
    await waitFor(() => screen.getByRole('button', { name: /Store secret/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Store secret/i }));
    // Clear the path field if any default value
    await fireEvent.input(screen.getByLabelText('Path'), { target: { value: '' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Store$/ }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument();
    });
  });

  it('renders Fetch, Rotate, and Delete actions per row', async () => {
    render(SecretsView);
    await waitFor(() => {
      const fetchButtons = screen.getAllByRole('button', { name: /Fetch secret/i });
      expect(fetchButtons.length).toBe(mockSecrets.length);
      const rotateButtons = screen.getAllByRole('button', { name: /Rotate secret/i });
      expect(rotateButtons.length).toBe(mockSecrets.length);
      const deleteButtons = screen.getAllByRole('button', { name: /Delete secret/i });
      expect(deleteButtons.length).toBe(mockSecrets.length);
    });
  });

  it('renders exactly one heading with accessible name "Secrets" in the empty state', async () => {
    mockGetSDKClient.mockResolvedValueOnce({
      secrets: {
        listSecrets: vi.fn().mockResolvedValue({ secrets: [], count: 0 }),
        storeSecret: vi.fn(),
        getSecret: vi.fn(),
        deleteSecret: vi.fn(),
        rotateSecret: vi.fn(),
      },
    });
    render(SecretsView);
    await waitFor(() => {
      const headings = screen.getAllByRole('heading', { name: /^Secrets$/i });
      expect(headings).toHaveLength(1);
    });
  });

  it('shows an error alert when listSecrets fails', async () => {
    mockGetSDKClient.mockResolvedValueOnce({
      secrets: {
        listSecrets: vi.fn().mockRejectedValue(new Error('Secrets engine offline')),
        storeSecret: vi.fn(),
        getSecret: vi.fn(),
        deleteSecret: vi.fn(),
        rotateSecret: vi.fn(),
      },
    });
    render(SecretsView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Secrets engine offline');
    });
  });
});
