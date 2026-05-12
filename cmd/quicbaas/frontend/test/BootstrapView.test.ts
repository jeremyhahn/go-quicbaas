/**
 * BootstrapView.test.ts
 *
 * Verifies BootstrapView renders the auto-bootstrap wizard and that every
 * HTTP submit lands on /api/v1/bootstrap/{auto,dane,noise,spki}. The legacy
 * /api/v1/bootstrap (no method suffix) path must NOT be hit.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

interface FetchCall {
  url: string;
  method: string;
  body: unknown;
}

interface FetchSpy {
  calls: FetchCall[];
  mock: ReturnType<typeof vi.fn>;
}

function makeFetchSpy(
  responses: Record<string, (call: FetchCall) => unknown>,
): FetchSpy {
  const calls: FetchCall[] = [];
  const mock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const method = (init?.method ?? 'GET').toUpperCase();
    let body: unknown = undefined;
    if (init?.body !== undefined) {
      try { body = JSON.parse(init.body as string); }
      catch { body = init.body; }
    }
    const call: FetchCall = { url, method, body };
    calls.push(call);

    if (url.includes('/api/v1/seal/')) {
      throw new Error(`forbidden legacy URL hit: ${url}`);
    }
    if (url.includes('/api/v1/ceremony/')) {
      throw new Error(`forbidden legacy URL hit: ${url}`);
    }

    const keys = Object.keys(responses).sort((a, b) => b.length - a.length);
    for (const key of keys) {
      if (url.includes(key)) {
        const data = responses[key](call);
        return Promise.resolve({
          ok: true,
          status: 200,
          statusText: 'OK',
          text: () => Promise.resolve(JSON.stringify(data ?? {})),
          json: () => Promise.resolve(data ?? {}),
        });
      }
    }

    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: () => Promise.resolve('{}'),
      json: () => Promise.resolve({}),
    });
  });

  return { calls, mock };
}

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import BootstrapView from '../src/views/security/BootstrapView.svelte';

describe('BootstrapView', () => {
  let spy: FetchSpy;

  beforeEach(() => {
    vi.clearAllMocks();
    spy = makeFetchSpy({
      '/api/v1/bootstrap/auto': () => ({
        method: 'auto',
        bundle_pem: '-----BEGIN CERTIFICATE-----\nAUTO\n-----END CERTIFICATE-----',
        content_type: 'application/pem-certificate-chain',
      }),
      '/api/v1/bootstrap/dane': () => ({
        method: 'dane',
        bundle_pem: '-----BEGIN CERTIFICATE-----\nDANE\n-----END CERTIFICATE-----',
        content_type: 'application/pem-certificate-chain',
      }),
      '/api/v1/bootstrap/noise': () => ({
        method: 'noise',
        bundle_pem: '-----BEGIN CERTIFICATE-----\nNOISE\n-----END CERTIFICATE-----',
        content_type: 'application/pem-certificate-chain',
      }),
      '/api/v1/bootstrap/spki': () => ({
        method: 'spki',
        bundle_pem: '-----BEGIN CERTIFICATE-----\nSPKI\n-----END CERTIFICATE-----',
        content_type: 'application/pem-certificate-chain',
      }),
    });
    vi.stubGlobal('fetch', spy.mock);
  });

  it('renders the "Bootstrap" heading', () => {
    render(BootstrapView);
    expect(screen.getByRole('heading', { name: 'Bootstrap' })).toBeInTheDocument();
  });

  it('renders the method picker with all four methods', () => {
    render(BootstrapView);
    expect(screen.getByTestId('bootstrap-method-picker')).toBeInTheDocument();
    expect(screen.getByTestId('bootstrap-method-radio-spki-pin')).toBeInTheDocument();
    expect(screen.getByTestId('bootstrap-method-radio-dane')).toBeInTheDocument();
    expect(screen.getByTestId('bootstrap-method-radio-noise')).toBeInTheDocument();
    expect(screen.getByTestId('bootstrap-method-radio-direct')).toBeInTheDocument();
  });

  it('defaults to spki-pin selected', () => {
    render(BootstrapView);
    const radio = screen.getByTestId('bootstrap-method-radio-spki-pin') as HTMLInputElement;
    expect(radio.checked).toBe(true);
  });

  it('shows the server URL input', () => {
    render(BootstrapView);
    expect(screen.getByLabelText(/Server URL/i)).toBeInTheDocument();
  });

  it('shows the material input for spki-pin / dane / noise but hides it for direct', async () => {
    render(BootstrapView);
    expect(screen.getByLabelText(/SPKI Pin value/i)).toBeInTheDocument();

    await fireEvent.click(screen.getByTestId('bootstrap-method-radio-direct'));
    await waitFor(() => {
      expect(screen.queryByLabelText(/SPKI Pin value/i)).not.toBeInTheDocument();
      expect(screen.getByText(/No additional material required/i)).toBeInTheDocument();
    });
  });

  it('switching method updates the material input label', async () => {
    render(BootstrapView);

    await fireEvent.click(screen.getByTestId('bootstrap-method-radio-dane'));
    await waitFor(() => {
      expect(screen.getByLabelText(/DANE value/i)).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByTestId('bootstrap-method-radio-noise'));
    await waitFor(() => {
      expect(screen.getByLabelText(/Noise value/i)).toBeInTheDocument();
    });
  });

  it('Bootstrap submit button is disabled until server URL and material are filled', async () => {
    render(BootstrapView);
    const btn = screen.getByRole('button', { name: /^Bootstrap$/i });
    expect(btn).toBeDisabled();

    const url = screen.getByLabelText(/Server URL/i);
    await fireEvent.input(url, { target: { value: 'https://daemon.test:9443' } });
    expect(btn).toBeDisabled();

    const material = screen.getByLabelText(/SPKI Pin value/i);
    await fireEvent.input(material, { target: { value: 'sha256/abcdef' } });
    await waitFor(() => {
      expect(btn).not.toBeDisabled();
    });
  });

  it('direct method only requires the server URL', async () => {
    render(BootstrapView);
    await fireEvent.click(screen.getByTestId('bootstrap-method-radio-direct'));

    const btn = screen.getByRole('button', { name: /^Bootstrap$/i });
    expect(btn).toBeDisabled();

    await fireEvent.input(screen.getByLabelText(/Server URL/i), {
      target: { value: 'https://daemon.test:9443' },
    });
    await waitFor(() => {
      expect(btn).not.toBeDisabled();
    });
  });

  it('SPKI submission posts to /api/v1/bootstrap/spki', async () => {
    render(BootstrapView);
    await fireEvent.input(screen.getByLabelText(/Server URL/i), {
      target: { value: 'https://daemon.test:9443' },
    });
    await fireEvent.input(screen.getByLabelText(/SPKI Pin value/i), {
      target: { value: 'sha256/abcdef' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /^Bootstrap$/i }));

    await waitFor(() => {
      const hits = spy.calls.filter((c) => c.url.endsWith('/api/v1/bootstrap/spki'));
      expect(hits.length).toBeGreaterThan(0);
      expect(hits[0].method).toBe('POST');
    });
  });

  it('DANE submission posts to /api/v1/bootstrap/dane', async () => {
    render(BootstrapView);
    await fireEvent.click(screen.getByTestId('bootstrap-method-radio-dane'));
    await fireEvent.input(screen.getByLabelText(/Server URL/i), {
      target: { value: 'https://daemon.test:9443' },
    });
    await fireEvent.input(screen.getByLabelText(/DANE value/i), {
      target: { value: '3 1 1 abcdef' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /^Bootstrap$/i }));

    await waitFor(() => {
      const hits = spy.calls.filter((c) => c.url.endsWith('/api/v1/bootstrap/dane'));
      expect(hits.length).toBeGreaterThan(0);
      expect(hits[0].method).toBe('POST');
    });
  });

  it('Noise submission posts to /api/v1/bootstrap/noise', async () => {
    render(BootstrapView);
    await fireEvent.click(screen.getByTestId('bootstrap-method-radio-noise'));
    await fireEvent.input(screen.getByLabelText(/Server URL/i), {
      target: { value: 'https://daemon.test:9443' },
    });
    await fireEvent.input(screen.getByLabelText(/Noise value/i), {
      target: { value: 'abcd1234' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /^Bootstrap$/i }));

    await waitFor(() => {
      const hits = spy.calls.filter((c) => c.url.endsWith('/api/v1/bootstrap/noise'));
      expect(hits.length).toBeGreaterThan(0);
      expect(hits[0].method).toBe('POST');
    });
  });

  it('Direct submission posts to /api/v1/bootstrap/auto', async () => {
    render(BootstrapView);
    await fireEvent.click(screen.getByTestId('bootstrap-method-radio-direct'));
    await fireEvent.input(screen.getByLabelText(/Server URL/i), {
      target: { value: 'https://daemon.test:9443' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /^Bootstrap$/i }));

    await waitFor(() => {
      const hits = spy.calls.filter((c) => c.url.endsWith('/api/v1/bootstrap/auto'));
      expect(hits.length).toBeGreaterThan(0);
      expect(hits[0].method).toBe('POST');
    });
  });

  // ---------------------------------------------------------------------------
  // Issue #83: radio buttons must be visible in dark theme (radio-primary class)
  // ---------------------------------------------------------------------------

  it('all four method radio inputs carry the radio-primary class (#83)', () => {
    const { container } = render(BootstrapView);
    const radios = container.querySelectorAll('input[type="radio"][name="bootstrap-method"]');
    expect(radios).toHaveLength(4);
    for (const radio of Array.from(radios)) {
      expect((radio as HTMLElement).className).toContain('radio-primary');
    }
  });

  it('regression: never calls /api/v1/seal/* or /api/v1/ceremony/*', async () => {
    render(BootstrapView);
    await fireEvent.input(screen.getByLabelText(/Server URL/i), {
      target: { value: 'https://daemon.test:9443' },
    });
    await fireEvent.input(screen.getByLabelText(/SPKI Pin value/i), {
      target: { value: 'sha256/abcdef' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /^Bootstrap$/i }));

    await waitFor(() => {
      expect(spy.calls.length).toBeGreaterThan(0);
    });
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/seal/'))).toBe(true);
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/ceremony/'))).toBe(true);
  });
});
