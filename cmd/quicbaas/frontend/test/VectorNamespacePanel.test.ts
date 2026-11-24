// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import { APIError } from '@quicbaas/sdk';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/lib/download.ts', () => ({
  downloadText: vi.fn(),
}));

import VectorNamespacePanel from '../src/components/data/VectorNamespacePanel.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { downloadText } from '../src/lib/download.ts';

const info = {
  namespace: 'docs',
  dimensions: 768,
  distance_metric: 'cosine',
  index_type: 'hnsw',
  vector_count: 4210,
  indexed_count: 4200,
  storage_bytes: 1024,
  index_bytes: 512,
};

const stats = {
  namespace: 'docs',
  vector_count: 4210,
  indexed_count: 4200,
  storage_bytes: 1024,
  index_bytes: 512,
  avg_search_latency_ms: 1.5,
  search_count: 77,
};

function makeClient(overrides: Record<string, unknown> = {}) {
  return {
    vectors: {
      getNamespace: vi.fn().mockResolvedValue(info),
      getNamespaceStats: vi.fn().mockResolvedValue(stats),
      rebuildIndex: vi.fn().mockResolvedValue({ operation_id: 'op-r', started: 'now', estimated_duration_ms: 10 }),
      getIndexProgress: vi.fn(),
      ...(overrides.vectors as Record<string, unknown> | undefined),
    },
    vectorAdmin: {
      compactNamespace: vi.fn().mockResolvedValue({ operation_id: 'op-c', started: true }),
      getCompactionProgress: vi.fn(),
      exportVectors: vi.fn().mockResolvedValue([]),
      importVectors: vi.fn(),
      ...(overrides.vectorAdmin as Record<string, unknown> | undefined),
    },
  };
}

describe('VectorNamespacePanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('loads the namespace details and stats', async () => {
    const client = makeClient();
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorNamespacePanel, { props: { namespace: 'docs', onclose: vi.fn() } });
    await waitFor(() => {
      expect(screen.getByTestId('ns-dimension')).toHaveTextContent('768');
    });
    expect(screen.getByTestId('ns-metric')).toHaveTextContent('cosine');
    expect(screen.getByTestId('ns-vector-count')).toHaveTextContent('4210');
    expect(screen.getByTestId('ns-search-count')).toHaveTextContent('77');
    expect(client.vectors.getNamespace).toHaveBeenCalledWith('docs');
    expect(client.vectors.getNamespaceStats).toHaveBeenCalledWith('docs');
  });

  it('rebuilds the index and polls its progress until it completes', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const getIndexProgress = vi.fn()
      .mockResolvedValueOnce({ operation_id: 'op-r', namespace: 'docs', percent_complete: 40, status: 'running' })
      .mockResolvedValueOnce({ operation_id: 'op-r', namespace: 'docs', percent_complete: 100, status: 'completed' });
    const client = makeClient({ vectors: { getIndexProgress } });
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorNamespacePanel, { props: { namespace: 'docs', onclose: vi.fn() } });
    await waitFor(() => expect(screen.getByTestId('ns-dimension')).toHaveTextContent('768'));

    await fireEvent.click(screen.getByRole('button', { name: /rebuild index/i }));
    await waitFor(() => expect(client.vectors.rebuildIndex).toHaveBeenCalledWith('docs', { async: true }));
    await waitFor(() => expect(getIndexProgress).toHaveBeenCalledWith('docs', 'op-r'));
    await waitFor(() => expect(screen.getByTestId('index-progress')).toHaveTextContent('40%'));

    await vi.advanceTimersByTimeAsync(2000);
    await waitFor(() => expect(screen.getByTestId('index-progress')).toHaveTextContent(/completed/));
    expect(getIndexProgress).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(6000);
    expect(getIndexProgress).toHaveBeenCalledTimes(2);
  });

  it('compacts the namespace and polls the compaction progress', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const getCompactionProgress = vi.fn()
      .mockResolvedValueOnce({ operation_id: 'op-c', namespace: 'docs', percent_complete: 10, status: 'running' })
      .mockResolvedValueOnce({ operation_id: 'op-c', namespace: 'docs', percent_complete: 100, status: 'completed', space_reclaimed_bytes: 64 });
    const client = makeClient({ vectorAdmin: { getCompactionProgress } });
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorNamespacePanel, { props: { namespace: 'docs', onclose: vi.fn() } });
    await waitFor(() => expect(screen.getByTestId('ns-dimension')).toHaveTextContent('768'));

    await fireEvent.click(screen.getByRole('button', { name: /^compact/i }));
    await waitFor(() => expect(client.vectorAdmin.compactNamespace).toHaveBeenCalledWith('docs'));
    await waitFor(() => expect(getCompactionProgress).toHaveBeenCalledWith('op-c'));
    await waitFor(() => expect(screen.getByTestId('compact-progress')).toHaveTextContent('10%'));

    await vi.advanceTimersByTimeAsync(2000);
    await waitFor(() => expect(screen.getByTestId('compact-progress')).toHaveTextContent(/completed/));
    await vi.advanceTimersByTimeAsync(6000);
    expect(getCompactionProgress).toHaveBeenCalledTimes(2);
  });

  it('exports the vectors as JSON Lines through the download helper', async () => {
    const entries = [
      { id: 'a', vector: [1, 2] },
      { id: 'b', vector: [3, 4], metadata: { k: 'v' } },
    ];
    const client = makeClient({ vectorAdmin: { exportVectors: vi.fn().mockResolvedValue(entries) } });
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorNamespacePanel, { props: { namespace: 'docs', onclose: vi.fn() } });
    await waitFor(() => expect(screen.getByTestId('ns-dimension')).toHaveTextContent('768'));

    await fireEvent.click(screen.getByRole('button', { name: /export/i }));
    await waitFor(() => expect(downloadText).toHaveBeenCalled());
    expect(client.vectorAdmin.exportVectors).toHaveBeenCalledWith('docs');
    const [filename, content, mime] = vi.mocked(downloadText).mock.calls[0];
    expect(filename).toBe('docs.jsonl');
    expect(content).toBe('{"id":"a","vector":[1,2]}\n{"id":"b","vector":[3,4],"metadata":{"k":"v"}}\n');
    expect(mime).toBe('application/x-ndjson');
  });

  it('imports a JSON Lines file and shows the imported and failed counts', async () => {
    const importVectors = vi.fn().mockResolvedValue({
      vectors_imported: 2, vectors_skipped: 0, vectors_failed: 1, duration_ms: 5, timestamp: 'now',
    });
    const client = makeClient({ vectorAdmin: { importVectors } });
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorNamespacePanel, { props: { namespace: 'docs', onclose: vi.fn() } });
    await waitFor(() => expect(screen.getByTestId('ns-dimension')).toHaveTextContent('768'));

    const file = new File(['{"id":"a","vector":[1,2]}\n\n{"id":"b","vector":[3,4]}\n'], 'in.jsonl');
    await fireEvent.change(screen.getByLabelText(/import file/i), { target: { files: [file] } });
    await waitFor(() => expect(importVectors).toHaveBeenCalledWith('docs', [
      { id: 'a', vector: [1, 2] },
      { id: 'b', vector: [3, 4] },
    ]));
    await waitFor(() => expect(screen.getByTestId('import-result')).toHaveTextContent('2 imported'));
    expect(screen.getByTestId('import-result')).toHaveTextContent('1 failed');
  });

  it('rejects a malformed import line without calling the SDK', async () => {
    const client = makeClient();
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorNamespacePanel, { props: { namespace: 'docs', onclose: vi.fn() } });
    await waitFor(() => expect(screen.getByTestId('ns-dimension')).toHaveTextContent('768'));

    const file = new File(['{"id":"a","vector":[1,2]}\nnot json\n'], 'in.jsonl');
    await fireEvent.change(screen.getByLabelText(/import file/i), { target: { files: [file] } });
    await waitFor(() => expect(screen.getByTestId('import-error')).toHaveTextContent(/line 2/i));
    expect(client.vectorAdmin.importVectors).not.toHaveBeenCalled();
  });

  it.each([403, 404])('shows an inline error when getNamespace fails with %i', async (status) => {
    const client = makeClient({
      vectors: { getNamespace: vi.fn().mockRejectedValue(new APIError(status, 'namespace unavailable')) },
    });
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    render(VectorNamespacePanel, { props: { namespace: 'docs', onclose: vi.fn() } });
    await waitFor(() => expect(screen.getByTestId('ns-error')).toHaveTextContent('namespace unavailable'));
  });

  it('drops a response still in flight for the previous namespace', async () => {
    let resolveOld: (v: unknown) => void = () => {};
    const getNamespace = vi.fn((name: string) => {
      if (name === 'old') return new Promise((r) => { resolveOld = r; });
      return Promise.resolve({ ...info, namespace: 'new', dimensions: 32 });
    });
    const client = makeClient({ vectors: { getNamespace } });
    vi.mocked(getSDKClient).mockResolvedValue(client as never);
    const view = render(VectorNamespacePanel, { props: { namespace: 'old', onclose: vi.fn() } });
    await waitFor(() => expect(getNamespace).toHaveBeenCalledWith('old'));

    await view.rerender({ namespace: 'new', onclose: vi.fn() });
    await waitFor(() => expect(screen.getByTestId('ns-dimension')).toHaveTextContent('32'));
    resolveOld({ ...info, namespace: 'old', dimensions: 999 });
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.getByTestId('ns-dimension')).toHaveTextContent('32');
  });
});
