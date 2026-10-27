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
import { render, screen, fireEvent, waitFor, within } from '@testing-library/svelte';

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

// sealedStatus is the sealed system barrier's status as served: qrdb's
// BarrierService.sealedStatus reports only sealed and initialized, and
// barrierStatusToResponse (pkg/api/rest/handlers_barrier.go) forwards its
// empty strategy unchanged.
function sealedStatus(initialized: boolean): RawBarrierStatus {
  return { sealed: true, initialized, strategy: '', hardware_backed: false };
}

// ceremonyBarrier models the system barrier across the initialization
// ceremony: uninitialized until the ceremony answers, initialized after.
function ceremonyBarrier(): {
  status: () => RawBarrierStatus;
  initialize: <T>(resp: T) => T;
} {
  let initialized = false;
  return {
    status: () => sealedStatus(initialized),
    initialize: (resp) => {
      initialized = true;
      return resp;
    },
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

const CA_PEM = '-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----';
const TLS_PEM = '-----BEGIN CERTIFICATE-----\ntls\n-----END CERTIFICATE-----';
const ADMIN_CSR_PEM = '-----BEGIN CERTIFICATE REQUEST-----\nadmin\n-----END CERTIFICATE REQUEST-----';
const ADMIN_CERT_PEM = '-----BEGIN CERTIFICATE-----\nadmin\n-----END CERTIFICATE-----';
const ADMIN_KEY_PEM = '-----BEGIN PRIVATE KEY-----\nadmin\n-----END PRIVATE KEY-----';
const OFFICERS = [
  { username: 'alice', csr_pem: '-----BEGIN CERTIFICATE REQUEST-----\nalice\n-----END CERTIFICATE REQUEST-----' },
  { username: 'bob', csr_pem: '-----BEGIN CERTIFICATE REQUEST-----\nbob\n-----END CERTIFICATE REQUEST-----' },
];

// fillCeremonyForm completes the system initialization ceremony form: the
// PINs, a 2-of-2 officer roster and one administrator credential selector.
async function fillCeremonyForm(opts: {
  adminCSR?: string;
  generate?: boolean;
  officers?: string;
}): Promise<void> {
  await screen.findByRole('button', { name: /Initialize Shamir barrier/i });
  await fireEvent.input(screen.getByLabelText(/Threshold/i), { target: { value: '2' } });
  await fireEvent.input(screen.getByLabelText(/Security Officer PIN/i), {
    target: { value: 'so-pin-1234' },
  });
  await fireEvent.input(screen.getByLabelText(/User PIN/i), { target: { value: 'user-pin-1234' } });
  await fireEvent.input(screen.getByLabelText(/Officers/i), {
    target: { value: opts.officers ?? JSON.stringify(OFFICERS) },
  });
  if (opts.generate) {
    await fireEvent.change(screen.getByLabelText(/Administrator credential/i), {
      target: { value: 'generate' },
    });
  } else {
    await fireEvent.input(screen.getByLabelText(/Administrator CSR/i), {
      target: { value: opts.adminCSR ?? '' },
    });
  }
}

// ----------------------------------------------------------------------------

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));
vi.mock('../src/lib/download.ts', () => ({ downloadText: vi.fn() }));

import BarrierView from '../src/views/security/BarrierView.svelte';
import { downloadText } from '../src/lib/download.ts';

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
        initialized: false,
      }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-shamir-init')).toBeInTheDocument();
    });
    expect(screen.getByLabelText(/Threshold/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Security Officer PIN/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/User PIN/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Officers/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Administrator credential/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Initialize Shamir barrier/i })).toBeInTheDocument();
    expect(screen.getByTestId('barrier-status-badge')).toHaveTextContent('shamir');
  });

  // The system barrier is initialized only by the qrdb initialization
  // ceremony; /api/v1/barrier/shamir/initialize refuses with 412.
  it('shamir init runs the initialization ceremony at /api/v1/init/initialize', async () => {
    spy = makeFetchSpy({
      '/api/v1/init/initialize': () => ({
        spki_pin: 'pin',
        ca_cert_pem: CA_PEM,
        tls_cert_pem: TLS_PEM,
        state: 'initialized',
        share_count: 2,
        admin_cert_pem: ADMIN_CERT_PEM,
        admin_credential_source: 'csr-sign',
      }),
      '/api/v1/barrier/status': () => ({ sealed: true, strategy: 'shamir' }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await fillCeremonyForm({ adminCSR: ADMIN_CSR_PEM });
    await fireEvent.click(screen.getByRole('button', { name: /Initialize Shamir barrier/i }));

    await waitFor(() => {
      expect(spy.calls.some((c) => c.url.endsWith('/api/v1/init/initialize'))).toBe(true);
    });
    const call = spy.calls.find((c) => c.url.endsWith('/api/v1/init/initialize'));
    expect(call?.method).toBe('POST');
    expect(call?.body).toEqual({
      so_pin: 'so-pin-1234',
      user_pin: 'user-pin-1234',
      threshold: 2,
      officers: OFFICERS,
      admin_csr_pem: ADMIN_CSR_PEM,
    });
    expect(spy.calls.some((c) => c.url.includes('/api/v1/barrier/shamir/initialize'))).toBe(false);
  });

  it('delivers the administrator credentials the ceremony issues', async () => {
    const barrier = ceremonyBarrier();
    spy = makeFetchSpy({
      '/api/v1/init/initialize': () => barrier.initialize({
        spki_pin: 'pin',
        ca_cert_pem: CA_PEM,
        tls_cert_pem: TLS_PEM,
        state: 'initialized',
        share_count: 2,
        admin_cert_pem: ADMIN_CERT_PEM,
        // []byte on the wire: JSON carries it base64-encoded.
        admin_key_pem: btoa(ADMIN_KEY_PEM),
        admin_credential_source: 'generated',
      }),
      '/api/v1/barrier/status': () => barrier.status(),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await fillCeremonyForm({ generate: true });
    await fireEvent.click(screen.getByRole('button', { name: /Initialize Shamir barrier/i }));

    // The refresh after a successful ceremony reports initialized: the
    // one-time credentials must outlive that transition.
    await waitFor(() => {
      expect(screen.queryByTestId('barrier-strategy-shamir-init')).not.toBeInTheDocument();
    });
    const result = screen.getByTestId('barrier-shamir-init-result');
    expect(result).toHaveTextContent(/any 2 of 2 shares/);
    expect(screen.getByTestId('barrier-init-admin-cert')).toHaveValue(ADMIN_CERT_PEM);
    expect(screen.getByTestId('barrier-init-admin-key')).toHaveValue(ADMIN_KEY_PEM);
    expect(screen.getByTestId('barrier-init-ca-cert')).toHaveValue(CA_PEM);
    const call = spy.calls.find((c) => c.url.endsWith('/api/v1/init/initialize'));
    expect(call?.body).toMatchObject({ admin_generate: true });
    expect(call?.body).not.toHaveProperty('admin_csr_pem');

    // The operator dismisses the credentials once they are saved.
    await fireEvent.click(screen.getByRole('button', { name: /Continue/i }));
    expect(screen.queryByTestId('barrier-shamir-init-result')).not.toBeInTheDocument();
    expect(screen.queryByTestId('barrier-init-admin-key')).not.toBeInTheDocument();
  });

  it('delivers every initial officer certificate and keeps them until saved', async () => {
    // holders[] is transport.CeremonyHolderCert (go-qrdb sdk/go
    // transport/ceremony_types.go): cert_pem is a PEM string on the wire.
    const holders = [
      { username: 'alice', role: 'unseal', cert_pem: '-----BEGIN CERTIFICATE-----\nalice\n-----END CERTIFICATE-----' },
      { username: 'bob', role: 'unseal', cert_pem: '-----BEGIN CERTIFICATE-----\nbob\n-----END CERTIFICATE-----' },
    ];
    const barrier = ceremonyBarrier();
    spy = makeFetchSpy({
      '/api/v1/init/initialize': () => barrier.initialize({
        spki_pin: 'pin',
        ca_cert_pem: CA_PEM,
        tls_cert_pem: TLS_PEM,
        state: 'initialized',
        share_count: 2,
        holders,
        admin_cert_pem: ADMIN_CERT_PEM,
        admin_credential_source: 'csr-sign',
      }),
      '/api/v1/barrier/status': () => barrier.status(),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await fillCeremonyForm({ adminCSR: ADMIN_CSR_PEM });
    await fireEvent.click(screen.getByRole('button', { name: /Initialize Shamir barrier/i }));
    await waitFor(() => {
      expect(screen.queryByTestId('barrier-strategy-shamir-init')).not.toBeInTheDocument();
    });

    const shown = screen.getAllByTestId('barrier-init-holder');
    expect(shown).toHaveLength(2);
    expect(shown[0]).toHaveTextContent('alice');
    expect(within(shown[0]).getByTestId('barrier-init-holder-cert')).toHaveValue(holders[0].cert_pem);
    expect(within(shown[1]).getByTestId('barrier-init-holder-cert')).toHaveValue(holders[1].cert_pem);

    // No issued certificate can be fetched again, so Continue waits for all.
    const cont = screen.getByRole('button', { name: /Continue/i });
    expect(cont).toBeDisabled();
    await fireEvent.click(within(shown[0]).getByRole('button', { name: /Download certificate/i }));
    expect(downloadText).toHaveBeenCalledWith('alice.crt.pem', holders[0].cert_pem);
    expect(cont).toBeDisabled();
    await fireEvent.click(within(shown[1]).getByRole('button', { name: /Download certificate/i }));
    expect(downloadText).toHaveBeenCalledWith('bob.crt.pem', holders[1].cert_pem);
    expect(cont).toBeEnabled();
  });

  it('refuses an officers roster that is not a JSON array without calling the server', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => ({ sealed: true, strategy: 'shamir' }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await fillCeremonyForm({ adminCSR: ADMIN_CSR_PEM, officers: '{"username":"alice"}' });
    await fireEvent.click(screen.getByRole('button', { name: /Initialize Shamir barrier/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/JSON array/i);
    expect(spy.calls.some((c) => c.url.endsWith('/api/v1/init/initialize'))).toBe(false);
  });

  it('shows the ceremony refusal and keeps the form', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => ({ sealed: true, strategy: 'shamir' }),
    });
    const ok = spy.mock.getMockImplementation();
    spy.mock.mockImplementation((url: string, init?: RequestInit) => {
      if (url.endsWith('/api/v1/init/initialize')) {
        return Promise.resolve({
          ok: false,
          status: 409,
          statusText: 'Conflict',
          text: () => Promise.resolve(JSON.stringify({ error: 'ceremony already initialized' })),
          json: () => Promise.resolve({ error: 'ceremony already initialized' }),
        });
      }
      return ok?.(url, init);
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await fillCeremonyForm({ adminCSR: ADMIN_CSR_PEM });
    await fireEvent.click(screen.getByRole('button', { name: /Initialize Shamir barrier/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/already initialized/);
    expect(screen.getByRole('button', { name: /Initialize Shamir barrier/i })).toBeInTheDocument();
    expect(screen.queryByTestId('barrier-shamir-init-result')).not.toBeInTheDocument();
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

  // A sealed system barrier reports initialized with no initialized_at
  // (go-quicbaas#1916): the explicit flag alone selects quorum unseal.
  it('shamir strategy (sealed, initialized, no initialized_at) shows quorum unseal', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({ initialized_at: undefined }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-shamir-unseal')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('barrier-strategy-shamir-init')).not.toBeInTheDocument();
  });

  it('never infers initialized from initialized_at', async () => {
    spy = makeFetchSpy({
      '/api/v1/barrier/status': () => makeStatus({ initialized: undefined }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('barrier-strategy-shamir-init')).toBeInTheDocument();
    });
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
        method_data: { purpose: 'system/unseal/unseal' },
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
    expect(screen.getByTestId('shamir-challenge')).toHaveTextContent('system/unseal/unseal');
    // The holder signs qrdb's canonical mtls.ChallengeMessage, never the bare nonce.
    expect(screen.getByTestId('shamir-signing-message')).toHaveValue(
      'cXJkYi9tdGxzLWNoYWxsZW5nZS92MQBjaC0xAHN5c3RlbS91bnNlYWwvdW5zZWFsAG5vbmNl',
    );
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

  it('shamir unseal shows a challenge with no purpose as an error, not a nonce to sign', async () => {
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
    });
    vi.stubGlobal('fetch', spy.mock);

    render(BarrierView);
    await screen.findByTestId('shamir-share-input');

    await fireEvent.input(screen.getByLabelText(/Officer username/i), { target: { value: 'officer-1' } });
    await fireEvent.click(screen.getByRole('button', { name: /Request challenge/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/no purpose/);
    expect(screen.queryByTestId('shamir-challenge')).toBeNull();
    expect(screen.queryByText(/bm9uY2U=/)).toBeNull();
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
