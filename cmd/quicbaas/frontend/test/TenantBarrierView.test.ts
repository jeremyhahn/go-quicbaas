/**
 * TenantBarrierView.test.ts
 *
 * Verifies the strategy-none branch's Enable encryption form: it migrates a
 * tenant's unencrypted barrier to a keyed one through
 * client.barrier.enableEncryption (go-quicbaas#2372).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

const listTenants = vi.fn();
const enableEncryption = vi.fn();

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getOriginSDKClient: vi.fn(async () => ({
    iam: { listTenants },
    barrier: { enableEncryption },
  })),
}));

import { showToast } from '../src/stores/toast.svelte.ts';
import TenantBarrierView from '../src/views/security/TenantBarrierView.svelte';

function jsonResponse(data: unknown) {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: 'OK',
    headers: new Headers({ 'content-type': 'application/json' }),
    text: () => Promise.resolve(JSON.stringify(data)),
    json: () => Promise.resolve(data),
  });
}

const fetchMock = vi.fn((url: string) => {
  if (url.includes('/api/v1/tenants/t1/barrier/status')) {
    return jsonResponse({
      tenant_id: 't1',
      strategy: 'none',
      initialized: true,
      sealed: false,
      hardware_backed: false,
      shamir_threshold: 0,
      shamir_total: 0,
    });
  }
  return jsonResponse({});
});

async function renderNone() {
  render(TenantBarrierView);
  await waitFor(() => {
    expect(screen.getByTestId('tenant-barrier-none')).toBeInTheDocument();
  });
  return screen.getByRole('form', { name: 'Enable encryption' });
}

function enableButton() {
  return screen.getByRole('button', { name: 'Enable' });
}

async function typePassphrase(pw: string, confirm: string) {
  await fireEvent.input(screen.getByLabelText('Passphrase'), { target: { value: pw } });
  await fireEvent.input(screen.getByLabelText('Confirm passphrase'), {
    target: { value: confirm },
  });
}

async function confirmEnable() {
  await fireEvent.click(enableButton());
  await waitFor(() => {
    expect(screen.getByTestId('tenant-barrier-enable-confirm')).toBeInTheDocument();
  });
  expect(screen.getByTestId('tenant-barrier-enable-confirm')).toHaveTextContent(/re-encrypted/);
  await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
}

describe('TenantBarrierView enable encryption', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listTenants.mockResolvedValue({ tenants: [{ id: 't1', name: 'acme' }] });
    enableEncryption.mockResolvedValue({ started: true });
    vi.stubGlobal('fetch', fetchMock);
  });

  it('passphrase sends tenant_id, strategy, the defaults and the passphrase bytes', async () => {
    await renderNone();
    await typePassphrase('s3cret-pw', 's3cret-pw');
    expect(enableButton()).not.toBeDisabled();
    await confirmEnable();

    await waitFor(() => {
      expect(enableEncryption).toHaveBeenCalledTimes(1);
    });
    const req = enableEncryption.mock.calls[0][0];
    expect(req.tenant_id).toBe('t1');
    expect(req.strategy).toBe('passphrase');
    expect(req.mechanism).toBe('swap');
    expect(req.availability).toBe('online');
    expect(Array.from(req.passphrase as Uint8Array)).toEqual(
      Array.from(new TextEncoder().encode('s3cret-pw')),
    );
    await waitFor(() => {
      expect(showToast).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'success', message: expect.stringMatching(/started/) }),
      );
    });
    await waitFor(() => {
      expect((screen.getByLabelText('Passphrase') as HTMLInputElement).value).toBe('');
      expect((screen.getByLabelText('Confirm passphrase') as HTMLInputElement).value).toBe('');
    });
  });

  it('a mismatched confirm keeps Enable disabled', async () => {
    await renderNone();
    expect(enableButton()).toBeDisabled();
    await typePassphrase('one', 'two');
    expect(enableButton()).toBeDisabled();
    expect(enableEncryption).not.toHaveBeenCalled();
  });

  it('shamir sends threshold and total', async () => {
    await renderNone();
    await fireEvent.change(screen.getByLabelText('Strategy'), { target: { value: 'shamir' } });
    await fireEvent.input(screen.getByLabelText('Threshold'), { target: { value: '3' } });
    await fireEvent.input(screen.getByLabelText('Total shares'), { target: { value: '5' } });
    expect(enableButton()).not.toBeDisabled();
    await confirmEnable();

    await waitFor(() => {
      expect(enableEncryption).toHaveBeenCalledTimes(1);
    });
    const req = enableEncryption.mock.calls[0][0];
    expect(req.tenant_id).toBe('t1');
    expect(req.strategy).toBe('shamir');
    expect(req.shamir_threshold).toBe(3);
    expect(req.shamir_total).toBe(5);
    expect(req.passphrase).toBeUndefined();
  });

  it('shamir keeps Enable disabled when threshold exceeds total', async () => {
    await renderNone();
    await fireEvent.change(screen.getByLabelText('Strategy'), { target: { value: 'shamir' } });
    await fireEvent.input(screen.getByLabelText('Threshold'), { target: { value: '4' } });
    await fireEvent.input(screen.getByLabelText('Total shares'), { target: { value: '3' } });
    expect(enableButton()).toBeDisabled();
  });

  it('a rejection toasts the error and clears the passphrase inputs', async () => {
    enableEncryption.mockRejectedValue(new Error('migration refused'));
    await renderNone();
    await typePassphrase('pw', 'pw');
    await confirmEnable();

    await waitFor(() => {
      expect(showToast).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'error', message: 'migration refused' }),
      );
    });
    await waitFor(() => {
      expect((screen.getByLabelText('Passphrase') as HTMLInputElement).value).toBe('');
      expect((screen.getByLabelText('Confirm passphrase') as HTMLInputElement).value).toBe('');
    });
  });
});

// The tenant Shamir init response is share-free (#1888): go-qrdb seals every
// share to an officer enclave and REST returns only {tenant_id, strategy,
// threshold, total_shares, share_count}. The view shows a sealed-to-officers
// notice from that body and never reaches a plaintext-shares reveal.
describe('TenantBarrierView init (share-free response)', () => {
  interface FetchCall {
    url: string;
    method: string;
    body: unknown;
  }
  let calls: FetchCall[];
  let initialized: boolean;
  let initStrategy: string;

  beforeEach(() => {
    vi.clearAllMocks();
    calls = [];
    initialized = false;
    initStrategy = '';
    listTenants.mockResolvedValue({ tenants: [{ id: 't1', name: 'acme' }] });
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string, init?: RequestInit) => {
        const method = (init?.method ?? 'GET').toUpperCase();
        const body = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined;
        calls.push({ url, method, body });
        if (url.includes('/api/v1/tenants/t1/barrier/status')) {
          return jsonResponse({
            tenant_id: 't1',
            sealed: true,
            initialized,
            strategy: initialized ? initStrategy : '',
            hardware_backed: false,
            shamir_threshold: initialized && initStrategy === 'shamir' ? 2 : 0,
            shamir_total: initialized && initStrategy === 'shamir' ? 3 : 0,
          });
        }
        if (url.includes('/api/v1/tenants/t1/barrier/init') && method === 'POST') {
          initialized = true;
          initStrategy = (body as { strategy: string }).strategy;
          if (initStrategy !== 'shamir') {
            return Promise.resolve({
              ok: true,
              status: 204,
              statusText: 'No Content',
              headers: new Headers(),
              text: () => Promise.resolve(''),
              json: () => Promise.reject(new Error('no body')),
            });
          }
          // Exactly the body go-qrdb's TenantBarrierInitResponse marshals to:
          // no shares field at all.
          return jsonResponse({
            tenant_id: 't1',
            strategy: 'shamir',
            threshold: 2,
            total_shares: 3,
            share_count: 3,
          });
        }
        return jsonResponse({});
      }),
    );
  });

  async function renderUninitialized(): Promise<void> {
    render(TenantBarrierView);
    await waitFor(() => {
      expect(screen.getByTestId('tenant-barrier-init')).toBeInTheDocument();
    });
  }

  function field(id: string): HTMLInputElement {
    const el = document.getElementById(id);
    if (!(el instanceof HTMLInputElement)) throw new Error(`missing input #${id}`);
    return el;
  }

  async function initShamir(): Promise<void> {
    await renderUninitialized();
    await fireEvent.click(screen.getByTestId('barrier-strategy-radio-shamir'));
    await fireEvent.input(field('barrier-init-threshold'), { target: { value: '2' } });
    await fireEvent.input(field('barrier-init-total'), { target: { value: '3' } });
    await fireEvent.click(screen.getByTestId('barrier-strategy-submit'));
  }

  it('a Shamir init shows the sealed notice and never a share', async () => {
    await initShamir();

    await waitFor(() => {
      expect(screen.getByTestId('tenant-barrier-shamir-sealed')).toBeInTheDocument();
    });
    const notice = screen.getByTestId('tenant-barrier-shamir-sealed');
    expect(notice).toHaveTextContent('2 of 3');
    expect(notice).toHaveTextContent('3 shares are sealed');
    expect(notice).toHaveTextContent('Ceremony page');
    expect(screen.queryByTestId('tenant-barrier-shares-reveal')).toBeNull();
    expect(document.body.textContent ?? '').not.toMatch(/undefined/);

    const init = calls.find((c) => c.url.includes('/barrier/init'));
    expect(init?.body).toEqual({ strategy: 'shamir', threshold: 2, shares: 3 });
    expect(showToast).toHaveBeenCalledWith({
      message: 'Tenant barrier initialized: 3 shares sealed to officers',
      type: 'success',
    });
  });

  it('dismissing the sealed notice reloads status', async () => {
    await initShamir();
    await waitFor(() => {
      expect(screen.getByTestId('tenant-barrier-shamir-sealed')).toBeInTheDocument();
    });
    const statusReads = calls.filter((c) => c.url.includes('/barrier/status')).length;

    await fireEvent.click(screen.getByTestId('tenant-barrier-shamir-sealed-dismiss'));
    await waitFor(() => {
      expect(screen.queryByTestId('tenant-barrier-shamir-sealed')).toBeNull();
    });
    expect(calls.filter((c) => c.url.includes('/barrier/status')).length).toBeGreaterThan(
      statusReads,
    );
    expect(screen.queryByTestId('tenant-barrier-init')).toBeNull();
  });

  it('a passphrase init toasts success and shows no sealed notice', async () => {
    await renderUninitialized();
    await fireEvent.click(screen.getByTestId('barrier-strategy-radio-passphrase'));
    await fireEvent.input(field('barrier-init-passphrase'), { target: { value: 'pw-test' } });
    await fireEvent.input(field('barrier-init-passphrase-confirm'), {
      target: { value: 'pw-test' },
    });
    await fireEvent.click(screen.getByTestId('barrier-strategy-submit'));

    await waitFor(() => {
      expect(showToast).toHaveBeenCalledWith({
        message: 'Tenant barrier initialized',
        type: 'success',
      });
    });
    expect(screen.queryByTestId('tenant-barrier-shamir-sealed')).toBeNull();
  });
});
