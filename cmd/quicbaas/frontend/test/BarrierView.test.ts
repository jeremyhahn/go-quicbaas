/**
 * BarrierView.test.ts
 *
 * Verifies that BarrierView renders the correct strategy-aware UI AND that
 * every HTTP call lands on the canonical /api/v1/barrier/* surface. The view
 * must NEVER hit /api/v1/seal/* — that path is gone.
 *
 * The view talks to the daemon via the typed @quicbaas/sdk RestBarrierClient
 * (constructed by createBarrierClient). We mock global fetch and assert each
 * call URL matches the expected canonical /api/v1/barrier/* path.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

interface RawBarrierStatus {
  sealed: boolean;
  initialized?: boolean;
  strategy: string;
  threshold?: number;
  total_shares?: number;
  shares_provided?: number;
  version?: number;
  hardware_backed?: boolean;
  initialized_at?: string;
}

function makeStatus(over: Partial<RawBarrierStatus> = {}): RawBarrierStatus {
  return {
    sealed: true,
    initialized: true,
    strategy: 'shamir',
    threshold: 2,
    total_shares: 3,
    shares_provided: 0,
    version: 1,
    initialized_at: '2025-01-01T00:00:00Z',
    ...over,
  };
}

// FetchSpy records every URL the view hits so tests can assert canonical
// /api/v1/barrier/* paths and prove no /api/v1/seal/* leaks.
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

    // FORBID legacy paths up-front — fail loudly if any code regresses.
    if (url.includes('/api/v1/seal/')) {
      throw new Error(`forbidden legacy URL hit: ${url}`);
    }
    if (url.includes('/api/v1/ceremony/')) {
      throw new Error(`forbidden legacy URL hit: ${url}`);
    }

    // Match the response handler by URL substring. Order: longest-first.
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

    // Default: empty 200.
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

// ----------------------------------------------------------------------------

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import BarrierView from '../src/views/security/BarrierView.svelte';

describe('BarrierView strategy-aware rendering', () => {
  let spy: FetchSpy;

  beforeEach(() => {
    vi.clearAllMocks();
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus(),
    });
    vi.stubGlobal('fetch', spy.mock);
  });

  it('renders the "Barrier" page heading', async () => {
    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Barrier' })).toBeInTheDocument();
    });
  });

  it('hits /api/v1/barrier/status (never /api/v1/seal/status)', async () => {
    render(BarrierView);
    await waitFor(() => {
      expect(spy.calls.some((c) => c.url.endsWith('/api/v1/barrier/status'))).toBe(true);
    });
    // Regression guard.
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/seal/'))).toBe(true);
  });

  it('disabled strategy shows the disabled banner', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({ strategy: 'disabled', initialized: false }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-disabled')).toBeInTheDocument();
    });
    expect(screen.getByTestId('barrier-status-badge')).toHaveTextContent('disabled');
  });

  it('software strategy shows a single password field when sealed', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({ strategy: 'software', sealed: true }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-software')).toBeInTheDocument();
    });
    expect(screen.getByLabelText(/Unseal password/i)).toBeInTheDocument();
    expect(screen.getByTestId('barrier-status-badge')).toHaveTextContent('software');
  });

  it('shamir strategy (uninitialized) shows the init form with threshold/total fields', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => ({
        sealed: true,
        strategy: 'shamir',
        // Omit initialized_at to flag the daemon as uninitialized.
      }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-shamir-init')).toBeInTheDocument();
    });
    expect(screen.getByLabelText(/Threshold/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Total shares/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Initialize Shamir barrier/i })).toBeInTheDocument();
    expect(screen.getByTestId('barrier-status-badge')).toHaveTextContent('shamir');
  });

  it('shamir init posts to /api/v1/barrier/shamir/initialize', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/shamir/initialize': () => ({
        shares: ['s1', 's2', 's3'],
        threshold: 2,
        total_shares: 3,
      }),
      '/api/v1/barrier/status': () => ({
        sealed: true,
        strategy: 'shamir',
      }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Initialize Shamir barrier/i })).toBeInTheDocument();
    });
    await fireEvent.click(screen.getByRole('button', { name: /Initialize Shamir barrier/i }));

    await waitFor(() => {
      const hits = spy.calls.filter((c) => c.url.endsWith('/api/v1/barrier/shamir/initialize'));
      expect(hits.length).toBeGreaterThan(0);
      expect(hits[0].method).toBe('POST');
    });
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/seal/'))).toBe(true);
  });

  it('software unseal posts to /api/v1/barrier/unseal', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({ strategy: 'software', sealed: true }),
      '/api/v1/barrier/unseal': () => ({}),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByLabelText(/Unseal password/i)).toBeInTheDocument();
    });
    await fireEvent.input(screen.getByLabelText(/Unseal password/i), {
      target: { value: 'secret-pw' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /Unseal/i }));

    // The passphrase goes to the direct unseal route, base64 encoded.
    await waitFor(() => {
      const hit = spy.calls.find((c) => c.url.endsWith('/api/v1/barrier/unseal'));
      expect(hit).toMatchObject({ method: 'POST', body: { secret: btoa('secret-pw') } });
    });
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/seal/'))).toBe(true);
  });

  it('shamir strategy (initialized + sealed) shows share input + progress meter', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({
        strategy: 'shamir',
        sealed: true,
        threshold: 3,
        total_shares: 5,
        shares_provided: 1,
      }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-shamir-unseal')).toBeInTheDocument();
    });
    expect(screen.getByTestId('shamir-share-input')).toBeInTheDocument();
    expect(screen.getByTestId('shamir-progress-bar')).toBeInTheDocument();
    expect(screen.getByTestId('shamir-progress-text')).toHaveTextContent('1 / 3');
  });

  it('shamir unseal answers a quorum challenge per holder', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({ strategy: 'shamir', sealed: true, threshold: 2 }),
      '/api/v1/barrier/quorum/begin': () => ({
        challenge_id: 'ch-1',
        domain: 'unseal',
        op: 'unseal',
        username: 'officer-1',
        factor: 'mtls',
        nonce: 'bm9uY2U=',
        expires_at: '2026-10-25T00:05:00Z',
      }),
      '/api/v1/barrier/quorum/submit': () => ({ domain: 'unseal', op: 'unseal', required: 2, submitted: 1, complete: false }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await screen.findByTestId('shamir-share-input');

    await fireEvent.input(screen.getByLabelText(/Officer username/i), { target: { value: 'officer-1' } });
    await fireEvent.click(screen.getByRole('button', { name: /Request challenge/i }));

    expect(await screen.findByTestId('shamir-challenge')).toHaveTextContent('ch-1');
    expect(screen.getByTestId('shamir-challenge')).toHaveTextContent('bm9uY2U=');
    expect(spy.calls.find((c) => c.url.endsWith('/api/v1/barrier/quorum/begin'))).toMatchObject({
      method: 'POST',
      body: { domain: 'unseal', op: 'unseal', username: 'officer-1' },
    });

    await fireEvent.input(screen.getByLabelText(/^Share$/i), { target: { value: 'c2hhcmU=' } });
    await fireEvent.input(screen.getByLabelText(/Signed response/i), { target: { value: 'c2ln' } });
    await fireEvent.click(screen.getByRole('button', { name: /Submit share/i }));

    await waitFor(() => {
      expect(screen.getByTestId('shamir-progress-text')).toHaveTextContent('1 / 2');
    });
    expect(spy.calls.find((c) => c.url.endsWith('/api/v1/barrier/quorum/submit'))).toMatchObject({
      method: 'POST',
      body: {
        domain: 'unseal',
        op: 'unseal',
        username: 'officer-1',
        challenge_id: 'ch-1',
        factor: 'mtls',
        response: 'c2ln',
        share: 'c2hhcmU=',
      },
    });
    expect(spy.calls.some((c) => c.url.includes('/unseal-share'))).toBe(false);
  });

  it('shamir unseal shows the server refusal and keeps the progress', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({ strategy: 'shamir', sealed: true, threshold: 2 }),
    });
    const answer = spy.mock.getMockImplementation()!;
    spy.mock.mockImplementation((url: string, init?: RequestInit) => {
      if (url.endsWith('/api/v1/barrier/quorum/begin')) {
        return Promise.resolve({
          ok: false,
          status: 403,
          statusText: 'Forbidden',
          headers: new Headers({ 'Content-Type': 'application/json' }),
          text: () => Promise.resolve(JSON.stringify({ error: 'not an officer' })),
          json: () => Promise.resolve({ error: 'not an officer' }),
        });
      }
      return answer(url, init);
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await screen.findByTestId('shamir-share-input');
    await fireEvent.input(screen.getByLabelText(/Officer username/i), { target: { value: 'mallory' } });
    await fireEvent.click(screen.getByRole('button', { name: /Request challenge/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/not an officer/);
    expect(screen.queryByTestId('shamir-challenge')).toBeNull();
    expect(screen.getByTestId('shamir-progress-text')).toHaveTextContent('0 / 2');
  });

  it('tpm2 strategy shows auto-init / auto-unseal banner and hardware_backed badge', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({
        strategy: 'tpm2',
        hardware_backed: true,
        sealed: false,
      }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-auto')).toBeInTheDocument();
    });
    expect(screen.getByText(/Auto-init at boot/i)).toBeInTheDocument();
    expect(screen.getByTestId('barrier-auto-unseal-active')).toBeInTheDocument();
    const badge = screen.getByTestId('barrier-status-badge');
    expect(badge).toHaveTextContent('tpm2');
    expect(badge).toHaveTextContent('hardware_backed');
  });

  it('pkcs11 strategy classifies as hardware_backed and uses auto banner', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({
        strategy: 'pkcs11',
        hardware_backed: true,
        sealed: false,
      }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-auto')).toBeInTheDocument();
    });
    expect(screen.getByTestId('barrier-status-badge')).toHaveTextContent('pkcs11');
  });

  it('aws-kms strategy shows the auto banner with the strategy in the badge', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({
        strategy: 'aws-kms',
        hardware_backed: true,
        sealed: false,
      }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-auto')).toBeInTheDocument();
    });
    expect(screen.getByTestId('barrier-status-badge')).toHaveTextContent('aws-kms');
  });

  it('regression: never calls /api/v1/seal/* across any strategy', async () => {
    const strategies = ['software', 'shamir', 'tpm2', 'pkcs11', 'aws-kms', 'disabled'];
    for (const strategy of strategies) {
      const localSpy = makeFetchSpy({
        '/api/v1/barrier/status': () => makeStatus({ strategy }),
      });
      vi.stubGlobal('fetch', localSpy.mock);
      const { unmount } = render(BarrierView);
      await waitFor(() => {
        expect(localSpy.calls.length).toBeGreaterThan(0);
      });
      expect(localSpy.calls.every((c) => !c.url.includes('/api/v1/seal/'))).toBe(true);
      expect(localSpy.calls.every((c) => !c.url.includes('/api/v1/ceremony/'))).toBe(true);
      unmount();
    }
  });
});
