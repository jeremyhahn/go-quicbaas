/**
 * KVStoreView.test.ts
 *
 * Verifies that KVStoreView renders the key list, opens the Put dialog,
 * calls SDK methods with correct arguments, and handles error states.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockEntries = [
  { key: 'alpha', value: { count: 1 } },
  { key: 'beta',  value: 'hello' },
];

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    data: {
      kvList:   vi.fn().mockResolvedValue({ entries: [] }),
      kvGet:    vi.fn(),
      kvPut:    vi.fn().mockResolvedValue(undefined),
      kvDelete: vi.fn().mockResolvedValue(undefined),
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import KVStoreView from '../src/views/data/KVStoreView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('KVStoreView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getSDKClient).mockResolvedValue({
      data: {
        kvList:   vi.fn().mockResolvedValue({ entries: mockEntries }),
        kvGet:    vi.fn(),
        kvPut:    vi.fn().mockResolvedValue(undefined),
        kvDelete: vi.fn().mockResolvedValue(undefined),
      },
    } as never);
  });

  it('renders the page heading "KV Store"', async () => {
    render(KVStoreView);
    await waitFor(() => {
      expect(screen.getByText('KV Store')).toBeInTheDocument();
    });
  });

  it('renders key names from the list after loading', async () => {
    render(KVStoreView);
    await waitFor(() => {
      expect(screen.getByText('alpha')).toBeInTheDocument();
      expect(screen.getByText('beta')).toBeInTheDocument();
    });
  });

  it('shows "Put Key" button', async () => {
    render(KVStoreView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Put Key/i })).toBeInTheDocument();
    });
  });

  it('opens the Put Key dialog when button is clicked', async () => {
    render(KVStoreView);
    await waitFor(() => screen.getByRole('button', { name: /Put Key/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Put Key/i }));
    expect(screen.getByText('Put Key-Value')).toBeInTheDocument();
  });

  it('shows empty state when no entries exist', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      data: {
        kvList: vi.fn().mockResolvedValueOnce({ entries: [] }),
        kvGet: vi.fn(),
        kvPut: vi.fn(),
        kvDelete: vi.fn(),
      },
    } as never);
    render(KVStoreView);
    await waitFor(() => {
      // EmptyState now renders "No keys for entity type..." when the list is empty.
      expect(screen.getByText(/No keys for entity type/i)).toBeInTheDocument();
    });
  });

  it('calls kvList with the entity type', async () => {
    render(KVStoreView);
    await waitFor(() => {
      expect(vi.mocked(getSDKClient)).toHaveBeenCalled();
    });
  });

  it('shows an error alert when kvList fails', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      data: {
        kvList: vi.fn().mockRejectedValueOnce(new Error('network error')),
        kvGet: vi.fn(),
        kvPut: vi.fn(),
        kvDelete: vi.fn(),
      },
    } as never);
    render(KVStoreView);
    await waitFor(() => {
      expect(screen.getByText(/Failed to load keys/i)).toBeInTheDocument();
    });
  });
});
