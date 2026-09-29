/**
 * TrustView.test.ts
 *
 * Verifies that TrustView mounts, calls crossSignedRootPool on init, renders
 * the root pool (or an empty message), and that the AIA auto-import and DANE
 * publish forms parse their PEM inputs and call the corresponding
 * TrustClient methods with the right arguments. Also verifies graceful
 * degradation when a fetch fails.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeBootstrap: vi.fn(),
}));

import TrustView from '../src/views/security/TrustView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const mockCrossSignedRootPool = vi.fn();
const mockAutoImportAIA = vi.fn();
const mockPublishDANE = vi.fn();

function mockClient(): void {
  vi.mocked(getSDKClient).mockResolvedValue({
    trust: {
      crossSignedRootPool: mockCrossSignedRootPool,
      autoImportAIA: mockAutoImportAIA,
      publishDANE: mockPublishDANE,
    },
  } as never);
}

const CERT_PEM = '-----BEGIN CERTIFICATE-----\nMOCKCERTDATA\n-----END CERTIFICATE-----';

beforeEach(() => {
  vi.clearAllMocks();
  mockCrossSignedRootPool.mockResolvedValue({ root_pems: [] });
  mockAutoImportAIA.mockResolvedValue({ imported: 0 });
  mockPublishDANE.mockResolvedValue({ published: 0 });
  mockClient();
  if (!URL.createObjectURL) {
    URL.createObjectURL = vi.fn(() => 'blob:mock');
  }
  if (!URL.revokeObjectURL) {
    URL.revokeObjectURL = vi.fn();
  }
});

describe('TrustView', () => {
  it('calls crossSignedRootPool on mount', async () => {
    render(TrustView);
    await waitFor(() => expect(mockCrossSignedRootPool).toHaveBeenCalled());
  });

  it('renders the page heading with icon', async () => {
    const { container } = render(TrustView);
    await waitFor(() => {
      const h1 = container.querySelector('h1');
      expect(h1).not.toBeNull();
      expect(h1!.textContent).toContain('Trust');
      expect(container.querySelector('header.page-header svg')).not.toBeNull();
    });
  });

  it('shows a message when no cross-signed roots exist', async () => {
    render(TrustView);
    await waitFor(() => {
      expect(screen.getByText(/no cross-signed roots configured/i)).toBeInTheDocument();
    });
  });

  it('renders each root when present', async () => {
    mockCrossSignedRootPool.mockResolvedValue({ root_pems: [CERT_PEM, CERT_PEM] });
    render(TrustView);
    await waitFor(() => {
      expect(screen.getByText('Root #1')).toBeInTheDocument();
      expect(screen.getByText('Root #2')).toBeInTheDocument();
    });
  });

  it('degrades gracefully when crossSignedRootPool fails', async () => {
    mockCrossSignedRootPool.mockRejectedValue(new Error('root pool unavailable'));
    render(TrustView);
    await waitFor(() => {
      expect(screen.getByText('root pool unavailable')).toBeInTheDocument();
    });
  });
});

describe('TrustView — AIA auto-import', () => {
  it('parses the pasted chain and calls autoImportAIA with each cert', async () => {
    mockAutoImportAIA.mockResolvedValue({ imported: 2 });
    const { container } = render(TrustView);
    await waitFor(() => expect(mockCrossSignedRootPool).toHaveBeenCalled());

    await fireEvent.input(container.querySelector('#aia-chain') as HTMLTextAreaElement, {
      target: { value: `${CERT_PEM}\n${CERT_PEM}` },
    });
    const importBtn = screen.getByRole('button', { name: /import issuing cas/i });
    await fireEvent.click(importBtn);

    await waitFor(() => expect(mockAutoImportAIA).toHaveBeenCalled());
    const req = mockAutoImportAIA.mock.calls[0][0];
    expect(req.chain_pems).toHaveLength(2);
    await waitFor(() => expect(screen.getByText(/imported 2 issuing ca/i)).toBeInTheDocument());
  });

  it('shows a client-side error when the chain textarea is empty', async () => {
    const { container } = render(TrustView);
    await waitFor(() => expect(container.querySelector('#aia-chain')).not.toBeNull());
    const importBtn = screen.getByRole('button', { name: /import issuing cas/i });
    await fireEvent.click(importBtn);
    await waitFor(() => expect(screen.getByText(/paste at least one/i)).toBeInTheDocument());
    expect(mockAutoImportAIA).not.toHaveBeenCalled();
  });
});

describe('TrustView — DANE publication', () => {
  it('publishes DANE records with name, port, and cert', async () => {
    mockPublishDANE.mockResolvedValue({ published: 1 });
    const { container } = render(TrustView);
    await fireEvent.input(container.querySelector('#dane-name') as HTMLInputElement, {
      target: { value: 'app.example.com' },
    });
    await fireEvent.input(container.querySelector('#dane-port') as HTMLInputElement, {
      target: { value: '8443' },
    });
    await fireEvent.input(container.querySelector('#dane-pem') as HTMLTextAreaElement, {
      target: { value: CERT_PEM },
    });
    const publishBtn = screen.getByRole('button', { name: /publish dane records/i });
    await fireEvent.click(publishBtn);

    await waitFor(() => expect(mockPublishDANE).toHaveBeenCalled());
    const req = mockPublishDANE.mock.calls[0][0];
    expect(req.name).toBe('app.example.com');
    expect(req.port).toBe(8443);
    expect(req.cert_pem).toBe(CERT_PEM);
    await waitFor(() => expect(screen.getByText(/published 1 tlsa/i)).toBeInTheDocument());
  });

  it('surfaces a typed "not configured" error from the server', async () => {
    mockPublishDANE.mockRejectedValue(new Error('DANE publisher not configured'));
    const { container } = render(TrustView);
    await fireEvent.input(container.querySelector('#dane-name') as HTMLInputElement, {
      target: { value: 'app.example.com' },
    });
    await fireEvent.input(container.querySelector('#dane-pem') as HTMLTextAreaElement, {
      target: { value: CERT_PEM },
    });
    const publishBtn = screen.getByRole('button', { name: /publish dane records/i });
    await fireEvent.click(publishBtn);

    await waitFor(() => expect(screen.getByText('DANE publisher not configured')).toBeInTheDocument());
  });
});
