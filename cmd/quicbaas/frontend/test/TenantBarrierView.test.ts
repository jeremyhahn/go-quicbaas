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
  getSDKClient: vi.fn(async () => ({
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
