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
  authorization: string | null;
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
    const authorization = new Headers(init?.headers).get('Authorization');
    const call: FetchCall = { url, method, body, authorization };
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

// The barrier routes are permission-gated: the view's client must attach the
// logged-in user's bearer, which it reads from the auth store per request.
vi.mock('../src/stores/auth.svelte.ts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../src/stores/auth.svelte.ts')>()),
  getValidAccessToken: vi.fn(async () => 'barrier-test-token'),
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

  it('shamir strategy (uninitialized) shows the ceremony init form', async () => {
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
    expect(screen.getByLabelText('SO PIN')).toHaveAttribute('type', 'password');
    expect(screen.getByLabelText('User PIN')).toHaveAttribute('type', 'password');
    expect(screen.getAllByTestId('barrier-ceremony-officer')).toHaveLength(1);
    expect(screen.getByLabelText('Username')).toBeInTheDocument();
    expect(screen.getByLabelText('CSR (PEM)')).toBeInTheDocument();
    expect(screen.queryByLabelText(/Total shares/i)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Initialize Shamir barrier/i })).toBeDisabled();
    expect(screen.getByTestId('barrier-status-badge')).toHaveTextContent('shamir');
  });

  // The system barrier refuses a bare Shamir init (#1895), so the form runs the
  // initialization ceremony (POST /api/v1/init/initialize) with the threshold,
  // the PINs and the officers, and never calls /api/v1/barrier/shamir/initialize.
  it('shamir init runs the initialization ceremony (POST /api/v1/init/initialize)', async () => {
    const csr = '-----BEGIN CERTIFICATE REQUEST-----\nfake\n-----END CERTIFICATE REQUEST-----';
    spy = makeFetchSpy({
      '/api/v1/init/initialize': () => ({
        spki_pin: 'pin',
        ca_cert_pem: 'ca',
        tls_cert_pem: 'tls',
        state: 'initialized',
        share_count: 2,
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
    await fireEvent.input(screen.getByLabelText(/Threshold/i), { target: { value: '2' } });
    await fireEvent.input(screen.getByLabelText('SO PIN'), { target: { value: '123456' } });
    await fireEvent.input(screen.getByLabelText('User PIN'), { target: { value: '123456' } });
    await fireEvent.click(screen.getByRole('button', { name: /Add officer/i }));
    const usernames = screen.getAllByLabelText('Username');
    const csrs = screen.getAllByLabelText('CSR (PEM)');
    expect(usernames).toHaveLength(2);
    await fireEvent.input(usernames[0], { target: { value: 'alice' } });
    await fireEvent.input(csrs[0], { target: { value: csr } });
    await fireEvent.input(usernames[1], { target: { value: 'bob' } });
    await fireEvent.input(csrs[1], { target: { value: csr } });
    await fireEvent.click(screen.getByRole('button', { name: /Initialize Shamir barrier/i }));

    await waitFor(() => {
      expect(screen.getByTestId('barrier-ceremony-result')).toHaveTextContent(/2 shares sealed/);
    });
    expect(screen.getByTestId('barrier-ceremony-result')).toHaveTextContent('initialized');
    const hits = spy.calls.filter((c) => c.url.endsWith('/api/v1/init/initialize'));
    expect(hits).toHaveLength(1);
    expect(hits[0].method).toBe('POST');
    expect(hits[0].authorization).toBe('Bearer barrier-test-token');
    expect(hits[0].body).toEqual({
      threshold: 2,
      so_pin: '123456',
      user_pin: '123456',
      officers: [
        { username: 'alice', csr_pem: csr },
        { username: 'bob', csr_pem: csr },
      ],
    });
    expect(spy.calls.some((c) => c.url.includes('/api/v1/barrier/shamir/initialize'))).toBe(false);
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/seal/'))).toBe(true);
    // The ceremony seals each share to its officer: none reach the browser.
    expect(screen.queryAllByTestId('shamir-share-result')).toHaveLength(0);
  });

  it('shamir ceremony init renders a 400 in the form', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => ({ sealed: true, strategy: 'shamir' }),
    });
    const base = spy.mock.getMockImplementation()!;
    spy.mock.mockImplementation((url: string, init?: RequestInit) => {
      if (url.includes('/api/v1/init/initialize')) {
        spy.calls.push({
          url,
          method: (init?.method ?? 'GET').toUpperCase(),
          body: undefined,
          authorization: new Headers(init?.headers).get('Authorization'),
        });
        return Promise.resolve({
          ok: false,
          status: 400,
          statusText: 'Bad Request',
          headers: new Headers({ 'content-type': 'application/json' }),
          text: () => Promise.resolve(JSON.stringify({ error: 'threshold exceeds officers' })),
          json: () => Promise.resolve({ error: 'threshold exceeds officers' }),
        });
      }
      return base(url, init);
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Initialize Shamir barrier/i })).toBeInTheDocument();
    });
    await fireEvent.input(screen.getByLabelText(/Threshold/i), { target: { value: '1' } });
    await fireEvent.input(screen.getByLabelText('SO PIN'), { target: { value: '123456' } });
    await fireEvent.input(screen.getByLabelText('User PIN'), { target: { value: '123456' } });
    await fireEvent.input(screen.getByLabelText('Username'), { target: { value: 'alice' } });
    await fireEvent.input(screen.getByLabelText('CSR (PEM)'), {
      target: { value: '-----BEGIN CERTIFICATE REQUEST-----\nfake\n-----END CERTIFICATE REQUEST-----' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /Initialize Shamir barrier/i }));
    await waitFor(() => {
      expect(screen.getByTestId('barrier-ceremony-error')).toHaveTextContent(
        'threshold exceeds officers',
      );
    });
    expect(screen.queryByTestId('barrier-ceremony-result')).not.toBeInTheDocument();
    // Both PINs are cleared after a failed call.
    expect(screen.getByLabelText('SO PIN')).toHaveValue('');
    expect(screen.getByLabelText('User PIN')).toHaveValue('');
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

    // The structural BarrierUnsealForm submits via client.unseal which the
    // BarrierView adapter routes to the SDK's unsealWithShare → POST
    // /api/v1/barrier/shamir/unseal-share. We accept either: the regression
    // we care about is that /api/v1/seal is NEVER hit.
    await waitFor(() => {
      const ok =
        spy.calls.some((c) => c.url.endsWith('/api/v1/barrier/unseal')) ||
        spy.calls.some((c) => c.url.endsWith('/api/v1/barrier/shamir/unseal-share'));
      expect(ok).toBe(true);
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

describe('BarrierView operations card', () => {
  let spy: FetchSpy;
  let hasKeys: boolean;

  function opsSpy(extra: Record<string, (call: FetchCall) => unknown> = {}): FetchSpy {
    return makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({ strategy: 'shamir', sealed: false }),
      '/api/v1/barrier/recovery/status': () => ({ has_keys: hasKeys }),
      ...extra,
    });
  }

  function countHits(suffix: string, method: string): number {
    return spy.calls.filter((c) => c.url.endsWith(suffix) && c.method === method).length;
  }

  async function renderOps(): Promise<void> {
    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-operations')).toBeInTheDocument();
    });
  }

  beforeEach(() => {
    vi.clearAllMocks();
    hasKeys = false;
    spy = opsSpy();
    vi.stubGlobal('fetch', spy.mock);
  });

  it('is not shown while the barrier is sealed', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({ strategy: 'shamir', sealed: true }),
    });
    vi.stubGlobal('fetch', spy.mock);
    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-shamir-unseal')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('barrier-operations')).not.toBeInTheDocument();
  });

  it('seals only after the confirm and reloads the status', async () => {
    await renderOps();
    await fireEvent.click(screen.getByRole('button', { name: 'Seal barrier' }));
    expect(countHits('/api/v1/barrier/seal', 'POST')).toBe(0);
    expect(screen.getByTestId('barrier-op-confirm')).toHaveTextContent(
      /Every service that uses the barrier stops until it is unsealed/,
    );
    const before = countHits('/api/v1/barrier/status', 'GET');
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(countHits('/api/v1/barrier/seal', 'POST')).toBe(1);
      expect(countHits('/api/v1/barrier/status', 'GET')).toBeGreaterThan(before);
    });
    const { showToast } = await import('../src/stores/toast.svelte.ts');
    expect(showToast).toHaveBeenCalledWith(expect.objectContaining({ message: 'Barrier sealed' }));
  });

  it('cancel on the seal confirm does not seal', async () => {
    await renderOps();
    await fireEvent.click(screen.getByRole('button', { name: 'Seal barrier' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByTestId('barrier-op-confirm')).not.toBeInTheDocument();
    expect(countHits('/api/v1/barrier/seal', 'POST')).toBe(0);
  });

  it('toasts the message of a rejected seal', async () => {
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.endsWith('/api/v1/barrier/seal')) {
        return Promise.reject(new Error('seal exploded'));
      }
      return spy.mock(url, init);
    });
    vi.stubGlobal('fetch', fetchMock);
    await renderOps();
    await fireEvent.click(screen.getByRole('button', { name: 'Seal barrier' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    const { showToast } = await import('../src/stores/toast.svelte.ts');
    await waitFor(() => {
      expect(showToast).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'error', message: expect.stringContaining('seal exploded') }),
      );
    });
  });

  it('rekey sends the typed values, reports the distribution and clears the inputs', async () => {
    // The real wire shape: shamirInitToResponse, no share material.
    spy = opsSpy({
      '/api/v1/barrier/rekey': () => ({ threshold: 3, total_shares: 4, share_count: 4 }),
    });
    vi.stubGlobal('fetch', spy.mock);
    await renderOps();
    const threshold = screen.getByLabelText('New threshold') as HTMLInputElement;
    const total = screen.getByLabelText('New total shares') as HTMLInputElement;
    await fireEvent.input(threshold, { target: { value: '3' } });
    await fireEvent.input(total, { target: { value: '4' } });
    await fireEvent.submit(screen.getByTestId('barrier-rekey-form'));
    expect(countHits('/api/v1/barrier/rekey', 'POST')).toBe(0);
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(countHits('/api/v1/barrier/rekey', 'POST')).toBe(1);
    });
    const call = spy.calls.find((c) => c.url.endsWith('/api/v1/barrier/rekey'));
    expect(call?.body).toEqual({ threshold: 3, total: 4 });
    await waitFor(() => {
      expect(screen.getByTestId('barrier-rekey-result-summary')).toHaveTextContent(
        '4 shares distributed; any 3 of 4 reconstruct the key.',
      );
    });
    expect(screen.getByTestId('barrier-rekey-result')).toHaveTextContent(/share-claim flow/);
    expect(threshold.value).toBe('');
    expect(total.value).toBe('');
  });

  it('generate reports the recovery share distribution', async () => {
    // The real wire shape: recoveryKeysToResponse, no key material.
    spy = opsSpy({
      '/api/v1/barrier/recovery/generate': () => ({ threshold: 1, total: 2, share_count: 2 }),
    });
    vi.stubGlobal('fetch', spy.mock);
    await renderOps();
    await waitFor(() => {
      expect(screen.getByTestId('barrier-recovery-status')).toHaveTextContent(/No recovery keys/);
    });
    await fireEvent.input(screen.getByLabelText('Recovery threshold'), { target: { value: '1' } });
    await fireEvent.input(screen.getByLabelText('Recovery keys to generate'), { target: { value: '2' } });
    hasKeys = true;
    await fireEvent.submit(screen.getByTestId('barrier-recovery-form'));
    await waitFor(() => {
      expect(screen.getByTestId('barrier-recovery-result-summary')).toHaveTextContent(
        '2 shares distributed; any 1 of 2 reconstruct the key.',
      );
    });
    expect(screen.getByTestId('barrier-recovery-result')).toHaveTextContent(/share-claim flow/);
    const call = spy.calls.find((c) => c.url.endsWith('/api/v1/barrier/recovery/generate'));
    expect(call?.body).toEqual({ threshold: 1, total: 2 });
    await waitFor(() => {
      expect(screen.getByTestId('barrier-recovery-status')).toHaveTextContent('Recovery keys exist.');
    });
  });

  it('sends the logged-in bearer on every barrier operation', async () => {
    spy = opsSpy({
      '/api/v1/barrier/rekey': () => ({ threshold: 2, total_shares: 3, share_count: 3 }),
    });
    vi.stubGlobal('fetch', spy.mock);
    await renderOps();
    await fireEvent.input(screen.getByLabelText('New threshold'), { target: { value: '2' } });
    await fireEvent.input(screen.getByLabelText('New total shares'), { target: { value: '3' } });
    await fireEvent.submit(screen.getByTestId('barrier-rekey-form'));
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(countHits('/api/v1/barrier/rekey', 'POST')).toBe(1);
    });
    const paths = ['/api/v1/barrier/status', '/api/v1/barrier/recovery/status', '/api/v1/barrier/rekey'];
    for (const path of paths) {
      const hits = spy.calls.filter((c) => c.url.endsWith(path));
      expect(hits.length).toBeGreaterThan(0);
      for (const hit of hits) expect(hit.authorization).toBe('Bearer barrier-test-token');
    }
  });

  it('deletes recovery keys only after the confirm and refreshes the status', async () => {
    hasKeys = true;
    await renderOps();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Delete recovery keys' })).toBeInTheDocument();
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Delete recovery keys' }));
    expect(countHits('/api/v1/barrier/recovery', 'DELETE')).toBe(0);
    hasKeys = false;
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(countHits('/api/v1/barrier/recovery', 'DELETE')).toBe(1);
      expect(screen.getByTestId('barrier-recovery-status')).toHaveTextContent(/No recovery keys/);
    });
  });
});
