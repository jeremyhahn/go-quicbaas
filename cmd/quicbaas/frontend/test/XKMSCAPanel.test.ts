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
import type { XKMSCACertificate } from '@quicbaas/sdk';

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

import XKMSCAPanel from '../src/components/security/XKMSCAPanel.svelte';

const CA_PEM = '-----BEGIN CERTIFICATE-----\nMIICAroot\n-----END CERTIFICATE-----';
const LEAF_PEM = '-----BEGIN CERTIFICATE-----\nMIICleaf\n-----END CERTIFICATE-----';
const CHAIN_PEM = '-----BEGIN CERTIFICATE-----\nMIICchain\n-----END CERTIFICATE-----';
// A marker, not key material: the panel shows whatever the server returns.
const KEY_MARKER = 'issued-key-material-marker';
const CSR_PEM = '-----BEGIN CERTIFICATE REQUEST-----\nMIICcsr\n-----END CERTIFICATE REQUEST-----';

const CA_CERT: XKMSCACertificate = {
  certificate_pem: CA_PEM,
  subject: 'CN=xkms Root',
  issuer: 'CN=xkms Root Issuer',
  serial_number: 'ca01',
  not_before: '2026-01-01T00:00:00Z',
  not_after: '2036-01-01T00:00:00Z',
  is_ca: true,
};

type Fn = ReturnType<typeof vi.fn>;
let ca: Record<string, Fn>;

function newCA(): Record<string, Fn> {
  return {
    xkmsGetCACertificate: vi.fn().mockResolvedValue(CA_CERT),
    xkmsGetCABundle: vi.fn().mockResolvedValue({
      bundle_pem: CA_PEM, certificates: [CA_PEM], content_type: 'application/x-pem-file',
    }),
    xkmsIssueCertificate: vi.fn().mockResolvedValue({
      certificate_pem: LEAF_PEM, chain_pem: CHAIN_PEM, private_key_pem: KEY_MARKER, serial_number: 'beef01',
    }),
    xkmsSignCSR: vi.fn().mockResolvedValue({
      certificate_pem: LEAF_PEM, chain_pem: CHAIN_PEM, serial_number: 'cafe02',
    }),
    xkmsRevokeCertificate: vi.fn().mockResolvedValue({ success: true }),
  };
}

function toastText(): string {
  return JSON.stringify(mockShowToast.mock.calls);
}

async function loaded(): Promise<void> {
  await waitFor(() => expect(screen.getByText('CN=xkms Root')).toBeInTheDocument());
}

describe('XKMSCAPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    ca = newCA();
    mockGetSDKClient.mockResolvedValue({ ca });
  });

  it('shows the CA certificate metadata and PEM on mount', async () => {
    render(XKMSCAPanel);
    await loaded();
    expect(ca.xkmsGetCACertificate).toHaveBeenCalledWith(undefined);
    expect(screen.getByText('CN=xkms Root Issuer')).toBeInTheDocument();
    expect(screen.getByText('ca01')).toBeInTheDocument();
    expect(screen.getByText(/2026-01-01T00:00:00Z/)).toBeInTheDocument();
    expect(screen.getByText(/2036-01-01T00:00:00Z/)).toBeInTheDocument();
    expect(screen.getByTestId('ca-is-ca')).toHaveTextContent('yes');
    expect(screen.getByTestId('ca-cert-pem')).toHaveTextContent('MIICAroot');
    expect(ca.xkmsGetCABundle).not.toHaveBeenCalled();
  });

  it('passes an identity to xkmsGetCACertificate and an empty one as undefined', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.input(screen.getByLabelText('Identity (optional)'), { target: { value: 'tenant-a' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Load CA certificate' }));
    await waitFor(() => expect(ca.xkmsGetCACertificate).toHaveBeenLastCalledWith('tenant-a'));
    await fireEvent.input(screen.getByLabelText('Identity (optional)'), { target: { value: '  ' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Load CA certificate' }));
    await waitFor(() => expect(ca.xkmsGetCACertificate).toHaveBeenCalledTimes(3));
    expect(ca.xkmsGetCACertificate).toHaveBeenLastCalledWith(undefined);
  });

  it('gets the bundle with only the options that are set and shows its PEM', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Get CA bundle' }));
    await waitFor(() => expect(ca.xkmsGetCABundle).toHaveBeenCalledWith({}));
    await waitFor(() => expect(screen.getByTestId('ca-bundle-pem')).toHaveTextContent('MIICAroot'));
    await fireEvent.input(screen.getByLabelText('Store type (optional)'), { target: { value: 'pkcs8' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Get CA bundle' }));
    await waitFor(() => expect(ca.xkmsGetCABundle).toHaveBeenLastCalledWith({ store_type: 'pkcs8' }));
    await fireEvent.input(screen.getByLabelText('Bundle algorithm (optional)'), { target: { value: 'ecdsa' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Get CA bundle' }));
    await waitFor(() => expect(ca.xkmsGetCABundle).toHaveBeenLastCalledWith({ store_type: 'pkcs8', algorithm: 'ecdsa' }));
  });

  it('issues a certificate with every field and shows the private key once', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Issue certificate' }));
    const d = screen.getByRole('dialog', { name: 'Issue certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Profile'), { target: { value: 'server' } });
    await fireEvent.input(within(d).getByLabelText('Common name'), { target: { value: 'api.example' } });
    await fireEvent.input(within(d).getByLabelText('Organization (optional)'), { target: { value: 'Example' } });
    await fireEvent.input(within(d).getByLabelText('SANs (comma-separated)'), { target: { value: 'a.example, b.example,' } });
    await fireEvent.input(within(d).getByLabelText('Validity days (optional)'), { target: { value: '90' } });
    await fireEvent.input(within(d).getByLabelText('Algorithm (optional)'), { target: { value: 'ecdsa' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Issue' }));
    await waitFor(() => expect(ca.xkmsIssueCertificate).toHaveBeenCalledWith({
      profile: 'server', common_name: 'api.example', organization: 'Example',
      sans: ['a.example', 'b.example'], validity_days: 90, algorithm: 'ecdsa',
    }));
    const r = await screen.findByRole('dialog', { name: 'Issued certificate dialog' });
    expect(within(r).getByTestId('issued-cert-pem')).toHaveTextContent('MIICleaf');
    expect(within(r).getByTestId('issued-chain-pem')).toHaveTextContent('MIICchain');
    expect(within(r).getByTestId('issued-private-key')).toHaveTextContent(KEY_MARKER);
    expect(r).toHaveTextContent('beef01');
    expect(toastText()).toContain('beef01');
    expect(toastText()).not.toContain(KEY_MARKER);
    expect(toastText()).not.toContain('BEGIN CERTIFICATE');
    await fireEvent.click(within(r).getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('dialog', { name: 'Issued certificate dialog' })).not.toBeInTheDocument();
    expect(screen.queryByText(new RegExp(KEY_MARKER))).not.toBeInTheDocument();
  });

  it('issues with only profile and common name set', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Issue certificate' }));
    const d = screen.getByRole('dialog', { name: 'Issue certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Profile'), { target: { value: 'client' } });
    await fireEvent.input(within(d).getByLabelText('Common name'), { target: { value: 'svc' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Issue' }));
    await waitFor(() => expect(ca.xkmsIssueCertificate).toHaveBeenCalledWith({ profile: 'client', common_name: 'svc' }));
  });

  it('refuses an issue without a profile or common name', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Issue certificate' }));
    const d = screen.getByRole('dialog', { name: 'Issue certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Common name'), { target: { value: 'svc' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Issue' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('Profile is required');
    await fireEvent.input(within(d).getByLabelText('Profile'), { target: { value: 'server' } });
    await fireEvent.input(within(d).getByLabelText('Common name'), { target: { value: ' ' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Issue' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('Common name is required');
    expect(ca.xkmsIssueCertificate).not.toHaveBeenCalled();
  });

  it('signs a CSR with its profile and validity and shows the certificate', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Sign CSR' }));
    const d = screen.getByRole('dialog', { name: 'Sign CSR dialog' });
    await fireEvent.input(within(d).getByLabelText('CSR PEM'), { target: { value: CSR_PEM } });
    await fireEvent.input(within(d).getByLabelText('Profile (optional)'), { target: { value: 'server' } });
    await fireEvent.input(within(d).getByLabelText('Validity days (optional)'), { target: { value: '30' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Sign' }));
    await waitFor(() => expect(ca.xkmsSignCSR).toHaveBeenCalledWith({
      csr_pem: CSR_PEM, profile: 'server', validity_days: 30,
    }));
    const r = await screen.findByRole('dialog', { name: 'Signed certificate dialog' });
    expect(within(r).getByTestId('signed-cert-pem')).toHaveTextContent('MIICleaf');
    expect(r).toHaveTextContent('cafe02');
    expect(toastText()).not.toContain('BEGIN');
  });

  it('refuses a CSR that is not a PEM certificate request', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Sign CSR' }));
    const d = screen.getByRole('dialog', { name: 'Sign CSR dialog' });
    await fireEvent.input(within(d).getByLabelText('CSR PEM'), { target: { value: LEAF_PEM } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Sign' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('BEGIN CERTIFICATE REQUEST');
    expect(ca.xkmsSignCSR).not.toHaveBeenCalled();
  });

  it('revokes only after a confirmation, with the reason as a number', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Revoke certificate' }));
    const d = screen.getByRole('dialog', { name: 'Revoke certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Serial number'), { target: { value: 'beef01' } });
    expect((within(d).getByLabelText('Reason') as HTMLSelectElement).value).toBe('0');
    await fireEvent.change(within(d).getByLabelText('Reason'), { target: { value: '1' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Revoke' }));
    expect(ca.xkmsRevokeCertificate).not.toHaveBeenCalled();
    expect(d).toHaveTextContent('beef01');
    await fireEvent.click(within(d).getByRole('button', { name: 'Confirm revoke' }));
    await waitFor(() => expect(ca.xkmsRevokeCertificate).toHaveBeenCalledWith('beef01', 1));
    await waitFor(() => expect(mockShowToast).toHaveBeenCalledWith(expect.objectContaining({ type: 'success' })));
    expect(toastText()).toContain('beef01');
    expect(screen.queryByRole('dialog', { name: 'Revoke certificate dialog' })).not.toBeInTheDocument();
  });

  it('offers the RFC 5280 reason codes', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Revoke certificate' }));
    const d = screen.getByRole('dialog', { name: 'Revoke certificate dialog' });
    const values = Array.from((within(d).getByLabelText('Reason') as HTMLSelectElement).options).map((o) => o.value);
    expect(values).toEqual(['0', '1', '2', '3', '4', '5', '6', '8', '9', '10']);
  });

  it('makes no revoke call on Cancel and refuses an empty serial', async () => {
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Revoke certificate' }));
    const d = screen.getByRole('dialog', { name: 'Revoke certificate dialog' });
    await fireEvent.click(within(d).getByRole('button', { name: 'Revoke' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('Serial number is required');
    await fireEvent.input(within(d).getByLabelText('Serial number'), { target: { value: 'beef01' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Revoke' }));
    await fireEvent.click(within(d).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog', { name: 'Revoke certificate dialog' })).not.toBeInTheDocument();
    expect(ca.xkmsRevokeCertificate).not.toHaveBeenCalled();
  });

  it('shows a refused revoke message inline', async () => {
    ca.xkmsRevokeCertificate.mockResolvedValue({ success: false, message: 'serial not found' });
    render(XKMSCAPanel);
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Revoke certificate' }));
    const d = screen.getByRole('dialog', { name: 'Revoke certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Serial number'), { target: { value: 'nope' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Revoke' }));
    await fireEvent.click(within(d).getByRole('button', { name: 'Confirm revoke' }));
    await waitFor(() => expect(within(d).getByRole('alert')).toHaveTextContent('serial not found'));
    expect(mockShowToast).not.toHaveBeenCalled();
  });

  it('shows a 403 from the CA certificate in its section and still offers the bundle', async () => {
    ca.xkmsGetCACertificate.mockRejectedValue(new APIError(403, 'ca cert denied'));
    ca.xkmsGetCABundle.mockRejectedValue(new APIError(403, 'bundle denied'));
    render(XKMSCAPanel);
    await waitFor(() => expect(screen.getByTestId('ca-cert-error')).toHaveTextContent('ca cert denied'));
    await fireEvent.click(screen.getByRole('button', { name: 'Get CA bundle' }));
    await waitFor(() => expect(screen.getByTestId('ca-bundle-error')).toHaveTextContent('bundle denied'));
  });

  it('shows a 403 from issue, sign and revoke in their dialogs', async () => {
    ca.xkmsIssueCertificate.mockRejectedValue(new APIError(403, 'issue denied'));
    ca.xkmsSignCSR.mockRejectedValue(new APIError(403, 'sign denied'));
    ca.xkmsRevokeCertificate.mockRejectedValue(new APIError(403, 'revoke denied'));
    render(XKMSCAPanel);
    await loaded();

    await fireEvent.click(screen.getByRole('button', { name: 'Issue certificate' }));
    let d = screen.getByRole('dialog', { name: 'Issue certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Profile'), { target: { value: 'server' } });
    await fireEvent.input(within(d).getByLabelText('Common name'), { target: { value: 'svc' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Issue' }));
    await waitFor(() => expect(within(d).getByRole('alert')).toHaveTextContent('issue denied'));
    await fireEvent.click(within(d).getByRole('button', { name: 'Cancel' }));

    await fireEvent.click(screen.getByRole('button', { name: 'Sign CSR' }));
    d = screen.getByRole('dialog', { name: 'Sign CSR dialog' });
    await fireEvent.input(within(d).getByLabelText('CSR PEM'), { target: { value: CSR_PEM } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Sign' }));
    await waitFor(() => expect(within(d).getByRole('alert')).toHaveTextContent('sign denied'));
    await fireEvent.click(within(d).getByRole('button', { name: 'Cancel' }));

    await fireEvent.click(screen.getByRole('button', { name: 'Revoke certificate' }));
    d = screen.getByRole('dialog', { name: 'Revoke certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Serial number'), { target: { value: 'beef01' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Revoke' }));
    await fireEvent.click(within(d).getByRole('button', { name: 'Confirm revoke' }));
    await waitFor(() => expect(within(d).getByRole('alert')).toHaveTextContent('revoke denied'));
  });

  it('shows the not-available state and makes no call without ca', async () => {
    mockGetSDKClient.mockResolvedValue({ ca: null });
    render(XKMSCAPanel);
    await waitFor(() => expect(screen.getByText(/certificate authority is not available/i)).toBeInTheDocument());
    expect(ca.xkmsGetCACertificate).not.toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: 'Issue certificate' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Get CA bundle' })).not.toBeInTheDocument();
  });
});
