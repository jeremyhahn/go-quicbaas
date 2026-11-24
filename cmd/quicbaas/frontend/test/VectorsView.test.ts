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

const mockHealth = {
  status: 'healthy', node_id: 3, version: '1.2.3', uptime_seconds: 60,
  namespaces_healthy: 2, namespaces_degraded: 0, namespaces_unhealthy: 0,
};
const mockCluster = {
  cluster_id: 'c1', total_nodes: 5, total_vectors: 49, total_namespaces: 2,
  total_storage_bytes: 100, total_index_bytes: 50, cluster_qps: 9, timestamp: 'now',
};
const mockGlobal = {
  cluster_id: 'c1', total_nodes: 5, total_vectors: 49, total_namespaces: 2,
  total_storage_bytes: 4096, total_index_bytes: 2048, cluster_qps: 9, timestamp: 'now',
};

/** The SDK client every case starts from; a case overrides what it needs. */
function makeClient(vectors: Record<string, unknown> = {}, vectorAdmin: Record<string, unknown> = {}) {
  return {
    vectors: {
      listNamespaces:    vi.fn().mockResolvedValue(mockNamespaces),
      createNamespace:   vi.fn().mockResolvedValue({ name: 'new-ns' }),
      deleteNamespace:   vi.fn().mockResolvedValue(undefined),
      searchSimilar:     vi.fn().mockResolvedValue([]),
      hybridSearch:      vi.fn().mockResolvedValue({ results: [], total_candidates: 0, total_filtered: 0, search_time_ms: 0, node_id: 1 }),
      searchByMetadata:  vi.fn().mockResolvedValue({ results: [], total_count: 0 }),
      health:            vi.fn().mockResolvedValue(mockHealth),
      clusterStats:      vi.fn().mockResolvedValue(mockCluster),
      getNamespace:      vi.fn().mockResolvedValue({
        namespace: 'docs', dimensions: 768, distance_metric: 'cosine', index_type: 'hnsw',
        vector_count: 42, indexed_count: 42, storage_bytes: 1, index_bytes: 1,
      }),
      getNamespaceStats: vi.fn().mockResolvedValue({
        namespace: 'docs', vector_count: 42, indexed_count: 42, storage_bytes: 1, index_bytes: 1,
        avg_search_latency_ms: 1, search_count: 3,
      }),
      rebuildIndex:      vi.fn(),
      getIndexProgress:  vi.fn(),
      ...vectors,
    },
    vectorAdmin: {
      getGlobalStats:        vi.fn().mockResolvedValue(mockGlobal),
      compactNamespace:      vi.fn(),
      getCompactionProgress: vi.fn(),
      exportVectors:         vi.fn(),
      importVectors:         vi.fn(),
      ...vectorAdmin,
    },
  };
}

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
    vi.mocked(getSDKClient).mockResolvedValue(makeClient() as never);
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

  it('the status strip loads health, cluster stats and global stats once', async () => {
    const client = makeClient();
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorsView);
    await waitFor(() => {
      expect(screen.getByTestId('vector-health')).toHaveTextContent('healthy');
    });
    expect(screen.getByTestId('vector-health')).toHaveTextContent('1.2.3');
    expect(screen.getByTestId('vector-cluster')).toHaveTextContent('5 nodes');
    expect(screen.getByTestId('vector-global')).toHaveTextContent('4096 B stored');
    expect(client.vectors.health).toHaveBeenCalledTimes(1);
    expect(client.vectors.clusterStats).toHaveBeenCalledTimes(1);
    expect(client.vectorAdmin.getGlobalStats).toHaveBeenCalledTimes(1);
  });

  it('a failing status call leaves the other status cards shown', async () => {
    vi.mocked(getSDKClient).mockResolvedValue(
      makeClient({ clusterStats: vi.fn().mockRejectedValue(new Error('cluster down')) }) as never,
    );
    render(VectorsView);
    await waitFor(() => {
      expect(screen.getByTestId('vector-cluster-error')).toHaveTextContent('cluster down');
    });
    await waitFor(() => expect(screen.getByTestId('vector-health')).toHaveTextContent('healthy'));
    expect(screen.getByTestId('vector-global')).toHaveTextContent('4096 B stored');
  });

  it('clicking a namespace row opens its detail panel', async () => {
    const client = makeClient();
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorsView);
    await waitFor(() => screen.getByRole('button', { name: 'Open namespace docs' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Open namespace docs' }));
    await waitFor(() => expect(screen.getByTestId('ns-dimension')).toHaveTextContent('768'));
    expect(client.vectors.getNamespace).toHaveBeenCalledWith('docs');
  });

  async function chooseMode(mode: string): Promise<void> {
    await waitFor(() => screen.getByRole('button', { name: 'Open namespace docs' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Open namespace docs' }));
    await fireEvent.change(screen.getByLabelText('Search mode'), { target: { value: mode } });
  }

  it('Similar mode calls searchSimilar', async () => {
    const client = makeClient();
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorsView);
    await chooseMode('similar');
    await fireEvent.input(screen.getByPlaceholderText('Enter search text…'), { target: { value: 'hello' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Run similarity search' }));
    await waitFor(() => expect(client.vectors.searchSimilar).toHaveBeenCalledWith('docs', { text: 'hello', top_k: 10 }));
    expect(client.vectors.hybridSearch).not.toHaveBeenCalled();
  });

  it('Hybrid mode calls hybridSearch with the namespace, query vector and filter', async () => {
    const client = makeClient({
      hybridSearch: vi.fn().mockResolvedValue({
        results: [{ id: 'hit-1', score: 0.5, rank: 1 }], total_candidates: 1, total_filtered: 1, search_time_ms: 1, node_id: 1,
      }),
    });
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorsView);
    await chooseMode('hybrid');
    await fireEvent.input(screen.getByPlaceholderText('[0.1, 0.2, 0.3]'), { target: { value: '[0.5, 1]' } });
    await fireEvent.input(screen.getByPlaceholderText('{"lang":"en"}'), { target: { value: '{"lang":"en"}' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Run hybrid search' }));
    await waitFor(() => expect(client.vectors.hybridSearch).toHaveBeenCalledWith({
      namespace: 'docs', query_vector: [0.5, 1], k: 10, metadata_filters: { lang: 'en' }, include_metadata: true,
    }));
    await waitFor(() => expect(screen.getByText('hit-1')).toBeInTheDocument());
  });

  it('Metadata mode calls searchByMetadata with the filter', async () => {
    const client = makeClient({
      searchByMetadata: vi.fn().mockResolvedValue({ results: [{ id: 'meta-1', version: 1 }], total_count: 1 }),
    });
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorsView);
    await chooseMode('metadata');
    await fireEvent.input(screen.getByPlaceholderText('{"lang":"en"}'), { target: { value: '{"lang":"en"}' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Run metadata search' }));
    await waitFor(() => expect(client.vectors.searchByMetadata).toHaveBeenCalledWith({
      namespace: 'docs', filters: { lang: 'en' }, limit: 10, include_metadata: true,
    }));
    await waitFor(() => expect(screen.getByText('meta-1')).toBeInTheDocument());
  });

  it('an invalid filter JSON shows an error and makes no call', async () => {
    const client = makeClient();
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorsView);
    await chooseMode('metadata');
    await fireEvent.input(screen.getByPlaceholderText('{"lang":"en"}'), { target: { value: '{not json' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Run metadata search' }));
    await waitFor(() => expect(screen.getByText(/Metadata filter must be a JSON object/)).toBeInTheDocument());
    expect(client.vectors.searchByMetadata).not.toHaveBeenCalled();
    expect(client.vectors.hybridSearch).not.toHaveBeenCalled();
  });
});
