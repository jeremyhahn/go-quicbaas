/**
 * EmbeddingsView.test.ts
 *
 * Verifies that EmbeddingsView renders correctly and the primary actions call
 * the SDK with the correct shape:
 *  - Provider table is populated from client.embeddings.listProviders on mount
 *  - "Generate" button invokes client.embeddings.generate with the right payload
 *  - Results (dimensions, vector preview) surface after a successful generation
 *  - Error states are shown when the SDK rejects
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mock SDK client
// ---------------------------------------------------------------------------

const { mockGenerate, mockListProviders } = vi.hoisted(() => ({
  mockGenerate: vi.fn(),
  mockListProviders: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    embeddings: {
      generate: mockGenerate,
      listProviders: mockListProviders,
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  navigateTo: vi.fn(),
  routerStore: { route: { name: 'embeddings', params: {}, hash: '#/embeddings' } },
}));

import EmbeddingsView from '../src/views/ai/EmbeddingsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('EmbeddingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListProviders.mockResolvedValue({
      providers: [
        { name: 'openai-ada', type: 'openai', model: 'text-embedding-ada-002', dimensions: 1536, status: 'available' },
        { name: 'local-minilm', type: 'local', model: 'all-MiniLM-L6-v2', dimensions: 384, status: 'available' },
      ],
      default_provider: 'openai-ada',
    });
    mockGenerate.mockResolvedValue({
      embedding: [0.123456, -0.234567, 0.345678, -0.456789, 0.567890, -0.678901, 0.789012, -0.890123, 0.901234],
      metadata: {
        provider: 'openai-ada',
        model: 'text-embedding-ada-002',
        cache_hit: false,
      },
    });
    vi.mocked(getSDKClient).mockResolvedValue({
      embeddings: { generate: mockGenerate, listProviders: mockListProviders },
    } as never);
  });

  it('calls listProviders on mount', async () => {
    render(EmbeddingsView);
    await waitFor(() => {
      expect(mockListProviders).toHaveBeenCalledWith(
        expect.objectContaining({ include_unavailable: false }),
      );
    });
  });

  it('renders provider names from listProviders response', async () => {
    render(EmbeddingsView);
    await waitFor(() => {
      // Provider names appear in both table and select — assert at least one instance exists
      expect(screen.getAllByText('openai-ada').length).toBeGreaterThan(0);
      expect(screen.getAllByText('local-minilm').length).toBeGreaterThan(0);
    });
  });

  it('renders the "Generate" button', async () => {
    render(EmbeddingsView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Generate Embedding/i })).toBeInTheDocument();
    });
  });

  it('calls client.embeddings.generate with correct shape when Generate is clicked', async () => {
    render(EmbeddingsView);
    await waitFor(() => screen.getByRole('button', { name: /Generate Embedding/i }));

    // Fill in the input text textarea
    const ta = screen.getByPlaceholderText(/Enter text to embed/i);
    expect(ta).toBeDefined();
    await fireEvent.input(ta, { target: { value: 'Hello, embedding world!' } });

    await fireEvent.click(screen.getByRole('button', { name: /Generate Embedding/i }));

    await waitFor(() => {
      expect(mockGenerate).toHaveBeenCalledWith(
        expect.objectContaining({
          text: 'Hello, embedding world!',
        }),
      );
    });
  });

  it('displays dimensions and vector preview after successful generation', async () => {
    render(EmbeddingsView);
    await waitFor(() => screen.getByRole('button', { name: /Generate Embedding/i }));

    const ta = screen.getByPlaceholderText(/Enter text to embed/i);
    await fireEvent.input(ta, { target: { value: 'test input' } });
    await fireEvent.click(screen.getByRole('button', { name: /Generate Embedding/i }));

    await waitFor(() => {
      // Dimensions label appears in both table header and result panel — at least one must exist
      expect(screen.getAllByText(/Dimensions/i).length).toBeGreaterThan(0);
      // Vector preview section heading
      expect(screen.getByText(/Vector Preview/i)).toBeInTheDocument();
    });
  });

  it('shows error message when generate fails', async () => {
    mockGenerate.mockRejectedValueOnce(new Error('model not loaded'));
    render(EmbeddingsView);
    await waitFor(() => screen.getByRole('button', { name: /Generate Embedding/i }));

    const ta = screen.getByPlaceholderText(/Enter text to embed/i);
    await fireEvent.input(ta, { target: { value: 'anything' } });
    await fireEvent.click(screen.getByRole('button', { name: /Generate Embedding/i }));

    await waitFor(() => {
      expect(screen.getByText(/model not loaded/i)).toBeInTheDocument();
    });
  });

  it('shows error when Generate is clicked with empty input', async () => {
    render(EmbeddingsView);
    await waitFor(() => screen.getByRole('button', { name: /Generate Embedding/i }));

    // Click Generate without entering any text
    await fireEvent.click(screen.getByRole('button', { name: /Generate Embedding/i }));

    await waitFor(() => {
      expect(screen.getByText(/Input text is required/i)).toBeInTheDocument();
    });
    expect(mockGenerate).not.toHaveBeenCalled();
  });

  it('shows error alert when listProviders fails', async () => {
    mockListProviders.mockRejectedValueOnce(new Error('provider registry unavailable'));
    render(EmbeddingsView);
    await waitFor(() => {
      expect(screen.getByText(/provider registry unavailable/i)).toBeInTheDocument();
    });
  });

  /**
   * Regression: the "default" provider badge previously rendered as
   * `badge-primary`, which paints with the brand red and reads as a
   * destructive/error tag. The badge MUST NOT use any destructive class
   * (badge-error, badge-primary on the brand-red theme, or any explicit
   * red background utility).
   */
  it('renders the "default" provider badge with a non-destructive style', async () => {
    render(EmbeddingsView);

    const badge = await screen.findByTestId('default-provider-badge');
    expect(badge).toBeInTheDocument();
    expect(badge.textContent?.trim()).toBe('default');

    const cls = badge.className;
    expect(cls).not.toMatch(/\bbadge-error\b/);
    expect(cls).not.toMatch(/\bbadge-primary\b/);
    expect(cls).not.toMatch(/\bbg-red-/);
    expect(cls).not.toMatch(/\bbg-error\b/);
    // The badge must still be visually marked — assert at least one
    // non-destructive style hook is present.
    expect(cls).toMatch(/\bbadge-success\b|\bbadge-outline\b|\bbadge-ghost\b|\bbadge-neutral\b/);
  });
});
