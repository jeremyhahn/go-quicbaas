/**
 * CertificatesView.test.ts
 *
 * Verifies that CertificatesView mounts, calls caInfo/listCertificates on
 * init, renders the CA info panel and certificate list, exposes issue /
 * sign-csr / revoke / verify / check-revoked / bundle / CRL affordances that
 * call the corresponding CAClient methods with the right arguments, and
 * degrades gracefully when a fetch fails.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import type { CAInfoResponse, CertificateSummary } from '@quicbaas/sdk';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import CertificatesView from '../src/views/security/CertificatesView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const mockCaInfo = vi.fn();
const mockListCertificates = vi.fn();
const mockIssueCertificate = vi.fn();
const mockSignCSR = vi.fn();
const mockRevokeCertificate = vi.fn();
const mockVerifyCertificate = vi.fn();
const mockIsRevoked = vi.fn();
const mockGetCABundle = vi.fn();
const mockGenerateCRL = vi.fn();

function mockClient(): void {
  vi.mocked(getSDKClient).mockResolvedValue({
    ca: {
      caInfo: mockCaInfo,
      listCertificates: mockListCertificates,
      issueCertificate: mockIssueCertificate,
      signCSR: mockSignCSR,
      revokeCertificate: mockRevokeCertificate,
      verifyCertificate: mockVerifyCertificate,
      isRevoked: mockIsRevoked,
      getCABundle: mockGetCABundle,
      generateCRL: mockGenerateCRL,
    },
  } as never);
}

function caInfo(overrides: Partial<CAInfoResponse> = {}): CAInfoResponse {
  return {
    common_name: 'QuicBaaS Root CA',
    subject: 'CN=QuicBaaS Root CA',
    issuer: 'CN=QuicBaaS Root CA',
    serial_number: '01',
    not_before: '2026-01-01T00:00:00Z',
    not_after: '2036-01-01T00:00:00Z',
    is_ca: true,
    key_type: 'ecdsa',
    key_bits: 256,
    signature_algorithm: 'ECDSA-SHA256',
    certificate_pem: '-----BEGIN CERTIFICATE-----\nMOCK\n-----END CERTIFICATE-----',
    ca_bundle_pem: '-----BEGIN CERTIFICATE-----\nMOCK\n-----END CERTIFICATE-----',
    timestamp: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function cert(overrides: Partial<CertificateSummary> = {}): CertificateSummary {
  return {
    common_name: 'app.example.com',
    subject: 'CN=app.example.com',
    issuer: 'CN=QuicBaaS Root CA',
    serial_number: 'abc123',
    not_before: '2026-01-01T00:00:00Z',
    not_after: '2027-01-01T00:00:00Z',
    is_ca: false,
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockCaInfo.mockResolvedValue(caInfo());
  mockListCertificates.mockResolvedValue({ certificates: [], total_count: 0, timestamp: '' });
  mockIssueCertificate.mockResolvedValue({
    certificate_pem: '-----BEGIN CERTIFICATE-----\nISSUED\n-----END CERTIFICATE-----',
    private_key_pem: '-----BEGIN PRIVATE KEY-----\nSECRET\n-----END PRIVATE KEY-----',
    ca_certificate_pem: '',
    chain_pem: '',
    serial_number: 'new-serial',
    not_before: '',
    not_after: '',
    timestamp: '',
  });
  mockSignCSR.mockResolvedValue({
    certificate_pem: '-----BEGIN CERTIFICATE-----\nSIGNED\n-----END CERTIFICATE-----',
    ca_certificate_pem: '',
    chain_pem: '',
    serial_number: 'signed-serial',
    not_before: '',
    not_after: '',
    timestamp: '',
  });
  mockRevokeCertificate.mockResolvedValue({ success: true, timestamp: '' });
  mockVerifyCertificate.mockResolvedValue({ valid: true, timestamp: '' });
  mockIsRevoked.mockResolvedValue({ revoked: false });
  mockGetCABundle.mockResolvedValue({ bundle_pem: 'BUNDLE', timestamp: '' });
  mockGenerateCRL.mockResolvedValue({ crl_pem: '-----BEGIN X509 CRL-----\nCRL\n-----END X509 CRL-----' });
  mockClient();
  // jsdom doesn't implement createObjectURL/revokeObjectURL or clipboard.
  if (!URL.createObjectURL) {
    URL.createObjectURL = vi.fn(() => 'blob:mock');
  }
  if (!URL.revokeObjectURL) {
    URL.revokeObjectURL = vi.fn();
  }
  Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
});

describe('CertificatesView', () => {
  it('calls caInfo and listCertificates on mount', async () => {
    render(CertificatesView);
    await waitFor(() => {
      expect(mockCaInfo).toHaveBeenCalled();
      expect(mockListCertificates).toHaveBeenCalled();
    });
  });

  it('renders the page heading with icon', async () => {
    const { container } = render(CertificatesView);
    await waitFor(() => {
      const h1 = container.querySelector('h1');
      expect(h1).not.toBeNull();
      expect(h1!.textContent).toContain('Certificates');
      expect(container.querySelector('div.bg-primary\\/10 svg')).not.toBeNull();
    });
  });

  it('renders CA info panel fields', async () => {
    render(CertificatesView);
    await waitFor(() => {
      expect(screen.getByText('QuicBaaS Root CA')).toBeInTheDocument();
      expect(screen.getByText(/ECDSA-SHA256/)).toBeInTheDocument();
    });
  });

  it('renders an empty state when zero certificates are returned', async () => {
    render(CertificatesView);
    await waitFor(() => {
      expect(screen.queryByText(/no certificates found/i)).toBeTruthy();
    });
  });

  it('degrades gracefully when listCertificates fails', async () => {
    mockListCertificates.mockRejectedValue(new Error('boom'));
    render(CertificatesView);
    await waitFor(() => {
      expect(screen.getByText('boom')).toBeInTheDocument();
    });
  });

  it('renders certificate rows when present', async () => {
    mockListCertificates.mockResolvedValue({
      certificates: [cert({ common_name: 'app.example.com' })],
      total_count: 1,
      timestamp: '',
    });
    render(CertificatesView);
    await waitFor(() => {
      expect(screen.getByText('app.example.com')).toBeInTheDocument();
    });
  });
});

describe('CertificatesView — issue dialog', () => {
  it('issues a certificate and shows the one-time private key result', async () => {
    const { container } = render(CertificatesView);
    await waitFor(() => expect(mockListCertificates).toHaveBeenCalled());

    const issueBtn = container.querySelector('button[aria-label="Issue certificate"]') as HTMLButtonElement;
    await fireEvent.click(issueBtn);
    await waitFor(() => expect(container.querySelector('#issue-cn')).not.toBeNull());

    await fireEvent.input(container.querySelector('#issue-cn') as HTMLInputElement, {
      target: { value: 'new.example.com' },
    });

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Issue',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(mockIssueCertificate).toHaveBeenCalled());
    const req = mockIssueCertificate.mock.calls[0][0];
    expect(req.common_name).toBe('new.example.com');

    await waitFor(() => {
      expect(screen.getByText(/shown only once/i)).toBeInTheDocument();
      expect(screen.getByText(/SECRET/)).toBeInTheDocument();
    });
  });

  it('rejects submit client-side when common name is missing', async () => {
    const { container } = render(CertificatesView);
    const issueBtn = container.querySelector('button[aria-label="Issue certificate"]') as HTMLButtonElement;
    await fireEvent.click(issueBtn);
    await waitFor(() => expect(container.querySelector('#issue-cn')).not.toBeNull());

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Issue',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(screen.getByText(/common name is required/i)).toBeInTheDocument());
    expect(mockIssueCertificate).not.toHaveBeenCalled();
  });
});

describe('CertificatesView — sign CSR dialog', () => {
  it('signs a CSR and shows the result', async () => {
    const { container } = render(CertificatesView);
    const signBtn = screen.getByRole('button', { name: /sign csr/i });
    await fireEvent.click(signBtn);
    await waitFor(() => expect(container.querySelector('#csr-pem')).not.toBeNull());

    await fireEvent.input(container.querySelector('#csr-pem') as HTMLTextAreaElement, {
      target: { value: '-----BEGIN CERTIFICATE REQUEST-----\nMOCK\n-----END CERTIFICATE REQUEST-----' },
    });

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Sign',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(mockSignCSR).toHaveBeenCalled());
    expect(mockSignCSR.mock.calls[0][0].csr_pem).toContain('CERTIFICATE REQUEST');
    await waitFor(() => expect(screen.getByText('CSR Signed')).toBeInTheDocument());
  });
});

describe('CertificatesView — revoke', () => {
  it('revokes a certificate after confirmation', async () => {
    mockListCertificates.mockResolvedValue({
      certificates: [cert({ common_name: 'app.example.com', serial_number: 'serial-xyz' })],
      total_count: 1,
      timestamp: '',
    });
    const { container } = render(CertificatesView);
    const revokeBtn = await waitFor(() => {
      const el = container.querySelector('button[aria-label="Revoke certificate app.example.com"]');
      expect(el).not.toBeNull();
      return el as HTMLButtonElement;
    });
    await fireEvent.click(revokeBtn);
    await waitFor(() => expect(screen.getByText('Revoke Certificate?')).toBeInTheDocument());

    const confirmBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Revoke',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);

    await waitFor(() => expect(mockRevokeCertificate).toHaveBeenCalledWith('serial-xyz'));
  });
});

describe('CertificatesView — utilities', () => {
  it('verifies a pasted certificate', async () => {
    const { container } = render(CertificatesView);
    const verifyOpenBtn = screen.getByRole('button', { name: /verify certificate/i });
    await fireEvent.click(verifyOpenBtn);
    await waitFor(() => expect(container.querySelector('#verify-pem')).not.toBeNull());

    await fireEvent.input(container.querySelector('#verify-pem') as HTMLTextAreaElement, {
      target: { value: '-----BEGIN CERTIFICATE-----\nMOCK\n-----END CERTIFICATE-----' },
    });
    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Verify',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(mockVerifyCertificate).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByText(/certificate is valid/i)).toBeInTheDocument());
  });

  it('checks revocation status for a serial number', async () => {
    mockIsRevoked.mockResolvedValue({ revoked: true, message: 'revoked by admin' });
    const { container } = render(CertificatesView);
    const checkOpenBtn = screen.getByRole('button', { name: /check revocation status/i });
    await fireEvent.click(checkOpenBtn);
    await waitFor(() => expect(container.querySelector('#check-serial')).not.toBeNull());

    await fireEvent.input(container.querySelector('#check-serial') as HTMLInputElement, {
      target: { value: 'serial-abc' },
    });
    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Check',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(mockIsRevoked).toHaveBeenCalledWith('serial-abc'));
    await waitFor(() => expect(screen.getByText(/certificate is revoked/i)).toBeInTheDocument());
  });

  it('downloads the CA bundle when Bundle is clicked', async () => {
    render(CertificatesView);
    const bundleBtn = screen.getByRole('button', { name: /download ca bundle/i });
    await fireEvent.click(bundleBtn);
    await waitFor(() => expect(mockGetCABundle).toHaveBeenCalled());
  });

  it('generates a CRL when CRL is clicked', async () => {
    render(CertificatesView);
    const crlBtn = screen.getByRole('button', { name: /generate crl/i });
    await fireEvent.click(crlBtn);
    await waitFor(() => expect(mockGenerateCRL).toHaveBeenCalled());
  });
});
