/**
 * CeremonyView.test.ts
 *
 * Verifies that CeremonyView renders the officer list, the share-claim flow
 * and the officer enrollment form AND that every HTTP call lands on the
 * barrier holder routes the server serves: GET/POST /api/v1/barrier/officers
 * and /api/v1/barrier/officers/{username}/claim/{begin,complete}. It never
 * hits /api/v1/ceremony/* or the removed /api/v1/init/claim-cert,
 * /api/v1/init/officer-share and /api/v1/init/enroll-external-officer routes.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/svelte';

interface FetchCall {
  url: string;
  method: string;
  body: unknown;
}

interface FetchSpy {
  calls: FetchCall[];
  mock: ReturnType<typeof vi.fn>;
}

const removedRoutes = ['/api/v1/ceremony/', '/api/v1/seal/', '/api/v1/init/claim-cert', '/api/v1/init/officer-share', '/api/v1/init/enroll-external-officer'];

function reply(status: number, data: unknown) {
  return Promise.resolve({
    ok: status < 400,
    status,
    statusText: status < 400 ? 'OK' : 'Service Unavailable',
    headers: new Headers({ 'Content-Type': 'application/json' }),
    text: () => Promise.resolve(JSON.stringify(data ?? {})),
    json: () => Promise.resolve(data ?? {}),
  });
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

    for (const route of removedRoutes) {
      if (url.includes(route)) {
        throw new Error(`removed route hit: ${url}`);
      }
    }

    const keys = Object.keys(responses).sort((a, b) => b.length - a.length);
    for (const key of keys) {
      if (url.includes(key)) {
        return reply(200, responses[key](call));
      }
    }
    return reply(200, {});
  });

  return { calls, mock };
}

// unavailableSpy answers the officer list with the daemon's 503 envelope.
function unavailableSpy(): FetchSpy {
  const spy = makeFetchSpy({});
  const answer = spy.mock.getMockImplementation()!;
  spy.mock.mockImplementation((url: string, init?: RequestInit) => {
    if (url.endsWith('/api/v1/barrier/officers') && (init?.method ?? 'GET') === 'GET') {
      spy.calls.push({ url, method: 'GET', body: undefined });
      return reply(503, { error: 'ceremony service not available' });
    }
    return answer(url, init);
  });
  return spy;
}

// readBlob reads a Blob as text; jsdom's Blob has no text() method.
function readBlob(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(reader.error);
    reader.readAsText(blob);
  });
}

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import CeremonyView from '../src/views/security/CeremonyView.svelte';

const mockOfficers = [
  {
    username: 'alice',
    role: 'officer',
    share_index: 1,
    factors: ['yubikey'],
    enrolled_at: '2026-10-25T00:00:00Z',
    share_pending: true,
  },
  {
    username: 'bob',
    role: 'officer',
    share_index: 2,
    factors: ['passphrase'],
    enrolled_at: '2026-10-25T00:00:00Z',
    claimed_at: '2026-10-25T00:01:00Z',
    share_pending: false,
  },
];

const csr = '-----BEGIN CERTIFICATE REQUEST-----\nABC\n-----END CERTIFICATE REQUEST-----';

describe('CeremonyView', () => {
  let spy: FetchSpy;

  beforeEach(() => {
    vi.clearAllMocks();
    spy = makeFetchSpy({
      '/api/v1/barrier/officers': (call) =>
        call.method === 'GET' ? { officers: mockOfficers } : { cert_pem: 'CERT', role: 'officer' },
      '/api/v1/barrier/officers/alice/claim/begin': () => ({ nonce_hex: 'beef', expires_at: '2026-10-25T00:05:00Z' }),
      '/api/v1/barrier/officers/alice/claim/complete': () => ({ share_index: 1, envelope: 'ZW52ZWxvcGU=' }),
    });
    vi.stubGlobal('fetch', spy.mock);
  });

  it('renders the "Ceremony" heading', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Ceremony' })).toBeInTheDocument();
    });
  });

  it('lists the officers from GET /api/v1/barrier/officers', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(spy.calls.some((c) => c.url.endsWith('/api/v1/barrier/officers') && c.method === 'GET')).toBe(true);
    });
  });

  it('renders each officer with its claim state, share index, factors and kind', async () => {
    render(CeremonyView);

    const aliceTr = (await screen.findByText('alice')).closest('tr')!;
    expect(within(aliceTr).getByText('enrolled')).toBeInTheDocument();
    expect(within(aliceTr).getByText('1')).toBeInTheDocument();
    expect(within(aliceTr).getByText('yubikey')).toBeInTheDocument();
    expect(within(aliceTr).getByText('cert')).toBeInTheDocument();

    const bobTr = screen.getByText('bob').closest('tr')!;
    expect(within(bobTr).getByText('claimed')).toBeInTheDocument();
    expect(within(bobTr).getByText('passphrase')).toBeInTheDocument();
  });

  it('claims a share: begin shows the message to sign, complete posts the signature', async () => {
    render(CeremonyView);
    const form = await screen.findByTestId('ceremony-claim-challenge');

    await fireEvent.input(within(form).getByLabelText(/^Username$/i), { target: { value: 'alice' } });
    await fireEvent.click(within(form).getByRole('button', { name: /Request Challenge/i }));

    await waitFor(() => {
      // The officer signs qrdb's canonical ceremony.ShareClaimMessage for the
      // system barrier, never the bare nonce.
      expect(within(form).getByTestId('ceremony-claim-message')).toHaveValue(
        'cXJkYi9zaGFyZS1jbGFpbS92MQBzeXN0ZW0AYWxpY2UAvu8=',
      );
    });
    expect(within(form).queryByText(/^beef$/)).toBeNull();
    expect(spy.calls.find((c) => c.url.endsWith('/api/v1/barrier/officers/alice/claim/begin'))).toMatchObject({
      method: 'POST',
      body: { username: 'alice' },
    });

    await fireEvent.input(within(form).getByLabelText(/Signature \(base64\)/i), { target: { value: 'c2lnbg==' } });
    await fireEvent.click(within(form).getByRole('button', { name: /Submit Claim/i }));

    await waitFor(() => {
      expect(screen.getByTestId('ceremony-claim-envelope')).toHaveTextContent('ZW52ZWxvcGU=');
    });
    expect(spy.calls.find((c) => c.url.endsWith('/api/v1/barrier/officers/alice/claim/complete'))).toMatchObject({
      method: 'POST',
      body: { username: 'alice', nonce_hex: 'beef', signature: 'c2lnbg==' },
    });
  });

  it('enrolls an officer with POST /api/v1/barrier/officers', async () => {
    render(CeremonyView);
    const form = (await screen.findByTestId('ceremony-enroll-form')) as HTMLFormElement;
    expect(within(form).getByLabelText(/Upload CSR/i)).toBeInTheDocument();

    await fireEvent.input(within(form).getByLabelText(/^Username$/i), { target: { value: 'carol' } });
    await fireEvent.input(within(form).getByLabelText(/^CSR \(PEM\)$/i), { target: { value: csr } });
    await fireEvent.submit(form);

    await waitFor(() => {
      expect(spy.calls.find((c) => c.url.endsWith('/api/v1/barrier/officers') && c.method === 'POST')).toMatchObject({
        body: { username: 'carol', csr_pem: csr },
      });
    });
  });

  it('delivers the enrolled officer certificate: shown and downloadable after the form clears', async () => {
    const certPEM = '-----BEGIN CERTIFICATE-----\nCAROL\n-----END CERTIFICATE-----';
    spy = makeFetchSpy({
      '/api/v1/barrier/officers': (call) =>
        call.method === 'GET' ? { officers: mockOfficers } : { cert_pem: certPEM, role: 'officer' },
    });
    vi.stubGlobal('fetch', spy.mock);
    const blobs: Blob[] = [];
    URL.createObjectURL = vi.fn((blob: Blob) => { blobs.push(blob); return 'blob:officer-cert'; });
    URL.revokeObjectURL = vi.fn();
    const downloads: string[] = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      downloads.push(this.download);
    });

    render(CeremonyView);
    const form = (await screen.findByTestId('ceremony-enroll-form')) as HTMLFormElement;
    await fireEvent.input(within(form).getByLabelText(/^Username$/i), { target: { value: 'carol' } });
    await fireEvent.input(within(form).getByLabelText(/^CSR \(PEM\)$/i), { target: { value: csr } });
    await fireEvent.submit(form);

    const issued = await screen.findByTestId('ceremony-enrolled-cert');
    expect(issued).toHaveTextContent('CAROL');
    expect((within(form).getByLabelText(/^Username$/i) as HTMLInputElement).value).toBe('');

    await fireEvent.click(within(issued).getByRole('button', { name: /Download certificate/i }));
    expect(downloads).toEqual(['carol.crt.pem']);
    expect(blobs).toHaveLength(1);
    expect(await readBlob(blobs[0])).toBe(certPEM);
    click.mockRestore();
  });

  it('refuses to enroll without a CSR and sends nothing', async () => {
    render(CeremonyView);
    const form = (await screen.findByTestId('ceremony-enroll-form')) as HTMLFormElement;

    await fireEvent.input(within(form).getByLabelText(/^Username$/i), { target: { value: 'carol' } });
    await fireEvent.submit(form);

    expect(await within(form).findByRole('alert')).toHaveTextContent(/CSR/);
    expect(spy.calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('refresh button reissues the officers fetch', async () => {
    render(CeremonyView);
    await screen.findByText('alice');
    const initialCalls = spy.calls.length;

    await fireEvent.click(screen.getByRole('button', { name: /Refresh/i }));
    await waitFor(() => {
      expect(spy.calls.length).toBeGreaterThan(initialCalls);
    });
  });

  it('renders an informational empty state, not a destructive banner, when the officer list returns 503', async () => {
    vi.stubGlobal('fetch', unavailableSpy().mock);

    render(CeremonyView);

    const emptyState = await screen.findByTestId('ceremony-service-unavailable');
    expect(emptyState.className).toMatch(/alert-info/);
    expect(emptyState.className).not.toMatch(/alert-error/);
    expect(emptyState.textContent ?? '').toMatch(/quicbaasd init/);
    for (const card of Array.from(document.querySelectorAll('.alert-error'))) {
      expect(card.textContent ?? '').not.toMatch(/ceremony service not available/);
    }
  });

  it('disables the claim and enroll buttons when the ceremony service is inactive (#82)', async () => {
    vi.stubGlobal('fetch', unavailableSpy().mock);

    render(CeremonyView);
    await screen.findByTestId('ceremony-service-unavailable');

    expect(screen.getByRole('button', { name: /Submit Claim/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /Enroll Officer/i })).toBeDisabled();
  });

  it('does not render an error banner when the officer list is empty', async () => {
    spy = makeFetchSpy({ '/api/v1/barrier/officers': () => ({ officers: [] }) });
    vi.stubGlobal('fetch', spy.mock);

    render(CeremonyView);

    const officerCard = (await screen.findByTestId('ceremony-officer-list')).closest('.card');
    await waitFor(() => {
      expect(spy.calls.some((c) => c.url.endsWith('/api/v1/barrier/officers'))).toBe(true);
    });
    expect(officerCard).not.toBeNull();
    expect(officerCard!.querySelector('.alert-error')).toBeNull();
  });
});
