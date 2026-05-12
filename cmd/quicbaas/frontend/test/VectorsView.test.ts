/**
 * VectorsView.test.ts
 *
 * Verifies that VectorsView lists namespaces, opens the create dialog,
 * calls the search SDK method, and handles error states.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { APIError } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockNamespaces = [
  { name: 'docs', dimension: 768, metric: 'cosine', vector_count: 42 },
  { name: 'images', dimension: 512, metric: 'euclidean', vector_count: 7 },
];

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    vectors: {
      listNamespaces:  vi.fn().mockResolvedValue([]),
      createNamespace: vi.fn().mockResolvedValue({ name: 'new-ns' }),
      deleteNamespace: vi.fn().mockResolvedValue(undefined),
      searchSimilar:   vi.fn().mockResolvedValue([]),
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import VectorsView from '../src/views/data/VectorsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('VectorsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getSDKClient).mockResolvedValue({
      vectors: {
        listNamespaces:  vi.fn().mockResolvedValue(mockNamespaces),
        createNamespace: vi.fn().mockResolvedValue({ name: 'new-ns' }),
        deleteNamespace: vi.fn().mockResolvedValue(undefined),
        searchSimilar:   vi.fn().mockResolvedValue([]),
      },
    } as never);
  });

  it('renders the page heading "Vectors"', async () => {
    render(VectorsView);
    await waitFor(() => {
      expect(screen.getByText('Vectors')).toBeInTheDocument();
    });
  });

  it('renders namespace names from the list', async () => {
    render(VectorsView);
    await waitFor(() => {
      // Both the card list and the dropdown show namespace names.
      // Use getAllByText to accept multiple occurrences.
      const docsEls = screen.getAllByText('docs');
      expect(docsEls.length).toBeGreaterThan(0);
      const imagesEls = screen.getAllByText('images');
      expect(imagesEls.length).toBeGreaterThan(0);
    });
  });

  it('shows "New Namespace" button', async () => {
    render(VectorsView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /New Namespace/i })).toBeInTheDocument();
    });
  });

  it('opens the create namespace dialog when button is clicked', async () => {
    render(VectorsView);
    await waitFor(() => screen.getByRole('button', { name: /New Namespace/i }));
    await fireEvent.click(screen.getByRole('button', { name: /New Namespace/i }));
    expect(screen.getByText('New Vector Namespace')).toBeInTheDocument();
  });

  it('shows "Search" button in the search panel', async () => {
    render(VectorsView);
    await waitFor(() => {
      // The Search button now uses aria-label="Run similarity search" so the
      // accessible name matches the aria-label rather than the visible text.
      expect(screen.getByRole('button', { name: /Run similarity search/i })).toBeInTheDocument();
    });
  });

  it('Search button uses btn-block so the label cannot be clipped (#36)', async () => {
    const { container } = render(VectorsView);
    await waitFor(() => {
      const btn = container.querySelector('button[aria-label="Run similarity search"]');
      expect(btn).not.toBeNull();
      expect(btn!.className).toContain('btn-block');
    });
  });

  it('shows empty state when no namespaces exist', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      vectors: {
        listNamespaces:  vi.fn().mockResolvedValueOnce([]),
        createNamespace: vi.fn(),
        deleteNamespace: vi.fn(),
        searchSimilar:   vi.fn(),
      },
    } as never);
    render(VectorsView);
    await waitFor(() => {
      expect(screen.getByText(/No namespaces found/i)).toBeInTheDocument();
    });
  });

  it('shows error alert when listNamespaces fails', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      vectors: {
        listNamespaces:  vi.fn().mockRejectedValueOnce(new Error('connection refused')),
        createNamespace: vi.fn(),
        deleteNamespace: vi.fn(),
        searchSimilar:   vi.fn(),
      },
    } as never);
    render(VectorsView);
    await waitFor(() => {
      expect(screen.getByText(/connection refused/i)).toBeInTheDocument();
    });
  });

  it('shows disabled state info alert when vector subsystem returns 408 not available', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      vectors: {
        listNamespaces:  vi.fn().mockRejectedValueOnce(new APIError(408, 'vector service not available')),
        createNamespace: vi.fn(),
        deleteNamespace: vi.fn(),
        searchSimilar:   vi.fn(),
      },
    } as never);
    render(VectorsView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/QUICBAAS_VECTOR_ENABLED/i);
    });
  });

  it('shows disabled state info alert when vector subsystem returns 503 not configured', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      vectors: {
        listNamespaces:  vi.fn().mockRejectedValueOnce(new APIError(503, 'vector storage not configured')),
        createNamespace: vi.fn(),
        deleteNamespace: vi.fn(),
        searchSimilar:   vi.fn(),
      },
    } as never);
    render(VectorsView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/QUICBAAS_VECTOR_ENABLED/i);
    });
  });

  it('disables "+ New Namespace" button when vector subsystem is not enabled (#69)', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      vectors: {
        listNamespaces:  vi.fn().mockRejectedValueOnce(new APIError(503, 'vector storage not configured')),
        createNamespace: vi.fn(),
        deleteNamespace: vi.fn(),
        searchSimilar:   vi.fn(),
      },
    } as never);
    render(VectorsView);
    await waitFor(() => screen.getByRole('alert'));
    const createBtn = screen.getByRole('button', { name: /New Namespace/i });
    expect(createBtn).toBeDisabled();
  });

  it('disables "Search" button when vector subsystem is not enabled (#69)', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      vectors: {
        listNamespaces:  vi.fn().mockRejectedValueOnce(new APIError(503, 'vector storage not configured')),
        createNamespace: vi.fn(),
        deleteNamespace: vi.fn(),
        searchSimilar:   vi.fn(),
      },
    } as never);
    render(VectorsView);
    await waitFor(() => screen.getByRole('alert'));
    const searchBtn = screen.getByRole('button', { name: /Run similarity search/i });
    expect(searchBtn).toBeDisabled();
  });
});
