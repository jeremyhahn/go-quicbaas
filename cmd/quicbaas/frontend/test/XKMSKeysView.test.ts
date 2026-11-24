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
import type { XKMSKeyInfo, XKMSBackendInfo, XKMSBackendCapabilities } from '@quicbaas/sdk';

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

import XKMSKeysView from '../src/views/security/XKMSKeysView.svelte';

function caps(over: Partial<XKMSBackendCapabilities> = {}): XKMSBackendCapabilities {
  return {
    Keys: true, HardwareBacked: false, Signing: true, Decryption: false, KeyRotation: true,
    SymmetricEncryption: false, Sealing: false, Import: false, Export: false, KeyAgreement: false,
    ECIES: false, Attestation: false, QuantumSigning: false, KeyEncapsulation: false,
    ...over,
  };
}

const BACKENDS: XKMSBackendInfo[] = [
  { id: 'pkcs8', type: 'software', hardware_backed: false, capabilities: caps() },
  { id: 'tpm2', type: 'tpm', hardware_backed: true, capabilities: caps({ HardwareBacked: true }) },
];

const KEYS: XKMSKeyInfo[] = [
  { key_id: 'signer', key_type: 'rsa', algorithm: 'RS256', backend: 'pkcs8' },
  { key_id: 'sealer', key_type: 'ecdsa', algorithm: 'ES256', backend: 'tpm2' },
];

type Fn = ReturnType<typeof vi.fn>;
let xkmsKeys: Record<string, Fn>;
let xkms: Record<string, Fn>;
let ca: Record<string, Fn>;
let piv: Record<string, Fn>;

function newKeys(): Record<string, Fn> {
  return {
    listKeys: vi.fn().mockResolvedValue({ keys: KEYS }),
    generateKey: vi.fn().mockResolvedValue({ key_id: 'new-key', key_type: 'rsa' }),
    getKey: vi.fn().mockResolvedValue(KEYS[0]),
    deleteKey: vi.fn(),
    rotateKey: vi.fn(),
    encrypt: vi.fn(),
    decrypt: vi.fn(),
    sign: vi.fn(),
    verify: vi.fn(),
    listKeyVersions: vi.fn().mockResolvedValue({ key_id: 'signer', versions: [], total: 0 }),
    enableAllKeyVersions: vi.fn(),
    disableAllKeyVersions: vi.fn(),
    enableKeyVersion: vi.fn(),
    disableKeyVersion: vi.fn(),
  };
}

function newXKMS(): Record<string, Fn> {
  return {
    health: vi.fn().mockResolvedValue({ status: 'ok', version: '1.2.0' }),
    listBackends: vi.fn().mockResolvedValue({ backends: BACKENDS }),
    getBackend: vi.fn().mockImplementation((id: string) => Promise.resolve(BACKENDS.find((b) => b.id === id))),
    listCertificates: vi.fn().mockResolvedValue({ certificates: [] }),
    getCertificate: vi.fn(),
    saveCertificate: vi.fn(),
    deleteCertificate: vi.fn(),
    canSeal: vi.fn(),
    seal: vi.fn(),
    unseal: vi.fn(),
  };
}

function newPIV(): Record<string, Fn> {
  return {
    listSlots: vi.fn().mockResolvedValue({
      slots: [{ slot: '9a', name: 'PIV Authentication', description: '', has_cert: false, has_key: false }],
    }),
    getCertificate: vi.fn(),
    storeCertificate: vi.fn(),
    deleteCertificate: vi.fn(),
    generateKey: vi.fn(),
    generateCSR: vi.fn(),
  };
}

function newCA(): Record<string, Fn> {
  return {
    xkmsGetCACertificate: vi.fn().mockResolvedValue({
      certificate_pem: '', subject: 'CN=page root', issuer: 'CN=page root', serial_number: 'ca99',
      not_before: '', not_after: '', is_ca: true,
    }),
    xkmsGetCABundle: vi.fn(),
    xkmsSignCSR: vi.fn(),
    xkmsIssueCertificate: vi.fn(),
    xkmsRevokeCertificate: vi.fn(),
  };
}

function dialog(): HTMLElement {
  return screen.getByRole('dialog', { name: 'Generate key dialog' });
}

describe('XKMSKeysView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    xkmsKeys = newKeys();
    xkms = newXKMS();
    ca = newCA();
    piv = newPIV();
    mockGetSDKClient.mockResolvedValue({ xkmsKeys, xkms, ca, piv });
  });

  it('lists keys with their id, type, algorithm and backend', async () => {
    render(XKMSKeysView);
    await waitFor(() => expect(screen.getByText('signer')).toBeInTheDocument());
    expect(xkmsKeys.listKeys).toHaveBeenCalledWith();
    expect(screen.getByText('sealer')).toBeInTheDocument();
    expect(screen.getByText('RS256')).toBeInTheDocument();
    expect(screen.getByText('ES256')).toBeInTheDocument();
    expect(screen.getByText('ecdsa')).toBeInTheDocument();
    const table = screen.getByRole('table', { name: 'Key Store keys' });
    expect(within(table).getByText('pkcs8')).toBeInTheDocument();
    expect(within(table).getByText('tpm2')).toBeInTheDocument();
  });

  it('shows the health badge and the backends list', async () => {
    render(XKMSKeysView);
    await waitFor(() => expect(screen.getByTestId('health-badge')).toHaveTextContent('ok'));
    expect(xkms.health).toHaveBeenCalledTimes(1);
    const list = await screen.findByRole('list', { name: 'Backends' });
    expect(within(list).getByText('pkcs8')).toBeInTheDocument();
    expect(within(list).getByText('tpm2')).toBeInTheDocument();
  });

  it('shows a failed health call on its own and still loads the keys', async () => {
    xkms.health.mockRejectedValue(new APIError(503, 'xkms unreachable'));
    render(XKMSKeysView);
    await waitFor(() => expect(screen.getByTestId('health-error')).toHaveTextContent('xkms unreachable'));
    expect(screen.getByText('signer')).toBeInTheDocument();
    expect(screen.queryByTestId('health-badge')).not.toBeInTheDocument();
  });

  it('filters the keys by the chosen backend and shows its capabilities', async () => {
    render(XKMSKeysView);
    await waitFor(() => expect(screen.getByRole('option', { name: 'tpm2' })).toBeInTheDocument());
    xkmsKeys.listKeys.mockResolvedValue({ keys: [KEYS[1]] });
    await fireEvent.change(screen.getByLabelText('Backend'), { target: { value: 'tpm2' } });
    await waitFor(() => expect(xkmsKeys.listKeys).toHaveBeenCalledWith('tpm2'));
    await waitFor(() => expect(xkms.getBackend).toHaveBeenCalledWith('tpm2'));
    await waitFor(() => expect(screen.getByTestId('backend-capabilities')).toHaveTextContent('HardwareBacked'));
    await waitFor(() => expect(screen.queryByText('signer')).not.toBeInTheDocument());
  });

  it('generates an RSA key with its size, then toasts and reloads', async () => {
    render(XKMSKeysView);
    await waitFor(() => screen.getByText('signer'));
    await fireEvent.click(screen.getByRole('button', { name: 'Generate key' }));
    const d = dialog();
    await fireEvent.input(within(d).getByLabelText('Key ID'), { target: { value: 'new-key' } });
    await fireEvent.input(within(d).getByLabelText('Backend'), { target: { value: 'pkcs8' } });
    await fireEvent.change(within(d).getByLabelText('Key size'), { target: { value: '4096' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    await waitFor(() => expect(xkmsKeys.generateKey).toHaveBeenCalledWith({
      key_id: 'new-key', backend: 'pkcs8', key_type: 'rsa', key_size: 4096,
    }));
    await waitFor(() => expect(mockShowToast).toHaveBeenCalledWith(expect.objectContaining({ type: 'success' })));
    expect(xkmsKeys.listKeys).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole('dialog', { name: 'Generate key dialog' })).not.toBeInTheDocument();
  });

  it('generates an EC key with its curve and algorithm', async () => {
    render(XKMSKeysView);
    await waitFor(() => screen.getByText('signer'));
    await fireEvent.click(screen.getByRole('button', { name: 'Generate key' }));
    const d = dialog();
    await fireEvent.input(within(d).getByLabelText('Key ID'), { target: { value: 'ec-key' } });
    await fireEvent.input(within(d).getByLabelText('Backend'), { target: { value: 'tpm2' } });
    await fireEvent.change(within(d).getByLabelText('Key type'), { target: { value: 'ecdsa' } });
    await fireEvent.change(within(d).getByLabelText('Curve'), { target: { value: 'P-384' } });
    await fireEvent.input(within(d).getByLabelText('Algorithm (optional)'), { target: { value: 'ES384' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    await waitFor(() => expect(xkmsKeys.generateKey).toHaveBeenCalledWith({
      key_id: 'ec-key', backend: 'tpm2', key_type: 'ecdsa', curve: 'P-384', algorithm: 'ES384',
    }));
  });

  it('refuses an empty key id or backend without a call', async () => {
    xkms.listBackends.mockResolvedValue({ backends: [] });
    render(XKMSKeysView);
    await waitFor(() => screen.getByText('signer'));
    await fireEvent.click(screen.getByRole('button', { name: 'Generate key' }));
    const d = dialog();
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('Key ID is required');
    await fireEvent.input(within(d).getByLabelText('Key ID'), { target: { value: 'k1' } });
    await fireEvent.input(within(d).getByLabelText('Backend'), { target: { value: '  ' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Generate' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('Backend is required');
    expect(xkmsKeys.generateKey).not.toHaveBeenCalled();
  });

  it('shows a 403 from listKeys inline', async () => {
    xkmsKeys.listKeys.mockRejectedValue(new APIError(403, 'xkms keys denied'));
    render(XKMSKeysView);
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('xkms keys denied'));
  });

  it('shows the not-available state and makes no call without xkmsKeys', async () => {
    mockGetSDKClient.mockResolvedValue({ xkmsKeys: null, xkms: null });
    render(XKMSKeysView);
    await waitFor(() => expect(screen.getByText(/Key Store is not available on this connection/)).toBeInTheDocument());
    expect(xkmsKeys.listKeys).not.toHaveBeenCalled();
    expect(xkms.health).not.toHaveBeenCalled();
    expect(xkms.listBackends).not.toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: 'Generate key' })).not.toBeInTheDocument();
  });

  it('opens the key panel from Manage and closes it', async () => {
    render(XKMSKeysView);
    await waitFor(() => screen.getByText('signer'));
    await fireEvent.click(screen.getByRole('button', { name: 'Manage key signer' }));
    const panel = await screen.findByRole('dialog', { name: 'Manage key signer' });
    await waitFor(() => expect(xkmsKeys.getKey).toHaveBeenCalledWith('signer', 'pkcs8'));
    await fireEvent.click(within(panel).getByRole('button', { name: 'Close key panel' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Manage key signer' })).not.toBeInTheDocument());
  });

  it('renders the Certificates and Certificate authority sections', async () => {
    render(XKMSKeysView);
    await waitFor(() => screen.getByText('signer'));
    const certs = screen.getByRole('region', { name: 'Certificates' });
    await waitFor(() => expect(xkms.listCertificates).toHaveBeenCalledWith());
    expect(within(certs).getByRole('table', { name: 'Key Store certificates' })).toBeInTheDocument();
    const authority = screen.getByRole('region', { name: 'Certificate authority' });
    await waitFor(() => expect(within(authority).getByText('ca99')).toBeInTheDocument());
    expect(ca.xkmsGetCACertificate).toHaveBeenCalledTimes(1);
  });

  it('passes the chosen backend to the certificates section', async () => {
    render(XKMSKeysView);
    await waitFor(() => expect(screen.getByRole('option', { name: 'tpm2' })).toBeInTheDocument());
    await fireEvent.change(screen.getByLabelText('Backend'), { target: { value: 'tpm2' } });
    await waitFor(() => expect(xkms.listCertificates).toHaveBeenCalledWith('tpm2'));
  });

  it('renders the Seal and unseal and PIV slots sections', async () => {
    render(XKMSKeysView);
    await waitFor(() => screen.getByText('signer'));
    const seal = screen.getByRole('region', { name: 'Seal and unseal' });
    expect(within(seal).getByRole('button', { name: 'Check seal capability' })).toBeInTheDocument();
    expect(xkms.canSeal).not.toHaveBeenCalled();
    const slots = screen.getByRole('region', { name: 'PIV slots' });
    await waitFor(() => expect(within(slots).getByText('PIV Authentication')).toBeInTheDocument());
    expect(piv.listSlots).toHaveBeenCalledWith();
  });

  it('passes the chosen backend to the seal and PIV sections', async () => {
    render(XKMSKeysView);
    await waitFor(() => expect(screen.getByRole('option', { name: 'tpm2' })).toBeInTheDocument());
    await fireEvent.change(screen.getByLabelText('Backend'), { target: { value: 'tpm2' } });
    await waitFor(() => expect(piv.listSlots).toHaveBeenCalledWith('tpm2'));
    const seal = screen.getByRole('region', { name: 'Seal and unseal' });
    await waitFor(() => expect((within(seal).getByLabelText('Seal backend') as HTMLInputElement).value).toBe('tpm2'));
  });
});
