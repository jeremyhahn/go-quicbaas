/**
 * CeremonyView.test.ts
 *
 * Verifies that CeremonyView renders the officer-list, claim-cert, claim-share,
 * and enroll-external-officer surfaces AND that every HTTP call lands on
 * /api/v1/init/* (never /api/v1/ceremony/*).
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

    if (url.includes('/api/v1/ceremony/')) {
      throw new Error(`forbidden legacy URL hit: ${url}`);
    }
    if (url.includes('/api/v1/seal/')) {
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

import CeremonyView from '../src/views/security/CeremonyView.svelte';

const mockOfficers = [
  {
    username: 'alice',
    kind: 'cert',
    claim_status: 'pending',
    share_index: 1,
    auth_factor: 'yubikey',
  },
  {
    username: 'bob',
    kind: 'pubkey',
    claim_status: 'enrolled',
    share_index: 2,
    auth_factor: 'passphrase',
  },
];

describe('CeremonyView', () => {
  let spy: FetchSpy;

  beforeEach(() => {
    vi.clearAllMocks();
    spy = makeFetchSpy({
      '/api/v1/init/officers': () => ({ officers: mockOfficers }),
      '/api/v1/init/claim-cert/complete': () => ({ cert_pem: 'PEM', ca_cert_pem: 'CA' }),
      '/api/v1/init/officer-share/complete': () => ({ share_index: 1, share_json: '{"index":1}' }),
      '/api/v1/init/enroll-external-officer': () => ({ sealed_share: '' }),
    });
    vi.stubGlobal('fetch', spy.mock);
  });

  it('renders the "Ceremony" heading', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Ceremony' })).toBeInTheDocument();
    });
  });

  it('hits /api/v1/init/officers (never /api/v1/ceremony/officers)', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(spy.calls.some((c) => c.url.endsWith('/api/v1/init/officers'))).toBe(true);
    });
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/ceremony/'))).toBe(true);
  });

  it('renders the officer list with each row showing username, status, share index, auth factor', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByTestId('ceremony-officer-list')).toBeInTheDocument();
    });

    const aliceRow = await screen.findByText('alice');
    expect(aliceRow).toBeInTheDocument();

    const aliceTr = aliceRow.closest('tr')!;
    const aliceWithin = within(aliceTr);
    expect(aliceWithin.getByText('pending')).toBeInTheDocument();
    expect(aliceWithin.getByText('1')).toBeInTheDocument();
    expect(aliceWithin.getByText('yubikey')).toBeInTheDocument();
    expect(aliceWithin.getByText('cert')).toBeInTheDocument();

    const bobRow = screen.getByText('bob').closest('tr')!;
    const bobWithin = within(bobRow);
    expect(bobWithin.getByText('enrolled')).toBeInTheDocument();
    expect(bobWithin.getByText('passphrase')).toBeInTheDocument();
    expect(bobWithin.getByText('pubkey')).toBeInTheDocument();
  });

  it('renders the Claim Certificate form with username/challenge/signature inputs', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByTestId('ceremony-claim-challenge')).toBeInTheDocument();
    });
    const form = screen.getByTestId('ceremony-claim-challenge');
    expect(within(form).getByLabelText(/Username$/i)).toBeInTheDocument();
    expect(within(form).getByLabelText(/^Challenge$/i)).toBeInTheDocument();
    expect(within(form).getByLabelText(/Signature \(base64\)/i)).toBeInTheDocument();
    expect(within(form).getByRole('button', { name: /Submit Claim/i })).toBeInTheDocument();
  });

  it('claim-cert submission posts to /api/v1/init/claim-cert/complete', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByTestId('ceremony-claim-challenge')).toBeInTheDocument();
    });
    const form = screen.getByTestId('ceremony-claim-challenge');
    const usernameInput = within(form).getByLabelText(/^Username$/i);
    const challengeInput = within(form).getByLabelText(/^Challenge$/i);
    const signatureInput = within(form).getByLabelText(/Signature \(base64\)/i);

    await fireEvent.input(usernameInput, { target: { value: 'alice' } });
    await fireEvent.input(challengeInput, { target: { value: 'deadbeef' } });
    await fireEvent.input(signatureInput, { target: { value: 'c2lnbg==' } });
    await fireEvent.click(within(form).getByRole('button', { name: /Submit Claim/i }));

    await waitFor(() => {
      const hits = spy.calls.filter((c) => c.url.endsWith('/api/v1/init/claim-cert/complete'));
      expect(hits.length).toBeGreaterThan(0);
      expect(hits[0].method).toBe('POST');
    });
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/ceremony/'))).toBe(true);
  });

  it('renders the Claim Share form', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByTestId('ceremony-claim-share')).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /Submit Share/i })).toBeInTheDocument();
  });

  it('claim-share submission posts to /api/v1/init/officer-share/complete', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByTestId('ceremony-claim-share')).toBeInTheDocument();
    });
    const form = screen.getByTestId('ceremony-claim-share');
    const usernameInput = within(form).getByLabelText(/^Username$/i);
    const challengeInput = within(form).getByLabelText(/^Challenge$/i);
    const signatureInput = within(form).getByLabelText(/Signature \(base64\)/i);

    await fireEvent.input(usernameInput, { target: { value: 'alice' } });
    await fireEvent.input(challengeInput, { target: { value: 'deadbeef' } });
    await fireEvent.input(signatureInput, { target: { value: 'c2lnbg==' } });
    await fireEvent.click(within(form).getByRole('button', { name: /Submit Share/i }));

    await waitFor(() => {
      const hits = spy.calls.filter((c) => c.url.endsWith('/api/v1/init/officer-share/complete'));
      expect(hits.length).toBeGreaterThan(0);
      expect(hits[0].method).toBe('POST');
    });
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/ceremony/'))).toBe(true);
  });

  it('renders the Enroll External Officer form with KIND selector + file upload + share index + auth factor', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByTestId('ceremony-enroll-form')).toBeInTheDocument();
    });
    const form = screen.getByTestId('ceremony-enroll-form');
    expect(within(form).getByLabelText(/^Kind$/i)).toBeInTheDocument();
    expect(within(form).getByLabelText(/Share index/i)).toBeInTheDocument();
    expect(within(form).getByLabelText(/Auth factor/i)).toBeInTheDocument();
    expect(within(form).getByLabelText(/Upload material/i)).toBeInTheDocument();
    expect(within(form).getByLabelText(/^Material \(/i)).toBeInTheDocument();
    expect(within(form).getByRole('button', { name: /Enroll Officer/i })).toBeInTheDocument();
  });

  it('enroll submission posts to /api/v1/init/enroll-external-officer', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByTestId('ceremony-enroll-form')).toBeInTheDocument();
    });
    const form = screen.getByTestId('ceremony-enroll-form') as HTMLFormElement;
    await fireEvent.input(within(form).getByLabelText(/Username/i), {
      target: { value: 'carol' },
    });
    // Share-index defaults to 1 already; setting it via fireEvent.input
    // triggers a jsdom Decimal/step-validation bug, so we leave the default.
    await fireEvent.input(within(form).getByLabelText(/Auth factor/i), {
      target: { value: 'mtls' },
    });
    await fireEvent.input(within(form).getByLabelText(/^Material \(/i), {
      target: { value: '-----BEGIN CERTIFICATE-----\nABC\n-----END CERTIFICATE-----' },
    });
    // Submit via the form's submit event to avoid jsdom's requestSubmit path
    // which crashes on numeric step validation when the share-index input
    // is touched (jsdom 25 lacks a Decimal constructor in some builds).
    await fireEvent.submit(form);

    await waitFor(() => {
      const hits = spy.calls.filter((c) =>
        c.url.endsWith('/api/v1/init/enroll-external-officer'),
      );
      expect(hits.length).toBeGreaterThan(0);
      expect(hits[0].method).toBe('POST');
    });
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/ceremony/'))).toBe(true);
  });

  it('Kind selector exposes both cert and pubkey options', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByTestId('ceremony-enroll-form')).toBeInTheDocument();
    });
    const select = screen.getByLabelText(/^Kind$/i) as HTMLSelectElement;
    const optionValues = Array.from(select.options).map((o) => o.value);
    expect(optionValues).toEqual(expect.arrayContaining(['cert', 'pubkey']));
  });

  it('refresh button reissues the officers fetch', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    const initialCalls = spy.calls.length;

    await fireEvent.click(screen.getByRole('button', { name: /Refresh/i }));
    await waitFor(() => {
      expect(spy.calls.length).toBeGreaterThan(initialCalls);
    });
  });

  it('regression: never calls /api/v1/ceremony/* on any operation', async () => {
    render(CeremonyView);
    await waitFor(() => {
      expect(spy.calls.length).toBeGreaterThan(0);
    });
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/ceremony/'))).toBe(true);
    expect(spy.calls.every((c) => !c.url.includes('/api/v1/seal/'))).toBe(true);
  });

  /**
   * Regression: when the daemon returns 503 with the typed envelope
   * {"error": "ceremony service not available"}, the previous behaviour
   * rendered a destructive red banner. That misleads operators — the
   * subsystem isn't broken, it just hasn't been bootstrapped yet. The
   * fix renders an informational empty state pointing at the next step.
   */
  it('renders an informational empty state (not a destructive banner) when /api/v1/init/officers returns 503', async () => {
    const calls: FetchCall[] = [];
    const mock503 = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      const method = (init?.method ?? 'GET').toUpperCase();
      calls.push({ url, method, body: undefined });
      if (url.includes('/api/v1/init/officers')) {
        return Promise.resolve({
          ok: false,
          status: 503,
          statusText: 'Service Unavailable',
          text: () =>
            Promise.resolve(
              JSON.stringify({ error: 'ceremony service not available' }),
            ),
          json: () =>
            Promise.resolve({ error: 'ceremony service not available' }),
        });
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        statusText: 'OK',
        text: () => Promise.resolve('{}'),
        json: () => Promise.resolve({}),
      });
    });
    vi.stubGlobal('fetch', mock503);

    render(CeremonyView);

    // The informational empty-state element must appear.
    const emptyState = await screen.findByTestId('ceremony-service-unavailable');
    expect(emptyState).toBeInTheDocument();
    expect(emptyState.className).toMatch(/alert-info/);
    // It must NOT carry the destructive class.
    expect(emptyState.className).not.toMatch(/alert-error/);

    // The empty state must actively guide the operator to the next step.
    expect(emptyState.textContent ?? '').toMatch(/quicbaasd init/);

    // A red destructive error banner anywhere in the document would be
    // a regression. We tolerate alert-error inside other unrelated
    // forms (claim share / enroll), but the Pending Officers card itself
    // must be clear.
    const officerCards = document.querySelectorAll('.alert-error');
    for (const card of Array.from(officerCards)) {
      expect(card.textContent ?? '').not.toMatch(/ceremony service not available/);
    }
  });

  /**
   * Regression: when the daemon's GET /api/v1/init/officers handler was
   * missing the request returned 404 and the view rendered a red error
   * banner ("404 Not Found") inside the Pending Officers card. With the
   * handler wired and returning `{"officers": []}` the page must NOT
   * render any alert-error envelope inside the Pending Officers card.
   */
  // ---------------------------------------------------------------------------
  // Issue #82: submit buttons must be disabled when ceremony service is inactive
  // ---------------------------------------------------------------------------

  it('"Submit Claim" button is disabled when ceremony service is inactive (#82)', async () => {
    const mock503 = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/api/v1/init/officers')) {
        return Promise.resolve({
          ok: false,
          status: 503,
          statusText: 'Service Unavailable',
          text: () => Promise.resolve(JSON.stringify({ error: 'ceremony service not available' })),
          json: () => Promise.resolve({ error: 'ceremony service not available' }),
        });
      }
      return Promise.resolve({
        ok: true, status: 200, statusText: 'OK',
        text: () => Promise.resolve('{}'),
        json: () => Promise.resolve({}),
      });
    });
    vi.stubGlobal('fetch', mock503);

    render(CeremonyView);

    await screen.findByTestId('ceremony-service-unavailable');

    const submitClaimBtn = screen.getByRole('button', { name: /Submit Claim/i });
    expect(submitClaimBtn).toBeDisabled();
  });

  it('"Submit Share" button is disabled when ceremony service is inactive (#82)', async () => {
    const mock503 = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/api/v1/init/officers')) {
        return Promise.resolve({
          ok: false,
          status: 503,
          statusText: 'Service Unavailable',
          text: () => Promise.resolve(JSON.stringify({ error: 'ceremony service not available' })),
          json: () => Promise.resolve({ error: 'ceremony service not available' }),
        });
      }
      return Promise.resolve({
        ok: true, status: 200, statusText: 'OK',
        text: () => Promise.resolve('{}'),
        json: () => Promise.resolve({}),
      });
    });
    vi.stubGlobal('fetch', mock503);

    render(CeremonyView);

    await screen.findByTestId('ceremony-service-unavailable');

    const submitShareBtn = screen.getByRole('button', { name: /Submit Share/i });
    expect(submitShareBtn).toBeDisabled();
  });

  it('does not render a 404 error banner when officers list is empty', async () => {
    spy = makeFetchSpy({
      // Empty roster — matches the canonical empty-state envelope.
      '/api/v1/init/officers': () => ({ officers: [] }),
    });
    vi.stubGlobal('fetch', spy.mock);

    render(CeremonyView);

    // Wait for the officers fetch to settle.
    await waitFor(() => {
      expect(spy.calls.some((c) => c.url.endsWith('/api/v1/init/officers'))).toBe(true);
    });

    // The Pending Officers card must not contain any alert-error envelope.
    const officerList = await screen.findByTestId('ceremony-officer-list');
    const officerCard = officerList.closest('.card');
    expect(officerCard).not.toBeNull();

    const alertErrorWithin = officerCard!.querySelector('.alert-error');
    expect(alertErrorWithin).toBeNull();

    // No "404" string should appear in the officer card either.
    expect(officerCard!.textContent ?? '').not.toMatch(/\b404\b/);
  });
});
