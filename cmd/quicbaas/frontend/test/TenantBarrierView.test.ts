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
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: vi.fn() }));
vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));

import { getSDKClient } from '../src/stores/sdkClient.ts';
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
          challenge_id: 'c-1', factor: 'cert', nonce: 'n-1', expires_at: '2030-01-01T00:00:00Z',
        },
        [`${BASE}/quorum/submit`]: { submitted: 1, required: 1, complete: true },
      },
    );
    render(TenantBarrierView);

    await fireEvent.click(await screen.findByTestId('tenant-barrier-seal'));
    const panel = await screen.findByTestId('tenant-barrier-shamir-seal');
    expect(panel).toHaveTextContent(/Seal progress/);

    await fireEvent.input(screen.getByLabelText(/Officer username/i), { target: { value: 'alice' } });
    await fireEvent.click(screen.getByRole('button', { name: /Request challenge/i }));
    await screen.findByTestId('shamir-challenge');
    await fireEvent.input(screen.getByLabelText(/Signed response/i), { target: { value: 'sig' } });
    await fireEvent.input(screen.getByLabelText(/^Share$/i), { target: { value: 'share-1' } });
    await fireEvent.click(screen.getByRole('button', { name: /Submit share/i }));

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

  it('rejects an empty passphrase without calling the unseal route', async () => {
    const calls = stubFetch(
      { strategy: 'passphrase', sealed: true, initialized: true, shamir_threshold: 0, shamir_total: 0 },
    );
    render(TenantBarrierView);

    await screen.findByTestId('tenant-barrier-passphrase-unseal');
    await fireEvent.click(screen.getByRole('button', { name: /^Unseal$/ }));

    expect(posts(calls, '/unseal')).toHaveLength(0);
  });
});
