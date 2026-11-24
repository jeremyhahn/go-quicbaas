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
import type { XKMSCertificateInfo, XKMSListCertificatesResponse } from '@quicbaas/sdk';

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

import XKMSCertificatesPanel from '../src/components/security/XKMSCertificatesPanel.svelte';

const PEM = '-----BEGIN CERTIFICATE-----\nMIIBcert\n-----END CERTIFICATE-----';

const CERTS: XKMSCertificateInfo[] = [
  {
    key_id: 'web-tls', subject: 'CN=web.example', issuer: 'CN=Root CA',
    serial_number: '0a1b2c', not_after: '2027-01-01T00:00:00Z',
  },
  {
    key_id: 'mtls-client', subject: 'CN=client', issuer: 'CN=Issuing CA',
    serial_number: '3d4e5f', not_after: '2026-12-31T00:00:00Z',
  },
];

type Fn = ReturnType<typeof vi.fn>;
let xkms: Record<string, Fn>;

function newXKMS(): Record<string, Fn> {
  return {
    listCertificates: vi.fn().mockResolvedValue({ certificates: CERTS }),
    getCertificate: vi.fn().mockResolvedValue({ key_id: 'web-tls', certificate_pem: PEM }),
    saveCertificate: vi.fn().mockResolvedValue(undefined),
    deleteCertificate: vi.fn().mockResolvedValue(undefined),
  };
}

function toastText(): string {
  return JSON.stringify(mockShowToast.mock.calls);
}

describe('XKMSCertificatesPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    xkms = newXKMS();
    mockGetSDKClient.mockResolvedValue({ xkms });
  });

  it('lists the certificates with key id, subject, issuer, serial and expiry', async () => {
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => expect(screen.getByText('web-tls')).toBeInTheDocument());
    expect(xkms.listCertificates).toHaveBeenCalledWith();
    expect(screen.getByText('mtls-client')).toBeInTheDocument();
    expect(screen.getByText('CN=web.example')).toBeInTheDocument();
    expect(screen.getByText('CN=Issuing CA')).toBeInTheDocument();
    expect(screen.getByText('0a1b2c')).toBeInTheDocument();
    expect(screen.getByText('2027-01-01T00:00:00Z')).toBeInTheDocument();
  });

  it('lists by the backend prop, reloads on a change and drops a late answer for the old backend', async () => {
    let resolveOld: (v: XKMSListCertificatesResponse) => void = () => {};
    xkms.listCertificates.mockImplementationOnce(
      () => new Promise<XKMSListCertificatesResponse>((r) => { resolveOld = r; }),
    );
    xkms.listCertificates.mockResolvedValue({ certificates: [CERTS[1]] });
    const { rerender } = render(XKMSCertificatesPanel, { props: { backend: 'pkcs8' } });
    await waitFor(() => expect(xkms.listCertificates).toHaveBeenCalledWith('pkcs8'));
    await rerender({ backend: 'tpm2' });
    await waitFor(() => expect(xkms.listCertificates).toHaveBeenCalledWith('tpm2'));
    await waitFor(() => expect(screen.getByText('mtls-client')).toBeInTheDocument());
    resolveOld({ certificates: [CERTS[0]] });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByText('web-tls')).not.toBeInTheDocument();
    expect(screen.getByText('mtls-client')).toBeInTheDocument();
  });

  it('views a certificate PEM through getCertificate', async () => {
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => screen.getByText('web-tls'));
    await fireEvent.click(screen.getByRole('button', { name: 'View certificate web-tls' }));
    await waitFor(() => expect(xkms.getCertificate).toHaveBeenCalledWith('web-tls'));
    const d = await screen.findByRole('dialog', { name: 'Certificate web-tls' });
    expect(within(d).getByTestId('cert-pem')).toHaveTextContent('BEGIN CERTIFICATE');
  });

  it('saves a PEM for a key id, then toasts the key id and reloads', async () => {
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => screen.getByText('web-tls'));
    await fireEvent.click(screen.getByRole('button', { name: 'Save certificate' }));
    const d = screen.getByRole('dialog', { name: 'Save certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Key ID'), { target: { value: 'new-cert' } });
    await fireEvent.input(within(d).getByLabelText('Certificate PEM'), { target: { value: PEM } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(xkms.saveCertificate).toHaveBeenCalledWith('new-cert', PEM));
    await waitFor(() => expect(mockShowToast).toHaveBeenCalledWith(expect.objectContaining({ type: 'success' })));
    expect(toastText()).toContain('new-cert');
    expect(toastText()).not.toContain('BEGIN CERTIFICATE');
    await waitFor(() => expect(xkms.listCertificates).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole('dialog', { name: 'Save certificate dialog' })).not.toBeInTheDocument();
  });

  it('refuses an empty key id or a PEM that is not a certificate without a call', async () => {
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => screen.getByText('web-tls'));
    await fireEvent.click(screen.getByRole('button', { name: 'Save certificate' }));
    const d = screen.getByRole('dialog', { name: 'Save certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Certificate PEM'), { target: { value: PEM } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Save' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('Key ID is required');
    await fireEvent.input(within(d).getByLabelText('Key ID'), { target: { value: 'k1' } });
    await fireEvent.input(within(d).getByLabelText('Certificate PEM'), { target: { value: 'not a pem' } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Save' }));
    expect(within(d).getByRole('alert')).toHaveTextContent('BEGIN CERTIFICATE');
    expect(xkms.saveCertificate).not.toHaveBeenCalled();
  });

  it('deletes only after a confirmation that names the key id, then reloads', async () => {
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => screen.getByText('web-tls'));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete certificate web-tls' }));
    const d = screen.getByRole('dialog', { name: 'Delete certificate dialog' });
    expect(d).toHaveTextContent('web-tls');
    expect(xkms.deleteCertificate).not.toHaveBeenCalled();
    await fireEvent.click(within(d).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(xkms.deleteCertificate).toHaveBeenCalledWith('web-tls'));
    await waitFor(() => expect(xkms.listCertificates).toHaveBeenCalledTimes(2));
    expect(toastText()).toContain('web-tls');
  });

  it('makes no delete call on Cancel', async () => {
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => screen.getByText('web-tls'));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete certificate web-tls' }));
    const d = screen.getByRole('dialog', { name: 'Delete certificate dialog' });
    await fireEvent.click(within(d).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog', { name: 'Delete certificate dialog' })).not.toBeInTheDocument();
    expect(xkms.deleteCertificate).not.toHaveBeenCalled();
  });

  it('shows a 403 from listCertificates inline', async () => {
    xkms.listCertificates.mockRejectedValue(new APIError(403, 'certificates denied'));
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('certificates denied'));
  });

  it('shows a 403 from getCertificate and deleteCertificate inline', async () => {
    xkms.getCertificate.mockRejectedValue(new APIError(403, 'view denied'));
    xkms.deleteCertificate.mockRejectedValue(new APIError(403, 'delete denied'));
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => screen.getByText('web-tls'));
    await fireEvent.click(screen.getByRole('button', { name: 'View certificate web-tls' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('view denied'));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete certificate web-tls' }));
    const d = screen.getByRole('dialog', { name: 'Delete certificate dialog' });
    await fireEvent.click(within(d).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('delete denied'));
  });

  it('shows a 403 from saveCertificate in the dialog', async () => {
    xkms.saveCertificate.mockRejectedValue(new APIError(403, 'save denied'));
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => screen.getByText('web-tls'));
    await fireEvent.click(screen.getByRole('button', { name: 'Save certificate' }));
    const d = screen.getByRole('dialog', { name: 'Save certificate dialog' });
    await fireEvent.input(within(d).getByLabelText('Key ID'), { target: { value: 'k1' } });
    await fireEvent.input(within(d).getByLabelText('Certificate PEM'), { target: { value: PEM } });
    await fireEvent.click(within(d).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(within(d).getByRole('alert')).toHaveTextContent('save denied'));
  });

  it('shows the not-available state and makes no call without xkms', async () => {
    mockGetSDKClient.mockResolvedValue({ xkms: null });
    render(XKMSCertificatesPanel, { props: { backend: '' } });
    await waitFor(() => expect(screen.getByText(/certificate store is not available/i)).toBeInTheDocument());
    expect(xkms.listCertificates).not.toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: 'Save certificate' })).not.toBeInTheDocument();
  });
});
