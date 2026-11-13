/**
 * TenantBarrierView.test.ts
 *
 * The tenant Shamir init response is share-free (go-quicbaas#1888): go-qrdb
 * seals every share to an officer enclave and the REST surface returns only
 * {tenant_id, strategy, threshold, total_shares, share_count}. The view must
 * render a sealed-to-officers summary from that body and never reach the
 * plaintext-shares reveal, whose shares.length access threw on undefined.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    iam: {
      listTenants: vi.fn().mockResolvedValue({
        tenants: [{ id: 't1', name: 'Tenant One' }],
      }),
    },
  }),
}));

import TenantBarrierView from '../src/views/security/TenantBarrierView.svelte';

interface FetchCall {
  url: string;
  method: string;
  body: unknown;
}

function jsonResponse(data: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: 'OK',
    headers: new Headers({ 'content-type': 'application/json' }),
    text: () => Promise.resolve(status === 204 ? '' : JSON.stringify(data)),
    json: () => Promise.resolve(data),
  } as unknown as Response;
}

describe('TenantBarrierView Shamir init (share-free response)', () => {
  let calls: FetchCall[];
  let initialized: boolean;

  beforeEach(() => {
    vi.clearAllMocks();
    calls = [];
    initialized = false;
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        const method = (init?.method ?? 'GET').toUpperCase();
        const body = init?.body !== undefined ? JSON.parse(init.body as string) : undefined;
        calls.push({ url, method, body });
        if (url.endsWith('/api/v1/tenants/t1/barrier/status')) {
          return Promise.resolve(
            jsonResponse({
              tenant_id: 't1',
              sealed: true,
              initialized,
              strategy: initialized ? 'shamir' : '',
              hardware_backed: false,
              shamir_threshold: initialized ? 2 : 0,
              shamir_total: initialized ? 3 : 0,
            }),
          );
        }
        if (url.endsWith('/api/v1/tenants/t1/barrier/init') && method === 'POST') {
          initialized = true;
          // Exactly the body go-qrdb's TenantBarrierInitResponse marshals to:
          // no shares field at all.
          return Promise.resolve(
            jsonResponse({
              tenant_id: 't1',
              strategy: 'shamir',
              threshold: 2,
              total_shares: 3,
              share_count: 3,
            }),
          );
        }
        return Promise.resolve(jsonResponse({}));
      }),
    );
  });

  async function initShamir(): Promise<void> {
    render(TenantBarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('tenant-barrier-init')).toBeInTheDocument();
    });
    await fireEvent.click(screen.getByTestId('barrier-strategy-radio-shamir'));
    await fireEvent.input(screen.getByLabelText(/Threshold/i), { target: { value: '2' } });
    await fireEvent.input(screen.getByLabelText(/Total shares/i), { target: { value: '3' } });
    await fireEvent.click(screen.getByTestId('barrier-strategy-submit'));
  }

  it('renders the sealed-to-officers summary, never a plaintext-shares reveal', async () => {
    await initShamir();

    await waitFor(() => {
      expect(screen.getByTestId('tenant-barrier-shares-sealed')).toBeInTheDocument();
    });
    const summary = screen.getByTestId('tenant-barrier-shares-sealed');
    expect(summary).toHaveTextContent('3 of 3 shares sealed');
    expect(summary).toHaveTextContent('2 of 3');
    expect(screen.queryByTestId('tenant-barrier-shares-reveal')).toBeNull();

    const init = calls.find((c) => c.url.endsWith('/barrier/init'));
    expect(init?.body).toEqual({ strategy: 'shamir', threshold: 2, shares: 3 });
  });

  it('dismissing the summary reloads status into the Shamir unseal flow', async () => {
    await initShamir();
    await waitFor(() => {
      expect(screen.getByTestId('tenant-barrier-shares-sealed')).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByTestId('tenant-barrier-shares-sealed-dismiss'));
    await waitFor(() => {
      expect(screen.queryByTestId('tenant-barrier-shares-sealed')).toBeNull();
    });
    expect(screen.queryByTestId('tenant-barrier-init')).toBeNull();
  });
});
