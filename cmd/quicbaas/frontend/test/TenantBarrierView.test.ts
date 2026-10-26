/**
 * TenantBarrierView.test.ts
 *
 * Verifies the tenant barrier's seal and unseal calls land on the routes the
 * server serves under /api/v1/tenants/{tenant_id}/barrier/*:
 *
 *   - a Shamir barrier seals through a quorum (quorum/begin + quorum/submit
 *     with op "seal"); the direct seal route refuses a Shamir barrier.
 *   - a passphrase barrier seals through the direct seal route.
 *   - a passphrase unseal sends the passphrase bytes base64-encoded once.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/svelte';
import { shareClaimMessage } from '@quicbaas/svelte-security';

vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: vi.fn() }));
vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));
vi.mock('../src/lib/download.ts', () => ({ downloadText: vi.fn() }));

import { getSDKClient } from '../src/stores/sdkClient.ts';
import { downloadText } from '../src/lib/download.ts';
import TenantBarrierView from '../src/views/security/TenantBarrierView.svelte';

const TENANT = 'acme';
const BASE = `/api/v1/tenants/${TENANT}/barrier`;

interface FetchCall {
  url: string;
  method: string;
  body: Record<string, unknown> | undefined;
}

interface RawTenantStatus {
  strategy: string;
  sealed: boolean;
  initialized: boolean;
  shamir_threshold: number;
  shamir_total: number;
}

// stubFetch serves the tenant barrier routes from responses (matched by path
// suffix) and records every call.
function stubFetch(
  status: RawTenantStatus,
  responses: Record<string, unknown> = {},
): FetchCall[] {
  const calls: FetchCall[] = [];
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const method = (init?.method ?? 'GET').toUpperCase();
    const body = init?.body !== undefined
      ? (JSON.parse(init.body as string) as Record<string, unknown>)
      : undefined;
    calls.push({ url, method, body });
    const path = new URL(url, 'http://localhost').pathname;
    const data = path === `${BASE}/status` ? status : (responses[path] ?? {});
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: () => Promise.resolve(JSON.stringify(data)),
      json: () => Promise.resolve(data),
    });
  }));
  return calls;
}

function posts(calls: FetchCall[], suffix: string): FetchCall[] {
  return calls.filter((c) => c.method === 'POST' && c.url.endsWith(`${BASE}${suffix}`));
}

describe('TenantBarrierView seal and unseal routes', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getSDKClient).mockResolvedValue({
      iam: {
        listTenants: vi.fn().mockResolvedValue({ tenants: [{ id: TENANT, name: TENANT }] }),
      },
    } as never);
  });

  it('seals a Shamir barrier through a seal quorum, never the direct seal route', async () => {
    const calls = stubFetch(
      { strategy: 'shamir', sealed: false, initialized: true, shamir_threshold: 1, shamir_total: 1 },
      {
        [`${BASE}/quorum/begin`]: {
          challenge_id: 'c-1', factor: 'mtls', nonce: 'bm9uY2U=', expires_at: '2030-01-01T00:00:00Z',
          method_data: { purpose: 'tenant/unseal/seal' },
        },
        [`${BASE}/quorum/submit`]: { submitted: 1, required: 1, complete: true },
      },
    );
    render(TenantBarrierView);

    await fireEvent.click(await screen.findByTestId('tenant-barrier-seal'));
    const panel = await screen.findByTestId('tenant-barrier-shamir-seal');
    expect(panel).toHaveTextContent(/Seal progress/);

    await fireEvent.input(within(panel).getByLabelText(/Officer username/i), { target: { value: 'alice' } });
    await fireEvent.click(within(panel).getByRole('button', { name: /Request challenge/i }));
    await within(panel).findByTestId('shamir-challenge');
    await fireEvent.input(within(panel).getByLabelText(/Signed response/i), { target: { value: 'sig' } });
    await fireEvent.input(within(panel).getByLabelText(/^Share$/i), { target: { value: 'share-1' } });
    await fireEvent.click(within(panel).getByRole('button', { name: /Submit share/i }));

    await waitFor(() => {
      expect(posts(calls, '/quorum/submit')).toHaveLength(1);
    });
    expect(posts(calls, '/quorum/begin')[0]?.body).toMatchObject({ op: 'seal', username: 'alice' });
    expect(posts(calls, '/quorum/submit')[0]?.body).toMatchObject({
      op: 'seal', username: 'alice', challenge_id: 'c-1', response: 'sig', share: 'share-1',
    });
    expect(posts(calls, '/seal')).toHaveLength(0);
  });

  it('seals a passphrase barrier through the direct seal route', async () => {
    const calls = stubFetch(
      { strategy: 'passphrase', sealed: false, initialized: true, shamir_threshold: 0, shamir_total: 0 },
    );
    render(TenantBarrierView);

    await fireEvent.click(await screen.findByTestId('tenant-barrier-seal'));

    await waitFor(() => {
      expect(posts(calls, '/seal')).toHaveLength(1);
    });
    expect(posts(calls, '/quorum/begin')).toHaveLength(0);
    expect(screen.queryByTestId('tenant-barrier-shamir-seal')).toBeNull();
  });

  it('unseals a passphrase barrier with the passphrase bytes base64-encoded once', async () => {
    const calls = stubFetch(
      { strategy: 'passphrase', sealed: true, initialized: true, shamir_threshold: 0, shamir_total: 0 },
    );
    const { container } = render(TenantBarrierView);

    await screen.findByTestId('tenant-barrier-passphrase-unseal');
    const input = container.querySelector('#barrier-unseal-input') as HTMLInputElement;
    await fireEvent.input(input, { target: { value: 'hunter2' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Unseal$/ }));

    await waitFor(() => {
      expect(posts(calls, '/unseal')).toHaveLength(1);
    });
    // base64("hunter2"), not base64(base64("hunter2")).
    expect(posts(calls, '/unseal')[0]?.body).toEqual({ key: 'aHVudGVyMg==' });
  });

  it('initializes, seals and unseals a passphrase barrier with the same passphrase bytes', async () => {
    // One status object, mutated as the barrier moves through its lifecycle;
    // stubFetch serves whatever it holds at the time of each status call.
    const status: RawTenantStatus = {
      strategy: 'none', sealed: false, initialized: false, shamir_threshold: 0, shamir_total: 0,
    };
    const calls = stubFetch(status);
    const { container } = render(TenantBarrierView);

    await screen.findByTestId('barrier-strategy-picker');
    await fireEvent.click(screen.getByTestId('barrier-strategy-radio-passphrase'));
    const passphrase = container.querySelector('#barrier-init-passphrase') as HTMLInputElement;
    const confirm = container.querySelector('#barrier-init-passphrase-confirm') as HTMLInputElement;
    await fireEvent.input(passphrase, { target: { value: 'hunter2' } });
    await fireEvent.input(confirm, { target: { value: 'hunter2' } });
    Object.assign(status, { strategy: 'passphrase', initialized: true });
    await fireEvent.click(screen.getByTestId('barrier-strategy-submit'));

    await waitFor(() => {
      expect(posts(calls, '/init')).toHaveLength(1);
    });

    const sealButton = await screen.findByTestId('tenant-barrier-seal');
    Object.assign(status, { sealed: true });
    await fireEvent.click(sealButton);
    await waitFor(() => {
      expect(posts(calls, '/seal')).toHaveLength(1);
    });

    await screen.findByTestId('tenant-barrier-passphrase-unseal');
    const unsealInput = container.querySelector('#barrier-unseal-input') as HTMLInputElement;
    await fireEvent.input(unsealInput, { target: { value: 'hunter2' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Unseal$/ }));
    await waitFor(() => {
      expect(posts(calls, '/unseal')).toHaveLength(1);
    });

    // base64("hunter2") on both routes: the barrier initialized through this
    // view is unlocked by the same typed passphrase.
    expect(posts(calls, '/init')[0]?.body).toEqual({ strategy: 'passphrase', passphrase: 'aHVudGVyMg==' });
    expect(posts(calls, '/unseal')[0]?.body).toEqual({ key: 'aHVudGVyMg==' });
  });

  it('rejects an empty passphrase without calling the unseal route', async () => {
    const calls = stubFetch(
      { strategy: 'passphrase', sealed: true, initialized: true, shamir_threshold: 0, shamir_total: 0 },
    );
    render(TenantBarrierView);

    await screen.findByTestId('tenant-barrier-passphrase-unseal');
    await fireEvent.click(screen.getByRole('button', { name: /^Unseal$/ }));

    expect(posts(calls, '/unseal')).toHaveLength(0);
  });

  it('shows the issued holder certificates after a share-free Shamir init', async () => {
    // The body go-qrdb's TenantBarrierInitResponse marshals to: share
    // metadata and holder certificates, never the shares themselves.
    const calls = stubFetch(
      { strategy: 'none', sealed: false, initialized: false, shamir_threshold: 0, shamir_total: 0 },
      {
        [`${BASE}/init`]: {
          tenant_id: TENANT,
          strategy: 'shamir',
          threshold: 2,
          total_shares: 3,
          share_count: 3,
          holders: [
            { username: 'officer-1', role: 'unseal', cert_pem: 'cert-pem-1' },
            { username: 'officer-2', role: 'unseal', cert_pem: 'cert-pem-2' },
          ],
        },
      },
    );
    render(TenantBarrierView);

    await screen.findByTestId('barrier-strategy-picker');
    await fireEvent.click(screen.getByTestId('barrier-strategy-radio-shamir'));
    await fireEvent.click(screen.getByTestId('barrier-strategy-submit'));

    const panel = await screen.findByTestId('tenant-barrier-holders');
    expect(posts(calls, '/init')[0]?.body).toMatchObject({ strategy: 'shamir', threshold: 2 });
    expect(panel).toHaveTextContent(/2 of 3/);
    expect(panel).toHaveTextContent('officer-1');
    expect(panel).toHaveTextContent('officer-2');
    expect(panel).toHaveTextContent('cert-pem-1');
    expect(screen.queryByTestId('tenant-barrier-shares-reveal')).toBeNull();
  });

  it('offers the holder claim on an initialized Shamir tenant barrier', async () => {
    stubFetch({ strategy: 'shamir', sealed: false, initialized: true, shamir_threshold: 2, shamir_total: 3 });
    render(TenantBarrierView);

    const claim = await screen.findByTestId('tenant-barrier-claim');
    expect(claim).toContainElement(screen.getByTestId('ceremony-claim-challenge'));
  });

  it('keeps a claimed envelope after init until the holder saves it', async () => {
    // qrdb destroys its copy of the envelope during the claim, so the view is
    // the only holder of it: Continue stays disabled until it is downloaded.
    stubFetch(
      { strategy: 'none', sealed: false, initialized: false, shamir_threshold: 0, shamir_total: 0 },
      {
        [`${BASE}/init`]: {
          tenant_id: TENANT,
          strategy: 'shamir',
          threshold: 2,
          total_shares: 3,
          share_count: 3,
          holders: [{ username: 'officer-1', role: 'unseal', cert_pem: 'cert-pem-1' }],
        },
        [`${BASE}/officers/officer-1/claim/begin`]: { nonce_hex: 'beef', expires_at: '2030-01-01T00:00:00Z' },
        [`${BASE}/officers/officer-1/claim/complete`]: { share_index: 1, envelope: 'ZW52' },
      },
    );
    render(TenantBarrierView);

    await screen.findByTestId('barrier-strategy-picker');
    await fireEvent.click(screen.getByTestId('barrier-strategy-radio-shamir'));
    await fireEvent.click(screen.getByTestId('barrier-strategy-submit'));
    const panel = await screen.findByTestId('tenant-barrier-holders');

    const claim = within(panel).getByTestId('tenant-barrier-claim');
    await fireEvent.input(within(claim).getByLabelText('Username'), { target: { value: 'officer-1' } });
    await fireEvent.click(within(claim).getByRole('button', { name: /Request Challenge/i }));
    await waitFor(() => {
      expect(within(claim).getByTestId('ceremony-claim-message')).not.toHaveValue('');
    });
    await fireEvent.input(within(claim).getByLabelText('Signature (base64)'), { target: { value: 'c2ln' } });
    await fireEvent.click(within(claim).getByRole('button', { name: /Submit Claim/i }));

    const saved = await within(panel).findByTestId('tenant-barrier-envelope');
    expect(saved).toHaveTextContent('officer-1');
    expect(saved).toHaveTextContent('ZW52');
    const cont = within(panel).getByRole('button', { name: /Continue/i });
    expect(cont).toBeDisabled();

    await fireEvent.click(within(saved).getByRole('button', { name: /Download envelope/i }));
    expect(downloadText).toHaveBeenCalledWith(
      `${TENANT}-officer-1-share-1.envelope`, 'ZW52', 'application/octet-stream',
    );
    expect(cont).toBeEnabled();
  });

  it('offers the holder claim on a freshly initialized, still sealed Shamir tenant barrier', async () => {
    // Holders claim their shares before anyone can unseal, so the claim must
    // be reachable while the barrier is sealed, beside the unseal input.
    stubFetch({ strategy: 'shamir', sealed: true, initialized: true, shamir_threshold: 2, shamir_total: 3 });
    render(TenantBarrierView);

    const claim = await screen.findByTestId('tenant-barrier-claim');
    expect(claim).toContainElement(screen.getByTestId('ceremony-claim-challenge'));
    expect(screen.getByTestId('tenant-barrier-shamir-unseal')).toBeInTheDocument();
  });

  it('lets a listed holder claim their share under the tenant claim label', async () => {
    const calls = stubFetch(
      { strategy: 'shamir', sealed: false, initialized: true, shamir_threshold: 2, shamir_total: 3 },
      {
        [`${BASE}/officers/officer-1/claim/begin`]: { nonce_hex: 'beef', expires_at: '2030-01-01T00:00:00Z' },
        [`${BASE}/officers/officer-1/claim/complete`]: { share_index: 1, envelope: 'ZW52' },
      },
    );
    render(TenantBarrierView);

    const claim = await screen.findByTestId('tenant-barrier-claim');
    await fireEvent.input(within(claim).getByLabelText('Username'), { target: { value: 'officer-1' } });
    await fireEvent.click(within(claim).getByRole('button', { name: /Request Challenge/i }));

    await waitFor(() => {
      expect(screen.getByTestId('ceremony-claim-message')).toHaveValue(
        shareClaimMessage(`tenant:${TENANT}`, 'officer-1', 'beef'),
      );
    });
    await fireEvent.input(within(claim).getByLabelText('Signature (base64)'), { target: { value: 'c2ln' } });
    await fireEvent.click(within(claim).getByRole('button', { name: /Submit Claim/i }));

    expect(await screen.findByTestId('ceremony-claim-envelope')).toHaveTextContent('ZW52');
    expect(posts(calls, '/officers/officer-1/claim/begin')[0]?.body).toEqual({ username: 'officer-1' });
    expect(posts(calls, '/officers/officer-1/claim/complete')[0]?.body).toEqual({
      username: 'officer-1', nonce_hex: 'beef', signature: 'c2ln',
    });
  });

  it('sends the inline holder roster with a Shamir init', async () => {
    const calls = stubFetch(
      { strategy: 'none', sealed: false, initialized: false, shamir_threshold: 0, shamir_total: 0 },
      {
        [`${BASE}/init`]: {
          tenant_id: TENANT, strategy: 'shamir', threshold: 1, total_shares: 2, share_count: 2,
          holders: [
            { username: 'officer-1', role: 'unseal', cert_pem: 'cert-pem-1' },
            { username: 'officer-2', role: 'unseal', cert_pem: 'cert-pem-2' },
          ],
        },
      },
    );
    render(TenantBarrierView);

    await screen.findByTestId('barrier-strategy-picker');
    await fireEvent.click(screen.getByTestId('barrier-strategy-radio-shamir'));
    await fireEvent.input(screen.getByLabelText('Threshold (M)'), { target: { value: '1' } });
    await fireEvent.input(screen.getByLabelText('Total shares (N)'), { target: { value: '2' } });
    for (const n of [1, 2]) {
      await fireEvent.click(screen.getByTestId('barrier-strategy-add-holder'));
      await fireEvent.input(screen.getByLabelText(`Holder ${n} username`), { target: { value: `officer-${n}` } });
      await fireEvent.input(screen.getByLabelText(`Holder ${n} CSR (PEM)`), { target: { value: `csr-${n}` } });
    }
    await fireEvent.click(screen.getByTestId('barrier-strategy-submit'));

    await screen.findByTestId('tenant-barrier-holders');
    expect(posts(calls, '/init')[0]?.body).toEqual({
      strategy: 'shamir',
      threshold: 1,
      shares: 2,
      officers: [
        { username: 'officer-1', csr_pem: 'csr-1' },
        { username: 'officer-2', csr_pem: 'csr-2' },
      ],
    });
  });

  it('refuses a Shamir init whose roster does not cover every share', async () => {
    const calls = stubFetch(
      { strategy: 'none', sealed: false, initialized: false, shamir_threshold: 0, shamir_total: 0 },
    );
    render(TenantBarrierView);

    await screen.findByTestId('barrier-strategy-picker');
    await fireEvent.click(screen.getByTestId('barrier-strategy-radio-shamir'));
    await fireEvent.click(screen.getByTestId('barrier-strategy-add-holder'));
    await fireEvent.input(screen.getByLabelText('Holder 1 username'), { target: { value: 'officer-1' } });
    await fireEvent.input(screen.getByLabelText('Holder 1 CSR (PEM)'), { target: { value: 'csr-1' } });

    expect(screen.getByTestId('barrier-strategy-submit')).toBeDisabled();
    expect(screen.getByTestId('barrier-strategy-roster')).toHaveTextContent(/1 of 3 holders/);
    await fireEvent.click(screen.getByTestId('barrier-strategy-submit'));
    expect(posts(calls, '/init')).toHaveLength(0);
  });

  it('reports the sealed share count when the holders were registered before init', async () => {
    stubFetch(
      { strategy: 'none', sealed: false, initialized: false, shamir_threshold: 0, shamir_total: 0 },
      {
        [`${BASE}/init`]: {
          tenant_id: TENANT, strategy: 'shamir', threshold: 2, total_shares: 3, share_count: 3,
        },
      },
    );
    render(TenantBarrierView);

    await screen.findByTestId('barrier-strategy-picker');
    await fireEvent.click(screen.getByTestId('barrier-strategy-radio-shamir'));
    await fireEvent.click(screen.getByTestId('barrier-strategy-submit'));

    const panel = await screen.findByTestId('tenant-barrier-holders');
    expect(panel).toHaveTextContent(/3 shares sealed/);
    expect(screen.queryByTestId('tenant-barrier-holder')).toBeNull();
  });
});
