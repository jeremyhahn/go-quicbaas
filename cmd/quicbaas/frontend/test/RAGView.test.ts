/**
 * RAGView.test.ts
 *
 * Verifies that RAGView renders correctly and the primary actions call the SDK
 * with the correct shape:
 *  - "Ingest Document" button is present and opens the dialog
 *  - "Run Query" button invokes client.rag.query with the right payload
 *  - Error states are surfaced when the SDK rejects
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mock SDK client
// ---------------------------------------------------------------------------

const { mockQuery, mockBuildContext } = vi.hoisted(() => ({
  mockQuery: vi.fn(),
  mockBuildContext: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    rag: {
      query: mockQuery,
      buildContext: mockBuildContext,
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  navigateTo: vi.fn(),
  routerStore: { route: { name: 'rag', params: {}, hash: '#/rag' } },
}));

import RAGView from '../src/views/ai/RAGView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('RAGView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockQuery.mockResolvedValue({
      context: 'Relevant context chunk here.',
      result_count: 2,
      tokens: 120,
      truncated: false,
      citations: [
        { index: 1, file_path: 'pkg/server/server.go', start_line: 10, end_line: 20, score: 0.92 },
      ],
    });
    mockBuildContext.mockResolvedValue({
      context: 'Built context.',
      result_count: 1,
      tokens: 50,
      truncated: false,
      citations: [],
    });
    vi.mocked(getSDKClient).mockResolvedValue({
      rag: { query: mockQuery, buildContext: mockBuildContext },
    } as never);
  });

  it('renders the "Ingest Document" button', async () => {
    render(RAGView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Ingest Document/i })).toBeInTheDocument();
    });
  });

  it('renders the "Run Query" button', async () => {
    render(RAGView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Run Query/i })).toBeInTheDocument();
    });
  });

  it('clicking "Ingest Document" opens the dialog', async () => {
    render(RAGView);
    await waitFor(() => screen.getByRole('button', { name: /Ingest Document/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Ingest Document/i }));
    expect(screen.getByRole('dialog', { name: /Ingest Document/i })).toBeInTheDocument();
  });

  it('calls client.rag.query with correct shape when Run Query is clicked', async () => {
    render(RAGView);
    await waitFor(() => screen.getByRole('button', { name: /Run Query/i }));

    // Fill in namespace and query
    const inputs = screen.getAllByRole('textbox');
    // First input should be namespace
    const namespaceInput = inputs.find((i) => (i as HTMLInputElement).placeholder === 'e.g. docs');
    expect(namespaceInput).toBeDefined();
    await fireEvent.input(namespaceInput!, { target: { value: 'testns' } });

    const textareas = screen.getAllByRole('textbox');
    const queryTA = textareas.find((el) => (el as HTMLTextAreaElement).placeholder?.includes('Ask a question'));
    expect(queryTA).toBeDefined();
    await fireEvent.input(queryTA!, { target: { value: 'How does TLS work?' } });

    await fireEvent.click(screen.getByRole('button', { name: /Run Query/i }));

    await waitFor(() => {
      expect(mockQuery).toHaveBeenCalledWith(
        expect.objectContaining({
          namespace: 'testns',
          query: 'How does TLS work?',
          include_citations: true,
        }),
      );
    });
  });

  it('displays query results after successful query', async () => {
    render(RAGView);
    await waitFor(() => screen.getByRole('button', { name: /Run Query/i }));

    const inputs = screen.getAllByRole('textbox');
    const ns = inputs.find((i) => (i as HTMLInputElement).placeholder === 'e.g. docs');
    await fireEvent.input(ns!, { target: { value: 'ns' } });
    const tas = screen.getAllByRole('textbox');
    const qa = tas.find((el) => (el as HTMLTextAreaElement).placeholder?.includes('Ask a question'));
    await fireEvent.input(qa!, { target: { value: 'query text' } });

    await fireEvent.click(screen.getByRole('button', { name: /Run Query/i }));

    await waitFor(() => {
      expect(screen.getByText(/Relevant context chunk here\./)).toBeInTheDocument();
    });
  });

  it('shows error message when query fails', async () => {
    mockQuery.mockRejectedValueOnce(new Error('server unavailable'));
    render(RAGView);
    await waitFor(() => screen.getByRole('button', { name: /Run Query/i }));

    const inputs = screen.getAllByRole('textbox');
    const ns = inputs.find((i) => (i as HTMLInputElement).placeholder === 'e.g. docs');
    await fireEvent.input(ns!, { target: { value: 'ns' } });
    const tas = screen.getAllByRole('textbox');
    const qa = tas.find((el) => (el as HTMLTextAreaElement).placeholder?.includes('Ask a question'));
    await fireEvent.input(qa!, { target: { value: 'anything' } });

    await fireEvent.click(screen.getByRole('button', { name: /Run Query/i }));

    await waitFor(() => {
      expect(screen.getByText(/server unavailable/i)).toBeInTheDocument();
    });
  });

  it('calls client.rag.buildContext when Ingest is submitted', async () => {
    render(RAGView);
    await waitFor(() => screen.getByRole('button', { name: /Ingest Document/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Ingest Document/i }));

    const dialog = screen.getByRole('dialog', { name: /Ingest Document/i });
    expect(dialog).toBeInTheDocument();

    // Fill in namespace in dialog
    const nsInputs = dialog.querySelectorAll('input[type="text"]');
    await fireEvent.input(nsInputs[0], { target: { value: 'mynamespace' } });

    // Fill in content textarea
    const ta = dialog.querySelector('textarea');
    expect(ta).toBeDefined();
    await fireEvent.input(ta!, { target: { value: 'Document body text.' } });

    await fireEvent.click(screen.getByRole('button', { name: /^Ingest$/i }));

    await waitFor(() => {
      expect(mockBuildContext).toHaveBeenCalledWith(
        expect.objectContaining({
          results: [expect.objectContaining({ content: 'Document body text.' })],
        }),
      );
    });
  });
});
