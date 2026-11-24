// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/svelte';
import { APIError } from '@quicbaas/sdk';
import type { ListPIVSlotsResponse, PIVSlotStatus } from '@quicbaas/sdk';

const { mockGetSDKClient, mockShowToast } = vi.hoisted(() => ({
  mockGetSDKClient: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

import XKMSPIVPanel from '../src/components/security/XKMSPIVPanel.svelte';

const PEM = '-----BEGIN CERTIFICATE-----\nMIIBpiv\n-----END CERTIFICATE-----';
const PUB = '-----BEGIN PUBLIC KEY-----\nMFkpub\n-----END PUBLIC KEY-----';
const CSR = '-----BEGIN CERTIFICATE REQUEST-----\nMIIBcsr\n-----END CERTIFICATE REQUEST-----';

const SLOTS: PIVSlotStatus[] = [
  {
    slot: '9a', name: 'PIV Authentication', description: 'auth', has_cert: true, has_key: true,
    subject: 'CN=alice', algorithm: 'ECDSAP256', not_after: '2027-01-01T00:00:00Z',
  },
  {
    slot: '9c', name: 'Digital Signature', description: 'sign', has_cert: false, has_key: false,
  },
];

type Fn = ReturnType<typeof vi.fn>;
let piv: Record<string, Fn>;

function newPIV(): Record<string, Fn> {
  return {
    listSlots: vi.fn().mockResolvedValue({ slots: SLOTS }),
    getCertificate: vi.fn().mockResolvedValue({ slot: '9a', certificate: PEM, format: 'pem' }),
    storeCertificate: vi.fn().mockResolvedValue(undefined),
    deleteCertificate: vi.fn().mockResolvedValue(undefined),
    generateKey: vi.fn().mockResolvedValue({ slot: '9c', certificate: PEM, public_key: PUB }),
    generateCSR: vi.fn().mockResolvedValue({ slot: '9c', csr: CSR }),
  };
}

function toastText(): string {
  return JSON.stringify(mockShowToast.mock.calls);
}

async function loaded(backend = ''): Promise<void> {
  render(XKMSPIVPanel, { props: { backend } });
  await waitFor(() => expect(screen.getByText('PIV Authentication')).toBeInTheDocument());
}

describe('XKMSPIVPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    piv = newPIV();
    mockGetSDKClient.mockResolvedValue({ piv });
  });

  it('lists the slots with name, cert and key flags, subject, algorithm and expiry', async () => {
    await loaded();
    expect(piv.listSlots).toHaveBeenCalledWith();
    expect(screen.getByText('9a')).toBeInTheDocument();
    expect(screen.getByText('9c')).toBeInTheDocument();
    expect(screen.getByText('Digital Signature')).toBeInTheDocument();
    expect(screen.getByText('CN=alice')).toBeInTheDocument();
    expect(screen.getByText('ECDSAP256')).toBeInTheDocument();
    expect(screen.getByText('2027-01-01T00:00:00Z')).toBeInTheDocument();
    expect(screen.getByTestId('piv-has-cert-9a')).toHaveTextContent('Yes');
    expect(screen.getByTestId('piv-has-key-9a')).toHaveTextContent('Yes');
    expect(screen.getByTestId('piv-has-cert-9c')).toHaveTextContent('No');
    expect(screen.getByTestId('piv-has-key-9c')).toHaveTextContent('No');
  });

  it('lists by the backend prop, reloads on a change and drops a late answer for the old backend', async () => {
    let resolveOld: (v: ListPIVSlotsResponse) => void = () => {};
    piv.listSlots.mockImplementationOnce(
      () => new Promise<ListPIVSlotsResponse>((r) => { resolveOld = r; }),
    );
    piv.listSlots.mockResolvedValue({ slots: [SLOTS[1]] });
    const { rerender } = render(XKMSPIVPanel, { props: { backend: 'yubikey' } });
    await waitFor(() => expect(piv.listSlots).toHaveBeenCalledWith('yubikey'));
    await rerender({ backend: 'pkcs11' });
    await waitFor(() => expect(piv.listSlots).toHaveBeenCalledWith('pkcs11'));
    await waitFor(() => expect(screen.getByText('Digital Signature')).toBeInTheDocument());
    resolveOld({ slots: [SLOTS[0]] });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByText('PIV Authentication')).not.toBeInTheDocument();
  });

  it('views a slot certificate PEM through getCertificate with the backend when set', async () => {
    await loaded('yubikey');
    await fireEvent.click(screen.getByRole('button', { name: 'View certificate 9a' }));
    await waitFor(() => expect(piv.getCertificate).toHaveBeenCalledWith('9a', { backend: 'yubikey', format: 'pem' }));
    const d = await screen.findByRole('dialog', { name: 'Slot 9a certificate' });
    expect(within(d).getByTestId('piv-cert-pem')).toHaveTextContent('BEGIN CERTIFICATE');
  });

  it('views without a backend when none is chosen', async () => {
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'View certificate 9a' }));
    await waitFor(() => expect(piv.getCertificate).toHaveBeenCalledWith('9a', { format: 'pem' }));
  });

  it('stores a PEM certificate in a slot, toasts the slot only and reloads', async () => {
    await loaded('yubikey');
    await fireEvent.click(screen.getByRole('button', { name: 'Store certificate 9c' }));
    const d = screen.getByRole('dialog', { name: 'Store certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Certificate PEM'), { target: { value: PEM } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Store' }));
    await waitFor(() => expect(piv.storeCertificate).toHaveBeenCalledWith('9c', {
      certificate: PEM, format: 'pem', backend: 'yubikey',
    }));
    await waitFor(() => expect(mockShowToast).toHaveBeenCalledWith(expect.objectContaining({ type: 'success' })));
    expect(toastText()).toContain('9c');
    expect(toastText()).not.toContain('BEGIN');
    await waitFor(() => expect(piv.listSlots).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole('dialog', { name: 'Store certificate dialog' })).not.toBeInTheDocument();
  });

  it('refuses a PEM that is not a certificate without a call', async () => {
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Store certificate 9c' }));
    const d = screen.getByRole('dialog', { name: 'Store certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Certificate PEM'), { target: { value: 'not a pem' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Store' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('BEGIN CERTIFICATE');
    expect(piv.storeCertificate).not.toHaveBeenCalled();
  });

  it('deletes a slot certificate only after a confirmation that names the slot, then reloads', async () => {
    await loaded('yubikey');
    await fireEvent.click(screen.getByRole('button', { name: 'Delete certificate 9a' }));
    const d = screen.getByRole('dialog', { name: 'Delete certificate dialog' });
    expect(d).toHaveTextContent('9a');
    expect(piv.deleteCertificate).not.toHaveBeenCalled();
    await fireEvent.click(within(d).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(piv.deleteCertificate).toHaveBeenCalledWith('9a', 'yubikey'));
    await waitFor(() => expect(piv.listSlots).toHaveBeenCalledTimes(2));
    expect(toastText()).toContain('9a');
  });

  it('makes no delete call when the confirmation is cancelled', async () => {
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Delete certificate 9a' }));
    const d = screen.getByRole('dialog', { name: 'Delete certificate dialog' });
    await fireEvent.click(within(d).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog', { name: 'Delete certificate dialog' })).not.toBeInTheDocument();
    expect(piv.deleteCertificate).not.toHaveBeenCalled();
  });

  it('generates a key with the algorithm, subject and set optional fields and shows the result', async () => {
    await loaded('yubikey');
    await fireEvent.click(screen.getByRole('button', { name: 'Generate key 9c' }));
    const d = screen.getByRole('dialog', { name: 'Generate key dialog' });
    const algo = within(d).getByLabelText('Algorithm') as HTMLSelectElement;
    expect(algo.value).toBe('ecdsap256');
    expect(Array.from(algo.options).map((o) => o.value)).toEqual(['rsa2048', 'rsa4096', 'ecdsap256', 'ecdsap384', 'ed25519']);
    await fireEvent.change(algo, { target: { value: 'rsa2048' } });
    await fireEvent.input(within(d).getByLabelText('Subject'), { target: { value: 'CN=bob' } });
    await fireEvent.input(within(d).getByLabelText('Organization'), { target: { value: 'Example' } });
    await fireEvent.input(within(d).getByLabelText('Validity days'), { target: { value: '90' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    await waitFor(() => expect(piv.generateKey).toHaveBeenCalledWith('9c', {
      backend: 'yubikey', algorithm: 'rsa2048', subject: 'CN=bob', organization: 'Example', validity_days: 90,
    }));
    const r = await screen.findByRole('dialog', { name: 'Slot 9c key' });
    expect(within(r).getByTestId('piv-key-cert')).toHaveTextContent('BEGIN CERTIFICATE');
    expect(within(r).getByTestId('piv-key-public')).toHaveTextContent('BEGIN PUBLIC KEY');
    expect(toastText()).not.toContain('BEGIN');
  });

  it('refuses an empty subject for key generation without a call', async () => {
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Generate key 9c' }));
    const d = screen.getByRole('dialog', { name: 'Generate key dialog' });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('Subject is required');
    expect(piv.generateKey).not.toHaveBeenCalled();
  });

  it('asks for a confirmation that names the slot before replacing a key, and Cancel makes no call', async () => {
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Generate key 9a' }));
    let d = screen.getByRole('dialog', { name: 'Generate key dialog' });
    await fireEvent.input(within(d).getByLabelText('Subject'), { target: { value: 'CN=alice' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    let c = screen.getByRole('dialog', { name: 'Replace key dialog' });
    expect(c).toHaveTextContent('9a');
    await fireEvent.click(within(c).getByRole('button', { name: 'Cancel' }));
    expect(piv.generateKey).not.toHaveBeenCalled();

    d = screen.getByRole('dialog', { name: 'Generate key dialog' });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    c = screen.getByRole('dialog', { name: 'Replace key dialog' });
    await fireEvent.click(within(c).getByRole('button', { name: 'Replace key' }));
    await waitFor(() => expect(piv.generateKey).toHaveBeenCalledWith('9a', { algorithm: 'ecdsap256', subject: 'CN=alice' }));
  });

  it('generates a CSR with the subject and set optional fields and shows it', async () => {
    await loaded('yubikey');
    await fireEvent.click(screen.getByRole('button', { name: 'Generate CSR 9c' }));
    const d = screen.getByRole('dialog', { name: 'Generate CSR dialog' });
    await fireEvent.input(within(d).getByLabelText('Subject'), { target: { value: 'CN=carol' } });
    await fireEvent.input(within(d).getByLabelText('Organizational unit'), { target: { value: 'Ops' } });
    await fireEvent.input(within(d).getByLabelText('Locality'), { target: { value: 'Austin' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    await waitFor(() => expect(piv.generateCSR).toHaveBeenCalledWith('9c', {
      backend: 'yubikey', subject: 'CN=carol', organizational_unit: 'Ops', locality: 'Austin',
    }));
    const r = await screen.findByRole('dialog', { name: 'Slot 9c CSR' });
    expect(within(r).getByTestId('piv-csr')).toHaveTextContent('BEGIN CERTIFICATE REQUEST');
    expect(toastText()).not.toContain('BEGIN');
  });

  it('refuses an empty subject for a CSR without a call', async () => {
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Generate CSR 9c' }));
    const d = screen.getByRole('dialog', { name: 'Generate CSR dialog' });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('Subject is required');
    expect(piv.generateCSR).not.toHaveBeenCalled();
  });

  it('shows a 403 from the list inline', async () => {
    piv.listSlots.mockRejectedValue(new APIError(403, 'slots denied'));
    render(XKMSPIVPanel, { props: { backend: '' } });
    expect(await screen.findByRole('alert')).toHaveTextContent('slots denied');
  });

  it('shows a 403 from view, delete, store, key and CSR inline', async () => {
    piv.getCertificate.mockRejectedValue(new APIError(403, 'view denied'));
    piv.deleteCertificate.mockRejectedValue(new APIError(403, 'delete denied'));
    piv.storeCertificate.mockRejectedValue(new APIError(403, 'store denied'));
    piv.generateKey.mockRejectedValue(new APIError(403, 'key denied'));
    piv.generateCSR.mockRejectedValue(new APIError(403, 'csr denied'));
    await loaded();

    await fireEvent.click(screen.getByRole('button', { name: 'View certificate 9a' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('view denied');

    await fireEvent.click(screen.getByRole('button', { name: 'Delete certificate 9a' }));
    await fireEvent.click(within(screen.getByRole('dialog', { name: 'Delete certificate dialog' })).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('delete denied'));

    await fireEvent.click(screen.getByRole('button', { name: 'Store certificate 9c' }));
    let d = screen.getByRole('dialog', { name: 'Store certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Certificate PEM'), { target: { value: PEM } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Store' }));
    await waitFor(() => expect(within(d).getByRole('alert')).toHaveTextContent('store denied'));
    await fireEvent.click(within(d).getByRole('button', { name: 'Cancel' }));

    await fireEvent.click(screen.getByRole('button', { name: 'Generate key 9c' }));
    d = screen.getByRole('dialog', { name: 'Generate key dialog' });
    await fireEvent.input(within(d).getByLabelText('Subject'), { target: { value: 'CN=x' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    await waitFor(() => expect(within(d).getByRole('alert')).toHaveTextContent('key denied'));
    await fireEvent.click(within(d).getByRole('button', { name: 'Cancel' }));

    await fireEvent.click(screen.getByRole('button', { name: 'Generate CSR 9c' }));
    d = screen.getByRole('dialog', { name: 'Generate CSR dialog' });
    await fireEvent.input(within(d).getByLabelText('Subject'), { target: { value: 'CN=x' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    await waitFor(() => expect(within(d).getByRole('alert')).toHaveTextContent('csr denied'));
  });

  it('shows the not-available state and makes no call without piv', async () => {
    mockGetSDKClient.mockResolvedValue({ piv: null });
    render(XKMSPIVPanel, { props: { backend: '' } });
    expect(await screen.findByTestId('xkms-piv-not-available')).toBeInTheDocument();
    expect(piv.listSlots).not.toHaveBeenCalled();
  });
});
