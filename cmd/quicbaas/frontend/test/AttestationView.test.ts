/**
 * AttestationView.test.ts
 *
 * Verifies that AttestationView:
 *   - Lists the endorsements and the reference values.
 *   - Filters the endorsements by kind through listEndorsements.
 *   - Adds an endorsement with the PEM in the chosen field.
 *   - Uploads reference values with the file's bytes in base64.
 *   - Deletes an endorsement and reference values after the confirmation,
 *     then reloads the list.
 *   - Shows a load error.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { Endorsement, ReferenceValues } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockEndorsements: Endorsement[] = [
  {
    name: 'vendor-signer',
    kind: 'reference-signer',
    certificate_pem: '-----BEGIN CERTIFICATE-----\nAAA\n-----END CERTIFICATE-----',
    created_at: '2026-11-01T10:00:00Z',
  },
  {
    name: 'tpm-root',
    kind: 'ak-root',
    public_key_pem: '-----BEGIN PUBLIC KEY-----\nBBB\n-----END PUBLIC KEY-----',
    created_at: '2026-11-02T10:00:00Z',
  },
];

const mockReferenceValues: ReferenceValues[] = [
  {
    name: 'fw-2026-11',
    manifest_id: 'manifest-abc',
    profile: 'tag:quicbaas,2026:tpm',
    signer: 'vendor-signer',
    not_before: '2026-11-01T00:00:00Z',
    not_after: '2027-11-01T00:00:00Z',
    pcr_indices: [0, 4, 7],
    stored_at: '2026-11-03T10:00:00Z',
  },
];

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const {
  mockListEndorsements,
  mockPutEndorsement,
  mockDeleteEndorsement,
  mockListReferenceValues,
  mockPutReferenceValues,
  mockDeleteReferenceValues,
  mockGetSDKClient,
} = vi.hoisted(() => ({
  mockListEndorsements: vi.fn(),
  mockPutEndorsement: vi.fn(),
  mockDeleteEndorsement: vi.fn(),
  mockListReferenceValues: vi.fn(),
  mockPutReferenceValues: vi.fn(),
  mockDeleteReferenceValues: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import AttestationView from '../src/views/security/AttestationView.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('AttestationView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListEndorsements.mockResolvedValue({ endorsements: mockEndorsements });
    mockPutEndorsement.mockResolvedValue(mockEndorsements[0]);
    mockDeleteEndorsement.mockResolvedValue(undefined);
    mockListReferenceValues.mockResolvedValue({ reference_values: mockReferenceValues });
    mockPutReferenceValues.mockResolvedValue(mockReferenceValues[0]);
    mockDeleteReferenceValues.mockResolvedValue(undefined);
    mockGetSDKClient.mockResolvedValue({
      attestation: {
        listEndorsements: mockListEndorsements,
        putEndorsement: mockPutEndorsement,
        deleteEndorsement: mockDeleteEndorsement,
        listReferenceValues: mockListReferenceValues,
        getReferenceValues: vi.fn(),
        putReferenceValues: mockPutReferenceValues,
        deleteReferenceValues: mockDeleteReferenceValues,
      },
    });
  });

  it('lists the endorsements and the reference values', async () => {
    render(AttestationView);
    await waitFor(() => {
      expect(screen.getByText('tpm-root')).toBeInTheDocument();
      expect(screen.getByText('manifest-abc')).toBeInTheDocument();
      expect(screen.getByText('0, 4, 7')).toBeInTheDocument();
    });
    expect(mockListEndorsements).toHaveBeenCalledWith(undefined);
  });

  it('filters the endorsements by kind', async () => {
    render(AttestationView);
    await waitFor(() => screen.getByText('tpm-root'));

    await fireEvent.change(screen.getByLabelText('Endorsement kind filter'), { target: { value: 'ak-root' } });
    await waitFor(() => {
      expect(mockListEndorsements).toHaveBeenLastCalledWith('ak-root');
    });
  });

  it('adds an endorsement with the PEM in the chosen field', async () => {
    render(AttestationView);
    await waitFor(() => screen.getByText('tpm-root'));

    await fireEvent.input(screen.getByLabelText('Endorsement name'), { target: { value: 'new-root' } });
    await fireEvent.change(screen.getByLabelText('Endorsement kind'), { target: { value: 'ak-root' } });
    await fireEvent.click(screen.getByLabelText('Public key'));
    await fireEvent.input(screen.getByLabelText('PEM'), { target: { value: 'PEM-DATA' } });
    await fireEvent.click(screen.getByRole('button', { name: /Add Endorsement/i }));

    await waitFor(() => {
      expect(mockPutEndorsement).toHaveBeenCalledWith('new-root', { kind: 'ak-root', public_key_pem: 'PEM-DATA' });
    });
  });

  it('uploads reference values with the file bytes in base64', async () => {
    render(AttestationView);
    await waitFor(() => screen.getByText('manifest-abc'));

    const file = new File(['corim-bytes'], 'fw.cbor', { type: 'application/cbor' });
    await fireEvent.input(screen.getByLabelText('Reference values name'), { target: { value: 'fw-next' } });
    await fireEvent.change(screen.getByLabelText('Signed CoRIM file'), { target: { files: [file] } });
    await fireEvent.click(screen.getByRole('button', { name: /^Upload$/ }));

    await waitFor(() => {
      expect(mockPutReferenceValues).toHaveBeenCalledWith('fw-next', btoa('corim-bytes'));
    });
  });

  it('deletes an endorsement after the confirmation and reloads', async () => {
    render(AttestationView);
    await waitFor(() => screen.getByText('tpm-root'));
    const before = mockListEndorsements.mock.calls.length;

    await fireEvent.click(screen.getByRole('button', { name: /Delete endorsement tpm-root/i }));
    expect(screen.getByRole('dialog', { name: /Delete confirmation/i })).toBeInTheDocument();
    expect(mockDeleteEndorsement).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: /^Delete$/ }));
    await waitFor(() => {
      expect(mockDeleteEndorsement).toHaveBeenCalledWith('tpm-root');
      expect(mockListEndorsements.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it('deletes reference values after the confirmation and reloads', async () => {
    render(AttestationView);
    await waitFor(() => screen.getByText('manifest-abc'));
    const before = mockListReferenceValues.mock.calls.length;

    await fireEvent.click(screen.getByRole('button', { name: /Delete reference values fw-2026-11/i }));
    expect(screen.getByRole('dialog', { name: /Delete confirmation/i })).toBeInTheDocument();
    expect(mockDeleteReferenceValues).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: /^Delete$/ }));
    await waitFor(() => {
      expect(mockDeleteReferenceValues).toHaveBeenCalledWith('fw-2026-11');
      expect(mockListReferenceValues.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it('shows a load error', async () => {
    mockListEndorsements.mockRejectedValue(new Error('Verifier unavailable'));
    render(AttestationView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Verifier unavailable');
    });
  });
});
